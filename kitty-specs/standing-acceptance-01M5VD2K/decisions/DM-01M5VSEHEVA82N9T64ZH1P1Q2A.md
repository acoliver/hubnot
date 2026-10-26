# Decision Moment `01M5VSEHEVA82N9T64ZH1P1Q2A`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `specify`
- **Slot key:** `specify.protocol.merge-fact-validity`
- **Input key:** `rule_merge_fact_validity_union`
- **Status:** `resolved`
- **Created:** `2026-10-26T09:42:44.955155+00:00`
- **Resolved:** `2026-10-26T09:42:44.958904+00:00`
- **Resolved by:** `maintainer (delegated remediation instruction)`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

Amends OQ-5 (decision 01M5VD5Z139R84BBXZM0QXGHJ1) after analysis findings C1, C2 and I1: when is a proposal.merged fact valid, given previously valid signed history and clients built before this mission?

## Options

- Union: valid if it passes the pre-mission maintainer check whatever its rule field says, or carries a canonical rule claim and passes strict rule validation
- The rule field selects the validator, and rule keys are refused outside proposal.merged
- Other

## Final answer

Decision A, union semantics. A proposal.merged fact is valid if EITHER (a) it passes the pre-mission maintainer-merge validation exactly (policy equals the base policy digest, a base-policy maintainer signed it, and its evidence holds enough current maintainer accept decisions), whatever its rule field says, OR (b) it carries a canonical rule claim and passes the new strict rule validation (named rule exists with acceptance rule, signer is one of its executing actors, author and every changed path allowed, no policy or protected-check path changed, and evidence is exactly the qualifying approvals and passing run results, one per counting reviewer and one per trusted runner per pipeline). A fact that passes neither is invalid. A canonical rule claim is exactly one top-level key equal to rule under case folding, spelled exactly rule, holding a JSON string that matches the rule-name grammar; any other shape is no claim and is never a parse error. Facts passing (a) are maintainer merges and are displayed as such; an unverified rule annotation on them may be reported but never makes them invalid. Strict evidence (OQ-5) applies only to path (b). There is no unconditional scan for a rule key: event kinds other than proposal.merged keep the pre-mission decoding and acceptance, so previously valid signed history, including legacy events with unknown or rule-named fields, stays valid, and signed legacy fixtures prove it on every admission surface and against the older client. Older and new clients agree on every fact that passes (a), including a rule-plus-maintainer-decisions fact, which both accept; older clients reject facts valid only through (b), and WP17 tests both. This decision supersedes the parse-time and evidence wording of OQ-5; the rest of OQ-5 (optional rule string on proposal.merged, base policy digest, no new event kind, maintainers-mode facts unchanged) stands.

## Rationale

The analysis showed that a parse-time refusal of rule keys invalidates authenticated legacy bytes (C2), that staged parser and admission work let surfaces disagree about rule-bearing maintainer facts (C1), and that older clients accept a maintainer-signed fact with enough decisions whatever its rule field says (I1). Union semantics keep every fact that passes the pre-mission check valid on every client and confine the new semantics to facts that need the rule path. The maintainer made this decision; the resolved OQ-5 record is immutable in the ledger, so this entry amends it.

## Change log

- `2026-10-26T09:42:44.955155+00:00` — opened
- `2026-10-26T09:42:44.958904+00:00` — resolved (final_answer="Decision A, union semantics. A proposal.merged fact is valid if EITHER (a) it passes the pre-mission maintainer-merge validation exactly (policy equals the base policy digest, a base-policy maintainer signed it, and its evidence holds enough current maintainer accept decisions), whatever its rule field says, OR (b) it carries a canonical rule claim and passes the new strict rule validation (named rule exists with acceptance rule, signer is one of its executing actors, author and every changed path allowed, no policy or protected-check path changed, and evidence is exactly the qualifying approvals and passing run results, one per counting reviewer and one per trusted runner per pipeline). A fact that passes neither is invalid. A canonical rule claim is exactly one top-level key equal to rule under case folding, spelled exactly rule, holding a JSON string that matches the rule-name grammar; any other shape is no claim and is never a parse error. Facts passing (a) are maintainer merges and are displayed as such; an unverified rule annotation on them may be reported but never makes them invalid. Strict evidence (OQ-5) applies only to path (b). There is no unconditional scan for a rule key: event kinds other than proposal.merged keep the pre-mission decoding and acceptance, so previously valid signed history, including legacy events with unknown or rule-named fields, stays valid, and signed legacy fixtures prove it on every admission surface and against the older client. Older and new clients agree on every fact that passes (a), including a rule-plus-maintainer-decisions fact, which both accept; older clients reject facts valid only through (b), and WP17 tests both. This decision supersedes the parse-time and evidence wording of OQ-5; the rest of OQ-5 (optional rule string on proposal.merged, base policy digest, no new event kind, maintainers-mode facts unchanged) stands.")
