package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	identityRecordVersion   = 1
	identityRotationVersion = 1
	maxIdentityStateBytes   = 64 * 1024

	identityLifecycleAvailable = "available"

	identityRelationshipDevice    = "device"
	identityRelationshipSuccessor = "successor"
)

var (
	errNoIdentity       = errors.New("no identity")
	identityStorageHook func(step string) error
)

type identityKeyringLayout struct {
	root            string
	legacy          string
	identities      string
	active          string
	rotation        string
	rotationCommand string
}

type identityRecord struct {
	Version    int    `json:"version"`
	Actor      string `json:"actor"`
	Name       string `json:"name"`
	PublicKey  string `json:"publicKey"`
	PrivateKey string `json:"privateKey"`
	Lifecycle  string `json:"lifecycle"`
}

type identityRecordMetadata struct {
	Actor     string
	Name      string
	PublicKey string
	Lifecycle string
	Active    bool
}

type identityRotationState struct {
	Version            int    `json:"version"`
	PredecessorActor   string `json:"predecessorActor"`
	TargetActor        string `json:"targetActor"`
	Relationship       string `json:"relationship"`
	AuthorizationEvent string `json:"authorizationEvent,omitempty"`
	AcceptanceEvent    string `json:"acceptanceEvent,omitempty"`
}

// identityKeyringPaths locates the private keyring. It lives in the Git
// common directory, so every linked worktree of a clone signs as that
// clone's actor. A linked worktree that already holds identity state in its
// own private Git directory, written by releases that kept the keyring
// there, keeps using that keyring as a whole: the two locations are never
// mixed, so the active pointer, records and rotation journals always come
// from one place.
func identityKeyringPaths() (identityKeyringLayout, error) {
	gitDir, commonDir, err := identityGitDirectories()
	if err != nil {
		return identityKeyringLayout{}, err
	}
	worktree := identityKeyringLayoutAt(filepath.Join(gitDir, "hn"))
	if commonDir == gitDir {
		return worktree, nil
	}
	held, err := identityKeyringPresent(worktree)
	if err != nil {
		return identityKeyringLayout{}, err
	}
	if held {
		return worktree, nil
	}
	return identityKeyringLayoutAt(filepath.Join(commonDir, "hn")), nil
}

// identityGitDirectories returns the physical Git directory and Git common
// directory of the current repository, honouring GIT_DIR and GIT_COMMON_DIR.
// Both come from one rev-parse. Git prints a relative common directory
// relative to its physical working directory, so a relative answer is
// resolved against the physical working directory rather than a logical
// one that may run through a symbolic link, and both answers are resolved
// to physical paths so a main checkout, a bare repository and a linked
// worktree compare reliably.
func identityGitDirectories() (string, string, error) {
	output, err := gitOutput("rev-parse", "--absolute-git-dir", "--git-common-dir")
	if err != nil {
		return "", "", fmt.Errorf("not inside a Git repository")
	}
	lines := strings.Split(strings.TrimSuffix(string(output), "\n"), "\n")
	if len(lines) != 2 || lines[0] == "" || lines[1] == "" || !filepath.IsAbs(lines[0]) {
		return "", "", fmt.Errorf("locate the Git common directory: unexpected git rev-parse output")
	}
	gitDir, commonDir := lines[0], lines[1]
	if !filepath.IsAbs(commonDir) {
		working, err := os.Getwd()
		if err != nil {
			return "", "", fmt.Errorf("locate the Git common directory: %w", err)
		}
		working, err = filepath.EvalSymlinks(working)
		if err != nil {
			return "", "", fmt.Errorf("locate the Git common directory: %w", err)
		}
		commonDir = filepath.Join(working, commonDir)
	}
	physicalGitDir, err := filepath.EvalSymlinks(gitDir)
	if err != nil {
		return "", "", fmt.Errorf("locate the Git directory: %w", err)
	}
	physicalCommonDir, err := filepath.EvalSymlinks(commonDir)
	if err != nil {
		return "", "", fmt.Errorf("locate the Git common directory: %w", err)
	}
	return physicalGitDir, physicalCommonDir, nil
}

func identityKeyringLayoutAt(root string) identityKeyringLayout {
	return identityKeyringLayout{
		root:            root,
		legacy:          filepath.Join(root, "identity.json"),
		identities:      filepath.Join(root, "identities"),
		active:          filepath.Join(root, "active"),
		rotation:        filepath.Join(root, "rotation.json"),
		rotationCommand: filepath.Join(root, "rotation-command.json"),
	}
}

// identityKeyringPresent reports whether a keyring location holds identity
// state: an active pointer, a legacy identity, a rotation journal (the
// command journal carries the rotation target's private key) or an identity
// record. The hn directory itself may exist without identity state, because
// other private state such as the memory index lives there too. A location
// that cannot be inspected, or whose directories are symbolic links or not
// directories, is an error, so a worktree never silently falls back past
// keys it may hold.
func identityKeyringPresent(paths identityKeyringLayout) (bool, error) {
	inspect := func(err error) error {
		return fmt.Errorf("inspect worktree identity keyring %s: %w", paths.root, err)
	}
	for _, directory := range []string{paths.root, paths.identities} {
		info, err := os.Lstat(directory)
		if errors.Is(err, os.ErrNotExist) {
			if directory == paths.root {
				return false, nil
			}
			continue
		}
		if err != nil {
			return false, inspect(err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return false, inspect(fmt.Errorf("private identity directory %s is a symbolic link", directory))
		}
		if !info.IsDir() {
			return false, inspect(fmt.Errorf("private identity path %s is not a directory", directory))
		}
	}
	for _, file := range []string{paths.active, paths.legacy, paths.rotation, paths.rotationCommand} {
		if _, err := os.Lstat(file); err == nil {
			return true, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, inspect(err)
		}
	}
	entries, err := os.ReadDir(paths.identities)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, inspect(err)
	}
	for _, entry := range entries {
		// Match listIdentityRecords: only *.json entries are records, so an
		// interrupted write's temporary file does not pin the worktree.
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			return true, nil
		}
	}
	return false, nil
}

func filepathForIdentityRecord(paths identityKeyringLayout, actor string) string {
	return filepath.Join(paths.identities, actor+".json")
}

func ensureIdentityKeyringDirectories(paths identityKeyringLayout) error {
	if err := ensurePrivateDirectory(paths.root); err != nil {
		return err
	}
	return ensurePrivateDirectory(paths.identities)
}

func ensurePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		err = os.Mkdir(path, 0o700)
		if err == nil {
			if err := os.Chmod(path, 0o700); err != nil {
				return fmt.Errorf("protect private identity directory: %w", err)
			}
			return nil
		}
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("create private identity directory: %w", err)
		}
		// A concurrent command, possibly in another worktree of the clone,
		// created it first; validate what it created.
		info, err = os.Lstat(path)
	}
	if err != nil {
		return err
	}
	return validatePrivateDirectory(path, info)
}

func validatePrivateDirectory(path string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("private identity directory %s is a symbolic link", path)
	}
	if !info.IsDir() {
		return fmt.Errorf("private identity path %s is not a directory", path)
	}
	if info.Mode().Perm() != 0o700 {
		return fmt.Errorf("private identity directory %s has unsafe permissions", path)
	}
	return nil
}

func readPrivateFile(path string) ([]byte, error) {
	return readPrivateFileBounded(path, maxIdentityStateBytes)
}

func readPrivateFileBounded(path string, maximum int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("private identity file %s is a symbolic link", path)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("private identity path %s is not a regular file", path)
	}
	if info.Mode().Perm() != 0o600 {
		return nil, fmt.Errorf("private identity file %s has unsafe permissions", path)
	}
	if info.Size() > maximum {
		return nil, fmt.Errorf("private identity file %s is too large", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(contents)) > maximum {
		return nil, fmt.Errorf("private identity file %s is too large", path)
	}
	return contents, nil
}

func decodePrivateJSON(contents []byte, target any, description string) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("read %s: %w", description, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("read %s: unexpected trailing JSON", description)
		}
		return fmt.Errorf("read %s: %w", description, err)
	}
	return nil
}

func writePrivateFileAtomic(path string, contents []byte) error {
	return writePrivateFileAtomicWithReplacement(path, contents, false)
}

func replacePrivateFileAtomic(path string, contents []byte) error {
	return writePrivateFileAtomicWithReplacement(path, contents, true)
}

func writePrivateFileAtomicWithReplacement(path string, contents []byte, replaceInvalid bool) error {
	directory := filepath.Dir(path)
	if info, err := os.Lstat(path); err == nil && !replaceInvalid {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("private identity file %s is a symbolic link", path)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("private identity path %s is not a regular file", path)
		}
		if info.Mode().Perm() != 0o600 {
			return fmt.Errorf("private identity file %s has unsafe permissions", path)
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	temporary, err := os.CreateTemp(directory, ".hn-private-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			_ = temporary.Close()
		}
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	closed = true
	if err := os.Chmod(temporaryPath, 0o600); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return syncPrivateDirectory(directory)
}

// createPrivateFileExclusive durably creates path with contents and fails
// with an error matching os.ErrExist when path already exists. Readers see
// either no file or the complete contents: the bytes are written to a
// temporary file first and published with a hard link, which, unlike a
// rename, never replaces an existing file.
//
// The caller must hold the identity keyring lock, which every hn command
// that writes path also holds. On a file system without hard links the
// publication therefore falls back to an absence check and an atomic rename
// inside that lock, where no other hn command can create path between the
// check and the rename.
func createPrivateFileExclusive(path string, contents []byte) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".hn-private-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			_ = temporary.Close()
		}
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	closed = true
	if err := linkPrivateFile(temporaryPath, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return err
		}
		if _, statErr := os.Lstat(path); statErr == nil {
			return fmt.Errorf("create private identity file %s: %w", path, os.ErrExist)
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
		if err := runIdentityStorageHook("before-active-fallback-publish"); err != nil {
			return err
		}
		if err := os.Rename(temporaryPath, path); err != nil {
			return err
		}
	}
	return syncPrivateDirectory(directory)
}

// linkPrivateFile hard-links temporary to path. Tests on a file system with
// hard links simulate one without them with HN_TEST_IDENTITY_NO_HARD_LINKS.
func linkPrivateFile(temporary, path string) error {
	if os.Getenv("HN_INTERNAL_TESTING") == "1" && os.Getenv("HN_TEST_IDENTITY_NO_HARD_LINKS") == "1" {
		return &os.LinkError{Op: "link", Old: temporary, New: path, Err: errors.ErrUnsupported}
	}
	return os.Link(temporary, path)
}

const identityKeyringLockName = "keyring.lock"

var (
	errIdentityKeyringBusy = errors.New("the identity keyring is busy")

	// heldIdentityKeyringLocks counts this process's holds of each keyring
	// lock, so a command that holds the lock, such as a rotation, can call
	// helpers that take it again. flock(2) locks belong to an open file
	// description, so a second open in the same process would otherwise
	// wait for the first.
	heldIdentityKeyringLocks = struct {
		sync.Mutex
		files map[string]*os.File
		holds map[string]int
	}{files: map[string]*os.File{}, holds: map[string]int{}}
)

// identityKeyringLockWait is how long a short keyring update, such as
// publishing the first active pointer, waits for another command's lock.
// Tests may change it with HN_TEST_IDENTITY_LOCK_WAIT.
func identityKeyringLockWait() time.Duration {
	if os.Getenv("HN_INTERNAL_TESTING") == "1" {
		if value, ok := os.LookupEnv("HN_TEST_IDENTITY_LOCK_WAIT"); ok {
			if wait, err := time.ParseDuration(value); err == nil && wait >= 0 {
				return wait
			}
		}
	}
	return 30 * time.Second
}

// lockIdentityKeyring takes an exclusive flock(2) on keyring.lock in the
// keyring's root, waiting up to wait, and returns the function that releases
// it. The lock is cross-process and keyring-scoped: commands in every
// worktree that share a keyring contend for one lock, while a linked
// worktree that keeps a legacy keyring has its own. The kernel releases it
// when a process exits, so an interrupted command never leaves it held. A
// lock that stays busy past wait returns an error matching
// errIdentityKeyringBusy.
func lockIdentityKeyring(paths identityKeyringLayout, wait time.Duration) (func(), error) {
	if err := ensureIdentityKeyringDirectories(paths); err != nil {
		return nil, err
	}
	path := filepath.Join(paths.root, identityKeyringLockName)
	held := &heldIdentityKeyringLocks
	held.Lock()
	defer held.Unlock()
	release := func() {
		held.Lock()
		defer held.Unlock()
		held.holds[path]--
		if held.holds[path] == 0 {
			_ = held.files[path].Close()
			delete(held.files, path)
			delete(held.holds, path)
		}
	}
	if held.holds[path] > 0 {
		held.holds[path]++
		return release, nil
	}
	file, err := openPrivateLockFile(path)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(wait)
	for {
		locked, err := tryServeFileLock(file, serveLockExclusive)
		if err != nil {
			_ = file.Close()
			return nil, fmt.Errorf("lock identity keyring %s: %w", paths.root, err)
		}
		if locked {
			break
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			_ = file.Close()
			return nil, fmt.Errorf("%w: another hn command holds %s", errIdentityKeyringBusy, path)
		}
		time.Sleep(min(25*time.Millisecond, remaining))
	}
	held.files[path] = file
	held.holds[path] = 1
	return release, nil
}

// openPrivateLockFile opens or creates a mode-0600 regular lock file without
// following a symbolic link at its final component.
func openPrivateLockFile(path string) (*os.File, error) {
	for attempt := 0; attempt < 3; attempt++ {
		before, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
			if errors.Is(err, os.ErrExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if err := file.Chmod(0o600); err != nil {
				_ = file.Close()
				return nil, err
			}
			return file, nil
		}
		if err != nil {
			return nil, err
		}
		if before.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("private identity lock %s is a symbolic link", path)
		}
		if !before.Mode().IsRegular() {
			return nil, fmt.Errorf("private identity lock %s is not a regular file", path)
		}
		if before.Mode().Perm() != 0o600 {
			return nil, fmt.Errorf("private identity lock %s has unsafe permissions", path)
		}
		file, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			return nil, err
		}
		after, err := file.Stat()
		if err != nil {
			_ = file.Close()
			return nil, err
		}
		if os.SameFile(before, after) {
			return file, nil
		}
		_ = file.Close()
	}
	return nil, fmt.Errorf("private identity lock %s changed while it was being opened", path)
}

func syncPrivateDirectory(directory string) error {
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer directoryHandle.Close()
	if err := directoryHandle.Sync(); err != nil {
		return err
	}
	return nil
}

func identityFromRecord(record identityRecord) *Identity {
	return &Identity{
		Actor:      record.Actor,
		Name:       record.Name,
		PublicKey:  record.PublicKey,
		PrivateKey: record.PrivateKey,
	}
}

func recordFromIdentity(identity *Identity, lifecycle string) identityRecord {
	return identityRecord{
		Version:    identityRecordVersion,
		Actor:      identity.Actor,
		Name:       identity.Name,
		PublicKey:  identity.PublicKey,
		PrivateKey: identity.PrivateKey,
		Lifecycle:  lifecycle,
	}
}

func storeIdentityRecord(identity *Identity, lifecycle string) (string, error) {
	if err := validateIdentity(identity); err != nil {
		return "", err
	}
	if lifecycle != identityLifecycleAvailable {
		return "", fmt.Errorf("unsupported local identity lifecycle %q", lifecycle)
	}
	paths, err := identityKeyringPaths()
	if err != nil {
		return "", err
	}
	if err := ensureIdentityKeyringDirectories(paths); err != nil {
		return "", err
	}
	path := filepathForIdentityRecord(paths, identity.Actor)
	want := recordFromIdentity(identity, lifecycle)
	existing, err := loadIdentityRecord(identity.Actor)
	if err == nil {
		got := recordFromIdentity(existing, lifecycle)
		if got != want {
			return "", fmt.Errorf("identity record for actor %s already contains different key material or metadata", identity.Actor)
		}
		return path, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	contents, err := json.MarshalIndent(want, "", "  ")
	if err != nil {
		return "", err
	}
	if err := writePrivateFileAtomic(path, append(contents, '\n')); err != nil {
		return "", err
	}
	return path, nil
}

func loadIdentityRecord(actor string) (*Identity, error) {
	if !validActorFingerprint(actor) {
		return nil, fmt.Errorf("invalid identity actor fingerprint")
	}
	paths, err := identityKeyringPaths()
	if err != nil {
		return nil, err
	}
	if err := checkPrivateDirectory(paths.root); err != nil {
		return nil, err
	}
	if err := checkPrivateDirectory(paths.identities); err != nil {
		return nil, err
	}
	contents, err := readPrivateFile(filepathForIdentityRecord(paths, actor))
	if err != nil {
		return nil, err
	}
	var record identityRecord
	if err := decodePrivateJSON(contents, &record, "identity record"); err != nil {
		return nil, err
	}
	if record.Version != identityRecordVersion {
		return nil, fmt.Errorf("unsupported identity record version %d", record.Version)
	}
	if record.Lifecycle != identityLifecycleAvailable {
		return nil, fmt.Errorf("unsupported local identity lifecycle %q", record.Lifecycle)
	}
	if record.Actor != actor {
		return nil, fmt.Errorf("identity record actor does not match its path")
	}
	identity := identityFromRecord(record)
	if err := validateIdentity(identity); err != nil {
		return nil, err
	}
	return identity, nil
}

func listIdentityRecords() ([]identityRecordMetadata, error) {
	paths, err := identityKeyringPaths()
	if err != nil {
		return nil, err
	}
	if err := checkPrivateDirectory(paths.root); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	if err := checkPrivateDirectory(paths.identities); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	active, activeErr := loadActiveActor()
	if activeErr != nil && !errors.Is(activeErr, os.ErrNotExist) {
		return nil, activeErr
	}
	entries, err := os.ReadDir(paths.identities)
	if err != nil {
		return nil, err
	}
	metadata := make([]identityRecordMetadata, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		actor := strings.TrimSuffix(entry.Name(), ".json")
		identity, err := loadIdentityRecord(actor)
		if err != nil {
			return nil, err
		}
		metadata = append(metadata, identityRecordMetadata{
			Actor:     identity.Actor,
			Name:      identity.Name,
			PublicKey: identity.PublicKey,
			Lifecycle: identityLifecycleAvailable,
			Active:    activeErr == nil && active == identity.Actor,
		})
	}
	sort.Slice(metadata, func(i, j int) bool { return metadata[i].Actor < metadata[j].Actor })
	return metadata, nil
}

func loadActiveActor() (string, error) {
	paths, err := identityKeyringPaths()
	if err != nil {
		return "", err
	}
	if err := checkPrivateDirectory(paths.root); err != nil {
		return "", err
	}
	contents, err := readPrivateFile(paths.active)
	if err != nil {
		return "", err
	}
	actor := strings.TrimSpace(string(contents))
	if !validActorFingerprint(actor) || string(contents) != actor+"\n" {
		return "", fmt.Errorf("active identity contains an invalid actor fingerprint")
	}
	return actor, nil
}

func initializeActiveIdentity(actor string) error {
	if _, err := loadIdentityRecord(actor); err != nil {
		return fmt.Errorf("cannot activate identity: %w", err)
	}
	paths, err := identityKeyringPaths()
	if err != nil {
		return err
	}
	// Every worktree of a clone shares this pointer, so two first-time
	// initializations can race. The keyring lock, which every writer of the
	// pointer holds, serializes them: the second sees the winner like any
	// later caller.
	release, err := lockIdentityKeyring(paths, identityKeyringLockWait())
	if err != nil {
		return err
	}
	defer release()
	current, err := loadActiveActor()
	if err == nil {
		if current == actor {
			return nil
		}
		return fmt.Errorf("active identity already exists for actor %s", current)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	err = createPrivateFileExclusive(paths.active, []byte(actor+"\n"))
	if !errors.Is(err, os.ErrExist) {
		return err
	}
	current, err = loadActiveActor()
	if err != nil {
		return err
	}
	if current != actor {
		return fmt.Errorf("active identity already exists for actor %s", current)
	}
	return nil
}

func replaceActiveIdentity(actor, expected string) error {
	if _, err := loadIdentityRecord(actor); err != nil {
		return fmt.Errorf("cannot activate identity: %w", err)
	}
	paths, err := identityKeyringPaths()
	if err != nil {
		return err
	}
	release, err := lockIdentityKeyring(paths, identityKeyringLockWait())
	if err != nil {
		return err
	}
	defer release()
	current, err := loadActiveActor()
	if err != nil {
		return err
	}
	if current == actor {
		return nil
	}
	if current != expected {
		return fmt.Errorf("active identity changed from expected actor %s", expected)
	}
	return writePrivateFileAtomic(paths.active, []byte(actor+"\n"))
}

func loadLegacyIdentity() (*Identity, error) {
	paths, err := identityKeyringPaths()
	if err != nil {
		return nil, err
	}
	if err := checkPrivateDirectory(paths.root); err != nil {
		return nil, err
	}
	contents, err := readPrivateFile(paths.legacy)
	if err != nil {
		return nil, err
	}
	var identity Identity
	if err := decodePrivateJSON(contents, &identity, "legacy identity"); err != nil {
		return nil, err
	}
	if err := validateIdentity(&identity); err != nil {
		return nil, err
	}
	return &identity, nil
}

func loadActiveIdentity() (*Identity, error) {
	actor, err := loadActiveActor()
	if err == nil {
		return loadIdentityRecord(actor)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	legacy, err := loadLegacyIdentity()
	if errors.Is(err, os.ErrNotExist) {
		return nil, errNoIdentity
	}
	if err != nil {
		return nil, err
	}
	if _, err := storeIdentityRecord(legacy, identityLifecycleAvailable); err != nil {
		return nil, err
	}
	if err := runIdentityStorageHook("migration-record-durable"); err != nil {
		return nil, err
	}
	if err := initializeActiveIdentity(legacy.Actor); err != nil {
		return nil, err
	}
	return loadIdentityRecord(legacy.Actor)
}

func validateRotationState(state identityRotationState, requireComplete bool) error {
	if state.Version != identityRotationVersion {
		return fmt.Errorf("unsupported identity rotation version %d", state.Version)
	}
	if !validActorFingerprint(state.PredecessorActor) || !validActorFingerprint(state.TargetActor) {
		return fmt.Errorf("identity rotation has an invalid actor fingerprint")
	}
	if state.PredecessorActor == state.TargetActor {
		return fmt.Errorf("identity rotation target must differ from its predecessor")
	}
	if state.Relationship != identityRelationshipDevice && state.Relationship != identityRelationshipSuccessor {
		return fmt.Errorf("unsupported identity relationship %q", state.Relationship)
	}
	if state.AuthorizationEvent != "" && !validEventID(state.AuthorizationEvent) {
		return fmt.Errorf("identity rotation has an invalid authorization event ID")
	}
	if state.AcceptanceEvent != "" && !validEventID(state.AcceptanceEvent) {
		return fmt.Errorf("identity rotation has an invalid acceptance event ID")
	}
	if state.AcceptanceEvent != "" && state.AuthorizationEvent == "" {
		return fmt.Errorf("identity rotation acceptance requires an authorization")
	}
	if requireComplete && (state.AuthorizationEvent == "" || state.AcceptanceEvent == "") {
		return fmt.Errorf("identity rotation is incomplete")
	}
	return nil
}

func storeIdentityRotation(state identityRotationState) error {
	if err := validateRotationState(state, false); err != nil {
		return err
	}
	if _, err := loadIdentityRecord(state.PredecessorActor); err != nil {
		return fmt.Errorf("read rotation predecessor: %w", err)
	}
	if _, err := loadIdentityRecord(state.TargetActor); err != nil {
		return fmt.Errorf("read rotation target: %w", err)
	}
	paths, err := identityKeyringPaths()
	if err != nil {
		return err
	}
	if err := ensureIdentityKeyringDirectories(paths); err != nil {
		return err
	}
	existing, err := loadIdentityRotation()
	if err == nil {
		if existing.PredecessorActor != state.PredecessorActor || existing.TargetActor != state.TargetActor || existing.Relationship != state.Relationship {
			return fmt.Errorf("another identity rotation is already in progress")
		}
		if existing.AuthorizationEvent != "" {
			if state.AuthorizationEvent != "" && state.AuthorizationEvent != existing.AuthorizationEvent {
				return fmt.Errorf("identity rotation authorization cannot be replaced")
			}
			state.AuthorizationEvent = existing.AuthorizationEvent
		}
		if existing.AcceptanceEvent != "" {
			if state.AcceptanceEvent != "" && state.AcceptanceEvent != existing.AcceptanceEvent {
				return fmt.Errorf("identity rotation acceptance cannot be replaced")
			}
			state.AcceptanceEvent = existing.AcceptanceEvent
		}
		if state == existing {
			return nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	contents, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateFileAtomic(paths.rotation, append(contents, '\n'))
}

func loadIdentityRotation() (identityRotationState, error) {
	paths, err := identityKeyringPaths()
	if err != nil {
		return identityRotationState{}, err
	}
	if err := checkPrivateDirectory(paths.root); err != nil {
		return identityRotationState{}, err
	}
	contents, err := readPrivateFile(paths.rotation)
	if err != nil {
		return identityRotationState{}, err
	}
	var state identityRotationState
	if err := decodePrivateJSON(contents, &state, "identity rotation state"); err != nil {
		return identityRotationState{}, err
	}
	if err := validateRotationState(state, false); err != nil {
		return identityRotationState{}, err
	}
	return state, nil
}

func switchActiveIdentity(state identityRotationState) error {
	if err := validateRotationState(state, true); err != nil {
		return err
	}
	if _, err := loadIdentityRecord(state.TargetActor); err != nil {
		return fmt.Errorf("read rotation target: %w", err)
	}
	active, err := loadActiveActor()
	if err != nil {
		return err
	}
	persisted, err := loadIdentityRotation()
	if errors.Is(err, os.ErrNotExist) && active == state.TargetActor {
		return nil
	}
	if err != nil {
		return err
	}
	if persisted != state {
		return fmt.Errorf("completed identity rotation does not match durable transaction state")
	}
	if active != state.TargetActor {
		if active != state.PredecessorActor {
			return fmt.Errorf("active identity is %s, not rotation predecessor %s", active, state.PredecessorActor)
		}
		if err := runIdentityStorageHook("before-active-switch"); err != nil {
			return err
		}
		if err := replaceActiveIdentity(state.TargetActor, state.PredecessorActor); err != nil {
			return err
		}
	}
	return removeIdentityRotation(state)
}

func removeIdentityRotation(expected identityRotationState) error {
	persisted, err := loadIdentityRotation()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if persisted != expected {
		return fmt.Errorf("completed identity rotation does not match durable transaction state")
	}
	if err := runIdentityStorageHook("before-rotation-cleanup"); err != nil {
		return err
	}
	paths, err := identityKeyringPaths()
	if err != nil {
		return err
	}
	if err := os.Remove(paths.rotation); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := syncPrivateDirectory(paths.root); err != nil {
		return err
	}
	return runIdentityStorageHook("after-rotation-cleanup")
}

func runIdentityStorageHook(step string) error {
	if identityStorageHook != nil {
		return identityStorageHook(step)
	}
	if os.Getenv("HN_INTERNAL_TESTING") != "1" {
		return nil
	}
	// Concurrency tests widen a race window by pausing every process at the
	// same step, so an unserialized interleaving becomes reliable.
	if os.Getenv("HN_TEST_IDENTITY_PAUSE_AT") == step {
		pause, err := time.ParseDuration(os.Getenv("HN_TEST_IDENTITY_PAUSE"))
		if err != nil || pause < 0 {
			pause = time.Second
		}
		time.Sleep(pause)
	}
	if os.Getenv("HN_TEST_ROTATION_INTERRUPT_AFTER") == step {
		return fmt.Errorf("injected rotation interruption after %s", step)
	}
	return nil
}

func checkPrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	return validatePrivateDirectory(path, info)
}

func validActorFingerprint(actor string) bool {
	if len(actor) != 64 || strings.ToLower(actor) != actor {
		return false
	}
	for _, character := range actor {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
