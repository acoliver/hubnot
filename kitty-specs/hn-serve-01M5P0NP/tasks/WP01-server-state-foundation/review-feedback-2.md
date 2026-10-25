# WP01 independent implementation review, cycle 2

Verdict: **REQUEST_CHANGES**

Reviewed the assigned lane-a worktree at `65e67fd`, using the supplied review action and full prompt. WP01 implementation commits are `f0029d1` and `f6109bb`. Other work packages and coordination-artifact changes were excluded from this verdict.

## Blocking issue: hook regeneration causes valid concurrent commands to reject each other

**Location:** `serve_state.go:186-197`, `serve_state.go:228-233`, and `serve_state.go:1040-1092`.

Every administration command regenerates both hooks. `writeServeExecutableAtomic` creates a named `.hn-hook-*` temporary file inside `hooks/`, and `checkServeHooks` immediately rejects every entry there except `pre-receive` and `post-receive`. Regeneration and validation have no cross-process coordination. A second valid administration command can therefore validate while the first command's temporary file exists and report false state corruption. This affects live `hn serve repo list`, `repo show`, `repo lock-exec`, and `key list` paths. A per-repository lock in `lock-exec` does not cover this earlier shared-layout phase.

**Observed reproduction:** after initializing one valid empty server home, I ran 160 built-CLI `serve repo list` invocations using 16 concurrent workers. **154 invocations exited 1**, with errors such as:

```text
hn: hn serve state defect: .../.hn-serve/hooks/.hn-hook-1471585735: unexpected entry
```

The final isolated invocation exited 0 with empty output, without any repair by the reviewer. A separate 32-call cold-start run produced 29 failures. The primary initialized-home reproduction rules out missing layout or intentionally corrupt fixtures as the cause.

Run this self-cleaning reproduction from the assigned lane worktree:

```sh
python3 - <<'PY'
import concurrent.futures
import os
from pathlib import Path
import subprocess
import tempfile

with tempfile.TemporaryDirectory(prefix='.wp01-review-', dir='.') as temporary:
    root = Path(temporary).resolve()
    binary = root / 'hn'
    subprocess.run(['go', 'build', '-o', str(binary), '.'], check=True)
    home = root / 'home'
    home.mkdir(mode=0o700)
    env = dict(os.environ, HOME=str(home), HN_INTERNAL_TESTING='0')

    def invoke(_):
        return subprocess.run(
            [str(binary), 'serve', 'repo', 'list'],
            env=env, capture_output=True, text=True, timeout=15)

    initial = invoke(0)
    assert initial.returncode == 0, initial.stderr
    with concurrent.futures.ThreadPoolExecutor(max_workers=16) as pool:
        results = list(pool.map(invoke, range(160)))
    failures = [p for p in results if p.returncode]
    print(f'{len(failures)} failures out of {len(results)} calls')
    for failure in failures[:5]:
        print(failure.stderr.strip())
    final = invoke(0)
    print('isolated final call:', final.returncode, repr(final.stderr))
    assert not failures, 'valid concurrent administration commands rejected each other'
PY
```

**Required correction:** make hook regeneration and whole-layout validation safe across processes. Ensure validators cannot observe an active writer's staging file as corrupt state. Cover session/hook validation as well as administration validation while preserving the session validator's read-only behavior. Preserve rejection of genuine unexpected entries and abandoned staging files; broadly ignoring `.hn-hook-*` would weaken the fail-closed contract. Add production-CLI concurrent-command regression coverage and a deterministic regression for validation during an in-progress hook write. Update the guide if the correction introduces a persistent coordination file or changes its administration rules.

## Requirement and acceptance assessment

| Criterion | Assessment |
| --- | --- |
| C-001, FR-001 | PASS. Existing commands retain their paths. `main.go` changes only add the `serve` route and three usage lines. The service introduces no network listener or dependency for ordinary remotes. |
| C-003 | PASS. `go.mod` and existing dependencies are unchanged; WP01 imports standard-library packages only. Windows/amd64 client compilation succeeds. |
| FR-003, storage portion | PASS for the recorded cycle-2 scope. Production registry decoding enforces version, sorted unique fingerprints, unique public keys, full actors, canonical fingerprint syntax, single-line key shape, and UTC timestamps. Tests exercise production loaders and validators, including unknown fields, trailing data, and the 16 MiB limit. Registry writing and lookup were explicitly deferred in the supplied Activity Log. |
| NFR-004 | PASS. UID refusal is the first dispatcher operation. Independent built-CLI checks covered five command shapes with effective UID 0 and unchanged HOME; the ungated override is ignored. Session validation performs no writes, with valid and defective layouts covered by tree snapshots. |
| NFR-008 | FAIL for concurrent operation: internally generated active hook staging files are diagnosed as state defects. Static malformed-layout cases otherwise have refusing production-path tests. |
| T001 | PASS. No-argument usage, unknown-command handling, strict gated UID parsing, refusal before HOME resolution, and help output are covered. Independent CLI help and refusal checks passed. |
| T002 | PASS for static layout behavior; concurrency blocker above remains. Tests cover fixed absolute paths, creation modes, symlinks, unexpected entries, directory/repository pairing, required config, strict JSON, modes, bounded transient walks, and unchanged session state. Independent built-CLI checks rejected symlinks, group-readable files and FIFOs in each of the three transient subtrees, plus a Unix socket in `state/tmp`. |
| T003 | Registry schemas, shape checks, loaders and strict validation are exercised. Atomic save and two-key save round-trip criteria are DEFERRED by the supplied cycle-2 scope decision, not verified or claimed complete here. |
| T004 | Name/ref/config/owner validation and config loading PASS. Tests exercise contract defaults, invalid names, invalid budgets, unknown fields, default-first protected ordering, and sorted owners. Config creation/saving and owner load/save/empty-file behavior are DEFERRED by the supplied cycle-2 scope decision. No byte-stable generated-default/save result is claimed. |
| T005 | Lock contention, timeout sentinel, symlink refusal, 0600 lock files, default/gated wait, process-level exclusion and child exit-status behavior are covered. Push-log entry types, appender and two-appends criteria are DEFERRED by the supplied cycle-2 scope decision. Production validates push-log type/mode/size and has no log reader. |
| T006 | Exact script bytes, mode 0700, resolved executable-path safety, stale-hook refusal and single-command administration repair PASS. Concurrent hook regeneration FAILS as detailed above. |
| T007, FR-023 foundation | PASS for source/headings and state documentation. The guide has all 14 requested headings, section ownership comments, account/layout/mode/refusal rules, executable relocation guidance, protected-list ordering and local-filesystem guidance. No Markdown renderer was invoked. |
| NFR-007 | PASS. No pre-existing test file or assertion is changed by either WP01 implementation commit. |
| FR-002 / FR-011 / FR-022 / NFR-005 supporting foundation | Config and owner schemas and the repository flock path have meaningful production-path tests. The explicitly deferred writers/appender remain work for their first consumer WPs. This review does not approve those later features. |

## Anti-pattern checklist

1. **Dead code: PASS.** Targeted production-reference checks found callers for all 15 command functions, 57 state functions/methods, and both platform variants of the file-lock helper. Live path: `main.run -> cmdServe -> prepareServeLayout -> validateServeLayout`; `repo show` loads config and totals stored LFS bytes; `key list` loads the registry; `repo lock-exec` calls `acquireServeLock -> tryServeFileLock`. Layout validation calls the registry/config/owner decoders and all path helpers.
2. **Synthetic-fixture test: PASS.** Literal JSON is input to real production decoders/loaders/CLI paths; assertions depend on production outputs and refusals. The guide skeleton test is explicitly a documentation test.
3. **Silent empty return: PASS.** Empty registry, missing optional files/directories, ignored non-file directory entries during LFS size traversal, and the test-only malformed wait fallback have explicit documented semantics. Production read/decode/lock/write errors propagate.
4. **FR coverage: PASS for the supplied revised scope.** FR-001 is supported by the narrow entry-point diff and unchanged existing tests; FR-003 has production loader/validator/output assertions. Deferred save APIs are not represented as tested functionality.
5. **Frozen surface: PASS.** The two WP01 commits touch only the seven scoped files listed below; existing identity helpers, replication code, dependency metadata and pre-existing tests remain unchanged.
6. **Locked decision: PASS.** No added listener, root operation, configurable server home, non-standard dependency or session-mode write appears in the WP01 code. Protected-list ordering follows the explicit prompt resolution.
7. **Shared-file ownership: PASS with this coordination note.** `serve_commands.go`, `main.go` and `docs/serve-v0.md` are shared-lane integration surfaces. The supplied Activity Log explicitly moves `repo list|show|lock-exec` and `key list` into this cycle; WP05 must extend those existing handlers, and WP02 must integrate its writer with this dispatcher. Platform lock files have the prompt's one-line portability rationale. WP02 and WP05 must rebase or otherwise incorporate the WP01 correction before continuing.
8. **Production fragility: FAIL.** Active, legitimate cross-process hook writes cause false corruption errors, as independently reproduced above. Other fail-loud errors correspond to invalid inputs/state, unavailable operations or a real lock timeout.

## Scope and verification

WP01 scope: `main.go`, `serve_commands.go`, `serve_state.go`, `serve_state_test.go`, `docs/serve-v0.md`, plus justified platform files `serve_lock_unix.go` and `serve_lock_other.go`. The lane also contains WP03 and planning/coordination commits; those were not reviewed or approved as part of WP01.

The scoped diff is seven files and 3,498 insertions. The two platform lock files have the explicit portability rationale in the supplied Activity Log. Final lane HEAD remained `65e67fd`.

| Actual command/check | Observed result |
| --- | --- |
| `go test -race ./...` | FAIL: Go's default 10-minute timeout, `FAIL hubnot 600.259s`. The timeout identified `TestMemoryIndexTenThousandRecordsPerformanceAndRecovery` as running for 3m38s, with a stack in the existing memory-index query validation path. No WP01 implementation or test file modifies that path. This timeout is absent from the supplied baseline exemption and remains an unresolved full-suite gate; it is not claimed as a passing suite or attributed to WP01 without evidence. |
| `go test -race -run TestServeState ./...` | PASS: `ok hubnot 28.679s`; example package has no test files. Run after the full suite terminated, with no duplicate test runs in parallel. |
| `go vet ./...` | PASS, including a final rerun after the focused tests. |
| `go build ./...` | PASS. |
| `gofmt -l main.go serve_commands.go serve_state.go serve_state_test.go serve_lock_unix.go serve_lock_other.go` | PASS, no output, including final rerun. |
| `gofmt -l .` | PASS, no output. |
| `git diff --check` | PASS. |
| `GOOS=windows GOARCH=amd64 go build -o .review-wp01-9757e653/hn.exe .` | PASS, compilation only. |
| Independent built-CLI hostile-state fixtures | PASS: nine symlink/group-readable/FIFO cases across the three transient trees, plus a Unix socket. The initial socket harness used an overlong absolute address; retrying with a short relative bind address passed. |
| Independent built-CLI root/help fixtures | PASS: five root-refusal command shapes with unchanged HOME, ungated override ignored, no-argument usage and all three help lines. |
| Independent built-CLI concurrency probe | FAIL: 154/160 initialized-home calls, as detailed above; final isolated call passed. |

No implementation file was changed, no task status was moved, and no commit or push was performed. All reviewer-owned binaries and fixture directories under `.review-wp01-9757e653` were removed. Final lane status showed only the pre-existing untracked `.spec-kitty/` directory, which was preserved. The parent reviewer owns the required WP01 move-task and dependency notification. **Dependency notice: WP02 and WP05 must rebase or otherwise incorporate the WP01 correction before continuing.** Required feedback path: `kitty-specs/hn-serve-01M5P0NP/tasks/WP01-server-state-foundation/review-feedback-2.md`.
