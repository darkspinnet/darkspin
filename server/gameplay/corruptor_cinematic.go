package gameplay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	"github.com/darkspinnet/darkspin/server/zone"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

// The registry lock owns the run. The clock belongs to the zone, not the
// transport which happened to deliver the final hit of the first stage.
type corruptorCinematicRun struct {
	runtime              campaignNPCActionRuntime
	zone                 *zone.Zone
	objectID             uint32
	sourceGeneration     uint64
	entryAt              time.Time
	until                time.Time
	timestamp            uint64
	timestampAt          time.Time
	state                campaignCorruptorState
	entryBoss            zonenpc.Snapshot
	prefixPackets        [][]byte
	focus                raknet.Vector3
	targets              []uint32
	companions           []zonecompanion.Actor
	lease                zonenpc.CinematicLease
	modifiers            []zoneeffect.Modifier
	stopContext          func() bool
	isRetired            bool
	isComplete           bool
	isResuming           bool
	isAdmitted           bool
	isArmed              bool
	isPrefixPrepared     bool
	isDeathPresented     bool
	continuation         corruptorPhaseContinuation
	continuationIdentity gameplayProducerIdentity
}

func (e *corruptorCinematicRun) key() string {
	return fmt.Sprintf("corruptor-cinematic:%d:%d", e.objectID, e.state.phaseGeneration)
}

// The caller owns the registry lock and is still committing the accepted
// resurrection. No transport lookup or later retained-session insertion owns
// this reservation or its original clock.
func (e *gameplayPeerSession) acceptCorruptorSecondAggro(boss zonenpc.Snapshot, deathNounName string) (*corruptorCinematicRun, error) {
	if e == nil || e.zone == nil || e.zone.NPCs() == nil {
		return nil, errors.New("corruptor cinematic world unavailable")
	}
	state, isStateFound := e.campaignCorruptorStates[boss.Plan.ObjectID]
	if !isStateFound || !state.isStageTwo || !state.isPhasePending {
		return nil, errors.New("corruptor cinematic acceptance unavailable")
	}
	if state.secondAggro != nil {
		return state.secondAggro, nil
	}
	entryAt := time.Now().Add(zonenpc.CorruptorStageOneDeathDuration)
	until := entryAt.Add(zonenpc.SecondAggroCinematicDuration)
	focus := zonenpc.SecondAggroCinematicFocus(boss.Plan.Position)
	sourceGeneration, isReserved := e.zone.NPCs().ReserveSecondAggroCinematic(boss.Plan.ObjectID, focus, until, deathNounName)
	if !isReserved {
		return nil, errors.New("corruptor cinematic reservation unavailable")
	}
	run := &corruptorCinematicRun{zone: e.zone, objectID: boss.Plan.ObjectID,
		sourceGeneration: sourceGeneration, entryAt: entryAt, until: until,
		entryBoss: boss, focus: raknet.Vector3(focus)}
	state.secondAggro = run
	run.state = state
	e.campaignCorruptorStates[boss.Plan.ObjectID] = state
	return run, nil
}

// Bind the captured world acceptance before any optional damage presentation
// can fail. The original driver's active/retained lookup is intentionally absent.
func (e campaignNPCActionRuntime) startCorruptorSecondAggro(run *corruptorCinematicRun, timestamp uint64) error {
	if run == nil {
		return nil
	}
	err := e.armCorruptorSecondAggro(run, timestamp, run.entryAt.Add(-zonenpc.CorruptorStageOneDeathDuration))
	if err != nil {
		return fmt.Errorf("sceneArm: %w", err)
	}
	return nil
}

func (e campaignNPCActionRuntime) armCorruptorSecondAggro(run *corruptorCinematicRun, timestamp uint64, timestampAt time.Time) error {
	if e.timer == nil || e.now == nil || e.modifierPool == nil {
		return errors.New("corruptor cinematic authority unavailable")
	}
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	if run.isArmed || run.isComplete {
		return nil
	}
	run.runtime = e
	run.timestamp = timestamp
	run.timestampAt = timestampAt
	run.isArmed = true
	sceneContext := run.zone.Context()
	if sceneContext == nil {
		run.isArmed = false
		return errors.New("corruptor cinematic context unavailable")
	}
	run.stopContext = context.AfterFunc(sceneContext, run.cancel)
	return nil
}

// Publication drains accepted damage projection ahead of the replacement
// scene in the same recipient FIFO. Early arming owns cancellation only.
func (e campaignNPCActionRuntime) publishCorruptorSecondAggro(run *corruptorCinematicRun) {
	if run == nil {
		return
	}
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	if !run.isArmed || run.isAdmitted || run.isComplete {
		return
	}
	packets, err := run.admitLocked()
	if err != nil {
		run.retryAdmissionLocked(err)
		// Deferred scene admission must not reject the accepted damage result.
		return
	}
	run.queueLocked(packets)
}

// Drain healing/effect preparation and final aura retirement can fail after
// acceptance but before publishAreaResults. A current-member poll recovers
// that retained acceptance without requiring the original producer to survive.
func (e campaignNPCActionRuntime) recoverCorruptorSecondAggro(sessionKey string, generation uint64, timestamp uint64) error {
	e.registry.mutex.RLock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || member.generation != generation || member.zone == nil {
		e.registry.mutex.RUnlock()
		return nil
	}
	runs := make(map[*corruptorCinematicRun]struct{})
	for _, candidate := range e.registry.sessions {
		if candidate.zone != member.zone {
			continue
		}
		for _, state := range candidate.campaignCorruptorStates {
			if state.secondAggro != nil && !state.secondAggro.isArmed && !state.secondAggro.isComplete {
				runs[state.secondAggro] = struct{}{}
			}
		}
	}
	for _, retained := range e.registry.retainedSessions {
		if retained.peerSession.zone != member.zone {
			continue
		}
		for _, state := range retained.peerSession.campaignCorruptorStates {
			if state.secondAggro != nil && !state.secondAggro.isArmed && !state.secondAggro.isComplete {
				runs[state.secondAggro] = struct{}{}
			}
		}
	}
	e.registry.mutex.RUnlock()
	for run := range runs {
		err := e.armCorruptorSecondAggro(run, timestamp, e.now())
		if err != nil {
			return fmt.Errorf("sceneRecovery: %w", err)
		}
		e.publishCorruptorSecondAggro(run)
	}
	return nil
}

func (e *corruptorCinematicRun) sourceTimestamp(at time.Time) uint64 {
	return e.timestamp + uint64(max(time.Duration(0), at.Sub(e.timestampAt))/time.Millisecond)
}

func (e *corruptorCinematicRun) prepareEntryPackets() ([][]byte, error) {
	deletePacket, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{ObjectID: []uint32{e.objectID}})
	if err != nil {
		return nil, fmt.Errorf("entryDelete: %w", err)
	}
	spawnPackets, err := npcraknet.TargetedSpawn(e.entryBoss.Plan, e.entryBoss.TargetObjectID)
	if err != nil {
		return nil, fmt.Errorf("entrySpawn: %w", err)
	}
	animationPacket, err := npcraknet.AnimationState(e.objectID, campaignCorruptorStageTwoAnimation, e.sourceTimestamp(e.entryAt))
	if err != nil {
		return nil, fmt.Errorf("entryAnimation: %w", err)
	}
	profile := zonenpc.ActionProfile{ImpactEffectName: "scaldronboss_transition_shader_effect.ServerEventDef"}
	resurrectionPackets, err := npcraknet.ResurrectionHit(e.objectID, e.entryBoss, profile)
	if err != nil {
		return nil, fmt.Errorf("entryShader: %w", err)
	}
	packets := append([][]byte{deletePacket}, spawnPackets...)
	packets = append(packets, animationPacket)
	return append(packets, resurrectionPackets...), nil
}

func (e *corruptorCinematicRun) admitLocked() ([][]byte, error) {
	if e.isAdmitted || e.isComplete {
		return nil, nil
	}
	source, isSourceFound := e.zone.NPCs().NPC(e.objectID)
	if !isSourceFound || source.IsDefeated || !source.IsSecondAggroCinematic(e.sourceGeneration) {
		e.isComplete = true
		e.retireLocked()
		if e.stopContext != nil {
			e.stopContext()
		}
		return nil, nil
	}
	if e.runtime.now().Before(e.entryAt) {
		return e.presentStageOneDeathLocked(source)
	}
	if !e.isPrefixPrepared {
		packets, err := e.prepareEntryPackets()
		if err != nil {
			return nil, fmt.Errorf("entryPackets: %w", err)
		}
		e.prefixPackets = packets
		e.isPrefixPrepared = true
	}
	e.isRetired = false
	e.modifiers = nil
	e.targets = nil
	e.companions = nil
	if !e.until.After(e.runtime.now()) {
		err := e.zone.Timeline().Schedule(e.key(), 0, e.finish, e.runtime.timer.Schedule)
		if err != nil {
			return nil, fmt.Errorf("expiredSchedule: %w", err)
		}
		e.isAdmitted = true
		packets := make([][]byte, 0, len(e.prefixPackets))
		for _, packet := range e.prefixPackets {
			if len(packet) > 0 && raknet.PacketID(packet[0]) != raknet.SetAnimationState {
				packets = append(packets, packet)
			}
		}
		e.queueLocked(packets)
		return nil, nil
	}
	targets, err := e.zone.NPCs().SecondAggroCinematicTargets(e.objectID, zonePosition(e.focus))
	if err != nil {
		return nil, fmt.Errorf("cinematicTargets: %w", err)
	}
	packets := make([][]byte, 0)
	for _, target := range targets {
		at := e.runtime.now()
		remaining := max(time.Nanosecond, e.until.Sub(at))
		presentationTimestamp := e.sourceTimestamp(at)
		instanceID, allocationErr := e.runtime.modifierPool.Allocate()
		if allocationErr != nil {
			e.retireLocked()
			return nil, fmt.Errorf("cinematicAllocate: %w", allocationErr)
		}
		modifier := zoneeffect.Modifier{InstanceID: instanceID, GUID: util.HashID("MinionCinematic"),
			SourceObjectID: e.objectID, TargetObjectID: target.Plan.ObjectID,
			Rank: 1, Duration: remaining, ExpiresAt: e.until,
			Kind: zoneeffect.ModifierKindBuff, InitiatorObject: e.objectID}
		e.modifiers = append(e.modifiers, modifier)
		e.targets = append(e.targets, target.Plan.ObjectID)
		createPacket, createErr := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
			SourceObjectID: e.objectID, TargetObjectID: target.Plan.ObjectID,
			ModifierID: modifier.GUID, InstanceID: instanceID,
			Duration: remaining, Timestamp: presentationTimestamp})
		if createErr != nil {
			e.retireLocked()
			return nil, fmt.Errorf("cinematicCreate: %w", createErr)
		}
		pausePackets, pauseErr := npcraknet.MovementPause(target.Plan.ObjectID, target.Plan.Position, target.Facing)
		if pauseErr != nil {
			e.retireLocked()
			return nil, fmt.Errorf("cinematicPause: %w", pauseErr)
		}
		packets = append(packets, createPacket)
		packets = append(packets, pausePackets...)
	}
	err = e.zone.Timeline().Schedule(e.key(), max(time.Duration(0), e.until.Sub(e.runtime.now())), e.finish, e.runtime.timer.Schedule)
	if err != nil {
		e.retireLocked()
		return nil, fmt.Errorf("cinematicSchedule: %w", err)
	}
	if e.zone.Companion() != nil {
		at := e.runtime.now()
		companionTargets := e.zone.Companion().SecondAggroCinematicTargets(zonePosition(e.focus), at)
		for _, target := range companionTargets {
			heldActor, isHeld := e.zone.Companion().BeginCinematic(target, at, e.until)
			if !isHeld {
				continue
			}
			e.companions = append(e.companions, heldActor)
			createPacket, createErr := e.prepareCompanionModifier(heldActor)
			if createErr != nil {
				e.zone.Timeline().Cancel(e.key())
				e.retireLocked()
				return nil, fmt.Errorf("cinematicCompanion: %w", createErr)
			}
			stopPacket, stopErr := raknet.MarshalApplication(raknet.ObjectPlayerMoveMessage{
				ObjectID: heldActor.ObjectID, GoalFlags: 0x20, GoalPosition: raknet.Vector3(heldActor.Position)})
			if stopErr != nil {
				e.zone.Timeline().Cancel(e.key())
				e.retireLocked()
				return nil, fmt.Errorf("cinematicCompanionStop: %w", stopErr)
			}
			packets = append(packets, createPacket, stopPacket)
		}
	}
	for _, modifier := range e.modifiers {
		err = e.zone.Effect().Put(modifier)
		if err != nil {
			e.zone.Timeline().Cancel(e.key())
			e.retireLocked()
			return nil, fmt.Errorf("cinematicInventory: %w", err)
		}
	}
	e.lease, err = e.zone.NPCs().BeginSecondAggroCinematic(e.objectID, e.sourceGeneration, e.targets, zonePosition(e.focus), e.until)
	if err != nil {
		e.zone.Timeline().Cancel(e.key())
		e.retireLocked()
		return nil, fmt.Errorf("cinematicHold: %w", err)
	}
	camera, err := raknet.MarshalApplication(raknet.NewCinematicMessage(
		max(time.Duration(0), e.until.Sub(e.runtime.now())), e.focus, zonenpc.SecondAggroCinematicRadius))
	if err != nil {
		e.zone.Timeline().Cancel(e.key())
		e.retireLocked()
		return nil, fmt.Errorf("cinematicCamera: %w", err)
	}
	e.isAdmitted = true
	// C9 is deliberately excluded from generic NPC fanout. Queue the entire
	// scene, including delete/create, through one FIFO for every recipient.
	scenePackets := append(clonePendingPackets(e.prefixPackets), camera)
	e.queueLocked(append(scenePackets, packets...))
	return nil, nil
}

// Preserve the original body until its authored death animation completes.
// The zone-owned reservation already blocks actions and incoming damage; only
// the later entry replaces the client object and begins the second-aggro scene.
func (e *corruptorCinematicRun) presentStageOneDeathLocked(source zonenpc.Snapshot) ([][]byte, error) {
	err := e.zone.Timeline().Schedule(e.key()+":entry", max(time.Duration(0), e.entryAt.Sub(e.runtime.now())), e.retryAdmission, e.runtime.timer.Schedule)
	if err != nil {
		return nil, fmt.Errorf("deathSchedule: %w", err)
	}
	if e.isDeathPresented {
		return nil, nil
	}
	packets, err := npcraknet.MovementPause(e.objectID, source.Plan.Position, source.Facing)
	if err != nil {
		return nil, fmt.Errorf("deathStop: %w", err)
	}
	animationPacket, err := npcraknet.AnimationState(e.objectID, "sca_boss_death", e.timestamp)
	if err != nil {
		return nil, fmt.Errorf("deathAnimation: %w", err)
	}
	packets = append(packets, animationPacket)
	e.isDeathPresented = true
	return packets, nil
}

func (e *corruptorCinematicRun) prepareCompanionModifier(target zonecompanion.Actor) ([]byte, error) {
	at := e.runtime.now()
	remaining := max(time.Nanosecond, e.until.Sub(at))
	presentationTimestamp := e.sourceTimestamp(at)
	instanceID, err := e.runtime.modifierPool.Allocate()
	if err != nil {
		return nil, fmt.Errorf("companionAllocate: %w", err)
	}
	modifier := zoneeffect.Modifier{InstanceID: instanceID, GUID: util.HashID("MinionCinematic"),
		SourceObjectID: e.objectID, TargetObjectID: target.ObjectID,
		Rank: 1, Duration: remaining, ExpiresAt: e.until,
		Kind: zoneeffect.ModifierKindBuff, InitiatorObject: e.objectID}
	e.modifiers = append(e.modifiers, modifier)
	packet, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
		SourceObjectID: e.objectID, TargetObjectID: target.ObjectID, ModifierID: modifier.GUID,
		InstanceID: instanceID, Duration: remaining, Timestamp: presentationTimestamp})
	if err != nil {
		return nil, fmt.Errorf("companionCreate: %w", err)
	}
	return packet, nil
}

func (e *corruptorCinematicRun) retryAdmissionLocked(admissionErr error) {
	if e.runtime.logger != nil {
		e.runtime.logger.Printf("Corruptor cinematic admission pending: %v", admissionErr)
	}
	err := e.zone.Timeline().Schedule(e.key()+":admission", time.Second, e.retryAdmission, e.runtime.timer.Schedule)
	if err != nil && e.runtime.logger != nil {
		e.runtime.logger.Printf("Corruptor cinematic admission retry unavailable: %v", err)
	}
}

func (e *corruptorCinematicRun) retryAdmission() {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	if e.isComplete || e.isAdmitted || !e.zone.IsActive() {
		return
	}
	packets, err := e.admitLocked()
	if err != nil {
		e.retryAdmissionLocked(err)
		return
	}
	e.queueLocked(packets)
}

// Both the ability-owned Intangible and the modifier-owned Immobilized have
// distinct owners. Never clear a combat root or a different intangible hold.
func (e *corruptorCinematicRun) retireLocked() [][]byte {
	if e.isRetired {
		return nil
	}
	e.isRetired = true
	e.zone.NPCs().EndSecondAggroCinematic(e.lease)
	for _, companion := range e.companions {
		isEnded := e.zone.Companion().EndCinematic(companion)
		if !isEnded {
			// Replaced, dead or superseded recipients keep their newer owner.
			continue
		}
	}
	packets := make([][]byte, 0, len(e.modifiers))
	for _, modifier := range e.modifiers {
		removed, isRemoved := e.zone.Effect().Remove(modifier.InstanceID)
		if isRemoved && removed.TargetObjectID != modifier.TargetObjectID && e.runtime.logger != nil {
			e.runtime.logger.Printf("Corruptor cinematic modifier target changed instance=%d", modifier.InstanceID)
		}
		packet, err := effectraknet.ModifierDelete(modifier.TargetObjectID, modifier.InstanceID)
		if err != nil {
			if e.runtime.logger != nil {
				e.runtime.logger.Printf("Corruptor cinematic modifier delete failed: %v", err)
			}
		} else {
			packets = append(packets, packet)
		}
		err = e.runtime.modifierPool.Release(modifier.InstanceID)
		if err != nil && e.runtime.logger != nil {
			e.runtime.logger.Printf("Corruptor cinematic modifier release failed: %v", err)
		}
	}
	return packets
}

func (e *corruptorCinematicRun) queueLocked(packets [][]byte) {
	if len(packets) == 0 || !e.zone.IsActive() {
		return
	}
	for sessionKey, member := range e.runtime.registry.sessions {
		if member.zone != e.zone || member.isZoneTerminal() {
			continue
		}
		err := member.queueCampaignPresentation(packets)
		if err != nil && e.runtime.logger != nil {
			e.runtime.logger.Printf("Corruptor cinematic presentation queue failed: %v", err)
		}
		e.runtime.registry.sessions[sessionKey] = member
	}
}

func (e *corruptorCinematicRun) cancel() {
	e.zone.Timeline().Cancel(e.key())
	e.zone.Timeline().Cancel(e.key() + ":entry")
	e.zone.Timeline().Cancel(e.key() + ":admission")
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	e.isComplete = true
	if !e.isResuming {
		e.continuation.cancelPortals()
	}
	packets := e.retireLocked()
	e.queueLocked(packets)
}

func (e *corruptorCinematicRun) finish() {
	e.runtime.registry.mutex.Lock()
	if e.isComplete || e.isResuming {
		e.runtime.registry.mutex.Unlock()
		return
	}
	source, isSourceFound := e.zone.NPCs().NPC(e.objectID)
	if !isSourceFound || source.IsDefeated || !source.IsSecondAggroCinematic(e.sourceGeneration) {
		e.isComplete = true
		packets := e.retireLocked()
		e.queueLocked(packets)
		if e.stopContext != nil {
			e.stopContext()
		}
		e.runtime.registry.mutex.Unlock()
		return
	}
	remaining := e.until.Sub(e.runtime.now())
	if remaining > 0 {
		e.runtime.registry.mutex.Unlock()
		err := e.zone.Timeline().Schedule(e.key(), remaining, e.finish, e.runtime.timer.Schedule)
		if err != nil && e.runtime.logger != nil {
			e.runtime.logger.Printf("Corruptor cinematic deadline retry failed: %v", err)
		}
		return
	}
	packets := e.retireLocked()
	e.queueLocked(packets)
	if !e.zone.IsActive() {
		e.isComplete = true
		e.runtime.registry.mutex.Unlock()
		return
	}
	boss, isBossFound := e.zone.NPCs().NPC(e.objectID)
	if !isBossFound || boss.IsDefeated || !boss.IsCorruptorStageTwo {
		e.isComplete = true
		e.runtime.registry.mutex.Unlock()
		return
	}
	sessionKey := ""
	member := gameplayPeerSession{}
	for candidateKey, candidate := range e.runtime.registry.sessions {
		if candidate.zone != e.zone || candidate.isRejoinPending || candidate.isZoneTerminal() ||
			!candidate.isCampaignPresentationAvailable() || candidate.schedulePacket.Address == nil ||
			candidate.transportGeneration == 0 || candidate.schedulePacket.TransportGeneration != candidate.transportGeneration {
			continue
		}
		if sessionKey == "" || candidateKey < sessionKey {
			sessionKey, member = candidateKey, candidate
		}
	}
	if sessionKey == "" {
		e.runtime.registry.mutex.Unlock()
		err := e.zone.Timeline().Schedule(e.key(), 250*time.Millisecond, e.finish, e.runtime.timer.Schedule)
		if err != nil && e.runtime.logger != nil {
			e.runtime.logger.Printf("Corruptor cinematic resume deferred: %v", err)
		}
		return
	}
	if member.campaignCorruptorStates == nil {
		member.campaignCorruptorStates = make(map[uint32]campaignCorruptorState)
	}
	state := e.state
	if e.continuation.isPrepared {
		state = e.latestContinuationStateLocked()
		e.continuation.state = state
	}
	owner := zonenpc.ActionOwner{UserID: member.binding.UserID, PeerGeneration: member.generation}
	isClaimed := e.zone.NPCs().ClaimSecondAggroController(e.objectID, e.sourceGeneration, owner, e.runtime.now())
	if !isClaimed {
		e.runtime.registry.mutex.Unlock()
		return
	}
	for candidateKey, candidate := range e.runtime.registry.sessions {
		if candidateKey != sessionKey && candidate.zone == e.zone {
			delete(candidate.campaignCorruptorStates, e.objectID)
		}
	}
	for _, retained := range e.runtime.registry.retainedSessions {
		if retained.peerSession.zone == e.zone && retained.peerSession.binding.UserID != member.binding.UserID {
			delete(retained.peerSession.campaignCorruptorStates, e.objectID)
		}
	}
	member.campaignCorruptorStates[e.objectID] = state
	e.runtime.registry.sessions[sessionKey] = member
	e.isResuming = true
	identity := gameplayProducerIdentityFromSession(sessionKey, member, true)
	if e.continuationIdentity != identity {
		// Old transport jobs cannot carry this actor after driver replacement.
		// Keep semantic preparation and re-admit its unfinished world driver.
		e.continuation.cancelPortals()
		e.continuation.isActionRestarted = false
		e.continuation.areActorsScheduled = false
		e.continuation.isPeriodScheduled = false
		e.continuation.isComplete = false
		e.continuationIdentity = identity
	}
	step := campaignCorruptorPhaseStep{runtime: e.runtime, packet: member.schedulePacket.Autonomous(),
		world:    e.zone,
		identity: identity, sceneGeneration: e.sourceGeneration,
		continuation: &e.continuation,
		sessionKey:   sessionKey, generation: member.generation, objectID: e.objectID,
		phaseGeneration: state.phaseGeneration, isStageTwoStart: true,
		timestamp: e.sourceTimestamp(e.runtime.now())}
	e.runtime.registry.mutex.Unlock()
	packets, err := e.runtime.advanceCorruptorPhase(step)
	if err != nil && e.runtime.logger != nil {
		e.runtime.logger.Printf("Corruptor cinematic phase continuation failed: %v", err)
	}
	e.runtime.registry.mutex.Lock()
	currentSource, isCurrentSourceFound := e.zone.NPCs().NPC(e.objectID)
	if e.isComplete || !e.zone.IsActive() || !isCurrentSourceFound || currentSource.IsDefeated || !currentSource.IsSecondAggroCinematic(e.sourceGeneration) {
		e.isComplete = true
		e.continuation.cancelPortals()
		e.isResuming = false
		e.runtime.registry.mutex.Unlock()
		return
	}
	e.queueLocked(packets)
	current, isCurrent := e.runtime.registry.sessions[sessionKey]
	currentState := current.campaignCorruptorStates[e.objectID]
	isAccepted := isCurrent && current.zone == e.zone && current.generation == step.generation &&
		gameplayProducerIdentityFromSession(sessionKey, current, isCurrent) == identity &&
		current.transportGeneration == step.packet.TransportGeneration &&
		currentState.phaseGeneration == state.phaseGeneration && currentState.isPhaseActivated && e.continuation.isComplete && err == nil
	e.isResuming = false
	if isAccepted {
		e.isComplete = true
		currentState.secondAggro = nil
		current.campaignCorruptorStates[e.objectID] = currentState
		e.runtime.registry.sessions[sessionKey] = current
		if e.stopContext != nil {
			e.stopContext()
		}
	}
	e.runtime.registry.mutex.Unlock()
	if !isAccepted {
		retryErr := e.zone.Timeline().Schedule(e.key(), time.Second, e.finish, e.runtime.timer.Schedule)
		if retryErr != nil && e.runtime.logger != nil {
			e.runtime.logger.Printf("Corruptor cinematic resume retry failed: %v", retryErr)
		}
	}
}

func (e *corruptorCinematicRun) latestContinuationStateLocked() campaignCorruptorState {
	identity := e.continuationIdentity
	member, isFound := e.runtime.registry.sessions[identity.sessionKey]
	if isFound && member.zone == e.zone && member.generation == identity.zoneGeneration && gameplaySessionMemberKey(member) == identity.memberKey {
		state, isStateFound := member.campaignCorruptorStates[e.objectID]
		if isStateFound && state.phaseGeneration == e.state.phaseGeneration {
			return state
		}
	}
	retained := e.runtime.registry.retainedSessions[identity.memberKey]
	if retained != nil && retained.peerSession.zone == e.zone && retained.peerSession.generation == identity.zoneGeneration {
		state, isStateFound := retained.peerSession.campaignCorruptorStates[e.objectID]
		if isStateFound && state.phaseGeneration == e.state.phaseGeneration {
			return state
		}
	}
	return e.continuation.state
}
