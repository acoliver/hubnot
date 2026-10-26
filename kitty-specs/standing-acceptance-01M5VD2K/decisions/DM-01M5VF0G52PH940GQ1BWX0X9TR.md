# Decision Moment `01M5VF0G52PH940GQ1BWX0X9TR`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `plan`
- **Slot key:** `plan.serve.verdict-steps`
- **Input key:** `serve_rule_verdict`
- **Status:** `resolved`
- **Created:** `2026-10-26T06:40:19.106589+00:00`
- **Resolved:** `2026-10-26T06:40:21.099026+00:00`
- **Resolved by:** `llxprt`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

Where do the rule checks sit in the hn serve protected-branch table, and which reason code reports each failure?

## Options

- Rule checks replace steps 10, 11 and 13 when hn-rule is present
- Separate rule table before the protected table
- Rule checks after the existing table
- Other

## Final answer

With hn-rule present, steps 1 to 9 run unchanged; a maintainer rejection is still reported as protected-blocked. A new step 7a requires the destination policy digest to equal the base digest and reports protected-destination-policy with a revise next action. evaluateRuleAcceptance replaces step 10. A current trusted change request or trusted failure is reported as protected-rule-veto, naming the blocking events. An unknown rule, acceptance mode maintainers, a rule target branch other than the pushed ref, a disallowed author or path, a policy or protected-check change, or unmet thresholds is reported as protected-rule-not-satisfied. Step 11 is skipped because the destination policy is the base policy. Step 12 runs unchanged. Step 13 requires the mapped actor to be an executing actor of the named rule in the base policy and reports advance-not-rule-executor. Without hn-rule nothing changes.

## Rationale

Plan-phase decision made by the planning agent under the maintainer's delegated non-interactive instruction (spec confirmed, OQ-1..11 accepted). FR-016, FR-017 and FR-018 (OQ-6, OQ-7). Keeping the existing step order means the stale-tip race, fast-forward and blocking-lineage answers stay identical, and each new code maps to one step.

## Change log

- `2026-10-26T06:40:19.106589+00:00` — opened
- `2026-10-26T06:40:21.099026+00:00` — resolved (final_answer="With hn-rule present, steps 1 to 9 run unchanged; a maintainer rejection is still reported as protected-blocked. A new step 7a requires the destination policy digest to equal the base digest and reports protected-destination-policy with a revise next action. evaluateRuleAcceptance replaces step 10. A current trusted change request or trusted failure is reported as protected-rule-veto, naming the blocking events. An unknown rule, acceptance mode maintainers, a rule target branch other than the pushed ref, a disallowed author or path, a policy or protected-check change, or unmet thresholds is reported as protected-rule-not-satisfied. Step 11 is skipped because the destination policy is the base policy. Step 12 runs unchanged. Step 13 requires the mapped actor to be an executing actor of the named rule in the base policy and reports advance-not-rule-executor. Without hn-rule nothing changes.")
