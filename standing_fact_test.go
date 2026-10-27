package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Fact-admission tests for proposal.merged (WP07, FR-012, FR-024, FR-025).
//
// Facts are signed in-process with the standing harness and judged by the
// built hn binary. A signed fact the harness must keep away from other
// clones is never published: each test resets the signer's actor ref to
// its value before the fact, so one invalid fact never leaks into the
// next case. In-process signing changes the working directory, so no test
// here may call t.Parallel.

// standingFactRulePathSurfaces is the only record of which fact-admission
// surfaces implement path (b), the rule check of admitMergeFact (decision
// 01M5VSEHF5B1FM9TB52KWKQGWZ). A WP that adds path (b) to a surface flips
// that surface's entry here; TestStandingFactBoundary then expects B1 to be
// accepted on it. The hn serve actor-chain check runs the import code, so
// it follows "import" and is exercised end to end in WP16.
var standingFactRulePathSurfaces = map[string]bool{
	"local":   true,
	"import":  true,
	"shallow": true,
}

// standingFactSurfaces is the order the boundary test visits surfaces in.
var standingFactSurfaces = []string{"local", "import", "shallow"}

// standingFactChainHead is the commit of role's actor ref, empty when role
// has no chain yet.
func standingFactChainHead(h *standingHarness, role string) string {
	h.t.Helper()
	return h.git(role, "for-each-ref", "--format=%(objectname)", actorRef(h.Actor(role)))
}

// standingFactSetChain points role's actor ref at head, or deletes it when
// head is empty. It never touches the remote.
func standingFactSetChain(h *standingHarness, role, head string) {
	h.t.Helper()
	ref := actorRef(h.Actor(role))
	if head == "" {
		if standingFactChainHead(h, role) != "" {
			h.git(role, "update-ref", "-d", ref)
		}
		return
	}
	h.git(role, "update-ref", ref, head)
}

// standingFactParent is the chain commit before commit, empty for the first
// event of a chain.
func standingFactParent(h *standingHarness, role, commit string) string {
	h.t.Helper()
	parents := strings.Fields(h.git(role, "rev-list", "--parents", "-n", "1", commit))
	if len(parents) < 2 {
		return ""
	}
	return parents[1]
}

// standingFactSyncWithFact syncs role's clone without publishing signed:
// it drops the fact, syncs the clean chain, and puts the fact back. The
// fact's chain commit names the clean head as its parent, so the restored
// chain is the same one the signer wrote.
func standingFactSyncWithFact(h *standingHarness, signed standingHarnessSigned) {
	h.t.Helper()
	parent := standingFactParent(h, signed.Role, signed.Commit)
	standingFactSetChain(h, signed.Role, parent)
	h.hn(signed.Role, "sync", "origin")
	if head := standingFactChainHead(h, signed.Role); head != parent {
		h.t.Fatalf("sync moved %s's chain from %s to %s", signed.Role, parent, head)
	}
	standingFactSetChain(h, signed.Role, signed.Commit)
}

// standingFactAssertMerged requires role's clone to project candidate as
// merged, with fact listed by hn log.
func standingFactAssertMerged(t *testing.T, h *standingHarness, role, candidate, fact string) {
	t.Helper()
	status, code := h.Run(role, "proposal", "status", candidate)
	if code != 0 || !strings.Contains(status, "Status:   merged") {
		t.Fatalf("hn proposal status %s in %s's clone exited %d, want merged:\n%s", candidate, role, code, status)
	}
	log, code := h.Run(role, "log")
	if code != 0 || !strings.Contains(log, operationalShortID(fact)) {
		t.Fatalf("hn log in %s's clone exited %d without fact %s:\n%s", role, code, fact, log)
	}
}

// standingFactAssertRefused requires every projecting command in role's
// clone to refuse with want and leave role's refs unchanged (US3-3).
func standingFactAssertRefused(t *testing.T, h *standingHarness, role, candidate, want string) {
	t.Helper()
	commands := [][]string{{"log"}, {"issue", "list"}}
	if candidate != "" {
		commands = append([][]string{{"proposal", "status", candidate}}, commands...)
	}
	for _, command := range commands {
		before := h.Refs(role)
		output, code := h.Run(role, command...)
		if code == 0 || !strings.Contains(output, want) {
			t.Fatalf("hn %s in %s's clone exited %d, want a refusal containing %q:\n%s", strings.Join(command, " "), role, code, want, output)
		}
		if after := h.Refs(role); after != before {
			t.Fatalf("hn %s changed %s's refs:\nbefore:\n%s\nafter:\n%s", strings.Join(command, " "), role, before, after)
		}
	}
}

// standingFactRefusal is the refusal a defect row must produce for fact.
func standingFactRefusal(defect standingHarnessDefect, fact string) string {
	if defect.Expected == standingHarnessMaintainerPath {
		return "merge " + operationalShortID(fact) + " is not authorized by the proposal policy"
	}
	return "rule merge " + fact + " fails " + defect.Expected
}

// standingFactShapes returns one install function per signed shape of a
// defect row. A row with several payload shapes signs them as one chain,
// and projection reports only the first invalid fact, so every shape after
// the first is signed again alone on the clean chain with the same extra
// members.
func standingFactShapes(h *standingHarness, clean string, c standingHarnessCase, signed []standingHarnessSigned) []func() standingHarnessSigned {
	h.t.Helper()
	installs := make([]func() standingHarnessSigned, 0, len(signed))
	for index, item := range signed {
		item := item
		if index == 0 {
			installs = append(installs, func() standingHarnessSigned {
				standingFactSetChain(h, item.Role, item.Commit)
				return item
			})
			continue
		}
		canonical := item.Unannotated
		if len(canonical) == 0 || !bytes.HasPrefix(item.Payload, canonical[:len(canonical)-1]) {
			h.t.Fatalf("shape %d of %s is not an annotated payload:\n%s", index, item.ID, item.Payload)
		}
		members := string(item.Payload[len(canonical) : len(item.Payload)-1])
		installs = append(installs, func() standingHarnessSigned {
			standingFactSetChain(h, item.Role, clean)
			event := h.factEvent(item.Role, c)
			event.Rule = ""
			return h.signAnnotated(item.Role, event, members)
		})
	}
	return installs
}

// standingFactRunDefect signs each shape of defect in its signer's clone,
// requires every projecting command there to refuse it, and then requires
// the row's matched valid control to project. Signer and E chains are
// clean again afterwards, and nothing in this function publishes a fact.
func standingFactRunDefect(t *testing.T, h *standingHarness, defect standingHarnessDefect) {
	t.Helper()
	started := time.Now()
	c := defect.Prepare()
	for _, role := range []string{defect.Signer, "E"} {
		h.hn(role, "sync", "origin")
	}
	clean := standingFactChainHead(h, defect.Signer)
	cleanE := standingFactChainHead(h, "E")

	signed := defect.Build(c)
	if len(signed) == 0 {
		t.Fatalf("%s signed nothing", defect.ID)
	}
	for index, install := range standingFactShapes(h, clean, c, signed) {
		fact := install()
		if fact.Candidate != c.Candidate.ID {
			t.Fatalf("%s shape %d cites %s, want %s", defect.ID, index, fact.Candidate, c.Candidate.ID)
		}
		standingFactAssertRefused(t, h, defect.Signer, c.Candidate.ID, standingFactRefusal(defect, fact.ID))
		standingFactSetChain(h, defect.Signer, clean)
	}

	// The matched valid control proves the refusal is the defect's. F15 to
	// F18 controls open their own candidate, which E fetches with the fact
	// set aside.
	control := defect.Control(c)
	if control.Role != "E" {
		t.Fatalf("%s control is signed by %s, want E", defect.ID, control.Role)
	}
	if control.Candidate != c.Candidate.ID {
		standingFactSyncWithFact(h, control)
	}
	standingFactAssertMerged(t, h, "E", control.Candidate, control.ID)
	standingFactSetChain(h, "E", cleanE)
	t.Logf("%s: %d shape(s) refused and control accepted in %s", defect.ID, len(signed), time.Since(started).Round(time.Millisecond))
}

// T033 step 1: the valid fact of User Story 1 projects in E's own clone.
// FR-014 (local part): a missing candidate commit refuses path (b) and is
// never judged as an empty change list.
func TestStandingFactLocalValidRuleMerge(t *testing.T) {
	h := newStandingHarness(t, standingHarnessDefaultPolicy())
	candidate := h.ReadyCandidate(nil)
	h.hn("E", "sync", "origin")
	fact := h.SignFact("E", nil)
	standingFactAssertMerged(t, h, "E", candidate.ID, fact)
	h.hn("E", "issue", "list")

	h.inClone("E", func(*Identity) {
		events, err := collectEvents()
		if err != nil {
			t.Fatal(err)
		}
		byID := make(map[string]StoredEvent, len(events))
		for _, stored := range events {
			byID[stored.ID] = stored
		}
		merge, proposal := byID[fact], byID[candidate.ID]
		memo := newRuleValidationMemo(localRuleObjectAccess())
		if authority, err := validateMergeEventWith(memo, merge, proposal, byID); authority != mergeByRule || err != nil {
			t.Fatalf("validateMergeEventWith = %q, %v; want a rule merge", authority, err)
		}

		missing := proposal
		missing.Event.Head = strings.Repeat("0", len(proposal.Event.Head))
		memo = newRuleValidationMemo(localRuleObjectAccess())
		authority, err := validateMergeEventWith(memo, merge, missing, byID)
		var factErr *ruleFactError
		if authority != "" || err == nil || errors.As(err, &factErr) ||
			!strings.HasPrefix(err.Error(), "merge "+fact+": inspect changed paths") {
			t.Fatalf("absent head = %q, %v; want the projection error naming %s", authority, err, fact)
		}
		if memo.changedPathCalls != 1 {
			t.Fatalf("absent head read changed paths %d times, want 1", memo.changedPathCalls)
		}
	})
}

// T033 steps 2 and 3: F01 to F20 refused by every projecting command in the
// signer's clone, each next to its matched valid control.
//
// Cost decision: between rows the signer's and E's actor refs are reset
// with git update-ref to their clean heads, which is cheaper than a
// git clone --no-local per row, and no row's fact ever reaches the remote.
// The fact-only rows share one harness. Every row that builds its own
// candidates gets a fresh harness: each hn command verifies every event in
// the clone, so with all rows in one harness a row grew from 1 s at F01
// to over 60 s by F18 and the matrix took 6m41s; with a fresh harness per
// such row it took 2m24s on the same machine.
func TestStandingFactLocalDefects(t *testing.T) {
	if testing.Short() {
		t.Skip("builds about twenty candidates for the fixture matrix")
	}
	started := time.Now()
	binary := buildOperationalBinary(t)
	factOnly := map[string]bool{"F01": true, "F03": true, "F04": true, "F13": true, "F14": true, "F19": true, "F20": true}
	// F21 and F22 are missing-dependency rows of import and shallow
	// verification (WP08, WP09).
	semantic := func(id string) bool { return id != "F21" && id != "F22" }
	t.Run("fact-only rows", func(t *testing.T) {
		h := newStandingHarnessWithBinary(t, binary, standingHarnessDefaultPolicy())
		defects := standingHarnessDefects(h, h.ReadyCandidate(nil))
		for _, id := range standingHarnessDefectIDs {
			if factOnly[id] {
				standingFactRunDefect(t, h, defects[id])
			}
		}
	})
	for _, id := range standingHarnessDefectIDs {
		if factOnly[id] || !semantic(id) {
			continue
		}
		t.Run(id, func(t *testing.T) {
			h := newStandingHarnessWithBinary(t, binary, standingHarnessDefaultPolicy())
			defect, exists := standingHarnessDefects(h, h.ReadyCandidate(nil))[id]
			if !exists {
				t.Fatalf("defect table lacks %s", id)
			}
			standingFactRunDefect(t, h, defect)
		})
	}
	t.Logf("F01 to F20 took %s", time.Since(started).Round(time.Millisecond))
}

// T033 step 4 (NFR-005): two valid rule merge facts for one candidate, by
// E and by X, which this policy also lists in rule ordinary, project
// together.
//
// Counter decision: hn exposes no counter through HN_INTERNAL_TESTING, so
// there is no black-box count. The bound is WP05's memo unit test; here
// the production surface validateMergeEventWith, with the real local object
// access, is driven over both facts with one memo and its call counters
// are read in-process.
func TestStandingFactLocalMemoSharedAcrossFacts(t *testing.T) {
	policy := standingHarnessDefaultPolicy()
	for index := range policy.Rules {
		if policy.Rules[index].Name == "ordinary" {
			policy.Rules[index].Actors = []string{"E", "X"}
		}
	}
	h := newStandingHarness(t, policy)
	candidate := h.ReadyCandidate(nil)
	h.hn("E", "sync", "origin")
	h.hn("X", "sync", "origin")
	byE := h.SignFact("E", nil)
	byX := h.SignFact("X", nil)
	// X's chain enters E's local store directly, beside E's own chain.
	xRef := actorRef(h.Actor("X"))
	h.git("E", "fetch", "-q", h.Work("X"), xRef+":"+xRef)
	standingFactAssertMerged(t, h, "E", candidate.ID, byE)
	assertOperationalContains(t, h.hn("E", "log"), operationalShortID(byX))

	h.inClone("E", func(*Identity) {
		events, err := collectEvents()
		if err != nil {
			t.Fatal(err)
		}
		byID := make(map[string]StoredEvent, len(events))
		for _, stored := range events {
			byID[stored.ID] = stored
		}
		memo := newRuleValidationMemo(localRuleObjectAccess())
		for _, fact := range []string{byE, byX} {
			if authority, err := validateMergeEventWith(memo, byID[fact], byID[candidate.ID], byID); authority != mergeByRule || err != nil {
				t.Fatalf("fact %s = %q, %v; want a rule merge", fact, authority, err)
			}
		}
		if memo.policyCalls != 1 || memo.changedPathCalls != 1 || memo.pipelineDigestCalls != 1 {
			t.Fatalf("two facts made %d policy, %d changed-path and %d pipeline reads, want 1 each",
				memo.policyCalls, memo.changedPathCalls, memo.pipelineDigestCalls)
		}
	})
}

// T034 step 2: acceptance merge facts keep their meaning and messages.
func TestStandingFactLocalAcceptanceFactUnchanged(t *testing.T) {
	h := newStandingHarness(t, standingHarnessDefaultPolicy())
	merged := h.ReadyCandidate(nil)
	h.Accept("M", merged.ID)
	h.Checkout("M", "main", h.Base())
	h.hn("M", "merge", merged.ID)
	fact := h.latest("M")
	if fact.Kind != "proposal.merged" || fact.Subject != merged.ID {
		t.Fatalf("hn merge appended %#v, want the acceptance merge fact of %s", fact, merged.ID)
	}
	standingFactAssertMerged(t, h, "M", merged.ID, fact.ID)

	unaccepted := h.ReadyCandidate(nil)
	h.hn("M", "sync", "origin")
	clean := standingFactChainHead(h, "M")
	bare := h.SignFactFor("M", h.Case(unaccepted.ID), func(event *Event) {
		event.Evidence, event.Rule = nil, ""
	})
	standingFactAssertRefused(t, h, "M", unaccepted.ID, "merge "+operationalShortID(bare.ID)+" lacks required acceptance evidence")
	standingFactSetChain(h, "M", clean)
	standingFactAssertMerged(t, h, "M", merged.ID, fact.ID)
}

// T034 step 3 (US3-6, analysis C1): a maintainer merge with a rule claim is
// a maintainer merge. M is not an executing actor of rule ordinary, so the
// second fact would fail path (b) at rule-signer: it projects only because
// path (a) runs first. Neither fact reads a commit for path (b).
func TestStandingFactLocalRuleClaimOnMaintainerMerge(t *testing.T) {
	h := newStandingHarness(t, standingHarnessDefaultPolicy())
	claims := []string{"nosuch", "ordinary"}
	facts := map[string]string{}
	for _, claim := range claims {
		candidate := h.ReadyCandidate(nil)
		decision := h.Accept("M", candidate.ID)
		claim := claim
		facts[candidate.ID] = h.SignFactFor("M", h.Case(candidate.ID), func(event *Event) {
			event.Evidence, event.Rule = []string{decision.ID}, claim
		}).ID
	}
	for candidate, fact := range facts {
		standingFactAssertMerged(t, h, "M", candidate, fact)
	}

	h.inClone("M", func(*Identity) {
		events, err := collectEvents()
		if err != nil {
			t.Fatal(err)
		}
		byID := make(map[string]StoredEvent, len(events))
		for _, stored := range events {
			byID[stored.ID] = stored
		}
		memo := newRuleValidationMemo(localRuleObjectAccess())
		for candidate, fact := range facts {
			if byID[fact].Event.Rule == "" {
				t.Fatalf("fact %s carries no rule claim", fact)
			}
			if authority, err := validateMergeEventWith(memo, byID[fact], byID[candidate], byID); authority != mergeByMaintainers || err != nil {
				t.Fatalf("fact %s = %q, %v; want a maintainer merge", fact, authority, err)
			}
		}
		if memo.policyCalls+memo.changedPathCalls+memo.pipelineDigestCalls != 0 {
			t.Fatalf("maintainer merges read path (b) inputs: %d policy, %d changed-path, %d pipeline",
				memo.policyCalls, memo.changedPathCalls, memo.pipelineDigestCalls)
		}
	})
}

// T034 step 4 (US3-7, C-004): legacy history in a local store keeps its
// verdict. L01 to L05 project; L06 is refused by the strict resource
// decoder as before.
func TestStandingFactLocalLegacyHistory(t *testing.T) {
	h := newStandingHarness(t, standingHarnessLegacyPolicy())
	fixtures := standingHarnessLegacyFixtures(h)
	type built struct {
		fixture standingHarnessFixture
		signed  []standingHarnessSigned
	}
	var all []built
	for _, fixture := range fixtures {
		all = append(all, built{fixture: fixture, signed: fixture.Build()})
	}
	// R holds L01 and M holds L02 to L05; M reads them all after R
	// publishes. S holds the refused L06 and publishes nothing.
	h.Publish("R")
	h.hn("M", "sync", "origin")
	log := h.hn("M", "log")
	for _, item := range all {
		switch item.fixture.Verdict {
		case standingHarnessValid, standingHarnessValidMaintainer, standingHarnessAnnotatedMaintainer:
			for _, signed := range item.signed {
				assertOperationalContains(t, log, operationalShortID(signed.ID))
				if bytes.Contains(signed.Payload, []byte(`"kind":"proposal.merged"`)) {
					standingFactAssertMerged(t, h, "M", signed.Candidate, signed.ID)
				}
			}
		case standingHarnessRefusedAsBefore:
			standingFactAssertRefused(t, h, item.fixture.Signer, "", standingHarnessUnknownRuleField)
		default:
			t.Fatalf("%s has unexpected verdict %q", item.fixture.ID, item.fixture.Verdict)
		}
	}
}

// standingFactAccepted reports whether a declared verdict admits the fact.
func standingFactAccepted(verdict string) bool {
	switch verdict {
	case standingHarnessValidRuleMerge, standingHarnessValidMaintainer, standingHarnessAnnotatedMaintainer:
		return true
	case standingHarnessRefused:
		return false
	}
	panic("standing fact test: unknown verdict " + verdict)
}

// standingFactShallowVerifier is a depth-one clone of the remote that holds
// every actor chain of the remote in full and every proposal code ref at
// depth one, all fetched with Git alone into the accepted refs of origin.
// That is the complete closure a shallow verifier needs, obtained without
// hn sync.
func standingFactShallowVerifier(h *standingHarness) string {
	h.t.Helper()
	shallow := filepath.Join(h.t.TempDir(), "shallow")
	runOperationalGit(h.t, "", "clone", "-q", "--depth", "1", "file://"+h.Remote(), shallow)
	configureOperationalGit(h.t, shallow, "Standing Shallow Verifier")
	h.hnIn(shallow, "replication", "select", "origin", "--all")
	runOperationalGit(h.t, shallow, "fetch", "-q", "origin", "+refs/hn/actors/*:refs/hn/remotes/origin/actors/*")
	runOperationalGit(h.t, shallow, "fetch", "-q", "--depth", "1", "origin", "+refs/hn/proposals/*:refs/hn/remotes/origin/proposals/*")
	if value := runOperationalGit(h.t, shallow, "rev-parse", "--is-shallow-repository"); value != "true" {
		h.t.Fatalf("shallow verifier is not shallow: %s", value)
	}
	return shallow
}

// standingFactInterimShallow is the refusal of path (b) in a shallow clone
// until WP09.
func standingFactInterimShallow(fact string) string {
	return "rule merge " + fact + " cannot be verified in a shallow clone by this build; verify it in a full clone"
}

// T092: the activation boundary of decision 01M5VSEHF5B1FM9TB52KWKQGWZ.
// Every boundary fixture is judged on every surface, and B1 must be
// accepted exactly where standingFactRulePathSurfaces says path (b) exists.
func TestStandingFactBoundary(t *testing.T) {
	h := newStandingHarness(t, standingHarnessDefaultPolicy())
	for _, fixture := range standingHarnessBoundaryFixtures(h) {
		signed := fixture.Build()
		if len(signed) != 1 || signed[0].Role != fixture.Signer {
			t.Fatalf("%s signed %d facts, want one by %s", fixture.ID, len(signed), fixture.Signer)
		}
		fact := signed[0]
		ref := actorRef(h.Actor(fact.Role))
		clean := standingFactParent(h, fact.Role, fact.Commit)
		remoteBefore := runOperationalGit(t, h.Remote(), "for-each-ref", "--format=%(objectname)", ref)

		for _, surface := range standingFactSurfaces {
			want := fixture.WithoutRulePath
			if standingFactRulePathSurfaces[surface] {
				want = fixture.WithRulePath
			}
			accepted := standingFactAccepted(want)
			var output string
			var status int
			var verifier string
			switch surface {
			case "local":
				// The signer's own full clone, synced with the fact set aside.
				standingFactSyncWithFact(h, fact)
				verifier = h.Work(fact.Role)
				output, status = h.RunIn(verifier, "proposal", "status", fact.Candidate)
				// Publish the chain with Git alone for the other surfaces.
				h.git(fact.Role, "push", "-q", "origin", "+"+ref+":"+ref)
			case "import":
				verifier = h.Verifier()
				output, status = h.RunIn(verifier, "sync", "origin")
				if status == 0 {
					output, status = h.RunIn(verifier, "proposal", "status", fact.Candidate)
				}
				promoted := runOperationalGit(t, verifier, "for-each-ref", "--format=%(objectname)", acceptedActorRef("origin", h.Actor(fact.Role)))
				if (promoted == fact.Commit) != accepted {
					t.Fatalf("%s on import: accepted actor ref is %q with fact commit %s, want accepted=%v\n%s", fixture.ID, promoted, fact.Commit, accepted, output)
				}
			case "shallow":
				verifier = standingFactShallowVerifier(h)
				output, status = h.RunIn(verifier, "proposal", "status", fact.Candidate)
			}
			t.Logf("%s on %s exited %d:\n%s", fixture.ID, surface, status, output)
			if accepted {
				if status != 0 || !strings.Contains(output, "Status:   merged") {
					t.Fatalf("%s on %s exited %d, want %s:\n%s", fixture.ID, surface, status, want, output)
				}
				continue
			}
			if status == 0 {
				t.Fatalf("%s on %s was accepted, want refused:\n%s", fixture.ID, surface, output)
			}
			if !strings.Contains(output, operationalShortID(fact.ID)) {
				t.Fatalf("%s on %s refused without naming fact %s:\n%s", fixture.ID, surface, fact.ID, output)
			}
			if surface == "shallow" && fixture.NeedsRulePath {
				// The interim refusal WP09 replaces.
				assertOperationalContains(t, output, standingFactInterimShallow(fact.ID))
			}
		}

		// A fact some surface refuses leaves the remote and the signer's
		// chain, so the next fixture starts from history every clone imports.
		if !standingFactAccepted(fixture.WithRulePath) || !standingFactAccepted(fixture.WithoutRulePath) {
			if remoteBefore == "" {
				runOperationalGit(t, h.Remote(), "update-ref", "-d", ref)
			} else {
				runOperationalGit(t, h.Remote(), "update-ref", ref, remoteBefore)
			}
			standingFactSetChain(h, fact.Role, clean)
		}
	}
}

// Replication import tests (WP08, FR-012 to FR-014, FR-024, C-004, C-005).
//
// A defective fact reaches the remote with raw Git: the signer's own
// hn sync projects first and would refuse it (WP07). The signer's actor
// ref is pushed with git push, and reset afterwards with git update-ref and
// a forced push, so each defect is the only one on the chain. The signer
// never runs hn sync while a defect is installed.

// standingFactRemoteChain is the value of role's actor ref on the remote,
// empty when the remote has none.
func standingFactRemoteChain(h *standingHarness, role string) string {
	h.t.Helper()
	return runOperationalGit(h.t, h.Remote(), "for-each-ref", "--format=%(objectname)", actorRef(h.Actor(role)))
}

// standingFactRawPublish makes the remote's actor ref of role equal role's
// local chain with Git alone, deleting it when the local chain is empty.
func standingFactRawPublish(h *standingHarness, role string) {
	h.t.Helper()
	ref := actorRef(h.Actor(role))
	if standingFactChainHead(h, role) == "" {
		if standingFactRemoteChain(h, role) != "" {
			h.git(role, "push", "-q", "origin", "--delete", ref)
		}
		return
	}
	h.git(role, "push", "-q", "-f", "origin", ref+":"+ref)
}

// standingFactAcceptedChain is the value of role's accepted actor ref in a
// verifier clone, empty when nothing of role's chain was promoted.
func standingFactAcceptedChain(h *standingHarness, verifier, role string) string {
	h.t.Helper()
	return runOperationalGit(h.t, verifier, "for-each-ref", "--format=%(objectname)", acceptedActorRef("origin", h.Actor(role)))
}

// standingFactAssertImportedExcept requires the verifier to hold every
// remote actor chain as accepted, except the refused roles' chains, of
// which nothing is promoted (C-005).
func standingFactAssertImportedExcept(t *testing.T, h *standingHarness, verifier, output string, refused ...string) {
	t.Helper()
	for _, role := range standingHarnessRoles {
		remote, accepted := standingFactRemoteChain(h, role), standingFactAcceptedChain(h, verifier, role)
		if standingFactListed(role, refused) {
			if accepted != "" {
				t.Fatalf("the verifier promoted %s's refused chain to %s:\n%s", role, accepted, output)
			}
			continue
		}
		if accepted != remote {
			t.Fatalf("the verifier holds %s's chain at %q, want the remote's %q:\n%s", role, accepted, remote, output)
		}
	}
}

// standingFactListed reports whether role is one of roles.
func standingFactListed(role string, roles []string) bool {
	for _, listed := range roles {
		if listed == role {
			return true
		}
	}
	return false
}

// standingFactAssertNotMerged requires hn proposal status in the verifier
// to succeed without reporting candidate merged.
func standingFactAssertNotMerged(t *testing.T, h *standingHarness, verifier, candidate string) {
	t.Helper()
	status, code := h.RunIn(verifier, "proposal", "status", candidate)
	if code != 0 || strings.Contains(status, "Status:   merged") {
		t.Fatalf("hn proposal status %s in the verifier exited %d, want a projection that is not merged:\n%s", candidate, code, status)
	}
}

// standingFactImportRefusal is the import refusal a defect row must produce
// for fact: the rule check, or today's maintainer-path refusal of a fact
// without a canonical rule claim (F19, F20).
func standingFactImportRefusal(h *standingHarness, defect standingHarnessDefect, c standingHarnessCase, fact string) string {
	if defect.Expected == standingHarnessMaintainerPath {
		return "merge " + fact + " is not authorized by exact base policy " + h.PolicyDigest(c.Candidate.Base)
	}
	return "rule merge " + fact + " fails " + defect.Expected
}

// standingFactImportDefect signs each shape of defect, publishes it with
// raw Git, and requires a fresh verifier's hn sync to refuse the signer's
// chain with the row's refusal while importing every other chain. The
// signer's local and remote chains are clean again afterwards.
func standingFactImportDefect(t *testing.T, h *standingHarness, defect standingHarnessDefect) {
	t.Helper()
	started := time.Now()
	c := defect.Prepare()
	clean := standingFactChainHead(h, defect.Signer)
	if remote := standingFactRemoteChain(h, defect.Signer); remote != clean {
		t.Fatalf("%s's chain is %q locally and %q on the remote before %s", defect.Signer, clean, remote, defect.ID)
	}
	signed := defect.Build(c)
	if len(signed) == 0 {
		t.Fatalf("%s signed nothing", defect.ID)
	}
	for index, install := range standingFactShapes(h, clean, c, signed) {
		fact := install()
		if fact.Candidate != c.Candidate.ID {
			t.Fatalf("%s shape %d cites %s, want %s", defect.ID, index, fact.Candidate, c.Candidate.ID)
		}
		standingFactRawPublish(h, defect.Signer)
		verifier := h.Verifier()
		output, status := h.RunIn(verifier, "sync", "origin")
		want := standingFactImportRefusal(h, defect, c, fact.ID)
		if status == 0 || !strings.Contains(output, want) {
			t.Fatalf("%s shape %d: hn sync exited %d, want a refusal containing %q:\n%s", defect.ID, index, status, want, output)
		}
		standingFactAssertImportedExcept(t, h, verifier, output, defect.Signer)
		standingFactAssertNotMerged(t, h, verifier, c.Candidate.ID)
		standingFactSetChain(h, defect.Signer, clean)
		standingFactRawPublish(h, defect.Signer)
	}
	t.Logf("%s: %d shape(s) refused on import in %s", defect.ID, len(signed), time.Since(started).Round(time.Millisecond))
}

// T035: replication reads path (b) inputs from one complete store, and a
// missing or split commit pair is a dependency that is never evaluated.
func TestStandingFactImportObjectAccess(t *testing.T) {
	root := t.TempDir()
	commit := func(repository string, files map[string]string) string {
		t.Helper()
		standingHarnessWriteFiles(t, repository, files)
		runOperationalGit(t, repository, "commit", "-q", "-m", "standing import object access")
		return runOperationalGit(t, repository, "rev-parse", "HEAD")
	}
	quarantine, accepted := filepath.Join(root, "quarantine"), filepath.Join(root, "accepted")
	initOperationalRepository(t, quarantine, "Quarantine")
	initOperationalRepository(t, accepted, "Accepted")
	actors := map[string]string{}
	for index, role := range standingHarnessRoles {
		actors[role] = strings.Repeat(string("abcdef9"[index]), 64)
	}
	policy := standingHarnessDefaultPolicy().JSON(actors)
	acceptedBase := commit(accepted, map[string]string{serveGovernedPolicyPath: policy})
	acceptedHead := commit(accepted, map[string]string{"src/accepted.txt": "accepted head\n"})
	quarantineBase := commit(quarantine, map[string]string{serveGovernedPolicyPath: policy, "src/quarantine.txt": "quarantine base\n"})
	quarantineGit, acceptedGit := filepath.Join(quarantine, ".git"), filepath.Join(accepted, ".git")

	access := replicationRuleObjectAccess(quarantineGit, acceptedGit)
	if changed, err := access.ChangedPaths(acceptedBase, acceptedHead); err != nil || strings.Join(changed, ",") != "src/accepted.txt" {
		t.Fatalf("base and head only in the accepted store: %v %#v", err, changed)
	}
	if changed, err := access.ChangedPaths(quarantineBase, acceptedHead); err == nil {
		t.Fatalf("base only in the quarantine and head only in the accepted store produced %#v", changed)
	}
	if _, digest, err := access.PolicyAt(acceptedBase); err != nil || digest != eventID([]byte(policy)) {
		t.Fatalf("policy at the accepted base = %q, %v", digest, err)
	}

	own := newRuleValidationMemo(access)
	if (replicationDependencyOptions{RuleMemo: own}).ruleMemo(quarantineGit, acceptedGit) != own {
		t.Fatal("ruleMemo did not return the option's memo")
	}
	if fresh := (replicationDependencyOptions{}).ruleMemo(quarantineGit, acceptedGit); fresh == nil || fresh == own {
		t.Fatal("ruleMemo without an option did not create a memo")
	}

	proposalID := "sha256:" + strings.Repeat("1", 64)
	arm := func(base, head, rule string, memo *ruleValidationMemo, evidence ...StoredEvent) (StoredEvent, *replicationDependency, error) {
		t.Helper()
		proposal := StoredEvent{ID: proposalID, Event: Event{Kind: "proposal.open", Actor: actors["A"], Base: base, Head: head}}
		merge := StoredEvent{ID: "sha256:" + strings.Repeat("2", 64), Event: Event{Kind: "proposal.merged", Actor: actors["E"],
			Subject: proposal.ID, Head: head, Commit: head, Policy: "sha256:" + strings.Repeat("3", 64), Rule: rule}}
		byID := map[string]StoredEvent{proposal.ID: proposal}
		for _, item := range evidence {
			merge.Event.Evidence = append(merge.Event.Evidence, item.ID)
			byID[item.ID] = item
		}
		byID[merge.ID] = merge
		empty := map[string]string{}
		dependency, err := replicationEventDependencyWithOptions(quarantineGit, acceptedGit, merge, byID, empty, empty, empty,
			replicationDependencyOptions{RuleMemo: memo})
		return merge, dependency, err
	}

	// Both commits in the accepted store: path (b) reads its changed paths
	// there and judges the fact.
	memo := newRuleValidationMemo(access)
	merge, dependency, err := arm(acceptedBase, acceptedHead, "ordinary", memo)
	var factErr *ruleFactError
	if dependency != nil || !errors.As(err, &factErr) || factErr.FactID != merge.ID || factErr.Check != ruleCheckPolicyDigest {
		t.Fatalf("accepted-store fact = %#v, %v; want the policy-digest refusal", dependency, err)
	}
	if entry := memo.changed[[2]string{acceptedBase, acceptedHead}]; entry.err != nil || strings.Join(entry.paths, ",") != "src/accepted.txt" {
		t.Fatalf("accepted-store changed paths = %#v, %v", entry.paths, entry.err)
	}

	missing := strings.Repeat("4", 40)
	for _, row := range []struct {
		name, base, head, missing, reason string
	}{
		{"split stores", quarantineBase, acceptedHead, acceptedHead, "rule merge base and head commits are not in one store"},
		{"absent base", missing, acceptedHead, missing, "rule merge base commit is absent"},
		{"absent head", acceptedBase, missing, missing, "rule merge head commit is absent"},
		{"absent base and head", missing, strings.Repeat("5", 40), missing, "rule merge base commit is absent"},
	} {
		memo := newRuleValidationMemo(access)
		merge, dependency, err := arm(row.base, row.head, "ordinary", memo)
		if err != nil || dependency == nil {
			t.Fatalf("%s: dependency %#v, error %v; want a missing dependency", row.name, dependency, err)
		}
		want := replicationDependency{EventID: merge.ID, EventKind: "proposal.merged", Missing: row.missing,
			Recovery: "add exact proposal selection --proposal " + proposalID, Reason: row.reason}
		if *dependency != want {
			t.Fatalf("%s: dependency %#v, want %#v", row.name, *dependency, want)
		}
		if memo.policyCalls+memo.changedPathCalls+memo.pipelineDigestCalls != 0 {
			t.Fatalf("%s: path (b) read objects for an incomplete commit pair", row.name)
		}
	}

	// A cited run result whose request is absent waits for the request,
	// with the evidence recovery, before any object is read.
	request := "sha256:" + strings.Repeat("6", 64)
	result := StoredEvent{ID: "sha256:" + strings.Repeat("7", 64), Event: Event{Kind: "run.result", Actor: actors["T"],
		Subject: request, Outcome: "passed", Commit: acceptedHead}}
	memo = newRuleValidationMemo(access)
	merge, dependency, err = arm(acceptedBase, acceptedHead, "ordinary", memo, result)
	wantRequest := replicationDependency{EventID: merge.ID, EventKind: "proposal.merged", Missing: request,
		Recovery: "select the full actor history supplying " + request, Reason: "run request is absent"}
	if err != nil || dependency == nil || *dependency != wantRequest {
		t.Fatalf("absent run request = %#v, %v; want %#v", dependency, err, wantRequest)
	}
	if memo.policyCalls+memo.changedPathCalls+memo.pipelineDigestCalls != 0 {
		t.Fatal("path (b) read objects for a fact whose run request is absent")
	}

	// Without a rule claim the arm returns today's error, whatever the
	// commits, and never waits for them.
	merge, dependency, err = arm(quarantineBase, missing, "", nil)
	want := validateReplicationMergeAt(quarantineGit, acceptedGit, merge, StoredEvent{ID: proposalID,
		Event: Event{Kind: "proposal.open", Actor: actors["A"], Base: quarantineBase, Head: missing}}, nil)
	if dependency != nil || err == nil || want == nil || err.Error() != want.Error() {
		t.Fatalf("fact without a rule claim = %#v, %v; want %v", dependency, err, want)
	}
}

// standingFactAssertVerifierMerged requires the verifier to report candidate
// merged and to list fact in hn log.
func standingFactAssertVerifierMerged(t *testing.T, h *standingHarness, verifier, candidate, fact string) {
	t.Helper()
	status, code := h.RunIn(verifier, "proposal", "status", candidate)
	if code != 0 || !strings.Contains(status, "Status:   merged") {
		t.Fatalf("hn proposal status %s in the verifier exited %d, want merged:\n%s", candidate, code, status)
	}
	log, code := h.RunIn(verifier, "log")
	if code != 0 || !strings.Contains(log, operationalShortID(fact)) {
		t.Fatalf("hn log in the verifier exited %d without fact %s:\n%s", code, fact, log)
	}
}

// T039: a valid rule merge fact, published with raw Git, imports into a
// fresh clone with no identity, which then reports its candidate merged; a
// second sync changes nothing. An acceptance merge fact imported in the
// same sync keeps today's verdict.
func TestStandingFactImportValidRuleMerge(t *testing.T) {
	h := newStandingHarness(t, standingHarnessDefaultPolicy())
	maintained := h.ReadyCandidate(nil)
	h.Accept("M", maintained.ID)
	h.Checkout("M", "main", h.Base())
	h.hn("M", "merge", maintained.ID)
	acceptance := h.latest("M")
	if acceptance.Kind != "proposal.merged" || acceptance.Subject != maintained.ID {
		t.Fatalf("hn merge appended %#v, want the acceptance merge fact of %s", acceptance, maintained.ID)
	}
	h.Publish("M")

	candidate := h.ReadyCandidate(nil)
	fact := h.SignFact("E", nil)
	standingFactRawPublish(h, "E")

	verifier := h.Verifier()
	output := h.hnIn(verifier, "sync", "origin")
	t.Logf("first sync:\n%s", output)
	standingFactAssertImportedExcept(t, h, verifier, output)
	if accepted := standingFactAcceptedChain(h, verifier, "E"); accepted != standingFactChainHead(h, "E") {
		t.Fatalf("E's accepted chain is %q, want the fact's chain %s", accepted, standingFactChainHead(h, "E"))
	}
	standingFactAssertVerifierMerged(t, h, verifier, candidate.ID, fact)
	standingFactAssertVerifierMerged(t, h, verifier, maintained.ID, acceptance.ID)

	before := runOperationalGit(t, verifier, "for-each-ref", "--format=%(refname) %(objectname)")
	again := h.hnIn(verifier, "sync", "origin")
	if strings.Contains(again, ": failed (") {
		t.Fatalf("second sync reported a failure:\n%s", again)
	}
	if after := runOperationalGit(t, verifier, "for-each-ref", "--format=%(refname) %(objectname)"); after != before {
		t.Fatalf("second sync changed the verifier's refs:\nbefore:\n%s\nafter:\n%s\n%s", before, after, again)
	}
	standingFactAssertVerifierMerged(t, h, verifier, candidate.ID, fact)
}

// T038 steps 1 to 3 (NFR-001, C-005): F01 to F20 published with raw Git are
// refused by a fresh verifier's hn sync, which names the fact and its
// failed check (or today's maintainer-path refusal for F19 and F20),
// promotes nothing from the signer's chain and imports every other chain.
//
// Cost decision: as in TestStandingFactLocalDefects, the fact-only rows
// share one harness and every row that builds its own candidates gets a
// fresh harness, because every sync verifies every event on the remote.
func TestStandingFactImportDefects(t *testing.T) {
	if testing.Short() {
		t.Skip("builds about twenty candidates for the fixture matrix")
	}
	started := time.Now()
	binary := buildOperationalBinary(t)
	factOnly := map[string]bool{"F01": true, "F03": true, "F04": true, "F13": true, "F14": true, "F19": true, "F20": true}
	// F21 and F22 are missing-dependency rows with their own tests below.
	semantic := func(id string) bool { return id != "F21" && id != "F22" }
	t.Run("fact-only rows", func(t *testing.T) {
		h := newStandingHarnessWithBinary(t, binary, standingHarnessDefaultPolicy())
		defects := standingHarnessDefects(h, h.ReadyCandidate(nil))
		for _, id := range standingHarnessDefectIDs {
			if factOnly[id] {
				standingFactImportDefect(t, h, defects[id])
			}
		}
	})
	for _, id := range standingHarnessDefectIDs {
		if factOnly[id] || !semantic(id) {
			continue
		}
		t.Run(id, func(t *testing.T) {
			h := newStandingHarnessWithBinary(t, binary, standingHarnessDefaultPolicy())
			defect, exists := standingHarnessDefects(h, h.ReadyCandidate(nil))[id]
			if !exists {
				t.Fatalf("defect table lacks %s", id)
			}
			standingFactImportDefect(t, h, defect)
		})
	}
	t.Logf("F01 to F20 on import took %s", time.Since(started).Round(time.Millisecond))
}

// T038 step 4 (US3-5, FR-013): F21's only approval is R's, signed and never
// published. Import waits for it as a missing dependency, and admits the
// fact once R publishes.
func TestStandingFactImportWithheldApproval(t *testing.T) {
	h := newStandingHarness(t, standingHarnessDefaultPolicy())
	defect := standingHarnessDefects(h, h.ReadyCandidate(nil))["F21"]
	c := defect.Prepare()
	signed := defect.Build(c)
	if len(signed) != 1 {
		t.Fatalf("F21 signed %d facts, want 1", len(signed))
	}
	fact := signed[0]
	if remote := standingFactRemoteChain(h, "R"); remote == standingFactChainHead(h, "R") {
		t.Fatalf("R's approval %s is already on the remote", c.Approval.ID)
	}
	standingFactRawPublish(h, "E")

	verifier := h.Verifier()
	output, status := h.RunIn(verifier, "sync", "origin")
	want := "event " + fact.ID + " (proposal.merged) missing dependency " + c.Approval.ID +
		": evidence is absent; recovery: select the full actor history supplying " + c.Approval.ID
	if status == 0 || !strings.Contains(output, want) {
		t.Fatalf("hn sync exited %d, want a refusal containing %q:\n%s", status, want, output)
	}
	standingFactAssertImportedExcept(t, h, verifier, output, "E")
	standingFactAssertNotMerged(t, h, verifier, c.Candidate.ID)

	h.Publish("R")
	output = h.hnIn(verifier, "sync", "origin")
	if strings.Contains(output, ": failed (") {
		t.Fatalf("sync after R published reported a failure:\n%s", output)
	}
	if accepted := standingFactAcceptedChain(h, verifier, "E"); accepted != fact.Commit {
		t.Fatalf("E's accepted chain is %q, want the fact's commit %s:\n%s", accepted, fact.Commit, output)
	}
	standingFactAssertVerifierMerged(t, h, verifier, c.Candidate.ID, fact.ID)
}

// T038 step 5 (FR-014): F22's candidate head is absent from every store of
// the verifier. Import reports the head as a missing dependency of the fact
// with the exact proposal selection as recovery, and never judges an empty
// change list.
func TestStandingFactImportAbsentCommit(t *testing.T) {
	h := newStandingHarness(t, standingHarnessDefaultPolicy())
	defect := standingHarnessDefects(h, h.ReadyCandidate(nil))["F22"]
	c := defect.Prepare()
	signed := defect.Build(c)
	if len(signed) != 1 {
		t.Fatalf("F22 signed %d facts, want 1", len(signed))
	}
	fact := signed[0]
	standingFactRawPublish(h, "E")

	verifier := h.Verifier()
	runOperationalGitFailure(t, verifier, "cat-file", "-e", c.Candidate.Head+"^{commit}")
	output, status := h.RunIn(verifier, "sync", "origin")
	want := "event " + fact.ID + " (proposal.merged) missing dependency " + c.Candidate.Head +
		": rule merge head commit is absent; recovery: add exact proposal selection --proposal " + c.Candidate.ID
	if status == 0 || !strings.Contains(output, want) {
		t.Fatalf("hn sync exited %d, want a refusal containing %q:\n%s", status, want, output)
	}
	// A's chain opens the withdrawn candidate, so it waits for the same code,
	// and every chain citing A's candidates waits for A's: the existing
	// import closure, which E's own diagnostic does not depend on.
	assertOperationalContains(t, output, "missing dependency "+c.Candidate.ID+": proposal code ref is absent")
	if accepted := standingFactAcceptedChain(h, verifier, "E"); accepted != "" {
		t.Fatalf("the verifier promoted E's chain to %s:\n%s", accepted, output)
	}
	if strings.Contains(output, "fails "+ruleCheckChangedPaths) {
		t.Fatalf("an absent head was judged as a change list:\n%s", output)
	}
}

// T038 step 7 (US3-7, C-004): legacy history published with raw Git imports
// into a fresh verifier with its old verdicts: L01 and L02 are promoted, the
// maintainer merges of L03 to L05 report merged, and S's chain with L06 is
// refused as before. B2 and B3, maintainer merges with a rule claim, are
// imported by TestStandingFactBoundary on the "import" surface.
func TestStandingFactImportLegacyHistory(t *testing.T) {
	h := newStandingHarness(t, standingHarnessLegacyPolicy())
	type built struct {
		fixture standingHarnessFixture
		signed  []standingHarnessSigned
	}
	var all []built
	for _, fixture := range standingHarnessLegacyFixtures(h) {
		all = append(all, built{fixture: fixture, signed: fixture.Build()})
	}
	for _, role := range []string{"R", "M", "S"} {
		standingFactRawPublish(h, role)
	}

	verifier := h.Verifier()
	output, status := h.RunIn(verifier, "sync", "origin")
	t.Logf("hn sync exited %d:\n%s", status, output)
	if status == 0 {
		t.Fatalf("hn sync accepted S's chain with L06:\n%s", output)
	}
	assertOperationalContains(t, output, "Replication actor "+h.Actor("S")+": failed (", standingHarnessUnknownRuleField)
	standingFactAssertImportedExcept(t, h, verifier, output, "S")
	log := h.hnIn(verifier, "log")
	for _, item := range all {
		switch item.fixture.Verdict {
		case standingHarnessValid, standingHarnessValidMaintainer, standingHarnessAnnotatedMaintainer:
			for _, signed := range item.signed {
				assertOperationalContains(t, log, operationalShortID(signed.ID))
				if bytes.Contains(signed.Payload, []byte(`"kind":"proposal.merged"`)) {
					standingFactAssertVerifierMerged(t, h, verifier, signed.Candidate, signed.ID)
				}
			}
		case standingHarnessRefusedAsBefore:
			for _, signed := range item.signed {
				if strings.Contains(log, operationalShortID(signed.ID)) {
					t.Fatalf("refused %s event %s is in the verifier's log:\n%s", item.fixture.ID, signed.ID, log)
				}
			}
		default:
			t.Fatalf("%s has unexpected verdict %q", item.fixture.ID, item.fixture.Verdict)
		}
	}
}

// Shallow verification tests (WP09, FR-012 to FR-014, FR-025, C-004).
//
// A shallow verifier is a depth-one clone of the remote whose accepted refs
// are filled with Git alone, so each test decides exactly which facts and
// objects the verifier holds. Facts reach the remote with raw Git as in the
// import tests, and the signer's chain is reset on both sides after each
// case.

// standingFactShallowArrangement is what a shallow verifier leaves out. The
// zero value is the complete closure of decision 01M5VSEHFV2K03JJY0NN6DFNAT:
// every actor chain in full, every proposal code ref and every scratch
// branch (the harness pushes each commit to one) at depth one, so the base
// and head of every candidate, the base policy blob and the pipeline
// definitions are present.
type standingFactShallowArrangement struct {
	SkipChains    []string // roles whose actor chains are not fetched
	SkipProposals []string // candidates whose proposal code refs are not fetched
	SkipCommits   []string // commits no fetched ref may point at
	DropObjects   []string // objects deleted from the verifier after fetching
}

// standingFactShallowVerifierWith builds a shallow verifier with the
// replication selection the existing shallow verifier uses, --all for
// origin, holding the complete closure minus what arrangement leaves out.
func standingFactShallowVerifierWith(h *standingHarness, arrangement standingFactShallowArrangement) string {
	h.t.Helper()
	shallow := filepath.Join(h.t.TempDir(), "shallow")
	runOperationalGit(h.t, "", "clone", "-q", "--depth", "1", "file://"+h.Remote(), shallow)
	configureOperationalGit(h.t, shallow, "Standing Shallow Verifier")
	h.hnIn(shallow, "replication", "select", "origin", "--all")
	skipRef, skipCommit := map[string]bool{}, map[string]bool{}
	for _, role := range arrangement.SkipChains {
		skipRef[actorRef(h.Actor(role))] = true
	}
	for _, candidate := range arrangement.SkipProposals {
		skipRef[proposalRef(candidate)] = true
	}
	for _, commit := range arrangement.SkipCommits {
		skipCommit[commit] = true
	}
	var chains, code []string
	listed := runOperationalGit(h.t, h.Remote(), "for-each-ref", "--format=%(objectname) %(refname)",
		"refs/hn/actors", "refs/hn/proposals", "refs/heads/standing")
	for _, line := range strings.Split(listed, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || skipRef[fields[1]] || skipCommit[fields[0]] {
			continue
		}
		ref := fields[1]
		switch {
		case strings.HasPrefix(ref, "refs/hn/actors/"):
			chains = append(chains, "+"+ref+":"+acceptedActorRef("origin", strings.TrimPrefix(ref, "refs/hn/actors/")))
		case strings.HasPrefix(ref, "refs/hn/proposals/"):
			code = append(code, "+"+ref+":"+acceptedProposalRef("origin", strings.TrimPrefix(ref, "refs/hn/proposals/")))
		default:
			code = append(code, "+"+ref+":refs/remotes/origin/"+strings.TrimPrefix(ref, "refs/heads/"))
		}
	}
	if len(chains) > 0 {
		runOperationalGit(h.t, shallow, append([]string{"fetch", "-q", "origin"}, chains...)...)
	}
	if len(code) > 0 {
		runOperationalGit(h.t, shallow, append([]string{"fetch", "-q", "--depth", "1", "origin"}, code...)...)
	}
	standingFactDropObjects(h.t, shallow, arrangement.DropObjects...)
	if value := runOperationalGit(h.t, shallow, "rev-parse", "--is-shallow-repository"); value != "true" {
		h.t.Fatalf("shallow verifier is not shallow: %s", value)
	}
	return shallow
}

// standingFactDropObjects deletes objects from repository with Git and the
// file system alone: every pack is unpacked into loose objects, and the
// loose files of objects are removed. Nothing else in the repository
// changes.
func standingFactDropObjects(t *testing.T, repository string, objects ...string) {
	t.Helper()
	if len(objects) == 0 {
		return
	}
	objectDir := filepath.Join(repository, ".git", "objects")
	packs, err := filepath.Glob(filepath.Join(objectDir, "pack", "*.pack"))
	if err != nil {
		t.Fatal(err)
	}
	contents := make([][]byte, 0, len(packs))
	for _, pack := range packs {
		data, err := os.ReadFile(pack)
		if err != nil {
			t.Fatal(err)
		}
		contents = append(contents, data)
	}
	// unpack-objects skips objects the repository already holds, so the
	// packs leave before their objects are written loose.
	files, err := filepath.Glob(filepath.Join(objectDir, "pack", "pack-*"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if err := os.Remove(file); err != nil {
			t.Fatal(err)
		}
	}
	for _, data := range contents {
		standingHarnessGitInput(t, repository, data, nil, "unpack-objects", "-q")
	}
	for _, object := range objects {
		if err := os.Remove(filepath.Join(objectDir, object[:2], object[2:])); err != nil {
			t.Fatalf("drop object %s: %v", object, err)
		}
		runOperationalGitFailure(t, repository, "cat-file", "-e", object)
	}
}

// standingFactShallowGap is the stable diagnostic of a shallow dependency
// gap of kind for missing: owned by candidate with the exact proposal
// selection as recovery, or, with no candidate, an event fact recovered by
// selecting the actor history that supplies it. Every harness verifier
// selects --all, so no gap names an already selected supplier.
func standingFactShallowGap(kind ShallowDependencyKind, missing, candidate string) string {
	gap := "kind=" + string(kind) + "; missing=" + missing
	if candidate == "" {
		return gap + "; remote=origin; recovery=select the actor history supplying full event ID " + missing + ", then retry"
	}
	return gap + "; owner=" + replicationProposal + ":" + candidate + "; remote=origin; ref=" + proposalRef(candidate) +
		"; recovery=hn replication select origin --proposal " + candidate +
		" (preserve existing selectors and budgets), then hn sync origin --recover-shallow"
}

// standingFactAssertShallowRefused requires hn proposal status of candidate
// and hn log in the shallow verifier to refuse with want, to leave every ref
// unchanged, and never to report the interim refusal WP09 removed or a
// judged empty change list. A semantic refusal must not be a shallow gap.
func standingFactAssertShallowRefused(t *testing.T, h *standingHarness, verifier, candidate, fact, want string, semantic bool) {
	t.Helper()
	for _, command := range [][]string{{"proposal", "status", candidate}, {"log"}} {
		before := runOperationalGit(t, verifier, "for-each-ref", "--format=%(refname) %(objectname)")
		output, status := h.RunIn(verifier, command...)
		t.Logf("hn %s in the shallow verifier exited %d:\n%s", strings.Join(command, " "), status, output)
		if status == 0 || !strings.Contains(output, want) {
			t.Fatalf("hn %s in the shallow verifier exited %d, want a refusal containing %q:\n%s", strings.Join(command, " "), status, want, output)
		}
		if semantic && strings.Contains(output, "shallow dependency gap") {
			t.Fatalf("hn %s refused a semantic row through a shallow gap:\n%s", strings.Join(command, " "), output)
		}
		for _, never := range []string{standingFactInterimShallow(fact), "the candidate has no changed paths"} {
			if strings.Contains(output, never) {
				t.Fatalf("hn %s reported %q:\n%s", strings.Join(command, " "), never, output)
			}
		}
		if after := runOperationalGit(t, verifier, "for-each-ref", "--format=%(refname) %(objectname)"); after != before {
			t.Fatalf("hn %s changed the shallow verifier's refs:\nbefore:\n%s\nafter:\n%s", strings.Join(command, " "), before, after)
		}
	}
}

// standingFactResetChain makes role's local and remote chains clean again.
func standingFactResetChain(h *standingHarness, role, clean string) {
	h.t.Helper()
	standingFactSetChain(h, role, clean)
	standingFactRawPublish(h, role)
}

// standingFactShallowControl publishes control with raw Git, requires a
// shallow verifier with the complete closure to report its candidate
// merged, and resets E's chain to clean.
func standingFactShallowControl(t *testing.T, h *standingHarness, control standingHarnessSigned, clean string) {
	t.Helper()
	if control.Role != "E" {
		t.Fatalf("control %s is signed by %s, want E", control.ID, control.Role)
	}
	standingFactRawPublish(h, "E")
	verifier := standingFactShallowVerifierWith(h, standingFactShallowArrangement{})
	standingFactAssertVerifierMerged(t, h, verifier, control.Candidate, control.ID)
	standingFactResetChain(h, "E", clean)
}

// T041 step 2: the valid rule merge fact of User Story 1 verifies in a
// shallow clone that holds its complete closure. This arrangement, the zero
// standingFactShallowArrangement, is the shallow fixture of the matrix.
func TestStandingFactShallowValidRuleMerge(t *testing.T) {
	h := newStandingHarness(t, standingHarnessDefaultPolicy())
	candidate := h.ReadyCandidate(nil)
	fact := h.SignFact("E", nil)
	standingFactRawPublish(h, "E")
	verifier := standingFactShallowVerifierWith(h, standingFactShallowArrangement{})
	standingFactAssertVerifierMerged(t, h, verifier, candidate.ID, fact)
	if output, status := h.RunIn(verifier, "issue", "list"); status != 0 {
		t.Fatalf("hn issue list in the shallow verifier exited %d:\n%s", status, output)
	}
}

// standingFactShallowDefect runs one semantic row on the shallow surface.
// The row's matched valid control is accepted first with the complete
// closure; then each shape of the defect, published with raw Git, is
// refused by a fresh shallow verifier with the same arrangement, naming the
// row's derivation check (or today's maintainer-path refusal for F19 and
// F20) and never a shallow gap.
func standingFactShallowDefect(t *testing.T, h *standingHarness, defect standingHarnessDefect) {
	t.Helper()
	started := time.Now()
	c := defect.Prepare()
	cleanE := standingFactChainHead(h, "E")
	clean := standingFactChainHead(h, defect.Signer)
	for _, role := range []string{"E", defect.Signer} {
		if remote := standingFactRemoteChain(h, role); remote != standingFactChainHead(h, role) {
			t.Fatalf("%s's chain is %q locally and %q on the remote before %s", role, standingFactChainHead(h, role), remote, defect.ID)
		}
	}

	// F15 to F18 controls open their own candidate, which needs syncs, so
	// the control runs while no defect is on the remote.
	standingFactShallowControl(t, h, defect.Control(c), cleanE)

	signed := defect.Build(c)
	if len(signed) == 0 {
		t.Fatalf("%s signed nothing", defect.ID)
	}
	for index, install := range standingFactShapes(h, clean, c, signed) {
		fact := install()
		if fact.Candidate != c.Candidate.ID {
			t.Fatalf("%s shape %d cites %s, want %s", defect.ID, index, fact.Candidate, c.Candidate.ID)
		}
		standingFactRawPublish(h, defect.Signer)
		verifier := standingFactShallowVerifierWith(h, standingFactShallowArrangement{})
		standingFactAssertShallowRefused(t, h, verifier, c.Candidate.ID, fact.ID, standingFactRefusal(defect, fact.ID), true)
		standingFactResetChain(h, defect.Signer, clean)
	}
	t.Logf("%s: control accepted and %d shape(s) refused with %q in a shallow clone in %s",
		defect.ID, len(signed), defect.Expected, time.Since(started).Round(time.Millisecond))
}

// T041 step 3 (NFR-001, decisions 01M5VSEHFV2K03JJY0NN6DFNAT and
// 01M5VWE2Q3AVA8NPGRCSKBFW5M): F01 to F20 on the shallow surface, each with
// a complete closure and a passing matched control.
//
// Cost decision: as in TestStandingFactImportDefects, the fact-only rows
// share one harness and every row that builds its own candidates gets a
// fresh harness.
func TestStandingFactShallowDefects(t *testing.T) {
	if testing.Short() {
		t.Skip("builds about twenty candidates for the fixture matrix")
	}
	started := time.Now()
	binary := buildOperationalBinary(t)
	factOnly := map[string]bool{"F01": true, "F03": true, "F04": true, "F13": true, "F14": true, "F19": true, "F20": true}
	// F21 and F22 are missing-dependency rows, in the tests below.
	semantic := func(id string) bool { return id != "F21" && id != "F22" }
	t.Run("fact-only rows", func(t *testing.T) {
		h := newStandingHarnessWithBinary(t, binary, standingHarnessDefaultPolicy())
		defects := standingHarnessDefects(h, h.ReadyCandidate(nil))
		for _, id := range standingHarnessDefectIDs {
			if factOnly[id] {
				standingFactShallowDefect(t, h, defects[id])
			}
		}
	})
	for _, id := range standingHarnessDefectIDs {
		if factOnly[id] || !semantic(id) {
			continue
		}
		t.Run(id, func(t *testing.T) {
			h := newStandingHarnessWithBinary(t, binary, standingHarnessDefaultPolicy())
			defect, exists := standingHarnessDefects(h, h.ReadyCandidate(nil))[id]
			if !exists {
				t.Fatalf("defect table lacks %s", id)
			}
			standingFactShallowDefect(t, h, defect)
		})
	}
	t.Logf("F01 to F20 in a shallow clone took %s", time.Since(started).Round(time.Millisecond))
}

// T041 step 4 (FR-013, FR-014): the missing-dependency table of the shallow
// surface. Each row is a valid rule merge fact by E that a shallow verifier
// missing one input refuses with that input's gap kind and recovery, with
// every ref unchanged. The policy blob and pipeline definition rows delete
// one object from an otherwise complete verifier, because no fetch through
// Git's public commands yields a commit without its tree's blobs. F21 runs
// last, since its approval stays unpublished until the end.
func TestStandingFactShallowMissingDependencies(t *testing.T) {
	h := newStandingHarness(t, standingHarnessDefaultPolicy())
	c := h.Case(h.ReadyCandidate(nil).ID)
	// A candidate whose base is a scratch commit, so the verifier can lack
	// the base while holding the head.
	offMainBase := h.Commit("M", h.base, nil)
	offMain := h.readyCase("A", offMainBase, nil)
	policyBlob := runOperationalGit(t, h.Remote(), "rev-parse", c.Candidate.Base+":"+serveGovernedPolicyPath)
	pipelineBlob := runOperationalGit(t, h.Remote(), "rev-parse", c.Candidate.Head+":.hn/pipelines/"+standingHarnessPipeline+".json")
	clean := standingFactChainHead(h, "E")

	rows := []struct {
		name        string
		c           standingHarnessCase
		arrangement standingFactShallowArrangement
		kind        ShallowDependencyKind
		missing     string
		owner       string
	}{
		{"missing review", c, standingFactShallowArrangement{SkipChains: []string{"R"}}, shallowSelectedFact, c.Approval.ID, ""},
		{"missing run result", c, standingFactShallowArrangement{SkipChains: []string{"T"}}, shallowSelectedFact, c.Result.ID, ""},
		{"missing run request", c, standingFactShallowArrangement{SkipChains: []string{"M"}}, shallowRunRequest, c.Request.ID, ""},
		{"missing base commit", offMain, standingFactShallowArrangement{SkipCommits: []string{offMainBase}},
			shallowBaseCommit, offMainBase, offMain.Candidate.ID},
		{"missing head", c, standingFactShallowArrangement{SkipCommits: []string{c.Candidate.Head}},
			shallowProposalCodeRef, c.Candidate.Head, c.Candidate.ID},
		{"missing code ref", c, standingFactShallowArrangement{SkipProposals: []string{c.Candidate.ID}},
			shallowProposalCodeRef, c.Candidate.Head, c.Candidate.ID},
		{"missing policy blob", c, standingFactShallowArrangement{DropObjects: []string{policyBlob}},
			shallowPolicyBlob, policyBlob, c.Candidate.ID},
		{"missing pipeline definition", c, standingFactShallowArrangement{DropObjects: []string{pipelineBlob}},
			shallowPipelineDefinition, pipelineBlob, c.Candidate.ID},
	}
	for _, row := range rows {
		fact := h.SignFactFor("E", row.c, nil)
		standingFactRawPublish(h, "E")
		verifier := standingFactShallowVerifierWith(h, row.arrangement)
		standingFactAssertShallowRefused(t, h, verifier, row.c.Candidate.ID, fact.ID, standingFactShallowGap(row.kind, row.missing, row.owner), false)
		t.Logf("%s: refused as %s %s", row.name, row.kind, row.missing)
		standingFactResetChain(h, "E", clean)
	}

	// F21: R's approval is signed and never published.
	f21 := standingHarnessDefects(h, c.Candidate)["F21"]
	withheld := f21.Prepare()
	signed := f21.Build(withheld)
	if len(signed) != 1 {
		t.Fatalf("F21 signed %d facts, want 1", len(signed))
	}
	standingFactRawPublish(h, "E")
	verifier := standingFactShallowVerifierWith(h, standingFactShallowArrangement{})
	standingFactAssertShallowRefused(t, h, verifier, withheld.Candidate.ID, signed[0].ID,
		standingFactShallowGap(shallowSelectedFact, withheld.Approval.ID, ""), false)
	h.Publish("R")
	verifier = standingFactShallowVerifierWith(h, standingFactShallowArrangement{})
	standingFactAssertVerifierMerged(t, h, verifier, withheld.Candidate.ID, signed[0].ID)
}

// T041 step 5 (C-004): maintainer merges keep their shallow verdicts. An
// acceptance merge fact whose decision is missing still reports a missing
// decision, and with a rule claim the same gap is labelled a selected event
// fact. A maintainer merge with a rule claim verifies without its proposal
// code ref, which path (a) never reads, while a rule merge for the same
// candidate in the same verifier waits for it. The candidate's head commit
// stays present: validating the maintainer's acceptance decision reads the
// pipeline definitions at the head, as it did before this mission. B2 and
// B3 are verified on the shallow surface by TestStandingFactBoundary.
func TestStandingFactShallowMaintainerMergesUnchanged(t *testing.T) {
	h := newStandingHarness(t, standingHarnessDefaultPolicy())
	unaccepted := h.Case(h.ReadyCandidate(nil).ID)
	// No chain supplies this decision.
	absent := eventID([]byte("standing shallow: a decision no chain supplies"))
	cleanM := standingFactChainHead(h, "M")
	for _, row := range []struct {
		rule string
		kind ShallowDependencyKind
	}{{"", shallowDecision}, {"ordinary", shallowSelectedFact}} {
		fact := h.SignFactFor("M", unaccepted, func(event *Event) {
			event.Evidence, event.Rule = []string{absent}, row.rule
		})
		standingFactRawPublish(h, "M")
		verifier := standingFactShallowVerifierWith(h, standingFactShallowArrangement{})
		standingFactAssertShallowRefused(t, h, verifier, unaccepted.Candidate.ID, fact.ID, standingFactShallowGap(row.kind, absent, ""), false)
		standingFactResetChain(h, "M", cleanM)
	}

	accepted := h.Case(h.ReadyCandidate(nil).ID)
	decision := h.Accept("M", accepted.Candidate.ID)
	maintainer := h.SignFactFor("M", accepted, func(event *Event) {
		event.Evidence, event.Rule = []string{decision.ID, accepted.Approval.ID}, "ordinary"
	})
	standingFactRawPublish(h, "M")
	withoutCodeRef := standingFactShallowArrangement{SkipProposals: []string{accepted.Candidate.ID}}
	verifier := standingFactShallowVerifierWith(h, withoutCodeRef)
	runOperationalGitFailure(t, verifier, "rev-parse", "--verify", "-q", acceptedProposalRef("origin", accepted.Candidate.ID))
	log := h.hnIn(verifier, "log")
	assertOperationalContains(t, log, operationalShortID(maintainer.ID))
	standingFactAssertVerifierMerged(t, h, standingFactShallowVerifierWith(h, standingFactShallowArrangement{}), accepted.Candidate.ID, maintainer.ID)

	clean := standingFactChainHead(h, "E")
	rule := h.SignFactFor("E", accepted, nil)
	standingFactRawPublish(h, "E")
	verifier = standingFactShallowVerifierWith(h, withoutCodeRef)
	before := runOperationalGit(t, verifier, "for-each-ref", "--format=%(refname) %(objectname)")
	output, status := h.RunIn(verifier, "log")
	want := standingFactShallowGap(shallowProposalCodeRef, accepted.Candidate.Head, accepted.Candidate.ID)
	if status == 0 || !strings.Contains(output, want) {
		t.Fatalf("hn log with rule merge %s exited %d, want a refusal containing %q:\n%s", rule.ID, status, want, output)
	}
	if after := runOperationalGit(t, verifier, "for-each-ref", "--format=%(refname) %(objectname)"); after != before {
		t.Fatalf("hn log changed the shallow verifier's refs:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	standingFactResetChain(h, "E", clean)
}

// T041 step 5 (US3-7, C-004): legacy history verifies in a shallow clone
// with its old verdicts. L01 and L02 are listed and the maintainer merges of
// L03 to L05 report merged; once S publishes its chain with L06, the
// strict resource decoder refuses it as before.
func TestStandingFactShallowLegacyHistory(t *testing.T) {
	h := newStandingHarness(t, standingHarnessLegacyPolicy())
	type built struct {
		fixture standingHarnessFixture
		signed  []standingHarnessSigned
	}
	var all []built
	for _, fixture := range standingHarnessLegacyFixtures(h) {
		all = append(all, built{fixture: fixture, signed: fixture.Build()})
	}
	for _, role := range []string{"R", "M"} {
		standingFactRawPublish(h, role)
	}
	verifier := standingFactShallowVerifierWith(h, standingFactShallowArrangement{})
	log := h.hnIn(verifier, "log")
	for _, item := range all {
		switch item.fixture.Verdict {
		case standingHarnessValid, standingHarnessValidMaintainer, standingHarnessAnnotatedMaintainer:
			for _, signed := range item.signed {
				assertOperationalContains(t, log, operationalShortID(signed.ID))
				if bytes.Contains(signed.Payload, []byte(`"kind":"proposal.merged"`)) {
					standingFactAssertVerifierMerged(t, h, verifier, signed.Candidate, signed.ID)
				}
			}
		case standingHarnessRefusedAsBefore:
			for _, signed := range item.signed {
				if strings.Contains(log, operationalShortID(signed.ID)) {
					t.Fatalf("refused %s event %s is in the shallow verifier's log:\n%s", item.fixture.ID, signed.ID, log)
				}
			}
			standingFactRawPublish(h, item.fixture.Signer)
			refused := standingFactShallowVerifierWith(h, standingFactShallowArrangement{})
			output, status := h.RunIn(refused, "log")
			if status == 0 || !strings.Contains(output, standingHarnessUnknownRuleField) {
				t.Fatalf("hn log with %s's chain holding %s exited %d, want %q:\n%s", item.fixture.Signer, item.fixture.ID, status, standingHarnessUnknownRuleField, output)
			}
		default:
			t.Fatalf("%s has unexpected verdict %q", item.fixture.ID, item.fixture.Verdict)
		}
	}
}

// T042 (FR-014): a rule merge fact whose candidate head is unavailable is
// refused or waits on every surface reached through public commands, and no
// surface judges an empty change list. Local projection runs in a full
// clone whose copy of the head was deleted: git diff-tree exits non-zero
// for the absent object, so changedPathsAt returns its error. Shallow
// verification and hn sync import run after F22 withdraws the head from
// the remote.
func TestStandingFactShallowNoEmptyDiff(t *testing.T) {
	h := newStandingHarness(t, standingHarnessDefaultPolicy())
	local := h.Case(h.ReadyCandidate(nil).ID)
	clean := standingFactChainHead(h, "E")
	fact := h.SignFactFor("E", local, nil)
	standingFactRawPublish(h, "E")
	emptyList := "fails " + ruleCheckChangedPaths + ": the candidate has no changed paths"

	// Local projection: a full clone that imported the fact, with its head
	// deleted and every ref to it removed.
	full := h.Verifier()
	h.hnIn(full, "sync", "origin")
	standingFactAssertVerifierMerged(t, h, full, local.Candidate.ID, fact.ID)
	for _, line := range strings.Split(runOperationalGit(t, full, "for-each-ref", "--format=%(objectname) %(refname)"), "\n") {
		if fields := strings.Fields(line); len(fields) == 2 && fields[0] == local.Candidate.Head {
			runOperationalGit(t, full, "update-ref", "-d", fields[1])
		}
	}
	standingFactDropObjects(t, full, local.Candidate.Head)
	if value := runOperationalGit(t, full, "rev-parse", "--is-shallow-repository"); value != "false" {
		t.Fatalf("the full clone is shallow: %s", value)
	}
	output, status := h.RunIn(full, "log")
	t.Logf("hn log in the full clone without the head exited %d:\n%s", status, output)
	if want := "merge " + fact.ID + ": inspect changed paths"; status == 0 || !strings.Contains(output, want) {
		t.Fatalf("hn log in the full clone exited %d, want a refusal containing %q:\n%s", status, want, output)
	}
	if strings.Contains(output, emptyList) {
		t.Fatalf("local projection judged an empty change list:\n%s", output)
	}
	standingFactResetChain(h, "E", clean)

	// F22: the candidate head is absent from every store.
	f22 := standingHarnessDefects(h, local.Candidate)["F22"]
	withdrawn := f22.Prepare()
	signed := f22.Build(withdrawn)
	if len(signed) != 1 {
		t.Fatalf("F22 signed %d facts, want 1", len(signed))
	}
	standingFactRawPublish(h, "E")

	shallow := standingFactShallowVerifierWith(h, standingFactShallowArrangement{})
	runOperationalGitFailure(t, shallow, "cat-file", "-e", withdrawn.Candidate.Head+"^{commit}")
	standingFactAssertShallowRefused(t, h, shallow, withdrawn.Candidate.ID, signed[0].ID,
		standingFactShallowGap(shallowProposalCodeRef, withdrawn.Candidate.Head, withdrawn.Candidate.ID), false)

	verifier := h.Verifier()
	runOperationalGitFailure(t, verifier, "cat-file", "-e", withdrawn.Candidate.Head+"^{commit}")
	output, status = h.RunIn(verifier, "sync", "origin")
	t.Logf("hn sync without the head exited %d:\n%s", status, output)
	want := "event " + signed[0].ID + " (proposal.merged) missing dependency " + withdrawn.Candidate.Head + ": rule merge head commit is absent"
	if status == 0 || !strings.Contains(output, want) {
		t.Fatalf("hn sync exited %d, want a refusal containing %q:\n%s", status, want, output)
	}
	if strings.Contains(output, emptyList) {
		t.Fatalf("import judged an empty change list:\n%s", output)
	}
}
