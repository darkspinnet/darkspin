package gameplay

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

// moveLocked advances only the current behavior lease. Its caller owns the
// registry lock, interruptions, the deadline, and the final movement cleanup.
func (e *campaignNPCBehaviorRun) moveLocked(member *gameplayPeerSession,
	source zonenpc.Snapshot, now time.Time,
) ([][]byte, bool, error) {
	behavior := strings.ToLower(e.run.Choice.Behavior)
	switch behavior {
	case "nbehavior_wander", "nbehavior_stroll", "nbehavior_strafe", "nbehavior_smallstrafe",
		"nbehavior_flee", "nbehavior_fleefacing", "nbehavior_strafeoridle":
	default:
		// Patrol requires an authored initial waypoint and its target links.
		// No route can be inferred from unrelated marker or teleporter order.
		return nil, true, nil
	}
	if now.Before(e.pauseUntil) {
		return nil, false, nil
	}
	isWander := behavior == "nbehavior_wander"
	isStroll := behavior == "nbehavior_stroll"
	isStrafeOrIdle := behavior == "nbehavior_strafeoridle"
	isStrafe := behavior == "nbehavior_strafe" || behavior == "nbehavior_smallstrafe" || isStrafeOrIdle
	isFlee := behavior == "nbehavior_flee" || behavior == "nbehavior_fleefacing"
	isFacing := isStrafe || behavior == "nbehavior_fleefacing"
	targetObjectID := source.TargetObjectID
	if (behavior == "nbehavior_strafe" || behavior == "nbehavior_smallstrafe") && e.strafe != nil {
		targetObjectID = e.strafeTargetID
	}
	target, isTargetFound := member.campaignNPCTarget(e.generation, targetObjectID)
	if (isStrafe || isFlee) && !isTargetFound && behavior != "nbehavior_strafe" && behavior != "nbehavior_smallstrafe" {
		return nil, true, nil
	}
	if member.zone.NPCRandom() == nil {
		return nil, true, nil
	}
	stopDistance := max(source.Plan.ActorFootprintRadius()*1.5, float32(0.01))
	if isStroll {
		stopDistance = 0.25
	}
	var packets [][]byte
	// Keep this as a variable so Duration truncates the Lua float32 seconds
	// at runtime; Go rejects a fractional-nanosecond constant conversion.
	flavorPauseSeconds := float32(4.4)
	flavorPause := time.Duration(float64(flavorPauseSeconds) * float64(time.Second))
	if !e.isMoving {
		if behavior == "nbehavior_strafe" || behavior == "nbehavior_smallstrafe" {
			startPackets, startErr := e.startLuaStrafeLocked(member, source, target, isTargetFound, now)
			if startErr != nil {
				return nil, false, fmt.Errorf("behaviorStrafeStart: %w", startErr)
			}
			packets = append(packets, startPackets...)
			updated, isFound := member.zone.NPCs().NPC(source.Plan.ObjectID)
			if !isFound {
				return packets, true, nil
			}
			source = updated
		}
		// Build-103 chunks 400 and 451 both define StrafeOrIdle, with
		// different probabilities (.8 and .5). Preserve Dark Spin's existing
		// .5 policy until the native global registration winner is established.
		if isStrafeOrIdle && member.zone.NPCRandom().Float64() >= 0.5 {
			e.pauseUntil = now.Add(flavorPause)
			idlePackets, err := e.strafeIdleLocked(member, source, target.ObjectID, target.Position)
			if err != nil {
				return nil, false, fmt.Errorf("behaviorStrafeIdle: %w", err)
			}
			return idlePackets, false, nil
		}
		if isFlee {
			if e.flee == nil {
				delay := 3 * time.Second
				if isFacing {
					delay = time.Second
				}
				e.flee = &campaignNPCSupportIdle{nextAt: now.Add(delay)}
			}
			if now.Before(e.flee.pauseUntil) {
				return nil, false, nil
			}
			safeDistance := float32(14)
			if isFacing {
				safeDistance = 10
			}
			if zonegeometry.Distance(source.Plan.Position, target.Position) >= safeDistance {
				e.pauseUntil = now.Add(500 * time.Millisecond)
				if isFacing {
					turns, err := e.faceMovementTargetLocked(member, source, target.ObjectID, target.Position)
					if err != nil {
						return nil, false, fmt.Errorf("behaviorSafeFace: %w", err)
					}
					return turns, false, nil
				}
				return nil, false, nil
			}
		}
		destination, isFound, err := e.movementDestinationLocked(member, source, target.Position)
		if err != nil {
			return nil, false, fmt.Errorf("behaviorDestination: %w", err)
		}
		if !isFound {
			return nil, true, nil
		}
		e.destination = destination
		e.isWaypointPublished = false
		e.isMoving = true
		e.wasMoving = true
	}
	if behavior == "nbehavior_strafe" || behavior == "nbehavior_smallstrafe" {
		stopDistance = e.strafePlan.NearGoalDistance
	}
	speed := source.Plan.NPCProfile.BaseCombatSpeed
	if isWander || isStroll {
		speed = source.Plan.NPCProfile.IdleMovementSpeed
	}
	speed *= max(float32(0), 1+zonenpc.EffectiveMovementSpeedBuff(source.Plan))
	speed *= member.zone.NPCs().SlowMovementScale(source.Plan.ObjectID, now)
	if speed <= 0 {
		return packets, true, nil
	}
	if (isWander || isStroll) && (!e.isIdleSpeedPublished || e.idleMovementSpeed != speed) {
		// Preplaced actors start with zero idle speed. Enable native walking
		// only when an authoritative idle movement is actually admitted.
		attributePacket, attributeErr := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
			ObjectID: source.Plan.ObjectID, Value: map[uint8]float32{
				11: source.Plan.NPCProfile.IdleMovementSpeed,
				48: (1+zonenpc.EffectiveMovementSpeedBuff(source.Plan))*
					member.zone.NPCs().SlowMovementScale(source.Plan.ObjectID, now) - 1,
			},
		})
		if attributeErr != nil {
			return nil, false, fmt.Errorf("behaviorIdleSpeed: %w", attributeErr)
		}
		e.isIdleSpeedPublished = true
		e.idleMovementSpeed = speed
		packets = append(packets, attributePacket)
	}
	step, err := member.zone.NPCs().AdvancePursuit(member.zone.Navigation(), source.Plan.ObjectID,
		e.destination, stopDistance, speed, source.NavigationRadius(), campaignNPCBehaviorTick)
	if err != nil {
		return nil, false, fmt.Errorf("behaviorAdvance: %w", err)
	}
	if step.IsBlocked || step.IsInRange {
		e.isMoving = false
		stops, stopErr := npcraknet.StopAtPose(source.Plan.ObjectID, step.Position, source.Facing)
		if stopErr != nil {
			return nil, false, fmt.Errorf("behaviorArrivalStop: %w", stopErr)
		}
		packets = append(packets, stops...)
		if step.IsBlocked {
			return packets, true, nil
		}
		source.Plan.Position = step.Position
		if isStroll || isWander {
			isTurnNeeded := isStroll
			if isWander && member.zone.NPCRandom().Float64() < float64(float32(0.35)) {
				isTurnNeeded = true
				e.pauseUntil = now.Add(flavorPause)
			}
			if isTurnNeeded && isTargetFound {
				turns, turnErr := e.faceMovementTargetLocked(member, source, target.ObjectID, target.Position)
				if turnErr != nil {
					return nil, false, fmt.Errorf("behaviorArrivalFace: %w", turnErr)
				}
				packets = append(packets, turns...)
			}
			return packets, isStroll, nil
		}
		if isStrafe {
			return packets, !isStrafeOrIdle, nil
		}
		movementBuff := (1+zonenpc.EffectiveMovementSpeedBuff(source.Plan))*
			member.zone.NPCs().SlowMovementScale(source.Plan.ObjectID, now) - 1
		if movementBuff > -0.5 {
			e.flee.advance(now, isFacing)
			attributes, updated, buffErr := e.setMovementBuffLocked(member, source, e.flee.movementBuff, now)
			if buffErr != nil {
				return nil, false, fmt.Errorf("behaviorFleeBuff: %w", buffErr)
			}
			packets = append(packets, attributes...)
			if updated.IsDefeated || updated.HitPoint <= 0 {
				return packets, true, nil
			}
		}
		return packets, false, nil
	}
	waypoint := e.destination
	if step.IsWaypointKnown {
		waypoint = step.Waypoint
	}
	var moves [][]byte
	if isFacing {
		moves, err = npcraknet.BoundedStrafe(source.Plan.ObjectID, step.Position, waypoint,
			target.ObjectID, target.Position, stopDistance)
		isFacingCommitted := member.zone.NPCs().CommitFacing(zonenpc.AttackPlan{SourceObjectID: source.Plan.ObjectID,
			TargetObjectID: target.ObjectID, ActionGeneration: source.ActionGeneration,
			SourcePosition: step.Position, TargetPosition: target.Position})
		if !isFacingCommitted {
			return packets, true, nil
		}
	} else {
		// Fixed-point movement lets the client interpolate toward the next
		// corridor waypoint. Position corrections here snap its root each tick.
		if !e.isWaypointPublished || e.publishedWaypoint != waypoint {
			moves, err = npcraknet.PursuitWaypoint(source.Plan.ObjectID, waypoint)
			if err == nil {
				e.isWaypointPublished = true
				e.publishedWaypoint = waypoint
			}
		}
	}
	if err != nil {
		return nil, false, fmt.Errorf("behaviorMoveMarshal: %w", err)
	}
	return append(packets, moves...), false, nil
}

func (e *campaignNPCBehaviorRun) movementDestinationLocked(member *gameplayPeerSession,
	source zonenpc.Snapshot, target game.Vec3,
) (game.Vec3, bool, error) {
	behavior := strings.ToLower(e.run.Choice.Behavior)
	if behavior == "nbehavior_strafe" || behavior == "nbehavior_smallstrafe" {
		return game.Vec3(e.strafePlan.Destination), e.strafePlan.IsMoveRequested && e.strafePlan.IsWaiting, nil
	}
	random := member.zone.NPCRandom()
	if behavior == "nbehavior_flee" || behavior == "nbehavior_fleefacing" {
		minimum, maximum := float32(5), float32(10)
		if e.flee.isTired {
			minimum, maximum = 7, 8
		}
		destination, isFound, err := supportRetreatDestination(*member, source, target, minimum, maximum)
		if err != nil {
			return game.Vec3{}, false, fmt.Errorf("behaviorRetreat: %w", err)
		}
		if isFound {
			return destination, true, nil
		}
		destination, isProjected, projectErr := zoneaction.NPCProjectPosition(member.zone.Navigation(),
			source.Plan.Position.Add(target.Sub(source.Plan.Position).Scale(0.25)),
			source.NavigationRadius(), source.Navigation)
		if projectErr != nil {
			return game.Vec3{}, false, fmt.Errorf("behaviorFallbackProject: %w", projectErr)
		}
		return destination, isProjected, nil
	}
	if behavior == "nbehavior_strafeoridle" {
		delta := target.Sub(source.Plan.Position)
		length := delta.Length()
		if length == 0 {
			return game.Vec3{}, false, nil
		}
		distance := float32(8)
		side := float32(1)
		if random.Float64() <= 0.5 {
			side = -1
		}
		desired := source.Plan.Position.Add(game.Vec3{
			X: -delta.Y / length * distance * side,
			Y: delta.X / length * distance * side, Z: delta.Z / length * distance,
		})
		destination, isProjected, projectErr := zoneaction.NPCProjectPosition(member.zone.Navigation(), desired,
			source.NavigationRadius(), source.Navigation)
		if projectErr != nil {
			return game.Vec3{}, false, fmt.Errorf("behaviorStrafeProject: %w", projectErr)
		}
		return destination, isProjected, nil
	}
	radius, attempts := float32(5), 1
	if behavior == "nbehavior_stroll" {
		radius, attempts = 7, 4
	}
	for range attempts {
		// The imported behavior owns the radius and attempt count. These draws
		// use the shared radial placement policy; native query internals remain
		// separate from the authored Lua behavior and navigation projection.
		angle := random.Float64() * 2 * math.Pi
		distance := float32(random.Float64()) * radius
		desired := source.Plan.Position.Add(game.Vec3{
			X: float32(math.Cos(angle)) * distance, Y: float32(math.Sin(angle)) * distance,
		})
		destination, isFound, err := zoneaction.NPCProjectPosition(member.zone.Navigation(), desired,
			source.NavigationRadius(), source.Navigation)
		if err != nil {
			return game.Vec3{}, false, fmt.Errorf("behaviorIdleProject: %w", err)
		}
		if !isFound {
			continue
		}
		if behavior == "nbehavior_stroll" {
			destination, isFound, err = zoneaction.NPCDirectMovementDestination(member.zone.Navigation(),
				source.Plan.Position, destination, source.NavigationRadius(), source.Navigation)
			if err != nil {
				return game.Vec3{}, false, fmt.Errorf("behaviorStrollPath: %w", err)
			}
			if !isFound || zonegeometry.Distance(source.Plan.Position, destination) <= radius*float32(0.8) {
				continue
			}
		}
		return destination, true, nil
	}
	return game.Vec3{}, false, nil
}

func (e *campaignNPCBehaviorRun) strafeIdleLocked(member *gameplayPeerSession,
	source zonenpc.Snapshot, targetObjectID uint32, targetPosition game.Vec3,
) ([][]byte, error) {
	packets, err := npcraknet.StopAtPose(source.Plan.ObjectID, source.Plan.Position, source.Facing)
	if err != nil {
		return nil, fmt.Errorf("strafeIdleStop: %w", err)
	}
	e.wasMoving = true
	turns, err := e.faceMovementTargetLocked(member, source, targetObjectID, targetPosition)
	if err != nil {
		return nil, fmt.Errorf("strafeIdleFace: %w", err)
	}
	animationPacket, err := raknet.MarshalApplication(raknet.SetAnimationStateMessage{
		ObjectID: source.Plan.ObjectID, State: util.HashID("wander_flavor"), Timestamp: e.timestamp, Scale: 1,
	})
	if err != nil {
		return nil, fmt.Errorf("strafeIdleAnimation: %w", err)
	}
	packets = append(packets, turns...)
	return append(packets, animationPacket), nil
}

func (e *campaignNPCBehaviorRun) setMovementBuffLocked(member *gameplayPeerSession,
	source zonenpc.Snapshot, movementBuff float32, now time.Time,
) ([][]byte, zonenpc.Snapshot, error) {
	if e.movementBuff == movementBuff {
		return nil, source, nil
	}
	updated, isFound, err := member.zone.NPCs().SetSupportIdleMovementBuff(source.Plan.ObjectID, movementBuff)
	if err != nil {
		return nil, source, fmt.Errorf("behaviorBuffStore: %w", err)
	}
	if !isFound {
		return nil, source, errors.New("behavior movement actor unavailable")
	}
	e.movementBuff = movementBuff
	packet, err := npcSupportMovementAttribute(source.Plan.ObjectID,
		(1+zonenpc.EffectiveMovementSpeedBuff(updated.Plan))*member.zone.NPCs().SlowMovementScale(source.Plan.ObjectID, now)-1)
	if err != nil {
		return nil, updated, fmt.Errorf("behaviorBuffMarshal: %w", err)
	}
	return [][]byte{packet}, updated, nil
}

func (e *campaignNPCBehaviorRun) faceMovementTargetLocked(member *gameplayPeerSession,
	source zonenpc.Snapshot, targetObjectID uint32, targetPosition game.Vec3,
) ([][]byte, error) {
	plan := zonenpc.AttackPlan{SourceObjectID: source.Plan.ObjectID,
		TargetObjectID: targetObjectID, ActionGeneration: source.ActionGeneration,
		SourcePosition: source.Plan.Position, TargetPosition: targetPosition}
	if !member.zone.NPCs().CommitFacing(plan) {
		return nil, nil
	}
	packets, err := npcraknet.FaceTarget(plan, false)
	if err != nil {
		return nil, fmt.Errorf("behaviorFaceMarshal: %w", err)
	}
	return packets, nil
}
