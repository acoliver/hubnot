# Contract: automerge rule fields, validation and inspection

Covers FR-001 to FR-007 and FR-022 (policy part). The policy version stays `hn.policy/0`. A policy without `acceptance` and `reviewers` parses, validates and prints exactly as before, apart from the additive output lines in [Inspection output](#inspection-output).

## Schema of one rule

```json
{
  "name": "ordinary",
  "targetBranch": "main",
  "acceptance": "rule",
  "actors": ["<executor fingerprint>"],
  "authors": ["<author fingerprint>"],
  "reviewers": ["<gpt-reviewer fingerprint>"],
  "paths": ["src/", "tests/", "docs/"]
}
```

| Field | Required | Values |
|----|----|----|
| `acceptance` | no | `"maintainers"` or `"rule"`. Absent equals `"maintainers"` |
| `reviewers` | no | Array of 1 to 64 distinct full actor fingerprints, each listed in `proposals.trustedReviewers`, with at least `proposals.requiredApprovals` entries |

Every other field keeps its existing meaning and bounds.

## Decoding

`AutomergeAcceptance` and `AutomergeReviewers` are named types whose `UnmarshalJSON` never fails. Each records that the key was present and keeps the raw JSON value, including `null`. The existing strict decoder (`DisallowUnknownFields`, `ensureJSONEnd`) is unchanged. All refusals happen in `validateOptionalPolicy`, where the rule name is known.

## Validation order and refusal messages

Validation runs inside `validateOptionalPolicy` for each rule in document order, after the existing name, target-branch, actor-list and path checks. The first failure is returned. `<rule>` is the rule name, quoted with `%q`.

| Order | Condition | Message |
|----|----|----|
| 1 | `acceptance` present and `null`, not a string, or a string other than `maintainers` or `rule` | `automerge rule <rule> acceptance must be "maintainers" or "rule"` |
| 2 | `reviewers` present and `null` or not an array of strings | `automerge rule <rule> reviewers must be an array of actor fingerprints` |
| 3 | Acceptance mode `maintainers` and an actor is not a maintainer | `automerge rule <rule> actor <actor> is not a maintainer` (existing text) |
| 4 | Acceptance mode `rule`, `proposals.requiredApprovals` is 0 and the policy defines no pipeline (decision `01M66YE8SNSPCYB1ER757D9251`, superseding the refusal of every rule with 0) | `automerge rule <rule> has acceptance "rule" and proposals.requiredApprovals 0, which requires at least one pipeline` |
| 5 | `reviewers` present with 0 or more than 64 entries | `automerge rule <rule> requires 1 to 64 reviewers when reviewers is present` |
| 6 | `reviewers` contains an invalid or duplicate actor | `automerge reviewers contains invalid actor ...` or `automerge reviewers contains duplicate actor ...` (from `validateActorList`) |
| 7 | A reviewer is not in `proposals.trustedReviewers` | `automerge rule <rule> reviewer <actor> is not a trusted reviewer` |
| 8 | Fewer reviewers than `proposals.requiredApprovals` | `automerge rule <rule> lists N reviewers, fewer than proposals.requiredApprovals (M)` |
| 9 | Acceptance mode `rule` and a path selector is `.hn` or `.hn/`, or starts with `.hn/` | `automerge rule <rule> path <path> covers .hn, which a rule with acceptance "rule" cannot cover` |
| 10 | Acceptance mode `rule` and a path selector overlaps a `protectedChecks.paths` selector | `automerge rule <rule> path <path> overlaps protected check path <protected>` |

### Overlap definition

Let `literal(s)` be `s` with one trailing `/` removed. A rule selector `r` overlaps a protected selector `p` when any of these holds:

- `literal(r) == literal(p)`
- `literal(p)` starts with `literal(r) + "/"` (the rule covers an ancestor of the protected path)
- `literal(r)` starts with `literal(p) + "/"` (the rule covers a descendant of the protected path)

Examples with `protectedChecks.paths` = `["scripts/lint.sh"]`: `scripts/` overlaps, `scripts/lint.sh` overlaps, `scripts/lint.sh.bak` does not overlap, `script/` does not overlap. With `protectedChecks.paths` = `["scripts/"]`: `scripts/lint/run.sh` overlaps, `scripts/` overlaps, `scriptsx/` does not overlap.

Runtime refusals of policy amendments and protected-check changes stay in both acceptance modes (`inspectAutomerge` blockers "policy amendments require manual merge" and "protected check changes require manual merge").

## Inspection output

### hn policy show

For each rule, after the existing `Rule NAME; target branch BRANCH:` line:

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

For acceptance mode `maintainers`, the acceptance line reads `  Acceptance: maintainers (maintainer acceptance required)` and the actor heading keeps its existing text, `  Executing maintainers:`. When `reviewers` is absent, the counted-reviewer block reads `  Counted reviewers: all trusted reviewers`. Actors print as full fingerprints in sorted order, as today.

### hn policy check

For each rule name in the union of base and proposed rules, after the existing per-rule lines, in this order:

```text
  automerge rule ordinary acceptance: added (rule)
  automerge rule ordinary reviewers added: <gpt-reviewer fingerprint>
  automerge rule ordinary reviewers removed: none
```

The acceptance value is one of `unchanged (maintainers)`, `unchanged (rule)`, `added (rule)`, `added (maintainers)`, `removed (rule)`, `removed (maintainers)` or `changed (maintainers -> rule)`. Here "added" and "removed" refer to the rule. An absent field compares as `maintainers`, so a rule that gains `"acceptance": "maintainers"` reports `unchanged (maintainers)`. Reviewer lists use the existing `actorChanges` format, and an absent field compares as an empty list. Rule names are processed in sorted order, as today.

## Tested examples

`docs/governance-v0.md` carries these examples, each run by `standing_quality_test.go`:

- `<!-- example: standing-policy-rule -->`: the rule JSON above inside a full policy; `hn policy check --base main --file .hn/policy.json` succeeds and prints the lines above.
- `<!-- example: standing-policy-refusals -->`: one refused draft per row 4 to 10, with the expected message.
