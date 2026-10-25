# WP03 review: changes requested

## Blocking findings

1. **The new wire implementation is not reachable from any live `hn` production command path.** The implementation commit adds only `serve_reasons.go`, `serve_wire.go`, their tests, and protocol documentation. Searching production `*.go` call sites finds no callers of `renderServeRejection`, `renderServeRejections`, `extractServeRejections`, `serveRemoteIdentifies`, or `serveMarkerObjectID`; every use outside the new definitions is in `serve_reasons_test.go`. This leaves the rejection renderer and parser unintegrated with server hooks and push clients, and leaves marker identification unintegrated with replication selection or merge commands. It violates the review requirement to verify that new code is reachable from live `hn` command paths and the anti-pattern checklist's dead-code rule. Add production call paths through the actual hook/client entry points, or revise the WP boundaries/dependencies so these wire functions are not delivered as completed production behavior until those integrations land. Add behavior tests through the resulting public command paths. Reproduction: from the lane, `grep -R -n -E 'renderServeRejection|renderServeRejections|extractServeRejections|serveRemoteIdentifies|serveMarkerObjectID' --include='*.go' .` shows only definitions and `serve_reasons_test.go` references.

2. **The new protocol documentation omits the backslash sanitization rule.** `docs/protocol-v0.md:361-365` says `"` and then has an empty inline-code span for the second character, so it does not document that backslashes in `NEXT` are replaced with `?`. `serve_reasons.go:205-209` does perform that replacement, and the work-package requirements explicitly require documenting it. Correct the text to name both characters unambiguously (for example, quote the backslash in a fenced or otherwise unambiguous representation) and keep it covered by documentation checks.

## Requirement and acceptance mapping

- **FR-013:** The commit defines 27 reason codes and supplies renderer, parser, stderr extraction, golden and round-trip tests, and a docs-table consistency test. Code-level wire coverage is substantial. However the core functions have no production call sites, so the requested behavior is not available from live hooks/clients; see finding 1. The doc sanitizer description also misses backslash; see finding 2.
- **FR-017:** Marker ref/content, blob hashing without writing, and remote identification are implemented with SHA-1/SHA-256 temporary-repository tests, wrong-marker, absent-marker, transport failure and argument-injection cases. `serveRemoteIdentifies` has no production caller, so identification does not reach a live command path; see finding 1.
- **C-008:** `docs/protocol-v0.md` adds the requested section before Known limits, covering marker, push options, line shape, ordering, reason table and budget fields. The backslash sanitization omission is a documentation gap; see finding 2.
- **T014-T019:** Catalogue and default actions; renderer/sanitization/sorting; strict parser and stderr extraction; marker and push-option constants and identification; protocol tests; protocol section and catalogue drift test are present. Integration into executable paths is absent.
- **No pre-existing test assertion changed:** the implementation commit adds only new files and one documentation section; it does not modify existing test files or assertions.
- **Owned-file scope:** implementation commit `8bbcf14` touches exactly the four owned deliverables (`serve_reasons.go`, `serve_wire.go`, `serve_reasons_test.go`, `docs/protocol-v0.md`).

## Anti-pattern checklist

1. Dead code: **FAIL**. New production utilities have no production callers.
2. Synthetic-fixture test: **PASS** for the marker and rejection behaviors; tests exercise the actual functions. This does not repair the missing live command integration.
3. Silent empty return: **PASS**. `serveReasonDefaultNext` returns an empty string for unknown codes by documented design; render rejects unknown codes. No unexplained silent-failure path found.
4. FR coverage: **PASS at unit level**, but **FAIL as live-path coverage** for the same missing production integrations above.
5. Frozen surface: **PASS**. The implementation commit changes no spec/contract-frozen file.
6. Locked decision: **PASS**. No MUST NOT violation found in the reviewed owned diff.
7. Shared-file ownership: **N/A**. The implementation commit changes only WP03-owned files; no shared source file is changed.
8. Production fragility: **N/A**. No new `raise` or equivalent request-handler panic path is present; render panics only on documented programming errors.

## Fresh verification

The independent delegated reviewer ran these commands in the assigned lane:

- `go test -race ./...`: PASS, exit 0 (hubnot 283.107s).
- `go vet ./...`: PASS, exit 0.
- `gofmt -l *.go`: PASS, no output for root Go sources. A broader `gofmt -l .` also encountered pre-existing ignored `.review-wp03` artifacts, which were preserved.
- `git diff --check 8bbcf14^ 8bbcf14`: PASS.

## Dependent-agent notice

WP05 (lane-a) and WP11 (lane-c): WP03 has changes requested. Preserve existing work and rebase your own lane after WP03 remediation lands:

```sh
git rebase kitty/mission-hn-serve-01M5P0NP-lane-b
```

No direct agent-message channel was available; this notice is recorded with the review feedback and surfaced in the final review result. No other WP status was changed.
