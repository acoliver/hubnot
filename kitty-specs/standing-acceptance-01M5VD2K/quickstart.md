# Quickstart: standing acceptance for ordinary changes

This walkthrough shows the flow the mission delivers, with the roles of the spec. It assumes a repository that already uses Hubnot proposals, reviews and CI, with a maintainer `M`, an author `A`, a trusted reviewer `R`, a trusted runner `T` for pipeline `test`, and an executing actor `E` that is not a maintainer. `<M>`, `<A>` and the other angle-bracket names stand for full actor fingerprints, which `hn identity show` prints.

The same steps are the User Story 1, 2 and 4 acceptance tests, and the documented examples marked `standing-*` in `docs/` are executed by those tests.

## 1. The maintainer drafts a standing rule

Edit `.hn/policy.json` on a branch so it contains, alongside the existing sections:

```json
"proposals": {
  "requiredApprovals": 1,
  "requiredAccepts": 1,
  "trustedReviewers": ["<R>"],
  "allowAuthorApproval": false
},
"automerge": {
  "rules": [
    {
      "name": "ordinary",
      "targetBranch": "main",
      "acceptance": "rule",
      "actors": ["<E>"],
      "authors": ["<A>"],
      "reviewers": ["<R>"],
      "paths": ["src/", "tests/", "docs/"]
    }
  ]
}
```

Check the draft against the current policy:

```sh
hn policy check --base main --file .hn/policy.json
```

The comparison lists `automerge rule ordinary acceptance: added (rule)`, the executing actor, the authors, the paths and `automerge rule ordinary reviewers added: <R>`. A draft whose rule covers `.hn/`, overlaps a protected-check path, or sits in a policy with `requiredApprovals` of 0 is refused with a message naming the rule and the field.

## 2. The amendment lands through the manual path

```sh
git commit -am "policy: adopt standing rule ordinary"
hn proposal open --base main --head HEAD "Adopt standing rule ordinary"
# R reviews, T's CI passes, then:
hn decide <amendment> --accept      # as M
hn merge <amendment>                # as M, on main
```

No automerge rule can merge the amendment; `hn automerge <amendment> --rule ordinary` reports `policy amendments require manual merge`. From now on, candidates whose signed base contains this policy can use the rule.

## 3. An ordinary change goes in with no maintainer step

```sh
# A opens a change to src/ and tests/
hn proposal open --base main --head feature "Fix parser edge case"
# R approves, a maintainer or listed requester requests CI, T runs it
hn review <candidate> --approve                 # as R
hn run request <candidate> test                 # as M or a requester of test
hn runner once --accept-pipeline test --accept-actor <M>   # as T
```

E previews and executes from a clean full clone with `main` checked out at the candidate's base:

```sh
hn automerge <candidate> --rule ordinary
hn automerge <candidate> --rule ordinary --execute
```

The preview prints `Acceptance: rule (standing acceptance; no maintainer acceptance required)`, `Counted reviewers:` with `<R>`, `Counted approvals: 1/1 (author approvals do not count)` and no blockers. Execution creates a native two-parent merge commit on `main` and records:

```text
Recorded rule merge event 7d8e9f0a1b2c under rule ordinary with 2 evidence item(s)
```

No maintainer signed anything for this candidate.

## 4. A verifier checks the result

From a different full clone with no Hubnot identity:

```sh
hn sync origin
hn proposal status <candidate>
```

`hn sync` verifies every event, and the rule merge fact is checked against the base policy, the candidate's base and head and its two evidence items. `hn proposal status` reports `Status:   merged` and `Merged by rule ordinary, signed by <E> in fact sha256:...`.

## 5. The same flow through hn serve

On a hosted repository with `main` protected, E saves the all-trusted selection once and then executes with `--remote`:

```sh
hn replication select origin --all-trusted
hn automerge <candidate> --rule ordinary --execute --remote origin
```

The client publishes its facts and imports the hosted ones, inspects the rule again, and pushes the head with `hn-advance`, `hn-expected-tip` and `hn-rule=ordinary`. The server evaluates rule `ordinary` itself and fast-forwards `main` to the head. E's client then records the rule merge fact with `commit` equal to the head and publishes it. The server checks that fact like every other verifier.

The server refuses the request when:

| Situation | Reason code |
|----|----|
| A trusted change request or failure arrived after E's import | `protected-rule-veto` |
| The requester is not an executing actor of `ordinary` | `advance-not-rule-executor` |
| `main`'s policy changed after the candidate's base | `protected-destination-policy` (revise the candidate) |
| The candidate does not satisfy the rule | `protected-rule-not-satisfied` |
| E pushes `main` directly without an advancement request | `protected-not-ready` (maintainer acceptance missing, as today) |

## 6. Recovering from an interruption

If the merge or advancement happened but the fact was not recorded, rerun the same command:

```sh
hn automerge <candidate> --rule ordinary --execute                 # local
hn automerge <candidate> --rule ordinary --execute --remote origin # hn serve
```

The command checks the rule again and records the missing rule merge fact once, without a second merge or advancement. `hn merge` repair stays maintainer-only.

## What stays with maintainers

- Policy amendments and protected-check changes always take the manual path.
- A maintainer rejection blocks execution and advancement.
- An executing actor cannot run `hn decide`, `hn merge` or `hn merge --remote`, and cannot execute any rule that does not list it.
