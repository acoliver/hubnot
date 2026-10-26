---
work_package_id: WP06
title: Standing acceptance test harness
dependencies:
- WP01
- WP03
requirement_refs:
- C-002
planning_base_branch: feat/standing-acceptance
merge_target_branch: feat/standing-acceptance
branch_strategy: Planning artifacts for this mission were generated on feat/standing-acceptance. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into feat/standing-acceptance unless the human explicitly redirects the landing branch.
subtasks:
- T025
- T026
- T027
- T028
- T029
- T030
- T091
phase: Phase 2 - Shared derivation and test harness
history:
- !!omap
  - timestamp: '2026-10-26T06:54:14Z'
  - agent: system
  - action: Prompt generated via /spec-kitty.tasks
agent_profile: implementer-ivan
authoritative_surface: standing_harness_test.go
create_intent:
- standing_harness_test.go
execution_mode: code_change
owned_files:
- standing_harness_test.go
role: implementer
tags: []
tracker_refs: []
---

# WP06: Standing acceptance test harness

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter, and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `codex`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## Objective

Build one black-box test harness, `standing_harness_test.go`, that every later test WP uses for local and ordinary-remote scenarios (C-002). It drives the built `hn` binary and `git` against real temporary repositories and one real bare remote, with every role of the spec, and it builds valid and defective signed rule merge facts for the fixture matrix of [contracts/rule-merge-fact.md](../contracts/rule-merge-fact.md).

This WP adds no production code and no test of mission behavior: it adds helpers and a self-test. The `hn serve` fixture is separate and lives in `standing_serve_test.go` (WP14, tasks decision 6).

## Context

- Read spec "User Scenarios & Testing" (all independent tests), [quickstart.md](../quickstart.md) sections 1 to 4, and [contracts/rule-merge-fact.md](../contracts/rule-merge-fact.md) "Fixture matrix".
- Start with `spec-kitty agent action implement WP06 --agent <name> --mission standing-acceptance-01M5VD2K`.
- Helpers to reuse from existing test files (read them, do not edit them): `buildOperationalBinary`, `runOperationalCommand`, `runOperationalCommandFailure`, `runOperationalCommandFailureWithEnv`, `runOperationalGit`, `runOperationalGitBytes`, `initOperationalRepository`, `configureOperationalGit`, `readOperationalIdentity`, `operationalLabel`, `operationalActorRef`, `operationalDigest`, `latestOperationalEvent`, `assertOperationalContains`, `operationalEvent` (`operational_acceptance_test.go`); the policy-fixture pattern of `newPolicyPublicFixture` (`policy_rules_acceptance_test.go`); `serveGovernedPolicy.JSON` and `serveGovernedPipelineJSON` as models for policy and pipeline JSON (`serve_protected_test.go`); the forged-chain technique of `TestServeFactsAlteredEventIsInvalid` and `newServeTestChain` (`serve_facts_test.go`).
- CLI forms: `hn init --name NAME`, `hn identity public`, `hn proposal open --base REV --head REV [--body TEXT] TITLE`, `hn review PROPOSAL --approve|--request-changes [--body TEXT]`, `hn run request PROPOSAL PIPELINE`, `hn run execute REQUEST --backend host --allow-unsafe-host-execution`, `hn decide PROPOSAL --accept|--reject [--body TEXT]`, `hn merge PROPOSAL`, `hn sync [REMOTE]`, `hn replication select REMOTE --all`.
- An identity lives in its repository; each participant therefore has its own clone. `runOperationalCommand` uses a fresh `HOME` per call and a 20-second timeout; if any harness command needs longer, add a `standingHarnessRun` variant with its own timeout instead of changing the shared helper.

## Branch Strategy

- **Planning/base branch at prompt creation**: `feat/standing-acceptance`
- **Final merge target for completed work**: `feat/standing-acceptance`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. Each computed lane gets exactly one worktree, and sequential WPs in the same lane reuse it.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `standing_harness_test.go` | NEW. All helpers prefixed `standingHarness` or methods on `*standingHarness`. Later WPs add their own helpers in their own files. |

## Subtasks

### Subtask T025: Participants, clones and a shared bare remote

**Steps**:

1. Type:

   ```go
   type standingHarness struct {
       t        *testing.T
       binary   string
       remote   string            // bare repository path, the "origin" of every clone
       work     map[string]string // role -> clone path
       actors   map[string]string // role -> full actor fingerprint
       base     string            // main after the seed policy commit
       commits  int
   }
   ```

2. `newStandingHarness(t, policy standingHarnessPolicy) *standingHarness`. Roles, each a clone with its own identity (`hn init --name`), Git user configuration and `hn replication select origin --all`:
   - `M` maintainer; `A` author; `R` trusted reviewer counted by the rule ("gpt-reviewer" in the spec); `S` second trusted reviewer not counted ("sonnet-reviewer"); `T` trusted runner of pipeline `test`; `E` executing actor of rule `ordinary`, not a maintainer; `X` executing actor of a second rule `other` only.
3. The binary is built once per test with `buildOperationalBinary`. If the package already caches a built binary for a test run, reuse that mechanism; otherwise build per test and keep the number of harness tests small.
4. Seed: a source repository commits `.hn/policy.json` (from the policy builder, with the real fingerprints), `.hn/pipelines/test.json` (one step that fails while a flag file exists, as `serveGovernedPipelineJSON` does), and `src/`, `tests/`, `docs/` placeholder files; it is pushed to the bare remote as `main`. Every clone then fetches and checks out `main`. `h.base` is that commit.

### Subtask T026: Standing policy builder and adoption through a maintainer amendment

**Steps**:

1. Builder by role:

   ```go
   type standingHarnessRule struct {
       Name, TargetBranch string
       Acceptance         *string  // nil leaves the field out
       Actors, Authors    []string // roles, resolved to fingerprints
       Reviewers          []string // roles; nil leaves the field out
       Paths              []string
   }
   type standingHarnessPolicy struct {
       Maintainers, TrustedReviewers []string // roles
       RequiredApprovals             int
       AllowAuthorApproval           bool
       Runners, Requesters           []string // roles for pipeline "test"; Requesters nil leaves it out
       ProtectedPaths                []string // nil leaves protectedChecks out; M is the designated reviewer
       Rules                         []standingHarnessRule
   }
   ```

   `func (p standingHarnessPolicy) JSON(actors map[string]string) string` encodes with `map[string]any` so absent fields stay absent, as `serveGovernedPolicy.JSON` does.
2. `standingHarnessDefaultPolicy()`: maintainers `[M]`, trusted reviewers `[R, S]`, `requiredApprovals` 1, `requiredAccepts` 1, runner `[T]`, rule `ordinary` with `acceptance` `rule`, actors `[E]`, authors `[A]`, reviewers `[R]`, paths `src/`, `tests/`, `docs/`, and rule `other` with `acceptance` `rule`, actors `[X]`, authors `[A]`, paths `other/`.
3. Two ways to install a policy:
   - `newStandingHarness(t, policy)` seeds it directly as the first commit (fast path for most tests).
   - `h.AdoptThroughAmendment(policy) (amendment string)`: from a seed without automerge, M commits the new policy on a branch, A or M opens the amendment proposal, R approves, M requests CI and T runs it, M accepts with `hn decide --accept`, M runs `hn merge` on `main` and pushes `main` and its facts with `hn sync origin` and `git push origin main`. Returns the amendment's candidate ID and updates `h.base`. This exercises the manual path the spec requires for adoption (US2-2) and must never call `hn automerge`.

### Subtask T027: Candidate, review and CI helpers

Each helper runs in the role's clone, syncs before acting and publishes after acting, and returns the event it appended (read with `latestOperationalEvent` from the role's actor ref, asserting the expected kind):

- `h.Commit(role, parent string, files map[string]string) string`: commit on a detached `parent` in the role's clone; a value `standingHarnessRemove` deletes the path. Returns the commit. Pushes the commit to a scratch branch so other clones can fetch it.
- `h.Open(role, base, head string) operationalEvent`: `hn proposal open`; also creates and publishes the proposal code ref (the CLI does this).
- `h.Approve(role, candidate)`, `h.RequestChanges(role, candidate)`.
- `h.RequestCI(role, candidate) operationalEvent`, `h.RunCI(request string) operationalEvent` (as T), `h.FailCI()` and `h.PassCI()` toggling the pipeline's flag file.
- `h.Accept(role, candidate)`, `h.Reject(role, candidate, body)`.
- `h.ReadyCandidate(files map[string]string) operationalEvent`: A commits on `h.base`, opens, R approves, M requests CI, T runs it, with no maintainer decision. This is the US1 starting point.
- `h.Checkout(role, branch, commit)`: put a clone's `main` at a commit with a clean worktree, as automerge requires.

### Subtask T028: Chain, ref and output inspection helpers

- `h.Events(role) []operationalEvent`: every event on the role's own actor chain, oldest first.
- `h.MergeFacts(role, candidate string) []operationalEvent`: every `proposal.merged` for the candidate across all actor refs in the role's clone. Extend `operationalEvent` reading so the `rule` field is visible: if `operationalEvent` has no `Rule` field, parse the payload with a local struct in this file (`standingHarnessFact` with `Rule`, `Evidence`, `Policy`, `Commit`, `Head`, `Actor`, `Subject`) via `git show <ref>:event.json`; do not edit `operational_acceptance_test.go`.
- `h.Refs(role) string`: `git for-each-ref --format='%(refname) %(objectname)'` for before/after comparisons; `h.RemoteRefs()` the same for the bare remote.
- `h.Verifier() string`: a fresh full clone with no Hubnot identity, with `hn replication select origin --all`, for US1-2 style checks.
- `h.Run(role, args...) (output string, status int)`: run `hn` without failing the test, for refusal assertions.

### Subtask T029: Signed fact builder for valid and defective rule merge facts

**Purpose**: WP07, WP08, WP09 and WP16 need facts that the real client would never sign.

**Steps**:

1. `h.SignFact(role string, mutate func(*Event)) string`: `t.Chdir` into the role's clone, `loadIdentity()`, `nextEvent(identity, "proposal.merged")`, fill a valid rule merge fact for a given candidate (`Subject`, `Head`, `Commit`, `Policy` = base policy digest, `Evidence`, `Rule` = `ordinary`), apply `mutate`, and `appendEvent` (which validates content only, never relationships). Returns the fact ID. Restore the working directory afterwards (`t.Chdir` does this at test end; call it in a subtest or save and restore explicitly when signing several facts in one test).
2. `h.SignRaw(role string, payload []byte) string`: for payloads Hubnot's own signer never writes, of any kind: the non-canonical rule shapes of F19 and F20 and the legacy history fixtures of T091. Sign the exact bytes with the identity's private key, then write a chain commit the way `appendEventWithAttachments` does (`event.json` and `signature` blobs, `mktree`, `commit-tree` with the previous chain head as parent, `update-ref` of the actor ref with the old value). Read `appendEventWithAttachments` in `store.go` for the exact tree layout and signature encoding (`base64.RawStdEncoding`).
3. `standingHarnessDefects(h, candidate operationalEvent) map[string]standingHarnessDefect`: one entry per fixture F01 to F22 of the contract, each with the signer role, a builder function and the expected outcome: a check name, `maintainer-path` for F19 and F20 (no rule claim, so the refusal is the existing `merge <id> is not authorized by ...`), or `missing dependency`. Every defect is signed by a non-maintainer (E, or X for F03) and cites approvals and run results, so path (a) fails for all of them. The candidate-shape defects (F15 author outside, F16 path outside, F17 policy change, F18 protected change, F22 absent commits) need their own candidates; give each defect a `Prepare` function that creates its candidate first.
4. Keep the builder independent of WP05's Go types: it may refer to check names only as strings, so this WP compiles before the validator exists.

### Subtask T030: Harness self-test

**Steps** (`TestStandingHarness...`):

1. `newStandingHarness` with the default policy: every role has a distinct fingerprint, `hn policy show` in M's clone prints rule `ordinary`, and `main` equals `h.base` in every clone.
2. `ReadyCandidate`: the candidate exists in E's clone after `hn sync origin`, with one approval and one passing result, and zero `proposal.decision` events.
3. `AdoptThroughAmendment`: `main` advances by a merge commit, M's chain gains one acceptance decision and one acceptance merge fact, and `hn policy show` lists the adopted rule.
4. `SignFact` with no mutation writes one `proposal.merged` with `rule` `ordinary` to E's chain, and `MergeFacts` reads its `rule` and `evidence`. Do not run any command that projects events in E's clone after signing in this self-test: until WP07 lands, projection runs only the maintainer check and refuses this fact (FR-025).
5. `SignRaw` writes a chain commit whose `event.json` bytes equal the given payload.
6. Mark the slow tests with `if testing.Short() { t.Skip(...) }` only if they exceed a few seconds each; record timings in the Activity Log.
7. `standingHarnessLegacyFixtures` and `standingHarnessBoundaryFixtures` (T091) each build every fixture once in a fresh harness without error, and every raw payload verifies with `verifyEvent` in-process.

### Subtask T091: Legacy history and activation boundary fixture builders

**Purpose**: decision `01M5VSEHEVA82N9T64ZH1P1Q2A` requires signed legacy fixtures on every admission surface and against the older client, and decision `01M5VSEHF5B1FM9TB52KWKQGWZ` requires boundary fixtures at every landing state. WP07, WP08, WP09, WP16 and WP17 consume both sets.

**Steps**:

1. `standingHarnessLegacyFixtures(h) []standingHarnessFixture`, one entry per contract row L01 to L06, each with a stable ID, the signer role, a builder and the expected verdict (`valid`, `valid maintainer merge`, `valid maintainer merge with unverified annotation`, or `refused as before` for L06). Build L01 and L02 with `SignRaw` from the JSON that `encodeAndSign` would produce for the same event plus the extra key. Build L03 to L05 the same way from a maintainer merge of a candidate that M accepted with `h.Accept`, adding the `rule` key variants. Use a policy with maintainers-mode rules only (for example rule `maint` with actors `[M]`), so the older client of WP17 can read it; give L04 `rule` set to `maint`.
2. `standingHarnessBoundaryFixtures(h) []standingHarnessFixture` for contract rows B1 to B4: B1 is `SignFact("E", nil)` for a ready candidate; B2 and B3 are M-signed `proposal.merged` facts built with `SignFact("M", ...)` whose evidence is M's accept decision (plus the approval for B3) and whose `Rule` is `nosuch` (B2) or `ordinary` (B3); B4 is F01. Each entry records whether it needs path (b) (`B1` only) and its expected verdict on a surface that implements path (b) and on one that does not. B5 is a push, not a fact; WP13 builds it.
3. Keep these builders independent of WP05's Go types, as for the defect table.

## Test Strategy

Only the self-test runs in this WP. Run `go test -race -run 'TestStandingHarness' ./...`, then the full gate.

## Definition of Done

- [ ] T025 to T030 and T091 complete; full gate passes.
- [ ] **C-002**: every helper drives the public `hn` and `git` CLIs, except the two fact signers, which only write signed facts that the CLI then judges.
- [ ] Adoption uses `hn decide` and `hn merge`, never `hn automerge`.
- [ ] The defect table covers F01 to F22 with expected outcomes; F19 and F20 expect the maintainer-path refusal.
- [ ] The legacy (L01 to L06) and boundary (B1 to B4) fixture builders exist and self-test.
- [ ] No pre-existing test file is edited.

## Risks

- In-process `t.Chdir` signing is not safe with `t.Parallel()`. Harness tests and every test that signs facts must not be parallel.
- Run results need a real pipeline execution on the host; keep the pipeline step trivial.

## Reviewer Guidance

- Run the self-test twice and confirm no state leaks between runs (all paths under `t.TempDir()`).
- Check that the raw signer produces a chain the existing `loadStoredEvent` can read.

## Activity Log

- 2026-10-26T06:54:14Z – system – Prompt generated via /spec-kitty.tasks
- 2026-10-26T09:45:00Z – llxprt – Prompt revised after analysis (C1, C2; decisions 01M5VSEHEVA82N9T64ZH1P1Q2A and 01M5VSEHF5B1FM9TB52KWKQGWZ): SignRaw for any kind, F19 and F20 expect the maintainer-path refusal, T091 adds legacy and boundary fixtures
