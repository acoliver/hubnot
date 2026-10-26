package main

import (
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// ruleAcceptanceTestActors are well-formed fingerprints; the live evaluator
// never verifies signatures, so no identities are needed.
var (
	ruleTestMaintainer  = strings.Repeat("a", 64)
	ruleTestGPT         = strings.Repeat("b", 64)
	ruleTestRunner      = strings.Repeat("c", 64)
	ruleTestAuthor      = strings.Repeat("d", 64)
	ruleTestSonnet      = strings.Repeat("e", 64)
	ruleTestOutsider    = strings.Repeat("f", 64)
	ruleTestMaintainer2 = strings.Repeat("9", 64)
)

const (
	ruleTestRuleName   = "ordinary"
	ruleTestBase       = "1111111111111111111111111111111111111111"
	ruleTestHead       = "2222222222222222222222222222222222222222"
	ruleTestOtherHead  = "3333333333333333333333333333333333333333"
	ruleTestPolicyHash = "sha256:" + "7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a"
)

var (
	ruleTestDefinition      = eventID([]byte("pipeline test definition"))
	ruleTestOtherDefinition = eventID([]byte("pipeline test other definition"))
)

// ruleAcceptanceTestWorld builds in-memory events for one candidate. Each
// builder method assigns the next sequence of its actor and a later
// timestamp, and records the event in Events.
type ruleAcceptanceTestWorld struct {
	t           *testing.T
	Policy      PolicyDocument
	Proposal    StoredEvent
	Target      string
	Changed     []string
	Definitions map[string]string
	Events      []StoredEvent
	sequences   map[string]uint64
	counter     int
}

func newRuleAcceptanceTestWorld(t *testing.T) *ruleAcceptanceTestWorld {
	t.Helper()
	world := &ruleAcceptanceTestWorld{
		t: t,
		Policy: PolicyDocument{
			Version:     policyVersion,
			Maintainers: []string{ruleTestMaintainer},
			Proposals: ProposalPolicy{
				RequiredApprovals: 1,
				RequiredAccepts:   1,
				TrustedReviewers:  []string{ruleTestSonnet, ruleTestGPT},
			},
			Pipelines: map[string]PipelinePolicy{
				"test": {RequiredResults: 1, TrustedRunners: []string{ruleTestRunner}},
			},
			Automerge: &AutomergePolicy{Rules: []AutomergeRule{{
				Name:         ruleTestRuleName,
				TargetBranch: "main",
				Actors:       []string{ruleTestMaintainer},
				Authors:      []string{ruleTestAuthor},
				Paths:        []string{"docs/"},
				Acceptance:   automergeAcceptanceOf(automergeAcceptanceRule),
			}}},
		},
		Target:      "main",
		Changed:     []string{"docs/guide.md"},
		Definitions: map[string]string{"test": ruleTestDefinition},
		sequences:   make(map[string]uint64),
	}
	world.Proposal = world.event(Event{Kind: "proposal.open", Actor: ruleTestAuthor, Base: ruleTestBase, Head: ruleTestHead})
	return world
}

// rule returns the world's single rule for editing.
func (w *ruleAcceptanceTestWorld) rule() *AutomergeRule {
	return &w.Policy.Automerge.Rules[0]
}

func (w *ruleAcceptanceTestWorld) event(event Event) StoredEvent {
	w.counter++
	w.sequences[event.Actor]++
	event.Protocol = protocolVersion
	event.Sequence = w.sequences[event.Actor]
	event.Timestamp = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC).Add(time.Duration(w.counter) * time.Second).Format(time.RFC3339)
	stored := StoredEvent{ID: eventID([]byte(fmt.Sprintf("rule-acceptance-test-event-%d", w.counter))), Event: event}
	if event.Kind != "proposal.open" {
		w.Events = append(w.Events, stored)
	}
	return stored
}

func (w *ruleAcceptanceTestWorld) review(actor, verdict string) StoredEvent {
	return w.event(Event{Kind: "review.submit", Actor: actor, Subject: w.Proposal.ID, Verdict: verdict})
}

func (w *ruleAcceptanceTestWorld) request(actor, pipeline string) StoredEvent {
	return w.event(Event{Kind: "run.request", Actor: actor, Subject: w.Proposal.ID, Pipeline: pipeline, Definition: ruleTestDefinition, Commit: ruleTestHead})
}

func (w *ruleAcceptanceTestWorld) requestWith(actor string, edit func(*Event)) StoredEvent {
	event := Event{Kind: "run.request", Actor: actor, Subject: w.Proposal.ID, Pipeline: "test", Definition: ruleTestDefinition, Commit: ruleTestHead}
	edit(&event)
	return w.event(event)
}

func (w *ruleAcceptanceTestWorld) result(runner string, request StoredEvent, outcome string) StoredEvent {
	return w.event(Event{Kind: "run.result", Actor: runner, Subject: request.ID, Pipeline: request.Event.Pipeline, Definition: request.Event.Definition, Commit: request.Event.Commit, Outcome: outcome})
}

func (w *ruleAcceptanceTestWorld) decision(maintainer, verdict string) StoredEvent {
	return w.event(Event{Kind: "proposal.decision", Actor: maintainer, Subject: w.Proposal.ID, Policy: ruleTestPolicyHash, Verdict: verdict, Body: "decided"})
}

// passingRun records a maintainer-requested test run with one passing result.
func (w *ruleAcceptanceTestWorld) passingRun() StoredEvent {
	return w.result(ruleTestRunner, w.request(ruleTestMaintainer, "test"), "passed")
}

func (w *ruleAcceptanceTestWorld) inputs() ruleInputs {
	return ruleInputs{
		Policy:              w.Policy,
		PolicyDigest:        ruleTestPolicyHash,
		Proposal:            w.Proposal,
		RuleName:            ruleTestRuleName,
		TargetBranch:        w.Target,
		Changed:             w.Changed,
		PipelineDefinitions: w.Definitions,
		Events:              w.Events,
	}
}

func (w *ruleAcceptanceTestWorld) evaluate() *ruleAcceptance {
	return evaluateRuleAcceptance(w.inputs())
}

func ruleTestIDs(events ...StoredEvent) []string {
	ids := make([]string, 0, len(events))
	for _, event := range events {
		ids = append(ids, event.ID)
	}
	sort.Strings(ids)
	return ids
}

func ruleTestVetoes(vetoes ...ruleVeto) []ruleVeto {
	sort.Slice(vetoes, func(i, j int) bool { return vetoes[i].Event.ID < vetoes[j].Event.ID })
	return vetoes
}

// ruleTestExpect is the full observable verdict a case asserts.
type ruleTestExpect struct {
	Approvals []string
	Vetoes    []ruleVeto
	Unmet     []ruleUnmet
	Evidence  []string
	Satisfied bool
}

func assertRuleAcceptance(t *testing.T, got *ruleAcceptance, want ruleTestExpect) {
	t.Helper()
	if gotApprovals := ruleTestIDs(got.Approvals...); !reflect.DeepEqual(gotApprovals, nonNilStrings(want.Approvals)) {
		t.Errorf("approvals = %v, want %v", gotApprovals, want.Approvals)
	}
	if !reflect.DeepEqual(got.Vetoes, want.Vetoes) {
		t.Errorf("vetoes = %+v, want %+v", got.Vetoes, want.Vetoes)
	}
	if !reflect.DeepEqual(got.Unmet, want.Unmet) {
		t.Errorf("unmet = %q, want %q", got.Unmet, want.Unmet)
	}
	if !reflect.DeepEqual(got.Evidence, nonNilStrings(want.Evidence)) {
		t.Errorf("evidence = %v, want %v", got.Evidence, want.Evidence)
	}
	if got.Satisfied() != want.Satisfied {
		t.Errorf("Satisfied() = %v, want %v", got.Satisfied(), want.Satisfied)
	}
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func ruleTestUnmet(kind, format string, args ...any) ruleUnmet {
	return ruleUnmet{Kind: kind, Text: fmt.Sprintf(format, args...)}
}

func TestRuleAcceptanceLiveHappyPath(t *testing.T) {
	world := newRuleAcceptanceTestWorld(t)
	approval := world.review(ruleTestGPT, "approve")
	passed := world.passingRun()

	got := world.evaluate()
	assertRuleAcceptance(t, got, ruleTestExpect{
		Approvals: ruleTestIDs(approval),
		Evidence:  ruleTestIDs(approval, passed),
		Satisfied: true,
	})
	if got.Rule == nil || got.Rule.Name != ruleTestRuleName || !got.Standing || got.HasRuleReviewers {
		t.Fatalf("rule = %+v, standing %v, has reviewers %v", got.Rule, got.Standing, got.HasRuleReviewers)
	}
	if want := []string{ruleTestGPT, ruleTestSonnet}; !reflect.DeepEqual(got.CountedReviewers, want) {
		t.Fatalf("counted reviewers = %v, want %v", got.CountedReviewers, want)
	}
	if got.RequiredApprovals != 1 || len(got.Pipelines) != 1 || got.Pipelines[0].Name != "test" || got.Pipelines[0].Required != 1 ||
		!reflect.DeepEqual(ruleTestIDs(got.Pipelines[0].Passed...), ruleTestIDs(passed)) || len(got.Pipelines[0].Failed) != 0 {
		t.Fatalf("thresholds = %d approvals, pipelines %+v", got.RequiredApprovals, got.Pipelines)
	}
}

func TestRuleAcceptanceLiveRuleReviewersRestrictCounting(t *testing.T) {
	world := newRuleAcceptanceTestWorld(t)
	world.rule().Reviewers = automergeReviewersOf(ruleTestGPT)
	sonnet := world.review(ruleTestSonnet, "approve")
	passed := world.passingRun()

	got := world.evaluate()
	assertRuleAcceptance(t, got, ruleTestExpect{
		Unmet:    []ruleUnmet{ruleTestUnmet(ruleUnmetApprovals, "rule approval threshold is unmet: 0 of 1 counted approvals")},
		Evidence: ruleTestIDs(passed),
	})
	if !got.HasRuleReviewers || !reflect.DeepEqual(got.CountedReviewers, []string{ruleTestGPT}) {
		t.Fatalf("counted reviewers = %v, has rule reviewers %v", got.CountedReviewers, got.HasRuleReviewers)
	}

	gpt := world.review(ruleTestGPT, "approve")
	got = world.evaluate()
	assertRuleAcceptance(t, got, ruleTestExpect{
		Approvals: ruleTestIDs(gpt),
		Evidence:  ruleTestIDs(gpt, passed),
		Satisfied: true,
	})
	for _, id := range got.Evidence {
		if id == sonnet.ID {
			t.Fatalf("evidence %v includes the uncounted approval %s", got.Evidence, sonnet.ID)
		}
	}
}

func TestRuleAcceptanceLiveChangeRequestVetoes(t *testing.T) {
	t.Run("uncounted trusted reviewer", func(t *testing.T) {
		world := newRuleAcceptanceTestWorld(t)
		world.rule().Reviewers = automergeReviewersOf(ruleTestGPT)
		gpt := world.review(ruleTestGPT, "approve")
		passed := world.passingRun()
		change := world.review(ruleTestSonnet, "request-changes")
		assertRuleAcceptance(t, world.evaluate(), ruleTestExpect{
			Approvals: ruleTestIDs(gpt),
			Vetoes:    []ruleVeto{{Kind: ruleVetoChangeRequest, Event: change}},
			Evidence:  ruleTestIDs(gpt, passed),
		})
	})
	t.Run("author change request with author approval disabled", func(t *testing.T) {
		world := newRuleAcceptanceTestWorld(t)
		world.Policy.Proposals.TrustedReviewers = append(world.Policy.Proposals.TrustedReviewers, ruleTestAuthor)
		gpt := world.review(ruleTestGPT, "approve")
		passed := world.passingRun()
		change := world.review(ruleTestAuthor, "request-changes")
		assertRuleAcceptance(t, world.evaluate(), ruleTestExpect{
			Approvals: ruleTestIDs(gpt),
			Vetoes:    []ruleVeto{{Kind: ruleVetoChangeRequest, Event: change}},
			Evidence:  ruleTestIDs(gpt, passed),
		})
	})
	t.Run("untrusted reviewer is ignored", func(t *testing.T) {
		world := newRuleAcceptanceTestWorld(t)
		gpt := world.review(ruleTestGPT, "approve")
		passed := world.passingRun()
		world.review(ruleTestOutsider, "request-changes")
		assertRuleAcceptance(t, world.evaluate(), ruleTestExpect{
			Approvals: ruleTestIDs(gpt),
			Evidence:  ruleTestIDs(gpt, passed),
			Satisfied: true,
		})
	})
	t.Run("later approval replaces the change request", func(t *testing.T) {
		world := newRuleAcceptanceTestWorld(t)
		world.review(ruleTestGPT, "request-changes")
		gpt := world.review(ruleTestGPT, "approve")
		passed := world.passingRun()
		assertRuleAcceptance(t, world.evaluate(), ruleTestExpect{
			Approvals: ruleTestIDs(gpt),
			Evidence:  ruleTestIDs(gpt, passed),
			Satisfied: true,
		})
	})
}

func TestRuleAcceptanceLiveAuthorApprovalByMode(t *testing.T) {
	world := newRuleAcceptanceTestWorld(t)
	world.Policy.Proposals.AllowAuthorApproval = true
	world.Policy.Proposals.TrustedReviewers = append(world.Policy.Proposals.TrustedReviewers, ruleTestAuthor)
	world.rule().Reviewers = automergeReviewersOf(ruleTestGPT, ruleTestAuthor)
	own := world.review(ruleTestAuthor, "approve")
	passed := world.passingRun()

	assertRuleAcceptance(t, world.evaluate(), ruleTestExpect{
		Unmet:    []ruleUnmet{ruleTestUnmet(ruleUnmetApprovals, "rule approval threshold is unmet: 0 of 1 counted approvals")},
		Evidence: ruleTestIDs(passed),
	})

	world.rule().Acceptance = automergeAcceptanceOf(automergeAcceptanceMaintainers)
	assertRuleAcceptance(t, world.evaluate(), ruleTestExpect{
		Approvals: ruleTestIDs(own),
		Unmet:     []ruleUnmet{ruleTestUnmet(ruleUnmetMode, "automerge rule %q has acceptance \"maintainers\" and needs maintainer acceptance", ruleTestRuleName)},
		Evidence:  ruleTestIDs(own, passed),
	})

	world.Policy.Proposals.AllowAuthorApproval = false
	assertRuleAcceptance(t, world.evaluate(), ruleTestExpect{
		Unmet: []ruleUnmet{
			ruleTestUnmet(ruleUnmetMode, "automerge rule %q has acceptance \"maintainers\" and needs maintainer acceptance", ruleTestRuleName),
			ruleTestUnmet(ruleUnmetApprovals, "rule approval threshold is unmet: 0 of 1 counted approvals"),
		},
		Evidence: ruleTestIDs(passed),
	})
}

func TestRuleAcceptanceLiveMaintainersModeWithReviewers(t *testing.T) {
	world := newRuleAcceptanceTestWorld(t)
	world.rule().Acceptance = automergeAcceptanceOf(automergeAcceptanceMaintainers)
	world.rule().Reviewers = automergeReviewersOf(ruleTestGPT)
	world.review(ruleTestSonnet, "approve")
	passed := world.passingRun()
	mode := ruleTestUnmet(ruleUnmetMode, "automerge rule %q has acceptance \"maintainers\" and needs maintainer acceptance", ruleTestRuleName)

	got := world.evaluate()
	assertRuleAcceptance(t, got, ruleTestExpect{
		Unmet:    []ruleUnmet{mode, ruleTestUnmet(ruleUnmetApprovals, "rule approval threshold is unmet: 0 of 1 counted approvals")},
		Evidence: ruleTestIDs(passed),
	})
	if got.Standing || !got.HasRuleReviewers || !reflect.DeepEqual(got.CountedReviewers, []string{ruleTestGPT}) {
		t.Fatalf("standing %v, has rule reviewers %v, counted %v", got.Standing, got.HasRuleReviewers, got.CountedReviewers)
	}
	if kinds := got.UnmetOf(ruleUnmetApprovals); len(kinds) != 1 || kinds[0].Kind != ruleUnmetApprovals {
		t.Fatalf("UnmetOf(approvals) = %+v", kinds)
	}

	gpt := world.review(ruleTestGPT, "approve")
	assertRuleAcceptance(t, world.evaluate(), ruleTestExpect{
		Approvals: ruleTestIDs(gpt),
		Unmet:     []ruleUnmet{mode},
		Evidence:  ruleTestIDs(gpt, passed),
	})
}

func TestRuleAcceptanceLiveRequesterAuthorization(t *testing.T) {
	missing := ruleTestUnmet(ruleUnmetPipeline, "pipeline %q has 0 of 1 passing trusted results", "test")

	world := newRuleAcceptanceTestWorld(t)
	gpt := world.review(ruleTestGPT, "approve")
	passed := world.result(ruleTestRunner, world.request(ruleTestOutsider, "test"), "passed")
	assertRuleAcceptance(t, world.evaluate(), ruleTestExpect{
		Approvals: ruleTestIDs(gpt),
		Unmet:     []ruleUnmet{missing},
		Evidence:  ruleTestIDs(gpt),
	})

	pipeline := world.Policy.Pipelines["test"]
	pipeline.Requesters = []string{ruleTestOutsider}
	world.Policy.Pipelines = map[string]PipelinePolicy{"test": pipeline}
	assertRuleAcceptance(t, world.evaluate(), ruleTestExpect{
		Approvals: ruleTestIDs(gpt),
		Evidence:  ruleTestIDs(gpt, passed),
		Satisfied: true,
	})
}

func TestRuleAcceptanceLiveLatestResultWins(t *testing.T) {
	missing := ruleTestUnmet(ruleUnmetPipeline, "pipeline %q has 0 of 1 passing trusted results", "test")
	t.Run("passed then failed", func(t *testing.T) {
		world := newRuleAcceptanceTestWorld(t)
		gpt := world.review(ruleTestGPT, "approve")
		request := world.request(ruleTestMaintainer, "test")
		world.result(ruleTestRunner, request, "passed")
		failed := world.result(ruleTestRunner, request, "failed")
		got := world.evaluate()
		assertRuleAcceptance(t, got, ruleTestExpect{
			Approvals: ruleTestIDs(gpt),
			Vetoes:    []ruleVeto{{Kind: ruleVetoFailure, Event: failed}},
			Unmet:     []ruleUnmet{missing},
			Evidence:  ruleTestIDs(gpt),
		})
		if !reflect.DeepEqual(ruleTestIDs(got.Pipelines[0].Failed...), ruleTestIDs(failed)) {
			t.Fatalf("failed = %v", ruleTestIDs(got.Pipelines[0].Failed...))
		}
	})
	t.Run("failed then passed", func(t *testing.T) {
		world := newRuleAcceptanceTestWorld(t)
		gpt := world.review(ruleTestGPT, "approve")
		request := world.request(ruleTestMaintainer, "test")
		world.result(ruleTestRunner, request, "failed")
		passed := world.result(ruleTestRunner, request, "passed")
		assertRuleAcceptance(t, world.evaluate(), ruleTestExpect{
			Approvals: ruleTestIDs(gpt),
			Evidence:  ruleTestIDs(gpt, passed),
			Satisfied: true,
		})
	})
	t.Run("later failure on another request", func(t *testing.T) {
		world := newRuleAcceptanceTestWorld(t)
		gpt := world.review(ruleTestGPT, "approve")
		world.result(ruleTestRunner, world.request(ruleTestMaintainer, "test"), "passed")
		failed := world.result(ruleTestRunner, world.request(ruleTestMaintainer, "test"), "failed")
		assertRuleAcceptance(t, world.evaluate(), ruleTestExpect{
			Approvals: ruleTestIDs(gpt),
			Vetoes:    []ruleVeto{{Kind: ruleVetoFailure, Event: failed}},
			Unmet:     []ruleUnmet{missing},
			Evidence:  ruleTestIDs(gpt),
		})
	})
	t.Run("untrusted runner is ignored", func(t *testing.T) {
		world := newRuleAcceptanceTestWorld(t)
		gpt := world.review(ruleTestGPT, "approve")
		request := world.request(ruleTestMaintainer, "test")
		passed := world.result(ruleTestRunner, request, "passed")
		world.result(ruleTestOutsider, request, "failed")
		assertRuleAcceptance(t, world.evaluate(), ruleTestExpect{
			Approvals: ruleTestIDs(gpt),
			Evidence:  ruleTestIDs(gpt, passed),
			Satisfied: true,
		})
	})
}

func TestRuleAcceptanceLiveRequestAndResultQualification(t *testing.T) {
	missing := ruleTestUnmet(ruleUnmetPipeline, "pipeline %q has 0 of 1 passing trusted results", "test")
	cases := []struct {
		name  string
		build func(w *ruleAcceptanceTestWorld)
	}{
		{"definition differs", func(w *ruleAcceptanceTestWorld) {
			w.result(ruleTestRunner, w.requestWith(ruleTestMaintainer, func(e *Event) { e.Definition = ruleTestOtherDefinition }), "passed")
		}},
		{"definition unavailable at head", func(w *ruleAcceptanceTestWorld) {
			w.Definitions = map[string]string{}
			w.passingRun()
		}},
		{"commit differs from head", func(w *ruleAcceptanceTestWorld) {
			w.result(ruleTestRunner, w.requestWith(ruleTestMaintainer, func(e *Event) { e.Commit = ruleTestOtherHead }), "passed")
		}},
		{"request for another proposal", func(w *ruleAcceptanceTestWorld) {
			w.result(ruleTestRunner, w.requestWith(ruleTestMaintainer, func(e *Event) { e.Subject = eventID([]byte("another proposal")) }), "passed")
		}},
		{"pipeline outside the base policy", func(w *ruleAcceptanceTestWorld) {
			w.Definitions["lint"] = ruleTestDefinition
			w.result(ruleTestRunner, w.request(ruleTestMaintainer, "lint"), "passed")
		}},
		{"result commit differs from request", func(w *ruleAcceptanceTestWorld) {
			request := w.request(ruleTestMaintainer, "test")
			w.event(Event{Kind: "run.result", Actor: ruleTestRunner, Subject: request.ID, Pipeline: "test", Definition: ruleTestDefinition, Commit: ruleTestOtherHead, Outcome: "passed"})
		}},
		{"result definition differs from request", func(w *ruleAcceptanceTestWorld) {
			request := w.request(ruleTestMaintainer, "test")
			w.event(Event{Kind: "run.result", Actor: ruleTestRunner, Subject: request.ID, Pipeline: "test", Definition: ruleTestOtherDefinition, Commit: ruleTestHead, Outcome: "passed"})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			world := newRuleAcceptanceTestWorld(t)
			gpt := world.review(ruleTestGPT, "approve")
			tc.build(world)
			assertRuleAcceptance(t, world.evaluate(), ruleTestExpect{
				Approvals: ruleTestIDs(gpt),
				Unmet:     []ruleUnmet{missing},
				Evidence:  ruleTestIDs(gpt),
			})
		})
	}
}

func TestRuleAcceptanceLiveMaintainerRejection(t *testing.T) {
	t.Run("current rejection vetoes", func(t *testing.T) {
		world := newRuleAcceptanceTestWorld(t)
		gpt := world.review(ruleTestGPT, "approve")
		passed := world.passingRun()
		reject := world.decision(ruleTestMaintainer, "reject")
		assertRuleAcceptance(t, world.evaluate(), ruleTestExpect{
			Approvals: ruleTestIDs(gpt),
			Vetoes:    []ruleVeto{{Kind: ruleVetoRejection, Event: reject}},
			Evidence:  ruleTestIDs(gpt, passed),
		})
	})
	t.Run("later accept by the same maintainer", func(t *testing.T) {
		world := newRuleAcceptanceTestWorld(t)
		gpt := world.review(ruleTestGPT, "approve")
		passed := world.passingRun()
		world.decision(ruleTestMaintainer, "reject")
		world.decision(ruleTestMaintainer, "accept")
		assertRuleAcceptance(t, world.evaluate(), ruleTestExpect{
			Approvals: ruleTestIDs(gpt),
			Evidence:  ruleTestIDs(gpt, passed),
			Satisfied: true,
		})
	})
	t.Run("rejection under another policy digest or by a non-maintainer", func(t *testing.T) {
		world := newRuleAcceptanceTestWorld(t)
		gpt := world.review(ruleTestGPT, "approve")
		passed := world.passingRun()
		world.event(Event{Kind: "proposal.decision", Actor: ruleTestMaintainer, Subject: world.Proposal.ID, Policy: eventID([]byte("other policy")), Verdict: "reject", Body: "old"})
		world.decision(ruleTestOutsider, "reject")
		assertRuleAcceptance(t, world.evaluate(), ruleTestExpect{
			Approvals: ruleTestIDs(gpt),
			Evidence:  ruleTestIDs(gpt, passed),
			Satisfied: true,
		})
	})
	t.Run("one maintainer of two rejects", func(t *testing.T) {
		world := newRuleAcceptanceTestWorld(t)
		world.Policy.Maintainers = []string{ruleTestMaintainer, ruleTestMaintainer2}
		gpt := world.review(ruleTestGPT, "approve")
		passed := world.passingRun()
		world.decision(ruleTestMaintainer, "accept")
		reject := world.decision(ruleTestMaintainer2, "reject")
		assertRuleAcceptance(t, world.evaluate(), ruleTestExpect{
			Approvals: ruleTestIDs(gpt),
			Vetoes:    []ruleVeto{{Kind: ruleVetoRejection, Event: reject}},
			Evidence:  ruleTestIDs(gpt, passed),
		})
	})
}

func TestRuleAcceptanceLiveRuleMissing(t *testing.T) {
	cases := []struct {
		name string
		edit func(w *ruleAcceptanceTestWorld) ruleInputs
		text string
	}{
		{"automerge disabled", func(w *ruleAcceptanceTestWorld) ruleInputs {
			w.Policy.Automerge = nil
			return w.inputs()
		}, "automerge is disabled in the signed base policy"},
		{"rule absent", func(w *ruleAcceptanceTestWorld) ruleInputs {
			inputs := w.inputs()
			inputs.RuleName = "absent"
			return inputs
		}, `automerge rule "absent" does not exist in the signed base policy`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			world := newRuleAcceptanceTestWorld(t)
			world.review(ruleTestGPT, "approve")
			world.review(ruleTestSonnet, "request-changes")
			world.passingRun()
			got := evaluateRuleAcceptance(tc.edit(world))
			want := &ruleAcceptance{RequiredApprovals: 1, Unmet: []ruleUnmet{{Kind: ruleUnmetMissing, Text: tc.text}}}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("evaluation = %+v, want %+v", got, want)
			}
			if got.Satisfied() {
				t.Fatal("a missing rule is satisfied")
			}
		})
	}
}

func TestRuleAcceptanceLiveEligibilityConditions(t *testing.T) {
	cases := []struct {
		name  string
		edit  func(w *ruleAcceptanceTestWorld)
		unmet []ruleUnmet
	}{
		{"target branch mismatch", func(w *ruleAcceptanceTestWorld) { w.Target = "release" },
			[]ruleUnmet{ruleTestUnmet(ruleUnmetTargetBranch, `rule target "main" does not match release`)}},
		{"target branch unchecked when empty", func(w *ruleAcceptanceTestWorld) { w.Target = "" }, nil},
		{"author outside the rule", func(w *ruleAcceptanceTestWorld) { w.rule().Authors = []string{ruleTestOutsider} },
			[]ruleUnmet{ruleTestUnmet(ruleUnmetAuthor, "proposal author is not allowed by the rule")}},
		{"path outside the rule", func(w *ruleAcceptanceTestWorld) { w.Changed = []string{"docs/guide.md", "src/main.go", "docs"} },
			[]ruleUnmet{
				ruleTestUnmet(ruleUnmetPath, `changed path "src/main.go" is outside the rule allowlist`),
				ruleTestUnmet(ruleUnmetPath, `changed path "docs" is outside the rule allowlist`),
			}},
		{"no changed paths", func(w *ruleAcceptanceTestWorld) { w.Changed = nil },
			[]ruleUnmet{ruleTestUnmet(ruleUnmetPath, "proposal has no changed paths")}},
		{"policy amendment", func(w *ruleAcceptanceTestWorld) {
			w.rule().Paths = []string{"docs/", ".hn/"}
			w.Changed = []string{".hn/policy.json"}
		}, []ruleUnmet{ruleTestUnmet(ruleUnmetManualPath, "policy amendments require manual merge")}},
		{"protected change", func(w *ruleAcceptanceTestWorld) {
			w.Policy.ProtectedChecks = &ProtectedChecksPolicy{Paths: []string{"docs/locked/"}, TrustedReviewers: []string{ruleTestGPT}, RequiredApprovals: 1}
			w.Changed = []string{"docs/locked/a.md"}
		}, []ruleUnmet{ruleTestUnmet(ruleUnmetManualPath, "protected check changes require manual merge")}},
		{"policy amendment under protected checks", func(w *ruleAcceptanceTestWorld) {
			w.Policy.ProtectedChecks = &ProtectedChecksPolicy{Paths: []string{"scripts/"}, TrustedReviewers: []string{ruleTestGPT}, RequiredApprovals: 1}
			w.rule().Paths = []string{"docs/", ".hn/"}
			w.Changed = []string{".hn/policy.json"}
		}, []ruleUnmet{
			ruleTestUnmet(ruleUnmetManualPath, "policy amendments require manual merge"),
			ruleTestUnmet(ruleUnmetManualPath, "protected check changes require manual merge"),
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			world := newRuleAcceptanceTestWorld(t)
			gpt := world.review(ruleTestGPT, "approve")
			passed := world.passingRun()
			tc.edit(world)
			assertRuleAcceptance(t, world.evaluate(), ruleTestExpect{
				Approvals: ruleTestIDs(gpt),
				Unmet:     tc.unmet,
				Evidence:  ruleTestIDs(gpt, passed),
				Satisfied: len(tc.unmet) == 0,
			})
		})
	}
}

func TestRuleAcceptanceLiveUnmetOrder(t *testing.T) {
	world := newRuleAcceptanceTestWorld(t)
	world.rule().Acceptance = automergeAcceptanceOf(automergeAcceptanceMaintainers)
	world.rule().Authors = []string{ruleTestOutsider}
	world.Policy.ProtectedChecks = &ProtectedChecksPolicy{Paths: []string{"scripts/"}, TrustedReviewers: []string{ruleTestGPT}, RequiredApprovals: 1}
	world.Target = "release"
	world.Changed = []string{".hn/policy.json", "src/main.go"}
	pipeline := world.Policy.Pipelines["test"]
	world.Policy.Pipelines = map[string]PipelinePolicy{"test": pipeline, "lint": {RequiredResults: 2, TrustedRunners: []string{ruleTestRunner}}}
	world.Definitions["lint"] = ruleTestDefinition
	lint := world.result(ruleTestRunner, world.request(ruleTestMaintainer, "lint"), "passed")

	got := world.evaluate()
	want := []ruleUnmet{
		ruleTestUnmet(ruleUnmetMode, "automerge rule %q has acceptance \"maintainers\" and needs maintainer acceptance", ruleTestRuleName),
		ruleTestUnmet(ruleUnmetTargetBranch, `rule target "main" does not match release`),
		ruleTestUnmet(ruleUnmetAuthor, "proposal author is not allowed by the rule"),
		ruleTestUnmet(ruleUnmetManualPath, "policy amendments require manual merge"),
		ruleTestUnmet(ruleUnmetManualPath, "protected check changes require manual merge"),
		ruleTestUnmet(ruleUnmetPath, `changed path ".hn/policy.json" is outside the rule allowlist`),
		ruleTestUnmet(ruleUnmetPath, `changed path "src/main.go" is outside the rule allowlist`),
		ruleTestUnmet(ruleUnmetApprovals, "rule approval threshold is unmet: 0 of 1 counted approvals"),
		ruleTestUnmet(ruleUnmetPipeline, "pipeline %q has 1 of 2 passing trusted results", "lint"),
		ruleTestUnmet(ruleUnmetPipeline, "pipeline %q has 0 of 1 passing trusted results", "test"),
	}
	assertRuleAcceptance(t, got, ruleTestExpect{Unmet: want, Evidence: ruleTestIDs(lint)})
	if names := []string{got.Pipelines[0].Name, got.Pipelines[1].Name}; !reflect.DeepEqual(names, []string{"lint", "test"}) {
		t.Fatalf("pipeline order = %v", names)
	}
	if paths := got.UnmetOf(ruleUnmetPath, ruleUnmetManualPath); !reflect.DeepEqual(paths, want[3:7]) {
		t.Fatalf("UnmetOf(path, manual-path) = %q, want %q", paths, want[3:7])
	}
}

func TestRuleAcceptanceLiveDeterministic(t *testing.T) {
	world := newRuleAcceptanceTestWorld(t)
	world.Policy.Maintainers = []string{ruleTestMaintainer, ruleTestMaintainer2}
	world.Policy.Proposals.RequiredApprovals = 2
	world.Policy.Pipelines = map[string]PipelinePolicy{
		"test": {RequiredResults: 2, TrustedRunners: []string{ruleTestRunner, ruleTestSonnet}},
		"lint": {RequiredResults: 1, TrustedRunners: []string{ruleTestRunner}, Requesters: []string{ruleTestGPT}},
		"docs": {RequiredResults: 1, TrustedRunners: []string{ruleTestRunner}},
	}
	world.Definitions = map[string]string{"test": ruleTestDefinition, "lint": ruleTestDefinition, "docs": ruleTestDefinition}
	world.review(ruleTestGPT, "request-changes")
	world.review(ruleTestGPT, "approve")
	world.review(ruleTestSonnet, "approve")
	world.review(ruleTestOutsider, "request-changes")
	first := world.request(ruleTestMaintainer, "test")
	second := world.request(ruleTestMaintainer2, "test")
	world.result(ruleTestRunner, first, "failed")
	world.result(ruleTestRunner, second, "passed")
	world.result(ruleTestSonnet, first, "passed")
	world.result(ruleTestSonnet, second, "failed")
	world.result(ruleTestRunner, world.request(ruleTestGPT, "lint"), "passed")
	world.result(ruleTestRunner, world.request(ruleTestOutsider, "docs"), "passed")
	world.result(ruleTestRunner, world.request(ruleTestMaintainer, "docs"), "failed")
	world.decision(ruleTestMaintainer, "reject")
	world.decision(ruleTestMaintainer2, "reject")
	world.decision(ruleTestMaintainer2, "accept")

	baseline := world.evaluate()
	if len(baseline.Approvals) != 2 || len(baseline.Vetoes) != 3 || len(baseline.Evidence) != 4 {
		t.Fatalf("baseline approvals %d, vetoes %+v, evidence %v", len(baseline.Approvals), baseline.Vetoes, baseline.Evidence)
	}
	random := rand.New(rand.NewSource(20261026))
	for i := 0; i < 10; i++ {
		shuffled := append([]StoredEvent(nil), world.Events...)
		random.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })
		inputs := world.inputs()
		inputs.Events = shuffled
		if got := evaluateRuleAcceptance(inputs); !reflect.DeepEqual(got, baseline) {
			t.Fatalf("shuffle %d changed the evaluation:\n got %+v\nwant %+v", i, got, baseline)
		}
	}
}
