package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fix2Want(t *testing.T, out, want string) {
	t.Helper()
	if !strings.Contains(out, want) {
		t.Fatalf("wanted %q in %s", want, selectedSummary(out))
	}
}
func fix2Intent(root, revision string) string {
	return fmt.Sprintf(`{"protocol":"hn.selected-author/1","kind":"intent","root":%q,"id":%q}`, root, revision)
}
func fix2Comment(t *testing.T, d *selectedDriver, who, body string) {
	t.Helper()
	d.g.act(who, "issue.comment", "issue", "comment", d.issue, "--body", body)
}
func fix2B(t *testing.T, g *serveGovernedFixture) operationalEvent {
	t.Helper()
	g.Author = "U"
	b := g.OpenProposal(g.Base, g.Commit(g.Base, map[string]string{"auto/b.txt": "b\n"}))
	g.Author = "A"
	return b
}
func fix2Merge(t *testing.T, d *selectedDriver, who, root, path string) {
	t.Helper()
	fix2Want(t, d.author(who, root), `"status": "prepared"`)
	fix2Want(t, d.step(), `"status": "requested"`)
	d.worker()
	fix2Want(t, d.step(), `"status": "merge-published"`)
	d.step()
	if got := d.g.git("R", "show", "origin/main:"+path); got == "" {
		t.Fatal("missing merged content")
	}
}
func fix2Blocked(t *testing.T, d *selectedDriver, root string) {
	t.Helper()
	d.g.hn("M", "sync", "origin")
	raw := d.g.hn("M", "issue", "history", d.issue, "--kind", "comments", "--json")
	var obj struct {
		Data struct {
			Items []struct{ Actor, Body string }
		}
	}
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		t.Fatal(err)
	}
	for _, r := range obj.Data.Items {
		var v map[string]any
		json.Unmarshal([]byte(r.Body), &v)
		if r.Actor == d.g.actors["R"] && v["kind"] == "selection" && v["phase"] == "blocked" && v["root"] == root {
			return
		}
	}
	t.Fatal("missing signed blocked outcome for selected invalid root")
}
func TestFix2PoisonIntent(t *testing.T) {
	g := selectedFixture(t)
	jitRecordBinary(t, g)
	d := newSelectedDriver(t, g)
	a := g.OpenProposal(g.Base, g.Commit(g.Base, map[string]string{"auto/a": "a\n"}))
	b := fix2B(t, g)
	fix2Comment(t, d, "U", fix2Intent(a.ID, a.ID))
	fix2Want(t, d.step(), `"status": "idle"`)
	r := g.Revise(a.ID, g.Base, a.Head)
	fix2Comment(t, d, "A", fix2Intent(r.ID, r.ID))
	d.author("U", b.ID)
	out := d.step()
	fix2Want(t, out, `"status": "refused"`)
	fix2Want(t, out, `"root": "`+b.ID+`"`)
	fix2Merge(t, d, "U", b.ID, "auto/b.txt")
	if d.count("R", r.ID) != 0 {
		t.Fatal("invalid root admitted")
	}
	t.Log("CONTROL foreign signed intent idle; own nonroot intent visibly refused; unrelated B merged")
}
func TestFix2ForkIsolation(t *testing.T) {
	for _, selected := range []bool{false, true} {
		t.Run(fmt.Sprint(selected), func(t *testing.T) {
			g := selectedFixture(t)
			d := newSelectedDriver(t, g)
			a := g.OpenProposal(g.Base, g.Commit(g.Base, map[string]string{"auto/a": "a\n"}))
			b := fix2B(t, g)
			d.author("A", a.ID)
			if selected {
				d.step()
			}
			g.Revise(a.ID, g.Base, a.Head)
			g.hn("A", "proposal", "revise", a.ID, "--base", g.Base, "--head", a.Head, "--body", "honest sibling")
			g.hn("A", "sync", "origin")
			d.author("U", b.ID)
			out := d.step()
			if selected {
				fix2Want(t, out, `"status": "needs-attention"`)
				fix2Blocked(t, d, a.ID)
				out = d.step()
			} else {
				fix2Want(t, out, `"status": "refused"`)
			}
			fix2Want(t, out, `"root": "`+b.ID+`"`)
			fix2Merge(t, d, "U", b.ID, "auto/b.txt")
			if d.count("R", a.ID) != 0 {
				t.Fatal("fork admitted")
			}
			t.Log("real signed fork isolated; unrelated B merged; selected fork has signed blocked outcome")
		})
	}
}
func TestFix2FreshIntentAfterFailedRevision(t *testing.T) {
	g := selectedFixture(t)
	d := newSelectedDriver(t, g)
	a := g.OpenProposal(g.Base, g.Commit(g.Base, map[string]string{"auto/a": "a\n"}))
	b := fix2B(t, g)
	d.author("A", a.ID)
	d.author("U", b.ID)
	d.step()
	d.author("A", a.ID)
	d.step()
	if err := os.WriteFile(g.CIFailFlag, []byte("fail"), 0600); err != nil {
		t.Fatal(err)
	}
	d.worker()
	fix2Want(t, d.step(), `"status": "failure"`)
	fix2Want(t, d.author("A", a.ID), `"status": "needs-attention"`)
	r := g.Revise(a.ID, g.Base, a.Head)
	d.author("U", b.ID)
	out := d.step()
	fix2Want(t, out, `"root": "`+b.ID+`"`)
	if d.count("R", r.ID) != 0 {
		t.Fatal("stale intent granted new manual revision")
	}
	os.Remove(g.CIFailFlag)
	fix2Merge(t, d, "U", b.ID, "auto/b.txt")
	fix2Want(t, d.step(), `"status": "idle"`)
	d.author("A", a.ID)
	fix2Want(t, d.step(), `"root": "`+a.ID+`"`)
	fix2Merge(t, d, "A", a.ID, "auto/a")
	if d.count("R", a.ID) != 1 {
		t.Fatal("failed exact revision implicitly retried")
	}
	t.Log("old intent did not select manual revision after failed gate; live B merged; explicit fresh A intent later merged original lineage")
}
func TestFix2ApprovalThresholdReprepare(t *testing.T) {
	g := selectedFixture(t)
	var policy map[string]any
	json.Unmarshal([]byte(g.git("A", "show", g.Base+":.hn/policy.json")), &policy)
	policy["proposals"].(map[string]any)["requiredApprovals"] = 1
	raw, _ := json.Marshal(policy)
	base := g.Commit(g.Base, map[string]string{".hn/policy.json": string(raw) + "\n"})
	g.Host(base)
	g.SetMainForTest(base)
	g.Base = base
	for _, who := range []string{"A", "M", "R", "U"} {
		g.git(who, "fetch", "-q", "origin")
		g.git(who, "checkout", "-q", "-B", "main", "origin/main")
	}
	d := newSelectedDriver(t, g)
	a := g.OpenProposal(base, g.Commit(base, map[string]string{"auto/a": "a\n"}))
	fix2Want(t, d.author("A", a.ID), `"status": "needs-attention"`)
	if d.count("R", a.ID) != 0 {
		t.Fatal("threshold bypass before original approval")
	}
	g.act("M", "review.submit", "review", a.ID, "--approve", "--body", "original exact approval")
	d.author("A", a.ID)
	d.step()
	next := g.Commit(base, map[string]string{"auto/external": "external\n"})
	g.Host(next)
	g.SetMainForTest(next)
	fix2Want(t, d.step(), `"status": "invalidated"`)
	out := d.author("A", a.ID)
	t.Log(selectedSummary(out))
	r := g.latest("A")
	if r.ID == a.ID {
		t.Fatal("fixture did not create new revision")
	}
	fix2Want(t, d.step(), `"status": "needs-attention"`)
	fix2Blocked(t, d, a.ID)
	d.worker()
	if d.count("R", r.ID) != 0 || g.ServerTip("main") != next {
		t.Fatal("old approval carried to prepared revision")
	}
	fix2Want(t, d.author("A", a.ID), `"status": "needs-attention"`)
	g.act("M", "review.submit", "review", r.ID, "--approve", "--body", "legitimate current revision approval")
	d.author("A", a.ID)
	fix2Want(t, d.step(), `"status": "selected"`)
	fix2Merge(t, d, "A", a.ID, "auto/a")
	t.Log("requiredApprovals=1: old exact approval did not carry; signed blocked outcome, no gate/merge; current revision reviewer approval restored liveness")
}
func TestFix2QueueNonOnceErrorVisible(t *testing.T) {
	g := selectedFixture(t)
	d := newSelectedDriver(t, g)
	// A syntactically valid config with the wrong owner exercises the real child error.
	d.step()
	var config map[string]string
	data, _ := os.ReadFile(d.config())
	json.Unmarshal(data, &config)
	config["owner"] = "wrong-owner"
	path := filepath.Join(t.TempDir(), "wrong-owner.json")
	data, _ = json.Marshal(config)
	os.WriteFile(path, data, 0600)
	queue, _ := filepath.Abs("integration/merge-queue.py")
	client, _ := filepath.Abs("selected_author.py")
	out, rc, err := g.advanceExec("R", nil, "python3", queue, "--clone", g.work["R"], "--strict-jit-config", path, "--strict-jit-client", client)
	if err != nil || rc == 0 {
		t.Fatalf("non-once coordinator error not surfaced rc=%d err=%v: %s", rc, err, out)
	}
	fix2Want(t, out, "strict JIT coordinator failed")
	t.Log("non-once queue exits nonzero with explicit coordinator error")
}
