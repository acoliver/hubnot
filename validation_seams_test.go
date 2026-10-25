package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// seamFixture is a repository whose base commit carries a policy requiring one
// trusted approval, one passing "check" result and one acceptance. A second
// pipeline definition, "lint", exists at every head but is not required by the
// base policy, so destination policies can add it.
type seamFixture struct {
	gitDir string
	alice  *Identity
	bob    *Identity
	carol  *Identity
	base   string
	policy PolicyDocument
	digest string
}

func newSeamFixture(t *testing.T) *seamFixture {
	t.Helper()
	root := inReplicationTestRepository(t)
	fixture := &seamFixture{alice: testIdentity(t, "Alice"), bob: testIdentity(t, "Bob"), carol: testIdentity(t, "Carol")}
	writeTestPolicy(t, root, PolicyDocument{
		Version:     policyVersion,
		Maintainers: []string{fixture.alice.Actor},
		Proposals: ProposalPolicy{
			RequiredApprovals: 1,
			RequiredAccepts:   1,
			TrustedReviewers:  []string{fixture.bob.Actor},
		},
		Pipelines: map[string]PipelinePolicy{
			"check": {RequiredResults: 1, TrustedRunners: []string{fixture.bob.Actor}},
		},
	})
	writeSeamPipeline(t, root, "check")
	writeSeamPipeline(t, root, "lint")
	mustGit(t, "add", ".hn")
	mustGit(t, "commit", "-q", "-m", "policy and pipelines")
	fixture.base = mustGitText(t, "rev-parse", "HEAD")
	policy, _, digest, err := loadPolicy(fixture.base)
	if err != nil {
		t.Fatal(err)
	}
	fixture.policy, fixture.digest = policy, digest
	gitDir, err := requireGitRepository()
	if err != nil {
		t.Fatal(err)
	}
	fixture.gitDir = gitDir
	return fixture
}

func writeSeamPipeline(t *testing.T, root, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".hn", "pipelines"), 0o755); err != nil {
		t.Fatal(err)
	}
	definition := []byte("{\"version\":\"hn.pipeline/0\",\"steps\":[{\"name\":\"" + name + "\",\"command\":\"true\"}]}\n")
	if err := os.WriteFile(filepath.Join(root, ".hn", "pipelines", name+".json"), definition, 0o644); err != nil {
		t.Fatal(err)
	}
}

func seamHead(t *testing.T, label string) string {
	t.Helper()
	mustGit(t, "commit", "--allow-empty", "-q", "-m", label)
	return mustGitText(t, "rev-parse", "HEAD")
}

func (fixture *seamFixture) open(t *testing.T, title, head string, publishCode bool) *StoredEvent {
	t.Helper()
	event, err := nextEvent(fixture.alice, "proposal.open")
	if err != nil {
		t.Fatal(err)
	}
	event.Title = title
	event.Base = fixture.base
	event.Head = head
	stored, err := appendEvent(event, fixture.alice)
	if err != nil {
		t.Fatal(err)
	}
	if publishCode {
		if err := createProposalRef(stored.ID, head); err != nil {
			t.Fatal(err)
		}
	}
	return stored
}

func (fixture *seamFixture) revise(t *testing.T, predecessor *StoredEvent, head string) *StoredEvent {
	t.Helper()
	event, err := nextEvent(fixture.alice, "proposal.revise")
	if err != nil {
		t.Fatal(err)
	}
	event.Subject = predecessor.ID
	event.Base = predecessor.Event.Head
	event.Head = head
	stored, err := appendEvent(event, fixture.alice)
	if err != nil {
		t.Fatal(err)
	}
	if err := createProposalRef(stored.ID, head); err != nil {
		t.Fatal(err)
	}
	return stored
}

// passingEvidence records Bob's approval and Bob's passing "check" result for a
// run Alice requested, and returns their IDs.
func (fixture *seamFixture) passingEvidence(t *testing.T, proposal *StoredEvent) []string {
	t.Helper()
	review := appendRevisionReview(t, fixture.bob, proposal.ID)
	request := appendSeamRunRequest(t, fixture.alice, proposal, "check")
	result := appendRevisionRunResult(t, fixture.bob, request)
	return []string{review.ID, result.ID}
}

func appendSeamRunRequest(t *testing.T, maintainer *Identity, proposal *StoredEvent, pipeline string) *StoredEvent {
	t.Helper()
	_, _, definition, err := loadPipeline(proposal.Event.Head, pipeline)
	if err != nil {
		t.Fatal(err)
	}
	event, err := nextEvent(maintainer, "run.request")
	if err != nil {
		t.Fatal(err)
	}
	event.Subject = proposal.ID
	event.Pipeline = pipeline
	event.Definition = definition
	event.Commit = proposal.Event.Head
	stored, err := appendEvent(event, maintainer)
	if err != nil {
		t.Fatal(err)
	}
	return stored
}

func appendSeamDecision(t *testing.T, maintainer *Identity, proposalID, policy, verdict string, evidence []string) *StoredEvent {
	t.Helper()
	event, err := nextEvent(maintainer, "proposal.decision")
	if err != nil {
		t.Fatal(err)
	}
	event.Subject = proposalID
	event.Policy = policy
	event.Verdict = verdict
	if verdict == "reject" {
		event.Body = "rejected by the seam fixture"
	}
	event.Evidence = append([]string(nil), evidence...)
	stored, err := appendEvent(event, maintainer)
	if err != nil {
		t.Fatal(err)
	}
	return stored
}

func (fixture *seamFixture) mergeAtHead(t *testing.T, proposal *StoredEvent) *StoredEvent {
	t.Helper()
	events, err := collectEvents()
	if err != nil {
		t.Fatal(err)
	}
	evaluation, err := evaluateProposal(proposal, events)
	if err != nil {
		t.Fatal(err)
	}
	if !evaluation.Accepted {
		t.Fatalf("merge precondition: proposal %s is not accepted", proposal.ID)
	}
	merge, err := appendProposalMergeEvent(fixture.alice, proposal, evaluation, proposal.Event.Head)
	if err != nil {
		t.Fatal(err)
	}
	return merge
}

func mustCollectSeamEvents(t *testing.T) []StoredEvent {
	t.Helper()
	events, err := collectEvents()
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func mustEvaluateSeamProposal(t *testing.T, proposal *StoredEvent, events []StoredEvent) *ProposalEvaluation {
	t.Helper()
	evaluation, err := evaluateProposal(proposal, events)
	if err != nil {
		t.Fatal(err)
	}
	return evaluation
}

func clonePolicyForSeam(policy PolicyDocument) PolicyDocument {
	clone := policy
	clone.Maintainers = append([]string(nil), policy.Maintainers...)
	clone.Proposals.TrustedReviewers = append([]string(nil), policy.Proposals.TrustedReviewers...)
	clone.Pipelines = make(map[string]PipelinePolicy, len(policy.Pipelines))
	for name, pipeline := range policy.Pipelines {
		pipeline.TrustedRunners = append([]string(nil), pipeline.TrustedRunners...)
		clone.Pipelines[name] = pipeline
	}
	return clone
}

func TestValidationSeamsBaseEvaluationEquivalence(t *testing.T) {
	fixture := newSeamFixture(t)

	blocked := fixture.open(t, "blocked", seamHead(t, "blocked head"), true)

	ready := fixture.open(t, "ready", seamHead(t, "ready head"), true)
	fixture.passingEvidence(t, ready)

	accepted := fixture.open(t, "accepted", seamHead(t, "accepted head"), true)
	appendSeamDecision(t, fixture.alice, accepted.ID, fixture.digest, "accept", fixture.passingEvidence(t, accepted))

	rejected := fixture.open(t, "rejected", seamHead(t, "rejected head"), true)
	fixture.passingEvidence(t, rejected)
	appendSeamDecision(t, fixture.alice, rejected.ID, fixture.digest, "reject", nil)

	merged := fixture.open(t, "merged", seamHead(t, "merged head"), true)
	appendSeamDecision(t, fixture.alice, merged.ID, fixture.digest, "accept", fixture.passingEvidence(t, merged))
	fixture.mergeAtHead(t, merged)

	superseded := fixture.open(t, "superseded", seamHead(t, "superseded head"), true)
	revision := fixture.revise(t, superseded, seamHead(t, "revision head"))

	unpublished := fixture.open(t, "unpublished", seamHead(t, "unpublished head"), false)
	fixture.passingEvidence(t, unpublished)

	events := mustCollectSeamEvents(t)
	cases := []struct {
		name     string
		proposal *StoredEvent
		status   string
	}{
		{"blocked", blocked, "blocked"},
		{"ready", ready, "ready for decision"},
		{"accepted", accepted, "accepted"},
		{"rejected", rejected, "rejected"},
		{"merged", merged, "merged"},
		{"superseded", superseded, "superseded"},
		{"revision", revision, "blocked"},
		{"unpublished", unpublished, "blocked"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			want := mustEvaluateSeamProposal(t, test.proposal, events)
			if got := proposalStatus(want); got != test.status {
				t.Fatalf("status = %q, want %q", got, test.status)
			}
			policy, _, digest, err := loadPolicy(test.proposal.Event.Base)
			if err != nil {
				t.Fatal(err)
			}
			underPolicy, err := evaluateProposalUnderPolicy(test.proposal, events, policy, digest)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(underPolicy, want) {
				t.Fatalf("evaluateProposalUnderPolicy differs from evaluateProposal:\n got %#v\nwant %#v", underPolicy, want)
			}
			head, exists, err := proposalHead(test.proposal.ID)
			if err != nil {
				t.Fatal(err)
			}
			withCode, err := evaluateProposalWithCode(test.proposal, events, policy, digest, proposalCodeState{Head: head, Exists: exists})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(withCode, want) {
				t.Fatalf("evaluateProposalWithCode differs from evaluateProposal:\n got %#v\nwant %#v", withCode, want)
			}
		})
	}

	t.Run("supplied code for an absent ref", func(t *testing.T) {
		fromRefs := mustEvaluateSeamProposal(t, unpublished, events)
		if fromRefs.CodeAvailable || fromRefs.CodeMatches || fromRefs.Ready {
			t.Fatalf("unpublished proposal evaluated from refs: available=%t matches=%t ready=%t", fromRefs.CodeAvailable, fromRefs.CodeMatches, fromRefs.Ready)
		}
		supplied, err := evaluateProposalWithCode(unpublished, events, fixture.policy, fixture.digest, proposalCodeState{Head: unpublished.Event.Head, Exists: true})
		if err != nil {
			t.Fatal(err)
		}
		if !supplied.CodeAvailable || !supplied.CodeMatches || !supplied.Ready {
			t.Fatalf("supplied code: available=%t matches=%t ready=%t; want all true", supplied.CodeAvailable, supplied.CodeMatches, supplied.Ready)
		}
		wrong, err := evaluateProposalWithCode(unpublished, events, fixture.policy, fixture.digest, proposalCodeState{Head: fixture.base, Exists: true})
		if err != nil {
			t.Fatal(err)
		}
		if !wrong.CodeAvailable || wrong.CodeMatches || wrong.Ready {
			t.Fatalf("mismatched supplied code: available=%t matches=%t ready=%t", wrong.CodeAvailable, wrong.CodeMatches, wrong.Ready)
		}
	})

	t.Run("error precedence", func(t *testing.T) {
		notProposal := events[0]
		for index := range events {
			if !isProposalKind(events[index].Event.Kind) {
				notProposal = events[index]
				break
			}
		}
		_, wantErr := evaluateProposal(&notProposal, events)
		_, underErr := evaluateProposalUnderPolicy(&notProposal, events, fixture.policy, fixture.digest)
		_, codeErr := evaluateProposalWithCode(&notProposal, events, fixture.policy, fixture.digest, proposalCodeState{})
		if wantErr == nil || underErr == nil || codeErr == nil || underErr.Error() != wantErr.Error() || codeErr.Error() != wantErr.Error() {
			t.Fatalf("non-proposal errors differ: base=%v under=%v code=%v", wantErr, underErr, codeErr)
		}
	})
}

func TestValidationSeamsDestinationPolicySemantics(t *testing.T) {
	fixture := newSeamFixture(t)
	ready := fixture.open(t, "ready", seamHead(t, "ready head"), true)
	fixture.passingEvidence(t, ready)
	merged := fixture.open(t, "merged", seamHead(t, "merged head"), true)
	appendSeamDecision(t, fixture.alice, merged.ID, fixture.digest, "accept", fixture.passingEvidence(t, merged))
	fixture.mergeAtHead(t, merged)
	events := mustCollectSeamEvents(t)

	base := mustEvaluateSeamProposal(t, merged, events)
	if len(base.Approvals) != 1 || !base.Ready || !base.Accepted || !base.Merged {
		t.Fatalf("base evaluation precondition: approvals=%d ready=%t accepted=%t merged=%t", len(base.Approvals), base.Ready, base.Accepted, base.Merged)
	}

	t.Run("removed trusted reviewer", func(t *testing.T) {
		destination := clonePolicyForSeam(fixture.policy)
		destination.Proposals.TrustedReviewers = []string{fixture.carol.Actor}
		evaluation, err := evaluateProposalUnderPolicy(merged, events, destination, fixture.digest)
		if err != nil {
			t.Fatal(err)
		}
		if len(evaluation.Approvals) != 0 || evaluation.Ready {
			t.Fatalf("approvals=%d ready=%t; want 0 and false", len(evaluation.Approvals), evaluation.Ready)
		}
	})

	t.Run("author approval follows the destination", func(t *testing.T) {
		appendRevisionReview(t, fixture.alice, ready.ID)
		withAuthorReview := mustCollectSeamEvents(t)
		destination := clonePolicyForSeam(fixture.policy)
		destination.Proposals.TrustedReviewers = []string{fixture.alice.Actor}
		denied, err := evaluateProposalUnderPolicy(ready, withAuthorReview, destination, fixture.digest)
		if err != nil {
			t.Fatal(err)
		}
		destination.Proposals.AllowAuthorApproval = true
		allowed, err := evaluateProposalUnderPolicy(ready, withAuthorReview, destination, fixture.digest)
		if err != nil {
			t.Fatal(err)
		}
		if len(denied.Approvals) != 0 || len(allowed.Approvals) != 1 || allowed.Approvals[0].Event.Actor != fixture.alice.Actor {
			t.Fatalf("author approval: denied=%d allowed=%d", len(denied.Approvals), len(allowed.Approvals))
		}
	})

	t.Run("raised approval threshold", func(t *testing.T) {
		destination := clonePolicyForSeam(fixture.policy)
		destination.Proposals.RequiredApprovals = 2
		evaluation, err := evaluateProposalUnderPolicy(merged, events, destination, fixture.digest)
		if err != nil {
			t.Fatal(err)
		}
		if len(evaluation.Approvals) != 1 || evaluation.Ready {
			t.Fatalf("approvals=%d ready=%t; want 1 and false", len(evaluation.Approvals), evaluation.Ready)
		}
	})

	t.Run("removed maintainer", func(t *testing.T) {
		destination := clonePolicyForSeam(fixture.policy)
		destination.Maintainers = []string{fixture.carol.Actor}
		evaluation, err := evaluateProposalUnderPolicy(merged, events, destination, fixture.digest)
		if err != nil {
			t.Fatal(err)
		}
		if len(evaluation.AcceptDecisions) != 0 || evaluation.Accepted || evaluation.Ready || len(evaluation.Pipelines) != 1 || len(evaluation.Pipelines[0].Passed) != 0 {
			t.Fatalf("removed maintainer evidence counted: accepts=%d accepted=%t ready=%t pipelines=%#v", len(evaluation.AcceptDecisions), evaluation.Accepted, evaluation.Ready, evaluation.Pipelines)
		}
	})

	t.Run("removed runner and raised result threshold", func(t *testing.T) {
		destination := clonePolicyForSeam(fixture.policy)
		destination.Pipelines["check"] = PipelinePolicy{RequiredResults: 1, TrustedRunners: []string{fixture.carol.Actor}}
		untrusted, err := evaluateProposalUnderPolicy(merged, events, destination, fixture.digest)
		if err != nil {
			t.Fatal(err)
		}
		destination.Pipelines["check"] = PipelinePolicy{RequiredResults: 2, TrustedRunners: []string{fixture.bob.Actor}}
		raised, err := evaluateProposalUnderPolicy(merged, events, destination, fixture.digest)
		if err != nil {
			t.Fatal(err)
		}
		if len(untrusted.Pipelines[0].Passed) != 0 || untrusted.Ready || len(raised.Pipelines[0].Passed) != 1 || raised.Pipelines[0].Required != 2 || raised.Ready {
			t.Fatalf("runner semantics: untrusted=%#v raised=%#v", untrusted.Pipelines, raised.Pipelines)
		}
	})

	t.Run("destination-only pipeline loads at the proposal head", func(t *testing.T) {
		destination := clonePolicyForSeam(fixture.policy)
		destination.Pipelines["lint"] = PipelinePolicy{RequiredResults: 1, TrustedRunners: []string{fixture.bob.Actor}}
		evaluation, err := evaluateProposalUnderPolicy(merged, events, destination, fixture.digest)
		if err != nil {
			t.Fatal(err)
		}
		names := make([]string, 0, len(evaluation.Pipelines))
		for _, pipeline := range evaluation.Pipelines {
			names = append(names, pipeline.Name)
		}
		if !reflect.DeepEqual(names, []string{"check", "lint"}) || len(evaluation.Pipelines[1].Passed) != 0 || evaluation.Ready {
			t.Fatalf("destination pipeline evaluation: names=%v pipelines=%#v ready=%t", names, evaluation.Pipelines, evaluation.Ready)
		}
	})

	t.Run("destination-only pipeline without a definition", func(t *testing.T) {
		destination := clonePolicyForSeam(fixture.policy)
		destination.Pipelines["absent"] = PipelinePolicy{RequiredResults: 1, TrustedRunners: []string{fixture.bob.Actor}}
		_, err := evaluateProposalUnderPolicy(merged, events, destination, fixture.digest)
		_, _, _, loadErr := loadPipeline(merged.Event.Head, "absent")
		if loadErr == nil {
			t.Fatal("test precondition: pipeline absent loaded")
		}
		var definitionErr pipelineDefinitionError
		if !errors.As(err, &definitionErr) || definitionErr.Pipeline != "absent" {
			t.Fatalf("error = %#v, want pipelineDefinitionError for absent", err)
		}
		if want := fmt.Sprintf("required pipeline %q: %v", "absent", loadErr); err.Error() != want {
			t.Fatalf("error text = %q, want %q", err.Error(), want)
		}
	})

	t.Run("decisions signed against another digest", func(t *testing.T) {
		otherDigest := "sha256:" + strings.Repeat("0", 64)
		evaluation, err := evaluateProposalUnderPolicy(merged, events, fixture.policy, otherDigest)
		if err != nil {
			t.Fatal(err)
		}
		if len(evaluation.AcceptDecisions) != 0 || evaluation.Accepted || len(evaluation.MergeEvents) != 0 || evaluation.Merged || evaluation.PolicyDigest != otherDigest {
			t.Fatalf("other digest counted: accepts=%d accepted=%t merges=%d merged=%t digest=%s", len(evaluation.AcceptDecisions), evaluation.Accepted, len(evaluation.MergeEvents), evaluation.Merged, evaluation.PolicyDigest)
		}
	})
}

func TestValidationSeamsBasePipelineErrorKeepsItsText(t *testing.T) {
	fixture := newSeamFixture(t)
	mustGit(t, "checkout", "-q", "--detach")
	mustGit(t, "rm", "-q", ".hn/pipelines/check.json")
	mustGit(t, "commit", "-q", "-m", "drop required pipeline")
	brokenHead := mustGitText(t, "rev-parse", "HEAD")
	mustGit(t, "checkout", "-q", "main")
	proposal := fixture.open(t, "broken pipeline", brokenHead, true)
	events := mustCollectSeamEvents(t)

	_, err := evaluateProposal(proposal, events)
	_, _, _, loadErr := loadPipeline(brokenHead, "check")
	if loadErr == nil {
		t.Fatal("test precondition: dropped pipeline loaded")
	}
	var definitionErr pipelineDefinitionError
	if !errors.As(err, &definitionErr) || definitionErr.Pipeline != "check" {
		t.Fatalf("error = %#v, want pipelineDefinitionError for check", err)
	}
	if want := fmt.Sprintf("required pipeline %q: %v", "check", loadErr); err.Error() != want {
		t.Fatalf("error text = %q, want %q", err.Error(), want)
	}
	if !errors.Is(err, errors.Unwrap(definitionErr)) {
		t.Fatal("pipelineDefinitionError does not unwrap to its cause")
	}
}

// seamEventCommit writes a signed event commit with the given parents without
// moving any ref, so a test can build hostile ranges on top of a hosted head.
func seamEventCommit(t *testing.T, identity *Identity, event Event, corruptSignature bool, extra map[string][]byte, parents ...string) string {
	t.Helper()
	payload, signature, err := encodeAndSign(event, identity)
	if err != nil {
		t.Fatal(err)
	}
	if corruptSignature {
		signature[0] ^= 0xff
	}
	blobs := map[string][]byte{
		"event.json": payload,
		"signature":  []byte(base64.RawStdEncoding.EncodeToString(signature)),
	}
	for name, contents := range extra {
		blobs[name] = contents
	}
	names := make([]string, 0, len(blobs))
	for name := range blobs {
		names = append(names, name)
	}
	sort.Strings(names)
	var treeInput strings.Builder
	for _, name := range names {
		blob, err := gitInput(blobs[name], nil, "hash-object", "-w", "--stdin")
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&treeInput, "100644 blob %s\t%s\n", strings.TrimSpace(string(blob)), name)
	}
	tree, err := gitInput([]byte(treeInput.String()), nil, "mktree")
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"commit-tree", strings.TrimSpace(string(tree)), "-m", "seam event"}
	for _, parent := range parents {
		args = append(args, "-p", parent)
	}
	commit, err := gitInput(nil, nil, args...)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(commit))
}

func appendSeamIssue(t *testing.T, identity *Identity, title string) *StoredEvent {
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

func seamIssueEvent(identity *Identity, sequence uint64, previous, title string) Event {
	event := newEvent(identity, "issue.open", sequence, previous)
	event.Title = title
	return event
}

func TestValidationSeamsActorRangeValidation(t *testing.T) {
	inReplicationTestRepository(t)
	gitDir, err := requireGitRepository()
	if err != nil {
		t.Fatal(err)
	}
	dave := testIdentity(t, "Dave")
	eve := testIdentity(t, "Eve")
	appendSeamIssue(t, dave, "first hosted")
	oldHead := appendSeamIssue(t, dave, "second hosted").Commit
	prefix, err := validateQuarantinedActor(gitDir, dave.Actor, oldHead)
	if err != nil {
		t.Fatal(err)
	}
	last := prefix[len(prefix)-1]

	t.Run("valid append joins the prefix", func(t *testing.T) {
		appendSeamIssue(t, dave, "third appended")
		newHead := appendSeamIssue(t, dave, "fourth appended").Commit
		joined, err := validateQuarantinedActorRange(gitDir, dave.Actor, oldHead, newHead, prefix)
		if err != nil {
			t.Fatal(err)
		}
		full, err := validateQuarantinedActor(gitDir, dave.Actor, newHead)
		if err != nil {
			t.Fatal(err)
		}
		if len(joined) != 4 || !reflect.DeepEqual(joined, full) {
			t.Fatalf("joined chain has %d events and differs from full validation", len(joined))
		}
		if len(prefix) != 2 {
			t.Fatalf("range validation modified the caller's prefix: %d events", len(prefix))
		}
		fromEmpty, err := validateQuarantinedActorRange(gitDir, dave.Actor, "", newHead, nil)
		if err != nil || !reflect.DeepEqual(fromEmpty, full) {
			t.Fatalf("empty old head: err=%v equal=%t", err, reflect.DeepEqual(fromEmpty, full))
		}
	})

	t.Run("empty range returns the prefix", func(t *testing.T) {
		joined, err := validateQuarantinedActorRange(gitDir, dave.Actor, oldHead, oldHead, prefix)
		if err != nil || !reflect.DeepEqual(joined, prefix) {
			t.Fatalf("empty range: err=%v joined=%d", err, len(joined))
		}
	})

	t.Run("inconsistent prefix", func(t *testing.T) {
		if _, err := validateQuarantinedActorRange(gitDir, dave.Actor, oldHead, oldHead, prefix[:1]); err == nil || !strings.Contains(err.Error(), "does not end at") {
			t.Fatalf("short prefix error = %v", err)
		}
		if _, err := validateQuarantinedActorRange(gitDir, dave.Actor, oldHead, oldHead, nil); err == nil {
			t.Fatal("missing prefix accepted")
		}
	})

	validThird := seamIssueEvent(dave, 3, last.ID, "valid third")
	validThirdCommit := seamEventCommit(t, dave, validThird, false, nil, oldHead)
	validThirdPayload, _, err := encodeAndSign(validThird, dave)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		newHead string
		want    string
	}{
		{"other actor", seamEventCommit(t, eve, seamIssueEvent(eve, 3, last.ID, "foreign"), false, nil, oldHead), "belongs to actor"},
		{"broken signature", seamEventCommit(t, dave, seamIssueEvent(dave, 3, last.ID, "forged"), true, nil, oldHead), "verify"},
		{"wrong sequence", seamEventCommit(t, dave, seamIssueEvent(dave, 4, last.ID, "skipped"), false, nil, oldHead), "invalid sequence"},
		{"wrong previous", seamEventCommit(t, dave, seamIssueEvent(dave, 3, prefix[0].ID, "forked"), false, nil, oldHead), "broken event chain"},
		{"extra tree entry", seamEventCommit(t, dave, seamIssueEvent(dave, 3, last.ID, "attached"), false, map[string][]byte{"extra.txt": []byte("x")}, oldHead), "unsupported"},
		{"merge commit", seamEventCommit(t, dave, seamIssueEvent(dave, 4, eventID(validThirdPayload), "merge"), false, nil, validThirdCommit, oldHead), "commit parent does not match"},
		{"commit without event parent", seamEventCommit(t, dave, seamIssueEvent(dave, 3, last.ID, "rootless"), false, nil), "commit parent does not match"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := validateQuarantinedActorRange(gitDir, dave.Actor, oldHead, test.newHead, prefix)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestValidationSeamsAbsentProposalCodeDependency(t *testing.T) {
	fixture := newSeamFixture(t)
	head := seamHead(t, "candidate head")
	emptyTree := mustGitTextFromInput(t, nil, "mktree")
	orphan := mustGitText(t, "commit-tree", emptyTree, "-m", "orphan head")
	absentCommit := strings.Repeat("e", len(fixture.base))
	proposalEvent := func(identity *Identity, kind, subject, base, head string) StoredEvent {
		event := newEvent(identity, kind, 1, "")
		event.Subject, event.Base, event.Head = subject, base, head
		if kind == "proposal.open" {
			event.Title = "dependency fixture"
		}
		payload, _, err := encodeAndSign(event, identity)
		if err != nil {
			t.Fatal(err)
		}
		return StoredEvent{ID: eventID(payload), Event: event}
	}
	open := proposalEvent(fixture.alice, "proposal.open", "", fixture.base, head)
	byID := map[string]StoredEvent{open.ID: open}
	allow := replicationDependencyOptions{AllowAbsentProposalCode: true}
	empty := map[string]string{}

	t.Run("zero option matches the existing behavior", func(t *testing.T) {
		want, wantErr := replicationEventDependency(fixture.gitDir, fixture.gitDir, open, byID, empty, empty, empty)
		got, gotErr := replicationEventDependencyWithOptions(fixture.gitDir, fixture.gitDir, open, byID, empty, empty, empty, replicationDependencyOptions{})
		if wantErr != nil || gotErr != nil || !reflect.DeepEqual(got, want) || want == nil || want.Missing != open.ID || want.Reason != "proposal code ref is absent" {
			t.Fatalf("zero option: got=%#v err=%v want=%#v err=%v", got, gotErr, want, wantErr)
		}
		present := map[string]string{open.ID: head}
		want, wantErr = replicationEventDependency(fixture.gitDir, fixture.gitDir, open, byID, present, empty, empty)
		got, gotErr = replicationEventDependencyWithOptions(fixture.gitDir, fixture.gitDir, open, byID, present, empty, empty, allow)
		if want != nil || got != nil || wantErr != nil || gotErr != nil {
			t.Fatalf("present code: got=%#v err=%v want=%#v err=%v", got, gotErr, want, wantErr)
		}
	})

	t.Run("absent code with existing base", func(t *testing.T) {
		dependency, err := replicationEventDependencyWithOptions(fixture.gitDir, fixture.gitDir, open, byID, empty, empty, empty, allow)
		if dependency != nil || err != nil {
			t.Fatalf("dependency=%#v err=%v; want accepted", dependency, err)
		}
	})

	t.Run("absent head skips ancestry", func(t *testing.T) {
		event := proposalEvent(fixture.alice, "proposal.open", "", fixture.base, absentCommit)
		dependency, err := replicationEventDependencyWithOptions(fixture.gitDir, fixture.gitDir, event, byID, empty, empty, empty, allow)
		if dependency != nil || err != nil {
			t.Fatalf("dependency=%#v err=%v; want accepted", dependency, err)
		}
	})

	t.Run("missing base", func(t *testing.T) {
		event := proposalEvent(fixture.alice, "proposal.open", "", absentCommit, head)
		dependency, err := replicationEventDependencyWithOptions(fixture.gitDir, fixture.gitDir, event, byID, empty, empty, empty, allow)
		if err != nil || dependency == nil || dependency.Missing != absentCommit || dependency.Reason != "proposal base commit is absent" || dependency.EventID != event.ID {
			t.Fatalf("dependency=%#v err=%v; want missing base %s", dependency, err, absentCommit)
		}
	})

	t.Run("present head that does not descend from the base", func(t *testing.T) {
		event := proposalEvent(fixture.alice, "proposal.open", "", fixture.base, orphan)
		_, err := replicationEventDependencyWithOptions(fixture.gitDir, fixture.gitDir, event, byID, empty, empty, empty, allow)
		if err == nil || !strings.Contains(err.Error(), "does not descend") {
			t.Fatalf("error = %v, want ancestry failure", err)
		}
	})

	t.Run("revision still checks predecessor and author", func(t *testing.T) {
		foreign := proposalEvent(fixture.bob, "proposal.revise", open.ID, head, absentCommit)
		_, err := replicationEventDependencyWithOptions(fixture.gitDir, fixture.gitDir, foreign, byID, empty, empty, empty, allow)
		if err == nil || !strings.Contains(err.Error(), "predecessor author") {
			t.Fatalf("foreign revision error = %v", err)
		}
		orphaned := proposalEvent(fixture.alice, "proposal.revise", "sha256:"+strings.Repeat("f", 64), head, absentCommit)
		dependency, err := replicationEventDependencyWithOptions(fixture.gitDir, fixture.gitDir, orphaned, byID, empty, empty, empty, allow)
		if err != nil || dependency == nil || dependency.Missing != orphaned.Event.Subject {
			t.Fatalf("missing predecessor: dependency=%#v err=%v", dependency, err)
		}
		own := proposalEvent(fixture.alice, "proposal.revise", open.ID, head, absentCommit)
		dependency, err = replicationEventDependencyWithOptions(fixture.gitDir, fixture.gitDir, own, byID, empty, empty, empty, allow)
		if err != nil || dependency != nil {
			t.Fatalf("own revision: dependency=%#v err=%v", dependency, err)
		}
	})
}

// assertFastForwardMergeAccepted checks that a proposal.merged fact whose
// commit equals its head passes relationship validation and closes the
// lineage, in whatever repository the test is currently inside.
func assertFastForwardMergeAccepted(t *testing.T, predecessorID, candidateID, mergeID string) {
	t.Helper()
	events := mustCollectSeamEvents(t)
	merge, err := resolveFullEvent(events, mergeID)
	if err != nil {
		t.Fatalf("fast-forward merge fact is not visible: %v", err)
	}
	if merge.Event.Commit != merge.Event.Head {
		t.Fatalf("merge commit %s != head %s", merge.Event.Commit, merge.Event.Head)
	}
	candidate, err := resolveFullEvent(events, candidateID)
	if err != nil {
		t.Fatal(err)
	}
	evaluation := mustEvaluateSeamProposal(t, candidate, events)
	if !evaluation.Merged || proposalStatus(evaluation) != "merged" || len(evaluation.MergeEvents) != 1 || evaluation.MergeEvents[0].ID != mergeID {
		t.Fatalf("candidate evaluation: merged=%t status=%q merges=%d", evaluation.Merged, proposalStatus(evaluation), len(evaluation.MergeEvents))
	}
	lineage, err := buildLineageIndex(events)
	if err != nil {
		t.Fatal(err)
	}
	predecessorState, err := lineage.state(predecessorID)
	if err != nil {
		t.Fatal(err)
	}
	candidateState, err := lineage.state(candidateID)
	if err != nil {
		t.Fatal(err)
	}
	if !predecessorState.LineageClosed || !candidateState.Merged || candidateState.MergeConflict {
		t.Fatalf("lineage: predecessor closed=%t candidate merged=%t conflict=%t", predecessorState.LineageClosed, candidateState.Merged, candidateState.MergeConflict)
	}
	predecessor, err := resolveFullEvent(events, predecessorID)
	if err != nil {
		t.Fatal(err)
	}
	if status := proposalStatus(mustEvaluateSeamProposal(t, predecessor, events)); status != "lineage closed" {
		t.Fatalf("predecessor status = %q, want lineage closed", status)
	}
}

func TestValidationSeamsFastForwardMergeFactValidatesLocallyAndThroughReplication(t *testing.T) {
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	publisher := filepath.Join(root, "publisher")
	remote := filepath.Join(root, "project.git")
	receiver := filepath.Join(root, "receiver")
	if err := os.Mkdir(publisher, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })
	mustGit(t, "-C", publisher, "init", "-q", "-b", "main")
	mustGit(t, "-C", publisher, "config", "user.name", "Publisher")
	mustGit(t, "-C", publisher, "config", "user.email", "publisher@hn.invalid")
	if err := os.Chdir(publisher); err != nil {
		t.Fatal(err)
	}
	alice := testIdentity(t, "Alice")
	bob := testIdentity(t, "Bob")
	writeTestPolicy(t, publisher, PolicyDocument{
		Version:     policyVersion,
		Maintainers: []string{alice.Actor},
		Proposals:   ProposalPolicy{RequiredApprovals: 1, RequiredAccepts: 1, TrustedReviewers: []string{bob.Actor}},
		Pipelines:   map[string]PipelinePolicy{"check": {RequiredResults: 1, TrustedRunners: []string{bob.Actor}}},
	})
	writeSeamPipeline(t, publisher, "check")
	mustGit(t, "add", ".hn")
	mustGit(t, "commit", "-q", "-m", "policy and pipeline")
	fixture := &seamFixture{alice: alice, bob: bob, base: mustGitText(t, "rev-parse", "HEAD")}

	predecessor := fixture.open(t, "fast-forward merge fact", seamHead(t, "predecessor head"), true)
	candidate := fixture.revise(t, predecessor, seamHead(t, "candidate head"))
	_, _, digest, err := loadPolicy(candidate.Event.Base)
	if err != nil {
		t.Fatal(err)
	}
	appendSeamDecision(t, alice, candidate.ID, digest, "accept", fixture.passingEvidence(t, candidate))
	merge := fixture.mergeAtHead(t, candidate)
	if merge.Event.Commit != candidate.Event.Head || merge.Event.Head != candidate.Event.Head {
		t.Fatalf("merge fact commit=%s head=%s; want both %s", merge.Event.Commit, merge.Event.Head, candidate.Event.Head)
	}
	assertFastForwardMergeAccepted(t, predecessor.ID, candidate.ID, merge.ID)

	mustGit(t, "clone", "-q", "--bare", publisher, remote)
	mustGit(t, "remote", "add", "origin", remote)
	for _, ref := range []string{actorRef(alice.Actor), actorRef(bob.Actor), proposalRef(predecessor.ID), proposalRef(candidate.ID)} {
		mustGit(t, "push", "-q", "origin", ref+":"+ref)
	}
	mustGit(t, "clone", "--no-local", "-q", remote, receiver)
	if err := os.Chdir(receiver); err != nil {
		t.Fatal(err)
	}
	result, err := runReplicationTransaction(ReplicationSelection{
		Version: replicationSelectionVersion, Remote: "origin",
		Actors:    []string{alice.Actor, bob.Actor},
		Proposals: []string{predecessor.ID, candidate.ID},
		Budgets:   defaultReplicationBudgets(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.hasFailures() {
		t.Fatalf("replication of a fast-forward merge fact failed: %#v", result.Outcomes)
	}
	assertRefValue(t, acceptedActorRef("origin", alice.Actor), merge.Commit)
	assertFastForwardMergeAccepted(t, predecessor.ID, candidate.ID, merge.ID)
}
