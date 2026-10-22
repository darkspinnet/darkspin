package horde

import (
	"errors"
	"time"
)

// Policy is an operator-owned compatibility policy. Native server horde wave
// selection and completion rules have not been recovered.
type Policy struct {
	IsMutationAgentEnabled          bool
	MutationAgentMinChainLevel      uint32
	WaveCount                       int
	MutationAgentWaveOrdinal        int
	InterWaveDelay                  time.Duration
	IsMutationAgentDefeatCompleting bool
}

func DefaultPolicy() Policy {
	return Policy{
		IsMutationAgentEnabled: true, MutationAgentMinChainLevel: 7,
		WaveCount: 3, MutationAgentWaveOrdinal: 3,
		InterWaveDelay: InterWaveDelay, IsMutationAgentDefeatCompleting: true,
	}
}

func (e Policy) Validate() error {
	if e.MutationAgentMinChainLevel < 1 || e.MutationAgentMinChainLevel > 72 {
		return errors.New("horde mutation minimum chain level must be between 1 and 72")
	}
	if e.WaveCount < 1 || e.WaveCount > 10 {
		return errors.New("horde wave count must be between 1 and 10")
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
	policy.IsMutationAgentEnabled = e.areMutationAgentsEnabled
	return policy
}
