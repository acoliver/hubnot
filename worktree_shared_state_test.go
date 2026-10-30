package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if err := os.Rename(cloneWide, legacy); err != nil {
		t.Fatal(err)
	}

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
