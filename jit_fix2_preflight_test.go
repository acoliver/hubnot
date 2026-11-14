package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFix2PreflightScopeAndGlobalAuthority(t *testing.T) {
	g := selectedFixture(t)
	d := newSelectedDriver(t, g)
	a := g.OpenProposal(g.Base, g.Commit(g.Base, map[string]string{"auto/a": "a\n"}))
	d.author("A", a.ID)
	g.hn("R", "sync", "origin")
	// Drop only this clone's proposal code ref after successful authenticated sync.
	for _, ref := range strings.Fields(g.git("R", "for-each-ref", "--format=%(refname)", "refs/hn")) {
		if strings.HasSuffix(ref, "/proposals/"+a.ID[7:]) {
			g.git("R", "update-ref", "-d", ref)
		}
	}
	out, rc := g.clients["R"].exec(g.work["R"], g.h.Binary, "jit-check", a.ID, "--remote", "origin", "--target", g.Base, "--requester", g.actors["R"], "--pipeline", "test", "--rule", "auto")
	var obj struct {
		OK   bool `json:"ok"`
		Data struct{ Scope, ID, Refusal string }
	}
	if err := json.Unmarshal([]byte(out), &obj); err != nil || rc != 0 || !obj.OK || obj.Data.Scope != "proposal" || obj.Data.ID != a.ID || obj.Data.Refusal == "" {
		t.Fatalf("missing explicit per-proposal preflight refusal rc=%d: %s", rc, out)
	}
	g.git("R", "update-ref", proposalRef(a.ID), a.Head)
	g.hn("R", "sync", "origin")
	g.hn("R", "replication", "select", "origin", "--actor", g.actors["R"])
	if out, rc := d.call("R", "coordinate", "--owner", "fixture-owner"); rc == 0 || !strings.Contains(out, `"status": "error"`) {
		t.Fatalf("unknown trust coverage swallowed rc=%d: %s", rc, out)
	}
	g.hn("R", "replication", "select", "origin", "--all-trusted")
	// A real signed but malformed coordinator record is global authority damage.
	fix2Comment(t, d, "R", `{"protocol":"hn.selected-author/1","kind":"selection","generation":2,"previous":"","phase":"prepare"}`)
	if out, rc := d.call("R", "coordinate", "--owner", "fixture-owner"); rc == 0 || !strings.Contains(out, "chain fork or gap") {
		t.Fatalf("generation failure swallowed rc=%d: %s", rc, out)
	}
	t.Log("explicit code-ref refusal scoped per proposal; incomplete trust coverage and signed generation gap remain fatal")
}
