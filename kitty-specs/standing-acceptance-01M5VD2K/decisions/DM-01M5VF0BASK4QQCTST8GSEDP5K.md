# Decision Moment `01M5VF0BASK4QQCTST8GSEDP5K`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `plan`
- **Slot key:** `plan.serve.wire-form`
- **Input key:** `serve_rule_wire`
- **Status:** `resolved`
- **Created:** `2026-10-26T06:40:14.169743+00:00`
- **Resolved:** `2026-10-26T06:40:16.720938+00:00`
- **Resolved by:** `llxprt`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

How does an hn serve advancement request name the rule?

## Options

- Third push option hn-rule=NAME, valid only with hn-advance
- Encode the rule into hn-advance
- New ref namespace
- Other

## Final answer

Add a third push option, hn-rule=NAME. It is valid only together with hn-advance, may appear at most once, and NAME must match the rule-name grammar. servePushOptionLimit becomes 3 and servePushOptionSentence names all three options. An older server refuses the unknown option with unsupported-command, which fails closed. hn merge --remote never sends hn-rule. hn automerge --rule NAME --execute --remote sends it.

## Rationale

Plan-phase decision made by the planning agent under the maintainer's delegated non-interactive instruction (spec confirmed, OQ-1..11 accepted). The spec assumes the existing push-option channel, and the hn serve parser already refuses unknown options. Existing push-option tests compare against the servePushOptionSentence constant and use counts that stay refused, so no frozen assertion changes.

## Change log

- `2026-10-26T06:40:14.169743+00:00` — opened
- `2026-10-26T06:40:16.720938+00:00` — resolved (final_answer="Add a third push option, hn-rule=NAME. It is valid only together with hn-advance, may appear at most once, and NAME must match the rule-name grammar. servePushOptionLimit becomes 3 and servePushOptionSentence names all three options. An older server refuses the unknown option with unsupported-command, which fails closed. hn merge --remote never sends hn-rule. hn automerge --rule NAME --execute --remote sends it.")
