package runtime

import (
	"errors"
	"fmt"
	"time"

	"github.com/BurntSushi/toml"
	zonehorde "github.com/darkspinnet/darkspin/server/zone/horde"
)

// MutationAgentConfiguration owns operator choices for the unrecovered horde
// policy. It does not override authored Mutation Agent ability properties.
type MutationAgentConfiguration struct {
	Version                   uint32 `json:"version" toml:"version"`
	IsEnabled                 bool   `json:"isEnabled" toml:"is_enabled"`
	MinimumChainLevel         uint32 `json:"minimumChainLevel" toml:"minimum_chain_level"`
	WaveCount                 int    `json:"waveCount" toml:"wave_count"`
	SpawnWave                 int    `json:"spawnWave" toml:"spawn_wave"`
	InterWaveDelayMillisecond uint32 `json:"interWaveDelayMillisecond" toml:"inter_wave_delay_millisecond"`
	IsAgentDefeatCompleting   bool   `json:"isAgentDefeatCompleting" toml:"is_agent_defeat_completing"`
}

func mutationAgentConfiguration(policy zonehorde.Policy) MutationAgentConfiguration {
	return MutationAgentConfiguration{
		Version: 1, IsEnabled: policy.IsMutationAgentEnabled,
		MinimumChainLevel: policy.MutationAgentMinChainLevel,
		WaveCount:         policy.WaveCount, SpawnWave: policy.MutationAgentWaveOrdinal,
		InterWaveDelayMillisecond: uint32(policy.InterWaveDelay / time.Millisecond),
		IsAgentDefeatCompleting:   policy.IsMutationAgentDefeatCompleting,
	}
}

func (e MutationAgentConfiguration) policy() zonehorde.Policy {
	return zonehorde.Policy{
		IsMutationAgentEnabled: e.IsEnabled, MutationAgentMinChainLevel: e.MinimumChainLevel,
		WaveCount: e.WaveCount, MutationAgentWaveOrdinal: e.SpawnWave,
		InterWaveDelay:                  time.Duration(e.InterWaveDelayMillisecond) * time.Millisecond,
		IsMutationAgentDefeatCompleting: e.IsAgentDefeatCompleting,
	}
}

func decodeMutationAgentConfiguration(payload []byte) (MutationAgentConfiguration, bool, error) {
	configuration := mutationAgentConfiguration(zonehorde.DefaultPolicy())
	var raw struct {
		MutationAgent toml.Primitive `toml:"mutation_agent"`
	}
	metadata, err := toml.Decode(string(payload), &raw)
	if err != nil {
		return configuration, false, fmt.Errorf("mutationDecode: %w", err)
	}
	if !metadata.IsDefined("mutation_agent") {
		return configuration, true, nil
	}
	err = metadata.PrimitiveDecode(raw.MutationAgent, &configuration)
	if err != nil {
		return configuration, false, fmt.Errorf("mutationFields: %w", err)
	}
	if configuration.Version != 1 {
		return configuration, false, errors.New("mutation agent settings version unsupported")
	}
	err = configuration.policy().Validate()
	if err != nil {
		return configuration, false, fmt.Errorf("mutationPolicy: %w", err)
	}
	return configuration, false, nil
}

// HordePolicy is captured by a new zone; changes never alter an active wave.
func (e *serverRuleStore) HordePolicy() zonehorde.Policy {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.hordePolicy
}
