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
	return ruleApprovalDefect(policy, rule, proposal, review) == ""
}

// ruleApprovalDefect is the single predicate behind ruleApprovalCounts. It
// returns "" for a counting approval and otherwise the first reason it does
// not count, which the fact validator reports (check 8).
func ruleApprovalDefect(policy PolicyDocument, rule *AutomergeRule, proposal StoredEvent, review StoredEvent) string {
	switch {
	case rule == nil:
		return "no rule counts it"
	case review.Event.Kind != "review.submit":
		return fmt.Sprintf("it has kind %q", review.Event.Kind)
	case review.Event.Subject != proposal.ID:
		return "it reviews another proposal"
	case review.Event.Verdict != "approve":
		return fmt.Sprintf("its verdict is %q", review.Event.Verdict)
	}
	actor := review.Event.Actor
	if !actorListed(actor, policy.Proposals.TrustedReviewers) {
		return fmt.Sprintf("reviewer %s is not a trusted reviewer", actor)
	}
	if !rule.Reviewers.IsZero() && !actorListed(actor, rule.Reviewers.Actors()) {
		return fmt.Sprintf("reviewer %s is not one of the rule's reviewers", actor)
	}
	authorCounts := !rule.Acceptance.Standing() && policy.Proposals.AllowAuthorApproval
	if actor == proposal.Event.Actor && !authorCounts {
		return "it is the candidate author's own approval"
	}
	return ""
}

// ruleRequestQualifies reports whether request is a run request whose results
// can count for proposal: it names the candidate head, a base-policy pipeline
// whose definition digest at the head is known and matches, and a signer
// allowed by mayRequestRun (C-009).
func ruleRequestQualifies(policy PolicyDocument, proposal StoredEvent, definitions map[string]string, request StoredEvent) bool {
	return ruleRequestDefect(policy, proposal, definitions, request) == ""
}

// ruleRequestDefect is the single predicate behind ruleRequestQualifies: ""
// when request qualifies, otherwise the first reason it does not.
func ruleRequestDefect(policy PolicyDocument, proposal StoredEvent, definitions map[string]string, request StoredEvent) string {
	switch {
	case request.Event.Kind != "run.request":
		return fmt.Sprintf("request %s has kind %q", request.ID, request.Event.Kind)
	case request.Event.Subject != proposal.ID:
		return fmt.Sprintf("request %s is for another proposal", request.ID)
	case request.Event.Commit != proposal.Event.Head:
		return fmt.Sprintf("request %s names commit %q, not the candidate head %s", request.ID, request.Event.Commit, proposal.Event.Head)
	}
	pipeline := request.Event.Pipeline
	if _, exists := policy.Pipelines[pipeline]; !exists {
		return fmt.Sprintf("request %s names pipeline %q, which the base policy does not define", request.ID, pipeline)
	}
	definition, known := definitions[pipeline]
	if !known {
		return fmt.Sprintf("the definition of pipeline %q is unavailable at the candidate head", pipeline)
	}
	if definition != request.Event.Definition {
		return fmt.Sprintf("request %s names definition %q, not the pipeline %q definition %s at the candidate head", request.ID, request.Event.Definition, pipeline, definition)
	}
	if !mayRequestRun(policy, pipeline, request.Event.Actor) {
		return fmt.Sprintf("request %s is signed by %s, who is neither a maintainer nor a requester of pipeline %q", request.ID, request.Event.Actor, pipeline)
	}
	return ""
}

// ruleResultQualifies reports whether result answers a qualifying request
// with the same pipeline, commit and definition and is signed by a trusted
// runner of that pipeline. The caller checks the outcome.
func ruleResultQualifies(policy PolicyDocument, proposal StoredEvent, definitions map[string]string, result, request StoredEvent) bool {
	return ruleResultDefect(policy, proposal, definitions, result, request) == ""
}

// ruleResultDefect is the single predicate behind ruleResultQualifies: ""
// when result qualifies, otherwise the first reason it does not.
func ruleResultDefect(policy PolicyDocument, proposal StoredEvent, definitions map[string]string, result, request StoredEvent) string {
	if result.Event.Kind != "run.result" || result.Event.Subject != request.ID {
		return fmt.Sprintf("it does not answer request %s", request.ID)
	}
	if defect := ruleRequestDefect(policy, proposal, definitions, request); defect != "" {
		return defect
	}
	if result.Event.Pipeline != request.Event.Pipeline || result.Event.Commit != request.Event.Commit || result.Event.Definition != request.Event.Definition {
		return fmt.Sprintf("its pipeline, commit or definition differs from request %s", request.ID)
	}
	if !actorListed(result.Event.Actor, policy.Pipelines[request.Event.Pipeline].TrustedRunners) {
		return fmt.Sprintf("runner %s is not a trusted runner of pipeline %q", result.Event.Actor, request.Event.Pipeline)
	}
	return ""
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
	names := rulePipelineNames(inputs.Policy)
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

// Rule merge fact check names, in the order validateRuleMergeFact runs them
// (contracts/rule-merge-fact.md "Check names"). The names are stable: every
// surface reports them and tests match them.
const (
	ruleCheckPolicyDigest      = "policy-digest"
	ruleCheckRuleExists        = "rule-exists"
	ruleCheckRuleAcceptance    = "rule-acceptance"
	ruleCheckRuleSigner        = "rule-signer"
	ruleCheckRuleAuthor        = "rule-author"
	ruleCheckManualPath        = "manual-path"
	ruleCheckChangedPaths      = "changed-paths"
	ruleCheckEvidenceItem      = "evidence-item"
	ruleCheckEvidenceDuplicate = "evidence-duplicate"
	ruleCheckApprovals         = "approval-threshold"
	ruleCheckPipelines         = "pipeline-threshold"
)

// ruleFactError is the first failed check of a rule merge fact. Surfaces wrap
// it in their own style and recover it with errors.As.
type ruleFactError struct {
	FactID  string // full event ID of the rule merge fact
	Check   string
	EventID string // the evidence item, when the check is about one
	Detail  string
}

func (e *ruleFactError) Error() string {
	return fmt.Sprintf("rule merge %s fails %s: %s", e.FactID, e.Check, e.Detail)
}

// ruleFactFailure builds a ruleFactError. Details can quote event-supplied
// text, so they always pass through safeText.
func ruleFactFailure(merge StoredEvent, check, eventID, format string, args ...any) *ruleFactError {
	return &ruleFactError{FactID: merge.ID, Check: check, EventID: eventID, Detail: safeText(fmt.Sprintf(format, args...))}
}

// rulePipelineNames returns the base-policy pipeline names in sorted order.
func rulePipelineNames(policy PolicyDocument) []string {
	names := make([]string, 0, len(policy.Pipelines))
	for name := range policy.Pipelines {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ruleFactResult is a passing run result cited by a fact, with the request
// it answers.
type ruleFactResult struct {
	Result, Request StoredEvent
}

// validateRuleMergeFact is path (b) of the merge fact invariant: it judges a
// rule merge fact against its base policy, the candidate's changed paths, the
// pipeline definition digests at the head and the fact's listed evidence
// only (FR-012, FR-015), and returns nil or the first failed check as a
// *ruleFactError. The rule judged is the fact's own claim, merge.Event.Rule;
// inputs.RuleName and inputs.Events are not read. Callers have already run
// validateProposalMergeBinding, and reach this only through admitMergeFact.
func validateRuleMergeFact(inputs ruleInputs, merge StoredEvent, byID map[string]StoredEvent) error {
	name := merge.Event.Rule
	if merge.Event.Policy != inputs.PolicyDigest {
		return ruleFactFailure(merge, ruleCheckPolicyDigest, "", "policy %s is not the base policy digest %s", merge.Event.Policy, inputs.PolicyDigest)
	}
	rule := ruleByName(inputs.Policy, name)
	if rule == nil {
		return ruleFactFailure(merge, ruleCheckRuleExists, "", "rule %q does not exist in the base policy", name)
	}
	if !rule.Acceptance.Standing() {
		return ruleFactFailure(merge, ruleCheckRuleAcceptance, "", "rule %q has acceptance %q", name, rule.Acceptance.Mode())
	}
	if !actorListed(merge.Event.Actor, rule.Actors) {
		return ruleFactFailure(merge, ruleCheckRuleSigner, "", "signer %s is not an executing actor of rule %q", merge.Event.Actor, name)
	}
	if !ruleAuthorAllowed(rule, inputs.Proposal) {
		return ruleFactFailure(merge, ruleCheckRuleAuthor, "", "author %s is not allowed by rule %q", inputs.Proposal.Event.Actor, name)
	}
	for _, p := range inputs.Changed {
		// The manual-path predicate is per path, so asking it one path at a
		// time names the first offending path without a second predicate.
		if policyAmendment, protectedChange := ruleManualPathChanges(inputs.Policy, []string{p}); policyAmendment || protectedChange {
			return ruleFactFailure(merge, ruleCheckManualPath, "", "changed path %q requires manual merge", p)
		}
	}
	if len(inputs.Changed) == 0 {
		return ruleFactFailure(merge, ruleCheckChangedPaths, "", "the candidate has no changed paths")
	}
	if disallowed := ruleDisallowedPaths(rule, inputs.Changed); len(disallowed) > 0 {
		return ruleFactFailure(merge, ruleCheckChangedPaths, "", "changed path %q is outside rule %q", disallowed[0], name)
	}

	approvals, results, failure := ruleFactEvidence(inputs, rule, merge, byID)
	if failure != nil {
		return failure
	}

	reviewers := make(map[string]string, len(approvals))
	for _, approval := range approvals {
		actor := approval.Event.Actor
		if previous, exists := reviewers[actor]; exists {
			return ruleFactFailure(merge, ruleCheckEvidenceDuplicate, approval.ID, "approvals %s and %s are both by reviewer %s", previous, approval.ID, actor)
		}
		reviewers[actor] = approval.ID
	}
	runnersByPipeline := make(map[string]map[string]string)
	for _, cited := range results {
		pipeline, runner := cited.Request.Event.Pipeline, cited.Result.Event.Actor
		runners := runnersByPipeline[pipeline]
		if runners == nil {
			runners = make(map[string]string)
			runnersByPipeline[pipeline] = runners
		}
		if previous, exists := runners[runner]; exists {
			return ruleFactFailure(merge, ruleCheckEvidenceDuplicate, cited.Result.ID, "run results %s and %s are both by runner %s for pipeline %q", previous, cited.Result.ID, runner, pipeline)
		}
		runners[runner] = cited.Result.ID
	}

	if required := inputs.Policy.Proposals.RequiredApprovals; len(reviewers) < required {
		return ruleFactFailure(merge, ruleCheckApprovals, "", "%d of %d counted approvals", len(reviewers), required)
	}
	for _, pipeline := range rulePipelineNames(inputs.Policy) {
		if required := inputs.Policy.Pipelines[pipeline].RequiredResults; len(runnersByPipeline[pipeline]) < required {
			return ruleFactFailure(merge, ruleCheckPipelines, "", "pipeline %q has %d of %d passing trusted results", pipeline, len(runnersByPipeline[pipeline]), required)
		}
	}
	return nil
}

// ruleFactEvidence runs check 8 over the evidence in listed order. Every
// item must be a counting approval or a passing run result; qualification
// uses the same predicates as evaluateRuleAcceptance. It reads byID only for
// the listed items and the requests their results answer (FR-015).
func ruleFactEvidence(inputs ruleInputs, rule *AutomergeRule, merge StoredEvent, byID map[string]StoredEvent) ([]StoredEvent, []ruleFactResult, *ruleFactError) {
	var approvals []StoredEvent
	var results []ruleFactResult
	for _, id := range merge.Event.Evidence {
		item, exists := byID[id]
		if !exists {
			return nil, nil, ruleFactFailure(merge, ruleCheckEvidenceItem, id, "evidence %s is unavailable", id)
		}
		switch item.Event.Kind {
		case "review.submit":
			if defect := ruleApprovalDefect(inputs.Policy, rule, inputs.Proposal, item); defect != "" {
				return nil, nil, ruleFactFailure(merge, ruleCheckEvidenceItem, id, "review %s is not a counting approval of rule %q: %s", id, rule.Name, defect)
			}
			approvals = append(approvals, item)
		case "run.result":
			if item.Event.Outcome != "passed" {
				return nil, nil, ruleFactFailure(merge, ruleCheckEvidenceItem, id, "run result %s has outcome %q, not \"passed\"", id, item.Event.Outcome)
			}
			request, exists := byID[item.Event.Subject]
			if !exists {
				return nil, nil, ruleFactFailure(merge, ruleCheckEvidenceItem, id, "run result %s answers request %s, which is unavailable", id, item.Event.Subject)
			}
			if defect := ruleResultDefect(inputs.Policy, inputs.Proposal, inputs.PipelineDefinitions, item, request); defect != "" {
				return nil, nil, ruleFactFailure(merge, ruleCheckEvidenceItem, id, "run result %s does not qualify: %s", id, defect)
			}
			results = append(results, ruleFactResult{Result: item, Request: request})
		default:
			return nil, nil, ruleFactFailure(merge, ruleCheckEvidenceItem, id, "evidence %s has kind %s, which a rule merge fact cannot cite", id, item.Event.Kind)
		}
	}
	return approvals, results, nil
}

// ruleObjectAccess is how one surface reads the Git objects path (b) needs:
// the policy and its digest at a commit, the sorted changed paths between two
// commits (bounded by the changed-path limit), and a pipeline definition
// digest at a commit. Each surface supplies its own store and error style.
type ruleObjectAccess struct {
	PolicyAt       func(commit string) (PolicyDocument, string, error)
	ChangedPaths   func(base, head string) ([]string, error)
	PipelineDigest func(head, pipeline string) (string, error)
}

type ruleMemoPolicy struct {
	policy PolicyDocument
	digest string
	err    error
}

type ruleMemoPaths struct {
	paths []string
	err   error
}

type ruleMemoDigest struct {
	digest string
	err    error
}

// ruleValidationMemo caches object reads for one validation pass (NFR-005,
// tasks decision 5): the base policy per commit, changed paths per
// (base, head) and pipeline digests per (head, pipeline). Errors are cached
// too, so a failure is not retried within the pass. A memo belongs to one
// pass on one goroutine; create a new one for each pass.
type ruleValidationMemo struct {
	access   ruleObjectAccess
	policies map[string]ruleMemoPolicy
	changed  map[[2]string]ruleMemoPaths
	digests  map[[2]string]ruleMemoDigest

	// Calls made to access, read by tests to prove the bound.
	policyCalls         int
	changedPathCalls    int
	pipelineDigestCalls int
}

func newRuleValidationMemo(access ruleObjectAccess) *ruleValidationMemo {
	return &ruleValidationMemo{
		access:   access,
		policies: make(map[string]ruleMemoPolicy),
		changed:  make(map[[2]string]ruleMemoPaths),
		digests:  make(map[[2]string]ruleMemoDigest),
	}
}

func (m *ruleValidationMemo) policyAt(commit string) (PolicyDocument, string, error) {
	entry, cached := m.policies[commit]
	if !cached {
		m.policyCalls++
		if m.access.PolicyAt == nil {
			entry.err = fmt.Errorf("rule merge validation cannot read the policy at %s", commit)
		} else {
			entry.policy, entry.digest, entry.err = m.access.PolicyAt(commit)
		}
		m.policies[commit] = entry
	}
	return entry.policy, entry.digest, entry.err
}

func (m *ruleValidationMemo) changedPaths(base, head string) ([]string, error) {
	key := [2]string{base, head}
	entry, cached := m.changed[key]
	if !cached {
		m.changedPathCalls++
		if m.access.ChangedPaths == nil {
			entry.err = fmt.Errorf("rule merge validation cannot read the changed paths from %s to %s", base, head)
		} else {
			entry.paths, entry.err = m.access.ChangedPaths(base, head)
		}
		m.changed[key] = entry
	}
	return entry.paths, entry.err
}

func (m *ruleValidationMemo) pipelineDigest(head, pipeline string) (string, error) {
	key := [2]string{head, pipeline}
	entry, cached := m.digests[key]
	if !cached {
		m.pipelineDigestCalls++
		if m.access.PipelineDigest == nil {
			entry.err = fmt.Errorf("rule merge validation cannot read pipeline %q at %s", pipeline, head)
		} else {
			entry.digest, entry.err = m.access.PipelineDigest(head, pipeline)
		}
		m.digests[key] = entry
	}
	return entry.digest, entry.err
}

// inputsForFact loads the path (b) inputs of merge for proposal: the policy at
// the candidate base, the changed paths base..head and the digest of every
// base-policy pipeline at the head. A pipeline digest error leaves that
// pipeline out of PipelineDefinitions, so its results cannot qualify. A
// policy or changed-path error is returned unchanged for the caller to wrap
// in its own style; it never becomes an empty change list (FR-014).
func (m *ruleValidationMemo) inputsForFact(merge, proposal StoredEvent) (ruleInputs, error) {
	policy, digest, err := m.policyAt(proposal.Event.Base)
	if err != nil {
		return ruleInputs{}, err
	}
	changed, err := m.changedPaths(proposal.Event.Base, proposal.Event.Head)
	if err != nil {
		return ruleInputs{}, err
	}
	definitions := make(map[string]string, len(policy.Pipelines))
	for _, pipeline := range rulePipelineNames(policy) {
		if definition, err := m.pipelineDigest(proposal.Event.Head, pipeline); err == nil {
			definitions[pipeline] = definition
		}
	}
	return ruleInputs{
		Policy:              policy,
		PolicyDigest:        digest,
		Proposal:            proposal,
		RuleName:            merge.Event.Rule,
		Changed:             changed,
		PipelineDefinitions: definitions,
	}, nil
}

// validateRuleMergeFactWith loads the inputs of merge through memo and runs
// validateRuleMergeFact. Surfaces call it from the rulePath of admitMergeFact
// after reporting any missing path (b) object in their own dependency style.
func validateRuleMergeFactWith(memo *ruleValidationMemo, merge, proposal StoredEvent, byID map[string]StoredEvent) error {
	inputs, err := memo.inputsForFact(merge, proposal)
	if err != nil {
		return err
	}
	return validateRuleMergeFact(inputs, merge, byID)
}

// mergeAuthority names the path through which a proposal.merged fact is valid.
type mergeAuthority string

const (
	mergeByMaintainers mergeAuthority = "maintainers"
	mergeByRule        mergeAuthority = "rule"
)

// admitMergeFact applies the union validity of proposal.merged facts
// (decision 01M5VSEHEVA82N9T64ZH1P1Q2A). maintainerPath is the caller's
// existing acceptance merge fact check, unchanged. rulePath reports any
// missing path (b) input in the caller's own dependency style and then
// calls validateRuleMergeFact. Every fact-admission surface calls this;
// none orders the two paths itself (FR-024).
func admitMergeFact(merge StoredEvent, maintainerPath, rulePath func() error) (mergeAuthority, error) {
	maintainerErr := maintainerPath()
	if maintainerErr == nil {
		return mergeByMaintainers, nil
	}
	if merge.Event.Rule == "" {
		return "", maintainerErr
	}
	if err := rulePath(); err != nil {
		return "", err
	}
	return mergeByRule, nil
}
