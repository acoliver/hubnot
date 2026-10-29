package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// standingPolicyCommitBase commits draft as the policy on the checked-out
// main branch, so later checks compare against it with --base main.
func standingPolicyCommitBase(t *testing.T, repo standingPolicyFieldsRepo, draft []byte) {
	t.Helper()
	repo.WriteDraft(t, draft)
	runOperationalGit(t, repo.Repository, "add", ".hn/policy.json")
	runOperationalGit(t, repo.Repository, "commit", "-q", "--allow-empty", "-m", "base policy")
}

func standingPolicyWithoutAutomerge(policy, _ map[string]any) { delete(policy, "automerge") }

// standingPolicyRules replaces the draft's automerge rules.
func standingPolicyRules(rules ...map[string]any) standingPolicyFieldsEdit {
	return func(policy, _ map[string]any) {
		list := make([]any, len(rules))
		for i, rule := range rules {
			list[i] = rule
		}
		policy["automerge"] = map[string]any{"rules": list}
	}
}

// standingPolicyAssertSuffix fails unless output ends with exactly the
// given lines.
func standingPolicyAssertSuffix(t *testing.T, output string, lines ...string) {
	t.Helper()
	want := strings.Join(lines, "\n") + "\n"
	if !strings.HasSuffix(output, want) {
		t.Fatalf("output does not end with:\n%s\ngot:\n%s", want, output)
	}
}

// standingPolicyRuleChangeLines are the hn policy check lines for one rule
// in their printed order. Callers pass every value.
func standingPolicyRuleChangeLines(name string, values ...string) []string {
	labels := []string{
		"enabled", "target branch",
		"actors added", "actors removed",
		"authors added", "authors removed",
		"paths added", "paths removed",
		"acceptance", "reviewers added", "reviewers removed",
	}
	if len(values) != len(labels) {
		panic("standingPolicyRuleChangeLines needs one value per label")
	}
	lines := make([]string, len(labels))
	for i, label := range labels {
		lines[i] = "  automerge rule " + name + " " + label + ": " + values[i]
	}
	return lines
}

func TestStandingPolicyInspectionCheck(t *testing.T) {
	repo := standingPolicyFieldsNewRepo(t)
	actors := repo.Actors
	standing := standingPolicyFieldsStanding(actors.Executor)
	set := standingPolicyFieldsSet
	with := standingPolicyFieldsWith

	// US2-1: the comparison lists the added standing rule with its acceptance
	// mode, executing actors, authors, paths, target and reviewers as full
	// identities.
	t.Run("US2-1 added standing rule", func(t *testing.T) {
		standingPolicyCommitBase(t, repo, standingPolicyFieldsDraft(t, actors, standingPolicyWithoutAutomerge))
		output := repo.Check(t, standingPolicyFieldsDraft(t, actors, with(
			standing,
			set("reviewers", []string{actors.Reviewer}),
			set("paths", []string{"src/", "tests/", "docs/"}),
		)), true)
		assertPolicyOutputContains(t, output, "  automerge enabled: changed (false -> true)\n")
		standingPolicyAssertSuffix(t, output, standingPolicyRuleChangeLines("ordinary",
			"changed (false -> true)", `"" -> "main"`,
			actors.Executor, "(none)",
			actors.Author, "(none)",
			"docs/, src/, tests/", "(none)",
			"added (rule)", actors.Reviewer, "(none)",
		)...)
	})

	cases := []struct {
		name             string
		base, draft      standingPolicyFieldsEdit
		acceptance       string
		reviewersAdded   string
		reviewersRemoved string
	}{
		{"rule added in maintainers mode", standingPolicyWithoutAutomerge, nil, "added (maintainers)", "(none)", "(none)"},
		{"rule added in rule mode", standingPolicyWithoutAutomerge, with(standing, set("reviewers", []string{actors.Reviewer})), "added (rule)", actors.Reviewer, "(none)"},
		{"rule removed in maintainers mode", nil, standingPolicyWithoutAutomerge, "removed (maintainers)", "(none)", "(none)"},
		{"rule removed in rule mode", with(standing, set("reviewers", []string{actors.Reviewer2, actors.Reviewer})), standingPolicyWithoutAutomerge, "removed (rule)", "(none)", actors.Reviewer + ", " + actors.Reviewer2},
		{"absent acceptance stays absent", nil, set("paths", []string{"docs/", "src/"}), "unchanged (maintainers)", "(none)", "(none)"},
		{"explicit maintainers gained", nil, set("acceptance", "maintainers"), "unchanged (maintainers)", "(none)", "(none)"},
		{"explicit maintainers dropped", set("acceptance", "maintainers"), nil, "unchanged (maintainers)", "(none)", "(none)"},
		{"rule mode unchanged", standing, with(standing, set("reviewers", []string{actors.Reviewer})), "unchanged (rule)", actors.Reviewer, "(none)"},
		{"maintainers to rule", nil, standing, "changed (maintainers -> rule)", "(none)", "(none)"},
		{"rule to maintainers", with(standing, set("reviewers", []string{actors.Reviewer})), set("reviewers", []string{actors.Reviewer2}), "changed (rule -> maintainers)", actors.Reviewer2, actors.Reviewer},
	}
	for _, tc := range cases {
		t.Run("change "+tc.name, func(t *testing.T) {
			standingPolicyCommitBase(t, repo, standingPolicyFieldsDraft(t, actors, tc.base))
			output := repo.Check(t, standingPolicyFieldsDraft(t, actors, tc.draft), true)
			lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
			if len(lines) < 4 || !strings.HasPrefix(lines[len(lines)-4], "  automerge rule ordinary paths removed: ") {
				t.Fatalf("acceptance and reviewer lines do not follow the paths lines:\n%s", output)
			}
			standingPolicyAssertSuffix(t, output,
				"  automerge rule ordinary acceptance: "+tc.acceptance,
				"  automerge rule ordinary reviewers added: "+tc.reviewersAdded,
				"  automerge rule ordinary reviewers removed: "+tc.reviewersRemoved,
			)
		})
	}

	t.Run("rules in sorted order", func(t *testing.T) {
		standingPolicyCommitBase(t, repo, standingPolicyFieldsDraft(t, actors, standingPolicyWithoutAutomerge))
		draft := standingPolicyFieldsDraft(t, actors, standingPolicyRules(
			map[string]any{
				"name": "b", "targetBranch": "main", "acceptance": "rule",
				"actors": []string{actors.Executor}, "authors": []string{actors.Author},
				"reviewers": []string{actors.Reviewer2}, "paths": []string{"src/"},
			},
			map[string]any{
				"name": "a", "targetBranch": "main",
				"actors": []string{actors.Maintainer}, "authors": []string{actors.Author},
				"paths": []string{"docs/"},
			},
		))
		output := repo.Check(t, draft, true)
		want := standingPolicyRuleChangeLines("a",
			"changed (false -> true)", `"" -> "main"`,
			actors.Maintainer, "(none)", actors.Author, "(none)", "docs/", "(none)",
			"added (maintainers)", "(none)", "(none)",
		)
		want = append(want, standingPolicyRuleChangeLines("b",
			"changed (false -> true)", `"" -> "main"`,
			actors.Executor, "(none)", actors.Author, "(none)", "src/", "(none)",
			"added (rule)", actors.Reviewer2, "(none)",
		)...)
		standingPolicyAssertSuffix(t, output, want...)
	})
}

func TestStandingPolicyInspectionShow(t *testing.T) {
	repo := standingPolicyFieldsNewRepo(t)
	actors := repo.Actors
	standingPolicyCommitBase(t, repo, standingPolicyFieldsDraft(t, actors, standingPolicyRules(
		map[string]any{
			"name": "ordinary", "targetBranch": "main", "acceptance": "rule",
			"actors": []string{actors.Executor}, "authors": []string{actors.Author},
			"reviewers": []string{actors.Reviewer}, "paths": []string{"src/", "tests/", "docs/"},
		},
		map[string]any{
			"name": "explicit", "targetBranch": "release", "acceptance": "maintainers",
			"actors": []string{actors.Maintainer}, "authors": []string{actors.Author},
			"reviewers": []string{actors.Reviewer2, actors.Reviewer}, "paths": []string{"CHANGELOG.md"},
		},
		map[string]any{
			"name": "docs-maint", "targetBranch": "main",
			"actors": []string{actors.Maintainer}, "authors": []string{actors.Author},
			"paths": []string{"notes/"},
		},
	)))
	output := runOperationalCommand(t, repo.Binary, repo.Repository, "policy", "show")
	want := strings.Join([]string{
		"Automerge: enabled (explicit execution only)",
		"Rule docs-maint; target branch main:",
		"  Acceptance: maintainers (maintainer acceptance required)",
		"  Executing maintainers:",
		"  " + actors.Maintainer,
		"  Proposal authors:",
		"  " + actors.Author,
		"  Counted reviewers: all trusted reviewers",
		"  Allowed literal paths:",
		`    "notes/"`,
		"Rule explicit; target branch release:",
		"  Acceptance: maintainers (maintainer acceptance required)",
		"  Executing maintainers:",
		"  " + actors.Maintainer,
		"  Proposal authors:",
		"  " + actors.Author,
		"  Counted reviewers:",
		"  " + actors.Reviewer,
		"  " + actors.Reviewer2,
		"  Allowed literal paths:",
		`    "CHANGELOG.md"`,
		"Rule ordinary; target branch main:",
		"  Acceptance: rule (standing acceptance; no maintainer acceptance per proposal)",
		"  Executing actors:",
		"  " + actors.Executor,
		"  Proposal authors:",
		"  " + actors.Author,
		"  Counted reviewers:",
		"  " + actors.Reviewer,
		"  Allowed literal paths:",
		`    "docs/"`,
		`    "src/"`,
		`    "tests/"`,
		"Pipelines:",
		"  (none)",
	}, "\n") + "\n"
	if !strings.Contains(output, want) {
		t.Fatalf("hn policy show output lacks:\n%s\ngot:\n%s", want, output)
	}
}

// standingPolicyPlaceholders maps the guide's placeholder fingerprints to
// the actors of a test repository.
func standingPolicyPlaceholders(actors standingPolicyFieldsActors) *strings.Replacer {
	return strings.NewReplacer(
		"<maintainer-actor>", actors.Maintainer,
		"<executor-actor>", actors.Executor,
		"<author-actor>", actors.Author,
		"<reviewer-actor>", actors.Reviewer,
		"<second-reviewer-actor>", actors.Reviewer2,
		"<untrusted-actor>", actors.Untrusted,
	)
}

var standingPolicyLeftoverPlaceholder = regexp.MustCompile(`<[a-z-]+-actor>`)

func standingPolicySubstitute(t *testing.T, actors standingPolicyFieldsActors, text string) string {
	t.Helper()
	substituted := standingPolicyPlaceholders(actors).Replace(text)
	if leftover := standingPolicyLeftoverPlaceholder.FindString(substituted); leftover != "" {
		t.Fatalf("documented example uses unknown placeholder %s", leftover)
	}
	return substituted
}

// standingPolicyGuideBlocks returns the fenced blocks that follow marker in
// docs/governance-v0.md, up to the next heading or example marker.
func standingPolicyGuideBlocks(t *testing.T, marker string) []string {
	t.Helper()
	guide, err := os.ReadFile(filepath.Join("docs", "governance-v0.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, rest, found := strings.Cut(string(guide), marker+"\n")
	if !found {
		t.Fatalf("governance guide omits %s", marker)
	}
	var blocks []string
	for {
		rest = strings.TrimLeft(rest, "\n")
		if !strings.HasPrefix(rest, "```") {
			break
		}
		_, body, _ := strings.Cut(rest, "\n")
		block, after, found := strings.Cut(body, "```\n")
		if !found {
			t.Fatalf("unterminated block after %s", marker)
		}
		blocks = append(blocks, block)
		rest = after
	}
	if len(blocks) == 0 {
		t.Fatalf("no fenced block follows %s", marker)
	}
	return blocks
}

type standingPolicyDocumentedRefusal struct {
	Rule, Proposals, Policy map[string]any
	Refusal                 string
}

func (r standingPolicyDocumentedRefusal) Edit(policy, rule map[string]any) {
	for key, value := range r.Rule {
		rule[key] = value
	}
	proposals := policy["proposals"].(map[string]any)
	for key, value := range r.Proposals {
		proposals[key] = value
	}
	for key, value := range r.Policy {
		policy[key] = value
	}
}

func standingPolicyParseRefusals(t *testing.T, block string) []standingPolicyDocumentedRefusal {
	t.Helper()
	var entries []standingPolicyDocumentedRefusal
	for _, paragraph := range strings.Split(strings.TrimSpace(block), "\n\n") {
		var entry standingPolicyDocumentedRefusal
		for _, line := range strings.Split(paragraph, "\n") {
			if strings.HasPrefix(line, "# ") {
				continue
			}
			key, value, found := strings.Cut(line, ": ")
			if !found {
				t.Fatalf("documented refusal line has no key: %q", line)
			}
			var target *map[string]any
			switch key {
			case "rule":
				target = &entry.Rule
			case "proposals":
				target = &entry.Proposals
			case "policy":
				target = &entry.Policy
			case "refusal":
				entry.Refusal = value
				continue
			default:
				t.Fatalf("documented refusal has unknown key %q", key)
			}
			if err := json.Unmarshal([]byte(value), target); err != nil {
				t.Fatalf("documented %s fragment %q: %v", key, value, err)
			}
		}
		if entry.Rule == nil || entry.Refusal == "" {
			t.Fatalf("documented refusal lacks a rule or refusal line:\n%s", paragraph)
		}
		entries = append(entries, entry)
	}
	return entries
}

// The guide's refusal example has one draft per contract row 4 to 10; each
// draft runs through hn policy check and is refused with the documented text.
func TestStandingPolicyDocumentedRefusals(t *testing.T) {
	const marker = "<!-- example: standing-policy-refusals -->"
	blocks := standingPolicyGuideBlocks(t, marker)
	if len(blocks) != 1 {
		t.Fatalf("%s is followed by %d blocks, want 1", marker, len(blocks))
	}
	repo := standingPolicyFieldsNewRepo(t)
	entries := standingPolicyParseRefusals(t, standingPolicySubstitute(t, repo.Actors, blocks[0]))
	if len(entries) != 7 {
		t.Fatalf("documented %d refusals, want one for each of contract rows 4 to 10", len(entries))
	}
	for _, entry := range entries {
		t.Run(entry.Refusal, func(t *testing.T) {
			output := repo.Check(t, standingPolicyFieldsDraft(t, repo.Actors, entry.Edit), false)
			if !strings.Contains(output, entry.Refusal+"\n") {
				t.Fatalf("refusal differs from the guide; want %q in:\n%s", entry.Refusal, output)
			}
		})
	}
}

// standingPolicyDocumentedCommand splits a documented "$ hn ..." block into
// hn arguments and the output lines that follow its "..." line.
func standingPolicyDocumentedCommand(t *testing.T, block string) ([]string, string) {
	t.Helper()
	commandLine, rest, _ := strings.Cut(block, "\n")
	if !strings.HasPrefix(commandLine, "$ hn ") {
		t.Fatalf("documented block does not start with an hn command: %q", block)
	}
	elision, expected, _ := strings.Cut(rest, "\n")
	if elision != "..." || expected == "" {
		t.Fatalf("documented block for %q lacks the ... line and output", commandLine)
	}
	return strings.Fields(strings.TrimPrefix(commandLine, "$ hn ")), expected
}

// The guide's standing rule example: the documented policy validates, hn
// policy check ends with the documented lines against a base without the
// automerge section, and hn policy show prints the documented rule.
func TestStandingPolicyDocumentedRule(t *testing.T) {
	const marker = "<!-- example: standing-policy-rule -->"
	blocks := standingPolicyGuideBlocks(t, marker)
	if len(blocks) != 3 {
		t.Fatalf("%s is followed by %d blocks, want policy, check and show", marker, len(blocks))
	}
	repo := standingPolicyFieldsNewRepo(t)
	draft := []byte(standingPolicySubstitute(t, repo.Actors, blocks[0]))
	var base map[string]any
	if err := json.Unmarshal(draft, &base); err != nil {
		t.Fatalf("documented policy: %v", err)
	}
	delete(base, "automerge")
	encodedBase, err := json.MarshalIndent(base, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	standingPolicyCommitBase(t, repo, append(encodedBase, '\n'))

	checkArgs, checkLines := standingPolicyDocumentedCommand(t, standingPolicySubstitute(t, repo.Actors, blocks[1]))
	repo.WriteDraft(t, draft)
	output := runOperationalCommand(t, repo.Binary, repo.Repository, checkArgs...)
	if !strings.HasSuffix(output, checkLines) {
		t.Fatalf("hn %s output does not end with the documented lines:\n%s\ngot:\n%s", strings.Join(checkArgs, " "), checkLines, output)
	}

	standingPolicyCommitBase(t, repo, draft)
	showArgs, showLines := standingPolicyDocumentedCommand(t, standingPolicySubstitute(t, repo.Actors, blocks[2]))
	output = runOperationalCommand(t, repo.Binary, repo.Repository, showArgs...)
	if !strings.HasSuffix(output, showLines) {
		t.Fatalf("hn %s output does not end with the documented lines:\n%s\ngot:\n%s", strings.Join(showArgs, " "), showLines, output)
	}
}
