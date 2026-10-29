//go:build scenario

package gameplay

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/scenario"
)

func (e *gameplaySessionRegistry) validateScenarioSpoffitLocked(member gameplayPeerSession, req scenario.SpawnRequest) error {
	if req.NounName != spoffitNoun || req.Owner != nil || req.IsFixture || req.IsAIEnabled != nil || member.deployedHitPoint() <= 0 {
		return errors.New("SpoffitCrystal requires its exact named real-hero context")
	}
	profile, err := spoffitProfile(member)
	if err != nil {
		return fmt.Errorf("spoffitInstalled: %w", err)
	}
	if profile.HitPoint != 25 {
		return errors.New("Spoffit installed own health unavailable")
	}
	var active *spoffitRun
	for run := range e.spoffits {
		if run.world == member.zone && run.userID == member.binding.UserID && run.generation == member.generation && run.ownerObjectID == member.deployedObjectID && !run.isStopped {
			active = run
			break
		}
	}
	if req.IsSpoffitRemoval {
		if active == nil || !active.isBirthPublished || active.isBurrowing || active.removalRequestID != "" {
			return errors.New("Spoffit removal requires this hero's active published crystal")
		}
		return nil
	}
	if active != nil {
		return errors.New("SpoffitCrystal is already active; unknown activation must not be replayed")
	}
	for _, receipt := range e.spawnReceipts {
		if receipt.IsSpoffitCrystal && !receipt.IsSpoffitRemoval && receipt.UserID == int64(member.binding.UserID) &&
			receipt.GameID == member.binding.GameID && receipt.PeerGeneration == member.generation &&
			receipt.HeroOwnerObjectID == member.deployedObjectID && receipt.State != "retired" && receipt.Failure == "" {
			return errors.New("SpoffitCrystal already has an outstanding owned activation")
		}
	}
	return nil
}

func (e gameplayPendingRuntime) consumeScenarioSpoffit(req raknet.Packet, queued gameplayPeerSession, receipt scenario.SpawnReceipt) ([][]byte, bool, error) {
	e.registry.mutex.Lock()
	member, isFound := e.registry.sessions[req.Address.String()]
	if !isFound || member.generation != queued.generation || member.transportGeneration != receipt.TransportGeneration || member.deployedObjectID != receipt.HeroOwnerObjectID || member.zone != queued.zone {
		e.registry.mutex.Unlock()
		return nil, true, errors.New("Spoffit real hero ownership changed before consumption")
	}
	err := e.registry.validateScenarioSpawnLocked(receipt.RequestID, member)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, true, fmt.Errorf("spoffitRequest: %w", err)
	}
	if receipt.IsSpoffitRemoval {
		var active *spoffitRun
		for run := range e.registry.spoffits {
			if run.world == member.zone && run.userID == member.binding.UserID && run.generation == member.generation && run.ownerObjectID == member.deployedObjectID && run.isBirthPublished && !run.isStopped && !run.isBurrowing && run.removalRequestID == "" {
				active = run
				break
			}
		}
		if active == nil {
			e.registry.mutex.Unlock()
			return nil, true, errors.New("Spoffit crystal ended before removal consumption")
		}
		active.removalRequestID = receipt.RequestID
		err = req.AfterResponseCommit(active.commitRemoval)
		if err == nil {
			receipt.IsAdmitted = true
			receipt.State = "admitted"
			receipt.ObjectID = active.objectID
			e.registry.spawnReceipts[receipt.RequestID] = receipt
		} else {
			active.removalRequestID = ""
		}
		e.registry.mutex.Unlock()
		if err != nil {
			return nil, true, fmt.Errorf("spoffitRemovalCommit: %w", err)
		}
		return nil, true, nil
	}
	run, err := e.damage.npc.newSpoffitLocked(member, req, receipt.RequestID)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, true, fmt.Errorf("spoffitActivate: %w", err)
	}
	receipt.IsAdmitted = true
	receipt.State = "admitted"
	e.registry.spawnReceipts[receipt.RequestID] = receipt
	err = run.scheduleLocked(raknet.ScheduledPacketProducer{Produce: run.produceCreator, AfterCommit: run.commitCreator})
	e.registry.mutex.Unlock()
	if err != nil {
		run.fail(err)
		return nil, true, fmt.Errorf("spoffitCreatorSchedule: %w", err)
	}
	return nil, true, nil
}

func (e *gameplaySessionRegistry) recordSpoffitBirthLocked(requestID string, objectID, ownerID uint32) {
	receipt, isFound := e.spawnReceipts[requestID]
	if !isFound || !receipt.IsSpoffitCrystal || receipt.IsSpoffitRemoval {
		return
	}
	receipt.ObjectID = objectID
	receipt.HeroOwnerObjectID = ownerID
	receipt.IsPublished = true
	receipt.State = "published"
	e.spawnReceipts[requestID] = receipt
}

func (e *gameplaySessionRegistry) recordSpoffitRemovalLocked(requestID string, objectID, ownerID uint32, isRetired bool) {
	receipt, isFound := e.spawnReceipts[requestID]
	if !isFound || !receipt.IsSpoffitCrystal {
		return
	}
	receipt.ObjectID = objectID
	receipt.HeroOwnerObjectID = ownerID
	if isRetired {
		receipt.State = "retired"
	} else {
		receipt.State = "retiring"
	}
	e.spawnReceipts[requestID] = receipt
}

func (e *gameplaySessionRegistry) recordSpoffitFailureLocked(requestID, removalID string, cause error) {
	for _, id := range []string{requestID, removalID} {
		receipt, isFound := e.spawnReceipts[id]
		if isFound {
			receipt.State = "failed"
			receipt.Failure = cause.Error()
			e.spawnReceipts[id] = receipt
		}
	}
}
