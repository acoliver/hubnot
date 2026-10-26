# Contract: hn automerge under a standing rule

Covers FR-005 (thresholds), FR-006, FR-008, FR-009, FR-011, FR-017 (local), FR-019, FR-020 and FR-021, and the preview-to-execution part of NFR-002. The command grammar is unchanged:

```text
hn automerge PROPOSAL --rule NAME [--execute [--remote REMOTE]]
```

Behavior for rules with acceptance mode `maintainers` is unchanged, except that a rule with `reviewers` also checks the rule approval threshold (US5-4), and the preview prints the additive lines below.

## Preview

The keyless preview writes no event and no ref. After the existing `Rule:` and `Permitted executing actors:` lines it prints:

```text
Acceptance: rule (standing acceptance; no maintainer acceptance required)
Counted reviewers:
  <gpt-reviewer fingerprint>
Counted approvals: 1/1 (author approvals do not count)
```

For acceptance mode `maintainers`:

```text
Acceptance: maintainers (maintainer acceptance required)
Counted reviewers: all trusted reviewers
Counted approvals: 1/1
```

When `reviewers` is set, the reviewer list replaces `all trusted reviewers`. The parenthetical `(author approvals do not count)` appears only under acceptance mode `rule`. The existing `Changed paths:`, `Eligible:` and `Blocked:` blocks follow unchanged.

## Blockers by acceptance mode

Blockers are listed in the order `inspectAutomerge` produces them. Under acceptance mode `rule`, two existing blockers are replaced and the rest stay.

| Blocker | `maintainers` | `rule` |
|----|----|----|
| `current approval, protected check or CI evidence is not ready` | yes | replaced by the two threshold blockers below |
| `current required maintainer acceptances are missing` | yes | no |
| `rule approval threshold is unmet: N of M counted approvals` | only when `reviewers` is set | yes |
| `pipeline "P" has N of M passing trusted results` | covered by readiness | yes |
| `proposal has a current maintainer rejection` | yes | yes |
| `proposal has a current trusted change request` | yes | yes (any trusted reviewer, counted or not) |
| `pipeline "P" has a current trusted failure` | yes | yes |
| Policy amendment, protected change, author, path, branch, target, lineage, code, merged, clean tree, policy digest, shallow, partial and unfinished-operation blockers | yes | yes, unchanged |

Under acceptance mode `rule`, an approval counts only from a current `approve` review by a trusted reviewer, by a rule reviewer when `reviewers` is set, and never by the candidate author. A run result counts under the same rules as in [data-model.md](../data-model.md#qualifying-evidence).

## Local execution

`hn automerge PROPOSAL --rule NAME --execute` under acceptance mode `rule`:

1. Runs the preview checks; any blocker refuses with `automerge blocked: ...` (existing text).
2. Loads the identity and requires it to be listed in the rule's `actors`. Otherwise it refuses with `identity <actor> is not an executing actor for automerge rule "NAME"` (existing text).
3. Applies the existing upstream check for `hn serve` remotes.
4. Calls `mergeProposal` with a guard that carries the rule authority. The guard replaces the accepted and maintainer checks at `governance.go` lines 172, 185, 241, 247, 285 and 291 with a fresh `evaluateRuleAcceptance` and executing-actor membership. Every other check, the expected-old lease and the checkout behavior stay unchanged.
5. Records the fact with `appendRuleMergeEvent`, which self-checks before signing (see [rule-merge-fact.md](rule-merge-fact.md#recording-a-rule-merge-fact)).

A maintainer who is an executing actor of a rule with acceptance mode `rule` follows the same steps and records a rule merge fact (FR-011).

## Local repair (FR-020)

Under acceptance mode `rule` only, the preview reports a repair state instead of the blocker `target no longer equals the signed base` when all of these hold:

- the candidate has no merge fact under its base policy;
- the checked-out target contains the head through exactly one first-parent merge commit that names the head as a parent (`findProposalMergeCommit`);
- the target's `.hn/policy.json` digest equals the base policy digest;
- every other rule check passes on current evidence.

Preview output in the repair state:

```text
Repair: code merged at 3f4e5d6c7b8a; --execute records the missing rule merge fact
```

`--execute` then records the rule merge fact with `commit` set to that merge commit. It creates no commit and moves no ref:

```text
Recorded missing rule merge event 7d8e9f0a1b2c under rule ordinary with 2 evidence item(s)
```

A second rerun reports `proposal is already recorded as merged` (existing blocker) and changes nothing. For acceptance mode `maintainers` the existing blocker text, which directs to `hn merge`, is unchanged.

## Remote execution

`hn automerge PROPOSAL --rule NAME --execute --remote REMOTE` under acceptance mode `rule` keeps the existing steps of `automergeRemote`: remote identification, the selection must import every trusted actor, `hn sync`, a second inspection, and the local target must equal the hosted tip. It then calls `mergeRemoteBound` with `serveAdvanceBinding{ExpectedActor, ExpectedTip, Rule: NAME}`. With a rule binding:

| `mergeRemoteBound` step | Change |
|----|----|
| 1 Validate locally | Rule authority replaces the accepted and maintainer checks of `serveAdvanceValidate` |
| 6 Request advancement | Adds `--push-option=hn-rule=NAME` |
| 7 Confirm, rerun message | Names `hn automerge PROPOSAL --rule NAME --execute --remote REMOTE` |
| 8 Record | `serveAdvanceRecord` uses the rule authority and records a rule merge fact with `commit` = head |

Output:

```text
Advanced origin refs/heads/main to 9b2e4c6a8d0f for proposal 1c2d3e4f5a6b
Recorded rule merge event 7d8e9f0a1b2c under rule ordinary with 2 evidence item(s)
```

On refusal the client prints each `hn-serve/1` line, as `hn merge --remote` does, and exits 1 with the branch unchanged.

### Remote repair

When the hosted tip equals or descends from the head, the candidate has no merge fact, and the local target still equals the signed base, `automergeRemote` skips the local-equals-hosted check, inspects the rule again against the imported facts, and calls `mergeRemoteBound`. Step 5 then records the missing rule merge fact once:

```text
origin refs/heads/main already contains 9b2e4c6a8d0f for proposal 1c2d3e4f5a6b
Recorded missing rule merge event 7d8e9f0a1b2c under rule ordinary with 2 evidence item(s)
```

### Publication-only rerun

Decision `01M5VSEHFCZGQHQ14CKJ3MAMP7`. Today both inspections stop a rerun with `proposal is already recorded as merged` before any publication step, so a rerun after recording needs its own path. `cmdAutomerge` takes it, before `inspection.eligible()`, when every one of these holds:

1. the command is `hn automerge PROPOSAL --rule NAME --execute --remote REMOTE`;
2. the identity loads and is listed in the `actors` of rule NAME in the candidate's base policy (authorization);
3. the local store holds a `proposal.merged` for PROPOSAL whose `rule` is NAME, whose signer is that identity, whose `policy` is the base policy digest, and which projection admitted as a rule merge (path (b));
4. REMOTE is identified as an `hn serve` remote.

The path then:

1. prints the preview as usual, followed by `Publication-only rerun: rule merge event <short ID> is already recorded`;
2. skips the `proposal is already recorded as merged` blocker of the first inspection and of the inspection after import, and the local-equals-hosted check; every other refusal of the four conditions above still applies;
3. publishes or republishes the identity's actor chain with the existing `publishLocalFacts(REMOTE)`;
4. reads the hosted tip of the rule's target branch and requires it to equal or descend from the fact's `commit`.

It signs nothing, appends no event and sends no branch update. Output:

```text
origin refs/heads/main contains 9b2e4c6a8d0f for proposal 1c2d3e4f5a6b
Rule merge event 7d8e9f0a1b2c under rule ordinary is published on origin
```

When the hosted branch does not contain the fact's commit, the command exits 1 with `rule merge event <short ID> is recorded but REMOTE refs/heads/BRANCH does not contain <commit>; a publication-only rerun never advances the branch`, after publishing. A rerun without `--remote`, a rerun by any other identity, and a rerun when the only merge facts are acceptance merge facts or another actor's rule merge facts keep the existing `proposal is already recorded as merged` refusal.

### Interruptions and reruns

| Interrupted after | Hosted branch | Local fact | Rerun of the same command |
|----|----|----|----|
| Fact publication before the push | unchanged | absent | Full run |
| Push, before the server applied it | unchanged | absent | Full run |
| Push, after the server applied it | head | absent | Remote repair records the fact once |
| Recording | head | present, unpublished | Publication-only rerun publishes the fact and confirms the branch; no new fact, no branch update |
| Second publication | head | present, published | Publication-only rerun confirms the branch; no new fact, no branch update |
| Completed run | head | present, published | Publication-only rerun, identical output; nothing changes |

## Preview and execution agreement (NFR-002)

The keyless preview and local execution run in the same clone, so they share the branch tip, the base and target policy, the candidate code and the fact set. For every rule advancement fixture, local execution by an authorized executing actor merges exactly when the preview reports no blocker, and when it refuses it names the same blockers. Executor authorization (`identity <actor> is not an executing actor ...`), the `hn serve` upstream refusal and the expected-old ref lease are execution-only checks and are tested on their own (see [serve-rule-advancement.md](serve-rule-advancement.md#agreement-nfr-002)).

## Commands an executing actor cannot use (FR-019)

| Command | Refusal | State change |
|----|----|----|
| `hn decide PROPOSAL --accept` | `identity <actor> is not a maintainer under policy <digest>` (existing) | none |
| `hn merge PROPOSAL` | `identity <actor> is not a maintainer under proposal policy` (existing) | none |
| `hn merge PROPOSAL --remote REMOTE` | step 1 maintainer refusal (existing) | none |
| `hn automerge PROPOSAL --rule OTHER --execute` | `identity <actor> is not an executing actor for automerge rule "OTHER"` (existing) | none |
| `git push REMOTE HEAD:main` to `hn serve` | `protected-not-ready` for missing acceptance (existing) | no hosted ref changes |
| Policy amendment or protected change under any rule | `policy amendments require manual merge` or `protected check changes require manual merge` (existing) | none |

The maintainer refusals for `hn decide` and `hn merge` print shortened actor IDs, as they do today. The existing text is kept because existing tests may read it.

## Tested examples

`docs/governance-v0.md` carries `<!-- example: standing-preview -->` (the rule-mode preview above) and `<!-- example: standing-execute -->` (the local flow of User Story 1). `standing_quality_test.go` runs both.
