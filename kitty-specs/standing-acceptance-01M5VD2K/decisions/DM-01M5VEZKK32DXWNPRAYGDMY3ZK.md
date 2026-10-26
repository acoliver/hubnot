# Decision Moment `01M5VEZKK32DXWNPRAYGDMY3ZK`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `plan`
- **Slot key:** `plan.protocol.rule-field`
- **Input key:** `rule_field_parsing`
- **Status:** `resolved`
- **Created:** `2026-10-26T06:39:49.859592+00:00`
- **Resolved:** `2026-10-26T06:39:52.585091+00:00`
- **Resolved by:** `llxprt`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

How is the new optional rule field of proposal.merged represented and checked at parse time?

## Options

- Optional Event.Rule string with omitempty plus a top-level key scan
- Optional Event.Rule string with omitempty only
- New event kind
- Other

## Final answer

Event gains Rule string `json:"rule,omitempty"` after Evidence, so every existing payload re-encodes byte-identically. validateEventContent rejects a non-empty Rule on every kind other than proposal.merged and requires the rule-name grammar (validPipelineName) on proposal.merged. verifyEvent also walks the top-level keys of each non-resource payload. It rejects any key that equals "rule" under case folding unless the key is exactly "rule", appears once, sits on proposal.merged and holds a non-empty JSON string. That catches rule null, rule "" and case variants that Go's decoder would otherwise drop or fold. No new event kind and no protocol version change.

## Rationale

Plan-phase decision made by the planning agent under the maintainer's delegated non-interactive instruction (spec confirmed, OQ-1..11 accepted). OQ-5 and FR-010 require an optional field on proposal.merged only, and parse-time rejection of empty, invalid or misplaced rule values. encoding/json treats null and "" as absent and matches keys case-insensitively, so without the key scan the CLI could not tell those payloads apart. The scan is one token walk per event, which costs less than the ed25519 verification each event already gets.

## Change log

- `2026-10-26T06:39:49.859592+00:00` — opened
- `2026-10-26T06:39:52.585091+00:00` — resolved (final_answer="Event gains Rule string `json:"rule,omitempty"` after Evidence, so every existing payload re-encodes byte-identically. validateEventContent rejects a non-empty Rule on every kind other than proposal.merged and requires the rule-name grammar (validPipelineName) on proposal.merged. verifyEvent also walks the top-level keys of each non-resource payload. It rejects any key that equals "rule" under case folding unless the key is exactly "rule", appears once, sits on proposal.merged and holds a non-empty JSON string. That catches rule null, rule "" and case variants that Go's decoder would otherwise drop or fold. No new event kind and no protocol version change.")
