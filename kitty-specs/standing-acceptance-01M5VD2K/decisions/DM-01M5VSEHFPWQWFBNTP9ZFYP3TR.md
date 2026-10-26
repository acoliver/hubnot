# Decision Moment `01M5VSEHFPWQWFBNTP9ZFYP3TR`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `plan`
- **Slot key:** `plan.quality.older-client-binary`
- **Input key:** `older_client_binary_lifetime`
- **Status:** `resolved`
- **Created:** `2026-10-26T09:42:44.982489+00:00`
- **Resolved:** `2026-10-26T09:42:44.985018+00:00`
- **Resolved by:** `maintainer (delegated remediation instruction)`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

Amends decision 01M5VF0QVPQ871M0DA8Q0SG5CX after analysis findings U1 and I1: how long does the older-client binary live, and what must the older-client tests show?

## Options

- One owning parent test builds it in its own temporary directory and runs every consumer as a subtest; no process-wide cache
- Process-wide sync.Once cache in the first caller temporary directory
- Other

## Final answer

Commit 7a47e6952a427ad7e3a3766c221fd334d9331950 stays the older-client reference. TestStandingCompatOlderClient builds it once with git archive and go build (GOTOOLCHAIN=local, GOFLAGS empty) into its own t.TempDir() and runs every older-client consumer (opt-out comparison, policy fields, merge facts, legacy history) as a subtest, so the binary outlives all consumers and is removed after the last one; there is no process-wide cache. Each subtest checks that the binary exists before use. It skips with a visible reason under -short and when the commit object or go is absent. The gate runs the whole parent without -short. The tests show that older and new clients agree on every fact that passes the maintainer check, including a rule-plus-maintainer-decisions fact that both accept, accept the same signed legacy events with unknown or rule-named fields, and that the older client rejects facts valid only through the rule path.

## Rationale

A sync.Once cache that points into the first caller t.TempDir() is deleted by that test cleanup before later consumers run. One owning parent test ties the binary lifetime to its consumers.

## Change log

- `2026-10-26T09:42:44.982489+00:00` — opened
- `2026-10-26T09:42:44.985018+00:00` — resolved (final_answer="Commit 7a47e6952a427ad7e3a3766c221fd334d9331950 stays the older-client reference. TestStandingCompatOlderClient builds it once with git archive and go build (GOTOOLCHAIN=local, GOFLAGS empty) into its own t.TempDir() and runs every older-client consumer (opt-out comparison, policy fields, merge facts, legacy history) as a subtest, so the binary outlives all consumers and is removed after the last one; there is no process-wide cache. Each subtest checks that the binary exists before use. It skips with a visible reason under -short and when the commit object or go is absent. The gate runs the whole parent without -short. The tests show that older and new clients agree on every fact that passes the maintainer check, including a rule-plus-maintainer-decisions fact that both accept, accept the same signed legacy events with unknown or rule-named fields, and that the older client rejects facts valid only through the rule path.")
