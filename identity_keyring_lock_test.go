package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// keyringRecordActors lists the actors with a private record in the clone's
// shared keyring.
func keyringRecordActors(t *testing.T, clone worktreeClone) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(clone.commonDir, "hn", "identities"))
	if err != nil {
		t.Fatal(err)
	}
	actors := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") {
			actors = append(actors, strings.TrimSuffix(entry.Name(), ".json"))
		}
	}
	slices.Sort(actors)
	return actors
}

func assertNoRotationJournals(t *testing.T, clone worktreeClone) {
	t.Helper()
	for _, name := range []string{"rotation.json", "rotation-command.json"} {
		if _, err := os.Lstat(filepath.Join(clone.commonDir, "hn", name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("rotation journal %s remains: %v", name, err)
		}
	}
}

const rotationBusyMessage = "another identity rotation or identity update for this keyring is in progress"

func TestWorktreeConcurrentRotationsLeaveOneRotation(t *testing.T) {
	clone := newWorktreeClone(t, 2)
	mustHNIn(t, clone.main, "init", "--name", "Rotating actor")
	predecessor := shownIdentityIn(t, clone.main).Actor

	// Every rotation pauses after reading the keyring and before writing its
	// intent, so unserialized rotations would each generate a target.
	t.Setenv("HN_INTERNAL_TESTING", "1")
	t.Setenv("HN_TEST_IDENTITY_PAUSE_AT", "rotation-before-intent")
	t.Setenv("HN_TEST_IDENTITY_PAUSE", "1s")
	results := runHNInParallel(t, clone.worktrees, func(int) []string {
		return []string{"identity", "rotate"}
	})
	winner := -1
	for index, result := range results {
		if result.Error == "" {
			if winner != -1 {
				t.Fatalf("both simultaneous rotations reported success: %#v", results)
			}
			winner = index
			continue
		}
		if !strings.Contains(result.Error, rotationBusyMessage) {
			t.Fatalf("worktree %d rotation failed uncleanly: %s", index, result.Error)
		}
	}
	if winner == -1 {
		t.Fatalf("no simultaneous rotation succeeded: %#v", results)
	}
	successor := shownIdentityIn(t, clone.main).Actor
	if successor == predecessor || !strings.Contains(results[winner].Stdout, "Rotated identity from "+predecessor+" to "+successor) {
		t.Fatalf("active identity %s does not match the reported rotation %q", successor, results[winner].Stdout)
	}
	if got, want := keyringRecordActors(t, clone), sortedUniqueStrings(predecessor, successor); !slices.Equal(got, want) {
		t.Fatalf("keyring records %v, want only the predecessor and one successor %v", got, want)
	}
	assertNoRotationJournals(t, clone)
	for _, dir := range clone.worktrees {
		if got := shownIdentityIn(t, dir).Actor; got != successor {
			t.Fatalf("identity in %s = %s, want %s", dir, got, successor)
		}
	}
}

func TestWorktreeInterruptedConcurrentRotationRetryCompletes(t *testing.T) {
	clone := newWorktreeClone(t, 2)
	mustHNIn(t, clone.main, "init", "--name", "Rotating actor")
	predecessor := shownIdentityIn(t, clone.main).Actor

	// Both rotations start together, and whichever runs is interrupted once
	// its authorization is durable, leaving its journals behind.
	t.Setenv("HN_INTERNAL_TESTING", "1")
	t.Setenv("HN_TEST_IDENTITY_PAUSE_AT", "rotation-before-intent")
	t.Setenv("HN_TEST_IDENTITY_PAUSE", "1s")
	t.Setenv("HN_TEST_ROTATION_INTERRUPT_AFTER", "rotation-authorization-durable")
	results := runHNInParallel(t, clone.worktrees, func(int) []string {
		return []string{"identity", "rotate"}
	})
	interrupted := 0
	for index, result := range results {
		switch {
		case strings.Contains(result.Error, "injected rotation interruption after rotation-authorization-durable"):
			interrupted++
		case strings.Contains(result.Error, rotationBusyMessage):
		default:
			t.Fatalf("worktree %d rotation: stdout %q error %q", index, result.Stdout, result.Error)
		}
	}
	if interrupted != 1 {
		t.Fatalf("%d rotations ran, want exactly one: %#v", interrupted, results)
	}
	if active := shownIdentityIn(t, clone.main).Actor; active != predecessor {
		t.Fatalf("interrupted rotation switched the active identity to %s", active)
	}
	records := keyringRecordActors(t, clone)
	if len(records) != 2 || !slices.Contains(records, predecessor) {
		t.Fatalf("keyring records after the interrupted race %v, want the predecessor and one target", records)
	}
	target := records[0]
	if target == predecessor {
		target = records[1]
	}

	// An ordinary retry from either worktree completes that one journal.
	t.Setenv("HN_TEST_IDENTITY_PAUSE_AT", "")
	t.Setenv("HN_TEST_ROTATION_INTERRUPT_AFTER", "")
	retried := mustHNIn(t, clone.worktrees[1], "identity", "rotate")
	if !strings.Contains(retried, "Rotated identity from "+predecessor+" to "+target) {
		t.Fatalf("retry rotated %q, want the journaled target %s", retried, target)
	}
	for _, dir := range append([]string{clone.main}, clone.worktrees...) {
		if got := shownIdentityIn(t, dir).Actor; got != target {
			t.Fatalf("identity in %s = %s, want %s", dir, got, target)
		}
	}
	if got := keyringRecordActors(t, clone); !slices.Equal(got, records) {
		t.Fatalf("retry changed keyring records from %v to %v", records, got)
	}
	assertNoRotationJournals(t, clone)
}

func TestWorktreeConcurrentInitWithoutHardLinksActivatesOneIdentity(t *testing.T) {
	// The file system offers no hard links, and every initializer pauses
	// between its absence check and the replacing rename it falls back to.
	t.Setenv("HN_INTERNAL_TESTING", "1")
	t.Setenv("HN_TEST_IDENTITY_NO_HARD_LINKS", "1")
	t.Setenv("HN_TEST_IDENTITY_PAUSE_AT", "before-active-fallback-publish")
	t.Setenv("HN_TEST_IDENTITY_PAUSE", "500ms")
	const rounds = 3
	for round := 0; round < rounds; round++ {
		clone := newWorktreeClone(t, 2)
		results := runHNInParallel(t, clone.worktrees, func(index int) []string {
			return []string{"init", "--name", fmt.Sprintf("Racer %d", index)}
		})
		winner := -1
		for index, result := range results {
			if result.Error == "" {
				if winner != -1 {
					t.Fatalf("round %d: both hn init commands reported success without hard links: %#v", round, results)
				}
				winner = index
				continue
			}
			if !strings.Contains(result.Error, "identity already exists") {
				t.Fatalf("round %d worktree %d init failed uncleanly: %s", round, index, result.Error)
			}
		}
		if winner == -1 {
			t.Fatalf("round %d: no hn init succeeded: %#v", round, results)
		}
		identity := shownIdentityIn(t, clone.main)
		if identity.Name != fmt.Sprintf("Racer %d", winner) || !strings.Contains(results[winner].Stdout, "("+shortID(identity.Actor)+")") {
			t.Fatalf("round %d: active identity %#v, want the reported winner %q", round, identity, results[winner].Stdout)
		}
		if got := keyringRecordActors(t, clone); !slices.Equal(got, []string{identity.Actor}) {
			t.Fatalf("round %d: keyring records %v, want only the active %s", round, got, identity.Actor)
		}
	}
}
