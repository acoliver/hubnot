package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTransportReasonKeepsTheSSHCause(t *testing.T) {
	err := errors.New("git ls-remote --refs -- hub refs/hn/serve/v1: gitium@minisforum: Permission denied (publickey).\n" +
		"fatal: Could not read from remote repository.\n\nPlease make sure you have the correct access rights\nand the repository exists.")
	if got, want := transportReason(err), "gitium@minisforum: Permission denied (publickey)."; got != want {
		t.Fatalf("transportReason = %q, want %q", got, want)
	}
	if got := transportReason(errors.New("fatal: Could not read from remote repository.")); got != "git ls-remote failed" {
		t.Fatalf("transportReason of only generic advice = %q", got)
	}
	long := transportReason(errors.New(strings.Repeat("x", 500)))
	if len([]rune(long)) != 243 || !strings.HasSuffix(long, "...") {
		t.Fatalf("transportReason did not bound a long line: %d runes", len([]rune(long)))
	}
	if got := transportReason(errors.New("bad\x1b[31m")); strings.ContainsRune(got, '\x1b') {
		t.Fatalf("transportReason kept a control character: %q", got)
	}
}

func TestServeRemoteIdentifiesReportsWhyTheRemoteCannotBeListed(t *testing.T) {
	repository := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "hub", filepath.Join(t.TempDir(), "missing.git")}} {
		command := exec.Command("git", args...)
		command.Dir = repository
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repository); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	identified, reason := serveRemoteIdentifies("hub")
	if identified {
		t.Fatal("a missing remote identified as hn serve")
	}
	if !strings.HasPrefix(reason, "cannot list "+serveMarkerRef+" on the remote: ") || !strings.Contains(reason, "does not appear to be a git repository") {
		t.Fatalf("reason = %q, want the listing failure with git's cause", reason)
	}
	message := identificationLostError("hub", reason).Error()
	if !strings.HasPrefix(message, "cannot reach remote hub to confirm it is hn serve (") || strings.Contains(message, "reselect") {
		t.Fatalf("unreachable remote error = %q", message)
	}
	if got, want := identificationLostError("hub", "remote does not advertise "+serveMarkerRef).Error(),
		"remote hub no longer identifies as hn serve; reselect with hn replication select"; got != want {
		t.Fatalf("lost marker error = %q, want %q", got, want)
	}
}
