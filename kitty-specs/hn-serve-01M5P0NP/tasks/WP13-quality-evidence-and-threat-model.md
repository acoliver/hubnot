---
work_package_id: WP13
title: Quality evidence, real sshd round trip and threat model
dependencies:
- WP12
requirement_refs:
- C-001
- C-009
- C-011
- FR-023
- NFR-001
- NFR-004
- NFR-007
planning_base_branch: mission/hn-serve
merge_target_branch: mission/hn-serve
branch_strategy: Planning artifacts for this mission were generated on mission/hn-serve. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into mission/hn-serve unless the human explicitly redirects the landing branch.
subtasks:
- T079
- T080
- T081
- T082
- T083
- T084
- T085
phase: Phase 6 - Quality evidence
history:
- !!omap
  - at: '2026-10-25T00:09:18Z'
  - actor: system
  - action: Prompt generated via /spec-kitty.tasks
agent_profile: implementer-ivan
authoritative_surface: serve_quality_test.go
create_intent:
- serve_quality_test.go
- serve_sshd_test.go
- docs/serve-v0.md
execution_mode: code_change
owned_files:
- serve_quality_test.go
- serve_sshd_test.go
- docs/threat-model.md
- docs/serve-v0.md
role: implementer
tags: []
tracker_refs: []
---

# WP13: Quality evidence, real sshd round trip and threat model

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter, and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `codex`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## Objective

Produce the cross-cutting evidence that individual WPs cannot, on the integrated candidate:

1. A guard that every `<!-- example: NAME -->` in `docs/` is executed by some test (FR-023).
2. A measurement of push verdict time for NFR-001.
3. An audit of every path the server writes during a full acceptance run (NFR-004) and of state modes (NFR-008).
4. One round trip through a real unprivileged OpenSSH server, the only proof of `restrict` semantics (C-002, OQ-9).
5. A traceability table from requirements and acceptance scenarios to tests, including the brief's six acceptance examples (SC-001, SC-006).
6. The consolidated `hn serve` threat model.
7. A green regression gate with every pre-existing test unmodified (NFR-007).

## Context

- Read spec Success Criteria, NFR-001, NFR-004, NFR-007, NFR-008, C-001, C-009, C-011; `contracts/server-state.md` (layout and the NFR-004 audit paragraph); `research.md` "Test harness" and "Premortem"; the charter's quality gates in `.kittify/charter/charter.md`.
- Start with `spec-kitty agent action implement WP13 --agent <name> --mission hn-serve-01M5P0NP`.
- Reuse every harness and fixture from WP05, WP06, WP09 and WP12.
- The brief's six acceptance examples (from `../notes/mission-1-hn-serve-brief.md`): (1) feature branch and actor chain accepted; (2) direct push to `main` rejected naming missing proposal, review, CI and acceptance; (3) another actor's chain rejected; (4) maintainer advances `main` and a fresh clone reproduces the evidence; (5) all-trusted runner discovers a new proposal and CI request; (6) Phase 0 flow with no `refs/hn` selection commands.

## Branch Strategy

- **Planning/base branch at prompt creation**: `mission/hn-serve`
- **Final merge target for completed work**: `mission/hn-serve`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. Each computed lane gets exactly one worktree, and sequential WPs in the same lane reuse it.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `serve_quality_test.go` | NEW. Example guard, timing, path audit, traceability check. |
| `serve_sshd_test.go` | NEW. Real sshd round trip. |
| `docs/threat-model.md` | CHANGE. Extend WP06's section into the full `hn serve` threat model. |
| `docs/serve-v0.md` | CHANGE. "Verification and traceability" section; light cross-link fixes elsewhere in the guide. |

## Subtasks

### Subtask T079: Documented-example coverage guard

**Steps**:

1. `TestServeQualityDocumentedExamplesAreExecuted`: scan every `docs/*.md` for `<!-- example: NAME -->` markers. For each `NAME`, require that some `*_test.go` file in the repository root contains the exact string `<!-- example: NAME -->` (the extracting tests use it as their search key, as `issue_acceptance_test.go` does for `rich-input`). Fail with the list of unexecuted names.
2. Also fail on duplicate marker names within one document.
3. This guard is decentralized: WP14 adds its LFS example and its own extracting test without editing this file.

**Files**: `serve_quality_test.go`.

### Subtask T080: NFR-001 verdict timing

**Steps**:

1. Fixture: a hosted repository holding 1,000 Hubnot events. Generate events quickly in-process inside a client clone (the test is in `package main`, so it can call `appendEvent` with a test identity as other tests do), across at least three actors, including one ready, accepted, CI-tested proposal whose head descends from `main`. Publish them with `hn sync` through the shim so the server validates them on arrival (raise budgets with `hn serve repo set` if the defaults are too low for setup pushes; they are not).
2. Snapshot the fixture once: copy the hosted bare repository and the server state for it into a pristine template directory, and copy the client clone as well. Each of 20 runs: restore the hosted repository, its server state and the client clone from that template (so every sample starts from exactly the same 1,000-event repository and the same `main` tip), append 50 new events to one actor chain locally, then time one `git push --atomic origin refs/hn/actors/<A> +<head>:refs/heads/main` through the shim. The push must be accepted. Assert before each timed push that the hosted repository holds exactly 1,000 events, so the workload never grows between samples; restoration time is excluded from the measurement.
3. Compute the 95th percentile of the 20 durations (sort and take the 19th value) and require it below 5 seconds. Log all durations with `t.Logf`.
4. Skip under `testing.Short()` with a visible reason; the full gate runs without `-short`. The `hn` binary used here is built without `-race`, so the race detector does not distort the measurement; note this in a comment.

**Files**: `serve_quality_test.go`.

### Subtask T081: NFR-004 written-path audit and NFR-008 mode audit

**Steps**:

1. Choose a compact full acceptance run: `repo create`, two `key add`, a feature branch push, `hn sync` of facts, a governed proposal advanced with `hn merge --remote`, a rejected direct `main` push and a `key remove`.
2. Before and after, walk the server `HOME` recursively and record path, type, size, mode and modification time. Run every server-side process with `TMPDIR` set to an extra sentinel directory outside `HOME` in the shim's environment, and record that directory too.
3. Assert every created or changed path is below `$HOME/.hn-serve/`, or is `$HOME/.ssh` or `$HOME/.ssh/authorized_keys`. Assert the sentinel directory is unchanged (the wrapper points Git's `TMPDIR` at `state/tmp`).
4. Mode audit: every directory under `.hn-serve/` outside `repositories/*.git/` is 0700; every file under `state/` is 0600; hook scripts are 0700; `authorized_keys` is 0600; `.ssh` is 0700.
5. Enforced write boundary: when `bwrap` (bubblewrap) is available, run the same acceptance run with the SSH shim launching every server-side process (`hn serve git`, the hooks and the Git programs they start) inside `bwrap --ro-bind / / --dev /dev --proc /proc --bind $HOME/.hn-serve $HOME/.hn-serve --bind $HOME/.ssh $HOME/.ssh` (no tmpfs and no other writable path: `/tmp`, the rest of `HOME` and every other absolute path stay read-only), and require the whole run to succeed. Every server-side `hn serve` invocation in the run goes through the same wrapper, including the administration commands (`repo create`, `key add`, `key remove`, `repo set`, `lock-exec`), so the boundary covers administration as well as every session, hook and Git descendant. Any write outside the allowed locations, including transient temporary files, then fails the run. Validate the boundary itself: under the same wrapper, an intentional write probe to `$HOME/outside`, to `/tmp` and to an absolute path outside `HOME` must each fail. Skip only this step with a visible reason when `bwrap` or unprivileged user namespaces are unavailable, and record the skip in the Activity Log as an evidence limitation for NFR-004; the snapshot audit in steps 2 and 3 remains as supplementary evidence. Put the wrapper in the shim helper so WP14 reuses it.
6. Note in a comment that WP14 extends this audit with LFS paths in its own test.

**Files**: `serve_quality_test.go`.

### Subtask T082: Real unprivileged sshd round trip

**Steps** (`TestServeSSHDRoundTrip` in `serve_sshd_test.go`):

1. Skip with a visible reason when `sshd` (`/usr/sbin/sshd` or on `PATH`), `ssh` or `ssh-keygen` is missing, when the effective uid is 0, or when `sshd` fails to start (include its log in the skip message).
2. Generate a host key and a client key with `ssh-keygen -t ed25519 -N ''` in a temp directory. Bind the client key with `hn serve key add` against a temporary server `HOME`.
3. Write an `sshd_config`: `ListenAddress 127.0.0.1`, a free `Port`, `HostKey`, `PidFile` in the temp directory, `AuthorizedKeysFile <server HOME>/.ssh/authorized_keys`, `PasswordAuthentication no`, `KbdInteractiveAuthentication no`, `UsePAM no`, `PermitUserEnvironment no`, `StrictModes yes` (if strict modes refuse the temp path because a parent such as `/tmp` is world-writable, set `StrictModes no` and say so in a comment). `sshd` runs the forced command with the account's real `HOME` from the password database; point it at the test server home with `SetEnv HOME=<server HOME>`. If the installed OpenSSH does not apply `SetEnv` to `HOME`, write the `AuthorizedKeysFile` copy with the command changed to `env HOME=<server HOME> <hn> serve git --key FP`, keep `restrict`, and document the deviation in the test.
4. Start `sshd -D -e -f <config>` as a child; wait for the port.
5. Client: `GIT_SSH_COMMAND="ssh -i <key> -p <port> -o UserKnownHostsFile=<file> -o StrictHostKeyChecking=accept-new -o IdentitiesOnly=yes"`. Clone the hosted repository from `ssh://<user>@127.0.0.1:<port>/NAME.git`, push a feature branch (accepted), push a direct `main` commit (rejected with a parseable `protected-no-proposal` line).
6. `restrict` semantics: `ssh -tt ... true` does not get a PTY and is refused by the forced command with `unsupported-command`; `ssh -N -L <port>:127.0.0.1:22 ...` forwarding is refused (assert the forward fails or `sshd` logs the denial).
7. Stop `sshd` and remove temp files in `t.Cleanup`.

**Files**: `serve_sshd_test.go`.

### Subtask T083: Requirement traceability and brief acceptance examples

**Steps**:

1. Write "Verification and traceability" in `docs/serve-v0.md`: a table with one row per FR, NFR and SC, and one row per user-story scenario (US1-1 to US5-4), naming the test function(s) that cover it. Add a second table mapping the brief's six acceptance examples to their tests. State that the real rust-pingpong project run is a manual dogfooding check and that `TestServeReplication...Phase0...` reproduces its command flow in the automated gate. Rows for FR-020, FR-021 and US5-1 to US5-4 are LFS rows. While `serve_lfs_test.go` does not exist (WP14 has not landed yet), each LFS row names its planned test and carries the exact marker `pending WP14`. If WP14 is moved to another mission, LFS rows carry `moved to its own mission` instead.
2. `TestServeQualityTraceabilityTestsExist`: parse the tables, extract every test name, and require a `func NAME(` declaration in some root `*_test.go` file. Exception for LFS rows only: when `serve_lfs_test.go` does not exist, an LFS row passes if it carries `pending WP14` or `moved to its own mission`. When `serve_lfs_test.go` exists, those markers are forbidden on every row and every LFS row's tests must be declared. Any non-LFS row with a marker fails. Fail with the missing names or misplaced markers. This keeps WP13's gate green and truthful before WP14 and makes it strict afterwards (C-010 separability is preserved).

**Files**: `docs/serve-v0.md`, `serve_quality_test.go`.

### Subtask T084: `docs/threat-model.md` consolidation

**Steps**: extend the "hn serve trust boundary" section that WP06 started into a complete threat model for the server and its clients:

1. Assets and actors: hosted repositories, protected branches, the key registry, signed facts; administrator, mapped actors, unmapped clients, other local users.
2. Hostile pushed data: quarantine before refs move, `receive.fsckObjects`, the replication validators reused for every Hubnot ref, budgets with inclusive limits, closed push option set.
3. Governance: the protected-branch invariant, base and destination policies, fast-forward only, no server commits and no server signatures (C-005, C-006), verdicts never evidence.
4. Concurrency: the session-wide lock, expected-tip checks, the 120-second wait budget and `repository-busy`.
5. Client side: identification grants no trust; all-trusted selection is bounded by per-ref budgets and full client validation; a server can withhold or delay refs but cannot make a client accept an invalid fact (NFR-006).
6. Administrator authority: `lock-exec` recovery bypasses governance by design; the administrator is trusted for availability and configuration, never for evidence.
7. Residual risks and non-goals: denial of service within budgets, a compromised server account, the excluded scope in C-011 (path-scoped approvals, delegated CI requesters, scheduling, queues, remote runners, web UI, GitHub API, publishing, per-repository access lists, runner cycle isolation, shared identity).
8. Leave a clearly marked place for WP14's LFS paragraph (a heading "Git LFS storage" with a one-line note that it applies only when LFS transfer is enabled).

Writing style: plain direct sentences, descriptive headings, no em dashes, claims tied to tests or to cited sources (`sshd(8)`, `githooks(5)`).

**Files**: `docs/threat-model.md`.

### Subtask T085: Full regression gate and pre-existing test audit

**Steps**:

1. Run on the integrated candidate: `gofmt -l .` (must print nothing), `go vet ./...`, `go build ./...`, `go test -race ./...`, `git diff --check`. Record each command and its result in the Activity Log.
2. Pre-existing tests: `git diff --diff-filter=M --name-only 6ecca57 HEAD -- '*_test.go'` (the planning commit on `mission/hn-serve`; use the mission's planning base if it differs) must print nothing. Any listed file is a NFR-007 violation to fix by restoring the assertions and adapting new code instead.
3. C-001 check: `git grep -n 'serve' -- commands.go replication.go quarantine.go governance.go` shows only the all-trusted and `--remote` additions; no existing command path requires a server.
4. C-011 check: confirm none of the excluded features was added (reviewer checklist item; note it in the Activity Log).

**Files**: none beyond fixes recorded in the Activity Log.

## Test Strategy

Integration-level tests over the whole server plus documentation checks. Run `go test -race -run 'TestServeQuality|TestServeSSHD' ./...`, then the full gate.

## Definition of Done

- [ ] T079 to T085 complete; full gate passes.
- [ ] **FR-023**: every documented example is executed (guard green); the guide has the traceability section; the threat model, replication guide, governance guide and protocol notes are updated (by WP03, WP04, WP06, WP11 and this WP).
- [ ] **NFR-001**: p95 under 5 seconds over 20 runs, durations logged.
- [ ] **NFR-004 / NFR-008**: the acceptance run passes inside the enforced write boundary, the out-of-bounds probes fail, the snapshot audit shows no write outside the allowed locations, and modes are as specified.
- [ ] **NFR-007**: gate green; no pre-existing test file modified.
- [ ] **C-001 / C-009 / C-011**: checks recorded.
- [ ] **SC-001 / SC-006**: the six brief examples map to passing tests; every requirement maps to tests, with LFS rows marked `pending WP14` until WP14 lands.
- [ ] The real-sshd test passes on hosts with OpenSSH and skips with a reason elsewhere.

## Risks

- Timing on a loaded host: keep the fixture exactly as specified and log raw durations for reviewers.
- The sshd test depends on local OpenSSH behavior; the skip conditions keep it honest without making the gate flaky.

## Reviewer Guidance

- Remove one extracting test locally and confirm the example guard fails.
- Check the path audit would catch a write to `$HOME/.cache`.
- Read the threat model against the code: every claim should match a test or a cited source.

## Activity Log

- 2026-10-25T00:09:18Z – system – Prompt generated via /spec-kitty.tasks
