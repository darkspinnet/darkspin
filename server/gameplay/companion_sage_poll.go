package gameplay

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	"github.com/darkspinnet/darkspin/server/zone"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

// Native Tick yields each simulation frame. This is the existing server's
// companion motion cadence, independent of a connection's packet scheduler.
const sagePassivePollDelay = 50 * time.Millisecond
const sageSpawnDuration = 2 * time.Second
const sageSpawnAnimation = "lifetempest_pet_spawn"

type sagePassivePoll struct {
	mutex         sync.Mutex
	isStopped     bool
	isArmed       bool
	runtime       gameplaySwitchRuntime
	run           *summonPassiveRun
	zone          *zone.Zone
	userID        uint64
	generation    uint64
	ownerObjectID uint32
	startedAt     time.Time
	sourceTime    uint64
	cancel        func()
	missingSince  time.Time
	spawnReleases map[uint32]time.Time
}

func (e gameplaySwitchPassiveSchedule) prepareWorldPoll(packet raknet.Packet) (*sagePassivePoll, error) {
	if e.runtime.npc.timer == nil {
		return nil, errors.New("Sage world timer unavailable")
	}
	e.runtime.registry.mutex.Lock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || member.generation != e.sessionGeneration || member.sagePassive != e.run ||
		member.deployedObjectID != e.targetObjectID || member.zone == nil {
		e.runtime.registry.mutex.Unlock()
		return nil, errors.New("Sage world owner unavailable")
	}
	poll := &sagePassivePoll{runtime: e.runtime, run: e.run, zone: member.zone,
		userID: member.binding.UserID, generation: member.generation,
		ownerObjectID: member.deployedObjectID, startedAt: e.runtime.now(), sourceTime: packet.SourceTime}
	poll.spawnReleases = make(map[uint32]time.Time)
	if !e.startedAt.IsZero() {
		poll.startedAt = e.startedAt
	}
	member.sagePassiveSchedule = poll
	e.run.AddCancel(poll.stop)
	e.runtime.registry.sessions[e.sessionKey] = member
	e.runtime.registry.mutex.Unlock()
	if e.isScheduledCommit {
		return poll, nil
	}
	err := packet.AfterResponseCommit(poll.commit)
	if err != nil {
		return poll, fmt.Errorf("sagePollCommit: %w", err)
	}
	return poll, nil
}

func (e *sagePassivePoll) commit() {
	e.mutex.Lock()
	if e.isStopped || e.isArmed {
		e.mutex.Unlock()
		return
	}
	e.isArmed = true
	e.mutex.Unlock()
	err := e.schedule()
	if err != nil {
		e.runtime.logger.Printf("RakNet Sage committed poll scheduling failed owner=%d: %v", e.ownerObjectID, err)
	}
}

// Scheduled OD publications have a producer AfterCommit rather than the
// original action packet's already-consumed AfterResponseCommit list.
func (e gameplaySwitchRuntime) commitSagePassive(sessionKey string, generation uint64, expected *sagePassivePoll) {
	e.registry.mutex.RLock()
	defer e.registry.mutex.RUnlock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || expected == nil || member.generation != generation || member.sagePassiveSchedule != expected ||
		member.deployedObjectID != expected.ownerObjectID || member.sagePassive != expected.run {
		return
	}
	expected.commit()
}

func (e gameplayPeerSession) commitSagePassiveReplay() {
	poll := e.sagePassiveSchedule
	if poll == nil || !poll.matches(e) {
		return
	}
	poll.commit()
}

func (e *sagePassivePoll) schedule() error {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.isStopped {
		return nil
	}
	cancel, err := e.runtime.npc.timer.Schedule(sagePassivePollDelay, e.execute)
	if err != nil {
		return fmt.Errorf("sagePollTimer: %w", err)
	}
	if cancel == nil {
		return errors.New("Sage world timer cancellation unavailable")
	}
	e.cancel = cancel
	return nil
}

func (e *sagePassivePoll) stop() {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.isStopped = true
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
}

func (e *sagePassivePoll) matches(member gameplayPeerSession) bool {
	return member.zone == e.zone && member.binding.UserID == e.userID &&
		member.generation == e.generation && member.deployedObjectID == e.ownerObjectID &&
		member.sagePassive == e.run && member.sagePassiveSchedule == e
}

func (e *sagePassivePoll) execute() {
	e.mutex.Lock()
	isStopped := e.isStopped
	e.cancel = nil
	e.mutex.Unlock()
	if isStopped {
		return
	}
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	at := e.runtime.now()
	for sessionKey, member := range e.runtime.registry.sessions {
		if !e.matches(member) {
			continue
		}
		e.missingSince = time.Time{}
		packets, err := e.advance(&member, at)
		e.runtime.registry.sessions[sessionKey] = member
		e.publishLocked(packets)
		e.finish(err)
		return
	}
	for _, retained := range e.runtime.registry.retainedSessions {
		if retained == nil || !e.matches(retained.peerSession) {
			continue
		}
		e.missingSince = time.Time{}
		packets, err := e.advance(&retained.peerSession, at)
		e.publishLocked(packets)
		e.finish(err)
		return
	}
	// Disconnect cleanup and endpoint handoff briefly remove the session from
	// both collections. Keep the original clock while that transfer finishes.
	if e.missingSince.IsZero() {
		e.missingSince = at
	}
	if at.Sub(e.missingSince) < gameplayRejoinGrace && !e.runtime.registry.isClosed {
		e.finish(nil)
		return
	}
	requests, err := e.run.Stop()
	if err != nil {
		e.runtime.logger.Printf("RakNet Sage orphan cleanup failed owner=%d requests=%d: %v", e.ownerObjectID, len(requests), err)
	}
}

func (e *sagePassivePoll) finish(cause error) {
	if cause != nil {
		e.runtime.logger.Printf("RakNet Sage world update failed owner=%d: %v", e.ownerObjectID, cause)
	}
	err := e.schedule()
	if err != nil {
		e.runtime.logger.Printf("RakNet Sage world scheduling failed owner=%d: %v", e.ownerObjectID, err)
	}
}

func (e *sagePassivePoll) publishLocked(packets [][]byte) {
	if len(packets) == 0 {
		return
	}
	for sessionKey, candidate := range e.runtime.registry.sessions {
		if candidate.zone != e.zone || candidate.isRejoinPending || candidate.isZoneTerminal() {
			continue
		}
		candidate.queueCampaignPackets(packets)
		candidate.isNPCRecoveryPending = true
		e.runtime.registry.sessions[sessionKey] = candidate
	}
}

func (e *sagePassivePoll) advance(member *gameplayPeerSession, at time.Time) ([][]byte, error) {
	if member.deployedHitPoint() <= 0 || member.isZoneTerminal() {
		return member.stopCampaignSagePassive()
	}
	for objectID, releaseAt := range e.spawnReleases {
		if at.Before(releaseAt) {
			continue
		}
		isReleased := e.zone.Companion().ReleaseRecall(objectID, sageSpawnAnimation)
		if isReleased {
			member.isNPCRecoveryPending = true
		}
		delete(e.spawnReleases, objectID)
	}
	actors := make(map[uint32]zonecompanion.Actor)
	for _, snapshot := range e.zone.Companion().MotionSnapshots(at) {
		actors[snapshot.Actor.ObjectID] = snapshot.Actor
	}
	packets := make([][]byte, 0)
	for _, slot := range e.run.Slots() {
		if !slot.IsAlive {
			continue
		}
		actor, isFound := actors[slot.ObjectID]
		isSeparated := isFound && actor.HitPoint > 0 &&
			zonegeometry.Distance(actor.Position, zonePosition(member.playerPosition)) > e.run.Definition().OwnerDistance
		if isFound && actor.HitPoint > 0 && !isSeparated {
			continue
		}
		requests, err := member.retireSageCompanion(slot.ObjectID, isSeparated)
		if err != nil {
			return packets, fmt.Errorf("sageSlotRetire: %w", err)
		}
		for _, req := range requests {
			encoded, activation, marshalErr := marshalSummonPassiveWorldRequest(req, raknet.Vector3{})
			if marshalErr != nil {
				return packets, fmt.Errorf("sageSlotDelete: %w", marshalErr)
			}
			if activation != nil {
				return packets, errors.New("Sage deletion produced spawn")
			}
			packets = append(packets, encoded...)
		}
	}
	requests, err := e.run.Advance(max(time.Duration(0), at.Sub(e.startedAt)))
	if err != nil {
		return packets, fmt.Errorf("sageRefill: %w", err)
	}
	spawnPackets, err := e.spawnLocked(member, requests, at)
	if err != nil {
		return packets, fmt.Errorf("sageSpawn: %w", err)
	}
	return append(packets, spawnPackets...), nil
}

func (e *gameplayPeerSession) retireSageCompanion(objectID uint32, isSeparated bool) ([]summonPassiveWorldRequest, error) {
	if e.sagePassive == nil || e.zone == nil || e.zone.Companion() == nil {
		return nil, errors.New("Sage retirement unavailable")
	}
	replacementID, err := e.reserveCampaignObjectID()
	if err != nil {
		return nil, fmt.Errorf("sageReplacementID: %w", err)
	}
	requests := []summonPassiveWorldRequest(nil)
	if isSeparated {
		requests, err = e.sagePassive.CompanionSeparated(objectID)
	} else {
		err = e.sagePassive.CompanionDied(objectID)
	}
	if err != nil {
		return nil, fmt.Errorf("sageVacate: %w", err)
	}
	err = e.sagePassive.ReplaceCompanion(objectID, replacementID)
	if err != nil {
		return requests, fmt.Errorf("sageReplace: %w", err)
	}
	activation := e.sagePassiveActivations[objectID]
	if activation.Attack != nil {
		activation.Attack.Stop()
	}
	e.cancelPetPlasmaBurn(objectID)
	e.zone.Companion().Remove(objectID)
	delete(e.sagePassiveActivations, objectID)
	return requests, nil
}

func (e *sagePassivePoll) spawnLocked(member *gameplayPeerSession, requests []summonPassiveWorldRequest, at time.Time) ([][]byte, error) {
	if len(requests) == 0 {
		return nil, nil
	}
	hitPoint := member.petHitPoint(e.runtime.program.NonPlayerHitPoint[util.HashID("HelperMelee")])
	footprintRadius, err := e.runtime.program.FootprintRadius("HelperMelee.Noun")
	if err != nil {
		return nil, fmt.Errorf("sageFootprint: %w", err)
	}
	if hitPoint <= 0 || footprintRadius <= 0 {
		return nil, errors.New("Sage companion profile unavailable")
	}
	packets := make([][]byte, 0)
	if member.sagePassiveActivations == nil {
		member.sagePassiveActivations = make(map[uint32]summonCompanionActivation)
	}
	for index, req := range requests {
		if req.Spawn == nil {
			return packets, errors.New("Sage refill missing spawn")
		}
		position, placementErr := summonPassiveSpawnPosition(member.playerPosition, index, len(requests), req.Spawn.SpawnRadius)
		if placementErr != nil {
			return packets, fmt.Errorf("sagePlacement: %w", placementErr)
		}
		encoded, activation, marshalErr := marshalSummonPassiveWorldRequest(req, position)
		if marshalErr != nil {
			return packets, fmt.Errorf("sageMarshal: %w", marshalErr)
		}
		if activation == nil {
			return packets, errors.New("Sage spawn activation unavailable")
		}
		err = member.zone.Companion().Put(zonecompanion.Actor{
			UserID: member.binding.UserID, PeerGeneration: member.generation,
			ObjectID: req.ObjectID, OwnerObjectID: req.OwnerObjectID,
			Noun: util.HashID(req.Spawn.NounName), Position: game.Vec3{X: position.X, Y: position.Y, Z: position.Z},
			FootprintRadius: footprintRadius, HitPoint: hitPoint, MaximumHitPoint: hitPoint,
			IsTargetable: true, IsCombatant: true, IsSageCompanion: true,
		})
		if err != nil {
			return packets, fmt.Errorf("sageWorldPut: %w", err)
		}
		member.sagePassiveActivations[req.ObjectID] = *activation
		packets = append(packets, encoded...)
		maximumPacket, maximumErr := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
			ObjectID: req.ObjectID, Value: map[uint8]float32{4: hitPoint},
		})
		if maximumErr != nil {
			return packets, fmt.Errorf("sageMaximum: %w", maximumErr)
		}
		resourcePacket, resourceErr := raknet.MarshalApplication(raknet.CombatantDataUpdateMessage{
			ObjectID: req.ObjectID, HitPoints: hitPoint,
		})
		if resourceErr != nil {
			return packets, fmt.Errorf("sageResource: %w", resourceErr)
		}
		packets = append(packets, maximumPacket, resourcePacket)
		err = member.zone.Companion().Recall(req.ObjectID, at, at.Add(sageSpawnDuration), sageSpawnAnimation)
		if err != nil {
			return packets, fmt.Errorf("sageSpawnControl: %w", err)
		}
		animation, animationErr := raknet.MarshalApplication(raknet.SetAnimationStateMessage{
			ObjectID: req.ObjectID, State: util.HashID(sageSpawnAnimation),
			Timestamp: advanceHeroArrivalTimestamp(e.sourceTime, e.startedAt, at), Scale: 1,
		})
		if animationErr != nil {
			return packets, fmt.Errorf("sageSpawnAnimation: %w", animationErr)
		}
		packets = append(packets, animation)
		e.spawnReleases[req.ObjectID] = at.Add(sageSpawnDuration)
	}
	return packets, nil
}
