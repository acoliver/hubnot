package main

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// blockingReader never returns, standing in for a producer that keeps its
// output open.
type blockingReader struct{ release chan struct{} }

func (b blockingReader) Read([]byte) (int, error) {
	<-b.release
	return 0, io.EOF
}

// Empty entries need no file body reads, so the limit must stop the tar
// reader itself, before it takes another header past the limit.
func TestExtractRunArchiveStopsEmptyEntriesAtTheLimit(t *testing.T) {
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	for i := 0; i < 4096; i++ {
		if err := writer.WriteHeader(&tar.Header{Name: fmt.Sprintf("empty-%04d", i), Mode: 0o644, Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	const limit = 64 << 10
	withMaxArchiveSize(t, limit)
	t.Setenv("TMPDIR", t.TempDir())
	block := blockingReader{release: make(chan struct{})}
	defer close(block.release)
	stream := &runArchiveReader{reader: io.MultiReader(bytes.NewReader(archive.Bytes()), block)}
	type outcome struct {
		root string
		err  error
	}
	done := make(chan outcome, 1)
	go func() {
		root, err := extractRunArchive(stream)
		done <- outcome{root, err}
	}()
	select {
	case got := <-done:
		if got.root != "" {
			defer os.RemoveAll(got.root)
		}
		if got.err == nil || stream.err == nil {
			t.Fatalf("an over-limit archive of empty entries was accepted: %v", got.err)
		}
		entries, err := os.ReadDir(got.root)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) > limit/512 {
			t.Fatalf("extracted %d entries from %d bytes", len(entries), limit)
		}
		if _, err := stream.Read(make([]byte, 1)); err != stream.err {
			t.Fatalf("refusal is not sticky: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("extraction blocked past the limit")
	}
}

// When git fails part way, its own diagnostic reaches the caller.
func TestExtractCommitKeepsGitsDiagnosticWhenItFailsMidStream(t *testing.T) {
	commit, tmp, _ := runArchiveFixture(t)
	withMaxArchiveSize(t, 64<<20)
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	// Real archive output cut inside a file body, then a failing exit.
	script := "#!/bin/sh\n'" + realGit + "' \"$@\" | head -c 20000\necho 'fatal: lab failure' >&2\nexit 23\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	_, err = extractCommit(commit)
	if err == nil || !strings.Contains(err.Error(), "fatal: lab failure") {
		t.Fatalf("git's diagnostic was lost: %v", err)
	}
	assertNoRunDirectories(t, tmp)
}
