# Decision Moment `01M5VD5RSPR0WPMH9K2BDYXNAS`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `specify`
- **Slot key:** `specify.policy.author-approval`
- **Input key:** `rule_author_approval`
- **Status:** `resolved`
- **Created:** `2026-10-26T06:08:14.646961+00:00`
- **Resolved:** `2026-10-26T06:09:04.682284+00:00`
- **Resolved by:** `llxprt`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

Under acceptance rule, does a proposal author's own approval count toward the rule when allowAuthorApproval is true?

## Options

- Never counts under acceptance rule
- Follows allowAuthorApproval as today
- Other

## Final answer

OQ-2: Under acceptance rule, a proposal author's own approval never counts toward the rule, even when proposals.allowAuthorApproval is true. This matches how designated protected-check approvals already exclude the author, and it is what makes cross-family review meaningful. Under acceptance maintainers, allowAuthorApproval keeps today's meaning.

## Rationale

Agent-selected default under the maintainer's delegated non-interactive instruction, chosen for consistency with the Hubnot charter (fail closed on hostile input, immutable signed history, trust only from explicit policy) and the existing automerge and protected-check behavior. Recorded in spec.md as an open question for maintainer confirmation at spec review.

## Change log

- `2026-10-26T06:08:14.646961+00:00` — opened
- `2026-10-26T06:09:04.682284+00:00` — resolved (final_answer="OQ-2: Under acceptance rule, a proposal author's own approval never counts toward the rule, even when proposals.allowAuthorApproval is true. This matches how designated protected-check approvals already exclude the author, and it is what makes cross-family review meaningful. Under acceptance maintainers, allowAuthorApproval keeps today's meaning.")
