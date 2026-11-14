package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

func init() { localCommands["jit-check"] = cmdJITCheck }

type jitRequestView struct {
	ID     string `json:"id"`
	Failed bool   `json:"failed"`
	Passed bool   `json:"passed"`
}
type jitCheckView struct {
	ID         string           `json:"id"`
	Root       string           `json:"root"`
	Author     string           `json:"author"`
	Base       string           `json:"base"`
	Head       string           `json:"head"`
	Target     string           `json:"target"`
	Policy     string           `json:"policy"`
	Definition string           `json:"definition"`
	Eligible   bool             `json:"eligible"`
	Ready      bool             `json:"ready"`
	Merged     bool             `json:"merged"`
	Blockers   []string         `json:"blockers"`
	Requests   []jitRequestView `json:"requests"`
}

// This read-only scheduling preflight reuses ordinary rule derivation. Missing
// pipeline passes alone do not prohibit preparation. It grants no authority.
func cmdJITCheck(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: hn jit-check FULL_PROPOSAL --target OID --requester ACTOR --rule NAME --pipeline NAME")
	}
	if err := requireFullEventID(args[0]); err != nil {
		return err
	}
	f := quietFlags("jit-check")
	remote := f.String("remote", "origin", "configured remote with fresh imported facts")
	target := f.String("target", "", "exact observed main")
	runner := f.String("runner", "", "optional executing worker actor")
	requester := f.String("requester", "", "configured coordinator")
	rule := f.String("rule", "ordinary", "ordinary standing rule")
	pipeline := f.String("pipeline", "gate", "pipeline")
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	if f.NArg() != 0 || !validFullGitObjectID(*target) || !validActorFingerprint(*requester) || (*runner != "" && !validActorFingerprint(*runner)) {
		return fmt.Errorf("full target and requester required")
	}
	events, err := collectEvents()
	if err != nil {
		return err
	}
	p, err := resolveFullEvent(events, args[0])
	if err != nil {
		return err
	}
	if !isProposalKind(p.Event.Kind) {
		return fmt.Errorf("not a proposal")
	}
	if err := guardProposalDependencies("jit-check", p, events); err != nil {
		return jitProposalRefusal(p.ID, err)
	}
	if err := guardBasePolicy("jit-check", p.Event.Base, replicationProposal, p.ID); err != nil {
		return err
	}
	if err := guardBasePolicy("jit-check", *target, replicationProposal, p.ID); err != nil {
		return err
	}
	if err := requireProposalCode(p); err != nil {
		return jitProposalRefusal(p.ID, err)
	}
	if err := guardPipelineDefinition("jit-check", p.Event.Head, *pipeline, p.ID, ""); err != nil {
		return jitProposalRefusal(p.ID, err)
	}
	evaluation, err := evaluateProposal(p, events)
	if err != nil {
		return err
	}
	changed, err := changedPathsAt("", p.Event.Base, p.Event.Head)
	if err != nil {
		return err
	}
	_, _, definition, err := loadPipeline(p.Event.Head, *pipeline)
	if err != nil {
		return err
	}
	current, _, _, err := loadPolicy(*target)
	if err != nil {
		return err
	}
	v := jitCheckView{ID: p.ID, Root: evaluation.Lineage.RootID, Author: p.Event.Actor, Base: p.Event.Base, Head: p.Event.Head, Target: *target, Policy: evaluation.PolicyDigest, Definition: definition, Merged: evaluation.Merged, Blockers: []string{}, Requests: []jitRequestView{}}
	if err := requireLineageTerminalEligibility("jit-check", p.ID, evaluation.Lineage); err != nil {
		v.Blockers = append(v.Blockers, err.Error())
	}
	if evaluation.Merged {
		v.Blockers = append(v.Blockers, "merged")
	}
	inputs := automergeRuleInputs(p, evaluation, *rule, changed, events)
	inputs.TargetBranch = "main"
	derived := evaluateRuleAcceptance(inputs)
	v.Ready = derived.Satisfied()
	selection, _, err := loadReplicationSelection(*remote)
	if err != nil {
		return err
	}
	if !selection.All && !selection.AllTrusted && !actorListed(*requester, selection.Actors) {
		return fmt.Errorf("replication selection omits coordinator")
	}
	for _, policy := range []PolicyDocument{evaluation.Policy, current} {
		if missing := automergeUnselectedActors(selection, policy); len(missing) > 0 {
			return fmt.Errorf("replication selection omits trusted veto actors; select --all-trusted")
		}

		inputs.Policy = policy
		derived = evaluateRuleAcceptance(inputs)
		for _, unmet := range derived.Unmet {
			if unmet.Kind != ruleUnmetPipeline {
				v.Blockers = append(v.Blockers, unmet.Text)
			}
		}
		for _, veto := range derived.Vetoes {
			v.Blockers = append(v.Blockers, veto.Kind+": "+veto.Event.ID)
		}
		r := ruleByName(policy, *rule)
		if r == nil || !actorListed(*requester, r.Actors) {
			v.Blockers = append(v.Blockers, "coordinator is not a rule actor")
		}
		if _, exists := policy.Pipelines[*pipeline]; !exists {
			v.Blockers = append(v.Blockers, "configured pipeline is absent from policy")
		}
		if *runner != "" && !actorListed(*runner, policy.Pipelines[*pipeline].TrustedRunners) {
			v.Blockers = append(v.Blockers, "worker is not a trusted pipeline runner")
		}
		if !mayRequestRun(policy, *pipeline, *requester) {
			v.Blockers = append(v.Blockers, "coordinator is not a pipeline requester")
		}
	}
	// Preserve unresolved lineage reviews before preparation can supersede them.
	index, err := buildLineageIndex(events)
	if err != nil {
		return err
	}
	for _, family := range proposalEvents(events) {
		state, err := index.state(family.ID)
		if err != nil {
			return err
		}
		if state.RootID != v.Root {
			continue
		}
		ev, err := evaluateProposal(&family, events)
		if err != nil {
			return err
		}
		if ev.Rejected {
			v.Blockers = append(v.Blockers, "lineage rejection: "+family.ID)
		}
		for _, cr := range ev.ChangeRequests {
			answered := false
			for _, approval := range events {
				if approval.Event.Kind != "review.submit" || approval.Event.Verdict != "approve" || approval.Event.Actor != cr.Event.Actor || approval.Event.Sequence <= cr.Event.Sequence {
					continue
				}
				st, err := index.state(approval.Event.Subject)
				if err == nil && st.RootID == v.Root {
					answered = true
				}
			}
			if !answered {
				v.Blockers = append(v.Blockers, "lineage change-request: "+cr.ID)
			}
		}
	}
	inputs.Policy = evaluation.Policy
	for _, request := range events {
		if request.Event.Actor != *requester || request.Event.Pipeline != *pipeline || !ruleRequestQualifies(evaluation.Policy, *p, inputs.PipelineDefinitions, request) {
			continue
		}
		q := jitRequestView{ID: request.ID}
		for _, result := range currentRunResults(events, request.ID) {
			if ruleResultQualifies(evaluation.Policy, *p, inputs.PipelineDefinitions, result, request) {
				if result.Event.Outcome == "passed" {
					q.Passed = true
				} else {
					q.Failed = true
				}
			}
		}
		v.Requests = append(v.Requests, q)
	}
	v.Eligible = len(v.Blockers) == 0
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"schema": "hn.jit-check/1", "ok": true, "data": v})
}

// Guard helpers preserve unknown failures. Only a typed, exact-proposal
// code/dependency refusal may cross the successful local-refusal boundary.
func jitProposalRefusal(id string, err error) error {
	var refusal *proposalDependencyRefusal
	if !errors.As(err, &refusal) || refusal.ProposalID != id {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"schema": "hn.jit-check/1", "ok": true,
		"data": map[string]any{"scope": "proposal", "id": id, "refusal": err.Error()},
	})
}
