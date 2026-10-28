package main

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Opt-out repositories and older clients (WP17, User Story 7, FR-022,
// NFR-004, C-003, C-004).
//
// TestStandingCompatOlderClient is the only test that builds or runs the
// older client (decision 01M5VSEHFPWQWFBNTP9ZFYP3TR). It builds hn from the
// older-client reference commit into its own temporary directory and runs
// every consumer as a sequential subtest, so the binary outlives them all
// and no package variable or cache holds its path. Facts are signed
// in-process with the standing harness, which changes the working
// directory, so no subtest may call t.Parallel.

// standingCompatOlderCommit is the older-client reference: an hn built
// from it predates this mission (decision 01M5VF0QVPQ871M0DA8Q0SG5CX).
const standingCompatOlderCommit = "7a47e6952a427ad7e3a3766c221fd334d9331950"

// standingCompatFrozenCommit is the implementation-start snapshot of the
// pre-existing test audit (decision 01M5VSEHF9P9VRVTY68PD8DZ7D).
const standingCompatFrozenCommit = "d881d9bcd0ade4fa1bce91061954a8582e8d9318"

// standingCompatUpstreamCommits are upstream releases merged after the
// snapshot. A test file that upstream changed is frozen at upstream's
// version instead: it may differ from the snapshot only where it equals the
// upstream file exactly.
var standingCompatUpstreamCommits = []string{
	"01a152cfbb822a7be9ccbc51dac34e3caa0cf925", // 0.0.2-dev and follow-up fixes
}

// standingCompatBuildTimeout bounds the older build, which competes with
// the race suite for the host.
const standingCompatBuildTimeout = 10 * time.Minute

// standingCompatAdditiveLines is the whole allow-list of output lines this
// mission adds for a repository without acceptance or reviewers (NFR-004).
// Command names the command family a line may appear in. After
// normalization, the newer client's output with these lines removed must
// equal the older client's output exactly, and the older client must print
// none of them.
var standingCompatAdditiveLines = []struct {
	Command string
	Line    *regexp.Regexp
}{
	{"automerge", regexp.MustCompile(`^Acceptance: maintainers \(maintainer acceptance required\)$`)},
	{"automerge", regexp.MustCompile(`^Counted reviewers: all trusted reviewers$`)},
	{"automerge", regexp.MustCompile(`^Counted approvals: \d+/\d+$`)},
	{"policy show", regexp.MustCompile(`^  Acceptance: maintainers \(maintainer acceptance required\)$`)},
	{"policy show", regexp.MustCompile(`^  Counted reviewers: all trusted reviewers$`)},
	{"policy check", regexp.MustCompile(`^  automerge rule \S+ acceptance: .+$`)},
	{"policy check", regexp.MustCompile(`^  automerge rule \S+ reviewers (added|removed): .+$`)},
	// Upstream 0.0.2-dev added audits after the older reference commit was
	// built; these lines come from that release, not from this mission.
	{"policy show", regexp.MustCompile(`^Audits: disabled$`)},
	{"policy check", regexp.MustCompile(`^  audits enabled: .+$`)},
	{"policy check", regexp.MustCompile(`^  audit scope: .+$`)},
	{"policy check", regexp.MustCompile(`^  audit (rule owners|requesters|executors) (added|removed): .+$`)},
}

// standingCompatPlaceholders normalizes the values that differ between two
// otherwise identical repositories: event IDs and digests, actor
// fingerprints, commit IDs, short IDs and timestamps. Longer forms come
// first so a short ID never eats part of a full one.
var standingCompatPlaceholders = []struct {
	Pattern *regexp.Regexp
	With    string
}{
	{regexp.MustCompile(`sha256:[0-9a-f]{64}`), "<id>"},
	{regexp.MustCompile(`\b[0-9a-f]{64}\b`), "<actor>"},
	{regexp.MustCompile(`\b[0-9a-f]{40}\b`), "<commit>"},
	{regexp.MustCompile(`\b[0-9a-f]{12}\b`), "<short>"},
	{regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})`), "<time>"},
}

// standingCompatRow is one observed old and new outcome for the table the
// parent test logs (T084 step 6).
type standingCompatRow struct {
	Step, Case, Older, Newer string
}

// standingCompatReport collects the rows of every subtest. The parent owns
// it and passes it down; it is never shared outside one parent run.
type standingCompatReport struct {
	rows []standingCompatRow
}

func (r *standingCompatReport) add(step, name, older, newer string) {
	r.rows = append(r.rows, standingCompatRow{Step: step, Case: name, Older: older, Newer: newer})
}

func (r *standingCompatReport) table() string {
	widths := [4]int{len("step"), len("case"), len("older client"), len("newer client")}
	for _, row := range r.rows {
		for index, cell := range []string{row.Step, row.Case, row.Older, row.Newer} {
			widths[index] = max(widths[index], len(cell))
		}
	}
	var out strings.Builder
	line := func(cells ...string) {
		for index, cell := range cells {
			fmt.Fprintf(&out, "| %-*s ", widths[index], cell)
		}
		out.WriteString("|\n")
	}
	line("step", "case", "older client", "newer client")
	line(strings.Repeat("-", widths[0]), strings.Repeat("-", widths[1]), strings.Repeat("-", widths[2]), strings.Repeat("-", widths[3]))
	for _, row := range r.rows {
		line(row.Step, row.Case, row.Older, row.Newer)
	}
	return out.String()
}

// standingCompatSourceGit runs git in the source repository and returns
// its output and error without failing the test, for skip decisions.
func standingCompatSourceGit(t *testing.T, source string, args ...string) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = source
	command.Env = operationalTestEnvironment(t.TempDir())
	command.Stdin = strings.NewReader("")
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return output, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return output, nil
}

// standingCompatExtract writes a git archive tar stream into destination,
// refusing any entry that would land outside it.
func standingCompatExtract(t *testing.T, archive []byte, destination string) int {
	t.Helper()
	reader := tar.NewReader(bytes.NewReader(archive))
	files := 0
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return files
		}
		if err != nil {
			t.Fatalf("read archive of %s: %v", standingCompatOlderCommit, err)
		}
		if header.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		name := filepath.Clean(filepath.FromSlash(header.Name))
		if filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
			t.Fatalf("archive entry %q leaves the extraction directory", header.Name)
		}
		target := filepath.Join(destination, name)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				t.Fatal(err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				t.Fatal(err)
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode).Perm()|0o200)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.Copy(file, reader); err != nil {
				file.Close()
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			files++
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(header.Linkname, target); err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatalf("archive entry %q has unsupported type %q", header.Name, header.Typeflag)
		}
	}
}

// standingCompatBuildEnvironment is the caller's environment with module
// and toolchain downloads disabled, so the older build needs no network.
func standingCompatBuildEnvironment() []string {
	environment := make([]string, 0, len(os.Environ())+3)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "GOFLAGS=") || strings.HasPrefix(entry, "GOTOOLCHAIN=") || strings.HasPrefix(entry, "GOPROXY=") {
			continue
		}
		environment = append(environment, entry)
	}
	return append(environment, "GOFLAGS=", "GOTOOLCHAIN=local", "GOPROXY=off")
}

// standingCompatBuildOlder builds hn from the older-client reference into
// the calling test's temporary directory and returns its path. Only
// TestStandingCompatOlderClient calls it.
func standingCompatBuildOlder(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("older-client check needs the full suite; run without -short")
	}
	source, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := standingCompatSourceGit(t, source, "cat-file", "-e", standingCompatOlderCommit+"^{commit}"); err != nil {
		t.Skipf("older-client reference commit %s is absent from this checkout (for example a shallow clone): %v", standingCompatOlderCommit, err)
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("older-client check builds commit %s and needs go on PATH: %v", standingCompatOlderCommit, err)
	}
	archive, err := standingCompatSourceGit(t, source, "archive", "--format=tar", standingCompatOlderCommit)
	if err != nil {
		t.Fatalf("archive older-client reference: %v", err)
	}
	directory := t.TempDir()
	tree := filepath.Join(directory, "source")
	files := standingCompatExtract(t, archive, tree)
	binary := filepath.Join(directory, "hn-older")

	ctx, cancel := context.WithTimeout(context.Background(), standingCompatBuildTimeout)
	defer cancel()
	version := exec.CommandContext(ctx, goTool, "version")
	version.Env = standingCompatBuildEnvironment()
	toolchain, err := version.CombinedOutput()
	if err != nil {
		t.Fatalf("go version: %v\n%s", err, toolchain)
	}
	build := exec.CommandContext(ctx, goTool, "build", "-o", binary, ".")
	build.Dir = tree
	build.Env = standingCompatBuildEnvironment()
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build older client from %s with %s: %v\n%s", standingCompatOlderCommit, strings.TrimSpace(string(toolchain)), err, output)
	}
	t.Logf("older client built from %s (%d files) with %s", standingCompatOlderCommit, files, strings.TrimSpace(string(toolchain)))
	return binary
}

// standingCompatRun runs binary in directory without failing the test.
func standingCompatRun(h *standingHarness, binary, directory string, args ...string) (string, int) {
	h.t.Helper()
	return standingHarnessRun(h.t, binary, directory, args...)
}

// standingCompatVerifier is a fresh full clone of the harness remote with
// no identity and binary's explicit --all selection for origin. It has not
// synced yet.
func standingCompatVerifier(h *standingHarness, binary, name string) string {
	h.t.Helper()
	verifier := filepath.Join(h.t.TempDir(), name)
	runOperationalGit(h.t, "", "clone", "-q", "--no-local", h.Remote(), verifier)
	configureOperationalGit(h.t, verifier, "Standing Compat "+name)
	if output, status := standingCompatRun(h, binary, verifier, "replication", "select", "origin", "--all"); status != 0 {
		h.t.Fatalf("%s replication select exited %d:\n%s", name, status, output)
	}
	return verifier
}

// standingCompatPromoted reports whether the verifier's accepted chain of
// role contains the chain commit.
func standingCompatPromoted(h *standingHarness, verifier, role, commit string) bool {
	h.t.Helper()
	accepted := standingFactAcceptedChain(h, verifier, role)
	if accepted == "" {
		return false
	}
	for _, listed := range strings.Fields(runOperationalGit(h.t, verifier, "rev-list", accepted)) {
		if listed == commit {
			return true
		}
	}
	return false
}

// standingCompatStatus runs hn proposal status for candidate in the
// verifier and reports whether it printed the merged status.
func standingCompatStatus(h *standingHarness, binary, verifier, candidate string) (string, int, bool) {
	h.t.Helper()
	output, status := standingCompatRun(h, binary, verifier, "proposal", "status", candidate)
	return output, status, status == 0 && strings.Contains(output, "Status:   merged\n")
}

// standingCompatChain is role's own chain in chain order, read with Git
// alone.
func standingCompatChain(h *standingHarness, role string) []standingHarnessFact {
	h.t.Helper()
	ref := operationalActorRef(h.Actor(role))
	if h.git(role, "for-each-ref", "--format=%(refname)", ref) == "" {
		return nil
	}
	var facts []standingHarnessFact
	for _, commit := range strings.Fields(h.git(role, "rev-list", "--reverse", ref)) {
		facts = append(facts, standingHarnessReadFact(h.t, runOperationalGitBytes(h.t, h.Work(role), "show", commit+":event.json")))
	}
	return facts
}

// standingCompatNormalize replaces the harness root and every value that
// differs between two harnesses with a placeholder.
func standingCompatNormalize(h *standingHarness, output string) string {
	output = strings.ReplaceAll(output, filepath.Dir(h.Work("M")), "<root>")
	for _, placeholder := range standingCompatPlaceholders {
		output = placeholder.Pattern.ReplaceAllString(output, placeholder.With)
	}
	return output
}

// standingCompatAdditive reports whether line is on the allow-list for
// command.
func standingCompatAdditive(command, line string) bool {
	for _, allowed := range standingCompatAdditiveLines {
		if allowed.Command == command && allowed.Line.MatchString(line) {
			return true
		}
	}
	return false
}

// standingCompatStep is one command of the opt-out script and what it
// printed.
type standingCompatStep struct {
	Name, Command string // Command keys the allow-list; "" allows nothing
	Succeeds      bool   // the expected exit, the same for both clients
	Output        string
	Status        int
}

// standingCompatOptOutScript runs the opt-out scenario of T081 in h, whose
// binary is the older or the newer client, on standingHarnessLegacyPolicy.
// Every file name and title is fixed, so two runs differ only in
// normalized values.
func standingCompatOptOutScript(h *standingHarness) []standingCompatStep {
	h.t.Helper()
	base := h.Base()
	ready := h.readyCase("A", base, map[string]string{"src/optout-ready.txt": "opt-out ready change\n"})
	h.Accept("M", ready.Candidate.ID)
	second := h.readyCase("A", base, map[string]string{"tests/optout-second.txt": "opt-out second change\n"})
	outside := h.readyCase("A", base, map[string]string{"other/optout.txt": "opt-out change outside the rule\n"})
	h.Accept("M", outside.Candidate.ID)

	var steps []standingCompatStep
	record := func(name, command string, succeeds bool, output string, status int) {
		steps = append(steps, standingCompatStep{Name: name, Command: command, Succeeds: succeeds, Output: output, Status: status})
	}
	output, status := standingAutomergePreview(h, "M", base, ready.Candidate.ID, "maint")
	record("preview of a ready, accepted candidate", "automerge", true, output, status)
	output, status = standingAutomergePreview(h, "M", base, second.Candidate.ID, "maint")
	record("preview of a candidate missing acceptance", "automerge", false, output, status)
	output, status = standingAutomergePreview(h, "M", base, outside.Candidate.ID, "maint")
	record("preview of a candidate outside the rule", "automerge", false, output, status)

	run := standingAutomergeExecute(h, "M", base, ready.Candidate.ID, "maint")
	record("hn automerge --execute by M", "automerge", true, run.Output, run.Status)
	if run.Status == 0 {
		h.git("M", "push", "-q", "origin", "main")
	}
	h.Publish("M")

	output, status = h.Run("M", "decide", second.Candidate.ID, "--accept", "--body", "Accepted by the compatibility test.")
	record("hn decide --accept of the second candidate", "", true, output, status)
	output, status = h.Run("M", "merge", second.Candidate.ID)
	record("hn merge of the second candidate", "", true, output, status)
	if status == 0 {
		h.git("M", "push", "-q", "origin", "main")
	}
	h.Publish("M")

	output, status = h.Run("M", "proposal", "status", ready.Candidate.ID)
	record("hn proposal status of the automerged candidate", "", true, output, status)
	output, status = h.Run("M", "proposal", "status", second.Candidate.ID)
	record("hn proposal status of the manually merged candidate", "", true, output, status)
	output, status = h.Run("M", "policy", "show")
	record("hn policy show", "policy show", true, output, status)
	output, status = h.Run("M", "policy", "check", "--base", "main", "--file", serveGovernedPolicyPath)
	record("hn policy check of an unchanged draft", "policy check", true, output, status)

	// The comparison is only meaningful if each step did what it is named
	// for, on whichever client ran it.
	for index, want := range []string{
		"\nEligible: explicit --execute is required to merge locally.\n",
		"\nBlocked:\n  " + standingAutomergeNotAccepted + "\n",
		"\nBlocked:\n  changed path \"other/optout.txt\" is outside the rule allowlist\n",
		"Merged proposal " + operationalShortID(ready.Candidate.ID) + " into main at ",
		"Recorded accept decision ",
		"Merged proposal " + operationalShortID(second.Candidate.ID) + " into main at ",
		"Status:   merged\n",
		"Status:   merged\n",
		"Rule maint; target branch main:\n  ",
		"  exact policy bytes: unchanged\n",
	} {
		assertOperationalContains(h.t, steps[index].Output, want)
	}
	return steps
}

// standingCompatCompareStep requires the newer client's output, without
// the allow-listed additive lines, to equal the older client's output line
// by line after normalization, with the same exit code.
func standingCompatCompareStep(t *testing.T, older, newer *standingHarness, oldStep, newStep standingCompatStep) {
	t.Helper()
	if oldStep.Name != newStep.Name {
		t.Fatalf("scripts diverged: older step %q, newer step %q", oldStep.Name, newStep.Name)
	}
	for client, step := range map[string]standingCompatStep{"older": oldStep, "newer": newStep} {
		if (step.Status == 0) != step.Succeeds {
			t.Fatalf("%s: %s client exited %d, want success=%v:\n%s", step.Name, client, step.Status, step.Succeeds, step.Output)
		}
	}
	if oldStep.Status != newStep.Status {
		t.Fatalf("%s: older client exited %d, newer client %d\nolder:\n%s\nnewer:\n%s", oldStep.Name, oldStep.Status, newStep.Status, oldStep.Output, newStep.Output)
	}
	oldLines := strings.Split(standingCompatNormalize(older, oldStep.Output), "\n")
	var additive, newLines []string
	for _, line := range strings.Split(standingCompatNormalize(newer, newStep.Output), "\n") {
		if standingCompatAdditive(newStep.Command, line) {
			additive = append(additive, line)
			continue
		}
		newLines = append(newLines, line)
	}
	for _, line := range oldLines {
		if standingCompatAdditive(oldStep.Command, line) {
			t.Fatalf("%s: the older client prints allow-listed line %q:\n%s", oldStep.Name, line, oldStep.Output)
		}
	}
	for index := 0; index < max(len(oldLines), len(newLines)); index++ {
		var oldLine, newLine string
		if index < len(oldLines) {
			oldLine = oldLines[index]
		}
		if index < len(newLines) {
			newLine = newLines[index]
		}
		if index >= len(oldLines) || index >= len(newLines) || oldLine != newLine {
			t.Fatalf("%s: line %d differs after normalization and the allow-list\nolder: %q\nnewer: %q\nolder output:\n%s\nnewer output:\n%s",
				oldStep.Name, index+1, oldLine, newLine, oldStep.Output, newStep.Output)
		}
	}
	t.Logf("%s: exit %d on both clients; %d additive line(s) on the newer client: %q", oldStep.Name, oldStep.Status, len(additive), additive)
}

// standingCompatCompareFacts requires every chain of the two harnesses to
// hold the same kinds in the same order with the same evidence counts, and
// no event of either to carry a rule key.
func standingCompatCompareFacts(t *testing.T, older, newer *standingHarness) {
	t.Helper()
	for _, role := range standingHarnessRoles {
		oldChain, newChain := standingCompatChain(older, role), standingCompatChain(newer, role)
		describe := func(chain []standingHarnessFact) string {
			parts := make([]string, 0, len(chain))
			for _, fact := range chain {
				parts = append(parts, fmt.Sprintf("%s/%d", fact.Kind, len(fact.Evidence)))
			}
			return strings.Join(parts, " ")
		}
		if describe(oldChain) != describe(newChain) {
			t.Fatalf("%s's chain differs:\nolder: %s\nnewer: %s", role, describe(oldChain), describe(newChain))
		}
		for _, chain := range [][]standingHarnessFact{oldChain, newChain} {
			for _, fact := range chain {
				for _, key := range standingEventTopLevelKeys(t, fact.Payload) {
					if strings.EqualFold(key, "rule") {
						t.Fatalf("%s's %s %s carries key %q:\n%s", role, fact.Kind, fact.ID, key, fact.Payload)
					}
				}
			}
		}
		t.Logf("%s's chain on both clients: [%s]", role, describe(newChain))
	}
}

// T081 (US7-1, NFR-004): the same opt-out script on two fresh repositories,
// one driven by each client.
func standingCompatOptOut(t *testing.T, older, newer string) {
	oldHarness := newStandingHarnessWithBinary(t, older, standingHarnessLegacyPolicy())
	oldSteps := standingCompatOptOutScript(oldHarness)
	newHarness := newStandingHarnessWithBinary(t, newer, standingHarnessLegacyPolicy())
	newSteps := standingCompatOptOutScript(newHarness)
	if len(oldSteps) != len(newSteps) {
		t.Fatalf("older script ran %d steps, newer %d", len(oldSteps), len(newSteps))
	}
	for index := range oldSteps {
		standingCompatCompareStep(t, oldHarness, newHarness, oldSteps[index], newSteps[index])
	}
	standingCompatCompareFacts(t, oldHarness, newHarness)
}

// T083 (US7-2): the older client rejects each new field, including the
// default value maintainers, and the newer client accepts the same policy.
func standingCompatPolicyFields(t *testing.T, older, newer string) {
	variants := []struct {
		Name, Field string
		Edit        func(*standingHarnessPolicy)
	}{
		{`"acceptance": "rule"`, "acceptance", func(p *standingHarnessPolicy) {
			p.Rules[0].Acceptance = standingHarnessString(automergeAcceptanceRule)
		}},
		{`"acceptance": "maintainers"`, "acceptance", func(p *standingHarnessPolicy) {
			p.Rules[0].Acceptance = standingHarnessString(automergeAcceptanceMaintainers)
		}},
		{`"reviewers": [R]`, "reviewers", func(p *standingHarnessPolicy) { p.Rules[0].Reviewers = []string{"R"} }},
	}
	for _, variant := range variants {
		policy := standingHarnessLegacyPolicy()
		variant.Edit(&policy)
		h := newStandingHarnessWithBinary(t, newer, policy)
		if !strings.Contains(policy.JSON(h.actors), `"`+variant.Field+`"`) {
			t.Fatalf("%s: policy does not carry %q:\n%s", variant.Name, variant.Field, policy.JSON(h.actors))
		}
		c := h.openCase("A", h.Base(), nil)
		for _, command := range [][]string{{"policy", "show"}, {"proposal", "status", c.Candidate.ID}} {
			output, status := standingCompatRun(h, older, h.Work("A"), command...)
			unknown := `unknown field "` + variant.Field + `"`
			if status == 0 || !strings.Contains(output, unknown) {
				t.Fatalf("%s: older hn %s exited %d, want a refusal containing %q:\n%s", variant.Name, strings.Join(command, " "), status, unknown, output)
			}
			t.Logf("%s: older hn %s exited %d: %s", variant.Name, strings.Join(command, " "), status, strings.TrimSpace(output))
			newOutput, newStatus := standingCompatRun(h, newer, h.Work("A"), command...)
			if newStatus != 0 {
				t.Fatalf("%s: newer hn %s exited %d:\n%s", variant.Name, strings.Join(command, " "), newStatus, newOutput)
			}
		}
		assertOperationalContains(t, h.hn("A", "policy", "show"), "Rule maint;")
	}
}

// T084 steps 1 to 3 (US7-3): the older client never admits a fact valid
// only through path (b).
func standingCompatRuleOnlyFacts(t *testing.T, older, newer string, report *standingCompatReport) {
	// Realistic case: User Story 1 with the newer client. The older
	// verifier may refuse already at the base policy, which carries the new
	// fields, so only the refusal is asserted.
	h := newStandingHarnessWithBinary(t, newer, standingHarnessDefaultPolicy())
	candidate := h.ReadyCandidate(standingAutomergeFiles)
	c := h.Case(candidate.ID)
	preview, status := standingAutomergePreview(h, "E", h.Base(), candidate.ID, "ordinary")
	requireStandingAutomergeEligible(t, preview, status)
	run := standingAutomergeExecute(h, "E", h.Base(), candidate.ID, "ordinary")
	fact := requireStandingRuleMerge(h, "E", run, candidate, "ordinary", c.Approval.ID, c.Result.ID)
	h.git("E", "push", "-q", "origin", "main")
	h.Publish("E")
	factCommit := h.git("E", "rev-parse", operationalActorRef(h.Actor("E")))

	oldVerifier := standingCompatVerifier(h, older, "older-verifier")
	syncOutput, syncStatus := standingCompatRun(h, older, oldVerifier, "sync", "origin")
	statusOutput, statusCode, merged := standingCompatStatus(h, older, oldVerifier, candidate.ID)
	t.Logf("realistic rule merge %s, older sync exited %d:\n%s\nolder status exited %d:\n%s", fact.ID, syncStatus, syncOutput, statusCode, statusOutput)
	if syncStatus == 0 || standingCompatPromoted(h, oldVerifier, "E", factCommit) || merged {
		t.Fatalf("older verifier admitted rule merge %s: sync exited %d, promoted=%v, merged=%v\n%s\n%s",
			fact.ID, syncStatus, standingCompatPromoted(h, oldVerifier, "E", factCommit), merged, syncOutput, statusOutput)
	}
	newVerifier := standingCompatVerifier(h, newer, "newer-verifier")
	newSync, newSyncStatus := standingCompatRun(h, newer, newVerifier, "sync", "origin")
	newStatus, newCode, newMerged := standingCompatStatus(h, newer, newVerifier, candidate.ID)
	if newSyncStatus != 0 || !newMerged || !strings.Contains(newStatus, standingAutomergeRuleStatusLine("ordinary", h.Actor("E"), fact.ID)) {
		t.Fatalf("newer verifier: sync exited %d, status exited %d, want the rule merge:\n%s\n%s", newSyncStatus, newCode, newSync, newStatus)
	}
	reason := "fact refused"
	if strings.Contains(syncOutput+statusOutput, `unknown field "acceptance"`) {
		reason = `base policy has unknown field "acceptance"`
	}
	report.add("1", "US1 rule merge, policy with acceptance rule",
		fmt.Sprintf("refused: %s (sync exit %d, status exit %d, not merged)", reason, syncStatus, statusCode),
		"accepted (merged by rule ordinary)")

	// Isolated case: a base policy the older client reads, and a fact that
	// names its maintainers-mode rule and lists the approval and the run
	// result, once signed by a non-maintainer and once by the maintainer.
	h = newStandingHarnessWithBinary(t, newer, standingHarnessLegacyPolicy())
	c = h.readyCase("A", h.Base(), nil)
	digest := h.PolicyDigest(c.Base)
	// Refused lists the chains a verifier cannot promote once the signer's
	// chain is refused: T's result answers M's run request, so T's chain
	// waits for M's.
	for _, signer := range []struct {
		Role, Older, Label string
		Refused            []string
	}{
		{"E", "is not authorized by exact base policy " + digest, "non-maintainer", []string{"E"}},
		{"M", "lacks required acceptance evidence", "maintainer, no decisions", []string{"M", "T"}},
	} {
		h.Publish(signer.Role)
		clean := standingFactChainHead(h, signer.Role)
		if remote := standingFactRemoteChain(h, signer.Role); remote != clean {
			t.Fatalf("%s's chain is %q locally and %q on the remote", signer.Role, clean, remote)
		}
		signed := h.SignFactFor(signer.Role, c, func(event *Event) { event.Rule = "maint" })
		if signed.Candidate != c.Candidate.ID || parseMergeRuleClaim(signed.Payload) != "maint" {
			t.Fatalf("%s's fact %s does not claim rule maint for %s:\n%s", signer.Role, signed.ID, c.Candidate.ID, signed.Payload)
		}
		standingFactRawPublish(h, signer.Role)

		for _, client := range []struct{ Name, Binary, Want string }{
			{"older", older, "merge " + signed.ID + " " + signer.Older},
			{"newer", newer, "rule merge " + signed.ID + " fails " + ruleCheckRuleAcceptance},
		} {
			verifier := standingCompatVerifier(h, client.Binary, client.Name+"-verifier-"+signer.Role)
			output, status := standingCompatRun(h, client.Binary, verifier, "sync", "origin")
			t.Logf("%s-signed fact %s, %s sync exited %d:\n%s", signer.Role, signed.ID, client.Name, status, output)
			if status == 0 || !strings.Contains(output, client.Want) {
				t.Fatalf("%s sync of %s's fact exited %d, want a refusal containing %q:\n%s", client.Name, signer.Role, status, client.Want, output)
			}
			// The older client's refusal is its evidence check: its event
			// decoder ignored the rule key instead of failing at parse time.
			if strings.Contains(output, "invalid event JSON") || strings.Contains(output, "invalid resource event JSON") {
				t.Fatalf("%s sync failed at parse time:\n%s", client.Name, output)
			}
			standingFactAssertImportedExcept(t, h, verifier, output, signer.Refused...)
			if _, code, merged := standingCompatStatus(h, client.Binary, verifier, c.Candidate.ID); code != 0 || merged {
				t.Fatalf("%s status of %s exited %d, merged=%v; want a projection that is not merged", client.Name, c.Candidate.ID, code, merged)
			}
		}
		report.add("2", "rule claim maint signed by "+signer.Role+" ("+signer.Label+")",
			"refused: merge "+strings.ReplaceAll(signer.Older, digest, "<base policy>"),
			"refused: rule merge fails "+ruleCheckRuleAcceptance)
		standingFactSetChain(h, signer.Role, clean)
		standingFactRawPublish(h, signer.Role)
	}
}

// T084 step 4 (US7-4, analysis finding I1): maintainer merges that cite
// enough acceptance decisions and carry a rule claim are maintainer merges
// on both clients.
func standingCompatMaintainerMergeAgreement(t *testing.T, older, newer string, report *standingCompatReport) {
	h := newStandingHarnessWithBinary(t, newer, standingHarnessLegacyPolicy())
	type variant struct {
		Name, Rule string
		Approval   bool
		Candidate  string
		Fact       string
	}
	variants := []*variant{
		{Name: "absent rule nosuch, decision", Rule: "nosuch"},
		{Name: "maintainers-mode rule maint, decision", Rule: "maint"},
		{Name: "rule maint, decision and approval", Rule: "maint", Approval: true},
	}
	for _, v := range variants {
		c := h.readyCase("A", h.Base(), nil)
		decision := h.Accept("M", c.Candidate.ID)
		evidence := []string{decision.ID}
		if v.Approval {
			evidence = append(evidence, c.Approval.ID)
		}
		v.Candidate = c.Candidate.ID
		v.Fact = h.SignFactFor("M", c, func(event *Event) { event.Evidence, event.Rule = evidence, v.Rule }).ID
	}
	h.Publish("M")
	if remote, local := standingFactRemoteChain(h, "M"), standingFactChainHead(h, "M"); remote != local {
		t.Fatalf("M's chain is %q on the remote, want %q", remote, local)
	}

	clients := []struct{ Name, Binary string }{{"older", older}, {"newer", newer}}
	verifiers := map[string]string{}
	for _, client := range clients {
		verifier := standingCompatVerifier(h, client.Binary, client.Name+"-verifier")
		output, status := standingCompatRun(h, client.Binary, verifier, "sync", "origin")
		if status != 0 {
			t.Fatalf("%s sync exited %d:\n%s", client.Name, status, output)
		}
		standingFactAssertImportedExcept(t, h, verifier, output)
		verifiers[client.Name] = verifier
	}
	for _, v := range variants {
		outcomes := map[string]string{}
		for _, client := range clients {
			output, code, merged := standingCompatStatus(h, client.Binary, verifiers[client.Name], v.Candidate)
			t.Logf("%s: %s status of %s exited %d:\n%s", v.Name, client.Name, v.Candidate, code, output)
			if !merged {
				t.Fatalf("%s: %s client does not report %s merged:\n%s", v.Name, client.Name, v.Candidate, output)
			}
			if client.Name == "newer" {
				assertOperationalContains(t, output, fmt.Sprintf("Merged by maintainers in fact %s; its rule annotation %q is not verified\n", v.Fact, v.Rule))
				outcomes[client.Name] = fmt.Sprintf("accepted (maintainer merge, annotation %q not verified)", v.Rule)
				continue
			}
			if strings.Contains(output, "annotation") || strings.Contains(output, "Merged by") {
				t.Fatalf("%s: older client prints a rule line:\n%s", v.Name, output)
			}
			outcomes[client.Name] = "accepted (merged, no rule line)"
		}
		report.add("4", v.Name, outcomes["older"], outcomes["newer"])
	}
}

// T084 step 5 (US7-5, C-004): the legacy fixtures reach the same verdict
// on both clients.
func standingCompatLegacyHistory(t *testing.T, older, newer string, report *standingCompatReport) {
	h := newStandingHarnessWithBinary(t, newer, standingHarnessLegacyPolicy())
	type built struct {
		fixture standingHarnessFixture
		signed  []standingHarnessSigned
	}
	var all []built
	for _, fixture := range standingHarnessLegacyFixtures(h) {
		all = append(all, built{fixture: fixture, signed: fixture.Build()})
	}
	if len(all) != 6 || all[0].fixture.ID != "L01" || all[5].fixture.ID != "L06" {
		t.Fatalf("legacy fixtures are not L01 to L06")
	}
	for _, role := range []string{"R", "M", "S"} {
		standingFactRawPublish(h, role)
	}

	verdicts := map[string]map[string]string{}
	for _, client := range []struct{ Name, Binary string }{{"older", older}, {"newer", newer}} {
		verifier := standingCompatVerifier(h, client.Binary, client.Name+"-verifier")
		output, status := standingCompatRun(h, client.Binary, verifier, "sync", "origin")
		t.Logf("%s sync exited %d:\n%s", client.Name, status, output)
		if status == 0 {
			t.Fatalf("%s sync accepted S's chain with L06:\n%s", client.Name, output)
		}
		assertOperationalContains(t, output, standingHarnessUnknownRuleField)
		standingFactAssertImportedExcept(t, h, verifier, output, "S")
		log, code := standingCompatRun(h, client.Binary, verifier, "log")
		if code != 0 {
			t.Fatalf("%s log exited %d:\n%s", client.Name, code, log)
		}
		verdicts[client.Name] = map[string]string{}
		for _, item := range all {
			listed, merged := 0, true
			for _, signed := range item.signed {
				if strings.Contains(log, operationalShortID(signed.ID)) {
					listed++
				}
				if bytes.Contains(signed.Payload, []byte(`"kind":"proposal.merged"`)) {
					if _, _, ok := standingCompatStatus(h, client.Binary, verifier, signed.Candidate); !ok {
						merged = false
					}
				}
			}
			verdict := "mixed"
			switch {
			case listed == len(item.signed) && merged:
				verdict = "accepted"
			case listed == 0 && !standingCompatPromoted(h, verifier, item.fixture.Signer, item.signed[0].Commit):
				verdict = "refused"
			}
			verdicts[client.Name][item.fixture.ID] = verdict
		}
	}
	for _, item := range all {
		want := "accepted"
		if item.fixture.Verdict == standingHarnessRefusedAsBefore {
			want = "refused"
		}
		oldVerdict, newVerdict := verdicts["older"][item.fixture.ID], verdicts["newer"][item.fixture.ID]
		if oldVerdict != want || newVerdict != want {
			t.Fatalf("%s: older client %s, newer client %s, want %s on both", item.fixture.ID, oldVerdict, newVerdict, want)
		}
		report.add("5", fmt.Sprintf("%s (%d signed event(s))", item.fixture.ID, len(item.signed)), oldVerdict, newVerdict)
	}
}

// T082: the one owner of the older client. Every consumer is a subtest and
// runs against the binary built here.
func TestStandingCompatOlderClient(t *testing.T) {
	older := standingCompatBuildOlder(t)
	newer := buildOperationalBinary(t)
	info, err := os.Stat(older)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("older client: %s (%d bytes); newer client: %s", older, info.Size(), newer)
	help, status := standingHarnessRun(t, older, t.TempDir(), "--help")
	if status != 0 || !strings.Contains(help, "hn automerge PROPOSAL --rule NAME") || !strings.Contains(help, "hn policy show [REV]") {
		t.Fatalf("older client --help exited %d:\n%s", status, help)
	}

	report := &standingCompatReport{}
	defer func() {
		t.Logf("observed outcomes (older client %s, newer client from this tree):\n%s", standingCompatOlderCommit[:12], report.table())
	}()
	t.Run("OptOut", func(t *testing.T) { standingCompatOptOut(t, older, newer) })
	t.Run("PolicyFields", func(t *testing.T) { standingCompatPolicyFields(t, older, newer) })
	t.Run("RuleOnlyFacts", func(t *testing.T) { standingCompatRuleOnlyFacts(t, older, newer, report) })
	t.Run("MaintainerMergeAgreement", func(t *testing.T) { standingCompatMaintainerMergeAgreement(t, older, newer, report) })
	t.Run("LegacyHistory", func(t *testing.T) { standingCompatLegacyHistory(t, older, newer, report) })
}

// T081 step 4 (C-003, NFR-004): the mission adds test files and never
// modifies, deletes or renames one that exists at the audit snapshot.
func TestStandingCompatPreExistingTestsFrozen(t *testing.T) {
	source, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := standingCompatSourceGit(t, source, "cat-file", "-e", standingCompatFrozenCommit+"^{commit}"); err != nil {
		t.Skipf("pre-existing test audit needs snapshot commit %s, which this checkout lacks: %v", standingCompatFrozenCommit, err)
	}
	output, err := standingCompatSourceGit(t, source, "diff", "--name-status", standingCompatFrozenCommit, "HEAD", "--", "*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	var added, changed, upstream []string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "A\t") {
			added = append(added, strings.TrimPrefix(line, "A\t"))
			continue
		}
		if standingCompatMatchesUpstream(t, source, line) {
			upstream = append(upstream, line)
			continue
		}
		changed = append(changed, line)
	}
	if len(upstream) != 0 {
		t.Logf("test files taken unchanged from upstream: %d", len(upstream))
	}
	if len(changed) != 0 {
		t.Fatalf("test files present at %s were modified, deleted or renamed:\n%s", standingCompatFrozenCommit, strings.Join(changed, "\n"))
	}
	t.Logf("test files added since %s: %s", standingCompatFrozenCommit[:12], strings.Join(added, ", "))
}

// standingCompatMatchesUpstream reports whether every path in one
// --name-status line is identical at HEAD and at some merged upstream
// commit, including being absent from both.
func standingCompatMatchesUpstream(t *testing.T, source, line string) bool {
	t.Helper()
	fields := strings.Split(line, "\t")
	if len(fields) < 2 {
		return false
	}
	for _, commit := range standingCompatUpstreamCommits {
		if _, err := standingCompatSourceGit(t, source, "cat-file", "-e", commit+"^{commit}"); err != nil {
			continue
		}
		same := true
		for _, path := range fields[1:] {
			if _, err := standingCompatSourceGit(t, source, "diff", "--quiet", commit, "HEAD", "--", path); err != nil {
				same = false
				break
			}
		}
		if same {
			return true
		}
	}
	return false
}
