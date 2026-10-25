# Decision Moment `01M5P0Q5YPNM1689REXHWSK7SR`

- **Mission:** `hn-serve-01M5P0NP`
- **Origin flow:** `specify`
- **Slot key:** `specify.merge.server-advance-trigger`
- **Input key:** `server_advance_trigger`
- **Status:** `resolved`
- **Created:** `2026-10-24T03:54:21.526672+00:00`
- **Resolved:** `2026-10-24T03:55:46.445692+00:00`
- **Resolved by:** `llxprt`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

What exactly asks the server to advance a protected branch, and who signs the proposal.merged fact when the server fast-forwards?

## Options

- The maintainer signs an acceptance and a proposal.merged fact whose result is the tested proposal head, then pushes only their actor ref; the server advances the branch in the same all-or-nothing push and holds no signing key
- The server holds its own actor key and signs proposal.merged after any qualifying acceptance
- An acceptance alone moves the branch and no merged fact is recorded
- Other

## Final answer

OQ-4: The maintainer asks with hn merge against the hn serve remote: the push carries their actor ref (with the signed acceptance) and an advancement request naming the full candidate. The server stores the valid facts, then fast-forwards the protected branch to the proposal head only if every gate holds, otherwise reports a structured refusal. After a confirmed advance the maintainer client signs proposal.merged with result commit equal to the proposal head; a rerun records it if interrupted. The server holds no signing key. Direct protected-branch pushes by any mapped actor pass under the same gates.

## Rationale

Agent-selected default under the maintainer's delegated non-interactive instruction, chosen for consistency with the .kittify charter (Go, standard library first, optional service, hostile input, public-CLI tests). Recorded in spec.md as an open question for maintainer confirmation at spec review. Existing proposal.merged validation requires a base-policy maintainer signer, so a server key would need a protocol change.

## Change log

- `2026-10-24T03:54:21.526672+00:00` — opened
- `2026-10-24T03:55:46.445692+00:00` — resolved (final_answer="OQ-4: The maintainer asks with hn merge against the hn serve remote: the push carries their actor ref (with the signed acceptance) and an advancement request naming the full candidate. The server stores the valid facts, then fast-forwards the protected branch to the proposal head only if every gate holds, otherwise reports a structured refusal. After a confirmed advance the maintainer client signs proposal.merged with result commit equal to the proposal head; a rerun records it if interrupted. The server holds no signing key. Direct protected-branch pushes by any mapped actor pass under the same gates.")
