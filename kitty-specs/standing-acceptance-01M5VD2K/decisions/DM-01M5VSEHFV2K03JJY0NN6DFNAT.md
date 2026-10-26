# Decision Moment `01M5VSEHFV2K03JJY0NN6DFNAT`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `plan`
- **Slot key:** `plan.quality.shallow-semantic-matrix`
- **Input key:** `shallow_semantic_defect_matrix`
- **Status:** `resolved`
- **Created:** `2026-10-26T09:42:44.987541+00:00`
- **Resolved:** `2026-10-26T09:42:44.989852+00:00`
- **Resolved by:** `maintainer (delegated remediation instruction)`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

How do shallow-surface rows of the defect matrix prove that semantic defects reach rule derivation instead of passing through an unrelated dependency gap (analysis finding U2)?

## Options

- Hydrate a complete closure for semantic rows, assert the derivation check, keep missing-dependency rows separate, add matched valid controls
- Count any shallow refusal as success
- Other

## Final answer

For every semantic defect row (F01 to F20), the shallow verifier holds the complete closure that path (b) needs: the selected evidence chains and the requests their results answer, the candidate base and head commits with the proposal code ref, the base policy blob and every base-policy pipeline definition at the head. The row passes only when the refusal names the expected check (rule merge <fact> fails <check>, or the maintainer-path refusal for F19 and F20); a shallow dependency gap on a semantic row fails the test. Each semantic row has a valid control with the same selection and object arrangement that the shallow verifier accepts. Missing-dependency rows (F21, F22 and the WP09 gap cases) are a separate table that asserts the dependency kind and recovery guidance.

## Rationale

Without matched closure, every shallow negative row could pass through an earlier gap and give a full rejection column without exercising derivation.

## Change log

- `2026-10-26T09:42:44.987541+00:00` — opened
- `2026-10-26T09:42:44.989852+00:00` — resolved (final_answer="For every semantic defect row (F01 to F20), the shallow verifier holds the complete closure that path (b) needs: the selected evidence chains and the requests their results answer, the candidate base and head commits with the proposal code ref, the base policy blob and every base-policy pipeline definition at the head. The row passes only when the refusal names the expected check (rule merge <fact> fails <check>, or the maintainer-path refusal for F19 and F20); a shallow dependency gap on a semantic row fails the test. Each semantic row has a valid control with the same selection and object arrangement that the shallow verifier accepts. Missing-dependency rows (F21, F22 and the WP09 gap cases) are a separate table that asserts the dependency kind and recovery guidance.")
