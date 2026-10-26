# Decision Moment `01M5VWE2Q9J3GEQHZQ0HF4F70W`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `plan`
- **Slot key:** `plan.quality.missing-evidence-recovery`
- **Input key:** `missing_evidence_recovery_fixture`
- **Status:** `resolved`
- **Created:** `2026-10-26T10:34:55.593563+00:00`
- **Resolved:** `2026-10-26T10:34:55.595686+00:00`
- **Resolved by:** `maintainer (delegated remediation instruction)`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

Which approval does the F21 missing-evidence fixture withhold so that publishing it permits admission under the default rule, which counts only R (analysis round 2 finding U3)?

## Options

- Withhold an otherwise qualifying approval from R, keep all other evidence sufficient, assert missing dependency before publication and admission after
- Add S to the fixture rule reviewers
- Other

## Final answer

F21 builds its own candidate whose only approval is an otherwise qualifying approval by R, the reviewer the default rule counts, signed in R's clone and not published (h.ApproveUnpublished). The CI request and passing result are published, so every other piece of evidence is sufficient. Before publication the verifier reports missing dependency naming the approval with its recovery guidance and the candidate is not merged; after R publishes (h.Publish) and the verifier syncs again, the fact is admitted and hn proposal status reports merged. No R helper runs between F21 preparation and the pre-publication assertion. Applied to WP06 and WP08 T038.

## Rationale

S is trusted but not counted by the default rule, so publishing an S approval would leave an evidence-item defect and the promised positive recovery could not occur.

## Change log

- `2026-10-26T10:34:55.593563+00:00` — opened
- `2026-10-26T10:34:55.595686+00:00` — resolved (final_answer="F21 builds its own candidate whose only approval is an otherwise qualifying approval by R, the reviewer the default rule counts, signed in R's clone and not published (h.ApproveUnpublished). The CI request and passing result are published, so every other piece of evidence is sufficient. Before publication the verifier reports missing dependency naming the approval with its recovery guidance and the candidate is not merged; after R publishes (h.Publish) and the verifier syncs again, the fact is admitted and hn proposal status reports merged. No R helper runs between F21 preparation and the pre-publication assertion. Applied to WP06 and WP08 T038.")
