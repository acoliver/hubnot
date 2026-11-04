package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// alpha requested a gate before beta, then revised its proposal and
// requested again. Ordered by request, beta's request would run first;
// ordered by lineage, alpha's revision keeps alpha's place and runs first.
func TestRunnerServesTheOldestLineageFirst(t *testing.T) {
	project := newRunnerActorsProject(t)
	alpha := project.alpha
	shown := runOperationalCommand(t, project.binary, alpha.repository, "proposal", "show", alpha.proposal)
	head := ""
	for _, line := range strings.Split(shown, "\n") {
		if strings.HasPrefix(line, "Head: ") {
			head = strings.TrimSpace(strings.TrimPrefix(line, "Head: "))
		}
	}
	if head == "" {
		t.Fatalf("no head in proposal show:\n%s", shown)
	}
	runOperationalGit(t, alpha.repository, "switch", "-q", "-c", "alpha-revision", head)
	if err := os.WriteFile(filepath.Join(alpha.repository, "alpha-revision.txt"), []byte("revised\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runOperationalGit(t, alpha.repository, "add", "alpha-revision.txt")
	runOperationalGit(t, alpha.repository, "commit", "-q", "-m", "alpha revision")
	revisedHead := runOperationalGit(t, alpha.repository, "rev-parse", "HEAD")
	runOperationalGit(t, alpha.repository, "switch", "-q", "main")
	runOperationalCommand(t, project.binary, alpha.repository, "proposal", "revise", alpha.proposal, "--base", "main", "--head", revisedHead)
	revision := latestOperationalEvent(t, alpha.repository, operationalActorRef(alpha.actor))
	runOperationalCommand(t, project.binary, alpha.repository, "run", "request", revision.ID, "test")
	request := latestOperationalEvent(t, alpha.repository, operationalActorRef(alpha.actor))
	if request.Kind != "run.request" || request.Subject != revision.ID {
		t.Fatalf("revision run request binding mismatch: %#v", request)
	}
	runOperationalCommand(t, project.binary, alpha.repository, "sync", "origin")

	output, code := project.runnerOnceWith(t, alpha.actor, project.beta.actor)
	if code != 0 {
		t.Fatalf("runner once exited %d:\n%s", code, output)
	}
	if !strings.Contains(output, "Runner accepted request "+operationalShortID(request.ID)+" ") {
		t.Fatalf("runner did not serve alpha's revision first:\n%s", output)
	}
	if strings.Contains(output, runnerAcceptedLine(project.beta)) {
		t.Fatalf("runner served beta before alpha's older lineage:\n%s", output)
	}
}
