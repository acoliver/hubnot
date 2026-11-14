package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// This adapter executes the real hn, then kills only its own verified parent
// coordinator at a deterministic publication boundary. It supplies no policy.
func selectedCrashAdapter(t *testing.T, g *serveGovernedFixture, boundary string) string {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "hn-crash")
	code := fmt.Sprintf(`#!%s
import os, signal, subprocess, sys
args=sys.argv[1:]
env=dict(os.environ)
match=%q
hit=(match=="request" and args[:2]==["run","request"]) or (match=="merge" and args[:1]==["automerge"])
if hit and match=="merge":
    env["HN_INTERNAL_TESTING"]="1"
    env["HN_TEST_SERVE_ADVANCE_INTERRUPT_AFTER"]="push"
p=subprocess.run([%q,*args],env=env)
if hit:
    parent=os.getppid()
    command=subprocess.check_output(["ps","-p",str(parent),"-o","command="],text=True)
    assert "selected_author.py" in command and "coordinate" in command
    os.kill(parent,signal.SIGKILL)
sys.exit(p.returncode)
`, python, boundary, g.h.Binary)
	if err := os.WriteFile(p, []byte(code), 0700); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSelectedAuthorPublicationCrashAndWorkerFences(t *testing.T) {
	g := selectedFixture(t)
	d := newSelectedDriver(t, g)
	a := g.OpenProposal(g.Base, g.Commit(g.Base, map[string]string{"auto/a": "a\n"}))
	d.author("A", a.ID)
	d.step()
	d.author("A", a.ID)
	if out, rc := d.call("R", "coordinate", "--owner", "fixture-owner", "--hn", selectedCrashAdapter(t, g, "request")); rc == 0 {
		t.Fatal("coordinator was not killed at request: " + out)
	}
	d.step()
	if d.count("R", a.ID) != 1 {
		t.Fatal("request publication crash duplicated admission")
	}
	item := jitProposal(t, g, "R", a.ID)
	request := item.Requests[0].ID
	// Simulate the operational wrapper appending its global actor list. The
	// native strict mode rejects widened selection before running anything.
	args := []string{"runner", "once", "--remote", "origin", "--accept-pipeline", "test", "--accept-actor", g.actors["R"], "--accept-actor", g.actors["A"], "--require-requester-policy", "--strict-target", "--skip-stale-base", "main", "--only-request", request, "--backend", "host", "--allow-unsafe-host-execution"}
	if out, rc := g.clients["U"].exec(g.work["U"], g.h.Binary, args...); rc == 0 || !strings.Contains(out, "one policy-fenced signer") {
		t.Fatal("widened wrapper accepted: " + out)
	}
	args = []string{"runner", "once", "--remote", "origin", "--accept-pipeline", "test", "--accept-actor", g.actors["R"], "--require-requester-policy", "--strict-target", "--skip-stale-base", "absent-target", "--only-request", request, "--backend", "host", "--allow-unsafe-host-execution"}
	if out, rc := g.clients["U"].exec(g.work["U"], g.h.Binary, args...); rc == 0 || !strings.Contains(out, "remote branch unavailable") {
		t.Fatal("unknown target failed open: " + out)
	}
	unselected := g.OpenProposal(g.Base, g.Commit(g.Base, map[string]string{"auto/unselected": "unselected\n"}))
	g.push("A", "origin", unselected.Head+":refs/heads/queue/want/"+unselected.ID[7:19])
	g.act("R", "run.request", "run", "request", unselected.ID, "test")
	d.worker()
	d.worker()
	g.hn("M", "sync", "origin")
	if item := jitProposal(t, g, "M", unselected.ID); len(item.Requests) != 1 || len(item.Requests[0].Results) != 0 {
		t.Fatal("unselected same-signer legacy request executed")
	}
	if out, rc := d.call("R", "coordinate", "--owner", "fixture-owner", "--hn", selectedCrashAdapter(t, g, "merge")); rc == 0 {
		t.Fatal("coordinator was not killed after server advance: " + out)
	}
	if g.ServerTip("main") != a.Head {
		t.Fatal("crash did not follow actual server advancement")
	}
	d.step()
	d.step()
	d.step()
	if g.ServerTip("main") != a.Head {
		t.Fatal("unsigned hint selected unrelated original")
	}
	if d.count("R", a.ID) != 1 {
		t.Fatal("merge crash requested gate again")
	}
	final := jitProposal(t, g, "R", a.ID)
	if !final.Merged || len(final.Merges) != 1 {
		t.Fatalf("original merge not repaired exactly once: %+v", final.Merges)
	}
	t.Log("real serve: SIGKILL after request append and after server advance recovered once; global accept append rejected; missing worker target refused")
}
