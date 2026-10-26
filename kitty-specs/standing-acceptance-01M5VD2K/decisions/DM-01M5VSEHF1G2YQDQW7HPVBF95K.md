# Decision Moment `01M5VSEHF1G2YQDQW7HPVBF95K`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `plan`
- **Slot key:** `plan.protocol.rule-claim`
- **Input key:** `rule_claim_parsing`
- **Status:** `resolved`
- **Created:** `2026-10-26T09:42:44.961482+00:00`
- **Resolved:** `2026-10-26T09:42:44.963608+00:00`
- **Resolved by:** `maintainer (delegated remediation instruction)`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

Amends decision 01M5VEZKK32DXWNPRAYGDMY3ZK under decision A: how is the rule field of proposal.merged represented and read without changing the validity of any previously valid event?

## Options

- Rule held outside encoding/json, set only from a canonical claim on proposal.merged, no refusing scan
- Event.Rule with a json tag plus a refusing top-level key scan
- Other

## Final answer

Event gains Rule string with the tag json:"-", so encoding/json never reads or writes it and every payload decodes and re-encodes exactly as before the mission, including resource kinds, whose strict decoder still refuses an unknown rule key. verifyEvent keeps every existing check and, only for proposal.merged, calls parseMergeRuleClaim, a single top-level token walk that sets Event.Rule when the claim is canonical (exactly one key equal to rule under Unicode case folding, spelled exactly rule, whose value is a JSON string accepted by validPipelineName). Every other shape (absent, null, empty, non-string, case variant, duplicate key, invalid name) leaves Rule empty and is never an error, so such a fact can be valid only through the unchanged maintainer check. Other kinds ignore rule keys as before. encodeAndSign refuses to sign a non-empty Rule on any kind other than proposal.merged or with an invalid name, and emits rule after every other field, so Hubnot only ever writes canonical claims. No new event kind and no protocol version change.

## Rationale

Decision A forbids an unconditional scan for a rule key and requires previously valid signed history to stay valid. Keeping Rule out of encoding/json also stops a typed field from turning "rule": 1 into a decode error on a fact that is valid today.

## Change log

- `2026-10-26T09:42:44.961482+00:00` — opened
- `2026-10-26T09:42:44.963608+00:00` — resolved (final_answer="Event gains Rule string with the tag json:"-", so encoding/json never reads or writes it and every payload decodes and re-encodes exactly as before the mission, including resource kinds, whose strict decoder still refuses an unknown rule key. verifyEvent keeps every existing check and, only for proposal.merged, calls parseMergeRuleClaim, a single top-level token walk that sets Event.Rule when the claim is canonical (exactly one key equal to rule under Unicode case folding, spelled exactly rule, whose value is a JSON string accepted by validPipelineName). Every other shape (absent, null, empty, non-string, case variant, duplicate key, invalid name) leaves Rule empty and is never an error, so such a fact can be valid only through the unchanged maintainer check. Other kinds ignore rule keys as before. encodeAndSign refuses to sign a non-empty Rule on any kind other than proposal.merged or with an invalid name, and emits rule after every other field, so Hubnot only ever writes canonical claims. No new event kind and no protocol version change.")
