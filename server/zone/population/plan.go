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
	initialInfinityChainLevelIndex = uint32(13)
	exploderScarabSelectionChance  = uint32(25)
	exploderScarabNounSpecies      = "citadelbasicsuicide"
	roboBomberNounName             = "CitadelSpecificThree.Noun"
)

func (s *Session) PlanSpawns(
	director game.CampaignDirector, decisions []Decision, firstObjectID uint32,
) ([]zonenpc.SpawnPlan, uint32, error) {
	return s.planSpawns(director, decisions, firstObjectID, 0, GroupCompositionContext{})
}

// PlanCampaignSpawns applies campaign-only population policies.
func (s *Session) PlanCampaignSpawns(
	director game.CampaignDirector, decisions []Decision, firstObjectID uint32,
	chainLevelIndex uint32,
) ([]zonenpc.SpawnPlan, uint32, error) {
	return s.planSpawns(director, decisions, firstObjectID, chainLevelIndex, GroupCompositionContext{})
}

// PlanCampaignSpawnsWithContext supplies live party and agent counts for the
// conditional mixed-group agent branch.
func (s *Session) PlanCampaignSpawnsWithContext(
	director game.CampaignDirector, decisions []Decision, firstObjectID uint32,
	chainLevelIndex uint32, compositionContext GroupCompositionContext,
) ([]zonenpc.SpawnPlan, uint32, error) {
	return s.planSpawns(director, decisions, firstObjectID, chainLevelIndex, compositionContext)
}

func (s *Session) planSpawns(
	director game.CampaignDirector, decisions []Decision, firstObjectID uint32,
	chainLevelIndex uint32, compositionContext GroupCompositionContext,
) ([]zonenpc.SpawnPlan, uint32, error) {
	if s == nil || s.Random() == nil {
		return nil, firstObjectID, errors.New("spawnPlan: nil population session")
	}
	if firstObjectID == 0 || firstObjectID >= zoneobject.ProjectileIDStart {
		return nil, firstObjectID, fmt.Errorf("spawnPlanFirstID: %d", firstObjectID)
	}
	minionEntries := PoolEntries(director, "minion")
	captainEntries := PoolEntries(director, "captain")
	requestedCount, err := maximumSpawnCount(decisions)
	if err != nil {
		return nil, firstObjectID, fmt.Errorf("spawnCount: %w", err)
	}
	if requestedCount == 0 {
		return nil, firstObjectID, nil
	}
	err = validateSpawnPools(
		director, decisions, minionEntries, captainEntries,
	)
	if err != nil {
		return nil, firstObjectID, fmt.Errorf("spawnValidate: %w", err)
	}
	plans := make([]zonenpc.SpawnPlan, 0, requestedCount)
	nextObjectID := firstObjectID
	for _, decision := range decisions {
		firstDecisionPlanIndex := len(plans)
		sectionMinions := minionEntries
		for _, roster := range s.sectionRosters {
			if roster.Section == decision.Section {
				sectionMinions = roster.Minions
				break
			}
		}
		if len(decision.ProvisionalNounNames) > 0 {
			if len(decision.ProvisionalNounNames) > int(zoneobject.ProjectileIDStart-nextObjectID) {
				return nil, firstObjectID, errors.New("spawnAuthoredID: exhausted")
			}
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
			applySpawnIntroductions(plans[firstDecisionPlanIndex:], decision)
			continue
		}
		var selectedMembers []groupMember
		count, captainCount, countErr := decisionSpawnCount(decision)
		if decision.Kind == sim.DirectorLocusSpike && decision.ProvisionalCount == 0 {
			selectedMembers, countErr = s.composeSpikeGroup(
				director, decision, compositionContext,
			)
			count = len(selectedMembers)
		}
		if countErr != nil {
			return nil, firstObjectID,
				fmt.Errorf("spawnDecision: %w", countErr)
		}
		for _, member := range selectedMembers {
			if member.isAgent {
				compositionContext.ActiveAgentCount++
			}
		}
		if count > int(zoneobject.ProjectileIDStart-nextObjectID) {
			return nil, firstObjectID, errors.New("spawnGroupID: exhausted")
		}
		positions := GroupPositions(decision.Positions, count)
		for index := 0; index < count; index++ {
			isCaptain := index < captainCount
			entries := sectionMinions
			if isCaptain {
				entries = captainEntries
			}
			var selectedEntry game.CampaignDirectorEntry
			if len(selectedMembers) > 0 {
				selectedEntry = selectedMembers[index].entry
				isCaptain = selectedMembers[index].isCaptain
			} else {
				var selectionErr error
				selectedEntry, selectionErr = s.selectPopulationEntry(entries)
				if selectionErr != nil {
					return nil, firstObjectID,
						fmt.Errorf("spawnPlanNoun[%d]: %w", index, selectionErr)
				}
			}
			profile := selectedEntry.NPCProfile
			bossIdentity := zonenpc.BossIdentity{}
			if isCaptain {
				bossIdentity = CaptainIdentity(director, selectedEntry.NounName)
				profile = zonenpc.ApplyCaptainProfile(profile, bossIdentity)
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
		applySpawnIntroductions(plans[firstDecisionPlanIndex:], decision)
	}
	plans, nextObjectID, err = pairCopterSpawns(plans, nextObjectID)
	if err != nil {
		return nil, firstObjectID, fmt.Errorf("spawnCopterPair: %w", err)
	}
	return plans, nextObjectID, nil
}

func applySpawnIntroductions(plans []zonenpc.SpawnPlan, decision Decision) {
	for index := range plans {
		if decision.Kind == sim.DirectorLocusSpike && decision.IsAmbush {
			plans[index].Introduction = zonenpc.SpawnIntroductionFloorWarp
			continue
		}
		// Ordinary map population is already present, including groups whose
		// authored pre-aggro pose keeps them dormant until approached.
		plans[index].Introduction = zonenpc.SpawnIntroductionDormant
	}
}

func (s *Session) selectPopulationEntry(
	entries []game.CampaignDirectorEntry,
) (game.CampaignDirectorEntry, error) {
	if s == nil || s.Random() == nil || len(entries) == 0 {
		return game.CampaignDirectorEntry{}, errors.New("population entry unavailable")
	}
	nounIndex, err := game.RosterIndex(entries, s.Random(), s.populationRules.RosterSelection == "local_and_external", s.populationRules.ExternalRosterWeightPercent)
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
	// First-time population must retain the authored roster shown in the
	// mission preview. Stage 13 explicitly includes both Scarabs and Robo-Bombers;
	// the legacy server replacement must not collapse those two archetypes.
	if director.IsFirstTimeRosterSelected {
		return plans, nil
	}
	if chainLevelIndex != initialInfinityChainLevelIndex ||
		!strings.EqualFold(director.Level, "infinity_2") {
		return plans, nil
	}
	isReplacementNeeded := false
	for _, plan := range plans {
		if isExploderScarabNoun(plan.NounName) {
			isReplacementNeeded = true
			break
		}
	}
	if !isReplacementNeeded {
		return plans, nil
	}
	// This fixed campaign replacement is independent of the selected roster.
	// Robo-Bomber is authored as a first-time minion, not an agent, and normal
	// roster composition can discard that pool entirely. Resolve its profile
	// from the complete noun catalog retained by the director instead.
	replacementProfile, isFound := director.NPCProfilesByNoun[strings.ToLower(roboBomberNounName)]
	if !isFound || !replacementProfile.IsKnown || replacementProfile.HitPoint <= 0 {
		return nil, errors.New("4-1 Robo-Bomber unavailable")
	}
	for index := range plans {
		if !isExploderScarabNoun(plans[index].NounName) {
			continue
		}
		plans[index].NounName = roboBomberNounName
		plans[index].AuthoredNounName = ""
		plans[index].NPCProfile = replacementProfile
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

// maximumSpawnCount reserves capacity without consuming composition draws.
// Object-ID limits are checked against each group's actual accepted members.
func maximumSpawnCount(decisions []Decision) (int, error) {
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
			if decision.Challenge != 0 {
				requestedCount += maximumGroupMemberCount
			}
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
) error {
	if len(minionEntries) == 0 {
		return errors.New("spawnPlanMinion: empty pool")
	}
	err := validateEntryProfiles("spawnPlanMinionProfile", minionEntries)
	if err != nil {
		return fmt.Errorf("spawnMinion: %w", err)
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
	isCaptainNeeded := decision.IsProvisionalCaptain && len(decision.ProvisionalNounNames) == 0
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
		if entry.NPCProfile.ChallengeValue < 0 {
			return fmt.Errorf("%s[%s]: negative challenge", errorName, entry.NounName)
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
		// A special-roster enemy is an archetype, not an elite rank. The
		// provisional encounter role must not add captain stats or affixes.
		isCaptain := strings.EqualFold(configKind, "captain")
		profile := selectedEntry.NPCProfile
		bossIdentity := zonenpc.BossIdentity{}
		if isCaptain {
			bossIdentity = CaptainIdentity(director, selectedEntry.NounName)
			profile = zonenpc.ApplyCaptainProfile(profile, bossIdentity)
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

// CaptainIdentity resolves the authored captain affixes for encounter and
// developer spawns through the same content lookup.
func CaptainIdentity(
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
		// Budgeted spikes select their entries through composeSpikeGroup.
		return 0, 0, nil
	default:
		return 0, 0, fmt.Errorf("spawnPlanKind: %d", decision.Kind)
	}
}

func PoolEntries(
	director game.CampaignDirector, configKind string,
) []game.CampaignDirectorEntry {
	var selectedEntries []game.CampaignDirectorEntry
	for _, pool := range director.Pools {
		if !strings.EqualFold(pool.ConfigKind, configKind) {
			continue
		}
		if strings.EqualFold(pool.ConfigurationName, "firstTimeConfig") {
			if director.IsFirstTimeRosterSelected {
				return pool.Entries
			}
			continue
		}
		if selectedEntries == nil || strings.EqualFold(pool.ConfigurationName, "levelConfig") {
			selectedEntries = pool.Entries
		}
	}
	if director.IsFirstTimeRosterSelected &&
		(strings.EqualFold(configKind, "minion") || strings.EqualFold(configKind, "special")) {
		return nil
	}
	return selectedEntries
}

// HordeEntries uses the mission's minion roster for horde and boss-add
// markers when no separate agent pool is authored.
func HordeEntries(director game.CampaignDirector) []game.CampaignDirectorEntry {
	entries := PoolEntries(director, "agent")
	if len(entries) != 0 {
		return entries
	}
	if !strings.EqualFold(director.Level, game.InitialChainLevel) {
		return PoolEntries(director, "minion")
	}
	for _, pool := range director.Pools {
		if strings.EqualFold(pool.ConfigurationName, "firstTimeConfig") &&
			strings.EqualFold(pool.ConfigKind, "minion") {
			return pool.Entries
		}
	}
	return PoolEntries(director, "minion")
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
