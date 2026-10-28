package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Keyless automerge preview under a standing rule (WP10), driven through
// the standing harness. Every preview runs in a clone whose main is checked
// out at the candidate's target, and must change no ref and append no
// event there.

// standingAutomergeFiles are the changes of the User Story 1 candidate.
var standingAutomergeFiles = map[string]string{
	"src/feature.txt":   "standing feature\n",
	"tests/feature.txt": "standing feature test\n",
}

const (
	standingAutomergeNotReady       = "current approval, protected check or CI evidence is not ready"
	standingAutomergeNotAccepted    = "current required maintainer acceptances are missing"
	standingAutomergeThreshold0of1  = "rule approval threshold is unmet: 0 of 1 counted approvals"
	standingAutomergeNoPass         = `pipeline "test" has 0 of 1 passing trusted results`
	standingAutomergeChangeRequest  = "proposal has a current trusted change request"
	standingAutomergeStandingLine   = "Acceptance: rule (standing acceptance; no maintainer acceptance required)\n"
	standingAutomergeMaintainerLine = "Acceptance: maintainers (maintainer acceptance required)\n"
)

// standingAutomergeLeftoverPlaceholder finds a documented placeholder that
// the documented-preview test does not substitute.
var standingAutomergeLeftoverPlaceholder = regexp.MustCompile(`<[a-z]+(-[a-z]+)*>`)

// standingAutomergePreview syncs role's clone, checks out its main at
// target and runs the keyless preview of candidate under rule. It fails the
// test when the preview changes a ref or appends an event in role's clone.
func standingAutomergePreview(h *standingHarness, role, target, candidate, rule string) (string, int) {
	h.t.Helper()
	h.hn(role, "sync", "origin")
	h.Checkout(role, "main", target)
	refs, events := h.Refs(role), len(h.Events(role))
	output, status := h.Run(role, "automerge", candidate, "--rule", rule)
	if after := h.Refs(role); after != refs {
		h.t.Fatalf("preview of %s changed refs in %s's clone:\nbefore:\n%s\nafter:\n%s", candidate, role, refs, after)
	}
	if after := len(h.Events(role)); after != events {
		h.t.Fatalf("preview of %s appended %d event(s) to %s's chain", candidate, after-events, role)
	}
	return output, status
}

// standingAutomergeBlockers returns the lines of the preview's Blocked:
// block, or nil when the preview has none.
func standingAutomergeBlockers(output string) []string {
	_, rest, found := strings.Cut(output, "\nBlocked:\n")
	if !found {
		return nil
	}
	var blockers []string
	for _, line := range strings.Split(rest, "\n") {
		if !strings.HasPrefix(line, "  ") {
			break
		}
		blockers = append(blockers, strings.TrimPrefix(line, "  "))
	}
	return blockers
}

// requireStandingAutomergeBlocked requires a failing preview whose blockers
// are exactly want, in order.
func requireStandingAutomergeBlocked(t *testing.T, output string, status int, want ...string) {
	t.Helper()
	got := standingAutomergeBlockers(output)
	if status == 0 || strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("preview exited %d with blockers\n%s\nwant\n%s\noutput:\n%s", status, strings.Join(got, "\n"), strings.Join(want, "\n"), output)
	}
}

// requireStandingAutomergeBlockedBy requires a failing preview whose
// blockers include every one of want.
func requireStandingAutomergeBlockedBy(t *testing.T, output string, status int, want ...string) {
	t.Helper()
	got := standingAutomergeBlockers(output)
	if status == 0 {
		t.Fatalf("preview succeeded, want blockers %q:\n%s", want, output)
	}
	for _, blocker := range want {
		if !stringInSlice(blocker, got) {
			t.Fatalf("preview blockers\n%s\nomit %q:\n%s", strings.Join(got, "\n"), blocker, output)
		}
	}
}

// requireStandingAutomergeEligible requires a successful preview with no
// Blocked: block.
func requireStandingAutomergeEligible(t *testing.T, output string, status int) {
	t.Helper()
	if status != 0 || strings.Contains(output, "Blocked:") || !strings.Contains(output, "\nEligible: explicit --execute is required to merge locally.\n") {
		t.Fatalf("preview exited %d, want eligible:\n%s", status, output)
	}
}

// standingAutomergeMaintainersVariant is the default policy with rule
// ordinary in acceptance mode maintainers (the field absent) and M as its
// only executing actor; its reviewers stay [R].
func standingAutomergeMaintainersVariant() standingHarnessPolicy {
	policy := standingHarnessDefaultPolicy()
	policy.Rules[0].Acceptance = nil
	policy.Rules[0].Actors = []string{"M"}
	return policy
}

// US1-4 and US1-3: the standing rule makes a ready candidate eligible with
// no decision; the same evidence under acceptance mode maintainers still
// needs the maintainer acceptance.
func TestStandingAutomergePreviewStandingRule(t *testing.T) {
	h := newStandingHarness(t, standingHarnessDefaultPolicy())

	candidate := h.ReadyCandidate(standingAutomergeFiles)
	output, status := standingAutomergePreview(h, "E", h.Base(), candidate.ID, "ordinary")
	requireStandingAutomergeEligible(t, output, status)
	assertOperationalContains(t, output,
		"Rule: ordinary\nPermitted executing actors:\n  "+h.Actor("E")+"\n"+
			standingAutomergeStandingLine+
			"Counted reviewers:\n  "+h.Actor("R")+"\n"+
			"Counted approvals: 1/1 (author approvals do not count)\n"+
			"Changed paths:\n  \"src/feature.txt\"\n  \"tests/feature.txt\"\n")

	variant := h.PolicyCommit(h.Base(), standingAutomergeMaintainersVariant())
	c := h.readyCase("A", variant, standingAutomergeFiles)
	output, status = standingAutomergePreview(h, "M", variant, c.Candidate.ID, "ordinary")
	requireStandingAutomergeBlocked(t, output, status, standingAutomergeNotAccepted)
	assertOperationalContains(t, output,
		"Permitted executing actors:\n  "+h.Actor("M")+"\n"+
			standingAutomergeMaintainerLine+
			"Counted reviewers:\n  "+h.Actor("R")+"\n"+
			"Counted approvals: 1/1\nChanged paths:\n")
}

// US5-1 to US5-4: only the rule's reviewers count, a change request from
// any trusted reviewer blocks, the author never counts under acceptance
// mode rule, and a maintainers-mode rule with reviewers checks its
// threshold as well.
func TestStandingAutomergePreviewRuleReviewers(t *testing.T) {
	h := newStandingHarness(t, standingHarnessDefaultPolicy())

	// US5-1: S is trusted but not counted; R's approval makes it eligible.
	c := h.openCase("A", h.Base(), nil)
	c.Approval = h.Approve("S", c.Candidate.ID)
	h.completeCase(&c)
	output, status := standingAutomergePreview(h, "E", h.Base(), c.Candidate.ID, "ordinary")
	requireStandingAutomergeBlocked(t, output, status, standingAutomergeThreshold0of1)
	assertOperationalContains(t, output, "Counted approvals: 0/1 (author approvals do not count)\n")
	h.Approve("R", c.Candidate.ID)
	output, status = standingAutomergePreview(h, "E", h.Base(), c.Candidate.ID, "ordinary")
	requireStandingAutomergeEligible(t, output, status)

	// US5-2: S's change request blocks although S's approvals never count.
	c = h.readyCase("A", h.Base(), nil)
	h.RequestChanges("S", c.Candidate.ID)
	output, status = standingAutomergePreview(h, "E", h.Base(), c.Candidate.ID, "ordinary")
	requireStandingAutomergeBlocked(t, output, status, standingAutomergeChangeRequest)

	// US5-3: allowAuthorApproval does not let the author's approval count.
	selfApproval := standingHarnessDefaultPolicy()
	selfApproval.AllowAuthorApproval = true
	selfApproval.TrustedReviewers = []string{"R", "S", "A"}
	selfApproval.Rules[0].Reviewers = []string{"R", "A"}
	base := h.PolicyCommit(h.Base(), selfApproval)
	c = h.openCase("A", base, nil)
	c.Approval = h.Approve("A", c.Candidate.ID)
	h.completeCase(&c)
	output, status = standingAutomergePreview(h, "E", base, c.Candidate.ID, "ordinary")
	requireStandingAutomergeBlocked(t, output, status, standingAutomergeThreshold0of1)

	// US5-4: under acceptance mode maintainers, M's acceptance and S's
	// approval satisfy today's checks; the rule threshold still blocks.
	base = h.PolicyCommit(h.Base(), standingAutomergeMaintainersVariant())
	c = h.openCase("A", base, nil)
	c.Approval = h.Approve("S", c.Candidate.ID)
	h.completeCase(&c)
	h.Accept("M", c.Candidate.ID)
	output, status = standingAutomergePreview(h, "M", base, c.Candidate.ID, "ordinary")
	requireStandingAutomergeBlocked(t, output, status, standingAutomergeThreshold0of1)
	assertOperationalContains(t, output, standingAutomergeMaintainerLine, "Counted approvals: 0/1\n")
}

// US6-1 to US6-3 and US6-7: vetoes and every other blocker still apply
// under acceptance mode rule, and a result counts only when it answers a
// request from a maintainer or a listed requester.
//
// The harness fails through the test it was built with, so these cases run
// in sequence rather than as subtests.
func TestStandingAutomergePreviewVetoesAndBlockers(t *testing.T) {
	h := newStandingHarness(t, standingHarnessDefaultPolicy())
	preview := func(candidate string) (string, int) {
		t.Helper()
		return standingAutomergePreview(h, "E", h.Base(), candidate, "ordinary")
	}

	// US6-1: a current maintainer rejection.
	candidate := h.ReadyCandidate(nil)
	h.Reject("M", candidate.ID, "Rejected by the standing preview test.")
	output, status := preview(candidate.ID)
	requireStandingAutomergeBlocked(t, output, status, "proposal has a current maintainer rejection")

	// US6-2: a counted reviewer's change request replaces its approval.
	candidate = h.ReadyCandidate(nil)
	h.RequestChanges("R", candidate.ID)
	output, status = preview(candidate.ID)
	requireStandingAutomergeBlocked(t, output, status, standingAutomergeThreshold0of1, standingAutomergeChangeRequest)

	// US6-3: the trusted runner's rerun fails.
	candidate = h.ReadyCandidate(nil)
	h.FailCI()
	h.act("T", "run.result", true, "run", "execute", h.Case(candidate.ID).Request.ID,
		"--backend", "host", "--allow-unsafe-host-execution", "--rerun")
	h.PassCI()
	output, status = preview(candidate.ID)
	requireStandingAutomergeBlocked(t, output, status, standingAutomergeNoPass, `pipeline "test" has a current trusted failure`)

	// A required pipeline without a passing result.
	c := h.openCase("A", h.Base(), nil)
	h.Approve("R", c.Candidate.ID)
	output, status = preview(c.Candidate.ID)
	requireStandingAutomergeBlocked(t, output, status, standingAutomergeNoPass)

	// An author outside the rule.
	c = h.readyCase("M", h.Base(), nil)
	output, status = preview(c.Candidate.ID)
	requireStandingAutomergeBlocked(t, output, status, "proposal author is not allowed by the rule")

	// A changed path outside the rule.
	candidate = h.ReadyCandidate(map[string]string{"other/x": "outside the rule\n"})
	output, status = preview(candidate.ID)
	requireStandingAutomergeBlocked(t, output, status, `changed path "other/x" is outside the rule allowlist`)

	// US6-7: A is neither a maintainer nor a requester of pipeline test, so
	// the result of A's request does not count until policy lists A.
	c = h.openCase("A", h.Base(), nil)
	h.Approve("R", c.Candidate.ID)
	h.RunCI(h.RequestCI("A", c.Candidate.ID).ID)
	output, status = preview(c.Candidate.ID)
	requireStandingAutomergeBlocked(t, output, status, standingAutomergeNoPass)

	listed := standingHarnessDefaultPolicy()
	listed.Requesters = []string{"A"}
	base := h.PolicyCommit(h.Base(), listed)
	c = h.openCase("A", base, nil)
	h.Approve("R", c.Candidate.ID)
	h.RunCI(h.RequestCI("A", c.Candidate.ID).ID)
	output, status = standingAutomergePreview(h, "E", base, c.Candidate.ID, "ordinary")
	requireStandingAutomergeEligible(t, output, status)
}

// US2-3 and FR-017 (local): a candidate cannot add the rule it is previewed
// under, and a target whose policy moved on since the candidate's base
// blocks.
func TestStandingAutomergePreviewPolicyChanges(t *testing.T) {
	h := newStandingHarness(t, standingHarnessDefaultPolicy())

	amended := standingHarnessDefaultPolicy()
	amended.Rules = append(amended.Rules, standingHarnessRule{
		Name: "newrule", TargetBranch: "main", Acceptance: standingHarnessString(automergeAcceptanceRule),
		Actors: []string{"E"}, Authors: []string{"A"}, Paths: []string{"lib/"},
	})
	candidate := h.ReadyCandidate(map[string]string{serveGovernedPolicyPath: amended.JSON(h.actors)})
	output, status := standingAutomergePreview(h, "E", h.Base(), candidate.ID, "newrule")
	requireStandingAutomergeBlockedBy(t, output, status,
		`automerge rule "newrule" does not exist in the signed base policy`, "policy amendments require manual merge")

	ready := h.ReadyCandidate(standingAutomergeFiles)
	moved := standingHarnessDefaultPolicy()
	moved.Rules[0].Paths = []string{"src/", "tests/", "docs/", "lib/"}
	h.AdoptThroughAmendment(moved)
	output, status = standingAutomergePreview(h, "E", h.Base(), ready.ID, "ordinary")
	requireStandingAutomergeBlockedBy(t, output, status,
		"target no longer equals the signed base; revise the candidate (use hn merge for a missing merge-event repair)",
		"current target policy differs from the signed base policy")
}

// A maintainers-mode rule without reviewers keeps today's blockers and
// gains only the three additive preview lines (NFR-004).
func TestStandingAutomergePreviewMaintainersRuleOptOut(t *testing.T) {
	h := newStandingHarness(t, standingHarnessDefaultPolicy())
	base := h.PolicyCommit(h.Base(), standingHarnessLegacyPolicy())
	header := func(c standingHarnessCase) string {
		return "Proposal: " + c.Candidate.ID + "\nBase: " + base + "\nHead: " + c.Candidate.Head +
			"\nPolicy: " + h.PolicyDigest(base) + "\nTarget: main at " + base +
			"\nRule: maint\nPermitted executing actors:\n  " + h.Actor("M") + "\n" +
			standingAutomergeMaintainerLine + "Counted reviewers: all trusted reviewers\n"
	}

	ready := h.readyCase("A", base, nil)
	output, status := standingAutomergePreview(h, "E", base, ready.Candidate.ID, "maint")
	requireStandingAutomergeBlocked(t, output, status, standingAutomergeNotAccepted)
	assertOperationalContains(t, output, header(ready)+"Counted approvals: 1/1\nChanged paths:\n")

	unapproved := h.openCase("A", base, nil)
	h.RunCI(h.RequestCI("M", unapproved.Candidate.ID).ID)
	output, status = standingAutomergePreview(h, "E", base, unapproved.Candidate.ID, "maint")
	requireStandingAutomergeBlocked(t, output, status, standingAutomergeNotReady, standingAutomergeNotAccepted)
	assertOperationalContains(t, output, header(unapproved)+"Counted approvals: 0/1\nChanged paths:\n")

	h.Accept("M", ready.Candidate.ID)
	output, status = standingAutomergePreview(h, "E", base, ready.Candidate.ID, "maint")
	requireStandingAutomergeEligible(t, output, status)
	assertOperationalContains(t, output, header(ready)+"Counted approvals: 1/1\nChanged paths:\n")
}

// The guide's standing-preview example runs on the User Story 1 fixture:
// every documented line appears in the preview, in the documented order.
func TestStandingAutomergeDocumentedPreview(t *testing.T) {
	const marker = "<!-- example: standing-preview -->"
	blocks := standingPolicyGuideBlocks(t, marker)
	if len(blocks) != 1 {
		t.Fatalf("%s is followed by %d blocks, want one", marker, len(blocks))
	}
	h := newStandingHarness(t, standingHarnessDefaultPolicy())
	candidate := h.ReadyCandidate(standingAutomergeFiles)
	documented := strings.NewReplacer(
		"<candidate-id>", candidate.ID,
		"<base-commit>", h.Base(),
		"<head-commit>", candidate.Head,
		"<policy-digest>", h.PolicyDigest(h.Base()),
		"<executor-actor>", h.Actor("E"),
		"<reviewer-actor>", h.Actor("R"),
	).Replace(blocks[0])
	if leftover := standingAutomergeLeftoverPlaceholder.FindString(documented); leftover != "" {
		t.Fatalf("documented preview uses unknown placeholder %s", leftover)
	}
	commandLine, expected, _ := strings.Cut(documented, "\n")
	if !strings.HasPrefix(commandLine, "$ hn automerge ") {
		t.Fatalf("documented preview does not start with an hn automerge command: %q", commandLine)
	}
	args := strings.Fields(strings.TrimPrefix(commandLine, "$ hn "))
	if len(args) != 4 || args[1] != candidate.ID || args[2] != "--rule" {
		t.Fatalf("documented preview command is %q, want hn automerge <candidate-id> --rule NAME", commandLine)
	}
	output, status := standingAutomergePreview(h, "E", h.Base(), candidate.ID, args[3])
	requireStandingAutomergeEligible(t, output, status)
	lines := strings.Split(output, "\n")
	next := 0
	for _, want := range strings.Split(strings.TrimSuffix(expected, "\n"), "\n") {
		for next < len(lines) && lines[next] != want {
			next++
		}
		if next == len(lines) {
			t.Fatalf("preview omits documented line %q in the documented order:\n%s", want, output)
		}
		next++
	}
}

// Local execution under a standing rule (WP11). Unlike the preview, an
// execution may change refs and append events, so these tests check exactly
// which ones change.

// standingAutomergeExecution is what one hn automerge --execute did in a
// role's clone.
type standingAutomergeExecution struct {
	Output string
	Status int
	Before string // main before the command
	After  string // main after the command
	// Appended is every event the command appended to the role's chain.
	Appended []operationalEvent
}

// standingAutomergeExecute checks out role's main at target with a clean
// worktree and runs hn automerge candidate --rule rule --execute. It neither
// syncs nor publishes, so it can follow a preview in the same clone with
// nothing in between.
func standingAutomergeExecute(h *standingHarness, role, target, candidate, rule string) standingAutomergeExecution {
	h.t.Helper()
	if main := h.git(role, "rev-parse", "main"); main != target {
		h.Checkout(role, "main", target)
	}
	before := len(h.Events(role))
	output, status := h.Run(role, "automerge", candidate, "--rule", rule, "--execute")
	events := h.Events(role)
	return standingAutomergeExecution{
		Output: output, Status: status, Before: target,
		After: h.git(role, "rev-parse", "main"), Appended: events[before:],
	}
}

// requireStandingRuleMerge requires an execution that advanced main to a
// native two-parent merge of the candidate with the head's tree and
// appended exactly one rule merge fact under rule for it, citing exactly
// evidence. It returns the fact.
func requireStandingRuleMerge(h *standingHarness, role string, run standingAutomergeExecution, candidate operationalEvent, rule string, evidence ...string) standingHarnessFact {
	h.t.Helper()
	t := h.t
	if run.Status != 0 {
		t.Fatalf("%s's execution of %s exited %d:\n%s", role, candidate.ID, run.Status, run.Output)
	}
	parents := strings.Fields(h.git(role, "rev-list", "--parents", "-n", "1", run.After))
	if len(parents) != 3 || parents[1] != run.Before || parents[2] != candidate.Head {
		t.Fatalf("main advanced to %v, want a merge commit with parents %s and %s", parents, run.Before, candidate.Head)
	}
	if tree, want := h.git(role, "rev-parse", run.After+"^{tree}"), h.git(role, "rev-parse", candidate.Head+"^{tree}"); tree != want {
		t.Fatalf("merge commit tree %s, want the head's tree %s", tree, want)
	}
	if len(run.Appended) != 1 || run.Appended[0].Kind != "proposal.merged" {
		t.Fatalf("%s's chain gained %#v, want exactly one proposal.merged", role, run.Appended)
	}
	fact := h.fact(role, run.Appended[0].ID)
	sorted := append([]string(nil), evidence...)
	sort.Strings(sorted)
	if fact.Rule != rule || fact.Subject != candidate.ID || fact.Head != candidate.Head || fact.Commit != run.After ||
		fact.Policy != h.PolicyDigest(candidate.Base) || fact.Actor != h.Actor(role) ||
		strings.Join(fact.Evidence, ",") != strings.Join(sorted, ",") {
		t.Fatalf("merge fact %+v, want rule %q by %s at %s under the base policy citing %v", fact, rule, role, run.After, sorted)
	}
	assertOperationalContains(t, run.Output,
		fmt.Sprintf("Merged proposal %s into main at %s\n", operationalShortID(candidate.ID), shortOID(run.After)),
		fmt.Sprintf("Recorded rule merge event %s under rule %s with %d evidence item(s)\n", operationalShortID(fact.ID), rule, len(sorted)))
	return fact
}

// standingAutomergeRuleStatusLine is the hn proposal status line of a rule
// merge.
func standingAutomergeRuleStatusLine(rule, signer, fact string) string {
	return fmt.Sprintf("Merged by rule %s, signed by %s in fact %s\n", rule, signer, fact)
}

// US1-1, US1-2 and US1-3 on the User Story 1 candidate, with the pre-sign
// self-check proven in-process before E executes.
func TestStandingAutomergeExecuteUserStory1(t *testing.T) {
	h := newStandingHarness(t, standingHarnessDefaultPolicy())
	candidate := h.ReadyCandidate(standingAutomergeFiles)
	c := h.Case(candidate.ID)

	// US1-3: the same evidence under a rule without acceptance, executed
	// by M, still needs the maintainer acceptance; nothing changes.
	variant := h.PolicyCommit(h.Base(), standingAutomergeMaintainersVariant())
	maintainers := h.readyCase("A", variant, standingAutomergeFiles)
	h.hn("M", "sync", "origin")
	h.Checkout("M", "main", variant)
	refs, remote := h.Refs("M"), h.RemoteRefs()
	chains := len(h.AllEvents("M"))
	refused := standingAutomergeExecute(h, "M", variant, maintainers.Candidate.ID, "ordinary")
	if refused.Status == 0 || !strings.Contains(refused.Output, "automerge blocked:") || !strings.Contains(refused.Output, standingAutomergeNotAccepted) {
		t.Fatalf("maintainers-mode execution exited %d, want automerge blocked: %s:\n%s", refused.Status, standingAutomergeNotAccepted, refused.Output)
	}
	if after := h.Refs("M"); after != refs {
		t.Fatalf("refused execution changed M's refs:\nbefore:\n%s\nafter:\n%s", refs, after)
	}
	if after := h.RemoteRefs(); after != remote {
		t.Fatalf("refused execution changed the remote:\nbefore:\n%s\nafter:\n%s", remote, after)
	}
	if after := len(h.AllEvents("M")); after != chains {
		t.Fatalf("refused execution changed the chains in M's clone: %d events, want %d", after, chains)
	}

	// The pre-sign self-check: an acceptance whose evidence omits the
	// approval is refused before anything is written.
	h.hn("E", "sync", "origin")
	ref := operationalActorRef(h.Actor("E"))
	head := h.git("E", "for-each-ref", "--format=%(objectname)", ref)
	h.inClone("E", func(identity *Identity) {
		events, err := collectEvents()
		if err != nil {
			t.Fatal(err)
		}
		proposal, err := resolveFullEvent(events, candidate.ID)
		if err != nil {
			t.Fatal(err)
		}
		evaluation, err := evaluateProposal(proposal, events)
		if err != nil {
			t.Fatal(err)
		}
		changed, err := changedPathsAt("", proposal.Event.Base, proposal.Event.Head)
		if err != nil {
			t.Fatal(err)
		}
		acceptance := evaluateRuleAcceptance(automergeRuleInputs(proposal, evaluation, "ordinary", changed, events))
		if !acceptance.Satisfied() {
			t.Fatalf("rule ordinary is not satisfied for %s: %+v", candidate.ID, acceptance.Unmet)
		}
		omitted := *acceptance
		omitted.Evidence = []string{c.Result.ID}
		stored, err := appendRuleMergeEvent(identity, proposal, evaluation, &omitted, candidate.Head)
		if err == nil || !strings.HasPrefix(err.Error(), "refusing to sign a rule merge fact that verifiers would reject: ") ||
			!strings.Contains(err.Error(), ruleCheckApprovals) {
			t.Fatalf("appendRuleMergeEvent without the approval returned %+v, %v; want the approval-threshold refusal", stored, err)
		}
	})
	if after := h.git("E", "for-each-ref", "--format=%(objectname)", ref); after != head {
		t.Fatalf("refused self-check moved E's actor ref from %q to %q", head, after)
	}

	// US1-1: E executes with no maintainer decision.
	preview, status := standingAutomergePreview(h, "E", h.Base(), candidate.ID, "ordinary")
	requireStandingAutomergeEligible(t, preview, status)
	run := standingAutomergeExecute(h, "E", h.Base(), candidate.ID, "ordinary")
	fact := requireStandingRuleMerge(h, "E", run, candidate, "ordinary", c.Approval.ID, c.Result.ID)

	// US1-2: E publishes; a verifier without an identity reports the rule
	// merge and its signer.
	h.git("E", "push", "-q", "origin", "main")
	h.Publish("E")
	if main := runOperationalGit(t, h.Remote(), "rev-parse", "main"); main != run.After {
		t.Fatalf("remote main is %s, want %s", main, run.After)
	}
	verifier := h.Verifier()
	h.hnIn(verifier, "sync", "origin")
	status2, code := h.RunIn(verifier, "proposal", "status", candidate.ID)
	if code != 0 {
		t.Fatalf("verifier status exited %d:\n%s", code, status2)
	}
	assertOperationalContains(t, status2, "Status:   merged\n"+standingAutomergeRuleStatusLine("ordinary", h.Actor("E"), fact.ID))
	if strings.Contains(status2, "Merged by maintainers") {
		t.Fatalf("verifier reports a maintainer merge:\n%s", status2)
	}
	h.hn("E", "sync", "origin")
	for _, event := range h.AllEvents("E") {
		if event.Kind == "proposal.decision" && event.Subject == candidate.ID {
			t.Fatalf("decision %s exists for %s", event.ID, candidate.ID)
		}
	}
}

// SC-002: after a maintainer adopts the standing rule, a candidate goes from
// open to merged with no event about it signed by M.
func TestStandingAutomergeExecuteWithoutMaintainer(t *testing.T) {
	h := newStandingHarness(t, standingHarnessSeedPolicy())
	adopted := standingHarnessDefaultPolicy()
	adopted.Requesters = []string{"A"}
	h.AdoptThroughAmendment(adopted)
	before := len(h.Events("M"))

	c := h.openCase("A", h.Base(), standingAutomergeFiles)
	c.Approval = h.Approve("R", c.Candidate.ID)
	c.Request = h.RequestCI("A", c.Candidate.ID)
	c.Result = h.RunCI(c.Request.ID)
	preview, status := standingAutomergePreview(h, "E", h.Base(), c.Candidate.ID, "ordinary")
	requireStandingAutomergeEligible(t, preview, status)
	run := standingAutomergeExecute(h, "E", h.Base(), c.Candidate.ID, "ordinary")
	requireStandingRuleMerge(h, "E", run, c.Candidate, "ordinary", c.Approval.ID, c.Result.ID)

	h.Publish("E")
	h.hn("M", "sync", "origin")
	if after := h.Events("M"); len(after) != before {
		t.Fatalf("M signed %d event(s) after the adoption: %#v", len(after)-before, after[before:])
	}
	for _, role := range []string{"E", "M"} {
		h.hn(role, "sync", "origin")
		for _, event := range h.AllEvents(role) {
			if event.Subject == c.Candidate.ID && event.Actor == h.Actor("M") {
				t.Fatalf("%s's clone holds %s %s about %s signed by M", role, event.Kind, event.ID, c.Candidate.ID)
			}
		}
	}
}

// FR-011: a maintainer listed in a standing rule's actors records a rule
// merge fact. Two rules: a candidate that satisfies two standing rules is
// recorded under the rule the executing actor names.
func TestStandingAutomergeExecuteMaintainerAndSecondRule(t *testing.T) {
	policy := standingHarnessDefaultPolicy()
	policy.Rules[0].Actors = []string{"E", "M"}
	policy.Rules = append(policy.Rules, standingHarnessRule{
		Name: "docsonly", TargetBranch: "main", Acceptance: standingHarnessString(automergeAcceptanceRule),
		Actors: []string{"E"}, Authors: []string{"A"}, Paths: []string{"docs/"},
	})
	h := newStandingHarness(t, policy)
	byMaintainer := h.readyCase("A", h.Base(), map[string]string{"src/fr011.txt": "merged by a maintainer executing the rule\n"})
	docs := h.readyCase("A", h.Base(), map[string]string{"docs/guide.md": "documentation change\n"})

	// M executes; the fact is a rule merge, which status reports.
	h.hn("M", "sync", "origin")
	run := standingAutomergeExecute(h, "M", h.Base(), byMaintainer.Candidate.ID, "ordinary")
	fact := requireStandingRuleMerge(h, "M", run, byMaintainer.Candidate, "ordinary", byMaintainer.Evidence()...)
	assertOperationalContains(t, h.hn("M", "proposal", "status", byMaintainer.Candidate.ID),
		"Status:   merged\n"+standingAutomergeRuleStatusLine("ordinary", h.Actor("M"), fact.ID))

	// E previews both rules, then executes the second one.
	for _, rule := range []string{"ordinary", "docsonly"} {
		output, status := standingAutomergePreview(h, "E", h.Base(), docs.Candidate.ID, rule)
		requireStandingAutomergeEligible(t, output, status)
	}
	run = standingAutomergeExecute(h, "E", h.Base(), docs.Candidate.ID, "docsonly")
	fact = requireStandingRuleMerge(h, "E", run, docs.Candidate, "docsonly", docs.Evidence()...)
	assertOperationalContains(t, h.hn("E", "proposal", "status", docs.Candidate.ID),
		standingAutomergeRuleStatusLine("docsonly", h.Actor("E"), fact.ID))
}

// FR-021: status of acceptance merges is unchanged, and a maintainer merge
// that carries a rule claim is reported with its claim unverified.
func TestStandingAutomergeExecuteStatusOfMaintainerMerges(t *testing.T) {
	h := newStandingHarness(t, standingHarnessDefaultPolicy())

	// B2 and B3: M's acceptance merge facts with a rule claim.
	fixtures := standingHarnessBoundaryFixtures(h)
	for _, fixture := range fixtures {
		if fixture.ID != "B2" && fixture.ID != "B3" {
			continue
		}
		signed := fixture.Build()
		if len(signed) != 1 {
			t.Fatalf("%s signed %d facts, want one", fixture.ID, len(signed))
		}
		claim := map[string]string{"B2": "nosuch", "B3": "ordinary"}[fixture.ID]
		status := h.hn("M", "proposal", "status", signed[0].Candidate)
		assertOperationalContains(t, status, fmt.Sprintf("Status:   merged\nMerged by maintainers in fact %s; its rule annotation %q is not verified\n", signed[0].ID, claim))
		if strings.Contains(status, "Merged by rule") {
			t.Fatalf("%s is reported as a rule merge:\n%s", fixture.ID, status)
		}
	}

	// An acceptance merge without a rule claim prints no new line.
	c := h.readyCase("A", h.Base(), nil)
	h.Accept("M", c.Candidate.ID)
	h.Checkout("M", "main", h.Base())
	h.hn("M", "merge", c.Candidate.ID)
	status := h.hn("M", "proposal", "status", c.Candidate.ID)
	assertOperationalContains(t, status, "Status:   merged\n")
	if strings.Contains(status, "Merged by") {
		t.Fatalf("acceptance merge status gained a merge line:\n%s", status)
	}
}

// standingAutomergeAgreement is one fixture of the preview-to-execution
// comparison (NFR-002): the candidate, the rule named, the declared tip of
// E's main and a blocker the preview must list ("" for the eligible one).
type standingAutomergeAgreement struct {
	Name, Candidate, Rule, Tip, Want string
}

// T094: in E's clone, with nothing synced between the two, the keyless
// preview is eligible exactly when execution merges, and a refused
// execution names the preview's blockers in the preview's order.
func TestStandingAutomergeAgreementPreviewExecute(t *testing.T) {
	h := newStandingHarness(t, standingHarnessDefaultPolicy())
	var table []string
	compare := func(f standingAutomergeAgreement) {
		t.Helper()
		h.hn("E", "sync", "origin")
		h.Checkout("E", "main", f.Tip)
		if main := h.git("E", "rev-parse", "main"); main != f.Tip {
			t.Fatalf("%s: E's main is %s, want the declared tip %s", f.Name, main, f.Tip)
		}
		refs := h.Refs("E")
		preview, status := h.Run("E", "automerge", f.Candidate, "--rule", f.Rule)
		if after := h.Refs("E"); after != refs {
			t.Fatalf("%s: preview changed E's refs", f.Name)
		}
		blockers := standingAutomergeBlockers(preview)
		eligible := status == 0 && len(blockers) == 0
		if f.Want == "" {
			requireStandingAutomergeEligible(t, preview, status)
		} else {
			requireStandingAutomergeBlockedBy(t, preview, status, f.Want)
		}
		run := standingAutomergeExecute(h, "E", f.Tip, f.Candidate, f.Rule)
		outcome := "refused"
		if eligible {
			outcome = "merged"
			if run.Status != 0 || run.After == f.Tip || len(run.Appended) != 1 || run.Appended[0].Kind != "proposal.merged" {
				t.Fatalf("%s: preview eligible but execution exited %d, moved main %s to %s and appended %#v:\n%s",
					f.Name, run.Status, f.Tip, run.After, run.Appended, run.Output)
			}
		} else {
			want := "hn: automerge blocked: " + strings.Join(blockers, "; ") + "\n"
			if run.Status == 0 || !strings.Contains(run.Output, want) ||
				strings.Join(standingAutomergeBlockers(run.Output), "\n") != strings.Join(blockers, "\n") {
				t.Fatalf("%s: preview blockers\n%s\nexecution exited %d with:\n%s", f.Name, strings.Join(blockers, "\n"), run.Status, run.Output)
			}
			if after := h.Refs("E"); after != refs || len(run.Appended) != 0 {
				t.Fatalf("%s: refused execution changed E's refs or chain", f.Name)
			}
		}
		previewOutcome := map[bool]string{true: "eligible", false: "blocked"}[eligible]
		table = append(table, fmt.Sprintf("%-38s preview %-8s execution %-8s agree", f.Name, previewOutcome, outcome))
	}

	base := h.Base()
	fixture := func(name string, c standingHarnessCase, rule, want string) standingAutomergeAgreement {
		return standingAutomergeAgreement{Name: name, Candidate: c.Candidate.ID, Rule: rule, Tip: c.Base, Want: want}
	}

	c := h.readyCase("A", base, nil)
	h.RequestChanges("R", c.Candidate.ID)
	compare(fixture("change request by counted reviewer", c, "ordinary", standingAutomergeChangeRequest))

	c = h.readyCase("A", base, nil)
	h.RequestChanges("S", c.Candidate.ID)
	compare(fixture("change request by uncounted reviewer", c, "ordinary", standingAutomergeChangeRequest))

	c = h.readyCase("A", base, nil)
	h.FailCI()
	h.act("T", "run.result", true, "run", "execute", c.Request.ID, "--backend", "host", "--allow-unsafe-host-execution", "--rerun")
	h.PassCI()
	compare(fixture("trusted failure", c, "ordinary", `pipeline "test" has a current trusted failure`))

	c = h.readyCase("A", base, nil)
	h.Reject("M", c.Candidate.ID, "Rejected by the agreement test.")
	compare(fixture("maintainer rejection", c, "ordinary", "proposal has a current maintainer rejection"))

	c = h.openCase("A", base, nil)
	c.Approval = h.Approve("S", c.Candidate.ID)
	h.completeCase(&c)
	compare(fixture("too few counted approvals", c, "ordinary", standingAutomergeThreshold0of1))

	c = h.openCase("A", base, nil)
	h.Approve("R", c.Candidate.ID)
	compare(fixture("too few passing results", c, "ordinary", standingAutomergeNoPass))

	compare(fixture("author outside the rule", h.readyCase("M", base, nil), "ordinary", "proposal author is not allowed by the rule"))
	compare(fixture("path outside the rule", h.readyCase("A", base, map[string]string{"other/x": "outside the rule\n"}), "ordinary",
		`changed path "other/x" is outside the rule allowlist`))

	policy := string(runOperationalGitBytes(t, h.Remote(), "show", base+":"+serveGovernedPolicyPath))
	compare(fixture("policy change", h.readyCase("A", base, map[string]string{
		serveGovernedPolicyPath: policy + "\n", "src/agreement.txt": "allowlisted change\n",
	}), "ordinary", "policy amendments require manual merge"))

	protected := standingHarnessDefaultPolicy()
	protected.ProtectedPaths = []string{"ci/"}
	compare(fixture("protected-check change", h.readyCase("A", h.PolicyCommit(base, protected), map[string]string{
		"src/agreement.txt": "allowlisted change\n", "ci/check.sh": "#!/bin/sh\nexit 0\n",
	}), "ordinary", "protected check changes require manual merge"))

	withMaintainersRule := standingHarnessDefaultPolicy()
	withMaintainersRule.Rules = append(withMaintainersRule.Rules, standingHarnessRule{
		Name: "maint", TargetBranch: "main", Acceptance: standingHarnessString(automergeAcceptanceMaintainers),
		Actors: []string{"M"}, Authors: []string{"A"}, Paths: []string{"src/", "tests/", "docs/"},
	})
	compare(fixture("rule with acceptance maintainers", h.readyCase("A", h.PolicyCommit(base, withMaintainersRule), nil), "maint",
		standingAutomergeNotAccepted))

	compare(fixture("rule that does not exist", h.readyCase("A", base, nil), "nosuch",
		`automerge rule "nosuch" does not exist in the signed base policy`))

	// The target policy differs: M lands H1, which keeps rule ordinary, and
	// A opens a candidate whose head builds on H1 but whose base is the
	// older commit. E's main is at H1.
	moved := standingHarnessDefaultPolicy()
	moved.Rules[0].Paths = []string{"src/", "tests/", "docs/", "lib/"}
	h.AdoptThroughAmendment(moved)
	h1 := h.Base()
	target := standingHarnessCase{Base: base, Author: "A"}
	target.Candidate = h.Open("A", base, h.Commit("A", h1, map[string]string{"src/target.txt": "built on the amendment\n"}))
	h.completeCase(&target)
	targetFixture := fixture("target policy differs from base policy", target, "ordinary", "current target policy differs from the signed base policy")
	targetFixture.Tip = h1
	compare(targetFixture)

	// Authorization is an execution-only check: X's preview is eligible
	// and X's execution is refused.
	satisfied := h.readyCase("A", h1, standingAutomergeFiles)
	h.hn("X", "sync", "origin")
	h.Checkout("X", "main", h1)
	preview, status := h.Run("X", "automerge", satisfied.Candidate.ID, "--rule", "ordinary")
	requireStandingAutomergeEligible(t, preview, status)
	refsX := h.Refs("X")
	refused := standingAutomergeExecute(h, "X", h1, satisfied.Candidate.ID, "ordinary")
	wantRefusal := fmt.Sprintf("identity %s is not an executing actor for automerge rule %q", h.Actor("X"), "ordinary")
	if refused.Status == 0 || !strings.Contains(refused.Output, wantRefusal) || h.Refs("X") != refsX || len(refused.Appended) != 0 {
		t.Fatalf("X's execution exited %d, want %q with nothing changed:\n%s", refused.Status, wantRefusal, refused.Output)
	}
	table = append(table, fmt.Sprintf("%-38s preview %-8s execution %-8s authorization refusal (not an agreement case)", "satisfied, executed by X", "eligible", "refused"))

	compare(fixture("satisfied", satisfied, "ordinary", ""))

	t.Logf("preview and execution agreement, %d fixtures, 100%% agreement:\n%s", len(table)-1, strings.Join(table, "\n"))
}

// standingAutomergeDocumentedLine matches one documented output line, where
// "..." stands for text that varies between runs.
func standingAutomergeDocumentedLine(documented string) *regexp.Regexp {
	parts := strings.Split(documented, "...")
	for index, part := range parts {
		parts[index] = regexp.QuoteMeta(part)
	}
	return regexp.MustCompile("^" + strings.Join(parts, ".*") + "$")
}

// The guide's standing-execute example replays the local User Story 1 flow
// on the harness: every command runs in the clone of the role its comment
// names, and every documented output line appears in that command's output
// in the documented order.
func TestStandingAutomergeDocumentedExecute(t *testing.T) {
	const marker = "<!-- example: standing-execute -->"
	blocks := standingPolicyGuideBlocks(t, marker)
	if len(blocks) != 1 {
		t.Fatalf("%s is followed by %d blocks, want one", marker, len(blocks))
	}
	policy := standingHarnessDefaultPolicy()
	policy.Requesters = []string{"A"}
	h := newStandingHarness(t, policy)
	head := h.Commit("A", h.Base(), standingAutomergeFiles)
	roles := map[string]string{}
	for role, name := range standingHarnessRoleNames {
		roles[name] = role
	}
	values := map[string]string{
		"<base-commit>":    h.Base(),
		"<head-commit>":    head,
		"<executor-actor>": h.Actor("E"),
	}
	substitute := func(text string) string {
		for placeholder, value := range values {
			text = strings.ReplaceAll(text, placeholder, value)
		}
		return text
	}
	learn := func(role string, appended []operationalEvent) {
		for _, event := range appended {
			short := operationalShortID(event.ID)
			switch event.Kind {
			case "proposal.open":
				values["<candidate-id>"], values["<candidate-short>"] = event.ID, short
			case "review.submit":
				values["<approval-short>"] = short
			case "run.request":
				values["<request-id>"], values["<request-short>"] = event.ID, short
			case "run.result":
				values["<result-short>"] = short
			case "proposal.merged":
				values["<fact-id>"], values["<fact-short>"] = event.ID, short
				values["<merge-short>"] = shortOID(h.fact(role, event.ID).Commit)
			}
		}
	}

	type step struct {
		role, command string
		expected      []string
	}
	var steps []step
	role := ""
	for _, line := range strings.Split(strings.TrimSuffix(blocks[0], "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "# As ") && strings.HasSuffix(line, ":"):
			name := strings.TrimSuffix(strings.TrimPrefix(line, "# As "), ":")
			if roles[name] == "" {
				t.Fatalf("documented flow names unknown identity %q", name)
			}
			role = roles[name]
		case strings.HasPrefix(line, "$ hn "):
			if role == "" {
				t.Fatalf("documented command %q has no identity", line)
			}
			steps = append(steps, step{role: role, command: strings.TrimPrefix(line, "$ hn ")})
		case len(steps) > 0:
			steps[len(steps)-1].expected = append(steps[len(steps)-1].expected, line)
		default:
			t.Fatalf("documented flow starts with %q, not an identity and a command", line)
		}
	}
	commands := []string{}
	for _, s := range steps {
		commands = append(commands, strings.Fields(s.command)[0])
	}
	if got := strings.Join(commands, " "); got != "proposal review run run automerge automerge proposal" {
		t.Fatalf("documented flow runs %q, want open, review, run request, runner, preview, execute and status", got)
	}

	for _, s := range steps {
		command := substitute(s.command)
		if leftover := standingAutomergeLeftoverPlaceholder.FindString(command); leftover != "" {
			t.Fatalf("documented command %q uses unknown placeholder %s", s.command, leftover)
		}
		h.hn(s.role, "sync", "origin")
		before := len(h.Events(s.role))
		output := h.hn(s.role, strings.Fields(command)...)
		learn(s.role, h.Events(s.role)[before:])
		h.Publish(s.role)
		lines := strings.Split(output, "\n")
		next := 0
		for _, documented := range s.expected {
			want := substitute(documented)
			if leftover := standingAutomergeLeftoverPlaceholder.FindString(want); leftover != "" {
				t.Fatalf("documented line %q uses unknown placeholder %s", documented, leftover)
			}
			pattern := standingAutomergeDocumentedLine(want)
			for next < len(lines) && !pattern.MatchString(lines[next]) {
				next++
			}
			if next == len(lines) {
				t.Fatalf("hn %s omits documented line %q in the documented order:\n%s", command, want, output)
			}
			next++
		}
	}
	if values["<fact-id>"] == "" {
		t.Fatal("documented flow recorded no merge fact")
	}
	if fact := h.fact("E", values["<fact-id>"]); fact.Rule != "ordinary" || fact.Actor != h.Actor("E") {
		t.Fatalf("documented flow recorded %+v, want a rule merge fact of rule ordinary by E", fact)
	}
}

// Local repair (WP12, FR-020) and executing-actor containment (FR-019,
// NFR-003, C-007). Every refusal is checked against a snapshot of the
// acting clone's refs, the number of events on each of its chains and the
// bare remote's refs.

// standingAutomergeRepairLine is the preview line of a repair state.
func standingAutomergeRepairLine(merge string) string {
	return fmt.Sprintf("Repair: code merged at %s; --execute records the missing rule merge fact\n", shortOID(merge))
}

// standingAutomergeContain leaves role's main at a native two-parent merge
// of c's head on c's base whose merge fact was never recorded, the state an
// interrupted hn automerge leaves behind. It builds the commit with git
// commit-tree and moves main with git update-ref, with no in-process step,
// and returns the merge commit.
func standingAutomergeContain(h *standingHarness, role string, c standingHarnessCase) string {
	h.t.Helper()
	h.Checkout(role, "main", c.Base)
	tree := h.git(role, "rev-parse", c.Candidate.Head+"^{tree}")
	merge := h.git(role, "commit-tree", tree, "-p", c.Base, "-p", c.Candidate.Head,
		"-m", "Merge HN proposal "+operationalShortID(c.Candidate.ID)+" without its merge fact")
	h.git(role, "update-ref", "refs/heads/main", merge, c.Base)
	h.git(role, "reset", "-q", "--hard", "main")
	if status := h.git(role, "status", "--porcelain", "--untracked-files=all"); status != "" {
		h.t.Fatalf("contained state in %s's clone is not clean:\n%s", role, status)
	}
	return merge
}

// standingAutomergeState is every ref of the clone at directory, the event
// count of every chain it holds and every ref of the bare remote.
func standingAutomergeState(h *standingHarness, directory string) string {
	h.t.Helper()
	refs := runOperationalGit(h.t, directory, "for-each-ref", "--format=%(refname) %(objectname)")
	var chains []string
	for _, ref := range strings.Fields(runOperationalGit(h.t, directory, "for-each-ref", "--format=%(refname)", "refs/hn/actors", "refs/hn/remotes")) {
		if !strings.HasPrefix(ref, "refs/hn/actors/") {
			if _, _, ok := parseAcceptedActorRef(ref); !ok {
				continue
			}
		}
		chains = append(chains, ref+" "+runOperationalGit(h.t, directory, "rev-list", "--count", ref))
	}
	return "refs:\n" + refs + "\nchain events:\n" + strings.Join(chains, "\n") + "\nremote refs:\n" + h.RemoteRefs()
}

// requireStandingAutomergeContained runs hn args in the clone at directory
// and requires it to fail with every one of want in its output, with no ref
// of the clone or the remote changed and no event on any chain (NFR-003).
func requireStandingAutomergeContained(h *standingHarness, directory string, args []string, want ...string) string {
	h.t.Helper()
	before := standingAutomergeState(h, directory)
	output, status := h.RunIn(directory, args...)
	if status == 0 {
		h.t.Fatalf("hn %s succeeded, want a refusal with %q:\n%s", strings.Join(args, " "), want, output)
	}
	for _, text := range want {
		if !strings.Contains(output, text) {
			h.t.Fatalf("hn %s exited %d without %q:\n%s", strings.Join(args, " "), status, text, output)
		}
	}
	if after := standingAutomergeState(h, directory); after != before {
		h.t.Fatalf("refused hn %s changed state:\nbefore:\n%s\nafter:\n%s", strings.Join(args, " "), before, after)
	}
	return output
}

// standingAutomergeRefsBut drops ref's line from a Refs listing.
func standingAutomergeRefsBut(refs, ref string) string {
	var kept []string
	for _, line := range strings.Split(refs, "\n") {
		if !strings.HasPrefix(line, ref+" ") {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// requireStandingRuleRepair runs the repair in role's clone, whose main is
// the contained merge, and requires exactly one new rule merge fact of rule
// ordinary for c with commit set to merge and c's evidence, no commit, no
// moved ref other than role's own chain, and the documented output. It
// returns the fact.
func requireStandingRuleRepair(h *standingHarness, role string, c standingHarnessCase, merge string) standingHarnessFact {
	h.t.Helper()
	t := h.t
	refs := h.Refs(role)
	run := standingAutomergeExecute(h, role, merge, c.Candidate.ID, "ordinary")
	if run.Status != 0 {
		t.Fatalf("%s's repair of %s exited %d:\n%s", role, c.Candidate.ID, run.Status, run.Output)
	}
	if run.After != merge {
		t.Fatalf("repair moved main from %s to %s", merge, run.After)
	}
	chain := operationalActorRef(h.Actor(role))
	if after := h.Refs(role); standingAutomergeRefsBut(after, chain) != standingAutomergeRefsBut(refs, chain) {
		t.Fatalf("repair changed refs other than %s:\nbefore:\n%s\nafter:\n%s", chain, refs, after)
	}
	if len(run.Appended) != 1 || run.Appended[0].Kind != "proposal.merged" {
		t.Fatalf("%s's chain gained %#v, want exactly one proposal.merged", role, run.Appended)
	}
	fact := h.fact(role, run.Appended[0].ID)
	evidence := c.Evidence()
	sort.Strings(evidence)
	if fact.Rule != "ordinary" || fact.Subject != c.Candidate.ID || fact.Head != c.Candidate.Head || fact.Commit != merge ||
		fact.Policy != h.PolicyDigest(c.Base) || fact.Actor != h.Actor(role) || strings.Join(fact.Evidence, ",") != strings.Join(evidence, ",") {
		t.Fatalf("repaired fact %+v, want rule ordinary by %s at %s under the base policy citing %v", fact, role, merge, evidence)
	}
	assertOperationalContains(t, run.Output, standingAutomergeRepairLine(merge),
		fmt.Sprintf("Recorded missing rule merge event %s under rule ordinary with %d evidence item(s)\n", operationalShortID(fact.ID), len(evidence)))
	if strings.Contains(run.Output, "Merged proposal") {
		t.Fatalf("repair reports a merge:\n%s", run.Output)
	}
	return fact
}

// requireStandingRepairDone requires a second rerun after a repair to be
// blocked as already merged and to change nothing.
func requireStandingRepairDone(h *standingHarness, role string, c standingHarnessCase, merge string) {
	h.t.Helper()
	refs := h.Refs(role)
	again := standingAutomergeExecute(h, role, merge, c.Candidate.ID, "ordinary")
	if again.Status == 0 || !strings.Contains(again.Output, "hn: automerge blocked: ") ||
		!stringInSlice("proposal is already recorded as merged", standingAutomergeBlockers(again.Output)) {
		h.t.Fatalf("second rerun exited %d, want blocked as already merged:\n%s", again.Status, again.Output)
	}
	if strings.Contains(again.Output, "Repair:") || again.After != merge || len(again.Appended) != 0 || h.Refs(role) != refs {
		h.t.Fatalf("second rerun changed state or reported a repair:\n%s", again.Output)
	}
}

// T056 steps 1 to 3: a run interrupted for real after the code merged is
// repaired by the same command through the binary, exactly once, and the
// repaired fact is admitted by another clone.
func TestStandingAutomergeRepairInterruptedRun(t *testing.T) {
	h := newStandingHarness(t, standingHarnessDefaultPolicy())
	candidate := h.ReadyCandidate(standingAutomergeFiles)
	c := h.Case(candidate.ID)
	rerun := "hn automerge " + candidate.ID + " --rule ordinary --execute"

	h.hn("E", "sync", "origin")
	h.Checkout("E", "main", h.Base())
	before := len(h.Events("E"))
	t.Cleanup(func() { automergeProcessHook = nil })
	interruption := errors.New("interrupted by the standing repair test")
	automergeProcessHook = func(step string) error {
		if step == "merge-committed" {
			return interruption
		}
		return nil
	}
	var runErr error
	h.inClone("E", func(*Identity) {
		runErr = cmdAutomerge([]string{candidate.ID, "--rule", "ordinary", "--execute"})
	})
	automergeProcessHook = nil
	merge := h.git("E", "rev-parse", "main")
	if want := fmt.Sprintf("code merged at %s but event recording interrupted; run '%s': %v", merge, rerun, interruption); runErr == nil || runErr.Error() != want {
		t.Fatalf("interrupted run returned %v, want %q", runErr, want)
	}
	if parents := strings.Fields(h.git("E", "rev-list", "--parents", "-n", "1", merge)); len(parents) != 3 || parents[1] != h.Base() || parents[2] != candidate.Head {
		t.Fatalf("main is %v, want a merge of %s and %s", parents, h.Base(), candidate.Head)
	}
	if after := h.Events("E"); len(after) != before {
		t.Fatalf("interrupted run appended %d event(s) to E's chain", len(after)-before)
	}

	preview, status := standingAutomergePreview(h, "E", merge, candidate.ID, "ordinary")
	requireStandingAutomergeEligible(t, preview, status)
	assertOperationalContains(t, preview, "Target: main at "+merge+"\n",
		standingAutomergeRepairLine(merge)+"Eligible: explicit --execute is required to merge locally.\n")

	fact := requireStandingRuleRepair(h, "E", c, merge)
	assertOperationalContains(t, h.hn("E", "proposal", "status", candidate.ID),
		"Status:   merged\n"+standingAutomergeRuleStatusLine("ordinary", h.Actor("E"), fact.ID))
	requireStandingRepairDone(h, "E", c, merge)

	// The repaired fact is an ordinary rule merge fact for every verifier.
	h.git("E", "push", "-q", "origin", "main")
	h.Publish("E")
	h.hn("M", "sync", "origin")
	assertOperationalContains(t, h.hn("M", "proposal", "status", candidate.ID),
		"Status:   merged\n"+standingAutomergeRuleStatusLine("ordinary", h.Actor("E"), fact.ID))
}

// T056 steps 4, 5 and 7: the same repair from a state built with Git alone,
// and the refusals of a vetoed candidate, of a target whose policy moved
// and of hn merge by the executing actor.
func TestStandingAutomergeRepairConstructedState(t *testing.T) {
	h := newStandingHarness(t, standingHarnessDefaultPolicy())

	repaired := h.readyCase("A", h.Base(), standingAutomergeFiles)
	h.hn("E", "sync", "origin")
	merge := standingAutomergeContain(h, "E", repaired)
	// hn merge stays maintainer-only: it refuses E before any repair path.
	requireStandingAutomergeContained(h, h.Work("E"), []string{"merge", repaired.Candidate.ID},
		"proposal lacks 1 required acceptance decision(s)")
	preview, status := standingAutomergePreview(h, "E", merge, repaired.Candidate.ID, "ordinary")
	requireStandingAutomergeEligible(t, preview, status)
	assertOperationalContains(t, preview, standingAutomergeRepairLine(merge))
	requireStandingRuleRepair(h, "E", repaired, merge)
	requireStandingRepairDone(h, "E", repaired, merge)

	// A trusted change request that arrives before the rerun.
	vetoed := h.readyCase("A", h.Base(), map[string]string{"src/vetoed.txt": "vetoed after the merge\n"})
	h.hn("E", "sync", "origin")
	merge = standingAutomergeContain(h, "E", vetoed)
	h.RequestChanges("S", vetoed.Candidate.ID)
	preview, status = standingAutomergePreview(h, "E", merge, vetoed.Candidate.ID, "ordinary")
	requireStandingAutomergeBlocked(t, preview, status, standingAutomergeChangeRequest, automergeTargetMoved)
	if strings.Contains(preview, "Repair:") {
		t.Fatalf("vetoed candidate reports a repair:\n%s", preview)
	}
	requireStandingAutomergeContained(h, h.Work("E"), []string{"automerge", vetoed.Candidate.ID, "--rule", "ordinary", "--execute"},
		"hn: automerge blocked: "+standingAutomergeChangeRequest+"; "+automergeTargetMoved+"\n")

	// The target moved again with a policy-changing merge.
	moved := h.readyCase("A", h.Base(), map[string]string{"src/moved.txt": "merged before a policy change\n"})
	h.hn("E", "sync", "origin")
	standingAutomergeContain(h, "E", moved)
	amended := standingHarnessDefaultPolicy()
	amended.Rules[0].Paths = []string{"src/", "tests/", "docs/", "lib/"}
	policyChange := h.PolicyCommit(h.Base(), amended)
	h.git("E", "fetch", "-q", "origin")
	h.git("E", "merge", "-q", "--no-ff", "--no-edit", "-m", "Land a policy change outside hn", policyChange)
	tip := h.git("E", "rev-parse", "main")
	preview, status = standingAutomergePreview(h, "E", tip, moved.Candidate.ID, "ordinary")
	requireStandingAutomergeBlocked(t, preview, status, automergeTargetMoved, "current target policy differs from the signed base policy")
	if strings.Contains(preview, "Repair:") {
		t.Fatalf("moved policy reports a repair:\n%s", preview)
	}
	requireStandingAutomergeContained(h, h.Work("E"), []string{"automerge", moved.Candidate.ID, "--rule", "ordinary", "--execute"},
		"hn: automerge blocked: ", automergeTargetMoved)
}

// T056 steps 6 and 7: under acceptance mode maintainers the contained state
// keeps the blocker that directs to hn merge, hn merge refuses the
// executing actor and repairs for the maintainer as before.
func TestStandingAutomergeRepairMaintainersModeUnchanged(t *testing.T) {
	h := newStandingHarness(t, standingHarnessDefaultPolicy())
	variant := h.PolicyCommit(h.Base(), standingAutomergeMaintainersVariant())
	c := h.readyCase("A", variant, standingAutomergeFiles)
	h.Accept("M", c.Candidate.ID)

	h.hn("E", "sync", "origin")
	standingAutomergeContain(h, "E", c)
	requireStandingAutomergeContained(h, h.Work("E"), []string{"merge", c.Candidate.ID},
		fmt.Sprintf("identity %s is not a maintainer under proposal policy", shortID(h.Actor("E"))))

	h.hn("M", "sync", "origin")
	merge := standingAutomergeContain(h, "M", c)
	preview, status := standingAutomergePreview(h, "M", merge, c.Candidate.ID, "ordinary")
	requireStandingAutomergeBlocked(t, preview, status, automergeTargetMoved)
	if strings.Contains(preview, "Repair:") {
		t.Fatalf("maintainers-mode rule reports a repair:\n%s", preview)
	}
	requireStandingAutomergeContained(h, h.Work("M"), []string{"automerge", c.Candidate.ID, "--rule", "ordinary", "--execute"},
		"hn: automerge blocked: "+automergeTargetMoved+"\n")

	refs := h.Refs("M")
	before := len(h.Events("M"))
	output := h.hn("M", "merge", c.Candidate.ID)
	appended := h.Events("M")[before:]
	if len(appended) != 1 || appended[0].Kind != "proposal.merged" {
		t.Fatalf("hn merge repair appended %#v, want one proposal.merged", appended)
	}
	fact := h.fact("M", appended[0].ID)
	if fact.Commit != merge || fact.Rule != "" || fact.Subject != c.Candidate.ID {
		t.Fatalf("hn merge repair recorded %+v, want an acceptance merge fact at %s", fact, merge)
	}
	assertOperationalContains(t, output, fmt.Sprintf("Recorded missing merge event %s for proposal %s at %s with 1 acceptance decision(s)\n",
		operationalShortID(fact.ID), operationalShortID(c.Candidate.ID), shortOID(merge)))
	chain := operationalActorRef(h.Actor("M"))
	if main := h.git("M", "rev-parse", "main"); main != merge || standingAutomergeRefsBut(h.Refs("M"), chain) != standingAutomergeRefsBut(refs, chain) {
		t.Fatalf("hn merge repair moved main to %s or changed other refs", main)
	}
}

// T057 cases 1 to 3: policy amendments, rules a candidate grants itself and
// protected-check changes never merge by rule, whoever executes it.
func TestStandingAutomergeContainmentManualPath(t *testing.T) {
	h := newStandingHarness(t, standingHarnessDefaultPolicy())
	base := h.Base()
	execute := func(role, target, candidate, rule string, want ...string) {
		t.Helper()
		h.hn(role, "sync", "origin")
		h.Checkout(role, "main", target)
		requireStandingAutomergeContained(h, h.Work(role), []string{"automerge", candidate, "--rule", rule, "--execute"},
			append([]string{"hn: automerge blocked: "}, want...)...)
	}

	// US2-3: a head that adds rule selfgrant listing its author A and S.
	selfgrant := standingHarnessDefaultPolicy()
	selfgrant.Rules = append(selfgrant.Rules, standingHarnessRule{
		Name: "selfgrant", TargetBranch: "main", Acceptance: standingHarnessString(automergeAcceptanceRule),
		Actors: []string{"A", "S"}, Authors: []string{"A"}, Paths: []string{"src/"},
	})
	grant := h.readyCase("A", base, map[string]string{
		serveGovernedPolicyPath: selfgrant.JSON(h.actors), "src/selfgrant.txt": "granted by its own head\n",
	})
	for _, role := range []string{"A", "S"} {
		execute(role, base, grant.Candidate.ID, "selfgrant", `automerge rule "selfgrant" does not exist in the signed base policy`)
	}

	// US6-4: every review, CI result, designated approval and maintainer
	// acceptance is present, and the protected check still takes the
	// manual path; so does a policy change next to an allowed path.
	protected := standingHarnessDefaultPolicy()
	protected.ProtectedPaths = []string{"scripts/lint.sh"}
	protectedBase := h.PolicyCommit(base, protected)
	check := h.readyCase("A", protectedBase, map[string]string{
		"scripts/lint.sh": "#!/bin/sh\nexit 0\n", "src/x": "changed with the check\n",
	})
	h.Approve("M", check.Candidate.ID)
	h.Accept("M", check.Candidate.ID)
	execute("E", protectedBase, check.Candidate.ID, "ordinary", "protected check changes require manual merge")

	policy := string(runOperationalGitBytes(t, h.Remote(), "show", base+":"+serveGovernedPolicyPath))
	amendsPolicy := h.readyCase("A", base, map[string]string{serveGovernedPolicyPath: policy + "\n", "src/x": "changed with the policy\n"})
	h.Accept("M", amendsPolicy.Candidate.ID)
	execute("E", base, amendsPolicy.Candidate.ID, "ordinary", "policy amendments require manual merge")

	// US2-2: an amendment adding a standing rule, before its evidence and
	// once accepted, is refused under every rule; M lands it with hn merge.
	amended := standingHarnessDefaultPolicy()
	amended.Rules = append(amended.Rules, standingHarnessRule{
		Name: "docsonly", TargetBranch: "main", Acceptance: standingHarnessString(automergeAcceptanceRule),
		Actors: []string{"E"}, Authors: []string{"A"}, Paths: []string{"docs/"},
	})
	amendment := h.Open("M", base, h.PolicyCommit(base, amended))
	execute("E", base, amendment.ID, "ordinary", "policy amendments require manual merge")
	h.Approve("R", amendment.ID)
	h.RunCI(h.RequestCI("M", amendment.ID).ID)
	h.Accept("M", amendment.ID)
	for _, role := range []string{"E", "X"} {
		for _, rule := range []string{"ordinary", "other"} {
			execute(role, base, amendment.ID, rule, "policy amendments require manual merge")
		}
	}
	h.hn("M", "sync", "origin")
	h.Checkout("M", "main", base)
	output := h.hn("M", "merge", amendment.ID)
	assertOperationalContains(t, output, "Merged proposal "+operationalShortID(amendment.ID)+" into main at ", "Recorded merge event ")
	if merge := h.latest("M"); merge.Kind != "proposal.merged" || merge.Subject != amendment.ID || h.fact("M", merge.ID).Rule != "" {
		t.Fatalf("hn merge of the amendment appended %#v, want its acceptance merge fact", merge)
	}
}

// standingAutomergeCopyIdentity gives the clone at directory the hn
// identity of role's clone, so a fresh clone acts as role.
func standingAutomergeCopyIdentity(h *standingHarness, role, directory string) {
	h.t.Helper()
	source := filepath.Join(h.Work(role), ".git", "hn")
	target := filepath.Join(directory, ".git", "hn")
	err := filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative != "." && relative != "active" && relative != "identity.json" && relative != "rotation.json" &&
			relative != "identities" && filepath.Dir(relative) != "identities" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return os.MkdirAll(filepath.Join(target, relative), 0o700)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(target, relative), content, 0o600)
	})
	if err != nil {
		h.t.Fatal(err)
	}
	if actor := readOperationalIdentity(h.t, h.Binary(), directory).Actor; actor != h.Actor(role) {
		h.t.Fatalf("copied identity is %s, want %s", actor, h.Actor(role))
	}
}

// T057 case 4 (US6-5): a moved target, a dirty worktree, an unfinished
// merge, a shallow clone and a partial clone each refuse execution with
// today's message.
func TestStandingAutomergeContainmentRepositoryState(t *testing.T) {
	h := newStandingHarness(t, standingHarnessDefaultPolicy())
	c := h.readyCase("A", h.Base(), standingAutomergeFiles)
	args := []string{"automerge", c.Candidate.ID, "--rule", "ordinary", "--execute"}
	work := h.Work("E")
	h.hn("E", "sync", "origin")

	moved := h.Commit("M", h.Base(), map[string]string{"docs/moved.txt": "the target moved\n"})
	h.Checkout("E", "main", moved)
	requireStandingAutomergeContained(h, work, args, "hn: automerge blocked: "+automergeTargetMoved+"\n")

	h.Checkout("E", "main", h.Base())
	dirty := filepath.Join(work, "src", "dirty.txt")
	if err := os.WriteFile(dirty, []byte("uncommitted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	requireStandingAutomergeContained(h, work, args, "hn: automerge blocked: worktree and index must be clean before automerge\n")
	if err := os.Remove(dirty); err != nil {
		t.Fatal(err)
	}

	ours := h.Commit("E", h.Base(), map[string]string{"docs/conflict.txt": "ours\n"})
	theirs := h.Commit("E", h.Base(), map[string]string{"docs/conflict.txt": "theirs\n"})
	h.Checkout("E", "main", ours)
	runOperationalGitFailure(t, work, "merge", "--no-edit", theirs)
	mergeHead := filepath.Join(work, h.git("E", "rev-parse", "--git-path", "MERGE_HEAD"))
	if _, err := os.Stat(mergeHead); err != nil {
		t.Fatalf("the conflicting merge left no MERGE_HEAD: %v", err)
	}
	requireStandingAutomergeContained(h, work, args, "automerge refuses an unfinished Git operation (MERGE_HEAD); finish or abort it first")
	if _, err := os.Stat(mergeHead); err != nil {
		t.Fatalf("the refusal removed MERGE_HEAD: %v", err)
	}
	h.git("E", "merge", "--abort")

	shallow := filepath.Join(t.TempDir(), "shallow")
	runOperationalGit(t, "", "clone", "-q", "--depth", "1", "--no-local", h.Remote(), shallow)
	standingAutomergeCopyIdentity(h, "E", shallow)
	requireStandingAutomergeContained(h, shallow, args, "automerge requires a full repository; hydrate shallow history or use manual merge")

	partial := filepath.Join(t.TempDir(), "partial")
	runOperationalGit(t, "", "clone", "-q", "--filter=blob:none",
		"--upload-pack=git -c uploadpack.allowFilter=true upload-pack", "file://"+h.Remote(), partial)
	if promisor := runOperationalGit(t, partial, "config", "--get", "remote.origin.promisor"); promisor != "true" {
		t.Fatalf("partial clone has remote.origin.promisor %q, want true", promisor)
	}
	standingAutomergeCopyIdentity(h, "E", partial)
	requireStandingAutomergeContained(h, partial, args, "automerge does not support partial/promisor repositories; hydrate a full clone or use manual merge")
}

// T057 case 5 (US6-6): the executing actor E cannot publish a decision that
// counts, cannot merge manually, locally or through a remote, and cannot
// execute a rule that does not list it.
func TestStandingAutomergeContainmentForbiddenCommands(t *testing.T) {
	policy := standingHarnessDefaultPolicy()
	policy.Rules[1].Paths = []string{"src/", "tests/", "docs/"}
	h := newStandingHarness(t, policy)
	c := h.readyCase("A", h.Base(), standingAutomergeFiles)
	work, short := h.Work("E"), shortID(h.Actor("E"))
	h.hn("E", "sync", "origin")
	h.Checkout("E", "main", h.Base())

	requireStandingAutomergeContained(h, work, []string{"decide", c.Candidate.ID, "--accept"},
		fmt.Sprintf("identity %s is not a maintainer under policy %s", short, shortID(h.PolicyDigest(h.Base()))))
	requireStandingAutomergeContained(h, work, []string{"merge", c.Candidate.ID}, "proposal lacks 1 required acceptance decision(s)")

	// A maintainer acceptance does not open the manual paths to E.
	h.Accept("M", c.Candidate.ID)
	h.hn("E", "sync", "origin")
	requireStandingAutomergeContained(h, work, []string{"merge", c.Candidate.ID},
		fmt.Sprintf("identity %s is not a maintainer under proposal policy", short))
	requireStandingAutomergeContained(h, work, []string{"merge", c.Candidate.ID, "--remote", "origin"},
		fmt.Sprintf("advancement step 1 (validate locally) failed: identity %s is not a maintainer under proposal policy", short))

	// Rule other has the same paths and authors but lists only X.
	preview, status := h.Run("E", "automerge", c.Candidate.ID, "--rule", "other")
	requireStandingAutomergeEligible(t, preview, status)
	requireStandingAutomergeContained(h, work, []string{"automerge", c.Candidate.ID, "--rule", "other", "--execute"},
		fmt.Sprintf("identity %s is not an executing actor for automerge rule %q", h.Actor("E"), "other"))
}

// T057 case 6 (US6-1 to US6-3 at execution): every veto and rule blocker
// refuses --execute with automerge blocked: naming it, and the satisfied
// control merges.
func TestStandingAutomergeContainmentBlockersAtExecution(t *testing.T) {
	h := newStandingHarness(t, standingHarnessDefaultPolicy())
	refuse := func(c standingHarnessCase, blocker string) {
		t.Helper()
		h.hn("E", "sync", "origin")
		h.Checkout("E", "main", c.Base)
		requireStandingAutomergeContained(h, h.Work("E"), []string{"automerge", c.Candidate.ID, "--rule", "ordinary", "--execute"},
			"hn: automerge blocked: ", blocker)
	}

	rejected := h.readyCase("A", h.Base(), nil)
	h.Reject("M", rejected.Candidate.ID, "Rejected by the standing containment test.")
	refuse(rejected, "proposal has a current maintainer rejection")

	changes := h.readyCase("A", h.Base(), nil)
	h.RequestChanges("S", changes.Candidate.ID)
	refuse(changes, standingAutomergeChangeRequest)

	failed := h.readyCase("A", h.Base(), nil)
	h.FailCI()
	h.act("T", "run.result", true, "run", "execute", failed.Request.ID, "--backend", "host", "--allow-unsafe-host-execution", "--rerun")
	h.PassCI()
	refuse(failed, `pipeline "test" has a current trusted failure`)

	missing := h.openCase("A", h.Base(), nil)
	h.Approve("R", missing.Candidate.ID)
	refuse(missing, standingAutomergeNoPass)

	refuse(h.readyCase("M", h.Base(), nil), "proposal author is not allowed by the rule")
	refuse(h.readyCase("A", h.Base(), map[string]string{"other/x": "outside the rule\n"}), `changed path "other/x" is outside the rule allowlist`)

	satisfied := h.readyCase("A", h.Base(), standingAutomergeFiles)
	h.hn("E", "sync", "origin")
	run := standingAutomergeExecute(h, "E", h.Base(), satisfied.Candidate.ID, "ordinary")
	requireStandingRuleMerge(h, "E", run, satisfied.Candidate, "ordinary", satisfied.Evidence()...)
}

// standingAutomergeInterrupt runs hn automerge candidate --rule rule
// --execute in-process in role's clone, with fn called at the
// merge-committed seam, and returns the resulting main and its error.
func standingAutomergeInterrupt(h *standingHarness, role, target, candidate, rule string, fn func() error) (string, error) {
	h.t.Helper()
	h.hn(role, "sync", "origin")
	h.Checkout(role, "main", target)
	h.t.Cleanup(func() { automergeProcessHook = nil })
	automergeProcessHook = func(step string) error {
		if step == "merge-committed" {
			return fn()
		}
		return nil
	}
	var runErr error
	h.inClone(role, func(*Identity) {
		runErr = cmdAutomerge([]string{candidate, "--rule", rule, "--execute"})
	})
	automergeProcessHook = nil
	return h.git(role, "rev-parse", "main"), runErr
}

// T055 steps 3 and 4: the rerun hints. Under acceptance mode maintainers the
// interrupted run still names hn merge, byte for byte; under acceptance mode
// rule a recording failure after the merge names the same hn automerge
// command, which then repairs.
func TestStandingAutomergeRepairRerunHints(t *testing.T) {
	h := newStandingHarness(t, standingHarnessDefaultPolicy())
	interruption := errors.New("interrupted by the standing hint test")

	variant := h.PolicyCommit(h.Base(), standingAutomergeMaintainersVariant())
	accepted := h.readyCase("A", variant, standingAutomergeFiles)
	h.Accept("M", accepted.Candidate.ID)
	merge, err := standingAutomergeInterrupt(h, "M", variant, accepted.Candidate.ID, "ordinary", func() error { return interruption })
	if want := fmt.Sprintf("code merged at %s but event recording interrupted; run 'hn merge %s': %v", merge, accepted.Candidate.ID, interruption); err == nil || err.Error() != want {
		t.Fatalf("maintainers-mode interruption returned %v, want %q", err, want)
	}

	// A held lock on E's actor ref makes the append fail after the merge.
	c := h.readyCase("A", h.Base(), map[string]string{"src/hint.txt": "recorded after a failure\n"})
	lock := filepath.Join(h.Work("E"), h.git("E", "rev-parse", "--git-path", operationalActorRef(h.Actor("E")))+".lock")
	t.Cleanup(func() { _ = os.Remove(lock) })
	before := len(h.Events("E"))
	merge, err = standingAutomergeInterrupt(h, "E", h.Base(), c.Candidate.ID, "ordinary", func() error {
		if err := os.MkdirAll(filepath.Dir(lock), 0o755); err != nil {
			return err
		}
		return os.WriteFile(lock, nil, 0o644)
	})
	prefix := fmt.Sprintf("code merged at %s but recording failed: ", shortOID(merge))
	suffix := fmt.Sprintf("; run 'hn automerge %s --rule ordinary --execute'", c.Candidate.ID)
	if err == nil || !strings.HasPrefix(err.Error(), prefix) || !strings.HasSuffix(err.Error(), suffix) {
		t.Fatalf("recording failure returned %v, want %q...%q", err, prefix, suffix)
	}
	if merge == h.Base() || len(h.Events("E")) != before {
		t.Fatalf("recording failure left main at %s with %d new event(s)", merge, len(h.Events("E"))-before)
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	requireStandingRuleRepair(h, "E", c, merge)
}
