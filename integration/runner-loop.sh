#!/bin/bash
# runner-loop.sh INTERVAL_SECONDS HN_RUNNER_ONCE_ARGS...
# (needs hn with --only-policy-changes, fork e42a94e6 or later)
# Replaces `hn runner watch` for the runners whose gates share rd-gate's
# gate.lock (real-disunity, rd-stranded, rd-chess). Each turn holds
# runners.lock for a whole `hn runner once` (sync, at most one gate, publish),
# so runners take turns as whole gates. Before accepting anything a turn also
# waits until gate.lock is free. A gate step therefore never waits for the
# lock inside its own timeout, which is what killed real-disunity gates while
# an rd-stranded gate ran.
set -uo pipefail
interval=${1:?usage: runner-loop.sh INTERVAL ARGS...}
shift
STATE=${RD_GATE_STATE:-$HOME/.local/state/rd-gate}
HN=${RD_HN:-/usr/local/bin/hn}
export HN
if [ -n "${HUBNOT_STRICT_JIT_CONFIG:-}" ]; then
	[ "$#" = 0 ] || { echo "strict runner refuses legacy/passthrough arguments" >&2; exit 1; }
	client=${HUBNOT_STRICT_JIT_CLIENT:?strict worker requires explicit client}
	case "$client" in /*) ;; *) echo "strict worker client must be absolute" >&2; exit 1 ;; esac
	worker_args=(--config "$HUBNOT_STRICT_JIT_CONFIG" --host-lock-dir "$STATE")
	if [ "${RD_STRICT_JIT_BACKEND:-sandbox}" = host ] && [ "${RD_STRICT_JIT_ALLOW_HOST:-0}" = 1 ]; then
		worker_args+=(--backend host --allow-unsafe-host-execution)
	elif [ "${RD_STRICT_JIT_BACKEND:-sandbox}" != sandbox ]; then
		echo "strict host backend requires explicit unsafe-host permission" >&2; exit 1
	fi
	while :; do
		python3 "$client" worker "${worker_args[@]}"
		rc=$?
		[ "${RD_STRICT_JIT_ONCE:-0}" = 1 ] && exit "$rc"
		sleep "$interval"
	done
fi
short_pause=${RD_LOOP_SHORT_PAUSE:-10}
busy_pause=${RD_LOOP_BUSY_PAUSE:-30}
mkdir -p "$STATE"
stop=0
trap 'stop=1' TERM INT
echo "Runner loop: hn runner once $* (interval ${interval}s)"
# rd-oldworld's gate is light and takes no lock in rdx-gate, so its runner
# never waits for the heavy gates either.
light=0
repo=$(basename "$PWD")
[ "$repo" = rd-oldworld ] && light=1
yielding=0
# Each turn's output is streamed to the log as it happens (so "Runner
# accepted" appears when a gate starts) and kept in $turn to decide the wait.
turn=$(mktemp "${TMPDIR:-/tmp}/runner-loop.XXXXXX")
trap 'rm -f "$turn"' EXIT
# Requests the operator does not want run (one full sha256: ID per line,
# "#" comments allowed) are read each turn from this file.
skipfile=$HOME/.config/rd-runner/skip-requests/$(basename "$PWD")
acceptfile=$HOME/.config/rd-runner/accept-actors
firstfile=$HOME/.config/rd-runner/first-actors
# A killed Xvfb leaves /tmp/.XN-lock behind, and xvfb-run counts the display
# as taken: the gate's server then drifts onto displays that test harnesses
# pin (rd-chess verify_window.py uses :100). Remove our own dead ones.
reap_x_locks() {
	local f n p
	for f in /tmp/.X*-lock; do
		[ -O "$f" ] || continue
		p=$(tr -d ' ' < "$f" 2>/dev/null)
		[ -n "$p" ] && ! kill -0 "$p" 2>/dev/null || continue
		n=${f#/tmp/.X}; n=${n%-lock}
		rm -f "$f" "/tmp/.X11-unix/X$n" && echo "runner-loop: removed stale X display lock :$n"
	done
}
# Fair turns between heavy runners. A runner that finds the machine busy
# marks itself as waiting in $STATE/want/<repo>; one that gets the machine
# clears its mark. A runner that has just run a gate yields its next turn
# while another runner's mark is fresh, so a runner retrying 10 s after its
# own gate no longer wins every time over one inside a slow hn turn.
wantdir=$STATE/want
mkdir -p "$wantdir"
justran=0
yield_deadline=0
others_waiting() {
	find "$wantdir" -maxdepth 1 -type f -mmin -30 ! -name "$repo" -printf '%f ' 2>/dev/null
}
trap 'rm -f "$turn" "$wantdir/$repo"' EXIT
waiting=0
# hn from 0.1.0-beta.3 on prefixes runner lines with a UTC timestamp.
ACCEPTED='^([0-9TZ:.-]+ )?Runner accepted request'
# Upstream's --skip-stale-base is a switch with --target refs/heads/NAME; the
# fork's took the branch name. Units say "--skip-stale-base NAME", rewritten
# here for an hn that knows --target, so the binary swap needs no unit change.
translate_args() {
	args=()
	local new=0 skip=0
	"$HN" runner once --help 2>&1 | grep -q -- '-target' && new=1
	for a in "$@"; do
		if [ "$skip" = 1 ]; then
			skip=0
			if [ "$new" = 1 ] && [[ "$a" != -* ]]; then args+=(--target "refs/heads/$a"); continue; fi
		fi
		if [ "$a" = --skip-stale-base ]; then args+=("$a"); skip=1; continue; fi
		args+=("$a")
	done
}
while [ "$stop" = 0 ]; do
	reap_x_locks
	translate_args "$@"
	extra=()
	if [ -r "$skipfile" ]; then
		while read -r id _; do
			case "$id" in sha256:*) extra+=(--skip-request "$id") ;; esac
		done < "$skipfile"
	fi
	# Sessions added since the unit started (add-agent.sh), one full actor
	# per line, are accepted without a restart.
	if [ -r "$acceptfile" ]; then
		while read -r actor _; do
			[[ "$actor" =~ ^[0-9a-f]{64}$ ]] && [[ " $* ${extra[*]} " != *" $actor "* ]] && extra+=(--accept-actor "$actor")
		done < "$acceptfile"
	fi
	# The merge queue's actor (one full actor per line) is served before
	# older requests, once the installed hn knows --first-actor.
	if [ -r "$firstfile" ] && "$HN" runner once --help 2>&1 | grep -q -- '-first-actor'; then
		while read -r actor _; do
			[[ "$actor" =~ ^[0-9a-f]{64}$ ]] && extra+=(--first-actor "$actor")
		done < "$firstfile"
	fi
	busy=0
	if [ "$light" = 1 ]; then
		"$HN" runner once "${args[@]}" "${extra[@]}" 2>&1 | tee "$turn"
		rc=${PIPESTATUS[0]}
	else
		# Wait up to one interval for the machine, queued behind the other
		# runners. Exit 75: it is still busy (another runner, or the nightly
		# holding gate.lock).
		# $STATE/next-repo names the repository that gets the machine next
		# (set by the operator when Andrew waits on its gate; expires after
		# 3 h). Other heavy runners yield until that runner accepts a gate.
		prio=$(cat "$STATE/next-repo" 2>/dev/null || true)
		others=""
		fair_yield=0
		priority_mine=0
		[ "$prio" = "$repo" ] && [ -n "$(find "$STATE/next-repo" -mmin -180 2>/dev/null)" ] && priority_mine=1
		if [ "$justran" = 1 ]; then
			# The yield after our own gate lasts one handover: it ends when
			# another runner holds the machine or after 5 minutes, and a
			# runner holding priority never yields.
			if [ "$priority_mine" = 1 ] || ! flock -n "$STATE/gate.lock" true || [ "$(date +%s)" -ge "$yield_deadline" ]; then
				justran=0
			else
				others=$(others_waiting)
			fi
		fi
		if [ -n "$prio" ] && [ "$prio" != "$repo" ] && [ -n "$(find "$STATE/next-repo" -mmin -180 2>/dev/null)" ]; then
			rc=75
			[ "$yielding" = 1 ] || echo "runner-loop $(date -u +%H:%M:%SZ): yielding the machine to $prio"
			yielding=1
		elif [ -n "$others" ]; then
			rc=75
			fair_yield=1
			[ "$yielding" = 1 ] || echo "runner-loop $(date -u +%H:%M:%SZ): ran the last gate; yielding a turn to ${others% }"
			yielding=1
		else
			yielding=0
			flock -w "$interval" -E 75 "$STATE/runners.lock" sh -c 'flock -n -E 75 "$1/gate.lock" true || exit 75; shift; exec "$HN" runner once "$@"' _ "$STATE" "${args[@]}" "${extra[@]}" 2>&1 | tee "$turn"
			rc=${PIPESTATUS[0]}
			if [ "$prio" = "$repo" ] && grep -qE "$ACCEPTED" "$turn"; then
				rm -f "$STATE/next-repo" && echo "runner-loop $(date -u +%H:%M:%SZ): priority used; other runners resume"
			fi
			if [ "$rc" != 75 ]; then
				# Had the machine this turn: no longer waiting.
				rm -f "$wantdir/$repo"
				if grep -qE "$ACCEPTED" "$turn"; then
					justran=1
					yield_deadline=$(($(date +%s) + 300))
				else
					justran=0
				fi
			fi
		fi
		if [ "$rc" = 75 ]; then
			# A runner yielding its fair turn is not waiting: marking it would
			# make every runner that ran a gate yield to every other one.
			if [ "$fair_yield" = 0 ]; then
				touch "$wantdir/$repo"
				[ "$yielding" = 1 ] || justran=0
			fi
			# Policy-only requests need no build and take no lock (rd-gate
			# and rdx-gate pass them before locking), so serve those now.
			busy=1
			# Say so once per busy stretch: ordinary requests are passed
			# over silently by --only-policy-changes.
			if [ "$waiting" = 0 ]; then
				echo "runner-loop $(date -u +%H:%M:%SZ): another gate holds the machine; ordinary requests wait, policy changes are served"
				waiting=1
			fi
			"$HN" runner once "${args[@]}" "${extra[@]}" --only-policy-changes 2>&1 | grep -vE '^([0-9TZ:.-]+ )?No pending' | tee "$turn"
			rc=${PIPESTATUS[0]}
		fi
	fi
	if [ "$busy" = 0 ] && [ "$waiting" = 1 ]; then
		echo "runner-loop $(date -u +%H:%M:%SZ): machine free again"
		waiting=0
	fi
	[ "$rc" = 0 ] || echo "hn runner: once exited $rc"
	[ "$stop" = 0 ] || break
	if [ "$busy" = 0 ] && grep -qE "$ACCEPTED" "$turn"; then
		# Runners queued on runners.lock take the machine now.
		sleep "$short_pause" & wait $!
	elif [ "$busy" = 1 ]; then
		# The flock above already waited; a short pause only avoids spinning
		# while the nightly holds gate.lock.
		sleep "$busy_pause" & wait $!
	else
		sleep "$interval" & wait $!
	fi
done
echo "Runner stopped."
