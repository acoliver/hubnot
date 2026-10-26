# Decision Moment `01M5VF0M0JGZKDM1MAESM0HCZ2`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `plan`
- **Slot key:** `plan.serve.reason-catalogue`
- **Input key:** `serve_rule_reason_catalogue`
- **Status:** `resolved`
- **Created:** `2026-10-26T06:40:23.058956+00:00`
- **Resolved:** `2026-10-26T06:40:25.053468+00:00`
- **Resolved by:** `llxprt`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

How are the three new reason codes catalogued and documented, given that an existing test pins the exact catalogue list?

## Options

- Separate rule catalogue and separate documented table
- Append to serveReasonCatalogue
- Reuse existing reason codes
- Other

## Final answer

serveReasonCatalogue stays byte-identical. A new serveRuleReasonCatalogue holds the three codes, and serveReasonLookup consults it, so line parsing, rendering, the push log and the client accept them. docs/protocol-v0.md documents them in a separate table under a new heading, "Reason codes for rule advancement", directly after the existing table. serveReasonDocsTable stops at the first non-table line, so the existing drift test still compares only the original table. A new test compares the new table with serveRuleReasonCatalogue and checks that the two catalogues share no code.

## Rationale

Plan-phase decision made by the planning agent under the maintainer's delegated non-interactive instruction (spec confirmed, OQ-1..11 accepted). TestServeReasonsCatalogueOrderAndLookups pins the exact code list with reflect.DeepEqual, and C-003 forbids editing it. FR-016 names the three codes. This is a deliberate two-list split, recorded under Complexity Tracking in plan.md, and the new test keeps the two lists consistent.

## Change log

- `2026-10-26T06:40:23.058956+00:00` — opened
- `2026-10-26T06:40:25.053468+00:00` — resolved (final_answer="serveReasonCatalogue stays byte-identical. A new serveRuleReasonCatalogue holds the three codes, and serveReasonLookup consults it, so line parsing, rendering, the push log and the client accept them. docs/protocol-v0.md documents them in a separate table under a new heading, "Reason codes for rule advancement", directly after the existing table. serveReasonDocsTable stops at the first non-table line, so the existing drift test still compares only the original table. A new test compares the new table with serveRuleReasonCatalogue and checks that the two catalogues share no code.")
