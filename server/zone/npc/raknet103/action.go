package raknet103

import (
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func Blink(
	plan zonenpc.AttackPlan, destination game.Vec3, timestamp uint64,
) ([][]byte, error) {
	if plan.SourceObjectID == 0 || plan.Profile.TeleportAnimationName == "" ||
		!isFiniteVec3(destination) {
		return nil, errors.New("npc blink invalid")
	}
	position := raknet.Vector3{
		X: destination.X, Y: destination.Y, Z: destination.Z,
	}
	facing := plan.TargetPosition.Sub(destination)
	yaw := math.Atan2(-float64(facing.X), float64(facing.Y))
	messages := []raknet.ApplicationMessage{
		raknet.ObjectTeleportMessage{
			ObjectID: plan.SourceObjectID, Position: position,
			Orientation: raknet.Quaternion{
				Z: float32(math.Sin(yaw / 2)), W: float32(math.Cos(yaw / 2)),
			},
		},
		raknet.ObjectUpdateMessage{
			ObjectID: plan.SourceObjectID, PositionX: destination.X,
			PositionY: destination.Y, PositionZ: destination.Z,
			IsVisible: true,
		},
		raknet.ObjectPlayerMoveMessage{
			ObjectID: plan.SourceObjectID, GoalFlags: 0x20,
			GoalPosition: position,
		},
		raknet.SetAnimationStateMessage{
			ObjectID:  plan.SourceObjectID,
			State:     util.HashID(plan.Profile.TeleportAnimationName),
			Timestamp: timestamp, Scale: 1,
		},
	}
	return marshalMessages(messages, "blink")
}

func RandomTeleport(
	plan zonenpc.AttackPlan, destination game.Vec3, timestamp uint64,
) ([][]byte, error) {
	if plan.SourceObjectID == 0 || plan.TargetObjectID == 0 ||
		plan.Profile.TeleportReactionName == "" ||
		plan.Profile.TeleportEffectName == "" || !isFiniteVec3(destination) {
		return nil, errors.New("npc random teleport invalid")
	}
	position := vector(destination)
	return marshalMessages([]raknet.ApplicationMessage{
		raknet.ObjectPlayerMoveMessage{
			ObjectID: plan.TargetObjectID, GoalFlags: 0x20, GoalPosition: position,
		},
		raknet.ObjectUpdateMessage{
			ObjectID: plan.TargetObjectID, PositionX: destination.X,
			PositionY: destination.Y, PositionZ: destination.Z,
			IsVisible: true,
		},
		raknet.SetAnimationStateMessage{
			ObjectID:  plan.TargetObjectID,
			State:     util.HashID(plan.Profile.TeleportReactionName),
			Timestamp: timestamp, Scale: 1,
		},
		raknet.ObjectEffectMessage{
			Asset:    util.HashID(plan.Profile.TeleportEffectName),
			ObjectID: plan.TargetObjectID, AttackerID: plan.SourceObjectID,
		},
	}, "randomTeleport")
}

func ForcedMovement(
	plan zonenpc.AttackPlan, destination game.Vec3, timestamp uint64,
) ([][]byte, error) {
	profile := plan.Profile
	if plan.SourceObjectID == 0 || plan.TargetObjectID == 0 ||
		profile.ForcedMovementSpeed <= 0 || !isFiniteVec3(destination) {
		return nil, errors.New("npc forced movement invalid")
	}
	facing := plan.SourcePosition.Sub(plan.TargetPosition)
	yaw := math.Atan2(-float64(facing.X), float64(facing.Y))
	messages := []raknet.ApplicationMessage{
		raknet.ObjectPlayerMoveMessage{
			ObjectID: plan.TargetObjectID, GoalFlags: 0x20, GoalPosition: vector(destination),
		},
		// A reflected pose does not reset the locally controlled physics mover.
		// Match the authoritative teleport used by the server's motion state.
		raknet.ObjectTeleportMessage{
			ObjectID: plan.TargetObjectID, Position: vector(destination),
			Orientation: raknet.Quaternion{
				Z: float32(math.Sin(yaw / 2)), W: float32(math.Cos(yaw / 2)),
			},
		},
	}
	if profile.ForcedMovementReactionName != "" {
		messages = append(messages, raknet.SetAnimationStateMessage{
			ObjectID:  plan.TargetObjectID,
			State:     util.HashID(profile.ForcedMovementReactionName),
			Timestamp: timestamp, Scale: 1,
		})
	}
	if profile.ForcedMovementEffectName != "" {
		messages = append(messages, raknet.ObjectEffectMessage{
			Asset:    util.HashID(profile.ForcedMovementEffectName),
			ObjectID: plan.TargetObjectID, AttackerID: plan.SourceObjectID,
		})
	}
	return marshalMessages(messages, "forcedMovement")
}

func ForcedMovementEffect(
	objectID uint32, slot uint8, effectName string, isRemovalRequested bool,
) ([]byte, error) {
	if objectID == 0 || (!isRemovalRequested && effectName == "") {
		return nil, errors.New("npc forced movement effect invalid")
	}
	message := raknet.AttachedEffectMessage{
		Slot: slot + 1, ObjectID: objectID,
		IsRemovalRequested: isRemovalRequested,
		IsHardStop:         isRemovalRequested,
	}
	if !isRemovalRequested {
		message.Asset = util.HashID(effectName)
		message.IsForceAttached = true
	}
	packet, err := raknet.MarshalApplication(message)
	if err != nil {
		return nil, fmt.Errorf("forcedMovementEffectMarshal: %w", err)
	}
	return packet, nil
}

func Pursuit(plan zonenpc.FirstActionPlan) ([][]byte, error) {
	if plan.ObjectID == 0 || plan.TargetObjectID == 0 || !plan.IsPursuitNeeded ||
		plan.Profile.Range <= 0 || math.IsNaN(float64(plan.Profile.Range)) ||
		math.IsInf(float64(plan.Profile.Range), 0) ||
		!isFiniteVec3(plan.SourcePosition) || !isFiniteVec3(plan.TargetPosition) {
		return nil, errors.New("npc pursuit invalid")
	}
	source := vector(plan.SourcePosition)
	target := vector(plan.TargetPosition)
	if plan.Profile.AbilityName == "Flee" {
		return fleeMovement(plan.ObjectID, source, target)
	}
	if plan.Profile.AbilityName == "StealthAttack" {
		// Stealth travel has a fixed destination, not a live hero-follow goal.
		return marshalMessages([]raknet.ApplicationMessage{
			raknet.ObjectPlayerMoveMessage{
				ObjectID: plan.ObjectID, GoalFlags: 0x01, GoalPosition: target,
				Facing: direction(source, target),
			},
			raknet.LocomotionUnreliableMessage{
				ObjectID: plan.ObjectID, GoalPosition: target,
			},
		}, "stealthTravel")
	}
	if plan.Profile.Family == zonenpc.ActionProjectile {
		goal := pursuitGoal(source, target, plan.Profile.Range)
		return marshalMessages([]raknet.ApplicationMessage{
			raknet.ObjectPlayerMoveMessage{
				ObjectID: plan.ObjectID, GoalFlags: 0x01, GoalPosition: goal,
			},
			raknet.LocomotionUnreliableMessage{
				ObjectID: plan.ObjectID, GoalPosition: goal,
			},
		}, "projectilePursuit")
	}
	return marshalMessages([]raknet.ApplicationMessage{
		raknet.ObjectPlayerMoveMessage{
			ObjectID: plan.ObjectID, GoalFlags: 0x41, GoalPosition: target,
			Facing:              direction(source, target),
			AllowedStopDistance: plan.Profile.Range,
			DesiredStopDistance: plan.Profile.Range,
			TargetPosition:      target, TargetObjectID: plan.TargetObjectID,
		},
		raknet.LocomotionUnreliableMessage{
			ObjectID: plan.ObjectID, GoalPosition: target,
		},
	}, "pursuit")
}

// BurrowTravel preserves the authored underground animation while the
// Tunneler moves to the point where its poison nova will emerge.
func BurrowTravel(plan zonenpc.AttackPlan, timestamp uint64) ([][]byte, error) {
	if plan.SourceObjectID == 0 || !isFiniteVec3(plan.SourcePosition) ||
		!isFiniteVec3(plan.TargetPosition) || plan.Profile.AnimationName == "" {
		return nil, errors.New("npc burrow travel invalid")
	}
	source := vector(plan.SourcePosition)
	target := vector(plan.TargetPosition)
	return marshalMessages([]raknet.ApplicationMessage{
		raknet.ObjectPlayerMoveMessage{
			ObjectID: plan.SourceObjectID, GoalFlags: 0x01,
			GoalPosition: target, Facing: direction(source, target),
		},
		raknet.LocomotionUnreliableMessage{
			ObjectID: plan.SourceObjectID, GoalPosition: target,
		},
		raknet.SetAnimationStateMessage{
			ObjectID: plan.SourceObjectID, State: util.HashID(plan.Profile.AnimationName),
			Timestamp: timestamp, Scale: 1,
		},
	}, "burrowTravel")
}

// BurrowArrival reconciles the moving client root with the authoritative
// destination before playing the emerge attack.
func BurrowArrival(
	objectID uint32, position game.Vec3, facing game.Vec3, timestamp uint64,
) ([][]byte, error) {
	if objectID == 0 || !isFiniteVec3(position) || !isFiniteVec3(facing) ||
		facing.Length() <= 0 {
		return nil, errors.New("npc burrow arrival invalid")
	}
	yaw := math.Atan2(-float64(facing.X), float64(facing.Y))
	return marshalMessages([]raknet.ApplicationMessage{
		raknet.ObjectPlayerMoveMessage{
			ObjectID: objectID, GoalFlags: 0x20, GoalPosition: vector(position),
		},
		raknet.ObjectTeleportMessage{
			ObjectID: objectID, Position: vector(position),
			Orientation: raknet.Quaternion{
				Z: float32(math.Sin(yaw / 2)), W: float32(math.Cos(yaw / 2)),
			},
		},
		raknet.SetAnimationStateMessage{
			ObjectID: objectID, State: util.HashID("burrow_attack1"),
			Timestamp: timestamp, Scale: 1,
		},
	}, "burrowArrival")
}

func FleeRedirect(
	objectID uint32, sourcePosition game.Vec3, targetPosition game.Vec3,
) ([][]byte, error) {
	if objectID == 0 || !isFiniteVec3(sourcePosition) ||
		!isFiniteVec3(targetPosition) {
		return nil, errors.New("npc flee redirect invalid")
	}
	return fleeMovement(
		objectID, vector(sourcePosition), vector(targetPosition),
	)
}

func fleeMovement(
	objectID uint32, source raknet.Vector3, target raknet.Vector3,
) ([][]byte, error) {
	return marshalMessages([]raknet.ApplicationMessage{
		raknet.ObjectPlayerMoveMessage{
			ObjectID: objectID, GoalFlags: 0x01, GoalPosition: target,
			Facing: direction(source, target),
		},
		raknet.LocomotionUnreliableMessage{
			ObjectID: objectID, GoalPosition: target,
		},
	}, "flee")
}

func PursuitRedirect(
	objectID uint32, sourcePosition game.Vec3, targetObjectID uint32,
	targetPosition game.Vec3, stopDistance float32,
) ([][]byte, error) {
	return pursuitRedirect(
		objectID, sourcePosition, targetObjectID, targetPosition, stopDistance, false,
	)
}

func BoundedStrafe(
	objectID uint32, sourcePosition game.Vec3, destination game.Vec3,
	targetObjectID uint32, targetPosition game.Vec3, stopDistance float32,
) ([][]byte, error) {
	if objectID == 0 || targetObjectID == 0 || stopDistance <= 0 ||
		math.IsNaN(float64(stopDistance)) || math.IsInf(float64(stopDistance), 0) ||
		!isFiniteVec3(sourcePosition) || !isFiniteVec3(destination) ||
		!isFiniteVec3(targetPosition) {
		return nil, errors.New("npc bounded strafe invalid")
	}
	source := vector(sourcePosition)
	goal := vector(destination)
	target := vector(targetPosition)
	return marshalMessages([]raknet.ApplicationMessage{
		raknet.ObjectPositionUpdateMessage{
			ObjectID: objectID, PositionX: source.X,
			PositionY: source.Y, PositionZ: source.Z,
		},
		raknet.ObjectPlayerMoveMessage{
			ObjectID: objectID, GoalFlags: 0x41,
			GoalPosition: goal, Facing: direction(source, target),
			AllowedStopDistance: stopDistance,
			DesiredStopDistance: stopDistance,
			TargetPosition:      target, TargetObjectID: targetObjectID,
		},
		raknet.LocomotionUnreliableMessage{
			ObjectID: objectID, GoalPosition: goal,
		},
	}, "boundedStrafe")
}

func ProjectilePursuitRedirect(
	objectID uint32, sourcePosition game.Vec3, targetObjectID uint32,
	targetPosition game.Vec3, stopDistance float32,
) ([][]byte, error) {
	return pursuitRedirect(
		objectID, sourcePosition, targetObjectID, targetPosition, stopDistance, true,
	)
}

func pursuitRedirect(
	objectID uint32, sourcePosition game.Vec3, targetObjectID uint32,
	targetPosition game.Vec3, stopDistance float32, isProjectile bool,
) ([][]byte, error) {
	if objectID == 0 || targetObjectID == 0 || stopDistance <= 0 ||
		math.IsNaN(float64(stopDistance)) ||
		math.IsInf(float64(stopDistance), 0) ||
		!isFiniteVec3(sourcePosition) || !isFiniteVec3(targetPosition) {
		return nil, errors.New("npc pursuit redirect invalid")
	}
	source := vector(sourcePosition)
	target := vector(targetPosition)
	if isProjectile {
		goal := pursuitGoal(source, target, stopDistance)
		return marshalMessages([]raknet.ApplicationMessage{
			raknet.ObjectPlayerMoveMessage{
				ObjectID: objectID, GoalFlags: 0x01, GoalPosition: goal,
			},
			raknet.LocomotionUnreliableMessage{
				ObjectID: objectID, GoalPosition: goal,
			},
		}, "projectilePursuitRedirect")
	}
	return marshalMessages([]raknet.ApplicationMessage{
		raknet.ObjectPlayerMoveMessage{
			ObjectID: objectID, GoalFlags: 0x41, GoalPosition: target,
			Facing:              direction(source, target),
			AllowedStopDistance: stopDistance,
			DesiredStopDistance: stopDistance,
			TargetPosition:      target, TargetObjectID: targetObjectID,
		},
		raknet.LocomotionUnreliableMessage{
			ObjectID: objectID, GoalPosition: target,
		},
	}, "pursuitRedirect")
}

func pursuitGoal(
	source raknet.Vector3, target raknet.Vector3, stopDistance float32,
) raknet.Vector3 {
	deltaX := source.X - target.X
	deltaY := source.Y - target.Y
	deltaZ := source.Z - target.Z
	distance := float32(math.Sqrt(float64(
		deltaX*deltaX + deltaY*deltaY + deltaZ*deltaZ,
	)))
	if distance <= stopDistance || distance == 0 {
		return source
	}
	ratio := stopDistance / distance
	return raknet.Vector3{
		X: target.X + deltaX*ratio,
		Y: target.Y + deltaY*ratio,
		Z: target.Z + deltaZ*ratio,
	}
}

func PursuitProgress(
	progress []zonenpc.PursuitProgress, targetObjectID uint32,
) ([][]byte, error) {
	if targetObjectID == 0 {
		return nil, errors.New("npc pursuit progress target invalid")
	}
	packets := make([][]byte, 0, len(progress)*2)
	for index, step := range progress {
		if step.ObjectID == 0 || step.StopDistance <= 0 ||
			math.IsNaN(float64(step.StopDistance)) ||
			math.IsInf(float64(step.StopDistance), 0) ||
			!isFiniteVec3(step.Position) ||
			!isFiniteVec3(step.TargetPosition) {
			return nil, fmt.Errorf("pursuitProgress[%d]: invalid", index)
		}
		source := vector(step.Position)
		target := vector(step.TargetPosition)
		stepPackets, err := marshalMessages([]raknet.ApplicationMessage{
			raknet.ObjectPositionUpdateMessage{
				ObjectID: step.ObjectID, PositionX: source.X,
				PositionY: source.Y, PositionZ: source.Z,
			},
			raknet.ObjectPlayerMoveMessage{
				ObjectID: step.ObjectID, GoalFlags: 0x41,
				GoalPosition: target, Facing: direction(source, target),
				AllowedStopDistance: step.StopDistance,
				DesiredStopDistance: step.StopDistance,
				TargetPosition:      target,
				TargetObjectID:      targetObjectID,
			},
			raknet.LocomotionUnreliableMessage{
				ObjectID: step.ObjectID, GoalPosition: target,
			},
		}, "pursuitProgress")
		if err != nil {
			return nil, fmt.Errorf("pursuitProgress[%d]: %w", index, err)
		}
		packets = append(packets, stepPackets...)
	}
	return packets, nil
}

func FirstAggro(plan zonenpc.FirstActionPlan, timestamp uint64) ([][]byte, error) {
	if plan.ObjectID == 0 || plan.TargetObjectID == 0 ||
		!isFiniteVec3(plan.SourcePosition) ||
		!plan.Profile.IsFirstAggroDurationKnown {
		return nil, errors.New("npc first aggro invalid")
	}
	source := vector(plan.SourcePosition)
	messages := make([]raknet.ApplicationMessage, 0, 6)
	if plan.Profile.FirstAggroCinematicDuration > 0 {
		messages = append(messages, raknet.NewCinematicMessage(
			plan.Profile.FirstAggroCinematicDuration, source,
			plan.Profile.FirstAggroCinematicRadius,
		))
	}
	isDelayedReveal := plan.Profile.FirstAggroRevealDelay > 0
	targetObjectID := plan.TargetObjectID
	attackerCount := uint32(1)
	if isDelayedReveal {
		targetObjectID = 0
		attackerCount = 0
	}
	messages = append(messages,
		raknet.ObjectUpdateMessage{
			ObjectID: plan.ObjectID, PositionX: source.X,
			PositionY: source.Y, PositionZ: source.Z,
			IsVisible: plan.Profile.FirstAggroRevealDelay <= 0,
		},
		raknet.AgentBlackboardUpdateMessage{
			ObjectID: plan.ObjectID, TargetID: targetObjectID,
			IsInCombat: !isDelayedReveal, IsTargetable: !isDelayedReveal,
			AttackerCount: attackerCount,
		},
		attackTurn(plan.FirstAggroFacingPlan()),
	)
	if plan.Profile.FirstAggroRevealDelay <= 0 && plan.Profile.FirstAggroAnimationName != "" {
		messages = append(messages, raknet.SetAnimationStateMessage{
			ObjectID:  plan.ObjectID,
			State:     util.HashID(plan.Profile.FirstAggroAnimationName),
			Timestamp: timestamp, Scale: 1,
		})
	}
	if plan.Profile.FirstAggroEffectName != "" &&
		plan.Profile.FirstAggroEffectDelay <= 0 {
		messages = append(messages, raknet.PositionedEffectMessage{
			Asset:    util.HashID(plan.Profile.FirstAggroEffectName),
			Position: source,
		})
	}
	return marshalMessages(messages, "firstAggro")
}

func FirstAggroEffect(plan zonenpc.FirstActionPlan) ([][]byte, error) {
	if plan.ObjectID == 0 || !isFiniteVec3(plan.SourcePosition) ||
		plan.Profile.FirstAggroEffectName == "" ||
		plan.Profile.FirstAggroEffectDelay <= 0 {
		return nil, errors.New("npc first aggro effect invalid")
	}
	source := vector(plan.SourcePosition)
	return marshalMessages([]raknet.ApplicationMessage{
		raknet.PositionedEffectMessage{
			Asset: util.HashID(plan.Profile.FirstAggroEffectName), Position: source,
		},
	}, "firstAggroEffect")
}

func FirstAggroReveal(plan zonenpc.FirstActionPlan, timestamp uint64) ([][]byte, error) {
	if plan.ObjectID == 0 || !isFiniteVec3(plan.SourcePosition) ||
		plan.Profile.FirstAggroRevealDelay <= 0 ||
		plan.Profile.FirstAggroAnimationName == "" {
		return nil, errors.New("npc first aggro reveal invalid")
	}
	source := vector(plan.SourcePosition)
	return marshalMessages([]raknet.ApplicationMessage{
		raknet.ObjectUpdateMessage{
			ObjectID: plan.ObjectID, PositionX: source.X,
			PositionY: source.Y, PositionZ: source.Z, IsVisible: true,
		},
		raknet.ObjectUpdateMessage{
			ObjectID: plan.ObjectID, PositionX: source.X,
			PositionY: source.Y, PositionZ: source.Z, IsVisible: true,
		},
		raknet.SetAnimationStateMessage{
			ObjectID:  plan.ObjectID,
			State:     util.HashID(plan.Profile.FirstAggroAnimationName),
			Timestamp: timestamp, Scale: 1,
		},
		raknet.AgentBlackboardUpdateMessage{
			ObjectID: plan.ObjectID, TargetID: 0,
			IsInCombat: false, IsTargetable: false, AttackerCount: 0,
		},
	}, "firstAggroReveal")
}

func FirstAggroActivate(plan zonenpc.FirstActionPlan) ([][]byte, error) {
	if plan.ObjectID == 0 || plan.TargetObjectID == 0 ||
		!isFiniteVec3(plan.SourcePosition) ||
		plan.Profile.FirstAggroRevealDelay <= 0 {
		return nil, errors.New("npc first aggro activation invalid")
	}
	source := vector(plan.SourcePosition)
	return marshalMessages([]raknet.ApplicationMessage{
		raknet.ObjectPositionUpdateMessage{
			ObjectID: plan.ObjectID, PositionX: source.X,
			PositionY: source.Y, PositionZ: source.Z,
		},
		raknet.ObjectPlayerMoveMessage{
			ObjectID: plan.ObjectID, GoalFlags: 0x20, GoalPosition: source,
		},
		raknet.LocomotionUnreliableMessage{
			ObjectID: plan.ObjectID, GoalPosition: source,
		},
		raknet.AgentBlackboardUpdateMessage{
			ObjectID: plan.ObjectID, TargetID: plan.TargetObjectID,
			IsInCombat: true, IsTargetable: true, AttackerCount: 1,
		},
	}, "firstAggroActivate")
}

func AttackStart(plan zonenpc.AttackPlan, timestamp uint64) ([][]byte, error) {
	if plan.SourceObjectID == 0 || plan.TargetObjectID == 0 ||
		plan.Profile.AnimationName == "" {
		return nil, errors.New("npc attack start invalid")
	}
	source := vector(plan.SourcePosition)
	return marshalMessages([]raknet.ApplicationMessage{
		raknet.ObjectPositionUpdateMessage{
			ObjectID: plan.SourceObjectID, PositionX: source.X,
			PositionY: source.Y, PositionZ: source.Z,
		},
		attackTurn(plan),
		raknet.SetAnimationStateMessage{
			ObjectID: plan.SourceObjectID, State: util.HashID(plan.Profile.AnimationName),
			Timestamp: timestamp, Scale: 1,
		},
	}, "attackStart")
}

func MovementStop(objectID uint32, position game.Vec3) ([][]byte, error) {
	if objectID == 0 || !isFiniteVec3(position) {
		return nil, errors.New("npc movement stop invalid")
	}
	source := vector(position)
	return marshalMessages([]raknet.ApplicationMessage{
		raknet.ObjectUpdateMessage{
			ObjectID: objectID, PositionX: position.X,
			PositionY: position.Y, PositionZ: position.Z,
			IsVisible: true,
		},
		raknet.ObjectPlayerMoveMessage{
			ObjectID: objectID, GoalFlags: 0x20, GoalPosition: source,
		},
	}, "movementStop")
}

func MovementGoalUpdate(objectID uint32, goalPosition game.Vec3) ([]byte, error) {
	if objectID == 0 || !isFiniteVec3(goalPosition) {
		return nil, errors.New("npc movement goal update invalid")
	}
	goal := vector(goalPosition)
	packet, err := raknet.MarshalApplication(raknet.LocomotionUnreliableMessage{
		ObjectID: objectID, GoalPosition: goal,
	})
	if err != nil {
		return nil, fmt.Errorf("movementGoalUpdateMarshal: %w", err)
	}
	return packet, nil
}

func ProjectileGoalUpdate(
	objectID uint32, sourcePosition game.Vec3, targetPosition game.Vec3,
	stopDistance float32,
) ([]byte, error) {
	if objectID == 0 || stopDistance <= 0 ||
		math.IsNaN(float64(stopDistance)) ||
		math.IsInf(float64(stopDistance), 0) ||
		!isFiniteVec3(sourcePosition) || !isFiniteVec3(targetPosition) {
		return nil, errors.New("npc projectile goal update invalid")
	}
	goal := pursuitGoal(
		vector(sourcePosition), vector(targetPosition), stopDistance,
	)
	packet, err := MovementGoalUpdate(
		objectID, game.Vec3{X: goal.X, Y: goal.Y, Z: goal.Z},
	)
	if err != nil {
		return nil, fmt.Errorf("projectileGoalUpdate: %w", err)
	}
	return packet, nil
}

func StationaryCastStart(
	plan zonenpc.AttackPlan, timestamp uint64,
) ([][]byte, error) {
	if plan.SourceObjectID == 0 || plan.Profile.AnimationName == "" ||
		!isFiniteVec3(plan.SourcePosition) {
		return nil, errors.New("npc stationary cast start invalid")
	}
	source := vector(plan.SourcePosition)
	return marshalMessages([]raknet.ApplicationMessage{
		raknet.ObjectPositionUpdateMessage{
			ObjectID: plan.SourceObjectID, PositionX: source.X,
			PositionY: source.Y, PositionZ: source.Z,
		},
		raknet.ObjectPlayerMoveMessage{
			ObjectID: plan.SourceObjectID, GoalFlags: 0x20,
			GoalPosition: source,
		},
		raknet.LocomotionUnreliableMessage{
			ObjectID: plan.SourceObjectID, GoalPosition: source,
		},
		raknet.SetAnimationStateMessage{
			ObjectID:  plan.SourceObjectID,
			State:     util.HashID(plan.Profile.AnimationName),
			Timestamp: timestamp, Scale: 1,
		},
	}, "stationaryCastStart")
}

func CancelAction(
	objectID uint32, position game.Vec3, timestamp uint64,
) ([][]byte, error) {
	if objectID == 0 || !isFiniteVec3(position) {
		return nil, errors.New("npc action cancellation invalid")
	}
	source := vector(position)
	return marshalMessages([]raknet.ApplicationMessage{
		raknet.ObjectUpdateMessage{
			ObjectID: objectID, PositionX: position.X,
			PositionY: position.Y, PositionZ: position.Z,
			IsVisible: true,
		},
		raknet.ObjectPlayerMoveMessage{
			ObjectID: objectID, GoalFlags: 0x20, GoalPosition: source,
		},
		raknet.SetAnimationStateMessage{
			ObjectID: objectID, Timestamp: timestamp, Scale: 1,
		},
	}, "cancelAction")
}

func CancelActions(
	npcs []zonenpc.Snapshot, timestamp uint64,
) ([][]byte, error) {
	packets := make([][]byte, 0, len(npcs)*3)
	for index, npc := range npcs {
		if !npc.IsActionStarted {
			continue
		}
		currentPackets, err := CancelAction(
			npc.Plan.ObjectID, npc.Plan.Position, timestamp,
		)
		if err != nil {
			return nil, fmt.Errorf("cancelActions[%d]: %w", index, err)
		}
		packets = append(packets, currentPackets...)
	}
	return packets, nil
}

func ChannelImpact(plan zonenpc.AttackPlan) ([]byte, error) {
	if plan.SourceObjectID == 0 || plan.TargetObjectID == 0 ||
		plan.Profile.TargetEffectName == "" {
		return nil, errors.New("npc channel impact invalid")
	}
	packet, err := raknet.MarshalApplication(raknet.ObjectEffectMessage{
		Asset:    util.HashID(plan.Profile.TargetEffectName),
		ObjectID: plan.TargetObjectID, AttackerID: plan.SourceObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("channelImpactMarshal: %w", err)
	}
	return packet, nil
}

func AttackImpact(plan zonenpc.AttackPlan) ([]byte, error) {
	if plan.SourceObjectID == 0 || plan.TargetObjectID == 0 ||
		plan.Profile.ImpactEffectName == "" {
		return nil, errors.New("npc attack impact invalid")
	}
	packet, err := raknet.MarshalApplication(raknet.ObjectEffectMessage{
		Asset:    util.HashID(plan.Profile.ImpactEffectName),
		ObjectID: plan.TargetObjectID, AttackerID: plan.SourceObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("attackImpactMarshal: %w", err)
	}
	return packet, nil
}

func BeamEffect(plan zonenpc.AttackPlan) ([]byte, error) {
	if plan.SourceObjectID == 0 || plan.TargetObjectID == 0 ||
		plan.Profile.TrailEffectName == "" {
		return nil, errors.New("npc beam effect invalid")
	}
	packet, err := raknet.MarshalApplication(raknet.ChainedEffectMessage{
		Asset:    util.HashID(plan.Profile.TrailEffectName),
		ObjectID: plan.SourceObjectID, SecondaryObjectID: plan.TargetObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("beamEffectMarshal: %w", err)
	}
	return packet, nil
}

func CryosBossChainStart(plan zonenpc.AttackPlan) ([][]byte, error) {
	if plan.SourceObjectID == 0 || plan.TargetObjectID == 0 ||
		plan.Profile.TrailEffectName == "" || plan.Profile.TargetEffectName == "" {
		return nil, errors.New("npc Cryos boss chain start invalid")
	}
	return marshalMessages([]raknet.ApplicationMessage{
		raknet.AttachedEffectMessage{
			Slot: 1, IsForceAttached: true,
			Asset:    util.HashID(plan.Profile.TrailEffectName),
			ObjectID: plan.SourceObjectID, SecondaryObjectID: plan.TargetObjectID,
		},
		raknet.AttachedEffectMessage{
			Slot: 1, IsForceAttached: true,
			Asset:    util.HashID(plan.Profile.TargetEffectName),
			ObjectID: plan.TargetObjectID, SecondaryObjectID: plan.SourceObjectID,
		},
	}, "cryosBossChainStart")
}

func CryosBossChainEnd(plan zonenpc.AttackPlan) ([][]byte, error) {
	if plan.SourceObjectID == 0 || plan.TargetObjectID == 0 {
		return nil, errors.New("npc Cryos boss chain end invalid")
	}
	return marshalMessages([]raknet.ApplicationMessage{
		raknet.AttachedEffectMessage{
			Slot: 1, ObjectID: plan.SourceObjectID,
			IsRemovalRequested: true, IsHardStop: true,
		},
		raknet.AttachedEffectMessage{
			Slot: 1, ObjectID: plan.TargetObjectID,
			IsRemovalRequested: true, IsHardStop: true,
		},
	}, "cryosBossChainEnd")
}

const twinLaserEndpointNounName = "SweepingBeamMarker.Noun"

func TwinLaserStart(
	plan zonenpc.AttackPlan, endpointObjectID uint32, timestamp uint64,
) ([][]byte, error) {
	packets, err := TwinLaserPairStart(plan, [2]uint32{endpointObjectID, endpointObjectID},
		[2]game.Vec3{plan.TargetPosition, plan.TargetPosition}, timestamp)
	if err != nil {
		return nil, fmt.Errorf("twinStart: %w", err)
	}
	return packets, nil
}

func TwinLaserPairStart(plan zonenpc.AttackPlan, endpointIDs [2]uint32,
	positions [2]game.Vec3, timestamp uint64,
) ([][]byte, error) {
	endpointObjectID := endpointIDs[0]
	if !isFiniteVec3(positions[0]) || !isFiniteVec3(positions[1]) {
		return nil, errors.New("npc twin laser endpoints invalid")
	}
	profile := plan.Profile
	if plan.SourceObjectID == 0 || plan.TargetObjectID == 0 ||
		endpointObjectID == 0 || endpointIDs[1] == 0 ||
		profile.LoopAnimationName == "" || profile.TrailEffectName == "" ||
		profile.SecondaryTrailEffectName == "" || profile.SourceEffectName == "" ||
		profile.SecondarySourceEffectName == "" || profile.GroundEffectName == "" ||
		!isFiniteVec3(plan.TargetPosition) {
		return nil, errors.New("npc twin laser start invalid")
	}
	createPacket, err := raknet.MarshalApplication(raknet.ObjectCreateMessage{
		ObjectID: endpointObjectID, Noun: util.HashID(twinLaserEndpointNounName),
		PositionX: positions[0].X, PositionY: positions[0].Y,
		PositionZ: positions[0].Z, Scale: 1,
		OwnerID: plan.SourceObjectID, IsCollisionEnabled: false,
	})
	if err != nil {
		return nil, fmt.Errorf("twinLaserEndpointCreate: %w", err)
	}
	effectPackets, err := marshalMessages([]raknet.ApplicationMessage{
		raknet.SetAnimationStateMessage{
			ObjectID: plan.SourceObjectID,
			State:    util.HashID(profile.LoopAnimationName), Timestamp: timestamp, Scale: 1,
		},
		raknet.AttachedEffectMessage{
			Slot: 1, IsForceAttached: true,
			Asset:    util.HashID(profile.TrailEffectName),
			ObjectID: plan.SourceObjectID, SecondaryObjectID: endpointObjectID,
		},
		raknet.AttachedEffectMessage{
			Slot: 2, IsForceAttached: true,
			Asset:    util.HashID(profile.SecondaryTrailEffectName),
			ObjectID: plan.SourceObjectID, SecondaryObjectID: endpointIDs[1],
		},
		raknet.AttachedEffectMessage{
			Slot: 3, IsForceAttached: true,
			Asset: util.HashID(profile.SourceEffectName), ObjectID: plan.SourceObjectID,
			SecondaryObjectID: endpointObjectID,
		},
		raknet.AttachedEffectMessage{
			Slot: 4, IsForceAttached: true,
			Asset:    util.HashID(profile.SecondarySourceEffectName),
			ObjectID: plan.SourceObjectID, SecondaryObjectID: endpointIDs[1],
		},
		raknet.AttachedEffectMessage{
			Slot: 1, IsForceAttached: true, Asset: util.HashID(profile.GroundEffectName),
			ObjectID: endpointObjectID,
		},
	}, "twinLaserStart")
	if err != nil {
		return nil, fmt.Errorf("twinLaserEffects: %w", err)
	}
	packets := [][]byte{createPacket}
	if endpointIDs[1] != endpointObjectID {
		secondPackets, secondErr := marshalMessages([]raknet.ApplicationMessage{
			raknet.ObjectCreateMessage{ObjectID: endpointIDs[1], Noun: util.HashID(twinLaserEndpointNounName),
				PositionX: positions[1].X, PositionY: positions[1].Y, PositionZ: positions[1].Z,
				Scale: 1, OwnerID: plan.SourceObjectID, IsCollisionEnabled: false},
			raknet.AttachedEffectMessage{Slot: 1, ObjectID: endpointIDs[1], IsForceAttached: true,
				Asset: util.HashID(profile.GroundEffectName)},
		}, "twinLaserSecond")
		if secondErr != nil {
			return nil, fmt.Errorf("twinLaserPair: %w", secondErr)
		}
		packets = append(packets, secondPackets...)
	}
	return append(packets, effectPackets...), nil
}

func TwinLaserEnd(
	plan zonenpc.AttackPlan, endpointObjectID uint32, timestamp uint64,
) ([][]byte, error) {
	return TwinLaserCleanup(plan, endpointObjectID, timestamp, true)
}

// TwinLaserCleanup always removes the cast-owned endpoint. Retired casts must
// not clear slots or overwrite the animation of a newer attack on the source.
func TwinLaserCleanup(
	plan zonenpc.AttackPlan, endpointObjectID uint32, timestamp uint64, isSourceCurrent bool,
) ([][]byte, error) {
	packets, err := twinLaserCleanup(plan, endpointObjectID, timestamp, isSourceCurrent, true)
	if err != nil {
		return nil, fmt.Errorf("twinCleanup: %w", err)
	}
	return packets, nil
}

// TwinLaserSweepCleanup preserves the end animation begun during the sweep.
func TwinLaserSweepCleanup(plan zonenpc.AttackPlan, endpointObjectID uint32,
	timestamp uint64, isSourceCurrent bool,
) ([][]byte, error) {
	packets, err := twinLaserCleanup(plan, endpointObjectID, timestamp, isSourceCurrent, false)
	if err != nil {
		return nil, fmt.Errorf("sweepCleanup: %w", err)
	}
	return packets, nil
}

func twinLaserCleanup(plan zonenpc.AttackPlan, endpointObjectID uint32,
	timestamp uint64, isSourceCurrent bool, isAnimationRequired bool,
) ([][]byte, error) {
	if plan.SourceObjectID == 0 || plan.TargetObjectID == 0 ||
		endpointObjectID == 0 ||
		plan.Profile.EndAnimationName == "" {
		return nil, errors.New("npc twin laser end invalid")
	}
	if !isSourceCurrent {
		return marshalMessages([]raknet.ApplicationMessage{
			raknet.AttachedEffectMessage{
				Slot: 1, ObjectID: endpointObjectID,
				IsRemovalRequested: true, IsHardStop: true,
			},
			raknet.ObjectDeleteMessage{ObjectID: []uint32{endpointObjectID}},
		}, "twinLaserRetired")
	}
	messages := []raknet.ApplicationMessage{
		raknet.AttachedEffectMessage{
			Slot: 1, ObjectID: plan.SourceObjectID,
			IsRemovalRequested: true, IsHardStop: true,
		},
		raknet.AttachedEffectMessage{
			Slot: 2, ObjectID: plan.SourceObjectID,
			IsRemovalRequested: true, IsHardStop: true,
		},
		raknet.AttachedEffectMessage{
			Slot: 3, ObjectID: plan.SourceObjectID,
			IsRemovalRequested: true, IsHardStop: true,
		},
		raknet.AttachedEffectMessage{
			Slot: 4, ObjectID: plan.SourceObjectID,
			IsRemovalRequested: true, IsHardStop: true,
		},
		raknet.AttachedEffectMessage{
			Slot: 1, ObjectID: endpointObjectID,
			IsRemovalRequested: true, IsHardStop: true,
		},
	}
	if isAnimationRequired {
		messages = append(messages, raknet.SetAnimationStateMessage{
			ObjectID: plan.SourceObjectID,
			State:    util.HashID(plan.Profile.EndAnimationName), Timestamp: timestamp, Scale: 1,
		})
	}
	messages = append(messages, raknet.ObjectDeleteMessage{ObjectID: []uint32{endpointObjectID}})
	return marshalMessages(messages, "twinLaserEnd")
}

func CopterLinkEffect(
	objectID uint32, secondaryObjectID uint32, slot uint8,
	effectName string, isRemovalRequested bool,
) ([]byte, error) {
	if objectID == 0 || (!isRemovalRequested &&
		(secondaryObjectID == 0 || effectName == "")) {
		return nil, errors.New("npc copter link effect invalid")
	}
	message := raknet.AttachedEffectMessage{
		Slot: slot + 1, ObjectID: objectID,
		SecondaryObjectID:  secondaryObjectID,
		IsRemovalRequested: isRemovalRequested,
		IsHardStop:         isRemovalRequested,
	}
	if !isRemovalRequested {
		message.Asset = util.HashID(effectName)
		message.IsForceAttached = true
	}
	packet, err := raknet.MarshalApplication(message)
	if err != nil {
		return nil, fmt.Errorf("copterLinkEffectMarshal: %w", err)
	}
	return packet, nil
}

func VoltroidEffect(
	objectID uint32, secondaryObjectID uint32, slot uint8,
	effectName string, isRemovalRequested bool,
) ([]byte, error) {
	if objectID == 0 || (!isRemovalRequested && effectName == "") {
		return nil, errors.New("npc voltroid effect invalid")
	}
	message := raknet.AttachedEffectMessage{
		Slot: slot + 1, ObjectID: objectID,
		SecondaryObjectID:  secondaryObjectID,
		IsRemovalRequested: isRemovalRequested,
		IsHardStop:         isRemovalRequested,
	}
	if !isRemovalRequested {
		message.Asset = util.HashID(effectName)
		message.IsForceAttached = true
	}
	packet, err := raknet.MarshalApplication(message)
	if err != nil {
		return nil, fmt.Errorf("voltroidEffectMarshal: %w", err)
	}
	return packet, nil
}

func AnimationState(
	objectID uint32, animationName string, timestamp uint64,
) ([]byte, error) {
	if objectID == 0 || animationName == "" {
		return nil, errors.New("npc animation state invalid")
	}
	packet, err := raknet.MarshalApplication(raknet.SetAnimationStateMessage{
		ObjectID: objectID, State: util.HashID(animationName),
		Timestamp: timestamp, Scale: 1,
	})
	if err != nil {
		return nil, fmt.Errorf("animationStateMarshal: %w", err)
	}
	return packet, nil
}

func ResetAnimation(objectID uint32, timestamp uint64) ([]byte, error) {
	if objectID == 0 {
		return nil, errors.New("npc animation reset invalid")
	}
	packet, err := raknet.MarshalApplication(raknet.SetAnimationStateMessage{
		ObjectID: objectID, Timestamp: timestamp, Scale: 1,
	})
	if err != nil {
		return nil, fmt.Errorf("animationResetMarshal: %w", err)
	}
	return packet, nil
}

func Position(objectID uint32, position game.Vec3) ([]byte, error) {
	if objectID == 0 || !isFiniteVec3(position) {
		return nil, errors.New("npc position invalid")
	}
	packet, err := raknet.MarshalApplication(raknet.ObjectPositionUpdateMessage{
		ObjectID: objectID, PositionX: position.X,
		PositionY: position.Y, PositionZ: position.Z,
	})
	if err != nil {
		return nil, fmt.Errorf("positionMarshal: %w", err)
	}
	return packet, nil
}

func StealthReveal(
	objectID uint32, position game.Vec3, effectName string,
) ([][]byte, error) {
	if objectID == 0 || effectName == "" || !isFiniteVec3(position) {
		return nil, errors.New("npc stealth reveal invalid")
	}
	return marshalMessages([]raknet.ApplicationMessage{
		raknet.ObjectEffectMessage{
			Asset: util.HashID(effectName), ObjectID: objectID,
		},
		raknet.ObjectUpdateMessage{
			ObjectID: objectID, PositionX: position.X,
			PositionY: position.Y, PositionZ: position.Z,
			IsVisible: true,
		},
	}, "stealthReveal")
}

func StealthState(
	objectID uint32, stealthType game.StealthType,
) ([]byte, error) {
	if objectID == 0 {
		return nil, errors.New("npc stealth state invalid")
	}
	packet, err := raknet.MarshalApplication(raknet.AgentBlackboardUpdateMessage{
		ObjectID: objectID, Stealth: uint8(stealthType), IsTargetable: true,
	})
	if err != nil {
		return nil, fmt.Errorf("stealthStateMarshal: %w", err)
	}
	return packet, nil
}

func ChargeupEffect(
	objectID uint32, effectName string, isRemovalRequested bool,
) ([]byte, error) {
	if objectID == 0 || effectName == "" {
		return nil, errors.New("npc chargeup effect invalid")
	}
	message := raknet.AttachedEffectMessage{
		Slot: 1, ObjectID: objectID,
		IsRemovalRequested: isRemovalRequested,
		IsHardStop:         isRemovalRequested,
	}
	if !isRemovalRequested {
		message.Asset = util.HashID(effectName)
		message.IsForceAttached = true
	}
	packet, err := raknet.MarshalApplication(message)
	if err != nil {
		return nil, fmt.Errorf("chargeupEffectMarshal: %w", err)
	}
	return packet, nil
}

func ResurrectionCast(
	sourceObjectID uint32, targetObjectID uint32,
	profile zonenpc.ActionProfile, timestamp uint64,
) ([][]byte, error) {
	if sourceObjectID == 0 || targetObjectID == 0 ||
		profile.AnimationName == "" ||
		(profile.TrailEffectName == "" && profile.TargetEffectName == "") {
		return nil, errors.New("npc resurrection cast invalid")
	}
	messages := []raknet.ApplicationMessage{
		raknet.SetAnimationStateMessage{
			ObjectID: sourceObjectID, State: util.HashID(profile.AnimationName),
			Timestamp: timestamp, Scale: 1,
		},
	}
	if profile.TrailEffectName != "" {
		messages = append(messages, raknet.ObjectEffectMessage{
			Asset:    util.HashID(profile.TrailEffectName),
			ObjectID: sourceObjectID, AttackerID: sourceObjectID,
		})
	}
	if profile.TargetEffectName != "" {
		messages = append(messages, raknet.ObjectEffectMessage{
			Asset:    util.HashID(profile.TargetEffectName),
			ObjectID: targetObjectID, AttackerID: sourceObjectID,
		})
	}
	return marshalMessages(messages, "resurrectionCast")
}

func ResurrectionState(target zonenpc.Snapshot) ([][]byte, error) {
	if target.Plan.ObjectID == 0 || target.HitPoint <= 0 {
		return nil, errors.New("npc resurrection state invalid")
	}
	return marshalMessages([]raknet.ApplicationMessage{
		raknet.CombatantDataDeltaMessage{
			ObjectID: target.Plan.ObjectID, HitPoints: target.HitPoint,
			IsHitPointChanged: true,
		},
		raknet.ObjectCollisionUpdateMessage{
			ObjectID: target.Plan.ObjectID, IsCollisionEnabled: true,
		},
		raknet.AgentBlackboardUpdateMessage{
			ObjectID: target.Plan.ObjectID, TargetID: target.TargetObjectID,
			IsInCombat:    target.TargetObjectID != 0,
			IsTargetable:  target.Plan.NPCProfile.IsTargetable,
			AttackerCount: 1,
		},
	}, "resurrectionState")
}

func ResurrectionHit(
	sourceObjectID uint32, target zonenpc.Snapshot,
	profile zonenpc.ActionProfile,
) ([][]byte, error) {
	if sourceObjectID == 0 || profile.ImpactEffectName == "" {
		return nil, errors.New("npc resurrection hit invalid")
	}
	packets, err := ResurrectionState(target)
	if err != nil {
		return nil, fmt.Errorf("resurrectionHitState: %w", err)
	}
	effectPacket, err := raknet.MarshalApplication(raknet.ObjectEffectMessage{
		Asset: util.HashID(profile.ImpactEffectName), ObjectID: target.Plan.ObjectID,
		AttackerID: sourceObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("resurrectionHitEffect: %w", err)
	}
	return append(packets, effectPacket), nil
}

func HealCast(
	sourceObjectID uint32, target zonenpc.Snapshot,
	profile zonenpc.ActionProfile, timestamp uint64,
) ([][]byte, error) {
	if sourceObjectID == 0 || target.Plan.ObjectID == 0 ||
		profile.AnimationName == "" || profile.TargetEffectName == "" {
		return nil, errors.New("npc heal cast invalid")
	}
	return marshalMessages([]raknet.ApplicationMessage{
		raknet.SetAnimationStateMessage{
			ObjectID: sourceObjectID, State: util.HashID(profile.AnimationName),
			Timestamp: timestamp, Scale: 1,
		},
		raknet.ObjectEffectMessage{
			Asset:    util.HashID(profile.TargetEffectName),
			ObjectID: target.Plan.ObjectID, AttackerID: sourceObjectID,
		},
	}, "healCast")
}

func HealHit(
	sourceObjectID uint32, target zonenpc.Snapshot, healedAmount float32,
	profile zonenpc.ActionProfile,
) ([][]byte, error) {
	if sourceObjectID == 0 || target.Plan.ObjectID == 0 || target.HitPoint <= 0 ||
		healedAmount <= 0 || profile.ImpactEffectName == "" {
		return nil, errors.New("npc heal hit invalid")
	}
	previousHitPoint := target.HitPoint - healedAmount
	integerChange := int32(target.HitPoint) - int32(previousHitPoint)
	return marshalMessages([]raknet.ApplicationMessage{
		raknet.ObjectEffectMessage{
			Asset:    util.HashID(profile.ImpactEffectName),
			ObjectID: target.Plan.ObjectID, AttackerID: sourceObjectID,
		},
		raknet.DamageCombatEventMessage{
			Flags: 0x0002, DeltaHealth: -healedAmount,
			TargetID: target.Plan.ObjectID, SourceID: sourceObjectID,
			IntegerHPChange: -integerChange,
		},
		raknet.CombatantDataDeltaMessage{
			ObjectID: target.Plan.ObjectID, HitPoints: target.HitPoint,
			IsHitPointChanged: true,
		},
	}, "healHit")
}

func RootedHealStart(
	sourceObjectID uint32, profile zonenpc.ActionProfile, timestamp uint64,
) ([][]byte, error) {
	if sourceObjectID == 0 || profile.LoopAnimationName == "" ||
		profile.TrailEffectName == "" {
		return nil, errors.New("npc rooted heal start invalid")
	}
	return marshalMessages([]raknet.ApplicationMessage{
		raknet.SetAnimationStateMessage{
			ObjectID: sourceObjectID, State: util.HashID(profile.LoopAnimationName),
			Timestamp: timestamp, Scale: 1,
		},
		raknet.ObjectEffectMessage{
			Asset: util.HashID(profile.TrailEffectName), ObjectID: sourceObjectID,
			AttackerID: sourceObjectID,
		},
	}, "rootedHealStart")
}

func HealDelta(
	sourceObjectID uint32, target zonenpc.Snapshot, healedAmount float32,
) ([][]byte, error) {
	if sourceObjectID == 0 || target.Plan.ObjectID == 0 || target.HitPoint <= 0 ||
		healedAmount <= 0 {
		return nil, errors.New("npc heal delta invalid")
	}
	previousHitPoint := target.HitPoint - healedAmount
	integerChange := int32(target.HitPoint) - int32(previousHitPoint)
	return marshalMessages([]raknet.ApplicationMessage{
		raknet.DamageCombatEventMessage{
			Flags: 0x0002, DeltaHealth: -healedAmount,
			TargetID: target.Plan.ObjectID, SourceID: sourceObjectID,
			IntegerHPChange: -integerChange,
		},
		raknet.CombatantDataDeltaMessage{
			ObjectID: target.Plan.ObjectID, HitPoints: target.HitPoint,
			IsHitPointChanged: true,
		},
	}, "healDelta")
}

func OozeGrowthCast(
	sourceObjectID uint32, targetObjectID uint32, position game.Vec3,
	profile zonenpc.ActionProfile, timestamp uint64,
) ([][]byte, error) {
	if sourceObjectID == 0 || targetObjectID == 0 ||
		!isFiniteVec3(position) || profile.AnimationName == "" {
		return nil, errors.New("npc ooze growth cast invalid")
	}
	return marshalMessages([]raknet.ApplicationMessage{
		raknet.ObjectPlayerMoveMessage{
			ObjectID: sourceObjectID, GoalFlags: 0x20, GoalPosition: vector(position),
		},
		raknet.ObjectUpdateMessage{
			ObjectID: sourceObjectID, PositionX: position.X,
			PositionY: position.Y, PositionZ: position.Z, IsVisible: true,
		},
		raknet.SetAnimationStateMessage{
			ObjectID: sourceObjectID, State: util.HashID(profile.AnimationName),
			Timestamp: timestamp, Scale: 1,
		},
	}, "oozeGrowthCast")
}

func ChargeStart(
	plan zonenpc.AttackPlan, timestamp uint64,
) ([][]byte, error) {
	if plan.SourceObjectID == 0 || plan.TargetObjectID == 0 ||
		plan.Profile.AnimationName == "" {
		return nil, errors.New("npc charge start invalid")
	}
	messages := []raknet.ApplicationMessage{
		attackTurn(plan),
		raknet.SetAnimationStateMessage{
			ObjectID: plan.SourceObjectID, State: util.HashID(plan.Profile.AnimationName),
			Timestamp: timestamp, Scale: 1,
		},
	}
	if plan.Profile.TrailEffectName != "" {
		messages = append(messages, raknet.ObjectEffectMessage{
			Asset:    util.HashID(plan.Profile.TrailEffectName),
			ObjectID: plan.SourceObjectID, AttackerID: plan.SourceObjectID,
		})
	}
	if plan.Profile.StealthType != 0 {
		messages = append(messages, raknet.AgentBlackboardUpdateMessage{
			ObjectID: plan.SourceObjectID, IsTargetable: true,
		})
	}
	return marshalMessages(messages, "chargeStart")
}

func ChargeMovementState(
	objectID uint32, movementSpeedBuff float32, stealthType game.StealthType,
	isCollisionEnabled bool,
) ([][]byte, error) {
	if objectID == 0 || math.IsNaN(float64(movementSpeedBuff)) ||
		math.IsInf(float64(movementSpeedBuff), 0) {
		return nil, errors.New("npc charge movement state invalid")
	}
	return marshalMessages([]raknet.ApplicationMessage{
		raknet.AttributeDataUpdateMessage{
			ObjectID: objectID, Value: map[uint8]float32{48: movementSpeedBuff},
		},
		raknet.AgentBlackboardUpdateMessage{
			ObjectID: objectID, Stealth: uint8(stealthType), IsTargetable: true,
		},
		raknet.ObjectCollisionUpdateMessage{
			ObjectID: objectID, IsCollisionEnabled: isCollisionEnabled,
		},
	}, "chargeMovementState")
}

func ChargeRelax(
	objectID uint32, animationName string, timestamp uint64,
) ([]byte, error) {
	return AnimationState(objectID, animationName, timestamp)
}

func ChargeCleanup(
	objectID uint32, position game.Vec3, facing game.Vec3,
) ([][]byte, error) {
	if objectID == 0 || !isFiniteVec3(position) ||
		!isFiniteVec3(facing) || facing.Length() <= 0 {
		return nil, errors.New("npc charge cleanup invalid")
	}
	positionVector := vector(position)
	facingVector := vector(facing.Scale(1 / facing.Length()))
	return marshalMessages([]raknet.ApplicationMessage{
		raknet.ObjectUpdateMessage{
			ObjectID: objectID, PositionX: position.X,
			PositionY: position.Y, PositionZ: position.Z,
			IsVisible: true,
		},
		raknet.ObjectPlayerMoveMessage{
			ObjectID: objectID, GoalFlags: 0x06,
			GoalPosition: positionVector, Facing: facingVector,
		},
	}, "chargeCleanup")
}

func AttackHit(
	plan zonenpc.AttackPlan, result zonenpc.AttackResult, hitPoint float32,
) ([][]byte, error) {
	if result.Damage <= 0 || hitPoint < 0 {
		return nil, errors.New("npc attack hit invalid")
	}
	flags := uint16(0x0001)
	if hitPoint == 0 {
		flags |= 0x0004
	}
	if result.IsCritical {
		flags |= 0x0008
	}
	packets, err := marshalMessages([]raknet.ApplicationMessage{
		raknet.DamageCombatEventMessage{
			Flags: flags, DeltaHealth: result.Damage,
			TargetID: plan.TargetObjectID, SourceID: plan.SourceObjectID,
			IntegerHPChange: -int32(result.Damage),
		},
		raknet.CombatantDataDeltaMessage{
			ObjectID: plan.TargetObjectID, HitPoints: hitPoint,
			IsHitPointChanged: true,
		},
	}, "attackHit")
	if err != nil {
		return nil, fmt.Errorf("attackHitMessages: %w", err)
	}
	return packets, nil
}

func PositionedEffect(effectName string, position game.Vec3) ([]byte, error) {
	if effectName == "" || !isFiniteVec3(position) {
		return nil, errors.New("npc positioned effect invalid")
	}
	packet, err := raknet.MarshalApplication(raknet.PositionedEffectMessage{
		Asset: util.HashID(effectName), Position: vector(position),
	})
	if err != nil {
		return nil, fmt.Errorf("positionedEffectMarshal: %w", err)
	}
	return packet, nil
}

func DeathDetonation(objectID uint32, effectName string) ([]byte, error) {
	if objectID == 0 || effectName == "" {
		return nil, errors.New("npc death detonation invalid")
	}
	packet, err := raknet.MarshalApplication(raknet.ObjectEffectMessage{
		Asset: util.HashID(effectName), ObjectID: objectID, AttackerID: objectID,
	})
	if err != nil {
		return nil, fmt.Errorf("deathDetonationMarshal: %w", err)
	}
	return packet, nil
}

func ModifierCreate(
	plan zonenpc.AttackPlan, instanceID uint32, timestamp uint64,
) ([]byte, error) {
	if plan.SourceObjectID == 0 || plan.TargetObjectID == 0 || instanceID == 0 ||
		plan.Profile.ModifierName == "" || plan.Profile.ModifierDuration <= 0 {
		return nil, errors.New("npc modifier create invalid")
	}
	packet, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
		SourceObjectID: plan.SourceObjectID,
		TargetObjectID: plan.TargetObjectID,
		ModifierID:     plan.Profile.ModifierGUID(),
		InstanceID:     instanceID,
		Duration:       plan.Profile.ModifierDuration,
		Timestamp:      timestamp,
	})
	if err != nil {
		return nil, fmt.Errorf("modifierCreate: %w", err)
	}
	return packet, nil
}

func marshalMessages(
	messages []raknet.ApplicationMessage, operation string,
) ([][]byte, error) {
	packets := make([][]byte, 0, len(messages))
	for index, message := range messages {
		move, isMove := message.(raknet.ObjectPlayerMoveMessage)
		if isMove && (move.GoalFlags == 0x42 || move.GoalFlags == 0x102 || move.GoalFlags == 0x06) {
			// Remote NPC movement consumes the partial goal even for turns.
			// 0x91 leaves it untouched; initialize it before enabling the turn.
			goalPacket, err := raknet.MarshalApplication(raknet.LocomotionUpdateContractMessage{
				ObjectID:   move.ObjectID,
				Locomotion: raknet.LocomotionReflection{PartialGoalPosition: &move.GoalPosition},
			})
			if err != nil {
				return nil, fmt.Errorf("%sTurnGoal[%d]: %w", operation, index, err)
			}
			packets = append(packets, goalPacket)
		}
		packet, err := raknet.MarshalApplication(message)
		if err != nil {
			return nil, fmt.Errorf("%sMarshal[%d]: %w", operation, index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func vector(position game.Vec3) raknet.Vector3 {
	return raknet.Vector3{X: position.X, Y: position.Y, Z: position.Z}
}

func direction(start raknet.Vector3, end raknet.Vector3) raknet.Vector3 {
	x := end.X - start.X
	y := end.Y - start.Y
	z := end.Z - start.Z
	length := float32(math.Sqrt(float64(x*x + y*y + z*z)))
	if length == 0 {
		return raknet.Vector3{X: 1}
	}
	return raknet.Vector3{X: x / length, Y: y / length, Z: z / length}
}

func isFiniteVec3(position game.Vec3) bool {
	return !math.IsNaN(float64(position.X)) && !math.IsInf(float64(position.X), 0) &&
		!math.IsNaN(float64(position.Y)) && !math.IsInf(float64(position.Y), 0) &&
		!math.IsNaN(float64(position.Z)) && !math.IsInf(float64(position.Z), 0)
}
