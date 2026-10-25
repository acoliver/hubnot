# WP03 independent review: changes requested

Reviewed as `llxprt` in the assigned lane at `4ad06d5184decceff5e510cacfa8d79586ddb485`. Implementation was not edited. No move-task command was run.

## Blocking issue 1: uncoordinated change outside WP03 ownership

**Locations:** `commands.go:139-140`, `commands.go:148-149`, and `commands.go:160-161`, introduced by commit `717d110`.

WP03 owns only `serve_reasons.go`, `serve_wire.go`, `serve_reasons_test.go`, and the new section in `docs/protocol-v0.md`. The authoritative lane map confirms that list at `lanes.json:13-18` and assigns `commands.go` to lane-c at `lanes.json:29-35`. This revision changes all three publication push call sites in that file. The visible WP03 Activity Log and prior review feedback contain no coordination agreement for this shared-file change; the implementation commit explains the integration but does not record coordination with lane-c.

**Violated criteria:** the explicit review requirement that changes stay inside WP-owned files; WP03 frontmatter `owned_files` and Ownership table; anti-pattern checklist item 7, Shared-file ownership, requiring an explicit coordination note for cross-WP shared-file edits.

**Reproduction, from the assigned lane:**

```sh
git show --format= --name-only 717d110
git diff kitty/mission-hn-serve-01M5P0NP..HEAD -- commands.go
```

Compare the resulting `commands.go` change with the WP03 ownership list and `kitty-specs/hn-serve-01M5P0NP/lanes.json` in the primary checkout. The implementation changes five source/documentation files, while WP03 currently owns four.

**Required remediation:** coordinate the minimal `publishLocalFacts` integration with the lane-c owner and have the coordinator explicitly amend WP03's authorized ownership/write scope and record the shared-file agreement in the review or move-task record. Alternatively, land that integration through the owning lane and rebase WP03 onto it before re-review. Preserve the live integration and its behavioral tests, which resolve the previous review's reachability finding. No implementation change for style is requested.

## Blocking issue 2: the required full race gate did not pass

**Reproduction:** `go test -race ./...` in the assigned lane, with no implementation edits.

The managed run exited 1 after 600.155 seconds for `hubnot`; `hubnot/examples/issue-consumer` has no tests. It reported:

```text
--- FAIL: TestMemoryIndexTenThousandRecordsPerformanceAndRecovery (221.16s)
    memory_index_test.go:1078: 10k production rebuild/persist: 7.884053175s
    memory_index_test.go:1119: 10k exact-plus-lexical query p95 took 2.293283761s
panic: test timed out after 10m0s
```

`memory_index_test.go:1118-1119` requires query p95 below one second. At the package timeout, `TestOperationalSelfHostingAlphaCrashRecoveryDeniesUnacceptedFacts/corrupt_receipt` was running; its stack was waiting for a subprocess at `operational_acceptance_test.go:1227`. This does not prove that subtest caused the timeout.

**Violated criteria:** WP03 Definition of Done, “T014 to T019 complete; full gate passes”; the supplied baseline permits only the missing JUnit artifact failure and does not list either observed failure. The mission regression gate is NFR-007.

These existing test files are unchanged by WP03. The run does not establish that WP03 caused the performance failure or timeout. Host contention is a possible explanation and has not been proven. Obtain a passing full race run with sufficient exclusive test resources, or investigate and document the reproducible cause before resubmission. Preserve the existing assertions and time limits; do not suppress or weaken them to pass the gate. An initial foreground attempt was terminated by the shell's 120-second inactivity guard; the completed managed run above is the authoritative result.

## Requirement and acceptance evidence

- **T014 / FR-013: PASS at WP foundation scope.** All 27 constants, catalogue meanings and default-action templates match the push-verdict contract. Missing kinds have fixed ordering. Catalogue and all-code round-trip tests exercise production functions.
- **T015 / FR-013: PASS.** Renderer produces two newline-terminated lines, sanitizes bytes and token separators, fixes evidence ordering, preserves stable byte-order sorting by original ref, and rejects documented programming errors. Golden bytes match the authoritative worked example exactly. Independent overlay probes verified odd values and round trips without editing implementation.
- **T016 / FR-013: PASS.** Parser rejects unsupported prefixes/codes/fields, duplicates, out-of-order fields, invalid evidence, invalid decimals, invalid budget combinations and malformed quoted next actions. Extraction handles Git prefixes, padding, CRLF, progress lines and multiple refs.
- **T017 / FR-017 identification: PASS.** Exact marker ref, exact 11-byte blob, push-option constants and no-write hashing are present. Real temporary repositories cover SHA-1, SHA-256, mismatched formats, missing and wrong markers and transport failures. Raw-advertisement tests reject extra lines/spacing and valid-looking output with failing transport status. Identification grants no trust. Git environment and timeout behavior match the existing replication helper, which also has no explicit timeout.
- **T018: PASS for focused protocol tests; full gate FAIL as above.** `go test -race -run 'TestServeReasons|TestServeWire' ./...` exited 0: `ok hubnot 4.479s`; the example package has no tests. Tests exercise production render/parse/identification and the public `run` dispatcher with real Git push rejection hooks, rather than asserting only fabricated output.
- **T019 / C-008: PASS.** The sole documentation change is the new “hn serve wire surface” section before Known limits. It documents marker/trust, both push options and restrictions, grammar/versioning, every sanitizer rule including backslash, ordering, sentence handling, budgets, all 27 codes and live sync behavior. Removing that inserted section reproduces the base document exactly. A disposable Go overlay added an undocumented catalogue code: `TestServeReasonsDocsTableMatchesCatalogue` failed as expected, exit 1, specifically identifying the extra code. The source was never mutated on disk.
- **Live path: PASS at this WP stage.** `main -> run("sync") -> cmdSync -> publishLocalFacts -> servePushRef -> extractServeRejections/parseServeRejectionLine -> serveRemoteIdentifies -> serveMarkerObjectID`; error printing invokes `serveRejectedPushError.Error -> renderServeRejections -> renderServeRejection`. Later server hook, administration and client-mode integrations remain downstream WP responsibilities. Both new production modules have live callers.
- **Existing assertions: PASS.** None of the 31 test files present at the authorized base changed. Only the new `serve_reasons_test.go` is added.
- **Diff/ownership: FAIL only for issue 1.** Implementation commits `8bbcf14` and `717d110` change the four declared deliverables plus `commands.go`. Planning/status history is excluded from implementation attribution as instructed. No private keys, generated binaries or frozen fixture files occur in those implementation commits.

## Other observed verification results

- `go vet ./...`: PASS, exit 0.
- `git ls-files -z -- '*.go' | xargs -0 gofmt -l`: PASS, exit 0 and no output, covering all 63 tracked Go files. No implementation formatting edits were made.
- `find . -type f -name '*.go' -not -path './.git/*' -print0 | xargs -0 gofmt -l`: only pre-existing ignored `.review-wp03/adversarial_test.go` and `.review-wp03/catalogue-mutation.go` were reported. Those artifacts were preserved.
- `go build -o .review-wp03-cycle2/build/ ./...`: PASS, exit 0. Disposable build output is removed after review.
- `git diff --check kitty/mission-hn-serve-01M5P0NP..HEAD -- commands.go docs/protocol-v0.md serve_reasons.go serve_reasons_test.go serve_wire.go`: PASS. The unrestricted planning-inclusive diff reports Markdown trailing whitespace in inherited `kitty-specs/hn-serve-01M5P0NP/spec.md:3-5`; this is outside WP03 implementation attribution and is not a new style blocker.
- `go test -overlay .review-wp03-cycle2/probe-overlay.json -run '^TestWP03Independent' -count=1 -v .`: PASS, exit 0, two independent production-function probes.
- `go test -overlay .review-wp03-cycle2/mutation-overlay.json -run '^TestServeReasonsDocsTableMatchesCatalogue$' -count=1 .`: expected FAIL, exit 1, proving the docs/catalogue drift guard detects an actual source mutation.

## Anti-pattern checklist

1. Dead code: **PASS at WP foundation scope**. Both modules reach the production sync error path; downstream-only wire helpers/constants retain the required stable foundation API.
2. Synthetic-fixture test: **PASS**. Behavioral assertions invoke the production implementations, including real Git and public command dispatch.
3. Silent empty return: **PASS**. Empty lookups/no-rejection results and successful nil returns have documented contracts; failed identification returns false with a diagnostic.
4. FR coverage: **PASS at WP scope**. FR-013 and FR-017 have substantive behavior assertions; C-008 has the documentation consistency test and mutation evidence.
5. Frozen surface: **PASS**. Implementation commits touch no frozen evidence/fixture/private-key surface.
6. Locked decision: **PASS**. No forbidden trust grant, signing, server merge, dependency addition or ordinary-remote mode change found in the reviewed implementation.
7. Shared-file ownership: **FAIL**, issue 1.
8. Production fragility: **PASS**. Renderer panics are documented and tested programming-error cases. The strict parser prevents malformed remote data from entering those panic cases.

## Dependent-agent notification and handoff

WP05 in lane-a and WP11 in lane-c depend on WP03. WP03 has changes requested. Preserve existing work and rebase your own lanes after the WP03 ownership/integration resolution lands. The lane-c owner also needs the explicit `commands.go` coordination agreement above.

This notification is recorded here and returned to the parent reviewer. No direct inter-agent messaging tool is available in this review session. The parent must deliver it to the dependent agents. No other WP status has been inspected or changed. The parent retains responsibility for committing this feedback and performing the final move:

```sh
spec-kitty agent tasks move-task WP03 --to planned --review-feedback-file /home/acoliver/projects/appaday/gitium/hubnot/kitty-specs/hn-serve-01M5P0NP/tasks/WP03-rejection-protocol-and-marker/review-feedback-2.md --mission hn-serve-01M5P0NP
```
