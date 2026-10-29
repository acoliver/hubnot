# Decision Moment `01M5VSEHFCZGQHQ14CKJ3MAMP7`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `plan`
- **Slot key:** `plan.client.remote-recovery`
- **Input key:** `remote_rule_publication_rerun`
- **Status:** `resolved`
- **Created:** `2026-10-26T09:42:44.972739+00:00`
- **Resolved:** `2026-10-26T09:42:44.974891+00:00`
- **Resolved by:** `maintainer (delegated remediation instruction)`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

Amends decision 01M5VF10Q3SQX8WTK2MEN92ZSC after analysis finding I3: how does rerunning hn automerge --execute --remote recover after the rule merge fact was recorded but not, or only partly, published?

## Options

- Authorized, idempotent publication-only rerun before both merged blockers, in its own WP
- Tell the executing actor to run hn sync
- Other

## Final answer

Decision D. Two rerun paths of hn automerge PROPOSAL --rule NAME --execute --remote REMOTE, for standing rules only. (1) Remote repair, unchanged from decision 01M5VF10Q3SQX8WTK2MEN92ZSC: when the hosted tip contains the head, the candidate has no merge fact and the local target still equals the signed base, the command records the missing rule merge fact once with commit equal to the head. (2) Publication-only rerun, new: when the identity is an executing actor of NAME in the base policy and the local store holds a rule merge fact for this proposal that names NAME, is signed by that identity and was admitted through path (b), the command skips the proposal is already recorded as merged blocker of both inspections and the local-equals-hosted check, publishes or republishes the actor chain, and confirms that the hosted branch contains the fact commit. It signs nothing, creates no fact and pushes no branch update; if the hosted branch does not contain the commit it exits 1 and says so. Tests cover interruptions at record and at republish and a repeated rerun. A rerun without --remote keeps the ordinary already-merged refusal. The command-level changes and tests belong to WP19, which also takes the remote repair branch and the interruption table from WP15.

## Rationale

The analysis showed that both inspections stop a rerun at the merged blocker before the promised publication step, so the record row of the interruption table was unreachable. The maintainer specified the publication-only path; a separate WP keeps WP15 within one implementer session.

## Change log

- `2026-10-26T09:42:44.972739+00:00` — opened
- `2026-10-26T09:42:44.974891+00:00` — resolved (final_answer="Decision D. Two rerun paths of hn automerge PROPOSAL --rule NAME --execute --remote REMOTE, for standing rules only. (1) Remote repair, unchanged from decision 01M5VF10Q3SQX8WTK2MEN92ZSC: when the hosted tip contains the head, the candidate has no merge fact and the local target still equals the signed base, the command records the missing rule merge fact once with commit equal to the head. (2) Publication-only rerun, new: when the identity is an executing actor of NAME in the base policy and the local store holds a rule merge fact for this proposal that names NAME, is signed by that identity and was admitted through path (b), the command skips the proposal is already recorded as merged blocker of both inspections and the local-equals-hosted check, publishes or republishes the actor chain, and confirms that the hosted branch contains the fact commit. It signs nothing, creates no fact and pushes no branch update; if the hosted branch does not contain the commit it exits 1 and says so. Tests cover interruptions at record and at republish and a repeated rerun. A rerun without --remote keeps the ordinary already-merged refusal. The command-level changes and tests belong to WP19, which also takes the remote repair branch and the interruption table from WP15.")
