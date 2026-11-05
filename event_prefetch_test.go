package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func prefetchFixtureCommit(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	for _, name := range []string{"event.json", "signature"} {
		_ = os.Remove(filepath.Join(dir, name))
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("add", "-A")
	run("-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "e")
	return run("rev-parse", "HEAD")
}

// The prefetch returns exact blob bytes, and leaves out every commit it
// cannot read as two blobs so that the per-event path reports the error.
func TestPrefetchEventBlobsReadsExactBytesAndLeavesOutUnreadableCommits(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	good := prefetchFixtureCommit(t, dir, map[string]string{"event.json": "{\"a\":1}\n\n", "signature": "c2ln\n"})
	missing := prefetchFixtureCommit(t, dir, map[string]string{"event.json": "{}"})
	empty := prefetchFixtureCommit(t, dir, map[string]string{"event.json": "", "signature": "x"})
	wd, _ := os.Getwd()
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	got := prefetchEventBlobs([]string{good, empty})
	if pair, ok := got[good]; !ok || string(pair[0]) != "{\"a\":1}\n\n" || string(pair[1]) != "c2ln\n" {
		t.Fatalf("good commit: %q %v", pair, ok)
	}
	if pair, ok := got[empty]; !ok || len(pair[0]) != 0 || string(pair[1]) != "x" {
		t.Fatalf("empty event.json: %q %v", pair, ok)
	}
	got = prefetchEventBlobs([]string{good, missing, empty})
	if _, ok := got[missing]; ok {
		t.Fatal("a commit without a signature blob was prefetched")
	}
	if _, ok := got[empty]; ok {
		t.Fatal("a commit after an unreadable one was prefetched from misaligned output")
	}
	if _, ok := got[good]; !ok {
		t.Fatal("the commit before the unreadable one was dropped")
	}
	if got := prefetchEventBlobs([]string{"not-an-id"}); len(got) != 0 {
		t.Fatal("an invalid commit ID was read")
	}
}
