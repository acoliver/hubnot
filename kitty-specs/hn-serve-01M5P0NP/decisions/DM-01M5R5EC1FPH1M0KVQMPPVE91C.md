# Decision Moment `01M5R5EC1FPH1M0KVQMPPVE91C`

- **Mission:** `hn-serve-01M5P0NP`
- **Origin flow:** `plan`
- **Slot key:** `plan.testing.harness`
- **Input key:** `test_harness`
- **Status:** `resolved`
- **Created:** `2026-10-24T23:55:24.591085+00:00`
- **Resolved:** `2026-10-24T23:55:25.897337+00:00`
- **Resolved by:** `llxprt (maintainer-delegated planning)`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

How do acceptance tests reach the server through the forced command (C-009, OQ-9)?

## Options

- Generated shell shim used as GIT_SSH_COMMAND that looks up the generated authorized_keys entry for its key and runs that exact forced command with SSH_ORIGINAL_COMMAND, plus one real unprivileged sshd test skipped with a reason when unavailable
- Call hn serve git directly from tests
- Other

## Final answer

Tests build the hn binary once per test (existing buildOperationalBinary pattern) and generate one POSIX sh shim per client key. The shim reads the server home's authorized_keys, finds the managed entry whose key matches its configured public key, extracts the command= value and runs it under env -i with HOME, PATH, SSH_ORIGINAL_COMMAND and, when set, GIT_PROTOCOL. git, git lfs and hn reach it through GIT_SSH_COMMAND. A shim configured with an unregistered key finds no entry and fails as sshd would. One test starts /usr/sbin/sshd as the test user on a loopback port with a temporary host key; it calls t.Skip with the reason when sshd or ssh-keygen is missing or cannot start.

## Rationale

Running the exact command string from the generated entry tests FR-004 and FR-005 end to end without root or a system sshd. env -i reproduces the minimal sshd session environment and proves the wrapper does not rely on inherited variables. The real-sshd test covers restrict and forced-command semantics that a shim cannot prove.

## Change log

- `2026-10-24T23:55:24.591085+00:00` — opened
- `2026-10-24T23:55:25.897337+00:00` — resolved (final_answer="Tests build the hn binary once per test (existing buildOperationalBinary pattern) and generate one POSIX sh shim per client key. The shim reads the server home's authorized_keys, finds the managed entry whose key matches its configured public key, extracts the command= value and runs it under env -i with HOME, PATH, SSH_ORIGINAL_COMMAND and, when set, GIT_PROTOCOL. git, git lfs and hn reach it through GIT_SSH_COMMAND. A shim configured with an unregistered key finds no entry and fails as sshd would. One test starts /usr/sbin/sshd as the test user on a loopback port with a temporary host key; it calls t.Skip with the reason when sshd or ssh-keygen is missing or cannot start.")
