package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
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

// serveTestConfig is the contract's default config.json for name.
func serveTestConfig(name string) string {
	return strings.Replace(serveTestContractConfig, `"name": "rust-pingpong"`, `"name": "`+name+`"`, 1)
}

func addServeTestRepo(t *testing.T, layout serveLayout, name string) {
	t.Helper()
	mkdirServeTest(t, layout.RepoGitDir(name), 0o700)
	mkdirServeTest(t, layout.RepoStateDir(name), 0o700)
	writeServeTestFile(t, layout.RepoConfig(name), serveTestConfig(name), 0o600)
}

func writeServeTestRegistry(t *testing.T, layout serveLayout, registry serveRegistry) {
	t.Helper()
	writeServeTestFile(t, layout.Registry, marshalServeTest(t, registry), 0o600)
}

// runServeTest runs hn in-process and returns its standard output.
func runServeTest(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var err error
	output := captureStdout(t, func() { err = run(args) })
	return output, err
}

func requireServeTestOutput(t *testing.T, want string, args ...string) {
	t.Helper()
	output, err := runServeTest(t, args...)
	if err != nil {
		t.Fatalf("hn %s: %v", strings.Join(args, " "), err)
	}
	if output != want {
		t.Fatalf("hn %s output:\n%s\nwant:\n%s", strings.Join(args, " "), output, want)
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

func serveTestFingerprint(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

func serveStateTestActor(seed string) string {
	sum := sha256.Sum256([]byte("actor:" + seed))
	return hex.EncodeToString(sum[:])
}

func serveTestBinding(seed string) serveKeyBinding {
	return serveKeyBinding{
		Fingerprint: serveTestFingerprint(seed),
		PublicKey:   "ssh-ed25519 " + base64.StdEncoding.EncodeToString([]byte("key:"+seed)),
		Actor:       serveStateTestActor(seed),
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
	t.Setenv("HN_INTERNAL_TESTING", "1")
	t.Setenv("HN_TEST_SERVE_EUID", "0")
	before := snapshotLegacyPrivateTree(t, home)

	invocations := [][]string{
		{"serve"},
		{"serve", "repo"},
		{"serve", "repo", "list"},
		{"serve", "repo", "show", "demo"},
		{"serve", "repo", "lock-exec", "demo", "--", "true"},
		{"serve", "key"},
		{"serve", "key", "list"},
		{"serve", "key", "add", "--actor", serveStateTestActor("a"), "-"},
		{"serve", "git", "--key", serveTestFingerprint("a")},
		{"serve", "hook", "pre-receive"},
		{"serve", "bogus"},
	}
	for _, args := range invocations {
		output, err := runServeTest(t, args...)
		if err == nil || err.Error() != "hn serve refuses to run as root; create an unprivileged user for the server" {
			t.Fatalf("run(%q) = %v, want the root refusal", args, err)
		}
		if output != "" {
			t.Fatalf("run(%q) printed %q before refusing", args, output)
		}
	}
	t.Setenv("HOME", "")
	if err := run([]string{"serve", "repo", "list"}); err == nil || !strings.Contains(err.Error(), "refuses to run as root") {
		t.Fatalf("root refusal must come before HOME is resolved, got %v", err)
	}
	t.Setenv("HOME", home)

	if after := snapshotLegacyPrivateTree(t, home); !reflect.DeepEqual(before, after) {
		t.Fatalf("root refusal changed HOME:\nbefore %v\nafter  %v", before, after)
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

// The administration routes are the live hn serve commands in this work
// package; each one prepares and validates the layout before it runs.
func TestServeStateAdministrationCommandsPrepareAndValidate(t *testing.T) {
	home := serveTestHome(t)
	root := filepath.Join(home, ".hn-serve")

	requireServeTestOutput(t, "", "serve", "repo", "list")
	for _, directory := range []string{"", "hooks", "repositories", "state", "state/tmp", "state/repos"} {
		path := filepath.Join(root, directory)
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Fatalf("hn serve repo list did not create %s as a 0700 directory: %v (%v)", path, info, err)
		}
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
	requireServeTestOutput(t, "", "serve", "key", "list")

	// A stale hook is repaired by any administration command.
	writeServeTestFile(t, filepath.Join(root, "hooks", "pre-receive"), "#!/bin/sh\nexit 0\n", 0o700)
	requireServeTestOutput(t, "", "serve", "key", "list")
	if contents, err := os.ReadFile(filepath.Join(root, "hooks", "pre-receive")); err != nil || string(contents) != string(serveHookScript("pre-receive", hnPath)) {
		t.Fatalf("administration command did not regenerate the hook: %q (%v)", contents, err)
	}

	stray := filepath.Join(root, "state", "stray")
	writeServeTestFile(t, stray, "x", 0o600)
	for _, args := range [][]string{
		{"serve", "repo", "list"},
		{"serve", "repo", "show", "demo"},
		{"serve", "repo", "lock-exec", "demo", "--", "true"},
		{"serve", "repo", "bogus"},
		{"serve", "key", "list"},
	} {
		output, err := runServeTest(t, args...)
		requireServeDefect(t, err, stray, "unexpected entry")
		if output != "" {
			t.Fatalf("hn %s printed %q on a defective layout", strings.Join(args, " "), output)
		}
	}
	removeServeTest(t, stray)

	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"serve", "repo"}, "usage: hn serve repo list|show|lock-exec ..."},
		{[]string{"serve", "repo", "bogus"}, `unknown hn serve repo subcommand "bogus"; run 'hn help'`},
		{[]string{"serve", "repo", "list", "extra"}, "usage: hn serve repo list"},
		{[]string{"serve", "repo", "show"}, "usage: hn serve repo show NAME"},
		{[]string{"serve", "repo", "show", "../x"}, `invalid hosted repository name "../x"`},
		{[]string{"serve", "repo", "show", "absent"}, "repository absent is not hosted; run 'hn serve repo list'"},
		{[]string{"serve", "repo", "lock-exec", "absent", "true"}, "usage: hn serve repo lock-exec NAME -- COMMAND [ARG]..."},
		{[]string{"serve", "repo", "lock-exec", "absent", "--"}, "usage: hn serve repo lock-exec NAME -- COMMAND [ARG]..."},
		{[]string{"serve", "repo", "lock-exec", "absent", "--", "true"}, "repository absent is not hosted; run 'hn serve repo list'"},
		{[]string{"serve", "key"}, "usage: hn serve key list"},
		{[]string{"serve", "key", "list", "extra"}, "usage: hn serve key list"},
		{[]string{"serve", "key", "bogus"}, `unknown hn serve key subcommand "bogus"; run 'hn help'`},
	} {
		_, err := runServeTest(t, tc.args...)
		if err == nil || err.Error() != tc.want {
			t.Fatalf("hn %s = %v, want %q", strings.Join(tc.args, " "), err, tc.want)
		}
	}
}

func TestServeStateSessionValidationRefusesMissingLayoutWithoutWriting(t *testing.T) {
	home := serveTestHome(t)
	layout, err := resolveServeLayout()
	if err != nil {
		t.Fatal(err)
	}
	before := snapshotLegacyPrivateTree(t, home)
	requireServeDefect(t, prepareServeLayout(layout, serveSessionValidation), layout.Root, "does not exist")
	if after := snapshotLegacyPrivateTree(t, home); !reflect.DeepEqual(before, after) {
		t.Fatalf("session validation wrote to HOME:\nbefore %v\nafter  %v", before, after)
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
	writeServeTestRegistry(t, layout, serveTestRegistry("a", "b"))
	owners := serveBranchOwners{Version: 1, Branches: []serveBranchOwner{{Ref: "refs/heads/feature/a", Owner: serveStateTestActor("a"), Created: "2026-10-24T20:05:00Z"}}}
	writeServeTestFile(t, layout.RepoBranches("alpha"), marshalServeTest(t, owners), 0o600)
	writeServeTestFile(t, layout.RepoBranches("beta.v2"), "{\n  \"version\": 1,\n  \"branches\": []\n}\n", 0o600)
	writeServeTestFile(t, layout.RepoPushLog("alpha"), `{"time":"2026-10-24T20:06:00Z","verdict":"accepted"}`+"\n", 0o600)
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
			writeServeTestFile(t, l.RepoConfig("demo"), serveTestConfig("demo"), 0o600)
			return l.RepoStateDir("demo"), "no matching repository"
		}, false},
		{"group-readable hosted repository directory", func(t *testing.T, l serveLayout) (string, string) {
			addServeTestRepo(t, l, "demo")
			chmodServeTest(t, l.RepoGitDir("demo"), 0o755)
			return l.RepoGitDir("demo"), "directory mode 0755, want 0700"
		}, false},
		{"world-writable hosted repository directory", func(t *testing.T, l serveLayout) (string, string) {
			addServeTestRepo(t, l, "demo")
			chmodServeTest(t, l.RepoGitDir("demo"), 0o777)
			return l.RepoGitDir("demo"), "directory mode 0777, want 0700"
		}, false},
		{"group-only hosted repository directory", func(t *testing.T, l serveLayout) (string, string) {
			addServeTestRepo(t, l, "demo")
			chmodServeTest(t, l.RepoGitDir("demo"), 0o750)
			return l.RepoGitDir("demo"), "directory mode 0750, want 0700"
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
			writeServeTestRegistry(t, l, serveTestRegistry("a"))
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
	// ensureServeLayout recreates a missing fixed directory, so the
	// administration command repairs this defect before validating.
	ensureRepairs := map[string]bool{"missing tmp directory": true}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			layout := newServeTestLayout(t)
			path, reason := tc.plant(t, layout)
			before := snapshotLegacyPrivateTree(t, layout.Home)
			requireServeDefect(t, validateServeLayout(layout, serveSessionValidation), path, reason)
			if after := snapshotLegacyPrivateTree(t, layout.Home); !reflect.DeepEqual(before, after) {
				t.Fatalf("session validation changed the fixture:\nbefore %v\nafter  %v", before, after)
			}
			if !tc.repaired && !ensureRepairs[tc.name] {
				output, err := runServeTest(t, "serve", "repo", "list")
				requireServeDefect(t, err, path, reason)
				if output != "" {
					t.Fatalf("hn serve repo list printed %q on a defective layout", output)
				}
			}
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

func TestServeStateCommandsRefuseHostileTransientEntries(t *testing.T) {
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
				path, reason := plant.plant(t, subtree.dir(layout))
				before := snapshotLegacyPrivateTree(t, layout.Home)
				requireServeDefect(t, validateServeLayout(layout, serveSessionValidation), path, reason)
				if after := snapshotLegacyPrivateTree(t, layout.Home); !reflect.DeepEqual(before, after) {
					t.Fatalf("session validation changed the fixture:\nbefore %v\nafter  %v", before, after)
				}
				for _, args := range [][]string{
					{"serve", "repo", "list"},
					{"serve", "repo", "show", "demo"},
					{"serve", "repo", "lock-exec", "demo", "--", "true"},
					{"serve", "key", "list"},
				} {
					output, err := runServeTest(t, args...)
					requireServeDefect(t, err, path, reason)
					if output != "" {
						t.Fatalf("hn %s printed %q with a hostile transient entry", strings.Join(args, " "), output)
					}
				}
				if _, err := os.Lstat(layout.RepoLock("demo")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("lock-exec took the lock on a defective layout: %v", err)
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

func TestServeStateRegistryContractExampleDecodes(t *testing.T) {
	layout := newServeTestLayout(t)
	writeServeTestFile(t, layout.Registry, serveTestContractRegistry, 0o600)
	registry, err := loadServeRegistry(layout)
	if err != nil {
		t.Fatalf("contract example: %v", err)
	}
	want := serveRegistry{Version: 1, Keys: []serveKeyBinding{{
		Fingerprint: "SHA256:Jd8bC8QWw0m0Y4l6m6yZ1gq5q3o9s0Kc2b2f4QwA9eU",
		PublicKey:   "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExampleOnlyNotARealKeyxxxxxxxxxxxxxxxxxxxxx",
		Actor:       "4f1c9a6d0d2b7e3a8c5f6e1b9d0a7c3e2f4b6d8a0c1e3f5a7b9d1c3e5f7a9b1c",
		Added:       "2026-10-24T20:00:00Z",
	}}}
	if !reflect.DeepEqual(registry, want) {
		t.Fatalf("contract registry = %+v, want %+v", registry, want)
	}
	if err := validateServeLayout(layout, serveSessionValidation); err != nil {
		t.Fatal(err)
	}
}

func TestServeStateKeyListPrintsBindings(t *testing.T) {
	layout := newServeTestLayout(t)
	registry := serveTestRegistry("alice", "bob", "carol")
	registry.Keys[1].PublicKey = "ecdsa-sha2-nistp256 " + base64.StdEncoding.EncodeToString([]byte("key:p256"))
	writeServeTestRegistry(t, layout, registry)
	want := ""
	for index, binding := range registry.Keys {
		keyType := "ssh-ed25519"
		if index == 1 {
			keyType = "ecdsa-sha2-nistp256"
		}
		want += binding.Fingerprint + " " + binding.Actor + " " + keyType + "\n"
	}
	requireServeTestOutput(t, want, "serve", "key", "list")

	// The registry is read only after validation; a malformed one refuses.
	writeServeTestFile(t, layout.Registry, `{"version":1,"keys":[],"extra":1}`, 0o600)
	output, err := runServeTest(t, "serve", "key", "list")
	requireServeDefect(t, err, layout.Registry, "unknown field")
	if output != "" {
		t.Fatalf("key list printed %q for a malformed registry", output)
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

// T004: repository configuration and branch owners.

func TestServeStateRepoConfigContractExampleDecodes(t *testing.T) {
	layout := newServeTestLayout(t)
	mkdirServeTest(t, layout.RepoGitDir("rust-pingpong"), 0o700)
	mkdirServeTest(t, layout.RepoStateDir("rust-pingpong"), 0o700)
	writeServeTestFile(t, layout.RepoConfig("rust-pingpong"), serveTestContractConfig, 0o600)
	config, err := loadServeRepoConfig(layout, "rust-pingpong")
	if err != nil {
		t.Fatal(err)
	}
	want := serveRepoConfig{
		Version:       1,
		Name:          "rust-pingpong",
		Protected:     []string{"refs/heads/main"},
		LFSQuotaBytes: 1073741824,
		Budgets:       defaultReplicationBudgets(),
	}
	if !reflect.DeepEqual(config, want) {
		t.Fatalf("contract config = %+v, want %+v", config, want)
	}
	requireServeTestOutput(t, "name: rust-pingpong\n"+
		"path: "+layout.RepoGitDir("rust-pingpong")+"\n"+
		"protected: refs/heads/main\n"+
		"lfs-quota: 1073741824\n"+
		"lfs-stored: 0\n"+
		"max-events: 10000\n"+
		"max-objects: 100000\n"+
		"max-object-bytes: 67108864\n"+
		"max-attachment-bytes: 16777216\n"+
		"max-total-bytes: 1073741824\n",
		"serve", "repo", "show", "rust-pingpong")
}

func TestServeStateRepoConfigKeepsDefaultBranchFirst(t *testing.T) {
	layout := newServeTestLayout(t)
	addServeTestRepo(t, layout, "demo")
	ordered := strings.Replace(serveTestConfig("demo"), `"protected": ["refs/heads/main"]`,
		`"protected": ["refs/heads/trunk", "refs/heads/alpha", "refs/heads/release"]`, 1)
	writeServeTestFile(t, layout.RepoConfig("demo"), ordered, 0o600)
	config, err := loadServeRepoConfig(layout, "demo")
	if err != nil {
		t.Fatalf("default-first protected list: %v", err)
	}
	want := []string{"refs/heads/trunk", "refs/heads/alpha", "refs/heads/release"}
	if !reflect.DeepEqual(config.Protected, want) {
		t.Fatalf("protected = %v, want %v", config.Protected, want)
	}
	output, err := runServeTest(t, "serve", "repo", "show", "demo")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "protected: refs/heads/trunk\nprotected: refs/heads/alpha\nprotected: refs/heads/release\n") {
		t.Fatalf("repo show does not keep the default branch first:\n%s", output)
	}
}

func TestServeStateRepoListAndShow(t *testing.T) {
	layout := newServeTestLayout(t)
	for _, name := range []string{"beta", "Alpha", "alpha-2", "0day"} {
		addServeTestRepo(t, layout, name)
	}
	requireServeTestOutput(t, "0day\nAlpha\nalpha-2\nbeta\n", "serve", "repo", "list")

	addServeTestLFS(t, layout, "beta")
	objects := filepath.Join(layout.RepoLFSObjects("beta"), "ab", "cd")
	mkdirServeTest(t, filepath.Dir(objects), 0o700)
	mkdirServeTest(t, objects, 0o700)
	writeServeTestFile(t, filepath.Join(objects, "abcd"+strings.Repeat("1", 60)), "12345", 0o600)
	writeServeTestFile(t, filepath.Join(objects, "abcd"+strings.Repeat("2", 60)), "1234567", 0o600)
	writeServeTestFile(t, filepath.Join(layout.RepoLFSIncoming("beta"), ".staged"), "not counted", 0o600)
	output, err := runServeTest(t, "serve", "repo", "show", "beta")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "\nlfs-quota: 1073741824\nlfs-stored: 12\n") {
		t.Fatalf("repo show lfs-stored:\n%s", output)
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
	valid := serveRepoConfig{
		Version:       1,
		Name:          "demo",
		Protected:     []string{"refs/heads/main", "refs/heads/release"},
		LFSQuotaBytes: 1073741824,
		Budgets:       defaultReplicationBudgets(),
	}
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
}

func TestServeStateBranchOwnersDecode(t *testing.T) {
	owners := serveBranchOwners{Version: 1, Branches: []serveBranchOwner{
		{Ref: "refs/heads/feature/a", Owner: serveStateTestActor("alice"), Created: "2026-10-24T20:05:00Z"},
		{Ref: "refs/heads/feature/b", Owner: serveStateTestActor("bob"), Created: "2026-10-24T20:06:00.5Z"},
	}}
	decoded, err := decodeServeBranchOwners([]byte(marshalServeTest(t, owners)))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, owners) {
		t.Fatalf("decoded owners = %+v, want %+v", decoded, owners)
	}
	empty, err := decodeServeBranchOwners([]byte(`{"version":1,"branches":[]}`))
	if err != nil || empty.Branches == nil || len(empty.Branches) != 0 {
		t.Fatalf("empty branch owners = %+v (%v), want an empty list", empty, err)
	}

	layout := newServeTestLayout(t)
	addServeTestRepo(t, layout, "demo")
	writeServeTestFile(t, layout.RepoBranches("demo"), marshalServeTest(t, owners), 0o600)
	if err := validateServeLayout(layout, serveSessionValidation); err != nil {
		t.Fatal(err)
	}
	requireServeTestOutput(t, "demo\n", "serve", "repo", "list")
}

func TestServeStateBranchOwnersRefuseInvalidContent(t *testing.T) {
	valid := serveBranchOwners{Version: 1, Branches: []serveBranchOwner{
		{Ref: "refs/heads/a", Owner: serveStateTestActor("alice"), Created: "2026-10-24T20:05:00Z"},
		{Ref: "refs/heads/b", Owner: serveStateTestActor("bob"), Created: "2026-10-24T20:05:00Z"},
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
			_, err := decodeServeBranchOwners([]byte(tc.contents(t)))
			requireServeErrorContains(t, err, tc.fragment)
			requireServeDefect(t, validateServeLayout(layout, serveSessionValidation), layout.RepoBranches("demo"), tc.fragment)
			_, err = runServeTest(t, "serve", "repo", "list")
			requireServeDefect(t, err, layout.RepoBranches("demo"), tc.fragment)
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

func TestServeStateLockExecRunsInRepository(t *testing.T) {
	layout := newServeTestLayout(t)
	addServeTestRepo(t, layout, "demo")
	t.Setenv("GIT_DIR", "/elsewhere")
	gitDir := layout.RepoGitDir("demo")
	physical, err := filepath.EvalSymlinks(gitDir)
	if err != nil {
		t.Fatal(err)
	}
	requireServeTestOutput(t, physical+"\n"+gitDir+"\n",
		"serve", "repo", "lock-exec", "demo", "--", "sh", "-c", `pwd -P; printf '%s\n' "$GIT_DIR"`)
	info, err := os.Lstat(layout.RepoLock("demo"))
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("lock-exec lock file = %v (%v), want a 0600 regular file", info, err)
	}
	release, err := acquireServeLock(layout.RepoLock("demo"), 0)
	if err != nil {
		t.Fatalf("lock-exec did not release the repository lock: %v", err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	if err := validateServeLayout(layout, serveSessionValidation); err != nil {
		t.Fatalf("layout after lock-exec: %v", err)
	}

	_, err = runServeTest(t, "serve", "repo", "lock-exec", "demo", "--", filepath.Join(layout.Home, "missing-command"))
	requireServeErrorContains(t, err, "cannot run")
	_, err = runServeTest(t, "serve", "repo", "lock-exec", "demo", "--", "sh", "-c", "kill -TERM $$")
	requireServeErrorContains(t, err, "did not exit normally")
}

func TestServeStateLockExecRefusesWhileLocked(t *testing.T) {
	layout := newServeTestLayout(t)
	addServeTestRepo(t, layout, "demo")
	release, err := acquireServeLock(layout.RepoLock("demo"), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	t.Setenv("HN_INTERNAL_TESTING", "1")
	t.Setenv("HN_TEST_SERVE_LOCK_WAIT", "100ms")
	marker := filepath.Join(layout.Home, "ran")
	output, err := runServeTest(t, "serve", "repo", "lock-exec", "demo", "--", "touch", marker)
	requireServeErrorContains(t, err, "repository-busy")
	requireServeErrorContains(t, err, "demo")
	if output != "" {
		t.Fatalf("busy lock-exec printed %q", output)
	}
	if _, err := os.Lstat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lock-exec ran its command without the lock: %v", err)
	}
}

// Black-box checks through the built hn binary, including behavior that
// in-process tests cannot observe: lock-exec's exit status passthrough and a
// lock held by another process.
func TestServeStateBinaryCommands(t *testing.T) {
	serveTestHome(t)
	binary := buildOperationalBinary(t)
	if _, err := checkServeExecutablePath(binary); err != nil {
		t.Skipf("temporary directory is not usable as an hn install path: %v", err)
	}
	newHome := func(t *testing.T) string {
		home := filepath.Join(t.TempDir(), "home")
		mkdirServeTest(t, home, 0o700)
		return home
	}
	runHN := func(t *testing.T, home string, extra []string, args ...string) (string, int) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, binary, args...)
		command.Dir = home
		command.Env = append([]string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "LANG=C"}, extra...)
		output, err := command.CombinedOutput()
		if ctx.Err() != nil {
			t.Fatalf("hn %s timed out: %s", strings.Join(args, " "), output)
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return string(output), exitErr.ExitCode()
		}
		if err != nil {
			t.Fatalf("hn %s: %v", strings.Join(args, " "), err)
		}
		return string(output), 0
	}
	addRepo := func(t *testing.T, home string) serveLayout {
		t.Helper()
		if output, status := runHN(t, home, nil, "serve", "repo", "list"); status != 0 || output != "" {
			t.Fatalf("hn serve repo list on a fresh home = %d %q", status, output)
		}
		layout := serveLayout{Home: home, Root: filepath.Join(home, ".hn-serve")}
		layout.Repositories = filepath.Join(layout.Root, "repositories")
		layout.Repos = filepath.Join(layout.Root, "state", "repos")
		addServeTestRepo(t, layout, "demo")
		return layout
	}

	t.Run("symlinked server root", func(t *testing.T) {
		home := newHome(t)
		target := filepath.Join(home, "missing-target")
		symlinkServeTest(t, target, filepath.Join(home, ".hn-serve"))
		want := "hn: hn serve state defect: " + filepath.Join(home, ".hn-serve") + ": is a symbolic link\n"
		for _, args := range [][]string{{"serve", "repo", "list"}, {"serve", "key", "list"}} {
			output, status := runHN(t, home, []string{"HN_INTERNAL_TESTING=0"}, args...)
			if status != 1 || output != want {
				t.Fatalf("hn %s = %d %q, want 1 %q", strings.Join(args, " "), status, output, want)
			}
		}
		if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("hn serve wrote through the symlinked root: %v", err)
		}
	})

	t.Run("hosted repository directory mode", func(t *testing.T) {
		home := newHome(t)
		layout := addRepo(t, home)
		chmodServeTest(t, layout.RepoGitDir("demo"), 0o755)
		output, status := runHN(t, home, nil, "serve", "repo", "list")
		want := "hn: hn serve state defect: " + layout.RepoGitDir("demo") + ": directory mode 0755, want 0700\n"
		if status != 1 || output != want {
			t.Fatalf("hn serve repo list = %d %q, want 1 %q", status, output, want)
		}
	})

	t.Run("root refusal", func(t *testing.T) {
		home := newHome(t)
		before := snapshotLegacyPrivateTree(t, home)
		for _, args := range [][]string{{"serve"}, {"serve", "repo", "list"}, {"serve", "key", "list"}, {"serve", "git", "--key", "x"}} {
			output, status := runHN(t, home, []string{"HN_INTERNAL_TESTING=1", "HN_TEST_SERVE_EUID=0"}, args...)
			if status != 1 || output != "hn: "+serveRootRefusal+"\n" {
				t.Fatalf("hn %s as root = %d %q", strings.Join(args, " "), status, output)
			}
		}
		if after := snapshotLegacyPrivateTree(t, home); !reflect.DeepEqual(before, after) {
			t.Fatalf("root refusal changed HOME:\nbefore %v\nafter  %v", before, after)
		}
		output, status := runHN(t, home, []string{"HN_TEST_SERVE_EUID=0"}, "serve")
		if status != 1 || output != "hn: usage: hn serve repo|key|git|hook ...\n" {
			t.Fatalf("ungated EUID override = %d %q, want the usage error", status, output)
		}
	})

	t.Run("generated hooks name the binary", func(t *testing.T) {
		home := newHome(t)
		addRepo(t, home)
		contents, err := os.ReadFile(filepath.Join(home, ".hn-serve", "hooks", "pre-receive"))
		if err != nil {
			t.Fatal(err)
		}
		if string(contents) != "#!/bin/sh\nexec "+binary+" serve hook pre-receive\n" {
			t.Fatalf("pre-receive = %q", contents)
		}
	})

	t.Run("lock-exec passes the exit status through", func(t *testing.T) {
		home := newHome(t)
		layout := addRepo(t, home)
		output, status := runHN(t, home, nil, "serve", "repo", "lock-exec", "demo", "--", "sh", "-c", "echo held; exit 7")
		if status != 7 || output != "held\n" {
			t.Fatalf("lock-exec exit = %d %q, want 7 %q", status, output, "held\n")
		}
		release, err := acquireServeLock(layout.RepoLock("demo"), 0)
		if err != nil {
			t.Fatalf("lock-exec left the lock held after exiting 7: %v", err)
		}
		_ = release()
		if output, status := runHN(t, home, nil, "serve", "repo", "lock-exec", "demo", "--", "true"); status != 0 || output != "" {
			t.Fatalf("lock-exec true = %d %q", status, output)
		}
	})

	t.Run("lock-exec holds the lock against another process", func(t *testing.T) {
		home := newHome(t)
		layout := addRepo(t, home)
		release := filepath.Join(home, "release")
		holder := exec.Command(binary, "serve", "repo", "lock-exec", "demo", "--", "sh", "-c",
			`while [ ! -e "$1" ]; do sleep 0.05; done`, "sh", release)
		holder.Dir = home
		holder.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "LANG=C"}
		if err := holder.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- holder.Wait() }()
		finished := false
		defer func() {
			if !finished {
				writeServeTestFile(t, release, "", 0o600)
				<-done
			}
		}()

		// Wait until the holder has the lock: our own non-blocking attempt
		// starts failing as busy.
		deadline := time.Now().Add(20 * time.Second)
		for {
			ours, err := acquireServeLock(layout.RepoLock("demo"), 0)
			if errors.Is(err, errServeLockBusy) {
				break
			}
			if err == nil {
				_ = ours()
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			if time.Now().After(deadline) {
				t.Fatal("lock-exec never took the repository lock")
			}
			time.Sleep(20 * time.Millisecond)
		}

		marker := filepath.Join(home, "second-ran")
		output, status := runHN(t, home, []string{"HN_INTERNAL_TESTING=1", "HN_TEST_SERVE_LOCK_WAIT=200ms"},
			"serve", "repo", "lock-exec", "demo", "--", "touch", marker)
		if status != 1 || !strings.HasPrefix(output, "hn: repository-busy: ") {
			t.Fatalf("contended lock-exec = %d %q, want a repository-busy refusal", status, output)
		}
		if _, err := os.Lstat(marker); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("contended lock-exec ran its command: %v", err)
		}
		writeServeTestFile(t, release, "", 0o600)
		finished = true
		if err := <-done; err != nil {
			t.Fatalf("holder lock-exec: %v", err)
		}
	})

	// Every administration command rewrites both hooks. Concurrent commands,
	// and session validation in another process, must never report another
	// command's in-progress hook write as a state defect.
	invoke := func(home string, args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, binary, args...)
		command.Dir = home
		command.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "LANG=C", "HN_INTERNAL_TESTING=0"}
		output, err := command.CombinedOutput()
		if err != nil {
			return string(output), fmt.Errorf("hn %s: %v: %s", strings.Join(args, " "), err, output)
		}
		return string(output), nil
	}
	fanOut := func(calls, workers int, call func(int) error) []error {
		jobs := make(chan int)
		var mutex sync.Mutex
		var failures []error
		var group sync.WaitGroup
		for range workers {
			group.Add(1)
			go func() {
				defer group.Done()
				for job := range jobs {
					if err := call(job); err != nil {
						mutex.Lock()
						failures = append(failures, err)
						mutex.Unlock()
					}
				}
			}()
		}
		for job := range calls {
			jobs <- job
		}
		close(jobs)
		group.Wait()
		return failures
	}

	t.Run("concurrent cold start", func(t *testing.T) {
		home := newHome(t)
		failures := fanOut(32, 16, func(int) error {
			output, err := invoke(home, "serve", "repo", "list")
			if err == nil && output != "" {
				err = fmt.Errorf("hn serve repo list printed %q on an empty server", output)
			}
			return err
		})
		if len(failures) != 0 {
			t.Fatalf("%d of 32 concurrent first commands failed; first: %v", len(failures), failures[0])
		}
		t.Setenv("HOME", home)
		layout, err := resolveServeLayout()
		if err != nil {
			t.Fatal(err)
		}
		if err := validateServeLayoutFor(layout, serveSessionValidation, binary); err != nil {
			t.Fatalf("layout after a concurrent cold start: %v", err)
		}
	})

	t.Run("concurrent commands and session validation", func(t *testing.T) {
		home := newHome(t)
		addRepo(t, home)
		t.Setenv("HOME", home)
		layout, err := resolveServeLayout()
		if err != nil {
			t.Fatal(err)
		}
		commands := []struct {
			args []string
			want string
		}{
			{[]string{"serve", "repo", "list"}, "demo\n"},
			{[]string{"serve", "key", "list"}, ""},
			{[]string{"serve", "repo", "show", "demo"}, ""},
		}
		stop := make(chan struct{})
		sessions := make(chan []error, 1)
		go func() {
			var errs []error
			for count := 0; ; count++ {
				select {
				case <-stop:
					if count == 0 {
						errs = append(errs, errors.New("session validation never ran"))
					}
					sessions <- errs
					return
				default:
				}
				if err := validateServeLayoutFor(layout, serveSessionValidation, binary); err != nil {
					errs = append(errs, err)
				}
			}
		}()
		failures := fanOut(160, 16, func(job int) error {
			command := commands[job%len(commands)]
			output, err := invoke(home, command.args...)
			if err == nil && command.want != "" && output != command.want {
				err = fmt.Errorf("hn %s printed %q, want %q", strings.Join(command.args, " "), output, command.want)
			}
			return err
		})
		close(stop)
		sessionFailures := <-sessions
		if len(failures) != 0 {
			t.Fatalf("%d of 160 concurrent administration commands failed; first: %v", len(failures), failures[0])
		}
		if len(sessionFailures) != 0 {
			t.Fatalf("%d concurrent session validations failed; first: %v", len(sessionFailures), sessionFailures[0])
		}
	})
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
	requireServeErrorContains(t, ensureServeHooks(layout, "/usr/local/bin/my hn"), "cannot use unquoted")
	elsewhere := t.TempDir()
	removeServeTest(t, layout.Hooks)
	symlinkServeTest(t, elsewhere, layout.Hooks)
	requireServeDefect(t, ensureServeHooks(layout, "/usr/local/bin/hn"), layout.Hooks, "symbolic link")
	if entries, err := os.ReadDir(elsewhere); err != nil || len(entries) != 0 {
		t.Fatalf("ensureServeHooks wrote through a symlinked hooks directory: %v (%v)", entries, err)
	}
}

// serveTestUnsafeExecutableNames are file names a /bin/sh word would expand,
// split, redirect, glob or otherwise not use literally.
var serveTestUnsafeExecutableNames = []string{
	`h"n`, "h'n", `h\n`, "h n", "h\tn", "h\nn", "h\u00a0n", "caf\u00e9",
	"hn$HOME", "hn${IFS}x", "hn$(id)", "hn`id`", "hn;id", "hn|id", "hn&id", "hn&&id",
	"hn<x", "hn>x", "hn*", "hn?", "hn[ab]", "hn]", "hn(x)", "hn{a,b}", "hn}", "hn~", "~hn",
	"hn#x", "#hn", "hn!", "hn%", "hn=x", "hn^id", "hn\x00x", "hn\x7f",
}

func TestServeStateExecutablePathRules(t *testing.T) {
	for _, name := range serveTestUnsafeExecutableNames {
		path := "/usr/local/bin/" + name
		if _, err := checkServeExecutablePath(path); err == nil || !strings.Contains(err.Error(), "cannot use unquoted") {
			t.Errorf("checkServeExecutablePath(%q) = %v, want a refusal", path, err)
		}
	}
	for _, path := range []string{"relative/hn", "/usr/local//bin/hn", "/usr/local/bin/../hn", "/usr/local/bin/./hn", "/usr/local/bin/"} {
		if _, err := checkServeExecutablePath(path); err == nil {
			t.Errorf("checkServeExecutablePath(%q) accepted a relative or unclean path", path)
		}
	}
	for _, path := range []string{"/usr/local/bin/hn-0.1_x", "/opt/hn+tools/v1@2,3:4/hn", "/nix/store/abc123-hn-0.0.1/bin/hn"} {
		if got, err := checkServeExecutablePath(path); err != nil || got != path {
			t.Fatalf("safe path %q = %q, %v", path, got, err)
		}
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

// Generated hooks run the hn path as one literal word. For each unsafe name
// an executable really exists at that path, so a hook that embedded it would
// run shell expansions; generation must refuse before any side effect.
func TestServeStateUnsafeExecutablePathsNeverReachAHook(t *testing.T) {
	layout := newServeTestLayout(t)
	marker := filepath.Join(layout.Home, "shell-expansion-ran")
	t.Setenv("HN_SERVE_TEST_MARKER", marker)
	before := map[string]string{}
	for _, name := range serveHookNames {
		contents, err := os.ReadFile(filepath.Join(layout.Hooks, name))
		if err != nil {
			t.Fatal(err)
		}
		before[name] = string(contents)
	}
	names := []string{
		"hn$(touch${IFS}${HN_SERVE_TEST_MARKER})",
		"hn`touch ${HN_SERVE_TEST_MARKER}`",
		"hn;touch${IFS}${HN_SERVE_TEST_MARKER}",
		"hn|touch${IFS}${HN_SERVE_TEST_MARKER}",
		"hn&touch${IFS}${HN_SERVE_TEST_MARKER}",
		"hn>${HN_SERVE_TEST_MARKER}",
		"hn$HN_SERVE_TEST_MARKER",
		"hn*",
		"hn{a,b}",
		"hn(x)",
	}
	directory := filepath.Join(layout.Home, "bin")
	mkdirServeTest(t, directory, 0o700)
	for _, name := range names {
		path := filepath.Join(directory, name)
		writeServeTestFile(t, path, "#!/bin/sh\nexit 0\n", 0o700)
		requireServeErrorContains(t, ensureServeHooks(layout, path), "cannot use unquoted")
		for _, hook := range serveHookNames {
			contents, err := os.ReadFile(filepath.Join(layout.Hooks, hook))
			if err != nil || string(contents) != before[hook] {
				t.Fatalf("refused path %q changed hook %s to %q (%v)", path, hook, contents, err)
			}
		}
		if _, err := os.Lstat(marker); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("path %q caused a shell side effect: %v", path, err)
		}
	}
	if entries, err := os.ReadDir(layout.Hooks); err != nil || len(entries) != 2 {
		t.Fatalf("refused hook writes left entries in hooks/: %v (%v)", entries, err)
	}
}

func TestServeStateGeneratedHooksExecuteTheLiteralPath(t *testing.T) {
	layout := newServeTestLayout(t)
	directory := filepath.Join(layout.Home, "opt", "hn+tools", "v1@2,3:4")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(layout.Home, "arguments")
	hnPath := filepath.Join(directory, "hn-0.1_x")
	if _, err := checkServeExecutablePath(hnPath); err != nil {
		t.Skipf("temporary directory is not usable as an hn install path: %v", err)
	}
	writeServeTestFile(t, hnPath, "#!/bin/sh\nprintf '%s|' \"$0\" \"$@\" > \""+record+"\"\n", 0o700)
	if err := ensureServeHooks(layout, hnPath); err != nil {
		t.Fatal(err)
	}
	for _, name := range serveHookNames {
		hook := filepath.Join(layout.Hooks, name)
		if output, err := exec.Command("/bin/sh", hook).CombinedOutput(); err != nil {
			t.Fatalf("hook %s: %v: %s", name, err, output)
		}
		contents, err := os.ReadFile(record)
		if err != nil {
			t.Fatal(err)
		}
		if want := hnPath + "|serve|hook|" + name + "|"; string(contents) != want {
			t.Fatalf("hook %s ran %q, want %q", name, contents, want)
		}
		removeServeTest(t, record)
	}
}

// A validator in either mode waits while an administration command is
// between staging and renaming a hook script, then sees the finished hooks.
func TestServeStateValidationWaitsForInProgressHookWrite(t *testing.T) {
	layout := newServeTestLayout(t)
	hnPath := serveTestExecutable(t)
	hook := filepath.Join(layout.Hooks, "pre-receive")
	release, err := lockServeLayout(layout, serveLockExclusive)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = release() })
	staged, err := stageServeExecutable(hook, serveHookScript("pre-receive", hnPath))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(staged) != layout.Hooks || !strings.HasPrefix(filepath.Base(staged), serveHookStagingPrefix) {
		t.Fatalf("staging file %s is not a %s* file in %s", staged, serveHookStagingPrefix, layout.Hooks)
	}

	results := make(chan error, 2)
	for _, mode := range []serveValidationMode{serveSessionValidation, serveAdministrationValidation} {
		go func() { results <- validateServeLayout(layout, mode) }()
	}
	select {
	case err := <-results:
		t.Fatalf("validation finished during an in-progress hook write: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	if _, err := os.Lstat(staged); err != nil {
		t.Fatalf("staging file disappeared while the writer held the layout lock: %v", err)
	}

	if err := commitServeExecutable(staged, hook); err != nil {
		t.Fatal(err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		select {
		case err := <-results:
			if err != nil {
				t.Fatalf("validation after the hook write finished: %v", err)
			}
		case <-time.After(30 * time.Second):
			t.Fatal("validation never resumed after the hook write finished")
		}
	}
	if entries, err := os.ReadDir(layout.Hooks); err != nil || len(entries) != 2 {
		t.Fatalf("hooks/ after the write = %v (%v), want only the two scripts", entries, err)
	}
}

// With no writer holding the layout lock, a staging file is left over from
// an interrupted command. It stays a defect in every mode until removed.
func TestServeStateAbandonedHookStagingFileIsADefect(t *testing.T) {
	layout := newServeTestLayout(t)
	staged, err := stageServeExecutable(filepath.Join(layout.Hooks, "post-receive"), serveHookScript("post-receive", serveTestExecutable(t)))
	if err != nil {
		t.Fatal(err)
	}
	before := snapshotLegacyPrivateTree(t, layout.Home)
	requireServeDefect(t, validateServeLayout(layout, serveSessionValidation), staged, "abandoned hook staging file")
	if after := snapshotLegacyPrivateTree(t, layout.Home); !reflect.DeepEqual(before, after) {
		t.Fatalf("session validation changed the layout:\nbefore %v\nafter  %v", before, after)
	}
	requireServeDefect(t, validateServeLayout(layout, serveAdministrationValidation), staged, "abandoned hook staging file")
	for _, args := range [][]string{{"serve", "repo", "list"}, {"serve", "key", "list"}} {
		output, err := runServeTest(t, args...)
		requireServeDefect(t, err, staged, "abandoned hook staging file")
		if output != "" {
			t.Fatalf("hn %s printed %q with an abandoned staging file", strings.Join(args, " "), output)
		}
	}
	if _, err := os.Lstat(staged); err != nil {
		t.Fatalf("validation removed the abandoned staging file instead of reporting it: %v", err)
	}
	removeServeTest(t, staged)
	requireServeTestOutput(t, "", "serve", "repo", "list")
	if err := validateServeLayout(layout, serveSessionValidation); err != nil {
		t.Fatal(err)
	}
}

// Sessions share the layout lock with each other; administration commands
// take it alone and report a busy layout after the lock wait.
func TestServeStateLayoutLockCoordinatesCommands(t *testing.T) {
	layout := newServeTestLayout(t)
	t.Setenv("HN_INTERNAL_TESTING", "1")
	t.Setenv("HN_TEST_SERVE_LOCK_WAIT", "100ms")
	const busy = "hn serve layout is busy: another hn serve command held the lock on "

	shared, err := lockServeLayout(layout, serveLockShared)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateServeLayout(layout, serveSessionValidation); err != nil {
		t.Fatalf("session validation beside another session: %v", err)
	}
	requireServeErrorContains(t, validateServeLayout(layout, serveAdministrationValidation), busy+layout.Root)
	output, err := runServeTest(t, "serve", "key", "list")
	requireServeErrorContains(t, err, busy+layout.Root)
	if output != "" {
		t.Fatalf("busy hn serve key list printed %q", output)
	}
	if err := shared(); err != nil {
		t.Fatal(err)
	}

	exclusive, err := lockServeLayout(layout, serveLockExclusive)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []serveValidationMode{serveSessionValidation, serveAdministrationValidation} {
		requireServeErrorContains(t, validateServeLayout(layout, mode), busy+layout.Root)
	}
	if err := exclusive(); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []serveValidationMode{serveSessionValidation, serveAdministrationValidation} {
		if err := validateServeLayout(layout, mode); err != nil {
			t.Fatalf("validation after the lock was released (mode %d): %v", mode, err)
		}
	}
}
