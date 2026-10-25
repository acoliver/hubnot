---
work_package_id: WP14
title: Git LFS transfer and quota
dependencies:
- WP13
requirement_refs:
- C-003
- C-010
- FR-020
- FR-021
planning_base_branch: mission/hn-serve
merge_target_branch: mission/hn-serve
branch_strategy: Planning artifacts for this mission were generated on mission/hn-serve. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into mission/hn-serve unless the human explicitly redirects the landing branch.
subtasks:
- T086
- T087
- T088
- T089
- T090
- T091
phase: Phase 7 - Large files (separable)
history:
- !!omap
  - at: '2026-10-25T00:09:18Z'
  - actor: system
  - action: Prompt generated via /spec-kitty.tasks
agent_profile: implementer-ivan
authoritative_surface: serve_lfs.go
create_intent:
- serve_lfs.go
- serve_lfs_test.go
- serve_session.go
- docs/serve-v0.md
execution_mode: code_change
owned_files:
- serve_lfs.go
- serve_lfs_test.go
- serve_session.go
- docs/serve-v0.md
- docs/threat-model.md
role: implementer
tags: []
tracker_refs: []
---

# WP14: Git LFS transfer and quota

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter, and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `codex`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## Objective

Serve Git LFS objects for hosted repositories over the `git-lfs-transfer` SSH protocol, version 1, using only the Go standard library (FR-020, C-003):

- uploads stream into staging, are verified for size and SHA-256 against their object ID, and only then become downloadable;
- each repository has an inclusive byte quota, default 1 GiB, enforced before an object becomes downloadable (FR-021, OQ-8);
- locking commands answer `status 501` (unsupported);
- interrupted uploads are never downloadable and never counted.

**Separability (C-010)**: this is the last WP and nothing depends on it. If it moves to its own mission, WP06's `git-lfs-transfer` refusal stays in place, FR-020 and FR-021 stay open, and every other requirement is unaffected. Keep every change inside the files listed below so that move stays a clean cut.

## Context

- Read `contracts/lfs-transfer.md` (all of it), `contracts/server-state.md` (LFS paths and `lfs.lock`), `data-model.md` ("LFS object and quota"), `research.md` "Git LFS transfer path and storage", and spec US5.
- Protocol source: the Git LFS SSH adapter proposal, <https://github.com/git-lfs/git-lfs/blob/main/docs/proposals/ssh_adapter.md>. Where the proposal is silent, follow the behavior of the Git LFS 3.7.1 client on the gate host and pin it in tests (contract).
- Start with `spec-kitty agent action implement WP14 --agent <name> --mission hn-serve-01M5P0NP`.
- From WP01: `serveLayout` LFS paths (`RepoLFSDir`, `RepoLFSIncoming`, `RepoLFSObjects`, `RepoLFSLock`), `serveRepoConfig.LFSQuotaBytes`, `acquireServeLock`. WP01's validator already accepts `lfs/`, `lfs/incoming/`, `lfs/objects/` and `lfs.lock`.
- From WP03: `serveRejection`, `renderServeRejection` (the `lfs-quota` and `lfs-object-mismatch` codes, `budget=lfs-quota`).
- From WP05: `hn serve repo set --lfs-quota` and `repo show`'s `lfs-stored:` line.
- From WP06: the `git-lfs-transfer` grammar and the refusal arm (`serveLFSSession` or equivalent) you replace; the shim.
- From WP13: the example guard (add a test containing your example marker) and the NFR-004 audit pattern.

## Branch Strategy

- **Planning/base branch at prompt creation**: `mission/hn-serve`
- **Final merge target for completed work**: `mission/hn-serve`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. Each computed lane gets exactly one worktree, and sequential WPs in the same lane reuse it.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `serve_lfs.go` | NEW. pkt-line framing, protocol, storage, quota. |
| `serve_session.go` | CHANGE. Replace only the `git-lfs-transfer` refusal arm with a call into `serve_lfs.go`. |
| `serve_lfs_test.go` | NEW. |
| `docs/serve-v0.md` | CHANGE. "Large files with Git LFS" section only. |
| `docs/threat-model.md` | CHANGE. Fill the "Git LFS storage" placeholder WP13 left. |

## Subtasks

### Subtask T086: pkt-line reader and writer

**Steps**:

1. Reader: read the four-hex-digit length header; `0000` is flush, `0001` is delim; a value of 2 or 3 is malformed; any value above 65,519 decimal (the proposal's limit, header included as the proposal defines it; confirm against the proposal text and pin with a test) is rejected **before** allocating a buffer. Return text packets with one trailing `\n` removed and data packets unchanged.
2. Writer: text lines with `\n`, data packets chunked to the maximum payload, flush and delim.
3. A malformed packet ends the session with `status 400` (when the protocol state allows a reply) and exit 1.

**Files**: `serve_lfs.go`.

### Subtask T087: LFS session protocol, batch and session dispatch

**Steps**:

1. In `serve_session.go`, replace the refusal arm so a validated `git-lfs-transfer` request calls `runServeLFSSession(layout, binding, repo, op, os.Stdin, os.Stdout)`. Change nothing else in that file.
2. Session (contract table): send `version=1` then flush (never advertise `locking`). Then loop over commands:
   - `version 1` gets `status 200` and flush; any other version `status 400`.
   - `batch` with arguments `transfer`, `refname` and `hash-algo` (ignore other arguments, as the proposal requires; `hash-algo` must be absent or `sha256`, otherwise `status 400`), followed by `oid size` lines until flush. Each OID is 64 lowercase hex and each size decimal. For `upload`: a stored object gets action `noop`, a missing one `upload`. For `download`: every object gets `download`. No `id` or `token` values.
   - `put-object OID`, `verify-object OID` only in `upload` sessions; `get-object OID` only in `download` sessions. Wrong-session commands get `status 400`.
   - `lock`, `unlock`, `list-lock`: `status 501` with message `locking is unsupported by hn serve` (FR-021, US5-4).
   - `quit`: `status 200`, flush, exit 0.
   - Unknown command: `status 400` and exit 1.
3. At session start, remove staged files older than one hour from `lfs/incoming/` for this repository, and create `lfs/`, `lfs/incoming/` and `lfs/objects/` with mode 0700 when absent.

**Files**: `serve_lfs.go`, `serve_session.go`.

### Subtask T088: Staged uploads, verification, verify-object and get-object

**Steps**:

1. `put-object OID` with `size=N`: stream the data packets into a new 0600 file in `lfs/incoming/` (random name, `O_EXCL`), hashing with SHA-256 as you write. Stop reading past `N + 1` bytes.
2. After the final packet, require byte count `== N` and hash `== OID`. On mismatch: remove the staged file and answer `status 422` with the message `hn-serve/1 reject ref=- actor=ACTOR code=lfs-object-mismatch ids=OID next="retry the upload"` (render with WP03's renderer, first line only) (US5-2).
3. On match: quota recheck under `lfs.lock` (T089), then rename into `lfs/objects/OID[0:2]/OID[2:4]/OID` (create intermediate directories 0700), `fsync` the file and directories, answer `status 200`. An object that already exists is not replaced; remove the staged copy and answer `status 200`.
4. `verify-object OID size=N`: `status 200` when the stored object exists with size `N`, else `status 404`.
5. `get-object OID`: when stored, `status 200`, argument `size=N`, delim, the bytes as data packets, flush; otherwise `status 404`. Open the file with `Lstat` checks (regular file, not a symlink).
6. A lost connection mid-upload leaves only the staged file, removed by the next session's cleanup.

**Files**: `serve_lfs.go`.

### Subtask T089: LFS quota checks and staged-file cleanup

**Steps**:

1. Stored bytes = sum of regular-file sizes under `lfs/objects/`. Staged files never count.
2. Check points (contract table):
   - `batch` for upload: stored bytes plus the declared sizes of every requested object that is not stored above the quota: the whole batch answers `status 507` with a `code=lfs-quota` line carrying `budget=lfs-quota limit=N measured=M`, where `M` is the stored bytes the upload would produce.
   - `put-object` before reading data: stored plus declared size above quota: `status 507` with the `lfs-quota` line; drain and discard the data.
   - `put-object` after verification: under `lfs.lock` (`acquireServeLock(layout.RepoLFSLock(name), short wait)`), recompute stored bytes; if adding this object exceeds the quota, remove the staged file and answer `status 507`; otherwise rename into place while still holding the lock.
3. The limit is inclusive: with quota `N` and stored `N - 10`, a 10-byte object is accepted and an 11-byte object is refused (US5-3).
4. LFS sessions never take the push lock (`RepoLock`); only `lfs.lock`, briefly.

**Files**: `serve_lfs.go`.

### Subtask T090: LFS black-box tests

**Steps** (tests `TestServeLFS...` in `serve_lfs_test.go`). Skip with a visible reason when `git lfs version` fails. Client config: `git lfs install --local`, `git config lfs.ssh.automultiplex false` (the shim does not support SSH multiplexing, plan IC-07 risk).

1. **US5-1**: A tracks `*.bin`, commits a 200 KiB random file, pushes branch and objects through the shim; a second clone (key B) fetches and gets byte-identical content. `hn serve repo show` reports `lfs-stored:` equal to the object size.
2. **US5-2**: drive `hn serve git --key <A>` directly with `SSH_ORIGINAL_COMMAND="git-lfs-transfer 'r.git' upload"` and hand-written pkt-lines: `version 1`, then `put-object <oid>` with content that does not hash to the OID: `status 422` with a parseable `lfs-object-mismatch` line; nothing under `lfs/objects/`; `lfs/incoming/` empty.
3. **US5-3**: set the quota to `N` with `hn serve repo set --lfs-quota N` where stored bytes are `N - 10`; uploading a 10-byte object is accepted; uploading an 11-byte object instead is refused with `status 507` (`code=lfs-quota`, `limit=N`, `measured=N+1`) before it becomes downloadable (a `get-object` for it returns `404`). Pin the Git LFS 3.7.1 client's visible behavior for the refusal (exit status and message) as the contract requires.
4. **US5-4**: `lock` and `list-lock` commands answer `status 501` with the unsupported message; the capability advertisement does not include `locking`.
5. **Interruption**: close stdin mid `put-object`; the next session removes staged files older than one hour (use an `HN_INTERNAL_TESTING` override for the age threshold, gated like other test hooks, or set the staged file's mtime back with `os.Chtimes`); the object is never downloadable and `lfs-stored:` is unchanged.
6. **Framing**: a length header of `ffff` is refused without a large allocation; unknown commands end the session with `status 400`.
7. **NFR-004 extension**: the WP13 path audit pattern applied to an LFS round trip: new paths only under `state/repos/NAME/lfs/` and `lfs.lock`, with modes 0700 and 0600.
8. **Documented example**: extract the `<!-- example: lfs-quota -->` block from the guide (T091) and compare with real output; this also satisfies WP13's example guard.

**Files**: `serve_lfs_test.go`.

### Subtask T091: Serve guide LFS section and threat-model LFS paragraph

**Steps**:

1. "Large files with Git LFS" in `docs/serve-v0.md`: client requirements (Git LFS 3.x with SSH transfer; set `lfs.ssh.automultiplex=false` if multiplexing causes problems), what the server stores and where, verification before download, the inclusive quota and how to raise it, `lfs-stored:` in `repo show`, staged-file cleanup, locking unsupported, and that `git-lfs-authenticate` and HTTP transfer are not offered. Include a marked example `<!-- example: lfs-quota -->` showing the `lfs-quota` rejection line.
2. `docs/threat-model.md`, under WP13's "Git LFS storage" heading: content addressing and verification, quota as the bound on storage, staging isolation, no locking, LFS sessions use the same key attribution and repository checks as Git sessions, and residual risk (a mapped actor can fill the quota).
3. Update the traceability rows for US5 in `docs/serve-v0.md` only if WP13 left them pointing at different test names.
4. Writing style: plain direct sentences, descriptive headings, no em dashes.

**Files**: `docs/serve-v0.md`, `docs/threat-model.md`.

## Test Strategy

Real Git LFS client through the shim for round trips, plus direct pkt-line sessions for protocol edges. Run `go test -race -run TestServeLFS ./...`, then the full gate.

## Definition of Done

- [ ] T086 to T091 complete; full gate passes.
- [ ] **FR-020**: uploads and downloads work for mapped keys over `git-lfs-transfer`; content is verified before it becomes downloadable.
- [ ] **FR-021**: inclusive quota enforced at all three check points; locking answers unsupported; default quota 1 GiB.
- [ ] **C-003**: no new dependency.
- [ ] **C-010**: the only change outside `serve_lfs*.go` and the docs is the one arm in `serve_session.go`.
- [ ] Interrupted uploads are never downloadable or counted.
- [ ] Guide section, threat-model paragraph and executed example in place; WP13's example guard and traceability check stay green.

## Risks

- The proposal leaves per-object batch errors undefined; pin observed client behavior and keep the server answers simple and documented.
- Large uploads: stream with a fixed buffer; never read a whole object into memory.

## Reviewer Guidance

- Confirm nothing becomes downloadable before verification and the quota recheck.
- Confirm the pkt-line length check happens before allocation.
- Confirm the `serve_session.go` diff is a single arm.

## Activity Log

- 2026-10-25T00:09:18Z – system – Prompt generated via /spec-kitty.tasks
