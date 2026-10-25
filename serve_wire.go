package main

import (
	"fmt"
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
	// option.
	advertised, err := gitText("ls-remote", "--refs", "--", remote, serveMarkerRef)
	if err != nil {
		return false, "cannot list " + serveMarkerRef + " on the remote"
	}
	if advertised == "" {
		return false, "remote does not advertise " + serveMarkerRef
	}
	lines := strings.Split(advertised, "\n")
	if len(lines) != 1 {
		return false, fmt.Sprintf("remote advertises %d refs matching %s", len(lines), serveMarkerRef)
	}
	oid, ref, found := strings.Cut(lines[0], "\t")
	if !found || ref != serveMarkerRef {
		return false, "remote advertises an unexpected ref instead of " + serveMarkerRef
	}
	if !validGitOID(oid) || oid != want {
		return false, serveMarkerRef + " on the remote is not the hn-serve/1 marker blob"
	}
	return true, ""
}
