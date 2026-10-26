package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// WP12: fan-out against hn serve. The black-box tests drive real
// participants, each with its own clone, hn identity and bound SSH key,
// through the SSH shim into one hosted repository. They check that the
// server advertises exactly the facts it accepted and the marker (FR-017),
// that runners and verifiers discover new work with only the all-trusted
// selection (FR-018, US4-1, US4-2), that the older selection modes behave
// the same against hn serve and an ordinary remote holding the same refs
// (FR-019), and that the Phase 0 team flow completes without actor or
// proposal selections and without direct pushes of main (SC-004, NFR-006).
//
// The real rust-pingpong Phase 0 run (notes/phase0-hubnot-findings.md) is a
// manual dogfooding check that needs a Rust toolchain. These tests
// reproduce its command flow with a portable shell pipeline instead.

// Participant roles of serveReplicationTeam. The hosted policy names the
// maintainer as maintainer, the reviewer as trusted reviewer and the runner
// as the trusted runner of pipeline test.
const (
	serveReplicationMaintainer   = "maintainer"
	serveReplicationOrchestrator = "orchestrator"
	serveReplicationImplementer  = "implementer"
	serveReplicationReviewer     = "reviewer"
	serveReplicationRunner       = "runner"

	serveReplicationRepository = "pingpong"
	serveReplicationScript     = "pingpong.sh"
	serveReplicationMain       = "refs/heads/main"
)

// serveReplicationCommand is one command a participant ran.
type serveReplicationCommand struct {
	Who     string
	Program string // "git" or "hn"
	Args    []string
}

func (command serveReplicationCommand) String() string {
	return command.Who + ": " + command.Program + " " + strings.Join(command.Args, " ")
}

// serveReplicationTeam hosts repository pingpong whose main carries
// .hn/policy.json (one trusted approval, one passing result of pipeline
// test from the runner, one maintainer acceptance) and
// .hn/pipelines/test.json, which passes when sh ./pingpong.sh prints pong.
//
// Every participant command goes through Git or Hn, which append it to the
// command log; a failing test prints the log. Participants enrolled at
// construction have a clone, an hn identity and a bound key. FreshClone
// adds a clone with a bound key and no Hubnot identity.
type serveReplicationTeam struct {
	t        *testing.T
	h        *serveHarness
	hosted   string
	Base     string
	actors   map[string]string
	keys     map[string]serveTestKey
	clients  map[string]serveHarnessClientEnv
	work     map[string]string
	commands []serveReplicationCommand
	commits  int
}

// newServeReplicationTeam enrolls the maintainer, implementer, reviewer and
// runner, then each extra participant, and hosts the repository.
func newServeReplicationTeam(t *testing.T, extra ...string) *serveReplicationTeam {
	t.Helper()
	h := newServeHarness(t)
	team := &serveReplicationTeam{
		t:       t,
		h:       h,
		hosted:  h.Layout().RepoGitDir(serveReplicationRepository),
		actors:  map[string]string{},
		keys:    map[string]serveTestKey{},
		clients: map[string]serveHarnessClientEnv{},
		work:    map[string]string{},
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("command log:\n%s", team.Log())
		}
	})
	names := append([]string{serveReplicationMaintainer, serveReplicationImplementer, serveReplicationReviewer, serveReplicationRunner}, extra...)
	for _, name := range names {
		team.enroll(name)
	}

	policy := serveGovernedPolicy{
		Maintainers:       []string{team.actors[serveReplicationMaintainer]},
		Reviewers:         []string{team.actors[serveReplicationReviewer]},
		RequiredApprovals: 1,
		Pipelines:         map[string][]string{"test": {team.actors[serveReplicationRunner]}},
	}
	source := filepath.Join(t.TempDir(), "source")
	initOperationalRepository(t, source, "Pingpong")
	serveHarnessWriteFile(t, source, serveGovernedPolicyPath, policy.JSON(t))
	serveHarnessWriteFile(t, source, filepath.Join(".hn", "pipelines", "test.json"), serveReplicationPipelineJSON(t))
	serveHarnessWriteFile(t, source, "README", "Ping pong, served by hn serve.\n")
	serveHarnessCommit(t, source, "pingpong base")
	h.Serve("serve", "repo", "create", serveReplicationRepository, "--from", source)

	for _, name := range names {
		team.bind(name, team.actors[name])
		team.Git(name, "remote", "add", "origin", serveHarnessCloneURL(serveReplicationRepository))
		team.Git(name, "fetch", "-q", "origin")
		team.Git(name, "checkout", "-q", "-B", "main", "origin/main")
	}
	team.Base = team.ServerRef(serveReplicationMain)
	if len(team.Base) != len(servePushZero) {
		t.Fatalf("hosted main %q is not a SHA-1 object ID", team.Base)
	}
	return team
}

// serveReplicationPipelineJSON is pipeline test: the project's script must
// print pong. It stands in for the cargo pipeline of rust-pingpong.
func serveReplicationPipelineJSON(t *testing.T) string {
	t.Helper()
	encoded, err := json.MarshalIndent(map[string]any{
		"version": "hn.pipeline/0",
		"steps": []any{map[string]any{
			"name": "pingpong", "command": "sh",
			"args": []string{"-c", `test "$(sh ./` + serveReplicationScript + `)" = pong`}, "timeoutSeconds": 10,
		}},
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded) + "\n"
}

// enroll gives name a key, a client environment, a fresh repository and an
// hn identity. The key is bound later, once the actor is known.
func (team *serveReplicationTeam) enroll(name string) {
	team.t.Helper()
	team.keys[name] = newServeTestKey(team.t, name+"@pingpong")
	team.clients[name] = serveHarnessClient(team.t, team.h, team.keys[name], nil)
	work := filepath.Join(team.t.TempDir(), "work-"+name)
	team.run(name, "", "git", "init", "-q", "-b", "main", work)
	team.work[name] = work
	team.Hn(name, "init", "--name", "Pingpong "+name)
	team.actors[name] = operationalLabel(team.t, team.Hn(name, "identity", "public"), "Actor:")
	if len(team.actors[name]) != 64 {
		team.t.Fatalf("%s has actor %q", name, team.actors[name])
	}
}

// bind binds name's key to actor with hn serve key add.
func (team *serveReplicationTeam) bind(name, actor string) {
	team.t.Helper()
	key := team.keys[name]
	output := team.h.ServeWith(serveHarnessRun{Stdin: key.PublicLine + "\n"}, "serve", "key", "add", "--actor", actor, "-")
	if want := "Bound " + key.Fingerprint + " to actor " + actor + "\n"; output != want {
		team.t.Fatalf("hn serve key add output = %q, want %q", output, want)
	}
}

// FreshClone clones the hosted repository for a new participant name with
// a bound key and no Hubnot identity, and returns the clone.
func (team *serveReplicationTeam) FreshClone(name string) string {
	team.t.Helper()
	team.FreshClient(name)
	dir := filepath.Join(team.t.TempDir(), "clone-"+name)
	team.run(name, "", "git", "clone", "-q", serveHarnessCloneURL(serveReplicationRepository), dir)
	team.work[name] = dir
	if refs := runOperationalGit(team.t, dir, "for-each-ref", "refs/hn"); strings.TrimSpace(refs) != "" {
		team.t.Fatalf("fresh clone %s already holds Hubnot refs:\n%s", name, refs)
	}
	return dir
}

// FreshClient gives a new participant name a bound key, bound to a
// deterministic actor that never publishes, without a clone.
func (team *serveReplicationTeam) FreshClient(name string) {
	team.t.Helper()
	team.keys[name] = newServeTestKey(team.t, name+"@pingpong")
	team.clients[name] = serveHarnessClient(team.t, team.h, team.keys[name], nil)
	team.bind(name, serveHarnessActor("replication-"+name))
}

// run logs and runs one participant command in dir (the client HOME when
// empty) and returns its output and exit status.
func (team *serveReplicationTeam) run(who, dir, program string, args ...string) (string, int) {
	team.t.Helper()
	team.commands = append(team.commands, serveReplicationCommand{Who: who, Program: program, Args: slices.Clone(args)})
	binary := program
	if program == "hn" {
		binary = team.h.Binary
	}
	return team.clients[who].exec(dir, binary, args...)
}

// Exec runs one command in who's clone and returns its output and status.
func (team *serveReplicationTeam) Exec(who, program string, args ...string) (string, int) {
	team.t.Helper()
	return team.run(who, team.work[who], program, args...)
}

func (team *serveReplicationTeam) must(who, program string, args ...string) string {
	team.t.Helper()
	output, status := team.Exec(who, program, args...)
	if status != 0 {
		team.t.Fatalf("%s: %s %s exited %d:\n%s", who, program, strings.Join(args, " "), status, output)
	}
	return output
}

// Git runs git in who's clone and requires exit 0.
func (team *serveReplicationTeam) Git(who string, args ...string) string {
	team.t.Helper()
	return team.must(who, "git", args...)
}

// Hn runs hn in who's clone and requires exit 0.
func (team *serveReplicationTeam) Hn(who string, args ...string) string {
	team.t.Helper()
	return team.must(who, "hn", args...)
}

// Log is the command log, one command per line.
func (team *serveReplicationTeam) Log() string {
	lines := make([]string, 0, len(team.commands))
	for _, command := range team.commands {
		lines = append(lines, command.String())
	}
	return strings.Join(lines, "\n")
}

// TracePackets runs fn with Git's packet trace enabled for who's commands
// and returns the trace. Client push> lines show the ref updates and push
// options Git sent to the server.
func (team *serveReplicationTeam) TracePackets(who string, fn func()) string {
	team.t.Helper()
	trace := filepath.Join(team.t.TempDir(), "packets-"+who)
	client := team.clients[who]
	traced := client
	traced.Env = append(slices.Clone(client.Env), "GIT_TRACE_PACKET="+trace)
	team.clients[who] = traced
	defer func() { team.clients[who] = client }()
	fn()
	data, err := os.ReadFile(trace)
	if err != nil {
		team.t.Fatalf("read packet trace of %s: %v", who, err)
	}
	return string(data)
}

// SelectAllTrusted saves the all-trusted selection in who's clone.
func (team *serveReplicationTeam) SelectAllTrusted(who string) {
	team.t.Helper()
	assertOperationalContains(team.t, team.Hn(who, "replication", "select", "origin", "--all-trusted"),
		"compatibility-all: false\nall-trusted: true\n")
}

// Latest is the newest event of who's own actor chain.
func (team *serveReplicationTeam) Latest(who string) operationalEvent {
	team.t.Helper()
	return latestOperationalEvent(team.t, team.work[who], actorRef(team.actors[who]))
}

// LocalRef is the value of ref in who's clone.
func (team *serveReplicationTeam) LocalRef(who, ref string) string {
	team.t.Helper()
	return strings.TrimSpace(runOperationalGit(team.t, team.work[who], "rev-parse", ref))
}

// ServerRef is the hosted value of ref, or "" when it does not exist.
func (team *serveReplicationTeam) ServerRef(ref string) string {
	team.t.Helper()
	return team.ServerRefs(ref)[ref]
}

// ServerRefs maps every hosted ref matching the for-each-ref patterns to
// its value, read directly from the server's repository.
func (team *serveReplicationTeam) ServerRefs(patterns ...string) map[string]string {
	team.t.Helper()
	args := append([]string{"for-each-ref", "--format=%(objectname) %(refname)"}, patterns...)
	return serveReplicationRefMap(team.t, serveHarnessGit(team.t, team.hosted, args...))
}

// PushLog is the hosted repository's push log.
func (team *serveReplicationTeam) PushLog() []servePushLogEntry {
	team.t.Helper()
	return readServePushLog(team.t, team.h.Layout().RepoPushLog(serveReplicationRepository))
}

// Commit commits files on a new branch from main in who's clone and returns
// the commit. No files writes a pingpong script that prints pong.
func (team *serveReplicationTeam) Commit(who, branch string, files map[string]string) string {
	team.t.Helper()
	team.commits++
	if len(files) == 0 {
		files = map[string]string{
			serveReplicationScript:                "#!/bin/sh\necho pong\n",
			fmt.Sprintf("notes-%d", team.commits): fmt.Sprintf("change %d\n", team.commits),
		}
	}
	team.Git(who, "switch", "-q", "-c", branch, "main")
	for _, path := range slices.Sorted(maps.Keys(files)) {
		writeServeTestFile(team.t, filepath.Join(team.work[who], path), files[path], 0o644)
		team.Git(who, "add", "--", path)
	}
	team.Git(who, "commit", "-q", "-m", fmt.Sprintf("pingpong change %d", team.commits))
	head := team.LocalRef(who, "HEAD")
	team.Git(who, "switch", "-q", "main")
	return head
}

// OpenProposalLocally commits on branch, pushes the branch and opens a
// proposal for it without publishing the proposal.
func (team *serveReplicationTeam) OpenProposalLocally(who, branch, title string) operationalEvent {
	team.t.Helper()
	head := team.Commit(who, branch, nil)
	team.Git(who, "push", "-q", "origin", branch)
	team.Hn(who, "proposal", "open", "--base", "main", "--head", branch, "--body", "Opened by the replication team.", title)
	event := team.Latest(who)
	if event.Kind != "proposal.open" || event.Head != head || event.Base != team.LocalRef(who, "main") {
		team.t.Fatalf("hn proposal open recorded %+v, want head %s", event, head)
	}
	return event
}

// OpenProposal opens a proposal like OpenProposalLocally and publishes it
// with hn sync.
func (team *serveReplicationTeam) OpenProposal(who, branch, title string) operationalEvent {
	team.t.Helper()
	event := team.OpenProposalLocally(who, branch, title)
	team.Hn(who, "sync", "origin")
	if got := team.ServerRef(proposalRef(event.ID)); got != event.Head {
		team.t.Fatalf("hosted %s = %q after hn sync, want %s", proposalRef(event.ID), got, event.Head)
	}
	return event
}

// serveReplicationRefMap parses "OID REF" or "OID\tREF" lines.
func serveReplicationRefMap(t *testing.T, listing string) map[string]string {
	t.Helper()
	refs := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(listing), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 || !validGitOID(fields[0]) {
			t.Fatalf("malformed ref line %q", line)
		}
		refs[fields[1]] = fields[0]
	}
	return refs
}

// serveReplicationOutcomes are the "Replication KIND ID: STATUS" lines of
// an hn sync output, sorted.
func serveReplicationOutcomes(output string) []string {
	var outcomes []string
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "Replication ") {
			outcomes = append(outcomes, line)
		}
	}
	slices.Sort(outcomes)
	return outcomes
}

// serveReplicationPromoted are the promoted outcome lines for actors and
// proposals, sorted.
func serveReplicationPromoted(actors []string, proposals []string) []string {
	var lines []string
	for _, actor := range actors {
		lines = append(lines, "Replication actor "+actor+": promoted")
	}
	for _, proposal := range proposals {
		lines = append(lines, "Replication proposal "+proposal+": promoted")
	}
	slices.Sort(lines)
	return lines
}

func requireServeReplicationOutcomes(t *testing.T, output string, want []string) {
	t.Helper()
	if got := serveReplicationOutcomes(output); !slices.Equal(got, want) {
		t.Fatalf("replication outcomes:\n%s\nwant:\n%s\nfull output:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"), output)
	}
}

// Command log checks (SC-004). They read every argument a participant
// passed, so a selection naming an actor or a proposal, or a push that
// could update main, cannot hide in a long flow.

// serveReplicationNamesActorOrProposal reports whether args, the arguments
// of one hn command, are a replication select that names an actor or a
// proposal. Go flags accept one or two dashes and an attached value.
func serveReplicationNamesActorOrProposal(args []string) bool {
	if len(args) < 2 || args[0] != "replication" || args[1] != "select" {
		return false
	}
	for _, arg := range args[2:] {
		name, _, _ := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if strings.HasPrefix(arg, "-") && (name == "actor" || name == "proposal") {
			return true
		}
	}
	return false
}

// serveReplicationSelectsAllTrustedOnly reports whether args, the
// arguments of one hn replication select, save the all-trusted selection
// and nothing broader or narrower.
func serveReplicationSelectsAllTrustedOnly(args []string) bool {
	if len(args) < 2 || args[0] != "replication" || args[1] != "select" || serveReplicationNamesActorOrProposal(args) {
		return false
	}
	allTrusted := false
	for _, arg := range args[2:] {
		switch strings.TrimLeft(arg, "-") {
		case "all-trusted":
			allTrusted = strings.HasPrefix(arg, "-")
		case "all":
			return false
		}
	}
	return allTrusted
}

// serveReplicationPushesMain reports whether args, the arguments of one git
// command, are a push that could update refs/heads/main. It is
// conservative: a push without an explicit refspec, a push of HEAD without
// a destination, --all and --mirror all count as pushes of main.
func serveReplicationPushesMain(args []string) bool {
	index := 0
	for index < len(args) && strings.HasPrefix(args[index], "-") {
		switch args[index] {
		case "-C", "-c", "--git-dir", "--work-tree", "--namespace":
			index++
		}
		index++
	}
	if index >= len(args) || args[index] != "push" {
		return false
	}
	var positional []string
	for rest := args[index+1:]; len(rest) > 0; rest = rest[1:] {
		arg := rest[0]
		switch {
		case arg == "--all" || arg == "--mirror" || arg == "--branches":
			return true
		case arg == "-o" || arg == "--push-option" || arg == "--repo" || arg == "--receive-pack" || arg == "--exec":
			if len(rest) > 1 {
				rest = rest[1:]
			}
		case arg == "--":
			positional = append(positional, rest[1:]...)
			rest = rest[len(rest)-1:]
		case strings.HasPrefix(arg, "-"):
		default:
			positional = append(positional, arg)
		}
	}
	if len(positional) < 2 {
		return true
	}
	for _, refspec := range positional[1:] {
		refspec = strings.TrimPrefix(refspec, "+")
		source, destination, mapped := strings.Cut(refspec, ":")
		if !mapped {
			destination = source
		}
		switch destination {
		case "", "main", serveReplicationMain, "HEAD", "@":
			return true
		}
	}
	return false
}

// requireServeReplicationLog checks the command log of each participant in
// who: every hn replication select saves only the all-trusted selection,
// none names an actor or a proposal, and no git push can update main. It
// returns the number of select commands each participant ran.
func (team *serveReplicationTeam) requireServeReplicationLog(who ...string) map[string]int {
	team.t.Helper()
	selects := map[string]int{}
	for _, command := range team.commands {
		if !slices.Contains(who, command.Who) {
			continue
		}
		switch command.Program {
		case "hn":
			if serveReplicationNamesActorOrProposal(command.Args) {
				team.t.Fatalf("%s ran a replication select naming an actor or proposal: %s\nlog:\n%s", command.Who, command, team.Log())
			}
			if len(command.Args) >= 2 && command.Args[0] == "replication" && command.Args[1] == "select" {
				if !serveReplicationSelectsAllTrustedOnly(command.Args) {
					team.t.Fatalf("%s saved a selection other than --all-trusted: %s\nlog:\n%s", command.Who, command, team.Log())
				}
				selects[command.Who]++
			}
		case "git":
			if serveReplicationPushesMain(command.Args) {
				team.t.Fatalf("%s ran a git push that can update main: %s\nlog:\n%s", command.Who, command, team.Log())
			}
		}
	}
	return selects
}

// The command log checks catch hidden selections and direct main pushes,
// so the SC-004 assertions are not vacuous.
func TestServeReplicationCommandLogChecks(t *testing.T) {
	actor := strings.Repeat("a", 64)
	for _, test := range []struct {
		args       []string
		names      bool
		allTrusted bool
	}{
		{[]string{"replication", "select", "origin", "--all-trusted"}, false, true},
		{[]string{"replication", "select", "--all-trusted"}, false, true},
		{[]string{"replication", "select", "origin", "-all-trusted"}, false, true},
		{[]string{"replication", "select", "origin", "--all-trusted", "--actor", actor}, true, false},
		{[]string{"replication", "select", "origin", "--all-trusted", "--actor=" + actor}, true, false},
		{[]string{"replication", "select", "origin", "--all-trusted", "-actor", actor}, true, false},
		{[]string{"replication", "select", "origin", "--all-trusted", "--max-events", "5", "--proposal", "sha256:" + actor}, true, false},
		{[]string{"replication", "select", "origin", "-proposal=sha256:" + actor}, true, false},
		{[]string{"replication", "select", "origin", "--all"}, false, false},
		{[]string{"replication", "select", "origin", "--all-trusted", "--all"}, false, false},
		{[]string{"replication", "select", "origin", "--memory", "sha256:" + actor}, false, false},
		{[]string{"replication", "show", "origin", "--actor", actor}, false, false},
		{[]string{"issue", "open", "--actor", actor, "title"}, false, false},
	} {
		if got := serveReplicationNamesActorOrProposal(test.args); got != test.names {
			t.Errorf("names actor or proposal %q = %t, want %t", test.args, got, test.names)
		}
		if got := serveReplicationSelectsAllTrustedOnly(test.args); got != test.allTrusted {
			t.Errorf("all-trusted only %q = %t, want %t", test.args, got, test.allTrusted)
		}
	}
	for _, test := range []struct {
		args []string
		main bool
	}{
		{[]string{"push", "origin", "feature/serve-ball"}, false},
		{[]string{"push", "-q", "-u", "origin", "feature/serve-ball"}, false},
		{[]string{"push", "origin", "HEAD:refs/heads/feature/x"}, false},
		{[]string{"push", "origin", "refs/hn/actors/" + actor + ":refs/hn/actors/" + actor}, false},
		{[]string{"push", "--push-option", "main", "origin", "feature/x"}, false},
		{[]string{"-C", "main", "push", "origin", "feature/x"}, false},
		{[]string{"fetch", "origin", "main"}, false},
		{[]string{"log", "main"}, false},
		{[]string{"push", "origin", "main"}, true},
		{[]string{"push", "origin", "HEAD:main"}, true},
		{[]string{"push", "origin", "+abc123:refs/heads/main"}, true},
		{[]string{"push", "--force", "origin", "feature/x:main"}, true},
		{[]string{"push", "origin", "feature/x", "HEAD"}, true},
		{[]string{"push", "origin", "--delete", "main"}, true},
		{[]string{"push", "origin", ":main"}, true},
		{[]string{"push", "origin", "+main"}, true},
		{[]string{"push", "origin", "--", "main"}, true},
		{[]string{"push", "origin"}, true},
		{[]string{"push"}, true},
		{[]string{"push", "--all", "origin"}, true},
		{[]string{"push", "--mirror", "origin"}, true},
		{[]string{"-C", "work", "push", "origin", "HEAD:main"}, true},
		{[]string{"-c", "push.default=current", "push", "origin"}, true},
	} {
		if got := serveReplicationPushesMain(test.args); got != test.main {
			t.Errorf("pushes main %q = %t, want %t", test.args, got, test.main)
		}
	}
}

// T074, FR-017: a hosted repository with two published actors and two
// proposals advertises the marker and exactly the actor chains and
// proposal refs it accepted, and nothing it refused. A client identifies
// it through the forced command, never imports the marker, and every
// advertised fact also promotes under exact selections.
func TestServeReplicationAdvertisesAcceptedFacts(t *testing.T) {
	team := newServeReplicationTeam(t, "intruder")
	implementer, maintainer := team.actors[serveReplicationImplementer], team.actors[serveReplicationMaintainer]
	first := team.OpenProposal(serveReplicationImplementer, "feature/first", "First change")
	second := team.OpenProposal(serveReplicationImplementer, "feature/second", "Second change")
	team.Hn(serveReplicationMaintainer, "issue", "open", "Track the serve rules")
	team.Hn(serveReplicationMaintainer, "sync", "origin")
	marker := team.ServerRef(serveMarkerRef)
	want := map[string]string{
		serveMarkerRef:         marker,
		actorRef(implementer):  team.LocalRef(serveReplicationImplementer, actorRef(implementer)),
		actorRef(maintainer):   team.LocalRef(serveReplicationMaintainer, actorRef(maintainer)),
		proposalRef(first.ID):  first.Head,
		proposalRef(second.ID): second.Head,
	}
	if blob, err := serveMarkerObjectID(team.hosted); err != nil || marker != blob {
		t.Fatalf("hosted marker = %q, want the hn-serve/1 blob %q (%v)", marker, blob, err)
	}

	// Two refused refs: the intruder's chain under an actor its key is not
	// bound to, and a proposal ref whose candidate is not hosted.
	before := team.ServerRefs()
	team.Hn("intruder", "issue", "open", "Pretend to be someone else")
	foreign := actorRef(serveHarnessActor("never-bound"))
	output, status := team.Exec("intruder", "git", "push", "origin", actorRef(team.actors["intruder"])+":"+foreign)
	if status == 0 {
		t.Fatalf("push of a foreign actor ref succeeded:\n%s", output)
	}
	requireServePushRejections(t, output, team.actors["intruder"], map[string]string{foreign: serveReasonActorKeyMismatch})
	third := team.OpenProposalLocally(serveReplicationImplementer, "feature/third", "Third change")
	refused := proposalRef(third.ID)
	output, status = team.Exec(serveReplicationImplementer, "git", "push", "origin", refused+":"+refused)
	if status == 0 {
		t.Fatalf("push of a proposal ref without its candidate succeeded:\n%s", output)
	}
	requireServePushRejections(t, output, implementer, map[string]string{refused: serveReasonDependencyMissing})
	after := team.ServerRefs()
	delete(after, "refs/heads/feature/third")
	if !maps.Equal(before, after) {
		t.Fatalf("refused pushes changed hosted refs:\nbefore %v\nafter  %v", before, after)
	}

	// The server holds exactly the accepted facts and advertises all of
	// them through the forced command, and nothing else under refs/hn/.
	if held := team.ServerRefs("refs/hn"); !maps.Equal(held, want) {
		t.Fatalf("hosted refs/hn:\n%v\nwant\n%v", held, want)
	}
	watcher := team.FreshClone("watcher")
	advertised := serveReplicationRefMap(t, team.Git("watcher", "ls-remote", "origin", "refs/hn/*"))
	if !maps.Equal(advertised, want) {
		t.Fatalf("advertised refs/hn:\n%v\nwant\n%v", advertised, want)
	}
	for _, ref := range []string{foreign, refused} {
		if _, found := advertised[ref]; found {
			t.Fatalf("refused ref %s is advertised", ref)
		}
	}

	// Identification works through the forced command and protocol v2.
	team.SelectAllTrusted("watcher")
	synced := team.Hn("watcher", "sync", "origin")
	requireServeReplicationOutcomes(t, synced, serveReplicationPromoted([]string{implementer, maintainer}, []string{first.ID, second.ID}))
	imported := serveReplicationRefMap(t, runOperationalGit(t, watcher, "for-each-ref", "--format=%(objectname) %(refname)", "refs/hn"))
	wantImported := map[string]string{
		acceptedActorRef("origin", implementer):  want[actorRef(implementer)],
		acceptedActorRef("origin", maintainer):   want[actorRef(maintainer)],
		acceptedProposalRef("origin", first.ID):  first.Head,
		acceptedProposalRef("origin", second.ID): second.Head,
	}
	if !maps.Equal(imported, wantImported) {
		t.Fatalf("watcher refs/hn after sync:\n%v\nwant\n%v", imported, wantImported)
	}
	for _, ref := range strings.Fields(runOperationalGit(t, watcher, "for-each-ref", "--format=%(refname)")) {
		if strings.Contains(ref, "serve") {
			t.Fatalf("watcher holds %s, derived from the marker", ref)
		}
	}
	if output, status := team.Exec("watcher", "git", "cat-file", "-e", marker); status == 0 {
		t.Fatalf("watcher holds the marker blob %s:\n%s", marker, output)
	}

	// Every advertised actor chain and proposal passed the server's checks,
	// so each also promotes under exact selections in a scratch clone.
	team.FreshClone("exact")
	args := []string{"replication", "select", "origin"}
	var actors, proposals []string
	for _, ref := range slices.Sorted(maps.Keys(advertised)) {
		if id, ok := strings.CutPrefix(ref, serveActorRefPrefix); ok {
			actors = append(actors, id)
			args = append(args, "--actor", id)
		} else if hash, ok := strings.CutPrefix(ref, serveProposalRefPrefix); ok {
			proposals = append(proposals, "sha256:"+hash)
			args = append(args, "--proposal", "sha256:"+hash)
		}
	}
	team.Hn("exact", args...)
	requireServeReplicationOutcomes(t, team.Hn("exact", "sync", "origin"), serveReplicationPromoted(actors, proposals))
}

// T075, US4-1 and FR-018: a runner that saved only the all-trusted
// selection discovers a brand-new proposal and the maintainer's run
// request on its next sync, executes the request and publishes the
// result, without selecting any actor or proposal.
func TestServeReplicationRunnerDiscoversNewWork(t *testing.T) {
	team := newServeReplicationTeam(t)
	implementer, maintainer, runner := team.actors[serveReplicationImplementer], team.actors[serveReplicationMaintainer], team.actors[serveReplicationRunner]
	team.SelectAllTrusted(serveReplicationRunner)
	idle := team.Hn(serveReplicationRunner, "sync", "origin")
	requireServeReplicationOutcomes(t, idle, nil)
	assertOperationalContains(t, idle, "Synchronized 0 verified events with origin; promoted 0 selections\n")

	team.SelectAllTrusted(serveReplicationImplementer)
	proposal := team.OpenProposal(serveReplicationImplementer, "feature/serve-ball", "Serve the ball")
	team.SelectAllTrusted(serveReplicationMaintainer)
	requireServeReplicationOutcomes(t, team.Hn(serveReplicationMaintainer, "sync", "origin"),
		serveReplicationPromoted([]string{implementer}, []string{proposal.ID}))
	team.Hn(serveReplicationMaintainer, "run", "request", proposal.ID, "test")
	request := team.Latest(serveReplicationMaintainer)
	if request.Kind != "run.request" || request.Subject != proposal.ID || request.Pipeline != "test" {
		t.Fatalf("run request %+v, want pipeline test for %s", request, proposal.ID)
	}
	team.Hn(serveReplicationMaintainer, "sync", "origin")

	// The runner's next sync promotes the new proposal and the chain that
	// carries the run request.
	discovered := team.Hn(serveReplicationRunner, "sync", "origin")
	requireServeReplicationOutcomes(t, discovered, serveReplicationPromoted([]string{implementer, maintainer}, []string{proposal.ID}))
	if got := team.LocalRef(serveReplicationRunner, acceptedActorRef("origin", maintainer)); got != team.LocalRef(serveReplicationMaintainer, actorRef(maintainer)) {
		t.Fatalf("runner promoted the maintainer's chain at %s, not the one carrying request %s", got, request.ID)
	}

	ran := team.Hn(serveReplicationRunner, "runner", "once", "--accept-pipeline", "test", "--accept-actor", maintainer,
		"--backend", "host", "--allow-unsafe-host-execution")
	result := team.Latest(serveReplicationRunner)
	if result.Kind != "run.result" || result.Subject != request.ID {
		t.Fatalf("runner recorded %+v, want a result for %s", result, request.ID)
	}
	assertOperationalContains(t, ran,
		"Runner accepted request "+shortID(request.ID)+" from "+shortID(maintainer)+" for pipeline test\n",
		"Runner recorded passed result "+shortID(result.ID)+" with host backend\n")
	logged := len(team.PushLog())
	team.Hn(serveReplicationRunner, "sync", "origin")

	// The server accepted the runner's chain; hn runner once already
	// published it, so the final sync has nothing to push.
	if got, want := team.ServerRef(actorRef(runner)), team.LocalRef(serveReplicationRunner, actorRef(runner)); got != want {
		t.Fatalf("hosted runner chain = %q, want %s", got, want)
	}
	published := false
	for _, entry := range team.PushLog()[:logged] {
		for _, update := range entry.Updates {
			if update.Ref == actorRef(runner) && update.New == team.ServerRef(actorRef(runner)) {
				published = entry.Verdict == servePushVerdictAccepted && entry.Actor == runner && entry.Key == team.keys[serveReplicationRunner].Fingerprint
			}
		}
	}
	if !published {
		t.Fatalf("push log has no accepted push of the runner's chain by the runner: %+v", team.PushLog())
	}
	team.Hn(serveReplicationMaintainer, "sync", "origin")
	assertOperationalContains(t, team.Hn(serveReplicationMaintainer, "proposal", "status", proposal.ID), "Pipeline: test 1/1 trusted passes")

	if selects := team.requireServeReplicationLog(serveReplicationRunner, serveReplicationMaintainer, serveReplicationImplementer); selects[serveReplicationRunner] != 1 {
		t.Fatalf("runner ran %d replication selects, want 1", selects[serveReplicationRunner])
	}
}

// T076, US4-2: actor X has a bound key and has never published. Naming X
// next to --all-trusted reports it as not advertised yet without failing,
// and the all-trusted selection alone never mentions X.
func TestServeReplicationUnpublishedActorIsNotAFailure(t *testing.T) {
	team := newServeReplicationTeam(t, "x")
	x := team.actors["x"]
	team.Hn(serveReplicationMaintainer, "issue", "open", "Welcome the new participant")
	team.Hn(serveReplicationMaintainer, "sync", "origin")
	proposal := team.OpenProposal(serveReplicationImplementer, "feature/serve-ball", "Serve the ball")
	if got := team.ServerRef(actorRef(x)); got != "" {
		t.Fatalf("X's chain is hosted at %s; X must never have pushed", got)
	}
	promoted := serveReplicationPromoted([]string{team.actors[serveReplicationImplementer], team.actors[serveReplicationMaintainer]}, []string{proposal.ID})

	team.FreshClone("named")
	team.Hn("named", "replication", "select", "origin", "--all-trusted", "--actor", x)
	named := team.Hn("named", "sync", "origin")
	requireServeReplicationOutcomes(t, named, slices.Sorted(slices.Values(append(slices.Clone(promoted), "Replication actor "+x+": not advertised yet"))))
	assertAllTrustedOmits(t, named, "failed", "hn:")

	team.FreshClone("plain")
	team.SelectAllTrusted("plain")
	plain := team.Hn("plain", "sync", "origin")
	requireServeReplicationOutcomes(t, plain, promoted)
	assertAllTrustedOmits(t, plain, x, "failed", "not advertised")
}

// T076, FR-019: exact selections, --all and the no-selection mode produce
// the same printed outcomes and the same accepted refs against hn serve as
// against an ordinary bare remote holding exactly the refs the hosted
// repository advertises.
func TestServeReplicationSelectionsMatchOrdinaryRemote(t *testing.T) {
	team := newServeReplicationTeam(t, "x")
	maintainer, implementer, reviewer := team.actors[serveReplicationMaintainer], team.actors[serveReplicationImplementer], team.actors[serveReplicationReviewer]
	team.Hn(serveReplicationMaintainer, "issue", "open", "Parity issue")
	memory := serveReplicationMemoryResult(t, team.Hn(serveReplicationMaintainer, "memory", "record", "--kind", "decision", "--at", "main",
		"--applies", "exact", "--content", "Serve parity is checked against an ordinary remote.", "--json"))
	team.Hn(serveReplicationMaintainer, "sync", "origin")
	first := team.OpenProposal(serveReplicationImplementer, "feature/first", "First change")
	second := team.OpenProposal(serveReplicationImplementer, "feature/second", "Second change")
	team.SelectAllTrusted(serveReplicationReviewer)
	team.Hn(serveReplicationReviewer, "sync", "origin")
	team.Hn(serveReplicationReviewer, "review", first.ID, "--approve", "--body", "Parity approval.")
	team.Hn(serveReplicationReviewer, "sync", "origin")
	if team.ServerRef(serveReplicationMemoryRef(t, maintainer, memory.Stream)) == "" {
		t.Fatalf("memory stream %s is not hosted", memory.Stream)
	}

	// The ordinary remote receives every advertised ref, the marker
	// included, through an ordinary fetch over the forced command.
	team.FreshClient("mirror")
	ordinary := filepath.Join(t.TempDir(), "ordinary.git")
	team.Git("mirror", "init", "-q", "--bare", ordinary)
	team.Git("mirror", "--git-dir="+ordinary, "fetch", "-q", serveHarnessCloneURL(serveReplicationRepository), "+refs/*:refs/*")
	team.Git("mirror", "--git-dir="+ordinary, "symbolic-ref", "HEAD", serveReplicationMain)
	hostedListing := team.Git("mirror", "ls-remote", serveHarnessCloneURL(serveReplicationRepository))
	if ordinaryListing := team.Git("mirror", "ls-remote", ordinary); ordinaryListing != hostedListing {
		t.Fatalf("ordinary remote advertises:\n%s\nhosted repository advertises:\n%s", ordinaryListing, hostedListing)
	}
	for _, ref := range []string{serveMarkerRef, actorRef(maintainer), actorRef(implementer), actorRef(reviewer), proposalRef(first.ID), proposalRef(second.ID), serveReplicationMemoryRef(t, maintainer, memory.Stream)} {
		if !strings.Contains(hostedListing, "\t"+ref+"\n") {
			t.Fatalf("hosted repository does not advertise %s:\n%s", ref, hostedListing)
		}
	}

	exact := []string{"--actor", maintainer, "--actor", implementer, "--actor", reviewer,
		"--proposal", first.ID, "--proposal", second.ID, "--memory", memory.Stream}
	for _, scenario := range []struct {
		name      string
		selection []string // nil: no saved selection
		status    int
		refs      int
		printed   string // one outcome line both remotes must print
	}{
		{"exact", exact, 0, 6, "Replication memory " + memory.Stream + ": promoted\n"},
		{"exact with an unpublished actor", append(slices.Clone(exact), "--actor", team.actors["x"]), 1, 6,
			"Replication actor " + team.actors["x"] + ": failed (" + replicationDependencyMissing + ")"},
		{"all", []string{"--all"}, 0, 6, "Replication memory " + memory.Stream + ": promoted\n"},
		{"no selection", nil, 0, 6, "Replication proposal " + second.ID + ": promoted\n"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			type observed struct {
				output string
				status int
				refs   map[string]string
			}
			observe := func(remote, url string) observed {
				dir := filepath.Join(t.TempDir(), remote)
				team.work["mirror"] = ""
				team.Git("mirror", "clone", "-q", "-o", remote, url, dir)
				team.work["mirror"] = dir
				if scenario.selection != nil {
					team.Hn("mirror", append([]string{"replication", "select", remote}, scenario.selection...)...)
				}
				output, status := team.Exec("mirror", "hn", "sync", remote)
				prefix := "refs/hn/remotes/" + remote + "/"
				refs := map[string]string{}
				for ref, value := range serveReplicationRefMap(t, runOperationalGit(t, dir, "for-each-ref", "--format=%(objectname) %(refname)", prefix)) {
					refs[strings.TrimPrefix(ref, prefix)] = value
				}
				return observed{strings.ReplaceAll(output, remote, "REMOTE"), status, refs}
			}
			hosted := observe("hosted", serveHarnessCloneURL(serveReplicationRepository))
			plain := observe("plain", ordinary)
			if hosted.output != plain.output || hosted.status != plain.status {
				t.Fatalf("hn serve exited %d:\n%s\nordinary remote exited %d:\n%s", hosted.status, hosted.output, plain.status, plain.output)
			}
			if !maps.Equal(hosted.refs, plain.refs) {
				t.Fatalf("accepted refs differ:\nhn serve %v\nordinary %v", hosted.refs, plain.refs)
			}
			if hosted.status != scenario.status || len(hosted.refs) != scenario.refs || !strings.Contains(hosted.output, scenario.printed) {
				t.Fatalf("both remotes exited %d with %d accepted refs, want %d with %d and %q:\n%s", hosted.status, len(hosted.refs), scenario.status, scenario.refs, scenario.printed, hosted.output)
			}
		})
	}
}

// serveReplicationMemoryRef is the ref of actor's memory stream.
func serveReplicationMemoryRef(t *testing.T, actor, stream string) string {
	t.Helper()
	ref, err := memoryRef(actor, stream)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

// serveReplicationMemoryResult decodes hn memory record --json output.
func serveReplicationMemoryResult(t *testing.T, output string) memoryCommandResultV0 {
	t.Helper()
	var result memoryCommandResultV0
	if err := json.Unmarshal([]byte(output), &result); err != nil || !validMemoryStreamID(result.Stream) {
		t.Fatalf("decode hn memory record output: %v\n%s", err, output)
	}
	return result
}

// serveReplicationIssue is the part of hn issue show --json the flow reads.
type serveReplicationIssue struct {
	Data struct {
		ID      string     `json:"id"`
		State   IssueState `json:"state"`
		HeadIDs []string   `json:"head_ids"`
	} `json:"data"`
}

func decodeServeReplicationIssue(t *testing.T, output string) serveReplicationIssue {
	t.Helper()
	var issue serveReplicationIssue
	if err := json.Unmarshal([]byte(output), &issue); err != nil || issue.Data.ID == "" {
		t.Fatalf("decode hn issue show --json: %v\n%s", err, output)
	}
	return issue
}

// T077, SC-004, US4-6 and NFR-006: the rust-pingpong Phase 0 flow with its
// five roles completes against hn serve. Every participant saves only the
// all-trusted selection, nobody pushes main, the server moves main exactly
// once, through the maintainer's hn merge --remote, and a fresh clone with
// no identity reproduces the evidence and sees the issue closed.
func TestServeReplicationPhaseZeroFlow(t *testing.T) {
	team := newServeReplicationTeam(t, serveReplicationOrchestrator)
	participants := []string{serveReplicationMaintainer, serveReplicationOrchestrator, serveReplicationImplementer, serveReplicationReviewer, serveReplicationRunner}
	maintainer := team.actors[serveReplicationMaintainer]
	for _, who := range participants {
		team.SelectAllTrusted(who)
	}
	act := func(who string, args ...string) string {
		t.Helper()
		team.Hn(who, "sync", "origin")
		output := team.Hn(who, args...)
		team.Hn(who, "sync", "origin")
		return output
	}

	// The orchestrator opens a signed issue with an acceptance criterion.
	opened := act(serveReplicationOrchestrator, "issue", "open", "--body", "Serve the ball and answer with pong.", "Serve the ball")
	issueEvent := team.Latest(serveReplicationOrchestrator)
	if issueEvent.Kind != "issue.open" || !strings.Contains(opened, shortID(issueEvent.ID)) {
		t.Fatalf("hn issue open printed:\n%s\nand recorded %+v", opened, issueEvent)
	}

	// The implementer reads the issue, implements it on a branch, pushes
	// the branch and opens a proposal.
	team.Hn(serveReplicationImplementer, "sync", "origin")
	read := decodeServeReplicationIssue(t, team.Hn(serveReplicationImplementer, "issue", "show", issueEvent.ID, "--json"))
	if read.Data.State.Title != "Serve the ball" || read.Data.State.Status != "open" || !strings.Contains(read.Data.State.Body, "pong") {
		t.Fatalf("implementer read issue %+v", read.Data)
	}
	team.Git(serveReplicationImplementer, "switch", "-q", "-c", "feature/serve-ball")
	writeServeTestFile(t, filepath.Join(team.work[serveReplicationImplementer], serveReplicationScript), "#!/bin/sh\necho pong\n", 0o644)
	team.Git(serveReplicationImplementer, "add", serveReplicationScript)
	team.Git(serveReplicationImplementer, "commit", "-q", "-m", "Serve the ball\n\nImplements "+issueEvent.ID+".")
	head := team.LocalRef(serveReplicationImplementer, "HEAD")
	team.Git(serveReplicationImplementer, "push", "-q", "-u", "origin", "feature/serve-ball")
	act(serveReplicationImplementer, "proposal", "open", "--base", "main", "--head", "feature/serve-ball",
		"--body", "Implements "+issueEvent.ID+".", "Serve the ball")
	candidate := team.Latest(serveReplicationImplementer)
	if candidate.Kind != "proposal.open" || candidate.Head != head || candidate.Base != team.Base {
		t.Fatalf("proposal %+v, want %s on %s", candidate, head, team.Base)
	}

	// The maintainer requests CI and the runner executes it.
	act(serveReplicationMaintainer, "run", "request", candidate.ID, "test")
	request := team.Latest(serveReplicationMaintainer)
	if request.Kind != "run.request" || request.Subject != candidate.ID {
		t.Fatalf("run request %+v", request)
	}
	ran := act(serveReplicationRunner, "runner", "once", "--accept-pipeline", "test", "--accept-actor", maintainer,
		"--backend", "host", "--allow-unsafe-host-execution")
	result := team.Latest(serveReplicationRunner)
	if result.Kind != "run.result" || result.Subject != request.ID || !strings.Contains(ran, "Runner recorded passed result "+shortID(result.ID)) {
		t.Fatalf("runner printed:\n%s\nand recorded %+v", ran, result)
	}

	// The reviewer approves the exact head; the maintainer accepts and
	// advances main on the server.
	act(serveReplicationReviewer, "review", candidate.ID, "--approve", "--body", "pingpong.sh answers pong.")
	approval := team.Latest(serveReplicationReviewer)
	act(serveReplicationMaintainer, "decide", candidate.ID, "--accept", "--body", "Accepted for the Phase 0 flow.")
	acceptance := team.Latest(serveReplicationMaintainer)
	if approval.Kind != "review.submit" || acceptance.Kind != "proposal.decision" {
		t.Fatalf("approval %+v, acceptance %+v", approval, acceptance)
	}
	assertOperationalContains(t, team.Hn(serveReplicationMaintainer, "proposal", "status", candidate.ID), "Status:   accepted")
	logged := len(team.PushLog())
	var merged string
	packets := team.TracePackets(serveReplicationMaintainer, func() {
		merged = team.Hn(serveReplicationMaintainer, "merge", candidate.ID, "--remote", "origin")
	})
	advancedBy := team.PushLog()[logged:]
	mergeFact := team.Latest(serveReplicationMaintainer)
	if want := serveAdvanceSuccessLines("origin", head, candidate.ID, mergeFact.ID, 1); merged != want {
		t.Fatalf("hn merge --remote printed:\n%s\nwant:\n%s", merged, want)
	}
	if mergeFact.Kind != "proposal.merged" || mergeFact.Commit != head {
		t.Fatalf("merge fact %+v, want commit %s", mergeFact, head)
	}
	if tip := team.ServerRef(serveReplicationMain); tip != head {
		t.Fatalf("hosted main = %s, want the tested head %s", tip, head)
	}

	// The orchestrator closes the issue.
	team.Hn(serveReplicationOrchestrator, "sync", "origin")
	current := decodeServeReplicationIssue(t, team.Hn(serveReplicationOrchestrator, "issue", "show", issueEvent.ID, "--json"))
	if len(current.Data.HeadIDs) != 1 {
		t.Fatalf("issue heads %q, want one", current.Data.HeadIDs)
	}
	team.Hn(serveReplicationOrchestrator, "issue", "close", issueEvent.ID, "--expect", current.Data.HeadIDs[0], "--json")
	team.Hn(serveReplicationOrchestrator, "sync", "origin")

	// SC-004 from the command log: each participant saved the all-trusted
	// selection once, nobody named an actor or a proposal, and no git
	// push could update main.
	selects := team.requireServeReplicationLog(participants...)
	for _, who := range participants {
		if selects[who] != 1 {
			t.Fatalf("%s ran %d replication selects, want 1\nlog:\n%s", who, selects[who], team.Log())
		}
	}
	// SC-004 from the server push log: main moved exactly once, by the
	// maintainer's key, during hn merge --remote, which pushes with
	// hn-advance; no push to main was even attempted otherwise.
	var mainEntries []int
	entries := team.PushLog()
	for index, entry := range entries {
		for _, update := range entry.Updates {
			if update.Ref == serveReplicationMain {
				mainEntries = append(mainEntries, index)
				break
			}
		}
	}
	if len(mainEntries) != 1 {
		t.Fatalf("push log has %d entries touching main, want 1: %+v", len(mainEntries), entries)
	}
	entry := entries[mainEntries[0]]
	if want := []servePushLogUpdate{{Ref: serveReplicationMain, Old: team.Base, New: head}}; entry.Verdict != servePushVerdictAccepted ||
		entry.Actor != maintainer || entry.Key != team.keys[serveReplicationMaintainer].Fingerprint || !slices.Equal(entry.Updates, want) {
		t.Fatalf("main push log entry %+v, want the maintainer's accepted advancement %+v", entry, want)
	}
	if mainEntries[0] < logged || mainEntries[0] >= logged+len(advancedBy) {
		t.Fatalf("main moved in push log entry %d, outside hn merge --remote (entries %d to %d)", mainEntries[0], logged, logged+len(advancedBy)-1)
	}
	// The push log keeps no push options, so the packet trace of hn merge
	// --remote shows that its one update of main carried hn-advance for the
	// candidate and the expected tip.
	var mainUpdates, options []string
	for _, line := range strings.Split(packets, "\n") {
		_, sent, found := strings.Cut(line, " push> ")
		if !found {
			continue
		}
		if strings.HasPrefix(sent, "hn-") {
			options = append(options, sent)
		}
		// A ref update is "OLD NEW REF", the first followed by the
		// capabilities after a NUL that the trace may print as \0.
		fields := strings.Fields(sent)
		if len(fields) < 3 || !validGitOID(fields[0]) || !validGitOID(fields[1]) {
			continue
		}
		ref, _, _ := strings.Cut(strings.ReplaceAll(fields[2], "\x00", `\`), `\`)
		if ref == serveReplicationMain {
			mainUpdates = append(mainUpdates, fields[0]+" "+fields[1]+" "+ref)
		}
	}
	if want := []string{team.Base + " " + head + " " + serveReplicationMain}; !slices.Equal(mainUpdates, want) {
		t.Fatalf("hn merge --remote sent main updates %q, want %q\ntrace:\n%s", mainUpdates, want, packets)
	}
	if want := []string{servePushOptionAdvance + "=" + candidate.ID, servePushOptionExpected + "=" + team.Base}; !slices.Equal(options, want) {
		t.Fatalf("hn merge --remote sent push options %q, want %q\ntrace:\n%s", options, want, packets)
	}

	// NFR-006: a fresh clone with no identity verifies everything itself.
	verifier := team.FreshClone("verifier")
	team.SelectAllTrusted("verifier")
	synced := team.Hn("verifier", "sync", "origin")
	assertAllTrustedOmits(t, synced, "failed", "not advertised")
	status := team.Hn("verifier", "proposal", "status", candidate.ID)
	assertOperationalContains(t, status, "Status:   merged", "Code:     ✓ signed head available and matched",
		"Pipeline: test 1/1 trusted passes", "Decision: 1/1 accepts; 0 rejects")
	want := map[string][]string{
		"review.submit":     {approval.ID},
		"run.request":       {request.ID},
		"run.result":        {result.ID},
		"proposal.decision": {acceptance.ID},
		"proposal.merged":   {mergeFact.ID},
	}
	maintainerEvidence := serveAdvanceEvidence(serveAdvanceEvents(t, team.work[serveReplicationMaintainer]), candidate.ID)
	verifierEvidence := serveAdvanceEvidence(serveAdvanceEvents(t, verifier), candidate.ID)
	if len(maintainerEvidence) != len(want) || len(verifierEvidence) != len(want) {
		t.Fatalf("evidence kinds: maintainer %q, verifier %q, want %q", maintainerEvidence, verifierEvidence, want)
	}
	for kind, ids := range want {
		if !slices.Equal(maintainerEvidence[kind], ids) || !slices.Equal(verifierEvidence[kind], ids) {
			t.Fatalf("%s: maintainer %q, verifier %q, want %q", kind, maintainerEvidence[kind], verifierEvidence[kind], ids)
		}
	}
	closed := decodeServeReplicationIssue(t, team.Hn("verifier", "issue", "show", issueEvent.ID, "--json"))
	if closed.Data.State.Status != "closed" || closed.Data.State.Title != "Serve the ball" {
		t.Fatalf("verifier sees issue %+v, want it closed", closed.Data)
	}
}

// serveReplicationSSHTrap is a GIT_SSH program that refuses every
// connection. Git runs GIT_SSH only when neither GIT_SSH_COMMAND nor
// core.sshCommand is set, so a Git process that reaches the trap has lost
// the clone's configuration.
const serveReplicationSSHTrap = "ssh trap: the clone's core.sshCommand was not applied"

// UseCloneSSHConfig moves who's SSH settings from the environment into
// who's clone, the way a user with several identities on one Unix account
// selects a key per clone: core.sshCommand names the shim, ssh.variant
// keeps protocol v2, GIT_SSH_COMMAND and GIT_SSH_VARIANT are removed and
// GIT_SSH points at the trap.
func (team *serveReplicationTeam) UseCloneSSHConfig(who string) {
	team.t.Helper()
	client := team.clients[who]
	client.Env = serveReplicationCloneSSHEnv(team.t, client.Env)
	team.clients[who] = client
	if team.work[who] != "" {
		team.Git(who, "config", "core.sshCommand", serveHarnessShellQuote(client.Shim))
		team.Git(who, "config", "ssh.variant", "ssh")
	}
}

// serveReplicationCloneSSHEnv is env without GIT_SSH_COMMAND and
// GIT_SSH_VARIANT and with GIT_SSH set to the trap.
func serveReplicationCloneSSHEnv(t *testing.T, env []string) []string {
	t.Helper()
	trap := filepath.Join(t.TempDir(), "ssh-trap")
	writeServeTestFile(t, trap, "#!/bin/sh\necho \""+serveReplicationSSHTrap+"\" >&2\nexit 255\n", 0o700)
	kept := []string{"GIT_SSH=" + trap}
	for _, entry := range env {
		if !strings.HasPrefix(entry, "GIT_SSH_COMMAND=") && !strings.HasPrefix(entry, "GIT_SSH_VARIANT=") && !strings.HasPrefix(entry, "GIT_SSH=") {
			kept = append(kept, entry)
		}
	}
	return kept
}

// Maintainer review cycle 2 of WP12: participants that select their SSH
// key only with their clone's core.sshCommand import each other's actor
// chains and proposals with hn sync, and the maintainer advances main with
// hn merge --remote. The replication quarantine fetch used to run Git
// without the clone's configuration, so every exact fetch reached the
// default SSH program and failed.
func TestServeReplicationHonoursCloneSSHCommand(t *testing.T) {
	team := newServeReplicationTeam(t)
	participants := []string{serveReplicationMaintainer, serveReplicationImplementer, serveReplicationReviewer, serveReplicationRunner}
	maintainer, implementer := team.actors[serveReplicationMaintainer], team.actors[serveReplicationImplementer]
	reviewer, runner := team.actors[serveReplicationReviewer], team.actors[serveReplicationRunner]
	for _, who := range participants {
		team.UseCloneSSHConfig(who)
		team.SelectAllTrusted(who)
	}
	// Outside a clone, Git has no core.sshCommand and reaches the trap, so
	// the configuration in each clone is the only working SSH setting.
	trapped, status := team.run(serveReplicationMaintainer, "", "git", "ls-remote", serveHarnessCloneURL(serveReplicationRepository))
	if status == 0 || !strings.Contains(trapped, serveReplicationSSHTrap) {
		t.Fatalf("git ls-remote outside a clone exited %d without the trap:\n%s", status, trapped)
	}

	proposal := team.OpenProposal(serveReplicationImplementer, "feature/clone-ssh", "Serve over the clone's SSH command")
	requireServeReplicationOutcomes(t, team.Hn(serveReplicationMaintainer, "sync", "origin"),
		serveReplicationPromoted([]string{implementer}, []string{proposal.ID}))
	team.Hn(serveReplicationMaintainer, "run", "request", proposal.ID, "test")
	request := team.Latest(serveReplicationMaintainer)
	team.Hn(serveReplicationMaintainer, "sync", "origin")

	requireServeReplicationOutcomes(t, team.Hn(serveReplicationRunner, "sync", "origin"),
		serveReplicationPromoted([]string{implementer, maintainer}, []string{proposal.ID}))
	team.Hn(serveReplicationRunner, "runner", "once", "--accept-pipeline", "test", "--accept-actor", maintainer,
		"--backend", "host", "--allow-unsafe-host-execution")
	if result := team.Latest(serveReplicationRunner); result.Kind != "run.result" || result.Subject != request.ID {
		t.Fatalf("runner recorded %+v, want a result for %s", result, request.ID)
	}
	team.Hn(serveReplicationRunner, "sync", "origin")

	reviewed := team.Hn(serveReplicationReviewer, "sync", "origin")
	assertOperationalContains(t, reviewed, "Replication actor "+runner+": promoted\n", "Replication proposal "+proposal.ID+": promoted\n")
	assertAllTrustedOmits(t, reviewed, "failed", "hn:")
	team.Hn(serveReplicationReviewer, "review", proposal.ID, "--approve", "--body", "Imported over the clone's SSH command.")
	team.Hn(serveReplicationReviewer, "sync", "origin")

	decided := team.Hn(serveReplicationMaintainer, "sync", "origin")
	assertOperationalContains(t, decided, "Replication actor "+runner+": promoted\n", "Replication actor "+reviewer+": promoted\n")
	assertAllTrustedOmits(t, decided, "failed", "hn:")
	team.Hn(serveReplicationMaintainer, "decide", proposal.ID, "--accept", "--body", "Accepted over the clone's SSH command.")
	merged := team.Hn(serveReplicationMaintainer, "merge", proposal.ID, "--remote", "origin")
	mergeFact := team.Latest(serveReplicationMaintainer)
	if want := serveAdvanceSuccessLines("origin", proposal.Head, proposal.ID, mergeFact.ID, 1); merged != want {
		t.Fatalf("hn merge --remote printed:\n%s\nwant:\n%s", merged, want)
	}
	if tip := team.ServerRef(serveReplicationMain); tip != proposal.Head {
		t.Fatalf("hosted main = %s, want the proposal head %s", tip, proposal.Head)
	}

	// A verifier that clones with the SSH command in its clone's
	// configuration imports the merged proposal and its evidence.
	team.FreshClient("verifier")
	team.UseCloneSSHConfig("verifier")
	verifier := filepath.Join(t.TempDir(), "clone-verifier")
	team.run("verifier", "", "git", "clone", "-q", "-c", "core.sshCommand="+serveHarnessShellQuote(team.clients["verifier"].Shim),
		"-c", "ssh.variant=ssh", serveHarnessCloneURL(serveReplicationRepository), verifier)
	team.work["verifier"] = verifier
	team.SelectAllTrusted("verifier")
	verified := team.Hn("verifier", "sync", "origin")
	assertAllTrustedOmits(t, verified, "failed", "hn:")
	assertOperationalContains(t, team.Hn("verifier", "proposal", "status", proposal.ID), "Status:   merged", "Pipeline: test 1/1 trusted passes")
	team.requireServeReplicationLog(append(participants, "verifier")...)
}

// cloneTransportConfigEnvironment hands a Git process in another
// repository exactly the clone's local transport keys, keeps an implicit
// boolean true, lets inherited GIT_CONFIG_COUNT and git -c entries win as
// the command scope does in the clone, and leaves every other key behind.
func TestCloneTransportConfigEnvironment(t *testing.T) {
	global := filepath.Join(t.TempDir(), "global-config")
	writeServeTestFile(t, global, "[http]\n\tproxy = http://global.invalid\n", 0o600)
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	clone := filepath.Join(t.TempDir(), "clone")
	runOperationalGit(t, "", "init", "-q", clone)
	included := filepath.Join(t.TempDir(), "included-config")
	writeServeTestFile(t, included, "[credential]\n\thelper = store\n", 0o600)
	for _, setting := range [][]string{
		{"core.sshCommand", "ssh -i 'key file' -o IdentitiesOnly=yes"},
		{"ssh.variant", "ssh"},
		{"protocol.version", "2"},
		{"http.postBuffer", "1048576"},
		{"http.https://example.invalid/.extraHeader", "Authorization: Bearer clone-token"},
		{"include.path", included},
		{"core.hooksPath", "/clone/hooks"},
		{"remote.origin.uploadpack", "/clone/upload-pack"},
		{"url.ssh://elsewhere.invalid/.insteadOf", "ssh://example.invalid/"},
		{"user.name", "Clone User"},
	} {
		runOperationalGit(t, clone, "config", setting[0], setting[1])
	}
	config, err := os.OpenFile(filepath.Join(clone, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.WriteString("[http]\n\tsslVerify\n"); err != nil {
		t.Fatal(err)
	}
	if err := config.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "protocol.version")
	t.Setenv("GIT_CONFIG_VALUE_0", "1")
	t.Setenv("GIT_CONFIG_PARAMETERS", "'http.postbuffer'='2048'") // what an outer git -c passes on
	t.Chdir(clone)

	environment := cloneTransportConfigEnvironment()
	other := filepath.Join(t.TempDir(), "other.git")
	runOperationalGit(t, "", "init", "-q", "--bare", other)
	lookup := func(key string) string {
		t.Helper()
		command := exec.Command("git", "--git-dir="+other, "config", "--get", key)
		command.Env = append(os.Environ(), environment...)
		output, _ := command.Output()
		return strings.TrimSpace(string(output))
	}
	for key, want := range map[string]string{
		"core.sshCommand":  "ssh -i 'key file' -o IdentitiesOnly=yes",
		"ssh.variant":      "ssh",
		"http.postBuffer":  "2048", // an inherited git -c value still wins
		"protocol.version": "1",    // the inherited command scope is read last and wins
		"http.https://example.invalid/.extraHeader": "Authorization: Bearer clone-token",
		"http.sslVerify":                         "true",
		"credential.helper":                      "store",
		"http.proxy":                             "http://global.invalid",
		"core.hooksPath":                         "",
		"remote.origin.uploadpack":               "",
		"url.ssh://elsewhere.invalid/.insteadOf": "",
		"user.name":                              "",
		"include.path":                           "",
	} {
		if got := lookup(key); got != want {
			t.Errorf("%s in the other repository = %q, want %q", key, got, want)
		}
	}
	if slices.ContainsFunc(environment, func(entry string) bool { return strings.Contains(entry, "global.invalid") }) {
		t.Errorf("global configuration was copied into the environment: %q", environment)
	}

	empty := filepath.Join(t.TempDir(), "empty")
	runOperationalGit(t, "", "init", "-q", empty)
	t.Chdir(empty)
	if environment := cloneTransportConfigEnvironment(); environment != nil {
		t.Fatalf("clone without transport keys: environment %q, want none", environment)
	}
}
