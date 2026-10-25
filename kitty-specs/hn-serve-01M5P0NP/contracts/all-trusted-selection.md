# Contract: server identification and all-trusted selection

Runners and verifiers need to import every new proposal and run request without naming each actor and candidate (US4). An `hn serve` remote can offer that safely because it accepts only actor chains and proposal refs that passed FR-007 and FR-010 on arrival. The client still validates everything it imports, so the server's checks reduce noise and never replace client verification (C-006, NFR-006).

## Server identification

An `hn serve` hosted repository advertises `refs/hn/serve/v1`, pointing at the blob `hn-serve/1\n`. A remote identifies as `hn serve` when `git ls-remote --refs REMOTE refs/hn/serve/v1` returns exactly one line whose object ID equals the output of `git hash-object --stdin` for those 11 bytes in the client repository. Any other result, including a transport error, means the remote does not identify as `hn serve`.

## Selecting

```text
hn replication select [REMOTE] --all-trusted [--actor ACTOR]... [--proposal ID]... [--memory STREAM]... [budgets]
```

| Rule | Result on violation |
|----|----|
| `--all-trusted` with `--all` | `--all-trusted is mutually exclusive with --all` |
| Remote does not identify as `hn serve` at select time | `remote REMOTE does not identify as hn serve; --all-trusted requires an hn serve remote` |
| Exact selectors present | Allowed; validated exactly as today |
| Budgets | Same flags, defaults and validation as today |

The selection file keeps `"version": 1` and adds `"allTrusted": true`. The field is omitted when false, so selection files written without the flag are byte-identical to today's files.

`hn replication show` prints `all-trusted: true` after the `compatibility-all:` line only when the mode is set. All other lines are unchanged.

## Syncing

`hn sync REMOTE` with an all-trusted selection behaves as follows.

1. Identification is checked again. If it fails, the import phase stops with `remote REMOTE no longer identifies as hn serve; reselect with hn replication select`, nothing is fetched, and the command exits 1 after the publication phase runs as usual.
2. Requests are every advertised `refs/hn/actors/<64-hex>` and `refs/hn/proposals/<64-hex>` ref, plus each exact selector. Memory refs are requested only through exact `--memory` selectors (OQ-3).
3. An exact actor or proposal selector that is not advertised gets status `not-advertised` with no diagnostic. It is not a failure, promotes nothing, and is printed as `Replication actor ID: not advertised yet`. Exact memory selectors keep their current `dependency-missing` behavior.
4. Every fetched ref goes through the existing quarantine transaction: per-ref budgets from the selection, structural validation, relationship and dependency validation, identity continuity, and atomic promotion into `refs/hn/remotes/REMOTE/...`. Nothing in this path changes.
5. `hasFailures` ignores `not-advertised` outcomes. All other statuses keep their meaning.

An actor registered on the server who has never pushed has no advertised ref, so it produces no request and no outcome (US4-2).

## Unchanged modes

Exact selections, `--all`, and the no-selection compatibility mode do not check identification and produce the same outcomes against an `hn serve` remote as against an ordinary remote holding the same refs (FR-019). The `--all` mode still requests advertised memory refs, as it does today. The marker ref is outside the patterns `resolveReplicationRequests` requests, so it is never imported.

## Example

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
