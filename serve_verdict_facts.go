package main

import (
	"bufio"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Rules for the Hubnot fact refs: actor chains, proposal code refs and
// memory streams. Each rule follows the step list of its section in
// kitty-specs/hn-serve-01M5P0NP/contracts/push-verdict.md and reports the
// first failing step. The checks reuse the client replication validators,
// so the server accepts exactly what a client would import. Inside the hook
// the hosted repository's GIT_DIR is both the quarantine and the accepted
// directory: Git's quarantine environment makes the pushed objects visible
// to every git call while no pushed ref is.

// serveHostedFactsSentence is the sentence of every fact and protected
// update in a push whose hosted facts could not be read.
const serveHostedFactsSentence = "hosted facts could not be verified; ask the administrator."

// serveDiagnosticLimit bounds the part of an internal error a sentence
// quotes.
const serveDiagnosticLimit = 240

// serveHostedFactsRejection fails a fact or protected update closed when
// the hosted facts cannot be loaded.
func serveHostedFactsRejection() *serveRejection {
	return &serveRejection{Code: serveReasonActorChainInvalid, Sentence: serveHostedFactsSentence}
}

// serveDiagnostic turns an internal error into one short line for a
// sentence, with the hosted repository path removed.
func serveDiagnostic(err error, gitDir string) string {
	message := redactReplicationDiagnostic(err.Error(), gitDir)
	message = strings.Join(strings.Fields(message), " ")
	if len(message) > serveDiagnosticLimit {
		message = message[:serveDiagnosticLimit] + "..."
	}
	return message
}

// serveCompactIDs drops empty values, so an absent head never renders as
// "?" in ids.
func serveCompactIDs(values ...string) []string {
	ids := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			ids = append(ids, value)
		}
	}
	return ids
}

// Facts known to the verdict: the hosted events plus the events accepted
// from actor refs earlier in the same push.

// hostedFactIndex returns the hosted events by ID, built once per hook run.
func (ctx *serveVerdictContext) hostedFactIndex() (map[string]StoredEvent, error) {
	hosted, err := ctx.HostedEvents()
	if err != nil {
		return nil, err
	}
	if ctx.hostedByID == nil {
		ctx.hostedByID = make(map[string]StoredEvent, len(hosted))
		for _, event := range hosted {
			ctx.hostedByID[event.ID] = event
		}
	}
	return ctx.hostedByID, nil
}

// lookupFact finds an event among the hosted facts and the facts accepted
// earlier in this push.
func (ctx *serveVerdictContext) lookupFact(id string) (StoredEvent, bool, error) {
	hosted, err := ctx.hostedFactIndex()
	if err != nil {
		return StoredEvent{}, false, err
	}
	if event, ok := hosted[id]; ok {
		return event, true, nil
	}
	for _, event := range ctx.PushFacts {
		if event.ID == id {
			return event, true, nil
		}
	}
	return StoredEvent{}, false, nil
}

// Hosted fact ceilings. Each push reads every hosted event with the
// batched reader of issue_read.go, which holds the events in memory and
// refuses to read past its limits. The ceilings below are those limits for
// the server, and the actor-ref rule refuses an append that would take the
// hosted facts past them, so a fact the server admits can never make the
// next push's read fail.

const (
	// serveHostedFactEventsCeiling is the client reader's event limit.
	serveHostedFactEventsCeiling = 100_000
	// serveHostedFactBytesCeiling is the client reader's aggregate byte
	// limit: event.json, signature and run log bytes of all hosted events.
	serveHostedFactBytesCeiling = 256 << 20

	serveHostedFactEventsBudget = "hosted-fact-events"
	serveHostedFactBytesBudget  = "hosted-fact-bytes"
)

// serveHostedFactCeilings bounds the hosted facts of one repository: how
// many events all hosted actor chains hold together and how many bytes the
// reader loads for them.
type serveHostedFactCeilings struct {
	Events, Bytes int64
}

func defaultServeHostedFactCeilings() serveHostedFactCeilings {
	return serveHostedFactCeilings{Events: serveHostedFactEventsCeiling, Bytes: serveHostedFactBytesCeiling}
}

// readLimits turns the ceilings into reader limits. A single object may
// use the whole byte ceiling, so every object the five budgets admit stays
// readable, whatever the budgets are or were when it was admitted; the
// memory the read uses stays bounded by the byte ceiling. Each hosted
// actor ref reaches at least its own events, so there are never more refs
// than events.
func (ceilings serveHostedFactCeilings) readLimits() issueReadLimits {
	return issueReadLimits{events: int(ceilings.Events), refs: int(ceilings.Events), object: ceilings.Bytes, total: ceilings.Bytes}
}

// serveFactUsage is what the hosted fact reader spends on a set of events:
// one event each, and the bytes of each event.json, signature and run log.
type serveFactUsage struct {
	Events, Bytes int64
}

func (usage serveFactUsage) plus(other serveFactUsage) serveFactUsage {
	return serveFactUsage{Events: usage.Events + other.Events, Bytes: usage.Bytes + other.Bytes}
}

// hostedFactUsage measures the hosted events the way the reader counts
// them. event.json and the run log are in memory already; the signature is
// stored base64-encoded and its blob may hold surrounding whitespace, so
// the batched object query gives the exact blob sizes.
func (ctx *serveVerdictContext) hostedFactUsage() (serveFactUsage, error) {
	hosted, err := ctx.HostedEvents()
	if err != nil {
		return serveFactUsage{}, err
	}
	objects, err := ctx.hostedEventObjects()
	if err != nil {
		return serveFactUsage{}, err
	}
	usage := serveFactUsage{Events: int64(len(hosted))}
	for _, event := range hosted {
		usage.Bytes += int64(len(event.Payload)+len(event.Attachments["log.txt"])) + objects[event.Commit].SignatureBytes
	}
	return usage, nil
}

// serveHostedEventObjects is what one hosted event commit reaches besides
// itself: the object IDs of its tree, event.json, signature and, for a run
// result, log.txt, and the stored size of its signature blob.
type serveHostedEventObjects struct {
	IDs            []string
	SignatureBytes int64
}

// hostedEventObjects returns the objects of every hosted event commit,
// read in one batched query at most once per hook run.
func (ctx *serveVerdictContext) hostedEventObjects() (map[string]serveHostedEventObjects, error) {
	if !ctx.hostedObjectsLoaded {
		ctx.hostedObjectsLoaded = true
		hosted, err := ctx.HostedEvents()
		if err != nil {
			ctx.hostedObjectsErr = err
		} else {
			ctx.hostedObjects, ctx.hostedObjectsErr = serveEventObjects(ctx.GitDir, hosted)
		}
	}
	return ctx.hostedObjects, ctx.hostedObjectsErr
}

// serveEventObjects reads the objects of each event commit with one
// cat-file batch. It uses the object IDs Git stored, never IDs derived from
// decoded content: a signature blob may hold whitespace the reader trims,
// and any of these blobs can recur later, for example as a run log.
func serveEventObjects(gitDir string, events []StoredEvent) (map[string]serveHostedEventObjects, error) {
	paths := []string{"^{tree}", ":event.json", ":signature", ":log.txt"}
	input := make([]string, 0, len(paths)*len(events))
	for _, event := range events {
		for _, path := range paths {
			input = append(input, event.Commit+path)
		}
	}
	answers, err := serveBatchCheck(gitDir, input)
	if err != nil {
		return nil, err
	}
	objects := make(map[string]serveHostedEventObjects, len(events))
	for index, event := range events {
		entry := serveHostedEventObjects{IDs: make([]string, 0, len(paths))}
		for offset, path := range paths {
			answer := answers[index*len(paths)+offset]
			if !answer.Found {
				if path == ":log.txt" {
					continue
				}
				return nil, fmt.Errorf("hosted %s%s is missing", event.Commit, path)
			}
			entry.IDs = append(entry.IDs, answer.ID)
			if path == ":signature" {
				entry.SignatureBytes = answer.Size
			}
		}
		objects[event.Commit] = entry
	}
	return objects, nil
}

// serveBatchAnswer is cat-file's answer for one requested object.
type serveBatchAnswer struct {
	ID    string
	Type  string
	Size  int64
	Found bool
}

// serveBatchCheck asks cat-file --batch-check about each object name in
// one process. A name that does not resolve is not Found.
func serveBatchCheck(gitDir string, names []string) ([]serveBatchAnswer, error) {
	answers := make([]serveBatchAnswer, len(names))
	if len(names) == 0 {
		return answers, nil
	}
	batch, err := gitInputAt(gitDir, []byte(strings.Join(names, "\n")+"\n"), "cat-file", "--batch-check=%(objectname) %(objecttype) %(objectsize)")
	if err != nil {
		return nil, fmt.Errorf("measure fact objects: %w", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(batch), "\n"), "\n")
	if len(lines) != len(names) {
		return nil, fmt.Errorf("measure fact objects: %d answers for %d names", len(lines), len(names))
	}
	for index, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			// "<name> missing" or "<name> ambiguous".
			continue
		}
		size, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil || size < 0 {
			return nil, fmt.Errorf("invalid object size %q", line)
		}
		answers[index] = serveBatchAnswer{ID: fields[0], Type: fields[1], Size: size, Found: true}
	}
	return answers, nil
}

// serveBlobSizes returns the size of each REV:PATH blob in one batched
// query, or -1 for a path that does not name a blob.
func serveBlobSizes(gitDir string, paths []string) ([]int64, error) {
	answers, err := serveBatchCheck(gitDir, paths)
	if err != nil {
		return nil, err
	}
	sizes := make([]int64, len(paths))
	for index, answer := range answers {
		sizes[index] = -1
		if answer.Found && answer.Type == "blob" {
			sizes[index] = answer.Size
		}
	}
	return sizes, nil
}

// serveAppendedFactUsage measures the appended commits the way the reader
// will count them once they are hosted. event.json and signature must be
// present; log.txt counts wherever it is, which chain validation later
// allows only for a run result.
func serveAppendedFactUsage(gitDir string, commits []string) (serveFactUsage, error) {
	paths := make([]string, 0, 3*len(commits))
	for _, commit := range commits {
		paths = append(paths, commit+":event.json", commit+":signature", commit+":log.txt")
	}
	sizes, err := serveBlobSizes(gitDir, paths)
	if err != nil {
		return serveFactUsage{}, err
	}
	usage := serveFactUsage{Events: int64(len(commits))}
	for index, size := range sizes {
		if size < 0 {
			if strings.HasSuffix(paths[index], ":log.txt") {
				continue
			}
			return serveFactUsage{}, fmt.Errorf("%s is not a blob", paths[index])
		}
		usage.Bytes += size
	}
	return usage, nil
}

// serveHostedCeilingRejection refuses an actor update that would take the
// hosted facts past a ceiling, events first. Limits are inclusive.
func serveHostedCeilingRejection(ref string, ceilings serveHostedFactCeilings, after serveFactUsage) *serveRejection {
	checks := []struct {
		name, unit         string
		limit, measurement int64
	}{
		{serveHostedFactEventsBudget, "events", ceilings.Events, after.Events},
		{serveHostedFactBytesBudget, "bytes", ceilings.Bytes, after.Bytes},
	}
	for _, check := range checks {
		if check.measurement > check.limit {
			return &serveRejection{
				Code:     serveReasonOverBudget,
				Budget:   check.name,
				Limit:    check.limit,
				Measured: check.measurement,
				Sentence: fmt.Sprintf("accepting %s would bring the hosted facts of this repository to %d %s and the server reads at most %d; ask the administrator.",
					ref, check.measurement, check.unit, check.limit),
			}
		}
	}
	return nil
}

// serveBudgetChecks lists the five budgets in the order the mapping reports
// them, with the names hn serve repo show prints.
func serveBudgetChecks(budgets ReplicationBudgets, measured ReplicationMeasurements) []struct {
	name               string
	limit, measurement int64
} {
	return []struct {
		name               string
		limit, measurement int64
	}{
		{"max-events", budgets.MaxEvents, measured.Events},
		{"max-objects", budgets.MaxObjects, measured.Objects},
		{"max-object-bytes", budgets.MaxObjectBytes, measured.LargestObjectBytes},
		{"max-attachment-bytes", budgets.MaxAttachmentBytes, measured.LargestAttachmentBytes},
		{"max-total-bytes", budgets.MaxTotalBytes, measured.TotalBytes},
	}
}

// serveOverBudgetRejection maps the measurements that came back with a
// replicationBudgetExceeded to one over-budget rejection: the first
// exceeded limit in the order events, objects, largest object, largest
// attachment, total bytes. Limits are inclusive, so only a measurement
// above its limit counts.
func serveOverBudgetRejection(ref string, budgets ReplicationBudgets, measured ReplicationMeasurements) *serveRejection {
	for _, check := range serveBudgetChecks(budgets, measured) {
		if check.measurement > check.limit {
			return &serveRejection{
				Code:     serveReasonOverBudget,
				Budget:   check.name,
				Limit:    check.limit,
				Measured: check.measurement,
				Sentence: fmt.Sprintf("%s measures %d for %s and this repository allows at most %d; split the history or ask the administrator to raise it with hn serve repo set.",
					ref, check.measurement, check.name, check.limit),
			}
		}
	}
	// replicationBudgetExceeded always comes with an exceeded measurement;
	// this keeps the rejection fail-closed if that ever changes.
	return &serveRejection{Code: serveReasonOverBudget, Sentence: ref + " exceeds a budget of this repository."}
}

// Actor chain refs/hn/actors/<A> (FR-007, C-007).

// checkServeActorUpdate runs the contract's actor chain steps in order:
// rewrite, ownership, budgets, chain validity, dependencies and the
// combined projection. On success the appended events join the push's
// facts for the checks that follow.
func checkServeActorUpdate(ctx *serveVerdictContext, update serveRefUpdate) *serveRejection {
	actor := strings.TrimPrefix(update.Ref, serveActorRefPrefix)

	// Step 1: actor history is append-only.
	if update.Deletes() {
		return &serveRejection{
			Code:     serveReasonActorChainRewrite,
			IDs:      []string{update.Old},
			Sentence: update.Ref + " cannot be deleted; actor histories on this server are append-only.",
		}
	}
	if !update.Creates() && !serveIsAncestor(ctx.GitDir, update.Old, update.New) {
		return &serveRejection{
			Code:     serveReasonActorChainRewrite,
			IDs:      []string{update.Old, update.New},
			Sentence: update.Ref + " is at " + update.Old + " and " + update.New + " does not descend from it; actor histories on this server are append-only.",
		}
	}

	// Step 2: only the mapped actor appends to its own chain.
	if actor != ctx.Actor {
		return &serveRejection{
			Code:     serveReasonActorKeyMismatch,
			Next:     "push only refs/hn/actors/" + ctx.Actor,
			Sentence: update.Ref + " belongs to actor " + actor + " and this key is bound to actor " + ctx.Actor + ".",
		}
	}

	hosted, err := ctx.HostedEvents()
	if err != nil {
		return serveHostedFactsRejection()
	}
	oldHead := ""
	if !update.Creates() {
		oldHead = update.Old
	}
	prefix := serveHostedActorPrefix(hosted, actor)
	if (oldHead == "" && len(prefix) != 0) || (oldHead != "" && (len(prefix) == 0 || prefix[len(prefix)-1].Commit != oldHead)) {
		return &serveRejection{
			Code:     serveReasonActorChainInvalid,
			IDs:      serveCompactIDs(oldHead),
			Sentence: "the hosted history of actor " + actor + " does not end at " + update.Ref + "; ask the administrator.",
		}
	}

	// Step 3: budgets, measured over what this push adds, then the
	// repository-wide ceilings of the hosted fact read.
	budgets := ctx.Config.Budgets
	hostedObjects, err := ctx.hostedEventObjects()
	if err != nil {
		return serveHostedFactsRejection()
	}
	measured, appendedCommits, err := measureServeActorRange(ctx.GitDir, oldHead, update.New, prefix, hostedObjects, budgets, update.Ref)
	if err != nil {
		var exceeded replicationBudgetExceeded
		if errors.As(err, &exceeded) {
			return serveOverBudgetRejection(update.Ref, budgets, measured)
		}
		return serveActorChainInvalidRejection(ctx, update, actor, oldHead, prefix, err)
	}
	appendedUsage, err := serveAppendedFactUsage(ctx.GitDir, appendedCommits)
	if err != nil {
		return serveActorChainInvalidRejection(ctx, update, actor, oldHead, prefix, err)
	}
	hostedUsage, err := ctx.hostedFactUsage()
	if err != nil {
		return serveHostedFactsRejection()
	}
	if rejection := serveHostedCeilingRejection(update.Ref, ctx.HostedCeilings, hostedUsage.plus(ctx.pushFactUsage).plus(appendedUsage)); rejection != nil {
		return rejection
	}

	// Step 4: signature, actor, sequence, previous ID, parent and tree.
	joined, err := validateQuarantinedActorRange(ctx.GitDir, actor, oldHead, update.New, prefix)
	if err != nil {
		return serveActorChainInvalidRejection(ctx, update, actor, oldHead, prefix, err)
	}
	appended := joined[len(prefix):]

	// Step 5: every reference resolves against hosted facts plus the push.
	if rejection := checkServeActorDependencies(ctx, hosted, appended); rejection != nil {
		return rejection
	}

	// Step 6: the combined projection.
	combined := make([]StoredEvent, 0, len(hosted)+len(ctx.PushFacts)+len(appended))
	combined = append(append(append(combined, hosted...), ctx.PushFacts...), appended...)
	if err := serveValidateProjection(combined); err != nil {
		var ids []string
		if len(appended) > 0 {
			ids = []string{appended[len(appended)-1].ID}
		}
		return &serveRejection{
			Code:     serveReasonActorChainInvalid,
			IDs:      ids,
			Sentence: update.Ref + " does not fit the hosted facts: " + serveDiagnostic(err, ctx.GitDir),
		}
	}

	// Step 7: later checks of this push see the appended events, and later
	// actor updates count them against the hosted fact ceilings.
	ctx.PushFacts = append(ctx.PushFacts, appended...)
	ctx.pushFactUsage = ctx.pushFactUsage.plus(appendedUsage)
	return nil
}

// serveHostedActorPrefix returns the hosted events of actor in sequence
// order. They come from the batched hosted read; nothing here reads the
// hosted chain commit by commit (NFR-001).
func serveHostedActorPrefix(hosted []StoredEvent, actor string) []StoredEvent {
	var prefix []StoredEvent
	for _, event := range hosted {
		if event.Event.Actor == actor {
			prefix = append(prefix, event)
		}
	}
	sort.SliceStable(prefix, func(i, j int) bool { return prefix[i].Event.Sequence < prefix[j].Event.Sequence })
	return prefix
}

func serveValidateProjection(events []StoredEvent) error {
	if err := validateExactEventReferenceClosure(events); err != nil {
		return err
	}
	if _, err := validateIssueRelationships(events); err != nil {
		return err
	}
	_, err := ProjectIdentityContinuity(events)
	return err
}

// checkServeActorDependencies resolves each appended event's references
// with the client import check. Code for a proposal event may be absent:
// it can arrive in a later push, and only matching hosted code can ever
// advance a protected branch.
func checkServeActorDependencies(ctx *serveVerdictContext, hosted, appended []StoredEvent) *serveRejection {
	byID := make(map[string]StoredEvent, len(hosted)+len(ctx.PushFacts)+len(appended))
	for _, group := range [][]StoredEvent{hosted, ctx.PushFacts, appended} {
		for _, event := range group {
			byID[event.ID] = event
		}
	}
	codeHeads, _, err := acceptedProposalHeads()
	if err != nil {
		return serveHostedFactsRejection()
	}
	for _, other := range ctx.PushUpdates {
		// A push that moves or deletes a hosted proposal ref is rejected by
		// the proposal rule; until then the hosted value stays authoritative.
		if other.Class == serveRefProposal && other.Creates() {
			codeHeads["sha256:"+strings.TrimPrefix(other.Ref, serveProposalRefPrefix)] = other.New
		}
	}
	options := replicationDependencyOptions{AllowAbsentProposalCode: true}
	for _, event := range appended {
		dependency, err := replicationEventDependencyWithOptions(ctx.GitDir, ctx.GitDir, event, byID, codeHeads,
			map[string]string{}, map[string]string{}, options)
		if err != nil {
			return &serveRejection{
				Code:     serveReasonActorChainInvalid,
				IDs:      []string{event.ID},
				Sentence: "event " + event.ID + " (" + event.Event.Kind + ") has an invalid relationship: " + serveDiagnostic(err, ctx.GitDir),
			}
		}
		if dependency != nil {
			return &serveRejection{
				Code:     serveReasonDependencyMissing,
				IDs:      []string{dependency.Missing},
				Sentence: "event " + event.ID + " (" + event.Event.Kind + ") needs " + dependency.Missing + ", which is not hosted and not in this push.",
			}
		}
	}
	return nil
}

// serveActorChainInvalidRejection names the first failing appended event,
// by its full event ID when event.json is readable, even with a broken
// signature (US1-4), and by its commit otherwise. The diagnostic walk runs
// only after a check has failed, so an accepted push pays for one
// validation pass.
func serveActorChainInvalidRejection(ctx *serveVerdictContext, update serveRefUpdate, actor, oldHead string, prefix []StoredEvent, cause error) *serveRejection {
	failure, found := serveFirstFailingActorEvent(ctx.GitDir, actor, oldHead, update.New, prefix)
	if !found {
		failure = serveActorFailure{ID: serveActorEventName(ctx.GitDir, update.New), Reason: serveDiagnostic(cause, ctx.GitDir)}
	}
	return &serveRejection{
		Code:     serveReasonActorChainInvalid,
		IDs:      []string{failure.ID},
		Sentence: update.Ref + ": " + failure.ID + " fails validation: " + failure.Reason,
	}
}

// serveActorFailure is the first appended commit that fails, by full event
// ID or commit, and why.
type serveActorFailure struct {
	ID, Reason string
}

// serveActorEventName returns the full event ID of commit's event.json, or
// the commit itself when event.json cannot be read.
func serveActorEventName(gitDir, commit string) string {
	payload, err := gitOutputAt(gitDir, "show", commit+":event.json")
	if err != nil {
		return commit
	}
	return eventID(payload)
}

// serveFirstFailingActorEvent walks oldHead..newHead in order with the same
// per-commit checks validateQuarantinedActorRange applies and returns the
// first commit that fails one of them.
func serveFirstFailingActorEvent(gitDir, actor, oldHead, newHead string, prefix []StoredEvent) (serveActorFailure, bool) {
	args := []string{"rev-list", "--reverse", newHead}
	if oldHead != "" {
		args = append(args, "--not", oldHead)
	}
	listed, err := gitTextAt(gitDir, args...)
	if err != nil {
		return serveActorFailure{ID: newHead, Reason: "the pushed history cannot be listed"}, true
	}
	previousCommit, previousID, previousSequence := oldHead, "", uint64(0)
	if len(prefix) > 0 {
		last := prefix[len(prefix)-1]
		previousID, previousSequence = last.ID, last.Event.Sequence
	}
	for _, commit := range strings.Fields(listed) {
		payload, err := gitOutputAt(gitDir, "show", commit+":event.json")
		if err != nil {
			return serveActorFailure{ID: commit, Reason: "the commit has no readable event.json."}, true
		}
		id := eventID(payload)
		stored, err := loadStoredEventAt(gitDir, commit)
		if err != nil {
			return serveActorFailure{ID: id, Reason: "the event does not decode or its signature does not verify."}, true
		}
		if stored.Event.Actor != actor {
			return serveActorFailure{ID: id, Reason: "the event is signed by actor " + stored.Event.Actor + " and the ref belongs to actor " + actor + "."}, true
		}
		if err := validateQuarantinedEventTree(gitDir, *stored); err != nil {
			return serveActorFailure{ID: id, Reason: "the event commit must hold exactly event.json, signature and, for a run result, log.txt."}, true
		}
		parents, err := gitTextAt(gitDir, "show", "-s", "--format=%P", commit)
		if err != nil || parents != previousCommit {
			return serveActorFailure{ID: id, Reason: "the event commit's parent is not the previous event's commit."}, true
		}
		if stored.Event.Sequence != previousSequence+1 || stored.Event.Previous != previousID {
			return serveActorFailure{ID: id, Reason: "the event's sequence or previous event ID does not continue the chain."}, true
		}
		previousCommit, previousID, previousSequence = commit, stored.ID, stored.Event.Sequence
	}
	return serveActorFailure{}, false
}

// measureServeActorRangeUnderBudgets measures one actor ref of a push the
// way measureQuarantinedSelectionUnderBudgets measures a selected actor
// chain whose previous accepted value is oldHead, with the same totals and
// the same inclusive limits, without reading the hosted chain again. The
// hosted prefix, already loaded in one batch, supplies its event count,
// its objects and its attachments; only oldHead..newHead is walked. It
// measures sizes and counts and verifies no signature.
//
// The objects counted are those reachable from newHead and not from
// oldHead. git rev-list --not excludes every hosted commit but only the
// trees and blobs of oldHead itself, so every object an earlier hosted
// event reaches is discounted by its stored object ID: its tree, event.json,
// signature and run log. Any of those blobs can recur in the push, for
// example a later run log with the bytes of an earlier signature blob,
// whitespace included. The IDs come from one batched query over the prefix
// (serveEventObjects), which the verdict context reads once per hook run.
//
// The client helper stops at the first limit a running total crosses. The
// server must name the first exceeded budget in the order events, objects,
// largest object, largest attachment, total bytes, so this measurement
// stops early only on events and objects, which come first in that order,
// and otherwise measures every byte dimension before it checks the
// budgets. That work stays bounded: it sizes at most max-objects objects
// and at most max-events attachments without reading their contents.
func measureServeActorRangeUnderBudgets(gitDir, oldHead, newHead string, hostedPrefix []StoredEvent, budgets ReplicationBudgets, selectionID string) (ReplicationMeasurements, error) {
	hostedObjects, err := serveEventObjects(gitDir, hostedPrefix)
	if err != nil {
		return ReplicationMeasurements{}, err
	}
	measurements, _, err := measureServeActorRange(gitDir, oldHead, newHead, hostedPrefix, hostedObjects, budgets, selectionID)
	return measurements, err
}

// measureServeActorRange is measureServeActorRangeUnderBudgets with the
// objects of the hosted events already read, keyed by commit. It also
// returns the appended commits in chain order.
func measureServeActorRange(gitDir, oldHead, newHead string, hostedPrefix []StoredEvent, hostedObjects map[string]serveHostedEventObjects, budgets ReplicationBudgets, selectionID string) (ReplicationMeasurements, []string, error) {
	if objectType, err := gitTextAt(gitDir, "cat-file", "-t", newHead); err != nil || objectType != "commit" {
		return ReplicationMeasurements{}, nil, fmt.Errorf("selected root %s is not a commit", newHead)
	}
	revisions := []string{newHead}
	if oldHead != "" {
		revisions = append(revisions, "--not", oldHead)
	}
	listedCommits, err := gitTextAt(gitDir, append([]string{"rev-list", "--reverse"}, revisions...)...)
	if err != nil {
		return ReplicationMeasurements{}, nil, fmt.Errorf("count selected actor events: %w", err)
	}
	appended := strings.Fields(listedCommits)
	measurements := ReplicationMeasurements{Events: int64(len(hostedPrefix) + len(appended))}
	if measurements.Events < 1 {
		return measurements, nil, fmt.Errorf("invalid selected actor event count %d", measurements.Events)
	}
	if err := enforceReplicationBudgets(selectionID, budgets, measurements); err != nil {
		return measurements, appended, replicationBudgetExceeded{err}
	}

	listed, err := gitTextAt(gitDir, append([]string{"rev-list", "--objects"}, revisions...)...)
	if err != nil {
		return ReplicationMeasurements{}, nil, fmt.Errorf("enumerate selected graph: %w", err)
	}
	previouslyAccepted := make(map[string]bool, 5*len(hostedPrefix))
	for _, event := range hostedPrefix {
		reached, ok := hostedObjects[event.Commit]
		if !ok {
			return ReplicationMeasurements{}, nil, fmt.Errorf("the objects of hosted event %s were not read", event.ID)
		}
		previouslyAccepted[event.Commit] = true
		for _, objectID := range reached.IDs {
			previouslyAccepted[objectID] = true
		}
	}
	objects := make([]string, 0)
	seen := make(map[string]bool)
	for _, line := range strings.Split(listed, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || seen[fields[0]] {
			continue
		}
		seen[fields[0]] = true
		if previouslyAccepted[fields[0]] {
			continue
		}
		objects = append(objects, fields[0])
	}
	measurements.Objects = int64(len(objects))
	if err := enforceReplicationBudgets(selectionID, budgets, measurements); err != nil {
		return measurements, appended, replicationBudgetExceeded{err}
	}
	if len(objects) > 0 {
		batch, err := gitInputAt(gitDir, []byte(strings.Join(objects, "\n")+"\n"),
			"cat-file", "--batch-check=%(objectname) %(objecttype) %(objectsize)")
		if err != nil {
			return ReplicationMeasurements{}, nil, fmt.Errorf("measure selected graph: %w", err)
		}
		scanner := bufio.NewScanner(strings.NewReader(string(batch)))
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) != 3 || fields[1] == "missing" {
				return ReplicationMeasurements{}, nil, fmt.Errorf("unreadable reachable object %q", scanner.Text())
			}
			size, err := strconv.ParseInt(fields[2], 10, 64)
			if err != nil || size < 0 {
				return ReplicationMeasurements{}, nil, fmt.Errorf("invalid object size for %s", fields[0])
			}
			measurements.TotalBytes += size
			if size > measurements.LargestObjectBytes {
				measurements.LargestObjectBytes = size
			}
		}
		if err := scanner.Err(); err != nil {
			return ReplicationMeasurements{}, nil, err
		}
	}

	// Attachments: the hosted prefix's, then each appended commit's
	// log.txt, the only attachment an actor event can carry.
	for _, event := range hostedPrefix {
		for _, attachment := range event.Attachments {
			measurements.LargestAttachmentBytes = max(measurements.LargestAttachmentBytes, int64(len(attachment)))
		}
	}
	logs := make([]string, 0, len(appended))
	for _, commit := range appended {
		logs = append(logs, commit+":log.txt")
	}
	sizes, err := serveBlobSizes(gitDir, logs)
	if err != nil {
		return ReplicationMeasurements{}, nil, fmt.Errorf("measure appended attachments: %w", err)
	}
	for _, size := range sizes {
		measurements.LargestAttachmentBytes = max(measurements.LargestAttachmentBytes, size)
	}

	if err := enforceReplicationBudgets(selectionID, budgets, measurements); err != nil {
		return measurements, appended, replicationBudgetExceeded{err}
	}
	return measurements, appended, nil
}

// measureServeSelectionUnderBudgets measures a proposal or memory ref with
// the client helper, unchanged, and reports the first exceeded budget in
// the server's order. The helper stops at the first running total that
// crosses a limit, so it runs with the byte limits lifted and the event
// and object limits kept: those two come first in the order, and the
// object limit bounds how many sizes the helper reads. Proposal and memory
// selections carry no attachments, so the helper never loads contents.
func measureServeSelectionUnderBudgets(gitDir, kind, root, acceptedRoot string, budgets ReplicationBudgets, selectionID string) (ReplicationMeasurements, error) {
	counting := budgets
	counting.MaxObjectBytes, counting.MaxAttachmentBytes, counting.MaxTotalBytes = math.MaxInt64, math.MaxInt64, math.MaxInt64
	measurements, err := measureQuarantinedSelectionUnderBudgets(gitDir, gitDir, kind, root, acceptedRoot, counting, selectionID)
	if err != nil {
		return measurements, err
	}
	if err := enforceReplicationBudgets(selectionID, budgets, measurements); err != nil {
		return measurements, replicationBudgetExceeded{err}
	}
	return measurements, nil
}

// Proposal code ref refs/hn/proposals/<id> (FR-010).

// checkServeProposalUpdate: a proposal ref is created once, equal to its
// candidate's signed head and descending from the candidate's base, and
// never moved or deleted.
func checkServeProposalUpdate(ctx *serveVerdictContext, update serveRefUpdate) *serveRejection {
	// Step 1: immutable once hosted.
	if !update.Creates() {
		sentence := update.Ref + " already points at " + update.Old + "; proposal code refs on this server cannot be moved."
		if update.Deletes() {
			sentence = update.Ref + " cannot be deleted; proposal code refs on this server are immutable."
		}
		return &serveRejection{
			Code:     serveReasonProposalRefImmutable,
			IDs:      serveCompactIDs(update.Old, serveNonZero(update.New)),
			Sentence: sentence,
		}
	}

	// Step 2: the candidate is a hosted fact or arrived earlier in the push.
	candidateID := "sha256:" + strings.TrimPrefix(update.Ref, serveProposalRefPrefix)
	candidate, found, err := ctx.lookupFact(candidateID)
	if err != nil {
		return serveHostedFactsRejection()
	}
	if !found {
		return &serveRejection{
			Code:     serveReasonDependencyMissing,
			IDs:      []string{candidateID},
			Sentence: "candidate " + candidateID + " is not hosted and not in this push; push its actor history first.",
		}
	}

	// Step 3: the ref is the candidate's signed head.
	if !isProposalKind(candidate.Event.Kind) || candidate.Event.Head != update.New {
		sentence := update.Ref + " points at " + update.New + " but candidate " + candidateID + " signs head " + candidate.Event.Head + "."
		if !isProposalKind(candidate.Event.Kind) {
			sentence = candidateID + " is not a proposal; its kind is " + candidate.Event.Kind + "."
		}
		return &serveRejection{
			Code:     serveReasonProposalRefMismatch,
			IDs:      serveCompactIDs(candidateID, candidate.Event.Head, update.New),
			Sentence: sentence,
		}
	}

	// Step 4: the head descends from the signed base, which must exist.
	if !serveIsAncestor(ctx.GitDir, candidate.Event.Base, update.New) {
		return &serveRejection{
			Code:     serveReasonProposalRefMismatch,
			IDs:      serveCompactIDs(candidateID, candidate.Event.Base, update.New),
			Sentence: update.Ref + " points at " + update.New + ", which does not descend from the candidate's base " + candidate.Event.Base + ".",
		}
	}

	// Step 5: the candidate's whole code graph, as client replication
	// measures a new proposal selection.
	budgets := ctx.Config.Budgets
	measured, err := measureServeSelectionUnderBudgets(ctx.GitDir, replicationProposal, update.New, "", budgets, update.Ref)
	if err != nil {
		var exceeded replicationBudgetExceeded
		if errors.As(err, &exceeded) {
			return serveOverBudgetRejection(update.Ref, budgets, measured)
		}
		return &serveRejection{
			Code:     serveReasonDependencyMissing,
			IDs:      []string{update.New},
			Sentence: "the code graph of " + update.New + " cannot be read: " + serveDiagnostic(err, ctx.GitDir),
		}
	}
	return nil
}

// serveNonZero returns oid, or "" for the all-zero object ID.
func serveNonZero(oid string) string {
	if serveZeroOID(oid) {
		return ""
	}
	return oid
}

// Memory stream refs/hn/memory/<A>/<stream> (FR-010).

// checkServeMemoryUpdate: only the stream's owner appends, never rewrites,
// and the result must be a valid hn-memory/0 stream.
func checkServeMemoryUpdate(ctx *serveVerdictContext, update serveRefUpdate) *serveRejection {
	actor, stream, ok := parseMemoryRef(update.Ref)
	if !ok {
		// classifyServeRef only puts parseable refs in this class.
		return &serveRejection{Code: serveReasonMemoryInvalid, Sentence: update.Ref + " is not a memory stream ref."}
	}

	// Step 1: append-only.
	if update.Deletes() {
		return &serveRejection{
			Code:     serveReasonMemoryInvalid,
			IDs:      []string{update.Old},
			Sentence: update.Ref + " cannot be deleted; memory streams on this server are append-only.",
		}
	}
	if !update.Creates() && !serveIsAncestor(ctx.GitDir, update.Old, update.New) {
		return &serveRejection{
			Code:     serveReasonMemoryInvalid,
			IDs:      []string{update.Old, update.New},
			Sentence: update.Ref + " is at " + update.Old + " and " + update.New + " does not descend from it; memory streams on this server are append-only.",
		}
	}

	// Step 2: only the owner pushes the stream.
	if actor != ctx.Actor {
		return &serveRejection{
			Code:     serveReasonMemoryKeyMismatch,
			Sentence: update.Ref + " belongs to actor " + actor + " and this key is bound to actor " + ctx.Actor + ".",
		}
	}

	// Step 3: budgets.
	oldHead := ""
	if !update.Creates() {
		oldHead = update.Old
	}
	budgets := ctx.Config.Budgets
	measured, err := measureServeSelectionUnderBudgets(ctx.GitDir, replicationMemory, update.New, oldHead, budgets, update.Ref)
	if err != nil {
		var exceeded replicationBudgetExceeded
		if errors.As(err, &exceeded) {
			return serveOverBudgetRejection(update.Ref, budgets, measured)
		}
		return &serveRejection{
			Code:     serveReasonMemoryInvalid,
			IDs:      []string{stream},
			Sentence: "memory stream " + stream + " cannot be measured: " + serveDiagnostic(err, ctx.GitDir),
		}
	}

	// Step 4: hn-memory/0 validation, as client replication loads a stream.
	if _, err := loadMemoryStreamAt(ctx.GitDir, memoryStreamSource{Ref: update.Ref, Actor: actor, Stream: stream, Head: update.New}); err != nil {
		return &serveRejection{
			Code:     serveReasonMemoryInvalid,
			IDs:      []string{stream},
			Sentence: "memory stream " + stream + " fails hn-memory/0 validation: " + serveDiagnostic(err, ctx.GitDir),
		}
	}
	return nil
}
