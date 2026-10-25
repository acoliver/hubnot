package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
)

func gitOutput(args ...string) ([]byte, error) {
	return gitInput(nil, nil, args...)
}

func gitText(args ...string) (string, error) {
	out, err := gitOutput(args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func gitInput(input []byte, extraEnv []string, args ...string) ([]byte, error) {
	return gitInputWithDirectory("", input, extraEnv, args...)
}

func gitOutputAt(gitDir string, args ...string) ([]byte, error) {
	return gitInputAt(gitDir, nil, args...)
}

func gitTextAt(gitDir string, args ...string) (string, error) {
	out, err := gitOutputAt(gitDir, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func gitInputAt(gitDir string, input []byte, args ...string) ([]byte, error) {
	return gitInputWithDirectory(gitDir, input, nil, args...)
}

func gitInputWithDirectory(gitDir string, input []byte, extraEnv []string, args ...string) ([]byte, error) {
	if gitDir != "" {
		args = append([]string{"--git-dir=" + gitDir}, args...)
	}
	cmd := exec.Command("git", args...)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Env = append(os.Environ(), extraEnv...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), message)
	}
	return stdout.Bytes(), nil
}

// copyGitObjects streams one ordinary, non-thin pack from the quarantine
// object database into the main repository without creating a ref or writing
// FETCH_HEAD. Accepted refs are updated separately after every object is
// verified in the destination.
func copyGitObjects(sourceGitDir, destinationGitDir string, roots []string) error {
	if len(roots) == 0 {
		return nil
	}
	input := strings.NewReader(strings.Join(roots, "\n") + "\n")
	producer := exec.Command("git", "--git-dir="+sourceGitDir, "pack-objects", "--stdout", "--revs")
	consumer := exec.Command("git", "--git-dir="+destinationGitDir, "index-pack", "--stdin")
	pipe, err := producer.StdoutPipe()
	if err != nil {
		return fmt.Errorf("prepare quarantine object copy: %w", err)
	}
	producer.Stdin = input
	var producerError, consumerError bytes.Buffer
	producer.Stderr = &producerError
	consumer.Stdin = pipe
	consumer.Stdout = io.Discard
	consumer.Stderr = &consumerError
	if err := consumer.Start(); err != nil {
		return fmt.Errorf("start object import: %w", err)
	}
	if err := producer.Start(); err != nil {
		_ = consumer.Process.Kill()
		_ = consumer.Wait()
		return fmt.Errorf("start quarantine object export: %w", err)
	}
	producerErr := producer.Wait()
	consumerErr := consumer.Wait()
	if producerErr != nil {
		return fmt.Errorf("export quarantine objects: %s", commandFailure(producerErr, producerError.String()))
	}
	if consumerErr != nil {
		return fmt.Errorf("import quarantine objects: %s", commandFailure(consumerErr, consumerError.String()))
	}
	return nil
}

func commandFailure(err error, stderr string) string {
	message := strings.TrimSpace(stderr)
	if message == "" {
		message = err.Error()
	}
	return message
}

func requireGitRepository() (string, error) {
	gitDir, err := gitText("rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", fmt.Errorf("not inside a Git repository")
	}
	return gitDir, nil
}

func refValue(ref string) (string, bool, error) {
	out, err := gitOutput("rev-parse", "--verify", "--quiet", ref)
	if err != nil {
		// rev-parse returns status 1 when a ref is absent. Check with show-ref so
		// actual repository errors are still surfaced by callers elsewhere.
		return "", false, nil
	}
	return strings.TrimSpace(string(out)), true, nil
}

// cloneTransportSections are the configuration sections whose keys all
// shape how Git reaches a remote. cloneTransportKeys are single transport
// keys from other sections.
var (
	cloneTransportSections = []string{"credential", "http", "protocol", "ssh"}
	cloneTransportKeys     = []string{"core.askpass", "core.gitproxy", "core.sshcommand"}
)

// cloneTransportConfigEnvironment carries the current clone's own transport
// configuration, such as core.sshCommand, ssh.variant, http.*, credential.*
// and protocol.*, into a Git process that runs against another repository,
// for example a replication fetch into a temporary quarantine. That process
// already reads the system, global and command-line configuration; only the
// clone's local and worktree scopes, includes followed, are missing.
//
// The keys travel as GIT_CONFIG_COUNT entries, so values such as an
// http.extraHeader token never appear in a process argument list. The
// clone's entries come first and any GIT_CONFIG_COUNT entries already in
// the environment follow, so the command-line scope still wins as it does
// in the clone. GIT_SSH_COMMAND and other transport variables keep
// overriding the configuration. Refs, hooks, remotes, url.* rewrites
// (already applied by git remote get-url) and every other key stay behind.
//
// It returns nil when the clone has no transport configuration, and also
// when Git cannot list it: --show-scope needs Git 2.26, and GIT_CONFIG_COUNT
// needs Git 2.31. An older Git then fetches with the configuration it had
// before, where the environment, such as GIT_SSH_COMMAND, still applies.
func cloneTransportConfigEnvironment() []string {
	listing, err := gitOutput("config", "--list", "--show-scope", "--includes", "-z")
	if err != nil {
		return nil
	}
	var keys, values []string
	fields := strings.Split(strings.TrimSuffix(string(listing), "\x00"), "\x00")
	if len(fields)%2 != 0 {
		return nil
	}
	for index := 0; index < len(fields); index += 2 {
		scope, entry := fields[index], fields[index+1]
		if scope != "local" && scope != "worktree" {
			continue
		}
		key, value, valued := strings.Cut(entry, "\n")
		if !cloneTransportKey(key) {
			continue
		}
		// A key without a value is an implicit boolean true. An empty
		// value would read as false and could turn http.sslVerify off.
		if !valued {
			value = "true"
		}
		keys, values = append(keys, key), append(values, value)
	}
	if len(keys) == 0 {
		return nil
	}
	inherited := 0
	if count, set := os.LookupEnv("GIT_CONFIG_COUNT"); set && count != "" {
		inherited, err = strconv.Atoi(count)
		if err != nil || inherited < 0 {
			// Git itself rejects this environment; leave it for Git to report.
			return nil
		}
	}
	for index := 0; index < inherited; index++ {
		keys = append(keys, os.Getenv(fmt.Sprintf("GIT_CONFIG_KEY_%d", index)))
		values = append(values, os.Getenv(fmt.Sprintf("GIT_CONFIG_VALUE_%d", index)))
	}
	environment := make([]string, 0, 2*len(keys)+1)
	environment = append(environment, "GIT_CONFIG_COUNT="+strconv.Itoa(len(keys)))
	for index := range keys {
		environment = append(environment,
			fmt.Sprintf("GIT_CONFIG_KEY_%d=%s", index, keys[index]),
			fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s", index, values[index]))
	}
	return environment
}

// cloneTransportKey reports whether key, as git config --list prints it,
// is transport configuration that cloneTransportConfigEnvironment carries.
func cloneTransportKey(key string) bool {
	if slices.Contains(cloneTransportKeys, key) {
		return true
	}
	section, _, found := strings.Cut(key, ".")
	return found && slices.Contains(cloneTransportSections, section)
}
