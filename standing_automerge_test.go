package main

import (
	"regexp"
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
