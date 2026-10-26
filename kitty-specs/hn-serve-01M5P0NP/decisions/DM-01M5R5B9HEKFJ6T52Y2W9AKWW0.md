# Decision Moment `01M5R5B9HEKFJ6T52Y2W9AKWW0`

- **Mission:** `hn-serve-01M5P0NP`
- **Origin flow:** `plan`
- **Slot key:** `plan.architecture.enforcement-point`
- **Input key:** `enforcement_point`
- **Status:** `resolved`
- **Created:** `2026-10-24T23:53:43.726899+00:00`
- **Resolved:** `2026-10-24T23:53:51.881882+00:00`
- **Resolved by:** `llxprt (maintainer-delegated planning)`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

Where does hn serve enforce the push verdict, and how are protected updates serialized?

## Options

- pre-receive and post-receive hooks run by a forced-command wrapper that holds a per-repository exclusive lock for the whole receive-pack session
- proc-receive hook that applies ref updates itself
- reference-transaction hook with per-ref checks
- Other

## Final answer

pre-receive and post-receive hooks run by a forced-command wrapper that holds a per-repository exclusive lock for the whole receive-pack session

## Rationale

pre-receive sees quarantined objects and a non-zero exit rejects every ref and discards the pushed objects (FR-012), verified by spike with git 2.53. Hooks cannot hold a lock across the ref update, so the wrapper takes a flock before starting receive-pack and shares the descriptor with it. Every check and ref update for one hosted repository then runs one push at a time (FR-022, NFR-005). post-receive records branch owners and the push log only after refs move. proc-receive and reference-transaction were rejected because they require reimplementing ref updates or split non-atomic pushes into separate transactions.

## Change log

- `2026-10-24T23:53:43.726899+00:00` — opened
- `2026-10-24T23:53:51.881882+00:00` — resolved (final_answer="pre-receive and post-receive hooks run by a forced-command wrapper that holds a per-repository exclusive lock for the whole receive-pack session")
