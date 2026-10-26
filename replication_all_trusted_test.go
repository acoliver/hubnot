package main

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The marker is written by hand into ordinary bare remotes. Identification
// only gates the all-trusted mode and grants no trust, so an ordinary remote
// carrying the marker is enough for client-side tests.
const (
	allTrustedMarkerRef     = "refs/hn/serve/v1"
	allTrustedMarkerContent = "hn-serve/1\n"
)

// Golden selection files produced by the code before --all-trusted existed.
const (
	allTrustedGoldenActor    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	allTrustedGoldenProposal = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	allTrustedGoldenMemory   = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	allTrustedGoldenExact    = `{
  "version": 1,
  "remote": "origin",
  "actors": [
    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  ],
  "proposals": [
    "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
  ],
  "memories": [
    "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
  ],
  "all": false,
  "budgets": {
    "maxEvents": 10000,
    "maxObjects": 100000,
    "maxObjectBytes": 67108864,
    "maxAttachmentBytes": 16777216,
    "maxTotalBytes": 1073741824
  }
}
`
	allTrustedGoldenAll = `{
  "version": 1,
  "remote": "origin",
  "actors": null,
  "proposals": null,
  "all": true,
  "budgets": {
    "maxEvents": 10000,
    "maxObjects": 100000,
    "maxObjectBytes": 67108864,
    "maxAttachmentBytes": 16777216,
    "maxTotalBytes": 1073741824
  }
}
`
	allTrustedExpectedFile = `{
  "version": 1,
  "remote": "origin",
  "actors": null,
  "proposals": null,
  "all": false,
  "allTrusted": true,
  "budgets": {
    "maxEvents": 10000,
    "maxObjects": 100000,
    "maxObjectBytes": 67108864,
    "maxAttachmentBytes": 16777216,
    "maxTotalBytes": 1073741824
  }
}
`
	allTrustedDefaultBudgetLines = "max-events: 10000\nmax-objects: 100000\nmax-object-bytes: 67108864\nmax-attachment-bytes: 16777216\nmax-total-bytes: 1073741824\n"
)

type allTrustedProject struct {
	binary          string
	root            string
	remote          string
	maintainer      string
	maintainerActor string
}

// newAllTrustedProject creates a maintainer repository with a baseline policy
// and pipeline, pushed to an ordinary bare remote that is optionally marked.
func newAllTrustedProject(t *testing.T, binary string, marked bool) allTrustedProject {
	t.Helper()
	root := t.TempDir()
	project := allTrustedProject{
		binary:     binary,
		root:       root,
		remote:     filepath.Join(root, "project.git"),
		maintainer: filepath.Join(root, "maintainer"),
	}
	initOperationalRepository(t, project.maintainer, "Maintainer")
	runOperationalCommand(t, binary, project.maintainer, "init", "--name", "Maintainer device")
	project.maintainerActor = readOperationalIdentity(t, binary, project.maintainer).Actor
	writeOperationalPolicy(t, project.maintainer, project.maintainerActor, project.maintainerActor, true, "", false)
	writeOperationalPipeline(t, project.maintainer)
	runOperationalGit(t, project.maintainer, "add", ".hn")
	runOperationalGit(t, project.maintainer, "commit", "-q", "-m", "baseline policy")
	runOperationalGit(t, "", "init", "--bare", "-q", project.remote)
	runOperationalGit(t, project.maintainer, "remote", "add", "origin", project.remote)
	runOperationalGit(t, project.maintainer, "push", "-q", "-u", "origin", "main")
	runOperationalGit(t, project.remote, "symbolic-ref", "HEAD", "refs/heads/main")
	if marked {
		markAllTrustedRemote(t, project.remote)
	}
	return project
}

func (project allTrustedProject) clone(t *testing.T, name string) string {
	t.Helper()
	return cloneAllTrustedRemote(t, project.root, project.remote, name)
}

// cloneWithIdentity clones the project and creates a fresh local identity.
func (project allTrustedProject) cloneWithIdentity(t *testing.T, name string) (string, string) {
	t.Helper()
	repository := project.clone(t, name)
	runOperationalCommand(t, project.binary, repository, "init", "--name", name+" device")
	return repository, readOperationalIdentity(t, project.binary, repository).Actor
}

func cloneAllTrustedRemote(t *testing.T, root, remote, name string) string {
	t.Helper()
	repository := filepath.Join(root, name)
	runOperationalGit(t, "", "clone", "-q", remote, repository)
	configureOperationalGit(t, repository, name)
	return repository
}

func markAllTrustedRemote(t *testing.T, remote string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "marker")
	if err := os.WriteFile(path, []byte(allTrustedMarkerContent), 0o600); err != nil {
		t.Fatal(err)
	}
	oid := runOperationalGit(t, remote, "hash-object", "-w", path)
	runOperationalGit(t, remote, "update-ref", allTrustedMarkerRef, oid)
}

// openAllTrustedProposal commits a change on a branch and opens a proposal
// for it, returning the proposal event ID.
func openAllTrustedProposal(t *testing.T, binary, repository, actor, title string) string {
	t.Helper()
	branch := "change-" + strings.ToLower(strings.ReplaceAll(title, " ", "-"))
	runOperationalGit(t, repository, "switch", "-q", "-c", branch)
	if err := os.WriteFile(filepath.Join(repository, branch+".txt"), []byte(title+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runOperationalGit(t, repository, "add", branch+".txt")
	runOperationalGit(t, repository, "commit", "-q", "-m", title)
	head := runOperationalGit(t, repository, "rev-parse", "HEAD")
	runOperationalGit(t, repository, "switch", "-q", "main")
	runOperationalCommand(t, binary, repository, "proposal", "open", "--base", "main", "--head", head, title)
	proposal := latestOperationalEvent(t, repository, operationalActorRef(actor))
	if proposal.Kind != "proposal.open" || proposal.Head != head {
		t.Fatalf("opened proposal binding mismatch: %#v", proposal)
	}
	return proposal.ID
}

func recordAllTrustedMemory(t *testing.T, binary, repository, content string) string {
	t.Helper()
	head := runOperationalGit(t, repository, "rev-parse", "HEAD")
	return recordOperationalMemory(t, binary, repository,
		"record", "--kind", "decision", "--at", head, "--applies", "exact", "--content", content, "--json").Stream
}

// runAllTrustedCommand runs hn and returns its combined output and exit code.
func runAllTrustedCommand(t *testing.T, binary, directory string, overrides map[string]string, args ...string) (string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, args...)
	command.Dir = directory
	command.Env = operationalTestEnvironmentWith(t.TempDir(), overrides)
	command.Stdin = strings.NewReader("")
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("hn %s timed out: %v\n%s", strings.Join(args, " "), ctx.Err(), output)
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

func allTrustedSelectionPath(repository, remote string) string {
	return filepath.Join(repository, ".git", "hn", "replication", "selections", base64.RawURLEncoding.EncodeToString([]byte(remote))+".json")
}

func readAllTrustedSelection(t *testing.T, repository string) string {
	t.Helper()
	contents, err := os.ReadFile(allTrustedSelectionPath(repository, "origin"))
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

func assertNoAllTrustedSelection(t *testing.T, repository string) {
	t.Helper()
	if _, err := os.Stat(allTrustedSelectionPath(repository, "origin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused selection wrote a selection file: %v", err)
	}
}

func acceptedAllTrustedRefs(t *testing.T, repository string) string {
	t.Helper()
	return runOperationalGit(t, repository, "for-each-ref", "--format=%(refname) %(objectname)", "refs/hn/remotes")
}

func assertAllTrustedOmits(t *testing.T, output string, values ...string) {
	t.Helper()
	for _, value := range values {
		if strings.Contains(output, value) {
			t.Fatalf("output unexpectedly contains %q:\n%s", value, output)
		}
	}
}

func TestReplicationAllTrustedValidationAndFailureStatus(t *testing.T) {
	base := ReplicationSelection{Version: replicationSelectionVersion, Remote: "origin", Budgets: defaultReplicationBudgets()}

	both := base
	both.All, both.AllTrusted = true, true
	if err := validateReplicationSelection(both); err == nil || err.Error() != "--all-trusted is mutually exclusive with --all" {
		t.Fatalf("--all with --all-trusted error = %v", err)
	}
	alone := base
	alone.AllTrusted = true
	if err := validateReplicationSelection(alone); err != nil {
		t.Fatalf("--all-trusted alone must satisfy the selector rule: %v", err)
	}
	if err := validateReplicationSelection(base); err == nil || !strings.Contains(err.Error(), "requires at least one actor") {
		t.Fatalf("empty selection error = %v", err)
	}
	invalid := alone
	invalid.Actors = []string{"abc"}
	exactInvalid := base
	exactInvalid.Actors = []string{"abc"}
	gotErr, wantErr := validateReplicationSelection(invalid), validateReplicationSelection(exactInvalid)
	if gotErr == nil || wantErr == nil || gotErr.Error() != wantErr.Error() {
		t.Fatalf("exact selectors with --all-trusted validated differently: got %v, want %v", gotErr, wantErr)
	}
	duplicate := alone
	duplicate.Proposals = []string{allTrustedGoldenProposal, allTrustedGoldenProposal}
	if err := validateReplicationSelection(duplicate); err == nil || !strings.Contains(err.Error(), "duplicate proposal selector") {
		t.Fatalf("duplicate exact selector with --all-trusted error = %v", err)
	}

	notAdvertised := replicationTransactionResult{Outcomes: []ReplicationOutcome{
		{Kind: replicationActor, ID: allTrustedGoldenActor, Status: replicationPromoted},
		{Kind: replicationActor, ID: strings.Repeat("d", 64), Status: replicationNotAdvertised},
	}}
	if notAdvertised.hasFailures() {
		t.Fatal("not-advertised outcome counted as a failure")
	}
	for _, status := range []string{replicationFetched, replicationOverBudget, replicationStructuralInvalid, replicationDependencyMissing, replicationRelationshipBad, replicationPromotable} {
		failed := replicationTransactionResult{Outcomes: append(append([]ReplicationOutcome(nil), notAdvertised.Outcomes...),
			ReplicationOutcome{Kind: replicationProposal, ID: allTrustedGoldenProposal, Status: status})}
		if !failed.hasFailures() {
			t.Fatalf("status %s no longer counts as a failure", status)
		}
	}

	dependency := MemoryDependency{Kind: "evidence-event", MissingID: allTrustedGoldenProposal}
	recovery := memoryReplicationRecovery(alone, dependency, []string{replicationActor + ":" + allTrustedGoldenActor, replicationProposal + ":" + allTrustedGoldenProposal})
	if strings.Contains(recovery, "--actor") || strings.Contains(recovery, "--proposal") {
		t.Fatalf("all-trusted memory recovery asked for actor or proposal selectors: %s", recovery)
	}
	recovery = memoryReplicationRecovery(alone, dependency, []string{replicationMemory + ":" + allTrustedGoldenMemory})
	if !strings.Contains(recovery, "--memory "+allTrustedGoldenMemory) {
		t.Fatalf("all-trusted memory recovery did not require the exact memory selector: %s", recovery)
	}
}

func TestReplicationAllTrustedRefusesUnmarkedRemote(t *testing.T) {
	binary := buildOperationalBinary(t)
	project := newAllTrustedProject(t, binary, false)
	runner := project.clone(t, "runner")
	output, code := runAllTrustedCommand(t, binary, runner, nil, "replication", "select", "origin", "--all-trusted")
	if code != 1 || output != "hn: remote origin does not identify as hn serve; --all-trusted requires an hn serve remote\n" {
		t.Fatalf("unmarked remote refusal: code=%d output=%q", code, output)
	}
	assertNoAllTrustedSelection(t, runner)

	// A marker ref that points at any other object does not identify.
	other := filepath.Join(t.TempDir(), "other")
	if err := os.WriteFile(other, []byte("hn-serve/2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runOperationalGit(t, project.remote, "update-ref", allTrustedMarkerRef, runOperationalGit(t, project.remote, "hash-object", "-w", other))
	output, code = runAllTrustedCommand(t, binary, runner, nil, "replication", "select", "origin", "--all-trusted")
	if code != 1 || !strings.Contains(output, "remote origin does not identify as hn serve") {
		t.Fatalf("wrong marker refusal: code=%d output=%q", code, output)
	}
	assertNoAllTrustedSelection(t, runner)
}

func TestReplicationAllTrustedMutualExclusionBeforeNetwork(t *testing.T) {
	binary := buildOperationalBinary(t)
	repository := filepath.Join(t.TempDir(), "repository")
	initOperationalRepository(t, repository, "Offline")
	runOperationalGit(t, repository, "remote", "add", "origin", "ssh://hn-all-trusted.invalid/project.git")
	contacted := filepath.Join(t.TempDir(), "contacted")
	environment := map[string]string{"GIT_SSH_COMMAND": "touch '" + contacted + "'; false", "GIT_SSH_VARIANT": "ssh"}

	output, code := runAllTrustedCommand(t, binary, repository, environment, "replication", "select", "origin", "--all", "--all-trusted")
	if code != 1 || output != "hn: --all-trusted is mutually exclusive with --all\n" {
		t.Fatalf("mutual exclusion: code=%d output=%q", code, output)
	}
	if _, err := os.Stat(contacted); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("mutual exclusion check contacted the remote: %v", err)
	}
	assertNoAllTrustedSelection(t, repository)

	// The same remote is contacted, and refused, once the flags are valid.
	output, code = runAllTrustedCommand(t, binary, repository, environment, "replication", "select", "origin", "--all-trusted")
	if code != 1 || !strings.Contains(output, "remote origin does not identify as hn serve") {
		t.Fatalf("unreachable remote refusal: code=%d output=%q", code, output)
	}
	if _, err := os.Stat(contacted); err != nil {
		t.Fatalf("identification did not contact the remote: %v", err)
	}
	assertNoAllTrustedSelection(t, repository)
}

func TestReplicationAllTrustedDiscoversNewProposalAndRunRequest(t *testing.T) {
	binary := buildOperationalBinary(t)
	project := newAllTrustedProject(t, binary, true)
	runner, runnerActor := project.cloneWithIdentity(t, "runner")
	selected := runOperationalCommand(t, binary, runner, "replication", "select", "origin", "--all-trusted")
	assertOperationalContains(t, selected, "compatibility-all: false\nall-trusted: true\n")
	shown := runOperationalCommand(t, binary, runner, "replication", "show", "origin")
	assertAllTrustedOmits(t, shown, "actor:", "proposal:", "memory:")

	implementer, implementerActor := project.cloneWithIdentity(t, "implementer")
	proposal := openAllTrustedProposal(t, binary, implementer, implementerActor, "Discovered change")
	runOperationalCommand(t, binary, implementer, "sync", "origin")

	runOperationalCommand(t, binary, project.maintainer, "replication", "select", "origin", "--all-trusted")
	discovered := runOperationalCommand(t, binary, project.maintainer, "sync", "origin")
	assertOperationalContains(t, discovered,
		"Replication actor "+implementerActor+": promoted",
		"Replication proposal "+proposal+": promoted",
	)
	runOperationalCommand(t, binary, project.maintainer, "run", "request", proposal, "test")
	request := latestOperationalEvent(t, project.maintainer, operationalActorRef(project.maintainerActor))
	if request.Kind != "run.request" || request.Subject != proposal {
		t.Fatalf("run request binding mismatch: %#v", request)
	}
	runOperationalCommand(t, binary, project.maintainer, "sync", "origin")

	synced := runOperationalCommand(t, binary, runner, "sync", "origin")
	assertOperationalContains(t, synced,
		"Replication actor "+implementerActor+": promoted",
		"Replication actor "+project.maintainerActor+": promoted",
		"Replication proposal "+proposal+": promoted",
	)
	assertAllTrustedOmits(t, synced, "failed", "not advertised")
	assertOperationalRef(t, runner, acceptedActorRef("origin", implementerActor), true)
	assertOperationalRef(t, runner, acceptedActorRef("origin", project.maintainerActor), true)
	assertOperationalRef(t, runner, acceptedProposalRef("origin", proposal), true)
	assertOperationalRef(t, runner, allTrustedMarkerRef, false)
	if log := runOperationalCommand(t, binary, runner, "log"); !strings.Contains(log, operationalShortID(request.ID)) || !strings.Contains(log, operationalShortID(proposal)) {
		t.Fatalf("runner projection omitted the discovered facts:\n%s", log)
	}

	ran := runOperationalCommand(t, binary, runner, "runner", "once", "--accept-pipeline", "test", "--accept-actor", project.maintainerActor,
		"--backend", "host", "--allow-unsafe-host-execution")
	assertOperationalContains(t, ran, "Runner accepted request "+operationalShortID(request.ID), "Runner recorded ")
	result := latestOperationalEvent(t, runner, operationalActorRef(runnerActor))
	if result.Kind != "run.result" || result.Subject != request.ID {
		t.Fatalf("runner result binding mismatch: %#v", result)
	}
	if published := runOperationalGit(t, project.remote, "rev-parse", operationalActorRef(runnerActor)); published != runOperationalGit(t, runner, "rev-parse", operationalActorRef(runnerActor)) {
		t.Fatalf("runner result was not published: remote=%s", published)
	}
	if after := runOperationalCommand(t, binary, runner, "replication", "show", "origin"); after != shown {
		t.Fatalf("sync changed the saved all-trusted selection:\nbefore=%s\nafter=%s", shown, after)
	}
}

func TestReplicationAllTrustedUnpublishedActorIsNotAFailure(t *testing.T) {
	binary := buildOperationalBinary(t)
	project := newAllTrustedProject(t, binary, true)
	runOperationalCommand(t, binary, project.maintainer, "issue", "open", "maintainer fact")
	runOperationalCommand(t, binary, project.maintainer, "sync", "origin")
	_, registeredActor := project.cloneWithIdentity(t, "registered")
	unpublishedProposal := "sha256:" + strings.Repeat("e", 64)

	runner := project.clone(t, "runner")
	runOperationalCommand(t, binary, runner, "replication", "select", "origin", "--all-trusted",
		"--actor", registeredActor, "--actor", project.maintainerActor, "--proposal", unpublishedProposal)
	output, code := runAllTrustedCommand(t, binary, runner, nil, "sync", "origin")
	if code != 0 {
		t.Fatalf("sync with an unpublished exact actor exited %d:\n%s", code, output)
	}
	assertOperationalContains(t, output,
		"Replication actor "+registeredActor+": not advertised yet\n",
		"Replication proposal "+unpublishedProposal+": not advertised yet\n",
		"Replication actor "+project.maintainerActor+": promoted\n",
	)
	assertAllTrustedOmits(t, output, "failed", "dependency-missing")
	if strings.Count(output, "Replication actor "+project.maintainerActor) != 1 {
		t.Fatalf("advertised exact actor selector was requested twice:\n%s", output)
	}
	assertOperationalRef(t, runner, acceptedActorRef("origin", registeredActor), false)
	assertOperationalRef(t, runner, acceptedActorRef("origin", project.maintainerActor), true)
}

func TestReplicationAllTrustedImportsOnlyExactMemory(t *testing.T) {
	binary := buildOperationalBinary(t)
	project := newAllTrustedProject(t, binary, true)
	selectedStream := recordAllTrustedMemory(t, binary, project.maintainer, "Selected memory stream.")
	runOperationalCommand(t, binary, project.maintainer, "sync", "origin")
	other, otherActor := project.cloneWithIdentity(t, "other")
	otherStream := recordAllTrustedMemory(t, binary, other, "Advertised but unselected memory stream.")
	runOperationalCommand(t, binary, other, "sync", "origin")
	if selectedStream == otherStream {
		t.Fatalf("memory fixtures share stream %s", selectedStream)
	}
	selectedRef := "refs/hn/remotes/origin/memory/" + project.maintainerActor + "/" + strings.TrimPrefix(selectedStream, "sha256:")
	otherRef := "refs/hn/remotes/origin/memory/" + otherActor + "/" + strings.TrimPrefix(otherStream, "sha256:")

	plain := project.clone(t, "plain")
	runOperationalCommand(t, binary, plain, "replication", "select", "origin", "--all-trusted")
	output := runOperationalCommand(t, binary, plain, "sync", "origin")
	assertAllTrustedOmits(t, output, selectedStream, otherStream)
	assertOperationalRef(t, plain, selectedRef, false)
	assertOperationalRef(t, plain, otherRef, false)

	runner := project.clone(t, "runner")
	runOperationalCommand(t, binary, runner, "replication", "select", "origin", "--all-trusted", "--memory", selectedStream)
	output = runOperationalCommand(t, binary, runner, "sync", "origin")
	assertOperationalContains(t, output, "Replication memory "+selectedStream+": promoted")
	assertAllTrustedOmits(t, output, otherStream)
	assertOperationalRef(t, runner, selectedRef, true)
	assertOperationalRef(t, runner, otherRef, false)

	missingStream := "sha256:" + strings.Repeat("f", 64)
	missing := project.clone(t, "missing")
	runOperationalCommand(t, binary, missing, "replication", "select", "origin", "--all-trusted", "--memory", missingStream)
	output, code := runAllTrustedCommand(t, binary, missing, nil, "sync", "origin")
	if code != 1 {
		t.Fatalf("unadvertised exact memory selector exited %d:\n%s", code, output)
	}
	assertOperationalContains(t, output, "Replication memory "+missingStream+": failed (dependency-missing)")
}

func TestReplicationAllTrustedIdentificationLostSkipsImport(t *testing.T) {
	binary := buildOperationalBinary(t)
	project := newAllTrustedProject(t, binary, true)
	runner, runnerActor := project.cloneWithIdentity(t, "runner")
	runOperationalCommand(t, binary, runner, "replication", "select", "origin", "--all-trusted")
	runOperationalCommand(t, binary, project.maintainer, "issue", "open", "fact the runner must not import")
	runOperationalCommand(t, binary, project.maintainer, "sync", "origin")
	runOperationalGit(t, project.remote, "update-ref", "-d", allTrustedMarkerRef)
	runOperationalCommand(t, binary, runner, "issue", "open", "local fact to publish")

	output, code := runAllTrustedCommand(t, binary, runner, nil, "sync", "origin")
	if code != 1 {
		t.Fatalf("sync after identification loss exited %d:\n%s", code, output)
	}
	assertOperationalContains(t, output,
		"hn: import phase failed (publication phase completed): remote origin no longer identifies as hn serve; reselect with hn replication select\n")
	assertAllTrustedOmits(t, output, "Replication ")
	if refs := acceptedAllTrustedRefs(t, runner); refs != "" {
		t.Fatalf("sync imported refs after identification loss:\n%s", refs)
	}
	local := runOperationalGit(t, runner, "rev-parse", operationalActorRef(runnerActor))
	if published := runOperationalGit(t, project.remote, "rev-parse", operationalActorRef(runnerActor)); published != local {
		t.Fatalf("publication did not run after identification loss: remote=%s local=%s", published, local)
	}
	if !strings.Contains(runOperationalCommand(t, binary, runner, "replication", "show", "origin"), "all-trusted: true") {
		t.Fatal("identification loss changed the saved selection")
	}
}

func TestReplicationAllTrustedForgedMarkerGrantsNoTrust(t *testing.T) {
	binary := buildOperationalBinary(t)
	project := newAllTrustedProject(t, binary, true)
	runOperationalCommand(t, binary, project.maintainer, "issue", "open", "valid maintainer fact")
	runOperationalCommand(t, binary, project.maintainer, "sync", "origin")

	forger, forgedActor := project.cloneWithIdentity(t, "forger")
	runOperationalCommand(t, binary, forger, "issue", "open", "fact with a broken signature")
	forged := forgeAllTrustedSignature(t, forger, operationalActorRef(forgedActor))
	runOperationalGit(t, forger, "push", "-q", "origin", forged+":"+operationalActorRef(forgedActor))

	runner := project.clone(t, "runner")
	runOperationalCommand(t, binary, runner, "replication", "select", "origin", "--all-trusted")
	output, code := runAllTrustedCommand(t, binary, runner, nil, "sync", "origin")
	if code != 1 {
		t.Fatalf("sync with a forged actor chain exited %d:\n%s", code, output)
	}
	assertOperationalContains(t, output,
		"Replication actor "+forgedActor+": failed (structurally-invalid)",
		"Replication actor "+project.maintainerActor+": promoted",
		"independently valid selections were promoted",
	)
	assertOperationalRef(t, runner, acceptedActorRef("origin", forgedActor), false)
	assertOperationalRef(t, runner, acceptedActorRef("origin", project.maintainerActor), true)
	if log := runOperationalCommand(t, binary, runner, "log"); strings.Contains(log, "broken signature") {
		t.Fatalf("forged fact entered the accepted projection:\n%s", log)
	}
}

// forgeAllTrustedSignature rewrites the head event of ref with one flipped
// signature byte, keeping its parents, event payload and tree layout.
func forgeAllTrustedSignature(t *testing.T, repository, ref string) string {
	t.Helper()
	head := runOperationalGit(t, repository, "rev-parse", ref)
	encoded := runOperationalGit(t, repository, "show", head+":signature")
	signature, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil || len(signature) == 0 {
		t.Fatalf("decode signature %q: %v", encoded, err)
	}
	signature[0] ^= 0xff
	path := filepath.Join(t.TempDir(), "signature")
	if err := os.WriteFile(path, []byte(base64.RawStdEncoding.EncodeToString(signature)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	blob := runOperationalGit(t, repository, "hash-object", "-w", path)
	entries := strings.Split(runOperationalGit(t, repository, "ls-tree", head), "\n")
	replaced := false
	for index, entry := range entries {
		if strings.HasSuffix(entry, "\tsignature") {
			entries[index] = "100644 blob " + blob + "\tsignature"
			replaced = true
		}
	}
	if !replaced {
		t.Fatalf("event %s has no signature entry: %v", head, entries)
	}
	tree := runAllTrustedGitInput(t, repository, strings.Join(entries, "\n")+"\n", "mktree")
	args := []string{"commit-tree", tree, "-m", runOperationalGit(t, repository, "log", "-1", "--format=%B", head)}
	for _, parent := range strings.Fields(runOperationalGit(t, repository, "log", "-1", "--format=%P", head)) {
		args = append(args, "-p", parent)
	}
	forged := runOperationalGit(t, repository, args...)
	if forged == head {
		t.Fatal("forged commit equals the valid head")
	}
	return forged
}

func runAllTrustedGitInput(t *testing.T, directory, input string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = directory
	command.Env = operationalTestEnvironment(t.TempDir())
	command.Stdin = strings.NewReader(input)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func TestReplicationAllTrustedExistingModesIgnoreMarker(t *testing.T) {
	binary := buildOperationalBinary(t)
	project := newAllTrustedProject(t, binary, false)
	runOperationalCommand(t, binary, project.maintainer, "issue", "open", "parity maintainer fact")
	stream := recordAllTrustedMemory(t, binary, project.maintainer, "Parity memory stream.")
	runOperationalCommand(t, binary, project.maintainer, "sync", "origin")
	implementer, implementerActor := project.cloneWithIdentity(t, "implementer")
	proposal := openAllTrustedProposal(t, binary, implementer, implementerActor, "Parity change")
	runOperationalCommand(t, binary, implementer, "sync", "origin")
	invalidActor := strings.Repeat("f", 64)
	runOperationalGit(t, project.remote, "update-ref", operationalActorRef(invalidActor), runOperationalGit(t, project.remote, "rev-parse", "main"))

	marked := filepath.Join(project.root, "marked.git")
	runOperationalGit(t, "", "clone", "-q", "--mirror", project.remote, marked)
	markAllTrustedRemote(t, marked)
	unmarkedRefs := runOperationalGit(t, project.remote, "for-each-ref", "--format=%(refname) %(objectname)")
	markedRefs := make([]string, 0)
	for _, line := range strings.Split(runOperationalGit(t, marked, "for-each-ref", "--format=%(refname) %(objectname)"), "\n") {
		if !strings.HasPrefix(line, allTrustedMarkerRef+" ") {
			markedRefs = append(markedRefs, line)
		}
	}
	if unmarkedRefs != strings.Join(markedRefs, "\n") {
		t.Fatalf("parity remotes hold different refs:\nunmarked=%s\nmarked=%v", unmarkedRefs, markedRefs)
	}

	modes := map[string][]string{
		"exact": {"--actor", project.maintainerActor, "--actor", implementerActor, "--actor", invalidActor,
			"--proposal", proposal, "--memory", stream},
		"all":          {"--all"},
		"no-selection": nil,
	}
	for name, selectors := range modes {
		t.Run(name, func(t *testing.T) {
			type observed struct {
				output   string
				code     int
				accepted string
			}
			results := make(map[string]observed)
			for remoteName, remote := range map[string]string{"unmarked": project.remote, "marked": marked} {
				receiver := cloneAllTrustedRemote(t, t.TempDir(), remote, "receiver")
				if selectors != nil {
					runOperationalCommand(t, binary, receiver, append([]string{"replication", "select", "origin"}, selectors...)...)
				}
				output, code := runAllTrustedCommand(t, binary, receiver, nil, "sync", "origin")
				results[remoteName] = observed{output: output, code: code, accepted: acceptedAllTrustedRefs(t, receiver)}
				assertAllTrustedOmits(t, output, "not advertised", allTrustedMarkerRef, "hn serve")
				assertOperationalRef(t, receiver, allTrustedMarkerRef, false)
			}
			unmarked, markedResult := results["unmarked"], results["marked"]
			if unmarked != markedResult {
				t.Fatalf("outcomes differ between marked and unmarked remotes:\nunmarked=%#v\nmarked=%#v", unmarked, markedResult)
			}
			assertOperationalContains(t, unmarked.output,
				"Replication actor "+project.maintainerActor+": promoted",
				"Replication actor "+invalidActor+": failed (structurally-invalid)",
				"Replication proposal "+proposal+": promoted",
				"Replication memory "+stream+": promoted",
			)
			if unmarked.code != 1 {
				t.Fatalf("sync with an invalid actor exited %d", unmarked.code)
			}
		})
	}
}

func TestReplicationAllTrustedSelectionFilesAndShowOutput(t *testing.T) {
	binary := buildOperationalBinary(t)
	project := newAllTrustedProject(t, binary, true)
	repository := project.clone(t, "selector")

	exactShow := "remote: origin\nsaved: true\ncompatibility-all: false\n" +
		"actor: " + allTrustedGoldenActor + "\nproposal: " + allTrustedGoldenProposal + "\nmemory: " + allTrustedGoldenMemory + "\n" +
		allTrustedDefaultBudgetLines
	output := runOperationalCommand(t, binary, repository, "replication", "select", "origin",
		"--memory", allTrustedGoldenMemory, "--proposal", allTrustedGoldenProposal, "--actor", allTrustedGoldenActor)
	if output != "Saved replication selection for origin\n"+exactShow {
		t.Fatalf("exact select output changed:\n%s", output)
	}
	if got := readAllTrustedSelection(t, repository); got != allTrustedGoldenExact {
		t.Fatalf("exact selection file is not byte-identical to the golden file:\n%s", got)
	}
	if got := runOperationalCommand(t, binary, repository, "replication", "show", "origin"); got != exactShow {
		t.Fatalf("exact show output changed:\n%s", got)
	}

	allShow := "remote: origin\nsaved: true\ncompatibility-all: true\n" + allTrustedDefaultBudgetLines
	output = runOperationalCommand(t, binary, repository, "replication", "select", "origin", "--all")
	if output != "Saved replication selection for origin\n"+allShow {
		t.Fatalf("--all select output changed:\n%s", output)
	}
	if got := readAllTrustedSelection(t, repository); got != allTrustedGoldenAll {
		t.Fatalf("--all selection file is not byte-identical to the golden file:\n%s", got)
	}
	if got := runOperationalCommand(t, binary, repository, "replication", "show", "origin"); got != allShow {
		t.Fatalf("--all show output changed:\n%s", got)
	}

	unsaved := project.clone(t, "unsaved")
	if got := runOperationalCommand(t, binary, unsaved, "replication", "show", "origin"); got != "remote: origin\nsaved: false\ncompatibility-all: true\n"+allTrustedDefaultBudgetLines {
		t.Fatalf("no-selection show output changed:\n%s", got)
	}

	allTrustedShow := "remote: origin\nsaved: true\ncompatibility-all: false\nall-trusted: true\n" + allTrustedDefaultBudgetLines
	output = runOperationalCommand(t, binary, repository, "replication", "select", "origin", "--all-trusted")
	if output != "Saved replication selection for origin\n"+allTrustedShow {
		t.Fatalf("--all-trusted select output:\n%s", output)
	}
	if got := readAllTrustedSelection(t, repository); got != allTrustedExpectedFile {
		t.Fatalf("--all-trusted selection file:\n%s", got)
	}
	if got := runOperationalCommand(t, binary, repository, "replication", "show", "origin"); got != allTrustedShow {
		t.Fatalf("--all-trusted show output:\n%s", got)
	}
}

func TestReplicationAllTrustedShallowRecoveryStillNeedsExactSelectors(t *testing.T) {
	binary := buildOperationalBinary(t)
	fixture := setupShallowRecoveryFixture(t, defaultReplicationBudgets())
	remote := strings.TrimPrefix(runOperationalGit(t, fixture.clone, "remote", "get-url", "origin"), "file://")
	markAllTrustedRemote(t, remote)
	runOperationalCommand(t, binary, fixture.clone, "replication", "select", "origin", "--all-trusted")
	beforeRefs := acceptedAllTrustedRefs(t, fixture.clone)

	output, code := runAllTrustedCommand(t, binary, fixture.clone, nil, "sync", "origin", "--recover-shallow")
	if code != 1 {
		t.Fatalf("shallow recovery with a pure all-trusted selection exited %d:\n%s", code, output)
	}
	assertOperationalContains(t, output, fixture.actor.Actor, "is not in the saved exact selection",
		"hn replication select origin --actor "+fixture.actor.Actor)
	if after := acceptedAllTrustedRefs(t, fixture.clone); after != beforeRefs {
		t.Fatalf("refused shallow recovery changed accepted refs:\nbefore=%s\nafter=%s", beforeRefs, after)
	}
	if state := runOperationalGit(t, fixture.clone, "rev-parse", "--is-shallow-repository"); state != "true" {
		t.Fatalf("refused shallow recovery unshallowed the repository: %s", state)
	}
}

func TestReplicationAllTrustedDocumentedExample(t *testing.T) {
	guide, err := os.ReadFile(filepath.Join("docs", "replication-v0.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, example, found := strings.Cut(string(guide), "<!-- example: all-trusted-select -->\n```text\n")
	if !found {
		t.Fatal("replication guide omits the all-trusted-select example")
	}
	example, _, found = strings.Cut(example, "```\n")
	if !found {
		t.Fatal("all-trusted-select example is not terminated")
	}
	commandLine, expected, found := strings.Cut(example, "\n")
	if !found || !strings.HasPrefix(commandLine, "$ hn ") {
		t.Fatalf("example does not start with an hn command: %q", example)
	}

	binary := buildOperationalBinary(t)
	project := newAllTrustedProject(t, binary, true)
	repository := project.clone(t, "documented")
	got := runOperationalCommand(t, binary, repository, strings.Fields(strings.TrimPrefix(commandLine, "$ hn "))...)
	if got != expected {
		t.Fatalf("documented example output differs:\nwant:\n%s\ngot:\n%s", expected, got)
	}
}
