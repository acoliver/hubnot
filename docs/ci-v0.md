# CI and runner v0

Hubnot CI separates coordination from computation:

```text
signed proposal
      ↓
signed run request ── exact commit + exact pipeline digest
      ↓
explicit runner execution
      ↓
signed result ─────── outcome + exit code + duration + log digest
```

Any identity can produce a result. The protocol preserves who claimed what; it
does not currently decide whose result is trusted.

Results also sign the claimed execution backend, operating-system/architecture
pair, and runner implementation version. These fields make environment claims
inspectable; they do not independently prove those claims.

## Pipeline format

Pipelines live at `.hn/pipelines/<name>.json` in the proposed commit:

```json
{
  "version": "hn.pipeline/0",
  "steps": [
    {
      "name": "Unit tests",
      "command": "go",
      "args": ["test", "./..."],
      "workingDirectory": ".",
      "timeoutSeconds": 300
    }
  ]
}
```

`command` is executed directly, without a shell. A repository-defined custom
action is an executable tracked in the commit, for example
`./.hn/actions/test`. Shell behavior is only present if that executable itself
is a shell script.

Unknown JSON fields are rejected. Pipelines are limited to 64 steps, each step
is time-limited, and relative command and working-directory paths may not
escape the extracted checkout.

## Request verification

Before publishing a request, the client verifies that:

1. the subject is a signed proposal;
2. the proposal's code ref exists and matches its signed head;
3. the named pipeline exists in that exact commit;
4. the pipeline parses and passes structural validation.

The request signs the proposal ID, commit, pipeline name, and SHA-256 digest of
the exact pipeline bytes.

Before execution, a runner repeats every check. A pipeline changed in another
commit cannot satisfy the request.

One actor produces at most one current result for an exact request by default.
`hn run execute <full-request-id> --rerun` deliberately executes again and
publishes a replacement signed result; it does not rewrite the prior event.

## Reference sandbox executor

The default Linux backend uses Bubblewrap. It:

- resolves the `bwrap` executable only from the same fixed canonical system
  directories exposed through the sandbox `PATH`, ignoring ambient `PATH`;
- extracts the requested commit into a generated temporary directory;
- rejects unsafe archive paths and unsupported archive entry types;
- creates separate user, PID, network, IPC, UTS, and cgroup namespaces;
- drops capabilities and does not expose the runner's normal `HOME`;
- mounts system tool directories read-only;
- exposes the generated checkout as the only writable project workspace;
- exposes `PATH`, an isolated temporary `HOME` and `TMPDIR`, `CI=true`, and the
  exact `HN_COMMIT`;
- provides no external network interface;
- stops after the first failed or timed-out step;
- caps the signed combined stdout/stderr log at 1 MiB;
- removes the temporary checkout after execution.

The integration test runs the same custom action through both backends and
verifies that a host-only marker visible to the host backend is absent inside
Bubblewrap.

The sandbox still relies on the host kernel and currently lacks seccomp rules,
strong CPU/memory/disk quotas, controlled network capabilities, and portable
non-Linux implementations. It should not yet be treated as a hardened hostile
multi-tenant boundary.

An unsandboxed fallback exists for development. It retains the host user's
normal filesystem and network access and requires both:

```text
--backend host --allow-unsafe-host-execution
```

## Runner discovery

`hn runner once` and `hn runner watch` synchronize HN refs and discover signed
requests. They require a local acceptance policy with two exact matches:

```text
accepted pipeline name
accepted request-signer fingerprint (--accept-actor, repeatable)
```

The signer match uses the full 32-byte public-key fingerprint. There is no
"accept anyone" mode. This means a trusted actor explicitly requests execution
of a particular proposal while the runner independently chooses whether to
honor requests from that actor.

Repeating `--accept-actor` lets one runner serve several requesters, such as
the maintainers and the `requesters` a pipeline's policy lists. The runner
takes 1 to 64 distinct fingerprints and refuses duplicate or malformed values.
When it lists more than one actor, it executes a request from a listed signer
only if that signer may request the pipeline under the base policy of the
requested proposal; it skips other requests from listed signers with a note on
standard error. A runner with a single `--accept-actor` keeps its original
rule and executes every matching request from that actor.

The single-actor rule is kept for compatibility. It predates pipeline
`requesters`, and the established distributed-runner contract, which
`TestDistributedPipelineRun` exercises, has a one-actor runner execute a
request from an actor that the base policy neither lists as a maintainer nor
as a requester. Such a result grants nothing: readiness counts a result only
when its run request was signed by a maintainer or a listed requester under
the proposal's base policy, so the operator's exact choice of one actor
decides only what the runner spends its host on. Operators should note the
consequence: reducing a runner from several `--accept-actor` values to one
also removes the base-policy filter, and the runner then executes every
request that the remaining actor signs.

`runner once` handles at most one request and publishes its result. `runner
watch` repeats this process, waits when no work is available, survives transient
sync errors, and cancels active execution on SIGINT or SIGTERM.

## Attestation limits

A valid signature proves attribution and integrity. It does not prove that the
runner used the reference executor, ran the commands faithfully, or reported an
honest result. Trust policy, reproducible multi-runner agreement, and stronger
environment attestations remain future protocol layers.
