# Governance v0

Hubnot policy answers which verified signed claims count for a project. It is
a normal versioned file at `.hn/policy.json`:

The active policy starts with fresh `hn` actor fingerprints. Roles are never
imported from `.nh/policy.json`, `.git/nh/**`, or `refs/nh/*`; those paths can
remain as historical evidence but provide no authority to this protocol.

```json
{
  "version": "hn.policy/0",
  "maintainers": ["<full-actor-fingerprint>"],
  "proposals": {
    "requiredApprovals": 1,
    "requiredAccepts": 1,
    "trustedReviewers": ["<full-actor-fingerprint>"],
    "allowAuthorApproval": false
  },
  "pipelines": {
    "test": {
      "requiredResults": 1,
      "trustedRunners": ["<full-actor-fingerprint>"]
    }
  }
}
```

A pipeline may also list `requesters`: 1–64 actors, besides maintainers,
whose run requests count for that pipeline. Omit the field to keep run
requests maintainer-only. Requesting a run grants nothing else: results still
count only from `trustedRunners`, and requesters gain no review, decision or
merge authority. Older clients reject policies that contain the field.

```json
"test": {
  "requiredResults": 1,
  "trustedRunners": ["<runner-actor>"],
  "requesters": ["<orchestrator-actor>"]
}
```

One runner serves several requesters when it repeats `--accept-actor`, as in
`hn runner once --accept-pipeline test --accept-actor <maintainer-actor> --accept-actor <orchestrator-actor>`.
With more than one accepted actor, the runner executes only requests whose
signer is a maintainer or listed requester under the proposal's base policy.
See [ci-v0.md](ci-v0.md#runner-discovery).

Actor values are complete lowercase SHA-256 fingerprints of Ed25519 public
keys. Unknown JSON fields, duplicate or malformed actors, an empty maintainer
list, unsatisfiable thresholds, invalid pipeline names, and unsupported policy
versions are rejected. Policy files are limited to 1 MiB.

## Roles that sound alike

Three optional policy sections name actors who may ask for work or carry it
out. They are independent: listing an actor in one grants nothing in the
others.

| Field | Who it lists | What the actor may do | Maintainers included |
|---|---|---|---|
| `pipelines.NAME.requesters` | CI requesters | Sign a `run.request` that counts for that pipeline | Yes, always |
| `pipelines.NAME.trustedRunners` | CI runners | Sign a `run.result` that counts | No |
| `automerge.rules[].actors` | Rule executors | Execute that automerge rule | No |
| `audits.requesters` | Audit requesters | Request an audit (see [audits-v1.md](audits-v1.md)) | No |
| `audits.executors` | Audit executors | Execute an audit and sign its result | No |
| `audits.ruleOwners` | Audit rule owners | Authorize or revoke audit rules | No |

Audit roles also require the operator's local authority adoption, and audit
results never count as CI results or merge evidence. CI requesters and rule
executors need no local adoption.

## Inspect policy

```sh
hn policy show [REV]
```

The revision defaults to `HEAD` and resolves to one full commit object ID. The
command runs the canonical parser and validator and prints:

- the resolved commit and full `sha256:<64-hex>` digest of the exact policy
  bytes;
- every maintainer and the required acceptance count;
- every trusted reviewer, approval threshold, and author-approval rule;
- pipelines in lexical order, with required results and full trusted-runner
  actor IDs.

Trust-bearing actor IDs are never shortened in this output.

Trust-bearing commands also require complete `sha256:<64-hex>` event IDs.
Prefixes are display conveniences and are rejected before event lookup, even
when a local prefix would happen to be unambiguous.

## Validate an amendment

```sh
hn policy check --base REV --head REV
hn policy check --base REV --file PATH
```

Exactly one of `--head` and `--file` is required. Both the base and proposed
policy go through the same canonical validation used during candidate
evaluation. `--file` reads an explicit working-tree draft and does not mutate
it. The command prints resolved commit IDs when applicable, both full policy
digests, and a deterministic comparison of:

- exact-byte change;
- maintainers and required acceptances;
- trusted reviewers, required approvals, and author approval;
- added and removed pipelines;
- required-result and trusted-runner changes for every pipeline.

Pipeline names and actor IDs are sorted. A malformed side is identified as
`base` or `proposed`, and validation fails before a candidate is prepared.

## An amendment is an ordinary candidate

A policy amendment introduces no administrator event or mutable control plane.
It is a normal immutable proposal candidate whose `head` changes
`.hn/policy.json`:

```sh
hn policy check --base main --file .hn/policy.json
git add .hn/policy.json
git commit -m "amend collaboration policy"
hn policy check --base main --head HEAD
hn proposal open --base main --head HEAD \
  --body "Governed only by the signed base policy." \
  "Amend collaboration policy"
```

When valid policy bytes differ, `hn proposal open` prints both full digests and
states which base digest governs. The proposed policy becomes relevant only to
later candidates whose signed base commit contains those bytes. A candidate
cannot add a permissive actor or lower a threshold and then use that proposed
authority to accept itself.

## Immutable evaluation context

A candidate signs both a base and a head commit. Its governance policy is
always loaded from:

```text
<signed-base-commit>:.hn/policy.json
```

The SHA-256 protocol ID of those exact bytes is signed by decisions and merge
facts. Policy authority comes only from the explicit actor fingerprints in
that base document.

Identity-continuity projection and replication selection are deliberately
non-authoritative inputs:

- an accepted device/successor relation may explain an actor's relationship,
  but cannot transfer or inherit a role;
- a selected and cryptographically valid actor history is visible, but its
  evidence counts only if that exact actor appears in the applicable base
  policy;
- a successor, related device, or similarly named actor is never substituted
  for a listed actor.

## Readiness and exact evidence

`hn proposal status <full-candidate-id>` independently derives readiness from
accepted repository data:

1. the candidate code ref exists and equals the signed head;
2. the configured number of distinct trusted reviewers currently approve;
3. author approval counts only when the base policy permits it;
4. every required pipeline has enough distinct passing trusted runners;
5. each run request was signed by a base-policy maintainer, or by an actor in
   that pipeline's optional `requesters` list, and binds the exact candidate,
   head, pipeline name, and pipeline-definition digest.

A reviewer's latest review is current. Change requests are displayed but are
not a protocol-level veto. A runner's latest exact result is current for a
pipeline. Evidence for a predecessor or sibling candidate never qualifies a
revision, even when code or lineage is related.

Before publishing an acceptance, the client reloads the verified event set and
re-evaluates the exact base policy. An accept decision signs the exact review
and passing run-result IDs selected as evidence. A rejection requires an
explanation and does not require readiness.

Only actors listed as base-policy maintainers can publish qualifying decisions.
A maintainer's latest decision is current; a current rejection blocks merge.
The required number of distinct current acceptances must be present.

## Destination policy on a protected branch

When a protected branch on an `hn serve` remote moves, the update must satisfy
two policies: the candidate's signed base policy, and the policy at the branch
tip that the update replaces. The first check is the readiness and acceptance
evaluation described above. The second check, the destination check, counts
the same signed reviews, CI results and decisions under the destination
policy:

- reviews count only from the destination policy's trusted reviewers, and an
  author's own approval counts only when the destination policy allows it;
- run requests count only when a destination maintainer or a listed
  destination pipeline requester signed them, results
  count only from each destination pipeline's trusted runners, and each
  pipeline needs the destination's required number of passing results;
- decisions count only when a destination maintainer signed them, and the
  destination's required number of acceptances applies.

Decisions and merge facts are still matched against the base policy digest,
because that is the digest maintainers signed. The destination policy supplies
the actor lists and thresholds. Evidence signed by an actor that the
destination policy no longer lists does not count toward the destination
check, even when the base policy still lists that actor.

A pipeline that only the destination policy requires is loaded from the
candidate's signed head, the same place base-policy pipelines are loaded from.
If the head has no valid definition for it, the destination check fails. See
[protected branches](serve-v0.md#protected-branches) for how `hn serve`
reports these refusals.

## Fast-forward merge facts

A `proposal.merged` fact names the candidate's signed `head` and a resulting
`commit`. Two shapes are valid:

- `commit` equal to `head` records that a protected branch was fast-forwarded
  to the exact tested head;
- `commit` different from `head` records a `--no-ff` merge commit whose second
  parent is the head.

Validation is the same for both shapes. Hubnot checks that the fact binds the
candidate's immutable head, that a base-policy maintainer signed it against
the base policy digest, and that it cites enough current acceptance decisions.
A fact that fails this check is valid only as a rule merge fact, through the
rule check that [Executing a standing rule](#executing-a-standing-rule) and
the protocol describe. Neither check inspects the ancestry of `commit`. `hn merge PROPOSAL` without
`--remote` keeps producing the second shape.

## Merge and conflict recovery

`hn merge <full-candidate-id>` requires:

- a clean worktree on a named branch;
- a current branch descended from the signed base;
- an accepted candidate ref matching the signed head;
- enough base-policy acceptance decisions and no current rejection;
- the active actor to be a base-policy maintainer.

Immediately before changing Git state, merge reloads facts, policy, evidence,
and lineage gates. Git performs a `--no-ff` merge. A successful merge emits a
signed `proposal.merged` event binding the candidate, proposed head, resulting
merge commit, exact base-policy digest, and acceptance decision IDs.

The merge commit and merge event exist locally before either publication step.
Publish collaboration refs with `hn sync`; publish the primary branch with an
ordinary explicit `git push`. Either action can be retried without rewriting
the signed fact.

If Git created the merge commit but event creation or append failed, rerun
`hn merge <full-candidate-id>`. When the proposal head is already contained,
the repair path reloads and verifies current facts, policy, readiness,
acceptance evidence, maintainer authority, and lineage. It records the missing
event only when exactly one first-parent merge commit since the signed base
directly names the proposal head as a parent. It refuses stale evidence,
ambiguous or non-direct containment, an existing merge fact, and shallow data
until the exact dependencies have been recovered. It never performs another
code merge during repair.

On a Git conflict, Hubnot aborts the merge and restores the previously clean
worktree. Resolve the code with Git and publish a new immutable
`proposal.revise` candidate with explicit base and head commits. The original,
siblings, and their evidence remain inspectable. The revision requires new
exact review, CI, and acceptance evidence.

There is no timestamp or delivery-order winner among siblings. A candidate
with successors is superseded; a lineage with a merged member is closed; and
multiple valid merge facts produce a visible merge conflict rather than an
invented winner.

## Optional check protection and automerge

Check protection and automerge are independent repository opt-ins. Omit either
section from `.hn/policy.json` to keep it disabled; `null` also disables it in
clients that support these fields. An empty or incomplete object is invalid.
Initialization, upgrades and imports do not install rules or enable automation.
Enabling either capability is an ordinary governed policy amendment.

The following complete example enables both. Replace every `<...-actor>` with a
full lowercase 64-hex actor fingerprint from `hn identity public`; these are
placeholders, not actor names or provider accounts. The required `lint` pipeline
must exist at `.hn/pipelines/lint.json`.

```json
{
  "version": "hn.policy/0",
  "maintainers": ["<maintainer-actor>"],
  "proposals": {
    "requiredApprovals": 1,
    "requiredAccepts": 1,
    "trustedReviewers": ["<reviewer-actor>"],
    "allowAuthorApproval": false
  },
  "pipelines": {
    "lint": {
      "requiredResults": 1,
      "trustedRunners": ["<runner-actor>"]
    }
  },
  "protectedChecks": {
    "paths": [".golangci.yml", "scripts/lint/"],
    "trustedReviewers": ["<check-reviewer-actor>"],
    "requiredApprovals": 1
  },
  "automerge": {
    "rules": [
      {
        "name": "docs-only",
        "targetBranch": "main",
        "actors": ["<maintainer-actor>"],
        "authors": ["<author-actor>"],
        "paths": ["docs/"]
      }
    ]
  }
}
```

For protection alone, omit `automerge`. For automerge alone, omit
`protectedChecks`. A rule never creates a missing review, passing run, or
acceptance, and it never lowers the existing thresholds.

Inspect the configuration and proposed amendment before committing it:

```sh
hn policy show main
hn policy check --base main --file .hn/policy.json
```

Inspection and comparison expose activation, protected paths, designated
reviewers, thresholds and every automerge rule's actors, authors, paths and target,
along with its acceptance mode and counted reviewers (see
[Inspecting a standing rule](#inspecting-a-standing-rule)).
Older clients that do not recognize these fields reject policies containing
them, including a `null` field. Omit the fields for compatibility with such
clients. Existing signed bytes and historical facts are not rewritten.

### Protected paths and exact review

While `protectedChecks` is enabled, these paths require designated approval:

- Every configured literal file or directory.
- `.hn/policy.json`, including changes that remove or disable protection.
- `.hn/pipelines/<name>.json` for every pipeline required by the signed base policy.

A path is repository-relative. A directory selector ends in `/` and covers its
descendants. Selectors have no glob or expression syntax; absolute paths,
traversal, noncanonical paths, control characters, backslashes, wildcard
characters, colons and `.git` components are rejected. Configuration permits at
most 64 selectors, each at most 4096 bytes. `paths: []` is allowed for protection;
the policy and required pipelines are still protected.
Changed-path Git output is limited to 8 MiB; exceeding the bound refuses the
operation rather than evaluating a truncated change list.

Protection compares Git trees, not the working copy. Additions, removals,
renames, content changes, mode/type changes and replacement of an ancestor count.
A required pipeline cannot evade review by moving or replacing its definition.
Select lint configurations, invoked scripts and other check inputs deliberately.
Protection gates changes to those files; it does not determine whether every
possible source-code or linter change is semantically weaker.

Designated approvals are ordinary `hn review C --approve` events for that exact
candidate. They count only from distinct listed actors other than the candidate
author, even when ordinary policy allows author approval. A designated actor
need not be an ordinary trusted reviewer. An approval may satisfy both roles
only when that exact actor is listed in both places; both thresholds still apply.
There may be 1–64 designated reviewers, and the required approval count must be
between 1 and that reviewer count.

`hn proposal status C` reports changed protected paths and current designated
approval counts. An acceptance signs the exact qualifying review IDs alongside
its ordinary review and run evidence; local and fetched evidence admission
validate those bindings. Revisions need new exact-candidate evidence. A later
withdrawal blocks a new protected merge without invalidating what an earlier
decision originally signed.

### Signed-base status and the current target

Status and immutable acceptance evidence are evaluated against the candidate's
signed base. A new manual merge also checks the actual target. If protection
is enabled there, revise against the current target when either:

- Its exact policy digest differs from the candidate's base-policy digest.
- Any protected path changed from the signed base to the current target,
  even when policy bytes stayed identical.

This prevents a candidate from incorporating a target's stricter lint check,
restoring the weaker old base bytes, and hiding that restoration in an otherwise
documentation-only base-to-head diff. An older acceptance is not authority to
ignore the target's protected baseline. Unprotected target changes alone do not
impose this additional revision requirement. The existing contained-merge repair
path remains governed by the original signed base and evidence.

### Rule-based native automerge

An automerge policy contains 1–64 uniquely named rules. Every rule explicitly
names a local target branch, 1–64 executing actors, 1–64 authors and 1–64 literal
allowed paths. Executing actors must also be base-policy maintainers unless the
rule has acceptance mode `rule` (see
[Standing acceptance rule fields](#standing-acceptance-rule-fields)). Directory
allowlists permit descendants only; allowing `docs/` does not permit replacing
the `docs` ancestor itself. Every changed path must match a rule selector.

```sh
hn proposal status <full-candidate-event-id>
hn automerge <full-candidate-event-id> --rule docs-only
hn automerge <full-candidate-event-id> --rule docs-only --execute
```

The default command is a read-only, keyless preview. It prints full candidate,
base, head and policy identities, the observed target, allowed executing actors,
changed paths and blockers. A blocked preview returns failure. An eligible
preview establishes candidate/target eligibility; execution additionally checks
the selected private signing identity. A preview result never authorizes a later
execution without reevaluation.

After the executing actors, the preview prints three lines about the rule's
acceptance. `Acceptance:` names the acceptance mode and says whether the
proposal needs maintainer acceptance. `Counted reviewers:` lists the full
fingerprints of the reviewers in the rule's `reviewers` field, or reads `all
trusted reviewers` when the rule has no such field. `Counted approvals:` gives
the current approvals the rule counts and the `proposals.requiredApprovals`
threshold, as `N/M`. Under acceptance mode `rule` it adds `(author approvals do
not count)`, because the candidate author's approval never counts there.

Under acceptance mode `rule`, two blockers change. The readiness blocker
`current approval, protected check or CI evidence is not ready` is replaced by
the rule's own thresholds, `rule approval threshold is unmet: N of M counted
approvals` and `pipeline "P" has N of M passing trusted results`, and the
blocker `current required maintainer acceptances are missing` is dropped. A
pipeline result counts only when it answers a run request from a maintainer or
a listed requester of that pipeline. Every other blocker keeps its text and
position. A current change request from any trusted reviewer, counted or not,
a current trusted pipeline failure and a current maintainer rejection still
block the rule. Under acceptance mode `maintainers` the blockers stay as they
were, and a rule with `reviewers` also reports the rule approval threshold.

The preview below is for a candidate by the rule's author that changes
`src/feature.txt` and `tests/feature.txt` under the standing rule `ordinary`,
with one approval from the counted reviewer, one passing result of pipeline
`test` and no maintainer decision. It writes no event and no ref.

<!-- example: standing-preview -->
```text
$ hn automerge <candidate-id> --rule ordinary
Proposal: <candidate-id>
Base: <base-commit>
Head: <head-commit>
Policy: <policy-digest>
Target: main at <base-commit>
Rule: ordinary
Permitted executing actors:
  <executor-actor>
Acceptance: rule (standing acceptance; no maintainer acceptance required)
Counted reviewers:
  <reviewer-actor>
Counted approvals: 1/1 (author approvals do not count)
Changed paths:
  "src/feature.txt"
  "tests/feature.txt"
Eligible: explicit --execute is required to merge locally.
```

Under acceptance mode `maintainers`, execution requires current ordinary and
designated approvals, required passing pipeline results, and the existing
maintainer acceptances. Under acceptance mode `rule` the rule's thresholds
replace the maintainer acceptances, as
[Executing a standing rule](#executing-a-standing-rule) describes. Execution
in either mode also refuses current
trusted change requests or pipeline failures, maintainer rejections, superseded
or closed lineages, mismatched code refs, disallowed authors or changed paths,
policy amendments, and protected-check changes. The latter two take the manual
path even after they receive the required review and acceptance.

The checked-out branch must match the named rule, be clean, and still equal the
candidate's exact signed base. After target movement, create a revision and obtain
fresh evidence. Unfinished merges, rebases, cherry-picks, reverts, sequencer
operations or bisects refuse automation until finished or aborted.

Automerge also refuses shallow repositories and active partial/promisor
configuration before object evaluation. Use a full clone, or explicitly hydrate
and reconcile its configuration outside this command. Downloading some missing
objects alone does not make a configured partial clone eligible. Selected data
that suffices for an ordinary manual merge does not waive this full-repository
requirement. Automerge does not fetch missing objects or invoke shallow
recovery. With `--remote`, it imports hosted facts with `hn sync` only after
these checks pass (see [Commit path, publication and recovery](#commit-path-publication-and-recovery)).

#### Executing a standing rule

An actor listed in the `actors` of a rule with acceptance mode `rule` executes
it with the same command:

```sh
hn automerge <full-candidate-event-id> --rule ordinary --execute
```

The executing actor does not need to be a maintainer. A maintainer listed in the
rule's `actors` follows the same path and records the same kind of merge fact.
Any other identity is refused with `identity <actor> is not an executing actor
for automerge rule "ordinary"`, even when the preview is eligible.

Two things change compared with acceptance mode `maintainers`. No maintainer
decision is needed: the rule's approval and pipeline thresholds and its
executing actors take the place of the maintainer acceptances and the
maintainer identity check. The recorded `proposal.merged` fact names the rule
in its `rule` field, carries the base policy digest in `policy`, and lists the
counted approvals and passing run results, sorted, as its `evidence`. Every
other blocker of the preview still applies. Execution inspects the candidate,
the target and the evidence again immediately before it advances the branch,
under the same expected-old ref lease, and it signs the evidence of that last
inspection.

Before it signs, `hn` builds the fact in memory and judges it with the merge
fact admission that every verifier applies. If verifiers would refuse the fact,
or would admit it as anything other than a rule merge, `hn` signs nothing and
reports `refusing to sign a rule merge fact that verifiers would reject:`
followed by the failed check. The code is already merged at that point, so the
whole message starts with `code merged at <commit> but recording failed:`.

`hn proposal status` prints one line after `Status:   merged` for each rule
merge fact, sorted by fact ID, with the full fingerprint of the signer and the
full fact ID:

```text
Merged by rule ordinary, signed by <executor-actor> in fact <fact-id>
```

A maintainer merge fact that also carries a rule claim prints
`Merged by maintainers in fact <fact-id>; its rule annotation "<name>" is not
verified`. Its maintainer acceptances make it valid, and nothing checked its
rule claim. Merge facts without a rule claim add no line.

The example below runs User Story 1 locally under the standing rule `ordinary`.
Pipeline `test` lists the author in `requesters`, so no maintainer signs
anything for the candidate. The author has committed `src/feature.txt` and
`tests/feature.txt` at `<head-commit>` on top of `main` at `<base-commit>`.
Each `# As` line names the identity that runs the commands below it; each
identity syncs before and after its command. `...` stands for output that
differs between runs. The runner uses the unsafe host backend, which the
README describes and which needs both of its flags on every invocation.

<!-- example: standing-execute -->
```text
# As author:
$ hn proposal open --base <base-commit> --head <head-commit> Add the standing feature
Opened proposal <candidate-short>: Add the standing feature
# As gpt-reviewer:
$ hn review <candidate-id> --approve
Recorded approve review <approval-short> on proposal <candidate-short>
# As author:
$ hn run request <candidate-id> test
Requested pipeline test as run <request-short>
# As runner:
$ hn run execute <request-id> --backend host --allow-unsafe-host-execution
Recorded passed result <result-short> with host backend (exit 0, ...ms)
# As integrator:
$ hn automerge <candidate-id> --rule ordinary
Rule: ordinary
Acceptance: rule (standing acceptance; no maintainer acceptance required)
Counted approvals: 1/1 (author approvals do not count)
Eligible: explicit --execute is required to merge locally.
$ hn automerge <candidate-id> --rule ordinary --execute
Merged proposal <candidate-short> into main at <merge-short>
Recorded rule merge event <fact-short> under rule ordinary with 2 evidence item(s)
$ hn proposal status <candidate-id>
Status:   merged
Merged by rule ordinary, signed by <executor-actor> in fact <fact-id>
```

`<fact-id>` is the full event ID, including its `sha256:` prefix.

### Commit path, publication and recovery

Automatic execution without `--remote` creates a native two-parent merge commit
from the verified head tree and uses an expected-old Git ref transaction for the actual target
advance. It rechecks current evidence and target while holding that lease, then
records the signed `proposal.merged` fact: with the exact acceptances under
acceptance mode `maintainers`, or as a rule merge fact under a standing rule.
Ignored files that the checkout would overwrite cause refusal.

This commit path uses `git commit-tree`, not ordinary `git merge`/`git commit`.
It does not run `pre-merge-commit` or `pre-commit` hooks. Put required checks in
native pipelines and configure their trusted runners in policy; a local Git
merge hook is not an automerge acceptance gate. Operator-controlled Git
`reference-transaction` hooks retain their normal behavior.

Without `--remote`, no command here starts a daemon, executes a pipeline,
creates an approval or acceptance, synchronizes, or pushes. Publication
remains explicit:

```sh
hn sync origin

git push origin main:main
```

An `hn serve` remote accepts no local merge commit on a protected branch, so
the explicit publication above cannot land an automerge there. Use
`hn automerge PROPOSAL --rule NAME --execute --remote REMOTE`, which imports
the hosted facts, inspects the rule again against them and asks the server to
advance the branch to the signed head. See
[Automerge rules](serve-v0.md#automerge-rules).

If code merged but its signed fact could not be recorded under acceptance
mode `maintainers`, inspect the target and retry `hn merge C` for the existing
manual repair. Under a standing rule, rerun the same `hn automerge` command
instead (see [Repairing an interrupted rule merge](#repairing-an-interrupted-rule-merge)). Repair requires current
qualifying evidence and one unique first-parent merge that directly names C's
head; it adds only the missing event. Repeating automerge does not create a
second merge when the target already moved. A failure before ref advancement
can leave unreferenced Git objects; an interrupted checkout can leave staged
candidate changes on the old target. Preserve local work, inspect `git status`
and the target, and reconcile the checkout before retrying. Do not discard local
changes merely to make a retry eligible.

These gates enforce Hubnot's native merge path. They do not prevent a process
with direct host or Git write access from changing branches outside Hubnot, and
the local ref lease is not a distributed transaction across arbitrary writers
or remote replicas.

### Limits of standing acceptance

A standing rule delegates one act: running `hn automerge` for the proposals
the rule covers. An executing actor that is not a maintainer gains no other
authority. Each refusal below happens before any event is signed and before
any ref changes.

| Command run by an executing actor that is not a maintainer | Result |
|----|----|
| `hn automerge PROPOSAL --rule NAME --execute` for a rule that lists the actor | merges when every check of the rule passes |
| `hn automerge PROPOSAL --rule OTHER --execute` for a rule that does not list the actor | refused with `identity <actor> is not an executing actor for automerge rule "OTHER"` |
| `hn decide PROPOSAL --accept` | refused with `identity <actor> is not a maintainer under policy <digest>` |
| `hn merge PROPOSAL` | refused with `identity <actor> is not a maintainer under proposal policy`, or with `proposal lacks N required acceptance decision(s)` while no maintainer has accepted |
| `hn merge PROPOSAL --remote REMOTE` | refused at step 1 with the same maintainer message |
| Any rule for a policy amendment or a protected-check change | refused with `policy amendments require manual merge` or `protected check changes require manual merge` |

The maintainer refusals print shortened actor IDs and policy digests.

Policy amendments and protected-check changes always take the manual path. A
maintainer accepts them with `hn decide --accept` and lands them with
`hn merge`, even when they already carry every review, passing result,
designated approval and acceptance. A candidate also cannot use a rule that
its own head adds. Rules come from the signed base policy, so `hn automerge`
reports `automerge rule "NAME" does not exist in the signed base policy`.

Every blocker of the preview applies to execution under a standing rule: a
maintainer rejection, a change request from any trusted reviewer, a trusted
pipeline failure, too few counted approvals or passing results, an author or
a changed path outside the rule, a target that moved since the signed base, a
dirty worktree, an unfinished Git operation, a shallow clone and a partial
clone. Execution then refuses with `automerge blocked:` followed by the
blockers.

#### Repairing an interrupted rule merge

An interruption can leave the merge commit on the target branch without its
rule merge fact. The error names the command to run next. Under a standing
rule that command is the same `hn automerge` command:

```text
code merged at <merge-commit> but event recording interrupted; run 'hn automerge <candidate-id> --rule ordinary --execute': ...
```

The same rerun hint follows `code merged at <merge-short> but recording
failed:` and the message for an uncertain ref outcome.

Rerun that command in the same clone. Its preview replaces the blocker
`target no longer equals the signed base; ...` with a repair line when all of
these hold:

- the candidate has no merge fact under its base policy;
- the checked-out target contains the head through exactly one first-parent
  merge commit that names the head as a parent;
- the `.hn/policy.json` digest of the target equals the base policy digest;
- every other check of the rule passes on the current evidence.

```text
Repair: code merged at <merge-short>; --execute records the missing rule merge fact
Eligible: explicit --execute is required to merge locally.
```

With `--execute` the command inspects the candidate again and records the
missing rule merge fact once, with `commit` set to the existing merge commit:

```text
Recorded missing rule merge event <fact-short> under rule ordinary with 2 evidence item(s)
```

The repair creates no commit and moves no branch; only the executing actor's
own event chain gains the fact. A second rerun is blocked with `proposal is
already recorded as merged` and changes nothing. When any condition fails,
for example because a trusted change request arrived or the target moved on
with a policy change, the preview keeps the moved-target blocker next to the
other blockers and `--execute` records nothing.

Under acceptance mode `maintainers` the repair is unchanged: the moved-target
blocker directs to `hn merge C`, and its repair path requires a base-policy
maintainer. An executing actor of a standing rule cannot repair through
`hn merge`.

### Standing acceptance rule fields

An automerge rule may carry two optional fields, `acceptance` and `reviewers`.

`acceptance` sets the rule's acceptance mode. Its value is exactly the string
`"maintainers"` or the string `"rule"`. A rule without the field uses
`maintainers`, which is the behavior described above: every executing actor
must be a base-policy maintainer, and every proposal still needs its maintainer
acceptances.

A rule with `"acceptance": "rule"` is a standing rule. The maintainer-accepted
policy amendment that adopted the rule is the standing acceptance for every
proposal that satisfies it, so maintainers do not accept those proposals one
by one. They grant this acceptance once, when they accept the amendment. The
executing actors of a standing rule need not be maintainers; an automation
identity can execute the rule and sign its merge fact. Listing an actor in a
rule's `actors` lets it execute that rule and grants it nothing else. Under a
standing rule the candidate author's own approval never counts, even when
`allowAuthorApproval` is true.

`reviewers` lists the trusted reviewers whose approvals count toward the rule,
in either acceptance mode. It holds 1 to 64 distinct full actor fingerprints.
Each one must also appear in `proposals.trustedReviewers`, and the list needs
at least `proposals.requiredApprovals` entries. A current change request from
any trusted reviewer still blocks the rule, whether or not the reviewer is in
the list. Without the field, approvals from every trusted reviewer count.

Policy validation adds three limits to a standing rule:

- `proposals.requiredApprovals` must be at least 1, so every proposal merged
  under the rule carries at least one independent approval.
- No path selector may be `.hn` or `.hn/` or start with `.hn/`. Policy and
  pipeline definitions stay under maintainer acceptance, so a standing rule
  cannot change the rules that govern it.
- No path selector may overlap a `protectedChecks.paths` selector. Remove one
  trailing `/` from each selector; the two overlap when they are equal, when
  the protected selector starts with the rule selector followed by `/`, or when
  the rule selector starts with the protected selector followed by `/`. With
  the protected path `scripts/lint.sh`, the rule paths `scripts/` and
  `scripts/lint.sh` overlap, and `scripts/lint.sh.bak` and `script/` do not.
  With the protected path `scripts/`, the rule path `scripts/lint/run.sh`
  overlaps and `scriptsx/` does not. Changes to protected checks keep their
  designated review and maintainer acceptance because a standing rule could
  otherwise weaken a check without any maintainer decision.

Policy amendments and protected-check changes still take the manual path under
both acceptance modes.

A rule without either field keeps its policy bytes, validation and behavior
exactly as before. Clients that predate these fields reject any policy that
contains either one, including `"acceptance": "maintainers"`. Omit both fields
while such clients must still read the policy.

#### Refused standing rules

Each draft below starts from a valid policy with the maintainer
`<maintainer-actor>`, the trusted reviewers `<reviewer-actor>` and
`<second-reviewer-actor>`, one required approval, one required acceptance, no
pipelines, and one rule named `ordinary` for the target `main` with the actor
`<maintainer-actor>`, the author `<author-actor>` and the path `docs/`. The
`rule:` line lists the fields the draft sets on that rule, `proposals:` lists
the fields it sets in `proposals`, and `policy:` lists the sections it adds to
the policy. `hn policy check --base main --file .hn/policy.json` refuses each
draft with the message on its `refusal:` line and changes nothing.

<!-- example: standing-policy-refusals -->
```text
# A standing rule needs at least one required approval.
rule: {"acceptance": "rule", "actors": ["<executor-actor>"]}
proposals: {"requiredApprovals": 0}
refusal: automerge rule "ordinary" has acceptance "rule", which requires proposals.requiredApprovals of at least 1

# A present reviewers list needs 1 to 64 entries.
rule: {"acceptance": "rule", "actors": ["<executor-actor>"], "reviewers": []}
refusal: automerge rule "ordinary" requires 1 to 64 reviewers when reviewers is present

# Reviewers are distinct full actor fingerprints.
rule: {"acceptance": "rule", "actors": ["<executor-actor>"], "reviewers": ["<reviewer-actor>", "<reviewer-actor>"]}
refusal: automerge reviewers contains duplicate actor <reviewer-actor>

# Every reviewer is a trusted reviewer.
rule: {"acceptance": "rule", "actors": ["<executor-actor>"], "reviewers": ["<untrusted-actor>"]}
refusal: automerge rule "ordinary" reviewer <untrusted-actor> is not a trusted reviewer

# Reviewers can supply the required approvals.
rule: {"acceptance": "rule", "actors": ["<executor-actor>"], "reviewers": ["<reviewer-actor>"]}
proposals: {"requiredApprovals": 2}
refusal: automerge rule "ordinary" lists 1 reviewers, fewer than proposals.requiredApprovals (2)

# A standing rule cannot cover .hn.
rule: {"acceptance": "rule", "actors": ["<executor-actor>"], "paths": ["docs/", ".hn/pipelines/"]}
refusal: automerge rule "ordinary" path ".hn/pipelines/" covers .hn, which a rule with acceptance "rule" cannot cover

# A standing rule cannot overlap a protected check path.
rule: {"acceptance": "rule", "actors": ["<executor-actor>"], "paths": ["docs/", "scripts/"]}
policy: {"protectedChecks": {"paths": ["scripts/lint.sh"], "trustedReviewers": ["<reviewer-actor>"], "requiredApprovals": 1}}
refusal: automerge rule "ordinary" path "scripts/" overlaps protected check path "scripts/lint.sh"
```

#### Inspecting a standing rule

`hn policy show` prints an `Acceptance:` line for every rule, followed by the
executing actors, the proposal authors, the counted reviewers and the allowed
paths. A standing rule reads `Acceptance: rule (standing acceptance; no
maintainer acceptance per proposal)` and lists its actors under `Executing
actors:`. A rule in mode `maintainers` reads `Acceptance: maintainers
(maintainer acceptance required)` and keeps the `Executing maintainers:`
heading. A rule without `reviewers` prints `Counted reviewers: all trusted
reviewers`.

`hn policy check` adds three lines after each rule's path lines: the
acceptance mode and the reviewers added and removed. The acceptance value is
`added (MODE)` or `removed (MODE)` when the whole rule is added or removed,
`unchanged (MODE)` when the mode stays the same, and `changed (OLD -> NEW)`
when it moves between modes. A rule without `acceptance` compares as
`maintainers`, so adding `"acceptance": "maintainers"` to an existing rule
reports `unchanged (maintainers)`. A rule without `reviewers` compares as an
empty list. Rules appear in name order.

Read these lines before accepting a policy amendment. Accepting an amendment
that adds a standing rule, or moves a rule to acceptance mode `rule`, is the
one maintainer act that grants standing acceptance. The lines show who can
execute the rule, whose proposals it covers, whose approvals count and which
paths it reaches, all as full identities.

The policy below adds the standing rule `ordinary`. On a `main` whose policy
is the same document without the `automerge` section, `hn policy check` ends
with the lines shown. After the amendment lands on `main`, `hn policy show
main` ends with the rule as shown.

<!-- example: standing-policy-rule -->
```json
{
  "version": "hn.policy/0",
  "maintainers": ["<maintainer-actor>"],
  "proposals": {
    "requiredApprovals": 1,
    "requiredAccepts": 1,
    "trustedReviewers": ["<reviewer-actor>"],
    "allowAuthorApproval": false
  },
  "pipelines": {},
  "automerge": {
    "rules": [
      {
        "name": "ordinary",
        "targetBranch": "main",
        "acceptance": "rule",
        "actors": ["<executor-actor>"],
        "authors": ["<author-actor>"],
        "reviewers": ["<reviewer-actor>"],
        "paths": ["src/", "tests/", "docs/"]
      }
    ]
  }
}
```

```text
$ hn policy check --base main --file .hn/policy.json
...
  automerge rule ordinary enabled: changed (false -> true)
  automerge rule ordinary target branch: "" -> "main"
  automerge rule ordinary actors added: <executor-actor>
  automerge rule ordinary actors removed: (none)
  automerge rule ordinary authors added: <author-actor>
  automerge rule ordinary authors removed: (none)
  automerge rule ordinary paths added: docs/, src/, tests/
  automerge rule ordinary paths removed: (none)
  automerge rule ordinary acceptance: added (rule)
  automerge rule ordinary reviewers added: <reviewer-actor>
  automerge rule ordinary reviewers removed: (none)
```

```text
$ hn policy show main
...
Automerge: enabled (explicit execution only)
Rule ordinary; target branch main:
  Acceptance: rule (standing acceptance; no maintainer acceptance per proposal)
  Executing actors:
  <executor-actor>
  Proposal authors:
  <author-actor>
  Counted reviewers:
  <reviewer-actor>
  Allowed literal paths:
    "docs/"
    "src/"
    "tests/"
Pipelines:
  (none)
```

## Standing acceptance traceability

This section maps every functional requirement, non-functional requirement,
success criterion and user-story scenario of the standing acceptance
specification, `kitty-specs/standing-acceptance-01M5VD2K/spec.md`, to the tests
that prove it. `TestStandingQualityTraceability` reads the IDs from the
specification when it runs. It fails when an ID has no row or more than one
row, when a row has an ID the specification does not define, when a row names
no test, and when a named test is not declared in a root `*_test.go` file.
The full regression gate of NFR-007 also runs `gofmt -l`, `go vet ./...`,
`go build ./...`, `go test -race ./...` and `git diff --check`.

### Requirements

| ID | Requirement | Tests |
|----|----|----|
| FR-001 | Acceptance mode field | `TestStandingPolicyFieldsRefusals`, `TestStandingPolicyFieldsAcceptedValues`, `TestStandingPolicyFieldsFoldedKeyCaseIsStillValidated` |
| FR-002 | Non-maintainer executing actors | `TestStandingPolicyFieldsRefusals`, `TestStandingPolicyFieldsAcceptedValues`, `TestStandingPolicyFieldsLegacyRuleKeepsCheckOrder` |
| FR-003 | Path safety for standing acceptance | `TestStandingPolicyFieldsRefusals`, `TestStandingPolicyFieldsSelectorOverlap`, `TestStandingPolicyDocumentedRefusals`, `TestStandingQualityCaseVariantDirectoryIsOrdinaryContent` |
| FR-004 | Minimum independent review | `TestStandingPolicyFieldsRefusals`, `TestStandingPolicyDocumentedRefusals` |
| FR-005 | Rule reviewers | `TestStandingPolicyFieldsRefusals`, `TestRuleAcceptanceLiveRuleReviewersRestrictCounting`, `TestRuleAcceptanceLiveChangeRequestVetoes`, `TestStandingAutomergePreviewRuleReviewers` |
| FR-006 | Author approval under standing acceptance | `TestRuleAcceptanceLiveAuthorApprovalByMode`, `TestStandingAutomergePreviewRuleReviewers` |
| FR-007 | Policy inspection | `TestStandingPolicyInspectionShow`, `TestStandingPolicyInspectionCheck`, `TestStandingPolicyDocumentedRule`, `TestStandingPolicyFieldsPolicyCheckRefusals` |
| FR-008 | Preview | `TestStandingAutomergePreviewStandingRule`, `TestStandingAutomergeDocumentedPreview`, `TestStandingAutomergePreviewMaintainersRuleOptOut` |
| FR-009 | Execution without per-proposal acceptance | `TestStandingAutomergeExecuteUserStory1`, `TestStandingAutomergeContainmentBlockersAtExecution`, `TestStandingAutomergeContainmentRepositoryState`, `TestStandingServeAdvanceUserStory4` |
| FR-010 | Rule merge fact | `TestStandingEventCanonicalClaimRoundTrip`, `TestStandingEventNonCanonicalRuleIsNoClaim`, `TestStandingEventSigningRefusesNonCanonicalRule`, `TestStandingEventEncodingUnchangedWithoutRule`, `TestStandingAutomergeExecuteUserStory1`, `TestStandingQualityDocumentedRuleMergeFact` |
| FR-011 | Rule merge signer | `TestRuleAcceptanceFactDefects`, `TestStandingFactLocalDefects`, `TestStandingAutomergeExecuteMaintainerAndSecondRule` |
| FR-012 | Re-derivation by every verifier | `TestRuleAcceptanceFactDefects`, `TestRuleAcceptanceFactCheckOrder`, `TestRuleAcceptanceLiveRequestAndResultQualification`, `TestStandingFactLocalDefects`, `TestStandingFactImportDefects`, `TestStandingFactShallowDefects`, `TestStandingFactMatrixAgreement` |
| FR-013 | Evidence availability | `TestStandingFactImportWithheldApproval`, `TestStandingFactShallowMissingDependencies`, `TestStandingFactShallowMaintainerMergesUnchanged` |
| FR-014 | Missing code objects | `TestStandingFactLocalValidRuleMerge`, `TestStandingFactImportObjectAccess`, `TestStandingFactImportAbsentCommit`, `TestStandingFactShallowNoEmptyDiff`, `TestStandingFactShallowRecovery` |
| FR-015 | Scope of fact validation | `TestRuleAcceptanceFactImmutable`, `TestStandingFactMatrixAgreement` |
| FR-016 | hn serve advancement under a rule | `TestStandingServeVerdictAcceptsRuleAdvancement`, `TestStandingServeVerdictVetoes`, `TestStandingServeVerdictRuleNotSatisfied`, `TestStandingServeWireRuleCatalogue`, `TestStandingServeWireRuleCodesRoundTrip` |
| FR-017 | Target policy must equal base policy | `TestStandingAutomergePreviewPolicyChanges`, `TestStandingServeVerdictDestinationPolicy` |
| FR-018 | Direct protected pushes unchanged | `TestStandingServeVerdictAcceptsRuleAdvancement`, `TestStandingServeAdvanceForbiddenCommands` |
| FR-019 | Bounded executing-actor authority | `TestStandingAutomergeContainmentForbiddenCommands`, `TestStandingAutomergeRepairConstructedState`, `TestStandingServeAdvanceForbiddenCommands`, `TestStandingServeRecoveryRefusals` |
| FR-020 | Recovery of an interrupted rule merge | `TestStandingAutomergeRepairInterruptedRun`, `TestStandingAutomergeRepairConstructedState`, `TestStandingAutomergeRepairRerunHints`, `TestStandingServeRecoveryInterruptions`, `TestStandingServeRecoveryCompletedRun`, `TestStandingServeRecoveryRefusals`, `TestStandingServeRecoveryAcceptanceMergeRefused`, `TestStandingServeRecoveryHostedBranchLacksCommit` |
| FR-021 | Status names the rule | `TestStandingAutomergeExecuteUserStory1`, `TestStandingAutomergeExecuteStatusOfMaintainerMerges` |
| FR-022 | Compatibility | `TestStandingCompatOlderClient`, `TestStandingEventLegacyVerdictsUnchanged`, `TestStandingPolicyFieldsLegacyEncodingIsUnchanged` |
| FR-023 | Documentation with tested examples | `TestServeQualityDocumentedExamplesAreExecuted`, `TestStandingQualityDocumentedRuleMergeFact`, `TestStandingAutomergeDocumentedPreview`, `TestStandingAutomergeDocumentedExecute`, `TestStandingPolicyDocumentedRefusals`, `TestStandingPolicyDocumentedRule`, `TestStandingServeDocumentedAdvance`, `TestStandingServeDocumentedVeto`, `TestStandingServeWireRuleReasonDocsTable`, `TestStandingQualityDocsStyle` |
| FR-024 | Union validity of merge facts | `TestRuleAcceptanceAdmitMergeFact`, `TestRuleAcceptanceAdmitMergeFactWithValidator`, `TestStandingFactLocalRuleClaimOnMaintainerMerge`, `TestStandingFactMatrixAgreement` |
| FR-025 | Fail-closed activation | `TestStandingFactBoundary`, `TestStandingServeWireRuleRefusedBeforeVerdict`, `TestStandingHarnessLegacyAndBoundaryFixtures` |
| NFR-001 | Fail-closed verification | `TestStandingFactMatrixAgreement`, `TestStandingFactLocalDefects`, `TestStandingFactImportDefects`, `TestStandingFactShallowDefects` |
| NFR-002 | Verdict agreement | `TestStandingFactMatrixAgreement`, `TestStandingAutomergeAgreementPreviewExecute`, `TestStandingServeAdvanceAgreementPreviewServer`, `TestStandingServeAdvanceAuthorizationAndTransport` |
| NFR-003 | Executing-actor containment | `TestStandingAutomergeContainmentForbiddenCommands`, `TestStandingServeAdvanceForbiddenCommands`, `TestStandingServeRecoveryRefusals` |
| NFR-004 | Opt-out compatibility | `TestStandingCompatOlderClient`, `TestStandingCompatPreExistingTestsFrozen`, `TestStandingAutomergePreviewMaintainersRuleOptOut` |
| NFR-005 | Bounded validation work | `TestRuleAcceptanceFactMemo`, `TestStandingFactLocalMemoSharedAcrossFacts`, `TestStandingFactMatrixChangedPathBound` |
| NFR-006 | Import cost | `TestStandingQualityImportTimeNFR006` |
| NFR-007 | Regression gate | `TestStandingQualityRootSourcesAreGofmtFormatted`, `TestStandingCompatPreExistingTestsFrozen` |

### Success criteria

| ID | Success criterion | Tests |
|----|----|----|
| SC-001 | Every acceptance scenario passes through the public CLIs, locally and through `hn serve` | `TestStandingQualityTraceability`, `TestStandingAutomergeExecuteUserStory1`, `TestStandingServeAdvanceUserStory4` |
| SC-002 | An ordinary change merges with zero maintainer-signed events for it | `TestStandingAutomergeExecuteWithoutMaintainer`, `TestStandingServeAdvanceUserStory4` |
| SC-003 | No forged or insufficient rule merge fact is accepted | `TestStandingFactMatrixAgreement`, `TestStandingFactMatrixChangedPathBound` |
| SC-004 | A fresh verifier reproduces every rule merge verdict | `TestStandingFactMatrixExecutionAdmitted`, `TestStandingAutomergeExecuteUserStory1`, `TestStandingServeAdvanceUserStory4` |
| SC-005 | No policy amendment or protected-check change merges by rule | `TestStandingAutomergeContainmentManualPath`, `TestStandingFactLocalDefects`, `TestStandingFactMatrixAgreement` |
| SC-006 | Every requirement maps to tests and the full gate passes | `TestStandingQualityTraceability`, `TestStandingQualityTraceabilityCheckRejectsGaps` |

### User-story scenarios

| ID | Scenario | Tests |
|----|----|----|
| US1-1 | The executing actor merges with no maintainer decision | `TestStandingAutomergeExecuteUserStory1`, `TestStandingAutomergeDocumentedExecute` |
| US1-2 | A clone with no identity verifies the rule merge | `TestStandingAutomergeExecuteUserStory1`, `TestStandingFactImportValidRuleMerge` |
| US1-3 | The same evidence under acceptance mode `maintainers` is refused | `TestStandingAutomergeExecuteUserStory1`, `TestStandingAutomergePreviewStandingRule` |
| US1-4 | The keyless preview shows the standing rule | `TestStandingAutomergePreviewStandingRule`, `TestStandingAutomergeDocumentedPreview` |
| US2-1 | `hn policy check` lists the added standing rule | `TestStandingPolicyInspectionCheck`, `TestStandingPolicyDocumentedRule` |
| US2-2 | The amendment takes the manual path | `TestStandingAutomergeContainmentManualPath`, `TestStandingHarnessAdoption` |
| US2-3 | A candidate cannot use a rule its own head adds | `TestStandingAutomergePreviewPolicyChanges`, `TestStandingAutomergeContainmentManualPath` |
| US2-4 | A standing rule cannot list `.hn` paths | `TestStandingPolicyFieldsRefusals`, `TestStandingPolicyDocumentedRefusals` |
| US2-5 | A standing rule cannot overlap a protected path | `TestStandingPolicyFieldsRefusals`, `TestStandingPolicyFieldsSelectorOverlap` |
| US2-6 | Malformed acceptance and reviewers fields are refused | `TestStandingPolicyFieldsRefusals`, `TestStandingPolicyFieldsPolicyCheckRefusals`, `TestStandingPolicyDocumentedRefusals` |
| US3-1 | Import refuses each defective fact | `TestStandingFactImportDefects` |
| US3-2 | `hn serve` refuses each defective fact | `TestStandingFactMatrixAgreement` |
| US3-3 | Local projection refuses each defective fact | `TestStandingFactLocalDefects` |
| US3-4 | Shallow verification requires the fact's inputs | `TestStandingFactShallowDefects`, `TestStandingFactShallowMissingDependencies`, `TestStandingFactShallowRecovery` |
| US3-5 | An absent evidence ID is a missing dependency | `TestStandingFactImportWithheldApproval` |
| US3-6 | A maintainer merge with a rule claim is a maintainer merge | `TestStandingFactLocalRuleClaimOnMaintainerMerge`, `TestStandingFactBoundary`, `TestStandingAutomergeExecuteStatusOfMaintainerMerges` |
| US3-7 | Legacy history keeps its verdicts | `TestStandingFactLocalLegacyHistory`, `TestStandingFactImportLegacyHistory`, `TestStandingFactShallowLegacyHistory`, `TestStandingEventLegacyVerdictsUnchanged`, `TestStandingFactMatrixAgreement` |
| US4-1 | The executing actor advances a protected branch | `TestStandingServeAdvanceUserStory4`, `TestStandingServeVerdictAcceptsRuleAdvancement`, `TestStandingServeDocumentedAdvance` |
| US4-2 | A new clone verifies the hosted rule merge | `TestStandingServeAdvanceUserStory4` |
| US4-3 | A late veto refuses the advancement | `TestStandingServeVerdictVetoes`, `TestStandingServeAdvanceVeto`, `TestStandingServeDocumentedVeto` |
| US4-4 | A direct push still needs maintainer acceptance | `TestStandingServeVerdictAcceptsRuleAdvancement`, `TestStandingServeAdvanceForbiddenCommands` |
| US4-5 | An actor outside the rule is refused | `TestStandingServeVerdictAcceptsRuleAdvancement`, `TestStandingServeAdvanceAuthorizationAndTransport` |
| US4-6 | A changed destination policy is refused | `TestStandingServeVerdictDestinationPolicy` |
| US4-7 | A rerun records a missing fact once | `TestStandingServeRecoveryInterruptions`, `TestStandingServeAdvanceInterruptedAfterPublish` |
| US4-8 | A rerun publishes a recorded fact without a second advancement | `TestStandingServeRecoveryInterruptions`, `TestStandingServeRecoveryCompletedRun` |
| US5-1 | Only the rule's reviewers count | `TestStandingAutomergePreviewRuleReviewers`, `TestRuleAcceptanceLiveRuleReviewersRestrictCounting`, `TestStandingFactMatrixExecutionAdmitted` |
| US5-2 | A change request from any trusted reviewer blocks | `TestStandingAutomergePreviewRuleReviewers`, `TestRuleAcceptanceLiveChangeRequestVetoes` |
| US5-3 | The author's own approval never counts | `TestStandingAutomergePreviewRuleReviewers`, `TestRuleAcceptanceLiveAuthorApprovalByMode` |
| US5-4 | A maintainers-mode rule with reviewers records an acceptance merge fact | `TestStandingQualityMaintainersModeReviewersExecution`, `TestStandingAutomergePreviewRuleReviewers`, `TestRuleAcceptanceLiveMaintainersModeWithReviewers` |
| US6-1 | A maintainer rejection refuses execution | `TestStandingAutomergePreviewVetoesAndBlockers`, `TestStandingAutomergeContainmentBlockersAtExecution`, `TestStandingServeVerdictRuleNotSatisfied` |
| US6-2 | Change requests, failures and missing results refuse execution | `TestStandingAutomergePreviewVetoesAndBlockers`, `TestStandingAutomergeContainmentBlockersAtExecution`, `TestStandingServeVerdictVetoes` |
| US6-3 | An author or path outside the rule refuses execution | `TestStandingAutomergePreviewVetoesAndBlockers`, `TestStandingAutomergeContainmentBlockersAtExecution` |
| US6-4 | Policy and protected-check changes take the manual path | `TestStandingAutomergeContainmentManualPath` |
| US6-5 | Repository state refusals still apply | `TestStandingAutomergeContainmentRepositoryState` |
| US6-6 | The executing actor gains no other authority | `TestStandingAutomergeContainmentForbiddenCommands`, `TestStandingServeAdvanceForbiddenCommands` |
| US6-7 | A result answering an unlisted requester does not count | `TestStandingAutomergePreviewVetoesAndBlockers`, `TestRuleAcceptanceLiveRequesterAuthorization` |
| US7-1 | Repositories that do not opt in are unchanged | `TestStandingCompatOlderClient`, `TestStandingAutomergePreviewMaintainersRuleOptOut` |
| US7-2 | An older client rejects the new policy fields | `TestStandingCompatOlderClient` |
| US7-3 | An older client rejects rule-only facts | `TestStandingCompatOlderClient` |
| US7-4 | Both clients accept a maintainer merge with a rule claim | `TestStandingCompatOlderClient` |
| US7-5 | Both clients accept legacy history | `TestStandingCompatOlderClient` |
