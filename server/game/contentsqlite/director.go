// Package contentsqlite adapts immutable SQLite content to game feature ports.
package contentsqlite

import (
	"context"
	"errors"
	"fmt"
	"strings"

	contentsqlite "github.com/darkspinnet/darkspin/content/sqlite"
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/util"
)

// DirectorSource loads campaign director inputs from content.db.
type DirectorSource struct {
	store levelDirectorStore
}

type levelDirectorStore interface {
	LevelDirector(context.Context, string) (contentsqlite.LevelDirector, error)
	NonPlayerNounProfiles(context.Context) ([]contentsqlite.NonPlayerNounProfile, error)
	NPCAffixes(context.Context) ([]contentsqlite.NPCAffix, error)
	EliteStageTunings(context.Context) ([]contentsqlite.EliteStageTuning, error)
	ElitePromotionTuning(context.Context) (contentsqlite.ElitePromotionTuning, error)
	AttributeMirrorTuning(context.Context) (contentsqlite.AttributeMirrorTuning, error)
	NounNavigationEntries(context.Context) ([]contentsqlite.NounNavigation, error)
	NounFootprints(context.Context) ([]contentsqlite.NounFootprint, error)
	DirectorCompositionTuning(context.Context) (contentsqlite.DirectorCompositionTuning, error)
	DirectorTunings(context.Context) ([]contentsqlite.DirectorTuning, error)
	EquipmentDropSetting(context.Context) (contentsqlite.EquipmentDropSetting, error)
	AIAssets(context.Context) ([]contentsqlite.AIAsset, error)
	AIDefinition(context.Context, uint32) (contentsqlite.AIDefinition, error)
	AIPhase(context.Context, uint32) (contentsqlite.AIPhase, error)
	AICondition(context.Context, uint32) (contentsqlite.AICondition, error)
}

type sectionBucketStore interface {
	SectionBuckets(context.Context, uint32) ([]contentsqlite.SectionBucket, error)
}

// build103CampaignNPCProfile contains physical profiles recovered from the
// shipped build for campaign nouns whose noun-physics link is not yet imported
// into content.db. Its complete rows remain an emergency fallback, but imported
// ClassAttributes always supply combat statistics when available.
var build103CampaignNPCProfile = map[string]contentsqlite.NonPlayerNounProfile{
	"dest_prefab_cryos_plants_shascope.noun": {
		NounName: game.CryosFungusNoun, HitPoint: 5,
		GraphicsScale: 1, FootprintRadius: 1, IsTargetable: true,
	},
	"dest_prefab_islands_instrument_scitech_7.noun": {
		NounName: game.GraviticStabilizerNoun, HitPoint: 20,
		// The shipped noun bounds are (-4,-5,0)..(4,5,2.5).
		// Include the corners in radial melee reach and navigation clearance.
		CriticalRating: 5, GraphicsScale: 1, FootprintRadius: 6.403125,
		IsTargetable: true,
	},
	"dest_prefab_islands_instrument_scitech_3.noun": {
		NounName: "DEST_prefab_islands_instrument_scitech_3.Noun", HitPoint: 1,
		// The shipped noun bounds are (-1.4,-1.5,0)..(1.4,1.9,5).
		CriticalRating: 5, GraphicsScale: 1, FootprintRadius: 2.36,
		IsTargetable: true,
	},
	"dest_tota_headstatue_b.noun": {
		NounName: "DEST_tota_headstatue_b.Noun", HitPoint: 10,
		GraphicsScale: 1, FootprintRadius: 1.25, IsTargetable: true,
	},
	"dest_tota_headstatue_c.noun": {
		NounName: "DEST_tota_headstatue_c.Noun", HitPoint: 10,
		GraphicsScale: 1, FootprintRadius: 1.25, IsTargetable: true,
	},
	"dest_prefab_islands_instrument_scitech_11.noun": {
		NounName: "DEST_prefab_islands_instrument_scitech_11.Noun", HitPoint: 1,
		// The shipped noun bounds are (-1,-1.25,0)..(1,1.35,5).
		CriticalRating: 5, GraphicsScale: 1, FootprintRadius: 1.68,
		IsTargetable: true,
	},
	"zelembasicranged.noun": {
		NounName: "ZelemBasicRanged.Noun", HitPoint: 20, PowerPoint: 75,
		Strength: 10, Dexterity: 10, Mind: 10, DodgeRating: 60,
		ResistRating: 60, CriticalRating: 45, GraphicsScale: 1.30, FootprintRadius: 0.650,
	},
	"zelembasichybrid.noun": {
		NounName: "ZelemBasicHybrid.Noun", HitPoint: 22, PowerPoint: 75,
		Strength: 10, Dexterity: 10, Mind: 10, DodgeRating: 60,
		ResistRating: 60, CriticalRating: 45, GraphicsScale: 1.25, FootprintRadius: 0.625,
	},
	"zelembasicrepair.noun": {
		NounName: "ZelemBasicRepair.Noun", HitPoint: 18, PowerPoint: 75,
		Strength: 10, Dexterity: 10, Mind: 10, DodgeRating: 60,
		ResistRating: 60, CriticalRating: 45, GraphicsScale: 1.10, FootprintRadius: 0.550,
	},
	"zelemspecialhaster.noun": {
		NounName: "ZelemSpecialHaster.Noun", HitPoint: 100, PowerPoint: 75,
		Strength: 10, Dexterity: 10, Mind: 10, DodgeRating: 60,
		ResistRating: 60, CriticalRating: 45, GraphicsScale: 1.85, FootprintRadius: 0.925,
	},
	"nomadsnipe.noun": {
		NounName: "NomadSnipe.Noun", HitPoint: 60, PowerPoint: 100,
		Strength: 10, Dexterity: 10, Mind: 10, DodgeRating: 60,
		ResistRating: 60, CriticalRating: 45, GraphicsScale: 1.85, FootprintRadius: 0.925,
	},
	"nomadwithdrone.noun": {
		NounName: "NomadWithDrone.Noun", HitPoint: 110, PowerPoint: 100,
		Strength: 10, Dexterity: 10, Mind: 10, DodgeRating: 60,
		ResistRating: 60, CriticalRating: 45, GraphicsScale: 1.90, FootprintRadius: 0.950,
	},
	"zelembasicmelee.noun": {
		NounName: "ZelemBasicMelee.Noun", HitPoint: 22, PowerPoint: 75,
		Strength: 10, Dexterity: 10, Mind: 10, DodgeRating: 60,
		ResistRating: 60, CriticalRating: 45, GraphicsScale: 1, FootprintRadius: 0.50,
	},
	"zelembasicrangedhoming.noun": {
		NounName: "ZelemBasicRangedHoming.Noun", HitPoint: 22, PowerPoint: 75,
		Strength: 10, Dexterity: 10, Mind: 10, DodgeRating: 60,
		ResistRating: 60, CriticalRating: 45, GraphicsScale: 1.4, FootprintRadius: 0.70,
	},
	"verdanthbasicplunge.noun": {
		NounName: "VerdanthBasicPlunge.Noun", HitPoint: 20, PowerPoint: 75,
		Strength: 10, Dexterity: 10, Mind: 10, DodgeRating: 60,
		ResistRating: 60, CriticalRating: 45, GraphicsScale: 1.05, FootprintRadius: 0.525,
	},
	"zelemspecialone.noun": {
		NounName: "ZelemSpecialOne.Noun", HitPoint: 110, PowerPoint: 75,
		Strength: 10, Dexterity: 10, Mind: 10, DodgeRating: 60,
		ResistRating: 60, CriticalRating: 45, GraphicsScale: 2.6, FootprintRadius: 1.30,
	},
	"zelemspecialtwo.noun": {
		NounName: "ZelemSpecialTwo.Noun", HitPoint: 90, PowerPoint: 75,
		Strength: 10, Dexterity: 10, Mind: 10, DodgeRating: 60,
		ResistRating: 60, CriticalRating: 45, GraphicsScale: 2.4, FootprintRadius: 1.20,
	},
	"nomadspecialthree.noun": {
		NounName: "NomadSpecialThree.Noun", HitPoint: 100, PowerPoint: 100,
		Strength: 10, Dexterity: 10, Mind: 10, DodgeRating: 60,
		ResistRating: 60, CriticalRating: 45, GraphicsScale: 2.7, FootprintRadius: 1.35,
	},
}

func NewDirectorSource(store levelDirectorStore) (*DirectorSource, error) {
	if store == nil {
		return nil, errors.New("create director source: nil store")
	}
	return &DirectorSource{store: store}, nil
}

func (s *DirectorSource) LoadCampaignDirector(
	ctx context.Context, levelName string, stage uint32,
) (game.CampaignDirector, error) {
	director, err := s.store.LevelDirector(ctx, levelName)
	if err != nil {
		return game.CampaignDirector{}, fmt.Errorf("directorRead: %w", err)
	}
	compositionTuning, err := s.store.DirectorCompositionTuning(ctx)
	if err != nil {
		return game.CampaignDirector{}, fmt.Errorf("directorComposition: %w", err)
	}
	mirrorTuning, err := s.store.AttributeMirrorTuning(ctx)
	if err != nil {
		return game.CampaignDirector{}, fmt.Errorf("directorMirror: %w", err)
	}
	dnaTuning, err := loadDNADropTuning(ctx, s.store)
	if err != nil {
		return game.CampaignDirector{}, fmt.Errorf("directorDNA: %w", err)
	}
	tunings, err := s.store.DirectorTunings(ctx)
	if err != nil {
		return game.CampaignDirector{}, fmt.Errorf("directorOrbScales: %w", err)
	}
	equipmentSetting, err := s.store.EquipmentDropSetting(ctx)
	if err != nil {
		return game.CampaignDirector{}, fmt.Errorf("directorEquipmentGate: %w", err)
	}
	if equipmentSetting.SourceResourceID == nil {
		return game.CampaignDirector{}, errors.New("equipment gate source resource missing")
	}
	// An absent property follows the native false lookup. A missing import
	// resource is handled above rather than silently enabling equipment.
	isEquipmentDropEnabled := equipmentSetting.IsEquipmentDropEnabled != nil &&
		*equipmentSetting.IsEquipmentDropEnabled
	orbDifficultyScales := make([]float32, len(tunings))
	for index, tuning := range tunings {
		if tuning.Difficulty != index+1 {
			return game.CampaignDirector{}, fmt.Errorf("orbScaleStage[%d]: %d", index, tuning.Difficulty)
		}
		orbDifficultyScales[index] = tuning.OrbDifficultyScale
	}
	eliteTunings, err := s.store.EliteStageTunings(ctx)
	if err != nil {
		return game.CampaignDirector{}, fmt.Errorf("directorEliteStages: %w", err)
	}
	elitePromotion, err := s.store.ElitePromotionTuning(ctx)
	if err != nil {
		return game.CampaignDirector{}, fmt.Errorf("directorElitePromotion: %w", err)
	}
	profiles, err := s.store.NonPlayerNounProfiles(ctx)
	if err != nil {
		return game.CampaignDirector{}, fmt.Errorf("directorProfiles: %w", err)
	}
	graphsByID, err := campaignAIGraphs(ctx, s.store)
	if err != nil {
		return game.CampaignDirector{}, fmt.Errorf("directorGraphs: %w", err)
	}
	affixes, err := s.store.NPCAffixes(ctx)
	if err != nil {
		return game.CampaignDirector{}, fmt.Errorf("directorAffixes: %w", err)
	}
	affixesByName := make(map[string]contentsqlite.NPCAffix, len(affixes))
	for _, affix := range affixes {
		affixKey := strings.ToLower(affix.AssetName)
		if _, isFound := affixesByName[affixKey]; isFound {
			return game.CampaignDirector{}, fmt.Errorf("directorAffixDuplicate: %q", affix.AssetName)
		}
		affixesByName[affixKey] = affix
	}
	nouns, err := s.store.NounNavigationEntries(ctx)
	if err != nil {
		return game.CampaignDirector{}, fmt.Errorf("directorNounTypes: %w", err)
	}
	footprints, footprintErr := s.store.NounFootprints(ctx)
	if footprintErr != nil {
		return game.CampaignDirector{}, fmt.Errorf("directorFootprints: %w", footprintErr)
	}
	nounFootprintsByNoun := make(map[uint32]game.NavigationFootprint, len(footprints))
	nounFootprintsByInstance := make(map[uint32]game.NavigationFootprint, len(footprints))
	for _, footprint := range footprints {
		nounFootprintsByInstance[footprint.InstanceID] = game.NavigationFootprint{
			SizeClass: footprint.SizeClass,
			Extents:   game.Vec3{X: footprint.ExtentX, Y: footprint.ExtentY, Z: footprint.ExtentZ},
			Minimum:   game.Vec3{X: footprint.MinimumX, Y: footprint.MinimumY, Z: footprint.MinimumZ},
			Maximum:   game.Vec3{X: footprint.MaximumX, Y: footprint.MaximumY, Z: footprint.MaximumZ},
		}
		if footprint.NounName != "" {
			nounFootprintsByNoun[util.HashID(footprint.NounName)] = nounFootprintsByInstance[footprint.InstanceID]
		}
	}
	nounProjectilesByInstance := make(map[uint32]bool, len(nouns))
	staticBlockersByInstance := make(map[uint32]bool)
	nounTypesByInstance := make(map[uint32]game.NounType, len(nouns))
	nounInteractablesByID := make(map[uint32]*game.CampaignInteractableDefinition, len(nouns))
	for _, noun := range nouns {
		if noun.InstanceID > uint64(^uint32(0)) {
			return game.CampaignDirector{}, fmt.Errorf("nounTypeID[%d]: out of range", noun.ResourceID)
		}
		nounID := uint32(noun.InstanceID)
		// Build 103 sub_9E7620 admits fixed nonzero-physics objects with
		// collision enabled. isDynamicWall changes its blockage mask; it does
		// not exclude the object. Authored PHYS design boxes use this flag.
		// Doors, switches and combatants require their own collision lifecycle.
		if noun.IsFixed && noun.PhysicsType != 0 && !noun.IsDoor && !noun.IsSwitch &&
			!noun.IsPressureSwitch && noun.LocomotionTuning == nil &&
			!noun.IsCombatantComponentPresent {
			staticBlockersByInstance[nounID] = true
		}
		nounType := game.NounType(noun.NounType)
		previousType, isFound := nounTypesByInstance[nounID]
		if isFound && previousType != nounType {
			return game.CampaignDirector{}, fmt.Errorf("nounTypeDuplicate[%#x]: conflicting category", nounID)
		}
		nounTypesByInstance[nounID] = nounType
		if noun.IsProjectilePresent != nil {
			nounProjectilesByInstance[nounID] = *noun.IsProjectilePresent
		}
		nounInteractablesByID[nounID] = campaignInteractable(noun.Interactable)
	}
	profileByNoun := make(map[string]contentsqlite.NonPlayerNounProfile, len(profiles))
	for _, profile := range profiles {
		profileByNoun[strings.ToLower(profile.NounName)] = profile
	}
	for nounName, profile := range build103CampaignNPCProfile {
		authoredProfile, isProfileFound := profileByNoun[nounName]
		if !isProfileFound {
			profileByNoun[nounName] = profile
			continue
		}
		// The ClassAttributes instance is keyed by the noun basename even
		// when the noun-physics link is absent. Preserve those exact combat
		// stats while supplying the separately recovered physical profile.
		authoredProfile.GraphicsScale = profile.GraphicsScale
		authoredProfile.FootprintRadius = profile.FootprintRadius
		profileByNoun[nounName] = authoredProfile
	}
	result := game.CampaignDirector{
		AttributeMirrorTuning: game.AttributeMirrorTuning{
			GlobalBaseMaximums:          mirrorTuning.GlobalBaseMaximums,
			GlobalMaximums:              mirrorTuning.GlobalMaximums,
			HealthPerStrength:           mirrorTuning.HealthPerStrength,
			ManaPerMind:                 mirrorTuning.ManaPerMind,
			PhysicalDefensePerDexterity: mirrorTuning.PhysicalDefensePerDexterity,
			EnergyDefensePerMind:        mirrorTuning.EnergyDefensePerMind,
			CriticalPerDexterity:        mirrorTuning.CriticalPerDexterity, IsKnown: true,
		},
		EliteMinionMinimumStage: elitePromotion.MinionMinimumStage, EliteSpecialMinimumStage: elitePromotion.SpecialMinimumStage,
		EliteAffixesByNoun:        make(map[string][]game.CampaignEliteAffix),
		IsEquipmentDropEnabled:    isEquipmentDropEnabled,
		NounFootprintsByNoun:      nounFootprintsByNoun,
		NounFootprintsByInstance:  nounFootprintsByInstance,
		NounTypesByInstance:       nounTypesByInstance,
		NounProjectilesByInstance: nounProjectilesByInstance,
		StaticBlockersByInstance:  staticBlockersByInstance,
		OrbDifficultyScales:       orbDifficultyScales,
		DNADropTuning:             dnaTuning,
		CompositionTuning: game.CampaignCompositionTuning{
			GroupChallengeMultiplier: compositionTuning.GroupChallengeMultiplier,
		},
		PickupTuning: game.CampaignPickupTuning{
			ResurrectionHealthFraction: compositionTuning.ResurrectionPickup.HealthFraction,
		},
		Level: director.Name,
		LevelCatalogAsset: game.CampaignAssetIdentity{
			Ordinal: director.CatalogOrdinal, AssetName: director.CatalogAssetName,
			SourceName: director.CatalogSourceName,
		},
		PlanetConfigName:  director.PlanetConfigName,
		PrimaryType:       director.PrimaryType,
		SecondaryType:     director.SecondaryType,
		TertiaryType:      director.TertiaryType,
		QuaternaryType:    director.QuaternaryType,
		EntryPositions:    make([]game.Vec3, 0, len(director.EntryPositions)),
		Pools:             make([]game.CampaignDirectorPool, 0, len(director.Pools)),
		MarkerSets:        make([]game.CampaignDirectorMarkerSet, 0, len(director.MarkerSets)),
		Scripts:           make([]game.CampaignScriptBinding, 0, len(director.Scripts)),
		NPCProfilesByNoun: make(map[string]game.CampaignNPCProfile, len(profileByNoun)),
		NPCIdentitiesByNoun: make(
			map[string]game.CampaignNPCIdentity, len(profileByNoun),
		),
	}
	for _, tuning := range eliteTunings {
		result.EliteTunings = append(result.EliteTunings, game.CampaignEliteTuning{Stage: tuning.Stage, MinimumAffixCount: tuning.MinimumAffixCount, MaximumAffixCount: tuning.MaximumAffixCount, Chance: tuning.Chance, SpecialMinimumAffixCount: tuning.SpecialMinimumAffixCount, SpecialMaximumAffixCount: tuning.SpecialMaximumAffixCount, SpecialChance: tuning.SpecialChance})
	}
	if bucketStore, isAvailable := s.store.(sectionBucketStore); isAvailable {
		chapter := (stage-1)/4 + 1
		buckets, bucketErr := bucketStore.SectionBuckets(ctx, chapter)
		if bucketErr != nil {
			return game.CampaignDirector{}, fmt.Errorf("directorSectionBuckets: %w", bucketErr)
		}
		for _, bucket := range buckets {
			result.SectionBuckets = append(result.SectionBuckets, game.CampaignSectionBucket{
				Ordinal: bucket.Ordinal, Difficulty: bucket.Difficulty,
				MinionCount: bucket.MinionCount, SpecialCount: bucket.SpecialCount,
				Chance: bucket.Chance,
			})
		}
	}
	if director.Camera != nil {
		result.CameraYawOverride = director.Camera.Yaw
	}
	for nounName, profile := range profileByNoun {
		for index, name := range profile.NPCAffixNames {
			affix, isFound := affixesByName[strings.ToLower(name)]
			if !isFound || affix.ModifierName == nil || *affix.ModifierName == "" {
				continue
			}
			minimum, maximum := profile.NPCAffixMinimumDifficulties[index], profile.NPCAffixMaximumDifficulties[index]
			if !profile.AreNPCAffixDifficultiesKnown {
				minimum, maximum = 1, 72
			}
			parentName, childName := "", ""
			if affix.ParentName != nil {
				parentName = *affix.ParentName
			}
			if affix.ChildName != nil {
				childName = *affix.ChildName
			}
			candidate := game.CampaignEliteAffix{Name: name, ParentName: parentName, ChildName: childName, ModifierName: *affix.ModifierName, ModifierID: affix.ModifierHash, MinimumDifficulty: minimum, MaximumDifficulty: maximum}
			result.EliteAffixesByNoun[nounName] = append(result.EliteAffixesByNoun[nounName], candidate)
			// Build 103 supplies these lists only on captain classes. Share the
			// authored candidates and intervals for the opt-in ambient policy;
			// do not mistake an ordinary class's empty list for no eligible affix.
			result.AmbientEliteAffixes = append(result.AmbientEliteAffixes, candidate)
		}
		result.NPCProfilesByNoun[nounName] = game.CampaignNPCProfile{
			NPCClassInstanceID: profile.NPCClassInstanceID, ClassAttributeBases: profile.ClassAttributeBases, AreClassAttributeBasesKnown: profile.IsClassKnown,
			ActorFootprint: result.ActorFootprintForAsset(nounName),
			AIGraph:        campaignAIGraph(profile.AIDefinitionInstanceID, graphsByID),
			NounType:       result.NounTypeForAsset(nounName),
			AggroType:      profile.AggroType,
			ChallengeValue: profile.ChallengeValue, NPCRank: profile.NPCRank,
			NPCType: profile.NPCType, CreatureType: profile.CreatureType,
			DropTypes:    profile.DropTypes,
			IsTargetable: profile.IsTargetable, IsPlayerPet: profile.IsPlayerPet, IsClassKnown: profile.IsClassKnown,
			PlayerCountHealthScale: profile.PlayerCountHealthScale,
			AggroRange:             profile.AggroRange, AlertRange: profile.AlertRange,
			DropAggroRange:    profile.DropAggroRange,
			IdleMovementSpeed: profile.IdleMovementSpeed,
			BaseCombatSpeed:   profile.BaseCombatSpeed,
			HitPoint:          profile.HitPoint, PowerPoint: profile.PowerPoint,
			Strength: profile.Strength, Dexterity: profile.Dexterity, Mind: profile.Mind,
			DodgeRating: profile.DodgeRating, ResistRating: profile.ResistRating,
			CriticalRating: profile.CriticalRating, GraphicsScale: profile.GraphicsScale,
			FootprintRadius: profile.FootprintRadius, IsKnown: true,
		}
		if strings.TrimSpace(profile.DisplayName) != "" {
			identity, identityErr := campaignNPCIdentityAtStage(profile, affixesByName, stage)
			if identityErr != nil {
				return game.CampaignDirector{}, fmt.Errorf("directorIdentity[%s]: %w", nounName, identityErr)
			}
			result.NPCIdentitiesByNoun[nounName] = identity
		}
	}
	for _, position := range director.EntryPositions {
		result.EntryPositions = append(result.EntryPositions, game.Vec3{
			X: position[0], Y: position[1], Z: position[2],
		})
	}
	pools := make([]contentsqlite.LevelDirectorPool, 0, len(director.Pools)+len(director.ExternalPools))
	pools = append(pools, director.Pools...)
	pools = append(pools, director.ExternalPools...)
	for poolIndex, pool := range pools {
		configKind := resolvedConfigurationKind(
			pool.ConfigKind, pool.ConfigurationOrdinal,
		)
		mapped := game.CampaignDirectorPool{
			ConfigurationOrdinal: pool.ConfigurationOrdinal,
			ConfigurationName:    pool.ConfigurationName,
			ConfigKind:           configKind,
			SpawnKind:            pool.SpawnKind,
			Entries:              make([]game.CampaignDirectorEntry, 0, len(pool.Entries)),
		}
		for _, entry := range pool.Entries {
			nounKey := strings.ToLower(entry.NounName)
			profile, isProfileFound := profileByNoun[nounKey]
			if !isProfileFound {
				profile = fallbackCampaignNPCProfile(entry.NounName, configKind, director.Name)
				isProfileFound = true
			} else if profile.HitPoint <= 0 {
				fallbackProfile := fallbackCampaignNPCProfile(
					entry.NounName, configKind, director.Name,
				)
				parentKey, isParentNamed := inheritedCaptainNounKey(entry.NounName)
				parentProfile, isParentFound := profileByNoun[parentKey]
				if isParentNamed && isParentFound && parentProfile.HitPoint > 0 {
					profile = inheritCampaignCombatProfile(
						fallbackProfile, parentProfile, profile,
					)
				} else {
					profile = retainCampaignClassMetadata(fallbackProfile, profile)
				}
			} else if !profile.IsClassKnown && profile.ChallengeValue == 0 {
				_, isCompatibilityProfile := build103CampaignNPCProfile[nounKey]
				if isCompatibilityProfile {
					profile.ChallengeValue = fallbackCampaignNPCProfile(
						entry.NounName, configKind, director.Name,
					).ChallengeValue
				}
			}
			mapped.Entries = append(mapped.Entries, game.CampaignDirectorEntry{
				IsExternalSource: poolIndex >= len(director.Pools),
				Ordinal:          entry.Ordinal, ConfigurationEntryOrdinal: entry.ConfigurationEntryOrdinal,
				NounName: entry.NounName, MinimumDifficulty: entry.MinimumDifficulty,
				MaximumDifficulty: entry.MaximumDifficulty, IsHordeLegal: entry.IsHordeLegal,
				NPCProfile: game.CampaignNPCProfile{
					NPCClassInstanceID: profile.NPCClassInstanceID, ClassAttributeBases: profile.ClassAttributeBases, AreClassAttributeBasesKnown: profile.IsClassKnown,
					ActorFootprint: result.ActorFootprintForAsset(entry.NounName),
					AIGraph:        campaignAIGraph(profile.AIDefinitionInstanceID, graphsByID),
					NounType:       result.NounTypeForAsset(entry.NounName),
					AggroType:      profile.AggroType,
					ChallengeValue: profile.ChallengeValue, NPCRank: profile.NPCRank,
					NPCType: profile.NPCType, CreatureType: profile.CreatureType,
					DropTypes:    profile.DropTypes,
					IsTargetable: profile.IsTargetable, IsPlayerPet: profile.IsPlayerPet, IsClassKnown: profile.IsClassKnown,
					PlayerCountHealthScale: profile.PlayerCountHealthScale,
					AggroRange:             profile.AggroRange, AlertRange: profile.AlertRange,
					DropAggroRange:    profile.DropAggroRange,
					IdleMovementSpeed: profile.IdleMovementSpeed,
					BaseCombatSpeed:   profile.BaseCombatSpeed,
					HitPoint:          profile.HitPoint, PowerPoint: profile.PowerPoint,
					Strength: profile.Strength, Dexterity: profile.Dexterity, Mind: profile.Mind,
					DodgeRating: profile.DodgeRating, ResistRating: profile.ResistRating,
					CriticalRating: profile.CriticalRating, GraphicsScale: profile.GraphicsScale,
					FootprintRadius: profile.FootprintRadius, IsKnown: isProfileFound,
				},
			})
		}
		if poolIndex < len(director.Pools) {
			result.Pools = append(result.Pools, mapped)
		} else {
			result.ExternalPools = append(result.ExternalPools, mapped)
		}
	}
	for _, markerSet := range director.MarkerSets {
		mapped := game.CampaignDirectorMarkerSet{
			Ordinal: markerSet.Ordinal, Name: markerSet.Name,
			CatalogAsset: game.CampaignAssetIdentity{
				Ordinal: markerSet.CatalogOrdinal, AssetName: markerSet.CatalogAssetName,
				SourceName: markerSet.CatalogSourceName,
			},
			GroupName: markerSet.GroupName, Weight: markerSet.Weight,
			Conditions: markerSet.Conditions,
			Markers:    make([]game.CampaignDirectorMarker, 0, len(markerSet.Markers)),
			Triggers:   make([]game.CampaignDirectorTrigger, 0, len(markerSet.Triggers)),
		}
		for _, definition := range markerSet.Definitions {
			mapped.Definitions = append(mapped.Definitions, game.CampaignMarkerDefinition{
				Ordinal: definition.Ordinal, MarkerID: definition.MarkerID, NounName: definition.NounName,
				Position: game.Vec3{X: definition.PositionX, Y: definition.PositionY, Z: definition.PositionZ},
				Rotation: game.Vec3{X: definition.RotationX, Y: definition.RotationY, Z: definition.RotationZ},
				Scale:    definition.Scale, IsVisible: definition.IsVisible,
				IsCollisionEnabled:      definition.IsCollisionEnabled,
				TeleporterTriggerRadius: definition.TeleporterTriggerRadius,
				Teleporter:              campaignTeleporter(definition.Teleporter),
			})
		}
		for _, marker := range markerSet.Markers {
			nounStem := strings.TrimSuffix(strings.ToLower(marker.NounName), ".noun")
			mappedMarker := game.CampaignDirectorMarker{
				NounType:         result.NounTypeForAsset(marker.NounName),
				SpawnTrigger:     campaignSpawnTrigger(marker.SpawnTrigger),
				EventListener:    campaignEventListener(marker.EventListener),
				Interactable:     campaignInteractable(marker.Interactable),
				NounInteractable: nounInteractablesByID[util.HashID(nounStem)],
				Combatant:        campaignCombatant(marker.Combatant),
				Ordinal:          marker.Ordinal, MarkerID: marker.MarkerID, MarkerSetName: markerSet.Name,
				Name:     marker.Name,
				NounName: marker.NounName, SpawnKind: marker.SpawnKind, PoolKind: marker.PoolKind,
				IsSpawnKindKnown:    marker.IsSpawnKindKnown,
				SpawnSectionType:    marker.SpawnSectionType,
				IsSpawnSectionKnown: marker.IsSpawnSectionKnown,
				IsSpikeActive:       marker.IsSpikeActive,
				Position:            game.Vec3{X: marker.PositionX, Y: marker.PositionY, Z: marker.PositionZ},
				Rotation:            game.Vec3{X: marker.RotationX, Y: marker.RotationY, Z: marker.RotationZ},
				Scale:               marker.Scale, IsVisible: marker.IsVisible,
				IsCollisionEnabled:      marker.IsCollisionEnabled,
				TargetMarkerID:          marker.TargetMarkerID,
				TeleporterTriggerRadius: marker.TeleporterTriggerRadius,
				SpatialRadius:           marker.SpatialRadius, ExclusionRadius: marker.ExclusionRadius,
				IsExclusionVolume: marker.IsExclusionVolume,
				Events:            make([]game.CampaignDirectorEvent, 0, len(marker.Events)),
			}
			nounKey := strings.ToLower(marker.NounName)
			profileKey := campaignMarkerProfileKey(nounKey)
			profile, isProfileFound := profileByNoun[profileKey]
			if !isProfileFound && marker.IsSpawnKindKnown {
				profile = fallbackCampaignNPCProfile(marker.NounName, marker.PoolKind, director.Name)
				isProfileFound = true
			} else if isProfileFound && !profile.IsClassKnown && profile.ChallengeValue == 0 {
				_, isCompatibilityProfile := build103CampaignNPCProfile[nounKey]
				if isCompatibilityProfile {
					profile.ChallengeValue = fallbackCampaignNPCProfile(
						marker.NounName, marker.PoolKind, director.Name,
					).ChallengeValue
				}
			}
			mappedMarker.NPCProfile = game.CampaignNPCProfile{
				ActorFootprint: result.ActorFootprintForAsset(marker.NounName),
				AIGraph:        campaignAIGraph(profile.AIDefinitionInstanceID, graphsByID),
				NounType:       mappedMarker.NounType,
				AggroType:      profile.AggroType,
				ChallengeValue: profile.ChallengeValue, NPCRank: profile.NPCRank,
				NPCType: profile.NPCType, CreatureType: profile.CreatureType,
				DropTypes:    profile.DropTypes,
				IsTargetable: profile.IsTargetable, IsPlayerPet: profile.IsPlayerPet, IsClassKnown: profile.IsClassKnown,
				PlayerCountHealthScale: profile.PlayerCountHealthScale,
				AggroRange:             profile.AggroRange, AlertRange: profile.AlertRange,
				DropAggroRange:    profile.DropAggroRange,
				IdleMovementSpeed: profile.IdleMovementSpeed,
				BaseCombatSpeed:   profile.BaseCombatSpeed,
				HitPoint:          profile.HitPoint, PowerPoint: profile.PowerPoint,
				Strength: profile.Strength, Dexterity: profile.Dexterity, Mind: profile.Mind,
				DodgeRating: profile.DodgeRating, ResistRating: profile.ResistRating,
				CriticalRating: profile.CriticalRating, GraphicsScale: profile.GraphicsScale,
				FootprintRadius: profile.FootprintRadius, IsKnown: isProfileFound,
			}
			for _, event := range marker.Events {
				mappedMarker.Events = append(mappedMarker.Events, game.CampaignDirectorEvent{
					EventHash: event.EventHash, NativeCallbackHash: event.NativeCallbackHash, LuaCallbackName: event.LuaCallbackName,
					Ordinal: event.Ordinal, ComponentName: event.ComponentName,
					EventKind: event.EventKind, EventSlot: event.EventSlot,
					EventName: event.EventName, CallbackName: event.CallbackName,
					TriggerRadius:     event.TriggerRadius,
					IsTriggerOnceOnly: event.IsTriggerOnceOnly, IsServerOnly: event.IsServerOnly,
				})
			}
			mapped.Markers = append(mapped.Markers, mappedMarker)
		}
		for _, trigger := range markerSet.Triggers {
			mappedTrigger := game.CampaignDirectorTrigger{
				Rotation:        game.Vec3{X: trigger.RotationX, Y: trigger.RotationY, Z: trigger.RotationZ},
				SpawnTrigger:    campaignSpawnTrigger(trigger.SpawnTrigger),
				EventListener:   campaignEventListener(trigger.EventListener),
				Interactable:    campaignInteractable(trigger.Interactable),
				Combatant:       campaignCombatant(trigger.Combatant),
				ExclusionRadius: trigger.ExclusionRadius, IsExclusionVolume: trigger.IsExclusionVolume,
				Ordinal: trigger.Ordinal, MarkerID: trigger.MarkerID, Name: trigger.Name,
				NounName: trigger.NounName,
				Position: game.Vec3{X: trigger.PositionX, Y: trigger.PositionY, Z: trigger.PositionZ},
				Events:   make([]game.CampaignDirectorEvent, 0, len(trigger.Events)),
			}
			for _, event := range trigger.Events {
				mappedTrigger.Events = append(mappedTrigger.Events, game.CampaignDirectorEvent{
					EventHash: event.EventHash, NativeCallbackHash: event.NativeCallbackHash, LuaCallbackName: event.LuaCallbackName,
					Ordinal: event.Ordinal, ComponentName: event.ComponentName,
					EventKind: event.EventKind, EventSlot: event.EventSlot,
					EventName: campaignTriggerEventName(trigger.SpawnTrigger, event), CallbackName: event.CallbackName,
					TriggerRadius:     event.TriggerRadius,
					IsTriggerOnceOnly: event.IsTriggerOnceOnly, IsServerOnly: event.IsServerOnly,
				})
			}
			mapped.Triggers = append(mapped.Triggers, mappedTrigger)
		}
		result.MarkerSets = append(result.MarkerSets, mapped)
	}
	for _, script := range director.Scripts {
		nounStem := strings.TrimSuffix(strings.ToLower(script.NounName), ".noun")
		result.Scripts = append(result.Scripts, game.CampaignScriptBinding{
			Interactable:     campaignInteractable(script.Interactable),
			NounInteractable: nounInteractablesByID[util.HashID(nounStem)],
			MarkerSetOrdinal: script.MarkerSetOrdinal, MarkerSetName: script.MarkerSetName,
			MarkerSetWeight: script.MarkerSetWeight, MarkerOrdinal: script.MarkerOrdinal,
			MarkerID: script.MarkerID, MarkerName: script.MarkerName, NounName: script.NounName,
			Position: game.Vec3{X: script.PositionX, Y: script.PositionY, Z: script.PositionZ},
			Rotation: game.Vec3{X: script.RotationX, Y: script.RotationY, Z: script.RotationZ},
			Scale:    script.Scale, IsVisible: script.IsVisible,
			IsCollisionEnabled:    script.IsCollisionEnabled,
			InteractableAbility:   script.InteractableAbility,
			InteractableUseLimit:  script.InteractableUseLimit,
			InteractableChallenge: script.InteractableChallenge,
			EventOrdinal:          script.EventOrdinal, EventName: script.EventName,
			CallbackName: script.CallbackName, LuaChunkID: script.LuaChunkID,
			LuaSourceName: script.LuaSourceName, LuaSHA256: script.LuaSHA256,
		})
	}
	return result, nil
}

// campaignMarkerProfileKey preserves the placed noun identity while resolving
// authored variants whose packaged class references the base attributes.
func campaignMarkerProfileKey(nounKey string) string {
	switch nounKey {
	case "dest_prefab_islands_instrument_scitech_11_noshadow.noun":
		return "dest_prefab_islands_instrument_scitech_11.noun"
	case "dest_prefab_islands_instrument_scitech_7_noshadow.noun":
		return "dest_prefab_islands_instrument_scitech_7.noun"
	case "dest_prefab_tota_heroplant_p3_b.noun":
		// Its own NonPlayerClass references DEST_tota_HeroPlant_P3_b.ClassAttributes.
		return "dest_tota_heroplant_p3_b.noun"
	case "tutorialbasicpoisonnoorbs.noun":
		return "tutorialbasicpoison.noun"
	case "tutorialspecialone_intro.noun":
		return "tutorialspecialone.noun"
	default:
		return nounKey
	}
}

func fallbackCampaignNPCProfile(
	nounName string, configKind string, levelName string,
) contentsqlite.NonPlayerNounProfile {
	profile := contentsqlite.NonPlayerNounProfile{
		NounName: nounName, HitPoint: 24, PowerPoint: 75,
		Strength: 10, Dexterity: 10, Mind: 10,
		DodgeRating: 60, ResistRating: 60, CriticalRating: 45,
		GraphicsScale: 1, FootprintRadius: 0.5,
	}
	profile.ChallengeValue = game.CampaignFallbackChallenge(levelName)
	switch strings.ToLower(configKind) {
	case "captain":
		profile.HitPoint = 100
		profile.GraphicsScale = 1.8
		profile.FootprintRadius = 0.9
	case "special", "boss":
		profile.HitPoint = 250
		profile.PowerPoint = 100
		profile.GraphicsScale = 2.3
		profile.FootprintRadius = 1.15
	}
	return profile
}

func retainCampaignClassMetadata(
	profile contentsqlite.NonPlayerNounProfile,
	authored contentsqlite.NonPlayerNounProfile,
) contentsqlite.NonPlayerNounProfile {
	if authored.IsClassKnown || authored.ChallengeValue > 0 {
		profile.ChallengeValue = authored.ChallengeValue
	}
	profile.NPCRank = authored.NPCRank
	profile.AIDefinitionInstanceID = authored.AIDefinitionInstanceID
	profile.NPCClassInstanceID = authored.NPCClassInstanceID
	profile.ClassAttributeBases = authored.ClassAttributeBases
	profile.AggroType = authored.AggroType
	profile.NPCType = authored.NPCType
	profile.CreatureType = authored.CreatureType
	profile.DropTypes = authored.DropTypes
	profile.IsTargetable = authored.IsTargetable
	profile.IsPlayerPet = authored.IsPlayerPet
	profile.IsClassKnown = authored.IsClassKnown
	profile.PlayerCountHealthScale = authored.PlayerCountHealthScale
	profile.AggroRange = authored.AggroRange
	profile.AlertRange = authored.AlertRange
	profile.DropAggroRange = authored.DropAggroRange
	profile.IdleMovementSpeed = authored.IdleMovementSpeed
	profile.BaseCombatSpeed = authored.BaseCombatSpeed
	return profile
}

func inheritCampaignCombatProfile(
	profile contentsqlite.NonPlayerNounProfile,
	parent contentsqlite.NonPlayerNounProfile,
	authored contentsqlite.NonPlayerNounProfile,
) contentsqlite.NonPlayerNounProfile {
	profile.HitPoint = parent.HitPoint
	profile.PowerPoint = parent.PowerPoint
	profile.Strength = parent.Strength
	profile.Dexterity = parent.Dexterity
	profile.Mind = parent.Mind
	profile.DodgeRating = parent.DodgeRating
	profile.ResistRating = parent.ResistRating
	profile.CriticalRating = parent.CriticalRating
	return retainCampaignClassMetadata(profile, authored)
}

func inheritedCaptainNounKey(nounName string) (string, bool) {
	baseName := strings.ToLower(strings.TrimSpace(nounName))
	baseName = strings.TrimSuffix(baseName, ".noun")
	for _, rankSuffix := range []string{"_2", "_3"} {
		captainSuffix := "_captain" + rankSuffix
		if strings.HasSuffix(baseName, captainSuffix) {
			return strings.TrimSuffix(baseName, captainSuffix) + rankSuffix + ".noun", true
		}
	}
	if !strings.HasSuffix(baseName, "_captain") {
		return "", false
	}
	return strings.TrimSuffix(baseName, "_captain") + ".noun", true
}

func resolvedConfigurationKind(configKind string, configurationOrdinal int) string {
	if configKind != "" && !strings.EqualFold(configKind, "unknown") {
		return configKind
	}
	switch configurationOrdinal {
	case 0:
		return "minion"
	case 1:
		return "special"
	case 2:
		return "agent"
	case 3:
		return "captain"
	default:
		return "unknown"
	}
}
