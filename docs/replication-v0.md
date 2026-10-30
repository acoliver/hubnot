# Selected replication and shallow recovery v0

Hubnot imports collaboration facts through an explicit validate-before-
promotion transaction. Selection controls which remote histories may enter the
local accepted projection; project policy independently controls whose valid
claims qualify as authority.

## Save a per-remote selection

```sh
hn replication select [REMOTE] \
  [--actor <full-64-hex-actor>]... \
  [--proposal <full-sha256-candidate-event-id>]... \
  [--memory <full-sha256-stream-id>]... \
  [--all | --all-trusted] \
  [--max-events N] \
  [--max-objects N] \
  [--max-object-bytes N] \
  [--max-attachment-bytes N] \
  [--max-total-bytes N]

hn replication show [REMOTE]
```

The remote defaults to `origin`. Actor, candidate, and memory-stream selectors
must be full IDs. Every budget is a positive integer. `--all` cannot be
combined with exact selectors, and an exact selection must name at least one
actor, candidate, or memory stream. An actor/candidate-only selection imports
zero memory; memory selection is independent transport authorization.

Selections are sorted and saved as owner-only local state below
`hn/replication/selections/` in the clone's Git common directory, which is
`.git/hn/replication/selections/` in an ordinary clone; they are never
committed or published. Every linked worktree of the clone uses the same
selections (see [Linked worktrees](#linked-worktrees-share-selections-and-acceptance-state)).
`show` reports whether the selection was explicitly saved, whether
compatibility-all is active, every full selected ID, and every budget.

When flags are omitted, the saved budget defaults are:

| Budget | Default |
| --- | ---: |
| `max-events` | 10,000 |
| `max-objects` | 100,000 |
| `max-object-bytes` | 67,108,864 (64 MiB) |
| `max-attachment-bytes` | 16,777,216 (16 MiB) |
| `max-total-bytes` | 1,073,741,824 (1 GiB) |

No saved selection means version-0 bounded compatibility-all. The client
enumerates advertised `refs/hn/actors/*`, `refs/hn/proposals/*`, and
`refs/hn/memory/*/*`, accepts only well-formed ref names, and sends every
discovered ref through the same quarantine, budgets, validation, and promotion
path. A memory stream ID must resolve to one unambiguous actor owner. An
explicit saved selection remains authoritative until replaced.

## All-trusted selection for hn serve remotes

```text
hn replication select [REMOTE] --all-trusted [--actor ACTOR]... [--proposal ID]... [--memory STREAM]... [budgets]
```

An `hn serve` remote accepts actor chains and proposal refs only after they
pass its push checks. Runners and verifiers that follow such a remote can save
an all-trusted selection and skip naming each actor and candidate. On every
`hn sync REMOTE`, the selection requests each advertised
`refs/hn/actors/<full-actor>` and `refs/hn/proposals/<candidate-hash>` ref
with a well-formed name, in sorted ref order, followed by each exact `--actor`
and `--proposal` selector. An exact selector for an advertised ref is
requested once.

### How the client identifies an hn serve remote

A remote identifies as `hn serve` when
`git ls-remote --refs REMOTE refs/hn/serve/v1` returns exactly one line whose
object ID equals the local `git hash-object` result for the 11-byte blob
`hn-serve/1
`. Any other answer, including a transport error, means the
remote does not identify. `hn replication select --all-trusted` checks
identification before it saves anything and refuses an ordinary remote:

```text
hn: remote origin does not identify as hn serve; --all-trusted requires an hn serve remote
```

Identification only decides whether the mode may run. Anyone who can push to
an ordinary remote can write the marker ref, so the marker grants no trust.
Every requested ref goes through the quarantine transaction described below:
the saved per-ref budgets, structural validation, relationship and dependency
validation, identity continuity, and atomic promotion into
`refs/hn/remotes/REMOTE/...`. An actor chain with a broken signature fails as
`structurally-invalid` and promotes nothing, exactly as under an exact
selection. The marker ref lies outside the advertised patterns that
replication reads, so it is never imported.

`hn sync` checks identification again on every run. When the remote no longer
identifies, the import phase fetches nothing and stops with
`remote REMOTE no longer identifies as hn serve; reselect with hn replication select`.
The publication phase still runs, and the command exits 1. Exact selections,
`--all` and the no-selection compatibility mode never check identification and
behave the same against an `hn serve` remote and an ordinary remote holding
the same refs.

### Selector rules and unadvertised refs

- `--all-trusted` cannot be combined with `--all`. The command refuses with
  `--all-trusted is mutually exclusive with --all` before it contacts the
  remote.
- Exact `--actor`, `--proposal` and `--memory` selectors may be added. They
  are validated exactly as in an exact selection. Budget flags, defaults and
  validation are unchanged.
- An exact actor or proposal selector that the remote does not advertise yet
  reports `not-advertised` and prints, for example,
  `Replication actor <full-actor>: not advertised yet`. Nothing is requested
  or promoted for it, and it does not count as a failure, so `hn sync` can
  still exit 0. The status exists only in all-trusted mode, where a named
  participant who has not pushed yet is an ordinary situation. A participant
  who has never pushed and is not named produces no request and no outcome.
- Memory streams are never discovered by this mode. Only exact `--memory`
  selectors import memory, and an exact memory selector that is not advertised
  still reports `dependency-missing`. Recovery guidance for a memory stream
  asks only for missing `--memory` selectors, because advertised actor chains
  and proposals are already requested.

### Storage, compatibility and shallow recovery

The selection file keeps `"version": 1` and adds `"allTrusted": true`. The
field is omitted when the mode is off, so selection files saved without
`--all-trusted` are byte-identical to files written by earlier versions.
`hn replication show` prints `all-trusted: true` directly after the
`compatibility-all:` line only when the mode is set; every other line is
unchanged.

An older `hn` binary rejects unknown fields when it reads a selection file. It
therefore fails closed on an all-trusted file: `hn sync` and
`hn replication show` stop with `json: unknown field "allTrusted"`, and
`hn sync` imports and publishes nothing until the selection is replaced.

`hn sync REMOTE --recover-shallow` still requires exact selectors. It recovers
only a supplier that the saved selection names exactly, so a pure all-trusted
selection is refused with the existing `is not in the saved exact selection`
diagnostic. To recover, save the exact supplier selectors next to
`--all-trusted` and retry.

### Linked worktrees share selections and acceptance state

The main checkout and every linked worktree created with `git worktree add`
share one object database and one set of accepted refs, so `hn` keeps the
state that selects and guards them in `hn/` below the Git common directory,
the directory `git rev-parse --git-common-dir` reports. That state is the
saved selections, the quarantine repositories, transaction receipts, pending
anchors, unaccepted-object records, and the shallow-gap record. A sync run in
any worktree uses the clone's saved selection and budgets, and a pending
acceptance left by an interrupted sync in one worktree blocks trust-sensitive
reads in every worktree until it is recovered from any of them.

Releases before this one kept this state in the private Git directory of the
worktree that ran `hn`, `.git/worktrees/NAME/hn/`, when that was a linked
worktree. The main checkout's state was already in the common directory.
`hn` applies these compatibility rules to state an older release left in a
linked worktree:

- A saved selection found in any linked worktree's private directory applies
  to the whole clone. When the clone-wide selection and the legacy copies for
  a remote are identical, or only one exists, that selection is used. When they
  differ, `hn replication show` and `hn sync` stop with
  `saved replication selections for remote REMOTE differ between PATH and PATH`
  instead of choosing one, because either choice could widen what another
  worktree imports. `hn replication select REMOTE ...` saves one clone-wide
  selection and removes the legacy copies for that remote.
- Receipts, pending anchors, and unaccepted-object records found in any linked
  worktree's private directory are read together with the clone-wide journal,
  so they keep guarding every worktree. The same transaction may appear in
  both places only with identical contents; anything else fails closed.
- The next `hn sync` from any worktree moves that journal into the clone-wide
  location before it reconciles, copying each file before removing the legacy
  original, and then completes it like any other pending transaction.
- A shallow-gap record left in a linked worktree is ignored.
  `hn sync REMOTE --recover-shallow` rediscovers the gap when no record exists.

### Example

<!-- example: all-trusted-select -->
```text
$ hn replication select origin --all-trusted
Saved replication selection for origin
remote: origin
saved: true
compatibility-all: false
all-trusted: true
max-events: 10000
max-objects: 100000
max-object-bytes: 67108864
max-attachment-bytes: 16777216
max-total-bytes: 1073741824
```

## Exact fetch and quarantine lifecycle

### Actor publication after import

After importing, `hn sync` publishes local actor refs with ordinary non-forced
Git pushes. A local copy can be older than the same actor's history on the
remote, particularly in older clones. If the exact actor history verified and
promoted during this sync already contains that local copy, publication skips
the redundant push. The local ref is retained and the accepted remote ref
records the newer verified history. Other local actors can still publish their
new events normally.

This check uses only successful actor selections from the current import, not
cached accepted refs or unvalidated advertisements. Missing remote actors are
still published; newer local histories still fast-forward. Divergent histories
still fail publication rather than being force-pushed or silently ignored.
Proposal and memory publication behavior is unchanged.

For each selected actor, Hubnot requests exactly:

```text
+refs/hn/actors/<full-actor>:refs/hn/quarantine/actors/<full-actor>
```

For each selected candidate, it requests exactly:

```text
+refs/hn/proposals/<candidate-hash>:refs/hn/quarantine/proposals/<candidate-hash>
```

For each selected memory stream, it requests exactly the one advertised owner:

```text
+refs/hn/memory/<full-actor>/<stream-hash>:refs/hn/quarantine/memory/<full-actor>/<stream-hash>
```

The destination refs live inside a generated separate bare repository, not the
main object database. The transaction then:

1. reads ordinary Git ref advertisement and resolves only selected refs;
2. exact-fetches each advertised selection into quarantine;
3. confirms the fetched root equals the advertised object ID;
4. measures the selected graph against every configured budget;
5. validates root/object types, exact event or two-file memory tree shape,
   signatures, actor fingerprints, sequence and parent chains, signed
   attachments, candidate ref/head bindings, identity relations, and all exact
   event, memory lifecycle, anchor, policy, pipeline, evidence, and merge
   relationships;
6. classifies valid, invalid, over-budget, and missing-dependency selections
   independently, then propagates failure only to selections that depend on a
   failed selection;
7. writes a private pending-acceptance anchor and validated transaction
   receipt before copying objects;
8. copies required promotable objects with ordinary Git pack plumbing and
   verifies their presence in the main object database;
9. removes quarantine, then updates all promotable accepted refs in one atomic
   `git update-ref` transaction;
10. releases only the verified shallow boundaries involved, records a complete
    receipt, and removes the pending anchor.

Accepted roots are:

```text
refs/hn/remotes/<remote>/actors/<full-actor>
refs/hn/remotes/<remote>/proposals/<candidate-hash>
refs/hn/remotes/<remote>/memory/<full-actor>/<stream-hash>
```

`collectEvents` projects only local actor refs and these accepted remote refs.
Objects copied before a failed accepted-ref transaction may remain as
unreferenced residue, but they are not accepted roots and trust-sensitive
operations remain fail-closed. Durable anchors/receipts prevent an interrupted
copy from being mistaken for acceptance; retry reconciles exact transaction
state.

### Transport settings for the quarantine fetch

Hubnot fetches each selection into a temporary bare repository under
`hn/replication/quarantine/` in the Git common directory, and that repository has no configuration of
its own. So that the fetch reaches the remote the way `git fetch` in the clone
does, Hubnot reads the clone's local and worktree configuration, includes
followed, and passes the transport settings to the fetch: `core.sshCommand`,
`core.gitProxy`, `core.askPass` and every `ssh.*`, `http.*`, `credential.*` and
`protocol.*` key. A clone that selects its SSH key with
`git config core.sshCommand "ssh -i KEY -o IdentitiesOnly=yes"` therefore
imports with that key. System and global configuration already apply to the
fetch.

The settings travel in `GIT_CONFIG_COUNT` environment entries, so a value such
as an `http.extraHeader` token never appears in a process argument list.
Settings already in the environment keep their precedence. `GIT_SSH_COMMAND`
overrides `core.sshCommand`, and `GIT_CONFIG_COUNT` entries or `git -c` values
that hn inherits override the clone's settings, as they do for any Git command
in the clone. Refs, hooks, remotes, `url.*` rewrites and every other key stay
out of the quarantine. `git remote get-url` has already applied `url.*` rewrites
to the URL that Hubnot fetches.

The environment entries need Git 2.31 or later, which added `GIT_CONFIG_COUNT`
([2.31.0 release notes](https://github.com/git/git/blob/master/Documentation/RelNotes/2.31.0.adoc)),
and reading the configuration by scope needs Git 2.26
([2.26.0 release notes](https://github.com/git/git/blob/master/Documentation/RelNotes/2.26.0.adoc)).
With an older Git the quarantine fetch sees only system, global and
environment settings, so export `GIT_SSH_COMMAND` to choose a key.

## Budget semantics

Budgets are evaluated per selected ref. Objects shared by two selections count
independently for both. Only objects reachable from that same selection's
previous accepted ref are discounted from object and byte measurements;
unrelated objects already present locally cannot weaken a later check.

| Budget | Measurement |
| --- | --- |
| `max-events` | Number of commits/events in the selected actor or memory history |
| `max-objects` | Newly reachable Git objects beyond that selection's previous accepted value |
| `max-object-bytes` | Largest individual newly reachable Git object |
| `max-attachment-bytes` | Largest attachment in the selected actor history (`log.txt` today) |
| `max-total-bytes` | Sum of newly reachable Git object sizes |

All comparisons are inclusive: if a measured value is `N`, a configured limit
of `N-1` rejects, `N` accepts, and `N+1` accepts. A rejected selection reports
the full selected ID, budget name, configured value, and measured value, and
its accepted ref does not advance.

Standard Git may receive the selected pack before Hubnot can measure its
contents. These are hard validation, promotion, accepted-projection, and
retention boundaries. They are not portable hard pre-download network, CPU,
memory, or disk quotas.

## Per-selection failure isolation

`hn sync` reports one outcome per full selected actor/candidate/stream ID. A
memory outcome has `kind=memory` and the full stream ID:

- `promoted` — selected data validated and its accepted ref committed;
- `over-budget` — at least one inclusive limit was exceeded;
- `structurally-invalid` — fetch/root/object/event-chain validation failed;
- `relationship-invalid` — an available referenced fact contradicts the
  signed relationship;
- `dependency-missing` — an exact required actor, event, candidate, code ref,
  or evidence fact was not selected/advertised.

A valid selection promotes independently only when its relationship closure
does not depend on a failed selection. A failed actor selection therefore
blocks a selected candidate or evidence chain that needs it, but not an
unrelated valid history.

Missing dependency is not treated as malformed data. Diagnostics include the
failing selection's full ID, referencing event's full ID and kind, missing full
ID when derivable, and an exact next action such as:

```text
add exact proposal selection --proposal <full-sha256-candidate-id>
select the full actor history supplying <full-sha256-event-id>
```

No similarly titled candidate, related device, predecessor/successor, or short
prefix is substituted.

## Publication is separate from import

After import, `hn sync [REMOTE]` uses ordinary explicit Git pushes to publish
all local `refs/hn/actors/*` histories, locally present
`refs/hn/proposals/*` candidate refs, and local `refs/hn/memory/*/*` streams.
Remote histories accepted under `refs/hn/remotes/*` are not republished as
local facts. The command does not push a primary branch:

```sh
hn sync origin
git push origin main:main
```

These are separate observable and retryable actions. Use exact ref targets for
manual staged publication; do not wildcard-push or force-rewrite immutable
collaboration facts.

## Depth-limited repositories

A shallow repository is allowed when every exact dependency required by an
operation already exists. Trust-sensitive operations explicitly probe their
candidate event, code ref/head, base commit, policy blob, pipeline definition,
run request/result, decision, merge ancestry, selected evidence, and actor
predecessors.

If an exact object/fact is absent beyond the shallow boundary, the command
fails with a durable diagnostic containing:

- original operation;
- dependency kind and full missing ID;
- owning full actor/candidate ID when derivable;
- remote and exact supplying ref;
- the exact selection/recovery action.

Recovery is explicit:

```sh
hn replication select origin \
  --actor <full-supplying-actor> \
  --proposal <full-supplying-candidate> \
  --memory <full-supplying-stream> \
  --max-events 10000 \
  --max-objects 100000 \
  --max-object-bytes 16777216 \
  --max-attachment-bytes 1048576 \
  --max-total-bytes 268435456
hn sync origin --recover-shallow
```

Recovery requires an explicitly saved exact selection; compatibility-all is
refused. It builds the smallest subset containing the recorded supplier, runs
that subset through the same quarantine/budgets/validation/atomic promotion,
checks that the saved selection bytes did not change, and replays the complete
original verification before clearing the gap. It never issues `--unshallow`,
never fetches all branch history, never silently adds a selector, and never
reuses a partial trust decision.

Calling `--recover-shallow` in a complete repository is an error. Retrying
after successful recovery is an idempotent accepted-projection verification.

## Private transaction state and threat boundary

Selections, generated quarantine repositories, transaction receipts, pending
anchors, unaccepted-object records, and shallow-gap records live below
`hn/replication/` or `hn/` in the clone's Git common directory (`.git/hn/` in
an ordinary clone), shared by every linked worktree. Directories require mode `0700`; state
files require mode `0600`; symlinks, unexpected entries, unknown JSON fields,
oversized records, missing matching anchors/receipts, and unsafe modes fail
closed.

The alpha does not claim that selection implies trust, that quotas stop bytes
before network receipt, that rejected unreachable residue is securely erased,
or that a remote cannot advertise hostile data. Its guarantee is narrower:
selected data is not an accepted projection root until bounded validation and
atomic promotion succeed. Memory recall consumes only local and promoted
accepted roots, never quarantine.

All-trusted selection lets the remote decide which actor chains and proposals
are requested. A malicious or misconfigured server can therefore make a client
fetch more refs, each bounded by the saved per-ref budgets, and it can withhold
refs so that facts arrive late or never. It cannot make the client accept an
invalid fact, because every requested ref passes the same validation before
promotion.
