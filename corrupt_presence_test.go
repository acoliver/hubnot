package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCorruptPresenceLegacyPromisorBoundary(t *testing.T) {
	for _, mode := range []string{"missing", "corrupt inventory", "corrupt diagnostic", "partial response"} {
		t.Run(mode, func(t *testing.T) {
			inReplicationTestRepository(t)
			dir := mustGitText(t, "rev-parse", "--absolute-git-dir")
			id := mustGitTextFromInput(t, []byte("not stored legacy"), "hash-object", "--stdin")
			if mode == "corrupt inventory" {
				corruptPresenceObject(t)
			}
			bin := t.TempDir()
			extra := ""
			if mode == "corrupt diagnostic" {
				extra = "echo 'error: inflate: corrupt storage' >&2\n"
			}
			if mode == "partial response" {
				extra = "printf '%s missing\\n' '" + id + "'\n"
			}
			script := "#!/bin/sh\nfor a in \"$@\"; do if [ \"$a\" = '--batch-all-objects' ]; then exec /usr/bin/git \"$@\"; fi; done\nfor a in \"$@\"; do if [ \"$a\" = '--batch-check=%(objectname) %(objecttype)' ]; then\n" + extra + "echo 'warning: lazy fetching disabled; some objects may not be available' >&2\necho 'fatal: could not fetch " + id + " from promisor remote' >&2\nexit 128\nfi; done\nexec /usr/bin/git \"$@\"\n"
			if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			got, err := gitObjectsPresentAt(dir, []string{id})
			if mode == "missing" {
				if err != nil || got == nil || got[id] {
					t.Fatalf("legacy physical absence: %v %v", got, err)
				}
			} else if err == nil || got != nil {
				t.Fatalf("legacy failure returned usable presence: %v %v", got, err)
			}
		})
	}
}

func corruptPresenceObject(t *testing.T) string {
	t.Helper()
	id := mustGitTextFromInput(t, []byte("damaged storage fixture"), "hash-object", "-w", "--stdin")
	dir := mustGitText(t, "rev-parse", "--absolute-git-dir")
	path := filepath.Join(dir, "objects", id[:2], id[2:])
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("invalid object"), 0o644); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestCorruptPresenceRealStorage(t *testing.T) {
	inReplicationTestRepository(t)
	dir := mustGitText(t, "rev-parse", "--absolute-git-dir")
	valid := mustGitText(t, "rev-parse", "HEAD")
	missing := mustGitTextFromInput(t, []byte("not stored"), "hash-object", "--stdin")
	got, err := gitObjectsPresentAt(dir, []string{valid, missing})
	if err != nil || !got[valid] || got[missing] {
		t.Fatalf("ordinary valid/missing: %v %v", got, err)
	}
	corrupt := corruptPresenceObject(t)
	for _, ids := range [][]string{{corrupt}, {valid, corrupt}, {missing}, {valid, missing}} {
		got, err := gitObjectsPresentAt(dir, ids)
		if err == nil || got != nil {
			t.Fatalf("damaged storage returned usable presence: %v %v", got, err)
		}
	}
}

func TestCorruptPresenceTransportRefusal(t *testing.T) {
	for _, fault := range []string{"valid", "malformed", "partial", "surplus", "nonzero", "diagnostic", "stderr limit", "stdout limit", "failed missing"} {
		t.Run(fault, func(t *testing.T) {
			inReplicationTestRepository(t)
			dir := mustGitText(t, "rev-parse", "--absolute-git-dir")
			id := mustGitText(t, "rev-parse", "HEAD")
			reply := "printf '%s commit\\n' '" + id + "'"
			switch fault {
			case "malformed":
				reply = "printf 'broken\\n'"
			case "partial":
				reply = "printf '%s commit' '" + id + "'"
			case "surplus":
				reply += "; printf '%s commit\\n' '" + id + "'"
			case "nonzero":
				reply += "; exit 7"
			case "diagnostic":
				reply += "; echo 'error: storage is corrupt' >&2"
			case "stderr limit":
				reply += "; head -c 18000 /dev/zero >&2"
			case "stdout limit":
				reply = "head -c 18000 /dev/zero"
			case "failed missing":
				reply = "printf '%s missing\\n' '" + id + "'; echo 'fatal: storage unavailable' >&2; exit 7"
			}
			bin := t.TempDir()
			seen := filepath.Join(bin, "seen")
			script := "#!/bin/sh\nfor a in \"$@\"; do if [ \"$a\" = '--batch-all-objects' ]; then exec /usr/bin/git \"$@\"; fi; done\nfor a in \"$@\"; do if [ \"$a\" = '--batch-check=%(objectname) %(objecttype)' ]; then\n: > '" + seen + "'\n" + reply + "\nexit 0\nfi; done\nexec /usr/bin/git \"$@\"\n"
			if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			got, err := gitObjectsPresentAt(dir, []string{id})
			if _, e := os.Stat(seen); e != nil {
				t.Fatalf("transport injection was not exercised: %v", e)
			}
			if fault == "valid" {
				if err != nil || !got[id] {
					t.Fatalf("valid transport control: %v %v", got, err)
				}
				return
			}
			if err == nil || got != nil {
				t.Fatalf("%s produced usable presence: %v %v", fault, got, err)
			}
			want := map[string]string{
				"malformed": "invalid object probe", "partial": "truncated",
				"surplus": "returned 2 object probes", "nonzero": "exit status 7",
				"diagnostic": "storage is corrupt", "stderr limit": "exceeds bound",
				"stdout limit": "exceeds bound", "failed missing": "storage unavailable",
			}[fault]
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("%s failed at the wrong boundary: want %q in %v", fault, want, err)
			}
		})
	}
}

func TestCorruptPresenceTransactionFatal(t *testing.T) {
	selection, actor, _, _ := setupSingleActorReplication(t)
	corruptPresenceObject(t)
	result, err := runReplicationTransaction(selection)
	if err == nil {
		t.Fatalf("corrupt main storage was deferred or accepted: %+v", result.Outcomes)
	}
	if !strings.Contains(err.Error(), "unaccepted-object inspection") {
		t.Fatalf("unexpected failure: %v", err)
	}
	assertRefAbsent(t, acceptedActorRef("origin", actor.Actor))
	events, readErr := collectEvents()
	if readErr != nil || len(events) != 0 {
		t.Fatalf("trust projection changed: %d %v", len(events), readErr)
	}
	dir := mustGitText(t, "rev-parse", "--absolute-git-dir")
	for _, sub := range []string{"anchors", "transactions"} {
		entries, e := os.ReadDir(filepath.Join(dir, "hn", "replication", sub))
		if e != nil && !os.IsNotExist(e) {
			t.Fatal(e)
		}
		if len(entries) != 0 {
			t.Fatalf("fatal pre-anchor inspection left %s: %v", sub, entries)
		}
	}
}
