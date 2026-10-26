package main

import (
	"strings"
	"testing"
)

// Opt-in protected checks hold for a push through hn serve as they do for a
// local hn merge. A candidate that changes a protected path needs an
// approval from a protected-check reviewer before the server advances main,
// and a stale candidate cannot carry its approval past a newer protected
// baseline on the destination: it must be revised against the current tip.
func TestServeProtectedChecksEnforcedOnPush(t *testing.T) {
	g := newServeGovernedFixture(t, serveGovernedOptions{ProtectedPaths: []string{"lint.conf"}})
	guardianApprove := func(candidate string) operationalEvent {
		t.Helper()
		return g.act(g.Maintainer, "review.submit", "review", candidate, "--approve", "--body", "Protected check change approved.")
	}

	// A change outside the protected paths needs only the ordinary evidence.
	plain := g.Commit(g.Base, nil)
	g.ReadyProposal(g.Base, plain)
	g.AcceptMain("A", plain)

	// A lint.conf change with ordinary approval and passing CI, but no
	// protected-check approval and therefore no acceptance yet, is refused.
	lint := g.Commit(plain, map[string]string{"lint.conf": "max-complexity = 99\n"})
	candidate := g.OpenProposal(plain, lint)
	g.Approve(candidate.ID)
	request := g.RequestCI(candidate.ID)
	g.RunCI(request.ID)
	rejection := g.RejectMain("A", lint, serveReasonProtectedNotReady)
	requireServeIDs(t, rejection, candidate.ID)
	requireServeMissing(t, rejection, serveEvidenceReview, serveEvidenceAcceptance)
	requireServeSentence(t, rejection, "from protected-check reviewers for lint.conf")

	// The protected-check reviewer approves, the maintainer accepts, and the
	// server advances main.
	guardianApprove(candidate.ID)
	g.Accept(candidate.ID)
	g.AcceptMain("A", lint)
	g.VerifyAccepted(candidate.ID)

	// A candidate opened on the older base, before the lint.conf change
	// landed, whose head fast-forwards the new tip. Every approval is
	// present, yet the protected baseline changed between its base and the
	// tip, so the server refuses it.
	stale := g.Commit(lint, nil)
	old := g.OpenProposal(plain, stale)
	g.Approve(old.ID)
	guardianApprove(old.ID)
	oldRequest := g.RequestCI(old.ID)
	g.RunCI(oldRequest.ID)
	g.Accept(old.ID)
	rejection = g.RejectMain("A", stale, serveReasonProtectedDestinationPolicy)
	requireServeIDs(t, rejection, old.ID)
	requireServeSentence(t, rejection, "target protected check baseline changed")
	requireServeNext(t, rejection, "revise "+old.ID+" against "+serveShortCommit(lint))
	if tip := g.ServerTip("main"); tip != lint {
		t.Fatalf("hosted main = %s after the refused push, want %s", tip, lint)
	}

	// hn merge --remote asks the server to advance with the same verdict, so
	// the maintainer cannot advance the stale candidate either.
	output, status := g.Advance(g.Maintainer, old.ID)
	if status == 0 || !strings.Contains(output, "target protected check baseline changed") {
		t.Fatalf("hn merge --remote of the stale candidate exited %d:\n%s", status, output)
	}
	if tip := g.ServerTip("main"); tip != lint {
		t.Fatalf("hosted main = %s after the refused advancement, want %s", tip, lint)
	}

	// Revised against the current tip, the candidate no longer changes a
	// protected path and lands with the ordinary evidence.
	revised := g.Revise(old.ID, lint, stale)
	g.Approve(revised.ID)
	revisedRequest := g.RequestCI(revised.ID)
	g.RunCI(revisedRequest.ID)
	g.Accept(revised.ID)
	g.AcceptMain("A", stale)
	g.VerifyAccepted(revised.ID)
}
