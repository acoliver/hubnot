package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// WP10: hn merge PROPOSAL --remote REMOTE [--branch BRANCH]. The black-box
// tests drive real maintainers, reviewers and runners through the SSH shim
// with the governed fixture of serve_protected_test.go and check the nine
// steps of contracts/advancement.md, the interruption table (FR-015), the
// unchanged local merge (FR-016), a fresh identity-less verifier (NFR-006)
// and the advancement race (NFR-005).

// advanceExec runs program in who's clone with the client environment plus
// env and returns the combined output and exit status. It never calls
// t.Fatal, so goroutines may use it.
func (g *serveGovernedFixture) advanceExec(who string, env []string, program string, args ...string) (string, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, program, args...)
	command.Dir = g.work[who]
	command.Env = append(slices.Clone(g.clients[who].Env), env...)
	command.Stdin = strings.NewReader("")
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		return string(output), -1, ctx.Err()
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return string(output), exitErr.ExitCode(), nil
	}
	if err != nil {
		return string(output), -1, err
	}
	return string(output), 0, nil
}

// Advance runs hn merge candidate --remote origin in who's clone with extra
// environment entries and returns the output and exit status.
func (g *serveGovernedFixture) Advance(who, candidate string, env ...string) (string, int) {
	g.t.Helper()
	output, status, err := g.advanceExec(who, env, g.h.Binary, "merge", candidate, "--remote", "origin")
	if err != nil {
		g.t.Fatalf("hn merge %s --remote origin: %v\n%s", candidate, err, output)
	}
	return output, status
}

// AdvanceInterrupted runs the advancement with the test-only interruption
// after step and requires the documented interruption failure.
func (g *serveGovernedFixture) AdvanceInterrupted(who, candidate, step string) {
	g.t.Helper()
	output, status := g.Advance(who, candidate, "HN_INTERNAL_TESTING=1", serveAdvanceInterruptEnv+"="+step)
	if status != 1 || !strings.HasSuffix(output, "hn: interrupted for testing after "+step+"\n") {
		g.t.Fatalf("interrupted advancement after %s exited %d:\n%s", step, status, output)
	}
}

// Published reports whether who's actor chain on the server equals the
// local one.
func (g *serveGovernedFixture) Published(who string) bool {
	g.t.Helper()
	ref := g.actorRef(who)
	return g.hostedRef(ref) == g.localHead(who, ref)
}

// serveAdvanceEvents decodes every event in the local and accepted actor
// chains of the clone at dir, keyed by event ID.
func serveAdvanceEvents(t *testing.T, dir string) map[string]operationalEvent {
	t.Helper()
	var commits []string
	seen := map[string]bool{}
	for _, ref := range strings.Fields(runOperationalGit(t, dir, "for-each-ref", "--format=%(refname)", "refs/hn/actors", "refs/hn/remotes")) {
		if strings.HasPrefix(ref, "refs/hn/remotes/") && !strings.Contains(ref, "/actors/") {
			continue
		}
		for _, commit := range strings.Fields(runOperationalGit(t, dir, "rev-list", ref)) {
			if !seen[commit] {
				seen[commit] = true
				commits = append(commits, commit)
			}
		}
	}
	events := map[string]operationalEvent{}
	if len(commits) == 0 {
		return events
	}
	var input bytes.Buffer
	for _, commit := range commits {
		input.WriteString(commit + ":event.json\n")
	}
	command := exec.Command("git", "cat-file", "--batch")
	command.Dir = dir
	command.Stdin = &input
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git cat-file --batch in %s: %v", dir, err)
	}
	reader := bufio.NewReader(bytes.NewReader(output))
	for range commits {
		header, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		var oid, kind string
		var size int
		if _, err := fmt.Sscanf(header, "%s %s %d", &oid, &kind, &size); err != nil || kind != "blob" {
			t.Fatalf("unexpected cat-file header %q", header)
		}
		payload := make([]byte, size+1)
		if _, err := io.ReadFull(reader, payload); err != nil {
			t.Fatal(err)
		}
		payload = payload[:size]
		var event operationalEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			t.Fatalf("decode event: %v\n%s", err, payload)
		}
		event.ID = operationalDigest(payload)
		events[event.ID] = event
	}
	return events
}

// serveAdvanceEvidence lists the governance evidence for candidate by kind:
// approvals, run requests, run results, decisions and merge facts.
func serveAdvanceEvidence(events map[string]operationalEvent, candidate string) map[string][]string {
	evidence := map[string][]string{}
	requests := map[string]bool{}
	for id, event := range events {
		if event.Kind == "run.request" && event.Subject == candidate {
			requests[id] = true
		}
	}
	for id, event := range events {
		if event.Subject == candidate || (event.Kind == "run.result" && requests[event.Subject]) {
			evidence[event.Kind] = append(evidence[event.Kind], id)
		}
	}
	for kind := range evidence {
		slices.Sort(evidence[kind])
	}
	return evidence
}

// MergeFacts are the proposal.merged events for candidate in who's clone.
func (g *serveGovernedFixture) MergeFacts(who, candidate string) []operationalEvent {
	g.t.Helper()
	var facts []operationalEvent
	for _, event := range serveAdvanceEvents(g.t, g.work[who]) {
		if event.Kind == "proposal.merged" && event.Subject == candidate {
			facts = append(facts, event)
		}
	}
	return facts
}

// requireFastForwardFact checks the single merge fact for candidate in who's
// clone: commit equals head, and evidence names the acceptances.
func (g *serveGovernedFixture) requireFastForwardFact(who, candidate, head string, acceptances ...string) operationalEvent {
	g.t.Helper()
	facts := g.MergeFacts(who, candidate)
	if len(facts) != 1 {
		g.t.Fatalf("%s's clone holds %d merge facts for %s, want 1: %+v", who, len(facts), candidate, facts)
	}
	fact := facts[0]
	slices.Sort(acceptances)
	if fact.Head != head || fact.Commit != head || fact.Actor != g.actors[who] || !slices.Equal(fact.Evidence, acceptances) {
		g.t.Fatalf("merge fact %+v, want head = commit = %s by %s citing %q", fact, head, who, acceptances)
	}
	return fact
}

// FreshVerifier clones the hosted repository with a new bound key and no
// Hubnot identity, selects every advertised fact, syncs, and returns the
// clone and hn proposal status for candidate (NFR-006).
func (g *serveGovernedFixture) FreshVerifier(seed, candidate string) (string, string) {
	g.t.Helper()
	key := serveHarnessAddKey(g.h, serveHarnessActor("advance-verifier-"+seed))
	client := serveHarnessClient(g.t, g.h, key, nil)
	dir := filepath.Join(g.t.TempDir(), "verifier-"+seed)
	client.Git("", "clone", "-q", serveHarnessCloneURL("r"), dir)
	if refs := runOperationalGit(g.t, dir, "for-each-ref", "refs/hn/actors"); strings.TrimSpace(refs) != "" {
		g.t.Fatalf("fresh verifier already holds a local actor chain:\n%s", refs)
	}
	client.Hn(dir, "replication", "select", "origin", "--all")
	client.Hn(dir, "sync", "origin")
	return dir, client.Hn(dir, "proposal", "status", candidate)
}

// serveAdvanceSuccessLines are the two lines of a full advancement.
func serveAdvanceSuccessLines(remote, head, candidate, merge string, decisions int) string {
	return fmt.Sprintf("Advanced %s refs/heads/main to %s for proposal %s\nRecorded merge event %s with %d acceptance decision(s)\n",
		remote, shortOID(head), shortID(candidate), shortID(merge), decisions)
}

// serveAdvanceRepairLines are the two lines of the step 5 repair path.
func serveAdvanceRepairLines(remote, head, candidate, merge string, decisions int) string {
	return fmt.Sprintf("%s refs/heads/main already contains %s for proposal %s\nRecorded missing merge event %s with %d acceptance decision(s)\n",
		remote, shortOID(head), shortID(candidate), shortID(merge), decisions)
}

func serveAdvanceAlreadyLine(candidate string) string {
	return "Proposal " + shortID(candidate) + " is already advanced on origin refs/heads/main and recorded as merged\n"
}

// serveAdvanceLocalState is who's branches, tags, HEAD and worktree status,
// which an advancement must leave unchanged (C-005).
func (g *serveGovernedFixture) serveAdvanceLocalState(who string) string {
	g.t.Helper()
	return g.git(who, "for-each-ref", "--format=%(objectname) %(refname)", "refs/heads", "refs/tags") + "\n" +
		g.git(who, "rev-parse", "HEAD") + "\n" + g.git(who, "status", "--porcelain")
}

// requireOneRejection parses the single hn-serve/1 rejection hn printed.
func requireOneRejection(t *testing.T, output string) serveRejection {
	t.Helper()
	rejections, lines := extractServeRejections(output)
	if len(rejections) != 1 || len(lines) != 2 {
		t.Fatalf("want one rejection line pair, got %d rejections in %d lines:\n%s", len(rejections), len(lines), output)
	}
	if strings.Contains(output, "remote: hn-serve") {
		t.Fatalf("hn printed Git's remote: prefix:\n%s", output)
	}
	return rejections[0]
}

// serveAdvanceTwoMaintainers sets main to a commit whose policy lists M and
// R as maintainers and returns it, so R can act as the second maintainer.
func serveAdvanceTwoMaintainers(g *serveGovernedFixture) string {
	g.t.Helper()
	policy := g.Policy
	policy.Maintainers = []string{g.actors["M"], g.actors["R"]}
	base := g.Commit(g.Base, map[string]string{serveGovernedPolicyPath: policy.JSON(g.t)})
	g.SetMainForTest(base)
	return base
}

// US3-1, US3-2, NFR-006 and C-005: M advances main to the tested head
// without a new commit, records commit == head, and a fresh clone with no
// identity reproduces the review, CI, acceptance and merge evidence.
func TestServeAdvanceFastForwardsAndVerifies(t *testing.T) {
	g := newServeGovernedFixture(t, serveGovernedOptions{})
	head := g.Commit(g.Base, nil)
	p := g.ReadyProposal(g.Base, head)
	commits := serveHarnessGit(t, g.hosted, "rev-list", "--count", head)
	local := g.serveAdvanceLocalState("M")
	logged := len(g.pushLog())

	output := g.hn("M", "merge", p.Candidate.ID, "--remote", "origin")

	fact := g.requireFastForwardFact("M", p.Candidate.ID, head, p.Acceptance.ID)
	if want := serveAdvanceSuccessLines("origin", head, p.Candidate.ID, fact.ID, 1); output != want {
		t.Fatalf("output:\n%s\nwant:\n%s", output, want)
	}
	if tip := g.ServerTip("main"); tip != head {
		t.Fatalf("hosted main = %s, want the tested head %s", tip, head)
	}
	if got := serveHarnessGit(t, g.hosted, "rev-list", "--count", serveGovernedMain); got != commits {
		t.Fatalf("main reaches %s commits and the head %s; a commit was created", got, commits)
	}
	if after := g.serveAdvanceLocalState("M"); after != local {
		t.Fatalf("advancement changed the local checkout:\nbefore\n%s\nafter\n%s", local, after)
	}
	if !g.Published("M") {
		t.Fatal("the merge fact was not published")
	}
	// The fact push, the advancement and the merge fact push.
	entries := g.pushLog()[logged:]
	if len(entries) != 2 {
		t.Fatalf("push log gained %d entries, want the advancement and the merge fact: %+v", len(entries), entries)
	}
	if entries[0].Verdict != servePushVerdictAccepted || entries[0].Actor != g.actors["M"] {
		t.Fatalf("advancement entry %+v", entries[0])
	}

	dir, status := g.FreshVerifier("us3-2", p.Candidate.ID)
	assertOperationalContains(t, status, "Status:   merged", "Code:     ✓ signed head available and matched", "Decision: 1/1 accepts; 0 rejects")
	want := serveAdvanceEvidence(serveAdvanceEvents(t, g.work["M"]), p.Candidate.ID)
	got := serveAdvanceEvidence(serveAdvanceEvents(t, dir), p.Candidate.ID)
	for kind, ids := range map[string][]string{
		"review.submit":     {p.Approval.ID},
		"run.request":       {p.Request.ID},
		"run.result":        {p.Result.ID},
		"proposal.decision": {p.Acceptance.ID},
		"proposal.merged":   {fact.ID},
	} {
		if !slices.Equal(want[kind], ids) || !slices.Equal(got[kind], ids) {
			t.Fatalf("%s: maintainer %q verifier %q, want %q", kind, want[kind], got[kind], ids)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("verifier evidence %q, maintainer evidence %q", got, want)
	}
}

// US3-3: main moved after the proposal was opened. The server names the
// current tip and directs a revision; main does not move. The tip is not
// in M's clone, so step 4 fetches it.
func TestServeAdvanceRefusesNotFastForward(t *testing.T) {
	g := newServeGovernedFixture(t, serveGovernedOptions{})
	head := g.Commit(g.Base, nil)
	p := g.ReadyProposal(g.Base, head)
	other := g.Commit(g.Base, nil)
	g.SetMainForTest(other)

	output, status := g.Advance("M", p.Candidate.ID)
	if status != 1 {
		t.Fatalf("advancement exited %d:\n%s", status, output)
	}
	rejection := requireOneRejection(t, output)
	if rejection.Code != serveReasonProtectedNotFastForward || rejection.Actor != g.actors["M"] {
		t.Fatalf("rejection %+v, want M's protected-not-fast-forward", rejection)
	}
	requireServeIDs(t, rejection, other, head)
	requireServeNext(t, rejection, "revise the proposal onto "+other+" with hn proposal revise")
	assertOperationalContains(t, output, "hn: advancement step 6 (request advancement) failed: origin refused to advance refs/heads/main")
	if tip := g.ServerTip("main"); tip != other {
		t.Fatalf("hosted main moved to %s", tip)
	}
	if fetched := g.git("M", "rev-parse", "refs/remotes/origin/main"); fetched != other {
		t.Fatalf("step 4 fetched %s into refs/remotes/origin/main, want %s", fetched, other)
	}
	if facts := g.MergeFacts("M", p.Candidate.ID); len(facts) != 0 {
		t.Fatalf("a refused advancement recorded %d merge facts", len(facts))
	}
}

// US3-4 through the command: the destination policy needs two approvals.
func TestServeAdvanceRefusesDestinationPolicy(t *testing.T) {
	g := newServeGovernedFixture(t, serveGovernedOptions{})
	policy := g.Policy
	policy.Reviewers = []string{g.actors[g.Reviewer], g.actors[g.Maintainer]}
	policy.RequiredApprovals = 2
	destination := g.Commit(g.Base, map[string]string{serveGovernedPolicyPath: policy.JSON(t)})
	head := g.Commit(destination, nil)
	p := g.ReadyProposal(g.Base, head)
	g.SetMainForTest(destination)

	output, status := g.Advance("M", p.Candidate.ID)
	if status != 1 {
		t.Fatalf("advancement exited %d:\n%s", status, output)
	}
	rejection := requireOneRejection(t, output)
	if rejection.Code != serveReasonProtectedDestinationPolicy {
		t.Fatalf("rejection %+v, want protected-destination-policy", rejection)
	}
	requireServeMissing(t, rejection, serveEvidenceReview)
	requireServeSentence(t, rejection, "destination policy requires 2 approvals; 1 trusted approval found")
	if tip := g.ServerTip("main"); tip != destination {
		t.Fatalf("hosted main moved to %s", tip)
	}
	if facts := g.MergeFacts("M", p.Candidate.ID); len(facts) != 0 {
		t.Fatalf("a refused advancement recorded %d merge facts", len(facts))
	}
}

// advance-not-maintainer: the author is stopped at step 1 locally, before
// any push. The server check holds for a direct hn-advance push.
func TestServeAdvanceNeedsMaintainer(t *testing.T) {
	g := newServeGovernedFixture(t, serveGovernedOptions{})
	head := g.Commit(g.Base, nil)
	p := g.ReadyProposal(g.Base, head)
	g.hn("A", "sync", "origin")
	logged := len(g.pushLog())

	output, status := g.Advance("A", p.Candidate.ID)
	if status != 1 {
		t.Fatalf("advancement by the author exited %d:\n%s", status, output)
	}
	want := "hn: advancement step 1 (validate locally) failed: identity " + shortID(g.actors["A"]) + " is not a maintainer under proposal policy\n"
	if output != want {
		t.Fatalf("output %q, want %q", output, want)
	}
	if entries := g.pushLog(); len(entries) != logged {
		t.Fatalf("a locally refused advancement pushed: %+v", entries[logged:])
	}

	rejection := g.RejectMain("A", head, serveReasonAdvanceNotMaintainer, "hn-advance="+p.Candidate.ID, "hn-expected-tip="+g.Base)
	requireServeIDs(t, rejection, p.Candidate.ID)
	if tip := g.ServerTip("main"); tip != g.Base {
		t.Fatalf("hosted main moved to %s", tip)
	}
}

// Step 2: an ordinary bare remote is refused before anything is written.
func TestServeAdvanceRefusesOrdinaryRemote(t *testing.T) {
	g := newServeGovernedFixture(t, serveGovernedOptions{})
	head := g.Commit(g.Base, nil)
	p := g.ReadyProposal(g.Base, head)
	plain := filepath.Join(t.TempDir(), "plain.git")
	runOperationalGit(t, "", "init", "-q", "--bare", plain)
	g.git("M", "remote", "add", "plain", plain)
	g.git("M", "push", "-q", "plain", "main")
	plainRefs := serveHarnessGit(t, plain, "for-each-ref", "--format=%(objectname) %(refname)")
	localRefs := g.git("M", "for-each-ref", "--format=%(objectname) %(refname)")

	output, status, err := g.advanceExec("M", nil, g.h.Binary, "merge", p.Candidate.ID, "--remote", "plain")
	if err != nil || status != 1 {
		t.Fatalf("advancement against an ordinary remote exited %d (%v):\n%s", status, err, output)
	}
	want := "hn: remote plain is not an hn serve remote; use hn merge " + p.Candidate.ID + " in a checkout of the target branch\n"
	if output != want {
		t.Fatalf("output %q, want %q", output, want)
	}
	if got := serveHarnessGit(t, plain, "for-each-ref", "--format=%(objectname) %(refname)"); got != plainRefs {
		t.Fatalf("the ordinary remote changed:\nbefore\n%s\nafter\n%s", plainRefs, got)
	}
	if got := g.git("M", "for-each-ref", "--format=%(objectname) %(refname)"); got != localRefs {
		t.Fatalf("M's refs changed:\nbefore\n%s\nafter\n%s", localRefs, got)
	}
}

// FR-015: the interruption table of contracts/advancement.md, row by row.
// Each row interrupts a fresh candidate on the current tip after one step,
// checks the server branch, M's merge facts and publication, then reruns
// the same command without the variable and requires exactly one merge
// fact. "Step 6, before the server applied the update" leaves the same
// state as an interruption after step 3 (branch unchanged, no fact, facts
// published), so the publish row covers both; a refused step 6 is covered
// by the refusal tests above.
func TestServeAdvanceInterruptionTable(t *testing.T) {
	g := newServeGovernedFixture(t, serveGovernedOptions{})
	for _, row := range []struct {
		step      string
		advanced  bool
		facts     int
		published bool
		rerun     string // full, repair or already
	}{
		{step: "publish", advanced: false, facts: 0, published: true, rerun: "full"},
		{step: "push", advanced: true, facts: 0, published: true, rerun: "repair"},
		{step: "record", advanced: true, facts: 1, published: false, rerun: "already"},
		{step: "republish", advanced: true, facts: 1, published: true, rerun: "already"},
	} {
		t.Run(row.step, func(t *testing.T) {
			tip := g.ServerTip("main")
			head := g.Commit(tip, nil)
			p := g.ReadyProposal(tip, head)

			g.AdvanceInterrupted("M", p.Candidate.ID, row.step)
			wantTip := tip
			if row.advanced {
				wantTip = head
			}
			if got := g.ServerTip("main"); got != wantTip {
				t.Fatalf("after the interruption main = %s, want %s", got, wantTip)
			}
			if facts := g.MergeFacts("M", p.Candidate.ID); len(facts) != row.facts {
				t.Fatalf("after the interruption M holds %d merge facts, want %d", len(facts), row.facts)
			}
			if published := g.Published("M"); published != row.published {
				t.Fatalf("after the interruption published = %t, want %t", published, row.published)
			}

			output := g.hn("M", "merge", p.Candidate.ID, "--remote", "origin")
			fact := g.requireFastForwardFact("M", p.Candidate.ID, head, p.Acceptance.ID)
			var want string
			switch row.rerun {
			case "full":
				want = serveAdvanceSuccessLines("origin", head, p.Candidate.ID, fact.ID, 1)
			case "repair":
				want = serveAdvanceRepairLines("origin", head, p.Candidate.ID, fact.ID, 1)
			default:
				want = serveAdvanceAlreadyLine(p.Candidate.ID)
			}
			if output != want {
				t.Fatalf("rerun output:\n%s\nwant:\n%s", output, want)
			}
			if got := g.ServerTip("main"); got != head {
				t.Fatalf("after the rerun main = %s, want %s", got, head)
			}
			if !g.Published("M") {
				t.Fatal("after the rerun the merge fact is not published")
			}
			// Any further rerun changes nothing.
			if again := g.hn("M", "merge", p.Candidate.ID, "--remote", "origin"); again != serveAdvanceAlreadyLine(p.Candidate.ID) {
				t.Fatalf("second rerun output %q", again)
			}
			g.requireFastForwardFact("M", p.Candidate.ID, head, p.Acceptance.ID)
		})
	}
}

// Steps 3 and 9 refused by the server: the step 3 refusal shows the
// server's hn-serve/1 lines and leaves main alone; a refused step 9 keeps
// the fact local and says how to publish it.
func TestServeAdvancePublicationRefusals(t *testing.T) {
	g := newServeGovernedFixture(t, serveGovernedOptions{})
	head := g.Commit(g.Base, nil)
	p := g.ReadyProposal(g.Base, head)
	ref := g.actorRef("M")
	hosted := g.git("M", "rev-list", "--count", ref)

	// Step 3: an unpublished local event exceeds max-events.
	g.h.Serve("serve", "repo", "set", "r", "--max-events", hosted)
	g.hn("M", "issue", "open", "Unpublished local event")
	output, status := g.Advance("M", p.Candidate.ID)
	if status != 1 {
		t.Fatalf("advancement exited %d:\n%s", status, output)
	}
	rejection := requireOneRejection(t, output)
	if rejection.Ref != ref || rejection.Code != serveReasonOverBudget {
		t.Fatalf("rejection %+v, want over-budget on %s", rejection, ref)
	}
	assertOperationalContains(t, output, "hn: advancement step 3 (publish facts) failed: replication actor publication failed for remote origin")
	if tip := g.ServerTip("main"); tip != g.Base {
		t.Fatalf("hosted main moved to %s", tip)
	}

	// Step 9: step 3 has nothing new to push, then the merge fact exceeds
	// the budget.
	local := g.git("M", "rev-list", "--count", ref)
	g.h.Serve("serve", "repo", "set", "r", "--max-events", local)
	output, status = g.Advance("M", p.Candidate.ID)
	if status != 1 || !strings.HasSuffix(output, "hn: merge fact recorded locally; run hn sync origin to publish it\n") {
		t.Fatalf("advancement exited %d:\n%s", status, output)
	}
	fact := g.requireFastForwardFact("M", p.Candidate.ID, head, p.Acceptance.ID)
	assertOperationalContains(t, output, serveAdvanceSuccessLines("origin", head, p.Candidate.ID, fact.ID, 1), "code=over-budget")
	if tip := g.ServerTip("main"); tip != head || g.Published("M") {
		t.Fatalf("hosted main = %s, published = %t; want the head and an unpublished fact", tip, g.Published("M"))
	}

	g.h.Serve("serve", "repo", "set", "r", "--max-events", "10000")
	g.hn("M", "sync", "origin")
	if !g.Published("M") {
		t.Fatal("hn sync did not publish the merge fact")
	}
	if output := g.hn("M", "merge", p.Candidate.ID, "--remote", "origin"); output != serveAdvanceAlreadyLine(p.Candidate.ID) {
		t.Fatalf("rerun output %q", output)
	}
}

// A second maintainer runs the command after the first one advanced. R's
// clone has not seen M's merge fact, so R records its own through the
// step 5 path; a verifier sees merged and no merge conflict.
func TestServeAdvanceSecondMaintainer(t *testing.T) {
	g := newServeGovernedFixture(t, serveGovernedOptions{})
	base := serveAdvanceTwoMaintainers(g)
	head := g.Commit(base, nil)
	p := g.ReadyProposal(base, head)
	g.hn("R", "sync", "origin")

	first := g.hn("M", "merge", p.Candidate.ID, "--remote", "origin")
	mFact := g.requireFastForwardFact("M", p.Candidate.ID, head, p.Acceptance.ID)
	if first != serveAdvanceSuccessLines("origin", head, p.Candidate.ID, mFact.ID, 1) {
		t.Fatalf("M's output:\n%s", first)
	}

	second := g.hn("R", "merge", p.Candidate.ID, "--remote", "origin")
	rFact := g.requireFastForwardFact("R", p.Candidate.ID, head, p.Acceptance.ID)
	if want := serveAdvanceRepairLines("origin", head, p.Candidate.ID, rFact.ID, 1); second != want {
		t.Fatalf("R's output:\n%s\nwant:\n%s", second, want)
	}
	if tip := g.ServerTip("main"); tip != head {
		t.Fatalf("hosted main = %s, want %s", tip, head)
	}

	dir, status := g.FreshVerifier("second", p.Candidate.ID)
	assertOperationalContains(t, status, "Status:   merged")
	if strings.Contains(status, "merge conflict") {
		t.Fatalf("two merge facts for one candidate were reported as a conflict:\n%s", status)
	}
	merges := serveAdvanceEvidence(serveAdvanceEvents(t, dir), p.Candidate.ID)["proposal.merged"]
	want := []string{mFact.ID, rFact.ID}
	slices.Sort(want)
	if !slices.Equal(merges, want) {
		t.Fatalf("verifier merge facts %q, want %q", merges, want)
	}

	// Once R has synced, a rerun reports the branch as advanced.
	g.hn("R", "sync", "origin")
	if output := g.hn("R", "merge", p.Candidate.ID, "--remote", "origin"); output != serveAdvanceAlreadyLine(p.Candidate.ID) {
		t.Fatalf("R's rerun output %q", output)
	}
}

// serveAdvanceDocumentedStale returns the guide's reject-stale-advance lines
// without Git's remote: prefix, and its illustrative actor, expected tip
// and current tip.
func serveAdvanceDocumentedStale(t *testing.T) ([]string, string, string, string) {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("docs", "serve-v0.md"))
	if err != nil {
		t.Fatal(err)
	}
	const marker = "<!-- example: reject-stale-advance -->\n"
	guide := string(contents)
	if strings.Count(guide, marker) != 1 {
		t.Fatalf("the serve guide must contain exactly one %q", strings.TrimSpace(marker))
	}
	_, after, _ := strings.Cut(guide, marker)
	_, after, found := strings.Cut(after, "```text\n")
	if !found {
		t.Fatal("no text block after the reject-stale-advance marker")
	}
	block, _, found := strings.Cut(after, "```\n")
	if !found {
		t.Fatal("unterminated reject-stale-advance block")
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimSuffix(block, "\n"), "\n") {
		if !strings.HasPrefix(line, "remote: ") {
			t.Fatalf("documented line %q does not show Git's remote: prefix", line)
		}
		lines = append(lines, serveCleanStderrLine(line))
	}
	if len(lines) != 2 {
		t.Fatalf("reject-stale-advance has %d lines, want 2", len(lines))
	}
	rejection, err := parseServeRejectionLine(lines[0])
	if err != nil {
		t.Fatalf("documented line does not parse: %v", err)
	}
	if rejection.Code != serveReasonProtectedStaleTip || len(rejection.IDs) != 2 {
		t.Fatalf("documented rejection %+v, want protected-stale-tip with two IDs", rejection)
	}
	return lines, rejection.Actor, rejection.IDs[0], rejection.IDs[1]
}

// requireDocumentedStale compares relayed lines with the guide's
// reject-stale-advance example after substituting fixture values.
func requireDocumentedStale(t *testing.T, relayed []string, actor, expected, current string) {
	t.Helper()
	documented, docActor, docExpected, docCurrent := serveAdvanceDocumentedStale(t)
	want := make([]string, 0, len(documented))
	for _, line := range documented {
		line = strings.ReplaceAll(line, docActor, actor)
		line = strings.ReplaceAll(line, docExpected, expected)
		want = append(want, strings.ReplaceAll(line, docCurrent, current))
	}
	requireServeRelayedLines(t, relayed, want)
}

// US3-6: M and R advance two different ready proposals at the same moment.
// Exactly one advances main; the other is refused with protected-stale-tip,
// or protected-not-fast-forward when its step 4 already read the winner's
// head, and main stays at the winner's head.
func TestServeAdvanceConcurrentMaintainers(t *testing.T) {
	g := newServeGovernedFixture(t, serveGovernedOptions{})
	base := serveAdvanceTwoMaintainers(g)
	heads := map[string]string{}
	candidates := map[string]string{}
	for _, who := range []string{"M", "R"} {
		heads[who] = g.Commit(base, nil)
		candidates[who] = g.ReadyProposal(base, heads[who]).Candidate.ID
	}
	g.hn("R", "sync", "origin")

	type result struct {
		output string
		status int
		err    error
	}
	results := map[string]result{}
	var mutex sync.Mutex
	var group sync.WaitGroup
	start := make(chan struct{})
	for _, who := range []string{"M", "R"} {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			output, status, err := g.advanceExec(who, nil, g.h.Binary, "merge", candidates[who], "--remote", "origin")
			mutex.Lock()
			results[who] = result{output, status, err}
			mutex.Unlock()
		}()
	}
	close(start)
	group.Wait()

	var winner, loser string
	for _, who := range []string{"M", "R"} {
		if results[who].err != nil {
			t.Fatalf("%s: %v\n%s", who, results[who].err, results[who].output)
		}
		if results[who].status == 0 {
			winner = who
		} else {
			loser = who
		}
	}
	if winner == "" || loser == "" || results[loser].status != 1 {
		t.Fatalf("want exactly one success: M exited %d:\n%s\nR exited %d:\n%s",
			results["M"].status, results["M"].output, results["R"].status, results["R"].output)
	}
	if tip := g.ServerTip("main"); tip != heads[winner] {
		t.Fatalf("hosted main = %s, want the winner's head %s", tip, heads[winner])
	}
	if facts := g.MergeFacts(loser, candidates[loser]); len(facts) != 0 {
		t.Fatalf("the loser recorded %d merge facts", len(facts))
	}
	g.requireFastForwardFact(winner, candidates[winner], heads[winner], g.MergeFacts(winner, candidates[winner])[0].Evidence...)

	rejection := requireOneRejection(t, results[loser].output)
	if rejection.Actor != g.actors[loser] {
		t.Fatalf("rejection actor %s, want the loser %s", rejection.Actor, g.actors[loser])
	}
	switch rejection.Code {
	case serveReasonProtectedStaleTip:
		requireServeIDs(t, rejection, base, heads[winner])
		_, lines := extractServeRejections(results[loser].output)
		requireDocumentedStale(t, lines, g.actors[loser], base, heads[winner])
	case serveReasonProtectedNotFastForward:
		requireServeIDs(t, rejection, heads[winner], heads[loser])
	default:
		t.Fatalf("loser rejection %+v, want protected-stale-tip or protected-not-fast-forward", rejection)
	}
	t.Logf("winner %s, loser %s refused with %s", winner, loser, rejection.Code)
}

// NFR-005: 50 repetitions of ten concurrent advancement pushes of ten
// ready candidates on the same tip T. Each time exactly one push advances
// main to its head, the other nine get protected-stale-tip, the push log
// gains exactly ten entries and no other ref changes.
func TestServeAdvanceRaceNFR005(t *testing.T) {
	const candidates, repetitions = 10, 50
	g := newServeGovernedFixture(t, serveGovernedOptions{Solo: true})
	type entry struct{ candidate, head string }
	var entries []entry
	for index := 0; index < candidates; index++ {
		// The server validates a decision against the pipeline at the head,
		// so the head must be hosted before the actor chain is published.
		head := g.Commit(g.Base, nil)
		g.Host(head)
		g.hn("M", "proposal", "open", "--base", g.Base, "--head", head, fmt.Sprintf("Race proposal %d", index))
		candidate := g.latest("M")
		g.hn("M", "review", candidate.ID, "--approve", "--body", "Solo approval.")
		g.hn("M", "run", "request", candidate.ID, "test")
		request := g.latest("M")
		g.hn("M", "run", "execute", request.ID, "--backend", "host", "--allow-unsafe-host-execution")
		g.hn("M", "decide", candidate.ID, "--accept", "--body", "Solo acceptance.")
		entries = append(entries, entry{candidate.ID, head})
	}
	g.hn("M", "sync", "origin")
	otherRefs := func() string {
		var kept []string
		for _, line := range strings.Split(g.ServerRefs(), "\n") {
			if !strings.HasSuffix(line, " "+serveGovernedMain) {
				kept = append(kept, line)
			}
		}
		return strings.Join(kept, "\n")
	}

	started := time.Now()
	for repetition := 0; repetition < repetitions; repetition++ {
		if tip := g.ServerTip("main"); tip != g.Base {
			g.h.Serve("serve", "repo", "lock-exec", "r", "--", "git", "update-ref", serveGovernedMain, g.Base, tip)
		}
		refs := otherRefs()
		logged := len(g.pushLog())

		type result struct {
			output string
			status int
			err    error
		}
		results := make([]result, candidates)
		var group sync.WaitGroup
		start := make(chan struct{})
		for index, e := range entries {
			group.Add(1)
			go func() {
				defer group.Done()
				<-start
				output, status, err := g.advanceExec("M", nil, "git", "push", "--porcelain",
					"-o", servePushOptionAdvance+"="+e.candidate, "-o", servePushOptionExpected+"="+g.Base,
					"origin", "+"+e.head+":"+serveGovernedMain)
				results[index] = result{output, status, err}
			}()
		}
		close(start)
		group.Wait()

		winner := -1
		for index, r := range results {
			if r.err != nil {
				t.Fatalf("repetition %d push %d: %v\n%s", repetition, index, r.err, r.output)
			}
			if r.status == 0 {
				if winner >= 0 {
					t.Fatalf("repetition %d: pushes %d and %d both succeeded", repetition, winner, index)
				}
				winner = index
			}
		}
		if winner < 0 {
			t.Fatalf("repetition %d: no push succeeded:\n%s", repetition, results[0].output)
		}
		tip := g.ServerTip("main")
		if tip != entries[winner].head {
			t.Fatalf("repetition %d: main = %s, want the winner's head %s", repetition, tip, entries[winner].head)
		}
		for index, r := range results {
			if index == winner {
				continue
			}
			rejections, _ := extractServeRejections(r.output)
			if len(rejections) != 1 || rejections[0].Code != serveReasonProtectedStaleTip ||
				!slices.Equal(rejections[0].IDs, []string{g.Base, tip}) {
				t.Fatalf("repetition %d push %d: want one protected-stale-tip naming %s,%s:\n%s", repetition, index, g.Base, tip, r.output)
			}
		}
		if after := otherRefs(); after != refs {
			t.Fatalf("repetition %d changed refs other than main:\nbefore\n%s\nafter\n%s", repetition, refs, after)
		}
		added := g.pushLog()[logged:]
		accepted := 0
		for _, logEntry := range added {
			if logEntry.Verdict == servePushVerdictAccepted {
				accepted++
			}
		}
		if len(added) != candidates || accepted != 1 {
			t.Fatalf("repetition %d: push log gained %d entries with %d accepted, want %d with 1", repetition, len(added), accepted, candidates)
		}
	}
	t.Logf("%d repetitions of %d concurrent advancement pushes took %s", repetitions, candidates, time.Since(started).Round(time.Millisecond))
}

// FR-016: --branch alone is a usage error, and hn merge PROPOSAL in a
// checkout whose origin is an hn serve remote still merges locally with
// --no-ff without contacting the server.
func TestServeAdvanceKeepsLocalMerge(t *testing.T) {
	g := newServeGovernedFixture(t, serveGovernedOptions{})
	head := g.Commit(g.Base, nil)
	p := g.ReadyProposal(g.Base, head)
	g.hn("M", "sync", "origin")
	logged := len(g.pushLog())

	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"merge", p.Candidate.ID, "--branch", "main"}, "hn: " + serveAdvanceUsage + "\n"},
		{[]string{"merge", p.Candidate.ID, "--remote"}, "hn: " + serveAdvanceUsage + "\n"},
		{[]string{"merge"}, "hn: usage: hn merge PROPOSAL\n"},
		{[]string{"merge", p.Candidate.ID, "extra"}, "hn: usage: hn merge PROPOSAL\n"},
	} {
		output, status, err := g.advanceExec("M", nil, g.h.Binary, tc.args...)
		if err != nil || status != 1 || !strings.HasSuffix(output, tc.want) {
			t.Fatalf("hn %q exited %d (%v):\n%s\nwant suffix %q", tc.args, status, err, output, tc.want)
		}
	}

	g.git("M", "checkout", "-q", "main")
	output := g.hn("M", "merge", p.Candidate.ID)
	merge := g.latest("M")
	if merge.Kind != "proposal.merged" || merge.Head != head || merge.Commit == head {
		t.Fatalf("local merge recorded %+v, want a --no-ff merge commit", merge)
	}
	if parents := strings.Fields(g.git("M", "show", "-s", "--format=%P", merge.Commit)); len(parents) != 2 || parents[1] != head {
		t.Fatalf("merge commit parents %q, want the head as second parent", parents)
	}
	assertOperationalContains(t, output, "Merged proposal "+shortID(p.Candidate.ID)+" into main at "+shortOID(merge.Commit))
	if entries := g.pushLog(); len(entries) != logged {
		t.Fatalf("hn merge without --remote contacted the server: %+v", entries[logged:])
	}
	if tip := g.ServerTip("main"); tip != g.Base {
		t.Fatalf("hosted main moved to %s", tip)
	}
}

// T067: the guide's advance example runs as written on the fixture, and
// its reject-stale-advance example matches the step 6 push a race loser
// sends, after replacing the illustrative IDs with fixture IDs.
func TestServeAdvanceDocumentedExample(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("docs", "serve-v0.md"))
	if err != nil {
		t.Fatal(err)
	}
	command, output := serveAdminTestExample(t, string(contents), "<!-- example: advance -->")
	fields := strings.Fields(command)
	const documentedCandidate = "sha256:1c2d3e4f..."
	if len(fields) < 2 || fields[0] != "hn" || !slices.Contains(fields, documentedCandidate) {
		t.Fatalf("advance example command %q does not run hn merge on %s", command, documentedCandidate)
	}

	g := newServeGovernedFixture(t, serveGovernedOptions{})
	head := g.Commit(g.Base, nil)
	p := g.ReadyProposal(g.Base, head)
	staleHead := g.Commit(g.Base, nil)
	stale := g.ReadyProposal(g.Base, staleHead)

	args := fields[1:]
	for index, arg := range args {
		if arg == documentedCandidate {
			args[index] = p.Candidate.ID
		}
	}
	got := g.hn("M", args...)
	fact := g.requireFastForwardFact("M", p.Candidate.ID, head, p.Acceptance.ID)
	replacer := strings.NewReplacer("9b2e4c6a8d0f", shortOID(head), "1c2d3e4f5a6b", shortID(p.Candidate.ID), "7d8e9f0a1b2c", shortID(fact.ID))
	want := strings.Split(strings.TrimSuffix(replacer.Replace(output), "\n"), "\n")
	gotLines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if !slices.Equal(gotLines, want) {
		t.Fatalf("advance output:\n%s\nwant:\n%s", strings.Join(gotLines, "\n"), strings.Join(want, "\n"))
	}

	// The loser's step 6 push still expects the base as the tip.
	g.git("M", "fetch", "-q", "origin")
	relayed := g.clients["M"].GitFailure(g.work["M"], "push", "--porcelain",
		"--push-option="+servePushOptionAdvance+"="+stale.Candidate.ID,
		"--push-option="+servePushOptionExpected+"="+g.Base,
		"origin", "+"+staleHead+":"+serveGovernedMain)
	_, lines := extractServeRejections(relayed)
	requireDocumentedStale(t, lines, g.actors["M"], g.Base, head)
}

// T061: argument parsing. Flags go before or after PROPOSAL, BRANCH is
// normalized to refs/heads/, and every malformed form is a usage error or
// an invalid-branch error.
func TestServeAdvanceParseArguments(t *testing.T) {
	const id = "sha256:0000000000000000000000000000000000000000000000000000000000000001"
	for _, tc := range []struct {
		args   []string
		remote string
		branch string
		err    string
	}{
		{args: []string{id, "--remote", "origin"}, remote: "origin", branch: "refs/heads/main"},
		{args: []string{"--remote", "origin", id}, remote: "origin", branch: "refs/heads/main"},
		{args: []string{"--branch", "release", id, "--remote=hub"}, remote: "hub", branch: "refs/heads/release"},
		{args: []string{id, "--remote", "origin", "--branch", "refs/heads/team/dev"}, remote: "origin", branch: "refs/heads/team/dev"},
		{args: []string{id, "--branch", "main"}, err: serveAdvanceUsage},
		{args: []string{id, "--remote"}, err: serveAdvanceUsage},
		{args: []string{id, "--remote="}, err: serveAdvanceUsage},
		{args: []string{"--remote", "origin"}, err: serveAdvanceUsage},
		{args: []string{id, id, "--remote", "origin"}, err: serveAdvanceUsage},
		{args: []string{id, "--remote", "origin", "--force"}, err: serveAdvanceUsage},
		{args: []string{id, "--remote", "origin", "--branch", "bad..name"}, err: `invalid branch "bad..name"`},
		{args: []string{id, "--remote", "origin", "--branch", "refs/tags/v1"}, err: `invalid branch "refs/tags/v1": name a branch or a refs/heads/ ref`},
		{args: []string{id, "--remote", "origin", "--branch", "refs/heads/"}, err: `invalid branch "refs/heads/"`},
		{args: []string{id, "--remote", "origin", "--branch", "-x"}, err: `invalid branch "-x"`},
	} {
		if !mergeRemoteRequested(tc.args) {
			t.Fatalf("%q does not select remote advancement", tc.args)
		}
		proposal, remote, branch, err := parseMergeRemoteArgs(tc.args)
		if tc.err != "" {
			if err == nil || err.Error() != tc.err {
				t.Fatalf("%q: err = %v, want %q", tc.args, err, tc.err)
			}
			continue
		}
		if err != nil || proposal != id || remote != tc.remote || branch != tc.branch {
			t.Fatalf("%q = %q %q %q %v, want %q %q %q", tc.args, proposal, remote, branch, err, id, tc.remote, tc.branch)
		}
	}
	for _, args := range [][]string{{id}, {}, {id, "--remotes", "x"}, {id, "-remote", "x"}} {
		if mergeRemoteRequested(args) {
			t.Fatalf("%q selects remote advancement", args)
		}
	}
	if err := run([]string{"merge", id, "--remote", "bad/remote"}); err == nil || err.Error() != `invalid remote name "bad/remote"` {
		t.Fatalf("invalid remote = %v", err)
	}
}
