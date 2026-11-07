package gameplay

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

const campaignFollowStrafeTick = 50 * time.Millisecond

type campaignFollowStrafeRun struct {
	runtime     campaignNPCPursuitRuntime
	packet      raknet.Packet
	sessionKey  string
	generation  uint64
	npcSession  *zonenpc.Session
	run         zonenpc.TargetTooFarRun
	profile     zonenpc.ActionProfile
	onArrival   campaignNPCArrival
	timestamp   uint64
	movementAt  time.Time
	goalUntil   time.Time
	destination game.Vec3
	isMoving    bool
	isLeft      bool
}

func (r campaignNPCPursuitRuntime) startTargetTooFar(packet raknet.Packet, sessionKey string,
	generation uint64, objectID, targetObjectID uint32, timestamp uint64,
	profile zonenpc.ActionProfile, onArrival campaignNPCArrival,
) (bool, error) {
	if targetObjectID != 0 || isCampaignNPCSecondaryPursuit(profile.AbilityName) ||
		profile.Family == zonenpc.ActionCharge || profile.AbilityName == "Repair" {
		return false, nil
	}
	if r.registry == nil || r.now == nil || onArrival == nil {
		return false, errors.New("target too far runtime unavailable")
	}
	r.registry.mutex.Lock()
	member, isFound := r.registry.sessions[sessionKey]
	if !isFound || member.generation != generation || member.zone == nil || member.isZoneTerminal() {
		r.registry.mutex.Unlock()
		return false, nil
	}
	npcSession := member.zone.NPCs()
	source, isSourceFound := npcSession.NPC(objectID)
	owner := zonenpc.ActionOwner{UserID: member.binding.UserID, PeerGeneration: generation}
	if !isSourceFound || !source.IsActionStarted || source.ActionOwner != owner || source.TargetObjectID == 0 ||
		source.Plan.IsFixture || source.Plan.OwnerObjectID != 0 || source.Plan.NPCProfile.IsPlayerPet ||
		source.IsScenarioControlled() || !source.IsScenarioAIEnabled() {
		r.registry.mutex.Unlock()
		return false, nil
	}
	now := r.now()
	current, isCurrent := npcSession.CurrentTargetTooFar(objectID, owner, source.ActionGeneration, now)
	if isCurrent && current.Serial != 0 {
		r.registry.mutex.Unlock()
		return true, nil
	}
	choice, isSelected := npcSession.SelectTargetTooFar(objectID, owner, source.ActionGeneration, now)
	if !isSelected || choice.Behavior != "nBehavior_FollowStrafe" || member.zone.NPCRandom() == nil {
		r.registry.mutex.Unlock()
		return false, nil
	}
	var err error
	member, err = r.advanceTargetPoseAtLocked(member, source.TargetObjectID, now)
	if err != nil {
		r.registry.mutex.Unlock()
		return false, fmt.Errorf("followStartTarget: %w", err)
	}
	r.registry.sessions[sessionKey] = member
	target, isTargetFound := member.campaignNPCTarget(generation, source.TargetObjectID)
	if !isTargetFound || target.HitPoint <= 0 {
		r.registry.mutex.Unlock()
		return false, nil
	}
	if zonegeometry.Distance(source.Plan.Position, target.Position) < max(float32(0.5), profile.Range) {
		r.registry.mutex.Unlock()
		return false, nil
	}
	idlePackets, err := retireNPCBehavior(npcSession, source)
	if err != nil {
		r.registry.mutex.Unlock()
		return false, fmt.Errorf("followIdleRetire: %w", err)
	}
	r.registry.queueChannelPresentationLocked(member.zone, idlePackets)
	jitter := time.Duration(0)
	if choice.AuthoredCooldown > 0 {
		jitter = time.Duration(member.zone.NPCRandom().Float64() * float64(time.Second))
	}
	run, isNew := npcSession.BeginTargetTooFar(zonenpc.TargetTooFarRequest{
		ObjectID: objectID, TargetObjectID: target.ObjectID, Owner: owner,
		ActionGeneration: source.ActionGeneration, Choice: choice,
		Cooldown: time.Duration(choice.AuthoredCooldown) * time.Second, Jitter: jitter, Now: now,
	})
	if !isNew {
		r.registry.mutex.Unlock()
		return false, nil
	}
	step := &campaignFollowStrafeRun{runtime: r, packet: packet.Autonomous(), sessionKey: sessionKey,
		generation: generation, npcSession: npcSession, run: run, onArrival: onArrival,
		profile: profile, timestamp: timestamp, movementAt: now}
	initialSide, err := sim.ScriptRandom(member.zone.NPCRandom())
	if err != nil {
		cleanupPackets, isResumable, cleanupErr := step.finishLocked()
		if isResumable && cleanupErr == nil {
			r.registry.queueChannelPresentationLocked(member.zone, cleanupPackets)
		}
		r.registry.mutex.Unlock()
		return true, fmt.Errorf("followSideDraw: %w", errors.Join(err, cleanupErr))
	}
	step.isLeft = initialSide < 0.5
	r.registry.mutex.Unlock()
	err = step.schedule()
	if err != nil {
		r.registry.mutex.Lock()
		cleanupPackets, isResumable, cleanupErr := step.finishLocked()
		if isResumable && cleanupErr == nil {
			r.registry.queueChannelPresentationLocked(member.zone, cleanupPackets)
		}
		r.registry.mutex.Unlock()
		return true, fmt.Errorf("followStartSchedule: %w", errors.Join(err, cleanupErr))
	}
	return true, nil
}

func (e *campaignFollowStrafeRun) schedule() error {
	cancel, err := scheduleNPCProducers(e.runtime.registry, e.packet, []raknet.ScheduledPacketProducer{{
		Delay: campaignFollowStrafeTick, Produce: e.produce,
	}})
	if err != nil {
		return fmt.Errorf("followSchedule: %w", err)
	}
	if cancel == nil {
		return errors.New("follow schedule cancellation unavailable")
	}
	return nil
}

func (e *campaignFollowStrafeRun) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || member.generation != e.generation || member.zone == nil ||
		member.zone.NPCs() != e.npcSession || member.isZoneTerminal() {
		isFinished, isStopAllowed := e.npcSession.FinishTargetTooFar(e.run)
		if isFinished && isStopAllowed {
			// The retained feature owner is cleaned without publishing to a
			// missing/replaced gameplay peer or resuming its old action.
		}
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	now := e.runtime.now()
	elapsed := max(time.Duration(0), now.Sub(e.movementAt))
	e.movementAt = now
	e.timestamp += uint64(elapsed / time.Millisecond)
	if !e.npcSession.IsTargetTooFarActive(e.run, now) {
		return e.finishAndResumeLocked()
	}
	source, isSourceFound := e.npcSession.NPC(e.run.ObjectID)
	if !isSourceFound || source.TargetObjectID != e.run.TargetObjectID {
		return e.finishAndResumeLocked()
	}
	updatedMember, err := e.runtime.advanceTargetPoseAtLocked(member, e.run.TargetObjectID, now)
	if err != nil {
		return e.failLocked(member, "followTargetPose", err)
	}
	member = updatedMember
	e.runtime.registry.sessions[e.sessionKey] = member
	target, isTargetFound := member.campaignNPCTarget(e.generation, e.run.TargetObjectID)
	// The higher-priority attack leaf preempts the native FollowStrafe loop.
	// Its own float0=.5 is not permission to delay an already-ranged attack
	// until the NPC overlaps its victim. The caller supplied padded range.
	if !isTargetFound || target.HitPoint <= 0 ||
		zonegeometry.Distance(source.Plan.Position, target.Position) < max(float32(0.5), e.profile.Range) {
		return e.finishAndResumeLocked()
	}
	if e.isMoving && !now.Before(e.goalUntil) {
		// Build 103 A08D40 stores Lua argument 4 as a seconds countdown;
		// A08610 subtracts simulator delta seconds until it expires. Chunk
		// 803 supplies 4, so this sampled goal cannot remain held forever.
		e.isMoving = false
	}
	if !e.isMoving {
		e.isLeft = !e.isLeft
		var isDestinationFound bool
		e.destination, isDestinationFound, err = e.selectDestinationLocked(member, source, target.Position)
		if err != nil {
			return e.failLocked(member, "followDestination", err)
		}
		if !isDestinationFound {
			return e.finishToPursuitLocked()
		}
		e.isMoving = true
		e.goalUntil = now.Add(4 * time.Second)
	}
	speed := source.Plan.NPCProfile.BaseCombatSpeed * max(float32(0), 1+zonenpc.EffectiveMovementSpeedBuff(source.Plan))
	speed = graspingDeadMovementSpeed(member, source.Plan.Position, speed)
	speed *= e.npcSession.SlowMovementScale(source.Plan.ObjectID, now)
	if speed <= 0 {
		return e.finishAndResumeLocked()
	}
	if elapsed == 0 {
		e.runtime.registry.mutex.Unlock()
		err = e.schedule()
		if err != nil {
			e.runtime.registry.mutex.Lock()
			return e.failLocked(member, "followClockSchedule", err)
		}
		return nil, nil
	}
	step, err := e.npcSession.AdvancePursuit(member.zone.Navigation(), source.Plan.ObjectID,
		e.destination, 0.5, speed, source.NavigationRadius(), elapsed)
	if err != nil {
		return e.failLocked(member, "followAdvance", err)
	}
	if step.IsBlocked {
		return e.finishToPursuitLocked()
	}
	if zonegeometry.Distance(step.Position, target.Position) < max(float32(0.5), e.profile.Range) {
		return e.finishAndResumeLocked()
	}
	isFacingCommitted := e.npcSession.CommitFacing(zonenpc.AttackPlan{
		SourceObjectID: source.Plan.ObjectID, TargetObjectID: target.ObjectID,
		ActionGeneration: e.run.ActionGeneration, SourcePosition: step.Position, TargetPosition: target.Position,
	})
	if !isFacingCommitted {
		return e.finishAndResumeLocked()
	}
	var packets [][]byte
	if step.IsInRange {
		e.isMoving = false
		updated, isUpdated := e.npcSession.NPC(e.run.ObjectID)
		if !isUpdated {
			return e.finishAndResumeLocked()
		}
		packets, err = npcraknet.StopAtPose(e.run.ObjectID, step.Position, updated.Facing)
	} else {
		goal := e.destination
		if step.IsWaypointKnown {
			goal = step.Waypoint
		}
		positionPacket, positionErr := raknet.MarshalApplication(raknet.ObjectPositionUpdateMessage{
			ObjectID: e.run.ObjectID, PositionX: step.Position.X, PositionY: step.Position.Y, PositionZ: step.Position.Z,
		})
		if positionErr != nil {
			return e.failLocked(member, "followPosition", positionErr)
		}
		// MoveToPointExact + FaceObjectDuringMove is flag 0x101. Object-follow
		// flag 0x40 would steer toward the actor instead of this lateral goal.
		packets, err = npcraknet.CircleTargetMovement(e.run.ObjectID, goal, target.ObjectID)
		packets = append([][]byte{positionPacket}, packets...)
	}
	if err != nil {
		return e.failLocked(member, "followMoveMarshal", err)
	}
	e.runtime.registry.mutex.Unlock()
	err = e.schedule()
	if err != nil {
		e.runtime.registry.mutex.Lock()
		return e.failLocked(member, "followNextSchedule", err)
	}
	return packets, nil
}

func (e *campaignFollowStrafeRun) selectDestinationLocked(member gameplayPeerSession,
	source zonenpc.Snapshot, target game.Vec3,
) (game.Vec3, bool, error) {
	if zonegeometry.Distance(source.Plan.Position, target) <= 8 {
		return target, true, nil
	}
	random := member.zone.NPCRandom()
	if random == nil {
		return game.Vec3{}, false, nil
	}
	// Preserve the Lua operands rather than assuming inclusive integer bounds:
	// build 103's two-argument math.random excludes its upper argument.
	lateral, err := sim.ScriptRandom(random, 0, 5)
	if err != nil {
		return game.Vec3{}, false, fmt.Errorf("followLateralDraw: %w", err)
	}
	forward, err := sim.ScriptRandom(random, 4, 7)
	if err != nil {
		return game.Vec3{}, false, fmt.Errorf("followForwardDraw: %w", err)
	}
	direction := target.Sub(source.Plan.Position)
	direction = direction.Scale(1 / direction.Length())
	facing := source.Facing
	facingLength := float32(math.Hypot(float64(facing.X), float64(facing.Y)))
	if facingLength == 0 {
		facing = direction
		facingLength = float32(math.Hypot(float64(facing.X), float64(facing.Y)))
	}
	if facingLength == 0 {
		return game.Vec3{}, false, nil
	}
	right := game.Vec3{X: facing.Y / facingLength, Y: -facing.X / facingLength}
	if e.isLeft {
		right = right.Scale(-1)
	}
	desired := source.Plan.Position.Add(right.Scale(float32(lateral))).Add(game.Vec3{
		X: direction.X * float32(forward), Y: direction.Y * float32(forward),
	})
	projected, isFound, err := zoneaction.NPCProjectPosition(member.zone.Navigation(), desired,
		source.NavigationRadius(), source.Navigation)
	if err != nil {
		return game.Vec3{}, false, fmt.Errorf("followProject: %w", err)
	}
	return projected, isFound, nil
}

func (e *campaignFollowStrafeRun) finishLocked() ([][]byte, bool, error) {
	isFinished, isStopAllowed := e.npcSession.FinishTargetTooFar(e.run)
	if !isFinished || !isStopAllowed {
		return nil, false, nil
	}
	source, isFound := e.npcSession.NPC(e.run.ObjectID)
	if !isFound || source.IsDefeated {
		return nil, false, nil
	}
	packets, err := npcraknet.StopAtPose(e.run.ObjectID, source.Plan.Position, source.Facing)
	if err != nil {
		return nil, false, fmt.Errorf("followStop: %w", err)
	}
	return packets, true, nil
}

func (e *campaignFollowStrafeRun) finishAndResumeLocked() ([][]byte, error) {
	packets, isResumable, err := e.finishLocked()
	e.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("followFinish: %w", err)
	}
	if !isResumable {
		return packets, nil
	}
	resumePackets, err := e.onArrival(e.timestamp)
	if err != nil {
		return packets, fmt.Errorf("followResume: %w", err)
	}
	return append(packets, resumePackets...), nil
}

func (e *campaignFollowStrafeRun) finishToPursuitLocked() ([][]byte, error) {
	packets, isResumable, err := e.finishLocked()
	e.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("followFallbackFinish: %w", err)
	}
	if !isResumable {
		return packets, nil
	}
	// Bypass TargetTooFar selection once after an unavailable goal/corridor.
	// Otherwise its authored zero cooldown could immediately re-enter at the
	// same blocked pose. Ordinary pursuit retains its existing release/backoff.
	err = e.runtime.scheduleActionCorrection(e.packet, e.sessionKey, e.generation,
		e.run.ActionGeneration, e.run.ObjectID, 0, e.timestamp, game.Vec3{},
		e.timestamp, e.profile, e.onArrival, e.runtime.now(), false, false)
	if err != nil {
		return packets, fmt.Errorf("followFallbackSchedule: %w", err)
	}
	return packets, nil
}

func (e *campaignFollowStrafeRun) failLocked(member gameplayPeerSession, step string, err error) ([][]byte, error) {
	packets, isResumable, finishErr := e.finishLocked()
	if isResumable && finishErr == nil {
		e.runtime.registry.queueChannelPresentationLocked(member.zone, packets)
	}
	e.runtime.registry.mutex.Unlock()
	return nil, fmt.Errorf("%s: %w", step, errors.Join(err, finishErr))
}
