# Identity continuity and local keyring v0

Identity has two deliberately separate surfaces:

| Public signed facts | Private local state |
| --- | --- |
| Actor public keys and fingerprints | Private signing keys |
| `identity.authorize` and `identity.accept` events | Active-actor pointer |
| Actor refs under `refs/hn/actors/*` | Rotation journals and replication selections |
| Deterministic continuity projection | Files below the repository's `.git/hn/` |

Only the left column is stored as Git objects or transported. Never copy
`.git/hn`, an identity record, or a private key to establish a second device.

The `hn` namespace begins a new trust root. It does not inspect `.git/nh/**`,
reuse a legacy actor fingerprint, or infer continuity from `refs/nh/*`.
Operators initialize fresh actors; coexistence of both namespaces in one Git
repository is historical coexistence, not protocol compatibility.

## Actor invariant

An actor is the lowercase hexadecimal SHA-256 fingerprint of exactly one
Ed25519 public key. It has one append-only event chain and one private signer.
Different devices initialize distinct actors. A person, device group, or key
lineage is a projection over signed relationships among those actors—not a
shared actor key and not a policy principal created by inference.

`hn identity public` emits only display name, full actor fingerprint, and
raw-base64 public key. `hn identity show` emits the active name, actor, and
actor ref; it never emits private key material.

`hn identity show --json` emits exactly one `hn.identity/1` envelope containing
`{schema,ok:true,data:{actor,name,public_key}}`. The key is public raw-base64;
private keys and identity filesystem paths are never included. Failures have
`{schema,ok:false,error:{code,message}}` and a nonzero exit; `invalid_input`
means invalid flags/arguments, `repository_error` means identity unavailable.
Machine discovery is read-only: it validates the active identity record or a
legacy identity without initializing, migrating, rotating, or repairing private
state. Valid legacy public fields can be inspected before explicit migration.
Invalid or absent identity state returns `repository_error`; it does not fall
back past an invalid active pointer. Private file paths, bytes, modes and Git
refs remain unchanged. Default human output and legacy migration remain unchanged.

Use the full lowercase actor fingerprint as `--actor ACTOR` on issue mutations
to pin the actual signing identity. Omission retains the current active-identity
default. A mismatch gives `actor_mismatch` without appending, even for a replay;
repair the caller binding deliberately. The command captures one identity and
uses that same signer through append: changing the active pointer afterward
does not substitute another signer. Pinning is not a distributed issue lock.

## Mutual relationship facts

The authorizing actor publishes:

```sh
hn identity authorize \
  --relationship device \
  --actor <full-64-hex-target-actor> \
  --public-key <raw-base64-target-public-key>
```

The target actor independently publishes:

```sh
hn identity accept <full-sha256-authorization-event-id>
```

`authorize` rejects shortened/malformed actors, unsupported relationship
values, public keys whose fingerprint differs from the named actor, and
self-targets. `accept` resolves the full signed authorization and requires the
active actor and public key to match its exact target.

`device` keeps both actors active in the projection. `successor` expresses
planned replacement: exactly one accepted acyclic successor retires the
predecessor in the identity projection. Neither relationship changes project
policy.

## Separate-device operator flow

In each clone, initialize independently:

```sh
# Existing device
hn init --name "Existing device"
hn identity public

# New device, in a separate clone
hn init --name "Review device"
hn identity public
```

Exchange only the printed public fields. On the existing device, authorize the
new actor and publish collaboration refs:

```sh
hn identity authorize --relationship device \
  --actor <full-new-device-actor> \
  --public-key <new-device-public-key>
hn sync origin
```

Save an exact selection in the new clone, synchronize the authorizing actor,
and accept the full authorization event ID:

```sh
hn replication select origin \
  --actor <full-existing-device-actor> \
  --max-events 10000 \
  --max-objects 100000 \
  --max-object-bytes 16777216 \
  --max-attachment-bytes 1048576 \
  --max-total-bytes 268435456
hn sync origin
hn identity accept <full-sha256-authorization-event-id>
hn sync origin
```

Then add both full actor selectors in each clone, synchronize, and inspect:

```sh
hn replication select origin \
  --actor <full-existing-device-actor> \
  --actor <full-new-device-actor> \
  --max-events 10000 \
  --max-objects 100000 \
  --max-object-bytes 16777216 \
  --max-attachment-bytes 1048576 \
  --max-total-bytes 268435456
hn sync origin
hn identity list
```

The relationship can now be reconstructed from public facts in an
identity-free clone. To give the new actor a reviewer, runner, maintainer, or
decision role, amend `.hn/policy.json` through the ordinary base-governed
candidate workflow.

## Deterministic projection

Projection validates the signed payload of every identity event rather than
trusting decoded fields supplied by a caller. Output ordering is derived from
IDs, never arrival order.

- Authorization without matching acceptance is `pending`.
- One or more matching acceptances produce one `accepted` edge; duplicate
  acceptance facts remain visible and add no authority.
- An acceptance whose authorization is unavailable is reported with both full
  acceptance and subject IDs. No relation is inferred.
- A signer or public-key mismatch is a conflict.
- Multiple accepted outgoing successors are `competing-successors`; the
  predecessor becomes ambiguous and no successor wins.
- An accepted successor cycle marks its members ambiguous and reports every
  involved actor/event ID.
- A conflict fetched later changes the derived projection, but never changes
  existing payloads, signatures, decisions, or base-policy authority.
- Timestamps and cross-actor delivery order never resolve a conflict.

## Local keyring and permissions

Current local state is rooted in `hn/` below the clone's Git common
directory, the directory `git rev-parse --git-common-dir` reports. In an
ordinary clone that is `.git/hn/`; in a bare repository it is `hn/` inside the
repository directory:

```text
.git/hn/
├── active
├── identities/
│   └── <full-actor-fingerprint>.json
├── identity.json             # optional legacy migration source
├── keyring.lock              # empty; held with flock(2) while the keyring changes
├── rotation.json             # only while rotation is incomplete
├── state.lock                # empty; flock(2) for replication state, see replication-v0.md
└── rotation-command.json     # only while rotation/retry is incomplete
```

Linked worktrees created with `git worktree add` share the common directory,
so every worktree of a clone uses this one keyring and signs as the clone's
actor. `hn init` run first in a linked worktree creates the keyring in the
common directory, where the main checkout and the other worktrees find it.
`GIT_DIR` and `GIT_COMMON_DIR` select the directories as they do for Git.
The worktrees also share the actor ref, which `hn` advances with a
compare-and-swap. When commands in two worktrees append for the actor at the
same time, one appends and the other fails without appending (issue commands
report `concurrent_update`); retrying the failed command succeeds.

Commands that change the keyring take an exclusive `flock(2)` on
`keyring.lock` in the keyring's directory, so commands in every worktree
that share a keyring contend for one lock. The kernel releases the lock when
a process exits, so an interrupted command never leaves it held. Publishing
the first active pointer and replacing it during rotation hold the lock,
waiting up to 30 seconds for another holder. When two first-time `hn init`
commands race, exactly one identity becomes active; the other command
reports that an identity already exists and deletes the unused key it
generated. This holds on file systems without hard links too (see below).

`hn identity rotate` holds the lock for its whole transaction: reading the
journals and the active pointer, generating the target, writing both
journals, appending the signed authorization and acceptance, switching the
pointer, and removing the journals. A second rotation that finds the lock
held does not wait, because once the first finished it would start a further
rotation from the new identity. It fails without writing anything:

```text
hn: another identity rotation or identity update for this keyring is in progress, possibly in another worktree of this clone; retry hn identity rotate when it finishes
```

Only the lock holder writes the rotation journals, so simultaneous rotations
leave at most one consistent pair of journals. If the rotation holding the
lock is interrupted, the next `hn identity rotate` from any worktree completes
that rotation with its journaled target.

Earlier releases kept the keyring in each linked worktree's own private Git
directory, `.git/worktrees/<name>/hn/`. A linked worktree whose private
directory still holds identity state keeps using that keyring as a whole,
including legacy migration and rotation, while the main checkout and other
worktrees keep the clone's keyring. Identity state here means an `active`
pointer, `identity.json`, either rotation journal, or a `*.json` record in
`identities/`. The two locations are never combined: a worktree keyring with
a record but no `active` pointer reports no identity and does not borrow the
common pointer. Other private state, such as the memory index, may create
`hn/` in the worktree's directory; that directory alone does not select a
worktree keyring. If the worktree's private `hn/` or `hn/identities/` cannot
be inspected, or is a symbolic link or not a directory, identity commands in
that worktree fail with an error naming that location and never use the
common keyring instead. `hn` does not move or merge a per-worktree keyring. Its actor stays distinct, and can be
related to the clone's actor with `hn identity authorize` and
`hn identity accept` like any other device. Such a worktree has its own
`keyring.lock` beside its own keyring.

Other private state follows the state it guards. Replication selections and
acceptance journals guard the clone's shared refs and objects, so they also
live below the Git common directory; [replication-v0.md](replication-v0.md#linked-worktrees-share-selections-and-acceptance-state)
describes them and the rules for copies an earlier release left in a linked
worktree. Two kinds of private state stay in each worktree's own Git
directory. The memory index (`hn/memory/index-v0.json`) is a derived cache
that `hn` verifies against the live accepted sources and the projection
policy before every use and rebuilds when they differ; it guards nothing,
and worktrees on different commits would otherwise invalidate one shared
cache in turn. The audit authority anchor (`hn/audit/authority.json`) is an
explicit, opt-in operator selection of the branch whose policy grants audit
roles; a worktree without one refuses every audit write, request, and
execution, so it cannot widen what the clone accepts, and an operator may
anchor different worktrees to different branches.

The paths above are logical and must never be committed or copied into
documentation evidence. Private directories must be regular directories with
mode `0700`. Private records, the active pointer, and transaction journals
must be regular non-symlink files with mode `0600`. Unknown fields, trailing JSON, oversized state, wrong actor
paths, mismatched key pairs, symlinks, and unsafe modes fail closed.

Writes use a mode-`0600` temporary file, file sync, atomic rename, and directory
sync. The first active pointer is published with a hard link instead of a
rename, so it never replaces a pointer that another command created first.
On a file system without hard links, the pointer is published with a fresh
absence check and an atomic rename while the command holds the keyring lock.
Every `hn` command that writes the pointer holds that lock, so no other
command can create the pointer between the check and the rename, and readers
still see either no pointer or a complete one. An older `hn` release does not
take the lock; there, only the hard-link publication is exclusive. Legacy `.git/hn/identity.json` migration recomputes and preserves the
same actor/public/private key bytes, writes the actor record durably, and then
creates the active pointer. Retrying does not generate a replacement actor.
The legacy source may remain on disk; once `active` exists, ordinary loading
uses the keyring record it names.

## Planned rotation

```sh
hn identity rotate --name "Rotated device"
hn identity show
hn identity list
hn sync origin
```

Rotation creates a distinct target actor and a `successor` authorization from
the predecessor plus an acceptance from the target. The predecessor remains
active until both exact signed events are durable and verified. Only then does
the active pointer switch. Completed rotation journals are removed.

If interrupted, the private journals bind the predecessor, target key, and any
durable event IDs. A retry verifies those exact signed events, appends only a
missing side, and completes the pointer switch. It refuses replacement event
IDs, changed target material, competing matching facts, or a changed active
predecessor. The predecessor history remains public and inspectable.

Rotation does not give the successor any inherited project role. A separate
policy amendment is required if the successor should become authoritative.

## Explicit deferrals

The operational alpha does not implement:

- lost-key recovery or rotation after the predecessor key is unavailable;
- compromise response or revocation with global erasure;
- social, organizational, or threshold recovery;
- concurrent disconnected writers sharing one actor/private key;
- implicit policy migration to devices or successors;
- a claim that distinct keys prove distinct people.

Published continuity facts are immutable and may be superseded or made
ambiguous by later facts. Hubnot cannot remove copies already replicated by
others.
