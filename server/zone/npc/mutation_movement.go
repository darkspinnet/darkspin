package npc

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/navigation"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

// MutationMovementState is the private table of one admitted Wander behavior.
// Its lifecycle owner retains this value; it is not a second actor registry.
type MutationMovementState struct {
	TargetObjectID    uint32
	TargetGeneration  uint64
	Origin            game.Vec3
	Destination       game.Vec3
	Waypoint          game.Vec3
	MovementSpeed     float32
	MovementSpeedBuff float32
	UpdatedAt         time.Time
	CheckAt           time.Time
	IsInitialized     bool
	IsFleeing         bool
	IsPaused          bool
	IsApproaching     bool
}

// approachMutationCandidate owns movement until the selected friendly actor is
// inside the established NPC pursuit stop distance. Core calls this INSTEAD of
// Wander for this tick and starts Beam only when the returned status is true.
func (e *Session) approachMutationCandidate(source *Snapshot, target Snapshot, req MutationAdvanceRequest) (bool, error) {
	if source.Mutation.EventCount >= uint32(len(source.Mutation.Events)) {
		return false, nil
	}
	if req.At.IsZero() || source.IsDefeated || target.IsDefeated || target.HitPoint <= 0 ||
		!zonegeometry.IsFinite(target.Plan.Position) {
		return false, errors.New("mutation approach unavailable")
	}
	stopDistance, err := zoneaction.NPCStopDistance(12, source.Plan.ActorFootprintRadius(), target.Plan.ActorFootprintRadius())
	if err != nil {
		return false, fmt.Errorf("mutationApproachRange: %w", err)
	}
	candidate := *source
	previous := candidate.Mutation.Movement
	state := previous
	isSameTarget := state.IsApproaching && state.TargetObjectID == target.Plan.ObjectID &&
		state.TargetGeneration == target.Mutation.Generation
	if !isSameTarget {
		state.UpdatedAt = req.At
	}
	state.TargetObjectID = target.Plan.ObjectID
	state.TargetGeneration = target.Mutation.Generation
	state.IsApproaching = true
	state.Destination = target.Plan.Position
	state.CheckAt = req.At.Add(50 * time.Millisecond)
	distance := zonegeometry.Distance(candidate.Plan.Position, target.Plan.Position)
	isInRange := distance < stopDistance
	isBlocked := candidate.Mutation.Beam.IsActive || isMutationMovementBlocked(candidate, req.At)
	speed, movementBuff, err := mutationApproachSpeed(candidate, req.At)
	if err != nil {
		return false, fmt.Errorf("mutationApproachSpeed: %w", err)
	}
	if !isInRange && !isBlocked {
		elapsed := req.At.Sub(state.UpdatedAt)
		if elapsed < 0 {
			return false, errors.New("mutation approach clock reversed")
		}
		travel := min(speed*float32(elapsed.Seconds()), max(float32(0), distance-stopDistance+
			pursuitArrivalMargin(candidate.Plan.Position, target.Plan.Position, stopDistance)))
		if travel > 0 {
			step, stepErr := zoneaction.AdvancePursuitPathStep(e.navigationMesh, candidate.Plan.Position,
				target.Plan.Position, candidate.NavigationRadius(), travel, candidate.Navigation)
			if stepErr != nil {
				isBlocked = true
			} else if step.Position != candidate.Plan.Position {
				candidate.Facing = directionTo(candidate.Plan.Position, step.Position)
				candidate.Plan.Position = step.Position
				candidate.PositionRevision++
			}
		}
		isInRange = zonegeometry.Distance(candidate.Plan.Position, target.Plan.Position) < stopDistance
	}
	state.UpdatedAt = req.At
	state.MovementSpeedBuff = movementBuff
	state.IsPaused = isBlocked || isInRange
	state.Waypoint = candidate.Plan.Position
	state.MovementSpeed = 0
	if !state.IsPaused {
		waypoint, isPathFound := e.mutationWaypointLocked(candidate, target.Plan.Position)
		state.IsPaused = !isPathFound
		if isPathFound {
			// The endpoint carries the same stop distance as server advancement.
			goal := target.Plan.Position.Add(directionTo(target.Plan.Position, candidate.Plan.Position).Scale(stopDistance))
			if waypoint == target.Plan.Position {
				waypoint = goal
			}
			state.Waypoint = waypoint
			state.MovementSpeed = speed
		}
	}
	candidate.Mutation.Movement = state
	isChanged := candidate.Plan.Position != source.Plan.Position || previous.Waypoint != state.Waypoint ||
		previous.MovementSpeed != state.MovementSpeed || previous.MovementSpeedBuff != state.MovementSpeedBuff ||
		previous.IsPaused != state.IsPaused || !isSameTarget
	if isChanged {
		isQueued := appendMutationEvent(&candidate, MutationEvent{Kind: MutationEventMovement, At: req.At,
			Position: candidate.Plan.Position, Facing: candidate.Facing, Waypoint: state.Waypoint,
			MovementSpeed: state.MovementSpeed, MovementSpeedBuff: state.MovementSpeedBuff})
		if !isQueued {
			return false, errors.New("mutation approach outbox full")
		}
	}
	*source = candidate
	return isInRange && !isBlocked, nil
}

func mutationApproachSpeed(actor Snapshot, at time.Time) (float32, float32, error) {
	profile, isFound := ActionProfileForPlan(actor.Plan)
	if !isFound {
		return 0, 0, errors.New("mutation approach profile unavailable")
	}
	speed := profile.MovementSpeed
	bonus := EffectiveMovementSpeedBuff(actor.Plan)
	if actor.Mutation.Movement.IsFleeing {
		baseSpeed := actor.Plan.NPCProfile.BaseCombatSpeed
		if baseSpeed <= 0 {
			baseSpeed = speed / max(float32(0.01), 1+bonus)
		}
		speed += baseSpeed * 1.5
		bonus += 1.5
	}
	scale := float32(1)
	if at.Before(actor.status.slowExpiresAt) {
		scale = max(float32(0.05), actor.status.slowMovementScale)
	}
	scale *= areaShiftMovementScale(actor, at)
	return speed * max(float32(0.05), scale), bonus, nil
}

type MutationMovementRequest struct {
	ObjectID        uint32
	State           MutationMovementState
	PlayerPositions []game.Vec3
	At              time.Time
	IsPaused        bool
}

type MutationMovementResult struct {
	State             MutationMovementState
	Position          game.Vec3
	Waypoint          game.Vec3
	MovementSpeed     float32
	MovementSpeedBuff float32
	IsChanged         bool
	IsBlocked         bool
}

func (e *Session) mutationCandidate(source Snapshot, req MutationAdvanceRequest) (Snapshot, bool) {
	allyReq := MutationAllyRequest{ObjectID: source.Plan.ObjectID, At: req.At,
		IsPassiveAvailable: true, IsSlotAvailable: true}
	for _, target := range req.Targets {
		if target.ObjectID == source.TargetObjectID && target.IsAlive {
			allyReq.BestTargetPosition = target.Position
			allyReq.IsBestTargetAvailable = true
			break
		}
	}
	if target, isFound := e.npcs[source.TargetObjectID]; isFound && !target.IsDefeated {
		allyReq.BestTargetPosition = target.Plan.Position
		allyReq.IsBestTargetAvailable = true
	}
	return e.selectMutationAllyLocked(source, allyReq)
}

func (e *Session) advanceMutationMovement(source *Snapshot, req MutationAdvanceRequest) error {
	if source.Mutation.EventCount >= uint32(len(source.Mutation.Events)) {
		return nil
	}
	candidate := *source
	if candidate.Mutation.Movement.IsApproaching && !candidate.Mutation.Beam.IsActive {
		candidate.Mutation.Movement = MutationMovementState{}
	}
	positions := make([]game.Vec3, 0, len(req.ControlledTargets))
	for _, target := range req.ControlledTargets {
		positions = append(positions, target.Position)
	}
	result, err := e.advanceMutationMovementLocked(&candidate, MutationMovementRequest{
		ObjectID: source.Plan.ObjectID, State: candidate.Mutation.Movement,
		PlayerPositions: positions, At: req.At, IsPaused: source.Mutation.Beam.IsActive,
	})
	if err != nil {
		return fmt.Errorf("mutationMovement: %w", err)
	}
	candidate.Mutation.Movement = result.State
	if result.IsChanged {
		isQueued := appendMutationEvent(&candidate, MutationEvent{
			Kind: MutationEventMovement, At: req.At, Position: result.Position,
			Facing: candidate.Facing, Waypoint: result.State.Waypoint,
			MovementSpeed:     result.State.MovementSpeed,
			MovementSpeedBuff: result.State.MovementSpeedBuff,
		})
		if !isQueued {
			return errors.New("mutation movement outbox full")
		}
	}
	*source = candidate
	return nil
}

func (e *Session) advanceMutationMovementLocked(source *Snapshot, req MutationMovementRequest) (MutationMovementResult, error) {
	actor := *source
	if actor.IsDefeated || actor.Plan.NounName != MutationAgentNounName {
		return MutationMovementResult{}, nil
	}
	state := req.State
	result := MutationMovementResult{State: state, Position: actor.Plan.Position, Waypoint: state.Destination}
	if req.At.IsZero() || (state.IsInitialized && (!zonegeometry.IsFinite(state.Origin) ||
		!zonegeometry.IsFinite(state.Destination) || req.At.Before(state.UpdatedAt))) {
		return result, errors.New("mutation movement state invalid")
	}
	if req.IsPaused || isMutationMovementBlocked(actor, req.At) {
		// Paused elapsed time must not become a movement burst on resume.
		result.State.UpdatedAt = req.At
		result.State.Waypoint = actor.Plan.Position
		result.State.MovementSpeed = 0
		result.State.MovementSpeedBuff = EffectiveMovementSpeedBuff(actor.Plan)
		if state.IsFleeing {
			result.State.MovementSpeedBuff += 1.5
		}
		result.State.IsPaused = true
		result.IsChanged = !state.IsPaused || state.Waypoint != actor.Plan.Position ||
			state.MovementSpeed != 0 || state.MovementSpeedBuff != result.State.MovementSpeedBuff
		result.IsBlocked = true
		return result, nil
	}
	if !state.IsInitialized {
		state.Origin = actor.Plan.Position
		destination, err := e.mutationDestinationLocked(actor, state.Origin)
		if err != nil {
			return result, fmt.Errorf("mutationDestination: %w", err)
		}
		state.Destination = destination
		state.IsInitialized = true
		state.UpdatedAt = req.At
		state.CheckAt = req.At.Add(500 * time.Millisecond)
	}
	profile, isProfileFound := ActionProfileForPlan(actor.Plan)
	if !isProfileFound {
		return result, errors.New("mutation movement profile unavailable")
	}
	speed := profile.NonCombatMovementSpeed
	if IsCombatMovementState(actor.Plan, actor.TargetObjectID) {
		speed = profile.MovementSpeed
	}
	baseSpeed := actor.Plan.NPCProfile.IdleMovementSpeed
	if IsCombatMovementState(actor.Plan, actor.TargetObjectID) {
		baseSpeed = actor.Plan.NPCProfile.BaseCombatSpeed
	}
	if baseSpeed <= 0 {
		baseSpeed = speed / max(float32(0.01), 1+EffectiveMovementSpeedBuff(actor.Plan))
	}
	if state.IsFleeing {
		speed += baseSpeed * 1.5
	}
	// Match SlowMovementScale without reacquiring the already-held NPC lock.
	slowScale := float32(1)
	if req.At.Before(actor.status.slowExpiresAt) {
		slowScale = max(float32(0.05), actor.status.slowMovementScale)
	}
	slowScale *= areaShiftMovementScale(actor, req.At)
	speed *= max(float32(0.05), slowScale)
	elapsed := req.At.Sub(state.UpdatedAt)
	if elapsed > 0 && speed > 0 && actor.Plan.Position != state.Destination {
		step, err := zoneaction.AdvancePursuitPathStep(e.navigationMesh, actor.Plan.Position,
			state.Destination, actor.NavigationRadius(), speed*float32(elapsed.Seconds()), actor.Navigation)
		if err != nil {
			// An unavailable corridor owns no straight-line fallback through walls.
			result.IsBlocked = true
		} else {
			actor.Facing = directionTo(actor.Plan.Position, step.Position)
			actor.Plan.Position = step.Position
			result.Waypoint = step.Waypoint
		}
	}
	state.UpdatedAt = req.At
	if !req.At.Before(state.CheckAt) || zonegeometry.Distance(actor.Plan.Position, state.Destination) <= 1 {
		direction := game.Vec3{}
		isPlayerNear := false
		for _, position := range req.PlayerPositions {
			if !zonegeometry.IsFinite(position) || zonegeometry.Distance(position, actor.Plan.Position) >= 8 {
				continue
			}
			direction = direction.Add(directionTo(position, actor.Plan.Position))
			isPlayerNear = true
		}
		if isPlayerNear {
			direction = directionTo(game.Vec3{}, direction)
			destination, isClear, err := e.mutationFleeDestinationLocked(actor, direction)
			if err != nil {
				return result, fmt.Errorf("mutationFlee: %w", err)
			}
			if !isClear {
				destination, err = e.mutationDestinationLocked(actor, state.Origin)
				if err != nil {
					return result, fmt.Errorf("mutationFallback: %w", err)
				}
			}
			state.Destination = destination
			state.IsFleeing = true
		} else {
			if zonegeometry.Distance(actor.Plan.Position, state.Destination) < 2 {
				destination, err := e.mutationDestinationLocked(actor, state.Origin)
				if err != nil {
					return result, fmt.Errorf("mutationWander: %w", err)
				}
				state.Destination = destination
			}
			state.IsFleeing = false
		}
		state.CheckAt = req.At.Add(500 * time.Millisecond)
		result.Waypoint = state.Destination
	}
	result.Position = actor.Plan.Position
	state.MovementSpeedBuff = EffectiveMovementSpeedBuff(actor.Plan)
	if state.IsFleeing {
		state.MovementSpeedBuff += 1.5
	}
	if state.IsFleeing != req.State.IsFleeing {
		fleeSpeed := baseSpeed * 1.5 * max(float32(0.05), slowScale)
		if state.IsFleeing {
			speed += fleeSpeed
		} else {
			speed -= fleeSpeed
		}
	}
	waypoint, isPathFound := e.mutationWaypointLocked(actor, state.Destination)
	state.Waypoint = waypoint
	state.MovementSpeed = speed
	state.IsPaused = !isPathFound || actor.Plan.Position == state.Destination
	if state.IsPaused {
		state.Waypoint = actor.Plan.Position
		state.MovementSpeed = 0
	}
	result.State = state
	result.IsChanged = result.Position != source.Plan.Position || state.Waypoint != req.State.Waypoint ||
		state.MovementSpeed != req.State.MovementSpeed || state.MovementSpeedBuff != req.State.MovementSpeedBuff ||
		state.IsPaused != req.State.IsPaused || !req.State.IsInitialized
	if result.Position != source.Plan.Position {
		actor.PositionRevision++
	}
	*source = actor
	return result, nil
}

func (e *Session) mutationWaypointLocked(actor Snapshot, destination game.Vec3) (game.Vec3, bool) {
	if e.navigationMesh == nil {
		return actor.Plan.Position, false
	}
	planLayer, isLayerFound := e.navigationMesh.ActorLayer(actor.NavigationRadius(), actor.Navigation)
	if !isLayerFound {
		return actor.Plan.Position, false
	}
	path, err := e.navigationMesh.CreatePath(navigation.Vec3(actor.Plan.Position), navigation.Vec3(destination), navigation.PathOptions{
		ProjectionOptions: navigation.ProjectionOptions{PlanLayer: planLayer, MaxDistance: 12}, MaxVisitedPolygon: 2048,
	})
	if err != nil {
		return actor.Plan.Position, false
	}
	for _, point := range path.Points {
		waypoint := game.Vec3(point)
		if waypoint != actor.Plan.Position {
			return waypoint, true
		}
	}
	return actor.Plan.Position, false
}

// Mirror RootRemaining/StunRemaining under the caller's lock. Cinematic and
// navigation holds remain effective independently of shield-suppressed debuffs.
func isMutationMovementBlocked(actor Snapshot, at time.Time) bool {
	if !actor.IsNavigationCollisionEnabled || actor.IsIntroductionPending() ||
		actor.cinematic.isPending || at.Before(actor.cinematic.immobilizedUntil) ||
		at.Before(actor.cinematic.intangibleUntil) {
		return true
	}
	isStatusSuppressed := at.Before(actor.status.chargeProtectionEnd) || actor.IsTurtleActive
	if actor.IsShieldActive {
		shieldDuration, isInvincitron := NomadWithDroneShieldDuration(actor.Plan.NounName)
		if isInvincitron && shieldDuration > 0 {
			return true
		}
		shieldProfile, isDirectional := NomadShielderDirectionalShieldProfile(actor.Plan.NounName)
		if !isDirectional || shieldProfile.SetupAnimationName == "" {
			isStatusSuppressed = true
		}
	}
	if isStatusSuppressed {
		return false
	}
	return at.Before(actor.status.rootExpiresAt) || at.Before(actor.status.bioEntangleExpiresAt) ||
		at.Before(actor.status.supportHealerRootExpiresAt) || at.Before(actor.status.stunExpiresAt) ||
		at.Before(actor.status.banishExpiresAt) || at.Before(actor.status.sleepExpiresAt)
}

func (e *Session) mutationDestinationLocked(actor Snapshot, origin game.Vec3) (game.Vec3, error) {
	if e.startupRandom == nil || e.navigationMesh == nil {
		return game.Vec3{}, errors.New("mutation navigation or random unavailable")
	}
	planLayer, isLayerFound := e.navigationMesh.ActorLayer(actor.NavigationRadius(), actor.Navigation)
	if !isLayerFound {
		return game.Vec3{}, errors.New("mutation navigation layer unavailable")
	}
	destination := origin
	for attempt := 0; attempt < 20; attempt++ {
		angle := e.startupRandom.Float64() * math.Pi * 2
		destination = game.Vec3{X: origin.X + float32(math.Cos(angle)*8), Y: origin.Y + float32(math.Sin(angle)*8), Z: origin.Z}
		path, err := e.navigationMesh.CreatePath(navigation.Vec3(actor.Plan.Position), navigation.Vec3(destination), navigation.PathOptions{
			ProjectionOptions: navigation.ProjectionOptions{PlanLayer: planLayer, MaxDistance: 0.01}, MaxVisitedPolygon: 2048,
		})
		if err == nil && len(path.Points) > 0 {
			return destination, nil
		}
	}
	// Lua ignores the helper's false status and retains its last candidate.
	// Movement still requires a safe path before changing authoritative position.
	return destination, nil
}

func (e *Session) mutationFleeDestinationLocked(actor Snapshot, direction game.Vec3) (game.Vec3, bool, error) {
	// A direct corridor slightly beyond four units is a conservative adapter
	// for native DistanceToObstacle > 4, not a claim of raycast equivalence.
	desired := actor.Plan.Position.Add(direction.Scale(4.01))
	clearDestination, isClear, err := zoneaction.NPCDirectMovementDestination(e.navigationMesh,
		actor.Plan.Position, desired, actor.NavigationRadius(), actor.Navigation)
	if err != nil {
		return game.Vec3{}, false, fmt.Errorf("mutationClear: %w", err)
	}
	if !isClear || zonegeometry.Distance(actor.Plan.Position, clearDestination) <= 4 {
		return actor.Plan.Position, false, nil
	}
	destination, isProjected, err := zoneaction.NPCProjectPosition(e.navigationMesh,
		actor.Plan.Position.Add(direction.Scale(4)), actor.NavigationRadius(), actor.Navigation)
	if err != nil {
		return game.Vec3{}, false, fmt.Errorf("mutationProject: %w", err)
	}
	return destination, isProjected, nil
}

type MutationAllyRequest struct {
	ObjectID              uint32
	BestTargetPosition    game.Vec3
	At                    time.Time
	IsBestTargetAvailable bool
	IsPassiveAvailable    bool
	IsSlotAvailable       bool
}

// selectMutationAllyLocked implements this phase's Radius15/IgnoreMySpecies=true
// condition. Passive slot ownership and subsequent ability admission are separate.
func (e *Session) selectMutationAllyLocked(source Snapshot, req MutationAllyRequest) (Snapshot, bool) {
	if source.IsDefeated || !req.IsBestTargetAvailable || !req.IsPassiveAvailable || !req.IsSlotAvailable ||
		!zonegeometry.IsFinite(req.BestTargetPosition) || zonegeometry.Distance(source.Plan.Position, req.BestTargetPosition) >= 25 {
		return Snapshot{}, false
	}
	approach := source.Mutation.Movement
	if approach.IsApproaching {
		target, isFound := e.npcs[approach.TargetObjectID]
		if isFound && target.Mutation.Generation == approach.TargetGeneration && isMutationAllyCandidate(source, target, req.At) {
			return target, true
		}
	}
	selected := Snapshot{}
	nearestDistance := float32(math.Inf(1))
	for _, objectID := range e.objectIDs {
		candidate := e.npcs[objectID]
		if !isMutationAllyCandidate(source, candidate, req.At) {
			continue
		}
		distance := zonegeometry.Distance(source.Plan.Position, candidate.Plan.Position)
		if distance <= 5 || distance > 15 || distance >= nearestDistance {
			continue
		}
		selected = candidate
		nearestDistance = distance
	}
	return selected, selected.Plan.ObjectID != 0
}

func isMutationAllyCandidate(source Snapshot, candidate Snapshot, at time.Time) bool {
	if candidate.Plan.ObjectID == source.Plan.ObjectID || candidate.IsDefeated || candidate.HitPoint <= 0 ||
		candidate.Faction != source.Faction || candidate.Plan.NPCProfile.NounType != game.NounTypeCreature ||
		candidate.Plan.IsEliteModifierApplied() || strings.EqualFold(candidate.Plan.NounName, source.Plan.NounName) ||
		at.Before(candidate.status.intangibleExpiresAt) || at.Before(candidate.cinematic.intangibleUntil) {
		return false
	}
	distance := zonegeometry.Distance(source.Plan.Position, candidate.Plan.Position)
	return distance > 5 && distance <= 15
}
