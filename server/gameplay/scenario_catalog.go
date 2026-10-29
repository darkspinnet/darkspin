//go:build scenario

package gameplay

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/scenario"
	zoneboss "github.com/darkspinnet/darkspin/server/zone/boss"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	zonepopulation "github.com/darkspinnet/darkspin/server/zone/population"
)

type scenarioNPCCatalogProvider interface {
	ScenarioNPCCatalog(context.Context, int64, uint32) (scenario.NPCCatalog, error)
}

func (e Lifecycle) ScenarioNPCCatalog(ctx context.Context, userID int64, gameID uint32) (scenario.NPCCatalog, error) {
	provider, isSupported := e.syncSnapshot.(scenarioNPCCatalogProvider)
	if !isSupported {
		return scenario.NPCCatalog{}, errors.New("NPC catalog provider unavailable")
	}
	catalog, err := provider.ScenarioNPCCatalog(ctx, userID, gameID)
	if err != nil {
		return catalog, fmt.Errorf("catalogProvider: %w", err)
	}
	return catalog, nil
}

func (e *gameplaySessionRegistry) ScenarioNPCCatalog(ctx context.Context, userID int64, gameID uint32) (scenario.NPCCatalog, error) {
	catalog := scenario.NPCCatalog{Entries: []scenario.NPCCatalogEntry{}}
	if ctx == nil || userID <= 0 || gameID == 0 {
		return catalog, errors.New("NPC catalog requires an admitted user and game")
	}
	err := e.lockScenarioSpawn(ctx)
	if err != nil {
		return catalog, fmt.Errorf("catalogLock: %w", err)
	}
	defer e.mutex.Unlock()
	peer, observation, err := e.scenarioSessionLocked(ctx, userID)
	if err != nil {
		return catalog, fmt.Errorf("catalogSession: %w", err)
	}
	if observation.Outcome != scenario.Passed || peer.binding.GameID != gameID || peer.zone == nil ||
		!peer.stage.IsDungeon() || !peer.dungeonSetup.IsCommitted() || peer.isZoneTerminal() {
		return catalog, errors.New("NPC catalog requires the current committed live dungeon")
	}
	director := peer.zone.DirectorDefinition()
	catalog.GameID = gameID
	catalog.ZoneGeneration = peer.zone.Snapshot().Generation
	catalog.Level = director.Level
	catalog.Difficulty = peer.binding.Difficulty
	nouns := make([]string, 0, len(director.NPCProfilesByNoun))
	for noun := range director.NPCProfilesByNoun {
		nouns = append(nouns, noun)
	}
	sort.Slice(nouns, func(left, right int) bool {
		return strings.ToLower(nouns[left]) < strings.ToLower(nouns[right])
	})
	for _, noun := range nouns {
		profile := director.NPCProfilesByNoun[noun]
		plan := scenarioNPCSpawnPlan(director, noun, profile)
		action, isActionRecovered := zonenpc.RecoveredActionProfileForPlan(plan)
		if !isActionRecovered {
			fallbackAction, isActionKnown := zonenpc.ActionProfileForPlan(plan)
			if isActionKnown {
				action = fallbackAction
			}
		}
		_, controlErr := zonenpc.PrepareScenarioControlledPlan(plan, "catalog")
		isStagingSupported := profile.HitPoint > 0 && isActionRecovered && controlErr == nil
		supportedAbilities := []string{}
		if isStagingSupported {
			supportedAbilities = zonenpc.ScenarioSupportedAbilityNames(plan)
		}
		isControlSupported := isStagingSupported && len(supportedAbilities) != 0
		entry := scenario.NPCCatalogEntry{Noun: noun, HitPoint: profile.HitPoint,
			Rank: profile.NPCRank, AbilityName: action.AbilityName,
			SupportedAbilityNames:  supportedAbilities,
			AutonomousAbilityNames: zonenpc.ScenarioAutonomousAbilityNames(plan),
			Family:                 scenarioNPCFamilyName(action.Family), TargetKind: scenarioNPCTargetKind(action.Family),
			Range: action.Range, MinimumRange: action.MinimumRange,
			IsActionRecovered:  isActionRecovered,
			IsControlSupported: isControlSupported,
			IsStagingSupported: isStagingSupported,
			IsFinalBoss:        zoneboss.IsFinalBossNoun(noun)}
		catalog.Entries = append(catalog.Entries, entry)
	}
	return catalog, nil
}

func scenarioNPCSpawnPlan(director game.CampaignDirector, noun string, profile game.CampaignNPCProfile) zonenpc.SpawnPlan {
	plan := zonenpc.SpawnPlan{NounName: noun, NPCProfile: profile}
	if !strings.Contains(strings.ToLower(noun), "_captain") {
		return plan
	}
	plan.IsCaptain = true
	plan.BossIdentity = zonepopulation.CaptainIdentity(director, noun)
	plan.NPCProfile = zonenpc.ApplyCaptainProfile(profile, plan.BossIdentity)
	return plan
}

func scenarioNPCTargetKind(family zonenpc.ActionFamily) string {
	switch family {
	case zonenpc.ActionZelemHaster, zonenpc.ActionResurrect:
		return "ally"
	case zonenpc.ActionUnknown:
		return "unknown"
	default:
		return "hero"
	}
}

func scenarioNPCFamilyName(family zonenpc.ActionFamily) string {
	switch family {
	case zonenpc.ActionZelemRanged:
		return "zelem_ranged"
	case zonenpc.ActionZelemHaster:
		return "zelem_haster"
	case zonenpc.ActionNomadSnipe:
		return "nomad_snipe"
	case zonenpc.ActionNomadDrone:
		return "nomad_drone"
	case zonenpc.ActionProjectile:
		return "projectile"
	case zonenpc.ActionRetainedArea:
		return "retained_area"
	case zonenpc.ActionPushPull:
		return "push_pull"
	case zonenpc.ActionChannelDrain:
		return "channel_drain"
	case zonenpc.ActionResurrect:
		return "resurrect"
	case zonenpc.ActionCharge:
		return "charge"
	case zonenpc.ActionCone:
		return "cone"
	case zonenpc.ActionLeap:
		return "leap"
	case zonenpc.ActionMelee:
		return "melee"
	case zonenpc.ActionDetonate:
		return "detonate"
	default:
		return "unknown"
	}
}
