package main

import (
	"encoding/json"
	"os"
	"testing"
)

// serveAuditOutcome is event.audit.outcome of an audit execute response.
func serveAuditOutcome(t *testing.T, data json.RawMessage) string {
	t.Helper()
	var response struct {
		Event struct {
			Audit struct {
				Outcome string `json:"outcome"`
			} `json:"audit"`
		} `json:"event"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatal(err)
	}
	return response.Event.Audit.Outcome
}

// Audits stay observational on protected branches. The trusted runner of
// pipeline test also holds every audit role and audits candidates with
// rule test, the protected pipeline's own committed definition. A failed
// audit of a ready candidate's head does not block the protected push,
// and a passing audit of a candidate whose latest CI result failed does
// not supply the missing CI: the push is protected-not-ready naming CI
// only. Both audit results are hosted facts when main is pushed.
func TestServeAuditObservationalOnProtectedBranches(t *testing.T) {
	g := newServeGovernedFixture(t, serveGovernedOptions{})
	runner := g.Runner
	policy := serveAuditEnable(g.servePushFixture, runner, map[string]string{"test": ""})
	serveAuditID(t, g.audit(runner, "rule", "authorize", "test", "--source", "pipeline", "--at", policy))
	auditHead := func(head, intent string, fail bool) string {
		t.Helper()
		g.hn(runner, "sync", "origin")
		request := serveAuditID(t, g.audit(runner, "request", "--at", head, "--rule", "test", "--intent", intent))
		if fail {
			writeServeTestFile(t, g.CIFailFlag, "fail\n", 0o644)
		}
		result := g.audit(runner, "execute", request, "--backend", "host", "--allow-unsafe-host-execution")
		if fail {
			if err := os.Remove(g.CIFailFlag); err != nil {
				t.Fatal(err)
			}
		}
		g.hn(runner, "sync", "origin")
		ref := g.actorRef(runner)
		if g.hostedRef(ref) != g.localHead(runner, ref) {
			t.Fatal("the runner's audit facts were not published")
		}
		return serveAuditOutcome(t, result)
	}

	head := g.Commit(g.Base, nil)
	p := g.ReadyProposal(g.Base, head)
	if outcome := auditHead(head, "audit the ready candidate", true); outcome != "failed" {
		t.Fatalf("audit of the ready candidate %s, want failed", outcome)
	}
	g.AcceptMain("A", head)
	g.VerifyAccepted(p.Candidate.ID)

	next := g.Commit(head, nil)
	q := g.ReadyProposal(head, next)
	writeServeTestFile(t, g.CIFailFlag, "fail\n", 0o644)
	g.RunCI(q.Request.ID, "--rerun")
	if err := os.Remove(g.CIFailFlag); err != nil {
		t.Fatal(err)
	}
	if outcome := auditHead(next, "audit the candidate whose CI failed", false); outcome != "passed" {
		t.Fatalf("audit of the failed-CI candidate %s, want passed", outcome)
	}
	rejection := g.RejectMain("A", next, serveReasonProtectedNotReady)
	requireServeIDs(t, rejection, q.Candidate.ID)
	requireServeMissing(t, rejection, serveEvidenceCI)
	requireServeSentence(t, rejection, "base policy requires 1 passing result of pipeline test; 0 trusted passing results found")
}
