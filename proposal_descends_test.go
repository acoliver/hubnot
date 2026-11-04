package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A proposal whose head does not descend from its base is refused before it
// is signed: the hub would refuse to publish it, and every later event of
// the actor would then be stuck behind it.
func TestProposalOpenRefusesAHeadThatDoesNotDescendFromTheBase(t *testing.T) {
	project := newRunnerActorsProject(t)
	repo := project.alpha.repository
	commit := func(name string) {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runOperationalGit(t, repo, "add", name)
		runOperationalGit(t, repo, "commit", "-q", "-m", name)
	}
	runOperationalGit(t, repo, "switch", "-q", "-c", "stacked", "main")
	commit("stacked.txt")
	runOperationalGit(t, repo, "switch", "-q", "main")
	commit("main-moved.txt")
	chain := operationalActorRef(project.alpha.actor)
	before := runOperationalGit(t, repo, "rev-parse", chain)
	output := runOperationalCommandFailure(t, project.binary, repo, "proposal", "open", "--base", "main", "--head", "stacked", "Stacked")
	if !strings.Contains(output, "does not descend from base") {
		t.Fatalf("proposal open of a non-descending head failed for another reason:\n%s", output)
	}
	if after := runOperationalGit(t, repo, "rev-parse", chain); after != before {
		t.Fatalf("the refused proposal was signed: actor chain moved from %s to %s", before, after)
	}
	runOperationalGit(t, repo, "switch", "-q", "stacked")
	runOperationalGit(t, repo, "merge", "-q", "--no-edit", "main")
	runOperationalGit(t, repo, "switch", "-q", "main")
	runOperationalCommand(t, project.binary, repo, "proposal", "open", "--base", "main", "--head", "stacked", "Stacked, merged with main")
}
