package spawn

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/sim"
)

const ChunkID int64 = 655
const SHA256 = "0f1ff2f4dfaf35491d91b31c894061af3e3cc293fbb05f97ae656e75ef8c47f1"
const Duration = 500 * time.Millisecond
const NPCRole sim.Role = "npc"
const phase sim.Phase = "spawnModifier"

type Run struct {
	session       *sim.Session
	outbox        *outbox
	isImmobilized bool
}

type outbox struct {
	events        []sim.Event
	isImmobilized bool
}

func New(program sim.Program) (*Run, []sim.Event, error) {
	err := validateProgram(program)
	if err != nil {
		return nil, nil, fmt.Errorf("programValidate: %w", err)
	}
	modifierOutbox := &outbox{}
	director, err := sim.NewDirector([]sim.PhaseDefinition{{
		Name: phase, ActiveRoles: []sim.Role{NPCRole},
	}}, phase)
	if err != nil {
		return nil, nil, fmt.Errorf("directorCreate: %w", err)
	}
	dispatcher := sim.NewDispatcher(sim.Ports{
		Locomotion: modifierOutbox, Attribute: modifierOutbox, Presentation: modifierOutbox,
	})
	session, err := sim.NewSession(director, dispatcher)
	if err != nil {
		return nil, nil, fmt.Errorf("sessionCreate: %w", err)
	}
	err = session.RunProgram(
		context.Background(), director.Simulator().Scope(NPCRole), program,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("programRun: %w", err)
	}
	if !modifierOutbox.isImmobilized {
		return nil, nil, errors.New("activation did not immobilize npc")
	}
	events := modifierOutbox.drain()
	if len(events) == 0 {
		return nil, nil, errors.New("immediate events missing")
	}
	run := &Run{session: session, outbox: modifierOutbox, isImmobilized: true}
	return run, events, nil
}

func validateProgram(program sim.Program) error {
	if program.Provenance.LuaChunkID != ChunkID ||
		program.Provenance.BytecodeSHA256 != SHA256 {
		return fmt.Errorf(
			"identity: got %d/%s",
			program.Provenance.LuaChunkID,
			program.Provenance.BytecodeSHA256,
		)
	}
	if len(program.Steps) != 7 {
		return fmt.Errorf("stepCount: %d", len(program.Steps))
	}
	expectedIntent := []sim.Intent{
		sim.LocomotionStopIntent{Role: NPCRole},
		sim.AttributeModifierIntent{
			Role: NPCRole, AttributeKind: sim.AttributeImmobilized, Amount: 1,
		},
		sim.EffectIntent{Role: NPCRole, EffectName: "generic_spawn.ServerEventDef"},
		sim.AnimationIntent{Role: NPCRole, AnimationName: "horde_beam_in"},
		nil,
		sim.EffectIntent{
			Role: NPCRole, EffectName: "generic_spawn.ServerEventDef", IsStopped: true,
		},
		sim.AnimationResetIntent{Role: NPCRole},
	}
	for index, intent := range expectedIntent {
		if intent == nil {
			continue
		}
		emit, isEmit := program.Steps[index].(sim.EmitStep)
		if !isEmit || emit.Intent != intent {
			return fmt.Errorf("step[%d]: %#v", index, program.Steps[index])
		}
	}
	wait, isWait := program.Steps[4].(sim.WaitStep)
	if !isWait || wait.Duration != Duration {
		return fmt.Errorf("wait: %#v", program.Steps[4])
	}
	return nil
}

func (r *Run) Advance(ctx context.Context, deadline time.Duration) ([]sim.Event, error) {
	if r == nil || r.session == nil || r.outbox == nil || ctx == nil {
		return nil, errors.New("invalid spawn modifier run")
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
	if len(events) == 0 {
		return nil, fmt.Errorf("deadlineEmpty: %s", deadline)
	}
	if deadline == Duration {
		r.isImmobilized = false
		r.outbox.isImmobilized = false
	}
	return events, nil
}

func (r *Run) Stop() {
	if r == nil || r.session == nil {
		return
	}
	r.isImmobilized = false
	if r.outbox != nil {
		r.outbox.isImmobilized = false
	}
	simulator := r.session.Director().Simulator()
	simulator.InvalidateRole(NPCRole)
	simulator.Stop()
}

func (r *Run) IsImmobilized() bool {
	return r != nil && r.isImmobilized
}

func (o *outbox) Stop(
	_ context.Context, meta sim.EventMeta, intent sim.LocomotionStopIntent,
) error {
	if intent.Role != NPCRole {
		return fmt.Errorf("locomotionIntent: %#v", intent)
	}
	o.append(meta, intent)
	return nil
}

func (o *outbox) ApplyModifier(
	_ context.Context, meta sim.EventMeta, intent sim.AttributeModifierIntent,
) error {
	if intent.Role != NPCRole ||
		intent.AttributeKind != sim.AttributeImmobilized || intent.Amount != 1 {
		return fmt.Errorf("attributeIntent: %#v", intent)
	}
	o.isImmobilized = true
	o.append(meta, intent)
	return nil
}

func (o *outbox) Animate(
	_ context.Context, meta sim.EventMeta, intent sim.AnimationIntent,
) error {
	o.append(meta, intent)
	return nil
}

func (o *outbox) ApplyEffect(
	_ context.Context, meta sim.EventMeta, intent sim.EffectIntent,
) error {
	o.append(meta, intent)
	return nil
}

func (o *outbox) ResetAnimation(
	_ context.Context, meta sim.EventMeta, intent sim.AnimationResetIntent,
) error {
	o.append(meta, intent)
	return nil
}

func (o *outbox) SetVisibility(
	_ context.Context, meta sim.EventMeta, intent sim.VisibilityIntent,
) error {
	return fmt.Errorf("visibilityIntent: %#v", intent)
}

func (o *outbox) StartCinematic(
	_ context.Context, meta sim.EventMeta, intent sim.CinematicIntent,
) error {
	return fmt.Errorf("cinematicIntent: %#v", intent)
}

func (o *outbox) ShowDialogue(
	_ context.Context, meta sim.EventMeta, intent sim.DialogueIntent,
) error {
	return fmt.Errorf("dialogueIntent: %#v", intent)
}

func (o *outbox) Notify(
	_ context.Context, meta sim.EventMeta, intent sim.ClientEventIntent,
) error {
	return fmt.Errorf("clientEventIntent: %#v", intent)
}

func (o *outbox) append(meta sim.EventMeta, intent sim.Intent) {
	o.events = append(o.events, sim.Event{
		ID: meta.ID, At: meta.At, Phase: meta.Phase,
		Provenance: meta.Provenance, Intent: intent,
	})
}

func (o *outbox) drain() []sim.Event {
	events := o.events
	o.events = nil
	return events
}
