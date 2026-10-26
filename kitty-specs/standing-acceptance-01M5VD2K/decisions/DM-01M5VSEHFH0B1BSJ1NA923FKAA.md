# Decision Moment `01M5VSEHFH0B1BSJ1NA923FKAA`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `specify`
- **Slot key:** `specify.quality.verdict-agreement`
- **Input key:** `rule_verdict_agreement_scope`
- **Status:** `resolved`
- **Created:** `2026-10-26T09:42:44.977456+00:00`
- **Resolved:** `2026-10-26T09:42:44.979899+00:00`
- **Resolved by:** `maintainer (delegated remediation instruction)`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

What does NFR-002 agreement compare, given that the keyless preview has no actor input while hn serve checks the requester (analysis finding I4)?

## Options

- Rule satisfaction under identical inputs; authorization and transport checks listed and tested separately
- Add an actor-aware preview
- Other

## Final answer

Decision E. NFR-002 has two parts. Fact agreement: the four fact-admission surfaces reach the same verdict on every valid, invalid and legacy merge-fact fixture. Rule-satisfaction agreement: under identical inputs (the same target branch tip, base and target policy, candidate code and fact set), the keyless preview, local execution and the hn serve verdict reach the same rule-satisfaction outcome for every advancement fixture. Executor authorization (identity listed in the rule actors locally; step 13 advance-not-rule-executor on hn serve) and transport and binding checks (hn serve remote identification, selection coverage, local target equals hosted tip, hn-expected-tip and stale tip, fast-forward, code ref hosting, hn-rule support, protected baseline, clean worktree and repository state, the expected-old lease) are excluded from agreement, listed explicitly, and tested on their own. Preview against local execution is tested in WP11, preview against hn serve in WP15, fact agreement in WP16.

## Rationale

An unrelated actor obtains an eligible keyless preview of a satisfied rule while its own push must fail advance-not-rule-executor, so agreement over every advancement verdict is unrealizable. Separating the shared rule derivation from requester and transport checks keeps a strong, testable claim. The maintainer made this decision.

## Change log

- `2026-10-26T09:42:44.977456+00:00` — opened
- `2026-10-26T09:42:44.979899+00:00` — resolved (final_answer="Decision E. NFR-002 has two parts. Fact agreement: the four fact-admission surfaces reach the same verdict on every valid, invalid and legacy merge-fact fixture. Rule-satisfaction agreement: under identical inputs (the same target branch tip, base and target policy, candidate code and fact set), the keyless preview, local execution and the hn serve verdict reach the same rule-satisfaction outcome for every advancement fixture. Executor authorization (identity listed in the rule actors locally; step 13 advance-not-rule-executor on hn serve) and transport and binding checks (hn serve remote identification, selection coverage, local target equals hosted tip, hn-expected-tip and stale tip, fast-forward, code ref hosting, hn-rule support, protected baseline, clean worktree and repository state, the expected-old lease) are excluded from agreement, listed explicitly, and tested on their own. Preview against local execution is tested in WP11, preview against hn serve in WP15, fact agreement in WP16.")
