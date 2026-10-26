# Contract: hn serve rule advancement

Covers FR-016, FR-017 (server), FR-018, FR-020 (remote), FR-025 (server part) and the rule-satisfaction part of NFR-002, and supersedes the sentence in `docs/serve-v0.md` that says the server does not evaluate automerge rules. The server still signs nothing (C-008) and still never creates a commit.

## Push options

| Option | Value | Rules |
|----|----|----|
| `hn-advance` | Full candidate event ID | Existing |
| `hn-expected-tip` | Full commit ID | Existing |
| `hn-rule` | Rule name (1 to 64 of `A-Z a-z 0-9 - _ .`, excluding `.` and `..`) | New. At most once. Valid only together with `hn-advance`. |

At most three options are accepted (`servePushOptionLimit = 3`). Every parse failure rejects every ref update with `unsupported-command`, as today. The sentence constant becomes:

```text
push options other than hn-advance, hn-expected-tip and hn-rule are not supported.
```

New refusals in `parseServePushOptions`:

| Input | Error |
|----|----|
| `hn-rule` twice | `hn-rule appears more than once` |
| `hn-rule` value fails the grammar | `hn-rule value "X" is not a valid rule name` |
| `hn-rule` without `hn-advance` | `hn-rule requires hn-advance` |

The existing shape rule still applies: an `hn-advance` push must have exactly one ref update, and it must be to a protected branch.

### Interim refusal until the verdict lands (FR-025)

WP13 lands the parser before WP14 lands the rule steps. In that interval `evaluateServePush` refuses every update of a push whose options carry `hn-rule` with `unsupported-command` and the sentence `this server does not evaluate hn-rule advancement yet.`, before any protected check runs, so a build in that state behaves like a server built before this mission and never applies the maintainer path to a request that names a rule. WP14 removes the interim refusal in the same change that adds the rule steps. Boundary fixture B5 of [rule-merge-fact.md](rule-merge-fact.md#activation-boundary-fixtures-fr-025) holds before and after WP14. A client sending `hn-rule` to a server built before this mission receives `unsupported-command`. The client then reports `REMOTE does not support rule advancement (hn-rule); upgrade hn on the server` and exits 1 with the branch unchanged.

## Protected-branch steps with hn-rule

The steps of `evaluateServeProtectedUpdate` run in this order. Rows marked "unchanged" behave exactly as without `hn-rule`. The first failing step is reported, and the existing multi-candidate rule (latest failing step, ties to the lowest event ID) still applies. With `hn-advance` the candidate set is the single named candidate.

| Step | Check | Reason on failure |
|----|----|----|
| 1 | Not a deletion | `protected-deletion` (unchanged) |
| 2 | Not a creation | `protected-policy-invalid` (unchanged) |
| 3 | `hn-expected-tip` equals the tip | `protected-stale-tip` (unchanged) |
| 4 | Destination policy at the tip is valid | `protected-policy-invalid` (unchanged) |
| 5 | Head descends from the tip | `protected-not-fast-forward` (unchanged) |
| 6 | Candidate with this head exists | `protected-no-proposal` (unchanged) |
| 7 | Base policy is valid | `protected-policy-invalid` (unchanged) |
| 7a | Destination policy digest equals base policy digest | `protected-destination-policy`, next `revise <candidate> against <tip>` |
| 8 | Hosted or pushed code ref equals the head | `proposal-ref-mismatch` (unchanged) |
| 9 | Lineage, merged state, maintainer rejection | `protected-blocked` (unchanged) |
| 10 | `evaluateRuleAcceptance` passes; see the next table | `protected-rule-veto` or `protected-rule-not-satisfied` |
| 11 | Destination readiness | skipped; step 7a makes it identical to step 10 |
| 12 | Protected baseline at the destination (`guardTargetProtectedPolicy`) | `protected-destination-policy` (unchanged) |
| 13 | Requester is listed in the named rule's `actors` in the base policy | `advance-not-rule-executor` |

Without `hn-rule`, steps 7a and the rule versions of 10 and 13 do not run. A direct push, or an `hn-advance` push without `hn-rule`, needs maintainer acceptance under both policies as today (FR-018).

### Step 10 under a rule

`evaluateRuleAcceptance` runs on the hosted facts plus the facts accepted earlier in the same push, with the hosted repository for objects. Changed paths come from `changedPathsAt(gitDir, base, head)`.

| Order | Condition | Reason | IDs on the line |
|----|----|----|----|
| a | The base policy has no rule with that name | `protected-rule-not-satisfied` | candidate |
| b | The rule's acceptance mode is `maintainers` | `protected-rule-not-satisfied` | candidate |
| c | The rule's `targetBranch` differs from the pushed branch | `protected-rule-not-satisfied` | candidate |
| d | A current trusted change request or a current trusted failure exists | `protected-rule-veto` | candidate, then each blocking event ID, sorted |
| e | Author not allowed, a changed path outside the rule, or a policy or protected-check change | `protected-rule-not-satisfied` | candidate |
| f | Counted approvals or passing results below threshold | `protected-rule-not-satisfied` with `missing=review` and/or `missing=ci` | candidate |

## Reason codes

The three codes live in `serveRuleReasonCatalogue` and in a second documented table in `docs/protocol-v0.md`, under the heading "Reason codes for rule advancement".

| Code | Meaning | Typical next action |
|----|----|----|
| `advance-not-rule-executor` | The requester is not an executing actor of the named rule in the base policy | ask an executing actor of the rule to run `hn automerge --remote` |
| `protected-rule-not-satisfied` | The named rule does not exist, is not a standing rule, or the candidate does not satisfy it | run `hn automerge <candidate> --rule <rule>` |
| `protected-rule-veto` | A current trusted change request or trusted failure blocks the rule | resolve the named events, then retry |

`serveReasonLookup` searches `serveReasonCatalogue` first and then `serveRuleReasonCatalogue`, so the line parser, the renderer, the push log and the client all accept the new codes. A new test requires the two catalogues to share no code and each documented table to equal its catalogue.

## Rejection examples

The candidate, actor and commit IDs below are placeholders. Each line is followed by the `hn-serve:` sentence line, as today.

```text
hn-serve/1 reject ref=refs/heads/main actor=<executor> code=protected-rule-veto ids=sha256:<candidate>,sha256:<change request> next="resolve the named events, then retry"
hn-serve: candidate sha256:<candidate> is blocked by rule ordinary: trusted change request sha256:<change request>.
```

```text
hn-serve/1 reject ref=refs/heads/main actor=<other> code=advance-not-rule-executor ids=sha256:<candidate> next="ask an executing actor of the rule to run hn automerge --remote"
hn-serve: actor <other> requested advancement of refs/heads/main under rule ordinary and is not one of its executing actors in the base policy of sha256:<candidate>.
```

```text
hn-serve/1 reject ref=refs/heads/main actor=<executor> code=protected-destination-policy ids=sha256:<candidate> next="revise sha256:<candidate> against <tip>"
hn-serve: refs/heads/main at <tip8> has policy sha256:<dest> and candidate sha256:<candidate> was based on policy sha256:<base>; rule ordinary applies only under its own policy.
```

## Atomicity and concurrency

Rule advancement uses the existing repository lock, held from the advertisement until `receive-pack` exits. The race between two executing actors is decided at step 3: the loser receives `protected-stale-tip`, as two maintainers do today. Facts that reach the server after the client's import and before the advancement are visible at step 10, which is how US4-3 is met.

## Agreement (NFR-002)

Decision `01M5VSEHFH0B1BSJ1NA923FKAA`. Agreement compares rule satisfaction, the outcome of the derivation every surface shares, under identical inputs. It does not compare requester authorization or transport.

### Matched inputs

Every comparison fixture declares its target tip and holds these inputs equal on the compared surfaces. The preview and local execution run in the same clone, so they share every input. Agreement requires equality across the compared surfaces; the declared tip may differ from the signed base where the fixture says so.

| Input | How the fixture holds it equal |
|----|----|
| Target branch and tip | E's checked-out `main` equals the hosted `main`, and both equal the fixture's declared tip. The declared tip is the candidate's signed base, except in the FR-017 fixture |
| Base and target policy | the same commits on both surfaces, so the same digests. The FR-017 fixture puts both targets at the same amendment commit H1 and builds the candidate's head on top of H1 with its signed base at the older commit, so the target differs from the signed base, the server reaches step 7a, and both surfaces see a target policy that differs from the base policy |
| Candidate code | the head commit and the proposal code ref are present locally and on the server |
| Fact set | E runs `hn sync origin` with the all-trusted selection immediately before the comparison, and no fact is published between E's preview and E's push |

### Rule-satisfaction outcome

Satisfied means, for the keyless preview, no blocker; for local execution by an authorized executing actor, the merge happens; for `hn serve`, no rejection from steps 7a, 8, 9 and 10. The fixtures cover a satisfied candidate; a trusted change request; a trusted failure; a maintainer rejection; a destination policy that differs from the base policy; a rule that does not exist; a rule with acceptance mode `maintainers`; a rule for another target branch; an author outside the rule; a path outside the rule; a policy or protected-check change; too few counted approvals; too few passing results.

### Checks excluded from agreement and tested on their own

| Check | Where it runs | Test |
|----|----|----|
| Executor authorization: identity listed in the rule's `actors` | `cmdAutomerge`, `proposalMergeGuard`, `serveAdvanceValidate` with a rule binding | WP12 T057 (local), WP15 T095 (remote client) |
| Executor authorization: step 13 `advance-not-rule-executor` | `hn serve` | WP14 T068 step 5, WP15 T095 |
| Remote identification and the `hn serve` upstream refusal | client | existing serve tests, WP15 T095 |
| Selection covers every trusted actor | `automergeRemote` | existing tests, WP15 T095 |
| Local target equals hosted tip; `hn-expected-tip` and stale tip (step 3) | client and `hn serve` | WP14 T068 step 9, WP15 T095 |
| Deletion, creation, destination policy validity, fast-forward (steps 1, 2, 4, 5); candidate exists (step 6) | `hn serve` | existing serve tests, unchanged |
| Protected baseline at the destination (step 12) | `hn serve` | existing serve tests, unchanged |
| `hn-rule` support on the server | client mapping of `unsupported-command` | WP15 T071 unit test |
| Clean worktree, unfinished operation, shallow or partial clone, expected-old ref lease | local execution | existing automerge tests, WP12 T057 |

### Executable comparisons

| Comparison | Test | WP |
|----|----|----|
| Four fact-admission surfaces on every fact fixture | `TestStandingFactMatrixAgreement` | WP16 T078 |
| Keyless preview and local execution | `TestStandingAutomergeAgreementPreviewExecute` | WP11 T094 |
| Keyless preview and `hn serve` verdict | `TestStandingServeAdvanceAgreementPreviewServer` | WP15 T095 |
| Live evaluation and fact validation on generated worlds | `TestRuleAcceptanceFactProperty` | WP05 T024 |
| Recorded facts and the four surfaces | `TestStandingFactMatrixExecutionAdmitted` | WP16 T080 |

Local execution and `hn serve` are each compared with the same preview under the same inputs, so they agree with each other on rule satisfaction.

## Tested examples

`docs/serve-v0.md` carries `<!-- example: standing-serve-advance -->` (US4-1 flow) and `<!-- example: standing-serve-veto -->` (US4-3 rejection). `standing_serve_test.go` runs both through the forced-command harness.
