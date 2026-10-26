package game

import "errors"

// EnemyPopulationRules controls the reconstructed standing campaign population.
// Authored spawn points and separate boss/horde encounters still bound the result.
type EnemyPopulationRules struct {
	EnemiesPerSection uint32
}

func DefaultEnemyPopulationRules() EnemyPopulationRules {
	return EnemyPopulationRules{EnemiesPerSection: 15}
}

func (e EnemyPopulationRules) Validate() error {
	if e.EnemiesPerSection < 10 || e.EnemiesPerSection > 60 {
		return errors.New("enemies per section must be between 10 and 60")
	}
	return nil
}

func (e EnemyPopulationRules) Resolved() EnemyPopulationRules {
	if e.EnemiesPerSection == 0 {
		return DefaultEnemyPopulationRules()
	}
	return e
}

// BossSpawnRules controls reconstructed encounter policy, not authored NPC stats.
type BossSpawnRules struct {
	OpeningAddsPerAnchor    uint32
	InitialBossAddCount     uint32
	CaptainDelayMillisecond uint32
}

func DefaultBossSpawnRules() BossSpawnRules {
	return BossSpawnRules{OpeningAddsPerAnchor: 1, InitialBossAddCount: 2, CaptainDelayMillisecond: 2000}
}

func (e BossSpawnRules) Validate() error {
	if e.OpeningAddsPerAnchor < 1 || e.OpeningAddsPerAnchor > 4 ||
		e.InitialBossAddCount < 1 || e.InitialBossAddCount > 12 ||
		e.CaptainDelayMillisecond > 30000 {
		return errors.New("boss spawn settings outside allowed ranges")
	}
	return nil
}

func (e BossSpawnRules) Resolved() BossSpawnRules {
	if e.OpeningAddsPerAnchor == 0 {
		return DefaultBossSpawnRules()
	}
	return e
}
