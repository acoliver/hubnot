package main

import (
	"strings"
	"testing"
)

func TestSelectedAuthorUntrustedWorkerOnCurrentBinding(t *testing.T) {
	g := selectedFixture(t)
	d := newSelectedDriver(t, g)
	a := g.OpenProposal(g.Base, g.Commit(g.Base, map[string]string{"auto/trust.txt": "trust\n"}))
	d.author("A", a.ID)
	d.step()
	d.author("A", a.ID)
	d.step()
	if out, rc := d.call("A", "worker", "--backend", "host", "--allow-unsafe-host-execution"); rc != 0 || !strings.Contains(out, "refused") {
		t.Fatalf("untrusted active worker admitted: rc=%d %s", rc, out)
	}
	item := jitProposal(t, g, "R", a.ID)
	if len(item.Requests) != 1 {
		t.Fatal("missing selected original request")
	}
	request := item.Requests[0].ID
	args := []string{"runner", "once", "--remote", "origin", "--accept-pipeline", "test", "--accept-actor", g.actors["R"], "--require-requester-policy", "--strict-target", "--skip-stale-base", "main", "--only-request", request, "--backend", "host", "--allow-unsafe-host-execution"}
	if out, rc := g.clients["A"].exec(g.work["A"], g.h.Binary, args...); rc == 0 || !strings.Contains(out, "not a trusted pipeline runner") {
		t.Fatal("native strict worker ignored trust: " + out)
	}
	g.hn("M", "sync", "origin")
	if item := jitProposal(t, g, "M", a.ID); len(item.Requests[0].Results) != 0 {
		t.Fatal("untrusted worker produced result")
	}
	d.worker()
	g.hn("M", "sync", "origin")
	if item := jitProposal(t, g, "M", a.ID); len(item.Pipelines[0].Passed) != 1 {
		t.Fatal("trusted worker failed positive control")
	}
	if g.ServerTip("main") != g.Base {
		t.Fatal("worker gained advancement authority")
	}
	t.Log("real serve: untrusted identity refused on unchanged exact selected tuple by both public worker and native strict runner; trusted worker passed")
}
