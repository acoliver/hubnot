package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// reviseRunnerParticipant revises participant's proposal onto a new head
// based on main, requests pipeline test on the revision and publishes both.
// It returns the revision's run request ID and the revision ID.
func reviseRunnerParticipant(t *testing.T, project runnerActorsProject, participant runnerActorsParticipant) (string, string) {
	t.Helper()
	repository := participant.repository
	runOperationalGit(t, repository, "switch", "-q", "-c", "revision", "main")
	if err := os.WriteFile(filepath.Join(repository, "revision.txt"), []byte("revised\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runOperationalGit(t, repository, "add", "revision.txt")
	runOperationalGit(t, repository, "commit", "-q", "-m", "revised change")
	head := runOperationalGit(t, repository, "rev-parse", "HEAD")
	runOperationalGit(t, repository, "switch", "-q", "main")
	runOperationalCommand(t, project.binary, repository, "proposal", "revise", participant.proposal, "--base", "main", "--head", head)
	revision := latestOperationalEvent(t, repository, operationalActorRef(participant.actor))
	if !isProposalKind(revision.Kind) || revision.Head != head {
		t.Fatalf("revision binding mismatch: %#v", revision)
	}
	runOperationalCommand(t, project.binary, repository, "run", "request", revision.ID, "test")
	request := latestOperationalEvent(t, repository, operationalActorRef(participant.actor))
	if request.Kind != "run.request" || request.Subject != revision.ID {
		t.Fatalf("revision run request binding mismatch: %#v", request)
	}
	runOperationalCommand(t, project.binary, repository, "sync", "origin")
	return request.ID, revision.ID
}

// advanceRemoteMain pushes a new commit to the remote's main from
// participant's clone, so every open proposal's base becomes stale.
func advanceRemoteMain(t *testing.T, participant runnerActorsParticipant) {
	t.Helper()
	repository := participant.repository
	if err := os.WriteFile(filepath.Join(repository, "moved.txt"), []byte("main moved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runOperationalGit(t, repository, "add", "moved.txt")
	runOperationalGit(t, repository, "commit", "-q", "-m", "move main")
	runOperationalGit(t, repository, "push", "-q", "origin", "main")
}

func (project runnerActorsProject) runnerOnceArgs(t *testing.T, extra []string, actors ...string) (string, int) {
	t.Helper()
	args := []string{"runner", "once", "--accept-pipeline", "test"}
	for _, actor := range actors {
		args = append(args, "--accept-actor", actor)
	}
	args = append(args, extra...)
	args = append(args, "--backend", "host", "--allow-unsafe-host-execution")
	return runAllTrustedCommand(t, project.binary, project.runner, nil, args...)
}

func TestRunnerSkipsSupersededRequestAndRunsTheRevision(t *testing.T) {
	project := newRunnerActorsProject(t)
	revisionRequest, revision := reviseRunnerParticipant(t, project, project.alpha)

	output, code := project.runnerOnceWith(t, project.alpha.actor)
	if code != 0 {
		t.Fatalf("runner once exited %d:\n%s", code, output)
	}
	assertOperationalContains(t, output, "Runner skipped request "+operationalShortID(project.alpha.request)+" from "+
		operationalShortID(project.alpha.actor)+": its proposal is superseded by "+operationalShortID(revision)+"\n")
	assertOperationalContains(t, output, "Runner accepted request "+operationalShortID(revisionRequest)+" ")
	if strings.Contains(output, runnerAcceptedLine(project.alpha)) {
		t.Fatalf("runner executed the superseded request:\n%s", output)
	}

	output, code = project.runnerOnceWith(t, project.alpha.actor)
	if code != 0 {
		t.Fatalf("second runner once exited %d:\n%s", code, output)
	}
	assertOperationalContains(t, output, "No pending test requests from actor "+operationalShortID(project.alpha.actor)+".\n")
	project.assertRunnerResult(t, project.alpha, false)
}

func TestRunnerSkipStaleBaseOnlyWithTheFlag(t *testing.T) {
	project := newRunnerActorsProject(t)
	listed := []string{project.alpha.actor, project.beta.actor}
	advanceRemoteMain(t, project.outsider)

	output, code := project.runnerOnceArgs(t, []string{"--skip-stale-base", "main"}, listed...)
	if code != 0 {
		t.Fatalf("runner once --skip-stale-base exited %d:\n%s", code, output)
	}
	for _, participant := range []runnerActorsParticipant{project.alpha, project.beta} {
		assertOperationalContains(t, output, "Runner skipped request "+operationalShortID(participant.request)+" from "+operationalShortID(participant.actor)+": its proposal base ")
	}
	assertOperationalContains(t, output, "is not the current target tip ")
	if strings.Contains(output, "Runner accepted request") {
		t.Fatalf("runner executed a stale-base request:\n%s", output)
	}
	project.assertRunnerResult(t, project.alpha, false)
	project.assertRunnerResult(t, project.beta, false)

	// Without the flag the same requests run, as before this change.
	output, code = project.runnerOnceWith(t, listed...)
	if code != 0 {
		t.Fatalf("runner once without the flag exited %d:\n%s", code, output)
	}
	if strings.Contains(output, "Runner skipped request") || !strings.Contains(output, "Runner accepted request") {
		t.Fatalf("runner without --skip-stale-base did not run a stale-base request:\n%s", output)
	}
}

func TestRunnerSkipStaleBaseFailsOpenWhenTheBranchCannotBeRead(t *testing.T) {
	project := newRunnerActorsProject(t)
	output, code := project.runnerOnceArgs(t, []string{"--skip-stale-base", "no-such-branch"}, project.alpha.actor)
	if code != 0 {
		t.Fatalf("runner once exited %d:\n%s", code, output)
	}
	assertOperationalContains(t, output, runnerAcceptedLine(project.alpha))
	if strings.Contains(output, "Runner skipped request") {
		t.Fatalf("runner skipped a request although the target tip could not be read:\n%s", output)
	}
}

func TestRunnerSkipStaleBaseRefusesAMalformedBranch(t *testing.T) {
	project := newRunnerActorsProject(t)
	output, code := project.runnerOnceArgs(t, []string{"--skip-stale-base", "bad..name"}, project.alpha.actor)
	if code == 0 {
		t.Fatalf("runner once accepted a malformed --skip-stale-base:\n%s", output)
	}
	assertOperationalContains(t, output, `runner --skip-stale-base "bad..name" is not a valid branch name`)
	if strings.Contains(output, "Runner accepted request") {
		t.Fatalf("runner executed a request despite a usage error:\n%s", output)
	}
}
