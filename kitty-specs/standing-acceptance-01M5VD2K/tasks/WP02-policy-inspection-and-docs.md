---
work_package_id: WP02
title: Policy inspection output and policy documentation for standing rules
dependencies:
- WP01
requirement_refs:
- FR-007
- FR-023
planning_base_branch: feat/standing-acceptance
merge_target_branch: feat/standing-acceptance
branch_strategy: Planning artifacts for this mission were generated on feat/standing-acceptance. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into feat/standing-acceptance unless the human explicitly redirects the landing branch.
subtasks:
- T007
- T008
- T009
- T010
phase: Phase 1 - Foundations
history:
- !!omap
  - timestamp: '2026-10-26T06:54:14Z'
  - agent: system
  - action: Prompt generated via /spec-kitty.tasks
agent_profile: implementer-ivan
authoritative_surface: policy_commands.go
create_intent:
- standing_policy_test.go
execution_mode: code_change
owned_files:
- policy_commands.go
- standing_policy_test.go
- docs/governance-v0.md
- docs/protocol-v0.md
role: implementer
tags: []
tracker_refs: []
---

# WP02: Policy inspection output and policy documentation for standing rules

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter, and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `codex`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## Objective

Make the maintainer's one act of granting standing acceptance visible before it lands (FR-007, US2-1):

- `hn policy show` prints each rule's acceptance mode and its counted reviewers, and labels a standing rule's actors "Executing actors".
- `hn policy check --base REV --file PATH` reports, per rule, added, removed and changed acceptance modes and reviewer lists, in deterministic order.

Every line printed today keeps its text and position. The new lines are additive, so existing tests, which use substring checks, keep passing.

This WP also writes the policy documentation for WP01's fields (HN-005, FR-023): the governance guide's "Standing acceptance rule fields" section with the `standing-policy-refusals` and `standing-policy-rule` examples, and the protocol guide's policy-field paragraph. WP01 owns no document, so its files stay disjoint from every other WP and execution lanes stay acyclic (tasks decision 12).

## Context

- Read [contracts/policy-rule-fields.md](../contracts/policy-rule-fields.md) "Inspection output" and "Tested examples"; spec US2-1 and FR-007.
- Start with `spec-kitty agent action implement WP02 --agent <name> --mission standing-acceptance-01M5VD2K`.
- Existing code in `policy_commands.go`: `printOptionalPolicy`, `compareOptionalPolicies`, `actorChanges`, `printActors`, `sortedDifference`, `joinedOrNone`, `policyChange`.
- From WP01: `AutomergeRule.Acceptance` (`Mode()`, `Standing()`, `IsZero()`), `AutomergeRule.Reviewers` (`Actors()`, `IsZero()`), `automergeAcceptanceOf`, `automergeReviewersOf`, and the black-box helpers in `standing_policy_fields_test.go` (`standingPolicyFields...`), which this WP reuses without editing that file.
- Existing tests that read this output: `policy_commands_test.go`, `policy_rules_acceptance_test.go` (`assertPolicyOutputContains`). Run them before and after.

## Branch Strategy

- **Planning/base branch at prompt creation**: `feat/standing-acceptance`
- **Final merge target for completed work**: `feat/standing-acceptance`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. Each computed lane gets exactly one worktree, and sequential WPs in the same lane reuse it.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `policy_commands.go` | CHANGE. `printOptionalPolicy` and `compareOptionalPolicies` only. |
| `standing_policy_test.go` | NEW. `TestStandingPolicyInspection...` and the two example tests. Do not edit WP01's `standing_policy_fields_test.go`. |
| `docs/governance-v0.md` | CHANGE. New "Standing acceptance rule fields" subsection with both examples. |
| `docs/protocol-v0.md` | CHANGE. "Optional policy rules" section only. |

## Subtasks

### Subtask T007: `hn policy show` acceptance, executing actors and counted reviewers

**Steps**: in `printOptionalPolicy`, for each rule (already sorted by name), print in this order after the existing `Rule NAME; target branch BRANCH:` line:

1. The acceptance line:
   - acceptance mode `rule`: `  Acceptance: rule (standing acceptance; no maintainer acceptance per proposal)`
   - acceptance mode `maintainers` (explicit or absent): `  Acceptance: maintainers (maintainer acceptance required)`
2. The actor block. Acceptance mode `maintainers`: keep `printActors("  Executing maintainers:", r.Actors)` exactly. Acceptance mode `rule`: `printActors("  Executing actors:", r.Actors)`.
3. `printActors("  Proposal authors:", r.Authors)` unchanged.
4. The counted-reviewer block. `reviewers` present: `printActors("  Counted reviewers:", r.Reviewers.Actors())`. Absent: the single line `  Counted reviewers: all trusted reviewers`.
5. `  Allowed literal paths:` and the quoted sorted paths, unchanged.

Expected output for the contract's example rule:

```text
Rule ordinary; target branch main:
  Acceptance: rule (standing acceptance; no maintainer acceptance per proposal)
  Executing actors:
    <executor fingerprint>
  Proposal authors:
    <author fingerprint>
  Counted reviewers:
    <gpt-reviewer fingerprint>
  Allowed literal paths:
    "docs/"
    "src/"
    "tests/"
```

Check how `printActors` formats its list (indentation, sorting, full fingerprints) and reuse it so new blocks match existing ones. If `printActors` indents list items relative to the label in a way that differs from the example above, follow `printActors` and record the difference in the Activity Log; the contract describes intent, the existing helper decides the exact indentation.

**Files**: `policy_commands.go`.

### Subtask T008: `hn policy check` acceptance and reviewer changes

**Steps**: in `compareOptionalPolicies`, inside the loop over sorted rule names, after the existing per-rule changes (enabled, target branch, actors, authors, paths added, paths removed), append in this order:

1. `automerge rule NAME acceptance` with one of these values:

   | Base rule | Proposed rule | Value |
   |----|----|----|
   | absent | present, mode M | `added (M)` |
   | present, mode M | absent | `removed (M)` |
   | present, mode M | present, mode M | `unchanged (M)` |
   | present, mode A | present, mode B | `changed (A -> B)` |

   "Present" and "absent" refer to the rule, not the field. An absent `acceptance` field compares as `maintainers`, so a rule that gains `"acceptance": "maintainers"` reports `unchanged (maintainers)`.
2. `actorChanges("automerge rule "+NAME+" reviewers", oldReviewers, newReviewers)`, where an absent field is an empty list. This produces the existing "added" and "removed" lines, for example `automerge rule ordinary reviewers added: <fingerprint>` and `automerge rule ordinary reviewers removed: none`. Confirm the exact text by reading `actorChanges` and `printPolicyCheck`.

Do not reorder or rename any existing change line.

**Files**: `policy_commands.go`.

### Subtask T009: Black-box inspection tests for US2-1

**Steps** (tests `TestStandingPolicyInspection...` in `standing_policy_test.go`, through the built binary):

1. **US2-1**: base policy on `main` with no automerge section; draft adds rule `ordinary` with `"acceptance": "rule"`, a non-maintainer executing actor E, author A, reviewers `[R]` and paths `src/`, `tests/`, `docs/`. `hn policy check --base main --file .hn/policy.json` exits 0 and prints, as full fingerprints, `automerge rule ordinary enabled: added` (confirm existing text), the actors and authors added lines, the paths, `automerge rule ordinary acceptance: added (rule)` and `automerge rule ordinary reviewers added: <R>`.
2. **Change table**: one case per row of the T008 table, including `"maintainers"` gained explicitly (`unchanged (maintainers)`) and `maintainers -> rule`.
3. **Order**: two rules `b` and `a` in the draft; assert the `a` lines print before the `b` lines and that the acceptance and reviewer lines follow each rule's paths lines.
4. **Show**: commit the standing policy to `main` and run `hn policy show`; assert the acceptance line, `Executing actors:`, the counted-reviewer block, and for a second rule without `acceptance` or `reviewers`, `Acceptance: maintainers (maintainer acceptance required)`, `Executing maintainers:` and `Counted reviewers: all trusted reviewers`.
5. **Unchanged output**: for a policy without the new fields, run the same commands with the pre-existing `policy_commands_test.go` expectations in mind; existing tests already cover it, so run them and record the result.

### Subtask T010: Governance and protocol guides for the rule fields, with both policy examples

**Purpose**: document the fields where policy is documented, with executed examples (HN-005, FR-023).

**Steps**:

1. In `docs/governance-v0.md`, under "Optional check protection and automerge", add the subsection "Standing acceptance rule fields" after "Rule-based native automerge". Explain in plain sentences: what `acceptance` and `reviewers` mean; that absence keeps today's behavior; that a rule with acceptance mode `rule` is the standing acceptance for every proposal that satisfies it, granted once by the maintainer-accepted amendment that adopted it; that executing actors of such a rule need not be maintainers; the requirement of at least one approval; why a standing rule cannot cover `.hn` or overlap protected-check paths (with the overlap definition of the contract); and that older clients reject a policy that contains either field, even `"acceptance": "maintainers"`.
2. Add `<!-- example: standing-policy-refusals -->` followed by a fenced block that shows, for contract rows 4 to 10, the draft fragment and the exact refusal message, with placeholder fingerprints in the form the guide already uses. Add `TestStandingPolicyDocumentedRefusals` in `standing_policy_test.go`; its source contains the exact marker string (the guard `TestServeQualityDocumentedExamplesAreExecuted` looks for it); it reads the block after the marker, substitutes real fingerprints, runs each draft through `hn policy check` with WP01's helpers and compares each refusal with the documented message.
3. Add a paragraph "Inspecting a standing rule" that explains what `hn policy show` and `hn policy check` print for a standing rule and why a maintainer reads the acceptance and reviewer lines before accepting an amendment. Add `<!-- example: standing-policy-rule -->` followed by the full rule JSON (inside a complete policy with placeholder fingerprints) and the expected `hn policy check` lines. Add `TestStandingPolicyDocumentedRule`, containing the exact marker string, which extracts the JSON and the expected lines, substitutes real fingerprints, runs `hn policy check --base main --file .hn/policy.json` and asserts every documented line appears.
4. In `docs/protocol-v0.md`, "Optional policy rules": replace the sentence that says a rule "restricts executing maintainers" with text that covers both acceptance modes, and add one paragraph on `acceptance` and `reviewers` that links to the governance guide section of step 1. Keep the protocol text short; the governance guide holds the detail. Do not touch the `proposal.merged` paragraphs; WP07 documents rule merge facts.
5. Style: plain direct sentences, descriptive headings, no em dashes, no rhetorical contrasts.

## Test Strategy

Black-box tests through the built binary plus the unchanged existing policy-command tests. Run `go test -race -run 'TestStandingPolicy|TestPolicy' ./...`, then the full gate.

## Definition of Done

- [ ] T007 to T010 complete; full gate passes.
- [ ] **FR-007**: `hn policy show` prints acceptance mode and full counted-reviewer list per rule; `hn policy check` reports acceptance and reviewer changes per rule in sorted rule order.
- [ ] **US2-1**: the comparison lists the added rule with acceptance mode, executing actors, authors, paths, target branch and reviewers as full identities.
- [ ] Every pre-existing output line is unchanged and every pre-existing test passes with no assertion change.
- [ ] Governance section and protocol paragraph written; `standing-policy-refusals` and `standing-policy-rule` executed by tests that contain their markers.
- [ ] **FR-023 (policy fields)**: the documented examples match the CLI output.

## Risks

- Existing tests may assert the absence of certain lines with negative substring checks. Search `policy_commands_test.go` and `policy_rules_acceptance_test.go` for `!strings.Contains` on policy output before adding lines.

## Reviewer Guidance

- Run `hn policy show` on a pre-existing fixture before and after; only the new acceptance and counted-reviewer lines may differ.
- Confirm "Executing maintainers:" still prints for maintainer rules.

## Activity Log

- 2026-10-26T06:54:14Z – system – Prompt generated via /spec-kitty.tasks
