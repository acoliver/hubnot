---
work_package_id: WP01
title: Server state foundation and the hn serve entry point
dependencies: []
requirement_refs:
- C-001
- C-003
- FR-001
- FR-003
- NFR-004
- NFR-008
planning_base_branch: mission/hn-serve
merge_target_branch: mission/hn-serve
branch_strategy: Planning artifacts for this mission were generated on mission/hn-serve. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into mission/hn-serve unless the human explicitly redirects the landing branch.
subtasks:
- T001
- T002
- T003
- T004
- T005
- T006
- T007
phase: Phase 1 - Foundations
history:
- !!omap
  - at: '2026-10-25T00:09:18Z'
  - actor: system
  - action: Prompt generated via /spec-kitty.tasks
agent_profile: implementer-ivan
authoritative_surface: serve_state.go
create_intent:
- serve_state.go
- serve_state_test.go
- serve_commands.go
- docs/serve-v0.md
execution_mode: code_change
owned_files:
- serve_state.go
- serve_state_test.go
- serve_commands.go
- main.go
- docs/serve-v0.md
role: implementer
tags: []
tracker_refs: []
---

# WP01: Server state foundation and the hn serve entry point

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter, and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `codex`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## Objective

Create the foundation every other `hn serve` work package builds on:

1. An `hn serve` command group that refuses to run as uid 0 and validates the whole server state layout before it dispatches any subcommand.
2. One fixed server home under `$HOME/.hn-serve/` with typed, fail-closed access to the key registry, per-repository configuration, branch-owner records, the push log, the repository locks and the generated hook scripts.
3. The skeleton of the new serve guide `docs/serve-v0.md`, with the server account and state layout section written.

This WP adds no network-facing behavior. Nothing in it is reachable from any existing `hn` command, which keeps C-001 (optional service) and FR-001 (no existing command gains a dependency) true.

## Context

- Mission: `hn-serve-01M5P0NP`. Read `kitty-specs/hn-serve-01M5P0NP/spec.md`, `plan.md`, `data-model.md` and especially `contracts/server-state.md` and `contracts/serve-cli.md` before coding.
- Start with `spec-kitty agent action implement WP01 --agent <name> --mission hn-serve-01M5P0NP`. Use only the runtime-assigned lane worktree.
- The repository is one flat Go `package main` (Go 1.26, standard library only, C-003). Errors go to stderr as `hn: <message>` through `main.go`, exit status 1.
- Reuse the private-file helpers in `identity_keyring.go`: `ensurePrivateDirectory`, `validatePrivateDirectory`, `readPrivateFileBounded`, `decodePrivateJSON`, `writePrivateFileAtomic`, `replacePrivateFileAtomic`, `checkPrivateDirectory`. Their messages mention "private identity"; wrap errors with serve-specific context (for example `fmt.Errorf("hn serve state %s: %w", path, err)`) instead of changing those helpers.
- Reuse `ReplicationBudgets`, `defaultReplicationBudgets()` and `ReplicationBudgets.validate()` from `replication.go` for the repository budgets.
- Reuse `validActorFingerprint` from `identity_keyring.go` for actor fields.
- The `HN_INTERNAL_TESTING=1` gate pattern for test-only behavior already exists in `identity_keyring.go` (`runIdentityStorageHook`) and `quarantine.go` (`runReplicationProcessHook`). Follow it.

## Branch Strategy

- **Planning/base branch at prompt creation**: `mission/hn-serve`
- **Final merge target for completed work**: `mission/hn-serve`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json` and records the lane branch in `base_branch`. Each computed lane gets exactly one worktree, and sequential WPs in the same lane reuse it.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`; do not create a different worktree by hand.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `serve_commands.go` | NEW. Dispatcher `cmdServe`, uid-0 refusal, layout validation call. Later WPs (WP05, WP06, WP07) each add one `case` line here. |
| `serve_state.go` | NEW. Layout, validation, registry, config, branch owners, push log, locks, hook scripts. |
| `serve_state_test.go` | NEW. Unit tests for this WP. |
| `main.go` | CHANGE. Route `serve`; add the three `hn serve` usage lines. WP10 later adds the `hn merge ... --remote` usage line. |
| `docs/serve-v0.md` | NEW. Guide skeleton plus the "Server account and state layout" section. Later WPs fill the other sections. |

Do not edit other files. A small, justified out-of-map edit is acceptable only with a one-line rationale in the Activity Log.

## Subtasks

### Subtask T001: `cmdServe` dispatcher, uid-0 refusal, `main.go` route and usage

**Purpose**: give every later `hn serve` subcommand one entry point that applies the uid and layout checks first (NFR-004, NFR-008).

**Steps**:

1. In `main.go`, add `case "serve": return cmdServe(args[1:])` to `run`. Add these lines to `printUsage` directly after `hn merge PROPOSAL` (WP10 will insert the `--remote` line above them later):

   ```text
     hn serve repo create|list|show|set|lock-exec ...
     hn serve key add|remove|list ...
     hn serve git --key FINGERPRINT
   ```

   `namespace_cutover_test.go` asserts that the usage output contains `Usage:\n  hn init`; keep that true.

2. Create `serve_commands.go` with:

   ```go
   func cmdServe(args []string) error {
       if err := refuseServeAsRoot(); err != nil {
           return err
       }
       if len(args) == 0 {
           return usageError("usage: hn serve repo|key|git|hook ...")
       }
       layout, err := resolveServeLayout()
       if err != nil {
           return err
       }
       // Administration subcommands regenerate hooks before validation (T006).
       switch args[0] {
       // WP05 adds: case "repo", case "key"
       // WP06 adds: case "git"
       // WP07 adds: case "hook"
       default:
           return fmt.Errorf("unknown hn serve subcommand %q; run 'hn help'", args[0])
       }
   }
   ```

   Keep the validation call inside the dispatcher so a later subcommand cannot skip it. Choose the exact structure yourself, but each later WP must be able to add its route with one `case` that receives an already validated `serveLayout`. Provide a helper such as `validateServeLayout(layout, serveValidationMode)` where the mode distinguishes administration commands (which may repair hook scripts, T006) from session and hook commands (which never write).

3. Implement `refuseServeAsRoot()`: when `os.Geteuid() == 0`, return exactly `hn serve refuses to run as root; create an unprivileged user for the server` (printed by `main.go` with the `hn: ` prefix). The check runs before any state is read, so it must come before `resolveServeLayout`.

4. Test-only override: when `HN_INTERNAL_TESTING=1` and `HN_TEST_SERVE_EUID` is set to a decimal integer, use that value in place of `os.Geteuid()`. Parse it strictly; a malformed value is an error. Without `HN_INTERNAL_TESTING=1` the variable is ignored. This lets tests on a normal account exercise the refusal, and the plan's risk table requires it.

**Files**: `main.go`, `serve_commands.go`.

**Validation**:
- [ ] `hn serve` with no arguments prints the usage error and exits 1.
- [ ] With `HN_INTERNAL_TESTING=1 HN_TEST_SERVE_EUID=0`, every `hn serve ...` invocation prints the root refusal and touches nothing under `$HOME` (assert the directory listing is unchanged).
- [ ] `HN_TEST_SERVE_EUID=0` without `HN_INTERNAL_TESTING=1` has no effect.
- [ ] `hn help` output still contains `Usage:\n  hn init` and the three new lines.

### Subtask T002: Server home layout resolution and fail-closed layout validation

**Purpose**: one source of truth for every server path, and one validator that refuses to serve on any defect (NFR-008).

**Steps**:

1. Define a `serveLayout` struct with absolute paths: `Home`, `Root` (`$HOME/.hn-serve`), `Hooks`, `Repositories`, `State`, `Registry` (`state/registry.json`), `Tmp` (`state/tmp`), `Repos` (`state/repos`), `SSHDir` (`$HOME/.ssh`) and `AuthorizedKeys`. Add helpers for per-repository paths: `RepoGitDir(name)` (`repositories/NAME.git`), `RepoStateDir(name)`, `RepoConfig(name)`, `RepoBranches(name)`, `RepoPushLog(name)`, `RepoLock(name)`, `RepoLFSLock(name)`, `RepoLFSDir(name)`, `RepoLFSIncoming(name)`, `RepoLFSObjects(name)`.

2. `resolveServeLayout()` reads `HOME` (fail when unset, empty or relative) and builds the struct. It does not create anything.

3. `ensureServeLayout(layout)` creates `.hn-serve/`, `hooks/`, `repositories/`, `state/`, `state/tmp/` and `state/repos/` with mode 0700 through `ensurePrivateDirectory`. Only administration commands call it (WP05). Session and hook commands require the layout to exist and refuse otherwise.

4. `validateServeLayout(layout, mode)` enforces every rule in `contracts/server-state.md`, "Layout" and the paragraph after it:
   - No symlink anywhere in the layout (use `os.Lstat` on every path you walk).
   - Every directory is mode 0700, every state file 0600. Report the path and the observed mode.
   - `state/` contains only `registry.json`, `tmp` and `repos`. `state/repos/` contains only directories whose names pass the repository-name grammar (T004). `state/repos/NAME/` contains only `config.json`, `branches.json`, `push.log`, `lock`, `lfs.lock` and `lfs/`. `lfs/` contains only `incoming/` and `objects/`. Anything else is a defect naming the path.
   - Every `state/repos/NAME/` has a matching `repositories/NAME.git/` real directory, and the reverse. Do not inspect inside the bare repository beyond confirming it is a directory.
   - `config.json` exists for every repository and decodes (T004); `registry.json` decodes when present (T003); `branches.json` decodes when present.
   - Do not validate contents of `state/tmp/`, `lfs/incoming/` or `lfs/objects/` beyond "real directory with mode 0700"; WP14 owns object-level checks.
   - `.ssh/` and `authorized_keys` are validated by WP02's writer. This validator only refuses a `.ssh` that is a symlink.

5. Every defect returns an error of the form `hn serve state defect: <path>: <reason>`. The first defect found stops validation; keep the walk order deterministic (sorted names) so messages are stable in tests.

**Files**: `serve_state.go`.

**Validation**:
- [ ] Each defect class above has a test that creates exactly that defect in a temporary `HOME` and asserts the message names the path.
- [ ] A freshly ensured, empty layout validates.

### Subtask T003: Key registry types, decoding, encoding and invariants

**Purpose**: the owner-only registry from OQ-1 and FR-003, decoded strictly so a malformed file stops the server (NFR-008).

**Steps**:

1. Types matching `contracts/server-state.md`:

   ```go
   type serveRegistry struct {
       Version int              `json:"version"`
       Keys    []serveKeyBinding `json:"keys"`
   }
   type serveKeyBinding struct {
       Fingerprint string `json:"fingerprint"`
       PublicKey   string `json:"publicKey"`
       Actor       string `json:"actor"`
       Added       string `json:"added"`
   }
   ```

2. `loadServeRegistry(layout)`: a missing file is an empty registry (`Version: 1`, no keys). Any other read error is fatal. Read with `readPrivateFileBounded` (16 MiB limit) and decode with `decodePrivateJSON` or an equivalent decoder that uses `DisallowUnknownFields` and rejects trailing data.

3. `validateServeRegistry`: version must be 1; keys sorted by fingerprint; fingerprints unique; public keys unique; `Actor` passes `validActorFingerprint`; `Added` parses as RFC 3339 UTC. Fingerprint syntax: define `validServeFingerprint(value string) bool` here (prefix `SHA256:`, exactly 43 characters of unpadded standard base64, decodes to 32 bytes). WP02 and WP06 reuse it, so keep the name. WP02 owns key parsing; this WP checks only the fingerprint shape and that `PublicKey` is a non-empty single line with exactly two space-separated fields. WP05 recomputes the fingerprint from the public key on load through WP02's parser when it needs to; leave a short comment noting that.

4. `saveServeRegistry(layout, registry)`: validate, marshal with stable indentation and a trailing newline, write with `replacePrivateFileAtomic` (0600).

5. `lookupServeKey(registry, fingerprint) (serveKeyBinding, bool)` for WP06.

**Files**: `serve_state.go`.

**Validation**:
- [ ] Round trip of a two-key registry is byte-stable.
- [ ] Unknown field, duplicate fingerprint, duplicate public key, unsorted keys, short actor, malformed fingerprint, trailing data and a file over 16 MiB are each refused.

### Subtask T004: Repository config and branch-owner records

**Purpose**: per-repository settings (FR-002, OQ-5) and the ownership state FR-011 needs, as strict typed records.

**Steps**:

1. Repository-name grammar from `data-model.md`: `[A-Za-z0-9][A-Za-z0-9._-]{0,99}`, never containing `..`, never ending in `.git`. Implement `validServeRepositoryName(name string) bool` with explicit byte checks (no regular expression is needed).

2. Config type:

   ```go
   type serveRepoConfig struct {
       Version       int                `json:"version"`
       Name          string             `json:"name"`
       Protected     []string           `json:"protected"`
       LFSQuotaBytes int64              `json:"lfsQuotaBytes"`
       Budgets       ReplicationBudgets `json:"budgets"`
   }
   ```

   Validation: version 1; `Name` equals the directory name; `Protected` non-empty, unique, each `refs/heads/` followed by a name that `git check-ref-format --branch` would accept (implement a conservative check in Go: no `..`, no control bytes, no space, `~`, `^`, `:`, `?`, `*`, `[`, `\`, no leading `-` or `/`, no trailing `/` or `.lock`, no `//`, no `@{`). Keep the first entry first: it is the default protected branch, so do not sort the list. Reject duplicates. `LFSQuotaBytes > 0`. `Budgets.validate()` passes.

   Note: `data-model.md` says the list is sorted and the first entry is the default branch; those two statements conflict when the default is not alphabetically first. Resolve it this way: entry 0 is the default protected branch, entries 1..n are sorted and unique. Record this in a comment and in the guide.

3. `newServeRepoConfig(name, protected)` returns defaults: `refs/heads/main` when `protected` is empty, quota `1073741824`, `defaultReplicationBudgets()`.

4. Branch owners:

   ```go
   type serveBranchOwners struct {
       Version  int                `json:"version"`
       Branches []serveBranchOwner `json:"branches"`
   }
   type serveBranchOwner struct {
       Ref     string `json:"ref"`
       Owner   string `json:"owner"`
       Created string `json:"created"`
   }
   ```

   Sorted by `Ref`, unique refs, `Ref` starts with `refs/heads/`, `Owner` passes `validActorFingerprint`, `Created` RFC 3339. A missing file means no owners.

5. Load and save functions for both records, with the same strict decoding and atomic 0600 writes as T003. `saveServeBranchOwners` removes the file when the list becomes empty only if you can do so safely; otherwise write an empty list. Pick one behavior and test it.

**Files**: `serve_state.go`.

**Validation**:
- [ ] Defaults match `contracts/server-state.md` byte for byte when marshalled.
- [ ] Invalid names (`..`, `a/b`, `x.git`, 101 characters, leading `.`) are refused.
- [ ] Unknown fields and invalid budgets are refused.

### Subtask T005: Push log append and flock repository locks

**Purpose**: the serialization primitive behind FR-022 and NFR-005, and the informational push log from `data-model.md`.

**Steps**:

1. `acquireServeLock(path string, wait time.Duration) (release func() error, err error)`: open or create the lock file with mode 0600 (`O_RDWR|O_CREATE`, then confirm mode and that it is a regular file, not a symlink, using `Lstat` before open and `Fstat` after), then `syscall.Flock(fd, LOCK_EX|LOCK_NB)` in a loop with a short sleep until `wait` elapses. On timeout return a sentinel error `errServeLockBusy` so WP06 can map it to `repository-busy`. The lock is released by closing the descriptor.

2. Wait budget: the default for pushes is 120 seconds (`contracts/serve-cli.md`). Add `serveLockWait()` returning 120 s, or the value of `HN_TEST_SERVE_LOCK_WAIT` (a Go duration string) when `HN_INTERNAL_TESTING=1`. WP06 uses it.

3. `appendServePushLog(layout, name, entry)`: entry type with fields `time`, `actor`, `key`, `verdict` (`accepted` or `rejected`), `updates` (list of `{ref, old, new}`), `codes` (list). Marshal to one line, open with `O_WRONLY|O_APPEND|O_CREATE` and mode 0600, write the line with its newline in a single `Write`, `Sync`, close. Refuse a symlink or non-regular file. The caller holds the repository lock.

4. Do not add a reader for the push log in production code. Tests read it directly.

**Files**: `serve_state.go`.

**Validation**:
- [ ] Two goroutines contending for one lock: the second times out with `errServeLockBusy` when the wait is short, and succeeds after the first releases.
- [ ] A lock path that is a symlink is refused.
- [ ] Two appends produce two valid JSON lines in order; the file mode is 0600.

### Subtask T006: Hook script generation and validation

**Purpose**: Git runs server-owned hooks through `core.hooksPath`; their content is generated, never hand-edited, and validated on every command (`contracts/server-state.md`).

**Steps**:

1. `serveExecutablePath()`: `os.Executable()` then `filepath.EvalSymlinks`. Refuse a path containing a double quote, backslash, single quote or any whitespace, because WP02 embeds the same path in `authorized_keys` and the hook scripts must not need quoting.

2. `serveHookScript(name, hnPath string) []byte` returns exactly:

   ```sh
   #!/bin/sh
   exec HNPATH serve hook NAME
   ```

   for `NAME` in `pre-receive` and `post-receive`, with a trailing newline.

3. `ensureServeHooks(layout, hnPath)` writes both scripts with mode 0700 atomically (write a temporary file in `hooks/` with mode 0700, `fsync`, rename). `writePrivateFileAtomic` creates 0600 files; either extend locally in `serve_state.go` with a small helper or chmod the temp file before rename. Do not change `identity_keyring.go`.

4. Validation (T002) compares each hook's bytes to `serveHookScript(name, serveExecutablePath())`. Rules:
   - Administration mode: call `ensureServeHooks` first, then validate everything. This follows "regenerated by every administration command" in the contract and lets an administrator repair hooks after upgrading or moving the `hn` binary.
   - Session and hook mode: a missing or differing hook script is a defect and the command refuses. Write a sentence for the guide: after moving or upgrading `hn`, run any `hn serve repo` or `hn serve key` command to regenerate hooks and the `authorized_keys` block.
   - `hooks/` must contain only those two files.

**Files**: `serve_state.go`.

**Validation**:
- [ ] Generated scripts match the expected bytes and are mode 0700.
- [ ] Session-mode validation refuses a modified hook; administration-mode validation repairs it.

### Subtask T007: State unit tests and the serve guide skeleton

**Purpose**: prove every fail-closed rule, and start the guide that FR-023 requires.

**Steps**:

1. `serve_state_test.go`: table-driven tests named `TestServeState...`. Use `t.Setenv("HOME", t.TempDir())` and call the Go functions directly; tests that need `cmdServe` call `run([]string{"serve", ...})` in-process and capture the error. Cover T001 to T006 validation lists. Skip tests that rely on mode checks when `os.Geteuid() == 0`, with `t.Skip("hn serve refuses uid 0; run tests as an unprivileged user")`.

2. Create `docs/serve-v0.md` with this skeleton. Each later WP fills its section; keep the headings exactly so later WPs can find them:

   ```markdown
   # hn serve v0: governed local Git hosting

   ## Overview
   ## Server account and state layout        (WP01)
   ## Key bindings and authorized_keys       (WP02)
   ## Administering hosted repositories      (WP05)
   ## Connecting over SSH                    (WP06)
   ## Push verdicts and ref classes          (WP07)
   ## Hubnot fact refs and budgets           (WP08)
   ## Protected branches                     (WP09)
   ## Recovering a protected branch          (WP09)
   ## Advancing a protected branch           (WP10)
   ## Runners and verifiers                  (WP12)
   ## Verification and traceability          (WP13)
   ## Large files with Git LFS               (WP14)
   ```

   Do not leave the `(WPxx)` annotations in the published headings; put a one-line HTML comment under each empty heading instead, for example `<!-- section owned by WP05 -->`, so later WPs know where to write. Write the Overview (three or four sentences on what `hn serve` is, that it is optional, and that clients of ordinary remotes see no change) and the full "Server account and state layout" section: the dedicated unprivileged user, uid-0 refusal, the layout tree from `contracts/server-state.md`, the modes, what makes the server refuse to serve, the hook regeneration rule from T006 and the `config.json` protected-list ordering rule from T004.

3. Writing style: follow the existing docs voice (plain, direct sentences, descriptive headings). Do not use em dashes. Prefer stating what the server does over contrasting constructions.

**Files**: `serve_state_test.go`, `docs/serve-v0.md`.

**Validation**:
- [ ] `go test -race -run TestServeState ./...` passes.
- [ ] The guide renders and every skeleton heading exists.

## Test Strategy

Unit tests in `serve_state_test.go` call the functions directly with a temporary `HOME`. One in-process test of `run([]string{"serve"...})` covers the dispatcher and uid refusal. Black-box tests through the built binary start in WP05.

## Definition of Done

- [ ] T001 to T007 complete; `gofmt -l .` prints nothing; `go vet ./...`, `go build ./...`, `go test -race ./...` and `git diff --check` pass.
- [ ] **FR-001 / C-001**: no existing command calls into `serve_*.go`; `git grep -n 'cmdServe' -- '*.go'` shows only `main.go` and `serve_commands.go`.
- [ ] **NFR-004**: `hn serve` refuses uid 0 before reading state (test with the override); validation never writes in session mode.
- [ ] **NFR-008**: every defect listed in `contracts/server-state.md` that this WP owns has a refusing test.
- [ ] **FR-003 (storage)**: registry decoding enforces version, sorting, uniqueness, full actor fingerprints and fingerprint shape.
- [ ] **C-003**: no change to `go.mod`.
- [ ] No pre-existing test assertion changed (NFR-007).
- [ ] `docs/serve-v0.md` exists with the skeleton and the completed state layout section.

## Risks

- Overly strict validation could block the administration commands that must repair state. Mitigation: the administration mode in T006 regenerates hooks before validating them, and `ensureServeLayout` runs before validation for administration commands.
- `flock` semantics differ on network filesystems. The guide states that the server home must be on a local filesystem.
- The data model's sorted-versus-first protected list ambiguity is resolved in T004; reviewers should confirm the guide states it.

## Reviewer Guidance

- Confirm the uid check is the first statement in `cmdServe` and that no path is resolved before it.
- Try each defect by hand in a temporary `HOME` if a test seems missing.
- Confirm the hook script bytes and modes, and that session-mode validation is read-only.
- Confirm `main.go` changes are limited to the route and three usage lines.

## Activity Log

- 2026-10-25T00:09:18Z – system – Prompt generated via /spec-kitty.tasks
