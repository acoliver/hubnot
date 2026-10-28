package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Audit facts (docs/audits-v1.md) through hn serve. The black-box tests
// sign audit facts with hn audit in client clones and push them through
// the SSH shim and the real forced command, so every check runs inside
// receive-pack's pre-receive hook with Git's quarantine environment set.
// Every rejection goes through servePushFixture.reject, which requires the
// hosted refs to be byte-identical afterwards.

// serveAuditFixture is a servePushFixture with participants A and B where
// A holds audit authority. Policy is the commit in A's clone that carries
// the audit policy and rule check; it is never pushed, so only audit packs
// can bring it to the server. Target is an old commit that no ref reaches,
// and Parent is its parent, which no audit pack carries.
type serveAuditFixture struct {
	*servePushFixture
	Policy, Target, Parent string
}

// serveAuditPipeline is rule check: it prints one line and fails while
// flag exists.
func serveAuditPipeline(t *testing.T, flag string) string {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{
		"version": "hn.pipeline/0",
		"steps": []any{map[string]any{
			"name": "audit-check", "command": "sh",
			"args": []string{"-c", "echo audited by the serve fixture; test ! -e '" + flag + "'"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded) + "\n"
}

func newServeAuditFixture(t *testing.T) *serveAuditFixture {
	t.Helper()
	f := &serveAuditFixture{servePushFixture: newServeFactsFixture(t, "A", "B")}
	f.Policy = serveAuditEnable(f.servePushFixture, "A", map[string]string{"check": serveAuditPipeline(t, filepath.Join(t.TempDir(), "absent"))})
	f.git("A", "checkout", "-q", "--detach", "origin/main")
	f.git("A", "commit", "-q", "--allow-empty", "-m", "parent the audit snapshot omits")
	f.Parent = f.git("A", "rev-parse", "HEAD")
	writeServeTestFile(t, filepath.Join(f.work["A"], "old-code"), "inspect me\n", 0o644)
	f.git("A", "add", "old-code")
	f.git("A", "commit", "-q", "-m", "old code to audit")
	f.Target = f.git("A", "rev-parse", "HEAD")
	f.git("A", "checkout", "-q", "main")
	return f
}

// serveAuditEnable commits, on branch audit-authority from origin/main in
// who's clone, the hosted policy plus an audits section that names who in
// every role, and one pipeline file per rule (an empty definition keeps
// the existing file). It adopts that branch as who's audit authority and
// returns the commit. who's clone is back on main afterwards.
func serveAuditEnable(f *servePushFixture, who string, rules map[string]string) string {
	f.t.Helper()
	f.git(who, "fetch", "-q", "origin")
	f.git(who, "checkout", "-q", "-B", "audit-authority", "origin/main")
	var policy PolicyDocument
	if err := json.Unmarshal([]byte(f.git(who, "show", "HEAD:"+serveGovernedPolicyPath)), &policy); err != nil {
		f.t.Fatal(err)
	}
	actor := f.actors[who]
	policy.Audits = &AuditPolicy{Scope: "serve", RuleOwners: []string{actor}, Requesters: []string{actor}, Executors: []string{actor}}
	writeTestPolicy(f.t, f.work[who], policy)
	for rule, definition := range rules {
		if definition == "" {
			continue
		}
		path := filepath.Join(f.work[who], ".hn", "pipelines", rule+".json")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			f.t.Fatal(err)
		}
		writeServeTestFile(f.t, path, definition, 0o644)
	}
	f.git(who, "add", ".hn")
	f.git(who, "commit", "-q", "-m", "audit authority")
	commit := f.git(who, "rev-parse", "HEAD")
	f.git(who, "checkout", "-q", "main")
	f.audit(who, "authority", "set", "--ref", "refs/heads/audit-authority", "--expect", "absent")
	return commit
}

// audit runs hn audit ARGS --json in who's clone and returns the data of
// the successful hn.audit/1 response.
func (f *servePushFixture) audit(who string, args ...string) json.RawMessage {
	f.t.Helper()
	return serveAuditData(f.t, f.clients[who], f.work[who], args...)
}

func serveAuditData(t *testing.T, client serveHarnessClientEnv, dir string, args ...string) json.RawMessage {
	t.Helper()
	output := client.Hn(dir, append(append([]string{"audit"}, args...), "--json")...)
	var envelope struct {
		Schema string          `json:"schema"`
		OK     bool            `json:"ok"`
		Data   json.RawMessage `json:"data"`
	}
	start := strings.Index(output, "{")
	if start < 0 || json.Unmarshal([]byte(output[start:]), &envelope) != nil || envelope.Schema != "hn.audit/1" || !envelope.OK {
		t.Fatalf("hn audit %s: unexpected response\n%s", strings.Join(args, " "), output)
	}
	return envelope.Data
}

// serveAuditID is the full event ID of a mutation response.
func serveAuditID(t *testing.T, data json.RawMessage) string {
	t.Helper()
	var response struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &response); err != nil || !validEventID(response.ID) {
		t.Fatalf("audit response %s has no full event ID: %v", data, err)
	}
	return response.ID
}

// serveAuditListIDs is the sorted IDs of hn audit list in dir.
func serveAuditListIDs(t *testing.T, client serveHarnessClientEnv, dir string) []string {
	t.Helper()
	var list struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(serveAuditData(t, client, dir, "list"), &list); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		ids = append(ids, item.ID)
	}
	if len(ids) != list.Total {
		t.Fatalf("audit list returned %d of %d facts", len(ids), list.Total)
	}
	slices.Sort(ids)
	return ids
}

// hostedHas reports whether the hosted object database holds object.
func (f *servePushFixture) hostedHas(object string) bool {
	f.t.Helper()
	_, status := f.clients["A"].exec("", "git", "--git-dir="+f.hosted, "cat-file", "-e", object)
	return status == 0
}

// Every audit kind is accepted through the receive hook, each push on top
// of hosted audit facts, and an ordinary event after them. The snapshot of
// an old target no ref reaches arrives with its request, without its
// parent. A clone without an hn identity reads the facts and the result
// log, and a second participant's sync imports them.
func TestServeAuditFactsPushReadAndSync(t *testing.T) {
	f := newServeAuditFixture(t)
	if f.hostedHas(f.Target) || f.hostedHas(f.Policy) {
		t.Fatal("the audit fixture commits are hosted before any audit push")
	}
	auth := serveAuditID(t, f.audit("A", "rule", "authorize", "check", "--source", "pipeline", "--at", f.Policy))
	f.pushOwn("A")
	request := serveAuditID(t, f.audit("A", "request", "--at", f.Target, "--rule", "check", "--intent", "inspect old code"))
	f.pushOwn("A")
	result := serveAuditID(t, f.audit("A", "execute", request, "--backend", "host", "--allow-unsafe-host-execution"))
	f.pushOwn("A")
	revoke := serveAuditID(t, f.audit("A", "rule", "revoke", "check", "--expect", auth, "--reason", "retired after the serve test"))
	f.pushOwn("A")
	f.issue("A", "ordinary fact after the audit facts")
	f.pushOwn("A")
	for _, entry := range f.pushLog() {
		if entry.Verdict != servePushVerdictAccepted {
			t.Fatalf("push log entry %+v, want only accepted pushes", entry)
		}
	}
	if !f.hostedHas(f.Target) || !f.hostedHas(f.Policy) {
		t.Fatal("the accepted audit snapshots are not in the hosted object database")
	}
	if f.hostedHas(f.Parent) {
		t.Fatal("the audit snapshot transport brought the target's parent history")
	}
	want := []string{auth, request, result, revoke}
	slices.Sort(want)

	// A clone with no hn identity selects A and reads every fact back.
	reader := f.clients["B"]
	dir := filepath.Join(t.TempDir(), "keyless")
	reader.Git("", "clone", "-q", serveHarnessCloneURL("r"), dir)
	if _, err := os.Stat(filepath.Join(dir, ".git", "hn", "identity.json")); !os.IsNotExist(err) {
		t.Fatalf("the reader clone has an hn identity: %v", err)
	}
	reader.Hn(dir, "replication", "select", "origin", "--actor", f.actors["A"])
	reader.Hn(dir, "sync", "origin")
	if got := serveAuditListIDs(t, reader, dir); !slices.Equal(got, want) {
		t.Fatalf("keyless reader lists %q, want %q", got, want)
	}
	shown := string(serveAuditData(t, reader, dir, "show", result))
	if !strings.Contains(shown, `"outcome":"passed"`) || !strings.Contains(shown, f.Target) {
		t.Fatalf("keyless show of the result:\n%s", shown)
	}
	var log string
	if err := json.Unmarshal(serveAuditData(t, reader, dir, "logs", result), &log); err != nil || !strings.Contains(log, "audited by the serve fixture") {
		t.Fatalf("keyless log %q: %v", log, err)
	}
	if got := strings.TrimSpace(reader.Git(dir, "show", f.Target+":old-code")); got != "inspect me" {
		t.Fatalf("the reader's audit snapshot of the target holds %q", got)
	}

	// A second participant's ordinary sync imports the same facts.
	f.hn("B", "sync", "origin")
	if got := serveAuditListIDs(t, f.clients["B"], f.work["B"]); !slices.Equal(got, want) {
		t.Fatalf("B lists %q after sync, want %q", got, want)
	}
	if got, hosted := f.git("B", "rev-parse", acceptedActorRef("origin", f.actors["A"])), f.hostedRef(f.actorRef("A")); got != hosted {
		t.Fatalf("B accepted A's chain at %s, hosted at %s", got, hosted)
	}
}

// Inside receive-pack's hooks Git sets GIT_QUARANTINE_PATH and points
// GIT_OBJECT_DIRECTORY at the push quarantine, with the hosted objects as
// an alternate. Audit pack verification still runs in its own temporary
// repository: a valid pack verifies and installs into the quarantine only,
// and the hosted objects reachable through the alternate cannot supply a
// member the signed pack omits.
func TestAuditPackVerificationIgnoresReceiveEnvironment(t *testing.T) {
	_, id, at := auditFixture(t)
	auth := auditAuthorizeFixture(t, id, at)
	dir, err := requireGitRepository()
	if err != nil {
		t.Fatal(err)
	}
	objects, err := auditDependencyObjectIDsAt("", auth.Event)
	if err != nil {
		t.Fatal(err)
	}
	policyBlob := mustGitText(t, "rev-parse", at+":.hn/policy.json")
	retained := slices.DeleteFunc(slices.Clone(objects), func(object string) bool { return object == policyBlob })
	partial, err := gitInput([]byte(strings.Join(retained, "\n")+"\n"), nil, "pack-objects", "--stdout", "--window=0", "--depth=0")
	if err != nil {
		t.Fatal(err)
	}
	hostedPacks, err := filepath.Glob(filepath.Join(dir, "objects", "pack", "*.pack"))
	if err != nil {
		t.Fatal(err)
	}

	quarantine := filepath.Join(migrationBare(t), "objects")
	t.Setenv("GIT_OBJECT_DIRECTORY", quarantine)
	t.Setenv("GIT_ALTERNATE_OBJECT_DIRECTORIES", filepath.Join(dir, "objects"))
	t.Setenv("GIT_QUARANTINE_PATH", quarantine)
	if _, err := loadStoredEventAt(dir, auth.Commit); err != nil {
		t.Fatalf("a valid audit fact does not load in the receive environment: %v", err)
	}
	if installed, _ := filepath.Glob(filepath.Join(quarantine, "pack", "*.pack")); len(installed) != 1 {
		t.Fatalf("quarantine packs %q, want the one installed audit pack", installed)
	}
	if after, _ := filepath.Glob(filepath.Join(dir, "objects", "pack", "*.pack")); !slices.Equal(after, hostedPacks) {
		t.Fatalf("accepted packs changed from %q to %q", hostedPacks, after)
	}
	if err := importAuditPack(dir, partial, auth.Event); err == nil {
		t.Fatal("an alternate object database supplied the policy blob a signed audit pack omits")
	}
}

// serveAuditVariant commits, on commit's parent, commit's tree with the
// members in replace swapped for the given bytes, or removed for nil, in
// the current repository, and returns the new commit. The signed
// event.json and signature stay as they are.
func serveAuditVariant(t *testing.T, commit string, replace map[string][]byte) string {
	t.Helper()
	entries := map[string]string{}
	for _, line := range strings.Split(mustGitText(t, "ls-tree", commit), "\n") {
		meta, name, _ := strings.Cut(line, "\t")
		entries[name] = meta
	}
	for name, contents := range replace {
		delete(entries, name)
		if contents != nil {
			entries[name] = "100644 blob " + mustGitTextFromInput(t, contents, "hash-object", "-w", "--stdin")
		}
	}
	var tree strings.Builder
	for _, name := range slices.Sorted(maps.Keys(entries)) {
		tree.WriteString(entries[name] + "\t" + name + "\n")
	}
	id := mustGitTextFromInput(t, []byte(tree.String()), "mktree")
	return mustGitText(t, "commit-tree", id, "-p", commit+"^", "-m", "tampered audit attachments")
}

// serveObjectFiles lists every file below the hosted object database, so a
// rejected push can be shown to leave accepted storage byte-identical.
func (f *servePushFixture) hostedObjectFiles() []string {
	f.t.Helper()
	var files []string
	root := filepath.Join(f.hosted, "objects")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			files = append(files, strings.TrimPrefix(path, root))
		}
		return nil
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return files
}

// A corrupt, missing or swapped audit.pack and a tampered audit-result log
// are refused by the receive hook, naming the result and the attachment
// cause, by the batched reader and by loadStoredEventAt alike. The push
// that carries a valid request together with the tampered result changes
// no hosted ref and no file of the hosted object database, so the
// request's snapshot is not hosted. Untampered, the same events are read
// by the batched reader with the attachments loadStoredEventAt returns,
// within its read budget, and the push is accepted. A damaged hosted audit
// fact makes the server's own batched read refuse later fact updates.
func TestServeAuditTamperedAttachmentsRefusedByPushAndReaders(t *testing.T) {
	f := newServeAuditFixture(t)
	serveAuditID(t, f.audit("A", "rule", "authorize", "check", "--source", "pipeline", "--at", f.Policy))
	f.pushOwn("A")
	request := serveAuditID(t, f.audit("A", "request", "--at", f.Target, "--rule", "check", "--intent", "tampered result"))
	result := serveAuditID(t, f.audit("A", "execute", request, "--backend", "host", "--allow-unsafe-host-execution"))
	ref := f.actorRef("A")
	good := f.localHead("A", ref)
	t.Chdir(f.work["A"])

	reference, err := loadStoredEventAt("", good)
	if err != nil {
		t.Fatal(err)
	}
	batched, err := issueReadStoredEvents([]string{good}, defaultIssueReadLimits())
	if err != nil || len(batched) != 1 {
		t.Fatalf("batched read of the untampered result: %v", err)
	}
	if !reflect.DeepEqual(batched[0].Attachments, reference.Attachments) || len(reference.Attachments) != 2 {
		t.Fatalf("batched attachments %q differ from loadStoredEventAt's %q", slices.Sorted(maps.Keys(batched[0].Attachments)), slices.Sorted(maps.Keys(reference.Attachments)))
	}
	pack := reference.Attachments["audit.pack"]
	tight := defaultIssueReadLimits()
	tight.object = int64(len(pack)) - 1
	var readErr *IssueReadError
	if _, err := issueReadStoredEvents([]string{good}, tight); !errors.As(err, &readErr) || readErr.Code != "resource_limit" {
		t.Fatalf("an audit pack above the object read budget gave %v, want resource_limit", err)
	}

	ids, err := auditPackObjectIDs(pack, len(good))
	if err != nil {
		t.Fatal(err)
	}
	extra := mustGitTextFromInput(t, []byte("an object no signed snapshot names\n"), "hash-object", "-w", "--stdin")
	swapped, err := gitInput([]byte(strings.Join(append(ids, extra), "\n")+"\n"), nil, "pack-objects", "--stdout", "--window=0", "--depth=0")
	if err != nil {
		t.Fatal(err)
	}
	variants := map[string]map[string][]byte{
		"corrupt pack":    {"audit.pack": []byte("not a pack")},
		"missing pack":    {"audit.pack": nil},
		"extra object":    {"audit.pack": swapped},
		"tampered log":    {"log.txt": append(bytes.Clone(reference.Attachments["log.txt"]), "forged\n"...)},
		"undeclared file": {"notes.txt": []byte("not a typed attachment\n")},
	}
	for _, name := range slices.Sorted(maps.Keys(variants)) {
		bad := serveAuditVariant(t, good, variants[name])
		if _, err := loadStoredEventAt("", bad); err == nil {
			t.Fatalf("%s: loadStoredEventAt accepted the tampered result", name)
		}
		if _, err := issueReadStoredEvents([]string{bad}, defaultIssueReadLimits()); !errors.As(err, &readErr) || readErr.Code != "invalid_history" {
			t.Fatalf("%s: batched reader gave %v, want invalid_history", name, err)
		}
		mustGit(t, "update-ref", ref, bad)
		if _, _, err := collectIssueEvents(); err == nil {
			t.Fatalf("%s: collectIssueEvents accepted the tampered result", name)
		}
		objects := f.hostedObjectFiles()
		rejections := f.reject("A", map[string]string{ref: serveReasonActorChainInvalid}, "origin", ref+":"+ref)
		requireServeIDs(t, rejections[0], result)
		if !strings.Contains(rejections[0].Sentence, "attachments do not verify") {
			t.Fatalf("%s: sentence %q does not name the attachment failure", name, rejections[0].Sentence)
		}
		if after := f.hostedObjectFiles(); !slices.Equal(after, objects) {
			t.Fatalf("%s: the rejected push changed the hosted object database", name)
		}
		if f.hostedHas(f.Target) {
			t.Fatalf("%s: the rejected request's snapshot reached accepted storage", name)
		}
		mustGit(t, "update-ref", ref, good)
	}
	if _, _, err := collectIssueEvents(); err != nil {
		t.Fatalf("the restored chain does not read: %v", err)
	}
	f.pushOwn("A")

	// Damage the hosted chain behind the verdict's back: the corrupt
	// commit arrives on an ordinary branch and the administrator moves the
	// actor ref. The server's batched read then refuses every fact update.
	bad := serveAuditVariant(t, good, map[string][]byte{"audit.pack": []byte("not a pack")})
	f.push("A", "origin", bad+":refs/heads/damage")
	f.h.Serve("serve", "repo", "lock-exec", "r", "--", "git", "update-ref", ref, bad, good)
	f.issue("B", "fact pushed over damaged hosted audits")
	rejections := f.reject("B", map[string]string{f.actorRef("B"): serveReasonActorChainInvalid}, "origin", f.actorRef("B")+":"+f.actorRef("B"))
	if rejections[0].Sentence != serveHostedFactsSentence {
		t.Fatalf("sentence %q, want %q", rejections[0].Sentence, serveHostedFactsSentence)
	}
}
