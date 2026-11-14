#!/usr/bin/env python3
"""Merge queue for one hub repository (notes/merge-queue-design.md).

Runs in a clone whose hn identity is an actor and author of the policy's
`ordinary` automerge rule and a gate requester, with `main` checked out and
clean. Each turn it merges at most one passed proposal, oldest pass first:

- base == hub main: `hn automerge --rule ordinary --execute --remote hub`
  on the proposal itself, which records an ordinary merge event;
- otherwise: build M = merge(hub main, proposal head), open a queue
  proposal for M, request its gate, and merge it once it passes while main
  is unchanged. The author's land.sh then sees its head on hub main and
  records its own merge event through the repair path.

A merge conflict or a failed gate on M bounces the proposal: the queue
pushes refs/heads/queue/bounced/<proposal id12> to the hub and leaves it to
the author, whose land.sh then revises as it always did.

Only ordinary proposals are taken: the rule allows the author, every
changed path is inside its allowlist, no protected path or policy change.
Andrew's protected proposals stay with the review page.
"""
import argparse
import json
import os
import subprocess
import sys
import tempfile
import time
from datetime import datetime, timezone

RULE = "ordinary"


HEARTBEAT = 900


def log(msg):
    print(f"{datetime.now(timezone.utc):%H:%M:%SZ} queue: {msg}", flush=True)


def run(args, cwd, check=True, timeout=1800):
    p = subprocess.run(args, cwd=cwd, capture_output=True, text=True, timeout=timeout)
    if check and p.returncode != 0:
        raise RuntimeError(f"{' '.join(args[:4])} exited {p.returncode}: {(p.stderr or p.stdout).strip()[-600:]}")
    return p.stdout


class Queue:
    def __init__(self, clone, remote, interval, max_age):
        self.clone, self.remote, self.interval, self.max_age = clone, remote, interval, max_age
        self.state_path = os.path.join(run(["git", "rev-parse", "--absolute-git-dir"], clone).strip(), "merge-queue.json")
        self.state = json.load(open(self.state_path)) if os.path.exists(self.state_path) else {"inflight": {}}
        self.actor = json.loads(run(["hn", "identity", "show", "--json"], clone))["data"]["actor"]

    def save(self):
        tmp = self.state_path + ".tmp"
        json.dump(self.state, open(tmp, "w"), indent=1)
        os.replace(tmp, self.state_path)

    def git(self, *args, check=True):
        return run(["git", *args], self.clone, check=check).strip()

    def hn(self, *args, check=True):
        return run(["hn", *args], self.clone, check=check)

    def refresh(self):
        self.hn("sync", self.remote, check=False)
        self.git("fetch", "-q", "--prune", self.remote, "+refs/heads/*:refs/remotes/%s/*" % self.remote)
        self.tip = self.git("rev-parse", f"refs/remotes/{self.remote}/main")
        items = json.loads(self.hn("proposal", "list", "--json"))["data"]["items"]
        self.by_id = {i["id"]: i for i in items}
        bounced = self.git("for-each-ref", "--format=%(refname:lstrip=5)", f"refs/remotes/{self.remote}/queue/bounced")
        self.bounced = set(bounced.split())
        # land.sh pushes queue/want/<id12> while it waits for the queue; only
        # those proposals are taken, however old their gate pass, so a live
        # lander is never skipped and an abandoned proposal is never merged.
        want = self.git("for-each-ref", "--format=%(refname:lstrip=5)", f"refs/remotes/{self.remote}/queue/want")
        self.want = set(want.split())

    def open_change_request(self, item):
        """A trusted change request anywhere in the lineage that the same
        reviewer has not answered with a later approval (hn checks only the
        current revision)."""
        root = item["lineage"]["root"]
        family = [i for i in self.by_id.values() if i["lineage"]["root"] == root]
        approvals = [a for i in family for a in i["approvals"]]
        for i in family:
            for cr in i["changeRequests"]:
                if not any(a["actor"] == cr["actor"] and a["timestamp"] > cr["timestamp"] for a in approvals):
                    return f"{cr['actorName']} requested changes on {i['id'][7:19]}"
        return None

    def passed_at(self, item):
        times = [r["timestamp"] for q in item["runRequests"] if q["pipeline"] == "gate"
                 for r in q.get("results", []) if r["outcome"] == "passed"]
        return min(times) if times else None

    def failed(self, item):
        return any(r["outcome"] != "passed" for q in item["runRequests"] if q["pipeline"] == "gate"
                   for r in q.get("results", []))

    def eligible(self, item):
        lin = item["lineage"]
        if item["merged"] or lin["merged"] or lin["superseded"] or lin["closed"]:
            return False
        if item["protectedPaths"] or item["changesPolicy"]:
            return False
        rule = next((r for r in item.get("rules", []) if r["name"] == RULE), None)
        return bool(rule and rule.get("authorAllowed") and not rule.get("outsideAllowlist"))

    def candidates(self):
        out = []
        for item in self.by_id.values():
            if item["actor"] == self.actor or not self.eligible(item):
                continue
            if item["id"][7:19] in self.bounced:
                continue
            if subprocess.run(["git", "merge-base", "--is-ancestor", item["head"], self.tip], cwd=self.clone).returncode == 0:
                continue  # already on main; the author's land.sh records the merge
            at = self.passed_at(item)
            if not at:
                continue
            if item["id"][7:19] not in self.want:
                # Without land.sh's marker: only a recent pass (older
                # landers, or a lander between turns), never an old one.
                age = time.time() - datetime.strptime(at[:19], "%Y-%m-%dT%H:%M:%S").replace(tzinfo=timezone.utc).timestamp()
                if age >= self.max_age:
                    continue
            cr = self.open_change_request(item)
            if cr:
                self.note(item["id"], f"skipping {item['id'][7:19]}: {cr}; the author must answer it")
                continue
            out.append((at, item))
        return [i for _, i in sorted(out, key=lambda x: x[0])]

    def at_main(self, rev):
        if self.git("status", "--porcelain"):
            raise RuntimeError("queue clone main checkout is not clean")
        self.git("checkout", "-q", "main")
        self.git("reset", "-q", "--hard", rev)

    def automerge(self, pid):
        self.at_main(self.tip)
        out = self.hn("automerge", pid, "--rule", RULE, "--execute", "--remote", self.remote, check=False)
        lines = [l for l in out.splitlines() if l.startswith(("Advanced", "Recorded", "hn:"))]
        for l in lines:
            log("  " + l)
        return any(l.startswith("Recorded") for l in lines)

    def bounce(self, item, why):
        ref = f"refs/heads/queue/bounced/{item['id'][7:19]}"
        pushed = subprocess.run(["git", "push", "-q", self.remote, f"{item['head']}:{ref}"], cwd=self.clone, capture_output=True, text=True)
        if pushed.returncode != 0:
            log(f"could not push {ref}: {pushed.stderr.strip()[-300:]}")
        self.bounced.add(item["id"][7:19])
        log(f"bounced {item['id'][7:19]} to its author: {why}")
        self.state["inflight"].pop(item["id"], None)
        self.save()

    def build(self, item):
        work = tempfile.mkdtemp(prefix="merge-queue.")
        os.rmdir(work)
        branch = f"queue/m-{item['id'][7:19]}-{self.tip[:10]}"
        self.git("worktree", "add", "-q", "--detach", work, self.tip)
        try:
            msg = f"Queue: merge {item['id'][7:19]} onto main {self.tip[:10]}\n\n{item['title']}"
            p = subprocess.run(["git", "-c", "user.name=rd-queue", "-c", "user.email=rd-queue@minisforum",
                                "merge", "-q", "--no-ff", "--no-edit", "-m", msg, item["head"]], cwd=work, capture_output=True, text=True)
            if p.returncode != 0:
                return None
            merged = run(["git", "rev-parse", "HEAD"], work).strip()
        finally:
            self.git("worktree", "remove", "--force", work, check=False)
        self.git("push", "-q", self.remote, f"{merged}:refs/heads/{branch}")
        body = (f"Merge queue entry for proposal {item['id']} by {item['actorName']} (head {item['head'][:10]}), "
                f"merged onto hub main {self.tip[:10]}. The queue merges this when its gate passes on unchanged main; "
                f"the author's proposal is then recorded as merged.")
        out = self.hn("proposal", "open", "--base", self.tip, "--head", merged, "--body", body, f"Queue: {item['title']}"[:200])
        short = out.split("Opened proposal ", 1)[1].split(":", 1)[0]
        full = "sha256:" + next(r for r in self.git("for-each-ref", "--format=%(refname:lstrip=3)", "refs/hn/proposals").split() if r.startswith(short))
        self.hn("sync", self.remote, check=False)
        self.hn("run", "request", full, "gate")
        self.hn("sync", self.remote, check=False)
        return full

    def turn(self):
        self.refresh()
        # A queue proposal in flight: merge it, drop it, or wait for its gate.
        for orig, q in list(self.state["inflight"].items()):
            item, qi = self.by_id.get(orig), self.by_id.get(q["queue"])
            if not item or not qi or item["merged"] or item["lineage"]["merged"] or item["lineage"]["superseded"]:
                log(f"dropping queue entry for {orig[7:19]}: original merged, superseded or gone")
                self.state["inflight"].pop(orig)
                self.save()
                continue
            if qi["merged"]:
                self.state["inflight"].pop(orig)
                self.save()
                continue
            if q["tip"] != self.tip:
                log(f"main moved from {q['tip'][:10]} to {self.tip[:10]} under queue entry {q['queue'][7:19]}; rebuilding")
                self.state["inflight"].pop(orig)
                self.save()
                continue
            if self.failed(qi):
                self.bounce(item, f"the gate failed on the combined commit (queue proposal {q['queue'][7:19]})")
                return True
            cr = self.open_change_request(item)
            if cr:
                log(f"dropping queue entry for {orig[7:19]}: {cr} while its combined gate ran")
                self.state["inflight"].pop(orig)
                self.save()
                continue
            if self.passed_at(qi):
                log(f"gate passed on {q['queue'][7:19]} for {orig[7:19]}; merging")
                if self.automerge(q["queue"]):
                    self.state["inflight"].pop(orig)
                    self.save()
                return True
            return False  # wait for this gate; nothing else may move main
        for item in self.candidates():
            pid = item["id"]
            if item["base"] == self.tip:
                log(f"{pid[7:19]} ({item['actorName']}) passed on current main; merging it")
                self.automerge(pid)
                return True
            q = self.build(item)
            if not q:
                self.bounce(item, f"its head conflicts with hub main {self.tip[:10]}")
                return True
            log(f"{pid[7:19]} ({item['actorName']}) passed on {item['base'][:10]}; queue proposal {q[7:19]} gates it on {self.tip[:10]}")
            self.state["inflight"][pid] = {"queue": q, "tip": self.tip, "at": time.time()}
            self.save()
            return True
        return False

    def note(self, key, msg):
        """Log msg once per key until it changes."""
        seen = getattr(self, "_notes", {})
        if seen.get(key) != msg:
            log(msg)
        seen[key] = msg
        self._notes = seen

    def heartbeat(self):
        now = time.time()
        if now - getattr(self, "_beat", 0) < HEARTBEAT:
            return
        self._beat = now
        waiting = len([1 for w in self.want if any(i["id"][7:19] == w for i in self.by_id.values())])
        inflight = ", ".join(f"{o[7:19]} via {q['queue'][7:19]}" for o, q in self.state["inflight"].items()) or "none"
        log(f"alive: main {getattr(self, 'tip', '?')[:10]}, {waiting} lander(s) waiting, in flight: {inflight}")
        status = {"at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"), "main": getattr(self, "tip", ""),
                  "waiting": sorted(self.want), "inflight": self.state["inflight"]}
        tmp = self.state_path + ".status.tmp"
        json.dump(status, open(tmp, "w"), indent=1)
        os.replace(tmp, self.state_path.replace("merge-queue.json", "merge-queue-status.json"))

    def loop(self, once):
        while True:
            try:
                busy = self.turn()
                self.heartbeat()
            except Exception as error:  # keep serving; the next turn re-reads everything
                log(f"turn failed: {error}")
                busy = False
            if once:
                return
            time.sleep(5 if busy else self.interval)


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--clone", required=True)
    ap.add_argument("--remote", default="hub")
    ap.add_argument("--interval", type=int, default=60)
    ap.add_argument("--once", action="store_true")
    ap.add_argument("--max-age", type=int, default=6 * 3600, help="take only proposals whose gate passed this many seconds ago or less")
    ap.add_argument("--strict-jit-config", help="opt-in selected-author configuration; disables combined proposals")
    ap.add_argument("--strict-jit-client", help="absolute selected_author.py candidate path")
    a = ap.parse_args()
    if a.strict_jit_config:
        if not a.strict_jit_client or not os.path.isabs(a.strict_jit_client):
            ap.error("strict mode requires an absolute --strict-jit-client")
        gitdir = run(["git", "rev-parse", "--absolute-git-dir"], os.path.abspath(a.clone)).strip()
        legacy_path = os.path.join(gitdir, "merge-queue.json")
        if os.path.exists(legacy_path):
            with open(legacy_path) as stream:
                legacy = json.load(stream)
            if not isinstance(legacy, dict) or not isinstance(legacy.get("inflight"), dict) or legacy["inflight"]:
                raise RuntimeError("strict transition requires explicit reconciliation of legacy inflight state")
        # The existing sole queue service owns ordering. Each child is a
        # nonblocking admission turn; no gate or gate lock waits for admission.
        while True:
            result = subprocess.run([sys.executable, a.strict_jit_client, "coordinate", "--config", os.path.abspath(a.strict_jit_config)], cwd=os.path.abspath(a.clone))
            if result.returncode:
                log(f"strict JIT coordinator failed with exit {result.returncode}; admission stopped, operator attention required")
                return result.returncode
            if a.once:
                return 0
            time.sleep(a.interval)

    Queue(os.path.abspath(a.clone), a.remote, a.interval, a.max_age).loop(a.once)


if __name__ == "__main__":
    sys.exit(main())
