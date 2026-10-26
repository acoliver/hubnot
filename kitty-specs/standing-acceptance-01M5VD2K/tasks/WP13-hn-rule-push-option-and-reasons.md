---
work_package_id: WP13
title: hn-rule push option and rule reason codes
dependencies:
- WP03
- WP07
requirement_refs:
- FR-016
- FR-023
planning_base_branch: feat/standing-acceptance
merge_target_branch: feat/standing-acceptance
branch_strategy: Planning artifacts for this mission were generated on feat/standing-acceptance. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into feat/standing-acceptance unless the human explicitly redirects the landing branch.
subtasks:
- T059
- T060
- T061
- T062
- T063
phase: Phase 5 - hn serve rule advancement
history:
- !!omap
  - timestamp: '2026-10-26T06:54:14Z'
  - agent: system
  - action: Prompt generated via /spec-kitty.tasks
agent_profile: implementer-ivan
authoritative_surface: serve_hooks.go
create_intent:
- standing_serve_test.go
execution_mode: code_change
owned_files:
- serve_wire.go
- serve_hooks.go
- serve_verdict.go
- serve_reasons.go
- standing_serve_test.go
- docs/protocol-v0.md
role: implementer
tags: []
tracker_refs: []
---

# WP13: hn-rule push option and rule reason codes

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter, and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `codex`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## Objective

Add the wire vocabulary of rule advancement ([contracts/serve-rule-advancement.md](../contracts/serve-rule-advancement.md) "Push options" and "Reason codes"; FR-016; decisions `01M5VF0BASK4QQCTST8GSEDP5K` and `01M5VF0M0JGZKDM1MAESM0HCZ2`):

- a third push option, `hn-rule=NAME`, valid only together with `hn-advance`, at most once, with the rule-name grammar; at most three options in total;
- three reason codes, `advance-not-rule-executor`, `protected-rule-not-satisfied` and `protected-rule-veto`, in a second catalogue `serveRuleReasonCatalogue` that every lookup consults, documented in a second table.

This WP changes no verdict. WP14 uses `options.Rule` in the protected verdict, and WP15 sends the option from the client. Until WP14 lands, a push with `hn-rule` parses and the verdict ignores it, so the existing maintainer checks still apply; do not ship a client that sends it before WP14 (no client sends it in this WP).

## Context

- Read the contract sections named above; plan "Complexity Tracking"; [research.md](../research.md) findings 13, 15 and 16.
- WP07 is a dependency only because both WPs edit `docs/protocol-v0.md` (different sections); it orders the edits. Technically this WP needs only WP03.
- Start with `spec-kitty agent action implement WP13 --agent <name> --mission standing-acceptance-01M5VD2K`.
- Existing code: `servePushOptionAdvance`, `servePushOptionExpected` (`serve_wire.go`); `servePushOptionLimit`, `servePushOptionSentence`, `parseServePushOptions` (`serve_hooks.go` lines 48 to 51 and 664 to 710); `servePushOptions` (`serve_verdict.go` line 69); `serveReason`, `serveReasonCatalogue`, `serveReasonLookup`, `serveReasonKnown`, `serveReasonDefaultNext` (`serve_reasons.go`); `validPipelineName` (`ci.go`).
- Frozen tests that must pass unchanged: `TestServeReasonsCatalogueOrderAndLookups` (pins `serveReasonCatalogue` with `reflect.DeepEqual`), `TestServeReasonsDocsTableMatchesCatalogue` (compares the **first** reason table under "hn serve wire surface" in `docs/protocol-v0.md` with `serveReasonCatalogue`; its parser stops at the first non-table line), `TestServeReasonsEveryCodeRoundTrips`, `TestServePushOptions` (compares refusals with `servePushOptionSentence`; its three-option case includes an unknown option and stays refused).

## Branch Strategy

- **Planning/base branch at prompt creation**: `feat/standing-acceptance`
- **Final merge target for completed work**: `feat/standing-acceptance`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. Each computed lane gets exactly one worktree, and sequential WPs in the same lane reuse it.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `serve_wire.go` | CHANGE. `servePushOptionRule = "hn-rule"`. |
| `serve_hooks.go` | CHANGE. Limit, sentence, parser. |
| `serve_verdict.go` | CHANGE. `Rule string` on `servePushOptions` only. |
| `serve_reasons.go` | CHANGE. Three constants, `serveRuleReasonCatalogue`, lookup. `serveReasonCatalogue` stays byte-identical. |
| `standing_serve_test.go` | NEW. `TestStandingServeWire...`. WP14 and WP15 add tests later. |
| `docs/protocol-v0.md` | CHANGE. "Push options" and a new "Reason codes for rule advancement" subsection. |

## Subtasks

### Subtask T059: `hn-rule` constant, `servePushOptions.Rule`, limit and sentence

**Steps**:

1. `serve_wire.go`: add `servePushOptionRule = "hn-rule"` next to the two existing names.
2. `serve_verdict.go`: `type servePushOptions struct { Advance, ExpectedTip, Rule string }`.
3. `serve_hooks.go`: `servePushOptionLimit = 3` with the comment updated to name all three options; `servePushOptionSentence = "push options other than hn-advance, hn-expected-tip and hn-rule are not supported."`.

### Subtask T060: `parseServePushOptions` rules for `hn-rule`

**Steps**:

1. New `case servePushOptionRule:` in the switch:
   - already set: `hn-rule appears more than once`;
   - `!validPipelineName(argument)`: `hn-rule value %q is not a valid rule name`;
   - otherwise `options.Rule = argument`.
2. After the loop: `options.Rule != "" && options.Advance == ""`: `hn-rule requires hn-advance`.
3. Every parse error already rejects every ref update with `unsupported-command` and the sentence; keep that path.
4. The existing shape rule (an `hn-advance` push has exactly one ref update, to a protected branch) applies unchanged to a push that also carries `hn-rule`.
5. Error texts use the same style as the existing ones (`%s appears more than once` with the option name); align the three messages above with that style if a test of the existing format suggests it, and record any difference.

### Subtask T061: `serveRuleReasonCatalogue` and lookups

**Steps**:

1. Constants: `serveReasonAdvanceNotRuleExecutor = "advance-not-rule-executor"`, `serveReasonProtectedRuleNotSatisfied = "protected-rule-not-satisfied"`, `serveReasonProtectedRuleVeto = "protected-rule-veto"`.
2. Catalogue (meanings and next-action templates from the contract):

   ```go
   var serveRuleReasonCatalogue = []serveReason{
       {serveReasonAdvanceNotRuleExecutor, "The requester is not an executing actor of the named rule in the base policy", "ask an executing actor of the rule to run hn automerge --remote"},
       {serveReasonProtectedRuleNotSatisfied, "The named rule does not exist, is not a standing rule, or the candidate does not satisfy it", "run hn automerge <candidate> --rule <rule>"},
       {serveReasonProtectedRuleVeto, "A current trusted change request or trusted failure blocks the rule", "resolve the named events, then retry"},
   }
   ```

3. `serveReasonLookup` searches `serveReasonCatalogue` first, then `serveRuleReasonCatalogue`. `serveReasonKnown`, `serveReasonDefaultNext`, the renderer, the strict line parser and the client extractor all go through it; check each caller and the parser to confirm none iterates `serveReasonCatalogue` directly for validation. If one does, route it through the lookup.
4. `serveReasonCatalogue` itself is not edited (frozen assertion).
5. If the `<candidate>` and `<rule>` placeholders need substitution support in the renderer, check how existing templates with `<candidate>` are filled and follow the same pattern; the substitution itself happens in WP14.

### Subtask T062: Push option and catalogue tests, including the docs-table drift test

**Steps** (tests `TestStandingServeWire...` in `standing_serve_test.go`; unit tests on the parser and catalogue):

1. **Parser accepts**: `hn-advance` + `hn-expected-tip` + `hn-rule=ordinary` (three options); `hn-advance` + `hn-rule` (two); order-independent.
2. **Parser refuses**: `hn-rule` twice (count 3 with an advance); `hn-rule=` empty; `hn-rule=bad name`; `hn-rule=.`; a 65-character name; `hn-rule` without `hn-advance`; four options; `hn-rule` with an unknown option. Each error contains the expected message.
3. **Sentence**: the constant names all three options.
4. **Catalogues disjoint**: no code appears in both catalogues; every rule code round-trips through `renderServeRejection` and `parseServeRejectionLine`; `serveReasonKnown` and `serveReasonDefaultNext` work for the three codes.
5. **Docs drift**: a parser for the second table, found under the heading `### Reason codes for rule advancement` inside "hn serve wire surface", compared with `serveRuleReasonCatalogue` using the same backtick-stripping rule as `serveReasonDocsTable` (write a local copy named `standingServeRuleReasonDocsTable`; do not edit `serve_reasons_test.go`).
6. Run the frozen tests listed in Context and record that they pass unchanged.

### Subtask T063: Protocol guide push option and rule reason table

**Steps** in `docs/protocol-v0.md`:

1. "Push options": add the `hn-rule` row (value: rule name, 1 to 64 of `A-Z a-z 0-9 - _ .`, excluding `.` and `..`; rule: names the automerge rule whose standing acceptance the advancement uses; valid only with `hn-advance`). Update the paragraph after the table: each option at most once, at most three options, `hn-rule` requires `hn-advance`, and a server built before this mission refuses `hn-rule` with `unsupported-command`.
2. After the existing "Reason codes" table and its surrounding text, add the subsection `### Reason codes for rule advancement` with one sentence of introduction and a table with the header line `| Code | Meaning | Typical next action |` and the three rows of T061 (commands in backticks). Keep it after the first table so `TestServeReasonsDocsTableMatchesCatalogue` still reads only the first table. Make sure a non-table line (a blank line and the heading) separates the two tables.
3. Style: plain direct sentences, no em dashes.

## Test Strategy

Unit tests for the parser and catalogues, a docs drift test, and the unchanged serve suites. Run `go test -race -run 'TestStandingServeWire|TestServeReasons|TestServePush' ./...`, then the full gate.

## Definition of Done

- [ ] T059 to T063 complete; full gate passes.
- [ ] **FR-016 (wire)**: `hn-rule` parsed with every refusal of the contract table; limit 3; sentence names all three options.
- [ ] The three codes are known to every lookup; `serveReasonCatalogue` and its documented table are unchanged; the second table equals `serveRuleReasonCatalogue`.
- [ ] Every frozen serve test passes with no assertion change.

## Risks

- Any code path that validates codes by ranging over `serveReasonCatalogue` directly would reject the new codes on the client. T061 step 3 audits for it.

## Reviewer Guidance

- `git diff` of `serve_reasons.go` must show no change inside `serveReasonCatalogue`.
- Confirm a push with `hn-rule` and no `hn-advance` is refused before any verdict runs.

## Activity Log

- 2026-10-26T06:54:14Z – system – Prompt generated via /spec-kitty.tasks
