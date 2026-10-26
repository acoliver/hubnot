package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// hn automerge advances an hn serve protected branch only through the
// server. Plain --execute on a branch that tracks the serve remote is
// refused before it writes a local merge commit or merge fact; with
// --remote, the rule's checks run and the server advances main to the
// signed proposal head.
func TestServeAutomergeAdvancesThroughServer(t *testing.T) {
	g := newServeGovernedFixture(t, serveGovernedOptions{AutomergePaths: []string{"auto/"}})
	head := g.Commit(g.Base, map[string]string{"auto/change.txt": "automerged\n"})
	p := g.ReadyProposal(g.Base, head)

	// The maintainer's clone has main checked out, tracking origin.
	g.git(g.Maintainer, "checkout", "-q", "main")
	g.git(g.Maintainer, "reset", "-q", "--hard", "origin/main")
	g.hn(g.Maintainer, "sync", "origin")
	before := g.git(g.Maintainer, "rev-parse", "HEAD")

	output, status, err := g.advanceExec(g.Maintainer, nil, g.h.Binary, "automerge", p.Candidate.ID, "--rule", "auto", "--execute")
	if err != nil {
		t.Fatal(err)
	}
	if status == 0 || !strings.Contains(output, "tracks hn serve remote origin") ||
		!strings.Contains(output, "--execute --remote origin") {
		t.Fatalf("plain automerge --execute exited %d:\n%s", status, output)
	}
	if after := g.git(g.Maintainer, "rev-parse", "HEAD"); after != before {
		t.Fatalf("refused automerge moved local main from %s to %s", before, after)
	}
	if facts := g.MergeFacts(g.Maintainer, p.Candidate.ID); len(facts) != 0 {
		t.Fatalf("refused automerge recorded merge facts %+v", facts)
	}
	if tip := g.ServerTip("main"); tip != g.Base {
		t.Fatalf("hosted main = %s after the refusal, want %s", tip, g.Base)
	}

	output, status, err = g.advanceExec(g.Maintainer, nil, g.h.Binary, "automerge", p.Candidate.ID, "--rule", "auto", "--execute", "--remote", "origin")
	if err != nil {
		t.Fatal(err)
	}
	if status != 0 {
		t.Fatalf("automerge --execute --remote origin exited %d:\n%s", status, output)
	}
	if tip := g.ServerTip("main"); tip != head {
		t.Fatalf("hosted main = %s, want the signed head %s", tip, head)
	}
	g.requireFastForwardFact(g.Maintainer, p.Candidate.ID, head, p.Acceptance.ID)
}

// automergeServeFixture hosts a repository with automerge rule "auto" for
// auto/ paths and the maintainer's main checked out at the hosted tip.
func automergeServeFixture(t *testing.T, options serveGovernedOptions) *serveGovernedFixture {
	t.Helper()
	options.AutomergePaths = []string{"auto/"}
	g := newServeGovernedFixture(t, options)
	g.git(g.Maintainer, "checkout", "-q", "main")
	g.git(g.Maintainer, "reset", "-q", "--hard", "origin/main")
	return g
}

func (g *serveGovernedFixture) automerge(candidate string, extra ...string) (string, int) {
	g.t.Helper()
	args := append([]string{"automerge", candidate, "--rule", "auto", "--execute"}, extra...)
	output, status, err := g.advanceExec(g.Maintainer, nil, g.h.Binary, args...)
	if err != nil {
		g.t.Fatal(err)
	}
	return output, status
}

// A trusted change request that exists only on the server blocks the rule,
// although the ordinary approval quorum still holds and the server alone
// would advance.
func TestServeAutomergeSeesHostedChangeRequest(t *testing.T) {
	g := automergeServeFixture(t, serveGovernedOptions{RunnerReviews: true})
	head := g.Commit(g.Base, map[string]string{"auto/a.txt": "a\n"})
	p := g.ReadyProposal(g.Base, head)
	g.act(g.Runner, "review.submit", "review", p.Candidate.ID, "--request-changes", "--body", "Not yet.")

	output, status := g.automerge(p.Candidate.ID, "--remote", "origin")
	if status == 0 || !strings.Contains(output, "proposal has a current trusted change request") {
		t.Fatalf("automerge with a hosted change request exited %d:\n%s", status, output)
	}
	if tip := g.ServerTip("main"); tip != g.Base {
		t.Fatalf("hosted main = %s, want %s", tip, g.Base)
	}
}

// The advancement is bound to the hosted tip the rule was inspected
// against: when hosted main moved past the local branch, a candidate whose
// head happens to fast-forward the new tip is refused until the local
// branch is updated.
func TestServeAutomergeBindsInspectedTip(t *testing.T) {
	g := automergeServeFixture(t, serveGovernedOptions{})
	first := g.Commit(g.Base, map[string]string{"auto/first.txt": "1\n"})
	p1 := g.ReadyProposal(g.Base, first)
	g.hn(g.Maintainer, "merge", p1.Candidate.ID, "--remote", "origin")
	if tip := g.ServerTip("main"); tip != first {
		t.Fatalf("hosted main = %s, want %s", tip, first)
	}

	second := g.Commit(first, map[string]string{"auto/second.txt": "2\n"})
	p2 := g.ReadyProposal(g.Base, second)
	output, status := g.automerge(p2.Candidate.ID, "--remote", "origin")
	if status == 0 || !strings.Contains(output, "update the local branch to the hosted tip") {
		t.Fatalf("automerge against a moved hosted tip exited %d:\n%s", status, output)
	}
	if tip := g.ServerTip("main"); tip != first {
		t.Fatalf("hosted main = %s after the refusal, want %s", tip, first)
	}
}

// Plain --execute refuses when the target branch's upstream cannot be
// classified, so an unreachable hn serve remote never leads to a local
// merge commit or merge fact.
func TestServeAutomergeFailsClosedOnUnreachableUpstream(t *testing.T) {
	g := automergeServeFixture(t, serveGovernedOptions{})
	head := g.Commit(g.Base, map[string]string{"auto/a.txt": "a\n"})
	p := g.ReadyProposal(g.Base, head)
	g.hn(g.Maintainer, "sync", "origin")
	g.git(g.Maintainer, "remote", "add", "down", filepath.Join(t.TempDir(), "missing.git"))
	g.git(g.Maintainer, "config", "branch.main.remote", "down")
	before := g.git(g.Maintainer, "rev-parse", "HEAD")

	output, status := g.automerge(p.Candidate.ID)
	if status == 0 || !strings.Contains(output, "cannot tell whether down") {
		t.Fatalf("automerge with an unreachable upstream exited %d:\n%s", status, output)
	}
	if after := g.git(g.Maintainer, "rev-parse", "HEAD"); after != before {
		t.Fatalf("refused automerge moved local main from %s to %s", before, after)
	}
	if facts := g.MergeFacts(g.Maintainer, p.Candidate.ID); len(facts) != 0 {
		t.Fatalf("refused automerge recorded merge facts %+v", facts)
	}
}

// A saved selection that leaves out a trusted actor would hide that actor's
// veto from the inspection, so automerge --remote refuses it.
func TestServeAutomergeRequiresTrustedSelection(t *testing.T) {
	g := automergeServeFixture(t, serveGovernedOptions{RunnerReviews: true})
	head := g.Commit(g.Base, map[string]string{"auto/a.txt": "a\n"})
	p := g.ReadyProposal(g.Base, head)
	g.hn(g.Maintainer, "replication", "select", "origin", "--actor", g.actors[g.Maintainer], "--proposal", p.Candidate.ID)
	g.act(g.Runner, "review.submit", "review", p.Candidate.ID, "--request-changes", "--body", "Not yet.")

	output, status := g.automerge(p.Candidate.ID, "--remote", "origin")
	if status == 0 || !strings.Contains(output, "--all-trusted") {
		t.Fatalf("automerge with a narrow selection exited %d:\n%s", status, output)
	}
	if tip := g.ServerTip("main"); tip != g.Base {
		t.Fatalf("hosted main = %s, want %s", tip, g.Base)
	}

	g.hn(g.Maintainer, "replication", "select", "origin", "--all-trusted")
	output, status = g.automerge(p.Candidate.ID, "--remote", "origin")
	if status == 0 || !strings.Contains(output, "proposal has a current trusted change request") {
		t.Fatalf("automerge after --all-trusted exited %d:\n%s", status, output)
	}
	if facts := g.MergeFacts(g.Maintainer, p.Candidate.ID); len(facts) != 0 {
		t.Fatalf("refused automerge recorded merge facts %+v", facts)
	}
}
