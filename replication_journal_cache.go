package main

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// The replication acceptance state is rebuilt from every transaction
// receipt, pending anchor and unaccepted-object record of the clone. One
// command asks for it once per policy, pipeline and event read, so on a
// clone with a large journal the same files were parsed hundreds of times.
// The parse is cached per process and reused only while a fingerprint of
// every input file (name, size, modification time, inode) is unchanged.
// The fingerprint is taken under the same shared clone-state lock as the
// parse, and every journal writer replaces files, so any write changes it.
type replicationAcceptanceCacheEntry struct {
	fingerprint string
	denied      map[string]bool
	pending     map[string]replicationTransactionRecord
}

var replicationAcceptanceCache = struct {
	sync.Mutex
	entries map[string]replicationAcceptanceCacheEntry
}{entries: map[string]replicationAcceptanceCacheEntry{}}

func replicationJournalFingerprint(locations []string) (string, error) {
	var parts []string
	for _, location := range locations {
		parts = append(parts, "L "+location)
		for _, directory := range []string{replicationTransactionsPath(location), replicationAnchorsPath(location)} {
			entries, err := os.ReadDir(directory)
			if os.IsNotExist(err) {
				parts = append(parts, "D- "+directory)
				continue
			}
			if err != nil {
				return "", err
			}
			parts = append(parts, "D "+directory)
			for _, entry := range entries {
				part, err := replicationJournalFileStamp(filepath.Join(directory, entry.Name()))
				if err != nil {
					return "", err
				}
				parts = append(parts, part)
			}
		}
		part, err := replicationJournalFileStamp(replicationUnacceptedObjectsPath(location))
		if err != nil {
			return "", err
		}
		parts = append(parts, part)
	}
	sort.Strings(parts)
	return strings.Join(parts, "\n"), nil
}

func replicationJournalFileStamp(path string) (string, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "F- " + path, nil
	}
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("F %s %d %d %d %o", path, info.Size(), info.ModTime().UnixNano(), fileInode(info), info.Mode()), nil
}

// The cached maps are never handed out: callers get copies they may change.
func replicationAcceptanceCacheGet(gitDir, fingerprint string) (map[string]bool, map[string]replicationTransactionRecord, bool) {
	replicationAcceptanceCache.Lock()
	defer replicationAcceptanceCache.Unlock()
	entry, ok := replicationAcceptanceCache.entries[gitDir]
	if !ok || entry.fingerprint != fingerprint {
		return nil, nil, false
	}
	return maps.Clone(entry.denied), maps.Clone(entry.pending), true
}

func replicationAcceptanceCachePut(gitDir, fingerprint string, denied map[string]bool, pending map[string]replicationTransactionRecord) {
	replicationAcceptanceCache.Lock()
	defer replicationAcceptanceCache.Unlock()
	replicationAcceptanceCache.entries[gitDir] = replicationAcceptanceCacheEntry{
		fingerprint: fingerprint, denied: maps.Clone(denied), pending: maps.Clone(pending),
	}
}
