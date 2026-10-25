# Hubnot protocol v0 experiment

This document records the format implemented by the operational alpha. It is a
testable sketch, not a compatibility promise. Existing version-0 event payloads
and event IDs were not changed when identity continuity was added. Agent memory
is the separate additive `hn-memory/0` protocol documented in
[memory-v0.md](memory-v0.md); memory records are not inserted into `hn/0`
actor chains or event kinds.

`hn/0` is a breaking namespace reset, not a compatible spelling of `nh/0`.
The runtime accepts and writes only `hn` wire versions and `refs/hn/*`; legacy
wire bytes and `refs/nh/*` may remain immutable evidence but are ignored.

## Actor identity

An actor is exactly one Ed25519 key pair. Its identifier is the lowercase
hexadecimal SHA-256 digest of the 32-byte public key:

```text
actor = hex(sha256(ed25519_public_key))
```

Each actor has one append-only event chain. A device group or key lineage is a
projection over distinct actors; it is never a shared actor secret. The
raw-base64 public key remains in every event so each event can be verified from
its own signed bytes.

## Event payload and identity

An event is UTF-8 JSON emitted without insignificant whitespace. The complete
version-0 field order is:

```json
{
  "protocol": "hn/0",
  "kind": "issue.open",
  "actor": "<full-public-key-fingerprint>",
  "actorName": "Alice",
  "publicKey": "<raw-base64-Ed25519-public-key>",
  "sequence": 1,
  "timestamp": "2026-08-29T12:00:00Z",
  "previous": "<full-previous-event-id>",
  "subject": "<full-subject-event-id>",
  "relationship": "device",
  "targetActor": "<full-target-actor-fingerprint>",
  "targetKey": "<raw-base64-target-public-key>",
  "title": "An issue title",
  "body": "Optional body",
  "base": "<full-Git-commit-id>",
  "head": "<full-Git-commit-id>",
  "verdict": "approve",
  "pipeline": "test",
  "definition": "<full-pipeline-digest>",
  "commit": "<full-Git-commit-id>",
  "outcome": "passed",
  "exitCode": 0,
  "durationMs": 1,
  "log": "<full-log-digest>",
  "backend": "sandbox",
  "platform": "linux/amd64",
  "runner": "hn/<version>",
  "policy": "<full-policy-digest>",
  "evidence": ["<full-evidence-event-id>"],
  "issue": {"title": "Current work", "body": "Description", "status": "open", "criteria": [], "labels": [], "assignees": [], "relations": [], "metadata": {}},
  "parents": ["<full-prior-issue-revision-id>"],
  "operation": "actor-scoped-operation-key",
  "request": "<canonical-request-sha256-id>",
  "intent": "revise"
}
```

This is a field inventory, not a valid single event: fields unused by an event
kind are omitted. `previous` identifies the preceding event in the same actor
chain. `subject` identifies the exact event on which a dependent fact relies.
Optional string, integer-zero, and empty-list fields are omitted by the current
encoder.

The event identifier is independent of Git's configured object-hash format:

```text
event_id = "sha256:" + hex(sha256(exact_event_payload_bytes))
```

The signature is Ed25519 over those exact payload bytes. Verification does not
reserialize JSON. Every accepted event must have protocol `hn/0`, a positive
sequence, a valid RFC 3339 timestamp, a public key whose fingerprint equals
`actor`, and a valid signature. Full event IDs have the form
`sha256:<64-lowercase-hex>`.

The implemented kinds are:

```text
issue.open
issue.comment
issue.revise
proposal.open
proposal.revise
review.submit
run.request
run.result
proposal.decision
proposal.merged
identity.authorize
identity.accept
```

## Issue roots, revisions and reliable operations

The optional `issue`, `parents`, `operation`, `request`, and `intent` fields
are appended to the Event field inventory. When absent they are omitted, so
existing signed payload bytes and IDs remain unchanged. The `hn.issue/1`
versioned CLI envelope is separate from the signed `hn/0` event protocol.

`issue.open` establishes stable identity. A legacy title/body opening maps to
an open issue; a rich opening additionally signs full IssueState with matching
title/body. `issue.revise` signs `subject` equal to the opening ID, a complete
`issue` state, and sorted unique `parents` identifying 1–200 same-issue opening
or revision events. It carries no separate title/body fields. `issue.comment`
signs its opening subject and body, without state or revision parents.

`previous` is the previous event in this actor's history; `parents` are the
issue-content ancestors and may belong to other actors. The verified projection
computes all maximal issue revisions and preserves conflicts without a selected
state. Explicit reconciliation appends a successor referencing the consumed
heads; staged reconciliation preserves all unconsumed heads.

IssueState contains title, body, open/closed status, criteria with local IDs,
labels, assignees, typed relations and namespaced string metadata. Criteria
order is preserved; label/assignee/relation sets are canonically sorted and
reject duplicates. Assignment, criteria and status confer no policy or consumer
execution authority. Root/parent/relation dependencies participate in exact
replication closure and stay quarantined if suppliers are absent.

`intent` distinguishes open/revise/resolve/close/reopen/comment. When an
operation key is present, `operation` and `request` are both signed. The request
digest is SHA-256 over the canonical intent/kind/subject/parents/state/title/body
encoding; actor sequence, timestamp and operation key are excluded from that
encoding, while the operation key is separately scoped to its signer. The
reader recomputes the digest from signed semantics. Reusing a key for different
semantics fails; identical retries return the original event before checking
current issue heads. No cross-actor atomicity or shared operation authority is
implied.

See [issues-v1](issues-v1.md) for complete limits, machine schema, directed graph,
retry and staged-resolution examples. Older executables may reject the new
kind; reverting an executable does not erase newly signed facts.

## Identity continuity events

`identity.authorize` adds exactly these event-specific fields:

```json
{
  "relationship": "device",
  "targetActor": "<full-target-actor-fingerprint>",
  "targetKey": "<raw-base64-target-public-key>"
}
```

`relationship` is exactly `device` or `successor`. `targetActor` must be a full
64-hex fingerprint, must equal the fingerprint derived from `targetKey`, and
must differ from the signer. `body` may carry an explanation. Its ordinary
`previous` field is present unless it is the signer's first event.

`identity.accept` uses `subject` for the full authorization event ID. Its
signer and public key must exactly equal that authorization's target actor and
key. Its ordinary `previous` field is present unless it is the target actor's
first event.

The deterministic projection applies the following rules over verified facts:

- authorization without a matching acceptance is `pending`;
- one or more matching acceptances produce one accepted edge; duplicate exact
  claims remain inspectable but add no authority;
- an accepted `device` edge keeps both actors active;
- exactly one accepted acyclic `successor` edge retires the predecessor in the
  identity projection;
- multiple accepted outgoing successors are `competing-successors`;
- accepted successor cycles are `successor-cycle` conflicts;
- an acceptance whose authorization is absent is reported as a missing fact;
- a signer/key mismatch is a conflict, never a relationship;
- timestamps and cross-actor delivery order never pick a winner.

Edges, actors, conflicts, acceptance IDs, and event IDs are sorted for stable
inspection. Identity projection is descriptive only. It does not edit policy
and does not grant a maintainer, reviewer, runner, or decision role.

## Proposal candidates and revisions

`proposal.open` signs a title and distinct `base`/`head` Git commit IDs.
`proposal.revise` is a new immutable candidate and signs:

```text
subject   full event ID of the exact predecessor candidate
base      full resolved Git commit used as the revised diff base
head      full resolved Git commit containing the revised code
body      optional explanation
title     omitted; inherited from the lineage root
```

The predecessor must be an available `proposal.open` or `proposal.revise`
event, and the same actor must sign both. Base and head are distinct commits.
Missing predecessors, actor mismatches, and cycles fail closed.

One predecessor may have multiple successors. IDs are sorted for deterministic
projection, but no timestamp, delivery order, or actor-chain position creates a
global latest winner. Evidence always binds one exact candidate; evidence for
a predecessor or sibling cannot qualify a revision.

## Git representation and refs

Every event is a Git commit whose tree contains:

```text
event.json    exact signed payload bytes
signature     raw-base64 Ed25519 signature
```

A `run.result` tree also contains `log.txt`; the exact log bytes hash to the
signed `log` field. No other tree entry is accepted. The commit parent is the
preceding event commit for that actor. The signed `previous` event ID provides
an independent protocol chain.

Public roots are:

```text
refs/hn/actors/<full-actor-fingerprint>
refs/hn/proposals/<candidate-event-sha256-without-prefix>
```

The candidate ref points to the exact signed `head` commit, making the proposed
code reachable for transport and garbage collection. A mismatch fails closed.

Accepted remote-tracking roots are local:

```text
refs/hn/remotes/<remote>/actors/<full-actor-fingerprint>
refs/hn/remotes/<remote>/proposals/<candidate-event-sha256-without-prefix>
```

Actor chains begin at sequence 1, increase by one, and match both Git parents
and signed `previous` IDs. Different events at the same actor sequence are a
fork and are rejected by the current projection.

## Runs and governance events

A `run.request` references a candidate through `subject` and signs the
repository-local pipeline name, SHA-256 ID of the exact pipeline JSON bytes,
and proposed Git head. A `run.result` references the request and repeats all
three bindings. It also signs `passed` or `failed`, exit code, duration, exact
log digest, backend, platform, and runner. Any mismatch is rejected.

These events are attestations. Signatures protect attribution and contents;
they do not prove honest execution. The exact base policy chooses which actors'
claims count.

A `proposal.decision` references one candidate, the SHA-256 ID of its exact
base policy bytes, and an `accept` or `reject` verdict. Accept decisions include
the exact review and run-result event IDs that satisfy policy. Rejections
require an explanation.

A `proposal.merged` event binds the candidate, proposed head, resulting Git
commit, base-policy digest, and acceptance-decision IDs. It is emitted after
Git creates the merge commit; branch publication remains a separate ordinary
Git action.

## Optional policy rules

`hn.policy/0` may include independent optional `protectedChecks` and `automerge`
sections. Absence or `null` disables each in supporting clients; either may be
enabled alone. No existing signed payload is re-encoded. Clients that do not
recognize a present field reject that policy rather than infer permissive rules.
See [governance](governance-v0.md#optional-check-protection-and-automerge) for the
full schema, selector bounds and commands.

Protection qualifies existing `review.submit` events. A protected change needs
distinct designated actors other than the candidate author, signing approval
for that exact candidate. Selectors come from the signed base policy and include
its policy file and every required pipeline definition. Tree comparisons include
removal, rename, mode/type and ancestor changes. A `proposal.decision` acceptance
includes the exact qualifying designated-review IDs in its existing `evidence`
array; local and quarantine admission enforce the same base-bound rule. Current
withdrawals affect later merge eligibility, not the validity of older signed
acceptance evidence.

A prospective manual merge separately inspects the current target. Enabled
protection there requires revision when its exact policy digest differs from
the candidate's base, or protected paths changed from base to target even under
identical policy bytes. This prevents hidden reversion of a newer check to old
base bytes. It does not reinterpret historical decisions or contained-merge
repair under a later policy.

Automerge introduces no event kind, implicit role or automatic decision. A named
base-policy rule restricts executing maintainers, authors, target branch and
allowed paths in addition to existing current review/run/acceptance requirements.
Default preview is keyless and read-only; execution is explicit. Version-one
automation requires a full non-promisor repository, a clean checked-out rule
target equal to the signed base, and no unfinished Git operation. It refuses
policy amendments and protected changes for manual handling.

Automatic commit creation uses the verified head tree with signed base and head
as the two parents. An expected-old ref transaction guards the actual advance
and a final evidence/target check. This path uses `git commit-tree`, so ordinary
`pre-merge-commit` and `pre-commit` hooks are not invoked; required check authority
comes from native policy and pipeline evidence. Operator `reference-transaction`
hooks remain Git behavior. The resulting `proposal.merged` is the existing
signed fact binding candidate, head, result commit, base policy and exact
acceptance IDs. Missing-event repair uses ordinary `hn merge`; collaboration
and branch publication remain explicit separate actions.

## Synchronization

`hn replication select` records full per-remote actor/candidate selectors and
positive budgets below `.git/hn/`. A saved selection is authoritative. With no
saved selection, version 0 uses bounded compatibility-all: it enumerates only
advertised `refs/hn/actors/*` and `refs/hn/proposals/*`, then applies the same
quarantine, budgets, validation, and promotion transaction.

Each selected ref is fetched with its own exact refspec into a generated bare
quarantine repository. It is measured, structurally and cryptographically
validated, and checked for exact relationships before objects are copied and
accepted refs are atomically updated. Invalid, over-budget, or
dependency-missing selections do not become accepted projection roots.
Standard Git may download a selected pack before measurement, so the budgets
are hard before promotion and retention, not portable pre-download network
quotas. [Replication v0](replication-v0.md) defines the transaction and shallow
recovery boundary in detail.

Publication uses ordinary explicit Git pushes of local actor and candidate
refs. `hn sync` never publishes the repository's primary branch; use a
separate explicit `git push` for that outcome.

## hn serve wire surface

`hn serve` hosts repositories over SSH and answers with ordinary Git
transports. This section defines what a client can observe from such a server:
a marker ref, two push options and a line format for rejections. `serve_wire.go`
and `serve_reasons.go` implement it.

### Server marker and identification

Every hosted repository advertises:

```text
refs/hn/serve/v1
```

The ref points at a blob whose content is exactly the 11 bytes `hn-serve/1`
followed by one newline (`0x0a`). A push that targets the ref is refused with
`reserved-ref`.

A client identifies a remote by running
`git ls-remote --refs REMOTE refs/hn/serve/v1` and computing
`git hash-object --stdin` of the same 11 bytes in its own repository, without
writing the object. The remote identifies as `hn serve` only when the listing
is exactly one line, that line names `refs/hn/serve/v1`, and its object ID
equals the computed ID. The ID depends on the object format, so a SHA-256
client identifies only a SHA-256 marker and a SHA-1 client only a SHA-1
marker. A transport error, an empty listing, an extra line, a different ref or
a different object ID all mean the remote does not identify as `hn serve`.

Identification only decides which client modes are allowed, such as
all-trusted selection and `hn merge --remote`. It grants no trust. Anyone can
create the marker on an ordinary remote, so a client validates every ref it
imports exactly as it does for any other remote.

### Push options

A client may send these push options with `git push --push-option`:

| Option | Value | Rule |
|----|----|----|
| `hn-advance` | full candidate event ID, `sha256:` followed by 64 lowercase hex digits | requests advancement of a protected branch to that candidate's signed head |
| `hn-expected-tip` | full commit ID of the branch tip the client read | the update is refused with `protected-stale-tip` when the branch has moved |

Each option may appear at most once. A push with `hn-advance` must contain
exactly one ref update, and that update must target a protected branch. Any
other option, a repeated option, a malformed value, or an `hn-advance` push
with another shape rejects every ref update in the push with
`unsupported-command`.

### Rejection lines

Each rejected ref update produces exactly two lines on stderr. Git relays them
to the pushing client with a `remote: ` prefix.

```text
hn-serve/1 reject ref=REF actor=ACTOR code=CODE [ids=ID[,ID]...] [missing=KIND[,KIND]...] [budget=NAME limit=N measured=N] next="NEXT"
hn-serve: SENTENCE
```

Fields appear in the order shown, separated by single spaces. Bracketed fields
are omitted when they have no value.

| Field | Content |
|----|----|
| `ref` | full ref name, or `-` for a refusal of the whole session |
| `actor` | full mapped actor fingerprint, or `-` when the key is unknown |
| `code` | one code from the reason-code table below |
| `ids` | comma-separated full `sha256:` event IDs and full commit IDs |
| `missing` | comma-separated evidence kinds, always in the order `proposal`, `review`, `ci`, `acceptance` |
| `budget`, `limit`, `measured` | the exceeded budget, its limit and the measured value; see the budget fields below |
| `next` | the next action in double quotes; always last |

Every value is printable ASCII. Control bytes and non-ASCII bytes in any value
are replaced with `?`, one `?` per byte. Space, `=` and `,` inside `ref`,
`actor`, `code`, `ids`, `missing` and `budget` values are also replaced with
`?`. Inside `next`, the double quote (byte `0x22`) and the backslash (byte
`0x5c`) are also replaced with `?`, so the quoted value cannot end early or
contain an escape. The sentence follows the same control and non-ASCII rule
and stays on one line.

When a push has several rejected ref updates, the line pairs appear in byte
order of the ref name before replacement. A session refusal uses `ref=-`.

A parser accepts a line only when it starts with `hn-serve/1 reject ` and every
following field matches this grammar in this order. Unknown fields, fields out
of order, repeated fields, an unknown code, an unknown or repeated evidence
kind, a limit that is not canonical decimal, a missing `next` and text after
the closing quote are all errors. The sentence line is not parsed. A new field
or a change to an existing field requires a new version prefix such as
`hn-serve/2`.

A client reading `git push` stderr removes one leading `remote: ` prefix and
the trailing spaces Git adds to relayed lines, treats a carriage return as a
line break, ignores Git's own lines such as `! [remote rejected]`, and pairs
each rejection line with the `hn-serve: ` line that follows it.

For example, a push of `main` one commit ahead with no proposal produces:

```text
remote: hn-serve/1 reject ref=refs/heads/main actor=4f1c9a6d0d2b7e3a8c5f6e1b9d0a7c3e2f4b6d8a0c1e3f5a7b9d1c3e5f7a9b1c code=protected-no-proposal ids=9b2e4c6a8d0f1e3c5a7b9d1f3e5c7a9b2d4f6e8a missing=proposal,review,ci,acceptance next="open a proposal for 9b2e4c6a8d0f1e3c5a7b9d1f3e5c7a9b2d4f6e8a"
remote: hn-serve: refs/heads/main is protected and 9b2e4c6a has no accepted, CI-tested proposal.
```

### Rejections reported by hn sync

`hn sync REMOTE` publishes local actor, proposal and memory refs with one
`git push` per ref. When such a push fails, the error names the publication
phase and the remote, as it does for any remote. If Git's stderr for that push
carries `hn-serve/1` rejection lines and the remote identifies as `hn serve`,
the error continues with those rejections, one line pair per rejected ref:

```text
hn: publication phase failed after replication: replication actor publication failed for remote origin
hn-serve/1 reject ref=refs/hn/actors/ACTOR actor=ACTOR code=actor-chain-rewrite next="append new events instead"
hn-serve: The push rewrites refs/hn/actors/ACTOR.
```

The client parses each rejection and renders it again with the rules above.
It never echoes the server's bytes, so a hostile server cannot place terminal
control sequences in the output. A rejection line that does not parse is
dropped. A rejection without a sentence line gets the meaning from the
reason-code table as its sentence. A remote that does not identify as
`hn serve` gets the plain error even when its hooks print lines in this format.

### Budget fields

`budget`, `limit` and `measured` appear together and only with the codes
`over-budget` and `lfs-quota`. `limit` and `measured` are non-negative decimal
integers without a sign or leading zeros. `over-budget` names the exceeded
server budget. For a pushed Hubnot ref that is the first exceeded replication
budget, using the names `hn replication show` prints: `max-events`,
`max-objects`, `max-object-bytes`, `max-attachment-bytes` or
`max-total-bytes`. `lfs-quota` uses
`budget=lfs-quota`, with the quota in bytes as `limit` and the stored bytes the
upload would produce as `measured`.

### Reason codes

The next action is a template. A server replaces placeholders such as
`<commit>` with the full ID before it sends the line.

| Code | Meaning | Typical next action |
|----|----|----|
| `unknown-key` | The authenticating key is not in the registry | ask the administrator to run `hn serve key add` |
| `unhosted-repository` | The requested repository is not hosted or its path is unsafe | check the clone URL |
| `unsupported-command` | The SSH request, push option set or advancement shape is not supported | use `git fetch`, `git push`, `hn merge --remote` or `git lfs` |
| `repository-busy` | Another push held the repository lock for more than 120 seconds | retry the push |
| `actor-key-mismatch` | The pushed actor chain belongs to another actor | push only `refs/hn/actors/<your actor>` |
| `actor-chain-invalid` | An event fails signature, sequence, shape or relationship validation | inspect the named event with `hn log` |
| `actor-chain-rewrite` | The push deletes or rewrites hosted actor history | append new events instead |
| `dependency-missing` | A referenced event or Git object is not hosted and not in the push | publish the named fact or commit first |
| `protected-no-proposal` | No proposal has the pushed commit as its signed head | open a proposal for `<commit>` |
| `protected-not-ready` | Review, CI or acceptance is unmet under the base policy | run `hn proposal status <candidate>` |
| `protected-destination-policy` | A threshold of the destination policy is unmet | gather the evidence the current policy requires |
| `protected-not-fast-forward` | The head does not descend from the current tip | revise the proposal onto `<tip>` with `hn proposal revise` |
| `protected-stale-tip` | The branch moved since the client read it | fetch, then retry or revise |
| `protected-blocked` | The candidate is superseded, its lineage is closed or conflicted, it is merged, or a maintainer rejected it | run `hn proposal status <candidate>` |
| `protected-policy-invalid` | Base or destination policy is missing or malformed, or the push creates the branch | ask the administrator; see recovery in the serve guide |
| `protected-deletion` | The push deletes a protected branch | protected branches cannot be deleted |
| `advance-not-maintainer` | The advancement requester is not a maintainer under both policies | ask a maintainer to run `hn merge --remote` |
| `proposal-ref-mismatch` | A proposal ref differs from its candidate's signed head or does not descend from its base | push the signed head |
| `proposal-ref-immutable` | The push moves or deletes a hosted proposal ref | create a revision instead |
| `memory-key-mismatch` | The memory stream belongs to another actor | push only your own memory refs |
| `memory-invalid` | The memory stream fails hn-memory/0 validation or is rewritten | inspect the stream with `hn memory show` |
| `reserved-ref` | The ref is outside every supported class | push branches, tags and Hubnot refs only |
| `tag-immutable` | The push moves or deletes an existing tag | create a new tag |
| `branch-not-owner` | A forced update or deletion of another actor's branch | ask the owner or a maintainer |
| `over-budget` | A pushed Hubnot ref exceeds a server budget | split the history or ask the administrator |
| `lfs-quota` | An LFS upload would exceed the repository quota | ask the administrator to raise the quota |
| `lfs-object-mismatch` | Uploaded LFS content does not match its object ID or size | retry the upload |

## Known limits

- One actor remains single-writer; disconnected concurrent writers sharing a
  private key are unsupported.
- Planned rotation requires the predecessor key. Lost-key, compromise, social,
  and organizational recovery are not implemented.
- Continuity and selection never confer policy authority.
- There is no portable hard pre-download byte quota, moderation network,
  selective global deletion, redaction guarantee, or global erasure of
  already replicated immutable facts.
- JSON canonicalization and extension negotiation are not stable
  cross-language contracts yet.
- Hosted ref retention, large-scale behavior, and providers not listed in
  [host compatibility](host-compatibility.md) remain unproven.

## Alpha native resource kinds

The optional-native-resource expansion registers `project`, `project-item`,
`discussion`, `discussion-reply` and `workflow` `.open` / `.revise` facts under the
existing `hn/0` envelope. Typed versioned JSON lives in `body`; the existing
`subject`, `parents`, `intent`, `operation`, `request` fields bind exact aggregate
history and actor-global replay intent. No identity, policy or CI authority is
added. New-kind raw JSON is strict and bounded (256 KiB, workflows 2 MiB);
existing signed bytes and legacy admission behavior remain unchanged. Older
alpha clients reject these new unknown kinds and must be upgraded before selecting
histories that contain them. Exact container/schema/answer references participate
in reader, quarantine and shallow recovery closure before promotion. See
[Native resources](resources-v1.md) for typed schemas, command grammar, conflict
projection, bounds and authority limits.
