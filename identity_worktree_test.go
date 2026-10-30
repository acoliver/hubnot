package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// worktreeClone is a real temporary clone with linked worktrees. Its paths
// are physical, so they compare equal to the paths Git reports.
type worktreeClone struct {
	root      string
	main      string
	commonDir string
	worktrees []string
	gitDirs   []string
}

func newWorktreeClone(t *testing.T, linked int) worktreeClone {
	t.Helper()
	restoreWorkingDirectoryAfter(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	clone := worktreeClone{root: root, main: filepath.Join(root, "main")}
	worktreeGit(t, root, "init", "-q", "-b", "main", clone.main)
	worktreeGit(t, clone.main, "config", "user.name", "Worktree Test")
	worktreeGit(t, clone.main, "config", "user.email", "worktree-test@hn.invalid")
	worktreeGit(t, clone.main, "commit", "--allow-empty", "-q", "-m", "base")
	clone.commonDir = filepath.Join(clone.main, ".git")
	for index := 0; index < linked; index++ {
		path := filepath.Join(root, fmt.Sprintf("wt%d", index))
		worktreeGit(t, clone.main, "worktree", "add", "-q", "-b", fmt.Sprintf("wt%d", index), path)
		clone.worktrees = append(clone.worktrees, path)
		clone.gitDirs = append(clone.gitDirs, worktreeGit(t, path, "rev-parse", "--absolute-git-dir"))
	}
	return clone
}

func restoreWorkingDirectoryAfter(t *testing.T) {
	t.Helper()
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(original); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
}

func worktreeGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

// runHNIn runs one hn command line in dir through the CLI entry point.
func runHNIn(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	return captureTestOutput(t, func() error { return run(args) })
}

func mustHNIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	output, err := runHNIn(t, dir, args...)
	if err != nil {
		t.Fatalf("hn %s in %s: %v\n%s", strings.Join(args, " "), dir, err, output)
	}
	return output
}

type worktreeShownIdentity struct {
	Actor     string `json:"actor"`
	Name      string `json:"name"`
	PublicKey string `json:"public_key"`
}

func shownIdentityIn(t *testing.T, dir string) worktreeShownIdentity {
	t.Helper()
	var envelope struct {
		OK   bool                  `json:"ok"`
		Data worktreeShownIdentity `json:"data"`
	}
	if err := json.Unmarshal([]byte(mustHNIn(t, dir, "identity", "show", "--json")), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || !validActorFingerprint(envelope.Data.Actor) {
		t.Fatalf("identity show in %s = %#v", dir, envelope)
	}
	return envelope.Data
}

type worktreeIssueEnvelope struct {
	OK   bool `json:"ok"`
	Data struct {
		EventID string `json:"event_id"`
	} `json:"data"`
	Error *issueCommandError `json:"error"`
}

func openWorktreeIssue(t *testing.T, dir, title string) string {
	t.Helper()
	var envelope worktreeIssueEnvelope
	if err := json.Unmarshal([]byte(mustHNIn(t, dir, "issue", "open", "--json", title)), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || !validEventID(envelope.Data.EventID) {
		t.Fatalf("issue open in %s = %#v", dir, envelope)
	}
	return envelope.Data.EventID
}

// actorEventsIn returns the verified events of actor as seen from dir, in
// sequence order. collectEvents rejects broken or forked actor chains.
func actorEventsIn(t *testing.T, dir, actor string) []StoredEvent {
	t.Helper()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	events, err := collectEvents()
	if err != nil {
		t.Fatalf("collect events in %s: %v", dir, err)
	}
	var chain []StoredEvent
	for _, stored := range events {
		if stored.Event.Actor == actor {
			chain = append(chain, stored)
		}
	}
	for index := range chain {
		for later := index + 1; later < len(chain); later++ {
			if chain[later].Event.Sequence < chain[index].Event.Sequence {
				chain[index], chain[later] = chain[later], chain[index]
			}
		}
	}
	return chain
}

func requireExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func requireAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s exists or is unreadable: %v", path, err)
	}
}

// writeWorktreeKeyring writes the keyring an older release left in a
// linked worktree's own Git directory: either a keyring record with its
// active pointer or only a legacy identity.json.
func writeWorktreeKeyring(t *testing.T, gitDir string, identity *Identity, legacyOnly bool) {
	t.Helper()
	root := filepath.Join(gitDir, "hn")
	for _, directory := range []string{root, filepath.Join(root, "identities")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path string, value any) {
		encoded, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(encoded, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if legacyOnly {
		write(filepath.Join(root, "identity.json"), identity)
		return
	}
	write(filepath.Join(root, "identities", identity.Actor+".json"), recordFromIdentity(identity, identityLifecycleAvailable))
	if err := os.WriteFile(filepath.Join(root, "active"), []byte(identity.Actor+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestWorktreeIdentitySharedFromMainCheckout(t *testing.T) {
	clone := newWorktreeClone(t, 1)
	worktree, worktreeGitDir := clone.worktrees[0], clone.gitDirs[0]
	mustHNIn(t, clone.main, "init", "--name", "Main checkout")
	mainIdentity := shownIdentityIn(t, clone.main)

	if got := shownIdentityIn(t, worktree); got != mainIdentity {
		t.Fatalf("worktree identity = %#v, want main checkout identity %#v", got, mainIdentity)
	}
	human := mustHNIn(t, worktree, "identity", "show")
	if !strings.Contains(human, "Actor:  "+mainIdentity.Actor+"\n") {
		t.Fatalf("worktree human identity show = %q, want actor %s", human, mainIdentity.Actor)
	}
	eventID := openWorktreeIssue(t, worktree, "Opened from a linked worktree")

	chain := actorEventsIn(t, clone.main, mainIdentity.Actor)
	if len(chain) != 1 || chain[0].ID != eventID || chain[0].Event.Actor != mainIdentity.Actor || chain[0].Event.PublicKey != mainIdentity.PublicKey {
		t.Fatalf("main checkout sees worktree event chain %#v, want %s signed by %s", chain, eventID, mainIdentity.Actor)
	}
	if head := worktreeGit(t, clone.main, "rev-parse", actorRef(mainIdentity.Actor)); head != chain[0].Commit {
		t.Fatalf("actor ref = %s, want worktree event commit %s", head, chain[0].Commit)
	}
	requireExists(t, filepath.Join(clone.commonDir, "hn", "identities", mainIdentity.Actor+".json"))
	requireAbsent(t, filepath.Join(worktreeGitDir, "hn", "active"))
	requireAbsent(t, filepath.Join(worktreeGitDir, "hn", "identities"))

	if output, err := runHNIn(t, worktree, "init", "--name", "Second"); err == nil || !strings.Contains(err.Error(), "identity already exists") {
		t.Fatalf("hn init in worktree after main init = %q, %v; want identity already exists", output, err)
	}
	if got := shownIdentityIn(t, worktree); got != mainIdentity {
		t.Fatalf("worktree identity changed after refused init: %#v", got)
	}
}

func TestWorktreeIdentityInitInLinkedWorktreeUsesCommonDirectory(t *testing.T) {
	clone := newWorktreeClone(t, 2)
	first, firstGitDir := clone.worktrees[0], clone.gitDirs[0]
	output := mustHNIn(t, first, "init", "--name", "Worktree first")
	worktreeIdentity := shownIdentityIn(t, first)

	record := filepath.Join(clone.commonDir, "hn", "identities", worktreeIdentity.Actor+".json")
	if !strings.Contains(output, "Private identity stored in "+record+"\n") {
		t.Fatalf("hn init output = %q, want record %s", output, record)
	}
	requireExists(t, record)
	requireExists(t, filepath.Join(clone.commonDir, "hn", "active"))
	requireAbsent(t, filepath.Join(firstGitDir, "hn"))
	requirePathMode(t, filepath.Join(clone.commonDir, "hn"), 0o700)
	requirePathMode(t, record, 0o600)

	for _, dir := range []string{clone.main, clone.worktrees[1]} {
		if got := shownIdentityIn(t, dir); got != worktreeIdentity {
			t.Fatalf("identity in %s = %#v, want worktree-created %#v", dir, got, worktreeIdentity)
		}
	}
	firstEvent := openWorktreeIssue(t, first, "From the initializing worktree")
	secondEvent := openWorktreeIssue(t, clone.main, "From the main checkout")
	chain := actorEventsIn(t, clone.worktrees[1], worktreeIdentity.Actor)
	if len(chain) != 2 || chain[0].ID != firstEvent || chain[1].ID != secondEvent || chain[1].Event.Previous != firstEvent {
		t.Fatalf("shared actor chain = %#v, want %s then %s", chain, firstEvent, secondEvent)
	}
}

func TestWorktreeIdentityBareRepositoryWorktrees(t *testing.T) {
	clone := newWorktreeClone(t, 0)
	bare := filepath.Join(clone.root, "bare.git")
	worktree := filepath.Join(clone.root, "bare-worktree")
	worktreeGit(t, clone.root, "clone", "-q", "--bare", clone.main, bare)
	worktreeGit(t, bare, "worktree", "add", "-q", "-b", "linked", worktree)

	mustHNIn(t, worktree, "init", "--name", "Bare worktree")
	identity := shownIdentityIn(t, worktree)
	requireExists(t, filepath.Join(bare, "hn", "identities", identity.Actor+".json"))
	if got := shownIdentityIn(t, bare); got != identity {
		t.Fatalf("bare repository identity = %#v, want linked worktree identity %#v", got, identity)
	}
	eventID := openWorktreeIssue(t, worktree, "From a bare repository worktree")
	if chain := actorEventsIn(t, bare, identity.Actor); len(chain) != 1 || chain[0].ID != eventID {
		t.Fatalf("bare repository chain = %#v, want %s", chain, eventID)
	}
}

func TestWorktreeIdentityHonoursGitDirectoryOverrides(t *testing.T) {
	clone := newWorktreeClone(t, 1)
	worktreeGitDir := clone.gitDirs[0]
	mustHNIn(t, clone.main, "init", "--name", "Main checkout")
	mainIdentity := shownIdentityIn(t, clone.main)

	outside := filepath.Join(clone.root, "outside", "deep")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_DIR", worktreeGitDir)
	if got := shownIdentityIn(t, outside); got != mainIdentity {
		t.Fatalf("identity with GIT_DIR naming the worktree = %#v, want %#v", got, mainIdentity)
	}

	// Git prints a relative GIT_COMMON_DIR as given, relative to its
	// physical working directory. Reach that directory through a symbolic
	// link, with PWD naming the link as a shell would, so a logical
	// resolution of the relative path names a different directory.
	link := filepath.Join(clone.root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	relative, err := filepath.Rel(outside, clone.commonDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_COMMON_DIR", relative)
	t.Setenv("PWD", link)
	if got := shownIdentityIn(t, link); got != mainIdentity {
		t.Fatalf("identity with relative GIT_COMMON_DIR through a symlinked directory = %#v, want %#v", got, mainIdentity)
	}
}

func TestWorktreeIdentityKeepsPerWorktreeLegacyKeyring(t *testing.T) {
	clone := newWorktreeClone(t, 3)
	mustHNIn(t, clone.main, "init", "--name", "Main checkout")
	mainIdentity := shownIdentityIn(t, clone.main)

	recordHolder := testIdentity(t, "Older worktree keyring")
	writeWorktreeKeyring(t, clone.gitDirs[0], recordHolder, false)
	legacyHolder := testIdentity(t, "Older worktree legacy identity")
	writeWorktreeKeyring(t, clone.gitDirs[1], legacyHolder, true)

	for index, want := range []*Identity{recordHolder, legacyHolder} {
		dir := clone.worktrees[index]
		got := shownIdentityIn(t, dir)
		if got.Actor != want.Actor || got.PublicKey != want.PublicKey {
			t.Fatalf("identity in worktree %d = %#v, want its own %s", index, got, want.Actor)
		}
		eventID := openWorktreeIssue(t, dir, fmt.Sprintf("Signed by worktree %d", index))
		if chain := actorEventsIn(t, clone.main, want.Actor); len(chain) != 1 || chain[0].ID != eventID {
			t.Fatalf("worktree %d actor chain = %#v, want %s", index, chain, eventID)
		}
		list := mustHNIn(t, dir, "identity", "list")
		if !strings.Contains(list, want.Actor) || strings.Contains(list, mainIdentity.Actor) {
			t.Fatalf("worktree %d identity list mixes keyrings: %q", index, list)
		}
	}
	// The legacy identity migrated inside its own worktree's Git directory.
	requireExists(t, filepath.Join(clone.gitDirs[1], "hn", "identities", legacyHolder.Actor+".json"))
	requireExists(t, filepath.Join(clone.gitDirs[1], "hn", "active"))
	requireAbsent(t, filepath.Join(clone.commonDir, "hn", "identities", legacyHolder.Actor+".json"))
	requireAbsent(t, filepath.Join(clone.commonDir, "hn", "identities", recordHolder.Actor+".json"))

	if got := shownIdentityIn(t, clone.main); got != mainIdentity {
		t.Fatalf("main checkout identity = %#v, want its own %#v", got, mainIdentity)
	}
	if got := shownIdentityIn(t, clone.worktrees[2]); got != mainIdentity {
		t.Fatalf("worktree without its own keyring = %#v, want the clone identity %#v", got, mainIdentity)
	}
	if active, err := os.ReadFile(filepath.Join(clone.commonDir, "hn", "active")); err != nil || string(active) != mainIdentity.Actor+"\n" {
		t.Fatalf("common active pointer = %q, %v; want %s", active, err, mainIdentity.Actor)
	}
}

func TestWorktreeIdentityUnreadableKeyringIsAnError(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(t *testing.T, hnDir string)
		want    string
	}{
		{"hn is a regular file", func(t *testing.T, hnDir string) {
			if err := os.WriteFile(hnDir, []byte("not a directory\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, "is not a directory"},
		{"hn is a symbolic link", func(t *testing.T, hnDir string) {
			target := hnDir + "-target"
			if err := os.Mkdir(target, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, hnDir); err != nil {
				t.Skipf("symbolic links unavailable: %v", err)
			}
		}, "is a symbolic link"},
		{"identities is a regular file", func(t *testing.T, hnDir string) {
			if err := os.Mkdir(hnDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(hnDir, "identities"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}, "is not a directory"},
		{"hn denies access", func(t *testing.T, hnDir string) {
			if runtime.GOOS == "windows" || os.Geteuid() == 0 {
				t.Skip("permission bits do not deny access here")
			}
			if err := os.Mkdir(hnDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(hnDir, 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(hnDir, 0o700) })
		}, "permission denied"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			clone := newWorktreeClone(t, 1)
			worktree := clone.worktrees[0]
			mustHNIn(t, clone.main, "init", "--name", "Main checkout")
			mainIdentity := shownIdentityIn(t, clone.main)
			openWorktreeIssue(t, clone.main, "Before the worktree keyring broke")
			headBefore := worktreeGit(t, clone.main, "rev-parse", "--verify", actorRef(mainIdentity.Actor))
			test.prepare(t, filepath.Join(clone.gitDirs[0], "hn"))

			for _, args := range [][]string{{"identity", "show"}, {"identity", "show", "--json"}, {"identity", "list"}, {"issue", "open", "Must not be signed"}, {"init"}} {
				output, err := runHNIn(t, worktree, args...)
				if err == nil {
					t.Fatalf("hn %s fell back past an unreadable worktree keyring: %q", strings.Join(args, " "), output)
				}
				if strings.Contains(output, mainIdentity.Actor) {
					t.Fatalf("hn %s exposed the common identity: %q", strings.Join(args, " "), output)
				}
				if args[len(args)-1] != "--json" && (!strings.Contains(err.Error(), "inspect worktree identity keyring") || !strings.Contains(err.Error(), test.want)) {
					t.Fatalf("hn %s error = %v, want an inspection error containing %q", strings.Join(args, " "), err, test.want)
				}
			}
			if head := worktreeGit(t, clone.main, "rev-parse", "--verify", actorRef(mainIdentity.Actor)); head != headBefore {
				t.Fatalf("actor ref moved from %s to %s", headBefore, head)
			}
			if got := shownIdentityIn(t, clone.main); got != mainIdentity {
				t.Fatalf("main checkout identity = %#v, want %#v", got, mainIdentity)
			}
		})
	}
}

func TestWorktreeIdentityPartialWorktreeKeyringIsNotMixed(t *testing.T) {
	clone := newWorktreeClone(t, 3)
	mustHNIn(t, clone.main, "init", "--name", "Main checkout")
	mainIdentity := shownIdentityIn(t, clone.main)

	// A private hn directory holding only other private state, such as the
	// memory index, does not pin the worktree.
	unrelated := filepath.Join(clone.gitDirs[0], "hn", "memory")
	if err := os.MkdirAll(unrelated, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clone.gitDirs[0], "hn", ".hn-private-interrupted"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := shownIdentityIn(t, clone.worktrees[0]); got != mainIdentity {
		t.Fatalf("worktree with unrelated private state = %#v, want clone identity %#v", got, mainIdentity)
	}

	// A worktree keyring holding a record but no active pointer is still
	// that worktree's keyring; the common pointer is never combined with it.
	orphan := testIdentity(t, "Interrupted worktree init")
	writeWorktreeKeyring(t, clone.gitDirs[1], orphan, false)
	if err := os.Remove(filepath.Join(clone.gitDirs[1], "hn", "active")); err != nil {
		t.Fatal(err)
	}
	output, err := runHNIn(t, clone.worktrees[1], "identity", "show")
	if err == nil || !strings.Contains(err.Error(), "no identity") || strings.Contains(output, mainIdentity.Actor) {
		t.Fatalf("partial worktree keyring identity show = %q, %v; want no identity without the common actor", output, err)
	}
	list := mustHNIn(t, clone.worktrees[1], "identity", "list")
	if !strings.Contains(list, orphan.Actor) || strings.Contains(list, mainIdentity.Actor) {
		t.Fatalf("partial worktree keyring list = %q, want only %s", list, orphan.Actor)
	}

	// A rotation command journal holds a rotation target's private key, so
	// it pins the worktree keyring too.
	journalRoot := filepath.Join(clone.gitDirs[2], "hn")
	if err := os.Mkdir(journalRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(journalRoot, "rotation-command.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output, err = runHNIn(t, clone.worktrees[2], "identity", "show")
	if err == nil || !strings.Contains(err.Error(), "no identity") || strings.Contains(output, mainIdentity.Actor) {
		t.Fatalf("worktree with only a rotation journal identity show = %q, %v; want no identity without the common actor", output, err)
	}
}

type worktreeChildResult struct {
	Stdout string `json:"stdout"`
	Error  string `json:"error"`
}

// TestWorktreeIdentityCLIChild runs one hn command line in a separate
// process for the concurrency tests below. It does nothing when run
// directly.
func TestWorktreeIdentityCLIChild(t *testing.T) {
	dir := os.Getenv("HN_WORKTREE_TEST_CHILD_DIR")
	if dir == "" {
		return
	}
	var args []string
	if err := json.Unmarshal([]byte(os.Getenv("HN_WORKTREE_TEST_CHILD_ARGS")), &args); err != nil {
		t.Fatal(err)
	}
	start, err := strconv.ParseInt(os.Getenv("HN_WORKTREE_TEST_CHILD_START"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	// Starting together after process startup makes the commands overlap.
	time.Sleep(time.Until(time.Unix(0, start)))
	stdout, runErr := captureTestOutput(t, func() error { return run(args) })
	result := worktreeChildResult{Stdout: stdout}
	if runErr != nil {
		result.Error = runErr.Error()
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("HN_WORKTREE_TEST_CHILD_RESULT"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
}

// runHNInParallel runs one command line per directory, each in its own
// process, started together.
func runHNInParallel(t *testing.T, dirs []string, args func(index int) []string) []worktreeChildResult {
	t.Helper()
	results := t.TempDir()
	start := strconv.FormatInt(time.Now().Add(500*time.Millisecond).UnixNano(), 10)
	commands := make([]*exec.Cmd, len(dirs))
	outputs := make([][]byte, len(dirs))
	for index, dir := range dirs {
		encoded, err := json.Marshal(args(index))
		if err != nil {
			t.Fatal(err)
		}
		command := exec.Command(os.Args[0], "-test.run=^TestWorktreeIdentityCLIChild$")
		command.Env = append(os.Environ(),
			"HN_WORKTREE_TEST_CHILD_DIR="+dir,
			"HN_WORKTREE_TEST_CHILD_ARGS="+string(encoded),
			"HN_WORKTREE_TEST_CHILD_START="+start,
			"HN_WORKTREE_TEST_CHILD_RESULT="+filepath.Join(results, strconv.Itoa(index)+".json"),
		)
		commands[index] = command
	}
	errs := make(chan error, len(commands))
	for index, command := range commands {
		go func() {
			output, err := command.CombinedOutput()
			outputs[index] = output
			errs <- err
		}()
	}
	for range commands {
		if err := <-errs; err != nil {
			t.Fatalf("child process: %v", err)
		}
	}
	parsed := make([]worktreeChildResult, len(dirs))
	for index := range dirs {
		raw, err := os.ReadFile(filepath.Join(results, strconv.Itoa(index)+".json"))
		if err != nil {
			t.Fatalf("child %d result: %v\n%s", index, err, outputs[index])
		}
		if err := json.Unmarshal(raw, &parsed[index]); err != nil {
			t.Fatal(err)
		}
	}
	return parsed
}

func TestWorktreeConcurrentAppendsKeepOneActorChain(t *testing.T) {
	clone := newWorktreeClone(t, 2)
	mustHNIn(t, clone.main, "init", "--name", "Shared actor")
	actor := shownIdentityIn(t, clone.main).Actor

	const rounds = 20
	var signed []string
	conflicts := 0
	for round := 0; round < rounds; round++ {
		results := runHNInParallel(t, clone.worktrees, func(index int) []string {
			return []string{"issue", "open", "--json", fmt.Sprintf("Round %d from worktree %d", round, index)}
		})
		for index, result := range results {
			var envelope worktreeIssueEnvelope
			if err := json.Unmarshal([]byte(result.Stdout), &envelope); err != nil {
				t.Fatalf("round %d worktree %d output %q: %v", round, index, result.Stdout, err)
			}
			if result.Error == "" {
				if !envelope.OK || !validEventID(envelope.Data.EventID) {
					t.Fatalf("round %d worktree %d succeeded without an event: %q", round, index, result.Stdout)
				}
				signed = append(signed, envelope.Data.EventID)
				continue
			}
			// A losing append must fail as a reported concurrent update,
			// leaving nothing behind, and a retry must then succeed.
			conflicts++
			if envelope.OK || envelope.Error == nil || envelope.Error.Code != "concurrent_update" || !strings.Contains(result.Error, "retry") {
				t.Fatalf("round %d worktree %d failed uncleanly: %q (%s)", round, index, result.Stdout, result.Error)
			}
			signed = append(signed, openWorktreeIssue(t, clone.worktrees[index], fmt.Sprintf("Round %d retry from worktree %d", round, index)))
		}
	}
	t.Logf("%d of %d concurrent appends lost the race and were retried", conflicts, 2*rounds)

	chain := actorEventsIn(t, clone.main, actor)
	if len(chain) != len(signed) {
		t.Fatalf("actor chain has %d events, but %d appends reported success", len(chain), len(signed))
	}
	reported := make(map[string]bool, len(signed))
	for _, id := range signed {
		reported[id] = true
	}
	for index, stored := range chain {
		if !reported[stored.ID] {
			t.Fatalf("chain event %s was not reported by any command", stored.ID)
		}
		delete(reported, stored.ID)
		if stored.Event.Sequence != uint64(index+1) || (index > 0 && stored.Event.Previous != chain[index-1].ID) {
			t.Fatalf("chain breaks at index %d: %#v", index, stored.Event)
		}
	}
	if len(reported) != 0 {
		t.Fatalf("reported events missing from the actor chain: %v", reported)
	}
	if count := worktreeGit(t, clone.main, "rev-list", "--count", actorRef(actor)); count != strconv.Itoa(len(signed)) {
		t.Fatalf("actor ref history has %s commits, want %d", count, len(signed))
	}
	if merges := worktreeGit(t, clone.main, "rev-list", "--min-parents=2", actorRef(actor)); merges != "" {
		t.Fatalf("actor ref history forks: %s", merges)
	}
}

func TestWorktreeConcurrentInitActivatesOneIdentity(t *testing.T) {
	const rounds = 8
	for round := 0; round < rounds; round++ {
		clone := newWorktreeClone(t, 2)
		results := runHNInParallel(t, clone.worktrees, func(index int) []string {
			return []string{"init", "--name", fmt.Sprintf("Racer %d", index)}
		})
		winner := -1
		for index, result := range results {
			if result.Error == "" {
				if winner != -1 {
					t.Fatalf("round %d: both concurrent hn init commands reported success: %#v", round, results)
				}
				winner = index
				continue
			}
			if !strings.Contains(result.Error, "identity already exists") {
				t.Fatalf("round %d worktree %d init failed uncleanly: %s", round, index, result.Error)
			}
		}
		if winner == -1 {
			t.Fatalf("round %d: no concurrent hn init succeeded: %#v", round, results)
		}
		want := fmt.Sprintf("Racer %d", winner)
		identity := shownIdentityIn(t, clone.main)
		if identity.Name != want || !strings.Contains(results[winner].Stdout, "("+shortID(identity.Actor)+")") {
			t.Fatalf("round %d: active identity %#v, want the reported winner %q", round, identity, results[winner].Stdout)
		}
		for _, dir := range clone.worktrees {
			if got := shownIdentityIn(t, dir); got != identity {
				t.Fatalf("round %d: identity in %s = %#v, want %#v", round, dir, got, identity)
			}
		}
		records, err := os.ReadDir(filepath.Join(clone.commonDir, "hn", "identities"))
		if err != nil {
			t.Fatal(err)
		}
		if len(records) != 1 || records[0].Name() != identity.Actor+".json" {
			t.Fatalf("round %d: keyring records %v, want only the active %s", round, records, identity.Actor)
		}
	}
}
