package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
)

// Scarab daze blocks actions but deliberately leaves ordinary walking available.
// Its native modifier owns is_stunned, status_dazed and status_snared presentation.
func (e *gameplayPeerSession) isScarabDazed() bool {
	if e == nil || e.deployedObjectID == 0 {
		return false
	}
	for _, run := range e.campaignNPCModifiers {
		if run != nil && run.isActive() && run.record.TargetObjectID == e.deployedObjectID &&
			run.record.GUID == util.HashID("CitadelMinionSuicide_Daze") {
			return true
		}
	}
	return false
}

func (e campaignNPCActionRuntime) refreshScarabDazeMovement(sessionKey string, generation uint64, targetObjectIDs ...uint32) ([][]byte, error) {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || member.generation != generation || member.zone == nil {
		return nil, nil
	}
	targetObjectID := member.deployedObjectID
	if len(targetObjectIDs) != 0 {
		targetObjectID = targetObjectIDs[0]
	}
	if targetObjectID == 0 {
		return nil, nil
	}
	if targetObjectID != member.deployedObjectID {
		actor, isActorFound := member.zone.Companion().Snapshot(targetObjectID)
		if !isActorFound || actor.UserID != member.binding.UserID || actor.PeerGeneration != generation || actor.HitPoint <= 0 {
			return nil, nil
		}
		buff, isBuffFound := member.zone.Companion().MovementSpeedBuff(targetObjectID, e.now())
		if !isBuffFound {
			return nil, nil
		}
		packet, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
			ObjectID: targetObjectID, Value: map[uint8]float32{48: buff}})
		if err != nil {
			return nil, fmt.Errorf("companionSlowAttribute: %w", err)
		}
		return [][]byte{packet}, nil
	}
	if member.playerMotion != nil {
		increase := max(float32(-0.9), e.registry.passiveMovementIncrease(member)+member.enemyMovementSpeedBuff())
		err := member.playerMotion.SetSpeed(e.now(), zonePlayerMoveSpeed*(1+increase))
		if err != nil {
			return nil, fmt.Errorf("dazeMotion: %w", err)
		}
	}
	e.registry.sessions[sessionKey] = member
	packet, err := marshalGraviticSpeed(member)
	if err != nil {
		return nil, fmt.Errorf("dazeSpeed: %w", err)
	}
	return [][]byte{packet}, nil
}
