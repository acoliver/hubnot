# Decision Moment `01M5R5CP4DDYC71WFE8Q74TET5`

- **Mission:** `hn-serve-01M5P0NP`
- **Origin flow:** `plan`
- **Slot key:** `plan.architecture.server-home`
- **Input key:** `server_home_layout`
- **Status:** `resolved`
- **Created:** `2026-10-24T23:54:29.389239+00:00`
- **Resolved:** `2026-10-24T23:54:30.955123+00:00`
- **Resolved by:** `llxprt (maintainer-delegated planning)`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

Where do hn serve state, hosted repositories and the forced command live, and how does the forced command find them?

## Options

- Fixed layout under the server user home ($HOME/.hn-serve) with an absolute hn path in the forced command
- Configurable --home flag repeated in every forced command
- Other

## Final answer

Fixed layout under the server user home: $HOME/.hn-serve/state (0700) and $HOME/.hn-serve/repositories (0700); managed entries in $HOME/.ssh/authorized_keys; forced command is the absolute path of the installed hn executable followed by serve git --key FINGERPRINT

## Rationale

The server runs as a dedicated Unix user (OQ-7), so that user's home is the natural namespace and the forced command stays exactly hn serve git --key FINGERPRINT (FR-004) with no extra flags. An absolute executable path removes dependence on the sshd session PATH. Tests set HOME to a temporary directory. A --home flag would add a second source of truth to every authorized_keys entry.

## Change log

- `2026-10-24T23:54:29.389239+00:00` — opened
- `2026-10-24T23:54:30.955123+00:00` — resolved (final_answer="Fixed layout under the server user home: $HOME/.hn-serve/state (0700) and $HOME/.hn-serve/repositories (0700); managed entries in $HOME/.ssh/authorized_keys; forced command is the absolute path of the installed hn executable followed by serve git --key FINGERPRINT")
