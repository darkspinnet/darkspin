package population

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
)

const maximumGroupMemberCount = 15

const MaximumGroupMemberCount = maximumGroupMemberCount

// GroupComposer owns the state shared by candidate draws. Callers own their
// temporary eligible lists and decide whether to retry after a rejected draw.
type GroupComposer struct {
	random          *sim.SimulatorRandom
	budget          uint32
	multiplier      float32
	sumChallenge    int64
	cost            float32
	selectedEntries []game.CampaignDirectorEntry
}

type GroupRoster struct {
	entries []game.CampaignDirectorEntry
}

type groupComposer = GroupComposer
type groupRoster = GroupRoster

type GroupComposerInput struct {
	Random     *sim.SimulatorRandom
	Budget     uint32
	Multiplier float32
}

func NewGroupComposer(req GroupComposerInput) (*GroupComposer, error) {
	if req.Random == nil || math.IsNaN(float64(req.Multiplier)) ||
		math.IsInf(float64(req.Multiplier), 0) || req.Multiplier < 0 {
		return nil, errors.New("group composer input invalid")
	}
	return &GroupComposer{
		random: req.Random, budget: req.Budget, multiplier: req.Multiplier,
		selectedEntries: make([]game.CampaignDirectorEntry, 0, MaximumGroupMemberCount),
	}, nil
}

func NewGroupRoster(entries []game.CampaignDirectorEntry) *GroupRoster {
	return &GroupRoster{entries: append([]game.CampaignDirectorEntry(nil), entries...)}
}

func (e *GroupRoster) Count() int      { return len(e.entries) }
func (e *GroupComposer) Count() int    { return len(e.selectedEntries) }
func (e *GroupComposer) Cost() float32 { return e.cost }

func (e *GroupComposer) Entries() []game.CampaignDirectorEntry {
	return append([]game.CampaignDirectorEntry(nil), e.selectedEntries...)
}

// CanFit bounds callers that refill empty pools. A refill with no affordable
// candidate must terminate rather than repeating the native no-fit loop.
func (e *GroupComposer) CanFit(entries []game.CampaignDirectorEntry) bool {
	if e.Count() >= MaximumGroupMemberCount {
		return false
	}
	for _, entry := range entries {
		if entry.NPCProfile.IsKnown && entry.NPCProfile.ChallengeValue >= 0 &&
			e.candidateCost(entry) <= float32(e.budget) {
			return true
		}
	}
	return false
}

func (e *GroupComposer) candidateCost(entry game.CampaignDirectorEntry) float32 {
	sumChallenge := e.sumChallenge + int64(entry.NPCProfile.ChallengeValue)
	countMultiplier := float32(e.multiplier * float32(len(e.selectedEntries)))
	groupMultiplier := float32(1 + countMultiplier)
	return float32(float32(sumChallenge) * groupMultiplier)
}

func (e *GroupComposer) tryAdd(roster *GroupRoster) (bool, error) {
	isAccepted, err := e.TryAdd(roster)
	if err != nil {
		return false, fmt.Errorf("groupDraw: %w", err)
	}
	return isAccepted, nil
}

// TryAdd follows sub_9F6230 / build 103 sub_9FBA70. Accepted entries stay
// eligible for repeated draws; rejection removes the first matching noun,
// which need not be the drawn slot when a roster contains duplicate nouns.
// The roster must own a copy, not an authored roster slice.
func (e *GroupComposer) TryAdd(
	roster *GroupRoster,
) (bool, error) {
	if len(roster.entries) == 0 || len(e.selectedEntries) >= maximumGroupMemberCount {
		return false, nil
	}
	nounIndex, err := e.random.Index(uint32(len(roster.entries)))
	if err != nil {
		return false, fmt.Errorf("groupIndex: %w", err)
	}
	entry := roster.entries[nounIndex]
	if !entry.NPCProfile.IsKnown || entry.NPCProfile.ChallengeValue < 0 {
		return false, fmt.Errorf("groupChallenge[%s]: invalid profile", entry.NounName)
	}
	sumChallenge := e.sumChallenge + int64(entry.NPCProfile.ChallengeValue)
	// Explicit float32 conversions preserve the separate MULSS/ADDSS steps
	// in activation/group-cost.asm, including the count before insertion.
	cost := e.candidateCost(entry)
	if cost <= float32(e.budget) {
		e.selectedEntries = append(e.selectedEntries, entry)
		e.sumChallenge = sumChallenge
		e.cost = cost
		return true, nil
	}
	for index, eligibleEntry := range roster.entries {
		if strings.EqualFold(eligibleEntry.NounName, entry.NounName) {
			roster.entries = append(roster.entries[:index], roster.entries[index+1:]...)
			break
		}
	}
	return false, nil
}
