package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// hn merge PROPOSAL --remote REMOTE [--branch BRANCH] asks an hn serve
// remote to fast-forward a protected branch to the head of an accepted
// proposal, then signs the proposal.merged fact with commit equal to that
// head. contracts/advancement.md defines the nine steps; the functions below
// follow them in order. The command never creates a commit, never changes
// the checkout and never moves a local branch (C-005). The server signs
// nothing (C-006): the maintainer's client records the merge fact.

const (
	serveAdvanceUsage         = "usage: hn merge PROPOSAL [--remote REMOTE [--branch BRANCH]]"
	serveAdvanceDefaultBranch = "main"
	serveAdvanceInterruptEnv  = "HN_TEST_SERVE_ADVANCE_INTERRUPT_AFTER"
)

// mergeRemoteRequested reports whether hn merge arguments select remote
// advancement. Without a --remote or --branch token, cmdMerge keeps its
// original argument handling and messages unchanged (FR-016).
func mergeRemoteRequested(args []string) bool {
	for _, argument := range args {
		if argument == "--remote" || argument == "--branch" ||
			strings.HasPrefix(argument, "--remote=") || strings.HasPrefix(argument, "--branch=") {
			return true
		}
	}
	return false
}

// parseMergeRemoteArgs parses one positional PROPOSAL with --remote REMOTE
// and an optional --branch BRANCH, in any order, and returns the branch as a
// full refs/heads/ name.
func parseMergeRemoteArgs(args []string) (proposalID, remote, branchRef string, err error) {
	flags := quietFlags("merge")
	remoteFlag := flags.String("remote", "", "hn serve remote that holds the protected branch")
	branchFlag := flags.String("branch", serveAdvanceDefaultBranch, "protected branch to advance")
	var positional []string
	rest := args
	for {
		if err := flags.Parse(rest); err != nil {
			return "", "", "", usageError(serveAdvanceUsage)
		}
		if flags.NArg() == 0 {
			break
		}
		positional = append(positional, flags.Arg(0))
		rest = flags.Args()[1:]
	}
	remoteSet := false
	flags.Visit(func(set *flag.Flag) {
		if set.Name == "remote" {
			remoteSet = true
		}
	})
	if len(positional) != 1 || !remoteSet || *remoteFlag == "" {
		return "", "", "", usageError(serveAdvanceUsage)
	}
	branchRef, err = normalizeAdvanceBranch(*branchFlag)
	if err != nil {
		return "", "", "", err
	}
	return positional[0], *remoteFlag, branchRef, nil
}

// normalizeAdvanceBranch turns a short branch name or a refs/heads/ name
// into a valid full branch ref.
func normalizeAdvanceBranch(branch string) (string, error) {
	ref := branch
	if !strings.HasPrefix(ref, "refs/heads/") {
		if strings.HasPrefix(ref, "refs/") {
			return "", fmt.Errorf("invalid branch %q: name a branch or a refs/heads/ ref", branch)
		}
		ref = "refs/heads/" + ref
	}
	name := strings.TrimPrefix(ref, "refs/heads/")
	if name == "" || strings.HasPrefix(name, "-") {
		return "", fmt.Errorf("invalid branch %q", branch)
	}
	if _, err := gitOutput("check-ref-format", ref); err != nil {
		return "", fmt.Errorf("invalid branch %q", branch)
	}
	return ref, nil
}

// serveAdvanceStepError names the failing step, as the contract requires.
func serveAdvanceStepError(step int, name string, err error) error {
	return fmt.Errorf("advancement step %d (%s) failed: %w", step, name, err)
}

// serveAdvanceInterrupt is the test-only interruption point after a step,
// gated like runReplicationProcessHook.
func serveAdvanceInterrupt(step string) error {
	if os.Getenv("HN_INTERNAL_TESTING") == "1" && os.Getenv(serveAdvanceInterruptEnv) == step {
		return fmt.Errorf("interrupted for testing after %s", step)
	}
	return nil
}

// serveAdvanceRerun is the command a user reruns after an interruption.
func serveAdvanceRerun(proposalID, remote, branchRef string) string {
	command := "hn merge " + proposalID + " --remote " + remote
	if branchRef != "refs/heads/"+serveAdvanceDefaultBranch {
		command += " --branch " + strings.TrimPrefix(branchRef, "refs/heads/")
	}
	return command
}

// serveAdvanceBinding pins an advancement to what a caller already checked.
// ExpectedActor is the signing identity the caller authorized; ExpectedTip
// is the hosted branch tip the caller inspected. Empty fields are unbound.
type serveAdvanceBinding struct {
	ExpectedActor string
	ExpectedTip   string
}

func cmdMergeRemote(proposalID, remote, branchRef string) error {
	return mergeRemoteBound(proposalID, remote, branchRef, serveAdvanceBinding{})
}

func mergeRemoteBound(proposalID, remote, branchRef string, binding serveAdvanceBinding) error {
	if !validReplicationRemote(remote) {
		return fmt.Errorf("invalid remote name %q", remote)
	}
	if _, err := gitOutput("remote", "get-url", remote); err != nil {
		return fmt.Errorf("remote %q does not exist", remote)
	}

	// Step 1: validate locally.
	identity, proposal, evaluation, err := serveAdvanceValidate(proposalID)
	if err != nil {
		return serveAdvanceStepError(1, "validate locally", err)
	}
	if binding.ExpectedActor != "" && identity.Actor != binding.ExpectedActor {
		return serveAdvanceStepError(1, "validate locally", fmt.Errorf("selected signing identity changed or is not bound to this advancement"))
	}
	head := proposal.Event.Head

	// Step 2: identify the remote.
	if identified, _ := serveRemoteIdentifies(remote); !identified {
		return fmt.Errorf("remote %s is not an hn serve remote; use hn merge %s in a checkout of the target branch", remote, proposalID)
	}

	// Step 3: publish facts in their own pushes, so they stay on the server
	// even when the advancement is refused. A refusal carries the server's
	// hn-serve/1 lines in the serveRejectedPushError text.
	if err := publishLocalFacts(remote, nil); err != nil {
		return serveAdvanceStepError(3, "publish facts", err)
	}
	if err := serveAdvanceInterrupt("publish"); err != nil {
		return err
	}

	// Step 4: read the tip.
	tip, err := serveAdvanceReadTip(remote, branchRef)
	if err != nil {
		return serveAdvanceStepError(4, "read the tip", err)
	}
	if binding.ExpectedTip != "" && tip != binding.ExpectedTip {
		return serveAdvanceStepError(4, "read the tip", fmt.Errorf("%s %s is at %s, not the inspected %s; update the local branch and rerun", remote, branchRef, shortOID(tip), shortOID(binding.ExpectedTip)))
	}

	// Step 5: repair if already advanced.
	if head == tip || serveAdvanceIsAncestor(head, tip) {
		if evaluation.Merged {
			fmt.Printf("Proposal %s is already advanced on %s %s and recorded as merged\n", shortID(proposal.ID), remote, branchRef)
			return nil
		}
		fmt.Printf("%s %s already contains %s for proposal %s\n", remote, branchRef, shortOID(head), shortID(proposal.ID))
		return serveAdvanceRecordAndPublish(identity, proposal.ID, head, remote, "Recorded missing merge event")
	}
	if evaluation.Merged {
		return serveAdvanceStepError(1, "validate locally", fmt.Errorf("proposal %s is already recorded as merged but %s %s does not contain its head %s", proposal.ID, remote, branchRef, shortOID(head)))
	}

	// Step 6: request advancement. The forced refspec makes Git send a
	// non-fast-forward update instead of refusing it on the client, so the
	// server's checks decide and report their structured reason.
	stderr, err := serveAdvanceGit("push", "--porcelain",
		"--push-option="+servePushOptionAdvance+"="+proposal.ID,
		"--push-option="+servePushOptionExpected+"="+tip,
		remote, "+"+head+":"+branchRef)
	if err != nil {
		if _, lines := extractServeRejections(stderr); len(lines) > 0 {
			for _, line := range lines {
				fmt.Fprintln(os.Stderr, oneLine(line))
			}
			return serveAdvanceStepError(6, "request advancement", fmt.Errorf("%s refused to advance %s to %s; the branch is unchanged", remote, branchRef, shortOID(head)))
		}
		fmt.Fprint(os.Stderr, safeText(stderr))
		return serveAdvanceStepError(6, "request advancement", fmt.Errorf("git push to %s failed: %v", remote, err))
	}
	if err := serveAdvanceInterrupt("push"); err != nil {
		return err
	}

	// Step 7: confirm.
	current, found, err := serveAdvanceRemoteRef(remote, branchRef)
	if err != nil {
		return serveAdvanceStepError(7, "confirm", err)
	}
	if !found || current != head {
		at := current
		if !found {
			at = "no commit"
		}
		return fmt.Errorf("server accepted the push but %s is at %s; rerun %s", branchRef, at, serveAdvanceRerun(proposal.ID, remote, branchRef))
	}
	fmt.Printf("Advanced %s %s to %s for proposal %s\n", remote, branchRef, shortOID(head), shortID(proposal.ID))

	// Steps 8 and 9.
	return serveAdvanceRecordAndPublish(identity, proposal.ID, head, remote, "Recorded merge event")
}

// serveAdvanceValidate is step 1: the checks of cmdMerge that do not depend
// on a checkout, in the same order and with the same functions, plus
// readiness under the base policy. An existing merge fact is not an error
// here; step 5 decides what it means.
func serveAdvanceValidate(proposalID string) (*Identity, *StoredEvent, *ProposalEvaluation, error) {
	if err := requireFullEventID(proposalID); err != nil {
		return nil, nil, nil, err
	}
	// The proposal status scope checks the candidate's evaluation
	// dependencies without the checkout ancestry that a local merge needs.
	if err := prepareShallowVerification(shallowVerificationScope{Operation: "proposal status", Subject: proposalID}); err != nil {
		return nil, nil, nil, err
	}
	if err := guardShallowEventClosure("proposal advancement"); err != nil {
		return nil, nil, nil, err
	}
	events, err := collectEvents()
	if err != nil {
		return nil, nil, nil, err
	}
	proposal, err := resolveEventDependency("proposal advancement", proposalID, shallowCandidateEvent, events)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := guardProposalEvaluationDependencies("proposal advancement", proposal, events); err != nil {
		return nil, nil, nil, err
	}
	evaluation, err := evaluateProposal(proposal, events)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := requireLineageTerminalEligibility("merge", proposal.ID, evaluation.Lineage); err != nil {
		return nil, nil, nil, err
	}
	if evaluation.Rejected {
		return nil, nil, nil, fmt.Errorf("proposal has a current maintainer rejection")
	}
	if !evaluation.Accepted {
		return nil, nil, nil, fmt.Errorf("proposal lacks %d required acceptance decision(s)", evaluation.Policy.Proposals.RequiredAccepts)
	}
	if err := requireProposalCode(proposal); err != nil {
		return nil, nil, nil, err
	}
	identity, err := loadIdentity()
	if err != nil {
		return nil, nil, nil, err
	}
	if !actorListed(identity.Actor, evaluation.Policy.Maintainers) {
		return nil, nil, nil, fmt.Errorf("identity %s is not a maintainer under proposal policy", shortID(identity.Actor))
	}
	if !evaluation.Ready {
		return nil, nil, nil, fmt.Errorf("proposal is not ready; run 'hn proposal status %s' for missing evidence", proposal.ID)
	}
	return identity, proposal, evaluation, nil
}

// serveAdvanceReadTip is step 4: the remote value of branchRef, fetched
// into refs/remotes/REMOTE/NAME when the commit is not present locally.
func serveAdvanceReadTip(remote, branchRef string) (string, error) {
	tip, found, err := serveAdvanceRemoteRef(remote, branchRef)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("%s has no %s", remote, branchRef)
	}
	if serveAdvanceHasCommit(tip) {
		return tip, nil
	}
	tracking := "refs/remotes/" + remote + "/" + strings.TrimPrefix(branchRef, "refs/heads/")
	if _, err := gitOutput("fetch", "--quiet", "--no-tags", remote, "+"+branchRef+":"+tracking); err != nil {
		return "", fmt.Errorf("fetch %s from %s: %w", branchRef, remote, err)
	}
	if !serveAdvanceHasCommit(tip) {
		return "", fmt.Errorf("%s moved on %s while it was fetched; rerun the command", branchRef, remote)
	}
	return tip, nil
}

// serveAdvanceRemoteRef reads exactly one ref from git ls-remote. A pattern
// also matches longer refs ending in the same components, so only the line
// naming branchRef itself counts.
func serveAdvanceRemoteRef(remote, branchRef string) (string, bool, error) {
	output, err := gitOutput("ls-remote", "--", remote, branchRef)
	if err != nil {
		return "", false, fmt.Errorf("cannot list %s on %s", branchRef, remote)
	}
	for _, line := range strings.Split(string(output), "\n") {
		oid, ref, ok := strings.Cut(line, "\t")
		if !ok || ref != branchRef {
			continue
		}
		if !validGitOID(oid) {
			return "", false, fmt.Errorf("%s advertised a malformed object ID for %s", remote, branchRef)
		}
		return oid, true, nil
	}
	return "", false, nil
}

func serveAdvanceHasCommit(oid string) bool {
	_, err := gitOutput("cat-file", "-e", oid+"^{commit}")
	return err == nil
}

func serveAdvanceIsAncestor(ancestor, descendant string) bool {
	_, err := gitOutput("--no-replace-objects", "merge-base", "--is-ancestor", ancestor, descendant)
	return err == nil
}

// serveAdvanceRecordAndPublish runs steps 8 and 9 and prints the second
// output line with the given prefix.
func serveAdvanceRecordAndPublish(identity *Identity, proposalID, head, remote, recorded string) error {
	stored, err := serveAdvanceRecord(identity, proposalID, head)
	if err != nil {
		return serveAdvanceStepError(8, "record the merge fact", err)
	}
	fmt.Printf("%s %s with %d acceptance decision(s)\n", recorded, shortID(stored.ID), len(stored.Event.Evidence))
	if err := serveAdvanceInterrupt("record"); err != nil {
		return err
	}
	if err := publishLocalFacts(remote, nil); err != nil {
		fmt.Fprintf(os.Stderr, "hn: %v\n", err)
		return fmt.Errorf("merge fact recorded locally; run hn sync %s to publish it", remote)
	}
	return serveAdvanceInterrupt("republish")
}

// serveAdvanceRecord is step 8. It re-collects the events and re-evaluates
// the candidate the way the repair block of cmdMerge does, then appends
// proposal.merged with commit equal to the head. The branch was
// fast-forwarded, so there is no merge commit to look for.
func serveAdvanceRecord(identity *Identity, proposalID, head string) (*StoredEvent, error) {
	events, err := collectEvents()
	if err != nil {
		return nil, err
	}
	proposal, err := resolveFullEvent(events, proposalID)
	if err != nil {
		return nil, err
	}
	if err := guardProposalEvaluationDependencies("proposal advancement", proposal, events); err != nil {
		return nil, err
	}
	evaluation, err := evaluateProposal(proposal, events)
	if err != nil {
		return nil, err
	}
	if err := requireLineageTerminalEligibility("record merge event for", proposal.ID, evaluation.Lineage); err != nil {
		return nil, err
	}
	if evaluation.Merged {
		return nil, fmt.Errorf("proposal %s is already recorded as merged", proposal.ID)
	}
	if evaluation.Rejected || !evaluation.Accepted {
		return nil, fmt.Errorf("current accepting decisions no longer satisfy proposal policy")
	}
	if !evaluation.Ready {
		return nil, fmt.Errorf("current approval and CI evidence no longer satisfies proposal policy")
	}
	if !actorListed(identity.Actor, evaluation.Policy.Maintainers) {
		return nil, fmt.Errorf("identity %s is not a maintainer under proposal policy", shortID(identity.Actor))
	}
	if proposal.Event.Head != head {
		return nil, fmt.Errorf("proposal head %s does not match the advanced commit %s", shortOID(proposal.Event.Head), shortOID(head))
	}
	return appendProposalMergeEvent(identity, proposal, evaluation, head)
}

// serveAdvanceGit runs git with the inherited environment and returns its
// stderr, which carries the relayed remote: lines. Standard output, the
// porcelain status, is appended when stderr is empty so a failure always
// has text to show.
func serveAdvanceGit(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Stdin = bytes.NewReader(nil)
	cmd.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil && stderr.Len() == 0 {
		return stdout.String(), err
	}
	return stderr.String(), err
}
