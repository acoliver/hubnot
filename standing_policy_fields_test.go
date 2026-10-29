package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// standingPolicyFieldsActors names the fingerprints a draft policy uses.
// Maintainer and Executor are real identities in black-box tests; the others
// only need to be well-formed fingerprints for policy validation.
type standingPolicyFieldsActors struct {
	Maintainer string
	Executor   string
	Author     string
	Reviewer   string
	Reviewer2  string
	Untrusted  string
}

const standingPolicyFieldsRuleName = "ordinary"

func standingPolicyFieldsSyntheticActors() standingPolicyFieldsActors {
	return standingPolicyFieldsActors{
		Maintainer: strings.Repeat("a", 64),
		Executor:   strings.Repeat("c", 64),
		Author:     strings.Repeat("d", 64),
		Reviewer:   strings.Repeat("b", 64),
		Reviewer2:  strings.Repeat("e", 64),
		Untrusted:  strings.Repeat("f", 64),
	}
}

// standingPolicyFieldsEdit changes a draft before encoding. policy is the
// whole document and rule is its single automerge rule.
type standingPolicyFieldsEdit func(policy, rule map[string]any)

// standingPolicyFieldsDraft encodes a valid hn.policy/0 document with one
// maintainers-mode automerge rule, then applies edit.
func standingPolicyFieldsDraft(t *testing.T, actors standingPolicyFieldsActors, edit standingPolicyFieldsEdit) []byte {
	t.Helper()
	rule := map[string]any{
		"name":         standingPolicyFieldsRuleName,
		"targetBranch": "main",
		"actors":       []string{actors.Maintainer},
		"authors":      []string{actors.Author},
		"paths":        []string{"docs/"},
	}
	policy := map[string]any{
		"version":     policyVersion,
		"maintainers": []string{actors.Maintainer},
		"proposals": map[string]any{
			"requiredApprovals":   1,
			"requiredAccepts":     1,
			"trustedReviewers":    []string{actors.Reviewer, actors.Reviewer2},
			"allowAuthorApproval": false,
		},
		"pipelines": map[string]any{},
		"automerge": map[string]any{"rules": []any{rule}},
	}
	if edit != nil {
		edit(policy, rule)
	}
	encoded, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(encoded, '\n')
}

func standingPolicyFieldsRaw(raw string) json.RawMessage { return json.RawMessage(raw) }

func standingPolicyFieldsStanding(executor string) standingPolicyFieldsEdit {
	return func(_, rule map[string]any) {
		rule["acceptance"] = "rule"
		rule["actors"] = []string{executor}
	}
}

func standingPolicyFieldsWith(edits ...standingPolicyFieldsEdit) standingPolicyFieldsEdit {
	return func(policy, rule map[string]any) {
		for _, edit := range edits {
			edit(policy, rule)
		}
	}
}

func standingPolicyFieldsSet(key string, value any) standingPolicyFieldsEdit {
	return func(_, rule map[string]any) { rule[key] = value }
}

func standingPolicyFieldsRequiredApprovals(n int) standingPolicyFieldsEdit {
	return func(policy, _ map[string]any) {
		policy["proposals"].(map[string]any)["requiredApprovals"] = n
	}
}

func standingPolicyFieldsProtected(reviewer string, paths ...string) standingPolicyFieldsEdit {
	return func(policy, _ map[string]any) {
		policy["protectedChecks"] = map[string]any{
			"paths":             paths,
			"trustedReviewers":  []string{reviewer},
			"requiredApprovals": 1,
		}
	}
}

type standingPolicyFieldsCase struct {
	Name    string
	Edit    standingPolicyFieldsEdit
	Message string
}

// standingPolicyFieldsRefusals returns one refused draft per contract row
// (1 to 10), every .hn selector and both overlap directions.
func standingPolicyFieldsRefusals(actors standingPolicyFieldsActors) []standingPolicyFieldsCase {
	rule := fmt.Sprintf("%q", standingPolicyFieldsRuleName)
	acceptanceMessage := "automerge rule " + rule + ` acceptance must be "maintainers" or "rule"`
	reviewersMessage := "automerge rule " + rule + " reviewers must be an array of actor fingerprints"
	maintainerMessage := "automerge rule " + rule + " actor " + actors.Executor + " is not a maintainer"
	countMessage := "automerge rule " + rule + " requires 1 to 64 reviewers when reviewers is present"
	standing := standingPolicyFieldsStanding(actors.Executor)
	var cases []standingPolicyFieldsCase
	for _, raw := range []string{`"Rule"`, `""`, `null`, `1`, `true`, `["rule"]`, `{}`} {
		cases = append(cases, standingPolicyFieldsCase{
			Name: "row1 acceptance " + raw, Edit: standingPolicyFieldsSet("acceptance", standingPolicyFieldsRaw(raw)), Message: acceptanceMessage,
		})
	}
	for _, raw := range []string{`null`, `"` + actors.Reviewer + `"`, `[1]`, `[null]`, `{}`} {
		cases = append(cases, standingPolicyFieldsCase{
			Name: "row2 reviewers " + raw, Edit: standingPolicyFieldsSet("reviewers", standingPolicyFieldsRaw(raw)), Message: reviewersMessage,
		})
	}
	many := make([]string, 65)
	for i := range many {
		many[i] = fmt.Sprintf("%064x", i+1)
	}
	cases = append(cases,
		standingPolicyFieldsCase{
			Name:    "row3 absent acceptance non-maintainer actor",
			Edit:    standingPolicyFieldsSet("actors", []string{actors.Executor}),
			Message: maintainerMessage,
		},
		standingPolicyFieldsCase{
			Name:    "row3 explicit maintainers non-maintainer actor",
			Edit:    standingPolicyFieldsWith(standingPolicyFieldsSet("acceptance", "maintainers"), standingPolicyFieldsSet("actors", []string{actors.Executor})),
			Message: maintainerMessage,
		},
		standingPolicyFieldsCase{
			Name:    "row4 rule without required approvals",
			Edit:    standingPolicyFieldsWith(standing, standingPolicyFieldsRequiredApprovals(0)),
			Message: "automerge rule " + rule + ` has acceptance "rule", which requires proposals.requiredApprovals of at least 1`,
		},
		standingPolicyFieldsCase{
			Name:    "row5 empty reviewers",
			Edit:    standingPolicyFieldsWith(standing, standingPolicyFieldsSet("reviewers", []string{})),
			Message: countMessage,
		},
		standingPolicyFieldsCase{
			Name:    "row5 sixty-five reviewers",
			Edit:    standingPolicyFieldsWith(standing, standingPolicyFieldsSet("reviewers", many)),
			Message: countMessage,
		},
		standingPolicyFieldsCase{
			Name:    "row6 duplicate reviewer",
			Edit:    standingPolicyFieldsWith(standing, standingPolicyFieldsSet("reviewers", []string{actors.Reviewer, actors.Reviewer})),
			Message: "automerge reviewers contains duplicate actor " + actors.Reviewer,
		},
		standingPolicyFieldsCase{
			Name:    "row6 invalid reviewer",
			Edit:    standingPolicyFieldsWith(standing, standingPolicyFieldsSet("reviewers", []string{"not-a-fingerprint"})),
			Message: `automerge reviewers contains invalid actor "not-a-fingerprint"`,
		},
		standingPolicyFieldsCase{
			Name:    "row7 untrusted reviewer",
			Edit:    standingPolicyFieldsWith(standing, standingPolicyFieldsSet("reviewers", []string{actors.Untrusted})),
			Message: "automerge rule " + rule + " reviewer " + actors.Untrusted + " is not a trusted reviewer",
		},
		standingPolicyFieldsCase{
			Name:    "row8 fewer reviewers than required approvals",
			Edit:    standingPolicyFieldsWith(standing, standingPolicyFieldsRequiredApprovals(2), standingPolicyFieldsSet("reviewers", []string{actors.Reviewer})),
			Message: "automerge rule " + rule + " lists 1 reviewers, fewer than proposals.requiredApprovals (2)",
		},
	)
	for _, selector := range []string{".hn", ".hn/", ".hn/pipelines/", ".hn/policy.json"} {
		cases = append(cases, standingPolicyFieldsCase{
			Name:    "row9 " + selector,
			Edit:    standingPolicyFieldsWith(standing, standingPolicyFieldsSet("paths", []string{"docs/", selector})),
			Message: fmt.Sprintf("automerge rule %s path %q covers .hn, which a rule with acceptance \"rule\" cannot cover", rule, selector),
		})
	}
	for _, overlap := range []struct{ protected, path string }{
		{"scripts/lint.sh", "scripts/"},
		{"scripts/lint.sh", "scripts/lint.sh"},
		{"scripts/", "scripts/lint/run.sh"},
		{"scripts/", "scripts/"},
	} {
		cases = append(cases, standingPolicyFieldsCase{
			Name:    "row10 " + overlap.path + " over " + overlap.protected,
			Edit:    standingPolicyFieldsWith(standing, standingPolicyFieldsProtected(actors.Reviewer, overlap.protected), standingPolicyFieldsSet("paths", []string{"docs/", overlap.path})),
			Message: fmt.Sprintf("automerge rule %s path %q overlaps protected check path %q", rule, overlap.path, overlap.protected),
		})
	}
	return cases
}

func standingPolicyFieldsRule(t *testing.T, policy PolicyDocument) AutomergeRule {
	t.Helper()
	if policy.Automerge == nil || len(policy.Automerge.Rules) != 1 {
		t.Fatalf("expected one automerge rule: %#v", policy.Automerge)
	}
	return policy.Automerge.Rules[0]
}

func TestStandingPolicyFieldsRefusals(t *testing.T) {
	actors := standingPolicyFieldsSyntheticActors()
	for _, tc := range standingPolicyFieldsRefusals(actors) {
		t.Run(tc.Name, func(t *testing.T) {
			_, _, err := parsePolicyBytes(standingPolicyFieldsDraft(t, actors, tc.Edit))
			if err == nil || !strings.Contains(err.Error(), tc.Message) {
				t.Fatalf("got %v, want %q", err, tc.Message)
			}
		})
	}
}

func TestStandingPolicyFieldsAcceptedValues(t *testing.T) {
	actors := standingPolicyFieldsSyntheticActors()
	standing := standingPolicyFieldsStanding(actors.Executor)
	cases := []struct {
		name      string
		edit      standingPolicyFieldsEdit
		mode      string
		reviewers []string
	}{
		{"absent fields", nil, "maintainers", nil},
		{"explicit maintainers", standingPolicyFieldsSet("acceptance", "maintainers"), "maintainers", nil},
		{"rule with non-maintainer executor", standing, "rule", nil},
		{"reviewers in maintainers mode", standingPolicyFieldsSet("reviewers", []string{actors.Reviewer}), "maintainers", []string{actors.Reviewer}},
		{"reviewers in rule mode", standingPolicyFieldsWith(standing, standingPolicyFieldsSet("reviewers", []string{actors.Reviewer2, actors.Reviewer})), "rule", []string{actors.Reviewer2, actors.Reviewer}},
		{"rule path .hnx", standingPolicyFieldsWith(standing, standingPolicyFieldsSet("paths", []string{".hnx/"})), "rule", nil},
		{"maintainers mode may still name .hn", standingPolicyFieldsSet("paths", []string{".hn/"}), "maintainers", nil},
		{"maintainers mode may overlap protected paths", standingPolicyFieldsWith(standingPolicyFieldsProtected(actors.Reviewer, "scripts/"), standingPolicyFieldsSet("paths", []string{"scripts/"})), "maintainers", nil},
		{"lint.sh.bak beside protected lint.sh", standingPolicyFieldsWith(standing, standingPolicyFieldsProtected(actors.Reviewer, "scripts/lint.sh"), standingPolicyFieldsSet("paths", []string{"scripts/lint.sh.bak"})), "rule", nil},
		{"script/ beside protected lint.sh", standingPolicyFieldsWith(standing, standingPolicyFieldsProtected(actors.Reviewer, "scripts/lint.sh"), standingPolicyFieldsSet("paths", []string{"script/"})), "rule", nil},
		{"scriptsx/ beside protected scripts/", standingPolicyFieldsWith(standing, standingPolicyFieldsProtected(actors.Reviewer, "scripts/"), standingPolicyFieldsSet("paths", []string{"scriptsx/"})), "rule", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			policy, _, err := parsePolicyBytes(standingPolicyFieldsDraft(t, actors, tc.edit))
			if err != nil {
				t.Fatal(err)
			}
			rule := standingPolicyFieldsRule(t, policy)
			if got := rule.Acceptance.Mode(); got != tc.mode {
				t.Fatalf("Mode() = %q, want %q", got, tc.mode)
			}
			if got := rule.Acceptance.Standing(); got != (tc.mode == "rule") {
				t.Fatalf("Standing() = %v for mode %q", got, tc.mode)
			}
			if got := rule.Reviewers.Actors(); !reflect.DeepEqual(got, tc.reviewers) {
				t.Fatalf("Actors() = %#v, want %#v", got, tc.reviewers)
			}
		})
	}
}

// encoding/json folds key case when decoding into a struct, so a differently
// cased key fills the field. The value is still validated.
func TestStandingPolicyFieldsFoldedKeyCaseIsStillValidated(t *testing.T) {
	actors := standingPolicyFieldsSyntheticActors()
	_, _, err := parsePolicyBytes(standingPolicyFieldsDraft(t, actors, standingPolicyFieldsSet("Acceptance", "Rule")))
	if err == nil || !strings.Contains(err.Error(), `automerge rule "ordinary" acceptance must be "maintainers" or "rule"`) {
		t.Fatalf("folded key was not validated: %v", err)
	}
	policy, _, err := parsePolicyBytes(standingPolicyFieldsDraft(t, actors, standingPolicyFieldsWith(standingPolicyFieldsStanding(actors.Executor), func(_, rule map[string]any) {
		delete(rule, "acceptance")
		rule["Acceptance"] = "rule"
	})))
	if err != nil {
		t.Fatal(err)
	}
	if !standingPolicyFieldsRule(t, policy).Acceptance.Standing() {
		t.Fatal("folded acceptance key did not decode")
	}
}

// A rule without the new fields keeps reporting the maintainer check before
// the path check, exactly as before the fields existed.
func TestStandingPolicyFieldsLegacyRuleKeepsCheckOrder(t *testing.T) {
	actors := standingPolicyFieldsSyntheticActors()
	_, _, err := parsePolicyBytes(standingPolicyFieldsDraft(t, actors, standingPolicyFieldsWith(
		standingPolicyFieldsSet("actors", []string{actors.Executor}),
		standingPolicyFieldsSet("paths", []string{"../escape"}),
	)))
	want := `automerge rule "ordinary" actor ` + actors.Executor + " is not a maintainer"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("got %v, want %q", err, want)
	}
}

func TestStandingPolicyFieldsSelectorOverlap(t *testing.T) {
	cases := []struct {
		rule, protected string
		overlap         bool
	}{
		{"scripts/", "scripts/lint.sh", true},
		{"scripts/lint.sh", "scripts/lint.sh", true},
		{"scripts/lint.sh.bak", "scripts/lint.sh", false},
		{"script/", "scripts/lint.sh", false},
		{"scripts/lint/run.sh", "scripts/", true},
		{"scripts/", "scripts/", true},
		{"scriptsx/", "scripts/", false},
		{"scripts", "scripts/", true},
	}
	for _, tc := range cases {
		if got := automergeSelectorsOverlap(tc.rule, tc.protected); got != tc.overlap {
			t.Errorf("automergeSelectorsOverlap(%q, %q) = %v, want %v", tc.rule, tc.protected, got, tc.overlap)
		}
	}
}

func standingPolicyFieldsLegacyDocument() PolicyDocument {
	actors := standingPolicyFieldsSyntheticActors()
	return PolicyDocument{
		Version:     policyVersion,
		Maintainers: []string{actors.Maintainer},
		Proposals:   ProposalPolicy{RequiredApprovals: 1, RequiredAccepts: 1, TrustedReviewers: []string{actors.Reviewer}},
		Pipelines:   map[string]PipelinePolicy{},
		Automerge: &AutomergePolicy{Rules: []AutomergeRule{{
			Name: "docs", TargetBranch: "main", Actors: []string{actors.Maintainer}, Authors: []string{actors.Author}, Paths: []string{"docs/"},
		}}},
	}
}

func TestStandingPolicyFieldsLegacyEncodingIsUnchanged(t *testing.T) {
	a, b, d := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("d", 64)
	// Encoding of this document before the acceptance and reviewers fields existed.
	want := `{"version":"hn.policy/0","maintainers":["` + a + `"],"proposals":{"requiredApprovals":1,"requiredAccepts":1,"trustedReviewers":["` + b + `"],"allowAuthorApproval":false},"pipelines":{},"automerge":{"rules":[{"name":"docs","targetBranch":"main","actors":["` + a + `"],"authors":["` + d + `"],"paths":["docs/"]}]}}`
	encoded, err := json.Marshal(standingPolicyFieldsLegacyDocument())
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != want {
		t.Fatalf("legacy encoding changed:\n got %s\nwant %s", encoded, want)
	}
	indented, err := json.MarshalIndent(standingPolicyFieldsLegacyDocument(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(indented), "acceptance") || strings.Contains(string(indented), "reviewers\"") {
		t.Fatalf("absent fields were encoded:\n%s", indented)
	}
	policy, _, err := parsePolicyBytes(indented)
	if err != nil {
		t.Fatal(err)
	}
	if rule := standingPolicyFieldsRule(t, policy); !rule.Acceptance.IsZero() || !rule.Reviewers.IsZero() {
		t.Fatalf("absent fields decoded as present: %#v", rule)
	}
}

func TestStandingPolicyFieldsPresentFieldsRoundTrip(t *testing.T) {
	actors := standingPolicyFieldsSyntheticActors()
	document := standingPolicyFieldsLegacyDocument()
	document.Automerge.Rules[0].Actors = []string{actors.Executor}
	document.Automerge.Rules[0].Acceptance = automergeAcceptanceOf("rule")
	document.Automerge.Rules[0].Reviewers = automergeReviewersOf(actors.Reviewer)
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"acceptance": "rule"`, `"reviewers": [`, actors.Reviewer} {
		if !strings.Contains(string(encoded), want) {
			t.Fatalf("encoding lacks %q:\n%s", want, encoded)
		}
	}
	policy, _, err := parsePolicyBytes(encoded)
	if err != nil {
		t.Fatal(err)
	}
	rule := standingPolicyFieldsRule(t, policy)
	if !rule.Acceptance.Standing() || !reflect.DeepEqual(rule.Reviewers.Actors(), []string{actors.Reviewer}) {
		t.Fatalf("round trip lost fields: mode %q reviewers %#v", rule.Acceptance.Mode(), rule.Reviewers.Actors())
	}
	again, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(encoded) {
		t.Fatalf("second encoding differs:\n%s\n%s", again, encoded)
	}
}

func TestStandingPolicyFieldsChangedPathsInOneStore(t *testing.T) {
	root := t.TempDir()
	commit := func(repository, file, content string) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repository, file), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		runOperationalGit(t, repository, "add", file)
		runOperationalGit(t, repository, "commit", "-q", "-m", file+" "+content)
		return runOperationalGit(t, repository, "rev-parse", "HEAD")
	}
	first, second := filepath.Join(root, "first"), filepath.Join(root, "second")
	initOperationalRepository(t, first, "First")
	initOperationalRepository(t, second, "Second")
	firstBase := commit(first, "a.txt", "first base\n")
	firstHead := commit(first, "b.txt", "first head\n")
	secondBase := commit(second, "c.txt", "second base\n")
	secondHead := commit(second, "d.txt", "second head\n")
	firstGit, secondGit := filepath.Join(first, ".git"), filepath.Join(second, ".git")

	changed, err := changedPathsInOneStore(firstGit, secondGit, firstBase, firstHead)
	if err != nil || !reflect.DeepEqual(changed, []string{"b.txt"}) {
		t.Fatalf("both commits in the first store: %v %#v", err, changed)
	}
	changed, err = changedPathsInOneStore(firstGit, secondGit, secondBase, secondHead)
	if err != nil || !reflect.DeepEqual(changed, []string{"d.txt"}) {
		t.Fatalf("both commits only in the second store: %v %#v", err, changed)
	}
	if changed, err := changedPathsInOneStore(firstGit, secondGit, firstBase, secondHead); err == nil {
		t.Fatalf("mixed stores produced a diff: %#v", changed)
	}
	if changed, err := changedPathsInOneStore(firstGit, "", secondBase, secondHead); err == nil {
		t.Fatalf("missing commits without an accepted store produced a diff: %#v", changed)
	}
	if changed, err := changedPathsInOneStore(firstGit, firstGit, secondBase, secondHead); err == nil {
		t.Fatalf("same store used as fallback produced a diff: %#v", changed)
	}
}

// standingPolicyFieldsRepo is a Git repository with two hn identities in
// separate clones and a valid base policy committed on main.
type standingPolicyFieldsRepo struct {
	Binary     string
	Repository string
	Actors     standingPolicyFieldsActors
}

func standingPolicyFieldsNewRepo(t *testing.T) standingPolicyFieldsRepo {
	t.Helper()
	binary := buildOperationalBinary(t)
	root := t.TempDir()
	repository := filepath.Join(root, "maintainer")
	initOperationalRepository(t, repository, "Maintainer")
	runOperationalGit(t, repository, "commit", "--allow-empty", "-q", "-m", "seed")
	runOperationalCommand(t, binary, repository, "init", "--name", "Maintainer device")
	executor := filepath.Join(root, "executor")
	runOperationalGit(t, "", "clone", "-q", repository, executor)
	configureOperationalGit(t, executor, "Executor")
	runOperationalCommand(t, binary, executor, "init", "--name", "Executor device")

	actors := standingPolicyFieldsSyntheticActors()
	actors.Maintainer = readOperationalIdentity(t, binary, repository).Actor
	actors.Executor = readOperationalIdentity(t, binary, executor).Actor
	if actors.Maintainer == actors.Executor {
		t.Fatal("separate clones produced the same actor")
	}
	repo := standingPolicyFieldsRepo{Binary: binary, Repository: repository, Actors: actors}
	repo.WriteDraft(t, standingPolicyFieldsDraft(t, actors, nil))
	runOperationalGit(t, repository, "add", ".hn/policy.json")
	runOperationalGit(t, repository, "commit", "-q", "-m", "base policy")
	return repo
}

func (r standingPolicyFieldsRepo) WriteDraft(t *testing.T, draft []byte) {
	t.Helper()
	directory := filepath.Join(r.Repository, ".hn")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "policy.json"), draft, 0o644); err != nil {
		t.Fatal(err)
	}
}

// Check runs hn policy check against main for draft and returns its output.
// It fails the test if the exit status differs from wantSuccess or any ref
// changed.
func (r standingPolicyFieldsRepo) Check(t *testing.T, draft []byte, wantSuccess bool) string {
	t.Helper()
	r.WriteDraft(t, draft)
	before := runOperationalGit(t, r.Repository, "for-each-ref", "--format=%(refname) %(objectname)")
	args := []string{"policy", "check", "--base", "main", "--file", ".hn/policy.json"}
	var output string
	if wantSuccess {
		output = runOperationalCommand(t, r.Binary, r.Repository, args...)
	} else {
		output = runOperationalCommandFailure(t, r.Binary, r.Repository, args...)
	}
	if after := runOperationalGit(t, r.Repository, "for-each-ref", "--format=%(refname) %(objectname)"); after != before {
		t.Fatalf("hn policy check changed refs:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	return output
}

func TestStandingPolicyFieldsPolicyCheckRefusals(t *testing.T) {
	repo := standingPolicyFieldsNewRepo(t)
	for _, tc := range standingPolicyFieldsRefusals(repo.Actors) {
		t.Run(tc.Name, func(t *testing.T) {
			output := repo.Check(t, standingPolicyFieldsDraft(t, repo.Actors, tc.Edit), false)
			if !strings.Contains(output, tc.Message) {
				t.Fatalf("output lacks %q:\n%s", tc.Message, output)
			}
		})
	}
	t.Run("valid standing draft", func(t *testing.T) {
		draft := standingPolicyFieldsDraft(t, repo.Actors, standingPolicyFieldsWith(
			standingPolicyFieldsStanding(repo.Actors.Executor),
			standingPolicyFieldsSet("reviewers", []string{repo.Actors.Reviewer}),
		))
		output := repo.Check(t, draft, true)
		if !strings.Contains(output, "The base policy governs this amendment candidate.") {
			t.Fatalf("valid standing draft output:\n%s", output)
		}
	})
}
