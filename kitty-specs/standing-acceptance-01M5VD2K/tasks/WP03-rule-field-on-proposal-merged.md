---
work_package_id: WP03
title: The rule claim on proposal.merged
dependencies: []
requirement_refs:
- C-004
- FR-010
- FR-022
- FR-024
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

# WP03: The rule claim on proposal.merged

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter, and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `codex`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## Objective

Carry the rule name of a rule merge fact in the existing `proposal.merged` event, with no new event kind (OQ-5, C-004), and recognize it only in one canonical form, without refusing any payload that verifies today ([contracts/rule-merge-fact.md](../contracts/rule-merge-fact.md) "Wire shape" and "Rule claim recognition"; decisions `01M5VSEHEVA82N9T64ZH1P1Q2A` and `01M5VSEHF1G2YQDQW7HPVBF95K`, which amend OQ-5 and the plan decision `01M5VEZKK32DXWNPRAYGDMY3ZK`).

- `Event.Rule` carries the tag `json:"-"`, so `encoding/json` never reads or writes it. Every payload decodes and re-encodes exactly as before, including resource kinds, whose strict decoder keeps refusing an unknown `rule` key.
- `verifyEvent` keeps every existing check. For `proposal.merged` only, it then sets `Event.Rule` from `parseMergeRuleClaim(payload)` when the claim is canonical: exactly one top-level key equal to `rule` under Unicode case folding, spelled exactly `rule`, whose value is a JSON string accepted by `validPipelineName`. Every other shape leaves `Rule` empty and is never an error.
- `encodeAndSign` refuses `Rule` on any kind other than `proposal.merged` (`rule is only valid on proposal.merged`) and an invalid name on `proposal.merged` (`proposal merge rule must be a valid rule name`), and appends `rule` after every other field when `Rule` is set. These refusals restrict what Hubnot signs; they never restrict what verifiers accept.

There is no refusing key scan: decision A forbids any unconditional scan for a rule key, because it would reject authenticated legacy bytes (analysis finding C2). This WP does not decide when a fact is valid and changes no admission surface; with only this WP landed, every surface still runs today's maintainer check alone, so every fact that only the rule path would admit is still refused (FR-025). WP05 adds the validator and the union combinator, and WP07 documents the field together with the admission rule that gives it meaning. This WP owns no document, so its files stay disjoint from every other WP and execution lanes stay acyclic (tasks decision 12).

## Context

- Read [contracts/rule-merge-fact.md](../contracts/rule-merge-fact.md) "Wire shape", "Rule claim recognition" and "Legacy history fixtures"; [research.md](../research.md) findings 1, 19 and 20 and section "Rule claim recognition and union validity"; [data-model.md](../data-model.md) "Rule merge fact".
- Start with `spec-kitty agent action implement WP03 --agent <name> --mission standing-acceptance-01M5VD2K`.
- Existing code in `event.go`: `Event`, `encodeAndSign` (calls `validateEventContent` before signing), `verifyEvent` (plain `json.Unmarshal`, then `strictResourceJSON` for resource kinds, then signature and `validateEventContent`), `validateEventContent` (early return for resource kinds). `validPipelineName` is in `ci.go`. `strictResourceJSON` and `resourceJSONKeys` are in `resource_model.go`; `resourceJSONKeys` derives the allowed keys from the `Event` type's JSON tags, which is why `Rule` must not have a `rule` tag.
- Why a tagged field is wrong: `encoding/json` folds key case (`"RULE"` would fill it), treats `"rule": null` as absent, and returns a decode error for `"rule": 1`, which would make a fact that is valid today fail to decode.

## Branch Strategy

- **Planning/base branch at prompt creation**: `feat/standing-acceptance`
- **Final merge target for completed work**: `feat/standing-acceptance`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. Each computed lane gets exactly one worktree, and sequential WPs in the same lane reuse it.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `event.go` | CHANGE. One struct field with `json:"-"`, `parseMergeRuleClaim`, the call in `verifyEvent`, signing rules and the encoding of `rule` in `encodeAndSign`. |
| `standing_event_test.go` | NEW. Claim, legacy and encoding tests (`TestStandingEvent...`), helpers prefixed `standingEvent`. |

## Subtasks

### Subtask T011: `Event.Rule`, signing rules and encoding

**Steps**:

1. Add the field after `Evidence` with the tag `json:"-"`:

   ```go
   Evidence     []string `json:"evidence,omitempty"`
   // Rule is a canonical rule claim of a proposal.merged event. It is set
   // only by verifyEvent from the signed bytes and only written by
   // encodeAndSign, never by encoding/json, so legacy payloads keep their
   // meaning (decision 01M5VSEHF1G2YQDQW7HPVBF95K).
   Rule         string   `json:"-"`
   ```

2. In `validateEventContent`, before the resource-kind early return, refuse `event.Rule != ""` on resource kinds with `rule is only valid on proposal.merged`; after it, refuse the same on every kind other than `proposal.merged`; in the `proposal.merged` case, after the existing checks and in their current order, refuse `event.Rule != "" && !validPipelineName(event.Rule)` with `proposal merge rule must be a valid rule name`. `verifyEvent` only ever sets a valid name on `proposal.merged`, so these checks never refuse a verified payload; they bind what `encodeAndSign` will sign.
3. In `encodeAndSign`, when `event.Rule == ""`, marshal exactly as today. When it is set, marshal the value below, so `rule` follows every other field. Confirm with a test that it emits the fields in the same order as `json.Marshal(event)` and adds only the final `rule` key.

   ```go
   wire := struct {
       Event
       Rule string `json:"rule"`
   }{event, event.Rule}
   ```

### Subtask T012: `parseMergeRuleClaim` and its call in `verifyEvent`

**Purpose**: recognize the canonical claim with one pass over the top-level object, and refuse nothing.

**Steps**:

1. Add `func parseMergeRuleClaim(payload []byte) string`. Implementation: a `json.Decoder` over the payload; read the opening `{` with `Token()`; loop while `More()`: read the key with `Token()`; when `strings.EqualFold(key, "rule")` is false, skip the value by decoding into one reused `json.RawMessage`; otherwise count the key, remember whether it is exactly `rule`, and read its value with `Token()` when it is a scalar (a `string` token is a candidate name; anything else, including `nil`, `bool`, `float64` or a `json.Delim`, disqualifies the claim; for a delimiter, decode the rest of the nested value into the reused `json.RawMessage` to keep the walk aligned).
2. Return the name only when exactly one key matched under case folding, it is spelled exactly `rule`, its value is a string, and `validPipelineName` accepts it. Return `""` in every other case, including any decoder error.
3. Call it from `verifyEvent` only when `event.Kind == "proposal.merged"`, after every existing check has passed and before the return, and set `event.Rule`. Every other kind never calls it and keeps today's behavior.
4. Keep the walk cheap: no map of keys and no decoding of other values into Go types. T014 measures it.

### Subtask T013: Claim, legacy and encoding tests

**Steps** (tests `TestStandingEvent...` in `standing_event_test.go`):

1. **Byte identity**: for one event of every non-resource kind Hubnot writes today (issue.open, issue.comment, proposal.open, proposal.revise, review.submit, run.request, run.result, proposal.decision, proposal.merged without rule, identity events) build the `Event`, run `encodeAndSign`, and assert the payload has no `"rule"` key and equals `json.Marshal` of the same value, so `Rule` changes nothing when it is empty.
2. **Canonical claim round trip**: a `proposal.merged` signed by `encodeAndSign` with `Rule` `ordinary` ends with `,"rule":"ordinary"}` and verifies with `Rule == "ordinary"`; a 64-character name round-trips.
3. **No claim, no error** (raw signed payloads, built with the test identity's private key so the signature is valid, all on `proposal.merged`): `"rule": ""`, `"rule": null`, `"rule": 1`, `"rule": true`, `"rule": ["ordinary"]`, `"rule": {"x": 1}`, `"RULE": "ordinary"`, `"Rule": "ordinary"`, `"rule"` plus `"RULE"`, two `rule` keys, `"rule": "bad name"`, `"rule": "."` and a 65-character name. Each verifies without error, with the same event ID and the same decoded fields as the payload with the key removed would give, and with `Rule == ""`.
4. **Legacy history, other kinds** (fixtures L01, L02 and L06 of the contract): each payload asserts its declared outcome. L01 and L02, a signed `review.submit` with `"rule": "annotation"`, a signed `run.request` with `"RULE": "x"` and a signed `issue.comment` with `"rule": 1`, verify without error and with `Rule == ""`. L06 is the negative control: a signed resource event with an added `rule` key is still refused by `strictResourceJSON` with today's error `invalid resource event JSON: unknown field "rule"`, and the same resource event without the key verifies.
5. **Signing refusal**: `encodeAndSign` refuses an `Event` with `Rule` set on `review.submit` and on a resource kind, and with an invalid name on `proposal.merged`.
6. Build raw signed payloads with a helper `standingEventSignRaw(t, identity, payload []byte) []byte` that signs with `identity.privateKey()`. Use an identity from `testIdentity` or `createIdentity` inside `inIdentityTestRepository`, as `event_test.go` does.

### Subtask T014: Claim-walk cost and legacy audit

**Purpose**: measure the walk's cost on every `proposal.merged` read, and show that no payload that verifies today changes verdict.

**Steps**:

1. Add `BenchmarkStandingEventRuleClaim` over a typical `proposal.merged` payload with and without a claim, and `BenchmarkStandingEventVerify` running `verifyEvent` on the same payloads. Record both numbers in the Activity Log. The walk should cost a small fraction of `verifyEvent` (which includes `ed25519.Verify`); if it costs more than 10% of it, simplify the walk (for example a depth-counting byte walk to skip values) and record the change.
2. Audit: add `TestStandingEventLegacyVerdictsUnchanged`. For every `event.json` blob reachable from `refs/hn/actors/*` in any fixture repository committed under `testdata/` (check with `git ls-files testdata`), and for every raw payload of T013 steps 3 and 4, assert that `verifyEvent` gives the same error or no error, the same ID and the same decoded fields (ignoring `Rule`) as a decode into a local copy of the pre-mission `Event` struct definition kept in the test file. If the repository has no committed event fixtures, use the T013 payloads plus one event of every non-resource kind with every optional field set; record which form you used.
3. Run the full existing suite once and record in the Activity Log that no existing test failed (NFR-004).

## Test Strategy

Unit tests with real signatures, plus the unchanged existing event, replication and store tests (`event_test.go`, `revision_event_test.go`, `replication_test.go`). Run `go test -race -run 'TestStandingEvent|TestEvent' ./...` and `go test -run '^$' -bench 'StandingEvent' ./...`, then the full gate.

## Definition of Done

- [ ] T011 to T014 complete; full gate passes.
- [ ] **FR-010 (shape) / FR-024 (claim)**: `proposal.merged` carries an optional canonical rule claim; every non-canonical shape is no claim and no error.
- [ ] **C-004 / FR-022 (event)**: no payload that verifies at `d881d9b` changes verdict, ID or decoded fields; resource events with a `rule` key are still refused; every existing kind encodes byte-identically.
- [ ] `encodeAndSign` writes only canonical claims and only on `proposal.merged`.
- [ ] Claim-walk cost measured and recorded; legacy audit passes.
- [ ] No admission surface changed; no document changed (WP02 documents the policy fields; WP07 documents the `rule` field).

## Risks

- A `rule` JSON tag on `Event` would let resource events with a `rule` key pass `resourceJSONKeys`, and would turn `"rule": 1` into a decode error. The `json:"-"` tag and the T013 step 4 control test guard both.
- The walk must stay aligned when a `rule`-like key holds a nested value; T013 step 3 covers arrays and objects.

## Reviewer Guidance

- Confirm `verifyEvent` returns exactly the errors it returned before for every payload of T013 and T014.
- Confirm no other `Event` field moved and that `encodeAndSign` output is unchanged when `Rule` is empty.

## Activity Log

- 2026-10-26T06:54:14Z – system – Prompt generated via /spec-kitty.tasks
- 2026-10-26T09:45:00Z – llxprt – Prompt revised after analysis (C2, decisions 01M5VSEHEVA82N9T64ZH1P1Q2A and 01M5VSEHF1G2YQDQW7HPVBF95K): canonical claim recognition replaces the refusing key scan; legacy verdict audit added
- 2026-10-26T10:35:12Z – llxprt – Prompt revised after analysis round 2 (I5; decision 01M5VWE2Q03GKFCWDA2WGS8MPZ): T013 step 4 asserts each legacy fixture's declared outcome, with L06 as the negative control
- 2026-10-26T11:17:05Z – llxprt – shell_pid=85418 – T014 cost (go test -bench StandingEvent, Ryzen 9 8945HS): decoder-based walk 10.7us no-claim / 13.6us claim vs verifyEvent 70.9us (~15%, over the 10% budget), so the walk was replaced by a depth-counting byte walk: parseMergeRuleClaim 0.44us no-claim / 0.63us claim vs verifyEvent 52.1us / 51.6us (~1%). The decoder form is kept in standing_event_test.go as a reference; a differential test and FuzzStandingEventRuleClaim (60s, 2.58M execs) found no disagreement on valid JSON. Legacy audit form: testdata/ has no committed event fixtures, so TestStandingEventLegacyVerdictsUnchanged uses one event of every non-resource kind with optional fields set plus the T013 step 3 and 4 raw payloads; its declared verdicts were also confirmed on base 0f109eb (event.go identical to d881d9b) in a scratch worktree. NFR-004: full race suite passed, no existing test failed or changed.
