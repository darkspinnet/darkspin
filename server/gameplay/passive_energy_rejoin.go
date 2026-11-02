package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/util"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
)

func (e *energySentinelPassiveRun) presentation(timestamp uint64) ([][]byte, error) {
	if e == nil {
		return nil, nil
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.isStopped || e.npc == nil {
		return nil, nil
	}
	packets := make([][]byte, 0, len(e.targets))
	for _, target := range e.targets {
		if target.objectID == 0 {
			continue
		}
		npc, isFound := e.npc.NPC(target.objectID)
		if !isFound || npc.IsDefeated || npc.HitPoint <= 0 {
			continue
		}
		packet, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
			SourceObjectID: e.sourceObjectID, TargetObjectID: target.objectID,
			ModifierID: util.HashID("EnergySentinelVulnerability"),
			InstanceID: target.modifier.instanceID, IsIndefinite: true,
			Timestamp: timestamp,
		})
		if err != nil {
			return nil, fmt.Errorf("energyPassiveReplay: %w", err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func (e *gameplaySessionRegistry) energySentinelPresentation(
	peerSession gameplayPeerSession, timestamp uint64,
) ([][]byte, error) {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	packets := make([][]byte, 0)
	for _, candidate := range e.sessions {
		if candidate.zone != peerSession.zone || candidate.deployedHitPoint() <= 0 {
			continue
		}
		markers, err := candidate.energySentinelPassive.presentation(timestamp)
		if err != nil {
			return nil, fmt.Errorf("energyPassiveMember: %w", err)
		}
		packets = append(packets, markers...)
	}
	return packets, nil
}
