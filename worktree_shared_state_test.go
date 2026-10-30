package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sharedStateProject is a remote carrying two published actors, alpha and
// beta, each with one issue, and a consumer clone with one linked worktree.
// It drives the built hn binary, so every command runs in its own process.
type sharedStateProject struct {
	binary     string
	root       string
	remote     string
	alpha      string
	beta       string
	consumer   string
	linked     string
	commonDir  string
	linkedGit  string
	alphaIssue string
	betaIssue  string
}

func newSharedStateProject(t *testing.T) sharedStateProject {
	t.Helper()
	binary := buildOperationalBinary(t)
	base := newAllTrustedProject(t, binary, false)
	project := sharedStateProject{binary: binary, root: base.root, remote: base.remote}
	for _, author := range []struct {
		name  string
		actor *string
		title string
		issue *string
	}{
		{"alpha", &project.alpha, "Alpha shared-state issue", &project.alphaIssue},
		{"beta", &project.beta, "Beta shared-state issue", &project.betaIssue},
	} {
		repository, actor := base.cloneWithIdentity(t, author.name)
		*author.actor = actor
		runOperationalCommand(t, binary, repository, "issue", "open", author.title)
		*author.issue = author.title
		runOperationalCommand(t, binary, repository, "sync", "origin")
	}
	// --no-local makes Git transfer only what refs/heads needs, as a clone
	// over the network does; a local clone would link every object of the
	// remote, including the actors' unaccepted history.
	project.consumer = filepath.Join(project.root, "consumer")
	runOperationalGit(t, "", "clone", "-q", "--no-local", project.remote, project.consumer)
	configureOperationalGit(t, project.consumer, "consumer")
	project.linked = filepath.Join(project.root, "consumer-linked")
	runOperationalGit(t, project.consumer, "worktree", "add", "-q", "-b", "linked", project.linked)
	commonDir, err := filepath.EvalSymlinks(filepath.Join(project.consumer, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	project.commonDir = commonDir
	linkedGit, err := filepath.EvalSymlinks(runOperationalGit(t, project.linked, "rev-parse", "--absolute-git-dir"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(linkedGit) != filepath.Join(commonDir, "worktrees") {
		t.Fatalf("linked worktree Git directory %s is not below %s", linkedGit, commonDir)
	}
	project.linkedGit = linkedGit
	return project
}

func (project sharedStateProject) hn(t *testing.T, dir string, env map[string]string, args ...string) (string, int) {
	t.Helper()
	return runAllTrustedCommand(t, project.binary, dir, env, args...)
}

func (project sharedStateProject) mustHN(t *testing.T, dir string, args ...string) string {
	t.Helper()
	output, code := project.hn(t, dir, nil, args...)
	if code != 0 {
		t.Fatalf("hn %s in %s exited %d:\n%s", strings.Join(args, " "), dir, code, output)
	}
	return output
}

func (project sharedStateProject) importedActors(t *testing.T) string {
	t.Helper()
	return runOperationalGit(t, project.consumer, "for-each-ref", "--format=%(refname)", "refs/hn/remotes/origin/actors")
}

func sharedStateEntries(t *testing.T, directory string) []string {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// assertPendingEverywhere checks that every checkout of the clone refuses to
// read history while an interrupted replication's acceptance is pending.
func (project sharedStateProject) assertPendingEverywhere(t *testing.T) {
	t.Helper()
	for _, dir := range []string{project.consumer, project.linked} {
		output, code := project.hn(t, dir, nil, "issue", "list")
		if code == 0 || !strings.Contains(output, "replication acceptance pending") {
			t.Fatalf("hn issue list in %s with pending replication: exit %d:\n%s", dir, code, output)
		}
		if strings.Contains(output, project.alphaIssue) || strings.Contains(output, project.betaIssue) {
			t.Fatalf("hn issue list in %s showed pending history:\n%s", dir, output)
		}
	}
}

func (project sharedStateProject) assertIssuesEverywhere(t *testing.T, titles ...string) {
	t.Helper()
	for _, dir := range []string{project.consumer, project.linked} {
		assertOperationalContains(t, project.mustHN(t, dir, "issue", "list"), titles...)
	}
}

// interruptSyncIn runs hn sync in dir and stops it at a replication process
// boundary, leaving a durable pending journal behind.
func (project sharedStateProject) interruptSyncIn(t *testing.T, dir, boundary, want string) {
	t.Helper()
	output, code := project.hn(t, dir, map[string]string{
		"HN_INTERNAL_TESTING":                 "1",
		"HN_TEST_REPLICATION_INTERRUPT_AFTER": boundary,
	}, "sync", "origin")
	if code == 0 || !strings.Contains(output, want) {
		t.Fatalf("hn sync in %s interrupted at %s: exit %d:\n%s", dir, boundary, code, output)
	}
}

// interruptAfterPromotion leaves the accepted refs promoted and their objects
// copied while the transaction receipt still says validated.
func (project sharedStateProject) interruptAfterPromotion(t *testing.T, dir string) {
	t.Helper()
	project.interruptSyncIn(t, dir, "before-completion-receipt", "completion recording failed")
}

// interruptAfterAnchor leaves only a durable pending anchor, before any
// object is copied or ref promoted.
func (project sharedStateProject) interruptAfterAnchor(t *testing.T, dir string) {
	t.Helper()
	project.interruptSyncIn(t, dir, "after-pending-anchor", "interrupted after pending anchor before object copy")
}

func (project sharedStateProject) moveJournalToLegacyWorktree(t *testing.T, names ...string) string {
	t.Helper()
	legacy := filepath.Join(project.linkedGit, "hn", "replication")
	for _, directory := range []string{filepath.Join(project.linkedGit, "hn"), legacy} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range names {
		if err := os.Rename(filepath.Join(project.commonDir, "hn", "replication", name), filepath.Join(legacy, name)); err != nil {
			t.Fatal(err)
		}
	}
	return legacy
}

// moveSelectionToLegacyWorktree moves the clone-wide selection for origin
// below the linked worktree's private Git directory, where releases before
// selections became clone-wide saved it, and returns its new path.
func (project sharedStateProject) moveSelectionToLegacyWorktree(t *testing.T) string {
	t.Helper()
	legacy := replicationSelectionPathAt(project.linkedGit, "origin")
	for _, directory := range []string{
		filepath.Join(project.linkedGit, "hn"),
		filepath.Join(project.linkedGit, "hn", "replication"),
		filepath.Dir(legacy),
	} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Rename(replicationSelectionPathAt(project.commonDir, "origin"), legacy); err != nil {
		t.Fatal(err)
	}
	return legacy
}

type sharedStateResult struct {
	output string
	code   int
}

// startHN starts hn in dir in its own process and delivers its combined
// output and exit status when it exits.
func (project sharedStateProject) startHN(t *testing.T, dir string, env map[string]string, args ...string) <-chan sharedStateResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	command := exec.CommandContext(ctx, project.binary, args...)
	command.Dir = dir
	command.Env = operationalTestEnvironmentWith(t.TempDir(), env)
	command.Stdin = strings.NewReader("")
	output := &bytes.Buffer{}
	command.Stdout = output
	command.Stderr = output
	if err := command.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	done := make(chan sharedStateResult, 1)
	go func() {
		defer cancel()
		err := command.Wait()
		result := sharedStateResult{output: output.String()}
		var exitErr *exec.ExitError
		switch {
		case err == nil:
		case errors.As(err, &exitErr):
			result.code = exitErr.ExitCode()
		default:
			result.code = -1
			result.output += "\n" + err.Error()
		}
		done <- result
	}()
	return done
}

// overlapStateReader runs readerArgs paused inside its scan of clone state
// at step, after it has read the clone-wide location and before it reads the
// legacy linked-worktree ones, and runs writerArgs while it is paused. The
// writer gets up to five seconds before the reader resumes: a writer that
// does not wait for the reader's shared clone state lock finishes inside the
// pause, so the reader then reads the legacy locations after the writer
// changed them. It reports whether the writer finished during the pause.
func (project sharedStateProject) overlapStateReader(t *testing.T, step, readerDir string, readerArgs []string,
	writerDir string, writerEnv map[string]string, writerArgs []string) (sharedStateResult, sharedStateResult, bool) {
	t.Helper()
	control := t.TempDir()
	paused := filepath.Join(control, "paused")
	release := filepath.Join(control, "release")
	readerDone := project.startHN(t, readerDir, map[string]string{
		"HN_INTERNAL_TESTING":    "1",
		"HN_TEST_STATE_PAUSE_AT": step,
		"HN_TEST_STATE_PAUSED":   paused,
		"HN_TEST_STATE_RELEASE":  release,
	}, readerArgs...)
	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if _, err := os.Stat(paused); err == nil {
			break
		}
		select {
		case result := <-readerDone:
			t.Fatalf("hn %s exited %d before pausing at %s:\n%s", strings.Join(readerArgs, " "), result.code, step, result.output)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("hn %s never paused at %s", strings.Join(readerArgs, " "), step)
		}
	}
	writerDone := project.startHN(t, writerDir, writerEnv, writerArgs...)
	var writer sharedStateResult
	writerFinished := false
	select {
	case writer = <-writerDone:
		writerFinished = true
	case <-time.After(5 * time.Second):
	}
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	reader := <-readerDone
	if !writerFinished {
		writer = <-writerDone
	}
	return reader, writer, writerFinished
}

func TestWorktreeReplicationPendingStateIsCloneWide(t *testing.T) {
	project := newSharedStateProject(t)

	// A sync interrupted in the linked worktree after promotion records its
	// pending state clone-wide, so the main checkout refuses the promoted
	// but unaccepted history as the linked worktree does.
	project.interruptAfterPromotion(t, project.linked)
	if imported := project.importedActors(t); !strings.Contains(imported, project.alpha) || !strings.Contains(imported, project.beta) {
		t.Fatalf("interrupted sync did not promote both actors:\n%s", imported)
	}
	if entries := sharedStateEntries(t, filepath.Join(project.commonDir, "hn", "replication", "anchors")); len(entries) != 1 {
		t.Fatalf("clone-wide pending anchors = %v, want exactly one", entries)
	}
	if entries := sharedStateEntries(t, filepath.Join(project.linkedGit, "hn", "replication")); len(entries) != 0 {
		t.Fatalf("linked worktree holds private replication state %v", entries)
	}
	project.assertPendingEverywhere(t)
}

func TestWorktreeReplicationInterruptedSyncRecoversFromAnyWorktree(t *testing.T) {
	project := newSharedStateProject(t)
	project.interruptAfterAnchor(t, project.linked)
	anchors := filepath.Join(project.commonDir, "hn", "replication", "anchors")
	if entries := sharedStateEntries(t, anchors); len(entries) != 1 {
		t.Fatalf("clone-wide pending anchors = %v, want exactly one", entries)
	}
	project.assertPendingEverywhere(t)

	// A retry from the other checkout reconciles and completes the
	// interrupted transaction.
	project.mustHN(t, project.consumer, "sync", "origin")
	if entries := sharedStateEntries(t, anchors); len(entries) != 0 {
		t.Fatalf("pending anchors after retry = %v, want none", entries)
	}
	project.assertIssuesEverywhere(t, project.alphaIssue, project.betaIssue)
}

func TestWorktreeReplicationLegacyPendingStateStillGuards(t *testing.T) {
	project := newSharedStateProject(t)
	project.interruptAfterPromotion(t, project.consumer)

	// Releases before the state became clone-wide wrote it below the
	// linked worktree's private Git directory when sync ran there. Move the
	// journal there to reproduce that layout: it must still block the main
	// checkout, which never looked there before.
	project.moveJournalToLegacyWorktree(t, "transactions", "anchors")
	project.assertPendingEverywhere(t)
}

func TestWorktreeReplicationLegacyPendingStateMigratesOnSync(t *testing.T) {
	project := newSharedStateProject(t)
	project.interruptAfterAnchor(t, project.consumer)
	legacy := project.moveJournalToLegacyWorktree(t, "anchors")
	project.assertPendingEverywhere(t)

	// A sync from the main checkout moves the legacy journal clone-wide,
	// recovers it and completes it.
	project.mustHN(t, project.consumer, "sync", "origin")
	if entries := sharedStateEntries(t, filepath.Join(legacy, "anchors")); len(entries) != 0 {
		t.Fatalf("legacy anchors after migration = %v, want none", entries)
	}
	if entries := sharedStateEntries(t, filepath.Join(project.commonDir, "hn", "replication", "anchors")); len(entries) != 0 {
		t.Fatalf("clone-wide pending anchors after recovery = %v, want none", entries)
	}
	project.assertIssuesEverywhere(t, project.alphaIssue, project.betaIssue)
}

// TestWorktreeReplicationReaderOverlappingMigrationSeesPending overlaps a
// reader with the migration of a legacy pending journal. The reader has
// scanned the clone-wide location, still empty, when a sync from the linked
// worktree moves the journal there; it must still find the journal in the
// legacy location rather than miss it in both.
func TestWorktreeReplicationReaderOverlappingMigrationSeesPending(t *testing.T) {
	project := newSharedStateProject(t)
	project.interruptAfterPromotion(t, project.consumer)
	legacy := project.moveJournalToLegacyWorktree(t, "transactions", "anchors")

	reader, writer, writerFinished := project.overlapStateReader(t, "between-replication-locations",
		project.consumer, []string{"log"},
		project.linked, map[string]string{
			"HN_INTERNAL_TESTING":                 "1",
			"HN_TEST_REPLICATION_INTERRUPT_AFTER": "after-legacy-migration",
		}, []string{"sync", "origin"})
	if reader.code == 0 || !strings.Contains(reader.output, "replication acceptance pending; transaction=txn-") ||
		strings.Contains(reader.output, "state=invalid") {
		t.Fatalf("hn log overlapping a legacy journal migration: exit %d:\n%s", reader.code, reader.output)
	}
	if strings.Contains(reader.output, project.alphaIssue) || strings.Contains(reader.output, project.betaIssue) {
		t.Fatalf("hn log overlapping a legacy journal migration showed pending history:\n%s", reader.output)
	}
	if writer.code == 0 || !strings.Contains(writer.output, "interrupted after legacy worktree state migration") {
		t.Fatalf("hn sync migrating the legacy journal: exit %d:\n%s", writer.code, writer.output)
	}
	if writerFinished {
		t.Fatal("the migration finished while the reader was inside its scan")
	}
	for _, name := range []string{"transactions", "anchors"} {
		if entries := sharedStateEntries(t, filepath.Join(legacy, name)); len(entries) != 0 {
			t.Fatalf("legacy %s after migration = %v, want none", name, entries)
		}
	}
	project.assertPendingEverywhere(t)
}

// TestWorktreeReplicationSyncOverlappingSelectionSaveKeepsRestriction
// overlaps sync's selection read with a save that moves the same legacy
// restriction clone-wide. Sync has found no clone-wide selection when the
// save writes it and retires the legacy copy; it must still apply the
// restriction rather than fall back to importing every advertised actor.
func TestWorktreeReplicationSyncOverlappingSelectionSaveKeepsRestriction(t *testing.T) {
	project := newSharedStateProject(t)
	project.mustHN(t, project.linked, "replication", "select", "origin", "--actor", project.alpha)
	legacy := project.moveSelectionToLegacyWorktree(t)

	reader, writer, writerFinished := project.overlapStateReader(t, "between-selection-locations",
		project.consumer, []string{"sync", "origin"},
		project.linked, nil, []string{"replication", "select", "origin", "--actor", project.alpha})
	if reader.code != 0 {
		t.Fatalf("hn sync overlapping a selection save exited %d:\n%s", reader.code, reader.output)
	}
	if writer.code != 0 {
		t.Fatalf("hn replication select overlapping sync exited %d:\n%s", writer.code, writer.output)
	}
	if imported := project.importedActors(t); !strings.Contains(imported, project.alpha) || strings.Contains(imported, project.beta) {
		t.Fatalf("sync overlapping a save of the same restriction imported %q, want only %s", imported, project.alpha)
	}
	if writerFinished {
		t.Fatal("the selection save finished while sync was reading the selection")
	}
	if _, err := os.Lstat(legacy); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy selection %s survived the save: %v", legacy, err)
	}
	assertOperationalContains(t, project.mustHN(t, project.consumer, "replication", "show", "origin"),
		"saved: true\n", "compatibility-all: false\n", "actor: "+project.alpha+"\n")
}

func TestWorktreeReplicationSelectionIsCloneWide(t *testing.T) {
	project := newSharedStateProject(t)
	project.mustHN(t, project.consumer, "replication", "select", "origin", "--actor", project.alpha)

	shown := project.mustHN(t, project.linked, "replication", "show", "origin")
	assertOperationalContains(t, shown, "saved: true\n", "compatibility-all: false\n", "actor: "+project.alpha+"\n")
	project.mustHN(t, project.linked, "sync", "origin")
	imported := project.importedActors(t)
	if !strings.Contains(imported, project.alpha) || strings.Contains(imported, project.beta) {
		t.Fatalf("sync from the linked worktree imported %q, want only the selected actor %s", imported, project.alpha)
	}
	assertOperationalContains(t, project.mustHN(t, project.linked, "issue", "list"), project.alphaIssue)
	if output := project.mustHN(t, project.consumer, "issue", "list"); strings.Contains(output, project.betaIssue) {
		t.Fatalf("unselected actor's issue was imported:\n%s", output)
	}
}

func TestWorktreeReplicationLegacySelectionAppliesCloneWide(t *testing.T) {
	project := newSharedStateProject(t)
	project.mustHN(t, project.linked, "replication", "select", "origin", "--actor", project.alpha)

	// An earlier release saved this selection below the linked worktree's
	// private Git directory. Move it there to reproduce that layout.
	cloneWide := replicationSelectionPathAt(project.commonDir, "origin")
	legacy := project.moveSelectionToLegacyWorktree(t)

	// The only saved selection applies to the main checkout too, so sync
	// there never silently falls back to importing every advertised actor.
	assertOperationalContains(t, project.mustHN(t, project.consumer, "replication", "show", "origin"),
		"saved: true\n", "compatibility-all: false\n", "actor: "+project.alpha+"\n")
	project.mustHN(t, project.consumer, "sync", "origin")
	if imported := project.importedActors(t); !strings.Contains(imported, project.alpha) || strings.Contains(imported, project.beta) {
		t.Fatalf("sync with a legacy linked-worktree selection imported %q, want only %s", imported, project.alpha)
	}

	// A different clone-wide selection conflicts with the legacy one, so
	// neither silently wins.
	if err := os.WriteFile(cloneWide, []byte(`{
  "version": 1,
  "remote": "origin",
  "actors": [
    "`+project.beta+`"
  ],
  "proposals": [],
  "all": false,
  "budgets": {
    "maxEvents": 10000,
    "maxObjects": 100000,
    "maxObjectBytes": 67108864,
    "maxAttachmentBytes": 16777216,
    "maxTotalBytes": 1073741824
  }
}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"replication", "show", "origin"}, {"sync", "origin"}} {
		output, code := project.hn(t, project.linked, nil, args...)
		if code == 0 || !strings.Contains(output, "saved replication selections for remote origin differ") {
			t.Fatalf("hn %s with conflicting selections: exit %d:\n%s", strings.Join(args, " "), code, output)
		}
	}
	if imported := project.importedActors(t); strings.Contains(imported, project.beta) {
		t.Fatalf("conflicting selections imported %s:\n%s", project.beta, imported)
	}

	// Saving a selection resolves the conflict and retires the legacy copy.
	project.mustHN(t, project.linked, "replication", "select", "origin", "--actor", project.alpha)
	if _, err := os.Lstat(legacy); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy selection %s survived an explicit save: %v", legacy, err)
	}
	assertOperationalContains(t, project.mustHN(t, project.consumer, "replication", "show", "origin"),
		"saved: true\n", "actor: "+project.alpha+"\n")
}

// TestWorktreeCloneStateLockNestsAndFailsClosed checks that one command can take the
// clone state lock again while it holds it, that a nested exclusive request
// under a shared hold fails rather than upgrading, and that a lock file
// with an unsafe mode or a symbolic link fails a reader closed.
func TestWorktreeCloneStateLockNestsAndFailsClosed(t *testing.T) {
	stateDir := t.TempDir()
	outer, err := lockCloneState(stateDir, serveLockExclusive)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []serveLockKind{serveLockExclusive, serveLockShared} {
		inner, err := lockCloneState(stateDir, kind)
		if err != nil {
			t.Fatalf("nested clone state lock under an exclusive hold: %v", err)
		}
		inner()
	}
	outer()

	shared, err := lockCloneState(stateDir, serveLockShared)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockCloneState(stateDir, serveLockExclusive); err == nil {
		t.Fatal("exclusive clone state lock under a shared hold succeeded")
	}
	shared()
	release, err := lockCloneState(stateDir, serveLockExclusive)
	if err != nil {
		t.Fatalf("clone state lock after every hold was released: %v", err)
	}
	release()

	path := filepath.Join(stateDir, "hn", cloneStateLockName)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadReplicationAcceptanceState(stateDir); err == nil || !strings.Contains(err.Error(), "unsafe permissions") {
		t.Fatalf("reader with a mode-0644 clone state lock: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(stateDir, "elsewhere"), path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadReplicationAcceptanceState(stateDir); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("reader with a symbolic-link clone state lock: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(stateDir, "elsewhere")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("clone state lock followed its symbolic link: %v", err)
	}
}
