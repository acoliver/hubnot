package main

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const (
	serveTestActor  = "4f1c9a6d0d2b7e3a8c5f6e1b9d0a7c3e2f4b6d8a0c1e3f5a7b9d1c3e5f7a9b1c"
	serveTestCommit = "9b2e4c6a8d0f1e3c5a7b9d1f3e5c7a9b2d4f6e8a"
	serveTestEvent  = "sha256:1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d"
)

// The worked example reject-direct-main from contracts/push-verdict.md,
// without the "remote: " prefix Git adds. WP09 compares real hook output
// against this shape with fixture IDs.
const serveGoldenRejectDirectMain = `hn-serve/1 reject ref=refs/heads/main actor=4f1c9a6d0d2b7e3a8c5f6e1b9d0a7c3e2f4b6d8a0c1e3f5a7b9d1c3e5f7a9b1c code=protected-no-proposal ids=9b2e4c6a8d0f1e3c5a7b9d1f3e5c7a9b2d4f6e8a missing=proposal,review,ci,acceptance next="open a proposal for 9b2e4c6a8d0f1e3c5a7b9d1f3e5c7a9b2d4f6e8a"
hn-serve: refs/heads/main is protected and 9b2e4c6a has no accepted, CI-tested proposal.
`

func TestServeReasonsCatalogueOrderAndLookups(t *testing.T) {
	// Arrange
	want := []string{
		"unknown-key", "unhosted-repository", "unsupported-command", "repository-busy",
		"actor-key-mismatch", "actor-chain-invalid", "actor-chain-rewrite", "dependency-missing",
		"protected-no-proposal", "protected-not-ready", "protected-destination-policy",
		"protected-not-fast-forward", "protected-stale-tip", "protected-blocked",
		"protected-policy-invalid", "protected-deletion", "advance-not-maintainer",
		"proposal-ref-mismatch", "proposal-ref-immutable", "memory-key-mismatch", "memory-invalid",
		"reserved-ref", "tag-immutable", "branch-not-owner", "over-budget", "lfs-quota",
		"lfs-object-mismatch",
	}

	// Act
	got := make([]string, 0, len(serveReasonCatalogue))
	for _, reason := range serveReasonCatalogue {
		got = append(got, reason.Code)
	}

	// Assert
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("catalogue codes = %v, want %v", got, want)
	}
	for _, reason := range serveReasonCatalogue {
		if !serveReasonKnown(reason.Code) {
			t.Errorf("serveReasonKnown(%q) = false", reason.Code)
		}
		if reason.Meaning == "" || reason.Next == "" {
			t.Errorf("catalogue entry %q has an empty meaning or next action", reason.Code)
		}
		if got := serveReasonDefaultNext(reason.Code); got != reason.Next {
			t.Errorf("serveReasonDefaultNext(%q) = %q, want %q", reason.Code, got, reason.Next)
		}
		if serveSanitizeNext(reason.Next) != reason.Next {
			t.Errorf("default next action for %q is not wire-safe: %q", reason.Code, reason.Next)
		}
	}
	for _, unknown := range []string{"", "Unknown-Key", "unknown_key", "over-budget ", "made-up"} {
		if serveReasonKnown(unknown) || serveReasonDefaultNext(unknown) != "" {
			t.Errorf("unknown code %q is treated as known", unknown)
		}
	}
	if got := serveReasonDefaultNext(serveReasonProtectedNoProposal); got != "open a proposal for <commit>" {
		t.Errorf("protected-no-proposal next template = %q", got)
	}
}

func TestServeReasonsEveryCodeRoundTrips(t *testing.T) {
	for _, reason := range serveReasonCatalogue {
		variants := map[string]serveRejection{
			"bare": {
				Ref: "refs/heads/main", Actor: serveTestActor, Code: reason.Code,
				Next: reason.Next, Sentence: "plain sentence for " + reason.Code,
			},
			"session refusal": {
				Ref: "-", Actor: "-", Code: reason.Code, Next: reason.Next, Sentence: reason.Meaning,
			},
			"ids and missing": {
				Ref: "refs/hn/proposals/" + strings.TrimPrefix(serveTestEvent, "sha256:"), Actor: serveTestActor,
				Code: reason.Code, IDs: []string{serveTestEvent, serveTestCommit},
				Missing: []string{"proposal", "review", "ci", "acceptance"},
				Next:    "open a proposal for " + serveTestCommit, Sentence: "two IDs and every kind",
			},
			"one missing kind": {
				Ref: "refs/heads/main", Actor: serveTestActor, Code: reason.Code,
				IDs: []string{serveTestCommit}, Missing: []string{"ci"}, Next: "", Sentence: "s",
			},
		}
		if serveReasonCarriesBudget(reason.Code) {
			variants["budget"] = serveRejection{
				Ref: "refs/hn/actors/" + serveTestActor, Actor: serveTestActor, Code: reason.Code,
				IDs: []string{serveTestEvent}, Budget: "max-events", Limit: 10, Measured: 11,
				Next: reason.Next, Sentence: "one over the limit",
			}
			variants["budget zero values"] = serveRejection{
				Ref: "-", Actor: serveTestActor, Code: reason.Code,
				Budget: "lfs-quota", Limit: 0, Measured: 0, Next: reason.Next, Sentence: "zero",
			}
		}
		for name, rejection := range variants {
			t.Run(reason.Code+"/"+name, func(t *testing.T) {
				// Act
				rendered := renderServeRejection(rejection)
				lines := strings.SplitAfter(rendered, "\n")

				// Assert
				if len(lines) != 3 || lines[2] != "" {
					t.Fatalf("rendered %q, want exactly two newline-terminated lines", rendered)
				}
				parsed, err := parseServeRejectionLine(strings.TrimSuffix(lines[0], "\n"))
				if err != nil {
					t.Fatalf("parse %q: %v", lines[0], err)
				}
				withoutSentence := rejection
				withoutSentence.Sentence = ""
				if !reflect.DeepEqual(parsed, withoutSentence) {
					t.Fatalf("parsed = %#v, want %#v", parsed, withoutSentence)
				}
				extracted, cleaned := extractServeRejections(rendered)
				if !reflect.DeepEqual(extracted, []serveRejection{rejection}) {
					t.Fatalf("extracted = %#v, want %#v", extracted, rejection)
				}
				if strings.Join(cleaned, "\n")+"\n" != rendered {
					t.Fatalf("cleaned lines = %q, want %q", cleaned, rendered)
				}
			})
		}
	}
}

func TestServeReasonsGoldenRejectDirectMain(t *testing.T) {
	// Arrange
	rejection := serveRejection{
		Ref:      "refs/heads/main",
		Actor:    serveTestActor,
		Code:     serveReasonProtectedNoProposal,
		IDs:      []string{serveTestCommit},
		Missing:  []string{"acceptance", "ci", "review", "proposal"},
		Next:     strings.Replace(serveReasonDefaultNext(serveReasonProtectedNoProposal), "<commit>", serveTestCommit, 1),
		Sentence: "refs/heads/main is protected and " + serveTestCommit[:8] + " has no accepted, CI-tested proposal.",
	}

	// Act
	got := renderServeRejection(rejection)

	// Assert
	if got != serveGoldenRejectDirectMain {
		t.Fatalf("rendered:\n%s\nwant:\n%s", got, serveGoldenRejectDirectMain)
	}
}

func TestServeReasonsGoldenMatchesContractExample(t *testing.T) {
	// The planning contract is the source of the golden text. It lives outside
	// the package, so the check runs only where the mission artifacts exist.
	contract, err := os.ReadFile(filepath.Join("kitty-specs", "hn-serve-01M5P0NP", "contracts", "push-verdict.md"))
	if os.IsNotExist(err) {
		t.Skip("mission contract is not present in this checkout")
	}
	if err != nil {
		t.Fatal(err)
	}
	_, example, found := strings.Cut(string(contract), "<!-- example: reject-direct-main -->")
	if !found {
		t.Fatal("contract has no reject-direct-main example")
	}
	_, block, _ := strings.Cut(example, "```text\n")
	block, _, _ = strings.Cut(block, "```")
	want := strings.ReplaceAll(block, "remote: ", "")
	if want != serveGoldenRejectDirectMain {
		t.Fatalf("contract example:\n%s\ngolden:\n%s", want, serveGoldenRejectDirectMain)
	}
}

func TestServeReasonsSanitization(t *testing.T) {
	// Arrange
	rejection := serveRejection{
		Ref:      "refs/heads/a b,c=d\x1b[31m\tx\u00e9",
		Actor:    "act\nor",
		Code:     serveReasonBranchNotOwner,
		IDs:      []string{"id one", "id,two", "", "id=three"},
		Next:     "say \"hi\" \\ then\r\nleave \u2014 now",
		Sentence: "line one\nline two \u00e9\x00",
	}

	// Act
	rendered := renderServeRejection(rejection)
	extracted, _ := extractServeRejections(rendered)

	// Assert
	want := `hn-serve/1 reject ref=refs/heads/a?b?c?d?[31m?x?? actor=act?or code=branch-not-owner ids=id?one,id?two,?,id?three next="say ?hi? ? then??leave ??? now"` + "\n" +
		"hn-serve: line one?line two ???\n"
	if rendered != want {
		t.Fatalf("rendered:\n%q\nwant:\n%q", rendered, want)
	}
	if len(extracted) != 1 {
		t.Fatalf("sanitized output did not parse back: %q", rendered)
	}
	if extracted[0].Ref != "refs/heads/a?b?c?d?[31m?x??" || extracted[0].Next != "say ?hi? ? then??leave ??? now" {
		t.Fatalf("extracted = %#v", extracted[0])
	}
	for index := 0; index < len(rendered); index++ {
		character := rendered[index]
		if character != '\n' && (character < 0x20 || character >= 0x7f) {
			t.Fatalf("rendered output contains byte %#x", character)
		}
	}
}

func TestServeReasonsRenderDefaults(t *testing.T) {
	// Arrange
	rejection := serveRejection{Code: serveReasonUnknownKey, Sentence: "   "}

	// Act
	got := renderServeRejection(rejection)

	// Assert
	want := `hn-serve/1 reject ref=- actor=- code=unknown-key next=""` + "\n" +
		"hn-serve: The authenticating key is not in the registry\n"
	if got != want {
		t.Fatalf("rendered %q, want %q", got, want)
	}
	parsed, err := parseServeRejectionLine(strings.SplitN(got, "\n", 2)[0])
	if err != nil || parsed.Ref != "-" || parsed.Actor != "-" || parsed.Next != "" {
		t.Fatalf("parsed = %#v, %v", parsed, err)
	}
	if got := renderServeRejection(serveRejection{Code: serveReasonReservedRef, Sentence: "trailing   "}); !strings.HasSuffix(got, "hn-serve: trailing\n") {
		t.Fatalf("trailing sentence spaces kept: %q", got)
	}
}

func TestServeReasonsRenderRefusesProgrammingErrors(t *testing.T) {
	cases := map[string]serveRejection{
		"unknown code":           {Ref: "refs/heads/main", Actor: serveTestActor, Code: "protected-typo"},
		"empty code":             {Ref: "refs/heads/main", Actor: serveTestActor},
		"unknown missing kind":   {Code: serveReasonProtectedNotReady, Missing: []string{"review", "approval"}},
		"budget on other code":   {Code: serveReasonProtectedNotReady, Budget: "max-events", Limit: 1, Measured: 2},
		"limit on other code":    {Code: serveReasonReservedRef, Limit: 1},
		"limit without budget":   {Code: serveReasonOverBudget, Limit: 5, Measured: 6},
		"measured without name":  {Code: serveReasonLFSQuota, Measured: 6},
		"negative limit":         {Code: serveReasonOverBudget, Budget: "max-events", Limit: -1, Measured: 2},
		"negative measured":      {Code: serveReasonLFSQuota, Budget: "lfs-quota", Limit: 1, Measured: -2},
		"budget name on mistype": {Code: serveReasonLFSObjectMismatch, Budget: "lfs-quota"},
	}
	for name, rejection := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("renderServeRejection(%#v) did not panic", rejection)
				}
			}()
			_ = renderServeRejection(rejection)
		})
	}
	t.Run("list with one invalid entry", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("renderServeRejections did not panic on an unknown code")
			}
		}()
		_ = renderServeRejections([]serveRejection{{Code: serveReasonReservedRef}, {Code: "nope"}})
	})
}

func TestServeReasonsMissingKindsUseFixedOrder(t *testing.T) {
	// Arrange
	rejection := serveRejection{
		Ref: "refs/heads/main", Actor: serveTestActor, Code: serveReasonProtectedDestinationPolicy,
		Missing: []string{"acceptance", "review", "acceptance", "review"},
	}

	// Act
	line := strings.SplitN(renderServeRejection(rejection), "\n", 2)[0]

	// Assert
	if !strings.Contains(line, " missing=review,acceptance next=") {
		t.Fatalf("line %q does not carry missing=review,acceptance", line)
	}
	if ordered, err := serveOrderedMissing(nil); err != nil || ordered != nil {
		t.Fatalf("serveOrderedMissing(nil) = %#v, %v", ordered, err)
	}
}

func TestServeReasonsRenderListSortsByRef(t *testing.T) {
	// Arrange
	list := []serveRejection{
		{Ref: "refs/tags/v1", Actor: serveTestActor, Code: serveReasonTagImmutable, Next: "first tag"},
		{Ref: "refs/heads/main", Actor: serveTestActor, Code: serveReasonProtectedNoProposal, Next: "main"},
		{Ref: "refs/heads/Main", Actor: serveTestActor, Code: serveReasonBranchNotOwner, Next: "capital"},
		{Ref: "refs/tags/v1", Actor: serveTestActor, Code: serveReasonReservedRef, Next: "second tag"},
		{Ref: "-", Actor: "-", Code: serveReasonRepositoryBusy, Next: "session"},
	}
	original := append([]serveRejection(nil), list...)

	// Act
	rendered := renderServeRejections(list)
	extracted, _ := extractServeRejections(rendered)

	// Assert
	gotNext := make([]string, 0, len(extracted))
	for _, rejection := range extracted {
		gotNext = append(gotNext, rejection.Next)
	}
	wantNext := []string{"session", "capital", "main", "first tag", "second tag"}
	if !reflect.DeepEqual(gotNext, wantNext) {
		t.Fatalf("order = %v, want %v", gotNext, wantNext)
	}
	if !reflect.DeepEqual(list, original) {
		t.Fatal("renderServeRejections reordered the caller's slice")
	}
	if renderServeRejections(nil) != "" {
		t.Fatal("rendering no rejections produced output")
	}
}

func TestServeReasonsParserRejectsMalformedLines(t *testing.T) {
	valid := `hn-serve/1 reject ref=refs/heads/main actor=` + serveTestActor + ` code=over-budget ids=` + serveTestEvent + ` missing=proposal budget=max-events limit=10 measured=11 next="split the history"`
	if _, err := parseServeRejectionLine(valid); err != nil {
		t.Fatalf("valid line rejected: %v", err)
	}
	base := `hn-serve/1 reject ref=refs/heads/main actor=` + serveTestActor
	cases := map[string]string{
		"wrong version":            strings.Replace(valid, "hn-serve/1 ", "hn-serve/2 ", 1),
		"no version":               strings.TrimPrefix(valid, "hn-serve/1 "),
		"remote prefix kept":       "remote: " + valid,
		"wrong verb":               strings.Replace(valid, " reject ", " accept ", 1),
		"unknown code":             base + ` code=over-budgets next="x"`,
		"unknown field":            base + ` code=reserved-ref extra=1 next="x"`,
		"out of order ids":         base + ` code=protected-not-ready missing=review ids=` + serveTestCommit + ` next="x"`,
		"code before actor":        `hn-serve/1 reject ref=refs/heads/main code=reserved-ref actor=` + serveTestActor + ` next="x"`,
		"missing actor":            `hn-serve/1 reject ref=refs/heads/main code=reserved-ref next="x"`,
		"missing ref":              `hn-serve/1 reject actor=- code=reserved-ref next="x"`,
		"duplicate ref":            `hn-serve/1 reject ref=a ref=b actor=- code=reserved-ref next="x"`,
		"duplicate ids":            base + ` code=reserved-ref ids=a ids=b next="x"`,
		"duplicate next":           base + ` code=reserved-ref next="x" next="y"`,
		"budget on non-budget":     base + ` code=reserved-ref budget=max-events limit=1 measured=2 next="x"`,
		"budget without limit":     base + ` code=over-budget budget=max-events measured=2 next="x"`,
		"limit without budget":     base + ` code=over-budget limit=1 measured=2 next="x"`,
		"non-decimal limit":        base + ` code=over-budget budget=max-events limit=0x10 measured=2 next="x"`,
		"negative measured":        base + ` code=over-budget budget=max-events limit=1 measured=-2 next="x"`,
		"leading zero limit":       base + ` code=over-budget budget=max-events limit=01 measured=2 next="x"`,
		"overflow limit":           base + ` code=over-budget budget=max-events limit=9223372036854775808 measured=2 next="x"`,
		"unknown evidence kind":    base + ` code=protected-not-ready missing=review,approval next="x"`,
		"evidence out of order":    base + ` code=protected-not-ready missing=ci,review next="x"`,
		"evidence duplicated":      base + ` code=protected-not-ready missing=ci,ci next="x"`,
		"empty id":                 base + ` code=reserved-ref ids=a,,b next="x"`,
		"empty value":              base + ` code=reserved-ref ids= next="x"`,
		"no next":                  base + ` code=reserved-ref`,
		"no next with space":       base + ` code=reserved-ref `,
		"unquoted next":            base + ` code=reserved-ref next=x`,
		"unterminated quote":       base + ` code=reserved-ref next="x`,
		"trailing text":            base + ` code=reserved-ref next="x" extra`,
		"quote inside next":        base + ` code=reserved-ref next="a"b"`,
		"backslash inside next":    base + ` code=reserved-ref next="a\b"`,
		"double space":             base + `  code=reserved-ref next="x"`,
		"field without equals":     base + ` code=reserved-ref ids next="x"`,
		"comma in ref":             `hn-serve/1 reject ref=a,b actor=- code=reserved-ref next="x"`,
		"equals in value":          `hn-serve/1 reject ref=a=b actor=- code=reserved-ref next="x"`,
		"control byte":             base + " code=reserved-ref next=\"a\tb\"",
		"non-ascii byte":           base + " code=reserved-ref next=\"caf\u00e9\"",
		"trailing carriage return": valid + "\r",
	}
	for name, line := range cases {
		t.Run(name, func(t *testing.T) {
			if parsed, err := parseServeRejectionLine(line); err == nil {
				t.Fatalf("parseServeRejectionLine(%q) = %#v, want an error", line, parsed)
			}
		})
	}
}

func TestServeReasonsExtractFromGitPushStderr(t *testing.T) {
	// Arrange: the shape git push prints when pre-receive rejects two refs,
	// with Git's 8-space padding, CRLF, progress carriage returns and an ANSI
	// clear-to-end-of-line suffix.
	main := serveRejection{
		Ref: "refs/heads/main", Actor: serveTestActor, Code: serveReasonProtectedNoProposal,
		IDs: []string{serveTestCommit}, Missing: []string{"proposal", "review", "ci", "acceptance"},
		Next: "open a proposal for " + serveTestCommit, Sentence: "refs/heads/main is protected.",
	}
	tag := serveRejection{
		Ref: "refs/tags/v1", Actor: serveTestActor, Code: serveReasonTagImmutable,
		Next: "create a new tag", Sentence: "refs/tags/v1 already exists.",
	}
	rendered := renderServeRejections([]serveRejection{tag, main})
	renderedLines := strings.Split(strings.TrimSuffix(rendered, "\n"), "\n")
	stderr := "Enumerating objects: 5, done.\n" +
		"Writing objects:  50% (1/2)\rWriting objects: 100% (2/2), done.\n" +
		"remote: " + renderedLines[0] + "        \r\n" +
		"remote: " + renderedLines[1] + "        \n" +
		"remote: hn-serve/1 reject ref=x actor=- code=not-a-code next=\"x\"        \n" +
		"remote: hn-serve: " + "orphan sentence after a malformed line\n" +
		"remote: " + renderedLines[2] + "\x1b[K\n" +
		"remote: " + renderedLines[3] + "        \n" +
		"To ssh://hn@example/repo.git\n" +
		" ! [remote rejected] main -> main (pre-receive hook declined)\n" +
		" ! [remote rejected] v1 -> v1 (pre-receive hook declined)\n" +
		"error: failed to push some refs to 'ssh://hn@example/repo.git'\n" +
		"remote: hn-serve: unpaired trailing sentence\n"

	// Act
	rejections, lines := extractServeRejections(stderr)

	// Assert
	if !reflect.DeepEqual(rejections, []serveRejection{main, tag}) {
		t.Fatalf("rejections = %#v", rejections)
	}
	wantLines := []string{
		renderedLines[0], renderedLines[1],
		`hn-serve/1 reject ref=x actor=- code=not-a-code next="x"`,
		"hn-serve: orphan sentence after a malformed line",
		renderedLines[2],
		renderedLines[3],
		"hn-serve: unpaired trailing sentence",
	}
	if !reflect.DeepEqual(lines, wantLines) {
		t.Fatalf("lines =\n%q\nwant\n%q", lines, wantLines)
	}
	if got, cleaned := extractServeRejections("To x\n ! [rejected] main -> main (fetch first)\n"); got != nil || cleaned != nil {
		t.Fatalf("plain Git rejection extracted %#v %#v", got, cleaned)
	}
}

func TestServeReasonsExtractWithoutSentence(t *testing.T) {
	// Arrange
	line := `hn-serve/1 reject ref=refs/heads/main actor=- code=unknown-key next="x"`

	// Act
	rejections, lines := extractServeRejections("remote: " + line + "\nremote: hn-serve/1 reject ref=refs/heads/b actor=- code=reserved-ref next=\"y\"\nremote: hn-serve: second\n")

	// Assert
	if len(rejections) != 2 || rejections[0].Sentence != "" || rejections[1].Sentence != "second" {
		t.Fatalf("rejections = %#v", rejections)
	}
	if len(lines) != 3 || lines[0] != line {
		t.Fatalf("lines = %q", lines)
	}
}

// serveReasonDocsTable reads the reason-code table of the "hn serve wire
// surface" section. Backticks are dropped so the docs may format commands.
func serveReasonDocsTable(markdown string) ([]serveReason, error) {
	_, section, found := strings.Cut(markdown, "\n## hn serve wire surface\n")
	if !found {
		return nil, fmt.Errorf("docs have no hn serve wire surface section")
	}
	section, _, _ = strings.Cut(section, "\n## ")
	_, table, found := strings.Cut(section, "\n| Code | Meaning | Typical next action |\n|----|----|----|\n")
	if !found {
		return nil, fmt.Errorf("hn serve wire surface section has no reason-code table")
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

func compareServeReasonTable(documented, catalogue []serveReason) error {
	codes := func(reasons []serveReason) []string {
		list := make([]string, 0, len(reasons))
		for _, reason := range reasons {
			list = append(list, reason.Code)
		}
		return list
	}
	if !reflect.DeepEqual(codes(documented), codes(catalogue)) {
		return fmt.Errorf("documented codes %v differ from catalogue codes %v", codes(documented), codes(catalogue))
	}
	for index := range catalogue {
		if documented[index] != catalogue[index] {
			return fmt.Errorf("documented %#v differs from catalogue %#v", documented[index], catalogue[index])
		}
	}
	return nil
}

func TestServeReasonsDocsTableMatchesCatalogue(t *testing.T) {
	// Arrange
	markdown, err := os.ReadFile(filepath.Join("docs", "protocol-v0.md"))
	if err != nil {
		t.Fatal(err)
	}

	// Act
	documented, err := serveReasonDocsTable(string(markdown))
	if err != nil {
		t.Fatal(err)
	}

	// Assert
	if err := compareServeReasonTable(documented, serveReasonCatalogue); err != nil {
		t.Fatal(err)
	}
	extended := append(append([]serveReason(nil), serveReasonCatalogue...), serveReason{"new-code", "meaning", "next"})
	if compareServeReasonTable(documented, extended) == nil {
		t.Fatal("drift check accepted a catalogue code missing from the docs")
	}
	changed := append([]serveReason(nil), serveReasonCatalogue...)
	changed[0].Next = "something else"
	if compareServeReasonTable(documented, changed) == nil {
		t.Fatal("drift check accepted a changed next action")
	}
	known := strings.Index(string(markdown), "\n## hn serve wire surface\n")
	limits := strings.Index(string(markdown), "\n## Known limits\n")
	if known < 0 || limits < 0 || known > limits {
		t.Fatal("hn serve wire surface section must come before Known limits")
	}
}

func TestServeWireMarkerConstants(t *testing.T) {
	if len(serveMarkerContent) != 11 || serveMarkerContent != "hn-serve/1\n" {
		t.Fatalf("marker content = %q", serveMarkerContent)
	}
	if serveMarkerRef != "refs/hn/serve/v1" || servePushOptionAdvance != "hn-advance" || servePushOptionExpected != "hn-expected-tip" {
		t.Fatal("wire constants changed")
	}
	if !strings.HasPrefix(serveMarkerContent, strings.TrimSuffix(serveRejectionPrefix, " reject ")) {
		t.Fatal("marker and rejection lines use different version prefixes")
	}
}

func TestServeWireMarkerObjectIDUsesRepositoryFormat(t *testing.T) {
	header := fmt.Sprintf("blob %d\x00", len(serveMarkerContent))
	sha1Sum := sha1.Sum([]byte(header + serveMarkerContent))
	sha256Sum := sha256.Sum256([]byte(header + serveMarkerContent))
	for format, want := range map[string]string{
		"sha1":   hex.EncodeToString(sha1Sum[:]),
		"sha256": hex.EncodeToString(sha256Sum[:]),
	} {
		t.Run(format, func(t *testing.T) {
			// Arrange
			gitDir := filepath.Join(t.TempDir(), "repo.git")
			mustGit(t, "init", "-q", "--bare", "--object-format="+format, gitDir)

			// Act
			got, err := serveMarkerObjectID(gitDir)

			// Assert
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("marker object ID = %s, want %s", got, want)
			}
			if _, err := gitOutputAt(gitDir, "cat-file", "-e", got); err == nil {
				t.Fatal("serveMarkerObjectID wrote the marker object")
			}
		})
	}
}

// serveWireRemote creates a bare repository and, when content is non-empty,
// points refs/hn/serve/v1 at a blob holding it.
func serveWireRemote(t *testing.T, root, name, format, content string) string {
	t.Helper()
	path := filepath.Join(root, name)
	mustGit(t, "init", "-q", "--bare", "--object-format="+format, path)
	if content == "" {
		return path
	}
	output, err := gitInputAt(path, []byte(content), "hash-object", "-w", "--stdin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gitOutputAt(path, "update-ref", serveMarkerRef, strings.TrimSpace(string(output))); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestServeWireRemoteIdentification(t *testing.T) {
	for _, format := range []string{"sha1", "sha256"} {
		t.Run(format, func(t *testing.T) {
			// Arrange
			root := t.TempDir()
			marked := serveWireRemote(t, root, "marked.git", format, serveMarkerContent)
			different := serveWireRemote(t, root, "different.git", format, "hn-serve/2\n")
			unmarked := serveWireRemote(t, root, "unmarked.git", format, "")
			extra := serveWireRemote(t, root, "extra.git", format, serveMarkerContent)
			markerOID := mustGitTextAtDir(t, extra, "rev-parse", serveMarkerRef)
			if _, err := gitOutputAt(extra, "update-ref", "refs/other/"+serveMarkerRef, markerOID); err != nil {
				t.Fatal(err)
			}
			tree := serveWireRemote(t, root, "tree.git", format, "")
			emptyTree := mustGitTextAtDir(t, tree, "hash-object", "-w", "-t", "tree", "/dev/null")
			if _, err := gitOutputAt(tree, "update-ref", serveMarkerRef, emptyTree); err != nil {
				t.Fatal(err)
			}
			client := filepath.Join(root, "client")
			mustGit(t, "init", "-q", "--object-format="+format, client)
			withTestDirectory(t, client)
			mustGit(t, "remote", "add", "marked", marked)
			mustGit(t, "remote", "add", "different", different)
			mustGit(t, "remote", "add", "unmarked", unmarked)

			cases := []struct {
				remote string
				want   bool
			}{
				{"marked", true},
				{marked, true},
				{"file://" + marked, true},
				{"different", false},
				{different, false},
				{"unmarked", false},
				{unmarked, false},
				{extra, false},
				{tree, false},
				{filepath.Join(root, "missing.git"), false},
				{"no-such-remote", false},
				{"--upload-pack=touch " + filepath.Join(root, "pwned"), false},
				{"", false},
			}
			for _, tc := range cases {
				// Act
				got, diagnostic := serveRemoteIdentifies(tc.remote)

				// Assert
				if got != tc.want {
					t.Errorf("serveRemoteIdentifies(%q) = %v (%s), want %v", tc.remote, got, diagnostic, tc.want)
				}
				if got && diagnostic != "" {
					t.Errorf("identified remote %q carries diagnostic %q", tc.remote, diagnostic)
				}
				if !got && diagnostic == "" {
					t.Errorf("unidentified remote %q has no diagnostic", tc.remote)
				}
			}
			if _, err := os.Stat(filepath.Join(root, "pwned")); err == nil {
				t.Fatal("a remote argument was interpreted as a git option")
			}
		})
	}
}

func TestServeWireRemoteIdentificationRequiresLocalObjectFormat(t *testing.T) {
	// Arrange: a remote with the correct SHA-1 marker, read from a SHA-256
	// client, must not identify because the client computes a SHA-256 ID.
	root := t.TempDir()
	marked := serveWireRemote(t, root, "marked.git", "sha1", serveMarkerContent)
	client := filepath.Join(root, "client")
	mustGit(t, "init", "-q", "--object-format=sha256", client)
	withTestDirectory(t, client)

	// Act
	got, diagnostic := serveRemoteIdentifies(marked)

	// Assert
	if got || diagnostic == "" {
		t.Fatalf("SHA-256 client identified a SHA-1 marker: %v %q", got, diagnostic)
	}
}

func TestServeWireRemoteIdentificationOutsideRepository(t *testing.T) {
	// Arrange
	root := t.TempDir()
	marked := serveWireRemote(t, root, "marked.git", "sha1", serveMarkerContent)
	outside := filepath.Join(root, "outside")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	withTestDirectory(t, outside)
	t.Setenv("GIT_CEILING_DIRECTORIES", root)

	// Act
	got, diagnostic := serveRemoteIdentifies(marked)

	// Assert
	if got || diagnostic == "" {
		t.Fatalf("identification outside a repository = %v %q", got, diagnostic)
	}
}

func TestServeReasonsRenderListSortsByOriginalRefBytes(t *testing.T) {
	// Arrange: ',' (0x2c) sorts before '0' (0x30), but its replacement '?'
	// (0x3f) sorts after it, so sorting rendered refs would invert the order.
	list := []serveRejection{
		{Ref: "refs/heads/a0", Code: serveReasonReservedRef, Next: "digit"},
		{Ref: "refs/heads/a,b", Code: serveReasonReservedRef, Next: "comma"},
		{Ref: "", Code: serveReasonRepositoryBusy, Next: "empty"},
		{Ref: "refs/heads/a b", Code: serveReasonReservedRef, Next: "space"},
	}

	// Act
	extracted, _ := extractServeRejections(renderServeRejections(list))

	// Assert
	gotNext := make([]string, 0, len(extracted))
	for _, rejection := range extracted {
		gotNext = append(gotNext, rejection.Next)
	}
	wantNext := []string{"empty", "space", "comma", "digit"}
	if !reflect.DeepEqual(gotNext, wantNext) {
		t.Fatalf("order = %v, want %v", gotNext, wantNext)
	}
}

func TestServeReasonsDocsNameEveryNextReplacement(t *testing.T) {
	// Arrange
	markdown, err := os.ReadFile(filepath.Join("docs", "protocol-v0.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, section, found := strings.Cut(string(markdown), "\n## hn serve wire surface\n")
	if !found {
		t.Fatal("docs have no hn serve wire surface section")
	}
	section, _, _ = strings.Cut(section, "\n## ")
	paragraph := strings.Join(strings.Fields(section), " ")

	// Act and Assert: each byte serveSanitizeNext replaces beyond the common
	// rule must be named, and an empty code span means a character was lost.
	for _, character := range []byte{'"', '\\'} {
		if serveSanitizeNext(string(character)) != "?" {
			t.Fatalf("serveSanitizeNext keeps %q", character)
		}
		named := fmt.Sprintf("(byte `0x%02x`)", character)
		if !strings.Contains(paragraph, named) {
			t.Errorf("docs do not name the next replacement %q as %s", character, named)
		}
	}
	for _, line := range strings.Split(section, "\n") {
		if !strings.HasPrefix(line, "```") && strings.Contains(line, "``") {
			t.Errorf("hn serve wire surface line has an empty code span: %q", line)
		}
	}
}

// FuzzServeReasonsParsedLineRendersIdentically checks the property hn sync
// relies on when it re-renders rejections from a server: a line the parser
// accepts renders back to the same line and never panics the renderer.
func FuzzServeReasonsParsedLineRendersIdentically(f *testing.F) {
	f.Add(strings.SplitN(serveGoldenRejectDirectMain, "\n", 2)[0])
	f.Add(`hn-serve/1 reject ref=- actor=- code=lfs-quota budget=lfs-quota limit=0 measured=0 next=""`)
	f.Add(`hn-serve/1 reject ref=a"b actor=? code=reserved-ref ids=? next="x"`)
	f.Add(`hn-serve/1 reject ref=r actor=a code=over-budget missing=review,ci budget=b limit=1 measured=2 next="n"`)
	for _, reason := range serveReasonCatalogue {
		rendered := renderServeRejection(serveRejection{Ref: "refs/heads/main", Actor: serveTestActor, Code: reason.Code, Next: reason.Next})
		f.Add(strings.SplitN(rendered, "\n", 2)[0])
	}
	f.Fuzz(func(t *testing.T, line string) {
		parsed, err := parseServeRejectionLine(line)
		if err != nil {
			return
		}
		first, _, _ := strings.Cut(renderServeRejection(parsed), "\n")
		if first != line {
			t.Fatalf("parsed line %q renders as %q", line, first)
		}
	})
}

// serveWireRealGit returns the git executable before a test shadows it.
func serveWireRealGit(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestServeWireRemoteIdentificationRequiresExactAdvertisement(t *testing.T) {
	// Arrange: a git shim answers ls-remote with fixed bytes and status, so
	// each case controls the exact advertisement the client reads.
	root := t.TempDir()
	realGit := serveWireRealGit(t)
	client := filepath.Join(root, "client")
	mustGit(t, "init", "-q", client)
	withTestDirectory(t, client)
	oid, err := serveMarkerObjectID("")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	shim := "#!/bin/sh\nif [ \"$1\" = ls-remote ]; then\n  printf '%s' \"$HN_TEST_ADVERTISEMENT\"\n  exit \"$HN_TEST_STATUS\"\nfi\nexec '" + realGit + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(shim), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	exact := oid + "\t" + serveMarkerRef + "\n"
	other := strings.Repeat("0", len(oid))
	cases := []struct {
		name, output, status string
		want                 bool
	}{
		{"exact", exact, "0", true},
		{"extra blank line", exact + "\n", "0", false},
		{"leading blank line", "\n" + exact, "0", false},
		{"leading space before oid", " " + exact, "0", false},
		{"trailing space after ref", oid + "\t" + serveMarkerRef + " \n", "0", false},
		{"crlf", oid + "\t" + serveMarkerRef + "\r\n", "0", false},
		{"no final newline", oid + "\t" + serveMarkerRef, "0", false},
		{"space instead of tab", oid + " " + serveMarkerRef + "\n", "0", false},
		{"duplicate line", exact + exact, "0", false},
		{"different oid", other + "\t" + serveMarkerRef + "\n", "0", false},
		{"uppercase oid", strings.ToUpper(oid) + "\t" + serveMarkerRef + "\n", "0", false},
		{"empty", "", "0", false},
		{"valid stdout but transport error", exact, "1", false},
		{"valid stdout but unreachable remote", exact, "128", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HN_TEST_ADVERTISEMENT", tc.output)
			t.Setenv("HN_TEST_STATUS", tc.status)

			// Act
			got, diagnostic := serveRemoteIdentifies("shimmed-remote")

			// Assert
			if got != tc.want {
				t.Fatalf("identification = %v (%q), want %v for stdout %q", got, diagnostic, tc.want, tc.output)
			}
			if got == (diagnostic != "") {
				t.Fatalf("identification %v has diagnostic %q", got, diagnostic)
			}
		})
	}
}

// serveWireSyncFixture builds a client repository with one local actor
// event and a bare origin whose pre-receive hook rejects every update with
// the given script body. The marker is written only when marked is true.
func serveWireSyncFixture(t *testing.T, hook string, marked bool) (string, *Identity) {
	t.Helper()
	root := inReplicationTestRepository(t)
	remote := filepath.Join(root, "origin.git")
	mustGit(t, "init", "-q", "--bare", remote)
	mustGit(t, "remote", "add", "origin", remote)
	if marked {
		oid, err := gitInputAt(remote, []byte(serveMarkerContent), "hash-object", "-w", "--stdin")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := gitOutputAt(remote, "update-ref", serveMarkerRef, strings.TrimSpace(string(oid))); err != nil {
			t.Fatal(err)
		}
	}
	script := "#!/bin/sh\n" + hook + "\nexit 1\n"
	if err := os.WriteFile(filepath.Join(remote, "hooks", "pre-receive"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	actor := testIdentity(t, "Serve Publisher")
	appendPublicationIssue(t, actor, "publication rejected by hn serve")
	return remote, actor
}

// serveWireRejectingHook prints one hn-serve/1 line pair per pushed ref. The
// sentence carries an ANSI color sequence, an OSC title sequence and UTF-8
// that the client must not pass through.
const serveWireRejectingHook = `while read old new ref; do
  printf 'hn-serve/1 reject ref=%s actor=- code=actor-chain-rewrite ids=%s next="append new events instead"\n' "$ref" "$new" >&2
  printf 'hn-serve: \033[31mrewrite\033[0m of %s \033]0;owned\007caf\303\251\n' "$ref" >&2
done`

func TestServeWireSyncReportsServeRejections(t *testing.T) {
	// Arrange
	remote, actor := serveWireSyncFixture(t, serveWireRejectingHook, true)
	ref := actorRef(actor.Actor)
	head := mustGitText(t, "rev-parse", ref)
	plain := "publication phase failed after replication: replication actor publication failed for remote origin"

	// Act: the public command dispatch, as main runs it.
	err := run([]string{"sync", "origin"})

	// Assert
	if err == nil {
		t.Fatal("sync succeeded against a rejecting hn serve remote")
	}
	want := plain + "\n" +
		"hn-serve/1 reject ref=" + ref + " actor=- code=actor-chain-rewrite ids=" + head + ` next="append new events instead"` + "\n" +
		"hn-serve: ?[31mrewrite?[0m of " + ref + " ?]0;owned?caf??"
	if err.Error() != want {
		t.Fatalf("sync error:\n%q\nwant:\n%q", err.Error(), want)
	}
	var rejected *serveRejectedPushError
	if !errors.As(err, &rejected) || len(rejected.Rejections) != 1 || rejected.Rejections[0].Code != serveReasonActorChainRewrite {
		t.Fatalf("sync error does not carry the parsed rejection: %#v", err)
	}
	if errors.Unwrap(rejected) == nil || errors.Unwrap(rejected).Error() != "replication actor publication failed for remote origin" {
		t.Fatalf("rejection error lost the phase error: %v", errors.Unwrap(rejected))
	}
	if strings.ContainsAny(err.Error(), "\x1b\x07\r") {
		t.Fatalf("sync error passed control bytes through: %q", err.Error())
	}
	if _, err := gitOutputAt(remote, "rev-parse", "--verify", "--quiet", ref); err == nil {
		t.Fatal("rejected actor ref reached the remote")
	}
}

func TestServeWireSyncKeepsPlainErrorWithoutServeRejections(t *testing.T) {
	plain := "publication phase failed after replication: replication actor publication failed for remote origin"
	cases := []struct {
		name   string
		hook   string
		marked bool
	}{
		// An ordinary remote whose hook imitates hn serve is not identified.
		{"unidentified remote printing rejection lines", serveWireRejectingHook, false},
		{"identified remote without rejection lines", "cat >/dev/null\necho 'push refused by policy' >&2", true},
		{"identified remote with an unparseable line", "cat >/dev/null\necho 'hn-serve/1 reject ref=x code=no-such-code next=\"n\"' >&2", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			serveWireSyncFixture(t, tc.hook, tc.marked)

			// Act
			err := run([]string{"sync", "origin"})

			// Assert
			if err == nil || err.Error() != plain {
				t.Fatalf("sync error = %v, want %q", err, plain)
			}
			var rejected *serveRejectedPushError
			if errors.As(err, &rejected) {
				t.Fatal("sync attached rejections it should have ignored")
			}
		})
	}
}
