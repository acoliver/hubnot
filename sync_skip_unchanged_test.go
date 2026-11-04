package main

import (
	"regexp"
	"strings"
	"testing"
)

// A sync after which nothing changed on the hub must not fetch or verify the
// already-accepted selections again; it reports them as unchanged and still
// sees every event.
func TestSyncReportsUnchangedSelectionsWithoutRefetching(t *testing.T) {
	team := newServeReplicationTeam(t)
	team.SelectAllTrusted(serveReplicationImplementer)
	proposal := team.OpenProposal(serveReplicationImplementer, "feature/unchanged", "Unchanged selections")
	team.SelectAllTrusted(serveReplicationMaintainer)
	first := team.Hn(serveReplicationMaintainer, "sync", "origin")
	assertOperationalContains(t, first, "Replication proposal "+proposal.ID+": promoted")

	second := team.Hn(serveReplicationMaintainer, "sync", "origin")
	if strings.Contains(second, ": promoted") {
		t.Fatalf("second sync promoted an unchanged selection:\n%s", second)
	}
	summary := regexp.MustCompile(`Synchronized (\d+) verified events with origin; promoted 0 selections; (\d+) unchanged\n`)
	firstCount := regexp.MustCompile(`Synchronized (\d+) verified events`).FindStringSubmatch(first)
	match := summary.FindStringSubmatch(second)
	if match == nil || firstCount == nil || match[1] != firstCount[1] || match[2] == "0" {
		t.Fatalf("second sync summary does not report unchanged selections with the same events:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}
