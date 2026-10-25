# Contract: hn serve command surface

`hn serve` subcommands run as the server's Unix user. Every subcommand refuses to run when the effective uid is 0, before reading any state, with `hn: hn serve refuses to run as root; create an unprivileged user for the server` and exit status 1 (NFR-004). Every subcommand validates the whole server state layout first and refuses to run on any defect listed in [server-state.md](server-state.md) (NFR-008). Errors go to stderr prefixed with `hn: ` and exit 1, matching `main.go`.

## Administration

The administrator's commands are listed below with their observable effects. Each command that changes the key registry rewrites the managed `authorized_keys` block in the same invocation (FR-004).

```text
hn serve repo create NAME --from SOURCE [--protect REF]... [--lfs-quota BYTES]
hn serve repo list
hn serve repo show NAME
hn serve repo set NAME [--protect REF]... [--unprotect REF]... [--lfs-quota BYTES]
                       [--max-events N] [--max-objects N] [--max-object-bytes N]
                       [--max-attachment-bytes N] [--max-total-bytes N]
hn serve repo lock-exec NAME -- COMMAND [ARG]...
hn serve key add --actor ACTOR KEYFILE|-
hn serve key remove FINGERPRINT
hn serve key list
```

`repo create` fetches `refs/heads/*` and `refs/tags/*` from `SOURCE` (a path or URL that `git fetch` accepts) into a new bare repository `repositories/NAME.git`, writes `refs/hn/serve/v1`, sets `HEAD` to the first protected ref, and writes `state/repos/NAME/config.json`. `--protect` defaults to `refs/heads/main` and may be given as a short branch name (`main`) or full ref. Creation is refused, and nothing is left behind, when `NAME` is invalid or exists, when a protected branch is absent from the import, or when a protected tip lacks a valid `.hn/policy.json`. Output on success:

```text
Created hosted repository NAME
Protected: refs/heads/main
LFS quota: 1073741824 bytes
Clone URL: ssh://USER@HOST/NAME.git
```

`repo list` prints one `NAME` per line in byte order. `repo show NAME` prints `name:`, `path:`, one `protected:` line per ref, `lfs-quota:`, `lfs-stored:` and one line per budget using the same names as `hn replication show`. `repo set` applies the same protected-tip rule as `repo create` to each newly protected ref and refuses to unprotect the last protected ref.

`repo lock-exec` takes the repository lock, runs `COMMAND` directly (no shell) with the working directory set to the bare repository and `GIT_DIR` set to it, releases the lock, and exits with the command's status. It exists for the documented recovery of a protected tip without valid policy (OQ-11).

`key add` reads one OpenSSH public key line from `KEYFILE` or stdin, ignores options and the comment, and refuses a malformed key, an unsupported type, a short or malformed `--actor`, or a key already bound. Output: `Bound SHA256:... to actor <64-hex>`. `key remove` requires the full fingerprint and refuses unknown fingerprints. `key list` prints `FINGERPRINT ACTOR KEYTYPE` per binding, sorted by fingerprint.

## Forced command

```text
hn serve git --key FINGERPRINT
```

The generated `authorized_keys` entry runs this command. It reads the client request from `SSH_ORIGINAL_COMMAND` and exits non-zero without starting Git when any check fails. Refusals print one `hn-serve/1` rejection line (format in [push-verdict.md](push-verdict.md)) with `ref=-` and one sentence to stderr.

| Check, in order | Refusal code |
|----|----|
| `--key` is a syntactically valid fingerprint bound in the registry | `unknown-key` |
| `SSH_ORIGINAL_COMMAND` is set and matches the grammar below | `unsupported-command` |
| The named repository is registered, its directory is a real directory below `repositories/`, and its state is valid | `unhosted-repository` |
| For pushes, the repository lock is acquired within 120 seconds | `repository-busy` |

Grammar, with `SP` a single space and no other whitespace permitted:

```text
request     = git-command SP path / lfs-command SP path SP lfs-op
git-command = "git-upload-pack" / "git-receive-pack" / "git upload-pack" / "git receive-pack"
lfs-command = "git-lfs-transfer"
lfs-op      = "upload" / "download"
path        = "'" name-path "'" / name-path
name-path   = ["/"] name [".git"]
name        = ALNUM *99(ALNUM / "." / "_" / "-")   ; never contains ".."
```

`git-lfs-authenticate`, shells, `scp`, `sftp`, extra arguments, double quotes, backslashes, `$`, backticks, `;`, `|`, `&`, newlines and any path with more than one segment are refused with `unsupported-command`. A name that matches the grammar but is not hosted, or whose directory is a symlink, is refused with `unhosted-repository`.

### Execution

For `git-upload-pack` the wrapper runs:

```text
git upload-pack --strict REPO
```

For `git-receive-pack` it takes the lock and runs:

```text
git -c core.hooksPath=$HOME/.hn-serve/hooks
    -c receive.advertisePushOptions=true
    -c receive.fsckObjects=true
    receive-pack REPO
```

For `git-lfs-transfer` it runs the LFS server in-process (see [lfs-transfer.md](lfs-transfer.md)).

Git runs with an environment built from scratch: `PATH` copied from the session (set by `sshd`, which a client cannot override under `restrict` and the default `PermitUserEnvironment no`), `HOME`, `LANG=C`, `GIT_CONFIG_NOSYSTEM=1`, `TMPDIR=$HOME/.hn-serve/state/tmp`, `GIT_PROTOCOL` only when the incoming value matches `version=[0-9]`, `HN_SERVE_ACTOR`, `HN_SERVE_KEY` and `HN_SERVE_REPOSITORY`. No shell is involved at any step. Settings passed with `-c` take precedence over the hosted repository's own `config`, so a repository file cannot disable the hooks or object checking.

## Internal hook commands

```text
hn serve hook pre-receive
hn serve hook post-receive
```

Generated hook scripts in `$HOME/.hn-serve/hooks/` execute these. They refuse to run unless `HN_SERVE_ACTOR`, `HN_SERVE_KEY` and `HN_SERVE_REPOSITORY` are set, `GIT_DIR` resolves to that hosted repository, and the key is still bound to that actor. `pre-receive` prints rejection lines to stderr, which Git relays to the client prefixed with `remote: `, and exits 1 on any rejection. `post-receive` never changes the verdict; it records branch owners and the push log and reports a failure to stderr only.

## Client commands added or changed

```text
hn merge PROPOSAL --remote REMOTE [--branch BRANCH]
hn replication select [REMOTE] --all-trusted [--actor ACTOR]... [--proposal ID]... [--memory STREAM]... [budgets]
```

Behavior is specified in [advancement.md](advancement.md) and [all-trusted-selection.md](all-trusted-selection.md). `hn merge PROPOSAL` without `--remote` is unchanged, and `--branch` without `--remote` is a usage error.

## Usage text

`printUsage` in `main.go` gains these lines after `hn merge PROPOSAL`:

```text
  hn merge PROPOSAL --remote REMOTE [--branch BRANCH]
  hn serve repo create|list|show|set|lock-exec ...
  hn serve key add|remove|list ...
  hn serve git --key FINGERPRINT
```
