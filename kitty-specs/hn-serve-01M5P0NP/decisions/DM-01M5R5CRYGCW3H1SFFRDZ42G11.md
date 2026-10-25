# Decision Moment `01M5R5CRYGCW3H1SFFRDZ42G11`

- **Mission:** `hn-serve-01M5P0NP`
- **Origin flow:** `plan`
- **Slot key:** `plan.protocol.server-identification`
- **Input key:** `server_identification`
- **Status:** `resolved`
- **Created:** `2026-10-24T23:54:32.272864+00:00`
- **Resolved:** `2026-10-24T23:54:33.568838+00:00`
- **Resolved by:** `llxprt (maintainer-delegated planning)`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

How does an hn serve remote identify itself so clients can tell it from an ordinary Git remote (FR-017, FR-018)?

## Options

- Server-maintained ref refs/hn/serve/v1 pointing at a blob containing hn-serve/1, visible to git ls-remote
- Custom SSH command outside the Git protocol
- Other

## Final answer

Server-maintained ref refs/hn/serve/v1 pointing at a blob containing hn-serve/1, created by hn serve repo create and refused for every push (reserved-ref)

## Rationale

Works through any transport and through the forced command without a new SSH command. Spike with git 2.53 confirmed ls-remote advertises blob refs under protocol v0 and v2. Identification is a usability guard only: all-trusted selection still sends every ref through client quarantine and validation, so a forged marker on an ordinary remote grants nothing (C-006, NFR-006).

## Change log

- `2026-10-24T23:54:32.272864+00:00` — opened
- `2026-10-24T23:54:33.568838+00:00` — resolved (final_answer="Server-maintained ref refs/hn/serve/v1 pointing at a blob containing hn-serve/1, created by hn serve repo create and refused for every push (reserved-ref)")
