# Decision Moment `01M5VD5Z139R84BBXZM0QXGHJ1`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `specify`
- **Slot key:** `specify.protocol.merge-fact-shape`
- **Input key:** `rule_merge_fact_shape`
- **Status:** `resolved`
- **Created:** `2026-10-26T06:08:21.027470+00:00`
- **Resolved:** `2026-10-26T06:09:10.781514+00:00`
- **Resolved by:** `llxprt`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

What is the exact shape of a rule merge fact and how strict is its evidence list?

## Options

- Optional rule string on proposal.merged only; policy stays the base digest; evidence lists exactly the qualifying approvals and passing run results, one per counting actor; any other item invalidates the fact; maintainers-mode facts unchanged
- Lenient evidence that ignores non-qualifying items
- A new event kind for rule merges
- Other

## Final answer

OQ-5: proposal.merged gains an optional rule string naming the automerge rule; the field is rejected on every other event kind and must be a valid rule name. policy remains the base policy digest. evidence lists exactly the qualifying approval review IDs and the passing run.result IDs for this candidate, with at most one approval per counting reviewer and at most one result per trusted runner per pipeline. Any other or non-qualifying item invalidates the fact. Merges executed under acceptance maintainers keep today's fact with no rule field and acceptance decision evidence. No new event kind.

## Rationale

Agent-selected default under the maintainer's delegated non-interactive instruction, chosen for consistency with the Hubnot charter (fail closed on hostile input, immutable signed history, trust only from explicit policy) and the existing automerge and protected-check behavior. Recorded in spec.md as an open question for maintainer confirmation at spec review.

## Change log

- `2026-10-26T06:08:21.027470+00:00` — opened
- `2026-10-26T06:09:10.781514+00:00` — resolved (final_answer="OQ-5: proposal.merged gains an optional rule string naming the automerge rule; the field is rejected on every other event kind and must be a valid rule name. policy remains the base policy digest. evidence lists exactly the qualifying approval review IDs and the passing run.result IDs for this candidate, with at most one approval per counting reviewer and at most one result per trusted runner per pipeline. Any other or non-qualifying item invalidates the fact. Merges executed under acceptance maintainers keep today's fact with no rule field and acceptance decision evidence. No new event kind.")
