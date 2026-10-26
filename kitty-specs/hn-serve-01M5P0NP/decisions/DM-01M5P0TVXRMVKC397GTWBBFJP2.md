# Decision Moment `01M5P0TVXRMVKC397GTWBBFJP2`

- **Mission:** `hn-serve-01M5P0NP`
- **Origin flow:** `specify`
- **Slot key:** `specify.testing.ssh-harness`
- **Input key:** `ssh_test_harness`
- **Status:** `resolved`
- **Created:** `2026-10-24T03:56:22.328945+00:00`
- **Resolved:** `2026-10-24T03:56:32.803356+00:00`
- **Resolved by:** `llxprt`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

How do acceptance tests exercise the SSH path without requiring a system sshd on every test host?

## Options

- Real git clients use an SSH command shim that runs the exact forced command from the generated authorized_keys entry; one additional test uses a real unprivileged OpenSSH server and skips with a visible reason when sshd is unavailable
- Require a real sshd for every test
- Call server functions directly
- Other

## Final answer

OQ-9: Real git and hn clients reach the server through an SSH command shim that runs the exact forced command from the generated authorized_keys entry against real temporary bare repositories; one additional end-to-end test uses a real unprivileged OpenSSH server and is skipped with a visible reason when sshd is unavailable.

## Rationale

Agent-selected default under the maintainer's delegated non-interactive instruction, chosen for consistency with the .kittify charter (Go, standard library first, optional service, hostile input, public-CLI tests). Recorded in spec.md as an open question for maintainer confirmation at spec review.

## Change log

- `2026-10-24T03:56:22.328945+00:00` — opened
- `2026-10-24T03:56:32.803356+00:00` — resolved (final_answer="OQ-9: Real git and hn clients reach the server through an SSH command shim that runs the exact forced command from the generated authorized_keys entry against real temporary bare repositories; one additional end-to-end test uses a real unprivileged OpenSSH server and is skipped with a visible reason when sshd is unavailable.")
