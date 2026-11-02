package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	"github.com/darkspinnet/darkspin/server/zone"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	heroraknet "github.com/darkspinnet/darkspin/server/zone/hero/raknet103"
)

// BeastPetPassive owns a pet-private OD use and checks player-owned ordinary
// usage every half second. Its timer belongs to the world, not a transport.
type beastPetResurrectionRun struct {
	runtime        campaignNPCActionRuntime
	damageRuntime  campaignDamageRuntime
	packet         raknet.Packet
	zone           *zone.Zone
	arenaWorld     *arenaAbilityWorld
	arenaGameID    uint32
	userID         uint64
	memberKey      gameplayMemberKey
	sessionKey     string
	generation     uint64
	ownerObjectID  uint32
	petObjectID    uint32
	startedAt      time.Time
	sourceTime     uint64
	nextPollAt     time.Time
	state          zonecompanion.Resurrection
	signInstanceID uint32
	cancel         func()
	resumeCancel   func()
	resumeRevision uint64
	revision       uint64
}

type beastPetResurrectionStep struct {
	run      *beastPetResurrectionRun
	revision uint64
}

type beastPetResumeStep struct {
	run      *beastPetResurrectionRun
	revision uint64
}

func (e beastPetResumeStep) execute() {
	e.run.resumeAttack(e.revision)
}

func (e gameplayPeerSession) isBeastResurrectionOverdrive(at time.Time) bool {
	// Native nPlayer.IsOverdriveActive reads player state without an HP check.
	return e.binding.IsOverdriveUnlocked && e.overdriveObjectID == e.deployedObjectID &&
		!e.overdriveStartsAt.IsZero() && !at.Before(e.overdriveStartsAt) && at.Before(e.overdriveExpiresAt)
}

func (e gameplayPeerSession) isBeastNormalResurrectionAvailable() bool {
	return e.squad != nil && !e.squad.IsBeastResurrectionUsed() &&
		e.deployedCreatureIndex < uint32(len(e.binding.Creatures)) &&
		e.binding.Creatures[e.deployedCreatureIndex].PassiveAbility == util.HashID("BeastSentinelPassive")
}

func (e *gameplayPeerSession) reserveBeastPetRevival(at time.Time) bool {
	if !e.canBeastPetRevive(at) {
		return false
	}
	e.squad.ReserveResurrection()
	return true
}

func (e *gameplayPeerSession) canBeastPetRevive(at time.Time) bool {
	run := e.beastPetResurrection
	if run == nil || e.squad == nil || run.ownerObjectID != e.deployedObjectID ||
		run.state.IsBurrowing || !run.state.IsEligible(e.isBeastResurrectionOverdrive(at), e.isBeastNormalResurrectionAvailable()) {
		return false
	}
	if run.arenaWorld != nil {
		pet, isFound := run.arenaWorld.companion.Snapshot(run.petObjectID)
		if !isFound || pet.HitPoint <= 0 || pet.OwnerObjectID != run.ownerObjectID {
			return false
		}
		return true
	}
	if e.zone == nil || e.zone.Companion() == nil {
		return false
	}
	pet, isFound := e.zone.Companion().Snapshot(run.petObjectID)
	if !isFound || pet.HitPoint <= 0 || pet.OwnerObjectID != run.ownerObjectID {
		return false
	}
	return true
}

func (e *gameplayPeerSession) startBeastResurrection(
	damageRuntime campaignDamageRuntime, packet raknet.Packet, sessionKey string, timestamp uint64,
) error {
	runtime := damageRuntime.npc
	if runtime.timer == nil || runtime.modifierPool == nil || e.zone == nil || e.beastPetObjectID == 0 {
		return errors.New("beast resurrection runtime unavailable")
	}
	at := runtime.now()
	run := &beastPetResurrectionRun{runtime: runtime, damageRuntime: damageRuntime, packet: packet, zone: e.zone, userID: e.binding.UserID, sessionKey: sessionKey,
		memberKey:  gameplaySessionMemberKey(*e),
		generation: e.generation, ownerObjectID: e.deployedObjectID, petObjectID: e.beastPetObjectID,
		startedAt: at, sourceTime: timestamp, nextPollAt: at.Add(500 * time.Millisecond)}
	err := run.schedule(500 * time.Millisecond)
	if err != nil {
		return fmt.Errorf("passiveSchedule: %w", err)
	}
	e.beastPetResurrection = run
	return nil
}

func (e *beastPetResurrectionRun) schedule(delay time.Duration) error {
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	e.revision++
	step := beastPetResurrectionStep{run: e, revision: e.revision}
	cancel, err := e.runtime.timer.Schedule(max(time.Millisecond, delay), step.execute)
	if err != nil {
		return fmt.Errorf("timerSchedule: %w", err)
	}
	if cancel == nil {
		return errors.New("beast resurrection timer cancellation unavailable")
	}
	e.cancel = cancel
	return nil
}

func (e *beastPetResurrectionRun) timestamp(at time.Time) uint64 {
	return advanceHeroArrivalTimestamp(e.sourceTime, e.startedAt, at)
}

func (e beastPetResurrectionStep) execute() {
	e.run.execute(e.revision)
}

func (e *beastPetResurrectionRun) execute(revision uint64) {
	if e.arenaWorld != nil {
		e.executeArena(revision)
		return
	}
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	if e.revision != revision {
		return
	}
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || peerSession.beastPetResurrection != e {
		for sessionKey, candidate := range e.runtime.registry.sessions {
			if candidate.zone == e.zone && candidate.binding.UserID == e.userID && candidate.generation == e.generation && candidate.beastPetResurrection == e {
				e.sessionKey = sessionKey
				peerSession = candidate
				isFound = true
				break
			}
		}
	}
	var retained *gameplayRetainedSession
	if !isFound || peerSession.beastPetResurrection != e {
		retained = e.runtime.registry.retainedSessions[e.memberKey]
		if retained != nil {
			peerSession = retained.peerSession
			isFound = true
		}
	}
	if !isFound || peerSession.generation != e.generation || peerSession.beastPetResurrection != e ||
		peerSession.deployedObjectID != e.ownerObjectID || peerSession.zone == nil {
		return
	}
	e.cancel = nil
	at := e.runtime.now()
	packets, err := e.advance(&peerSession, at)
	if err != nil {
		e.runtime.logger.Printf("RakNet Savage Ally resurrection failed owner=%d pet=%d: %v", e.ownerObjectID, e.petObjectID, err)
		stopPackets, stopErr := e.stop(&peerSession)
		packets = append(packets, stopPackets...)
		peerSession.beastPetResurrection = nil
		if stopErr != nil {
			e.runtime.logger.Printf("RakNet Savage Ally resurrection cleanup failed pet=%d: %v", e.petObjectID, stopErr)
		}
	}
	if retained != nil {
		retained.peerSession = peerSession
	} else {
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	for sessionKey, candidate := range e.runtime.registry.sessions {
		if candidate.zone != peerSession.zone || candidate.isRejoinPending {
			continue
		}
		targetPackets := gameplayPeerPresentationPackets(packets)
		if candidate.binding.UserID == e.userID {
			targetPackets = packets
		}
		candidate.queueCampaignPackets(targetPackets)
		e.runtime.registry.sessions[sessionKey] = candidate
	}
}

func (e *beastPetResurrectionRun) advance(peerSession *gameplayPeerSession, at time.Time) ([][]byte, error) {
	pet, isFound := peerSession.zone.Companion().Snapshot(e.petObjectID)
	if !isFound || pet.HitPoint <= 0 || pet.OwnerObjectID != e.ownerObjectID {
		packets, err := e.stop(peerSession)
		peerSession.beastPetResurrection = nil
		if err != nil {
			return packets, fmt.Errorf("missingPetStop: %w", err)
		}
		return packets, nil
	}
	packets := make([][]byte, 0)
	if e.state.IsCasting && !e.state.IsHitApplied && !at.Before(e.state.HitAt) {
		healPackets, err := e.hit(peerSession, at)
		if err != nil {
			return nil, fmt.Errorf("castHit: %w", err)
		}
		packets = append(packets, healPackets...)
		e.state.IsHitApplied = true
	}
	if e.state.IsCasting && !at.Before(e.state.ReleaseAt) {
		e.state.IsCasting = false
		err := peerSession.zone.Companion().ControlAbility(e.petObjectID, at, time.Time{}, false, "")
		if err != nil {
			return nil, fmt.Errorf("castRelease: %w", err)
		}
		if peerSession.deployedHitPoint() <= 0 && peerSession.squad.IsResurrectionPending() {
			err = e.failRevival(peerSession, at)
			if err != nil {
				return nil, fmt.Errorf("castFailure: %w", err)
			}
		}
		if peerSession.deployedHitPoint() > 0 {
			resumeErr := e.scheduleResume()
			if resumeErr != nil {
				return nil, fmt.Errorf("attackSchedule: %w", resumeErr)
			}
		}
	}
	if e.state.IsBurrowing && !at.Before(e.state.ReleaseAt) {
		deletePackets, deleteErr := peerSession.stopBeastPet()
		if deleteErr != nil {
			return nil, fmt.Errorf("burrowDelete: %w", deleteErr)
		}
		return deletePackets, nil
	}
	if !at.Before(e.nextPollAt) {
		e.nextPollAt = at.Add(500 * time.Millisecond)
		isOverdrive := peerSession.isBeastResurrectionOverdrive(at)
		isEligible := e.state.IsEligible(isOverdrive, peerSession.isBeastNormalResurrectionAvailable())
		if peerSession.deployedHitPoint() > 0 {
			signPackets, err := e.syncSign(isEligible, at)
			if err != nil {
				return nil, fmt.Errorf("signSync: %w", err)
			}
			packets = append(packets, signPackets...)
		} else if !e.state.IsCasting && !e.state.IsBurrowing && !pet.IsAbilityControlled {
			if isEligible {
				castPackets, err := e.begin(peerSession, at, isOverdrive)
				if err != nil {
					return nil, fmt.Errorf("castBegin: %w", err)
				}
				packets = append(packets, castPackets...)
			} else {
				burrowPackets, err := e.burrow(peerSession, at)
				if err != nil {
					return nil, fmt.Errorf("burrowBegin: %w", err)
				}
				packets = append(packets, burrowPackets...)
			}
		}
		e.state.ObserveOverdrive(isOverdrive)
	}
	next := e.nextPollAt
	if e.state.IsCasting {
		next = minTime(next, e.state.ReleaseAt)
		if !e.state.IsHitApplied {
			next = minTime(next, e.state.HitAt)
		}
	}
	if e.state.IsBurrowing {
		next = minTime(next, e.state.ReleaseAt)
	}
	err := e.schedule(next.Sub(at))
	if err != nil {
		return nil, fmt.Errorf("nextSchedule: %w", err)
	}
	return packets, nil
}

func minTime(left time.Time, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}

func (e *beastPetResurrectionRun) begin(peerSession *gameplayPeerSession, at time.Time, isOverdrive bool) ([][]byte, error) {
	if !e.state.Begin(at, isOverdrive) {
		return nil, nil
	}
	peerSession.squad.ReserveResurrection()
	if !isOverdrive {
		peerSession.squad.ConsumeBeastResurrection()
	}
	err := peerSession.syncZoneSquadCheckpoint()
	if err != nil {
		return nil, fmt.Errorf("useCheckpoint: %w", err)
	}
	activation, isFound := peerSession.sagePassiveActivations[e.petObjectID]
	if isFound && activation.Attack != nil {
		activation.Attack.Stop()
		activation.Attack = nil
		peerSession.sagePassiveActivations[e.petObjectID] = activation
	}
	err = peerSession.zone.Companion().ControlAbility(e.petObjectID, at, e.state.ReleaseAt, true, "beastsentinel_pet_res")
	if err != nil {
		return nil, fmt.Errorf("castControl: %w", err)
	}
	packets, err := e.animation("beastsentinel_pet_res", at)
	if err != nil {
		return nil, fmt.Errorf("castAnimation: %w", err)
	}
	return packets, nil
}

func (e *beastPetResurrectionRun) animation(name string, at time.Time) ([][]byte, error) {
	stop, err := raknet.MarshalApplication(raknet.ObjectPlayerMoveMessage{ObjectID: e.petObjectID, GoalFlags: 0x20})
	if err != nil {
		return nil, fmt.Errorf("petStop: %w", err)
	}
	animation, err := raknet.MarshalApplication(raknet.SetAnimationStateMessage{ObjectID: e.petObjectID, State: util.HashID(name), Timestamp: e.timestamp(at), Scale: 1})
	if err != nil {
		return nil, fmt.Errorf("petAnimation: %w", err)
	}
	return [][]byte{stop, animation}, nil
}

func (e *beastPetResurrectionRun) hit(peerSession *gameplayPeerSession, at time.Time) ([][]byte, error) {
	if peerSession.deployedHitPoint() > 0 {
		if peerSession.squad.IsResurrectionPending() {
			err := peerSession.squad.CancelResurrection()
			if err != nil {
				return nil, fmt.Errorf("livingCancel: %w", err)
			}
			err = peerSession.syncZoneSquadCheckpoint()
			if err != nil {
				return nil, fmt.Errorf("livingSync: %w", err)
			}
		}
		return nil, nil
	}
	fraction := float32(0.25)
	if peerSession.isBeastResurrectionOverdrive(at) {
		fraction = 0.5
	}
	hitPoint := peerSession.characterHitPointMaximum(peerSession.deployedCreatureIndex) * fraction
	previousSquad := peerSession.squad
	nextSquad := *previousSquad
	err := nextSquad.ResurrectCharacter(peerSession.deployedCreatureIndex, hitPoint)
	if err != nil {
		return nil, fmt.Errorf("ownerHealth: %w", err)
	}
	resource, err := peerSession.marshalCampaignCharacterResourceValues(peerSession.deployedCreatureIndex, hitPoint, peerSession.deployedManaPoint())
	if err != nil {
		return nil, fmt.Errorf("ownerResource: %w", err)
	}
	world, err := heroraknet.HitPoint(e.ownerObjectID, hitPoint)
	if err != nil {
		return nil, fmt.Errorf("ownerWorld: %w", err)
	}
	heal, err := raknet.MarshalApplication(raknet.DamageCombatEventMessage{Flags: 0x0002, DeltaHealth: -hitPoint, TargetID: e.ownerObjectID, SourceID: e.petObjectID, IntegerHPChange: -int32(hitPoint)})
	if err != nil {
		return nil, fmt.Errorf("ownerHeal: %w", err)
	}
	// Native HealDamage emits a distinct resurrection combat event after a
	// positive heal crosses from dead to alive (sub_9E5870, flags 0x100).
	revive, err := raknet.MarshalApplication(raknet.CombatEventMessage{
		Flags: 0x0100, TargetID: e.ownerObjectID, SourceID: e.petObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("ownerRevive: %w", err)
	}
	peerSession.squad = &nextSquad
	err = peerSession.syncZoneHero()
	if err == nil {
		err = peerSession.syncZoneSquadCheckpoint()
	}
	if err != nil {
		peerSession.squad = previousSquad
		heroErr := peerSession.syncZoneHero()
		checkpointErr := peerSession.syncZoneSquadCheckpoint()
		return nil, fmt.Errorf("reviveSync: %w", errors.Join(err, heroErr, checkpointErr))
	}
	peerSession.isHeroSelectionPending = false
	peerSession.isHeroSelectionScheduled = false
	peerSession.heroSelectionReadyAt = time.Time{}
	peerSession.heroInputLockedObjectID = 0
	peerSession.heroInputLockedUntil = time.Time{}
	peerSession.isPartyDefeatQueued = false
	return [][]byte{heal, revive, resource, world}, nil
}

func (e *beastPetResurrectionRun) burrow(peerSession *gameplayPeerSession, at time.Time) ([][]byte, error) {
	if peerSession.squad.IsResurrectionPending() {
		err := e.failRevival(peerSession, at)
		if err != nil {
			return nil, fmt.Errorf("burrowFailure: %w", err)
		}
	}
	e.state.IsBurrowing = true
	e.state.ReleaseAt = at.Add(2200 * time.Millisecond)
	err := peerSession.zone.Companion().ControlAbility(e.petObjectID, at, e.state.ReleaseAt, true, "beastsentinel_pet_burrow")
	if err != nil {
		return nil, fmt.Errorf("burrowControl: %w", err)
	}
	packets, err := e.animation("beastsentinel_pet_burrow", at)
	if err != nil {
		return nil, fmt.Errorf("burrowAnimation: %w", err)
	}
	return packets, nil
}

func (e *beastPetResurrectionRun) failRevival(peerSession *gameplayPeerSession, at time.Time) error {
	err := peerSession.squad.CancelResurrection()
	if err != nil {
		return fmt.Errorf("squadCancel: %w", err)
	}
	if peerSession.deployedHitPoint() <= 0 && peerSession.squad.LivingCount() > 0 {
		peerSession.isHeroSelectionPending = true
		peerSession.isHeroSelectionScheduled = false
		peerSession.heroSelectionReadyAt = at.Add(campaignHeroDeathSelectionDelay)
	}
	err = peerSession.syncZoneSquadCheckpoint()
	if err != nil {
		return fmt.Errorf("cancelCheckpoint: %w", err)
	}
	return nil
}

func (e *beastPetResurrectionRun) stop(peerSession *gameplayPeerSession) ([][]byte, error) {
	if e.arenaWorld != nil {
		return e.stopArena(peerSession)
	}
	e.revision++
	e.resumeRevision++
	if e.resumeCancel != nil {
		e.resumeCancel()
		e.resumeCancel = nil
	}
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	packets, err := e.removeSign()
	if peerSession.zone != nil && peerSession.zone.Companion() != nil {
		pet, isFound := peerSession.zone.Companion().Snapshot(e.petObjectID)
		if isFound && pet.HitPoint > 0 && pet.IsAbilityControlled && pet.UserID == e.userID &&
			pet.PeerGeneration == e.generation && pet.OwnerObjectID == e.ownerObjectID {
			controlErr := peerSession.zone.Companion().ControlAbility(e.petObjectID, e.runtime.now(), time.Time{}, false, "")
			err = errors.Join(err, controlErr)
		}
	}
	if peerSession.squad != nil && peerSession.squad.IsResurrectionPending() {
		cancelErr := e.failRevival(peerSession, e.runtime.now())
		err = errors.Join(err, cancelErr)
	}
	if err != nil {
		return packets, fmt.Errorf("passiveStop: %w", err)
	}
	return packets, nil
}

func (e *beastPetResurrectionRun) scheduleResume() error {
	if e.resumeCancel != nil {
		e.resumeCancel()
		e.resumeCancel = nil
	}
	e.resumeRevision++
	step := beastPetResumeStep{run: e, revision: e.resumeRevision}
	cancel, err := e.runtime.timer.Schedule(time.Millisecond, step.execute)
	if err != nil {
		return fmt.Errorf("resumeTimer: %w", err)
	}
	if cancel == nil {
		return errors.New("pet attack cancellation unavailable")
	}
	e.resumeCancel = cancel
	return nil
}

func (e *beastPetResurrectionRun) resumeAttack(revision uint64) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.resumeRevision == revision && isFound && peerSession.beastPetResurrection == e && peerSession.generation == e.generation && peerSession.deployedHitPoint() > 0 && e.packet.Address.String() == e.sessionKey
	packet := e.packet
	sessionKey := e.sessionKey
	generation := e.generation
	transportGeneration := peerSession.transportGeneration
	timestamp := e.timestamp(e.runtime.now())
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return
	}
	packets, err := e.damageRuntime.startBeastPetAttackWithResume(packet, sessionKey, generation, timestamp,
		e, revision, transportGeneration)
	if err != nil {
		e.runtime.logger.Printf("RakNet Savage Ally resumed attack failed pet=%d: %v", e.petObjectID, err)
		return
	}
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	current, isCurrentFound := e.runtime.registry.sessions[sessionKey]
	if e.resumeRevision != revision || !isCurrentFound || current.beastPetResurrection != e ||
		current.generation != generation || current.transportGeneration != transportGeneration {
		return
	}
	for sessionKey, candidate := range e.runtime.registry.sessions {
		if candidate.zone == e.zone && !candidate.isRejoinPending {
			candidate.queueCampaignPackets(packets)
			e.runtime.registry.sessions[sessionKey] = candidate
		}
	}
}
