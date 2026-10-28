package main

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"maps"
	"os"
	"slices"
	"strings"
)

// auditPackFact is an audit event with the audit.pack it carries.
type auditPackFact struct {
	Pack  []byte
	Event Event
}

// auditPackObject is one object of an audit pack as walkAuditPack decodes
// it: its actual type and expanded size, its contents when it is a commit
// or tree, and its entry, the header and compressed data the pack stores.
type auditPackObject struct {
	kind    string
	size    int64
	content []byte
	entry   []byte
}

// auditSnapshot is a decoded audit pack: its sorted object IDs, the same
// IDs in pack order and its objects by ID.
type auditSnapshot struct {
	ids     []string
	order   []string
	objects map[string]auditPackObject
}

func decodeAuditSnapshot(pack []byte, oidLength int) (*auditSnapshot, error) {
	s := &auditSnapshot{objects: map[string]auditPackObject{}}
	ids, err := walkAuditPack(pack, oidLength, true, func(oid string, object auditPackObject) {
		s.order = append(s.order, oid)
		s.objects[oid] = object
	})
	if err != nil {
		return nil, err
	}
	s.ids = ids
	return s, nil
}

// verifyAuditPacks checks each fact's audit.pack against its signed fact
// and installs nothing. It returns the decoded snapshot of each fact, or
// the index of the first fact that fails with its error, or -1 with an
// error no fact explains.
//
// Every check but Git's object syntax check reads the objects the pack
// parser decoded, so a fact costs no Git process: exact membership, the
// snapshot and path closure and the object shapes are computed per pack
// from that pack's objects alone, as a private repository holding only the
// pack would answer. Identical packs are decoded once and a pack bound to
// the same snapshot roots is checked once. The syntax check indexes every
// verified pack with git index-pack --fsck-objects in one private
// temporary repository per object format, with one index-pack per
// maxAuditPack bytes of packs (see fsckAuditSnapshots), so the number of
// Git processes grows with the bytes verified, never with the number of
// facts.
func verifyAuditPacks(facts []auditPackFact) ([]*auditSnapshot, int, error) {
	snapshots := make([]*auditSnapshot, len(facts))
	decoded := map[string]*auditSnapshot{}
	bound := map[string]bool{}
	owners := map[*auditSnapshot]int{}
	formats := map[int][]*auditSnapshot{}
	for index, fact := range facts {
		if fact.Event.Audit == nil {
			return nil, index, fmt.Errorf("audit fact required")
		}
		oidLength := len(fact.Event.Audit.PolicyCommit)
		if oidLength != 40 && oidLength != 64 {
			return nil, index, fmt.Errorf("audit requires exact authority policy commit")
		}
		digest := sha256.Sum256(fact.Pack)
		key := fmt.Sprintf("%d:%x", oidLength, digest)
		snapshot, seen := decoded[key]
		if !seen {
			var err error
			if snapshot, err = decodeAuditSnapshot(fact.Pack, oidLength); err != nil {
				return nil, index, err
			}
			decoded[key] = snapshot
			owners[snapshot] = index
			formats[oidLength] = append(formats[oidLength], snapshot)
		}
		binding := key + "\x00" + strings.Join(auditSnapshotRoots(fact.Event), "\x00")
		if !bound[binding] {
			if err := validateAuditPackBinding(snapshot, fact.Event); err != nil {
				return nil, index, err
			}
			bound[binding] = true
		}
		snapshots[index] = snapshot
	}
	for _, oidLength := range slices.Sorted(maps.Keys(formats)) {
		failed, err := fsckAuditSnapshots(oidLength, formats[oidLength])
		if err != nil {
			if failed < 0 {
				return nil, -1, err
			}
			return nil, owners[formats[oidLength][failed]], err
		}
	}
	return snapshots, -1, nil
}

// auditSnapshotRoots is what an audit fact's exact closure depends on
// besides the pack: its policy commit, source commit and path and code
// commit.
func auditSnapshotRoots(e Event) []string {
	f := e.Audit
	roots := []string{f.PolicyCommit, f.CodeCommit, "", ""}
	if f.Source != nil {
		roots[2], roots[3] = f.Source.Commit, f.Source.Path
	}
	return roots
}

// auditSnapshotDependencies is the closure auditDependencyObjectIDsAt
// computes with rev-parse, ls-tree and rev-list, computed from the objects
// of s alone: the policy commit, its root tree and the trees and regular
// blob of the .hn/policy.json path; the same for a Git rule source's commit
// and path; and the code commit with every tree and blob of its snapshot,
// gitlinks excluded. A tree the closure must read and s lacks is an error,
// as it is for Git in a repository holding only the pack.
func auditSnapshotDependencies(s *auditSnapshot, e Event) ([]string, error) {
	f := e.Audit
	objects := []string{}
	treeOf := func(commit string) (string, error) {
		object, ok := s.objects[strings.ToLower(commit)]
		if !ok || object.kind != "commit" {
			return "", fmt.Errorf("audit dependency commit %s unavailable", commit)
		}
		header, _, _ := bytes.Cut(object.content, []byte("\n"))
		tree, ok := strings.CutPrefix(string(header), "tree ")
		if !ok || !validGitOID(tree) || len(tree) != len(commit) {
			return "", fmt.Errorf("malformed audit commit")
		}
		return strings.ToLower(tree), nil
	}
	addPath := func(commit, path string) error {
		tree, err := treeOf(commit)
		if err != nil {
			return err
		}
		objects = append(objects, commit, tree)
		parts := strings.Split(path, "/")
		for index, part := range parts {
			entries, err := auditSnapshotTree(s, tree)
			if err != nil {
				return err
			}
			entry, found := auditTreeEntry{}, false
			for _, candidate := range entries {
				if candidate.name == part {
					entry, found = candidate, true
					break
				}
			}
			if !found {
				return fmt.Errorf("audit dependency path missing: %s", strings.Join(parts[:index+1], "/"))
			}
			if index == len(parts)-1 && (entry.mode != "100644" && entry.mode != "100755") {
				return fmt.Errorf("audit dependency must be a regular blob")
			}
			if index < len(parts)-1 && entry.mode != "40000" {
				return fmt.Errorf("audit dependency parent must be a tree")
			}
			objects = append(objects, entry.oid)
			tree = entry.oid
		}
		return nil
	}
	if err := addPath(f.PolicyCommit, ".hn/policy.json"); err != nil {
		return nil, err
	}
	if f.Source != nil && f.Source.Commit != "" {
		if err := addPath(f.Source.Commit, f.Source.Path); err != nil {
			return nil, err
		}
	}
	if f.CodeCommit != "" {
		tree, err := treeOf(f.CodeCommit)
		if err != nil {
			return nil, err
		}
		objects = append(objects, strings.ToLower(f.CodeCommit), tree)
		walked := map[string]bool{tree: true}
		pending := []string{tree}
		for len(pending) > 0 {
			current := pending[len(pending)-1]
			pending = pending[:len(pending)-1]
			entries, err := auditSnapshotTree(s, current)
			if err != nil {
				return nil, err
			}
			for _, entry := range entries {
				switch entry.mode {
				case "40000":
					objects = append(objects, entry.oid)
					if !walked[entry.oid] {
						walked[entry.oid] = true
						pending = append(pending, entry.oid)
					}
				case "160000":
					// Submodule content is not part of the snapshot.
				default:
					objects = append(objects, entry.oid)
				}
			}
		}
	}
	objects = sortedUniqueStrings(objects...)
	if len(objects) > maxAuditObjects {
		return nil, fmt.Errorf("audit dependency object count exceeded")
	}
	return objects, nil
}

type auditTreeEntry struct {
	mode, name, oid string
}

// auditSnapshotTree parses the entries of the tree id of s.
func auditSnapshotTree(s *auditSnapshot, id string) ([]auditTreeEntry, error) {
	object, ok := s.objects[id]
	if !ok || object.kind != "tree" {
		return nil, fmt.Errorf("audit dependency tree %s unavailable", id)
	}
	raw := object.content
	oidBytes := len(id) / 2
	var entries []auditTreeEntry
	for len(raw) > 0 {
		space := bytes.IndexByte(raw, ' ')
		nul := bytes.IndexByte(raw, 0)
		if space <= 0 || nul <= space || len(raw)-nul-1 < oidBytes {
			return nil, fmt.Errorf("malformed audit tree")
		}
		entries = append(entries, auditTreeEntry{mode: string(raw[:space]), name: string(raw[space+1 : nul]), oid: hex.EncodeToString(raw[nul+1 : nul+1+oidBytes])})
		raw = raw[nul+1+oidBytes:]
	}
	return entries, nil
}

// fsckAuditSnapshots runs git index-pack --fsck-objects over the objects
// of snapshots, in order, in one private temporary repository that is
// removed afterwards. Snapshots are combined into packs of at most
// maxAuditPack bytes, each snapshot whole in one pack, so every check Git
// makes of one pack, such as reading a .gitmodules blob a tree of the pack
// names, is made in the combined pack too. A combined pack can only be
// refused more often than its snapshots alone. When a combined pack is
// refused, its snapshots are indexed one at a time and the index of the
// first refused one is returned with its error, or of the first one of
// the combined pack when none is refused alone.
func fsckAuditSnapshots(oidLength int, snapshots []*auditSnapshot) (int, error) {
	if len(snapshots) == 0 {
		return -1, nil
	}
	temporary, err := os.MkdirTemp("", "hn-audit-objects-*")
	if err != nil {
		return -1, err
	}
	defer os.RemoveAll(temporary)
	format := "sha1"
	if oidLength == 64 {
		format = "sha256"
	}
	if _, err := gitPrivateInputAt(temporary, nil, "init", "--bare", "--quiet", "--template=", "--object-format="+format); err != nil {
		return -1, err
	}
	// Validate Git's object syntax without demanding complete parent history
	// or unrelated members of policy/source path-proof trees. The temporary
	// repository holds no ref, so nothing it indexes is used again.
	index := func(entries [][]byte) error {
		_, err := gitPrivateInputAt(temporary, buildAuditPack(entries, oidLength), "index-pack", "--stdin", "--fsck-objects", "--max-input-size=67108864")
		return err
	}
	first := 0
	var entries [][]byte
	members := map[string]bool{}
	size := 0
	flush := func(end int) (int, error) {
		err := index(entries)
		if err == nil || end-first == 1 {
			return first, err
		}
		for alone := first; alone < end; alone++ {
			own := make([][]byte, 0, len(snapshots[alone].order))
			for _, oid := range snapshots[alone].order {
				own = append(own, snapshots[alone].objects[oid].entry)
			}
			if aloneErr := index(own); aloneErr != nil {
				return alone, aloneErr
			}
		}
		return first, err
	}
	trailer := 20
	if oidLength == 64 {
		trailer = 32
	}
	for current, snapshot := range snapshots {
		added := 0
		for _, oid := range snapshot.order {
			if !members[oid] {
				added += len(snapshot.objects[oid].entry)
			}
		}
		if len(entries) > 0 && 12+size+added+trailer > maxAuditPack {
			if failed, err := flush(current); err != nil {
				return failed, err
			}
			first, entries, members, size = current, nil, map[string]bool{}, 0
		}
		for _, oid := range snapshot.order {
			if !members[oid] {
				members[oid] = true
				entries = append(entries, snapshot.objects[oid].entry)
				size += len(snapshot.objects[oid].entry)
			}
		}
	}
	if failed, err := flush(len(snapshots)); err != nil {
		return failed, err
	}
	return -1, nil
}

// buildAuditPack assembles a version 2 pack from nondelta pack entries,
// with the trailer checksum of the object format oidLength names.
func buildAuditPack(entries [][]byte, oidLength int) []byte {
	var checksum hash.Hash = sha1.New()
	if oidLength == 64 {
		checksum = sha256.New()
	}
	var pack bytes.Buffer
	pack.WriteString("PACK")
	_ = binary.Write(&pack, binary.BigEndian, uint32(2))
	_ = binary.Write(&pack, binary.BigEndian, uint32(len(entries)))
	for _, entry := range entries {
		pack.Write(entry)
	}
	_, _ = checksum.Write(pack.Bytes())
	pack.Write(checksum.Sum(nil))
	return pack.Bytes()
}

// installAuditObjects writes to the object database of gitDir, with the
// inherited environment, the objects of snapshots it lacks, so a read of
// already installed history writes nothing. Inside a receive hook the
// objects land in the push quarantine. The snapshots must be verified.
func installAuditObjects(gitDir string, snapshots []*auditSnapshot) error {
	formats := map[int][]*auditSnapshot{}
	var names []string
	listed := map[string]bool{}
	for _, snapshot := range snapshots {
		if snapshot == nil {
			continue
		}
		for _, oid := range snapshot.order {
			if !listed[oid] {
				listed[oid] = true
				names = append(names, oid)
			}
		}
		formats[len(snapshot.order[0])] = append(formats[len(snapshot.order[0])], snapshot)
	}
	answers, err := serveBatchCheck(gitDir, names)
	if err != nil {
		return err
	}
	missing := map[string]bool{}
	for index, answer := range answers {
		if !answer.Found {
			missing[names[index]] = true
		}
	}
	if len(missing) == 0 {
		return nil
	}
	for oidLength, group := range formats {
		trailer := 20
		if oidLength == 64 {
			trailer = 32
		}
		var entries [][]byte
		size := 0
		install := func() error {
			if len(entries) == 0 {
				return nil
			}
			_, err := gitInputAt(gitDir, buildAuditPack(entries, oidLength), "index-pack", "--stdin", "--max-input-size=67108864")
			entries, size = nil, 0
			return err
		}
		for _, snapshot := range group {
			for _, oid := range snapshot.order {
				if !missing[oid] {
					continue
				}
				delete(missing, oid)
				entry := snapshot.objects[oid].entry
				if len(entries) > 0 && 12+size+len(entry)+trailer > maxAuditPack {
					if err := install(); err != nil {
						return err
					}
				}
				entries = append(entries, entry)
				size += len(entry)
			}
		}
		if err := install(); err != nil {
			return err
		}
	}
	return nil
}
