//go:build unix

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// A temporary bare repository that a serve session creates under state/tmp
// must pass the state check another session runs while it exists.
func TestServeUmaskKeepsASessionsTemporaryRepositoryValid(t *testing.T) {
	old := syscall.Umask(0o002) // the account default that produced 0664
	defer syscall.Umask(old)
	tmp := t.TempDir()
	if err := os.Chmod(tmp, 0o700); err != nil {
		t.Fatal(err)
	}
	initBare := func(name string) string {
		dir, err := os.MkdirTemp(tmp, name)
		if err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("git", "init", "--bare", "--quiet", "--template=", dir).CombinedOutput(); err != nil {
			t.Fatalf("git init: %v\n%s", err, out)
		}
		return dir
	}
	loose := initBare("hn-audit-objects-*")
	if err := checkServeTransientTree(tmp, serveTransientEntryLimit); err == nil {
		t.Fatalf("without the serve umask the check accepted %s", filepath.Join(loose, "HEAD"))
	}
	if err := os.RemoveAll(loose); err != nil {
		t.Fatal(err)
	}
	restrictServeUmask()
	repo := initBare("hn-audit-objects-*")
	if err := checkServeTransientTree(tmp, serveTransientEntryLimit); err != nil {
		t.Fatalf("with the serve umask: %v", err)
	}
	// The audit object check indexes packs there; Git writes the pack and
	// its index read-only (0400 under the serve umask), and a session that
	// starts meanwhile walks them.
	src := t.TempDir()
	run := func(dir string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run(src, "init", "--quiet")
	run(src, "commit", "--quiet", "--allow-empty", "-m", "x")
	pack := exec.Command("git", "pack-objects", "--stdout", "--revs")
	pack.Dir = src
	pack.Stdin = strings.NewReader("HEAD\n")
	data, err := pack.Output()
	if err != nil {
		t.Fatal(err)
	}
	index := exec.Command("git", "index-pack", "--stdin", "--fsck-objects")
	index.Dir = repo
	index.Stdin = bytes.NewReader(data)
	if out, err := index.CombinedOutput(); err != nil {
		t.Fatalf("index-pack: %v\n%s", err, out)
	}
	packs, _ := filepath.Glob(filepath.Join(repo, "objects", "pack", "*.idx"))
	if len(packs) == 0 {
		t.Fatal("index-pack wrote no index")
	}
	if info, err := os.Stat(packs[0]); err != nil || info.Mode().Perm()&0o200 != 0 {
		t.Fatalf("expected a read-only index, got %v %v", info.Mode(), err)
	}
	if err := checkServeTransientTree(tmp, serveTransientEntryLimit); err != nil {
		t.Fatalf("a read-only pack of a running audit check is a defect: %v", err)
	}
	// Group or other access is still refused.
	loosePack := filepath.Join(repo, "objects", "pack", "loose")
	if err := os.WriteFile(loosePack, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(loosePack, 0o440); err != nil {
		t.Fatal(err)
	}
	if err := checkServeTransientTree(tmp, serveTransientEntryLimit); err == nil {
		t.Fatal("a group-readable file was accepted")
	}
}
