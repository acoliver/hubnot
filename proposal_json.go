package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// proposalListJSON is one proposal in `hn proposal list --json`: the signed
// facts plus the evaluation every other command uses, so a reviewing tool
// shows what hn itself decides instead of parsing its prose.
type proposalListJSON struct {
	ID            string                `json:"id"`
	Title         string                `json:"title"`
	Body          string                `json:"body"`
	Actor         string                `json:"actor"`
	ActorName     string                `json:"actorName"`
	Timestamp     string                `json:"timestamp"`
	Base          string                `json:"base"`
	Head          string                `json:"head"`
	PolicyDigest  string                `json:"policyDigest,omitempty"`
	CodeAvailable bool                  `json:"codeAvailable"`
	CodeMatches   bool                  `json:"codeMatches"`
	Lineage       proposalLineageJSON   `json:"lineage"`
	Changed       []string              `json:"changedPaths"`
	Policy        bool                  `json:"changesPolicy"`
	Protected     []string              `json:"protectedPaths"`
	Rules         []proposalRuleJSON    `json:"rules"`
	Pipelines     []proposalPipeJSON    `json:"pipelines"`
	Requests      []proposalRequestJSON `json:"runRequests"`
	Approvals     []proposalFactJSON    `json:"approvals"`
	Changes       []proposalFactJSON    `json:"changeRequests"`
	Accepts       []proposalFactJSON    `json:"accepts"`
	Rejects       []proposalFactJSON    `json:"rejects"`
	Merges        []proposalFactJSON    `json:"merges"`
	Ready         bool                  `json:"ready"`
	Accepted      bool                  `json:"accepted"`
	Rejected      bool                  `json:"rejected"`
	Merged        bool                  `json:"merged"`
	Error         string                `json:"error,omitempty"`
}

type proposalLineageJSON struct {
	Root        string   `json:"root"`
	Predecessor string   `json:"predecessor,omitempty"`
	Successors  []string `json:"successors"`
	Superseded  bool     `json:"superseded"`
	Closed      bool     `json:"closed"`
	Merged      bool     `json:"merged"`
	Conflict    bool     `json:"mergeConflict"`
}

// proposalRuleJSON reports, per automerge rule of the base policy, what in
// the change the rule does not cover: those paths need a maintainer.
type proposalRuleJSON struct {
	Name             string   `json:"name"`
	Standing         bool     `json:"standing"`
	AuthorAllowed    bool     `json:"authorAllowed"`
	OutsideAllowlist []string `json:"outsideAllowlist"`
}

type proposalPipeJSON struct {
	Name     string             `json:"name"`
	Required int                `json:"required"`
	Passed   []proposalFactJSON `json:"passed"`
	Failed   []proposalFactJSON `json:"failed"`
}

type proposalRequestJSON struct {
	ID        string             `json:"id"`
	Pipeline  string             `json:"pipeline"`
	Actor     string             `json:"actor"`
	ActorName string             `json:"actorName"`
	Timestamp string             `json:"timestamp"`
	Results   []proposalFactJSON `json:"results"`
}

type proposalFactJSON struct {
	ID        string `json:"id"`
	Actor     string `json:"actor"`
	ActorName string `json:"actorName"`
	Timestamp string `json:"timestamp"`
	Outcome   string `json:"outcome,omitempty"`
	Rule      string `json:"rule,omitempty"`
	Body      string `json:"body,omitempty"`
}

func proposalFacts(events []StoredEvent) []proposalFactJSON {
	out := make([]proposalFactJSON, 0, len(events))
	for _, stored := range events {
		e := stored.Event
		out = append(out, proposalFactJSON{ID: stored.ID, Actor: e.Actor, ActorName: e.ActorName, Timestamp: e.Timestamp, Outcome: e.Outcome, Rule: e.Rule, Body: e.Body})
	}
	return out
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// proposalJSON evaluates one proposal. An evaluation that fails is reported
// in Error with the signed facts still filled in, so one unreadable
// proposal never hides the others.
func proposalJSON(proposal *StoredEvent, events []StoredEvent, lineage *lineageIndex) proposalListJSON {
	e := proposal.Event
	item := proposalListJSON{ID: proposal.ID, Title: e.Title, Body: e.Body, Actor: e.Actor, ActorName: e.ActorName,
		Timestamp: e.Timestamp, Base: e.Base, Head: e.Head,
		Changed: []string{}, Protected: []string{}, Rules: []proposalRuleJSON{}, Pipelines: []proposalPipeJSON{},
		Requests: []proposalRequestJSON{}, Approvals: []proposalFactJSON{}, Changes: []proposalFactJSON{},
		Accepts: []proposalFactJSON{}, Rejects: []proposalFactJSON{}, Merges: []proposalFactJSON{}}
	if state, err := lineage.state(proposal.ID); err == nil {
		item.Lineage = proposalLineageJSON{Root: state.RootID, Predecessor: state.PredecessorID, Successors: nonNil(state.SuccessorIDs),
			Superseded: state.Superseded, Closed: state.LineageClosed, Merged: state.Merged, Conflict: state.MergeConflict}
		if title, err := lineageDisplayTitle(lineage, state); err == nil {
			item.Title = title
		}
	} else {
		item.Lineage.Successors = []string{}
	}
	for index := range events {
		request := &events[index]
		if request.Event.Kind != "run.request" || request.Event.Subject != proposal.ID {
			continue
		}
		item.Requests = append(item.Requests, proposalRequestJSON{ID: request.ID, Pipeline: request.Event.Pipeline, Actor: request.Event.Actor,
			ActorName: request.Event.ActorName, Timestamp: request.Event.Timestamp, Results: proposalFacts(currentRunResults(events, request.ID))})
	}
	evaluation, err := evaluateProposal(proposal, events)
	if err != nil {
		item.Error = err.Error()
		return item
	}
	item.PolicyDigest = evaluation.PolicyDigest
	item.CodeAvailable, item.CodeMatches = evaluation.CodeAvailable, evaluation.CodeMatches
	item.Ready, item.Accepted, item.Rejected, item.Merged = evaluation.Ready, evaluation.Accepted, evaluation.Rejected, evaluation.Merged
	item.Approvals, item.Changes = proposalFacts(evaluation.Approvals), proposalFacts(evaluation.ChangeRequests)
	item.Accepts, item.Rejects, item.Merges = proposalFacts(evaluation.AcceptDecisions), proposalFacts(evaluation.RejectDecisions), proposalFacts(evaluation.MergeEvents)
	for _, pipeline := range evaluation.Pipelines {
		item.Pipelines = append(item.Pipelines, proposalPipeJSON{Name: pipeline.Name, Required: pipeline.Required,
			Passed: proposalFacts(pipeline.Passed), Failed: proposalFacts(pipeline.Failed)})
	}
	if !evaluation.CodeAvailable {
		return item
	}
	changed, err := changedPathsAt("", e.Base, e.Head)
	if err != nil {
		item.Error = err.Error()
		return item
	}
	item.Changed = nonNil(changed)
	for _, p := range changed {
		if protectedPathMatches(".hn/policy.json", p) {
			item.Policy = true
		}
	}
	item.Protected = nonNil(changedProtectedPaths(evaluation.Policy, changed))
	if evaluation.Policy.Automerge != nil {
		for _, rule := range evaluation.Policy.Automerge.Rules {
			outside := []string{}
			for _, p := range changed {
				if !automergePathAllowed(rule.Paths, p) {
					outside = append(outside, p)
				}
			}
			item.Rules = append(item.Rules, proposalRuleJSON{Name: rule.Name, Standing: rule.Acceptance.Standing(),
				AuthorAllowed: actorListed(e.Actor, rule.Authors), OutsideAllowlist: outside})
		}
	}
	return item
}

// cmdProposalListJSON prints every proposal as one JSON document, oldest
// first, wrapped like the issue commands' JSON.
func cmdProposalListJSON() error {
	events, err := collectEvents()
	if err != nil {
		return err
	}
	lineage, err := buildLineageIndex(events)
	if err != nil {
		return err
	}
	proposals := proposalEvents(events)
	sort.SliceStable(proposals, func(i, j int) bool { return proposals[i].Event.Timestamp < proposals[j].Event.Timestamp })
	items := make([]proposalListJSON, 0, len(proposals))
	for index := range proposals {
		items = append(items, proposalJSON(&proposals[index], events, lineage))
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(map[string]any{"schema": "hn.proposal-list/1", "ok": true, "data": map[string]any{"items": items}}); err != nil {
		return fmt.Errorf("write proposal list: %w", err)
	}
	return nil
}
