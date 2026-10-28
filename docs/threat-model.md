# Collaboration and optional transfer boundary

This document describes native collaboration and optional transfer additions, alongside the existing
[replication boundary](replication-v0.md), [CI execution boundary](ci-v0.md),
[governance rules](governance-v0.md) and [memory safety model](memory-safety.md).
Hubnot remains an experimental repository-native protocol, not a hardened
multi-tenant service.

## Authority and hostile data

Accepted signed Git actor histories are the durable source of issue facts.
Issue queries rebuild a verified in-memory view. There is no authoritative
issue cache, database, service, or hidden synchronization source.

Fetched payloads, signatures, object framing, actor sequences, parent/root
references, relation targets, run attachments and ref advertisements are
untrusted. The issue reader reuses signature, actor-chain and relationship
validation and rejects pending admission. Physically present objects do not
become accepted merely because they can be read from the object database.
Missing suppliers remain quarantined until explicit selected recovery succeeds.
Queries never promote or repair replication state.

JSON requests reject unknown or duplicate fields, explicit null values,
malformed raw UTF-8 and unpaired surrogate escapes. Valid text, including
intentional U+FFFD, remains data. Machine stdout is one JSON value; consumers
must keep diagnostic stderr separate and avoid interpreting issue text as
commands or instructions.

## Bounds and operational failures

New requests, state fields, revision parents and output pages have explicit
[documented bounds](issues-v1.md#limits). The reader bounds ref enumeration,
history traversal, individual object lengths and aggregate object bytes before
allocating those object bodies. A persistent Git batch process replaces
per-event object-fetch subprocesses. All selected signed records remain
verified; mixed governance histories can still require separate policy or
pipeline reads. No constant total process-count claim applies to arbitrary
non-issue governance content.

A resource-limit error does not mean a legacy signature is invalid. A naturally
failed Git batch child retains its bounded diagnostic and exit cause as a
repository error. Deliberate reader cancellation retains the original parser
or resource-limit diagnosis. These are local validation and ingestion bounds,
not universal CPU/disk/network quotas. Replication pack transfer has its own
explicit limitations documented in the replication guide.

## Concurrent actors and retries

A revision refers to its stable opening ID and exact prior issue revisions.
The actor's Previous field separately links its own append-only history.
Concurrent issue heads remain visible with authorship and full states; neither
timestamps nor arrival order grants one head authority.

The writer checks an observed snapshot and uses actor-ref compare-and-swap.
This protects one actor append, not a cross-actor global transaction. Other
actors can create valid concurrent work after that snapshot. Pagination rejects
changed snapshots; staged resolution requires the exact observed snapshot and
preserves every unconsumed head.

Operation identity is scoped to the signing actor. Signed intent and a digest
of canonical request semantics distinguish retries from new work. Replays
return the original event before checking current heads; divergent key reuse
fails. These guarantees do not merge independent actors' operations or create
an execution lease.

Assignment, lifecycle, criteria, graph edges and namespaced metadata are
informational. They do not grant Go Kitty execution, review acceptance,
integration authority, or Hubnot maintainer/reviewer/runner policy roles.
Distributed cycles are explicit graph diagnostics, not a reason to discard
otherwise valid signed facts or silently declare work ready.

## Compatibility and recovery

Existing signed records are retained byte-for-byte. Rich changes append new
facts; they do not rewrite history. Older executables may reject `issue.revise`
or new rich payload shapes. Rolling back a binary cannot remove already
published signed facts, and this feature provides no implicit history rewrite
or Beads migration. Preserve a compatible reader when retaining rich histories.

Standing acceptance follows the same rule. Executables built before it, such
as the reference build of commit `7a47e69`, reject a policy that contains
`acceptance` or `reviewers` as an unknown field. That includes
`"acceptance": "maintainers"`, which means the same as leaving the field out.
Every client that works on a repository using either field therefore needs a
newer executable. Older executables also reject every `proposal.merged` fact
that is valid only through a standing rule. Such a fact lists no maintainer
acceptance decisions and is usually signed by an actor who is not a
maintainer, so the older maintainer check refuses it. A mixed team fails
closed and cannot mistake standing acceptance for maintainer acceptance.

Older and newer clients agree on every maintainer merge, including one whose
payload also names a rule. The older client ignores the unknown `rule` key,
and the newer client runs the maintainer check first and reports the rule
name as an unverified annotation. Both clients also reach the same verdict on
all history signed before standing acceptance. Rolling back a binary does not
remove published rule merge facts, and a newer client keeps judging each of
them against its own base policy. `TestStandingCompatOlderClient` builds the
older client and checks each of these statements.

## Automation identity and historical observations

Public identity JSON explicitly selects actor, name and public key; it never
serializes the private identity record. Issue mutations may pin an expected
actor fingerprint. The comparison binds the captured identity actually used
for append, before operation replay; a mismatch produces no issue append. The
active identity file changing after capture cannot substitute another signer.
This is signer attribution, not a cross-actor lock or new execution authority.

Actor/key operation lookup runs the existing bounded verified reader and
returns one original signed event's request semantics, never current issue
state. Duplicate signed keys fail explicitly. Invalid/over-budget history
cannot be interpreted as an absent operation. Detailed list pages use the
same verified catalog and mode-bound cursors; conflicting state is null and
full state is never truncated. Detailed rows also expose the immutable opening
event metadata (empty for legacy), so clients can compare original and current
claims without separate history calls. No namespace interpretation is applied.
Neither read creates an index, bypasses trust
admission, or grants review, execution, assignment or merge authority.

Machine identity discovery reads and validates existing active or legacy identity
state without writing private keyring files or Git refs. It never performs the
legacy migration retained by default human identity commands. Invalid private
state yields a typed public error without exposing private fields or paths.


## Native typed resources

Projects/items, discussions/replies and workflows use strict type-specific
states in registered signed event kinds. Child items/replies are independently
revisioned aggregates. Project items pin exact schema revisions and accepted
answers pin exact reply revisions; a later schema or reply edit does not
reinterpret those historical facts. Cross-resource references participate in
accepted-history, quarantine and shallow-closure validation. Unknown new kinds
can be rejected by older binaries; upgrading never rewrites old signed bytes.

Native workflows separate inert foreign source from validated native pipeline
state. Materialization writes explicit pipeline bytes, while a run request must
bind the exact signed workflow revision and committed proposal pipeline bytes.
Import, materialization and request never execute source text. Existing policy,
actor and explicit runner backend checks remain the execution authority.

## Optional provider boundary

The local `hn gh issue` subset calls native issue functions and performs no
forge access. Provider-neutral transfer models contain no HTTP/SDK dependency;
network clients and GitHub registrations are excluded with `nogithub`. All
native facts remain usable without a provider, credential or service.

Only explicit transfer commands contact an explicitly selected provider.
Transport requires HTTPS except explicit loopback-IP HTTP and refuses redirects
or pagination outside the requested endpoint. Requests/responses, nested
pagination, aggregate bytes, whole-command time, native payloads and complete
future checkpoints have finite bounds. GraphQL errors/partial data, malformed
or ambiguous JSON, invalid Unicode and incomplete traversal fail before their
content is accepted. Provider errors do not echo bodies or tokens. External
source locators in signed metadata are descriptive; inspecting them does not
fetch those addresses.

Original authors, URLs, category/lock states, assignees, project visibility,
workflow enabled state and other provider observations remain claims signed by
the importing local actor. They never grant native policy roles, moderation,
runner rights or permission to execute instructions. GitHub creation uses the
authenticated provider account and cannot impersonate original authors/times.
Unsupported semantics require explicit mapping/acknowledgment or refusal;
foreign workflow preservation is not a claim of execution equivalence.

Imports preflight a complete selected issue or typed graph before publication,
but a multi-event import can leave valid partial work when interrupted. Its
recovery reuses exact frozen source content, signer, native expectations and
verified signed progress. Baselines and mappings require producer-shaped signed
import witnesses; editable latest metadata alone cannot reset overwrite authority.

Issue, discussion and project exports authenticate their complete canonical
private checkpoint with the currently selected native identity. This closes
manufactured comment/parent/field bindings or progress that could otherwise
suppress or redirect publication. Keys are not stored in the journal; a signer
switch, unsigned journal or altered/noncanonical envelope requires explicit
recovery. Preview remains private-key-free. The journal signature authenticates
local intent, not remote success or provider permissions. Workflow recovery
separately binds exact native/source bytes, explicit destination and guarded
blob intent. All exporters preflight the whole future journal, including pending
results and accumulated bindings, before the first mutation.

Private journals/lock files remain below the resolved Git directory with
0700 directories and0600 files, outside cloned signed facts. One local lock is
not a distributed lease. Every remote mutation persists prepared/in_flight
intent before one write; an uncertain outcome needs one exact positive
reconciliation. A negative complete scan is not proof of nonexecution and
cannot authorize another POST. Structured publication markers are forgeable
associations and need native/provenance/content checks; ordinary marker-like
prose is inert data. Remote-only edits are preserved or refused for manual
recovery, never silently made a new overwrite baseline. External APIs do not
supply atomic compare-and-swap for every field, so a single publisher and
explicit reconciliation remain necessary. There is no global exactly-once or background-sync guarantee. Provider transfer
never grants native review, protected-check, execution or automerge authority.

## Optional protected checks and native automerge

Both capabilities require explicit native repository policy. Initialization,
upgrades, imported metadata and actor names do not enable either or infer roles.
Omission or `null` disables a section in supporting clients; older clients reject
unknown present policy fields. GitHub accounts, `gh`, hosted checks and provider
permissions confer no native authority.

Protection gates literal file changes rather than detecting every semantic
weakening of lint. It automatically covers policy and required pipeline
definitions while enabled; maintainers must also select relevant configuration,
scripts and indirect inputs. Deletion, rename, mode/type and ancestor replacement
cannot escape a selected path. Inline suppressions or changes to unselected
source files still need ordinary code review.

Designated approvals require exact candidate IDs and distinct listed actors
other than its author. They are signed claims, not proof of honest review.
Current reviews govern readiness; immutable acceptances carry exact evidence
IDs and retain their original meaning. Local and quarantine admission both
validate designated evidence. A proposal cannot disable protection to authorize
itself. A new manual merge also requires revision after current-target policy
or protected-baseline changes, including identical-policy target tightening
hidden by restoring old base bytes in the candidate.

Automerge permits only a named rule's explicitly listed executing actors,
authors, branch and allowed paths, and only after the current approval and CI
requirements hold. It does not create those facts. Named rules are data, not
executable expressions or instructions. A rule's acceptance mode decides where
acceptance authority comes from. Under acceptance mode `maintainers`, the
default, every executing actor must be a base-policy maintainer and every
proposal still needs its current maintainer acceptances. Under acceptance mode
`rule`, acceptance authority moves from per-proposal maintainer decisions to
the maintainer-accepted policy amendment that adopted the rule. The executing
actor gains authority over exactly that rule: it cannot publish a decision that
counts, merge with `hn merge`, repair through `hn merge` or execute a rule that
does not list it (`TestStandingAutomergeContainmentForbiddenCommands`,
`TestStandingAutomergeRepairConstructedState`).

Policy amendments and protected-check changes stay manual under both acceptance
modes, even after review (`TestStandingAutomergeContainmentManualPath`). Policy
validation refuses a standing rule whose paths cover `.hn` or overlap a
protected-check path (`TestStandingPolicyFieldsRefusals`). A standing rule
needs at least one independent approval: `proposals.requiredApprovals` must be
at least 1, and the candidate author's own approval never counts, even when
`allowAuthorApproval` is true (`TestRuleAcceptanceLiveAuthorApprovalByMode`).
Every verifier re-derives the rule from the candidate's base policy, its base
and head commits and the evidence the merge fact lists, and refuses the fact
when any check fails (`TestStandingFactMatrixAgreement`). An eligible preview
does not grant its reader signing authority or carry authorization into a later
execution.

Preview loads no signing key and changes no collaboration refs, target, index or
private identity state. It refuses shallow and active partial/promisor clones
before object evaluation, preventing lazy fetch or recovery writes in this
command. The supported automatic path uses complete local Git data. Preview,
and execution without `--remote`, fetch no objects, start no daemon, execute
no workflow, and neither synchronize nor publish; normal native operation does
not need a provider or network service. When the rule's target branch tracks a
remote, execution without `--remote` reads only that remote's
`refs/hn/serve/v1` advertisement with `git ls-remote`. It refuses an upstream
that is an `hn serve` remote, or one it cannot reach to tell, before it creates
a merge commit or merge fact. Existing explicit local CI host execution remains
separately unsafe and separately opted in per invocation.

Execution with `--remote REMOTE` contacts the network and has side effects
before and after the hosted advancement. It first checks that `REMOTE`
identifies as an `hn serve` remote and that its replication selection imports
every trusted actor. It then runs `hn sync REMOTE`, which imports the hosted
facts and publishes the local actor chains. That publication happens even when
the rule evaluation that follows the import refuses the advancement. After the
second evaluation, the client asks the server to fast-forward the hosted target
branch to the signed proposal head; no local merge commit is created. Once the
server applies the update, the client records the merge fact and publishes it.
A rerun after the rule merge fact exists takes the publication-only path, which
publishes the existing fact and confirms the hosted branch without advancing it
(`docs/serve-v0.md`, "Automerge rules" and "Rerunning a rule advancement").

Local execution, without `--remote`, rechecks current candidate/evidence and
target, binds its actual signer, and advances the local target using the
signed-base expected-old ref lease. It refuses an unfinished Git operation,
dirty index/worktree or an ignored-file collision. Automatic `commit-tree` creation does not invoke `pre-merge-commit`
or `pre-commit` hooks: required checks must be configured as native pipelines.
Operator-controlled Git `reference-transaction` hooks remain outside proposal
instructions and retain Git behavior.

The ref lease is local; it does not make actor evidence and branch updates an
atomic distributed transaction or constrain arbitrary host/Git writers. Direct
Git writes can bypass Hubnot's path, so these rules are not host access control.
Interrupted checkout/ref/event steps can leave inspectable work or a missing
merge fact. Recovery observes the actual target, preserves local changes and
uses the existing manual missing-event repair where applicable; uncertain
outcomes do not authorize a second merge or destructive reset.

### Residual risks of standing acceptance

The maintainer accepted the following risk when standing acceptance was
planned. A modified client of an executing actor, working against an ordinary
remote, can leave a current change request or a trusted pipeline failure out
of its view, merge the candidate and record a rule merge fact whose listed
evidence satisfies the rule. That fact verifies. Verifiers judge a merge fact
by its own signed content, the base policy, the candidate's code and the
evidence it lists, and events outside that evidence do not change its validity
(`TestRuleAcceptanceFactImmutable`). Signed facts on different actor chains
have no delivery-order winner, so a verifier cannot tell whether the executing
actor saw a veto in time. Acceptance merge facts are judged the same way, and a
malicious maintainer can already record one while ignoring a change request.

`hn serve` closes this gap for protected branches. When an advancement request
names a rule, the server evaluates the rule itself against the facts it holds
at that moment. A current trusted change request or trusted failure refuses the
request with `protected-rule-veto`, and a current maintainer rejection refuses
it with `protected-blocked` (`TestStandingServeVerdictVetoes`,
`TestStandingServeVerdictRuleNotSatisfied`). Repositories that adopt standing
rules should host their target branches on `hn serve` and protect them there.

A case-variant directory such as `.HN/` is ordinary content. Git tree paths are
case-sensitive, and Hubnot reads policy and pipeline definitions only from
`.hn/policy.json` and `.hn/pipelines/`. Policy validation accepts a standing
rule over `.HN/` and refuses one over `.hn/`, and a rule over `.HN/` gives no
control over policy or pipelines
(`TestStandingQualityCaseVariantDirectoryIsOrdinaryContent`).

## hn serve trust boundary

`hn serve` hosts repositories for a dedicated, unprivileged Unix account. This
section is the threat model for that server and for the clients that use it:
what each party can and cannot influence, which test or cited source backs
each claim, and which risks remain. The operator guide is
[serve-v0.md](serve-v0.md), and its
[Verification and traceability](serve-v0.md#verification-and-traceability)
section maps every requirement to its tests.

### Assets and actors

The assets are the hosted repositories below `$HOME/.hn-serve/repositories`,
the protected branches of each one, the key registry in
`$HOME/.hn-serve/state/registry.json` with the managed block of
`$HOME/.ssh/authorized_keys` generated from it, and the signed Hubnot facts the
repositories hold.

| Actor | What it controls |
|----|----|
| Administrator | The server Unix account: its files, the `hn serve repo` and `hn serve key` commands and the `sshd` configuration |
| Mapped actor | One or more SSH keys bound to one full actor fingerprint; may fetch and push every hosted repository, subject to the push checks |
| Unmapped client | Anyone else who can reach `sshd`; holds no key in the managed block |
| Other local user | Another account on the server host; has no access to the server account's files, which are mode 0700 and 0600 |

### Sessions and the forced command

**Authentication and session restrictions.** OpenSSH authenticates every
connection; `hn serve` implements no SSH protocol of its own. Each managed
`authorized_keys` entry has the form
`restrict,command="ABS_HN serve git --key FINGERPRINT" KEY`. The `restrict`
option disables port, agent and X11 forwarding, terminal allocation and
execution of `~/.ssh/rc`, and the forced command replaces whatever the client
asked to run. The client's request reaches the forced command only as the
`SSH_ORIGINAL_COMMAND` variable
([sshd(8), AUTHORIZED_KEYS FILE FORMAT](https://man.openbsd.org/sshd.8#AUTHORIZED_KEYS_FILE_FORMAT)).
`TestServeSSHDRoundTrip` runs a real unprivileged `sshd` with the generated
entries and checks that a terminal request and a local port forward are
refused while Git fetch and push work. The forced command names a fingerprint
only. The actor is looked up in the key registry on every connection, so
removing a binding takes effect at the next connection
(`TestServeSessionRemovedKeyRefused`). A key without a binding is refused
before any repository data moves (`TestServeSessionUnknownKeyRefused`).

**Closed command grammar.** `hn serve git` accepts `git-upload-pack`,
`git-receive-pack` and `git-lfs-transfer` requests for one repository name, in
a fixed grammar with single spaces as the only separators. It matches the
request byte by byte and never hands it to a shell, so quoting, command
substitution, separators and extra arguments cannot add a command. Requests
with control bytes, non-ASCII bytes, double quotes, backslashes, `$`,
backticks, `;`, `|`, `&`, `<`, `>`, more than one path segment or an unknown
command are refused with `unsupported-command` before Git starts
(`TestParseServeRequestRefusesEverythingElse`). A refusal writes only to
stderr, so the Git protocol stream on stdout stays empty.

**Environment built from scratch.** Git and the hooks receive only `PATH`,
`HOME`, `LANG=C`, `GIT_CONFIG_NOSYSTEM=1`, a `TMPDIR` inside the server state,
`GIT_PROTOCOL` in its exact `version=N` form, and `HN_SERVE_ACTOR`,
`HN_SERVE_KEY` and `HN_SERVE_REPOSITORY` taken from the registry
(`TestBuildServeGitEnvironmentKeepsOnlyServerValues`). Every other variable
from the session is dropped, including any `GIT_DIR`, `GIT_CONFIG_*` or
`LD_PRELOAD`. A client therefore cannot choose the actor that the hooks
attribute a push to. OpenSSH adds a second barrier: under `restrict` and the
default `PermitUserEnvironment no`, a client cannot set session variables
apart from those the administrator allows with `AcceptEnv`
([sshd_config(5)](https://man.openbsd.org/sshd_config.5#PermitUserEnvironment)).
The test suite uses `HN_INTERNAL_TESTING=1` to pass `HN_TEST_SERVE_*`
variables through; those variables only come from the server account's own
session, and every reader of them also requires `HN_INTERNAL_TESTING=1`.

**Server configuration wins over repository configuration.** A push runs
`git receive-pack` with `core.hooksPath`, `receive.fsckObjects` and
`receive.advertisePushOptions` given as `-c` options
(`TestServeSessionStartsGitWithServerControlledInvocation`). Git gives
command-line settings precedence over the hosted repository's `config`
([git-config(1), CONFIGURATION FILE](https://git-scm.com/docs/git-config#_configuration_file)),
and `GIT_CONFIG_NOSYSTEM=1` keeps the system file out, so a hosted repository
cannot disable the push checks or object checking. Fetches run
`git upload-pack --strict`. Administration commands never write hooks or
`core.hooksPath` into a hosted repository.

**Attribution without authority.** A key binding records which actor a
connection speaks for and lets that key fetch and push. It grants no policy
role. Maintainer, reviewer and runner roles come only from signed policy
documents, the server signs no facts, and its verdicts are never evidence a
client relies on.

**Least privilege.** Every `hn serve` command refuses to run as uid 0 before it
reads any state (`TestServeStateRootRefusalPrecedesEveryStateAccess`). The
server writes only below `$HOME/.hn-serve/` and to the managed block of
`$HOME/.ssh/authorized_keys`. `TestServeQualityEnforcedWriteBoundary` runs a
full acceptance run with every server-side process, administration commands
included, inside a bubblewrap sandbox in which only those two locations are
writable, and `TestServeQualityWrittenPathsAndModes` compares the server home
and a sentinel `TMPDIR` before and after the same run and checks the modes
NFR-008 requires. Before each session the server validates the whole layout
without writing, and any defect, such as a hosted repository directory
replaced by a symbolic link, refuses every session
(`TestServeStateValidationRefusesEachLayoutDefect`). The session wrapper
itself writes only the repository lock file; during a push, Git and the hooks
write to the hosted repository and its state directory.

### Hostile pushed data

Every pushed object and ref is untrusted until the `pre-receive` hook accepts
the whole push.

- **Quarantine before refs move.** Git keeps a push's objects in a quarantine
  directory while `pre-receive` runs and deletes it when the hook rejects the
  push ([githooks(5), pre-receive](https://git-scm.com/docs/githooks#pre-receive)).
  A rejected push changes no ref, and its objects never enter the hosted
  object database (`TestServePushAllOrNothing`).
- **Object checking.** `receive.fsckObjects` makes Git check every received
  object before the hook sees it
  ([git-config(1), receive.fsckObjects](https://git-scm.com/docs/git-config#Documentation/git-config.txt-receivefsckObjects)).
- **The replication validators for every Hubnot ref.** An actor chain is
  accepted only from a key bound to that actor, only as an append, and only
  when the new events pass the same signature, sequence, parent and
  relationship validation that client replication import uses
  (`TestServeFactsActorKeyMismatch`, `TestServeFactsAlteredEventIsInvalid`,
  `TestServeFactsActorChainRewrite`, `TestServeFactsDependencyMissing`,
  `TestValidationSeamsActorRangeValidation`). Proposal code refs must match a
  validated candidate's signed head and never move afterwards, and memory
  streams pass `hn-memory/0` validation (`TestServeFactsProposalRefRules`,
  `TestServeFactsMemoryRefRules`). Refs outside the documented classes,
  including the server marker, are refused with `reserved-ref`
  (`TestServePushTagsAndReservedRefs`).
- **Budgets with inclusive limits.** Each pushed fact ref is measured against
  five per-repository budgets that default to the replication defaults; a
  measurement equal to its limit passes and one above it is refused with
  `over-budget` (`TestServeFactsBudgetsAreInclusive`). Two fixed ceilings
  bound the hosted facts that every push reads
  (`TestServeFactsHostedFactCeilings`), and a push is limited to 10,000 ref
  updates and 4 MiB of ref update lines
  (`TestServePushOverRefUpdateBudgetRejectsEveryRef`).
- **A closed push option set.** Only `hn-advance`, `hn-expected-tip` and
  `hn-rule` are accepted, each at most once with a well-formed value, and
  `hn-rule` only together with `hn-advance`; any other option refuses the
  whole push with `unsupported-command` (`TestServePushOptions`,
  `TestStandingServeWireRuleOptionShapeRefusals`).

### Governance of protected branches

**The protected-branch invariant.** After any successful push, every
protected branch tip is the signed head of a proposal that is accepted and
ready under its signed base policy and under the destination policy, which is
the `.hn/policy.json` at the tip being replaced, and that head carries the
trusted CI results both policies require. Evidence signed by an actor the
destination policy no longer trusts does not count toward the destination
check (`TestServeProtectedDestinationPolicy`,
`TestValidationSeamsDestinationPolicySemantics`). A protected tip without a
valid policy refuses every update until the administrator repairs it
(`TestServeProtectedPolicyInvalid`). For an advancement request that names a
standing rule, the server's own evaluation of that rule takes the place of the
maintainer acceptances, and the destination policy must be the base policy, as
the next paragraph describes.

**Advancement under a standing rule.** An advancement request may name a rule
with the `hn-rule` push option. For such a request the server evaluates the
named rule itself, from the candidate's signed base policy and the facts it
holds when the push arrives, including the vetoes among them: a current
trusted change request or trusted failure refuses the request with
`protected-rule-veto`, and a current maintainer rejection refuses it with
`protected-blocked`. The requesting mapped actor must be one of the rule's
executing actors, which replaces the maintainer requirement for that request;
any other actor is refused with `advance-not-rule-executor`. When the
destination policy differs from the base policy, the server refuses the
request with `protected-destination-policy` and the candidate must be revised
(`TestStandingServeVerdictAcceptsRuleAdvancement`,
`TestStandingServeVerdictVetoes`, `TestStandingServeVerdictDestinationPolicy`).
A direct push of a protected branch, and an advancement request that names no
rule, still need maintainer acceptances
(`TestStandingServeAdvanceForbiddenCommands`). The server still signs nothing.
The executing actor's client signs the rule merge fact after the branch moves,
and the server judges that fact like any other fact it receives
(`TestStandingServeAdvanceUserStory4`).

**Merge-fact validity.** Every verifier, the server's actor-chain check
included, judges a `proposal.merged` fact with one union rule. The fact is
valid through the unchanged maintainer check, or, when that check fails,
through the rule check. A rule name on a fact that passes the maintainer check
is an unverified annotation and grants nothing. No event that verified before
standing acceptance stops verifying (`TestStandingFactMatrixAgreement`,
`TestStandingCompatOlderClient`). Each landing state of the mission refused
rule-only facts on every surface that did not yet implement the rule check, so
no intermediate build admitted a fact it could not verify (decisions
`01M5VSEHEVA82N9T64ZH1P1Q2A` and `01M5VSEHF5B1FM9TB52KWKQGWZ`;
`TestStandingFactBoundary`).

**Fast-forward only.** A protected branch moves only to a descendant of its
current tip (`TestServeProtectedNotFastForwardAndStaleTip`). The server never
creates a commit and never signs anything, so the commit on the branch is the
exact commit CI tested, and the merge fact for an advancement is signed by
the client of the maintainer or executing actor that requested it
(`TestServeAdvanceFastForwardsAndVerifies`). The
protected list comes only from server configuration and is never read from
pushed content.

**Verdicts are never evidence.** A client trusts a protected tip only through
the signed facts it verifies itself. A fresh clone with no Hubnot identity
recomputes the review, CI, acceptance and merge evidence from the facts it
fetched (`TestServeAdvanceFastForwardsAndVerifies`,
`TestServeReplicationPhaseZeroFlow`).

### Concurrency

A push holds an exclusive lock on `state/repos/NAME/lock` from before
`git receive-pack` starts until it exits, so the advertised tips, the hook's
verdict and the ref update form one step per repository. With
`hn-expected-tip`, a push checked against a tip that has since moved is
refused with `protected-stale-tip` before any policy check, so the loser of a
race always learns why (`TestServeAdvanceConcurrentMaintainers`). In 50
rounds of 10 concurrent advancement pushes, exactly one push advances the
branch each round (`TestServeAdvanceRaceNFR005`). A push that waits more than
120 seconds for the lock is refused with `repository-busy`, which bounds how
long a slow upload can delay other pushes
(`TestServeSessionPushWaitsForLockAndFetchDoesNot`). Fetches never take the
lock. The lock descriptor is not inherited by Git or the hooks, and the hooks
never take the lock themselves because the session already holds it
(`TestServePushHooksNeverTakeRepositoryLock`).

### What clients rely on

Identification grants no trust. The marker ref `refs/hn/serve/v1` only decides
whether a client may save the all-trusted selection for a remote; a forged
marker on an ordinary remote lets the client attempt the import, and every
ref still goes through client quarantine, validation and the saved per-ref
budgets (`TestReplicationAllTrustedForgedMarkerGrantsNoTrust`). The
all-trusted selection imports only actor chains and proposals; memory streams
still need an exact selection (`TestReplicationAllTrustedImportsOnlyExactMemory`).

A server, or anyone who controls the server account, can withhold refs, delay
them or serve an old state. It cannot make a client accept a fact whose
signature, sequence, parents, relationships or policy evidence do not verify,
because the client runs the same validation for facts from `hn serve` as for
facts from any other remote. Selections other than all-trusted produce the
same result against `hn serve` as against an ordinary remote holding the same
refs (`TestServeReplicationSelectionsMatchOrdinaryRemote`).

### Administrator authority

The administrator is trusted for availability and configuration. No
administrator action creates evidence that clients accept. The administrator
decides which repositories exist, which keys
map to which actors, which branches are protected and which budgets apply,
and can stop the service at any time. `hn serve repo lock-exec` runs an
administrator command under the repository lock; the documented recovery of a
protected branch uses it to move the branch with `git update-ref`, which
bypasses governance by design (`TestServeAdminLockExec`). Such a move creates
no signed fact, so a client that checks the branch tip against the facts sees
no merge evidence for it. The serve guide asks the administrator to record
every repair out of band.

### Residual risks and non-goals

- **Denial of service within the budgets.** A mapped actor can fill a
  repository up to its budgets and ceilings, hold the repository lock for up
  to the length of one push, or open many fetches. The budgets bound each push
  and the ceilings bound the hosted facts; neither bounds connection rates,
  CPU time or disk space for ordinary branches and tags. Rate limiting belongs
  to `sshd` and the host.
- **A compromised server account.** Whoever controls the account can rewrite
  any hosted ref, change the registry, replace the hooks or stop serving.
  Clients then still refuse every invalid fact, as described above, but a
  protected branch tip on that server proves nothing beyond the facts that a
  client verifies for it.
- **Key compromise.** A stolen SSH key acts as its mapped actor for transport
  until `hn serve key remove` takes it out. It cannot sign facts, because
  facts need the actor's Hubnot signing key, which the server never holds.
- **Excluded scope.** This mission does not provide path-scoped approval
  rules, delegated CI requesters, scheduling, queues, remote runners, a web
  UI, a GitHub-compatible API, GitHub publishing, per-repository access lists,
  runner cycle isolation or shared identity across clones (spec constraint
  C-011). Every mapped key may read and push every hosted repository, subject
  to the push checks.

### Git LFS storage

Hosted repositories store Git LFS objects that mapped actors upload over
`git-lfs-transfer`, as the guide describes in
[Large files with Git LFS](serve-v0.md#large-files-with-git-lfs). LFS adds a
second way for a mapped actor to write to the server account's disk, so it
gets its own checks.

**Same attribution and repository checks as Git.** An LFS session runs through
the same forced command as a fetch or push. The key must be bound in the
registry, the request must match the closed grammar, and the repository must
be hosted before the first protocol byte is sent; otherwise the session is
refused with an `hn-serve/1` line on stderr and nothing on stdout
(`TestServeLFSSessionChecksKeyAndRepository`). The rejection lines an LFS session sends
name the mapped actor from the registry. Every mapped actor may read and write
the LFS objects of every hosted repository, as with Git refs.

**Content addressing and verification before download.** An object's name is
the SHA-256 of its content. The server accepts only 64 lowercase hexadecimal
digits as an object ID, so an ID can never name a path outside
`lfs/objects/`. An upload is written to `lfs/incoming/`, and the server
compares its byte count with the declared size and its SHA-256 with the object
ID before the file can be renamed into `lfs/objects/`. A mismatch removes the
file and answers `lfs-object-mismatch` (`TestServeLFSObjectMismatch`). A
download opens the stored file only after checking with `Lstat` that it is a
regular file, and compares the opened file with that check, so a planted
symbolic link is not followed. A stored object is never replaced, so an actor
cannot overwrite an object that another actor uploaded.

**Quota as the bound on storage.** Each repository's stored LFS bytes are
bounded by its quota, 1 GiB unless the administrator sets another value. The
server checks the quota before an upload starts and again under `lfs.lock`
right before the rename, so concurrent uploads cannot exceed it together
(`TestServeLFSQuotaBoundary`, `TestServeLFSQuotaRecheckedBeforeStoring`).
An upload of an object that is already stored skips the quota check because
the server writes none of it to disk. It hashes that data in memory, reads at
most the smaller of the declared size and the stored size plus one byte, and
answers `lfs-object-mismatch` to anything other than the stored content. An
actor who knows a stored object ID therefore cannot use it to stage data
beyond the quota by declaring a large size
(`TestServeLFSStoredObjectRetryStagesNothing`,
`TestServeLFSStoredObjectOversizedRetryEndsSession`).
Requests are bounded too: a pkt-line length header above 65519 is refused
before the server reads the packet, and one request may carry at most 64
arguments, 10,000 object lines and 4 MiB of text (`TestServeLFSFramingRefusals`,
`TestServeLFSPacketReaderChecksLengthBeforeReading`). The server reads at
most an upload's declared size plus one byte of object data, whether it
stores the data or discards it after a quota refusal, because it reads each
data packet only up to the bytes still allowed. Data that goes past the
declared size gets an `lfs-object-mismatch` answer as soon as that extra
byte arrives, and the session ends without reading the rest, so a client
that declares a small object and keeps sending cannot hold the session open
or make the server read without limit
(`TestServeLFSOversizedUploadEndsSession`,
`TestServeLFSUploadReadsNoFurtherThanDeclaredSize`,
`TestServeLFSUploadBodyReadCappedAtDeclaredSizePlusOne`).

**Staging isolation.** Uploads in progress live only in `lfs/incoming/`, which
no download reads and the quota never counts. An interrupted upload therefore
never becomes downloadable. A session removes its staged file when the
connection closes, and every LFS session removes staged files older than one
hour, so a killed session leaves its file behind for at most that long plus
the time until the next LFS session (`TestServeLFSInterruptedUploadNeverStored`).
Renames and removals in `lfs/incoming/` happen under the exclusive layout lock,
so a session validating the layout at the same moment never sees a file vanish
between listing the directory and inspecting the file.

**No locking.** The server never advertises the `locking` capability and
answers every lock command with status 501, so no lock state exists for an
actor to forge or abuse (`TestServeLFSLockingUnsupported`).

**Writes stay inside the server state.** An LFS upload writes only below
`state/repos/NAME/lfs/` and `state/repos/NAME/lfs.lock`, with modes 0700 and
0600, and a download writes nothing. `TestServeLFSEnforcedWriteBoundary` runs
the round trip inside the same bubblewrap boundary as the full acceptance run,
and `TestServeLFSWrittenPathsAndModes` checks the paths and modes by snapshot.

**Residual risk.** A mapped actor can fill a repository's LFS quota with
arbitrary verified objects, after which uploads to that repository fail with
`lfs-quota` until the administrator raises the quota or removes objects by
hand. Objects that no ref points to any longer are not collected. Uploads in
progress do not count against the quota while they are written: only an
object that is not stored yet is staged, each staged file is capped at its
declared size plus one byte, and that size must fit in the quota when the
upload starts, but several concurrent uploads can together
occupy more disk than the quota until they finish or are cleaned up. An actor
can also keep a connection open while it sends object data slowly; this holds
no lock, and connection limits belong to `sshd` and the host.
