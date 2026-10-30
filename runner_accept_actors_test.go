package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runnerActorsParticipant is a clone whose identity opened a proposal and
// signed a run.request for pipeline test on it.
type runnerActorsParticipant struct {
	repository string
	actor      string
	proposal   string
	request    string
}

type runnerActorsProject struct {
	binary      string
	runner      string
	runnerActor string
	alpha       runnerActorsParticipant
	beta        runnerActorsParticipant
	outsider    runnerActorsParticipant
}

// newRunnerActorsProject builds a remote whose base policy lists alpha and
// beta as requesters of pipeline test. Alpha, beta and an outsider the policy
// does not list each open a proposal, request test on it and publish both.
func newRunnerActorsProject(t *testing.T) runnerActorsProject {
	t.Helper()
	binary := buildOperationalBinary(t)
	root := t.TempDir()
	remote := filepath.Join(root, "project.git")
	maintainer := filepath.Join(root, "maintainer")
	initOperationalRepository(t, maintainer, "Maintainer")
	runOperationalCommand(t, binary, maintainer, "init", "--name", "Maintainer device")
	maintainerActor := readOperationalIdentity(t, binary, maintainer).Actor
	if err := os.WriteFile(filepath.Join(maintainer, "README.md"), []byte("runner actors\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runOperationalGit(t, maintainer, "add", "README.md")
	runOperationalGit(t, maintainer, "commit", "-q", "-m", "initial")
	runOperationalGit(t, "", "init", "--bare", "-q", remote)
	runOperationalGit(t, maintainer, "remote", "add", "origin", remote)
	runOperationalGit(t, maintainer, "push", "-q", "-u", "origin", "main")
	runOperationalGit(t, remote, "symbolic-ref", "HEAD", "refs/heads/main")

	clones := map[string]*runnerActorsParticipant{}
	project := runnerActorsProject{binary: binary}
	for name, participant := range map[string]*runnerActorsParticipant{"alpha": &project.alpha, "beta": &project.beta, "outsider": &project.outsider} {
		participant.repository = cloneAllTrustedRemote(t, root, remote, name)
		runOperationalCommand(t, binary, participant.repository, "init", "--name", name+" device")
		participant.actor = readOperationalIdentity(t, binary, participant.repository).Actor
		clones[name] = participant
	}
	project.runner = cloneAllTrustedRemote(t, root, remote, "runner")
	runOperationalCommand(t, binary, project.runner, "init", "--name", "runner device")
	project.runnerActor = readOperationalIdentity(t, binary, project.runner).Actor

	writeRunnerActorsPolicy(t, maintainer, maintainerActor, project.runnerActor, []string{project.alpha.actor, project.beta.actor})
	writeOperationalPipeline(t, maintainer)
	runOperationalGit(t, maintainer, "add", ".hn")
	runOperationalGit(t, maintainer, "commit", "-q", "-m", "policy with pipeline requesters")
	runOperationalGit(t, maintainer, "push", "-q", "origin", "main")

	for _, repository := range []string{project.alpha.repository, project.beta.repository, project.outsider.repository, project.runner} {
		runOperationalGit(t, repository, "fetch", "-q", "origin")
		runOperationalGit(t, repository, "merge", "-q", "--ff-only", "origin/main")
	}
	for _, name := range []string{"alpha", "beta", "outsider"} {
		participant := clones[name]
		participant.proposal = openAllTrustedProposal(t, binary, participant.repository, participant.actor, "Change by "+name)
		runOperationalCommand(t, binary, participant.repository, "run", "request", participant.proposal, "test")
		request := latestOperationalEvent(t, participant.repository, operationalActorRef(participant.actor))
		if request.Kind != "run.request" || request.Subject != participant.proposal || request.Pipeline != "test" {
			t.Fatalf("%s run request binding mismatch: %#v", name, request)
		}
		participant.request = request.ID
		runOperationalCommand(t, binary, participant.repository, "sync", "origin")
	}
	return project
}

func writeRunnerActorsPolicy(t *testing.T, repository, maintainer, runner string, requesters []string) {
	t.Helper()
	policy := map[string]any{
		"version":     "hn.policy/0",
		"maintainers": []string{maintainer},
		"proposals": map[string]any{
			"requiredApprovals":   1,
			"requiredAccepts":     1,
			"trustedReviewers":    []string{maintainer},
			"allowAuthorApproval": false,
		},
		"pipelines": map[string]any{
			"test": map[string]any{
				"requiredResults": 1,
				"trustedRunners":  []string{runner},
				"requesters":      requesters,
			},
		},
	}
	encoded, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repository, ".hn"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, ".hn", "policy.json"), append(encoded, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// runnerOnceWith runs hn runner once on the host backend with one
// --accept-actor per actor and returns its combined output and exit code.
func (project runnerActorsProject) runnerOnceWith(t *testing.T, actors ...string) (string, int) {
	t.Helper()
	args := []string{"runner", "once", "--accept-pipeline", "test"}
	for _, actor := range actors {
		args = append(args, "--accept-actor", actor)
	}
	args = append(args, "--backend", "host", "--allow-unsafe-host-execution")
	return runAllTrustedCommand(t, project.binary, project.runner, nil, args...)
}

// runnerAcceptedLine is the line hn runner prints when it starts a request.
func runnerAcceptedLine(participant runnerActorsParticipant) string {
	return "Runner accepted request " + operationalShortID(participant.request) + " from " + operationalShortID(participant.actor) + " for pipeline test\n"
}

// assertRunnerResult checks whether the runner's identity holds a current
// result for participant's request, as hn run show reports it.
func (project runnerActorsProject) assertRunnerResult(t *testing.T, participant runnerActorsParticipant, want bool) {
	t.Helper()
	shown := runOperationalCommand(t, project.binary, project.runner, "run", "show", participant.request)
	has := strings.Contains(shown, "Current results:") && strings.Contains(shown, "runner device: passed, exit 0")
	if has != want {
		t.Fatalf("run show %s: runner result present=%t, want %t:\n%s", operationalShortID(participant.request), has, want, shown)
	}
}

func TestRunnerAcceptActorsExecutesEveryListedRequester(t *testing.T) {
	project := newRunnerActorsProject(t)
	listed := []string{project.alpha.actor, project.beta.actor}

	// hn runner once executes at most one request per invocation, so the
	// same runner configuration runs once per pending request.
	executed := map[string]bool{}
	outputs := make([]string, 0, 2)
	for cycle := 1; cycle <= 2; cycle++ {
		output, code := project.runnerOnceWith(t, listed...)
		if code != 0 {
			t.Fatalf("runner once cycle %d exited %d:\n%s", cycle, code, output)
		}
		outputs = append(outputs, output)
		for _, participant := range []runnerActorsParticipant{project.alpha, project.beta, project.outsider} {
			if strings.Contains(output, runnerAcceptedLine(participant)) {
				executed[participant.actor] = true
			}
		}
	}
	if !executed[project.alpha.actor] || !executed[project.beta.actor] || executed[project.outsider.actor] {
		t.Fatalf("one runner listing both requesters executed alpha=%t beta=%t outsider=%t, want both requesters and not the outsider:\n%s",
			executed[project.alpha.actor], executed[project.beta.actor], executed[project.outsider.actor], strings.Join(outputs, "\n---\n"))
	}
	for _, output := range outputs {
		assertOperationalContains(t, output, "Runner recorded passed result ")
	}
	project.assertRunnerResult(t, project.alpha, true)
	project.assertRunnerResult(t, project.beta, true)

	output, code := project.runnerOnceWith(t, listed...)
	if code != 0 {
		t.Fatalf("idle runner once exited %d:\n%s", code, output)
	}
	assertOperationalContains(t, output, "No pending test requests from actors "+operationalShortID(project.alpha.actor)+", "+operationalShortID(project.beta.actor)+".\n")
	if strings.Contains(output, "Runner accepted request") {
		t.Fatalf("idle runner executed a request:\n%s", output)
	}
	project.assertRunnerResult(t, project.outsider, false)
}

func TestRunnerAcceptActorsIgnoresUnlistedAndNonRequesterSigners(t *testing.T) {
	project := newRunnerActorsProject(t)
	// Beta may request test under the base policy but the runner does not
	// list it; the outsider is listed by the runner but not by the policy.
	listed := []string{project.outsider.actor, project.alpha.actor}

	output, code := project.runnerOnceWith(t, listed...)
	if code != 0 {
		t.Fatalf("runner once exited %d:\n%s", code, output)
	}
	assertOperationalContains(t, output, runnerAcceptedLine(project.alpha), "Runner recorded passed result ")
	output, code = project.runnerOnceWith(t, listed...)
	if code != 0 {
		t.Fatalf("second runner once exited %d:\n%s", code, output)
	}
	for _, participant := range []runnerActorsParticipant{project.beta, project.outsider} {
		if strings.Contains(output, runnerAcceptedLine(participant)) {
			t.Fatalf("runner executed %s:\n%s", operationalShortID(participant.request), output)
		}
	}
	assertOperationalContains(t, output,
		"hn runner: skipped "+operationalShortID(project.outsider.request)+": signer "+operationalShortID(project.outsider.actor)+" is neither a maintainer nor a requester of pipeline test under the base policy\n",
		"No pending test requests from actors "+operationalShortID(project.outsider.actor)+", "+operationalShortID(project.alpha.actor)+".\n",
	)
	project.assertRunnerResult(t, project.alpha, true)
	project.assertRunnerResult(t, project.beta, false)
	project.assertRunnerResult(t, project.outsider, false)
}

func TestRunnerAcceptActorsSingleFlagUnchanged(t *testing.T) {
	project := newRunnerActorsProject(t)

	// A single --accept-actor keeps executing every request its actor signs,
	// as before, even one whose signer the base policy does not list.
	output, code := project.runnerOnceWith(t, project.outsider.actor)
	if code != 0 {
		t.Fatalf("single-actor runner once exited %d:\n%s", code, output)
	}
	assertOperationalContains(t, output, runnerAcceptedLine(project.outsider), "Runner recorded passed result ")
	if strings.Contains(output, "skipped") {
		t.Fatalf("single-actor runner skipped a request:\n%s", output)
	}
	project.assertRunnerResult(t, project.outsider, true)
	output, code = project.runnerOnceWith(t, project.outsider.actor)
	if code != 0 {
		t.Fatalf("idle single-actor runner once exited %d:\n%s", code, output)
	}
	assertOperationalContains(t, output, "No pending test requests from actor "+operationalShortID(project.outsider.actor)+".\n")

	output, code = project.runnerOnceWith(t, project.beta.actor)
	if code != 0 {
		t.Fatalf("single requester runner once exited %d:\n%s", code, output)
	}
	assertOperationalContains(t, output, runnerAcceptedLine(project.beta), "Runner recorded passed result ")
	if strings.Contains(output, runnerAcceptedLine(project.alpha)) {
		t.Fatalf("single-actor runner executed another signer's request:\n%s", output)
	}
	project.assertRunnerResult(t, project.alpha, false)

	output, code = project.runnerOnceWith(t, "abc123")
	if code == 0 || !strings.Contains(output, "runner requires --accept-pipeline NAME and a full --accept-actor ACTOR fingerprint") {
		t.Fatalf("malformed single --accept-actor: code=%d output=%q", code, output)
	}
}

func TestRunnerAcceptActorsRefusesDuplicateAndMalformedValues(t *testing.T) {
	binary := buildOperationalBinary(t)
	repository := filepath.Join(t.TempDir(), "runner")
	initOperationalRepository(t, repository, "Runner")
	runOperationalCommand(t, binary, repository, "init", "--name", "Runner device")
	first := strings.Repeat("a", 64)
	second := strings.Repeat("b", 64)
	tooMany := make([]string, 0, maxRunnerAcceptedActors+1)
	for index := 0; index <= maxRunnerAcceptedActors; index++ {
		tooMany = append(tooMany, fmt.Sprintf("%064x", index+1))
	}
	cases := []struct {
		name    string
		command string
		actors  []string
		want    string
	}{
		{"exact duplicate", "once", []string{first, second, first}, "runner --accept-actor " + first + " is listed more than once"},
		{"duplicate in another case", "once", []string{first, strings.ToUpper(first)}, "runner --accept-actor " + strings.ToUpper(first) + " is listed more than once"},
		{"duplicate under watch", "watch", []string{second, second}, "runner --accept-actor " + second + " is listed more than once"},
		{"short fingerprint", "once", []string{first, "abc123"}, `runner --accept-actor "abc123" is not a full actor fingerprint`},
		{"prefixed fingerprint", "once", []string{"sha256:" + second, first}, `runner --accept-actor "sha256:` + second + `" is not a full actor fingerprint`},
		{"non-hex fingerprint", "watch", []string{first, strings.Repeat("g", 64)}, `runner --accept-actor "` + strings.Repeat("g", 64) + `" is not a full actor fingerprint`},
		{"empty value", "once", []string{first, ""}, `runner --accept-actor "" is not a full actor fingerprint`},
		{"more than 64", "once", tooMany, "runner accepts 1 to 64 --accept-actor fingerprints, got 65"},
		{"none", "once", nil, "runner requires --accept-pipeline NAME and a full --accept-actor ACTOR fingerprint"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			args := []string{"runner", testCase.command, "--accept-pipeline", "test"}
			for _, actor := range testCase.actors {
				args = append(args, "--accept-actor", actor)
			}
			output, code := runAllTrustedCommand(t, binary, repository, nil, args...)
			if code == 0 || !strings.Contains(output, testCase.want) {
				t.Fatalf("hn %s: code=%d output=%q, want refusal %q", strings.Join(args, " "), code, output, testCase.want)
			}
			// Refusal precedes synchronization, which would name the
			// missing origin remote.
			if strings.Contains(output, "origin") {
				t.Fatalf("refused runner reached synchronization:\n%s", output)
			}
		})
	}

	// Exactly 64 distinct fingerprints pass validation and reach the sync.
	args := []string{"runner", "once", "--accept-pipeline", "test"}
	for _, actor := range tooMany[:maxRunnerAcceptedActors] {
		args = append(args, "--accept-actor", actor)
	}
	output, code := runAllTrustedCommand(t, binary, repository, nil, args...)
	if code == 0 || !strings.Contains(output, `remote "origin" does not exist`) || strings.Contains(output, "accept-actor") {
		t.Fatalf("64 distinct actors: code=%d output=%q, want validation to pass and sync to need origin", code, output)
	}
}

// TestRunnerSingleAcceptActorUnlistedResultDoesNotCount pins the reason the
// single --accept-actor rule keeps executing a signer that the base policy
// does not list: the result it records grants that proposal nothing, while
// the same runner's result for a listed requester counts.
func TestRunnerSingleAcceptActorUnlistedResultDoesNotCount(t *testing.T) {
	project := newRunnerActorsProject(t)
	for _, participant := range []runnerActorsParticipant{project.outsider, project.beta} {
		output, code := project.runnerOnceWith(t, participant.actor)
		if code != 0 {
			t.Fatalf("single-actor runner once exited %d:\n%s", code, output)
		}
		assertOperationalContains(t, output, runnerAcceptedLine(participant), "Runner recorded passed result ")
		project.assertRunnerResult(t, participant, true)
	}
	outsider := runOperationalCommand(t, project.binary, project.runner, "proposal", "status", project.outsider.proposal)
	assertOperationalContains(t, outsider, "Pipeline: test 0/1 trusted passes\n")
	beta := runOperationalCommand(t, project.binary, project.runner, "proposal", "status", project.beta.proposal)
	assertOperationalContains(t, beta, "Pipeline: test 1/1 trusted passes\n")
}
