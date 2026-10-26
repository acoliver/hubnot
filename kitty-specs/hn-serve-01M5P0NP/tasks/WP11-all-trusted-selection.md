---
work_package_id: WP11
title: All-trusted selection on the client
dependencies:
- WP03
- WP04
requirement_refs:
- C-008
- FR-017
- FR-018
- FR-019
- NFR-006
planning_base_branch: mission/hn-serve
merge_target_branch: mission/hn-serve
branch_strategy: Planning artifacts for this mission were generated on mission/hn-serve. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into mission/hn-serve unless the human explicitly redirects the landing branch.
subtasks:
- T068
- T069
- T070
- T071
- T072
- T073
phase: Phase 5 - Fan-out
history:
- !!omap
  - at: '2026-10-25T00:09:18Z'
  - actor: system
  - action: Prompt generated via /spec-kitty.tasks
agent_profile: implementer-ivan
authoritative_surface: replication.go
create_intent:
- replication_all_trusted_test.go
execution_mode: code_change
owned_files:
- replication.go
- quarantine.go
- commands.go
- replication_all_trusted_test.go
- docs/replication-v0.md
role: implementer
tags: []
tracker_refs: []
---

# WP11: All-trusted selection on the client

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter, and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `codex`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## Objective

Add the client replication mode from `contracts/all-trusted-selection.md`:

```text
hn replication select [REMOTE] --all-trusted [--actor ACTOR]... [--proposal ID]... [--memory STREAM]... [budgets]
```

With it saved, `hn sync REMOTE` imports every actor chain and proposal that an identified `hn serve` remote advertises, without per-actor or per-proposal selection, while every ref still passes client quarantine, validation and per-ref budgets (FR-018, NFR-006). Existing exact, `--all` and no-selection modes behave exactly as before (FR-019). The mode is refused for remotes that do not identify as `hn serve` (FR-017 client side, OQ-3).

This WP runs in the client and validator lane, in parallel with the server lane. It tests against ordinary bare remotes that carry a hand-written marker blob, which is enough because identification gates the mode and grants no trust. WP12 repeats the key scenarios against real hosted repositories.

## Context

- Read `contracts/all-trusted-selection.md` (all of it), `research.md` "All-trusted selection storage", spec US4 and FR-017 to FR-019, and the IC-06 risks in the plan.
- Start with `spec-kitty agent action implement WP11 --agent <name> --mission hn-serve-01M5P0NP`.
- WP04 edited `quarantine.go` earlier in this lane (range validator, dependency option). Do not touch those additions.
- From WP03: `serveRemoteIdentifies(remote) (bool, string)`, `serveMarkerRef`, `serveMarkerContent`.
- Existing code: `ReplicationSelection`, `validateReplicationSelection`, `saveReplicationSelection`, `loadReplicationSelection`, `cmdReplicationSelect`, `printReplicationSelection` (`replication.go`); `resolveReplicationRequests`, `replicationTransactionResult.hasFailures`, `memoryReplicationRecovery`, status constants (`quarantine.go`); `cmdSync` (`commands.go`).
- `shallow.go` checks `selection.All` when deciding whether shallow recovery can use exact selectors. Leave `shallow.go` unchanged: shallow recovery keeps requiring exact selectors, and an all-trusted selection with exact selectors still exposes them. Record this decision in the replication guide.

## Branch Strategy

- **Planning/base branch at prompt creation**: `mission/hn-serve`
- **Final merge target for completed work**: `mission/hn-serve`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. This WP shares its lane worktree with WP04, which runs first.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `replication.go` | CHANGE. Field, validation, flag, select-time identification, show line, usage string. |
| `quarantine.go` | CHANGE. All-trusted request resolution, `not-advertised` status, `hasFailures`, memory recovery selection check. |
| `commands.go` | CHANGE. `hn sync` identification recheck and output line. |
| `replication_all_trusted_test.go` | NEW. |
| `docs/replication-v0.md` | CHANGE. New all-trusted section. |

## Subtasks

### Subtask T068: `AllTrusted` selection field, validation and select-time identification

**Steps**:

1. Add to `ReplicationSelection`: `AllTrusted bool `json:"allTrusted,omitempty"``. Place it after `All` in the struct. Because of `omitempty`, files written without the flag stay byte-identical to today's (FR-019, NFR-007); add a test that marshals a selection without the flag and compares with a golden file produced by the current code.
2. `validateReplicationSelection`:
   - `All && AllTrusted`: `--all-trusted is mutually exclusive with --all`.
   - The "at least one selector" rule is satisfied by `AllTrusted` alone.
   - Exact selectors combined with `AllTrusted` are validated exactly as today.
3. `cmdReplicationSelect`: add `allTrusted := flags.Bool("all-trusted", false, "import every actor chain and proposal advertised by an hn serve remote")`; update the usage error string to include `[--all-trusted]`. Before saving, when `--all-trusted` is set, require `serveRemoteIdentifies(remote)`; on false return `remote REMOTE does not identify as hn serve; --all-trusted requires an hn serve remote` and save nothing (US4-4). Run the mutual-exclusion check before the network call so the cheaper error wins.
4. An older `hn` binary rejects `allTrusted` through `DisallowUnknownFields`, which fails closed (research). Mention it in the docs.

**Files**: `replication.go`.

### Subtask T069: `hn replication show` all-trusted line

**Steps**: in `printReplicationSelection`, print `all-trusted: true` directly after the `compatibility-all:` line only when `AllTrusted` is set. Every other line is unchanged, so existing show assertions keep passing. Match the contract example output exactly.

**Files**: `replication.go`.

### Subtask T070: All-trusted request resolution and `not-advertised` status

**Steps** in `quarantine.go`:

1. Add the status constant `replicationNotAdvertised = "not-advertised"` beside the others.
2. In `resolveReplicationRequests`, add a branch for `selection.AllTrusted`:
   - Request every advertised `refs/hn/actors/<64-hex>` (valid fingerprint) and `refs/hn/proposals/<64-hex>` (valid event ID), in sorted ref order, exactly as the `All` branch builds them, but **never** memory refs.
   - Then add each exact actor and proposal selector unless already requested (deduplicate by `request.key()`). An exact actor or proposal selector that is not advertised produces an outcome with status `replicationNotAdvertised`, no diagnostic, no `MissingID`, and no request.
   - Exact memory selectors keep their current behavior, including `dependency-missing` when not advertised and the ambiguous-owner failure.
   - The marker ref `refs/hn/serve/v1` is outside the `ls-remote` patterns and is never requested; keep it that way.
3. `hasFailures`: ignore outcomes with `replicationNotAdvertised`. All other statuses keep their meaning.
4. `memoryReplicationRecovery`: when `selection.AllTrusted`, treat actor and proposal selectors as selected (they are requested whenever advertised); memory selectors still need exact selection.
5. Everything after request resolution (fetch into quarantine, budgets, structural, relationship and dependency validation, identity continuity, atomic promotion) is unchanged. Do not add a branch for `AllTrusted` anywhere in that path.

**Files**: `quarantine.go`.

### Subtask T071: `hn sync` identification recheck and output

**Steps** in `cmdSync` (`commands.go`):

1. After loading the selection and before `runReplicationTransaction`: when `selection.AllTrusted` and `serveRemoteIdentifies(remote)` is false, skip the transaction (nothing is fetched) and set the import error to `remote REMOTE no longer identifies as hn serve; reselect with hn replication select`. The publication phase still runs, and the command exits 1 through the existing error composition (`import phase failed (publication phase completed): ...`).
2. Output: for an outcome with status `not-advertised`, print `Replication actor ID: not advertised yet` (or `proposal`), using the existing loop. Other lines are unchanged.
3. Exact, `--all` and no-selection modes do not call `serveRemoteIdentifies` at all (FR-019).

**Files**: `commands.go`.

### Subtask T072: All-trusted client tests against ordinary remotes

**Steps** (tests `TestReplicationAllTrusted...` in `replication_all_trusted_test.go`). Build ordinary bare remotes with `git init --bare`; "marked" remotes also get `refs/hn/serve/v1` pointing at the `hn-serve/1\n` blob (written with `git hash-object -w --stdin` and `git update-ref`). Publish facts into them from author clones with `hn sync` (ordinary remotes accept any push). Use the existing operational helpers for building and running `hn`.

1. **US4-4**: `--all-trusted` against an unmarked remote is refused, names the remote, and writes no selection file.
2. **Mutual exclusion**: `--all --all-trusted` is refused before any network access (use a remote URL that would fail if contacted).
3. **Discovery**: on a marked remote, a runner clone saves `--all-trusted`; an author publishes a new proposal and a maintainer publishes a run request; the runner's `hn sync` promotes both without naming them (client-side part of US4-1).
4. **US4-2**: an exact actor selector for an actor with no advertised ref yields `not advertised yet`, and `hn sync` exits 0.
5. **US4-3**: all-trusted plus one exact `--memory` selector: only that memory stream imports; another advertised memory stream does not.
6. **Identification lost**: delete the marker ref from the remote after selecting; `hn sync` fetches nothing, still publishes local facts, prints the reselect message and exits 1.
7. **Forged marker grants no trust (NFR-006)**: on a marked ordinary remote, push an actor chain with a broken signature (craft as WP08 describes); the runner's `hn sync` rejects that ref through the normal validation path and promotes nothing from it.
8. **FR-019 parity**: for exact, `--all` and no-selection modes, run the same scenario against a marked and an unmarked remote holding the same refs; the printed outcomes and the resulting `refs/hn/remotes/*` are identical.
9. **Byte-identical files and output**: a selection saved without `--all-trusted` is byte-identical to the golden file; `hn replication show` output for existing selections is unchanged; with the flag, output matches the contract example exactly.
10. **Shallow recovery unchanged**: `hn sync --recover-shallow` with a pure all-trusted selection reports the existing exact-selection requirement.

**Files**: `replication_all_trusted_test.go`.

### Subtask T073: `docs/replication-v0.md` all-trusted section with tested example

**Steps**:

1. Add a section "All-trusted selection for hn serve remotes" after "Save a per-remote selection": what the mode requests, how the client identifies an `hn serve` remote and that identification grants no trust, that every ref still goes through quarantine, validation and budgets, the `not-advertised` status and why it is not a failure, memory streams needing exact selection, mutual exclusion with `--all`, the recheck on every sync, older binaries failing closed on the new field, and shallow recovery still needing exact selectors.
2. Add a short paragraph to "Private transaction state and threat boundary" stating the trust boundary: a malicious or misconfigured server can make a client fetch more refs, bounded by per-ref budgets, and can withhold refs; it cannot make the client accept an invalid fact.
3. Include the marked example `<!-- example: all-trusted-select -->` from the contract (command and full output) and add `TestReplicationAllTrustedDocumentedExample` that runs it against a marked remote and compares output exactly.
4. Writing style: plain direct sentences, descriptive headings, no em dashes.

**Files**: `docs/replication-v0.md`, `replication_all_trusted_test.go`.

## Test Strategy

Client-side black-box tests against ordinary bare remotes with and without the marker, using the built binary. Run `go test -race -run TestReplicationAllTrusted ./...`, then the full gate, paying attention to `replication_test.go`, `memory_replication_test.go`, `shallow_test.go` and `operational_acceptance_test.go`, which must pass unchanged.

## Definition of Done

- [ ] T068 to T073 complete; full gate passes.
- [ ] **FR-018**: the mode is saved only for identified remotes, excludes memory, combines with exact selectors, treats unadvertised exact selectors as normal, and keeps quarantine, validation and budgets.
- [ ] **FR-019**: exact, `--all` and no-selection outcomes are identical for marked and unmarked remotes; selection files and show output are byte-identical without the flag.
- [ ] **FR-017 (client)**: identification is rechecked on every sync.
- [ ] **NFR-006**: a forged marker cannot make the client accept an invalid fact.
- [ ] **C-008**: the replication guide documents the mode and its trust boundary; its example is executed.
- [ ] No pre-existing test assertion changed.

## Risks

- `hasFailures` drives the exit status of `hn sync`; only `not-advertised` may be ignored.
- Deduplicating exact selectors against advertised refs must not change request order in ways that alter output for existing modes; keep all new logic inside the `AllTrusted` branch.

## Reviewer Guidance

- Confirm `AllTrusted` appears only in selection storage, validation, show, request resolution, `hasFailures`, memory recovery and `cmdSync`.
- Confirm no code after request resolution branches on the mode.
- Diff a selection file written by the old and new binaries.

## Activity Log

- 2026-10-25T00:09:18Z – system – Prompt generated via /spec-kitty.tasks
