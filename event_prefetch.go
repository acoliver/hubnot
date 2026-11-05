package main

import (
	"bufio"
	"bytes"
	"io"
	"strconv"
	"strings"
)

// maxPrefetchedEventBlob bounds one prefetched blob. Larger blobs, and any
// commit whose blobs do not read back as exactly two blobs, are left to the
// per-event path, which reports the error it always did.
const maxPrefetchedEventBlob = 1 << 20

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
