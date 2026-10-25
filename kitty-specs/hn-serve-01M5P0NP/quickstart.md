# Quickstart: hosting a governed repository with hn serve

This walkthrough follows the spec's user stories in the order a team meets them. It describes the behavior the implementation must deliver and is the outline for `docs/serve-v0.md`, where each fenced example marked `<!-- example: ... -->` is executed by `serve_quality_test.go`. Host names, user names and IDs below are illustrative.

## 1. Prepare the server account (administrator, US2)

`hn serve` runs as its own unprivileged user and relies on the system's OpenSSH. Run these as root once, then do everything else as that user:

```sh
useradd --create-home --shell /bin/sh hn-serve
install -m 0755 ./hn /usr/local/bin/hn
```

The server needs Git 2.11 or later for object quarantine and push options, and the Git LFS client on participant machines if the team uses LFS. `sshd` must allow public-key login for `hn-serve`. With the default `StrictModes yes`, the home directory and `~/.ssh` must not be writable by group or others.

## 2. Host a repository (administrator, US2)

The administrator seeds a hosted repository from an existing one whose `main` already carries `.hn/policy.json`:

<!-- example: repo-create -->
```sh
sudo -u hn-serve hn serve repo create pingpong --from /srv/import/rust-pingpong.git
```

```text
Created hosted repository pingpong
Protected: refs/heads/main
LFS quota: 1073741824 bytes
Clone URL: ssh://hn-serve@git.example.internal/pingpong.git
```

Only branches and tags are imported. Hubnot facts arrive through governed pushes in step 4.

## 3. Bind each participant's SSH key to their actor (administrator, US2)

Each participant sends their SSH public key and their full actor fingerprint from `hn identity show`. Runners and verifiers need bindings too, because they publish results.

<!-- example: key-add -->
```sh
sudo -u hn-serve hn serve key add --actor 4f1c9a6d0d2b7e3a8c5f6e1b9d0a7c3e2f4b6d8a0c1e3f5a7b9d1c3e5f7a9b1c alice.pub
```

```text
Bound SHA256:Jd8bC8QWw0m0Y4l6m6yZ1gq5q3o9s0Kc2b2f4QwA9eU to actor 4f1c9a6d0d2b7e3a8c5f6e1b9d0a7c3e2f4b6d8a0c1e3f5a7b9d1c3e5f7a9b1c
```

`~hn-serve/.ssh/authorized_keys` now holds a managed line with `restrict` and the forced command `/usr/local/bin/hn serve git --key SHA256:Jd8b...`. Keys outside the managed block are untouched. Removing a binding with `hn serve key remove SHA256:...` rewrites the block, and that key is refused on its next connection.

## 4. Contribute (participant, US1)

Participants clone and push with ordinary Git, then publish facts with `hn sync`:

```sh
git clone ssh://hn-serve@git.example.internal/pingpong.git
cd pingpong
git switch -c feature/serve-ball
# edit, commit
git push -u origin feature/serve-ball
hn proposal open --base main --head feature/serve-ball "Serve the ball"
hn sync origin
```

The server accepts the feature branch, records Alice as its owner, and accepts her actor chain and proposal ref after validating them. A direct push to `main` is refused for the whole push:

<!-- example: reject-direct-main -->
```text
remote: hn-serve/1 reject ref=refs/heads/main actor=4f1c...9b1c code=protected-no-proposal ids=9b2e4c6a... missing=proposal,review,ci,acceptance next="open a proposal for 9b2e4c6a..."
remote: hn-serve: refs/heads/main is protected and 9b2e4c6a has no accepted, CI-tested proposal.
 ! [remote rejected] main -> main (pre-receive hook declined)
```

The guide prints full IDs; they are shortened here for width.

## 5. Discover work without selecting it (runner or verifier, US4)

A runner clone saves the all-trusted selection once:

<!-- example: all-trusted-select -->
```sh
hn replication select origin --all-trusted
```

From then on, `hn sync origin` imports every advertised actor chain and proposal, validates each one in client quarantine, and treats actors that have not published yet as normal. A maintainer requests CI, the runner's next sync sees the request, and `hn runner once --accept-pipeline test --accept-actor <maintainer>` executes it and publishes the signed result on the following `hn sync`.

## 6. Review, accept and advance (reviewer and maintainer, US3)

Reviewers run `hn review` and maintainers run `hn decide` as today, then `hn sync origin`. When `hn proposal status <candidate>` reports `accepted`, a maintainer advances `main`:

<!-- example: advance -->
```sh
hn merge sha256:1c2d3e4f... --remote origin
```

```text
Advanced origin refs/heads/main to 9b2e4c6a8d0f for proposal 1c2d3e4f5a6b
Recorded merge event 7d8e9f0a1b2c with 1 acceptance decision(s)
```

`main` on the server now equals the proposal head that CI tested, and no new commit exists. A fresh clone with no identity can confirm the evidence:

```sh
git clone ssh://hn-serve@git.example.internal/pingpong.git verify && cd verify
hn replication select origin --all-trusted
hn sync origin
hn proposal status sha256:1c2d3e4f...
```

If `main` moved after the proposal was opened, the advancement is refused with `protected-not-fast-forward`, and the author runs `hn proposal revise` onto the current tip and gathers fresh review and CI evidence. If the advancing command is interrupted after the server moved `main`, rerunning the same `hn merge ... --remote origin` records the missing merge fact once.

## 7. Large files (participant, US5)

With Git LFS installed, `git lfs track "*.bin"`, commit and `git push` upload objects over the same SSH forced command. Uploads are verified against their object ID before they become downloadable. An upload that would take the repository past its quota is refused with `lfs-quota`; the administrator raises it with `hn serve repo set pingpong --lfs-quota 2147483648`. Locking is not supported.

## 8. Recover a protected branch without valid policy (administrator)

If `main` on the server ever lacks a valid `.hn/policy.json`, every update to it is refused with `protected-policy-invalid`. Recovery happens outside the governed push path, as the server user, while holding the repository lock:

```sh
sudo -u hn-serve hn serve repo lock-exec pingpong -- git update-ref refs/heads/main <repaired-commit> <current-tip>
```

The guide explains how to prepare `<repaired-commit>` and why the update's old value must be given.
