# Decision Moment `01M5VD5X2W8DHPC3TK3JF6VXQ0`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `specify`
- **Slot key:** `specify.policy.rule-path-safety`
- **Input key:** `rule_path_safety`
- **Status:** `resolved`
- **Created:** `2026-10-26T06:08:19.036958+00:00`
- **Resolved:** `2026-10-26T06:09:08.771464+00:00`
- **Resolved by:** `llxprt`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

Which rule path selectors does policy validation forbid for acceptance rule?

## Options

- Any selector equal to, inside, or an ancestor of .hn, and any selector overlapping a protectedChecks path in either direction
- Only selectors inside .hn/
- Runtime refusal only, no validation
- Other

## Final answer

OQ-4: For acceptance rule, policy validation rejects a rule path selector that equals .hn or .hn/, lies inside .hn/, or would match .hn itself, and rejects a selector that overlaps any protectedChecks.paths selector in either direction (equal, ancestor or descendant). The runtime refusals of policy amendments and protected-check changes remain for both modes.

## Rationale

Agent-selected default under the maintainer's delegated non-interactive instruction, chosen for consistency with the Hubnot charter (fail closed on hostile input, immutable signed history, trust only from explicit policy) and the existing automerge and protected-check behavior. Recorded in spec.md as an open question for maintainer confirmation at spec review.

## Change log

- `2026-10-26T06:08:19.036958+00:00` — opened
- `2026-10-26T06:09:08.771464+00:00` — resolved (final_answer="OQ-4: For acceptance rule, policy validation rejects a rule path selector that equals .hn or .hn/, lies inside .hn/, or would match .hn itself, and rejects a selector that overlaps any protectedChecks.paths selector in either direction (equal, ancestor or descendant). The runtime refusals of policy amendments and protected-check changes remain for both modes.")
