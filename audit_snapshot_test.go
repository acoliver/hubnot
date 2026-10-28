package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// countGitProcesses puts a git wrapper first on PATH that logs each
// invocation and returns a function that runs read and returns the
// logged command lines.
func countGitProcesses(t *testing.T) func(read func()) []string {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	wrapper := t.TempDir()
	logPath := filepath.Join(wrapper, "calls")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$HN_TEST_AUDIT_CALLS\"\nexec \"$HN_TEST_REAL_GIT\" \"$@\"\n"
	if err := os.WriteFile(filepath.Join(wrapper, "git"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HN_TEST_REAL_GIT", realGit)
	t.Setenv("HN_TEST_AUDIT_CALLS", logPath)
	t.Setenv("PATH", wrapper+string(os.PathListSeparator)+os.Getenv("PATH"))
	return func(read func()) []string {
		if err := os.WriteFile(logPath, nil, 0600); err != nil {
			t.Fatal(err)
		}
		read()
		calls, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Split(strings.TrimSuffix(string(calls), "\n"), "\n")
	}
}

// The batched reader verifies a hosted audit history read-scoped: one
// audit and twenty audits, over distinct and repeated snapshots, cost the
// same Git processes, with one private temporary repository and no
// per-fact show, rev-parse, ls-tree or rev-list. The attachments equal
// what loadStoredEventAt returns for each fact.
func TestIssueReadAuditHistoryProcessCount(t *testing.T) {
	_, id, at := auditFixture(t)
	auth := auditAuthorizeFixture(t, id, at)
	mustGit(t, "switch", "-qc", "audited")
	var targets []string
	for i := 0; i < 10; i++ {
		if err := os.MkdirAll(filepath.Join("src", fmt.Sprint(i%3)), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join("src", fmt.Sprint(i%3), fmt.Sprintf("file-%d", i)), []byte(fmt.Sprintf("audited content %d\n", i)), 0644); err != nil {
			t.Fatal(err)
		}
		mustGit(t, "add", "src")
		mustGit(t, "commit", "-qm", fmt.Sprintf("audited %d", i))
		targets = append(targets, mustGitText(t, "rev-parse", "HEAD"))
	}
	mustGit(t, "switch", "-q", "main")
	commits := []string{auth.Commit}
	for i := 0; len(commits) < 20; i++ {
		// Distinct snapshots first, then repeats of the first ones.
		request, err := AuditRequest(targets[i%len(targets)], "check", "", id)
		if err != nil {
			t.Fatal(err)
		}
		commits = append(commits, request.Commit)
	}
	record := countGitProcesses(t)
	var batched []StoredEvent
	counts := map[int][]string{}
	for _, n := range []int{1, len(commits)} {
		counts[n] = record(func() {
			var err error
			if batched, err = issueReadStoredEvents(commits[:n], defaultIssueReadLimits()); err != nil {
				t.Fatal(err)
			}
		})
	}
	small, large := counts[1], counts[len(commits)]
	if len(large) != len(small) {
		t.Fatalf("Git processes grow with audit history: 1 audit %d %q, %d audits %d %q", len(small), small, len(commits), len(large), large)
	}
	inits := 0
	for _, call := range large {
		if strings.Contains(call, " init ") {
			inits++
		}
		for _, perFact := range []string{"show ", "rev-parse ", "ls-tree ", "rev-list "} {
			if strings.HasPrefix(call, perFact) {
				t.Fatalf("per-fact Git command %q in the batched read", call)
			}
		}
	}
	if inits != 1 {
		t.Fatalf("%d temporary repositories for one read, want 1: %q", inits, large)
	}
	t.Logf("1 audit: %d Git processes; %d audits: %d Git processes", len(small), len(commits), len(large))
	for index, commit := range commits {
		reference, err := loadStoredEventAt("", commit)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(batched[index].Attachments, reference.Attachments) {
			t.Fatalf("batched attachments of %s differ from loadStoredEventAt's", commit)
		}
	}
}

// Batching keeps every pack's own checks: a pack that lacks an object
// another pack of the read holds, a pack with an extra object and a pack
// only Git's object syntax check refuses are each refused, naming that
// fact, however many valid facts share the read and its snapshots.
func TestIssueReadAuditBatchKeepsPerPackChecks(t *testing.T) {
	_, id, at := auditFixture(t)
	auth := auditAuthorizeFixture(t, id, at)
	first, err := AuditRequest(at, "check", "", id)
	if err != nil {
		t.Fatal(err)
	}
	second, err := AuditRequest(at, "check", "", id)
	if err != nil {
		t.Fatal(err)
	}
	pack := second.Attachments["audit.pack"]
	ids, err := auditPackObjectIDs(pack, len(at))
	if err != nil {
		t.Fatal(err)
	}
	policyBlob := mustGitText(t, "rev-parse", at+":.hn/policy.json")
	lacking, err := gitInput([]byte(strings.Join(slices.DeleteFunc(slices.Clone(ids), func(id string) bool { return id == policyBlob }), "\n")+"\n"), nil, "pack-objects", "--stdout", "--window=0", "--depth=0")
	if err != nil {
		t.Fatal(err)
	}
	extra := mustGitTextFromInput(t, []byte("an object no signed snapshot names\n"), "hash-object", "-w", "--stdin")
	withExtra, err := gitInput([]byte(strings.Join(append(slices.Clone(ids), extra), "\n")+"\n"), nil, "pack-objects", "--stdout", "--window=0", "--depth=0")
	if err != nil {
		t.Fatal(err)
	}
	valid := []string{auth.Commit, first.Commit}
	for name, variant := range map[string][]byte{"lacking an object": lacking, "extra object": withExtra} {
		bad := serveAuditVariant(t, second.Commit, map[string][]byte{"audit.pack": variant})
		if _, err := loadStoredEventAt("", bad); err == nil {
			t.Fatalf("%s: loadStoredEventAt accepted the pack", name)
		}
		var readErr *IssueReadError
		_, err := issueReadStoredEvents(append(slices.Clone(valid), bad), defaultIssueReadLimits())
		if !errors.As(err, &readErr) || readErr.Code != "invalid_history" || !strings.Contains(err.Error(), "audit "+shortID(second.ID)+" ") {
			t.Fatalf("%s: batched reader gave %v, want invalid_history naming %s", name, err, shortID(second.ID))
		}
	}

	// A signed request whose code commit only Git's object check refuses,
	// read after valid facts whose packs share its tree.
	_, malformedFact := auditMalformedCommitPack(t, auth)
	e, err := nextEvent(id, "audit.request")
	if err != nil {
		t.Fatal(err)
	}
	e.Audit = &AuditFact{Scope: "project", Rule: "check", PolicyCommit: at, PolicyDigest: auth.Event.Audit.PolicyDigest, Authorization: auth.ID, CodeCommit: malformedFact.Audit.PolicyCommit, DefinitionDigest: auth.Event.Audit.DefinitionDigest, Intent: "inspect"}
	attachments, err := auditAttachments(e, nil)
	if err != nil {
		t.Fatal(err)
	}
	hostile := appendRawSignedEventCommit(t, id, e, second.Commit, attachments)
	if _, err := loadStoredEventAt("", hostile.Commit); err == nil {
		t.Fatal("loadStoredEventAt accepted a malformed snapshot commit")
	}
	var readErr *IssueReadError
	_, err = issueReadStoredEvents(append(slices.Clone(valid), second.Commit, hostile.Commit), defaultIssueReadLimits())
	if !errors.As(err, &readErr) || readErr.Code != "invalid_history" || !strings.Contains(err.Error(), "audit "+shortID(hostile.ID)+" ") || !strings.Contains(err.Error(), "index-pack") {
		t.Fatalf("batched reader gave %v, want an index-pack refusal naming %s", err, shortID(hostile.ID))
	}
	if _, err := issueReadStoredEvents(append(slices.Clone(valid), second.Commit), defaultIssueReadLimits()); err != nil {
		t.Fatalf("the valid facts no longer read: %v", err)
	}
}

// The parser-based closure verification computes equals the closure the
// writer computes with rev-parse, ls-tree and rev-list, for a snapshot
// with nested and repeated trees, an executable, a symbolic link and a
// gitlink, in both object formats.
func TestAuditSnapshotClosureMatchesGit(t *testing.T) {
	for _, format := range []string{"sha1", "sha256"} {
		t.Run(format, func(t *testing.T) {
			t.Setenv("GIT_DEFAULT_HASH", format)
			_, id, at := auditFixture(t)
			auth := auditAuthorizeFixture(t, id, at)
			mustGit(t, "switch", "-qc", "snapshot")
			for _, dir := range []string{"a/deep", "b/deep"} {
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "same"), []byte("repeated tree\n"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile("run", []byte("#!/bin/sh\n"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("a/deep/same", "link"); err != nil {
				t.Fatal(err)
			}
			mustGit(t, "add", "a", "b", "run", "link")
			mustGit(t, "update-index", "--add", "--cacheinfo", "160000,"+at+",module")
			mustGit(t, "commit", "-qm", "snapshot shapes")
			target := mustGitText(t, "rev-parse", "HEAD")
			mustGit(t, "switch", "-q", "main")
			e, err := nextEvent(id, "audit.request")
			if err != nil {
				t.Fatal(err)
			}
			e.Audit = &AuditFact{Scope: "project", Rule: "check", PolicyCommit: at, PolicyDigest: auth.Event.Audit.PolicyDigest, Authorization: auth.ID, CodeCommit: target, DefinitionDigest: auth.Event.Audit.DefinitionDigest, Intent: "inspect"}
			for _, event := range []Event{auth.Event, e} {
				want, err := auditDependencyObjectIDsAt("", event)
				if err != nil {
					t.Fatal(err)
				}
				attachments, err := auditAttachments(event, nil)
				if err != nil {
					t.Fatal(err)
				}
				snapshot, err := decodeAuditSnapshot(attachments["audit.pack"], len(at))
				if err != nil {
					t.Fatal(err)
				}
				got, err := auditSnapshotDependencies(snapshot, event)
				if err != nil {
					t.Fatal(err)
				}
				if !slices.Equal(got, want) {
					t.Fatalf("%s closure %q, Git's %q", event.Kind, got, want)
				}
				if err := verifyAuditPack(attachments["audit.pack"], event); err != nil {
					t.Fatalf("%s: %v", event.Kind, err)
				}
			}
		})
	}
}
