# Decision Moment `01M5VEZRTBF8R7RZGHSH5EC6VS`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `plan`
- **Slot key:** `plan.policy.field-decoding`
- **Input key:** `policy_field_decoding`
- **Status:** `resolved`
- **Created:** `2026-10-26T06:39:55.211455+00:00`
- **Resolved:** `2026-10-26T06:39:57.646034+00:00`
- **Resolved by:** `llxprt`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

How are acceptance and reviewers decoded so that null and empty values are rejected instead of being read as absent?

## Options

- Named types with UnmarshalJSON that reject null
- Pointer fields
- Pre-scan of raw policy bytes
- Other

## Final answer

Add two named types in protected_checks.go. AutomergeAcceptance is a string kind whose UnmarshalJSON rejects JSON null, non-strings and any value other than "maintainers" or "rule". AutomergeReviewers is a []string kind whose UnmarshalJSON rejects null and decodes an array of strings into a non-nil slice. When a field is absent it keeps its zero value, which means acceptance maintainers or no rule reviewers. validateOptionalPolicy then applies FR-002 to FR-005, with messages that name the rule and the field. The policy digest stays the digest of the exact bytes.

## Rationale

Plan-phase decision made by the planning agent under the maintainer's delegated non-interactive instruction (spec confirmed, OQ-1..11 accepted). FR-001 and US2-6 require null, "" and non-string acceptance values, and empty reviewers, to be rejected. Pointer fields cannot tell null from absent. Named types keep the existing strict decoder with DisallowUnknownFields and add no second parse.

## Change log

- `2026-10-26T06:39:55.211455+00:00` — opened
- `2026-10-26T06:39:57.646034+00:00` — resolved (final_answer="Add two named types in protected_checks.go. AutomergeAcceptance is a string kind whose UnmarshalJSON rejects JSON null, non-strings and any value other than "maintainers" or "rule". AutomergeReviewers is a []string kind whose UnmarshalJSON rejects null and decodes an array of strings into a non-nil slice. When a field is absent it keeps its zero value, which means acceptance maintainers or no rule reviewers. validateOptionalPolicy then applies FR-002 to FR-005, with messages that name the rule and the field. The policy digest stays the digest of the exact bytes.")
