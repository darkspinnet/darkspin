package gameplay

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func (e *corruptorDeathRun) releaseHoldLocked(targetID uint32, isPublished bool) ([][]byte, error) {
	modifier, isOwned := e.modifiers[targetID]
	if !isOwned {
		return nil, nil
	}
	delete(e.modifiers, targetID)
	e.world.NPCs().ReleaseDeathCinematic(e.job.ObjectID, modifier.InstanceID, targetID)
	if target, isHeld := e.companions[targetID]; isHeld {
		isEnded := e.world.Companion().EndCinematic(target)
		if !isEnded {
			// A dead/replaced lifetime no longer owns this hold.
		}
		delete(e.companions, targetID)
	}
	removed, isRemoved := e.world.Effect().Remove(modifier.InstanceID)
	var removeErr error
	if isRemoved && removed.TargetObjectID != targetID {
		removeErr = errors.New("death hold inventory target changed")
	}
	releaseErr := e.runtime.modifierPool.Release(modifier.InstanceID)
	if !isPublished {
		if removeErr != nil || releaseErr != nil {
			return nil, fmt.Errorf("deathHoldRollback: %w", errors.Join(removeErr, releaseErr))
		}
		return nil, nil
	}
	packet, marshalErr := effectraknet.ModifierDelete(targetID, modifier.InstanceID)
	if removeErr != nil || releaseErr != nil || marshalErr != nil {
		return nil, fmt.Errorf("deathHoldRetire: %w", errors.Join(removeErr, releaseErr, marshalErr))
	}
	return [][]byte{packet}, nil
}

func (e *corruptorDeathRun) deleteJobLocked() ([]byte, error) {
	if !e.isAdmitted || e.isJobRetired {
		return nil, nil
	}
	packet, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{ObjectID: []uint32{e.job.ObjectID}})
	if err != nil {
		return nil, fmt.Errorf("deathJobDelete: %w", err)
	}
	job, isFound := e.world.NPCs().NPC(e.job.ObjectID)
	if isFound && !job.IsDefeated {
		result, defeatErr := e.world.NPCs().Defeat(e.job.ObjectID, e.job.ObjectID)
		if defeatErr != nil {
			return nil, fmt.Errorf("deathJobDefeat: %w", defeatErr)
		}
		if !result.IsDefeated {
			return nil, errors.New("death job resisted lifecycle retirement")
		}
	}
	err = e.world.RecordNPCDeathObjectives(e.world.Context(), []zonenpc.DeathEvent{{
		Kind: zonenpc.DeathDelete, TargetObjectID: e.job.ObjectID}})
	e.isJobRetired = true
	if err != nil {
		return packet, fmt.Errorf("deathJobRecord: %w", err)
	}
	return packet, nil
}

// Failure/teardown releases partially admitted resources. Ordinary successful
// job completion intentionally keeps surviving MinionCinematicDeath holds.
func (e *corruptorDeathRun) retireLocked() error {
	e.isComplete = true
	e.isArmed = false
	e.targets = nil
	isCanceled := e.world.Timeline().Cancel(e.key())
	if !isCanceled {
		// The current callback may already have consumed this timeline slot.
	}
	if e.stopContext != nil {
		isStopped := e.stopContext()
		if !isStopped {
			// A concurrently started context callback repeats this idempotently.
		}
		e.stopContext = nil
	}
	packets := make([][]byte, 0, len(e.modifiers)+1)
	var retireErr error
	for targetID := range e.modifiers {
		holdPackets, err := e.releaseHoldLocked(targetID, true)
		packets = append(packets, holdPackets...)
		if err != nil {
			retireErr = errors.Join(retireErr, fmt.Errorf("deathHold[%d]: %w", targetID, err))
		}
	}
	packet, deleteErr := e.deleteJobLocked()
	if len(packet) > 0 {
		packets = append(packets, packet)
	}
	e.queueLocked(packets)
	if retireErr != nil || deleteErr != nil {
		return fmt.Errorf("deathRetire: %w", errors.Join(retireErr, deleteErr))
	}
	return nil
}

func (e *corruptorDeathRun) cancel() {
	e.runtime.registry.mutex.Lock()
	err := e.retireLocked()
	e.runtime.registry.mutex.Unlock()
	if err != nil {
		e.runtime.logger.Printf("Corruptor death world retirement job=%d: %v", e.job.ObjectID, err)
	}
}
