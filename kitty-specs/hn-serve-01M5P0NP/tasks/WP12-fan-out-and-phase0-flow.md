---
work_package_id: WP12
title: Fan-out against hn serve and the Phase 0 flow
dependencies:
- WP10
- WP11
requirement_refs:
- FR-017
- FR-018
- FR-019
- NFR-006
planning_base_branch: mission/hn-serve
merge_target_branch: mission/hn-serve
branch_strategy: Planning artifacts for this mission were generated on mission/hn-serve. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into mission/hn-serve unless the human explicitly redirects the landing branch.
subtasks:
- T074
- T075
- T076
- T077
- T078
phase: Phase 5 - Fan-out
history:
- !!omap
  - at: '2026-10-25T00:09:18Z'
  - actor: system
  - action: Prompt generated via /spec-kitty.tasks
agent_profile: implementer-ivan
authoritative_surface: serve_replication_test.go
create_intent:
- serve_replication_test.go
- docs/serve-v0.md
execution_mode: code_change
owned_files:
- serve_replication_test.go
- docs/serve-v0.md
role: implementer
tags: []
tracker_refs: []
---

# WP12: Fan-out against hn serve and the Phase 0 flow

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter, and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `codex`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## Objective

This is the fan-in point of the server lane (WP01 to WP10) and the client lane (WP04, WP11). Prove end to end, against real hosted repositories through the forced command, that:

1. A hosted repository advertises every actor chain and proposal ref it holds, plus the marker, and clients identify it (FR-017).
2. A runner with only the all-trusted selection discovers a brand-new proposal and a maintainer-signed run request and executes it, with no per-actor or per-proposal selection (US4-1, FR-018).
3. A registered participant who has not published causes no failure (US4-2), and existing selections produce the same outcomes against `hn serve` as against an ordinary remote holding the same refs (US4-5, FR-019).
4. The rust-pingpong Phase 0 flow, reproduced with its five roles, completes against `hn serve` with zero actor or proposal selection commands and zero direct pushes of `main` (US4-6, SC-004).

Then document the runner and verifier workflow in the serve guide.

This WP is mostly tests. If a test exposes a defect in code owned by an earlier WP, fix it here with a minimal change and record a one-line rationale in the Activity Log; prefer fixes in server-lane files (`serve_*.go`), which this lane already holds. A defect in `replication.go`, `quarantine.go` or `commands.go` is still fixable here, because WP11 has merged by the time this WP starts; keep such a fix minimal and explain it.

## Context

- Read spec US4 (all scenarios), SC-004, `contracts/all-trusted-selection.md`, `quickstart.md` steps 4 to 6, and `../notes/phase0-hubnot-findings.md` if available (the Phase 0 run: maintainer, orchestrator, implementer, reviewer and runner; issue, implementation, proposal, CI request, review, CI run, acceptance, merge).
- Start with `spec-kitty agent action implement WP12 --agent <name> --mission hn-serve-01M5P0NP`.
- Reuse: `serveHarness`, `serveHarnessShim`, `serveHarnessClient` (WP05, WP06); `newServeGovernedFixture` (WP09); `hn merge --remote` (WP10); `hn replication select --all-trusted` (WP11).
- The runner runs CI with `hn runner once --accept-pipeline test --accept-actor <maintainer> --backend host --allow-unsafe-host-execution`, as the operational tests do, because the gate host may lack Bubblewrap.

## Branch Strategy

- **Planning/base branch at prompt creation**: `mission/hn-serve`
- **Final merge target for completed work**: `mission/hn-serve`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. Each computed lane gets exactly one worktree, and sequential WPs in the same lane reuse it.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `serve_replication_test.go` | NEW. |
| `docs/serve-v0.md` | CHANGE. "Runners and verifiers" section only. |

## Subtasks

### Subtask T074: FR-017 advertisement and identification through the shim

**Steps** (tests `TestServeReplication...` in `serve_replication_test.go`):

1. On a hosted repository with two published actors and two proposals, `git ls-remote` through the shim lists `refs/hn/serve/v1`, every `refs/hn/actors/*` and every `refs/hn/proposals/*` the server accepted, and nothing under `refs/hn/` that was refused.
2. `hn replication select origin --all-trusted` in a client clone succeeds (identification works through the forced command and protocol v2).
3. After `hn sync`, the client has no ref derived from `refs/hn/serve/v1` (the marker is never imported).
4. Server-side facts all passed FR-007 and FR-010 on arrival: cross-check by fetching every advertised actor ref into a scratch clone with exact selections and asserting each promotes.

**Files**: `serve_replication_test.go`.

### Subtask T075: US4-1 runner discovery and execution

**Steps**:

1. Runner clone: `hn replication select origin --all-trusted`, then `hn sync origin` once (nothing new yet).
2. Implementer opens a brand-new proposal and syncs; maintainer (who also uses all-trusted) syncs, runs `hn run request <proposal> test` and syncs.
3. Runner: `hn sync origin` promotes the new proposal and the run request (assert the output lines name them as promoted); `hn runner once ...` executes the request and records a result; `hn sync origin` publishes it, and the server accepts the runner's actor chain.
4. Assert the runner never ran a `hn replication select` command with `--actor` or `--proposal`.

**Files**: `serve_replication_test.go`.

### Subtask T076: US4-2 unpublished actor and FR-019 parity against hn serve

**Steps**:

1. **US4-2**: bind a key for actor X who never pushes. A client with `--all-trusted` plus `--actor X` syncs: exit 0, output `Replication actor X: not advertised yet`, no failure line. A client with only `--all-trusted` produces no line for X at all.
2. **FR-019 parity**: build an ordinary bare remote holding exactly the refs the hosted repository advertises (mirror the hosted refs with `git push --mirror` from a scratch clone, or fetch them into a bare repository; exclude nothing). For each of exact selection, `--all` and no selection, run identical scenarios against both remotes from fresh clones and compare the printed replication outcomes and the resulting `refs/hn/remotes/<remote>/*` values (normalize the remote name). They must be identical.

**Files**: `serve_replication_test.go`.

### Subtask T077: SC-004 Phase 0 flow end to end

**Purpose**: the brief's sixth acceptance example and SC-004.

**Steps**:

1. Fixture modeled on rust-pingpong: a small project with `main` carrying `.hn/policy.json` (one trusted approval, one passing `test` result from the runner, one maintainer acceptance) and `.hn/pipelines/test.json` running a portable shell check (no Rust toolchain needed; record in a comment that the real rust-pingpong run is a manual dogfooding check and this test reproduces its command flow). Host it with `hn serve repo create`.
2. Five participants, each with its own clone, `hn init` identity and bound SSH key: maintainer, orchestrator, implementer, reviewer, runner. Every participant runs `hn replication select origin --all-trusted` once.
3. Flow, each step followed by the actor's `hn sync origin`:
   - orchestrator opens a signed issue;
   - implementer reads it (`hn issue show --json`), creates a branch, commits, `git push origin <branch>`, opens a proposal;
   - maintainer requests CI (`hn run request`);
   - runner executes it (`hn runner once ... --backend host --allow-unsafe-host-execution`);
   - reviewer approves the exact head (`hn review --approve`);
   - maintainer accepts (`hn decide --accept`), then advances with `hn merge <proposal> --remote origin`;
   - orchestrator closes the issue.
4. Record every command each participant ran (wrap the harness runners to append to a per-test log). Assert: no command is `hn replication select` with `--actor` or `--proposal`; no command is a `git push` whose refspec targets `refs/heads/main`; the server push log shows `refs/heads/main` updated exactly once, by the maintainer, with `hn-advance` (check the `updates` entry and the maintainer actor).
5. A fresh verifier clone with no identity: `--all-trusted`, `hn sync`, `hn proposal status <candidate>` reports `merged` with the same evidence IDs, and `hn issue show` reports the issue closed (NFR-006).

**Files**: `serve_replication_test.go`.

### Subtask T078: Serve guide section for runners and verifiers

**Steps**:

1. "Runners and verifiers" in `docs/serve-v0.md`: why runners and verifiers need key bindings (they publish results), saving `--all-trusted` once, what each sync imports and validates, the `not advertised yet` line, memory streams needing exact selection, how a runner executes requests (`hn runner once` and `hn runner watch` with their accept flags), and the verifier recipe from quickstart step 6 (`git clone`, select, sync, `hn proposal status`). Link to the replication guide's all-trusted section for the trust boundary.
2. Add a short "End-to-end team flow" subsection that lists the Phase 0 sequence above as numbered commands, matching T077.
3. Writing style: plain direct sentences, descriptive headings, no em dashes.

**Files**: `docs/serve-v0.md`.

## Test Strategy

Black-box, multi-participant tests through the shim. These are the slowest tests in the mission; share fixtures within a test function with subtests, and keep each test under a few minutes. Run `go test -race -run TestServeReplication ./...`, then the full gate.

## Definition of Done

- [ ] T074 to T078 complete; full gate passes.
- [ ] **FR-017**: hosted repositories advertise every accepted actor and proposal ref and the marker; refused refs are never advertised; the marker is never imported.
- [ ] **FR-018**: US4-1 and US4-2 pass against `hn serve` with only the all-trusted selection.
- [ ] **FR-019**: exact, `--all` and no-selection outcomes are identical against `hn serve` and an ordinary remote holding the same refs.
- [ ] **SC-004**: the Phase 0 flow passes with zero actor or proposal selections and zero direct `main` pushes, proven from the command log and the server push log.
- [ ] **NFR-006**: the verifier clone reproduces all evidence without trusting the server.
- [ ] Guide section written.

## Risks

- A participant without a key binding cannot publish; the fixture binds every participant, runners included (research premortem).
- The flow is long; failures should print the command log to make diagnosis quick.

## Reviewer Guidance

- Read the command log assertion and confirm it would catch a hidden `--actor` selection or a direct `main` push.
- Confirm the parity test really compares both remotes with identical ref sets.

## Activity Log

- 2026-10-25T00:09:18Z – system – Prompt generated via /spec-kitty.tasks
