package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/zone"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

// Chunk265's idle jump owns motion while the parent cooldown elapses.
type nomadFrogJump struct {
	cast        *nomadFrogCast
	origin      game.Vec3
	destination game.Vec3
	direction   game.Vec3
	duration    time.Duration
	elapsed     time.Duration
}

func (e *nomadFrogCast) idle() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.currentLocked(member, isFound) {
		e.runtime.registry.mutex.RUnlock()
		return nil, nil
	}
	source, isSourceFound := member.zone.NPCs().NPC(e.plan.SourceObjectID)
	target, isTargetFound := member.campaignNPCTarget(e.generation, source.TargetObjectID)
	isMobile := member.isCampaignNPCActionActiveAt(e.generation, e.plan.SourceObjectID, e.runtime.now()) && member.zone.NPCs().RootRemaining(e.plan.SourceObjectID, e.runtime.now()) == 0
	world := member.zone
	e.runtime.registry.mutex.RUnlock()
	if !isSourceFound {
		return nil, nil
	}
	if !e.runtime.now().Before(e.readyAt) {
		return e.resume(e.timestamp + uint64(e.plan.Profile.ReleaseDelay/time.Millisecond))
	}
	if !isTargetFound || !isMobile {
		return nil, e.schedule(250*time.Millisecond, e.idle)
	}
	delta := target.Position.Sub(source.Plan.Position)
	delta.Z = 0
	distance := delta.Length()
	travel := min(float32(10), distance-source.Plan.ActorFootprintRadius()-target.ActorFootprintRadius-1)
	if distance <= 4 || travel < 3 {
		return e.walkFrogFallback(source, target)
	}
	direction := delta.Scale(1 / distance)
	destination := source.Plan.Position.Add(direction.Scale(travel))
	mesh := world.Navigation()
	if mesh != nil {
		projected, isReachable, err := zoneaction.NPCDirectMovementDestination(mesh, source.Plan.Position, destination, source.Plan.ActorFootprintRadius(), source.Navigation)
		if err != nil {
			return nil, fmt.Errorf("frogJumpDestination: %w", err)
		}
		if !isReachable || projected.Sub(source.Plan.Position).Length() < 3 {
			return e.walkFrogFallback(source, target)
		}
		destination = projected
	}
	jump := &nomadFrogJump{cast: e, origin: source.Plan.Position, destination: destination, direction: direction}
	packet, err := npcraknet.AnimationState(e.plan.SourceObjectID, "nomad_lieu_lf_4_jump_part_1", e.timestamp)
	if err != nil {
		return nil, fmt.Errorf("frogJumpWarmup: %w", err)
	}
	err = e.schedule(600000024*time.Nanosecond, jump.launch)
	if err != nil {
		return nil, fmt.Errorf("frogJumpWarmupSchedule: %w", err)
	}
	return [][]byte{packet}, nil
}

// Chunk265 computes footprint+1.5 but never passes that temporary to the
// MoveTowardObject call. Its actual continuation threshold is center distance2.
// This movement-only path never constructs a melee damage plan.
func (e *nomadFrogCast) walkFrogFallback(source zonenpc.Snapshot, target zone.NPCTarget) ([][]byte, error) {
	if source.Plan.Position.Sub(target.Position).Length() <= 2 {
		return nil, e.schedule(time.Second, e.idle)
	}
	packets, err := npcraknet.PursuitRedirect(e.plan.SourceObjectID, source.Plan.Position, target.ObjectID, target.Position, 2)
	if err != nil {
		return nil, fmt.Errorf("frogWalkMarshal: %w", err)
	}
	walk := &nomadFrogWalk{cast: e, movementAt: e.runtime.now()}
	err = e.schedule(campaignPursuitFallbackTick, walk.advance)
	if err != nil {
		return nil, fmt.Errorf("frogWalkSchedule: %w", err)
	}
	return packets, nil
}

type nomadFrogWalk struct {
	cast       *nomadFrogCast
	movementAt time.Time
}

func (e *nomadFrogWalk) advance() ([][]byte, error) {
	e.cast.runtime.registry.mutex.Lock()
	member, isFound := e.cast.runtime.registry.sessions[e.cast.sessionKey]
	if !e.cast.currentLocked(member, isFound) {
		e.cast.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	source, isSourceFound := member.zone.NPCs().NPC(e.cast.plan.SourceObjectID)
	target, isTargetFound := member.campaignNPCTarget(e.cast.generation, source.TargetObjectID)
	isMobile := member.isCampaignNPCActionActiveAt(e.cast.generation, e.cast.plan.SourceObjectID, e.cast.runtime.now()) && member.zone.NPCs().RootRemaining(e.cast.plan.SourceObjectID, e.cast.runtime.now()) == 0
	if !isSourceFound || !isTargetFound || !isMobile {
		e.cast.runtime.registry.mutex.Unlock()
		return nil, e.cast.schedule(campaignPursuitFallbackTick, e.cast.idle)
	}
	distance := target.Position.Sub(source.Plan.Position).Length()
	movementProfile, isProfileFound := zonenpc.ActionProfileForPlan(source.Plan)
	if !isProfileFound {
		e.cast.runtime.registry.mutex.Unlock()
		return nil, e.cast.schedule(time.Second, e.cast.idle)
	}
	travel := min(max(float32(0), distance-2), float32(max(time.Duration(0), e.cast.runtime.now().Sub(e.movementAt)).Seconds())*graspingDeadMovementSpeed(member, source.Plan.Position, movementProfile.MovementSpeed)*member.zone.NPCs().SlowMovementScale(e.cast.plan.SourceObjectID, e.cast.runtime.now()))
	position := source.Plan.Position
	if travel > 0 && member.zone.Navigation() != nil {
		step, err := zoneaction.AdvancePursuitPathStep(member.zone.Navigation(), position, target.Position, source.Plan.ActorFootprintRadius(), travel, source.Navigation)
		if err != nil {
			e.cast.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("frogWalkPath: %w", err)
		}
		position = step.Position
	} else if travel > 0 {
		position = position.Add(target.Position.Sub(position).Scale(travel / distance))
	}
	err := member.zone.NPCs().SetPosition(e.cast.plan.SourceObjectID, position)
	e.cast.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("frogWalkPosition: %w", err)
	}
	return e.cast.idle()
}

func (e *nomadFrogJump) launch() ([][]byte, error) {
	e.cast.runtime.registry.mutex.RLock()
	member, isFound := e.cast.runtime.registry.sessions[e.cast.sessionKey]
	isCurrent := e.cast.currentLocked(member, isFound) && member.isCampaignNPCActionActiveAt(e.cast.generation, e.cast.plan.SourceObjectID, e.cast.runtime.now())
	e.cast.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, e.cast.schedule(time.Second, e.cast.idle)
	}
	e.duration = time.Duration(float64(e.destination.Sub(e.origin).Length()) / 20 * float64(time.Second))
	animation, err := npcraknet.AnimationState(e.cast.plan.SourceObjectID, "nomad_lieu_lf_4_jump_part_2", e.cast.timestamp)
	if err != nil {
		return nil, fmt.Errorf("frogJumpAnimation: %w", err)
	}
	packet, err := raknet.MarshalApplication(raknet.ObjectJumpMessage{ObjectID: e.cast.plan.SourceObjectID,
		JumpPosition:  raknet.Vector3{X: e.destination.X, Y: e.destination.Y, Z: e.destination.Z},
		JumpDirection: raknet.Vector3{X: e.direction.X, Y: e.direction.Y, Z: e.direction.Z}, JumpParameters: [4]float32{20, 2, 3, 8}})
	if err != nil {
		return nil, fmt.Errorf("frogJumpMarshal: %w", err)
	}
	err = e.cast.schedule(min(campaignHopperFlightStep, e.duration), e.advance)
	if err != nil {
		return nil, fmt.Errorf("frogFlightSchedule: %w", err)
	}
	return [][]byte{animation, packet}, nil
}

func (e *nomadFrogJump) advance() ([][]byte, error) {
	e.elapsed = min(e.duration, e.elapsed+campaignHopperFlightStep)
	distance := e.destination.Sub(e.origin).Length()
	traveled := min(distance, float32(e.elapsed.Seconds())*20)
	position := e.origin.Add(e.destination.Sub(e.origin).Scale(traveled / distance))
	// The native jump uses minimum height2, maximum3, and arc width8.
	half := distance * .5
	width := min(float32(8), half)
	peak := max(float32(2), min(float32(3), 3*distance/16))
	denominator := half*half - (half-width)*(half-width)
	if denominator > 0 && e.elapsed < e.duration {
		position.Z += max(float32(0), min(peak, peak/denominator*(half*half-(traveled-half)*(traveled-half))))
	}
	e.cast.runtime.registry.mutex.Lock()
	member, isFound := e.cast.runtime.registry.sessions[e.cast.sessionKey]
	if !e.cast.currentLocked(member, isFound) {
		e.cast.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	err := member.zone.NPCs().SetPosition(e.cast.plan.SourceObjectID, position)
	e.cast.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("frogFlightPosition: %w", err)
	}
	if e.elapsed < e.duration {
		return nil, e.cast.schedule(min(campaignHopperFlightStep, e.duration-e.elapsed), e.advance)
	}
	packet, err := npcraknet.AnimationState(e.cast.plan.SourceObjectID, "nomad_lieu_lf_4_jump_part_3", e.cast.timestamp)
	if err != nil {
		return nil, fmt.Errorf("frogJumpLanding: %w", err)
	}
	err = e.cast.schedule(time.Second, e.cast.idle)
	if err != nil {
		return nil, fmt.Errorf("frogJumpRetry: %w", err)
	}
	return [][]byte{packet}, nil
}
