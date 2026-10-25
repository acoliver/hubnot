---
work_package_id: WP07
title: Hook entry points and push verdict framework
dependencies:
- WP06
requirement_refs:
- FR-006
- FR-010
- FR-011
- FR-012
- FR-013
- NFR-003
planning_base_branch: mission/hn-serve
merge_target_branch: mission/hn-serve
branch_strategy: Planning artifacts for this mission were generated on mission/hn-serve. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into mission/hn-serve unless the human explicitly redirects the landing branch.
subtasks:
- T041
- T042
- T043
- T044
- T045
- T046
- T047
phase: Phase 3 - Push verdict
history:
- !!omap
  - at: '2026-10-25T00:09:18Z'
  - actor: system
  - action: Prompt generated via /spec-kitty.tasks
agent_profile: implementer-ivan
authoritative_surface: serve_verdict.go
create_intent:
- serve_hooks.go
- serve_verdict.go
- serve_push_test.go
- serve_commands.go
- docs/serve-v0.md
execution_mode: code_change
owned_files:
- serve_hooks.go
- serve_verdict.go
- serve_push_test.go
- serve_commands.go
- docs/serve-v0.md
role: implementer
tags: []
tracker_refs: []
---

# WP07: Hook entry points and push verdict framework

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter, and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `codex`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## Objective

Build the push verdict engine's framework, the deep module of this mission (plan, "Component boundaries"):

1. `hn serve hook pre-receive` and `hn serve hook post-receive`, run by the generated hook scripts inside `git receive-pack`.
2. Parsing of the hook's ref updates and push options, classification of every ref update into the seven classes from `data-model.md`, and an all-or-nothing verdict: exit 0 only when every ref passes, otherwise print one `hn-serve/1` line plus sentence per failing ref and exit 1 so Git rejects every ref and discards quarantined objects (FR-012, FR-013).
3. The rules for tags (create-only), ordinary branches (owner or maintainer for forced updates and deletions, FR-011) and reserved refs (FR-010).
4. `post-receive` bookkeeping: branch owners and the push log (FR-006).

Actor chain, proposal and memory refs (WP08) and protected branches (WP09) get fail-closed placeholder arms here that those WPs replace.

## Context

- Read `contracts/push-verdict.md` (all of it), `data-model.md` ("Branch owner", "Push session, ref update and push verdict", "Push log entry"), `contracts/serve-cli.md` ("Internal hook commands") and the plan's "Domain rules and invariants" rule 3.
- Start with `spec-kitty agent action implement WP07 --agent <name> --mission hn-serve-01M5P0NP`.
- From WP01: layout, `loadServeRepoConfig`, `loadServeBranchOwners`, `saveServeBranchOwners`, `appendServePushLog`, `loadServeRegistry`, `lookupServeKey`.
- From WP03: `serveRejection`, `renderServeRejections`, codes, `servePushOptionAdvance`, `servePushOptionExpected`, `serveMarkerRef`.
- From WP06: the wrapper sets `HN_SERVE_ACTOR`, `HN_SERVE_KEY`, `HN_SERVE_REPOSITORY` and holds the repository lock for the whole `receive-pack` run. **Hooks must never take `RepoLock`**; the wrapper already holds it, and a second `flock` from the hook process would block. Harness: `serveHarnessShim`, `serveHarnessClient`, `serveHarnessCloneURL`.
- From existing code: `loadReplicationPolicyAt(gitDir, acceptedGitDir, commit)` in `quarantine.go` reads and parses `.hn/policy.json` at a commit in a given repository; `actorListed` in `policy.go`; `collectIssueEvents` in `issue_read.go` returns every verified hosted event.
- Git's hook environment: `receive-pack` runs hooks with the working directory at the repository and `GIT_DIR` set (often `.`), plus quarantine variables (`GIT_QUARANTINE_PATH`, `GIT_OBJECT_DIRECTORY`, `GIT_ALTERNATE_OBJECT_DIRECTORIES`) so plain `git` commands see pushed objects. Do not clear those variables when you run Git from the hook.

## Branch Strategy

- **Planning/base branch at prompt creation**: `mission/hn-serve`
- **Final merge target for completed work**: `mission/hn-serve`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. Each computed lane gets exactly one worktree, and sequential WPs in the same lane reuse it.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `serve_hooks.go` | NEW. Hook entry points, input parsing, output, bookkeeping. |
| `serve_verdict.go` | NEW. Classification, verdict context, aggregation, tag, branch and reserved rules, placeholder arms. WP08 and WP09 replace only their arms. |
| `serve_commands.go` | CHANGE. Add `case "hook"` only. |
| `serve_push_test.go` | NEW. |
| `docs/serve-v0.md` | CHANGE. "Push verdicts and ref classes" section only. |

## Subtasks

### Subtask T041: Hook entry checks

**Steps**:

1. `case "hook": return cmdServeHook(layout, args[1:])`; accept exactly `pre-receive` or `post-receive`.
2. Refuse to run (exit 1 with `hn: hn serve hook: <reason>`, which Git relays and which rejects the push) unless:
   - `HN_SERVE_ACTOR`, `HN_SERVE_KEY` and `HN_SERVE_REPOSITORY` are set and well-formed (`validActorFingerprint`, `validServeFingerprint`, `validServeRepositoryName`).
   - `GIT_DIR`, resolved against the working directory and with symlinks evaluated, equals `layout.RepoGitDir(HN_SERVE_REPOSITORY)` with symlinks evaluated.
   - The registry still binds `HN_SERVE_KEY` to `HN_SERVE_ACTOR` (a key removed during a long push is refused).
3. Load the repository configuration once and pass it down.

**Files**: `serve_hooks.go`, `serve_commands.go`.

### Subtask T042: Ref update and push option parsing

**Steps**:

1. Read stdin with a bound (for example 4 MiB and 10,000 lines; exceeding either rejects every ref with `over-budget`, `budget=ref-updates`). Each line is `OLD NEW REF` separated by single spaces. OIDs must be lowercase hex of the repository's object format length (`git rev-parse --show-object-format`: 40 for `sha1`, 64 for `sha256`). The all-zero OID marks creation (`OLD`) or deletion (`NEW`). A malformed line rejects the whole push.
2. Push options: read `GIT_PUSH_OPTION_COUNT` (absent means none) and `GIT_PUSH_OPTION_<n>`. Allowed: `hn-advance=<full sha256: event ID>` (use `validEventID`) and `hn-expected-tip=<full commit ID of the object format length>`, each at most once. Any other option, a repeat or a malformed value rejects every ref update with `unsupported-command` and the sentence `hn-serve: push options other than hn-advance and hn-expected-tip are not supported.`.
3. If `hn-advance` is present, the push must contain exactly one ref update and it must be a protected branch; otherwise reject every ref update with `unsupported-command`. (WP09 implements what the option means.)
4. Produce:

   ```go
   type serveRefUpdate struct{ Old, New, Ref string; Class serveRefClass }
   type servePushOptions struct{ Advance, ExpectedTip string }
   ```

**Files**: `serve_hooks.go`.

### Subtask T043: Ref classification, verdict context and all-or-nothing aggregation

**Purpose**: one boundary between hook input and rejection lines (plan: "callers hand it the hook's ref updates, the mapped actor, push options and repository configuration, and it returns either acceptance or an ordered list of rejections").

**Steps**:

1. Classes, checked in this order:

   | Class | Ref pattern |
   |----|----|
   | `protected` | listed in `config.Protected` |
   | `actor` | `refs/hn/actors/<64 lowercase hex>` |
   | `proposal` | `refs/hn/proposals/<64 lowercase hex>` |
   | `memory` | `refs/hn/memory/<64-hex actor>/<64-hex stream>` (reuse `parseMemoryRef`) |
   | `tag` | `refs/tags/*` |
   | `branch` | other `refs/heads/*` |
   | `reserved` | everything else, including `refs/hn/serve/*`, malformed `refs/hn/*`, `refs/notes/*`, `HEAD` |

2. Entry point:

   ```go
   func evaluateServePush(ctx *serveVerdictContext, updates []serveRefUpdate, options servePushOptions) []serveRejection
   ```

   `serveVerdictContext` holds: `GitDir`, `Actor`, `Key`, `Config`, the branch owners, lazily loaded hosted events (`collectIssueEvents`, loaded at most once per hook run), a slot for facts accepted earlier in this push (WP08 fills it), and a lazily computed set of repository maintainers: actors in `maintainers` of the policy at the tip of `Config.Protected[0]`, read with `loadReplicationPolicyAt(gitDir, gitDir, tip)`. A missing or invalid policy yields an empty set, so no actor counts as a maintainer.

3. Evaluation order: actor refs first (in ref order), then memory, then proposal, then tag, branch and reserved, then protected. Later classes may rely on facts accepted from earlier classes in the same push (contract, "Inputs"). The returned rejections are sorted by ref byte order; `renderServeRejections` (WP03) does the sort.

4. Placeholder arms: for `actor`, `proposal`, `memory` and `protected`, return a rejection with code `reserved-ref` and the sentence `hn-serve: <class> updates are not enabled in this build.`. Put each placeholder in its own small function (`checkServeActorUpdate`, `checkServeProposalUpdate`, `checkServeMemoryUpdate`, `checkServeProtectedUpdate`) in `serve_verdict.go` so WP08 and WP09 replace exactly those functions. Do not write tests that assert placeholder codes.

5. `pre-receive` flow: parse, evaluate, and if any rejection exists, write the rendered lines to stderr, append a `rejected` push log entry (codes in line order), and exit 1. Otherwise exit 0 and write nothing to stderr. Every rejection line carries `actor=<mapped actor>` (FR-006).

**Files**: `serve_verdict.go`, `serve_hooks.go`.

### Subtask T044: Tag, ordinary branch and reserved ref rules

**Steps**:

1. **Tag**: creation is accepted from any mapped actor. Update or deletion: `tag-immutable`, `ids=<old>` (and `<new>` for updates), next `create a new tag`.
2. **Ordinary branch** (FR-011, OQ-6 as tightened):
   - Creation (old zero) and fast-forward updates (`git merge-base --is-ancestor OLD NEW` succeeds) are accepted from any mapped actor.
   - A non-fast-forward update or a deletion is accepted only when the mapped actor equals the recorded owner or is in the maintainer set. A branch with no owner record accepts these only from maintainers.
   - Otherwise `branch-not-owner`, with `ids` naming the owner actor when recorded, and the sentence `hn-serve: REF belongs to OWNER; only its owner or a maintainer can force-update or delete it.` (or `...has no recorded owner; only a maintainer can...`).
3. **Reserved**: `reserved-ref`, next `push branches, tags and Hubnot refs only`. This includes any update of `refs/hn/serve/v1`.

**Files**: `serve_verdict.go`.

### Subtask T045: Branch-owner recording and push log

**Steps**:

1. `post-receive` reads the same `OLD NEW REF` lines (only refs that actually moved). For each ordinary branch: creation adds `{ref, owner: HN_SERVE_ACTOR, created: now UTC RFC 3339}`; deletion removes the record; updates leave it. Save once with WP01's atomic writer. Protected branches never get records.
2. Append one `accepted` push log entry with `time`, `actor`, `key`, `verdict`, `updates` and empty `codes`.
3. `post-receive` never changes the verdict. On any failure print `hn-serve: post-receive bookkeeping failed: <cause>` to stderr and exit 0. A missing owner record makes the branch behave as unowned, which fails closed (plan rule 3).
4. Do not take any lock: the wrapper holds `RepoLock` during the whole session.

**Files**: `serve_hooks.go`.

### Subtask T046: Push framework black-box tests

**Purpose**: prove FR-006, FR-010 (tags, reserved), FR-011, FR-012 and FR-013 through real pushes.

**Steps** (tests `TestServePush...` in `serve_push_test.go`). Use actors A, B and maintainer M as 64-hex values; the policy on `main` lists M as maintainer. Signed Hubnot events are not needed for these classes.

1. **US1-6**: B creates `feature/b` (accepted; `branches.json` records B). A force-pushes `feature/b`: rejected with `branch-not-owner` naming B, branch unchanged. A deletes it: rejected. B force-pushes: accepted. M deletes it: accepted, record removed.
2. **Unowned branch**: a branch imported at `repo create` (from WP05's source fixture) accepts a fast-forward from A, refuses a forced update from A, accepts one from M.
3. **No maintainers when policy is invalid**: replace `main` with a commit whose policy is malformed using `lock-exec ... git update-ref`; M's forced update of an unowned branch is now refused.
4. **Tags**: create accepted; move and delete refused with `tag-immutable`.
5. **Reserved**: pushes to `refs/hn/serve/v1`, `refs/notes/commits`, `refs/hn/other/x` and `refs/hn/actors/short` are refused with `reserved-ref`.
6. **All-or-nothing (FR-012)**: one push with a valid new branch plus a reserved ref, both with and without `--atomic`: whole push rejected, the new branch absent on the server. For every rejected push in this file, snapshot `git for-each-ref` on the hosted repository before and after and require byte-identical output (SC-002).
7. **Quarantine discard**: after the rejected push in step 6, `git --git-dir=<hosted> cat-file -e <new commit>` fails.
8. **Push options**: `-o foo=bar` rejects every ref with `unsupported-command`; `-o hn-advance=<id>` with two refs rejects every ref; `-o hn-expected-tip=zz` rejects.
9. **Hooks cannot be disabled**: set `core.hooksPath=/dev/null` and `receive.fsckObjects=false` in the hosted repository's config with `lock-exec ... git config`; a reserved-ref push is still rejected.
10. **Attribution (FR-006)**: every rejection line parses with `parseServeRejectionLine` and has `actor=<pusher>`; exactly one line per rejected ref; the push log has one entry per push with the right verdict, actor, key, updates and codes.
11. **Key removed mid-session**: start a push from A with an `HN_INTERNAL_TESTING` hook pause if needed, or simulate by invoking `hn serve hook pre-receive` directly with an environment whose key is no longer bound; it refuses.

**Files**: `serve_push_test.go`.

### Subtask T047: Serve guide section on ref classes, branches, tags and the push log

**Steps**:

1. Write "Push verdicts and ref classes" in `docs/serve-v0.md`: all-or-nothing verdicts and quarantine; the class table; evaluation order and that one push may rely on facts it carries; ordinary-branch ownership including unowned branches and the maintainer definition; tags; reserved refs; allowed push options and what happens with others; the rejection line shape with a link to the code table in `docs/protocol-v0.md`; the push log location and fields; a recommendation to use `git push --atomic`.
2. Include one marked example `<!-- example: reject-branch-not-owner -->` showing the two relayed lines for US1-6, and add `TestServePushDocumentedExample` that extracts it, substitutes fixture IDs and compares with the real stderr field for field.
3. Writing style: plain direct sentences, descriptive headings, no em dashes.

**Files**: `docs/serve-v0.md`, `serve_push_test.go`.

## Test Strategy

Black-box pushes through the shim against real hosted repositories, plus unit tests for classification and option parsing. Run `go test -race -run TestServePush ./...`, then the full gate.

## Definition of Done

- [ ] T041 to T047 complete; full gate passes.
- [ ] **FR-006**: every rejection line and push log entry names the mapped actor.
- [ ] **FR-010 (tags, reserved)** and **FR-011**: rules enforced as specified, including unowned branches and the invalid-policy case.
- [ ] **FR-012**: mixed pushes reject entirely; refs byte-identical; quarantined objects absent.
- [ ] **FR-013**: one parseable line plus sentence per rejected ref, in ref order.
- [ ] **NFR-003**: malformed input, unknown push options and invalid default policy fail closed.
- [ ] Hooks never acquire `RepoLock` (code review item).
- [ ] Guide section written; its example is executed by a test.

## Risks

- `collectIssueEvents` must see the hosted repository through `GIT_DIR`; verify early with a debug test that it returns zero events on a fresh hosted repository and does not error.
- Placeholders must stay fail-closed; never accept an actor, proposal, memory or protected update in this WP.

## Reviewer Guidance

- Confirm the evaluation order and the sort of output lines.
- Confirm the push log is written for both verdicts and that `post-receive` cannot reject.
- Run the all-or-nothing test with `--atomic` and without.

## Activity Log

- 2026-10-25T00:09:18Z – system – Prompt generated via /spec-kitty.tasks
