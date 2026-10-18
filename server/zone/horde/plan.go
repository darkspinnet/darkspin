package horde

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	zoneobject "github.com/darkspinnet/darkspin/server/zone/object"
	zonepopulation "github.com/darkspinnet/darkspin/server/zone/population"
)

func PlanInitialWave(
	director game.CampaignDirector,
	publication game.CampaignDirectorPublication,
	firstObjectID uint32, gameID uint32,
) ([]zonenpc.SpawnPlan, uint32, error) {
	_, isHorde := InitialActorCount(publication.MarkerSetName)
	if !isHorde {
		return nil, firstObjectID, nil
	}
	plans, nextObjectID, err := PlanFirstWave(
		director, publication, firstObjectID, gameID,
	)
	if err != nil {
		return nil, firstObjectID, fmt.Errorf("initialWave: %w", err)
	}
	return plans, nextObjectID, nil
}

func PlanFirstWave(
	director game.CampaignDirector,
	publication game.CampaignDirectorPublication,
	firstObjectID uint32, gameID uint32,
) ([]zonenpc.SpawnPlan, uint32, error) {
	plans, nextObjectID, err := PlanFirstWaveWithSections(director, publication, firstObjectID, gameID, nil)
	if err != nil {
		return nil, firstObjectID, fmt.Errorf("firstWaveSections: %w", err)
	}
	return plans, nextObjectID, nil
}

func PlanFirstWaveWithSections(
	director game.CampaignDirector, publication game.CampaignDirectorPublication,
	firstObjectID uint32, gameID uint32, sections []zonepopulation.SectionRoster,
) ([]zonenpc.SpawnPlan, uint32, error) {
	if !IsTrigger(publication) {
		return nil, firstObjectID, nil
	}
	actorCount, isHorde := ListenerCount(publication)
	if !isHorde {
		return nil, firstObjectID,
			errors.New("hordePlanPublication: invalid")
	}
	if !strings.HasPrefix(strings.ToLower(publication.MarkerSetName), "zelems_1_") {
		actorCount = max(6, actorCount*2)
	}
	plans, nextObjectID, err := PlanWaveWithSections(
		director, publication, firstObjectID, gameID, actorCount, 1, sections,
	)
	if err != nil {
		return nil, firstObjectID, fmt.Errorf("firstWave: %w", err)
	}
	return plans, nextObjectID, nil
}

func PlanWave(
	director game.CampaignDirector,
	publication game.CampaignDirectorPublication,
	firstObjectID uint32, gameID uint32,
	actorCount int, waveOrdinal int,
) ([]zonenpc.SpawnPlan, uint32, error) {
	plans, nextObjectID, err := PlanWaveWithSections(
		director, publication, firstObjectID, gameID, actorCount, waveOrdinal, nil,
	)
	if err != nil {
		return nil, firstObjectID, fmt.Errorf("waveSections: %w", err)
	}
	return plans, nextObjectID, nil
}

func PlanWaveWithSections(
	director game.CampaignDirector, publication game.CampaignDirectorPublication,
	firstObjectID uint32, gameID uint32, actorCount int, waveOrdinal int,
	sections []zonepopulation.SectionRoster,
) ([]zonenpc.SpawnPlan, uint32, error) {
	listenerCount, isHorde := ListenerCount(publication)
	if !isHorde || actorCount <= 0 || waveOrdinal <= 0 {
		return nil, firstObjectID, errors.New("hordePlanWave: invalid")
	}
	if firstObjectID == 0 || firstObjectID >= zoneobject.ProjectileIDStart {
		return nil, firstObjectID,
			errors.New("hordePlanObjectID: exhausted")
	}
	authoredPositions := make([]game.Vec3, 0, listenerCount)
	authoredRotations := make([]game.Vec3, 0, listenerCount)
	for _, listener := range publication.Listeners {
		if listener.CallbackName != "HordeSpawner_Register" {
			continue
		}
		authoredPositions = append(authoredPositions, listener.Position)
		authoredRotations = append(authoredRotations, listener.Rotation)
	}
	seed := gameID
	if director.IsInitialLayoutSelected {
		// Stable across co-op admission, retries and checkpoint restoration.
		// The simulator MT stream is separate from the native layout LCG.
		seed = director.MapVariantSeed ^ 0x484f5244
	}
	random := sim.NewSimulatorRandom(seed ^ publication.TriggerMarkerID ^ uint32(waveOrdinal)*0x103)
	stage := max(uint32(1), director.Difficulty)
	section := zonepopulation.Section(publication.MarkerSetName)
	if section == 0 {
		// Horde markers do not encode the dispatcher's current route section.
		// Keep this local choice explicit until that caller is recovered.
		sectionIndex, sectionErr := random.Index(3)
		if sectionErr != nil {
			return nil, firstObjectID, fmt.Errorf("hordeWaveSection: %w", sectionErr)
		}
		section = sim.DirectorRouteSectionA + sim.DirectorRouteSection(sectionIndex)
	}
	minions := zonepopulation.PoolEntries(director, "minion")
	specials := zonepopulation.PoolEntries(director, "special")
	budget, err := localWaveGroupBudget(minions, stage, actorCount, director.CompositionTuning.GroupChallengeMultiplier)
	if err != nil {
		return nil, firstObjectID, fmt.Errorf("hordeWaveBudget: %w", err)
	}
	group, err := ComposeGroup(GroupInput{
		Budget: budget, Stage: stage, Section: section,
		Multiplier: director.CompositionTuning.GroupChallengeMultiplier,
		Minions:    minions, Specials: specials, Sections: sections, Random: random,
		PolicySeed: seed ^ publication.TriggerMarkerID ^ uint32(waveOrdinal)*0x484f5244,
	})
	if err != nil {
		return nil, firstObjectID, fmt.Errorf("hordeWaveGroup: %w", err)
	}
	actorCount = len(group.Entries)
	if actorCount == 0 {
		return nil, firstObjectID, errors.New("hordePlanGroup: no affordable candidate")
	}
	if actorCount > int(zoneobject.ProjectileIDStart-firstObjectID) {
		return nil, firstObjectID, errors.New("hordePlanObjectID: exhausted")
	}
	positions := zonepopulation.GroupPositions(authoredPositions, actorCount)
	if len(positions) != actorCount {
		return nil, firstObjectID, errors.New("hordePlanPosition: incomplete")
	}
	plans := make([]zonenpc.SpawnPlan, 0, actorCount)
	for index, entry := range group.Entries {
		plans = append(plans, zonenpc.SpawnPlan{
			ObjectID: firstObjectID + uint32(index),
			NounName: entry.NounName, Position: positions[index],
			Rotation:      zonepopulation.GroupRotation(authoredRotations, index),
			LocusID:       publication.TriggerMarkerID,
			Kind:          sim.DirectorLocusHorde,
			Introduction:  zonenpc.SpawnIntroductionFloorWarp,
			MarkerSetName: publication.MarkerSetName,
			NPCProfile:    entry.NPCProfile,
		})
	}
	if waveOrdinal == 3 {
		profile, isFound := director.NPCProfilesByNoun[strings.ToLower(zonenpc.MutationAgentNounName)]
		if !isFound || !profile.IsKnown {
			return nil, firstObjectID, errors.New("mutation agent profile unavailable")
		}
		if firstObjectID+uint32(actorCount) >= zoneobject.ProjectileIDStart {
			return nil, firstObjectID, errors.New("mutation agent object ID exhausted")
		}
		plans = append(plans, zonenpc.SpawnPlan{
			ObjectID: firstObjectID + uint32(actorCount), NounName: zonenpc.MutationAgentNounName,
			Position: positions[0], LocusID: publication.TriggerMarkerID,
			Kind: sim.DirectorLocusHorde, MarkerSetName: publication.MarkerSetName,
			NPCProfile: profile, ActionProfile: zonenpc.MutationAgentActionProfile(), IsActionKnown: true,
			Introduction: zonenpc.SpawnIntroductionFloorWarp,
		})
		actorCount++
	}
	return plans, firstObjectID + uint32(actorCount), nil
}

// Existing wave policy supplies a count hint, not a recovered native budget.
// Translate it into the cost of that many baseline minions, capped at fifteen;
// actual membership is then decided exclusively by the shared budget composer.
func localWaveGroupBudget(
	minions []game.CampaignDirectorEntry, stage uint32, count int, multiplier float32,
) (uint32, error) {
	entries := eligibleGroupEntries(minions, stage, false)
	if len(entries) == 0 || math.IsNaN(float64(multiplier)) ||
		math.IsInf(float64(multiplier), 0) || multiplier < 0 {
		return 0, errors.New("wave budget roster or multiplier invalid")
	}
	count = min(count, zonepopulation.MaximumGroupMemberCount)
	challenge := max(int32(1), entries[0].NPCProfile.ChallengeValue)
	cost := float32(float32(int64(challenge)*int64(count)) * float32(1+float32(multiplier*float32(count-1))))
	if math.IsInf(float64(cost), 0) || float64(cost) > math.MaxUint32 {
		return 0, errors.New("wave budget overflow")
	}
	return uint32(math.Ceil(float64(cost))), nil
}

func InitialActorCount(markerSetName string) (int, bool) {
	switch strings.ToLower(markerSetName) {
	case "zelems_1_ai_horde_1.markerset":
		return 3, true
	case "zelems_1_ai_horde_2.markerset":
		return 2, true
	default:
		return 0, false
	}
}

func ValidateInitialOrder(
	publication game.CampaignDirectorPublication,
	session *Session,
) error {
	_, isHorde := InitialActorCount(publication.MarkerSetName)
	if !isHorde {
		return nil
	}
	if session == nil {
		return errors.New("hordeOrder: unavailable")
	}
	// Horde 1 and Horde 2 belong to the same alternative group. Neither may
	// wait for the other to complete; only an already active horde can defer it.
	if session.IsActive() {
		return errors.New("hordeOrder: another horde active")
	}
	return nil
}

func IsInitialDeferred(
	publication game.CampaignDirectorPublication,
	session *Session,
) bool {
	_, isInitial := InitialActorCount(publication.MarkerSetName)
	return isInitial && session != nil && session.IsActive()
}

func eligibleAgents(
	director game.CampaignDirector,
) []game.CampaignDirectorEntry {
	agentEntry := zonepopulation.HordeEntries(director)
	eligibleEntry := make([]game.CampaignDirectorEntry, 0, len(agentEntry))
	for _, entry := range agentEntry {
		if !entry.IsHordeLegal || entry.NounName == "" ||
			!entry.NPCProfile.IsKnown {
			continue
		}
		eligibleEntry = append(eligibleEntry, entry)
	}
	return eligibleEntry
}
