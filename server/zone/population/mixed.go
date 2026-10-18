package population

import (
	"errors"
	"fmt"
	"sync"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
)

// GroupCompositionContext supplies the live state read by sub_9F5110.
type GroupCompositionContext struct {
	AlivePartyCount  int
	ActiveAgentCount int
}

type groupMember struct {
	entry     game.CampaignDirectorEntry
	isCaptain bool
	isAgent   bool
}

// Mixed composition's probability rolls use a global LCG, separate from the
// simulator MT used for noun selection. Its original global seed is unknown;
// the session seed keeps our stream stable across the map session.
type mixedGroupRandom struct {
	mu    sync.Mutex
	state uint32
}

func (e *mixedGroupRandom) draw() float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.state *= 663608941
	return float64(int32(e.state))*(1.0/4294967296.0) + 0.5
}

func (s *Session) composeSpikeGroup(
	director game.CampaignDirector, decision Decision, context GroupCompositionContext,
) ([]groupMember, error) {
	if decision.Challenge == 0 {
		return nil, nil
	}
	if s.mixedRandom == nil {
		return nil, errors.New("mixedRandom: unavailable")
	}
	minionEntries := PoolEntries(director, "minion")
	specialEntries := PoolEntries(director, "special")
	agentEntries := PoolEntries(director, "agent")
	currentMinionEntries := minionEntries
	currentSpecialEntries := specialEntries
	nextMinionEntries := minionEntries
	for _, roster := range s.sectionRosters {
		if roster.Section == decision.Section {
			currentMinionEntries = roster.Minions
			currentSpecialEntries = roster.Specials
		}
		if roster.Section == nextMixedSection(decision.Section) {
			nextMinionEntries = roster.Minions
		}
	}
	currentMinions := NewGroupRoster(currentMinionEntries)
	combinedEntries := append([]game.CampaignDirectorEntry(nil), nextMinionEntries...)
	combinedEntries = append(combinedEntries, currentMinionEntries...)
	combinedMinions := NewGroupRoster(combinedEntries)
	globalMinions := NewGroupRoster(minionEntries)
	currentSpecials := NewGroupRoster(currentSpecialEntries)
	globalSpecials := NewGroupRoster(specialEntries)
	agents := NewGroupRoster(agentEntries)
	composer, err := NewGroupComposer(GroupComposerInput{
		Random: s.Random(), Budget: decision.Challenge, Multiplier: s.groupChallengeMultiplier,
	})
	if err != nil {
		return nil, fmt.Errorf("mixedComposer: %w", err)
	}
	specialCeiling := mixedSpecialCeiling(s.mixedRandom.draw())
	isAgentAllowed := director.Difficulty >= 9 && mixedAgentAllowed(context)
	members := make([]groupMember, 0, maximumGroupMemberCount)
	minionCount, specialCount, agentCount := 0, 0, 0
	activeMinions := currentMinions
	for composer.Count() < maximumGroupMemberCount && composer.Cost() < float32(decision.Challenge) &&
		activeMinions.Count() != 0 {
		// The native mixed loop consumes this draw even when it selects a minion.
		s.mixedRandom.draw()
		activeMinions = currentMinions
		if minionCount >= 8 {
			activeMinions = globalMinions
		} else if minionCount >= 4 {
			activeMinions = combinedMinions
		}
		activeSpecials := currentSpecials
		if specialCount >= 2 {
			activeSpecials = globalSpecials
		}
		chosenRoster := activeMinions
		isAgent := false
		isSpecial := false
		if activeSpecials.Count() != 0 && specialCount+agentCount < specialCeiling {
			chosenRoster = activeSpecials
			isSpecial = true
			if agents.Count() != 0 && isAgentAllowed && s.mixedRandom.draw() < 0.10 {
				chosenRoster = agents
				isAgent = true
				isSpecial = false
			}
		}
		if chosenRoster.Count() == 0 {
			break
		}
		isAccepted, addErr := composer.tryAdd(chosenRoster)
		if addErr != nil {
			return nil, fmt.Errorf("mixedAdd: %w", addErr)
		}
		if !isAccepted {
			continue
		}
		entry := composer.selectedEntries[len(composer.selectedEntries)-1]
		members = append(members, groupMember{entry: entry, isAgent: isAgent})
		switch {
		case isAgent:
			agentCount++
		case isSpecial:
			specialCount++
		default:
			minionCount++
		}
	}
	return members, nil
}

func mixedSpecialCeiling(draw float64) int {
	switch {
	case draw < 0.05:
		return 99
	case draw < 0.20:
		return 2
	case draw < 0.70:
		return 1
	default:
		return 0
	}
}

func mixedAgentAllowed(context GroupCompositionContext) bool {
	limit := context.AlivePartyCount - 2
	if context.AlivePartyCount == 2 {
		limit = 1
	}
	return context.ActiveAgentCount < limit
}

func nextMixedSection(section sim.DirectorRouteSection) sim.DirectorRouteSection {
	switch section {
	case sim.DirectorRouteSectionA:
		return sim.DirectorRouteSectionB
	case sim.DirectorRouteSectionB:
		return sim.DirectorRouteSectionC
	case sim.DirectorRouteSectionC:
		return sim.DirectorRouteSectionA
	default:
		return section
	}
}
