---
work_package_id: WP03
title: Rejection-line protocol, server marker and remote identification
dependencies: []
requirement_refs:
- C-008
- FR-013
- FR-017
planning_base_branch: mission/hn-serve
merge_target_branch: mission/hn-serve
branch_strategy: Planning artifacts for this mission were generated on mission/hn-serve. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into mission/hn-serve unless the human explicitly redirects the landing branch.
subtasks:
- T014
- T015
- T016
- T017
- T018
- T019
phase: Phase 1 - Foundations
history:
- !!omap
  - at: '2026-10-25T00:09:18Z'
  - actor: system
  - action: Prompt generated via /spec-kitty.tasks
agent_profile: implementer-ivan
authoritative_surface: serve_reasons.go
create_intent:
- serve_reasons.go
- serve_wire.go
- serve_reasons_test.go
execution_mode: code_change
owned_files:
- serve_reasons.go
- serve_wire.go
- serve_reasons_test.go
- docs/protocol-v0.md
role: implementer
tags: []
tracker_refs: []
---

# WP03: Rejection-line protocol, server marker and remote identification

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter, and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `codex`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## Objective

Define the wire vocabulary that the server and the client share, in two small files, so every later WP uses one implementation:

- `serve_reasons.go`: the reason-code catalogue, rendering of `hn-serve/1` rejection lines with their plain sentence, a strict parser, and extraction of those lines from `git push` stderr (FR-013).
- `serve_wire.go`: the marker ref `refs/hn/serve/v1` and its blob content, the push option names, and the client check that a remote identifies as `hn serve` (FR-017, research decision `01M5R5CRYGCW3H1SFFRDZ42G11`).
- `docs/protocol-v0.md`: a new section documenting all of it, including the full reason-code table (FR-013 requires documented codes; C-008 requires protocol docs to move with the code).

This WP has no dependencies and runs in its own lane, in parallel with WP01, WP02 and WP04.

## Context

- Read `contracts/push-verdict.md` ("Reason-code catalogue", "Line format", "Worked examples"), `contracts/all-trusted-selection.md` ("Server identification"), `contracts/advancement.md` (step 2 and step 6) and `contracts/lfs-transfer.md` (the `budget=lfs-quota` fields).
- Start with `spec-kitty agent action implement WP03 --agent <name> --mission hn-serve-01M5P0NP`.
- Git helpers live in `git.go` (`gitText`, `gitOutput`, `gitOutputAt`). Look at how they pass stdin before you add a new one; if none accepts stdin, run `exec.Command("git", "hash-object", "--stdin")` locally in `serve_wire.go` with the same environment handling the helpers use.
- `safeDiagnostic` in `replication.go` and `safeText` in `commands.go` already sanitize output; read them, but implement the stricter `hn-serve/1` sanitization rule from the contract.

## Branch Strategy

- **Planning/base branch at prompt creation**: `mission/hn-serve`
- **Final merge target for completed work**: `mission/hn-serve`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. Each computed lane gets exactly one worktree.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `serve_reasons.go` | NEW. Catalogue, renderer, parser, stderr extraction. |
| `serve_wire.go` | NEW. Marker constants, push option names, remote identification. |
| `serve_reasons_test.go` | NEW. Tests for both files. |
| `docs/protocol-v0.md` | CHANGE. New section "hn serve wire surface". Do not edit other sections. |

## Subtasks

### Subtask T014: Reason-code catalogue

**Purpose**: stable, documented codes (FR-013, OQ-2 plus the two codes added by research decision `01M5R5D36533BCY8APVZ0F3PJJ`).

**Steps**:

1. Declare one constant per code and a catalogue entry with the meaning and default next action from the `contracts/push-verdict.md` table. The full list, in catalogue order:

   `unknown-key`, `unhosted-repository`, `unsupported-command`, `repository-busy`, `actor-key-mismatch`, `actor-chain-invalid`, `actor-chain-rewrite`, `dependency-missing`, `protected-no-proposal`, `protected-not-ready`, `protected-destination-policy`, `protected-not-fast-forward`, `protected-stale-tip`, `protected-blocked`, `protected-policy-invalid`, `protected-deletion`, `advance-not-maintainer`, `proposal-ref-mismatch`, `proposal-ref-immutable`, `memory-key-mismatch`, `memory-invalid`, `reserved-ref`, `tag-immutable`, `branch-not-owner`, `over-budget`, `lfs-quota`, `lfs-object-mismatch`.

2. Provide `serveReasonKnown(code string) bool` and `serveReasonDefaultNext(code string) string`. Default next actions are templates; callers substitute IDs (for example `open a proposal for <commit>`), so expose them as plain strings and let callers format.

3. Evidence kinds for `missing=`: `proposal`, `review`, `ci`, `acceptance`, always emitted in that order regardless of input order.

**Files**: `serve_reasons.go`.

### Subtask T015: `hn-serve/1` rejection line rendering and sanitization

**Purpose**: one renderer so every rejection on every path is identical in shape (FR-013, SC-002).

**Steps**:

1. Type:

   ```go
   type serveRejection struct {
       Ref      string   // full ref name, or "-" for session refusals
       Actor    string   // full 64-hex actor, or "-" when the key is unknown
       Code     string
       IDs      []string // full sha256: event IDs and full commit IDs
       Missing  []string // subset of proposal, review, ci, acceptance
       Budget   string   // only with over-budget and lfs-quota
       Limit    int64
       Measured int64
       Next     string
       Sentence string
   }
   ```

2. `renderServeRejection(r serveRejection) string` returns exactly two lines, each ending in `\n`:

   ```text
   hn-serve/1 reject ref=REF actor=ACTOR code=CODE [ids=ID[,ID]...] [missing=KIND[,KIND]...] [budget=NAME limit=N measured=N] next="NEXT"
   hn-serve: SENTENCE
   ```

   Rules from the contract: fields in that order, single spaces, optional fields omitted when empty, `budget`/`limit`/`measured` only for `over-budget` and `lfs-quota` (refuse rendering budget fields for other codes; return an error or panic in a way tests catch, your choice, but document it). `NEXT` and every other value: printable ASCII only; replace control characters and non-ASCII bytes with `?`; additionally replace `"` and `\` in `NEXT` with `?`, and replace spaces, `=` and `,` inside `REF`, `ACTOR`, `CODE`, `ids` and `missing` values with `?` so the line stays parseable. The sentence is one line with the same control and non-ASCII replacement.

3. `renderServeRejections(list []serveRejection) string` sorts by `Ref` byte order (stable for equal refs) and concatenates. The hook in WP07 uses this.

4. An unknown code is a programming error: refuse to render it so a typo cannot reach clients.

**Files**: `serve_reasons.go`.

### Subtask T016: Strict line parser and client stderr extraction

**Purpose**: clients (WP10, tests in every WP) must read rejection lines reliably from `git push` stderr.

**Steps**:

1. `parseServeRejectionLine(line string) (serveRejection, error)`: accept a line only when it starts with `hn-serve/1 reject ` and every following field matches the grammar in order. Unknown fields, out-of-order fields, duplicate fields, an unknown code, an unknown evidence kind, a non-decimal limit, a missing `next="..."` or text after the closing quote are errors. Sentence lines are not parsed by this function.

2. `extractServeRejections(stderr string) ([]serveRejection, []string)`: walk stderr lines; for each line, strip one leading `remote: ` prefix if present and trailing spaces and `\r` (Git pads relayed hook output; see `contracts/advancement.md` step 6 and the plan's IC-05 risk). Collect parsed rejections, and pair each with the following `hn-serve: ` sentence line when present. Return the rejections plus the cleaned original `hn-serve/1` and `hn-serve:` lines in order so callers can print them verbatim.

3. Ignore Git's own lines (`! [remote rejected] ...`, `To ...`, `error: failed to push ...`).

**Files**: `serve_reasons.go`.

### Subtask T017: Server marker constants, push option names and remote identification

**Purpose**: let a client tell an `hn serve` remote from an ordinary one without trusting it for anything else (FR-017, C-006).

**Steps**:

1. Constants in `serve_wire.go`:

   ```go
   const (
       serveMarkerRef          = "refs/hn/serve/v1"
       serveMarkerContent      = "hn-serve/1\n" // 11 bytes
       servePushOptionAdvance  = "hn-advance"
       servePushOptionExpected = "hn-expected-tip"
   )
   ```

2. `serveMarkerObjectID(gitDir string) (string, error)`: compute the blob ID of `serveMarkerContent` in the repository's object format with `git --git-dir=GITDIR hash-object --stdin` (no `-w`). For the server, WP05 writes the blob with `-w` and points the ref at it.

3. `serveRemoteIdentifies(remote string) (bool, string)`: run `git ls-remote --refs REMOTE refs/hn/serve/v1` in the current repository. Return true only when the output is exactly one line `OID\trefs/hn/serve/v1` and `OID` equals `serveMarkerObjectID` for the local repository. Any transport error, extra line, different ref or different OID returns false plus a short diagnostic for the caller's message. Never return an error that a caller could mistake for success. Apply the same environment and timeout behavior as other remote-touching Git calls in the codebase; check `quarantine.go`'s `ls-remote` usage in `resolveReplicationRequests` and follow it.

4. Document in a comment that identification gates client modes only and grants no trust; every imported ref is still validated (NFR-006).

**Files**: `serve_wire.go`.

### Subtask T018: Protocol unit tests

**Purpose**: lock the format before three other lanes depend on it.

**Steps** (tests named `TestServeReasons...` and `TestServeWire...` in `serve_reasons_test.go`):

1. Every catalogue code renders and parses back to an equal struct (round trip), with and without IDs, missing kinds and budget fields.
2. Golden test: render the `reject-direct-main` worked example from `contracts/push-verdict.md` (with its illustrative IDs) and compare byte for byte with the contract text minus the `remote: ` prefix. Keep this golden string in the test; WP09 compares the real hook output against the same shape with fixture IDs.
3. Sanitization: control bytes, UTF-8, quotes and backslashes in `Next`, spaces and commas in refs are replaced as specified, and the result still parses.
4. Parser rejects: wrong version prefix, unknown code, out-of-order fields, duplicate field, budget fields on a non-budget code, trailing text, unterminated quote.
5. `extractServeRejections` handles real Git stderr shapes: `remote: ` prefixes with trailing padding, CRLF, interleaved Git lines, two rejected refs.
6. Identification: build three temporary bare repositories with `git init --bare`: one with the correct marker blob at `refs/hn/serve/v1`, one with a different blob at that ref, and one without the ref. Use a client repository with each as a remote. Only the first identifies. A nonexistent remote path returns false. Repeat the positive case with `git init --object-format=sha256` for both client and remote to cover the object-format rule.

**Files**: `serve_reasons_test.go`.

### Subtask T019: `docs/protocol-v0.md` serve wire section

**Purpose**: C-008 and FR-013: protocol notes move with the wire change.

**Steps**: add a section titled "hn serve wire surface" before "Known limits". Cover:

1. The marker ref, its exact 11-byte blob content, how a client identifies a remote, and that identification grants no trust.
2. The two push options, their value formats (full `sha256:` event ID; full commit ID), the at-most-once rule, and the single-ref rule for `hn-advance` (enforced by WP07 and WP09).
3. The `hn-serve/1` line grammar, field order, sanitization, ordering by ref, the sentence line and the versioning rule (a new field requires a new version prefix).
4. The full reason-code table (code, meaning, typical next action), copied from the contract and kept in sync with `serve_reasons.go`. Add a test in `serve_reasons_test.go` that reads `docs/protocol-v0.md`, extracts the code column of that table and asserts it equals the catalogue exactly, so the docs cannot drift.
5. The `budget=`, `limit=` and `measured=` fields and the codes that carry them.

Writing style: plain direct sentences, descriptive headings, no em dashes.

**Files**: `docs/protocol-v0.md`, `serve_reasons_test.go`.

## Test Strategy

Unit tests only, plus temporary bare repositories for identification. Run with `go test -race -run 'TestServeReasons|TestServeWire' ./...`, then the full gate.

## Definition of Done

- [ ] T014 to T019 complete; full gate passes.
- [ ] **FR-013**: every code renders a line in the documented format; the parser accepts exactly the renderer's output; the docs table equals the catalogue (test-enforced).
- [ ] **FR-017 (identification)**: `serveRemoteIdentifies` is true only for the exact marker blob in the local object format, for SHA-1 and SHA-256 repositories.
- [ ] **C-008**: `docs/protocol-v0.md` documents the marker, push options, line grammar and codes.
- [ ] No pre-existing test assertion changed.

## Risks

- Downstream WPs depend on these names. Keep exported-within-package names stable once merged; if a rename is unavoidable, record it in the Activity Log.
- `ls-remote` against an unreachable remote may hang without a timeout; follow the existing remote-call pattern.

## Reviewer Guidance

- Render and parse a few lines by hand with odd input.
- Confirm the docs-table test fails when a code is added to the catalogue but not to the docs.
- Confirm identification never returns true on a transport error.

## Activity Log

- 2026-10-25T00:09:18Z – system – Prompt generated via /spec-kitty.tasks
