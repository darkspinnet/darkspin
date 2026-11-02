package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	actionraknet "github.com/darkspinnet/darkspin/server/zone/action/raknet103"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

// Deactivate in QuantumBlink restores the saved position and animation even
// when the strike coroutine is interrupted. Clear the owner first so no later
// producer can strike or retain its damage immunity after cancellation.
func (e *gameplayPeerSession) interruptQuantumBlinkLocked(now time.Time) ([][]byte, error) {
	if e == nil || e.heroQuantumBlink == nil {
		return nil, nil
	}
	run := e.heroQuantumBlink
	e.heroQuantumBlink = nil
	run.Stop()
	if e.deployedObjectID != run.sourceObjectID {
		return nil, nil
	}
	packets := make([][]byte, 0, 3)
	var cleanupErr error
	if run.isPositionSaved {
		teleportPackets, err := e.returnQuantumBlinkPosition(run, now)
		cleanupErr = err
		packets = append(packets, teleportPackets...)
	}
	timestamp := run.sourceTime + uint64(max(time.Duration(0), now.Sub(run.startedAt))/time.Millisecond)
	resetPacket, err := npcraknet.ResetAnimation(run.sourceObjectID, timestamp)
	if err != nil {
		return packets, fmt.Errorf("blinkAnimation: %w", errors.Join(cleanupErr, err))
	}
	packets = append(packets, resetPacket)
	if cleanupErr != nil {
		return packets, fmt.Errorf("blinkCleanup: %w", cleanupErr)
	}
	return packets, nil
}

func (e *gameplayPeerSession) returnQuantumBlinkPosition(run *heroQuantumBlinkRun, now time.Time) ([][]byte, error) {
	err := e.teleportPlayer(now, run.returnPosition)
	if err != nil {
		return nil, fmt.Errorf("blinkReturn: %w", err)
	}
	err = e.syncZoneHeroPose()
	if err != nil {
		return nil, fmt.Errorf("blinkReturnPose: %w", err)
	}
	packets, err := actionraknet.Teleport(actionraknet.TeleportRequest{
		ObjectID: run.sourceObjectID, Position: game.Vec3(run.returnPosition), Orientation: run.orientation,
	})
	if err != nil {
		return nil, fmt.Errorf("blinkTeleport: %w", err)
	}
	return packets, nil
}

// Pose packets enter each recipient's FIFO while the registry still owns the
// pose mutation. A concurrent cancel therefore queues its reset afterward.
func (e *gameplaySessionRegistry) queueQuantumBlinkPoseLocked(source *gameplayPeerSession, packets [][]byte) error {
	if len(packets) == 0 {
		return nil
	}
	if source.binding.Mode == game.ModeArena {
		source.queuePackets(packets)
		for key, candidate := range e.sessions {
			if candidate.binding.Mode != game.ModeArena || candidate.binding.GameID != source.binding.GameID ||
				candidate.binding.UserID == source.binding.UserID || candidate.isRejoinPending {
				continue
			}
			candidate.queuePackets(packets)
			e.sessions[key] = candidate
		}
		return nil
	}
	if source.zone == nil {
		source.queuePackets(packets)
		return nil
	}
	hero, isFound := source.zone.Hero().Snapshot(source.binding.UserID, source.generation)
	if !isFound || hero.ObjectID != source.deployedObjectID {
		return nil
	}
	source.queueCampaignPackets(packets)
	var queueErr error
	for sessionKey, candidate := range e.sessions {
		if candidate.zone != source.zone || (candidate.binding.UserID == source.binding.UserID && candidate.generation == source.generation) {
			continue
		}
		err := candidate.queueCampaignPresentation(packets)
		if err != nil {
			queueErr = errors.Join(queueErr, fmt.Errorf("blinkPeerQueue: %w", err))
		}
		e.sessions[sessionKey] = candidate
	}
	if queueErr != nil {
		return fmt.Errorf("blinkQueue: %w", queueErr)
	}
	return nil
}

func (e *gameplayPeerSession) retireQuantumBlinkLocked(now time.Time) error {
	if e == nil || e.heroQuantumBlink == nil {
		return nil
	}
	run := e.heroQuantumBlink
	packets, cleanupErr := e.interruptQuantumBlinkLocked(now)
	if run.registry == nil {
		e.queueCampaignPackets(packets)
		if cleanupErr != nil {
			return fmt.Errorf("blinkRetire: %w", cleanupErr)
		}
		return nil
	}
	queueErr := run.registry.queueQuantumBlinkPoseLocked(e, packets)
	if cleanupErr != nil || queueErr != nil {
		return fmt.Errorf("blinkRetire: %w", errors.Join(cleanupErr, queueErr))
	}
	return nil
}
