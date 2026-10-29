package main

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
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

// An audit pack must end in the checksum of its own object format, as git
// index-pack requires in a repository of that format. For SHA-1 and
// SHA-256 alike, a pack whose trailer is the valid checksum of the other
// format is refused by Git, verifyAuditPack, loadStoredEventAt and the
// batched reader, which names the fact, and the pack with its own trailer
// is accepted by all four.
func TestIssueReadAuditPackTrailerFormatParity(t *testing.T) {
	for _, format := range []string{"sha1", "sha256"} {
		t.Run(format, func(t *testing.T) {
			t.Setenv("GIT_DEFAULT_HASH", format)
			_, id, at := auditFixture(t)
			auth := auditAuthorizeFixture(t, id, at)
			request, err := AuditRequest(at, "check", "", id)
			if err != nil {
				t.Fatal(err)
			}
			pack := request.Attachments["audit.pack"]
			body := pack[:len(pack)-len(at)/2]
			var other []byte
			if format == "sha1" {
				sum := sha256.Sum256(body)
				other = sum[:]
			} else {
				sum := sha1.Sum(body)
				other = sum[:]
			}
			wrong := append(slices.Clone(body), other...)
			gitIndex := func(candidate []byte) error {
				dir := t.TempDir()
				if out, err := exec.Command("git", "init", "--bare", "--quiet", "--object-format="+format, dir).CombinedOutput(); err != nil {
					t.Fatalf("git init: %v: %s", err, out)
				}
				cmd := exec.Command("git", "--git-dir="+dir, "index-pack", "--stdin", "--fsck-objects")
				cmd.Stdin = bytes.NewReader(candidate)
				if out, err := cmd.CombinedOutput(); err != nil {
					return fmt.Errorf("%v: %s", err, out)
				}
				return nil
			}
			if err := gitIndex(pack); err != nil {
				t.Fatalf("Git refuses the pack with its own trailer: %v", err)
			}
			if err := gitIndex(wrong); err == nil {
				t.Fatal("Git accepts the pack with the other format's trailer")
			}
			if err := verifyAuditPack(pack, request.Event); err != nil {
				t.Fatalf("verifyAuditPack refuses the pack with its own trailer: %v", err)
			}
			if err := verifyAuditPack(wrong, request.Event); err == nil {
				t.Fatal("verifyAuditPack accepts the pack with the other format's trailer")
			}
			if _, err := loadStoredEventAt("", request.Commit); err != nil {
				t.Fatalf("loadStoredEventAt refuses the pack with its own trailer: %v", err)
			}
			bad := serveAuditVariant(t, request.Commit, map[string][]byte{"audit.pack": wrong})
			if _, err := loadStoredEventAt("", bad); err == nil {
				t.Fatal("loadStoredEventAt accepts the pack with the other format's trailer")
			}
			if _, err := issueReadStoredEvents([]string{auth.Commit, request.Commit}, defaultIssueReadLimits()); err != nil {
				t.Fatalf("the batched reader refuses the pack with its own trailer: %v", err)
			}
			var readErr *IssueReadError
			_, err = issueReadStoredEvents([]string{auth.Commit, bad}, defaultIssueReadLimits())
			if !errors.As(err, &readErr) || readErr.Code != "invalid_history" || !strings.Contains(err.Error(), "audit "+shortID(request.ID)+" ") {
				t.Fatalf("the batched reader gave %v for the other format's trailer, want invalid_history naming %s", err, shortID(request.ID))
			}
		})
	}
}

// auditAmplifiedPacks is the reviewer's round-2 case: twelve valid audit
// packs of about 9 KB each, whose code commits carry 8 MiB messages that
// zlib compresses about a thousandfold.
const (
	auditAmplifiedPacks   = 12
	auditAmplifiedMessage = 8 << 20
)

// A read of many small, valid, highly compressed audit packs keeps the
// decoded commits and trees of one pack at a time: what verifyAuditPacks
// keeps for twelve packs that decode to 96 MiB of commits is smaller than
// one decoded pack. The read also counts the bytes every audit event's
// pack expands to against its budget: at exactly the expanded total the
// read succeeds, and one byte below it the read is refused as
// resource_limit before the crossing object is inflated.
func TestIssueReadAuditAmplifiedPacksStayBounded(t *testing.T) {
	_, id, at := auditFixture(t)
	auth := auditAuthorizeFixture(t, id, at)
	mustGit(t, "switch", "-qc", "amplified")
	messagePath := filepath.Join(t.TempDir(), "message")
	filler := strings.Repeat("a", auditAmplifiedMessage)
	var targets []string
	for i := 0; i < auditAmplifiedPacks; i++ {
		if err := os.WriteFile(messagePath, []byte(fmt.Sprintf("amplified %d\n\n%s", i, filler)), 0600); err != nil {
			t.Fatal(err)
		}
		mustGit(t, "commit", "-q", "--allow-empty", "--cleanup=verbatim", "-F", messagePath)
		targets = append(targets, mustGitText(t, "rev-parse", "HEAD"))
	}
	mustGit(t, "switch", "-q", "main")
	commits := []string{auth.Commit}
	facts := []auditPackFact{{Pack: auth.Attachments["audit.pack"], Event: auth.Event}}
	compressed := int64(0)
	for _, target := range targets {
		request, err := AuditRequest(target, "check", "", id)
		if err != nil {
			t.Fatal(err)
		}
		commits = append(commits, request.Commit)
		facts = append(facts, auditPackFact{Pack: request.Attachments["audit.pack"], Event: request.Event})
		compressed += int64(len(request.Attachments["audit.pack"]))
	}
	structural, expanded := int64(0), int64(0)
	for _, fact := range facts {
		decoded, err := decodeAuditSnapshot(fact.Pack, len(at))
		if err != nil {
			t.Fatal(err)
		}
		for _, object := range decoded.objects {
			structural += int64(len(object.content))
			expanded += object.size
		}
	}
	if structural < auditAmplifiedPacks*auditAmplifiedMessage {
		t.Fatalf("the packs decode to %d bytes of commits and trees, want at least %d", structural, auditAmplifiedPacks*auditAmplifiedMessage)
	}

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	snapshots, failed, err := verifyAuditPacks(facts)
	if err != nil {
		t.Fatalf("fact %d: %v", failed, err)
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	retained := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	runtime.KeepAlive(snapshots)
	t.Logf("packs=%d compressed=%d decoded commits and trees=%d expanded=%d retained by verifyAuditPacks=%d", auditAmplifiedPacks, compressed, structural, expanded, retained)
	if retained >= auditAmplifiedMessage {
		t.Fatalf("verifyAuditPacks retains %d bytes for %d packs, at least one decoded pack of %d", retained, auditAmplifiedPacks, auditAmplifiedMessage)
	}

	exact := defaultIssueReadLimits()
	exact.auditExpanded = expanded
	if _, err := issueReadStoredEvents(commits, exact); err != nil {
		t.Fatalf("the read at exactly its expansion budget: %v", err)
	}
	below := exact
	below.auditExpanded = expanded - 1
	var readErr *IssueReadError
	_, err = issueReadStoredEvents(commits, below)
	if !errors.As(err, &readErr) || readErr.Code != "resource_limit" || !strings.Contains(err.Error(), fmt.Sprintf("past its %d-byte budget", expanded-1)) {
		t.Fatalf("the read one byte below its expansion budget gave %v, want resource_limit", err)
	}
}
