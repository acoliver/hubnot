# Decision Moment `01M5VF10Q3SQX8WTK2MEN92ZSC`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `plan`
- **Slot key:** `plan.execution.remote-repair`
- **Input key:** `remote_rule_repair`
- **Status:** `resolved`
- **Created:** `2026-10-26T06:40:36.068089+00:00`
- **Resolved:** `2026-10-26T06:40:38.433099+00:00`
- **Resolved by:** `llxprt`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

How does hn automerge --execute --remote recover after the server advanced but the rule merge fact was not recorded?

## Options

- Repair branch in automergeRemote reusing mergeRemoteBound step 5
- Require hn merge --remote
- Manual fact construction
- Other

## Final answer

automergeRemote takes a repair branch when the hosted tip equals or descends from the candidate head and the local target still equals the signed base. It re-runs the rule inspection against the imported facts, skips the check that the local branch equals the hosted tip, and calls mergeRemoteBound with the rule binding. Step 5 of mergeRemoteBound then records the missing rule merge fact once, with commit equal to the head. When a rule binding is present, rerun messages from mergeRemoteBound name the hn automerge command.

## Rationale

Plan-phase decision made by the planning agent under the maintainer's delegated non-interactive instruction (spec confirmed, OQ-1..11 accepted). FR-020 requires the same command to repair, and today a rerun stops at the local-equals-hosted check. mergeRemoteBound step 5 already implements once-only recording for fast-forward facts.

## Change log

- `2026-10-26T06:40:36.068089+00:00` — opened
- `2026-10-26T06:40:38.433099+00:00` — resolved (final_answer="automergeRemote takes a repair branch when the hosted tip equals or descends from the candidate head and the local target still equals the signed base. It re-runs the rule inspection against the imported facts, skips the check that the local branch equals the hosted tip, and calls mergeRemoteBound with the rule binding. Step 5 of mergeRemoteBound then records the missing rule merge fact once, with commit equal to the head. When a rule binding is present, rerun messages from mergeRemoteBound name the hn automerge command.")
