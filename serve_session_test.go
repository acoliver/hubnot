package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// T034: the SSH_ORIGINAL_COMMAND grammar.

func TestParseServeRequestAcceptsEveryGrammarForm(t *testing.T) {
	longName := strings.Repeat("a", serveRepositoryNameMaximum)
	for _, tc := range []struct {
		command string
		want    serveRequest
	}{
		{"git-upload-pack 'r.git'", serveRequest{Service: serveServiceUploadPack, Repo: "r"}},
		{"git-upload-pack '/r.git'", serveRequest{Service: serveServiceUploadPack, Repo: "r"}},
		{"git-upload-pack 'r'", serveRequest{Service: serveServiceUploadPack, Repo: "r"}},
		{"git-upload-pack '/r'", serveRequest{Service: serveServiceUploadPack, Repo: "r"}},
		{"git-upload-pack r.git", serveRequest{Service: serveServiceUploadPack, Repo: "r"}},
		{"git-upload-pack /r.git", serveRequest{Service: serveServiceUploadPack, Repo: "r"}},
		{"git-upload-pack r", serveRequest{Service: serveServiceUploadPack, Repo: "r"}},
		{"git-upload-pack /r", serveRequest{Service: serveServiceUploadPack, Repo: "r"}},
		{"git upload-pack 'r.git'", serveRequest{Service: serveServiceUploadPack, Repo: "r"}},
		{"git upload-pack /r", serveRequest{Service: serveServiceUploadPack, Repo: "r"}},
		{"git-receive-pack '/r.git'", serveRequest{Service: serveServiceReceivePack, Repo: "r"}},
		{"git-receive-pack r", serveRequest{Service: serveServiceReceivePack, Repo: "r"}},
		{"git receive-pack 'r.git'", serveRequest{Service: serveServiceReceivePack, Repo: "r"}},
		{"git receive-pack /r.git", serveRequest{Service: serveServiceReceivePack, Repo: "r"}},
		{"git-lfs-transfer 'r.git' download", serveRequest{Service: serveServiceLFSTransfer, Repo: "r", LFSOp: "download"}},
		{"git-lfs-transfer '/r.git' upload", serveRequest{Service: serveServiceLFSTransfer, Repo: "r", LFSOp: "upload"}},
		{"git-lfs-transfer r upload", serveRequest{Service: serveServiceLFSTransfer, Repo: "r", LFSOp: "upload"}},
		{"git-upload-pack 'my-repo_1.v2.git'", serveRequest{Service: serveServiceUploadPack, Repo: "my-repo_1.v2"}},
		{"git-upload-pack '" + longName + ".git'", serveRequest{Service: serveServiceUploadPack, Repo: longName}},
	} {
		got, err := parseServeRequest(tc.command)
		if err != nil {
			t.Fatalf("parseServeRequest(%q) = %v", tc.command, err)
		}
		if got != tc.want {
			t.Fatalf("parseServeRequest(%q) = %+v, want %+v", tc.command, got, tc.want)
		}
	}
}

func TestParseServeRequestRefusesEverythingElse(t *testing.T) {
	for _, command := range []string{
		"",
		"sh",
		"/bin/sh -c id",
		"bash",
		"scp -t /tmp",
		"sftp",
		"internal-sftp",
		"git-upload-archive 'r.git'",
		"git-lfs-authenticate r.git download",
		"git-upload-pack",
		"git-upload-pack ",
		"git-upload-pack 'r.git' extra",
		"git-upload-pack 'r.git';id",
		"git-upload-pack 'r.git'|id",
		"git-upload-pack 'r.git'&id",
		"git-upload-pack 'r.git'>out",
		"git-upload-pack <in",
		`git-upload-pack "r.git"`,
		`git-upload-pack r\.git`,
		"git-upload-pack 'r.git'\nid",
		"git-upload-pack\t'r.git'",
		"git-upload-pack  'r.git'",
		" git-upload-pack 'r.git'",
		"git-upload-pack 'r.git' ",
		"git  upload-pack 'r.git'",
		"GIT-UPLOAD-PACK 'r.git'",
		"git-upload-pack $(id)",
		"git-upload-pack '$(id).git'",
		"git-upload-pack `id`",
		"git-upload-pack 'r.git",
		"git-upload-pack r.git'",
		"git-upload-pack 'r'.git",
		"git-upload-pack 'r.git'x",
		"git-upload-pack '",
		"git-upload-pack ''",
		"git-upload-pack '/'",
		"git-upload-pack '.git'",
		"git-upload-pack '../r.git'",
		"git-upload-pack '/a/r.git'",
		"git-upload-pack 'a/r.git'",
		"git-upload-pack '//r.git'",
		"git-upload-pack 'r.git.git'",
		"git-upload-pack '.r'",
		"git-upload-pack 'r..x'",
		"git-upload-pack -h",
		"git-upload-pack --strict",
		"git-upload-pack '" + strings.Repeat("a", serveRepositoryNameMaximum+1) + "'",
		"git-upload-pack 'r\u00e9.git'",
		"git-upload-pack 'r\x7f.git'",
		"git-upload-pack 'r\x00.git'",
		"git-lfs-transfer 'r.git'",
		"git-lfs-transfer 'r.git' delete",
		"git-lfs-transfer 'r.git' download extra",
		"git-lfs-transfer 'r.git'  download",
		"git-lfs-transfer 'a/r.git' download",
	} {
		_, err := parseServeRequest(command)
		var refusal *serveRequestError
		if !errors.As(err, &refusal) {
			t.Fatalf("parseServeRequest(%q) = %v, want a serveRequestError", command, err)
		}
	}
}

// Maintainer note from WP01: lookupServeKey over the validated registry.
func TestLookupServeKeyFindsOnlyBoundFingerprints(t *testing.T) {
	registry := emptyServeRegistry()
	for _, seed := range []string{"a", "b", "c", "d"} {
		registry.Keys = append(registry.Keys, serveTestBinding(seed))
	}
	encoded, err := encodeServeRegistry(registry)
	if err != nil {
		t.Fatal(err)
	}
	registry, err = decodeServeRegistry(encoded)
	if err != nil {
		t.Fatal(err)
	}
	for _, binding := range registry.Keys {
		got, ok := lookupServeKey(registry, binding.Fingerprint)
		if !ok || got != binding {
			t.Fatalf("lookupServeKey(%s) = %+v, %v; want %+v", binding.Fingerprint, got, ok, binding)
		}
	}
	for _, fingerprint := range []string{serveTestFingerprint("unbound"), "", "SHA256:", "sha256:x", registry.Keys[0].Fingerprint + "x"} {
		if got, ok := lookupServeKey(registry, fingerprint); ok {
			t.Fatalf("lookupServeKey(%q) = %+v, want no binding", fingerprint, got)
		}
	}
	if _, ok := lookupServeKey(emptyServeRegistry(), registry.Keys[0].Fingerprint); ok {
		t.Fatal("lookupServeKey found a binding in an empty registry")
	}
}

// T036: the environment Git receives.

func TestBuildServeGitEnvironmentKeepsOnlyServerValues(t *testing.T) {
	layout := serveLayout{Home: "/srv/hn", Tmp: "/srv/hn/.hn-serve/state/tmp"}
	binding := serveKeyBinding{Fingerprint: serveTestFingerprint("k"), Actor: serveStateTestActor("mapped")}
	hostile := []string{
		"HN_SERVE_ACTOR=evil",
		"HN_SERVE_KEY=evil",
		"HN_SERVE_REPOSITORY=evil",
		"GIT_DIR=/tmp/x",
		"GIT_CONFIG_GLOBAL=/tmp/evil.gitconfig",
		"GIT_CONFIG_PARAMETERS='core.hooksPath'='/tmp'",
		"GIT_EXEC_PATH=/tmp",
		"LD_PRELOAD=/tmp/evil.so",
		"HOME=/home/evil",
		"TMPDIR=/tmp/evil",
		"LANG=xx",
		"SSH_ORIGINAL_COMMAND=git-upload-pack 'r.git'",
		"HN_TEST_SERVE_LOCK_WAIT=1ms",
		"PATH=/usr/bin:/bin",
		"PATH=/evil",
	}
	want := []string{
		"PATH=/usr/bin:/bin",
		"HOME=/srv/hn",
		"LANG=C",
		"GIT_CONFIG_NOSYSTEM=1",
		"TMPDIR=/srv/hn/.hn-serve/state/tmp",
		"HN_SERVE_ACTOR=" + binding.Actor,
		"HN_SERVE_KEY=" + binding.Fingerprint,
		"HN_SERVE_REPOSITORY=r",
	}
	withProtocol := slices.Insert(slices.Clone(want), 5, "GIT_PROTOCOL=version=2")
	for _, tc := range []struct {
		name   string
		parent []string
		want   []string
	}{
		{"hostile without protocol", hostile, want},
		{"malformed protocol", append(slices.Clone(hostile), "GIT_PROTOCOL=version=2:evil"), want},
		{"protocol with extra keys", append(slices.Clone(hostile), "GIT_PROTOCOL=version=2:object-format=sha256"), want},
		{"two-digit protocol", append(slices.Clone(hostile), "GIT_PROTOCOL=version=22"), want},
		{"empty protocol", append(slices.Clone(hostile), "GIT_PROTOCOL="), want},
		{"exact protocol", append(slices.Clone(hostile), "GIT_PROTOCOL=version=2"), withProtocol},
		{"malformed protocol first", append(slices.Clone(hostile), "GIT_PROTOCOL=version=2:evil", "GIT_PROTOCOL=version=2"), want},
		{"testing gate other than 1", append(slices.Clone(hostile), "HN_INTERNAL_TESTING=yes"), want},
		{"no PATH", []string{"HN_SERVE_ACTOR=evil"}, want[1:]},
	} {
		if got := buildServeGitEnvironment(tc.parent, layout, binding, "r"); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s:\ngot  %q\nwant %q", tc.name, got, tc.want)
		}
	}

	gated := append(slices.Clone(hostile), "HN_INTERNAL_TESTING=1", "HN_TEST_SERVE_EUID=1000", "HN_TEST_SERVE_LOCK_WAIT=2ms", "HN_TEST_OTHER=x", "HN_TEST_SERVE_NOVALUE")
	wantTesting := append(slices.Clone(want), "HN_INTERNAL_TESTING=1", "HN_TEST_SERVE_LOCK_WAIT=1ms", "HN_TEST_SERVE_EUID=1000")
	if got := buildServeGitEnvironment(gated, layout, binding, "r"); !reflect.DeepEqual(got, wantTesting) {
		t.Fatalf("testing exception:\ngot  %q\nwant %q", got, wantTesting)
	}
}

// serveSessionFixture is a server with one hosted repository r and one
// bound participant key.
type serveSessionFixture struct {
	h     *serveHarness
	key   serveTestKey
	actor string
}

func newServeSessionFixture(t *testing.T) serveSessionFixture {
	t.Helper()
	h := newServeHarness(t)
	h.Serve("serve", "repo", "create", "r", "--from", serveHarnessSourceRepo(t))
	actor := serveHarnessActor("participant")
	return serveSessionFixture{h: h, key: serveHarnessAddKey(h, actor), actor: actor}
}

// serveSessionResult is one direct hn serve git invocation with stdout and
// stderr kept apart, because stdout is Git protocol data.
type serveSessionResult struct {
	Stdout string
	Stderr string
	Status int
}

// runServeSessionProgram runs program with args and environment, keeping
// stdout and stderr apart.
func runServeSessionProgram(t *testing.T, dir string, environment []string, program string, args ...string) serveSessionResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, program, args...)
	command.Dir = dir
	command.Env = environment
	command.Stdin = strings.NewReader("")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if ctx.Err() != nil {
		t.Fatalf("%s %s timed out: %v\n%s", program, strings.Join(args, " "), ctx.Err(), stderr.String())
	}
	result := serveSessionResult{Stdout: stdout.String(), Stderr: stderr.String()}
	var exitErr *exec.ExitError
	switch {
	case errors.As(err, &exitErr):
		result.Status = exitErr.ExitCode()
	case err != nil:
		t.Fatalf("%s %s: %v", program, strings.Join(args, " "), err)
	}
	return result
}

// serveSessionDirect runs hn serve git --key fingerprint the way sshd would,
// with request as SSH_ORIGINAL_COMMAND unless it is nil.
func serveSessionDirect(t *testing.T, h *serveHarness, fingerprint string, request *string, extra map[string]string) serveSessionResult {
	t.Helper()
	environment := map[string]string{}
	for name, value := range extra {
		environment[name] = value
	}
	if request != nil {
		environment["SSH_ORIGINAL_COMMAND"] = *request
	}
	return runServeSessionProgram(t, h.Home, serveHarnessEnvironment(h.Home, environment), h.Binary, "serve", "git", "--key", fingerprint)
}

// serveSessionViaShim runs the shim the way Git runs ssh, with request as
// the remote command.
func serveSessionViaShim(t *testing.T, client serveHarnessClientEnv, request string) serveSessionResult {
	t.Helper()
	return runServeSessionProgram(t, client.Home, client.Env, client.Shim, "-o", "SendEnv=GIT_PROTOCOL", "hn-serve@localhost", request)
}

func serveSessionRequest(request string) *string {
	return &request
}

// requireServeSessionRefusal checks NFR-003's refusal shape: exit 1, empty
// stdout, and stderr made of exactly one parseable hn-serve/1 line with
// ref=- and its hn-serve: sentence.
func requireServeSessionRefusal(t *testing.T, label string, result serveSessionResult, code, actor, sentence string) {
	t.Helper()
	if result.Status != 1 {
		t.Fatalf("%s: exit %d, want 1\nstdout %q\nstderr %q", label, result.Status, result.Stdout, result.Stderr)
	}
	if result.Stdout != "" {
		t.Fatalf("%s: stdout %q, want empty", label, result.Stdout)
	}
	lines := strings.Split(strings.TrimSuffix(result.Stderr, "\n"), "\n")
	if len(lines) != 2 || !strings.HasSuffix(result.Stderr, "\n") {
		t.Fatalf("%s: stderr %q, want one rejection line and one sentence line", label, result.Stderr)
	}
	rejection, err := parseServeRejectionLine(lines[0])
	if err != nil {
		t.Fatalf("%s: rejection line %q does not parse: %v", label, lines[0], err)
	}
	if actor == "" {
		actor = "-"
	}
	if rejection.Ref != "-" || rejection.Code != code || rejection.Actor != actor {
		t.Fatalf("%s: rejection %+v, want ref=- code=%s actor=%s", label, rejection, code, actor)
	}
	if rejection.Next != serveReasonDefaultNext(code) {
		t.Fatalf("%s: next %q, want %q", label, rejection.Next, serveReasonDefaultNext(code))
	}
	if want := serveSentencePrefix + sentence; lines[1] != want {
		t.Fatalf("%s: sentence %q, want %q", label, lines[1], want)
	}
}

// serveSessionSnapshot captures every hosted ref and every file below the
// server HOME, contents included.
func serveSessionSnapshot(t *testing.T, h *serveHarness) map[string]string {
	t.Helper()
	snapshot := snapshotLegacyPrivateTree(t, h.Home)
	layout := h.Layout()
	for _, entry := range serveHarnessEntries(t, layout.Repositories) {
		path := filepath.Join(layout.Repositories, entry)
		if info, err := os.Lstat(path); err != nil || !info.IsDir() {
			continue
		}
		snapshot["refs:"+entry] = serveHarnessGit(t, path, "for-each-ref", "--format=%(objectname) %(refname)")
	}
	return snapshot
}

func requireServeSessionUnchanged(t *testing.T, label string, before, after map[string]string) {
	t.Helper()
	if !reflect.DeepEqual(before, after) {
		for name, value := range after {
			if before[name] != value {
				t.Errorf("%s changed or added %s", label, name)
			}
		}
		for name := range before {
			if _, ok := after[name]; !ok {
				t.Errorf("%s removed %s", label, name)
			}
		}
		t.FailNow()
	}
}

const (
	serveSessionUnknownKeySentence  = "this SSH key is not registered with the server."
	serveSessionUnsupportedSentence = "only git fetch, git push and Git LFS transfer are supported."
	serveSessionLFSSentence         = "Git LFS transfer is not enabled on this server."
)

// T039 US2-1: clone and fetch through the exact forced command, under both
// protocol versions.
func TestServeSessionCloneAndFetchOverSSH(t *testing.T) {
	fixture := newServeSessionFixture(t)
	h := fixture.h
	hosted := h.Layout().RepoGitDir("r")
	client := serveHarnessClient(t, h, fixture.key, nil)
	traced := client
	traced.Env = append(slices.Clone(client.Env), "GIT_TRACE_PACKET=1")

	clones := map[string]string{}
	for _, version := range []string{"0", "2"} {
		directory := filepath.Join(t.TempDir(), "clone-v"+version)
		output := traced.Git("", "-c", "protocol.version="+version, "clone", serveHarnessCloneURL("r"), directory)
		if negotiated := strings.Contains(output, "< version 2"); negotiated != (version == "2") {
			t.Fatalf("protocol.version=%s: server answered version 2 = %v, want %v\n%s", version, negotiated, version == "2", output)
		}
		if got, want := strings.TrimSpace(client.Git(directory, "rev-parse", "HEAD")), serveHarnessGit(t, hosted, "rev-parse", "refs/heads/main"); got != want {
			t.Fatalf("protocol.version=%s clone HEAD = %s, want hosted main %s", version, got, want)
		}
		clones[version] = directory
	}

	// hn identifies the server through the same forced command: the marker
	// ref is advertised over SSH, and an ordinary remote is not identified.
	if output := client.Hn(clones["2"], "replication", "select", "origin", "--all-trusted"); !strings.Contains(output, "all-trusted: true\n") {
		t.Fatalf("all-trusted selection over SSH:\n%s", output)
	}
	client.Git(clones["2"], "remote", "add", "plain", clones["0"])
	if output := client.HnFailure(clones["2"], "replication", "select", "plain", "--all-trusted"); !strings.Contains(output, "does not identify as hn serve") {
		t.Fatalf("all-trusted selection of an ordinary remote:\n%s", output)
	}

	h.Serve("serve", "repo", "lock-exec", "r", "--", "git", "update-ref", "refs/heads/feature", "refs/heads/release")
	release := serveHarnessGit(t, hosted, "rev-parse", "refs/heads/release")
	for version, directory := range clones {
		client.Git(directory, "-c", "protocol.version="+version, "fetch", "origin")
		if got := strings.TrimSpace(client.Git(directory, "rev-parse", "refs/remotes/origin/feature")); got != release {
			t.Fatalf("protocol.version=%s fetch: origin/feature = %s, want %s", version, got, release)
		}
	}
}

// T039 US2-2: an unbound or malformed key is refused before anything else.
func TestServeSessionUnknownKeyRefused(t *testing.T) {
	fixture := newServeSessionFixture(t)
	h := fixture.h
	before := serveSessionSnapshot(t, h)
	unbound := newServeTestKey(t, "stranger@laptop")
	request := serveSessionRequest("git-upload-pack 'r.git'")
	for _, fingerprint := range []string{unbound.Fingerprint, "", "SHA256:short", "not-a-fingerprint", strings.ToLower(fixture.key.Fingerprint)} {
		result := serveSessionDirect(t, h, fingerprint, request, nil)
		requireServeSessionRefusal(t, "--key "+fingerprint, result, serveReasonUnknownKey, "", serveSessionUnknownKeySentence)
	}
	// The key check comes first: an unknown key with a bad request is
	// still unknown-key.
	requireServeSessionRefusal(t, "unknown key and bad request", serveSessionDirect(t, h, unbound.Fingerprint, serveSessionRequest("sh"), nil),
		serveReasonUnknownKey, "", serveSessionUnknownKeySentence)
	requireServeSessionRefusal(t, "unknown key and no request", serveSessionDirect(t, h, unbound.Fingerprint, nil, nil),
		serveReasonUnknownKey, "", serveSessionUnknownKeySentence)

	stranger := serveHarnessClient(t, h, unbound, nil)
	shim := serveSessionViaShim(t, stranger, "git-upload-pack 'r.git'")
	if shim.Status != 255 || shim.Stdout != "" || shim.Stderr != "Permission denied (publickey).\n" {
		t.Fatalf("shim for an unregistered key = %+v, want exit 255 and the sshd refusal", shim)
	}
	output := stranger.GitFailure("", "clone", serveHarnessCloneURL("r"), filepath.Join(t.TempDir(), "clone"))
	if !strings.Contains(output, "Permission denied (publickey).") {
		t.Fatalf("clone with an unregistered key:\n%s", output)
	}
	requireServeSessionUnchanged(t, "unknown-key refusals", before, serveSessionSnapshot(t, h))
}

// T039 US2-3: every request outside the grammar is unsupported-command and
// changes nothing.
func TestServeSessionUnsupportedCommandsRefusedWithoutChanges(t *testing.T) {
	fixture := newServeSessionFixture(t)
	h := fixture.h
	client := serveHarnessClient(t, h, fixture.key, nil)
	before := serveSessionSnapshot(t, h)

	requireServeSessionRefusal(t, "no SSH_ORIGINAL_COMMAND", serveSessionDirect(t, h, fixture.key.Fingerprint, nil, nil),
		serveReasonUnsupportedCommand, fixture.actor, serveSessionUnsupportedSentence)
	for _, request := range []string{
		"",
		"sh",
		"/bin/sh -c id",
		"scp -t /tmp",
		"sftp",
		"git-upload-archive 'r.git'",
		"git-lfs-authenticate r.git download",
		"git-upload-pack 'r.git' extra",
		"git-upload-pack 'r.git';id",
		`git-upload-pack "r.git"`,
		"git-upload-pack 'r.git'\nid",
		"git-upload-pack\t'r.git'",
		"git-upload-pack $(id)",
		"git-upload-pack `id`",
		"git-receive-pack 'r.git' && touch pwned",
	} {
		requireServeSessionRefusal(t, "direct "+request, serveSessionDirect(t, h, fixture.key.Fingerprint, &request, nil),
			serveReasonUnsupportedCommand, fixture.actor, serveSessionUnsupportedSentence)
		requireServeSessionRefusal(t, "shim "+request, serveSessionViaShim(t, client, request),
			serveReasonUnsupportedCommand, fixture.actor, serveSessionUnsupportedSentence)
	}
	requireServeSessionUnchanged(t, "unsupported-command refusals", before, serveSessionSnapshot(t, h))
}

// T039 US2-4: unsafe paths are grammar failures; a well-formed name that is
// not hosted, or whose directory is a symlink, is refused without changes.
func TestServeSessionUnsafeAndUnhostedPathsRefused(t *testing.T) {
	fixture := newServeSessionFixture(t)
	h := fixture.h
	before := serveSessionSnapshot(t, h)
	for _, request := range []string{"git-upload-pack '../r.git'", "git-upload-pack '/a/r.git'", "git-upload-pack 'a/r.git'", "git-receive-pack '../r.git'"} {
		requireServeSessionRefusal(t, request, serveSessionDirect(t, h, fixture.key.Fingerprint, &request, nil),
			serveReasonUnsupportedCommand, fixture.actor, serveSessionUnsupportedSentence)
	}
	for _, request := range []string{"git-upload-pack 'missing.git'", "git-receive-pack '/missing.git'", "git-lfs-transfer missing download"} {
		requireServeSessionRefusal(t, request, serveSessionDirect(t, h, fixture.key.Fingerprint, &request, nil),
			serveReasonUnhostedRepository, fixture.actor, "repository missing is not hosted here.")
	}
	// Real Git clients see the refusal on stderr, followed by Git's own
	// error, as the serve guide shows.
	client := serveHarnessClient(t, h, fixture.key, nil)
	for _, tc := range []struct{ url, code string }{
		{"ssh://hn-serve@localhost/a/r.git", serveReasonUnsupportedCommand},
		{"hn-serve@localhost:missing.git", serveReasonUnhostedRepository},
	} {
		output := client.GitFailure("", "clone", tc.url, filepath.Join(t.TempDir(), "clone"))
		rejections, _ := extractServeRejections(output)
		if len(rejections) != 1 || rejections[0].Code != tc.code || rejections[0].Actor != fixture.actor {
			t.Fatalf("clone %s: rejections %+v\n%s", tc.url, rejections, output)
		}
		if !strings.Contains(output, "fatal: Could not read from remote repository.") {
			t.Fatalf("clone %s did not end with Git's own error:\n%s", tc.url, output)
		}
	}
	requireServeSessionUnchanged(t, "path refusals", before, serveSessionSnapshot(t, h))

	layout := h.Layout()
	serveSessionPlantSymlinkedRepository(t, layout, "link", "r")
	before = serveSessionSnapshot(t, h)
	for _, request := range []string{"git-upload-pack 'link.git'", "git-receive-pack 'link.git'"} {
		result := serveSessionDirect(t, h, fixture.key.Fingerprint, &request, nil)
		if result.Status != 1 || result.Stdout != "" {
			t.Fatalf("%s on a symlinked repository = %+v, want exit 1 and empty stdout", request, result)
		}
		defect := "hn: hn serve state defect: " + layout.RepoGitDir("link") + ": is a symbolic link\n"
		if result.Stderr != defect && !strings.Contains(result.Stderr, "code="+serveReasonUnhostedRepository) {
			t.Fatalf("%s on a symlinked repository: stderr %q", request, result.Stderr)
		}
	}
	requireServeSessionUnchanged(t, "symlinked repository refusals", before, serveSessionSnapshot(t, h))
}

// serveSessionPlantSymlinkedRepository makes repositories/NAME.git a
// symlink to target's repository and gives it a matching state directory.
func serveSessionPlantSymlinkedRepository(t *testing.T, layout serveLayout, name, target string) {
	t.Helper()
	config, err := os.ReadFile(layout.RepoConfig(target))
	if err != nil {
		t.Fatal(err)
	}
	renamed := strings.Replace(string(config), `"name": "`+target+`"`, `"name": "`+name+`"`, 1)
	if renamed == string(config) {
		t.Fatalf("could not rename the configuration:\n%s", config)
	}
	if err := os.Mkdir(layout.RepoStateDir(name), 0o700); err != nil {
		t.Fatal(err)
	}
	writeServeTestFile(t, layout.RepoConfig(name), renamed, 0o600)
	if err := os.Symlink(layout.RepoGitDir(target), layout.RepoGitDir(name)); err != nil {
		t.Fatal(err)
	}
}

// The session's own hosted check refuses a symlink even when layout
// validation is skipped, which closes the window between the two.
func TestServeSessionRepositoryHostedRequiresRealDirectory(t *testing.T) {
	fixture := newServeSessionFixture(t)
	layout := fixture.h.Layout()
	serveSessionPlantSymlinkedRepository(t, layout, "link", "r")
	if !serveSessionRepositoryHosted(layout, "r") {
		t.Fatal("hosted repository r is not hosted")
	}
	for _, name := range []string{"link", "missing", "../r", ""} {
		if serveSessionRepositoryHosted(layout, name) {
			t.Fatalf("serveSessionRepositoryHosted(%q) = true", name)
		}
	}
	// A configuration without its repository directory is not hosted.
	if err := os.Remove(layout.RepoGitDir("link")); err != nil {
		t.Fatal(err)
	}
	if serveSessionRepositoryHosted(layout, "link") {
		t.Fatal("state without a repository directory counts as hosted")
	}
}

// T039 US2-5: a removed key loses access at once.
func TestServeSessionRemovedKeyRefused(t *testing.T) {
	fixture := newServeSessionFixture(t)
	h := fixture.h
	client := serveHarnessClient(t, h, fixture.key, nil)
	client.Git("", "clone", serveHarnessCloneURL("r"), filepath.Join(t.TempDir(), "before-removal"))

	h.Serve("serve", "key", "remove", fixture.key.Fingerprint)
	before := serveSessionSnapshot(t, h)
	shim := serveSessionViaShim(t, client, "git-upload-pack 'r.git'")
	if shim.Status != 255 || shim.Stdout != "" || shim.Stderr != "Permission denied (publickey).\n" {
		t.Fatalf("shim after key removal = %+v, want exit 255 and the sshd refusal", shim)
	}
	client.GitFailure("", "clone", serveHarnessCloneURL("r"), filepath.Join(t.TempDir(), "after-removal"))
	requireServeSessionRefusal(t, "removed key", serveSessionDirect(t, h, fixture.key.Fingerprint, serveSessionRequest("git-upload-pack 'r.git'"), nil),
		serveReasonUnknownKey, "", serveSessionUnknownKeySentence)
	requireServeSessionUnchanged(t, "removed-key refusals", before, serveSessionSnapshot(t, h))
}

// T037: the LFS seam refuses after the key and repository checks.
func TestServeSessionLFSSeamRefusedWhenDisabled(t *testing.T) {
	if serveLFSEnabled {
		t.Skip("LFS transfer enabled; covered by TestServeLFS...")
	}
	fixture := newServeSessionFixture(t)
	h := fixture.h
	client := serveHarnessClient(t, h, fixture.key, nil)
	before := serveSessionSnapshot(t, h)
	for _, request := range []string{"git-lfs-transfer 'r.git' download", "git-lfs-transfer /r.git upload"} {
		requireServeSessionRefusal(t, "direct "+request, serveSessionDirect(t, h, fixture.key.Fingerprint, &request, nil),
			serveReasonUnsupportedCommand, fixture.actor, serveSessionLFSSentence)
		requireServeSessionRefusal(t, "shim "+request, serveSessionViaShim(t, client, request),
			serveReasonUnsupportedCommand, fixture.actor, serveSessionLFSSentence)
	}
	requireServeSessionRefusal(t, "lfs with unknown key", serveSessionDirect(t, h, newServeTestKey(t, "").Fingerprint, serveSessionRequest("git-lfs-transfer 'r.git' download"), nil),
		serveReasonUnknownKey, "", serveSessionUnknownKeySentence)
	requireServeSessionUnchanged(t, "LFS refusals", before, serveSessionSnapshot(t, h))
}

// T037: a push waits for the repository lock and reports repository-busy;
// a fetch at the same time never waits.
func TestServeSessionPushWaitsForLockAndFetchDoesNot(t *testing.T) {
	fixture := newServeSessionFixture(t)
	h := fixture.h
	hosted := h.Layout().RepoGitDir("r")
	client := serveHarnessClient(t, h, fixture.key, map[string]string{"HN_INTERNAL_TESTING": "1", "HN_TEST_SERVE_LOCK_WAIT": "300ms"})
	work := filepath.Join(t.TempDir(), "work")
	client.Git("", "clone", serveHarnessCloneURL("r"), work)
	writeServeTestFile(t, filepath.Join(work, "change"), "change\n", 0o644)
	client.Git(work, "add", "change")
	client.Git(work, "commit", "-q", "-m", "change")

	signals := t.TempDir()
	started, finish := filepath.Join(signals, "started"), filepath.Join(signals, "finish")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	holder := exec.CommandContext(ctx, h.Binary, "serve", "repo", "lock-exec", "r", "--",
		"sh", "-c", `touch "$1"; while [ ! -e "$2" ]; do sleep 0.05; done`, "sh", started, finish)
	holder.Dir = h.Home
	holder.Env = serveHarnessEnvironment(h.Home, nil)
	var holderOutput bytes.Buffer
	holder.Stdout, holder.Stderr = &holderOutput, &holderOutput
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	holderDone := make(chan error, 1)
	go func() { holderDone <- holder.Wait() }()
	held := true
	defer func() {
		if held {
			_ = os.WriteFile(finish, nil, 0o600)
			<-holderDone
		}
	}()
	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if _, err := os.Lstat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("lock-exec holder never started: %s", holderOutput.String())
		}
	}

	refsBefore := serveHarnessGit(t, hosted, "for-each-ref", "--format=%(objectname) %(refname)")
	output := client.GitFailure(work, "push", "origin", "HEAD:refs/heads/topic")
	rejections, _ := extractServeRejections(output)
	if len(rejections) != 1 || rejections[0].Code != serveReasonRepositoryBusy || rejections[0].Ref != "-" || rejections[0].Actor != fixture.actor {
		t.Fatalf("push beside a held lock: rejections %+v\n%s", rejections, output)
	}
	if want := "another push held r for more than 300ms; retry."; rejections[0].Sentence != want {
		t.Fatalf("busy sentence %q, want %q", rejections[0].Sentence, want)
	}
	if count := strings.Count(output, "hn-serve/1 "); count != 1 {
		t.Fatalf("push printed %d hn-serve/1 lines:\n%s", count, output)
	}

	fetched := filepath.Join(t.TempDir(), "fetched")
	client.Git("", "clone", serveHarnessCloneURL("r"), fetched)
	select {
	case err := <-holderDone:
		held = false
		t.Fatalf("the lock holder finished before the clone completed, so the clone proves nothing: %v\n%s", err, holderOutput.String())
	default:
	}
	if refsAfter := serveHarnessGit(t, hosted, "for-each-ref", "--format=%(objectname) %(refname)"); refsAfter != refsBefore {
		t.Fatalf("refused push changed hosted refs:\nbefore %s\nafter  %s", refsBefore, refsAfter)
	}

	held = false
	if err := os.WriteFile(finish, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := <-holderDone; err != nil {
		t.Fatalf("lock-exec holder: %v\n%s", err, holderOutput.String())
	}
}

// serveSessionFakeGit puts a git on PATH that records its arguments,
// environment and working directory, then runs body.
func serveSessionFakeGit(t *testing.T, body string) (record string) {
	t.Helper()
	bin := t.TempDir()
	record = filepath.Join(t.TempDir(), "record")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > " + serveHarnessShellQuote(record+".args") + "\n" +
		"env > " + serveHarnessShellQuote(record+".env") + "\n" +
		"pwd > " + serveHarnessShellQuote(record+".dir") + "\n" +
		body + "\n"
	writeServeTestFile(t, filepath.Join(bin, "git"), script, 0o700)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return record
}

func readServeSessionRecord(t *testing.T, path string) []string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(contents), "\n"), "\n")
}

// T036: the exact Git invocation and environment, observed by a fake git.
// The receive-pack run also proves the lock is held for the whole run and
// released afterwards.
func TestServeSessionStartsGitWithServerControlledInvocation(t *testing.T) {
	fixture := newServeSessionFixture(t)
	layout := fixture.h.Layout()
	signals := t.TempDir()
	started, finish := filepath.Join(signals, "started"), filepath.Join(signals, "finish")
	// A failing assertion must not leave the fake git waiting forever.
	t.Cleanup(func() { _ = os.WriteFile(finish, nil, 0o600) })
	record := serveSessionFakeGit(t, `touch "`+started+`"; while [ ! -e "`+finish+`" ]; do sleep 0.05; done; exit 3`)
	hostile := []string{
		"PATH=" + os.Getenv("PATH"),
		"HN_SERVE_ACTOR=evil",
		"GIT_DIR=/tmp/x",
		"GIT_CONFIG_GLOBAL=/tmp/evil",
		"LD_PRELOAD=/tmp/evil.so",
		"GIT_PROTOCOL=version=2",
	}

	for _, tc := range []struct {
		request string
		args    []string
		locks   bool
	}{
		{"git-upload-pack '/r.git'", []string{"upload-pack", "--strict", layout.RepoGitDir("r")}, false},
		{"git receive-pack r", []string{
			"-c", "core.hooksPath=" + layout.Hooks,
			"-c", "receive.advertisePushOptions=true",
			"-c", "receive.fsckObjects=true",
			"receive-pack", layout.RepoGitDir("r"),
		}, true},
	} {
		for _, path := range []string{started, finish} {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
		}
		var stdout, stderr bytes.Buffer
		session := &serveSession{
			layout:      layout,
			environment: append(slices.Clone(hostile), "SSH_ORIGINAL_COMMAND="+tc.request),
			stdin:       strings.NewReader(""),
			stdout:      &stdout,
			stderr:      &stderr,
		}
		type outcome struct {
			status int
			err    error
		}
		done := make(chan outcome, 1)
		go func() {
			status, err := session.run([]string{"--key", fixture.key.Fingerprint})
			done <- outcome{status, err}
		}()
		for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(10 * time.Millisecond) {
			if _, err := os.Lstat(started); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: fake git never started; stderr %q", tc.request, stderr.String())
			}
		}
		release, err := acquireServeLock(layout.RepoLock("r"), 0)
		if tc.locks {
			if !errors.Is(err, errServeLockBusy) {
				t.Fatalf("%s: lock while receive-pack runs = %v, want busy", tc.request, err)
			}
		} else {
			if err != nil {
				t.Fatalf("%s: upload-pack holds the repository lock: %v", tc.request, err)
			}
			if err := release(); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(finish, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		result := <-done
		if result.err != nil || result.status != 3 {
			t.Fatalf("%s: run = %d, %v; want Git's status 3", tc.request, result.status, result.err)
		}
		if stdout.Len() != 0 || stderr.Len() != 0 {
			t.Fatalf("%s: wrapper wrote stdout %q stderr %q", tc.request, stdout.String(), stderr.String())
		}
		if got := readServeSessionRecord(t, record+".args"); !reflect.DeepEqual(got, tc.args) {
			t.Fatalf("%s: git arguments\ngot  %q\nwant %q", tc.request, got, tc.args)
		}
		if got := readServeSessionRecord(t, record+".dir"); !reflect.DeepEqual(got, []string{layout.Home}) {
			t.Fatalf("%s: git working directory %q, want %s", tc.request, got, layout.Home)
		}
		environment := readServeSessionRecord(t, record+".env")
		for _, want := range buildServeGitEnvironment(session.environment, layout, serveKeyBinding{Fingerprint: fixture.key.Fingerprint, Actor: fixture.actor}, "r") {
			if !slices.Contains(environment, want) {
				t.Fatalf("%s: git environment lacks %q:\n%s", tc.request, want, strings.Join(environment, "\n"))
			}
		}
		for _, entry := range environment {
			name, _, _ := strings.Cut(entry, "=")
			switch name {
			case "GIT_DIR", "GIT_CONFIG_GLOBAL", "LD_PRELOAD", "SSH_ORIGINAL_COMMAND":
				t.Fatalf("%s: client variable %q reached git", tc.request, entry)
			case "HN_SERVE_ACTOR":
				if entry != "HN_SERVE_ACTOR="+fixture.actor {
					t.Fatalf("%s: git saw %q, want the mapped actor", tc.request, entry)
				}
			}
		}
		release, err = acquireServeLock(layout.RepoLock("r"), 0)
		if err != nil {
			t.Fatalf("%s: lock still held after git exited: %v", tc.request, err)
		}
		if err := release(); err != nil {
			t.Fatal(err)
		}
	}
}

// Git's exit status passes through the built binary, and arguments other
// than exactly --key FINGERPRINT are a usage error that starts nothing.
func TestServeSessionExitStatusAndUsage(t *testing.T) {
	fixture := newServeSessionFixture(t)
	h := fixture.h
	bin := t.TempDir()
	ran := filepath.Join(t.TempDir(), "ran")
	writeServeTestFile(t, filepath.Join(bin, "git"), "#!/bin/sh\ntouch "+serveHarnessShellQuote(ran)+"\nexit 5\n", 0o700)
	fake := map[string]string{"PATH": bin + string(os.PathListSeparator) + os.Getenv("PATH")}

	result := serveSessionDirect(t, h, fixture.key.Fingerprint, serveSessionRequest("git-upload-pack 'r.git'"), fake)
	if result.Status != 5 || result.Stdout != "" || result.Stderr != "" {
		t.Fatalf("session with git exiting 5 = %+v, want status 5 and no output of its own", result)
	}
	if err := os.Remove(ran); err != nil {
		t.Fatalf("fake git did not run: %v", err)
	}

	environment := serveHarnessEnvironment(h.Home, map[string]string{"PATH": fake["PATH"], "SSH_ORIGINAL_COMMAND": "git-upload-pack 'r.git'"})
	for _, args := range [][]string{
		{},
		{"--key"},
		{"--key=" + fixture.key.Fingerprint},
		{"--key", fixture.key.Fingerprint, "extra"},
		{"-k", fixture.key.Fingerprint},
		{fixture.key.Fingerprint, "--key"},
	} {
		result := runServeSessionProgram(t, h.Home, environment, h.Binary, append([]string{"serve", "git"}, args...)...)
		if result.Status != 1 || result.Stdout != "" || result.Stderr != "hn: "+serveGitUsage+"\n" {
			t.Fatalf("hn serve git %q = %+v, want the usage error", args, result)
		}
	}
	if _, err := os.Lstat(ran); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a usage error started git: %v", err)
	}
}

func TestDescribeServeWait(t *testing.T) {
	for wait, want := range map[time.Duration]string{
		120 * time.Second:       "120 seconds",
		time.Second:             "1 second",
		300 * time.Millisecond:  "300ms",
		1500 * time.Millisecond: "1.5s",
		0:                       "0s",
	} {
		if got := describeServeWait(wait); got != want {
			t.Fatalf("describeServeWait(%v) = %q, want %q", wait, got, want)
		}
	}
}
