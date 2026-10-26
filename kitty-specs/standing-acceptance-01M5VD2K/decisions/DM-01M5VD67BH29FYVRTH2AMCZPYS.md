# Decision Moment `01M5VD67BH29FYVRTH2AMCZPYS`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `specify`
- **Slot key:** `specify.recovery.interrupted-rule-merge`
- **Input key:** `rule_merge_recovery`
- **Status:** `resolved`
- **Created:** `2026-10-26T06:08:29.553142+00:00`
- **Resolved:** `2026-10-26T06:09:18.913260+00:00`
- **Resolved by:** `llxprt`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

How does an executing actor recover when the code merged or the branch advanced but the rule merge fact was not recorded?

## Options

- Rerun the same hn automerge --execute command, which re-verifies and records the missing rule fact exactly once under the existing repair conditions; hn merge repair stays maintainer-only
- Require a maintainer to accept and run hn merge repair
- No recovery path
- Other

## Final answer

OQ-9: An executing actor recovers by rerunning the same hn automerge PROPOSAL --rule NAME --execute command (with the same --remote when used). When the target already holds the merge under the existing repair conditions (locally, exactly one first-parent merge commit since the signed base directly names the head; on hn serve, the hosted branch tip equals the head), it re-verifies the rule and records the missing rule merge fact exactly once, without another code merge or advancement. hn merge repair stays maintainer-only and records an acceptance fact.

## Rationale

Agent-selected default under the maintainer's delegated non-interactive instruction, chosen for consistency with the Hubnot charter (fail closed on hostile input, immutable signed history, trust only from explicit policy) and the existing automerge and protected-check behavior. Recorded in spec.md as an open question for maintainer confirmation at spec review.

## Change log

- `2026-10-26T06:08:29.553142+00:00` — opened
- `2026-10-26T06:09:18.913260+00:00` — resolved (final_answer="OQ-9: An executing actor recovers by rerunning the same hn automerge PROPOSAL --rule NAME --execute command (with the same --remote when used). When the target already holds the merge under the existing repair conditions (locally, exactly one first-parent merge commit since the signed base directly names the head; on hn serve, the hosted branch tip equals the head), it re-verifies the rule and records the missing rule merge fact exactly once, without another code merge or advancement. hn merge repair stays maintainer-only and records an acceptance fact.")
