package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

const serveTestContractConfig = `{
  "version": 1,
  "name": "rust-pingpong",
  "protected": ["refs/heads/main"],
  "lfsQuotaBytes": 1073741824,
  "budgets": {
    "maxEvents": 10000,
    "maxObjects": 100000,
    "maxObjectBytes": 67108864,
    "maxAttachmentBytes": 16777216,
    "maxTotalBytes": 1073741824
  }
}
`

const serveTestContractRegistry = `{
  "version": 1,
  "keys": [
    {
      "fingerprint": "SHA256:Jd8bC8QWw0m0Y4l6m6yZ1gq5q3o9s0Kc2b2f4QwA9eU",
      "publicKey": "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExampleOnlyNotARealKeyxxxxxxxxxxxxxxxxxxxxx",
      "actor": "4f1c9a6d0d2b7e3a8c5f6e1b9d0a7c3e2f4b6d8a0c1e3f5a7b9d1c3e5f7a9b1c",
      "added": "2026-10-24T20:00:00Z"
    }
  ]
}
`

func serveTestHome(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("hn serve requires a Unix host")
	}
	if os.Geteuid() == 0 {
		t.Skip("hn serve refuses uid 0; run tests as an unprivileged user")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HN_INTERNAL_TESTING", "0")
	return home
}

func serveTestExecutable(t *testing.T) string {
	t.Helper()
	path, err := serveExecutablePath()
	if err != nil {
		t.Fatalf("serveExecutablePath: %v", err)
	}
	return path
}

func newServeTestLayout(t *testing.T) serveLayout {
	t.Helper()
	serveTestHome(t)
	layout, err := resolveServeLayout()
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureServeLayout(layout); err != nil {
		t.Fatal(err)
	}
	if err := ensureServeHooks(layout, serveTestExecutable(t)); err != nil {
		t.Fatal(err)
	}
	return layout
}

func mkdirServeTest(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Mkdir(path, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func writeServeTestFile(t *testing.T, path, contents string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func chmodServeTest(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func symlinkServeTest(t *testing.T, target, path string) {
	t.Helper()
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

func removeServeTest(t *testing.T, path string) {
	t.Helper()
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
}

func addServeTestRepo(t *testing.T, layout serveLayout, name string) {
	t.Helper()
	mkdirServeTest(t, layout.RepoGitDir(name), 0o700)
	mkdirServeTest(t, layout.RepoStateDir(name), 0o700)
	if err := saveServeRepoConfig(layout, newServeRepoConfig(name, nil)); err != nil {
		t.Fatal(err)
	}
}

func addServeTestLFS(t *testing.T, layout serveLayout, name string) {
	t.Helper()
	mkdirServeTest(t, layout.RepoLFSDir(name), 0o700)
	mkdirServeTest(t, layout.RepoLFSIncoming(name), 0o700)
	mkdirServeTest(t, layout.RepoLFSObjects(name), 0o700)
}

func requireServeDefect(t *testing.T, err error, path, reason string) {
	t.Helper()
	if err == nil {
		t.Fatalf("validation passed, want a defect at %s (%s)", path, reason)
	}
	prefix := "hn serve state defect: " + path + ": "
	if !strings.HasPrefix(err.Error(), prefix) || !strings.Contains(err.Error(), reason) {
		t.Fatalf("defect = %q, want prefix %q and reason containing %q", err, prefix, reason)
	}
	var defect *serveStateDefectError
	if !errors.As(err, &defect) {
		t.Fatalf("defect %q is not a serveStateDefectError", err)
	}
}

func requireServeErrorContains(t *testing.T, err error, fragment string) {
	t.Helper()
	if err == nil {
		t.Fatalf("operation succeeded, want an error containing %q", fragment)
	}
	if !strings.Contains(err.Error(), fragment) {
		t.Fatalf("error = %q, want it to contain %q", err, fragment)
	}
}

func registerServeProbeRoute(t *testing.T, name string, mode serveValidationMode) *[]serveLayout {
	t.Helper()
	calls := &[]serveLayout{}
	previous := lookupServeRoute
	lookupServeRoute = func(subcommand string) (serveRoute, bool) {
		if subcommand == name {
			return serveRoute{mode: mode, run: func(layout serveLayout, args []string) error {
				*calls = append(*calls, layout)
				return nil
			}}, true
		}
		return previous(subcommand)
	}
	t.Cleanup(func() { lookupServeRoute = previous })
	return calls
}

func serveTestFingerprint(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

func serveTestActor(seed string) string {
	sum := sha256.Sum256([]byte("actor:" + seed))
	return hex.EncodeToString(sum[:])
}

func serveTestBinding(seed string) serveKeyBinding {
	return serveKeyBinding{
		Fingerprint: serveTestFingerprint(seed),
		PublicKey:   "ssh-ed25519 " + base64.StdEncoding.EncodeToString([]byte("key:"+seed)),
		Actor:       serveTestActor(seed),
		Added:       "2026-10-24T20:00:00Z",
	}
}

func serveTestRegistry(seeds ...string) serveRegistry {
	registry := serveRegistry{Version: 1, Keys: []serveKeyBinding{}}
	for _, seed := range seeds {
		registry.Keys = append(registry.Keys, serveTestBinding(seed))
	}
	sort.Slice(registry.Keys, func(i, j int) bool {
		return registry.Keys[i].Fingerprint < registry.Keys[j].Fingerprint
	})
	return registry
}

func marshalServeTest(t *testing.T, value any) string {
	t.Helper()
	contents, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(contents) + "\n"
}

// T007: the serve guide keeps the headings later work packages fill in.

func TestServeStateGuideSkeleton(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("docs", "serve-v0.md"))
	if err != nil {
		t.Fatal(err)
	}
	guide := string(contents)
	want := []string{
		"# hn serve v0: governed local Git hosting",
		"## Overview",
		"## Server account and state layout",
		"## Key bindings and authorized_keys",
		"## Administering hosted repositories",
		"## Connecting over SSH",
		"## Push verdicts and ref classes",
		"## Hubnot fact refs and budgets",
		"## Protected branches",
		"## Recovering a protected branch",
		"## Advancing a protected branch",
		"## Runners and verifiers",
		"## Verification and traceability",
		"## Large files with Git LFS",
	}
	// Later work packages may add headings; the skeleton ones must stay, in order.
	next := 0
	for _, line := range strings.Split(guide, "\n") {
		if next < len(want) && line == want[next] {
			next++
		}
	}
	if next != len(want) {
		t.Fatalf("guide is missing heading %q (or it is out of order)", want[next])
	}
}

// T001: dispatcher, uid-0 refusal, usage.

func TestServeStateDispatcherRequiresSubcommand(t *testing.T) {
	serveTestHome(t)
	err := run([]string{"serve"})
	if err == nil || err.Error() != "usage: hn serve repo|key|git|hook ..." {
		t.Fatalf("hn serve without arguments = %v, want the usage error", err)
	}
}

func TestServeStateDispatcherRejectsUnknownSubcommandWithoutState(t *testing.T) {
	home := serveTestHome(t)
	before := snapshotLegacyPrivateTree(t, home)
	err := run([]string{"serve", "bogus"})
	requireServeErrorContains(t, err, `unknown hn serve subcommand "bogus"; run 'hn help'`)
	if after := snapshotLegacyPrivateTree(t, home); !reflect.DeepEqual(before, after) {
		t.Fatalf("unknown subcommand changed HOME:\nbefore %v\nafter  %v", before, after)
	}
}

func TestServeStateRootRefusalPrecedesEveryStateAccess(t *testing.T) {
	home := serveTestHome(t)
	writeServeTestFile(t, filepath.Join(home, "sentinel"), "unchanged\n", 0o600)
	adminCalls := registerServeProbeRoute(t, "probe-admin", serveAdministrationValidation)
	sessionCalls := registerServeProbeRoute(t, "probe-session", serveSessionValidation)
	t.Setenv("HN_INTERNAL_TESTING", "1")
	t.Setenv("HN_TEST_SERVE_EUID", "0")
	before := snapshotLegacyPrivateTree(t, home)

	invocations := [][]string{
		{"serve"},
		{"serve", "repo", "list"},
		{"serve", "key", "add", "--actor", serveTestActor("a"), "-"},
		{"serve", "git", "--key", serveTestFingerprint("a")},
		{"serve", "hook", "pre-receive"},
		{"serve", "bogus"},
		{"serve", "probe-admin"},
		{"serve", "probe-session"},
	}
	for _, args := range invocations {
		err := run(args)
		if err == nil || err.Error() != "hn serve refuses to run as root; create an unprivileged user for the server" {
			t.Fatalf("run(%q) = %v, want the root refusal", args, err)
		}
	}
	t.Setenv("HOME", "")
	if err := run([]string{"serve", "probe-admin"}); err == nil || !strings.Contains(err.Error(), "refuses to run as root") {
		t.Fatalf("root refusal must come before HOME is resolved, got %v", err)
	}
	t.Setenv("HOME", home)

	if after := snapshotLegacyPrivateTree(t, home); !reflect.DeepEqual(before, after) {
		t.Fatalf("root refusal changed HOME:\nbefore %v\nafter  %v", before, after)
	}
	if len(*adminCalls) != 0 || len(*sessionCalls) != 0 {
		t.Fatalf("root refusal dispatched a route: admin %d, session %d", len(*adminCalls), len(*sessionCalls))
	}
}

func TestServeStateEUIDOverrideRequiresTestingGate(t *testing.T) {
	serveTestHome(t)
	t.Setenv("HN_TEST_SERVE_EUID", "0")
	err := run([]string{"serve"})
	if err == nil || err.Error() != "usage: hn serve repo|key|git|hook ..." {
		t.Fatalf("HN_TEST_SERVE_EUID without HN_INTERNAL_TESTING=1 = %v, want it ignored", err)
	}
}

func TestServeStateEUIDOverrideParsesStrictly(t *testing.T) {
	serveTestHome(t)
	t.Setenv("HN_INTERNAL_TESTING", "1")
	for _, value := range []string{"", "root", "-1", "+0", " 0", "0x0", "1.0", "99999999999999999999"} {
		t.Setenv("HN_TEST_SERVE_EUID", value)
		requireServeErrorContains(t, run([]string{"serve"}), "HN_TEST_SERVE_EUID")
	}
	t.Setenv("HN_TEST_SERVE_EUID", "1000")
	if err := run([]string{"serve"}); err == nil || err.Error() != "usage: hn serve repo|key|git|hook ..." {
		t.Fatalf("non-root override = %v, want the usage error", err)
	}
}

func TestServeStateUsageListsServeCommands(t *testing.T) {
	output := captureStdout(t, printUsage)
	if !strings.Contains(output, "Usage:\n  hn init") {
		t.Fatalf("usage lost the hn init line:\n%s", output)
	}
	want := "  hn merge PROPOSAL\n" +
		"  hn serve repo create|list|show|set|lock-exec ...\n" +
		"  hn serve key add|remove|list ...\n" +
		"  hn serve git --key FINGERPRINT\n"
	if !strings.Contains(output, want) {
		t.Fatalf("usage does not list hn serve after hn merge:\n%s", output)
	}
}

func TestServeStateDispatcherValidatesBeforeRouting(t *testing.T) {
	home := serveTestHome(t)
	adminCalls := registerServeProbeRoute(t, "probe-admin", serveAdministrationValidation)
	sessionCalls := registerServeProbeRoute(t, "probe-session", serveSessionValidation)
	root := filepath.Join(home, ".hn-serve")

	before := snapshotLegacyPrivateTree(t, home)
	requireServeDefect(t, run([]string{"serve", "probe-session"}), root, "does not exist")
	if after := snapshotLegacyPrivateTree(t, home); !reflect.DeepEqual(before, after) {
		t.Fatalf("session validation wrote to HOME:\nbefore %v\nafter  %v", before, after)
	}
	if len(*sessionCalls) != 0 {
		t.Fatal("session route ran on a missing layout")
	}

	if err := run([]string{"serve", "probe-admin", "extra"}); err != nil {
		t.Fatalf("administration route on a fresh HOME: %v", err)
	}
	if len(*adminCalls) != 1 || (*adminCalls)[0].Root != root {
		t.Fatalf("administration route calls = %+v, want one call with root %s", *adminCalls, root)
	}
	hnPath := serveTestExecutable(t)
	for _, name := range []string{"pre-receive", "post-receive"} {
		contents, err := os.ReadFile(filepath.Join(root, "hooks", name))
		if err != nil {
			t.Fatal(err)
		}
		if string(contents) != string(serveHookScript(name, hnPath)) {
			t.Fatalf("hook %s = %q", name, contents)
		}
	}

	if err := run([]string{"serve", "probe-session"}); err != nil {
		t.Fatalf("session route on an ensured layout: %v", err)
	}
	if len(*sessionCalls) != 1 {
		t.Fatalf("session route calls = %d, want 1", len(*sessionCalls))
	}

	writeServeTestFile(t, filepath.Join(root, "state", "stray"), "x", 0o600)
	requireServeDefect(t, run([]string{"serve", "probe-session"}), filepath.Join(root, "state", "stray"), "unexpected entry")
	requireServeDefect(t, run([]string{"serve", "probe-admin"}), filepath.Join(root, "state", "stray"), "unexpected entry")
	if len(*sessionCalls) != 1 || len(*adminCalls) != 1 {
		t.Fatal("a route ran on a defective layout")
	}
}

// T002: layout resolution and fail-closed validation.

func TestServeStateResolveLayoutRequiresAbsoluteHome(t *testing.T) {
	serveTestHome(t)
	for _, home := range []string{"", "relative/home"} {
		t.Setenv("HOME", home)
		requireServeErrorContains(t, func() error { _, err := resolveServeLayout(); return err }(), "HOME")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	layout, err := resolveServeLayout()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(home, ".hn-serve")
	want := serveLayout{
		Home:           home,
		Root:           root,
		Hooks:          filepath.Join(root, "hooks"),
		Repositories:   filepath.Join(root, "repositories"),
		State:          filepath.Join(root, "state"),
		Registry:       filepath.Join(root, "state", "registry.json"),
		Tmp:            filepath.Join(root, "state", "tmp"),
		Repos:          filepath.Join(root, "state", "repos"),
		SSHDir:         filepath.Join(home, ".ssh"),
		AuthorizedKeys: filepath.Join(home, ".ssh", "authorized_keys"),
	}
	if layout != want {
		t.Fatalf("layout = %+v, want %+v", layout, want)
	}
	if entries, err := os.ReadDir(home); err != nil || len(entries) != 0 {
		t.Fatalf("resolveServeLayout created entries %v (%v)", entries, err)
	}
	repo := filepath.Join(root, "state", "repos", "demo")
	paths := map[string]string{
		layout.RepoGitDir("demo"):      filepath.Join(root, "repositories", "demo.git"),
		layout.RepoStateDir("demo"):    repo,
		layout.RepoConfig("demo"):      filepath.Join(repo, "config.json"),
		layout.RepoBranches("demo"):    filepath.Join(repo, "branches.json"),
		layout.RepoPushLog("demo"):     filepath.Join(repo, "push.log"),
		layout.RepoLock("demo"):        filepath.Join(repo, "lock"),
		layout.RepoLFSLock("demo"):     filepath.Join(repo, "lfs.lock"),
		layout.RepoLFSDir("demo"):      filepath.Join(repo, "lfs"),
		layout.RepoLFSIncoming("demo"): filepath.Join(repo, "lfs", "incoming"),
		layout.RepoLFSObjects("demo"):  filepath.Join(repo, "lfs", "objects"),
	}
	for got, want := range paths {
		if got != want {
			t.Fatalf("repository path = %s, want %s", got, want)
		}
	}
}

func TestServeStateEnsuredEmptyLayoutValidates(t *testing.T) {
	layout := newServeTestLayout(t)
	for _, directory := range []string{layout.Root, layout.Hooks, layout.Repositories, layout.State, layout.Tmp, layout.Repos} {
		info, err := os.Lstat(directory)
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Fatalf("%s mode = %v, want a 0700 directory", directory, info.Mode())
		}
	}
	for _, mode := range []serveValidationMode{serveSessionValidation, serveAdministrationValidation} {
		if err := validateServeLayout(layout, mode); err != nil {
			t.Fatalf("fresh layout (mode %d): %v", mode, err)
		}
	}
	if err := ensureServeLayout(layout); err != nil {
		t.Fatalf("ensureServeLayout is not idempotent: %v", err)
	}
}

func TestServeStatePopulatedLayoutValidates(t *testing.T) {
	layout := newServeTestLayout(t)
	addServeTestRepo(t, layout, "alpha")
	addServeTestRepo(t, layout, "beta.v2")
	addServeTestLFS(t, layout, "alpha")
	if err := saveServeRegistry(layout, serveTestRegistry("a", "b")); err != nil {
		t.Fatal(err)
	}
	owners := serveBranchOwners{Version: 1, Branches: []serveBranchOwner{{Ref: "refs/heads/feature/a", Owner: serveTestActor("a"), Created: "2026-10-24T20:05:00Z"}}}
	if err := saveServeBranchOwners(layout, "alpha", owners); err != nil {
		t.Fatal(err)
	}
	if err := appendServePushLog(layout, "alpha", servePushLogEntry{Time: "2026-10-24T20:06:00Z", Actor: serveTestActor("a"), Key: serveTestFingerprint("a"), Verdict: "accepted"}); err != nil {
		t.Fatal(err)
	}
	for _, lock := range []string{layout.RepoLock("alpha"), layout.RepoLFSLock("alpha")} {
		release, err := acquireServeLock(lock, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := release(); err != nil {
			t.Fatal(err)
		}
	}
	objectDir := filepath.Join(layout.RepoLFSObjects("alpha"), "ab", "cd")
	if err := os.MkdirAll(objectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	chmodServeTest(t, filepath.Dir(objectDir), 0o700)
	chmodServeTest(t, objectDir, 0o700)
	writeServeTestFile(t, filepath.Join(objectDir, "abcd"+strings.Repeat("0", 60)), "object", 0o600)
	writeServeTestFile(t, filepath.Join(layout.RepoLFSIncoming("alpha"), ".staged-1"), "partial", 0o600)
	writeServeTestFile(t, filepath.Join(layout.Tmp, "admin.lock"), "", 0o600)
	mkdirServeTest(t, layout.SSHDir, 0o700)

	if err := validateServeLayout(layout, serveSessionValidation); err != nil {
		t.Fatalf("populated layout: %v", err)
	}
}

func TestServeStateValidationRefusesEachLayoutDefect(t *testing.T) {
	type plantFunc func(t *testing.T, layout serveLayout) (path, reason string)
	cases := []struct {
		name     string
		plant    plantFunc
		repaired bool
	}{
		{"symlinked server root", func(t *testing.T, l serveLayout) (string, string) {
			real := filepath.Join(l.Home, "real-serve")
			if err := os.Rename(l.Root, real); err != nil {
				t.Fatal(err)
			}
			symlinkServeTest(t, real, l.Root)
			return l.Root, "symbolic link"
		}, false},
		{"symlinked ssh directory", func(t *testing.T, l serveLayout) (string, string) {
			symlinkServeTest(t, t.TempDir(), l.SSHDir)
			return l.SSHDir, "symbolic link"
		}, false},
		{"hooks directory mode", func(t *testing.T, l serveLayout) (string, string) {
			chmodServeTest(t, l.Hooks, 0o755)
			return l.Hooks, "mode 0755"
		}, false},
		{"state directory mode", func(t *testing.T, l serveLayout) (string, string) {
			chmodServeTest(t, l.State, 0o750)
			return l.State, "mode 0750"
		}, false},
		{"missing tmp directory", func(t *testing.T, l serveLayout) (string, string) {
			removeServeTest(t, l.Tmp)
			return l.Tmp, "does not exist"
		}, false},
		{"tmp is a file", func(t *testing.T, l serveLayout) (string, string) {
			removeServeTest(t, l.Tmp)
			writeServeTestFile(t, l.Tmp, "", 0o600)
			return l.Tmp, "not a directory"
		}, false},
		{"repository state directory mode", func(t *testing.T, l serveLayout) (string, string) {
			addServeTestRepo(t, l, "demo")
			chmodServeTest(t, l.RepoStateDir("demo"), 0o755)
			return l.RepoStateDir("demo"), "mode 0755"
		}, false},
		{"unexpected entry in server root", func(t *testing.T, l serveLayout) (string, string) {
			path := filepath.Join(l.Root, "notes")
			writeServeTestFile(t, path, "x", 0o600)
			return path, "unexpected entry"
		}, false},
		{"unexpected entry in state", func(t *testing.T, l serveLayout) (string, string) {
			path := filepath.Join(l.State, "extra.json")
			writeServeTestFile(t, path, "{}", 0o600)
			return path, "unexpected entry"
		}, false},
		{"unexpected entries reported in sorted order", func(t *testing.T, l serveLayout) (string, string) {
			writeServeTestFile(t, filepath.Join(l.State, "zz-extra"), "", 0o600)
			writeServeTestFile(t, filepath.Join(l.State, "aa-extra"), "", 0o600)
			return filepath.Join(l.State, "aa-extra"), "unexpected entry"
		}, false},
		{"unexpected entry in repository state", func(t *testing.T, l serveLayout) (string, string) {
			addServeTestRepo(t, l, "demo")
			path := filepath.Join(l.RepoStateDir("demo"), "notes.txt")
			writeServeTestFile(t, path, "", 0o600)
			return path, "unexpected entry"
		}, false},
		{"unexpected entry in lfs", func(t *testing.T, l serveLayout) (string, string) {
			addServeTestRepo(t, l, "demo")
			addServeTestLFS(t, l, "demo")
			path := filepath.Join(l.RepoLFSDir("demo"), "other")
			mkdirServeTest(t, path, 0o700)
			return path, "unexpected entry"
		}, false},
		{"lfs incoming mode", func(t *testing.T, l serveLayout) (string, string) {
			addServeTestRepo(t, l, "demo")
			addServeTestLFS(t, l, "demo")
			chmodServeTest(t, l.RepoLFSIncoming("demo"), 0o711)
			return l.RepoLFSIncoming("demo"), "mode 0711"
		}, false},
		{"invalid repository state name", func(t *testing.T, l serveLayout) (string, string) {
			path := filepath.Join(l.Repos, ".hidden")
			mkdirServeTest(t, path, 0o700)
			return path, "not a valid repository name"
		}, false},
		{"state without repository", func(t *testing.T, l serveLayout) (string, string) {
			mkdirServeTest(t, l.RepoStateDir("demo"), 0o700)
			if err := saveServeRepoConfig(l, newServeRepoConfig("demo", nil)); err != nil {
				t.Fatal(err)
			}
			return l.RepoStateDir("demo"), "no matching repository"
		}, false},
		{"repository without state", func(t *testing.T, l serveLayout) (string, string) {
			mkdirServeTest(t, l.RepoGitDir("demo"), 0o700)
			return l.RepoGitDir("demo"), "no matching state directory"
		}, false},
		{"symlinked repository", func(t *testing.T, l serveLayout) (string, string) {
			addServeTestRepo(t, l, "demo")
			removeServeTest(t, l.RepoGitDir("demo"))
			symlinkServeTest(t, t.TempDir(), l.RepoGitDir("demo"))
			return l.RepoGitDir("demo"), "symbolic link"
		}, false},
		{"repository entry is a file", func(t *testing.T, l serveLayout) (string, string) {
			addServeTestRepo(t, l, "demo")
			removeServeTest(t, l.RepoGitDir("demo"))
			writeServeTestFile(t, l.RepoGitDir("demo"), "", 0o600)
			return l.RepoGitDir("demo"), "not a directory"
		}, false},
		{"repository entry without git suffix", func(t *testing.T, l serveLayout) (string, string) {
			path := filepath.Join(l.Repositories, "demo")
			mkdirServeTest(t, path, 0o700)
			return path, "not a hosted repository directory"
		}, false},
		{"repository entry with invalid name", func(t *testing.T, l serveLayout) (string, string) {
			path := filepath.Join(l.Repositories, "-bad.git")
			mkdirServeTest(t, path, 0o700)
			return path, "not a hosted repository directory"
		}, false},
		{"missing repository config", func(t *testing.T, l serveLayout) (string, string) {
			addServeTestRepo(t, l, "demo")
			removeServeTest(t, l.RepoConfig("demo"))
			return l.RepoConfig("demo"), "does not exist"
		}, false},
		{"repository config mode", func(t *testing.T, l serveLayout) (string, string) {
			addServeTestRepo(t, l, "demo")
			chmodServeTest(t, l.RepoConfig("demo"), 0o644)
			return l.RepoConfig("demo"), "mode 0644"
		}, false},
		{"repository config unknown field", func(t *testing.T, l serveLayout) (string, string) {
			addServeTestRepo(t, l, "demo")
			contents := strings.Replace(serveTestContractConfig, `"name": "rust-pingpong",`, `"name": "demo", "owner": "x",`, 1)
			writeServeTestFile(t, l.RepoConfig("demo"), contents, 0o600)
			return l.RepoConfig("demo"), "unknown field"
		}, false},
		{"repository config names another repository", func(t *testing.T, l serveLayout) (string, string) {
			addServeTestRepo(t, l, "demo")
			writeServeTestFile(t, l.RepoConfig("demo"), serveTestContractConfig, 0o600)
			return l.RepoConfig("demo"), `"rust-pingpong"`
		}, false},
		{"symlinked repository config", func(t *testing.T, l serveLayout) (string, string) {
			addServeTestRepo(t, l, "demo")
			target := filepath.Join(t.TempDir(), "config.json")
			writeServeTestFile(t, target, strings.Replace(serveTestContractConfig, "rust-pingpong", "demo", 1), 0o600)
			removeServeTest(t, l.RepoConfig("demo"))
			symlinkServeTest(t, target, l.RepoConfig("demo"))
			return l.RepoConfig("demo"), "symbolic link"
		}, false},
		{"registry mode", func(t *testing.T, l serveLayout) (string, string) {
			if err := saveServeRegistry(l, serveTestRegistry("a")); err != nil {
				t.Fatal(err)
			}
			chmodServeTest(t, l.Registry, 0o640)
			return l.Registry, "mode 0640"
		}, false},
		{"registry unknown field", func(t *testing.T, l serveLayout) (string, string) {
			writeServeTestFile(t, l.Registry, `{"version":1,"keys":[],"extra":1}`, 0o600)
			return l.Registry, "unknown field"
		}, false},
		{"registry too large", func(t *testing.T, l serveLayout) (string, string) {
			writeServeTestFile(t, l.Registry, "", 0o600)
			if err := os.Truncate(l.Registry, 16<<20+1); err != nil {
				t.Fatal(err)
			}
			return l.Registry, "larger than 16777216 bytes"
		}, false},
		{"symlinked registry", func(t *testing.T, l serveLayout) (string, string) {
			target := filepath.Join(t.TempDir(), "registry.json")
			writeServeTestFile(t, target, `{"version":1,"keys":[]}`, 0o600)
			symlinkServeTest(t, target, l.Registry)
			return l.Registry, "symbolic link"
		}, false},
		{"branch owners trailing data", func(t *testing.T, l serveLayout) (string, string) {
			addServeTestRepo(t, l, "demo")
			writeServeTestFile(t, l.RepoBranches("demo"), `{"version":1,"branches":[]}{}`, 0o600)
			return l.RepoBranches("demo"), "trailing"
		}, false},
		{"push log mode", func(t *testing.T, l serveLayout) (string, string) {
			addServeTestRepo(t, l, "demo")
			writeServeTestFile(t, l.RepoPushLog("demo"), "", 0o644)
			return l.RepoPushLog("demo"), "mode 0644"
		}, false},
		{"push log too large", func(t *testing.T, l serveLayout) (string, string) {
			addServeTestRepo(t, l, "demo")
			writeServeTestFile(t, l.RepoPushLog("demo"), "", 0o600)
			if err := os.Truncate(l.RepoPushLog("demo"), 64<<20+1); err != nil {
				t.Fatal(err)
			}
			return l.RepoPushLog("demo"), "larger than 67108864 bytes"
		}, false},
		{"symlinked lock", func(t *testing.T, l serveLayout) (string, string) {
			addServeTestRepo(t, l, "demo")
			symlinkServeTest(t, filepath.Join(t.TempDir(), "lock"), l.RepoLock("demo"))
			return l.RepoLock("demo"), "symbolic link"
		}, false},
		{"lfs lock is a directory", func(t *testing.T, l serveLayout) (string, string) {
			addServeTestRepo(t, l, "demo")
			mkdirServeTest(t, l.RepoLFSLock("demo"), 0o700)
			return l.RepoLFSLock("demo"), "not a regular file"
		}, false},
		{"missing hook script", func(t *testing.T, l serveLayout) (string, string) {
			path := filepath.Join(l.Hooks, "pre-receive")
			removeServeTest(t, path)
			return path, "does not exist"
		}, true},
		{"modified hook script", func(t *testing.T, l serveLayout) (string, string) {
			path := filepath.Join(l.Hooks, "post-receive")
			writeServeTestFile(t, path, "#!/bin/sh\nexit 0\n", 0o700)
			return path, "differs from the generated script"
		}, true},
		{"hook script mode", func(t *testing.T, l serveLayout) (string, string) {
			path := filepath.Join(l.Hooks, "pre-receive")
			chmodServeTest(t, path, 0o755)
			return path, "mode 0755"
		}, true},
		{"symlinked hook script", func(t *testing.T, l serveLayout) (string, string) {
			path := filepath.Join(l.Hooks, "pre-receive")
			target := filepath.Join(t.TempDir(), "pre-receive")
			writeServeTestFile(t, target, string(serveHookScript("pre-receive", serveTestExecutable(t))), 0o700)
			removeServeTest(t, path)
			symlinkServeTest(t, target, path)
			return path, "symbolic link"
		}, true},
		{"unexpected hook script", func(t *testing.T, l serveLayout) (string, string) {
			path := filepath.Join(l.Hooks, "update")
			writeServeTestFile(t, path, "#!/bin/sh\n", 0o700)
			return path, "unexpected entry"
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			layout := newServeTestLayout(t)
			path, reason := tc.plant(t, layout)
			requireServeDefect(t, validateServeLayout(layout, serveSessionValidation), path, reason)
			err := validateServeLayout(layout, serveAdministrationValidation)
			if tc.repaired {
				if err != nil {
					t.Fatalf("administration validation did not repair the hook: %v", err)
				}
				if err := validateServeLayout(layout, serveSessionValidation); err != nil {
					t.Fatalf("session validation after repair: %v", err)
				}
				return
			}
			requireServeDefect(t, err, path, reason)
		})
	}
}

func TestServeStateSessionValidationNeverWrites(t *testing.T) {
	layout := newServeTestLayout(t)
	addServeTestRepo(t, layout, "demo")
	before := snapshotLegacyPrivateTree(t, layout.Home)
	if err := validateServeLayout(layout, serveSessionValidation); err != nil {
		t.Fatal(err)
	}
	if after := snapshotLegacyPrivateTree(t, layout.Home); !reflect.DeepEqual(before, after) {
		t.Fatalf("session validation of a valid layout wrote state:\nbefore %v\nafter  %v", before, after)
	}
	writeServeTestFile(t, filepath.Join(layout.Hooks, "pre-receive"), "#!/bin/sh\n", 0o700)
	before = snapshotLegacyPrivateTree(t, layout.Home)
	if err := validateServeLayout(layout, serveSessionValidation); err == nil {
		t.Fatal("session validation accepted a modified hook")
	}
	if after := snapshotLegacyPrivateTree(t, layout.Home); !reflect.DeepEqual(before, after) {
		t.Fatalf("session validation wrote state:\nbefore %v\nafter  %v", before, after)
	}
}

func TestServeStateSessionRefusesHostileTransientEntries(t *testing.T) {
	subtrees := []struct {
		name string
		dir  func(serveLayout) string
	}{
		{"tmp", func(l serveLayout) string { return l.Tmp }},
		{"lfs incoming", func(l serveLayout) string { return l.RepoLFSIncoming("demo") }},
		{"lfs objects", func(l serveLayout) string { return l.RepoLFSObjects("demo") }},
	}
	plants := []struct {
		name  string
		plant func(t *testing.T, dir string) (path, reason string)
	}{
		{"symlink", func(t *testing.T, dir string) (string, string) {
			path := filepath.Join(dir, "link")
			symlinkServeTest(t, "/etc/passwd", path)
			return path, "symbolic link"
		}},
		{"group-readable file", func(t *testing.T, dir string) (string, string) {
			path := filepath.Join(dir, "staged")
			writeServeTestFile(t, path, "data", 0o640)
			return path, "mode 0640"
		}},
		{"fifo", func(t *testing.T, dir string) (string, string) {
			mkfifo, err := exec.LookPath("mkfifo")
			if err != nil {
				t.Skip("mkfifo is not available")
			}
			path := filepath.Join(dir, "pipe")
			if output, err := exec.Command(mkfifo, "-m", "600", path).CombinedOutput(); err != nil {
				t.Fatalf("mkfifo: %v: %s", err, output)
			}
			return path, "named pipe"
		}},
		{"nested directory mode", func(t *testing.T, dir string) (string, string) {
			path := filepath.Join(dir, "ab")
			mkdirServeTest(t, path, 0o755)
			return path, "mode 0755"
		}},
		{"nested symlink", func(t *testing.T, dir string) (string, string) {
			mkdirServeTest(t, filepath.Join(dir, "ab"), 0o700)
			mkdirServeTest(t, filepath.Join(dir, "ab", "cd"), 0o700)
			path := filepath.Join(dir, "ab", "cd", "abcd")
			symlinkServeTest(t, "/etc/passwd", path)
			return path, "symbolic link"
		}},
	}
	for _, subtree := range subtrees {
		for _, plant := range plants {
			t.Run(subtree.name+"/"+plant.name, func(t *testing.T) {
				layout := newServeTestLayout(t)
				addServeTestRepo(t, layout, "demo")
				addServeTestLFS(t, layout, "demo")
				calls := registerServeProbeRoute(t, "probe-session", serveSessionValidation)
				path, reason := plant.plant(t, subtree.dir(layout))
				requireServeDefect(t, run([]string{"serve", "probe-session"}), path, reason)
				if len(*calls) != 0 {
					t.Fatal("session route ran with a hostile transient entry")
				}
			})
		}
	}
}

func TestServeStateTransientWalkIsBounded(t *testing.T) {
	serveTestHome(t)
	dir := filepath.Join(t.TempDir(), "objects")
	mkdirServeTest(t, dir, 0o700)
	mkdirServeTest(t, filepath.Join(dir, "ab"), 0o700)
	writeServeTestFile(t, filepath.Join(dir, "ab", "one"), "", 0o600)
	writeServeTestFile(t, filepath.Join(dir, "two"), "", 0o600)
	if err := checkServeTransientTree(dir, 3); err != nil {
		t.Fatalf("three entries under a limit of three: %v", err)
	}
	writeServeTestFile(t, filepath.Join(dir, "three"), "", 0o600)
	requireServeDefect(t, checkServeTransientTree(dir, 3), dir, "more than 3 entries")
}

// T003: key registry.

func TestServeStateRegistryRoundTripIsByteStable(t *testing.T) {
	layout := newServeTestLayout(t)
	registry := serveTestRegistry("alice", "bob")
	if err := saveServeRegistry(layout, registry); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(layout.Registry)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loadServeRegistry(layout)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, registry) {
		t.Fatalf("loaded registry = %+v, want %+v", loaded, registry)
	}
	if err := saveServeRegistry(layout, loaded); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(layout.Registry)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) || !strings.HasSuffix(string(first), "}\n") {
		t.Fatalf("registry bytes changed across a round trip:\n%s\n%s", first, second)
	}
	info, err := os.Lstat(layout.Registry)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("registry mode = %v (%v), want 0600", info.Mode(), err)
	}
}

func TestServeStateRegistryContractExampleRoundTrips(t *testing.T) {
	layout := newServeTestLayout(t)
	writeServeTestFile(t, layout.Registry, serveTestContractRegistry, 0o600)
	registry, err := loadServeRegistry(layout)
	if err != nil {
		t.Fatalf("contract example: %v", err)
	}
	if err := saveServeRegistry(layout, registry); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(layout.Registry)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != serveTestContractRegistry {
		t.Fatalf("registry encoding differs from the contract:\n%s", contents)
	}
}

func TestServeStateMissingRegistryIsEmpty(t *testing.T) {
	layout := newServeTestLayout(t)
	registry, err := loadServeRegistry(layout)
	if err != nil {
		t.Fatal(err)
	}
	if registry.Version != 1 || len(registry.Keys) != 0 {
		t.Fatalf("missing registry = %+v, want an empty version 1 registry", registry)
	}
	if _, err := os.Lstat(layout.Registry); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("loading a missing registry created it: %v", err)
	}
}

func TestServeStateRegistryRefusesInvalidContent(t *testing.T) {
	valid := serveTestRegistry("alice", "bob")
	mutate := func(change func(*serveRegistry)) func(t *testing.T) string {
		return func(t *testing.T) string {
			registry := serveRegistry{Version: valid.Version, Keys: append([]serveKeyBinding(nil), valid.Keys...)}
			change(&registry)
			return marshalServeTest(t, registry)
		}
	}
	cases := []struct {
		name     string
		contents func(t *testing.T) string
		fragment string
	}{
		{"unknown field", func(t *testing.T) string {
			return strings.Replace(marshalServeTest(t, valid), `"actor"`, `"role": "maintainer", "actor"`, 1)
		}, "unknown field"},
		{"trailing data", func(t *testing.T) string { return marshalServeTest(t, valid) + "{}" }, "trailing"},
		{"version", mutate(func(r *serveRegistry) { r.Version = 2 }), "version"},
		{"duplicate fingerprint", mutate(func(r *serveRegistry) {
			r.Keys[1].Fingerprint = r.Keys[0].Fingerprint
		}), "duplicate fingerprint"},
		{"duplicate public key", mutate(func(r *serveRegistry) { r.Keys[1].PublicKey = r.Keys[0].PublicKey }), "duplicate public key"},
		{"unsorted keys", mutate(func(r *serveRegistry) { r.Keys[0], r.Keys[1] = r.Keys[1], r.Keys[0] }), "not sorted"},
		{"short actor", mutate(func(r *serveRegistry) { r.Keys[0].Actor = r.Keys[0].Actor[:12] }), "actor"},
		{"uppercase actor", mutate(func(r *serveRegistry) { r.Keys[0].Actor = strings.ToUpper(r.Keys[0].Actor) }), "actor"},
		{"malformed fingerprint", mutate(func(r *serveRegistry) { r.Keys[0].Fingerprint = "MD5:aa:bb" }), "fingerprint"},
		{"public key with comment", mutate(func(r *serveRegistry) { r.Keys[0].PublicKey += " alice@laptop" }), "public key"},
		{"public key with newline", mutate(func(r *serveRegistry) { r.Keys[0].PublicKey += "\nssh-ed25519 AAAA" }), "public key"},
		{"empty public key", mutate(func(r *serveRegistry) { r.Keys[0].PublicKey = "" }), "public key"},
		{"local added time", mutate(func(r *serveRegistry) { r.Keys[0].Added = "2026-10-24T20:00:00+02:00" }), "added"},
		{"malformed added time", mutate(func(r *serveRegistry) { r.Keys[0].Added = "yesterday" }), "added"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			layout := newServeTestLayout(t)
			writeServeTestFile(t, layout.Registry, tc.contents(t), 0o600)
			_, err := loadServeRegistry(layout)
			requireServeErrorContains(t, err, tc.fragment)
			requireServeErrorContains(t, err, layout.Registry)
			requireServeDefect(t, validateServeLayout(layout, serveSessionValidation), layout.Registry, tc.fragment)
		})
	}
	t.Run("file over 16 MiB", func(t *testing.T) {
		layout := newServeTestLayout(t)
		writeServeTestFile(t, layout.Registry, "", 0o600)
		if err := os.Truncate(layout.Registry, 16<<20+1); err != nil {
			t.Fatal(err)
		}
		_, err := loadServeRegistry(layout)
		requireServeErrorContains(t, err, "too large")
	})
	t.Run("save refuses an invalid registry", func(t *testing.T) {
		layout := newServeTestLayout(t)
		invalid := serveTestRegistry("alice", "bob")
		invalid.Keys[0], invalid.Keys[1] = invalid.Keys[1], invalid.Keys[0]
		requireServeErrorContains(t, saveServeRegistry(layout, invalid), "not sorted")
		if _, err := os.Lstat(layout.Registry); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("an invalid registry was written: %v", err)
		}
	})
}

func TestServeStateFingerprintShape(t *testing.T) {
	valid := serveTestFingerprint("alice")
	cases := map[string]bool{
		valid: true,
		"SHA256:Jd8bC8QWw0m0Y4l6m6yZ1gq5q3o9s0Kc2b2f4QwA9eU": true,
		"":                                   false,
		"SHA256:":                            false,
		strings.TrimPrefix(valid, "SHA256:"): false,
		"sha256:" + strings.TrimPrefix(valid, "SHA256:"): false,
		valid[:len(valid)-1]:                             false,
		valid + "A":                                      false,
		valid[:len(valid)-1] + "=":                       false,
		valid[:len(valid)-1] + "V":                       false,
		valid[:len(valid)-1] + "-":                       false,
		valid[:20] + "\n" + valid[21:]:                   false,
		"SHA256:" + strings.Repeat("A", 42) + "_":        false,
		"SHA256:" + base64.RawURLEncoding.EncodeToString(make([]byte, 32)): true,
	}
	for value, want := range cases {
		if got := validServeFingerprint(value); got != want {
			t.Errorf("validServeFingerprint(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestServeStateLookupKey(t *testing.T) {
	registry := serveTestRegistry("alice", "bob", "carol")
	for _, binding := range registry.Keys {
		got, ok := lookupServeKey(registry, binding.Fingerprint)
		if !ok || got != binding {
			t.Fatalf("lookupServeKey(%s) = %+v, %v", binding.Fingerprint, got, ok)
		}
	}
	if _, ok := lookupServeKey(registry, serveTestFingerprint("mallory")); ok {
		t.Fatal("lookupServeKey found an unbound fingerprint")
	}
	if _, ok := lookupServeKey(serveRegistry{Version: 1}, serveTestFingerprint("alice")); ok {
		t.Fatal("lookupServeKey found a key in an empty registry")
	}
}

// T004: repository configuration and branch owners.

func TestServeStateRepoConfigDefaultsMatchContract(t *testing.T) {
	layout := newServeTestLayout(t)
	addServeTestRepo(t, layout, "rust-pingpong")
	contents, err := os.ReadFile(layout.RepoConfig("rust-pingpong"))
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != serveTestContractConfig {
		t.Fatalf("default config differs from the contract:\n%s", contents)
	}
	config, err := loadServeRepoConfig(layout, "rust-pingpong")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(config, newServeRepoConfig("rust-pingpong", nil)) {
		t.Fatalf("loaded config = %+v", config)
	}
	info, err := os.Lstat(layout.RepoConfig("rust-pingpong"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %v (%v), want 0600", info.Mode(), err)
	}
}

func TestServeStateRepoConfigKeepsDefaultBranchFirst(t *testing.T) {
	config := newServeRepoConfig("demo", []string{"refs/heads/trunk", "refs/heads/release", "refs/heads/alpha", "refs/heads/release", "refs/heads/trunk"})
	want := []string{"refs/heads/trunk", "refs/heads/alpha", "refs/heads/release"}
	if !reflect.DeepEqual(config.Protected, want) {
		t.Fatalf("protected = %v, want %v", config.Protected, want)
	}
	if config.LFSQuotaBytes != 1073741824 || config.Budgets != defaultReplicationBudgets() || config.Version != 1 {
		t.Fatalf("defaults = %+v", config)
	}
	layout := newServeTestLayout(t)
	mkdirServeTest(t, layout.RepoStateDir("demo"), 0o700)
	if err := saveServeRepoConfig(layout, config); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(layout.RepoConfig("demo"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(first), `"protected": ["refs/heads/trunk", "refs/heads/alpha", "refs/heads/release"],`) {
		t.Fatalf("protected list encoding:\n%s", first)
	}
	loaded, err := loadServeRepoConfig(layout, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if err := saveServeRepoConfig(layout, loaded); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(layout.RepoConfig("demo"))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("config bytes changed across a round trip:\n%s\n%s", first, second)
	}
}

func TestServeStateRepositoryNameGrammar(t *testing.T) {
	cases := map[string]bool{
		"a":                      true,
		"rust-pingpong":          true,
		"A.b_c-d":                true,
		"0day":                   true,
		"repo.gi":                true,
		strings.Repeat("a", 100): true,
		"":                       false,
		"..":                     false,
		"a..b":                   false,
		"a/b":                    false,
		"x.git":                  false,
		"x.GIT":                  true,
		strings.Repeat("a", 101): false,
		".hidden":                false,
		"-x":                     false,
		"_x":                     false,
		"a b":                    false,
		"a\x00b":                 false,
		"caf\u00e9":              false,
		"a\\b":                   false,
	}
	for name, want := range cases {
		if got := validServeRepositoryName(name); got != want {
			t.Errorf("validServeRepositoryName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestServeStateRepoConfigRefusesInvalidContent(t *testing.T) {
	valid := newServeRepoConfig("demo", []string{"refs/heads/main", "refs/heads/release"})
	mutate := func(change func(*serveRepoConfig)) func(t *testing.T) string {
		return func(t *testing.T) string {
			config := valid
			config.Protected = append([]string(nil), valid.Protected...)
			change(&config)
			return marshalServeTest(t, config)
		}
	}
	cases := []struct {
		name     string
		contents func(t *testing.T) string
		fragment string
	}{
		{"unknown field", func(t *testing.T) string {
			return strings.Replace(marshalServeTest(t, valid), `"name"`, `"public": true, "name"`, 1)
		}, "unknown field"},
		{"unknown budget field", func(t *testing.T) string {
			return strings.Replace(marshalServeTest(t, valid), `"maxEvents"`, `"maxRefs": 1, "maxEvents"`, 1)
		}, "unknown field"},
		{"trailing data", func(t *testing.T) string { return marshalServeTest(t, valid) + "{}" }, "trailing"},
		{"version", mutate(func(c *serveRepoConfig) { c.Version = 0 }), "version"},
		{"name mismatch", mutate(func(c *serveRepoConfig) { c.Name = "other" }), `"other"`},
		{"no protected branch", mutate(func(c *serveRepoConfig) { c.Protected = nil }), "protected"},
		{"duplicate default", mutate(func(c *serveRepoConfig) { c.Protected = append(c.Protected, "refs/heads/main") }), "duplicate protected ref"},
		{"duplicate later entry", mutate(func(c *serveRepoConfig) { c.Protected = append(c.Protected, "refs/heads/release") }), "duplicate protected ref"},
		{"unsorted later entries", mutate(func(c *serveRepoConfig) { c.Protected = append(c.Protected, "refs/heads/beta") }), "sorted"},
		{"zero quota", mutate(func(c *serveRepoConfig) { c.LFSQuotaBytes = 0 }), "lfsQuotaBytes"},
		{"negative quota", mutate(func(c *serveRepoConfig) { c.LFSQuotaBytes = -1 }), "lfsQuotaBytes"},
		{"zero budget", mutate(func(c *serveRepoConfig) { c.Budgets.MaxEvents = 0 }), "max-events"},
		{"negative budget", mutate(func(c *serveRepoConfig) { c.Budgets.MaxTotalBytes = -5 }), "max-total-bytes"},
	}
	for _, ref := range []string{
		"main", "refs/tags/v1", "refs/heads/", "refs/heads/a..b", "refs/heads/a b", "refs/heads/-x",
		"refs/heads/x.lock", "refs/heads/a/x.lock/b", "refs/heads/a//b", "refs/heads/a@{b", "refs/heads/x/",
		"refs/heads//x", "refs/heads/.x", "refs/heads/a/.b", "refs/heads/x.", "refs/heads/a~1", "refs/heads/a^",
		"refs/heads/a:b", "refs/heads/a?", "refs/heads/a*", "refs/heads/a[b", "refs/heads/a\\b",
		"refs/heads/a\tb", "refs/heads/a\x7fb", "refs/heads/@",
	} {
		ref := ref
		cases = append(cases, struct {
			name     string
			contents func(t *testing.T) string
			fragment string
		}{"invalid ref " + ref, mutate(func(c *serveRepoConfig) { c.Protected[1] = ref }), "not a valid branch ref"})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			layout := newServeTestLayout(t)
			addServeTestRepo(t, layout, "demo")
			writeServeTestFile(t, layout.RepoConfig("demo"), tc.contents(t), 0o600)
			_, err := loadServeRepoConfig(layout, "demo")
			requireServeErrorContains(t, err, tc.fragment)
			requireServeDefect(t, validateServeLayout(layout, serveSessionValidation), layout.RepoConfig("demo"), tc.fragment)
		})
	}
	t.Run("load refuses an invalid name", func(t *testing.T) {
		layout := newServeTestLayout(t)
		_, err := loadServeRepoConfig(layout, "../escape")
		requireServeErrorContains(t, err, "repository name")
	})
	t.Run("load refuses a missing config", func(t *testing.T) {
		layout := newServeTestLayout(t)
		_, err := loadServeRepoConfig(layout, "absent")
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("missing config error = %v, want os.ErrNotExist", err)
		}
	})
	t.Run("save refuses an invalid config", func(t *testing.T) {
		layout := newServeTestLayout(t)
		mkdirServeTest(t, layout.RepoStateDir("demo"), 0o700)
		config := newServeRepoConfig("demo", nil)
		config.LFSQuotaBytes = 0
		requireServeErrorContains(t, saveServeRepoConfig(layout, config), "lfsQuotaBytes")
		if _, err := os.Lstat(layout.RepoConfig("demo")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("an invalid config was written: %v", err)
		}
	})
}

func TestServeStateBranchOwnersRoundTrip(t *testing.T) {
	layout := newServeTestLayout(t)
	addServeTestRepo(t, layout, "demo")
	owners, err := loadServeBranchOwners(layout, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if owners.Version != 1 || len(owners.Branches) != 0 {
		t.Fatalf("missing branch owners = %+v, want an empty version 1 record", owners)
	}
	owners.Branches = []serveBranchOwner{
		{Ref: "refs/heads/feature/a", Owner: serveTestActor("alice"), Created: "2026-10-24T20:05:00Z"},
		{Ref: "refs/heads/feature/b", Owner: serveTestActor("bob"), Created: "2026-10-24T20:06:00.5Z"},
	}
	if err := saveServeBranchOwners(layout, "demo", owners); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadServeBranchOwners(layout, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, owners) {
		t.Fatalf("loaded owners = %+v, want %+v", loaded, owners)
	}

	// An emptied list is written as an empty record; the file is kept.
	if err := saveServeBranchOwners(layout, "demo", serveBranchOwners{Version: 1}); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(layout.RepoBranches("demo"))
	if err != nil {
		t.Fatalf("emptied branch owners file: %v", err)
	}
	if string(contents) != "{\n  \"version\": 1,\n  \"branches\": []\n}\n" {
		t.Fatalf("empty branch owners encoding = %q", contents)
	}
	info, err := os.Lstat(layout.RepoBranches("demo"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("branch owners mode = %v (%v), want 0600", info.Mode(), err)
	}
	if err := validateServeLayout(layout, serveSessionValidation); err != nil {
		t.Fatal(err)
	}
}

func TestServeStateBranchOwnersRefuseInvalidContent(t *testing.T) {
	valid := serveBranchOwners{Version: 1, Branches: []serveBranchOwner{
		{Ref: "refs/heads/a", Owner: serveTestActor("alice"), Created: "2026-10-24T20:05:00Z"},
		{Ref: "refs/heads/b", Owner: serveTestActor("bob"), Created: "2026-10-24T20:05:00Z"},
	}}
	mutate := func(change func(*serveBranchOwners)) func(t *testing.T) string {
		return func(t *testing.T) string {
			owners := serveBranchOwners{Version: valid.Version, Branches: append([]serveBranchOwner(nil), valid.Branches...)}
			change(&owners)
			return marshalServeTest(t, owners)
		}
	}
	cases := []struct {
		name     string
		contents func(t *testing.T) string
		fragment string
	}{
		{"unknown field", func(t *testing.T) string {
			return strings.Replace(marshalServeTest(t, valid), `"owner"`, `"protected": true, "owner"`, 1)
		}, "unknown field"},
		{"version", mutate(func(o *serveBranchOwners) { o.Version = 2 }), "version"},
		{"unsorted", mutate(func(o *serveBranchOwners) { o.Branches[0], o.Branches[1] = o.Branches[1], o.Branches[0] }), "not sorted"},
		{"duplicate", mutate(func(o *serveBranchOwners) { o.Branches[1].Ref = o.Branches[0].Ref }), "duplicate"},
		{"tag ref", mutate(func(o *serveBranchOwners) { o.Branches[0].Ref = "refs/tags/a" }), "not a valid branch ref"},
		{"short owner", mutate(func(o *serveBranchOwners) { o.Branches[0].Owner = "abc" }), "owner"},
		{"malformed created", mutate(func(o *serveBranchOwners) { o.Branches[0].Created = "2026-10-24" }), "created"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			layout := newServeTestLayout(t)
			addServeTestRepo(t, layout, "demo")
			writeServeTestFile(t, layout.RepoBranches("demo"), tc.contents(t), 0o600)
			_, err := loadServeBranchOwners(layout, "demo")
			requireServeErrorContains(t, err, tc.fragment)
			requireServeDefect(t, validateServeLayout(layout, serveSessionValidation), layout.RepoBranches("demo"), tc.fragment)
		})
	}
}

// T005: locks and push log.

func TestServeStateLockSerializesHolders(t *testing.T) {
	layout := newServeTestLayout(t)
	addServeTestRepo(t, layout, "demo")
	path := layout.RepoLock("demo")
	release, err := acquireServeLock(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("lock file mode = %v (%v), want a 0600 regular file", info.Mode(), err)
	}

	busy := make(chan error, 1)
	go func() {
		other, err := acquireServeLock(path, 50*time.Millisecond)
		if err == nil {
			_ = other()
		}
		busy <- err
	}()
	if err := <-busy; !errors.Is(err, errServeLockBusy) {
		t.Fatalf("contended lock = %v, want errServeLockBusy", err)
	}

	acquired := make(chan error, 1)
	go func() {
		other, err := acquireServeLock(path, 10*time.Second)
		if err == nil {
			err = other()
		}
		acquired <- err
	}()
	select {
	case err := <-acquired:
		t.Fatalf("second holder acquired the lock while the first held it: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	if err := release(); err != nil {
		t.Fatalf("second release: %v", err)
	}
	select {
	case err := <-acquired:
		if err != nil {
			t.Fatalf("waiting holder after release: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("waiting holder never acquired the released lock")
	}
}

func TestServeStateLockRefusesUnsafePaths(t *testing.T) {
	layout := newServeTestLayout(t)
	addServeTestRepo(t, layout, "demo")
	target := filepath.Join(t.TempDir(), "target")
	symlinkServeTest(t, target, layout.RepoLock("demo"))
	if _, err := acquireServeLock(layout.RepoLock("demo"), 0); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("symlinked lock = %v, want a refusal", err)
	}
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lock followed the symlink and created %s: %v", target, err)
	}
	mkdirServeTest(t, layout.RepoLFSLock("demo"), 0o700)
	if _, err := acquireServeLock(layout.RepoLFSLock("demo"), 0); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("directory lock = %v, want a refusal", err)
	}
	loose := filepath.Join(layout.Tmp, "loose.lock")
	writeServeTestFile(t, loose, "", 0o644)
	if _, err := acquireServeLock(loose, 0); err == nil || !strings.Contains(err.Error(), "mode 0644") {
		t.Fatalf("group-readable lock = %v, want a refusal", err)
	}
}

func TestServeStateLockWait(t *testing.T) {
	serveTestHome(t)
	if got := serveLockWait(); got != 120*time.Second {
		t.Fatalf("default lock wait = %v, want 120s", got)
	}
	t.Setenv("HN_TEST_SERVE_LOCK_WAIT", "250ms")
	if got := serveLockWait(); got != 120*time.Second {
		t.Fatalf("lock wait without the testing gate = %v, want 120s", got)
	}
	t.Setenv("HN_INTERNAL_TESTING", "1")
	if got := serveLockWait(); got != 250*time.Millisecond {
		t.Fatalf("testing lock wait = %v, want 250ms", got)
	}
	for _, value := range []string{"soon", "-1s", ""} {
		t.Setenv("HN_TEST_SERVE_LOCK_WAIT", value)
		if got := serveLockWait(); got != 120*time.Second {
			t.Fatalf("malformed testing lock wait %q = %v, want the 120s default", value, got)
		}
	}
}

func TestServeStatePushLogAppendsOrderedLines(t *testing.T) {
	layout := newServeTestLayout(t)
	addServeTestRepo(t, layout, "demo")
	oldID := strings.Repeat("1", 40)
	newID := strings.Repeat("2", 40)
	first := servePushLogEntry{
		Time:    "2026-10-24T20:06:00Z",
		Actor:   serveTestActor("alice"),
		Key:     serveTestFingerprint("alice"),
		Verdict: "rejected",
		Updates: []servePushLogUpdate{{Ref: "refs/heads/main", Old: oldID, New: newID}},
		Codes:   []string{"protected-no-proposal"},
	}
	second := servePushLogEntry{Time: "2026-10-24T20:07:00Z", Actor: serveTestActor("bob"), Key: serveTestFingerprint("bob"), Verdict: "accepted"}
	for _, entry := range []servePushLogEntry{first, second} {
		if err := appendServePushLog(layout, "demo", entry); err != nil {
			t.Fatal(err)
		}
	}
	contents, err := os.ReadFile(layout.RepoPushLog("demo"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(contents), "\n"), "\n")
	if len(lines) != 2 || !strings.HasSuffix(string(contents), "\n") {
		t.Fatalf("push log = %q, want two newline-terminated lines", contents)
	}
	wantFirst := `{"time":"2026-10-24T20:06:00Z","actor":"` + first.Actor + `","key":"` + first.Key +
		`","verdict":"rejected","updates":[{"ref":"refs/heads/main","old":"` + oldID + `","new":"` + newID +
		`"}],"codes":["protected-no-proposal"]}`
	if lines[0] != wantFirst {
		t.Fatalf("first line = %s\nwant         %s", lines[0], wantFirst)
	}
	if !strings.Contains(lines[1], `"verdict":"accepted","updates":[],"codes":[]`) {
		t.Fatalf("second line = %s, want empty lists", lines[1])
	}
	var decoded servePushLogEntry
	if err := json.Unmarshal([]byte(lines[1]), &decoded); err != nil || decoded.Actor != second.Actor {
		t.Fatalf("second line decode = %+v (%v)", decoded, err)
	}
	info, err := os.Lstat(layout.RepoPushLog("demo"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("push log mode = %v (%v), want 0600", info.Mode(), err)
	}
}

func TestServeStatePushLogRefusesUnsafeAppends(t *testing.T) {
	layout := newServeTestLayout(t)
	addServeTestRepo(t, layout, "demo")
	entry := servePushLogEntry{Time: "2026-10-24T20:06:00Z", Actor: serveTestActor("alice"), Key: serveTestFingerprint("alice"), Verdict: "maybe"}
	requireServeErrorContains(t, appendServePushLog(layout, "demo", entry), "verdict")
	entry.Verdict = "accepted"
	requireServeErrorContains(t, appendServePushLog(layout, "../demo", entry), "repository name")
	target := filepath.Join(t.TempDir(), "elsewhere.log")
	symlinkServeTest(t, target, layout.RepoPushLog("demo"))
	requireServeErrorContains(t, appendServePushLog(layout, "demo", entry), "symbolic link")
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("push log append followed a symlink: %v", err)
	}
}

// T006: hook scripts.

func TestServeStateHookScriptBytes(t *testing.T) {
	got := string(serveHookScript("pre-receive", "/usr/local/bin/hn"))
	if got != "#!/bin/sh\nexec /usr/local/bin/hn serve hook pre-receive\n" {
		t.Fatalf("pre-receive script = %q", got)
	}
	got = string(serveHookScript("post-receive", "/opt/hn/bin/hn"))
	if got != "#!/bin/sh\nexec /opt/hn/bin/hn serve hook post-receive\n" {
		t.Fatalf("post-receive script = %q", got)
	}
}

func TestServeStateEnsureHooksWritesExecutableScripts(t *testing.T) {
	layout := newServeTestLayout(t)
	hnPath := "/usr/local/bin/hn"
	if err := ensureServeHooks(layout, hnPath); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(layout.Hooks)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("hooks directory holds %d entries, want 2", len(entries))
	}
	for _, name := range []string{"pre-receive", "post-receive"} {
		path := filepath.Join(layout.Hooks, name)
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o700 {
			t.Fatalf("%s mode = %v, want a 0700 regular file", name, info.Mode())
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(contents) != string(serveHookScript(name, hnPath)) {
			t.Fatalf("%s = %q", name, contents)
		}
	}
	// Hooks naming another hn binary are a session defect until an
	// administration command regenerates them for the running binary.
	requireServeDefect(t, validateServeLayout(layout, serveSessionValidation), filepath.Join(layout.Hooks, "post-receive"), "differs")
	if err := validateServeLayout(layout, serveAdministrationValidation); err != nil {
		t.Fatal(err)
	}
	if err := validateServeLayout(layout, serveSessionValidation); err != nil {
		t.Fatal(err)
	}
}

func TestServeStateEnsureHooksRefusesUnsafeTargets(t *testing.T) {
	layout := newServeTestLayout(t)
	requireServeErrorContains(t, ensureServeHooks(layout, "/usr/local/bin/my hn"), "whitespace")
	elsewhere := t.TempDir()
	removeServeTest(t, layout.Hooks)
	symlinkServeTest(t, elsewhere, layout.Hooks)
	requireServeDefect(t, ensureServeHooks(layout, "/usr/local/bin/hn"), layout.Hooks, "symbolic link")
	if entries, err := os.ReadDir(elsewhere); err != nil || len(entries) != 0 {
		t.Fatalf("ensureServeHooks wrote through a symlinked hooks directory: %v (%v)", entries, err)
	}
}

func TestServeStateExecutablePathRules(t *testing.T) {
	for _, path := range []string{
		`/usr/local/bin/h"n`, "/usr/local/bin/h'n", `/usr/local/bin/h\n`, "/usr/local/bin/h n",
		"/usr/local/bin/h\tn", "/usr/local/bin/h\nn", "/usr/local/bin/h\u00a0n", "relative/hn",
	} {
		if _, err := checkServeExecutablePath(path); err == nil {
			t.Errorf("checkServeExecutablePath(%q) accepted an unsafe path", path)
		}
	}
	if got, err := checkServeExecutablePath("/usr/local/bin/hn-0.1_x"); err != nil || got != "/usr/local/bin/hn-0.1_x" {
		t.Fatalf("safe path = %q, %v", got, err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	path, err := serveExecutablePath()
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(path) {
		t.Fatalf("serveExecutablePath = %q, want an absolute path", path)
	}
	if resolved, err := filepath.EvalSymlinks(path); err != nil || resolved != path {
		t.Fatalf("serveExecutablePath = %q, want the symlink-resolved path %q (%v)", path, resolved, err)
	}
}
