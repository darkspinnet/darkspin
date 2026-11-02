package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
)

func (e campaignNPCActionRuntime) newSpoffitLocked(member gameplayPeerSession, req raknet.Packet, requestID string) (*spoffitRun, error) {
	if member.zone == nil || member.deployedObjectID == 0 || member.deployedHitPoint() <= 0 ||
		int(member.deployedCreatureIndex) >= len(member.binding.Creatures) || e.timer == nil || req.ScheduleGroupResult == nil {
		return nil, errors.New("Spoffit requires its live owned hero and scheduler")
	}
	for run := range e.registry.spoffits {
		if run.world == member.zone && run.userID == member.binding.UserID && run.generation == member.generation && run.ownerObjectID == member.deployedObjectID && !run.isStopped {
			return nil, errors.New("owned SpoffitCrystal is already active")
		}
	}
	profile, err := spoffitProfile(member)
	if err != nil {
		return nil, fmt.Errorf("spoffitProfile: %w", err)
	}
	footprint, err := e.program.NavigationRadiusByNoun(util.HashID(spoffitNoun), 1)
	if err != nil {
		return nil, fmt.Errorf("spoffitFootprint: %w", err)
	}
	instanceID, err := e.modifierPool.Allocate()
	if err != nil {
		return nil, fmt.Errorf("spoffitInstance: %w", err)
	}
	run := &spoffitRun{runtime: e, world: member.zone, profile: profile, packet: req.Autonomous(),
		userID: member.binding.UserID, generation: member.generation, ownerObjectID: member.deployedObjectID,
		instanceID: instanceID, footprintRadius: footprint, sourceTime: req.SourceTime, startedAt: e.now(), requestID: requestID}
	packet, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
		TargetID: run.ownerObjectID, ModifierGUID: util.HashID(spoffitCrystalModifier),
		InstanceID: instanceID, StackCount: 1, StartMilliseconds: req.SourceTime,
	})
	if err == nil {
		err = member.zone.Effect().Put(zoneeffect.Modifier{InstanceID: instanceID,
			GUID: util.HashID(spoffitCrystalModifier), SourceObjectID: run.ownerObjectID,
			TargetObjectID: run.ownerObjectID, InitiatorObject: run.ownerObjectID,
			Rank: 1, StackCount: 1, Kind: zoneeffect.ModifierKindBuff, IsIndefinite: true})
	}
	if err != nil {
		releaseErr := e.modifierPool.Release(instanceID)
		return nil, fmt.Errorf("spoffitCreator: %w", errors.Join(err, releaseErr))
	}
	run.creatorPackets = [][]byte{packet}
	if e.registry.spoffits == nil {
		e.registry.spoffits = make(map[*spoffitRun]struct{})
	}
	e.registry.spoffits[run] = struct{}{}
	// This world lifetime also owns the pre-publication interval. Transport
	// cancellation before its first producer runs must not orphan a modifier.
	err = run.schedulePollLocked()
	if err != nil {
		deleted, deleteErr := run.deleteLocked()
		if len(deleted) != 0 {
			return nil, errors.New("unpublished Spoffit rollback produced presentation")
		}
		return nil, fmt.Errorf("spoffitLifetime: %w", errors.Join(err, deleteErr))
	}
	return run, nil
}

func (e *spoffitRun) scheduleLocked(producer raknet.ScheduledPacketProducer) error {
	cancel, err := e.packet.ScheduleGroupResult([]raknet.ScheduledPacketProducer{producer}, e.fail)
	if err != nil {
		return fmt.Errorf("spoffitSchedule: %w", err)
	}
	if cancel == nil {
		return errors.New("Spoffit producer cancellation unavailable")
	}
	e.birthCancel = cancel
	return nil
}

func (e *spoffitRun) produceCreator() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	member, isOwner := e.ownerLocked()
	if e.isStopped || !isOwner || member.transportGeneration != e.packet.TransportGeneration || e.runtime.registry.isClosed {
		return nil, errors.New("Spoffit creator ownership ended before publication")
	}
	return clonePendingPackets(e.creatorPackets), nil
}

func (e *spoffitRun) commitCreator() {
	e.runtime.registry.mutex.Lock()
	if e.isStopped || e.isCreatorPublished {
		e.runtime.registry.mutex.Unlock()
		return
	}
	e.isCreatorPublished = true
	e.queueLocked(e.creatorPackets, false)
	e.birthCancel = nil
	err := e.scheduleLocked(raknet.ScheduledPacketProducer{Produce: e.produceBirth, AfterCommit: e.commitBirth})
	e.runtime.registry.mutex.Unlock()
	if err != nil {
		e.fail(fmt.Errorf("spoffitBirthSchedule: %w", err))
	}
}

func (e *spoffitRun) produceBirth() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	member, isOwner := e.ownerLocked()
	if e.isStopped || !isOwner || member.transportGeneration != e.packet.TransportGeneration || !e.isCreatorPublished {
		return nil, errors.New("Spoffit birth ownership ended")
	}
	if e.objectID != 0 {
		return clonePendingPackets(e.birthPackets), nil
	}
	packets, isSpawned, err := e.spawnLocked(member, e.runtime.now())
	if err != nil {
		return nil, fmt.Errorf("spoffitSpawn: %w", err)
	}
	if !isSpawned {
		return nil, nil
	}
	e.birthPackets = packets
	return clonePendingPackets(packets), nil
}

func (e *spoffitRun) commitBirth() {
	e.runtime.registry.mutex.Lock()
	if e.isStopped || e.isBirthPublished {
		e.runtime.registry.mutex.Unlock()
		return
	}
	e.birthCancel = nil
	var err error
	if e.objectID == 0 {
		err = e.scheduleLocked(raknet.ScheduledPacketProducer{Delay: time.Second, Produce: e.produceBirth, AfterCommit: e.commitBirth})
	} else {
		// The source write committed. Observers' reliable FIFO gets the birth
		// before this child can publish any independent world-follow output.
		e.queueLocked(e.birthPackets, false)
		e.isBirthPublished = true
		e.runtime.registry.recordSpoffitBirthLocked(e.requestID, e.objectID, e.ownerObjectID)
		err = e.schedulePollLocked()
	}
	e.runtime.registry.mutex.Unlock()
	if err != nil {
		e.fail(fmt.Errorf("spoffitBirthCommit: %w", err))
	}
}

func (e *spoffitRun) fail(cause error) {
	e.runtime.registry.mutex.Lock()
	if e.isStopped {
		if len(e.removalPackets) != 0 && !e.isRemovalPublished {
			e.queueLocked(e.removalPackets, false)
			e.removalPackets = nil
			e.runtime.registry.recordSpoffitFailureLocked(e.requestID, e.removalRequestID, cause)
		}
		e.runtime.registry.mutex.Unlock()
		return
	}
	if len(e.removalPackets) != 0 && !e.isRemovalPublished {
		// Only the scheduled source could have received this pending batch;
		// observers have no delivery owner until its successful AfterCommit.
		e.queueLocked(e.removalPackets, false)
		e.removalPackets = nil
	}
	packets, err := e.deleteLocked()
	e.queueLocked(packets, true)
	e.runtime.registry.recordSpoffitFailureLocked(e.requestID, e.removalRequestID, cause)
	e.runtime.registry.mutex.Unlock()
	e.runtime.logger.Printf("Spoffit publication failed owner=%d child=%d: %v", e.ownerObjectID, e.objectID, errors.Join(cause, err))
}
