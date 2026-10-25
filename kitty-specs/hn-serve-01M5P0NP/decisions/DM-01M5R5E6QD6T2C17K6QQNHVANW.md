# Decision Moment `01M5R5E6QD6T2C17K6QQNHVANW`

- **Mission:** `hn-serve-01M5P0NP`
- **Origin flow:** `plan`
- **Slot key:** `plan.admin.seeding`
- **Input key:** `repository_seeding`
- **Status:** `resolved`
- **Created:** `2026-10-24T23:55:19.149605+00:00`
- **Resolved:** `2026-10-24T23:55:20.474307+00:00`
- **Resolved by:** `llxprt (maintainer-delegated planning)`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

What does hn serve repo create import, what must hold at creation, and how does an administrator recover a protected branch with no valid policy (OQ-11)?

## Options

- Import refs/heads/* and refs/tags/* only, require a valid policy at every protected tip, set HEAD to the default protected branch, document manual recovery
- Mirror every ref including refs/hn/*
- Other

## Final answer

hn serve repo create NAME --from SOURCE fetches only refs/heads/* and refs/tags/* into a new bare repository, writes refs/hn/serve/v1, sets HEAD to refs/heads/main (or the first --protect value), and refuses creation when any protected branch is absent or lacks a valid .hn/policy.json at its tip. Imported branches have no owner. Hubnot facts arrive afterwards through governed pushes such as hn sync. Recovery of a protected tip with invalid policy is a documented administrator procedure: run git update-ref as the server user while holding the repository lock through hn serve repo lock-exec NAME -- COMMAND.

## Rationale

Mirroring refs/hn/* would host facts that never passed FR-007 or FR-010, which breaks the FR-017 claim that every advertised fact was validated on arrival. A protected branch without valid policy can never move through the governed path, so creating one only produces a dead repository; refusing at creation is fail-closed and gives a clear message. lock-exec gives administrators the repository lock without adding a governed-path bypass.

## Change log

- `2026-10-24T23:55:19.149605+00:00` — opened
- `2026-10-24T23:55:20.474307+00:00` — resolved (final_answer="hn serve repo create NAME --from SOURCE fetches only refs/heads/* and refs/tags/* into a new bare repository, writes refs/hn/serve/v1, sets HEAD to refs/heads/main (or the first --protect value), and refuses creation when any protected branch is absent or lacks a valid .hn/policy.json at its tip. Imported branches have no owner. Hubnot facts arrive afterwards through governed pushes such as hn sync. Recovery of a protected tip with invalid policy is a documented administrator procedure: run git update-ref as the server user while holding the repository lock through hn serve repo lock-exec NAME -- COMMAND.")
