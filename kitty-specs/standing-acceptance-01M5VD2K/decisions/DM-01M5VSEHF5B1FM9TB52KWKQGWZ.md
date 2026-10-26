# Decision Moment `01M5VSEHF5B1FM9TB52KWKQGWZ`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `plan`
- **Slot key:** `plan.rollout.activation-boundaries`
- **Input key:** `rule_path_activation_boundaries`
- **Status:** `resolved`
- **Created:** `2026-10-26T09:42:44.965332+00:00`
- **Resolved:** `2026-10-26T09:42:44.967158+00:00`
- **Resolved by:** `maintainer (delegated remediation instruction)`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

How do independently landable work packages avoid exposing inconsistent validation of rule-bearing facts or of the hn-rule push option (analysis finding C1)?

## Options

- Fail closed at every boundary through one shared path (b) validator and combinator, with boundary fixtures
- Land all admission surfaces atomically in one work package
- Other

## Final answer

Decision B. At every independently landable WP boundary, a surface that does not yet implement path (b) rejects facts valid only through (b) and does not interpret a rule, because it still runs only the unchanged maintainer check. One shared implementation serves every consumer: parseMergeRuleClaim (event.go, WP03), validateRuleMergeFact and the union combinator admitMergeFact (rule_acceptance.go, WP05). Local projection, replication import with the hn serve actor-chain check, and shallow verification each call admitMergeFact and no surface implements its own variant. Shallow verification refuses rule-path facts with an explicit interim refusal from WP07 until WP09 enables its closure rules. On hn serve, WP13 parses hn-rule but evaluateServePush refuses every update that carries it with unsupported-command, as a server built before the mission does, until WP14 replaces that refusal with the rule steps. Boundary fixtures: a valid rule-only fact (accepted exactly by the surfaces that implement (b)), downgrade-shaped maintainer facts with a rule naming an absent rule or an existing standing rule plus enough maintainer decisions (accepted everywhere at every boundary), a defective rule fact (refused everywhere), and a maintainer advancement carrying hn-rule (refused with hosted refs unchanged at every boundary). TestStandingFactBoundary holds the surface table that WP07, WP08 and WP09 update.

## Rationale

Union semantics make the maintainer path the safe default, so a surface that has not been extended already fails closed for rule-only facts. A shared validator and combinator remove the per-surface variants the analysis warned about, and the fixtures make each boundary observable.

## Change log

- `2026-10-26T09:42:44.965332+00:00` — opened
- `2026-10-26T09:42:44.967158+00:00` — resolved (final_answer="Decision B. At every independently landable WP boundary, a surface that does not yet implement path (b) rejects facts valid only through (b) and does not interpret a rule, because it still runs only the unchanged maintainer check. One shared implementation serves every consumer: parseMergeRuleClaim (event.go, WP03), validateRuleMergeFact and the union combinator admitMergeFact (rule_acceptance.go, WP05). Local projection, replication import with the hn serve actor-chain check, and shallow verification each call admitMergeFact and no surface implements its own variant. Shallow verification refuses rule-path facts with an explicit interim refusal from WP07 until WP09 enables its closure rules. On hn serve, WP13 parses hn-rule but evaluateServePush refuses every update that carries it with unsupported-command, as a server built before the mission does, until WP14 replaces that refusal with the rule steps. Boundary fixtures: a valid rule-only fact (accepted exactly by the surfaces that implement (b)), downgrade-shaped maintainer facts with a rule naming an absent rule or an existing standing rule plus enough maintainer decisions (accepted everywhere at every boundary), a defective rule fact (refused everywhere), and a maintainer advancement carrying hn-rule (refused with hosted refs unchanged at every boundary). TestStandingFactBoundary holds the surface table that WP07, WP08 and WP09 update.")
