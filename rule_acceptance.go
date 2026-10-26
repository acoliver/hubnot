package main

import (
	"fmt"
	"sort"
)

// The rule derivation is shared by every surface that judges standing
// acceptance (decision 01M5VEZEC0EGQWXKCPTJRECHAV). Nothing in this file runs
// Git or touches the filesystem: callers load objects from their own store
// and pass them in, so local, quarantine and hn serve verdicts cannot drift
// (NFR-002).

// ruleInputs is everything the rule derivation reads. Callers fill it
// from their own object store; the derivation itself runs no Git.
type ruleInputs struct {
	Policy              PolicyDocument    // base policy at the candidate's signed base
	PolicyDigest        string            // its digest
	Proposal            StoredEvent       // the candidate
	RuleName            string            // requested rule
	TargetBranch        string            // branch being advanced; "" skips the check
	Changed             []string          // sorted changed paths base..head
	PipelineDefinitions map[string]string // pipeline name -> definition digest at head; absent means unavailable
	Events              []StoredEvent     // current events (live) or the fact index values (validation)
}

// Unmet condition kinds, in the order evaluateRuleAcceptance reports them.
const (
	ruleUnmetMissing      = "rule-missing"
	ruleUnmetMode         = "rule-mode"
	ruleUnmetTargetBranch = "target-branch"
	ruleUnmetAuthor       = "author"
	ruleUnmetManualPath   = "manual-path"
	ruleUnmetPath         = "path"
	ruleUnmetApprovals    = "approvals"
	ruleUnmetPipeline     = "pipeline"
)

// Veto kinds: current evidence that blocks a rule whatever the thresholds say.
const (
	ruleVetoChangeRequest = "change-request"
	ruleVetoFailure       = "failure"
	ruleVetoRejection     = "rejection"
)

// ruleUnmet is one blocker sentence; Text is the contract wording.
type ruleUnmet struct{ Kind, Text string }

// ruleVeto names a current event that blocks the rule.
type ruleVeto struct {
	Kind  string // "change-request", "failure" or "rejection"
	Event StoredEvent
}

// rulePipeline is the qualifying evidence for one base-policy pipeline.
type rulePipeline struct {
	Name     string
	Required int
	Passed   []StoredEvent // latest passing result per trusted runner, sorted
	Failed   []StoredEvent // latest failing result per trusted runner, sorted
}

// ruleAcceptance is the live verdict of evaluateRuleAcceptance.
type ruleAcceptance struct {
	Rule              *AutomergeRule
	Standing          bool
	CountedReviewers  []string // rule reviewers or all trusted reviewers, sorted
	HasRuleReviewers  bool     // true when the rule's reviewers field is set
	Approvals         []StoredEvent
	RequiredApprovals int
	Pipelines         []rulePipeline // in sorted pipeline-name order
	Vetoes            []ruleVeto     // sorted by event ID
	Unmet             []ruleUnmet    // in the order of the ruleUnmet* kinds
	Evidence          []string       // sorted, unique
}

// Satisfied reports whether the named rule exists, carries standing
// acceptance, and has no unmet condition and no veto.
func (r *ruleAcceptance) Satisfied() bool {
	return r != nil && r.Rule != nil && r.Standing && len(r.Unmet) == 0 && len(r.Vetoes) == 0
}

// UnmetOf returns the unmet conditions of the given kinds, in report order.
func (r *ruleAcceptance) UnmetOf(kinds ...string) []ruleUnmet {
	if r == nil {
		return nil
	}
	var result []ruleUnmet
	for _, unmet := range r.Unmet {
		for _, kind := range kinds {
			if unmet.Kind == kind {
				result = append(result, unmet)
				break
			}
		}
	}
	return result
}

// ruleByName returns a copy of the named automerge rule, or nil when the
// policy has no automerge section or no rule with that name.
func ruleByName(policy PolicyDocument, name string) *AutomergeRule {
	if policy.Automerge == nil {
		return nil
	}
	for _, rule := range policy.Automerge.Rules {
		if rule.Name == name {
			copy := rule
			return &copy
		}
	}
	return nil
}

// ruleCountedReviewers returns the sorted reviewers whose approvals the rule
// counts: its reviewers field when set, otherwise proposals.trustedReviewers.
func ruleCountedReviewers(policy PolicyDocument, rule *AutomergeRule) []string {
	if rule != nil && !rule.Reviewers.IsZero() {
		return sortedUniqueStrings(rule.Reviewers.Actors()...)
	}
	return sortedUniqueStrings(policy.Proposals.TrustedReviewers...)
}

// ruleApprovalCounts reports whether review is an approval the rule counts
// (FR-005, FR-006). Under acceptance mode rule the author never counts,
// whatever allowAuthorApproval says; under maintainers it matches
// evaluateProposalEvidence.
func ruleApprovalCounts(policy PolicyDocument, rule *AutomergeRule, proposal StoredEvent, review StoredEvent) bool {
	if rule == nil || review.Event.Kind != "review.submit" || review.Event.Subject != proposal.ID || review.Event.Verdict != "approve" {
		return false
	}
	actor := review.Event.Actor
	if !actorListed(actor, policy.Proposals.TrustedReviewers) {
		return false
	}
	if !rule.Reviewers.IsZero() && !actorListed(actor, rule.Reviewers.Actors()) {
		return false
	}
	if rule.Acceptance.Standing() {
		return actor != proposal.Event.Actor
	}
	return policy.Proposals.AllowAuthorApproval || actor != proposal.Event.Actor
}

// ruleRequestQualifies reports whether request is a run request whose results
// can count for proposal: it names the candidate head, a base-policy pipeline
// whose definition digest at the head is known and matches, and a signer
// allowed by mayRequestRun (C-009).
func ruleRequestQualifies(policy PolicyDocument, proposal StoredEvent, definitions map[string]string, request StoredEvent) bool {
	if request.Event.Kind != "run.request" || request.Event.Subject != proposal.ID || request.Event.Commit != proposal.Event.Head {
		return false
	}
	pipeline := request.Event.Pipeline
	if _, exists := policy.Pipelines[pipeline]; !exists {
		return false
	}
	definition, known := definitions[pipeline]
	if !known || definition != request.Event.Definition {
		return false
	}
	return mayRequestRun(policy, pipeline, request.Event.Actor)
}

// ruleResultQualifies reports whether result answers a qualifying request
// with the same pipeline, commit and definition and is signed by a trusted
// runner of that pipeline. The caller checks the outcome.
func ruleResultQualifies(policy PolicyDocument, proposal StoredEvent, definitions map[string]string, result, request StoredEvent) bool {
	if result.Event.Kind != "run.result" || result.Event.Subject != request.ID {
		return false
	}
	if !ruleRequestQualifies(policy, proposal, definitions, request) {
		return false
	}
	if result.Event.Pipeline != request.Event.Pipeline || result.Event.Commit != request.Event.Commit || result.Event.Definition != request.Event.Definition {
		return false
	}
	return actorListed(result.Event.Actor, policy.Pipelines[request.Event.Pipeline].TrustedRunners)
}

// ruleLater reports whether candidate supersedes previous for one actor.
// Sequence decides, as in evaluateProposalEvidence; the event ID breaks a
// tie only so that the result never depends on event order.
func ruleLater(candidate, previous StoredEvent) bool {
	if candidate.Event.Sequence != previous.Event.Sequence {
		return candidate.Event.Sequence > previous.Event.Sequence
	}
	return candidate.ID < previous.ID
}

// ruleLivePipelines keeps, per base-policy pipeline, the latest qualifying
// result of each trusted runner, mirroring the pipeline loop of
// evaluateProposalEvidence with the stricter qualification above.
func ruleLivePipelines(inputs ruleInputs) []rulePipeline {
	names := make([]string, 0, len(inputs.Policy.Pipelines))
	for name := range inputs.Policy.Pipelines {
		names = append(names, name)
	}
	sort.Strings(names)
	pipelines := make([]rulePipeline, 0, len(names))
	for _, name := range names {
		latestByRunner := make(map[string]StoredEvent)
		for _, request := range inputs.Events {
			if request.Event.Pipeline != name || !ruleRequestQualifies(inputs.Policy, inputs.Proposal, inputs.PipelineDefinitions, request) {
				continue
			}
			for _, result := range currentRunResults(inputs.Events, request.ID) {
				if !ruleResultQualifies(inputs.Policy, inputs.Proposal, inputs.PipelineDefinitions, result, request) {
					continue
				}
				previous, found := latestByRunner[result.Event.Actor]
				if !found || ruleLater(result, previous) {
					latestByRunner[result.Event.Actor] = result
				}
			}
		}
		pipeline := rulePipeline{Name: name, Required: inputs.Policy.Pipelines[name].RequiredResults}
		for _, result := range latestByRunner {
			if result.Event.Outcome == "passed" {
				pipeline.Passed = append(pipeline.Passed, result)
			} else {
				pipeline.Failed = append(pipeline.Failed, result)
			}
		}
		sortStoredEvents(pipeline.Passed)
		sortStoredEvents(pipeline.Failed)
		pipelines = append(pipelines, pipeline)
	}
	return pipelines
}

// ruleAuthorAllowed reports whether the candidate author is listed by the rule.
func ruleAuthorAllowed(rule *AutomergeRule, proposal StoredEvent) bool {
	return rule != nil && actorListed(proposal.Event.Actor, rule.Authors)
}

// ruleManualPathChanges reports the two changes that always need a manual
// merge, using the same matches as inspectAutomerge.
func ruleManualPathChanges(policy PolicyDocument, changed []string) (policyAmendment, protectedChange bool) {
	for _, p := range changed {
		if protectedPathMatches(".hn/policy.json", p) {
			policyAmendment = true
			break
		}
	}
	return policyAmendment, len(changedProtectedPaths(policy, changed)) > 0
}

// ruleDisallowedPaths returns the changed paths outside the rule allowlist,
// in changed order.
func ruleDisallowedPaths(rule *AutomergeRule, changed []string) []string {
	var disallowed []string
	for _, p := range changed {
		if rule == nil || !automergePathAllowed(rule.Paths, p) {
			disallowed = append(disallowed, p)
		}
	}
	return disallowed
}

// ruleCurrentRejections returns the latest decision of each maintainer under
// the base policy digest when that decision is a rejection, as
// evaluateProposalEvidence selects current decisions.
func ruleCurrentRejections(inputs ruleInputs) []StoredEvent {
	current := make(map[string]StoredEvent)
	for _, stored := range inputs.Events {
		if stored.Event.Kind != "proposal.decision" || stored.Event.Subject != inputs.Proposal.ID || stored.Event.Policy != inputs.PolicyDigest || !actorListed(stored.Event.Actor, inputs.Policy.Maintainers) {
			continue
		}
		previous, exists := current[stored.Event.Actor]
		if !exists || ruleLater(stored, previous) {
			current[stored.Event.Actor] = stored
		}
	}
	var rejections []StoredEvent
	for _, decision := range current {
		if decision.Event.Verdict == "reject" {
			rejections = append(rejections, decision)
		}
	}
	sortEventsByID(rejections)
	return rejections
}

func sortEventsByID(events []StoredEvent) {
	sort.Slice(events, func(i, j int) bool { return events[i].ID < events[j].ID })
}

// evaluateRuleAcceptance judges the current evidence for the named rule. It
// never fails: every problem is an unmet condition or a veto. Its callers
// are the keyless preview and local execution (WP10, WP11) and the hn serve
// protected verdict (WP14).
func evaluateRuleAcceptance(inputs ruleInputs) *ruleAcceptance {
	result := &ruleAcceptance{RequiredApprovals: inputs.Policy.Proposals.RequiredApprovals}
	unmet := func(kind, format string, args ...any) {
		result.Unmet = append(result.Unmet, ruleUnmet{Kind: kind, Text: fmt.Sprintf(format, args...)})
	}

	rule := ruleByName(inputs.Policy, inputs.RuleName)
	if inputs.Policy.Automerge == nil {
		unmet(ruleUnmetMissing, "automerge is disabled in the signed base policy")
		return result
	}
	if rule == nil {
		unmet(ruleUnmetMissing, "automerge rule %q does not exist in the signed base policy", inputs.RuleName)
		return result
	}
	result.Rule = rule
	result.Standing = rule.Acceptance.Standing()
	result.HasRuleReviewers = !rule.Reviewers.IsZero()
	result.CountedReviewers = ruleCountedReviewers(inputs.Policy, rule)

	current := currentReviews(inputs.Events, inputs.Proposal.ID)
	for _, review := range current {
		if ruleApprovalCounts(inputs.Policy, rule, inputs.Proposal, review) {
			result.Approvals = append(result.Approvals, review)
		}
	}
	sortEventsByID(result.Approvals)
	result.Pipelines = ruleLivePipelines(inputs)

	for _, review := range current {
		if review.Event.Verdict == "request-changes" && actorListed(review.Event.Actor, inputs.Policy.Proposals.TrustedReviewers) {
			result.Vetoes = append(result.Vetoes, ruleVeto{Kind: ruleVetoChangeRequest, Event: review})
		}
	}
	for _, pipeline := range result.Pipelines {
		for _, failure := range pipeline.Failed {
			result.Vetoes = append(result.Vetoes, ruleVeto{Kind: ruleVetoFailure, Event: failure})
		}
	}
	for _, rejection := range ruleCurrentRejections(inputs) {
		result.Vetoes = append(result.Vetoes, ruleVeto{Kind: ruleVetoRejection, Event: rejection})
	}
	sort.SliceStable(result.Vetoes, func(i, j int) bool { return result.Vetoes[i].Event.ID < result.Vetoes[j].Event.ID })

	if !result.Standing {
		unmet(ruleUnmetMode, "automerge rule %q has acceptance \"maintainers\" and needs maintainer acceptance", rule.Name)
	}
	if inputs.TargetBranch != "" && inputs.TargetBranch != rule.TargetBranch {
		unmet(ruleUnmetTargetBranch, "rule target %q does not match %s", rule.TargetBranch, inputs.TargetBranch)
	}
	if !ruleAuthorAllowed(rule, inputs.Proposal) {
		unmet(ruleUnmetAuthor, "proposal author is not allowed by the rule")
	}
	policyAmendment, protectedChange := ruleManualPathChanges(inputs.Policy, inputs.Changed)
	if policyAmendment {
		unmet(ruleUnmetManualPath, "policy amendments require manual merge")
	}
	if protectedChange {
		unmet(ruleUnmetManualPath, "protected check changes require manual merge")
	}
	if len(inputs.Changed) == 0 {
		unmet(ruleUnmetPath, "proposal has no changed paths")
	}
	for _, p := range ruleDisallowedPaths(rule, inputs.Changed) {
		unmet(ruleUnmetPath, "changed path %q is outside the rule allowlist", p)
	}
	if len(result.Approvals) < result.RequiredApprovals {
		unmet(ruleUnmetApprovals, "rule approval threshold is unmet: %d of %d counted approvals", len(result.Approvals), result.RequiredApprovals)
	}
	for _, pipeline := range result.Pipelines {
		if len(pipeline.Passed) < pipeline.Required {
			unmet(ruleUnmetPipeline, "pipeline %q has %d of %d passing trusted results", pipeline.Name, len(pipeline.Passed), pipeline.Required)
		}
	}

	evidence := make([]string, 0, len(result.Approvals))
	for _, approval := range result.Approvals {
		evidence = append(evidence, approval.ID)
	}
	for _, pipeline := range result.Pipelines {
		for _, passed := range pipeline.Passed {
			evidence = append(evidence, passed.ID)
		}
	}
	result.Evidence = sortedUniqueStrings(evidence...)
	return result
}
