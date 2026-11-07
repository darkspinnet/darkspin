package gameplay

import (
	"context"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/zone"
	zonecheckpoint "github.com/darkspinnet/darkspin/server/zone/checkpoint"
	zonehorde "github.com/darkspinnet/darkspin/server/zone/horde"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func (e campaignPreparation) zoneRules(ctx context.Context, binding game.GameplayBinding) (game.ZoneRules, error) {
	if e.zone != nil {
		instance, isFound := e.zone.Get(uint64(binding.GameID))
		if isFound {
			rules := instance.DirectorDefinition().ZoneRules
			if rules.Key != "" {
				return rules, nil
			}
		}
	}
	var restore *zonecheckpoint.Snapshot
	if e.checkpoint != nil {
		snapshot, isFound, err := e.checkpoint.Load(ctx, uint64(binding.GameID))
		if err != nil {
			return game.ZoneRules{}, fmt.Errorf("zoneRuleCheckpoint: %w", err)
		}
		if isFound {
			err = zonecheckpoint.Validate(snapshot, uint64(binding.GameID), binding.Level, binding.Difficulty, binding.UserID)
			if err != nil {
				// Preparation's existing admission discards invalid saves.
			} else {
				if snapshot.IsZoneRuleSnapshotSet {
					return snapshot.ZoneRules, nil
				}
				restore = &snapshot
			}
		}
	}
	rules := game.DefaultZoneRules(binding)
	source, isSource := e.spawnRuleSource.(interface {
		ZoneRules(context.Context, game.GameplayBinding) (game.ZoneRules, error)
	})
	if isSource {
		var err error
		rules, err = source.ZoneRules(ctx, binding)
		if err != nil {
			return game.ZoneRules{}, fmt.Errorf("zoneRuleLoad: %w", err)
		}
	} else {
		rules = e.legacyZoneRules(rules)
	}
	err := rules.Validate()
	if err != nil {
		return game.ZoneRules{}, fmt.Errorf("zoneRuleValidate: %w", err)
	}
	if restore != nil && restore.IsSpawnRuleSnapshotSet {
		rules.EnemyPopulation = restore.EnemyPopulationRules.Resolved()
		rules.BossSpawning = restore.BossSpawnRules.Resolved()
	}
	return rules, nil
}

// Compatibility sources without the zone selector retain their prior defaults.
// Production JSON sources always use the typed selector above.
func (e campaignPreparation) legacyZoneRules(rules game.ZoneRules) game.ZoneRules {
	populationSource, isPopulationSource := e.spawnRuleSource.(interface {
		EnemyPopulationRules() game.EnemyPopulationRules
	})
	if isPopulationSource {
		rules.EnemyPopulation = populationSource.EnemyPopulationRules()
	}
	bossSource, isBossSource := e.spawnRuleSource.(interface{ BossSpawnRules() game.BossSpawnRules })
	if isBossSource {
		rules.BossSpawning = bossSource.BossSpawnRules()
	}
	spawnSource, isSpawnSource := e.spawnRuleSource.(interface {
		SpawnRulesSnapshot() (game.EnemyPopulationRules, game.BossSpawnRules)
	})
	if isSpawnSource {
		rules.EnemyPopulation, rules.BossSpawning = spawnSource.SpawnRulesSnapshot()
	}
	if e.spawnRuleSource != nil {
		rules.AllyAlert = game.ZoneAllyAlertRules(e.spawnRuleSource.AllyAlertRules())
	}
	policy := e.hordePolicy()
	rules.MutationAgent = game.ZoneMutationAgentRules{
		IsEnabled: policy.IsMutationAgentEnabled, MinimumChainLevel: policy.MutationAgentMinChainLevel,
		WaveCount: policy.WaveCount, SpawnWave: policy.MutationAgentWaveOrdinal,
		AdditionalWaveChancePercents: append([]uint32(nil), policy.AdditionalWaveChancePercents...),
		FixedWaves:                   game.CloneZoneHordeWaves(policy.FixedWaves),
		LieutenantChancePercent:      policy.LieutenantChancePercent,
		LieutenantLimitPerWave:       policy.LieutenantLimitPerWave,
		InterWaveDelayMillisecond:    uint32(policy.InterWaveDelay / time.Millisecond),
		IsAgentDefeatCompleting:      policy.IsMutationAgentDefeatCompleting,
	}
	return rules
}

func campaignZoneHordePolicy(rules game.ZoneMutationAgentRules) zonehorde.Policy {
	return zonehorde.Policy{
		IsMutationAgentEnabled: rules.IsEnabled, MutationAgentMinChainLevel: rules.MinimumChainLevel,
		WaveCount: rules.WaveCount, MutationAgentWaveOrdinal: rules.SpawnWave,
		AdditionalWaveChancePercents:    append([]uint32(nil), rules.AdditionalWaveChancePercents...),
		FixedWaves:                      game.CloneZoneHordeWaves(rules.FixedWaves),
		LieutenantChancePercent:         rules.LieutenantChancePercent,
		LieutenantLimitPerWave:          rules.LieutenantLimitPerWave,
		InterWaveDelay:                  time.Duration(rules.InterWaveDelayMillisecond) * time.Millisecond,
		IsMutationAgentDefeatCompleting: rules.IsAgentDefeatCompleting,
	}
}

func campaignZoneHordeSession(binding game.GameplayBinding, rules game.ZoneRules, snapshot *zonecheckpoint.Snapshot) *zonehorde.Session {
	progression := binding.ChainProgression
	if snapshot != nil && snapshot.IsZoneRuleSnapshotSet {
		// Saved rules contain the original world's effective eligibility. A
		// different restoring member must not apply their unlock gate again.
		progression = 72
	}
	return zonehorde.NewCampaignSessionWithSeed(progression, campaignZoneHordePolicy(rules.MutationAgent), binding.RunSeed)
}

func campaignZoneAllyAlertRules(instance *zone.Zone) zonenpc.AllyAlertRules {
	if instance == nil {
		return zonenpc.DefaultAllyAlertRules()
	}
	rules := instance.DirectorDefinition().ZoneRules
	if rules.Key == "" {
		// Directly constructed worlds predating zone-rule snapshots.
		return zonenpc.DefaultAllyAlertRules()
	}
	return zonenpc.AllyAlertRules(rules.AllyAlert)
}
