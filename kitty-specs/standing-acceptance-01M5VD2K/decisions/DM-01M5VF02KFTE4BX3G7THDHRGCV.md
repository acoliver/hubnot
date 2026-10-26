# Decision Moment `01M5VF02KFTE4BX3G7THDHRGCV`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `plan`
- **Slot key:** `plan.verification.missing-commits`
- **Input key:** `rule_missing_commits`
- **Status:** `resolved`
- **Created:** `2026-10-26T06:40:05.231140+00:00`
- **Resolved:** `2026-10-26T06:40:07.375038+00:00`
- **Resolved by:** `llxprt`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

How does each verifier get the changed paths for a rule merge fact, and what happens when the base or head commit is unavailable?

## Options

- Shared one-complete-store helper, missing-dependency before evaluation, shallow exact dependencies
- Compute paths from whichever store has each commit
- Skip path checks when commits are missing
- Other

## Final answer

Extract the one-complete-store lookup from validateProtectedDecisionAt into changedPathsInOneStore(gitDir, acceptedGitDir, base, head) without changing its behavior, and use it from the replication path. The local projection uses changedPathsAt(""). Before any evaluation, replication import reports an absent base or head as a replicationDependency that names the commit, with the recovery "add exact proposal selection --proposal <candidate>". hn serve renders that as dependency-missing. For a rule merge fact, guardShallowEventClosure requires the base and head commits (kinds base commit and proposal code ref), the base policy blob and each listed evidence fact (selected event fact). The existing run.result case already requires each answered run request. Each fact gets exactly one changed-path computation, bounded by the 8 MiB limit.

## Rationale

Plan-phase decision made by the planning agent under the maintainer's delegated non-interactive instruction (spec confirmed, OQ-1..11 accepted). OQ-10 and FR-014 require failing closed with recovery guidance, never evaluating an empty or partial change list. validateProtectedDecisionAt already follows the one-complete-store rule, so reusing it avoids a third variant.

## Change log

- `2026-10-26T06:40:05.231140+00:00` — opened
- `2026-10-26T06:40:07.375038+00:00` — resolved (final_answer="Extract the one-complete-store lookup from validateProtectedDecisionAt into changedPathsInOneStore(gitDir, acceptedGitDir, base, head) without changing its behavior, and use it from the replication path. The local projection uses changedPathsAt(""). Before any evaluation, replication import reports an absent base or head as a replicationDependency that names the commit, with the recovery "add exact proposal selection --proposal <candidate>". hn serve renders that as dependency-missing. For a rule merge fact, guardShallowEventClosure requires the base and head commits (kinds base commit and proposal code ref), the base policy blob and each listed evidence fact (selected event fact). The existing run.result case already requires each answered run request. Each fact gets exactly one changed-path computation, bounded by the 8 MiB limit.")
