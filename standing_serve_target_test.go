package main

import (
	"strings"
	"testing"
)

// Rule advancement against a hosted tip T that moved past the candidate's
// signed base B. Step 5 makes the head H descend from T, so T..H is what
// the advancement changes; the server must judge T..H by the rule's path
// conditions as it judges B..H. Otherwise H can undo a change T made, such
// as a newer pipeline definition, while B..H shows only allowed paths.

const standingServeTargetPipelinePath = ".hn/pipelines/test.json"

// standingServeTargetCase moves hosted main from B to T, one commit that
// changes targetFiles, builds H on T changing headFiles, and opens a
// candidate signed with base B and head H. It gathers R's approval when
// approve is set and U's passing result on M's run request, so only the
// target paths can refuse it. It requires B..H to be exactly
// baseHeadPaths, the only paths the candidate's own diff shows.
func standingServeTargetCase(t *testing.T, g *serveGovernedFixture, targetFiles, headFiles map[string]string, approve bool, baseHeadPaths ...string) (string, operationalEvent) {
	t.Helper()
	target := g.Commit(g.Base, targetFiles)
	g.SetMainForTest(target)
	head := g.Commit(target, headFiles)
	if got := g.git("A", "diff", "--name-only", g.Base, head); got != strings.Join(baseHeadPaths, "\n") {
		t.Fatalf("B..H paths = %q, want %q", got, baseHeadPaths)
	}
	p := g.OpenProposal(g.Base, head)
	if approve {
		g.Approve(p.ID)
	}
	g.RunCI(g.RequestCI(p.ID).ID)
	standingServeImport(g, "E")
	return target, p
}

// standingServeTargetRefused requires E's advancement of p under rule
// ordinary to be protected-rule-not-satisfied naming each T..H defect,
// with every hosted ref unchanged and main still at target.
func standingServeTargetRefused(t *testing.T, g *serveGovernedFixture, target string, p operationalEvent, defects ...string) {
	t.Helper()
	rejection, _ := standingServeRuleRefused(t, g, "E", p.ID, p.Head, "ordinary", serveReasonProtectedRuleNotSatisfied)
	requireServeIDs(t, rejection, p.ID)
	requireServeMissing(t, rejection, serveEvidenceProposal)
	requireServeNext(t, rejection, "revise "+p.ID+" against "+target)
	want := "candidate " + p.ID + " does not satisfy rule ordinary at refs/heads/main " + target[:8] + ": " + strings.Join(defects, "; ") + "."
	if rejection.Sentence != want {
		t.Fatalf("sentence\n%q\nwant\n%q", rejection.Sentence, want)
	}
	if tip := g.ServerTip("main"); tip != target {
		t.Fatalf("hosted main moved to %s, want %s", tip, target)
	}
}

// standingServeTargetPipelineRestore is the reviewer's reproduction: T
// adopts a pipeline definition that always fails, and H, built on T,
// restores B's definition and adds an ordinary src/ change. The runner
// passes H under the restored definition, and B..H shows only src/.
func standingServeTargetPipelineRestore(t *testing.T, g *serveGovernedFixture, approve bool) {
	t.Helper()
	original := serveGovernedPipelineJSON(t, g.CIFailFlag)
	failing := strings.Replace(original, "test ! -e", "exit 1; test ! -e", 1)
	if failing == original {
		t.Fatal("the failing pipeline definition equals the original")
	}
	target, p := standingServeTargetCase(t, g,
		map[string]string{standingServeTargetPipelinePath: failing},
		map[string]string{standingServeTargetPipelinePath: original, "src/restore.txt": "ordinary change\n"},
		approve, "src/restore.txt")
	standingServeTargetRefused(t, g, target, p,
		`path ".hn/pipelines/test.json" changed between the tip and the head is under .hn/`)
}

// Zero required approvals: a pipeline restore and a path outside the rule
// hidden from B..H are refused; a policy restore is refused by step 7a
// first; and a candidate on an older base whose T..H paths are all inside
// the rule still advances, with the pipeline definition at H equal to T's.
func TestStandingServeTargetPathsZeroApproval(t *testing.T) {
	g := newStandingServeFixture(t, standingZeroApprovalServePolicy)

	standingServeTargetPipelineRestore(t, g, false)

	outside, p := standingServeTargetCase(t, g,
		map[string]string{"other/x": "outside every rule path\n"},
		map[string]string{"other/x": serveGovernedRemove, "src/outside.txt": "ordinary change\n"},
		false, "src/outside.txt")
	standingServeTargetRefused(t, g, outside, p,
		`path "other/x" changed between the tip and the head is outside the rule allowlist`)

	// T amends the policy and H restores B's bytes. The destination policy
	// at T is not the base policy, so step 7a refuses before step 10.
	amended := standingServePolicyDocument()
	amended.RequiredApprovals = 0
	amended.Rules[1].Paths = []string{"src/", "tests/", "docs/", "other/"}
	amendment, p := standingServeTargetCase(t, g,
		map[string]string{serveGovernedPolicyPath: amended.JSON(g.actors)},
		map[string]string{serveGovernedPolicyPath: standingZeroApprovalServePolicy(g.actors), "src/policy.txt": "ordinary change\n"},
		false, "src/policy.txt")
	rejection, _ := standingServeRuleRefused(t, g, "E", p.ID, p.Head, "ordinary", serveReasonProtectedDestinationPolicy)
	requireServeIDs(t, rejection, p.ID)
	requireServeNext(t, rejection, "revise "+p.ID+" against "+amendment)
	if tip := g.ServerTip("main"); tip != amendment {
		t.Fatalf("hosted main moved to %s, want %s", tip, amendment)
	}

	target, p := standingServeTargetCase(t, g,
		map[string]string{"src/target.txt": "landed after the base\n"},
		map[string]string{"src/after.txt": "built on the target\n"},
		false, "src/after.txt", "src/target.txt")
	if at := g.git("A", "rev-parse", target+":"+standingServeTargetPipelinePath); at != g.git("A", "rev-parse", p.Head+":"+standingServeTargetPipelinePath) {
		t.Fatalf("pipeline definition at H differs from T's %s", at)
	}
	output, rejections, ok := g.RulePush("E", p.ID, p.Head, "ordinary")
	if !ok {
		t.Fatalf("rule advancement over T..H inside the rule was refused %+v:\n%s", rejections, output)
	}
	if tip := g.ServerTip("main"); tip != p.Head {
		t.Fatalf("hosted main = %s, want the candidate head %s", tip, p.Head)
	}
}

// One required approval: the same pipeline restore is refused although
// R approved the candidate.
func TestStandingServeTargetPathsOneApproval(t *testing.T) {
	g := newStandingServeFixture(t, standingServeDefaultPolicy)
	standingServeTargetPipelineRestore(t, g, true)
}

// With protectedChecks, a protected-check path that T added and H removes
// is refused at step 10, before the protected baseline of step 12.
func TestStandingServeTargetPathsProtectedCheck(t *testing.T) {
	g := newStandingServeFixture(t, func(actors map[string]string) string {
		policy := standingServePolicyDocument()
		policy.RequiredApprovals = 0
		policy.ProtectedPaths = []string{"scripts/"}
		return policy.JSON(actors)
	})
	target, p := standingServeTargetCase(t, g,
		map[string]string{"scripts/lint.sh": "exit 1\n"},
		map[string]string{"scripts/lint.sh": serveGovernedRemove, "src/protected.txt": "ordinary change\n"},
		false, "src/protected.txt")
	standingServeTargetRefused(t, g, target, p,
		`path "scripts/lint.sh" changed between the tip and the head is a protected check path`)
}
