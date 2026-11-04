package main

import (
	"strings"
	"testing"
)

// After alpha's gate passes, beta's request on the same base waits while the
// window lasts, because alpha merges first and moves the base. Without the
// flag, or once the window has passed, beta runs as before.
func TestRunnerDefersBehindAPassedProposalOnTheSameBase(t *testing.T) {
	project := newRunnerActorsProject(t)
	listed := []string{project.alpha.actor, project.beta.actor}

	output, code := project.runnerOnceWith(t, project.alpha.actor)
	if code != 0 || !strings.Contains(output, runnerAcceptedLine(project.alpha)) {
		t.Fatalf("alpha's gate did not run (exit %d):\n%s", code, output)
	}
	project.assertRunnerResult(t, project.alpha, true)

	output, code = project.runnerOnceArgs(t, []string{"--defer-behind-passed", "30m"}, listed...)
	if code != 0 {
		t.Fatalf("runner once --defer-behind-passed exited %d:\n%s", code, output)
	}
	assertOperationalContains(t, output, "Runner deferred request "+operationalShortID(project.beta.request)+" ")
	if strings.Contains(output, runnerAcceptedLine(project.beta)) {
		t.Fatalf("runner ran beta behind alpha's passed gate:\n%s", output)
	}
	project.assertRunnerResult(t, project.beta, false)

	output, code = project.runnerOnceArgs(t, []string{"--defer-behind-passed", "1ns"}, listed...)
	if code != 0 || !strings.Contains(output, runnerAcceptedLine(project.beta)) {
		t.Fatalf("runner did not run beta once the window had passed (exit %d):\n%s", code, output)
	}
	project.assertRunnerResult(t, project.beta, true)
}
