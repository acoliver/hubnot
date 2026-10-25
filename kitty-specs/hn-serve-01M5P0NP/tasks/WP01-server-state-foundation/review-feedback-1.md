# WP01 independent review: REQUEST_CHANGES

## Scope and authority

Reviewed only WP01 source, tests and documentation in `/home/acoliver/projects/appaday/gitium/hubnot/.worktrees/hn-serve-01M5P0NP-lane-a`, against `kitty/mission-hn-serve-01M5P0NP` (`839233710fcc03259f50ddf1c967d86f5f60607e`). Observed lane HEAD: `9ce7febdd226fbd2a57cecc1fad1423ffc7e9805`. The WP01 code commit is `f0029d1fa53af01b15b86a30415ad966f718a5d3`; its seven changed files are `main.go`, `serve_commands.go`, `serve_state.go`, `serve_state_test.go`, `serve_lock_unix.go`, `serve_lock_other.go`, and `docs/serve-v0.md`.

Read the entire generated review prompt at `/tmp/spec-kitty-review-prompts/hubnot-3129962f0ff3/hn-serve-01M5P0NP/WP01/479ae8c591214d4bb4e91ac004053493.md`, plus the required shared `spec.md`, `plan.md`, `data-model.md`, `contracts/server-state.md`, and `contracts/serve-cli.md`. No separate profile/charter load or alternative context discovery was performed. Planning-artifact differences and other WPs were excluded from the code review.

No production code, existing tests, WP status, or commits were changed by this review. Reviewer-only reproductions used a Go overlay in the assigned lane, then were removed. Findings below are blocking correctness/security or explicitly required checklist failures. Deferred administration, SSH and hook command implementations are not independently rejected for being deferred.

## Blocking findings

### R1: State foundation has no live production execution path

**Locations:** `serve_commands.go:31-34,44-55`; test substitution at `serve_state_test.go:169-183,356-400,780-841`. Zero-caller state APIs are at `serve_state.go:704,719,736,809,917,933,1006,1027,1067,1155,1189`.

`serveRouteFor` always returns false. Consequently `cmdServe` always returns at line 46 for any nonempty invocation, before resolution or preparation at lines 48-55. The production program cannot execute the new layout validator. The dispatcher/state tests reach it by replacing the global route lookup with test-only handlers. Separately, the eleven listed storage/defaults/logging/locking functions have no production call sites at all. Tests are their only consumers.

**Exact call-site reproduction:**

```sh
git grep -n -E 'loadServeRegistry\(|saveServeRegistry\(|lookupServeKey\(|newServeRepoConfig\(|loadServeRepoConfig\(|saveServeRepoConfig\(|loadServeBranchOwners\(|saveServeBranchOwners\(|appendServePushLog\(|acquireServeLock\(|serveLockWait\(' -- '*.go' ':!*_test.go'
```

The output contains exactly the eleven function definitions listed above, with no calls. `git grep -n -E 'prepareServeLayout|validateServeLayout|ensureServeLayout|resolveServeLayout|serveExecutablePath|ensureServeHooks|tryServeFileLock' -- '*.go' ':!*_test.go'` shows the validator chain behind the unreachable dispatcher branch, and the platform locking helper behind the unused acquisition function.

**Real-binary reproduction:** build with `go build -o .review-wp01-tmp/hn .`, use an unprivileged temporary HOME, and create `.hn-serve` as a symlink to a missing target. With `HN_INTERNAL_TESTING=0`, both `hn serve git --key test` and `hn serve repo list` exit 1 with `hn: unknown hn serve subcommand "git"; run 'hn help'` or the analogous `"repo"` message. Neither reaches state validation. There is no ref exposure in this reproduction: both commands refuse as unknown.

**Mapping:** mandatory anti-pattern checklist item 1 FAIL; T001's production validation boundary and T002's requested public-command hostile-state fixtures are unproven. This is the explicitly requested production-reachability check, rather than a request to implement future WPs' operational routes.

**Repair:** put the WP01 validation boundary on a real executable `cmdServe` path while retaining the root-first/no-arguments behavior. Exercise that path without replacing the route table in tests. Introduce storage/logging/locking APIs together with meaningful production consumers at their proper command boundaries, coordinating the foundation/follow-on split as necessary to satisfy the prompt's reachability rule. Do not add no-op calls, artificial startup writes, or test-only routes to make a grep pass. The first consumers for deferred functionality can remain in their assigned follow-on WPs, but the strict checklist cannot count the current test-only consumers as production reachability.

### R2: Hosted repository directory modes escape validation

**Location:** `serve_state.go:280-290`, particularly the `IsDir` check at lines 287-289 and unconditional acceptance at line 290.

`checkServeRepositories` checks the repository entry's name, symlink status and directory type, but omits its permissions. An otherwise valid layout with `repositories/demo.git` mode 0755 or 0777 passes session-mode validation. T002 requires every layout directory to be 0700; NFR-008 and the guide explicitly promise refusal of unsafe modes. This check concerns the bare repository's root directory and requires no inspection of repository contents. The 0700 ancestors limit cross-user traversal; the reproduction does not claim to bypass those ancestors.

**Observed reproduction:** create `newServeTestLayout(t)`, `addServeTestRepo(t, layout, "demo")`, chmod `layout.RepoGitDir("demo")` to 0755 or 0777, and call `validateServeLayout(layout, serveSessionValidation)`. Both return nil. Before/after snapshots remain identical, so the check is read-only but incorrectly accepts the defect. Independent tests failed with `accepted unsafe hosted repository directory .../repositories/demo.git with mode 0755` and `... mode 0777`.

**Mapping:** T002 mode validation and each-defect coverage; T007 fail-closed coverage; NFR-008; DoD complete-state-defect coverage. Also contradicts `docs/serve-v0.md:75-78`.

**Repair:** validate the `Lstat` result with the same exact-0700 directory check used elsewhere, preserving path-and-observed-mode diagnostics. Add regression cases for a group-readable and a group/world-writable hosted repository root in read-only session validation. Continue to avoid walking inside bare repositories.

### R3: Accepted executable paths execute shell substitutions in generated hooks

**Locations:** `serve_state.go:1217-1231`, especially the incomplete unsafe-character predicate at lines 1221-1223 and unquoted interpolation at line 1231; `ensureServeHooks` accepts the same path at lines 1235-1246.

The path validator rejects quotes, backslashes, whitespace and controls, but permits `$`, parentheses, backticks and other shell metacharacters. The generated script embeds that path unquoted into a `/bin/sh` command. A valid Unix filename containing command substitution therefore executes commands when Git starts the hook. It can also make the hook execute the wrong pathname or fail. This is local executable-path handling; no claim is made that an SSH client currently controls the installed executable path.

**Observed reproduction:** create an executable named `hn$(touch${IFS}${HN_WP01_MARKER})` in the temporary HOME, set `HN_WP01_MARKER` to `$HOME/shell-expansion-ran`, pass the pathname to `ensureServeHooks`, then execute the generated pre-receive script using `/bin/sh`. `checkServeExecutablePath` and generation both accept it. The hook creates the marker outside `.hn-serve` and `.ssh/authorized_keys`, then exits 127 because the expanded executable pathname ends at `/hn`. The independent test detected the marker and failed. All effects stayed inside the assigned lane's temporary HOME.

**Mapping:** T006 safe executable path / generated-script behavior; NFR-004 write confinement; fail-closed design and locked-decision checklist item 6. This is a behavior/security failure, not a request to change the contract's exact script bytes.

**Repair:** because T006 fixes the unquoted generated bytes, reject every pathname that can be interpreted by the shell rather than used as one literal executable word. A conservative shell-safe character policy is suitable. Test dollar expansion, command substitution, backticks, separators, redirection, globbing and grouping. Execute generated scripts in the regression test and assert that an unsafe name is refused before any side effect.

## Recreating the independent security tests

The following tests use WP01's existing fixture helpers and actual production validator/hook generator. They contain no replacement production implementation. Put them in a lane-owned temporary Go file and expose it as `serve_review_wp01_test.go` with a Go overlay. Reproductions were removed after verification.

```go
package main

import (
    "os"
    "os/exec"
    "path/filepath"
    "reflect"
    "testing"
)

func TestWP01ReviewRejectsUnsafeHostedDirectoryMode(t *testing.T) {
    for _, mode := range []os.FileMode{0o755, 0o777} {
        t.Run(mode.String(), func(t *testing.T) {
            layout := newServeTestLayout(t)
            addServeTestRepo(t, layout, "demo")
            path := layout.RepoGitDir("demo")
            chmodServeTest(t, path, mode)
            before := snapshotLegacyPrivateTree(t, layout.Home)
            err := validateServeLayout(layout, serveSessionValidation)
            if after := snapshotLegacyPrivateTree(t, layout.Home); !reflect.DeepEqual(before, after) {
                t.Fatal("session validation changed the fixture")
            }
            if err == nil {
                t.Fatalf("accepted unsafe hosted repository directory %s with mode %04o; want a state defect naming the path and mode", path, mode)
            }
        })
    }
}

func TestWP01ReviewHookPathCannotExecuteShellExpansion(t *testing.T) {
    layout := newServeTestLayout(t)
    marker := filepath.Join(layout.Home, "shell-expansion-ran")
    t.Setenv("HN_WP01_MARKER", marker)
    path := filepath.Join(layout.Home, "hn$(touch${IFS}${HN_WP01_MARKER})")
    writeServeTestFile(t, path, "#!/bin/sh\nexit 0\n", 0o700)
    if _, err := checkServeExecutablePath(path); err != nil {
        t.Logf("unsafe executable path refused: %v", err)
        return
    }
    if err := ensureServeHooks(layout, path); err != nil {
        t.Fatal(err)
    }
    output, runErr := exec.Command("/bin/sh", filepath.Join(layout.Hooks, "pre-receive")).CombinedOutput()
    if _, err := os.Lstat(marker); err == nil {
        t.Fatalf("generated hook executed shell expansion from the accepted executable path; marker=%s; hook exit=%v; output=%s", marker, runErr, output)
    } else if !os.IsNotExist(err) {
        t.Fatal(err)
    }
    if runErr != nil {
        t.Fatalf("generated hook failed to execute the accepted executable path: %v: %s", runErr, output)
    }
}
```

Overlay JSON used absolute lane paths, mapping `LANE/serve_review_wp01_test.go` to `LANE/.review-wp01-tmp/serve_review_wp01_test.go`. Exact run from the assigned lane:

```sh
TMPDIR=/home/acoliver/projects/appaday/gitium/hubnot/.worktrees/hn-serve-01M5P0NP-lane-a/.review-wp01-tmp go test -overlay .review-wp01-tmp/overlay.json -count=1 -run '^TestWP01Review' -v ./...
```

Exit 1. Unsafe-mode test failed for both modes; shell-expansion test failed after creating the marker. An additional reviewer control, `TestWP01ReviewTransientSocketRefusedReadOnly`, passed: a real Unix-domain socket in `state/tmp` was refused with its path and type, and snapshots were unchanged. Its first harness attempt exceeded the Unix socket address-length limit; using a relative socket address after `t.Chdir(layout.Home)` fixed that harness issue. That initial bind error is not a production finding.

## Acceptance mapping

All source references below are to the assigned lane. All test references are in `serve_state_test.go`.

| Criterion | Verdict | Concrete evidence |
| --- | --- | --- |
| T001: route and three usage lines only | PASS | `main.go:55-56,108-110`; diff is exactly five added lines. |
| T001: no-arguments usage and exit 1 | PASS | Test at 263; fresh binary returned `hn: usage: hn serve repo\|key\|git\|hook ...`, exit 1. |
| T001: root refusal precedes HOME/state for every invocation | PASS | First statement in `cmdServe`, lines 37-40; test at 281 covers repo/key/git/hook/unknown/probes and empty HOME; six real-binary invocations returned exact refusal, exit 1, unchanged HOME listing/sentinel. |
| T001: ungated override ignored; gated decimal parsing strict | PASS | Tests at 320,329; fresh binary with gate 0 and EUID override 0 still returned normal usage. |
| T001: existing usage prefix and three new lines | PASS | Test at 342; fresh `hn help` exit 0 preserved `Usage:\n  hn init` and all three serve lines. |
| T001: shared validation boundary is live before dispatch | FAIL | R1. Probe-route test at 356 passes, but only after substituting the route table; no production route reaches it. |
| T002: absolute HOME, fixed paths, no creation on resolution | PASS | `serve_state.go:100-165`; test at 405 checks all helpers and that HOME remains empty. |
| T002: administration creation / session read-only distinction | PASS at helper boundary | `serve_commands.go:60-66`, `serve_state.go:169-203`; tests at 356,456,760. Actual operational routes remain deferred; R1 records reachability. |
| T002: symlink, entry allowlists, missing config, strict JSON, repository pairing, deterministic paths | PASS at helper boundary | `serve_state.go:205-411,475-573`; defect table at 517, registry/config/owners tables at 926,1122,1248. Bare repository contents intentionally not traversed. |
| T002: every layout directory 0700 / state file 0600 | FAIL | Most paths covered by table at 517; hosted repository root bypasses mode checks, R2. |
| T002: transient files checked by Lstat without contents/name validation | PASS at helper boundary | `serve_state.go:413-458`; tests at 477,780 cover nested entries in tmp/incoming/objects, symlinks, 0640 files, FIFOs and 0755 directories. Reviewer socket control also passes read-only. Device/socket/FIFO classes use the same rejecting default branch. |
| T002: walk limit 100,000 | PASS at helper boundary | Constant at line 29, production calls at 311,391, counter at 427-429; test at 844 verifies inclusive limit and one-over rejection with reduced limit 3. A real 100,001-entry filesystem fixture was not run. |
| T002: public-command hostile transient fixtures | FAIL | Test at 780 uses `run` only after `registerServeProbeRoute`; no unmodified binary command reaches validation, R1. All nine required subtree/type combinations execute and pass at the substituted dispatcher boundary. |
| T002: every defect has a refusing test; ensured empty layout validates | FAIL overall | Empty/populated helper layouts pass tests at 456,477, with generated hooks. Existing defect table passes, but R2 is a missing refusing mode case and R1 blocks real command reachability. |
| T003: two-key byte-stable round trip, 0600, missing registry empty, strict records | PASS at storage boundary | Tests at 860,893,912; shape/lookup at 989,1014; bounded reads/strict decoder at `serve_state.go:688-743`. Public-key parsing/fingerprint recomputation is explicitly left to WP02/WP05 as required. |
| T003: unknown field, duplicate fingerprint, duplicate public key, unsorted keys, short actor, malformed fingerprint, trailing data, over 16 MiB refused | PASS | Each named case is tested at 926-987 through load and/or session validation; invalid saves refuse; version and UTC timestamp checks also tested. Production consumer gap is separately R1. |
| T004: marshalled defaults byte-identical to contract | PASS | Test at 1032 compares actual saved defaults to contract JSON; quota/budgets at 1055; production at 809-816,891-911. |
| T004: invalid names including `..`, `a/b`, `x.git`, 101 bytes, leading dot | PASS | Table at 1092; production byte grammar at 750-761. |
| T004: unknown fields, invalid budgets and config validation | PASS | Table at 1122 includes unknown nested budget, missing/default duplicates, unsorted suffix, invalid refs, version/name mismatch, invalid quota/budgets, trailing data and invalid save; production at 836-886. |
| T004: default branch first, suffix sorted/unique; strict branch owners and empty-save policy | PASS | Tests at 1055,1203,1248; production at 819-875,964-1046; empty owner record remains as tested; guide at 127-134. |
| T005: competing goroutines timeout with sentinel, then acquire after release | PASS at lock boundary | Test at 1289; production at 1155-1184 and `serve_lock_unix.go:12-35`; release is idempotent. |
| T005: unsafe lock paths refused | PASS | Test at 1343 checks symlink, directory, 0644 mode and absence of writes to symlink target; production at 1110-1149 checks Lstat/Fstat identity and uses exclusive creation. |
| T005: two appends are ordered JSON lines at 0600 | PASS at logging boundary | Tests at 1386,1432; production at 1067-1104 writes one newline-terminated JSON buffer, Syncs and closes. No production push-log reader. |
| T005: 120-second default and test-only wait override | PASS | Test at 1365; production at 1189-1198. Malformed/negative testing values keep the documented default. API reachability is R1. |
| T006: scripts have exact generated bytes, atomic 0700 writes | PASS for shell-safe paths | Tests at 1449,1460; production at 1230-1287 writes/chmods/Syncs/renames and syncs directory. |
| T006: session refuses stale/missing/modified hooks; administration repairs | PASS at helper boundary | Defect table at 711-733 and test at 1460; session snapshots at 760; symlinked hooks directory refused at 1501. Repair of individual stale/unsafe hook files is the explicit administration exception. |
| T006: executable path safely usable without quoting | FAIL | Listed quote/backslash/whitespace/control cases pass tests at 1513; accepted shell metacharacters execute expansion, R3. |
| T007: fresh focused race suite | PASS | `go test -race -count=1 -run TestServeState -v ./...`, exit 0; 36 top-level tests, 154 test/subtest starts, no skips, `ok hubnot 1.399s`. |
| T007: complete fail-closed coverage | FAIL | Existing green tests miss R2/R3; R1 substitutes a nonexistent production route in state/dispatcher cases. |
| T007: guide skeleton and completed server-layout section | PASS for required content | `docs/serve-v0.md:1-174`; ordered headings test at 227; overview, dedicated account/root refusal, tree, modes, limits, repair guidance, ordering and local-filesystem lock caution present. Standard Markdown fences/structure inspected; no HTML rendering tool run. Actual behavior must be corrected for R1/R2/R3. |

## Requirement and Definition of Done mapping

| Requirement | Verdict | Evidence |
| --- | --- | --- |
| C-001 | PASS | Only new `serve` branch calls `cmdServe`; no existing workflow calls state APIs. No new startup I/O, daemon, database or service dependency. |
| C-003 | PASS | `go.mod` unchanged, no `go.sum` change, all new imports are standard library; Go 1.26.8 verified. Windows client cross-build succeeds. |
| FR-001, WP01 optional command slice | PASS | New route and usage are the only `main.go` changes. `git grep -n 'cmdServe' -- '*.go'` returns only `main.go` and `serve_commands.go` (including comments). Full existing race suite passes. Network-facing hosting belongs to later WPs and is not judged here. |
| FR-003, storage slice | PASS at storage boundary | Strict bounded registry decode, version/sort/uniqueness/full actor/strict SHA256 fingerprint invariants, stable atomic 0600 writes; tests 860-1028 exercise actual production functions. No claim that deferred key CLI administration is implemented. R1 records missing production consumers. |
| NFR-004, root-first and read-only validation slice | PASS for those subcriteria; FAIL overall safety | Exact refusal before HOME/state and gated override verified through actual binary; read-only snapshots and validator source verified. R3 permits generated-hook side effects outside the allowed state/authorized_keys locations. Full later-WP acceptance write audit is outside this review. |
| NFR-008 | FAIL | Extensive bounded/strict/read-only checks pass their unit tests, but R2 accepts unsafe directory modes and R1 leaves the validator unreachable in the real CLI. |
| NFR-007, ancillary WP01 DoD | PASS for observed gates/assertion preservation | All mandatory gates below pass; `git diff --name-only --diff-filter=MDR BASE..HEAD -- '*_test.go'` is empty. The only test diff is a new file, 1538 added lines and zero deleted lines. |

| DoD item | Verdict | Evidence |
| --- | --- | --- |
| T001-T007 complete and all five quality checks pass | FAIL overall | All quality checks pass; R1/R2/R3 prevent full behavioral/checklist completion. |
| FR-001/C-001 isolation, `cmdServe` grep limited to two files | PASS | Exact grep and `main.go` diff verified. |
| NFR-004 root refusal before state; session validation never writes | PASS | Tests at 281,320,329,760; actual CLI listing/sentinel checks; reviewer read-only mode/socket checks. Broader confinement problem is R3. |
| NFR-008 every owned defect has a refusing test | FAIL | Missing hosted-directory mode refusal, R2; test-injected route gap, R1. |
| FR-003 registry storage invariants | PASS | Tables and round trips at 860-1028. |
| C-003 unchanged go.mod | PASS | Empty dependency diff, exit 0. |
| No pre-existing test assertion changed | PASS | No modified/deleted/renamed existing Go test files; full existing suite green. |
| Guide exists with required skeleton and state section | PASS for required content | Guide and headings test inspected; findings require behavior corrections to match its safety promises. |

## Eight anti-pattern checklist verdicts

1. **Dead code: FAIL.** R1. Targeted production-only greps show eleven APIs with definitions and no calls. The state validation module has a source call chain from `cmdServe`, but no invocation can reach it through the production route table. Tests install their own routes. Deferred future command functionality alone is not the finding.
2. **Synthetic-fixture test: PASS.** Registry/config/default/log fixtures are inputs or expected encodings for actual save/load/decode/append functions. Removing those production functions breaks compilation or behavior. The test-only dispatcher route is a reachability limitation recorded under R1, rather than a literal fixture being asserted against itself.
3. **Silent empty return: PASS.** Nil-error returns signal completed validation/writes; zero-value results carry errors; missing registry/owner files and optional layout entries have explicit contract-defined empty/absent semantics; unknown route returns false; missing key returns `(zero,false)`. Malformed lock-test override fallback is explicitly documented. No swallowed production validation exception was found.
4. **FR coverage: PASS for WP01's requirement slices.** FR-001 optional routing/usage/independence is exercised by `run`, the usage test, the full unchanged suite and independent binary calls. FR-003 storage behavior is asserted by real registry round trips and rejection tables. Later CLI/network portions are explicitly deferred. R1 separately fails the mandated production-consumer rule.
5. **Frozen surface: PASS.** Only the seven WP01 code/test/doc paths change in its code commit. `git log --oneline BASE..HEAD -- go.mod go.sum identity_keyring.go replication.go testdata` is empty; no pre-existing test changed. No signed-fact, journal, historical/frozen evidence, private key or generated binary was added by the WP01 code change. Frozen digest recomputation is N/A because no frozen evidence bytes changed. Planning-artifact changes were excluded as directed.
6. **Locked decision: FAIL.** R2 contradicts exact directory privacy/fail-closed state rules, and R3 contradicts the executable-path no-quoting safety assumption and NFR-004 write confinement. No new network listener, signing identity, merge creation, shell-based Git dispatch or third-party dependency was found in WP01 production source. The generated hook shell itself is contract-required.
7. **Shared-file ownership: PASS with this explicit coordination note.** `lanes.json` places these shared surfaces in sequential lane-a. `serve_commands.go` is the dispatcher seam for WP05/WP06/WP07; `main.go` is limited here to the serve route/usage, with later WP10 changes reserved; `docs/serve-v0.md` has section ownership comments for subsequent WPs. Corrective changes to those surfaces must preserve these seams and be consumed by dependent WPs. The Activity Log explicitly authorizes the two out-of-map platform lock files to preserve non-Unix client builds; the Windows cross-build passed. No other source-file ownership exception is needed.
8. **Production fragility: PASS.** Production-only panic/recover search has no matches (grep exit 1). Go errors consistently propagate with context for malformed or unsafe state; missing optional files have explicit successful semantics; contention uses the sentinel and timeout; EINTR retries. No new panic/bare raise on a transient CLI/request race was found. Read/open races refuse loudly rather than reporting success. This does not remove the specific R2/R3 defects.

## Fresh verification results

Host: Linux amd64, uid/euid 1000, Go `go1.26.8 linux/amd64`. Code and tests ran only in the assigned lane. Mandatory suite and focused suite ran sequentially, without overlapping duplicate suites.

| Command | Exit | Exact result / relevant evidence |
| --- | --- | --- |
| `go test -race ./...` | 0 | `ok hubnot 280.727s`; `? hubnot/examples/issue-consumer [no test files]`. Fresh execution, no cached marker. |
| `go test -race -count=1 -run TestServeState -v ./...` | 0 | `ok hubnot 1.399s`; example package has no tests; 36 top-level tests, 154 test/subtest starts, zero skips. |
| `go vet ./...` | 0 | No diagnostics. |
| `gofmt -l .` | 0 | No filenames/output. |
| `go build ./...` | 0 | No diagnostics. |
| `git diff --check` | 0 | No diagnostics. |
| `git diff --check kitty/mission-hn-serve-01M5P0NP..HEAD -- main.go serve_commands.go serve_state.go serve_state_test.go serve_lock_unix.go serve_lock_other.go docs/serve-v0.md` | 0 | No diagnostics. |
| `go build -o .review-wp01-tmp/hn .` | 0 | Built scoped binary for CLI checks. |
| `GOOS=windows GOARCH=amd64 go build -o .review-wp01-tmp/hn.exe .` | 0 | Documented non-Unix lock stub preserves client compilation. |
| Independent Python CLI assertions using scoped binary and temporary HOME | 0 | Usage exit 1, help exit 0, all three serve lines, six forced-root refusals exit 1 with exact text and unchanged HOME; ungated UID override ignored. Symlinked root invocations return unknown-subcommand rather than state defects, R1. |
| Reviewer-only Go overlay tests, `-count=1 -run '^TestWP01Review' -v ./...` | 1 | Both unsafe hosted-directory mode cases FAIL; accepted shell path produces marker and hook exit 127, FAIL; transient socket refusal control PASS. Initial socket harness bind-length issue was corrected and rerun. |
| `git diff --exit-code -- main.go serve_commands.go serve_state.go serve_state_test.go serve_lock_unix.go serve_lock_other.go docs/serve-v0.md` | 0 | Reviewed source/test/doc files unchanged during review. |

The first CLI probe attempt exited 1 because `go build ./...` produces no runnable `hn` artifact at the assumed scoped path. An explicit `go build -o .review-wp01-tmp/hn .` corrected the harness, and every real-binary check above was rerun successfully. This was not a WP01 build failure.

The baseline `<gate-coverage-junit>` no-artifact condition is explicitly pre-existing and is not a WP01 regression or a blocking finding. No JUnit artifact was required from these direct Go verification runs.

## Coordination, handoff and limitations

**WP02 and WP05 dependency notice:** WP01 requires corrective changes. Rebase/replay dependent work onto the corrected WP01 before proceeding, especially around executable-path handling, layout privacy, and production consumption of state APIs. Preserve the shared dispatcher/docs seams described above. This feedback and the review result carry the notification for foreground-agent relay; this review did not contact or alter another WP, inspect its status, or transition any task.

**Foreground action:** process REQUEST_CHANGES and the required status transition yourself. No `move-task`, commit, merge, push or production edit was performed by this reviewer.

Limitations: no real SSH/Git network session or future-WP administration flow was exercised; those routes are intentionally deferred. Filesystem/device creation requiring elevated privileges was not attempted. Transient entry bounds were verified using the production counter with a reduced limit, not 100,001 physical entries. Markdown structure and required headings were checked, with no separate HTML renderer. Unix behavior was executed on Linux; Windows was compile-only. The review does not claim resistance to every same-uid filesystem race beyond the inspected checks. All reviewer-owned binaries, scripts, overlay files, temporary homes and disposable logs are removed after their evidence is captured here; the pre-existing untracked `.spec-kitty/` directory is preserved.
