# Decision Moment `01M5VD610AQPYQAW0N79YBSKK1`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `specify`
- **Slot key:** `specify.governance.destination-policy`
- **Input key:** `rule_destination_policy`
- **Status:** `resolved`
- **Created:** `2026-10-26T06:08:23.050311+00:00`
- **Resolved:** `2026-10-26T06:09:12.787350+00:00`
- **Resolved by:** `llxprt`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

Which policy governs a rule merge when the target or destination branch policy differs from the proposal base policy?

## Options

- Standing acceptance applies only when the target policy digest equals the base policy digest, locally and on hn serve
- Evaluate the rule under both policies separately
- Base policy only
- Other

## Final answer

OQ-6: Standing acceptance applies only when the policy at the actual target (local automerge) or at the destination tip (hn serve) has the same digest as the candidate's base policy. Otherwise the update is refused and the candidate must be revised. A policy amendment that removes or narrows a rule or an executing actor therefore stops pending rule merges of older-base candidates.

## Rationale

Agent-selected default under the maintainer's delegated non-interactive instruction, chosen for consistency with the Hubnot charter (fail closed on hostile input, immutable signed history, trust only from explicit policy) and the existing automerge and protected-check behavior. Recorded in spec.md as an open question for maintainer confirmation at spec review.

## Change log

- `2026-10-26T06:08:23.050311+00:00` — opened
- `2026-10-26T06:09:12.787350+00:00` — resolved (final_answer="OQ-6: Standing acceptance applies only when the policy at the actual target (local automerge) or at the destination tip (hn serve) has the same digest as the candidate's base policy. Otherwise the update is refused and the candidate must be revised. A policy amendment that removes or narrows a rule or an executing actor therefore stops pending rule merges of older-base candidates.")
