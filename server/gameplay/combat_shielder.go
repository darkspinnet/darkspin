package gameplay

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type campaignNomadShielderSetup struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	objectID   uint32
	timestamp  uint64
	profile    zonenpc.NomadShielderShieldProfile
}

func (e campaignNomadShielderSetup) fail(
	step string, err error,
) ([][]byte, error) {
	e.rollback()
	e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
	return nil, fmt.Errorf("%s: %w", step, err)
}

const campaignNomadShielderShieldPoll = 50 * time.Millisecond

type campaignNomadShielderShieldRun struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	objectID   uint32
	timestamp  uint64
	profile    zonenpc.NomadShielderShieldProfile
	effectSlot uint8
	mutex      sync.Mutex
	cancel     raknet.CancelSchedule
}

func (e *campaignNomadShielderShieldRun) fail(
	step string, err error,
) ([][]byte, error) {
	e.stop()
	e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
	return nil, fmt.Errorf("%s: %w", step, err)
}

func (e *campaignNomadShielderShieldRun) schedulePoll() error {
	producer := raknet.ScheduledPacketProducer{
		Delay: campaignNomadShielderShieldPoll, Produce: e.poll,
	}
	cancel, err := e.packet.ScheduleProducers([]raknet.ScheduledPacketProducer{producer})
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		return fmt.Errorf("shielderShieldPollSchedule: %w", err)
	}
	e.mutex.Lock()
	e.cancel = cancel
	e.mutex.Unlock()
	return nil
}

func (e *campaignNomadShielderShieldRun) poll() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.campaignNPCShielderShields[e.objectID] == e &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	enemy, isEnemyFound := zonenpc.Snapshot{}, false
	if isCurrent {
		enemy, isEnemyFound = peerSession.zone.NPCs().NPC(e.objectID)
	}
	if isCurrent && isEnemyFound && !enemy.IsDefeated && enemy.IsShieldActive {
		e.timestamp += uint64(campaignNomadShielderShieldPoll / time.Millisecond)
		e.runtime.registry.mutex.Unlock()
		err := e.schedulePoll()
		if err != nil {
			return e.finishAfterPollFailure(err)
		}
		return nil, nil
	}
	if isCurrent {
		delete(peerSession.campaignNPCShielderShields, e.objectID)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	return e.finish(isCurrent && isEnemyFound && !enemy.IsDefeated)
}

func (e *campaignNomadShielderShieldRun) finishAfterPollFailure(
	pollErr error,
) ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.campaignNPCShielderShields[e.objectID] == e &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	if isCurrent {
		_ = peerSession.zone.NPCs().EndShield(e.objectID)
		delete(peerSession.campaignNPCShielderShields, e.objectID)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if e.runtime.logger != nil {
		e.runtime.logger.Printf(
			"Nomad Shielder shield monitor stopped after schedule failure for %s/%d: %v",
			e.sessionKey, e.objectID, pollErr,
		)
	}
	packets, err := e.finish(isCurrent)
	if err != nil {
		return nil, fmt.Errorf("shielderShieldPollFinish: %w", err)
	}
	return packets, nil
}

func (e *campaignNomadShielderShieldRun) finish(isAlive bool) ([][]byte, error) {
	e.mutex.Lock()
	e.cancel = nil
	e.mutex.Unlock()
	isEffectOwned := e.runtime.effectPool.Release(e.objectID, e.effectSlot)
	packets := make([][]byte, 0, 2)
	if isEffectOwned {
		removePacket, err := npcraknet.ShieldEffectAsset(
			e.objectID, e.effectSlot, e.profile.EffectName, true,
		)
		if err != nil {
			e.runtime.logger.Printf(
				"RakNet shielder effect removal omitted object=%d: %v",
				e.objectID, err,
			)
		} else {
			packets = append(packets, removePacket)
		}
	}
	if !isAlive {
		e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
		return packets, nil
	}
	stopTimestamp := e.timestamp +
		uint64(campaignNomadShielderShieldPoll/time.Millisecond)
	stopPacket, err := npcraknet.ShieldAnimation(
		e.objectID, e.profile.StopAnimationName, stopTimestamp,
	)
	if err != nil {
		e.runtime.logger.Printf(
			"RakNet shielder stop animation omitted object=%d: %v",
			e.objectID, err,
		)
	} else {
		packets = append(packets, stopPacket)
	}
	step := campaignNPCStunResumeStep{
		timestamp: stopTimestamp +
			uint64(e.profile.StopReleaseDelay/time.Millisecond),
		resume: e.resume,
	}
	cancel, scheduleErr := e.packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: e.profile.StopReleaseDelay, Produce: step.produce,
	}})
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		fallbackPackets, fallbackErr := e.resume(step.timestamp)
		if fallbackErr != nil {
			return e.fail(
				"shielderShieldStopFallback", errors.Join(scheduleErr, fallbackErr),
			)
		}
		return append(packets, fallbackPackets...), nil
	}
	return packets, nil
}

func (e *campaignNomadShielderShieldRun) resume(
	timestamp uint64,
) ([][]byte, error) {
	packets, err := e.runtime.produceEnemyMelee(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
	if err != nil {
		return e.fail("shielderShieldResume", err)
	}
	return packets, nil
}

func (e *campaignNomadShielderShieldRun) stop() {
	if e == nil {
		return
	}
	e.mutex.Lock()
	cancel := e.cancel
	e.cancel = nil
	e.mutex.Unlock()
	if cancel != nil {
		cancel()
	}
	e.runtime.effectPool.Release(e.objectID, e.effectSlot)
}

func (e campaignNomadShielderSetup) start() ([][]byte, error) {
	if !e.isCurrent() {
		return nil, nil
	}
	packet, err := npcraknet.ShieldAnimation(
		e.objectID, e.profile.SetupAnimationName,
		e.timestamp+uint64(e.profile.SetupWait/time.Millisecond),
	)
	if err != nil {
		return e.fail("shielderSetupAnimation", err)
	}
	return [][]byte{packet}, nil
}

func (e campaignNomadShielderSetup) activate() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.isCampaignNPCSourceActive(e.generation, e.objectID) &&
		peerSession.campaignNPCShielderShieldSetups[e.objectID]
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	idlePacket, err := npcraknet.ShieldAnimation(
		e.objectID, e.profile.IdleAnimationName,
		e.timestamp+uint64((e.profile.SetupWait+e.profile.SetupHitDelay)/time.Millisecond),
	)
	if err != nil {
		delete(peerSession.campaignNPCShielderShieldSetups, e.objectID)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
		e.runtime.registry.mutex.Unlock()
		return e.fail("shielderIdleAnimation", err)
	}
	effectSlot, isAllocated := e.runtime.effectPool.Allocate(e.objectID)
	if !isAllocated {
		delete(peerSession.campaignNPCShielderShieldSetups, e.objectID)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
		e.runtime.registry.mutex.Unlock()
		return e.fail(
			"shielderShieldEffect", errors.New("effect slot unavailable"),
		)
	}
	effectPacket, err := npcraknet.ShieldEffectAsset(
		e.objectID, effectSlot, e.profile.EffectName, false,
	)
	if err != nil {
		e.runtime.effectPool.Release(e.objectID, effectSlot)
		delete(peerSession.campaignNPCShielderShieldSetups, e.objectID)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
		e.runtime.registry.mutex.Unlock()
		return e.fail("shielderShieldEffect", err)
	}
	run := &campaignNomadShielderShieldRun{
		runtime: e.runtime, packet: e.packet, sessionKey: e.sessionKey,
		generation: e.generation, objectID: e.objectID,
		timestamp: e.timestamp +
			uint64((e.profile.SetupWait+e.profile.SetupHitDelay)/time.Millisecond),
		profile: e.profile, effectSlot: effectSlot,
	}
	err = run.schedulePoll()
	if err != nil {
		e.runtime.effectPool.Release(e.objectID, effectSlot)
		delete(peerSession.campaignNPCShielderShieldSetups, e.objectID)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
		e.runtime.registry.mutex.Unlock()
		return e.fail("shielderShieldEffectPoll", err)
	}
	err = peerSession.zone.NPCs().StartShield(e.objectID)
	if err != nil {
		run.stop()
		delete(peerSession.campaignNPCShielderShieldSetups, e.objectID)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
		e.runtime.registry.mutex.Unlock()
		return e.fail("shielderShieldStart", err)
	}
	if peerSession.campaignNPCShielderShields == nil {
		peerSession.campaignNPCShielderShields = make(
			map[uint32]*campaignNomadShielderShieldRun,
		)
	}
	peerSession.campaignNPCShielderShields[e.objectID] = run
	delete(peerSession.campaignNPCShielderShieldSetups, e.objectID)
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	return [][]byte{effectPacket, idlePacket}, nil
}

func (e campaignNomadShielderSetup) next() ([][]byte, error) {
	packets, err := e.runtime.produceEnemyMelee(
		e.packet, e.sessionKey, e.generation, e.objectID,
		e.timestamp+uint64((e.profile.SetupWait+e.profile.SetupReleaseDelay)/time.Millisecond),
	)
	if err != nil {
		return e.fail("shielderSetupNext", err)
	}
	return packets, nil
}

func (e campaignNomadShielderSetup) isCurrent() bool {
	e.runtime.registry.mutex.RLock()
	defer e.runtime.registry.mutex.RUnlock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	return isFound && peerSession.generation == e.generation &&
		peerSession.isCampaignNPCSourceActive(e.generation, e.objectID) &&
		peerSession.campaignNPCShielderShieldSetups[e.objectID]
}

func (e campaignNomadShielderSetup) rollback() {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || peerSession.generation != e.generation {
		return
	}
	delete(peerSession.campaignNPCShielderShieldSetups, e.objectID)
	e.runtime.registry.sessions[e.sessionKey] = peerSession
}

func (r campaignNPCActionRuntime) produceNomadShielderShield(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, bool, error) {
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.isCampaignNPCSourceActive(generation, objectID)
	if !isCurrent {
		r.registry.mutex.RUnlock()
		return nil, false, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	profile, isProfileFound := zonenpc.NomadShielderDirectionalShieldProfile(
		enemy.Plan.NounName,
	)
	isSetup := peerSession.campaignNPCShielderShieldSetups[objectID]
	isShieldStopping := peerSession.campaignNPCShielderShields[objectID] != nil &&
		!enemy.IsShieldActive
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, enemy.TargetObjectID,
	)
	r.registry.mutex.RUnlock()
	if !isEnemyFound || !isProfileFound {
		return nil, false, nil
	}
	if enemy.IsShieldActive {
		return nil, false, nil
	}
	if isSetup {
		return nil, true, nil
	}
	if isShieldStopping {
		return nil, true, nil
	}
	if !isTargetFound {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, true, nil
	}
	request := campaignNPCAttackRequest{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
		kind: campaignNPCAttackMelee,
	}
	isDeferred, err := r.pursuit.deferAction(
		packet, sessionKey, generation, objectID, timestamp, request.resume,
	)
	if err != nil {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, true, fmt.Errorf("shielderShieldStun: %w", err)
	}
	if isDeferred {
		return nil, true, nil
	}
	actionProfile := zonenpc.ActionProfile{
		Family: zonenpc.ActionMelee, AbilityName: "NomadShielderPositionForAttack",
		Range: profile.Range, MovementSpeed: profile.MovementSpeed,
	}
	action, err := campaignNPCActionWithProfile(
		enemy.Plan, target.ObjectID, target.Position, actionProfile,
		target.FootprintRadius,
	)
	if err != nil {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, true, fmt.Errorf("shielderShieldPlan: %w", err)
	}
	if action.IsPursuitNeeded {
		packets, marshalErr := npcraknet.Pursuit(action)
		if marshalErr != nil {
			r.releaseAction(sessionKey, generation, objectID)
			return nil, true, fmt.Errorf("shielderShieldPursuitMarshal: %w", marshalErr)
		}
		scheduleErr := r.pursuit.schedule(
			packet, sessionKey, generation, objectID, timestamp,
			action.TargetPosition, actionProfile, request.resume,
		)
		if scheduleErr != nil {
			r.releaseAction(sessionKey, generation, objectID)
			return nil, true, fmt.Errorf("shielderShieldPursuitSchedule: %w", scheduleErr)
		}
		return packets, true, nil
	}
	r.registry.mutex.Lock()
	peerSession, isFound = r.registry.sessions[sessionKey]
	isCurrent = isFound && peerSession.generation == generation &&
		peerSession.isCampaignNPCSourceActive(generation, objectID)
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, true, nil
	}
	if peerSession.campaignNPCShielderShieldSetups == nil {
		peerSession.campaignNPCShielderShieldSetups = make(map[uint32]bool)
	}
	if peerSession.campaignNPCShielderShieldSetups[objectID] {
		r.registry.mutex.Unlock()
		return nil, true, nil
	}
	err = peerSession.zone.NPCs().FacePosition(objectID, target.Position)
	if err != nil {
		r.registry.mutex.Unlock()
		r.releaseAction(sessionKey, generation, objectID)
		return nil, true, fmt.Errorf("shielderShieldFace: %w", err)
	}
	peerSession.campaignNPCShielderShieldSetups[objectID] = true
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	setup := campaignNomadShielderSetup{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
		profile: profile,
	}
	producer := []raknet.ScheduledPacketProducer{
		{Delay: profile.SetupWait, Produce: setup.start},
		{Delay: profile.SetupWait + profile.SetupHitDelay, Produce: setup.activate},
		{Delay: profile.SetupWait + profile.SetupReleaseDelay, Produce: setup.next},
	}
	cancel, scheduleErr := packet.ScheduleProducers(producer)
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		packets, failErr := setup.fail("shielderShieldSchedule", scheduleErr)
		return packets, true, failErr
	}
	return nil, true, nil
}
