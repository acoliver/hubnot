package main

import (
	"os"
	"os/exec"
	"slices"
	"sort"
	"strings"
)

// The push verdict engine. pre-receive hands evaluateServePush the push's
// ref updates, the mapped actor, the push options and the repository
// configuration, and gets back either no rejections (accept every ref) or
// one rejection per failing ref update, sorted by ref. Git then rejects
// every ref of a push with any rejection and discards its quarantined
// objects (FR-012). The rules are in
// kitty-specs/hn-serve-01M5P0NP/contracts/push-verdict.md.

// serveRefClass is the class a ref update is checked under.
type serveRefClass string

const (
	serveRefProtected serveRefClass = "protected"
	serveRefActor     serveRefClass = "actor"
	serveRefProposal  serveRefClass = "proposal"
	serveRefMemory    serveRefClass = "memory"
	serveRefTag       serveRefClass = "tag"
	serveRefBranch    serveRefClass = "branch"
	serveRefReserved  serveRefClass = "reserved"
)

// serveEvaluationOrder is the order classes are checked in. Later classes
// may rely on facts accepted from earlier classes of the same push: a
// proposal ref may name a candidate that arrives in an actor ref, and a
// protected update may rely on both.
var serveEvaluationOrder = []serveRefClass{
	serveRefActor,
	serveRefMemory,
	serveRefProposal,
	serveRefTag,
	serveRefBranch,
	serveRefReserved,
	serveRefProtected,
}

const (
	serveActorRefPrefix    = "refs/hn/actors/"
	serveProposalRefPrefix = "refs/hn/proposals/"
	serveTagRefPrefix      = "refs/tags/"
)

// serveRefUpdate is one OLD NEW REF line of a hook's standard input. Old
// is all zeros for a creation and New is all zeros for a deletion.
type serveRefUpdate struct {
	Old, New, Ref string
	Class         serveRefClass
}

func (update serveRefUpdate) Creates() bool { return serveZeroOID(update.Old) }

func (update serveRefUpdate) Deletes() bool { return serveZeroOID(update.New) }

func serveZeroOID(oid string) bool {
	return oid != "" && strings.Trim(oid, "0") == ""
}

// servePushOptions holds the two push options the server accepts, each
// empty when absent.
type servePushOptions struct {
	Advance, ExpectedTip string
}

// classifyServeRef returns the class of ref, checking the classes in the
// contract's order. Every refs/heads/ name that git check-ref-format
// accepts is a branch, and validServeGitBranchRef is also what branches.json
// requires, so every branch the verdict accepts gets an owner record. Git
// never passes a ref name that fails check-ref-format to the hooks; such a
// name would be reserved.
func classifyServeRef(config serveRepoConfig, ref string) serveRefClass {
	if slices.Contains(config.Protected, ref) {
		return serveRefProtected
	}
	if id, ok := strings.CutPrefix(ref, serveActorRefPrefix); ok {
		if serveLowerHex64(id) {
			return serveRefActor
		}
		return serveRefReserved
	}
	if id, ok := strings.CutPrefix(ref, serveProposalRefPrefix); ok {
		if serveLowerHex64(id) {
			return serveRefProposal
		}
		return serveRefReserved
	}
	if _, _, ok := parseMemoryRef(ref); ok {
		return serveRefMemory
	}
	if name, ok := strings.CutPrefix(ref, serveTagRefPrefix); ok && name != "" {
		return serveRefTag
	}
	if validServeGitBranchRef(ref) {
		return serveRefBranch
	}
	return serveRefReserved
}

// serveLowerHex64 accepts exactly 64 lowercase hex digits, the shape of a
// full actor fingerprint and of a proposal ref's event digest.
func serveLowerHex64(value string) bool {
	return validActorFingerprint(value)
}

func classifyServeRefUpdates(config serveRepoConfig, updates []serveRefUpdate) {
	for index := range updates {
		updates[index].Class = classifyServeRef(config, updates[index].Ref)
	}
}

// serveVerdictContext is everything the rules read besides the ref
// updates. It is built once per hook run.
type serveVerdictContext struct {
	GitDir string
	Actor  string
	Key    string
	Config serveRepoConfig
	Owners serveBranchOwners

	// PushFacts holds facts accepted from earlier classes of this push.
	// The actor-ref rule appends the events of each accepted actor update;
	// proposal and protected checks read it next to HostedEvents. Events
	// of a rejected actor update never enter it.
	PushFacts []StoredEvent

	// PushUpdates is every ref update of the push, set by
	// evaluateServePush. The actor-ref rule reads the proposal refs created
	// in the same push.
	PushUpdates []serveRefUpdate

	// HostedCeilings bounds the batched read of the hosted facts. The
	// actor-ref rule refuses any append that would take the hosted facts
	// past it, so every accepted push stays readable by the next one.
	HostedCeilings serveHostedFactCeilings

	loadHostedEvents    func() ([]StoredEvent, error)
	hostedLoaded        bool
	hostedEvents        []StoredEvent
	hostedErr           error
	hostedByID          map[string]StoredEvent
	hostedObjectsLoaded bool
	hostedObjects       map[string]serveHostedEventObjects
	hostedObjectsErr    error
	pushFactUsage       serveFactUsage
	maintainersLoaded   bool
	maintainers         []string
}

func newServeVerdictContext(gitDir, actor, key string, config serveRepoConfig, owners serveBranchOwners) *serveVerdictContext {
	ctx := &serveVerdictContext{
		GitDir:         gitDir,
		Actor:          actor,
		Key:            key,
		Config:         config,
		Owners:         owners,
		HostedCeilings: defaultServeHostedFactCeilings(),
	}
	ctx.loadHostedEvents = func() ([]StoredEvent, error) {
		// The hook runs with GIT_DIR naming the hosted repository and the
		// quarantine variables set, which collectIssueEventsWithLimits uses.
		events, _, err := collectIssueEventsWithLimits(ctx.HostedCeilings.readLimits())
		return events, err
	}
	return ctx
}

// HostedEvents returns every verified event reachable from the hosted
// refs/hn/actors/* refs, loading them at most once per hook run. The actor,
// proposal and memory rules and WP09's protected rule are its callers;
// ordinary branches, tags and reserved refs never need it.
func (ctx *serveVerdictContext) HostedEvents() ([]StoredEvent, error) {
	if !ctx.hostedLoaded {
		ctx.hostedLoaded = true
		ctx.hostedEvents, ctx.hostedErr = ctx.loadHostedEvents()
	}
	return ctx.hostedEvents, ctx.hostedErr
}

// IsMaintainer reports whether actor is listed in maintainers of the policy
// at the tip of the default protected branch. The set is read once per
// hook run. A missing tip or a missing or invalid policy yields an empty
// set, so no actor counts as a maintainer.
func (ctx *serveVerdictContext) IsMaintainer(actor string) bool {
	if !ctx.maintainersLoaded {
		ctx.maintainersLoaded = true
		ctx.maintainers = loadServeMaintainers(ctx.GitDir, ctx.Config.Protected[0])
	}
	return validActorFingerprint(actor) && actorListed(actor, ctx.maintainers)
}

// loadServeMaintainers returns maintainers of the policy at the tip of
// defaultRef, or nil when there is no tip or no valid policy there.
func loadServeMaintainers(gitDir, defaultRef string) []string {
	tip, err := gitTextAt(gitDir, "rev-parse", "--verify", "--quiet", defaultRef+"^{commit}")
	if err != nil || !validGitOID(tip) {
		return nil
	}
	policy, _, err := loadReplicationPolicyAt(gitDir, gitDir, tip)
	if err != nil {
		return nil
	}
	return policy.Maintainers
}

// evaluateServePush returns the rejections for a push, sorted by ref in the
// order the rejection lines are printed. No rejections means every ref
// update passes. Every rejection carries the mapped actor (FR-006).
func evaluateServePush(ctx *serveVerdictContext, updates []serveRefUpdate, options servePushOptions) []serveRejection {
	if options.Advance != "" && (len(updates) != 1 || updates[0].Class != serveRefProtected) {
		return rejectServeEveryUpdate(ctx, updates, serveReasonUnsupportedCommand,
			"hn-advance requires a push with exactly one ref update, to a protected branch.")
	}
	ordered := slices.Clone(updates)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Ref < ordered[j].Ref })
	ctx.PushUpdates = ordered
	hostedFailed := serveHostedFactsUnavailable(ctx, ordered)
	var rejections []serveRejection
	for _, class := range serveEvaluationOrder {
		for _, update := range ordered {
			if update.Class != class {
				continue
			}
			var rejection *serveRejection
			if hostedFailed && serveClassReadsFacts(class) {
				rejection = serveHostedFactsRejection()
			} else {
				rejection = checkServeRefUpdate(ctx, update, options)
			}
			if rejection == nil {
				continue
			}
			rejection.Ref = update.Ref
			rejection.Actor = ctx.Actor
			if rejection.Next == "" {
				rejection.Next = serveReasonDefaultNext(rejection.Code)
			}
			rejections = append(rejections, *rejection)
		}
	}
	sortServeRejections(rejections)
	return rejections
}

// serveClassReadsFacts reports whether a class's rule depends on the hosted
// facts: the three Hubnot fact classes and protected branches.
func serveClassReadsFacts(class serveRefClass) bool {
	switch class {
	case serveRefActor, serveRefProposal, serveRefMemory, serveRefProtected:
		return true
	}
	return false
}

// serveHostedFactsUnavailable loads the hosted facts when the push has an
// update that needs them and reports whether that load failed. A failure
// fails every such update closed with actor-chain-invalid; tags, ordinary
// branches and reserved refs are still checked and never load the facts.
func serveHostedFactsUnavailable(ctx *serveVerdictContext, updates []serveRefUpdate) bool {
	for _, update := range updates {
		if serveClassReadsFacts(update.Class) {
			_, err := ctx.HostedEvents()
			return err != nil
		}
	}
	return false
}

// rejectServeEveryUpdate rejects each ref update with the same code and
// sentence, for failures that belong to the push as a whole.
func rejectServeEveryUpdate(ctx *serveVerdictContext, updates []serveRefUpdate, code, sentence string) []serveRejection {
	rejections := make([]serveRejection, 0, len(updates))
	for _, update := range updates {
		rejections = append(rejections, serveRejection{
			Ref:      update.Ref,
			Actor:    ctx.Actor,
			Code:     code,
			Next:     serveReasonDefaultNext(code),
			Sentence: sentence,
		})
	}
	sortServeRejections(rejections)
	return rejections
}

// sortServeRejections orders rejections the way renderServeRejections
// prints them, so the push log lists codes in line order.
func sortServeRejections(list []serveRejection) {
	key := func(ref string) string {
		if ref == "" {
			return "-"
		}
		return ref
	}
	sort.SliceStable(list, func(i, j int) bool { return key(list[i].Ref) < key(list[j].Ref) })
}

// checkServeRefUpdate applies the rule for the update's class and returns
// nil when the update passes.
func checkServeRefUpdate(ctx *serveVerdictContext, update serveRefUpdate, options servePushOptions) *serveRejection {
	switch update.Class {
	case serveRefActor:
		return checkServeActorUpdate(ctx, update)
	case serveRefMemory:
		return checkServeMemoryUpdate(ctx, update)
	case serveRefProposal:
		return checkServeProposalUpdate(ctx, update)
	case serveRefTag:
		return checkServeTagUpdate(ctx, update)
	case serveRefBranch:
		return checkServeBranchUpdate(ctx, update)
	case serveRefProtected:
		return checkServeProtectedUpdate(ctx, update, options)
	default:
		return checkServeReservedUpdate(ctx, update)
	}
}

// The actor, proposal and memory rules are in serve_verdict_facts.go.

// checkServeProtectedUpdate applies the protected branch rule in
// serve_protected.go. Protected updates are checked last, so the facts
// accepted from actor refs earlier in the push, and the push's proposal
// refs, are visible to it.
func checkServeProtectedUpdate(ctx *serveVerdictContext, update serveRefUpdate, options servePushOptions) *serveRejection {
	return evaluateServeProtectedUpdate(ctx, update, options).Rejection
}

// checkServeTagUpdate: tags are create-only (FR-010).
func checkServeTagUpdate(ctx *serveVerdictContext, update serveRefUpdate) *serveRejection {
	switch {
	case update.Creates():
		return nil
	case update.Deletes():
		return &serveRejection{
			Code:     serveReasonTagImmutable,
			IDs:      []string{update.Old},
			Sentence: update.Ref + " cannot be deleted; tags on this server are immutable.",
		}
	default:
		return &serveRejection{
			Code:     serveReasonTagImmutable,
			IDs:      []string{update.Old, update.New},
			Sentence: update.Ref + " already points at " + update.Old + "; tags on this server cannot be moved.",
		}
	}
}

// checkServeBranchUpdate: any mapped actor may create or fast-forward an
// ordinary branch. A forced update or a deletion needs the recorded owner
// or a maintainer; a branch without a record needs a maintainer (FR-011).
func checkServeBranchUpdate(ctx *serveVerdictContext, update serveRefUpdate) *serveRejection {
	if update.Creates() {
		return nil
	}
	if !update.Deletes() && serveIsAncestor(ctx.GitDir, update.Old, update.New) {
		return nil
	}
	owner, recorded := lookupServeBranchOwner(ctx.Owners, update.Ref)
	if recorded && owner.Owner == ctx.Actor {
		return nil
	}
	if ctx.IsMaintainer(ctx.Actor) {
		return nil
	}
	if !recorded {
		return &serveRejection{
			Code:     serveReasonBranchNotOwner,
			Sentence: update.Ref + " has no recorded owner; only a maintainer can force-update or delete it.",
		}
	}
	return &serveRejection{
		Code:     serveReasonBranchNotOwner,
		IDs:      []string{owner.Owner},
		Sentence: update.Ref + " belongs to " + owner.Owner + "; only its owner or a maintainer can force-update or delete it.",
	}
}

// serveIsAncestor reports whether old is an ancestor of new. Any failure,
// including a missing object, counts as not an ancestor, which only ever
// asks for more authority.
func serveIsAncestor(gitDir, oldOID, newOID string) bool {
	command := exec.Command("git", "--git-dir="+gitDir, "merge-base", "--is-ancestor", oldOID, newOID)
	// The inherited environment keeps Git's quarantine variables, so the
	// pushed objects are visible.
	command.Env = os.Environ()
	return command.Run() == nil
}

// checkServeReservedUpdate refuses every ref outside the supported classes,
// including the server marker.
func checkServeReservedUpdate(ctx *serveVerdictContext, update serveRefUpdate) *serveRejection {
	sentence := update.Ref + " is not a branch, tag or Hubnot ref this server accepts."
	if update.Ref == serveMarkerRef {
		sentence = serveMarkerRef + " is the server marker and cannot be pushed."
	}
	return &serveRejection{Code: serveReasonReservedRef, Sentence: sentence}
}
