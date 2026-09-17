package sim

import (
	"context"
	"errors"
	"fmt"
)

// ObjectPort owns authoritative simulated-object mutations.
type ObjectPort interface {
	Spawn(context.Context, EventMeta, SpawnIntent) error
	Despawn(context.Context, EventMeta, DespawnIntent) error
	Move(context.Context, EventMeta, MovementIntent) error
	Teleport(context.Context, EventMeta, TeleportIntent) error
	CreateTriggerVolume(context.Context, EventMeta, TriggerVolumeIntent) error
	DestroyTriggerVolume(context.Context, EventMeta, DestroyTriggerVolumeIntent) error
}

// CompanionPort owns authoritative companion allocation and placement.
type CompanionPort interface {
	SpawnCompanion(context.Context, EventMeta, CompanionSpawnIntent) error
}

// GraphicsPort owns build-specific replicated graphics state.
type GraphicsPort interface {
	SetGraphicsState(context.Context, EventMeta, GraphicsStateIntent) error
}

// PhysicsPort owns authoritative physics and navigation collision state.
type PhysicsPort interface {
	SetPhysicsState(context.Context, EventMeta, PhysicsStateIntent) error
}

// InteractablePort owns accepted uses of world interactables.
type InteractablePort interface {
	Use(context.Context, EventMeta, InteractableUseIntent) error
}

// LootPort owns authored loot requests and their world allocation.
type LootPort interface {
	Drop(context.Context, EventMeta, LootDropIntent) error
}

// ResourcePort owns authoritative actor-resource mutations.
type ResourcePort interface {
	Change(context.Context, EventMeta, ResourceChangeIntent) error
}

// AreaPort owns live target resolution for authored area pulses.
type AreaPort interface {
	Pulse(context.Context, EventMeta, AreaPulseIntent) error
}

// CombatPort owns authoritative combat activation.
type CombatPort interface {
	Activate(context.Context, EventMeta, CombatIntent) error
}

type ProjectilePort interface {
	Launch(context.Context, EventMeta, ProjectileLaunchIntent) error
	Move(context.Context, EventMeta, ProjectileMotionIntent) error
	Impact(context.Context, EventMeta, ProjectileImpactIntent) error
}

type CooldownPort interface {
	Start(context.Context, EventMeta, CooldownIntent) error
}

type PositionedPresentationPort interface {
	ApplyPositionedEffect(context.Context, EventMeta, PositionedEffectIntent) error
}

// DamagePort owns accepted HP mutations and their resulting combatant state.
type DamagePort interface {
	Damage(context.Context, EventMeta, DamageIntent) error
	SetHitPoints(context.Context, EventMeta, HitPointIntent) error
}

// DeathPort owns non-packet Behavior_Death lifecycle state such as corpse
// fading and the eventual mark-for-delete mutation.
type DeathPort interface {
	ApplyDeathState(context.Context, EventMeta, DeathStateIntent) error
}

type LocomotionPort interface {
	Stop(context.Context, EventMeta, LocomotionStopIntent) error
}

type AttributePort interface {
	ApplyModifier(context.Context, EventMeta, AttributeModifierIntent) error
}

type ModifierPort interface {
	Request(context.Context, EventMeta, ModifierRequestIntent) error
}

// SquadPort owns authoritative squad and ability availability changes.
type SquadPort interface {
	Unlock(context.Context, EventMeta, UnlockIntent) error
}

// PickupPort owns phase-scoped pickup creation requested by authored content.
type PickupPort interface {
	DropCrystals(context.Context, EventMeta, CrystalDropIntent) error
}

// CrystalPickupPort owns authenticated crystal interaction admission and its
// mission-local delayed collection transaction.
type CrystalPickupPort interface {
	PickupCrystal(context.Context, EventMeta, CrystalPickupIntent) error
}

// ObjectivePort owns authoritative objective state.
type ObjectivePort interface {
	SetObjective(context.Context, EventMeta, ObjectiveIntent) error
	SetObjectiveData(context.Context, EventMeta, ObjectiveDataIntent) error
}

// ProgressionPort owns authoritative reward and progression transactions.
type ProgressionPort interface {
	Reward(context.Context, EventMeta, RewardIntent) error
}

// PresentationPort owns build-specific client presentation.
type PresentationPort interface {
	SetVisibility(context.Context, EventMeta, VisibilityIntent) error
	Animate(context.Context, EventMeta, AnimationIntent) error
	ApplyEffect(context.Context, EventMeta, EffectIntent) error
	StartCinematic(context.Context, EventMeta, CinematicIntent) error
	ShowDialogue(context.Context, EventMeta, DialogueIntent) error
	Notify(context.Context, EventMeta, ClientEventIntent) error
	ResetAnimation(context.Context, EventMeta, AnimationResetIntent) error
}

// Ports are the feature-owned boundaries consumed by semantic intents.
type Ports struct {
	Object                 ObjectPort
	Companion              CompanionPort
	Graphics               GraphicsPort
	Physics                PhysicsPort
	Interactable           InteractablePort
	Loot                   LootPort
	Resource               ResourcePort
	Area                   AreaPort
	Locomotion             LocomotionPort
	Attribute              AttributePort
	Modifier               ModifierPort
	Combat                 CombatPort
	Projectile             ProjectilePort
	Cooldown               CooldownPort
	Damage                 DamagePort
	Death                  DeathPort
	Squad                  SquadPort
	Pickup                 PickupPort
	CrystalPickup          CrystalPickupPort
	Objective              ObjectivePort
	Progression            ProgressionPort
	Presentation           PresentationPort
	PositionedPresentation PositionedPresentationPort
}

// Dispatcher applies one typed event to exactly one feature port. Control-flow
// events remain local to the simulator and are not dispatched.
type Dispatcher struct{ ports Ports }

func NewDispatcher(ports Ports) *Dispatcher { return &Dispatcher{ports: ports} }

// Dispatch applies one event without converting it to a protocol or storage
// representation.
func (d *Dispatcher) Dispatch(ctx context.Context, event Event) error {
	if d == nil {
		return errors.New("nil dispatcher")
	}
	if ctx == nil {
		return errors.New("nil context")
	}
	if event.Intent == nil {
		return errors.New("nil intent")
	}
	meta := EventMeta{ID: event.ID, At: event.At, Phase: event.Phase, Provenance: event.Provenance}
	var err error
	switch intent := event.Intent.(type) {
	case WaitIntent, ConditionalWaitIntent, SequenceCompleteIntent, AbilityReleaseIntent:
		return nil
	case TargetValidationIntent:
		return nil
	case SpawnIntent:
		if d.ports.Object == nil {
			return errors.New("object port missing")
		}
		err = d.ports.Object.Spawn(ctx, meta, intent)
	case CompanionSpawnIntent:
		if d.ports.Companion == nil {
			return errors.New("companion port missing")
		}
		err = d.ports.Companion.SpawnCompanion(ctx, meta, intent)
	case DespawnIntent:
		if d.ports.Object == nil {
			return errors.New("object port missing")
		}
		err = d.ports.Object.Despawn(ctx, meta, intent)
	case MovementIntent:
		if d.ports.Object == nil {
			return errors.New("object port missing")
		}
		err = d.ports.Object.Move(ctx, meta, intent)
	case TeleportIntent:
		if d.ports.Object == nil {
			return errors.New("object port missing")
		}
		err = d.ports.Object.Teleport(ctx, meta, intent)
	case TriggerVolumeIntent:
		if d.ports.Object == nil {
			return errors.New("object port missing")
		}
		err = d.ports.Object.CreateTriggerVolume(ctx, meta, intent)
	case DestroyTriggerVolumeIntent:
		if d.ports.Object == nil {
			return errors.New("object port missing")
		}
		err = d.ports.Object.DestroyTriggerVolume(ctx, meta, intent)
	case LocomotionStopIntent:
		if d.ports.Locomotion == nil {
			return errors.New("locomotion port missing")
		}
		err = d.ports.Locomotion.Stop(ctx, meta, intent)
	case GraphicsStateIntent:
		if d.ports.Graphics == nil {
			return errors.New("graphics port missing")
		}
		err = d.ports.Graphics.SetGraphicsState(ctx, meta, intent)
	case PhysicsStateIntent:
		if d.ports.Physics == nil {
			return errors.New("physics port missing")
		}
		err = d.ports.Physics.SetPhysicsState(ctx, meta, intent)
	case InteractableUseIntent:
		if d.ports.Interactable == nil {
			return errors.New("interactable port missing")
		}
		err = d.ports.Interactable.Use(ctx, meta, intent)
	case LootDropIntent:
		if d.ports.Loot == nil {
			return errors.New("loot port missing")
		}
		err = d.ports.Loot.Drop(ctx, meta, intent)
	case AttributeModifierIntent:
		if d.ports.Attribute == nil {
			return errors.New("attribute port missing")
		}
		err = d.ports.Attribute.ApplyModifier(ctx, meta, intent)
	case ModifierRequestIntent:
		if d.ports.Modifier == nil {
			return errors.New("modifier port missing")
		}
		err = d.ports.Modifier.Request(ctx, meta, intent)
	case CombatIntent:
		if d.ports.Combat == nil {
			return errors.New("combat port missing")
		}
		err = d.ports.Combat.Activate(ctx, meta, intent)
	case CooldownIntent:
		if d.ports.Cooldown == nil {
			return errors.New("cooldown port missing")
		}
		err = d.ports.Cooldown.Start(ctx, meta, intent)
	case ResourceChangeIntent:
		if d.ports.Resource == nil {
			return errors.New("resource port missing")
		}
		err = d.ports.Resource.Change(ctx, meta, intent)
	case AreaPulseIntent:
		if d.ports.Area == nil {
			return errors.New("area port missing")
		}
		err = d.ports.Area.Pulse(ctx, meta, intent)
	case ProjectileLaunchIntent:
		if d.ports.Projectile == nil {
			return errors.New("projectile port missing")
		}
		err = d.ports.Projectile.Launch(ctx, meta, intent)
	case ProjectileImpactIntent:
		if d.ports.Projectile == nil {
			return errors.New("projectile port missing")
		}
		err = d.ports.Projectile.Impact(ctx, meta, intent)
	case ProjectileMotionIntent:
		if d.ports.Projectile == nil {
			return errors.New("projectile port missing")
		}
		err = d.ports.Projectile.Move(ctx, meta, intent)
	case DamageIntent:
		if d.ports.Damage == nil {
			return errors.New("damage port missing")
		}
		err = d.ports.Damage.Damage(ctx, meta, intent)
	case HitPointIntent:
		if d.ports.Damage == nil {
			return errors.New("damage port missing")
		}
		err = d.ports.Damage.SetHitPoints(ctx, meta, intent)
	case DeathStateIntent:
		if d.ports.Death == nil {
			return errors.New("death port missing")
		}
		err = d.ports.Death.ApplyDeathState(ctx, meta, intent)
	case UnlockIntent:
		if d.ports.Squad == nil {
			return errors.New("squad port missing")
		}
		err = d.ports.Squad.Unlock(ctx, meta, intent)
	case CrystalDropIntent:
		if d.ports.Pickup == nil {
			return errors.New("pickup port missing")
		}
		err = d.ports.Pickup.DropCrystals(ctx, meta, intent)
	case CrystalPickupIntent:
		if d.ports.CrystalPickup == nil {
			return errors.New("crystal pickup port missing")
		}
		err = d.ports.CrystalPickup.PickupCrystal(ctx, meta, intent)
	case ObjectiveIntent:
		if d.ports.Objective == nil {
			return errors.New("objective port missing")
		}
		err = d.ports.Objective.SetObjective(ctx, meta, intent)
	case ObjectiveDataIntent:
		if d.ports.Objective == nil {
			return errors.New("objective port missing")
		}
		err = d.ports.Objective.SetObjectiveData(ctx, meta, intent)
	case RewardIntent:
		if d.ports.Progression == nil {
			return errors.New("progression port missing")
		}
		err = d.ports.Progression.Reward(ctx, meta, intent)
	case VisibilityIntent:
		if d.ports.Presentation == nil {
			return errors.New("presentation port missing")
		}
		err = d.ports.Presentation.SetVisibility(ctx, meta, intent)
	case AnimationIntent:
		if d.ports.Presentation == nil {
			return errors.New("presentation port missing")
		}
		err = d.ports.Presentation.Animate(ctx, meta, intent)
	case AnimationResetIntent:
		if d.ports.Presentation == nil {
			return errors.New("presentation port missing")
		}
		err = d.ports.Presentation.ResetAnimation(ctx, meta, intent)
	case EffectIntent:
		if d.ports.Presentation == nil {
			return errors.New("presentation port missing")
		}
		err = d.ports.Presentation.ApplyEffect(ctx, meta, intent)
	case PositionedEffectIntent:
		if d.ports.PositionedPresentation == nil {
			return errors.New("positioned presentation port missing")
		}
		err = d.ports.PositionedPresentation.ApplyPositionedEffect(ctx, meta, intent)
	case CinematicIntent:
		if d.ports.Presentation == nil {
			return errors.New("presentation port missing")
		}
		err = d.ports.Presentation.StartCinematic(ctx, meta, intent)
	case DialogueIntent:
		if d.ports.Presentation == nil {
			return errors.New("presentation port missing")
		}
		err = d.ports.Presentation.ShowDialogue(ctx, meta, intent)
	case ClientEventIntent:
		if d.ports.Presentation == nil {
			return errors.New("presentation port missing")
		}
		err = d.ports.Presentation.Notify(ctx, meta, intent)
	default:
		return fmt.Errorf("intentUnsupported: %T", event.Intent)
	}
	if err != nil {
		return fmt.Errorf("intentDispatch[%T]: %w", event.Intent, err)
	}
	return nil
}
