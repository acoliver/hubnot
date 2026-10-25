---
work_package_id: WP08
title: Actor chain, proposal and memory ref rules
dependencies:
- WP07
- WP04
requirement_refs:
- C-007
- FR-007
- FR-010
- FR-012
- NFR-002
- NFR-003
planning_base_branch: mission/hn-serve
merge_target_branch: mission/hn-serve
branch_strategy: Planning artifacts for this mission were generated on mission/hn-serve. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into mission/hn-serve unless the human explicitly redirects the landing branch.
subtasks:
- T048
- T049
- T050
- T051
- T052
- T053
phase: Phase 3 - Push verdict
history:
- !!omap
  - at: '2026-10-25T00:09:18Z'
  - actor: system
  - action: Prompt generated via /spec-kitty.tasks
agent_profile: implementer-ivan
authoritative_surface: serve_verdict_facts.go
create_intent:
- serve_verdict_facts.go
- serve_verdict.go
- serve_facts_test.go
- docs/serve-v0.md
execution_mode: code_change
owned_files:
- serve_verdict_facts.go
- serve_verdict.go
- serve_facts_test.go
- docs/serve-v0.md
role: implementer
tags: []
tracker_refs: []
---

# WP08: Actor chain, proposal and memory ref rules

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter, and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `codex`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## Objective

Replace WP07's fail-closed placeholders for Hubnot fact refs with the real rules from `contracts/push-verdict.md`, reusing the replication validators so the server checks exactly what clients check (FR-007, research decision `01M5R5CY0Z5RZ3V0J8Q2W5C49K`):

- `refs/hn/actors/<A>`: only from a key mapped to `A`, only as an append, and only when the appended events pass signature, sequence, parent, tree, relationship and dependency validation (FR-007, C-007).
- `refs/hn/proposals/<id>`: created once, equal to its candidate's signed head, descending from its base, never moved or deleted (FR-010).
- `refs/hn/memory/<A>/<stream>`: only from `A`, append-only, valid under `hn-memory/0` (FR-010).
- Every one of those refs measured against the repository budgets, with the limit inclusive (NFR-002).

Facts accepted from an actor ref in a push are visible to later checks in the same push.

## Context

- Read `contracts/push-verdict.md` sections "Inputs", "Actor chain", "Proposal ref", "Memory ref" and the precedence notes; the plan's rule 2 ("Candidate code arrives after the candidate"); `research.md` "Validation reuse boundary".
- Start with `spec-kitty agent action implement WP08 --agent <name> --mission hn-serve-01M5P0NP`.
- From WP04 (`quarantine.go`): `validateQuarantinedActorRange(gitDir, actor, oldHead, newHead, hostedPrefix)` and `replicationEventDependencyWithOptions(..., replicationDependencyOptions{AllowAbsentProposalCode: true})`.
- From WP07: `serveVerdictContext` (hosted events, in-push fact slot, maintainers), the placeholder functions `checkServeActorUpdate`, `checkServeProposalUpdate`, `checkServeMemoryUpdate` that you replace, and the harness tests' snapshot helpers.
- Existing code to reuse: `measureQuarantinedSelectionUnderBudgets` (`quarantine.go`; kinds `replicationActor`, `replicationProposal`, `replicationMemory` from `replication.go`), `replicationBudgetExceeded`, `validateExactEventReferenceClosure` (`shallow.go`), `validateIssueRelationships` (`issue_projection.go`), `ProjectIdentityContinuity` (`identity_continuity.go`), `acceptedProposalHeads` (`quarantine.go`), `parseMemoryRef` and `loadMemoryStreamAt` (`memory_store.go`), `eventID`, `validEventID`, `isProposalKind`.
- Inside the hook, pass the hosted repository's `GIT_DIR` as both the quarantine and the accepted directory; Git's quarantine environment makes pushed objects visible to plain `git` calls.

## Branch Strategy

- **Planning/base branch at prompt creation**: `mission/hn-serve`
- **Final merge target for completed work**: `mission/hn-serve`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. Each computed lane gets exactly one worktree, and sequential WPs in the same lane reuse it.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `serve_verdict_facts.go` | NEW. Actor, proposal and memory rules and the budget mapping. |
| `serve_verdict.go` | CHANGE. Remove the three placeholder functions and call the new rules; extend the context's in-push fact slot if needed. Leave the protected placeholder for WP09. |
| `serve_facts_test.go` | NEW. |
| `docs/serve-v0.md` | CHANGE. "Hubnot fact refs and budgets" section only. |

## Subtasks

### Subtask T048: Actor chain rule

**Steps**: implement `checkServeActorUpdate(ctx, update)` in `serve_verdict_facts.go`, running the contract's steps in order and reporting the first failure:

1. **Rewrite**: `NEW` zero (deletion), or `OLD` non-zero and not an ancestor of `NEW` (`git merge-base --is-ancestor OLD NEW`): `actor-chain-rewrite`, `ids=OLD[,NEW]`, next `append new events instead`.
2. **Ownership**: the actor in the ref name differs from `ctx.Actor`: `actor-key-mismatch`, next `push only refs/hn/actors/<your actor>`.
3. **Budgets**: measure only what this push adds, without re-reading the hosted prefix event by event. Add a range variant beside the existing helper (for example `measureQuarantinedActorRangeUnderBudgets(gitDir, A, OLD, NEW, hostedPrefix, budgets, label)` in `quarantine.go`) that counts the hosted prefix from the batched hosted events already in `ctx` and walks only `OLD..NEW` for new events, objects and attachments; it must give the same totals and the same inclusive limits as `measureQuarantinedSelectionUnderBudgets` for the same chain, which a unit test asserts on a fixture chain. Ordinary client replication keeps calling the existing helper unchanged. This step measures sizes and counts only and does not verify signatures. On `replicationBudgetExceeded`, emit `over-budget` with `budget`, `limit` and `measured` (T051 maps them). Any other measurement error goes to the diagnostic walk in step 4, so it is reported as `actor-chain-invalid` with the full event ID or commit, never as a generic error.
4. **Chain validity**: build `hostedPrefix` from `ctx` hosted events whose actor is `A`, sorted by sequence; when `OLD` is non-zero its last commit must equal `OLD` (otherwise fail closed with `actor-chain-invalid`). Before calling `validateQuarantinedActorRange`, walk `OLD..NEW` yourself to find the first failing commit for diagnostics: for each commit read `event.json` (`git show COMMIT:event.json`) and, when it is readable, compute `eventID(payload)` so the rejection names the full event ID even when the signature is broken (US1-4); when unreadable, name the commit. Then call `validateQuarantinedActorRange(gitDir, A, OLD, NEW, hostedPrefix)`; an error is `actor-chain-invalid` with `ids=<first failing event ID or commit>`.
5. **Dependencies**: build `byID` from hosted events plus events accepted earlier in this push plus this chain's appended events; build `codeHeads` from hosted `refs/hn/proposals/*` (`acceptedProposalHeads` reads the current repository) plus the `NEW` value of every proposal ref in this push. For each appended event call `replicationEventDependencyWithOptions(gitDir, gitDir, event, byID, codeHeads, eventOwners, codeOwners, replicationDependencyOptions{AllowAbsentProposalCode: true})`. A returned dependency is `dependency-missing` with `ids=<dependency.Missing>` (full ID) and next `publish the named fact or commit first`; an error is `actor-chain-invalid` naming the event. `eventOwners` and `codeOwners` can be empty maps; they only shape client recovery text.
6. **Projection**: run `validateExactEventReferenceClosure`, `validateIssueRelationships` and `ProjectIdentityContinuity` on hosted events plus all accepted push events plus the appended events: failure is `actor-chain-invalid`.
7. On success, add the appended events to the context's in-push fact set.

**Files**: `serve_verdict_facts.go`.

**Notes**:
- Identity rotation: a clone whose local `refs/hn/actors/` holds a predecessor actor will try to push that chain with the successor's key and receive `actor-key-mismatch`. That is the designed behavior (OQ-1); the guide (T053) tells operators to bind a key for each actor.
- Never re-read the hosted prefix commit by commit; NFR-001 depends on the batched hosted read plus the range walk.

### Subtask T049: Proposal ref rule

**Steps**: `checkServeProposalUpdate(ctx, update)`:

1. `OLD` non-zero (update or deletion of a hosted ref): `proposal-ref-immutable`, next `create a revision instead`.
2. Candidate `sha256:<id>` not found in hosted events or accepted push events: `dependency-missing`, `ids=sha256:<id>`.
3. Candidate is not a proposal kind (`isProposalKind`), or `NEW` differs from `Event.Head`: `proposal-ref-mismatch`, `ids=<candidate>,<signed head>,<NEW>`, next `push the signed head`.
4. `NEW` does not descend from `Event.Base` (`merge-base --is-ancestor BASE NEW`, failing also when the base is absent): `proposal-ref-mismatch`.
5. Budgets with `replicationProposal`, `OLD` empty: `over-budget`.

**Files**: `serve_verdict_facts.go`.

### Subtask T050: Memory ref rule

**Steps**: `checkServeMemoryUpdate(ctx, update)`; parse the ref with `parseMemoryRef`:

1. Deletion, or non-zero `OLD` that is not an ancestor of `NEW`: `memory-invalid`.
2. Ref actor differs from `ctx.Actor`: `memory-key-mismatch`, next `push only your own memory refs`.
3. Budgets with `replicationMemory`: `over-budget`.
4. `loadMemoryStreamAt(gitDir, <source for this stream at NEW>)` fails: `memory-invalid`, `ids=<stream>`. Read `memory_store.go` to build the `memoryStreamSource` the same way client replication does (see `classifyReplicationMemoryDependencies` in `quarantine.go`).

**Files**: `serve_verdict_facts.go`.

### Subtask T051: Wire fact rules into the verdict and share in-push facts

**Steps**:

1. In `serve_verdict.go`, delete the three placeholder functions and route the classes to the new rules. Keep WP07's evaluation order (actor, memory, proposal, then the rest, then protected), so proposal refs and protected updates see facts accepted from actor refs in the same push.
2. If an actor update is rejected, its events must not enter the in-push fact set, so a proposal ref that depends on them is rejected with `dependency-missing`.
3. Budget mapping helper: given the budgets and the measurements returned with `replicationBudgetExceeded`, pick the first exceeded limit in the order events, objects, largest object, largest attachment, total bytes, and emit `budget=max-events|max-objects|max-object-bytes|max-attachment-bytes|max-total-bytes`, `limit=<budget>`, `measured=<value>`. Use the same names `hn serve repo show` prints.
4. Hosted event loading failure (`collectIssueEvents` error) fails closed: reject every Hubnot fact ref and every protected update in the push with `actor-chain-invalid` and the sentence `hn-serve: hosted facts could not be verified; ask the administrator.`.

**Files**: `serve_verdict.go`, `serve_verdict_facts.go`.

### Subtask T052: Fact-ref black-box tests including budgets

**Purpose**: US1-1, US1-3, US1-4, US1-5, FR-010 fact classes and NFR-002, through real `hn` and `git` clients.

**Steps** (tests `TestServeFacts...` in `serve_facts_test.go`). Set up identities with `hn init` in client clones made through the harness, bind each actor's SSH key, and use a policy on `main` listing real actors. Look at `replication_test.go`, `memory_replication_test.go` and `operational_acceptance_test.go` for helpers that create signed events, tamper with event commits and read refs.

1. **US1-1**: A creates a feature branch and a proposal, `git push origin feature/a` then `hn sync origin`. Both accepted; the hosted repository has `refs/heads/feature/a`, `refs/hn/actors/<A>` and `refs/hn/proposals/<id>`.
2. **Code-ref ordering**: the actor push (carrying `proposal.open`) is accepted before the proposal ref arrives; a separate later push of the proposal ref is accepted; a proposal ref pushed before its candidate's actor chain is rejected with `dependency-missing`; a single push carrying both is accepted.
3. **US1-3**: fetch B's actor ref into A's clone as `refs/hn/actors/<B>` and push it with A's key: `actor-key-mismatch`; B's hosted chain unchanged.
4. **US1-4**: append to A's chain a commit whose `event.json` was altered after signing (build with `git hash-object -w`, `git mktree`, `git commit-tree -p <head>`): `actor-chain-invalid` with `ids=<eventID of the altered payload>`.
5. **US1-5**: push `+<earlier commit>:refs/hn/actors/<A>` and a deletion: `actor-chain-rewrite`; a diverging chain (same sequence, different event): `actor-chain-rewrite`.
6. **Dependency missing**: A reviews B's proposal locally after importing it from B's clone directly (not through the server), then pushes A's chain while B's chain is not hosted: `dependency-missing` naming B's proposal event ID in full.
7. **Proposal refs**: move a hosted proposal ref: `proposal-ref-immutable`; delete it: same; point a new proposal ref at the wrong commit: `proposal-ref-mismatch` naming candidate, head and pushed commit.
8. **Memory refs**: A records memory with `hn memory record` and syncs (accepted); B pushes A's memory ref: `memory-key-mismatch`; a rewritten memory ref: `memory-invalid`.
9. **NFR-002 budgets**: set `--max-events` to A's current hosted chain length plus one; pushing one new event is accepted (at the limit); pushing two is rejected with `over-budget budget=max-events limit=N measured=N+1`. Repeat the at-limit and one-over pair for `--max-objects`. Then do the same for the three byte budgets, each with a small limit set through `hn serve repo set` so fixtures stay fast: `--max-object-bytes` (largest single object, at the limit and one byte over), `--max-attachment-bytes` (an event attachment such as a run log, at the limit and one byte over) and `--max-total-bytes` (total pushed bytes for the ref, at the limit and one byte over). Each one-over case is rejected with `over-budget budget=<name> limit=N measured=N+1`. After every over-budget rejection, all hosted refs are byte-identical.
10. **NFR-003**: every rejection in this file leaves refs byte-identical (snapshot helper from WP07) and emits exactly one parseable line per rejected ref.

**Files**: `serve_facts_test.go`.

### Subtask T053: Serve guide section on Hubnot fact refs and budgets

**Steps**:

1. Write "Hubnot fact refs and budgets" in `docs/serve-v0.md`: who may push each class; append-only actor chains and why; the validation performed (same as replication import) and that the server's acceptance is never evidence (C-006); proposal ref immutability and the revision path; memory refs and that `hn-memory/0` validation applies; the code-ref exception and why it cannot advance a protected branch; budgets, their defaults, inclusive limits and `hn serve repo set`; the identity-rotation note from T048; and the rejection codes for this section with links to the code table.
2. Include one marked example `<!-- example: reject-actor-key-mismatch -->` with the two relayed lines for US1-3, and a test `TestServeFactsDocumentedExample` that compares it field for field with real output after ID substitution.
3. Writing style: plain direct sentences, descriptive headings, no em dashes.

**Files**: `docs/serve-v0.md`, `serve_facts_test.go`.

## Test Strategy

Black-box pushes with real signed facts through the shim. Run `go test -race -run TestServeFacts ./...`, then the full gate.

## Definition of Done

- [ ] T048 to T053 complete; full gate passes.
- [ ] **FR-007**: actor updates accepted only from the mapped actor, only as appends, only when validation passes; failures name full IDs.
- [ ] **FR-010 (fact classes)**: proposal refs immutable and equal to signed heads; memory refs owned and valid.
- [ ] **FR-012**: a rejected fact ref rejects the whole push; refs byte-identical.
- [ ] **NFR-002**: at-limit accepted, one-over rejected, refs unchanged, for all five budgets; the range measurement matches the existing helper's totals.
- [ ] **NFR-003**: invalid signatures and missing dependencies fail closed.
- [ ] **C-007**: no path rewrites or deletes hosted actor or proposal history.
- [ ] Guide section written; its example is executed.

## Risks

- Proposal budgets measure the candidate's whole code graph when the ref is new, as client replication does; repositories with very large histories may need higher `--max-objects`. Document this in T053.
- `hn sync` pushes each actor, proposal and memory ref in its own `git push`; tests must account for several pushes per sync.

## Reviewer Guidance

- Compare each check with the contract's step list and order.
- Confirm the server never calls a per-event loader over the hosted prefix.
- Confirm rejected actor updates do not leak events into the in-push fact set.

## Activity Log

- 2026-10-25T00:09:18Z – system – Prompt generated via /spec-kitty.tasks
