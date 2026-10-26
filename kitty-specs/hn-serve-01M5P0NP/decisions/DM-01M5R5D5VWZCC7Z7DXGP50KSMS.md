# Decision Moment `01M5R5D5VWZCC7Z7DXGP50KSMS`

- **Mission:** `hn-serve-01M5P0NP`
- **Origin flow:** `plan`
- **Slot key:** `plan.replication.all-trusted-storage`
- **Input key:** `all_trusted_storage`
- **Status:** `resolved`
- **Created:** `2026-10-24T23:54:45.500528+00:00`
- **Resolved:** `2026-10-24T23:54:46.783955+00:00`
- **Resolved by:** `llxprt (maintainer-delegated planning)`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

How is the all-trusted selection stored and reported without changing existing selections (FR-018, FR-019)?

## Options

- Keep selection version 1 and add allTrusted with omitempty; print all-trusted only when set; not-yet-advertised actor and proposal selectors are a non-failure status in all-trusted mode
- Bump the selection file to version 2
- Other

## Final answer

Selection files keep version 1 and gain allTrusted (omitempty), so existing files and their bytes are unchanged. hn replication show prints all-trusted: true only when set. The remote must advertise refs/hn/serve/v1 at select time and again at every sync. Under all-trusted, exact actor or proposal selectors that are not advertised report status not-advertised, which is not a failure; exact memory selectors keep their current semantics.

## Rationale

omitempty keeps FR-019 byte-for-byte for existing selections and keeps existing show-output assertions valid (NFR-007). Older binaries fail closed on the unknown field, which is safe. Rechecking identification at sync time stops an all-trusted selection from silently becoming a full import from a remote that stopped being hn serve.

## Change log

- `2026-10-24T23:54:45.500528+00:00` — opened
- `2026-10-24T23:54:46.783955+00:00` — resolved (final_answer="Selection files keep version 1 and gain allTrusted (omitempty), so existing files and their bytes are unchanged. hn replication show prints all-trusted: true only when set. The remote must advertise refs/hn/serve/v1 at select time and again at every sync. Under all-trusted, exact actor or proposal selectors that are not advertised report status not-advertised, which is not a failure; exact memory selectors keep their current semantics.")
