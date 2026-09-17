package sim

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Session coordinates one director, simulator clock, and set of feature ports.
type Session struct {
	director       *Director
	dispatcher     *Dispatcher
	nextEventIndex int
}

func NewSession(director *Director, dispatcher *Dispatcher) (*Session, error) {
	if director == nil {
		return nil, errors.New("nil director")
	}
	if dispatcher == nil {
		return nil, errors.New("nil dispatcher")
	}
	return &Session{director: director, dispatcher: dispatcher}, nil
}

func (s *Session) Director() *Director { return s.director }

// Reset starts a clean director/simulator epoch and clears dispatch progress.
func (s *Session) Reset(initialPhase Phase, facts map[Fact]int64) error {
	err := s.director.Reset(initialPhase, facts)
	if err != nil {
		return fmt.Errorf("directorReset: %w", err)
	}
	s.nextEventIndex = 0
	return nil
}

// RunProgram starts one normalized program and dispatches every immediately
// emitted event.
func (s *Session) RunProgram(ctx context.Context, scope CancelScope, program Program) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	err := RunProgram(s.director.Simulator(), scope, program)
	if err != nil {
		return fmt.Errorf("programRun: %w", err)
	}
	err = s.DispatchPending(ctx)
	if err != nil {
		return fmt.Errorf("pendingDispatch: %w", err)
	}
	return nil
}

// AdvanceBy advances the simulation clock and dispatches newly due events.
func (s *Session) AdvanceBy(ctx context.Context, duration time.Duration) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	err := s.director.Simulator().AdvanceBy(duration)
	if err != nil {
		return fmt.Errorf("clockAdvance: %w", err)
	}
	err = s.DispatchPending(ctx)
	if err != nil {
		return fmt.Errorf("pendingDispatch: %w", err)
	}
	return nil
}

// DispatchPending validates and applies events in order. The cursor advances
// only after success, so an adapter failure remains visible and retryable.
func (s *Session) DispatchPending(ctx context.Context) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	events := s.director.Simulator().Events()
	if s.nextEventIndex > len(events) {
		return fmt.Errorf("eventCursor: got %d, have %d", s.nextEventIndex, len(events))
	}
	for s.nextEventIndex < len(events) {
		event := events[s.nextEventIndex]
		if event.Scope.sessionGeneration != 0 && !s.director.Simulator().isScopeActive(event.Scope) {
			s.nextEventIndex++
			continue
		}
		err := s.validateEvent(event)
		if err != nil {
			return fmt.Errorf("eventValidate[%d]: %w", s.nextEventIndex, err)
		}
		err = s.dispatcher.Dispatch(ctx, event)
		if err != nil {
			return fmt.Errorf("eventDispatch[%d]: %w", s.nextEventIndex, err)
		}
		s.nextEventIndex++
	}
	return nil
}

func (s *Session) validateEvent(event Event) error {
	roles, err := intentRoles(event.Intent)
	if err != nil {
		return fmt.Errorf("intentRoles: %w", err)
	}
	for _, role := range roles {
		if role == "" {
			return errors.New("empty role")
		}
		if !s.director.IsRoleActive(role) {
			return fmt.Errorf("roleInactive: %s", role)
		}
	}
	return nil
}

func intentRoles(intent Intent) ([]Role, error) {
	switch typedIntent := intent.(type) {
	case WaitIntent:
		return nil, nil
	case ConditionalWaitIntent:
		roles := make([]Role, 0, 2)
		if typedIntent.Role != "" {
			roles = append(roles, typedIntent.Role)
		}
		if typedIntent.TargetRole != "" {
			roles = append(roles, typedIntent.TargetRole)
		}
		return roles, nil
	case SpawnIntent:
		return []Role{typedIntent.Role}, nil
	case CompanionSpawnIntent:
		return []Role{typedIntent.Role, typedIntent.OwnerRole}, nil
	case DespawnIntent:
		return []Role{typedIntent.Role}, nil
	case VisibilityIntent:
		return []Role{typedIntent.Role}, nil
	case AnimationIntent:
		return []Role{typedIntent.Role}, nil
	case AnimationResetIntent:
		return []Role{typedIntent.Role}, nil
	case GraphicsStateIntent:
		return []Role{typedIntent.Role}, nil
	case PhysicsStateIntent:
		return []Role{typedIntent.Role}, nil
	case InteractableUseIntent:
		return []Role{typedIntent.Role}, nil
	case LootDropIntent:
		return []Role{typedIntent.SourceRole, typedIntent.PlayerRole}, nil
	case EffectIntent:
		if typedIntent.ActorRole != "" {
			return []Role{typedIntent.Role, typedIntent.ActorRole}, nil
		}
		return []Role{typedIntent.Role}, nil
	case PositionedEffectIntent:
		return nil, nil
	case CinematicIntent:
		return []Role{typedIntent.FocusRole}, nil
	case DialogueIntent:
		return []Role{typedIntent.Role}, nil
	case ClientEventIntent:
		return nil, nil
	case ObjectiveIntent:
		return nil, nil
	case ObjectiveDataIntent:
		return nil, nil
	case UnlockIntent:
		return []Role{typedIntent.PlayerRole}, nil
	case CrystalDropIntent:
		return []Role{typedIntent.PlayerRole}, nil
	case CrystalPickupIntent:
		return []Role{typedIntent.PlayerRole, typedIntent.AgentRole, typedIntent.TargetRole}, nil
	case MovementIntent:
		return []Role{typedIntent.Role, typedIntent.Target}, nil
	case TeleportIntent:
		return []Role{typedIntent.Role}, nil
	case TriggerVolumeIntent:
		return []Role{typedIntent.Role}, nil
	case DestroyTriggerVolumeIntent:
		return []Role{typedIntent.Role}, nil
	case LocomotionStopIntent:
		return []Role{typedIntent.Role}, nil
	case AttributeModifierIntent:
		return []Role{typedIntent.Role}, nil
	case ModifierRequestIntent:
		return []Role{typedIntent.TargetRole, typedIntent.InitiatorRole}, nil
	case CombatIntent:
		return []Role{typedIntent.ActorRole, typedIntent.TargetRole}, nil
	case TargetValidationIntent:
		return []Role{typedIntent.ActorRole, typedIntent.TargetRole}, nil
	case CooldownIntent:
		return []Role{typedIntent.Role}, nil
	case ResourceChangeIntent:
		return []Role{typedIntent.Role}, nil
	case AreaPulseIntent:
		return []Role{typedIntent.Role, typedIntent.ActorRole}, nil
	case AbilityReleaseIntent:
		return []Role{typedIntent.Role}, nil
	case ProjectileLaunchIntent:
		roles := []Role{typedIntent.Role, typedIntent.ActorRole}
		if typedIntent.TargetRole != "" {
			roles = append(roles, typedIntent.TargetRole)
		}
		return roles, nil
	case ProjectileImpactIntent:
		roles := []Role{typedIntent.Role}
		if typedIntent.TargetRole != "" {
			roles = append(roles, typedIntent.TargetRole)
		}
		return roles, nil
	case ProjectileMotionIntent:
		return []Role{typedIntent.Role}, nil
	case DamageIntent:
		return []Role{typedIntent.ActorRole, typedIntent.TargetRole}, nil
	case HitPointIntent:
		return []Role{typedIntent.Role}, nil
	case DeathStateIntent:
		return []Role{typedIntent.Role}, nil
	case RewardIntent:
		return []Role{typedIntent.PlayerRole}, nil
	case SequenceCompleteIntent:
		return []Role{typedIntent.Role}, nil
	default:
		return nil, fmt.Errorf("unsupported: %T", intent)
	}
}
