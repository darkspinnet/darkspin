package population

import (
	"errors"
	"slices"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
)

// Preserve the existing first-clear entry coverage policy using the selected roster.
func addSecondChainEntryPopulation(plans, candidates []candidate, director game.CampaignDirector) ([]candidate, error) {
	if len(director.EntryPositions) == 0 {
		return plans, nil
	}
	entry := director.EntryPositions[0]
	usedIDs := make(map[uint32]bool)
	minionNoun := ""
	for _, plan := range plans {
		usedIDs[plan.locusID] = true
		for _, sourceID := range plan.sourceLocusIDs {
			usedIDs[sourceID] = true
		}
		if plan.section != sim.DirectorRouteSectionA || minionNoun != "" {
			continue
		}
		if plan.provisionalCount > 1 && len(plan.provisionalNounNames) > 1 {
			minionNoun = plan.provisionalNounNames[1]
		} else if !plan.isProvisionalCaptain && len(plan.provisionalNounNames) != 0 {
			minionNoun = plan.provisionalNounNames[0]
		}
	}
	if minionNoun == "" {
		return nil, errors.New("second chain entry minion unavailable")
	}
	nearby := make([]candidate, 0)
	for _, candidate := range candidates {
		if candidate.section != sim.DirectorRouteSectionA ||
			candidate.kind != sim.DirectorLocusWanderer ||
			len(candidate.positions) != 1 || usedIDs[candidate.locusID] {
			continue
		}
		position := candidate.positions[0]
		deltaX := position.X - entry.X
		deltaY := position.Y - entry.Y
		deltaZ := position.Z - entry.Z
		planarDistanceSquared := deltaX*deltaX + deltaY*deltaY
		if planarDistanceSquared < 20*20 || planarDistanceSquared > 70*70 ||
			deltaZ < -16 || deltaZ > 5 {
			continue
		}
		nearby = append(nearby, candidate)
	}
	slices.SortFunc(nearby, func(left, right candidate) int {
		leftDistance := candidateDistanceSquared(left, entry)
		rightDistance := candidateDistanceSquared(right, entry)
		if leftDistance < rightDistance {
			return -1
		}
		if leftDistance > rightDistance {
			return 1
		}
		if candidateBefore(left, right) {
			return -1
		}
		return 1
	})
	entryPlans := make([]candidate, 0, 5)
	for _, candidate := range nearby {
		position := candidate.positions[0]
		isSeparated := true
		for _, selected := range entryPlans {
			other := selected.positions[0]
			deltaX := position.X - other.X
			deltaY := position.Y - other.Y
			if deltaX*deltaX+deltaY*deltaY < 8*8 {
				isSeparated = false
				break
			}
		}
		if !isSeparated {
			continue
		}
		candidate.provisionalCount = 1
		candidate.provisionalNounNames = []string{minionNoun}
		candidate.isAmbush = false
		entryPlans = append(entryPlans, candidate)
		if len(entryPlans) == cap(entryPlans) {
			break
		}
	}
	return append(plans, entryPlans...), nil
}
