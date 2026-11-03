package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// The hn serve marker and push options. An hn serve hosted repository
// advertises serveMarkerRef pointing at a blob holding exactly
// serveMarkerContent; docs/protocol-v0.md documents both.
const (
	serveMarkerRef          = "refs/hn/serve/v1"
	serveMarkerContent      = "hn-serve/1\n" // 11 bytes
	servePushOptionAdvance  = "hn-advance"
	servePushOptionExpected = "hn-expected-tip"
	servePushOptionRule     = "hn-rule"
)

// serveMarkerObjectID returns the blob ID of serveMarkerContent in the object
// format of the repository at gitDir, without writing the object. An empty
// gitDir means the repository of the current directory.
func serveMarkerObjectID(gitDir string) (string, error) {
	output, err := gitInputAt(gitDir, []byte(serveMarkerContent), "hash-object", "--stdin")
	if err != nil {
		return "", err
	}
	oid := strings.TrimSpace(string(output))
	if !validGitOID(oid) {
		return "", fmt.Errorf("git hash-object returned an invalid object ID %q", oid)
	}
	return oid, nil
}

// serveRemoteIdentifies reports whether remote, a remote name or URL of the
// repository in the current directory, identifies as hn serve: it must
// advertise exactly one serveMarkerRef whose object ID equals the marker blob
// in the local object format. Every other outcome, including a transport
// error, is false with a short diagnostic for the caller's message.
//
// Identification only gates client modes such as all-trusted selection and
// remote advancement. It grants no trust: anyone can create the marker on an
// ordinary remote, so every imported ref is still validated (NFR-006).
func serveRemoteIdentifies(remote string) (bool, string) {
	if remote == "" {
		return false, "no remote given"
	}
	gitDir, err := requireGitRepository()
	if err != nil {
		return false, err.Error()
	}
	want, err := serveMarkerObjectID(gitDir)
	if err != nil {
		return false, "cannot compute the local hn serve marker object ID"
	}
	// Same invocation shape as the replication advertisement read in
	// resolveReplicationRequests: --refs, and -- so a remote cannot be an
	// option. The raw output is compared byte for byte; trimming it would
	// accept blank lines or stray spaces that are not exactly one line.
	output, err := gitOutput("ls-remote", "--refs", "--", remote, serveMarkerRef)
	if err != nil {
		return false, "cannot list " + serveMarkerRef + " on the remote: " + transportReason(err)
	}
	advertised := string(output)
	if advertised == "" {
		return false, "remote does not advertise " + serveMarkerRef
	}
	if advertised == want+"\t"+serveMarkerRef+"\n" {
		return true, ""
	}
	if lines := strings.Count(strings.TrimSuffix(advertised, "\n"), "\n") + 1; lines != 1 {
		return false, fmt.Sprintf("remote advertises %d lines for %s", lines, serveMarkerRef)
	}
	oid, found := strings.CutSuffix(advertised, "\t"+serveMarkerRef+"\n")
	if !found || !validGitOID(oid) {
		return false, "remote advertisement for " + serveMarkerRef + " is malformed"
	}
	return false, serveMarkerRef + " on the remote is not the hn-serve/1 marker blob"
}

// serveRejectedPushError is a failed git push whose remote identified as hn
// serve and relayed hn-serve/1 rejections. Error appends the rejections,
// re-rendered by renderServeRejections, to the caller's message on their own
// lines. Unwrap returns the caller's message so existing error handling and
// wording are kept.
type serveRejectedPushError struct {
	err        error
	Rejections []serveRejection
}

func (e *serveRejectedPushError) Error() string {
	return e.err.Error() + "\n" + strings.TrimSuffix(renderServeRejections(e.Rejections), "\n")
}

func (e *serveRejectedPushError) Unwrap() error {
	return e.err
}

// servePushRef runs git push REMOTE REF:REF, the publication push of hn sync.
// It returns nil on success. On failure it returns failure unchanged, unless
// Git's stderr carries hn-serve/1 rejections and the remote identifies as hn
// serve; then the returned serveRejectedPushError adds those rejections.
//
// The rejections are parsed and rendered again, never echoed, so every byte
// a server sends passes the hn-serve/1 sanitizer before it reaches a
// terminal. Identification runs only after a failed push that carries
// rejection lines, so ordinary remotes cost no extra round trip.
func servePushRef(remote, ref string, failure error) error {
	stderr, err := gitPushStderr("push", remote, ref+":"+ref)
	if err == nil {
		return nil
	}
	rejections, _ := extractServeRejections(stderr)
	if len(rejections) == 0 {
		return failure
	}
	if identified, _ := serveRemoteIdentifies(remote); !identified {
		return failure
	}
	return &serveRejectedPushError{err: failure, Rejections: rejections}
}

// gitPushStderr runs git with the environment gitInputWithDirectory uses and
// returns its stderr, which carries the relayed remote: lines that
// gitInputWithDirectory folds into its error message.
func gitPushStderr(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Stdin = bytes.NewReader(nil)
	cmd.Env = os.Environ()
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stderr.String(), err
}

// transportReason is the first informative line of a git error, without
// gitRun's "git ARGS: " prefix and git's generic remote-access advice,
// bounded and with control characters replaced. For an SSH remote it is
// ssh's own reason, such as "Permission denied (publickey)".
func transportReason(err error) string {
	reason := ""
	for index, line := range strings.Split(err.Error(), "\n") {
		line = strings.TrimSpace(line)
		if index == 0 && strings.HasPrefix(line, "git ") {
			if _, rest, found := strings.Cut(line, ": "); found {
				line = strings.TrimSpace(rest)
			}
		}
		if line == "" || strings.HasPrefix(line, "fatal: Could not read from remote repository") ||
			strings.HasPrefix(line, "Please make sure you have the correct access rights") || line == "and the repository exists." {
			continue
		}
		reason = line
		break
	}
	if reason == "" {
		reason = "git ls-remote failed"
	}
	if runes := []rune(reason); len(runes) > 240 {
		reason = string(runes[:240]) + "..."
	}
	return safeText(reason)
}

// identificationLostError is the import error of a sync whose all-trusted
// selection could not confirm the remote is hn serve. A remote that could
// not be listed at all is an access problem, such as a missing SSH key, and
// says so; reselecting would not help. A remote that answered without the
// marker no longer identifies.
func identificationLostError(remote, reason string) error {
	if strings.HasPrefix(reason, "cannot list ") {
		return fmt.Errorf("cannot reach remote %s to confirm it is hn serve (%s); hosted facts were not imported", remote, reason)
	}
	return fmt.Errorf("remote %s no longer identifies as hn serve; reselect with hn replication select", remote)
}
