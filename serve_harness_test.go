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

	// Wrap, when set, is a command line that every server-side process the
	// harness starts runs under: administration commands through Exec, and
	// forced commands (with the hooks and Git programs they start) through
	// shims built afterwards. WP13 sets it with EnforceWriteBoundary; WP14
	// reuses it for LFS transfers.
	Wrap []string
	// Env is added to the environment of every server-side process, before
	// the entries of one run or one shim, which win on conflict. Set it
	// before building client shims.
	Env map[string]string
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
	program, arguments := h.Binary, args
	if len(h.Wrap) > 0 {
		program = h.Wrap[0]
		arguments = append(append(append([]string{}, h.Wrap[1:]...), h.Binary), args...)
	}
	command := exec.CommandContext(ctx, program, arguments...)
	command.Dir = run.Dir
	if command.Dir == "" {
		command.Dir = h.Home
	}
	command.Env = serveHarnessEnvironment(h.Home, serveHarnessMergeEnv(h.Env, run.Env))
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

// serveHarnessMergeEnv returns base overlaid with override, or nil when
// both are empty.
func serveHarnessMergeEnv(base, override map[string]string) map[string]string {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}
	merged := make(map[string]string, len(base)+len(override))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range override {
		merged[key] = value
	}
	return merged
}

// serveHarnessWriteBoundary is the bubblewrap command line that confines a
// server-side process to writes below $HOME/.hn-serve and $HOME/.ssh, the
// only locations NFR-004 allows. The whole filesystem is bound read-only,
// with no tmpfs: /tmp, the rest of HOME and every other path stay
// read-only, so any write elsewhere, including a transient temporary file,
// fails the command that attempts it.
func serveHarnessWriteBoundary(bwrap, home string) []string {
	root := filepath.Join(home, serveHomeDirectoryName)
	sshDir := filepath.Join(home, ".ssh")
	return []string{
		bwrap,
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--proc", "/proc",
		"--bind", root, root,
		"--bind", sshDir, sshDir,
	}
}

// EnforceWriteBoundary runs every later server-side process inside
// serveHarnessWriteBoundary and returns the wrapper. Bind mounts need
// existing targets, so it creates $HOME/.hn-serve and $HOME/.ssh with mode
// 0700 when they are absent, the mode hn serve itself creates them with.
// It skips the test with the reason when bubblewrap or unprivileged user
// namespaces are unavailable.
func (h *serveHarness) EnforceWriteBoundary() []string {
	h.t.Helper()
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		h.t.Skipf("NFR-004 evidence limitation: enforced write boundary skipped because bubblewrap (bwrap) is not on PATH: %v", err)
	}
	for _, directory := range []string{filepath.Join(h.Home, serveHomeDirectoryName), filepath.Join(h.Home, ".ssh")} {
		if err := os.Mkdir(directory, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			h.t.Fatal(err)
		}
	}
	wrap := serveHarnessWriteBoundary(bwrap, h.Home)
	probe := exec.Command(wrap[0], append(append([]string{}, wrap[1:]...), "true")...)
	probe.Env = serveHarnessEnvironment(h.Home, nil)
	if output, err := probe.CombinedOutput(); err != nil {
		h.t.Skipf("NFR-004 evidence limitation: enforced write boundary skipped because bwrap cannot create its sandbox (unprivileged user namespaces may be disabled): %v\n%s", err, output)
	}
	h.Wrap = wrap
	return wrap
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

// SSH command shim and client helpers (WP06). A client's git and hn reach
// the server through GIT_SSH_COMMAND, which points at a POSIX sh script that
// plays sshd: it finds the managed authorized_keys entry for its key and
// runs that entry's exact command= value under env -i, the way sshd runs a
// forced command.

// serveHarnessShellQuote quotes value as one single-quoted sh word.
func serveHarnessShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// serveHarnessShim writes the SSH shim for key and returns its path.
// extraEnv is added to the forced command's environment, after HOME, PATH,
// SSH_ORIGINAL_COMMAND and, when the client sets it, GIT_PROTOCOL; it stands
// in for variables the server account's sshd session would carry. The
// harness Env comes first and extraEnv wins on conflict. When the harness
// has a Wrap command line, the forced command runs under it.
//
// The shim skips the ssh options Git may pass (-o VALUE, -p PORT, -l USER,
// -i FILE, -F FILE, -4, -6, -T), takes the host and requires exactly one
// more argument, the remote command. An unknown key exits 255 with
// "Permission denied (publickey).", as sshd refuses one, and so does an
// entry that does not start with restrict,command=.
func serveHarnessShim(t *testing.T, h *serveHarness, key serveTestKey, extraEnv map[string]string) string {
	t.Helper()
	fields := strings.Fields(key.PublicLine)
	if len(fields) < 2 {
		t.Fatalf("malformed test key line %q", key.PublicLine)
	}
	canonical := fields[0] + " " + fields[1]
	merged := serveHarnessMergeEnv(h.Env, extraEnv)
	names := make([]string, 0, len(merged))
	for name := range merged {
		names = append(names, name)
	}
	sort.Strings(names)
	var extra strings.Builder
	for _, name := range names {
		extra.WriteString(" " + serveHarnessShellQuote(name+"="+merged[name]))
	}
	var wrap strings.Builder
	for _, word := range h.Wrap {
		wrap.WriteString(serveHarnessShellQuote(word) + " ")
	}
	base := wrap.String() + "env -i HOME=" + serveHarnessShellQuote(h.Home) +
		" PATH=" + serveHarnessShellQuote(os.Getenv("PATH")) +
		` SSH_ORIGINAL_COMMAND="$request"`
	authorizedKeys := serveHarnessShellQuote(filepath.Join(h.Home, ".ssh", "authorized_keys"))
	script := `#!/bin/sh
# hn serve test SSH shim; see serveHarnessShim.
while [ $# -gt 0 ]; do
	case $1 in
	-o|-p|-l|-i|-F)
		if [ $# -lt 2 ]; then
			echo "ssh shim: $1 needs a value" >&2
			exit 255
		fi
		shift 2 ;;
	-o?*|-p?*|-l?*|-i?*|-F?*|-4|-6|-T)
		shift ;;
	-*)
		echo "ssh shim: unsupported option $1" >&2
		exit 255 ;;
	*)
		break ;;
	esac
done
if [ $# -ne 2 ]; then
	echo "ssh shim: want HOST COMMAND, got $# arguments" >&2
	exit 255
fi
request=$2
found=
if [ -f ` + authorizedKeys + ` ]; then
	while IFS= read -r line || [ -n "$line" ]; do
		case $line in
		*` + serveHarnessShellQuote(" "+canonical+" ") + `*|*` + serveHarnessShellQuote(" "+canonical) + `)
			found=$line
			break ;;
		esac
	done < ` + authorizedKeys + `
fi
if [ -z "$found" ]; then
	echo "Permission denied (publickey)." >&2
	exit 255
fi
case $found in
'restrict,command="'*) ;;
*)
	echo "ssh shim: authorized_keys entry does not start with restrict,command=" >&2
	exit 255 ;;
esac
forced=${found#restrict,command=\"}
forced=${forced%%\"*}
if [ -n "${GIT_PROTOCOL+set}" ]; then
	exec ` + base + ` GIT_PROTOCOL="$GIT_PROTOCOL"` + extra.String() + ` /bin/sh -c "$forced"
fi
exec ` + base + extra.String() + ` /bin/sh -c "$forced"
`
	path := filepath.Join(t.TempDir(), "ssh-shim")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// serveHarnessClientEnv is one participant's client environment: a fresh
// HOME with a user name and email, and Git's SSH transport pointed at the
// shim for one key.
type serveHarnessClientEnv struct {
	t      *testing.T
	Home   string
	Shim   string
	Env    []string
	Binary string
}

// serveHarnessClient builds the client environment for key. extraEnv goes
// to the shim (see serveHarnessShim), not to the client.
func serveHarnessClient(t *testing.T, h *serveHarness, key serveTestKey, extraEnv map[string]string) serveHarnessClientEnv {
	t.Helper()
	shim := serveHarnessShim(t, h, key, extraEnv)
	home := filepath.Join(t.TempDir(), "client-home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	config := "[user]\n\tname = Serve Client\n\temail = serve-client@hn.invalid\n[init]\n\tdefaultBranch = main\n"
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	return serveHarnessClientEnv{
		t:    t,
		Home: home,
		Shim: shim,
		Env: []string{
			"HOME=" + home,
			"PATH=" + os.Getenv("PATH"),
			"GIT_SSH_COMMAND=" + serveHarnessShellQuote(shim),
			"GIT_SSH_VARIANT=ssh",
			"GIT_CONFIG_NOSYSTEM=1",
			"GIT_TERMINAL_PROMPT=0",
			"LANG=C",
		},
		Binary: h.Binary,
	}
}

// serveHarnessCloneURL is the SSH URL of a hosted repository. The shim
// ignores the host, so the user and host only have to look right.
func serveHarnessCloneURL(name string) string {
	return "ssh://hn-serve@localhost/" + name + ".git"
}

// exec runs program in dir with the client environment and returns its
// combined output and exit status.
func (c serveHarnessClientEnv) exec(dir, program string, args ...string) (string, int) {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, program, args...)
	command.Dir = dir
	if command.Dir == "" {
		command.Dir = c.Home
	}
	command.Env = c.Env
	command.Stdin = strings.NewReader("")
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		c.t.Fatalf("%s %s timed out: %v\n%s", program, strings.Join(args, " "), ctx.Err(), output)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return string(output), exitErr.ExitCode()
	}
	if err != nil {
		c.t.Fatalf("%s %s: %v\n%s", program, strings.Join(args, " "), err, output)
	}
	return string(output), 0
}

// Git runs client git in dir and fails the test unless it exits 0.
func (c serveHarnessClientEnv) Git(dir string, args ...string) string {
	c.t.Helper()
	output, status := c.exec(dir, "git", args...)
	if status != 0 {
		c.t.Fatalf("git %s exited %d:\n%s", strings.Join(args, " "), status, output)
	}
	return output
}

// GitFailure runs client git in dir and fails the test if it exits 0.
func (c serveHarnessClientEnv) GitFailure(dir string, args ...string) string {
	c.t.Helper()
	output, status := c.exec(dir, "git", args...)
	if status == 0 {
		c.t.Fatalf("git %s unexpectedly succeeded:\n%s", strings.Join(args, " "), output)
	}
	return output
}

// Hn runs the client's hn in dir and fails the test unless it exits 0.
func (c serveHarnessClientEnv) Hn(dir string, args ...string) string {
	c.t.Helper()
	output, status := c.exec(dir, c.Binary, args...)
	if status != 0 {
		c.t.Fatalf("hn %s exited %d:\n%s", strings.Join(args, " "), status, output)
	}
	return output
}

// HnFailure runs the client's hn in dir and fails the test if it exits 0.
func (c serveHarnessClientEnv) HnFailure(dir string, args ...string) string {
	c.t.Helper()
	output, status := c.exec(dir, c.Binary, args...)
	if status == 0 {
		c.t.Fatalf("hn %s unexpectedly succeeded:\n%s", strings.Join(args, " "), output)
	}
	return output
}
