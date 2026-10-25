---
work_package_id: WP06
title: Forced-command session wrapper and SSH test harness
dependencies:
- WP05
requirement_refs:
- C-002
- C-009
- FR-005
- FR-006
- FR-022
- NFR-003
- NFR-004
planning_base_branch: mission/hn-serve
merge_target_branch: mission/hn-serve
branch_strategy: Planning artifacts for this mission were generated on mission/hn-serve. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into mission/hn-serve unless the human explicitly redirects the landing branch.
subtasks:
- T034
- T035
- T036
- T037
- T038
- T039
- T040
phase: Phase 2 - Hosting and sessions
history:
- !!omap
  - at: '2026-10-25T00:09:18Z'
  - actor: system
  - action: Prompt generated via /spec-kitty.tasks
agent_profile: implementer-ivan
authoritative_surface: serve_session.go
create_intent:
- serve_session.go
- serve_session_test.go
- serve_harness_test.go
- serve_commands.go
- docs/serve-v0.md
execution_mode: code_change
owned_files:
- serve_session.go
- serve_session_test.go
- serve_harness_test.go
- serve_commands.go
- docs/serve-v0.md
- docs/threat-model.md
role: implementer
tags: []
tracker_refs: []
---

# WP06: Forced-command session wrapper and SSH test harness

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter, and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `codex`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## Objective

Implement `hn serve git --key FINGERPRINT`, the command every managed `authorized_keys` entry forces. It must:

1. Accept only Git fetch (`git-upload-pack`) and Git push (`git-receive-pack`) for one hosted repository, plus the grammar for `git-lfs-transfer`, which it refuses until WP14 enables it (FR-005, C-010).
2. Refuse unknown keys, unsupported commands and unsafe or unhosted paths with one `hn-serve/1` line before any repository data moves (NFR-003).
3. Attribute the session to the mapped actor and pass it to hooks through an environment that it builds from scratch, so a client cannot forge it (FR-006, HN-003).
4. Hold the repository lock for the whole `receive-pack` run, with a 120-second wait budget and `repository-busy` refusal (FR-022, NFR-005). Fetches never take the lock.
5. Start Git with server-controlled configuration that the hosted repository's own `config` cannot override.

Extend the test harness with the SSH command shim (OQ-9, C-009), and document connecting over SSH and the server trust boundary.

## Context

- Read `contracts/serve-cli.md` ("Forced command", "Execution"), `research.md` ("Session dispatch grammar and environment", "Test harness", "Push enforcement point and serialization", and the spike table) and the plan's IC-02 risks.
- Start with `spec-kitty agent action implement WP06 --agent <name> --mission hn-serve-01M5P0NP`.
- From WP01: `serveLayout`, `loadServeRegistry`, `lookupServeKey`, `validServeFingerprint`, `validServeRepositoryName`, `acquireServeLock`, `serveLockWait`, `errServeLockBusy`.
- From WP03: `serveRejection`, `renderServeRejection`, the reason codes.
- From WP05: `serveHarness`, `serveHarnessSourceRepo`, `serveHarnessAddKey`, `hn serve repo lock-exec`.
- Spike facts (research): push invokes the shim as `HOST "git-receive-pack '/r.git'"` with no `GIT_PROTOCOL`; fetch invokes it as `-o SendEnv=GIT_PROTOCOL HOST "git-upload-pack 'r.git'"` with `GIT_PROTOCOL=version=2`. The grammar accepts an optional leading slash.
- Until WP07 lands, `receive-pack` runs the generated `pre-receive` script, which calls `hn serve hook pre-receive`, an unknown subcommand at this point. Every push therefore fails closed. Do not write tests that depend on a push succeeding; WP07 adds them.

## Branch Strategy

- **Planning/base branch at prompt creation**: `mission/hn-serve`
- **Final merge target for completed work**: `mission/hn-serve`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. Each computed lane gets exactly one worktree, and sequential WPs in the same lane reuse it.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `serve_session.go` | NEW. Grammar, checks, environment, lock, exec. WP14 later replaces only the LFS refusal arm. |
| `serve_commands.go` | CHANGE. Add `case "git"` only. |
| `serve_harness_test.go` | CHANGE. Add shim and client helpers; do not change WP05 helpers' behavior. |
| `serve_session_test.go` | NEW. |
| `docs/serve-v0.md` | CHANGE. "Connecting over SSH" section only. |
| `docs/threat-model.md` | CHANGE. New section on the `hn serve` trust boundary. |

## Subtasks

### Subtask T034: `SSH_ORIGINAL_COMMAND` grammar parser

**Purpose**: a closed grammar with no shell removes the injection cases in the spec's edge list.

**Steps**:

1. Implement `parseServeRequest(command string) (serveRequest, error)`:

   ```go
   type serveRequest struct {
       Service string // "upload-pack", "receive-pack" or "lfs-transfer"
       Repo    string // repository name without leading "/" or trailing ".git"
       LFSOp   string // "upload" or "download" for lfs-transfer
   }
   ```

2. Grammar (from the contract), with `SP` exactly one space and no other whitespace anywhere:

   ```text
   request     = git-command SP path / lfs-command SP path SP lfs-op
   git-command = "git-upload-pack" / "git-receive-pack" / "git upload-pack" / "git receive-pack"
   lfs-command = "git-lfs-transfer"
   lfs-op      = "upload" / "download"
   path        = "'" name-path "'" / name-path
   name-path   = ["/"] name [".git"]
   name        = ALNUM *99(ALNUM / "." / "_" / "-")   ; never contains ".."
   ```

   Parse by explicit prefix matching and byte scanning. Do not use `strings.Fields` (it accepts tabs and repeated spaces) or a shell-word splitter.
3. Refuse with a typed error mapped to `unsupported-command`: empty input; any byte outside printable ASCII; double quotes, backslashes, `$`, backticks, `;`, `|`, `&`, `<`, `>`, newline, tab; an unmatched single quote; extra arguments; any path with more than one segment; `git-lfs-authenticate`; `git-upload-archive`; `scp`, `sftp`, shells.
4. A `name` that fails `validServeRepositoryName` after stripping `/` and `.git` is `unsupported-command` (grammar failure). A well-formed name that is not hosted is decided in T035 as `unhosted-repository`.

**Files**: `serve_session.go`.

**Validation**: table test with every accepted form (quoted, unquoted, with and without slash and `.git`, both git-command spellings, both LFS ops) and every refusal above.

### Subtask T035: `hn serve git --key` checks and refusal lines

**Purpose**: FR-005 and NFR-003: refuse in a fixed order, print one structured line, change nothing.

**Steps**:

1. Add `case "git": return cmdServeGit(layout, args[1:])` in `serve_commands.go`. Parse exactly `--key FINGERPRINT`; anything else is a usage error.
2. Checks in this order; the first failure prints one `hn-serve/1` line with `ref=-` and its sentence to stderr and exits 1, without starting Git and without writing anything:

   | Check | Code | `actor` field |
   |----|----|----|
   | `--key` is a valid fingerprint bound in the registry | `unknown-key` | `-` |
   | `SSH_ORIGINAL_COMMAND` is set and parses (T034) | `unsupported-command` | mapped actor |
   | Repository is registered (`state/repos/NAME/config.json` loads) and `repositories/NAME.git` is a real directory (`Lstat`, not a symlink) | `unhosted-repository` | mapped actor |
   | For `receive-pack`, the lock is acquired within `serveLockWait()` | `repository-busy` | mapped actor |

3. Layout validation already ran in `cmdServe` in session mode (WP01). A layout defect, including a symlinked repository directory, refuses there with a state-defect message; that is also fail-closed and acceptable.
4. Stdout must stay empty on every refusal: Git reads stdout as protocol data.
5. Sentences, for example: `hn-serve: this SSH key is not registered with the server.`; `hn-serve: only git fetch, git push and Git LFS transfer are supported.`; `hn-serve: repository NAME is not hosted here.`; `hn-serve: another push held NAME for more than 120 seconds; retry.`

**Files**: `serve_session.go`, `serve_commands.go`.

### Subtask T036: Environment built from scratch and Git execution

**Purpose**: the hook's `HN_SERVE_ACTOR` can come only from the wrapper, and repository configuration cannot disable checks.

**Steps**:

1. `buildServeGitEnvironment(parent []string, layout serveLayout, binding serveKeyBinding, repo string) []string` returns only:
   - `PATH` copied from the parent (set by `sshd`; a client cannot override it under `restrict` and the default `PermitUserEnvironment no`).
   - `HOME=<layout.Home>`, `LANG=C`, `GIT_CONFIG_NOSYSTEM=1`, `TMPDIR=<layout.Tmp>`.
   - `GIT_PROTOCOL` only when the parent value matches `version=[0-9]` exactly.
   - `HN_SERVE_ACTOR=<binding.Actor>`, `HN_SERVE_KEY=<binding.Fingerprint>`, `HN_SERVE_REPOSITORY=<name>`.
   - When the parent has `HN_INTERNAL_TESTING=1`, also copy `HN_INTERNAL_TESTING` and any `HN_TEST_SERVE_*` variables, so later WPs can inject test-only behavior into hooks. Nothing else is copied. Document this exception in a comment.
2. Exec forms (run as a child with `exec.Command`, stdin, stdout and stderr connected to the wrapper's, no shell):

   ```text
   git upload-pack --strict REPO_DIR
   git -c core.hooksPath=<layout.Hooks> -c receive.advertisePushOptions=true -c receive.fsckObjects=true receive-pack REPO_DIR
   ```

   `REPO_DIR` is the absolute `repositories/NAME.git`. Return Git's exit status as the wrapper's exit status (use `os.Exit` after cleanup, or a small error type mapped in `cmdServeGit`; `main.go` is not owned here).
3. Never pass `--key` or any client-provided string to Git other than the validated repository path.

**Files**: `serve_session.go`.

**Validation**: unit test of `buildServeGitEnvironment` with a parent containing `HN_SERVE_ACTOR=evil`, `GIT_DIR=/tmp/x`, `GIT_CONFIG_GLOBAL=...`, `LD_PRELOAD=...`, `GIT_PROTOCOL=version=2:evil` and `GIT_PROTOCOL=version=2`; only the allowed values survive.

### Subtask T037: Push lock with wait budget, fetch without lock, LFS refusal seam

**Purpose**: FR-022 serialization and the IC-07 separability seam.

**Steps**:

1. For `receive-pack`: `release, err := acquireServeLock(layout.RepoLock(name), serveLockWait())` before starting Git; map `errServeLockBusy` to `repository-busy`; hold the lock until Git exits; then release. The lock descriptor is opened close-on-exec (Go's default), so Git and the hooks do not inherit it.
2. Hooks run while the wrapper holds the lock. Record in a comment, and in the Activity Log for WP07, that hook code must never try to take `RepoLock` (a second `flock` from another process would block until timeout). Hooks rely on the wrapper's lock.
3. For `upload-pack`: no lock.
4. For `lfs-transfer`: after the key and repository checks pass, refuse with `unsupported-command` and the sentence `hn-serve: Git LFS transfer is not enabled on this server.`. Put this in one clearly marked function or `case` arm, for example `serveLFSSession(...)`, so WP14 can replace that arm and nothing else.

**Files**: `serve_session.go`.

### Subtask T038: SSH command shim and client helpers in the harness

**Purpose**: real `git` clients reach the server through the exact forced command written in `authorized_keys` (OQ-9, C-009).

**Steps**:

1. `serveHarnessShim(t, h *serveHarness, key serveTestKey, extraEnv map[string]string) string` writes an executable POSIX `sh` script in a temp directory that:
   - Skips `ssh` options Git may pass (`-o VALUE`, `-p PORT`, `-l USER`, `-4`, `-6`, `-T`, `-i FILE`, `-F FILE`), takes the next argument as the host, and requires exactly one more argument, the remote command.
   - Finds the line of `<h.Home>/.ssh/authorized_keys` that contains the key's canonical form (`ssh-ed25519 BASE64`). If none exists, print `Permission denied (publickey).` to stderr and exit 255, the way `sshd` refuses an unknown key.
   - Extracts the `command="..."` value from that line and runs it the way `sshd` does: `env -i HOME=<h.Home> PATH=<PATH> SSH_ORIGINAL_COMMAND="$cmd" [GIT_PROTOCOL="$GIT_PROTOCOL" if set] [extraEnv...] /bin/sh -c "$forced"`.
   - Requires the line to start with `restrict,command=` and exits 255 otherwise, so the harness also checks the entry format.
2. `serveHarnessClient(t, h, key, extraEnv) serveHarnessClientEnv` returns an environment for client `git` and `hn` commands: a fresh client `HOME`, `GIT_SSH_COMMAND=<shim>`, `GIT_SSH_VARIANT=ssh`, `GIT_CONFIG_NOSYSTEM=1`, `GIT_TERMINAL_PROMPT=0`, user name and email configured, `PATH`. Add `Git(dir, args...)`, `GitFailure(dir, args...)`, `Hn(dir, args...)`, `HnFailure(dir, args...)` methods returning combined output.
3. `serveHarnessCloneURL(name string) string` returns `ssh://hn-serve@localhost/NAME.git`.
4. Keep helper names prefixed with `serveHarness`.

**Files**: `serve_harness_test.go`.

### Subtask T039: Session black-box tests

**Purpose**: prove US2 acceptance scenarios 1 to 5 and the session edge cases through real Git.

**Steps** (tests `TestServeSession...` in `serve_session_test.go`):

1. **US2-1**: create a repository and bind key A; `git clone` through the shim succeeds; `git fetch` after a server-side `lock-exec ... git update-ref` change fetches it; both protocol v0 and v2 (`-c protocol.version=0` and `=2`).
2. **US2-2**: run `hn serve git --key <valid but unbound fingerprint>` directly with `SSH_ORIGINAL_COMMAND="git-upload-pack 'r.git'"`: exit 1, stdout empty, stderr has one `unknown-key` line that parses with `parseServeRejectionLine`. A shim for an unregistered key fails clone with exit 255.
3. **US2-3**: with a registered key, each of: no `SSH_ORIGINAL_COMMAND`, `sh`, `/bin/sh -c id`, `scp -t /tmp`, `git-upload-archive 'r.git'`, `git-lfs-authenticate r.git download`, `git-upload-pack 'r.git' extra`, `git-upload-pack 'r.git';id`, `git-upload-pack "r.git"`, `git-upload-pack 'r.git'\nid`, a tab separator, `$(id)` and a backtick form. Each: exit 1, stdout empty, one `unsupported-command` line, refs and files of every hosted repository unchanged (snapshot `git for-each-ref` and a recursive listing before and after).
4. **US2-4**: `git-upload-pack '../r.git'`, `'/a/r.git'`, `'a/r.git'` (unsupported-command); `'missing.git'` (unhosted-repository); a symlink placed at `repositories/link.git` with matching state (refused with zero changes, either `unhosted-repository` or the state-defect message).
5. **US2-5**: after `hn serve key remove`, the shim refuses with exit 255, and a direct `hn serve git --key <removed>` refuses with `unknown-key`.
6. **LFS seam**: put this case in its own test function, `TestServeSessionLFSSeamRefusedWhenDisabled`, guarded by a package-level test constant `serveLFSEnabled` defined in `serve_session.go` next to the refusal arm (`const serveLFSEnabled = false`). While it is false, `git-lfs-transfer 'r.git' download` is refused with `unsupported-command` and the not-enabled sentence. When it is true, the test skips with the reason "LFS transfer enabled; covered by TestServeLFS...". This keeps the refusal test meaningful if LFS is deferred, and lets WP14 enable LFS by changing the constant and the arm without editing this test file.
7. **Lock**: start `hn serve repo lock-exec r -- sleep 3` in the background; a `git push` through a shim created with `extraEnv` `HN_INTERNAL_TESTING=1` and `HN_TEST_SERVE_LOCK_WAIT=300ms` fails with one `repository-busy` line in stderr; a `git clone` at the same time succeeds (fetch does not lock).
8. **Environment**: the T036 unit test, plus an end-to-end check that the wrapper works under the shim's `env -i` (implied by 1).

**Files**: `serve_session_test.go`.

### Subtask T040: Serve guide connection section and threat-model trust boundary

**Steps**:

1. "Connecting over SSH" in `docs/serve-v0.md`: OpenSSH requirements (public-key login for the server user; with the default `StrictModes yes`, home and `~/.ssh` not writable by group or others; keep the default `PermitUserEnvironment no`), the clone URL form, the requests accepted, minimum server Git 2.11 (quarantine and push options, research), the refusal codes `unknown-key`, `unsupported-command`, `unhosted-repository` and `repository-busy` with what to do, and that fetches never wait for pushes. Link to the reason-code table in `docs/protocol-v0.md`.
2. `docs/threat-model.md`: add a section "hn serve trust boundary" covering: OpenSSH authenticates and `restrict` disables forwarding, PTY and `~/.ssh/rc` (cite `sshd(8)` AUTHORIZED_KEYS FILE FORMAT as research does); the closed command grammar with no shell; the environment rebuilt from scratch so a client cannot set the mapped actor; hooks and checks passed with `-c` so hosted repository configuration cannot disable them; the key registry grants attribution and transport access only and never a policy role (C-006); uid-0 refusal and the write locations (NFR-004); the lock and its wait budget. WP13 later adds verdict and client-side trust content; leave a short closing sentence that points to the serve guide for push verdict rules.
3. Writing style: plain direct sentences, descriptive headings, no em dashes.

**Files**: `docs/serve-v0.md`, `docs/threat-model.md`.

## Test Strategy

Black-box tests through the shim with real `git`; unit tests for the grammar and the environment builder. Run `go test -race -run TestServeSession ./...`, then the full gate.

## Definition of Done

- [ ] T034 to T040 complete; full gate passes.
- [ ] **FR-005**: only fetch and push are executed; every other request and every unsafe path is refused with zero repository changes; LFS is parsed and refused pending WP14.
- [ ] **FR-006**: the mapped actor and key reach Git's environment only from the registry (unit-tested), ready for hooks in WP07.
- [ ] **FR-022 / NFR-005 (primitive)**: pushes hold the repository lock for the whole `receive-pack` run; contention beyond the wait budget refuses with `repository-busy`; fetches never lock.
- [ ] **NFR-003**: every refusal fixture ends with exit 1, empty stdout, one parseable line and zero changes.
- [ ] **C-002 / C-009**: the harness runs the exact `command=` value from the generated `authorized_keys` under `env -i`.
- [ ] Guide section and threat-model section written.

## Risks

- A wrapper bug that writes to stdout before refusing corrupts the Git protocol; every refusal test asserts empty stdout.
- The shim must mimic `sshd`'s `sh -c` execution of the forced command; quoting in the shim is the most fragile part, so test it with a path that contains no special characters (WP01 refuses such paths anyway).

## Reviewer Guidance

- Read `buildServeGitEnvironment` and confirm nothing outside the list can pass.
- Confirm the LFS refusal is isolated in one arm.
- Confirm no hook or repository config is written by this WP.

## Activity Log

- 2026-10-25T00:09:18Z – system – Prompt generated via /spec-kitty.tasks
