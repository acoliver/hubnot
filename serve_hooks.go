package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// hn serve hook pre-receive and hn serve hook post-receive run inside
// git receive-pack, started by the generated hook scripts. hn serve git
// holds the repository lock for the whole receive-pack run, so the hooks
// never take RepoLock themselves: a second flock from this process would
// wait until the lock budget expires. Git relays the hooks' stderr to the
// client with a "remote: " prefix. Nothing here writes to stdout.

const (
	serveHookPreReceive  = "pre-receive"
	serveHookPostReceive = "post-receive"
	serveHookUsage       = "usage: hn serve hook pre-receive|post-receive"

	// Bounds on a hook's standard input. Git writes one line per ref
	// update; a push beyond either bound is rejected with over-budget.
	serveRefUpdateLineLimit   = 10_000
	serveRefUpdateByteLimit   = 4 << 20
	serveRefUpdatesBudget     = "ref-updates"
	serveRefUpdateBytesBudget = "ref-update-bytes"
	serveHookReadChunk        = 64 << 10

	// Only hn-advance and hn-expected-tip exist, each at most once.
	servePushOptionLimit = 2

	servePushOptionSentence = "push options other than hn-advance and hn-expected-tip are not supported."
)

// serveHook is one hook invocation. cmdServeHook fills it from the
// process; tests may construct it with their own streams and environment.
type serveHook struct {
	layout      serveLayout
	environment []string
	workingDir  string
	stdin       io.Reader
	stderr      io.Writer
	now         func() time.Time
}

// serveHookSession is what the entry checks establish: the hosted
// repository Git is updating, the actor and key the wrapper mapped, and
// the repository configuration, loaded once.
type serveHookSession struct {
	Repo      string
	Actor     string
	Key       string
	GitDir    string
	Config    serveRepoConfig
	OIDLength int
}

// cmdServeHook is the session route for hn serve hook. cmdServe has already
// refused uid 0 and validated the layout read-only.
func cmdServeHook(layout serveLayout, args []string) error {
	if len(args) != 1 || (args[0] != serveHookPreReceive && args[0] != serveHookPostReceive) {
		return usageError(serveHookUsage)
	}
	workingDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("hn serve hook: cannot read the working directory: %v", err)
	}
	hook := &serveHook{
		layout:      layout,
		environment: os.Environ(),
		workingDir:  workingDir,
		stdin:       os.Stdin,
		stderr:      os.Stderr,
		now:         time.Now,
	}
	if args[0] == serveHookPostReceive {
		// post-receive never changes the verdict, so it always exits 0.
		hook.postReceive()
		return nil
	}
	status, err := hook.preReceive()
	if err != nil {
		return err
	}
	if status != 0 {
		// The rejection lines are already on stderr; main would add an
		// "hn: " line. Nothing is held, so exiting skips no cleanup.
		os.Exit(status)
	}
	return nil
}

func serveHookError(format string, args ...any) error {
	return fmt.Errorf("hn serve hook: "+format, args...)
}

// enter performs the entry checks of T041. Any failure is an error, which
// makes pre-receive exit 1 and therefore rejects the whole push.
func (h *serveHook) enter() (serveHookSession, error) {
	var session serveHookSession
	for _, name := range []string{"HN_SERVE_ACTOR", "HN_SERVE_KEY", "HN_SERVE_REPOSITORY"} {
		if value, ok := lookupServeEnvironment(h.environment, name); !ok || value == "" {
			return session, serveHookError("%s is not set; hooks run only inside git receive-pack started by hn serve git", name)
		}
	}
	session.Actor, _ = lookupServeEnvironment(h.environment, "HN_SERVE_ACTOR")
	session.Key, _ = lookupServeEnvironment(h.environment, "HN_SERVE_KEY")
	session.Repo, _ = lookupServeEnvironment(h.environment, "HN_SERVE_REPOSITORY")
	if !validActorFingerprint(session.Actor) {
		return session, serveHookError("HN_SERVE_ACTOR is not a full 64-hex actor fingerprint")
	}
	if !validServeFingerprint(session.Key) {
		return session, serveHookError("HN_SERVE_KEY is not a SHA256 key fingerprint")
	}
	if !validServeRepositoryName(session.Repo) {
		return session, serveHookError("HN_SERVE_REPOSITORY is not a valid repository name")
	}
	gitDir, err := h.resolveGitDir(session.Repo)
	if err != nil {
		return session, err
	}
	session.GitDir = gitDir
	registry, err := loadServeRegistry(h.layout)
	if err != nil {
		return session, serveHookError("%v", err)
	}
	binding, bound := lookupServeKey(registry, session.Key)
	if !bound {
		return session, serveHookError("key %s is no longer registered", session.Key)
	}
	if binding.Actor != session.Actor {
		return session, serveHookError("key %s is no longer bound to actor %s", session.Key, session.Actor)
	}
	session.Config, err = loadServeRepoConfig(h.layout, session.Repo)
	if err != nil {
		return session, serveHookError("repository %s is not hosted: %v", session.Repo, err)
	}
	session.OIDLength, err = serveObjectFormatLength(session.GitDir)
	if err != nil {
		return session, serveHookError("%v", err)
	}
	return session, nil
}

// resolveGitDir requires GIT_DIR, resolved against the working directory
// with symlinks evaluated, to be the hosted repository of repo.
func (h *serveHook) resolveGitDir(repo string) (string, error) {
	gitDir, ok := lookupServeEnvironment(h.environment, "GIT_DIR")
	if !ok || gitDir == "" {
		return "", serveHookError("GIT_DIR is not set; hooks run only inside git receive-pack started by hn serve git")
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(h.workingDir, gitDir)
	}
	resolved, err := filepath.EvalSymlinks(gitDir)
	if err != nil {
		return "", serveHookError("cannot resolve GIT_DIR %s: %v", gitDir, err)
	}
	hosted := h.layout.RepoGitDir(repo)
	want, err := filepath.EvalSymlinks(hosted)
	if err != nil {
		return "", serveHookError("cannot resolve hosted repository %s: %v", hosted, err)
	}
	if resolved != want {
		return "", serveHookError("GIT_DIR %s is not the hosted repository %s", gitDir, hosted)
	}
	return want, nil
}

// serveObjectFormatLength returns the hex length of object IDs in gitDir.
func serveObjectFormatLength(gitDir string) (int, error) {
	format, err := gitTextAt(gitDir, "rev-parse", "--show-object-format")
	if err != nil {
		return 0, err
	}
	switch format {
	case "sha1":
		return 40, nil
	case "sha256":
		return 64, nil
	default:
		return 0, fmt.Errorf("unsupported object format %q", format)
	}
}

// serveRefUpdateBudgetError reports hook input beyond one of the bounds.
type serveRefUpdateBudgetError struct {
	Budget   string
	Limit    int64
	Measured int64
}

func (e *serveRefUpdateBudgetError) Error() string {
	return fmt.Sprintf("%s: %d exceeds the limit %d", e.Budget, e.Measured, e.Limit)
}

// parseServeRefUpdates reads every OLD NEW REF line. It always reads the
// input to its end, so Git never sees a broken pipe, but keeps only the
// first serveRefUpdateByteLimit bytes; past either bound it returns a
// serveRefUpdateBudgetError that measures the whole input. Any malformed
// line is an error that rejects the push.
func parseServeRefUpdates(input io.Reader, oidLength int) ([]serveRefUpdate, error) {
	var kept bytes.Buffer
	var total, lines int64
	last := byte('\n')
	chunk := make([]byte, serveHookReadChunk)
	for {
		n, err := input.Read(chunk)
		if n > 0 {
			data := chunk[:n]
			lines += int64(bytes.Count(data, []byte{'\n'}))
			total += int64(n)
			last = data[n-1]
			if total <= serveRefUpdateByteLimit {
				kept.Write(data)
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read ref updates: %w", err)
		}
	}
	if last != '\n' {
		lines++
	}
	if total > serveRefUpdateByteLimit {
		return nil, &serveRefUpdateBudgetError{Budget: serveRefUpdateBytesBudget, Limit: serveRefUpdateByteLimit, Measured: total}
	}
	if lines > serveRefUpdateLineLimit {
		return nil, &serveRefUpdateBudgetError{Budget: serveRefUpdatesBudget, Limit: serveRefUpdateLineLimit, Measured: lines}
	}
	text := kept.String()
	if text == "" {
		return nil, nil
	}
	if !strings.HasSuffix(text, "\n") {
		return nil, errors.New("the last ref update line is not terminated by a newline")
	}
	updates := make([]serveRefUpdate, 0, lines)
	seen := make(map[string]bool, lines)
	for number, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		update, err := parseServeRefUpdateLine(line, oidLength)
		if err != nil {
			return nil, fmt.Errorf("ref update line %d: %w", number+1, err)
		}
		if seen[update.Ref] {
			return nil, fmt.Errorf("ref update line %d repeats %s", number+1, update.Ref)
		}
		seen[update.Ref] = true
		updates = append(updates, update)
	}
	return updates, nil
}

func parseServeRefUpdateLine(line string, oidLength int) (serveRefUpdate, error) {
	fields := strings.Split(line, " ")
	if len(fields) != 3 {
		return serveRefUpdate{}, errors.New("want OLD NEW REF separated by single spaces")
	}
	update := serveRefUpdate{Old: fields[0], New: fields[1], Ref: fields[2]}
	if !serveLowerHexOfLength(update.Old, oidLength) || !serveLowerHexOfLength(update.New, oidLength) {
		return serveRefUpdate{}, fmt.Errorf("object IDs must be %d lowercase hex digits", oidLength)
	}
	if update.Creates() && update.Deletes() {
		return serveRefUpdate{}, errors.New("both object IDs are zero")
	}
	// Git has already checked the ref name; classification decides what
	// the server accepts.
	if update.Ref == "" {
		return serveRefUpdate{}, errors.New("empty ref name")
	}
	return update, nil
}

func serveLowerHexOfLength(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

// parseServePushOptions reads GIT_PUSH_OPTION_COUNT and GIT_PUSH_OPTION_<n>.
// An absent count means no options. Every error rejects every ref update
// with unsupported-command.
func parseServePushOptions(environment []string, oidLength int) (servePushOptions, error) {
	var options servePushOptions
	countText, present := lookupServeEnvironment(environment, "GIT_PUSH_OPTION_COUNT")
	if !present {
		return options, nil
	}
	count, err := serveParseDecimal(countText)
	if err != nil {
		return options, fmt.Errorf("GIT_PUSH_OPTION_COUNT: %w", err)
	}
	if count > servePushOptionLimit {
		return options, fmt.Errorf("%d push options; at most %d are supported", count, servePushOptionLimit)
	}
	for index := int64(0); index < count; index++ {
		name := "GIT_PUSH_OPTION_" + strconv.FormatInt(index, 10)
		value, ok := lookupServeEnvironment(environment, name)
		if !ok {
			return options, fmt.Errorf("%s is not set", name)
		}
		option, argument, found := strings.Cut(value, "=")
		if !found {
			return options, fmt.Errorf("push option %q has no value", value)
		}
		switch option {
		case servePushOptionAdvance:
			if options.Advance != "" {
				return options, fmt.Errorf("%s appears more than once", option)
			}
			digest, ok := strings.CutPrefix(argument, "sha256:")
			if !ok || !serveLowerHexOfLength(digest, 64) {
				return options, fmt.Errorf("%s value %q is not a full event ID", option, argument)
			}
			options.Advance = argument
		case servePushOptionExpected:
			if options.ExpectedTip != "" {
				return options, fmt.Errorf("%s appears more than once", option)
			}
			if !serveLowerHexOfLength(argument, oidLength) {
				return options, fmt.Errorf("%s value %q is not a full commit ID", option, argument)
			}
			options.ExpectedTip = argument
		default:
			return options, fmt.Errorf("push option %q is not supported", option)
		}
	}
	return options, nil
}

// preReceive returns 1 after printing rejections and 0 when every ref
// update passes. An error is an entry-check or internal failure that main
// reports with an hn: line and exit status 1, which also rejects the push.
func (h *serveHook) preReceive() (int, error) {
	session, err := h.enter()
	if err != nil {
		return 0, err
	}
	updates, err := parseServeRefUpdates(h.stdin, session.OIDLength)
	var budget *serveRefUpdateBudgetError
	if errors.As(err, &budget) {
		return h.reject(session, nil, []serveRejection{{
			Ref:      "-",
			Actor:    session.Actor,
			Code:     serveReasonOverBudget,
			Budget:   budget.Budget,
			Limit:    budget.Limit,
			Measured: budget.Measured,
			Next:     serveReasonDefaultNext(serveReasonOverBudget),
			Sentence: "the push carries more ref updates than the server accepts at once; push fewer refs at a time.",
		}}), nil
	}
	if err != nil {
		return 0, serveHookError("%v", err)
	}
	if len(updates) == 0 {
		return 0, nil
	}
	owners, err := loadServeBranchOwners(h.layout, session.Repo)
	if err != nil {
		return 0, serveHookError("%v", err)
	}
	classifyServeRefUpdates(session.Config, updates)
	ctx := newServeVerdictContext(session.GitDir, session.Actor, session.Key, session.Config, owners)
	var rejections []serveRejection
	if options, err := parseServePushOptions(h.environment, session.OIDLength); err != nil {
		rejections = rejectServeEveryUpdate(ctx, updates, serveReasonUnsupportedCommand, servePushOptionSentence)
	} else {
		rejections = evaluateServePush(ctx, updates, options)
	}
	if len(rejections) == 0 {
		return 0, nil
	}
	return h.reject(session, updates, rejections), nil
}

// reject prints the rejections in ref order, appends a rejected push log
// entry with the codes in line order, and returns exit status 1. A push log
// failure is reported but cannot turn the rejection into an acceptance.
func (h *serveHook) reject(session serveHookSession, updates []serveRefUpdate, rejections []serveRejection) int {
	sortServeRejections(rejections)
	_, _ = io.WriteString(h.stderr, renderServeRejections(rejections))
	codes := make([]string, 0, len(rejections))
	for _, rejection := range rejections {
		codes = append(codes, rejection.Code)
	}
	entry := servePushLogEntry{
		Time:    serveHookTimestamp(h.now()),
		Actor:   session.Actor,
		Key:     session.Key,
		Verdict: servePushVerdictRejected,
		Updates: servePushLogUpdates(updates),
		Codes:   codes,
	}
	if err := appendServePushLog(h.layout, session.Repo, entry); err != nil {
		fmt.Fprintf(h.stderr, "%spush log append failed: %v\n", serveSentencePrefix, serveSanitizeText(err.Error()))
	}
	return 1
}

// postReceive records branch owners and the accepted push log entry. It
// never changes the verdict: each failure is one line on stderr. A missing
// owner record makes its branch unowned, which needs a maintainer for a
// forced update or deletion, so a failure here fails closed.
func (h *serveHook) postReceive() {
	for _, err := range h.recordPush() {
		fmt.Fprintf(h.stderr, "%spost-receive bookkeeping failed: %s\n", serveSentencePrefix, serveSanitizeText(err.Error()))
	}
}

func (h *serveHook) recordPush() []error {
	session, err := h.enter()
	if err != nil {
		return []error{err}
	}
	updates, err := parseServeRefUpdates(h.stdin, session.OIDLength)
	if err != nil {
		return []error{err}
	}
	classifyServeRefUpdates(session.Config, updates)
	now := h.now()
	var failures []error
	if err := recordServeBranchOwners(h.layout, session, updates, now); err != nil {
		failures = append(failures, err)
	}
	entry := servePushLogEntry{
		Time:    serveHookTimestamp(now),
		Actor:   session.Actor,
		Key:     session.Key,
		Verdict: servePushVerdictAccepted,
		Updates: servePushLogUpdates(updates),
	}
	if err := appendServePushLog(h.layout, session.Repo, entry); err != nil {
		failures = append(failures, err)
	}
	return failures
}

// recordServeBranchOwners applies the moved ordinary branches to
// branches.json: a creation records the pushing actor, a deletion removes
// the record and an update keeps it. Protected branches never get records.
// The file is written once, and only when a record changed.
func recordServeBranchOwners(layout serveLayout, session serveHookSession, updates []serveRefUpdate, now time.Time) error {
	owners, err := loadServeBranchOwners(layout, session.Repo)
	if err != nil {
		return err
	}
	records := make(map[string]serveBranchOwner, len(owners.Branches))
	for _, branch := range owners.Branches {
		records[branch.Ref] = branch
	}
	changed := false
	for _, update := range updates {
		if update.Class != serveRefBranch {
			continue
		}
		switch {
		case update.Creates():
			records[update.Ref] = serveBranchOwner{Ref: update.Ref, Owner: session.Actor, Created: serveHookTimestamp(now)}
			changed = true
		case update.Deletes():
			if _, ok := records[update.Ref]; ok {
				delete(records, update.Ref)
				changed = true
			}
		}
	}
	if !changed {
		return nil
	}
	owners.Branches = make([]serveBranchOwner, 0, len(records))
	for _, branch := range records {
		owners.Branches = append(owners.Branches, branch)
	}
	return saveServeBranchOwners(layout, session.Repo, owners)
}

func servePushLogUpdates(updates []serveRefUpdate) []servePushLogUpdate {
	logged := make([]servePushLogUpdate, 0, len(updates))
	for _, update := range updates {
		logged = append(logged, servePushLogUpdate{Ref: update.Ref, Old: update.Old, New: update.New})
	}
	return logged
}

func serveHookTimestamp(now time.Time) string {
	return now.UTC().Format(time.RFC3339)
}
