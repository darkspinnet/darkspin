package population

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	zoneobject "github.com/darkspinnet/darkspin/server/zone/object"
)

const (
	mutationAgentNounName          = "MutationAgent.Noun"
	mutationAgentMinimumChainLevel = uint32(5)
	mutationAgentMaximumChainLevel = uint32(24)
	mutationAgentMinimumChance     = uint32(5)
	mutationAgentMaximumChance     = uint32(10)
	initialInfinityChainLevelIndex = uint32(13)
	exploderScarabSelectionChance  = uint32(25)
	exploderScarabNounSpecies      = "citadelbasicsuicide"
	roboBomberNounName             = "CitadelSpecificThree.Noun"
	roboBomberConfigKind           = "agent"
)

func (s *Session) PlanSpawns(
	director game.CampaignDirector, decisions []Decision, firstObjectID uint32,
) ([]zonenpc.SpawnPlan, uint32, error) {
	return s.planSpawns(director, decisions, firstObjectID, 0)
}

// PlanCampaignSpawns applies campaign-only population policies. Mutation
// Agents can replace one ordinary escort in an eligible population cluster;
// boss, named-Captain, horde, and follow-up planners never enter this path.
func (s *Session) PlanCampaignSpawns(
	director game.CampaignDirector, decisions []Decision, firstObjectID uint32,
	chainLevelIndex uint32,
) ([]zonenpc.SpawnPlan, uint32, error) {
	return s.planSpawns(director, decisions, firstObjectID, chainLevelIndex)
}

func (s *Session) planSpawns(
	director game.CampaignDirector, decisions []Decision, firstObjectID uint32,
	chainLevelIndex uint32,
) ([]zonenpc.SpawnPlan, uint32, error) {
	if s == nil || s.Random() == nil {
		return nil, firstObjectID, errors.New("spawnPlan: nil population session")
	}
	if firstObjectID == 0 || firstObjectID >= zoneobject.ProjectileIDStart {
		return nil, firstObjectID, fmt.Errorf("spawnPlanFirstID: %d", firstObjectID)
	}
	minionEntries := PoolEntries(director, "minion")
	captainEntries := PoolEntries(director, "captain")
	requestedCount, err := requestedSpawnCount(decisions)
	if err != nil {
		return nil, firstObjectID, fmt.Errorf("spawnCount: %w", err)
	}
	if requestedCount == 0 {
		return nil, firstObjectID, nil
	}
	err = validateSpawnPools(
		director, decisions, minionEntries, captainEntries,
		firstObjectID, requestedCount,
	)
	if err != nil {
		return nil, firstObjectID, fmt.Errorf("spawnValidate: %w", err)
	}
	plans := make([]zonenpc.SpawnPlan, 0, requestedCount)
	nextObjectID := firstObjectID
	for _, decision := range decisions {
		firstDecisionPlanIndex := len(plans)
		if len(decision.ProvisionalNounNames) > 0 {
			var planErr error
			plans, nextObjectID, planErr = appendAuthoredPlans(
				plans, director, decision, nextObjectID,
			)
			if planErr != nil {
				return nil, firstObjectID,
					fmt.Errorf("spawnAuthored: %w", planErr)
			}
			plans, planErr = replaceInitialInfinityExploderScarabs(
				director, plans, chainLevelIndex,
			)
			if planErr != nil {
				return nil, firstObjectID,
					fmt.Errorf("spawnRoboBomber: %w", planErr)
			}
			plans, planErr = s.applyMutationAgent(
				director, plans, firstDecisionPlanIndex, decision, chainLevelIndex,
			)
			if planErr != nil {
				return nil, firstObjectID,
					fmt.Errorf("spawnMutationAgent: %w", planErr)
			}
			applySpawnIntroductions(plans[firstDecisionPlanIndex:], decision)
			continue
		}
		count, captainCount, countErr := decisionSpawnCount(decision)
		if countErr != nil {
			return nil, firstObjectID,
				fmt.Errorf("spawnDecision: %w", countErr)
		}
		positions := GroupPositions(decision.Positions, count)
		for index := 0; index < count; index++ {
			isCaptain := index < captainCount
			entries := minionEntries
			if isCaptain {
				entries = captainEntries
			}
			selectedEntry, selectionErr := s.selectPopulationEntry(entries)
			if selectionErr != nil {
				return nil, firstObjectID,
					fmt.Errorf("spawnPlanNoun[%d]: %w", index, selectionErr)
			}
			profile := selectedEntry.NPCProfile
			bossIdentity := zonenpc.BossIdentity{}
			if isCaptain {
				profile = zonenpc.ApplyEliteProfile(profile)
				bossIdentity = captainIdentity(director, selectedEntry.NounName)
			}
			plans = append(plans, zonenpc.SpawnPlan{
				ObjectID: nextObjectID, NounName: selectedEntry.NounName,
				Position: positions[index], LocusID: decision.LocusID,
				Rotation: GroupRotation(decision.Rotations, index),
				Kind:     decision.Kind, IsCaptain: isCaptain,
				MarkerSetName: decision.MarkerSetName,
				NPCProfile:    profile, BossIdentity: bossIdentity,
			})
			nextObjectID++
		}
		plans, countErr = replaceInitialInfinityExploderScarabs(
			director, plans, chainLevelIndex,
		)
		if countErr != nil {
			return nil, firstObjectID,
				fmt.Errorf("spawnRoboBomber: %w", countErr)
		}
		plans, countErr = s.applyMutationAgent(
			director, plans, firstDecisionPlanIndex, decision, chainLevelIndex,
		)
		if countErr != nil {
			return nil, firstObjectID,
				fmt.Errorf("spawnMutationAgent: %w", countErr)
		}
		applySpawnIntroductions(plans[firstDecisionPlanIndex:], decision)
	}
	return plans, nextObjectID, nil
}

func applySpawnIntroductions(plans []zonenpc.SpawnPlan, decision Decision) {
	for index := range plans {
		profile, isProfileFound := zonenpc.ActionProfileForPlan(plans[index])
		switch {
		case isProfileFound && profile.PreAggroAnimationName != "":
			plans[index].Introduction = zonenpc.SpawnIntroductionDormant
		case decision.IsAmbush:
			plans[index].Introduction = zonenpc.SpawnIntroductionAmbush
		case decision.IsFloorIntroduction:
			plans[index].Introduction = zonenpc.SpawnIntroductionFloorWarp
		}
	}
}

func (s *Session) selectPopulationEntry(
	entries []game.CampaignDirectorEntry,
) (game.CampaignDirectorEntry, error) {
	if s == nil || s.Random() == nil || len(entries) == 0 {
		return game.CampaignDirectorEntry{}, errors.New("population entry unavailable")
	}
	nounIndex, err := s.Random().Index(uint32(len(entries)))
	if err != nil {
		return game.CampaignDirectorEntry{}, fmt.Errorf("entryIndex: %w", err)
	}
	selectedEntry := entries[nounIndex]
	if !isExploderScarabNoun(selectedEntry.NounName) {
		return selectedEntry, nil
	}
	roll, err := s.Random().Index(100)
	if err != nil {
		return game.CampaignDirectorEntry{}, fmt.Errorf("scarabRoll: %w", err)
	}
	if roll < exploderScarabSelectionChance {
		return selectedEntry, nil
	}
	ordinaryEntries := make([]game.CampaignDirectorEntry, 0, len(entries)-1)
	for _, entry := range entries {
		if !isExploderScarabNoun(entry.NounName) {
			ordinaryEntries = append(ordinaryEntries, entry)
		}
	}
	if len(ordinaryEntries) == 0 {
		return selectedEntry, nil
	}
	nounIndex, err = s.Random().Index(uint32(len(ordinaryEntries)))
	if err != nil {
		return game.CampaignDirectorEntry{}, fmt.Errorf("scarabReplacement: %w", err)
	}
	return ordinaryEntries[nounIndex], nil
}

func replaceInitialInfinityExploderScarabs(
	director game.CampaignDirector, plans []zonenpc.SpawnPlan,
	chainLevelIndex uint32,
) ([]zonenpc.SpawnPlan, error) {
	if chainLevelIndex != initialInfinityChainLevelIndex ||
		!strings.EqualFold(director.Level, "infinity_2") {
		return plans, nil
	}
	replacementEntry, configKind, isFound := EntryByNoun(director, roboBomberNounName)
	if !isFound || !strings.EqualFold(configKind, roboBomberConfigKind) ||
		!replacementEntry.NPCProfile.IsKnown {
		return nil, errors.New("4-1 Robo-Bomber unavailable")
	}
	for index := range plans {
		if !isExploderScarabNoun(plans[index].NounName) {
			continue
		}
		plans[index].NounName = replacementEntry.NounName
		plans[index].AuthoredNounName = ""
		plans[index].NPCProfile = replacementEntry.NPCProfile
		plans[index].ActionProfile = zonenpc.ActionProfile{}
		plans[index].IsActionKnown = false
	}
	return plans, nil
}

func isExploderScarabNoun(nounName string) bool {
	normalized := strings.ToLower(strings.TrimSpace(nounName))
	normalized = strings.TrimSuffix(normalized, ".noun")
	normalized = strings.TrimSuffix(normalized, "_2")
	normalized = strings.TrimSuffix(normalized, "_3")
	normalized = strings.TrimSuffix(normalized, "_captain")
	return normalized == exploderScarabNounSpecies
}

func (s *Session) applyMutationAgent(
	director game.CampaignDirector, plans []zonenpc.SpawnPlan,
	firstDecisionPlanIndex int, decision Decision, chainLevelIndex uint32,
) ([]zonenpc.SpawnPlan, error) {
	chance := mutationAgentChance(chainLevelIndex)
	isEligibleCluster := decision.Kind == sim.DirectorLocusSpike ||
		decision.IsProvisionalCaptain
	if chance == 0 || !isEligibleCluster ||
		len(plans)-firstDecisionPlanIndex < 3 {
		return plans, nil
	}
	mutationAgentNounKey := strings.ToLower(mutationAgentNounName)
	profile, isProfileFound := director.NPCProfilesByNoun[mutationAgentNounKey]
	if !isProfileFound || !profile.IsKnown || profile.HitPoint <= 0 {
		return nil, errors.New("profile unavailable")
	}
	roll, err := s.Random().Index(100)
	if err != nil {
		return nil, fmt.Errorf("roll: %w", err)
	}
	if roll >= chance {
		return plans, nil
	}
	agentPlanIndex := -1
	for planIndex := len(plans) - 1; planIndex >= firstDecisionPlanIndex; planIndex-- {
		if plans[planIndex].IsCaptain || plans[planIndex].IsBoss {
			continue
		}
		agentPlanIndex = planIndex
		break
	}
	if agentPlanIndex < 0 {
		return plans, nil
	}
	mutationTargetIndex := nearestMutationTargetPlan(
		plans, firstDecisionPlanIndex, agentPlanIndex,
	)
	if mutationTargetIndex < 0 {
		return plans, nil
	}
	targetPlan := &plans[mutationTargetIndex]
	// MutationAgent.Noun has authored class and AI records but no render noun.
	// A naturally selected Mutation Agent cannot retain its own invisible noun,
	// so borrow the nearby mutation target's known-renderable body. Otherwise
	// retain the selected escort body. Preserve the authored identity separately
	// for behavior and diagnostics.
	if strings.EqualFold(plans[agentPlanIndex].NounName, mutationAgentNounName) {
		plans[agentPlanIndex].NounName = targetPlan.NounName
	}
	plans[agentPlanIndex].AuthoredNounName = mutationAgentNounName
	plans[agentPlanIndex].NPCProfile = profile
	plans[agentPlanIndex].BossIdentity = zonenpc.BossIdentity{}
	plans[agentPlanIndex].ActionProfile = zonenpc.MutationAgentActionProfile()
	plans[agentPlanIndex].IsActionKnown = true

	targetAction, isTargetActionFound := zonenpc.ActionProfileForPlan(*targetPlan)
	if !isTargetActionFound {
		return nil, fmt.Errorf(
			"targetAction[%s]: unavailable", targetPlan.NounName,
		)
	}
	targetPlan.IsElite = true
	targetPlan.NPCProfile = zonenpc.ApplyEliteProfile(targetPlan.NPCProfile)
	targetAction.FirstAggroAnimationName = "npc_mutationagent_infected_grow"
	targetAction.FirstAggroDelay = zonenpc.MutationAgentTransformDuration
	targetAction.FirstAggroRevealDelay = 0
	targetAction.FirstAggroCinematicDuration = 0
	targetAction.FirstAggroCinematicRadius = 0
	targetAction.FirstAggroEffectName = ""
	targetAction.FirstAggroEffectDelay = 0
	targetAction.IsFirstAggroDurationKnown = true
	targetPlan.ActionProfile = targetAction
	targetPlan.IsActionKnown = true
	return plans, nil
}

func nearestMutationTargetPlan(
	plans []zonenpc.SpawnPlan, firstPlanIndex int, agentPlanIndex int,
) int {
	if firstPlanIndex < 0 || firstPlanIndex >= len(plans) ||
		agentPlanIndex < firstPlanIndex || agentPlanIndex >= len(plans) {
		return -1
	}
	agentPosition := plans[agentPlanIndex].Position
	targetPlanIndex := -1
	targetDistance := float32(math.MaxFloat32)
	for planIndex := firstPlanIndex; planIndex < len(plans); planIndex++ {
		candidate := plans[planIndex]
		if planIndex == agentPlanIndex || candidate.IsCaptain || candidate.IsBoss ||
			candidate.IsFixture || !candidate.NPCProfile.IsTargetable ||
			strings.EqualFold(candidate.NounName, mutationAgentNounName) {
			continue
		}
		distance := candidate.Position.Sub(agentPosition).Length()
		if distance > 15 || distance >= targetDistance {
			continue
		}
		targetPlanIndex = planIndex
		targetDistance = distance
	}
	return targetPlanIndex
}

func mutationAgentChance(chainLevelIndex uint32) uint32 {
	if chainLevelIndex < mutationAgentMinimumChainLevel {
		return 0
	}
	if chainLevelIndex >= mutationAgentMaximumChainLevel {
		return mutationAgentMaximumChance
	}
	progress := chainLevelIndex - mutationAgentMinimumChainLevel
	span := mutationAgentMaximumChainLevel - mutationAgentMinimumChainLevel
	chanceSpan := mutationAgentMaximumChance - mutationAgentMinimumChance
	return mutationAgentMinimumChance + progress*chanceSpan/span
}

func requestedSpawnCount(decisions []Decision) (int, error) {
	requestedCount := 0
	for _, decision := range decisions {
		if len(decision.ProvisionalNounNames) > 0 {
			requestedCount += len(decision.ProvisionalNounNames)
			continue
		}
		if decision.ProvisionalCount > 0 {
			requestedCount += decision.ProvisionalCount
			continue
		}
		switch decision.Kind {
		case sim.DirectorLocusWanderer:
			if decision.Wanderer.IsSpawn {
				requestedCount += int(decision.Wanderer.ClumpSize)
			}
		case sim.DirectorLocusSpike:
			requestedCount += SpikeGroupSize(decision.Challenge)
		default:
			return 0, fmt.Errorf("spawnPlanKind: %d", decision.Kind)
		}
	}
	return requestedCount, nil
}

func validateSpawnPools(
	director game.CampaignDirector, decisions []Decision,
	minionEntries []game.CampaignDirectorEntry,
	captainEntries []game.CampaignDirectorEntry,
	firstObjectID uint32, requestedCount int,
) error {
	if len(minionEntries) == 0 {
		return errors.New("spawnPlanMinion: empty pool")
	}
	err := validateEntryProfiles("spawnPlanMinionProfile", minionEntries)
	if err != nil {
		return fmt.Errorf("spawnMinion: %w", err)
	}
	if requestedCount > int(zoneobject.ProjectileIDStart-firstObjectID) {
		return errors.New("spawnPlanObjectID: exhausted")
	}
	for _, decision := range decisions {
		err = validateDecision(director, decision, captainEntries)
		if err != nil {
			return fmt.Errorf("spawnDecision: %w", err)
		}
	}
	return nil
}

func validateDecision(
	director game.CampaignDirector, decision Decision,
	captainEntries []game.CampaignDirectorEntry,
) error {
	for _, nounName := range decision.ProvisionalNounNames {
		entry, _, isFound := EntryByNoun(director, nounName)
		if !isFound {
			return fmt.Errorf("spawnPlanFixtureNoun[%s]: missing", nounName)
		}
		if !entry.NPCProfile.IsKnown {
			return fmt.Errorf("spawnPlanFixtureProfile[%s]: missing", nounName)
		}
	}
	isCaptainNeeded := decision.IsProvisionalCaptain ||
		(decision.Kind == sim.DirectorLocusSpike && decision.Challenge != 0)
	if isCaptainNeeded && len(captainEntries) == 0 {
		return errors.New("spawnPlanCaptain: empty pool")
	}
	if isCaptainNeeded {
		err := validateEntryProfiles("spawnPlanCaptainProfile", captainEntries)
		if err != nil {
			return fmt.Errorf("spawnCaptain: %w", err)
		}
	}
	if len(decision.Positions) == 0 {
		return errors.New("spawnPlanPosition: empty")
	}
	return nil
}

func validateEntryProfiles(
	errorName string, entries []game.CampaignDirectorEntry,
) error {
	for _, entry := range entries {
		if !entry.NPCProfile.IsKnown {
			return fmt.Errorf("%s[%s]: missing", errorName, entry.NounName)
		}
	}
	return nil
}

func appendAuthoredPlans(
	plans []zonenpc.SpawnPlan, director game.CampaignDirector,
	decision Decision, nextObjectID uint32,
) ([]zonenpc.SpawnPlan, uint32, error) {
	positions := GroupPositions(
		decision.Positions, len(decision.ProvisionalNounNames),
	)
	for index, nounName := range decision.ProvisionalNounNames {
		selectedEntry, configKind, isFound := EntryByNoun(director, nounName)
		if !isFound {
			return nil, nextObjectID,
				fmt.Errorf("spawnPlanFixtureNoun[%d]: %s", index, nounName)
		}
		isCaptain := strings.EqualFold(configKind, "captain")
		profile := selectedEntry.NPCProfile
		bossIdentity := zonenpc.BossIdentity{}
		if isCaptain {
			profile = zonenpc.ApplyEliteProfile(profile)
			bossIdentity = captainIdentity(director, selectedEntry.NounName)
		}
		plans = append(plans, zonenpc.SpawnPlan{
			ObjectID: nextObjectID, NounName: selectedEntry.NounName,
			Position: positions[index], LocusID: decision.LocusID,
			Rotation: GroupRotation(decision.Rotations, index),
			Kind:     decision.Kind, IsCaptain: isCaptain,
			MarkerSetName: decision.MarkerSetName,
			NPCProfile:    profile, BossIdentity: bossIdentity,
		})
		nextObjectID++
	}
	return plans, nextObjectID, nil
}

func captainIdentity(
	director game.CampaignDirector, nounName string,
) zonenpc.BossIdentity {
	nounKey := strings.ToLower(strings.TrimSpace(nounName))
	identityKey := captainIdentityNounKey(nounKey)
	identity, isFound := director.NPCIdentitiesByNoun[identityKey]
	if !isFound && identityKey != nounKey {
		identity, isFound = director.NPCIdentitiesByNoun[nounKey]
	}
	if !isFound {
		return zonenpc.BossIdentity{}
	}
	bossIdentity, isIdentityValid := zonenpc.BossIdentityFromContent(identity)
	if !isIdentityValid {
		return zonenpc.BossIdentity{}
	}
	return bossIdentity
}

func captainIdentityNounKey(nounName string) string {
	baseName := strings.TrimSuffix(nounName, ".noun")
	if strings.Contains(baseName, "_captain") {
		return baseName + ".noun"
	}
	for _, rankSuffix := range []string{"_2", "_3"} {
		if strings.HasSuffix(baseName, rankSuffix) {
			return strings.TrimSuffix(baseName, rankSuffix) +
				"_captain" + rankSuffix + ".noun"
		}
	}
	return baseName + "_captain.noun"
}

func decisionSpawnCount(decision Decision) (int, int, error) {
	if decision.ProvisionalCount > 0 {
		if decision.Kind != sim.DirectorLocusWanderer {
			return 0, 0, errors.New("spawnPlanFixture: invalid kind")
		}
		captainCount := 0
		if decision.IsProvisionalCaptain {
			captainCount = 1
		}
		return decision.ProvisionalCount, captainCount, nil
	}
	switch decision.Kind {
	case sim.DirectorLocusWanderer:
		if !decision.Wanderer.IsSpawn {
			return 0, 0, nil
		}
		return int(decision.Wanderer.ClumpSize), 0, nil
	case sim.DirectorLocusSpike:
		count := SpikeGroupSize(decision.Challenge)
		if count == 0 {
			return 0, 0, nil
		}
		return count, 1, nil
	default:
		return 0, 0, fmt.Errorf("spawnPlanKind: %d", decision.Kind)
	}
}

func PoolEntries(
	director game.CampaignDirector, configKind string,
) []game.CampaignDirectorEntry {
	for _, pool := range director.Pools {
		if strings.EqualFold(pool.ConfigKind, configKind) {
			return pool.Entries
		}
	}
	return nil
}

func EntryByNoun(
	director game.CampaignDirector, nounName string,
) (game.CampaignDirectorEntry, string, bool) {
	for _, pool := range director.Pools {
		for _, entry := range pool.Entries {
			if strings.EqualFold(entry.NounName, nounName) {
				return entry, pool.ConfigKind, true
			}
		}
	}
	return game.CampaignDirectorEntry{}, "", false
}

func SpikeGroupSize(challenge uint32) int {
	if challenge == 0 {
		return 0
	}
	return int(min(uint32(6), max(uint32(2), (challenge+19)/20)))
}

func GroupPositions(authored []game.Vec3, count int) []game.Vec3 {
	if count <= 0 || len(authored) == 0 {
		return nil
	}
	positions := make([]game.Vec3, 0, count)
	authoredCount := min(count, len(authored))
	positions = append(positions, authored[:authoredCount]...)
	anchor := authored[0]
	for index := authoredCount; index < count; index++ {
		angle := 2 * math.Pi * float64(index-authoredCount) /
			float64(count-authoredCount)
		positions = append(positions, game.Vec3{
			X: anchor.X + 2*float32(math.Cos(angle)),
			Y: anchor.Y + 2*float32(math.Sin(angle)),
			Z: anchor.Z,
		})
	}
	return positions
}
