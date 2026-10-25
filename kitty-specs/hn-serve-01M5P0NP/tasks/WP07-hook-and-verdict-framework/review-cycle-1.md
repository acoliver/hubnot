---
affected_files: []
cycle_number: 1
mission_slug: hn-serve-01M5P0NP
reproduction_command:
reviewed_at: '2026-10-25T10:20:53Z'
reviewer_agent: user
wp_id: WP07
---

# WP07 review: changes required

Reviewer: llxprt, independent of the implementation model.

## Reviewed implementation

- Workspace: `/home/acoliver/projects/appaday/gitium/hubnot/.worktrees/hn-serve-01M5P0NP-lane-a`.
- Reviewed HEAD: `a08e4d431b3b68f8c1d9943a0efb07d80b2dac7a`.
- Authoritative prompt base: `ceb364d846952d1a24afd36502c9637fac34f016` (`kitty/mission-hn-serve-01M5P0NP`).
- WP07 implementation commit: `851520206500fc970ba730e3418b3fc290b65d9b`, parent `465f574d46f7ae7683653da6814c59b9e70ebaa7`.
- WP07's six implementation files are unchanged between its implementation commit and reviewed HEAD. Earlier lane changes belong to dependencies.

## Blocking issue 1: budget failures discard per-ref diagnostics and audit updates

Locations: `serve_hooks.go:234-238`, `serve_hooks.go:355-367`, `serve_hooks.go:403-409`, and `docs/serve-v0.md:785-788`.

T042 requires exceeding either ref-input bound to reject every ref with `over-budget` and `budget=ref-updates`. FR-013 and the push-verdict contract require one structured line and sentence naming each rejected ref. T045 requires the push log to include its updates.

On a budget failure, the parser returns no updates. Pre-receive then emits one rejection with `ref=-` and logs an empty update list. A real push of 10,001 valid new branches through the public forced-command/receive-pack path reproduced this: exit 1, hosted refs unchanged, exactly one `over-budget` rejection with `ref=-`, and zero updates in the rejected push-log entry. The byte-limit path has the same loss and additionally uses `budget=ref-update-bytes` instead of the specified `ref-updates`.

Required correction:

1. Preserve bounded-memory input handling while retaining or streaming sufficient ref information to emit the required ordered per-ref rejection pairs and complete audit updates for either exceeded bound.
2. Use the contracted budget field.
3. Add hook-level or real-push regressions checking ref names, diagnostic count/order, actor attribution, push-log updates/codes, and unchanged hosted refs. Existing parser-only budget tests do not exercise the hook output or log.
4. Update the guide's budget paragraph to describe the corrected behavior. Its current single-session-rejection description contradicts the acceptance contract.

## Blocking issue 2: valid ordinary branch `@` is classified as reserved

Locations: `serve_verdict.go:99-102` and `serve_state.go:903-905`.

T043 classifies other `refs/heads/*` as ordinary branches. FR-011 and the push-verdict contract allow a mapped actor to create an unprotected branch. The new classifier calls `validServeBranchRef`, whose inherited validation rejects the branch name `@`.

`git check-ref-format --branch @` succeeds on the installed Git. A real mapped-actor push of `HEAD:refs/heads/@` through the live forced-command path nevertheless exits 1 with `code=reserved-ref`. This is a server classification restriction absent from the supplied contract.

Required correction:

1. Align ordinary-branch classification and branch-owner validation with the contracted Git branch surface. Changing only the classifier would still prevent post-receive from persisting ownership through the same validator.
2. Add a real-push regression proving creation of `refs/heads/@`, its owner record and log entry, and subsequent ownership enforcement.
3. Align any guide text describing extra branch-name restrictions with the corrected behavior.

Keep all pre-existing tests and assertions unchanged. Add regression coverage without weakening existing assertions.

## Acceptance and requirement coverage

| Criterion | Review result |
| --- | --- |
| T041 | PASS: live hook dispatch, mapped actor/key/repository validation, symlink-resolved Git directory validation, registry recheck and configuration loading. |
| T042 | FAIL: issue 1. Other malformed OID/input, duplicate-ref and push-option checks fail closed. |
| T043 | FAIL: issue 2. Context, class evaluation ordering, ref-sorted aggregation and fail-closed deferred arms are otherwise implemented. |
| T044 | FAIL: issue 2. Tag immutability, normal ownership, unowned branches, maintainer lookup and reserved refs are otherwise implemented and tested. |
| T045 | FAIL: issue 1. Normal owner creation/deletion/preservation, accepted/rejected attribution, ordered appends and non-rejecting post-receive behavior are otherwise implemented. |
| T046 | Real pushes cover US1-6, unowned branches, invalid policy, tags, reserved refs, options, hook override resistance, key revocation, attribution and mixed-ref quarantine rejection. New regressions are needed for issues 1 and 2. |
| T047 | Maintained guide and executable rejection example are delivered. Documentation needs the corrections in issues 1 and 2. |

- FR-006: normal rejection/log attribution is exercised; oversized-push audit updates are incomplete under issue 1.
- FR-010, WP07 portion: tag create-only and reserved-ref enforcement are implemented. Actor/proposal/memory and protected semantic rules are explicitly scheduled for WP08 and WP09; current placeholders fail closed.
- FR-011: normal ownership scenarios pass; valid branch creation fails under issue 2.
- FR-012: mixed pushes preserve all hosted refs with and without `--atomic`, and rejected commits are absent from the hosted object database.
- FR-013: normal rejection pairs are parseable, attributed and sorted; over-budget per-ref output fails under issue 1.
- NFR-003, WP07 portion: malformed input/options, invalid default policy and deferred rules fail closed.

## Reachability, scope and assertion integrity

Production command paths reach both hooks through `main.go` -> `cmdServe` -> `cmdServeHook`; pre-receive reaches classification/evaluation and rejected logging, and post-receive reaches owner persistence and accepted logging. Lazy `HostedEvents` and PushFacts consumers are expressly assigned to WP08 T048-T051 and WP09 T054-T057 in `tasks.md`.

The five frontmatter-owned files are in scope. `serve_state.go` writers are explicitly authorized by the WP01 carry-over maintainer note in WP07. Shared `serve_commands.go`, `serve_verdict.go` and `docs/serve-v0.md` edits follow sequential lane ownership; the WP06 activity-log handoff records the wrapper-held RepoLock contract. Hooks do not reacquire RepoLock. The separate pre-existing layout-write lock protects atomic owner staging.

No pre-existing test file or assertion was changed. WP07 adds `serve_push_test.go`. No frozen signed fixture/schema, dependency file, generated binary or private key was changed. Behavioral documentation was updated with implementation.

## Required anti-pattern checklist

1. Dead code: PASS. Production call sites verified; explicitly scheduled WP08/WP09 fact consumers satisfy the dependent-library exception.
2. Synthetic-fixture tests: PASS. Behavioral tests invoke real Git and production CLI paths; unit tests invoke production parser/classifier functions.
3. Silent empty return: PASS. Empty results represent acceptance, no changes, or the explicitly fail-closed empty maintainer set; failures propagate errors or diagnostics.
4. FR coverage: PASS for the checklist's assertion-per-requirement condition. Full acceptance still fails on the uncovered contract cases above.
5. Frozen surface: PASS. No pre-existing tests or frozen signed fixtures changed.
6. Locked decision: PASS for explicit prohibitions on signing, commit creation, history rewriting, quarantine bypass and RepoLock acquisition in hooks. Positive contract violations are issues 1 and 2.
7. Shared-file ownership: PASS, with the carry-over authorization and sequential coordination recorded above.
8. Production fragility: PASS. No unexplained panic/raise path found; failure paths return errors, reject, or report non-rejecting post-receive bookkeeping errors.

## Verification evidence

The independent reviewer executed:

```sh
go test -count=1 -v -run '^TestServePush(AllOrNothing|BranchOwnershipAndPushLog|DocumentedExample)$' .
```

All three selected tests passed. Both blocking issues were independently reproduced through real pushes. Evidence is under `/home/acoliver/projects/appaday/gitium/.scratch/wp/hn-serve-01M5P0NP/WP07/independent-review/` in `focused-tests.log` and `contract-repros.log`. Generated reproduction fixtures, key, binary and isolated cache were removed.

The reviewing parent executed all required quality gates from the lane workspace:

```sh
# Every changed .go file against the authoritative prompt base:
git diff --name-only --diff-filter=ACMR -z kitty/mission-hn-serve-01M5P0NP..HEAD -- '*.go' | xargs -0 -r gofmt -l
go vet ./...
go build ./...
go test -race -count=1 -timeout 60m -skip '^TestMemoryIndexTenThousandRecordsPerformanceAndRecovery$' ./...
go test -count=1 -run '^TestMemoryIndexTenThousandRecordsPerformanceAndRecovery$' .
```

All gates passed. Formatting printed nothing. Vet and build exited 0. The completed race run exited 0 (`hubnot`, 268.944s); the standalone non-race performance test exited 0 (11.241s). No base-worktree comparison was needed. An initial foreground race attempt was terminated by the shell's 120-second inactivity cutoff; its processes were confirmed stopped before the successful managed-job rerun.

Logs and temporary files were scoped under `/home/acoliver/projects/appaday/gitium/.scratch/wp/hn-serve-01M5P0NP/WP07/`: `gofmt.log`, `vet.log`, `build.log`, `race.log`, and `performance-head.log`. No test assertions were edited, and no temporary worktree was created. Approval remains blocked by the two reproduced contract defects despite the passing gates.

## Dependency handoff

WP08 depends on WP07. WP08's agent must wait for the corrected framework and rebase any existing work onto the corrected lane head before continuing. This review changes only WP07's status.
