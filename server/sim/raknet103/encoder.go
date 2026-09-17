package raknet103

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
)

// Binding resolves one stable simulation role to its build-103 object.
type Binding struct {
	ObjectID    uint32
	Position    sim.Position
	Rotation    sim.Position
	Orientation raknet.Quaternion
	Team        uint8
	ManaPoints  float32
}

// RoleResolver is owned by the gameplay session adapter. Implementations must
// reject missing or stale role generations.
type RoleResolver interface {
	ResolveRole(context.Context, sim.Role) (Binding, error)
}

// Encoder translates semantic presentation events to build-103 application
// payloads. It performs no socket writes and owns no scheduling.
type Encoder struct {
	roleResolver                RoleResolver
	sourceTimeEpochMilliseconds uint64
	effectSlots                 map[uint32][16]bool
}

func NewEncoder(roleResolver RoleResolver, sourceTimeEpochMilliseconds uint64) (*Encoder, error) {
	if roleResolver == nil {
		return nil, errors.New("nil role resolver")
	}
	return &Encoder{
		roleResolver: roleResolver, sourceTimeEpochMilliseconds: sourceTimeEpochMilliseconds,
		effectSlots: make(map[uint32][16]bool),
	}, nil
}

// EncodeBatch marshals a single-deadline event group entirely before returning
// any bytes. Callers can therefore publish or reject the batch as one outbox
// transaction.
func (e *Encoder) EncodeBatch(ctx context.Context, events []sim.Event) ([][]byte, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	if len(events) == 0 {
		return nil, nil
	}
	deadline := events[0].At
	packets := make([][]byte, 0, len(events))
	for index, event := range events {
		if event.At != deadline {
			return nil, fmt.Errorf("deadlineMismatch[%d]: %s", index, event.At)
		}
		packet, err := e.encodeEvent(ctx, event)
		if err != nil {
			return nil, fmt.Errorf("eventEncode[%d]: %w", index, err)
		}
		if packet != nil {
			intent, isStop := event.Intent.(sim.LocomotionStopIntent)
			if isStop && intent.IsTurn {
				goalPacket, goalErr := e.encodeTurnGoal(ctx, intent)
				if goalErr != nil {
					return nil, fmt.Errorf("turnGoal[%d]: %w", index, goalErr)
				}
				packets = append(packets, goalPacket)
			}
			packets = append(packets, packet)
		}
	}
	return packets, nil
}

func (e *Encoder) encodeEvent(ctx context.Context, event sim.Event) ([]byte, error) {
	if event.At < 0 {
		return nil, errors.New("negative event time")
	}
	switch intent := event.Intent.(type) {
	case sim.WaitIntent, sim.ConditionalWaitIntent, sim.SequenceCompleteIntent, sim.TargetValidationIntent,
		sim.CooldownIntent, sim.AbilityReleaseIntent, sim.ProjectileImpactIntent,
		sim.AttributeModifierIntent:
		return nil, nil
	case sim.VisibilityIntent:
		binding, err := e.resolveRole(ctx, intent.Role)
		if err != nil {
			return nil, fmt.Errorf("visibilityRole: %w", err)
		}
		return marshalApplication(raknet.ObjectUpdateMessage{
			ObjectID: binding.ObjectID, PositionX: binding.Position.X,
			PositionY: binding.Position.Y, PositionZ: binding.Position.Z,
			IsVisible: intent.IsVisible,
		})
	case sim.LocomotionStopIntent:
		binding, err := e.resolveRole(ctx, intent.Role)
		if err != nil {
			return nil, fmt.Errorf("stopRole: %w", err)
		}
		goalFlags := uint32(0x20)
		if intent.IsTurn {
			goalFlags = 0x42
		}
		return marshalApplication(raknet.ObjectPlayerMoveMessage{
			ObjectID: binding.ObjectID, GoalFlags: goalFlags,
			GoalPosition: raknet.Vector3{
				X: binding.Position.X, Y: binding.Position.Y, Z: binding.Position.Z,
			},
			Facing: raknet.Vector3{
				X: intent.Facing.X, Y: intent.Facing.Y, Z: intent.Facing.Z,
			},
			TargetPosition: raknet.Vector3{
				X: intent.TargetPosition.X, Y: intent.TargetPosition.Y, Z: intent.TargetPosition.Z,
			},
		})
	case sim.TeleportIntent:
		binding, err := e.resolveRole(ctx, intent.Role)
		if err != nil {
			return nil, fmt.Errorf("teleportRole: %w", err)
		}
		return marshalApplication(raknet.ObjectTeleportMessage{
			ObjectID: binding.ObjectID,
			Position: raknet.Vector3{
				X: intent.Destination.X, Y: intent.Destination.Y, Z: intent.Destination.Z,
			},
			Orientation: binding.Orientation,
		})
	case sim.AnimationIntent:
		if intent.AnimationName == "" {
			return nil, errors.New("empty animation")
		}
		binding, err := e.resolveRole(ctx, intent.Role)
		if err != nil {
			return nil, fmt.Errorf("animationRole: %w", err)
		}
		timestamp, err := e.timestamp(event.At)
		if err != nil {
			return nil, fmt.Errorf("animationTime: %w", err)
		}
		return marshalApplication(raknet.SetAnimationStateMessage{
			ObjectID: binding.ObjectID, State: util.HashID(intent.AnimationName),
			Timestamp: timestamp, Scale: 1,
		})
	case sim.AnimationResetIntent:
		binding, err := e.resolveRole(ctx, intent.Role)
		if err != nil {
			return nil, fmt.Errorf("animationResetRole: %w", err)
		}
		timestamp, err := e.timestamp(event.At)
		if err != nil {
			return nil, fmt.Errorf("animationResetTime: %w", err)
		}
		return marshalApplication(raknet.SetAnimationStateMessage{
			ObjectID: binding.ObjectID, Timestamp: timestamp, Scale: 1,
		})
	case sim.GraphicsStateIntent:
		if intent.State == 0 {
			return nil, errors.New("empty graphics state")
		}
		binding, err := e.resolveRole(ctx, intent.Role)
		if err != nil {
			return nil, fmt.Errorf("graphicsRole: %w", err)
		}
		timestamp, err := e.timestamp(event.At)
		if err != nil {
			return nil, fmt.Errorf("graphicsTime: %w", err)
		}
		return marshalApplication(raknet.SetObjectGFXStateMessage{
			ObjectID: binding.ObjectID, State: intent.State, Timestamp: timestamp,
		})
	case sim.PhysicsStateIntent:
		if intent.CollisionKind == sim.CollisionStateNavigation {
			return nil, nil
		}
		if intent.CollisionKind != "" && intent.CollisionKind != sim.CollisionStatePhysics {
			return nil, fmt.Errorf("physicsKind: %s", intent.CollisionKind)
		}
		binding, err := e.resolveRole(ctx, intent.Role)
		if err != nil {
			return nil, fmt.Errorf("physicsRole: %w", err)
		}
		return marshalApplication(raknet.ObjectCollisionUpdateMessage{
			ObjectID: binding.ObjectID, IsCollisionEnabled: intent.IsCollisionEnabled,
		})
	case sim.CinematicIntent:
		binding, err := e.resolveRole(ctx, intent.FocusRole)
		if err != nil {
			return nil, fmt.Errorf("cinematicRole: %w", err)
		}
		return marshalApplication(raknet.NewCinematicMessage(
			intent.Duration,
			raknet.Vector3{X: binding.Position.X, Y: binding.Position.Y, Z: binding.Position.Z},
			intent.Radius,
		))
	case sim.ClientEventIntent:
		if intent.EventKind != sim.ClientEventPlayerUnlockedSecondCreature ||
			intent.EventID != sim.ClientEventPlayerUnlockedSecondCreatureID {
			return nil, fmt.Errorf("clientEventUnsupported: %s/%08x", intent.EventKind, intent.EventID)
		}
		return marshalApplication(raknet.ClientEventMessage{ClientEventID: intent.EventID})
	case sim.ProjectileLaunchIntent:
		projectile, err := e.resolveRole(ctx, intent.Role)
		if err != nil {
			return nil, fmt.Errorf("projectileRole: %w", err)
		}
		actor, err := e.resolveRole(ctx, intent.ActorRole)
		if err != nil {
			return nil, fmt.Errorf("projectileActor: %w", err)
		}
		var movementType uint8
		if intent.IsHoming {
			movementType = 3
		}
		return marshalApplication(raknet.ProjectileObjectCreateMessage{
			ObjectID: projectile.ObjectID, Noun: util.HashID(intent.NounName),
			Position: raknet.Vector3{X: intent.Position.X, Y: intent.Position.Y, Z: intent.Position.Z},
			Rotation: raknet.Vector3{
				X: projectile.Rotation.X, Y: projectile.Rotation.Y, Z: projectile.Rotation.Z,
			},
			Orientation: projectile.Orientation,
			LinearVelocity: raknet.Vector3{
				X: intent.Direction.X * intent.Speed,
				Y: intent.Direction.Y * intent.Speed,
				Z: intent.Direction.Z * intent.Speed,
			},
			Scale: 1, Team: actor.Team, MovementType: movementType,
		})
	case sim.EffectIntent:
		binding, err := e.resolveRole(ctx, intent.Role)
		if err != nil {
			return nil, fmt.Errorf("effectRole: %w", err)
		}
		if intent.ActorRole != "" {
			if intent.IsStopped {
				return nil, errors.New("object effect cannot stop")
			}
			actor, actorErr := e.resolveRole(ctx, intent.ActorRole)
			if actorErr != nil {
				return nil, fmt.Errorf("effectActor: %w", actorErr)
			}
			return marshalApplication(raknet.ObjectEffectMessage{
				IsCritical: intent.IsCritical, Asset: util.HashID(intent.EffectName),
				ObjectID: binding.ObjectID, AttackerID: actor.ObjectID,
				Facing: raknet.Vector3{X: intent.Facing.X, Y: intent.Facing.Y, Z: intent.Facing.Z},
			})
		}
		if intent.IsStopped {
			slot, isFound := e.releaseEffect(binding.ObjectID, intent.Slot)
			if !isFound {
				return nil, nil
			}
			return marshalApplication(raknet.AttachedEffectMessage{
				Slot: slot + 1, IsRemovalRequested: true, ObjectID: binding.ObjectID,
			})
		}
		slot, isAllocated := e.allocateEffect(binding.ObjectID, intent.Slot)
		if !isAllocated {
			return nil, nil
		}
		packet, err := marshalApplication(raknet.AttachedEffectMessage{
			Slot: slot + 1, IsForceAttached: true, Asset: util.HashID(intent.EffectName),
			ObjectID: binding.ObjectID,
		})
		if err != nil {
			e.freeEffect(binding.ObjectID, slot)
			return nil, fmt.Errorf("effectMarshal: %w", err)
		}
		return packet, nil
	case sim.ProjectileMotionIntent:
		binding, err := e.resolveRole(ctx, intent.Role)
		if err != nil {
			return nil, fmt.Errorf("motionRole: %w", err)
		}
		if intent.IsHoming {
			if intent.TargetRole == "" || intent.HomingDelay <= 0 {
				return nil, errors.New("homing motion invalid")
			}
			target, targetErr := e.resolveRole(ctx, intent.TargetRole)
			if targetErr != nil {
				return nil, fmt.Errorf("motionTarget: %w", targetErr)
			}
			targetObjectID := target.ObjectID
			targetPosition := raknet.Vector3{
				X: target.Position.X, Y: target.Position.Y, Z: target.Position.Z,
			}
			initialDirection := raknet.Vector3{
				X: intent.Direction.X, Y: intent.Direction.Y, Z: intent.Direction.Z,
			}
			reflectedLastUpdate := int32(1000)
			return marshalApplication(raknet.LocomotionUpdateContractMessage{
				ObjectID: binding.ObjectID,
				Locomotion: raknet.LocomotionReflection{
					Projectile: &raknet.ProjectileParameter{
						Speed: intent.Speed, Acceleration: intent.Acceleration,
						Range: intent.Distance, Direction: initialDirection,
						Flags: 1, HomingDelay: float32(intent.HomingDelay.Seconds()),
					},
					TargetObjectID:      &targetObjectID,
					TargetPosition:      &targetPosition,
					InitialDirection:    &initialDirection,
					ReflectedLastUpdate: &reflectedLastUpdate,
				},
			})
		}
		var collision *raknet.Vector3
		if intent.ExpectedGeometryCollision != nil {
			collision = &raknet.Vector3{
				X: intent.ExpectedGeometryCollision.X,
				Y: intent.ExpectedGeometryCollision.Y,
				Z: intent.ExpectedGeometryCollision.Z,
			}
		}
		return marshalApplication(raknet.ProjectileLocomotionMessage{
			ObjectID: binding.ObjectID,
			Parameter: raknet.ProjectileParameter{
				Speed: intent.Speed, Acceleration: intent.Acceleration,
				Range: intent.Distance, Direction: raknet.Vector3{
					X: intent.Direction.X, Y: intent.Direction.Y, Z: intent.Direction.Z,
				},
			},
			ExpectedGeometryCollision: collision, ReflectedLastUpdate: 1000,
		})
	case sim.PositionedEffectIntent:
		if intent.IsFacingOmitted {
			return marshalApplication(raknet.DropPresentationMessage{
				Asset: util.HashID(intent.EffectName),
				Position: raknet.Vector3{
					X: intent.Position.X, Y: intent.Position.Y, Z: intent.Position.Z,
				},
			})
		}
		return marshalApplication(raknet.PositionedEffectMessage{
			Asset:    util.HashID(intent.EffectName),
			Position: raknet.Vector3{X: intent.Position.X, Y: intent.Position.Y, Z: intent.Position.Z},
			Facing:   raknet.Vector3{X: intent.Facing.X, Y: intent.Facing.Y, Z: intent.Facing.Z},
		})
	case sim.DamageIntent:
		actor, err := e.resolveRole(ctx, intent.ActorRole)
		if err != nil {
			return nil, fmt.Errorf("damageActor: %w", err)
		}
		target, err := e.resolveRole(ctx, intent.TargetRole)
		if err != nil {
			return nil, fmt.Errorf("damageTarget: %w", err)
		}
		flags := uint16(0x0001)
		if intent.IsKilling {
			flags = 0x0005
		}
		if intent.IsCritical {
			flags |= 0x0008
		}
		return marshalApplication(raknet.DamageCombatEventMessage{
			Flags: flags, DeltaHealth: intent.DeltaHealth, TargetID: target.ObjectID,
			SourceID: actor.ObjectID, IntegerHPChange: intent.IntegerHitPointChange,
		})
	case sim.HitPointIntent:
		target, err := e.resolveRole(ctx, intent.Role)
		if err != nil {
			return nil, fmt.Errorf("hitPointRole: %w", err)
		}
		return marshalApplication(raknet.CombatantDataDeltaMessage{
			ObjectID: target.ObjectID, HitPoints: intent.HitPoints, IsHitPointChanged: true,
		})
	case sim.DespawnIntent:
		binding, err := e.resolveRole(ctx, intent.Role)
		if err != nil {
			return nil, fmt.Errorf("despawnRole: %w", err)
		}
		delete(e.effectSlots, binding.ObjectID)
		return marshalApplication(raknet.ObjectDeleteMessage{ObjectID: []uint32{binding.ObjectID}})
	default:
		return nil, fmt.Errorf("intentUnsupported: %T", event.Intent)
	}
}

func (e *Encoder) allocateEffect(objectID uint32, requestedSlot uint8) (uint8, bool) {
	slots := e.effectSlots[objectID]
	if requestedSlot > uint8(len(slots)) {
		return 0, false
	}
	if requestedSlot != 0 {
		slot := requestedSlot - 1
		slots[slot] = true
		e.effectSlots[objectID] = slots
		return slot, true
	}
	for index, isAllocated := range slots {
		if isAllocated {
			continue
		}
		slots[index] = true
		e.effectSlots[objectID] = slots
		return uint8(index), true
	}
	return 0, false
}

func (e *Encoder) releaseEffect(objectID uint32, requestedSlot uint8) (uint8, bool) {
	slots := e.effectSlots[objectID]
	if requestedSlot > uint8(len(slots)) {
		return 0, false
	}
	if requestedSlot != 0 {
		slot := requestedSlot - 1
		if !slots[slot] {
			return 0, false
		}
		e.freeEffect(objectID, slot)
		return slot, true
	}
	for index, isAllocated := range slots {
		if !isAllocated {
			continue
		}
		e.freeEffect(objectID, uint8(index))
		return uint8(index), true
	}
	return 0, false
}

func (e *Encoder) freeEffect(objectID uint32, slot uint8) {
	slots := e.effectSlots[objectID]
	slots[slot] = false
	e.effectSlots[objectID] = slots
}

func marshalApplication(message raknet.ApplicationMessage) ([]byte, error) {
	packet, err := raknet.MarshalApplication(message)
	if err != nil {
		return nil, fmt.Errorf("applicationMarshal: %w", err)
	}
	return packet, nil
}

func (e *Encoder) resolveRole(ctx context.Context, role sim.Role) (Binding, error) {
	if role == "" {
		return Binding{}, errors.New("empty role")
	}
	binding, err := e.roleResolver.ResolveRole(ctx, role)
	if err != nil {
		return Binding{}, fmt.Errorf("roleResolve[%s]: %w", role, err)
	}
	if binding.ObjectID == 0 {
		return Binding{}, fmt.Errorf("objectMissing[%s]", role)
	}
	return binding, nil
}

func (e *Encoder) timestamp(at time.Duration) (uint64, error) {
	milliseconds := uint64(at / time.Millisecond)
	if milliseconds > math.MaxUint64-e.sourceTimeEpochMilliseconds {
		return 0, errors.New("source time overflow")
	}
	return e.sourceTimeEpochMilliseconds + milliseconds, nil
}
