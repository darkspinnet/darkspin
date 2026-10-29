package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

func (e *corruptorDeathRun) hasHoldLocked(targetID uint32) bool {
	guid := util.HashID("MinionCinematicDeath")
	for _, modifier := range e.world.Effect().Snapshot() {
		if modifier.TargetObjectID == targetID && modifier.GUID == guid &&
			(e.runtime.now().Before(modifier.ExpiresAt) || modifier.IsIndefinite) {
			return true
		}
	}
	return false
}

func (e *corruptorDeathRun) createHoldLocked(targetID uint32, now time.Time) (zoneeffect.Modifier, []byte, error) {
	instanceID, err := e.runtime.modifierPool.Allocate()
	if err != nil {
		return zoneeffect.Modifier{}, nil, fmt.Errorf("deathHoldAllocate: %w", err)
	}
	modifier := zoneeffect.Modifier{InstanceID: instanceID, GUID: util.HashID("MinionCinematicDeath"),
		SourceObjectID: e.job.ObjectID, TargetObjectID: targetID, InitiatorObject: e.job.ObjectID,
		Rank: 1, StackCount: 1, Duration: zonenpc.MinionCinematicDeathDuration,
		ExpiresAt: now.Add(zonenpc.MinionCinematicDeathDuration), Kind: zoneeffect.ModifierKindBuff}
	packet, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
		SourceObjectID: e.job.ObjectID, TargetObjectID: targetID,
		ModifierID: modifier.GUID, InstanceID: instanceID,
		Duration: modifier.Duration, Timestamp: e.timestamp + uint64(now.Sub(e.startedAt)/time.Millisecond)})
	if err == nil {
		err = e.world.Effect().Put(modifier)
	}
	if err != nil {
		releaseErr := e.runtime.modifierPool.Release(instanceID)
		return zoneeffect.Modifier{}, nil, fmt.Errorf("deathHoldCreate: %w", errors.Join(err, releaseErr))
	}
	e.modifiers[targetID] = modifier
	return modifier, packet, nil
}

func (e *corruptorDeathRun) holdNPCLocked(target zonenpc.Snapshot, now time.Time) error {
	if e.hasHoldLocked(target.Plan.ObjectID) {
		return nil
	}
	pausePackets, err := npcraknet.MovementPause(target.Plan.ObjectID, target.Plan.Position, target.Facing)
	if err != nil {
		return fmt.Errorf("deathHoldPause: %w", err)
	}
	modifier, packet, err := e.createHoldLocked(target.Plan.ObjectID, now)
	if err != nil {
		return fmt.Errorf("deathHoldModifier: %w", err)
	}
	isHeld := e.world.NPCs().HoldDeathCinematic(e.job.ObjectID, modifier.InstanceID,
		target.Plan.ObjectID, modifier.ExpiresAt)
	if !isHeld {
		rollbackPackets, rollbackErr := e.releaseHoldLocked(target.Plan.ObjectID, false)
		if len(rollbackPackets) != 0 {
			return errors.New("unpublished death hold rollback produced packets")
		}
		return fmt.Errorf("deathHoldRejected: %w", errors.Join(errors.New("target replaced before hold"), rollbackErr))
	}
	e.queueLocked(append([][]byte{packet}, pausePackets...))
	return nil
}

func (e *corruptorDeathRun) holdCompanionLocked(target zonecompanion.Actor, now time.Time) error {
	if e.hasHoldLocked(target.ObjectID) {
		return nil
	}
	pausePacket, err := raknet.MarshalApplication(raknet.ObjectPlayerMoveMessage{
		ObjectID: target.ObjectID, GoalFlags: 0x20, GoalPosition: raknet.Vector3(target.Position)})
	if err != nil {
		return fmt.Errorf("deathPetPause: %w", err)
	}
	modifier, packet, err := e.createHoldLocked(target.ObjectID, now)
	if err != nil {
		return fmt.Errorf("deathPetModifier: %w", err)
	}
	held, isHeld := e.world.Companion().BeginCinematic(target, now, modifier.ExpiresAt)
	if !isHeld {
		rollbackPackets, rollbackErr := e.releaseHoldLocked(target.ObjectID, false)
		if len(rollbackPackets) != 0 {
			return errors.New("unpublished death pet rollback produced packets")
		}
		if rollbackErr != nil {
			return fmt.Errorf("deathPetRollback: %w", rollbackErr)
		}
		return nil
	}
	e.companions[target.ObjectID] = held
	e.queueLocked([][]byte{packet, pausePacket})
	return nil
}
