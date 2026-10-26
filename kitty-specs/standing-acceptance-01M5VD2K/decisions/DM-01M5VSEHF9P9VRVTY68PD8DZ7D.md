# Decision Moment `01M5VSEHF9P9VRVTY68PD8DZ7D`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `plan`
- **Slot key:** `plan.quality.frozen-tests`
- **Input key:** `frozen_test_snapshot`
- **Status:** `resolved`
- **Created:** `2026-10-26T09:42:44.969104+00:00`
- **Resolved:** `2026-10-26T09:42:44.970815+00:00`
- **Resolved by:** `maintainer (delegated remediation instruction)`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

Which commit freezes pre-existing tests for the C-003 and NFR-004 audit, now that the reviewed sync-speedup merge changed replication_borrow_test.go after 7a47e69 (analysis finding I2)?

## Options

- Freeze at d881d9b and audit only mission edits; keep 7a47e69 as the older-client reference
- Freeze at 7a47e69
- Other

## Final answer

Decision C. Pre-existing tests are frozen at d881d9bcd0ade4fa1bce91061954a8582e8d9318, the merge commit that brought in the reviewed sync speedup. The audit checks only mission edits: git diff --name-status d881d9b -- *_test.go must list no modified, deleted or renamed test file that exists at d881d9b, only added files. 7a47e6952a427ad7e3a3766c221fd334d9331950 stays the older-client compatibility reference and is not used for the audit. C-001 (go.mod and go.sum unchanged) is audited against d881d9b as well.

## Rationale

The reviewed merge legitimately changed a test file after 7a47e69, so an audit against 7a47e69 fails for a correct implementation. Separating the audit snapshot from the older-binary reference keeps both checks meaningful. The maintainer made this decision.

## Change log

- `2026-10-26T09:42:44.969104+00:00` — opened
- `2026-10-26T09:42:44.970815+00:00` — resolved (final_answer="Decision C. Pre-existing tests are frozen at d881d9bcd0ade4fa1bce91061954a8582e8d9318, the merge commit that brought in the reviewed sync speedup. The audit checks only mission edits: git diff --name-status d881d9b -- *_test.go must list no modified, deleted or renamed test file that exists at d881d9b, only added files. 7a47e6952a427ad7e3a3766c221fd334d9331950 stays the older-client compatibility reference and is not used for the audit. C-001 (go.mod and go.sum unchanged) is audited against d881d9b as well.")
