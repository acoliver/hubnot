# Decision Moment `01M5P0QBVR1SJJQ14CS8R07RCJ`

- **Mission:** `hn-serve-01M5P0NP`
- **Origin flow:** `specify`
- **Slot key:** `specify.lfs.quota-locking`
- **Input key:** `lfs_quota_locking`
- **Status:** `resolved`
- **Created:** `2026-10-24T03:54:27.576473+00:00`
- **Resolved:** `2026-10-24T03:55:52.333979+00:00`
- **Resolved by:** `llxprt`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

What is the default per-repository LFS quota, what does it count, and is LFS file locking in scope?

## Options

- Default 1 GiB per repository, counting stored verified object bytes; an upload that would exceed it is refused before it becomes visible; locking operations answer unsupported
- No default quota (unlimited until configured)
- Include LFS locking
- Other

## Final answer

OQ-8: Default 1 GiB LFS quota per repository, configurable with hn serve; counts stored verified object bytes; an upload that would exceed it is refused before it becomes visible; LFS locking operations answer unsupported.

## Rationale

Agent-selected default under the maintainer's delegated non-interactive instruction, chosen for consistency with the .kittify charter (Go, standard library first, optional service, hostile input, public-CLI tests). Recorded in spec.md as an open question for maintainer confirmation at spec review.

## Change log

- `2026-10-24T03:54:27.576473+00:00` — opened
- `2026-10-24T03:55:52.333979+00:00` — resolved (final_answer="OQ-8: Default 1 GiB LFS quota per repository, configurable with hn serve; counts stored verified object bytes; an upload that would exceed it is refused before it becomes visible; LFS locking operations answer unsupported.")
