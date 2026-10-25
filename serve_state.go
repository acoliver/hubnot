package main

import (
	"bytes"
	"encoding/base64"
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
	"unicode"
)

// Server state for hn serve. Everything lives below one fixed home,
// $HOME/.hn-serve, owned by the unprivileged server account. The layout and
// file schemas are specified in
// kitty-specs/hn-serve-01M5P0NP/contracts/server-state.md.

const (
	serveHomeDirectoryName     = ".hn-serve"
	serveStateFileLimit        = 16 << 20
	servePushLogLimit          = 64 << 20
	serveHookScriptLimit       = 4096
	serveTransientEntryLimit   = 100_000
	serveRegistryVersion       = 1
	serveRepoConfigVersion     = 1
	serveBranchOwnersVersion   = 1
	serveRepositoryNameMaximum = 100
	serveDefaultProtectedRef   = "refs/heads/main"
	serveDefaultLFSQuotaBytes  = 1 << 30
	serveDefaultLockWait       = 120 * time.Second
	serveLockPollInterval      = 10 * time.Millisecond
	serveFingerprintPrefix     = "SHA256:"
	serveBranchRefPrefix       = "refs/heads/"
	serveVerdictAccepted       = "accepted"
	serveVerdictRejected       = "rejected"
)

// serveHookNames lists the generated hook scripts in byte order, which is
// also the order validation reports them in.
var serveHookNames = []string{"post-receive", "pre-receive"}

// errServeLockBusy reports that a repository lock stayed held for the whole
// wait. WP06 maps it to the repository-busy refusal.
var errServeLockBusy = errors.New("hn serve repository lock is busy")

// serveValidationMode selects what validateServeLayout may do. The zero value
// is the read-only session mode so a forgotten mode can never write.
type serveValidationMode int

const (
	// serveSessionValidation is used by hn serve git and hn serve hook. It
	// never writes; a missing or stale hook script is a defect.
	serveSessionValidation serveValidationMode = iota
	// serveAdministrationValidation is used by hn serve repo and hn serve key.
	// It regenerates the hook scripts for the running hn binary first.
	serveAdministrationValidation
)

// serveStateDefectError is a fail-closed layout or state defect. Its message
// always names the offending path.
type serveStateDefectError struct {
	Path   string
	Reason string
}

func (defect *serveStateDefectError) Error() string {
	return "hn serve state defect: " + defect.Path + ": " + defect.Reason
}

func serveDefect(path, format string, args ...any) error {
	return &serveStateDefectError{Path: path, Reason: fmt.Sprintf(format, args...)}
}

func serveStateError(path string, err error) error {
	return fmt.Errorf("hn serve state %s: %w", path, err)
}

// serveLayout holds every absolute path the server uses. Per-repository
// helpers take a name the caller has already checked with
// validServeRepositoryName.
type serveLayout struct {
	Home           string
	Root           string
	Hooks          string
	Repositories   string
	State          string
	Registry       string
	Tmp            string
	Repos          string
	SSHDir         string
	AuthorizedKeys string
}

func (layout serveLayout) RepoGitDir(name string) string {
	return filepath.Join(layout.Repositories, name+".git")
}

func (layout serveLayout) RepoStateDir(name string) string {
	return filepath.Join(layout.Repos, name)
}

func (layout serveLayout) RepoConfig(name string) string {
	return filepath.Join(layout.RepoStateDir(name), "config.json")
}

func (layout serveLayout) RepoBranches(name string) string {
	return filepath.Join(layout.RepoStateDir(name), "branches.json")
}

func (layout serveLayout) RepoPushLog(name string) string {
	return filepath.Join(layout.RepoStateDir(name), "push.log")
}

func (layout serveLayout) RepoLock(name string) string {
	return filepath.Join(layout.RepoStateDir(name), "lock")
}

func (layout serveLayout) RepoLFSLock(name string) string {
	return filepath.Join(layout.RepoStateDir(name), "lfs.lock")
}

func (layout serveLayout) RepoLFSDir(name string) string {
	return filepath.Join(layout.RepoStateDir(name), "lfs")
}

func (layout serveLayout) RepoLFSIncoming(name string) string {
	return filepath.Join(layout.RepoLFSDir(name), "incoming")
}

func (layout serveLayout) RepoLFSObjects(name string) string {
	return filepath.Join(layout.RepoLFSDir(name), "objects")
}

// resolveServeLayout builds the layout from HOME without touching the disk.
func resolveServeLayout() (serveLayout, error) {
	home := os.Getenv("HOME")
	if home == "" {
		return serveLayout{}, errors.New("hn serve requires HOME to name the server account's home directory")
	}
	if !filepath.IsAbs(home) {
		return serveLayout{}, fmt.Errorf("hn serve requires an absolute HOME; got %q", home)
	}
	home = filepath.Clean(home)
	root := filepath.Join(home, serveHomeDirectoryName)
	state := filepath.Join(root, "state")
	sshDir := filepath.Join(home, ".ssh")
	return serveLayout{
		Home:           home,
		Root:           root,
		Hooks:          filepath.Join(root, "hooks"),
		Repositories:   filepath.Join(root, "repositories"),
		State:          state,
		Registry:       filepath.Join(state, "registry.json"),
		Tmp:            filepath.Join(state, "tmp"),
		Repos:          filepath.Join(state, "repos"),
		SSHDir:         sshDir,
		AuthorizedKeys: filepath.Join(sshDir, "authorized_keys"),
	}, nil
}

// ensureServeLayout creates the fixed directories with mode 0700. Only
// administration commands call it; sessions require an existing layout.
func ensureServeLayout(layout serveLayout) error {
	for _, directory := range []string{layout.Root, layout.Hooks, layout.Repositories, layout.State, layout.Tmp, layout.Repos} {
		info, err := os.Lstat(directory)
		switch {
		case err == nil:
			if err := checkServeDirectoryInfo(directory, info); err != nil {
				return err
			}
		case errors.Is(err, os.ErrNotExist):
			if err := ensurePrivateDirectory(directory); err != nil {
				return serveStateError(directory, err)
			}
		default:
			return serveStateError(directory, err)
		}
	}
	return nil
}

// validateServeLayout enforces every layout rule and stops at the first
// defect, walking names in sorted order so messages are stable. In
// administration mode it first regenerates the hook scripts; in session mode
// it only reads.
func validateServeLayout(layout serveLayout, mode serveValidationMode) error {
	hnPath, err := serveExecutablePath()
	if err != nil {
		return err
	}
	if mode == serveAdministrationValidation {
		if err := ensureServeHooks(layout, hnPath); err != nil {
			return err
		}
	}
	return checkServeLayout(layout, hnPath)
}

func checkServeLayout(layout serveLayout, hnPath string) error {
	// WP02's authorized_keys writer validates .ssh fully; here a symlinked
	// .ssh is the only defect, and a missing one is allowed.
	if info, err := os.Lstat(layout.SSHDir); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return serveDefect(layout.SSHDir, "is a symbolic link")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return serveDefect(layout.SSHDir, "cannot inspect: %v", err)
	}
	if err := requireServeDirectory(layout.Root); err != nil {
		return err
	}
	if _, err := listServeDirectory(layout.Root, "hooks", "repositories", "state"); err != nil {
		return err
	}
	if err := checkServeHooks(layout, hnPath); err != nil {
		return err
	}
	repositories, err := checkServeRepositories(layout)
	if err != nil {
		return err
	}
	states, err := checkServeStateDirectory(layout)
	if err != nil {
		return err
	}
	return checkServeRepositoryPairs(layout, repositories, states)
}

func checkServeHooks(layout serveLayout, hnPath string) error {
	if err := requireServeDirectory(layout.Hooks); err != nil {
		return err
	}
	if _, err := listServeDirectory(layout.Hooks, serveHookNames...); err != nil {
		return err
	}
	for _, name := range serveHookNames {
		path := filepath.Join(layout.Hooks, name)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			return serveDefect(path, "hook script does not exist; run any hn serve repo or hn serve key command to regenerate it")
		}
		if err != nil {
			return serveDefect(path, "cannot inspect: %v", err)
		}
		if err := checkServeRegularInfo(path, info, 0o700, serveHookScriptLimit); err != nil {
			return err
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return serveDefect(path, "cannot read: %v", err)
		}
		if !bytes.Equal(contents, serveHookScript(name, hnPath)) {
			return serveDefect(path, "hook script differs from the generated script; run any hn serve repo or hn serve key command to regenerate it")
		}
	}
	return nil
}

// checkServeRepositories returns the hosted names found as NAME.git real
// directories below repositories/. It does not look inside them.
func checkServeRepositories(layout serveLayout) (map[string]bool, error) {
	if err := requireServeDirectory(layout.Repositories); err != nil {
		return nil, err
	}
	entries, err := listServeDirectory(layout.Repositories)
	if err != nil {
		return nil, err
	}
	names := make(map[string]bool, len(entries))
	for _, entry := range entries {
		path := filepath.Join(layout.Repositories, entry)
		name, ok := strings.CutSuffix(entry, ".git")
		if !ok || !validServeRepositoryName(name) {
			return nil, serveDefect(path, "unexpected entry; not a hosted repository directory")
		}
		info, err := os.Lstat(path)
		if err != nil {
			return nil, serveDefect(path, "cannot inspect: %v", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, serveDefect(path, "is a symbolic link")
		}
		if !info.IsDir() {
			return nil, serveDefect(path, "is not a directory")
		}
		names[name] = true
	}
	return names, nil
}

func checkServeStateDirectory(layout serveLayout) (map[string]bool, error) {
	if err := requireServeDirectory(layout.State); err != nil {
		return nil, err
	}
	if _, err := listServeDirectory(layout.State, "registry.json", "repos", "tmp"); err != nil {
		return nil, err
	}
	if err := checkServeOptionalJSON(layout.Registry, func(contents []byte) error {
		_, err := decodeServeRegistry(contents)
		return err
	}); err != nil {
		return nil, err
	}
	if err := requireServeDirectory(layout.Tmp); err != nil {
		return nil, err
	}
	if err := checkServeTransientTree(layout.Tmp, serveTransientEntryLimit); err != nil {
		return nil, err
	}
	if err := requireServeDirectory(layout.Repos); err != nil {
		return nil, err
	}
	entries, err := listServeDirectory(layout.Repos)
	if err != nil {
		return nil, err
	}
	names := make(map[string]bool, len(entries))
	for _, name := range entries {
		path := filepath.Join(layout.Repos, name)
		if !validServeRepositoryName(name) {
			return nil, serveDefect(path, "unexpected entry; not a valid repository name")
		}
		if err := requireServeDirectory(path); err != nil {
			return nil, err
		}
		if err := checkServeRepositoryState(layout, name); err != nil {
			return nil, err
		}
		names[name] = true
	}
	return names, nil
}

func checkServeRepositoryState(layout serveLayout, name string) error {
	directory := layout.RepoStateDir(name)
	if _, err := listServeDirectory(directory, "branches.json", "config.json", "lfs", "lfs.lock", "lock", "push.log"); err != nil {
		return err
	}
	configPath := layout.RepoConfig(name)
	if _, err := os.Lstat(configPath); errors.Is(err, os.ErrNotExist) {
		return serveDefect(configPath, "repository configuration does not exist")
	}
	if err := checkServeOptionalJSON(configPath, func(contents []byte) error {
		_, err := decodeServeRepoConfig(contents, name)
		return err
	}); err != nil {
		return err
	}
	if err := checkServeOptionalJSON(layout.RepoBranches(name), func(contents []byte) error {
		_, err := decodeServeBranchOwners(contents)
		return err
	}); err != nil {
		return err
	}
	// push.log is informational and never read by the server; only its
	// type, mode and size are checked.
	for _, file := range []struct {
		path  string
		limit int64
	}{
		{layout.RepoLFSLock(name), serveStateFileLimit},
		{layout.RepoLock(name), serveStateFileLimit},
		{layout.RepoPushLog(name), servePushLogLimit},
	} {
		if err := checkServeOptionalFile(file.path, file.limit); err != nil {
			return err
		}
	}
	return checkServeRepositoryLFS(layout, name)
}

func checkServeRepositoryLFS(layout serveLayout, name string) error {
	directory := layout.RepoLFSDir(name)
	present, err := checkServeOptionalDirectory(directory)
	if err != nil || !present {
		return err
	}
	if _, err := listServeDirectory(directory, "incoming", "objects"); err != nil {
		return err
	}
	for _, subtree := range []string{layout.RepoLFSIncoming(name), layout.RepoLFSObjects(name)} {
		present, err := checkServeOptionalDirectory(subtree)
		if err != nil {
			return err
		}
		if present {
			if err := checkServeTransientTree(subtree, serveTransientEntryLimit); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkServeRepositoryPairs(layout serveLayout, repositories, states map[string]bool) error {
	for _, name := range sortedServeNames(repositories) {
		if !states[name] {
			return serveDefect(layout.RepoGitDir(name), "hosted repository has no matching state directory %s", layout.RepoStateDir(name))
		}
	}
	for _, name := range sortedServeNames(states) {
		if !repositories[name] {
			return serveDefect(layout.RepoStateDir(name), "repository state has no matching repository %s", layout.RepoGitDir(name))
		}
	}
	return nil
}

// checkServeTransientTree walks a subtree that holds transient or
// content-addressed files (state/tmp, lfs/incoming, lfs/objects). It never
// follows symlinks and never reads contents or checks names; WP14 checks
// those at its own use boundaries. The walk stops after limit entries so a
// hostile directory cannot stall startup.
func checkServeTransientTree(root string, limit int) error {
	count := 0
	var walk func(directory string) error
	walk = func(directory string) error {
		entries, err := os.ReadDir(directory)
		if err != nil {
			return serveDefect(directory, "cannot list directory: %v", err)
		}
		for _, entry := range entries {
			count++
			if count > limit {
				return serveDefect(root, "holds more than %d entries", limit)
			}
			path := filepath.Join(directory, entry.Name())
			info, err := os.Lstat(path)
			if err != nil {
				return serveDefect(path, "cannot inspect: %v", err)
			}
			mode := info.Mode()
			switch {
			case mode&os.ModeSymlink != 0:
				return serveDefect(path, "is a symbolic link")
			case mode.IsDir():
				if err := checkServeDirectoryInfo(path, info); err != nil {
					return err
				}
				if err := walk(path); err != nil {
					return err
				}
			case mode.IsRegular():
				if mode.Perm() != 0o600 {
					return serveDefect(path, "file mode %04o, want 0600", mode.Perm())
				}
			default:
				return serveDefect(path, "is a %s, not a regular file or directory", describeServeFileType(mode))
			}
		}
		return nil
	}
	return walk(root)
}

func describeServeFileType(mode os.FileMode) string {
	switch {
	case mode&os.ModeNamedPipe != 0:
		return "named pipe"
	case mode&os.ModeSocket != 0:
		return "socket"
	case mode&os.ModeCharDevice != 0:
		return "character device"
	case mode&os.ModeDevice != 0:
		return "device"
	default:
		return "special file"
	}
}

// listServeDirectory returns the sorted entry names of a directory and, when
// allowed names are given, refuses the first entry outside them.
func listServeDirectory(directory string, allowed ...string) ([]string, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, serveDefect(directory, "cannot list directory: %v", err)
	}
	permitted := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		permitted[name] = true
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if len(allowed) > 0 && !permitted[entry.Name()] {
			return nil, serveDefect(filepath.Join(directory, entry.Name()), "unexpected entry")
		}
		names = append(names, entry.Name())
	}
	return names, nil
}

func requireServeDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return serveDefect(path, "directory does not exist; run any hn serve repo or hn serve key command to create the server layout")
	}
	if err != nil {
		return serveDefect(path, "cannot inspect: %v", err)
	}
	return checkServeDirectoryInfo(path, info)
}

func checkServeOptionalDirectory(path string) (bool, error) {
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return true, requireServeDirectory(path)
}

func checkServeDirectoryInfo(path string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return serveDefect(path, "is a symbolic link")
	}
	if !info.IsDir() {
		return serveDefect(path, "is not a directory")
	}
	if info.Mode().Perm() != 0o700 {
		return serveDefect(path, "directory mode %04o, want 0700", info.Mode().Perm())
	}
	return nil
}

func checkServeRegularInfo(path string, info os.FileInfo, perm os.FileMode, limit int64) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return serveDefect(path, "is a symbolic link")
	}
	if !info.Mode().IsRegular() {
		return serveDefect(path, "is not a regular file")
	}
	if info.Mode().Perm() != perm {
		return serveDefect(path, "file mode %04o, want %04o", info.Mode().Perm(), perm)
	}
	if info.Size() > limit {
		return serveDefect(path, "file is larger than %d bytes", limit)
	}
	return nil
}

func checkServeOptionalFile(path string, limit int64) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return serveDefect(path, "cannot inspect: %v", err)
	}
	return checkServeRegularInfo(path, info, 0o600, limit)
}

func checkServeOptionalJSON(path string, decode func([]byte) error) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return serveDefect(path, "cannot inspect: %v", err)
	}
	if err := checkServeRegularInfo(path, info, 0o600, serveStateFileLimit); err != nil {
		return err
	}
	contents, err := readPrivateFileBounded(path, serveStateFileLimit)
	if err != nil {
		return serveDefect(path, "%v", err)
	}
	if err := decode(contents); err != nil {
		return serveDefect(path, "%v", err)
	}
	return nil
}

func sortedServeNames(set map[string]bool) []string {
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func encodeServeJSON(value any) ([]byte, error) {
	contents, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(contents, '\n'), nil
}

// validServeTimestamp accepts RFC 3339 timestamps in UTC written with Z.
func validServeTimestamp(value string) bool {
	if !strings.HasSuffix(value, "Z") {
		return false
	}
	_, err := time.Parse(time.RFC3339Nano, value)
	return err == nil
}

// Key registry (state/registry.json).

type serveRegistry struct {
	Version int               `json:"version"`
	Keys    []serveKeyBinding `json:"keys"`
}

type serveKeyBinding struct {
	Fingerprint string `json:"fingerprint"`
	PublicKey   string `json:"publicKey"`
	Actor       string `json:"actor"`
	Added       string `json:"added"`
}

func emptyServeRegistry() serveRegistry {
	return serveRegistry{Version: serveRegistryVersion, Keys: []serveKeyBinding{}}
}

// validServeFingerprint checks the shape ssh-keygen -l -E sha256 prints:
// SHA256: and 43 characters of canonical unpadded standard base64 that decode
// to a 32-byte digest.
func validServeFingerprint(value string) bool {
	encoded, ok := strings.CutPrefix(value, serveFingerprintPrefix)
	if !ok || len(encoded) != 43 {
		return false
	}
	for i := 0; i < len(encoded); i++ {
		character := encoded[i]
		if !isServeAlphanumeric(character) && character != '+' && character != '/' {
			return false
		}
	}
	decoded, err := base64.RawStdEncoding.Strict().DecodeString(encoded)
	return err == nil && len(decoded) == 32
}

// validServePublicKeyLine checks only the stored shape: key type and blob
// separated by one space. WP02 owns key parsing; WP05 recomputes the
// fingerprint from this line through WP02's parser when it needs to.
func validServePublicKeyLine(value string) bool {
	fields := strings.Split(value, " ")
	if len(fields) != 2 || fields[0] == "" || fields[1] == "" {
		return false
	}
	for _, character := range value {
		if character != ' ' && (unicode.IsSpace(character) || unicode.IsControl(character)) {
			return false
		}
	}
	return true
}

func validateServeRegistry(registry serveRegistry) error {
	if registry.Version != serveRegistryVersion {
		return fmt.Errorf("unsupported key registry version %d", registry.Version)
	}
	publicKeys := make(map[string]string, len(registry.Keys))
	for index, binding := range registry.Keys {
		if !validServeFingerprint(binding.Fingerprint) {
			return fmt.Errorf("key %d has a malformed fingerprint %q", index, binding.Fingerprint)
		}
		if index > 0 {
			previous := registry.Keys[index-1].Fingerprint
			if binding.Fingerprint == previous {
				return fmt.Errorf("duplicate fingerprint %s", binding.Fingerprint)
			}
			if binding.Fingerprint < previous {
				return fmt.Errorf("keys are not sorted by fingerprint: %s follows %s", binding.Fingerprint, previous)
			}
		}
		if !validServePublicKeyLine(binding.PublicKey) {
			return fmt.Errorf("fingerprint %s has a malformed public key; want key type and base64 blob only", binding.Fingerprint)
		}
		if other, exists := publicKeys[binding.PublicKey]; exists {
			return fmt.Errorf("duplicate public key: fingerprints %s and %s hold the same key", other, binding.Fingerprint)
		}
		publicKeys[binding.PublicKey] = binding.Fingerprint
		if !validActorFingerprint(binding.Actor) {
			return fmt.Errorf("fingerprint %s is bound to a malformed actor %q; want a full 64-hex actor fingerprint", binding.Fingerprint, binding.Actor)
		}
		if !validServeTimestamp(binding.Added) {
			return fmt.Errorf("fingerprint %s has a malformed added time %q; want RFC 3339 UTC", binding.Fingerprint, binding.Added)
		}
	}
	return nil
}

func decodeServeRegistry(contents []byte) (serveRegistry, error) {
	var registry serveRegistry
	if err := decodePrivateJSON(contents, &registry, "hn serve key registry"); err != nil {
		return serveRegistry{}, err
	}
	if registry.Keys == nil {
		registry.Keys = []serveKeyBinding{}
	}
	if err := validateServeRegistry(registry); err != nil {
		return serveRegistry{}, err
	}
	return registry, nil
}

// loadServeRegistry treats a missing registry as empty; any other read or
// decoding problem is fatal.
func loadServeRegistry(layout serveLayout) (serveRegistry, error) {
	contents, err := readPrivateFileBounded(layout.Registry, serveStateFileLimit)
	if errors.Is(err, os.ErrNotExist) {
		return emptyServeRegistry(), nil
	}
	if err != nil {
		return serveRegistry{}, serveStateError(layout.Registry, err)
	}
	registry, err := decodeServeRegistry(contents)
	if err != nil {
		return serveRegistry{}, serveStateError(layout.Registry, err)
	}
	return registry, nil
}

func saveServeRegistry(layout serveLayout, registry serveRegistry) error {
	if registry.Keys == nil {
		registry.Keys = []serveKeyBinding{}
	}
	if err := validateServeRegistry(registry); err != nil {
		return serveStateError(layout.Registry, err)
	}
	contents, err := encodeServeJSON(registry)
	if err != nil {
		return serveStateError(layout.Registry, err)
	}
	if err := replacePrivateFileAtomic(layout.Registry, contents); err != nil {
		return serveStateError(layout.Registry, err)
	}
	return nil
}

func lookupServeKey(registry serveRegistry, fingerprint string) (serveKeyBinding, bool) {
	index := sort.Search(len(registry.Keys), func(i int) bool {
		return registry.Keys[i].Fingerprint >= fingerprint
	})
	if index < len(registry.Keys) && registry.Keys[index].Fingerprint == fingerprint {
		return registry.Keys[index], true
	}
	return serveKeyBinding{}, false
}

// Repository names and branch refs.

// validServeRepositoryName implements [A-Za-z0-9][A-Za-z0-9._-]{0,99} with no
// ".." and no trailing ".git".
func validServeRepositoryName(name string) bool {
	if len(name) == 0 || len(name) > serveRepositoryNameMaximum || !isServeAlphanumeric(name[0]) {
		return false
	}
	for i := 1; i < len(name); i++ {
		character := name[i]
		if !isServeAlphanumeric(character) && character != '.' && character != '_' && character != '-' {
			return false
		}
	}
	return !strings.Contains(name, "..") && !strings.HasSuffix(name, ".git")
}

func isServeAlphanumeric(character byte) bool {
	return (character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9')
}

// validServeBranchRef is a conservative subset of what
// git check-ref-format --branch accepts, applied to refs/heads/NAME.
func validServeBranchRef(ref string) bool {
	name, ok := strings.CutPrefix(ref, serveBranchRefPrefix)
	if !ok || name == "" || name == "@" {
		return false
	}
	if strings.HasPrefix(name, "-") || strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") ||
		strings.HasSuffix(name, ".") || strings.Contains(name, "..") || strings.Contains(name, "//") ||
		strings.Contains(name, "@{") {
		return false
	}
	for i := 0; i < len(name); i++ {
		character := name[i]
		if character < 0x20 || character == 0x7f || strings.IndexByte(" ~^:?*[\\", character) >= 0 {
			return false
		}
	}
	for _, component := range strings.Split(name, "/") {
		if strings.HasPrefix(component, ".") || strings.HasSuffix(component, ".lock") {
			return false
		}
	}
	return true
}

// Repository configuration (state/repos/NAME/config.json).

type serveRepoConfig struct {
	Version       int                `json:"version"`
	Name          string             `json:"name"`
	Protected     []string           `json:"protected"`
	LFSQuotaBytes int64              `json:"lfsQuotaBytes"`
	Budgets       ReplicationBudgets `json:"budgets"`
}

// newServeRepoConfig returns the default configuration. The protected list
// keeps its first entry first because entry 0 is the default protected
// branch and HEAD points to it; the remaining entries are sorted and
// deduplicated. data-model.md calls the list both sorted and default-first;
// this ordering is how both statements hold when the default is not
// alphabetically first.
func newServeRepoConfig(name string, protected []string) serveRepoConfig {
	return serveRepoConfig{
		Version:       serveRepoConfigVersion,
		Name:          name,
		Protected:     normalizeServeProtectedRefs(protected),
		LFSQuotaBytes: serveDefaultLFSQuotaBytes,
		Budgets:       defaultReplicationBudgets(),
	}
}

func normalizeServeProtectedRefs(protected []string) []string {
	if len(protected) == 0 {
		return []string{serveDefaultProtectedRef}
	}
	refs := []string{protected[0]}
	seen := map[string]bool{protected[0]: true}
	var rest []string
	for _, ref := range protected[1:] {
		if !seen[ref] {
			seen[ref] = true
			rest = append(rest, ref)
		}
	}
	sort.Strings(rest)
	return append(refs, rest...)
}

func validateServeRepoConfig(config serveRepoConfig, name string) error {
	if config.Version != serveRepoConfigVersion {
		return fmt.Errorf("unsupported repository configuration version %d", config.Version)
	}
	if !validServeRepositoryName(config.Name) {
		return fmt.Errorf("configuration has a malformed repository name %q", config.Name)
	}
	if config.Name != name {
		return fmt.Errorf("configuration names repository %q, want %q", config.Name, name)
	}
	if len(config.Protected) == 0 {
		return errors.New("at least one protected branch is required")
	}
	for index, ref := range config.Protected {
		if !validServeBranchRef(ref) {
			return fmt.Errorf("protected ref %q is not a valid branch ref", ref)
		}
		if index == 0 {
			continue
		}
		if ref == config.Protected[0] {
			return fmt.Errorf("duplicate protected ref %s", ref)
		}
		if index > 1 {
			previous := config.Protected[index-1]
			if ref == previous {
				return fmt.Errorf("duplicate protected ref %s", ref)
			}
			if ref < previous {
				return fmt.Errorf("protected refs after the default must be sorted: %s follows %s", ref, previous)
			}
		}
	}
	if config.LFSQuotaBytes <= 0 {
		return errors.New("lfsQuotaBytes must be a positive integer")
	}
	if err := config.Budgets.validate(); err != nil {
		return fmt.Errorf("budgets: %w", err)
	}
	return nil
}

func decodeServeRepoConfig(contents []byte, name string) (serveRepoConfig, error) {
	var config serveRepoConfig
	if err := decodePrivateJSON(contents, &config, "hn serve repository configuration"); err != nil {
		return serveRepoConfig{}, err
	}
	if err := validateServeRepoConfig(config, name); err != nil {
		return serveRepoConfig{}, err
	}
	return config, nil
}

// encodeServeRepoConfig writes the layout shown in the server-state
// contract, with the protected list on one line.
func encodeServeRepoConfig(config serveRepoConfig) ([]byte, error) {
	name, err := json.Marshal(config.Name)
	if err != nil {
		return nil, err
	}
	protected := make([]string, len(config.Protected))
	for index, ref := range config.Protected {
		encoded, err := json.Marshal(ref)
		if err != nil {
			return nil, err
		}
		protected[index] = string(encoded)
	}
	budgets, err := json.MarshalIndent(config.Budgets, "  ", "  ")
	if err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	fmt.Fprintf(&buffer, "{\n  \"version\": %d,\n  \"name\": %s,\n  \"protected\": [%s],\n  \"lfsQuotaBytes\": %d,\n  \"budgets\": %s\n}\n",
		config.Version, name, strings.Join(protected, ", "), config.LFSQuotaBytes, budgets)
	return buffer.Bytes(), nil
}

// loadServeRepoConfig requires the configuration to exist. A missing file
// keeps os.ErrNotExist in the chain so callers can report an unhosted
// repository.
func loadServeRepoConfig(layout serveLayout, name string) (serveRepoConfig, error) {
	if !validServeRepositoryName(name) {
		return serveRepoConfig{}, fmt.Errorf("invalid repository name %q", name)
	}
	path := layout.RepoConfig(name)
	contents, err := readPrivateFileBounded(path, serveStateFileLimit)
	if err != nil {
		return serveRepoConfig{}, serveStateError(path, err)
	}
	config, err := decodeServeRepoConfig(contents, name)
	if err != nil {
		return serveRepoConfig{}, serveStateError(path, err)
	}
	return config, nil
}

func saveServeRepoConfig(layout serveLayout, config serveRepoConfig) error {
	if !validServeRepositoryName(config.Name) {
		return fmt.Errorf("invalid repository name %q", config.Name)
	}
	path := layout.RepoConfig(config.Name)
	if err := validateServeRepoConfig(config, config.Name); err != nil {
		return serveStateError(path, err)
	}
	contents, err := encodeServeRepoConfig(config)
	if err != nil {
		return serveStateError(path, err)
	}
	if err := replacePrivateFileAtomic(path, contents); err != nil {
		return serveStateError(path, err)
	}
	return nil
}

// Branch owners (state/repos/NAME/branches.json).

type serveBranchOwners struct {
	Version  int                `json:"version"`
	Branches []serveBranchOwner `json:"branches"`
}

type serveBranchOwner struct {
	Ref     string `json:"ref"`
	Owner   string `json:"owner"`
	Created string `json:"created"`
}

func validateServeBranchOwners(owners serveBranchOwners) error {
	if owners.Version != serveBranchOwnersVersion {
		return fmt.Errorf("unsupported branch owners version %d", owners.Version)
	}
	for index, branch := range owners.Branches {
		if !validServeBranchRef(branch.Ref) {
			return fmt.Errorf("branch owner ref %q is not a valid branch ref", branch.Ref)
		}
		if index > 0 {
			previous := owners.Branches[index-1].Ref
			if branch.Ref == previous {
				return fmt.Errorf("duplicate branch owner ref %s", branch.Ref)
			}
			if branch.Ref < previous {
				return fmt.Errorf("branch owners are not sorted by ref: %s follows %s", branch.Ref, previous)
			}
		}
		if !validActorFingerprint(branch.Owner) {
			return fmt.Errorf("branch %s has a malformed owner %q; want a full 64-hex actor fingerprint", branch.Ref, branch.Owner)
		}
		if !validServeTimestamp(branch.Created) {
			return fmt.Errorf("branch %s has a malformed created time %q; want RFC 3339 UTC", branch.Ref, branch.Created)
		}
	}
	return nil
}

func decodeServeBranchOwners(contents []byte) (serveBranchOwners, error) {
	var owners serveBranchOwners
	if err := decodePrivateJSON(contents, &owners, "hn serve branch owners"); err != nil {
		return serveBranchOwners{}, err
	}
	if owners.Branches == nil {
		owners.Branches = []serveBranchOwner{}
	}
	if err := validateServeBranchOwners(owners); err != nil {
		return serveBranchOwners{}, err
	}
	return owners, nil
}

// loadServeBranchOwners treats a missing file as no recorded owners.
func loadServeBranchOwners(layout serveLayout, name string) (serveBranchOwners, error) {
	if !validServeRepositoryName(name) {
		return serveBranchOwners{}, fmt.Errorf("invalid repository name %q", name)
	}
	path := layout.RepoBranches(name)
	contents, err := readPrivateFileBounded(path, serveStateFileLimit)
	if errors.Is(err, os.ErrNotExist) {
		return serveBranchOwners{Version: serveBranchOwnersVersion, Branches: []serveBranchOwner{}}, nil
	}
	if err != nil {
		return serveBranchOwners{}, serveStateError(path, err)
	}
	owners, err := decodeServeBranchOwners(contents)
	if err != nil {
		return serveBranchOwners{}, serveStateError(path, err)
	}
	return owners, nil
}

// saveServeBranchOwners always writes the file, including an empty list, so
// the record never disappears between a read and a write.
func saveServeBranchOwners(layout serveLayout, name string, owners serveBranchOwners) error {
	if !validServeRepositoryName(name) {
		return fmt.Errorf("invalid repository name %q", name)
	}
	path := layout.RepoBranches(name)
	if owners.Branches == nil {
		owners.Branches = []serveBranchOwner{}
	}
	if err := validateServeBranchOwners(owners); err != nil {
		return serveStateError(path, err)
	}
	contents, err := encodeServeJSON(owners)
	if err != nil {
		return serveStateError(path, err)
	}
	if err := replacePrivateFileAtomic(path, contents); err != nil {
		return serveStateError(path, err)
	}
	return nil
}

// Push log (state/repos/NAME/push.log) and repository locks.

type servePushLogUpdate struct {
	Ref string `json:"ref"`
	Old string `json:"old"`
	New string `json:"new"`
}

type servePushLogEntry struct {
	Time    string               `json:"time"`
	Actor   string               `json:"actor"`
	Key     string               `json:"key"`
	Verdict string               `json:"verdict"`
	Updates []servePushLogUpdate `json:"updates"`
	Codes   []string             `json:"codes"`
}

// appendServePushLog appends one JSON line with a single write. The caller
// holds the repository lock. The server never reads the log back.
func appendServePushLog(layout serveLayout, name string, entry servePushLogEntry) error {
	if !validServeRepositoryName(name) {
		return fmt.Errorf("invalid repository name %q", name)
	}
	path := layout.RepoPushLog(name)
	if entry.Verdict != serveVerdictAccepted && entry.Verdict != serveVerdictRejected {
		return serveStateError(path, fmt.Errorf("push log verdict %q must be accepted or rejected", entry.Verdict))
	}
	if entry.Updates == nil {
		entry.Updates = []servePushLogUpdate{}
	}
	if entry.Codes == nil {
		entry.Codes = []string{}
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return serveStateError(path, err)
	}
	line = append(line, '\n')
	file, err := openServeStateFile(path, os.O_WRONLY|os.O_APPEND)
	if err != nil {
		return err
	}
	written, err := file.Write(line)
	if err == nil && written != len(line) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return serveStateError(path, err)
	}
	return nil
}

// openServeStateFile opens or creates a 0600 regular state file without
// following a symlink at the final path component: an existing file is
// checked with Lstat before the open and compared with Fstat after it, and a
// new file is created with O_EXCL.
func openServeStateFile(path string, flag int) (*os.File, error) {
	for attempt := 0; attempt < 3; attempt++ {
		before, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			file, err := os.OpenFile(path, flag|os.O_CREATE|os.O_EXCL, 0o600)
			if errors.Is(err, os.ErrExist) {
				continue
			}
			if err != nil {
				return nil, serveStateError(path, err)
			}
			if err := file.Chmod(0o600); err != nil {
				_ = file.Close()
				return nil, serveStateError(path, err)
			}
			return file, nil
		}
		if err != nil {
			return nil, serveStateError(path, err)
		}
		if err := checkServeRegularInfo(path, before, 0o600, servePushLogLimit); err != nil {
			return nil, err
		}
		file, err := os.OpenFile(path, flag, 0)
		if err != nil {
			return nil, serveStateError(path, err)
		}
		after, err := file.Stat()
		if err != nil {
			_ = file.Close()
			return nil, serveStateError(path, err)
		}
		if !os.SameFile(before, after) {
			_ = file.Close()
			return nil, serveDefect(path, "changed while it was being opened")
		}
		return file, nil
	}
	return nil, serveDefect(path, "changed while it was being opened")
}

// acquireServeLock takes an exclusive flock on path, waiting up to wait.
// It returns errServeLockBusy (wrapped with the path) on timeout. Closing
// the descriptor, which release does once, releases the lock. Go opens files
// close-on-exec, so child processes do not inherit the lock.
func acquireServeLock(path string, wait time.Duration) (func() error, error) {
	file, err := openServeStateFile(path, os.O_RDWR)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(wait)
	for {
		locked, err := tryServeFileLock(file)
		if err != nil {
			_ = file.Close()
			return nil, serveStateError(path, err)
		}
		if locked {
			break
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			_ = file.Close()
			return nil, fmt.Errorf("%w: %s", errServeLockBusy, path)
		}
		time.Sleep(min(serveLockPollInterval, remaining))
	}
	var once sync.Once
	var closeErr error
	release := func() error {
		once.Do(func() { closeErr = file.Close() })
		return closeErr
	}
	return release, nil
}

// serveLockWait is how long a push waits for the repository lock. Tests may
// shorten it with HN_TEST_SERVE_LOCK_WAIT when HN_INTERNAL_TESTING=1; a
// malformed or negative value keeps the default.
func serveLockWait() time.Duration {
	if os.Getenv("HN_INTERNAL_TESTING") == "1" {
		if value, ok := os.LookupEnv("HN_TEST_SERVE_LOCK_WAIT"); ok {
			if wait, err := time.ParseDuration(value); err == nil && wait >= 0 {
				return wait
			}
		}
	}
	return serveDefaultLockWait
}

// Hook scripts ($HOME/.hn-serve/hooks).

// serveExecutablePath returns the absolute, symlink-resolved path of the
// running hn binary. WP02 embeds the same path in authorized_keys, so a path
// that would need quoting is refused.
func serveExecutablePath() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("hn serve cannot locate its own executable: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("hn serve cannot resolve its own executable %s: %w", path, err)
	}
	return checkServeExecutablePath(resolved)
}

func checkServeExecutablePath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("hn serve executable path %q is not absolute", path)
	}
	unsafe := strings.IndexFunc(path, func(character rune) bool {
		return character == '"' || character == '\'' || character == '\\' || unicode.IsSpace(character) || unicode.IsControl(character)
	})
	if unsafe >= 0 {
		return "", fmt.Errorf("hn serve executable path %q contains a quote, backslash, whitespace or control character; install hn at a path without them", path)
	}
	return path, nil
}

func serveHookScript(name, hnPath string) []byte {
	return []byte("#!/bin/sh\nexec " + hnPath + " serve hook " + name + "\n")
}

// ensureServeHooks atomically rewrites both hook scripts with mode 0700.
func ensureServeHooks(layout serveLayout, hnPath string) error {
	if _, err := checkServeExecutablePath(hnPath); err != nil {
		return err
	}
	for _, directory := range []string{layout.Root, layout.Hooks} {
		if err := requireServeDirectory(directory); err != nil {
			return err
		}
	}
	for _, name := range serveHookNames {
		path := filepath.Join(layout.Hooks, name)
		if err := writeServeExecutableAtomic(path, serveHookScript(name, hnPath)); err != nil {
			return serveStateError(path, err)
		}
	}
	return nil
}

// writeServeExecutableAtomic is the 0700 counterpart of
// writePrivateFileAtomic: temporary file in the same directory, fsync,
// rename, directory fsync.
func writeServeExecutableAtomic(path string, contents []byte) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".hn-hook-*")
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
	if err := temporary.Chmod(0o700); err != nil {
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
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return syncPrivateDirectory(directory)
}
