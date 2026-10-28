package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Administration commands for hn serve: hn serve repo create and set, and
// hn serve key add and remove. The command surface is specified in
// kitty-specs/hn-serve-01M5P0NP/contracts/serve-cli.md. Repositories exist
// only by these commands; no push path creates one (FR-002).

const (
	serveAdminLockName        = "admin.lock"
	serveCreateStagingPrefix  = "create-"
	serveKeyInputLimit        = servePublicKeyLineLimit
	serveAuthorizedKeysLagFix = "rerun any hn serve key or repo command"
)

// serveAdministration is the context of one hn serve repo or key command.
// It holds the administration lock, state/tmp/admin.lock, which serializes
// administrators so registry and repository changes never interleave.
type serveAdministration struct {
	layout  serveLayout
	hnPath  string
	release func() error
}

// finish releases the administration lock. It is safe to call more than
// once; lock-exec calls it before it waits for the repository lock.
func (admin *serveAdministration) finish() error {
	if err := admin.release(); err != nil {
		return serveStateError(filepath.Join(admin.layout.Tmp, serveAdminLockName), err)
	}
	return nil
}

// runServeAdministration is the preamble every hn serve repo and key
// subcommand shares: create the fixed directories, take the administration
// lock, remove staging left by an interrupted repo create and validate the
// layout, which regenerates the hook scripts. Only then does the subcommand
// run, still holding the administration lock. Every subcommand also
// rewrites the managed authorized_keys block from the registry, which is
// idempotent and repairs the block after hn moves, like the hooks: see
// rewriteAuthorizedKeys and refreshAuthorizedKeys.
func runServeAdministration(layout serveLayout, administer func(*serveAdministration, []string) error, args []string) error {
	if err := ensureServeLayout(layout); err != nil {
		return err
	}
	lockPath := filepath.Join(layout.Tmp, serveAdminLockName)
	wait := serveLockWait()
	release, err := acquireServeLock(lockPath, wait)
	if errors.Is(err, errServeLockBusy) {
		return fmt.Errorf("hn serve administration is busy: another hn serve repo or key command held %s for %v; try again when it finishes", lockPath, wait)
	}
	if err != nil {
		return err
	}
	admin := &serveAdministration{layout: layout, release: release}
	err = admin.prepare()
	if err == nil {
		err = administer(admin, args)
	}
	if finishErr := admin.finish(); finishErr != nil && err == nil {
		err = finishErr
	}
	return err
}

func (admin *serveAdministration) prepare() error {
	if err := removeAbandonedServeStaging(admin.layout); err != nil {
		return err
	}
	if err := validateServeLayout(admin.layout, serveAdministrationValidation); err != nil {
		return err
	}
	hnPath, err := serveExecutablePath()
	if err != nil {
		return err
	}
	admin.hnPath = hnPath
	return nil
}

// rewriteAuthorizedKeys rewrites the managed block from the current
// registry. Commands that change state call it before their change, so an
// unsafe .ssh, broken block markers or a registry the block cannot hold stop
// them before anything is written.
func (admin *serveAdministration) rewriteAuthorizedKeys() error {
	registry, err := loadServeRegistry(admin.layout)
	if err != nil {
		return err
	}
	return writeServeAuthorizedKeys(admin.layout, registry.Keys, admin.hnPath)
}

// refreshAuthorizedKeys is the form read-only commands and lock-exec use.
// Unsafe SSH state and every write failure refuse the command, like
// rewriteAuthorizedKeys, and lock-exec calls it before it starts anything.
// The one tolerated failure is a registry binding whose stored public key
// does not parse or does not match its fingerprint. That binding can only
// be repaired with hn serve key remove, which an administrator finds with
// hn serve key list, so inspection still runs and the block is left as it
// is. Leaving it fails closed: the forced command names only a
// fingerprint, and hn serve git authorizes from the registry.
func (admin *serveAdministration) refreshAuthorizedKeys() error {
	if err := checkServeAuthorizedKeysState(admin.layout); err != nil {
		return err
	}
	registry, err := loadServeRegistry(admin.layout)
	if err != nil {
		return err
	}
	if _, err := renderServeAuthorizedKeysBlock(registry.Keys, admin.hnPath); err != nil {
		fmt.Fprintf(os.Stderr, "hn serve: warning: authorized_keys was left unchanged because %s holds a binding it cannot write: %v; hn serve key add, repo create and repo set refuse until 'hn serve key remove FINGERPRINT' removes that binding\n", admin.layout.Registry, err)
		return nil
	}
	return writeServeAuthorizedKeys(admin.layout, registry.Keys, admin.hnPath)
}

// checkServeAuthorizedKeysState applies the checks writeServeAuthorizedKeys
// makes before it writes, without needing a block: .ssh and authorized_keys
// must be safe and the managed block markers well formed. Every failure
// names the offending path.
func checkServeAuthorizedKeysState(layout serveLayout) error {
	exists, err := checkServeSSHDirectory(layout.SSHDir)
	if err != nil || !exists {
		return err
	}
	existing, err := readServeAuthorizedKeys(layout.AuthorizedKeys)
	if err != nil {
		return err
	}
	if _, err := mergeServeAuthorizedKeys(existing, nil); err != nil {
		return serveDefect(layout.AuthorizedKeys, "%v; fix the managed block markers by hand", err)
	}
	return nil
}

// removeAbandonedServeStaging deletes create-* directories in state/tmp.
// hn serve repo create holds the administration lock for as long as its
// staging directory exists, so any such directory seen under that lock was
// left by an interrupted create. Its Git files do not follow the 0600/0700
// rule for state/tmp, so leaving it would make every command refuse.
func removeAbandonedServeStaging(layout serveLayout) error {
	entries, err := os.ReadDir(layout.Tmp)
	if err != nil {
		return serveStateError(layout.Tmp, err)
	}
	var abandoned []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), serveCreateStagingPrefix) && entry.Type().IsDir() {
			abandoned = append(abandoned, filepath.Join(layout.Tmp, entry.Name()))
		}
	}
	if len(abandoned) == 0 {
		return nil
	}
	release, err := lockServeLayout(layout, serveLockExclusive)
	if err != nil {
		return err
	}
	for _, path := range abandoned {
		if removeErr := os.RemoveAll(path); removeErr != nil && err == nil {
			err = serveStateError(path, removeErr)
		}
	}
	if releaseErr := release(); releaseErr != nil && err == nil {
		err = serveStateError(layout.Root, releaseErr)
	}
	return err
}

// Keys.

// cmdServeKeyAdd binds one SSH public key to one actor (FR-003). The block
// is rendered before any write so a key the block cannot hold changes
// nothing; the registry is saved before authorized_keys so a failure in
// between leaves the new key unable to connect.
func cmdServeKeyAdd(admin *serveAdministration, args []string) error {
	const usage = "usage: hn serve key add --actor ACTOR KEYFILE|-"
	flags := quietFlags("serve key add")
	actor := flags.String("actor", "", "full 64-hex actor fingerprint")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 || *actor == "" {
		return usageError(usage)
	}
	if !validActorFingerprint(*actor) {
		return fmt.Errorf("--actor %q is not a full 64-hex actor fingerprint; copy the Actor line of hn identity show", *actor)
	}
	line, err := readServeKeyInput(flags.Arg(0))
	if err != nil {
		return err
	}
	parsed, err := parseServePublicKey(line)
	if err != nil {
		return err
	}
	registry, err := loadServeRegistry(admin.layout)
	if err != nil {
		return err
	}
	for _, binding := range registry.Keys {
		if binding.Fingerprint == parsed.Fingerprint || binding.PublicKey == parsed.Canonical {
			return fmt.Errorf("key %s is already bound to actor %s; run 'hn serve key remove %s' first to rebind it", binding.Fingerprint, binding.Actor, binding.Fingerprint)
		}
	}
	next := serveRegistry{Version: serveRegistryVersion, Keys: append(slices.Clone(registry.Keys), serveKeyBinding{
		Fingerprint: parsed.Fingerprint,
		PublicKey:   parsed.Canonical,
		Actor:       *actor,
		Added:       time.Now().UTC().Format(time.RFC3339),
	})}
	if err := admin.changeKeys(next); err != nil {
		return err
	}
	fmt.Printf("Bound %s to actor %s\n", parsed.Fingerprint, *actor)
	return nil
}

// cmdServeKeyRemove removes one binding by its full fingerprint. The key's
// next connection finds no binding and is refused.
func cmdServeKeyRemove(admin *serveAdministration, args []string) error {
	if len(args) != 1 {
		return usageError("usage: hn serve key remove FINGERPRINT")
	}
	fingerprint := args[0]
	if !validServeFingerprint(fingerprint) {
		return fmt.Errorf("%q is not a full SHA256 key fingerprint; give SHA256: and all 43 characters as hn serve key list prints them", fingerprint)
	}
	registry, err := loadServeRegistry(admin.layout)
	if err != nil {
		return err
	}
	index := slices.IndexFunc(registry.Keys, func(binding serveKeyBinding) bool { return binding.Fingerprint == fingerprint })
	if index < 0 {
		return fmt.Errorf("no key with fingerprint %s is bound; run 'hn serve key list'", fingerprint)
	}
	removed := registry.Keys[index]
	next := serveRegistry{Version: serveRegistryVersion, Keys: slices.Delete(slices.Clone(registry.Keys), index, index+1)}
	if err := admin.changeKeys(next); err != nil {
		return err
	}
	fmt.Printf("Removed %s from actor %s\n", removed.Fingerprint, removed.Actor)
	return nil
}

// changeKeys first checks that .ssh and authorized_keys are safe and the
// block markers well formed, and renders the block for next. Only then does
// it save the registry and rewrite authorized_keys. The block for the
// current registry is not needed, so key remove can drop a binding whose
// stored key cannot be written. A failure after the registry write is
// reported with the repair step and fails closed: an added key cannot
// connect yet and a removed key finds no binding when it connects.
func (admin *serveAdministration) changeKeys(next serveRegistry) error {
	if err := checkServeAuthorizedKeysState(admin.layout); err != nil {
		return err
	}
	if _, err := renderServeAuthorizedKeysBlock(next.Keys, admin.hnPath); err != nil {
		return err
	}
	if err := saveServeRegistry(admin.layout, next); err != nil {
		return err
	}
	if err := writeServeAuthorizedKeys(admin.layout, next.Keys, admin.hnPath); err != nil {
		return fmt.Errorf("registry updated but authorized_keys was not rewritten: %v; %s", err, serveAuthorizedKeysLagFix)
	}
	return nil
}

// readServeKeyInput reads KEYFILE, or standard input for "-", and returns
// its one non-empty line. The read is bounded by the public key line limit.
func readServeKeyInput(source string) (string, error) {
	name := source
	var reader io.Reader = os.Stdin
	if source == "-" {
		name = "standard input"
	} else {
		file, err := os.Open(source)
		if err != nil {
			return "", fmt.Errorf("read SSH public key: %w", err)
		}
		defer file.Close()
		reader = file
	}
	contents, err := io.ReadAll(io.LimitReader(reader, serveKeyInputLimit+1))
	if err != nil {
		return "", fmt.Errorf("read SSH public key from %s: %w", name, err)
	}
	if len(contents) > serveKeyInputLimit {
		return "", fmt.Errorf("%s is larger than %d bytes; give one SSH public key line", name, serveKeyInputLimit)
	}
	var lines []string
	for _, line := range strings.Split(string(contents), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	switch len(lines) {
	case 0:
		return "", fmt.Errorf("%s holds no SSH public key line", name)
	case 1:
		return lines[0], nil
	default:
		return "", fmt.Errorf("%s holds %d non-empty lines; give exactly one SSH public key line", name, len(lines))
	}
}

// Repositories.

// cmdServeRepoCreate seeds a hosted repository from SOURCE (FR-002, OQ-7,
// OQ-11). Everything is staged in state/tmp/create-* and moved into place
// only after every check passed, so a refusal leaves nothing in
// repositories/ or state/repos/.
func cmdServeRepoCreate(admin *serveAdministration, args []string) error {
	const usage = "usage: hn serve repo create NAME --from SOURCE [--protect REF]... [--lfs-quota BYTES]"
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return usageError(usage)
	}
	name := args[0]
	flags := quietFlags("serve repo create")
	source := flags.String("from", "", "path or URL that git fetch accepts")
	var protect repeatedStringFlag
	flags.Var(&protect, "protect", "protected branch, short or full ref")
	quota := flags.Int64("lfs-quota", serveDefaultLFSQuotaBytes, "LFS quota in bytes")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || *source == "" {
		return usageError(usage)
	}
	if !validServeRepositoryName(name) {
		return fmt.Errorf("invalid hosted repository name %q; use a letter or digit followed by up to 99 letters, digits, '.', '_' or '-', without '..' or a trailing .git", name)
	}
	if *quota <= 0 {
		return errors.New("--lfs-quota must be a positive number of bytes")
	}
	protected, err := normalizeServeProtectedRefs(protect)
	if err != nil {
		return err
	}
	config, err := newServeRepoConfig(name, protected)
	if err != nil {
		return err
	}
	config.LFSQuotaBytes = *quota
	layout := admin.layout
	for _, path := range []string{layout.RepoGitDir(name), layout.RepoStateDir(name)} {
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("hosted repository %s already exists; run 'hn serve repo show %s'", name, name)
		} else if !errors.Is(err, os.ErrNotExist) {
			return serveStateError(path, err)
		}
	}
	from, err := resolveServeSource(*source)
	if err != nil {
		return err
	}
	cloneURL, err := serveCloneURL(name)
	if err != nil {
		return err
	}
	if err := admin.rewriteAuthorizedKeys(); err != nil {
		return err
	}
	if err := createServeRepository(layout, config, from); err != nil {
		return err
	}
	fmt.Printf("Created hosted repository %s\n", name)
	for _, ref := range config.Protected {
		fmt.Printf("Protected: %s\n", ref)
	}
	fmt.Printf("LFS quota: %d bytes\n", config.LFSQuotaBytes)
	fmt.Printf("Clone URL: %s\n", cloneURL)
	return nil
}

// resolveServeSource refuses a SOURCE Git would read as an option and makes
// an existing local path absolute, because Git runs in the staging
// directory rather than the administrator's working directory. Anything
// else, such as an ssh:// URL, goes to Git unchanged.
func resolveServeSource(source string) (string, error) {
	if strings.HasPrefix(source, "-") {
		return "", fmt.Errorf("--from %q looks like an option; give a repository path or URL", source)
	}
	if strings.ContainsAny(source, "\x00\n\r") {
		return "", fmt.Errorf("--from %q contains a control character", source)
	}
	if _, err := os.Stat(source); err != nil {
		return source, nil
	}
	absolute, err := filepath.Abs(source)
	if err != nil {
		return "", fmt.Errorf("resolve --from %s: %w", source, err)
	}
	return absolute, nil
}

// serveCloneURL is the URL participants clone from: the server account's
// login and this host's name. It is computed before anything is written.
func serveCloneURL(name string) (string, error) {
	account, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("hn serve cannot determine the server account's login name: %w", err)
	}
	host, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("hn serve cannot determine this host's name: %w", err)
	}
	return "ssh://" + account.Username + "@" + host + "/" + name + ".git", nil
}

// createServeRepository builds the repository and its state in a staging
// directory, then renames both into place. It holds the layout lock
// exclusively throughout: the staged Git files do not follow the state/tmp
// mode rules, and the two renames briefly leave a repository without its
// pair, so no validator may run until both are done. A crash between the
// renames leaves that pairing defect, which validation reports, so the
// server refuses to serve half a repository.
func createServeRepository(layout serveLayout, config serveRepoConfig, source string) (err error) {
	release, err := lockServeLayout(layout, serveLockExclusive)
	if err != nil {
		return err
	}
	defer func() {
		if releaseErr := release(); releaseErr != nil && err == nil {
			err = serveStateError(layout.Root, releaseErr)
		}
	}()
	staging, err := os.MkdirTemp(layout.Tmp, serveCreateStagingPrefix+"*")
	if err != nil {
		return serveStateError(layout.Tmp, err)
	}
	defer func() {
		if removeErr := os.RemoveAll(staging); removeErr != nil && err == nil {
			err = serveStateError(staging, removeErr)
		}
	}()
	name := config.Name
	gitDir := filepath.Join(staging, name+".git")
	if err := stageServeRepository(staging, gitDir, config, source); err != nil {
		return fmt.Errorf("cannot create hosted repository %s: %w", name, err)
	}
	stateDir := filepath.Join(staging, "state")
	if err := ensurePrivateDirectory(stateDir); err != nil {
		return serveStateError(stateDir, err)
	}
	contents, err := encodeServeRepoConfig(config)
	if err != nil {
		return err
	}
	if err := writePrivateFileAtomic(filepath.Join(stateDir, "config.json"), contents); err != nil {
		return serveStateError(filepath.Join(stateDir, "config.json"), err)
	}
	return commitServeRepository(layout, name, stateDir, gitDir)
}

// stageServeRepository creates the bare repository at gitDir from source
// and checks it. Only refs/heads/* and refs/tags/* are imported: Hubnot
// facts must arrive through governed pushes (FR-017), so refs/hn/*,
// refs/notes/* and every other namespace stay behind. fetch.fsckObjects
// treats the source as hostile input.
func stageServeRepository(staging, gitDir string, config serveRepoConfig, source string) error {
	format, err := detectServeSourceFormat(staging, source)
	if err != nil {
		return err
	}
	// An empty --template keeps hook samples and any administrator template
	// out of the repository. No hook or core.hooksPath is ever written into
	// it; sessions pass the hooks with -c (IC-02).
	if _, err := serveAdminGit(staging, nil, "init", "--quiet", "--bare", "--template=", "--object-format="+format, gitDir); err != nil {
		return err
	}
	if _, err := serveAdminGit(staging, nil, "--git-dir="+gitDir, "-c", "fetch.fsckObjects=true",
		"fetch", "--quiet", "--no-tags", source, "+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*"); err != nil {
		return fmt.Errorf("import branches and tags from %s: %w", source, err)
	}
	for _, ref := range config.Protected {
		if err := checkServeProtectedTip(gitDir, ref); err != nil {
			return err
		}
	}
	marker, err := serveAdminGit(staging, []byte(serveMarkerContent), "--git-dir="+gitDir, "hash-object", "-w", "--stdin")
	if err != nil {
		return err
	}
	markerID := strings.TrimSpace(string(marker))
	if !validGitOID(markerID) {
		return fmt.Errorf("git hash-object returned an invalid object ID %q", markerID)
	}
	if _, err := serveAdminGit(staging, nil, "--git-dir="+gitDir, "update-ref", serveMarkerRef, markerID); err != nil {
		return err
	}
	if _, err := serveAdminGit(staging, nil, "--git-dir="+gitDir, "symbolic-ref", "HEAD", config.Protected[0]); err != nil {
		return err
	}
	// Git creates the directory with the process umask. Validation does not
	// look inside a hosted repository but requires its top directory 0700.
	if err := os.Chmod(gitDir, 0o700); err != nil {
		return serveStateError(gitDir, err)
	}
	return nil
}

// detectServeSourceFormat reads the source's object format from the length
// of the object IDs it advertises: 40 hex digits for SHA-1, 64 for SHA-256.
func detectServeSourceFormat(staging, source string) (string, error) {
	output, err := serveAdminGit(staging, nil, "ls-remote", source)
	if err != nil {
		return "", fmt.Errorf("list refs of %s: %w", source, err)
	}
	format := ""
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		oid, _, _ := strings.Cut(line, "\t")
		var lineFormat string
		switch {
		case len(oid) == 40 && validGitOID(oid):
			lineFormat = "sha1"
		case len(oid) == 64 && validGitOID(oid):
			lineFormat = "sha256"
		default:
			return "", fmt.Errorf("%s advertised %q, which is not a SHA-1 or SHA-256 object ID", source, oid)
		}
		if format != "" && lineFormat != format {
			return "", fmt.Errorf("%s advertised both SHA-1 and SHA-256 object IDs", source)
		}
		format = lineFormat
	}
	if format == "" {
		return "", fmt.Errorf("%s has no refs to import", source)
	}
	return format, nil
}

// commitServeRepository renames the staged state directory and then the
// staged repository into the layout. If the second rename fails the first
// is undone, so nothing is left behind.
func commitServeRepository(layout serveLayout, name, stateDir, gitDir string) error {
	finalState, finalGitDir := layout.RepoStateDir(name), layout.RepoGitDir(name)
	// rename(2) replaces an empty directory, so the absence checked before
	// staging is checked again. The administration and layout locks keep
	// any other command from creating either path in between.
	for _, path := range []string{finalState, finalGitDir} {
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("hosted repository %s already exists; run 'hn serve repo show %s'", name, name)
		} else if !errors.Is(err, os.ErrNotExist) {
			return serveStateError(path, err)
		}
	}
	if err := os.Rename(stateDir, finalState); err != nil {
		return serveStateError(finalState, err)
	}
	if err := os.Rename(gitDir, finalGitDir); err != nil {
		if undoErr := os.RemoveAll(finalState); undoErr != nil {
			return fmt.Errorf("hn serve state %s: %v; removing %s also failed: %v", finalGitDir, err, finalState, undoErr)
		}
		return serveStateError(finalGitDir, err)
	}
	for _, directory := range []string{layout.Repos, layout.Repositories} {
		if err := syncPrivateDirectory(directory); err != nil {
			return serveStateError(directory, err)
		}
	}
	return nil
}

// cmdServeRepoSet changes protected branches, the LFS quota and push
// budgets. Every check runs before the first write, so a refused change
// writes nothing. It holds the repository lock so no push moves a branch
// between its policy check and the saved configuration.
func cmdServeRepoSet(admin *serveAdministration, args []string) error {
	const usage = "usage: hn serve repo set NAME [--protect REF]... [--unprotect REF]... [--lfs-quota BYTES] [--max-events N] [--max-objects N] [--max-object-bytes N] [--max-attachment-bytes N] [--max-total-bytes N]"
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return usageError(usage)
	}
	layout := admin.layout
	name := args[0]
	if err := requireServeHostedRepository(layout, name); err != nil {
		return err
	}
	config, err := loadServeRepoConfig(layout, name)
	if err != nil {
		return err
	}
	flags := quietFlags("serve repo set")
	var protect, unprotect repeatedStringFlag
	flags.Var(&protect, "protect", "protect a branch, short or full ref")
	flags.Var(&unprotect, "unprotect", "stop protecting a branch, short or full ref")
	flags.Int64Var(&config.LFSQuotaBytes, "lfs-quota", config.LFSQuotaBytes, "LFS quota in bytes")
	flags.Int64Var(&config.Budgets.MaxEvents, "max-events", config.Budgets.MaxEvents, "maximum actor events")
	flags.Int64Var(&config.Budgets.MaxObjects, "max-objects", config.Budgets.MaxObjects, "maximum reachable objects")
	flags.Int64Var(&config.Budgets.MaxObjectBytes, "max-object-bytes", config.Budgets.MaxObjectBytes, "maximum individual object bytes")
	flags.Int64Var(&config.Budgets.MaxAttachmentBytes, "max-attachment-bytes", config.Budgets.MaxAttachmentBytes, "maximum event attachment bytes")
	flags.Int64Var(&config.Budgets.MaxTotalBytes, "max-total-bytes", config.Budgets.MaxTotalBytes, "maximum total reachable bytes")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return usageError(usage)
	}
	if config.LFSQuotaBytes <= 0 {
		return errors.New("--lfs-quota must be a positive number of bytes")
	}
	if err := config.Budgets.validate(); err != nil {
		return err
	}
	added, err := changeServeProtected(&config, protect, unprotect)
	if err != nil {
		return err
	}
	if err := validateServeRepoConfig(config, name); err != nil {
		return err
	}
	if err := admin.rewriteAuthorizedKeys(); err != nil {
		return err
	}
	release, err := acquireServeRepoLock(layout, name)
	if err != nil {
		return err
	}
	err = applyServeRepoSet(layout, config, added)
	if releaseErr := release(); releaseErr != nil && err == nil {
		err = serveStateError(layout.RepoLock(name), releaseErr)
	}
	if err != nil {
		return err
	}
	return printServeRepo(layout, config)
}

// changeServeProtected applies --protect and --unprotect to config and
// returns the newly protected refs, which still need the protected-tip
// check. When the default is unprotected, the next remaining entry becomes
// the default.
func changeServeProtected(config *serveRepoConfig, protect, unprotect []string) ([]string, error) {
	protectRefs, err := normalizeServeProtectedRefs(protect)
	if err != nil {
		return nil, err
	}
	unprotectRefs, err := normalizeServeProtectedRefs(unprotect)
	if err != nil {
		return nil, err
	}
	for _, ref := range unprotectRefs {
		if slices.Contains(protectRefs, ref) {
			return nil, fmt.Errorf("%s is named in both --protect and --unprotect", ref)
		}
		if !slices.Contains(config.Protected, ref) {
			return nil, fmt.Errorf("%s is not protected in hosted repository %s", ref, config.Name)
		}
	}
	var remaining, added []string
	for _, ref := range config.Protected {
		if !slices.Contains(unprotectRefs, ref) {
			remaining = append(remaining, ref)
		}
	}
	for _, ref := range protectRefs {
		if !slices.Contains(config.Protected, ref) {
			added = append(added, ref)
		}
	}
	all := append(remaining, added...)
	if len(all) == 0 {
		return nil, fmt.Errorf("cannot unprotect %s: hosted repository %s needs at least one protected branch; protect another branch first", strings.Join(unprotectRefs, ", "), config.Name)
	}
	config.Protected = orderServeProtected(all[0], all[1:])
	return added, nil
}

// applyServeRepoSet checks each newly protected tip, saves the configuration
// and points HEAD at the default protected branch. HEAD is set on every run,
// so rerunning hn serve repo set NAME repairs a HEAD left behind by a failure
// after the save.
func applyServeRepoSet(layout serveLayout, config serveRepoConfig, added []string) error {
	gitDir := layout.RepoGitDir(config.Name)
	for _, ref := range added {
		if err := checkServeProtectedTip(gitDir, ref); err != nil {
			return fmt.Errorf("cannot protect %s in hosted repository %s: %w", ref, config.Name, err)
		}
	}
	if err := saveServeRepoConfig(layout, config); err != nil {
		return err
	}
	if _, err := serveAdminGit(gitDir, nil, "--git-dir="+gitDir, "symbolic-ref", "HEAD", config.Protected[0]); err != nil {
		return fmt.Errorf("configuration of %s saved but HEAD was not pointed at %s: %v; rerun 'hn serve repo set %s'", config.Name, config.Protected[0], err, config.Name)
	}
	return nil
}

// normalizeServeProtectedRefs turns short branch names such as main into
// full refs and refuses anything that is not a valid branch ref. Duplicates
// are dropped; the first occurrence keeps its position.
func normalizeServeProtectedRefs(values []string) ([]string, error) {
	refs := make([]string, 0, len(values))
	for _, value := range values {
		ref := value
		if !strings.HasPrefix(ref, "refs/") {
			ref = serveBranchRefPrefix + ref
		}
		if !validServeBranchRef(ref) {
			return nil, fmt.Errorf("%q is not a branch; give a branch name such as main or a full ref such as refs/heads/main", value)
		}
		if !slices.Contains(refs, ref) {
			refs = append(refs, ref)
		}
	}
	return refs, nil
}

// checkServeProtectedTip enforces the protected-tip rule: ref exists and
// its tip commit holds a .hn/policy.json that parsePolicyBytes accepts.
func checkServeProtectedTip(gitDir, ref string) error {
	output, err := serveAdminGit(gitDir, nil, "--git-dir="+gitDir, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return fmt.Errorf("protected branch %s does not exist", ref)
	}
	tip := strings.TrimSpace(string(output))
	object := tip + ":.hn/policy.json"
	kind, err := serveAdminGit(gitDir, nil, "--git-dir="+gitDir, "cat-file", "-t", object)
	if err != nil {
		return fmt.Errorf("protected branch %s has no .hn/policy.json at its tip %s", ref, tip)
	}
	if kind := strings.TrimSpace(string(kind)); kind != "blob" {
		return fmt.Errorf("protected branch %s has a .hn/policy.json %s, not a file, at its tip %s", ref, kind, tip)
	}
	sizeText, err := serveAdminGit(gitDir, nil, "--git-dir="+gitDir, "cat-file", "-s", object)
	if err != nil {
		return err
	}
	size, err := strconv.ParseInt(strings.TrimSpace(string(sizeText)), 10, 64)
	if err != nil {
		return fmt.Errorf("git cat-file -s %s returned %q", object, sizeText)
	}
	if size > maxPolicySize {
		return fmt.Errorf("protected branch %s has an invalid .hn/policy.json at its tip %s: policy exceeds %d bytes", ref, tip, maxPolicySize)
	}
	contents, err := serveAdminGit(gitDir, nil, "--git-dir="+gitDir, "cat-file", "blob", object)
	if err != nil {
		return err
	}
	if _, _, err := parsePolicyBytes(contents); err != nil {
		return fmt.Errorf("protected branch %s has an invalid .hn/policy.json at its tip %s: %v", ref, tip, err)
	}
	return nil
}

// serveAdminGit runs git in directory with the system configuration ignored,
// repository discovery stopped at directory and no inherited variable that
// redirects it (gitRoutingVariables). Errors carry Git's standard error.
func serveAdminGit(directory string, input []byte, args ...string) ([]byte, error) {
	command := exec.Command("git", args...)
	command.Dir = directory
	command.Env = append(isolatedGitEnvironment(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CEILING_DIRECTORIES="+filepath.Dir(directory))
	command.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), message)
	}
	return stdout.Bytes(), nil
}
