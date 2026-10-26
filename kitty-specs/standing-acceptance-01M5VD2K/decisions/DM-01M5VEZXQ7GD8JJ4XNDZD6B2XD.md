# Decision Moment `01M5VEZXQ7GD8JJ4XNDZD6B2XD`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `plan`
- **Slot key:** `plan.protocol.evidence-set`
- **Input key:** `rule_evidence_set`
- **Status:** `resolved`
- **Created:** `2026-10-26T06:40:00.231602+00:00`
- **Resolved:** `2026-10-26T06:40:02.905158+00:00`
- **Resolved by:** `llxprt`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

Which event IDs does a rule merge fact list as evidence, and how does a verifier judge them?

## Options

- All current counting approvals and latest passing results, each item must qualify
- Minimal set that just meets thresholds
- Same evidence as evaluation.Evidence
- Other

## Final answer

Execution lists, sorted and unique, the current approve review of each counting reviewer and, for each base-policy pipeline, the latest passing result of each trusted runner. A counting reviewer is in trustedReviewers, is in the rule's reviewers when that field is set, and is never the author under acceptance mode rule. A result counts when its request was signed by a maintainer or a listed requester and names the pipeline definition digest at the head. A verifier requires every listed item to qualify on those terms, with at most one approval per reviewer and at most one result per runner per pipeline. The approvals must reach proposals.requiredApprovals and each pipeline must reach its requiredResults. Any other item invalidates the fact, including decisions, change requests, failed results, approvals the rule does not count and results whose request is not authorized.

## Rationale

Plan-phase decision made by the planning agent under the maintainer's delegated non-interactive instruction (spec confirmed, OQ-1..11 accepted). OQ-5 accepted strict evidence: exactly the qualifying items, one per counting actor. Listing every current counting item, not a minimal subset, matches how evaluation.Evidence is built today and removes any choice the signer could make. Judging only listed items keeps fact validity immutable (FR-015).

## Change log

- `2026-10-26T06:40:00.231602+00:00` — opened
- `2026-10-26T06:40:02.905158+00:00` — resolved (final_answer="Execution lists, sorted and unique, the current approve review of each counting reviewer and, for each base-policy pipeline, the latest passing result of each trusted runner. A counting reviewer is in trustedReviewers, is in the rule's reviewers when that field is set, and is never the author under acceptance mode rule. A result counts when its request was signed by a maintainer or a listed requester and names the pipeline definition digest at the head. A verifier requires every listed item to qualify on those terms, with at most one approval per reviewer and at most one result per runner per pipeline. The approvals must reach proposals.requiredApprovals and each pipeline must reach its requiredResults. Any other item invalidates the fact, including decisions, change requests, failed results, approvals the rule does not count and results whose request is not authorized.")
