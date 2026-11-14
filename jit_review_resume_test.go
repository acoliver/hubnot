package main

import (
	"strings"
	"testing"
)

func TestSelectedAuthorResolvedReviewReusesOriginalAttempt(t *testing.T) {
	g := selectedFixture(t)
	d := newSelectedDriver(t, g)
	a := g.OpenProposal(g.Base, g.Commit(g.Base, map[string]string{"auto/review.txt": "review\n"}))
	d.author("A", a.ID)
	d.step()
	d.author("A", a.ID)
	d.step()
	g.act("M", "review.submit", "review", a.ID, "--request-changes", "--body", "Discuss this original before running")
	if out := d.step(); !strings.Contains(out, "needs-attention") {
		t.Fatal("coordinator ignored review: " + out)
	}
	d.worker()
	g.hn("M", "sync", "origin")
	if item := jitProposal(t, g, "M", a.ID); len(item.Requests) != 1 || len(item.Requests[0].Results) != 0 {
		t.Fatal("review-blocked work executed")
	}
	g.act("M", "review.submit", "review", a.ID, "--approve", "--body", "The original is now approved")
	if out := d.step(); !strings.Contains(out, "selected") {
		t.Fatal("resolved review left original permanently blocked: " + out)
	}
	d.author("A", a.ID)
	d.step()
	if d.count("R", a.ID) != 1 {
		t.Fatal("review recovery created a second gate attempt")
	}
	d.worker()
	d.step()
	d.step()
	if g.ServerTip("main") != a.Head {
		t.Fatal("resolved original failed to advance")
	}
	t.Log("real serve: signed review blocks admission execution; same-reviewer approval restores original progress using the same request")
}
