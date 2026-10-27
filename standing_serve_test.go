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
