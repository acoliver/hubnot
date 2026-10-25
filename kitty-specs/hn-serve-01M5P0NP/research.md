# Research: hn serve, a governed local Git server

This file records the Phase 0 findings behind [plan.md](plan.md). The spec's eleven open questions were confirmed by the maintainer before planning, so the research below covers what the spec leaves to the planner: where the push check runs, how existing validation is reused, and the wire shapes that clients and the server share. Every decision has a resolved plan-flow Decision Moment in `kitty-specs/hn-serve-01M5P0NP/decisions/`.

## Method

The planner read the existing implementation (`main.go`, `commands.go`, `git.go`, `event.go`, `store.go`, `policy.go`, `governance.go`, `replication.go`, `quarantine.go`, `shallow.go`, `lineage.go`, `memory_store.go`, `identity_keyring.go`, `issue_read.go`) and the test helpers in `operational_acceptance_test.go`. Three throwaway spikes ran in `/tmp/hn-plan-spike-*` directories that were removed afterwards, using the gate host's Git 2.53.0 and Git LFS 3.7.1. Protocol details come from the primary documentation cited inline.

## Spike results

| Spike | Observation | Consequence |
|----|----|----|
| `pre-receive` hook supplied through `git -c core.hooksPath=... receive-pack` | The hook saw `GIT_QUARANTINE_PATH`, could read the pushed commit with `git cat-file`, received `GIT_PUSH_OPTION_COUNT=1` with `GIT_PUSH_OPTION_0=hn-advance=...`, and inherited an environment variable set by the caller. After the hook exited 1, Git reported `[remote rejected] (pre-receive hook declined)` and the bare repository held only the one marker blob created before the push | The all-or-nothing verdict, push options and wrapper-set actor variables work as planned |
| Blob ref `refs/hn/serve/v1` in a bare repository | `git ls-remote` listed it under protocol v0 and v2 | Server identification can be a plain advertised ref |
| `GIT_SSH_COMMAND` shim with `GIT_SSH_VARIANT=ssh` | Push invoked the shim as `hn-serve "git-receive-pack '/r.git'"` with no `GIT_PROTOCOL`. Fetch invoked it as `-o SendEnv=GIT_PROTOCOL hn-serve "git-upload-pack 'r.git'"` with `GIT_PROTOCOL=version=2` | The command grammar must accept an optional leading slash; the shim must skip `-o` options and forward `GIT_PROTOCOL` |

Git documents the quarantine behavior: objects received by `receive-pack` stay in a temporary directory until `pre-receive` succeeds and are removed when it fails ([githooks, pre-receive](https://git-scm.com/docs/githooks#pre-receive)). The same page documents `GIT_PUSH_OPTION_COUNT` and `GIT_PUSH_OPTION_<n>`, which require `receive.advertisePushOptions` ([git-config, receive.advertisePushOptions](https://git-scm.com/docs/git-config#Documentation/git-config.txt-receiveadvertisePushOptions)). Push options shipped in Git 2.10, whose release notes say `git push` "learned to accept and pass extra options to the receiving end so that hooks can read and react to them" ([2.10.0 release notes](https://github.com/git/git/blob/master/Documentation/RelNotes/2.10.0.adoc)). Object quarantine shipped in Git 2.11, whose release notes say received objects are stored "in a temporary area" and then either migrated or purged once the hook decides ([2.11.0 release notes](https://github.com/git/git/blob/master/Documentation/RelNotes/2.11.0.adoc)). The serve guide therefore states Git 2.11 as the minimum server version.

## Decisions

### Push enforcement point and serialization

Decision `01M5R5B9HEKFJ6T52Y2W9AKWW0`.

- **Decision**: The wrapper takes an exclusive `flock` on `state/repos/NAME/lock` before it starts `git receive-pack` and holds it until `receive-pack` exits. `pre-receive` computes the verdict; `post-receive` records branch owners and the push log after refs move. Fetches do not take the lock.
- **Rationale**: Hooks are separate processes that exit before Git updates refs, so no hook can hold a lock across the update. A session-wide lock makes the advertised tip, the check and the ref update one serialized step per repository, which is what FR-022 and NFR-005 need. `pre-receive` is the only hook that sees every ref and quarantined objects before anything is visible.
- **Alternatives considered**: `proc-receive` would make `hn` responsible for applying ref updates itself, which duplicates Git's ref transaction code. `reference-transaction` runs per transaction and cannot express a rejection line per ref with the information the spec requires. A lock taken inside `pre-receive` would be released before the ref update.

### Server home layout and forced command

Decision `01M5R5CP4DDYC71WFE8Q74TET5`.

- **Decision**: A fixed layout under the server user's home: `$HOME/.hn-serve/state` and `$HOME/.hn-serve/repositories`, both mode 0700, and the managed block in `$HOME/.ssh/authorized_keys`. The forced command is the absolute path of the running `hn` executable followed by `serve git --key FINGERPRINT`.
- **Rationale**: The server is a dedicated Unix user (OQ-7), and a fixed layout keeps the forced command exactly as FR-004 states. An absolute path makes the command independent of the `sshd` session `PATH`. Tests point `HOME` at a temporary directory.
- **Alternatives considered**: A `--home` flag in every forced command adds a second source of truth that can drift from the registry.

### Server identification

Decision `01M5R5CRYGCW3H1SFFRDZ42G11`.

- **Decision**: `hn serve repo create` writes `refs/hn/serve/v1` pointing at a blob whose content is `hn-serve/1` followed by a newline. Pushes to it are refused with `reserved-ref`. A client compares the advertised object ID with `git hash-object --stdin` of the same bytes in its own repository, so it needs no extra fetch.
- **Rationale**: The marker travels over every Git transport and needs no new SSH command. Identification only controls which client modes are allowed; all-trusted selection still validates every ref on the client, so a forged marker on an ordinary remote grants no trust (C-006, NFR-006).
- **Alternatives considered**: A custom SSH command such as `hn-serve-info` would work only over SSH and would add a second grammar to the forced command.

### Advancement request wire shape

Decision `01M5R5CVF1JC0DC3JEN2E41X6N`.

- **Decision**: `hn merge PROPOSAL --remote REMOTE [--branch BRANCH]` publishes local facts, reads the remote tip `T`, takes the repair path when the head is already contained in `T`, and otherwise pushes `+HEAD_OID:refs/heads/BRANCH` with push options `hn-advance=CANDIDATE` and `hn-expected-tip=T`. On acceptance it confirms the new tip, signs `proposal.merged` with `commit` and `head` both equal to the proposal head, and publishes again.
- **Rationale**: Publishing facts in their own push keeps them on the server even when advancement is refused, as FR-014 requires; a single push would discard them on refusal because verdicts are all-or-nothing. The expected tip lets the server report `protected-stale-tip` to the loser of a race (US3-6). The forced refspec is needed because Git would otherwise refuse a non-fast-forward on the client and the user would never see the server's structured reason. `validateProposalMergeBinding` and `validateMergeEvent` check subject, head, policy and acceptance evidence and never inspect `commit` ancestry, so the fast-forward fact validates on every client without a protocol change. The only code that assumes a merge commit is `findProposalMergeCommit` in the local repair path, which the remote mode does not call.
- **Alternatives considered**: One atomic push carrying both the actor chain and the branch update; rejected for the reason above.

### Validation reuse boundary

Decision `01M5R5CY0Z5RZ3V0J8Q2W5C49K`.

- **Decision**: The hook reuses the client replication validators listed in the plan's reuse table, with the hosted repository as both the quarantine and accepted directory. Two additions are needed: a range variant of `validateQuarantinedActor` that re-reads only `old..new`, and an option on the dependency check that accepts a candidate event whose code ref is neither hosted nor in the same push.
- **Rationale**: FR-007 requires "the same signature, sequence, parent and relationship validation used for replication import". Per-commit loading with `loadStoredEvent` costs about two `git show` processes per event, so a 1,000-event repository would need about 2,000 subprocesses per push; the batched reader in `issue_read.go` uses one `rev-list` and one `cat-file --batch`. The code-ref exception exists because `publishLocalFacts` pushes actor refs before proposal refs in separate `git push` invocations.
- **Alternatives considered**: A separate server validator duplicates trust logic and invites drift. Changing `hn sync` to push all refs in one atomic push would change client behavior against ordinary remotes.

### Destination policy evaluation

Decision `01M5R5D0KF0C3VSZVSMJT1F6ZR`.

- **Decision**: Extract `evaluateProposalUnderPolicy(proposal, events, policy, decisionDigest)` from `evaluateProposal`. Base evaluation passes the base policy and its digest, which keeps current behavior. Destination evaluation passes the destination policy and the base digest.
- **Rationale**: `evaluateProposal` filters decisions by `Event.Policy == digest`, and maintainers sign against the base policy digest. Filtering by the destination digest would discard every acceptance whenever the policy file changed. Applying the destination policy's actor lists and thresholds to the same signed facts gives C-004 and the spec's edge case that evidence from actors removed from the destination policy does not count.
- **Alternatives considered**: Requiring maintainers to re-sign decisions against the destination policy changes the governance flow and wire semantics.

### Additional reason codes and unnamed ref namespaces

Decision `01M5R5D36533BCY8APVZ0F3PJJ`.

- **Decision**: Add `repository-busy` and `advance-not-maintainer` to the OQ-2 list. Refs outside `refs/heads/*`, `refs/tags/*`, `refs/hn/actors/*`, `refs/hn/proposals/*` and `refs/hn/memory/*/*` are refused with `reserved-ref`. Unknown push options are refused with `unsupported-command`.
- **Rationale**: OQ-2 calls its list initial, and FR-013 requires every code to be documented. Fail-closed handling of unnamed namespaces follows HN-003.

### All-trusted selection storage

Decision `01M5R5D5VWZCC7Z7DXGP50KSMS`.

- **Decision**: Keep selection version 1 and add `allTrusted` with `omitempty`. `hn replication show` prints `all-trusted: true` only when set. Identification is checked at select time and on every sync. Unadvertised exact actor and proposal selectors report `not-advertised`, which is not a failure, only in all-trusted mode.
- **Rationale**: Existing selection files and show output stay byte-identical (FR-019, NFR-007). An older `hn` binary rejects the unknown field through `DisallowUnknownFields`, which fails closed.

### Repository seeding and recovery

Decision `01M5R5E6QD6T2C17K6QQNHVANW`.

- **Decision**: `hn serve repo create NAME --from SOURCE` imports only `refs/heads/*` and `refs/tags/*`, writes the marker ref, sets `HEAD` to the default protected branch, and refuses creation when a protected branch is missing or has no valid `.hn/policy.json` at its tip. Hubnot facts arrive later through governed pushes. `hn serve repo lock-exec NAME -- COMMAND...` runs an administrator command while holding the repository lock, for the documented recovery procedure.
- **Rationale**: Importing `refs/hn/*` would host facts that never passed FR-007 or FR-010, which contradicts FR-017. A protected branch without valid policy can never move, so creating one produces an unusable repository.

### Git LFS transfer path and storage

Decision `01M5R5E9B0RZHT3WNTHTRPW4M2`.

- **Decision**: Implement protocol version 1 of `git-lfs-transfer` as described in the Git LFS [SSH adapter proposal](https://github.com/git-lfs/git-lfs/blob/main/docs/proposals/ssh_adapter.md): `version`, `batch`, `put-object`, `verify-object`, `get-object` and `quit`. The `locking` capability is never advertised and every lock command answers status 501. `git-lfs-authenticate` is refused as `unsupported-command`. Objects live in `state/repos/NAME/lfs/objects/`; uploads stream to `state/repos/NAME/lfs/incoming/`, are checked for size and SHA-256, and are renamed into place under `state/repos/NAME/lfs.lock` after a quota recheck.
- **Rationale**: The proposal says the client runs `git-lfs-transfer {path} {operation}` over SSH with `upload` or `download`, and that locking is enabled only when the server advertises it. That fits the forced-command model and needs no HTTP listener. Verify-then-rename means an interrupted or mismatched upload is never downloadable and never counted.
- **Alternatives considered**: `git-lfs-authenticate` plus an HTTPS endpoint would add a listener, TLS configuration and a second authentication path.

### Test harness

Decision `01M5R5EC1FPH1M0KVQMPPVE91C`.

- **Decision**: Each test builds `hn` with the existing `buildOperationalBinary` pattern and writes one POSIX `sh` shim per client key. The shim skips `ssh` options, takes the host and command arguments, finds the managed `authorized_keys` entry for its public key, and runs that entry's `command=` value under `env -i` with `HOME`, `PATH`, `SSH_ORIGINAL_COMMAND` and, when present, `GIT_PROTOCOL`. Clients use `GIT_SSH_COMMAND=<shim>` and `GIT_SSH_VARIANT=ssh`. Test SSH public keys are generated with `crypto/ed25519` and encoded in OpenSSH wire format, so only the real-sshd test needs `ssh-keygen`.
- **Rationale**: Executing the generated command string tests FR-004 and FR-005 together. `env -i` reproduces a minimal `sshd` session and proves the wrapper does not depend on inherited variables. The real-sshd test is the only proof of `restrict` semantics, so it is kept but allowed to skip with a reason.

### Session dispatch grammar and environment

Decision `01M5R5EEKH2R8NQ1VV3DAR0BAF`.

- **Decision**: The grammar in [contracts/serve-cli.md](contracts/serve-cli.md). The wrapper executes Git directly with an environment built from scratch and passes server configuration with `-c`.
- **Rationale**: A closed grammar with no shell removes the injection cases in the spec's edge list. A rebuilt environment means the hook's `HN_SERVE_ACTOR` can only come from the wrapper. OpenSSH documents that `restrict` disables port, agent and X11 forwarding, PTY allocation and `~/.ssh/rc` execution, and that a forced command receives the client's request in `SSH_ORIGINAL_COMMAND` ([sshd(8), AUTHORIZED_KEYS FILE FORMAT](https://man.openbsd.org/sshd.8#AUTHORIZED_KEYS_FILE_FORMAT)).

## Verified assumptions from the spec

The spec lists assumptions that planning had to check against code.

- **A fast-forward `proposal.merged` fact validates on existing clients.** Confirmed by reading `validateProposalMergeBinding` (`store.go`), `validateMergeEvent` (`policy.go`) and `validateReplicationMergeAt` (`quarantine.go`). None of them read `Event.Commit` beyond `validGitOID`. `lineage.go` treats any merge fact as closing the lineage. No validation path changes; `docs/governance-v0.md` documents the meaning.
- **Bare hosted repositories have no client replication state.** `readReplicationStateDirectory` returns "absent" when `<gitDir>/hn` does not exist, so `loadPolicy`, `loadStoredEvent` and `collectIssueEvents` work unchanged inside the hook.
- **Proposal readiness depends on a hosted code ref.** `evaluateProposal` sets `CodeMatches` from `proposalHead`, which reads `refs/hn/proposals/*`. A candidate whose code ref has not arrived is never ready, which keeps the code-ref exception safe.

## Premortem

The planner assumed the mission had failed and asked what most likely caused it. The table lists the causes judged plausible and the plan element that addresses each.

| Failure story | Likelihood | Plan response |
|----|----|----|
| Protected branch moved without CI because destination evaluation skipped a pipeline only the destination requires | Medium | Destination evaluation loads every destination pipeline at the proposal head and rejects when a definition is missing. A fixture adds a destination-only pipeline |
| Two maintainers advanced at once and both succeeded | Low | Session lock plus expected tip; NFR-005 test runs 50 rounds of 10 concurrent pushes |
| An attacker sent `HN_SERVE_ACTOR` through SSH environment forwarding | Low | `restrict` and the default `PermitUserEnvironment no` block it, and the wrapper rebuilds the environment |
| A rejected push left objects reachable | Low | Git quarantine discards them. Tests assert `git cat-file -e` fails for a pushed object after rejection |
| A hosted repository's own `config` disabled hooks | Low | Hooks come from `-c core.hooksPath`, which overrides repository config; administration never writes `core.hooksPath` into the repository |
| The extraction in `policy.go` changed `hn proposal status` output | Medium | Behavior-preserving extraction with unchanged existing tests |
| The Phase 0 flow still needed manual selection because the runner's own actor had no SSH key | Medium | The quickstart binds a key for every participant, including runners, and SC-004's test registers all of them |
| LFS objects counted twice after an interrupted upload | Low | Quota counts only files under `lfs/objects/`, recomputed under the LFS lock |

## Adversarial evidence

No dependency is added, upgraded or removed, so the `contracts/adversarial-evidence-contract.md` requirement for dependency decisions does not apply and no contested findings exist to record. The security-relevant design choices above were challenged in the premortem, and no separate adversarial-squad run took place during planning. Disposition: `deferred_with_rationale`. An adversarial review of the push verdict is better spent on the implemented hook during work-package review, where it can exercise real fixtures.
