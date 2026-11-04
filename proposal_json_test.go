package main

import (
	"encoding/json"
	"testing"
)

// hn proposal list --json reports each proposal with its signed facts, its
// gate requests and the paths it changes, for review tools.
func TestProposalListJSONReportsFactsRequestsAndPaths(t *testing.T) {
	project := newRunnerActorsProject(t)
	output := runOperationalCommand(t, project.binary, project.alpha.repository, "proposal", "list", "--json")
	var document struct {
		Schema string `json:"schema"`
		OK     bool   `json:"ok"`
		Data   struct {
			Items []proposalListJSON `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &document); err != nil {
		t.Fatalf("proposal list --json is not JSON: %v\n%s", err, output)
	}
	if document.Schema != "hn.proposal-list/1" || !document.OK {
		t.Fatalf("unexpected envelope: %+v", document)
	}
	var alpha *proposalListJSON
	for index := range document.Data.Items {
		if document.Data.Items[index].ID == project.alpha.proposal {
			alpha = &document.Data.Items[index]
		}
	}
	if alpha == nil {
		t.Fatalf("alpha's proposal %s missing from:\n%s", project.alpha.proposal, output)
	}
	if alpha.Actor != project.alpha.actor || alpha.Base == "" || alpha.Head == "" || alpha.Timestamp == "" || alpha.Error != "" {
		t.Fatalf("alpha's facts are incomplete: %+v", alpha)
	}
	if len(alpha.Requests) != 1 || alpha.Requests[0].ID != project.alpha.request || alpha.Requests[0].Pipeline != "test" {
		t.Fatalf("alpha's run request is not reported: %+v", alpha.Requests)
	}
	if len(alpha.Changed) == 0 || alpha.Lineage.Root != project.alpha.proposal || alpha.Merged || alpha.Lineage.Superseded {
		t.Fatalf("alpha's paths or lineage are wrong: %+v", alpha)
	}
}
