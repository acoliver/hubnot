package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// WP07: hook entry points and the push verdict framework. The black-box
// tests push through the SSH shim into a real hosted repository; the unit
// tests cover parsing, classification and the state writers.

// servePushFixture is a server hosting r, seeded from a source repository
// whose main policy names M as the only maintainer, with three bound
// participants A, B and M, each with a clone of r.
type servePushFixture struct {
	t       *testing.T
	h       *serveHarness
	hosted  string
	actors  map[string]string
	keys    map[string]serveTestKey
	clients map[string]serveHarnessClientEnv
	work    map[string]string
	commits int
}

func newServePushFixture(t *testing.T) *servePushFixture {
	t.Helper()
	h := newServeHarness(t)
	f := &servePushFixture{
		t:       t,
		h:       h,
		hosted:  h.Layout().RepoGitDir("r"),
		actors:  map[string]string{},
		keys:    map[string]serveTestKey{},
		clients: map[string]serveHarnessClientEnv{},
		work:    map[string]string{},
	}
	for _, name := range []string{"A", "B", "M"} {
		f.actors[name] = serveHarnessActor("push-" + name)
	}
	h.Serve("serve", "repo", "create", "r", "--from", serveHarnessSourceRepo(t, f.actors["M"]))
	for _, name := range []string{"A", "B", "M"} {
		f.keys[name] = serveHarnessAddKey(h, f.actors[name])
		f.clients[name] = serveHarnessClient(t, h, f.keys[name], nil)
		f.work[name] = filepath.Join(t.TempDir(), "work-"+name)
		f.clients[name].Git("", "clone", "-q", serveHarnessCloneURL("r"), f.work[name])
	}
	if tip := f.hostedRef("refs/heads/main"); len(tip) != len(servePushZero) {
		t.Fatalf("hosted main %q is not a SHA-1 object ID; the fixtures assume SHA-1", tip)
	}
	return f
}

// git runs git in who's clone.
func (f *servePushFixture) git(who string, args ...string) string {
	f.t.Helper()
	return strings.TrimSpace(f.clients[who].Git(f.work[who], args...))
}

// commitOn fetches, points a local branch at start and adds one commit with
// a unique file, returning the new commit ID.
func (f *servePushFixture) commitOn(who, start string) string {
	f.t.Helper()
	f.git(who, "fetch", "-q", "origin")
	f.git(who, "checkout", "-q", "-B", "work", start)
	f.commits++
	name := fmt.Sprintf("change-%d", f.commits)
	writeServeTestFile(f.t, filepath.Join(f.work[who], name), name+"\n", 0o644)
	f.git(who, "add", name)
	f.git(who, "commit", "-q", "-m", name)
	return f.git(who, "rev-parse", "HEAD")
}

func (f *servePushFixture) refs() string {
	f.t.Helper()
	return serveHarnessGit(f.t, f.hosted, "for-each-ref", "--format=%(objectname) %(refname)")
}

func (f *servePushFixture) hostedRef(ref string) string {
	f.t.Helper()
	output, status := f.clients["A"].exec("", "git", "--git-dir="+f.hosted, "rev-parse", "--verify", "--quiet", ref)
	if status != 0 {
		return ""
	}
	return strings.TrimSpace(output)
}

// push runs git push in who's clone and requires it to succeed with no
// hn-serve/1 output.
func (f *servePushFixture) push(who string, args ...string) {
	f.t.Helper()
	output := f.clients[who].Git(f.work[who], append([]string{"push"}, args...)...)
	if strings.Contains(output, "hn-serve") {
		f.t.Fatalf("accepted push %v printed hn-serve output:\n%s", args, output)
	}
}

// reject runs a git push that must be rejected. It requires the hosted refs
// to be byte-identical before and after (SC-002), exactly one rejection
// line pair per entry of want in ref order, each attributed to the pusher
// (FR-006, FR-013), and returns the parsed rejections.
func (f *servePushFixture) reject(who string, want map[string]string, args ...string) []serveRejection {
	f.t.Helper()
	before := f.refs()
	output := f.clients[who].GitFailure(f.work[who], append([]string{"push"}, args...)...)
	if after := f.refs(); after != before {
		f.t.Fatalf("rejected push %v changed hosted refs:\nbefore\n%s\nafter\n%s", args, before, after)
	}
	return requireServePushRejections(f.t, output, f.actors[who], want)
}

func requireServePushRejections(t *testing.T, output, actor string, want map[string]string) []serveRejection {
	t.Helper()
	rejections, lines := extractServeRejections(output)
	if len(rejections) != len(want) || len(lines) != 2*len(want) {
		t.Fatalf("got %d rejections in %d hn-serve lines, want %d pairs:\n%s", len(rejections), len(lines), len(want), output)
	}
	refs := make([]string, 0, len(want))
	for ref := range want {
		refs = append(refs, ref)
	}
	slices.Sort(refs)
	for index, rejection := range rejections {
		if rejection.Ref != refs[index] || rejection.Code != want[refs[index]] {
			t.Fatalf("rejection %d is %s %s, want %s %s:\n%s", index, rejection.Ref, rejection.Code, refs[index], want[refs[index]], output)
		}
		if rejection.Actor != actor {
			t.Fatalf("rejection %d actor %s, want the pusher %s", index, rejection.Actor, actor)
		}
		if rejection.Sentence == "" {
			t.Fatalf("rejection %d has no sentence:\n%s", index, output)
		}
		if !strings.HasPrefix(lines[2*index], serveRejectionPrefix) || !strings.HasPrefix(lines[2*index+1], serveSentencePrefix) {
			t.Fatalf("lines %d and %d are not a rejection pair:\n%s", 2*index, 2*index+1, output)
		}
	}
	return rejections
}

func (f *servePushFixture) owners() serveBranchOwners {
	f.t.Helper()
	owners, err := loadServeBranchOwners(f.h.Layout(), "r")
	if err != nil {
		f.t.Fatal(err)
	}
	return owners
}

func (f *servePushFixture) pushLog() []servePushLogEntry {
	f.t.Helper()
	return readServePushLog(f.t, f.h.Layout().RepoPushLog("r"))
}

func readServePushLog(t *testing.T, path string) []servePushLogEntry {
	t.Helper()
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var entries []servePushLogEntry
	for _, line := range strings.Split(strings.TrimSuffix(string(contents), "\n"), "\n") {
		var entry servePushLogEntry
		if err := decodePrivateJSON([]byte(line), &entry, "push log line"); err != nil {
			t.Fatalf("push log line %q: %v", line, err)
		}
		if err := validateServePushLogEntry(entry); err != nil {
			t.Fatalf("push log line %q: %v", line, err)
		}
		entries = append(entries, entry)
	}
	return entries
}

// servePushLogWant is the part of a push log entry a test controls.
type servePushLogWant struct {
	who     string
	verdict string
	updates []servePushLogUpdate
	codes   []string
}

func (f *servePushFixture) requirePushLog(want []servePushLogWant) {
	f.t.Helper()
	entries := f.pushLog()
	if len(entries) != len(want) {
		f.t.Fatalf("push log has %d entries, want %d: %+v", len(entries), len(want), entries)
	}
	for index, entry := range entries {
		expected := want[index]
		codes := expected.codes
		if codes == nil {
			codes = []string{}
		}
		if entry.Actor != f.actors[expected.who] || entry.Key != f.keys[expected.who].Fingerprint ||
			entry.Verdict != expected.verdict || !reflect.DeepEqual(entry.Updates, expected.updates) || !reflect.DeepEqual(entry.Codes, codes) {
			f.t.Fatalf("push log entry %d = %+v\nwant actor %s key %s verdict %s updates %+v codes %q",
				index, entry, f.actors[expected.who], f.keys[expected.who].Fingerprint, expected.verdict, expected.updates, codes)
		}
		if !validServeTimestamp(entry.Time) {
			f.t.Fatalf("push log entry %d time %q", index, entry.Time)
		}
	}
}

var servePushZero = strings.Repeat("0", 40)

const (
	servePushFeature = "refs/heads/feature/b"
	servePushRelease = "refs/heads/release"
)

// T046 steps 1, 2, 3 and 10: ordinary-branch ownership (US1-6, FR-011),
// unowned branches, the invalid-policy case and push log attribution
// (FR-006).
func TestServePushBranchOwnershipAndPushLog(t *testing.T) {
	f := newServePushFixture(t)
	b := f.actors["B"]
	var log []servePushLogWant

	// US1-6: B creates feature/b and becomes its recorded owner.
	created := f.commitOn("B", "origin/main")
	f.push("B", "origin", "HEAD:"+servePushFeature)
	if got := f.hostedRef(servePushFeature); got != created {
		t.Fatalf("feature/b = %q, want %s", got, created)
	}
	owner, recorded := lookupServeBranchOwner(f.owners(), servePushFeature)
	if !recorded || owner.Owner != b || !validServeTimestamp(owner.Created) {
		t.Fatalf("owner record for feature/b = %+v, %v; want owner B", owner, recorded)
	}
	log = append(log, servePushLogWant{"B", servePushVerdictAccepted, []servePushLogUpdate{{servePushFeature, servePushZero, created}}, nil})

	// A cannot force-update or delete B's branch.
	forced := f.commitOn("A", "origin/main")
	rejections := f.reject("A", map[string]string{servePushFeature: serveReasonBranchNotOwner}, "--force", "origin", "HEAD:"+servePushFeature)
	if !reflect.DeepEqual(rejections[0].IDs, []string{b}) || rejections[0].Next != serveReasonDefaultNext(serveReasonBranchNotOwner) {
		t.Fatalf("branch-not-owner rejection %+v, want ids naming B", rejections[0])
	}
	if want := servePushFeature + " belongs to " + b + "; only its owner or a maintainer can force-update or delete it."; rejections[0].Sentence != want {
		t.Fatalf("sentence %q, want %q", rejections[0].Sentence, want)
	}
	log = append(log, servePushLogWant{"A", servePushVerdictRejected, []servePushLogUpdate{{servePushFeature, created, forced}}, []string{serveReasonBranchNotOwner}})
	f.reject("A", map[string]string{servePushFeature: serveReasonBranchNotOwner}, "origin", ":"+servePushFeature)
	log = append(log, servePushLogWant{"A", servePushVerdictRejected, []servePushLogUpdate{{servePushFeature, created, servePushZero}}, []string{serveReasonBranchNotOwner}})

	// The owner may force-update, and a maintainer may delete.
	rewritten := f.commitOn("B", "origin/main")
	f.push("B", "--force", "origin", "HEAD:"+servePushFeature)
	if got := f.hostedRef(servePushFeature); got != rewritten {
		t.Fatalf("feature/b after the owner's forced update = %q, want %s", got, rewritten)
	}
	log = append(log, servePushLogWant{"B", servePushVerdictAccepted, []servePushLogUpdate{{servePushFeature, created, rewritten}}, nil})
	if owner, _ := lookupServeBranchOwner(f.owners(), servePushFeature); owner.Owner != b {
		t.Fatalf("a forced update changed the owner record to %+v", owner)
	}
	f.push("M", "origin", ":"+servePushFeature)
	if got := f.hostedRef(servePushFeature); got != "" {
		t.Fatalf("feature/b still exists at %s after the maintainer deleted it", got)
	}
	if owners := f.owners(); len(owners.Branches) != 0 {
		t.Fatalf("owner records after deletion = %+v, want none", owners.Branches)
	}
	log = append(log, servePushLogWant{"M", servePushVerdictAccepted, []servePushLogUpdate{{servePushFeature, rewritten, servePushZero}}, nil})
	// The empty-save policy keeps branches.json as an empty list.
	if contents, err := os.ReadFile(f.h.Layout().RepoBranches("r")); err != nil || string(contents) != "{\n  \"version\": 1,\n  \"branches\": []\n}\n" {
		t.Fatalf("branches.json after the last deletion = %q, %v", contents, err)
	}

	// release was imported at repo create and has no owner: anyone may
	// fast-forward it, only a maintainer may force it.
	releaseTip := f.hostedRef(servePushRelease)
	advanced := f.commitOn("A", "origin/release")
	f.push("A", "origin", "HEAD:"+servePushRelease)
	log = append(log, servePushLogWant{"A", servePushVerdictAccepted, []servePushLogUpdate{{servePushRelease, releaseTip, advanced}}, nil})
	if _, recorded := lookupServeBranchOwner(f.owners(), servePushRelease); recorded {
		t.Fatal("a fast-forward recorded an owner for an imported branch")
	}
	unowned := f.commitOn("A", "origin/main")
	rejections = f.reject("A", map[string]string{servePushRelease: serveReasonBranchNotOwner}, "--force", "origin", "HEAD:"+servePushRelease)
	if len(rejections[0].IDs) != 0 {
		t.Fatalf("an unowned branch rejection names ids %q", rejections[0].IDs)
	}
	if want := servePushRelease + " has no recorded owner; only a maintainer can force-update or delete it."; rejections[0].Sentence != want {
		t.Fatalf("sentence %q, want %q", rejections[0].Sentence, want)
	}
	log = append(log, servePushLogWant{"A", servePushVerdictRejected, []servePushLogUpdate{{servePushRelease, advanced, unowned}}, []string{serveReasonBranchNotOwner}})
	byMaintainer := f.commitOn("M", "origin/main")
	f.push("M", "--force", "origin", "HEAD:"+servePushRelease)
	log = append(log, servePushLogWant{"M", servePushVerdictAccepted, []servePushLogUpdate{{servePushRelease, advanced, byMaintainer}}, nil})

	// With a malformed policy at the main tip nobody is a maintainer.
	f.git("A", "fetch", "-q", "origin")
	f.git("A", "checkout", "-q", "-B", "broken", "origin/main")
	writeServeTestFile(t, filepath.Join(f.work["A"], ".hn", "policy.json"), "{not json\n", 0o644)
	f.git("A", "commit", "-q", "-am", "malformed policy")
	broken := f.git("A", "rev-parse", "HEAD")
	f.push("A", "origin", "HEAD:refs/heads/broken")
	log = append(log, servePushLogWant{"A", servePushVerdictAccepted, []servePushLogUpdate{{"refs/heads/broken", servePushZero, broken}}, nil})
	f.h.Serve("serve", "repo", "lock-exec", "r", "--", "git", "update-ref", "refs/heads/main", broken)
	refused := f.commitOn("M", "origin/broken")
	f.reject("M", map[string]string{servePushRelease: serveReasonBranchNotOwner}, "--force", "origin", "HEAD:"+servePushRelease)
	log = append(log, servePushLogWant{"M", servePushVerdictRejected, []servePushLogUpdate{{servePushRelease, byMaintainer, refused}}, []string{serveReasonBranchNotOwner}})
	// A still owns broken, so A may force it while nobody is a maintainer.
	f.push("A", "--force", "origin", "HEAD~1:refs/heads/broken")
	log = append(log, servePushLogWant{"A", servePushVerdictAccepted, []servePushLogUpdate{{"refs/heads/broken", broken, f.git("A", "rev-parse", "HEAD~1")}}, nil})

	f.requirePushLog(log)
	if info, err := os.Lstat(f.h.Layout().RepoPushLog("r")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("push.log mode: %v %v", info, err)
	}
}

// T046 steps 4, 5 and 9: tags are create-only, every other ref is reserved,
// and the repository's own config cannot disable the hooks.
func TestServePushTagsAndReservedRefs(t *testing.T) {
	f := newServePushFixture(t)
	tip := f.commitOn("A", "origin/main")
	f.push("A", "origin", "HEAD:refs/tags/v2")
	if got := f.hostedRef("refs/tags/v2"); got != tip {
		t.Fatalf("refs/tags/v2 = %q, want %s", got, tip)
	}
	v1 := f.hostedRef("refs/tags/v1")
	rejections := f.reject("A", map[string]string{"refs/tags/v1": serveReasonTagImmutable}, "--force", "origin", "HEAD:refs/tags/v1")
	if !reflect.DeepEqual(rejections[0].IDs, []string{v1, tip}) {
		t.Fatalf("moved tag ids %q, want old and new", rejections[0].IDs)
	}
	rejections = f.reject("A", map[string]string{"refs/tags/v1": serveReasonTagImmutable}, "origin", ":refs/tags/v1")
	if !reflect.DeepEqual(rejections[0].IDs, []string{v1}) {
		t.Fatalf("deleted tag ids %q, want the old tag", rejections[0].IDs)
	}
	f.reject("B", map[string]string{"refs/tags/v2": serveReasonTagImmutable}, "origin", ":refs/tags/v2")

	for _, ref := range []string{serveMarkerRef, "refs/notes/commits", "refs/hn/other/x", "refs/hn/actors/short", "refs/hn/proposals/" + strings.Repeat("A", 64), "refs/remotes/origin/main"} {
		rejections := f.reject("A", map[string]string{ref: serveReasonReservedRef}, "--force", "origin", "HEAD:"+ref)
		if rejections[0].Next != "push branches, tags and Hubnot refs only" {
			t.Fatalf("%s next %q", ref, rejections[0].Next)
		}
	}
	rejections = f.reject("A", map[string]string{serveMarkerRef: serveReasonReservedRef}, "origin", ":"+serveMarkerRef)
	if want := serveMarkerRef + " is the server marker and cannot be pushed."; rejections[0].Sentence != want {
		t.Fatalf("marker sentence %q, want %q", rejections[0].Sentence, want)
	}

	// The hosted repository's own config cannot switch the hooks or object
	// checking off: hn serve git passes both with -c.
	f.h.Serve("serve", "repo", "lock-exec", "r", "--", "git", "config", "core.hooksPath", "/dev/null")
	f.h.Serve("serve", "repo", "lock-exec", "r", "--", "git", "config", "receive.fsckObjects", "false")
	f.reject("A", map[string]string{"refs/notes/commits": serveReasonReservedRef}, "origin", "HEAD:refs/notes/commits")
}

// T046 steps 6 and 7: one failing ref rejects the whole push, with and
// without --atomic, and the quarantined objects are discarded (FR-012).
func TestServePushAllOrNothing(t *testing.T) {
	f := newServePushFixture(t)
	for _, atomic := range []bool{false, true} {
		fresh := f.commitOn("A", "origin/main")
		args := []string{"origin", "HEAD:refs/heads/topic", "HEAD:refs/notes/commits"}
		if atomic {
			args = append([]string{"--atomic"}, args...)
		}
		f.reject("A", map[string]string{"refs/notes/commits": serveReasonReservedRef}, args...)
		if got := f.hostedRef("refs/heads/topic"); got != "" {
			t.Fatalf("atomic=%v: topic exists at %s after a rejected push", atomic, got)
		}
		if _, status := f.clients["A"].exec("", "git", "--git-dir="+f.hosted, "cat-file", "-e", fresh); status == 0 {
			t.Fatalf("atomic=%v: the rejected commit %s is in the hosted object database", atomic, fresh)
		}
	}
	// The same branch alone is accepted, so the rejection came from the
	// reserved ref only.
	f.push("A", "origin", "HEAD:refs/heads/topic")

	// Several failing refs produce one pair each, in ref order, whatever
	// their class order. B does not own A's topic.
	f.commitOn("B", "origin/main")
	f.reject("B", map[string]string{
		"refs/heads/topic":   serveReasonBranchNotOwner,
		"refs/hn/serve/v1":   serveReasonReservedRef,
		"refs/notes/commits": serveReasonReservedRef,
		"refs/tags/v1":       serveReasonTagImmutable,
	}, "--force", "origin", "HEAD:refs/tags/v1", "HEAD:refs/notes/commits", "HEAD:"+serveMarkerRef, "HEAD:refs/heads/topic", "HEAD:refs/heads/fine")
	if got := f.hostedRef("refs/heads/fine"); got != "" {
		t.Fatalf("fine exists at %s after a rejected push", got)
	}
	entries := f.pushLog()
	last := entries[len(entries)-1]
	if want := []string{serveReasonBranchNotOwner, serveReasonReservedRef, serveReasonReservedRef, serveReasonTagImmutable}; !reflect.DeepEqual(last.Codes, want) {
		t.Fatalf("push log codes %q, want %q in line order", last.Codes, want)
	}
	if len(last.Updates) != 5 {
		t.Fatalf("push log lists %d updates, want all 5: %+v", len(last.Updates), last.Updates)
	}
}

// T046 step 8: push options outside the two supported ones, and an
// hn-advance push of the wrong shape, reject every ref update.
func TestServePushOptions(t *testing.T) {
	f := newServePushFixture(t)
	f.commitOn("A", "origin/main")
	advance := "hn-advance=sha256:" + strings.Repeat("ab", 32)
	tip := strings.Repeat("a", len(f.hostedRef("refs/heads/main")))
	for _, tc := range []struct {
		name    string
		options []string
		refs    []string
	}{
		{"unknown option", []string{"foo=bar"}, []string{"refs/heads/one"}},
		{"unknown option on two refs", []string{"foo=bar"}, []string{"refs/heads/one", "refs/heads/two"}},
		{"advance with two refs", []string{advance}, []string{"refs/heads/one", "refs/heads/two"}},
		{"advance to an ordinary branch", []string{advance}, []string{"refs/heads/one"}},
		{"malformed expected tip", []string{"hn-expected-tip=zz"}, []string{"refs/heads/one"}},
		{"repeated expected tip", []string{"hn-expected-tip=" + tip, "hn-expected-tip=" + tip}, []string{"refs/heads/one"}},
		{"malformed advance", []string{"hn-advance=" + strings.Repeat("ab", 32)}, []string{"refs/heads/one"}},
		{"bare option", []string{"hn-advance"}, []string{"refs/heads/one"}},
	} {
		args := []string{}
		for _, option := range tc.options {
			args = append(args, "-o", option)
		}
		args = append(args, "origin")
		want := map[string]string{}
		for _, ref := range tc.refs {
			args = append(args, "HEAD:"+ref)
			want[ref] = serveReasonUnsupportedCommand
		}
		rejections := f.reject("A", want, args...)
		sentence := servePushOptionSentence
		if strings.HasPrefix(tc.name, "advance") {
			sentence = "hn-advance requires a push with exactly one ref update, to a protected branch."
		}
		for _, rejection := range rejections {
			if rejection.Sentence != sentence {
				t.Fatalf("%s: sentence %q, want %q", tc.name, rejection.Sentence, sentence)
			}
		}
	}
	// A well-formed hn-expected-tip on an ordinary branch is allowed.
	f.push("A", "-o", "hn-expected-tip="+tip, "origin", "HEAD:refs/heads/one")
}

// T041 and T046 step 11: the hook refuses to run outside a session it can
// trust, including when the key was removed while the push was running.
func TestServePushHookEntryChecks(t *testing.T) {
	f := newServePushFixture(t)
	layout := f.h.Layout()
	tip := f.hostedRef("refs/heads/main")
	stdin := tip + " " + strings.Repeat("1", 40) + " refs/heads/topic\n"
	base := map[string]string{
		"HN_SERVE_ACTOR":      f.actors["A"],
		"HN_SERVE_KEY":        f.keys["A"].Fingerprint,
		"HN_SERVE_REPOSITORY": "r",
		"GIT_DIR":             ".",
	}
	with := func(changes map[string]string) map[string]string {
		environment := map[string]string{}
		for name, value := range base {
			environment[name] = value
		}
		for name, value := range changes {
			if value == "<unset>" {
				delete(environment, name)
				continue
			}
			environment[name] = value
		}
		return environment
	}
	other := filepath.Join(t.TempDir(), "other.git")
	runOperationalGit(t, "", "init", "-q", "--bare", other)
	linked := filepath.Join(t.TempDir(), "linked.git")
	if err := os.Symlink(f.hosted, linked); err != nil {
		t.Fatal(err)
	}
	refused := []struct {
		name    string
		env     map[string]string
		dir     string
		message string
	}{
		{"no actor", with(map[string]string{"HN_SERVE_ACTOR": "<unset>"}), f.hosted, "HN_SERVE_ACTOR is not set"},
		{"no key", with(map[string]string{"HN_SERVE_KEY": "<unset>"}), f.hosted, "HN_SERVE_KEY is not set"},
		{"no repository", with(map[string]string{"HN_SERVE_REPOSITORY": "<unset>"}), f.hosted, "HN_SERVE_REPOSITORY is not set"},
		{"short actor", with(map[string]string{"HN_SERVE_ACTOR": "abc"}), f.hosted, "HN_SERVE_ACTOR is not a full"},
		{"malformed key", with(map[string]string{"HN_SERVE_KEY": "SHA256:short"}), f.hosted, "HN_SERVE_KEY is not a SHA256"},
		{"unsafe repository", with(map[string]string{"HN_SERVE_REPOSITORY": "../r"}), f.hosted, "HN_SERVE_REPOSITORY is not a valid"},
		{"no GIT_DIR", with(map[string]string{"GIT_DIR": "<unset>"}), f.hosted, "GIT_DIR is not set"},
		{"other GIT_DIR", with(map[string]string{"GIT_DIR": other}), f.hosted, "is not the hosted repository"},
		{"relative GIT_DIR elsewhere", with(nil), other, "is not the hosted repository"},
		{"unhosted repository", with(map[string]string{"HN_SERVE_REPOSITORY": "missing"}), f.hosted, "cannot resolve hosted repository"},
		{"key bound to another actor", with(map[string]string{"HN_SERVE_ACTOR": f.actors["B"]}), f.hosted, "is no longer bound to actor"},
		{"unknown key", with(map[string]string{"HN_SERVE_KEY": newServeTestKey(t, "").Fingerprint}), f.hosted, "is no longer registered"},
	}
	for _, tc := range refused {
		output, status := f.h.Exec(serveHarnessRun{Env: tc.env, Stdin: stdin, Dir: tc.dir}, "serve", "hook", "pre-receive")
		if status != 1 || !strings.HasPrefix(output, "hn: hn serve hook: ") || !strings.Contains(output, tc.message) {
			t.Fatalf("%s: exit %d output %q, want exit 1 and hn: hn serve hook: ...%s", tc.name, status, output, tc.message)
		}
	}
	// A symlink to the hosted repository resolves to it and is accepted;
	// the reserved ref then gives an ordinary rejection.
	output, status := f.h.Exec(serveHarnessRun{Env: with(map[string]string{"GIT_DIR": linked}), Stdin: tip + " " + strings.Repeat("1", 40) + " refs/notes/x\n", Dir: f.hosted}, "serve", "hook", "pre-receive")
	if status != 1 {
		t.Fatalf("symlinked GIT_DIR: exit %d output %q", status, output)
	}
	requireServePushRejections(t, output, f.actors["A"], map[string]string{"refs/notes/x": serveReasonReservedRef})
	// A creation passes and prints nothing.
	if output, status := f.h.Exec(serveHarnessRun{Env: with(nil), Stdin: servePushZero + " " + tip + " refs/heads/created\n", Dir: f.hosted}, "serve", "hook", "pre-receive"); status != 0 || output != "" {
		t.Fatalf("accepted creation: exit %d output %q", status, output)
	}
	// Malformed input rejects the push.
	for _, input := range []string{
		tip + "  " + tip + " refs/heads/x\n",
		strings.ToUpper(tip) + " " + tip + " refs/heads/x\n",
		tip + " " + tip + " refs/heads/x",
		tip[:39] + " " + tip + " refs/heads/x\n",
		servePushZero + " " + servePushZero + " refs/heads/x\n",
	} {
		output, status := f.h.Exec(serveHarnessRun{Env: with(nil), Stdin: input, Dir: f.hosted}, "serve", "hook", "pre-receive")
		if status != 1 || !strings.HasPrefix(output, "hn: hn serve hook: ") {
			t.Fatalf("malformed input %q: exit %d output %q", input, status, output)
		}
	}
	output, status = f.h.Exec(serveHarnessRun{Env: with(nil), Dir: f.hosted}, "serve", "hook", "update")
	if status != 1 || output != "hn: "+serveHookUsage+"\n" {
		t.Fatalf("unknown hook: exit %d output %q", status, output)
	}

	// The key removed while a push runs: the hook sees the session's
	// environment but the registry no longer binds the key.
	f.h.Serve("serve", "key", "remove", f.keys["A"].Fingerprint)
	output, status = f.h.Exec(serveHarnessRun{Env: with(nil), Stdin: stdin, Dir: f.hosted}, "serve", "hook", "pre-receive")
	if status != 1 || !strings.Contains(output, "hn: hn serve hook: key "+f.keys["A"].Fingerprint+" is no longer registered") {
		t.Fatalf("removed key: exit %d output %q", status, output)
	}
	// post-receive never fails the push; it reports the bookkeeping failure.
	output, status = f.h.Exec(serveHarnessRun{Env: with(nil), Stdin: stdin, Dir: f.hosted}, "serve", "hook", "post-receive")
	if status != 0 || !strings.HasPrefix(output, "hn-serve: post-receive bookkeeping failed: hn serve hook: key ") {
		t.Fatalf("post-receive with a removed key: exit %d output %q", status, output)
	}
	if entries := readServePushLog(t, layout.RepoPushLog("r")); len(entries) != 1 || entries[0].Verdict != servePushVerdictRejected {
		t.Fatalf("push log after direct hook runs = %+v, want only the symlinked GIT_DIR rejection", entries)
	}
}

// post-receive records owners only for ordinary branches and keeps going
// when one part of the bookkeeping fails.
func TestServePushPostReceiveBookkeeping(t *testing.T) {
	f := newServePushFixture(t)
	layout := f.h.Layout()
	tip := f.hostedRef("refs/heads/main")
	env := map[string]string{
		"HN_SERVE_ACTOR":      f.actors["B"],
		"HN_SERVE_KEY":        f.keys["B"].Fingerprint,
		"HN_SERVE_REPOSITORY": "r",
		"GIT_DIR":             ".",
	}
	stdin := servePushZero + " " + tip + " refs/heads/main\n" +
		servePushZero + " " + tip + " refs/heads/new\n" +
		servePushZero + " " + tip + " refs/tags/t\n"
	output, status := f.h.Exec(serveHarnessRun{Env: env, Stdin: stdin, Dir: f.hosted}, "serve", "hook", "post-receive")
	if status != 0 || output != "" {
		t.Fatalf("post-receive: exit %d output %q", status, output)
	}
	owners := f.owners()
	if len(owners.Branches) != 1 || owners.Branches[0].Ref != "refs/heads/new" || owners.Branches[0].Owner != f.actors["B"] {
		t.Fatalf("owners = %+v, want only refs/heads/new owned by B", owners.Branches)
	}
	f.requirePushLog([]servePushLogWant{{"B", servePushVerdictAccepted, []servePushLogUpdate{
		{"refs/heads/main", servePushZero, tip}, {"refs/heads/new", servePushZero, tip}, {"refs/tags/t", servePushZero, tip},
	}, nil}})

	// A failing push log append is reported, exits 0 and does not stop the
	// owner record.
	if err := os.Truncate(layout.RepoPushLog("r"), servePushLogLimit-10); err != nil {
		t.Fatal(err)
	}
	output, status = f.h.Exec(serveHarnessRun{Env: env, Stdin: servePushZero + " " + tip + " refs/heads/other\n" + tip + " " + servePushZero + " refs/heads/new\n", Dir: f.hosted}, "serve", "hook", "post-receive")
	if status != 0 || !strings.HasPrefix(output, "hn-serve: post-receive bookkeeping failed: ") || !strings.Contains(output, "push log would exceed") || strings.Count(output, "\n") != 1 {
		t.Fatalf("post-receive with a full push log: exit %d output %q", status, output)
	}
	owners = f.owners()
	if len(owners.Branches) != 1 || owners.Branches[0].Ref != "refs/heads/other" {
		t.Fatalf("owners = %+v, want refs/heads/new removed and refs/heads/other recorded", owners.Branches)
	}
}

// T047: the guide's example is the real output of US1-6, field for field.
func TestServePushDocumentedExample(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("docs", "serve-v0.md"))
	if err != nil {
		t.Fatal(err)
	}
	const marker = "<!-- example: reject-branch-not-owner -->\n"
	guide := string(contents)
	if strings.Count(guide, marker) != 1 {
		t.Fatalf("the serve guide must contain exactly one %q", strings.TrimSpace(marker))
	}
	_, after, _ := strings.Cut(guide, marker)
	_, after, found := strings.Cut(after, "```text\n")
	if !found {
		t.Fatal("no text block after the example marker")
	}
	block, _, found := strings.Cut(after, "```\n")
	if !found {
		t.Fatal("unterminated example block")
	}
	documented := strings.Split(strings.TrimSuffix(block, "\n"), "\n")
	if len(documented) != 2 {
		t.Fatalf("example has %d lines, want the two relayed lines", len(documented))
	}
	documentedRejection, err := parseServeRejectionLine(serveCleanStderrLine(documented[0]))
	if err != nil {
		t.Fatalf("documented line does not parse: %v", err)
	}
	documentedPusher, documentedOwner := documentedRejection.Actor, documentedRejection.IDs[0]

	f := newServePushFixture(t)
	f.commitOn("B", "origin/main")
	f.push("B", "origin", "HEAD:"+servePushFeature)
	f.commitOn("A", "origin/main")
	output := f.clients["A"].GitFailure(f.work["A"], "push", "--force", "origin", "HEAD:"+servePushFeature)
	_, lines := extractServeRejections(output)
	want := make([]string, 0, 2)
	for _, line := range documented {
		line = strings.ReplaceAll(line, documentedPusher, f.actors["A"])
		line = strings.ReplaceAll(line, documentedOwner, f.actors["B"])
		if !strings.HasPrefix(line, "remote: ") {
			t.Fatalf("documented line %q does not show Git's remote: prefix", line)
		}
		want = append(want, serveCleanStderrLine(line))
	}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("relayed lines:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	got, err := parseServeRejectionLine(lines[0])
	if err != nil {
		t.Fatal(err)
	}
	expected, err := parseServeRejectionLine(want[0])
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("fields %+v, want %+v", got, expected)
	}
}

// Unit tests.

func TestServePushParseRefUpdates(t *testing.T) {
	old, new40 := strings.Repeat("a", 40), strings.Repeat("b", 40)
	updates, err := parseServeRefUpdates(strings.NewReader(old+" "+new40+" refs/heads/x\n"+servePushZero+" "+new40+" refs/tags/t\n"), 40)
	if err != nil {
		t.Fatal(err)
	}
	want := []serveRefUpdate{{Old: old, New: new40, Ref: "refs/heads/x"}, {Old: servePushZero, New: new40, Ref: "refs/tags/t"}}
	if !reflect.DeepEqual(updates, want) || !updates[1].Creates() || updates[0].Creates() || updates[0].Deletes() {
		t.Fatalf("updates = %+v", updates)
	}
	sha256Old := strings.Repeat("c", 64)
	if updates, err := parseServeRefUpdates(strings.NewReader(sha256Old+" "+strings.Repeat("0", 64)+" refs/heads/x\n"), 64); err != nil || !updates[0].Deletes() {
		t.Fatalf("sha256 deletion = %+v, %v", updates, err)
	}
	if updates, err := parseServeRefUpdates(strings.NewReader(""), 40); err != nil || len(updates) != 0 {
		t.Fatalf("empty input = %+v, %v", updates, err)
	}
	for _, input := range []string{
		old + " " + new40 + " refs/heads/x",
		old + " " + new40 + "\n",
		old + "  " + new40 + " refs/heads/x\n",
		old + " " + new40 + " refs/heads/x extra\n",
		old + "\t" + new40 + " refs/heads/x\n",
		strings.ToUpper(old) + " " + new40 + " refs/heads/x\n",
		old[:39] + " " + new40 + " refs/heads/x\n",
		sha256Old + " " + new40 + " refs/heads/x\n",
		servePushZero + " " + servePushZero + " refs/heads/x\n",
		old + " " + new40 + " \n",
		old + " " + new40 + " refs/heads/x\n" + old + " " + new40 + " refs/heads/x\n",
		"\n",
	} {
		if _, err := parseServeRefUpdates(strings.NewReader(input), 40); err == nil {
			t.Fatalf("parseServeRefUpdates(%q) accepted malformed input", input)
		}
	}

	many := numberedServeRefUpdates(old, new40, serveRefUpdateLineLimit+1)
	var budget *serveRefUpdateBudgetError
	if _, err := parseServeRefUpdates(strings.NewReader(many), 40); !errors.As(err, &budget) ||
		*budget != (serveRefUpdateBudgetError{Budget: serveRefUpdatesBudget, Limit: serveRefUpdateLineLimit, Measured: serveRefUpdateLineLimit + 1}) {
		t.Fatalf("10,001 lines: %v", err)
	}
	huge := old + " " + new40 + " refs/heads/" + strings.Repeat("x", serveRefUpdateByteLimit) + "\n"
	if _, err := parseServeRefUpdates(strings.NewReader(huge), 40); !errors.As(err, &budget) ||
		*budget != (serveRefUpdateBudgetError{Budget: serveRefUpdateBytesBudget, Limit: serveRefUpdateByteLimit, Measured: int64(len(huge))}) {
		t.Fatalf("over 4 MiB: %v", err)
	}
	if updates, err := parseServeRefUpdates(strings.NewReader(numberedServeRefUpdates(old, new40, serveRefUpdateLineLimit)), 40); err != nil || len(updates) != serveRefUpdateLineLimit {
		t.Fatalf("exactly 10,000 lines: %d updates, %v", len(updates), err)
	}
}

func numberedServeRefUpdates(oldOID, newOID string, count int) string {
	var lines strings.Builder
	for index := 0; index < count; index++ {
		fmt.Fprintf(&lines, "%s %s refs/heads/b%d\n", oldOID, newOID, index)
	}
	return lines.String()
}

func TestServePushParseOptions(t *testing.T) {
	advance := "sha256:" + strings.Repeat("ab", 32)
	tip := strings.Repeat("c", 40)
	for _, tc := range []struct {
		env  []string
		want servePushOptions
	}{
		{nil, servePushOptions{}},
		{[]string{"GIT_PUSH_OPTION_COUNT=0"}, servePushOptions{}},
		{[]string{"GIT_PUSH_OPTION_COUNT=1", "GIT_PUSH_OPTION_0=hn-advance=" + advance}, servePushOptions{Advance: advance}},
		{[]string{"GIT_PUSH_OPTION_COUNT=2", "GIT_PUSH_OPTION_0=hn-expected-tip=" + tip, "GIT_PUSH_OPTION_1=hn-advance=" + advance}, servePushOptions{Advance: advance, ExpectedTip: tip}},
	} {
		got, err := parseServePushOptions(tc.env, 40)
		if err != nil || got != tc.want {
			t.Fatalf("parseServePushOptions(%q) = %+v, %v; want %+v", tc.env, got, err, tc.want)
		}
	}
	for _, env := range [][]string{
		{"GIT_PUSH_OPTION_COUNT="},
		{"GIT_PUSH_OPTION_COUNT=-1"},
		{"GIT_PUSH_OPTION_COUNT=01", "GIT_PUSH_OPTION_0=hn-advance=" + advance},
		{"GIT_PUSH_OPTION_COUNT=3", "GIT_PUSH_OPTION_0=hn-advance=" + advance, "GIT_PUSH_OPTION_1=hn-expected-tip=" + tip, "GIT_PUSH_OPTION_2=x=y"},
		{"GIT_PUSH_OPTION_COUNT=1"},
		{"GIT_PUSH_OPTION_COUNT=1", "GIT_PUSH_OPTION_0=foo=bar"},
		{"GIT_PUSH_OPTION_COUNT=1", "GIT_PUSH_OPTION_0=hn-advance"},
		{"GIT_PUSH_OPTION_COUNT=1", "GIT_PUSH_OPTION_0=hn-advance=" + strings.ToUpper(advance)},
		{"GIT_PUSH_OPTION_COUNT=1", "GIT_PUSH_OPTION_0=hn-advance=sha256:" + strings.Repeat("AB", 32)},
		{"GIT_PUSH_OPTION_COUNT=1", "GIT_PUSH_OPTION_0=hn-advance=" + advance[:70]},
		{"GIT_PUSH_OPTION_COUNT=1", "GIT_PUSH_OPTION_0=hn-expected-tip=zz"},
		{"GIT_PUSH_OPTION_COUNT=1", "GIT_PUSH_OPTION_0=hn-expected-tip=" + strings.Repeat("c", 64)},
		{"GIT_PUSH_OPTION_COUNT=2", "GIT_PUSH_OPTION_0=hn-advance=" + advance, "GIT_PUSH_OPTION_1=hn-advance=" + advance},
	} {
		if got, err := parseServePushOptions(env, 40); err == nil {
			t.Fatalf("parseServePushOptions(%q) = %+v, want an error", env, got)
		}
	}
}

func TestServePushClassifiesRefs(t *testing.T) {
	config, err := newServeRepoConfig("r", []string{"refs/heads/main", "refs/heads/release"})
	if err != nil {
		t.Fatal(err)
	}
	actor := serveStateTestActor("classify")
	stream := strings.Repeat("d", 64)
	for ref, want := range map[string]serveRefClass{
		"refs/heads/main":                              serveRefProtected,
		"refs/heads/release":                           serveRefProtected,
		"refs/hn/actors/" + actor:                      serveRefActor,
		"refs/hn/actors/" + strings.ToUpper(actor):     serveRefReserved,
		"refs/hn/actors/short":                         serveRefReserved,
		"refs/hn/actors/" + actor + "/x":               serveRefReserved,
		"refs/hn/proposals/" + stream:                  serveRefProposal,
		"refs/hn/proposals/sha256:" + stream:           serveRefReserved,
		"refs/hn/memory/" + actor + "/" + stream:       serveRefMemory,
		"refs/hn/memory/" + actor:                      serveRefReserved,
		"refs/hn/memory/" + actor + "/" + stream + "x": serveRefReserved,
		"refs/tags/v1":                                 serveRefTag,
		"refs/tags/release/1.0":                        serveRefTag,
		"refs/heads/feature/a":                         serveRefBranch,
		"refs/heads/mainline":                          serveRefBranch,
		"refs/heads/x.lock":                            serveRefReserved,
		serveMarkerRef:                                 serveRefReserved,
		"refs/hn/serve/v2":                             serveRefReserved,
		"refs/hn/other/x":                              serveRefReserved,
		"refs/notes/commits":                           serveRefReserved,
		"refs/remotes/origin/main":                     serveRefReserved,
		"HEAD":                                         serveRefReserved,
	} {
		if got := classifyServeRef(config, ref); got != want {
			t.Errorf("classifyServeRef(%q) = %s, want %s", ref, got, want)
		}
	}
}

// evaluateServePush checks every update, sorts rejections by ref and
// attributes each to the mapped actor.
func TestServePushEvaluateOrdersRejectionsByRef(t *testing.T) {
	config, err := newServeRepoConfig("r", nil)
	if err != nil {
		t.Fatal(err)
	}
	actor := serveStateTestActor("evaluate")
	ctx := newServeVerdictContext(t.TempDir(), actor, serveTestFingerprint("evaluate"), config, emptyServeBranchOwners())
	ctx.loadHostedEvents = func() ([]StoredEvent, error) {
		t.Fatal("tags, branches and reserved refs must not load hosted events")
		return nil, nil
	}
	old, newer := strings.Repeat("1", 40), strings.Repeat("2", 40)
	updates := []serveRefUpdate{
		{Old: old, New: newer, Ref: "refs/tags/b"},
		{Old: servePushZero, New: newer, Ref: "refs/notes/z"},
		{Old: servePushZero, New: newer, Ref: "refs/tags/new"},
		{Old: servePushZero, New: newer, Ref: "refs/heads/created"},
		{Old: old, New: servePushZero, Ref: "refs/tags/a"},
		{Old: servePushZero, New: newer, Ref: "refs/hn/x"},
	}
	classifyServeRefUpdates(config, updates)
	rejections := evaluateServePush(ctx, updates, servePushOptions{})
	var got []string
	for _, rejection := range rejections {
		if rejection.Actor != actor || rejection.Next != serveReasonDefaultNext(rejection.Code) {
			t.Fatalf("rejection %+v is not attributed with the default next action", rejection)
		}
		got = append(got, rejection.Ref+" "+rejection.Code)
	}
	want := []string{"refs/hn/x reserved-ref", "refs/notes/z reserved-ref", "refs/tags/a tag-immutable", "refs/tags/b tag-immutable"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rejections %q, want %q", got, want)
	}
	if rejections := evaluateServePush(ctx, updates[2:4], servePushOptions{}); len(rejections) != 0 {
		t.Fatalf("creations were rejected: %+v", rejections)
	}
	// hn-advance needs exactly one protected update.
	rejections = evaluateServePush(ctx, updates[2:4], servePushOptions{Advance: "sha256:" + strings.Repeat("a", 64)})
	if len(rejections) != 2 || rejections[0].Code != serveReasonUnsupportedCommand || rejections[0].Ref != "refs/heads/created" {
		t.Fatalf("advance with two ordinary refs: %+v", rejections)
	}
}

// The hosted facts load lazily, at most once, and a fresh hosted
// repository has none (the risk noted for WP08).
func TestServePushHostedEventsLoadOnceFromFreshRepository(t *testing.T) {
	h := newServeHarness(t)
	h.Serve("serve", "repo", "create", "r", "--from", serveHarnessSourceRepo(t))
	hosted := h.Layout().RepoGitDir("r")
	t.Setenv("GIT_DIR", hosted)
	config, err := loadServeRepoConfig(h.Layout(), "r")
	if err != nil {
		t.Fatal(err)
	}
	ctx := newServeVerdictContext(hosted, serveHarnessActor("events"), serveTestFingerprint("events"), config, emptyServeBranchOwners())
	events, err := ctx.HostedEvents()
	if err != nil || len(events) != 0 {
		t.Fatalf("hosted events on a fresh repository = %d, %v; want none", len(events), err)
	}
	calls := 0
	ctx = newServeVerdictContext(hosted, ctx.Actor, ctx.Key, config, emptyServeBranchOwners())
	ctx.loadHostedEvents = func() ([]StoredEvent, error) {
		calls++
		return nil, errors.New("boom")
	}
	for range 3 {
		if _, err := ctx.HostedEvents(); err == nil || err.Error() != "boom" {
			t.Fatalf("HostedEvents error = %v", err)
		}
	}
	if calls != 1 {
		t.Fatalf("hosted events loaded %d times, want once", calls)
	}
	if !ctx.IsMaintainer(serveHarnessActor("maintainer")) || ctx.IsMaintainer(ctx.Actor) || ctx.IsMaintainer("") {
		t.Fatal("maintainers do not come from the policy at the default protected branch tip")
	}
}

func TestServePushBranchOwnersSaveAndLoad(t *testing.T) {
	layout := newServeTestLayout(t)
	addServeTestRepo(t, layout, "demo")
	owners, err := loadServeBranchOwners(layout, "demo")
	if err != nil || !reflect.DeepEqual(owners, emptyServeBranchOwners()) {
		t.Fatalf("missing branches.json = %+v, %v; want an empty record set", owners, err)
	}
	a, b := serveStateTestActor("a"), serveStateTestActor("b")
	owners.Branches = []serveBranchOwner{
		{Ref: "refs/heads/z", Owner: a, Created: "2026-10-25T10:00:00Z"},
		{Ref: "refs/heads/a", Owner: b, Created: "2026-10-25T10:01:00Z"},
	}
	if err := saveServeBranchOwners(layout, "demo", owners); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadServeBranchOwners(layout, "demo")
	if err != nil || len(loaded.Branches) != 2 || loaded.Branches[0].Ref != "refs/heads/a" {
		t.Fatalf("saved owners are not sorted by ref: %+v, %v", loaded, err)
	}
	if record, ok := lookupServeBranchOwner(loaded, "refs/heads/z"); !ok || record.Owner != a {
		t.Fatalf("lookup refs/heads/z = %+v, %v", record, ok)
	}
	if _, ok := lookupServeBranchOwner(loaded, "refs/heads/m"); ok {
		t.Fatal("lookup found an unrecorded branch")
	}
	if info, err := os.Lstat(layout.RepoBranches("demo")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("branches.json mode: %v, %v", info, err)
	}
	if err := saveServeBranchOwners(layout, "demo", emptyServeBranchOwners()); err != nil {
		t.Fatal(err)
	}
	if contents, err := os.ReadFile(layout.RepoBranches("demo")); err != nil || string(contents) != "{\n  \"version\": 1,\n  \"branches\": []\n}\n" {
		t.Fatalf("empty save wrote %q, %v; want an empty list", contents, err)
	}
	invalid := serveBranchOwners{Version: 1, Branches: []serveBranchOwner{{Ref: "refs/tags/x", Owner: a, Created: "2026-10-25T10:00:00Z"}}}
	requireServeErrorContains(t, saveServeBranchOwners(layout, "demo", invalid), "not a valid branch ref")
	writeServeTestFile(t, layout.RepoBranches("demo"), "{\"version\":1,\"branches\":[],\"extra\":1}\n", 0o600)
	if _, err := loadServeBranchOwners(layout, "demo"); err == nil {
		t.Fatal("a damaged branches.json loaded")
	}
	if err := validateServeLayout(layout, serveSessionValidation); err == nil {
		t.Fatal("layout validation accepted a damaged branches.json")
	}
}

func TestServePushLogAppendsOrderedLines(t *testing.T) {
	layout := newServeTestLayout(t)
	addServeTestRepo(t, layout, "demo")
	actor, key := serveStateTestActor("log"), serveTestFingerprint("log")
	first := servePushLogEntry{
		Time: "2026-10-25T10:00:00Z", Actor: actor, Key: key, Verdict: servePushVerdictRejected,
		Updates: []servePushLogUpdate{{Ref: "refs/heads/main", Old: strings.Repeat("1", 40), New: strings.Repeat("2", 40)}},
		Codes:   []string{serveReasonProtectedNoProposal},
	}
	second := servePushLogEntry{Time: "2026-10-25T10:01:00Z", Actor: actor, Key: key, Verdict: servePushVerdictAccepted}
	for _, entry := range []servePushLogEntry{first, second} {
		if err := appendServePushLog(layout, "demo", entry); err != nil {
			t.Fatal(err)
		}
	}
	contents, err := os.ReadFile(layout.RepoPushLog("demo"))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"time":"2026-10-25T10:00:00Z","actor":"` + actor + `","key":"` + key + `","verdict":"rejected","updates":[{"ref":"refs/heads/main","old":"` + strings.Repeat("1", 40) + `","new":"` + strings.Repeat("2", 40) + `"}],"codes":["protected-no-proposal"]}` + "\n" +
		`{"time":"2026-10-25T10:01:00Z","actor":"` + actor + `","key":"` + key + `","verdict":"accepted","updates":[],"codes":[]}` + "\n"
	if string(contents) != want {
		t.Fatalf("push.log:\n%s\nwant:\n%s", contents, want)
	}
	if info, err := os.Lstat(layout.RepoPushLog("demo")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("push.log mode: %v, %v", info, err)
	}
	if err := validateServeLayout(layout, serveSessionValidation); err != nil {
		t.Fatalf("layout after two appends: %v", err)
	}

	for _, tc := range []struct {
		name  string
		entry servePushLogEntry
	}{
		{"bad time", servePushLogEntry{Time: "now", Actor: actor, Key: key, Verdict: servePushVerdictAccepted}},
		{"bad actor", servePushLogEntry{Time: second.Time, Actor: "abc", Key: key, Verdict: servePushVerdictAccepted}},
		{"bad key", servePushLogEntry{Time: second.Time, Actor: actor, Key: "SHA256:x", Verdict: servePushVerdictAccepted}},
		{"bad verdict", servePushLogEntry{Time: second.Time, Actor: actor, Key: key, Verdict: "maybe"}},
		{"accepted with codes", servePushLogEntry{Time: second.Time, Actor: actor, Key: key, Verdict: servePushVerdictAccepted, Codes: []string{serveReasonReservedRef}}},
		{"rejected without codes", servePushLogEntry{Time: second.Time, Actor: actor, Key: key, Verdict: servePushVerdictRejected}},
		{"unknown code", servePushLogEntry{Time: second.Time, Actor: actor, Key: key, Verdict: servePushVerdictRejected, Codes: []string{"nope"}}},
		{"bad update", servePushLogEntry{Time: second.Time, Actor: actor, Key: key, Verdict: servePushVerdictAccepted, Updates: []servePushLogUpdate{{Ref: "refs/heads/x", Old: "zz", New: "zz"}}}},
	} {
		if err := appendServePushLog(layout, "demo", tc.entry); err == nil {
			t.Fatalf("%s: appended an invalid entry", tc.name)
		}
	}
	if after, _ := os.ReadFile(layout.RepoPushLog("demo")); !bytes.Equal(after, contents) {
		t.Fatal("an invalid entry changed push.log")
	}

	// A symlinked log is refused without touching its target.
	target := filepath.Join(t.TempDir(), "target")
	writeServeTestFile(t, target, "", 0o600)
	removeServeTest(t, layout.RepoPushLog("demo"))
	symlinkServeTest(t, target, layout.RepoPushLog("demo"))
	if err := appendServePushLog(layout, "demo", second); err == nil {
		t.Fatal("appended through a symlink")
	}
	if contents, _ := os.ReadFile(target); len(contents) != 0 {
		t.Fatal("the symlink target was written")
	}

	// An append that would push the log past its limit is refused, so the
	// log never becomes a layout defect.
	removeServeTest(t, layout.RepoPushLog("demo"))
	writeServeTestFile(t, layout.RepoPushLog("demo"), "", 0o600)
	if err := os.Truncate(layout.RepoPushLog("demo"), servePushLogLimit-10); err != nil {
		t.Fatal(err)
	}
	requireServeErrorContains(t, appendServePushLog(layout, "demo", second), "push log would exceed")
	if info, _ := os.Stat(layout.RepoPushLog("demo")); info.Size() != servePushLogLimit-10 {
		t.Fatalf("refused append changed the size to %d", info.Size())
	}
}

// The hooks never take the repository lock (code review item): the hook
// code has no path to it. The accepted pushes in the other tests also show
// the hook does not wait, because hn serve git holds the lock throughout.
func TestServePushHooksNeverTakeRepositoryLock(t *testing.T) {
	for _, file := range []string{"serve_hooks.go", "serve_verdict.go"} {
		contents, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"acquireServeLock(", "acquireServeRepoLock(", ".RepoLock(", "holdServeLock("} {
			if bytes.Contains(contents, []byte(forbidden)) {
				t.Fatalf("%s calls %s; hooks run under the wrapper's repository lock", file, forbidden)
			}
		}
	}
}
