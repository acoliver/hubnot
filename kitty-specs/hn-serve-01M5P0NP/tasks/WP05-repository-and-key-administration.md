---
work_package_id: WP05
title: Repository and key administration commands
dependencies:
- WP01
- WP02
- WP03
requirement_refs:
- FR-002
- FR-003
- FR-004
- FR-023
- NFR-008
planning_base_branch: mission/hn-serve
merge_target_branch: mission/hn-serve
branch_strategy: Planning artifacts for this mission were generated on mission/hn-serve. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into mission/hn-serve unless the human explicitly redirects the landing branch.
subtasks:
- T027
- T028
- T029
- T030
- T031
- T032
- T033
phase: Phase 2 - Hosting and sessions
history:
- !!omap
  - at: '2026-10-25T00:09:18Z'
  - actor: system
  - action: Prompt generated via /spec-kitty.tasks
agent_profile: implementer-ivan
authoritative_surface: serve_admin.go
create_intent:
- serve_admin.go
- serve_admin_test.go
- serve_harness_test.go
- serve_commands.go
- docs/serve-v0.md
execution_mode: code_change
owned_files:
- serve_admin.go
- serve_admin_test.go
- serve_harness_test.go
- serve_commands.go
- docs/serve-v0.md
- README.md
role: implementer
tags: []
tracker_refs: []
---

# WP05: Repository and key administration commands

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter, and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `codex`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## Objective

Give the server administrator the full administration surface from `contracts/serve-cli.md`:

```text
hn serve repo create NAME --from SOURCE [--protect REF]... [--lfs-quota BYTES]
hn serve repo list
hn serve repo show NAME
hn serve repo set NAME [--protect REF]... [--unprotect REF]... [--lfs-quota BYTES]
                       [--max-events N] [--max-objects N] [--max-object-bytes N]
                       [--max-attachment-bytes N] [--max-total-bytes N]
hn serve repo lock-exec NAME -- COMMAND [ARG]...
hn serve key add --actor ACTOR KEYFILE|-
hn serve key remove FINGERPRINT
hn serve key list
```

Every command runs as the server user, validates state (WP01), regenerates the hook scripts and the managed `authorized_keys` block (WP02), and fails closed. Pushes never create repositories (FR-002, OQ-7, OQ-11). This WP also creates the black-box test harness that later WPs extend, and writes the administration section of the serve guide.

## Context

- Read `contracts/serve-cli.md` ("Administration"), `contracts/server-state.md`, `data-model.md` ("Hosted repository", "Protected branch"), `research.md` ("Repository seeding and recovery", "Server identification") and `quickstart.md` steps 2, 3 and 8.
- Start with `spec-kitty agent action implement WP05 --agent <name> --mission hn-serve-01M5P0NP`.
- From WP01: `cmdServe`, `serveLayout`, `ensureServeLayout`, `validateServeLayout` (administration mode), registry and config load/save, `acquireServeLock`, `serveLockWait`, `serveExecutablePath`, `ensureServeHooks`, `validServeRepositoryName`, `newServeRepoConfig`.
- From WP02: `parseServePublicKey`, `writeServeAuthorizedKeys`, test helper `newServeTestKey`.
- From WP03: `serveMarkerRef`, `serveMarkerContent`.
- From existing code: `parsePolicyBytes` (`policy.go`), `ReplicationBudgets` and its `validate()` (`replication.go`), `quietFlags` and `repeatedStringFlag` (`commands.go`, `replication.go`), `buildOperationalBinary` and `writeOperationalPolicy` (`operational_acceptance_test.go`).
- Note: the existing operational helpers give every command a fresh temporary `HOME`. Server commands need one stable server `HOME`, so the harness has its own runner.

## Branch Strategy

- **Planning/base branch at prompt creation**: `mission/hn-serve`
- **Final merge target for completed work**: `mission/hn-serve`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. Each computed lane gets exactly one worktree, and sequential WPs in the same lane reuse it.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `serve_admin.go` | NEW. All `repo` and `key` subcommands. |
| `serve_commands.go` | CHANGE. Add `case "repo"` and `case "key"` only. |
| `serve_harness_test.go` | NEW. Server-home harness; WP06 adds SSH helpers to it later. |
| `serve_admin_test.go` | NEW. |
| `docs/serve-v0.md` | CHANGE. "Administering hosted repositories" section only. |
| `README.md` | CHANGE. Add the serve guide to the documentation list. |

## Maintainer note: carried over from WP01

WP01 already implemented and tested `hn serve repo list`, `repo show`, `repo lock-exec` and `hn serve key list` (administration mode) in `serve_commands.go` and `serve_state.go`. Reuse them; do not reimplement. WP01 deliberately left these APIs for their first callers, which are in this WP: `saveServeRegistry`, `newServeRepoConfig` (contract defaults, protected list default-first) and `saveServeRepoConfig`. Their schemas and validators already exist in `serve_state.go`; add the writers there, atomically, using the existing private-file helpers, with tests for byte-stable round trips.

## Subtasks

### Subtask T027: `hn serve key add`, `remove` and `list`

**Purpose**: FR-003 and FR-004: bind keys to actors and keep `authorized_keys` generated from the registry.

**Steps**:

1. Administration preamble shared by every `repo` and `key` subcommand (write it once as a helper):
   - `ensureServeLayout(layout)`, then take an administration lock so two administrators cannot interleave registry or repository changes: `acquireServeLock(filepath.Join(layout.Tmp, "admin.lock"), serveLockWait())`. WP01's validator does not inspect `state/tmp/` contents, so the lock file there is allowed.
   - `validateServeLayout(layout, administration mode)`, which regenerates hook scripts (WP01 T006).
   - After the command's own change, rewrite the managed block with `writeServeAuthorizedKeys(layout, registry.Keys, serveExecutablePath())`. Rewriting on every administration command (also `repo` ones) is idempotent and repairs the block after `hn` moves, matching the hook regeneration rule.

2. `key add --actor ACTOR KEYFILE|-`:
   - `--actor` must pass `validActorFingerprint` (full 64-hex lowercase); refuse short or malformed input.
   - Read exactly one non-empty line from the file or stdin (bound the read, for example 16 KiB). More than one non-empty line is an error.
   - `parseServePublicKey`; refuse a fingerprint or canonical key already in the registry.
   - Build the new registry, render the block first (catches problems before any write), save the registry, then write `authorized_keys`.
   - Output exactly `Bound SHA256:... to actor <64-hex>`.

3. `key remove FINGERPRINT`: require `validServeFingerprint`; refuse unknown fingerprints; save registry, then rewrite the block.

4. `key list`: one line per binding, `FINGERPRINT ACTOR KEYTYPE`, sorted by fingerprint. Empty registry prints nothing.

5. Write order and failure: registry first, then `authorized_keys`. A failure after the registry write leaves the system fail-closed (an added key cannot connect yet; a removed key reaches `hn serve git`, finds no binding and is refused as `unknown-key` in WP06). Print `registry updated but authorized_keys was not rewritten: <cause>; rerun any hn serve key or repo command` and exit 1.

**Files**: `serve_admin.go`, `serve_commands.go`.

### Subtask T028: `hn serve repo create`

**Purpose**: FR-002, OQ-7, OQ-11: repositories exist only by administrator action and start in a governable state.

**Steps**:

1. Parse `NAME`, `--from SOURCE` (required), repeated `--protect REF` (short `main` or full `refs/heads/main`; default `refs/heads/main`) and `--lfs-quota BYTES` (positive). Refuse an invalid or existing name (check both `repositories/NAME.git` and `state/repos/NAME`).
2. Stage everything under a new directory in `state/tmp/` (for example `create-<random>`), so a failure leaves nothing in `repositories/` or `state/repos/`:
   - Detect the source object format from `git ls-remote SOURCE` OID length (40 is SHA-1, 64 is SHA-256; refuse anything else or an empty listing). Run `git init --bare --object-format=FORMAT staged/NAME.git`.
   - Fetch only branches and tags: `git --git-dir=staged/NAME.git -c fetch.fsckObjects=true fetch --no-tags SOURCE '+refs/heads/*:refs/heads/*' '+refs/tags/*:refs/tags/*'`. Run Git with `GIT_CONFIG_NOSYSTEM=1`. Never import `refs/hn/*`, `refs/notes/*` or anything else: facts must arrive through governed pushes so FR-017 holds.
   - Each protected ref must exist in the staged repository, and `git show TIP:.hn/policy.json` must parse with `parsePolicyBytes`. Otherwise refuse with a message naming the ref and the reason.
   - Write the marker: `git hash-object -w --stdin` with `serveMarkerContent`, then `git update-ref refs/hn/serve/v1 OID`.
   - `git symbolic-ref HEAD <first protected ref>`.
   - Do not write `core.hooksPath` or any hook into the repository; the wrapper passes hooks with `-c` (plan, IC-02 risk).
   - Write `config.json` via WP01 with protected list ordering (entry 0 is the default, the rest sorted).
   - Apply modes: the bare repository directory 0700. Leave Git's internal file modes as Git creates them, but make sure the top directory is 0700 so WP01's validator accepts it.
3. Commit by renaming `staged/state` to `state/repos/NAME` and then `staged/NAME.git` to `repositories/NAME.git`, then remove the staging directory. If the second rename fails, remove `state/repos/NAME` again. A crash between renames leaves a pairing defect, which WP01's validator reports, so the server refuses to serve instead of serving half a repository.
4. Output exactly:

   ```text
   Created hosted repository NAME
   Protected: refs/heads/main
   LFS quota: 1073741824 bytes
   Clone URL: ssh://USER@HOST/NAME.git
   ```

   One `Protected:` line per protected ref. `USER` is the current user's login (`os/user`), `HOST` is `os.Hostname()`.

**Files**: `serve_admin.go`.

### Subtask T029: `hn serve repo list` and `show`

**Steps**:

1. `repo list`: one name per line in byte order.
2. `repo show NAME`:

   ```text
   name: NAME
   path: /abs/.hn-serve/repositories/NAME.git
   protected: refs/heads/main
   lfs-quota: 1073741824
   lfs-stored: 0
   max-events: 10000
   max-objects: 100000
   max-object-bytes: 67108864
   max-attachment-bytes: 16777216
   max-total-bytes: 1073741824
   ```

   `lfs-stored` sums regular-file sizes under `state/repos/NAME/lfs/objects/` and is 0 when the directory does not exist. WP14 relies on this line; keep its name.

**Files**: `serve_admin.go`.

### Subtask T030: `hn serve repo set`

**Steps**:

1. Flags: repeated `--protect`, repeated `--unprotect`, `--lfs-quota`, and the five budget flags with the same names and validation as `hn replication select`.
2. Each newly protected ref must exist and carry a valid `.hn/policy.json` at its tip (same rule as create). Refuse unprotecting the last protected ref, and refuse a ref named in both `--protect` and `--unprotect`.
3. When the default (entry 0) is unprotected, the next remaining entry becomes the default; update `HEAD` with `git symbolic-ref`. Print the resulting configuration in the `repo show` format.
4. Save atomically through WP01. A refused change writes nothing.

**Files**: `serve_admin.go`.

### Subtask T031: `hn serve repo lock-exec`

**Purpose**: the documented recovery path for a protected tip without valid policy (OQ-11, quickstart step 8). WP06 tests also use it to hold the lock.

**Steps**:

1. Syntax: `hn serve repo lock-exec NAME -- COMMAND [ARG]...`. The `--` is required.
2. Take the repository lock (`RepoLock(name)`, wait `serveLockWait()`; on timeout print a `repository-busy` sentence and exit 1), then run `COMMAND` with `exec.Command` (no shell), working directory the bare repository, environment equal to the current one plus `GIT_DIR=<bare repo>`. Connect stdin, stdout and stderr.
3. Release the lock, then exit with the command's exit status. `main.go` maps errors to status 1, so call `os.Exit(status)` after releasing; document why in a comment. A command that cannot start exits 1 with the cause.
4. `lock-exec` does not hold the administration lock (a long recovery should not block key changes).

**Files**: `serve_admin.go`.

### Subtask T032: Administration black-box tests and server-home harness

**Purpose**: prove administration through the public CLI (C-009) and give later WPs one harness.

**Steps**:

1. `serve_harness_test.go` (helpers prefixed `serveHarness`):
   - `type serveHarness struct { t *testing.T; Binary, Home string }`; `newServeHarness(t)` builds the binary once per test with `buildOperationalBinary`, creates the server home with `t.TempDir()`, and skips with `t.Skip("hn serve refuses uid 0; run tests as an unprivileged user")` when `os.Geteuid() == 0`.
   - `(h *serveHarness) Serve(args ...string) string` and `ServeFailure(args ...string) string` run the binary with a minimal environment: `HOME=h.Home`, `PATH`, `GIT_CONFIG_NOSYSTEM=1`, `GIT_TERMINAL_PROMPT=0`, `LANG=C`, plus optional overrides (for example `HN_INTERNAL_TESTING=1`, `HN_TEST_SERVE_LOCK_WAIT=200ms`). Use a 60-second context timeout and include output in failures.
   - `serveHarnessSourceRepo(t, maintainers ...string) string`: a non-bare repository with a commit on `main` carrying `.hn/policy.json` (reuse `writeOperationalPolicy` or write a minimal valid `hn.policy/0` document), plus one extra branch and one tag, plus a fake `refs/hn/actors/<64-hex>` ref to prove it is not imported.
   - `serveHarnessAddKey(h, actor) serveTestKey` using `newServeTestKey` and `hn serve key add --actor ACTOR -` with the key on stdin.
2. `serve_admin_test.go` (tests `TestServeAdmin...`):
   - US2-1 (administration part): after `repo create` and `key add`, `authorized_keys` contains the begin and end markers and one line starting `restrict,command="<abs binary> serve git --key SHA256:...` followed by the canonical key and the `hn-serve:` comment.
   - US2-5 (administration part): after `key remove`, the line is gone and lines outside the block are byte-identical (write two admin lines before the first `key add`).
   - Import scope: the hosted repository has the branches, the tag, `refs/hn/serve/v1` pointing at the correct blob, `HEAD` at `refs/heads/main`, and no `refs/hn/actors/*`.
   - Creation refusals leave nothing behind: invalid name, existing name, missing protected branch, missing policy, malformed policy. After each, `repositories/` and `state/repos/` list exactly what they listed before.
   - `repo set`: protect a second branch with policy (accepted), protect one without policy (refused), unprotect the last (refused), change quota and budgets, invalid budget (refused).
   - `repo show` and `repo list` output.
   - `lock-exec`: exit status passthrough (`sh -c 'exit 7'` returns 7); while one `lock-exec ... sleep 2` runs, a second `lock-exec` with `HN_TEST_SERVE_LOCK_WAIT=200ms` fails with a busy message.
   - NFR-008 through the CLI: registry mode 0644, a symlinked `state/repos/NAME`, an unknown file in `state/`, and a repository directory without state each make `hn serve repo list` refuse and name the path.
   - uid refusal through the binary with `HN_INTERNAL_TESTING=1 HN_TEST_SERVE_EUID=0`.

**Files**: `serve_harness_test.go`, `serve_admin_test.go`.

### Subtask T033: Serve guide administration section, tested examples, README link

**Steps**:

1. Write "Administering hosted repositories" in `docs/serve-v0.md`: preparing the server account (adapt quickstart step 1), creating a repository from an existing one and why only branches and tags are imported, binding and removing keys (every participant, runners and verifiers included), listing and showing, changing protected branches and quotas and budgets, the regeneration rule, and the failure message after a partial key change. Leave recovery with `lock-exec` to WP09's "Recovering a protected branch" section; mention the command and link.
2. Include the two marked examples from the quickstart, `<!-- example: repo-create -->` and `<!-- example: key-add -->`, each a command block followed by an output block. Add `TestServeAdminDocumentedExamples` in `serve_admin_test.go` that extracts each marked example (follow the `<!-- example: rich-input -->` extraction in `issue_acceptance_test.go`), substitutes fixture values for the illustrative actor, fingerprint, user, host and paths, runs the command through the harness, and compares output line by line.
3. Add `- [hn serve: governed local Git hosting](docs/serve-v0.md)` to the documentation list in `README.md`.
4. Writing style: plain direct sentences, descriptive headings, no em dashes.

**Files**: `docs/serve-v0.md`, `serve_admin_test.go`, `README.md`.

## Test Strategy

Black-box tests drive the built binary with a stable server `HOME`. No SSH yet; WP06 adds the shim. Run `go test -race -run TestServeAdmin ./...`, then the full gate.

## Definition of Done

- [ ] T027 to T033 complete; full gate passes.
- [ ] **FR-002**: create, list, show and set behave as the contract states; pushes cannot create repositories (no code path does); creation refusals leave nothing behind.
- [ ] **FR-003**: full fingerprints and actors are required; one key binds to one actor; an actor may hold several keys (test with two keys for one actor).
- [ ] **FR-004**: after every registry change the managed block is rewritten with `restrict` and the forced command; outside lines are byte-identical.
- [ ] **NFR-008**: unsafe registry or layout state makes administration refuse and name the path.
- [ ] **FR-023 (part)**: the administration section exists and its two examples are executed by a test.
- [ ] No pre-existing test assertion changed.

## Risks

- `git fetch` from an administrator-supplied source is hostile input: `fetch.fsckObjects=true` and the refspec allowlist bound it.
- Renaming between `state/tmp` and the final paths requires one filesystem; the layout guarantees it because both live under `.hn-serve/`.
- `lock-exec` exits through `os.Exit`; never call it while holding any lock other than the one already released.

## Reviewer Guidance

- Run `repo create` against a source with `refs/hn/*` refs and confirm none are hosted.
- Interrupt `repo create` (for example by making the source unreadable mid-run in a test) and confirm nothing remains.
- Check the `authorized_keys` byte preservation test uses CRLF and a missing final newline.

## Activity Log

- 2026-10-25T00:09:18Z – system – Prompt generated via /spec-kitty.tasks
