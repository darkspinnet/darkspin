package raknet103

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneteleport "github.com/darkspinnet/darkspin/server/zone/teleport"
)

const teleporterPhase sim.Phase = "teleport"

type Run struct {
	session *sim.Session
	outbox  *teleporterOutbox
	cancel  raknet.CancelSchedule
}

type teleporterOutbox struct {
	objectID          uint32
	position          sim.Position
	sourceTime        uint64
	packets           [][]byte
	isStopped         bool
	isImmobilized     bool
	appliedEventKinds map[sim.EventID]string
}

func (e *Run) IsAuthorityEstablished() bool {
	return e != nil && e.outbox != nil && e.outbox.isStopped && e.outbox.isImmobilized
}

func (e *Run) Position() sim.Position {
	if e == nil || e.outbox == nil {
		return sim.Position{}
	}
	return e.outbox.position
}

func (e *Run) ObjectID() uint32 {
	if e == nil || e.outbox == nil {
		return 0
	}
	return e.outbox.objectID
}

func NewRun(
	program sim.Program, objectID uint32, position sim.Position, sourceTime uint64,
) (*Run, [][]byte, error) {
	err := ValidateProgram(program)
	if err != nil {
		return nil, nil, fmt.Errorf("programValidate: %w", err)
	}
	if objectID == 0 {
		return nil, nil, errors.New("object ID missing")
	}
	director, err := sim.NewDirector([]sim.PhaseDefinition{{
		Name: teleporterPhase, ActiveRoles: []sim.Role{zoneteleport.EntrantRole},
	}}, teleporterPhase)
	if err != nil {
		return nil, nil, fmt.Errorf("directorCreate: %w", err)
	}
	outbox := &teleporterOutbox{
		objectID: objectID, position: position, sourceTime: sourceTime,
		appliedEventKinds: make(map[sim.EventID]string),
	}
	dispatcher := sim.NewDispatcher(sim.Ports{
		Object: outbox, Locomotion: outbox, Attribute: outbox, Presentation: outbox,
	})
	session, err := sim.NewSession(director, dispatcher)
	if err != nil {
		return nil, nil, fmt.Errorf("sessionCreate: %w", err)
	}
	err = session.RunProgram(
		context.Background(), director.Simulator().Scope(zoneteleport.EntrantRole), program,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("programRun: %w", err)
	}
	if !outbox.isStopped || !outbox.isImmobilized {
		return nil, nil, errors.New("authority not established")
	}
	return &Run{session: session, outbox: outbox}, outbox.drain(), nil
}

func ValidateProgram(program sim.Program) error {
	if program.Provenance.LuaChunkID != zoneteleport.ModifierChunkID ||
		program.Provenance.BytecodeSHA256 != zoneteleport.ModifierSHA256 ||
		program.Provenance.FunctionName != "TeleporterModifier.Activate" {
		return fmt.Errorf("identity: %d/%s/%s", program.Provenance.LuaChunkID,
			program.Provenance.BytecodeSHA256, program.Provenance.FunctionName)
	}
	if len(program.Steps) != 8 {
		return fmt.Errorf("stepCount: %d", len(program.Steps))
	}
	checks := []struct {
		index int
		kind  string
	}{
		{index: 0, kind: "stop"},
		{index: 1, kind: "immobilize"},
		{index: 2, kind: "teleportOut"},
		{index: 3, kind: "waitOut"},
		{index: 4, kind: "teleport"},
		{index: 5, kind: "waitZero"},
		{index: 6, kind: "teleportIn"},
		{index: 7, kind: "waitIn"},
	}
	for _, check := range checks {
		err := validateTeleporterStep(program.Steps[check.index], check.kind)
		if err != nil {
			return fmt.Errorf("step[%d]: %w", check.index, err)
		}
	}
	return nil
}

func validateTeleporterStep(step sim.Step, kind string) error {
	switch kind {
	case "stop":
		emit, isEmit := step.(sim.EmitStep)
		stop, isStop := emit.Intent.(sim.LocomotionStopIntent)
		if !isEmit || !isStop || stop.Role != zoneteleport.EntrantRole {
			return fmt.Errorf("stop: %#v", step)
		}
	case "immobilize":
		emit, isEmit := step.(sim.EmitStep)
		want := sim.AttributeModifierIntent{
			Role: zoneteleport.EntrantRole, AttributeKind: sim.AttributeImmobilized, Amount: 1,
		}
		if !isEmit || emit.Intent != want {
			return fmt.Errorf("immobilize: %#v", step)
		}
	case "teleportOut", "teleportIn":
		emit, isEmit := step.(sim.EmitStep)
		animation, isAnimation := emit.Intent.(sim.AnimationIntent)
		want := "character_teleport_out"
		if kind == "teleportIn" {
			want = "character_teleport_in"
		}
		if !isEmit || !isAnimation || animation.Role != zoneteleport.EntrantRole ||
			animation.AnimationName != want {
			return fmt.Errorf("animation: %#v", step)
		}
	case "waitOut", "waitZero", "waitIn":
		wait, isWait := step.(sim.WaitStep)
		want := time.Duration(0)
		if kind != "waitZero" {
			want = 500 * time.Millisecond
		}
		if !isWait || wait.Duration != want {
			return fmt.Errorf("wait: %#v", step)
		}
	case "teleport":
		emit, isEmit := step.(sim.EmitStep)
		teleport, isTeleport := emit.Intent.(sim.TeleportIntent)
		if !isEmit || !isTeleport || teleport.Role != zoneteleport.EntrantRole {
			return fmt.Errorf("teleport: %#v", step)
		}
	default:
		return fmt.Errorf("unknown kind: %s", kind)
	}
	return nil
}

func (e *Run) Advance(ctx context.Context, deadline time.Duration) ([][]byte, error) {
	if e == nil || e.session == nil || e.outbox == nil {
		return nil, errors.New("nil teleporter run")
	}
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	now := e.session.Director().Simulator().Now()
	if deadline < now {
		return nil, fmt.Errorf("deadlineBeforeNow: %s < %s", deadline, now)
	}
	err := e.session.AdvanceBy(ctx, deadline-now)
	if err != nil {
		return nil, fmt.Errorf("sessionAdvance: %w", err)
	}
	return e.outbox.drain(), nil
}

type advanceStep struct {
	run      *Run
	deadline time.Duration
}

func (e advanceStep) produce() ([][]byte, error) {
	packets, err := e.run.Advance(context.Background(), e.deadline)
	if err != nil {
		return nil, fmt.Errorf("teleporterAdvance[%s]: %w", e.deadline, err)
	}
	return packets, nil
}

func (e *Run) ScheduledProducers() ([]raknet.ScheduledPacketProducer, error) {
	if e == nil || e.session == nil || e.outbox == nil {
		return nil, errors.New("nil teleporter run")
	}
	producers := make([]raknet.ScheduledPacketProducer, 0, len(zoneteleport.Deadline))
	for _, deadline := range zoneteleport.Deadline {
		step := advanceStep{run: e, deadline: deadline}
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: deadline, Produce: step.produce,
		})
	}
	return producers, nil
}

func (e *Run) Stop() {
	if e == nil {
		return
	}
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	e.Abort()
}

func (e *Run) SetCancel(cancel raknet.CancelSchedule) {
	if e != nil {
		e.cancel = cancel
	}
}

func (e *Run) Abort() {
	if e == nil || e.session == nil {
		return
	}
	e.session.Director().Simulator().Stop()
}

func (o *teleporterOutbox) Stop(_ context.Context, meta sim.EventMeta, intent sim.LocomotionStopIntent) error {
	if intent.Role != zoneteleport.EntrantRole {
		return fmt.Errorf("stopIntent: %#v", intent)
	}
	isDuplicate, err := o.checkEvent(meta.ID, "locomotionStop")
	if err != nil {
		return fmt.Errorf("stopEvent: %w", err)
	}
	if isDuplicate {
		return nil
	}
	o.isStopped = true
	o.appliedEventKinds[meta.ID] = "locomotionStop"
	return nil
}

func (o *teleporterOutbox) ApplyModifier(
	_ context.Context, meta sim.EventMeta, intent sim.AttributeModifierIntent,
) error {
	if intent.Role != zoneteleport.EntrantRole ||
		intent.AttributeKind != sim.AttributeImmobilized || intent.Amount != 1 {
		return fmt.Errorf("attributeIntent: %#v", intent)
	}
	isDuplicate, err := o.checkEvent(meta.ID, "immobilized")
	if err != nil {
		return fmt.Errorf("attributeEvent: %w", err)
	}
	if isDuplicate {
		return nil
	}
	o.isImmobilized = true
	o.appliedEventKinds[meta.ID] = "immobilized"
	return nil
}

func (o *teleporterOutbox) Teleport(_ context.Context, meta sim.EventMeta, intent sim.TeleportIntent) error {
	if intent.Role != zoneteleport.EntrantRole {
		return fmt.Errorf("teleportIntent: %#v", intent)
	}
	isDuplicate, err := o.checkEvent(meta.ID, "teleport")
	if err != nil {
		return fmt.Errorf("teleportEvent: %w", err)
	}
	if isDuplicate {
		return nil
	}
	packet, err := raknet.MarshalApplication(raknet.ObjectTeleportMessage{
		ObjectID: o.objectID,
		Position: raknet.Vector3{X: intent.Destination.X, Y: intent.Destination.Y, Z: intent.Destination.Z},
	})
	if err != nil {
		return fmt.Errorf("teleportMarshal: %w", err)
	}
	o.position = intent.Destination
	o.packets = append(o.packets, packet)
	o.appliedEventKinds[meta.ID] = "teleport"
	return nil
}

func (o *teleporterOutbox) Animate(_ context.Context, meta sim.EventMeta, intent sim.AnimationIntent) error {
	if intent.Role != zoneteleport.EntrantRole || intent.AnimationName == "" {
		return fmt.Errorf("animationIntent: %#v", intent)
	}
	eventKind := "animation:" + intent.AnimationName
	isDuplicate, err := o.checkEvent(meta.ID, eventKind)
	if err != nil {
		return fmt.Errorf("animationEvent: %w", err)
	}
	if isDuplicate {
		return nil
	}
	timestamp := o.sourceTime + uint64(meta.At/time.Millisecond)
	packet, err := raknet.MarshalApplication(raknet.SetAnimationStateMessage{
		ObjectID: o.objectID, State: util.HashID(intent.AnimationName), Timestamp: timestamp, Scale: 1,
	})
	if err != nil {
		return fmt.Errorf("animationMarshal: %w", err)
	}
	o.packets = append(o.packets, packet)
	o.appliedEventKinds[meta.ID] = eventKind
	return nil
}

func (o *teleporterOutbox) checkEvent(id sim.EventID, eventKind string) (bool, error) {
	if id == (sim.EventID{}) {
		return false, errors.New("event ID missing")
	}
	previous, isFound := o.appliedEventKinds[id]
	if !isFound {
		return false, nil
	}
	if previous != eventKind {
		return false, fmt.Errorf("eventConflict: %#v", id)
	}
	return true, nil
}

func (o *teleporterOutbox) drain() [][]byte {
	packets := o.packets
	o.packets = nil
	return packets
}

func (*teleporterOutbox) Spawn(context.Context, sim.EventMeta, sim.SpawnIntent) error {
	return errors.New("spawn unsupported")
}
func (*teleporterOutbox) Despawn(context.Context, sim.EventMeta, sim.DespawnIntent) error {
	return errors.New("despawn unsupported")
}
func (*teleporterOutbox) Move(context.Context, sim.EventMeta, sim.MovementIntent) error {
	return errors.New("move unsupported")
}
func (*teleporterOutbox) CreateTriggerVolume(context.Context, sim.EventMeta, sim.TriggerVolumeIntent) error {
	return errors.New("trigger create unsupported")
}
func (*teleporterOutbox) DestroyTriggerVolume(
	context.Context, sim.EventMeta, sim.DestroyTriggerVolumeIntent,
) error {
	return errors.New("trigger destroy unsupported")
}
func (*teleporterOutbox) SetVisibility(context.Context, sim.EventMeta, sim.VisibilityIntent) error {
	return errors.New("visibility unsupported")
}
func (*teleporterOutbox) ApplyEffect(context.Context, sim.EventMeta, sim.EffectIntent) error {
	return errors.New("effect unsupported")
}
func (*teleporterOutbox) StartCinematic(context.Context, sim.EventMeta, sim.CinematicIntent) error {
	return errors.New("cinematic unsupported")
}
func (*teleporterOutbox) ShowDialogue(context.Context, sim.EventMeta, sim.DialogueIntent) error {
	return errors.New("dialogue unsupported")
}
func (*teleporterOutbox) Notify(context.Context, sim.EventMeta, sim.ClientEventIntent) error {
	return errors.New("client event unsupported")
}
func (*teleporterOutbox) ResetAnimation(context.Context, sim.EventMeta, sim.AnimationResetIntent) error {
	return errors.New("animation reset unsupported")
}
