package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelectedAuthorPreparationConflictReportsAttention(t *testing.T) {
	g := selectedFixture(t)
	d := newSelectedDriver(t, g)
	a := g.OpenProposal(g.Base, g.Commit(g.Base, map[string]string{"auto/shared.txt": "author\n"}))
	external := g.Commit(g.Base, map[string]string{"auto/shared.txt": "external\n"})
	g.Host(external)
	g.SetMainForTest(external)
	d.author("A", a.ID)
	d.step()
	out := d.author("A", a.ID)
	if !strings.Contains(out, "needs-attention") || !strings.Contains(out, "preparation") {
		t.Fatal("preparation conflict was not reported: " + out)
	}
	// Retry through the real lander and its existing synthetic-repository reporter.
	g.git("A", "remote", "add", "hub", g.git("A", "remote", "get-url", "origin"))
	g.git("A", "branch", "conflicting-topic", a.Head)
	g.git("A", "checkout", "-q", "main")
	land, err := filepath.Abs("../integration/land.sh")
	if err != nil {
		t.Fatal(err)
	}
	client, err := filepath.Abs("selected_author.py")
	if err != nil {
		t.Fatal(err)
	}
	env := []string{"HUBNOT_STRICT_JIT_CONFIG=" + d.config(), "HUBNOT_STRICT_JIT_CLIENT=" + client, "PATH=" + filepath.Dir(g.h.Binary) + ":" + os.Getenv("PATH")}
	out, rc, err := g.advanceExec("A", env, "bash", land, a.ID, "--branch", "conflicting-topic", "--jit-once")
	if err != nil || rc != 2 || !strings.Contains(out, "needs-attention") {
		t.Fatalf("lander did not report attention: rc=%d err=%v %s", rc, err, out)
	}
	issues := g.hn("A", "issue", "list", "--json")
	if !strings.Contains(issues, "[landing-failed]") || !strings.Contains(issues, "conflicting-topic") {
		t.Fatal("attention issue absent: " + issues)
	}
	if d.count("R", a.ID) != 0 {
		t.Fatal("conflicting preparation received gate")
	}
	current := jitProposal(t, g, "R", a.ID)
	if current.Lineage.Superseded || g.ServerTip("main") != external {
		t.Fatal("conflict revised original or advanced main")
	}
	t.Log("real serve: selected preparation conflict and retry reach actual lander attention issue without revision, gate or advancement")
}
