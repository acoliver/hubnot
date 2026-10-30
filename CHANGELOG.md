# Changelog

User-visible changes and release requirements for Hubnot. The protocol remains
experimental; alpha releases do not promise stable compatibility.

## Unreleased

### Added

- `hn runner once` and `hn runner watch` accept `--accept-actor` up to 64
  times, so one runner can serve the maintainers and the requesters that a
  pipeline's `requesters` policy lists. Duplicate and malformed fingerprints
  are refused before synchronization. With more than one accepted actor the
  runner executes only requests whose signer counts under the proposal's base
  policy. A single `--accept-actor` behaves as before, because the existing
  distributed-runner contract executes a single accepted actor's requests and
  readiness already ignores results whose request signer does not count; see
  `docs/ci-v0.md`.

### Fixed

- Linked Git worktrees of a clone share its identity. The private keyring now
  lives in the Git common directory, so `hn init` in any worktree serves the
  main checkout and every other worktree. A worktree that already holds a
  keyring in its own private Git directory keeps it, and an unreadable
  worktree keyring is an error. Concurrent `hn init` commands activate exactly
  one identity, including on file systems without hard links, and an append
  that loses a race for the actor ref reports a retryable concurrent update.
  Commands that change the keyring hold an `flock(2)` on `hn/keyring.lock`;
  `hn identity rotate` holds it for the whole rotation, so a second
  simultaneous rotation fails without writing and an interrupted rotation
  leaves one journal that the next `hn identity rotate` completes. See
  `docs/identity-v0.md`.
- Replication selections and acceptance journals are clone-wide. They live in
  `hn/replication/` below the Git common directory, so `hn sync` from any
  linked worktree uses the clone's saved selection and budgets, as the main
  checkout does, and a pending acceptance left
  by an interrupted sync blocks trust-sensitive reads in every worktree. State
  that earlier releases wrote in a linked worktree's private Git directory
  still applies: a legacy selection governs the whole clone, differing
  selections are an error until `hn replication select` saves one, and legacy
  journals guard every worktree until the next `hn sync` moves and recovers
  them. Readers hold `hn/state.lock` shared across the clone-wide and legacy
  locations, and commands that move, publish, save, or remove that state
  hold it exclusively, so a concurrent reader never misses a pending
  acceptance or a saved restriction while it moves. The memory index and the
  audit authority anchor stay per worktree. A sync interrupted after
  promotion and before its completion receipt is still recoverable only with
  `hn sync REMOTE --recover-shallow`, which needs a shallow clone; in a full
  clone it stays pending, as it did before. See `docs/replication-v0.md`.
- Migration sessions and internal project/archive/imported-record spools share
  a path policy that handles verified macOS system temporary-directory aliases
  while retaining rejection of arbitrary symlinks and unsafe private paths.
  macOS regressions exercise literal aliases and child environments without
  TMPDIR. Published `0.0.2-dev` packages retain the limitation in [#16](https://github.com/LynnColeArt/hubnot/issues/16).
- Packager refusal fixtures build hostile committed trees directly with Git
  plumbing, including file and directory case collisions, so the same assertions
  exercise real inputs on case-insensitive filesystems.

### Development

- Linux/macOS PR checks run the repository-local filesystem, migration and
  packager regression gates with normal temporary-directory environments.
  These optional hosted checks add no runtime or provider dependency to Hubnot.

## [0.0.2-dev](https://github.com/LynnColeArt/hubnot/releases/tag/v0.0.2-dev) — 2026-10-27

Portable-collaboration alpha prerelease, built from
`e80880abc792e2c386182e06027fc251b134c0d2`. The executable reports `0.0.2-dev`.
**Mac runtime testing is still pending for both Intel and Apple Silicon.**

### Removed

- Obsolete tracked `.nh/policy.json` and `.nh/pipelines/test.json` from the retired
  `nh` namespace. The root `.nh/` directory is now ignored. Active `.hn/`
  configuration is unchanged; earlier files remain available in Git history.

### Fixed

- Test repository fixtures now resolve physical paths consistently with Git when
  temporary directories use aliases, including macOS `/var` and `/private/var`.
  This correction changes tests only; existing `0.0.1-dev` assets remain
  unchanged. ([#10](https://github.com/LynnColeArt/hubnot/pull/10))

### Added

- Portable project snapshots and bounded archives retain explicitly selected Git
  history, exact signed native facts, attributed foreign communications and
  retrieved attachment bytes. Saved guided/scripted `hn migrate` plans preview
  capabilities and explicit loss decisions before guarded apply/resume. Imported
  history does not install signers, activate policy or grant runner authority.
- Tested migration profiles for GitHub, GitLab including self-hosted endpoints,
  separate Forgejo and Gitea versions, Bitbucket Cloud source preservation, and
  explicitly mapped Azure DevOps Boards work items/comments. Preservation does
  not imply destination recreation; unavailable reads and removed APIs are
  reported. Legacy explicit transfer commands retain their behavior.
- `noforge` excludes all network adapters while retaining native collaboration,
  local/archive migration, foreign inspection, and independent audits.
  `nogithub` continues to exclude GitHub only.
- Independent signed audit authorizations, revocations, requests and results,
  with separate explicit local authority adoption and existing sandbox/unsafe-host
  execution boundaries. Audit outcomes never supply or block merge evidence.
  Deliberate linked follow-up issues deduplicate retries and later findings,
  preserve operator edits and expose concurrent publisher limits. No scheduler
  or automatic execution is introduced; native policy defaults remain unchanged.
- Go/Git-only `hn-release` packages an exact committed revision into deterministic
  Linux/macOS binary and source archives with normalized metadata, SHA-256
  checksums and declared provenance. Native-only packages omit all adapters.
  The packager cross-compiles for Windows/macOS; their packager-host runtime
  qualification remains outstanding. Packaging never publishes or changes tags.
  The immutable `0.0.1-dev` artifacts retain their original Python reproduction
  requirements and GitHub-only native-only meaning.

### Downloads

| Platform | Standard | Native-only |
| --- | --- | --- |
| Linux AMD64 | [Download](https://github.com/LynnColeArt/hubnot/releases/download/v0.0.2-dev/hubnot_0.0.2-dev_linux_amd64.tar.gz) | [Download](https://github.com/LynnColeArt/hubnot/releases/download/v0.0.2-dev/hubnot_0.0.2-dev_linux_amd64_native-only.tar.gz) |
| Linux ARM64 | [Download](https://github.com/LynnColeArt/hubnot/releases/download/v0.0.2-dev/hubnot_0.0.2-dev_linux_arm64.tar.gz) | [Download](https://github.com/LynnColeArt/hubnot/releases/download/v0.0.2-dev/hubnot_0.0.2-dev_linux_arm64_native-only.tar.gz) |
| macOS Intel | [Download](https://github.com/LynnColeArt/hubnot/releases/download/v0.0.2-dev/hubnot_0.0.2-dev_darwin_amd64.tar.gz) | [Download](https://github.com/LynnColeArt/hubnot/releases/download/v0.0.2-dev/hubnot_0.0.2-dev_darwin_amd64_native-only.tar.gz) |
| macOS Apple Silicon | [Download](https://github.com/LynnColeArt/hubnot/releases/download/v0.0.2-dev/hubnot_0.0.2-dev_darwin_arm64.tar.gz) | [Download](https://github.com/LynnColeArt/hubnot/releases/download/v0.0.2-dev/hubnot_0.0.2-dev_darwin_arm64_native-only.tar.gz) |

[Source archive](https://github.com/LynnColeArt/hubnot/releases/download/v0.0.2-dev/hubnot_0.0.2-dev_source.tar.gz),
[SHA256SUMS](https://github.com/LynnColeArt/hubnot/releases/download/v0.0.2-dev/SHA256SUMS)
and [build provenance](https://github.com/LynnColeArt/hubnot/releases/download/v0.0.2-dev/PROVENANCE.json)
accompany the binary packages. Native-only excludes every network provider
adapter while retaining native commands and local/archive migration.

### Verification and distribution limits

- The release source passed the full race suite, independent mission review,
  and final pre-merge vet/build/targeted race checks.
- All eleven assets reproduced byte-for-byte with Go 1.26.4. Independent
  inspection verified exact source files, archive contents and metadata,
  executable architectures, static Linux linking, checksums and provenance.
  All eleven published downloads match the verified local files.
- Both Linux AMD64 and ARM64 standard/native-only binaries passed runtime smoke
  checks with Git-only application PATH and no provider credentials, including
  signed native issues, saved archive migration, exact native actor history
  import and local continuation. No new full ARM64 Go-suite run is claimed.
- Both macOS architectures were cross-compiled and inspected. **Mac runtime
  testing is still pending for 0.0.2-dev**; previous Apple Silicon test results
  apply only to the earlier release. The packages have no Apple Developer ID
  signature or notarization.
- Running prebuilt binaries requires Git 2.x. Source builds and the new release
  tooling require Go and Git alone; Python is not required. macOS/Windows
  packager-host execution remains unverified, and Windows CLI distribution
  remains outside this release matrix.
- Protected checks, automerge and independent audits remain optional and
  disabled by default. No scheduler, service or background worker is installed.

## [0.0.1-dev](https://github.com/LynnColeArt/hubnot/releases/tag/v0.0.1-dev) — 2026-10-25

First packaged operational-alpha prerelease, built from
`f82b57420f0c825a9ea6f3b9f93ede680c8cf06f`. The executable reports `0.0.1-dev`.

### Added

- Git-native signed issues, projects and typed planning items, threaded
  discussions with pinned answers, and versioned workflows.
- Immutable proposals and revisions, independent reviews, CI evidence,
  policy-governed decisions and local merges, distinct device identities,
  selected quarantined replication, and deliberate signed agent memory.
- Local `hn gh issue` compatibility commands and explicit provider-neutral
  import/export with GitHub as the first optional adapter. Native operation
  requires no provider account or service.
- Independent, opt-in protected checks and rule-based automerge. Protected check
  changes require designated independent approvals; current protected baselines
  prevent hidden lint reversions. Automerge uses an explicit named rule and
  current native evidence, with a read-only preview and separate `--execute`.
  Both capabilities are disabled by default and have no GitHub dependency.
- MIT licensing and downloadable Linux AMD64/ARM64 and macOS Intel/Apple Silicon
  archives, each in standard and native-only variants. Native-only builds omit
  the GitHub adapter while retaining native commands and local compatibility.
- Source archive, product documentation, SHA-256 checksums, build provenance,
  and a reproducible packaging script. Linux binaries are statically linked;
  macOS binaries target macOS 12 or newer.

### Verification

- The release source passed the full race suite, full `nogithub` suite, vet,
  public acceptance matrix, and independent review before merge.
- All eight binary archives passed format, permissions, and provenance checks
  and were reproduced byte-for-byte with Go 1.26.4.
- Both Linux AMD64 packages passed CLI smoke checks. Both Linux ARM64 packages
  passed native resource and policy acceptance matrices on ARM64 Ubuntu 24.04;
  the standard package also passed rich-issue lifecycle and consumer, replication,
  agent-memory, and self-hosting scenarios. An initial consumer setup failure
  was resolved with a source-matched prebuilt fixture and the affected scenario
  passed on rerun; both attempts are included in the release evidence.
- Both macOS Apple Silicon packages passed startup, identity, signed-issue
  creation, and issue-list smoke checks on macOS. Intel Mac packages were
  cross-compiled and inspected; runtime testing remains outstanding.
- Public Linux downloads were checked against the tested archives and published
  SHA-256 checksums. Detailed logs and verification records accompany the release.

### Distribution limits

- Running the packaged executable requires Git 2.x. Python and Go are not
  runtime dependencies. Reproducing this release with its packaging script
  requires Python 3.12+, Git, and Go 1.26.4; removing that packaging dependency
  is required for the next release.
- macOS executables have no Apple Developer ID signature or notarization.
  Download approval may be needed in Privacy & Security. The default safe CI
  executor uses Linux bubblewrap; macOS has no bundled safe CI backend.
- No Windows executable packages are included in this release.
