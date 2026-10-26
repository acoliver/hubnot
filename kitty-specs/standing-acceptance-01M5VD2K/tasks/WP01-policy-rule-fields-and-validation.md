---
work_package_id: WP01
title: Automerge rule fields and policy validation
dependencies: []
requirement_refs:
- C-007
- FR-001
- FR-002
- FR-003
- FR-004
- FR-005
- FR-022
- FR-023
planning_base_branch: feat/standing-acceptance
merge_target_branch: feat/standing-acceptance
branch_strategy: Planning artifacts for this mission were generated on feat/standing-acceptance. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into feat/standing-acceptance unless the human explicitly redirects the landing branch.
subtasks:
- T001
- T002
- T003
- T004
- T005
- T006
phase: Phase 1 - Foundations
history:
- !!omap
  - timestamp: '2026-10-26T06:54:14Z'
  - agent: system
  - action: Prompt generated via /spec-kitty.tasks
agent_profile: implementer-ivan
authoritative_surface: protected_checks.go
create_intent:
- standing_policy_test.go
execution_mode: code_change
owned_files:
- protected_checks.go
- standing_policy_test.go
- docs/governance-v0.md
role: implementer
tags: []
tracker_refs: []
---

# WP01: Automerge rule fields and policy validation

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter, and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `codex`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## Objective

Give each automerge rule two optional fields and validate them exactly as [contracts/policy-rule-fields.md](../contracts/policy-rule-fields.md) specifies:

- `acceptance`: exactly `"maintainers"` or `"rule"`. Absent means `maintainers`, which is today's behavior (FR-001).
- `reviewers`: 1 to 64 distinct full actor fingerprints, each in `proposals.trustedReviewers`, with at least `proposals.requiredApprovals` members (FR-005, validation part).

Under acceptance mode `rule`, executing actors need not be maintainers (FR-002); `proposals.requiredApprovals` must be at least 1 (FR-004); and no path selector may cover `.hn` or overlap a `protectedChecks.paths` selector (FR-003, C-007). A policy without the new fields parses, validates and encodes exactly as before (FR-022, policy part).

This WP changes no runtime merge behavior. Later WPs read the fields.

## Context

- Read [spec.md](../spec.md) User Story 2 (scenarios 4 to 6), FR-001 to FR-005 and FR-022; [contracts/policy-rule-fields.md](../contracts/policy-rule-fields.md) in full; [data-model.md](../data-model.md) "Automerge rule (changed)"; [research.md](../research.md) findings 2 and 7 and decision `01M5VEZRTBF8R7RZGHSH5EC6VS`.
- Start with `spec-kitty agent action implement WP01 --agent <name> --mission standing-acceptance-01M5VD2K`.
- Existing code: `AutomergeRule`, `validateOptionalPolicy`, `validatePolicyPaths`, `protectedPathMatches` (`protected_checks.go`); `parsePolicyBytes` (strict decoder with `DisallowUnknownFields`), `validatePolicy`, `validateActorList`, `actorListed` (`policy.go`); `validPipelineName` (`ci.go`).
- Existing tests that must pass unchanged: `policy_test.go`, `protected_checks_test.go`, `policy_rules_acceptance_test.go`, `automerge_test.go`, `serve_automerge_test.go`, `policy_commands_test.go`.

**Critical encoding fact**: several existing tests build a `PolicyDocument` value and write it with `json.Marshal` or `json.MarshalIndent` (`writeTestPolicy` in `policy_test.go`, `serve_harness_test.go` line 277, `policy_rules_acceptance_test.go` line 78, `protected_checks_test.go` line 423). A rule whose new fields are absent must therefore encode with no `acceptance` and no `reviewers` key, byte for byte as today. Use the Go 1.24+ `omitzero` tag option together with a `MarshalJSON` method (details in T001).

## Branch Strategy

- **Planning/base branch at prompt creation**: `feat/standing-acceptance`
- **Final merge target for completed work**: `feat/standing-acceptance`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. Each computed lane gets exactly one worktree, and sequential WPs in the same lane reuse it.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `protected_checks.go` | CHANGE. New types and decoders, two fields on `AutomergeRule`, validation in `validateOptionalPolicy`, overlap helper. WP08 later extracts `changedPathsInOneStore` in this file; do not touch `validateProtectedDecisionAt`. |
| `standing_policy_test.go` | NEW. Unit tests and black-box refusal tests. WP02 adds inspection tests later. |
| `docs/governance-v0.md` | CHANGE. One new section on rule fields with the `standing-policy-refusals` example. |

## Subtasks

### Subtask T001: `AutomergeAcceptance` and `AutomergeReviewers` types, decoders and `AutomergeRule` fields

**Purpose**: represent presence, the raw JSON value and the parsed value of each new field, so validation can refuse `null`, empty strings, wrong types and wrong case while knowing the rule name.

**Steps**:

1. In `protected_checks.go` add:

   ```go
   // AutomergeAcceptance is a rule's acceptance mode. Its decoder never
   // fails; validateOptionalPolicy refuses every value other than the
   // strings "maintainers" and "rule", because only there is the rule
   // name known for the message.
   type AutomergeAcceptance struct {
       present bool
       raw     json.RawMessage
   }
   ```

   Keep the fields unexported. Provide:
   - `func (a *AutomergeAcceptance) UnmarshalJSON(raw []byte) error`: set `present = true`, copy `raw` (the decoder reuses its buffer), return nil. Go calls this method for `null` too, because the field is a non-pointer named type.
   - `func (a AutomergeAcceptance) MarshalJSON() ([]byte, error)`: return the stored raw value when present. Only tests in this mission marshal a present value.
   - `func (a AutomergeAcceptance) IsZero() bool { return !a.present }`, so `omitzero` drops an absent field.
   - `func (a AutomergeAcceptance) Mode() string`: `"maintainers"` when absent; otherwise the decoded string. Call it only after validation.
   - `func (a AutomergeAcceptance) Standing() bool`: true only when `Mode() == "rule"`.
   - `func automergeAcceptanceOf(mode string) AutomergeAcceptance`: builds a present value from a Go string, for tests and fixtures.

2. Add `AutomergeReviewers` with the same shape (`present`, `raw`), plus `Actors() []string` that decodes the validated array, `IsZero`, `MarshalJSON`, and `automergeReviewersOf(actors ...string)`.

3. Extend the rule struct. Keep the existing field order and add the new fields last so marshalled output for absent fields is unchanged:

   ```go
   type AutomergeRule struct {
       Name         string              `json:"name"`
       TargetBranch string              `json:"targetBranch"`
       Actors       []string            `json:"actors"`
       Authors      []string            `json:"authors"`
       Paths        []string            `json:"paths"`
       Acceptance   AutomergeAcceptance `json:"acceptance,omitzero"`
       Reviewers    AutomergeReviewers  `json:"reviewers,omitzero"`
   }
   ```

4. Do not change `parsePolicyBytes`. `DisallowUnknownFields` still refuses every other unknown key.

**Validation**: a unit test marshals the policy built by `newAutomergeTestRepo`-style construction with no new fields and compares it byte for byte with the same document encoded before this change (encode a hand-written equivalent map, or keep a golden string in the test).

### Subtask T002: Acceptance and reviewer validation (contract rows 1 to 8)

**Purpose**: refuse every malformed value with the contract's message, in the contract's order, after the existing name, target-branch, actor-list and path checks.

**Steps**: in `validateOptionalPolicy`, inside the per-rule loop, after `validatePolicyPaths` for the rule, run in this order and return the first failure (`<rule>` is `%q` of the rule name):

1. `acceptance` present and its raw value is `null`, not a JSON string, or a string other than `maintainers` or `rule` (case-sensitive): `automerge rule <rule> acceptance must be "maintainers" or "rule"`.
2. `reviewers` present and its raw value is `null` or not an array of strings: `automerge rule <rule> reviewers must be an array of actor fingerprints`.
3. Acceptance mode `maintainers` and an actor is not a maintainer: keep the existing message `automerge rule %q actor %s is not a maintainer` byte for byte. Move the existing loop so it runs only in this mode.
4. Acceptance mode `rule` and `policy.Proposals.RequiredApprovals == 0`: `automerge rule <rule> has acceptance "rule", which requires proposals.requiredApprovals of at least 1`.
5. `reviewers` present with 0 or more than 64 entries: `automerge rule <rule> requires 1 to 64 reviewers when reviewers is present`.
6. `validateActorList("automerge reviewers", reviewers)`: its existing messages for invalid and duplicate actors.
7. A reviewer not in `proposals.trustedReviewers`: `automerge rule <rule> reviewer <actor> is not a trusted reviewer`.
8. `len(reviewers) < proposals.requiredApprovals`: `automerge rule <rule> lists N reviewers, fewer than proposals.requiredApprovals (M)`.

Note on ordering: the existing per-rule loop checks `actors` and `authors` with a `map` iteration. Keep that code as is; row 3 replaces only the maintainer-membership loop that follows it.

**Files**: `protected_checks.go`.

### Subtask T003: Path safety for standing rules (rows 9 and 10) and the overlap helper

**Purpose**: a standing rule must never authorize a change to policy files, pipeline definitions or protected-check paths (FR-003, C-007).

**Steps**:

1. Row 9, acceptance mode `rule` only: for each rule path `p`, let `q = strings.TrimSuffix(p, "/")`. Refuse when `q == ".hn"` or `strings.HasPrefix(q, ".hn/")`: `automerge rule <rule> path <path> covers .hn, which a rule with acceptance "rule" cannot cover` (print the path with `%q`).
2. Row 10, acceptance mode `rule` only and `policy.ProtectedChecks != nil`: refuse the first rule path that overlaps any `protectedChecks.paths` selector: `automerge rule <rule> path <path> overlaps protected check path <protected>` (both with `%q`).
3. Add the helper:

   ```go
   // automergeSelectorsOverlap reports whether a rule selector and a
   // protected selector are equal or one is an ancestor of the other.
   func automergeSelectorsOverlap(rule, protected string) bool {
       r, p := strings.TrimSuffix(rule, "/"), strings.TrimSuffix(protected, "/")
       return r == p || strings.HasPrefix(p, r+"/") || strings.HasPrefix(r, p+"/")
   }
   ```

4. Check rule paths in their document order and protected paths in their document order, so the reported pair is deterministic.
5. Do not change the runtime blockers in `inspectAutomerge` ("policy amendments require manual merge", "protected check changes require manual merge"). They stay in both modes.

**Edge cases from the contract**: with protected `["scripts/lint.sh"]`: `scripts/` and `scripts/lint.sh` overlap; `scripts/lint.sh.bak` and `script/` do not. With protected `["scripts/"]`: `scripts/lint/run.sh` and `scripts/` overlap; `scriptsx/` does not. `.hn`, `.hn/`, `.hn/pipelines/` and `.hn/policy.json` are all refused by row 9. A rule path `.hnx/` is allowed.

### Subtask T004: Decoder and validation unit tests

**Purpose**: contract coverage of every row and of the decoders, independent of Git.

**Steps** (tests named `TestStandingPolicyFields...` in `standing_policy_test.go`):

1. Decode table through `parsePolicyBytes` with a minimal valid policy (one maintainer M, one trusted reviewer R, `requiredApprovals` 1, `requiredAccepts` 1, one rule). Cases for `acceptance`: absent (valid, `Mode()` is `maintainers`), `"maintainers"` (valid), `"rule"` with a non-maintainer actor (valid, `Standing()` true), `"Rule"`, `""`, `null`, `1`, `true`, `["rule"]`, `{}` (each refused with the row 1 message naming the rule).
2. Cases for `reviewers`: absent, `[R]`, `null`, `"R"`, `[1]`, `[]`, 65 entries, `[R, R]`, `[X]` with X untrusted, and `requiredApprovals` 2 with two trusted reviewers but `reviewers` listing one. Expect rows 2, 5, 6, 7 and 8 in turn.
3. Mode cases: explicit `"maintainers"` with a non-maintainer actor and absent `acceptance` with a non-maintainer actor both give the existing maintainer message. `"rule"` with `requiredApprovals` 0 gives row 4.
4. Path cases: every example of T003, in both directions, plus the four `.hn` selectors.
5. Encoding: an existing-style `PolicyDocument` built in Go with no new fields marshals without the keys `acceptance` and `reviewers`; a document with `automergeAcceptanceOf("rule")` and `automergeReviewersOf(R)` marshals with them and round-trips through `parsePolicyBytes`.

Assert messages with `strings.Contains` on the full expected message.

### Subtask T005: Black-box `hn policy check` refusals for US2-4, US2-5 and US2-6

**Purpose**: prove the refusals through the public CLI, as C-002 requires.

**Steps**:

1. Build the binary once with `buildOperationalBinary(t)`. Create a repository with `initOperationalRepository`, two identities with `hn init` in separate clones (maintainer and a second actor used as executing actor), and commit a valid base policy on `main`.
2. For each refused draft of T004 (one per row 1 to 10, plus each `.hn` selector and both overlap directions), write the draft to `.hn/policy.json` in the worktree and run `hn policy check --base main --file .hn/policy.json`. Assert a non-zero exit, the expected message naming the rule and the field or path, and that no ref changed (`git for-each-ref` before and after).
3. Run the same command for a valid standing draft (non-maintainer executing actor, `reviewers` set) and assert exit 0.
4. Name helpers `standingPolicy...`. Use `runOperationalCommand` and its failure-returning variants as other black-box tests do; do not call `cmdPolicyCheck` in-process.

### Subtask T006: Governance guide rule-field section and `standing-policy-refusals` example

**Purpose**: HN-005 and FR-023: the policy fields are documented where policy is documented, with an executed example.

**Steps**:

1. In `docs/governance-v0.md`, under "Optional check protection and automerge", add a subsection titled "Standing acceptance rule fields" after "Rule-based native automerge". Explain in plain sentences: what `acceptance` and `reviewers` mean; that absence keeps today's behavior; that a rule with acceptance mode `rule` is the standing acceptance for every proposal that satisfies it, granted once by the maintainer-accepted amendment that adopted it; the requirement of at least one approval; why a standing rule cannot cover `.hn` or overlap protected-check paths; and that older clients reject a policy that contains either field, even `"acceptance": "maintainers"`.
2. Add `<!-- example: standing-policy-refusals -->` followed by a fenced block that shows, for rows 4 to 10, the draft fragment and the exact refusal message. Use placeholder fingerprints in the form the guide already uses.
3. Add `TestStandingPolicyDocumentedRefusals` in `standing_policy_test.go`. Its source must contain the exact marker string `<!-- example: standing-policy-refusals -->` (the guard `TestServeQualityDocumentedExamplesAreExecuted` looks for it). The test reads the block that follows the marker, substitutes real fingerprints, runs each draft through `hn policy check` and compares each refusal with the documented message.
4. Style: direct sentences, descriptive headings, no em dashes, no rhetorical contrasts.

## Test Strategy

Unit tests for decoders and every validation row, black-box tests through the built binary for the refusals, and the unchanged existing suite. Run `go test -race -run 'TestStandingPolicy' ./...`, then the full gate.

## Definition of Done

- [ ] T001 to T006 complete; `gofmt -l .` prints nothing, `go vet ./...`, `go build ./...`, `go test -race ./...` and `git diff --check` pass.
- [ ] **FR-001**: `"maintainers"` and `"rule"` accepted; `null`, `""`, `"Rule"`, numbers, booleans, arrays and objects refused with the row 1 message.
- [ ] **FR-002**: non-maintainer executing actors are valid only under acceptance mode `rule`; the existing maintainer message is unchanged.
- [ ] **FR-003**: every `.hn` selector and every overlap direction is refused with a message naming the rule and both selectors.
- [ ] **FR-004**: acceptance mode `rule` with `requiredApprovals` 0 is refused.
- [ ] **FR-005** (validation): rows 2 and 5 to 8 refuse with their messages.
- [ ] **FR-022** (policy): policies without the new fields parse and marshal byte for byte as before; every pre-existing test passes with no assertion change.
- [ ] Guide section written; `standing-policy-refusals` executed by a test that contains the marker.

## Risks

- `encoding/json` folds key case when decoding into a struct, so `"Acceptance": "rule"` would also fill the field. This is acceptable for policy files: the value is still validated, and older clients refuse the key. Record in the Activity Log that you checked this.
- A decoder that returns an error for `null` would produce a message without the rule name. Keep all refusals in `validateOptionalPolicy`.

## Reviewer Guidance

- Diff `validateOptionalPolicy` and confirm the maintainers-mode path still produces the same messages in the same order.
- Marshal a pre-existing test policy before and after the change and compare bytes.
- Check each overlap example of the contract against `automergeSelectorsOverlap`.

## Activity Log

- 2026-10-26T06:54:14Z – system – Prompt generated via /spec-kitty.tasks
