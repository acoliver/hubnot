package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQuarantineBatchOrdinaryImportControls(t *testing.T) {
	for _, defect := range []string{"valid", "signature", "missing signature", "mode", "extra attachment", "parent", "missing event", "missing code"} {
		t.Run(defect, func(t *testing.T) {
			selection, actor, stored, root := setupSingleActorReplication(t)
			receiver, _ := os.Getwd()
			if err := os.Chdir(filepath.Join(root, "publisher")); err != nil {
				t.Fatal(err)
			}
			payload, encoded := stored.Payload, []byte(base64.RawStdEncoding.EncodeToString(stored.Signature))
			want := replicationStructuralInvalid
			switch defect {
			case "signature":
				encoded = []byte(base64.RawStdEncoding.EncodeToString(make([]byte, 64)))
			case "missing event", "missing code":
				e := newEvent(actor, "issue.comment", 1, "")
				e.Subject = "sha256:" + strings.Repeat("a", 64)
				e.Body = "absent dependency"
				if defect == "missing code" {
					e.Kind = "proposal.open"
					e.Subject = ""
					e.Body = ""
					e.Title = "absent code ref"
					e.Base = mustGitText(t, "rev-parse", "HEAD")
					mustGit(t, "commit", "--allow-empty", "-q", "-m", "candidate")
					e.Head = mustGitText(t, "rev-parse", "HEAD")
				}
				var sig []byte
				var err error
				payload, sig, err = encodeAndSign(e, actor)
				if err != nil {
					t.Fatal(err)
				}
				encoded = []byte(base64.RawStdEncoding.EncodeToString(sig))
				want = replicationDependencyMissing
			}
			mode := "100644"
			if defect == "mode" {
				mode = "100755"
			}
			entries := []memoryTreeFixture{{Mode: mode, Kind: "blob", Name: "event.json", OID: writeMemoryBlob(t, payload)}}
			if defect != "missing signature" {
				entries = append(entries, memoryTreeFixture{Mode: "100644", Kind: "blob", Name: "signature", OID: writeMemoryBlob(t, encoded)})
			}
			if defect == "extra attachment" {
				entries = append(entries, memoryTreeFixture{Mode: "100644", Kind: "blob", Name: "extra", OID: writeMemoryBlob(t, []byte("unbound"))})
			}
			var parents []string
			if defect == "parent" {
				parents = []string{mustGitText(t, "rev-parse", "HEAD")}
			}
			head := writeMemoryCommitFromEntries(t, entries, parents)
			mustGit(t, "update-ref", actorRef(actor.Actor), head)
			mustGit(t, "push", "-q", "--force", "origin", actorRef(actor.Actor)+":"+actorRef(actor.Actor))
			if err := os.Chdir(receiver); err != nil {
				t.Fatal(err)
			}
			result, err := runReplicationTransaction(selection)
			if err != nil {
				t.Fatal(err)
			}
			if defect == "valid" {
				if result.hasFailures() {
					t.Fatalf("valid import: %+v", result.Outcomes)
				}
				assertRefValue(t, acceptedActorRef("origin", actor.Actor), head)
				events, err := collectEvents()
				if err != nil || len(events) != 1 || events[0].ID != stored.ID {
					t.Fatalf("normal read: %d %v", len(events), err)
				}
			} else {
				if !replicationOutcomeHasStatus(result.Outcomes, replicationActor, actor.Actor, want) {
					t.Fatalf("want %s, got %+v", want, result.Outcomes)
				}
				assertRefAbsent(t, acceptedActorRef("origin", actor.Actor))
			}
		})
	}
}

// This is a subprocess-cost requirement, separate from behavioral acceptance.
func TestQuarantineBatchOrdinaryImportBlobProcessBound(t *testing.T) {
	selection, actor, stored, _ := setupSingleActorReplication(t)
	trace := filepath.Join(t.TempDir(), "trace.jsonl")
	t.Setenv("GIT_TRACE2_EVENT", trace)
	result, err := runReplicationTransaction(selection)
	if err != nil || result.hasFailures() {
		t.Fatalf("ordinary import: %+v %v", result.Outcomes, err)
	}
	assertRefValue(t, acceptedActorRef("origin", actor.Actor), stored.Commit)
	raw, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	shows := 0
	for _, line := range strings.Split(string(raw), "\n") {
		var record struct {
			Event string   `json:"event"`
			Argv  []string `json:"argv"`
		}
		if json.Unmarshal([]byte(line), &record) != nil || record.Event != "start" {
			continue
		}
		for _, arg := range record.Argv {
			if strings.HasSuffix(arg, ":event.json") || strings.HasSuffix(arg, ":signature") {
				shows++
			}
		}
	}
	if shows != 0 {
		t.Fatalf("ordinary quarantine import launched %d per-blob Git processes; want zero", shows)
	}
}

func TestQuarantineBatchOrdinaryFallback(t *testing.T) {
	for _, broken := range []string{"malformed", "truncated", "child exit", "signature"} {
		t.Run(broken, func(t *testing.T) {
			selection, actor, stored, root := setupSingleActorReplication(t)
			if broken == "signature" {
				receiver, _ := os.Getwd()
				if err := os.Chdir(filepath.Join(root, "publisher")); err != nil {
					t.Fatal(err)
				}
				head := writeMemoryCommitFromEntries(t, []memoryTreeFixture{{Mode: "100644", Kind: "blob", Name: "event.json", OID: writeMemoryBlob(t, stored.Payload)}, {Mode: "100644", Kind: "blob", Name: "signature", OID: writeMemoryBlob(t, []byte("bad"))}}, nil)
				mustGit(t, "update-ref", actorRef(actor.Actor), head)
				mustGit(t, "push", "-q", "--force", "origin", actorRef(actor.Actor)+":"+actorRef(actor.Actor))
				if err := os.Chdir(receiver); err != nil {
					t.Fatal(err)
				}
			}
			shim := t.TempDir()
			seen := filepath.Join(shim, "seen")
			reply := "printf 'broken\\n'; exit 0"
			if broken == "truncated" {
				reply = "printf 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa blob 123\\nshort'; exit 0"
			}
			if broken == "child exit" {
				reply = "exit 7"
			}
			script := "#!/bin/sh\nfor a in \"$@\"; do if [ \"$a\" = --batch ]; then echo hit >> '" + seen + "'; " + reply + "; fi; done\nexec /usr/bin/git \"$@\"\n"
			if err := os.WriteFile(filepath.Join(shim, "git"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", shim+string(os.PathListSeparator)+os.Getenv("PATH"))
			result, err := runReplicationTransaction(selection)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(seen); err != nil {
				t.Fatalf("batch boundary injection unused: %v", err)
			}
			if broken == "signature" {
				if !result.hasFailures() {
					t.Fatal("fallback accepted corrupt signature")
				}
				assertRefAbsent(t, acceptedActorRef("origin", actor.Actor))
				return
			}
			if result.hasFailures() {
				t.Fatalf("fallback lost ordinary valid import: %+v", result.Outcomes)
			}
			assertRefValue(t, acceptedActorRef("origin", actor.Actor), stored.Commit)
		})
	}
}

func TestQuarantineBatchOrdinaryOversizeFallback(t *testing.T) {
	for _, ceiling := range []string{"aggregate", "object"} {
		t.Run(ceiling, func(t *testing.T) {
			selection, actor, first, root := setupSingleActorReplication(t)
			receiver, _ := os.Getwd()
			if err := os.Chdir(filepath.Join(root, "publisher")); err != nil {
				t.Fatal(err)
			}
			last := first
			count := 6
			size := 900000
			if ceiling == "object" {
				count = 1
				size = 1100000
			}
			for i := 0; i < count; i++ {
				e, err := nextEvent(actor, "issue.open")
				if err != nil {
					t.Fatal(err)
				}
				e.Title = "large legacy payload"
				e.Body = strings.Repeat("x", size)
				last, err = appendEvent(e, actor)
				if err != nil {
					t.Fatal(err)
				}
			}
			mustGit(t, "push", "-q", "origin", actorRef(actor.Actor)+":"+actorRef(actor.Actor))
			if err := os.Chdir(receiver); err != nil {
				t.Fatal(err)
			}
			result, err := runReplicationTransaction(selection)
			if err != nil || result.hasFailures() {
				t.Fatalf("prefetch ceilings changed ordinary valid payload contract: %+v %v", result.Outcomes, err)
			}
			assertRefValue(t, acceptedActorRef("origin", actor.Actor), last.Commit)
			events, err := collectEvents()
			if err != nil || len(events) != count+1 {
				t.Fatalf("large payload normal read: %d %v", len(events), err)
			}

		})
	}
}
