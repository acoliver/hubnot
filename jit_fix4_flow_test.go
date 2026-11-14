package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type vrControl struct {
	extra      []string
	t          *testing.T
	g          *serveGovernedFixture
	d          *selectedDriver
	root, name string
	seq        int
}

func vrNew(t *testing.T, name string) *vrControl {
	root := os.Getenv("HN_FIX3_LV_ROOT")
	if root == "" {
		t.Skip("explicit isolated synthetic root required")
	}
	c := &vrControl{t: t, root: root, name: name}
	return c
}
func (c *vrControl) init() {
	t := c.t
	c.g = selectedFixture(t)
	c.d = newSelectedDriver(t, c.g)
	raw, e := os.ReadFile(c.g.h.Binary)
	if e != nil {
		t.Fatal(e)
	}
	c.save("fixture-native", map[string]any{"sha256": fmt.Sprintf("%x", sha256.Sum256(raw))})
	c.save("fixture", map[string]any{"actors": c.g.actors, "issue": c.d.issue, "base": c.g.Base, "hosted": c.g.hosted, "work": c.g.work, "native": c.g.h.Binary, "strategy": "one fresh existing small fixture; no relocated authorized-key paths", "server_home": c.g.h.Home, "clients": c.g.clients, "tmpdir": os.Getenv("TMPDIR")})
	return
}
func (c *vrControl) save(name string, v any) {
	c.t.Helper()
	raw, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		c.t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(c.root, "evidence", c.name+"-"+name+".json"), append(raw, '\n'), 0600); e != nil {
		c.t.Fatal(e)
	}
}
func (c *vrControl) run(name, who, program string, args ...string) (string, string, int) {
	c.t.Helper()
	c.save("phase", map[string]any{"next": name, "who": who, "argv": append([]string{program}, args...)})
	cmd := exec.Command(program, args...)
	cmd.Dir = c.g.work[who]
	cmd.Env = append(append([]string{}, c.g.clients[who].Env...), "HN_JIT_TRACE=1", "PYTHONDONTWRITEBYTECODE=1")
	cmd.Env = append(cmd.Env, c.extra...)
	var out, errout bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errout
	e := cmd.Run()
	rc := 0
	if e != nil {
		if x, ok := e.(*exec.ExitError); ok {
			rc = x.ExitCode()
		} else {
			c.t.Fatal(e)
		}
	}
	c.seq++
	c.save(fmt.Sprintf("%02d-%s", c.seq, name), map[string]any{"argv": append([]string{program}, args...), "cwd": cmd.Dir, "stdout": out.String(), "stderr": errout.String(), "rc": rc})
	return out.String(), errout.String(), rc
}
func (c *vrControl) author(id string) (string, int) {
	script, _ := filepath.Abs("selected_author.py")
	args := c.d.args("author", "--proposal", id)
	args[0] = script
	o, e, rc := c.run("author", "A", "python3", args...)
	return o + e, rc
}
func vrStatus(t *testing.T, out, want string) {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		var v map[string]any
		if json.Unmarshal([]byte(line), &v) == nil && v["status"] == want {
			return
		}
	}
	t.Fatalf("missing actual status %s: %s", want, out)
}
func (c *vrControl) snapshot(name string) map[string]any {
	c.t.Helper()
	g := c.g
	g.hn("R", "sync", "origin")
	o, e, rc := c.run(name+"-proposals", "R", g.h.Binary, "proposal", "list", "--json")
	if rc != 0 {
		c.t.Fatal(o, e)
	}
	var proposals map[string]any
	if e := json.Unmarshal([]byte(o), &proposals); e != nil {
		c.t.Fatal(e)
	}
	h, e, rc := c.run(name+"-history", "R", g.h.Binary, "issue", "history", c.d.issue, "--kind", "comments", "--json")
	if rc != 0 {
		c.t.Fatal(h, e)
	}
	var history map[string]any
	if e := json.Unmarshal([]byte(h), &history); e != nil {
		c.t.Fatal(e)
	}
	envs := map[string]any{}
	for _, ref := range strings.Fields(g.git("R", "--git-dir="+g.hosted, "for-each-ref", "--format=%(refname)", "refs/hn/actors")) {
		for _, commit := range strings.Fields(g.git("R", "--git-dir="+g.hosted, "rev-list", ref)) {
			cmd := exec.Command("git", "--git-dir="+g.hosted, "show", commit+":event.json")
			raw, err := cmd.Output()
			if err != nil {
				c.t.Fatal(err)
			}
			var event map[string]any
			if err = json.Unmarshal(raw, &event); err != nil {
				c.t.Fatal(err)
			}
			id := fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
			sig := g.git("R", "--git-dir="+g.hosted, "show", commit+":signature")
			envs[id] = map[string]any{"payload": string(raw), "event": event, "signature": sig, "commit": commit}
		}
	}
	s := map[string]any{"proposals": proposals, "history": history, "server_refs": g.git("R", "--git-dir="+g.hosted, "for-each-ref", "--format=%(refname) %(objectname)"), "main": g.ServerTip("main"), "policy": g.git("R", "show", g.Base+":.hn/policy.json"), "events": serveAdvanceEvents(c.t, g.work["R"]), "signed_envelopes": envs, "author_refs": g.git("A", "for-each-ref", "--format=%(refname) %(objectname)"), "author_HEAD": g.git("A", "rev-parse", "HEAD"), "clone_refs": c.cloneRefs(), "clone_heads": c.cloneHeads(), "target_policy": g.git("R", "show", g.ServerTip("main")+":.hn/policy.json"), "definition": g.git("R", "show", g.ServerTip("main")+":.hn/pipelines/test.json")}
	c.save(name, s)
	return s
}
func (c *vrControl) noAdmission(s map[string]any) {
	c.t.Helper()
	if s["main"] != c.g.ServerTip("main") {
		c.t.Fatal("main moved")
	}
	for _, v := range s["proposals"].(map[string]any)["data"].(map[string]any)["items"].([]any) {
		p := v.(map[string]any)
		if p["accepted"] == true || p["ready"] == true || p["merged"] == true || len(p["runRequests"].([]any)) != 0 || len(p["merges"].([]any)) != 0 {
			c.t.Errorf("refused proposal admitted: %v", p)
		}
	}
}
func vrSame(t *testing.T, a, b map[string]any, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if !reflect.DeepEqual(a[k], b[k]) {
			t.Fatalf("unexpected change %s", k)
		}
	}
}

func (c *vrControl) freshAll() {
	for _, who := range []string{"A", "M", "R", "U"} {
		o, e, rc := c.run("baseline-sync-"+who, who, c.g.h.Binary, "sync", "origin")
		if rc != 0 {
			c.t.Fatal(o, e)
		}
		o, e, rc = c.run("baseline-fetch-"+who, who, "git", "fetch", "-q", "origin", "+refs/heads/main:refs/remotes/origin/main")
		if rc != 0 {
			c.t.Fatal(o, e)
		}
	}
}
func (c *vrControl) cloneRefs() map[string]string {
	m := map[string]string{}
	for _, w := range []string{"A", "M", "R", "U"} {
		m[w] = c.g.git(w, "for-each-ref", "--format=%(refname) %(objectname)")
	}
	return m
}
func (c *vrControl) cloneHeads() map[string]string {
	m := map[string]string{}
	for _, w := range []string{"A", "M", "R", "U"} {
		m[w] = c.g.git(w, "rev-parse", "HEAD")
	}
	return m
}
func vrRefs(s string) map[string]string {
	m := map[string]string{}
	for _, l := range strings.Split(s, "\n") {
		v := strings.Fields(l)
		if len(v) == 2 {
			m[v[0]] = v[1]
		}
	}
	return m
}
func (c *vrControl) exactRefs(b, a map[string]any, worker bool) {
	c.t.Helper()
	old := vrRefs(b["server_refs"].(string))
	new := vrRefs(a["server_refs"].(string))
	actor := c.g.actors["R"]
	local := "refs/hn/actors/" + actor
	remote := "refs/hn/remotes/origin/actors/" + actor
	if old[local] == new[local] {
		c.t.Fatal("missing coordinator chain advance")
	}
	for _, w := range []string{"A", "M", "R", "U"} {
		expected := vrRefs(b["clone_refs"].(map[string]string)[w])
		actual := vrRefs(a["clone_refs"].(map[string]string)[w])
		if w == "R" {
			expected[local] = new[local]
			expected[remote] = new[local]
		}
		if w == "U" && worker {
			expected[remote] = new[local]
		}
		if !reflect.DeepEqual(expected, actual) {
			c.t.Fatalf("unbound clone ref change %s: expected %v actual %v", w, expected, actual)
		}
	}
	vrSame(c.t, b, a, "clone_heads")
}
func (c *vrControl) exactBlocked(b, a map[string]any, root string) {
	c.t.Helper()
	be := b["events"].(map[string]operationalEvent)
	var selection, prepared, blocked map[string]any
	previous := ""
	gen := float64(0)
	for id, row := range b["signed_envelopes"].(map[string]any) {
		e := row.(map[string]any)["event"].(map[string]any)
		var body map[string]any
		raw, ok := e["body"].(string)
		if !ok || json.Unmarshal([]byte(raw), &body) != nil {
			continue
		}
		if e["actor"] == c.g.actors["R"] && body["kind"] == "selection" {
			if n, ok := body["generation"].(float64); ok && n > gen {
				gen = n
				previous = id
				selection = body
			}
		}
		if e["actor"] == c.g.actors["A"] && body["kind"] == "prepared" {
			prepared = body
		}
	}
	for id, row := range a["signed_envelopes"].(map[string]any) {
		if _, ok := be[id]; !ok {
			e := row.(map[string]any)["event"].(map[string]any)
			if e["kind"] != "issue.comment" || e["actor"] != c.g.actors["R"] || e["subject"] != c.d.issue {
				c.t.Fatal("unexpected blocked signer/subject", e)
			}
			raw, ok := e["body"].(string)
			if !ok || json.Unmarshal([]byte(raw), &blocked) != nil {
				c.t.Fatal("bad blocked body")
			}
		}
	}
	if selection == nil || prepared == nil || prepared["selection"] != previous {
		c.t.Fatal("prepared acknowledgment not bound to active selection")
	}
	expected := map[string]any{}
	for k, v := range prepared["binding"].(map[string]any) {
		expected[k] = v
	}
	expected["protocol"] = "hn.selected-author/1"
	expected["kind"] = "selection"
	expected["phase"] = "blocked"
	expected["generation"] = gen + 1
	expected["previous"] = previous
	expected["reasons"] = []any{"lineage rejection: " + root}
	if !reflect.DeepEqual(expected, blocked) {
		c.t.Fatalf("blocked fact mismatch: expected %v actual %v", expected, blocked)
	}
	c.save("exact-blocked-binding", map[string]any{"expected": expected, "actual": blocked, "previous_selection": selection, "prepared": prepared})
}
func (c *vrControl) worker() (string, int) {
	script, _ := filepath.Abs("../integration/runner-loop.sh")
	client, _ := filepath.Abs("selected_author.py")
	o, e, rc := c.run("worker", "U", "env", "HUBNOT_STRICT_JIT_CONFIG="+c.d.config(), "HUBNOT_STRICT_JIT_CLIENT="+client, "RD_GATE_STATE="+filepath.Join(c.g.clients["U"].Home, "owned-gate"), "RD_STRICT_JIT_ONCE=1", "RD_STRICT_JIT_BACKEND=host", "RD_STRICT_JIT_ALLOW_HOST=1", "bash", script, "1")
	return o + e, rc
}
func (c *vrControl) preserve() {
	if !c.t.Failed() {
		return
	}
	if _, e := os.Stat(filepath.Join(c.root, "evidence", c.name+"-result.json")); e == nil {
		return
	}
	cmd := exec.Command("tar", "-czf", filepath.Join(c.root, "evidence", "partial-fixture.tar.gz"), "-C", os.Getenv("TMPDIR"), ".")
	raw, e := cmd.CombinedOutput()
	c.save("partial-snapshot", map[string]any{"status": "PARTIAL", "tar_error": fmt.Sprint(e), "tar_output": string(raw), "original_tmpdir": os.Getenv("TMPDIR"), "restore": "restore tmp archive to exact original TMPDIR; fixture descriptor and phase retained verbatim; paths are absolute"})
}

func (c *vrControl) queueWith(config string) (string, int) {
	queue, _ := filepath.Abs("../integration/merge-queue.py")
	client, _ := filepath.Abs("selected_author.py")
	o, e, rc := c.run("coordinator", "R", "python3", queue, "--clone", c.g.work["R"], "--once", "--strict-jit-config", config, "--strict-jit-client", client)
	return o + e, rc
}

func TestFix4Flow(t *testing.T) {
	c := vrNew(t, "fix4")
	defer c.preserve()
	var healthy, raced operationalEvent
	gateResume := os.Getenv("HN_FIX4_RESUME_STAGE") == "healthy-gate"
	if os.Getenv("HN_FIX4_RESUME") == "1" {
		c.seq = 200
		c.name = "fix4-final"
		var fx struct {
			Actors                                   map[string]string
			Base, Hosted, Issue, Native, Server_home string
			Work                                     map[string]string
			Clients                                  map[string]serveHarnessClientEnv
		}
		raw, err := os.ReadFile(filepath.Join(c.root, "evidence", "fix4-fixture.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(raw, &fx); err != nil {
			t.Fatal(err)
		}
		for who, client := range fx.Clients {
			client.t = t
			fx.Clients[who] = client
		}
		c.g = &serveGovernedFixture{servePushFixture: &servePushFixture{t: t, h: &serveHarness{t: t, Binary: fx.Native, Home: fx.Server_home}, hosted: fx.Hosted, actors: fx.Actors, work: fx.Work, clients: fx.Clients}, Base: fx.Base, Author: "A", Maintainer: "M", Runner: "U", Reviewer: "R"}
		c.d = &selectedDriver{t: t, g: c.g, issue: fx.Issue}
		raw, err = os.ReadFile(filepath.Join(c.root, "evidence", "fix4-cont-resume-roots.json"))
		if err != nil {
			t.Fatal(err)
		}
		var roots map[string]operationalEvent
		if err = json.Unmarshal(raw, &roots); err != nil {
			t.Fatal(err)
		}
		raced = roots["raced"]
		if gateResume {
			healthy = roots["healthy"]
		}
		native, err := os.ReadFile(filepath.Join(c.root, "artifacts", "candidate-hn-darwin-arm64"))
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(fx.Native+".new", native, 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.Rename(fx.Native+".new", fx.Native); err != nil {
			t.Fatal(err)
		}
	} else {
		c.init()
	}
	g := c.g
	d := c.d
	base := g.ServerTip("main")
	if !gateResume {
		healthy = g.OpenProposal(base, g.Commit(base, map[string]string{"auto/healthy-final.txt": "healthy final\n"}))
	}
	if raced.ID == "" {
		raced = g.OpenProposal(g.Base, g.Commit(g.Base, map[string]string{"auto/race.txt": "race\n"}))
	}

	c.save("resume-roots", map[string]any{"healthy": healthy, "raced": raced})
	must := func(name, want, out string, rc int) {
		t.Helper()
		c.save(name, map[string]any{"rc": rc, "output": out})
		if rc != 0 {
			t.Fatal(name, out)
		}
		vrStatus(t, out, want)
	}
	var out string
	var rc int
	if !gateResume {
		out, rc = c.author(healthy.ID)
		must("healthy-intent", "waiting", out, rc)
		out, rc = c.queueWith(d.config())
		must("healthy-select", "selected", out, rc)
		out, rc = c.author(healthy.ID)
		must("healthy-prepared", "prepared", out, rc)
		out, rc = c.queueWith(d.config())
		must("healthy-requested", "requested", out, rc)
		out, rc = c.worker()
		if rc != 0 {
			t.Fatal(out)
		}
	}
	g.hn("R", "sync", "origin")
	hp := jitProposal(t, g, "R", healthy.ID)
	if len(hp.Pipelines) != 1 || len(hp.Pipelines[0].Passed) != 1 {
		t.Fatal("healthy gate did not pass", hp)
	}
	for _, args := range [][]string{{"fetch", "-q", "origin", "+refs/heads/main:refs/remotes/origin/main"}, {"checkout", "-q", "main"}, {"merge", "--ff-only", "origin/main"}} {
		o, e, r := c.run("align-healthy-R-main", "R", "git", args...)
		if r != 0 {
			t.Fatal(o, e)
		}
	}
	// Old native/server compatibility uses only this owned synthetic binary path.
	originalBytes, err := os.ReadFile(g.h.Binary)
	if err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(c.root, "inputs", "old-hn-darwin-arm64")
	oldBytes, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(oldBytes)) != "cee886e805bf5968acae93bd497e5f4dc37994fd69ce37467c4d776ad27d00c1" {
		t.Fatal("old native hash mismatch")
	}
	replaceBinary := func(raw []byte) {
		t.Helper()
		p := g.h.Binary + ".new"
		if e := os.WriteFile(p, raw, 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.Rename(p, g.h.Binary); e != nil {
			t.Fatal(e)
		}
	}
	// Current client, old forced-command server and old hook executable.
	replaceBinary(oldBytes)
	currentClient := filepath.Join(c.root, "artifacts", "candidate-hn-darwin-arm64")
	o, e, r := c.run("old-server-strict-refusal", "R", currentClient, "automerge", healthy.ID, "--rule", "auto", "--execute", "--remote", "origin", "--strict-jit-root", healthy.ID)
	replaceBinary(originalBytes)
	if r == 0 || !strings.Contains(o+e, "unsupported-command") || g.ServerTip("main") != healthy.Base {
		t.Fatal("old server did not fail closed", o, e, r)
	}
	o, e, r = c.run("old-native-strict-refusal", "R", oldPath, "automerge", healthy.ID, "--rule", "auto", "--execute", "--remote", "origin", "--strict-jit-root", healthy.ID)
	if r == 0 || !strings.Contains(o+e, "flag provided but not defined") {
		t.Fatal("old native did not refuse", o, e, r)
	}
	out, rc = c.queueWith(d.config())
	must("healthy-merged", "merge-published", out, rc)
	c.snapshot("healthy-final")
	if g.ServerTip("main") != healthy.Head || !jitProposal(t, g, "R", healthy.ID).Merged {
		t.Fatal("strict healthy did not merge")
	}
	out, rc = c.queueWith(d.config())
	must("healthy-done", "merged", out, rc)
	out, rc = c.author(raced.ID)
	must("race-intent", "waiting", out, rc)
	out, rc = c.queueWith(d.config())
	must("race-select", "selected", out, rc)
	out, rc = c.author(raced.ID)
	must("race-prepared", "prepared", out, rc)
	g.hn("R", "sync", "origin")
	root := jitProposal(t, g, "R", raced.ID)
	if len(root.Lineage.Successors) != 1 {
		t.Fatal("prepared successor missing", root)
	}
	leaf := root.Lineage.Successors[0]
	out, rc = c.queueWith(d.config())
	must("race-requested", "requested", out, rc)
	out, rc = c.worker()
	if rc != 0 {
		t.Fatal(out)
	}
	before := c.snapshot("race-before")
	lp := jitProposal(t, g, "R", leaf)
	if lp.Lineage.Root != raced.ID || lp.Base != healthy.Head || len(lp.Requests) != 1 || len(lp.Pipelines[0].Passed) != 1 || lp.Requests[0].Actor != g.actors["R"] || lp.Requests[0].Results[0].Actor != g.actors["U"] {
		t.Fatal("real gate prefix prerequisite", lp)
	}
	for _, args := range [][]string{{"fetch", "-q", "origin", "+refs/heads/main:refs/remotes/origin/main"}, {"checkout", "-q", "main"}, {"merge", "--ff-only", "origin/main"}} {
		o, e, r := c.run("align-R-main", "R", "git", args...)
		if r != 0 {
			t.Fatal(o, e)
		}
	}
	push := func(name, root string) string {
		t.Helper()
		o, e, r := c.run(name, "R", "git", "push", "--porcelain", "--push-option=hn-advance="+leaf, "--push-option=hn-expected-tip="+lp.Base, "--push-option=hn-rule=auto", "--push-option=hn-strict-jit-root="+root, "origin", "+"+lp.Head+":refs/heads/main")
		if r == 0 || !strings.Contains(o+e, "strict JIT") || g.ServerTip("main") != lp.Base {
			t.Fatal(name, o, e, r)
		}
		return o + e
	}
	push("server-unrelated-root", healthy.ID)
	push("server-nonoriginal-root", leaf)
	push("server-unknown-root", "sha256:"+strings.Repeat("f", 64))
	// The actual verified catalog is incomplete when its signed predecessor is absent.
	var signed []StoredEvent
	inDir(t, g.work["R"], func() {
		var er error
		signed, er = collectEvents()
		if er != nil {
			t.Fatal(er)
		}
	})
	incomplete := []StoredEvent{}
	for _, ev := range signed {
		if ev.ID != raced.ID {
			incomplete = append(incomplete, ev)
		}
	}
	var incompleteError error
	inDir(t, g.work["R"], func() { incompleteError = checkStrictJITLineage(incomplete, leaf, raced.ID) })
	if incompleteError == nil || !strings.Contains(incompleteError.Error(), "unavailable predecessor") {
		t.Fatal("incomplete catalog admitted", incompleteError)
	}
	c.save("incomplete-catalog", map[string]any{"refused": true, "error": incompleteError.Error(), "omitted_signed_predecessor": raced.ID})
	wrapper := os.Getenv("HN_VR_WRAPPER")
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	cfgdir := filepath.Join(c.root, "evidence")
	cfgPath := filepath.Join(cfgdir, "race-strict-jit.json")
	cfg, _ := json.Marshal(map[string]string{"hn": wrapper, "remote": "origin", "issue": d.issue, "coordinator": g.actors["R"], "pipeline": "test", "rule": "auto", "owner": "fixture-owner"})
	if err = os.WriteFile(cfgPath, cfg, 0600); err != nil {
		t.Fatal(err)
	}
	wcfg := filepath.Join(cfgdir, "race-wrapper-config.json")
	calls := filepath.Join(cfgdir, "race-wrapper-calls.jsonl")
	wj, _ := json.Marshal(map[string]any{"stage": "git-push", "real_git": realGit, "native": g.h.Binary, "original": raced.ID, "prepared": leaf, "hosted": g.hosted, "m_env": g.clients["M"].Env, "m_cwd": g.work["M"], "calls": calls, "state": filepath.Join(cfgdir, "race-wrapper-state.json"), "veto_body": "Late original-root veto after real passing gate"})
	if err = os.WriteFile(wcfg, wj, 0600); err != nil {
		t.Fatal(err)
	}
	path := ""
	for _, v := range g.clients["R"].Env {
		if strings.HasPrefix(v, "PATH=") {
			path = strings.TrimPrefix(v, "PATH=")
		}
	}
	c.extra = []string{"HN_VR_CONFIG=" + wcfg, "PATH=" + filepath.Join(c.root, "tools") + ":" + path}
	out, rc = c.queueWith(cfgPath)
	c.extra = nil
	c.save("race-coordinator", map[string]any{"rc": rc, "output": out})
	after := c.snapshot("race-after")
	fp := jitProposal(t, g, "R", leaf)
	rp := jitProposal(t, g, "R", raced.ID)
	var st map[string]any
	sr, _ := os.ReadFile(filepath.Join(cfgdir, "race-wrapper-state.json"))
	_ = json.Unmarshal(sr, &st)
	if st["injected"] != true || rc == 0 || strings.Contains(out, "merge-published") || !strings.Contains(out, "strict JIT lineage rejection") || g.ServerTip("main") != lp.Base || fp.Merged || !rp.Rejected || fp.Rejected {
		t.Fatal("race strict guard outcome", out, rc, st, fp, rp)
	}
	be := before["events"].(map[string]operationalEvent)
	ae := after["events"].(map[string]operationalEvent)
	for id, ev := range ae {
		if _, ok := be[id]; !ok && ev.Kind == "proposal.merged" {
			t.Fatal("refused race emitted merge", ev)
		}
	}
	c.save("race-result", map[string]any{"status": "PASS", "boundary": "actual protected push after native client validation", "original": raced.ID, "prepared": leaf, "main_before": before["main"], "main_after": after["main"], "root_rejected": rp.Rejected, "successor_rejected": fp.Rejected, "successor_merged": fp.Merged, "coordinator_rc": rc, "veto": g.latest("M"), "gate": lp.Pipelines[0].Passed[0]})
	// The same exact prepared successor remains mergeable under ordinary FR005.
	o, e, r = c.run("ordinary-FR005-positive", "R", g.h.Binary, "automerge", leaf, "--rule", "auto", "--execute", "--remote", "origin")
	if r != 0 || g.ServerTip("main") != lp.Head {
		t.Fatal("ordinary FR005 was changed", o, e, r)
	}
	final := c.snapshot("ordinary-final")
	fp = jitProposal(t, g, "R", leaf)
	rp = jitProposal(t, g, "R", raced.ID)
	if !fp.Merged || fp.Rejected || !rp.Rejected {
		t.Fatal("legacy identity/decision semantics", fp, rp)
	}
	c.save("result", map[string]any{"status": "PASS", "healthy_strict": "PASS", "actual_guard_race": "PASS", "ordinary_FR005": "PASS", "main": final["main"], "root": raced.ID, "current": leaf})
}

func inDir(t *testing.T, path string, fn func()) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir(path); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(old); err != nil {
			t.Fatal(err)
		}
	}()
	fn()
}

func TestFix4RaceResume(t *testing.T) {
	c := vrNew(t, "fix4-resume")
	c.seq = 300
	defer c.preserve()
	var fx struct {
		Actors                                   map[string]string
		Base, Hosted, Issue, Native, Server_home string
		Work                                     map[string]string
		Clients                                  map[string]serveHarnessClientEnv
	}
	raw, err := os.ReadFile(filepath.Join(c.root, "evidence", "fix4-fixture.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	for who, client := range fx.Clients {
		client.t = t
		fx.Clients[who] = client
	}
	g := &serveGovernedFixture{servePushFixture: &servePushFixture{t: t, h: &serveHarness{t: t, Binary: fx.Native, Home: fx.Server_home}, hosted: fx.Hosted, actors: fx.Actors, work: fx.Work, clients: fx.Clients}, Base: fx.Base, Author: "A", Maintainer: "M", Runner: "U", Reviewer: "R"}
	c.g = g
	c.d = &selectedDriver{t: t, g: g, issue: fx.Issue}
	d := c.d
	var saved struct{ Original, Prepared string }
	raw, err = os.ReadFile(filepath.Join(c.root, "evidence", "race-wrapper-config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	raced := operationalEvent{ID: saved.Original}
	leaf := saved.Prepared
	before := c.snapshot("race-before")
	lp := jitProposal(t, g, "R", leaf)
	if lp.Lineage.Root != raced.ID || lp.Base != g.ServerTip("main") || len(lp.Requests) != 1 || len(lp.Pipelines[0].Passed) != 1 || lp.Merged || jitProposal(t, g, "R", raced.ID).Rejected {
		t.Fatal("unchanged real pre-veto prefix required", lp)
	}
	var out, o, e string
	var rc, r int
	wrapper := os.Getenv("HN_VR_WRAPPER")
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	cfgdir := filepath.Join(c.root, "evidence")
	cfgPath := filepath.Join(cfgdir, "race-strict-jit.json")
	cfg, _ := json.Marshal(map[string]string{"hn": wrapper, "remote": "origin", "issue": d.issue, "coordinator": g.actors["R"], "pipeline": "test", "rule": "auto", "owner": "fixture-owner"})
	if err = os.WriteFile(cfgPath, cfg, 0600); err != nil {
		t.Fatal(err)
	}
	wcfg := filepath.Join(cfgdir, "resume-wrapper-config.json")
	calls := filepath.Join(cfgdir, "resume-wrapper-calls.jsonl")
	wj, _ := json.Marshal(map[string]any{"stage": "git-push", "real_git": realGit, "native": g.h.Binary, "original": raced.ID, "prepared": leaf, "hosted": g.hosted, "m_env": g.clients["M"].Env, "m_cwd": g.work["M"], "calls": calls, "state": filepath.Join(cfgdir, "resume-wrapper-state.json"), "veto_body": "Late original-root veto after real passing gate"})
	if err = os.WriteFile(wcfg, wj, 0600); err != nil {
		t.Fatal(err)
	}
	path := ""
	for _, v := range g.clients["R"].Env {
		if strings.HasPrefix(v, "PATH=") {
			path = strings.TrimPrefix(v, "PATH=")
		}
	}
	c.extra = []string{"HN_VR_CONFIG=" + wcfg, "PATH=" + filepath.Join(c.root, "tools") + ":" + path}
	out, rc = c.queueWith(cfgPath)
	c.extra = nil
	c.save("race-coordinator", map[string]any{"rc": rc, "output": out})
	after := c.snapshot("race-after")
	fp := jitProposal(t, g, "R", leaf)
	rp := jitProposal(t, g, "R", raced.ID)
	var st map[string]any
	sr, _ := os.ReadFile(filepath.Join(cfgdir, "resume-wrapper-state.json"))
	_ = json.Unmarshal(sr, &st)
	if st["injected"] != true || rc == 0 || strings.Contains(out, "merge-published") || !strings.Contains(out, "strict JIT lineage rejection") || g.ServerTip("main") != lp.Base || fp.Merged || !rp.Rejected || fp.Rejected {
		t.Fatal("race strict guard outcome", out, rc, st, fp, rp)
	}
	be := before["events"].(map[string]operationalEvent)
	ae := after["events"].(map[string]operationalEvent)
	for id, ev := range ae {
		if _, ok := be[id]; !ok && ev.Kind == "proposal.merged" {
			t.Fatal("refused race emitted merge", ev)
		}
	}
	c.save("race-result", map[string]any{"status": "PASS", "boundary": "actual protected push after native client validation", "original": raced.ID, "prepared": leaf, "main_before": before["main"], "main_after": after["main"], "root_rejected": rp.Rejected, "successor_rejected": fp.Rejected, "successor_merged": fp.Merged, "coordinator_rc": rc, "veto": g.latest("M"), "gate": lp.Pipelines[0].Passed[0]})
	// The same exact prepared successor remains mergeable under ordinary FR005.
	o, e, r = c.run("ordinary-FR005-positive", "R", g.h.Binary, "automerge", leaf, "--rule", "auto", "--execute", "--remote", "origin")
	if r != 0 || g.ServerTip("main") != lp.Head {
		t.Fatal("ordinary FR005 was changed", o, e, r)
	}
	final := c.snapshot("ordinary-final")
	fp = jitProposal(t, g, "R", leaf)
	rp = jitProposal(t, g, "R", raced.ID)
	if !fp.Merged || fp.Rejected || !rp.Rejected {
		t.Fatal("legacy identity/decision semantics", fp, rp)
	}
	c.save("result", map[string]any{"status": "PASS", "healthy_strict": "PASS", "actual_guard_race": "PASS", "ordinary_FR005": "PASS", "main": final["main"], "root": raced.ID, "current": leaf})
}
