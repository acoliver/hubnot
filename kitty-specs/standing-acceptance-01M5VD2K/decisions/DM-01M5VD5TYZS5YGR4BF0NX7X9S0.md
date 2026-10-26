# Decision Moment `01M5VD5TYZS5YGR4BF0NX7X9S0`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `specify`
- **Slot key:** `specify.policy.rule-reviewers`
- **Input key:** `rule_reviewers_semantics`
- **Status:** `resolved`
- **Created:** `2026-10-26T06:08:16.863411+00:00`
- **Resolved:** `2026-10-26T06:09:06.785398+00:00`
- **Resolved by:** `llxprt`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

What are the exact forms and semantics of the rule fields acceptance and reviewers (explicit default, size bounds, threshold, vetoes, both modes)?

## Options

- Explicit maintainers allowed; reviewers 1-64 subset of trustedReviewers with at least requiredApprovals members; counts only for approvals; any trusted change request still blocks; applies in both modes
- reviewers also narrows which change requests block
- reviewers only allowed with acceptance rule
- Other

## Final answer

OQ-3: acceptance accepts exactly the strings maintainers and rule; absence and an explicit maintainers are equivalent; null, empty and any other value are rejected. reviewers is optional; when present it lists 1-64 distinct full actor fingerprints, every one also in proposals.trustedReviewers, with at least proposals.requiredApprovals members. It narrows only which approvals count toward the rule. A current change request from any proposals.trustedReviewers actor still blocks. reviewers is allowed in both modes; under maintainers mode it is an execution-time restriction and the merge fact stays the existing acceptance fact.

## Rationale

Agent-selected default under the maintainer's delegated non-interactive instruction, chosen for consistency with the Hubnot charter (fail closed on hostile input, immutable signed history, trust only from explicit policy) and the existing automerge and protected-check behavior. Recorded in spec.md as an open question for maintainer confirmation at spec review.

## Change log

- `2026-10-26T06:08:16.863411+00:00` — opened
- `2026-10-26T06:09:06.785398+00:00` — resolved (final_answer="OQ-3: acceptance accepts exactly the strings maintainers and rule; absence and an explicit maintainers are equivalent; null, empty and any other value are rejected. reviewers is optional; when present it lists 1-64 distinct full actor fingerprints, every one also in proposals.trustedReviewers, with at least proposals.requiredApprovals members. It narrows only which approvals count toward the rule. A current change request from any proposals.trustedReviewers actor still blocks. reviewers is allowed in both modes; under maintainers mode it is an execution-time restriction and the merge fact stays the existing acceptance fact.")
