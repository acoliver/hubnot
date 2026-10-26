package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// WP09: protected branch governance. The black-box tests build real signed
// governance evidence with the public hn and git commands in client clones,
// push through the SSH shim into a hosted repository, and check every step
// of the protected branch table in contracts/push-verdict.md. Every
// rejection goes through servePushFixture.reject, which requires the hosted
// refs to be byte-identical afterwards and one parseable line pair per
// rejected ref (FR-012, FR-013, SC-002). After every accepted protected
// update a fresh verifier clone checks the candidate (SC-003).

// Governed proposal fixture. WP10, WP12 and WP13 reuse it.
//
// newServeGovernedFixture hosts repository r whose main carries a policy
// that requires one trusted approval, one passing result of pipeline test
// from a trusted runner and one maintainer acceptance. Four participants
// have their own clone, hn identity and bound SSH key: author A, reviewer
// R, runner U and maintainer M. Each clone saved the explicit --all
// selection for origin. The methods drive the public CLIs; each one runs
// hn sync origin before and after it acts, so the actor sees the others'
// facts and publishes its own.
//
//	Commit(parent, files)         commit in the author's clone
//	OpenProposal(base, head)      the author opens a candidate
//	Approve, RequestCI, RunCI     reviewer, maintainer and runner evidence
//	Accept, Reject                the maintainer's decisions
//	Revise(id, base, head)        the author revises a candidate
//	ReadyProposal(base, head)     all of the above for one accepted candidate
//	PushMain, RejectMain          push a commit to main and read the verdict
//	ServerTip, ServerRefs         read the hosted repository
//	SetMainForTest(commit)        administrator shortcut for preconditions
//	VerifyAccepted(candidate)     SC-003 check from a fresh verifier clone
type serveGovernedFixture struct {
	*servePushFixture

	// Participant names playing each role; all "M" with Solo.
	Author, Reviewer, Runner, Maintainer string

	// Base is the main tip the repository was created with. Policy is the
	// policy it carries.
	Base   string
	Policy serveGovernedPolicy

	// CIFailFlag is a path outside every repository. While it exists,
	// pipeline test fails; the definition is the same either way.
	CIFailFlag string

	proposals int
	scratch   int
	verifier  *serveHarnessClientEnv
}

// serveGovernedOptions configures newServeGovernedFixture.
type serveGovernedOptions struct {
	// Solo makes M every role: author, trusted reviewer with author
	// approval allowed, trusted runner and maintainer. One clone can then
	// build a governed candidate locally without publishing anything. The
	// methods still publish after acting; drive M's commands directly when
	// facts must stay local.
	Solo bool

	// ProtectedPaths opts the policy in to protected checks for these
	// paths, with the maintainer as the one protected-check reviewer.
	ProtectedPaths []string

	// AutomergePaths adds automerge rule "auto" for main, executed by the
	// maintainer for proposals by the author that change only these paths.
	AutomergePaths []string

	// RunnerReviews makes the runner a second trusted reviewer.
	RunnerReviews bool
}

// serveGovernedPolicy is an hn.policy/0 document by role. Every proposal
// needs one acceptance and every pipeline one passing result.
type serveGovernedPolicy struct {
	Maintainers         []string
	Reviewers           []string
	RequiredApprovals   int
	AllowAuthorApproval bool
	Pipelines           map[string][]string // pipeline name to trusted runners
	ProtectedPaths      []string            // protectedChecks paths; none leaves the section out
	ProtectedReviewers  []string            // protectedChecks trusted reviewers
	AutomergeActors     []string            // automerge rule "auto" executing actors
	AutomergeAuthors    []string            // automerge rule "auto" authors
	AutomergePaths      []string            // automerge rule "auto" paths; none leaves the section out
}

// JSON encodes the policy the way a repository stores it.
func (policy serveGovernedPolicy) JSON(t *testing.T) string {
	t.Helper()
	pipelines := map[string]any{}
	for name, runners := range policy.Pipelines {
		pipelines[name] = map[string]any{"requiredResults": 1, "trustedRunners": runners}
	}
	document := map[string]any{
		"version":     policyVersion,
		"maintainers": policy.Maintainers,
		"proposals": map[string]any{
			"requiredApprovals":   policy.RequiredApprovals,
			"requiredAccepts":     1,
			"trustedReviewers":    policy.Reviewers,
			"allowAuthorApproval": policy.AllowAuthorApproval,
		},
		"pipelines": pipelines,
	}
	if len(policy.ProtectedPaths) > 0 {
		document["protectedChecks"] = map[string]any{
			"paths": policy.ProtectedPaths, "trustedReviewers": policy.ProtectedReviewers, "requiredApprovals": 1,
		}
	}
	if len(policy.AutomergePaths) > 0 {
		document["automerge"] = map[string]any{"rules": []map[string]any{{
			"name": "auto", "targetBranch": "main", "actors": policy.AutomergeActors,
			"authors": policy.AutomergeAuthors, "paths": policy.AutomergePaths,
		}}}
	}
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded) + "\n"
}

// serveGovernedPipelineJSON is pipeline test: one step that fails while
// flag exists.
func serveGovernedPipelineJSON(t *testing.T, flag string) string {
	t.Helper()
	encoded, err := json.MarshalIndent(map[string]any{
		"version": "hn.pipeline/0",
		"steps": []any{map[string]any{
			"name": "governed-check", "command": "sh",
			"args": []string{"-c", "test ! -e '" + flag + "'"}, "timeoutSeconds": 10,
		}},
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded) + "\n"
}

// serveGovernedRemove as a Commit file content removes the file.
const serveGovernedRemove = "\x00remove"

const (
	serveGovernedMain       = "refs/heads/main"
	serveGovernedPolicyPath = ".hn/policy.json"
)

func newServeGovernedFixture(t *testing.T, options serveGovernedOptions) *serveGovernedFixture {
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
	names := []string{"A", "M", "R", "U"}
	for _, name := range names {
		work := filepath.Join(t.TempDir(), "work-"+name)
		runOperationalGit(t, "", "init", "-q", "-b", "main", work)
		runOperationalCommand(t, h.Binary, work, "init", "--name", "Governed "+name)
		f.actors[name] = readOperationalIdentity(t, h.Binary, work).Actor
		f.work[name] = work
	}
	g := &serveGovernedFixture{
		servePushFixture: f,
		Author:           "A", Reviewer: "R", Runner: "U", Maintainer: "M",
		CIFailFlag: filepath.Join(t.TempDir(), "ci-fails"),
	}
	if options.Solo {
		g.Author, g.Reviewer, g.Runner = "M", "M", "M"
	}
	g.Policy = serveGovernedPolicy{
		Maintainers:         []string{f.actors[g.Maintainer]},
		Reviewers:           []string{f.actors[g.Reviewer]},
		RequiredApprovals:   1,
		AllowAuthorApproval: options.Solo,
		Pipelines:           map[string][]string{"test": {f.actors[g.Runner]}},
	}
	if len(options.ProtectedPaths) > 0 {
		g.Policy.ProtectedPaths = options.ProtectedPaths
		g.Policy.ProtectedReviewers = []string{f.actors[g.Maintainer]}
	}
	if options.RunnerReviews && g.Runner != g.Reviewer {
		g.Policy.Reviewers = append(g.Policy.Reviewers, f.actors[g.Runner])
	}
	if len(options.AutomergePaths) > 0 {
		g.Policy.AutomergePaths = options.AutomergePaths
		g.Policy.AutomergeActors = []string{f.actors[g.Maintainer]}
		g.Policy.AutomergeAuthors = []string{f.actors[g.Author]}
	}

	source := filepath.Join(t.TempDir(), "source")
	initOperationalRepository(t, source, "Source")
	serveHarnessWriteFile(t, source, serveGovernedPolicyPath, g.Policy.JSON(t))
	serveHarnessWriteFile(t, source, filepath.Join(".hn", "pipelines", "test.json"), serveGovernedPipelineJSON(t, g.CIFailFlag))
	serveHarnessCommit(t, source, "governed base")
	h.Serve("serve", "repo", "create", "r", "--from", source)

	for _, name := range names {
		f.keys[name] = serveHarnessAddKey(h, f.actors[name])
		f.clients[name] = serveHarnessClient(t, h, f.keys[name], nil)
		f.git(name, "remote", "add", "origin", serveHarnessCloneURL("r"))
		f.git(name, "fetch", "-q", "origin")
		f.git(name, "checkout", "-q", "-B", "main", "origin/main")
		f.hn(name, "replication", "select", "origin", "--all")
	}
	g.Base = g.ServerTip("main")
	if len(g.Base) != len(servePushZero) {
		t.Fatalf("hosted main %q is not a SHA-1 object ID; the fixtures assume SHA-1", g.Base)
	}
	return g
}

// Commit adds one commit on parent in the author's clone and returns it.
// files maps repository paths to contents, serveGovernedRemove deletes a
// path, and no files writes one new uniquely named file.
func (g *serveGovernedFixture) Commit(parent string, files map[string]string) string {
	g.t.Helper()
	who := g.Author
	g.git(who, "checkout", "-q", "--detach", parent)
	g.commits++
	if len(files) == 0 {
		files = map[string]string{fmt.Sprintf("change-%d", g.commits): fmt.Sprintf("governed change %d\n", g.commits)}
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if files[path] == serveGovernedRemove {
			g.git(who, "rm", "-q", "--", path)
			continue
		}
		full := filepath.Join(g.work[who], path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			g.t.Fatal(err)
		}
		writeServeTestFile(g.t, full, files[path], 0o644)
		g.git(who, "add", "--", path)
	}
	g.git(who, "commit", "-q", "-m", fmt.Sprintf("governed commit %d", g.commits))
	return g.git(who, "rev-parse", "HEAD")
}

// Host makes commit a hosted object by pushing it to a scratch ordinary
// branch from the author's clone, unless the server already has it.
func (g *serveGovernedFixture) Host(commit string) {
	g.t.Helper()
	if _, status := g.clients["A"].exec("", "git", "--git-dir="+g.hosted, "cat-file", "-e", commit+"^{commit}"); status == 0 {
		return
	}
	g.scratch++
	g.push(g.Author, "origin", fmt.Sprintf("%s:refs/heads/governed/scratch-%d", commit, g.scratch))
}

// act syncs who, runs one hn command that appends an event of kind to
// who's chain, publishes it and returns the event.
func (g *serveGovernedFixture) act(who, kind string, args ...string) operationalEvent {
	g.t.Helper()
	g.hn(who, "sync", "origin")
	g.hn(who, args...)
	event := g.latest(who)
	if event.Kind != kind {
		g.t.Fatalf("hn %s appended %s, want %s", strings.Join(args, " "), event.Kind, kind)
	}
	g.hn(who, "sync", "origin")
	return event
}

// OpenProposal opens a candidate for head on base in the author's clone and
// publishes it with its code ref. base becomes hosted first, because the
// server needs a candidate's base before it accepts the candidate.
func (g *serveGovernedFixture) OpenProposal(base, head string) operationalEvent {
	g.t.Helper()
	g.Host(base)
	g.proposals++
	event := g.act(g.Author, "proposal.open", "proposal", "open", "--base", base, "--head", head,
		"--body", "Opened by the governed fixture.", fmt.Sprintf("Governed proposal %d", g.proposals))
	if event.Base != base || event.Head != head {
		g.t.Fatalf("proposal %s binds %s..%s, want %s..%s", event.ID, event.Base, event.Head, base, head)
	}
	return event
}

// Revise revises candidate onto base and head in the author's clone.
func (g *serveGovernedFixture) Revise(candidate, base, head string) operationalEvent {
	g.t.Helper()
	g.Host(base)
	return g.act(g.Author, "proposal.revise", "proposal", "revise", candidate, "--base", base, "--head", head,
		"--body", "Revised by the governed fixture.")
}

// Approve records the reviewer's approval of candidate.
func (g *serveGovernedFixture) Approve(candidate string) operationalEvent {
	g.t.Helper()
	return g.act(g.Reviewer, "review.submit", "review", candidate, "--approve", "--body", "Approved by the governed fixture.")
}

// RequestCI records the maintainer's run request for pipeline test.
func (g *serveGovernedFixture) RequestCI(candidate string) operationalEvent {
	g.t.Helper()
	return g.act(g.Maintainer, "run.request", "run", "request", candidate, "test")
}

// RunCI executes request on the runner's host; extra adds flags such as
// --rerun.
func (g *serveGovernedFixture) RunCI(request string, extra ...string) operationalEvent {
	g.t.Helper()
	args := append([]string{"run", "execute", request, "--backend", "host", "--allow-unsafe-host-execution"}, extra...)
	return g.act(g.Runner, "run.result", args...)
}

// Accept records the maintainer's acceptance of candidate.
func (g *serveGovernedFixture) Accept(candidate string) operationalEvent {
	g.t.Helper()
	return g.act(g.Maintainer, "proposal.decision", "decide", candidate, "--accept", "--body", "Accepted by the governed fixture.")
}

// Reject records the maintainer's rejection of candidate.
func (g *serveGovernedFixture) Reject(candidate string) operationalEvent {
	g.t.Helper()
	return g.act(g.Maintainer, "proposal.decision", "decide", candidate, "--reject", "--body", "Rejected by the governed fixture.")
}

// serveGovernedProposal is the evidence ReadyProposal built.
type serveGovernedProposal struct {
	Candidate, Approval, Request, Result, Acceptance operationalEvent
}

// ReadyProposal opens a candidate for head on base and gathers one
// approval, one passing CI result and one acceptance for it.
func (g *serveGovernedFixture) ReadyProposal(base, head string) serveGovernedProposal {
	g.t.Helper()
	var p serveGovernedProposal
	p.Candidate = g.OpenProposal(base, head)
	p.Approval = g.Approve(p.Candidate.ID)
	p.Request = g.RequestCI(p.Candidate.ID)
	p.Result = g.RunCI(p.Request.ID)
	p.Acceptance = g.Accept(p.Candidate.ID)
	return p
}

// ServerTip is the hosted value of branch, a short name or a full ref, or
// "" when it does not exist.
func (g *serveGovernedFixture) ServerTip(branch string) string {
	g.t.Helper()
	if !strings.HasPrefix(branch, "refs/") {
		branch = "refs/heads/" + branch
	}
	return g.hostedRef(branch)
}

// ServerRefs is every hosted ref with its value, for byte-identical
// comparisons.
func (g *serveGovernedFixture) ServerRefs() string {
	g.t.Helper()
	return g.refs()
}

// serveGovernedPushArgs builds a forced push of commit to main. The forced
// refspec makes Git send a non-fast-forward update instead of refusing it
// on the client, so the server's own checks decide.
func serveGovernedPushArgs(commit string, pushOptions []string) []string {
	args := make([]string, 0, len(pushOptions)+2)
	for _, option := range pushOptions {
		args = append(args, "--push-option="+option)
	}
	return append(args, "origin", "+"+commit+":"+serveGovernedMain)
}

// PushMain pushes commit to main from who's clone and returns Git's output,
// the parsed rejections and whether the push succeeded.
func (g *serveGovernedFixture) PushMain(who, commit string, pushOptions ...string) (string, []serveRejection, bool) {
	g.t.Helper()
	output, status := g.clients[who].exec(g.work[who], "git", append([]string{"push"}, serveGovernedPushArgs(commit, pushOptions)...)...)
	rejections, _ := extractServeRejections(output)
	return output, rejections, status == 0
}

// RejectMain requires a push of commit to main to be refused with code and
// the hosted refs to stay byte-identical, and returns the one rejection.
func (g *serveGovernedFixture) RejectMain(who, commit, code string, pushOptions ...string) serveRejection {
	g.t.Helper()
	return g.reject(who, map[string]string{serveGovernedMain: code}, serveGovernedPushArgs(commit, pushOptions)...)[0]
}

// AcceptMain requires a push of commit to main to be accepted and main to
// equal commit afterwards.
func (g *serveGovernedFixture) AcceptMain(who, commit string, pushOptions ...string) {
	g.t.Helper()
	g.push(who, serveGovernedPushArgs(commit, pushOptions)...)
	if tip := g.ServerTip("main"); tip != commit {
		g.t.Fatalf("hosted main = %s after an accepted push, want %s", tip, commit)
	}
}

// SetMainForTest moves hosted main to commit with the administrator's
// lock-exec, bypassing governance. Use it only to arrange preconditions.
func (g *serveGovernedFixture) SetMainForTest(commit string) {
	g.t.Helper()
	g.Host(commit)
	current := g.ServerTip("main")
	g.h.Serve("serve", "repo", "lock-exec", "r", "--", "git", "update-ref", serveGovernedMain, commit, current)
	if tip := g.ServerTip("main"); tip != commit {
		g.t.Fatalf("hosted main = %s after lock-exec, want %s", tip, commit)
	}
}

// serveGovernedPipelineLine is one pipeline line of hn proposal status.
var serveGovernedPipelineLine = regexp.MustCompile(`(?m)^Pipeline: (\S+) (\d+)/(\d+) trusted passes`)

// VerifyAccepted is the SC-003 check: a fresh clone with no Hubnot
// identity selects every hosted actor and the candidate exactly, syncs,
// and must report the candidate accepted, with its signed head as code and
// every required pipeline passed. It returns hn proposal status output.
func (g *serveGovernedFixture) VerifyAccepted(candidate string) string {
	g.t.Helper()
	if g.verifier == nil {
		key := serveHarnessAddKey(g.h, serveHarnessActor("governed-verifier"))
		client := serveHarnessClient(g.t, g.h, key, nil)
		g.verifier = &client
	}
	dir := filepath.Join(g.t.TempDir(), "verifier")
	g.verifier.Git("", "clone", "-q", serveHarnessCloneURL("r"), dir)
	// Exact selection must name every hosted proposal: an actor chain
	// imports only when the code of each candidate it signs is selected.
	args := []string{"replication", "select", "origin"}
	for _, ref := range strings.Fields(serveHarnessGit(g.t, g.hosted, "for-each-ref", "--format=%(refname)", serveActorRefPrefix)) {
		args = append(args, "--actor", strings.TrimPrefix(ref, serveActorRefPrefix))
	}
	selected := false
	for _, ref := range strings.Fields(serveHarnessGit(g.t, g.hosted, "for-each-ref", "--format=%(refname)", serveProposalRefPrefix)) {
		id := "sha256:" + strings.TrimPrefix(ref, serveProposalRefPrefix)
		selected = selected || id == candidate
		args = append(args, "--proposal", id)
	}
	if !selected {
		g.t.Fatalf("candidate %s has no hosted code ref", candidate)
	}
	g.verifier.Hn(dir, args...)
	g.verifier.Hn(dir, "sync", "origin")
	status := g.verifier.Hn(dir, "proposal", "status", candidate)
	assertOperationalContains(g.t, status, "Status:   accepted", "Code:     ✓ signed head available and matched")
	pipelines := serveGovernedPipelineLine.FindAllStringSubmatch(status, -1)
	if len(pipelines) == 0 {
		g.t.Fatalf("verifier status lists no pipeline:\n%s", status)
	}
	for _, pipeline := range pipelines {
		passed, _ := strconv.Atoi(pipeline[2])
		required, _ := strconv.Atoi(pipeline[3])
		if passed < required {
			g.t.Fatalf("verifier reports pipeline %s with %d of %d trusted passes:\n%s", pipeline[1], passed, required, status)
		}
	}
	return status
}

// requireServeMissing checks the missing field of a rejection.
func requireServeMissing(t *testing.T, rejection serveRejection, want ...string) {
	t.Helper()
	if !slices.Equal(rejection.Missing, want) {
		t.Fatalf("%s %s missing = %q, want %q (sentence %q)", rejection.Ref, rejection.Code, rejection.Missing, want, rejection.Sentence)
	}
}

// requireServeSentence checks that a rejection's sentence contains part.
func requireServeSentence(t *testing.T, rejection serveRejection, part string) {
	t.Helper()
	if !strings.Contains(rejection.Sentence, part) {
		t.Fatalf("%s %s sentence %q does not contain %q", rejection.Ref, rejection.Code, rejection.Sentence, part)
	}
}

func requireServeNext(t *testing.T, rejection serveRejection, want string) {
	t.Helper()
	if rejection.Next != want {
		t.Fatalf("%s %s next = %q, want %q", rejection.Ref, rejection.Code, rejection.Next, want)
	}
}

// serveGovernedDirectMain makes A's local main one commit ahead of hosted
// main with no proposal for it and returns that commit.
func serveGovernedDirectMain(g *serveGovernedFixture) string {
	g.t.Helper()
	head := g.Commit(g.Base, nil)
	g.git("A", "checkout", "-q", "-B", "main", head)
	return head
}

// requireServeRelayedLines compares the hn-serve lines Git relayed with an
// expected pair after replacing illustrative values with fixture values,
// line by line and field by field.
func requireServeRelayedLines(t *testing.T, relayed, expected []string) {
	t.Helper()
	if !reflect.DeepEqual(relayed, expected) {
		t.Fatalf("relayed lines:\n%s\nwant:\n%s", strings.Join(relayed, "\n"), strings.Join(expected, "\n"))
	}
	got, err := parseServeRejectionLine(relayed[0])
	if err != nil {
		t.Fatal(err)
	}
	want, err := parseServeRejectionLine(expected[0])
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fields %+v, want %+v", got, want)
	}
}

// serveExampleReplacer replaces an illustrative actor and commit with
// fixture values: the full commit everywhere and its eight-digit form in
// the sentence.
func serveExampleReplacer(actor, commit, fixtureActor, fixtureCommit string) func(string) string {
	return func(line string) string {
		line = strings.ReplaceAll(line, actor, fixtureActor)
		line = strings.ReplaceAll(line, commit, fixtureCommit)
		return strings.ReplaceAll(line, commit[:8], fixtureCommit[:8])
	}
}

// US1-2: a direct push of main one commit ahead with no proposal is
// refused with every evidence kind missing, field for field as the
// contract's reject-direct-main example. A feature branch in the same push
// does not move.
func TestServeProtectedRejectsDirectPushWithoutProposal(t *testing.T) {
	g := newServeGovernedFixture(t, serveGovernedOptions{})
	head := serveGovernedDirectMain(g)
	output := g.clients["A"].GitFailure(g.work["A"], "push", "origin", "main")
	_, lines := extractServeRejections(output)
	replace := serveExampleReplacer(serveTestActor, serveTestCommit, g.actors["A"], head)
	var want []string
	for _, line := range strings.Split(strings.TrimSuffix(serveGoldenRejectDirectMain, "\n"), "\n") {
		want = append(want, replace(line))
	}
	requireServeRelayedLines(t, lines, want)
	if tip := g.ServerTip("main"); tip != g.Base {
		t.Fatalf("hosted main moved to %s", tip)
	}

	rejection := g.reject("A", map[string]string{serveGovernedMain: serveReasonProtectedNoProposal},
		"origin", "main", "HEAD:refs/heads/feature/a")[0]
	requireServeIDs(t, rejection, head)
	requireServeMissing(t, rejection, serveEvidenceKinds...)
	if got := g.ServerTip("feature/a"); got != "" {
		t.Fatalf("feature/a exists at %s after the push was rejected", got)
	}
	entries := g.pushLog()
	if last := entries[len(entries)-1]; last.Verdict != servePushVerdictRejected || !slices.Equal(last.Codes, []string{serveReasonProtectedNoProposal}) {
		t.Fatalf("push log entry %+v, want one protected-no-proposal rejection", last)
	}
}

// T060: the guide's reject-direct-main example is the real output, field
// for field, after replacing its illustrative IDs with fixture IDs.
func TestServeProtectedDocumentedExample(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("docs", "serve-v0.md"))
	if err != nil {
		t.Fatal(err)
	}
	const marker = "<!-- example: reject-direct-main -->\n"
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
	if documentedRejection.Code != serveReasonProtectedNoProposal || len(documentedRejection.IDs) != 1 {
		t.Fatalf("documented rejection %+v, want protected-no-proposal naming one commit", documentedRejection)
	}

	g := newServeGovernedFixture(t, serveGovernedOptions{})
	head := serveGovernedDirectMain(g)
	output := g.clients["A"].GitFailure(g.work["A"], "push", "origin", "main")
	_, lines := extractServeRejections(output)
	replace := serveExampleReplacer(documentedRejection.Actor, documentedRejection.IDs[0], g.actors["A"], head)
	want := make([]string, 0, 2)
	for _, line := range documented {
		if !strings.HasPrefix(line, "remote: ") {
			t.Fatalf("documented line %q does not show Git's remote: prefix", line)
		}
		want = append(want, serveCleanStderrLine(replace(line)))
	}
	requireServeRelayedLines(t, lines, want)
}

// A ready, accepted, CI-tested candidate head pushed to main by a mapped
// actor who is not a maintainer, without push options, is accepted. The
// server creates no commit: main is the head itself.
func TestServeProtectedAcceptsGovernedDirectPush(t *testing.T) {
	g := newServeGovernedFixture(t, serveGovernedOptions{})
	head := g.Commit(g.Base, nil)
	p := g.ReadyProposal(g.Base, head)
	commits := serveHarnessGit(t, g.hosted, "rev-list", "--count", head)
	g.push("A", "origin", head+":"+serveGovernedMain)
	if tip := g.ServerTip("main"); tip != head {
		t.Fatalf("hosted main = %s, want the candidate head %s", tip, head)
	}
	if got := serveHarnessGit(t, g.hosted, "rev-list", "--count", serveGovernedMain); got != commits {
		t.Fatalf("main reaches %s commits and the head %s; the server created a commit", got, commits)
	}
	entries := g.pushLog()
	if last := entries[len(entries)-1]; last.Verdict != servePushVerdictAccepted || last.Actor != g.actors["A"] {
		t.Fatalf("push log entry %+v, want A's accepted push", last)
	}
	g.VerifyAccepted(p.Candidate.ID)
}

// US3-5: missing trusted CI is refused under the base policy, both before
// any result and when the runner's latest result failed after acceptance.
func TestServeProtectedNotReady(t *testing.T) {
	g := newServeGovernedFixture(t, serveGovernedOptions{})

	approved := g.Commit(g.Base, nil)
	candidate := g.OpenProposal(g.Base, approved)
	g.Approve(candidate.ID)
	rejection := g.RejectMain("A", approved, serveReasonProtectedNotReady)
	requireServeIDs(t, rejection, candidate.ID)
	requireServeMissing(t, rejection, serveEvidenceCI, serveEvidenceAcceptance)
	requireServeNext(t, rejection, "run hn proposal status "+candidate.ID)
	requireServeSentence(t, rejection, "base policy requires 1 passing result of pipeline test; 0 trusted passing results found")
	requireServeSentence(t, rejection, "base policy requires 1 acceptance; 0 current acceptances found")

	// Accepted on a passing result, then the same runner's rerun fails:
	// only CI is missing.
	head := g.Commit(g.Base, nil)
	p := g.ReadyProposal(g.Base, head)
	writeServeTestFile(t, g.CIFailFlag, "fail\n", 0o644)
	failed := g.RunCI(p.Request.ID, "--rerun")
	rejection = g.RejectMain("A", head, serveReasonProtectedNotReady)
	requireServeIDs(t, rejection, p.Candidate.ID)
	requireServeMissing(t, rejection, serveEvidenceCI)
	requireServeSentence(t, rejection, "base policy requires 1 passing result of pipeline test; 0 trusted passing results found")
	if failed.Subject != p.Request.ID {
		t.Fatalf("failed result %+v does not answer request %s", failed, p.Request.ID)
	}

	// A passing rerun restores the evidence.
	if err := os.Remove(g.CIFailFlag); err != nil {
		t.Fatal(err)
	}
	g.RunCI(p.Request.ID, "--rerun")
	g.AcceptMain("A", head)
	g.VerifyAccepted(p.Candidate.ID)
}

// US3-4 and C-004: the destination policy at the tip being replaced
// governs too. Each case commits a policy change Y on the base X, builds a
// candidate with base X and a head H on top of Y, so the base policy is the
// one at X, and sets main to Y with the administrator shortcut. Pushing H
// is then a fast-forward whose destination policy is the one at Y.
func TestServeProtectedDestinationPolicy(t *testing.T) {
	g := newServeGovernedFixture(t, serveGovernedOptions{})
	reviewer, maintainer, runner := g.actors[g.Reviewer], g.actors[g.Maintainer], g.actors[g.Runner]
	arrange := func(policy serveGovernedPolicy) (string, string, serveGovernedProposal) {
		t.Helper()
		y := g.Commit(g.Base, map[string]string{serveGovernedPolicyPath: policy.JSON(t)})
		head := g.Commit(y, nil)
		p := g.ReadyProposal(g.Base, head)
		g.SetMainForTest(y)
		return y, head, p
	}

	// Two approvals at the destination, one at the base.
	twoApprovals := g.Policy
	twoApprovals.Reviewers = []string{reviewer, maintainer}
	twoApprovals.RequiredApprovals = 2
	twoY, twoHead, two := arrange(twoApprovals)
	rejection := g.RejectMain("A", twoHead, serveReasonProtectedDestinationPolicy)
	requireServeIDs(t, rejection, two.Candidate.ID)
	requireServeMissing(t, rejection, serveEvidenceReview)
	requireServeSentence(t, rejection, "destination policy requires 2 approvals; 1 trusted approval found")
	requireServeNext(t, rejection, serveReasonDefaultNext(serveReasonProtectedDestinationPolicy))

	// The destination no longer trusts R, so R's approval does not count
	// there, although the base policy still lists R.
	removed := g.Policy
	removed.Reviewers = []string{maintainer}
	_, removedHead, removedProposal := arrange(removed)
	rejection = g.RejectMain("A", removedHead, serveReasonProtectedDestinationPolicy)
	requireServeIDs(t, rejection, removedProposal.Candidate.ID)
	requireServeMissing(t, rejection, serveEvidenceReview)
	requireServeSentence(t, rejection, "destination policy requires 1 approval; 0 trusted approvals found")

	// A pipeline only the destination requires has no definition at H.
	lint := g.Policy
	lint.Pipelines = map[string][]string{"test": {runner}, "lint": {runner}}
	_, lintHead, lintProposal := arrange(lint)
	rejection = g.RejectMain("A", lintHead, serveReasonProtectedDestinationPolicy)
	requireServeIDs(t, rejection, lintProposal.Candidate.ID)
	requireServeMissing(t, rejection, serveEvidenceCI)
	requireServeSentence(t, rejection, "destination policy requires pipeline lint, which has no valid definition at "+lintHead)

	// A second approval from M, a reviewer only the destination trusts,
	// meets the two-approval destination. The base acceptance still counts
	// because decisions are matched by the base policy digest.
	g.hn(g.Maintainer, "sync", "origin")
	g.hn(g.Maintainer, "review", two.Candidate.ID, "--approve", "--body", "Second approval for the destination policy.")
	g.hn(g.Maintainer, "sync", "origin")
	g.SetMainForTest(twoY)
	g.AcceptMain("A", twoHead)
	g.VerifyAccepted(two.Candidate.ID)
}

// Step 9: a superseded candidate, a rejected candidate and an already
// merged candidate are each protected-blocked, naming the blocking event.
func TestServeProtectedBlocked(t *testing.T) {
	g := newServeGovernedFixture(t, serveGovernedOptions{})

	supersededHead := g.Commit(g.Base, nil)
	superseded := g.ReadyProposal(g.Base, supersededHead)
	revision := g.Revise(superseded.Candidate.ID, g.Base, g.Commit(g.Base, nil))
	rejection := g.RejectMain("A", supersededHead, serveReasonProtectedBlocked)
	requireServeIDs(t, rejection, superseded.Candidate.ID, revision.ID)
	requireServeNext(t, rejection, "run hn proposal status "+superseded.Candidate.ID)
	requireServeSentence(t, rejection, "is superseded by "+revision.ID)

	rejectedHead := g.Commit(g.Base, nil)
	rejected := g.OpenProposal(g.Base, rejectedHead)
	decision := g.Reject(rejected.ID)
	rejection = g.RejectMain("A", rejectedHead, serveReasonProtectedBlocked)
	requireServeIDs(t, rejection, rejected.ID, decision.ID)
	requireServeSentence(t, rejection, "has a current maintainer rejection "+decision.ID)

	// M merges the candidate locally with hn merge and publishes the merge
	// fact; main on the server stays at the base.
	mergedHead := g.Commit(g.Base, nil)
	merged := g.ReadyProposal(g.Base, mergedHead)
	g.hn(g.Maintainer, "sync", "origin")
	g.hn(g.Maintainer, "merge", merged.Candidate.ID)
	merge := g.latest(g.Maintainer)
	if merge.Kind != "proposal.merged" {
		t.Fatalf("hn merge appended %s", merge.Kind)
	}
	g.hn(g.Maintainer, "sync", "origin")
	rejection = g.RejectMain("A", mergedHead, serveReasonProtectedBlocked)
	requireServeIDs(t, rejection, merged.Candidate.ID, merge.ID)
	requireServeSentence(t, rejection, "is already merged by "+merge.ID)
	if tip := g.ServerTip("main"); tip != g.Base {
		t.Fatalf("hosted main moved to %s", tip)
	}
}

// serveGovernedSignProposal signs proposal.open for base..head with the
// author's identity in the author's clone, creates its code ref and pushes
// the actor chain, the head on a scratch branch and the code ref.
func serveGovernedSignProposal(g *serveGovernedFixture, base, head string) operationalEvent {
	g.t.Helper()
	g.Host(base)
	g.t.Chdir(g.work[g.Author])
	identity, err := loadIdentity()
	if err != nil {
		g.t.Fatal(err)
	}
	event, err := nextEvent(identity, "proposal.open")
	if err != nil {
		g.t.Fatal(err)
	}
	event.Title, event.Base, event.Head = "Signed in-process", base, head
	stored, err := appendEvent(event, identity)
	if err != nil {
		g.t.Fatal(err)
	}
	if err := createProposalRef(stored.ID, head); err != nil {
		g.t.Fatal(err)
	}
	actor, code := g.actorRef(g.Author), proposalRef(stored.ID)
	g.push(g.Author, "origin", actor+":"+actor, code+":"+code)
	candidate := g.latest(g.Author)
	if candidate.ID != stored.ID {
		g.t.Fatalf("latest event %s, want %s", candidate.ID, stored.ID)
	}
	return candidate
}

// OQ-11: a tip without a valid policy refuses every update, a candidate
// base without a policy is refused, and the documented administrator
// recovery repairs the branch.
func TestServeProtectedPolicyInvalid(t *testing.T) {
	g := newServeGovernedFixture(t, serveGovernedOptions{})

	// A base commit without a policy, under a valid destination. hn
	// proposal open refuses such a base, so the candidate is signed
	// in-process with the author's identity; the server accepts it as a
	// fact because a policy is read only when the candidate is evaluated.
	unpolicied := g.Commit(g.Base, map[string]string{serveGovernedPolicyPath: serveGovernedRemove})
	head := g.Commit(unpolicied, nil)
	candidate := serveGovernedSignProposal(g, unpolicied, head)
	rejection := g.RejectMain("A", head, serveReasonProtectedPolicyInvalid)
	requireServeIDs(t, rejection, candidate.ID, unpolicied)
	requireServeNext(t, rejection, "ask the administrator; see recovery in the serve guide")

	for _, tc := range []struct {
		name  string
		files map[string]string
	}{
		{"malformed", map[string]string{serveGovernedPolicyPath: "{not json\n"}},
		{"missing", map[string]string{serveGovernedPolicyPath: serveGovernedRemove}},
	} {
		broken := g.Commit(g.Base, tc.files)
		g.SetMainForTest(broken)
		next := g.Commit(broken, nil)
		// M pushes the author's commit too, so it fetches it first.
		g.Host(next)
		g.git("M", "fetch", "-q", "origin")
		for _, who := range []string{"A", "M"} {
			rejection := g.RejectMain(who, next, serveReasonProtectedPolicyInvalid)
			requireServeIDs(t, rejection, broken)
			requireServeSentence(t, rejection, "which has no valid .hn/policy.json")
		}

		// Recovery: the administrator moves main to a repaired commit that
		// descends from the broken tip, naming the tip as the old value.
		repaired := g.Commit(broken, map[string]string{serveGovernedPolicyPath: g.Policy.JSON(t)})
		g.Host(repaired)
		stale := g.Base
		if output, status := g.h.Exec(serveHarnessRun{}, "serve", "repo", "lock-exec", "r", "--", "git", "update-ref", serveGovernedMain, repaired, stale); status == 0 {
			t.Fatalf("%s: update-ref with a stale old value succeeded:\n%s", tc.name, output)
		}
		g.h.Serve("serve", "repo", "lock-exec", "r", "--", "git", "update-ref", serveGovernedMain, repaired, broken)
		if tip := g.ServerTip("main"); tip != repaired {
			t.Fatalf("%s: hosted main = %s after recovery, want %s", tc.name, tip, repaired)
		}
		// Governed updates work again.
		fixed := g.Commit(repaired, nil)
		g.RejectMain("A", fixed, serveReasonProtectedNoProposal)
		g.SetMainForTest(g.Base)
	}
}

// OQ-11 and step 1: a push never creates or deletes a protected branch,
// whoever pushes.
func TestServeProtectedCreationAndDeletion(t *testing.T) {
	g := newServeGovernedFixture(t, serveGovernedOptions{})
	for _, who := range []string{"A", "M"} {
		rejection := g.reject(who, map[string]string{serveGovernedMain: serveReasonProtectedDeletion}, "origin", ":"+serveGovernedMain)[0]
		requireServeIDs(t, rejection, g.Base)
		requireServeNext(t, rejection, "protected branches cannot be deleted")
	}
	g.h.Serve("serve", "repo", "lock-exec", "r", "--", "git", "update-ref", "-d", serveGovernedMain, g.Base)
	for _, who := range []string{"A", "M"} {
		rejection := g.reject(who, map[string]string{serveGovernedMain: serveReasonProtectedPolicyInvalid}, "origin", g.Base+":"+serveGovernedMain)[0]
		requireServeIDs(t, rejection, g.Base)
		requireServeSentence(t, rejection, "a push cannot create a protected branch")
	}
	if tip := g.ServerTip("main"); tip != "" {
		t.Fatalf("main was created at %s", tip)
	}
}

// Steps 3 and 5: a head that does not descend from the tip is
// protected-not-fast-forward naming the tip, and an expected tip that is
// stale is protected-stale-tip before anything else is read, as the
// contract's reject-stale-advance example shows.
func TestServeProtectedNotFastForwardAndStaleTip(t *testing.T) {
	g := newServeGovernedFixture(t, serveGovernedOptions{})
	head := g.Commit(g.Base, nil)
	p := g.ReadyProposal(g.Base, head)
	other := g.Commit(g.Base, nil)
	g.SetMainForTest(other)

	rejection := g.RejectMain("A", head, serveReasonProtectedNotFastForward)
	requireServeIDs(t, rejection, other, head)
	requireServeNext(t, rejection, "revise the proposal onto "+other+" with hn proposal revise")

	m := g.actors["M"]
	output := g.clients["M"].GitFailure(g.work["M"], append([]string{"push"},
		serveGovernedPushArgs(head, []string{"hn-advance=" + p.Candidate.ID, "hn-expected-tip=" + g.Base})...)...)
	_, lines := extractServeRejections(output)
	requireServeRelayedLines(t, lines, []string{
		"hn-serve/1 reject ref=refs/heads/main actor=" + m + " code=protected-stale-tip ids=" + g.Base + "," + other + ` next="fetch, then retry or revise"`,
		"hn-serve: refs/heads/main moved to " + other + " after this request read " + g.Base + ".",
	})
	if tip := g.ServerTip("main"); tip != other {
		t.Fatalf("hosted main moved to %s", tip)
	}
	// A stale expected tip wins over every later step, even for a commit
	// that has no candidate at all.
	g.RejectMain("A", g.Commit(other, nil), serveReasonProtectedStaleTip, "hn-expected-tip="+g.Base)
}

// Step 8 and the code-ref ordering: a candidate whose code ref was never
// pushed is proposal-ref-mismatch with missing=proposal; the same update
// with the code ref in the same push is accepted.
func TestServeProtectedCodeRefInSamePush(t *testing.T) {
	g := newServeGovernedFixture(t, serveGovernedOptions{Solo: true})
	head := g.Commit(g.Base, nil)
	// M builds the whole candidate locally and publishes the head commit on
	// an ordinary branch and its actor chain, but not the code ref.
	g.hn("M", "proposal", "open", "--base", g.Base, "--head", head, "Unpublished code")
	candidate := g.latest("M")
	g.hn("M", "review", candidate.ID, "--approve", "--body", "Solo approval.")
	g.hn("M", "run", "request", candidate.ID, "test")
	request := g.latest("M")
	g.hn("M", "run", "execute", request.ID, "--backend", "host", "--allow-unsafe-host-execution")
	g.hn("M", "decide", candidate.ID, "--accept", "--body", "Solo acceptance.")
	actor := g.actorRef("M")
	g.push("M", "origin", head+":refs/heads/feature/solo", actor+":"+actor)
	code := proposalRef(candidate.ID)
	if got := g.ServerTip(code); got != "" {
		t.Fatalf("code ref hosted at %s before it was pushed", got)
	}

	rejection := g.RejectMain("M", head, serveReasonProposalRefMismatch)
	requireServeIDs(t, rejection, candidate.ID)
	requireServeMissing(t, rejection, serveEvidenceProposal)
	requireServeSentence(t, rejection, "has no hosted code ref "+code)

	g.push("M", "origin", head+":"+serveGovernedMain, code+":"+code)
	if g.ServerTip("main") != head || g.ServerTip(code) != head {
		t.Fatal("the push carrying the code ref and the main update was not applied")
	}
	g.VerifyAccepted(candidate.ID)
}

// T057: a protected update sees facts accepted from actor refs earlier in
// the same push. M's acceptance alone is missing; pushed atomically with
// the update, it counts.
func TestServeProtectedUsesFactsFromTheSamePush(t *testing.T) {
	g := newServeGovernedFixture(t, serveGovernedOptions{})
	head := g.Commit(g.Base, nil)
	candidate := g.OpenProposal(g.Base, head)
	g.Approve(candidate.ID)
	g.RunCI(g.RequestCI(candidate.ID).ID)
	g.hn("M", "sync", "origin")
	g.hn("M", "decide", candidate.ID, "--accept", "--body", "Accepted in the same push.")

	rejection := g.RejectMain("M", head, serveReasonProtectedNotReady)
	requireServeMissing(t, rejection, serveEvidenceAcceptance)

	actor := g.actorRef("M")
	g.push("M", "--atomic", "origin", actor+":"+actor, head+":"+serveGovernedMain)
	if g.ServerTip("main") != head || g.ServerTip(actor) != g.localHead("M", actor) {
		t.Fatal("the push carrying the acceptance and the main update was not applied")
	}
	g.VerifyAccepted(candidate.ID)
}

// Steps 6 and 12 with hn-advance: the named candidate must have head H,
// and the requester must be a maintainer under both policies.
func TestServeProtectedAdvanceNeedsMaintainer(t *testing.T) {
	g := newServeGovernedFixture(t, serveGovernedOptions{})
	head := g.Commit(g.Base, nil)
	p := g.ReadyProposal(g.Base, head)
	other := g.OpenProposal(g.Base, g.Commit(g.Base, nil))

	rejection := g.RejectMain("M", head, serveReasonProtectedNoProposal, "hn-advance="+other.ID)
	requireServeIDs(t, rejection, head, other.ID)
	requireServeMissing(t, rejection, serveEvidenceKinds...)

	rejection = g.RejectMain("A", head, serveReasonAdvanceNotMaintainer, "hn-advance="+p.Candidate.ID)
	requireServeIDs(t, rejection, p.Candidate.ID)
	requireServeNext(t, rejection, "ask a maintainer to run hn merge --remote")

	g.AcceptMain("M", head, "hn-advance="+p.Candidate.ID, "hn-expected-tip="+g.Base)
	g.VerifyAccepted(p.Candidate.ID)
}

// The multi-candidate rule: any passing candidate with head H admits the
// update; otherwise the latest failing step is reported, ties going to the
// lowest event ID, and ids lists every candidate.
func TestServeProtectedMultipleCandidates(t *testing.T) {
	g := newServeGovernedFixture(t, serveGovernedOptions{})
	head := g.Commit(g.Base, nil)
	rejected := g.OpenProposal(g.Base, head)
	g.Reject(rejected.ID)
	accepted := g.OpenProposal(g.Base, head)
	g.Approve(accepted.ID)
	g.RunCI(g.RequestCI(accepted.ID).ID)
	g.Accept(accepted.ID)
	g.AcceptMain("A", head)
	g.VerifyAccepted(accepted.ID)

	// Both fail: the rejected one at step 9, the unreviewed one at step 10.
	next := g.Commit(head, nil)
	blocked := g.OpenProposal(head, next)
	g.Reject(blocked.ID)
	unreviewed := g.OpenProposal(head, next)
	rejection := g.RejectMain("A", next, serveReasonProtectedNotReady)
	both := []string{blocked.ID, unreviewed.ID}
	slices.Sort(both)
	requireServeIDs(t, rejection, both...)
	requireServeNext(t, rejection, "run hn proposal status "+unreviewed.ID)

	// Both fail at step 10: the lower event ID is reported.
	third := g.Commit(head, nil)
	first := g.OpenProposal(head, third)
	second := g.OpenProposal(head, third)
	pair := []string{first.ID, second.ID}
	slices.Sort(pair)
	rejection = g.RejectMain("A", third, serveReasonProtectedNotReady)
	requireServeIDs(t, rejection, pair...)
	requireServeNext(t, rejection, "run hn proposal status "+pair[0])
}

// Unit tests.

func TestServeProtectedUnmetClauses(t *testing.T) {
	evaluation := &ProposalEvaluation{
		Policy:    PolicyDocument{Proposals: ProposalPolicy{RequiredApprovals: 2, RequiredAccepts: 1}},
		Approvals: []StoredEvent{{ID: "sha256:" + strings.Repeat("1", 64)}},
		Pipelines: []PipelineEvaluation{
			{Name: "ok", Required: 1, Passed: []StoredEvent{{}}},
			{Name: "test", Required: 2},
		},
		Rejected: true,
	}
	missing, details := serveProtectedUnmet("destination policy", evaluation, nil, "h")
	if want := []string{serveEvidenceReview, serveEvidenceCI, serveEvidenceAcceptance}; !slices.Equal(missing, want) {
		t.Fatalf("missing %q, want %q", missing, want)
	}
	want := "destination policy requires 2 approvals; 1 trusted approval found; " +
		"destination policy requires 2 passing results of pipeline test; 0 trusted passing results found; " +
		"destination policy requires 1 acceptance; 0 current acceptances found and a current maintainer rejection"
	if details != want {
		t.Fatalf("details\n%q\nwant\n%q", details, want)
	}

	definition := &pipelineDefinitionError{Pipeline: "lint"}
	evaluation = &ProposalEvaluation{Policy: PolicyDocument{Proposals: ProposalPolicy{RequiredAccepts: 1}}, Accepted: true,
		Pipelines: []PipelineEvaluation{{Name: "test", Required: 1}}}
	missing, details = serveProtectedUnmet("base policy", evaluation, definition, "abc")
	if !slices.Equal(missing, []string{serveEvidenceCI}) || details != "base policy requires pipeline lint, which has no valid definition at abc" {
		t.Fatalf("missing %q details %q", missing, details)
	}
}

func TestServeProtectedIDsListCandidatesFirst(t *testing.T) {
	got := serveProtectedIDs([]string{"sha256:a", "sha256:b"}, []string{"", "sha256:b", "c"})
	if want := []string{"sha256:a", "sha256:b", "c"}; !slices.Equal(got, want) {
		t.Fatalf("ids %q, want %q", got, want)
	}
}
