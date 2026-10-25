package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// hn serve git --key FINGERPRINT is the forced command of every managed
// authorized_keys entry. It reads the client's request from
// SSH_ORIGINAL_COMMAND, checks it in the order the contract fixes
// (kitty-specs/hn-serve-01M5P0NP/contracts/serve-cli.md, "Forced command"),
// and either prints one hn-serve/1 refusal to stderr or starts Git with an
// environment and configuration the server controls. Stdout carries Git
// protocol data, so nothing in this file ever writes to it.

const (
	serveServiceUploadPack  = "upload-pack"
	serveServiceReceivePack = "receive-pack"
	serveServiceLFSTransfer = "lfs-transfer"

	serveLFSOperationUpload   = "upload"
	serveLFSOperationDownload = "download"

	serveGitUsage = "usage: hn serve git --key FINGERPRINT"
)

// serveRequest is one parsed SSH_ORIGINAL_COMMAND.
type serveRequest struct {
	Service string // serveServiceUploadPack, serveServiceReceivePack or serveServiceLFSTransfer
	Repo    string // repository name without leading "/" or trailing ".git"
	LFSOp   string // "upload" or "download" for lfs-transfer
}

// serveRequestError is a request outside the forced-command grammar. The
// session reports every one as unsupported-command; Reason is for tests
// and diagnostics only and never reaches the client.
type serveRequestError struct {
	Reason string
}

func (e *serveRequestError) Error() string {
	return "unsupported SSH request: " + e.Reason
}

func serveRequestRefusal(format string, args ...any) error {
	return &serveRequestError{Reason: fmt.Sprintf(format, args...)}
}

// serveGitCommands are the accepted Git command spellings, each with the
// single separating space. git-lfs-transfer is matched separately because
// it takes an operation after the path.
var serveGitCommands = []struct {
	prefix  string
	service string
}{
	{"git-upload-pack ", serveServiceUploadPack},
	{"git-receive-pack ", serveServiceReceivePack},
	{"git upload-pack ", serveServiceUploadPack},
	{"git receive-pack ", serveServiceReceivePack},
}

const serveLFSCommandPrefix = "git-lfs-transfer "

// serveRequestForbiddenBytes are printable bytes a shell would treat
// specially. The grammar rejects them anyway; refusing them first keeps the
// refusal independent of how the rest of the parser evolves.
const serveRequestForbiddenBytes = "\"\\$`;|&<>"

// parseServeRequest implements the closed grammar
//
//	request     = git-command SP path / lfs-command SP path SP lfs-op
//	git-command = "git-upload-pack" / "git-receive-pack" / "git upload-pack" / "git receive-pack"
//	lfs-command = "git-lfs-transfer"
//	lfs-op      = "upload" / "download"
//	path        = "'" name-path "'" / name-path
//	name-path   = ["/"] name [".git"]
//
// with SP exactly one space and name a validServeRepositoryName. It matches
// explicit prefixes and scans bytes; no shell-word splitting is involved, so
// tabs, repeated spaces and quoting tricks cannot change the meaning.
func parseServeRequest(command string) (serveRequest, error) {
	if command == "" {
		return serveRequest{}, serveRequestRefusal("empty request")
	}
	for index := 0; index < len(command); index++ {
		character := command[index]
		if character < 0x20 || character > 0x7e {
			return serveRequest{}, serveRequestRefusal("byte 0x%02x at offset %d is not printable ASCII", character, index)
		}
		if strings.IndexByte(serveRequestForbiddenBytes, character) >= 0 {
			return serveRequest{}, serveRequestRefusal("character %q at offset %d is not allowed", character, index)
		}
	}
	if rest, ok := strings.CutPrefix(command, serveLFSCommandPrefix); ok {
		path, operation, found := strings.Cut(rest, " ")
		if !found {
			return serveRequest{}, serveRequestRefusal("git-lfs-transfer requires a path and an operation")
		}
		if operation != serveLFSOperationUpload && operation != serveLFSOperationDownload {
			return serveRequest{}, serveRequestRefusal("git-lfs-transfer operation %q is not upload or download", operation)
		}
		repo, err := parseServeRequestPath(path)
		if err != nil {
			return serveRequest{}, err
		}
		return serveRequest{Service: serveServiceLFSTransfer, Repo: repo, LFSOp: operation}, nil
	}
	for _, candidate := range serveGitCommands {
		if rest, ok := strings.CutPrefix(command, candidate.prefix); ok {
			repo, err := parseServeRequestPath(rest)
			if err != nil {
				return serveRequest{}, err
			}
			return serveRequest{Service: candidate.service, Repo: repo}, nil
		}
	}
	return serveRequest{}, serveRequestRefusal("command is not git-upload-pack, git-receive-pack or git-lfs-transfer")
}

// parseServeRequestPath reads one path argument and returns the bare
// repository name. The caller has already refused control bytes and shell
// metacharacters.
func parseServeRequestPath(path string) (string, error) {
	if strings.IndexByte(path, ' ') >= 0 {
		return "", serveRequestRefusal("extra arguments after the path")
	}
	if strings.HasPrefix(path, "'") {
		inner, closed := strings.CutSuffix(path[1:], "'")
		if !closed {
			return "", serveRequestRefusal("unmatched single quote")
		}
		path = inner
	}
	if strings.IndexByte(path, '\'') >= 0 {
		return "", serveRequestRefusal("single quote inside the path")
	}
	name := strings.TrimPrefix(path, "/")
	name = strings.TrimSuffix(name, ".git")
	if strings.IndexByte(name, '/') >= 0 {
		return "", serveRequestRefusal("path has more than one segment")
	}
	if !validServeRepositoryName(name) {
		return "", serveRequestRefusal("%q is not a valid repository name", name)
	}
	return name, nil
}

// serveSession is one forced-command invocation. Tests construct it with
// their own streams and environment; cmdServeGit uses the process's.
type serveSession struct {
	layout      serveLayout
	environment []string
	stdin       io.Reader
	stdout      io.Writer
	stderr      io.Writer
}

// cmdServeGit is the session route for hn serve git. cmdServe has already
// refused uid 0 and validated the layout read-only.
func cmdServeGit(layout serveLayout, args []string) error {
	session := &serveSession{
		layout:      layout,
		environment: os.Environ(),
		stdin:       os.Stdin,
		stdout:      os.Stdout,
		stderr:      os.Stderr,
	}
	status, err := session.run(args)
	if err != nil {
		return err
	}
	if status != 0 {
		// main maps every error to status 1 and adds an "hn: " line, but a
		// refusal has already printed its hn-serve/1 lines and Git's own
		// status must pass through. The repository lock is released before
		// run returns, so exiting here skips no cleanup.
		os.Exit(status)
	}
	return nil
}

// run applies the checks in contract order and returns the exit status:
// 1 after a refusal, Git's status after Git ran. An error is an internal
// failure that main reports with status 1.
func (s *serveSession) run(args []string) (int, error) {
	fingerprint, err := parseServeGitArguments(args)
	if err != nil {
		return 0, err
	}
	registry, err := loadServeRegistry(s.layout)
	if err != nil {
		return 0, err
	}
	binding, bound := lookupServeKey(registry, fingerprint)
	if !bound {
		return s.refuse(serveReasonUnknownKey, "", "this SSH key is not registered with the server.")
	}
	command, present := lookupServeEnvironment(s.environment, "SSH_ORIGINAL_COMMAND")
	if !present {
		return s.refuse(serveReasonUnsupportedCommand, binding.Actor, "only git fetch, git push and Git LFS transfer are supported.")
	}
	request, err := parseServeRequest(command)
	if err != nil {
		return s.refuse(serveReasonUnsupportedCommand, binding.Actor, "only git fetch, git push and Git LFS transfer are supported.")
	}
	if !serveSessionRepositoryHosted(s.layout, request.Repo) {
		return s.refuse(serveReasonUnhostedRepository, binding.Actor, "repository "+request.Repo+" is not hosted here.")
	}
	switch request.Service {
	case serveServiceUploadPack:
		// Fetches never take the repository lock, so they never wait for a
		// push; Git's ref reads are already consistent on their own.
		return s.runGit(binding, request.Repo, "upload-pack", "--strict", s.layout.RepoGitDir(request.Repo))
	case serveServiceReceivePack:
		return s.receivePack(binding, request.Repo)
	case serveServiceLFSTransfer:
		return serveLFSSession(s, binding, request)
	default:
		return 0, fmt.Errorf("hn serve git: unhandled service %q", request.Service)
	}
}

func parseServeGitArguments(args []string) (string, error) {
	if len(args) != 2 || args[0] != "--key" {
		return "", usageError(serveGitUsage)
	}
	return args[1], nil
}

// serveSessionRepositoryHosted reports whether name has a loadable
// configuration and a real repositories/NAME.git directory. Layout
// validation already paired both; checking again here closes the window
// between validation and use, and any doubt counts as not hosted.
func serveSessionRepositoryHosted(layout serveLayout, name string) bool {
	if _, err := loadServeRepoConfig(layout, name); err != nil {
		return false
	}
	info, err := os.Lstat(layout.RepoGitDir(name))
	return err == nil && info.Mode().IsDir()
}

// refuse prints one session refusal (ref=-) and its sentence to stderr and
// returns exit status 1. An empty actor renders as "-".
func (s *serveSession) refuse(code, actor, sentence string) (int, error) {
	_, _ = io.WriteString(s.stderr, renderServeRejection(serveRejection{
		Ref:      "-",
		Actor:    actor,
		Code:     code,
		Next:     serveReasonDefaultNext(code),
		Sentence: sentence,
	}))
	return 1, nil
}

// receivePack holds the repository lock for the whole receive-pack run, so
// the advertised tips, the pre-receive verdict and the ref update form one
// serialized step per repository (FR-022). The hooks run while this process
// holds the lock and must never take RepoLock themselves: a second flock
// from another process would wait until the budget expires. They rely on
// this lock instead. The lock descriptor is close-on-exec, so neither Git
// nor the hooks inherit it.
func (s *serveSession) receivePack(binding serveKeyBinding, repo string) (int, error) {
	wait := serveLockWait()
	release, err := acquireServeLock(s.layout.RepoLock(repo), wait)
	if errors.Is(err, errServeLockBusy) {
		return s.refuse(serveReasonRepositoryBusy, binding.Actor,
			"another push held "+repo+" for more than "+describeServeWait(wait)+"; retry.")
	}
	if err != nil {
		return 0, err
	}
	status, runErr := s.runGit(binding, repo,
		"-c", "core.hooksPath="+s.layout.Hooks,
		"-c", "receive.advertisePushOptions=true",
		"-c", "receive.fsckObjects=true",
		"receive-pack", s.layout.RepoGitDir(repo))
	if releaseErr := release(); releaseErr != nil && runErr == nil {
		runErr = serveStateError(s.layout.RepoLock(repo), releaseErr)
	}
	return status, runErr
}

// describeServeWait prints the lock budget for the busy sentence: whole
// seconds as "120 seconds", anything shorter (test overrides) as Go prints
// it.
func describeServeWait(wait time.Duration) string {
	if wait == time.Second {
		return "1 second"
	}
	if wait > time.Second && wait%time.Second == 0 {
		return fmt.Sprintf("%d seconds", wait/time.Second)
	}
	return wait.String()
}

// runGit runs git directly, with no shell, connected to the session's
// streams, and returns Git's exit status. Only the validated repository
// path reaches Git's arguments; the fingerprint and the raw request never
// do.
func (s *serveSession) runGit(binding serveKeyBinding, repo string, args ...string) (int, error) {
	child := exec.Command("git", args...)
	child.Dir = s.layout.Home
	child.Env = buildServeGitEnvironment(s.environment, s.layout, binding, repo)
	child.Stdin = s.stdin
	child.Stdout = s.stdout
	child.Stderr = s.stderr
	err := child.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return 0, nil
	case errors.As(err, &exitErr) && exitErr.ExitCode() >= 0:
		return exitErr.ExitCode(), nil
	case errors.As(err, &exitErr):
		return 0, fmt.Errorf("hn serve git: git did not exit normally: %v", err)
	default:
		return 0, fmt.Errorf("hn serve git: cannot run git: %v", err)
	}
}

// buildServeGitEnvironment builds Git's environment from scratch, so the
// actor a hook sees can only come from the registry and no client-supplied
// variable such as GIT_DIR, GIT_CONFIG_* or LD_PRELOAD reaches Git. PATH is
// copied because sshd sets it and a client cannot override it under
// restrict and the default PermitUserEnvironment no. GIT_PROTOCOL is copied
// only in its exact version=N form.
//
// Test exception: when the parent has HN_INTERNAL_TESTING=1, that variable
// and every HN_TEST_SERVE_* variable are copied too, so tests can shorten
// waits or inject behavior into the hooks. In production the parent
// environment comes from sshd, which a client cannot extend, and every
// consumer of these variables also checks HN_INTERNAL_TESTING=1.
func buildServeGitEnvironment(parent []string, layout serveLayout, binding serveKeyBinding, repo string) []string {
	var environment []string
	if path, ok := lookupServeEnvironment(parent, "PATH"); ok {
		environment = append(environment, "PATH="+path)
	}
	environment = append(environment,
		"HOME="+layout.Home,
		"LANG=C",
		"GIT_CONFIG_NOSYSTEM=1",
		"TMPDIR="+layout.Tmp,
	)
	if protocol, ok := lookupServeEnvironment(parent, "GIT_PROTOCOL"); ok && validServeGitProtocol(protocol) {
		environment = append(environment, "GIT_PROTOCOL="+protocol)
	}
	environment = append(environment,
		"HN_SERVE_ACTOR="+binding.Actor,
		"HN_SERVE_KEY="+binding.Fingerprint,
		"HN_SERVE_REPOSITORY="+repo,
	)
	if internal, _ := lookupServeEnvironment(parent, "HN_INTERNAL_TESTING"); internal == "1" {
		environment = append(environment, "HN_INTERNAL_TESTING=1")
		seen := make(map[string]bool)
		for _, entry := range parent {
			name, _, found := strings.Cut(entry, "=")
			if !found || !strings.HasPrefix(name, "HN_TEST_SERVE_") || seen[name] {
				continue
			}
			seen[name] = true
			environment = append(environment, entry)
		}
	}
	return environment
}

// validServeGitProtocol accepts exactly version=N with one decimal digit.
func validServeGitProtocol(value string) bool {
	digit, ok := strings.CutPrefix(value, "version=")
	return ok && len(digit) == 1 && digit[0] >= '0' && digit[0] <= '9'
}

// lookupServeEnvironment returns the first value of name in environment,
// the same occurrence os.Getenv reports for the process environment.
func lookupServeEnvironment(environment []string, name string) (string, bool) {
	for _, entry := range environment {
		if value, ok := strings.CutPrefix(entry, name+"="); ok {
			return value, true
		}
	}
	return "", false
}

// serveLFSEnabled records whether the Git LFS transfer arm below serves
// requests. It is true since WP14 replaced the refusal in serveLFSSession;
// the refusal test in serve_session_test.go skips while it is true.
const serveLFSEnabled = true

// serveLFSSession is the Git LFS transfer arm of the session dispatch. It
// runs only after the key and repository checks pass and serves the
// git-lfs-transfer protocol on the session's streams (serve_lfs.go).
func serveLFSSession(session *serveSession, binding serveKeyBinding, request serveRequest) (int, error) {
	return runServeLFSSession(session.layout, binding, request.Repo, request.LFSOp, session.stdin, session.stdout)
}
