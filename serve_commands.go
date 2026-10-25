package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const serveRootRefusal = "hn serve refuses to run as root; create an unprivileged user for the server"

// serveRoute is one hn serve subcommand. cmdServe validates the layout in
// mode before it calls the route, so no subcommand can skip validation.
// Session routes set run, which receives the validated layout.
// Administration routes set administer, which runs inside the
// administration preamble (runServeAdministration).
type serveRoute struct {
	mode       serveValidationMode
	run        func(layout serveLayout, args []string) error
	administer func(admin *serveAdministration, args []string) error
}

// serveRouteFor maps a subcommand to its route. repo and key use
// administration mode; git uses session mode, and hook (WP07) adds the
// other session-mode case:
//
//	case "hook": return serveRoute{mode: serveSessionValidation, run: cmdServeHook}, true
func serveRouteFor(subcommand string) (serveRoute, bool) {
	switch subcommand {
	case "repo":
		return serveRoute{mode: serveAdministrationValidation, administer: cmdServeRepo}, true
	case "key":
		return serveRoute{mode: serveAdministrationValidation, administer: cmdServeKey}, true
	case "git":
		return serveRoute{mode: serveSessionValidation, run: cmdServeGit}, true
	}
	return serveRoute{}, false
}

func cmdServe(args []string) error {
	if err := refuseServeAsRoot(); err != nil {
		return err
	}
	if len(args) == 0 {
		return usageError("usage: hn serve repo|key|git|hook ...")
	}
	route, ok := serveRouteFor(args[0])
	if !ok {
		return fmt.Errorf("unknown hn serve subcommand %q; run 'hn help'", args[0])
	}
	layout, err := resolveServeLayout()
	if err != nil {
		return err
	}
	if route.mode == serveAdministrationValidation {
		return runServeAdministration(layout, route.administer, args[1:])
	}
	if err := prepareServeLayout(layout, route.mode); err != nil {
		return err
	}
	return route.run(layout, args[1:])
}

// prepareServeLayout creates the fixed directories for administration
// commands, then validates the whole layout. Session commands only validate.
// cmdServe sends administration routes through runServeAdministration
// instead, which performs the same two steps with the administration lock
// taken between them.
func prepareServeLayout(layout serveLayout, mode serveValidationMode) error {
	if mode == serveAdministrationValidation {
		if err := ensureServeLayout(layout); err != nil {
			return err
		}
	}
	return validateServeLayout(layout, mode)
}

// refuseServeAsRoot runs before any path is resolved or state is read.
func refuseServeAsRoot() error {
	euid, err := serveEffectiveUID()
	if err != nil {
		return err
	}
	if euid == 0 {
		return errors.New(serveRootRefusal)
	}
	return nil
}

// serveEffectiveUID honours HN_TEST_SERVE_EUID only when HN_INTERNAL_TESTING=1
// so tests on an ordinary account can exercise the root refusal.
func serveEffectiveUID() (int, error) {
	if os.Getenv("HN_INTERNAL_TESTING") == "1" {
		if value, ok := os.LookupEnv("HN_TEST_SERVE_EUID"); ok {
			return parseServeTestUID(value)
		}
	}
	return os.Geteuid(), nil
}

func parseServeTestUID(value string) (int, error) {
	malformed := fmt.Errorf("HN_TEST_SERVE_EUID must be a decimal user ID; got %q", value)
	if value == "" {
		return 0, malformed
	}
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return 0, malformed
		}
	}
	uid, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, malformed
	}
	return int(uid), nil
}

// Administration subcommands. They run inside the administration preamble;
// repo create, repo set, key add and key remove live in serve_admin.go.

// The bare-subcommand usage texts predate the write commands and are kept
// byte for byte because WP01's tests pin them; hn help lists every
// subcommand.
func cmdServeRepo(admin *serveAdministration, args []string) error {
	if len(args) == 0 {
		return usageError("usage: hn serve repo list|show|lock-exec ...")
	}
	layout := admin.layout
	switch args[0] {
	case "create":
		return cmdServeRepoCreate(admin, args[1:])
	case "list":
		if err := admin.refreshAuthorizedKeys(); err != nil {
			return err
		}
		return cmdServeRepoList(layout, args[1:])
	case "show":
		if err := admin.refreshAuthorizedKeys(); err != nil {
			return err
		}
		return cmdServeRepoShow(layout, args[1:])
	case "set":
		return cmdServeRepoSet(admin, args[1:])
	case "lock-exec":
		if err := admin.refreshAuthorizedKeys(); err != nil {
			return err
		}
		return cmdServeRepoLockExec(admin, args[1:])
	default:
		return fmt.Errorf("unknown hn serve repo subcommand %q; run 'hn help'", args[0])
	}
}

func cmdServeKey(admin *serveAdministration, args []string) error {
	if len(args) == 0 {
		return usageError("usage: hn serve key list")
	}
	switch args[0] {
	case "add":
		return cmdServeKeyAdd(admin, args[1:])
	case "remove":
		return cmdServeKeyRemove(admin, args[1:])
	case "list":
		if err := admin.refreshAuthorizedKeys(); err != nil {
			return err
		}
		return cmdServeKeyList(admin.layout, args[1:])
	default:
		return fmt.Errorf("unknown hn serve key subcommand %q; run 'hn help'", args[0])
	}
}

// cmdServeRepoList prints one hosted repository name per line in byte
// order. Validation has already paired every state directory with its
// repository, so the state directory names are the hosted names.
func cmdServeRepoList(layout serveLayout, args []string) error {
	if len(args) != 0 {
		return usageError("usage: hn serve repo list")
	}
	names, err := listServeDirectory(layout.Repos)
	if err != nil {
		return err
	}
	for _, name := range names {
		fmt.Println(name)
	}
	return nil
}

func cmdServeRepoShow(layout serveLayout, args []string) error {
	if len(args) != 1 {
		return usageError("usage: hn serve repo show NAME")
	}
	name := args[0]
	if err := requireServeHostedRepository(layout, name); err != nil {
		return err
	}
	config, err := loadServeRepoConfig(layout, name)
	if err != nil {
		return err
	}
	return printServeRepo(layout, config)
}

// printServeRepo prints the hn serve repo show format, which hn serve repo
// set also prints after a change.
func printServeRepo(layout serveLayout, config serveRepoConfig) error {
	name := config.Name
	stored, err := serveLFSStoredBytes(layout, name)
	if err != nil {
		return err
	}
	fmt.Printf("name: %s\n", config.Name)
	fmt.Printf("path: %s\n", layout.RepoGitDir(name))
	for _, ref := range config.Protected {
		fmt.Printf("protected: %s\n", ref)
	}
	fmt.Printf("lfs-quota: %d\n", config.LFSQuotaBytes)
	fmt.Printf("lfs-stored: %d\n", stored)
	fmt.Printf("max-events: %d\n", config.Budgets.MaxEvents)
	fmt.Printf("max-objects: %d\n", config.Budgets.MaxObjects)
	fmt.Printf("max-object-bytes: %d\n", config.Budgets.MaxObjectBytes)
	fmt.Printf("max-attachment-bytes: %d\n", config.Budgets.MaxAttachmentBytes)
	fmt.Printf("max-total-bytes: %d\n", config.Budgets.MaxTotalBytes)
	return nil
}

// serveLFSStoredBytes sums regular-file sizes below lfs/objects. Validation
// has already refused symlinks, special files and oversized trees there.
func serveLFSStoredBytes(layout serveLayout, name string) (int64, error) {
	root := layout.RepoLFSObjects(name)
	if _, err := os.Lstat(root); errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	var total int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		return 0, serveStateError(root, err)
	}
	return total, nil
}

// acquireServeRepoLock takes the repository lock that pushes, lock-exec and
// repo set share, and reports a lock that stays busy for the whole wait as
// repository-busy.
func acquireServeRepoLock(layout serveLayout, name string) (func() error, error) {
	release, err := acquireServeLock(layout.RepoLock(name), serveLockWait())
	if errors.Is(err, errServeLockBusy) {
		return nil, fmt.Errorf("repository-busy: another push or lock-exec holds the lock for %s; try again when it finishes", name)
	}
	return release, err
}

// requireServeHostedRepository refuses a malformed or unregistered name.
// Validation has already checked that a hosted name has both a real
// repositories/NAME.git directory and its state directory.
func requireServeHostedRepository(layout serveLayout, name string) error {
	if !validServeRepositoryName(name) {
		return fmt.Errorf("invalid hosted repository name %q", name)
	}
	if _, err := os.Lstat(layout.RepoStateDir(name)); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("repository %s is not hosted; run 'hn serve repo list'", name)
	} else if err != nil {
		return serveStateError(layout.RepoStateDir(name), err)
	}
	return nil
}

// cmdServeRepoLockExec runs one command under the repository lock, for the
// documented recovery of a protected tip without valid policy. It does not
// use a shell: COMMAND and its arguments go straight to exec. It releases
// the administration lock first, so a long recovery does not block key or
// repository changes.
func cmdServeRepoLockExec(admin *serveAdministration, args []string) error {
	if len(args) < 3 || args[1] != "--" {
		return usageError("usage: hn serve repo lock-exec NAME -- COMMAND [ARG]...")
	}
	layout := admin.layout
	name := args[0]
	if err := requireServeHostedRepository(layout, name); err != nil {
		return err
	}
	if err := admin.finish(); err != nil {
		return err
	}
	release, err := acquireServeRepoLock(layout, name)
	if err != nil {
		return err
	}
	status, runErr := runServeLockedCommand(layout.RepoGitDir(name), args[2], args[3:])
	if releaseErr := release(); releaseErr != nil && runErr == nil {
		runErr = serveStateError(layout.RepoLock(name), releaseErr)
	}
	if runErr != nil {
		return runErr
	}
	if status != 0 {
		// main maps every error to exit status 1, and lock-exec must pass
		// the command's own status through. The lock is already released and
		// nothing else is held, so exiting here skips no cleanup.
		os.Exit(status)
	}
	return nil
}

// runServeLockedCommand runs command in the bare repository with GIT_DIR set
// to it and the caller's other environment unchanged. It returns the exit
// status of a command that ran to completion.
func runServeLockedCommand(gitDir, command string, args []string) (int, error) {
	child := exec.Command(command, args...)
	child.Dir = gitDir
	environment := make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_DIR=") {
			environment = append(environment, entry)
		}
	}
	child.Env = append(environment, "GIT_DIR="+gitDir)
	child.Stdin = os.Stdin
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	err := child.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return 0, nil
	case errors.As(err, &exitErr) && exitErr.ExitCode() >= 0:
		return exitErr.ExitCode(), nil
	case errors.As(err, &exitErr):
		return 0, fmt.Errorf("hn serve repo lock-exec: %s did not exit normally: %v", command, err)
	default:
		return 0, fmt.Errorf("hn serve repo lock-exec: cannot run %s: %v", command, err)
	}
}

// cmdServeKeyList prints FINGERPRINT ACTOR KEYTYPE per binding. The registry
// is stored sorted by fingerprint, so the output is too.
func cmdServeKeyList(layout serveLayout, args []string) error {
	if len(args) != 0 {
		return usageError("usage: hn serve key list")
	}
	registry, err := loadServeRegistry(layout)
	if err != nil {
		return err
	}
	for _, binding := range registry.Keys {
		fmt.Printf("%s %s %s\n", binding.Fingerprint, binding.Actor, serveKeyType(binding))
	}
	return nil
}
