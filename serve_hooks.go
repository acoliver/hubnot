package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
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
	// update; a push beyond either bound gets one over-budget line pair
	// per ref update, each with budget=ref-updates and the limit and
	// measurement of the bound it exceeded.
	serveRefUpdateLineLimit = 10_000
	serveRefUpdateByteLimit = 4 << 20
	serveRefUpdatesBudget   = "ref-updates"
	// serveRefUpdateBytesBudget tells the byte bound apart inside a
	// serveRefUpdateBudgetError. Rejection lines still name
	// serveRefUpdatesBudget, with the byte limit and the byte count.
	serveRefUpdateBytesBudget = "ref-update-bytes"
	// serveRefUpdateMaxLine bounds one input line. receive-pack reads each
	// ref update from one pkt-line of at most 65520 bytes, so a longer
	// line cannot come from Git and is malformed.
	serveRefUpdateMaxLine = 65520
	// serveRefUpdateMergeFanIn bounds how many sorted spill runs, and so
	// open files, one merge reads at once.
	serveRefUpdateMergeFanIn = 64
	serveHookSpillPrefix     = ".hn-hook-refs-"

	// Only hn-advance, hn-expected-tip and hn-rule exist, each at most once.
	servePushOptionLimit = 3

	servePushOptionSentence = "push options other than hn-advance, hn-expected-tip and hn-rule are not supported."
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

// serveRefUpdateBudgetSentence is the sentence of every over-budget line for
// a push beyond one of the input bounds.
func serveRefUpdateBudgetSentence(budget *serveRefUpdateBudgetError) string {
	if budget.Budget == serveRefUpdateBytesBudget {
		return fmt.Sprintf("the push's ref update lines take %d bytes and the server reads at most %d bytes of them in one push; push fewer refs at a time.", budget.Measured, budget.Limit)
	}
	return fmt.Sprintf("the push carries %d ref updates and the server accepts at most %d in one push; push fewer refs at a time.", budget.Measured, budget.Limit)
}

// serveRefUpdateInput is a hook's standard input, read to its end.
type serveRefUpdateInput struct {
	// Updates holds every update in input order when the input is
	// well-formed and within both bounds.
	Updates []serveRefUpdate
	// Bytes and Lines measure the whole input.
	Bytes, Lines int64
	// Malformed describes the first malformed line, if any.
	Malformed error
	// spill holds every update, in sorted runs and in input order, when
	// the input is well-formed but beyond a bound and the reader was given
	// a spill directory.
	spill *serveRefUpdateSpill
}

func (input *serveRefUpdateInput) overBudget() bool {
	return input.Bytes > serveRefUpdateByteLimit || input.Lines > serveRefUpdateLineLimit
}

// Budget returns the first bound the input exceeds, checking bytes before
// lines, or nil when it is within both.
func (input *serveRefUpdateInput) Budget() *serveRefUpdateBudgetError {
	switch {
	case input.Bytes > serveRefUpdateByteLimit:
		return &serveRefUpdateBudgetError{Budget: serveRefUpdateBytesBudget, Limit: serveRefUpdateByteLimit, Measured: input.Bytes}
	case input.Lines > serveRefUpdateLineLimit:
		return &serveRefUpdateBudgetError{Budget: serveRefUpdatesBudget, Limit: serveRefUpdateLineLimit, Measured: input.Lines}
	}
	return nil
}

// Close removes the spill files, if any.
func (input *serveRefUpdateInput) Close() error {
	if input.spill == nil {
		return nil
	}
	err := input.spill.remove()
	input.spill = nil
	return err
}

// parseServeRefUpdates reads every OLD NEW REF line into memory. It is for
// input that must be within the bounds, such as post-receive's, which lists
// only refs of a push pre-receive accepted. Past either bound it returns a
// serveRefUpdateBudgetError measuring the whole input; otherwise a
// malformed line is an error.
func parseServeRefUpdates(source io.Reader, oidLength int) ([]serveRefUpdate, error) {
	input, err := readServeRefUpdates(source, oidLength, "")
	if err != nil {
		return nil, err
	}
	if budget := input.Budget(); budget != nil {
		return nil, budget
	}
	if input.Malformed != nil {
		return nil, input.Malformed
	}
	return input.Updates, nil
}

// readServeRefUpdates reads every OLD NEW REF line to the end of source, so
// Git never sees a broken pipe, with bounded memory. Updates stay in memory
// only while the input is within both bounds. Past a bound they are
// discarded, or, when spillDir is not empty, moved to files in a new
// private directory below spillDir so the caller can still name every
// update. A malformed line, including a repeated ref while the input is in
// memory, sets Malformed and stops keeping updates. The error result is an
// I/O failure; the caller must Close a returned input.
func readServeRefUpdates(source io.Reader, oidLength int, spillDir string) (*serveRefUpdateInput, error) {
	reader := &serveRefUpdateReader{
		input:     &serveRefUpdateInput{},
		oidLength: oidLength,
		spillDir:  spillDir,
		seen:      map[string]bool{},
	}
	buffered := bufio.NewReaderSize(source, serveRefUpdateMaxLine)
	tooLong := false
	for {
		chunk, err := buffered.ReadSlice('\n')
		reader.input.Bytes += int64(len(chunk))
		var takeErr error
		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			tooLong = true
			continue
		case err == nil && tooLong:
			tooLong = false
			takeErr = reader.take("", fmt.Errorf("the line is longer than %d bytes", serveRefUpdateMaxLine))
		case err == nil:
			takeErr = reader.take(string(chunk[:len(chunk)-1]), nil)
		case errors.Is(err, io.EOF):
			if len(chunk) > 0 || tooLong {
				takeErr = reader.take("", errors.New("the line is not terminated by a newline"))
			}
			if takeErr == nil && reader.input.spill != nil {
				takeErr = reader.input.spill.finish()
			}
			if takeErr != nil {
				_ = reader.input.Close()
				return nil, takeErr
			}
			return reader.input, nil
		default:
			takeErr = fmt.Errorf("read ref updates: %w", err)
		}
		if takeErr != nil {
			_, _ = io.Copy(io.Discard, buffered)
			_ = reader.input.Close()
			return nil, takeErr
		}
	}
}

type serveRefUpdateReader struct {
	input      *serveRefUpdateInput
	oidLength  int
	spillDir   string
	seen       map[string]bool
	overflowed bool
}

// take counts one complete line and keeps its update where it belongs.
// problem is a defect found before parsing, such as an overlong line.
func (reader *serveRefUpdateReader) take(line string, problem error) error {
	input := reader.input
	input.Lines++
	if input.overBudget() && !reader.overflowed {
		if err := reader.overflow(); err != nil {
			return err
		}
	}
	if input.Malformed != nil {
		return nil
	}
	var update serveRefUpdate
	if problem == nil {
		update, problem = parseServeRefUpdateLine(line, reader.oidLength)
	}
	if problem == nil && !reader.overflowed && reader.seen[update.Ref] {
		problem = fmt.Errorf("repeats %s", update.Ref)
	}
	if problem != nil {
		input.Malformed = fmt.Errorf("ref update line %d: %w", input.Lines, problem)
		input.Updates, reader.seen = nil, nil
		return nil
	}
	switch {
	case input.spill != nil:
		return input.spill.add(update)
	case reader.overflowed:
		return nil
	}
	reader.seen[update.Ref] = true
	input.Updates = append(input.Updates, update)
	return nil
}

// overflow runs once, when the input first exceeds a bound. The updates
// kept so far move to a spill, or are dropped when there is no spill
// directory or the input is already malformed.
func (reader *serveRefUpdateReader) overflow() error {
	reader.overflowed = true
	kept := reader.input.Updates
	reader.input.Updates, reader.seen = nil, nil
	if reader.spillDir == "" || reader.input.Malformed != nil {
		return nil
	}
	spill, err := newServeRefUpdateSpill(reader.spillDir)
	if err != nil {
		return err
	}
	reader.input.spill = spill
	for _, update := range kept {
		if err := spill.add(update); err != nil {
			return err
		}
	}
	return nil
}

// serveRefUpdateSpill keeps the updates of a push beyond the input bounds
// on disk in a private directory below the server's tmp directory: every
// line in input order, and the same lines as runs sorted by ref of at most
// runLimit bytes each. Memory holds one run plus one buffered reader per
// merged run, and at most fanIn runs are merged at once.
type serveRefUpdateSpill struct {
	dir      string
	input    *os.File
	inputOut *bufio.Writer
	count    int64
	run      []serveRefUpdate
	runBytes int
	runs     []string
	files    int
	runLimit int
	fanIn    int
}

func newServeRefUpdateSpill(parent string) (*serveRefUpdateSpill, error) {
	dir, err := os.MkdirTemp(parent, serveHookSpillPrefix+"*")
	if err != nil {
		return nil, fmt.Errorf("create hook scratch directory: %w", err)
	}
	input, err := os.OpenFile(filepath.Join(dir, "input"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("create hook scratch file: %w", err)
	}
	return &serveRefUpdateSpill{
		dir:      dir,
		input:    input,
		inputOut: bufio.NewWriter(input),
		runLimit: serveRefUpdateByteLimit,
		fanIn:    serveRefUpdateMergeFanIn,
	}, nil
}

func serveRefUpdateText(update serveRefUpdate) string {
	return update.Old + " " + update.New + " " + update.Ref + "\n"
}

func (spill *serveRefUpdateSpill) add(update serveRefUpdate) error {
	text := serveRefUpdateText(update)
	if _, err := spill.inputOut.WriteString(text); err != nil {
		return err
	}
	spill.count++
	spill.run = append(spill.run, update)
	spill.runBytes += len(text)
	if spill.runBytes >= spill.runLimit {
		return spill.flushRun()
	}
	return nil
}

// flushRun writes the current run sorted by ref. The sort is stable, so
// updates of one ref keep their input order.
func (spill *serveRefUpdateSpill) flushRun() error {
	if len(spill.run) == 0 {
		return nil
	}
	slices.SortStableFunc(spill.run, func(a, b serveRefUpdate) int { return strings.Compare(a.Ref, b.Ref) })
	path, err := spill.writeFile(func(out *bufio.Writer) error {
		for _, update := range spill.run {
			if _, err := out.WriteString(serveRefUpdateText(update)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	spill.runs = append(spill.runs, path)
	spill.run, spill.runBytes = spill.run[:0], 0
	return nil
}

func (spill *serveRefUpdateSpill) writeFile(fill func(*bufio.Writer) error) (string, error) {
	spill.files++
	path := filepath.Join(spill.dir, "run-"+strconv.Itoa(spill.files))
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	out := bufio.NewWriter(file)
	err = fill(out)
	if err == nil {
		err = out.Flush()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return path, err
}

// finish writes the last run and merges groups of runs until at most fanIn
// remain. Groups are contiguous and the merge prefers the earlier run for
// equal refs, so updates of one ref keep their input order.
func (spill *serveRefUpdateSpill) finish() error {
	if err := spill.inputOut.Flush(); err != nil {
		return err
	}
	if err := spill.flushRun(); err != nil {
		return err
	}
	spill.run = nil
	for len(spill.runs) > spill.fanIn {
		var merged []string
		for start := 0; start < len(spill.runs); start += spill.fanIn {
			group := spill.runs[start:min(start+spill.fanIn, len(spill.runs))]
			path, err := spill.writeFile(func(out *bufio.Writer) error {
				return mergeServeRefUpdateRuns(group, func(update serveRefUpdate) error {
					_, err := out.WriteString(serveRefUpdateText(update))
					return err
				})
			})
			if err != nil {
				return err
			}
			for _, done := range group {
				if err := os.Remove(done); err != nil {
					return err
				}
			}
			merged = append(merged, path)
		}
		spill.runs = merged
	}
	return nil
}

// eachSorted visits every update in byte order of the ref.
func (spill *serveRefUpdateSpill) eachSorted(visit func(serveRefUpdate) error) error {
	return mergeServeRefUpdateRuns(spill.runs, visit)
}

// eachInput visits every update in input order.
func (spill *serveRefUpdateSpill) eachInput(visit func(serveRefUpdate) error) error {
	file, err := os.Open(spill.input.Name())
	if err != nil {
		return err
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	for {
		update, ok, err := readServeSpilledUpdate(reader)
		if err != nil || !ok {
			return err
		}
		if err := visit(update); err != nil {
			return err
		}
	}
}

func (spill *serveRefUpdateSpill) remove() error {
	_ = spill.input.Close()
	return os.RemoveAll(spill.dir)
}

// readServeSpilledUpdate reads one line the spill wrote. ok is false at
// the end of the file.
func readServeSpilledUpdate(reader *bufio.Reader) (serveRefUpdate, bool, error) {
	line, err := reader.ReadString('\n')
	if errors.Is(err, io.EOF) && line == "" {
		return serveRefUpdate{}, false, nil
	}
	if errors.Is(err, io.EOF) {
		return serveRefUpdate{}, false, errors.New("hook scratch file ends inside a line")
	}
	if err != nil {
		return serveRefUpdate{}, false, err
	}
	fields := strings.SplitN(strings.TrimSuffix(line, "\n"), " ", 3)
	if len(fields) != 3 {
		return serveRefUpdate{}, false, fmt.Errorf("hook scratch file has a damaged line %q", line)
	}
	return serveRefUpdate{Old: fields[0], New: fields[1], Ref: fields[2]}, true, nil
}

// mergeServeRefUpdateRuns visits the updates of sorted runs in byte order
// of the ref; for equal refs the earlier run comes first.
func mergeServeRefUpdateRuns(paths []string, visit func(serveRefUpdate) error) error {
	type head struct {
		reader *bufio.Reader
		update serveRefUpdate
		ok     bool
	}
	heads := make([]head, len(paths))
	for index, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		heads[index].reader = bufio.NewReader(file)
		heads[index].update, heads[index].ok, err = readServeSpilledUpdate(heads[index].reader)
		if err != nil {
			return err
		}
	}
	for {
		best := -1
		for index := range heads {
			if heads[index].ok && (best < 0 || heads[index].update.Ref < heads[best].update.Ref) {
				best = index
			}
		}
		if best < 0 {
			return nil
		}
		if err := visit(heads[best].update); err != nil {
			return err
		}
		var err error
		heads[best].update, heads[best].ok, err = readServeSpilledUpdate(heads[best].reader)
		if err != nil {
			return err
		}
	}
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
// An absent count means no options, and hn-rule is valid only together with
// hn-advance. Every error rejects every ref update with unsupported-command.
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
		case servePushOptionRule:
			if options.Rule != "" {
				return options, fmt.Errorf("%s appears more than once", option)
			}
			if !validPipelineName(argument) {
				return options, fmt.Errorf("%s value %q is not a valid rule name", option, argument)
			}
			options.Rule = argument
		default:
			return options, fmt.Errorf("push option %q is not supported", option)
		}
	}
	if options.Rule != "" && options.Advance == "" {
		return options, fmt.Errorf("%s requires %s", servePushOptionRule, servePushOptionAdvance)
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
	input, err := readServeRefUpdates(h.stdin, session.OIDLength, h.layout.Tmp)
	if err != nil {
		return 0, serveHookError("%v", err)
	}
	defer func() {
		if err := input.Close(); err != nil {
			fmt.Fprintf(h.stderr, "%scannot remove hook scratch files: %s\n", serveSentencePrefix, serveSanitizeText(err.Error()))
		}
	}()
	if input.Malformed != nil {
		return 0, serveHookError("%v", input.Malformed)
	}
	if budget := input.Budget(); budget != nil {
		return h.rejectOverBudget(session, input, budget)
	}
	updates := input.Updates
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

// rejectOverBudget rejects a push beyond one of the input bounds: one
// over-budget line pair for every ref update, in ref order, and a rejected
// push log entry listing every update with one code per line. Both are
// streamed from the spill files, so memory stays bounded however many ref
// updates the push carries.
func (h *serveHook) rejectOverBudget(session serveHookSession, input *serveRefUpdateInput, budget *serveRefUpdateBudgetError) (int, error) {
	if input.spill == nil {
		return 0, serveHookError("%v", budget)
	}
	template := serveRejection{
		Actor:    session.Actor,
		Code:     serveReasonOverBudget,
		Budget:   serveRefUpdatesBudget,
		Limit:    budget.Limit,
		Measured: budget.Measured,
		Next:     serveReasonDefaultNext(serveReasonOverBudget),
		Sentence: serveRefUpdateBudgetSentence(budget),
	}
	out := bufio.NewWriter(h.stderr)
	err := input.spill.eachSorted(func(update serveRefUpdate) error {
		rejection := template
		rejection.Ref = update.Ref
		_, err := out.WriteString(renderServeRejection(rejection))
		return err
	})
	if flushErr := out.Flush(); err == nil {
		err = flushErr
	}
	if err != nil {
		return 0, serveHookError("cannot report the over-budget push: %v", err)
	}
	if err := h.logOverBudget(session, input.spill); err != nil {
		fmt.Fprintf(h.stderr, "%spush log append failed: %v\n", serveSentencePrefix, serveSanitizeText(err.Error()))
	}
	return 1, nil
}

func (h *serveHook) logOverBudget(session serveHookSession, spill *serveRefUpdateSpill) error {
	stream, err := newServePushLogStream(spill.dir, servePushLogEntry{
		Time:    serveHookTimestamp(h.now()),
		Actor:   session.Actor,
		Key:     session.Key,
		Verdict: servePushVerdictRejected,
	})
	if err != nil {
		return err
	}
	defer stream.Discard()
	err = spill.eachInput(func(update serveRefUpdate) error {
		return stream.AddUpdate(servePushLogUpdate{Ref: update.Ref, Old: update.Old, New: update.New})
	})
	if err != nil {
		return err
	}
	for range spill.count {
		if err := stream.AddCode(serveReasonOverBudget); err != nil {
			return err
		}
	}
	return stream.Append(h.layout, session.Repo)
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
