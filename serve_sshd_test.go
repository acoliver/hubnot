package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// WP13 T082: one round trip through a real unprivileged OpenSSH server.
// The SSH shim of serve_harness_test.go runs the forced command the way
// sshd does, but only a real sshd proves the restrict semantics of the
// managed authorized_keys entries (C-002, OQ-9): no terminal and no port
// forwarding, while Git fetch and push still work through the forced
// command (sshd(8), AUTHORIZED_KEYS FILE FORMAT).

// serveSSHDLog collects sshd's stderr while it runs.
type serveSSHDLog struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (l *serveSSHDLog) Write(data []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buffer.Write(data)
}

func (l *serveSSHDLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buffer.String()
}

// serveSSHDProgram finds sshd in /usr/sbin or on PATH.
func serveSSHDProgram() (string, error) {
	if info, err := os.Stat("/usr/sbin/sshd"); err == nil && info.Mode().IsRegular() {
		return "/usr/sbin/sshd", nil
	}
	path, err := exec.LookPath("sshd")
	if err != nil {
		return "", err
	}
	// sshd re-executes itself and refuses a relative path.
	return filepath.Abs(path)
}

// serveSSHDFreePort returns a TCP port on 127.0.0.1 that was free a moment
// ago.
func serveSSHDFreePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

// serveSSHDLooseParent returns the first directory from path upwards that
// group or others can write, or "" when there is none. StrictModes yes
// refuses an authorized_keys file below such a directory.
func serveSSHDLooseParent(path string) string {
	for directory := filepath.Dir(path); ; directory = filepath.Dir(directory) {
		if info, err := os.Stat(directory); err == nil && info.Mode().Perm()&0o022 != 0 {
			return directory
		}
		if directory == filepath.Dir(directory) {
			return ""
		}
	}
}

// serveSSHDKeygen runs ssh-keygen for a new passphrase-less ed25519 key.
func serveSSHDKeygen(t *testing.T, path, comment string) {
	t.Helper()
	output, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", comment, "-f", path).CombinedOutput()
	if err != nil {
		t.Fatalf("ssh-keygen -f %s: %v\n%s", path, err, output)
	}
}

// serveSSHDServer is one running sshd -D child.
type serveSSHDServer struct {
	Port int
	Log  *serveSSHDLog
	stop func()
}

// Stop ends sshd and waits for it; later calls do nothing.
func (s *serveSSHDServer) Stop() { s.stop() }

// startServeSSHD writes sshd_config into run from settings plus a free
// Port, starts sshd -D -e as a child and waits until it listens. It skips
// the test with sshd's log when sshd does not start or listen, and stops
// sshd at cleanup.
func startServeSSHD(t *testing.T, sshd, run string, settings []string) *serveSSHDServer {
	t.Helper()
	port := serveSSHDFreePort(t)
	config := filepath.Join(run, "sshd_config")
	writeServeTestFile(t, config, strings.Join(append(slices.Clone(settings), "Port "+strconv.Itoa(port), ""), "\n"), 0o600)
	ctx, cancel := context.WithCancel(context.Background())
	daemon := exec.CommandContext(ctx, sshd, "-D", "-e", "-f", config)
	log := &serveSSHDLog{}
	daemon.Stdout, daemon.Stderr = log, log
	if err := daemon.Start(); err != nil {
		cancel()
		t.Skipf("real-sshd round trip skipped: sshd did not start: %v", err)
	}
	exited := make(chan struct{})
	var waitErr error
	go func() {
		waitErr = daemon.Wait()
		close(exited)
	}()
	var once sync.Once
	server := &serveSSHDServer{Port: port, Log: log, stop: func() {
		once.Do(func() {
			cancel()
			<-exited
		})
	}}
	t.Cleanup(server.Stop)
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		connection, err := net.DialTimeout("tcp", address, 200*time.Millisecond)
		if err == nil {
			connection.Close()
			return server
		}
		select {
		case <-exited:
			t.Skipf("real-sshd round trip skipped: sshd exited before listening: %v\nsshd log:\n%s", waitErr, log)
		default:
		}
		if time.Now().After(deadline) {
			t.Skipf("real-sshd round trip skipped: sshd did not listen on %s within 10s\nsshd log:\n%s", address, log)
		}
	}
}

func TestServeSSHDRoundTrip(t *testing.T) {
	sshd, err := serveSSHDProgram()
	if err != nil {
		t.Skipf("real-sshd round trip skipped: sshd is neither /usr/sbin/sshd nor on PATH: %v", err)
	}
	for _, program := range []string{"ssh", "ssh-keygen"} {
		if _, err := exec.LookPath(program); err != nil {
			t.Skipf("real-sshd round trip skipped: %s is not on PATH: %v", program, err)
		}
	}
	if os.Geteuid() == 0 {
		t.Skip("real-sshd round trip skipped: it needs an unprivileged user, and hn serve refuses uid 0")
	}
	account, err := user.Current()
	if err != nil {
		t.Skipf("real-sshd round trip skipped: cannot determine the current user: %v", err)
	}

	h := newServeHarness(t)
	maintainer := serveHarnessActor("sshd-maintainer")
	actor := serveHarnessActor("sshd-contributor")
	h.Serve("serve", "repo", "create", "r", "--from", serveHarnessSourceRepo(t, maintainer))

	keys := t.TempDir()
	hostKey := filepath.Join(keys, "host_ed25519")
	clientKey := filepath.Join(keys, "client_ed25519")
	serveSSHDKeygen(t, hostKey, "hn-serve-test-host")
	serveSSHDKeygen(t, clientKey, "hn-serve-test-client")
	h.Serve("serve", "key", "add", "--actor", actor, clientKey+".pub")
	authorizedKeys := h.Layout().AuthorizedKeys
	entries, err := os.ReadFile(authorizedKeys)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(entries), `restrict,command="`+h.Binary+` serve git --key SHA256:`) {
		t.Fatalf("authorized_keys has no restrict,command= entry for %s:\n%s", h.Binary, entries)
	}

	strictModes := "yes"
	if loose := serveSSHDLooseParent(authorizedKeys); loose != "" {
		// t.TempDir lives below a directory such as /tmp that others can
		// write, and StrictModes yes then refuses every key. The server
		// account in production keeps StrictModes yes (docs/serve-v0.md).
		strictModes = "no"
		t.Logf("StrictModes no: %s is writable by group or others", loose)
	}
	run := t.TempDir()
	// sshd runs a forced command with the account's HOME from the password
	// database. SetEnv HOME points it at the test server home instead;
	// OpenSSH applies SetEnv after setting HOME itself.
	settings := func(authorized string) []string {
		return []string{
			"ListenAddress 127.0.0.1",
			"HostKey " + hostKey,
			"PidFile " + filepath.Join(run, "sshd.pid"),
			"AuthorizedKeysFile " + authorized,
			"PasswordAuthentication no",
			"KbdInteractiveAuthentication no",
			"UsePAM no",
			"PermitUserEnvironment no",
			"StrictModes " + strictModes,
			"SetEnv HOME=" + h.Home,
			"LogLevel VERBOSE",
		}
	}
	server := startServeSSHD(t, sshd, run, settings(authorizedKeys))

	// The harness client environment (its own HOME and Git identity), with
	// Git's SSH transport pointed at the real ssh instead of the shim.
	client := serveHarnessClient(t, h, newServeTestKey(t, "unused"), nil)
	var sshArgs []string
	useServer := func() {
		sshArgs = []string{
			"-F", "/dev/null",
			"-i", clientKey,
			"-p", strconv.Itoa(server.Port),
			"-o", "UserKnownHostsFile=" + filepath.Join(run, "known_hosts"),
			"-o", "StrictHostKeyChecking=accept-new",
			"-o", "IdentitiesOnly=yes",
			"-o", "BatchMode=yes",
		}
		quoted := []string{"ssh"}
		for _, arg := range sshArgs {
			quoted = append(quoted, serveHarnessShellQuote(arg))
		}
		for index, entry := range client.Env {
			if strings.HasPrefix(entry, "GIT_SSH_COMMAND=") {
				client.Env[index] = "GIT_SSH_COMMAND=" + strings.Join(quoted, " ")
			}
		}
	}
	useServer()
	target := account.Username + "@127.0.0.1"
	url := func() string { return fmt.Sprintf("ssh://%s:%d/r.git", target, server.Port) }
	if output, status := client.exec("", "git", "ls-remote", url()); status != 0 {
		// Deviation for an OpenSSH that does not apply SetEnv to HOME: serve
		// a copy of authorized_keys whose forced commands set HOME with env,
		// keeping restrict.
		t.Logf("git ls-remote through sshd exited %d; retrying with env HOME= in the forced command:\n%s\nsshd log:\n%s", status, output, server.Log)
		server.Stop()
		forced := `restrict,command="` + h.Binary + ` serve git`
		copied := strings.ReplaceAll(string(entries), forced, `restrict,command="env HOME=`+h.Home+` `+h.Binary+` serve git`)
		authorizedCopy := filepath.Join(run, "authorized_keys")
		writeServeTestFile(t, authorizedCopy, copied, 0o600)
		server = startServeSSHD(t, sshd, run, settings(authorizedCopy))
		useServer()
		if output, status := client.exec("", "git", "ls-remote", url()); status != 0 {
			t.Fatalf("git ls-remote through sshd exited %d:\n%s\nsshd log:\n%s", status, output, server.Log)
		}
	}
	failWithLog := func(format string, args ...any) {
		t.Helper()
		t.Fatalf(format+"\nsshd log:\n%s", append(args, server.Log)...)
	}

	// Clone, then push a feature branch: accepted.
	work := filepath.Join(t.TempDir(), "work")
	if output, status := client.exec("", "git", "clone", "-q", url(), work); status != 0 {
		failWithLog("git clone %s exited %d:\n%s", url(), status, output)
	}
	hosted := h.Layout().RepoGitDir("r")
	base := serveHarnessGit(t, hosted, "rev-parse", "refs/heads/main")
	writeServeTestFile(t, filepath.Join(work, "feature.txt"), "over real sshd\n", 0o644)
	client.Git(work, "add", "feature.txt")
	client.Git(work, "commit", "-q", "-m", "feature over real sshd")
	commit := strings.TrimSpace(client.Git(work, "rev-parse", "HEAD"))
	if output, status := client.exec(work, "git", "push", "origin", "HEAD:refs/heads/feature/sshd"); status != 0 {
		failWithLog("feature branch push exited %d:\n%s", status, output)
	}
	if got := serveHarnessGit(t, hosted, "rev-parse", "refs/heads/feature/sshd"); got != commit {
		t.Fatalf("hosted feature/sshd = %s, want %s", got, commit)
	}

	// A direct main push: rejected with a parseable protected-no-proposal
	// line, and main unchanged.
	output, status := client.exec(work, "git", "push", "origin", "HEAD:refs/heads/main")
	if status == 0 {
		t.Fatalf("direct main push over real sshd succeeded:\n%s", output)
	}
	rejections, _ := extractServeRejections(output)
	if len(rejections) != 1 || rejections[0].Code != serveReasonProtectedNoProposal || rejections[0].Ref != "refs/heads/main" || rejections[0].Actor != actor {
		t.Fatalf("direct main push rejections = %+v, want one protected-no-proposal for %s:\n%s", rejections, actor, output)
	}
	if got := serveHarnessGit(t, hosted, "rev-parse", "refs/heads/main"); got != base {
		t.Fatalf("hosted main moved to %s after a rejected push", got)
	}

	// restrict: no terminal. ssh -tt asks for one and is refused. OpenSSH
	// 10 ends a forced-terminal (-tt) session as soon as the server refuses
	// the terminal, so the forced command's answer never reaches that
	// client; the same request without a terminal shows it: the forced
	// command replaces "true" and refuses it with unsupported-command.
	sshCommand := func(args ...string) (string, int) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, "ssh", append(append([]string{}, sshArgs...), args...)...)
		command.Env = client.Env
		command.Stdin = strings.NewReader("")
		output, err := command.CombinedOutput()
		var exitErr *exec.ExitError
		switch {
		case ctx.Err() != nil:
			failWithLog("ssh %s timed out:\n%s", strings.Join(args, " "), output)
		case errors.As(err, &exitErr):
			return string(output), exitErr.ExitCode()
		case err != nil:
			t.Fatalf("ssh %s: %v\n%s", strings.Join(args, " "), err, output)
		}
		return string(output), 0
	}
	output, status = sshCommand("-tt", target, "true")
	if status == 0 || !strings.Contains(output, "PTY allocation request failed") {
		failWithLog("ssh -tt true exited %d without a refused terminal:\n%s", status, output)
	}
	output, status = sshCommand(target, "true")
	rejections, _ = extractServeRejections(output)
	if status != 1 || len(rejections) != 1 || rejections[0].Code != serveReasonUnsupportedCommand || rejections[0].Actor != actor {
		failWithLog("ssh true exited %d with rejections %+v, want one unsupported-command refusal for %s:\n%s", status, rejections, actor, output)
	}

	// restrict: no port forwarding. The local listener accepts, and the
	// channel the client then asks for is refused.
	forwardPort := serveSSHDFreePort(t)
	forwardCtx, stopForward := context.WithCancel(context.Background())
	defer stopForward()
	forward := exec.CommandContext(forwardCtx, "ssh", append(append([]string{}, sshArgs...),
		"-N", "-o", "ExitOnForwardFailure=yes", "-L", fmt.Sprintf("127.0.0.1:%d:127.0.0.1:%d", forwardPort, server.Port), target)...)
	forward.Env = client.Env
	forwardLog := &serveSSHDLog{}
	forward.Stdout, forward.Stderr = forwardLog, forwardLog
	if err := forward.Start(); err != nil {
		t.Fatal(err)
	}
	forwardExited := make(chan struct{})
	go func() {
		forward.Wait()
		close(forwardExited)
	}()
	forwarded, listened := false, false
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		connection, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", forwardPort), 200*time.Millisecond)
		if err != nil {
			continue
		}
		listened = true
		connection.SetDeadline(time.Now().Add(5 * time.Second))
		// Through a working forward, the target sshd would greet with its
		// version banner.
		banner := make([]byte, 64)
		count, _ := connection.Read(banner)
		connection.Close()
		forwarded = strings.HasPrefix(string(banner[:count]), "SSH-")
		break
	}
	if !listened {
		failWithLog("ssh -N -L never listened on 127.0.0.1:%d:\n%s", forwardPort, forwardLog)
	}
	refused := func() bool {
		return strings.Contains(forwardLog.String(), "administratively prohibited") ||
			strings.Contains(server.Log.String(), "refused local port forward")
	}
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline) && !refused(); {
		time.Sleep(50 * time.Millisecond)
	}
	stopForward()
	<-forwardExited
	if forwarded || !refused() {
		failWithLog("port forwarding was not refused (forwarded=%t):\nssh output:\n%s", forwarded, forwardLog)
	}
}
