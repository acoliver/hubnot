# Decision Moment `01M5VEZEC0EGQWXKCPTJRECHAV`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `plan`
- **Slot key:** `plan.architecture.shared-rule-evaluator`
- **Input key:** `rule_evaluator_boundary`
- **Status:** `resolved`
- **Created:** `2026-10-26T06:39:44.512269+00:00`
- **Resolved:** `2026-10-26T06:39:47.203419+00:00`
- **Resolved by:** `llxprt`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

Where does the standing-acceptance rule derivation live so that preview, execution, the hn serve verdict and the four fact-admission surfaces reach the same verdict?

## Options

- One new file rule_acceptance.go with two pure functions shared by every caller
- Extend evaluateProposalEvidence with rule fields
- Separate implementations per surface, like today's two merge-fact validators
- Other

## Final answer

One new file rule_acceptance.go with two pure functions: evaluateRuleAcceptance (live: current evidence, including vetoes) and validateRuleMergeFact (immutable: the fact's listed evidence only). Both receive the base policy, its digest, the proposal, the rule name, the changed paths and the pipeline definition digests from the caller and do no Git I/O. Callers load objects: automerge.go and serve_protected.go call the live function; store.go (validateMergeEvent) and quarantine.go (validateReplicationMergeAt, which also serves the hn serve actor-chain check) call the fact function. The existing acceptance merge fact checks stay unchanged and still run when rule is absent.

## Rationale

Plan-phase decision made by the planning agent under the maintainer's delegated non-interactive instruction (spec confirmed, OQ-1..11 accepted). NFR-002 requires the same verdict on every surface, which one implementation gives by construction. Today store.go and quarantine.go carry two copies of the merge-fact check (DIRECTIVE_044). Keeping I/O in callers lets the local, quarantine and server object sources stay as they are.

## Change log

- `2026-10-26T06:39:44.512269+00:00` — opened
- `2026-10-26T06:39:47.203419+00:00` — resolved (final_answer="One new file rule_acceptance.go with two pure functions: evaluateRuleAcceptance (live: current evidence, including vetoes) and validateRuleMergeFact (immutable: the fact's listed evidence only). Both receive the base policy, its digest, the proposal, the rule name, the changed paths and the pipeline definition digests from the caller and do no Git I/O. Callers load objects: automerge.go and serve_protected.go call the live function; store.go (validateMergeEvent) and quarantine.go (validateReplicationMergeAt, which also serves the hn serve actor-chain check) call the fact function. The existing acceptance merge fact checks stay unchanged and still run when rule is absent.")
