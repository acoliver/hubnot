package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// WP05: hn serve repo and key administration, driven through the built
// binary with one stable server HOME (C-009).

func serveAdminTestCloneURL(t *testing.T, name string) string {
	t.Helper()
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	host, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	return "ssh://" + account.Username + "@" + host + "/" + name + ".git"
}

func serveAdminTestCreateOutput(t *testing.T, name string, quota int64, protected ...string) string {
	t.Helper()
	output := "Created hosted repository " + name + "\n"
	for _, ref := range protected {
		output += "Protected: " + ref + "\n"
	}
	return output + fmt.Sprintf("LFS quota: %d bytes\n", quota) + "Clone URL: " + serveAdminTestCloneURL(t, name) + "\n"
}

func serveAdminTestShowOutput(layout serveLayout, config serveRepoConfig, stored int64) string {
	output := "name: " + config.Name + "\n" + "path: " + layout.RepoGitDir(config.Name) + "\n"
	for _, ref := range config.Protected {
		output += "protected: " + ref + "\n"
	}
	return output + fmt.Sprintf("lfs-quota: %d\nlfs-stored: %d\nmax-events: %d\nmax-objects: %d\nmax-object-bytes: %d\nmax-attachment-bytes: %d\nmax-total-bytes: %d\n",
		config.LFSQuotaBytes, stored, config.Budgets.MaxEvents, config.Budgets.MaxObjects,
		config.Budgets.MaxObjectBytes, config.Budgets.MaxAttachmentBytes, config.Budgets.MaxTotalBytes)
}

// serveAdminTestGitInput runs git in directory with input on stdin.
func serveAdminTestGitInput(t *testing.T, directory string, input []byte, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = directory
	command.Env = operationalTestEnvironment(t.TempDir())
	command.Stdin = bytes.NewReader(input)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

// serveAdminTestHostedState lists what creation may change: the hosted
// repositories, their state directories and state/tmp.
func serveAdminTestHostedState(t *testing.T, layout serveLayout) [][]string {
	t.Helper()
	return [][]string{
		serveHarnessEntries(t, layout.Repositories),
		serveHarnessEntries(t, layout.Repos),
		serveHarnessEntries(t, layout.Tmp),
	}
}

func requireServeAdminContains(t *testing.T, output string, fragments ...string) {
	t.Helper()
	for _, fragment := range fragments {
		if !strings.Contains(output, fragment) {
			t.Fatalf("output does not contain %q:\n%s", fragment, output)
		}
	}
}

func readServeAdminFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

// FR-002, OQ-7: creation imports only branches and tags, writes the marker
// and HEAD, and stores the contract defaults.
func TestServeAdminCreateImportsOnlyBranchesAndTags(t *testing.T) {
	h := newServeHarness(t)
	layout := h.Layout()
	source := serveHarnessSourceRepo(t)

	output := h.Serve("serve", "repo", "create", "pingpong", "--from", source)
	if want := serveAdminTestCreateOutput(t, "pingpong", 1<<30, "refs/heads/main"); output != want {
		t.Fatalf("repo create output:\n%s\nwant:\n%s", output, want)
	}

	gitDir := layout.RepoGitDir("pingpong")
	refs := serveHarnessGit(t, gitDir, "for-each-ref", "--format=%(refname)")
	if want := "refs/heads/main\nrefs/heads/release\nrefs/hn/serve/v1\nrefs/tags/v1"; refs != want {
		t.Fatalf("hosted refs:\n%s\nwant:\n%s", refs, want)
	}
	if strings.Contains(refs, serveHarnessImportedActorRef) {
		t.Fatalf("repo create imported %s", serveHarnessImportedActorRef)
	}
	marker := serveHarnessGit(t, gitDir, "rev-parse", serveMarkerRef)
	if want := serveAdminTestGitInput(t, source, []byte(serveMarkerContent), "hash-object", "--stdin"); marker != want {
		t.Fatalf("%s = %s, want the hn-serve/1 blob %s", serveMarkerRef, marker, want)
	}
	if contents := serveHarnessGit(t, gitDir, "cat-file", "blob", marker); contents+"\n" != serveMarkerContent {
		t.Fatalf("marker blob = %q", contents)
	}
	if head := serveHarnessGit(t, gitDir, "symbolic-ref", "HEAD"); head != "refs/heads/main" {
		t.Fatalf("HEAD = %s, want refs/heads/main", head)
	}
	for _, branch := range []string{"main", "release"} {
		if got, want := serveHarnessGit(t, gitDir, "rev-parse", "refs/heads/"+branch), strings.TrimSpace(runOperationalGit(t, source, "rev-parse", branch)); got != want {
			t.Fatalf("hosted %s = %s, want %s", branch, got, want)
		}
	}
	info, err := os.Lstat(gitDir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("hosted repository directory = %v (%v), want a 0700 directory", info, err)
	}
	if _, err := os.Lstat(filepath.Join(gitDir, "hooks")); !os.IsNotExist(err) {
		t.Fatalf("repo create wrote a hooks directory into the repository: %v", err)
	}
	if config := runOperationalGit(t, "", "--git-dir="+gitDir, "config", "--list", "--local"); strings.Contains(config, "hookspath") {
		t.Fatalf("repo create configured hooks in the repository:\n%s", config)
	}

	want, err := newServeRepoConfig("pingpong", nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := encodeServeRepoConfig(want)
	if err != nil {
		t.Fatal(err)
	}
	if got := readServeAdminFile(t, layout.RepoConfig("pingpong")); got != string(encoded) {
		t.Fatalf("config.json:\n%s\nwant:\n%s", got, encoded)
	}
	if entries := serveHarnessEntries(t, layout.Tmp); !reflect.DeepEqual(entries, []string{serveAdminLockName}) {
		t.Fatalf("state/tmp after create = %v, want only the administration lock", entries)
	}
	if output := h.Serve("serve", "repo", "list"); output != "pingpong\n" {
		t.Fatalf("repo list = %q", output)
	}

	t.Run("protect list and quota", func(t *testing.T) {
		runOperationalGit(t, source, "branch", "stable", "main")
		output := h.Serve("serve", "repo", "create", "second", "--from", source, "--protect", "stable", "--protect", "refs/heads/main", "--lfs-quota", "2048")
		if want := serveAdminTestCreateOutput(t, "second", 2048, "refs/heads/stable", "refs/heads/main"); output != want {
			t.Fatalf("repo create output:\n%s\nwant:\n%s", output, want)
		}
		if head := serveHarnessGit(t, layout.RepoGitDir("second"), "symbolic-ref", "HEAD"); head != "refs/heads/stable" {
			t.Fatalf("HEAD = %s, want the first protected ref", head)
		}
	})

	t.Run("sha256 source", func(t *testing.T) {
		repository := filepath.Join(t.TempDir(), "sha256")
		if output, err := exec.Command("git", "init", "-q", "--object-format=sha256", "-b", "main", repository).CombinedOutput(); err != nil {
			t.Skipf("git cannot create SHA-256 repositories: %v: %s", err, output)
		}
		configureOperationalGit(t, repository, "Source")
		serveHarnessWritePolicy(t, repository, serveHarnessActor("maintainer"))
		serveHarnessCommit(t, repository, "seed")
		h.Serve("serve", "repo", "create", "wide", "--from", repository)
		hosted := layout.RepoGitDir("wide")
		if format := serveHarnessGit(t, hosted, "rev-parse", "--show-object-format"); format != "sha256" {
			t.Fatalf("hosted object format = %s, want sha256", format)
		}
		if marker, want := serveHarnessGit(t, hosted, "rev-parse", serveMarkerRef), serveAdminTestGitInput(t, repository, []byte(serveMarkerContent), "hash-object", "--stdin"); marker != want || len(marker) != 64 {
			t.Fatalf("SHA-256 marker = %s, want %s", marker, want)
		}
	})
}

// FR-002: every refused creation leaves repositories/, state/repos/ and
// state/tmp/ exactly as they were.
func TestServeAdminCreateRefusalsLeaveNothingBehind(t *testing.T) {
	h := newServeHarness(t)
	layout := h.Layout()
	source := serveHarnessSourceRepo(t)
	h.Serve("serve", "repo", "create", "demo", "--from", source)

	malformed := serveHarnessSourceRepo(t)
	serveHarnessWriteFile(t, malformed, filepath.Join(".hn", "policy.json"), "{not json\n")
	serveHarnessCommit(t, malformed, "break policy")

	unknownField := serveHarnessSourceRepo(t)
	serveHarnessWriteFile(t, unknownField, filepath.Join(".hn", "policy.json"), `{"version":"hn.policy/0","maintainers":[],"extra":true}`+"\n")
	serveHarnessCommit(t, unknownField, "policy with unknown field")

	// A tree entry named .git is a fetch fsck error, so this source fails
	// after staging has created the bare repository.
	hostile := filepath.Join(t.TempDir(), "hostile")
	initOperationalRepository(t, hostile, "Hostile")
	blob := serveAdminTestGitInput(t, hostile, []byte("payload\n"), "hash-object", "-w", "--stdin")
	rawBlob, err := hex.DecodeString(blob)
	if err != nil {
		t.Fatal(err)
	}
	tree := serveAdminTestGitInput(t, hostile, append([]byte("100644 .git\x00"), rawBlob...), "hash-object", "-w", "-t", "tree", "--literally", "--stdin")
	commit := serveAdminTestGitInput(t, hostile, nil, "commit-tree", tree, "-m", "hostile")
	runOperationalGit(t, hostile, "update-ref", "refs/heads/main", commit)

	empty := filepath.Join(t.TempDir(), "empty")
	initOperationalRepository(t, empty, "Empty")

	for _, tc := range []struct {
		name string
		args []string
		want []string
	}{
		{"invalid name", []string{"bad..name", "--from", source}, []string{`invalid hosted repository name "bad..name"`}},
		{"trailing .git", []string{"demo.git", "--from", source}, []string{`invalid hosted repository name "demo.git"`}},
		{"existing name", []string{"demo", "--from", source}, []string{"hosted repository demo already exists"}},
		{"missing protected branch", []string{"fresh", "--from", source, "--protect", "nope"}, []string{"cannot create hosted repository fresh: protected branch refs/heads/nope does not exist"}},
		{"protected branch without policy", []string{"fresh", "--from", source, "--protect", "main", "--protect", "release"}, []string{"protected branch refs/heads/release has no .hn/policy.json at its tip"}},
		{"malformed policy", []string{"fresh", "--from", malformed}, []string{"protected branch refs/heads/main has an invalid .hn/policy.json at its tip", "parse policy"}},
		{"policy with unknown field", []string{"fresh", "--from", unknownField}, []string{"has an invalid .hn/policy.json", "unknown field"}},
		{"hostile objects", []string{"fresh", "--from", hostile}, []string{"cannot create hosted repository fresh: import branches and tags from " + hostile, "hasDotgit"}},
		{"source without refs", []string{"fresh", "--from", empty}, []string{empty + " has no refs to import"}},
		{"missing source", []string{"fresh", "--from", filepath.Join(t.TempDir(), "absent")}, []string{"cannot create hosted repository fresh: list refs of"}},
		{"option as source", []string{"fresh", "--from", "--upload-pack=touch"}, []string{"looks like an option"}},
		{"invalid protect ref", []string{"fresh", "--from", source, "--protect", "refs/tags/v1"}, []string{`"refs/tags/v1" is not a branch`}},
		{"zero quota", []string{"fresh", "--from", source, "--lfs-quota", "0"}, []string{"--lfs-quota must be a positive number of bytes"}},
		{"missing --from", []string{"fresh"}, []string{"usage: hn serve repo create NAME --from SOURCE"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := serveAdminTestHostedState(t, layout)
			output := h.ServeFailure(append([]string{"serve", "repo", "create"}, tc.args...)...)
			requireServeAdminContains(t, output, tc.want...)
			if after := serveAdminTestHostedState(t, layout); !reflect.DeepEqual(before, after) {
				t.Fatalf("refused creation changed the layout:\nbefore %v\nafter  %v", before, after)
			}
		})
	}
	if output := h.Serve("serve", "repo", "list"); output != "demo\n" {
		t.Fatalf("repo list after refusals = %q", output)
	}
}

// FR-003, FR-004 and US2-1/US2-5 (administration part): bindings require
// full fingerprints and actors, one actor may hold several keys, and every
// change rewrites the managed block while lines outside it keep every byte.
func TestServeAdminKeysMaintainAuthorizedKeys(t *testing.T) {
	h := newServeHarness(t)
	layout := h.Layout()
	binary, err := filepath.EvalSymlinks(h.Binary)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(layout.SSHDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Two administrator lines with CRLF endings and no final newline.
	adminOne, adminTwo := newServeTestKey(t, "admin-one"), newServeTestKey(t, "admin-two")
	outside := adminOne.PublicLine + "\r\n" + adminTwo.PublicLine
	writeServeTestFile(t, layout.AuthorizedKeys, outside, 0o600)

	h.Serve("serve", "repo", "create", "demo", "--from", serveHarnessSourceRepo(t))
	alice, bob := serveHarnessActor("alice"), serveHarnessActor("bob")
	laptop := serveHarnessAddKey(h, alice)
	token := serveHarnessAddKey(h, alice)
	desktop := serveHarnessAddKey(h, bob)

	entry := func(key serveTestKey, actor string) string {
		fields := strings.Fields(key.PublicLine)
		return `restrict,command="` + binary + ` serve git --key ` + key.Fingerprint + `" ` + fields[0] + " " + fields[1] + " hn-serve:" + actor[:12] + "\n"
	}
	bindings := []struct {
		key   serveTestKey
		actor string
	}{{laptop, alice}, {token, alice}, {desktop, bob}}
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].key.Fingerprint < bindings[j].key.Fingerprint })
	block := serveAuthorizedKeysBegin + "\n"
	list := ""
	for _, binding := range bindings {
		block += entry(binding.key, binding.actor)
		list += binding.key.Fingerprint + " " + binding.actor + " ssh-ed25519\n"
	}
	block += serveAuthorizedKeysEnd + "\n"
	if got, want := readServeAdminFile(t, layout.AuthorizedKeys), outside+"\n"+block; got != want {
		t.Fatalf("authorized_keys:\n%q\nwant:\n%q", got, want)
	}
	if output := h.Serve("serve", "key", "list"); output != list {
		t.Fatalf("key list:\n%s\nwant:\n%s", output, list)
	}
	requireServeKeysMode(t, layout.AuthorizedKeys, 0o600)

	t.Run("refusals change nothing", func(t *testing.T) {
		keyFile := filepath.Join(t.TempDir(), "fresh.pub")
		fresh := newServeTestKey(t, "fresh")
		writeServeTestFile(t, keyFile, fresh.PublicLine+"\n", 0o644)
		twoLines := filepath.Join(t.TempDir(), "two.pub")
		writeServeTestFile(t, twoLines, fresh.PublicLine+"\n\n"+adminOne.PublicLine+"\n", 0o644)
		for _, tc := range []struct {
			name  string
			stdin string
			args  []string
			want  string
		}{
			{"short actor", "", []string{"key", "add", "--actor", alice[:12], keyFile}, "is not a full 64-hex actor fingerprint"},
			{"uppercase actor", "", []string{"key", "add", "--actor", strings.ToUpper(alice), keyFile}, "is not a full 64-hex actor fingerprint"},
			{"missing actor", "", []string{"key", "add", keyFile}, "usage: hn serve key add --actor ACTOR KEYFILE|-"},
			{"already bound to the same actor", laptop.PublicLine + "\n", []string{"key", "add", "--actor", alice, "-"}, "key " + laptop.Fingerprint + " is already bound to actor " + alice},
			{"already bound to another actor", laptop.PublicLine + "\n", []string{"key", "add", "--actor", bob, "-"}, "is already bound to actor " + alice},
			{"two key lines", "", []string{"key", "add", "--actor", alice, twoLines}, "holds 2 non-empty lines"},
			{"empty input", "\n\n", []string{"key", "add", "--actor", alice, "-"}, "standard input holds no SSH public key line"},
			{"unsupported type", "ssh-dss AAAAB3NzaC1kc3M= old\n", []string{"key", "add", "--actor", alice, "-"}, `unsupported SSH key type "ssh-dss"`},
			{"oversized input", strings.Repeat("a", serveKeyInputLimit+1), []string{"key", "add", "--actor", alice, "-"}, "standard input is larger than 16384 bytes"},
			{"missing key file", "", []string{"key", "add", "--actor", alice, filepath.Join(t.TempDir(), "absent.pub")}, "read SSH public key"},
			{"short fingerprint", "", []string{"key", "remove", laptop.Fingerprint[:20]}, "is not a full SHA256 key fingerprint"},
			{"unknown fingerprint", "", []string{"key", "remove", fresh.Fingerprint}, "no key with fingerprint " + fresh.Fingerprint + " is bound"},
			{"remove without argument", "", []string{"key", "remove"}, "usage: hn serve key remove FINGERPRINT"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				registry := readServeAdminFile(t, layout.Registry)
				keys := readServeAdminFile(t, layout.AuthorizedKeys)
				output := h.ServeFailureWith(serveHarnessRun{Stdin: tc.stdin}, append([]string{"serve"}, tc.args...)...)
				requireServeAdminContains(t, output, tc.want)
				if readServeAdminFile(t, layout.Registry) != registry || readServeAdminFile(t, layout.AuthorizedKeys) != keys {
					t.Fatalf("refused %s changed the registry or authorized_keys", strings.Join(tc.args, " "))
				}
			})
		}
	})

	t.Run("key file with options and comment", func(t *testing.T) {
		carol := serveHarnessActor("carol")
		key := newServeTestKey(t, "carol@laptop")
		keyFile := filepath.Join(t.TempDir(), "carol.pub")
		writeServeTestFile(t, keyFile, `restrict,command="echo hi" `+key.PublicLine+"\r\n", 0o644)
		if output := h.Serve("serve", "key", "add", "--actor", carol, keyFile); output != "Bound "+key.Fingerprint+" to actor "+carol+"\n" {
			t.Fatalf("key add from a file = %q", output)
		}
		contents := readServeAdminFile(t, layout.AuthorizedKeys)
		if !strings.Contains(contents, entry(key, carol)) || strings.Contains(contents, "echo hi") || strings.Contains(contents, "carol@laptop") {
			t.Fatalf("authorized_keys kept input options or comment:\n%s", contents)
		}
		h.Serve("serve", "key", "remove", key.Fingerprint)
	})

	for _, binding := range bindings {
		if output := h.Serve("serve", "key", "remove", binding.key.Fingerprint); output != "Removed "+binding.key.Fingerprint+" from actor "+binding.actor+"\n" {
			t.Fatalf("key remove output = %q", output)
		}
		if strings.Contains(readServeAdminFile(t, layout.AuthorizedKeys), binding.key.Fingerprint) {
			t.Fatalf("authorized_keys still names removed key %s", binding.key.Fingerprint)
		}
	}
	empty := serveAuthorizedKeysBegin + "\n" + serveAuthorizedKeysEnd + "\n"
	if got := readServeAdminFile(t, layout.AuthorizedKeys); got != outside+"\n"+empty {
		t.Fatalf("authorized_keys after removing every key:\n%q\nwant:\n%q", got, outside+"\n"+empty)
	}
	if output := h.Serve("serve", "key", "list"); output != "" {
		t.Fatalf("key list after removing every key = %q", output)
	}
}

// Administrators are serialized by state/tmp/admin.lock: concurrent key
// additions all land, and a held lock makes a command report a busy
// administration after the wait.
func TestServeAdminLockSerializesAdministrators(t *testing.T) {
	h := newServeHarness(t)
	layout := h.Layout()
	h.Serve("serve", "repo", "list")

	const count = 8
	keys := make([]serveTestKey, count)
	for index := range keys {
		keys[index] = newServeTestKey(t, fmt.Sprintf("key-%d", index))
	}
	errs := make([]error, count)
	var group sync.WaitGroup
	for index := range keys {
		group.Add(1)
		go func() {
			defer group.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, h.Binary, "serve", "key", "add", "--actor", serveHarnessActor(fmt.Sprint(index)), "-")
			command.Dir = h.Home
			command.Env = serveHarnessEnvironment(h.Home, nil)
			command.Stdin = strings.NewReader(keys[index].PublicLine + "\n")
			if output, err := command.CombinedOutput(); err != nil {
				errs[index] = fmt.Errorf("%v: %s", err, output)
			}
		}()
	}
	group.Wait()
	for index, err := range errs {
		if err != nil {
			t.Fatalf("concurrent key add %d failed: %v", index, err)
		}
	}
	listed := strings.Count(h.Serve("serve", "key", "list"), "\n")
	block := strings.Count(readServeAdminFile(t, layout.AuthorizedKeys), "restrict,command=")
	if listed != count || block != count {
		t.Fatalf("after %d concurrent key adds: %d bindings listed, %d authorized_keys entries", count, listed, block)
	}

	t.Setenv("HOME", h.Home)
	release, err := acquireServeLock(filepath.Join(layout.Tmp, serveAdminLockName), 0)
	if err != nil {
		t.Fatal(err)
	}
	output := h.ServeFailureWith(serveHarnessRun{Env: map[string]string{"HN_INTERNAL_TESTING": "1", "HN_TEST_SERVE_LOCK_WAIT": "200ms"}}, "serve", "key", "list")
	requireServeAdminContains(t, output, "hn serve administration is busy: another hn serve repo or key command held "+filepath.Join(layout.Tmp, serveAdminLockName))
	if err := release(); err != nil {
		t.Fatal(err)
	}
}

// FR-002: repo set applies the protected-tip rule to newly protected refs,
// refuses to unprotect the last one, and changes quota and budgets.
func TestServeAdminRepoSet(t *testing.T) {
	h := newServeHarness(t)
	layout := h.Layout()
	source := serveHarnessSourceRepo(t)
	runOperationalGit(t, source, "branch", "stable", "main")
	h.Serve("serve", "repo", "create", "demo", "--from", source)
	gitDir := layout.RepoGitDir("demo")

	config, err := newServeRepoConfig("demo", nil)
	if err != nil {
		t.Fatal(err)
	}
	config.Protected = []string{"refs/heads/main", "refs/heads/stable"}
	if output := h.Serve("serve", "repo", "set", "demo", "--protect", "stable"); output != serveAdminTestShowOutput(layout, config, 0) {
		t.Fatalf("repo set --protect stable:\n%s", output)
	}

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"branch without policy", []string{"--protect", "release"}, "cannot protect refs/heads/release in hosted repository demo: protected branch refs/heads/release has no .hn/policy.json at its tip"},
		{"missing branch", []string{"--protect", "refs/heads/absent"}, "protected branch refs/heads/absent does not exist"},
		{"both protect and unprotect", []string{"--protect", "stable", "--unprotect", "stable"}, "refs/heads/stable is named in both --protect and --unprotect"},
		{"unprotect an unprotected ref", []string{"--unprotect", "release"}, "refs/heads/release is not protected in hosted repository demo"},
		{"unprotect everything", []string{"--unprotect", "main", "--unprotect", "stable"}, "needs at least one protected branch"},
		{"zero quota", []string{"--lfs-quota", "0"}, "--lfs-quota must be a positive number of bytes"},
		{"negative budget", []string{"--max-events", "-1"}, "max-events must be a positive integer"},
		{"zero budget", []string{"--max-total-bytes", "0"}, "max-total-bytes must be a positive integer"},
		{"invalid ref", []string{"--protect", "bad..ref"}, `"bad..ref" is not a branch`},
		{"extra argument", []string{"extra"}, "usage: hn serve repo set NAME"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := readServeAdminFile(t, layout.RepoConfig("demo"))
			output := h.ServeFailure(append([]string{"serve", "repo", "set", "demo"}, tc.args...)...)
			requireServeAdminContains(t, output, tc.want)
			if after := readServeAdminFile(t, layout.RepoConfig("demo")); after != before {
				t.Fatalf("refused repo set changed config.json:\n%s", after)
			}
			if head := serveHarnessGit(t, gitDir, "symbolic-ref", "HEAD"); head != "refs/heads/main" {
				t.Fatalf("refused repo set moved HEAD to %s", head)
			}
		})
	}
	h.ServeFailure("serve", "repo", "set", "absent")

	// Unprotecting the default promotes the next remaining entry.
	config.Protected = []string{"refs/heads/stable"}
	config.LFSQuotaBytes = 4096
	config.Budgets = ReplicationBudgets{MaxEvents: 5, MaxObjects: 6, MaxObjectBytes: 7, MaxAttachmentBytes: 8, MaxTotalBytes: 9}
	output := h.Serve("serve", "repo", "set", "demo", "--unprotect", "refs/heads/main", "--lfs-quota", "4096",
		"--max-events", "5", "--max-objects", "6", "--max-object-bytes", "7", "--max-attachment-bytes", "8", "--max-total-bytes", "9")
	if want := serveAdminTestShowOutput(layout, config, 0); output != want {
		t.Fatalf("repo set output:\n%s\nwant:\n%s", output, want)
	}
	if head := serveHarnessGit(t, gitDir, "symbolic-ref", "HEAD"); head != "refs/heads/stable" {
		t.Fatalf("HEAD after unprotecting the default = %s, want refs/heads/stable", head)
	}
	encoded, err := encodeServeRepoConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	if got := readServeAdminFile(t, layout.RepoConfig("demo")); got != string(encoded) {
		t.Fatalf("config.json:\n%s\nwant:\n%s", got, encoded)
	}
	if output := h.Serve("serve", "repo", "show", "demo"); output != serveAdminTestShowOutput(layout, config, 0) {
		t.Fatalf("repo show after set:\n%s", output)
	}
	h.ServeFailure("serve", "repo", "set", "demo", "--unprotect", "stable")

	// Protecting main again keeps stable as the default.
	config.Protected = []string{"refs/heads/stable", "refs/heads/main"}
	if output := h.Serve("serve", "repo", "set", "demo", "--protect", "main"); output != serveAdminTestShowOutput(layout, config, 0) {
		t.Fatalf("repo set --protect main:\n%s", output)
	}
}

func TestServeAdminRepoListAndShow(t *testing.T) {
	h := newServeHarness(t)
	layout := h.Layout()
	source := serveHarnessSourceRepo(t)
	for _, name := range []string{"zeta", "Alpha", "alpha.v2"} {
		h.Serve("serve", "repo", "create", name, "--from", source)
	}
	if output := h.Serve("serve", "repo", "list"); output != "Alpha\nalpha.v2\nzeta\n" {
		t.Fatalf("repo list = %q, want byte order", output)
	}
	config, err := newServeRepoConfig("zeta", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "name: zeta\n" +
		"path: " + filepath.Join(h.Home, ".hn-serve", "repositories", "zeta.git") + "\n" +
		"protected: refs/heads/main\n" +
		"lfs-quota: 1073741824\n" +
		"lfs-stored: 0\n" +
		"max-events: 10000\n" +
		"max-objects: 100000\n" +
		"max-object-bytes: 67108864\n" +
		"max-attachment-bytes: 16777216\n" +
		"max-total-bytes: 1073741824\n"
	if output := h.Serve("serve", "repo", "show", "zeta"); output != want || output != serveAdminTestShowOutput(layout, config, 0) {
		t.Fatalf("repo show:\n%s\nwant:\n%s", output, want)
	}
	requireServeAdminContains(t, h.ServeFailure("serve", "repo", "show", "absent"), "repository absent is not hosted")
}

// lock-exec passes the exit status through, holds the repository lock
// against a second lock-exec, and does not hold the administration lock.
func TestServeAdminLockExec(t *testing.T) {
	h := newServeHarness(t)
	h.Serve("serve", "repo", "create", "demo", "--from", serveHarnessSourceRepo(t))

	if output, status := h.Exec(serveHarnessRun{}, "serve", "repo", "lock-exec", "demo", "--", "sh", "-c", "exit 7"); status != 7 || output != "" {
		t.Fatalf("lock-exec exit 7 = %d %q", status, output)
	}
	if output := h.Serve("serve", "repo", "lock-exec", "demo", "--", "git", "rev-parse", "--abbrev-ref", "HEAD"); output != "main\n" {
		t.Fatalf("lock-exec git in the repository = %q", output)
	}

	started := filepath.Join(t.TempDir(), "started")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	holder := exec.CommandContext(ctx, h.Binary, "serve", "repo", "lock-exec", "demo", "--", "sh", "-c", "touch \"$1\"; sleep 3", "sh", started)
	holder.Dir = h.Home
	holder.Env = serveHarnessEnvironment(h.Home, nil)
	var holderOutput bytes.Buffer
	holder.Stdout, holder.Stderr = &holderOutput, &holderOutput
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if _, err := os.Lstat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("holding lock-exec never started: %s", holderOutput.String())
		}
	}
	short := serveHarnessRun{Env: map[string]string{"HN_INTERNAL_TESTING": "1", "HN_TEST_SERVE_LOCK_WAIT": "200ms"}}
	marker := filepath.Join(t.TempDir(), "ran")
	requireServeAdminContains(t, h.ServeFailureWith(short, "serve", "repo", "lock-exec", "demo", "--", "touch", marker), "repository-busy", "demo")
	if _, err := os.Lstat(marker); !os.IsNotExist(err) {
		t.Fatalf("second lock-exec ran without the lock: %v", err)
	}
	if output := h.ServeWith(short, "serve", "key", "list"); output != "" {
		t.Fatalf("key list beside a running lock-exec = %q", output)
	}
	requireServeAdminContains(t, h.ServeFailureWith(short, "serve", "repo", "set", "demo", "--lfs-quota", "5"), "repository-busy")
	if err := holder.Wait(); err != nil {
		t.Fatalf("holding lock-exec: %v\n%s", err, holderOutput.String())
	}
}

// NFR-008 through the CLI: unsafe state makes administration refuse and
// name the path.
func TestServeAdminRefusesUnsafeState(t *testing.T) {
	for _, tc := range []struct {
		name  string
		plant func(t *testing.T, layout serveLayout) (path, reason string)
	}{
		{"registry mode 0644", func(t *testing.T, layout serveLayout) (string, string) {
			chmodServeTest(t, layout.Registry, 0o644)
			return layout.Registry, "file mode 0644, want 0600"
		}},
		{"symlinked repository state", func(t *testing.T, layout serveLayout) (string, string) {
			target := filepath.Join(t.TempDir(), "elsewhere")
			if err := os.Rename(layout.RepoStateDir("demo"), target); err != nil {
				t.Fatal(err)
			}
			symlinkServeTest(t, target, layout.RepoStateDir("demo"))
			return layout.RepoStateDir("demo"), "is a symbolic link"
		}},
		{"unknown file in state", func(t *testing.T, layout serveLayout) (string, string) {
			path := filepath.Join(layout.State, "notes.txt")
			writeServeTestFile(t, path, "x", 0o600)
			return path, "unexpected entry"
		}},
		{"repository without state", func(t *testing.T, layout serveLayout) (string, string) {
			removeServeTest(t, layout.RepoStateDir("demo"))
			return layout.RepoGitDir("demo"), "hosted repository has no matching state directory"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newServeHarness(t)
			layout := h.Layout()
			h.Serve("serve", "repo", "create", "demo", "--from", serveHarnessSourceRepo(t))
			serveHarnessAddKey(h, serveHarnessActor("alice"))
			path, reason := tc.plant(t, layout)
			want := "hn: hn serve state defect: " + path + ": " + reason
			for _, args := range [][]string{
				{"serve", "repo", "list"},
				{"serve", "repo", "create", "other", "--from", serveHarnessSourceRepo(t)},
				{"serve", "key", "add", "--actor", serveHarnessActor("bob"), "-"},
			} {
				output := h.ServeFailureWith(serveHarnessRun{Stdin: newServeTestKey(t, "").PublicLine}, args...)
				if !strings.HasPrefix(output, want) {
					t.Fatalf("hn %s on unsafe state:\n%s\nwant prefix %q", strings.Join(args, " "), output, want)
				}
			}
			if _, err := os.Lstat(layout.RepoGitDir("other")); !os.IsNotExist(err) {
				t.Fatalf("repo create ran on unsafe state: %v", err)
			}
		})
	}
}

// NFR-004: every administration command refuses uid 0 before it touches
// HOME.
func TestServeAdminRefusesRoot(t *testing.T) {
	h := newServeHarness(t)
	source := serveHarnessSourceRepo(t)
	root := serveHarnessRun{Env: map[string]string{"HN_INTERNAL_TESTING": "1", "HN_TEST_SERVE_EUID": "0"}}
	before := snapshotLegacyPrivateTree(t, h.Home)
	for _, args := range [][]string{
		{"serve", "repo", "create", "demo", "--from", source},
		{"serve", "repo", "set", "demo", "--lfs-quota", "5"},
		{"serve", "key", "add", "--actor", serveHarnessActor("alice"), "-"},
		{"serve", "key", "remove", serveTestFingerprint("alice")},
	} {
		if output := h.ServeFailureWith(root, args...); output != "hn: "+serveRootRefusal+"\n" {
			t.Fatalf("hn %s as root = %q", strings.Join(args, " "), output)
		}
	}
	if after := snapshotLegacyPrivateTree(t, h.Home); !reflect.DeepEqual(before, after) {
		t.Fatalf("root refusal changed HOME:\nbefore %v\nafter  %v", before, after)
	}
}

// An interrupted repo create leaves a staging directory whose Git files
// break the state/tmp rules; the next administration command removes it.
func TestServeAdminRemovesAbandonedCreateStaging(t *testing.T) {
	h := newServeHarness(t)
	layout := h.Layout()
	h.Serve("serve", "repo", "list")
	abandoned := filepath.Join(layout.Tmp, serveCreateStagingPrefix+"123", "demo.git")
	if err := os.MkdirAll(abandoned, 0o755); err != nil {
		t.Fatal(err)
	}
	writeServeTestFile(t, filepath.Join(abandoned, "HEAD"), "ref: refs/heads/main\n", 0o444)
	t.Setenv("HOME", h.Home)
	if err := validateServeLayoutFor(layout, serveSessionValidation, h.Binary); err == nil {
		t.Fatal("session validation accepted an abandoned staging directory")
	}
	if output := h.Serve("serve", "repo", "list"); output != "" {
		t.Fatalf("repo list = %q", output)
	}
	if entries := serveHarnessEntries(t, layout.Tmp); !reflect.DeepEqual(entries, []string{serveAdminLockName}) {
		t.Fatalf("state/tmp after cleanup = %v", entries)
	}
	if err := validateServeLayoutFor(layout, serveSessionValidation, h.Binary); err != nil {
		t.Fatalf("session validation after cleanup: %v", err)
	}
}

// serveAdminTestSSHSnapshot records what a refused command must not touch:
// the type and mode of .ssh and authorized_keys and the bytes reached
// through authorized_keys.
func serveAdminTestSSHSnapshot(t *testing.T, layout serveLayout) []string {
	t.Helper()
	var snapshot []string
	for _, path := range []string{layout.SSHDir, layout.AuthorizedKeys} {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		snapshot = append(snapshot, path+" "+info.Mode().String())
	}
	return append(snapshot, readServeAdminFile(t, layout.AuthorizedKeys))
}

// moveServeAdminTestBehindSymlink moves path to a private directory outside
// HOME, puts a symbolic link to it in its place and returns the undo.
func moveServeAdminTestBehindSymlink(t *testing.T, path string) func() {
	t.Helper()
	target := filepath.Join(t.TempDir(), filepath.Base(path))
	if err := os.Rename(path, target); err != nil {
		t.Fatal(err)
	}
	symlinkServeTest(t, target, path)
	return func() {
		removeServeTest(t, path)
		if err := os.Rename(target, path); err != nil {
			t.Fatal(err)
		}
	}
}

// rewriteServeAdminTestKeys replaces authorized_keys with change applied to
// its contents and returns the undo.
func rewriteServeAdminTestKeys(t *testing.T, layout serveLayout, change func(string) string) func() {
	t.Helper()
	original := readServeAdminFile(t, layout.AuthorizedKeys)
	writeServeTestFile(t, layout.AuthorizedKeys, change(original), 0o600)
	return func() { writeServeTestFile(t, layout.AuthorizedKeys, original, 0o600) }
}

// NFR-008, FR-004: unsafe .ssh or authorized_keys state and broken managed
// block markers make every administration command refuse and name the path,
// the read-only commands and lock-exec included. Nothing is written or
// repaired, and a refused lock-exec never starts its command. Once the state
// is fixed, administration works again.
func TestServeAdminRefusesUnsafeAuthorizedKeys(t *testing.T) {
	h := newServeHarness(t)
	layout := h.Layout()
	h.Serve("serve", "repo", "create", "demo", "--from", serveHarnessSourceRepo(t))
	key := serveHarnessAddKey(h, serveHarnessActor("alice"))
	source := serveHarnessSourceRepo(t)
	block := readServeAdminFile(t, layout.AuthorizedKeys)
	if !strings.HasPrefix(block, serveAuthorizedKeysBegin+"\n") || !strings.HasSuffix(block, serveAuthorizedKeysEnd+"\n") {
		t.Fatalf("authorized_keys after key add:\n%s", block)
	}

	for _, tc := range []struct {
		name   string
		path   string
		reason string
		plant  func(t *testing.T) (undo func())
	}{
		{".ssh writable by others", layout.SSHDir, "directory mode 0777 is writable by group or others", func(t *testing.T) func() {
			chmodServeTest(t, layout.SSHDir, 0o777)
			return func() { chmodServeTest(t, layout.SSHDir, 0o700) }
		}},
		{".ssh is a symbolic link", layout.SSHDir, "is a symbolic link", func(t *testing.T) func() {
			return moveServeAdminTestBehindSymlink(t, layout.SSHDir)
		}},
		{"authorized_keys writable by group", layout.AuthorizedKeys, "file mode 0660 is writable by group or others", func(t *testing.T) func() {
			chmodServeTest(t, layout.AuthorizedKeys, 0o660)
			return func() { chmodServeTest(t, layout.AuthorizedKeys, 0o600) }
		}},
		{"authorized_keys is a symbolic link", layout.AuthorizedKeys, "is a symbolic link", func(t *testing.T) func() {
			return moveServeAdminTestBehindSymlink(t, layout.AuthorizedKeys)
		}},
		{"duplicate begin marker", layout.AuthorizedKeys, "is a second managed block begin marker", func(t *testing.T) func() {
			return rewriteServeAdminTestKeys(t, layout, func(s string) string { return s + serveAuthorizedKeysBegin + "\n" })
		}},
		{"duplicate end marker", layout.AuthorizedKeys, "is a second managed block end marker", func(t *testing.T) func() {
			return rewriteServeAdminTestKeys(t, layout, func(s string) string { return s + serveAuthorizedKeysEnd + "\n" })
		}},
		{"begin marker without end marker", layout.AuthorizedKeys, "the managed block begin marker has no following end marker", func(t *testing.T) func() {
			return rewriteServeAdminTestKeys(t, layout, func(s string) string { return strings.TrimSuffix(s, serveAuthorizedKeysEnd+"\n") })
		}},
		{"end marker without begin marker", layout.AuthorizedKeys, "end marker without a preceding begin marker", func(t *testing.T) func() {
			return rewriteServeAdminTestKeys(t, layout, func(s string) string { return strings.TrimPrefix(s, serveAuthorizedKeysBegin+"\n") })
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			undo := tc.plant(t)
			registry := readServeAdminFile(t, layout.Registry)
			config := readServeAdminFile(t, layout.RepoConfig("demo"))
			ssh := serveAdminTestSSHSnapshot(t, layout)
			hosted := serveAdminTestHostedState(t, layout)
			started := filepath.Join(t.TempDir(), "started")
			want := "hn: hn serve state defect: " + tc.path + ": "
			for _, args := range [][]string{
				{"serve", "repo", "list"},
				{"serve", "repo", "show", "demo"},
				{"serve", "key", "list"},
				{"serve", "repo", "lock-exec", "demo", "--", "touch", started},
				{"serve", "key", "add", "--actor", serveHarnessActor("bob"), "-"},
				{"serve", "key", "remove", key.Fingerprint},
				{"serve", "repo", "set", "demo", "--lfs-quota", "5"},
				{"serve", "repo", "create", "other", "--from", source},
			} {
				output := h.ServeFailureWith(serveHarnessRun{Stdin: newServeTestKey(t, "").PublicLine}, args...)
				if !strings.HasPrefix(output, want) || !strings.Contains(output, tc.reason) || strings.Count(output, "\n") != 1 {
					t.Fatalf("hn %s:\n%s\nwant one line starting %q and containing %q", strings.Join(args, " "), output, want, tc.reason)
				}
			}
			if _, err := os.Lstat(started); !os.IsNotExist(err) {
				t.Fatalf("refused lock-exec started its command: %v", err)
			}
			if readServeAdminFile(t, layout.Registry) != registry || readServeAdminFile(t, layout.RepoConfig("demo")) != config {
				t.Fatal("a refused command changed the registry or the repository configuration")
			}
			if after := serveAdminTestSSHSnapshot(t, layout); !reflect.DeepEqual(after, ssh) {
				t.Fatalf("a refused command changed .ssh:\nbefore %q\nafter  %q", ssh, after)
			}
			if after := serveAdminTestHostedState(t, layout); !reflect.DeepEqual(after, hosted) {
				t.Fatalf("a refused command changed hosted state:\nbefore %v\nafter  %v", hosted, after)
			}

			undo()
			if output := h.Serve("serve", "repo", "list"); output != "demo\n" {
				t.Fatalf("repo list after the fix = %q", output)
			}
			if got := readServeAdminFile(t, layout.AuthorizedKeys); got != block {
				t.Fatalf("authorized_keys after the fix:\n%s\nwant:\n%s", got, block)
			}
		})
	}
}

// A registry binding whose stored public key does not parse cannot be
// written into the block. Commands that change state refuse; the read-only
// commands and lock-exec warn, leave authorized_keys as it is and still run,
// so key list finds the binding and key remove repairs the registry.
func TestServeAdminRegistryBindingDefect(t *testing.T) {
	h := newServeHarness(t)
	layout := h.Layout()
	h.Serve("serve", "repo", "create", "demo", "--from", serveHarnessSourceRepo(t))
	alice := serveHarnessAddKey(h, serveHarnessActor("alice"))
	block := readServeAdminFile(t, layout.AuthorizedKeys)

	t.Setenv("HOME", h.Home)
	registry, err := loadServeRegistry(layout)
	if err != nil {
		t.Fatal(err)
	}
	broken := serveTestBinding("mallory")
	registry.Keys = append(registry.Keys, broken)
	sort.Slice(registry.Keys, func(i, j int) bool { return registry.Keys[i].Fingerprint < registry.Keys[j].Fingerprint })
	writeServeTestRegistry(t, layout, registry)
	stored := readServeAdminFile(t, layout.Registry)

	warning := "hn serve: warning: authorized_keys was left unchanged because " + layout.Registry +
		" holds a binding it cannot write: fingerprint " + broken.Fingerprint + " has an invalid public key"
	requireServeAdminContains(t, h.Serve("serve", "key", "list"), warning,
		alice.Fingerprint+" "+serveHarnessActor("alice")+" ssh-ed25519\n", broken.Fingerprint+" "+broken.Actor+" ssh-ed25519\n")
	requireServeAdminContains(t, h.Serve("serve", "repo", "list"), warning, "demo\n")
	requireServeAdminContains(t, h.Serve("serve", "repo", "show", "demo"), warning, "name: demo\n")
	started := filepath.Join(t.TempDir(), "started")
	requireServeAdminContains(t, h.Serve("serve", "repo", "lock-exec", "demo", "--", "touch", started), warning)
	if _, err := os.Lstat(started); err != nil {
		t.Fatalf("lock-exec did not run its command: %v", err)
	}

	for _, args := range [][]string{
		{"serve", "key", "add", "--actor", serveHarnessActor("bob"), "-"},
		{"serve", "repo", "set", "demo", "--lfs-quota", "5"},
		{"serve", "repo", "create", "other", "--from", serveHarnessSourceRepo(t)},
	} {
		output := h.ServeFailureWith(serveHarnessRun{Stdin: newServeTestKey(t, "").PublicLine}, args...)
		requireServeAdminContains(t, output, "fingerprint "+broken.Fingerprint+" has an invalid public key")
	}
	if readServeAdminFile(t, layout.Registry) != stored || readServeAdminFile(t, layout.AuthorizedKeys) != block {
		t.Fatal("a command changed the registry or authorized_keys while a binding could not be written")
	}
	if _, err := os.Lstat(layout.RepoGitDir("other")); !os.IsNotExist(err) {
		t.Fatalf("repo create ran while a binding could not be written: %v", err)
	}

	if output := h.Serve("serve", "key", "remove", broken.Fingerprint); output != "Removed "+broken.Fingerprint+" from actor "+broken.Actor+"\n" {
		t.Fatalf("key remove of the broken binding = %q", output)
	}
	if output := h.Serve("serve", "key", "list"); output != alice.Fingerprint+" "+serveHarnessActor("alice")+" ssh-ed25519\n" {
		t.Fatalf("key list after the repair = %q", output)
	}
	if got := readServeAdminFile(t, layout.AuthorizedKeys); got != block {
		t.Fatalf("authorized_keys after the repair:\n%s\nwant:\n%s", got, block)
	}
}

// The state writers produce byte-stable files that the validators accept.
func TestServeAdminStateWritersRoundTrip(t *testing.T) {
	layout := newServeTestLayout(t)

	registry := serveTestRegistry("carol", "alice", "bob")
	registry.Keys[0], registry.Keys[2] = registry.Keys[2], registry.Keys[0]
	if err := saveServeRegistry(layout, registry); err != nil {
		t.Fatal(err)
	}
	stored := readServeAdminFile(t, layout.Registry)
	loaded, err := loadServeRegistry(layout)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, serveTestRegistry("alice", "bob", "carol")) {
		t.Fatalf("saved registry loads as %+v", loaded)
	}
	encoded, err := encodeServeRegistry(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != stored || !strings.HasSuffix(stored, "}\n") {
		t.Fatalf("registry round trip is not byte-stable:\n%s\nvs\n%s", encoded, stored)
	}
	requireServeKeysMode(t, layout.Registry, 0o600)
	if err := saveServeRegistry(layout, serveRegistry{Version: 1}); err != nil {
		t.Fatal(err)
	}
	if got := readServeAdminFile(t, layout.Registry); got != "{\n  \"version\": 1,\n  \"keys\": []\n}\n" {
		t.Fatalf("empty registry = %q", got)
	}
	duplicate := serveTestRegistry("alice")
	duplicate.Keys = append(duplicate.Keys, duplicate.Keys[0])
	requireServeErrorContains(t, saveServeRegistry(layout, duplicate), "duplicate fingerprint")

	addServeTestRepo(t, layout, "demo")
	config, err := newServeRepoConfig("demo", []string{"refs/heads/trunk", "refs/heads/release", "refs/heads/alpha", "refs/heads/release", "refs/heads/trunk"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"refs/heads/trunk", "refs/heads/alpha", "refs/heads/release"}; !reflect.DeepEqual(config.Protected, want) {
		t.Fatalf("protected = %v, want %v", config.Protected, want)
	}
	if config.LFSQuotaBytes != 1<<30 || config.Budgets != defaultReplicationBudgets() || config.Version != 1 {
		t.Fatalf("defaults = %+v", config)
	}
	if err := saveServeRepoConfig(layout, config); err != nil {
		t.Fatal(err)
	}
	reloaded, err := loadServeRepoConfig(layout, "demo")
	if err != nil {
		t.Fatal(err)
	}
	again, err := encodeServeRepoConfig(reloaded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reloaded, config) || string(again) != readServeAdminFile(t, layout.RepoConfig("demo")) {
		t.Fatalf("config round trip is not byte-stable:\n%s", again)
	}
	if defaults, err := newServeRepoConfig("demo", nil); err != nil || !reflect.DeepEqual(defaults.Protected, []string{"refs/heads/main"}) {
		t.Fatalf("default protected = %v (%v)", defaults.Protected, err)
	}
	config.LFSQuotaBytes = 0
	requireServeErrorContains(t, saveServeRepoConfig(layout, config), "lfsQuotaBytes must be a positive integer")
	if err := validateServeLayout(layout, serveSessionValidation); err != nil {
		t.Fatalf("layout after the writers: %v", err)
	}
}

// FR-023 (part): the two administration examples in the serve guide run as
// written, with fixture values for the illustrative actor, key, user, host
// and paths.
func TestServeAdminDocumentedExamples(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("docs", "serve-v0.md"))
	if err != nil {
		t.Fatal(err)
	}
	guide := string(contents)
	h := newServeHarness(t)
	source := serveHarnessSourceRepo(t)
	key := newServeTestKey(t, "alice@laptop")
	keyFile := filepath.Join(t.TempDir(), "alice.pub")
	writeServeTestFile(t, keyFile, key.PublicLine+"\n", 0o644)

	const (
		documentedSource      = "/srv/import/rust-pingpong.git"
		documentedAccount     = "hn-serve@git.example.internal"
		documentedFingerprint = "SHA256:Jd8bC8QWw0m0Y4l6m6yZ1gq5q3o9s0Kc2b2f4QwA9eU"
	)
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	host, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	for _, example := range []struct {
		name    string
		marker  string
		command map[string]string
		output  map[string]string
	}{
		{"repo-create", "<!-- example: repo-create -->", map[string]string{documentedSource: source}, map[string]string{documentedAccount: account.Username + "@" + host}},
		{"key-add", "<!-- example: key-add -->", map[string]string{"alice.pub": keyFile}, map[string]string{documentedFingerprint: key.Fingerprint}},
	} {
		t.Run(example.name, func(t *testing.T) {
			command, output := serveAdminTestExample(t, guide, example.marker)
			fields := strings.Fields(command)
			if len(fields) < 5 || strings.Join(fields[:4], " ") != "sudo -u hn-serve hn" {
				t.Fatalf("example %s command %q does not run hn as the server account", example.name, command)
			}
			args := fields[4:]
			for index, arg := range args {
				if replacement, ok := example.command[arg]; ok {
					args[index] = replacement
				}
			}
			for documented, actual := range example.output {
				if !strings.Contains(output, documented) {
					t.Fatalf("example %s output does not show %q:\n%s", example.name, documented, output)
				}
				output = strings.ReplaceAll(output, documented, actual)
			}
			got := strings.Split(h.Serve(args...), "\n")
			want := strings.Split(output, "\n")
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("example %s output:\n%s\nwant:\n%s", example.name, strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
		})
	}
}

// serveAdminTestExample returns the one command line and the output block
// that follow the example marker. Callers pass the complete marker comment
// so the documented-example guard of serve_quality_test.go finds it.
func serveAdminTestExample(t *testing.T, guide, marker string) (string, string) {
	t.Helper()
	name := strings.TrimSuffix(strings.TrimPrefix(marker, "<!-- example: "), " -->")
	marker += "\n"
	if strings.Count(guide, marker) != 1 {
		t.Fatalf("the serve guide must contain exactly one %q", strings.TrimSpace(marker))
	}
	_, after, _ := strings.Cut(guide, marker)
	command, after, ok := serveAdminTestFence(after, "sh")
	if !ok || strings.Count(command, "\n") != 1 {
		t.Fatalf("example %s: want a sh block with one command line after the marker", name)
	}
	output, _, ok := serveAdminTestFence(after, "text")
	if !ok {
		t.Fatalf("example %s: want a text output block after the command", name)
	}
	return strings.TrimSuffix(command, "\n"), output
}

// serveAdminTestFence returns the body of the fenced block that opens at the
// start of text, allowing only blank lines before it.
func serveAdminTestFence(text, language string) (string, string, bool) {
	text = strings.TrimLeft(text, "\n")
	body, ok := strings.CutPrefix(text, "```"+language+"\n")
	if !ok {
		return "", "", false
	}
	block, rest, ok := strings.Cut(body, "```\n")
	return block, rest, ok
}
