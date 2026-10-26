# Decision Moment `01M5VWE2Q3AVA8NPGRCSKBFW5M`

- **Mission:** `standing-acceptance-01M5VD2K`
- **Origin flow:** `plan`
- **Slot key:** `plan.quality.semantic-row-controls`
- **Input key:** `semantic_row_valid_controls`
- **Status:** `resolved`
- **Created:** `2026-10-26T10:34:55.587793+00:00`
- **Resolved:** `2026-10-26T10:34:55.590551+00:00`
- **Resolved by:** `maintainer (delegated remediation instruction)`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

How is the matched valid control built for semantic defect rows whose defect lies in the candidate itself (F15 to F18), where no change to the fact can remove it (analysis round 2 finding U2)?

## Options

- Same candidate with the defect removed for fact-only rows; a corresponding valid candidate with rebound reviews, requests, results and merge fact for F15 to F18
- Same candidate for every row
- Other

## Final answer

Fact-only semantic rows (F01 to F14, F19, F20) use the valid rule merge fact for the same candidate with the defect removed. Candidate-shape rows (F15 to F18) use a corresponding valid candidate built from the same base and files with the defect removed (an allowed author for F15, only allowlisted paths for F16, the src/ change without the manual-path file for F17 and F18), with its reviews, CI requests, run results and merge fact rebound to that candidate. Each control uses the same selection and hydration strategy as its row; both run with a complete closure; the row must still name its intended check, and a refusal through a dependency gap never counts as semantic success. WP06 supplies a Control builder per row; WP09 T041, WP16 T076 and contracts/rule-merge-fact.md use it.

## Rationale

F15 to F18 carry their defect in the signed candidate (author, base and head), so no evidence-only repair yields a valid path (b) fact for the same candidate. Refines the matched-control clause of decision 01M5VSEHFV2K03JJY0NN6DFNAT.

## Change log

- `2026-10-26T10:34:55.587793+00:00` — opened
- `2026-10-26T10:34:55.590551+00:00` — resolved (final_answer="Fact-only semantic rows (F01 to F14, F19, F20) use the valid rule merge fact for the same candidate with the defect removed. Candidate-shape rows (F15 to F18) use a corresponding valid candidate built from the same base and files with the defect removed (an allowed author for F15, only allowlisted paths for F16, the src/ change without the manual-path file for F17 and F18), with its reviews, CI requests, run results and merge fact rebound to that candidate. Each control uses the same selection and hydration strategy as its row; both run with a complete closure; the row must still name its intended check, and a refusal through a dependency gap never counts as semantic success. WP06 supplies a Control builder per row; WP09 T041, WP16 T076 and contracts/rule-merge-fact.md use it.")
