// Package contentsqlite adapts immutable SQLite content to game feature ports.
package contentsqlite

import (
	"context"
	"errors"
	"fmt"
	"strings"

	contentsqlite "github.com/darkspinnet/darkspin/content/sqlite"
	"github.com/darkspinnet/darkspin/server/game"
)

// DirectorSource loads campaign director inputs from content.db.
type DirectorSource struct {
	store levelDirectorStore
}

type levelDirectorStore interface {
	LevelDirector(context.Context, string) (contentsqlite.LevelDirector, error)
	NonPlayerNounProfiles(context.Context) ([]contentsqlite.NonPlayerNounProfile, error)
}

// build103CampaignNPCProfile contains physical profiles recovered from the
// shipped build for campaign nouns whose noun-physics link is not yet imported
// into content.db. Its complete rows remain an emergency fallback, but imported
// ClassAttributes always supply combat statistics when available.
var build103CampaignNPCProfile = map[string]contentsqlite.NonPlayerNounProfile{
	"dest_prefab_islands_instrument_scitech_11.noun": {
		NounName: "DEST_prefab_islands_instrument_scitech_11.Noun", HitPoint: 1,
		CriticalRating: 5, GraphicsScale: 1, FootprintRadius: 1,
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
	ctx context.Context, levelName string,
) (game.CampaignDirector, error) {
	director, err := s.store.LevelDirector(ctx, levelName)
	if err != nil {
		return game.CampaignDirector{}, fmt.Errorf("directorRead: %w", err)
	}
	profiles, err := s.store.NonPlayerNounProfiles(ctx)
	if err != nil {
		return game.CampaignDirector{}, fmt.Errorf("directorProfiles: %w", err)
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
		Level:             director.Name,
		EntryPositions:    make([]game.Vec3, 0, len(director.EntryPositions)),
		Pools:             make([]game.CampaignDirectorPool, 0, len(director.Pools)),
		MarkerSets:        make([]game.CampaignDirectorMarkerSet, 0, len(director.MarkerSets)),
		Scripts:           make([]game.CampaignScriptBinding, 0, len(director.Scripts)),
		NPCProfilesByNoun: make(map[string]game.CampaignNPCProfile, len(profileByNoun)),
		NPCIdentitiesByNoun: make(
			map[string]game.CampaignNPCIdentity, len(profileByNoun),
		),
	}
	for nounName, profile := range profileByNoun {
		result.NPCProfilesByNoun[nounName] = game.CampaignNPCProfile{
			ChallengeValue: profile.ChallengeValue, NPCRank: profile.NPCRank,
			IsTargetable: profile.IsTargetable, IsPlayerPet: profile.IsPlayerPet,
			PlayerCountHealthScale: profile.PlayerCountHealthScale,
			HitPoint:               profile.HitPoint, PowerPoint: profile.PowerPoint,
			Strength: profile.Strength, Dexterity: profile.Dexterity, Mind: profile.Mind,
			DodgeRating: profile.DodgeRating, ResistRating: profile.ResistRating,
			CriticalRating: profile.CriticalRating, GraphicsScale: profile.GraphicsScale,
			FootprintRadius: profile.FootprintRadius, IsKnown: true,
		}
		if strings.TrimSpace(profile.DisplayName) != "" {
			result.NPCIdentitiesByNoun[nounName] = game.CampaignNPCIdentity{
				DisplayName:   profile.DisplayName,
				NPCAffixNames: profile.NPCAffixNames,
				IsKnown:       true,
			}
		}
	}
	for _, position := range director.EntryPositions {
		result.EntryPositions = append(result.EntryPositions, game.Vec3{
			X: position[0], Y: position[1], Z: position[2],
		})
	}
	for _, pool := range director.Pools {
		configKind := resolvedConfigurationKind(
			pool.ConfigKind, pool.ConfigurationOrdinal,
		)
		mapped := game.CampaignDirectorPool{
			ConfigurationOrdinal: pool.ConfigurationOrdinal,
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
			} else if profile.ChallengeValue == 0 {
				_, isCompatibilityProfile := build103CampaignNPCProfile[nounKey]
				if isCompatibilityProfile {
					profile.ChallengeValue = fallbackCampaignNPCProfile(
						entry.NounName, configKind, director.Name,
					).ChallengeValue
				}
			}
			mapped.Entries = append(mapped.Entries, game.CampaignDirectorEntry{
				Ordinal: entry.Ordinal, ConfigurationEntryOrdinal: entry.ConfigurationEntryOrdinal,
				NounName: entry.NounName, MinimumDifficulty: entry.MinimumDifficulty,
				MaximumDifficulty: entry.MaximumDifficulty, IsHordeLegal: entry.IsHordeLegal,
				NPCProfile: game.CampaignNPCProfile{
					ChallengeValue: profile.ChallengeValue, NPCRank: profile.NPCRank,
					IsTargetable: profile.IsTargetable, IsPlayerPet: profile.IsPlayerPet,
					PlayerCountHealthScale: profile.PlayerCountHealthScale,
					HitPoint:               profile.HitPoint, PowerPoint: profile.PowerPoint,
					Strength: profile.Strength, Dexterity: profile.Dexterity, Mind: profile.Mind,
					DodgeRating: profile.DodgeRating, ResistRating: profile.ResistRating,
					CriticalRating: profile.CriticalRating, GraphicsScale: profile.GraphicsScale,
					FootprintRadius: profile.FootprintRadius, IsKnown: isProfileFound,
				},
			})
		}
		result.Pools = append(result.Pools, mapped)
	}
	for _, markerSet := range director.MarkerSets {
		mapped := game.CampaignDirectorMarkerSet{
			Ordinal: markerSet.Ordinal, Name: markerSet.Name, Weight: markerSet.Weight,
			Markers:  make([]game.CampaignDirectorMarker, 0, len(markerSet.Markers)),
			Triggers: make([]game.CampaignDirectorTrigger, 0, len(markerSet.Triggers)),
		}
		for _, marker := range markerSet.Markers {
			mappedMarker := game.CampaignDirectorMarker{
				Ordinal: marker.Ordinal, MarkerID: marker.MarkerID, MarkerSetName: markerSet.Name,
				Name:     marker.Name,
				NounName: marker.NounName, SpawnKind: marker.SpawnKind, PoolKind: marker.PoolKind,
				IsSpawnKindKnown: marker.IsSpawnKindKnown,
				Position:         game.Vec3{X: marker.PositionX, Y: marker.PositionY, Z: marker.PositionZ},
				Rotation:         game.Vec3{X: marker.RotationX, Y: marker.RotationY, Z: marker.RotationZ},
				Scale:            marker.Scale, IsVisible: marker.IsVisible,
				IsCollisionEnabled:      marker.IsCollisionEnabled,
				TargetMarkerID:          marker.TargetMarkerID,
				TeleporterTriggerRadius: marker.TeleporterTriggerRadius,
				Events:                  make([]game.CampaignDirectorEvent, 0, len(marker.Events)),
			}
			nounKey := strings.ToLower(marker.NounName)
			profileKey := campaignMarkerProfileKey(nounKey)
			profile, isProfileFound := profileByNoun[profileKey]
			if !isProfileFound && marker.IsSpawnKindKnown {
				profile = fallbackCampaignNPCProfile(marker.NounName, marker.PoolKind, director.Name)
				isProfileFound = true
			} else if isProfileFound && profile.ChallengeValue == 0 {
				_, isCompatibilityProfile := build103CampaignNPCProfile[nounKey]
				if isCompatibilityProfile {
					profile.ChallengeValue = fallbackCampaignNPCProfile(
						marker.NounName, marker.PoolKind, director.Name,
					).ChallengeValue
				}
			}
			mappedMarker.NPCProfile = game.CampaignNPCProfile{
				ChallengeValue: profile.ChallengeValue, NPCRank: profile.NPCRank,
				IsTargetable: profile.IsTargetable, IsPlayerPet: profile.IsPlayerPet,
				PlayerCountHealthScale: profile.PlayerCountHealthScale,
				HitPoint:               profile.HitPoint, PowerPoint: profile.PowerPoint,
				Strength: profile.Strength, Dexterity: profile.Dexterity, Mind: profile.Mind,
				DodgeRating: profile.DodgeRating, ResistRating: profile.ResistRating,
				CriticalRating: profile.CriticalRating, GraphicsScale: profile.GraphicsScale,
				FootprintRadius: profile.FootprintRadius, IsKnown: isProfileFound,
			}
			for _, event := range marker.Events {
				mappedMarker.Events = append(mappedMarker.Events, game.CampaignDirectorEvent{
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
				Ordinal: trigger.Ordinal, MarkerID: trigger.MarkerID, Name: trigger.Name,
				NounName: trigger.NounName,
				Position: game.Vec3{X: trigger.PositionX, Y: trigger.PositionY, Z: trigger.PositionZ},
				Events:   make([]game.CampaignDirectorEvent, 0, len(trigger.Events)),
			}
			for _, event := range trigger.Events {
				mappedTrigger.Events = append(mappedTrigger.Events, game.CampaignDirectorEvent{
					Ordinal: event.Ordinal, ComponentName: event.ComponentName,
					EventKind: event.EventKind, EventSlot: event.EventSlot,
					EventName: event.EventName, CallbackName: event.CallbackName,
					TriggerRadius:     event.TriggerRadius,
					IsTriggerOnceOnly: event.IsTriggerOnceOnly, IsServerOnly: event.IsServerOnly,
				})
			}
			mapped.Triggers = append(mapped.Triggers, mappedTrigger)
		}
		result.MarkerSets = append(result.MarkerSets, mapped)
	}
	for _, script := range director.Scripts {
		result.Scripts = append(result.Scripts, game.CampaignScriptBinding{
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
// authored tutorial variants that share their base actor's class and physics.
func campaignMarkerProfileKey(nounKey string) string {
	switch nounKey {
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
	if authored.ChallengeValue > 0 {
		profile.ChallengeValue = authored.ChallengeValue
	}
	profile.NPCRank = authored.NPCRank
	profile.IsTargetable = authored.IsTargetable
	profile.IsPlayerPet = authored.IsPlayerPet
	profile.PlayerCountHealthScale = authored.PlayerCountHealthScale
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
