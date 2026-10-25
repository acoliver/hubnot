# Decision Moment `01M5R5E9B0RZHT3WNTHTRPW4M2`

- **Mission:** `hn-serve-01M5P0NP`
- **Origin flow:** `plan`
- **Slot key:** `plan.lfs.transfer`
- **Input key:** `lfs_transfer_design`
- **Status:** `resolved`
- **Created:** `2026-10-24T23:55:21.824371+00:00`
- **Resolved:** `2026-10-24T23:55:23.275992+00:00`
- **Resolved by:** `llxprt (maintainer-delegated planning)`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

Which LFS transfer path and storage layout does hn serve implement (FR-020, FR-021)?

## Options

- git-lfs-transfer pure SSH protocol only, objects under the state directory per repository, staged uploads verified then renamed under a per-repository LFS lock
- git-lfs-authenticate plus an HTTP server
- Other

## Final answer

Implement the git-lfs-transfer SSH protocol (version 1: batch, put-object, verify-object, get-object; every lock command answers unsupported) with the standard library. git-lfs-authenticate is refused as unsupported-command. Objects live at state/repos/NAME/lfs/objects/OID[0:2]/OID[2:4]/OID; uploads stream into state/repos/NAME/lfs/incoming, are checked for size and SHA-256, and are renamed into place only while holding the repository LFS lock, after a quota recheck. The quota counts verified stored bytes only.

## Rationale

hn serve does not run an HTTP listener (C-002, HN-001), and git-lfs 3.x prefers git-lfs-transfer over SSH (3.7.1 is installed on the test host). Keeping LFS state outside the bare repository keeps git gc and the push check independent of LFS. Verify-then-rename under a lock ensures interrupted or mismatched uploads are never downloadable and never counted (FR-020, FR-021, the interrupted-upload edge case).

## Change log

- `2026-10-24T23:55:21.824371+00:00` — opened
- `2026-10-24T23:55:23.275992+00:00` — resolved (final_answer="Implement the git-lfs-transfer SSH protocol (version 1: batch, put-object, verify-object, get-object; every lock command answers unsupported) with the standard library. git-lfs-authenticate is refused as unsupported-command. Objects live at state/repos/NAME/lfs/objects/OID[0:2]/OID[2:4]/OID; uploads stream into state/repos/NAME/lfs/incoming, are checked for size and SHA-256, and are renamed into place only while holding the repository LFS lock, after a quota recheck. The quota counts verified stored bytes only.")
