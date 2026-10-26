---
work_package_id: WP02
title: SSH public keys and the managed authorized_keys block
dependencies:
- WP01
requirement_refs:
- C-002
- C-003
- FR-003
- FR-004
planning_base_branch: mission/hn-serve
merge_target_branch: mission/hn-serve
branch_strategy: Planning artifacts for this mission were generated on mission/hn-serve. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into mission/hn-serve unless the human explicitly redirects the landing branch.
subtasks:
- T008
- T009
- T010
- T011
- T012
- T013
phase: Phase 1 - Foundations
history:
- !!omap
  - at: '2026-10-25T00:09:18Z'
  - actor: system
  - action: Prompt generated via /spec-kitty.tasks
agent_profile: implementer-ivan
authoritative_surface: serve_keys.go
create_intent:
- serve_keys.go
- serve_keys_test.go
- docs/serve-v0.md
execution_mode: code_change
owned_files:
- serve_keys.go
- serve_keys_test.go
- docs/serve-v0.md
role: implementer
tags: []
tracker_refs: []
---

# WP02: SSH public keys and the managed authorized_keys block

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter, and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `codex`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## Objective

Implement the SSH-facing primitives that let `hn serve` rely on OpenSSH instead of implementing SSH (C-002):

1. Parse one OpenSSH public key line and compute its `SHA256:` fingerprint exactly as `ssh-keygen -l -E sha256` prints it (FR-003).
2. Render the managed `authorized_keys` block whose entries carry `restrict` and the forced command `ABS_HN serve git --key FINGERPRINT` (FR-004).
3. Merge that block into an existing `authorized_keys` file while preserving every line outside it byte for byte, and write the result safely.
4. Document key bindings and the managed block in `docs/serve-v0.md`.

Everything here is a library used by WP05 (`hn serve key ...`) and by the WP05 and WP06 test harness. No command is added.

## Context

- Read `contracts/server-state.md` ("registry.json", "Managed authorized_keys block") and `data-model.md` ("Key registry and key binding").
- Start with `spec-kitty agent action implement WP02 --agent <name> --mission hn-serve-01M5P0NP`.
- WP01 provides `serveLayout` (with `SSHDir` and `AuthorizedKeys`), `serveExecutablePath()` and the guide skeleton. Use them.
- Standard library only: `crypto/sha256`, `encoding/base64`, `encoding/binary`, `bytes`, `strings`. Do not import `golang.org/x/crypto/ssh` (C-003).
- OpenSSH wire format for public key blobs (RFC 4253 section 6.6): a sequence of `uint32` big-endian length-prefixed strings; the first string is the key type name.

## Branch Strategy

- **Planning/base branch at prompt creation**: `mission/hn-serve`
- **Final merge target for completed work**: `mission/hn-serve`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. Each computed lane gets exactly one worktree, and sequential WPs in the same lane reuse it.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `serve_keys.go` | NEW. Parsing, fingerprints, block rendering, merge, safe write. |
| `serve_keys_test.go` | NEW. Unit tests and the shared test-key helper `newServeTestKey`. |
| `docs/serve-v0.md` | CHANGE. Write only the "Key bindings and authorized_keys" section. |

## Subtasks

### Subtask T008: OpenSSH public key parsing

**Purpose**: accept exactly one well-formed public key per binding and refuse everything else before it reaches the registry.

**Steps**:

1. `parseServePublicKey(line string) (serveParsedKey, error)` where:

   ```go
   type serveParsedKey struct {
       Type        string // e.g. "ssh-ed25519"
       Blob        []byte // decoded key blob
       Fingerprint string // "SHA256:..." (T009)
       Canonical   string // "TYPE BASE64" with no options and no comment
   }
   ```

2. Input handling:
   - Trim one trailing `\n` or `\r\n`. Refuse any other control byte, an empty line and a line longer than 16 KiB.
   - The line may start with an options field (as found in an `authorized_keys` line). Detect it: if the first whitespace-separated token is not a supported key type, treat it as options and skip it, honouring double-quoted sections that can contain spaces (OpenSSH allows `command="a b"`). If after skipping one options token the next token is still not a supported type, refuse.
   - The key type token is followed by one base64 token. Anything after that is the comment and is ignored.
   - Supported types (from `data-model.md`): `ssh-ed25519`, `sk-ssh-ed25519@openssh.com`, `ecdsa-sha2-nistp256`, `ecdsa-sha2-nistp384`, `ecdsa-sha2-nistp521`, `sk-ecdsa-sha2-nistp256@openssh.com`, `ssh-rsa`. Any other type is refused with a message naming it.

3. Blob checks: decode with standard base64 (padding required, as OpenSSH writes it); refuse decoding errors. Read the first length-prefixed string from the blob and require it to equal the type token. Walk the remaining length-prefixed strings to confirm the blob is fully consumed with no trailing bytes and no length that overruns the buffer. Do not interpret key material beyond that.

4. Return `Canonical` as `Type + " " + base64.StdEncoding.EncodeToString(Blob)`.

**Files**: `serve_keys.go`.

**Validation**:
- [ ] Keys with and without comments, and with an options prefix containing a quoted space, parse to the same canonical form.
- [ ] Type mismatch between token and blob, truncated blob, trailing bytes, unsupported type (`ssh-dss`), bad base64 and multi-line input are refused.

### Subtask T009: SHA256 fingerprints and fingerprint syntax

**Purpose**: the registry key is the fingerprint; it must match what administrators see from `ssh-keygen`.

**Steps**:

1. `serveFingerprint(blob []byte) string` returns `"SHA256:" + base64.RawStdEncoding.EncodeToString(sha256.Sum256(blob)[:])` (43 characters after the prefix).
2. Fingerprint syntax is checked by `validServeFingerprint(value string) bool`, which WP01 defines in `serve_state.go` for registry validation (prefix `SHA256:`, exactly 43 characters from the standard base64 alphabet, no padding, decodes to 32 bytes). Reuse it; do not define a second checker. Add a test asserting that every value `serveFingerprint` produces passes `validServeFingerprint`.

**Files**: `serve_keys.go`.

**Validation**:
- [ ] Known-answer test: a fixed ed25519 public key line and its fingerprint (generate once with `ssh-keygen -l -E sha256 -f key.pub`, then embed both as constants).
- [ ] When `ssh-keygen` is on `PATH`, a test generates a fresh key with `newServeTestKey`, writes it to a temp file and compares `ssh-keygen -l -E sha256 -f` output with `serveFingerprint`. Skip with a visible reason when `ssh-keygen` is missing.

### Subtask T010: Managed authorized_keys line rendering

**Purpose**: FR-004 requires every managed entry to carry `restrict` and the exact forced command.

**Steps**:

1. Constants:

   ```go
   const (
       serveAuthorizedKeysBegin = "# BEGIN hn serve managed keys; edit with hn serve key"
       serveAuthorizedKeysEnd   = "# END hn serve managed keys"
   )
   ```

2. `renderServeAuthorizedKeysBlock(bindings []serveKeyBinding, hnPath string) ([]byte, error)`:
   - Refuse `hnPath` that is not absolute or contains `"`, `\`, `'` or whitespace.
   - Sort bindings by fingerprint. For each: `restrict,command="HNPATH serve git --key FINGERPRINT" CANONICAL hn-serve:ACTOR12` where `CANONICAL` is the parsed canonical key and `ACTOR12` is the first 12 characters of the actor.
   - Re-parse each stored `publicKey` with T008 and confirm its fingerprint equals the binding's fingerprint; refuse a mismatch (this is where a tampered registry is caught).
   - Output: begin marker line, one line per binding, end marker line, each terminated by `\n`. An empty registry still renders the two marker lines.

**Files**: `serve_keys.go`.

**Validation**:
- [ ] Golden test against the example in `contracts/server-state.md` using that example's key and fingerprint values (replace the illustrative key with a real generated key and its fingerprint so the re-parse check passes).

### Subtask T011: Managed block merge that preserves outside lines byte for byte

**Purpose**: administrators may keep their own keys in the same file; `hn serve` must never alter them.

**Steps**:

1. `mergeServeAuthorizedKeys(existing []byte, block []byte) ([]byte, error)`:
   - Split `existing` into lines while keeping each line's original terminator (`\n`, `\r\n`, or none for a final unterminated line). A line is a marker line when its content without the terminator equals a marker exactly.
   - Defects (refuse): more than one begin marker, more than one end marker, an end marker without a preceding begin marker, a begin marker without a following end marker.
   - When a block exists, replace the lines from begin through end inclusive with `block`, and keep everything before and after untouched, including the terminator style of outside lines.
   - When no block exists, append `block` after the existing content. If `existing` is non-empty and does not end with `\n`, insert one `\n` first. That inserted newline is the only permitted change to outside content, and the contract requires it.
2. Pure function, no I/O.

**Files**: `serve_keys.go`.

**Validation (golden cases)**:
- [ ] Empty file.
- [ ] File with two admin keys and no final newline.
- [ ] File with CRLF admin lines before and after an existing block.
- [ ] Block in the middle; block at the end; block replaced by an empty block.
- [ ] Each defect case refused.
- [ ] Property: for every case, the bytes outside the block are identical before and after.

### Subtask T012: Safe write of `$HOME/.ssh/authorized_keys`

**Purpose**: `sshd` reads this file with `StrictModes`; `hn serve` must not create an unsafe one or follow a symlink planted by someone else (NFR-004, NFR-008).

**Steps**:

1. `writeServeAuthorizedKeys(layout serveLayout, bindings []serveKeyBinding, hnPath string) error`:
   - Ensure `$HOME/.ssh` exists with mode 0700 (create it if absent). Refuse if it is a symlink, not a directory, or group- or other-writable.
   - If `authorized_keys` exists: refuse if it is a symlink, not a regular file, or group- or other-writable. Read it with a bound (for example 4 MiB).
   - Render, merge, and replace the file atomically: temp file in `.ssh/` with mode 0600, write, `fsync`, rename over the original, `fsync` the directory (`syncPrivateDirectory` in `identity_keyring.go` does this).
2. Do not chmod an existing unsafe file to make it safe; refuse and tell the administrator which path and mode to fix. Automatic repair would hide a misconfiguration.

**Files**: `serve_keys.go`.

**Validation**:
- [ ] Creates `.ssh` (0700) and `authorized_keys` (0600) when absent.
- [ ] Refuses a symlinked `authorized_keys`, a symlinked `.ssh`, a 0664 `authorized_keys` and a 0775 `.ssh`; the target file is unchanged after refusal.

### Subtask T013: Key unit tests and the serve guide section on key bindings

**Purpose**: prove the formats and document them for administrators.

**Steps**:

1. In `serve_keys_test.go`, add the shared helper used by later harness code:

   ```go
   type serveTestKey struct {
       Private     ed25519.PrivateKey
       PublicLine  string // "ssh-ed25519 BASE64 comment"
       Fingerprint string
   }
   func newServeTestKey(t *testing.T, comment string) serveTestKey
   ```

   Build the OpenSSH blob by hand: `string("ssh-ed25519") || string(32-byte public key)`, each length-prefixed. Later WPs (WP05, WP06) reuse this helper; keep its name and shape stable. For the real-sshd test in WP13 a private key in OpenSSH format is generated with `ssh-keygen` instead, so no private-key encoder is needed here.

2. Name tests `TestServeKeys...`. Cover every validation bullet above.

3. Write the "Key bindings and authorized_keys" section of `docs/serve-v0.md`: what a binding is (full SSH fingerprint to full actor fingerprint), that one key binds to one actor and an actor may have several keys, the supported key types, the fingerprint format with a pointer to `ssh-keygen -l -E sha256`, the exact managed block format with an example, the preservation rule for lines outside the block, the refusal cases for `.ssh` and `authorized_keys`, and the fact that a binding grants attribution and transport access only, never a policy role (C-006). Leave the `hn serve key` command walkthrough to WP05's section; link to it by heading.

**Files**: `serve_keys_test.go`, `docs/serve-v0.md`.

## Test Strategy

Pure unit tests with golden byte cases; one optional cross-check against `ssh-keygen`. No binary build is needed.

## Definition of Done

- [ ] T008 to T013 complete; full gate passes (`gofmt -l .` empty, `go vet ./...`, `go build ./...`, `go test -race ./...`, `git diff --check`).
- [ ] **FR-003**: short or malformed fingerprints and malformed or unsupported keys are refused; fingerprints match `ssh-keygen` output.
- [ ] **FR-004**: rendered entries carry `restrict` and `command="ABS_HN serve git --key FP"`; lines outside the block are preserved byte for byte in every golden case.
- [ ] **C-002 / C-003**: no SSH implementation and no new dependency.
- [ ] The guide section is written and matches the implemented format exactly.

## Risks

- Terminator preservation is easy to get subtly wrong; the property check in T011 guards it.
- Accepting an options prefix in `key add` input is convenient but must never copy options into the managed entry. Only the canonical key reaches the output.

## Reviewer Guidance

- Diff a merged file against its input outside the block with `cmp` in a scratch directory.
- Confirm the fingerprint re-check in T010 rejects a registry whose `publicKey` was altered.
- Confirm no write happens when any check fails.

## Activity Log

- 2026-10-25T00:09:18Z – system – Prompt generated via /spec-kitty.tasks
