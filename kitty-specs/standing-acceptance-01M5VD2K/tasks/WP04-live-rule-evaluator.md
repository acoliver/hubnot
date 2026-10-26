---
work_package_id: WP04
title: Live rule evaluator
dependencies:
- WP01
requirement_refs:
- C-009
- FR-005
- FR-006
- FR-009
- FR-015
- NFR-002
planning_base_branch: feat/standing-acceptance
merge_target_branch: feat/standing-acceptance
branch_strategy: Planning artifacts for this mission were generated on feat/standing-acceptance. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into feat/standing-acceptance unless the human explicitly redirects the landing branch.
subtasks:
- T015
- T016
- T017
- T018
- T019
phase: Phase 2 - Shared derivation and test harness
history:
- !!omap
  - timestamp: '2026-10-26T06:54:14Z'
  - agent: system
  - action: Prompt generated via /spec-kitty.tasks
agent_profile: implementer-ivan
authoritative_surface: rule_acceptance.go
create_intent:
- rule_acceptance.go
- rule_acceptance_test.go
execution_mode: code_change
owned_files:
- rule_acceptance.go
- rule_acceptance_test.go
role: implementer
tags: []
tracker_refs: []
---

# WP04: Live rule evaluator

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter, and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `codex`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## Objective

Create `rule_acceptance.go` with the shared rule derivation and its first entry point, `evaluateRuleAcceptance`, a pure function (no Git, no I/O) that judges **current** evidence for a named rule. It is called later by the keyless preview and local execution (WP10, WP11), and by the `hn serve` protected verdict (WP14). WP05 adds the second entry point, `validateRuleMergeFact`, in the same file and must reuse this WP's helpers so live evaluation and fact validation cannot drift (NFR-002).

The function reports:

- the matched rule, its acceptance mode and the reviewers it counts;
- counting approvals (FR-005, FR-006) and, per base-policy pipeline, qualifying passing and failing results (C-009: pipeline `requesters` from commit `7a47e69` are honored through `mayRequestRun`);
- vetoes: current trusted change requests from any trusted reviewer, current trusted failures, current maintainer rejections;
- unmet conditions with a kind and the contract sentence (tasks decision 4);
- the exact evidence list a rule merge fact would carry (decision `01M5VEZXQ7GD8JJ4XNDZD6B2XD`).

## Context

- Read [data-model.md](../data-model.md) "Qualifying evidence", "Rule inputs" and "Rule acceptance evaluation"; [contracts/automerge-cli.md](../contracts/automerge-cli.md) "Blockers by acceptance mode"; [contracts/serve-rule-advancement.md](../contracts/serve-rule-advancement.md) "Step 10 under a rule"; [research.md](../research.md) findings 9 and 10 and decisions `01M5VEZEC0EGQWXKCPTJRECHAV` and `01M5VEZXQ7GD8JJ4XNDZD6B2XD`.
- Start with `spec-kitty agent action implement WP04 --agent <name> --mission standing-acceptance-01M5VD2K`.
- Existing code to reuse, not copy: `currentReviews` (`proposal.go`), `currentRunResults` (`ci.go`), `mayRequestRun`, `actorListed`, `sortStoredEvents`, `sortedUniqueStrings` (`policy.go` and helpers), `automergePathAllowed` (`automerge.go`), `protectedPathMatches`, `changedProtectedPaths` (`protected_checks.go`). Read `evaluateProposalEvidence` in `policy.go` lines 364 to 487: the pipeline loop there is the semantics to match for "latest result per trusted runner".
- From WP01: `AutomergeRule.Acceptance.Standing()`, `AutomergeRule.Reviewers` (`IsZero`, `Actors`).
- Do not reuse `ProposalEvaluation.Approvals`: it includes the author when `allowAuthorApproval` is true (`policy.go` line 373).

## Branch Strategy

- **Planning/base branch at prompt creation**: `feat/standing-acceptance`
- **Final merge target for completed work**: `feat/standing-acceptance`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. Each computed lane gets exactly one worktree, and sequential WPs in the same lane reuse it.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `rule_acceptance.go` | NEW. Types, helpers, `evaluateRuleAcceptance`. WP05 adds the fact validator. |
| `rule_acceptance_test.go` | NEW. `TestRuleAcceptanceLive...` unit tests. WP05 adds `TestRuleAcceptanceFact...`. |

## Subtasks

### Subtask T015: `rule_acceptance.go` inputs, result types and rule lookup

**Steps**:

1. Inputs, built by each caller from its own object access:

   ```go
   // ruleInputs is everything the rule derivation reads. Callers fill it
   // from their own object store; the derivation itself runs no Git.
   type ruleInputs struct {
       Policy              PolicyDocument    // base policy at the candidate's signed base
       PolicyDigest        string            // its digest
       Proposal            StoredEvent       // the candidate
       RuleName            string
       TargetBranch        string            // branch being advanced; "" skips the check
       Changed             []string          // sorted changed paths base..head
       PipelineDefinitions map[string]string // pipeline name -> definition digest at head; absent means unavailable
       Events              []StoredEvent     // current events (live) or the fact index values (validation)
   }
   ```

2. Result types:

   ```go
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
   type ruleUnmet struct{ Kind, Text string }
   type ruleVeto struct {
       Kind  string // "change-request", "failure" or "rejection"
       Event StoredEvent
   }
   type rulePipeline struct {
       Name     string
       Required int
       Passed   []StoredEvent // latest passing result per trusted runner, sorted
       Failed   []StoredEvent // latest failing result per trusted runner, sorted
   }
   type ruleAcceptance struct {
       Rule              *AutomergeRule
       Standing          bool
       CountedReviewers  []string // rule reviewers or all trusted reviewers, sorted
       HasRuleReviewers  bool     // true when the rule's reviewers field is set
       Approvals         []StoredEvent
       RequiredApprovals int
       Pipelines         []rulePipeline // in sorted pipeline-name order
       Vetoes            []ruleVeto     // sorted by event ID
       Unmet             []ruleUnmet    // in the order of T018
       Evidence          []string       // sorted, unique
   }
   ```

   Add `func (r *ruleAcceptance) Satisfied() bool` (rule found, standing, no unmet, no vetoes) and `func (r *ruleAcceptance) UnmetOf(kinds ...string) []ruleUnmet`.
3. `func ruleByName(policy PolicyDocument, name string) *AutomergeRule` returns a copy or nil.
4. `func ruleCountedReviewers(policy PolicyDocument, rule *AutomergeRule) []string`: sorted rule reviewers when set, otherwise sorted `proposals.trustedReviewers`.

### Subtask T016: Counting approvals and qualifying run results

These helpers are shared with WP05. Each takes the policy, rule and proposal explicitly.

1. `func ruleApprovalCounts(policy PolicyDocument, rule *AutomergeRule, proposal StoredEvent, review StoredEvent) bool`. True when `review.Event.Kind == "review.submit"`, `Subject == proposal.ID`, `Verdict == "approve"`, the actor is in `proposals.trustedReviewers`, the actor is in the rule's `reviewers` when set, and:
   - acceptance mode `rule`: the actor is not the candidate author, whatever `allowAuthorApproval` says (FR-006);
   - acceptance mode `maintainers`: `allowAuthorApproval || actor != author`, which matches `evaluateProposalEvidence`.
2. `func ruleRequestQualifies(policy PolicyDocument, proposal StoredEvent, definitions map[string]string, request StoredEvent) bool`. True when `Kind == "run.request"`, `Subject == proposal.ID`, `Commit == proposal.Event.Head`, the pipeline exists in the base policy, `definitions[pipeline]` exists and equals `request.Event.Definition`, and `mayRequestRun(policy, pipeline, request.Event.Actor)` holds.
3. `func ruleResultQualifies(policy PolicyDocument, proposal StoredEvent, definitions map[string]string, result, request StoredEvent) bool`. True when `result.Event.Kind == "run.result"`, `result.Event.Subject == request.ID`, the request qualifies, the result's `Pipeline`, `Commit` and `Definition` equal the request's, and the result's actor is a trusted runner of that pipeline. Outcome is checked by the caller (live evaluation needs both outcomes; validation needs `passed`).
4. `func ruleLivePipelines(inputs ruleInputs) []rulePipeline`: for each base-policy pipeline in sorted order, collect qualifying requests, call `currentRunResults` per request, keep the latest qualifying result per trusted runner by `Sequence` (as `evaluateProposalEvidence` does), and split into `Passed` and `Failed`.

### Subtask T017: Author, path and manual-path checks

1. `func ruleAuthorAllowed(rule *AutomergeRule, proposal StoredEvent) bool`: `actorListed(proposal.Event.Actor, rule.Authors)`.
2. `func ruleManualPathChanges(policy PolicyDocument, changed []string) (policyAmendment, protectedChange bool)`: `protectedPathMatches(".hn/policy.json", p)` for any changed path, and `len(changedProtectedPaths(policy, changed)) > 0`. Same functions `inspectAutomerge` uses today, so the verdicts are identical.
3. `func ruleDisallowedPaths(rule *AutomergeRule, changed []string) []string`: changed paths not allowed by `automergePathAllowed(rule.Paths, p)`, in changed order.

### Subtask T018: `evaluateRuleAcceptance`: vetoes, typed unmet conditions and evidence

**Signature**: `func evaluateRuleAcceptance(inputs ruleInputs) *ruleAcceptance`. It never returns an error; every problem is an unmet condition or a veto.

**Order of unmet conditions** (the text is the contract or the existing `inspectAutomerge` text):

1. `rule-missing`: `automerge is disabled in the signed base policy` when `Policy.Automerge == nil`; otherwise `automerge rule %q does not exist in the signed base policy`. When the rule is missing, return after this entry: nothing else can be evaluated.
2. `rule-mode`: `automerge rule %q has acceptance "maintainers" and needs maintainer acceptance` when the rule is not standing. Still evaluate the rest, so the maintainers-mode preview can read the approval count (US5-4).
3. `target-branch`: when `TargetBranch != ""` and differs from `rule.TargetBranch`: `rule target %q does not match %s`.
4. `author`: `proposal author is not allowed by the rule`.
5. `manual-path`: `policy amendments require manual merge`, then `protected check changes require manual merge`.
6. `path`: `proposal has no changed paths` when `Changed` is empty; then one `changed path %q is outside the rule allowlist` per disallowed path.
7. `approvals`: when distinct counting reviewers < `proposals.requiredApprovals`: `rule approval threshold is unmet: N of M counted approvals`.
8. `pipeline`: per pipeline in sorted order with `len(Passed) < Required`: `pipeline %q has N of M passing trusted results`.

**Approvals**: walk `currentReviews(inputs.Events, proposal.ID)`, keep those for which `ruleApprovalCounts` holds, sorted by event ID.

**Vetoes** (data-model "Rule acceptance evaluation"):
- every current review with verdict `request-changes` whose actor is in `proposals.trustedReviewers`, counted by the rule or not, including the author's own (this matches `inspectAutomerge` today and `TestAutomergeAuthorChangeRequestDoesNotDisappearWithAuthorApprovalDisabled`);
- every latest failing result in `Pipelines`;
- every current maintainer `proposal.decision` with verdict `reject` and `Policy == PolicyDigest` (latest per maintainer by sequence, as in `evaluateProposalEvidence`).

**Evidence**: the IDs of `Approvals` plus, for each pipeline, the IDs of `Passed`, through `sortedUniqueStrings`. This is every current counting item, so the signer has no choice to make.

**Determinism**: the result depends only on the inputs, never on map iteration order. Sort every slice you build from a map.

### Subtask T019: Live evaluator unit tests

**Steps** (tests `TestRuleAcceptanceLive...` in `rule_acceptance_test.go`). Build `StoredEvent` values in memory with distinct IDs, actors and sequences; no Git. Write a small builder `ruleAcceptanceTestWorld` with methods `review(actor, verdict)`, `request(actor, pipeline)`, `result(runner, request, outcome)`, `decision(maintainer, verdict)` that assign increasing sequences per actor.

Cases (each asserts approvals, vetoes, unmet kinds and texts, and evidence):

1. **Happy path (US1)**: standing rule, one trusted reviewer approves, one passing `test` result from a maintainer-requested run. `Satisfied()`; evidence is the approval and result IDs, sorted.
2. **US5-1**: trusted reviewers `[gpt, sonnet]`, rule reviewers `[gpt]`, `requiredApprovals` 1. Only sonnet approves: `approvals` unmet with `0 of 1`. gpt approves: satisfied; evidence excludes sonnet's approval.
3. **US5-2**: sonnet has a current change request: veto of kind `change-request` naming it, although sonnet is not counted.
4. **US5-3 / FR-006**: `allowAuthorApproval` true, author is a rule reviewer and approves: the approval does not count under acceptance mode `rule`; under acceptance mode `maintainers` it does.
5. **US5-4**: acceptance mode `maintainers` with `reviewers` set: `rule-mode` unmet plus approval counting restricted to the rule reviewers.
6. **US6-7 / C-009**: a run requested by an actor that is neither a maintainer nor a listed requester does not qualify (`pipeline` unmet); the same run requested by a listed `requesters` actor qualifies.
7. **Latest result wins**: a runner reports passed then failed for the same request: failure veto and `pipeline` unmet; failed then passed: satisfied.
8. **Wrong definition or commit**: a request whose `Definition` differs from `PipelineDefinitions[test]`, or whose pipeline is absent from `PipelineDefinitions`, does not qualify.
9. **Maintainer rejection**: veto of kind `rejection`; an older reject followed by an accept by the same maintainer is no veto.
10. **Rule missing, automerge disabled, target branch mismatch (A8), author outside, path outside, no changed paths, policy amendment and protected change**: one case each with the expected kind and text.
11. **Determinism**: shuffle `Events` with a fixed seed ten times and assert identical results with `reflect.DeepEqual`.

## Test Strategy

Pure unit tests, table-driven. Run `go test -race -run 'TestRuleAcceptanceLive' ./...`, then the full gate (no existing behavior changes in this WP).

## Definition of Done

- [ ] T015 to T019 complete; full gate passes.
- [ ] `rule_acceptance.go` imports nothing that runs Git or touches the filesystem.
- [ ] **FR-005, FR-006**: counting follows the rule reviewers in both modes and excludes the author under acceptance mode `rule`.
- [ ] **FR-009 (eligibility)**: every unmet kind and veto of the contract tables is produced with its text.
- [ ] **C-009**: requester authorization uses `mayRequestRun`.
- [ ] **NFR-002 (preparation)**: helpers are exported within the package for WP05 and documented with one comment each.

## Risks

- A subtle mismatch with `evaluateProposalEvidence` in "latest per runner" would make the server and client disagree with existing readiness. Mirror that loop exactly.
- Do not add Git calls "for convenience"; callers own object access (decision `01M5VEZEC0EGQWXKCPTJRECHAV`).

## Reviewer Guidance

- Compare `ruleLivePipelines` line by line with the pipeline loop in `evaluateProposalEvidence`.
- Check that change requests from uncounted trusted reviewers are vetoes.

## Activity Log

- 2026-10-26T06:54:14Z – system – Prompt generated via /spec-kitty.tasks
