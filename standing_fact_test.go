package main

import (
	"bytes"
	"errors"
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
	"import":  false,
	"shallow": false,
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
