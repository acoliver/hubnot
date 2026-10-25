package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// WP08: the actor chain, proposal and memory ref rules and their budgets.
// The black-box tests push real signed facts made by hn in client clones
// through the SSH shim into a hosted repository; every rejection goes
// through servePushFixture.reject, which requires the hosted refs to be
// byte-identical afterwards and exactly one parseable line pair per
// rejected ref (FR-012, NFR-003).

// newServeFactsFixture is a servePushFixture whose participants are real
// hn identities. Each participant's clone is created with git init and hn
// init before the hosted repository exists, so the policy on main can list
// the real actors as maintainers and trusted reviewers. Each actor's SSH
// key is bound to that actor.
func newServeFactsFixture(t *testing.T, names ...string) *servePushFixture {
	t.Helper()
	h := newServeHarness(t)
	f := &servePushFixture{
		t:       t,
		h:       h,
		hosted:  h.Layout().RepoGitDir("r"),
		actors:  map[string]string{},
		keys:    map[string]serveTestKey{},
		clients: map[string]serveHarnessClientEnv{},
		work:    map[string]string{},
	}
	for _, name := range names {
		work := filepath.Join(t.TempDir(), "work-"+name)
		runOperationalGit(t, "", "init", "-q", "-b", "main", work)
		runOperationalCommand(t, h.Binary, work, "init", "--name", "Actor "+name)
		f.actors[name] = readOperationalIdentity(t, h.Binary, work).Actor
		f.work[name] = work
	}
	maintainers := make([]string, 0, len(names))
	for _, name := range names {
		maintainers = append(maintainers, f.actors[name])
	}
	slices.Sort(maintainers)
	h.Serve("serve", "repo", "create", "r", "--from", serveHarnessSourceRepo(t, maintainers...))
	for _, name := range names {
		f.keys[name] = serveHarnessAddKey(h, f.actors[name])
		f.clients[name] = serveHarnessClient(t, h, f.keys[name], nil)
		f.git(name, "remote", "add", "origin", serveHarnessCloneURL("r"))
		f.git(name, "fetch", "-q", "origin")
		f.git(name, "checkout", "-q", "-B", "main", "origin/main")
	}
	if tip := f.hostedRef("refs/heads/main"); len(tip) != len(servePushZero) {
		t.Fatalf("hosted main %q is not a SHA-1 object ID; the fixtures assume SHA-1", tip)
	}
	return f
}

// hn runs hn in who's clone and requires it to succeed.
func (f *servePushFixture) hn(who string, args ...string) string {
	f.t.Helper()
	return f.clients[who].Hn(f.work[who], args...)
}

func (f *servePushFixture) actorRef(who string) string {
	return actorRef(f.actors[who])
}

// localHead is the value of ref in who's clone.
func (f *servePushFixture) localHead(who, ref string) string {
	f.t.Helper()
	return f.git(who, "rev-parse", ref)
}

// latest decodes the newest event of who's own actor chain.
func (f *servePushFixture) latest(who string) operationalEvent {
	f.t.Helper()
	return latestOperationalEvent(f.t, f.work[who], f.actorRef(who))
}

// pushOwn pushes who's actor ref to the server and requires acceptance.
func (f *servePushFixture) pushOwn(who string) {
	f.t.Helper()
	ref := f.actorRef(who)
	f.push(who, "origin", ref+":"+ref)
	if got, want := f.hostedRef(ref), f.localHead(who, ref); got != want {
		f.t.Fatalf("hosted %s = %q after an accepted push, want %s", ref, got, want)
	}
}

// issue appends one issue.open event to who's chain and returns it.
func (f *servePushFixture) issue(who, title string) operationalEvent {
	f.t.Helper()
	f.hn(who, "issue", "open", title)
	event := f.latest(who)
	if event.Kind != "issue.open" {
		f.t.Fatalf("latest event of %s is %s, want issue.open", who, event.Kind)
	}
	return event
}

// proposal commits one change on a fresh branch from origin/main, opens a
// proposal for it and returns the proposal event.
func (f *servePushFixture) proposal(who, title string) operationalEvent {
	f.t.Helper()
	head := f.commitOn(who, "origin/main")
	f.hn(who, "proposal", "open", "--base", f.git(who, "rev-parse", "origin/main"), "--head", head, title)
	event := f.latest(who)
	if event.Kind != "proposal.open" || event.Head != head {
		f.t.Fatalf("latest event of %s is %+v, want proposal.open of %s", who, event, head)
	}
	return event
}

// gitInput runs git in who's clone with stdin and returns trimmed stdout.
func (f *servePushFixture) gitInput(who string, input []byte, args ...string) string {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = f.work[who]
	command.Env = f.clients[who].Env
	command.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		f.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return strings.TrimSpace(string(output))
}

// alterHead replaces the newest event of who's chain with a commit whose
// event.json was changed after signing and returns the changed payload's
// event ID.
func (f *servePushFixture) alterHead(who, from, to string) string {
	f.t.Helper()
	ref := f.actorRef(who)
	head := f.localHead(who, ref)
	payload := runOperationalGitBytes(f.t, f.work[who], "show", head+":event.json")
	altered := bytes.Replace(payload, []byte(from), []byte(to), 1)
	if bytes.Equal(altered, payload) {
		f.t.Fatalf("event.json at %s does not contain %q", head, from)
	}
	blob := f.gitInput(who, altered, "hash-object", "-w", "--stdin")
	signature := f.git(who, "rev-parse", head+":signature")
	tree := f.gitInput(who, []byte("100644 blob "+blob+"\tevent.json\n100644 blob "+signature+"\tsignature\n"), "mktree")
	commit := f.git(who, "commit-tree", tree, "-p", head+"^", "-m", "altered after signing")
	f.git(who, "update-ref", ref, commit, head)
	return operationalDigest(altered)
}

// requireIDs checks the ids field of a rejection.
func requireServeIDs(t *testing.T, rejection serveRejection, want ...string) {
	t.Helper()
	if !reflect.DeepEqual(rejection.IDs, want) {
		t.Fatalf("%s %s ids = %q, want %q (sentence %q)", rejection.Ref, rejection.Code, rejection.IDs, want, rejection.Sentence)
	}
}

// US1-1: a feature branch and a proposal published with git push and
// hn sync are accepted, and the hosted repository holds the branch, the
// actor chain and the proposal code ref.
func TestServeFactsFeatureBranchAndProposal(t *testing.T) {
	f := newServeFactsFixture(t, "A", "B")
	head := f.commitOn("A", "origin/main")
	f.push("A", "origin", "HEAD:refs/heads/feature/a")
	f.hn("A", "proposal", "open", "--base", f.git("A", "rev-parse", "origin/main"), "--head", head, "Feature A")
	proposal := f.latest("A")
	// hn sync pushes the actor ref and the proposal ref separately, the
	// actor ref first.
	f.hn("A", "sync", "origin")
	for ref, want := range map[string]string{
		"refs/heads/feature/a":   head,
		f.actorRef("A"):          f.localHead("A", f.actorRef("A")),
		proposalRef(proposal.ID): head,
	} {
		if got := f.hostedRef(ref); got != want {
			t.Fatalf("hosted %s = %q, want %s", ref, got, want)
		}
	}
	// B imports both through the server.
	f.hn("B", "sync", "origin")
	if got := f.git("B", "rev-parse", acceptedProposalRef("origin", proposal.ID)); got != head {
		t.Fatalf("B imported proposal code %q, want %s", got, head)
	}
	for _, entry := range f.pushLog() {
		if entry.Verdict != servePushVerdictAccepted {
			t.Fatalf("push log entry %+v, want only accepted pushes", entry)
		}
	}
}

// Code-ref ordering (plan rule 2): candidate code arrives after the
// candidate. The actor push carrying proposal.open is accepted without
// the code ref, a later push of the code ref is accepted, a code ref
// whose candidate is not hosted is dependency-missing, and one push
// carrying both is accepted.
func TestServeFactsProposalCodeArrivesAfterCandidate(t *testing.T) {
	f := newServeFactsFixture(t, "A")
	first := f.proposal("A", "first")
	f.pushOwn("A")
	if got := f.hostedRef(proposalRef(first.ID)); got != "" {
		t.Fatalf("proposal ref hosted at %s before it was pushed", got)
	}
	ref := proposalRef(first.ID)
	f.push("A", "origin", ref+":"+ref)
	if got := f.hostedRef(ref); got != first.Head {
		t.Fatalf("hosted %s = %q, want %s", ref, got, first.Head)
	}

	second := f.proposal("A", "second")
	secondRef := proposalRef(second.ID)
	rejections := f.reject("A", map[string]string{secondRef: serveReasonDependencyMissing}, "origin", secondRef+":"+secondRef)
	requireServeIDs(t, rejections[0], second.ID)
	if rejections[0].Next != "publish the named fact or commit first" {
		t.Fatalf("next %q", rejections[0].Next)
	}
	actor := f.actorRef("A")
	f.push("A", "origin", actor+":"+actor, secondRef+":"+secondRef)
	if f.hostedRef(secondRef) != second.Head || f.hostedRef(actor) != f.localHead("A", actor) {
		t.Fatal("the push carrying both the candidate and its code ref was not applied")
	}
}

// A rejected actor update contributes no facts: a proposal ref in the same
// push that depends on its events is dependency-missing.
func TestServeFactsRejectedActorEventsDoNotReachLaterChecks(t *testing.T) {
	f := newServeFactsFixture(t, "A")
	f.issue("A", "hosted first")
	f.pushOwn("A")
	candidate := f.proposal("A", "candidate")
	f.issue("A", "will be altered")
	altered := f.alterHead("A", "will be altered", "was altered")
	actor, code := f.actorRef("A"), proposalRef(candidate.ID)
	rejections := f.reject("A", map[string]string{
		actor: serveReasonActorChainInvalid,
		code:  serveReasonDependencyMissing,
	}, "origin", actor+":"+actor, code+":"+code)
	requireServeIDs(t, rejections[0], altered)
	requireServeIDs(t, rejections[1], candidate.ID)
}

// US1-3: an actor ref of another actor is refused, and that actor's hosted
// chain is unchanged.
func TestServeFactsActorKeyMismatch(t *testing.T) {
	f := newServeFactsFixture(t, "A", "B")
	f.issue("B", "B one")
	f.pushOwn("B")
	f.issue("B", "B two")
	bRef := f.actorRef("B")
	f.git("A", "fetch", "-q", f.work["B"], bRef+":"+bRef)
	hosted := f.hostedRef(bRef)
	rejections := f.reject("A", map[string]string{bRef: serveReasonActorKeyMismatch}, "origin", bRef+":"+bRef)
	if want := "push only refs/hn/actors/" + f.actors["A"]; rejections[0].Next != want {
		t.Fatalf("next %q, want %q", rejections[0].Next, want)
	}
	if want := bRef + " belongs to actor " + f.actors["B"] + " and this key is bound to actor " + f.actors["A"] + "."; rejections[0].Sentence != want {
		t.Fatalf("sentence %q, want %q", rejections[0].Sentence, want)
	}
	if f.hostedRef(bRef) != hosted {
		t.Fatal("B's hosted chain moved")
	}
	// B itself may append the same events.
	f.pushOwn("B")
}

// US1-4: an event whose event.json changed after signing is refused with
// the full event ID of the changed payload.
func TestServeFactsAlteredEventIsInvalid(t *testing.T) {
	f := newServeFactsFixture(t, "A")
	f.issue("A", "hosted")
	f.pushOwn("A")
	f.issue("A", "original title")
	altered := f.alterHead("A", "original title", "forged title")
	ref := f.actorRef("A")
	rejections := f.reject("A", map[string]string{ref: serveReasonActorChainInvalid}, "origin", ref+":"+ref)
	requireServeIDs(t, rejections[0], altered)
	if !strings.Contains(rejections[0].Sentence, altered) || !strings.Contains(rejections[0].Sentence, "signature") {
		t.Fatalf("sentence %q does not name the event and its signature", rejections[0].Sentence)
	}

	// A first event that is altered is named the same way.
	g := newServeFactsFixture(t, "A")
	g.issue("A", "first title")
	ref = g.actorRef("A")
	head := g.localHead("A", ref)
	payload := runOperationalGitBytes(t, g.work["A"], "show", head+":event.json")
	forged := bytes.Replace(payload, []byte("first title"), []byte("first forged"), 1)
	blob := g.gitInput("A", forged, "hash-object", "-w", "--stdin")
	tree := g.gitInput("A", []byte("100644 blob "+blob+"\tevent.json\n100644 blob "+g.git("A", "rev-parse", head+":signature")+"\tsignature\n"), "mktree")
	g.git("A", "update-ref", ref, g.git("A", "commit-tree", tree, "-m", "forged"), head)
	rejections = g.reject("A", map[string]string{ref: serveReasonActorChainInvalid}, "origin", ref+":"+ref)
	requireServeIDs(t, rejections[0], operationalDigest(forged))
}

// US1-5: deleting, rewinding or diverging from a hosted actor chain is
// actor-chain-rewrite (C-007).
func TestServeFactsActorChainRewrite(t *testing.T) {
	f := newServeFactsFixture(t, "A")
	f.issue("A", "one")
	f.issue("A", "two")
	f.pushOwn("A")
	ref := f.actorRef("A")
	hosted := f.hostedRef(ref)
	earlier := f.git("A", "rev-parse", ref+"^")

	rejections := f.reject("A", map[string]string{ref: serveReasonActorChainRewrite}, "origin", "+"+earlier+":"+ref)
	requireServeIDs(t, rejections[0], hosted, earlier)
	if rejections[0].Next != "append new events instead" {
		t.Fatalf("next %q", rejections[0].Next)
	}
	rejections = f.reject("A", map[string]string{ref: serveReasonActorChainRewrite}, "origin", ":"+ref)
	requireServeIDs(t, rejections[0], hosted)

	// A diverging chain: the same sequence with a different event.
	f.issue("A", "three")
	f.pushOwn("A")
	three := f.hostedRef(ref)
	f.git("A", "update-ref", ref, hosted, three)
	f.issue("A", "three, differently")
	diverged := f.localHead("A", ref)
	rejections = f.reject("A", map[string]string{ref: serveReasonActorChainRewrite}, "--force", "origin", ref+":"+ref)
	requireServeIDs(t, rejections[0], three, diverged)
}

// A reference to a fact the server does not host is dependency-missing,
// naming the missing event in full.
func TestServeFactsDependencyMissing(t *testing.T) {
	f := newServeFactsFixture(t, "A", "B")
	proposal := f.proposal("B", "B's change")
	bRef, code := f.actorRef("B"), proposalRef(proposal.ID)
	// A imports B's proposal straight from B's clone, not through the
	// server, and reviews it.
	f.git("A", "fetch", "-q", f.work["B"], bRef+":"+bRef, code+":"+code)
	f.hn("A", "review", proposal.ID, "--approve", "--body", "reviewed from B's clone")
	if review := f.latest("A"); review.Kind != "review.submit" || review.Subject != proposal.ID {
		t.Fatalf("A's latest event is %+v, want a review of %s", review, proposal.ID)
	}
	aRef := f.actorRef("A")
	rejections := f.reject("A", map[string]string{aRef: serveReasonDependencyMissing}, "origin", aRef+":"+aRef)
	requireServeIDs(t, rejections[0], proposal.ID)
	if !strings.Contains(rejections[0].Sentence, proposal.ID) {
		t.Fatalf("sentence %q does not name %s", rejections[0].Sentence, proposal.ID)
	}
	// Once B publishes, A's review is accepted.
	f.pushOwn("B")
	f.pushOwn("A")
}

// FR-010: hosted proposal refs never move or disappear, and a new one must
// equal its candidate's signed head.
func TestServeFactsProposalRefRules(t *testing.T) {
	f := newServeFactsFixture(t, "A")
	proposal := f.proposal("A", "immutable")
	f.hn("A", "sync", "origin")
	ref := proposalRef(proposal.ID)
	if f.hostedRef(ref) != proposal.Head {
		t.Fatalf("hosted %s = %q, want %s", ref, f.hostedRef(ref), proposal.Head)
	}
	other := f.commitOn("A", proposal.Head)
	rejections := f.reject("A", map[string]string{ref: serveReasonProposalRefImmutable}, "origin", "+"+other+":"+ref)
	requireServeIDs(t, rejections[0], proposal.Head, other)
	if rejections[0].Next != "create a revision instead" {
		t.Fatalf("next %q", rejections[0].Next)
	}
	rejections = f.reject("A", map[string]string{ref: serveReasonProposalRefImmutable}, "origin", ":"+ref)
	requireServeIDs(t, rejections[0], proposal.Head)

	pending := f.proposal("A", "pointed elsewhere")
	f.pushOwn("A")
	wrong := f.commitOn("A", "origin/main")
	pendingRef := proposalRef(pending.ID)
	rejections = f.reject("A", map[string]string{pendingRef: serveReasonProposalRefMismatch}, "origin", wrong+":"+pendingRef)
	requireServeIDs(t, rejections[0], pending.ID, pending.Head, wrong)
	if rejections[0].Next != "push the signed head" {
		t.Fatalf("next %q", rejections[0].Next)
	}
	// A code ref named after an event that is not a proposal.
	issue := f.issue("A", "not a proposal")
	f.pushOwn("A")
	issueRef := proposalRef(issue.ID)
	rejections = f.reject("A", map[string]string{issueRef: serveReasonProposalRefMismatch}, "origin", wrong+":"+issueRef)
	requireServeIDs(t, rejections[0], issue.ID, wrong)
	f.push("A", "origin", pending.Head+":"+pendingRef)
}

// FR-010: memory streams are pushed only by their owner, append-only, and
// must be valid hn-memory/0 streams.
func TestServeFactsMemoryRefRules(t *testing.T) {
	f := newServeFactsFixture(t, "A", "B")
	record := func(content string) string {
		output := f.hn("A", "memory", "record", "--kind", "observation", "--at", "HEAD", "--applies", "exact",
			"--topic", "serve", "--content", content, "--json")
		var result memoryCommandResultV0
		if err := decodeServeTestJSON(output, &result); err != nil {
			t.Fatalf("hn memory record output %q: %v", output, err)
		}
		return result.Stream
	}
	stream := record("first observation")
	f.hn("A", "sync", "origin")
	ref, err := memoryRef(f.actors["A"], stream)
	if err != nil {
		t.Fatal(err)
	}
	first := f.hostedRef(ref)
	if first == "" || first != f.localHead("A", ref) {
		t.Fatalf("hosted %s = %q after sync, want A's stream head", ref, first)
	}

	record("second observation")
	second := f.localHead("A", ref)
	f.git("B", "fetch", "-q", f.work["A"], ref+":"+ref)
	rejections := f.reject("B", map[string]string{ref: serveReasonMemoryKeyMismatch}, "origin", ref+":"+ref)
	if rejections[0].Next != "push only your own memory refs" {
		t.Fatalf("next %q", rejections[0].Next)
	}

	f.push("A", "origin", ref+":"+ref)
	rejections = f.reject("A", map[string]string{ref: serveReasonMemoryInvalid}, "origin", "+"+first+":"+ref)
	requireServeIDs(t, rejections[0], second, first)
	rejections = f.reject("A", map[string]string{ref: serveReasonMemoryInvalid}, "origin", ":"+ref)
	requireServeIDs(t, rejections[0], second)

	// A stream whose commit is not a signed memory fails hn-memory/0
	// validation and names the stream.
	tree := f.git("A", "rev-parse", second+"^{tree}")
	bogus := f.git("A", "commit-tree", tree, "-p", second, "-m", "unsigned copy")
	rejections = f.reject("A", map[string]string{ref: serveReasonMemoryInvalid}, "origin", bogus+":"+ref)
	requireServeIDs(t, rejections[0], stream)
}

func decodeServeTestJSON(output string, value any) error {
	return decodePrivateJSON([]byte(output), value, "test output")
}

// serveSetBudget changes one budget of r through hn serve repo set.
func serveSetBudget(f *servePushFixture, name string, value int64) {
	f.t.Helper()
	f.h.Serve("serve", "repo", "set", "r", "--"+name, strconv.FormatInt(value, 10))
}

func serveResetBudgets(f *servePushFixture) {
	f.t.Helper()
	defaults := defaultReplicationBudgets()
	f.h.Serve("serve", "repo", "set", "r",
		"--max-events", strconv.FormatInt(defaults.MaxEvents, 10),
		"--max-objects", strconv.FormatInt(defaults.MaxObjects, 10),
		"--max-object-bytes", strconv.FormatInt(defaults.MaxObjectBytes, 10),
		"--max-attachment-bytes", strconv.FormatInt(defaults.MaxAttachmentBytes, 10),
		"--max-total-bytes", strconv.FormatInt(defaults.MaxTotalBytes, 10))
}

// measureServeTestActorPush measures who's unpushed actor events with the
// client replication helper: the client clone is the quarantine and the
// hosted repository holds the previously accepted value.
func measureServeTestActorPush(f *servePushFixture, who string) ReplicationMeasurements {
	f.t.Helper()
	ref := f.actorRef(who)
	unbounded := ReplicationBudgets{MaxEvents: 1 << 62, MaxObjects: 1 << 62, MaxObjectBytes: 1 << 62, MaxAttachmentBytes: 1 << 62, MaxTotalBytes: 1 << 62}
	measured, err := measureQuarantinedSelectionUnderBudgets(filepath.Join(f.work[who], ".git"), f.hosted, replicationActor,
		f.localHead(who, ref), f.hostedRef(ref), unbounded, "test")
	if err != nil {
		f.t.Fatal(err)
	}
	return measured
}

// requireServeOverBudget checks the budget fields of an over-budget
// rejection.
func requireServeOverBudget(t *testing.T, rejection serveRejection, budget string, limit, measured int64) {
	t.Helper()
	if rejection.Code != serveReasonOverBudget || rejection.Budget != budget || rejection.Limit != limit || rejection.Measured != measured {
		t.Fatalf("rejection %+v, want over-budget budget=%s limit=%d measured=%d", rejection, budget, limit, measured)
	}
	if rejection.Next != "split the history or ask the administrator" {
		t.Fatalf("next %q", rejection.Next)
	}
}

// NFR-002: for every budget a push at the limit is accepted and a push one
// over is rejected with the budget, the limit and the measurement, and
// leaves the hosted refs byte-identical.
func TestServeFactsBudgetsAreInclusive(t *testing.T) {
	f := newServeFactsFixture(t, "A")
	ref := f.actorRef("A")
	f.issue("A", "hosted")
	f.pushOwn("A")

	// max-events counts the whole chain, hosted prefix included.
	hostedEvents := int64(1)
	limit := hostedEvents + 1
	serveSetBudget(f, "max-events", limit)
	f.issue("A", "event one")
	f.issue("A", "event two")
	rejections := f.reject("A", map[string]string{ref: serveReasonOverBudget}, "origin", ref+":"+ref)
	requireServeOverBudget(t, rejections[0], "max-events", limit, limit+1)
	f.push("A", "origin", f.localHead("A", ref)+"~1:"+ref)
	serveResetBudgets(f)
	f.pushOwn("A")

	for _, budget := range []string{"max-objects", "max-object-bytes", "max-total-bytes"} {
		f.issue("A", "measured by "+budget)
		measured := measureServeTestActorPush(f, "A")
		value := map[string]int64{
			"max-objects":      measured.Objects,
			"max-object-bytes": measured.LargestObjectBytes,
			"max-total-bytes":  measured.TotalBytes,
		}[budget]
		serveSetBudget(f, budget, value-1)
		rejections := f.reject("A", map[string]string{ref: serveReasonOverBudget}, "origin", ref+":"+ref)
		requireServeOverBudget(t, rejections[0], budget, value-1, value)
		serveSetBudget(f, budget, value)
		f.pushOwn("A")
		serveResetBudgets(f)
	}

	// max-attachment-bytes: a run result carries its log as an attachment.
	f.git("A", "fetch", "-q", "origin")
	f.git("A", "checkout", "-q", "-B", "pipeline", "origin/main")
	writeOperationalPipeline(t, f.work["A"])
	f.git("A", "add", ".hn")
	f.git("A", "commit", "-q", "-m", "add a pipeline")
	head := f.git("A", "rev-parse", "HEAD")
	f.git("A", "checkout", "-q", "main")
	f.hn("A", "proposal", "open", "--base", f.git("A", "rev-parse", "origin/main"), "--head", head, "with a pipeline")
	proposal := f.latest("A")
	f.hn("A", "run", "request", proposal.ID, "test")
	request := f.latest("A")
	f.hn("A", "run", "execute", request.ID, "--backend", "host", "--allow-unsafe-host-execution")
	if result := f.latest("A"); result.Kind != "run.result" {
		t.Fatalf("latest event is %s, want run.result", result.Kind)
	}
	measured := measureServeTestActorPush(f, "A")
	if measured.LargestAttachmentBytes < 2 {
		t.Fatalf("run log of %d bytes is too small for the fixture", measured.LargestAttachmentBytes)
	}
	serveSetBudget(f, "max-attachment-bytes", measured.LargestAttachmentBytes-1)
	rejections = f.reject("A", map[string]string{ref: serveReasonOverBudget}, "origin", ref+":"+ref)
	requireServeOverBudget(t, rejections[0], "max-attachment-bytes", measured.LargestAttachmentBytes-1, measured.LargestAttachmentBytes)
	serveSetBudget(f, "max-attachment-bytes", measured.LargestAttachmentBytes)
	f.pushOwn("A")
	serveResetBudgets(f)

	// Proposal and memory refs are measured too.
	code := proposalRef(proposal.ID)
	serveSetBudget(f, "max-objects", 1)
	rejections = f.reject("A", map[string]string{code: serveReasonOverBudget}, "origin", head+":"+code)
	if rejections[0].Budget != "max-objects" || rejections[0].Limit != 1 || rejections[0].Measured <= 1 {
		t.Fatalf("proposal over-budget %+v", rejections[0])
	}
	serveResetBudgets(f)
	f.push("A", "origin", head+":"+code)

	f.hn("A", "memory", "record", "--kind", "observation", "--at", "HEAD", "--applies", "exact",
		"--topic", "budget", "--content", "measured", "--json")
	memoryRefs := strings.Fields(f.git("A", "for-each-ref", "--format=%(refname)", memoryRefPrefix))
	if len(memoryRefs) != 1 {
		t.Fatalf("memory refs %q, want one", memoryRefs)
	}
	serveSetBudget(f, "max-total-bytes", 1)
	rejections = f.reject("A", map[string]string{memoryRefs[0]: serveReasonOverBudget}, "origin", memoryRefs[0]+":"+memoryRefs[0])
	if rejections[0].Budget != "max-total-bytes" || rejections[0].Limit != 1 {
		t.Fatalf("memory over-budget %+v", rejections[0])
	}
	serveResetBudgets(f)
	f.push("A", "origin", memoryRefs[0]+":"+memoryRefs[0])
}

// The range measurement the server uses gives the same totals and the
// same inclusive limits as the client replication helper for the same
// chain, including run logs that repeat across events.
func TestServeFactsRangeMeasurementMatchesSelectionHelper(t *testing.T) {
	inReplicationTestRepository(t)
	gitDir, err := requireGitRepository()
	if err != nil {
		t.Fatal(err)
	}
	identity := testIdentity(t, "Range")
	log := []byte("the same run log in two events\n")
	appendRun := func() {
		event, err := nextEvent(identity, "run.result")
		if err != nil {
			t.Fatal(err)
		}
		event.Subject = "sha256:" + strings.Repeat("a", 64)
		event.Pipeline = "test"
		event.Definition = "sha256:" + strings.Repeat("b", 64)
		event.Commit = strings.Repeat("c", 40)
		event.Outcome = "passed"
		event.DurationMS = 1
		event.Log = eventID(log)
		event.Backend = "host"
		event.Platform = "test/test"
		event.Runner = "hn/test"
		if _, err := appendEventWithAttachments(event, identity, map[string][]byte{"log.txt": log}); err != nil {
			t.Fatal(err)
		}
	}
	appendIssue := func(title string) {
		event, err := nextEvent(identity, "issue.open")
		if err != nil {
			t.Fatal(err)
		}
		event.Title = title
		if _, err := appendEvent(event, identity); err != nil {
			t.Fatal(err)
		}
	}
	appendIssue("first")
	appendRun()
	old, _, _ := refValue(actorRef(identity.Actor))
	appendIssue("second")
	appendRun()
	appendIssue("third")
	head, _, _ := refValue(actorRef(identity.Actor))

	unbounded := ReplicationBudgets{MaxEvents: 1 << 62, MaxObjects: 1 << 62, MaxObjectBytes: 1 << 62, MaxAttachmentBytes: 1 << 62, MaxTotalBytes: 1 << 62}
	for _, oldHead := range []string{"", old} {
		var prefix []StoredEvent
		if oldHead != "" {
			if prefix, err = loadActorEventsAt(gitDir, oldHead); err != nil {
				t.Fatal(err)
			}
		}
		want, err := measureQuarantinedSelectionUnderBudgets(gitDir, gitDir, replicationActor, head, oldHead, unbounded, "full")
		if err != nil {
			t.Fatal(err)
		}
		got, err := measureServeActorRangeUnderBudgets(gitDir, oldHead, head, prefix, unbounded, "range")
		if err != nil || got != want {
			t.Fatalf("old=%q: range measurement %+v, %v; want %+v", oldHead, got, err, want)
		}
		if oldHead != "" && want.Objects != 3*4 {
			// Three new commits, trees, event.json and signature blobs; the
			// repeated log blob is already hosted.
			t.Fatalf("old=%q: %d objects, want 12", oldHead, want.Objects)
		}
		for index, check := range serveBudgetChecks(unbounded, want) {
			for _, delta := range []int64{0, -1} {
				budgets := unbounded
				limit := check.measurement + delta
				switch index {
				case 0:
					budgets.MaxEvents = limit
				case 1:
					budgets.MaxObjects = limit
				case 2:
					budgets.MaxObjectBytes = limit
				case 3:
					budgets.MaxAttachmentBytes = limit
				case 4:
					budgets.MaxTotalBytes = limit
				}
				fullMeasured, fullErr := measureQuarantinedSelectionUnderBudgets(gitDir, gitDir, replicationActor, head, oldHead, budgets, "full")
				rangeMeasured, rangeErr := measureServeActorRangeUnderBudgets(gitDir, oldHead, head, prefix, budgets, "range")
				var fullExceeded, rangeExceeded replicationBudgetExceeded
				if errors.As(fullErr, &fullExceeded) != errors.As(rangeErr, &rangeExceeded) || (delta == 0) != (rangeErr == nil) {
					t.Fatalf("old=%q %s limit %d: full %v, range %v", oldHead, check.name, limit, fullErr, rangeErr)
				}
				if delta == 0 {
					continue
				}
				fullRejection := serveOverBudgetRejection("r", budgets, fullMeasured)
				rangeRejection := serveOverBudgetRejection("r", budgets, rangeMeasured)
				sameFields := fullRejection.Budget == rangeRejection.Budget && fullRejection.Limit == rangeRejection.Limit &&
					fullRejection.Measured == rangeRejection.Measured
				if !sameFields || rangeRejection.Budget != check.name || rangeRejection.Measured != check.measurement {
					t.Fatalf("old=%q %s one over: full %+v, range %+v", oldHead, check.name, fullRejection, rangeRejection)
				}
			}
		}
	}
}

// The mapping reports the first exceeded budget in the contract's order.
func TestServeFactsOverBudgetMapping(t *testing.T) {
	budgets := ReplicationBudgets{MaxEvents: 10, MaxObjects: 20, MaxObjectBytes: 30, MaxAttachmentBytes: 40, MaxTotalBytes: 50}
	for _, tc := range []struct {
		measured ReplicationMeasurements
		budget   string
		limit    int64
		value    int64
	}{
		{ReplicationMeasurements{Events: 11, Objects: 21, LargestObjectBytes: 31, LargestAttachmentBytes: 41, TotalBytes: 51}, "max-events", 10, 11},
		{ReplicationMeasurements{Events: 10, Objects: 21, TotalBytes: 51}, "max-objects", 20, 21},
		{ReplicationMeasurements{LargestObjectBytes: 31, TotalBytes: 51}, "max-object-bytes", 30, 31},
		{ReplicationMeasurements{LargestAttachmentBytes: 41, TotalBytes: 51}, "max-attachment-bytes", 40, 41},
		{ReplicationMeasurements{Events: 10, Objects: 20, LargestObjectBytes: 30, LargestAttachmentBytes: 40, TotalBytes: 51}, "max-total-bytes", 50, 51},
	} {
		rejection := serveOverBudgetRejection("refs/hn/actors/x", budgets, tc.measured)
		if rejection.Code != serveReasonOverBudget || rejection.Budget != tc.budget || rejection.Limit != tc.limit || rejection.Measured != tc.value {
			t.Fatalf("%+v mapped to %+v, want %s limit=%d measured=%d", tc.measured, rejection, tc.budget, tc.limit, tc.value)
		}
		renderServeRejection(serveRejection{Ref: "refs/hn/actors/x", Actor: "-", Code: rejection.Code, Budget: rejection.Budget, Limit: rejection.Limit, Measured: rejection.Measured, Sentence: rejection.Sentence})
	}
}

// T051: when the hosted facts cannot be read, every fact ref and protected
// update fails closed with actor-chain-invalid, and the other classes are
// still checked without loading them.
func TestServeFactsHostedLoadFailureFailsClosed(t *testing.T) {
	config, err := newServeRepoConfig("r", []string{"refs/heads/main"})
	if err != nil {
		t.Fatal(err)
	}
	actor := serveStateTestActor("hosted-failure")
	ctx := newServeVerdictContext(t.TempDir(), actor, serveTestFingerprint("hosted-failure"), config, emptyServeBranchOwners())
	loads := 0
	ctx.loadHostedEvents = func() ([]StoredEvent, error) {
		loads++
		return nil, errors.New("damaged hosted history")
	}
	oid := strings.Repeat("1", 40)
	stream := strings.Repeat("d", 64)
	updates := []serveRefUpdate{
		{Old: servePushZero, New: oid, Ref: "refs/hn/actors/" + actor},
		{Old: servePushZero, New: oid, Ref: "refs/hn/proposals/" + strings.Repeat("e", 64)},
		{Old: servePushZero, New: oid, Ref: "refs/hn/memory/" + actor + "/" + stream},
		{Old: oid, New: strings.Repeat("2", 40), Ref: "refs/heads/main"},
		{Old: servePushZero, New: oid, Ref: "refs/tags/fine"},
		{Old: oid, New: servePushZero, Ref: "refs/tags/moved"},
	}
	classifyServeRefUpdates(config, updates)
	rejections := evaluateServePush(ctx, updates, servePushOptions{})
	var got []string
	for _, rejection := range rejections {
		got = append(got, rejection.Ref+" "+rejection.Code)
		if rejection.Code == serveReasonActorChainInvalid && rejection.Sentence != serveHostedFactsSentence {
			t.Fatalf("sentence %q, want %q", rejection.Sentence, serveHostedFactsSentence)
		}
	}
	want := []string{
		"refs/heads/main actor-chain-invalid",
		"refs/hn/actors/" + actor + " actor-chain-invalid",
		"refs/hn/memory/" + actor + "/" + stream + " actor-chain-invalid",
		"refs/hn/proposals/" + strings.Repeat("e", 64) + " actor-chain-invalid",
		"refs/tags/moved tag-immutable",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rejections %q, want %q", got, want)
	}
	if loads != 1 {
		t.Fatalf("hosted facts loaded %d times, want once", loads)
	}
	if !strings.Contains(renderServeRejections(rejections), "hn-serve: "+serveHostedFactsSentence+"\n") {
		t.Fatal("the rendered sentence differs from the contract")
	}
}

// T053: the guide's example is the real output of US1-3, field for field.
func TestServeFactsDocumentedExample(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("docs", "serve-v0.md"))
	if err != nil {
		t.Fatal(err)
	}
	const marker = "<!-- example: reject-actor-key-mismatch -->\n"
	guide := string(contents)
	if strings.Count(guide, marker) != 1 {
		t.Fatalf("the serve guide must contain exactly one %q", strings.TrimSpace(marker))
	}
	_, after, _ := strings.Cut(guide, marker)
	_, after, found := strings.Cut(after, "```text\n")
	if !found {
		t.Fatal("no text block after the example marker")
	}
	block, _, found := strings.Cut(after, "```\n")
	if !found {
		t.Fatal("unterminated example block")
	}
	documented := strings.Split(strings.TrimSuffix(block, "\n"), "\n")
	if len(documented) != 2 {
		t.Fatalf("example has %d lines, want the two relayed lines", len(documented))
	}
	documentedRejection, err := parseServeRejectionLine(serveCleanStderrLine(documented[0]))
	if err != nil {
		t.Fatalf("documented line does not parse: %v", err)
	}
	documentedPusher := documentedRejection.Actor
	documentedOwner := strings.TrimPrefix(documentedRejection.Ref, serveActorRefPrefix)

	f := newServeFactsFixture(t, "A", "B")
	f.issue("B", "B one")
	f.pushOwn("B")
	f.issue("B", "B two")
	bRef := f.actorRef("B")
	f.git("A", "fetch", "-q", f.work["B"], bRef+":"+bRef)
	output := f.clients["A"].GitFailure(f.work["A"], "push", "origin", bRef+":"+bRef)
	_, lines := extractServeRejections(output)
	want := make([]string, 0, 2)
	for _, line := range documented {
		line = strings.ReplaceAll(line, documentedPusher, f.actors["A"])
		line = strings.ReplaceAll(line, documentedOwner, f.actors["B"])
		if !strings.HasPrefix(line, "remote: ") {
			t.Fatalf("documented line %q does not show Git's remote: prefix", line)
		}
		want = append(want, serveCleanStderrLine(line))
	}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("relayed lines:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	got, err := parseServeRejectionLine(lines[0])
	if err != nil {
		t.Fatal(err)
	}
	expected, err := parseServeRejectionLine(want[0])
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("fields %+v, want %+v", got, expected)
	}
}

// serveTestChain appends signed events to who's actor chain in-process,
// in who's clone, so a test controls run log sizes exactly.
type serveTestChain struct {
	t        *testing.T
	f        *servePushFixture
	who      string
	identity *Identity
}

func newServeTestChain(t *testing.T, f *servePushFixture, who string) *serveTestChain {
	t.Helper()
	t.Chdir(f.work[who])
	identity, err := loadIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if identity.Actor != f.actors[who] {
		t.Fatalf("identity in %s's clone is %s, want %s", who, identity.Actor, f.actors[who])
	}
	return &serveTestChain{t: t, f: f, who: who, identity: identity}
}

func (c *serveTestChain) issue(title string) {
	c.t.Helper()
	event, err := nextEvent(c.identity, "issue.open")
	if err != nil {
		c.t.Fatal(err)
	}
	event.Title = title
	if _, err := appendEvent(event, c.identity); err != nil {
		c.t.Fatal(err)
	}
}

// run appends a run request for proposal and a run result carrying log.
func (c *serveTestChain) run(proposal operationalEvent, log []byte) {
	c.t.Helper()
	request, err := nextEvent(c.identity, "run.request")
	if err != nil {
		c.t.Fatal(err)
	}
	request.Subject, request.Pipeline = proposal.ID, "test"
	request.Definition, request.Commit = "sha256:"+strings.Repeat("b", 64), proposal.Head
	stored, err := appendEvent(request, c.identity)
	if err != nil {
		c.t.Fatal(err)
	}
	result, err := nextEvent(c.identity, "run.result")
	if err != nil {
		c.t.Fatal(err)
	}
	result.Subject, result.Pipeline = stored.ID, request.Pipeline
	result.Definition, result.Commit = request.Definition, request.Commit
	result.Outcome, result.Log = "passed", eventID(log)
	result.Backend, result.Platform, result.Runner = "host", "linux/amd64", "hn/test"
	if _, err := appendEventWithAttachments(result, c.identity, map[string][]byte{"log.txt": log}); err != nil {
		c.t.Fatal(err)
	}
}

// Review cycle 1, issue 1: a run log the budgets admit stays readable. A
// log above the client reader's 8 MiB object limit and a log at the 16 MiB
// attachment budget are accepted with the default budgets, and an ordinary
// append after each is accepted too, which needs the hosted facts to load.
// One byte over the attachment budget is over-budget.
func TestServeFactsAdmittedRunLogsStayReadable(t *testing.T) {
	f := newServeFactsFixture(t, "A")
	proposal := f.proposal("A", "large signed runner logs")
	f.hn("A", "sync", "origin")
	chain := newServeTestChain(t, f, "A")
	ref := f.actorRef("A")
	attachmentBudget := defaultReplicationBudgets().MaxAttachmentBytes

	for _, size := range []int64{8<<20 + 1, attachmentBudget} {
		chain.run(proposal, bytes.Repeat([]byte("x"), int(size)))
		if measured := measureServeTestActorPush(f, "A"); measured.LargestAttachmentBytes != size {
			t.Fatalf("fixture log measures %d bytes, want %d", measured.LargestAttachmentBytes, size)
		}
		f.pushOwn("A")
		chain.issue(fmt.Sprintf("ordinary append after a %d-byte log", size))
		f.pushOwn("A")
	}

	hosted := f.hostedRef(ref)
	chain.run(proposal, bytes.Repeat([]byte("y"), int(attachmentBudget+1)))
	rejections := f.reject("A", map[string]string{ref: serveReasonOverBudget}, "origin", ref+":"+ref)
	requireServeOverBudget(t, rejections[0], "max-attachment-bytes", attachmentBudget, attachmentBudget+1)
	f.git("A", "update-ref", ref, hosted)
	chain.issue("ordinary append after the refused log")
	f.pushOwn("A")
}

// Review cycle 1, issue 2: when several budgets are exceeded at once, the
// rejection names the first in the order events, objects, largest object,
// largest attachment, total bytes, with complete measurements, for actor,
// proposal and memory refs alike. Every rejection leaves the hosted refs
// unchanged (servePushFixture.reject).
func TestServeFactsBudgetPriorityWithSeveralExceeded(t *testing.T) {
	f := newServeFactsFixture(t, "A")
	ref := f.actorRef("A")
	unbounded := ReplicationBudgets{MaxEvents: 1 << 62, MaxObjects: 1 << 62, MaxObjectBytes: 1 << 62, MaxAttachmentBytes: 1 << 62, MaxTotalBytes: 1 << 62}
	clientGitDir := filepath.Join(f.work["A"], ".git")
	measureClient := func(kind, ref string) ReplicationMeasurements {
		t.Helper()
		measured, err := measureQuarantinedSelectionUnderBudgets(clientGitDir, f.hosted, kind, f.localHead("A", ref), f.hostedRef(ref), unbounded, "test")
		if err != nil {
			t.Fatal(err)
		}
		return measured
	}
	type exceeded struct {
		budgets map[string]int64
		budget  string
		limit   int64
		value   int64
	}
	requireFirstExceeded := func(pushedRef, refspec string, cases []exceeded) {
		t.Helper()
		for _, tc := range cases {
			for name, value := range tc.budgets {
				serveSetBudget(f, name, value)
			}
			rejections := f.reject("A", map[string]string{pushedRef: serveReasonOverBudget}, "origin", refspec)
			requireServeOverBudget(t, rejections[0], tc.budget, tc.limit, tc.value)
			serveResetBudgets(f)
		}
	}

	// Actor ref: largest object before total bytes, objects before both.
	f.issue("A", strings.Repeat("a long title makes event.json the largest object ", 12))
	measured := measureServeTestActorPush(f, "A")
	requireFirstExceeded(ref, ref+":"+ref, []exceeded{
		{map[string]int64{"max-object-bytes": measured.LargestObjectBytes - 1, "max-total-bytes": 1},
			"max-object-bytes", measured.LargestObjectBytes - 1, measured.LargestObjectBytes},
		{map[string]int64{"max-objects": measured.Objects - 1, "max-object-bytes": 1, "max-total-bytes": 1},
			"max-objects", measured.Objects - 1, measured.Objects},
	})
	f.pushOwn("A")

	// Proposal ref: largest object before total bytes.
	proposal := f.proposal("A", "measured code")
	f.pushOwn("A")
	code := proposalRef(proposal.ID)
	f.git("A", "update-ref", code, proposal.Head)
	measured = measureClient(replicationProposal, code)
	requireFirstExceeded(code, proposal.Head+":"+code, []exceeded{
		{map[string]int64{"max-object-bytes": measured.LargestObjectBytes - 1, "max-total-bytes": 1},
			"max-object-bytes", measured.LargestObjectBytes - 1, measured.LargestObjectBytes},
	})
	f.push("A", "origin", proposal.Head+":"+code)

	// Actor ref with a run log: largest attachment before total bytes. The
	// log is the largest object and stays within max-object-bytes.
	chain := newServeTestChain(t, f, "A")
	chain.run(proposal, bytes.Repeat([]byte("z"), 4096))
	measured = measureServeTestActorPush(f, "A")
	if measured.LargestAttachmentBytes != 4096 {
		t.Fatalf("fixture log measures %d bytes, want 4096", measured.LargestAttachmentBytes)
	}
	requireFirstExceeded(ref, ref+":"+ref, []exceeded{
		{map[string]int64{"max-attachment-bytes": 4095, "max-total-bytes": 1},
			"max-attachment-bytes", 4095, 4096},
	})
	f.pushOwn("A")

	// Memory ref: largest object before total bytes.
	output := f.hn("A", "memory", "record", "--kind", "observation", "--at", "HEAD", "--applies", "exact",
		"--topic", "priority", "--content", strings.Repeat("a memory long enough to be the largest object ", 20), "--json")
	var result memoryCommandResultV0
	if err := decodeServeTestJSON(output, &result); err != nil {
		t.Fatalf("hn memory record output %q: %v", output, err)
	}
	memory, err := memoryRef(f.actors["A"], result.Stream)
	if err != nil {
		t.Fatal(err)
	}
	measured = measureClient(replicationMemory, memory)
	requireFirstExceeded(memory, memory+":"+memory, []exceeded{
		{map[string]int64{"max-object-bytes": measured.LargestObjectBytes - 1, "max-total-bytes": 1},
			"max-object-bytes", measured.LargestObjectBytes - 1, measured.LargestObjectBytes},
	})
	f.push("A", "origin", memory+":"+memory)
}

// serveTestHostedUsage counts what the hosted fact reader loads for the
// actor chains in the current repository: one event per commit and the
// bytes of each event.json, signature and run log, measured with git.
func serveTestHostedUsage(t *testing.T) serveFactUsage {
	t.Helper()
	commits := strings.Fields(mustGitText(t, "rev-list", "--glob=refs/hn/actors/*"))
	usage := serveFactUsage{Events: int64(len(commits))}
	for _, commit := range commits {
		for _, path := range []string{"event.json", "signature", "log.txt"} {
			if path == "log.txt" && !strings.Contains(mustGitText(t, "ls-tree", "--name-only", commit), "log.txt") {
				continue
			}
			size, err := strconv.ParseInt(mustGitText(t, "cat-file", "-s", commit+":"+path), 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			usage.Bytes += size
		}
	}
	return usage
}

// Review cycle 1, issue 1: the hosted fact ceilings. They are server
// constants (100,000 events and 256 MiB) too large to reach through real
// pushes in a test, so this test lowers them on the verdict context and
// runs evaluateServePush, the function the pre-receive hook calls, on real
// signed chains of two actors. Appends that bring the hosted facts exactly
// to a ceiling are accepted and the hosted facts then load under the same
// ceilings; one event or one byte over is over-budget. Lowering a ceiling
// by one after acceptance makes the load fail, so the accounting matches
// the reader exactly, including signature blobs with surrounding
// whitespace.
func TestServeFactsHostedFactCeilings(t *testing.T) {
	inReplicationTestRepository(t)
	gitDir, err := requireGitRepository()
	if err != nil {
		t.Fatal(err)
	}
	other, pusher := testIdentity(t, "Other"), testIdentity(t, "Pusher")
	issue := func(identity *Identity, title string) *StoredEvent {
		t.Helper()
		event, err := nextEvent(identity, "issue.open")
		if err != nil {
			t.Fatal(err)
		}
		event.Title = title
		stored, err := appendEvent(event, identity)
		if err != nil {
			t.Fatal(err)
		}
		return stored
	}
	issue(other, "hosted by another actor")
	issue(other, "also hosted by another actor")
	issue(pusher, "hosted first")

	// The appended event's signature blob carries a trailing newline, which
	// the reader trims but counts.
	pusherRef := actorRef(pusher.Actor)
	oldHead, _, _ := refValue(pusherRef)
	appended := issue(pusher, "appended by the push")
	signature := strings.TrimSpace(mustGitText(t, "cat-file", "-p", appended.Commit+":signature")) + "\n"
	blob := mustGitTextFromInput(t, []byte(signature), "hash-object", "-w", "--stdin")
	eventBlob := mustGitText(t, "rev-parse", appended.Commit+":event.json")
	tree := mustGitTextFromInput(t, []byte("100644 blob "+eventBlob+"\tevent.json\n100644 blob "+blob+"\tsignature\n"), "mktree")
	newHead := mustGitText(t, "commit-tree", tree, "-p", oldHead, "-m", "padded signature")
	mustGit(t, "update-ref", pusherRef, newHead)
	full := serveTestHostedUsage(t)
	mustGit(t, "update-ref", pusherRef, oldHead)

	config, err := newServeRepoConfig("r", []string{"refs/heads/main"})
	if err != nil {
		t.Fatal(err)
	}
	verdict := func(ceilings serveHostedFactCeilings) []serveRejection {
		t.Helper()
		ctx := newServeVerdictContext(gitDir, pusher.Actor, serveTestFingerprint("ceilings"), config, emptyServeBranchOwners())
		ctx.HostedCeilings = ceilings
		updates := []serveRefUpdate{{Old: oldHead, New: newHead, Ref: pusherRef}}
		classifyServeRefUpdates(config, updates)
		return evaluateServePush(ctx, updates, servePushOptions{})
	}
	loads := func(ceilings serveHostedFactCeilings) error {
		ctx := newServeVerdictContext(gitDir, pusher.Actor, serveTestFingerprint("ceilings"), config, emptyServeBranchOwners())
		ctx.HostedCeilings = ceilings
		_, err := ctx.HostedEvents()
		return err
	}
	requireCeiling := func(rejections []serveRejection, budget string, limit, measured int64) {
		t.Helper()
		if len(rejections) != 1 {
			t.Fatalf("rejections %+v, want one", rejections)
		}
		requireServeOverBudget(t, rejections[0], budget, limit, measured)
		renderServeRejections(rejections)
	}

	exact := serveHostedFactCeilings{Events: full.Events, Bytes: full.Bytes}
	requireCeiling(verdict(serveHostedFactCeilings{Events: full.Events - 1, Bytes: full.Bytes}),
		serveHostedFactEventsBudget, full.Events-1, full.Events)
	requireCeiling(verdict(serveHostedFactCeilings{Events: full.Events, Bytes: full.Bytes - 1}),
		serveHostedFactBytesBudget, full.Bytes-1, full.Bytes)
	requireCeiling(verdict(serveHostedFactCeilings{Events: full.Events - 1, Bytes: full.Bytes - 1}),
		serveHostedFactEventsBudget, full.Events-1, full.Events)
	if rejections := verdict(exact); len(rejections) != 0 {
		t.Fatalf("an append to exactly the ceilings was rejected: %+v", rejections)
	}

	// Git applies the accepted update; the next push reads the facts.
	mustGit(t, "update-ref", pusherRef, newHead)
	if err := loads(exact); err != nil {
		t.Fatalf("hosted facts at exactly the ceilings do not load: %v", err)
	}
	for _, lower := range []serveHostedFactCeilings{{Events: full.Events - 1, Bytes: full.Bytes}, {Events: full.Events, Bytes: full.Bytes - 1}} {
		if err := loads(lower); err == nil {
			t.Fatalf("hosted facts loaded under %+v, below their measured usage %+v", lower, full)
		}
	}
	if err := loads(defaultServeHostedFactCeilings()); err != nil {
		t.Fatalf("hosted facts do not load under the default ceilings: %v", err)
	}
}
