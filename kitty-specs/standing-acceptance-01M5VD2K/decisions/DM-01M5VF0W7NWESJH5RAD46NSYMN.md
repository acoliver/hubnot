# Decision Moment `01M5VF0W7NWESJH5RAD46NSYMN`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `plan`
- **Slot key:** `plan.docs.traceability`
- **Input key:** `docs_traceability`
- **Status:** `resolved`
- **Created:** `2026-10-26T06:40:31.477103+00:00`
- **Resolved:** `2026-10-26T06:40:33.714082+00:00`
- **Resolved by:** `llxprt`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

Where do the documentation examples and the requirement-to-test traceability for this mission live?

## Options

- Example markers in the four docs plus a governance-v0 traceability section with its own test
- Add rows to the serve-v0 traceability tables
- Traceability only in kitty-specs
- Other

## Final answer

Examples carry unique <!-- example: standing-... --> markers in docs/governance-v0.md, docs/protocol-v0.md and docs/serve-v0.md. A new test extracts and executes each one, so the existing example guard covers them. Traceability goes into a new "Standing acceptance traceability" section of docs/governance-v0.md, checked by its own test. The traceability section of docs/serve-v0.md is not used because an existing test pins it to the hn serve IDs.

## Rationale

Plan-phase decision made by the planning agent under the maintainer's delegated non-interactive instruction (spec confirmed, OQ-1..11 accepted). FR-023 and DIRECTIVE_037 require tested examples. TestServeQualityTraceabilityTestsExist refuses unexpected row IDs in serve-v0.md, and FR-001 to FR-023 would collide with the hn serve IDs.

## Change log

- `2026-10-26T06:40:31.477103+00:00` — opened
- `2026-10-26T06:40:33.714082+00:00` — resolved (final_answer="Examples carry unique <!-- example: standing-... --> markers in docs/governance-v0.md, docs/protocol-v0.md and docs/serve-v0.md. A new test extracts and executes each one, so the existing example guard covers them. Traceability goes into a new "Standing acceptance traceability" section of docs/governance-v0.md, checked by its own test. The traceability section of docs/serve-v0.md is not used because an existing test pins it to the hn serve IDs.")
