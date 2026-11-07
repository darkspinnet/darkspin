package zone

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	zonecheckpoint "github.com/darkspinnet/darkspin/server/zone/checkpoint"
	zonehorde "github.com/darkspinnet/darkspin/server/zone/horde"
)

func (e *Zone) checkpointZoneRules() game.ZoneRules {
	rules := e.info.DirectorDefinition.ZoneRules
	if rules.Key == "" {
		return rules
	}
	// Configuration stores the operator's enabled flag. A durable world
	// snapshot stores the result of its original progression unlock gate.
	rules.MutationAgent.IsEnabled = e.info.Horde.Policy().IsMutationAgentEnabled
	return rules
}

func (e *Zone) restoreZoneRules(snapshot zonecheckpoint.Snapshot) error {
	if !snapshot.IsZoneRuleSnapshotSet {
		return nil
	}
	err := snapshot.ZoneRules.Validate()
	if err != nil {
		return fmt.Errorf("restoreZoneRules: %w", err)
	}
	rules := snapshot.ZoneRules
	e.info.DirectorDefinition.ZoneRules = rules
	e.info.DirectorDefinition.EnemyPopulationRules = rules.EnemyPopulation
	e.info.DirectorDefinition.BossSpawnRules = rules.BossSpawning
	mutation := rules.MutationAgent
	policy := zonehorde.Policy{
		IsMutationAgentEnabled: mutation.IsEnabled, MutationAgentMinChainLevel: mutation.MinimumChainLevel,
		WaveCount: mutation.WaveCount, MutationAgentWaveOrdinal: mutation.SpawnWave,
		AdditionalWaveChancePercents:    append([]uint32(nil), mutation.AdditionalWaveChancePercents...),
		FixedWaves:                      game.CloneZoneHordeWaves(mutation.FixedWaves),
		LieutenantChancePercent:         mutation.LieutenantChancePercent,
		LieutenantLimitPerWave:          mutation.LieutenantLimitPerWave,
		InterWaveDelay:                  time.Duration(mutation.InterWaveDelayMillisecond) * time.Millisecond,
		IsMutationAgentDefeatCompleting: mutation.IsAgentDefeatCompleting,
	}
	// Eligibility is already resolved in this snapshot. Restore completed
	// encounters into this policy before the zone can admit another wave.
	e.info.Horde = zonehorde.NewCampaignSessionWithSeed(72, policy, e.info.RunSeed)
	return nil
}
