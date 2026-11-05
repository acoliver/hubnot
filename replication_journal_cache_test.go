package main

import (
	"os"
	"testing"
)

// A cached acceptance state must follow every journal write in the same
// process, and a caller changing the returned maps must not change it.
func TestReplicationAcceptanceCacheFollowsJournalWritesAndIsNotShared(t *testing.T) {
	_, _, receiver, _, first := setupMemoryReplicationFixture(t)
	if err := os.Chdir(receiver); err != nil {
		t.Fatal(err)
	}
	selection := ReplicationSelection{Version: replicationSelectionVersion, Remote: "origin", Memories: []string{first.Envelope.Stream}, Budgets: defaultReplicationBudgets()}
	replicationAfterCopyHook = func() error { return os.ErrClosed }
	_, err := runReplicationTransaction(selection)
	replicationAfterCopyHook = nil
	if err == nil {
		t.Fatal("transaction did not stop after copy")
	}
	gitDir := mustGitText(t, "rev-parse", "--absolute-git-dir")
	denied, pending, err := loadReplicationAcceptanceState(gitDir)
	if err != nil || len(denied) == 0 || len(pending) == 0 {
		t.Fatalf("interrupted transaction: denied=%d pending=%d err=%v", len(denied), len(pending), err)
	}
	// Mutating a returned map, including one served from the cache, must not
	// leak into the next read.
	want := len(denied)
	hit, _, err := loadReplicationAcceptanceState(gitDir)
	if err != nil || len(hit) != want {
		t.Fatalf("second read: got %d, want %d (%v)", len(hit), want, err)
	}
	for _, m := range []map[string]bool{denied, hit} {
		for id := range m {
			delete(m, id)
		}
	}
	again, _, err := loadReplicationAcceptanceState(gitDir)
	if err != nil || len(again) != want {
		t.Fatalf("cached state changed by a caller: got %d, want %d (%v)", len(again), want, err)
	}
	// Completing the transaction rewrites the journal; the next read must see it.
	if _, err := runReplicationTransaction(selection); err != nil {
		t.Fatal(err)
	}
	after, pendingAfter, err := loadReplicationAcceptanceState(gitDir)
	if err != nil || len(after) != 0 || len(pendingAfter) != 0 {
		t.Fatalf("stale cached state after the journal changed: denied=%d pending=%d err=%v", len(after), len(pendingAfter), err)
	}
}
