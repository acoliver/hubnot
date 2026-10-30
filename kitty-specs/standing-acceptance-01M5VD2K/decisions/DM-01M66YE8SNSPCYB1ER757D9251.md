# Decision Moment `01M66YE8SNSPCYB1ER757D9251`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `specify`
- **Slot key:** `specify.policy.rule-minimum-review-amendment`
- **Input key:** `rule_minimum_review_zero_approval`
- **Status:** `resolved`
- **Created:** `2026-10-30T17:41:37.717227+00:00`
- **Resolved:** `2026-10-30T17:43:03.091059+00:00`
- **Resolved by:** `maintainer (Andrew, 2026-10-30)`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

Amends OQ-1 (decision 01M5VD5PSRW8AVQYE7C8AHEE7Q): may a rule with acceptance rule be used when proposals.requiredApprovals is 0, so ordinary changes merge on the pipeline gate alone?

## Options

- Allow requiredApprovals 0 under acceptance rule; the rule then needs no approvals and the pipeline gate and every veto still apply
- Keep the OQ-1 refusal
- Add a rule-level approval threshold
- Other

## Final answer

Supersedes OQ-1 (decision 01M5VD5PSRW8AVQYE7C8AHEE7Q) and FR-004. proposals.requiredApprovals 0 is allowed with a rule in acceptance mode rule and means the rule needs no approvals: ordinary changes merge on the pipeline gate alone, with no approval from another session. Any value 1 to 64 keeps the confirmed behavior. No policy field is added. Every other blocker still applies: a current trusted change request, a current maintainer rejection, a failed or missing required pipeline result, the author, path and executing-actor limits, the manual path for policy and protected-check changes, and the target-policy check. Because every pipeline requires at least one passing result, policy validation refuses a standing rule with requiredApprovals 0 only when the policy defines no pipeline, so a rule merge always cites gate evidence. A rule merge fact under requiredApprovals 0 may list passing run results only; the strict path (b) evidence rules are otherwise unchanged, and approvals from counted reviewers, when present, are still listed. Clients that predate the rule fields still reject such facts.

## Rationale

Maintainer decision (Andrew, 2026-10-30): ordinary changes must be able to merge on the gate alone, configurable through the existing proposals.requiredApprovals field. The resolved OQ-1 record is immutable in the ledger, so this entry amends it. The pipeline requirement for requiredApprovals 0 was chosen by the implementer under delegated instruction so that a standing rule never merges with no evidence at all; it adds a validation, not a field.

## Change log

- `2026-10-30T17:41:37.717227+00:00` — opened
- `2026-10-30T17:43:03.091059+00:00` — resolved (final_answer="Supersedes OQ-1 (decision 01M5VD5PSRW8AVQYE7C8AHEE7Q) and FR-004. proposals.requiredApprovals 0 is allowed with a rule in acceptance mode rule and means the rule needs no approvals: ordinary changes merge on the pipeline gate alone, with no approval from another session. Any value 1 to 64 keeps the confirmed behavior. No policy field is added. Every other blocker still applies: a current trusted change request, a current maintainer rejection, a failed or missing required pipeline result, the author, path and executing-actor limits, the manual path for policy and protected-check changes, and the target-policy check. Because every pipeline requires at least one passing result, policy validation refuses a standing rule with requiredApprovals 0 only when the policy defines no pipeline, so a rule merge always cites gate evidence. A rule merge fact under requiredApprovals 0 may list passing run results only; the strict path (b) evidence rules are otherwise unchanged, and approvals from counted reviewers, when present, are still listed. Clients that predate the rule fields still reject such facts.")
