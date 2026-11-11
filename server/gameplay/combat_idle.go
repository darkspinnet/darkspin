package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

const campaignNPCBehaviorTick = 100 * time.Millisecond

// The actor owns selection, cooldowns and the lease. This adapter owns only the
// movement coroutine and its reliable client presentation.
type campaignNPCBehaviorRun struct {
	runtime              campaignNPCActionRuntime
	packet               raknet.Packet
	sessionKey           string
	generation           uint64
	run                  zonenpc.CombatIdleRun
	npcSession           *zonenpc.Session
	timestamp            uint64
	destination          game.Vec3
	route                zoneaction.PursuitRoute
	isMoving             bool
	wasMoving            bool
	isTurnChecked        bool
	pauseUntil           time.Time
	movementBuff         float32
	flee                 *campaignNPCSupportIdle
	strafe               *sim.LuaStrafeRuntime
	strafePlan           sim.LuaStrafePlan
	strafeTargetID       uint32
	isIdleSpeedPublished bool
	idleMovementSpeed    float32
	isWaypointPublished  bool
	publishedWaypoint    game.Vec3
}

func isCommonNPCBehavior(behavior string) bool {
	switch behavior {
	case "nBehavior_Idle", "nBehavior_CombatIdle", "nBehavior_Wander", "nBehavior_Stroll",
		"nBehavior_Strafe", "nBehavior_SmallStrafe", "nBehavior_StrafeOrIdle", "nBehavior_Flee", "nBehavior_FleeFacing":
		return true
	default:
		// Patrol needs a caller-supplied waypoint chain, absent from imported
		// definitions. Other specialized controllers retain their lifecycle;
		// a similar name is not inheritance.
		return false
	}
}

func retireNPCBehavior(npcSession *zonenpc.Session, actor zonenpc.Snapshot) ([][]byte, error) {
	targetRun, isTargetRunning := npcSession.CurrentTargetTooFar(actor.Plan.ObjectID,
		actor.ActionOwner, actor.ActionGeneration, time.Now())
	isTargetRetired, isTargetStopAllowed := false, false
	if isTargetRunning {
		isTargetRetired, isTargetStopAllowed = npcSession.FinishTargetTooFar(targetRun)
	}
	run, isRetired := npcSession.RetireCombatIdle(actor.Plan.ObjectID, actor.ActionOwner, actor.ActionGeneration)
	if !isRetired && (!isTargetRetired || !isTargetStopAllowed) {
		return nil, nil
	}
	var packets [][]byte
	switch run.Choice.Behavior {
	case "nBehavior_Strafe", "nBehavior_SmallStrafe", "nBehavior_Flee", "nBehavior_FleeFacing":
		current, isFound := npcSession.NPC(actor.Plan.ObjectID)
		if isFound {
			packet, err := npcSupportMovementAttribute(actor.Plan.ObjectID,
				(1+zonenpc.EffectiveMovementSpeedBuff(current.Plan))*npcSession.SlowMovementScale(actor.Plan.ObjectID, time.Now())-1)
			if err != nil {
				return nil, fmt.Errorf("behaviorRetireBuff: %w", err)
			}
			packets = append(packets, packet)
		}
	}
	stopPackets, err := npcraknet.StopAtPose(actor.Plan.ObjectID, actor.Plan.Position, actor.Facing)
	if err != nil {
		return nil, fmt.Errorf("behaviorRetireStop: %w", err)
	}
	return append(packets, stopPackets...), nil
}

func prepareNPCIdleWindow(npcSession *zonenpc.Session, actor zonenpc.Snapshot,
	now time.Time, releaseDelay time.Duration, cooldown time.Duration,
) ([][]byte, bool, error) {
	packets, err := retireNPCBehavior(npcSession, actor)
	if err != nil {
		return nil, false, fmt.Errorf("idleWindowRetire: %w", err)
	}
	isWindowSet := npcSession.MarkCombatIdleWindow(actor.Plan.ObjectID, actor.ActionOwner,
		actor.ActionGeneration, now, releaseDelay, max(releaseDelay, cooldown))
	return packets, isWindowSet, nil
}

func (e campaignNPCActionRuntime) startPassiveCombatIdle(packet raknet.Packet, sessionKey string,
	generation uint64, actor zonenpc.Snapshot, choice zonenpc.CombatIdleChoice, timestamp uint64,
) ([][]byte, error) {
	packets, err := e.startNPCBehavior(packet, sessionKey, generation, actor.Plan.ObjectID, 0, choice, timestamp)
	if err != nil {
		return nil, fmt.Errorf("passiveStart: %w", err)
	}
	return packets, nil
}

func (e campaignNPCActionRuntime) startNPCBehavior(packet raknet.Packet, sessionKey string,
	generation uint64, objectID uint32, actionGeneration uint64, choice zonenpc.CombatIdleChoice, timestamp uint64,
) ([][]byte, error) {
	if !isCommonNPCBehavior(choice.Behavior) {
		return nil, nil
	}
	e.registry.mutex.Lock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || member.generation != generation || member.zone == nil || member.isZoneTerminal() {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	npcSession := member.zone.NPCs()
	owner := zonenpc.ActionOwner{UserID: member.binding.UserID, PeerGeneration: generation}
	now := e.now()
	current, isActive := npcSession.CurrentCombatIdle(objectID, now)
	if isActive && current.Serial != 0 {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	if actionGeneration != 0 {
		remaining, isWindowOpen := npcSession.CombatIdleWindow(objectID, owner, actionGeneration, now)
		if !isWindowOpen || remaining <= 0 {
			e.registry.mutex.Unlock()
			return nil, nil
		}
	} else {
		currentChoice, isSelected := npcSession.SelectPassiveIdle(objectID, choice.Slot == zonenpc.CombatIdlePassive, now)
		if !isSelected || currentChoice != choice {
			e.registry.mutex.Unlock()
			return nil, nil
		}
	}
	// Existing support-idle instances own their fatigue modifiers and movement.
	if member.npcSupportIdles[objectID] != nil {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	random := member.zone.NPCRandom()
	if random == nil {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	duration := 30 * time.Second
	if choice.Behavior == "nBehavior_CombatIdle" {
		duration = 100*time.Millisecond + time.Duration(random.Float64()*float64(500*time.Millisecond))
	}
	if choice.Behavior == "nBehavior_Idle" && actionGeneration == 0 {
		duration = time.Second
	}
	jitter := time.Duration(0)
	if choice.AuthoredCooldown > 0 {
		jitter = time.Duration(random.Float64() * float64(time.Second))
	}
	run, isNew := npcSession.BeginCombatIdle(zonenpc.CombatIdleRequest{
		ObjectID: objectID, Owner: owner, ActionGeneration: actionGeneration,
		Choice: choice, Duration: duration,
		Cooldown: time.Duration(choice.AuthoredCooldown) * time.Second, Jitter: jitter, Now: now,
	})
	e.registry.mutex.Unlock()
	if !isNew {
		return nil, nil
	}
	step := &campaignNPCBehaviorRun{runtime: e, packet: packet.Autonomous(), sessionKey: sessionKey,
		generation: generation, run: run, npcSession: npcSession, timestamp: timestamp}
	return step.produce()
}

// pollCombatIdleBehaviors fills only the unspent, interruptible part of an
// admitted attack cooldown. Existing attack timers retain their original cadence.
func (r gameplayPendingRuntime) pollCombatIdleBehaviors(packet raknet.Packet) ([][]byte, error) {
	sessionKey := packet.Address.String()
	r.registry.mutex.RLock()
	member, isFound := r.registry.sessions[sessionKey]
	isEligible := isFound && member.zone != nil && member.stage.IsDungeon() &&
		member.dungeonSetup.IsCommitted() && !member.isRejoinPending && !member.isZoneTerminal()
	r.registry.mutex.RUnlock()
	if !isEligible {
		return nil, nil
	}
	npcSession := member.zone.NPCs()
	owner := zonenpc.ActionOwner{UserID: member.binding.UserID, PeerGeneration: member.generation}
	var packets [][]byte
	for _, presence := range npcSession.LiveActorSnapshots() {
		if presence.TargetObjectID == 0 || !presence.IsPublished || presence.IsFixture {
			continue
		}
		actor, isActorFound := npcSession.NPC(presence.ObjectID)
		if !isActorFound || actor.ActionOwner != owner || actor.Plan.OwnerObjectID != 0 ||
			actor.Plan.NPCProfile.IsPlayerPet || !actor.IsScenarioAIEnabled() || actor.IsScenarioControlled() {
			continue
		}
		choice, isSelected := npcSession.SelectCombatIdle(presence.ObjectID, owner, actor.ActionGeneration, r.now())
		if !isSelected {
			continue
		}
		idlePackets, err := r.damage.npc.startNPCBehavior(packet, sessionKey, member.generation,
			presence.ObjectID, actor.ActionGeneration, choice, packet.SourceTime)
		if err != nil {
			return nil, fmt.Errorf("combatIdleStart: %w", err)
		}
		packets = append(packets, idlePackets...)
	}
	return packets, nil
}

func (e *campaignNPCBehaviorRun) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || member.generation != e.generation || member.zone == nil ||
		member.zone.NPCs() != e.npcSession || member.isZoneTerminal() {
		// The retained feature owner survives peer removal long enough to retire
		// this exact lease. No packets belong to the absent/replaced peer.
		isFinished, isStopAllowed := e.npcSession.FinishCombatIdle(e.run)
		if isFinished && isStopAllowed {
			// State cleanup is complete; presentation is intentionally omitted.
		}
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	now := e.runtime.now()
	npcSession := member.zone.NPCs()
	if !npcSession.IsCombatIdleActive(e.run, now) {
		packets, err := e.finishLocked(&member)
		e.runtime.registry.mutex.Unlock()
		if err != nil {
			return nil, fmt.Errorf("behaviorFinish: %w", err)
		}
		if e.run.ActionGeneration != 0 {
			choice, isSelected := npcSession.SelectCombatIdle(e.run.ObjectID, e.run.Owner, e.run.ActionGeneration, now)
			if isSelected {
				nextPackets, nextErr := e.runtime.startNPCBehavior(e.packet, e.sessionKey, e.generation,
					e.run.ObjectID, e.run.ActionGeneration, choice, e.timestamp)
				if nextErr != nil {
					return packets, fmt.Errorf("behaviorReplace: %w", nextErr)
				}
				packets = append(packets, nextPackets...)
			}
		}
		return packets, nil
	}
	source, isSourceFound := npcSession.NPC(e.run.ObjectID)
	if !isSourceFound {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	var packets [][]byte
	var isFinished bool
	var err error
	switch e.run.Choice.Behavior {
	case "nBehavior_Idle":
		// Authored Idle waits forever until a higher-priority leaf interrupts.
	case "nBehavior_CombatIdle":
		if !e.isTurnChecked && source.TargetObjectID != 0 {
			target, isTargetFound := member.campaignNPCTarget(e.generation, source.TargetObjectID)
			if isTargetFound && source.IsIdleTurnNeeded(target.Position) {
				plan := zonenpc.AttackPlan{SourceObjectID: source.Plan.ObjectID, TargetObjectID: target.ObjectID,
					ActionGeneration: source.ActionGeneration, SourcePosition: source.Plan.Position, TargetPosition: target.Position}
				if npcSession.CommitFacing(plan) {
					packets, err = npcraknet.FaceTarget(plan, false)
				}
			}
			e.isTurnChecked = true
		}
	default:
		packets, isFinished, err = e.moveLocked(&member, source, now)
	}
	if err != nil || isFinished {
		stopPackets, stopErr := e.finishLocked(&member)
		packets = append(packets, stopPackets...)
		e.runtime.registry.mutex.Unlock()
		if err != nil || stopErr != nil {
			return nil, fmt.Errorf("behaviorStep: %w", errors.Join(err, stopErr))
		}
		return packets, nil
	}
	e.runtime.registry.mutex.Unlock()
	delay := min(campaignNPCBehaviorTick, max(time.Millisecond, e.run.Deadline.Sub(now)))
	e.timestamp += uint64(delay / time.Millisecond)
	cancel, scheduleErr := scheduleNPCProducers(e.runtime.registry, e.packet, []raknet.ScheduledPacketProducer{{
		Delay: delay, Produce: e.produce,
	}})
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("behavior scheduler returned no cancellation")
	}
	if scheduleErr != nil {
		e.runtime.registry.mutex.Lock()
		cleanupPackets, cleanupErr := e.finishLocked(&member)
		e.runtime.registry.mutex.Unlock()
		return append(packets, cleanupPackets...), fmt.Errorf("behaviorSchedule: %w", errors.Join(scheduleErr, cleanupErr))
	}
	return packets, nil
}

func (e *campaignNPCBehaviorRun) finishLocked(member *gameplayPeerSession) ([][]byte, error) {
	var strafeErr error
	if e.strafe != nil {
		cleanup, err := e.strafe.Deactivate()
		e.strafe = nil
		if err != nil {
			strafeErr = fmt.Errorf("strafeDeactivate: %w", err)
		} else if !cleanup.IsStopRequested || cleanup.RemovedMovementSpeedBuff != e.strafePlan.MovementSpeedBuff {
			strafeErr = errors.New("strafe cleanup contract invalid")
		}
	}
	npcSession := member.zone.NPCs()
	isFinished, isStopAllowed := npcSession.FinishCombatIdle(e.run)
	if !isFinished {
		return nil, strafeErr
	}
	source, isFound := npcSession.NPC(e.run.ObjectID)
	if !isFound || source.IsDefeated {
		return nil, strafeErr
	}
	var packets [][]byte
	if e.movementBuff != 0 {
		source, isFound = npcSession.NPC(e.run.ObjectID)
		if isFound {
			attributePacket, err := npcSupportMovementAttribute(e.run.ObjectID,
				(1+zonenpc.EffectiveMovementSpeedBuff(source.Plan))*npcSession.SlowMovementScale(e.run.ObjectID, e.runtime.now())-1)
			if err != nil {
				return nil, fmt.Errorf("behaviorBuffReset: %w", err)
			}
			packets = append(packets, attributePacket)
		}
	}
	if isStopAllowed && (e.wasMoving || e.isMoving) {
		stopPackets, err := npcraknet.StopAtPose(e.run.ObjectID, source.Plan.Position, source.Facing)
		if err != nil {
			return nil, fmt.Errorf("behaviorStop: %w", err)
		}
		packets = append(packets, stopPackets...)
	}
	return packets, strafeErr
}
