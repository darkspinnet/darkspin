package game

import (
	"errors"
	"fmt"
)

// ZoneRules is a mission-owned snapshot. File and launcher representations are
// adapter concerns; these rules remain fixed throughout a zone's lifetime.
type ZoneRules struct {
	Key                   string
	EnemyTargetPerSection uint32
	EnemyPopulation       EnemyPopulationRules
	BossSpawning          BossSpawnRules
	AllyAlert             ZoneAllyAlertRules
	MutationAgent         ZoneMutationAgentRules
}

type ZoneAllyAlertRules struct {
	IsEnabled                  bool
	RangePercent               uint32
	RangeOwner                 string
	MaxHops                    uint32
	IsDiagnosticLoggingEnabled bool
}

type ZoneMutationAgentRules struct {
	IsEnabled                    bool
	MinimumChainLevel            uint32
	WaveCount                    int
	AdditionalWaveChancePercents []uint32
	FixedWaves                   [][]ZoneHordeEnemy
	LieutenantChancePercent      uint32
	LieutenantLimitPerWave       int
	// SpawnWave retains the checkpoint representation; encounter policy resolves the final wave.
	SpawnWave                 int
	InterWaveDelayMillisecond uint32
	IsAgentDefeatCompleting   bool
}

func ZoneRuleKey(binding GameplayBinding) (string, error) {
	if binding.Mode == ModeTutorial {
		return "tutorial", nil
	}
	if binding.Mode == ModeArena {
		return "arena", nil
	}
	stage := binding.ChainLevelIndex
	if stage == 0 {
		stage = binding.Difficulty
	}
	if stage < 1 || stage > 72 {
		return "", fmt.Errorf("zoneRuleStage[%d]: outside campaign", stage)
	}
	mode := "campaign"
	if binding.IsReplayRandomized {
		mode = "replay"
	}
	return fmt.Sprintf("%s:%d-%d", mode, (stage-1)/4+1, (stage-1)%4+1), nil
}

func DefaultZoneRules(binding GameplayBinding) ZoneRules {
	key, err := ZoneRuleKey(binding)
	if err != nil {
		// Content-only/scenario directors can precede mission selection.
		key = "unselected"
	}
	return ZoneRules{
		Key: key, EnemyTargetPerSection: CampaignPopulationTarget(binding.Level),
		EnemyPopulation: DefaultEnemyPopulationRules(), BossSpawning: DefaultBossSpawnRules(),
		AllyAlert: ZoneAllyAlertRules{IsEnabled: true, RangePercent: 100, RangeOwner: "recipient", MaxHops: 1},
		MutationAgent: ZoneMutationAgentRules{IsEnabled: true, MinimumChainLevel: 6,
			LieutenantChancePercent: 30, LieutenantLimitPerWave: 1,
			WaveCount: 3, SpawnWave: 3, InterWaveDelayMillisecond: 500, IsAgentDefeatCompleting: true},
	}
}

func (e ZoneRules) Validate() error {
	if e.Key == "" || e.EnemyTargetPerSection < 1 || e.EnemyTargetPerSection > 1000 {
		return errors.New("zone identity or population target invalid")
	}
	err := e.EnemyPopulation.Validate()
	if err != nil {
		return fmt.Errorf("zonePopulation: %w", err)
	}
	err = e.BossSpawning.Validate()
	if err != nil {
		return fmt.Errorf("zoneBoss: %w", err)
	}
	alert := e.AllyAlert
	if alert.RangePercent < 1 || alert.RangePercent > 1000 || alert.MaxHops < 1 || alert.MaxHops > 16 ||
		(alert.RangeOwner != "source" && alert.RangeOwner != "recipient") {
		return errors.New("zone ally alert settings invalid")
	}
	mutation := e.MutationAgent
	err = ValidateZoneHordeWaves(mutation.FixedWaves, mutation.WaveCount)
	if err != nil {
		return fmt.Errorf("zoneHorde: %w", err)
	}
	if len(mutation.FixedWaves) != 0 && len(mutation.AdditionalWaveChancePercents) != 0 {
		return errors.New("fixed horde waves cannot use continuation chances")
	}
	if mutation.LieutenantChancePercent > 100 || mutation.LieutenantLimitPerWave < 0 || mutation.LieutenantLimitPerWave > 15 {
		return errors.New("zone mutation lieutenant settings invalid")
	}
	if mutation.MinimumChainLevel < 1 || mutation.MinimumChainLevel > 72 ||
		mutation.WaveCount < 1 || mutation.WaveCount > 10 ||
		mutation.WaveCount+len(mutation.AdditionalWaveChancePercents) > 10 ||
		mutation.SpawnWave < 1 || mutation.SpawnWave > mutation.WaveCount ||
		mutation.InterWaveDelayMillisecond > 30000 {
		return errors.New("zone mutation agent settings invalid")
	}
	for _, chancePercent := range mutation.AdditionalWaveChancePercents {
		if chancePercent > 100 {
			return errors.New("zone mutation wave chance must be between 0 and 100")
		}
	}
	return nil
}

func (e CampaignDirector) PopulationTarget() uint32 {
	if e.ZoneRules.EnemyTargetPerSection != 0 {
		return e.ZoneRules.EnemyTargetPerSection
	}
	return CampaignPopulationTarget(e.Level)
}
