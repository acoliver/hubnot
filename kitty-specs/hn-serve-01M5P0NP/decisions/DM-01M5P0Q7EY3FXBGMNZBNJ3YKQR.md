# Decision Moment `01M5P0Q7EY3FXBGMNZBNJ3YKQR`

- **Mission:** `hn-serve-01M5P0NP`
- **Origin flow:** `specify`
- **Slot key:** `specify.push.protected-branch-config`
- **Input key:** `protected_branch_config`
- **Status:** `resolved`
- **Created:** `2026-10-24T03:54:23.070524+00:00`
- **Resolved:** `2026-10-24T03:55:47.993701+00:00`
- **Resolved by:** `llxprt`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

Where is the protected-branch set configured and what is its default?

## Options

- Per-repository server-side setting administered with hn serve subcommands, default refs/heads/main only, never read from pushed repository content
- Read from a file committed in the hosted repository
- Global server-wide list
- Other

## Final answer

OQ-5: Per-repository server-side setting administered with hn serve subcommands; default protects refs/heads/main only; never read from pushed repository content.

## Rationale

Agent-selected default under the maintainer's delegated non-interactive instruction, chosen for consistency with the .kittify charter (Go, standard library first, optional service, hostile input, public-CLI tests). Recorded in spec.md as an open question for maintainer confirmation at spec review.

## Change log

- `2026-10-24T03:54:23.070524+00:00` — opened
- `2026-10-24T03:55:47.993701+00:00` — resolved (final_answer="OQ-5: Per-repository server-side setting administered with hn serve subcommands; default protects refs/heads/main only; never read from pushed repository content.")
