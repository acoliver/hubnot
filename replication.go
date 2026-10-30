package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode"
)

const replicationSelectionVersion = 1

const (
	replicationActor    = "actor"
	replicationProposal = "proposal"
	replicationMemory   = "memory"
)

var errShallowRecoverySelectionRequired = errors.New("selected shallow recovery requires an exact saved selection")

var errAllTrustedWithAll = errors.New("--all-trusted is mutually exclusive with --all")

type ReplicationBudgets struct {
	MaxEvents          int64 `json:"maxEvents"`
	MaxObjects         int64 `json:"maxObjects"`
	MaxObjectBytes     int64 `json:"maxObjectBytes"`
	MaxAttachmentBytes int64 `json:"maxAttachmentBytes"`
	MaxTotalBytes      int64 `json:"maxTotalBytes"`
}

func defaultReplicationBudgets() ReplicationBudgets {
	return ReplicationBudgets{
		MaxEvents:          10_000,
		MaxObjects:         100_000,
		MaxObjectBytes:     64 << 20,
		MaxAttachmentBytes: 16 << 20,
		MaxTotalBytes:      1 << 30,
	}
}

func (budgets ReplicationBudgets) validate() error {
	values := []struct {
		name  string
		value int64
	}{
		{"max-events", budgets.MaxEvents},
		{"max-objects", budgets.MaxObjects},
		{"max-object-bytes", budgets.MaxObjectBytes},
		{"max-attachment-bytes", budgets.MaxAttachmentBytes},
		{"max-total-bytes", budgets.MaxTotalBytes},
	}
	for _, value := range values {
		if value.value <= 0 {
			return fmt.Errorf("%s must be a positive integer", value.name)
		}
	}
	return nil
}

// ReplicationSelection is the saved per-remote selection. AllTrusted is
// omitted when false so files written without --all-trusted stay
// byte-identical to files from earlier versions.
type ReplicationSelection struct {
	Version    int                `json:"version"`
	Remote     string             `json:"remote"`
	Actors     []string           `json:"actors"`
	Proposals  []string           `json:"proposals"`
	Memories   []string           `json:"memories,omitempty"`
	All        bool               `json:"all"`
	AllTrusted bool               `json:"allTrusted,omitempty"`
	Budgets    ReplicationBudgets `json:"budgets"`
}

type repeatedStringFlag []string

func (values *repeatedStringFlag) String() string { return strings.Join(*values, ",") }

func (values *repeatedStringFlag) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func validReplicationRemote(remote string) bool {
	if remote == "" || remote == "." || remote == ".." || len(remote) > 128 {
		return false
	}
	first := rune(remote[0])
	if !((first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z') || (first >= '0' && first <= '9') || first == '_') {
		return false
	}
	for index, character := range remote {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '-' || character == '_' || (character == '.' && index > 0) {
			continue
		}
		return false
	}
	return !strings.HasSuffix(remote, ".") && !strings.Contains(remote, "..")
}

// replicationSelectionPath returns the clone-wide saved selection for
// remote. It lives below the Git common directory, because every worktree of
// a clone imports into the same accepted refs and object database, so one
// selection and one set of budgets govern sync from any of them.
func replicationSelectionPath(remote string) (string, error) {
	if !validReplicationRemote(remote) {
		return "", fmt.Errorf("invalid replication remote name")
	}
	gitDir, err := requireCloneStateDirectory()
	if err != nil {
		return "", err
	}
	return replicationSelectionPathAt(gitDir, remote), nil
}

func replicationSelectionPathAt(gitDir, remote string) string {
	encoded := base64.RawURLEncoding.EncodeToString([]byte(remote))
	return filepath.Join(gitDir, "hn", "replication", "selections", encoded+".json")
}

// legacyReplicationSelectionPaths returns the saved selections for remote
// that releases before selections became clone-wide wrote below the private
// Git directories of the clone's linked worktrees.
func legacyReplicationSelectionPaths(remote string) ([]string, error) {
	if !validReplicationRemote(remote) {
		return nil, fmt.Errorf("invalid replication remote name")
	}
	gitDir, err := requireCloneStateDirectory()
	if err != nil {
		return nil, err
	}
	worktrees, err := linkedWorktreeGitDirectories(gitDir)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0)
	for _, worktree := range worktrees {
		path := replicationSelectionPathAt(worktree, remote)
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, fmt.Errorf("inspect legacy replication selection %s: %w", path, err)
		}
		paths = append(paths, path)
	}
	return paths, nil
}

// lockReplicationSelections takes the clone state lock that serializes saved
// replication selections: shared for a reader, which must see the
// clone-wide selection and its legacy copies as one snapshot, exclusive for
// a save, which writes the clone-wide selection and then retires the legacy
// copies. Without it, a reader could miss the clone-wide file before a save
// and the legacy file after it, and fall back to compatibility-all.
func lockReplicationSelections(kind serveLockKind) (func(), error) {
	gitDir, err := requireCloneStateDirectory()
	if err != nil {
		return nil, err
	}
	return lockCloneState(gitDir, kind)
}

func readReplicationSelectionFile(path, remote string) (ReplicationSelection, error) {
	contents, err := readPrivateFile(path)
	if err != nil {
		return ReplicationSelection{}, err
	}
	return decodeReplicationSelection(contents, remote)
}

func decodeReplicationSelection(contents []byte, remote string) (ReplicationSelection, error) {
	var selection ReplicationSelection
	if err := decodePrivateJSON(contents, &selection, "replication selection"); err != nil {
		return ReplicationSelection{}, err
	}
	if selection.Remote != remote {
		return ReplicationSelection{}, fmt.Errorf("replication selection remote mismatch: got %q, want %q", selection.Remote, remote)
	}
	if err := validateReplicationSelection(selection); err != nil {
		return ReplicationSelection{}, fmt.Errorf("invalid saved replication selection: %w", err)
	}
	return selection, nil
}

func sameReplicationSelection(left, right ReplicationSelection) bool {
	return left.Version == right.Version && left.Remote == right.Remote &&
		slices.Equal(left.Actors, right.Actors) && slices.Equal(left.Proposals, right.Proposals) &&
		slices.Equal(left.Memories, right.Memories) && left.All == right.All &&
		left.AllTrusted == right.AllTrusted && left.Budgets == right.Budgets
}

// replicationSelectionSnapshot returns the exact bytes of every saved
// selection file for remote, clone-wide and legacy, labelled by path, so a
// caller can detect any change to the selection that governs the clone.
func replicationSelectionSnapshot(remote string) ([]byte, error) {
	path, err := replicationSelectionPath(remote)
	if err != nil {
		return nil, err
	}
	release, err := lockReplicationSelections(serveLockShared)
	if err != nil {
		return nil, err
	}
	defer release()
	legacy, err := legacyReplicationSelectionPaths(remote)
	if err != nil {
		return nil, err
	}
	var snapshot bytes.Buffer
	for _, candidate := range append([]string{path}, legacy...) {
		contents, err := readPrivateFile(candidate)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&snapshot, "%s\x00%d\x00", candidate, len(contents))
		snapshot.Write(contents)
	}
	return snapshot.Bytes(), nil
}

func validateReplicationSelection(selection ReplicationSelection) error {
	if selection.Version != replicationSelectionVersion {
		return fmt.Errorf("unsupported replication selection version %d", selection.Version)
	}
	if !validReplicationRemote(selection.Remote) {
		return fmt.Errorf("invalid replication remote name")
	}
	if err := selection.Budgets.validate(); err != nil {
		return err
	}
	if selection.All && selection.AllTrusted {
		return errAllTrustedWithAll
	}
	if selection.All && (len(selection.Actors) != 0 || len(selection.Proposals) != 0 || len(selection.Memories) != 0) {
		return fmt.Errorf("--all is mutually exclusive with actor, proposal, and memory selectors")
	}
	if !selection.All && !selection.AllTrusted && len(selection.Actors) == 0 && len(selection.Proposals) == 0 && len(selection.Memories) == 0 {
		return fmt.Errorf("replication selection requires at least one actor, proposal, or memory, or explicit --all")
	}
	seen := make(map[string]string)
	for _, actor := range selection.Actors {
		if !validActorFingerprint(actor) {
			return fmt.Errorf("actor selector %q must be a full actor fingerprint", actor)
		}
		if prior := seen[actor]; prior != "" {
			return fmt.Errorf("duplicate actor selector %s", actor)
		}
		seen[actor] = replicationActor
	}
	for _, proposal := range selection.Proposals {
		if !validEventID(proposal) {
			return fmt.Errorf("proposal selector %q must be a full event ID", proposal)
		}
		if prior := seen[proposal]; prior != "" {
			return fmt.Errorf("duplicate proposal selector %s", proposal)
		}
		seen[proposal] = replicationProposal
	}
	seenMemories := make(map[string]bool)
	for _, stream := range selection.Memories {
		if !validMemoryStreamID(stream) {
			return fmt.Errorf("memory selector %q must be a full memory stream ID", stream)
		}
		if seenMemories[stream] {
			return fmt.Errorf("duplicate memory selector %s", stream)
		}
		seenMemories[stream] = true
	}
	return nil
}

// saveReplicationSelection records the clone-wide selection for a remote and
// then removes any legacy linked-worktree copy for that remote, so the saved
// selection is the only one that governs sync from any worktree afterwards.
func saveReplicationSelection(selection ReplicationSelection) error {
	if err := validateReplicationSelection(selection); err != nil {
		return err
	}
	sort.Strings(selection.Actors)
	sort.Strings(selection.Proposals)
	sort.Strings(selection.Memories)
	path, err := replicationSelectionPath(selection.Remote)
	if err != nil {
		return err
	}
	release, err := lockReplicationSelections(serveLockExclusive)
	if err != nil {
		return err
	}
	defer release()
	legacy, err := legacyReplicationSelectionPaths(selection.Remote)
	if err != nil {
		return err
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(path)))
	selectionDirectory := filepath.Dir(path)
	for _, directory := range []string{root, filepath.Dir(selectionDirectory), selectionDirectory} {
		if err := ensurePrivateDirectory(directory); err != nil {
			return replicationPhaseError(selection.Remote, "selection setup")
		}
	}
	encoded, err := json.MarshalIndent(selection, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if err := writePrivateFileAtomic(path, encoded); err != nil {
		return replicationPhaseError(selection.Remote, "selection recording")
	}
	for _, legacyPath := range legacy {
		if err := os.Remove(legacyPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("saved the clone-wide replication selection for %s, but could not remove the legacy linked-worktree selection %s: %w", selection.Remote, legacyPath, err)
		}
		if err := syncPrivateDirectory(filepath.Dir(legacyPath)); err != nil {
			return err
		}
	}
	return nil
}

// loadReplicationSelection returns the selection that governs sync with
// remote from any worktree of the clone and whether one was saved. The
// clone-wide selection and any legacy copy that an earlier release saved in a
// linked worktree's private Git directory must all agree: a single saved
// selection, wherever it was saved, applies clone-wide, and selections that
// differ are an error until hn replication select saves one clone-wide
// selection, because silently preferring one could widen what another
// worktree imports. Only when no selection was saved anywhere does the
// compatibility default of all advertised refs apply.
func loadReplicationSelection(remote string) (ReplicationSelection, bool, error) {
	path, err := replicationSelectionPath(remote)
	if err != nil {
		return ReplicationSelection{}, false, err
	}
	release, err := lockReplicationSelections(serveLockShared)
	if err != nil {
		return ReplicationSelection{}, false, err
	}
	defer release()
	var selection ReplicationSelection
	source := ""
	contents, err := readPrivateFile(path)
	if err == nil {
		selection, err = decodeReplicationSelection(contents, remote)
		if err != nil {
			return ReplicationSelection{}, false, err
		}
		source = path
	} else if !errors.Is(err, os.ErrNotExist) {
		return ReplicationSelection{}, false, replicationPhaseError(remote, "selection reading")
	}
	runCloneStateHook("between-selection-locations")
	legacy, err := legacyReplicationSelectionPaths(remote)
	if err != nil {
		return ReplicationSelection{}, false, err
	}
	for _, legacyPath := range legacy {
		candidate, err := readReplicationSelectionFile(legacyPath, remote)
		if err != nil {
			return ReplicationSelection{}, false, fmt.Errorf("legacy linked-worktree replication selection %s: %w", legacyPath, err)
		}
		if source == "" {
			selection, source = candidate, legacyPath
			continue
		}
		if !sameReplicationSelection(selection, candidate) {
			return ReplicationSelection{}, false, fmt.Errorf("saved replication selections for remote %s differ between %s and %s; save one clone-wide selection with hn replication select %s", remote, source, legacyPath, remote)
		}
	}
	if source == "" {
		return ReplicationSelection{
			Version: replicationSelectionVersion,
			Remote:  remote,
			All:     true,
			Budgets: defaultReplicationBudgets(),
		}, false, nil
	}
	return selection, true, nil
}

func splitReplicationRemote(args []string) (string, []string, error) {
	remote := "origin"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		remote = args[0]
		args = args[1:]
	}
	if !validReplicationRemote(remote) {
		return "", nil, fmt.Errorf("invalid replication remote name")
	}
	return remote, args, nil
}

func cmdReplication(args []string) error {
	if len(args) == 0 {
		return usageError("usage: hn replication <select|show> [REMOTE]")
	}
	switch args[0] {
	case "select":
		return cmdReplicationSelect(args[1:])
	case "show":
		return cmdReplicationShow(args[1:])
	default:
		return fmt.Errorf("unknown replication command %q", args[0])
	}
}

func cmdReplicationSelect(args []string) error {
	remote, args, err := splitReplicationRemote(args)
	if err != nil {
		return err
	}
	budgets := defaultReplicationBudgets()
	flags := quietFlags("replication select")
	var actors, proposals, memories repeatedStringFlag
	flags.Var(&actors, "actor", "full actor fingerprint")
	flags.Var(&proposals, "proposal", "full candidate event ID")
	flags.Var(&memories, "memory", "full memory stream ID")
	all := flags.Bool("all", false, "select all advertised Hubnot refs")
	allTrusted := flags.Bool("all-trusted", false, "import every actor chain and proposal advertised by an hn serve remote")
	flags.Int64Var(&budgets.MaxEvents, "max-events", budgets.MaxEvents, "maximum actor events")
	flags.Int64Var(&budgets.MaxObjects, "max-objects", budgets.MaxObjects, "maximum reachable objects")
	flags.Int64Var(&budgets.MaxObjectBytes, "max-object-bytes", budgets.MaxObjectBytes, "maximum individual object bytes")
	flags.Int64Var(&budgets.MaxAttachmentBytes, "max-attachment-bytes", budgets.MaxAttachmentBytes, "maximum event attachment bytes")
	flags.Int64Var(&budgets.MaxTotalBytes, "max-total-bytes", budgets.MaxTotalBytes, "maximum total reachable bytes")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return usageError("usage: hn replication select [REMOTE] [--actor ACTOR]... [--proposal ID]... [--memory STREAM]... [--all] [--all-trusted] [budgets]")
	}
	selection := ReplicationSelection{
		Version: replicationSelectionVersion, Remote: remote, Actors: actors, Proposals: proposals, Memories: memories, All: *all,
		AllTrusted: *allTrusted, Budgets: budgets,
	}
	if selection.AllTrusted {
		// Local validation runs before the network identification so the
		// cheaper error wins and a refused selection never contacts the remote.
		if err := validateReplicationSelection(selection); err != nil {
			return err
		}
		if identified, _ := serveRemoteIdentifies(remote); !identified {
			return fmt.Errorf("remote %s does not identify as hn serve; --all-trusted requires an hn serve remote", remote)
		}
	}
	if err := saveReplicationSelection(selection); err != nil {
		return err
	}
	fmt.Printf("Saved replication selection for %s\n", remote)
	return printReplicationSelection(selection, true)
}

func cmdReplicationShow(args []string) error {
	remote, args, err := splitReplicationRemote(args)
	if err != nil {
		return err
	}
	if len(args) != 0 {
		return usageError("usage: hn replication show [REMOTE]")
	}
	selection, explicit, err := loadReplicationSelection(remote)
	if err != nil {
		return err
	}
	return printReplicationSelection(selection, explicit)
}

func printReplicationSelection(selection ReplicationSelection, explicit bool) error {
	fmt.Printf("remote: %s\n", selection.Remote)
	fmt.Printf("saved: %t\n", explicit)
	fmt.Printf("compatibility-all: %t\n", selection.All)
	if selection.AllTrusted {
		fmt.Println("all-trusted: true")
	}
	for _, actor := range selection.Actors {
		fmt.Printf("actor: %s\n", actor)
	}
	for _, proposal := range selection.Proposals {
		fmt.Printf("proposal: %s\n", proposal)
	}
	for _, stream := range selection.Memories {
		fmt.Printf("memory: %s\n", stream)
	}
	fmt.Printf("max-events: %d\n", selection.Budgets.MaxEvents)
	fmt.Printf("max-objects: %d\n", selection.Budgets.MaxObjects)
	fmt.Printf("max-object-bytes: %d\n", selection.Budgets.MaxObjectBytes)
	fmt.Printf("max-attachment-bytes: %d\n", selection.Budgets.MaxAttachmentBytes)
	fmt.Printf("max-total-bytes: %d\n", selection.Budgets.MaxTotalBytes)
	return nil
}

type ReplicationMeasurements struct {
	Events                 int64
	Objects                int64
	LargestObjectBytes     int64
	LargestAttachmentBytes int64
	TotalBytes             int64
}

func enforceReplicationBudgets(selectionID string, budgets ReplicationBudgets, measured ReplicationMeasurements) error {
	checks := []struct {
		name       string
		configured int64
		measured   int64
	}{
		{"max-events", budgets.MaxEvents, measured.Events},
		{"max-objects", budgets.MaxObjects, measured.Objects},
		{"max-object-bytes", budgets.MaxObjectBytes, measured.LargestObjectBytes},
		{"max-attachment-bytes", budgets.MaxAttachmentBytes, measured.LargestAttachmentBytes},
		{"max-total-bytes", budgets.MaxTotalBytes, measured.TotalBytes},
	}
	for _, check := range checks {
		if check.measured > check.configured {
			return fmt.Errorf("selection %s exceeded %s budget: configured=%d measured=%d", selectionID, check.name, check.configured, check.measured)
		}
	}
	return nil
}

func safeDiagnostic(value string) string {
	return strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return ' '
		}
		return character
	}, value)
}
