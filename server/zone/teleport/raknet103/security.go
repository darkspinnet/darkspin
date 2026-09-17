package raknet103

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneteleport "github.com/darkspinnet/darkspin/server/zone/teleport"
)

const securityTeleporterPhase sim.Phase = "security_teleporter_active"

type SecurityRun struct {
	session *sim.Session
	outbox  *securityTeleporterOutbox
	entry   sim.Program
}

type securityTeleporterOutbox struct {
	objectID          uint32
	position          sim.Position
	trigger           sim.TriggerVolumeIntent
	isTriggerActive   bool
	modifierRequest   *sim.ModifierRequestIntent
	packets           [][]byte
	appliedEventKinds map[sim.EventID]string
}

func (r *SecurityRun) IsReady() bool {
	return r != nil && r.session != nil && r.outbox != nil
}

func (r *SecurityRun) Trigger() (sim.TriggerVolumeIntent, bool) {
	if !r.IsReady() || !r.outbox.isTriggerActive {
		return sim.TriggerVolumeIntent{}, false
	}
	return r.outbox.trigger, true
}

func NewSecurityRun(
	program zoneteleport.Simulation, objectID uint32, position sim.Position,
) (*SecurityRun, [][]byte, error) {
	err := validateSecurityTeleporterProgram(program, position)
	if err != nil {
		return nil, nil, fmt.Errorf("programValidate: %w", err)
	}
	if objectID == 0 {
		return nil, nil, errors.New("object ID missing")
	}
	director, err := sim.NewDirector([]sim.PhaseDefinition{{
		Name: securityTeleporterPhase,
		ActiveRoles: []sim.Role{
			zoneteleport.SecurityRole,
			zoneteleport.EntrantRole,
		},
	}}, securityTeleporterPhase)
	if err != nil {
		return nil, nil, fmt.Errorf("directorCreate: %w", err)
	}
	outbox := &securityTeleporterOutbox{
		objectID: objectID, position: position, appliedEventKinds: make(map[sim.EventID]string),
	}
	dispatcher := sim.NewDispatcher(sim.Ports{
		Object: outbox, Modifier: outbox, Presentation: outbox,
	})
	session, err := sim.NewSession(director, dispatcher)
	if err != nil {
		return nil, nil, fmt.Errorf("sessionCreate: %w", err)
	}
	err = session.RunProgram(
		context.Background(), director.Simulator().Scope(zoneteleport.SecurityRole),
		program.Activation,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("activationRun: %w", err)
	}
	if !outbox.isTriggerActive {
		return nil, nil, errors.New("trigger not established")
	}
	return &SecurityRun{
		session: session, outbox: outbox, entry: program.Entry,
	}, outbox.drain(), nil
}

func validateSecurityTeleporterProgram(
	program zoneteleport.Simulation, position sim.Position,
) error {
	activation := program.Activation
	if activation.Provenance.LuaChunkID != zoneteleport.SecurityChunkID ||
		activation.Provenance.BytecodeSHA256 != zoneteleport.SecuritySHA256 ||
		activation.Provenance.FunctionName != "BossSecurityTeleporterPassive.Activate" {
		return fmt.Errorf("activationIdentity: %d/%s/%s", activation.Provenance.LuaChunkID,
			activation.Provenance.BytecodeSHA256, activation.Provenance.FunctionName)
	}
	if len(activation.Steps) != 2 {
		return fmt.Errorf("activationStepCount: %d", len(activation.Steps))
	}
	effectStep, isEffectStep := activation.Steps[0].(sim.EmitStep)
	effect, isEffect := effectStep.Intent.(sim.EffectIntent)
	if !isEffectStep || !isEffect || effect != (sim.EffectIntent{
		Role: zoneteleport.SecurityRole, EffectName: "zelem_boss_teleporter.ServerEventDef",
	}) {
		return fmt.Errorf("activationEffect: %#v", activation.Steps[0])
	}
	triggerStep, isTriggerStep := activation.Steps[1].(sim.EmitStep)
	trigger, isTrigger := triggerStep.Intent.(sim.TriggerVolumeIntent)
	if !isTriggerStep || !isTrigger || trigger != (sim.TriggerVolumeIntent{
		Role: zoneteleport.SecurityRole, Center: position, Radius: 2,
	}) {
		return fmt.Errorf("activationTrigger: %#v", activation.Steps[1])
	}
	entry := program.Entry
	if entry.Provenance.LuaChunkID != zoneteleport.SecurityChunkID ||
		entry.Provenance.BytecodeSHA256 != zoneteleport.SecuritySHA256 ||
		entry.Provenance.FunctionName != "BossSecurityTeleporterPassive.Triggerenter" {
		return fmt.Errorf("entryIdentity: %d/%s/%s", entry.Provenance.LuaChunkID,
			entry.Provenance.BytecodeSHA256, entry.Provenance.FunctionName)
	}
	if len(entry.Steps) != 1 {
		return fmt.Errorf("entryStepCount: %d", len(entry.Steps))
	}
	requestStep, isRequestStep := entry.Steps[0].(sim.EmitStep)
	request, isRequest := requestStep.Intent.(sim.ModifierRequestIntent)
	if !isRequestStep || !isRequest || request.TargetRole != zoneteleport.EntrantRole ||
		request.InitiatorRole != zoneteleport.SecurityRole ||
		request.ModifierGUID != zoneteleport.ModifierGUID ||
		request.Rank != 1 {
		return fmt.Errorf("entryRequest: %#v", entry.Steps[0])
	}
	return nil
}

// requestEntrant applies chunk 144's enter/stay policy. The caller supplies
// authoritative control ownership and whether the unique modifier already
// exists; non-player entrants and repeated stay notifications are ignored.
func (r *SecurityRun) RequestEntrant(
	ctx context.Context, callback sim.LuaTriggerCallback,
	isPlayerControlled bool, isOwnerPlayerControlled bool, isModifierActive bool,
) (*sim.ModifierRequestIntent, error) {
	if r == nil || r.session == nil || r.outbox == nil {
		return nil, errors.New("nil security teleporter run")
	}
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	if !r.outbox.isTriggerActive {
		return nil, errors.New("trigger inactive")
	}
	if !isPlayerControlled && !isOwnerPlayerControlled {
		return nil, nil
	}
	if callback == sim.LuaTriggerExit {
		return nil, nil
	}
	if callback != sim.LuaTriggerEnter && callback != sim.LuaTriggerStay {
		return nil, fmt.Errorf("callback rejected: %s", callback)
	}
	if callback == sim.LuaTriggerStay && isModifierActive {
		return nil, nil
	}
	r.outbox.modifierRequest = nil
	err := r.session.RunProgram(
		ctx, r.session.Director().Simulator().Scope(zoneteleport.EntrantRole), r.entry,
	)
	if err != nil {
		return nil, fmt.Errorf("entryRun: %w", err)
	}
	if r.outbox.modifierRequest == nil {
		return nil, errors.New("modifier request missing")
	}
	request := *r.outbox.modifierRequest
	return &request, nil
}

func (r *SecurityRun) Stop() {
	if r == nil || r.session == nil {
		return
	}
	r.session.Director().Simulator().Stop()
	r.outbox.isTriggerActive = false
}

func (o *securityTeleporterOutbox) CreateTriggerVolume(
	_ context.Context, meta sim.EventMeta, intent sim.TriggerVolumeIntent,
) error {
	if intent.Role != zoneteleport.SecurityRole ||
		intent.Center != o.position || intent.Radius != 2 {
		return fmt.Errorf("triggerIntent: %#v", intent)
	}
	isDuplicate, err := o.checkEvent(meta.ID, "triggerCreate")
	if err != nil {
		return fmt.Errorf("triggerEvent: %w", err)
	}
	if isDuplicate {
		return nil
	}
	o.trigger = intent
	o.isTriggerActive = true
	o.appliedEventKinds[meta.ID] = "triggerCreate"
	return nil
}

func (o *securityTeleporterOutbox) Request(
	_ context.Context, meta sim.EventMeta, intent sim.ModifierRequestIntent,
) error {
	if intent.TargetRole != zoneteleport.EntrantRole ||
		intent.InitiatorRole != zoneteleport.SecurityRole ||
		intent.ModifierGUID != zoneteleport.ModifierGUID || intent.Rank != 1 {
		return fmt.Errorf("modifierIntent: %#v", intent)
	}
	isDuplicate, err := o.checkEvent(meta.ID, "modifierRequest")
	if err != nil {
		return fmt.Errorf("modifierEvent: %w", err)
	}
	if isDuplicate {
		return nil
	}
	request := intent
	o.modifierRequest = &request
	o.appliedEventKinds[meta.ID] = "modifierRequest"
	return nil
}

func (o *securityTeleporterOutbox) ApplyEffect(
	_ context.Context, meta sim.EventMeta, intent sim.EffectIntent,
) error {
	if intent != (sim.EffectIntent{
		Role: zoneteleport.SecurityRole, EffectName: "zelem_boss_teleporter.ServerEventDef",
	}) {
		return fmt.Errorf("effectIntent: %#v", intent)
	}
	isDuplicate, err := o.checkEvent(meta.ID, "activeEffect")
	if err != nil {
		return fmt.Errorf("effectEvent: %w", err)
	}
	if isDuplicate {
		return nil
	}
	packet, err := raknet.MarshalApplication(raknet.ServerEventMessage{
		Asset: util.HashID(intent.EffectName), ObjectID: o.objectID,
		Position: raknet.Vector3{X: o.position.X, Y: o.position.Y, Z: o.position.Z},
	})
	if err != nil {
		return fmt.Errorf("effectMarshal: %w", err)
	}
	o.packets = append(o.packets, packet)
	o.appliedEventKinds[meta.ID] = "activeEffect"
	return nil
}

func (o *securityTeleporterOutbox) checkEvent(id sim.EventID, eventKind string) (bool, error) {
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

func (o *securityTeleporterOutbox) drain() [][]byte {
	packets := o.packets
	o.packets = nil
	return packets
}

func (*securityTeleporterOutbox) Spawn(context.Context, sim.EventMeta, sim.SpawnIntent) error {
	return errors.New("spawn unsupported")
}
func (*securityTeleporterOutbox) Despawn(context.Context, sim.EventMeta, sim.DespawnIntent) error {
	return errors.New("despawn unsupported")
}
func (*securityTeleporterOutbox) Move(context.Context, sim.EventMeta, sim.MovementIntent) error {
	return errors.New("move unsupported")
}
func (*securityTeleporterOutbox) Teleport(context.Context, sim.EventMeta, sim.TeleportIntent) error {
	return errors.New("teleport unsupported")
}
func (*securityTeleporterOutbox) DestroyTriggerVolume(
	context.Context, sim.EventMeta, sim.DestroyTriggerVolumeIntent,
) error {
	return errors.New("trigger destroy unsupported")
}
func (*securityTeleporterOutbox) SetVisibility(context.Context, sim.EventMeta, sim.VisibilityIntent) error {
	return errors.New("visibility unsupported")
}
func (*securityTeleporterOutbox) Animate(context.Context, sim.EventMeta, sim.AnimationIntent) error {
	return errors.New("animation unsupported")
}
func (*securityTeleporterOutbox) StartCinematic(context.Context, sim.EventMeta, sim.CinematicIntent) error {
	return errors.New("cinematic unsupported")
}
func (*securityTeleporterOutbox) ShowDialogue(context.Context, sim.EventMeta, sim.DialogueIntent) error {
	return errors.New("dialogue unsupported")
}
func (*securityTeleporterOutbox) Notify(context.Context, sim.EventMeta, sim.ClientEventIntent) error {
	return errors.New("client event unsupported")
}
func (*securityTeleporterOutbox) ResetAnimation(context.Context, sim.EventMeta, sim.AnimationResetIntent) error {
	return errors.New("animation reset unsupported")
}
