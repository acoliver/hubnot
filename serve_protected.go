package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Protected branch rule (FR-008, FR-009, C-004, C-005). A protected branch
// moves only by fast-forward to the signed head of a candidate that is
// accepted and ready under its signed base policy and under the destination
// policy, the policy at the tip the update replaces. The checks follow the
// "Protected branch" table of
// kitty-specs/hn-serve-01M5P0NP/contracts/push-verdict.md and the first
// failing step is reported. The server only decides: it never creates a
// commit and never signs anything (C-005, C-006).

// Steps of the protected branch table, in the order they run.
const (
	serveProtectedStepDeletion       = 1
	serveProtectedStepCreation       = 2
	serveProtectedStepStaleTip       = 3
	serveProtectedStepDestPolicy     = 4
	serveProtectedStepFastForward    = 5
	serveProtectedStepCandidate      = 6
	serveProtectedStepBasePolicy     = 7
	serveProtectedStepCode           = 8
	serveProtectedStepBlocked        = 9
	serveProtectedStepBaseReady      = 10
	serveProtectedStepDestReady      = 11
	serveProtectedStepAdvanceAllowed = 12
)

// serveProtectedVerdict is the outcome of one protected update. Step is 0
// and Rejection nil when the update passes; Candidate is then the candidate
// that passed. Otherwise Step is the failing step of the reported
// candidate, Candidate names it when the failure belongs to one, and
// Rejection is the line to print. Candidates lists every candidate whose
// signed head is the pushed commit, sorted by event ID.
type serveProtectedVerdict struct {
	Step       int
	Candidate  string
	Candidates []string
	Rejection  *serveRejection
}

// serveProtectedUpdate holds what every candidate check of one update
// reads: the tip T, the pushed head H, the destination policy at T and the
// facts known to this push.
type serveProtectedUpdate struct {
	ctx        *serveVerdictContext
	ref        string
	tip, head  string
	destPolicy PolicyDocument
	events     []StoredEvent
	// pushedCode maps a candidate ID to the commit of a proposal ref this
	// push creates for it.
	pushedCode map[string]string
	hostedCode map[string]string
}

// evaluateServeProtectedUpdate runs the twelve protected branch checks for
// one ref update. Facts accepted from actor refs earlier in the push count
// as facts; code counts when it is hosted or arrives as a proposal ref in
// the same push.
func evaluateServeProtectedUpdate(ctx *serveVerdictContext, update serveRefUpdate, options servePushOptions) serveProtectedVerdict {
	tip, head := update.Old, update.New

	// Step 1: protected branches are never deleted.
	if update.Deletes() {
		return serveProtectedReject(serveProtectedStepDeletion, "", nil, &serveRejection{
			Code:     serveReasonProtectedDeletion,
			IDs:      []string{tip},
			Sentence: update.Ref + " is protected and cannot be deleted.",
		})
	}

	// Step 2: only the administrator creates a protected branch (OQ-11).
	if update.Creates() {
		return serveProtectedReject(serveProtectedStepCreation, "", nil, &serveRejection{
			Code:     serveReasonProtectedPolicyInvalid,
			IDs:      []string{head},
			Sentence: update.Ref + " does not exist on this server and a push cannot create a protected branch; the administrator creates it with hn serve repo.",
		})
	}

	// Step 3: the request read an older tip. This runs before any policy
	// or candidate check, so the loser of a race always gets this code.
	if options.ExpectedTip != "" && options.ExpectedTip != tip {
		return serveProtectedReject(serveProtectedStepStaleTip, "", nil, &serveRejection{
			Code:     serveReasonProtectedStaleTip,
			IDs:      []string{options.ExpectedTip, tip},
			Sentence: update.Ref + " moved to " + tip + " after this request read " + options.ExpectedTip + ".",
		})
	}

	// Step 4: the destination policy at the current tip.
	destPolicy, _, err := loadReplicationPolicyAt(ctx.GitDir, ctx.GitDir, tip)
	if err != nil {
		return serveProtectedReject(serveProtectedStepDestPolicy, "", nil, &serveRejection{
			Code: serveReasonProtectedPolicyInvalid,
			IDs:  []string{tip},
			Sentence: update.Ref + " is at " + tip + ", which has no valid .hn/policy.json (" + serveDiagnostic(err, ctx.GitDir) +
				"); every update of this branch is refused until the administrator repairs it.",
		})
	}

	// Step 5: fast-forward only. The server never merges or rebases.
	if !serveIsAncestor(ctx.GitDir, tip, head) {
		return serveProtectedReject(serveProtectedStepFastForward, "", nil, &serveRejection{
			Code:     serveReasonProtectedNotFastForward,
			IDs:      []string{tip, head},
			Next:     "revise the proposal onto " + tip + " with hn proposal revise",
			Sentence: update.Ref + " is at " + tip + " and " + head + " does not descend from it.",
		})
	}

	hosted, err := ctx.HostedEvents()
	if err != nil {
		return serveProtectedReject(serveProtectedStepCandidate, "", nil, serveHostedFactsRejection())
	}
	events := make([]StoredEvent, 0, len(hosted)+len(ctx.PushFacts))
	events = append(append(events, hosted...), ctx.PushFacts...)

	// Step 6: the candidates whose signed head is H.
	candidates := serveProtectedCandidates(events, head, options.Advance)
	ids := make([]string, len(candidates))
	for index, candidate := range candidates {
		ids[index] = candidate.ID
	}
	if len(candidates) == 0 {
		sentence := update.Ref + " is protected and " + serveShortCommit(head) + " has no accepted, CI-tested proposal."
		if options.Advance != "" {
			sentence = "hn-advance names " + options.Advance + ", which is not a known proposal whose signed head is " + head + "."
		}
		return serveProtectedReject(serveProtectedStepCandidate, "", nil, &serveRejection{
			Code:     serveReasonProtectedNoProposal,
			IDs:      serveCompactIDs(head, options.Advance),
			Missing:  slices.Clone(serveEvidenceKinds),
			Next:     "open a proposal for " + head,
			Sentence: sentence,
		})
	}

	hostedCode, err := serveHostedProposalCode(ctx.GitDir)
	if err != nil {
		return serveProtectedReject(serveProtectedStepCode, "", ids, serveHostedFactsRejection())
	}
	check := &serveProtectedUpdate{
		ctx:        ctx,
		ref:        update.Ref,
		tip:        tip,
		head:       head,
		destPolicy: destPolicy,
		events:     events,
		pushedCode: servePushedProposalCode(ctx.PushUpdates),
		hostedCode: hostedCode,
	}

	// Steps 7 to 11 per candidate. Any passing candidate admits the update;
	// otherwise the latest failing step is reported, ties going to the
	// lowest event ID, which is the first in the sorted list.
	var reported *serveProtectedCandidateResult
	var passed *serveProtectedCandidateResult
	for index := range candidates {
		result := check.candidate(&candidates[index])
		if result.rejection == nil {
			passed = &result
			break
		}
		if reported == nil || result.step > reported.step {
			reported = &result
		}
	}
	if passed == nil {
		rejection := reported.rejection
		rejection.IDs = serveProtectedIDs(ids, rejection.IDs)
		return serveProtectedReject(reported.step, reported.candidate, ids, rejection)
	}

	// Step 12: an advancement request needs a maintainer of both policies.
	// A direct push without hn-advance skips this step (OQ-4).
	if options.Advance != "" && !(actorListed(ctx.Actor, passed.basePolicy.Maintainers) && actorListed(ctx.Actor, destPolicy.Maintainers)) {
		return serveProtectedReject(serveProtectedStepAdvanceAllowed, passed.candidate, ids, &serveRejection{
			Code: serveReasonAdvanceNotMaintainer,
			IDs:  serveProtectedIDs(ids, nil),
			Sentence: "actor " + ctx.Actor + " requested advancement of " + update.Ref + " and is not a maintainer under both the base policy of " +
				passed.candidate + " and the destination policy at " + tip + ".",
		})
	}
	return serveProtectedVerdict{Candidate: passed.candidate, Candidates: ids}
}

func serveProtectedReject(step int, candidate string, candidates []string, rejection *serveRejection) serveProtectedVerdict {
	return serveProtectedVerdict{Step: step, Candidate: candidate, Candidates: candidates, Rejection: rejection}
}

// serveShortCommit is the eight-digit form a sentence uses for a commit.
func serveShortCommit(oid string) string {
	if len(oid) > 8 {
		return oid[:8]
	}
	return oid
}

// serveProtectedIDs lists every candidate first, then the IDs specific to
// the reported failure, without repeats.
func serveProtectedIDs(candidates, specific []string) []string {
	ids := slices.Clone(candidates)
	for _, id := range specific {
		if id != "" && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids
}

// serveProtectedCandidates returns the proposal events whose signed head is
// head, sorted by event ID. With hn-advance the set is the named event
// alone, and it is empty unless that event is a proposal with that head.
func serveProtectedCandidates(events []StoredEvent, head, advance string) []StoredEvent {
	var candidates []StoredEvent
	seen := make(map[string]bool)
	for _, event := range events {
		if seen[event.ID] || !isProposalKind(event.Event.Kind) || event.Event.Head != head {
			continue
		}
		if advance != "" && event.ID != advance {
			continue
		}
		seen[event.ID] = true
		candidates = append(candidates, event)
	}
	slices.SortFunc(candidates, func(left, right StoredEvent) int { return strings.Compare(left.ID, right.ID) })
	return candidates
}

// serveHostedProposalCode reads every hosted proposal code ref in one call,
// keyed by candidate ID.
func serveHostedProposalCode(gitDir string) (map[string]string, error) {
	listed, err := gitTextAt(gitDir, "for-each-ref", "--format=%(refname) %(objectname)", serveProposalRefPrefix)
	if err != nil {
		return nil, err
	}
	code := make(map[string]string)
	for _, line := range strings.Split(listed, "\n") {
		ref, oid, found := strings.Cut(line, " ")
		if !found {
			continue
		}
		if digest, ok := strings.CutPrefix(ref, serveProposalRefPrefix); ok && serveLowerHex64(digest) {
			code["sha256:"+digest] = oid
		}
	}
	return code, nil
}

// servePushedProposalCode maps each candidate to the commit a proposal ref
// created in this push names. An update or deletion of a hosted proposal
// ref is refused by the proposal rule and never counts as code; the hosted
// value stays authoritative. Any refusal of a proposal ref rejects the
// whole push, so a protected update can only ever land together with code
// that passed the proposal rule.
func servePushedProposalCode(updates []serveRefUpdate) map[string]string {
	code := make(map[string]string)
	for _, update := range updates {
		if update.Class == serveRefProposal && update.Creates() {
			code["sha256:"+strings.TrimPrefix(update.Ref, serveProposalRefPrefix)] = update.New
		}
	}
	return code
}

// serveProtectedCandidateResult is the outcome of steps 7 to 11 for one
// candidate. rejection is nil when the candidate passes; basePolicy is set
// once step 7 passed.
type serveProtectedCandidateResult struct {
	candidate  string
	step       int
	rejection  *serveRejection
	basePolicy PolicyDocument
}

func (result serveProtectedCandidateResult) fail(step int, rejection *serveRejection) serveProtectedCandidateResult {
	result.step, result.rejection = step, rejection
	return result
}

// candidate runs steps 7 to 11 for one candidate.
func (check *serveProtectedUpdate) candidate(candidate *StoredEvent) serveProtectedCandidateResult {
	result := serveProtectedCandidateResult{candidate: candidate.ID}
	statusNext := "run hn proposal status " + candidate.ID

	// Step 7: the signed base policy.
	basePolicy, baseDigest, err := loadReplicationPolicyAt(check.ctx.GitDir, check.ctx.GitDir, candidate.Event.Base)
	if err != nil {
		return result.fail(serveProtectedStepBasePolicy, &serveRejection{
			Code: serveReasonProtectedPolicyInvalid,
			IDs:  []string{candidate.Event.Base},
			Sentence: "candidate " + candidate.ID + " has base " + candidate.Event.Base + ", which has no valid .hn/policy.json (" +
				serveDiagnostic(err, check.ctx.GitDir) + ").",
		})
	}
	result.basePolicy = basePolicy

	// Step 8: the candidate's code is hosted or arrives in this push.
	code := proposalCodeState{}
	if hosted, ok := check.hostedCode[candidate.ID]; ok {
		code = proposalCodeState{Head: hosted, Exists: true}
	} else if pushed, ok := check.pushedCode[candidate.ID]; ok {
		code = proposalCodeState{Head: pushed, Exists: true}
	}
	if !code.Exists || code.Head != check.head {
		sentence := "candidate " + candidate.ID + " has no hosted code ref " + proposalRef(candidate.ID) + " and none arrives in this push; publish it with hn sync."
		if code.Exists {
			sentence = "the code ref " + proposalRef(candidate.ID) + " of candidate " + candidate.ID + " points at " + code.Head + ", not at " + check.head + "."
		}
		return result.fail(serveProtectedStepCode, &serveRejection{
			Code:     serveReasonProposalRefMismatch,
			IDs:      serveCompactIDs(code.Head),
			Missing:  []string{serveEvidenceProposal},
			Sentence: sentence,
		})
	}

	// Steps 9 and 10: the base evaluation. A pipeline of the base policy
	// without a valid definition at H can never pass; the rest of the
	// evaluation is still read so a blocked candidate reports step 9.
	base, baseDefinitionErr, err := check.evaluate(candidate, basePolicy, baseDigest, code)
	if err != nil {
		return result.fail(serveProtectedStepBaseReady, &serveRejection{
			Code:     serveReasonProtectedNotReady,
			Next:     statusNext,
			Sentence: "candidate " + candidate.ID + " cannot be evaluated under its base policy: " + serveDiagnostic(err, check.ctx.GitDir),
		})
	}

	// Step 9: superseded, closed or conflicted lineage, already merged, or
	// a current maintainer rejection under the base policy.
	if reason, blocking := serveProtectedBlocking(base, check.events); reason != "" {
		return result.fail(serveProtectedStepBlocked, &serveRejection{
			Code:     serveReasonProtectedBlocked,
			IDs:      blocking,
			Next:     statusNext,
			Sentence: "candidate " + candidate.ID + " " + reason + ".",
		})
	}

	// Step 10: ready and accepted under the base policy.
	if baseDefinitionErr != nil || !base.Ready || !base.Accepted {
		missing, details := serveProtectedUnmet("base policy", base, baseDefinitionErr, check.head)
		return result.fail(serveProtectedStepBaseReady, &serveRejection{
			Code:     serveReasonProtectedNotReady,
			Missing:  missing,
			Next:     statusNext,
			Sentence: "candidate " + candidate.ID + " is not ready and accepted under its base policy: " + details + ".",
		})
	}

	// Step 11: the same signed facts under the destination policy, with
	// decisions still matched by the base policy digest they were signed
	// against (C-004).
	dest, destDefinitionErr, err := check.evaluate(candidate, check.destPolicy, baseDigest, code)
	if err != nil {
		return result.fail(serveProtectedStepDestReady, &serveRejection{
			Code:     serveReasonProtectedDestinationPolicy,
			Sentence: "candidate " + candidate.ID + " cannot be evaluated under the destination policy at " + check.tip + ": " + serveDiagnostic(err, check.ctx.GitDir),
		})
	}
	if destDefinitionErr != nil || !dest.Ready || !dest.Accepted {
		missing, details := serveProtectedUnmet("destination policy", dest, destDefinitionErr, check.head)
		return result.fail(serveProtectedStepDestReady, &serveRejection{
			Code:     serveReasonProtectedDestinationPolicy,
			Missing:  missing,
			Sentence: check.ref + " at " + serveShortCommit(check.tip) + " needs more evidence for candidate " + candidate.ID + ": " + details + ".",
		})
	}
	return result
}

// evaluate evaluates the candidate under policy with decisions matched by
// digest. A required pipeline without a valid definition at the head comes
// back as definitionErr together with the evaluation of the remaining
// evidence, read without code: reviews, decisions, merges and lineage do
// not depend on it, and no CI result counts.
func (check *serveProtectedUpdate) evaluate(candidate *StoredEvent, policy PolicyDocument, digest string, code proposalCodeState) (*ProposalEvaluation, *pipelineDefinitionError, error) {
	evaluation, err := evaluateProposalWithCode(candidate, check.events, policy, digest, code)
	if err == nil {
		return evaluation, nil, nil
	}
	var definitionErr pipelineDefinitionError
	if !errors.As(err, &definitionErr) {
		return nil, nil, err
	}
	evaluation, err = evaluateProposalWithCode(candidate, check.events, policy, digest, proposalCodeState{})
	if err != nil {
		return nil, nil, err
	}
	return evaluation, &definitionErr, nil
}

// serveProtectedBlocking returns why the evaluated candidate can never
// advance a branch and the events that block it, or "" when nothing does.
func serveProtectedBlocking(evaluation *ProposalEvaluation, events []StoredEvent) (string, []string) {
	lineage := evaluation.Lineage
	merges := func() []string {
		var ids []string
		for _, event := range events {
			if event.Event.Kind == "proposal.merged" && slices.Contains(lineage.MergedCandidateIDs, event.Event.Subject) {
				ids = append(ids, event.ID)
			}
		}
		slices.Sort(ids)
		return ids
	}
	switch {
	case lineage.MergeConflict:
		return "belongs to a lineage with competing merges of " + strings.Join(lineage.MergedCandidateIDs, ", "), merges()
	case evaluation.Merged || lineage.Merged:
		ids := merges()
		return "is already merged by " + strings.Join(ids, ", "), ids
	case lineage.LineageClosed:
		return "belongs to a lineage closed by merged candidate " + strings.Join(lineage.MergedCandidateIDs, ", "), merges()
	case lineage.Superseded:
		return "is superseded by " + strings.Join(lineage.SuccessorIDs, ", "), slices.Clone(lineage.SuccessorIDs)
	case evaluation.Rejected:
		ids := sortedDecisionIDs(evaluation.RejectDecisions)
		return "has a current maintainer rejection " + strings.Join(ids, ", "), ids
	}
	return "", nil
}

// serveProtectedUnmet lists the evidence kinds an evaluation lacks, in
// emission order, and one clause per unmet threshold, for example
// "destination policy requires 2 approvals; 1 trusted approval found".
func serveProtectedUnmet(label string, evaluation *ProposalEvaluation, definitionErr *pipelineDefinitionError, head string) ([]string, string) {
	var missing, clauses []string
	policy := evaluation.Policy
	if len(evaluation.Approvals) < policy.Proposals.RequiredApprovals {
		missing = append(missing, serveEvidenceReview)
		clauses = append(clauses, fmt.Sprintf("%s requires %s; %s found", label,
			servePlural(policy.Proposals.RequiredApprovals, "approval"), servePlural(len(evaluation.Approvals), "trusted approval")))
	}
	if definitionErr != nil {
		missing = append(missing, serveEvidenceCI)
		clauses = append(clauses, fmt.Sprintf("%s requires pipeline %s, which has no valid definition at %s", label, definitionErr.Pipeline, head))
	} else {
		for _, pipeline := range evaluation.Pipelines {
			if len(pipeline.Passed) >= pipeline.Required {
				continue
			}
			if !slices.Contains(missing, serveEvidenceCI) {
				missing = append(missing, serveEvidenceCI)
			}
			clauses = append(clauses, fmt.Sprintf("%s requires %s of pipeline %s; %s found", label,
				servePlural(pipeline.Required, "passing result"), pipeline.Name, servePlural(len(pipeline.Passed), "trusted passing result")))
		}
	}
	if !evaluation.Accepted {
		missing = append(missing, serveEvidenceAcceptance)
		clause := fmt.Sprintf("%s requires %s; %s found", label,
			servePlural(policy.Proposals.RequiredAccepts, "acceptance"), servePlural(len(evaluation.AcceptDecisions), "current acceptance"))
		if evaluation.Rejected {
			clause += " and a current maintainer rejection"
		}
		clauses = append(clauses, clause)
	}
	if len(clauses) == 0 {
		// Code that does not match keeps an evaluation from being ready;
		// step 8 rules that out, so this only keeps the rule fail-closed.
		missing = append(missing, serveEvidenceProposal)
		clauses = append(clauses, label+" requires the signed head as code")
	}
	return missing, strings.Join(clauses, "; ")
}

// servePlural renders a count with its noun, adding "s" unless it is 1.
func servePlural(count int, noun string) string {
	if count == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", count, noun)
}
