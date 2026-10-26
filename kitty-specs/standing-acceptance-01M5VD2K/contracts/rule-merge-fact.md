# Contract: rule merge fact

Covers FR-010 to FR-015, FR-022 (event part), FR-024, FR-025, NFR-001, NFR-002 and NFR-005. A rule merge fact is a `proposal.merged` event (protocol `hn/0`) that carries a canonical rule claim naming an automerge rule with acceptance mode `rule` in the candidate's base policy, and that is valid through path (b) of the spec invariant. Decisions `01M5VSEHEVA82N9T64ZH1P1Q2A` (union validity), `01M5VSEHF1G2YQDQW7HPVBF95K` (rule claim) and `01M5VSEHF5B1FM9TB52KWKQGWZ` (activation boundaries) govern this contract.

## Wire shape

```json
{
  "protocol": "hn/0",
  "kind": "proposal.merged",
  "actor": "<executor fingerprint>",
  "actorName": "integrator",
  "publicKey": "<base64 ed25519 key>",
  "sequence": 12,
  "timestamp": "2026-10-26T09:14:03.120Z",
  "previous": "sha256:<previous event>",
  "subject": "sha256:<candidate>",
  "head": "<candidate head commit>",
  "commit": "<result commit>",
  "policy": "sha256:<base policy digest>",
  "evidence": ["sha256:<approval>", "sha256:<passing run result>"],
  "rule": "ordinary"
}
```

`rule` is the last key. `Event.Rule` carries the tag `json:"-"`, so `encoding/json` never reads or writes it; `encodeAndSign` appends `rule` after every other field only when `Rule` is set. Acceptance merge facts and every other event therefore encode exactly as before.

## Rule claim recognition

No event is refused because of a `rule` key. Decoding of every event kind is unchanged: `verifyEvent` runs the existing `json.Unmarshal`, the strict resource decoder for resource kinds, and every existing check. Then, only for `proposal.merged`, `parseMergeRuleClaim(payload)` walks the top-level keys once and sets `Event.Rule`:

| Payload shape on `proposal.merged` | `Event.Rule` | Consequence |
|----|----|----|
| Exactly one top-level key equal to `rule` under Unicode case folding, spelled exactly `rule`, whose value is a JSON string accepted by `validPipelineName` (1 to 64 of `A-Z a-z 0-9 - _ .`, excluding `.` and `..`) | the name | canonical rule claim; path (b) is available |
| No such key | empty | path (a) only, as today |
| `"rule": null`, `""`, a number, a boolean, an array or an object | empty | path (a) only |
| A case variant such as `"RULE"` or `"Rule"`, alone or next to `rule` | empty | path (a) only |
| Two or more keys equal to `rule` under case folding | empty | path (a) only |
| A string that fails the rule-name grammar | empty | path (a) only |

Keys are compared after JSON unescaping. On every other kind a `rule` key is ignored exactly as today, and resource kinds keep their strict decoder, which refuses an unknown `rule` key as it does today. A decoder error inside the walk, which cannot occur after `json.Unmarshal` succeeded, also yields no claim.

Signing side: `encodeAndSign` refuses `Event.Rule` set on any kind other than `proposal.merged` (`rule is only valid on proposal.merged`) and an invalid name on `proposal.merged` (`proposal merge rule must be a valid rule name`). Hubnot therefore only writes canonical claims; this restricts what Hubnot signs and never what verifiers accept.

## Union admission

Every fact-admission surface calls one combinator, `admitMergeFact(merge, maintainerPath, rulePath)` in `rule_acceptance.go`, after its existing subject, evidence-presence and `validateProposalMergeBinding` checks:

1. Run `maintainerPath`, the surface's existing acceptance-fact check, unchanged (local: today's body of `validateMergeEvent`; import and `hn serve`: today's `validateReplicationMergeAt`). Success means the fact is a maintainer merge, whatever its `rule` field says.
2. Otherwise, when `merge.Event.Rule` is empty, return the maintainer-path error unchanged.
3. Otherwise run `rulePath`, which reports missing path (b) inputs (commits, policy blob, pipeline definitions) as the surface's dependency or shallow gap and then calls `validateRuleMergeFact`. Success means the fact is a rule merge; failure returns that error.

`rulePath` never runs for a fact that passes path (a), so a maintainer merge never waits for commits it did not need before the mission. No surface implements its own variant of this order (FR-024).

## Derivation (path b)

`validateRuleMergeFact(inputs, merge)` runs these checks in order and returns the first failure as `ruleFactError{Check, EventID, Detail}`. It runs only inside `rulePath`.

### Check names

| Order | Check | Passes when |
|----|----|----|
| 1 | `policy-digest` | `merge.policy` equals the digest of `.hn/policy.json` at the candidate base |
| 2 | `rule-exists` | The base policy has `automerge` and a rule named `merge.rule` |
| 3 | `rule-acceptance` | That rule's acceptance mode is `rule` |
| 4 | `rule-signer` | `merge.actor` is listed in the rule's `actors` |
| 5 | `rule-author` | The candidate's actor is listed in the rule's `authors` |
| 6 | `manual-path` | No changed path matches `.hn/policy.json` (`protectedPathMatches`) and `changedProtectedPaths(base policy, changed)` is empty |
| 7 | `changed-paths` | The change list is non-empty and every path is allowed by the rule (`automergePathAllowed`) |
| 8 | `evidence-item` | Every evidence ID is present and qualifies as a counting approval or a passing run result (see [data-model.md](../data-model.md#qualifying-evidence)); a `proposal.decision` fails this check, because path (b) is reached only after path (a) failed |
| 9 | `evidence-duplicate` | At most one approval per reviewer and one result per runner per pipeline |
| 10 | `approval-threshold` | Distinct counting reviewers number at least `proposals.requiredApprovals` |
| 11 | `pipeline-threshold` | For each base-policy pipeline, distinct passing runners number at least `requiredResults` |

`Detail` names the specific item: the rule name for checks 2 to 4, the author for 5, the path for 6 and 7, the evidence ID and its reason for 8 and 9, counts for 10, and the pipeline name and counts for 11. The `commit` field is not related to the head beyond its object ID shape, which matches acceptance merge facts today.

Rendered message on every surface:

```text
rule merge <full fact ID> fails <check>: <detail>
```

### Inputs and object access per surface

These inputs are read only inside `rulePath`, after path (a) failed.

| Surface | Caller | Base policy | Changed paths | Pipeline digests at head | Missing base or head |
|----|----|----|----|----|----|
| Local projection | `validateMergeEventWith` (`policy.go`) | `loadPolicy(base)` | `changedPathsAt("", base, head)` | `loadPipeline(head, name)` | Projection error; full clones always hold both after import |
| Replication import | `proposal.merged` arm of `replicationEventDependencyWithOptions` (`quarantine.go`) | `loadReplicationPolicyAt` | `changedPathsInOneStore(quarantine, accepted, base, head)` | `replicationPipelineDigestAt` | `replicationDependency` naming the commit; recovery `add exact proposal selection --proposal <candidate>` |
| hn serve actor chain | `checkServeActorDependencies` through `replicationEventDependencyWithOptions` | Same as replication, hosted repository for both stores | Same | Same | `dependency-missing` naming the commit |
| Shallow clone | `guardShallowEventClosure`, then local projection with shallow rule-path guards (WP09) | Shallow gap `policy blob` when absent | Gap `base commit` or `proposal code ref` when absent | Gap `pipeline definition` when absent | `ShallowDependencyGap` with recovery guidance; never evaluated |

Until WP09 lands, local projection in a shallow repository refuses every fact that needs path (b) with `rule merge <full fact ID> cannot be verified in a shallow clone by this build; verify it in a full clone`, and accepts maintainer merges exactly as today (activation boundaries below).

An absent evidence ID is reported before either path runs, as it is for decisions today. Replication reports `missing dependency <id>: evidence is absent; recovery: select the full actor history supplying <id>`. The shallow closure reports a gap of kind `selected event fact` for that ID. For a present `run.result` item, the existing `run.result` relationship rule requires its request.

### Memoization

Within one validation pass, changed paths are computed once per `(base, head)` and pipeline digests once per `(head, pipeline)`, so validating a fact performs at most one changed-path computation (NFR-005). A change list over 8 MiB is refused, never truncated.

## Recording a rule merge fact

`appendRuleMergeEvent(identity, proposal, evaluation, ruleAcceptance, resultCommit)`:

1. Build `proposal.merged` with `subject`, `policy` = base digest, `head`, `commit` = result commit, `evidence` = `ruleAcceptance.Evidence` and `rule` = rule name.
2. Encode and sign it in memory, compute its ID, and run `validateProposalMergeBinding` and `admitMergeFact` with the local maintainer path and rule path against the current event index plus the new event. Unless the result is a rule merge, return `refusing to sign a rule merge fact that verifiers would reject: <message>` and write nothing.
3. Append it with the existing `appendEvent`.

Output after a local merge:

```text
Merged proposal 1c2d3e4f5a6b into main at 3f4e5d6c7b8a
Recorded rule merge event 7d8e9f0a1b2c under rule ordinary with 2 evidence item(s)
```

## hn proposal status

For a candidate whose merge facts include a rule merge fact, one line follows `Status:   merged`:

```text
Merged by rule ordinary, signed by <executor fingerprint> in fact sha256:<fact ID>
```

When several merge facts exist, one such line prints per rule merge fact, sorted by fact ID. Acceptance merge facts without a rule claim print no new line. An acceptance merge fact that carries a rule claim is a maintainer merge and prints:

```text
Merged by maintainers in fact sha256:<fact ID>; its rule annotation "<name>" is not verified
```

## Fixture matrix (NFR-001)

`standing_fact_test.go` builds each defect fixture as a signed `proposal.merged` with exactly one defect from the valid User Story 1 fixture. Every defect fixture is signed by an actor who is not a maintainer (the executing actor E, or X for F03) and cites approvals and run results, so path (a) fails for all of them and the row tests path (b) or the absence of a claim. Each fixture is published through a real temporary remote, pushed to an `hn serve` hosted repository, and placed directly in a local store. A shallow clone selects each one with the complete closure of decision `01M5VSEHFV2K03JJY0NN6DFNAT`. Every surface must refuse, promote nothing and change no hosted ref.

| ID | Defect | Expected refusal |
|----|----|----|
| F01 | `rule` names a rule absent from the base policy | `rule-exists` |
| F02 | Named rule has acceptance mode `maintainers` | `rule-acceptance` |
| F03 | Signer is not an executing actor of the named rule | `rule-signer` |
| F04 | `policy` is not the base policy digest | `policy-digest` |
| F05 | An evidence approval reviews another proposal | `evidence-item` |
| F06 | Evidence includes a `proposal.decision` | `evidence-item` |
| F07 | Evidence includes a `request-changes` review | `evidence-item` |
| F08 | Evidence includes a failed run result | `evidence-item` |
| F09 | Evidence includes an approval from a trusted reviewer outside the rule's `reviewers` | `evidence-item` |
| F10 | Evidence includes the author's own approval with `allowAuthorApproval` true | `evidence-item` |
| F11 | Evidence includes a result whose request was signed by a non-maintainer, non-requester | `evidence-item` |
| F12 | Two approvals by the same reviewer | `evidence-duplicate` |
| F13 | Approvals below `proposals.requiredApprovals` | `approval-threshold` |
| F14 | A required pipeline lacks enough passing trusted results | `pipeline-threshold` |
| F15 | Candidate author not allowed by the rule | `rule-author` |
| F16 | A changed path outside the rule allowlist | `changed-paths` |
| F17 | Candidate changes `.hn/policy.json` | `manual-path` |
| F18 | Candidate changes a protected-check path | `manual-path` |
| F19 | The rule key is a case variant (`"RULE": "ordinary"`, `"Rule": "ordinary"`) (raw signed payloads) | no claim; maintainer-path refusal `merge <id> is not authorized by ...` |
| F20 | `"rule": ""`, `"rule": null`, `"rule": 1`, two `rule` keys, `"rule": "bad name"` (raw signed payloads) | no claim; maintainer-path refusal `merge <id> is not authorized by ...` |
| F21 | An evidence ID absent from the import | missing dependency |
| F22 | Base or head commit absent from every store | missing dependency |

In F17 and F18 the candidate changes the manual-path file together with a path under `src/`, which the rule allows, so check 6 reports before check 7. F01 to F20 are the semantic rows; F21 and F22, and the shallow gap cases of WP09, are a separate missing-dependency table.

## Legacy history fixtures (C-004, FR-022)

Each is a raw signed payload that the older-client reference build of `7a47e69` accepts. Every admission surface of the new client must accept it exactly as before, and the older client must accept it too (WP17). The policy is one the older client can read: maintainers-mode rules only.

| ID | Legacy event | Expected verdict on every surface and client |
|----|----|----|
| L01 | `review.submit` approve with an extra top-level `"rule": "annotation"` | valid review; counts as an approval as before |
| L02 | `run.request` with `"RULE": "x"`, and `issue.comment` with `"rule": 1` | valid as before |
| L03 | `proposal.merged` signed by the maintainer M citing enough M accept decisions, with `"rule": "nosuch"` (canonical claim, rule absent) | maintainer merge; status reports the annotation as not verified |
| L04 | As L03, with `rule` naming a rule that exists in the base policy, and evidence holding the decisions plus the approval | maintainer merge |
| L05 | As L03, with `"rule": null`, `"rule": ""`, `"rule": 1`, `"Rule": "ordinary"` or two `rule` keys | maintainer merge; no claim |
| L06 | A resource event with an added `rule` key (control) | refused by the strict resource decoder, as before |

## Activation boundary fixtures (FR-025)

`TestStandingFactBoundary` holds one table naming which surfaces implement path (b). WP07 creates it with local projection in full clones; WP08 adds replication import and the `hn serve` actor-chain check; WP09 adds shallow verification; WP16 asserts all four.

| ID | Fixture | Expected at every boundary |
|----|----|----|
| B1 | The valid rule-only fact of User Story 1 | accepted by the surfaces in the table; refused by every other surface |
| B2 | Downgrade shape: M signs a fact with `rule` naming an absent rule and enough M accept decisions | accepted by every surface as a maintainer merge |
| B3 | M signs a fact with `rule` naming the standing rule `ordinary` and enough M accept decisions | accepted by every surface as a maintainer merge |
| B4 | E signs F01 | refused by every surface |
| B5 | On `hn serve`, M requests a maintainer advancement of an accepted candidate with `hn-rule=nosuch` added | refused with every hosted ref unchanged, before and after WP14 (the same push without `hn-rule` succeeds) |

## Agreement (NFR-002)

Fact agreement: for every fixture of the defect matrix, the valid fixture set, the legacy history set and the boundary set B1 to B4 (once all four surfaces implement path (b)), the test records each surface's verdict and requires all four to agree (WP16). Rule-satisfaction agreement between the keyless preview, local execution and the `hn serve` verdict is defined, with its matched inputs and the checks it excludes, in [serve-rule-advancement.md](serve-rule-advancement.md#agreement-nfr-002).

## Tested example

`docs/protocol-v0.md` carries `<!-- example: standing-rule-merge-fact -->`, a rule merge fact JSON produced by the User Story 1 flow. `standing_quality_test.go` reads a freshly recorded fact from the executing actor's chain with `git show <actor ref>:event.json` and checks that its key set and `rule`, `policy` and `evidence` values have the documented shape.
