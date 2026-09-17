package raknet103

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneunlock "github.com/darkspinnet/darkspin/server/zone/unlock"
)

const abilityUnlockPhase sim.Phase = "unlockAbility"
const abilityUnlockPlayerRole = zoneunlock.PlayerRole
const creatureUnlockPhase sim.Phase = "unlockCreature"
const creatureUnlockPlayerRole = zoneunlock.PlayerRole

type CreatureUnlockRun struct {
	session *sim.Session
	outbox  *creatureUnlockOutbox
	cancel  raknet.CancelSchedule
}

type creatureUnlockOutbox struct {
	binding           game.GameplayBinding
	position          raknet.Vector3
	packets           [][]byte
	appliedEventKinds map[sim.EventID]string
}

func NewSecondCreatureUnlockRun(
	program sim.Program, binding game.GameplayBinding, position raknet.Vector3,
) (*CreatureUnlockRun, error) {
	err := validateSecondCreatureUnlockProgram(program)
	if err != nil {
		return nil, fmt.Errorf("programValidate: %w", err)
	}
	if binding.Slot > 255 {
		return nil, fmt.Errorf("bindingSlot: %d", binding.Slot)
	}
	director, err := sim.NewDirector([]sim.PhaseDefinition{{
		Name: creatureUnlockPhase, ActiveRoles: []sim.Role{creatureUnlockPlayerRole},
	}}, creatureUnlockPhase)
	if err != nil {
		return nil, fmt.Errorf("directorCreate: %w", err)
	}
	outbox := &creatureUnlockOutbox{
		binding: binding, position: position, appliedEventKinds: make(map[sim.EventID]string),
	}
	dispatcher := sim.NewDispatcher(sim.Ports{Squad: outbox, Presentation: outbox})
	session, err := sim.NewSession(director, dispatcher)
	if err != nil {
		return nil, fmt.Errorf("sessionCreate: %w", err)
	}
	err = session.RunProgram(context.Background(), director.Simulator().Scope(creatureUnlockPlayerRole), program)
	if err != nil {
		return nil, fmt.Errorf("programRun: %w", err)
	}
	if len(outbox.packets) != 0 {
		return nil, errors.New("unexpected immediate creature unlock")
	}
	return &CreatureUnlockRun{session: session, outbox: outbox}, nil
}

func validateSecondCreatureUnlockProgram(program sim.Program) error {
	if program.Provenance.LuaChunkID != zoneunlock.SecondCreatureChunkID ||
		program.Provenance.BytecodeSHA256 != zoneunlock.SecondCreatureSHA256 ||
		program.Provenance.FunctionName != zoneunlock.SecondCreatureCallback {
		return fmt.Errorf("identity: got %d/%s/%s", program.Provenance.LuaChunkID,
			program.Provenance.BytecodeSHA256, program.Provenance.FunctionName)
	}
	if len(program.Steps) != 3 {
		return fmt.Errorf("stepCount: %d", len(program.Steps))
	}
	wait, isWait := program.Steps[0].(sim.WaitStep)
	if !isWait || wait.Duration != zoneunlock.SecondCreatureDelay {
		return fmt.Errorf("wait: %#v", program.Steps[0])
	}
	emitUnlock, isEmitUnlock := program.Steps[1].(sim.EmitStep)
	if !isEmitUnlock {
		return fmt.Errorf("unlockStep: %T", program.Steps[1])
	}
	unlock, isUnlock := emitUnlock.Intent.(sim.UnlockIntent)
	if !isUnlock || unlock.PlayerRole != creatureUnlockPlayerRole ||
		unlock.UnlockKind != sim.UnlockSecondCreature || unlock.Count != 1 {
		return fmt.Errorf("unlock: %#v", emitUnlock.Intent)
	}
	emitClientEvent, isEmitClientEvent := program.Steps[2].(sim.EmitStep)
	if !isEmitClientEvent {
		return fmt.Errorf("clientEventStep: %T", program.Steps[2])
	}
	clientEvent, isClientEvent := emitClientEvent.Intent.(sim.ClientEventIntent)
	if !isClientEvent || clientEvent.EventKind != sim.ClientEventPlayerUnlockedSecondCreature ||
		clientEvent.EventID != sim.ClientEventPlayerUnlockedSecondCreatureID {
		return fmt.Errorf("clientEvent: %#v", emitClientEvent.Intent)
	}
	return nil
}

func (r *CreatureUnlockRun) Advance(ctx context.Context, deadline time.Duration) ([][]byte, error) {
	if r == nil || r.session == nil || r.outbox == nil {
		return nil, errors.New("nil creature unlock run")
	}
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	now := r.session.Director().Simulator().Now()
	if deadline < now {
		return nil, fmt.Errorf("deadlineBeforeNow: %s < %s", deadline, now)
	}
	err := r.session.AdvanceBy(ctx, deadline-now)
	if err != nil {
		return nil, fmt.Errorf("sessionAdvance: %w", err)
	}
	return r.outbox.drain(), nil
}

func (r *CreatureUnlockRun) Abort() {
	if r == nil || r.session == nil {
		return
	}
	r.session.Director().Simulator().Stop()
}

func (r *CreatureUnlockRun) SetCancel(cancel raknet.CancelSchedule) {
	if r != nil {
		r.cancel = cancel
	}
}

func (r *CreatureUnlockRun) Stop() {
	if r == nil {
		return
	}
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	r.Abort()
}

func (o *creatureUnlockOutbox) Unlock(
	_ context.Context, meta sim.EventMeta, intent sim.UnlockIntent,
) error {
	if intent.PlayerRole != creatureUnlockPlayerRole || intent.UnlockKind != sim.UnlockSecondCreature ||
		intent.Count != 1 {
		return fmt.Errorf("unlockIntent: %#v", intent)
	}
	isDuplicate, err := o.checkEvent(meta.ID, "unlockSecondCreature")
	if err != nil {
		return fmt.Errorf("unlockEvent: %w", err)
	}
	if isDuplicate {
		return nil
	}
	packets, err := MarshalSageUnlock(o.binding, o.position)
	if err != nil {
		return fmt.Errorf("unlockMarshal: %w", err)
	}
	o.packets = append(o.packets, packets...)
	o.appliedEventKinds[meta.ID] = "unlockSecondCreature"
	return nil
}

func (o *creatureUnlockOutbox) Notify(
	_ context.Context, meta sim.EventMeta, intent sim.ClientEventIntent,
) error {
	if intent.EventKind != sim.ClientEventPlayerUnlockedSecondCreature ||
		intent.EventID != sim.ClientEventPlayerUnlockedSecondCreatureID {
		return fmt.Errorf("clientEventIntent: %#v", intent)
	}
	isDuplicate, err := o.checkEvent(meta.ID, "notifySecondCreature")
	if err != nil {
		return fmt.Errorf("clientEvent: %w", err)
	}
	if isDuplicate {
		return nil
	}
	packet, err := raknet.MarshalApplication(raknet.ClientEventMessage{ClientEventID: intent.EventID})
	if err != nil {
		return fmt.Errorf("clientEventMarshal: %w", err)
	}
	o.packets = append(o.packets, packet)
	o.appliedEventKinds[meta.ID] = "notifySecondCreature"
	return nil
}

func (o *creatureUnlockOutbox) checkEvent(id sim.EventID, eventKind string) (bool, error) {
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

func (o *creatureUnlockOutbox) drain() [][]byte {
	packets := o.packets
	o.packets = nil
	return packets
}

func (*creatureUnlockOutbox) SetVisibility(context.Context, sim.EventMeta, sim.VisibilityIntent) error {
	return errors.New("visibility unsupported")
}

func (*creatureUnlockOutbox) Animate(context.Context, sim.EventMeta, sim.AnimationIntent) error {
	return errors.New("animation unsupported")
}

func (*creatureUnlockOutbox) ApplyEffect(context.Context, sim.EventMeta, sim.EffectIntent) error {
	return errors.New("effect unsupported")
}

func (*creatureUnlockOutbox) StartCinematic(context.Context, sim.EventMeta, sim.CinematicIntent) error {
	return errors.New("cinematic unsupported")
}

func (*creatureUnlockOutbox) ShowDialogue(context.Context, sim.EventMeta, sim.DialogueIntent) error {
	return errors.New("dialogue unsupported")
}

func (*creatureUnlockOutbox) ResetAnimation(context.Context, sim.EventMeta, sim.AnimationResetIntent) error {
	return errors.New("animation reset unsupported")
}

type AbilityUnlockRun struct {
	session *sim.Session
	outbox  *abilityUnlockOutbox
	cancel  raknet.CancelSchedule
}

type abilityUnlockOutbox struct {
	slot           uint8
	abilityCount   uint32
	packets        [][]byte
	appliedIntents map[sim.EventID]sim.UnlockIntent
}

func NewAbilityUnlockRun(program sim.Program, slot uint8) (*AbilityUnlockRun, error) {
	err := validateAbilityUnlockProgram(program)
	if err != nil {
		return nil, fmt.Errorf("programValidate: %w", err)
	}
	director, err := sim.NewDirector([]sim.PhaseDefinition{{
		Name: abilityUnlockPhase, ActiveRoles: []sim.Role{abilityUnlockPlayerRole},
	}}, abilityUnlockPhase)
	if err != nil {
		return nil, fmt.Errorf("directorCreate: %w", err)
	}
	outbox := &abilityUnlockOutbox{
		slot: slot, abilityCount: zoneunlock.TutorialInitialBoundary,
		appliedIntents: make(map[sim.EventID]sim.UnlockIntent),
	}
	dispatcher := sim.NewDispatcher(sim.Ports{Squad: outbox})
	session, err := sim.NewSession(director, dispatcher)
	if err != nil {
		return nil, fmt.Errorf("sessionCreate: %w", err)
	}
	scope := director.Simulator().Scope(abilityUnlockPlayerRole)
	err = session.RunProgram(context.Background(), scope, program)
	if err != nil {
		return nil, fmt.Errorf("programRun: %w", err)
	}
	if len(outbox.packets) != 0 {
		return nil, errors.New("unexpected immediate ability unlock")
	}
	return &AbilityUnlockRun{session: session, outbox: outbox}, nil
}

func validateAbilityUnlockProgram(program sim.Program) error {
	if program.Provenance.LuaChunkID != zoneunlock.AbilitySecondChunkID ||
		program.Provenance.BytecodeSHA256 != zoneunlock.AbilitySecondSHA256 ||
		program.Provenance.FunctionName != zoneunlock.AbilitySecondCallback {
		return fmt.Errorf("identity: got %d/%s/%s", program.Provenance.LuaChunkID,
			program.Provenance.BytecodeSHA256, program.Provenance.FunctionName)
	}
	if len(program.Steps) != 3 {
		return fmt.Errorf("stepCount: %d", len(program.Steps))
	}
	wait, isWait := program.Steps[0].(sim.WaitStep)
	if !isWait || wait.Duration != zoneunlock.AbilitySecondDelay {
		return fmt.Errorf("wait: %#v", program.Steps[0])
	}
	for index := 1; index < len(program.Steps); index++ {
		emit, isEmit := program.Steps[index].(sim.EmitStep)
		if !isEmit {
			return fmt.Errorf("emit[%d]: %T", index, program.Steps[index])
		}
		unlock, isUnlock := emit.Intent.(sim.UnlockIntent)
		if !isUnlock || unlock.PlayerRole != abilityUnlockPlayerRole ||
			unlock.UnlockKind != sim.UnlockNextAbility || unlock.Count != 1 {
			return fmt.Errorf("unlock[%d]: %#v", index, emit.Intent)
		}
	}
	return nil
}

func (r *AbilityUnlockRun) Advance(ctx context.Context, deadline time.Duration) ([][]byte, error) {
	if r == nil || r.session == nil || r.outbox == nil {
		return nil, errors.New("nil ability unlock run")
	}
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	now := r.session.Director().Simulator().Now()
	if deadline < now {
		return nil, fmt.Errorf("deadlineBeforeNow: %s < %s", deadline, now)
	}
	err := r.session.AdvanceBy(ctx, deadline-now)
	if err != nil {
		return nil, fmt.Errorf("sessionAdvance: %w", err)
	}
	return r.outbox.drain(), nil
}

func (r *AbilityUnlockRun) Stop() {
	if r == nil {
		return
	}
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	r.Abort()
}

func (r *AbilityUnlockRun) Abort() {
	if r == nil || r.session == nil {
		return
	}
	r.session.Director().Simulator().Stop()
}

func (r *AbilityUnlockRun) SetCancel(cancel raknet.CancelSchedule) {
	if r != nil {
		r.cancel = cancel
	}
}

func (r *AbilityUnlockRun) ClearCancel() {
	if r != nil {
		r.cancel = nil
	}
}

func (o *abilityUnlockOutbox) Unlock(_ context.Context, meta sim.EventMeta, intent sim.UnlockIntent) error {
	if meta.ID == (sim.EventID{}) {
		return errors.New("event ID missing")
	}
	if intent.PlayerRole != abilityUnlockPlayerRole {
		return fmt.Errorf("playerRole: %s", intent.PlayerRole)
	}
	if intent.UnlockKind != sim.UnlockNextAbility {
		return fmt.Errorf("unlockKind: %s", intent.UnlockKind)
	}
	if intent.Count != 1 {
		return fmt.Errorf("unlockCount: %d", intent.Count)
	}
	previous, isApplied := o.appliedIntents[meta.ID]
	if isApplied {
		if previous != intent {
			return fmt.Errorf("eventConflict: %#v", meta.ID)
		}
		return nil
	}
	if o.abilityCount < zoneunlock.TutorialAbilityBoundary {
		nextAbilityCount := o.abilityCount + 1
		packet, err := raknet.MarshalApplication(raknet.LabsPlayerAbilityCountMessage{
			Slot: o.slot, AbilityCount: nextAbilityCount,
		})
		if err != nil {
			return fmt.Errorf("abilityMarshal: %w", err)
		}
		o.abilityCount = nextAbilityCount
		o.packets = append(o.packets, packet)
	}
	if o.appliedIntents == nil {
		o.appliedIntents = make(map[sim.EventID]sim.UnlockIntent)
	}
	o.appliedIntents[meta.ID] = intent
	return nil
}

func (o *abilityUnlockOutbox) drain() [][]byte {
	packets := o.packets
	o.packets = nil
	return packets
}
