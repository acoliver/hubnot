package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"
)

// maxRunnerAcceptedActors bounds --accept-actor like policy actor lists.
const maxRunnerAcceptedActors = 64

type runnerOptions struct {
	Remote   string
	Pipeline string
	// AcceptedActor is the first, and for a single-actor runner the only,
	// accepted request signer.
	AcceptedActor string
	// AcceptedActors lists every accepted signer when --accept-actor is
	// repeated. A runner serving several actors executes a request only when
	// its signer may request the pipeline under the proposal's base policy.
	AcceptedActors  []string
	Backend         executionBackend
	Interval        time.Duration
	ReportNoPending bool
	// SkipStaleBase, when set, names the remote branch a proposal must still
	// be based on: a request whose proposal base is not that branch's tip on
	// the remote is skipped, since its result can never be used to merge.
	SkipStaleBase string
	// DeferBehindPassed, when positive, defers a request while another live
	// proposal on the same base has a result this runner passed within that
	// long: that proposal is about to merge and move the base, which would
	// make this request's result useless.
	DeferBehindPassed time.Duration
	// SkipRequests are full run request IDs the operator told this runner
	// not to execute, for example one known to exceed its step timeout.
	// hn has no cancel event, so the request stays unanswered.
	SkipRequests map[string]bool
	// OnlyPolicyChanges limits the runner to requests whose proposal changes
	// nothing outside .hn/, so a policy amendment can be served at once
	// while another runner holds the machine for a long build.
	OnlyPolicyChanges bool
}

// runnerReportedSkips remembers, per request ID, the reason last printed
// for skipping it, so a watching runner reports each skip once instead of
// on every polling cycle.
var runnerReportedSkips = map[string]string{}

// acceptedActors returns every signer whose requests the runner accepts.
func (options runnerOptions) acceptedActors() []string {
	if len(options.AcceptedActors) > 0 {
		return options.AcceptedActors
	}
	return []string{options.AcceptedActor}
}

// acceptedActorsLabel names the accepted signers in runner status lines.
func (options runnerOptions) acceptedActorsLabel() string {
	actors := options.acceptedActors()
	if len(actors) == 1 {
		return "actor " + shortID(actors[0])
	}
	short := make([]string, len(actors))
	for index, actor := range actors {
		short[index] = shortID(actor)
	}
	return "actors " + strings.Join(short, ", ")
}

func cmdRunner(args []string) error {
	if len(args) == 0 {
		return usageError("usage: hn runner <once|watch> --accept-pipeline NAME --accept-actor ACTOR [--accept-actor ACTOR]...")
	}
	switch args[0] {
	case "once":
		options, err := parseRunnerOptions("runner once", args[1:], false)
		if err != nil {
			return err
		}
		options.ReportNoPending = true
		_, err = runnerOnce(options)
		return err
	case "watch":
		options, err := parseRunnerOptions("runner watch", args[1:], true)
		if err != nil {
			return err
		}
		return runnerWatch(options)
	default:
		return fmt.Errorf("unknown runner command %q", args[0])
	}
}

func parseRunnerOptions(name string, args []string, watching bool) (runnerOptions, error) {
	flags := quietFlags(name)
	remote := flags.String("remote", "origin", "Git remote to synchronize")
	pipeline := flags.String("accept-pipeline", "", "only execute this pipeline name")
	var actors repeatedStringFlag
	flags.Var(&actors, "accept-actor", "only execute requests signed by this actor; repeat for up to 64 actors")
	backendName := flags.String("backend", "sandbox", "execution backend: sandbox or host")
	allowHost := flags.Bool("allow-unsafe-host-execution", false, "permit an unsandboxed host backend")
	interval := flags.Duration("interval", 30*time.Second, "watch polling interval")
	deferBehind := flags.Duration("defer-behind-passed", 0, "defer a request while another proposal on the same base passed within this long and has not merged (0 disables)")
	skipStaleBase := flags.String("skip-stale-base", "", "skip requests whose proposal base is not this branch's tip on the remote")
	onlyPolicy := flags.Bool("only-policy-changes", false, "only execute requests whose proposal changes nothing outside .hn/")
	var skipRequests repeatedStringFlag
	flags.Var(&skipRequests, "skip-request", "never execute this full run request ID; repeatable")
	if err := flags.Parse(args); err != nil {
		return runnerOptions{}, err
	}
	if flags.NArg() != 0 || !validPipelineName(*pipeline) || len(actors) == 0 || (len(actors) == 1 && !validActorID(actors[0])) {
		return runnerOptions{}, usageError("runner requires --accept-pipeline NAME and a full --accept-actor ACTOR fingerprint")
	}
	if len(actors) > maxRunnerAcceptedActors {
		return runnerOptions{}, usageError(fmt.Sprintf("runner accepts 1 to %d --accept-actor fingerprints, got %d", maxRunnerAcceptedActors, len(actors)))
	}
	seen := make(map[string]bool, len(actors))
	for _, actor := range actors {
		if !validActorID(actor) {
			return runnerOptions{}, usageError(fmt.Sprintf("runner --accept-actor %q is not a full actor fingerprint", actor))
		}
		// Hex decoding accepts either case, so the same fingerprint spelled
		// twice in different cases is still a duplicate.
		key := strings.ToLower(actor)
		if seen[key] {
			return runnerOptions{}, usageError(fmt.Sprintf("runner --accept-actor %s is listed more than once", actor))
		}
		seen[key] = true
	}
	if *skipStaleBase != "" {
		if _, err := gitOutput("check-ref-format", "refs/heads/"+*skipStaleBase); err != nil {
			return runnerOptions{}, usageError(fmt.Sprintf("runner --skip-stale-base %q is not a valid branch name", *skipStaleBase))
		}
	}
	skipped := make(map[string]bool, len(skipRequests))
	for _, id := range skipRequests {
		if err := requireFullEventID(id); err != nil {
			return runnerOptions{}, usageError(fmt.Sprintf("runner --skip-request %q: %v", id, err))
		}
		skipped[id] = true
	}
	if watching && *interval < time.Second {
		return runnerOptions{}, fmt.Errorf("runner watch interval must be at least 1s")
	}
	backend, err := selectBackend(*backendName, *allowHost)
	if err != nil {
		return runnerOptions{}, err
	}
	if _, err := loadIdentity(); err != nil {
		return runnerOptions{}, err
	}
	options := runnerOptions{
		Remote:            *remote,
		Pipeline:          *pipeline,
		AcceptedActor:     actors[0],
		Backend:           backend,
		Interval:          *interval,
		SkipStaleBase:     *skipStaleBase,
		DeferBehindPassed: *deferBehind,
		SkipRequests:      skipped,
	}
	options.OnlyPolicyChanges = *onlyPolicy
	if len(actors) > 1 {
		options.AcceptedActors = append([]string(nil), actors...)
	}
	return options, nil
}

func validActorID(actor string) bool {
	decoded, err := hex.DecodeString(actor)
	return err == nil && len(decoded) == 32
}

func runnerOnce(options runnerOptions) (bool, error) {
	return runnerOnceContext(context.Background(), options)
}

func runnerOnceContext(ctx context.Context, options runnerOptions) (bool, error) {
	if err := cmdSync([]string{options.Remote}); err != nil {
		return false, err
	}
	events, err := collectEvents()
	if err != nil {
		return false, err
	}
	identity, err := loadIdentity()
	if err != nil {
		return false, err
	}
	accepted := options.acceptedActors()
	// A request whose result can no longer be used is skipped. Both inputs
	// fail open: when the lineage or the remote tip cannot be read, every
	// request runs as before, because a lost gate costs more than a wasted one.
	lineage, lineageErr := buildLineageIndex(events)
	if lineageErr != nil {
		lineage = nil
	}
	staleTip := ""
	if options.SkipStaleBase != "" {
		staleTip = runnerRemoteBranchTip(options.Remote, options.SkipStaleBase)
	}
	var firstError error
	for _, index := range runnerRequestOrder(events, lineage, options.Pipeline) {
		request := &events[index]
		if request.Event.Kind != "run.request" || request.Event.Pipeline != options.Pipeline || !actorListed(request.Event.Actor, accepted) {
			continue
		}
		alreadyRan := false
		for _, result := range currentRunResults(events, request.ID) {
			if result.Event.Actor == identity.Actor {
				alreadyRan = true
				break
			}
		}
		if alreadyRan {
			continue
		}
		if len(accepted) > 1 {
			counts, err := runRequestCounts(events, request)
			if err != nil {
				fmt.Fprintf(os.Stderr, "hn runner: skipped %s: %v\n", shortID(request.ID), err)
				if firstError == nil {
					firstError = err
				}
				continue
			}
			if !counts {
				fmt.Fprintf(os.Stderr, "hn runner: skipped %s: signer %s is neither a maintainer nor a requester of pipeline %s under the base policy\n", shortID(request.ID), shortID(request.Event.Actor), oneLine(request.Event.Pipeline))
				continue
			}
		}
		reason := runnerDeadRequestReason(events, lineage, request, staleTip)
		if reason == "" && options.DeferBehindPassed > 0 {
			if deferral := runnerDeferBehindPassed(events, lineage, request, identity.Actor, options.DeferBehindPassed, time.Now()); deferral != "" {
				if runnerReportedSkips[request.ID] != deferral {
					runnerReportedSkips[request.ID] = deferral
					fmt.Printf("Runner deferred request %s from %s: %s\n", shortID(request.ID), shortID(request.Event.Actor), deferral)
				}
				continue
			}
		}
		if reason == "" && options.SkipRequests[request.ID] {
			reason = "the runner was started with --skip-request for it"
		}
		if reason == "" && options.OnlyPolicyChanges && !runnerPolicyOnlyRequest(events, request) {
			// Not reported: every other request is passed over on purpose.
			continue
		}
		if reason != "" {
			if runnerReportedSkips[request.ID] != reason {
				runnerReportedSkips[request.ID] = reason
				fmt.Printf("Runner skipped request %s from %s: %s\n", shortID(request.ID), shortID(request.Event.Actor), reason)
			}
			continue
		}
		fmt.Printf("Runner accepted request %s from %s for pipeline %s\n", shortID(request.ID), shortID(request.Event.Actor), oneLine(request.Event.Pipeline))
		stored, err := executeRunRequest(ctx, events, request, identity, options.Backend, false)
		if err != nil {
			fmt.Fprintf(os.Stderr, "hn runner: skipped %s: %v\n", shortID(request.ID), err)
			if firstError == nil {
				firstError = err
			}
			continue
		}
		fmt.Printf("Runner recorded %s result %s with %s backend\n", stored.Event.Outcome, shortID(stored.ID), options.Backend.Name())
		if err := cmdSync([]string{options.Remote}); err != nil {
			return true, err
		}
		return true, nil
	}
	if firstError != nil {
		return false, firstError
	}
	if options.ReportNoPending {
		fmt.Printf("No pending %s requests from %s.\n", oneLine(options.Pipeline), options.acceptedActorsLabel())
	}
	return false, nil
}

// runRequestCounts reports whether request's signer may request its pipeline
// under the base policy of the proposal it names, so a runner serving several
// actors executes only requests whose results can count.
func runRequestCounts(events []StoredEvent, request *StoredEvent) (bool, error) {
	proposal, err := resolveEvent(events, request.Event.Subject)
	if err != nil {
		return false, fmt.Errorf("resolve run proposal: %w", err)
	}
	if !isProposalKind(proposal.Event.Kind) {
		return false, fmt.Errorf("run request does not name a proposal")
	}
	if err := guardBasePolicy("runner", proposal.Event.Base, replicationProposal, proposal.ID); err != nil {
		return false, err
	}
	policy, _, _, err := loadPolicy(proposal.Event.Base)
	if err != nil {
		return false, err
	}
	return mayRequestRun(policy, request.Event.Pipeline, request.Event.Actor), nil
}

func runnerWatch(options runnerOptions) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Printf("Watching %s for pipeline %s requests from %s with %s backend\n", options.Remote, oneLine(options.Pipeline), options.acceptedActorsLabel(), options.Backend.Name())
	for {
		ran, err := runnerOnceContext(ctx, options)
		if err != nil {
			if ctx.Err() != nil {
				fmt.Println("Runner stopped.")
				return nil
			}
			fmt.Fprintf(os.Stderr, "hn runner: %v\n", err)
			ran = false
		}
		if ran {
			continue
		}
		timer := time.NewTimer(options.Interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			fmt.Println("Runner stopped.")
			return nil
		case <-timer.C:
		}
	}
}

// runnerPolicyOnlyRequest reports whether request's proposal changes at
// least one path and only paths below .hn/. Any failure to tell is false,
// so a policy-only runner never executes an ordinary request.
func runnerPolicyOnlyRequest(events []StoredEvent, request *StoredEvent) bool {
	proposal, err := resolveEvent(events, request.Event.Subject)
	if err != nil || !isProposalKind(proposal.Event.Kind) {
		return false
	}
	changed, err := changedPathsAt("", proposal.Event.Base, proposal.Event.Head)
	if err != nil || len(changed) == 0 {
		return false
	}
	for _, path := range changed {
		if !strings.HasPrefix(path, ".hn/") {
			return false
		}
	}
	return true
}

// runnerRequestOrder returns the indexes of events in the order the runner
// considers them: pipeline requests first, by when their proposal's lineage
// first requested the pipeline, so a proposal revised after its base moved
// keeps its place instead of going behind every newer request; then
// everything else in store order. A request whose lineage cannot be read
// is keyed by its own timestamp, which is the previous order.
func runnerRequestOrder(events []StoredEvent, lineage *lineageIndex, pipeline string) []int {
	root := func(request *StoredEvent) string {
		if lineage == nil {
			return request.ID
		}
		state, err := lineage.state(request.Event.Subject)
		if err != nil || state.RootID == "" {
			return request.ID
		}
		return state.RootID
	}
	type entry struct {
		index int
		root  string
		at    time.Time
	}
	var requests []entry
	var rest []int
	first := map[string]time.Time{}
	for index := range events {
		event := &events[index]
		if event.Event.Kind != "run.request" || event.Event.Pipeline != pipeline {
			rest = append(rest, index)
			continue
		}
		key := root(event)
		// An unreadable timestamp sorts last rather than first.
		at, err := time.Parse(time.RFC3339Nano, event.Event.Timestamp)
		if err != nil {
			at = time.Unix(1<<40, 0)
		}
		requests = append(requests, entry{index: index, root: key, at: at})
		if earliest, ok := first[key]; !ok || at.Before(earliest) {
			first[key] = at
		}
	}
	sort.SliceStable(requests, func(i, j int) bool {
		a, b := first[requests[i].root], first[requests[j].root]
		if !a.Equal(b) {
			return a.Before(b)
		}
		return requests[i].at.Before(requests[j].at)
	})
	order := make([]int, 0, len(events))
	for _, request := range requests {
		order = append(order, request.index)
	}
	return append(order, rest...)
}

// runnerDeferBehindPassed returns why request should wait, or "" when it
// should run. It waits while another proposal, outside its lineage and not
// merged, superseded or closed, has the same signed base and a passed result
// from this runner (actor) recorded within window: that proposal merges next,
// moves the base, and leaves this request's result unusable. The wait ends
// when the other proposal merges (the request then has a stale base) or when
// window has passed since its result. Anything that cannot be read means
// run, as with the other skips.
func runnerDeferBehindPassed(events []StoredEvent, lineage *lineageIndex, request *StoredEvent, actor string, window time.Duration, now time.Time) string {
	if lineage == nil {
		return ""
	}
	proposal, err := resolveEvent(events, request.Event.Subject)
	if err != nil || !isProposalKind(proposal.Event.Kind) {
		return ""
	}
	own, err := lineage.state(proposal.ID)
	if err != nil {
		return ""
	}
	for index := range events {
		other := &events[index]
		if other.Event.Kind != "run.request" || other.Event.Pipeline != request.Event.Pipeline || other.Event.Subject == proposal.ID {
			continue
		}
		candidate, err := resolveEvent(events, other.Event.Subject)
		if err != nil || !isProposalKind(candidate.Event.Kind) || candidate.Event.Base != proposal.Event.Base {
			continue
		}
		state, err := lineage.state(candidate.ID)
		if err != nil || state.Merged || state.Superseded || state.LineageClosed || state.RootID == own.RootID {
			continue
		}
		for _, result := range currentRunResults(events, other.ID) {
			if result.Event.Actor != actor || result.Event.Outcome != "passed" {
				continue
			}
			at, err := time.Parse(time.RFC3339Nano, result.Event.Timestamp)
			if err != nil || now.Sub(at) >= window || now.Before(at.Add(-time.Minute)) {
				continue
			}
			return fmt.Sprintf("proposal %s on the same base passed at %s and merges first; waiting until it merges or until %s",
				shortID(candidate.ID), at.UTC().Format("15:04Z"), at.Add(window).UTC().Format("15:04Z"))
		}
	}
	return ""
}

// runnerDeadRequestReason returns why request can no longer produce a usable
// result, or "" when it should run. A result is unusable when the request's
// proposal is superseded or its lineage has already merged, since neither can
// merge again, or, when staleTip is set, when the proposal's base is not that
// tip, since a proposal merges only onto its signed base. A request whose
// proposal cannot be resolved runs, so the existing refusals report it.
func runnerDeadRequestReason(events []StoredEvent, lineage *lineageIndex, request *StoredEvent, staleTip string) string {
	proposal, err := resolveEvent(events, request.Event.Subject)
	if err != nil || !isProposalKind(proposal.Event.Kind) {
		return ""
	}
	if lineage != nil {
		if state, err := lineage.state(proposal.ID); err == nil {
			switch {
			case state.Merged || state.LineageClosed:
				return "its proposal lineage has already merged"
			case state.Superseded:
				successors := make([]string, 0, len(state.SuccessorIDs))
				for _, id := range state.SuccessorIDs {
					successors = append(successors, shortID(id))
				}
				return "its proposal is superseded by " + strings.Join(successors, ", ")
			}
		}
	}
	if staleTip != "" && proposal.Event.Base != staleTip {
		return fmt.Sprintf("its proposal base %s is not the current target tip %s", shortOID(proposal.Event.Base), shortOID(staleTip))
	}
	return ""
}

// runnerRemoteBranchTip reads branch's tip on remote, or "" when it cannot be
// read, which turns the stale-base check off for this cycle.
func runnerRemoteBranchTip(remote, branch string) string {
	ref := "refs/heads/" + branch
	output, err := gitOutput("ls-remote", "--refs", "--", remote, ref)
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(output))
	if len(fields) != 2 || fields[1] != ref || !validFullGitObjectID(fields[0]) {
		return ""
	}
	return fields[0]
}
