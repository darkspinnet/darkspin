package gameplay

import (
	"fmt"

	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

// The authored ghost charges wait relaxTime after selecting their ending
// animation. Cooldown readiness is a separate deadline, not a pose duration.
type campaignChargePoseEnd struct {
	runtime          campaignNPCActionRuntime
	sessionKey       string
	generation       uint64
	objectID         uint32
	actionGeneration uint64
	readyTimestamp   uint64
	timestamp        uint64
}

func (e campaignChargePoseEnd) produce() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && member.isCampaignNPCSourceGenerationActive(
		e.generation, e.objectID, e.actionGeneration,
	) && member.campaignNPCChargeReadiness[e.objectID] == e.readyTimestamp
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	packet, err := npcraknet.ResetAnimation(e.objectID, e.timestamp)
	if err != nil {
		return nil, fmt.Errorf("chargePoseReset: %w", err)
	}
	return [][]byte{packet}, nil
}
