# Decision Moment `01M5R5EEKH2R8NQ1VV3DAR0BAF`

- **Mission:** `hn-serve-01M5P0NP`
- **Origin flow:** `plan`
- **Slot key:** `plan.security.session-dispatch`
- **Input key:** `session_dispatch`
- **Status:** `resolved`
- **Created:** `2026-10-24T23:55:27.217943+00:00`
- **Resolved:** `2026-10-24T23:55:28.668766+00:00`
- **Resolved by:** `llxprt (maintainer-delegated planning)`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

How does hn serve git parse SSH_ORIGINAL_COMMAND and build the environment for git and hooks (FR-005, NFR-003, NFR-004)?

## Options

- Strict grammar for git-upload-pack, git-receive-pack, git upload-pack, git receive-pack and git-lfs-transfer with one single-segment repository name; clean allowlisted environment; HN_SERVE_* set only by the wrapper
- Pass SSH_ORIGINAL_COMMAND to a shell
- Other

## Final answer

Accepted grammar: COMMAND SP QUOTED-PATH [SP (upload|download)] where COMMAND is git-upload-pack, git-receive-pack, git upload-pack, git receive-pack or git-lfs-transfer (the operation argument only for git-lfs-transfer). QUOTED-PATH is an optionally single-quoted, optionally leading-slash, single segment matching [A-Za-z0-9][A-Za-z0-9._-]{0,99} with an optional .git suffix, never .. and never a symlink, and it must be registered in server state. Anything else is refused with unsupported-command or unhosted-repository before git starts. The wrapper runs git directly (no shell) with an environment built from scratch: PATH, HOME, LANG=C, GIT_PROTOCOL only when it matches version=[0-9], HN_SERVE_ACTOR, HN_SERVE_KEY, HN_SERVE_REPOSITORY. Configuration such as core.hooksPath and receive.advertisePushOptions is passed with -c flags. Every hn serve subcommand refuses to run as uid 0.

## Rationale

A closed grammar with no shell removes metacharacter and argument-injection classes listed in the edge cases. Rebuilding the environment means a client-sent variable cannot forge the mapped actor that hooks read. Passing configuration with -c means a hosted repository's own config cannot disable the hooks.

## Change log

- `2026-10-24T23:55:27.217943+00:00` — opened
- `2026-10-24T23:55:28.668766+00:00` — resolved (final_answer="Accepted grammar: COMMAND SP QUOTED-PATH [SP (upload|download)] where COMMAND is git-upload-pack, git-receive-pack, git upload-pack, git receive-pack or git-lfs-transfer (the operation argument only for git-lfs-transfer). QUOTED-PATH is an optionally single-quoted, optionally leading-slash, single segment matching [A-Za-z0-9][A-Za-z0-9._-]{0,99} with an optional .git suffix, never .. and never a symlink, and it must be registered in server state. Anything else is refused with unsupported-command or unhosted-repository before git starts. The wrapper runs git directly (no shell) with an environment built from scratch: PATH, HOME, LANG=C, GIT_PROTOCOL only when it matches version=[0-9], HN_SERVE_ACTOR, HN_SERVE_KEY, HN_SERVE_REPOSITORY. Configuration such as core.hooksPath and receive.advertisePushOptions is passed with -c flags. Every hn serve subcommand refuses to run as uid 0.")
