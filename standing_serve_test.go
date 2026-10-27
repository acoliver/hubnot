package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
