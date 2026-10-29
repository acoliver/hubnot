# Decision Moment `01M5VF06S28D7WBPBE9K7BCK18`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `plan`
- **Slot key:** `plan.execution.local-authority`
- **Input key:** `local_rule_execution`
- **Status:** `resolved`
- **Created:** `2026-10-26T06:40:09.506817+00:00`
- **Resolved:** `2026-10-26T06:40:11.649754+00:00`
- **Resolved by:** `llxprt`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

How does local hn automerge --execute under acceptance mode rule reuse the native merge path without the maintainer-acceptance checks, and how does it repair an interrupted merge?

## Options

- Rule authority carried by proposalMergeGuard; repair inside hn automerge
- Separate rule merge function duplicating mergeProposal
- Let hn merge repair rule merges
- Other

## Final answer

proposalMergeGuard gains a rule authority: the rule name plus a check built on evaluateRuleAcceptance. When the guard carries it, mergeProposal replaces only the accepted check and the maintainer-identity check with the rule check and executing-actor membership. Every other check stays: rejection, lineage, code, clean tree, target, guardTargetProtectedPolicy and the expected-old lease. The fact is recorded with appendRuleMergeEvent. With no guard, or with an acceptance-mode guard, every line of today's path runs unchanged. Repair runs in hn automerge itself. Under acceptance mode rule, inspectAutomerge recognizes a target that contains the head through exactly one first-parent merge commit (findProposalMergeCommit) and whose policy equals the base policy. --execute then records the missing rule merge fact once, without merging. hn merge repair stays maintainer-only.

## Rationale

Plan-phase decision made by the planning agent under the maintainer's delegated non-interactive instruction (spec confirmed, OQ-1..11 accepted). FR-009 keeps every existing blocker except per-proposal acceptance, and FR-020 requires the same command to repair. The guard already exists to add policy gates without duplicating merge and repair semantics, and today it refuses repair (governance.go line 216), so repair has to live in automerge.

## Change log

- `2026-10-26T06:40:09.506817+00:00` — opened
- `2026-10-26T06:40:11.649754+00:00` — resolved (final_answer="proposalMergeGuard gains a rule authority: the rule name plus a check built on evaluateRuleAcceptance. When the guard carries it, mergeProposal replaces only the accepted check and the maintainer-identity check with the rule check and executing-actor membership. Every other check stays: rejection, lineage, code, clean tree, target, guardTargetProtectedPolicy and the expected-old lease. The fact is recorded with appendRuleMergeEvent. With no guard, or with an acceptance-mode guard, every line of today's path runs unchanged. Repair runs in hn automerge itself. Under acceptance mode rule, inspectAutomerge recognizes a target that contains the head through exactly one first-parent merge commit (findProposalMergeCommit) and whose policy equals the base policy. --execute then records the missing rule merge fact once, without merging. hn merge repair stays maintainer-only.")
