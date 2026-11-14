package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A slow but healthy policy Git command must finish inside the bound, and a
// command slower than the bound must fail with the deadline error.
func TestPolicyGitTimeoutBoundsSlowCommands(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not found")
	}
	bin := t.TempDir()
	shim := "#!/bin/sh\nsleep 1\nexec " + realGit + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	saved := policyGitTimeout
	t.Cleanup(func() { policyGitTimeout = saved })

	policyGitTimeout = 10 * time.Second
	if out, err := boundedPolicyGitOutput("", 128, "--version"); err != nil || !strings.HasPrefix(string(out), "git version") {
		t.Fatalf("slow git within the bound: out=%q err=%v", out, err)
	}

	policyGitTimeout = 200 * time.Millisecond
	_, err = boundedPolicyGitOutput("", 128, "--version")
	if err == nil || !strings.Contains(err.Error(), "policy Git inspection") {
		t.Fatalf("slow git past the bound: err=%v", err)
	}
}

func TestPolicyGitTimeoutDefaultIsGenerous(t *testing.T) {
	if policyGitTimeout < 5*time.Minute {
		t.Fatalf("policyGitTimeout = %v; a loaded machine needs minutes, not seconds", policyGitTimeout)
	}
}
