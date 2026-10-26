---
work_package_id: WP04
title: Validation seams for destination policy and server-side actor checks
dependencies: []
requirement_refs:
- C-004
- C-006
- FR-007
- FR-008
- FR-015
planning_base_branch: mission/hn-serve
merge_target_branch: mission/hn-serve
branch_strategy: Planning artifacts for this mission were generated on mission/hn-serve. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into mission/hn-serve unless the human explicitly redirects the landing branch.
subtasks:
- T020
- T021
- T022
- T023
- T024
- T025
- T026
phase: Phase 1 - Foundations
history:
- !!omap
  - at: '2026-10-25T00:09:18Z'
  - actor: system
  - action: Prompt generated via /spec-kitty.tasks
agent_profile: implementer-ivan
authoritative_surface: policy.go
create_intent:
- validation_seams_test.go
execution_mode: code_change
owned_files:
- policy.go
- quarantine.go
- validation_seams_test.go
- docs/governance-v0.md
role: implementer
tags: []
tracker_refs: []
---

# WP04: Validation seams for destination policy and server-side actor checks

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter, and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `codex`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## Objective

Make three behavior-preserving changes to existing validation code so the server can reuse it instead of duplicating trust logic (research decisions `01M5R5CY0Z5RZ3V0J8Q2W5C49K` and `01M5R5D0KF0C3VSZVSMJT1F6ZR`):

1. Extract `evaluateProposalUnderPolicy(proposal, events, policy, decisionDigest)` from `evaluateProposal` in `policy.go`, so WP09 can evaluate the same signed facts under the destination policy (C-004).
2. Add a range variant of `validateQuarantinedActor` in `quarantine.go` that validates only the appended commits of a pushed actor chain and joins them to the already-hosted prefix (FR-007, NFR-001).
3. Add an option to `replicationEventDependency` that accepts a `proposal.open` or `proposal.revise` event whose code ref is neither hosted nor in the same push, while still checking its base and, when the head is present, its ancestry.

Then prove, with tests, that a `proposal.merged` fact whose `commit` equals its `head` already validates on every path (FR-015, spec assumption), and document destination-policy evaluation and fast-forward merge facts in `docs/governance-v0.md`.

Every existing caller must behave exactly as before. Existing tests must pass without assertion changes (NFR-007).

## Context

- Read the plan sections "Reuse of existing validation" and "Domain rules and invariants", `research.md` ("Validation reuse boundary", "Destination policy evaluation", "Verified assumptions from the spec") and `contracts/push-verdict.md` (actor chain steps 4 and 5, protected steps 10 and 11).
- Start with `spec-kitty agent action implement WP04 --agent <name> --mission hn-serve-01M5P0NP`.
- Key existing functions:
  - `policy.go`: `evaluateProposal` (line ~247), `loadPolicy`, `validateDecisionEvent`, `validateMergeEvent`.
  - `quarantine.go`: `validateQuarantinedActor` (~848), `validateQuarantinedEventTree`, `replicationEventDependency` (~947), `validateReplicationMergeAt`.
  - `store.go`: `loadStoredEventAt`, `loadActorEventsAt`, `validateActorChains`, `validateProposalMergeBinding`.
  - `governance.go`: `appendProposalMergeEvent`.
  - Test helpers in `governance_revision_test.go` (`appendRevisionReview`, `appendRevisionRunResult`, `appendRevisionDecision`, `appendRevisionMerge`) and `operational_acceptance_test.go`.
- WP11 later edits `quarantine.go` in the same lane, after this WP. Keep your changes localized and well named so WP11's diff stays independent.

## Branch Strategy

- **Planning/base branch at prompt creation**: `mission/hn-serve`
- **Final merge target for completed work**: `mission/hn-serve`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. Each computed lane gets exactly one worktree, and WP11 reuses this lane's worktree after this WP.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `policy.go` | CHANGE. Extraction only, plus a typed wrapper for the pipeline-definition error. |
| `quarantine.go` | CHANGE. New range validator and dependency option; existing functions delegate. |
| `validation_seams_test.go` | NEW. |
| `docs/governance-v0.md` | CHANGE. Two new sections. |

## Subtasks

### Subtask T020: Extract `evaluateProposalUnderPolicy` from `evaluateProposal`

**Purpose**: the server needs readiness and acceptance computed under a policy other than the base policy.

**Steps**:

1. Split `evaluateProposal` into:

   ```go
   func evaluateProposal(proposal *StoredEvent, events []StoredEvent) (*ProposalEvaluation, error) {
       if !isProposalKind(proposal.Event.Kind) { ... unchanged error ... }
       policy, _, digest, err := loadPolicy(proposal.Event.Base)
       // keep the original order of failures: the lineage checks currently run before loadPolicy;
       // preserve that order so error precedence is unchanged.
       return evaluateProposalUnderPolicy(proposal, events, policy, digest)
   }
   ```

   Keep the existing order of operations exactly: kind check, `buildLineageIndex`, `lineage.state`, `lineage.candidate(root)`, then `loadPolicy`. A simple way is to move the lineage steps into a small helper used by both functions, or to have `evaluateProposalUnderPolicy` repeat them and `evaluateProposal` call `loadPolicy` after them. Whatever shape you choose, the first error returned for any input must be the same as today.

2. `evaluateProposalUnderPolicy(proposal *StoredEvent, events []StoredEvent, policy PolicyDocument, decisionDigest string) (*ProposalEvaluation, error)` contains everything after `loadPolicy`: code availability, reviews, pipelines, decisions, merges, readiness, acceptance, evidence. Every place that used `digest` uses `decisionDigest`. Set `evaluation.Policy = policy` and `evaluation.PolicyDigest = decisionDigest`. Add a doc comment explaining why the digest is a separate parameter: maintainers sign decisions against the base policy digest, so destination evaluation passes the base digest and the destination policy's lists and thresholds.

3. Code-state seam: `evaluateProposal` reads the candidate's code ref through `proposalHead(proposal.ID)`, which only sees refs that already exist. Inside the server hook, a protected update may rely on a proposal ref that arrives in the same push and is not yet a ref (`contracts/push-verdict.md`, "Inputs"). Provide one more internal variant and make `evaluateProposalUnderPolicy` a thin wrapper around it:

   ```go
   type proposalCodeState struct {
       Head   string
       Exists bool
   }

   // evaluateProposalWithCode is evaluateProposalUnderPolicy with the candidate's
   // code ref supplied by the caller instead of read from refs/hn/proposals.
   func evaluateProposalWithCode(proposal *StoredEvent, events []StoredEvent, policy PolicyDocument, decisionDigest string, code proposalCodeState) (*ProposalEvaluation, error)
   ```

   `evaluateProposalUnderPolicy` calls `proposalHead`, builds the `proposalCodeState`, and delegates. Error precedence stays as today: a `proposalHead` error is returned at the same point in the sequence it is returned now.

4. Do not change `ProposalEvaluation`, `proposalStatus`, `cmdProposalStatus`, `validateDecisionEvent` or `validateMergeEvent`.

**Files**: `policy.go`.

### Subtask T021: Destination evaluation semantics

**Purpose**: C-004 and the spec edge case "evidence signed by actors removed from the destination policy does not count".

**Steps**:

1. Confirm by test (T025) that with `evaluateProposalUnderPolicy(p, events, destPolicy, baseDigest)`:
   - Reviews count only from `destPolicy.Proposals.TrustedReviewers`, honouring `destPolicy.Proposals.AllowAuthorApproval`.
   - Run requests count only from `destPolicy.Maintainers`; results only from each destination pipeline's `TrustedRunners`; thresholds are the destination's `RequiredResults`.
   - Decisions count only when signed against `baseDigest` by actors in `destPolicy.Maintainers`; the threshold is `destPolicy.Proposals.RequiredAccepts`.
   - A pipeline present only in `destPolicy` is loaded at the proposal head through `loadPipeline`; if its definition is missing or invalid, the function returns an error.

2. Make that error distinguishable without changing its text: wrap the existing `fmt.Errorf("required pipeline %q: %w", name, err)` in a small type, for example:

   ```go
   type pipelineDefinitionError struct{ Pipeline string; err error }
   func (e pipelineDefinitionError) Error() string { return e.err.Error() }
   func (e pipelineDefinitionError) Unwrap() error { return e.err }
   ```

   so `errors.As` lets WP09 map it to `protected-destination-policy` with `missing=ci`. The message seen by existing callers stays byte-identical.

**Files**: `policy.go`.

### Subtask T022: Range variant of quarantined actor validation

**Purpose**: FR-007 requires the same signature, sequence, parent and tree validation as replication import, and NFR-001 forbids re-reading all hosted history per push.

**Steps**:

1. Add to `quarantine.go`:

   ```go
   // validateQuarantinedActorRange validates the commits in oldHead..newHead of
   // one actor chain and joins them to the hosted prefix. hostedPrefix holds the
   // actor's already-hosted events in sequence order, ending at oldHead.
   func validateQuarantinedActorRange(gitDir, actor, oldHead, newHead string, hostedPrefix []StoredEvent) ([]StoredEvent, error)
   ```

2. Behavior:
   - `oldHead == ""`: delegate to `validateQuarantinedActor(gitDir, actor, newHead)` and return its result.
   - Otherwise require `hostedPrefix` non-empty with its last `Commit` equal to `oldHead` (error otherwise; the caller passed inconsistent input).
   - List appended commits with `rev-list --reverse oldHead..newHead` through `gitTextAt(gitDir, ...)`. An empty list means no new events; return the prefix unchanged.
   - Load each with `loadStoredEventAt(gitDir, commit)`; require `Event.Actor == actor`; run `validateQuarantinedEventTree`.
   - Parent check: the first appended commit's parents (`show -s --format=%P`) must equal `oldHead`; each later one's must equal the previous appended commit. This rejects merges and side branches.
   - Run `validateActorChains(append(prefixCopy, appended...))` so sequence and previous-ID rules apply across the join.
   - Return the full joined chain (prefix plus appended).
3. Leave `validateQuarantinedActor` unchanged. Do not change `loadActorEventsAt`.
4. Ancestry (`oldHead` is an ancestor of `newHead`) is checked by the caller in WP08 step 1 and reported as `actor-chain-rewrite`; this function may assume it but must still fail safely (the parent check catches non-linear input).

**Files**: `quarantine.go`.

### Subtask T023: Dependency check option for an absent proposal code ref

**Purpose**: `hn sync` pushes the actor ref before the proposal ref in separate `git push` calls (`publishLocalFacts` in `commands.go`). The server must accept the first push. The protected check in WP09 still requires matching hosted code, so this exception cannot advance a branch.

**Steps**:

1. Add:

   ```go
   type replicationDependencyOptions struct {
       // AllowAbsentProposalCode accepts a proposal.open or proposal.revise event
       // whose code ref is absent from codeHeads. The base must exist, and the head
       // must descend from it whenever the head commit is present.
       AllowAbsentProposalCode bool
   }

   func replicationEventDependencyWithOptions(gitDir, acceptedGitDir string, event StoredEvent, byID map[string]StoredEvent,
       codeHeads, eventOwners, codeOwners map[string]string, options replicationDependencyOptions) (*replicationDependency, error)
   ```

   and make `replicationEventDependency` call it with the zero value. Existing call sites stay as they are.

2. Inside the proposal-kind branch, when `codeHeads[event.ID]` is absent and the option is set:
   - Check the base commit exists in `gitDir` or `acceptedGitDir` (`cat-file -e BASE^{commit}`). If absent, return a `replicationDependency` with `Missing` set to the base commit ID and `Reason: "proposal base commit is absent"`.
   - If the head commit exists in either directory, run the same `merge-base --is-ancestor` check the existing code uses; on failure return the existing "does not descend" error.
   - If the head commit is absent, skip the ancestry check.
   - Then continue into the `switch` so `proposal.revise` still checks its predecessor and author.
3. When the option is unset, behavior is byte-identical to today.

**Files**: `quarantine.go`.

### Subtask T024: Prove a fast-forward `proposal.merged` fact validates unchanged

**Purpose**: research verified by reading code that `validateProposalMergeBinding`, `validateMergeEvent` and `validateReplicationMergeAt` never inspect `commit` ancestry. FR-015 depends on that, so pin it with tests.

**Steps** (tests in `validation_seams_test.go`):

1. Build a repository with a policy requiring one approval, one pipeline result and one acceptance. Use the helpers in `governance_revision_test.go` to add review, run request and result, and decision, then call `appendProposalMergeEvent(identity, proposal, evaluation, proposal.Event.Head)` so `commit == head`.
2. Assert: `collectEvents()` succeeds (relationship validation includes the merge binding and `validateMergeEvent`); `evaluateProposal` reports `Merged == true`; `proposalStatus` returns `merged`; the lineage is closed (`buildLineageIndex` then `state`).
3. Replicate the actor chain into a second clone through an ordinary bare remote with an exact selection (follow the pattern in `replication_test.go`), and assert the merge event is promoted, which exercises `validateReplicationMergeAt`.
4. If any assertion fails, stop and record the finding in the Activity Log: the spec's assumption would be false and C-008 requires a documented validation change. Do not silently patch validation.

**Files**: `validation_seams_test.go`.

### Subtask T025: Seam unit tests and unchanged existing tests

**Purpose**: prove the seams behave as specified and that nothing else changed.

**Steps** (tests named `TestValidationSeams...`):

1. Equivalence: for a set of proposals in varied states (blocked, ready, accepted, rejected, merged, superseded), `evaluateProposal`, `evaluateProposalUnderPolicy` with the base policy and base digest, and `evaluateProposalWithCode` given the real code ref return deeply equal evaluations. `evaluateProposalWithCode` given `Exists: true` with the signed head for a candidate whose ref is absent reports `CodeMatches == true`.
2. Destination semantics (T021): a destination policy that removes the only trusted reviewer makes approvals zero; one that raises `RequiredApprovals` to 2 makes `Ready` false; one that adds a pipeline with no definition at the head returns a `pipelineDefinitionError` whose message equals the old text; decisions signed against a different digest never count.
3. Range validator (T022): appending two valid events validates; an appended event signed by another actor, a broken signature, a wrong sequence, a wrong previous ID, an extra tree entry, and a merge commit in the range are each rejected; `oldHead == ""` matches `validateQuarantinedActor`.
4. Dependency option (T023): a proposal event with no code ref and an existing base passes with the option and fails as today without it; a missing base returns a dependency naming the base commit; a present head that does not descend from the base is rejected.
5. Run the full suite and confirm no existing test changed: `git diff --stat mission/hn-serve -- '*_test.go'` must list only `validation_seams_test.go`.

**Files**: `validation_seams_test.go`.

### Subtask T026: `docs/governance-v0.md`: destination policy and fast-forward merge facts

**Purpose**: HN-005 and C-008: governance semantics change with documentation in the same WP.

**Steps**: add two sections after "Readiness and exact evidence":

1. **Destination policy**: when a protected branch on an `hn serve` remote moves, the update must satisfy both the proposal's signed base policy and the policy at the branch tip being replaced. Explain that the destination check counts the same signed reviews, CI results and decisions (decisions still signed against the base policy digest), applies the destination's maintainers, trusted reviewers, trusted runners, pipelines and thresholds, and that evidence from actors absent from the destination policy does not count. Mention that a pipeline required only by the destination policy is loaded at the proposal head. Link to `docs/serve-v0.md#protected-branches` (WP09 writes that section).
2. **Fast-forward merge facts**: a `proposal.merged` fact with `commit` equal to `head` records that a protected branch was fast-forwarded to the exact tested head; a fact with a different `commit` keeps its existing meaning (a `--no-ff` merge commit whose second parent is the head). Validation is identical for both shapes. `hn merge PROPOSAL` without `--remote` keeps producing the second shape (FR-016).

Do not describe the `hn merge --remote` command flow here; WP10 documents it in `docs/serve-v0.md`. Writing style: plain sentences, descriptive headings, no em dashes.

**Files**: `docs/governance-v0.md`.

## Test Strategy

Unit and in-repository tests in `validation_seams_test.go` using the existing signed-event helpers; one replication round trip through a temporary bare remote. The existing suite is the regression check.

## Definition of Done

- [ ] T020 to T026 complete; full gate passes.
- [ ] **C-004 / FR-008 (seam)**: `evaluateProposalUnderPolicy` exists, base evaluation is unchanged (equivalence test), destination semantics are tested.
- [ ] **FR-007 (seam)**: range validator rejects every listed defect and validates appends without re-reading the hosted prefix.
- [ ] **FR-015 (assumption)**: fast-forward merge facts validate locally and through replication with no validation change.
- [ ] **C-006**: no server identity, signing or verdict appears in these paths.
- [ ] **NFR-007**: no pre-existing test file changed.
- [ ] `docs/governance-v0.md` has both sections.

## Risks

- Error precedence in `evaluateProposal` is observable through `hn proposal status` and `hn merge`; the equivalence tests and the existing suite guard it.
- WP11 edits `quarantine.go` next; avoid reformatting unrelated code.

## Reviewer Guidance

- Diff `policy.go` and confirm the extraction moved code without changing logic.
- Confirm `replicationEventDependency` still behaves identically with the zero option.
- Re-run `go test -race ./...` and confirm pre-existing tests are untouched.

## Activity Log

- 2026-10-25T00:09:18Z – system – Prompt generated via /spec-kitty.tasks
