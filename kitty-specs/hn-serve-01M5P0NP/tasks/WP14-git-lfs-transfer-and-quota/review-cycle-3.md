---
affected_files: []
cycle_number: 3
mission_slug: hn-serve-01M5P0NP
reproduction_command:
reviewed_at: '2026-10-25T17:54:18Z'
reviewer_agent: user
wp_id: WP14
---

# WP14 review cycle 3: changes requested

## Blocking finding: a stored OID bypasses the upload staging bound

**Priority:** P1, authenticated storage exhaustion.

**Location:** `serve_lfs.go:649-666`, especially the `if !present` guard at line 653. Related inaccurate security claim: `docs/threat-model.md:416-419`.

`putObject` skips its pre-data quota check whenever the supplied OID is already stored, but still calls `receiveStaged` with the client's unrestricted declared size. Knowing any stored OID lets a mapped actor declare an arbitrarily large size and stream that many bytes into a new staging file. The N+1 read cap uses that attacker-selected size. Content verification happens only after the body ends, so the eventual hash mismatch does not prevent disk exhaustion. This requires one connection and no simultaneous uploads. Disk exhaustion can affect the server while the upload is active, even if eventual disconnect cleanup succeeds.

### Reproduction

1. Create a hosted repository and upload the one-byte content `x`. Its OID is `2d711642b726b04401627ca9fbac32f5c8530fb1903cc4db02258717921a4881`.
2. Set that repository's LFS quota to 100 bytes with `hn serve repo set r --lfs-quota 100`.
3. Open the normal forced-command path with a mapped key and `SSH_ORIGINAL_COMMAND="git-lfs-transfer 'r.git' upload"`. Negotiate `version 1`.
4. Send pkt-lines for `put-object <stored OID>`, `size=1048576`, delim, and 65,515 bytes of `z` as ordinary legal data packets. Leave the upload unfinished.
5. Inspect `state/repos/r/lfs/incoming/`. The server writes the body to a new staged file despite the 100-byte quota. Increasing the declared size permits arbitrarily more staging.

Independent deterministic session-boundary reproduction observed:

```text
missing OID control: quota=100 declared=1048576 partial-body=65515 staged=0 status=1 err=<nil>
stored OID:          quota=100 declared=1048576 partial-body=65515 staged=65515 status=1 err=<nil>
```

The additional live `hn serve git` reproduction observed `staged=12288` while the write was in progress, already above the quota, and failed its security assertion. Both tests used scratch-only Go overlays and unchanged production code/tests.

Evidence:

- `/home/acoliver/projects/appaday/gitium/.scratch/wp/hn-serve-01M5P0NP/WP14/review-3-quota-reproduction.log`
- `/home/acoliver/projects/appaday/gitium/.scratch/wp/hn-serve-01M5P0NP/WP14/review-3-quota-live.log`

### Expected behavior and fix

Preserve successful retries of a correctly declared stored object without charging its bytes twice, while ensuring an existing OID cannot authorize an arbitrarily large new staged file. For example, validate a duplicate's declared size against the stored object's actual size before staging, or handle stored-object retries through a bounded verification/discard path that creates no staged file. Reject inconsistent or over-quota requests before writing their bodies; end the session when safely draining is impossible. Keep the N+1 read cap and the final locked publication check.

Add a regression exercising an already-stored OID with a declared size above quota, including an unfinished body and observation of staging. Retain tests proving valid duplicates succeed at the quota and that the original stored object is unchanged. Ensure the guide and threat model accurately describe the resulting boundary. This addresses T088/T089 and the claimed FR-021 pre-upload checkpoint. The stored-object publication quota itself was not bypassed in the reproduction.

## Other review results and coordination

The prior cycle's N+1 read issue is fixed in `158e471`. The new reader performs no read-ahead and caps a crossing packet's payload read to the remaining allowance plus one byte.

WP14 implementation ownership was isolated to `0c29ad5`, `f953089`, and `158e471`, against implementation base `584f8780ca71d3f95b00aab9bd94a44bf7e6d30b`; reviewed lane HEAD was `f2fbb30158611615ea46071aada1ac758fd71231`. No pre-WP14 test assertions or dependencies changed.

Explicit shared-file coordination note: WP14 replaces WP06's reserved LFS refusal arm and enablement flag in `serve_session.go`; it fills WP13's LFS guide/threat-model placeholders and updates LFS traceability, skip-rule documentation, and existing overview references to reflect enablement. These changes stay in WP14's five owned files and preserve its separability. This continues the coordination recorded in `review-feedback-1.md`.

Full requirement coverage, all eight anti-pattern results, exact completed quality-gate results, cleanup, and remaining limitations are recorded in the final `review-3-verdict.md` under the scratch evidence directory. The parent agent must record feedback and perform the mission-scoped status transition. The delegated reviewer does not commit or move the task.

## Completed quality gates

All mandatory gates passed on the reviewed head: gofmt printed nothing for all three changed Go files; `go vet ./...` and `go build ./...` exited 0 with empty output; the exact mandatory race suite exited 0 with `ok hubnot 1013.832s`; the exact isolated non-race performance command exited 0 with `ok hubnot 18.254s`. No base comparison was necessary. Logs are `review-3-gofmt.log`, `review-3-vet.log`, `review-3-build.log`, `review-3-race.log`, and `review-3-performance.log` in the scratch evidence directory.

The sequential focused race run passed all 16 LFS tests and both documentation guards with no skips (`ok hubnot 10.145s`), including the enforced bubblewrap write boundary and both real-client/marked-example paths. See `review-3-focused.log`. These passing tests do not exercise the independently reproduced stored-OID staging attack.

The review-only test overlay directory was removed. No temporary worktree was created, no implementation/pre-existing tests were edited, and the parent retains responsibility for committing feedback and executing the mandatory mission-scoped move-task command.
