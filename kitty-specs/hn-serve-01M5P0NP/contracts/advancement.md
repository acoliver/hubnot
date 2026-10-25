# Contract: advancement request and fast-forward merge fact

A maintainer advances a protected branch on an `hn serve` remote with:

```text
hn merge PROPOSAL --remote REMOTE [--branch BRANCH]
```

`PROPOSAL` is a full candidate event ID. `BRANCH` defaults to `main` and may be a short name or `refs/heads/...`. The command never creates a commit, never changes the local checkout, and never moves a local branch. `hn merge PROPOSAL` without `--remote` keeps its existing `--no-ff` behavior (FR-016).

## Client steps

The steps run in order. A failure stops the command with a non-zero exit, and the message names the step.

1. **Validate locally.** Run the existing `hn merge` checks that do not depend on a checkout: full ID, shallow guards, lineage terminal eligibility, not already merged unless step 5 applies, no current rejection, accepted, code available, and the local identity is a maintainer under the base policy. Readiness under the base policy must also hold.
2. **Identify the remote.** `git ls-remote REMOTE refs/hn/serve/v1` must return exactly the object ID of the `hn-serve/1\n` blob in the local object format. Otherwise: `remote REMOTE is not an hn serve remote; use hn merge PROPOSAL in a checkout of the target branch`.
3. **Publish facts.** Run the existing publication phase of `hn sync` (`publishLocalFacts`) so the server holds the maintainer's acceptance and every local candidate and code ref. A rejection here is printed and stops the command.
4. **Read the tip.** `git ls-remote REMOTE refs/heads/BRANCH` gives `T`. Fetch it into `refs/remotes/REMOTE/BRANCH` if `T` is absent locally.
5. **Repair if already advanced.** When the proposal head `H` equals `T` or is an ancestor of `T`, skip to step 8 without pushing. If the candidate is already recorded as merged under its base policy, print `Proposal ID is already advanced on REMOTE BRANCH and recorded as merged` and exit 0.
6. **Request advancement.** Run:

   ```text
   git push --porcelain --push-option=hn-advance=PROPOSAL --push-option=hn-expected-tip=T REMOTE +H:refs/heads/BRANCH
   ```

   The forced refspec makes Git send the update even when it is not a fast-forward, so the server can return `protected-not-fast-forward` with the current tip. On rejection the client prints every `hn-serve/1` line from stderr with the `remote: ` prefix and trailing padding removed, then exits 1. The branch is unchanged.
7. **Confirm.** `git ls-remote REMOTE refs/heads/BRANCH` must now equal `H`. If it does not, exit 1 with `server accepted the push but BRANCH is at X; rerun hn merge PROPOSAL --remote REMOTE`.
8. **Record the merge fact.** Re-collect local events, re-evaluate the candidate, and require it to still be accepted, ready, lineage-eligible and unmerged, and the identity to still be a maintainer. Append `proposal.merged` with `subject` = candidate, `policy` = base policy digest, `head` = `H`, `commit` = `H`, and `evidence` = sorted accepting decision IDs, using the existing `appendProposalMergeEvent`.
9. **Publish again.** Run `publishLocalFacts` so the merge fact reaches the server. A failure here leaves the fact local and prints `merge fact recorded locally; run hn sync REMOTE to publish it`, with exit status 1.

Output on success:

```text
Advanced REMOTE refs/heads/main to 9b2e4c6a8d0f for proposal 1c2d3e4f5a6b
Recorded merge event 7d8e9f0a1b2c with 1 acceptance decision(s)
```

Output after the repair path:

```text
REMOTE refs/heads/main already contains 9b2e4c6a8d0f for proposal 1c2d3e4f5a6b
Recorded missing merge event 7d8e9f0a1b2c with 1 acceptance decision(s)
```

## Server behavior

The server applies the protected-branch rules in [push-verdict.md](push-verdict.md), including `advance-not-maintainer` for a requester who is not a maintainer under both the base and destination policy. A direct `git push` of a protected branch without push options is evaluated by the same rules except the maintainer requirement (OQ-4).

## Interruption and repeat runs

FR-015 requires a rerun to record a missing merge fact exactly once. The table lists each interruption point and what a rerun does.

| Interrupted after | Server branch | Local merge fact | Rerun behavior |
|----|----|----|----|
| Step 3 | unchanged | absent | Full run |
| Step 6, before the server applied the update | unchanged | absent | Full run |
| Step 6, after the server applied the update | `H` | absent | Step 5 repair path records the fact once |
| Step 8 | `H` | present, unpublished | Step 5 reports already merged and exits 0; step 3 of that run publishes the fact |
| Step 9 | `H` | present, published | Step 5 reports already merged and exits 0 |

A second maintainer who runs the command after the first one finished also takes the step 5 path. If the first maintainer's merge fact has not reached the second clone, the second clone records its own fact. Two merge facts for one candidate from different maintainers are valid under current rules, since `lineage.go` treats any merge fact as closing the lineage and `MergeConflict` arises only when different candidates in one lineage are merged.

## Meaning of the merge fact

A `proposal.merged` fact with `commit == head` means the protected branch was fast-forwarded to the proposal head, so the commit that reached the branch is the exact commit that CI tested. A fact with `commit != head` keeps its existing meaning: a `--no-ff` merge commit whose second parent is the head. Validation is identical for both shapes, and `docs/governance-v0.md` documents both.
