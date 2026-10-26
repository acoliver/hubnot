# Decision Moment `01M5P0QACKKG97FP4437Z4E5G5`

- **Mission:** `hn-serve-01M5P0NP`
- **Origin flow:** `specify`
- **Slot key:** `specify.hosting.provisioning-access`
- **Input key:** `repository_provisioning_access`
- **Status:** `resolved`
- **Created:** `2026-10-24T03:54:26.067513+00:00`
- **Resolved:** `2026-10-24T03:55:50.889271+00:00`
- **Resolved by:** `llxprt`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

How are hosted repositories provisioned, who may read them, and how does the server treat running as root?

## Options

- Repositories created only by hn serve repo subcommands (no create-on-push); every mapped key may read and push to every hosted repository subject to the push check; no anonymous access; hn serve refuses to run as uid 0
- Create repositories on first push
- Per-repository access lists in this mission
- Other

## Final answer

OQ-7: Repositories are created only by hn serve repo subcommands (no create-on-push); every mapped key may read and push to every hosted repository subject to the push check; no anonymous access; hn serve refuses to run as uid 0.

## Rationale

Agent-selected default under the maintainer's delegated non-interactive instruction, chosen for consistency with the .kittify charter (Go, standard library first, optional service, hostile input, public-CLI tests). Recorded in spec.md as an open question for maintainer confirmation at spec review.

## Change log

- `2026-10-24T03:54:26.067513+00:00` — opened
- `2026-10-24T03:55:50.889271+00:00` — resolved (final_answer="OQ-7: Repositories are created only by hn serve repo subcommands (no create-on-push); every mapped key may read and push to every hosted repository subject to the push check; no anonymous access; hn serve refuses to run as uid 0.")
