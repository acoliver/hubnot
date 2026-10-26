# Decision Moment `01M5P0PM67J5TM2TF6MCM313ME`

- **Mission:** `hn-serve-01M5P0NP`
- **Origin flow:** `specify`
- **Slot key:** `specify.push.rejection-format`
- **Input key:** `rejection_message_format`
- **Status:** `resolved`
- **Created:** `2026-10-24T03:54:03.335546+00:00`
- **Resolved:** `2026-10-24T03:55:43.397712+00:00`
- **Resolved by:** `llxprt`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

What format do push rejections use so agents can act on them?

## Options

- Per-ref Git ng status with a stable reason code, plus one versioned key=value line per rejected ref on the remote message channel carrying full IDs, missing evidence and next action
- Free-form human prose only
- JSON document on the remote message channel
- Other

## Final answer

OQ-2: A push is all-or-nothing. Git reports every ref of a rejected push as rejected, and the remote message channel carries one versioned line per rejected ref (hn-serve/1 reject, ref, stable reason code, full IDs, missing evidence kinds, next action) followed by one human-readable sentence.

## Rationale

Agent-selected default under the maintainer's delegated non-interactive instruction, chosen for consistency with the .kittify charter (Go, standard library first, optional service, hostile input, public-CLI tests). Recorded in spec.md as an open question for maintainer confirmation at spec review.

## Change log

- `2026-10-24T03:54:03.335546+00:00` — opened
- `2026-10-24T03:55:43.397712+00:00` — resolved (final_answer="OQ-2: A push is all-or-nothing. Git reports every ref of a rejected push as rejected, and the remote message channel carries one versioned line per rejected ref (hn-serve/1 reject, ref, stable reason code, full IDs, missing evidence kinds, next action) followed by one human-readable sentence.")
