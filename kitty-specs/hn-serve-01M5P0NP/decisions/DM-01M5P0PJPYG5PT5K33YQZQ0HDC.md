# Decision Moment `01M5P0PJPYG5PT5K33YQZQ0HDC`

- **Mission:** `hn-serve-01M5P0NP`
- **Origin flow:** `specify`
- **Slot key:** `specify.identity.registry-storage`
- **Input key:** `key_registry_storage`
- **Status:** `resolved`
- **Created:** `2026-10-24T03:54:01.822874+00:00`
- **Resolved:** `2026-10-24T03:55:41.916960+00:00`
- **Resolved by:** `llxprt`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

How is the SSH-key-to-actor registry stored and administered (format, location outside any repository, who may edit it)?

## Options

- Owner-only versioned JSON file in the server state directory, edited only through hn serve key subcommands run as the server Unix user; authorized_keys regenerated from it
- Hand-edited authorized_keys with fingerprint comments as the source of truth
- Registry stored as signed facts inside each hosted repository
- Other

## Final answer

OQ-1: Owner-only versioned JSON registry in the server state directory outside every hosted repository, edited only through hn serve key subcommands run as the server Unix user; full actor fingerprint and full SSH key fingerprint required; one SSH key maps to exactly one actor, an actor may have several keys; the managed authorized_keys file is regenerated from the registry and never hand-edited.

## Rationale

Agent-selected default under the maintainer's delegated non-interactive instruction, chosen for consistency with the .kittify charter (Go, standard library first, optional service, hostile input, public-CLI tests). Recorded in spec.md as an open question for maintainer confirmation at spec review.

## Change log

- `2026-10-24T03:54:01.822874+00:00` — opened
- `2026-10-24T03:55:41.916960+00:00` — resolved (final_answer="OQ-1: Owner-only versioned JSON registry in the server state directory outside every hosted repository, edited only through hn serve key subcommands run as the server Unix user; full actor fingerprint and full SSH key fingerprint required; one SSH key maps to exactly one actor, an actor may have several keys; the managed authorized_keys file is regenerated from the registry and never hand-edited.")
