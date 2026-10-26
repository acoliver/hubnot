# Decision Moment `01M5VD630G94HDM2WPJ00NM5ED`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `specify`
- **Slot key:** `specify.serve.rule-advancement`
- **Input key:** `serve_rule_advancement`
- **Status:** `resolved`
- **Created:** `2026-10-26T06:08:25.104327+00:00`
- **Resolved:** `2026-10-26T06:09:14.910709+00:00`
- **Resolved by:** `llxprt`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

How does hn serve admit a protected-branch update under standing acceptance?

## Options

- Only through an advancement request that names the rule; the server evaluates the named rule itself, including current trusted change requests and failures it holds; the requester must be an executing actor of that rule; direct pushes still need maintainer acceptances
- Also admit direct pushes that some rule would allow
- Keep the server unaware of rules and trust the client
- Other

## Final answer

OQ-7: hn serve admits a protected-branch update under standing acceptance only through an advancement request that names the rule. The server evaluates the named rule from the base policy and the facts it holds, including current trusted change requests, current trusted pipeline failures and maintainer rejections, in addition to its existing protected-branch checks. The requesting mapped actor must be an executing actor of that rule, in place of the maintainer requirement, for that request only. A direct push or an advancement request without a rule still requires maintainer acceptances. New refusals use new stable reason codes.

## Rationale

Agent-selected default under the maintainer's delegated non-interactive instruction, chosen for consistency with the Hubnot charter (fail closed on hostile input, immutable signed history, trust only from explicit policy) and the existing automerge and protected-check behavior. Recorded in spec.md as an open question for maintainer confirmation at spec review.

## Change log

- `2026-10-26T06:08:25.104327+00:00` — opened
- `2026-10-26T06:09:14.910709+00:00` — resolved (final_answer="OQ-7: hn serve admits a protected-branch update under standing acceptance only through an advancement request that names the rule. The server evaluates the named rule from the base policy and the facts it holds, including current trusted change requests, current trusted pipeline failures and maintainer rejections, in addition to its existing protected-branch checks. The requesting mapped actor must be an executing actor of that rule, in place of the maintainer requirement, for that request only. A direct push or an advancement request without a rule still requires maintainer acceptances. New refusals use new stable reason codes.")
