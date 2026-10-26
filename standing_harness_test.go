package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// Standing acceptance test harness (WP06, C-002).
//
// newStandingHarness gives every role of the spec its own clone with its
// own hn identity, all sharing one real bare remote "origin". Every helper
// drives the built hn binary and git in the role's clone; only the two fact
// signers, SignFact and SignRaw, write signed events in-process, and the CLI
// then judges what they wrote.
//
// Roles:
//
//	M  maintainer
//	A  author
//	R  trusted reviewer counted by rule ordinary ("gpt-reviewer")
//	S  trusted reviewer not counted by rule ordinary ("sonnet-reviewer")
//	T  trusted runner of pipeline test
//	E  executing actor of rule ordinary, not a maintainer
//	X  executing actor of rule other only
//
// Helpers that act sync the role's clone before acting and publish after
// acting, so every role sees the others' facts. Facts signed in-process are
// never published by the harness: before the admission WPs land, the
// clones of other roles would refuse to import them.
//
// In-process signing changes the working directory of the test process, so
// no harness test may call t.Parallel.

// standingHarnessRemove as a Commit file content deletes the path.
const standingHarnessRemove = "\x00remove"

// standingHarnessPipeline is the one pipeline every harness policy requires.
const standingHarnessPipeline = "test"

// standingHarnessCommandTimeout bounds one hn command. It is longer than the
// 20 seconds of runOperationalCommand because a harness sync imports every
// role's chain and the full race suite runs under load.
const standingHarnessCommandTimeout = 90 * time.Second

// Expected outcomes of a defect fixture other than a check name.
const (
	// standingHarnessMaintainerPath: no rule claim, so the refusal is the
	// existing maintainer-path one, "merge <id> is not authorized by ...".
	standingHarnessMaintainerPath = "maintainer-path"
	// standingHarnessMissingDependency: an input of the fact is absent.
	standingHarnessMissingDependency = "missing dependency"
)

// Verdicts of the legacy and boundary fixtures.
const (
	standingHarnessValid               = "valid"
	standingHarnessValidRuleMerge      = "valid rule merge"
	standingHarnessValidMaintainer     = "valid maintainer merge"
	standingHarnessAnnotatedMaintainer = "valid maintainer merge with unverified annotation"
	standingHarnessRefused             = "refused"
	standingHarnessRefusedAsBefore     = "refused as before"
)

// standingHarnessUnknownRuleField is the existing strict-resource refusal of
// a resource event that carries a rule key (L06).
const standingHarnessUnknownRuleField = `invalid resource event JSON: unknown field "rule"`

var standingHarnessRoles = []string{"M", "A", "R", "S", "T", "E", "X"}

var standingHarnessRoleNames = map[string]string{
	"M": "maintainer",
	"A": "author",
	"R": "gpt-reviewer",
	"S": "sonnet-reviewer",
	"T": "runner",
	"E": "integrator",
	"X": "other-integrator",
}

type standingHarness struct {
	t       *testing.T
	binary  string
	remote  string            // bare repository path, the "origin" of every clone
	work    map[string]string // role -> clone path
	actors  map[string]string // role -> full actor fingerprint
	base    string            // main after the seed policy commit
	commits int

	policy    standingHarnessPolicy          // policy at base
	ciFlag    string                         // pipeline test fails while this file exists
	proposals int                            // proposals opened, for unique titles
	scratch   map[string]string              // commit -> scratch branch on the remote
	cases     map[string]standingHarnessCase // candidate ID -> evidence the harness gathered
	lastReady string                         // candidate of the latest ReadyCandidate
}

// standingHarnessRule is one automerge rule by role.
type standingHarnessRule struct {
	Name, TargetBranch string
	Acceptance         *string  // nil leaves the field out
	Actors, Authors    []string // roles, resolved to fingerprints
	Reviewers          []string // roles; nil leaves the field out
	Paths              []string
}

// standingHarnessPolicy is an hn.policy/0 document by role. requiredAccepts
// is always 1 and pipeline test always needs one passing result.
type standingHarnessPolicy struct {
	Maintainers, TrustedReviewers []string // roles
	RequiredApprovals             int
	AllowAuthorApproval           bool
	Runners, Requesters           []string // roles for pipeline "test"; Requesters nil leaves it out
	ProtectedPaths                []string // nil leaves protectedChecks out; M is the designated reviewer
	Rules                         []standingHarnessRule
}

func standingHarnessString(value string) *string { return &value }

// standingHarnessDefaultPolicy is the policy of User Story 1: rule ordinary
// lets E merge A's changes under src/, tests/ and docs/ on R's approval and
// T's passing result; rule other lets X merge A's changes under other/.
func standingHarnessDefaultPolicy() standingHarnessPolicy {
	return standingHarnessPolicy{
		Maintainers:       []string{"M"},
		TrustedReviewers:  []string{"R", "S"},
		RequiredApprovals: 1,
		Runners:           []string{"T"},
		Rules: []standingHarnessRule{
			{
				Name: "ordinary", TargetBranch: "main", Acceptance: standingHarnessString(automergeAcceptanceRule),
				Actors: []string{"E"}, Authors: []string{"A"}, Reviewers: []string{"R"},
				Paths: []string{"src/", "tests/", "docs/"},
			},
			{
				Name: "other", TargetBranch: "main", Acceptance: standingHarnessString(automergeAcceptanceRule),
				Actors: []string{"X"}, Authors: []string{"A"}, Paths: []string{"other/"},
			},
		},
	}
}

// standingHarnessSeedPolicy is the default policy without automerge, the
// starting point of an adoption through a maintainer amendment.
func standingHarnessSeedPolicy() standingHarnessPolicy {
	policy := standingHarnessDefaultPolicy()
	policy.Rules = nil
	return policy
}

// standingHarnessLegacyPolicy has maintainers-mode rules only and none of
// the fields this mission adds, so the older client of WP17 reads it.
func standingHarnessLegacyPolicy() standingHarnessPolicy {
	return standingHarnessPolicy{
		Maintainers:       []string{"M"},
		TrustedReviewers:  []string{"R", "S"},
		RequiredApprovals: 1,
		Runners:           []string{"T"},
		Rules: []standingHarnessRule{{
			Name: "maint", TargetBranch: "main", Actors: []string{"M"}, Authors: []string{"A"},
			Paths: []string{"src/", "tests/", "docs/"},
		}},
	}
}

// clone returns a deep copy, so a variant never changes its source.
func (p standingHarnessPolicy) clone() standingHarnessPolicy {
	copied := p
	copyRoles := func(roles []string) []string {
		if roles == nil {
			return nil
		}
		return append([]string{}, roles...)
	}
	copied.Maintainers = copyRoles(p.Maintainers)
	copied.TrustedReviewers = copyRoles(p.TrustedReviewers)
	copied.Runners = copyRoles(p.Runners)
	copied.Requesters = copyRoles(p.Requesters)
	copied.ProtectedPaths = copyRoles(p.ProtectedPaths)
	copied.Rules = nil
	for _, rule := range p.Rules {
		r := rule
		if rule.Acceptance != nil {
			r.Acceptance = standingHarnessString(*rule.Acceptance)
		}
		r.Actors, r.Authors = copyRoles(rule.Actors), copyRoles(rule.Authors)
		r.Reviewers, r.Paths = copyRoles(rule.Reviewers), copyRoles(rule.Paths)
		copied.Rules = append(copied.Rules, r)
	}
	return copied
}

// JSON encodes the policy the way a repository stores it, with every role
// resolved to its fingerprint. It encodes maps, so absent fields stay
// absent.
func (p standingHarnessPolicy) JSON(actors map[string]string) string {
	resolve := func(roles []string) []string {
		resolved := make([]string, 0, len(roles))
		for _, role := range roles {
			actor, exists := actors[role]
			if !exists {
				panic(fmt.Sprintf("standing harness policy names unknown role %q", role))
			}
			resolved = append(resolved, actor)
		}
		return resolved
	}
	pipeline := map[string]any{"requiredResults": 1, "trustedRunners": resolve(p.Runners)}
	if p.Requesters != nil {
		pipeline["requesters"] = resolve(p.Requesters)
	}
	document := map[string]any{
		"version":     policyVersion,
		"maintainers": resolve(p.Maintainers),
		"proposals": map[string]any{
			"requiredApprovals":   p.RequiredApprovals,
			"requiredAccepts":     1,
			"trustedReviewers":    resolve(p.TrustedReviewers),
			"allowAuthorApproval": p.AllowAuthorApproval,
		},
		"pipelines": map[string]any{standingHarnessPipeline: pipeline},
	}
	if p.ProtectedPaths != nil {
		document["protectedChecks"] = map[string]any{
			"paths": p.ProtectedPaths, "trustedReviewers": resolve([]string{"M"}), "requiredApprovals": 1,
		}
	}
	if len(p.Rules) > 0 {
		rules := make([]map[string]any, 0, len(p.Rules))
		for _, r := range p.Rules {
			branch := r.TargetBranch
			if branch == "" {
				branch = "main"
			}
			rule := map[string]any{
				"name": r.Name, "targetBranch": branch, "actors": resolve(r.Actors),
				"authors": resolve(r.Authors), "paths": r.Paths,
			}
			if r.Acceptance != nil {
				rule["acceptance"] = *r.Acceptance
			}
			if r.Reviewers != nil {
				rule["reviewers"] = resolve(r.Reviewers)
			}
			rules = append(rules, rule)
		}
		document["automerge"] = map[string]any{"rules": rules}
	}
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		panic(err)
	}
	return string(encoded) + "\n"
}

// newStandingHarness builds the hn binary and a harness whose main carries
// policy as its first commit.
func newStandingHarness(t *testing.T, policy standingHarnessPolicy) *standingHarness {
	t.Helper()
	return newStandingHarnessWithBinary(t, buildOperationalBinary(t), policy)
}

// newStandingHarnessWithBinary is newStandingHarness for a test that builds
// several harnesses from one binary.
func newStandingHarnessWithBinary(t *testing.T, binary string, policy standingHarnessPolicy) *standingHarness {
	t.Helper()
	root := t.TempDir()
	h := &standingHarness{
		t: t, binary: binary, remote: filepath.Join(root, "origin.git"),
		work: map[string]string{}, actors: map[string]string{},
		ciFlag: filepath.Join(root, "ci-fails"), scratch: map[string]string{},
		cases: map[string]standingHarnessCase{},
	}
	for _, role := range standingHarnessRoles {
		work := filepath.Join(root, "work-"+role)
		initOperationalRepository(t, work, "Standing "+role)
		h.hnIn(work, "init", "--name", standingHarnessRoleNames[role])
		h.work[role] = work
		h.actors[role] = readOperationalIdentity(t, binary, work).Actor
	}

	source := filepath.Join(root, "source")
	initOperationalRepository(t, source, "Standing Seed")
	standingHarnessWriteFiles(t, source, map[string]string{
		".hn/policy.json": policy.JSON(h.actors),
		".hn/pipelines/" + standingHarnessPipeline + ".json": serveGovernedPipelineJSON(t, h.ciFlag),
		"src/README.md":   "Source placeholder.\n",
		"tests/README.md": "Tests placeholder.\n",
		"docs/README.md":  "Documentation placeholder.\n",
	})
	runOperationalGit(t, source, "commit", "-q", "-m", "standing seed policy")
	runOperationalGit(t, "", "init", "--bare", "-q", h.remote)
	runOperationalGit(t, source, "push", "-q", h.remote, "main:refs/heads/main")
	runOperationalGit(t, h.remote, "symbolic-ref", "HEAD", "refs/heads/main")
	h.base = runOperationalGit(t, source, "rev-parse", "HEAD")
	h.policy = policy.clone()

	for _, role := range standingHarnessRoles {
		h.git(role, "remote", "add", "origin", h.remote)
		h.git(role, "fetch", "-q", "origin")
		h.git(role, "checkout", "-q", "-B", "main", "origin/main")
		h.hn(role, "replication", "select", "origin", "--all")
	}
	return h
}

// Actor is role's full fingerprint.
func (h *standingHarness) Actor(role string) string {
	h.t.Helper()
	actor, exists := h.actors[role]
	if !exists {
		h.t.Fatalf("standing harness has no role %q", role)
	}
	return actor
}

// Work is role's clone.
func (h *standingHarness) Work(role string) string {
	h.t.Helper()
	work, exists := h.work[role]
	if !exists {
		h.t.Fatalf("standing harness has no role %q", role)
	}
	return work
}

// Base is main after the seed or the latest adoption.
func (h *standingHarness) Base() string { return h.base }

// Remote is the bare repository every clone calls origin.
func (h *standingHarness) Remote() string { return h.remote }

// Binary is the built hn binary.
func (h *standingHarness) Binary() string { return h.binary }

// Policy is the policy main carries at Base.
func (h *standingHarness) Policy() standingHarnessPolicy { return h.policy.clone() }

// standingHarnessRun runs hn without failing the test on a non-zero exit.
func standingHarnessRun(t *testing.T, binary, directory string, args ...string) (string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), standingHarnessCommandTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, binary, args...)
	command.Dir = directory
	command.Env = operationalTestEnvironment(t.TempDir())
	command.Stdin = strings.NewReader("")
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("hn %s timed out after %s: %v\n%s", strings.Join(args, " "), standingHarnessCommandTimeout, ctx.Err(), output)
	}
	if err == nil {
		return string(output), 0
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("hn %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output), exitErr.ExitCode()
}

// Run runs hn in role's clone without failing the test, for refusal
// assertions.
func (h *standingHarness) Run(role string, args ...string) (output string, status int) {
	h.t.Helper()
	return standingHarnessRun(h.t, h.binary, h.Work(role), args...)
}

// RunIn runs hn in any directory, such as a Verifier clone, without failing
// the test.
func (h *standingHarness) RunIn(directory string, args ...string) (output string, status int) {
	h.t.Helper()
	return standingHarnessRun(h.t, h.binary, directory, args...)
}

func (h *standingHarness) hnIn(directory string, args ...string) string {
	h.t.Helper()
	output, status := standingHarnessRun(h.t, h.binary, directory, args...)
	if status != 0 {
		h.t.Fatalf("hn %s in %s exited %d:\n%s", strings.Join(args, " "), directory, status, output)
	}
	return output
}

// hn runs hn in role's clone and fails the test unless it succeeds.
func (h *standingHarness) hn(role string, args ...string) string {
	h.t.Helper()
	return h.hnIn(h.Work(role), args...)
}

func (h *standingHarness) git(role string, args ...string) string {
	h.t.Helper()
	return runOperationalGit(h.t, h.Work(role), args...)
}

// standingHarnessGitInput runs git in directory with input on stdin and
// extra environment entries.
func standingHarnessGitInput(t *testing.T, directory string, input []byte, env map[string]string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = directory
	command.Env = operationalTestEnvironmentWith(t.TempDir(), env)
	command.Stdin = bytes.NewReader(input)
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("git %s timed out: %v\n%s", strings.Join(args, " "), ctx.Err(), output)
	}
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

// standingHarnessWriteFiles writes files into a worktree and stages them; a
// standingHarnessRemove value removes the path.
func standingHarnessWriteFiles(t *testing.T, worktree string, files map[string]string) {
	t.Helper()
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if files[path] == standingHarnessRemove {
			runOperationalGit(t, worktree, "rm", "-q", "--", path)
			continue
		}
		full := filepath.Join(worktree, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(files[path]), 0o644); err != nil {
			t.Fatal(err)
		}
		runOperationalGit(t, worktree, "add", "--", path)
	}
}

// latest is the head event of role's own actor chain.
func (h *standingHarness) latest(role string) operationalEvent {
	h.t.Helper()
	return latestOperationalEvent(h.t, h.Work(role), operationalActorRef(h.Actor(role)))
}

// act syncs role, runs one hn command that appends an event of kind to
// role's chain, publishes it unless publish is false, and returns it.
func (h *standingHarness) act(role, kind string, publish bool, args ...string) operationalEvent {
	h.t.Helper()
	h.hn(role, "sync", "origin")
	h.hn(role, args...)
	event := h.latest(role)
	if event.Kind != kind {
		h.t.Fatalf("hn %s in %s's clone appended %s, want %s", strings.Join(args, " "), role, event.Kind, kind)
	}
	if publish {
		h.Publish(role)
	}
	return event
}

// Publish runs the publish step of the helpers alone: hn sync origin in
// role's clone, which publishes role's whole chain.
func (h *standingHarness) Publish(role string) {
	h.t.Helper()
	h.hn(role, "sync", "origin")
}

// Commit adds one commit on a detached parent in role's clone and pushes it
// to a scratch branch, so other clones can fetch it. files maps repository
// paths to contents; standingHarnessRemove deletes a path, and no files
// writes one new uniquely named file under src/. role's clone returns to
// main afterwards.
func (h *standingHarness) Commit(role, parent string, files map[string]string) string {
	h.t.Helper()
	h.git(role, "fetch", "-q", "origin")
	h.git(role, "checkout", "-q", "--detach", parent)
	h.commits++
	if len(files) == 0 {
		files = map[string]string{fmt.Sprintf("src/change-%d.txt", h.commits): fmt.Sprintf("standing change %d\n", h.commits)}
	}
	standingHarnessWriteFiles(h.t, h.Work(role), files)
	h.git(role, "commit", "-q", "-m", fmt.Sprintf("standing commit %d", h.commits))
	commit := h.git(role, "rev-parse", "HEAD")
	branch := fmt.Sprintf("refs/heads/standing/scratch-%d", h.commits)
	h.git(role, "push", "-q", "origin", commit+":"+branch)
	h.scratch[commit] = branch
	h.git(role, "checkout", "-q", "main")
	return commit
}

// Open opens a candidate for head on base in role's clone with
// hn proposal open, which also creates the proposal code ref, and
// publishes both.
func (h *standingHarness) Open(role, base, head string) operationalEvent {
	h.t.Helper()
	h.proposals++
	event := h.act(role, "proposal.open", true, "proposal", "open", "--base", base, "--head", head,
		"--body", "Opened by the standing harness.", fmt.Sprintf("Standing candidate %d", h.proposals))
	if event.Base != base || event.Head != head {
		h.t.Fatalf("candidate %s binds %s..%s, want %s..%s", event.ID, event.Base, event.Head, base, head)
	}
	return event
}

// Approve records role's approval of candidate.
func (h *standingHarness) Approve(role, candidate string) operationalEvent {
	h.t.Helper()
	return h.act(role, "review.submit", true, "review", candidate, "--approve", "--body", "Approved by the standing harness.")
}

// ApproveUnpublished is Approve without the publish step, so the approval
// exists only in role's clone until Publish(role) or any later helper in
// role's clone.
func (h *standingHarness) ApproveUnpublished(role, candidate string) operationalEvent {
	h.t.Helper()
	return h.act(role, "review.submit", false, "review", candidate, "--approve", "--body", "Approved by the standing harness, not yet published.")
}

// RequestChanges records role's request-changes review of candidate.
func (h *standingHarness) RequestChanges(role, candidate string) operationalEvent {
	h.t.Helper()
	return h.act(role, "review.submit", true, "review", candidate, "--request-changes", "--body", "Changes requested by the standing harness.")
}

// RequestCI records role's run request for pipeline test.
func (h *standingHarness) RequestCI(role, candidate string) operationalEvent {
	h.t.Helper()
	return h.act(role, "run.request", true, "run", "request", candidate, standingHarnessPipeline)
}

// RunCI executes request on T's host.
func (h *standingHarness) RunCI(request string) operationalEvent {
	h.t.Helper()
	return h.act("T", "run.result", true, "run", "execute", request, "--backend", "host", "--allow-unsafe-host-execution")
}

// FailCI makes pipeline test fail until PassCI. The definition is the same
// either way.
func (h *standingHarness) FailCI() {
	h.t.Helper()
	if err := os.WriteFile(h.ciFlag, []byte("fail\n"), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

// PassCI makes pipeline test pass again.
func (h *standingHarness) PassCI() {
	h.t.Helper()
	if err := os.Remove(h.ciFlag); err != nil && !errors.Is(err, os.ErrNotExist) {
		h.t.Fatal(err)
	}
}

// Accept records role's acceptance decision for candidate.
func (h *standingHarness) Accept(role, candidate string) operationalEvent {
	h.t.Helper()
	return h.act(role, "proposal.decision", true, "decide", candidate, "--accept", "--body", "Accepted by the standing harness.")
}

// Reject records role's rejection of candidate with body.
func (h *standingHarness) Reject(role, candidate, body string) operationalEvent {
	h.t.Helper()
	return h.act(role, "proposal.decision", true, "decide", candidate, "--reject", "--body", body)
}

// standingHarnessCase is a candidate with the evidence the harness gathered
// for it: R's approval and T's passing result of M's request, plus any
// defect-specific evidence in Extra.
type standingHarnessCase struct {
	Base      string
	Files     map[string]string
	Author    string
	Candidate operationalEvent
	Approval  operationalEvent
	Request   operationalEvent
	Result    operationalEvent
	Extra     []operationalEvent
}

// Evidence is the evidence of a valid rule merge fact for the case.
func (c standingHarnessCase) Evidence() []string {
	return []string{c.Approval.ID, c.Result.ID}
}

// openCase commits files on base in author's clone and opens a candidate.
func (h *standingHarness) openCase(author, base string, files map[string]string) standingHarnessCase {
	h.t.Helper()
	head := h.Commit(author, base, files)
	return standingHarnessCase{Base: base, Files: files, Author: author, Candidate: h.Open(author, base, head)}
}

// completeCase adds R's approval unless the case has one, M's run request
// and T's passing result.
func (h *standingHarness) completeCase(c *standingHarnessCase) {
	h.t.Helper()
	if c.Approval.ID == "" {
		c.Approval = h.Approve("R", c.Candidate.ID)
	}
	c.Request = h.RequestCI("M", c.Candidate.ID)
	c.Result = h.RunCI(c.Request.ID)
	if outcome := h.fact("T", c.Result.ID).Outcome; outcome != "passed" {
		h.t.Fatalf("run result %s for candidate %s is %q, want passed", c.Result.ID, c.Candidate.ID, outcome)
	}
	h.cases[c.Candidate.ID] = *c
}

// readyCase opens a candidate by author on base and gathers R's approval
// and T's passing result of M's request, with no maintainer decision.
func (h *standingHarness) readyCase(author, base string, files map[string]string) standingHarnessCase {
	h.t.Helper()
	c := h.openCase(author, base, files)
	h.completeCase(&c)
	return c
}

// ReadyCandidate is the User Story 1 starting point: A commits files on
// h.base and opens a candidate, R approves, M requests CI and T runs it,
// with no maintainer decision.
func (h *standingHarness) ReadyCandidate(files map[string]string) operationalEvent {
	h.t.Helper()
	c := h.readyCase("A", h.base, files)
	h.lastReady = c.Candidate.ID
	return c.Candidate
}

// Case returns the evidence the harness gathered for candidate.
func (h *standingHarness) Case(candidate string) standingHarnessCase {
	h.t.Helper()
	c, exists := h.cases[candidate]
	if !exists {
		h.t.Fatalf("candidate %s was not built by the standing harness", candidate)
	}
	return c
}

// Checkout puts branch of role's clone at commit with a clean worktree, as
// automerge requires.
func (h *standingHarness) Checkout(role, branch, commit string) {
	h.t.Helper()
	h.git(role, "fetch", "-q", "origin")
	h.git(role, "checkout", "-q", "-f", "-B", branch, commit)
	h.git(role, "clean", "-q", "-fd")
}

// PolicyCommit has M commit policy as .hn/policy.json on parent and returns
// the commit, which is on the remote but on no branch the harness merges. A
// candidate on it is governed by policy.
func (h *standingHarness) PolicyCommit(parent string, policy standingHarnessPolicy) string {
	h.t.Helper()
	return h.Commit("M", parent, map[string]string{serveGovernedPolicyPath: policy.JSON(h.actors)})
}

// AdoptThroughAmendment installs policy on main through the manual path a
// maintainer amendment takes (US2-2), never hn automerge: M commits the
// policy on a branch and opens the amendment, R approves, M requests CI and
// T runs it, M accepts with hn decide --accept, merges with hn merge on
// main and publishes main and its facts. It returns the amendment's
// candidate ID and moves h.base and every clone's main to the merge.
func (h *standingHarness) AdoptThroughAmendment(policy standingHarnessPolicy) (amendment string) {
	h.t.Helper()
	head := h.PolicyCommit(h.base, policy)
	candidate := h.Open("M", h.base, head)
	h.Approve("R", candidate.ID)
	request := h.RequestCI("M", candidate.ID)
	h.RunCI(request.ID)
	h.Accept("M", candidate.ID)
	h.Checkout("M", "main", h.base)
	h.hn("M", "merge", candidate.ID)
	if merge := h.latest("M"); merge.Kind != "proposal.merged" || merge.Subject != candidate.ID {
		h.t.Fatalf("hn merge %s appended %#v, want its proposal.merged", candidate.ID, merge)
	}
	h.hn("M", "sync", "origin")
	h.git("M", "push", "-q", "origin", "main")
	h.base = h.git("M", "rev-parse", "HEAD")
	h.policy = policy.clone()
	for _, role := range standingHarnessRoles {
		h.Checkout(role, "main", "origin/main")
	}
	return candidate.ID
}

// Events is every event on role's own actor chain, oldest first.
func (h *standingHarness) Events(role string) []operationalEvent {
	h.t.Helper()
	ref := operationalActorRef(h.Actor(role))
	if h.git(role, "for-each-ref", "--format=%(refname)", ref) == "" {
		return nil
	}
	var events []operationalEvent
	for _, commit := range strings.Fields(h.git(role, "rev-list", "--reverse", ref)) {
		payload := runOperationalGitBytes(h.t, h.Work(role), "show", commit+":event.json")
		var event operationalEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			h.t.Fatalf("decode event at %s: %v\n%s", commit, err, payload)
		}
		event.ID = operationalDigest(payload)
		events = append(events, event)
	}
	return events
}

// standingHarnessFact is a signed event as the harness reads it from a
// chain. operationalEvent has no rule; Rule here is the canonical rule
// claim exactly as verifiers recognise it, empty for every non-canonical
// shape and for every kind other than proposal.merged.
type standingHarnessFact struct {
	ID       string
	Kind     string   `json:"kind"`
	Actor    string   `json:"actor"`
	Sequence uint64   `json:"sequence"`
	Subject  string   `json:"subject"`
	Head     string   `json:"head"`
	Commit   string   `json:"commit"`
	Policy   string   `json:"policy"`
	Verdict  string   `json:"verdict"`
	Outcome  string   `json:"outcome"`
	Evidence []string `json:"evidence"`
	Rule     string   `json:"-"`
	Payload  []byte   `json:"-"`
}

func standingHarnessReadFact(t *testing.T, payload []byte) standingHarnessFact {
	t.Helper()
	var fact standingHarnessFact
	if err := json.Unmarshal(payload, &fact); err != nil {
		t.Fatalf("decode signed event: %v\n%s", err, payload)
	}
	fact.ID = operationalDigest(payload)
	fact.Payload = append([]byte(nil), payload...)
	if fact.Kind == "proposal.merged" {
		fact.Rule = parseMergeRuleClaim(payload)
	}
	return fact
}

// fact reads the event with ID id from role's own chain.
func (h *standingHarness) fact(role, id string) standingHarnessFact {
	h.t.Helper()
	ref := operationalActorRef(h.Actor(role))
	for _, commit := range strings.Fields(h.git(role, "rev-list", ref)) {
		payload := runOperationalGitBytes(h.t, h.Work(role), "show", commit+":event.json")
		if operationalDigest(payload) == id {
			return standingHarnessReadFact(h.t, payload)
		}
	}
	h.t.Fatalf("event %s is not on %s's chain", id, role)
	return standingHarnessFact{}
}

// AllEvents is every event on every local and accepted actor ref in role's
// clone, read with git alone and sorted by ID, so reading never projects.
func (h *standingHarness) AllEvents(role string) []standingHarnessFact {
	h.t.Helper()
	refs := strings.Fields(h.git(role, "for-each-ref", "--format=%(refname)", "refs/hn/actors", "refs/hn/remotes"))
	seen := map[string]bool{}
	var facts []standingHarnessFact
	for _, ref := range refs {
		if !strings.HasPrefix(ref, "refs/hn/actors/") {
			if _, _, ok := parseAcceptedActorRef(ref); !ok {
				continue
			}
		}
		for _, commit := range strings.Fields(h.git(role, "rev-list", ref)) {
			if seen[commit] {
				continue
			}
			seen[commit] = true
			payload := runOperationalGitBytes(h.t, h.Work(role), "show", commit+":event.json")
			facts = append(facts, standingHarnessReadFact(h.t, payload))
		}
	}
	sort.Slice(facts, func(i, j int) bool { return facts[i].ID < facts[j].ID })
	return facts
}

// MergeFacts is every proposal.merged for candidate on every local and
// accepted actor ref in role's clone, sorted by ID.
func (h *standingHarness) MergeFacts(role, candidate string) []standingHarnessFact {
	h.t.Helper()
	var facts []standingHarnessFact
	for _, fact := range h.AllEvents(role) {
		if fact.Kind == "proposal.merged" && fact.Subject == candidate {
			facts = append(facts, fact)
		}
	}
	return facts
}

// Refs is every ref of role's clone with its value, for before and after
// comparisons.
func (h *standingHarness) Refs(role string) string {
	h.t.Helper()
	return h.git(role, "for-each-ref", "--format=%(refname) %(objectname)")
}

// RemoteRefs is every ref of the bare remote with its value.
func (h *standingHarness) RemoteRefs() string {
	h.t.Helper()
	return runOperationalGit(h.t, h.remote, "for-each-ref", "--format=%(refname) %(objectname)")
}

// Verifier is a fresh full clone of the remote with no Hubnot identity and
// the explicit --all selection for origin, for US1-2 style checks. It has
// not synced yet.
func (h *standingHarness) Verifier() string {
	h.t.Helper()
	verifier := filepath.Join(h.t.TempDir(), "verifier")
	runOperationalGit(h.t, "", "clone", "-q", "--no-local", h.remote, verifier)
	configureOperationalGit(h.t, verifier, "Standing Verifier")
	h.hnIn(verifier, "replication", "select", "origin", "--all")
	return verifier
}

// inClone runs fn in role's clone with role's identity loaded in-process,
// and restores the working directory afterwards, also when fn fails the
// test. Callers must not run in parallel tests.
func (h *standingHarness) inClone(role string, fn func(identity *Identity)) {
	h.t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		h.t.Fatal(err)
	}
	if err := os.Chdir(h.Work(role)); err != nil {
		h.t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(previous); err != nil {
			h.t.Errorf("restore working directory: %v", err)
		}
	}()
	identity, err := loadIdentity()
	if err != nil {
		h.t.Fatal(err)
	}
	if identity.Actor != h.Actor(role) {
		h.t.Fatalf("identity in %s's clone is %s, want %s", role, identity.Actor, h.Actor(role))
	}
	fn(identity)
}

// NextEvent is the next event of kind on role's chain, with its sequence,
// predecessor and signer filled, for building raw payloads.
func (h *standingHarness) NextEvent(role, kind string) Event {
	h.t.Helper()
	var event Event
	h.inClone(role, func(identity *Identity) {
		next, err := nextEvent(identity, kind)
		if err != nil {
			h.t.Fatal(err)
		}
		event = next
	})
	return event
}

// standingHarnessSigned is one event a fact signer wrote.
type standingHarnessSigned struct {
	ID        string
	Role      string
	Commit    string // chain commit that holds the event
	Candidate string // candidate the event concerns, when it concerns one
	Payload   []byte
	Signature []byte
	// Unannotated is, for a raw payload, the encoding encodeAndSign would
	// produce for the same event, before the extra members were added.
	Unannotated []byte
}

// PolicyDigest is the digest of .hn/policy.json at commit, read from the
// remote.
func (h *standingHarness) PolicyDigest(commit string) string {
	h.t.Helper()
	return operationalDigest(runOperationalGitBytes(h.t, h.remote, "show", commit+":"+serveGovernedPolicyPath))
}

// factEvent is the next proposal.merged on role's chain filled as a valid
// rule merge fact of rule ordinary for c. Its commit is the candidate head,
// the result of a fast-forward of the base.
func (h *standingHarness) factEvent(role string, c standingHarnessCase) Event {
	h.t.Helper()
	event := h.NextEvent(role, "proposal.merged")
	event.Subject, event.Head, event.Commit = c.Candidate.ID, c.Candidate.Head, c.Candidate.Head
	event.Policy = h.PolicyDigest(c.Candidate.Base)
	event.Evidence = c.Evidence()
	event.Rule = "ordinary"
	return event
}

// SignFactFor signs, in role's clone, a valid rule merge fact of rule
// ordinary for c, after mutate changes it. appendEvent validates content
// only, never relationships, so the CLI judges the fact later.
func (h *standingHarness) SignFactFor(role string, c standingHarnessCase, mutate func(*Event)) standingHarnessSigned {
	h.t.Helper()
	event := h.factEvent(role, c)
	if mutate != nil {
		mutate(&event)
	}
	var signed standingHarnessSigned
	h.inClone(role, func(identity *Identity) {
		stored, err := appendEvent(event, identity)
		if err != nil {
			h.t.Fatalf("sign merge fact in %s's clone: %v", role, err)
		}
		signed = standingHarnessSigned{
			ID: stored.ID, Role: role, Commit: stored.Commit, Candidate: event.Subject,
			Payload: stored.Payload, Signature: stored.Signature,
		}
	})
	return signed
}

// SignFact signs, in role's clone, a valid rule merge fact of rule ordinary
// for the candidate of the latest ReadyCandidate, after mutate changes it,
// and returns its ID. It writes the fact to role's chain only.
func (h *standingHarness) SignFact(role string, mutate func(*Event)) string {
	h.t.Helper()
	if h.lastReady == "" {
		h.t.Fatal("SignFact needs a candidate from ReadyCandidate")
	}
	return h.SignFactFor(role, h.Case(h.lastReady), mutate).ID
}

// signBytes signs payload with role's private key.
func (h *standingHarness) signBytes(role string, payload []byte) []byte {
	h.t.Helper()
	var signature []byte
	h.inClone(role, func(identity *Identity) {
		privateKey, err := identity.privateKey()
		if err != nil {
			h.t.Fatal(err)
		}
		signature = ed25519.Sign(privateKey, payload)
	})
	return signature
}

// signRaw signs the exact payload with role's key and appends it to role's
// chain the way appendEventWithAttachments does: event.json and signature
// blobs, mktree, commit-tree on the previous chain head and update-ref of
// the actor ref against its old value. It validates nothing.
func (h *standingHarness) signRaw(role string, payload []byte) standingHarnessSigned {
	h.t.Helper()
	signature := h.signBytes(role, payload)
	work, actor := h.Work(role), h.Actor(role)
	ref := actorRef(actor)
	id := eventID(payload)
	eventBlob := standingHarnessGitInput(h.t, work, payload, nil, "hash-object", "-w", "--stdin")
	signatureBlob := standingHarnessGitInput(h.t, work, []byte(base64.RawStdEncoding.EncodeToString(signature)), nil, "hash-object", "-w", "--stdin")
	tree := standingHarnessGitInput(h.t, work, []byte(fmt.Sprintf("100644 blob %s\tevent.json\n100644 blob %s\tsignature\n", eventBlob, signatureBlob)), nil, "mktree")
	previous := h.git(role, "for-each-ref", "--format=%(objectname)", ref)
	args := []string{"commit-tree", tree, "-m", "hn event " + id}
	if previous != "" {
		args = append(args, "-p", previous)
	}
	var header struct {
		Kind      string `json:"kind"`
		ActorName string `json:"actorName"`
	}
	_ = json.Unmarshal(payload, &header)
	email := shortID(actor) + "@hn.invalid"
	commit := standingHarnessGitInput(h.t, work, nil, map[string]string{
		"GIT_AUTHOR_NAME": header.ActorName, "GIT_AUTHOR_EMAIL": email,
		"GIT_COMMITTER_NAME": header.ActorName, "GIT_COMMITTER_EMAIL": email,
	}, args...)
	h.git(role, "update-ref", "-m", "hn: append "+header.Kind, ref, commit, previous)
	signed := standingHarnessSigned{
		ID: id, Role: role, Commit: commit,
		Payload: append([]byte(nil), payload...), Signature: signature,
	}
	switch header.Kind {
	case "proposal.merged", "review.submit", "run.request", "proposal.decision":
		var subject struct {
			Subject string `json:"subject"`
		}
		_ = json.Unmarshal(payload, &subject)
		signed.Candidate = subject.Subject
	}
	return signed
}

// SignRaw signs payload exactly as given with role's key, appends it to
// role's chain and returns its ID. Use it for payloads Hubnot's own signer
// never writes, of any kind. The payload's sequence and previous decide
// whether the chain stays valid; build them with NextEvent.
func (h *standingHarness) SignRaw(role string, payload []byte) string {
	h.t.Helper()
	return h.signRaw(role, payload).ID
}

// signAnnotated signs event's canonical encoding with members added as
// top-level keys before the closing brace.
func (h *standingHarness) signAnnotated(role string, event Event, members string) standingHarnessSigned {
	h.t.Helper()
	canonical, err := marshalEvent(event)
	if err != nil {
		h.t.Fatal(err)
	}
	signed := h.signRaw(role, standingEventWithMembers(canonical, members))
	signed.Unannotated = canonical
	return signed
}

// WithdrawCode removes c's head from the remote: the proposal code ref, the
// scratch branch and the author's local proposal ref, which a later sync
// would publish again, then prunes the remote. Clones that already fetched
// the head keep it; a fresh clone, E's clone (which never fetches) and a
// verifier do not. The author's chain then names a candidate whose code
// the remote no longer advertises, so build anything that needs later
// syncs before calling it, or use its own harness.
func (h *standingHarness) WithdrawCode(c standingHarnessCase) {
	h.t.Helper()
	head := c.Candidate.Head
	ref := proposalRef(c.Candidate.ID)
	runOperationalGit(h.t, h.remote, "update-ref", "-d", ref)
	if branch, exists := h.scratch[head]; exists {
		runOperationalGit(h.t, h.remote, "update-ref", "-d", branch)
		delete(h.scratch, head)
	}
	h.git(c.Author, "update-ref", "-d", ref)
	runOperationalGit(h.t, h.remote, "gc", "-q", "--prune=now")
	runOperationalGitFailure(h.t, h.remote, "cat-file", "-e", head+"^{commit}")
}

// standingHarnessDefect is one row of the fixture matrix of
// contracts/rule-merge-fact.md.
type standingHarnessDefect struct {
	ID string
	// Signer is the role that signs the defective fact: E, or X for F03.
	// Neither is a maintainer, so path (a) fails for every row.
	Signer string
	// Expected is the check that must refuse the fact,
	// standingHarnessMaintainerPath for F19 and F20, or
	// standingHarnessMissingDependency for F21 and F22.
	Expected string
	// Prepare builds the candidate and evidence the fact cites. Rows that
	// change only the fact return the shared candidate.
	Prepare func() standingHarnessCase
	// Build signs the defective fact for the prepared case: one fact, or
	// one per payload shape for F19 and F20.
	Build func(standingHarnessCase) []standingHarnessSigned
	// Control signs the matched valid rule merge fact of WP09 and WP16 for
	// the prepared case. For a fact-only row it cites the same candidate;
	// for F15 to F18 it builds a corresponding valid candidate from the
	// same base and files with the defect removed. nil for F21 and F22.
	Control func(standingHarnessCase) standingHarnessSigned
}

// standingHarnessDefectIDs lists the rows of the fixture matrix in order.
var standingHarnessDefectIDs = []string{
	"F01", "F02", "F03", "F04", "F05", "F06", "F07", "F08", "F09", "F10", "F11",
	"F12", "F13", "F14", "F15", "F16", "F17", "F18", "F19", "F20", "F21", "F22",
}

// standingHarnessDefects returns one entry per fixture F01 to F22, built
// from candidate, which ReadyCandidate must have created under the default
// policy. Every builder signs only; nothing is published.
//
// F21's Prepare withholds R's approval: it signs it with ApproveUnpublished,
// so the fact's approval is missing until h.Publish("R"). Every helper that
// runs in R's clone publishes R's whole chain, so run no R helper between
// F21's Prepare and its pre-publication assertion: build F21 last or in its
// own harness. F22's Prepare withdraws its candidate's code from the remote
// (WithdrawCode); build it after every row that needs later syncs, or in its
// own harness. F21 and F22 cannot share a harness.
func standingHarnessDefects(h *standingHarness, candidate operationalEvent) map[string]standingHarnessDefect {
	h.t.Helper()
	shared := h.Case(candidate.ID)
	same := func() standingHarnessCase { return shared }
	sign := func(signer string, mutate func(*Event, standingHarnessCase)) func(standingHarnessCase) []standingHarnessSigned {
		return func(c standingHarnessCase) []standingHarnessSigned {
			return []standingHarnessSigned{h.SignFactFor(signer, c, func(event *Event) {
				if mutate != nil {
					mutate(event, c)
				}
			})}
		}
	}
	evidence := func(items func(standingHarnessCase) []operationalEvent) func(*Event, standingHarnessCase) {
		return func(event *Event, c standingHarnessCase) {
			event.Evidence = nil
			for _, item := range items(c) {
				event.Evidence = append(event.Evidence, item.ID)
			}
		}
	}
	// raw signs one fact per payload shape: the valid fact without its rule
	// claim, with members added as top-level keys.
	raw := func(members ...string) func(standingHarnessCase) []standingHarnessSigned {
		return func(c standingHarnessCase) []standingHarnessSigned {
			signed := make([]standingHarnessSigned, 0, len(members))
			for _, member := range members {
				event := h.factEvent("E", c)
				event.Rule = ""
				signed = append(signed, h.signAnnotated("E", event, member))
			}
			return signed
		}
	}
	valid := func(c standingHarnessCase) standingHarnessSigned { return h.SignFactFor("E", c, nil) }
	rebound := func(author string, files func(standingHarnessCase) map[string]string) func(standingHarnessCase) standingHarnessSigned {
		return func(c standingHarnessCase) standingHarnessSigned {
			return valid(h.readyCase(author, c.Base, files(c)))
		}
	}
	srcOnly := func(name string) func(standingHarnessCase) map[string]string {
		return func(standingHarnessCase) map[string]string {
			return map[string]string{"src/" + name + ".txt": "allowlisted change of " + name + "\n"}
		}
	}
	variant := func(change func(*standingHarnessPolicy)) string {
		policy := h.policy.clone()
		change(&policy)
		return h.PolicyCommit(h.base, policy)
	}
	ordinary := func(policy *standingHarnessPolicy) *standingHarnessRule {
		for index := range policy.Rules {
			if policy.Rules[index].Name == "ordinary" {
				return &policy.Rules[index]
			}
		}
		h.t.Fatal("the harness policy has no rule ordinary")
		return nil
	}

	defects := []standingHarnessDefect{
		{ID: "F01", Signer: "E", Expected: "rule-exists", Prepare: same,
			Build: sign("E", func(event *Event, _ standingHarnessCase) { event.Rule = "nosuch" }), Control: valid},
		{ID: "F02", Signer: "E", Expected: "rule-acceptance",
			Prepare: func() standingHarnessCase {
				base := variant(func(policy *standingHarnessPolicy) {
					policy.Rules = append(policy.Rules, standingHarnessRule{
						Name: "maint", TargetBranch: "main", Acceptance: standingHarnessString(automergeAcceptanceMaintainers),
						Actors: []string{"M"}, Authors: []string{"A"}, Paths: []string{"src/", "tests/", "docs/"},
					})
				})
				return h.readyCase("A", base, nil)
			},
			Build: sign("E", func(event *Event, _ standingHarnessCase) { event.Rule = "maint" }), Control: valid},
		{ID: "F03", Signer: "X", Expected: "rule-signer", Prepare: same, Build: sign("X", nil), Control: valid},
		{ID: "F04", Signer: "E", Expected: "policy-digest", Prepare: same,
			Build: sign("E", func(event *Event, _ standingHarnessCase) {
				event.Policy = eventID([]byte("standing harness: not the base policy"))
			}), Control: valid},
		{ID: "F05", Signer: "E", Expected: "evidence-item",
			Prepare: func() standingHarnessCase {
				c := shared
				other := h.openCase("A", h.base, nil)
				c.Extra = []operationalEvent{h.Approve("R", other.Candidate.ID)}
				return c
			},
			Build: sign("E", evidence(func(c standingHarnessCase) []operationalEvent {
				return []operationalEvent{c.Approval, c.Extra[0], c.Result}
			})), Control: valid},
		{ID: "F06", Signer: "E", Expected: "evidence-item",
			Prepare: func() standingHarnessCase {
				c := h.readyCase("A", h.base, nil)
				c.Extra = []operationalEvent{h.Accept("M", c.Candidate.ID)}
				return c
			},
			Build: sign("E", evidence(func(c standingHarnessCase) []operationalEvent {
				return []operationalEvent{c.Approval, c.Result, c.Extra[0]}
			})), Control: valid},
		{ID: "F07", Signer: "E", Expected: "evidence-item",
			Prepare: func() standingHarnessCase {
				c := h.openCase("A", h.base, nil)
				c.Extra = []operationalEvent{h.RequestChanges("R", c.Candidate.ID)}
				h.completeCase(&c)
				return c
			},
			Build: sign("E", evidence(func(c standingHarnessCase) []operationalEvent {
				return []operationalEvent{c.Extra[0], c.Approval, c.Result}
			})), Control: valid},
		{ID: "F08", Signer: "E", Expected: "evidence-item",
			Prepare: func() standingHarnessCase {
				c := h.openCase("A", h.base, nil)
				c.Approval = h.Approve("R", c.Candidate.ID)
				h.FailCI()
				failed := h.RunCI(h.RequestCI("M", c.Candidate.ID).ID)
				h.PassCI()
				if outcome := h.fact("T", failed.ID).Outcome; outcome != "failed" {
					h.t.Fatalf("run result %s is %q, want failed", failed.ID, outcome)
				}
				c.Extra = []operationalEvent{failed}
				h.completeCase(&c)
				return c
			},
			Build: sign("E", evidence(func(c standingHarnessCase) []operationalEvent {
				return []operationalEvent{c.Approval, c.Extra[0], c.Result}
			})), Control: valid},
		{ID: "F09", Signer: "E", Expected: "evidence-item",
			Prepare: func() standingHarnessCase {
				c := h.readyCase("A", h.base, nil)
				c.Extra = []operationalEvent{h.Approve("S", c.Candidate.ID)}
				return c
			},
			Build: sign("E", evidence(func(c standingHarnessCase) []operationalEvent {
				return []operationalEvent{c.Approval, c.Extra[0], c.Result}
			})), Control: valid},
		{ID: "F10", Signer: "E", Expected: "evidence-item",
			Prepare: func() standingHarnessCase {
				// The author's approval counts for no rule with reviewers, so
				// the variant drops ordinary's reviewers and lists A as a
				// trusted reviewer whose own approval the policy allows.
				base := variant(func(policy *standingHarnessPolicy) {
					policy.AllowAuthorApproval = true
					policy.TrustedReviewers = append(policy.TrustedReviewers, "A")
					ordinary(policy).Reviewers = nil
				})
				c := h.openCase("A", base, nil)
				c.Approval = h.Approve("R", c.Candidate.ID)
				c.Extra = []operationalEvent{h.Approve("A", c.Candidate.ID)}
				h.completeCase(&c)
				return c
			},
			Build: sign("E", evidence(func(c standingHarnessCase) []operationalEvent {
				return []operationalEvent{c.Approval, c.Extra[0], c.Result}
			})), Control: valid},
		{ID: "F11", Signer: "E", Expected: "evidence-item",
			Prepare: func() standingHarnessCase {
				c := h.readyCase("A", h.base, nil)
				c.Extra = []operationalEvent{h.RunCI(h.RequestCI("A", c.Candidate.ID).ID)}
				return c
			},
			Build: sign("E", evidence(func(c standingHarnessCase) []operationalEvent {
				return []operationalEvent{c.Approval, c.Result, c.Extra[0]}
			})), Control: valid},
		{ID: "F12", Signer: "E", Expected: "evidence-duplicate",
			Prepare: func() standingHarnessCase {
				c := h.readyCase("A", h.base, nil)
				c.Extra = []operationalEvent{h.Approve("R", c.Candidate.ID)}
				return c
			},
			Build: sign("E", evidence(func(c standingHarnessCase) []operationalEvent {
				return []operationalEvent{c.Approval, c.Extra[0], c.Result}
			})), Control: valid},
		{ID: "F13", Signer: "E", Expected: "approval-threshold", Prepare: same,
			Build: sign("E", evidence(func(c standingHarnessCase) []operationalEvent {
				return []operationalEvent{c.Result}
			})), Control: valid},
		{ID: "F14", Signer: "E", Expected: "pipeline-threshold", Prepare: same,
			Build: sign("E", evidence(func(c standingHarnessCase) []operationalEvent {
				return []operationalEvent{c.Approval}
			})), Control: valid},
		{ID: "F15", Signer: "E", Expected: "rule-author",
			Prepare: func() standingHarnessCase { return h.readyCase("S", h.base, srcOnly("f15")(shared)) },
			Build:   sign("E", nil), Control: rebound("A", srcOnly("f15"))},
		{ID: "F16", Signer: "E", Expected: "changed-paths",
			Prepare: func() standingHarnessCase {
				return h.readyCase("A", h.base, map[string]string{
					"src/f16.txt": "allowlisted change of f16\n", "outside/f16.txt": "change outside every rule\n",
				})
			},
			Build: sign("E", nil), Control: rebound("A", srcOnly("f16"))},
		{ID: "F17", Signer: "E", Expected: "manual-path",
			Prepare: func() standingHarnessCase {
				policy := string(runOperationalGitBytes(h.t, h.remote, "show", h.base+":"+serveGovernedPolicyPath))
				return h.readyCase("A", h.base, map[string]string{
					serveGovernedPolicyPath: policy + "\n", "src/f17.txt": "allowlisted change of f17\n",
				})
			},
			Build: sign("E", nil), Control: rebound("A", srcOnly("f17"))},
		{ID: "F18", Signer: "E", Expected: "manual-path",
			Prepare: func() standingHarnessCase {
				base := variant(func(policy *standingHarnessPolicy) { policy.ProtectedPaths = []string{"ci/"} })
				return h.readyCase("A", base, map[string]string{
					"src/f18.txt": "allowlisted change of f18\n", "ci/check.sh": "#!/bin/sh\nexit 0\n",
				})
			},
			Build: sign("E", nil), Control: rebound("A", srcOnly("f18"))},
		{ID: "F19", Signer: "E", Expected: standingHarnessMaintainerPath, Prepare: same,
			Build: raw(`"RULE":"ordinary"`, `"Rule":"ordinary"`), Control: valid},
		{ID: "F20", Signer: "E", Expected: standingHarnessMaintainerPath, Prepare: same,
			Build:   raw(`"rule":""`, `"rule":null`, `"rule":1`, `"rule":"ordinary","rule":"ordinary"`, `"rule":"bad name"`),
			Control: valid},
		{ID: "F21", Signer: "E", Expected: standingHarnessMissingDependency,
			Prepare: func() standingHarnessCase {
				c := h.openCase("A", h.base, nil)
				c.Approval = h.ApproveUnpublished("R", c.Candidate.ID)
				h.completeCase(&c)
				return c
			},
			Build: sign("E", nil)},
		{ID: "F22", Signer: "E", Expected: standingHarnessMissingDependency,
			Prepare: func() standingHarnessCase {
				c := h.readyCase("A", h.base, nil)
				h.WithdrawCode(c)
				return c
			},
			Build: sign("E", nil)},
	}
	table := make(map[string]standingHarnessDefect, len(defects))
	for _, defect := range defects {
		table[defect.ID] = defect
	}
	return table
}

// standingHarnessFixture is one legacy history fixture (L01 to L06) or one
// activation boundary fixture (B1 to B4) of contracts/rule-merge-fact.md.
type standingHarnessFixture struct {
	ID     string
	Signer string
	// Build prepares what the fixture cites and signs it: one event, or one
	// per payload shape or event kind the row names.
	Build func() []standingHarnessSigned
	// VerifyError is the exact verifyEvent error of every signed payload,
	// empty when each verifies.
	VerifyError string
	// Verdict is the expected verdict on every surface and client (legacy
	// fixtures). An acceptance merge fact whose payload carries a canonical
	// rule claim is standingHarnessAnnotatedMaintainer, because hn proposal
	// status reports the claim as an unverified annotation.
	Verdict string
	// NeedsRulePath is set when only path (b) admits the fact (B1).
	NeedsRulePath bool
	// WithRulePath and WithoutRulePath are the expected verdicts on a
	// surface that implements path (b) and on one that does not (boundary
	// fixtures).
	WithRulePath, WithoutRulePath string
}

// standingHarnessLegacyFixtures returns rows L01 to L06 for a harness on
// standingHarnessLegacyPolicy, which the older client can read. L01 to L05
// are raw payloads built from the JSON encodeAndSign would produce for the
// same event plus the extra key; L06 is the negative control. L06 is signed
// by S alone, because its refused event makes S's chain unreadable.
func standingHarnessLegacyFixtures(h *standingHarness) []standingHarnessFixture {
	h.t.Helper()
	var shared *standingHarnessCase
	ready := func() standingHarnessCase {
		if shared == nil {
			c := h.readyCase("A", h.base, nil)
			shared = &c
		}
		return *shared
	}
	accepted := func() (standingHarnessCase, operationalEvent) {
		c := h.readyCase("A", h.base, nil)
		return c, h.Accept("M", c.Candidate.ID)
	}
	merge := func(c standingHarnessCase, evidence ...operationalEvent) Event {
		event := h.NextEvent("M", "proposal.merged")
		event.Subject, event.Head, event.Commit = c.Candidate.ID, c.Candidate.Head, c.Candidate.Head
		event.Policy = h.PolicyDigest(c.Candidate.Base)
		for _, item := range evidence {
			event.Evidence = append(event.Evidence, item.ID)
		}
		return event
	}
	return []standingHarnessFixture{
		{ID: "L01", Signer: "R", Verdict: standingHarnessValid, Build: func() []standingHarnessSigned {
			c := ready()
			event := h.NextEvent("R", "review.submit")
			event.Subject, event.Verdict, event.Body = c.Candidate.ID, "approve", "Legacy approval with a rule annotation."
			return []standingHarnessSigned{h.signAnnotated("R", event, `"rule":"annotation"`)}
		}},
		{ID: "L02", Signer: "M", Verdict: standingHarnessValid, Build: func() []standingHarnessSigned {
			c := ready()
			issue := h.act("M", "issue.open", true, "issue", "open", "Legacy issue for L02")
			request := h.NextEvent("M", "run.request")
			request.Subject, request.Pipeline = c.Candidate.ID, standingHarnessPipeline
			request.Definition, request.Commit = c.Request.Definition, c.Request.Commit
			signed := []standingHarnessSigned{h.signAnnotated("M", request, `"RULE":"x"`)}
			comment := h.NextEvent("M", "issue.comment")
			comment.Subject, comment.Body = issue.ID, "Legacy comment with a numeric rule."
			return append(signed, h.signAnnotated("M", comment, `"rule":1`))
		}},
		{ID: "L03", Signer: "M", Verdict: standingHarnessAnnotatedMaintainer, Build: func() []standingHarnessSigned {
			c, decision := accepted()
			return []standingHarnessSigned{h.signAnnotated("M", merge(c, decision), `"rule":"nosuch"`)}
		}},
		{ID: "L04", Signer: "M", Verdict: standingHarnessAnnotatedMaintainer, Build: func() []standingHarnessSigned {
			c, decision := accepted()
			return []standingHarnessSigned{h.signAnnotated("M", merge(c, decision, c.Approval), `"rule":"maint"`)}
		}},
		{ID: "L05", Signer: "M", Verdict: standingHarnessValidMaintainer, Build: func() []standingHarnessSigned {
			c, decision := accepted()
			var signed []standingHarnessSigned
			for _, member := range []string{`"rule":null`, `"rule":""`, `"rule":1`, `"Rule":"ordinary"`, `"rule":"maint","rule":"maint"`} {
				signed = append(signed, h.signAnnotated("M", merge(c, decision), member))
			}
			return signed
		}},
		{ID: "L06", Signer: "S", VerifyError: standingHarnessUnknownRuleField, Verdict: standingHarnessRefusedAsBefore,
			Build: func() []standingHarnessSigned {
				event := h.NextEvent("S", "project.open")
				event.Intent = "open"
				event.Body = string(resourceState(h.t, ResourceProject, projectState()))
				return []standingHarnessSigned{h.signAnnotated("S", event, `"rule":"annotation"`)}
			}},
	}
}

// standingHarnessBoundaryFixtures returns rows B1 to B4 for a harness on
// the default policy. Each builder prepares its own candidate. B5 is a
// push, not a fact; WP13 builds it.
func standingHarnessBoundaryFixtures(h *standingHarness) []standingHarnessFixture {
	h.t.Helper()
	accepted := func() (standingHarnessCase, operationalEvent) {
		candidate := h.ReadyCandidate(nil)
		return h.Case(candidate.ID), h.Accept("M", candidate.ID)
	}
	return []standingHarnessFixture{
		{ID: "B1", Signer: "E", NeedsRulePath: true,
			WithRulePath: standingHarnessValidRuleMerge, WithoutRulePath: standingHarnessRefused,
			Build: func() []standingHarnessSigned {
				candidate := h.ReadyCandidate(nil)
				return []standingHarnessSigned{h.SignFactFor("E", h.Case(candidate.ID), nil)}
			}},
		{ID: "B2", Signer: "M",
			WithRulePath: standingHarnessAnnotatedMaintainer, WithoutRulePath: standingHarnessAnnotatedMaintainer,
			Build: func() []standingHarnessSigned {
				c, decision := accepted()
				return []standingHarnessSigned{h.SignFactFor("M", c, func(event *Event) {
					event.Evidence, event.Rule = []string{decision.ID}, "nosuch"
				})}
			}},
		{ID: "B3", Signer: "M",
			WithRulePath: standingHarnessAnnotatedMaintainer, WithoutRulePath: standingHarnessAnnotatedMaintainer,
			Build: func() []standingHarnessSigned {
				c, decision := accepted()
				return []standingHarnessSigned{h.SignFactFor("M", c, func(event *Event) {
					event.Evidence, event.Rule = []string{decision.ID, c.Approval.ID}, "ordinary"
				})}
			}},
		{ID: "B4", Signer: "E", WithRulePath: standingHarnessRefused, WithoutRulePath: standingHarnessRefused,
			Build: func() []standingHarnessSigned {
				candidate := h.ReadyCandidate(nil)
				f01 := standingHarnessDefects(h, candidate)["F01"]
				return f01.Build(f01.Prepare())
			}},
	}
}

// standingHarnessStored reads signed's event and signature back from its
// chain commit, requires the stored bytes to be the signed ones, and
// verifies them with verifyEvent.
func standingHarnessStored(t *testing.T, h *standingHarness, signed standingHarnessSigned) (Event, error) {
	t.Helper()
	payload := runOperationalGitBytes(t, h.Work(signed.Role), "show", signed.Commit+":event.json")
	if !bytes.Equal(payload, signed.Payload) {
		t.Fatalf("chain commit %s holds\n%s\nwant\n%s", signed.Commit, payload, signed.Payload)
	}
	encoded := runOperationalGit(t, h.Work(signed.Role), "show", signed.Commit+":signature")
	signature, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("signature of %s: %v", signed.Commit, err)
	}
	if operationalDigest(payload) != signed.ID {
		t.Fatalf("event %s is stored with digest %s", signed.ID, operationalDigest(payload))
	}
	event, _, err := verifyEvent(payload, signature)
	return event, err
}

// standingHarnessCount counts the events in role's clone that match.
func standingHarnessCount(h *standingHarness, role string, match func(standingHarnessFact) bool) int {
	h.t.Helper()
	count := 0
	for _, event := range h.AllEvents(role) {
		if match(event) {
			count++
		}
	}
	return count
}

// T030 steps 1, 2, 4 and 5 on one default harness.
func TestStandingHarnessSelfTest(t *testing.T) {
	started := time.Now()
	h := newStandingHarness(t, standingHarnessDefaultPolicy())
	t.Logf("harness with seven clones built in %s", time.Since(started).Round(time.Millisecond))

	// Step 1: distinct identities, the policy on main and main everywhere.
	owners := map[string]string{}
	for _, role := range standingHarnessRoles {
		actor := h.Actor(role)
		if !validActorFingerprint(actor) {
			t.Fatalf("role %s has invalid fingerprint %q", role, actor)
		}
		if other, duplicate := owners[actor]; duplicate {
			t.Fatalf("roles %s and %s share fingerprint %s", other, role, actor)
		}
		owners[actor] = role
		if main := h.git(role, "rev-parse", "main"); main != h.Base() {
			t.Fatalf("%s's main is %s, want %s", role, main, h.Base())
		}
	}
	if main := runOperationalGit(t, h.Remote(), "rev-parse", "main"); main != h.Base() {
		t.Fatalf("remote main is %s, want %s", main, h.Base())
	}
	if digest, want := h.PolicyDigest(h.Base()), operationalDigest([]byte(h.Policy().JSON(h.actors))); digest != want {
		t.Fatalf("policy on main has digest %s, want %s", digest, want)
	}
	shown := h.hn("M", "policy", "show")
	assertOperationalContains(t, shown, "Rule ordinary;", "Rule other;", h.Actor("E"), h.Actor("X"))

	// Step 2: the US1 starting point, seen from E's clone.
	step := time.Now()
	candidate := h.ReadyCandidate(nil)
	t.Logf("ReadyCandidate took %s", time.Since(step).Round(time.Millisecond))
	h.hn("E", "sync", "origin")
	status := h.hn("E", "proposal", "status", candidate.ID)
	assertOperationalContains(t, status, "Reviews:  1/1 trusted approvals", "Pipeline: test 1/1 trusted passes", "Status:   ready")
	c := h.Case(candidate.ID)
	approvals := standingHarnessCount(h, "E", func(event standingHarnessFact) bool {
		return event.Kind == "review.submit" && event.Subject == candidate.ID && event.Verdict == "approve"
	})
	results := standingHarnessCount(h, "E", func(event standingHarnessFact) bool {
		return event.Kind == "run.result" && event.Subject == c.Request.ID && event.Outcome == "passed"
	})
	decisions := standingHarnessCount(h, "E", func(event standingHarnessFact) bool {
		return event.Kind == "proposal.decision" && event.Subject == candidate.ID
	})
	if approvals != 1 || results != 1 || decisions != 0 {
		t.Fatalf("E sees %d approvals, %d passing results and %d decisions, want 1, 1 and 0", approvals, results, decisions)
	}
	if c.Request.Subject != candidate.ID || c.Request.Commit != candidate.Head {
		t.Fatalf("run request %#v does not bind candidate %s at %s", c.Request, candidate.ID, candidate.Head)
	}

	// Step 4: a valid rule merge fact on E's chain, read back with its rule.
	// Nothing projects E's clone afterwards: until WP07 lands, projection
	// runs only the maintainer check and refuses this fact (FR-025).
	before := len(h.Events("E"))
	factID := h.SignFact("E", nil)
	events := h.Events("E")
	if len(events) != before+1 || events[len(events)-1].ID != factID || events[len(events)-1].Kind != "proposal.merged" {
		t.Fatalf("SignFact appended %d events to E's chain, last %#v, want one proposal.merged %s", len(events)-before, events[len(events)-1], factID)
	}
	facts := h.MergeFacts("E", candidate.ID)
	if len(facts) != 1 || facts[0].ID != factID {
		t.Fatalf("MergeFacts found %d facts, want %s", len(facts), factID)
	}
	fact := facts[0]
	if fact.Rule != "ordinary" || fact.Actor != h.Actor("E") || fact.Head != candidate.Head || fact.Policy != h.PolicyDigest(h.Base()) ||
		strings.Join(fact.Evidence, ",") != strings.Join(c.Evidence(), ",") {
		t.Fatalf("rule merge fact %+v does not carry rule ordinary, E, the head, the base policy and evidence %v", fact, c.Evidence())
	}
	if !bytes.HasSuffix(fact.Payload, []byte(`,"rule":"ordinary"}`)) {
		t.Fatalf("rule is not the last key of the signed fact:\n%s", fact.Payload)
	}

	// Step 5: SignRaw stores the exact bytes in a chain the existing reader
	// and the CLI can read.
	first := h.NextEvent("S", "issue.open")
	first.Title = "Raw signed issue"
	canonical, err := marshalEvent(first)
	if err != nil {
		t.Fatal(err)
	}
	payload := standingEventWithMembers(canonical, `"note":"bytes Hubnot never writes"`)
	rawID := h.SignRaw("S", payload)
	ref := operationalActorRef(h.Actor("S"))
	if stored := runOperationalGitBytes(t, h.Work("S"), "show", ref+":event.json"); !bytes.Equal(stored, payload) || rawID != operationalDigest(payload) {
		t.Fatalf("SignRaw stored\n%s\nwith ID %s, want\n%s", stored, rawID, payload)
	}
	firstCommit := h.git("S", "rev-parse", ref)
	second := h.NextEvent("S", "issue.open")
	second.Title = "Second raw signed issue"
	if second.Sequence != first.Sequence+1 || second.Previous != rawID {
		t.Fatalf("event after a raw one has sequence %d and previous %s, want %d and %s", second.Sequence, second.Previous, first.Sequence+1, rawID)
	}
	secondPayload, err := marshalEvent(second)
	if err != nil {
		t.Fatal(err)
	}
	secondID := h.SignRaw("S", secondPayload)
	if parent := h.git("S", "rev-parse", ref+"^"); parent != firstCommit {
		t.Fatalf("raw chain commit has parent %s, want %s", parent, firstCommit)
	}
	h.inClone("S", func(*Identity) {
		stored, err := loadStoredEvent(firstCommit)
		if err != nil || stored.ID != rawID || !bytes.Equal(stored.Payload, payload) {
			t.Fatalf("loadStoredEvent(%s) = %v, %v; want %s", firstCommit, stored, err, rawID)
		}
	})
	assertOperationalContains(t, h.hn("S", "log"), operationalShortID(rawID), operationalShortID(secondID))
	t.Logf("self-test took %s", time.Since(started).Round(time.Millisecond))
}

// T030 step 3: adoption through a maintainer amendment.
func TestStandingHarnessAdoption(t *testing.T) {
	started := time.Now()
	h := newStandingHarness(t, standingHarnessSeedPolicy())
	assertOperationalContains(t, h.hn("M", "policy", "show"), "Automerge: disabled")
	before := h.Base()
	eventsBefore := len(h.Events("M"))

	amendment := h.AdoptThroughAmendment(standingHarnessDefaultPolicy())

	after := h.Base()
	parents := strings.Fields(h.git("M", "rev-list", "--parents", "-n", "1", after))
	if len(parents) != 3 || parents[1] != before {
		t.Fatalf("main advanced to %s with parents %v, want a merge commit on %s", after, parents[1:], before)
	}
	if main := runOperationalGit(t, h.Remote(), "rev-parse", "main"); main != after {
		t.Fatalf("remote main is %s, want %s", main, after)
	}
	for _, role := range standingHarnessRoles {
		if main := h.git(role, "rev-parse", "main"); main != after {
			t.Fatalf("%s's main is %s, want %s", role, main, after)
		}
	}
	var decisions, merges []operationalEvent
	for _, event := range h.Events("M")[eventsBefore:] {
		switch event.Kind {
		case "proposal.decision":
			decisions = append(decisions, event)
		case "proposal.merged":
			merges = append(merges, event)
		}
	}
	if len(decisions) != 1 || len(merges) != 1 || decisions[0].Subject != amendment || merges[0].Subject != amendment {
		t.Fatalf("M's chain gained decisions %v and merge facts %v, want one of each for %s", decisions, merges, amendment)
	}
	if verdict := h.fact("M", decisions[0].ID).Verdict; verdict != "accept" {
		t.Fatalf("amendment decision verdict is %q, want accept", verdict)
	}
	if merge := h.fact("M", merges[0].ID); merge.Commit != after || merge.Rule != "" ||
		strings.Join(merge.Evidence, ",") != decisions[0].ID {
		t.Fatalf("amendment merge fact %+v, want an acceptance merge fact at %s citing %s", merge, after, decisions[0].ID)
	}
	assertOperationalContains(t, h.hn("M", "policy", "show"), "Rule ordinary;", "Rule other;")
	if digest, want := h.PolicyDigest(after), operationalDigest([]byte(standingHarnessDefaultPolicy().JSON(h.actors))); digest != want {
		t.Fatalf("adopted policy digest %s, want %s", digest, want)
	}
	t.Logf("adoption took %s", time.Since(started).Round(time.Millisecond))
}

// T030 step 7 (T091): every legacy and boundary fixture builds once in a
// fresh harness and gives its declared verifyEvent outcome.
func TestStandingHarnessLegacyAndBoundaryFixtures(t *testing.T) {
	binary := buildOperationalBinary(t)
	// claims is the canonical rule claim verifyEvent reports per fixture;
	// every other signed event carries none.
	claims := map[string]string{"L03": "nosuch", "L04": "maint", "B1": "ordinary", "B2": "nosuch", "B3": "ordinary", "B4": "nosuch"}
	check := func(t *testing.T, h *standingHarness, fixtures []standingHarnessFixture, ids []string) []standingHarnessSigned {
		t.Helper()
		var all []standingHarnessSigned
		if len(fixtures) != len(ids) {
			t.Fatalf("%d fixtures, want %v", len(fixtures), ids)
		}
		for index, fixture := range fixtures {
			started := time.Now()
			if fixture.ID != ids[index] {
				t.Fatalf("fixture %d is %s, want %s", index, fixture.ID, ids[index])
			}
			if fixture.Verdict == "" && fixture.WithRulePath == "" {
				t.Fatalf("%s declares no verdict", fixture.ID)
			}
			signed := fixture.Build()
			if len(signed) == 0 {
				t.Fatalf("%s built nothing", fixture.ID)
			}
			for _, item := range signed {
				if item.Role != fixture.Signer {
					t.Fatalf("%s signed by %s, want %s", fixture.ID, item.Role, fixture.Signer)
				}
				event, err := standingHarnessStored(t, h, item)
				if fixture.VerifyError != "" {
					if err == nil || err.Error() != fixture.VerifyError {
						t.Fatalf("%s: verifyEvent error %v, want %q\n%s", fixture.ID, err, fixture.VerifyError, item.Payload)
					}
					if _, _, err := verifyEvent(item.Unannotated, h.signBytes(fixture.Signer, item.Unannotated)); err != nil {
						t.Fatalf("%s without its rule key does not verify: %v", fixture.ID, err)
					}
					continue
				}
				if err != nil {
					t.Fatalf("%s: verifyEvent refused %s: %v\n%s", fixture.ID, item.ID, err, item.Payload)
				}
				if event.Rule != claims[fixture.ID] {
					t.Fatalf("%s: rule claim %q, want %q\n%s", fixture.ID, event.Rule, claims[fixture.ID], item.Payload)
				}
				if event.Actor != h.Actor(fixture.Signer) {
					t.Fatalf("%s: signed by %s, want %s", fixture.ID, event.Actor, h.Actor(fixture.Signer))
				}
			}
			t.Logf("%s built %d signed event(s) in %s", fixture.ID, len(signed), time.Since(started).Round(time.Millisecond))
			all = append(all, signed...)
		}
		return all
	}

	t.Run("legacy", func(t *testing.T) {
		h := newStandingHarnessWithBinary(t, binary, standingHarnessLegacyPolicy())
		signed := check(t, h, standingHarnessLegacyFixtures(h), []string{"L01", "L02", "L03", "L04", "L05", "L06"})
		// L01 to L05 are valid history the current client projects: R
		// publishes L01 and M's clone reads every one of them. S, which
		// holds the refused L06, publishes nothing.
		h.Publish("R")
		h.hn("M", "sync", "origin")
		log := h.hn("M", "log")
		for _, item := range signed {
			if item.Role != "S" {
				assertOperationalContains(t, log, operationalShortID(item.ID))
			}
		}
	})
	t.Run("boundary", func(t *testing.T) {
		h := newStandingHarnessWithBinary(t, binary, standingHarnessDefaultPolicy())
		fixtures := standingHarnessBoundaryFixtures(h)
		signed := check(t, h, fixtures, []string{"B1", "B2", "B3", "B4"})
		// B2 and B3 are maintainer merges the current client already
		// projects. B1 and B4 stay on E's unpublished chain.
		log := h.hn("M", "log")
		for _, item := range signed {
			if item.Role == "M" {
				assertOperationalContains(t, log, operationalShortID(item.ID))
			}
		}
		for _, fixture := range fixtures {
			if fixture.NeedsRulePath != (fixture.ID == "B1") {
				t.Fatalf("%s needs path (b) = %v", fixture.ID, fixture.NeedsRulePath)
			}
		}
	})
}

// The defect table builds every row of the fixture matrix and its control.
func TestStandingHarnessDefectTable(t *testing.T) {
	if testing.Short() {
		t.Skip("builds about twenty candidates for the fixture matrix")
	}
	binary := buildOperationalBinary(t)
	want := map[string]string{
		"F01": "rule-exists", "F02": "rule-acceptance", "F03": "rule-signer", "F04": "policy-digest",
		"F05": "evidence-item", "F06": "evidence-item", "F07": "evidence-item", "F08": "evidence-item",
		"F09": "evidence-item", "F10": "evidence-item", "F11": "evidence-item", "F12": "evidence-duplicate",
		"F13": "approval-threshold", "F14": "pipeline-threshold", "F15": "rule-author", "F16": "changed-paths",
		"F17": "manual-path", "F18": "manual-path", "F19": standingHarnessMaintainerPath, "F20": standingHarnessMaintainerPath,
		"F21": standingHarnessMissingDependency, "F22": standingHarnessMissingDependency,
	}
	claims := map[string]string{"F01": "nosuch", "F02": "maint", "F19": "", "F20": ""}
	shapes := map[string]int{"F19": 2, "F20": 5}
	build := func(t *testing.T, h *standingHarness, defect standingHarnessDefect) (standingHarnessCase, []standingHarnessSigned) {
		t.Helper()
		started := time.Now()
		if defect.Expected != want[defect.ID] {
			t.Fatalf("%s expects %q, want %q", defect.ID, defect.Expected, want[defect.ID])
		}
		if wantSigner := map[bool]string{true: "X", false: "E"}[defect.ID == "F03"]; defect.Signer != wantSigner {
			t.Fatalf("%s is signed by %s, want %s", defect.ID, defect.Signer, wantSigner)
		}
		c := defect.Prepare()
		signed := defect.Build(c)
		wantShapes := shapes[defect.ID]
		if wantShapes == 0 {
			wantShapes = 1
		}
		if len(signed) != wantShapes {
			t.Fatalf("%s signed %d facts, want %d", defect.ID, len(signed), wantShapes)
		}
		wantClaim, named := claims[defect.ID]
		if !named {
			wantClaim = "ordinary"
		}
		for _, item := range signed {
			event, err := standingHarnessStored(t, h, item)
			if err != nil {
				t.Fatalf("%s: verifyEvent refused %s: %v\n%s", defect.ID, item.ID, err, item.Payload)
			}
			if event.Kind != "proposal.merged" || event.Subject != c.Candidate.ID || event.Head != c.Candidate.Head ||
				event.Actor != h.Actor(defect.Signer) || event.Rule != wantClaim {
				t.Fatalf("%s: fact %+v does not bind candidate %s, signer %s and rule %q", defect.ID, event, c.Candidate.ID, defect.Signer, wantClaim)
			}
		}
		if defect.Control == nil {
			if defect.ID != "F21" && defect.ID != "F22" {
				t.Fatalf("semantic row %s has no control", defect.ID)
			}
		} else {
			control := defect.Control(c)
			event, err := standingHarnessStored(t, h, control)
			if err != nil || event.Actor != h.Actor("E") || event.Rule != "ordinary" {
				t.Fatalf("%s control %+v: %v", defect.ID, event, err)
			}
			rebound := defect.ID == "F15" || defect.ID == "F16" || defect.ID == "F17" || defect.ID == "F18"
			if (control.Candidate != c.Candidate.ID) != rebound {
				t.Fatalf("%s control cites candidate %s, defect candidate %s", defect.ID, control.Candidate, c.Candidate.ID)
			}
			if rebound {
				controlCase := h.Case(control.Candidate)
				if controlCase.Base != c.Base || controlCase.Author != "A" {
					t.Fatalf("%s control candidate is by %s on %s, want A on %s", defect.ID, controlCase.Author, controlCase.Base, c.Base)
				}
			}
		}
		t.Logf("%s built in %s", defect.ID, time.Since(started).Round(time.Millisecond))
		return c, signed
	}

	// Each hn command verifies every event in the clone, so a harness slows
	// as its chains grow. The fact-only rows share one harness; every row
	// that builds its own candidates gets a fresh one, the way consumers
	// that need independent chains use the table.
	factOnly := map[string]bool{"F01": true, "F03": true, "F04": true, "F13": true, "F14": true, "F19": true, "F20": true}
	t.Run("fact-only rows", func(t *testing.T) {
		h := newStandingHarnessWithBinary(t, binary, standingHarnessDefaultPolicy())
		defects := standingHarnessDefects(h, h.ReadyCandidate(nil))
		if len(defects) != len(standingHarnessDefectIDs) {
			t.Fatalf("defect table has %d rows, want %d", len(defects), len(standingHarnessDefectIDs))
		}
		for _, id := range standingHarnessDefectIDs {
			if defects[id].ID != id {
				t.Fatalf("defect table lacks %s", id)
			}
			if factOnly[id] {
				build(t, h, defects[id])
			}
		}
	})
	for _, id := range standingHarnessDefectIDs {
		if factOnly[id] {
			continue
		}
		t.Run(id, func(t *testing.T) {
			h := newStandingHarnessWithBinary(t, binary, standingHarnessDefaultPolicy())
			c, _ := build(t, h, standingHarnessDefects(h, h.ReadyCandidate(nil))[id])
			switch id {
			case "F21":
				reviewerRef := operationalActorRef(h.Actor("R"))
				published := func() bool {
					for _, commit := range strings.Fields(runOperationalGit(t, h.Remote(), "rev-list", reviewerRef)) {
						if operationalDigest(runOperationalGitBytes(t, h.Remote(), "show", commit+":event.json")) == c.Approval.ID {
							return true
						}
					}
					return false
				}
				if published() {
					t.Fatalf("F21's approval %s reached the remote before Publish", c.Approval.ID)
				}
				h.Publish("R")
				if !published() {
					t.Fatalf("Publish did not publish F21's approval %s", c.Approval.ID)
				}
			case "F22":
				runOperationalGitFailure(t, h.Remote(), "cat-file", "-e", c.Candidate.Head+"^{commit}")
				runOperationalGitFailure(t, h.Work("E"), "cat-file", "-e", c.Candidate.Head+"^{commit}")
				runOperationalGitFailure(t, h.Verifier(), "cat-file", "-e", c.Candidate.Head+"^{commit}")
			}
		})
	}
}
