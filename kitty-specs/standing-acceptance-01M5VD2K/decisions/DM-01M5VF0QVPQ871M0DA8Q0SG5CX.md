# Decision Moment `01M5VF0QVPQ871M0DA8Q0SG5CX`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `plan`
- **Slot key:** `plan.testing.older-client`
- **Input key:** `older_client_evidence`
- **Status:** `resolved`
- **Created:** `2026-10-26T06:40:26.998267+00:00`
- **Resolved:** `2026-10-26T06:40:29.295849+00:00`
- **Resolved by:** `llxprt`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

How is the older-client fail-closed behavior of User Story 7, scenarios 2 and 3, demonstrated?

## Options

- Inside go test, building hn from the pre-mission commit
- Recorded review evidence only
- Not demonstrated
- Other

## Final answer

It runs inside go test. A test extracts commit 7a47e6952a427ad7e3a3766c221fd334d9331950, the last code commit before this mission, with git archive into t.TempDir(), builds it with go build, and runs that binary against repositories produced by the new binary. It skips with a visible reason under -short and when the commit object is absent, for example in a shallow CI checkout. The work-package review records the test output as evidence.

## Rationale

Plan-phase decision made by the planning agent under the maintainer's delegated non-interactive instruction (spec confirmed, OQ-1..11 accepted). The spec leaves the choice to planning. An executable test is stronger evidence than a recorded transcript, and the module uses only the standard library, so the old build needs no network.

## Change log

- `2026-10-26T06:40:26.998267+00:00` — opened
- `2026-10-26T06:40:29.295849+00:00` — resolved (final_answer="It runs inside go test. A test extracts commit 7a47e6952a427ad7e3a3766c221fd334d9331950, the last code commit before this mission, with git archive into t.TempDir(), builds it with go build, and runs that binary against repositories produced by the new binary. It skips with a visible reason under -short and when the commit object is absent, for example in a shallow CI checkout. The work-package review records the test output as evidence.")
