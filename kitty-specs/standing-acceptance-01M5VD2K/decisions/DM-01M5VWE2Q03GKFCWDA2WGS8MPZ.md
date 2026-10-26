# Decision Moment `01M5VWE2Q03GKFCWDA2WGS8MPZ`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `plan`
- **Slot key:** `plan.quality.legacy-verification-outcomes`
- **Input key:** `legacy_fixture_verification_outcomes`
- **Status:** `resolved`
- **Created:** `2026-10-26T10:34:55.584102+00:00`
- **Resolved:** `2026-10-26T10:34:55.585689+00:00`
- **Resolved by:** `maintainer (delegated remediation instruction)`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

What verification outcome does each legacy history fixture assert, given that L06 is a deliberately invalid resource event (analysis round 2 finding I5)?

## Options

- Each fixture asserts its declared outcome: L01 to L05 verify, L06 fails with the existing unknown-field error and stays a negative control
- Require every legacy payload to verify
- Other

## Final answer

Each legacy fixture asserts its declared verification outcome. L01 to L05 verify successfully. L06, a signed resource event with an added rule key, fails verifyEvent with the existing strict-resource error invalid resource event JSON: unknown field "rule" on the new client and on the 7a47e69 older client, and stays a negative control; the same resource event without the key verifies. Strict resource decoding is not relaxed. WP06 T030 and T091, WP03 T013 and the legacy section of contracts/rule-merge-fact.md state this, and no text says every fixture is accepted.

## Rationale

The harness self-test required every raw legacy payload to verify, which L06 cannot do without relaxing strict resource decoding, contradicting C-004 and the legacy contract.

## Change log

- `2026-10-26T10:34:55.584102+00:00` — opened
- `2026-10-26T10:34:55.585689+00:00` — resolved (final_answer="Each legacy fixture asserts its declared verification outcome. L01 to L05 verify successfully. L06, a signed resource event with an added rule key, fails verifyEvent with the existing strict-resource error invalid resource event JSON: unknown field "rule" on the new client and on the 7a47e69 older client, and stays a negative control; the same resource event without the key verifies. Strict resource decoding is not relaxed. WP06 T030 and T091, WP03 T013 and the legacy section of contracts/rule-merge-fact.md state this, and no text says every fixture is accepted.")
