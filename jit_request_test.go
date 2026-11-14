package main

import (
	"crypto/sha256"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func jitRecordBinary(t *testing.T, g *serveGovernedFixture) {
	t.Helper()
	data, err := os.ReadFile(g.h.Binary)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("actual fixture binary: sha256=%x bytes=%d", sha256.Sum256(data), len(data))
}

func TestJITRunnerRequesterPolicyFence(t *testing.T) {
	g := newServeGovernedFixture(t, serveGovernedOptions{})
	jitRecordBinary(t, g)
	p := g.OpenProposal(g.Base, g.Commit(g.Base, map[string]string{"auto/a.txt": "a\n"}))
	unlisted := g.act("R", "run.request", "run", "request", p.ID, "test")
	g.hn("U", "runner", "once", "--remote", "origin", "--accept-pipeline", "test", "--accept-actor", g.actors["R"], "--require-requester-policy", "--backend", "host", "--allow-unsafe-host-execution")
	g.hn("M", "sync", "origin")
	item := jitProposal(t, g, "M", p.ID)
	if len(item.Requests) != 1 || len(item.Requests[0].Results) != 0 {
		t.Fatalf("strict single-signer worker executed policy-unlisted request %s: %+v", unlisted.ID, item.Requests)
	}
	// Legacy single-signer behavior is deliberately opt-out compatible.
	g.hn("U", "runner", "once", "--remote", "origin", "--accept-pipeline", "test", "--accept-actor", g.actors["R"], "--backend", "host", "--allow-unsafe-host-execution")
	g.hn("M", "sync", "origin")
	item = jitProposal(t, g, "M", p.ID)
	if len(item.Requests[0].Results) != 1 || len(item.Pipelines[0].Passed) != 0 {
		t.Fatalf("legacy execution/counting control changed: %+v", item)
	}
	listed := g.RequestCI(p.ID)
	g.hn("U", "runner", "once", "--remote", "origin", "--accept-pipeline", "test", "--accept-actor", g.actors["M"], "--require-requester-policy", "--backend", "host", "--allow-unsafe-host-execution")
	g.hn("M", "sync", "origin")
	item = jitProposal(t, g, "M", p.ID)
	if len(item.Pipelines[0].Passed) != 1 {
		t.Fatalf("strict worker did not serve authorized request: %+v", item.Pipelines)
	}
	t.Logf("actual serve: strict worker ignored unlisted %s; legacy result remained non-counting; authorized %s passed", unlisted.ID, listed.ID)
}

func jitProposal(t *testing.T, g *serveGovernedFixture, who, id string) proposalListJSON {
	t.Helper()
	var list struct {
		Data struct {
			Items []proposalListJSON `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(g.hn(who, "proposal", "list", "--json")), &list); err != nil {
		t.Fatal(err)
	}
	for _, item := range list.Data.Items {
		if item.ID == id {
			if item.Error != "" {
				t.Fatal(item.Error)
			}
			return item
		}
	}
	t.Fatalf("proposal %s absent from successful list", id)
	return proposalListJSON{}
}

func TestJITRequestReuseSurvivesRestartAndFailure(t *testing.T) {
	g := newServeGovernedFixture(t, serveGovernedOptions{})
	jitRecordBinary(t, g)
	head := g.Commit(g.Base, map[string]string{"auto/a.txt": "a\n"})
	p := g.OpenProposal(g.Base, head)
	q := g.RequestCI(p.ID)
	// Each invocation is a new CLI process; no in-memory coordinator state.
	g.hn(g.Maintainer, "run", "request", p.ID, "test", "--reuse")
	item := jitProposal(t, g, g.Maintainer, p.ID)
	if len(item.Requests) != 1 || item.Requests[0].ID != q.ID {
		t.Fatalf("restart appended a duplicate request: got %+v, want only %s", item.Requests, q.ID)
	}
	if err := os.WriteFile(g.CIFailFlag, []byte("fail\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := g.RunCI(q.ID)
	g.hn(g.Maintainer, "sync", "origin")
	g.hn(g.Maintainer, "run", "request", p.ID, "test", "--reuse")
	g.hn(g.Maintainer, "sync", "origin")
	item = jitProposal(t, g, g.Maintainer, p.ID)
	if len(item.Requests) != 1 || len(item.Pipelines) != 1 || len(item.Pipelines[0].Failed) != 1 {
		t.Fatalf("reuse lost failure or implicitly reran: %+v", item)
	}
	// A different signer must create its own request, not adopt the coordinator's.
	g.hn(g.Author, "sync", "origin")
	g.hn(g.Author, "run", "request", p.ID, "test", "--reuse")
	other := g.latest(g.Author)
	if other.Kind != "run.request" || other.ID == q.ID {
		t.Fatalf("other signer did not create its own request: %+v", other)
	}
	g.hn(g.Author, "sync", "origin")
	g.hn(g.Maintainer, "sync", "origin")
	// Explicit request retains its pre-existing meaning; reuse now chooses it.
	explicit := g.RequestCI(p.ID)
	g.hn(g.Maintainer, "run", "request", p.ID, "test", "--reuse")
	if got := g.latest(g.Maintainer); got.ID != explicit.ID {
		t.Fatalf("reuse appended after explicit request: %+v", got)
	}
	// A new full revision with an identical head/definition still needs a request.
	revision := g.Revise(p.ID, g.Base, head)
	g.hn(g.Maintainer, "sync", "origin")
	g.hn(g.Maintainer, "run", "request", revision.ID, "test", "--reuse")
	newRequest := g.latest(g.Maintainer)
	if newRequest.Kind != "run.request" || newRequest.Subject != revision.ID || newRequest.ID == explicit.ID {
		t.Fatalf("request crossed proposal revisions: %+v", newRequest)
	}
	g.git("M", "update-ref", "refs/hn/proposals/"+strings.TrimPrefix(revision.ID, "sha256:"), g.Base)
	output, status := g.clients["M"].exec(g.work["M"], g.h.Binary, "run", "request", revision.ID, "test", "--reuse")
	if status == 0 || !strings.Contains(output, "code ref") {
		t.Fatalf("reuse bypassed conflicting-code guard: status=%d %s", status, output)
	}
	if got := g.latest("M"); got.ID != newRequest.ID {
		t.Fatalf("failed reuse mutated request history: %+v", got)
	}
	t.Logf("actual serve: request %s retained after failed result %s; explicit request %s retained; revision %s received %s", q.ID, result.ID, explicit.ID, revision.ID, newRequest.ID)
}

func TestJITServeCoordinatorAuthorityBoundary(t *testing.T) {
	g := newServeGovernedFixture(t, serveGovernedOptions{AutomergePaths: []string{"auto/"}})
	jitRecordBinary(t, g)
	// Establish a synthetic base policy before opening any work. R is a listed
	// non-maintainer coordinator; A and U are independent authors.
	var policy map[string]any
	if err := json.Unmarshal([]byte(g.Policy.JSON(t)), &policy); err != nil {
		t.Fatal(err)
	}
	policy["pipelines"].(map[string]any)["test"].(map[string]any)["requesters"] = []string{g.actors["R"], g.actors["A"], g.actors["U"]}
	policy["proposals"].(map[string]any)["trustedReviewers"] = []string{g.actors["M"]}
	policy["proposals"].(map[string]any)["requiredApprovals"] = 0
	rule := policy["automerge"].(map[string]any)["rules"].([]any)[0].(map[string]any)
	rule["acceptance"] = "rule"
	rule["actors"] = []string{g.actors["R"]}
	rule["authors"] = []string{g.actors["R"], g.actors["A"], g.actors["U"]}
	encoded, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	base := g.Commit(g.Base, map[string]string{".hn/policy.json": string(encoded) + "\n"})
	g.Host(base)
	g.SetMainForTest(base)
	g.Base = base
	for _, who := range []string{"A", "M", "R", "U"} {
		g.git(who, "fetch", "-q", "origin")
		g.git(who, "checkout", "-q", "-B", "main", "origin/main")
		g.hn(who, "replication", "select", "origin", "--all-trusted")
	}
	p1 := g.OpenProposal(base, g.Commit(base, map[string]string{"auto/a.txt": "a\n"}))
	g.Author = "U"
	p2 := g.OpenProposal(base, g.Commit(base, map[string]string{"auto/b.txt": "b\n"}))
	g.Author = "A"
	q1 := g.act("R", "run.request", "run", "request", p1.ID, "test")
	// A legacy producer can publish its request, but a queue-only worker ignores it.
	legacy := g.act("U", "run.request", "run", "request", p2.ID, "test")
	g.hn("U", "runner", "once", "--remote", "origin", "--accept-pipeline", "test", "--accept-actor", g.actors["R"], "--backend", "host", "--allow-unsafe-host-execution")
	g.hn("M", "sync", "origin")
	first := jitProposal(t, g, "M", p1.ID)
	second := jitProposal(t, g, "M", p2.ID)
	if len(first.Pipelines[0].Passed) != 1 || len(second.Pipelines[0].Passed) != 0 || len(second.Requests[0].Results) != 0 {
		t.Fatalf("coordinator authorization/fence failed: first=%+v second=%+v", first.Pipelines, second.Requests)
	}
	// The coordinator cannot revise either author's signed lineage.
	output, status := g.clients["R"].exec(g.work["R"], g.h.Binary, "proposal", "revise", p1.ID, "--base", base, "--head", p1.Head)
	if status == 0 || !strings.Contains(output, "not the author") {
		t.Fatalf("cross-author revise status=%d: %s", status, output)
	}
	// A fresh source with the same code has no gate evidence of its own.
	ungated := g.OpenProposal(base, p1.Head)
	g.act("M", "review.submit", "review", ungated.ID, "--approve")
	// The current queue names the source in prose, with no signed relationship.
	combined := g.act("R", "proposal.open", "proposal", "open", "--base", base, "--head", ungated.Head, "--body", "Queue source: "+ungated.ID, "Combined queue candidate")
	g.hn("M", "sync", "origin")
	if item := jitProposal(t, g, "M", combined.ID); len(item.Approvals) != 0 || item.Lineage.Root == ungated.ID {
		t.Fatalf("independent candidate unexpectedly inherited original review: %+v", item)
	}
	g.Reject(ungated.ID)
	g.hn("R", "sync", "origin")
	original := jitProposal(t, g, "R", ungated.ID)
	queue := jitProposal(t, g, "R", combined.ID)
	if !original.Rejected || len(original.Requests) != 0 || queue.Rejected {
		t.Fatalf("original veto boundary changed: original=%t combined=%t", original.Rejected, queue.Rejected)
	}
	// A second admitted request eventually runs under the same worker fence.
	q2 := g.act("R", "run.request", "run", "request", p2.ID, "test")
	g.hn("U", "runner", "once", "--remote", "origin", "--accept-pipeline", "test", "--accept-actor", g.actors["R"], "--backend", "host", "--allow-unsafe-host-execution")
	g.hn("M", "sync", "origin")
	if item := jitProposal(t, g, "M", p2.ID); len(item.Pipelines[0].Passed) != 1 {
		t.Fatalf("second admitted request did not run: %+v", item.Pipelines)
	}
	combinedRequest := g.act("R", "run.request", "run", "request", combined.ID, "test")
	g.hn("U", "runner", "once", "--remote", "origin", "--accept-pipeline", "test", "--accept-actor", g.actors["R"], "--backend", "host", "--allow-unsafe-host-execution")
	g.hn("R", "sync", "origin")
	g.git("R", "checkout", "-q", "main")
	g.git("R", "reset", "-q", "--hard", "origin/main")
	g.hn("R", "automerge", combined.ID, "--rule", "auto", "--execute", "--remote", "origin")
	if tip := g.ServerTip("main"); tip != p1.Head {
		t.Fatalf("combined merge control did not reach server: got %s want %s", tip, p1.Head)
	}
	t.Logf("actual serve advanced combined candidate on gate %s while ungated original %s remained rejected", combinedRequest.ID, ungated.ID)
	t.Logf("actual serve: non-maintainer cross-author %s and %s passed; legacy %s fenced; combined %s does not inherit rejection of %s", q1.ID, q2.ID, legacy.ID, combined.ID, ungated.ID)
}
