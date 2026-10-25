package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

// Black-box harness for hn serve. Every command runs the built hn binary
// with one stable server HOME, the way the server account runs it. WP06
// adds the SSH shim helpers here.

// serveHarness is one server account: a built hn binary and its HOME.
type serveHarness struct {
	t      *testing.T
	Binary string
	Home   string
}

// serveHarnessRun describes one invocation beyond its arguments.
type serveHarnessRun struct {
	Env   map[string]string // added to the minimal environment
	Stdin string
	Dir   string // working directory; HOME when empty
}

func newServeHarness(t *testing.T) *serveHarness {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("hn serve requires a Unix host")
	}
	if os.Geteuid() == 0 {
		t.Skip("hn serve refuses uid 0; run tests as an unprivileged user")
	}
	binary := buildOperationalBinary(t)
	if _, err := checkServeExecutablePath(binary); err != nil {
		t.Skipf("temporary directory is not usable as an hn install path: %v", err)
	}
	home := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	return &serveHarness{t: t, Binary: binary, Home: home}
}

// Layout is the server layout below the harness HOME.
func (h *serveHarness) Layout() serveLayout {
	h.t.Helper()
	root := filepath.Join(h.Home, serveHomeDirectoryName)
	state := filepath.Join(root, "state")
	sshDir := filepath.Join(h.Home, ".ssh")
	return serveLayout{
		Home:           h.Home,
		Root:           root,
		Hooks:          filepath.Join(root, "hooks"),
		Repositories:   filepath.Join(root, "repositories"),
		State:          state,
		Registry:       filepath.Join(state, "registry.json"),
		Tmp:            filepath.Join(state, "tmp"),
		Repos:          filepath.Join(state, "repos"),
		SSHDir:         sshDir,
		AuthorizedKeys: filepath.Join(sshDir, "authorized_keys"),
	}
}

// Exec runs hn with the minimal server environment and returns its combined
// output and exit status.
func (h *serveHarness) Exec(run serveHarnessRun, args ...string) (string, int) {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, h.Binary, args...)
	command.Dir = run.Dir
	if command.Dir == "" {
		command.Dir = h.Home
	}
	command.Env = serveHarnessEnvironment(h.Home, run.Env)
	command.Stdin = strings.NewReader(run.Stdin)
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		h.t.Fatalf("hn %s timed out: %v\n%s", strings.Join(args, " "), ctx.Err(), output)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return string(output), exitErr.ExitCode()
	}
	if err != nil {
		h.t.Fatalf("hn %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output), 0
}

// Serve runs hn and fails the test unless it exits 0.
func (h *serveHarness) Serve(args ...string) string {
	h.t.Helper()
	return h.ServeWith(serveHarnessRun{}, args...)
}

func (h *serveHarness) ServeWith(run serveHarnessRun, args ...string) string {
	h.t.Helper()
	output, status := h.Exec(run, args...)
	if status != 0 {
		h.t.Fatalf("hn %s exited %d:\n%s", strings.Join(args, " "), status, output)
	}
	return output
}

// ServeFailure runs hn and fails the test unless it exits 1.
func (h *serveHarness) ServeFailure(args ...string) string {
	h.t.Helper()
	return h.ServeFailureWith(serveHarnessRun{}, args...)
}

func (h *serveHarness) ServeFailureWith(run serveHarnessRun, args ...string) string {
	h.t.Helper()
	output, status := h.Exec(run, args...)
	if status != 1 {
		h.t.Fatalf("hn %s exited %d, want 1:\n%s", strings.Join(args, " "), status, output)
	}
	return output
}

// serveHarnessEnvironment is the minimal environment a server command gets:
// nothing is inherited apart from PATH.
func serveHarnessEnvironment(home string, extra map[string]string) []string {
	environment := []string{
		"HOME=" + home,
		"PATH=" + os.Getenv("PATH"),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"LANG=C",
	}
	keys := make([]string, 0, len(extra))
	for key := range extra {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		environment = append(environment, key+"="+extra[key])
	}
	return environment
}

// serveHarnessActor is a deterministic full actor fingerprint for tests.
func serveHarnessActor(seed string) string {
	return serveStateTestActor("harness:" + seed)
}

// serveHarnessSourceRepo creates a non-bare repository to seed a hosted
// repository from. main carries a valid .hn/policy.json naming maintainers
// (one deterministic actor when none are given); the repository also has a
// second branch, release, without policy, a tag v1, and a Hubnot actor ref
// that repo create must not import.
func serveHarnessSourceRepo(t *testing.T, maintainers ...string) string {
	t.Helper()
	if len(maintainers) == 0 {
		maintainers = []string{serveHarnessActor("maintainer")}
	}
	repository := filepath.Join(t.TempDir(), "source")
	initOperationalRepository(t, repository, "Source")
	serveHarnessWritePolicy(t, repository, maintainers...)
	serveHarnessCommit(t, repository, "seed with policy")
	runOperationalGit(t, repository, "tag", "v1")
	runOperationalGit(t, repository, "switch", "-q", "-c", "release")
	runOperationalGit(t, repository, "rm", "-q", filepath.Join(".hn", "policy.json"))
	writeServeTestFile(t, filepath.Join(repository, "README"), "release without policy\n", 0o644)
	serveHarnessCommit(t, repository, "release without policy")
	runOperationalGit(t, repository, "switch", "-q", "main")
	runOperationalGit(t, repository, "update-ref", serveHarnessImportedActorRef, "HEAD")
	return repository
}

// serveHarnessImportedActorRef is the Hubnot ref serveHarnessSourceRepo
// plants to prove repo create imports only branches and tags.
var serveHarnessImportedActorRef = "refs/hn/actors/" + serveHarnessActor("planted")

// serveHarnessWritePolicy writes a minimal valid hn.policy/0 document.
func serveHarnessWritePolicy(t *testing.T, repository string, maintainers ...string) {
	t.Helper()
	policy := map[string]any{
		"version":     policyVersion,
		"maintainers": maintainers,
		"proposals": map[string]any{
			"requiredApprovals":   1,
			"requiredAccepts":     1,
			"trustedReviewers":    maintainers,
			"allowAuthorApproval": false,
		},
		"pipelines": map[string]any{},
	}
	encoded, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	serveHarnessWriteFile(t, repository, filepath.Join(".hn", "policy.json"), string(encoded)+"\n")
}

// serveHarnessWriteFile writes and stages one file in repository.
func serveHarnessWriteFile(t *testing.T, repository, relative, contents string) {
	t.Helper()
	path := filepath.Join(repository, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	runOperationalGit(t, repository, "add", "--", relative)
}

func serveHarnessCommit(t *testing.T, repository, message string) {
	t.Helper()
	runOperationalGit(t, repository, "add", "-A")
	runOperationalGit(t, repository, "commit", "-q", "--allow-empty", "-m", message)
}

// serveHarnessAddKey binds a fresh ed25519 key to actor through
// hn serve key add with the key on standard input.
func serveHarnessAddKey(h *serveHarness, actor string) serveTestKey {
	h.t.Helper()
	key := newServeTestKey(h.t, "participant@laptop")
	output := h.ServeWith(serveHarnessRun{Stdin: key.PublicLine + "\n"}, "serve", "key", "add", "--actor", actor, "-")
	if want := "Bound " + key.Fingerprint + " to actor " + actor + "\n"; output != want {
		h.t.Fatalf("hn serve key add output = %q, want %q", output, want)
	}
	return key
}

// serveHarnessGit runs git against a hosted bare repository.
func serveHarnessGit(t *testing.T, gitDir string, args ...string) string {
	t.Helper()
	return strings.TrimSpace(runOperationalGit(t, "", append([]string{"--git-dir=" + gitDir}, args...)...))
}

// serveHarnessEntries lists a directory's entry names, or nil when it does
// not exist.
func serveHarnessEntries(t *testing.T, directory string) []string {
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
