package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

// WP18: closing evidence of the standing acceptance mission. These tests
// measure the import cost of rule merge facts (NFR-006), execute the
// protocol's rule merge fact example against a freshly recorded fact
// (FR-023), keep the governance guide's traceability section honest against
// the spec (SC-006), check the style of the mission's documentation, and
// back two claims of the threat model and the regression gate (NFR-007).
// The pre-existing test audit is TestStandingCompatPreExistingTestsFrozen.

// T085: NFR-006 import timing.

const (
	// standingQualityBaseEvents is the number of events the verifier holds
	// before each timed import.
	standingQualityBaseEvents = 1000
	// standingQualityImportFacts is the number of merge facts each variant's
	// chain adds on top of the base.
	standingQualityImportFacts = 20
	// standingQualityImportRuns is the number of timed imports per variant.
	standingQualityImportRuns = 20
	// standingQualityImportRatio bounds p95(rule) / p95(acceptance).
	standingQualityImportRatio = 1.5
	// standingQualityLargeCommandTimeout bounds one hn command on the
	// 1,000-event verifier. Such a command takes about 25 seconds on an idle
	// gate host, so the 90 seconds of the harness leave too little room
	// under the load of the full race suite.
	standingQualityLargeCommandTimeout = 10 * time.Minute
)

// The two import variants of NFR-006.
const (
	standingQualityRuleVariant       = "rule"
	standingQualityAcceptanceVariant = "acceptance"
)

// standingQualityAppend runs one hn command in role's clone, without the
// sync and publish steps of the harness helpers, and returns the event it
// appended to role's chain, which must be of kind.
func standingQualityAppend(h *standingHarness, role, kind string, args ...string) operationalEvent {
	h.t.Helper()
	h.hn(role, args...)
	event := h.latest(role)
	if event.Kind != kind {
		h.t.Fatalf("hn %s in %s's clone appended %s, want %s", strings.Join(args, " "), role, event.Kind, kind)
	}
	return event
}

// standingQualityChainEvents is the number of events on every actor chain
// under refPrefix in repository: each event is one commit of its chain.
func standingQualityChainEvents(t *testing.T, repository, refPrefix string) int {
	t.Helper()
	output := runOperationalGit(t, repository, "rev-list", "--count", "--glob="+refPrefix+"*")
	var count int
	if _, err := fmt.Sscan(output, &count); err != nil {
		t.Fatalf("rev-list --count printed %q", output)
	}
	return count
}

// standingQualityImportFixture is the NFR-006 fixture: a verifier holding
// the base events and, per variant, a bare remote that holds the base plus
// one signer chain extended by standingQualityImportFacts merge facts.
type standingQualityImportFixture struct {
	h         *standingHarness
	cases     []standingHarnessCase
	decisions []operationalEvent  // M's accept decision of each case
	verifier  string              // the live verifier every run restores
	templates map[string]string   // variant -> verifier template pointed at the variant's remote
	signers   map[string]string   // variant -> role whose chain the variant extends
	heads     map[string]string   // variant -> signer chain head holding the merge facts
	remotes   map[string]string   // variant -> bare remote
	facts     map[string][]string // variant -> merge fact IDs
}

// standingQualityPad appends count events to role's chain in-process and
// publishes the chain with a raw push: an issue every fourth event and
// comments on it in between, and for S first an approval of each case.
func standingQualityPad(h *standingHarness, role string, count int, cases []standingHarnessCase) {
	h.t.Helper()
	h.inClone(role, func(identity *Identity) {
		issue := ""
		for index := range count {
			var kind string
			switch {
			case role == "S" && index < len(cases):
				kind = "review.submit"
			case issue == "" || index%4 == 0:
				kind = "issue.open"
			default:
				kind = "issue.comment"
			}
			event, err := nextEvent(identity, kind)
			if err != nil {
				h.t.Fatal(err)
			}
			switch kind {
			case "review.submit":
				event.Subject, event.Verdict = cases[index].Candidate.ID, "approve"
				event.Body = "Approved by the NFR-006 fixture."
			case "issue.open":
				event.Title = fmt.Sprintf("NFR-006 fixture issue %s-%d", role, index)
			default:
				event.Subject, event.Body = issue, fmt.Sprintf("NFR-006 fixture comment %s-%d", role, index)
			}
			stored, err := appendEvent(event, identity)
			if err != nil {
				h.t.Fatalf("append %s to %s's chain: %v", kind, role, err)
			}
			if kind == "issue.open" {
				issue = stored.ID
			}
		}
	})
	ref := actorRef(h.Actor(role))
	h.git(role, "push", "-q", "origin", ref+":"+ref)
}

// newStandingQualityImportFixture builds the NFR-006 fixture once. Twenty
// candidates by A each get R's approval, T's passing result of M's request
// and M's accept decision through the hn CLI; every role's chain is then
// padded in-process so the remote holds exactly 1,000 events. The rule
// variant extends E's chain with one rule merge fact per candidate, citing
// the approval and the result; the acceptance variant extends M's chain
// with one acceptance merge fact per candidate, citing the decision. Both
// facts of a candidate record commit equal to the head, a fast-forward, so
// the two chains have the same shape and differ only in how the facts are
// authorized. The verifier imports the base once.
func newStandingQualityImportFixture(t *testing.T) *standingQualityImportFixture {
	t.Helper()
	h := newStandingHarness(t, standingHarnessDefaultPolicy())
	f := &standingQualityImportFixture{
		h:         h,
		templates: map[string]string{},
		signers:   map[string]string{standingQualityRuleVariant: "E", standingQualityAcceptanceVariant: "M"},
		heads:     map[string]string{},
		remotes:   map[string]string{},
		facts:     map[string][]string{},
	}

	heads := make([]string, 0, standingQualityImportFacts)
	for index := range standingQualityImportFacts {
		heads = append(heads, h.Commit("A", h.Base(), map[string]string{
			fmt.Sprintf("src/import-%02d.txt", index): fmt.Sprintf("NFR-006 import change %d\n", index),
		}))
	}
	h.hn("A", "sync", "origin")
	for index, head := range heads {
		candidate := standingQualityAppend(h, "A", "proposal.open", "proposal", "open", "--base", h.Base(), "--head", head,
			"--body", "Opened by the NFR-006 fixture.", fmt.Sprintf("Import timing candidate %02d", index))
		f.cases = append(f.cases, standingHarnessCase{Base: h.Base(), Author: "A", Candidate: candidate})
	}
	h.Publish("A")
	h.hn("R", "sync", "origin")
	for index := range f.cases {
		f.cases[index].Approval = standingQualityAppend(h, "R", "review.submit", "review", f.cases[index].Candidate.ID,
			"--approve", "--body", "Approved by the NFR-006 fixture.")
	}
	h.Publish("R")
	h.hn("M", "sync", "origin")
	for index := range f.cases {
		f.cases[index].Request = standingQualityAppend(h, "M", "run.request", "run", "request", f.cases[index].Candidate.ID, standingHarnessPipeline)
	}
	h.Publish("M")
	h.hn("T", "sync", "origin")
	for index := range f.cases {
		f.cases[index].Result = standingQualityAppend(h, "T", "run.result", "run", "execute", f.cases[index].Request.ID,
			"--backend", "host", "--allow-unsafe-host-execution")
		if outcome := h.fact("T", f.cases[index].Result.ID).Outcome; outcome != "passed" {
			t.Fatalf("run result %s is %q, want passed", f.cases[index].Result.ID, outcome)
		}
	}
	h.Publish("T")
	h.hn("M", "sync", "origin")
	for index := range f.cases {
		f.decisions = append(f.decisions, standingQualityAppend(h, "M", "proposal.decision", "decide", f.cases[index].Candidate.ID,
			"--accept", "--body", "Accepted by the NFR-006 fixture."))
	}
	h.Publish("M")

	remaining := standingQualityBaseEvents - standingQualityChainEvents(t, h.Remote(), "refs/hn/actors/")
	if remaining < len(standingHarnessRoles)+standingQualityImportFacts {
		t.Fatalf("the candidates produced %d events; no room to pad to %d", standingQualityBaseEvents-remaining, standingQualityBaseEvents)
	}
	for index, role := range standingHarnessRoles {
		count := remaining / len(standingHarnessRoles)
		if index == 0 {
			count += remaining % len(standingHarnessRoles)
		}
		standingQualityPad(h, role, count, f.cases)
	}
	if count := standingQualityChainEvents(t, h.Remote(), "refs/hn/actors/"); count != standingQualityBaseEvents {
		t.Fatalf("the base remote holds %d events, want %d", count, standingQualityBaseEvents)
	}

	root := t.TempDir()
	for _, variant := range []string{standingQualityRuleVariant, standingQualityAcceptanceVariant} {
		f.remotes[variant] = filepath.Join(root, variant+".git")
		runOperationalGit(t, "", "clone", "-q", "--mirror", "--no-local", h.Remote(), f.remotes[variant])
	}
	for index, c := range f.cases {
		f.facts[standingQualityRuleVariant] = append(f.facts[standingQualityRuleVariant], h.SignFactFor("E", c, nil).ID)
		decision := f.decisions[index].ID
		f.facts[standingQualityAcceptanceVariant] = append(f.facts[standingQualityAcceptanceVariant], h.SignFactFor("M", c, func(event *Event) {
			event.Rule = ""
			event.Evidence = []string{decision}
		}).ID)
	}
	for variant, role := range f.signers {
		ref := actorRef(h.Actor(role))
		h.git(role, "push", "-q", f.remotes[variant], ref+":"+ref)
		f.heads[variant] = h.git(role, "rev-parse", ref)
		if count := standingQualityChainEvents(t, f.remotes[variant], "refs/hn/actors/"); count != standingQualityBaseEvents+standingQualityImportFacts {
			t.Fatalf("the %s remote holds %d events, want %d", variant, count, standingQualityBaseEvents+standingQualityImportFacts)
		}
	}

	f.verifier = h.Verifier()
	standingQualityLargeCommand(t, h.Binary(), f.verifier, "sync", "origin")
	if count := standingQualityChainEvents(t, f.verifier, "refs/hn/remotes/origin/actors/"); count != standingQualityBaseEvents {
		t.Fatalf("the verifier accepted %d base events, want %d", count, standingQualityBaseEvents)
	}
	for variant, remote := range f.remotes {
		runOperationalGit(t, f.verifier, "remote", "set-url", "origin", remote)
		f.templates[variant] = filepath.Join(root, "verifier-"+variant)
		serveQualityCopyTree(t, f.verifier, f.templates[variant])
	}
	return f
}

// standingQualityLargeCommand runs hn in directory with the environment of
// the harness and standingQualityLargeCommandTimeout, fails the test unless
// it succeeds, and returns its output and the duration of the command alone.
func standingQualityLargeCommand(t *testing.T, binary, directory string, args ...string) (string, time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), standingQualityLargeCommandTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, binary, args...)
	command.Dir = directory
	command.Env = operationalTestEnvironment(t.TempDir())
	command.Stdin = strings.NewReader("")

	started := time.Now()
	output, err := command.CombinedOutput()
	elapsed := time.Since(started)

	if ctx.Err() != nil {
		t.Fatalf("hn %s timed out after %s: %v\n%s", strings.Join(args, " "), standingQualityLargeCommandTimeout, ctx.Err(), output)
	}
	if err != nil {
		t.Fatalf("hn %s in %s: %v\n%s", strings.Join(args, " "), directory, err, output)
	}
	return string(output), elapsed
}

// timedImport restores the verifier from variant's template, runs hn sync
// origin in it and returns the duration of that command alone. It requires
// the import to promote the variant's whole chain.
func (f *standingQualityImportFixture) timedImport(t *testing.T, variant string, run int) time.Duration {
	t.Helper()
	serveQualityRestoreTree(t, f.templates[variant], f.verifier)
	output, elapsed := standingQualityLargeCommand(t, f.h.Binary(), f.verifier, "sync", "origin")
	if strings.Contains(output, ": failed (") {
		t.Fatalf("run %d: hn sync of the %s variant reported a failure:\n%s", run, variant, output)
	}
	accepted := runOperationalGit(t, f.verifier, "for-each-ref", "--format=%(objectname)", acceptedActorRef("origin", f.h.Actor(f.signers[variant])))
	if accepted != f.heads[variant] {
		t.Fatalf("run %d: the verifier accepted %s's chain at %q, want the %s variant's %s:\n%s",
			run, f.signers[variant], accepted, variant, f.heads[variant], output)
	}
	return elapsed
}

// requireMerged requires the verifier, as the latest run left it, to report
// one candidate merged through the variant's fact.
func (f *standingQualityImportFixture) requireMerged(t *testing.T, variant string) {
	t.Helper()
	status, _ := standingQualityLargeCommand(t, f.h.Binary(), f.verifier, "proposal", "status", f.cases[0].Candidate.ID)
	want := "Status:   merged\n"
	if variant == standingQualityRuleVariant {
		want += fmt.Sprintf("Merged by rule ordinary, signed by %s in fact %s\n", f.h.Actor("E"), f.facts[variant][0])
	}
	assertOperationalContains(t, status, want)
}

// standingQualityDurations renders samples rounded to milliseconds.
func standingQualityDurations(samples []time.Duration) string {
	labels := make([]string, len(samples))
	for index, sample := range samples {
		labels[index] = sample.Round(time.Millisecond).String()
	}
	return strings.Join(labels, " ")
}

// NFR-006: importing an actor chain with 20 rule merge facts into a
// repository holding 1,000 events takes at most 1.5 times as long as
// importing the same chain shape with 20 acceptance merge facts, at the
// 95th percentile over 20 runs. Each run restores the verifier from the
// variant's template, so every sample starts from the same 1,000 events,
// and the variants alternate run by run to spread host noise. Restoration
// is not timed. The hn binary is built by buildOperationalBinary with plain
// go build, so the race detector does not distort the samples.
func TestStandingQualityImportTimeNFR006(t *testing.T) {
	if testing.Short() {
		t.Skip("NFR-006 timing builds a 1,000-event repository and imports 40 times; it runs in the full gate without -short")
	}
	started := time.Now()
	f := newStandingQualityImportFixture(t)
	t.Logf("NFR-006 fixture built in %s", time.Since(started).Round(time.Millisecond))

	samples := map[string][]time.Duration{}
	order := []string{standingQualityRuleVariant, standingQualityAcceptanceVariant}
	for run := range standingQualityImportRuns {
		for _, variant := range order {
			samples[variant] = append(samples[variant], f.timedImport(t, variant, run))
			if run == 0 {
				f.requireMerged(t, variant)
			}
		}
		slices.Reverse(order)
	}

	rule := serveQualityPercentile95(samples[standingQualityRuleVariant])
	acceptance := serveQualityPercentile95(samples[standingQualityAcceptanceVariant])
	ratio := float64(rule) / float64(acceptance)
	t.Logf("NFR-006 rule merge fact imports over %d runs: %s", standingQualityImportRuns, standingQualityDurations(samples[standingQualityRuleVariant]))
	t.Logf("NFR-006 acceptance merge fact imports over %d runs: %s", standingQualityImportRuns, standingQualityDurations(samples[standingQualityAcceptanceVariant]))
	t.Logf("NFR-006 95th percentiles: rule %s, acceptance %s, ratio %.3f (limit %.1f)",
		rule.Round(time.Millisecond), acceptance.Round(time.Millisecond), ratio, standingQualityImportRatio)
	if float64(rule) > standingQualityImportRatio*float64(acceptance) {
		t.Fatalf("NFR-006: the 95th percentile rule import %s exceeds %.1f times the acceptance import %s (ratio %.3f)",
			rule, standingQualityImportRatio, acceptance, ratio)
	}
}

// T086: the standing-rule-merge-fact example of docs/protocol-v0.md.

// standingQualityRuleMergeMarker introduces the documented rule merge fact.
const standingQualityRuleMergeMarker = "<!-- example: standing-rule-merge-fact -->"

// standingQualityObject decodes one JSON object and returns its top-level
// keys in document order with their raw values. It refuses a repeated key.
func standingQualityObject(data []byte) ([]string, map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil, nil, fmt.Errorf("not a JSON object: %v", err)
	}
	var keys []string
	values := map[string]json.RawMessage{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, nil, err
		}
		key, ok := token.(string)
		if !ok {
			return nil, nil, fmt.Errorf("object key %v is not a string", token)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, nil, err
		}
		if _, repeated := values[key]; repeated {
			return nil, nil, fmt.Errorf("key %q appears twice", key)
		}
		keys = append(keys, key)
		values[key] = value
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, nil, fmt.Errorf("unterminated JSON object: %v", err)
	}
	return keys, values, nil
}

// standingQualityExampleJSON is the first json fence after marker in doc.
func standingQualityExampleJSON(doc, marker string) (string, error) {
	_, after, found := strings.Cut(doc, marker)
	if !found {
		return "", fmt.Errorf("no %s marker", marker)
	}
	_, block, found := strings.Cut(after, "```json\n")
	if !found {
		return "", fmt.Errorf("no json block after %s", marker)
	}
	block, _, found = strings.Cut(block, "\n```")
	if !found {
		return "", fmt.Errorf("unterminated json block after %s", marker)
	}
	return block, nil
}

// standingQualityString decodes a raw JSON string value.
func standingQualityString(t *testing.T, name string, raw json.RawMessage) string {
	t.Helper()
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("%s is %s, want a JSON string", name, raw)
	}
	return value
}

// FR-023: the documented rule merge fact has the shape of a fact the client
// records. The local User Story 1 flow runs on the harness, and E's latest
// event, read with git show refs/hn/actors/<E>:event.json, must have the
// documented keys in the documented order, the documented protocol, kind
// and rule, the base policy digest, the sorted full IDs of the approval and
// the passing result as evidence, the merge commit as commit, and E's
// previous event as previous.
func TestStandingQualityDocumentedRuleMergeFact(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("docs", "protocol-v0.md"))
	if err != nil {
		t.Fatal(err)
	}
	example, err := standingQualityExampleJSON(string(contents), standingQualityRuleMergeMarker)
	if err != nil {
		t.Fatalf("docs/protocol-v0.md: %v", err)
	}
	documentedKeys, documented, err := standingQualityObject([]byte(example))
	if err != nil {
		t.Fatalf("docs/protocol-v0.md rule merge fact example: %v\n%s", err, example)
	}

	h := newStandingHarness(t, standingHarnessDefaultPolicy())
	candidate := h.ReadyCandidate(standingAutomergeFiles)
	c := h.Case(candidate.ID)
	// The documented fact follows an earlier event of the executing actor,
	// so it has a previous key, which the first event of a chain omits.
	h.hn("E", "issue", "open", "--body", "Opened before the documented rule merge.", "Earlier integrator event")
	previous := h.latest("E").ID
	h.hn("E", "sync", "origin")
	run := standingAutomergeExecute(h, "E", h.Base(), candidate.ID, "ordinary")
	if run.Status != 0 {
		t.Fatalf("E's execution exited %d:\n%s", run.Status, run.Output)
	}
	payload := runOperationalGitBytes(t, h.Work("E"), "show", actorRef(h.Actor("E"))+":event.json")
	keys, recorded, err := standingQualityObject(payload)
	if err != nil {
		t.Fatalf("recorded fact: %v\n%s", err, payload)
	}
	t.Logf("recorded rule merge fact:\n%s", payload)

	if !slices.Equal(keys, documentedKeys) {
		t.Fatalf("recorded keys %q, documented keys %q", keys, documentedKeys)
	}
	if last := keys[len(keys)-1]; last != "rule" {
		t.Fatalf("the last key is %q, want rule", last)
	}
	for _, name := range []string{"protocol", "kind", "rule"} {
		if got, want := standingQualityString(t, name, recorded[name]), standingQualityString(t, name, documented[name]); got != want {
			t.Errorf("%s = %q, documented %q", name, got, want)
		}
	}
	if rule := standingQualityString(t, "rule", recorded["rule"]); rule != "ordinary" {
		t.Errorf("rule = %q, want ordinary", rule)
	}
	for name, want := range map[string]string{
		"actor":    h.Actor("E"),
		"previous": previous,
		"subject":  candidate.ID,
		"head":     candidate.Head,
		"commit":   run.After,
		"policy":   h.PolicyDigest(candidate.Base),
	} {
		if got := standingQualityString(t, name, recorded[name]); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if run.After == run.Before || run.After == candidate.Head {
		t.Errorf("commit %s is not the merge commit of the local flow (base %s, head %s)", run.After, run.Before, candidate.Head)
	}
	var evidence, documentedEvidence []string
	if err := json.Unmarshal(recorded["evidence"], &evidence); err != nil {
		t.Fatalf("evidence is %s, want a list of IDs", recorded["evidence"])
	}
	if err := json.Unmarshal(documented["evidence"], &documentedEvidence); err != nil {
		t.Fatalf("documented evidence is %s, want a list", documented["evidence"])
	}
	want := []string{c.Approval.ID, c.Result.ID}
	sort.Strings(want)
	if !slices.Equal(evidence, want) || len(evidence) != len(documentedEvidence) {
		t.Errorf("evidence = %q, want the sorted approval and result %q, as many items as documented (%d)", evidence, want, len(documentedEvidence))
	}
	for _, id := range evidence {
		if !validEventID(id) {
			t.Errorf("evidence item %q is not a full event ID", id)
		}
	}
	if !sort.StringsAreSorted(evidence) {
		t.Errorf("evidence %q is not sorted", evidence)
	}
}

// T087: the "Standing acceptance traceability" section of
// docs/governance-v0.md.

const (
	standingQualityTraceabilityHeading = "## Standing acceptance traceability"
	standingQualitySpecPath            = "kitty-specs/standing-acceptance-01M5VD2K/spec.md"
)

var (
	standingQualitySpecRequirement = regexp.MustCompile(`(?m)^\| ((?:FR|NFR)-\d{3}) \|`)
	standingQualitySpecCriterion   = regexp.MustCompile(`(?m)^- \*\*(SC-\d{3})\*\*:`)
	standingQualitySpecStory       = regexp.MustCompile(`(?m)^### User Story (\d+):`)
	standingQualitySpecScenario    = regexp.MustCompile(`(?m)^(\d+)\. \*\*Given\*\*`)
	standingQualitySpecNextSection = regexp.MustCompile(`(?m)^#{2,3} `)
)

// standingQualitySpecIDs reads the IDs the traceability section must cover
// from the spec: every FR and NFR row of the requirement tables, every
// success criterion, and US<story>-<scenario> for every numbered acceptance
// scenario of every user story. Scenarios must be numbered 1, 2, 3 and so on
// within their story.
func standingQualitySpecIDs(spec string) ([]string, error) {
	var ids []string
	for _, match := range standingQualitySpecRequirement.FindAllStringSubmatch(spec, -1) {
		ids = append(ids, match[1])
	}
	for _, match := range standingQualitySpecCriterion.FindAllStringSubmatch(spec, -1) {
		ids = append(ids, match[1])
	}
	stories := standingQualitySpecStory.FindAllStringSubmatchIndex(spec, -1)
	for _, story := range stories {
		number := spec[story[2]:story[3]]
		body := spec[story[1]:]
		if next := standingQualitySpecNextSection.FindStringIndex(body); next != nil {
			body = body[:next[0]]
		}
		scenarios := standingQualitySpecScenario.FindAllStringSubmatch(body, -1)
		if len(scenarios) == 0 {
			return nil, fmt.Errorf("user story %s has no numbered acceptance scenario", number)
		}
		for index, scenario := range scenarios {
			if scenario[1] != fmt.Sprint(index+1) {
				return nil, fmt.Errorf("user story %s scenario %d is numbered %s", number, index+1, scenario[1])
			}
			ids = append(ids, fmt.Sprintf("US%s-%s", number, scenario[1]))
		}
	}
	for _, prefix := range []string{"FR-", "NFR-", "SC-", "US"} {
		if !slices.ContainsFunc(ids, func(id string) bool { return strings.HasPrefix(id, prefix) }) {
			return nil, fmt.Errorf("the spec defines no %s ID", prefix)
		}
	}
	return ids, nil
}

// standingQualityTraceRows parses every table of the guide's standing
// acceptance traceability section, which runs to the next level-two
// heading. The first row of a table is its header and the second its
// separator; the first cell of each data row is its ID.
func standingQualityTraceRows(guide string) ([]serveQualityTraceRow, error) {
	start := strings.Index(guide, "\n"+standingQualityTraceabilityHeading+"\n")
	if start < 0 {
		return nil, fmt.Errorf("the guide has no %q section", standingQualityTraceabilityHeading)
	}
	firstLine := strings.Count(guide[:start+1], "\n") + 1
	section := guide[start+1:]
	if end := strings.Index(section[1:], "\n## "); end >= 0 {
		section = section[:end+2]
	}
	var rows []serveQualityTraceRow
	tableLine := 0
	for index, line := range strings.Split(section, "\n") {
		if !strings.HasPrefix(line, "|") {
			tableLine = 0
			continue
		}
		tableLine++
		if tableLine <= 2 {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		row := serveQualityTraceRow{Line: firstLine + index, ID: strings.TrimSpace(cells[0]), Text: line}
		for _, match := range serveQualityTestName.FindAllStringSubmatch(line, -1) {
			row.Tests = append(row.Tests, match[1])
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("the %q section has no table rows", standingQualityTraceabilityHeading)
	}
	return rows, nil
}

// standingQualityTraceabilityProblems checks rows against the spec's IDs
// and the declared test functions: every ID has exactly one row, no row has
// an ID the spec does not define, every row names at least one test, and
// every named test is declared.
func standingQualityTraceabilityProblems(rows []serveQualityTraceRow, ids []string, declared map[string]bool) []string {
	var problems []string
	seen := map[string]int{}
	for _, row := range rows {
		seen[row.ID]++
		where := fmt.Sprintf("line %d (%s)", row.Line, row.ID)
		if !slices.Contains(ids, row.ID) {
			problems = append(problems, where+": the spec defines no such ID")
		}
		if len(row.Tests) == 0 {
			problems = append(problems, where+": names no test")
		}
		for _, name := range row.Tests {
			if !declared[name] {
				problems = append(problems, where+": "+name+" is not declared in any root *_test.go file")
			}
		}
	}
	for _, id := range ids {
		if seen[id] != 1 {
			problems = append(problems, fmt.Sprintf("%s has %d rows, want 1", id, seen[id]))
		}
	}
	sort.Strings(problems)
	return problems
}

// SC-006: the governance guide's standing acceptance traceability section
// maps every requirement, success criterion and user-story scenario of the
// spec, read at test time, to test functions that exist.
func TestStandingQualityTraceability(t *testing.T) {
	spec, err := os.ReadFile(filepath.FromSlash(standingQualitySpecPath))
	if errors.Is(err, os.ErrNotExist) {
		t.Skipf("%s is absent, as in a source archive without kitty-specs/; the traceability check needs the spec", standingQualitySpecPath)
	}
	if err != nil {
		t.Fatal(err)
	}
	ids, err := standingQualitySpecIDs(string(spec))
	if err != nil {
		t.Fatalf("%s: %v", standingQualitySpecPath, err)
	}
	guide, err := os.ReadFile(filepath.Join("docs", "governance-v0.md"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := standingQualityTraceRows(string(guide))
	if err != nil {
		t.Fatal(err)
	}
	if problems := standingQualityTraceabilityProblems(rows, ids, serveQualityDeclaredTests(t)); len(problems) > 0 {
		t.Fatalf("traceability problems in docs/governance-v0.md:\n%s", strings.Join(problems, "\n"))
	}
	t.Logf("%d traceability rows cover the %d IDs of %s", len(rows), len(ids), standingQualitySpecPath)
}

// The spec reader and the traceability check refuse gaps: a missing row, a
// duplicate row, an unknown ID, a row without a test and an undeclared test
// are each reported, rows after the section are ignored, and a story whose
// scenarios skip a number is refused.
func TestStandingQualityTraceabilityCheckRejectsGaps(t *testing.T) {
	spec := "# Spec\n\n### User Story 1: One\n\n1. **Given** a, **When** b, **Then** c.\n2. **Given** d.\n\n" +
		"### User Story 2: Two\n\n1. **Given** e.\n\n### Edge Cases\n\n3. **Given** not a scenario.\n\n" +
		"| ID | Title |\n|----|----|\n| FR-001 | One |\n| FR-002 | Two |\n| NFR-001 | Three |\n\n- **SC-001**: one.\n"
	ids, err := standingQualitySpecIDs(spec)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"FR-001", "FR-002", "NFR-001", "SC-001", "US1-1", "US1-2", "US2-1"}; !slices.Equal(ids, want) {
		t.Fatalf("spec IDs = %q, want %q", ids, want)
	}
	if _, err := standingQualitySpecIDs(strings.Replace(spec, "2. **Given** d.", "3. **Given** d.", 1)); err == nil ||
		!strings.Contains(err.Error(), "user story 1 scenario 2 is numbered 3") {
		t.Fatalf("a skipped scenario number was not refused: %v", err)
	}

	guide := "# Guide\n\n" + standingQualityTraceabilityHeading + "\n\n| Requirement | Tests |\n|----|----|\n" +
		"| FR-001 | `TestKnown` |\n" +
		"| FR-001 | `TestKnown` |\n" +
		"| NFR-001 | `TestMissing` |\n" +
		"| SC-001 | none yet |\n" +
		"| FR-099 | `TestKnown` |\n" +
		"\n| Scenario | Tests |\n|----|----|\n| US1-1 | `TestKnown` |\n| US2-1 | `TestKnown` |\n" +
		"\n## Next section\n\n| US1-2 | `TestKnown` |\n"
	rows, err := standingQualityTraceRows(guide)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 7 || rows[0].ID != "FR-001" || rows[0].Line != 7 {
		t.Fatalf("parsed rows %+v", rows)
	}
	problems := strings.Join(standingQualityTraceabilityProblems(rows, ids, map[string]bool{"TestKnown": true}), "\n")
	for _, want := range []string{
		"FR-001 has 2 rows, want 1",
		"FR-002 has 0 rows, want 1",
		"US1-2 has 0 rows, want 1",
		"line 9 (NFR-001): TestMissing is not declared in any root *_test.go file",
		"line 10 (SC-001): names no test",
		"line 11 (FR-099): the spec defines no such ID",
	} {
		if !strings.Contains(problems, want) {
			t.Errorf("problems do not include %q:\n%s", want, problems)
		}
	}
	complete := "\n" + standingQualityTraceabilityHeading + "\n\n| ID | Tests |\n|----|----|\n"
	for _, id := range ids {
		complete += "| " + id + " | `TestKnown` |\n"
	}
	rows, err = standingQualityTraceRows(complete)
	if err != nil {
		t.Fatal(err)
	}
	if problems := standingQualityTraceabilityProblems(rows, ids, map[string]bool{"TestKnown": true}); len(problems) > 0 {
		t.Fatalf("a complete section was refused:\n%s", strings.Join(problems, "\n"))
	}
}

// T089: style of the mission's documentation.

// standingQualityEmDash is U+2014, which the project's documentation does
// not use.
const standingQualityEmDash = '\u2014'

// standingQualityStyleSections are the headings of the sections this
// mission added, per document. Each one must exist, so a renamed heading
// fails the test instead of silently shrinking what it checks.
var standingQualityStyleSections = map[string][]string{
	"docs/governance-v0.md": {
		"#### Executing a standing rule",
		"### Limits of standing acceptance",
		"#### Repairing an interrupted rule merge",
		"### Standing acceptance rule fields",
		standingQualityTraceabilityHeading,
	},
	"docs/protocol-v0.md":  {"### Reason codes for rule advancement"},
	"docs/serve-v0.md":     {"#### Rerunning a rule advancement", "#### How the server judges a rule"},
	"docs/threat-model.md": {"### Residual risks of standing acceptance"},
}

// standingQualityStyleTerms mark a paragraph as standing acceptance text
// when it sits in a section that predates the mission.
var standingQualityStyleTerms = regexp.MustCompile(`(?i)\b(standing|rule merge|acceptance mode|rule claim|rule check|hn-rule|protected-rule|rule-executor)`)

// standingQualityStandingExample matches the marker of a standing example.
var standingQualityStandingExample = regexp.MustCompile(`^<!-- example: standing-[A-Za-z0-9._-]+ -->$`)

// standingQualityHeadingLevel is the level of a Markdown heading line, or 0.
func standingQualityHeadingLevel(line string) int {
	level := len(line) - len(strings.TrimLeft(line, "#"))
	if level == 0 || level > 6 || !strings.HasPrefix(line[level:], " ") {
		return 0
	}
	return level
}

// standingQualityFences reports, per line, whether it is inside or on a
// fence of a fenced code block, where # lines are not headings.
func standingQualityFences(lines []string) []bool {
	fenced := make([]bool, len(lines))
	open := false
	for index, line := range lines {
		if strings.HasPrefix(line, "```") {
			fenced[index] = true
			open = !open
			continue
		}
		fenced[index] = open
	}
	return fenced
}

// standingQualityStyleLines selects the lines of doc that hold this
// mission's text: each listed section through the next heading of the same
// or a higher level, each standing example from its marker through the end
// of the first code block after it, and each paragraph that mentions a
// standing term. It returns the selected line indexes and every listed
// heading or expected example it could not find.
func standingQualityStyleLines(doc string, headings []string) (map[int]bool, []string) {
	lines := strings.Split(doc, "\n")
	fenced := standingQualityFences(lines)
	selected := map[int]bool{}
	var missing []string
	for _, heading := range headings {
		start := slices.Index(lines, heading)
		if start < 0 {
			missing = append(missing, "heading "+heading)
			continue
		}
		level := standingQualityHeadingLevel(heading)
		for index := start; index < len(lines); index++ {
			if index > start && !fenced[index] {
				if other := standingQualityHeadingLevel(lines[index]); other > 0 && other <= level {
					break
				}
			}
			selected[index] = true
		}
	}
	for start, line := range lines {
		if !standingQualityStandingExample.MatchString(line) {
			continue
		}
		fences := 0
		for index := start; index < len(lines) && fences < 2; index++ {
			selected[index] = true
			if strings.HasPrefix(lines[index], "```") {
				fences++
			}
		}
		if fences < 2 {
			missing = append(missing, fmt.Sprintf("code block after the example marker on line %d", start+1))
		}
	}
	paragraph := []int{}
	flush := func() {
		text := ""
		for _, index := range paragraph {
			text += lines[index] + "\n"
		}
		if standingQualityStyleTerms.MatchString(text) {
			for _, index := range paragraph {
				selected[index] = true
			}
		}
		paragraph = paragraph[:0]
	}
	for index, line := range lines {
		if strings.TrimSpace(line) == "" && !fenced[index] {
			flush()
			continue
		}
		paragraph = append(paragraph, index)
	}
	flush()
	return selected, missing
}

// standingQualityStyleProblems lists each selected line of doc that holds
// an em dash, as "DOC:LINE: TEXT".
func standingQualityStyleProblems(name, doc string, selected map[int]bool) []string {
	var problems []string
	lines := strings.Split(doc, "\n")
	for _, index := range slices.Sorted(maps.Keys(selected)) {
		if strings.ContainsRune(lines[index], standingQualityEmDash) {
			problems = append(problems, fmt.Sprintf("%s:%d: %s", name, index+1, lines[index]))
		}
	}
	return problems
}

// FR-023 and the project style guide: the text this mission added to the
// governance, protocol, serve and threat-model documents, located by its
// section headings, its standing example markers and the paragraphs that
// mention standing acceptance terms, contains no em dash.
func TestStandingQualityDocsStyle(t *testing.T) {
	var problems []string
	checked := 0
	for _, name := range slices.Sorted(maps.Keys(standingQualityStyleSections)) {
		contents, err := os.ReadFile(filepath.FromSlash(name))
		if err != nil {
			t.Fatal(err)
		}
		selected, missing := standingQualityStyleLines(string(contents), standingQualityStyleSections[name])
		for _, gap := range missing {
			t.Errorf("%s: %s not found; update standingQualityStyleSections", name, gap)
		}
		if len(selected) == 0 {
			t.Errorf("%s: no standing acceptance text located", name)
		}
		checked += len(selected)
		problems = append(problems, standingQualityStyleProblems(name, string(contents), selected)...)
	}
	if len(problems) > 0 {
		t.Errorf("standing acceptance documentation contains em dashes:\n%s", strings.Join(problems, "\n"))
	}
	t.Logf("%d lines of standing acceptance documentation checked", checked)
}

// The style check finds an em dash in a listed section, in a standing
// example and in a paragraph that mentions a standing term, and ignores one
// in unrelated text and in a code line that only looks like a heading.
func TestStandingQualityDocsStyleDetectsEmDash(t *testing.T) {
	dash := string(standingQualityEmDash)
	doc := strings.Join([]string{
		"# Guide",
		"",
		"Unrelated text " + dash + " is not checked.",
		"",
		"A standing rule " + dash + " is checked.",
		"",
		"## Listed section",
		"",
		"```text",
		"# a code comment, not a heading",
		"```",
		"",
		"Inside the section " + dash + " is checked.",
		"",
		"## Other section",
		"",
		"Outside again " + dash + " is not checked.",
		"",
		"<!-- example: standing-demo -->",
		"```json",
		`{"note": "` + dash + `"}`,
		"```",
		"",
		"After the example " + dash + " is not checked.",
	}, "\n")
	selected, missing := standingQualityStyleLines(doc, []string{"## Listed section", "## Absent"})
	if !slices.Equal(missing, []string{"heading ## Absent"}) {
		t.Fatalf("missing = %q", missing)
	}
	got := standingQualityStyleProblems("doc.md", doc, selected)
	want := []string{
		"doc.md:5: A standing rule " + dash + " is checked.",
		"doc.md:13: Inside the section " + dash + " is checked.",
		`doc.md:21: {"note": "` + dash + `"}`,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("problems = %q, want %q", got, want)
	}
}

// T088: claims of the threat model that need their own evidence.

// The threat model states that a case-variant directory such as .HN/ is
// ordinary content (research row A7): policy validation refuses a standing
// rule that covers .hn, and accepts one whose path differs from .hn only in
// case, because Git paths are case-sensitive and Hubnot reads only .hn/.
func TestStandingQualityCaseVariantDirectoryIsOrdinaryContent(t *testing.T) {
	actors := standingPolicyFieldsSyntheticActors()
	standing := standingPolicyFieldsStanding(actors.Executor)
	for _, path := range []string{".HN/", ".Hn/policy.json", ".hN/pipelines/"} {
		if _, _, err := parsePolicyBytes(standingPolicyFieldsDraft(t, actors, standingPolicyFieldsWith(standing, standingPolicyFieldsSet("paths", []string{path})))); err != nil {
			t.Errorf("standing rule path %q was refused: %v", path, err)
		}
	}
	_, _, err := parsePolicyBytes(standingPolicyFieldsDraft(t, actors, standingPolicyFieldsWith(standing, standingPolicyFieldsSet("paths", []string{".hn/"}))))
	if err == nil || !strings.Contains(err.Error(), `path ".hn/" covers .hn`) {
		t.Fatalf("standing rule path .hn/ was not refused as covering .hn: %v", err)
	}
}

// US5-4: under acceptance mode maintainers a rule with reviewers counts only
// those reviewers, and execution records an ordinary acceptance merge fact.
// S's approval and M's acceptance do not satisfy rule ordinary, whose
// reviewers list R; once R approves, M executes the rule and records a
// proposal.merged without a rule key that cites only M's decision.
func TestStandingQualityMaintainersModeReviewersExecution(t *testing.T) {
	h := newStandingHarness(t, standingHarnessDefaultPolicy())
	variant := h.PolicyCommit(h.Base(), standingAutomergeMaintainersVariant())
	c := h.openCase("A", variant, standingAutomergeFiles)
	c.Approval = h.Approve("S", c.Candidate.ID)
	h.completeCase(&c)
	decision := h.Accept("M", c.Candidate.ID)
	preview, status := standingAutomergePreview(h, "M", variant, c.Candidate.ID, "ordinary")
	requireStandingAutomergeBlocked(t, preview, status, standingAutomergeThreshold0of1)
	refused := standingAutomergeExecute(h, "M", variant, c.Candidate.ID, "ordinary")
	if refused.Status == 0 || refused.After != variant || len(refused.Appended) != 0 {
		t.Fatalf("execution without a counted approval exited %d, moved main to %s and appended %d event(s):\n%s",
			refused.Status, refused.After, len(refused.Appended), refused.Output)
	}

	h.Approve("R", c.Candidate.ID)
	preview, status = standingAutomergePreview(h, "M", variant, c.Candidate.ID, "ordinary")
	requireStandingAutomergeEligible(t, preview, status)
	assertOperationalContains(t, preview, standingAutomergeMaintainerLine, "Counted approvals: 1/1\n")
	run := standingAutomergeExecute(h, "M", variant, c.Candidate.ID, "ordinary")
	if run.Status != 0 {
		t.Fatalf("M's execution exited %d:\n%s", run.Status, run.Output)
	}
	if len(run.Appended) != 1 || run.Appended[0].Kind != "proposal.merged" {
		t.Fatalf("M's execution appended %#v, want one proposal.merged", run.Appended)
	}
	fact := h.fact("M", run.Appended[0].ID)
	_, fields, err := standingQualityObject(fact.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, carries := fields["rule"]; carries || fact.Rule != "" {
		t.Fatalf("the maintainers-mode merge fact carries a rule key:\n%s", fact.Payload)
	}
	if fact.Subject != c.Candidate.ID || fact.Commit != run.After || fact.Policy != h.PolicyDigest(variant) ||
		!slices.Equal(fact.Evidence, []string{decision.ID}) {
		t.Fatalf("merge fact %+v, want an acceptance merge fact at %s citing only decision %s", fact, run.After, decision.ID)
	}
	h.git("M", "push", "-q", "origin", "main")
	h.Publish("M")
	verifier := h.Verifier()
	h.hnIn(verifier, "sync", "origin")
	status2 := h.hnIn(verifier, "proposal", "status", c.Candidate.ID)
	assertOperationalContains(t, status2, "Status:   merged\n")
	if strings.Contains(status2, "Merged by rule") {
		t.Fatalf("the acceptance merge is reported as a rule merge:\n%s", status2)
	}
}

// NFR-007, formatting part: every root Go source is gofmt-formatted. The
// full gate also runs gofmt -l, go vet, go build, go test -race and git
// diff --check; this test keeps the formatting part inside go test.
func TestStandingQualityRootSourcesAreGofmtFormatted(t *testing.T) {
	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) == 0 {
		t.Fatal("no root Go sources found")
	}
	var unformatted []string
	for _, source := range sources {
		contents, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		formatted, err := format.Source(contents)
		if err != nil {
			t.Fatalf("%s: %v", source, err)
		}
		if !bytes.Equal(formatted, contents) {
			unformatted = append(unformatted, source)
		}
	}
	if len(unformatted) > 0 {
		t.Fatalf("gofmt would change: %s", strings.Join(unformatted, ", "))
	}
	t.Logf("%d root Go sources are gofmt-formatted", len(sources))
}
