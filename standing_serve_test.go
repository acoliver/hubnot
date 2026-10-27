package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// Rule advancement on hn serve (WP13 wire vocabulary; WP14, WP15 and WP19
// add the verdict, the client and the documented examples). The contract is
// kitty-specs/standing-acceptance-01M5VD2K/contracts/serve-rule-advancement.md.

const (
	standingServeWireAdvance = "hn-advance=sha256:" + "abababababababababababababababababababababababababababababababab"
	standingServeWireTip     = "hn-expected-tip=" + "cccccccccccccccccccccccccccccccccccccccc"
)

// standingServeWireEnv builds the hook environment Git sets for options.
func standingServeWireEnv(options ...string) []string {
	env := []string{fmt.Sprintf("GIT_PUSH_OPTION_COUNT=%d", len(options))}
	for index, option := range options {
		env = append(env, fmt.Sprintf("GIT_PUSH_OPTION_%d=%s", index, option))
	}
	return env
}

func TestStandingServeWireParsesRuleOption(t *testing.T) {
	advance := strings.TrimPrefix(standingServeWireAdvance, "hn-advance=")
	tip := strings.TrimPrefix(standingServeWireTip, "hn-expected-tip=")
	longest := strings.Repeat("r", 64)
	for _, tc := range []struct {
		name    string
		options []string
		want    servePushOptions
	}{
		{"all three options", []string{standingServeWireAdvance, standingServeWireTip, "hn-rule=ordinary"},
			servePushOptions{Advance: advance, ExpectedTip: tip, Rule: "ordinary"}},
		{"rule first", []string{"hn-rule=ordinary", standingServeWireTip, standingServeWireAdvance},
			servePushOptions{Advance: advance, ExpectedTip: tip, Rule: "ordinary"}},
		{"advance and rule", []string{standingServeWireAdvance, "hn-rule=ordinary"},
			servePushOptions{Advance: advance, Rule: "ordinary"}},
		{"rule then advance", []string{"hn-rule=ordinary", standingServeWireAdvance},
			servePushOptions{Advance: advance, Rule: "ordinary"}},
		{"every grammar character", []string{standingServeWireAdvance, "hn-rule=Az09-_.x"},
			servePushOptions{Advance: advance, Rule: "Az09-_.x"}},
		{"64-character name", []string{standingServeWireAdvance, "hn-rule=" + longest},
			servePushOptions{Advance: advance, Rule: longest}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			got, err := parseServePushOptions(standingServeWireEnv(tc.options...), 40)

			// Assert
			if err != nil || got != tc.want {
				t.Fatalf("parseServePushOptions(%q) = %+v, %v; want %+v", tc.options, got, err, tc.want)
			}
		})
	}
}

func TestStandingServeWireRefusesRuleOption(t *testing.T) {
	tooLong := strings.Repeat("r", 65)
	for _, tc := range []struct {
		name    string
		options []string
		want    string
	}{
		{"rule twice", []string{standingServeWireAdvance, "hn-rule=one", "hn-rule=two"}, "hn-rule appears more than once"},
		{"same rule twice", []string{"hn-rule=one", standingServeWireAdvance, "hn-rule=one"}, "hn-rule appears more than once"},
		{"empty name", []string{standingServeWireAdvance, "hn-rule="}, `hn-rule value "" is not a valid rule name`},
		{"name with a space", []string{standingServeWireAdvance, "hn-rule=bad name"}, `hn-rule value "bad name" is not a valid rule name`},
		{"dot", []string{standingServeWireAdvance, "hn-rule=."}, `hn-rule value "." is not a valid rule name`},
		{"dot dot", []string{standingServeWireAdvance, "hn-rule=.."}, `hn-rule value ".." is not a valid rule name`},
		{"slash", []string{standingServeWireAdvance, "hn-rule=a/b"}, `hn-rule value "a/b" is not a valid rule name`},
		{"non-ASCII", []string{standingServeWireAdvance, "hn-rule=r\u00e9gle"}, "is not a valid rule name"},
		{"65-character name", []string{standingServeWireAdvance, "hn-rule=" + tooLong}, fmt.Sprintf("hn-rule value %q is not a valid rule name", tooLong)},
		{"bare rule", []string{standingServeWireAdvance, "hn-rule"}, `push option "hn-rule" has no value`},
		{"rule without advance", []string{"hn-rule=ordinary"}, "hn-rule requires hn-advance"},
		{"rule and tip without advance", []string{standingServeWireTip, "hn-rule=ordinary"}, "hn-rule requires hn-advance"},
		{"four options", []string{standingServeWireAdvance, standingServeWireTip, "hn-rule=ordinary", "hn-rule=other"}, "4 push options; at most 3 are supported"},
		{"rule with an unknown option", []string{standingServeWireAdvance, "hn-rule=ordinary", "foo=bar"}, `push option "foo" is not supported`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			got, err := parseServePushOptions(standingServeWireEnv(tc.options...), 40)

			// Assert
			if err == nil {
				t.Fatalf("parseServePushOptions(%q) = %+v, want an error", tc.options, got)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("parseServePushOptions(%q) error %q does not contain %q", tc.options, err, tc.want)
			}
		})
	}
}

func TestStandingServeWireSentenceNamesEveryOption(t *testing.T) {
	// Assert
	if servePushOptionLimit != 3 {
		t.Fatalf("servePushOptionLimit = %d, want 3", servePushOptionLimit)
	}
	want := "push options other than hn-advance, hn-expected-tip and hn-rule are not supported."
	if servePushOptionSentence != want {
		t.Fatalf("servePushOptionSentence = %q, want %q", servePushOptionSentence, want)
	}
	for _, option := range []string{servePushOptionAdvance, servePushOptionExpected, servePushOptionRule} {
		if !strings.Contains(servePushOptionSentence, option) {
			t.Errorf("sentence does not name %s", option)
		}
	}
}

func TestStandingServeWireRuleCatalogue(t *testing.T) {
	// Arrange
	want := []serveReason{
		{"advance-not-rule-executor", "The requester is not an executing actor of the named rule in the base policy", "ask an executing actor of the rule to run hn automerge --remote"},
		{"protected-rule-not-satisfied", "The named rule does not exist, is not a standing rule, or the candidate does not satisfy it", "run hn automerge <candidate> --rule <rule>"},
		{"protected-rule-veto", "A current trusted change request or trusted failure blocks the rule", "resolve the named events, then retry"},
	}

	// Assert
	if !slices.Equal(serveRuleReasonCatalogue, want) {
		t.Fatalf("serveRuleReasonCatalogue = %#v, want %#v", serveRuleReasonCatalogue, want)
	}
	for _, rule := range serveRuleReasonCatalogue {
		for _, base := range serveReasonCatalogue {
			if rule.Code == base.Code {
				t.Errorf("code %q appears in both catalogues", rule.Code)
			}
		}
	}
	for _, reason := range serveRuleReasonCatalogue {
		if !serveReasonKnown(reason.Code) {
			t.Errorf("serveReasonKnown(%q) = false", reason.Code)
		}
		if got := serveReasonDefaultNext(reason.Code); got != reason.Next {
			t.Errorf("serveReasonDefaultNext(%q) = %q, want %q", reason.Code, got, reason.Next)
		}
		if serveSanitizeNext(reason.Next) != reason.Next {
			t.Errorf("default next action for %q is not wire-safe: %q", reason.Code, reason.Next)
		}
		if serveReasonCarriesBudget(reason.Code) {
			t.Errorf("rule code %q must not carry budget fields", reason.Code)
		}
		if err := validateServePushLogCode(reason.Code); err != nil {
			t.Errorf("push log refuses %q: %v", reason.Code, err)
		}
	}
}

func TestStandingServeWireRuleCodesRoundTrip(t *testing.T) {
	for _, reason := range serveRuleReasonCatalogue {
		t.Run(reason.Code, func(t *testing.T) {
			// Arrange
			rejection := serveRejection{
				Ref: serveGovernedMain, Actor: serveTestActor, Code: reason.Code,
				IDs: []string{serveTestEvent, serveTestCommit}, Next: reason.Next,
				Sentence: "rule ordinary refused " + reason.Code,
			}

			// Act
			rendered := renderServeRejection(rejection)
			line, _, _ := strings.Cut(rendered, "\n")
			parsed, err := parseServeRejectionLine(line)
			extracted, lines := extractServeRejections("remote: " + strings.ReplaceAll(rendered, "\n", "\nremote: "))

			// Assert
			if err != nil {
				t.Fatalf("parseServeRejectionLine(%q): %v", line, err)
			}
			parsed.Sentence = rejection.Sentence
			if !slices.Equal(parsed.IDs, rejection.IDs) || parsed.Ref != rejection.Ref || parsed.Actor != rejection.Actor ||
				parsed.Code != rejection.Code || parsed.Next != rejection.Next {
				t.Fatalf("parsed %+v, want %+v", parsed, rejection)
			}
			if again := renderServeRejection(parsed); again != rendered {
				t.Fatalf("re-rendered %q, want %q", again, rendered)
			}
			if len(extracted) != 1 || len(lines) != 2 || extracted[0].Code != reason.Code || extracted[0].Sentence != rejection.Sentence {
				t.Fatalf("client extracted %+v from %q", extracted, lines)
			}
		})
	}
}

// standingServeRuleReasonDocsTable reads the table under "### Reason codes
// for rule advancement" inside the "hn serve wire surface" section, with
// the backtick-stripping rule of serveReasonDocsTable.
func standingServeRuleReasonDocsTable(markdown string) ([]serveReason, error) {
	_, section, found := strings.Cut(markdown, "\n## hn serve wire surface\n")
	if !found {
		return nil, fmt.Errorf("docs have no hn serve wire surface section")
	}
	section, _, _ = strings.Cut(section, "\n## ")
	_, subsection, found := strings.Cut(section, "\n### Reason codes for rule advancement\n")
	if !found {
		return nil, fmt.Errorf("hn serve wire surface section has no Reason codes for rule advancement subsection")
	}
	subsection, _, _ = strings.Cut(subsection, "\n### ")
	_, table, found := strings.Cut(subsection, "\n| Code | Meaning | Typical next action |\n|----|----|----|\n")
	if !found {
		return nil, fmt.Errorf("the rule advancement subsection has no reason-code table")
	}
	var reasons []serveReason
	for _, row := range strings.Split(table, "\n") {
		if !strings.HasPrefix(row, "|") {
			break
		}
		cells := strings.Split(strings.Trim(row, "|"), "|")
		if len(cells) != 3 {
			return nil, fmt.Errorf("reason-code row %q does not have three cells", row)
		}
		for index := range cells {
			cells[index] = strings.TrimSpace(strings.ReplaceAll(cells[index], "`", ""))
		}
		reasons = append(reasons, serveReason{Code: cells[0], Meaning: cells[1], Next: cells[2]})
	}
	return reasons, nil
}

func TestStandingServeWireRuleReasonDocsTable(t *testing.T) {
	// Arrange
	markdown, err := os.ReadFile(filepath.Join("docs", "protocol-v0.md"))
	if err != nil {
		t.Fatal(err)
	}

	// Act
	documented, err := standingServeRuleReasonDocsTable(string(markdown))
	if err != nil {
		t.Fatal(err)
	}
	first, err := serveReasonDocsTable(string(markdown))
	if err != nil {
		t.Fatal(err)
	}

	// Assert
	if err := compareServeReasonTable(documented, serveRuleReasonCatalogue); err != nil {
		t.Fatal(err)
	}
	if err := compareServeReasonTable(first, serveReasonCatalogue); err != nil {
		t.Fatalf("the first reason table no longer equals serveReasonCatalogue: %v", err)
	}
	extended := append(slices.Clone(serveRuleReasonCatalogue), serveReason{"new-code", "meaning", "next"})
	if compareServeReasonTable(documented, extended) == nil {
		t.Fatal("drift check accepted a rule code missing from the docs")
	}
	changed := slices.Clone(serveRuleReasonCatalogue)
	changed[1].Next = "something else"
	if compareServeReasonTable(documented, changed) == nil {
		t.Fatal("drift check accepted a changed next action")
	}
	_, wire, _ := strings.Cut(string(markdown), "\n## hn serve wire surface\n")
	wire, _, _ = strings.Cut(wire, "\n## ")
	firstTable := strings.Index(wire, "\n### Reason codes\n")
	ruleTable := strings.Index(wire, "\n### Reason codes for rule advancement\n")
	if firstTable < 0 || ruleTable < firstTable {
		t.Fatal("Reason codes for rule advancement must follow the first reason table inside hn serve wire surface")
	}
}

// Push options that fail before any verdict runs: hn-rule without
// hn-advance is a parse error, and hn-advance keeps its shape rule when the
// push also names a rule. Both hold before and after WP14.
func TestStandingServeWireRuleOptionShapeRefusals(t *testing.T) {
	// Arrange
	f := newServePushFixture(t)
	f.commitOn("A", "origin/main")
	shape := "hn-advance requires a push with exactly one ref update, to a protected branch."
	for _, tc := range []struct {
		name     string
		options  []string
		refs     []string
		sentence string
	}{
		{"rule without advance", []string{"hn-rule=ordinary"}, []string{"refs/heads/one"}, servePushOptionSentence},
		{"rule without advance to main", []string{"hn-rule=ordinary"}, []string{serveGovernedMain}, servePushOptionSentence},
		{"malformed rule", []string{standingServeWireAdvance, "hn-rule=bad name"}, []string{serveGovernedMain}, servePushOptionSentence},
		{"rule with advance to an ordinary branch", []string{standingServeWireAdvance, "hn-rule=ordinary"}, []string{"refs/heads/one"}, shape},
		{"rule with advance and two refs", []string{standingServeWireAdvance, "hn-rule=ordinary"}, []string{"refs/heads/one", "refs/heads/two"}, shape},
	} {
		args := []string{}
		for _, option := range tc.options {
			args = append(args, "-o", option)
		}
		args = append(args, "origin")
		want := map[string]string{}
		for _, ref := range tc.refs {
			args = append(args, "HEAD:"+ref)
			want[ref] = serveReasonUnsupportedCommand
		}

		// Act
		rejections := f.reject("A", want, args...)

		// Assert
		for _, rejection := range rejections {
			if rejection.Sentence != tc.sentence {
				t.Fatalf("%s: sentence %q, want %q", tc.name, rejection.Sentence, tc.sentence)
			}
		}
	}
}

// Boundary fixture B5 (contracts/rule-merge-fact.md, FR-025): a maintainer
// advancement that would succeed is refused once it names a rule, with
// every hosted ref unchanged, and the same request without hn-rule
// advances. The code is either the interim unsupported-command of WP13 or
// the protected-rule-not-satisfied of WP14, so the test holds across both.
func TestStandingServeWireRuleRefusedBeforeVerdict(t *testing.T) {
	// Arrange
	g := newServeGovernedFixture(t, serveGovernedOptions{})
	head := g.Commit(g.Base, nil)
	p := g.ReadyProposal(g.Base, head)
	advance := []string{"hn-advance=" + p.Candidate.ID, "hn-expected-tip=" + g.Base}
	before := g.ServerRefs()

	// Act
	output, rejections, pushed := g.PushMain(g.Maintainer, head, append(slices.Clone(advance), "hn-rule=nosuch")...)

	// Assert
	if pushed {
		t.Fatalf("a push naming rule nosuch was accepted:\n%s", output)
	}
	if after := g.ServerRefs(); after != before {
		t.Fatalf("the refused push changed hosted refs:\nbefore\n%s\nafter\n%s", before, after)
	}
	if len(rejections) != 1 {
		t.Fatalf("got %d rejections, want 1:\n%s", len(rejections), output)
	}
	rejection := rejections[0]
	if rejection.Ref != serveGovernedMain || rejection.Actor != g.actors[g.Maintainer] {
		t.Fatalf("rejection %+v is not for main and the maintainer", rejection)
	}
	if allowed := []string{serveReasonUnsupportedCommand, serveReasonProtectedRuleNotSatisfied}; !slices.Contains(allowed, rejection.Code) {
		t.Fatalf("rejection code %s, want one of %v:\n%s", rejection.Code, allowed, output)
	}
	if rejection.Code == serveReasonUnsupportedCommand && rejection.Sentence != "this server does not evaluate hn-rule advancement yet." {
		t.Fatalf("interim refusal sentence %q", rejection.Sentence)
	}

	g.AcceptMain(g.Maintainer, head, advance...)
	g.VerifyAccepted(p.Candidate.ID)
}

// Standing serve fixture (WP14 T067).
//
// newStandingServeFixture hosts repository r like newServeGovernedFixture,
// with the participants of the standing harness: author A, maintainer M,
// counted reviewer R, second trusted reviewer S, runner U, executing actor
// E of rule ordinary (not a maintainer) and executing actor X of rule
// other only. Author, Reviewer, Runner, Maintainer and CIFailFlag are set
// as the governed fixture sets them, so every serveGovernedFixture method
// works unchanged. main is protected because hn serve repo create
// protects refs/heads/main by default, as for the governed fixture.
func newStandingServeFixture(t *testing.T, policy func(actors map[string]string) string) *serveGovernedFixture {
	t.Helper()
	h := newServeHarness(t)
	f := &servePushFixture{
		t:       t,
		h:       h,
		hosted:  h.Layout().RepoGitDir("r"),
		actors:  map[string]string{},
		keys:    map[string]serveTestKey{},
		clients: map[string]serveHarnessClientEnv{},
		work:    map[string]string{},
	}
	names := []string{"A", "M", "R", "S", "U", "E", "X"}
	for _, name := range names {
		work := filepath.Join(t.TempDir(), "work-"+name)
		runOperationalGit(t, "", "init", "-q", "-b", "main", work)
		runOperationalCommand(t, h.Binary, work, "init", "--name", "Standing "+name)
		f.actors[name] = readOperationalIdentity(t, h.Binary, work).Actor
		f.work[name] = work
	}
	g := &serveGovernedFixture{
		servePushFixture: f,
		Author:           "A", Reviewer: "R", Runner: "U", Maintainer: "M",
		CIFailFlag: filepath.Join(t.TempDir(), "ci-fails"),
	}

	source := filepath.Join(t.TempDir(), "source")
	initOperationalRepository(t, source, "Source")
	serveHarnessWriteFile(t, source, serveGovernedPolicyPath, policy(f.actors))
	serveHarnessWriteFile(t, source, filepath.Join(".hn", "pipelines", "test.json"), serveGovernedPipelineJSON(t, g.CIFailFlag))
	serveHarnessCommit(t, source, "standing base")
	h.Serve("serve", "repo", "create", "r", "--from", source)

	for _, name := range names {
		f.keys[name] = serveHarnessAddKey(h, f.actors[name])
		f.clients[name] = serveHarnessClient(t, h, f.keys[name], nil)
		f.git(name, "remote", "add", "origin", serveHarnessCloneURL("r"))
		f.git(name, "fetch", "-q", "origin")
		f.git(name, "checkout", "-q", "-B", "main", "origin/main")
		f.hn(name, "replication", "select", "origin", "--all")
	}
	g.Base = g.ServerTip("main")
	if len(g.Base) != len(servePushZero) {
		t.Fatalf("hosted main %q is not a SHA-1 object ID; the fixtures assume SHA-1", g.Base)
	}
	return g
}

// standingServePolicyDocument is the policy of standingServeDefaultPolicy
// by role, for variants. U is the runner, as in the governed fixture.
func standingServePolicyDocument() standingHarnessPolicy {
	return standingHarnessPolicy{
		Maintainers:       []string{"M"},
		TrustedReviewers:  []string{"R", "S"},
		RequiredApprovals: 1,
		Runners:           []string{"U"},
		Rules: []standingHarnessRule{
			{
				Name: "ordinary", TargetBranch: "main", Acceptance: standingHarnessString(automergeAcceptanceRule),
				Actors: []string{"E"}, Authors: []string{"A"}, Reviewers: []string{"R"},
				Paths: []string{"src/", "tests/", "docs/"},
			},
			{
				Name: "other", TargetBranch: "main", Acceptance: standingHarnessString(automergeAcceptanceRule),
				Actors: []string{"X"}, Authors: []string{"A"}, Paths: []string{"src/", "tests/", "docs/"},
			},
			{
				Name: "maint", TargetBranch: "main", Actors: []string{"M"}, Authors: []string{"A"},
				Paths: []string{"src/"},
			},
			{
				Name: "release", TargetBranch: "release", Acceptance: standingHarnessString(automergeAcceptanceRule),
				Actors: []string{"E"}, Authors: []string{"A"}, Paths: []string{"src/"},
			},
		},
	}
}

// standingServeDefaultPolicy: maintainer M, trusted reviewers R and S, one
// approval and one acceptance, pipeline test run by U, and the rules
// ordinary (E for A's changes under src/, tests/ and docs/, counting R),
// other (X, same paths), maint (acceptance maintainers, M) and release
// (standing, for branch release).
func standingServeDefaultPolicy(actors map[string]string) string {
	return standingServePolicyDocument().JSON(actors)
}

// StandingReady commits files on the base as the author, opens a
// candidate, and gathers R's approval and U's passing result on M's run
// request. Nothing accepts it: only a standing rule can.
func (g *serveGovernedFixture) StandingReady(files map[string]string) serveGovernedProposal {
	g.t.Helper()
	var p serveGovernedProposal
	p.Candidate = g.OpenProposal(g.Base, g.Commit(g.Base, files))
	p.Approval = g.Approve(p.Candidate.ID)
	p.Request = g.RequestCI(p.Candidate.ID)
	p.Result = g.RunCI(p.Request.ID)
	return p
}

// RulePush requests advancement of main to head for candidate under rule
// from who's clone, bound to the current hosted tip, the way the client
// does: the three push options on PushMain's forced refspec.
func (g *serveGovernedFixture) RulePush(who, candidate, head, rule string) (output string, rejections []serveRejection, ok bool) {
	g.t.Helper()
	tip := g.ServerTip("main")
	return g.PushMain(who, head, "hn-advance="+candidate, "hn-expected-tip="+tip, "hn-rule="+rule)
}

// standingServeImport runs hn sync origin in who's clone, which imports
// the hosted facts and the candidates' code, as hn automerge --remote does
// before it requests advancement.
func standingServeImport(g *serveGovernedFixture, who string) {
	g.t.Helper()
	g.hn(who, "sync", "origin")
}

// standingServeRuleRefused requires who's rule push to be refused with code
// on main, attributed to who, as one line pair, with every hosted ref
// byte-identical afterwards (C-005). It returns the rejection and the two
// relayed lines.
func standingServeRuleRefused(t *testing.T, g *serveGovernedFixture, who, candidate, head, rule, code string) (serveRejection, []string) {
	t.Helper()
	before := g.ServerRefs()
	output, rejections, ok := g.RulePush(who, candidate, head, rule)
	if ok {
		t.Fatalf("rule push of %s under %s by %s was accepted:\n%s", candidate, rule, who, output)
	}
	if after := g.ServerRefs(); after != before {
		t.Fatalf("refused rule push changed hosted refs:\nbefore\n%s\nafter\n%s", before, after)
	}
	_, lines := extractServeRejections(output)
	if len(rejections) != 1 || len(lines) != 2 {
		t.Fatalf("got %d rejections in %d lines, want one pair:\n%s", len(rejections), len(lines), output)
	}
	rejection := rejections[0]
	if rejection.Ref != serveGovernedMain || rejection.Actor != g.actors[who] || rejection.Code != code {
		t.Fatalf("rejection %s %s %s, want %s %s %s:\n%s", rejection.Ref, rejection.Actor, rejection.Code,
			serveGovernedMain, g.actors[who], code, output)
	}
	return rejection, lines
}

// standingServeNotSatisfied checks a protected-rule-not-satisfied line: the
// candidate alone in ids, the next action with the candidate and rule
// substituted, the missing kinds and the sentence with each unmet text.
func standingServeNotSatisfied(t *testing.T, rejection serveRejection, candidate, rule string, missing []string, unmet ...string) {
	t.Helper()
	requireServeIDs(t, rejection, candidate)
	requireServeNext(t, rejection, "run hn automerge "+candidate+" --rule "+rule)
	requireServeMissing(t, rejection, missing...)
	want := "candidate " + candidate + " does not satisfy rule " + rule + ": " + strings.Join(unmet, "; ") + "."
	if rejection.Sentence != want {
		t.Fatalf("sentence\n%q\nwant\n%q", rejection.Sentence, want)
	}
}

// US4-1 on the server, US4-4, FR-018 and US4-5: an executing actor of a
// satisfied standing rule advances main to the exact head; the same actor
// needs maintainer acceptance without hn-rule, and an actor outside the
// rule is refused at step 13.
func TestStandingServeVerdictAcceptsRuleAdvancement(t *testing.T) {
	// Arrange
	g := newStandingServeFixture(t, standingServeDefaultPolicy)
	p := g.StandingReady(map[string]string{"src/feature.txt": "standing feature\n"})
	candidate, head := p.Candidate.ID, p.Candidate.Head
	for _, who := range []string{"E", "X"} {
		standingServeImport(g, who)
	}

	// Act and assert: US4-4, a direct push without push options.
	direct := g.RejectMain("E", head, serveReasonProtectedNotReady)
	requireServeIDs(t, direct, candidate)
	requireServeMissing(t, direct, serveEvidenceAcceptance)

	// FR-018: an advancement request without hn-rule still needs
	// maintainer acceptance, at step 10.
	unnamed := g.RejectMain("E", head, serveReasonProtectedNotReady, "hn-advance="+candidate, "hn-expected-tip="+g.Base)
	requireServeIDs(t, unnamed, candidate)
	requireServeMissing(t, unnamed, serveEvidenceAcceptance)

	// US4-5: X executes rule other only.
	outsider, _ := standingServeRuleRefused(t, g, "X", candidate, head, "ordinary", serveReasonAdvanceNotRuleExecutor)
	requireServeIDs(t, outsider, candidate)
	requireServeNext(t, outsider, "ask an executing actor of the rule to run hn automerge --remote")
	if want := "actor " + g.actors["X"] + " requested advancement of refs/heads/main under rule ordinary and is not one of its executing actors in the base policy of " + candidate + "."; outsider.Sentence != want {
		t.Fatalf("sentence\n%q\nwant\n%q", outsider.Sentence, want)
	}

	// US4-1: E's request under rule ordinary.
	commits := serveHarnessGit(t, g.hosted, "rev-list", "--count", head)
	output, rejections, ok := g.RulePush("E", candidate, head, "ordinary")
	if !ok || len(rejections) != 0 {
		t.Fatalf("rule advancement was refused:\n%s", output)
	}
	if tip := g.ServerTip("main"); tip != head {
		t.Fatalf("hosted main = %s, want the candidate head %s", tip, head)
	}
	if got := serveHarnessGit(t, g.hosted, "rev-list", "--count", serveGovernedMain); got != commits {
		t.Fatalf("main reaches %s commits and the head %s; the server created a commit", got, commits)
	}
	entries := g.pushLog()
	if last := entries[len(entries)-1]; last.Verdict != servePushVerdictAccepted || last.Actor != g.actors["E"] {
		t.Fatalf("push log entry %+v, want E's accepted push", last)
	}
}

// standingServeVetoCase is US4-3: E imports the hosted facts for a ready
// candidate, then S publishes a change request in its own push, and E's
// rule push is refused at step 10 with protected-rule-veto. It returns the
// fixture, the candidate, the change request, the rejection and the two
// relayed lines.
func standingServeVetoCase(t *testing.T) (*serveGovernedFixture, serveGovernedProposal, operationalEvent, serveRejection, []string) {
	t.Helper()
	g := newStandingServeFixture(t, standingServeDefaultPolicy)
	p := g.StandingReady(map[string]string{"src/veto.txt": "vetoed change\n"})
	standingServeImport(g, "E")
	change := g.act("S", "review.submit", "review", p.Candidate.ID, "--request-changes", "--body", "Changes requested after the import.")
	rejection, lines := standingServeRuleRefused(t, g, "E", p.Candidate.ID, p.Candidate.Head, "ordinary", serveReasonProtectedRuleVeto)
	return g, p, change, rejection, lines
}

// US4-3: a trusted change request or a trusted failure that reaches the
// server after the executing actor's import blocks the rule at step 10.
func TestStandingServeVerdictVetoes(t *testing.T) {
	// Arrange and act
	g, p, change, rejection, _ := standingServeVetoCase(t)

	// Assert
	requireServeIDs(t, rejection, p.Candidate.ID, change.ID)
	requireServeNext(t, rejection, "resolve the named events, then retry")
	if want := "candidate " + p.Candidate.ID + " is blocked by rule ordinary: trusted change request " + change.ID + "."; rejection.Sentence != want {
		t.Fatalf("sentence\n%q\nwant\n%q", rejection.Sentence, want)
	}
	if tip := g.ServerTip("main"); tip != g.Base {
		t.Fatalf("hosted main moved to %s", tip)
	}

	// Arrange: a second candidate whose runner's latest result fails.
	failing := g.StandingReady(map[string]string{"tests/failing.txt": "failing change\n"})
	standingServeImport(g, "E")
	writeServeTestFile(t, g.CIFailFlag, "fail\n", 0o644)
	failure := g.RunCI(failing.Request.ID, "--rerun")
	if err := os.Remove(g.CIFailFlag); err != nil {
		t.Fatal(err)
	}

	// Act
	rejection, _ = standingServeRuleRefused(t, g, "E", failing.Candidate.ID, failing.Candidate.Head, "ordinary", serveReasonProtectedRuleVeto)

	// Assert
	requireServeIDs(t, rejection, failing.Candidate.ID, failure.ID)
	if want := "candidate " + failing.Candidate.ID + " is blocked by rule ordinary: trusted failure " + failure.ID + "."; rejection.Sentence != want {
		t.Fatalf("sentence\n%q\nwant\n%q", rejection.Sentence, want)
	}
	if tip := g.ServerTip("main"); tip != g.Base {
		t.Fatalf("hosted main moved to %s", tip)
	}
}

// US4-6 and FR-017: M lands a policy amendment H1 on main. A builds the
// candidate's head on top of H1 but signs the older base, so the server
// reaches step 7a with a destination policy that differs from the base
// policy and refuses before it reads any rule evidence.
func TestStandingServeVerdictDestinationPolicy(t *testing.T) {
	// Arrange
	g := newStandingServeFixture(t, standingServeDefaultPolicy)
	amended := standingServePolicyDocument()
	amended.Rules[1].Paths = []string{"src/", "tests/", "docs/", "other/"}
	h1 := g.Commit(g.Base, map[string]string{serveGovernedPolicyPath: amended.JSON(g.actors)})
	g.ReadyProposal(g.Base, h1)
	g.AcceptMain("M", h1)
	head := g.Commit(h1, map[string]string{"src/after-amendment.txt": "built on the amendment\n"})
	p := g.OpenProposal(g.Base, head)
	g.Approve(p.ID)
	g.RunCI(g.RequestCI(p.ID).ID)
	standingServeImport(g, "E")
	_, baseDigest, err := loadReplicationPolicyAt(g.hosted, g.hosted, g.Base)
	if err != nil {
		t.Fatal(err)
	}
	_, destDigest, err := loadReplicationPolicyAt(g.hosted, g.hosted, h1)
	if err != nil {
		t.Fatal(err)
	}
	if baseDigest == destDigest {
		t.Fatal("the amendment did not change the policy digest")
	}

	// Act
	rejection, _ := standingServeRuleRefused(t, g, "E", p.ID, head, "ordinary", serveReasonProtectedDestinationPolicy)

	// Assert
	requireServeIDs(t, rejection, p.ID)
	requireServeMissing(t, rejection, serveEvidenceProposal)
	requireServeNext(t, rejection, "revise "+p.ID+" against "+h1)
	want := "refs/heads/main at " + h1[:8] + " has policy " + destDigest + " and candidate " + p.ID +
		" was based on policy " + baseDigest + "; rule ordinary applies only under its own policy."
	if rejection.Sentence != want {
		t.Fatalf("sentence\n%q\nwant\n%q", rejection.Sentence, want)
	}
	if tip := g.ServerTip("main"); tip != h1 {
		t.Fatalf("hosted main moved to %s", tip)
	}
}

// Step 10 under a rule, conditions a, b, c, e and f, each
// protected-rule-not-satisfied; a maintainer rejection is still
// protected-blocked at step 9; and boundary fixture B5 after the verdict:
// M's maintainer advancement naming a rule that does not exist.
func TestStandingServeVerdictRuleNotSatisfied(t *testing.T) {
	// Arrange
	g := newStandingServeFixture(t, standingServeDefaultPolicy)
	ready := g.StandingReady(map[string]string{"src/ready.txt": "ready change\n"})
	outsidePath := g.StandingReady(map[string]string{"other/x": "outside every rule path\n"})
	g.Author = "S"
	otherAuthor := g.StandingReady(map[string]string{"src/by-s.txt": "authored by S\n"})
	g.Author = "A"
	unapproved := g.OpenProposal(g.Base, g.Commit(g.Base, map[string]string{"src/unapproved.txt": "no approval\n"}))
	g.RunCI(g.RequestCI(unapproved.ID).ID)
	untested := g.OpenProposal(g.Base, g.Commit(g.Base, map[string]string{"src/untested.txt": "no CI\n"}))
	g.Approve(untested.ID)
	standingServeImport(g, "E")
	id, head := ready.Candidate.ID, ready.Candidate.Head

	for _, tc := range []struct {
		name, candidate, head, rule string
		missing                     []string
		unmet                       []string
	}{
		{"a: rule missing", id, head, "nosuch", nil,
			[]string{`automerge rule "nosuch" does not exist in the signed base policy`}},
		{"b: acceptance maintainers", id, head, "maint", nil,
			[]string{`automerge rule "maint" has acceptance "maintainers" and needs maintainer acceptance`}},
		{"c: another target branch", id, head, "release", nil,
			[]string{`rule target "release" does not match main`}},
		{"e: author outside", otherAuthor.Candidate.ID, otherAuthor.Candidate.Head, "ordinary", nil,
			[]string{"proposal author is not allowed by the rule"}},
		{"e: path outside", outsidePath.Candidate.ID, outsidePath.Candidate.Head, "ordinary", nil,
			[]string{`changed path "other/x" is outside the rule allowlist`}},
		{"f: approval missing", unapproved.ID, unapproved.Head, "ordinary", []string{serveEvidenceReview},
			[]string{"rule approval threshold is unmet: 0 of 1 counted approvals"}},
		{"f: CI missing", untested.ID, untested.Head, "ordinary", []string{serveEvidenceCI},
			[]string{`pipeline "test" has 0 of 1 passing trusted results`}},
	} {
		// Act
		rejection, _ := standingServeRuleRefused(t, g, "E", tc.candidate, tc.head, tc.rule, serveReasonProtectedRuleNotSatisfied)

		// Assert
		t.Logf("%s: %s", tc.name, rejection.Sentence)
		standingServeNotSatisfied(t, rejection, tc.candidate, tc.rule, tc.missing, tc.unmet...)
	}

	// B5 after the verdict: once M accepts the candidate, M could advance
	// it without hn-rule, but the maintainer path does not apply to a
	// request that names a rule, so naming one that does not exist fails.
	g.Accept(id)
	b5, _ := standingServeRuleRefused(t, g, "M", id, head, "nosuch", serveReasonProtectedRuleNotSatisfied)
	standingServeNotSatisfied(t, b5, id, "nosuch", nil, `automerge rule "nosuch" does not exist in the signed base policy`)

	// Step 9 before any rule step: a current maintainer rejection.
	decision := g.Reject(id)
	standingServeImport(g, "E")
	blocked, _ := standingServeRuleRefused(t, g, "E", id, head, "ordinary", serveReasonProtectedBlocked)
	requireServeIDs(t, blocked, id, decision.ID)
	requireServeSentence(t, blocked, "has a current maintainer rejection "+decision.ID)
	if tip := g.ServerTip("main"); tip != g.Base {
		t.Fatalf("hosted main moved to %s", tip)
	}
}

// Two executing actors request advancement of two ready candidates against
// the same expected tip at the same time. The repository lock serializes
// them: exactly one moves main, and the other is refused at step 3 or 5.
func TestStandingServeVerdictRace(t *testing.T) {
	// Arrange
	g := newStandingServeFixture(t, func(actors map[string]string) string {
		policy := standingServePolicyDocument()
		policy.Rules[0].Actors = []string{"E", "X"}
		return policy.JSON(actors)
	})
	first := g.StandingReady(map[string]string{"src/first.txt": "first\n"})
	second := g.StandingReady(map[string]string{"src/second.txt": "second\n"})
	requests := []struct {
		who string
		p   serveGovernedProposal
	}{{"E", first}, {"X", second}}
	for _, request := range requests {
		standingServeImport(g, request.who)
	}

	// Act
	type outcome struct {
		output     string
		rejections []serveRejection
		ok         bool
	}
	outcomes := make([]outcome, len(requests))
	var wait sync.WaitGroup
	for index, request := range requests {
		wait.Add(1)
		go func() {
			defer wait.Done()
			output, rejections, ok := g.PushMain(request.who, request.p.Candidate.Head,
				"hn-advance="+request.p.Candidate.ID, "hn-expected-tip="+g.Base, "hn-rule=ordinary")
			outcomes[index] = outcome{output, rejections, ok}
		}()
	}
	wait.Wait()

	// Assert
	var winners, losers []int
	for index, result := range outcomes {
		if result.ok {
			winners = append(winners, index)
		} else {
			losers = append(losers, index)
		}
	}
	if len(winners) != 1 || len(losers) != 1 {
		t.Fatalf("want exactly one accepted request, got outcomes:\n%s\n---\n%s", outcomes[0].output, outcomes[1].output)
	}
	winner, loser := requests[winners[0]], outcomes[losers[0]]
	if tip := g.ServerTip("main"); tip != winner.p.Candidate.Head {
		t.Fatalf("hosted main = %s, want the winner's head %s", tip, winner.p.Candidate.Head)
	}
	if len(loser.rejections) != 1 {
		t.Fatalf("loser got %d rejections:\n%s", len(loser.rejections), loser.output)
	}
	if code := loser.rejections[0].Code; code != serveReasonProtectedStaleTip && code != serveReasonProtectedNotFastForward {
		t.Fatalf("loser code %s, want protected-stale-tip or protected-not-fast-forward:\n%s", code, loser.output)
	}
}

// T069: the guide's standing-serve-veto example is the real US4-3 output,
// line for line, after its placeholders are replaced with fixture IDs.
func TestStandingServeDocumentedVeto(t *testing.T) {
	// Arrange
	contents, err := os.ReadFile(filepath.Join("docs", "serve-v0.md"))
	if err != nil {
		t.Fatal(err)
	}
	const marker = "<!-- example: standing-serve-veto -->\n"
	guide := string(contents)
	if strings.Count(guide, marker) != 1 {
		t.Fatalf("the serve guide must contain exactly one %q", strings.TrimSpace(marker))
	}
	_, after, _ := strings.Cut(guide, marker)
	_, after, found := strings.Cut(after, "```text\n")
	if !found {
		t.Fatal("no text block after the example marker")
	}
	block, _, found := strings.Cut(after, "```\n")
	if !found {
		t.Fatal("unterminated example block")
	}
	documented := strings.Split(strings.TrimSuffix(block, "\n"), "\n")
	if len(documented) != 2 {
		t.Fatalf("example has %d lines, want the rejection line and its sentence", len(documented))
	}

	// Act
	g, p, change, _, lines := standingServeVetoCase(t)

	// Assert
	replace := strings.NewReplacer(
		"<executor>", g.actors["E"],
		"sha256:<candidate>", p.Candidate.ID,
		"sha256:<change request>", change.ID,
	)
	want := []string{replace.Replace(documented[0]), replace.Replace(documented[1])}
	requireServeRelayedLines(t, lines, want)
}

// Client rule advancement (WP15): hn automerge PROPOSAL --rule NAME
// --execute --remote origin under a standing rule, through the
// forced-command harness.

// standingServeAutomerge runs hn automerge candidate --rule rule --execute
// --remote origin in who's clone with extra environment entries.
func standingServeAutomerge(g *serveGovernedFixture, who, candidate, rule string, env ...string) (string, int) {
	g.t.Helper()
	output, status, err := g.advanceExec(who, env, g.h.Binary, "automerge", candidate, "--rule", rule, "--execute", "--remote", "origin")
	if err != nil {
		g.t.Fatalf("hn automerge %s --rule %s --execute --remote origin: %v\n%s", candidate, rule, err, output)
	}
	return output, status
}

// standingServePreview runs the keyless preview hn automerge candidate
// --rule rule in who's clone.
func standingServePreview(g *serveGovernedFixture, who, candidate, rule string) (string, int) {
	g.t.Helper()
	output, status, err := g.advanceExec(who, nil, g.h.Binary, "automerge", candidate, "--rule", rule)
	if err != nil {
		g.t.Fatalf("hn automerge %s --rule %s: %v\n%s", candidate, rule, err, output)
	}
	return output, status
}

// standingServeSelectAllTrusted saves the all-trusted selection for origin
// in each named clone, as an executing actor must before hn automerge
// --remote.
func standingServeSelectAllTrusted(g *serveGovernedFixture, names ...string) {
	g.t.Helper()
	for _, who := range names {
		g.hn(who, "replication", "select", "origin", "--all-trusted")
	}
}

// standingServeAdvanceArrange is the US4-1 starting point: the default
// policy, E with the all-trusted selection and main checked out at the
// hosted tip, and a ready candidate under src/ that E has imported.
func standingServeAdvanceArrange(t *testing.T) (*serveGovernedFixture, serveGovernedProposal) {
	t.Helper()
	g := newStandingServeFixture(t, standingServeDefaultPolicy)
	standingServeSelectAllTrusted(g, "E")
	p := g.StandingReady(map[string]string{"src/advance.txt": "advanced under rule ordinary\n"})
	standingServeImport(g, "E")
	return g, p
}

// standingServeAdvanceLines are the last two output lines of a rule
// advancement under rule ordinary.
func standingServeAdvanceLines(head, candidate, fact string, evidence int) string {
	return fmt.Sprintf("Advanced origin refs/heads/main to %s for proposal %s\nRecorded rule merge event %s under rule ordinary with %d evidence item(s)\n",
		shortOID(head), shortID(candidate), shortID(fact), evidence)
}

// standingServeChainFacts decodes the proposal.merged facts for candidate on
// who's own actor chain, which is empty when who has never signed.
func standingServeChainFacts(g *serveGovernedFixture, who, candidate string) []standingHarnessFact {
	g.t.Helper()
	ref := g.actorRef(who)
	if g.git(who, "for-each-ref", "--format=%(refname)", ref) == "" {
		return nil
	}
	var facts []standingHarnessFact
	for _, commit := range strings.Fields(g.git(who, "rev-list", ref)) {
		payload := runOperationalGitBytes(g.t, g.work[who], "show", commit+":event.json")
		fact := standingHarnessReadFact(g.t, payload)
		if fact.Kind == "proposal.merged" && fact.Subject == candidate {
			facts = append(facts, fact)
		}
	}
	return facts
}

// standingServeRuleFact requires who's chain to hold exactly one merge fact
// for p: a rule merge fact under rule ordinary signed by who, with commit
// and head equal to the candidate head, the base policy, and the approval
// and the passing result as evidence. It returns the fact.
func standingServeRuleFact(t *testing.T, g *serveGovernedFixture, who string, p serveGovernedProposal) standingHarnessFact {
	t.Helper()
	facts := standingServeChainFacts(g, who, p.Candidate.ID)
	if len(facts) != 1 {
		t.Fatalf("%s's chain holds %d merge facts for %s, want 1: %+v", who, len(facts), p.Candidate.ID, facts)
	}
	_, policy, err := loadReplicationPolicyAt(g.hosted, g.hosted, p.Candidate.Base)
	if err != nil {
		t.Fatal(err)
	}
	fact := facts[0]
	evidence := []string{p.Approval.ID, p.Result.ID}
	slices.Sort(evidence)
	if fact.Rule != "ordinary" || fact.Actor != g.actors[who] || fact.Head != p.Candidate.Head || fact.Commit != p.Candidate.Head ||
		fact.Policy != policy || !slices.Equal(fact.Evidence, evidence) {
		t.Fatalf("merge fact %+v, want rule ordinary by %s with commit = head = %s under %s citing %q",
			fact, who, p.Candidate.Head, policy, evidence)
	}
	return fact
}

// standingServePushOptions lists the hn- push options a Git packet trace
// shows the client sending, in order.
func standingServePushOptions(t *testing.T, trace string) []string {
	t.Helper()
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatalf("read packet trace: %v", err)
	}
	var options []string
	for _, line := range strings.Split(string(data), "\n") {
		if _, sent, found := strings.Cut(line, " push> "); found && strings.HasPrefix(sent, "hn-") {
			options = append(options, strings.TrimSpace(sent))
		}
	}
	return options
}

// standingServeAllTrustedStatus clones the hosted repository with a new
// bound key and no Hubnot identity, saves the all-trusted selection, syncs,
// and returns hn proposal status for candidate (US4-2).
func standingServeAllTrustedStatus(g *serveGovernedFixture, seed, candidate string) string {
	g.t.Helper()
	key := serveHarnessAddKey(g.h, serveHarnessActor("standing-verifier-"+seed))
	client := serveHarnessClient(g.t, g.h, key, nil)
	dir := filepath.Join(g.t.TempDir(), "verifier-"+seed)
	client.Git("", "clone", "-q", serveHarnessCloneURL("r"), dir)
	client.Hn(dir, "replication", "select", "origin", "--all-trusted")
	client.Hn(dir, "sync", "origin")
	return client.Hn(dir, "proposal", "status", candidate)
}

// US4-1, US4-2 and C-008: E, an executing actor of rule ordinary and not a
// maintainer, advances hosted main to the exact head of a candidate no
// maintainer accepted. The client sends hn-rule, signs the rule merge fact
// with commit equal to the head and publishes it; a fresh clone with the
// all-trusted selection reports the candidate merged by the rule.
func TestStandingServeAdvanceUserStory4(t *testing.T) {
	// Arrange
	g, p := standingServeAdvanceArrange(t)
	candidate, head := p.Candidate.ID, p.Candidate.Head
	commits := serveHarnessGit(t, g.hosted, "rev-list", "--count", head)
	local := g.serveAdvanceLocalState("E")
	trace := filepath.Join(t.TempDir(), "packets")

	// Act
	output, status := standingServeAutomerge(g, "E", candidate, "ordinary", "GIT_TRACE_PACKET="+trace)

	// Assert: US4-1.
	if status != 0 {
		t.Fatalf("hn automerge --execute --remote origin exited %d:\n%s", status, output)
	}
	fact := standingServeRuleFact(t, g, "E", p)
	if want := standingServeAdvanceLines(head, candidate, fact.ID, 2); !strings.HasSuffix(output, want) {
		t.Fatalf("output:\n%s\nwant it to end with:\n%s", output, want)
	}
	if tip := g.ServerTip("main"); tip != head {
		t.Fatalf("hosted main = %s, want the candidate head %s", tip, head)
	}
	if got := serveHarnessGit(t, g.hosted, "rev-list", "--count", serveGovernedMain); got != commits {
		t.Fatalf("main reaches %s commits and the head %s; a commit was created", got, commits)
	}
	if after := g.serveAdvanceLocalState("E"); after != local {
		t.Fatalf("advancement changed E's checkout:\nbefore\n%s\nafter\n%s", local, after)
	}
	if !g.Published("E") {
		t.Fatal("the server does not hold E's chain with the rule merge fact")
	}
	want := []string{"hn-advance=" + candidate, "hn-expected-tip=" + g.Base, "hn-rule=ordinary"}
	if options := standingServePushOptions(t, trace); !slices.Equal(options, want) {
		t.Fatalf("the client sent push options %q, want %q", options, want)
	}

	// C-008: every hosted chain belongs to a participant, and the only merge
	// fact for the candidate is E's.
	participants := map[string]bool{}
	for _, actor := range g.actors {
		participants[serveActorRefPrefix+actor] = true
	}
	for _, ref := range strings.Fields(serveHarnessGit(t, g.hosted, "for-each-ref", "--format=%(refname)", serveActorRefPrefix)) {
		if !participants[ref] {
			t.Fatalf("hosted chain %s belongs to no participant", ref)
		}
	}
	for _, who := range []string{"A", "M", "R", "S", "U", "X"} {
		if facts := standingServeChainFacts(g, who, candidate); len(facts) != 0 {
			t.Fatalf("%s's chain holds merge facts %+v", who, facts)
		}
	}

	// US4-2: a fresh clone verifies the rule merge.
	verified := standingServeAllTrustedStatus(g, "us4-2", candidate)
	assertOperationalContains(t, verified, "Status:   merged\n", standingAutomergeRuleStatusLine("ordinary", g.actors["E"], fact.ID))
}

// T071: a refused rule advancement names the server upgrade only when the
// server rejected the request as an unsupported command and a rule was
// sent.
func TestStandingServeAdvanceRuleUnsupported(t *testing.T) {
	rejection := func(code string) serveRejection {
		return serveRejection{Ref: serveGovernedMain, Actor: serveTestActor, Code: code, Sentence: "refused."}
	}
	for _, tc := range []struct {
		name       string
		rejections []serveRejection
		rule       string
		want       bool
	}{
		{"older server refuses hn-rule", []serveRejection{rejection(serveReasonUnsupportedCommand)}, "ordinary", true},
		{"unsupported among other codes", []serveRejection{rejection(serveReasonProtectedStaleTip), rejection(serveReasonUnsupportedCommand)}, "ordinary", true},
		{"no rule sent", []serveRejection{rejection(serveReasonUnsupportedCommand)}, "", false},
		{"rule veto", []serveRejection{rejection(serveReasonProtectedRuleVeto)}, "ordinary", false},
		{"rule not satisfied", []serveRejection{rejection(serveReasonProtectedRuleNotSatisfied)}, "ordinary", false},
		{"not an executing actor", []serveRejection{rejection(serveReasonAdvanceNotRuleExecutor)}, "ordinary", false},
		{"stale tip", []serveRejection{rejection(serveReasonProtectedStaleTip)}, "ordinary", false},
		{"no rejections", nil, "ordinary", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			got := serveAdvanceRuleUnsupported(tc.rejections, tc.rule)

			// Assert
			if got != tc.want {
				t.Fatalf("serveAdvanceRuleUnsupported(%+v, %q) = %t, want %t", tc.rejections, tc.rule, got, tc.want)
			}
		})
	}
}

// T071: the rerun command of a rule advancement is the same hn automerge
// command; without a rule it is hn merge --remote, unchanged.
func TestStandingServeAdvanceRerunCommand(t *testing.T) {
	// Arrange
	candidate := serveTestEvent

	// Act and assert
	if got, want := serveAdvanceRerun(candidate, "origin", serveGovernedMain, serveAdvanceBinding{Rule: "ordinary"}),
		"hn automerge "+candidate+" --rule ordinary --execute --remote origin"; got != want {
		t.Fatalf("rule rerun %q, want %q", got, want)
	}
	if got, want := serveAdvanceRerun(candidate, "origin", serveGovernedMain, serveAdvanceBinding{ExpectedActor: serveTestActor}),
		"hn merge "+candidate+" --remote origin"; got != want {
		t.Fatalf("maintainer rerun %q, want %q", got, want)
	}
	if got, want := serveAdvanceRerun(candidate, "hosted", "refs/heads/release", serveAdvanceBinding{}),
		"hn merge "+candidate+" --remote hosted --branch release"; got != want {
		t.Fatalf("maintainer rerun %q, want %q", got, want)
	}
}

// standingServeInterrupted runs who's rule advancement with the test-only
// interruption after step and requires the documented interruption failure.
func standingServeInterrupted(g *serveGovernedFixture, who, candidate, step string) {
	g.t.Helper()
	output, status := standingServeAutomerge(g, who, candidate, "ordinary", "HN_INTERNAL_TESTING=1", serveAdvanceInterruptEnv+"="+step)
	if status != 1 || !strings.HasSuffix(output, "hn: interrupted for testing after "+step+"\n") {
		g.t.Fatalf("interrupted rule advancement after %s exited %d:\n%s", step, status, output)
	}
}

// US4-3 through the client. E's command imports the hosted facts first, so
// a change request S published after E's last sync blocks it locally. A
// change request that reaches the server after the client's import and
// before its push is the server's veto: the client stops after publishing
// its facts, S publishes, and the push the client would send is refused
// with protected-rule-veto.
func TestStandingServeAdvanceVeto(t *testing.T) {
	// Arrange
	g := newStandingServeFixture(t, standingServeDefaultPolicy)
	standingServeSelectAllTrusted(g, "E")
	p := g.StandingReady(map[string]string{"src/veto-client.txt": "vetoed before the import\n"})
	standingServeImport(g, "E")
	g.act("S", "review.submit", "review", p.Candidate.ID, "--request-changes", "--body", "Changes requested after E's last sync.")
	before := g.ServerRefs()

	// Act
	output, status := standingServeAutomerge(g, "E", p.Candidate.ID, "ordinary")

	// Assert
	if status == 0 || !strings.Contains(output, "hn: automerge blocked: proposal has a current trusted change request\n") {
		t.Fatalf("automerge with a hosted change request exited %d:\n%s", status, output)
	}
	if after := g.ServerRefs(); after != before {
		t.Fatalf("the refused command changed hosted refs:\nbefore\n%s\nafter\n%s", before, after)
	}
	if facts := standingServeChainFacts(g, "E", p.Candidate.ID); len(facts) != 0 {
		t.Fatalf("the refused command recorded merge facts %+v", facts)
	}

	// Arrange: a second candidate; E's command stops between its import and
	// its push, and S's change request arrives in that interval.
	q := g.StandingReady(map[string]string{"src/veto-server.txt": "vetoed after the import\n"})
	standingServeImport(g, "E")
	standingServeInterrupted(g, "E", q.Candidate.ID, "publish")
	change := g.act("S", "review.submit", "review", q.Candidate.ID, "--request-changes", "--body", "Changes requested before E's push.")

	// Act
	rejection, _ := standingServeRuleRefused(t, g, "E", q.Candidate.ID, q.Candidate.Head, "ordinary", serveReasonProtectedRuleVeto)

	// Assert
	requireServeIDs(t, rejection, q.Candidate.ID, change.ID)
	if tip := g.ServerTip("main"); tip != g.Base {
		t.Fatalf("hosted main moved to %s", tip)
	}
	if facts := standingServeChainFacts(g, "E", q.Candidate.ID); len(facts) != 0 {
		t.Fatalf("a vetoed advancement recorded merge facts %+v", facts)
	}
}

// The binding survives an interruption before the push: after the client
// stops once its facts are published, the branch and E's chain are
// unchanged, and a rerun of the same command runs every step and records
// one rule merge fact. WP19 covers the other interruption points.
func TestStandingServeAdvanceInterruptedAfterPublish(t *testing.T) {
	// Arrange
	g, p := standingServeAdvanceArrange(t)
	candidate, head := p.Candidate.ID, p.Candidate.Head

	// Act
	standingServeInterrupted(g, "E", candidate, "publish")

	// Assert
	if tip := g.ServerTip("main"); tip != g.Base {
		t.Fatalf("after the interruption hosted main = %s, want %s", tip, g.Base)
	}
	if facts := standingServeChainFacts(g, "E", candidate); len(facts) != 0 {
		t.Fatalf("after the interruption E holds merge facts %+v", facts)
	}

	// Act: the full rerun.
	output, status := standingServeAutomerge(g, "E", candidate, "ordinary")

	// Assert
	if status != 0 {
		t.Fatalf("the rerun exited %d:\n%s", status, output)
	}
	fact := standingServeRuleFact(t, g, "E", p)
	if want := standingServeAdvanceLines(head, candidate, fact.ID, 2); !strings.HasSuffix(output, want) {
		t.Fatalf("rerun output:\n%s\nwant it to end with:\n%s", output, want)
	}
	if tip := g.ServerTip("main"); tip != head {
		t.Fatalf("after the rerun hosted main = %s, want %s", tip, head)
	}
	if !g.Published("E") {
		t.Fatal("after the rerun the rule merge fact is not published")
	}
}

// standingServeChains is the local and hosted value of each named actor's
// chain, empty when absent, for unchanged-state comparisons.
func standingServeChains(g *serveGovernedFixture, names ...string) string {
	g.t.Helper()
	var state []string
	for _, who := range names {
		ref := g.actorRef(who)
		state = append(state, who+" local "+g.git(who, "for-each-ref", "--format=%(objectname)", ref), who+" hosted "+g.hostedRef(ref))
	}
	return strings.Join(state, "\n")
}

// FR-018, FR-019 and NFR-003 remote. E, an executing actor and not a
// maintainer, cannot use hn merge --remote or a direct push, and X cannot
// execute rule ordinary; every attempt leaves the hosted refs and both
// chains unchanged. M's hn merge --remote of an accepted candidate sends
// exactly hn-advance and hn-expected-tip and records an acceptance merge
// fact, as before this mission.
func TestStandingServeAdvanceForbiddenCommands(t *testing.T) {
	// Arrange
	g := newStandingServeFixture(t, standingServeDefaultPolicy)
	standingServeSelectAllTrusted(g, "E", "X")
	p := g.StandingReady(map[string]string{"src/forbidden.txt": "satisfies rule ordinary\n"})
	candidate, head := p.Candidate.ID, p.Candidate.Head
	for _, who := range []string{"E", "X"} {
		standingServeImport(g, who)
	}
	before, chains := g.ServerRefs(), standingServeChains(g, "E", "X")
	requireUnchanged := func(label string) {
		t.Helper()
		if after := g.ServerRefs(); after != before {
			t.Fatalf("%s changed hosted refs:\nbefore\n%s\nafter\n%s", label, before, after)
		}
		if after := standingServeChains(g, "E", "X"); after != chains {
			t.Fatalf("%s changed a chain:\nbefore\n%s\nafter\n%s", label, chains, after)
		}
	}

	// Act and assert: E's hn merge --remote stops at step 1 on the missing
	// acceptance.
	output, status := g.Advance("E", candidate)
	if want := "hn: advancement step 1 (validate locally) failed: proposal lacks 1 required acceptance decision(s)\n"; status != 1 || output != want {
		t.Fatalf("E's hn merge --remote exited %d with %q, want %q", status, output, want)
	}
	requireUnchanged("E's hn merge --remote")

	// E's direct push needs maintainer acceptance.
	direct := g.RejectMain("E", head, serveReasonProtectedNotReady)
	requireServeIDs(t, direct, candidate)
	requireServeMissing(t, direct, serveEvidenceAcceptance)
	requireUnchanged("E's direct push")

	// X executes rule other only.
	output, status = standingServeAutomerge(g, "X", candidate, "ordinary")
	if want := fmt.Sprintf("hn: identity %s is not an executing actor for automerge rule %q\n", g.actors["X"], "ordinary"); status == 0 || !strings.HasSuffix(output, want) {
		t.Fatalf("X's hn automerge --remote exited %d, want %q:\n%s", status, want, output)
	}
	requireUnchanged("X's hn automerge --remote")

	// Arrange: M accepts the candidate.
	acceptance := g.Accept(candidate)
	standingServeImport(g, "E")
	before, chains = g.ServerRefs(), standingServeChains(g, "E", "X")

	// Act and assert: E is still refused at step 1, as not a maintainer.
	output, status = g.Advance("E", candidate)
	if want := "hn: advancement step 1 (validate locally) failed: identity " + shortID(g.actors["E"]) + " is not a maintainer under proposal policy\n"; status != 1 || output != want {
		t.Fatalf("E's hn merge --remote of an accepted candidate exited %d with %q, want %q", status, output, want)
	}
	requireUnchanged("E's hn merge --remote of an accepted candidate")

	// M's maintainer advancement is unchanged and never sends hn-rule.
	trace := filepath.Join(t.TempDir(), "packets")
	output, status = g.Advance("M", candidate, "GIT_TRACE_PACKET="+trace)
	if status != 0 {
		t.Fatalf("M's hn merge --remote exited %d:\n%s", status, output)
	}
	fact := g.requireFastForwardFact("M", candidate, head, acceptance.ID)
	if want := serveAdvanceSuccessLines("origin", head, candidate, fact.ID, 1); output != want {
		t.Fatalf("output:\n%s\nwant:\n%s", output, want)
	}
	want := []string{"hn-advance=" + candidate, "hn-expected-tip=" + g.Base}
	if options := standingServePushOptions(t, trace); !slices.Equal(options, want) {
		t.Fatalf("hn merge --remote sent push options %q, want %q", options, want)
	}
	if facts := standingServeChainFacts(g, "M", candidate); len(facts) != 1 || facts[0].Rule != "" {
		t.Fatalf("M's merge facts %+v, want one acceptance merge fact without a rule", facts)
	}
}

// T075: the guide's standing-serve-advance example is the real US4-1 run.
// The documented command runs in E's clone after its placeholder is
// replaced, and the documented output lines are the last lines it prints.
func TestStandingServeDocumentedAdvance(t *testing.T) {
	// Arrange
	contents, err := os.ReadFile(filepath.Join("docs", "serve-v0.md"))
	if err != nil {
		t.Fatal(err)
	}
	const marker = "<!-- example: standing-serve-advance -->\n"
	guide := string(contents)
	if strings.Count(guide, marker) != 1 {
		t.Fatalf("the serve guide must contain exactly one %q", strings.TrimSpace(marker))
	}
	_, after, _ := strings.Cut(guide, marker)
	block := func(fence string) string {
		t.Helper()
		_, rest, found := strings.Cut(after, fence)
		if !found {
			t.Fatalf("no %q block after the example marker", strings.TrimSpace(fence))
		}
		text, rest, found := strings.Cut(rest, "```\n")
		if !found {
			t.Fatal("unterminated example block")
		}
		after = rest
		return strings.TrimSuffix(text, "\n")
	}
	command := strings.Fields(block("```sh\n"))
	documented := strings.Split(block("```text\n"), "\n")
	if len(command) < 2 || command[0] != "hn" || len(documented) != 2 {
		t.Fatalf("example has command %q and %d output lines, want an hn command and two lines", command, len(documented))
	}
	g, p := standingServeAdvanceArrange(t)
	args := slices.Clone(command[1:])
	for index, arg := range args {
		if arg == "sha256:1c2d3e4f..." {
			args[index] = p.Candidate.ID
		}
	}
	if want := []string{"automerge", p.Candidate.ID, "--rule", "ordinary", "--execute", "--remote", "origin"}; !slices.Equal(args, want) {
		t.Fatalf("documented command runs %q, want %q", args, want)
	}

	// Act
	output, status, err := g.advanceExec("E", nil, g.h.Binary, args...)

	// Assert
	if err != nil || status != 0 {
		t.Fatalf("documented command exited %d (%v):\n%s", status, err, output)
	}
	fact := standingServeRuleFact(t, g, "E", p)
	replace := strings.NewReplacer(
		"9b2e4c6a8d0f", shortOID(p.Candidate.Head),
		"1c2d3e4f5a6b", shortID(p.Candidate.ID),
		"7d8e9f0a1b2c", shortID(fact.ID),
	)
	want := replace.Replace(strings.Join(documented, "\n")) + "\n"
	if !strings.HasSuffix(output, want) {
		t.Fatalf("output:\n%s\nwant it to end with the documented lines:\n%s", output, want)
	}
}

// standingServeAgreement is one fixture of the preview-to-server
// comparison (NFR-002): the candidate, the rule named, the declared target
// tip, a blocker the preview must list and the code the server must answer
// ("" for the satisfied fixture).
type standingServeAgreement struct {
	Name      string
	Candidate operationalEvent
	Rule      string
	Tip       string
	Want      string
	Code      string
}

// standingServeAgreementCodes are the server steps a rule-satisfaction
// refusal may come from: 7a, 8, 9 and 10.
var standingServeAgreementCodes = []string{
	serveReasonProtectedDestinationPolicy, serveReasonProposalRefMismatch, serveReasonProtectedBlocked,
	serveReasonProtectedRuleVeto, serveReasonProtectedRuleNotSatisfied,
}

// standingServeMatchedInputs asserts the four matched inputs of the
// agreement contract for f in E's clone and on the server. The caller has
// just run hn sync origin in E's clone.
func standingServeMatchedInputs(t *testing.T, g *serveGovernedFixture, f standingServeAgreement) {
	t.Helper()
	// Target branch and tip.
	branch, local, hosted := g.git("E", "symbolic-ref", "HEAD"), g.git("E", "rev-parse", "refs/heads/main"), g.ServerTip("main")
	if branch != serveGovernedMain || local != hosted || hosted != f.Tip {
		t.Fatalf("%s: local main equals hosted main, and both equal the fixture's declared tip: E has %s at %s, hosted main is %s, declared tip %s",
			f.Name, branch, local, hosted, f.Tip)
	}
	// Base and target policy: the same commits, so the same digests.
	gitDir := filepath.Join(g.work["E"], ".git")
	for _, commit := range []string{f.Candidate.Base, f.Tip} {
		_, localDigest, localErr := loadReplicationPolicyAt(gitDir, gitDir, commit)
		_, hostedDigest, hostedErr := loadReplicationPolicyAt(g.hosted, g.hosted, commit)
		if localErr != nil || hostedErr != nil || localDigest != hostedDigest {
			t.Fatalf("%s: the policy at %s differs between E's clone (%s, %v) and the server (%s, %v)",
				f.Name, commit, localDigest, localErr, hostedDigest, hostedErr)
		}
	}
	// Candidate code: the head and the proposal code ref on both surfaces.
	// E's clone holds the code ref it imported from origin.
	imported := acceptedProposalRef("origin", f.Candidate.ID)
	if got := g.git("E", "rev-parse", "--verify", "--quiet", imported); got != f.Candidate.Head {
		t.Fatalf("%s: E's %s = %q, want the head %s", f.Name, imported, got, f.Candidate.Head)
	}
	if ref, got := proposalRef(f.Candidate.ID), g.hostedRef(proposalRef(f.Candidate.ID)); got != f.Candidate.Head {
		t.Fatalf("%s: hosted %s = %q, want the head %s", f.Name, ref, got, f.Candidate.Head)
	}
	// Fact set: E imports with the all-trusted selection.
	assertOperationalContains(t, g.hn("E", "replication", "show", "origin"), "all-trusted: true\n")
}

// standingServeOpenApproved opens a candidate for files on base and records
// approver's approval, with no run request.
func standingServeOpenApproved(g *serveGovernedFixture, base, approver string, files map[string]string) operationalEvent {
	g.t.Helper()
	candidate := g.OpenProposal(base, g.Commit(base, files))
	g.act(approver, "review.submit", "review", candidate.ID, "--approve", "--body", "Approved by the agreement fixture.")
	return candidate
}

// standingServeTested adds M's run request and U's passing result.
func standingServeTested(g *serveGovernedFixture, candidate string) {
	g.t.Helper()
	g.RunCI(g.RequestCI(candidate).ID)
}

// T095 (NFR-002): under matched inputs, E's keyless preview is eligible
// exactly when the server accepts E's rule push, and every server refusal
// comes from steps 7a, 8, 9 or 10. Authorization and transport are tested
// on their own in TestStandingServeAdvanceAuthorizationAndTransport.
func TestStandingServeAdvanceAgreementPreviewServer(t *testing.T) {
	// Arrange
	g := newStandingServeFixture(t, standingServeDefaultPolicy)
	standingServeSelectAllTrusted(g, "E")
	var table []string
	compare := func(f standingServeAgreement) {
		t.Helper()
		standingServeImport(g, "E")
		standingServeMatchedInputs(t, g, f)
		hosted := g.ServerRefs()
		refs := g.git("E", "for-each-ref", "--format=%(objectname) %(refname)")

		// Act: the preview, then the push the client would send.
		preview, status := standingServePreview(g, "E", f.Candidate.ID, f.Rule)
		if after := g.git("E", "for-each-ref", "--format=%(objectname) %(refname)"); after != refs {
			t.Fatalf("%s: the preview changed E's refs", f.Name)
		}
		if after := g.ServerRefs(); after != hosted {
			t.Fatalf("%s: a fact was published between the preview and the push", f.Name)
		}
		output, rejections, accepted := g.RulePush("E", f.Candidate.ID, f.Candidate.Head, f.Rule)

		// Assert
		eligible := status == 0 && len(standingAutomergeBlockers(preview)) == 0
		if f.Want == "" {
			requireStandingAutomergeEligible(t, preview, status)
		} else {
			requireStandingAutomergeBlockedBy(t, preview, status, f.Want)
		}
		if eligible != accepted {
			t.Fatalf("%s: preview eligible %t but server accepted %t\npreview:\n%s\npush:\n%s", f.Name, eligible, accepted, preview, output)
		}
		verdict := "accepted"
		if accepted {
			if tip := g.ServerTip("main"); tip != f.Candidate.Head {
				t.Fatalf("%s: hosted main = %s after an accepted push, want %s", f.Name, tip, f.Candidate.Head)
			}
		} else {
			if len(rejections) != 1 || !slices.Contains(standingServeAgreementCodes, rejections[0].Code) || rejections[0].Code != f.Code {
				t.Fatalf("%s: server rejections %+v, want one %s from steps 7a to 10:\n%s", f.Name, rejections, f.Code, output)
			}
			if after := g.ServerRefs(); after != hosted {
				t.Fatalf("%s: the refused push changed hosted refs", f.Name)
			}
			verdict = rejections[0].Code
		}
		previewOutcome := map[bool]string{true: "eligible", false: "blocked"}[eligible]
		table = append(table, fmt.Sprintf("%-38s preview %-8s server %-28s agree", f.Name, previewOutcome, verdict))
	}
	base := g.Base
	fixture := func(name string, candidate operationalEvent, rule, want, code string) standingServeAgreement {
		return standingServeAgreement{Name: name, Candidate: candidate, Rule: rule, Tip: candidate.Base, Want: want, Code: code}
	}

	// Act and assert: every refusal on the signed base first, so the target
	// tip stays at the base. One ready candidate serves the three rules
	// that cannot apply to it, and then S's change request.
	c := g.StandingReady(map[string]string{"src/ready.txt": "ready under rule ordinary\n"}).Candidate
	compare(fixture("rule that does not exist", c, "nosuch", `automerge rule "nosuch" does not exist in the signed base policy`, serveReasonProtectedRuleNotSatisfied))
	compare(fixture("rule with acceptance maintainers", c, "maint", standingAutomergeNotAccepted, serveReasonProtectedRuleNotSatisfied))
	compare(fixture("rule for another target branch", c, "release", `checked-out branch does not match rule target "release"`, serveReasonProtectedRuleNotSatisfied))
	g.act("S", "review.submit", "review", c.ID, "--request-changes", "--body", "Changes requested by the agreement fixture.")
	compare(fixture("trusted change request", c, "ordinary", standingAutomergeChangeRequest, serveReasonProtectedRuleVeto))

	failing := g.StandingReady(map[string]string{"src/failure.txt": "trusted failure\n"})
	writeServeTestFile(t, g.CIFailFlag, "fail\n", 0o644)
	g.RunCI(failing.Request.ID, "--rerun")
	if err := os.Remove(g.CIFailFlag); err != nil {
		t.Fatal(err)
	}
	compare(fixture("trusted failure", failing.Candidate, "ordinary", `pipeline "test" has a current trusted failure`, serveReasonProtectedRuleVeto))

	c = g.StandingReady(map[string]string{"src/rejected.txt": "maintainer rejection\n"}).Candidate
	g.Reject(c.ID)
	compare(fixture("maintainer rejection", c, "ordinary", "proposal has a current maintainer rejection", serveReasonProtectedBlocked))

	g.Author = "S"
	c = g.StandingReady(map[string]string{"src/by-s.txt": "authored by S\n"}).Candidate
	g.Author = "A"
	compare(fixture("author outside the rule", c, "ordinary", "proposal author is not allowed by the rule", serveReasonProtectedRuleNotSatisfied))

	c = g.StandingReady(map[string]string{"other/x": "outside every rule path\n"}).Candidate
	compare(fixture("path outside the rule", c, "ordinary", `changed path "other/x" is outside the rule allowlist`, serveReasonProtectedRuleNotSatisfied))

	c = g.StandingReady(map[string]string{
		serveGovernedPolicyPath: standingServeDefaultPolicy(g.actors) + "\n", "src/policy-change.txt": "allowlisted change\n",
	}).Candidate
	compare(fixture("policy change", c, "ordinary", "policy amendments require manual merge", serveReasonProtectedRuleNotSatisfied))

	c = standingServeOpenApproved(g, base, "S", map[string]string{"src/uncounted.txt": "approved by an uncounted reviewer\n"})
	standingServeTested(g, c.ID)
	compare(fixture("too few counted approvals", c, "ordinary", standingAutomergeThreshold0of1, serveReasonProtectedRuleNotSatisfied))

	c = standingServeOpenApproved(g, base, "R", map[string]string{"src/untested.txt": "no passing result\n"})
	compare(fixture("too few passing results", c, "ordinary", standingAutomergeNoPass, serveReasonProtectedRuleNotSatisfied))

	// The destination policy differs (WP14 T068 step 6): M lands policy
	// amendment H1, and A builds the head on H1 but signs the older base.
	// E's main and hosted main are both at H1, so the target differs from
	// the signed base and the server reaches step 7a.
	amended := standingServePolicyDocument()
	amended.Rules[1].Paths = []string{"src/", "tests/", "docs/", "other/"}
	h1 := g.Commit(base, map[string]string{serveGovernedPolicyPath: amended.JSON(g.actors)})
	g.ReadyProposal(base, h1)
	g.AcceptMain("M", h1)
	g.git("E", "fetch", "-q", "origin")
	g.git("E", "checkout", "-q", "-B", "main", h1)
	moved := g.OpenProposal(base, g.Commit(h1, map[string]string{"src/after-amendment.txt": "built on the amendment\n"}))
	g.Approve(moved.ID)
	standingServeTested(g, moved.ID)
	destination := fixture("destination policy differs from base", moved, "ordinary",
		"current target policy differs from the signed base policy", serveReasonProtectedDestinationPolicy)
	destination.Tip = h1
	compare(destination)

	// The satisfied candidate is based on H1 and compared last, because
	// the server accepts it and main moves.
	satisfied := g.OpenProposal(h1, g.Commit(h1, map[string]string{"src/satisfied.txt": "satisfies rule ordinary\n"}))
	g.Approve(satisfied.ID)
	standingServeTested(g, satisfied.ID)
	compare(fixture("satisfied", satisfied, "ordinary", "", ""))

	t.Logf("preview and hn serve agreement on rule satisfaction, %d fixtures, 100%% agreement:\n%s", len(table), strings.Join(table, "\n"))
}

// T095 (NFR-002): the checks agreement excludes, tested on their own.
// Authorization: X's preview of a satisfied candidate is eligible, but
// X's rule push and X's command are refused. Transport and binding: a
// stale hn-expected-tip, a selection that omits a trusted actor, a remote
// that is not an hn serve remote, and a local main behind the hosted tip
// are each refused, with the hosted refs unchanged.
func TestStandingServeAdvanceAuthorizationAndTransport(t *testing.T) {
	// Arrange
	g := newStandingServeFixture(t, standingServeDefaultPolicy)
	standingServeSelectAllTrusted(g, "E", "X")
	p := g.StandingReady(map[string]string{"src/authorized.txt": "satisfies rule ordinary\n"})
	candidate, head := p.Candidate.ID, p.Candidate.Head
	for _, who := range []string{"E", "X"} {
		standingServeImport(g, who)
	}
	var table []string
	refused := func(label, who, output string, status int, want string, before string) {
		t.Helper()
		if status == 0 || !strings.Contains(output, want) {
			t.Fatalf("%s exited %d, want %q:\n%s", label, status, want, output)
		}
		if after := g.ServerRefs(); after != before {
			t.Fatalf("%s changed hosted refs:\nbefore\n%s\nafter\n%s", label, before, after)
		}
		if facts := standingServeChainFacts(g, who, candidate); len(facts) != 0 {
			t.Fatalf("%s recorded merge facts %+v", label, facts)
		}
		table = append(table, fmt.Sprintf("%-44s refused (%s)", label, want))
	}

	// Act and assert: authorization. X's preview is eligible.
	preview, status := standingServePreview(g, "X", candidate, "ordinary")
	requireStandingAutomergeEligible(t, preview, status)
	table = append(table, fmt.Sprintf("%-44s eligible (preview judges rule satisfaction only)", "X's preview"))

	executor, _ := standingServeRuleRefused(t, g, "X", candidate, head, "ordinary", serveReasonAdvanceNotRuleExecutor)
	requireServeIDs(t, executor, candidate)
	table = append(table, fmt.Sprintf("%-44s refused (%s)", "X's rule push", executor.Code))

	before, logged := g.ServerRefs(), len(g.pushLog())
	output, status := standingServeAutomerge(g, "X", candidate, "ordinary")
	refused("X's hn automerge --remote", "X", output, status,
		fmt.Sprintf("identity %s is not an executing actor for automerge rule %q", g.actors["X"], "ordinary"), before)
	if entries := g.pushLog(); len(entries) != logged {
		t.Fatalf("X's locally refused command pushed: %+v", entries[logged:])
	}

	// Transport: a stale hn-expected-tip.
	stale := g.Commit(g.Base, nil)
	tip := g.RejectMain("E", head, serveReasonProtectedStaleTip, "hn-advance="+candidate, "hn-expected-tip="+stale, "hn-rule=ordinary")
	requireServeIDs(t, tip, stale, g.Base)
	table = append(table, fmt.Sprintf("%-44s refused (%s)", "E's rule push with a stale expected tip", tip.Code))

	// A selection that omits trusted actors is refused before the import:
	// S's new fact on the server does not reach E's clone.
	g.hn("E", "replication", "select", "origin", "--actor", g.actors["E"], "--proposal", candidate)
	g.hn("S", "issue", "open", "Published after E's last import")
	g.hn("S", "sync", "origin")
	refs, before := g.git("E", "for-each-ref", "--format=%(objectname) %(refname)"), g.ServerRefs()
	output, status = standingServeAutomerge(g, "E", candidate, "ordinary")
	refused("E's command with a narrow selection", "E", output, status, "run hn replication select origin --all-trusted", before)
	if after := g.git("E", "for-each-ref", "--format=%(objectname) %(refname)"); after != refs {
		t.Fatalf("the refused command imported facts:\nbefore\n%s\nafter\n%s", refs, after)
	}
	standingServeSelectAllTrusted(g, "E")

	// A remote that is not an hn serve remote.
	plain := filepath.Join(t.TempDir(), "plain.git")
	runOperationalGit(t, "", "init", "-q", "--bare", plain)
	g.git("E", "remote", "add", "plain", plain)
	g.git("E", "push", "-q", "plain", "main")
	plainRefs, before := serveHarnessGit(t, plain, "for-each-ref", "--format=%(objectname) %(refname)"), g.ServerRefs()
	output, status, err := g.advanceExec("E", nil, g.h.Binary, "automerge", candidate, "--rule", "ordinary", "--execute", "--remote", "plain")
	if err != nil {
		t.Fatal(err)
	}
	refused("E's command against an ordinary remote", "E", output, status, "remote plain is not an hn serve remote", before)
	if got := serveHarnessGit(t, plain, "for-each-ref", "--format=%(objectname) %(refname)"); got != plainRefs {
		t.Fatalf("the ordinary remote changed:\nbefore\n%s\nafter\n%s", plainRefs, got)
	}

	// E's main behind the hosted tip.
	ahead := g.Commit(g.Base, map[string]string{"docs/ahead.txt": "hosted main moved\n"})
	g.SetMainForTest(ahead)
	before = g.ServerRefs()
	output, status = standingServeAutomerge(g, "E", candidate, "ordinary")
	refused("E's command with main behind the hosted tip", "E", output, status,
		fmt.Sprintf("local main is at %s but origin has %s", shortOID(g.Base), shortOID(ahead)), before)

	t.Logf("authorization and transport refusals, tested apart from agreement:\n%s", strings.Join(table, "\n"))
}

// T070: step 1 in process. Without a rule binding the checks of hn merge
// apply unchanged; with one, the named rule must be satisfied for the
// advanced branch and the identity must be one of its executing actors.
func TestStandingServeAdvanceValidateRuleBinding(t *testing.T) {
	// Arrange
	h := newStandingHarness(t, standingHarnessDefaultPolicy())
	candidate := h.ReadyCandidate(standingAutomergeFiles)
	for _, role := range []string{"E", "X"} {
		h.hn(role, "sync", "origin")
	}
	ordinary := serveAdvanceBinding{Rule: "ordinary"}

	h.inClone("E", func(*Identity) {
		// Act
		_, _, _, acceptance, err := serveAdvanceValidate(candidate.ID, serveGovernedMain, ordinary)

		// Assert
		if err != nil || acceptance == nil || acceptance.Rule == nil || acceptance.Rule.Name != "ordinary" || !acceptance.Satisfied() {
			t.Fatalf("step 1 under rule ordinary returned %+v, %v; want the satisfied rule", acceptance, err)
		}
		for _, tc := range []struct {
			name, branch string
			binding      serveAdvanceBinding
			want         string
		}{
			{"no rule binding", serveGovernedMain, serveAdvanceBinding{}, "proposal lacks 1 required acceptance decision(s)"},
			{"another branch", "refs/heads/release", ordinary,
				`automerge rule "ordinary" is not satisfied by the current evidence; run 'hn automerge ` + candidate.ID + ` --rule ordinary' for its blockers`},
			{"paths outside the rule", serveGovernedMain, serveAdvanceBinding{Rule: "other"}, `automerge rule "other" is not satisfied by the current evidence`},
			{"rule that does not exist", serveGovernedMain, serveAdvanceBinding{Rule: "nosuch"}, `automerge rule "nosuch" is not satisfied by the current evidence`},
		} {
			_, _, _, refused, err := serveAdvanceValidate(candidate.ID, tc.branch, tc.binding)
			if err == nil || !strings.Contains(err.Error(), tc.want) || refused != nil {
				t.Fatalf("%s: step 1 returned %+v, %v; want %q", tc.name, refused, err, tc.want)
			}
		}
	})

	h.inClone("X", func(*Identity) {
		// Act
		_, _, _, acceptance, err := serveAdvanceValidate(candidate.ID, serveGovernedMain, ordinary)

		// Assert
		want := fmt.Sprintf("identity %s is not an executing actor for automerge rule %q", h.Actor("X"), "ordinary")
		if err == nil || err.Error() != want || acceptance != nil {
			t.Fatalf("X's step 1 returned %+v, %v; want %q", acceptance, err, want)
		}
	})
}
