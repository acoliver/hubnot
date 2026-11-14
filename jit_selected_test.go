package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func selectedFixture(t *testing.T) *serveGovernedFixture {
	g := newServeGovernedFixture(t, serveGovernedOptions{AutomergePaths: []string{"auto/"}, ProtectedPaths: []string{"protected/"}})
	var policy map[string]any
	if err := json.Unmarshal([]byte(g.Policy.JSON(t)), &policy); err != nil {
		t.Fatal(err)
	}
	policy["pipelines"].(map[string]any)["test"].(map[string]any)["requesters"] = []string{g.actors["R"]}
	policy["proposals"].(map[string]any)["trustedReviewers"] = []string{g.actors["M"]}
	policy["proposals"].(map[string]any)["requiredApprovals"] = 0
	rule := policy["automerge"].(map[string]any)["rules"].([]any)[0].(map[string]any)
	rule["acceptance"] = "rule"
	rule["actors"] = []string{g.actors["R"]}
	rule["authors"] = []string{g.actors["A"], g.actors["U"]}
	raw, _ := json.Marshal(policy)
	base := g.Commit(g.Base, map[string]string{".hn/policy.json": string(raw) + "\n"})
	g.Host(base)
	g.SetMainForTest(base)
	g.Base = base
	for _, who := range []string{"A", "M", "R", "U"} {
		g.git(who, "fetch", "-q", "origin")
		g.git(who, "checkout", "-q", "-B", "main", "origin/main")
		g.hn(who, "replication", "select", "origin", "--all-trusted")
	}
	return g
}

func selectedSummary(out string) string {
	lines := []string{}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "{") {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

type selectedDriver struct {
	t     *testing.T
	g     *serveGovernedFixture
	issue string
}

func newSelectedDriver(t *testing.T, g *serveGovernedFixture) *selectedDriver {
	var out struct {
		Data struct {
			ID string `json:"issue_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(g.hn("R", "issue", "open", "--json", "--operation", "jit-fixture", "Scheduling only")), &out); err != nil {
		t.Fatal(err)
	}
	g.hn("R", "sync", "origin")
	return &selectedDriver{t, g, out.Data.ID}
}
func (d *selectedDriver) args(mode string, extra ...string) []string {
	args := []string{"selected_author.py", mode, "--hn", d.g.h.Binary, "--remote", "origin", "--issue", d.issue, "--coordinator", d.g.actors["R"], "--pipeline", "test", "--rule", "auto"}
	return append(args, extra...)
}
func (d *selectedDriver) call(who, mode string, extra ...string) (string, int) {
	d.t.Helper()
	script, err := filepath.Abs("selected_author.py")
	if err != nil {
		d.t.Fatal(err)
	}
	args := d.args(mode, extra...)
	args[0] = script
	python, err := exec.LookPath("python3")
	if err != nil {
		d.t.Fatal(err)
	}
	out, rc, err := d.g.advanceExec(who, []string{"HN_JIT_TRACE=1"}, python, args...)
	if err != nil {
		d.t.Fatal(err)
	}
	return out, rc
}
func (d *selectedDriver) ok(who, mode string, extra ...string) string {
	d.t.Helper()
	out, rc := d.call(who, mode, extra...)
	if rc != 0 {
		d.t.Fatalf("%s %s rc=%d: %s", who, mode, rc, out)
	}
	d.t.Log(selectedSummary(out))
	return out
}
func (d *selectedDriver) config() string {
	d.t.Helper()
	config := map[string]string{"hn": d.g.h.Binary, "remote": "origin", "issue": d.issue, "coordinator": d.g.actors["R"], "pipeline": "test", "rule": "auto", "owner": "fixture-owner"}
	path := filepath.Join(d.t.TempDir(), "strict-jit.json")
	data, _ := json.Marshal(config)
	if err := os.WriteFile(path, data, 0600); err != nil {
		d.t.Fatal(err)
	}
	return path
}
func (d *selectedDriver) step() string {
	d.t.Helper()
	queue, err := filepath.Abs("integration/merge-queue.py")
	if err != nil {
		d.t.Fatal(err)
	}
	client, err := filepath.Abs("selected_author.py")
	if err != nil {
		d.t.Fatal(err)
	}
	out, rc, err := d.g.advanceExec("R", []string{"HN_JIT_TRACE=1"}, "python3", queue, "--clone", d.g.work["R"], "--once", "--strict-jit-config", d.config(), "--strict-jit-client", client)
	if err != nil {
		d.t.Fatal(err)
	}
	if rc != 0 {
		d.t.Fatalf("actual queue adapter rc=%d: %s", rc, out)
	}
	d.t.Log(selectedSummary(out))
	return out
}
func (d *selectedDriver) land(who, pid, head string) string {
	d.t.Helper()
	g := d.g
	g.git(who, "branch", "waiting-topic", head)
	g.git(who, "checkout", "-q", "main")
	land, err := filepath.Abs("integration/land.sh")
	if err != nil {
		d.t.Fatal(err)
	}
	client, err := filepath.Abs("selected_author.py")
	if err != nil {
		d.t.Fatal(err)
	}
	c := g.clients[who]
	c.Env = append(append([]string{}, c.Env...), "HUBNOT_STRICT_JIT_CONFIG="+d.config(), "HUBNOT_STRICT_JIT_CLIENT="+client, "PATH="+filepath.Dir(g.h.Binary)+":"+os.Getenv("PATH"))
	out, rc := c.exec(g.work[who], "bash", land, pid, "--branch", "waiting-topic", "--jit-once")
	if rc != 0 {
		d.t.Fatalf("actual land adapter rc=%d: %s", rc, out)
	}
	d.t.Log(selectedSummary(out))
	return out
}
func (d *selectedDriver) author(who, pid string) string {
	return d.ok(who, "author", "--proposal", pid)
}
func (d *selectedDriver) worker() string {
	d.t.Helper()
	script, err := filepath.Abs("integration/runner-loop.sh")
	if err != nil {
		d.t.Fatal(err)
	}
	client, err := filepath.Abs("selected_author.py")
	if err != nil {
		d.t.Fatal(err)
	}
	env := []string{"HN_JIT_TRACE=1", "HUBNOT_STRICT_JIT_CONFIG=" + d.config(), "HUBNOT_STRICT_JIT_CLIENT=" + client, "RD_GATE_STATE=" + filepath.Join(d.g.clients["U"].Home, "owned-gate"), "RD_STRICT_JIT_ONCE=1", "RD_STRICT_JIT_BACKEND=host", "RD_STRICT_JIT_ALLOW_HOST=1"}
	out, rc, err := d.g.advanceExec("U", env, "bash", script, "1")
	if err != nil || rc != 0 {
		d.t.Fatalf("actual worker adapter rc=%d err=%v: %s", rc, err, out)
	}
	d.t.Log(selectedSummary(out))
	return out
}
func (d *selectedDriver) count(who, pid string) int {
	d.g.hn(who, "sync", "origin")
	return len(jitProposal(d.t, d.g, who, pid).Requests)
}

func TestSelectedAuthorWaitingAndRecovery(t *testing.T) {
	g := selectedFixture(t)
	jitRecordBinary(t, g)
	d := newSelectedDriver(t, g)
	a := g.OpenProposal(g.Base, g.Commit(g.Base, map[string]string{"auto/a.txt": "a\n"}))
	g.Author = "U"
	b := g.OpenProposal(g.Base, g.Commit(g.Base, map[string]string{"auto/b.txt": "b\n"}))
	g.Author = "A"
	if out := d.land("U", b.ID, b.Head); !strings.Contains(out, "waiting") {
		t.Fatal(out)
	}
	if d.count("R", b.ID) != 0 || g.git("U", "rev-parse", "waiting-topic") != b.Head {
		t.Fatal("waiting lander rebased or requested a gate")
	}
	d.author("A", a.ID)
	// Each step is a fresh OS process: selection, request and result survive restart.
	d.step()
	if out, rc := d.call("R", "coordinate", "--owner", "fixture-owner", "--expect", d.issue); rc == 0 {
		t.Fatal("stale generation expectation accepted: " + out)
	}
	d.author("A", a.ID)
	d.step()
	if d.count("R", a.ID) != 1 {
		t.Fatal("selected author did not receive exactly one request")
	}
	d.step()
	if d.count("R", a.ID) != 1 {
		t.Fatal("restart duplicated request")
	}
	g.Author = "U"
	current := g.Revise(b.ID, g.Base, b.Head)
	g.Author = "A"
	before := jitProposal(t, g, "U", current.ID)
	rootBefore := jitProposal(t, g, "U", b.ID)
	headBefore := g.git("U", "rev-parse", "HEAD")
	branchBefore := g.git("U", "rev-parse", "waiting-topic")
	requestsBefore := d.count("R", current.ID)
	if requestsBefore != 0 {
		t.Fatal("waiting revision already has a request")
	}
	if out := d.author("U", b.ID); !strings.Contains(out, `"status": "waiting"`) {
		t.Fatal(out)
	}
	after := jitProposal(t, g, "U", current.ID)
	rootAfter := jitProposal(t, g, "U", b.ID)
	if after.ID != before.ID || after.Base != before.Base || after.Head != before.Head || len(after.Lineage.Successors) != 0 ||
		!reflect.DeepEqual(after.Lineage, before.Lineage) || !reflect.DeepEqual(rootAfter.Lineage, rootBefore.Lineage) ||
		g.git("U", "rev-parse", "HEAD") != headBefore || g.git("U", "rev-parse", "waiting-topic") != branchBefore ||
		d.count("R", current.ID) != requestsBefore || d.count("R", b.ID) != 0 {
		t.Fatal("waiting author revised/rebased/requested before turn")
	}
	// Wrong same-host owner cannot take over, even after the former process exited.
	if out, rc := d.call("R", "coordinate", "--owner", "stale-owner"); rc == 0 {
		t.Fatal("stale owner admitted: " + out)
	}
	d.worker()
	d.step()
	d.step()
	if g.ServerTip("main") != a.Head {
		t.Fatal("first original did not advance")
	}
	d.step()
	d.author("U", b.ID)
	d.step()
	d.worker()
	d.step()
	d.step()
	g.hn("M", "sync", "origin")
	second := jitProposal(t, g, "M", b.ID)
	for len(second.Lineage.Successors) == 1 {
		second = jitProposal(t, g, "M", second.Lineage.Successors[0])
	}
	if !second.Merged || second.Lineage.Root != b.ID {
		t.Fatalf("second original did not progress: %+v", second)
	}
	if got := g.git("R", "show", "origin/main:auto/b.txt"); got != "b" {
		t.Fatal(got)
	}
	t.Log("real serve: both original lineages merged; waiting author produced no revision or gate; restart did not duplicate requests")
}

func TestSelectedAuthorInvalidationVetoAndFailure(t *testing.T) {
	g := selectedFixture(t)
	d := newSelectedDriver(t, g)
	a := g.OpenProposal(g.Base, g.Commit(g.Base, map[string]string{"auto/a.txt": "a\n"}))
	d.author("A", a.ID)
	d.step()
	// An external writer moves main while the selection is active.
	external := g.Commit(g.Base, map[string]string{"auto/external.txt": "external\n"})
	g.Host(external)
	g.SetMainForTest(external)
	if out := d.author("A", a.ID); !strings.Contains(out, "invalidated") {
		t.Fatal(out)
	}
	if d.count("R", a.ID) != 0 {
		t.Fatal("stale selection requested")
	}
	d.step()
	g.Revise(a.ID, g.Base, a.Head)
	if out := d.author("A", a.ID); !strings.Contains(out, "invalidated") {
		t.Fatal(out)
	}
	d.step()
	d.author("A", a.ID)
	d.step()
	g.hn("R", "sync", "origin")
	original := jitProposal(t, g, "R", a.ID)
	if len(original.Lineage.Successors) != 1 {
		t.Fatal("selected author failed to revise original")
	}
	current := original.Lineage.Successors[0]
	for {
		next := jitProposal(t, g, "R", current)
		if len(next.Lineage.Successors) == 0 {
			break
		}
		if len(next.Lineage.Successors) != 1 {
			t.Fatal("ambiguous revision")
		}
		current = next.Lineage.Successors[0]
	}
	if d.count("R", current) != 1 {
		t.Fatal("reprepared original lacks gate")
	}
	if err := os.WriteFile(g.CIFailFlag, []byte("fail"), 0600); err != nil {
		t.Fatal(err)
	}
	d.worker()
	d.step()
	d.step()
	if d.count("R", current) != 1 || g.ServerTip("main") != external {
		t.Fatal("failed gate reran or merged")
	}
	if out := d.author("A", a.ID); !strings.Contains(out, "failure") {
		t.Fatal("failure hidden: " + out)
	}
	// A signed message by another actor cannot select a second proposal.
	fake := `{"protocol":"hn.selected-author/1","kind":"selection","generation":999,"phase":"prepare"}`
	g.act("A", "issue.comment", "issue", "comment", d.issue, "--body", fake)
	d.step()
	if d.count("R", current) != 1 {
		t.Fatal("spoofed selection affected request")
	}
	t.Log("real serve: main invalidation required selected re-preparation; trusted failure survived coordinator restart; forged scheduling actor ignored")
}

func TestSelectedAuthorRefusesOriginalVetoAndProtected(t *testing.T) {
	g := selectedFixture(t)
	d := newSelectedDriver(t, g)
	a := g.OpenProposal(g.Base, g.Commit(g.Base, map[string]string{"auto/a.txt": "a\n"}))
	protected := g.OpenProposal(g.Base, g.Commit(g.Base, map[string]string{"protected/x": "x\n"}))
	d.author("A", a.ID)
	d.author("A", protected.ID)
	g.Reject(a.ID)
	d.step()
	if d.count("R", a.ID) != 0 || d.count("R", protected.ID) != 0 {
		t.Fatal("veto/protected admission bypass")
	}
	if out := d.author("A", a.ID); !strings.Contains(out, "rejection") {
		t.Fatal(out)
	}
	// No independent combined lineage can hide this original rejection.
	if g.ServerTip("main") != g.Base {
		t.Fatal("refused original moved main")
	}
	// Current target policy removes this author while its admitted gate waits.
	c := g.OpenProposal(g.Base, g.Commit(g.Base, map[string]string{"auto/c.txt": "c\n"}))
	d.author("A", c.ID)
	d.step()
	d.author("A", c.ID)
	d.step()
	g.hn("R", "sync", "origin")
	var policy map[string]any
	if err := json.Unmarshal([]byte(g.git("A", "show", g.Base+":.hn/policy.json")), &policy); err != nil {
		t.Fatal(err)
	}
	rule := policy["automerge"].(map[string]any)["rules"].([]any)[0].(map[string]any)
	rule["authors"] = []string{g.actors["U"]}
	raw, _ := json.Marshal(policy)
	removed := g.Commit(g.Base, map[string]string{".hn/policy.json": string(raw) + "\n"})
	g.Host(removed)
	g.SetMainForTest(removed)
	if out := d.worker(); !strings.Contains(out, "refused") {
		t.Fatal("changed policy ran gate: " + out)
	}
	g.hn("M", "sync", "origin")
	if item := jitProposal(t, g, "M", c.ID); len(item.Requests) != 1 || len(item.Requests[0].Results) != 0 {
		t.Fatal("policy refusal executed")
	}
	// Fixture administrator restores the baseline, then selection incompleteness
	// must be an error rather than absence or an idle queue.
	g.SetMainForTest(g.Base)
	policy["automerge"].(map[string]any)["rules"].([]any)[0].(map[string]any)["authors"] = []string{g.actors["A"], g.actors["U"]}
	policy["pipelines"].(map[string]any)["test"].(map[string]any)["trustedRunners"] = []string{g.actors["M"]}
	trustRaw, _ := json.Marshal(policy)
	trustTip := g.Commit(g.Base, map[string]string{".hn/policy.json": string(trustRaw) + "\n"})
	g.Host(trustTip)
	g.SetMainForTest(trustTip)
	if out := d.worker(); !strings.Contains(out, "refused") {
		t.Fatal("untrusted worker ran: " + out)
	}
	g.SetMainForTest(g.Base)
	g.act("M", "review.submit", "review", c.ID, "--request-changes", "--body", "Hold this original")
	if out := d.worker(); !strings.Contains(out, "refused") {
		t.Fatal("change request ran gate: " + out)
	}
	g.act("M", "review.submit", "review", c.ID, "--approve", "--body", "Answered on this original")
	g.hn("R", "replication", "select", "origin", "--actor", g.actors["R"])
	if out, rc := d.call("R", "coordinate", "--owner", "fixture-owner"); rc == 0 {
		t.Fatal("omitted trust selection treated as none: " + out)
	}
	g.hn("R", "replication", "select", "origin", "--all-trusted")
	d.worker()
	g.Reject(c.ID)
	d.step()
	g.git("R", "checkout", "-q", "main")
	g.git("R", "reset", "-q", "--hard", g.Base)
	if out, rc := g.clients["R"].exec(g.work["R"], g.h.Binary, "automerge", c.ID, "--rule", "auto", "--execute", "--remote", "origin"); rc == 0 {
		t.Fatal("ordinary merge ignored original rejection: " + out)
	}
	if g.ServerTip("main") != g.Base {
		t.Fatal("late veto advanced original")
	}
	t.Log("real serve: target-policy author removal and incomplete trust selection refused; original rejection after passing gate blocked ordinary advancement")
	t.Log("real serve: original rejection and protected proposal refused before gate; protected route remains manual review")
}
