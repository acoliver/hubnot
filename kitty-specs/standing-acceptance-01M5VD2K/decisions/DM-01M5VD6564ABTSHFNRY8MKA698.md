# Decision Moment `01M5VD6564ABTSHFNRY8MKA698`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `specify`
- **Slot key:** `specify.protocol.post-merge-vetoes`
- **Input key:** `rule_merge_veto_scope`
- **Status:** `resolved`
- **Created:** `2026-10-26T06:08:27.332441+00:00`
- **Resolved:** `2026-10-26T06:09:16.846584+00:00`
- **Resolved by:** `llxprt`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

Do validators of a signed rule merge fact consider maintainer rejections, change requests or failures that are not part of its evidence?

## Options

- No; they judge the signed fact and its listed evidence, as acceptance merge facts are judged today; vetoes block execution and server advancement
- Yes; reject the fact if any veto exists anywhere
- Other

## Final answer

OQ-8: Validators judge a rule merge fact only by its signed content, the base policy, the candidate's code and its listed evidence, the same way acceptance merge facts are judged today. They do not look for maintainer rejections, change requests or failures outside the evidence, because signed facts across chains have no delivery-order winner. Those vetoes block execution and hn serve advancement.

## Rationale

Agent-selected default under the maintainer's delegated non-interactive instruction, chosen for consistency with the Hubnot charter (fail closed on hostile input, immutable signed history, trust only from explicit policy) and the existing automerge and protected-check behavior. Recorded in spec.md as an open question for maintainer confirmation at spec review.

## Change log

- `2026-10-26T06:08:27.332441+00:00` — opened
- `2026-10-26T06:09:16.846584+00:00` — resolved (final_answer="OQ-8: Validators judge a rule merge fact only by its signed content, the base policy, the candidate's code and its listed evidence, the same way acceptance merge facts are judged today. They do not look for maintainer rejections, change requests or failures outside the evidence, because signed facts across chains have no delivery-order winner. Those vetoes block execution and hn serve advancement.")
