package main

import (
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
