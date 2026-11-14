package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSelectedAuthorPolicyRefusalOnCurrentTarget(t *testing.T) {
	g := selectedFixture(t)
	original := g.OpenProposal(g.Base, g.Commit(g.Base, map[string]string{"auto/policy-control": "control\n"}))
	check := func(id, target string) jitCheckView {
		t.Helper()
		g.hn("R", "sync", "origin")
		g.git("R", "fetch", "-q", "origin")
		raw := g.hn("R", "jit-check", id, "--target", target, "--requester", g.actors["R"], "--remote", "origin", "--rule", "auto", "--pipeline", "test")
		var envelope struct {
			Data jitCheckView `json:"data"`
		}
		if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
			t.Fatal(err)
		}
		return envelope.Data
	}
	if value := check(original.ID, g.Base); !value.Eligible {
		t.Fatalf("baseline policy positive control refused: %+v", value)
	}
	var policy map[string]any
	if err := json.Unmarshal([]byte(g.git("A", "show", g.Base+":.hn/policy.json")), &policy); err != nil {
		t.Fatal(err)
	}
	policy["automerge"].(map[string]any)["rules"].([]any)[0].(map[string]any)["authors"] = []string{g.actors["U"]}
	policy["pipelines"].(map[string]any)["test"].(map[string]any)["requesters"] = []string{g.actors["M"]}
	raw, _ := json.Marshal(policy)
	denied := g.Commit(g.Base, map[string]string{".hn/policy.json": string(raw) + "\n"})
	g.Host(denied)
	g.SetMainForTest(denied)
	value := check(original.ID, denied)
	reasons := strings.Join(value.Blockers, "\n")
	if value.Eligible || !strings.Contains(reasons, "author is not allowed") || !strings.Contains(reasons, "coordinator is not a pipeline requester") {
		t.Fatalf("current policy predicates absent: %+v", value)
	}
	current := g.OpenProposal(denied, g.Commit(denied, map[string]string{"auto/current-policy": "denied\n"}))
	d := newSelectedDriver(t, g)
	if out := d.author("A", current.ID); !strings.Contains(out, "needs-attention") || !strings.Contains(out, "author is not allowed") || !strings.Contains(out, "coordinator is not a pipeline requester") {
		t.Fatal("unchanged-target policy refusal absent: " + out)
	}
	d.step()
	if d.count("R", current.ID) != 0 || g.ServerTip("main") != denied {
		t.Fatal("denied current binding requested or advanced")
	}
	if value := check(current.ID, g.Base); value.Eligible || !strings.Contains(strings.Join(value.Blockers, "\n"), "author is not allowed") {
		t.Fatal("signed-base policy bypassed by permissive target")
	}
	t.Log("real serve: permissive baseline positive; current-target and signed-base author/requester predicates refuse; unchanged-target original receives no request or advancement")
}
