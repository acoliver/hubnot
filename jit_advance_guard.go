package main

import (
	"fmt"
	"strings"
)

// checkStrictJITLineage is an opt-in request precondition, never an inherited
// review decision. The receive hook calls it on its fresh accepted catalog
// while receivePack owns RepoLock through the ref update. Client calls only
// provide earlier diagnostics and cannot authorize hosted advancement.
func checkStrictJITLineage(events []StoredEvent, current, root string) error {
	index, state, err := strictJITRootIdentity(events, current, root)
	if err != nil {
		return err
	}
	if err := requireLineageTerminalEligibility("strictly advance", current, state); err != nil {
		return err
	}
	if state.Merged {
		return fmt.Errorf("strict JIT lineage is already merged")
	}
	terminals := 0
	for _, id := range state.MemberIDs {
		member, err := index.candidate(id)
		if err != nil {
			return err
		}
		successors, err := index.successors(id)
		if err != nil {
			return err
		}
		if len(successors) == 0 {
			terminals++
		}
		policy, _, digest, err := loadPolicy(member.Event.Base)
		if err != nil {
			return fmt.Errorf("strict JIT family policy unavailable: %w", err)
		}
		evaluation, err := evaluateProposalWithCode(&member, events, policy, digest, proposalCodeState{})
		if err != nil {
			return fmt.Errorf("strict JIT family evaluation unavailable: %w", err)
		}
		if evaluation.Rejected {
			return fmt.Errorf("strict JIT lineage rejection: %s", id)
		}
		for _, request := range evaluation.ChangeRequests {
			answered := false
			for _, approval := range events {
				if approval.Event.Kind != "review.submit" || approval.Event.Verdict != "approve" || approval.Event.Actor != request.Event.Actor || approval.Event.Sequence <= request.Event.Sequence {
					continue
				}
				next, err := index.state(approval.Event.Subject)
				if err != nil {
					return fmt.Errorf("strict JIT review lineage unavailable: %w", err)
				}
				if next.RootID == root {
					answered = true
				}
			}
			if !answered {
				return fmt.Errorf("strict JIT unresolved lineage change request: %s", request.ID)
			}
		}
	}
	if terminals != 1 {
		return fmt.Errorf("strict JIT lineage has ambiguous current candidates")
	}
	return nil
}

// strictJITRootIdentity binds signed lineage without granting fresh advancement
// eligibility. Already recorded own-fact publication needs this identity too.
func strictJITRootIdentity(events []StoredEvent, current, root string) (*lineageIndex, proposalLineageState, error) {
	if err := requireFullEventID(current); err != nil {
		return nil, proposalLineageState{}, err
	}
	if err := requireFullEventID(root); err != nil {
		return nil, proposalLineageState{}, err
	}
	if root != strings.ToLower(root) {
		return nil, proposalLineageState{}, fmt.Errorf("strict JIT root must be a canonical full event ID")
	}
	index, err := buildLineageIndex(events)
	if err != nil {
		return nil, proposalLineageState{}, err
	}
	state, err := index.state(current)
	if err != nil {
		return nil, proposalLineageState{}, err
	}
	original, err := index.candidate(root)
	if err != nil {
		return nil, proposalLineageState{}, err
	}
	if state.RootID != root || original.Event.Kind != "proposal.open" {
		return nil, proposalLineageState{}, fmt.Errorf("strict JIT root does not match the signed original lineage")
	}
	return index, state, nil
}
