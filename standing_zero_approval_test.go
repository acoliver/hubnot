package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// Zero-approval standing rules (decision 01M66YE8SNSPCYB1ER757D9251, which
// supersedes OQ-1 and FR-004): with proposals.requiredApprovals 0 a
// standing rule merges on its pipeline gate alone, and every other blocker
// still applies. Every test drives hn and git through the standing
// harnesses.

// standingZeroApprovalPolicy is the default harness policy with no
// required approval. Rule ordinary keeps its reviewers field [R], so the
// counted-reviewer subset is exercised with a threshold of zero.
func standingZeroApprovalPolicy() standingHarnessPolicy {
	policy := standingHarnessDefaultPolicy()
	policy.RequiredApprovals = 0
	return policy
}

// standingZeroApprovalCountedLine is the preview's approval line for a
// zero-approval standing rule with no approval.
const standingZeroApprovalCountedLine = "Counted approvals: 0/0 (none required; author approvals do not count)\n"

// standingZeroApprovalGateCase opens a candidate by A on h.Base() and
// gathers only T's passing result of M's run request: no review and no
// decision.
func standingZeroApprovalGateCase(h *standingHarness, files map[string]string) standingHarnessCase {
	h.t.Helper()
	c := h.openCase("A", h.Base(), files)
	c.Request = h.RequestCI("M", c.Candidate.ID)
	c.Result = h.RunCI(c.Request.ID)
	if outcome := h.fact("T", c.Result.ID).Outcome; outcome != "passed" {
		h.t.Fatalf("run result %s for candidate %s is %q, want passed", c.Result.ID, c.Candidate.ID, outcome)
	}
	h.cases[c.Candidate.ID] = c
	return c
}

// standingZeroApprovalRefusedFact signs a rule merge fact by E for c after
// mutate, publishes it with raw Git, requires a fresh full verifier's hn
// sync to refuse E's chain with check, and restores E's clean chain.
func standingZeroApprovalRefusedFact(t *testing.T, h *standingHarness, c standingHarnessCase, check string, mutate func(*Event)) {
	t.Helper()
	clean := standingFactChainHead(h, "E")
	signed := h.SignFactFor("E", c, mutate)
	standingFactRawPublish(h, "E")
	verifier := h.Verifier()
	output, status := h.RunIn(verifier, "sync", "origin")
	want := "rule merge " + signed.ID + " fails " + check
	if status == 0 || !strings.Contains(output, want) {
		t.Fatalf("hn sync of fact %s exited %d, want a refusal containing %q:\n%s", signed.ID, status, want, output)
	}
	standingFactAssertImportedExcept(t, h, verifier, output, "E")
	standingFactAssertNotMerged(t, h, verifier, c.Candidate.ID)
	standingFactSetChain(h, "E", clean)
	standingFactRawPublish(h, "E")
}

// A zero-approval standing rule merges a candidate whose only evidence is
// a passing pipeline result. E, an executing actor who is not a
// maintainer, executes it; the fact cites the result alone; a shallow
// verifier and a full verifier both accept it. The strict fact check still
// needs the gate: a fact without the result, or citing an approval the rule
// does not count, is refused on import.
func TestStandingZeroApprovalExecuteGateOnly(t *testing.T) {
	h := newStandingHarness(t, standingZeroApprovalPolicy())
	if slices.Contains(h.Policy().Maintainers, "E") {
		t.Fatal("the executing actor must not be a maintainer")
	}

	// The strict fact check under requiredApprovals 0.
	refused := standingZeroApprovalGateCase(h, map[string]string{"src/refused.txt": "refused fact\n"})
	uncounted := h.Approve("S", refused.Candidate.ID)
	standingZeroApprovalRefusedFact(t, h, refused, ruleCheckPipelines, func(event *Event) { event.Evidence = nil })
	standingZeroApprovalRefusedFact(t, h, refused, ruleCheckEvidenceItem, func(event *Event) {
		event.Evidence = sortedUniqueStrings(uncounted.ID, refused.Result.ID)
	})

	// An approval from a counted reviewer is optional and still cited.
	approved := standingZeroApprovalGateCase(h, map[string]string{"docs/approved.md": "approved change\n"})
	h.Approve("R", approved.Candidate.ID)
	output, status := standingAutomergePreview(h, "E", h.Base(), approved.Candidate.ID, "ordinary")
	requireStandingAutomergeEligible(t, output, status)
	assertOperationalContains(t, output, "Counted approvals: 1/0 (none required; author approvals do not count)\n")

	c := standingZeroApprovalGateCase(h, standingAutomergeFiles)
	output, status = standingAutomergePreview(h, "E", h.Base(), c.Candidate.ID, "ordinary")
	requireStandingAutomergeEligible(t, output, status)
	assertOperationalContains(t, output,
		standingAutomergeStandingLine+"Counted reviewers:\n  "+h.Actor("R")+"\n"+standingZeroApprovalCountedLine)

	run := standingAutomergeExecute(h, "E", h.Base(), c.Candidate.ID, "ordinary")
	fact := requireStandingRuleMerge(h, "E", run, c.Candidate, "ordinary", c.Result.ID)
	assertOperationalContains(t, h.hn("E", "proposal", "status", c.Candidate.ID),
		"Status:   merged\n"+standingAutomergeRuleStatusLine("ordinary", h.Actor("E"), fact.ID))

	// The fact reaches the remote before main does, so the depth-one
	// verifier still holds the candidate's base.
	h.Publish("E")
	shallow := standingFactShallowVerifierWith(h, standingFactShallowArrangement{})
	standingFactAssertVerifierMerged(t, h, shallow, c.Candidate.ID, fact.ID)

	h.git("E", "push", "-q", "origin", "main")
	verifier := h.Verifier()
	output = h.hnIn(verifier, "sync", "origin")
	standingFactAssertImportedExcept(t, h, verifier, output)
	statusOutput, code := h.RunIn(verifier, "proposal", "status", c.Candidate.ID)
	if code != 0 {
		t.Fatalf("verifier status exited %d:\n%s", code, statusOutput)
	}
	assertOperationalContains(t, statusOutput, "Status:   merged\n"+standingAutomergeRuleStatusLine("ordinary", h.Actor("E"), fact.ID))
	for _, event := range h.AllEvents("E") {
		if event.Subject == c.Candidate.ID && (event.Kind == "review.submit" || event.Kind == "proposal.decision") {
			t.Fatalf("%s %s exists for the gate-only candidate %s", event.Kind, event.ID, c.Candidate.ID)
		}
	}
}

// Under requiredApprovals 0 every other blocker of a standing rule still
// applies: a trusted change request, a maintainer rejection, a missing or
// failing pipeline result, the author and path limits, a policy amendment
// and an actor outside the rule.
func TestStandingZeroApprovalBlockers(t *testing.T) {
	h := newStandingHarness(t, standingZeroApprovalPolicy())
	preview := func(candidate string) (string, int) {
		t.Helper()
		return standingAutomergePreview(h, "E", h.Base(), candidate, "ordinary")
	}
	// requireRefusedExecution requires role's execution to fail with want
	// and to change neither main nor role's chain.
	requireRefusedExecution := func(role, candidate, want string) {
		t.Helper()
		run := standingAutomergeExecute(h, role, h.Base(), candidate, "ordinary")
		if run.Status == 0 || !strings.Contains(run.Output, want) || run.After != run.Before || len(run.Appended) != 0 {
			t.Fatalf("%s's execution of %s exited %d, moved main %s -> %s and appended %d event(s); want a refusal containing %q:\n%s",
				role, candidate, run.Status, run.Before, run.After, len(run.Appended), want, run.Output)
		}
	}

	// A trusted change request, even from S, whom the rule does not count.
	c := standingZeroApprovalGateCase(h, nil)
	h.RequestChanges("S", c.Candidate.ID)
	output, status := preview(c.Candidate.ID)
	requireStandingAutomergeBlocked(t, output, status, standingAutomergeChangeRequest)
	requireRefusedExecution("E", c.Candidate.ID, standingAutomergeChangeRequest)

	// A current maintainer rejection.
	c = standingZeroApprovalGateCase(h, nil)
	h.Reject("M", c.Candidate.ID, "Rejected by the zero-approval test.")
	output, status = preview(c.Candidate.ID)
	requireStandingAutomergeBlocked(t, output, status, "proposal has a current maintainer rejection")
	requireRefusedExecution("E", c.Candidate.ID, "proposal has a current maintainer rejection")

	// No pipeline result at all.
	missing := h.openCase("A", h.Base(), nil)
	output, status = preview(missing.Candidate.ID)
	requireStandingAutomergeBlocked(t, output, status, standingAutomergeNoPass)
	assertOperationalContains(t, output, standingZeroApprovalCountedLine)
	requireRefusedExecution("E", missing.Candidate.ID, standingAutomergeNoPass)

	// A requested run that fails.
	failing := h.openCase("A", h.Base(), nil)
	h.FailCI()
	h.RunCI(h.RequestCI("M", failing.Candidate.ID).ID)
	h.PassCI()
	output, status = preview(failing.Candidate.ID)
	requireStandingAutomergeBlocked(t, output, status, standingAutomergeNoPass, `pipeline "test" has a current trusted failure`)
	requireRefusedExecution("E", failing.Candidate.ID, `pipeline "test" has a current trusted failure`)

	// A candidate by an author outside the rule.
	c = h.openCase("M", h.Base(), nil)
	h.RunCI(h.RequestCI("M", c.Candidate.ID).ID)
	output, status = preview(c.Candidate.ID)
	requireStandingAutomergeBlocked(t, output, status, "proposal author is not allowed by the rule")

	// A changed path outside the rule.
	c = standingZeroApprovalGateCase(h, map[string]string{"other/x": "outside the rule\n"})
	output, status = preview(c.Candidate.ID)
	requireStandingAutomergeBlocked(t, output, status, `changed path "other/x" is outside the rule allowlist`)

	// A policy amendment takes the manual path.
	amended := standingZeroApprovalPolicy()
	amended.RequiredApprovals = 1
	c = standingZeroApprovalGateCase(h, map[string]string{serveGovernedPolicyPath: amended.JSON(h.actors)})
	output, status = preview(c.Candidate.ID)
	requireStandingAutomergeBlockedBy(t, output, status, "policy amendments require manual merge")

	// X executes rule other only, so an eligible candidate stays unmerged.
	c = standingZeroApprovalGateCase(h, nil)
	output, status = preview(c.Candidate.ID)
	requireStandingAutomergeEligible(t, output, status)
	h.hn("X", "sync", "origin")
	requireRefusedExecution("X", c.Candidate.ID, "identity "+h.Actor("X")+` is not an executing actor for automerge rule "ordinary"`)
}

// standingZeroApprovalServePolicy is the standing serve policy with no
// required approval.
func standingZeroApprovalServePolicy(actors map[string]string) string {
	policy := standingServePolicyDocument()
	policy.RequiredApprovals = 0
	return policy.JSON(actors)
}

// standingZeroApprovalServeGate opens a candidate on the hosted base and
// gathers only U's passing result of M's run request.
func standingZeroApprovalServeGate(g *serveGovernedFixture, files map[string]string) serveGovernedProposal {
	g.t.Helper()
	var p serveGovernedProposal
	p.Candidate = g.OpenProposal(g.Base, g.Commit(g.Base, files))
	p.Request = g.RequestCI(p.Candidate.ID)
	p.Result = g.RunCI(p.Request.ID)
	return p
}

// hn serve under a zero-approval standing rule: the server refuses a rule
// advancement for a trusted change request, a missing pipeline result and
// a maintainer rejection; E's hn automerge --execute --remote advances main
// for a candidate with only a passing result; the server accepts E's chain
// with the rule merge fact; and a fresh clone reports the rule merge.
func TestStandingZeroApprovalServeAdvance(t *testing.T) {
	g := newStandingServeFixture(t, standingZeroApprovalServePolicy)
	standingServeSelectAllTrusted(g, "E")

	vetoed := standingZeroApprovalServeGate(g, map[string]string{"src/vetoed.txt": "vetoed change\n"})
	change := g.act("S", "review.submit", "review", vetoed.Candidate.ID, "--request-changes", "--body", "Changes requested under a zero-approval rule.")
	untested := g.OpenProposal(g.Base, g.Commit(g.Base, map[string]string{"src/untested.txt": "no CI\n"}))
	rejected := standingZeroApprovalServeGate(g, map[string]string{"src/rejected.txt": "rejected change\n"})
	decision := g.Reject(rejected.Candidate.ID)
	standingServeImport(g, "E")

	veto, _ := standingServeRuleRefused(t, g, "E", vetoed.Candidate.ID, vetoed.Candidate.Head, "ordinary", serveReasonProtectedRuleVeto)
	requireServeIDs(t, veto, vetoed.Candidate.ID, change.ID)
	notSatisfied, _ := standingServeRuleRefused(t, g, "E", untested.ID, untested.Head, "ordinary", serveReasonProtectedRuleNotSatisfied)
	standingServeNotSatisfied(t, notSatisfied, untested.ID, "ordinary", []string{serveEvidenceCI}, standingAutomergeNoPass)
	blocked, _ := standingServeRuleRefused(t, g, "E", rejected.Candidate.ID, rejected.Candidate.Head, "ordinary", serveReasonProtectedBlocked)
	requireServeIDs(t, blocked, rejected.Candidate.ID, decision.ID)

	p := standingZeroApprovalServeGate(g, map[string]string{"src/gate.txt": "merged on the gate alone\n"})
	standingServeImport(g, "E")
	output, status := standingServeAutomerge(g, "E", p.Candidate.ID, "ordinary")
	if status != 0 {
		t.Fatalf("hn automerge --execute --remote origin exited %d:\n%s", status, output)
	}
	facts := standingServeChainFacts(g, "E", p.Candidate.ID)
	if len(facts) != 1 {
		t.Fatalf("E's chain holds %d merge facts for %s, want 1: %+v", len(facts), p.Candidate.ID, facts)
	}
	fact := facts[0]
	if fact.Rule != "ordinary" || fact.Actor != g.actors["E"] || fact.Commit != p.Candidate.Head || !slices.Equal(fact.Evidence, []string{p.Result.ID}) {
		t.Fatalf("merge fact %+v, want rule ordinary by E at %s citing only %s", fact, p.Candidate.Head, p.Result.ID)
	}
	if want := standingServeAdvanceLines(p.Candidate.Head, p.Candidate.ID, fact.ID, 1); !strings.HasSuffix(output, want) {
		t.Fatalf("output:\n%s\nwant it to end with:\n%s", output, want)
	}
	if tip := g.ServerTip("main"); tip != p.Candidate.Head {
		t.Fatalf("hosted main = %s, want the candidate head %s", tip, p.Candidate.Head)
	}
	if !g.Published("E") {
		t.Fatal("the server does not hold E's chain with the rule merge fact")
	}
	verified := standingServeAllTrustedStatus(g, "zero-approval", p.Candidate.ID)
	assertOperationalContains(t, verified, "Status:   merged\n", standingAutomergeRuleStatusLine("ordinary", g.actors["E"], fact.ID))
}

// standingZeroApprovalOlderClient is a subtest of
// TestStandingCompatOlderClient, the only owner of the older client: a
// zero-approval rule merge, valid only through path (b), is refused by the
// older client and accepted by the newer one.
func standingZeroApprovalOlderClient(t *testing.T, older, newer string, report *standingCompatReport) {
	h := newStandingHarnessWithBinary(t, newer, standingZeroApprovalPolicy())
	c := standingZeroApprovalGateCase(h, standingAutomergeFiles)
	h.hn("E", "sync", "origin")
	run := standingAutomergeExecute(h, "E", h.Base(), c.Candidate.ID, "ordinary")
	fact := requireStandingRuleMerge(h, "E", run, c.Candidate, "ordinary", c.Result.ID)
	h.git("E", "push", "-q", "origin", "main")
	h.Publish("E")
	factCommit := h.git("E", "rev-parse", operationalActorRef(h.Actor("E")))

	oldVerifier := standingCompatVerifier(h, older, "older-verifier-zero")
	syncOutput, syncStatus := standingCompatRun(h, older, oldVerifier, "sync", "origin")
	statusOutput, statusCode, merged := standingCompatStatus(h, older, oldVerifier, c.Candidate.ID)
	t.Logf("zero-approval rule merge %s, older sync exited %d:\n%s\nolder status exited %d:\n%s", fact.ID, syncStatus, syncOutput, statusCode, statusOutput)
	if syncStatus == 0 || standingCompatPromoted(h, oldVerifier, "E", factCommit) || merged {
		t.Fatalf("older verifier admitted zero-approval rule merge %s: sync exited %d, merged=%v\n%s\n%s", fact.ID, syncStatus, merged, syncOutput, statusOutput)
	}
	newVerifier := standingCompatVerifier(h, newer, "newer-verifier-zero")
	newSync, newSyncStatus := standingCompatRun(h, newer, newVerifier, "sync", "origin")
	newStatus, newCode, newMerged := standingCompatStatus(h, newer, newVerifier, c.Candidate.ID)
	if newSyncStatus != 0 || !newMerged || !strings.Contains(newStatus, standingAutomergeRuleStatusLine("ordinary", h.Actor("E"), fact.ID)) {
		t.Fatalf("newer verifier: sync exited %d, status exited %d, want the rule merge:\n%s\n%s", newSyncStatus, newCode, newSync, newStatus)
	}
	report.add("zero", "zero-approval rule merge citing one run result",
		fmt.Sprintf("refused (sync exit %d, status exit %d, not merged)", syncStatus, statusCode), "accepted (merged by rule ordinary)")
}
