# Decision Moment `01M5R5D0KF0C3VSZVSMJT1F6ZR`

- **Mission:** `hn-serve-01M5P0NP`
- **Origin flow:** `plan`
- **Slot key:** `plan.validation.destination-policy`
- **Input key:** `destination_policy_evaluation`
- **Status:** `resolved`
- **Created:** `2026-10-24T23:54:40.111167+00:00`
- **Resolved:** `2026-10-24T23:54:41.416525+00:00`
- **Resolved by:** `llxprt (maintainer-delegated planning)`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

How is a proposal evaluated under the destination policy when its signed decisions carry the base policy digest?

## Options

- Refactor evaluateProposal into a policy-parameterized core; destination evaluation applies destination thresholds and actor lists to the same signed facts, matching decisions by the base digest they were signed against
- Require decisions to be re-signed against the destination policy
- Other

## Final answer

Extract evaluateProposalUnderPolicy(proposal, events, policy, decisionDigest) from evaluateProposal; evaluateProposal keeps its exact behavior by passing the base policy and base digest. Destination evaluation passes the destination policy and the base digest: approvals count only destination trusted reviewers, CI counts only requests from destination maintainers with results from destination trusted runners for destination-required pipelines, and acceptances or rejections count only from destination maintainers.

## Rationale

Maintainers sign decisions against the base policy digest, so filtering by the destination digest would discard every acceptance whenever the policy changed. Applying destination actor lists and thresholds to the same signed facts implements C-004 and the edge case that evidence from actors removed from the destination policy does not count. Re-signing would change the wire format and the existing governance flow.

## Change log

- `2026-10-24T23:54:40.111167+00:00` — opened
- `2026-10-24T23:54:41.416525+00:00` — resolved (final_answer="Extract evaluateProposalUnderPolicy(proposal, events, policy, decisionDigest) from evaluateProposal; evaluateProposal keeps its exact behavior by passing the base policy and base digest. Destination evaluation passes the destination policy and the base digest: approvals count only destination trusted reviewers, CI counts only requests from destination maintainers with results from destination trusted runners for destination-required pipelines, and acceptances or rejections count only from destination maintainers.")
