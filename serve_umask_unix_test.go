//go:build unix

package main

import (
	"os"
	"os/exec"
	"path/filepath"
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
	initBare("hn-audit-objects-*")
	if err := checkServeTransientTree(tmp, serveTransientEntryLimit); err != nil {
		t.Fatalf("with the serve umask: %v", err)
	}
}
