package horde

import (
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	zonepopulation "github.com/darkspinnet/darkspin/server/zone/population"
)

// GroupInput supplies a budget and fixed section archetypes. It contains no
// wave count, admission, pacing, or scheduler decision.
type GroupInput struct {
	Budget     uint32
	Stage      uint32
	Section    sim.DirectorRouteSection
	Multiplier float32
	Minions    []game.CampaignDirectorEntry
	Specials   []game.CampaignDirectorEntry
	Sections   []zonepopulation.SectionRoster
	Random     *sim.SimulatorRandom
	PolicySeed uint32
}

type GroupResult struct {
	Entries      []game.CampaignDirectorEntry
	SpecialCount int
	Cost         uint32
}

type hordeGroup struct {
	composer     *zonepopulation.GroupComposer
	stageMinions []game.CampaignDirectorEntry
	globalMinion *zonepopulation.GroupRoster
	policyState  uint32
}

// ComposeGroup follows sub_9F86D0 (mode 7), including independent LCG policy
// and simulator MT noun draws. Server seeds remain an explicit local policy.
func ComposeGroup(req GroupInput) (GroupResult, error) {
	if req.Stage == 0 || req.Random == nil || req.Section < sim.DirectorRouteSectionA ||
		req.Section > sim.DirectorRouteSectionAny {
		return GroupResult{}, errors.New("horde group input invalid")
	}
	composer, err := zonepopulation.NewGroupComposer(zonepopulation.GroupComposerInput{
		Random: req.Random, Budget: req.Budget, Multiplier: req.Multiplier,
	})
	if err != nil {
		return GroupResult{}, fmt.Errorf("hordeComposer: %w", err)
	}
	group := hordeGroup{
		composer: composer, stageMinions: eligibleGroupEntries(req.Minions, req.Stage, false),
		globalMinion: zonepopulation.NewGroupRoster(eligibleGroupEntries(req.Minions, req.Stage, true)),
		policyState:  req.PolicySeed | 1,
	}
	currentMinion := zonepopulation.NewGroupRoster(sectionGroupEntries(req, req.Section, false))
	// sub_9C18E0 selects the previous or next section with equal probability.
	adjacentSection := req.Section - 1
	if req.Random.Float64() >= 0.5 {
		adjacentSection = req.Section + 1
	}
	if adjacentSection == 0 {
		adjacentSection = sim.DirectorRouteSectionC
	} else if adjacentSection == sim.DirectorRouteSectionAny {
		adjacentSection = sim.DirectorRouteSectionA
	} else if adjacentSection > sim.DirectorRouteSectionAny {
		adjacentSection = sim.DirectorRouteSectionAny
	}
	combinedMinions := sectionGroupEntries(req, adjacentSection, false)
	combinedMinions = append(combinedMinions, sectionGroupEntries(req, req.Section, false)...)
	combinedMinion := zonepopulation.NewGroupRoster(combinedMinions)
	specialSectionIndex, err := req.Random.Index(3)
	if err != nil {
		return GroupResult{}, fmt.Errorf("hordeSpecialSection: %w", err)
	}
	sectionSpecial := zonepopulation.NewGroupRoster(sectionGroupEntries(
		req, sim.DirectorRouteSectionA+sim.DirectorRouteSection(specialSectionIndex), true,
	))
	globalSpecial := zonepopulation.NewGroupRoster(eligibleGroupEntries(req.Specials, req.Stage, true))
	activeMinion := group.resolveMinion(currentMinion)
	specialCeiling := hordeSpecialCeiling(req.Stage, group.policyDraw())
	minionCount, specialCount := 0, 0
	for composer.Count() < zonepopulation.MaximumGroupMemberCount && composer.Cost() < float32(req.Budget) &&
		activeMinion.Count() != 0 {
		// The native routine consumes a policy draw even though this draw does
		// not affect its special-first branch.
		group.policyDraw()
		specialRoster := sectionSpecial
		if specialCount >= 3 {
			specialRoster = globalSpecial
		}
		minionRoster := currentMinion
		if minionCount >= 13 {
			minionRoster = group.globalMinion
		} else if minionCount >= 7 {
			minionRoster = combinedMinion
		}
		activeMinion = group.resolveMinion(minionRoster)
		if specialRoster.Count() != 0 && specialCount < specialCeiling {
			isAccepted, drawErr := composer.TryAdd(specialRoster)
			if drawErr != nil {
				return GroupResult{}, fmt.Errorf("hordeSpecial: %w", drawErr)
			}
			if isAccepted {
				specialCount++
			}
			continue
		}
		isAccepted, drawErr := composer.TryAdd(activeMinion)
		if drawErr != nil {
			return GroupResult{}, fmt.Errorf("hordeMinion: %w", drawErr)
		}
		if isAccepted {
			minionCount++
		}
	}
	cost := min(float64(composer.Cost()), float64(math.MaxUint32))
	return GroupResult{
		Entries: composer.Entries(), SpecialCount: specialCount, Cost: uint32(cost),
	}, nil
}

// sub_9F8640 prefers remaining global horde minions, then refills from the
// global minion list using stage alone. CanFit prevents unsafe no-fit refills.
func (e *hordeGroup) resolveMinion(roster *zonepopulation.GroupRoster) *zonepopulation.GroupRoster {
	if roster.Count() != 0 {
		return roster
	}
	if e.globalMinion.Count() != 0 {
		return e.globalMinion
	}
	if e.composer.CanFit(e.stageMinions) {
		e.globalMinion = zonepopulation.NewGroupRoster(e.stageMinions)
	}
	return e.globalMinion
}

func (e *hordeGroup) policyDraw() float64 {
	e.policyState *= 663608941
	return float64(int32(e.policyState))*(1.0/4294967296.0) + 0.5
}

func hordeSpecialCeiling(stage uint32, draw float64) int {
	if draw < float64(float32(0.2)) {
		switch {
		case stage >= 33:
			return 99
		case stage >= 21:
			return 4
		case stage >= 9:
			return 3
		default:
			return 2
		}
	}
	// Keep the authored float32 cumulative comparisons from sub_9F86D0.
	if float32(draw) < float32(float32(0.2)+float32(0.05)) {
		return 2
	}
	if float32(draw) < float32(float32(float32(0.35)+float32(0.05))+float32(0.2)) {
		return 1
	}
	return 0
}

func sectionGroupEntries(req GroupInput, section sim.DirectorRouteSection, isSpecial bool) []game.CampaignDirectorEntry {
	if section != sim.DirectorRouteSectionAny {
		for _, roster := range req.Sections {
			if roster.Section != section {
				continue
			}
			if isSpecial {
				return eligibleGroupEntries(roster.Specials, req.Stage, true)
			}
			return eligibleGroupEntries(roster.Minions, req.Stage, true)
		}
	}
	// Unavailable section metadata falls back to the global roster explicitly;
	// it must not invent a section's archetype choices from noun names.
	if isSpecial {
		return eligibleGroupEntries(req.Specials, req.Stage, true)
	}
	return eligibleGroupEntries(req.Minions, req.Stage, true)
}

func eligibleGroupEntries(entries []game.CampaignDirectorEntry, stage uint32, isHordeOnly bool) []game.CampaignDirectorEntry {
	eligibleEntries := make([]game.CampaignDirectorEntry, 0, len(entries))
	for _, entry := range entries {
		if stage < entry.MinimumDifficulty || stage > entry.MaximumDifficulty ||
			(isHordeOnly && !entry.IsHordeLegal) || entry.NounName == "" ||
			!entry.NPCProfile.IsKnown || entry.NPCProfile.ChallengeValue < 0 {
			continue
		}
		eligibleEntries = append(eligibleEntries, entry)
	}
	return eligibleEntries
}
