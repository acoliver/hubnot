package main

import (
	"strings"
	"testing"
)

// alpha requested its gate before beta. Without --first-actor the runner
// serves alpha; with --first-actor beta it serves beta first, the way a
// merge queue's combined-commit gate must not wait behind older requests.
func TestRunnerServesFirstActorRequestsBeforeOlderOnes(t *testing.T) {
	project := newRunnerActorsProject(t)
	args := []string{"runner", "once", "--accept-pipeline", "test",
		"--accept-actor", project.alpha.actor, "--accept-actor", project.beta.actor,
		"--first-actor", project.beta.actor,
		"--backend", "host", "--allow-unsafe-host-execution"}
	output, code := runAllTrustedCommand(t, project.binary, project.runner, nil, args...)
	if code != 0 {
		t.Fatalf("runner once exited %d:\n%s", code, output)
	}
	if !strings.Contains(output, runnerAcceptedLine(project.beta)) {
		t.Fatalf("runner did not serve the first actor's request:\n%s", output)
	}
	if strings.Contains(output, runnerAcceptedLine(project.alpha)) {
		t.Fatalf("runner served alpha before the first actor:\n%s", output)
	}
}

func TestRunnerRefusesAnInvalidFirstActor(t *testing.T) {
	project := newRunnerActorsProject(t)
	output, code := runAllTrustedCommand(t, project.binary, project.runner, nil, "runner", "once",
		"--accept-pipeline", "test", "--accept-actor", project.alpha.actor, "--first-actor", "beta",
		"--backend", "host", "--allow-unsafe-host-execution")
	if code == 0 || !strings.Contains(output, `--first-actor "beta" is not a full actor fingerprint`) {
		t.Fatalf("runner once with an invalid --first-actor exited %d:\n%s", code, output)
	}
}
