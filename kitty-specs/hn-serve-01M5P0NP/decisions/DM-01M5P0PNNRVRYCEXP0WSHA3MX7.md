# Decision Moment `01M5P0PNNRVRYCEXP0WSHA3MX7`

- **Mission:** `hn-serve-01M5P0NP`
- **Origin flow:** `specify`
- **Slot key:** `specify.fanout.selector-interaction`
- **Input key:** `fanout_selector_interaction`
- **Status:** `resolved`
- **Created:** `2026-10-24T03:54:04.856370+00:00`
- **Resolved:** `2026-10-24T03:55:44.903354+00:00`
- **Resolved by:** `llxprt`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

How does the server fan-out selector interact with exact selections, replication budgets and memory streams?

## Options

- New --all-trusted mode, refused by remotes that do not advertise hn serve; additive with exact selectors; absent refs are not failures; every ref still passes client quarantine, validation and the same per-ref budgets; memory excluded
- Replace exact selection entirely when the remote is hn serve
- Reuse --all with no new selector
- Other

## Final answer

OQ-3: New --all-trusted saved selection mode, mutually exclusive with --all, refused for remotes that do not identify as hn serve; additive with exact actor and proposal selectors; refs not yet advertised are not failures; every imported ref still passes client quarantine, validation and the same per-ref budgets; memory streams are excluded and still need exact selection.

## Rationale

Agent-selected default under the maintainer's delegated non-interactive instruction, chosen for consistency with the .kittify charter (Go, standard library first, optional service, hostile input, public-CLI tests). Recorded in spec.md as an open question for maintainer confirmation at spec review.

## Change log

- `2026-10-24T03:54:04.856370+00:00` — opened
- `2026-10-24T03:55:44.903354+00:00` — resolved (final_answer="OQ-3: New --all-trusted saved selection mode, mutually exclusive with --all, refused for remotes that do not identify as hn serve; additive with exact actor and proposal selectors; refs not yet advertised are not failures; every imported ref still passes client quarantine, validation and the same per-ref budgets; memory streams are excluded and still need exact selection.")
