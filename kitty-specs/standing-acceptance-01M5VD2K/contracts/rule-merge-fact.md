# Contract: rule merge fact

Covers FR-010 to FR-015, FR-022 (event part), NFR-001, NFR-002 and NFR-005. A rule merge fact is a `proposal.merged` event (protocol `hn/0`) whose `rule` field names an automerge rule with acceptance mode `rule` in the candidate's base policy.

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

Field order follows the Go struct order, with `rule` last among the fields used here. `rule` is encoded with `omitempty`, so acceptance merge facts and every other event encode exactly as before.

## Parse-time rules

These run in `validateEventContent` and `verifyEvent` for every event, before any relationship check.

| Rule | Refusal |
|----|----|
| `rule` non-empty on any kind other than `proposal.merged` | `rule is only valid on proposal.merged` |
| `rule` on `proposal.merged` fails the rule-name grammar (1 to 64 of `A-Z a-z 0-9 - _ .`, excluding `.` and `..`) | `proposal merge rule must be a valid rule name` |
| A top-level key equal to `rule` under case folding that is not exactly `rule` | `event has a non-canonical rule key` |
| The key `rule` appears more than once | `event has a duplicate rule key` |
| The key `rule` holds `null`, `""` or a non-string | `proposal merge rule must be a valid rule name` |

Resource events keep their existing strict decoder and never carry `rule`.

## Derivation

`validateRuleMergeFact(inputs, merge)` runs these checks in order and returns the first failure as `ruleFactError{Check, EventID, Detail}`. Before calling it, every surface runs `validateProposalMergeBinding`, which requires `merge.head` to equal the candidate head.

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
| 8 | `evidence-item` | Every evidence ID is present and qualifies as a counting approval or a passing run result (see [data-model.md](../data-model.md#qualifying-evidence)) |
| 9 | `evidence-duplicate` | At most one approval per reviewer and one result per runner per pipeline |
| 10 | `approval-threshold` | Distinct counting reviewers number at least `proposals.requiredApprovals` |
| 11 | `pipeline-threshold` | For each base-policy pipeline, distinct passing runners number at least `requiredResults` |

`Detail` names the specific item: the rule name for checks 2 to 4, the author for 5, the path for 6 and 7, the evidence ID and its reason for 8 and 9, counts for 10, and the pipeline name and counts for 11. The `commit` field is not related to the head beyond its object ID shape, which matches acceptance merge facts today.

Rendered message on every surface:

```text
rule merge <full fact ID> fails <check>: <detail>
```

### Inputs and object access per surface

| Surface | Caller | Base policy | Changed paths | Pipeline digests at head | Missing base or head |
|----|----|----|----|----|----|
| Local projection | `validateMergeEvent` (`policy.go`) | `loadPolicy(base)` | `changedPathsAt("", base, head)` | `loadPipeline(head, name)` | Projection error; full clones always hold both after import |
| Replication import | `validateReplicationMergeAt` (`quarantine.go`) | `loadReplicationPolicyAt` | `changedPathsInOneStore(quarantine, accepted, base, head)` | `replicationPipelineDigestAt` | `replicationDependency` naming the commit; recovery `add exact proposal selection --proposal <candidate>` |
| hn serve actor chain | `checkServeActorDependencies` through `replicationEventDependencyWithOptions` | Same as replication, hosted repository for both stores | Same | Same | `dependency-missing` naming the commit |
| Shallow clone | `guardShallowEventClosure`, then local projection | Shallow gap `policy blob` when absent | Gap `base commit` or `proposal code ref` when absent | Gap `pipeline definition` when absent | `ShallowDependencyGap` with recovery guidance; never evaluated |

An absent evidence ID is reported before validation as it is for decisions today. Replication reports `missing dependency <id>: evidence is absent; recovery: select the full actor history supplying <id>`. The shallow closure reports a gap of kind `selected event fact` for that ID. For a present `run.result` item, the existing `run.result` relationship rule requires its request.

### Memoization

Within one validation pass, changed paths are computed once per `(base, head)` and pipeline digests once per `(head, pipeline)`, so validating a fact performs at most one changed-path computation (NFR-005). A change list over 8 MiB is refused, never truncated.

## Recording a rule merge fact

`appendRuleMergeEvent(identity, proposal, evaluation, ruleAcceptance, resultCommit)`:

1. Build `proposal.merged` with `subject`, `policy` = base digest, `head`, `commit` = result commit, `evidence` = `ruleAcceptance.Evidence` and `rule` = rule name.
2. Encode and sign it in memory, compute its ID, and run `validateProposalMergeBinding` and `validateRuleMergeFact` against the current event index plus the new event. If either fails, return `refusing to sign a rule merge fact that verifiers would reject: <message>` and write nothing.
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

When several merge facts exist, one such line prints per rule merge fact, sorted by fact ID. Acceptance merge facts print no new line.

## Fixture matrix (NFR-001)

`standing_fact_test.go` builds each fixture as a signed fact with exactly one defect from the valid User Story 1 fixture. Each fixture is published through a real temporary remote, pushed to an `hn serve` hosted repository, and placed directly in a local store. A shallow clone selects each one. Every surface must refuse, promote nothing and change no hosted ref. Rows marked parse are refused before relationship validation.

| ID | Defect | Expected check |
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
| F19 | `rule` on a `review.submit` event (raw signed payload) | parse |
| F20 | `"rule": ""`, `"rule": null` and `"RULE": "ordinary"` on `proposal.merged` (raw signed payloads) | parse |
| F21 | An evidence ID absent from the import | missing dependency |
| F22 | Base or head commit absent from every store | missing dependency |

In F17 and F18 the candidate changes the manual-path file together with a path under `src/`, which the rule allows, so check 6 reports before check 7.

## Agreement (NFR-002)

For every fixture above and for the valid fixture set, the test records each surface's verdict and requires all four to agree. For every rule advancement fixture in [serve-rule-advancement.md](serve-rule-advancement.md), the `hn serve` verdict must agree with the client preview evaluated on the same facts.

## Tested example

`docs/protocol-v0.md` carries `<!-- example: standing-rule-merge-fact -->`, a rule merge fact JSON produced by the User Story 1 flow. `standing_quality_test.go` reads a freshly recorded fact from the executing actor's chain with `git show <actor ref>:event.json` and checks that its key set and `rule`, `policy` and `evidence` values have the documented shape.
