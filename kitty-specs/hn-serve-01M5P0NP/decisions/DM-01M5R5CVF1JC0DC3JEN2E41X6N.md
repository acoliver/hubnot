# Decision Moment `01M5R5CVF1JC0DC3JEN2E41X6N`

- **Mission:** `hn-serve-01M5P0NP`
- **Origin flow:** `plan`
- **Slot key:** `plan.protocol.advancement-request`
- **Input key:** `advancement_request`
- **Status:** `resolved`
- **Created:** `2026-10-24T23:54:34.849289+00:00`
- **Resolved:** `2026-10-24T23:54:36.177211+00:00`
- **Resolved by:** `llxprt (maintainer-delegated planning)`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

What is the wire shape of an advancement request and how does the client record the merge fact (FR-014, FR-015)?

## Options

- hn merge PROPOSAL --remote REMOTE [--branch BRANCH]: publish own actor chain, then a forced single-ref push of the head with push options hn-advance and hn-expected-tip, then sign proposal.merged with commit equal to head
- Single atomic push carrying the actor chain and the branch update
- Other

## Final answer

hn merge PROPOSAL --remote REMOTE [--branch BRANCH, default main]: verify the remote identifies as hn serve; push refs/hn/actors/SELF; read the remote tip T; if head equals or is an ancestor of T run the fast-forward repair path; otherwise push +HEAD_OID:refs/heads/BRANCH with --push-option=hn-advance=CANDIDATE and --push-option=hn-expected-tip=T; on acceptance confirm the tip, sign proposal.merged with commit=head and head=head, and publish the actor chain again

## Rationale

Two pushes keep valid facts on the server even when advancement is refused (FR-014), which an all-or-nothing single push cannot. hn-expected-tip lets the server name protected-stale-tip when another advancement won the race (US3-6, NFR-005), because the forced refspec stops client-side git from pre-empting the server verdict. Without --remote, hn merge keeps its --no-ff path unchanged (FR-016). Existing merge-fact validation binds only subject, head, policy and evidence, so commit=head validates without a protocol change.

## Change log

- `2026-10-24T23:54:34.849289+00:00` — opened
- `2026-10-24T23:54:36.177211+00:00` — resolved (final_answer="hn merge PROPOSAL --remote REMOTE [--branch BRANCH, default main]: verify the remote identifies as hn serve; push refs/hn/actors/SELF; read the remote tip T; if head equals or is an ancestor of T run the fast-forward repair path; otherwise push +HEAD_OID:refs/heads/BRANCH with --push-option=hn-advance=CANDIDATE and --push-option=hn-expected-tip=T; on acceptance confirm the tip, sign proposal.merged with commit=head and head=head, and publish the actor chain again")
