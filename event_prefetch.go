package main

import (
	"bufio"
	"bytes"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

// maxPrefetchedEventBlob bounds one prefetched blob. Larger blobs, and any
// commit whose blobs do not read back as exactly two blobs, are left to the
// per-event path, which reports the error it always did.
const maxPrefetchedEventBlob = 1 << 20

// Quarantine prefetch is an operation-local accelerator, never an acceptance
// cache. Bound requests as well as decoded bytes; larger objects and any batch
// failure use the ordinary loader, preserving its validation and diagnostics.
const quarantinePrefetchCommits = 64
const quarantinePrefetchBytes = 4 << 20

func prefetchEventBlobsAt(gitDir string, commits []string) map[string][2][]byte {
	if gitDir == "" || len(commits) == 0 || len(commits) > quarantinePrefetchCommits {
		return nil
	}
	for _, commit := range commits {
		if !validFullGitObjectID(commit) {
			return nil
		}
	}
	cmd := exec.Command("git", "--git-dir="+gitDir, "cat-file", "--batch")
	cmd.Stderr = &issueReadStderr{}
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil
	}
	defer input.Close()
	output, err := cmd.StdoutPipe()
	if err != nil {
		return nil
	}
	if err := cmd.Start(); err != nil {
		return nil
	}
	finished := false
	defer func() {
		if !finished {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	reader := bufio.NewReaderSize(output, 4096)
	limits := issueReadLimits{object: maxPrefetchedEventBlob, total: quarantinePrefetchBytes}
	used := int64(0)
	result := make(map[string][2][]byte, len(commits))
	for _, commit := range commits {
		var pair [2][]byte
		for index, name := range []string{"event.json", "signature"} {
			if _, err := io.WriteString(input, commit+":"+name+"\n"); err != nil {
				return nil
			}
			blob, err := readIssueBatchTyped(reader, "blob", -1, limits, &used)
			if err != nil {
				return nil
			}
			pair[index] = blob
		}
		result[commit] = pair
	}
	if err := input.Close(); err != nil {
		return nil
	}
	if _, err := reader.ReadByte(); err != io.EOF {
		return nil
	}
	err = cmd.Wait()
	finished = true
	if err != nil {
		return nil
	}
	return result
}

// prefetchEventBlobs reads event.json and signature of every commit with
// one git cat-file --batch instead of two git processes per event. It is
// only an accelerator: any failure returns what was read so far, and the
// caller falls back to loadStoredEventAt for the rest.
func prefetchEventBlobs(commits []string) map[string][2][]byte {
	result := make(map[string][2][]byte, len(commits))
	if len(commits) == 0 {
		return result
	}
	var input strings.Builder
	for _, commit := range commits {
		if !validFullGitObjectID(commit) {
			return result
		}
		input.WriteString(commit + ":event.json\n" + commit + ":signature\n")
	}
	output, err := gitInputAt("", []byte(input.String()), "cat-file", "--batch")
	if err != nil {
		return result
	}
	reader := bufio.NewReader(bytes.NewReader(output))
	for _, commit := range commits {
		payload, ok := readPrefetchedBlob(reader)
		if !ok {
			return result
		}
		signature, ok := readPrefetchedBlob(reader)
		if !ok {
			return result
		}
		result[commit] = [2][]byte{payload, signature}
	}
	return result
}

// readPrefetchedBlob reads one "<oid> blob <size>" record and its content.
func readPrefetchedBlob(reader *bufio.Reader) ([]byte, bool) {
	header, err := reader.ReadString('\n')
	if err != nil {
		return nil, false
	}
	fields := strings.Fields(header)
	if len(fields) != 3 || fields[1] != "blob" {
		return nil, false
	}
	size, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil || size < 0 || size > maxPrefetchedEventBlob {
		return nil, false
	}
	data := make([]byte, size+1)
	if _, err := io.ReadFull(reader, data); err != nil || data[size] != '\n' {
		return nil, false
	}
	return data[:size], true
}
