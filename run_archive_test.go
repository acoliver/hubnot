package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runArchiveFixture commits a tree whose tar stream is a few hundred
// kilobytes, larger than one pipe buffer, and points TMPDIR at a fresh
// directory so leftover extraction directories are visible.
func runArchiveFixture(t *testing.T) (commit, tmp string, files map[string][]byte) {
	t.Helper()
	inReplicationTestRepository(t)
	files = map[string][]byte{}
	for i := 0; i < 40; i++ {
		name := filepath.Join("data", "part"+strings.Repeat("x", i%7)+string(rune('a'+i%26))+".bin")
		content := bytes.Repeat([]byte{byte(i), byte(255 - i)}, 4096+i*37)
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, content, 0o644); err != nil {
			t.Fatal(err)
		}
		files[name] = content
	}
	mustGit(t, "add", "-A")
	mustGit(t, "commit", "-q", "-m", "fixture")
	commit = mustGitText(t, "rev-parse", "HEAD")
	tmp = t.TempDir()
	t.Setenv("TMPDIR", tmp)
	return commit, tmp, files
}

func withMaxArchiveSize(t *testing.T, limit int64) {
	t.Helper()
	previous := maxArchiveSize
	maxArchiveSize = limit
	t.Cleanup(func() { maxArchiveSize = previous })
}

func assertNoRunDirectories(t *testing.T, tmp string) {
	t.Helper()
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "hn-run-") {
			t.Fatalf("extraction directory %s was left behind", entry.Name())
		}
	}
}

func TestExtractCommitStreamsTheWholeTree(t *testing.T) {
	commit, tmp, files := runArchiveFixture(t)
	withMaxArchiveSize(t, 64<<20)
	root, err := extractCommit(commit)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if filepath.Dir(root) != tmp {
		t.Fatalf("extracted to %s, not below TMPDIR %s", root, tmp)
	}
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s differs after extraction", name)
		}
	}
}

func TestExtractCommitRefusesAnArchiveOverTheLimit(t *testing.T) {
	commit, tmp, _ := runArchiveFixture(t)
	// Over one pipe buffer but under the archive, so the refusal happens
	// while git is still writing.
	withMaxArchiveSize(t, 128<<10)
	root, err := extractCommit(commit)
	if err == nil {
		os.RemoveAll(root)
		t.Fatal("an archive over the limit was extracted")
	}
	if !strings.Contains(err.Error(), "run archive exceeds 131072 bytes") {
		t.Fatalf("unexpected error: %v", err)
	}
	assertNoRunDirectories(t, tmp)
}

func TestExtractCommitReportsGitFailure(t *testing.T) {
	_, tmp, _ := runArchiveFixture(t)
	withMaxArchiveSize(t, 64<<20)
	_, err := extractCommit(strings.Repeat("0", 40))
	if err == nil || !strings.Contains(err.Error(), "archive run commit") {
		t.Fatalf("missing commit: err = %v", err)
	}
	assertNoRunDirectories(t, tmp)
}
