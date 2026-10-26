# Decision Moment `01M5VD69EDJJC7BGGZCQRD5R6Q`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `specify`
- **Slot key:** `specify.protocol.missing-code-objects`
- **Input key:** `rule_merge_missing_code`
- **Status:** `resolved`
- **Created:** `2026-10-26T06:08:31.693469+00:00`
- **Resolved:** `2026-10-26T06:09:21.054855+00:00`
- **Resolved by:** `llxprt`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

What happens when a validator lacks the base or head commit needed to derive a rule merge fact changed paths?

## Options

- Fail closed: report a missing dependency or shallow refusal with recovery guidance and never treat a missing side as an empty diff
- Skip path checks when objects are absent
- Other

## Final answer

OQ-10: Fail closed. Replication reports a missing dependency naming the absent commit and how to select it; shallow verification refuses with its existing recovery guidance; local projection and hn serve refuse the fact. A missing base or head is never treated as an empty or partial changed-path list.

## Rationale

Agent-selected default under the maintainer's delegated non-interactive instruction, chosen for consistency with the Hubnot charter (fail closed on hostile input, immutable signed history, trust only from explicit policy) and the existing automerge and protected-check behavior. Recorded in spec.md as an open question for maintainer confirmation at spec review.

## Change log

- `2026-10-26T06:08:31.693469+00:00` — opened
- `2026-10-26T06:09:21.054855+00:00` — resolved (final_answer="OQ-10: Fail closed. Replication reports a missing dependency naming the absent commit and how to select it; shallow verification refuses with its existing recovery guidance; local projection and hn serve refuse the fact. A missing base or head is never treated as an empty or partial changed-path list.")
