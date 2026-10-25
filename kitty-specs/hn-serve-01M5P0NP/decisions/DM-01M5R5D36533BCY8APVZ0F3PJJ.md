# Decision Moment `01M5R5D36533BCY8APVZ0F3PJJ`

- **Mission:** `hn-serve-01M5P0NP`
- **Origin flow:** `plan`
- **Slot key:** `plan.protocol.reason-codes`
- **Input key:** `reason_code_additions`
- **Status:** `resolved`
- **Created:** `2026-10-24T23:54:42.757439+00:00`
- **Resolved:** `2026-10-24T23:54:44.170073+00:00`
- **Resolved by:** `llxprt (maintainer-delegated planning)`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

Which reason codes beyond the OQ-2 initial list does the plan need, and how are unnamed ref namespaces handled?

## Options

- Add repository-busy and advance-not-maintainer; refuse refs outside refs/heads, refs/tags and the governed refs/hn classes with reserved-ref
- Keep the initial list only
- Other

## Final answer

Add repository-busy (lock wait budget, default 120 seconds, exceeded) and advance-not-maintainer (an advancement request whose pusher is not a maintainer in both base and destination policy, FR-014). Refs outside refs/heads/*, refs/tags/* and the governed refs/hn classes, such as refs/notes/*, are refused with reserved-ref. Precedence for one protected ref is fixed and documented in contracts/push-verdict.md.

## Rationale

OQ-2 calls its list initial, and FR-013 requires documented codes, so additions are allowed when documented. A bounded lock wait prevents a stalled client from blocking a repository indefinitely without a misleading code. Fail-closed handling of unnamed namespaces follows HN-003 and NFR-003.

## Change log

- `2026-10-24T23:54:42.757439+00:00` — opened
- `2026-10-24T23:54:44.170073+00:00` — resolved (final_answer="Add repository-busy (lock wait budget, default 120 seconds, exceeded) and advance-not-maintainer (an advancement request whose pusher is not a maintainer in both base and destination policy, FR-014). Refs outside refs/heads/*, refs/tags/* and the governed refs/hn classes, such as refs/notes/*, are refused with reserved-ref. Precedence for one protected ref is fixed and documented in contracts/push-verdict.md.")
