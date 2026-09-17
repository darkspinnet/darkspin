package ability

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/sim"
)

const areaEffectPhase sim.Phase = "areaEffect"
const AreaEffectActorRole sim.Role = "areaEffectActor"
const AreaEffectObjectRole sim.Role = "areaEffectObject"

const TargetedAOEActorRole = AreaEffectActorRole
const TargetedAOEEffectRole = AreaEffectObjectRole

type AreaEffectRequest struct {
	Ability  sim.AbilityDefinition
	Position sim.Position
}

type AreaEffectResult struct {
	Events []sim.Event
}

type areaEffectBehavior interface {
	Cancel() error
}

type AreaEffectRun struct {
	simulator *sim.Simulator
	session   *sim.Session
	behavior  areaEffectBehavior
	outbox    *areaEffectOutbox
}

type areaEffectOutbox struct {
	ability  sim.AbilityDefinition
	position sim.Position
	nounName string
	events   []sim.Event
}

func NewAreaEffectRun(
	req AreaEffectRequest,
) (*AreaEffectRun, AreaEffectResult, error) {
	nounName, err := areaEffectNoun(req.Ability)
	if err != nil {
		return nil, AreaEffectResult{}, fmt.Errorf("noun: %w", err)
	}
	outbox := &areaEffectOutbox{
		ability: req.Ability, position: req.Position, nounName: nounName,
	}
	director, err := sim.NewDirector([]sim.PhaseDefinition{{
		Name: areaEffectPhase,
		ActiveRoles: []sim.Role{
			AreaEffectActorRole,
			AreaEffectObjectRole,
		},
	}}, areaEffectPhase)
	if err != nil {
		return nil, AreaEffectResult{}, fmt.Errorf("directorCreate: %w", err)
	}
	dispatcher := sim.NewDispatcher(sim.Ports{
		Object: outbox, Resource: outbox, Area: outbox, Cooldown: outbox,
		Presentation: outbox, PositionedPresentation: outbox,
	})
	session, err := sim.NewSession(director, dispatcher)
	if err != nil {
		return nil, AreaEffectResult{}, fmt.Errorf("sessionCreate: %w", err)
	}
	simulator := director.Simulator()
	behavior, err := startAreaEffect(simulator, req)
	if err != nil {
		return nil, AreaEffectResult{}, fmt.Errorf("behaviorStart: %w", err)
	}
	run := &AreaEffectRun{
		simulator: simulator, session: session, behavior: behavior, outbox: outbox,
	}
	err = session.DispatchPending(context.Background())
	if err != nil {
		return nil, AreaEffectResult{}, fmt.Errorf("immediateDispatch: %w", err)
	}
	return run, outbox.drain(), nil
}

func startAreaEffect(
	simulator *sim.Simulator, req AreaEffectRequest,
) (areaEffectBehavior, error) {
	scope := simulator.Scope(AreaEffectObjectRole)
	switch req.Ability.Kind {
	case sim.AbilityKindTargetedAOE:
		behavior, err := sim.StartTargetedAOE(
			simulator, scope, sim.TargetedAOEInput{
				Definition: req.Ability,
				ActorRole:  AreaEffectActorRole,
				EffectRole: AreaEffectObjectRole,
				Position:   req.Position,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("targetedAOE: %w", err)
		}
		return behavior, nil
	case sim.AbilityKindAreaHealing:
		behavior, err := sim.StartAreaHealing(
			simulator, scope, sim.AreaHealingInput{
				Definition: req.Ability,
				ActorRole:  AreaEffectActorRole,
				EffectRole: AreaEffectObjectRole,
				Position:   req.Position,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("areaHealing: %w", err)
		}
		return behavior, nil
	default:
		return nil, fmt.Errorf("unsupported kind: %s", req.Ability.Kind)
	}
}

func areaEffectNoun(ability sim.AbilityDefinition) (string, error) {
	switch ability.Kind {
	case sim.AbilityKindTargetedAOE:
		if len(ability.EffectNouns) == 0 || ability.EffectNouns[0] == "" {
			return "", errors.New("targeted AOE noun missing")
		}
		return ability.EffectNouns[0], nil
	case sim.AbilityKindAreaHealing:
		if ability.SpawnNoun == "" {
			return "", errors.New("area healing noun missing")
		}
		return ability.SpawnNoun, nil
	default:
		return "", fmt.Errorf("unsupported kind: %s", ability.Kind)
	}
}

func (r *AreaEffectRun) Advance(
	ctx context.Context, deadline time.Duration,
) (AreaEffectResult, error) {
	if r == nil || r.simulator == nil || r.session == nil ||
		r.outbox == nil || ctx == nil {
		return AreaEffectResult{}, errors.New("invalid area effect run")
	}
	now := r.simulator.Now()
	if deadline < now {
		return AreaEffectResult{}, fmt.Errorf(
			"deadlineBeforeNow: %s < %s", deadline, now,
		)
	}
	err := r.session.AdvanceBy(ctx, deadline-now)
	if err != nil {
		return AreaEffectResult{}, fmt.Errorf("sessionAdvance: %w", err)
	}
	return r.outbox.drain(), nil
}

func (r *AreaEffectRun) Cancel(ctx context.Context) (AreaEffectResult, error) {
	if r == nil || r.simulator == nil || r.session == nil ||
		r.behavior == nil || r.outbox == nil || ctx == nil {
		return AreaEffectResult{}, errors.New("invalid area effect cancellation")
	}
	err := r.behavior.Cancel()
	if err != nil {
		return AreaEffectResult{}, fmt.Errorf("behaviorCancel: %w", err)
	}
	err = r.session.DispatchPending(ctx)
	if err != nil {
		return AreaEffectResult{}, fmt.Errorf("cleanupDispatch: %w", err)
	}
	return r.outbox.drain(), nil
}

func (r *AreaEffectRun) Stop() {
	if r == nil {
		return
	}
	if r.behavior != nil {
		_ = r.behavior.Cancel()
	}
	if r.simulator != nil {
		r.simulator.InvalidateRole(AreaEffectObjectRole)
		r.simulator.InvalidateRole(AreaEffectActorRole)
		r.simulator.Stop()
	}
}

type TargetedAOERequest = AreaEffectRequest
type TargetedAOEResult = AreaEffectResult
type TargetedAOERun = AreaEffectRun

func NewTargetedAOERun(
	req TargetedAOERequest,
) (*TargetedAOERun, TargetedAOEResult, error) {
	if req.Ability.Kind != sim.AbilityKindTargetedAOE {
		return nil, TargetedAOEResult{}, errors.New("invalid targeted AOE request")
	}
	return NewAreaEffectRun(req)
}

func (o *areaEffectOutbox) Spawn(
	_ context.Context, meta sim.EventMeta, intent sim.SpawnIntent,
) error {
	if intent.Role != AreaEffectObjectRole || intent.NounName != o.nounName {
		return fmt.Errorf("spawnIntent: %#v", intent)
	}
	o.append(meta, intent)
	return nil
}

func (o *areaEffectOutbox) Despawn(
	_ context.Context, meta sim.EventMeta, intent sim.DespawnIntent,
) error {
	if intent.Role != AreaEffectObjectRole {
		return fmt.Errorf("despawnIntent: %#v", intent)
	}
	o.append(meta, intent)
	return nil
}

func (o *areaEffectOutbox) Move(
	_ context.Context, _ sim.EventMeta, intent sim.MovementIntent,
) error {
	return fmt.Errorf("movementUnexpected: %#v", intent)
}

func (o *areaEffectOutbox) Teleport(
	_ context.Context, meta sim.EventMeta, intent sim.TeleportIntent,
) error {
	if intent.Role != AreaEffectObjectRole || intent.Destination != o.position {
		return fmt.Errorf("teleportIntent: %#v", intent)
	}
	o.append(meta, intent)
	return nil
}

func (o *areaEffectOutbox) CreateTriggerVolume(
	_ context.Context, _ sim.EventMeta, intent sim.TriggerVolumeIntent,
) error {
	return fmt.Errorf("triggerUnexpected: %#v", intent)
}

func (o *areaEffectOutbox) DestroyTriggerVolume(
	_ context.Context, _ sim.EventMeta, intent sim.DestroyTriggerVolumeIntent,
) error {
	return fmt.Errorf("triggerDestroyUnexpected: %#v", intent)
}

func (o *areaEffectOutbox) Start(
	_ context.Context, meta sim.EventMeta, intent sim.CooldownIntent,
) error {
	if intent.Role != AreaEffectActorRole ||
		intent.AbilityName != o.ability.Name ||
		intent.Duration != o.ability.Cooldown {
		return fmt.Errorf("cooldownIntent: %#v", intent)
	}
	o.append(meta, intent)
	return nil
}

func (o *areaEffectOutbox) Change(
	_ context.Context, meta sim.EventMeta, intent sim.ResourceChangeIntent,
) error {
	if intent.Role != AreaEffectActorRole ||
		intent.Delta != -o.ability.ManaCost {
		return fmt.Errorf("resourceIntent: %#v", intent)
	}
	o.append(meta, intent)
	return nil
}

func (o *areaEffectOutbox) Pulse(
	_ context.Context, meta sim.EventMeta, intent sim.AreaPulseIntent,
) error {
	if intent.Role != AreaEffectObjectRole ||
		intent.ActorRole != AreaEffectActorRole {
		return fmt.Errorf("pulseIntent: %#v", intent)
	}
	if o.ability.Kind == sim.AbilityKindAreaHealing &&
		(intent.DamageMinimum != 0 || intent.DamageMaximum != 0) {
		return fmt.Errorf("healingPulseIntent: %#v", intent)
	}
	o.append(meta, intent)
	return nil
}

func (o *areaEffectOutbox) SetVisibility(
	_ context.Context, meta sim.EventMeta, intent sim.VisibilityIntent,
) error {
	o.append(meta, intent)
	return nil
}

func (o *areaEffectOutbox) Animate(
	_ context.Context, meta sim.EventMeta, intent sim.AnimationIntent,
) error {
	o.append(meta, intent)
	return nil
}

func (o *areaEffectOutbox) ApplyEffect(
	_ context.Context, meta sim.EventMeta, intent sim.EffectIntent,
) error {
	o.append(meta, intent)
	return nil
}

func (o *areaEffectOutbox) StartCinematic(
	_ context.Context, meta sim.EventMeta, intent sim.CinematicIntent,
) error {
	o.append(meta, intent)
	return nil
}

func (o *areaEffectOutbox) ShowDialogue(
	_ context.Context, meta sim.EventMeta, intent sim.DialogueIntent,
) error {
	o.append(meta, intent)
	return nil
}

func (o *areaEffectOutbox) Notify(
	_ context.Context, meta sim.EventMeta, intent sim.ClientEventIntent,
) error {
	o.append(meta, intent)
	return nil
}

func (o *areaEffectOutbox) ResetAnimation(
	_ context.Context, meta sim.EventMeta, intent sim.AnimationResetIntent,
) error {
	o.append(meta, intent)
	return nil
}

func (o *areaEffectOutbox) ApplyPositionedEffect(
	_ context.Context, meta sim.EventMeta, intent sim.PositionedEffectIntent,
) error {
	o.append(meta, intent)
	return nil
}

func (o *areaEffectOutbox) append(meta sim.EventMeta, intent sim.Intent) {
	o.events = append(o.events, sim.Event{
		ID: meta.ID, At: meta.At, Phase: meta.Phase,
		Provenance: meta.Provenance, Intent: intent,
	})
}

func (o *areaEffectOutbox) drain() AreaEffectResult {
	result := AreaEffectResult{Events: o.events}
	o.events = nil
	return result
}
