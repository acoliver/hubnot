# Contract: push verdict, ref-class rules and rejection lines

This contract defines what `hn serve hook pre-receive` accepts. The hook runs under the repository lock with Git's object quarantine active, so it can read every pushed object while no pushed ref or object is visible to anyone else. It exits 0 only when every ref update in the push passes. Otherwise it prints one rejection line and one sentence per failing ref update and exits 1, and Git rejects every ref and discards the quarantined objects (FR-012).

## Inputs

| Input | Source |
|----|----|
| Ref updates `OLD NEW REF` | `pre-receive` stdin; `OLD` or `NEW` is the all-zero object ID for creation or deletion |
| Mapped actor and key | `HN_SERVE_ACTOR`, `HN_SERVE_KEY` from the wrapper |
| Push options | `GIT_PUSH_OPTION_COUNT`, `GIT_PUSH_OPTION_<n>` |
| Repository configuration | `state/repos/NAME/config.json`, `branches.json` |
| Hosted facts | Every event reachable from hosted `refs/hn/actors/*`, read with `collectIssueEvents` |

Facts in the push are added to the hosted facts for checks within the same push: a proposal ref may name a candidate that arrives in an actor ref of the same push, and a protected update may rely on facts in the same push once those actor updates have themselves passed. Code for a protected update must already be hosted or arrive as a proposal ref in the same push.

## Push options

Only `hn-advance=<full event ID>` and `hn-expected-tip=<full commit ID>` are allowed, each at most once. Any other option, a repeated option or a malformed value rejects every ref update in the push with `unsupported-command`. A push with `hn-advance` must contain exactly one ref update, to a protected branch; otherwise every ref update is rejected with `unsupported-command`.

## Rules by ref class

Classes are defined in [data-model.md](../data-model.md). "Maintainer of the repository" means an actor listed in `maintainers` of the policy at the tip of the default protected branch at verdict time.

### Actor chain `refs/hn/actors/<A>`

Checks run in this order and the first failure is reported.

1. Deletion, or `OLD` is not an ancestor of `NEW`: `actor-chain-rewrite`.
2. `A` differs from the mapped actor: `actor-key-mismatch`.
3. Budgets, measured with `measureQuarantinedSelectionUnderBudgets` against the hosted `OLD` value and the repository's budgets: `over-budget`.
4. Each appended event passes `validateQuarantinedActor` checks (signature, actor, sequence, previous ID, commit parent, tree shape) and the whole chain passes `validateActorChains`: `actor-chain-invalid`, naming the full ID of the first failing event, or its commit when the event cannot be decoded.
5. Each appended event's references resolve against hosted facts plus facts in the push, using `replicationEventDependency`. A missing event or Git object: `dependency-missing`, naming the full missing ID. A wrong relationship: `actor-chain-invalid`.
6. The combined projection passes `validateExactEventReferenceClosure`, `validateIssueRelationships` and `ProjectIdentityContinuity`: `actor-chain-invalid`.

The single server exception: in step 5, a `proposal.open` or `proposal.revise` event whose code ref is neither hosted nor in the push is not a dependency failure. Its base must still exist and the head must descend from the base whenever the head is present.

### Proposal ref `refs/hn/proposals/<id>`

1. Update or deletion of an existing ref: `proposal-ref-immutable`.
2. Candidate `sha256:<id>` is absent from hosted facts and the push: `dependency-missing`.
3. Candidate is not a proposal kind, or `NEW` differs from its signed head: `proposal-ref-mismatch`, naming the candidate and both commits.
4. `NEW` does not descend from the candidate's base: `proposal-ref-mismatch`.
5. Budgets: `over-budget`.

### Memory ref `refs/hn/memory/<A>/<stream>`

1. Deletion or a non-ancestor `OLD`: `memory-invalid`.
2. `A` differs from the mapped actor: `memory-key-mismatch`.
3. Budgets: `over-budget`.
4. `loadMemoryStreamAt` fails for the stream: `memory-invalid`.

### Tag `refs/tags/*`

Creation by any mapped actor is accepted. Update or deletion: `tag-immutable`.

### Ordinary branch `refs/heads/*`, not protected

Creation and fast-forward updates by any mapped actor are accepted. A non-fast-forward update or deletion is accepted only from the recorded owner or a maintainer of the repository; otherwise `branch-not-owner`, naming the owner when one is recorded. A branch with no owner record accepts these only from maintainers. When the default protected branch has no valid policy, no actor counts as a maintainer.

### Reserved

Every other ref, including `refs/hn/serve/v1`: `reserved-ref`.

### Protected branch

Checks run in this order and the first failure is reported. `T` is `OLD`, `H` is `NEW`.

| Step | Condition | Code | `missing` |
|----|----|----|----|
| 1 | `NEW` is zero (deletion) | `protected-deletion` | |
| 2 | `OLD` is zero (creation by push, OQ-11) | `protected-policy-invalid` | |
| 3 | `hn-expected-tip` present and differs from `T` | `protected-stale-tip` | |
| 4 | No valid `.hn/policy.json` at `T` (destination policy) | `protected-policy-invalid` | |
| 5 | `T` is not an ancestor of `H` | `protected-not-fast-forward` | |
| 6 | No candidate in hosted facts plus the push has signed head `H`, or `hn-advance` names a candidate whose head is not `H` | `protected-no-proposal` | `proposal,review,ci,acceptance` |
| 7 | No valid policy at the candidate's base | `protected-policy-invalid` | |
| 8 | Hosted code ref for the candidate is absent or differs from `H` | `proposal-ref-mismatch` | `proposal` |
| 9 | Lineage superseded, closed by a merged member, has competing merges, the candidate is already merged, or a current maintainer rejection exists under base policy | `protected-blocked` | |
| 10 | Not ready or not accepted under base policy | `protected-not-ready` | each of `review`, `ci`, `acceptance` that is unmet |
| 11 | Not ready or not accepted under destination policy | `protected-destination-policy` | each unmet kind |
| 12 | `hn-advance` present and the mapped actor is not a maintainer in both base and destination policy | `advance-not-maintainer` | |

When several candidates share head `H` and no `hn-advance` option names one, the update is accepted if any candidate passes steps 7 to 11. If none passes, the rejection reports the candidate whose first failing step is latest, breaking ties by lowest event ID, and `ids` lists every candidate with head `H`.

Base evaluation uses `evaluateProposal`. Destination evaluation uses `evaluateProposalUnderPolicy` with the destination policy and the base policy digest, so it counts the same signed decisions, reviews and CI results but only from actors and pipelines the destination policy lists, against the destination thresholds. A pipeline required only by the destination policy is loaded at `H`; a missing or invalid definition is a step 11 failure with `missing=ci`.

## Reason-code catalogue

| Code | Meaning | Typical next action |
|----|----|----|
| `unknown-key` | The authenticating key is not in the registry | ask the administrator to run hn serve key add |
| `unhosted-repository` | The requested repository is not hosted or its path is unsafe | check the clone URL |
| `unsupported-command` | The SSH request, push option set or advancement shape is not supported | use git fetch, git push, hn merge --remote or git lfs |
| `repository-busy` | Another push held the repository lock for more than 120 seconds | retry the push |
| `actor-key-mismatch` | The pushed actor chain belongs to another actor | push only refs/hn/actors/<your actor> |
| `actor-chain-invalid` | An event fails signature, sequence, shape or relationship validation | inspect the named event with hn log |
| `actor-chain-rewrite` | The push deletes or rewrites hosted actor history | append new events instead |
| `dependency-missing` | A referenced event or Git object is not hosted and not in the push | publish the named fact or commit first |
| `protected-no-proposal` | No proposal has the pushed commit as its signed head | open a proposal for <commit> |
| `protected-not-ready` | Review, CI or acceptance is unmet under the base policy | run hn proposal status <candidate> |
| `protected-destination-policy` | A threshold of the destination policy is unmet | gather the evidence the current policy requires |
| `protected-not-fast-forward` | The head does not descend from the current tip | revise the proposal onto <tip> with hn proposal revise |
| `protected-stale-tip` | The branch moved since the client read it | fetch, then retry or revise |
| `protected-blocked` | The candidate is superseded, its lineage is closed or conflicted, it is merged, or a maintainer rejected it | run hn proposal status <candidate> |
| `protected-policy-invalid` | Base or destination policy is missing or malformed, or the push creates the branch | ask the administrator; see recovery in the serve guide |
| `protected-deletion` | The push deletes a protected branch | protected branches cannot be deleted |
| `advance-not-maintainer` | The advancement requester is not a maintainer under both policies | ask a maintainer to run hn merge --remote |
| `proposal-ref-mismatch` | A proposal ref differs from its candidate's signed head or does not descend from its base | push the signed head |
| `proposal-ref-immutable` | The push moves or deletes a hosted proposal ref | create a revision instead |
| `memory-key-mismatch` | The memory stream belongs to another actor | push only your own memory refs |
| `memory-invalid` | The memory stream fails hn-memory/0 validation or is rewritten | inspect the stream with hn memory show |
| `reserved-ref` | The ref is outside every supported class | push branches, tags and Hubnot refs only |
| `tag-immutable` | The push moves or deletes an existing tag | create a new tag |
| `branch-not-owner` | A forced update or deletion of another actor's branch | ask the owner or a maintainer |
| `over-budget` | A pushed Hubnot ref exceeds a server budget | split the history or ask the administrator |
| `lfs-quota` | An LFS upload would exceed the repository quota | ask the administrator to raise the quota |
| `lfs-object-mismatch` | Uploaded LFS content does not match its object ID or size | retry the upload |

## Line format

Each rejected ref update produces exactly two lines on stderr, which Git relays to the client prefixed with `remote: `:

```text
hn-serve/1 reject ref=REF actor=ACTOR code=CODE [ids=ID[,ID]...] [missing=KIND[,KIND]...] [budget=NAME limit=N measured=N] next="NEXT"
hn-serve: SENTENCE
```

Fields appear in the order shown and are separated by single spaces. `REF` is the full ref name, or `-` for session refusals. `ACTOR` is the full mapped actor, or `-` when the key is unknown. `ids` holds full `sha256:` event IDs and full commit IDs. `budget`, `limit` and `measured` appear only with `over-budget` and `lfs-quota`. `NEXT` is printable ASCII without `"` or `\`; control characters and non-ASCII in any value are replaced with `?`. The sentence is one line of plain text. Lines are emitted in ref-name byte order. A parser accepts a line when it starts with `hn-serve/1 reject ` and every field after that matches the grammar; any future field is added with a new version prefix.

## Worked examples

<!-- example: reject-direct-main -->
A mapped actor pushes `main` one commit ahead with no proposal (US1-2):

```text
remote: hn-serve/1 reject ref=refs/heads/main actor=4f1c9a6d0d2b7e3a8c5f6e1b9d0a7c3e2f4b6d8a0c1e3f5a7b9d1c3e5f7a9b1c code=protected-no-proposal ids=9b2e4c6a8d0f1e3c5a7b9d1f3e5c7a9b2d4f6e8a missing=proposal,review,ci,acceptance next="open a proposal for 9b2e4c6a8d0f1e3c5a7b9d1f3e5c7a9b2d4f6e8a"
remote: hn-serve: refs/heads/main is protected and 9b2e4c6a has no accepted, CI-tested proposal.
```

<!-- example: reject-stale-advance -->
The second of two concurrent advancement requests (US3-6):

```text
remote: hn-serve/1 reject ref=refs/heads/main actor=<full maintainer actor> code=protected-stale-tip ids=<expected tip>,<current tip> next="fetch, then retry or revise"
remote: hn-serve: refs/heads/main moved to <current tip> after this request read <expected tip>.
```

The test suite extracts the first example, replaces the illustrative IDs with fixture IDs, and asserts the hook's output matches it field for field.
