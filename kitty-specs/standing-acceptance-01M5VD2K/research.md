# Research: Standing acceptance for automerge rules

**Mission**: `standing-acceptance-01M5VD2K` | **Branch**: `feat/standing-acceptance` | **Date**: 2026-10-26

## Method

Planning read the current code at commit `c9c0194` (Go code identical to `7a47e6952a427ad7e3a3766c221fd334d9331950`) along every path the spec names. It also read the four documents the mission changes, and the tests that pin output, catalogues and traceability. No spike code was written. Each finding below cites the file and line it rests on, so a reviewer can check it. Every open choice became a resolved plan-flow Decision Moment, whose ID appears with its decision.

## Code findings

These facts shape the design. Line numbers refer to commit `c9c0194`.

1. **Older clients drop the `rule` key and then apply the maintainer check.** `verifyEvent` decodes non-resource events with plain `json.Unmarshal` (`event.go` lines 100 to 139), so an unknown key is ignored and the signed bytes are kept for the event ID. An older client then runs `validateMergeEvent` (`policy.go` lines 547 to 568). That check needs a maintainer signer and `requiredAccepts` maintainer acceptance decisions, and `validatePolicy` forces `requiredAccepts` to be at least 1 (`policy.go` line 137). A fact valid only through path (b) lists no decisions, so every older client rejects it (US7-3). A maintainer-signed fact that cites enough decisions passes on an older client whatever its `rule` field says (analysis finding I1), which is why decision `01M5VSEHEVA82N9T64ZH1P1Q2A` makes such a fact valid on new clients too (US7-4).
2. **Older clients reject the new policy fields.** `parsePolicyBytes` uses `DisallowUnknownFields` (`policy.go` line 110), so `acceptance` and `reviewers` fail on older clients even with the value `maintainers` (US7-2).
3. **Two merge-fact validators exist today.** Local projection calls `validateMergeEvent` from `validateEventRelationships` (`store.go` lines 357 to 367). Replication import calls `validateReplicationMergeAt` (`quarantine.go` lines 1207 to 1224 and 1346 to 1367). Both encode the same acceptance rule with different messages and object access.
4. **The `hn serve` actor-chain check reuses replication import.** `checkServeActorDependencies` calls `replicationEventDependencyWithOptions` for every appended event (`serve_verdict_facts.go` lines 512 to 550). A change in the replication merge check therefore covers the fourth fact-admission surface without separate server code.
5. **A projected rule merge fact already counts as merged.** `evaluateProposalEvidence` collects every `proposal.merged` with a matching subject and policy digest (`policy.go` lines 446 to 451). It does not look at the signer or evidence, because projection has already validated the fact. `Merged`, `proposalStatus` and the lineage index (`lineage.go` lines 52 to 56) need no change for rule facts.
6. **Local automerge runs through the manual merge path.** `cmdAutomerge` calls `mergeProposal` with a `proposalMergeGuard` (`automerge.go` line 117). `mergeProposal` requires `evaluation.Accepted` and a maintainer identity at `governance.go` lines 172, 185, 241, 247, 285 and 291. It refuses the repair path whenever a guard is present (`governance.go` line 216).
7. **Policy validation forces executing actors to be maintainers.** See `validateOptionalPolicy`, `protected_checks.go` lines 84 to 88.
8. **Local automerge already compares target and base policy digests.** `inspectAutomerge` blocks with "current target policy differs from the signed base policy" (`automerge.go` lines 297 to 303), so FR-017 holds locally today.
9. **Author approvals can count today.** `evaluateProposalEvidence` counts the author's approval when `allowAuthorApproval` is true (`policy.go` line 373). Designated protected-check approvals always exclude the author (`protected_checks.go` line 233). Rule counting has to recount from `currentReviews` to satisfy FR-006.
10. **Run-result qualification already honors D1 requesters.** Decisions and evaluations use `mayRequestRun` (`policy.go` lines 49 to 55, 401, 527; `quarantine.go` line 1326), so pipeline `requesters` from commit `7a47e69` are used as shipped (C-009).
11. **Shallow verification treats merge evidence as decisions.** `guardShallowEventClosure` classifies every merge evidence ID as a `decision` dependency (`shallow.go` lines 1012 to 1020). Its `run.result` case already requires the answered request (lines 999 to 1002).
12. **The changed-path rule for imports already exists.** `validateProtectedDecisionAt` computes changed paths from the quarantine, then from the accepted store, and never from a mix (`protected_checks.go` lines 267 to 282). `changedPathsAt` refuses output over 8 MiB (lines 194 to 217).
13. **The `hn serve` push-option parser refuses unknown options.** `parseServePushOptions` accepts only `hn-advance` and `hn-expected-tip`, at most two options, and refuses anything else (`serve_hooks.go` lines 664 to 710). The wrapper passes `-c receive.advertisePushOptions=true` (`serve_session.go` line 279).
14. **The `hn serve` verdict requires a maintainer for advancement.** Step 13 of `evaluateServeProtectedUpdate` refuses an advancement requester who is not a maintainer under both policies (`serve_protected.go` lines 183 to 192). Step 10 requires `Ready` and `Accepted` under the base policy (lines 351 to 360).
15. **Existing tests pin three catalogues.**
    - `TestServeReasonsCatalogueOrderAndLookups` compares the reason-code list with `reflect.DeepEqual` (`serve_reasons_test.go` lines 30 to 52).
    - `TestServeReasonsDocsTableMatchesCatalogue` compares the first reason table under "hn serve wire surface" in `docs/protocol-v0.md` with that list (lines 495 to 511). Its parser stops at the first non-table line (lines 450 to 475).
    - `TestServeQualityTraceabilityTestsExist` refuses any traceability row ID other than the `hn serve` IDs (`serve_quality_test.go` lines 831 to 851 and 889 to 898).
16. **The push-option test compares against a constant.** `TestServePushOptions` checks rejection sentences against `servePushOptionSentence` (`serve_push_test.go` lines 437 to 444). Changing the constant's text changes no assertion. The case with three options includes an unknown option and stays refused (line 741).
17. **Existing output assertions are substring checks.** Policy, status and automerge tests use `assertOperationalContains` and `assertPolicyOutputContains` (for example `policy_rules_acceptance_test.go` line 262 and `serve_advance_test.go` line 292), so adding output lines leaves them passing.
18. **Documented examples are guarded.** `TestServeQualityDocumentedExamplesAreExecuted` requires every `<!-- example: NAME -->` marker in `docs/*.md` to be unique and named by some test file (`serve_quality_test.go` lines 96 to 116).
19. **`encoding/json` folds key case and treats null as absent.** A payload key `"RULE"` would fill a tagged `Rule` field, `"rule": null` or `"rule": ""` would decode to the empty string, and `"rule": 1` would be a decode error. A typed field therefore cannot tell a canonical claim from other shapes, and could turn a valid legacy fact into a decode error. Decision `01M5VSEHF1G2YQDQW7HPVBF95K` keeps `Rule` out of `encoding/json` and recognizes only the canonical claim with one token walk.
20. **Legacy payloads may carry rule-named keys.** `verifyEvent` authenticates the original bytes and ignores unknown top-level keys of non-resource events, so a signed `review.submit` with `"rule": "annotation"` is valid today. A refusing key scan would reject the same authenticated bytes, and re-encoding cannot repair immutable history (analysis finding C2). Resource events are decoded strictly (`strictResourceJSON`, `resource_model.go` lines 157 to 182) and keep refusing unknown keys.
21. **The frozen-test baseline moved.** `git diff --numstat 7a47e69 d881d9b -- '*_test.go'` reports `129 12 replication_borrow_test.go`, from the reviewed sync-speedup merge, and `git diff --numstat d881d9b HEAD -- '*_test.go'` is empty at analysis time. The audit snapshot is therefore `d881d9b` (decision `01M5VSEHF9P9VRVTY68PD8DZ7D`).
22. **A remote rerun after recording stops at the merged blocker.** `inspectAutomerge` adds `proposal is already recorded as merged` (`automerge.go` lines 233 to 237), and both `cmdAutomerge` and `automergeRemote` return on `inspection.eligible()` before any publication step (lines 55 to 80 and 654 to 659). The publication-only rerun of decision `01M5VSEHFCZGQHQ14CKJ3MAMP7` runs before both.

## Decisions

### Shared rule evaluator boundary

Decision `01M5VEZEC0EGQWXKCPTJRECHAV`.

- **Decision**: One new file, `rule_acceptance.go`, holds two pure functions. `evaluateRuleAcceptance` judges current evidence, including vetoes. `validateRuleMergeFact` judges a fact against only its listed evidence. Both take the base policy, digest, proposal, rule name, changed paths and pipeline definition digests from the caller and run no Git. `automerge.go` and `serve_protected.go` call the live function. `validateMergeEvent` in `policy.go` and `validateReplicationMergeAt` in `quarantine.go` call the fact function when `rule` is present. The acceptance-fact checks stay as they are. After analysis, the order of the two checks is fixed by `admitMergeFact` (decision `01M5VSEHEVA82N9T64ZH1P1Q2A`): the acceptance-fact check runs first for every fact, and the fact function runs only when it fails and a canonical claim exists.
- **Rationale**: NFR-002 requires the same verdict on every surface, and a single implementation gives that by construction. Callers already own different object sources (finding 3), so keeping I/O out of the functions avoids touching those sources.
- **Alternatives considered**: Adding rule fields to `evaluateProposalEvidence` would change a function that `hn proposal status`, `hn decide`, `hn merge` and the server all use. Adding rule logic separately to each surface would copy the duplication of finding 3 a third time.

### Rule field representation and parse-time checks

Decision `01M5VEZKK32DXWNPRAYGDMY3ZK`, **superseded** by decision `01M5VSEHF1G2YQDQW7HPVBF95K` (rule claim recognition, next section). The text below is kept as the record of the original plan.

- **Decision**: `Event` gains `Rule string` with JSON key `rule` and `omitempty`, after `Evidence`. Every existing payload therefore re-encodes byte-identically. `validateEventContent` refuses a non-empty `Rule` on every kind other than `proposal.merged`, and on `proposal.merged` requires the rule-name grammar (`validPipelineName`: 1 to 64 characters from `A-Z a-z 0-9 - _ .`, excluding `.` and `..`). `verifyEvent` also walks the top-level keys of each non-resource payload. It refuses any key equal to `rule` under Unicode case folding unless the key is exactly `rule`, appears once, sits on `proposal.merged` and holds a non-empty JSON string.
- **Rationale**: FR-010 and the spec's edge cases require parse-time refusal of empty, invalid and misplaced `rule` values. Finding 19 shows that decoding alone cannot see them. One token walk per event costs much less than the ed25519 verification each event already gets.
- **Alternatives considered**: A new event kind contradicts OQ-5 and C-004. Relying on decoding alone accepts `"rule": null` as an acceptance fact, and lets other implementations disagree with Go about case-variant keys.

### Rule claim recognition and union validity

Decisions `01M5VSEHEVA82N9T64ZH1P1Q2A` (specify, amends OQ-5) and `01M5VSEHF1G2YQDQW7HPVBF95K` (plan).

- **Decision**: `Event.Rule` has the tag `json:"-"`. `verifyEvent` keeps every existing check and, for `proposal.merged` only, sets `Rule` through `parseMergeRuleClaim` when the payload carries exactly one top-level key equal to `rule` under case folding, spelled exactly `rule`, with a JSON string value that is a valid rule name. Every other shape yields no claim and no error. Every admission surface calls `admitMergeFact`: today's maintainer check first and unchanged, then path (b) only for a claim. `encodeAndSign` writes only canonical claims.
- **Rationale**: findings 1, 19 and 20. Union validity keeps every fact that verified before the mission valid on every client, and lets old and new clients agree on every maintainer merge.
- **Alternatives considered**: The refusing key scan of the superseded decision rejects legacy bytes (C2). Choosing the validator by the presence of `rule` lets a maintainer fact with enough decisions be valid on old clients and invalid on new ones (I1).

### Activation boundaries

Decision `01M5VSEHF5B1FM9TB52KWKQGWZ`.

- **Decision**: every landing state fails closed through the unchanged path (a); shallow verification refuses rule-only facts until WP09; the `hn serve` hook refuses every `hn-rule` push until WP14; boundary fixtures B1 to B5 prove each state (plan "Activation boundaries").
- **Rationale**: analysis finding C1. One shared combinator and validator leave no surface-specific variant.
- **Alternatives considered**: Landing every admission surface in one work package would exceed one implementer session and still leave the parser and the hook staged separately.

### Decoding acceptance and reviewers

Decision `01M5VEZRTBF8R7RZGHSH5EC6VS`.

- **Decision**: Two named types, `AutomergeAcceptance` and `AutomergeReviewers`. Go calls a named type's `UnmarshalJSON` even for `null`, so these decoders see every present value. They record that the key was present and keep the raw value. `validateOptionalPolicy` then refuses `null`, non-strings and any `acceptance` value other than `maintainers` or `rule`. It also refuses `null`, non-array and empty `reviewers`, and applies FR-002 to FR-005. Every message names the rule and the field (see [contracts/policy-rule-fields.md](contracts/policy-rule-fields.md)). Absence leaves the zero value, which means `maintainers` and no rule reviewers.
- **Refinement during contract design**: The recorded final answer has the decoder itself refuse `null`. The contract moves that refusal into `validateOptionalPolicy`, because an error raised inside the decoder cannot name the rule, and US2-6 requires the message to name the rule and the field. The accepted and refused values are identical.
- **Rationale**: US2-6 lists `null`, `""`, `"Rule"` and `1` as refused values for `acceptance`, and an empty list as refused for `reviewers`. The existing strict decoder stays unchanged.
- **Alternatives considered**: Pointer fields cannot tell `null` from absence. A raw-bytes pre-scan would add a second parse of every policy.

### Rule merge fact evidence set

Decision `01M5VEZXQ7GD8JJ4XNDZD6B2XD`.

- **Decision**: Execution lists, sorted and unique, two kinds of item:
  - the current `approve` review of each counting reviewer;
  - for each base-policy pipeline, the latest passing result of each trusted runner whose request is authorized (`mayRequestRun`) and names the pipeline definition digest at the head.

  A verifier refuses the fact unless every listed item qualifies, with at most one approval per reviewer and one result per runner per pipeline, and unless every threshold is met.
- **Rationale**: OQ-5 accepted strict evidence. Listing every current counting item matches how `evaluation.Evidence` is built today and leaves the signer no choice to make. Judging only listed items keeps validity immutable (FR-015).
- **Alternatives considered**: A minimal subset that just meets thresholds lets the signer pick which approvals appear. Reusing `evaluation.Evidence` would admit author approvals and designated protected-check approvals.

### Changed paths and missing commits

Decision `01M5VF02KFTE4BX3G7THDHRGCV`.

- **Decision**: Extract the one-complete-store lookup from `validateProtectedDecisionAt` into `changedPathsInOneStore` without changing behavior, and use it on the replication path. Local projection keeps `changedPathsAt("")`. Before path (b) evaluation, and only after path (a) failed, replication reports an absent base or head as a missing dependency naming the commit, with recovery `add exact proposal selection --proposal <candidate>`. For a rule fact, the shallow closure requires the base commit, the head commit, the base policy blob and every listed evidence fact. Each fact gets one changed-path computation, memoized per `(base, head)` within a validation pass.
- **Rationale**: OQ-10 and FR-014 require failing closed with recovery guidance. Finding 12 shows that the right rule already exists.
- **Alternatives considered**: Reading base and head from different stores could pair a quarantined head with an unrelated accepted base. Skipping path checks when commits are missing would accept a fact without checking its paths.

### Local execution and local repair

Decision `01M5VF06S28D7WBPBE9K7BCK18`.

- **Decision**: `proposalMergeGuard` gains a rule authority. When it is present, the six accepted and maintainer checks of finding 6 call the authority instead. The authority checks rule eligibility with `evaluateRuleAcceptance` and executing-actor membership. Every other check is unchanged. Recording goes through `appendRuleMergeEvent`, which self-checks the event with `validateRuleMergeFact` before signing. Repair lives in `hn automerge`. Under acceptance mode `rule`, the inspection recognizes a checked-out target that contains the head through exactly one first-parent merge commit (`findProposalMergeCommit`) with the target policy equal to the base policy, and `--execute` records the missing fact once.
- **Rationale**: FR-009 keeps every blocker except per-proposal acceptance. FR-020 requires the same command to repair, and the guard refuses repair today.
- **Alternatives considered**: A separate rule merge function would duplicate the lease, checkout and rollback logic. Letting `hn merge` repair rule merges contradicts OQ-9.

### Wire form of a rule advancement

Decision `01M5VF0BASK4QQCTST8GSEDP5K`.

- **Decision**: A third push option, `hn-rule=NAME`. It is valid only with `hn-advance`, appears at most once, and its value must match the rule-name grammar. `servePushOptionLimit` becomes 3, and `servePushOptionSentence` names all three options.
- **Rationale**: The spec assumes the push-option channel. Older servers refuse the option (finding 13), which fails closed. Finding 16 shows no frozen assertion changes.
- **Alternatives considered**: Folding the rule into `hn-advance` would change the grammar of an existing option that older servers parse strictly. A new ref namespace would add a ref class the server has to classify.

### Protected verdict steps for a rule

Decision `01M5VF0G52PH940GQ1BWX0X9TR`.

- **Decision**: When `hn-rule` is present:
  - steps 1 to 9 run unchanged, and a maintainer rejection is still reported as `protected-blocked`;
  - new step 7a refuses a destination digest that differs from the base digest, with `protected-destination-policy`;
  - `evaluateRuleAcceptance` replaces step 10: vetoes report `protected-rule-veto` and every other rule failure reports `protected-rule-not-satisfied`;
  - step 11 is skipped, because the destination policy equals the base policy;
  - step 12 is unchanged;
  - step 13 requires an executing actor of the named rule and reports `advance-not-rule-executor`.
- **Rationale**: FR-016 to FR-018. The existing order keeps the race and lineage answers identical, and each new code maps to one step.
- **Alternatives considered**: A separate rule table before the protected table would duplicate stale-tip, fast-forward and code checks.

### Reason-code catalogue

Decision `01M5VF0M0JGZKDM1MAESM0HCZ2`.

- **Decision**: Add `serveRuleReasonCatalogue` with the three codes, consulted by `serveReasonLookup`. Document it in a second table under the heading "Reason codes for rule advancement" in `docs/protocol-v0.md`, plus a new drift test. `serveReasonCatalogue` stays byte-identical.
- **Rationale**: Finding 15 and C-003. This is recorded in Complexity Tracking.
- **Alternatives considered**: Appending to the existing catalogue fails a frozen assertion. Reusing existing codes contradicts FR-016.

### Older-client evidence

Decision `01M5VF0QVPQ871M0DA8Q0SG5CX`, **amended** by decision `01M5VSEHFPWQWFBNTP9ZFYP3TR`: one owning parent test, `TestStandingCompatOlderClient`, builds the binary in its own temporary directory and runs every consumer as a subtest, with no process-wide cache, and the tests cover agreement on maintainer merges and legacy history. The pre-existing test audit uses the separate snapshot `d881d9b` (decision `01M5VSEHF9P9VRVTY68PD8DZ7D`).

- **Decision**: `standing_compat_test.go` extracts commit `7a47e6952a427ad7e3a3766c221fd334d9331950` with `git archive` into `t.TempDir()`, builds it with `go build`, and drives it against repositories produced by the new binary. It skips with a visible reason under `-short` and when the commit object is absent.
- **Rationale**: An executable check is stronger than a transcript, and a standard-library build needs no network.
- **Alternatives considered**: Recorded review evidence only, which a later change could silently invalidate.

### Documentation examples and traceability

Decision `01M5VF0W7NWESJH5RAD46NSYMN`.

- **Decision**: Example markers named `standing-...` go in `docs/governance-v0.md`, `docs/protocol-v0.md` and `docs/serve-v0.md`. Each one is executed by the `standing_*_test.go` file that owns the behavior; the contracts name the file for each example. Traceability for FR, NFR, SC and US rows goes in a new section of `docs/governance-v0.md` titled "Standing acceptance traceability", with its own checking test.
- **Rationale**: Findings 15 and 18.
- **Alternatives considered**: Rows in `docs/serve-v0.md` would collide with the `hn serve` IDs.

### Remote repair

Decision `01M5VF10Q3SQX8WTK2MEN92ZSC`, **extended** by decision `01M5VSEHFCZGQHQ14CKJ3MAMP7`: an authorized publication-only rerun handles the recorded-but-unpublished states (finding 22), and both paths belong to WP19.

- **Decision**: `automergeRemote` takes a repair branch when the hosted tip equals or descends from the head and the local target still equals the signed base. It re-runs the rule inspection against the imported facts and calls `mergeRemoteBound` with the rule binding, whose step 5 records the missing fact once with `commit` equal to the head.
- **Rationale**: FR-020. Today a rerun stops at the local-equals-hosted check (`automerge.go` lines 669 to 676).
- **Alternatives considered**: Requiring `hn merge --remote` would need a maintainer.

## Verified assumptions from the spec

| Spec assumption | Verification | Result |
|----|----|----|
| The advancement request can carry the rule name through the push-option channel | Finding 13; the wrapper advertises push options | Holds; new option `hn-rule` |
| Older clients reject the new fields and rule facts | Findings 1 and 2 | Holds for every fact valid only through path (b). An older client accepts a maintainer-signed fact with enough decisions whatever its `rule` field says; under union validity the new client accepts it too, so they agree |
| The set of required pipelines is every pipeline in the base policy | `evaluateProposalEvidence` iterates `policy.Pipelines` (`policy.go` lines 383 to 425) | Holds |
| D1 `requesters` is used as shipped | Finding 10 | Holds |
| Manual `hn merge` and automerge under acceptance mode `maintainers` keep their behavior | Finding 6; the rule authority is present only for acceptance mode `rule` | Holds by design; NFR-004 tests confirm |
| Policy actors are distinct full fingerprints | `validateActorList` (`policy.go` lines 185 to 197) | Holds; a rule may list one actor as executing actor, author and reviewer, and independence comes from FR-004 and FR-006 |

## Premortem

The question here is how this mission would fail if it shipped as planned, and what the plan does about each failure mode.

| Failure mode | Likelihood | Mitigation in the plan |
|----|----|----|
| The executing actor signs a fact that its own projection rejects, and its clone refuses every later command | Medium without a guard | `appendRuleMergeEvent` builds the event, runs `validateRuleMergeFact` over the same events and only then calls `appendEvent`. A property test executes every valid fixture and admits every recorded fact on all four surfaces |
| A verifier accepts a fact another verifier rejects | Low | One fact function; NFR-002 agreement test over every valid and invalid fixture |
| A rule-key rule rejects a historical event | Removed by design: no `rule` key is ever a parse error | Signed legacy fixtures L01 to L06 on every surface and against the older client |
| Projection slows in repositories with many rule merges | Medium for the lab use case | Memo per `(base, head)` and pipeline digests per head within a pass; NFR-006 timing; follow-up issue if the gate host measures above the bound |
| The server and the client preview disagree about a veto | Medium, because they hold different fact sets | The client imports with an all-actor selection first (existing `automergeUnselectedActors`); the server judges its own facts and wins; the agreement test holds branch, policy, code and facts identical and compares rule satisfaction only (decision `01M5VSEHFH0B1BSJ1NA923FKAA`) |
| A rule with acceptance mode `rule` covers a protected path through an ancestor selector | Low | FR-003 checks equality, ancestor and descendant overlap. Runtime blockers for policy and protected changes stay in both modes |
| The new step 7a refuses ordinary maintainer advancements | None; it runs only with `hn-rule` | The existing serve tests run unchanged |
| A rule merge fact for a candidate whose code ref is not hosted reaches `hn serve` | Low | Missing base or head becomes `dependency-missing`; the actor push is refused until the code arrives |
| Pipeline requesters are not selected on a remote, so results do not count | Medium for partial selections | The threshold is then unmet, which fails closed; the guide recommends `--all-trusted` |

## Adversarial evidence

No dependency is added, upgraded or removed, so the dependency rule of `contracts/adversarial-evidence-contract.md` does not apply. Because the mission moves acceptance authority to a non-maintainer, planning challenged the trust design itself by looking for ways a hostile executing actor, author or reviewer could gain authority. Each contested finding is listed with its disposition.

| # | Challenge | Disposition | Detail |
|----|----|----|----|
| A1 | An executing actor cites approvals or results from another proposal | `accepted` (covered) | Every review must name the candidate as subject. Every result's request must name the candidate, its head and the head's pipeline digest. Fixture defects cover each case |
| A2 | An executing actor omits a current change request from the evidence and records a merge on an ordinary remote with a modified client | `accepted` with documentation | OQ-8: facts are judged by their own evidence, as acceptance facts are today, and a malicious maintainer has the same power. `hn serve` enforces vetoes at advancement. `docs/threat-model.md` records the residual risk |
| A3 | A rule allows `docs/` and the candidate replaces `docs` with a symlink into `.hn/` | `accepted` (covered) | `automergePathAllowed` never allows the ancestor `docs` itself (`automerge.go` lines 316 to 327). The policy is read from the `.hn/policy.json` tree path, and Git does not follow symlinks for `git show` |
| A4 | A maintainer adopts a rule over `scripts/` while protected checks cover `scripts/lint.sh` | `changed` | The spec's overlap rule (FR-003) is implemented as equal, ancestor or descendant in either direction, with a fixture for each direction |
| A5 | A rule merge fact uses `"RULE"` or `"rule": null` so that verifiers disagree | `changed` | The key scan of decision `01M5VEZKK32DXWNPRAYGDMY3ZK` was added during this challenge, and replaced after analysis by canonical-claim recognition (decision `01M5VSEHF1G2YQDQW7HPVBF95K`): every non-canonical shape is no claim on every verifier, so verifiers agree without refusing legacy bytes |
| A6 | The executing actor is also a listed reviewer and approves its own author's candidate | `accepted` | Spec assumption: independence comes from excluding the author's own approval and from the one-approval minimum |
| A7 | A case-variant directory such as `.HN/` evades the `.hn` refusal | `deferred_with_rationale` | Git tree paths are case-sensitive and Hubnot reads only `.hn/...`. A case-variant directory is ordinary content. Revisit if Hubnot ever reads policy case-insensitively |
| A8 | An actor requests advancement under a rule whose target branch is a different branch | `accepted` (covered) | Step 10 refuses a rule whose `targetBranch` does not match the pushed ref, with `protected-rule-not-satisfied` |

A full adversarial-squad run is better spent on the implemented verifier during work-package review, where it can exercise real fixtures. Disposition for the squad run: `deferred_with_rationale`.
