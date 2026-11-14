#!/usr/bin/env python3
"""One nonblocking turn of an opt-in, single-coordinator selected-author queue."""
import argparse
import fcntl
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import tempfile

PROTOCOL = "hn.selected-author/1"
TERMINAL = {"done", "failed", "blocked"}


class Refusal(RuntimeError):
    pass


class RootRefusal(Refusal):
    """An explicit proposal-local refusal, with no unknown authority failure."""
    pass


def output(status, **details):
    print(json.dumps(dict(status=status, **details)), flush=True)


def atomic_json(path, value):
    fd, tmp = tempfile.mkstemp(prefix=path.name + ".", dir=path.parent)
    try:
        with os.fdopen(fd, "w") as stream:
            json.dump(value, stream, sort_keys=True)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(tmp, path)
        directory = os.open(path.parent, os.O_RDONLY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        if os.path.exists(tmp):
            os.unlink(tmp)


class Session:
    def __init__(self, args):
        self.a = args
        self.dir = Path(self.git("rev-parse", "--git-common-dir")).resolve() / "jit-selected-author"
        self.dir.mkdir(exist_ok=True)
        self.actor = self.query("identity", "show", "--json")["actor"]
        self.selection = None
        self.comments = []
        # Worker locks handed to the runner, so they stay held while it or
        # its gate is alive even if this process is killed.
        self.held_locks = ()

    def run(self, *args, cwd=None):
        trace = os.environ.get("HN_JIT_TRACE") == "1"
        start = time.monotonic()
        if trace:
            print("jit command start: " + repr(args[:3]), file=sys.stderr, flush=True)
        executing = len(args) > 2 and args[1:3] == ("runner", "once")
        p = subprocess.run(args, cwd=cwd, capture_output=True, text=True,
                           timeout=None if executing else 90,
                           pass_fds=self.held_locks if executing else ())
        if trace:
            print(f"jit command end: {args[:3]!r} rc={p.returncode} elapsed={time.monotonic()-start:.3f}", file=sys.stderr, flush=True)
        if p.returncode:
            raise Refusal(f"{args[0:3]} exited {p.returncode}: {(p.stderr or p.stdout)[-1500:]}")
        return p.stdout.strip()

    def git(self, *args, cwd=None):
        return self.run("git", *args, cwd=cwd)

    def hn(self, *args):
        return self.run(self.a.hn, *args)

    def query(self, *args):
        obj = json.loads(self.hn(*args))
        if obj.get("ok") is not True or not isinstance(obj.get("data"), dict):
            raise Refusal("invalid hn JSON envelope")
        return obj["data"]

    def history(self):
        rows, cursor, snapshot = [], "", None
        while True:
            args = ["issue", "history", self.a.issue, "--kind", "comments", "--limit", "200", "--json"]
            if cursor:
                args += ["--cursor", cursor]
            obj = json.loads(self.hn(*args))
            if obj.get("ok") is not True or obj.get("schema") != "hn.issue/1":
                raise Refusal("invalid signed-history response")
            if snapshot is not None and snapshot != obj["snapshot"]:
                raise Refusal("history snapshot changed during pagination")
            snapshot = obj["snapshot"]
            rows.extend(obj["data"]["items"])
            cursor = obj.get("next_cursor", "")
            if not cursor:
                if len(rows) != obj["data"]["total"]:
                    raise Refusal("incomplete signed history")
                return rows

    def refresh(self):
        # A publication failure is an error. Unpublished local events survive in
        # this sole owner's clone and are reconciled by the next successful sync.
        self.hn("sync", self.a.remote)
        self.git("fetch", "-q", self.a.remote, "+refs/heads/main:refs/remotes/" + self.a.remote + "/main")
        self.tip = self.git("rev-parse", "refs/remotes/" + self.a.remote + "/main")
        data = self.query("proposal", "list", "--json")
        if not isinstance(data["items"], list):
            raise Refusal("invalid proposal list")
        self.items = {p["id"]: p for p in data["items"]}
        if len(self.items) != len(data["items"]):
            raise Refusal("duplicate proposal IDs")
        self.comments = []
        for row in self.history():
            try:
                value = json.loads(row.get("body", ""))
            except json.JSONDecodeError:
                continue
            if isinstance(value, dict) and value.get("protocol") == PROTOCOL:
                self.comments.append((row, value))
        selections = [(r, v) for r, v in self.comments if r["actor"] == self.a.coordinator and v.get("kind") == "selection"]
        selections.sort(key=lambda pair: pair[1]["generation"])
        previous, generation = "", 0
        self.selection = None
        for row, value in selections:
            if value["generation"] != generation + 1 or value["previous"] != previous:
                raise Refusal("coordinator scheduling chain fork or gap")
            if value["phase"] not in TERMINAL | {"prepare", "bound", "requested"}:
                raise Refusal("unknown selection phase")
            previous, generation = row["id"], value["generation"]
            self.selection = dict(value, event=row["id"])
        self.generation, self.previous = generation, previous

    def comment(self, value, operation):
        body = json.dumps(dict(value, protocol=PROTOCOL), sort_keys=True, separators=(",", ":"))
        result = self.query("issue", "comment", self.a.issue, "--actor", self.actor,
                            "--operation", operation, "--body", body, "--json")
        self.hn("sync", self.a.remote)
        return result["event_id"]

    def publish(self, phase, **details):
        if self.actor != self.a.coordinator:
            raise Refusal("only configured coordinator publishes selections")
        value = dict(kind="selection", phase=phase, generation=self.generation + 1,
                     previous=self.previous, **details)
        return self.comment(value, "jit-selection:" + self.a.issue[7:] + ":" + str(self.generation + 1))

    def current(self, root):
        if root not in self.items or self.items[root]["lineage"]["root"] != root:
            raise RootRefusal("intent must name the original lineage root")
        family = [p for p in self.items.values() if p["lineage"]["root"] == root]
        leaves = [p for p in family if not p["lineage"]["superseded"]]
        if len(leaves) != 1:
            raise RootRefusal("original lineage absent or ambiguous")
        return leaves[0]

    def check(self, item):
        if item.get("error"):
            raise Refusal(item["error"])
        result = self.query("jit-check", item["id"], "--target", self.tip,
                          "--requester", self.a.coordinator, "--remote", self.a.remote, "--rule", self.a.rule,
                          "--pipeline", self.a.pipeline,
                          *(["--runner", self.actor] if self.a.mode == "worker" else []))
        if result.get("scope") == "proposal" and result.get("id") == item["id"] and isinstance(result.get("refusal"), str):
            raise RootRefusal(result["refusal"])
        return result

    def binding(self, check):
        return dict({key: check[key] for key in ("root", "id", "author", "base", "head", "target", "policy", "definition")}, pipeline=self.a.pipeline, rule=self.a.rule)

    def same(self, selection, check):
        return all(selection.get(key) == value for key, value in self.binding(check).items())

    def has_intent(self, root, revision):
        author = self.items[root]["actor"]
        return any(row["actor"] == author and value.get("kind") == "intent" and
                   value.get("root") == root and value.get("id") == revision
                   for row, value in self.comments)

    def intent(self, root, revision):
        self.comment(dict(kind="intent", root=root, id=revision), "jit-intent-revision:" + revision[7:])

    def intents(self):
        roots = {value["root"] for row, value in self.comments
                 if value.get("kind") == "intent" and isinstance(value.get("root"), str) and
                 value["root"] in self.items and self.items[value["root"]]["actor"] == row["actor"]}
        eligible = []
        for root in sorted(roots, key=lambda root: (self.items[root]["timestamp"], root)):
            try:
                item = self.current(root)
            except RootRefusal as error:
                output("refused", root=root, reason=str(error)[:1500])
                continue
            if item["actor"] != self.items[root]["actor"]:
                output("refused", root=root, reason="current revision is not the original signed author")
                continue
            if not self.has_intent(root, item["id"]):
                output("refused", root=root, reason="fresh signed intent required for current revision")
                continue
            eligible.append(root)
        return eligible

    def owner(self):
        if self.actor != self.a.coordinator:
            raise Refusal("active identity is not configured coordinator")
        path = self.dir / "owner.json"
        desired = dict(owner=self.a.owner, issue=self.a.issue, actor=self.actor,
                       remote=self.a.remote, pipeline=self.a.pipeline, rule=self.a.rule)
        if path.exists():
            if json.loads(path.read_text()) != desired:
                raise Refusal("same-host coordinator ownership mismatch; explicit fenced migration required")
        else:
            atomic_json(path, desired)

    def coordinate(self):
        self.owner()
        self.refresh()
        if self.a.expect is not None and self.a.expect != self.previous:
            raise Refusal("stale coordinator selection expectation")
        s = self.selection
        if s and s["phase"] not in TERMINAL:
            try:
                return self.coordinate_active(s)
            except RootRefusal as error:
                self.publish("blocked", root=s["root"], id=s["id"], reasons=[str(error)[:1500]])
                return output("needs-attention", root=s["root"], reasons=[str(error)[:1500]])
        stopped = {v["id"] for r, v in self.comments if r["actor"] == self.a.coordinator and
                   v.get("kind") == "selection" and v.get("phase") == "failed"}
        for root in self.intents():
            item = self.current(root)
            if item["id"] in stopped or item["merged"] or item["lineage"]["merged"]:
                continue
            try:
                check = self.check(item)
            except RootRefusal as error:
                output("refused", root=root, reason=str(error)[:1500])
                continue
            if not check["eligible"]:
                output("refused", proposal=item["id"], reasons=check["blockers"])
                continue
            self.publish("prepare", **self.binding(check))
            return output("selected", root=root, proposal=item["id"])
        return output("idle")

    def coordinate_active(self, s):
        item = self.current(s["root"])
        if item["merged"] or item["lineage"]["merged"]:
            self.publish("done", **{k: v for k, v in s.items() if k in ("root", "id", "head", "target")})
            return output("merged", proposal=item["id"])
        check = self.check(item)
        # Reconcile a trusted failed result before any invalidation can
        # discard its identity or authorize an implicit rerun.
        if any(q["failed"] for q in check["requests"]):
            self.publish("failed", **self.binding(check))
            return output("failure", proposal=item["id"])
        if not check["eligible"]:
            self.publish("blocked", **self.binding(check), reasons=check["blockers"])
            return output("needs-attention", reasons=check["blockers"])
        if item["id"] != s["id"]:
            prepared = any(row["actor"] == check["author"] and value.get("kind") == "prepared" and
                           value.get("selection") == s["event"] and value.get("binding") == self.binding(check)
                           for row, value in self.comments)
            if not prepared and not self.has_intent(s["root"], item["id"]):
                raise RootRefusal("selected revision moved without fresh signed author intent or exact prepared acknowledgment")
        if s["phase"] == "prepare":
            if s["target"] != self.tip:
                self.publish("prepare", **self.binding(check))
                return output("invalidated", reason="main moved; selected root retained")
            ready = [(r, v) for r, v in self.comments if r["actor"] == check["author"] and
                     v.get("kind") == "prepared" and v.get("selection") == s["event"]]
            if not ready and check["id"] != s["id"]:
                self.publish("prepare", **self.binding(check))
                return output("invalidated", reason="original revision moved; selected root retained")
            if not ready:
                return output("waiting-for-selected-author", root=s["root"])
            if len(ready) != 1 or ready[0][1].get("binding") != self.binding(check) or check["base"] != self.tip:
                raise RootRefusal("selected author acknowledgment is ambiguous or does not match the current binding")
            self.publish("bound", **self.binding(check))
            return self.admit(check)
        if s["phase"] == "requested" and check["ready"] and self.contains(check["head"], self.tip):
            self.at_main(check["base"])
            self.hn("automerge", check["id"], "--rule", self.a.rule, "--execute", "--remote", self.a.remote, "--strict-jit-root", check["root"])
            self.hn("sync", self.a.remote)
            return output("merge-repaired", proposal=check["id"])
        if not self.same(s, check):
            self.publish("prepare", **self.binding(check))
            return output("invalidated", reason="main or original binding moved")
        if s["phase"] == "bound":
            return self.admit(check)
        matches = [q for q in check["requests"] if q["id"] == s["request"]]
        if len(matches) != 1:
            raise RootRefusal("signed admission request unavailable")
        if not check["ready"]:
            return output("waiting-for-gate", request=s["request"])
        # The hosted strict guard checks the authenticated root under the
        # same repository lease as the protected advancement.
        self.at_main(check["base"])
        self.hn("automerge", check["id"], "--rule", self.a.rule, "--execute", "--remote", self.a.remote, "--strict-jit-root", check["root"])
        self.hn("sync", self.a.remote)
        return output("merge-published", proposal=check["id"])

    def admit(self, check):
        # Re-read authenticated selection, code and policy after publication.
        self.refresh()
        current = self.check(self.current(check["root"]))
        if not self.selection or self.selection["phase"] != "bound":
            raise Refusal("authenticated bound selection unavailable")
        if not self.same(self.selection, current) or not current["eligible"] or current["base"] != self.tip:
            raise RootRefusal("binding changed before normal request")
        self.hn("run", "request", current["id"], self.a.pipeline, "--reuse")
        self.hn("sync", self.a.remote)
        self.refresh()
        current = self.check(self.current(check["root"]))
        # Reuse is admitted-history recovery, never a distributed reservation.
        if not self.same(self.selection, current) or len(current["requests"]) != 1:
            raise RootRefusal("request reconciliation is ambiguous or binding moved")
        self.publish("requested", **self.binding(current), request=current["requests"][0]["id"])
        return output("requested", request=current["requests"][0]["id"])

    def contains(self, head, tip):
        p = subprocess.run(["git", "merge-base", "--is-ancestor", head, tip], capture_output=True, text=True, timeout=30)
        if p.returncode not in (0, 1):
            raise Refusal("cannot establish main ancestry")
        return p.returncode == 0

    def at_main(self, base):
        if self.git("status", "--porcelain"):
            raise Refusal("main checkout must be clean")
        if self.git("branch", "--show-current") != "main":
            raise Refusal("coordinator must check out main")
        # Clone must be dedicated to this coordinator; never discard local work.
        old = self.git("rev-parse", "main")
        self.git("merge-base", "--is-ancestor", old, self.tip)
        self.git("reset", "-q", "--hard", base)

    def author(self):
        self.refresh()
        if self.a.proposal not in self.items:
            raise Refusal("proposal absent from successful list")
        root = self.items[self.a.proposal]["lineage"]["root"]
        try:
            item = self.current(root)
        except RootRefusal as error:
            return output("needs-attention", reason=str(error)[:1500])
        if item["actor"] != self.actor:
            raise Refusal("only original author prepares lineage")
        if item["lineage"]["merged"] or item["merged"]:
            return output("merged", proposal=item["id"])
        try:
            check = self.check(item)
        except RootRefusal as error:
            return output("needs-attention", reason=str(error)[:1500])
        if not check["eligible"]:
            return output("needs-attention", reasons=check["blockers"])
        self.intent(root, item["id"])
        s = self.selection
        if not s or s["phase"] != "prepare" or s["root"] != root:
            return output("waiting", proposal=item["id"])
        if s["target"] != self.tip:
            return output("invalidated", reason="main moved before preparation")
        planfile = self.dir / "preparation.json"
        plan = json.loads(planfile.read_text()) if planfile.exists() else None
        if s["id"] != item["id"] and not (plan and plan["selection"] == s["event"] and plan["head"] == item["head"] and plan["base"] == item["base"]):
            return output("invalidated", reason="original revision moved before preparation")
        if item["base"] != self.tip:
            if not plan or plan["selection"] != s["event"]:
                work = self.dir / "prepare-worktree"
                if work.exists():
                    return output("needs-attention", reason="unfinished preparation worktree; inspect conflict before retry")
                self.git("worktree", "add", "-q", "--detach", str(work), item["head"])
                try:
                    try:
                        self.git("merge", "--no-edit", self.tip, cwd=str(work))
                    except Refusal as error:
                        return output("needs-attention", reason="selected preparation failed: " + str(error))
                    head = self.git("rev-parse", "HEAD", cwd=str(work))
                    plan = dict(selection=s["event"], base=self.tip, head=head)
                    atomic_json(planfile, plan)
                finally:
                    # A conflicting worktree remains as useful failure evidence.
                    if not self.git("status", "--porcelain", cwd=str(work)):
                        self.git("worktree", "remove", str(work))
            self.hn("proposal", "revise", item["id"], "--base", plan["base"], "--head", plan["head"], "--body", "Selected-author preparation " + s["event"])
            self.hn("sync", self.a.remote)
            self.refresh()
            item = self.current(root)
            check = self.check(item)
        if not check["eligible"]:
            return output("needs-attention", reasons=check["blockers"])
        # A concurrent main move cannot turn prepared code into a grant.
        if self.previous != s["event"] or self.tip != s["target"] or check["base"] != self.tip or not check["eligible"]:
            return output("invalidated", reason="selection/main/eligibility changed after preparation")
        self.intent(root, item["id"])
        self.comment(dict(kind="prepared", selection=s["event"], binding=self.binding(check)), "jit-prepared:" + s["event"][7:])
        return output("prepared", proposal=item["id"])

    def worker(self):
        self.refresh()
        s = self.selection
        if not s or s["phase"] != "requested":
            return output("waiting", reason="no authenticated admitted request")
        check = self.check(self.current(s["root"]))
        if not self.same(s, check) or not check["eligible"] or check["base"] != self.tip:
            return output("refused", reason="stale target, binding or eligibility")
        if len(check["requests"]) != 1 or check["requests"][0]["id"] != s["request"]:
            raise Refusal("unexpected coordinator requests; no execution")
        args = ["runner", "once", "--remote", self.a.remote, "--accept-pipeline", self.a.pipeline,
                "--accept-actor", self.a.coordinator, "--require-requester-policy", "--strict-target",
                "--skip-stale-base", "main", "--only-request", s["request"], "--backend", self.a.backend]
        if self.a.allow_unsafe_host_execution:
            args += ["--allow-unsafe-host-execution"]
        # No global accept-file append and no user-supplied runner passthrough.
        result = self.hn(*args)
        return output("worker", result=result)


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("mode", choices=["coordinate", "author", "worker"])
    p.add_argument("--hn", default="hn")
    p.add_argument("--remote", default="hub")
    p.add_argument("--config")
    p.add_argument("--issue")
    p.add_argument("--coordinator")
    p.add_argument("--pipeline", default="gate")
    p.add_argument("--rule", default="ordinary")
    p.add_argument("--owner")
    p.add_argument("--expect")
    p.add_argument("--proposal")
    p.add_argument("--host-lock-dir")
    p.add_argument("--backend", choices=["sandbox", "host"], default="sandbox")
    p.add_argument("--allow-unsafe-host-execution", action="store_true")
    early = argparse.ArgumentParser(add_help=False)
    early.add_argument("--config")
    config, _ = early.parse_known_args()
    if config.config:
        values = json.loads(Path(config.config).read_text())
        allowed = {"hn", "remote", "issue", "coordinator", "pipeline", "rule", "owner"}
        if not isinstance(values, dict) or set(values) - allowed or any(not isinstance(v, str) or not v for v in values.values()):
            raise Refusal("invalid strict JIT configuration")
        p.set_defaults(**values)
    a = p.parse_args()
    if not a.issue or not a.coordinator:
        p.error("explicit issue and coordinator are required, directly or through --config")
    if a.mode == "coordinate" and not a.owner or a.mode == "author" and not a.proposal:
        p.error("coordinate requires --owner; author requires --proposal")
    session = Session(a)
    # Same-host clone ownership only. Central service configuration supplies
    # global ordering; copying the signing identity to a new host is unsupported.
    with (session.dir / "turn.lock").open("a+") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        if a.mode == "worker" and a.host_lock_dir:
            host = Path(a.host_lock_dir).resolve()
            host.mkdir(parents=True, exist_ok=True)
            with (host / "runners.lock").open("a+") as execution:
                fcntl.flock(execution, fcntl.LOCK_EX | fcntl.LOCK_NB)
                with (host / "gate.lock").open("a+") as gate:
                    fcntl.flock(gate, fcntl.LOCK_EX | fcntl.LOCK_NB)
                    fcntl.flock(gate, fcntl.LOCK_UN)
                # flock belongs to the open file, so the runner and the gate it
                # starts keep both locks until they have all exited.
                session.held_locks = (lock.fileno(), execution.fileno())
                session.worker()
        else:
            getattr(session, a.mode)()


if __name__ == "__main__":
    try:
        main()
    except (Refusal, ValueError, KeyError, OSError, subprocess.SubprocessError) as error:
        output("error", reason=str(error))
        sys.exit(1)
