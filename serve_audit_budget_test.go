package main

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// largeTarget commits, on origin/main in A's clone and outside every ref,
// one highly compressible file of 1 MiB of fill and returns the commit.
func (f *serveAuditFixture) largeTarget(name string, fill byte) string {
	f.t.Helper()
	f.git("A", "checkout", "-q", "--detach", "origin/main")
	writeServeTestFile(f.t, filepath.Join(f.work["A"], name), strings.Repeat(string(fill), serveAuditExpanded), 0o644)
	f.git("A", "add", name)
	f.git("A", "commit", "-q", "-m", "large compressible audit target "+name)
	commit := f.git("A", "rev-parse", "HEAD")
	f.git("A", "checkout", "-q", "main")
	return commit
}

// serveAuditExpanded is the size of a large audit target's one blob.
const serveAuditExpanded = 1 << 20

// The five push budgets count an audit fact's audit.pack as an attachment
// and the objects it expands to, before anything is unpacked, the way
// client replication measures the same chain: a 1 MiB compressible target
// in a small pack exceeds each budget set below it, and the push passes at
// exactly the client's measurements. Snapshot objects the hosted chain's
// packs already hold do not count again, and two packs of one push that
// share a snapshot count it once.
func TestServeAuditBudgetsCountPacksAndExpandedObjects(t *testing.T) {
	f := newServeAuditFixture(t)
	ref := f.actorRef("A")
	serveAuditID(t, f.audit("A", "rule", "authorize", "check", "--source", "pipeline", "--at", f.Policy))
	f.pushOwn("A")

	large := f.largeTarget("large", 'a')
	f.audit("A", "request", "--at", large, "--rule", "check", "--intent", "inspect a large snapshot")
	head := f.localHead("A", ref)
	packBytes, err := strconv.ParseInt(f.git("A", "cat-file", "-s", head+":audit.pack"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	if packBytes >= 64<<10 {
		t.Fatalf("the audit pack of the 1 MiB target holds %d bytes; the fixture needs a compressible target", packBytes)
	}
	measured := measureServeTestActorPush(f.servePushFixture, "A")
	if measured.LargestObjectBytes != serveAuditExpanded || measured.LargestAttachmentBytes < packBytes || measured.TotalBytes <= serveAuditExpanded {
		t.Fatalf("client measurement %+v does not count the %d-byte pack and its expanded target", measured, packBytes)
	}
	for _, tc := range []struct {
		budget       string
		limit, value int64
	}{
		{"max-objects", measured.Objects - 1, measured.Objects},
		{"max-object-bytes", 128 << 10, serveAuditExpanded},
		{"max-attachment-bytes", measured.LargestAttachmentBytes - 1, measured.LargestAttachmentBytes},
		{"max-total-bytes", 128 << 10, measured.TotalBytes},
	} {
		serveSetBudget(f.servePushFixture, tc.budget, tc.limit)
		rejections := f.reject("A", map[string]string{ref: serveReasonOverBudget}, "origin", ref+":"+ref)
		requireServeOverBudget(t, rejections[0], tc.budget, tc.limit, tc.value)
		serveResetBudgets(f.servePushFixture)
	}
	if f.hostedHas(large) {
		t.Fatal("an over-budget audit push left its snapshot in accepted storage")
	}
	for _, check := range serveBudgetChecks(defaultReplicationBudgets(), measured) {
		serveSetBudget(f.servePushFixture, check.name, check.measurement)
	}
	f.pushOwn("A")
	serveResetBudgets(f.servePushFixture)

	// A second request for the same target: every snapshot object is in a
	// hosted pack of the chain already.
	f.audit("A", "request", "--at", large, "--rule", "check", "--intent", "inspect the same snapshot again")
	measured = measureServeTestActorPush(f.servePushFixture, "A")
	if measured.LargestObjectBytes >= 128<<10 || measured.TotalBytes >= 128<<10 {
		t.Fatalf("client measurement %+v counts the hosted snapshot again", measured)
	}
	serveSetBudget(f.servePushFixture, "max-object-bytes", 128<<10)
	serveSetBudget(f.servePushFixture, "max-total-bytes", 128<<10)
	f.pushOwn("A")
	serveResetBudgets(f.servePushFixture)

	// Two requests for a new target in one push share its snapshot.
	other := f.largeTarget("other", 'b')
	f.audit("A", "request", "--at", other, "--rule", "check", "--intent", "first look")
	f.audit("A", "request", "--at", other, "--rule", "check", "--intent", "second look")
	measured = measureServeTestActorPush(f.servePushFixture, "A")
	if measured.LargestObjectBytes != serveAuditExpanded || measured.TotalBytes >= 2*serveAuditExpanded {
		t.Fatalf("client measurement %+v does not count the shared snapshot exactly once", measured)
	}
	serveSetBudget(f.servePushFixture, "max-total-bytes", measured.TotalBytes-1)
	rejections := f.reject("A", map[string]string{ref: serveReasonOverBudget}, "origin", ref+":"+ref)
	requireServeOverBudget(t, rejections[0], "max-total-bytes", measured.TotalBytes-1, measured.TotalBytes)
	serveSetBudget(f.servePushFixture, "max-total-bytes", measured.TotalBytes)
	f.pushOwn("A")
}

// serveAuditTestHostedUsage counts what the hosted fact reader loads for
// the actor chains of the current repository, audit attachments included,
// and returns the bytes of the audit packs separately.
func serveAuditTestHostedUsage(t *testing.T) (serveFactUsage, int64) {
	t.Helper()
	commits := strings.Fields(mustGitText(t, "rev-list", "--glob=refs/hn/actors/*"))
	usage := serveFactUsage{Events: int64(len(commits))}
	packs := int64(0)
	for _, commit := range commits {
		members := strings.Fields(mustGitText(t, "ls-tree", "--name-only", commit))
		for _, path := range members {
			size, err := strconv.ParseInt(mustGitText(t, "cat-file", "-s", commit+":"+path), 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			usage.Bytes += size
			if path == "audit.pack" {
				packs += size
			}
		}
	}
	return usage, packs
}

// The hosted fact ceilings count audit packs and audit-result logs. As in
// TestServeFactsHostedFactCeilings the ceilings are lowered on the verdict
// context that the pre-receive hook builds, here in A's clone with a real
// authorization hosted and a request and result appended: one byte below
// the total is over-budget, a ceiling that fits everything but the packs is
// too, the exact total is accepted, and the hosted facts then load at
// exactly that ceiling and not one byte below it.
func TestServeAuditHostedCeilingsCountAuditAttachments(t *testing.T) {
	f := newServeAuditFixture(t)
	serveAuditID(t, f.audit("A", "rule", "authorize", "check", "--source", "pipeline", "--at", f.Policy))
	f.pushOwn("A")
	request := serveAuditID(t, f.audit("A", "request", "--at", f.Target, "--rule", "check", "--intent", "ceilings"))
	f.audit("A", "execute", request, "--backend", "host", "--allow-unsafe-host-execution")
	ref := f.actorRef("A")
	oldHead, newHead := f.hostedRef(ref), f.localHead("A", ref)
	t.Chdir(f.work["A"])
	gitDir, err := requireGitRepository()
	if err != nil {
		t.Fatal(err)
	}
	full, packs := serveAuditTestHostedUsage(t)
	if packs == 0 {
		t.Fatal("the fixture chain holds no audit pack")
	}
	mustGit(t, "update-ref", ref, oldHead)

	config, err := newServeRepoConfig("r", []string{"refs/heads/main"})
	if err != nil {
		t.Fatal(err)
	}
	verdictContext := func(ceilings serveHostedFactCeilings) *serveVerdictContext {
		ctx := newServeVerdictContext(gitDir, f.actors["A"], serveTestFingerprint("audit-ceilings"), config, emptyServeBranchOwners())
		ctx.HostedCeilings = ceilings
		return ctx
	}
	verdict := func(ceilings serveHostedFactCeilings) []serveRejection {
		t.Helper()
		updates := []serveRefUpdate{{Old: oldHead, New: newHead, Ref: ref}}
		classifyServeRefUpdates(config, updates)
		return evaluateServePush(verdictContext(ceilings), updates, servePushOptions{})
	}
	for _, bytes := range []int64{full.Bytes - 1, full.Bytes - packs} {
		rejections := verdict(serveHostedFactCeilings{Events: full.Events, Bytes: bytes})
		if len(rejections) != 1 {
			t.Fatalf("ceiling %d bytes: rejections %+v, want one", bytes, rejections)
		}
		requireServeOverBudget(t, rejections[0], serveHostedFactBytesBudget, bytes, full.Bytes)
	}
	exact := serveHostedFactCeilings{Events: full.Events, Bytes: full.Bytes}
	if rejections := verdict(exact); len(rejections) != 0 {
		t.Fatalf("an append to exactly the ceilings was rejected: %+v", rejections)
	}
	mustGit(t, "update-ref", ref, newHead)
	if _, err := verdictContext(exact).HostedEvents(); err != nil {
		t.Fatalf("hosted audit facts at exactly the ceilings do not load: %v", err)
	}
	if _, err := verdictContext(serveHostedFactCeilings{Events: full.Events, Bytes: full.Bytes - 1}).HostedEvents(); err == nil {
		t.Fatal("hosted audit facts loaded one byte below their measured usage")
	}
}
