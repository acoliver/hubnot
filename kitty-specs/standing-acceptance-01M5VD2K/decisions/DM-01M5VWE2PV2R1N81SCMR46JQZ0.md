# Decision Moment `01M5VWE2PV2R1N81SCMR46JQZ0`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `plan`
- **Slot key:** `plan.quality.agreement-target-tips`
- **Input key:** `agreement_fixture_target_tips`
- **Status:** `resolved`
- **Created:** `2026-10-26T10:34:55.579509+00:00`
- **Resolved:** `2026-10-26T10:34:55.582348+00:00`
- **Resolved by:** `maintainer (delegated remediation instruction)`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

Which target tip does each rule-satisfaction agreement fixture use, given that the FR-017 fixture needs a target policy that differs from the base policy (analysis round 2 finding I4)?

## Options

- Each fixture declares its target tip; require equality across the compared surfaces; the FR-017 fixture puts both targets at the same amendment commit with the head descending from it
- Require target equal to the signed base for every fixture
- Other

## Final answer

Each agreement fixture declares its target tip, and the test requires that tip to be equal across the compared surfaces: for WP11 T094 the preview and the execution run in the same clone at that tip; for WP15 T095 E's checked-out main and the hosted main both equal it. The declared tip is the candidate's signed base for every fixture except the target-policy (FR-017) fixture. That fixture follows WP14 T068 step 6: M lands a policy amendment H1 on main, A builds the candidate's head on top of H1 and opens it with --base the older commit, and both compared targets are at H1. The target differs from the signed base by construction and the test does not assert target equals base for it; because the head descends from H1, hn serve passes steps 1 to 7 and refuses at step 7a with protected-destination-policy. Authorization and transport stay in their separate tests as decision 01M5VSEHFH0B1BSJ1NA923FKAA requires.

## Rationale

A universal target-equals-base assertion contradicts a fixture whose target policy must differ from the base policy, because one commit has one policy digest. Equality across surfaces is what NFR-002 requires. Refines the fixture recipe of decision 01M5VSEHFH0B1BSJ1NA923FKAA without changing it. Applied to WP11 T094, WP15 T095 and contracts/serve-rule-advancement.md Agreement.

## Change log

- `2026-10-26T10:34:55.579509+00:00` — opened
- `2026-10-26T10:34:55.582348+00:00` — resolved (final_answer="Each agreement fixture declares its target tip, and the test requires that tip to be equal across the compared surfaces: for WP11 T094 the preview and the execution run in the same clone at that tip; for WP15 T095 E's checked-out main and the hosted main both equal it. The declared tip is the candidate's signed base for every fixture except the target-policy (FR-017) fixture. That fixture follows WP14 T068 step 6: M lands a policy amendment H1 on main, A builds the candidate's head on top of H1 and opens it with --base the older commit, and both compared targets are at H1. The target differs from the signed base by construction and the test does not assert target equals base for it; because the head descends from H1, hn serve passes steps 1 to 7 and refuses at step 7a with protected-destination-policy. Authorization and transport stay in their separate tests as decision 01M5VSEHFH0B1BSJ1NA923FKAA requires.")
