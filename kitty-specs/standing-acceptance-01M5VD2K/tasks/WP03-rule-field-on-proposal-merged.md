---
work_package_id: WP03
title: The rule field on proposal.merged
dependencies: []
requirement_refs:
- C-004
- FR-010
- FR-022
planning_base_branch: feat/standing-acceptance
merge_target_branch: feat/standing-acceptance
branch_strategy: Planning artifacts for this mission were generated on feat/standing-acceptance. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into feat/standing-acceptance unless the human explicitly redirects the landing branch.
subtasks:
- T011
- T012
- T013
- T014
phase: Phase 1 - Foundations
history:
- !!omap
  - timestamp: '2026-10-26T06:54:14Z'
  - agent: system
  - action: Prompt generated via /spec-kitty.tasks
agent_profile: implementer-ivan
authoritative_surface: event.go
create_intent:
- standing_event_test.go
execution_mode: code_change
owned_files:
- event.go
- standing_event_test.go
role: implementer
tags: []
tracker_refs: []
---

# WP03: The rule field on proposal.merged

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter, and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `codex`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## Objective

Carry the rule name of a rule merge fact in the existing `proposal.merged` event, with no new event kind (OQ-5, C-004), and refuse at parse time every shape that verifiers could read differently ([contracts/rule-merge-fact.md](../contracts/rule-merge-fact.md) "Parse-time rules"):

| Input | Refusal |
|----|----|
| `rule` non-empty on any kind other than `proposal.merged` | `rule is only valid on proposal.merged` |
| `rule` on `proposal.merged` fails the rule-name grammar | `proposal merge rule must be a valid rule name` |
| A top-level key equal to `rule` under case folding but not exactly `rule` | `event has a non-canonical rule key` |
| The key `rule` appears more than once | `event has a duplicate rule key` |
| The key `rule` holds `null`, `""` or a non-string | `proposal merge rule must be a valid rule name` |

Every existing payload must encode byte-identically (FR-022, event part). This WP does not decide when a rule merge fact is valid; WP05 and WP07 do. WP07 documents the field in `docs/protocol-v0.md` together with the admission rule that gives it meaning; this WP owns no document, so its files stay disjoint from every other WP and execution lanes stay acyclic (tasks decision 12).

## Context

- Read [contracts/rule-merge-fact.md](../contracts/rule-merge-fact.md) "Wire shape" and "Parse-time rules"; [research.md](../research.md) findings 1 and 19 and decision `01M5VEZKK32DXWNPRAYGDMY3ZK`; [data-model.md](../data-model.md) "Rule merge fact".
- Start with `spec-kitty agent action implement WP03 --agent <name> --mission standing-acceptance-01M5VD2K`.
- Existing code in `event.go`: `Event`, `encodeAndSign` (calls `validateEventContent` before signing), `verifyEvent` (plain `json.Unmarshal`, then `strictResourceJSON` for resource kinds, then signature and `validateEventContent`), `validateEventContent` (early return for resource kinds). `validPipelineName` is in `ci.go`. `strictResourceJSON` is in `resource_model.go`.
- Why a key scan is needed: `encoding/json` folds key case (`"RULE"` fills `Rule`) and treats `"rule": null` as absent, so decoding alone cannot see those payloads.

## Branch Strategy

- **Planning/base branch at prompt creation**: `feat/standing-acceptance`
- **Final merge target for completed work**: `feat/standing-acceptance`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. Each computed lane gets exactly one worktree, and sequential WPs in the same lane reuse it.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `event.go` | CHANGE. One struct field, content rules, key scan. |
| `standing_event_test.go` | NEW. Parse and encoding tests (`TestStandingEvent...`), helpers prefixed `standingEvent`. |

## Subtasks

### Subtask T011: `Event.Rule` and content rules in `validateEventContent`

**Steps**:

1. Add the field immediately after `Evidence`:

   ```go
   Evidence     []string `json:"evidence,omitempty"`
   Rule         string   `json:"rule,omitempty"`
   ```

   `omitempty` keeps every existing payload's bytes unchanged. In the encoded JSON, `rule` follows `evidence`, which matches the contract's wire shape.
2. In `validateEventContent`, before the resource-kind early return, refuse `event.Rule != ""` on every resource kind with `rule is only valid on proposal.merged`.
3. After the resource branch, for every other kind: `event.Rule != "" && event.Kind != "proposal.merged"` gives the same message.
4. In the `proposal.merged` case: `event.Rule != "" && !validPipelineName(event.Rule)` gives `proposal merge rule must be a valid rule name`. Leave the existing `proposal.merged` checks untouched and in their current order; add the rule check after them.
5. Because `encodeAndSign` calls `validateEventContent`, Hubnot can no longer sign a misplaced or malformed rule.

### Subtask T012: Top-level `rule` key scan in `verifyEvent`

**Purpose**: refuse the shapes decoding cannot see, with one pass over the top-level object.

**Steps**:

1. Add `func scanEventRuleKey(payload []byte, kind string) error`, called from `verifyEvent` right after the first `json.Unmarshal` succeeds (so `kind` is known) and before the resource branch, for every kind.
2. Implementation: a `json.Decoder` over the payload; read the opening `{` with `Token()`; loop while `More()`: read the key with `Token()` (a string); if `strings.EqualFold(key, "rule")` is false, skip the value by decoding into one reused `json.RawMessage` variable (its `UnmarshalJSON` reuses the buffer); otherwise:
   - `key != "rule"`: `event has a non-canonical rule key`.
   - already seen: `event has a duplicate rule key`.
   - kind is not `proposal.merged`: `rule is only valid on proposal.merged`.
   - the value is not a JSON string, or is the empty string: `proposal merge rule must be a valid rule name`. Read the value with `Token()`; a `string` token is a string, anything else (`nil`, `bool`, `float64`, `json.Delim`) is refused. If it is a delimiter, return the error without consuming the nested value.
3. The grammar check of a present non-empty string stays in `validateEventContent` (T011), so `encodeAndSign` and `verifyEvent` share it.
4. Malformed JSON cannot reach the scan, because `json.Unmarshal` already succeeded; still return a wrapped error on any decoder error.
5. Keep the scan cheap: no map of keys, no decoding of other values into Go types. T014 measures it.

### Subtask T013: Parse-time and byte-identical encoding tests

**Steps** (tests `TestStandingEvent...` in `standing_event_test.go`):

1. **Byte identity**: for one event of every non-resource kind Hubnot writes today (issue.open, issue.comment, proposal.open, proposal.revise, review.submit, run.request, run.result, proposal.decision, proposal.merged without rule, identity events) build the `Event`, `json.Marshal` it, and assert the output has no `"rule"` key and equals the marshalled value of the same struct with the field removed. A practical way: compare against a golden payload string per kind kept in the test, or against `json.Marshal` of an anonymous struct copy without `Rule`.
2. **Accepted shapes**: a signed `proposal.merged` with `rule` `ordinary` verifies and decodes with `Rule == "ordinary"`; a 64-character name verifies.
3. **Refused shapes** (raw signed payloads, built with the test identity's private key so the signature is valid): `rule` on `review.submit` (F19); `"rule": ""`, `"rule": null`, `"rule": 1`, `"rule": ["ordinary"]`, `"RULE": "ordinary"`, `"Rule": "ordinary"`, two `rule` keys, and `"rule": "bad name"`, `"rule": "."`, a 65-character name on `proposal.merged` (F20 family). Each refused by `verifyEvent` with the table's message.
4. **Signing refusal**: `encodeAndSign` refuses an `Event` with `Rule` set on `review.submit` and with an invalid name on `proposal.merged`.
5. **Resource kinds**: a resource event payload with an added `"rule"` key (valid signature) is refused; a resource event without it verifies as before.
6. Build raw signed payloads with a small helper `standingEventSignRaw(t, identity, payload []byte) []byte` that signs with `identity.privateKey()`. Use an identity from `testIdentity` or `createIdentity` inside `inIdentityTestRepository`, as `event_test.go` does.

### Subtask T014: Key-scan cost and existing-payload audit

**Purpose**: the plan names two risks of the key scan: its cost on every event read, and the chance that a historical event carries a key the scan now refuses. Measure the first and audit the second.

**Steps**:

1. Add `BenchmarkStandingEventKeyScan` over a typical `review.submit` payload and a large `run.result` payload, and `BenchmarkStandingEventVerify` running `verifyEvent` on the same payloads. Record both numbers in the Activity Log. The scan should cost a small fraction of `verifyEvent` (which includes `ed25519.Verify`); if it costs more than 10% of it, simplify the scan (for example skip values with a depth-counting byte walk) and record the change.
2. Audit: add `TestStandingEventExistingPayloadsHaveNoRuleKey`, which walks every `event.json` blob reachable from `refs/hn/actors/*` in any fixture repository committed under `testdata/` (if the repository has such fixtures; check with `git ls-files testdata`), runs `scanEventRuleKey` on each and requires no refusal. If the repository has no committed event fixtures, the test instead encodes one event of every non-resource kind with every optional field set, and requires the scan to accept each; record which form you used.
3. Run the full existing suite once with the scan in place and record in the Activity Log that no existing test failed, which is the audit over every payload the suite produces (NFR-004).

## Test Strategy

Unit tests with real signatures, plus the unchanged existing event, replication and store tests (`event_test.go`, `revision_event_test.go`, `replication_test.go`). Run `go test -race -run 'TestStandingEvent|TestEvent' ./...` and `go test -run '^$' -bench 'StandingEvent' ./...`, then the full gate.

## Definition of Done

- [ ] T011 to T014 complete; full gate passes.
- [ ] **FR-010 (shape)**: `proposal.merged` carries an optional `rule`; `rule` is refused on every other kind, including resource kinds.
- [ ] Every row of the parse-time table refuses with its message, through `verifyEvent` and, where applicable, `encodeAndSign`.
- [ ] **FR-022 (event) / C-004**: every existing kind encodes byte-identically; every pre-existing test passes with no assertion change.
- [ ] Key-scan cost measured and recorded; existing-payload audit passes.
- [ ] No document changed (WP02 documents the policy fields; WP07 documents the `rule` field).

## Risks

- A historical event of another kind carrying a `rule` key would become invalid. No Hubnot client has written one; the full existing suite and its fixtures prove nothing else changes.
- `strictResourceJSON` may already refuse unknown keys; after the struct change `rule` is a known key, so the explicit refusal in T011 step 2 and the scan are what keep resource events strict. Test it.

## Reviewer Guidance

- Confirm the scan is called for every kind and runs before signature verification would let a malformed payload through.
- Confirm no other `Event` field moved.

## Activity Log

- 2026-10-26T06:54:14Z – system – Prompt generated via /spec-kitty.tasks
