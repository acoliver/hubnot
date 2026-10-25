# Contract: Git LFS transfer and quota

Hosted repositories serve Git LFS objects through the `git-lfs-transfer` SSH protocol, version 1, as described in the Git LFS [SSH adapter proposal](https://github.com/git-lfs/git-lfs/blob/main/docs/proposals/ssh_adapter.md). The implementation uses only the Go standard library (C-003). This concern is separable and may move to its own mission (C-010).

## Session

The client runs `git-lfs-transfer PATH upload|download` through the forced command. The wrapper resolves `PATH` with the same grammar and hosting checks as Git requests, then speaks pkt-line on stdin and stdout. A pkt-line length header may not exceed 65,519 decimal, the limit the proposal states, and the server rejects longer headers before allocating. A malformed pkt-line, an unknown command, or a command not allowed for the session's operation ends the session with status 400 and exit status 1.

| Step | Server sends |
|----|----|
| Capability advertisement | `version=1`, then flush. `locking` is never advertised |
| `version 1` | `status 200`, then flush. Any other version gets `status 400` |
| `batch` | One line per requested object, see below |
| `put-object OID` (upload only) | `status 200` after the object is stored, or an error status |
| `verify-object OID` (upload only) | `status 200` when the stored object exists with the given size, else `status 404` |
| `get-object OID` (download only) | `status 200`, a `size=N` argument, delim, the object bytes in pkt-lines, flush; or `status 404` |
| `lock`, `unlock`, `list-lock` | `status 501` with message `locking is unsupported by hn serve` (FR-021) |
| `quit` | `status 200`, flush, then exit 0 |

`batch` arguments other than `transfer`, `refname` and `hash-algo` are ignored, as the proposal requires. `hash-algo` must be absent or `sha256`; otherwise the batch answers `status 400`. Each `oid size` line must carry a 64-character lowercase hex OID and a decimal size. For upload, a stored object gets the action `noop` and a missing one gets `upload`. For download, every requested object gets `download`, and `get-object` answers `status 404` for an object that is not stored, so the client reports the missing object by OID. The server sets no `id` or `token` values. The proposal does not define per-object batch errors, so the tests in `serve_lfs_test.go` pin the observed Git LFS 3.7.1 client behavior for the missing-object and quota cases.

## Storage

Objects are stored at `state/repos/NAME/lfs/objects/OID[0:2]/OID[2:4]/OID` with mode 0600. An upload streams into a new file in `state/repos/NAME/lfs/incoming/` with mode 0600. After the last byte, the server checks that the byte count equals the declared `size` and that the SHA-256 of the content equals the OID. A mismatch removes the staged file and answers `status 422` with message `hn-serve/1 reject ref=- actor=ACTOR code=lfs-object-mismatch ids=OID next="retry the upload"`.

## Quota

The quota is `lfsQuotaBytes` from the repository configuration, default 1 GiB (OQ-8). Stored bytes are the sum of sizes of files under `lfs/objects/`. Staged files never count.

| Check point | Rule |
|----|----|
| `batch` for upload | When stored bytes plus the declared sizes of every requested object that is not stored exceed the quota, the whole batch answers `status 507` with a `code=lfs-quota` message line |
| `put-object`, before reading data | Stored bytes plus the declared size above the quota: `status 507` with a `code=lfs-quota` line, and the data is drained and discarded |
| `put-object`, after verification | Under `lfs.lock`, recompute stored bytes; if the object would exceed the quota, remove the staged file and answer `status 507`; otherwise rename it into `lfs/objects/` |

The limit is inclusive: with quota `N` and stored bytes `N - 10`, a 10-byte object is accepted and an 11-byte object is refused (US5-3). Rejection lines carry `budget=lfs-quota limit=N measured=M`, where `M` is the stored bytes the upload would produce.

## Interruption

A connection lost during `put-object` leaves only a staged file, which is never downloadable and never counted. At the start of each LFS session the server removes staged files older than one hour for that repository.
