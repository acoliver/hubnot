---
affected_files: []
cycle_number: 2
mission_slug: hn-serve-01M5P0NP
reproduction_command:
reviewed_at: '2026-10-25T19:08:31Z'
reviewer_agent: user
wp_id: WP12
---

# WP12 maintainer review: changes required (found by the hands-on Phase 0 run)

## Blocking issue: hn sync ignores the clone's own Git transport configuration

**Reproduction.** `/home/acoliver/projects/appaday/gitium/.setup/dogfood-serve.sh` runs the real rust-pingpong
project behind a real unprivileged OpenSSH daemon (log:
`/home/acoliver/projects/appaday/gitium/logs/dogfood-serve.log`). Each participant clone selects its own SSH key
the usual way for several identities on one Unix account:
`git config core.sshCommand "ssh -i KEY -o IdentitiesOnly=yes ..."`.
`git fetch`, `git push` and `git ls-remote` in that clone all work, and
`hn proposal open` plus `hn sync origin` publish facts. But every import fails:

```text
Replication actor d09a68ae…: failed (structurally-invalid): selection actor d09a68ae… failed exact fetch from remote origin
hn: replication import phase failed for one or more exact selections
```

A direct `git fetch origin refs/hn/actors/d09a68ae…:refs/x` in the same clone
succeeds. With `GIT_SSH_COMMAND` exported from the clone's `core.sshCommand`
the whole flow passes end to end (CI, review, accept, `hn merge --remote`, merge
fact). Log: `/home/acoliver/projects/appaday/gitium/logs/dogfood-serve-workaround.log`.

**Cause.** `quarantine.go` fetches each selection with
`gitOutputAt(quarantineDir, "fetch", ..., remoteURL, ...)`, which runs Git with
`--git-dir=<quarantine>`. That temporary bare repository has none of the
clone's configuration, so `core.sshCommand` (and any other transport settings
such as `ssh.variant`, `http.*`, `credential.*`, `protocol.*`) are lost. The
mission's tests did not catch this because the harness passes SSH settings
through `GIT_SSH_COMMAND` in the environment.

**Required fix.**
1. Make the quarantine fetch use the clone's transport configuration while
   still writing objects only into the quarantine. Choose the smallest safe
   mechanism (for example passing the clone's transport-relevant keys with
   `git -c KEY=VALUE`, read with `git config --get-regexp` from the clone, or an
   equivalent) and keep the existing environment behaviour. Do not import refs,
   hooks or unrelated configuration into the quarantine.
2. Apply the same rule to every other place where hn runs Git against a remote
   from a directory other than the clone (check `serve_client.go`, replication
   and any `gitOutputAt` call that takes a remote URL).
3. Add a regression test through the public CLI that configures SSH only with
   the clone's `core.sshCommand` (no `GIT_SSH_COMMAND` in the environment) and
   proves `hn sync` imports another participant's actor and proposal, and that
   `hn merge --remote` advances the branch.
4. Note the behaviour in `docs/serve-v0.md` (Runners and verifiers, or the
   client section) and in `docs/replication-v0.md`: a clone's `core.sshCommand`
   applies to replication fetches.

Maintainer authorization: this WP may edit `quarantine.go`, `replication.go`,
`git.go` and `docs/replication-v0.md` for this fix (they belong to WP04/WP11 in
lane-c, which is already merged into lane-a). Keep every pre-existing test
assertion unchanged.
