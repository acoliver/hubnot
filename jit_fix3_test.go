package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Preserve a signed real-server clone for the identical old/new native probe.
func TestFix3PrepareSignedSnapshot(t *testing.T) {
	root := os.Getenv("HN_FIX3_ROOT")
	if root == "" {
		t.Skip("explicit owned snapshot root required")
	}
	g := selectedFixture(t)
	a := g.OpenProposal(g.Base, g.Commit(g.Base, map[string]string{"auto/fix3": "signed probe\n"}))
	g.hn("R", "sync", "origin")
	dest := filepath.Join(root, "scope-fixture")
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("snapshot destination must be new")
	}
	if out, err := exec.Command("cp", "-R", g.work["R"], dest).CombinedOutput(); err != nil {
		t.Fatalf("copy signed clone: %v: %s", err, out)
	}
	t.Logf("real signed proposal %s snapshot %s", a.ID, dest)
}
