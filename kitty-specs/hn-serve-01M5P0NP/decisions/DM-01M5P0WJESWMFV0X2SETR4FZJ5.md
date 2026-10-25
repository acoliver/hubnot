# Decision Moment `01M5P0WJESWMFV0X2SETR4FZJ5`

- **Mission:** `hn-serve-01M5P0NP`
- **Origin flow:** `specify`
- **Slot key:** `specify.push.protected-branch-bootstrap`
- **Input key:** `protected_branch_bootstrap`
- **Status:** `resolved`
- **Created:** `2026-10-24T03:57:18.169482+00:00`
- **Resolved:** `2026-10-24T03:57:26.719363+00:00`
- **Resolved by:** `llxprt`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

How does a protected branch first come to exist in a newly hosted repository, and what happens when its current tip has no valid policy?

## Options

- The administrator seeds initial history with hn serve repo create --import; creating an absent protected branch by push is refused; a protected tip without a valid .hn/policy.json refuses every protected update until fixed by administrator re-seeding
- Any mapped actor may create the protected branch once
- Treat a missing policy as permissive
- Other

## Final answer

OQ-11: The administrator seeds a new hosted repository's initial history with an hn serve repo subcommand that imports an existing repository. Creating an absent protected branch by push is refused. If the current protected tip has no valid .hn/policy.json, every update to that branch is refused with a structured reason; recovery is an explicit administrator action outside the governed push path.

## Rationale

Agent-selected default under the maintainer's delegated non-interactive instruction, chosen for consistency with the .kittify charter (trust is explicit policy, fail closed on hostile or missing input). Recorded in spec.md as an open question for maintainer confirmation at spec review.

## Change log

- `2026-10-24T03:57:18.169482+00:00` — opened
- `2026-10-24T03:57:26.719363+00:00` — resolved (final_answer="OQ-11: The administrator seeds a new hosted repository's initial history with an hn serve repo subcommand that imports an existing repository. Creating an absent protected branch by push is refused. If the current protected tip has no valid .hn/policy.json, every update to that branch is refused with a structured reason; recovery is an explicit administrator action outside the governed push path.")
