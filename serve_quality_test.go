package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// WP13: cross-cutting quality evidence on the integrated candidate. These
// tests check what no single work package can: every documented example is
// executed by some test (FR-023), the push verdict time on a 1,000-event
// repository (NFR-001), every path the server writes during a compact full
// acceptance run and the modes it leaves (NFR-004, NFR-008), and that the
// guide's traceability tables name only tests that exist (SC-001, SC-006).
// The real OpenSSH round trip is in serve_sshd_test.go.

// T079: documented-example coverage guard.

// serveQualityExampleMarker matches one documented-example marker. The
// extracting test uses the whole marker as its search key, so the guard
// looks for the exact marker text in the test sources.
var serveQualityExampleMarker = regexp.MustCompile(`<!-- example: ([A-Za-z0-9._-]+) -->`)

// serveQualityGuardFile is excluded from the test sources the guard
// searches, so the guard's own fixtures can never satisfy it.
const serveQualityGuardFile = "serve_quality_test.go"

// serveQualityExampleGaps checks docs (path to content) against tests (path
// to content). It returns each "DOC: NAME" whose exact marker appears in no
// test, each "DOC: NAME" marked more than once in one document, and the
// number of distinct markers found.
func serveQualityExampleGaps(docs, tests map[string]string) (unexecuted, duplicates []string, total int) {
	for _, doc := range slices.Sorted(maps.Keys(docs)) {
		seen := map[string]int{}
		for _, match := range serveQualityExampleMarker.FindAllStringSubmatch(docs[doc], -1) {
			seen[match[1]]++
		}
		for _, name := range slices.Sorted(maps.Keys(seen)) {
			total++
			if seen[name] > 1 {
				duplicates = append(duplicates, fmt.Sprintf("%s: %s (%d markers)", doc, name, seen[name]))
			}
			marker := "<!-- example: " + name + " -->"
			executed := false
			for _, source := range tests {
				if strings.Contains(source, marker) {
					executed = true
					break
				}
			}
			if !executed {
				unexecuted = append(unexecuted, doc+": "+name)
			}
		}
	}
	return unexecuted, duplicates, total
}

// serveQualityReadFiles reads every file matching pattern, keyed by path,
// except the excluded base names.
func serveQualityReadFiles(t *testing.T, pattern string, exclude ...string) map[string]string {
	t.Helper()
	paths, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, path := range paths {
		if slices.Contains(exclude, filepath.Base(path)) {
			continue
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		files[path] = string(contents)
	}
	if len(files) == 0 {
		t.Fatalf("no files match %s", pattern)
	}
	return files
}

// FR-023: every <!-- example: NAME --> marker in docs/*.md is executed by
// a root *_test.go file that contains the exact marker, and no document
// marks one name twice. The guard is decentralized: a later work package
// adds its example and its own extracting test without editing this file.
func TestServeQualityDocumentedExamplesAreExecuted(t *testing.T) {
	docs := serveQualityReadFiles(t, filepath.Join("docs", "*.md"))
	tests := serveQualityReadFiles(t, "*_test.go", serveQualityGuardFile)
	unexecuted, duplicates, total := serveQualityExampleGaps(docs, tests)
	if total == 0 {
		t.Fatal("docs/*.md contain no example markers; the guard would check nothing")
	}
	if len(duplicates) > 0 {
		t.Errorf("documents mark an example name more than once:\n%s", strings.Join(duplicates, "\n"))
	}
	if len(unexecuted) > 0 {
		t.Errorf("documented examples that no test executes (no *_test.go contains the exact marker):\n%s", strings.Join(unexecuted, "\n"))
	}
	t.Logf("%d documented examples across %d documents", total, len(docs))
}

// The guard itself fails on a missing extractor and on a duplicate marker,
// and a marker that only appears in another document does not count.
func TestServeQualityExampleGuardDetectsGaps(t *testing.T) {
	docs := map[string]string{
		"docs/a.md": "<!-- example: alpha -->\ntext\n<!-- example: beta -->\n<!-- example: beta -->\n",
		"docs/b.md": "<!-- example: gamma -->\n",
	}
	tests := map[string]string{
		"a_test.go": `const marker = "<!-- example: alpha -->"`,
		"b_test.go": `const marker = "<!-- example: beta -->"`,
		"c_test.go": `// example: gamma`,
	}
	unexecuted, duplicates, total := serveQualityExampleGaps(docs, tests)
	if total != 3 {
		t.Fatalf("total = %d, want 3", total)
	}
	if want := []string{"docs/b.md: gamma"}; !slices.Equal(unexecuted, want) {
		t.Fatalf("unexecuted = %q, want %q", unexecuted, want)
	}
	if want := []string{"docs/a.md: beta (2 markers)"}; !slices.Equal(duplicates, want) {
		t.Fatalf("duplicates = %q, want %q", duplicates, want)
	}
}

// T080: NFR-001 push verdict time.

const (
	serveQualityHostedEvents = 1000
	serveQualityPushEvents   = 50
	serveQualityTimingRuns   = 20
	serveQualityVerdictLimit = 5 * time.Second
)

// serveQualityHostedEventCount is the number of events in every hosted
// actor chain; each event is one commit of its actor chain.
func serveQualityHostedEventCount(t *testing.T, hosted string) int {
	t.Helper()
	output := serveHarnessGit(t, hosted, "rev-list", "--count", "--glob="+serveActorRefPrefix+"*")
	count, err := strconv.Atoi(output)
	if err != nil {
		t.Fatalf("rev-list --count printed %q", output)
	}
	return count
}

// serveQualityCopyTree copies the directory source to destination, which
// must not exist, keeping every permission bit. It refuses anything other
// than directories and regular files.
func serveQualityCopyTree(t *testing.T, source, destination string) {
	t.Helper()
	type directoryMode struct {
		path string
		mode fs.FileMode
	}
	var directories []directoryMode
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case info.IsDir():
			directories = append(directories, directoryMode{target, info.Mode().Perm()})
			return os.Mkdir(target, 0o700)
		case info.Mode().IsRegular():
			return serveQualityCopyFile(path, target, info.Mode().Perm())
		default:
			return fmt.Errorf("%s: unexpected file type %s", path, info.Mode().Type())
		}
	})
	if err != nil {
		t.Fatalf("copy %s to %s: %v", source, destination, err)
	}
	for index := len(directories) - 1; index >= 0; index-- {
		if err := os.Chmod(directories[index].path, directories[index].mode); err != nil {
			t.Fatal(err)
		}
	}
}

func serveQualityCopyFile(source, destination string, mode fs.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	return os.Chmod(destination, mode)
}

// serveQualityRestoreTree replaces destination with a copy of template.
func serveQualityRestoreTree(t *testing.T, template, destination string) {
	t.Helper()
	if err := os.RemoveAll(destination); err != nil {
		t.Fatal(err)
	}
	serveQualityCopyTree(t, template, destination)
}

// serveQualityPercentile95 is the 95th percentile of 20 samples: the 19th
// smallest value.
func serveQualityPercentile95(samples []time.Duration) time.Duration {
	sorted := slices.Clone(samples)
	slices.Sort(sorted)
	return sorted[len(sorted)*95/100-1]
}

// NFR-001: on a hosted repository holding 1,000 Hubnot events from four
// actors, including one ready, accepted, CI-tested proposal whose head
// descends from main, a push of 50 new actor events together with the
// protected main update reaches its verdict in under 5 seconds at the 95th
// percentile over 20 runs. Every run restores the hosted repository, its
// server state and the pusher's clone from one template, so each sample
// starts from the same 1,000 events and the same main tip; restoration is
// not timed.
//
// The hn binary that runs the server side is built by buildOperationalBinary
// with plain go build, without -race, even when this test runs under go
// test -race, so the race detector does not distort the measurement. Only
// the in-process fixture appends run in the race-instrumented test binary,
// and they are outside the timed section.
func TestServeQualityPushVerdictTimeNFR001(t *testing.T) {
	if testing.Short() {
		t.Skip("NFR-001 timing builds a 1,000-event repository and pushes 20 times; it runs in the full gate without -short")
	}
	g := newServeGovernedFixture(t, serveGovernedOptions{})
	head := g.Commit(g.Base, nil)
	g.ReadyProposal(g.Base, head)

	// Pad the hosted chains to exactly 1,000 events across all four actors
	// with signed issue.open events appended in-process, then publish each
	// chain with hn sync so the server validates it on arrival.
	actors := []string{"A", "R", "U", "M"}
	remaining := serveQualityHostedEvents - serveQualityHostedEventCount(t, g.hosted)
	if remaining < len(actors) {
		t.Fatalf("the governed proposal produced %d events; no room to pad", serveQualityHostedEvents-remaining)
	}
	for index, who := range actors {
		count := remaining / len(actors)
		if index == 0 {
			count += remaining % len(actors)
		}
		chain := newServeTestChain(t, g.servePushFixture, who)
		for event := range count {
			chain.issue(fmt.Sprintf("NFR-001 fixture issue %s-%d", who, event))
		}
		g.hn(who, "sync", "origin")
		if !g.Published(who) {
			t.Fatalf("hn sync did not publish %s's padded chain", who)
		}
	}
	if count := serveQualityHostedEventCount(t, g.hosted); count != serveQualityHostedEvents {
		t.Fatalf("fixture holds %d hosted events, want %d", count, serveQualityHostedEvents)
	}

	// Snapshot the fixture once.
	pusher := "A"
	ref := g.actorRef(pusher)
	stateDir := g.h.Layout().RepoStateDir("r")
	template := t.TempDir()
	templates := map[string]string{
		g.hosted:       filepath.Join(template, "hosted"),
		stateDir:       filepath.Join(template, "state"),
		g.work[pusher]: filepath.Join(template, "clone"),
	}
	for live, saved := range templates {
		serveQualityCopyTree(t, live, saved)
	}
	baseTip := g.ServerTip("main")
	if baseTip != g.Base {
		t.Fatalf("hosted main moved to %s during fixture setup", baseTip)
	}

	durations := make([]time.Duration, 0, serveQualityTimingRuns)
	for run := range serveQualityTimingRuns {
		for live, saved := range templates {
			serveQualityRestoreTree(t, saved, live)
		}
		// The clone directory was replaced; enter the new one before
		// newServeTestChain records the working directory.
		if err := os.Chdir(g.work[pusher]); err != nil {
			t.Fatal(err)
		}
		if count := serveQualityHostedEventCount(t, g.hosted); count != serveQualityHostedEvents {
			t.Fatalf("run %d starts with %d hosted events, want %d", run, count, serveQualityHostedEvents)
		}
		if tip := g.ServerTip("main"); tip != baseTip {
			t.Fatalf("run %d starts with hosted main %s, want %s", run, tip, baseTip)
		}
		chain := newServeTestChain(t, g.servePushFixture, pusher)
		for event := range serveQualityPushEvents {
			chain.issue(fmt.Sprintf("NFR-001 run %d event %d", run, event))
		}
		local := g.localHead(pusher, ref)

		started := time.Now()
		output, status := g.clients[pusher].exec(g.work[pusher], "git", "push", "--atomic", "origin", ref, "+"+head+":"+serveGovernedMain)
		elapsed := time.Since(started)

		if status != 0 {
			t.Fatalf("run %d: timed push exited %d:\n%s", run, status, output)
		}
		if tip := g.ServerTip("main"); tip != head {
			t.Fatalf("run %d: hosted main = %s after an accepted push, want %s", run, tip, head)
		}
		if hosted := g.hostedRef(ref); hosted != local {
			t.Fatalf("run %d: hosted %s = %s, want %s", run, ref, hosted, local)
		}
		if count := serveQualityHostedEventCount(t, g.hosted); count != serveQualityHostedEvents+serveQualityPushEvents {
			t.Fatalf("run %d: %d hosted events after the push, want %d", run, count, serveQualityHostedEvents+serveQualityPushEvents)
		}
		durations = append(durations, elapsed)
	}
	labels := make([]string, len(durations))
	for index, duration := range durations {
		labels[index] = duration.Round(time.Millisecond).String()
	}
	p95 := serveQualityPercentile95(durations)
	t.Logf("NFR-001 push verdict durations over %d runs: %s", len(durations), strings.Join(labels, " "))
	t.Logf("NFR-001 95th percentile: %s (limit %s)", p95.Round(time.Millisecond), serveQualityVerdictLimit)
	if p95 >= serveQualityVerdictLimit {
		t.Fatalf("NFR-001: 95th percentile push verdict time %s is not below %s", p95, serveQualityVerdictLimit)
	}
}

func TestServeQualityPercentile95IsNineteenthOfTwenty(t *testing.T) {
	samples := make([]time.Duration, 20)
	for index := range samples {
		samples[index] = time.Duration(20-index) * time.Second
	}
	if got := serveQualityPercentile95(samples); got != 19*time.Second {
		t.Fatalf("p95 = %s, want 19s", got)
	}
}

// T081: NFR-004 written-path audit and NFR-008 mode audit.

// serveQualityEntry is what the audit records for one path.
type serveQualityEntry struct {
	Type    fs.FileMode
	Mode    fs.FileMode
	Size    int64
	ModTime time.Time
}

// serveQualitySnapshot records every path below root, including root as
// ".", by relative path.
func serveQualitySnapshot(t *testing.T, root string) map[string]serveQualityEntry {
	t.Helper()
	snapshot := map[string]serveQualityEntry{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		snapshot[filepath.ToSlash(relative)] = serveQualityEntry{
			Type:    info.Mode().Type(),
			Mode:    info.Mode().Perm() | info.Mode()&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		}
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return snapshot
}

// serveQualityChangedPaths lists every path created, removed or changed
// between two snapshots. A file changes when its type, mode, size or
// modification time changes. A directory changes when its type or mode
// changes; with directoryTimes its modification time counts too, which
// also reveals a transient entry created and removed in between. Without
// it, entries added below a directory appear as created paths themselves.
func serveQualityChangedPaths(before, after map[string]serveQualityEntry, directoryTimes bool) []string {
	var changed []string
	for path, was := range before {
		now, ok := after[path]
		switch {
		case !ok:
			changed = append(changed, path+" (removed)")
		case now.Type != was.Type || now.Mode != was.Mode:
			changed = append(changed, path+" (type or mode changed)")
		case now.Type.IsDir() && !directoryTimes:
		case now.Size != was.Size || !now.ModTime.Equal(was.ModTime):
			changed = append(changed, path+" (modified)")
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			changed = append(changed, path+" (created)")
		}
	}
	sort.Strings(changed)
	return changed
}

// serveQualityAllowedWrite reports whether NFR-004 lets the server write
// path, given relative to its HOME: $HOME/.hn-serve and everything below
// it, $HOME/.ssh and $HOME/.ssh/authorized_keys.
func serveQualityAllowedWrite(path string) bool {
	return path == serveHomeDirectoryName || strings.HasPrefix(path, serveHomeDirectoryName+"/") ||
		path == ".ssh" || path == ".ssh/authorized_keys"
}

// serveQualityForbiddenWrites lists the changed paths below HOME that
// NFR-004 does not allow.
func serveQualityForbiddenWrites(before, after map[string]serveQualityEntry) []string {
	var forbidden []string
	for _, change := range serveQualityChangedPaths(before, after, false) {
		path, _, _ := strings.Cut(change, " (")
		if path != "." && !serveQualityAllowedWrite(path) {
			forbidden = append(forbidden, change)
		}
	}
	return forbidden
}

// serveQualityModeProblems is the NFR-008 mode audit of a server HOME:
// every directory below .hn-serve outside repositories/*.git is 0700,
// every file below state is 0600, every hook script is 0700, .ssh is 0700
// and authorized_keys is 0600. It also requires the audited paths to
// exist, so an empty layout cannot pass.
func serveQualityModeProblems(t *testing.T, home string) []string {
	t.Helper()
	var problems []string
	root := filepath.Join(home, serveHomeDirectoryName)
	snapshot := serveQualitySnapshot(t, root)
	for _, required := range []string{".", "hooks", "hooks/pre-receive", "hooks/post-receive", "repositories", "state", "state/registry.json", "state/tmp", "state/repos"} {
		if _, ok := snapshot[required]; !ok {
			problems = append(problems, filepath.Join(serveHomeDirectoryName, required)+" is missing")
		}
	}
	for _, path := range slices.Sorted(maps.Keys(snapshot)) {
		entry := snapshot[path]
		inRepository := path != "repositories" && strings.HasPrefix(path, "repositories/")
		want := fs.FileMode(0)
		switch {
		case entry.Type.IsDir() && !inRepository:
			want = 0o700
		case entry.Type.IsRegular() && strings.HasPrefix(path, "state/"):
			want = 0o600
		case entry.Type.IsRegular() && strings.HasPrefix(path, "hooks/"):
			want = 0o700
		case entry.Type.IsRegular() && !inRepository:
			problems = append(problems, fmt.Sprintf("%s/%s is a file outside hooks, state and repositories", serveHomeDirectoryName, path))
			continue
		case !entry.Type.IsDir() && !entry.Type.IsRegular():
			problems = append(problems, fmt.Sprintf("%s/%s has type %s", serveHomeDirectoryName, path, entry.Type))
			continue
		default:
			continue
		}
		if entry.Mode != want {
			problems = append(problems, fmt.Sprintf("%s/%s has mode %04o, want %04o", serveHomeDirectoryName, path, entry.Mode, want))
		}
	}
	for _, check := range []struct {
		path string
		kind fs.FileMode
		mode fs.FileMode
	}{{".ssh", fs.ModeDir, 0o700}, {".ssh/authorized_keys", 0, 0o600}} {
		info, err := os.Lstat(filepath.Join(home, check.path))
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", check.path, err))
			continue
		}
		if info.Mode().Type() != check.kind || info.Mode().Perm() != check.mode {
			problems = append(problems, fmt.Sprintf("%s has mode %s, want %s", check.path, info.Mode(), check.kind|check.mode))
		}
	}
	sort.Strings(problems)
	return problems
}

// newServeQualityAcceptanceFixture prepares the compact full acceptance
// run on h, whose Wrap and Env must already be set: two participants with
// real hn identities, contributor A and maintainer M, where M holds every
// policy role. Nothing server-side runs yet.
func newServeQualityAcceptanceFixture(t *testing.T, h *serveHarness) *serveGovernedFixture {
	t.Helper()
	f := &servePushFixture{
		t:       t,
		h:       h,
		hosted:  h.Layout().RepoGitDir("r"),
		actors:  map[string]string{},
		keys:    map[string]serveTestKey{},
		clients: map[string]serveHarnessClientEnv{},
		work:    map[string]string{},
	}
	for _, name := range []string{"A", "M"} {
		work := filepath.Join(t.TempDir(), "work-"+name)
		runOperationalGit(t, "", "init", "-q", "-b", "main", work)
		runOperationalCommand(t, h.Binary, work, "init", "--name", "Quality "+name)
		f.actors[name] = readOperationalIdentity(t, h.Binary, work).Actor
		f.work[name] = work
	}
	g := &serveGovernedFixture{
		servePushFixture: f,
		Author:           "M", Reviewer: "M", Runner: "M", Maintainer: "M",
		CIFailFlag: filepath.Join(t.TempDir(), "ci-fails"),
	}
	g.Policy = serveGovernedPolicy{
		Maintainers:         []string{f.actors["M"]},
		Reviewers:           []string{f.actors["M"]},
		RequiredApprovals:   1,
		AllowAuthorApproval: true,
		Pipelines:           map[string][]string{"test": {f.actors["M"]}},
	}
	return g
}

// serveQualityAcceptanceRun is the compact full acceptance run of the
// NFR-004 audit. Every server-side process goes through h, so a Wrap and
// Env set on h cover administration commands, sessions, hooks and the Git
// programs they start: repo create, two key add, repo set, a feature
// branch push, hn sync of facts, a governed proposal advanced with hn
// merge --remote, a rejected direct main push, lock-exec and key remove.
// WP14 extends the audit with LFS paths in its own test.
func serveQualityAcceptanceRun(t *testing.T, g *serveGovernedFixture) {
	t.Helper()
	h := g.h
	source := filepath.Join(t.TempDir(), "source")
	initOperationalRepository(t, source, "Source")
	serveHarnessWriteFile(t, source, serveGovernedPolicyPath, g.Policy.JSON(t))
	serveHarnessWriteFile(t, source, filepath.Join(".hn", "pipelines", "test.json"), serveGovernedPipelineJSON(t, g.CIFailFlag))
	serveHarnessCommit(t, source, "governed base")
	h.Serve("serve", "repo", "create", "r", "--from", source)
	for _, name := range []string{"M", "A"} {
		g.keys[name] = serveHarnessAddKey(h, g.actors[name])
		g.clients[name] = serveHarnessClient(t, h, g.keys[name], nil)
		g.git(name, "remote", "add", "origin", serveHarnessCloneURL("r"))
		g.git(name, "fetch", "-q", "origin")
		g.git(name, "checkout", "-q", "-B", "main", "origin/main")
		g.hn(name, "replication", "select", "origin", "--all")
	}
	g.Base = g.ServerTip("main")
	h.Serve("serve", "repo", "set", "r", "--lfs-quota", "2147483648")

	// A feature branch push and hn sync of facts from a contributor.
	feature := g.commitOn("A", "origin/main")
	g.push("A", "origin", feature+":refs/heads/feature/audit")
	if tip := g.ServerTip("feature/audit"); tip != feature {
		t.Fatalf("hosted feature/audit = %q, want %s", tip, feature)
	}
	g.hn("A", "issue", "open", "--body", "Opened during the NFR-004 audit run.", "Audit the server")
	g.hn("A", "sync", "origin")
	if !g.Published("A") {
		t.Fatal("hn sync did not publish A's actor chain")
	}

	// A governed proposal advanced by the maintainer.
	head := g.Commit(g.Base, nil)
	p := g.ReadyProposal(g.Base, head)
	if output, status := g.Advance("M", p.Candidate.ID); status != 0 {
		t.Fatalf("hn merge --remote exited %d:\n%s", status, output)
	}
	if tip := g.ServerTip("main"); tip != head {
		t.Fatalf("hosted main = %s after the advancement, want %s", tip, head)
	}

	// A rejected direct main push.
	direct := g.commitOn("A", head)
	g.RejectMain("A", direct, serveReasonProtectedNoProposal)

	// Administrator recovery access and a key removal.
	h.Serve("serve", "repo", "lock-exec", "r", "--", "git", "count-objects")
	h.Serve("serve", "key", "remove", g.keys["A"].Fingerprint)
	if output := g.clients["A"].GitFailure(g.work["A"], "fetch", "-q", "origin"); !strings.Contains(output, "Permission denied (publickey).") {
		t.Fatalf("fetch with a removed key was not refused by the shim:\n%s", output)
	}
}

// NFR-004 and NFR-008, snapshot evidence: the acceptance run creates or
// changes nothing in the server HOME outside $HOME/.hn-serve, $HOME/.ssh
// and $HOME/.ssh/authorized_keys; nothing at all in a sentinel TMPDIR
// outside HOME that every server-side process receives (the session
// wrapper points Git's TMPDIR at state/tmp); and leaves the modes NFR-008
// requires. TestServeQualityEnforcedWriteBoundary is the enforced form.
func TestServeQualityWrittenPathsAndModes(t *testing.T) {
	h := newServeHarness(t)
	sentinel := t.TempDir()
	h.Env = map[string]string{"TMPDIR": sentinel}
	g := newServeQualityAcceptanceFixture(t, h)

	before := serveQualitySnapshot(t, h.Home)
	sentinelBefore := serveQualitySnapshot(t, sentinel)
	serveQualityAcceptanceRun(t, g)
	after := serveQualitySnapshot(t, h.Home)

	if forbidden := serveQualityForbiddenWrites(before, after); len(forbidden) > 0 {
		t.Errorf("NFR-004: the server wrote outside $HOME/.hn-serve and $HOME/.ssh/authorized_keys:\n%s", strings.Join(forbidden, "\n"))
	}
	if changed := serveQualityChangedPaths(sentinelBefore, serveQualitySnapshot(t, sentinel), true); len(changed) > 0 {
		t.Errorf("NFR-004: server-side processes wrote to the sentinel TMPDIR %s:\n%s", sentinel, strings.Join(changed, "\n"))
	}
	if problems := serveQualityModeProblems(t, h.Home); len(problems) > 0 {
		t.Errorf("NFR-008: unexpected modes after the acceptance run:\n%s", strings.Join(problems, "\n"))
	}
	written := 0
	for _, change := range serveQualityChangedPaths(before, after, false) {
		if strings.HasSuffix(change, "(created)") || strings.HasSuffix(change, "(modified)") {
			written++
		}
	}
	if written == 0 {
		t.Fatal("the audit saw no written path; the snapshot is not observing the server HOME")
	}
	t.Logf("NFR-004 snapshot audit: %d paths created or modified, all below the allowed locations", written)
}

// The snapshot audit would catch a stray write: a file in $HOME/.cache, a
// modified file directly in HOME and a removed file are reported, writes
// below the allowed locations are not, and a transient file in a sentinel
// directory shows through the directory's modification time.
func TestServeQualityPathAuditDetectsStrayWrites(t *testing.T) {
	home := t.TempDir()
	for _, directory := range []string{".hn-serve/state", ".ssh", ".config"} {
		if err := os.MkdirAll(filepath.Join(home, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-time.Hour)
	for _, file := range []string{".profile", ".config/kept", ".config/dropped"} {
		writeServeTestFile(t, filepath.Join(home, file), "before\n", 0o600)
		if err := os.Chtimes(filepath.Join(home, file), old, old); err != nil {
			t.Fatal(err)
		}
	}
	before := serveQualitySnapshot(t, home)

	writeServeTestFile(t, filepath.Join(home, ".hn-serve", "state", "registry.json"), "{}\n", 0o600)
	writeServeTestFile(t, filepath.Join(home, ".ssh", "authorized_keys"), "\n", 0o600)
	if err := os.MkdirAll(filepath.Join(home, ".cache"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeServeTestFile(t, filepath.Join(home, ".cache", "stray"), "stray\n", 0o600)
	writeServeTestFile(t, filepath.Join(home, ".profile"), "after\n", 0o600)
	if err := os.Remove(filepath.Join(home, ".config", "dropped")); err != nil {
		t.Fatal(err)
	}
	writeServeTestFile(t, filepath.Join(home, ".ssh", "known_hosts"), "\n", 0o600)

	got := serveQualityForbiddenWrites(before, serveQualitySnapshot(t, home))
	want := []string{
		".cache (created)",
		".cache/stray (created)",
		".config/dropped (removed)",
		".profile (modified)",
		".ssh/known_hosts (created)",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("forbidden writes = %q, want %q", got, want)
	}

	sentinel := t.TempDir()
	if err := os.Chtimes(sentinel, old, old); err != nil {
		t.Fatal(err)
	}
	sentinelBefore := serveQualitySnapshot(t, sentinel)
	transient := filepath.Join(sentinel, "tmp-123")
	writeServeTestFile(t, transient, "x", 0o600)
	if err := os.Remove(transient); err != nil {
		t.Fatal(err)
	}
	if changed := serveQualityChangedPaths(sentinelBefore, serveQualitySnapshot(t, sentinel), true); !slices.Equal(changed, []string{". (modified)"}) {
		t.Fatalf("transient sentinel write reported as %q", changed)
	}
}

// serveQualityProbe runs a shell write to target under wrap and returns
// whether it succeeded, removing anything it managed to create.
func serveQualityProbe(t *testing.T, h *serveHarness, wrap []string, target string) (bool, string) {
	t.Helper()
	args := append(append([]string{}, wrap[1:]...), "/bin/sh", "-c", `printf probe > "$1"`, "probe", target)
	command := exec.Command(wrap[0], args...)
	command.Env = serveHarnessEnvironment(h.Home, nil)
	output, err := command.CombinedOutput()
	if _, statErr := os.Lstat(target); statErr == nil {
		if removeErr := os.Remove(target); removeErr != nil {
			t.Errorf("remove probe %s: %v", target, removeErr)
		}
		return true, string(output)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("stat probe %s: %v", target, statErr)
	}
	return err == nil, string(output)
}

// NFR-004, enforced evidence: the whole acceptance run succeeds with every
// server-side process, administration commands included, inside a
// bubblewrap sandbox that can write only $HOME/.hn-serve and $HOME/.ssh,
// so any write elsewhere, even a transient temporary file, would fail the
// run. The boundary is validated first: a write probe succeeds in
// $HOME/.ssh and fails in $HOME, in /tmp and at an absolute path outside
// HOME. Without bubblewrap or unprivileged user namespaces the test skips
// with the reason, and TestServeQualityWrittenPathsAndModes remains as
// supplementary snapshot evidence.
func TestServeQualityEnforcedWriteBoundary(t *testing.T) {
	h := newServeHarness(t)
	wrap := h.EnforceWriteBoundary()
	sentinel := t.TempDir()
	h.Env = map[string]string{"TMPDIR": sentinel}

	if ok, output := serveQualityProbe(t, h, wrap, filepath.Join(h.Home, ".ssh", "boundary-probe")); !ok {
		t.Fatalf("the boundary refuses a write below $HOME/.ssh, so the probes below prove nothing:\n%s", output)
	}
	outside := filepath.Join(t.TempDir(), "outside-home")
	for _, target := range []string{
		filepath.Join(h.Home, "outside"),
		filepath.Join("/tmp", fmt.Sprintf("hn-serve-boundary-probe-%d-%d", os.Getpid(), time.Now().UnixNano())),
		outside,
	} {
		if ok, output := serveQualityProbe(t, h, wrap, target); ok {
			t.Fatalf("the boundary allowed a write to %s:\n%s", target, output)
		}
	}

	g := newServeQualityAcceptanceFixture(t, h)
	sentinelBefore := serveQualitySnapshot(t, sentinel)
	serveQualityAcceptanceRun(t, g)
	if changed := serveQualityChangedPaths(sentinelBefore, serveQualitySnapshot(t, sentinel), true); len(changed) > 0 {
		t.Errorf("the sentinel TMPDIR changed:\n%s", strings.Join(changed, "\n"))
	}
	if problems := serveQualityModeProblems(t, h.Home); len(problems) > 0 {
		t.Errorf("NFR-008: unexpected modes after the acceptance run:\n%s", strings.Join(problems, "\n"))
	}
	t.Logf("NFR-004: acceptance run passed inside %s", strings.Join(wrap, " "))
}

// T083: requirement traceability.

const (
	serveQualityTraceabilityHeading = "## Verification and traceability"
	serveQualityPendingLFS          = "pending WP14"
	serveQualityMovedLFS            = "moved to its own mission"
	serveQualityLFSTestFile         = "serve_lfs_test.go"
)

// serveQualityTraceRow is one data row of a traceability table.
type serveQualityTraceRow struct {
	Line  int
	ID    string
	Tests []string
	Text  string
}

var (
	serveQualityTestName    = regexp.MustCompile("`(Test[A-Za-z0-9_]+)`")
	serveQualityDeclaration = regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(`)
)

// serveQualityTraceRows parses every table of the guide's traceability
// section. The first row of a table is its header and the second its
// separator; the first cell of each data row is its ID.
func serveQualityTraceRows(guide string) ([]serveQualityTraceRow, error) {
	start := strings.Index(guide, "\n"+serveQualityTraceabilityHeading+"\n")
	if start < 0 {
		return nil, fmt.Errorf("the guide has no %q section", serveQualityTraceabilityHeading)
	}
	firstLine := strings.Count(guide[:start+1], "\n") + 1
	section := guide[start+1:]
	if end := strings.Index(section[1:], "\n## "); end >= 0 {
		section = section[:end+2]
	}
	var rows []serveQualityTraceRow
	tableLine := 0
	for index, line := range strings.Split(section, "\n") {
		if !strings.HasPrefix(line, "|") {
			tableLine = 0
			continue
		}
		tableLine++
		if tableLine <= 2 {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		row := serveQualityTraceRow{Line: firstLine + index, ID: strings.TrimSpace(cells[0]), Text: line}
		for _, match := range serveQualityTestName.FindAllStringSubmatch(line, -1) {
			row.Tests = append(row.Tests, match[1])
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// serveQualityTraceIDs are the rows the traceability tables must have: one
// per functional and non-functional requirement, success criterion and
// user-story scenario, and one per acceptance example of the brief.
func serveQualityTraceIDs() []string {
	var ids []string
	for number := 1; number <= 23; number++ {
		ids = append(ids, fmt.Sprintf("FR-%03d", number))
	}
	for number := 1; number <= 8; number++ {
		ids = append(ids, fmt.Sprintf("NFR-%03d", number))
	}
	for number := 1; number <= 6; number++ {
		ids = append(ids, fmt.Sprintf("SC-%03d", number))
	}
	for story, scenarios := range []int{6, 5, 6, 6, 4} {
		for scenario := 1; scenario <= scenarios; scenario++ {
			ids = append(ids, fmt.Sprintf("US%d-%d", story+1, scenario))
		}
	}
	for number := 1; number <= 6; number++ {
		ids = append(ids, fmt.Sprintf("Brief %d", number))
	}
	return ids
}

// serveQualityLFSRow reports whether id is one of the LFS rows WP14 owns.
func serveQualityLFSRow(id string) bool {
	return id == "FR-020" || id == "FR-021" || strings.HasPrefix(id, "US5-")
}

// serveQualityTraceabilityProblems checks rows against the declared test
// functions. Every expected ID has exactly one row, every row names at
// least one test, and every named test is declared. While WP14 has not
// landed (lfsLanded false), an LFS row may instead carry pending WP14 or
// moved to its own mission, and its planned tests are not checked. Those
// markers are refused on every non-LFS row, and on every row once
// serve_lfs_test.go exists.
func serveQualityTraceabilityProblems(rows []serveQualityTraceRow, declared map[string]bool, lfsLanded bool) []string {
	var problems []string
	seen := map[string]int{}
	for _, row := range rows {
		seen[row.ID]++
		marked := strings.Contains(row.Text, serveQualityPendingLFS) || strings.Contains(row.Text, serveQualityMovedLFS)
		where := fmt.Sprintf("line %d (%s)", row.Line, row.ID)
		if len(row.Tests) == 0 {
			problems = append(problems, where+": names no test")
		}
		switch {
		case marked && lfsLanded:
			problems = append(problems, where+": carries an LFS marker although "+serveQualityLFSTestFile+" exists")
		case marked && !serveQualityLFSRow(row.ID):
			problems = append(problems, where+": carries an LFS marker but is not an LFS row")
		case marked:
			continue
		}
		for _, name := range row.Tests {
			if !declared[name] {
				problems = append(problems, where+": "+name+" is not declared in any root *_test.go file")
			}
		}
	}
	for _, id := range serveQualityTraceIDs() {
		if seen[id] != 1 {
			problems = append(problems, fmt.Sprintf("%s has %d rows, want 1", id, seen[id]))
		}
	}
	for id := range seen {
		if !slices.Contains(serveQualityTraceIDs(), id) {
			problems = append(problems, fmt.Sprintf("unexpected row ID %q", id))
		}
	}
	sort.Strings(problems)
	return problems
}

// serveQualityDeclaredTests is every Test function declared in a root
// *_test.go file.
func serveQualityDeclaredTests(t *testing.T) map[string]bool {
	t.Helper()
	declared := map[string]bool{}
	for _, source := range serveQualityReadFiles(t, "*_test.go") {
		for _, match := range serveQualityDeclaration.FindAllStringSubmatch(source, -1) {
			declared[match[1]] = true
		}
	}
	return declared
}

// SC-001 and SC-006: the guide's traceability tables name, for every
// requirement, success criterion, user-story scenario and brief acceptance
// example, test functions that exist. LFS rows may say pending WP14 until
// serve_lfs_test.go exists; afterwards the check is strict for every row.
func TestServeQualityTraceabilityTestsExist(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("docs", "serve-v0.md"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := serveQualityTraceRows(string(contents))
	if err != nil {
		t.Fatal(err)
	}
	_, statErr := os.Stat(serveQualityLFSTestFile)
	lfsLanded := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal(statErr)
	}
	if problems := serveQualityTraceabilityProblems(rows, serveQualityDeclaredTests(t), lfsLanded); len(problems) > 0 {
		t.Fatalf("traceability problems in docs/serve-v0.md:\n%s", strings.Join(problems, "\n"))
	}
	t.Logf("%d traceability rows checked; %s present: %t", len(rows), serveQualityLFSTestFile, lfsLanded)
}

// The traceability check refuses missing tests, misplaced markers, rows
// without tests and missing rows.
func TestServeQualityTraceabilityCheckRejectsGaps(t *testing.T) {
	guide := "# Guide\n\n" + serveQualityTraceabilityHeading + "\n\n| Requirement | Tests |\n|----|----|\n" +
		"| FR-001 | `TestKnown` |\n" +
		"| FR-002 | `TestMissing` |\n" +
		"| FR-003 | pending WP14 `TestPlanned` |\n" +
		"| FR-020 | `TestServeLFSPlanned` (pending WP14) |\n" +
		"| FR-021 | none yet |\n" +
		"\n## Next section\n\n| FR-004 | `TestIgnored` |\n"
	rows, err := serveQualityTraceRows(guide)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 || rows[0].ID != "FR-001" || rows[0].Line != 7 {
		t.Fatalf("parsed rows %+v", rows)
	}
	declared := map[string]bool{"TestKnown": true}
	problems := strings.Join(serveQualityTraceabilityProblems(rows, declared, false), "\n")
	for _, want := range []string{
		"line 8 (FR-002): TestMissing is not declared",
		"line 9 (FR-003): carries an LFS marker but is not an LFS row",
		"line 11 (FR-021): names no test",
		"FR-005 has 0 rows, want 1",
	} {
		if !strings.Contains(problems, want) {
			t.Errorf("problems do not include %q:\n%s", want, problems)
		}
	}
	if strings.Contains(problems, "FR-020") {
		t.Errorf("a pending LFS row was refused before WP14 landed:\n%s", problems)
	}
	landed := strings.Join(serveQualityTraceabilityProblems(rows, declared, true), "\n")
	if !strings.Contains(landed, "line 10 (FR-020): carries an LFS marker although serve_lfs_test.go exists") {
		t.Errorf("a pending LFS row was accepted after WP14 landed:\n%s", landed)
	}
}
