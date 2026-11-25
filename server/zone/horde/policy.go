package horde

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
)

// Policy is an operator-owned compatibility policy. Native server horde wave
// selection and completion rules have not been recovered.
type Policy struct {
	IsMutationAgentEnabled bool
	// MutationAgentMinChainLevel is the completed progression required to
	// unlock Mutation Agents in any selected mission.
	MutationAgentMinChainLevel uint32
	// WaveCount is guaranteed; each chance can add one wave after the last succeeds.
	WaveCount                    int
	AdditionalWaveChancePercents []uint32
	FixedWaves                   [][]game.ZoneHordeEnemy
	LieutenantChancePercent      uint32
	LieutenantLimitPerWave       int
	// MutationAgentWaveOrdinal is retained for checkpoint compatibility.
	// WaveCount determines the final wave on which the agent spawns.
	MutationAgentWaveOrdinal        int
	InterWaveDelay                  time.Duration
	IsMutationAgentDefeatCompleting bool
}

func DefaultPolicy() Policy {
	return Policy{
		IsMutationAgentEnabled: true, MutationAgentMinChainLevel: 6,
		WaveCount: 3, MutationAgentWaveOrdinal: 3,
		LieutenantChancePercent: 30, LieutenantLimitPerWave: 1,
		InterWaveDelay: InterWaveDelay, IsMutationAgentDefeatCompleting: true,
	}
}

func (e Policy) Validate() error {
	err := game.ValidateZoneHordeWaves(e.FixedWaves, e.WaveCount)
	if err != nil {
		return fmt.Errorf("hordeWaves: %w", err)
	}
	if len(e.FixedWaves) != 0 && len(e.AdditionalWaveChancePercents) != 0 {
		return errors.New("fixed horde waves cannot use continuation chances")
	}
	if e.LieutenantChancePercent > 100 || e.LieutenantLimitPerWave < 0 || e.LieutenantLimitPerWave > 15 {
		return errors.New("horde lieutenant settings invalid")
	}
	if e.MutationAgentMinChainLevel < 1 || e.MutationAgentMinChainLevel > 72 {
		return errors.New("horde mutation minimum chain level must be between 1 and 72")
	}
	if e.WaveCount < 1 || e.WaveCount > 10 || e.WaveCount+len(e.AdditionalWaveChancePercents) > 10 {
		return errors.New("horde wave count must be between 1 and 10")
	}
	for _, chancePercent := range e.AdditionalWaveChancePercents {
		if chancePercent > 100 {
			return errors.New("horde wave chance must be between 0 and 100")
		}
	}
	if e.MutationAgentWaveOrdinal < 1 || e.MutationAgentWaveOrdinal > e.WaveCount {
		return errors.New("horde mutation wave must be within the configured wave count")
	}
	if e.InterWaveDelay < 0 || e.InterWaveDelay > 30*time.Second {
		return errors.New("horde interwave delay must be between 0 and 30 seconds")
	}
	return nil
}

func (e *Session) Policy() Policy {
	if e == nil {
		return DefaultPolicy()
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	policy := e.policy
	policy.AdditionalWaveChancePercents = append([]uint32(nil), policy.AdditionalWaveChancePercents...)
	policy.FixedWaves = game.CloneZoneHordeWaves(policy.FixedWaves)
	policy.IsMutationAgentEnabled = e.areMutationAgentsEnabled
	return policy
}

// EncounterPolicy uses a separate deterministic stream, never the enemy/loot
// random stream. Retries and restored worlds resolve the same final wave.
func (e *Session) EncounterPolicy(publication game.CampaignDirectorPublication) Policy {
	if e == nil {
		return DefaultPolicy()
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	policy := e.encounterPolicy(publication)
	current := e.encountersByMarkerSet[strings.ToLower(publication.MarkerSetName)]
	if current != nil {
		policy.WaveCount = current.waveCount
		policy.MutationAgentWaveOrdinal = current.waveCount
	}
	return policy
}

func (e *Session) encounterPolicy(publication game.CampaignDirectorPublication) Policy {
	policy := e.policy
	policy.FixedWaves = game.CloneZoneHordeWaves(policy.FixedWaves)
	policy.IsMutationAgentEnabled = e.areMutationAgentsEnabled
	if policy.IsMutationAgentEnabled && len(policy.AdditionalWaveChancePercents) != 0 {
		seed := uint32(e.waveSeed) ^ uint32(e.waveSeed>>32) ^
			publication.TriggerMarkerID ^ util.HashID(publication.MarkerSetName) ^ 0x57415645
		random := sim.NewSimulatorRandom(seed)
		for _, chancePercent := range policy.AdditionalWaveChancePercents {
			if random.Float64() >= float64(chancePercent)/100 {
				break
			}
			policy.WaveCount++
		}
	}
	policy.AdditionalWaveChancePercents = nil
	policy.MutationAgentWaveOrdinal = policy.WaveCount
	return policy
}
