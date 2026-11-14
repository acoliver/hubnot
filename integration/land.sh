#!/usr/bin/env bash
# land.sh PROPOSAL [--branch BRANCH] [--board THREAD] [--interval SECONDS]
#
# Lands one of your proposals on the hub without babysitting it. Run it from
# your main checkout (the one where `main` is checked out, clean), detached:
#
#   nohup ~/.local/share/hubnot/land.sh sha256:... > ../land-<name>.log 2>&1 < /dev/null &
#
# Every interval it syncs and then does the one thing the proposal needs:
#   - hub main moved past its base: merge hub main into the topic branch (in
#     the worktree that has it checked out, or a temporary one), push, revise
#     the proposal and request one gate. A merge conflict stops it.
#   - no gate on the current head: request one.
#   - gate passed: automerge from this checkout, with no timeout, so the
#     merge event can never be cut off.
#   - gate failed: stop and say where the logs are.
#   - head already on hub main but not recorded merged (an automerge that
#     was killed): repair it with main at the signed base.
#   - the change needs Andrew (a protected path, a policy change, or a path
#     or author outside the ordinary rule): keep it on current hub main and
#     gated, and wait. He accepts it on the review page, which merges it;
#     land.sh never waits for the merge queue or tries to merge such a change.
# It stops when the proposal is merged, and with --board posts one line to
# that board thread. Works on Linux and macOS (bash 3.2). Exit codes:
# 0 merged, 2 needs you (conflict or failed gate), 1 other error.
#
# Start it and go back to work; do not watch it. When it stops with exit 2
# it files the failure itself: a hub issue titled "[landing-failed] ..." and a
# board thread tagged landing-failed, posted by the "lander" identity with an
# @mention of you, so the board hook hands it to you at your next tool call.
# Fix it when you finish your current task (any agent may take one that has
# sat unclaimed for 2 hours). When a later land.sh run merges that branch, it
# comments on the issue, closes it and replies on the thread.
set -uo pipefail
PATH=/opt/homebrew/bin:$PATH
P=${1:?usage: land.sh PROPOSAL [--branch BRANCH] [--board THREAD] [--interval SECONDS]}
shift
JIT_ONCE=0
BRANCH="" BOARD="" INTERVAL=60 WAITING="" WAIT_SINCE=0 WAITED=""
while [ $# -gt 0 ]; do
	case "$1" in
	--jit-once) JIT_ONCE=1; shift ;;
	--branch) BRANCH=$2; shift 2 ;;
	--board) BOARD=$2; shift 2 ;;
	--interval) INTERVAL=$2; shift 2 ;;
	*) echo "land: unknown option $1" >&2; exit 1 ;;
	esac
done
[ "$JIT_ONCE" = 0 ] || [ -n "${HUBNOT_STRICT_JIT_CONFIG:-}" ] || { echo "--jit-once requires strict JIT configuration" >&2; exit 1; }
say() { echo "$(date -u +%H:%M:%SZ) land: $*"; }
FAILED_STATE="" # set once BRANCH is known
lander_post() { # lander_post new|reply ARG... (body on stdin); prints the new thread id for "new"
	local tok=$HOME/.config/rd-board/lander.token babble=$HOME/.local/bin/babble
	[ -r "$tok" ] && [ -x "$babble" ] || return 1
	BABBLE_URL=http://127.0.0.1:7420 BABBLE_TOKEN=$(cat "$tok") BABBLE_CONFIG=$HOME/.config/rd-board/no-babble-profile.toml \
		"$babble" "$@" --json 2>/dev/null | sed -n 's/^ *"thread_id": *\([0-9]*\).*/\1/p; s/^ *"id": *\([0-9]*\).*/\1/p' | head -1
}
hn_quiet() { # one hn command, retried once if the clone is busy
	local out
	out=$("$@" 2>&1) || { sleep 30; out=$("$@" 2>&1) || { echo "$out" >&2; return 1; }; }
	printf '%s\n' "$out"
}
report_failure() { # report_failure REASON: hub issue + board thread naming the author
	local reason=$1 author repo log title body issue thread
	author=$(board whoami --json 2>/dev/null | sed -n 's/^ *"name": *"\([^"]*\)".*/\1/p' | head -1)
	repo=$(git remote get-url hub 2>/dev/null | sed 's#.*[:/]##; s#\.git$##')
	log=$(readlink "/proc/$$/fd/1" 2>/dev/null || lsof -a -p $$ -d 1 -Fn 2>/dev/null | sed -n 's/^n//p' | head -1)
	title="[landing-failed] ${repo:-repo} $BRANCH: $(printf '%s' "$reason" | cut -c1-120)"
	body="land.sh stopped and needs the author${author:+ (@$author)}.

- Repository: ${repo:-unknown}
- Branch: $BRANCH
- Proposal: $P
- Reason: $reason
- land.sh log: ${log:-the file you redirected land.sh into}
- Checkout: $(pwd)

Fix it when your current task is done, then start land.sh again; it closes this issue when the branch merges. If this sits unclaimed for 2 hours any agent may take it: reply \"taking it\" on the board thread first."
	# Mutations need full sha256 ids, so read them from --json.
	issue=$(hn_quiet hn issue open --json --body "$body" "$title" | grep -o '"issue_id": *"sha256:[0-9a-f]\{64\}"' | head -1 | grep -o 'sha256:[0-9a-f]*')
	[ -n "$issue" ] && hn_quiet hn sync hub > /dev/null && say "filed hub issue ${issue:7:12}"
	thread=$(printf '@%s %s\n\n%s\n' "${author:-observer}" "$reason" "${issue:+Hub issue ${issue:7:12}. }Branch $BRANCH, proposal ${P:7:12}, repository ${repo:-unknown}. Log: ${log:-see your land.sh redirect}. Fix it after your current task, then rerun land.sh; any agent may take it if unclaimed for 2 hours (say \"taking it\" here first)." \
		| lander_post new "$title" --tag landing-failed --body -)
	[ -n "$thread" ] && say "posted board thread $thread"
	[ -n "$FAILED_STATE" ] && printf 'issue=%s\nthread=%s\n' "$issue" "$thread" > "$FAILED_STATE"
}
close_failure() { # after a merge: close any landing-failed issue and thread for this branch
	[ -n "$FAILED_STATE" ] && [ -f "$FAILED_STATE" ] || return 0
	local issue thread rev
	issue=$(sed -n 's/^issue=//p' "$FAILED_STATE") thread=$(sed -n 's/^thread=//p' "$FAILED_STATE")
	if [ -n "$issue" ]; then
		hn_quiet hn issue comment "$issue" --body "Merged as proposal ${P:7:12} (head ${head:0:10}) by land.sh." > /dev/null
		rev=$(hn_quiet hn issue heads "$issue" --json | grep -o '"items": *\[{"id": *"sha256:[0-9a-f]\{64\}"' | grep -o 'sha256:[0-9a-f]*')
		[ -n "$rev" ] && hn_quiet hn issue close "$issue" --expect "$rev" > /dev/null && say "closed hub issue ${issue:7:12}"
		hn_quiet hn sync hub > /dev/null
	fi
	[ -n "$thread" ] && printf 'Fixed: merged as proposal %s.\n' "${P:7:12}" | lander_post reply "$thread" > /dev/null
	rm -f "$FAILED_STATE"
}
die() {
	[ -z "${WAITING:-}" ] || unwant "$WAITING"
	say "$2"
	[ "$1" = 2 ] && report_failure "$2"
	[ "$1" = 0 ] && close_failure
	if [ -n "$BOARD" ]; then
		local what="needs attention"
		[ "$1" = 0 ] && what="done"
		echo "land.sh, proposal ${P:7:12}, $what: $2" | cut -c1-600 | board reply "$BOARD" > /dev/null 2>&1
	fi
	exit "$1"
}
full() { # short or full proposal id -> full id
	local id=${1#sha256:} match
	match=$(git for-each-ref --format='%(refname:lstrip=3)' refs/hn/proposals | grep "^$id")
	[ "$(printf '%s\n' "$match" | grep -c .)" = 1 ] && echo "sha256:$match"
}
field() { hn proposal show "$P" 2>/dev/null | sed -n "s/^$1: *//p" | head -1; }
# A change request binds to one revision in hn, so a revision drops it. Before
# land.sh revises or merges, it looks across the whole lineage: a change
# request is open until the same reviewer later approves a revision.
open_change_request() {
	hn proposal list --json 2>/dev/null | python3 -c '
import json, sys
pid = sys.argv[1]
items = json.load(sys.stdin)["data"]["items"]
me = next((i for i in items if i["id"] == pid), None)
if me is None:
    sys.exit(0)
family = [i for i in items if i["lineage"]["root"] == me["lineage"]["root"]]
approvals = [a for i in family for a in i["approvals"]]
for i in family:
    for cr in i["changeRequests"]:
        if not any(a["actor"] == cr["actor"] and a["timestamp"] > cr["timestamp"] for a in approvals):
            print(cr["actorName"] + " requested changes on " + i["id"][7:19] + ": " + " ".join(cr.get("body", "").split())[:300])
            sys.exit(0)
' "$P"
}

# Why the ordinary rule cannot merge P; empty when the session merges it
# itself. Such a proposal merges only through Andrew's Accept on the review
# page, and only while its base is hub main, so land.sh keeps it current.
# Read once per revision: the list is slow.
NEEDS="" NEEDS_FOR="" NEEDS_SAID=""
needs_maintainer() {
	[ "$NEEDS_FOR" = "$P" ] && return 0
	NEEDS_FOR=""
	NEEDS=$(hn proposal list --json 2>/dev/null | python3 -c '
import json, sys
items = json.load(sys.stdin)["data"]["items"]
me = next((i for i in items if i["id"] == sys.argv[1]), None)
if me is None:
    sys.exit(3)
why = []
if me.get("changesPolicy"):
    why.append("it changes the hub policy")
protected = me.get("protectedPaths", [])
if protected:
    why.append("protected: " + ", ".join(protected[:3]))
rule = next((r for r in me.get("rules", []) if r["name"] == "ordinary"), None)
if rule is None:
    why.append("no ordinary rule here")
else:
    outside = [p for p in rule.get("outsideAllowlist", []) if p not in protected]
    if outside:
        why.append("outside the ordinary rule: " + ", ".join(outside[:3]))
    if not rule.get("authorAllowed"):
        why.append("its author is not in the ordinary rule")
print("; ".join(why))
' "$P")
	local rc=$?
	if [ "$rc" -ne 0 ]; then
		NEEDS=""
		say "cannot determine ordinary-rule eligibility for $P (hn proposal list failed or returned invalid data); retrying next turn"
		return 1
	fi
	NEEDS_FOR=$P
}

[ "$(git branch --show-current)" = main ] || die 1 "run land.sh in the checkout where main is checked out"
P=$(full "$P") || true
P=${P:-$1}
[ -n "$P" ] || die 1 "proposal $1 not found; run hn sync hub first"
head=$(field Head)
if [ -z "$BRANCH" ]; then
	BRANCH=$(git for-each-ref --points-at "$head" --format='%(refname:short)' refs/heads | grep -vx main | head -1)
fi
[ -n "$BRANCH" ] || die 1 "no local branch points at the proposal head ${head:0:12}; pass --branch"
FAILED_STATE="$(git rev-parse --git-common-dir)/land-failed-$(printf '%s' "$BRANCH" | tr '/' '_')"
# Repositories listed in queue-repos have a merge queue on the hub box
# (notes/merge-queue-design.md): once the gate passes, the queue merges, and
# land.sh only waits. A proposal the queue bounces (conflict, or its gate
# failed on the combined commit) is revised here as before.
REPO=$(git remote get-url hub 2>/dev/null | sed 's#.*[:/]##; s#\.git$##')
QUEUED=""
grep -qx "$REPO" "${HUBNOT_QUEUE_REPOS:-$HOME/.local/share/hubnot/queue-repos}" 2>/dev/null && QUEUED=1
# Files a session edits and never commits (its memories). Their changes are
# set aside only for the instant land.sh needs a clean main checkout, then
# put back, also when land.sh stops; the session keeps editing them meanwhile.
KEEP_ASIDE=${HUBNOT_KEEP_ASIDE:-.llxprt/LLXPRT.md}
ASIDE=$(mktemp -d "${TMPDIR:-/tmp}/land-aside.XXXXXX")
aside_save() {
	local f
	for f in $KEEP_ASIDE; do
		git diff --quiet -- "$f" 2>/dev/null && continue
		[ -e "$f" ] || continue
		mkdir -p "$ASIDE/$(dirname "$f")" && cp -p "$f" "$ASIDE/$f" && git checkout -q -- "$f"
	done
}
aside_restore() {
	local f
	for f in $KEEP_ASIDE; do
		[ -e "$ASIDE/$f" ] || continue
		cp -p "$ASIDE/$f" "$f" && rm -f "$ASIDE/$f"
	done
}
trap 'aside_restore; rm -rf "$ASIDE"' EXIT
# A stale index.lock (a git killed mid-write) blocks every later command. It is
# removed only when over a minute old and no git process runs in this checkout.
unlock_stale() { # [DIR]: this checkout, or the worktree DIR
	local dir=${1:-.} lock p here age size holders
	lock=$(git -C "$dir" rev-parse --git-path index.lock 2>/dev/null) || return 0
	case "$lock" in /*) ;; *) lock=$dir/$lock ;; esac
	# Only a regular file is eligible; a symlink would be followed by -f.
	[ -f "$lock" ] && [ ! -L "$lock" ] || return 0
	size=$(wc -c < "$lock" 2>/dev/null) || return 0
	[ "$size" -eq 0 ] || return 0
	age=$(stat -c %Y "$lock" 2>/dev/null || stat -f %m "$lock" 2>/dev/null) || return 0
	[ $(( $(date +%s) - age )) -gt 60 ] || return 0
	here=$(cd "$dir" && pwd -P)
	for p in $(pgrep -x git 2>/dev/null); do
		[ "$(readlink "/proc/$p/cwd" 2>/dev/null || lsof -a -p "$p" -d cwd -Fn 2>/dev/null | sed -n 's/^n//p')" = "$here" ] && return 0
	done
	# A process may use git -C from a different cwd. Never remove a lock that
	# any process still has open; if lsof is unavailable, leave it untouched.
	# lsof reports "no holder" as exit 1 with no output. Any other status, PID
	# or message (permission, filesystem warning) means the answer is unknown.
	command -v lsof >/dev/null 2>&1 || return 0
	holders=$(lsof -t "$lock" 2>&1) && return 0
	[ $? -eq 1 ] && [ -z "$holders" ] || return 0
	rm -f "$lock" && say "removed a stale $lock"
}
clean_or_die() { # $1: what for
	unlock_stale
	aside_save
	local dirty
	dirty=$(git --no-optional-locks status --porcelain | head -3 | tr '\n' ' ')
	[ -z "$dirty" ] && return 0
	aside_restore
	die 2 "main checkout is not clean; cannot $1 ($dirty)"
}
unwant() { git push -q hub --delete "queue/want/${1:7:12}" 2> /dev/null || true; }
queue_has() { # the queue holds P: queued repo, gate passed on the head, not bounced
	[ -n "$QUEUED" ] || return 1
	[ -z "$NEEDS" ] || return 1 # the queue takes only what the ordinary rule can merge
	git rev-parse -q --verify "refs/remotes/hub/queue/bounced/${P:7:12}" > /dev/null && return 1
	printf '%s\n' "$status" | grep -q '^Pipeline: .*1/1 trusted passes'
}
say "landing ${P:7:12} (branch $BRANCH)"

while :; do
	if [ -n "${HUBNOT_STRICT_JIT_CONFIG:-}" ]; then
		client=${HUBNOT_STRICT_JIT_CLIENT:-$HOME/.local/share/hubnot/selected_author.py}
		if ! jout=$(python3 "$client" author --config "$HUBNOT_STRICT_JIT_CONFIG" --proposal "$P" 2>&1); then
			say "strict JIT admission unavailable; no direct fallback: $jout"
			[ "$JIT_ONCE" = 1 ] && exit 1
			sleep "$INTERVAL"
			continue
		fi
		jstate=$(printf '%s\n' "$jout" | python3 -c 'import json,sys; print(json.load(sys.stdin)["status"])') || {
			say "invalid strict JIT status; no direct fallback"
			[ "$JIT_ONCE" = 1 ] && exit 1
			sleep "$INTERVAL"
			continue
		}
		say "$jout"
		case "$jstate" in
		needs-attention) die 2 "strict JIT $P: $jout. Inspect the reported blocker before restarting the lander; strict admission grants no acceptance." ;;
		merged) die 0 "strict JIT original lineage merged: $jout" ;;
		waiting|prepared|invalidated) [ "$JIT_ONCE" = 1 ] && exit 0; sleep "$INTERVAL"; continue ;;
		*) say "unknown strict JIT status; no direct fallback"; [ "$JIT_ONCE" = 1 ] && exit 1; sleep "$INTERVAL"; continue ;;
		esac
	fi
	if ! hn sync hub > /dev/null 2>&1; then
		say "hn sync hub failed; retrying next turn"
		sleep "$INTERVAL"
		continue
	fi
	if ! git fetch -q --prune hub '+refs/heads/*:refs/remotes/hub/*'; then
		say "fetch from hub failed; retrying next turn"
		sleep "$INTERVAL"
		continue
	fi
	if ! status=$(hn proposal status "$P" 2>&1); then
		say "hn proposal status failed for $P: $(printf '%s' "$status" | tr '\n' ' ' | cut -c1-200); retrying next turn"
		sleep "$INTERVAL"
		continue
	fi
	state=$(printf '%s\n' "$status" | sed -n 's/^Status: *//p')
	[ -n "$state" ] || {
		say "hn proposal status returned no state for $P; retrying next turn"
		sleep "$INTERVAL"
		continue
	}
	if printf '%s\n' "$status" | grep -q 'superseded by'; then
		next=$(printf '%s\n' "$status" | grep -o 'superseded by sha256:[0-9a-f]\{64\}' | head -1 | cut -d' ' -f3)
		[ -n "$next" ] && { P=$next; say "following revision ${P:7:12}"; continue; }
	fi
	base=$(field Base) head=$(field Head) main=$(git rev-parse hub/main) || {
		say "could not read proposal base/head or hub main for $P; retrying next turn"
		sleep "$INTERVAL"
		continue
	}
	[ -n "$base" ] && [ -n "$head" ] || {
		say "proposal status omitted its base/head for $P; retrying next turn"
		sleep "$INTERVAL"
		continue
	}
	if [ "$state" = merged ]; then
		git merge -q --ff-only hub/main 2>/dev/null
		die 0 "merged ${P:7:12} (head ${head:0:10}); hub main is ${main:0:10}"
	fi
	if printf '%s\n' "$status" | grep -Eq '^Decision: .*; [1-9][0-9]* rejects'; then
		die 2 "${P:7:12}: Andrew rejected it; see hn proposal show for his note, fix it and revise, then rerun land.sh"
	fi
	needs_maintainer || { sleep "$INTERVAL"; continue; }
	if [ -n "$NEEDS" ] && git merge-base --is-ancestor "$head" hub/main 2>/dev/null; then
		# The review page advanced main for Andrew; its merge event follows.
		sleep "$INTERVAL"
		continue
	fi
	if git merge-base --is-ancestor "$head" hub/main 2>/dev/null; then
		# The queue advances main first and publishes its merge event on its
		# next sync; give it a few turns before treating this as a repair.
		if [ "$WAITING" = "$P" ] && [ "${EVENT_WAIT:-0}" -lt 3 ]; then
			EVENT_WAIT=$(( ${EVENT_WAIT:-0} + 1 ))
			[ "$EVENT_WAIT" = 1 ] && say "the queue merged the code; waiting for its merge event"
			sleep "$INTERVAL"
			continue
		fi
		say "head is on hub main but no merge event: repairing with main at the base ${base:0:10}"
		clean_or_die repair
		git reset -q --hard "$base"
		hn automerge "$P" --rule ordinary --execute --remote hub 2>&1 | grep -E '^(hn:|Recorded|Advanced)'
		git merge -q --ff-only hub/main
		aside_restore
		continue
	fi
	if queue_has; then
		if [ "$WAITING" != "$P" ]; then
			# The queue skips a change with an open change request too; say
			# so here rather than wait for nothing (checked once per
			# revision: the lineage read is slow, and the queue rechecks).
			cr=$(open_change_request)
			[ -z "$cr" ] || die 2 "${P:7:12}: open change request: $cr. Fix it, revise, and ask that reviewer to approve the new revision (hn review --approve); then rerun land.sh"
			[ -z "${WAITING:-}" ] || unwant "$WAITING"
			# The marker tells the queue a lander is waiting, so it takes
			# the change however old its gate pass is.
			git push -q hub "+$head:refs/heads/queue/want/${P:7:12}" 2>/dev/null || say "could not push the queue marker; the queue takes it only while the pass is recent"
			say "gate passed; the merge queue merges it"
			WAITING=$P WAIT_SINCE=$(date +%s)
		fi
		# A stopped queue must not strand the change: after QUEUE_WAIT
		# seconds land.sh merges or revises it itself, as without a queue.
		if [ $(( $(date +%s) - WAIT_SINCE )) -lt "${QUEUE_WAIT:-1800}" ]; then
			sleep "$INTERVAL"
			continue
		fi
		[ "$WAITED" = "$P" ] || say "WARNING: the merge queue has not merged it in ${QUEUE_WAIT:-1800} s; landing it without the queue (tell gitium on the board)"
		unwant "$P"
		WAITED=$P QUEUED=""
	fi
	if [ "$main" != "$base" ] || printf '%s\n' "$status" | grep -q '^Pipeline: .*1/1 trusted passes'; then
		cr=$(open_change_request)
		[ -z "$cr" ] || die 2 "${P:7:12}: open change request: $cr. Fix it, revise, and ask that reviewer to approve the new revision (hn review --approve); then rerun land.sh"
	fi
	if [ "$main" != "$base" ]; then
		wt=$(git worktree list --porcelain | awk -v b="refs/heads/$BRANCH" '/^worktree /{w=substr($0,10)} $0=="branch "b{print w}')
		tmp=""
		if [ -z "$wt" ]; then
			tmp=$(mktemp -d "${TMPDIR:-/tmp}/land.XXXXXX") && rmdir "$tmp"
			git worktree add -q "$tmp" "$BRANCH" || die 1 "cannot check out $BRANCH"
			wt=$tmp
		fi
		# Session memory files (KEEP_ASIDE) are always modified there; git
		# merge itself refuses if hub main changes one of them.
		unlock_stale "$wt"
		dirty=$(git -C "$wt" --no-optional-locks status --porcelain --untracked-files=no | awk -v keep=" $KEEP_ASIDE " 'index(keep, " " substr($0, 4) " ") == 0' | head -3 | tr '\n' ' ')
		[ -z "$dirty" ] || die 2 "$BRANCH has uncommitted changes in $wt ($dirty); not merging hub main into it"
		say "hub main moved to ${main:0:10}; merging it into $BRANCH"
		if ! merr=$(git -C "$wt" merge -q --no-edit hub/main 2>&1); then
			# Only unmerged paths mean a content conflict; anything else (a
			# lock, a full disk) is retried and then reported as what it is.
			unmerged=$(git -C "$wt" diff --name-only --diff-filter=U 2>/dev/null | head -3 | tr '\n' ' ')
			git -C "$wt" merge --abort 2>/dev/null
			[ -n "$tmp" ] && git worktree remove --force "$tmp"
			[ -z "$unmerged" ] || die 2 "${P:7:12}: merging hub main ${main:0:10} into $BRANCH conflicts in $unmerged; resolve it, then rerun land.sh"
			MERGE_TRIES=$(( ${MERGE_TRIES:-0} + 1 ))
			merr=$(printf '%s' "$merr" | tr '\n' ' ' | cut -c1-240)
			[ "$MERGE_TRIES" -lt 3 ] || die 2 "${P:7:12}: merging hub main ${main:0:10} into $BRANCH failed 3 times and it is not a content conflict: $merr"
			say "merging hub main into $BRANCH failed, not a conflict ($merr); retrying"
			sleep "$INTERVAL"
			continue
		fi
		MERGE_TRIES=0
		# The queue merges through its own proposal and merge commit, so this
		# lineage never reads merged; when the branch with main merged in
		# has main's exact tree, its change is already on main.
		if [ "$(git -C "$wt" rev-parse 'HEAD^{tree}')" = "$(git rev-parse 'hub/main^{tree}')" ]; then
			git -C "$wt" reset -q --hard "ORIG_HEAD"
			[ -n "$tmp" ] && git worktree remove --force "$tmp"
			[ -z "${WAITING:-}" ] || unwant "$WAITING"
			git merge -q --ff-only hub/main 2>/dev/null
			die 0 "${P:7:12}: its change is already on hub main ${main:0:10} (merged by the queue); nothing to revise"
		fi
		[ -n "$tmp" ] && git worktree remove --force "$tmp"
		git push -q hub "$BRANCH" || die 1 "push of $BRANCH to hub failed"
		# Another revision may already have exactly this base and head (an
		# earlier lander or a manual revise). Follow it instead of revising
		# again, so its gate result is not stranded.
		tip=$(git rev-parse "$BRANCH")
		for r in $(git for-each-ref --format='%(objectname) %(refname:lstrip=3)' refs/hn/proposals | awk -v t="$tip" '$1==t {print $2}'); do
			[ "sha256:$r" = "$P" ] && continue
			shown=$(hn proposal show "$r" 2>/dev/null)
			printf '%s\n' "$shown" | grep -q "^Base: $main" || continue
			printf '%s\n' "$shown" | grep -q '^State: *merged' && continue
			P=sha256:$r
			say "proposal ${r:0:12} already has head ${tip:0:10} on hub main ${main:0:10}; following it instead of revising"
			continue 2
		done
		out=$(hn proposal revise "$P" --base hub/main --head "$BRANCH" --body "Updated to hub main ${main:0:10} by land.sh" 2>&1)
		next=$(printf '%s\n' "$out" | sed -n 's/^Created proposal revision \(sha256:[0-9a-f]\{64\}\)$/\1/p')
		[ -n "$next" ] || die 1 "revise failed: $out"
		P=$next
		say "revised to ${P:7:12}"
		hn sync hub > /dev/null 2>&1
		hn run request "$P" gate > /dev/null && say "gate requested"
		hn sync hub > /dev/null 2>&1
		continue
	fi
	pipeline=$(printf '%s\n' "$status" | sed -n 's/^Pipeline: *//p')
	case "$pipeline" in
	*failure*)
		die 2 "${P:7:12}: the gate failed on ${head:0:10}; see hn run list and hn run logs" ;;
	*"1/1 trusted passes"*)
		if [ -n "$NEEDS" ]; then
			[ "$NEEDS_SAID" = "$P" ] || say "gate passed; ${P:7:12} needs Andrew ($NEEDS). It merges when he accepts it on the review page; keeping it on current hub main until then"
			NEEDS_SAID=$P
			sleep "$INTERVAL"
			continue
		fi
		clean_or_die automerge
		git merge -q --ff-only hub/main
		say "gate passed; automerging"
		hn automerge "$P" --rule ordinary --execute --remote hub 2>&1 | grep -E '^(hn:|Recorded|Advanced)'
		aside_restore
		continue ;;
	esac
	if ! hn run list 2>/dev/null | awk '$2=="gate"{print $3}' | grep -q "^${head:0:12}"; then
		hn run request "$P" gate > /dev/null && say "no gate on ${head:0:10}; requested one"
		hn sync hub > /dev/null 2>&1
	fi
	sleep "$INTERVAL"
done
