package interact

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
)

const phase sim.Phase = "campaignInteractable"
const Role sim.Role = "campaignInteractable"
const PlayerRole sim.Role = "player"

type Run struct {
	session    *sim.Session
	outbox     *outbox
	deadlines  []time.Duration
	isComplete bool
}

type outbox struct {
	ability        sim.AbilityDefinition
	events         []sim.Event
	isDropObserved bool
}

func NewRun(
	ctx context.Context, use game.CampaignScriptUse,
	ability sim.AbilityDefinition,
) (*Run, []sim.Event, error) {
	invocation := use.Invocation
	if ctx == nil || invocation.TargetObjectID == 0 || invocation.CallbackName == "" ||
		invocation.CallbackName != ability.Name || invocation.Challenge <= 0 {
		return nil, nil, errors.New("campaignInteractable: invalid invocation")
	}
	program, err := sim.InteractableProgram(
		ability, Role, PlayerRole,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("programCreate: %w", err)
	}
	deadlines, err := sim.InteractableDeadlines(ability)
	if err != nil {
		return nil, nil, fmt.Errorf("deadlineCreate: %w", err)
	}
	if len(deadlines) != 3 || deadlines[0] <= 0 || deadlines[1] <= deadlines[0] || deadlines[2] <= deadlines[1] {
		return nil, nil, fmt.Errorf("deadlineShape: %#v", deadlines)
	}
	director, err := sim.NewDirector([]sim.PhaseDefinition{{
		Name: phase,
		ActiveRoles: []sim.Role{
			Role,
			PlayerRole,
		},
	}}, phase)
	if err != nil {
		return nil, nil, fmt.Errorf("directorCreate: %w", err)
	}
	outbox := &outbox{ability: ability}
	dispatcher := sim.NewDispatcher(sim.Ports{
		Graphics: outbox, Physics: outbox, Interactable: outbox, Loot: outbox,
		Presentation: outbox,
	})
	session, err := sim.NewSession(director, dispatcher)
	if err != nil {
		return nil, nil, fmt.Errorf("sessionCreate: %w", err)
	}
	err = session.RunProgram(
		ctx, director.Simulator().Scope(Role), program,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("programRun: %w", err)
	}
	events := outbox.drain()
	if len(events) == 0 {
		return nil, nil, errors.New("immediate events missing")
	}
	return &Run{
		session: session, outbox: outbox, deadlines: deadlines,
	}, events, nil
}

func (r *Run) Deadlines() []time.Duration {
	if r == nil {
		return nil
	}
	return append([]time.Duration(nil), r.deadlines...)
}

func (r *Run) Advance(ctx context.Context, deadline time.Duration) ([]sim.Event, error) {
	if r == nil || r.session == nil || r.outbox == nil || ctx == nil || r.isComplete {
		return nil, errors.New("invalid campaign interactable run")
	}
	now := r.session.Director().Simulator().Now()
	if deadline < now {
		return nil, fmt.Errorf("deadlineBeforeNow: %s < %s", deadline, now)
	}
	err := r.session.AdvanceBy(ctx, deadline-now)
	if err != nil {
		return nil, fmt.Errorf("sessionAdvance: %w", err)
	}
	events := r.outbox.drain()
	isDropDeadline := len(r.deadlines) == 3 && deadline == r.deadlines[1] && r.outbox.isDropObserved
	if len(events) == 0 && !isDropDeadline {
		return nil, fmt.Errorf("deadlineEmpty: %s", deadline)
	}
	if deadline == r.deadlines[len(r.deadlines)-1] {
		if !r.outbox.isDropObserved {
			return nil, errors.New("drop selector missing")
		}
		r.isComplete = true
		r.session.Director().Simulator().Stop()
	}
	return events, nil
}

func (r *Run) Stop() {
	if r == nil || r.session == nil {
		return
	}
	r.isComplete = true
	simulator := r.session.Director().Simulator()
	simulator.InvalidateRole(Role)
	simulator.Stop()
}

func (o *outbox) Use(
	_ context.Context, _ sim.EventMeta, intent sim.InteractableUseIntent,
) error {
	if intent.Role != Role || intent.UseCount != 1 {
		return fmt.Errorf("interactableIntent: %#v", intent)
	}
	return nil
}

func (o *outbox) Drop(
	_ context.Context, _ sim.EventMeta, intent sim.LootDropIntent,
) error {
	if intent.SourceRole != Role ||
		intent.PlayerRole != PlayerRole ||
		intent.IsLoot != o.ability.IsLootDrop ||
		intent.IsCrystal != o.ability.IsCrystalDrop || intent.IsOrb != o.ability.IsOrbDrop {
		return fmt.Errorf("dropIntent: %#v", intent)
	}
	o.isDropObserved = true
	return nil
}

func (o *outbox) SetGraphicsState(
	_ context.Context, meta sim.EventMeta, intent sim.GraphicsStateIntent,
) error {
	o.record(meta, intent)
	return nil
}

func (o *outbox) SetPhysicsState(
	_ context.Context, meta sim.EventMeta, intent sim.PhysicsStateIntent,
) error {
	o.record(meta, intent)
	return nil
}

func (o *outbox) SetVisibility(
	_ context.Context, meta sim.EventMeta, intent sim.VisibilityIntent,
) error {
	o.record(meta, intent)
	return nil
}

func (o *outbox) Animate(
	_ context.Context, meta sim.EventMeta, intent sim.AnimationIntent,
) error {
	o.record(meta, intent)
	return nil
}

func (o *outbox) ApplyEffect(
	_ context.Context, meta sim.EventMeta, intent sim.EffectIntent,
) error {
	o.record(meta, intent)
	return nil
}

func (o *outbox) StartCinematic(
	_ context.Context, meta sim.EventMeta, intent sim.CinematicIntent,
) error {
	o.record(meta, intent)
	return nil
}

func (o *outbox) ShowDialogue(
	_ context.Context, meta sim.EventMeta, intent sim.DialogueIntent,
) error {
	o.record(meta, intent)
	return nil
}

func (o *outbox) Notify(
	_ context.Context, meta sim.EventMeta, intent sim.ClientEventIntent,
) error {
	o.record(meta, intent)
	return nil
}

func (o *outbox) ResetAnimation(
	_ context.Context, meta sim.EventMeta, intent sim.AnimationResetIntent,
) error {
	o.record(meta, intent)
	return nil
}

func (o *outbox) record(meta sim.EventMeta, intent sim.Intent) {
	o.events = append(o.events, sim.Event{
		ID: meta.ID, Sequence: meta.ID.Sequence, At: meta.At,
		Phase: meta.Phase, Intent: intent, Provenance: meta.Provenance,
	})
}

func (o *outbox) drain() []sim.Event {
	events := o.events
	o.events = nil
	return events
}
