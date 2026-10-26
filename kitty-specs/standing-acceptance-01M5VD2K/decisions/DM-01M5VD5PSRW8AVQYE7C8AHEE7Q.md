# Decision Moment `01M5VD5PSRW8AVQYE7C8AHEE7Q`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `specify`
- **Slot key:** `specify.policy.rule-minimum-review`
- **Input key:** `rule_minimum_review`
- **Status:** `resolved`
- **Created:** `2026-10-26T06:08:12.600073+00:00`
- **Resolved:** `2026-10-26T06:09:02.483840+00:00`
- **Resolved by:** `llxprt`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

Must a rule with acceptance rule require at least one independent approval, given that proposals.requiredApprovals may be 0?

## Options

- Reject acceptance rule when proposals.requiredApprovals is 0
- Allow zero approvals under acceptance rule
- Add a rule-level approval threshold
- Other

## Final answer

OQ-1: Policy validation rejects any automerge rule with acceptance rule when proposals.requiredApprovals is 0. A standing acceptance replaces the maintainer's per-proposal judgment, so at least one counting trusted approval for the exact candidate must remain. No rule-level threshold is added; the ordinary requiredApprovals applies.

## Rationale

Agent-selected default under the maintainer's delegated non-interactive instruction, chosen for consistency with the Hubnot charter (fail closed on hostile input, immutable signed history, trust only from explicit policy) and the existing automerge and protected-check behavior. Recorded in spec.md as an open question for maintainer confirmation at spec review.

## Change log

- `2026-10-26T06:08:12.600073+00:00` — opened
- `2026-10-26T06:09:02.483840+00:00` — resolved (final_answer="OQ-1: Policy validation rejects any automerge rule with acceptance rule when proposals.requiredApprovals is 0. A standing acceptance replaces the maintainer's per-proposal judgment, so at least one counting trusted approval for the exact candidate must remain. No rule-level threshold is added; the ordinary requiredApprovals applies.")
