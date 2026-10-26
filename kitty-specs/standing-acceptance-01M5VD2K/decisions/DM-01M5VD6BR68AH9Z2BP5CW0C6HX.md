# Decision Moment `01M5VD6BR68AH9Z2BP5CW0C6HX`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `specify`
- **Slot key:** `specify.docs.scope`
- **Input key:** `documentation_scope`
- **Status:** `resolved`
- **Created:** `2026-10-26T06:08:34.054805+00:00`
- **Resolved:** `2026-10-26T06:09:23.207969+00:00`
- **Resolved by:** `llxprt`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

Which documents change with this mission?

## Options

- governance-v0.md, protocol-v0.md, serve-v0.md and the trust section of threat-model.md, with tested examples
- Only the three documents the brief names
- Other

## Final answer

OQ-11: governance-v0.md, protocol-v0.md and serve-v0.md change as the brief lists, plus the trust-boundary text of threat-model.md, because the mission moves acceptance authority from maintainers to a policy rule. Examples in the docs are exercised by tests, as serve-v0.md examples already are.

## Rationale

Agent-selected default under the maintainer's delegated non-interactive instruction, chosen for consistency with the Hubnot charter (fail closed on hostile input, immutable signed history, trust only from explicit policy) and the existing automerge and protected-check behavior. Recorded in spec.md as an open question for maintainer confirmation at spec review.

## Change log

- `2026-10-26T06:08:34.054805+00:00` — opened
- `2026-10-26T06:09:23.207969+00:00` — resolved (final_answer="OQ-11: governance-v0.md, protocol-v0.md and serve-v0.md change as the brief lists, plus the trust-boundary text of threat-model.md, because the mission moves acceptance authority from maintainers to a policy rule. Examples in the docs are exercised by tests, as serve-v0.md examples already are.")
