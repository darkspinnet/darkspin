package raknet103

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneunlock "github.com/darkspinnet/darkspin/server/zone/unlock"
)

const PlayerRole = zoneunlock.PlayerRole
const playerOverdriveMaximumEnergy = float32(100)
const playerFeatureUnlockPhase sim.Phase = "playerFeatureUnlock"

type Run struct {
	session *sim.Session
	outbox  *playerFeatureUnlockOutbox
	cancel  raknet.CancelSchedule
}

// playerFeatureUnlockOutbox translates the recovered UnlockOverdrive and
// UnlockCrystals authority intents into their sparse build-103 reflections.
// It does not infer route ownership or persist mission-local lock state.
type playerFeatureUnlockOutbox struct {
	slot           uint8
	abilityCount   uint32
	packets        [][]byte
	appliedIntents map[sim.EventID]sim.UnlockIntent
	crystalDrops   []sim.CrystalDropIntent
	droppedIntents map[sim.EventID]sim.CrystalDropIntent
}

type CatalystRun struct {
	session *sim.Session
	outbox  *playerFeatureUnlockOutbox
	cancel  raknet.CancelSchedule
}

type CatalystBatch struct {
	Packets      [][]byte
	CrystalDrops []sim.CrystalDropIntent
}

func (b CatalystBatch) BuildCrystalWorldRequest(
	input sim.CrystalDropInput,
) (sim.CrystalDropWorldRequest, error) {
	if len(b.CrystalDrops) != 1 {
		return sim.CrystalDropWorldRequest{}, fmt.Errorf("crystalDropCount: %d", len(b.CrystalDrops))
	}
	if b.CrystalDrops[0].PlayerRole != input.PlayerRole {
		return sim.CrystalDropWorldRequest{}, fmt.Errorf(
			"crystalDropRole: got %q, want %q", input.PlayerRole, b.CrystalDrops[0].PlayerRole,
		)
	}
	request, err := sim.BuildCrystalDropWorldRequest(input)
	if err != nil {
		return sim.CrystalDropWorldRequest{}, fmt.Errorf("crystalDropBuild: %w", err)
	}
	return request, nil
}

func newPlayerFeatureUnlockOutbox(slot uint8, abilityCount uint32) *playerFeatureUnlockOutbox {
	return &playerFeatureUnlockOutbox{
		slot: slot, abilityCount: abilityCount, appliedIntents: make(map[sim.EventID]sim.UnlockIntent),
		droppedIntents: make(map[sim.EventID]sim.CrystalDropIntent),
	}
}

func NewCatalystRun(program sim.Program, slot uint8) (*CatalystRun, error) {
	err := validateCatalystUnlockProgram(program)
	if err != nil {
		return nil, fmt.Errorf("programValidate: %w", err)
	}
	director, err := sim.NewDirector([]sim.PhaseDefinition{{
		Name: playerFeatureUnlockPhase, ActiveRoles: []sim.Role{PlayerRole},
	}}, playerFeatureUnlockPhase)
	if err != nil {
		return nil, fmt.Errorf("directorCreate: %w", err)
	}
	outbox := newPlayerFeatureUnlockOutbox(slot, 0)
	session, err := sim.NewSession(director, sim.NewDispatcher(sim.Ports{
		Squad: outbox, Pickup: outbox,
	}))
	if err != nil {
		return nil, fmt.Errorf("sessionCreate: %w", err)
	}
	err = session.RunProgram(context.Background(), director.Simulator().Scope(PlayerRole), program)
	if err != nil {
		return nil, fmt.Errorf("programRun: %w", err)
	}
	if len(outbox.packets) != 0 || len(outbox.crystalDrops) != 0 {
		return nil, errors.New("unexpected immediate catalyst unlock")
	}
	return &CatalystRun{session: session, outbox: outbox}, nil
}

func validateCatalystUnlockProgram(program sim.Program) error {
	if program.Provenance.LuaChunkID != zoneunlock.CatalystChunkID ||
		program.Provenance.BytecodeSHA256 != zoneunlock.CatalystSHA256 ||
		program.Provenance.FunctionName != "nTutorial_CatalystUnlock.main" {
		return fmt.Errorf("identity: got %d/%s/%s", program.Provenance.LuaChunkID,
			program.Provenance.BytecodeSHA256, program.Provenance.FunctionName)
	}
	wantWait := []time.Duration{
		2 * time.Second, 4 * time.Second, 4 * time.Second,
		4 * time.Second, 4 * time.Second, 2 * time.Second,
	}
	if len(program.Steps) != len(wantWait)+2 {
		return fmt.Errorf("stepCount: %d", len(program.Steps))
	}
	waitIndex := 0
	for index, step := range program.Steps {
		emit, isEmit := step.(sim.EmitStep)
		if isEmit {
			if index == 2 {
				unlock, isUnlock := emit.Intent.(sim.UnlockIntent)
				if !isUnlock || unlock != (sim.UnlockIntent{
					PlayerRole: PlayerRole, UnlockKind: sim.UnlockCrystals, Count: 1,
				}) {
					return fmt.Errorf("unlock[%d]: %#v", index, emit.Intent)
				}
				continue
			}
			drop, isDrop := emit.Intent.(sim.CrystalDropIntent)
			if index != 3 || !isDrop || drop != (sim.CrystalDropIntent{PlayerRole: PlayerRole}) {
				return fmt.Errorf("drop[%d]: %#v", index, emit.Intent)
			}
			continue
		}
		wait, isWait := step.(sim.WaitStep)
		if !isWait || waitIndex >= len(wantWait) || wait.Duration != wantWait[waitIndex] {
			return fmt.Errorf("wait[%d]: %#v", index, step)
		}
		waitIndex++
	}
	if waitIndex != len(wantWait) {
		return fmt.Errorf("waitCount: %d", waitIndex)
	}
	return nil
}

func NewOverdriveRun(program sim.Program, slot uint8) (*Run, error) {
	err := validateOverdriveUnlockProgram(program)
	if err != nil {
		return nil, fmt.Errorf("programValidate: %w", err)
	}
	director, err := sim.NewDirector([]sim.PhaseDefinition{{
		Name: playerFeatureUnlockPhase, ActiveRoles: []sim.Role{PlayerRole},
	}}, playerFeatureUnlockPhase)
	if err != nil {
		return nil, fmt.Errorf("directorCreate: %w", err)
	}
	outbox := newPlayerFeatureUnlockOutbox(slot, 0)
	session, err := sim.NewSession(director, sim.NewDispatcher(sim.Ports{Squad: outbox}))
	if err != nil {
		return nil, fmt.Errorf("sessionCreate: %w", err)
	}
	err = session.RunProgram(context.Background(), director.Simulator().Scope(PlayerRole), program)
	if err != nil {
		return nil, fmt.Errorf("programRun: %w", err)
	}
	if len(outbox.packets) != 0 {
		return nil, errors.New("unexpected immediate player feature unlock")
	}
	return &Run{session: session, outbox: outbox}, nil
}

func NewSoloSupportRun(
	program sim.Program, slot uint8, abilityCount uint32,
) (*Run, error) {
	err := validateSoloSupportUnlockProgram(program)
	if err != nil {
		return nil, fmt.Errorf("programValidate: %w", err)
	}
	director, err := sim.NewDirector([]sim.PhaseDefinition{{
		Name: playerFeatureUnlockPhase, ActiveRoles: []sim.Role{PlayerRole},
	}}, playerFeatureUnlockPhase)
	if err != nil {
		return nil, fmt.Errorf("directorCreate: %w", err)
	}
	outbox := newPlayerFeatureUnlockOutbox(slot, abilityCount)
	session, err := sim.NewSession(director, sim.NewDispatcher(sim.Ports{Squad: outbox}))
	if err != nil {
		return nil, fmt.Errorf("sessionCreate: %w", err)
	}
	err = session.RunProgram(context.Background(), director.Simulator().Scope(PlayerRole), program)
	if err != nil {
		return nil, fmt.Errorf("programRun: %w", err)
	}
	if len(outbox.packets) != 0 {
		return nil, errors.New("unexpected immediate solo support unlock")
	}
	return &Run{session: session, outbox: outbox}, nil
}

func NewSupportRun(
	program sim.Program, slot uint8, abilityCount uint32,
) (*Run, error) {
	err := validateSupportUnlockProgram(program)
	if err != nil {
		return nil, fmt.Errorf("programValidate: %w", err)
	}
	director, err := sim.NewDirector([]sim.PhaseDefinition{{
		Name: playerFeatureUnlockPhase, ActiveRoles: []sim.Role{PlayerRole},
	}}, playerFeatureUnlockPhase)
	if err != nil {
		return nil, fmt.Errorf("directorCreate: %w", err)
	}
	outbox := newPlayerFeatureUnlockOutbox(slot, abilityCount)
	session, err := sim.NewSession(director, sim.NewDispatcher(sim.Ports{Squad: outbox}))
	if err != nil {
		return nil, fmt.Errorf("sessionCreate: %w", err)
	}
	err = session.RunProgram(context.Background(), director.Simulator().Scope(PlayerRole), program)
	if err != nil {
		return nil, fmt.Errorf("programRun: %w", err)
	}
	if len(outbox.packets) != 0 {
		return nil, errors.New("unexpected immediate support unlock")
	}
	return &Run{session: session, outbox: outbox}, nil
}

func validateSoloSupportUnlockProgram(program sim.Program) error {
	if program.Provenance.LuaChunkID != zoneunlock.SoloSupportChunkID ||
		program.Provenance.BytecodeSHA256 != zoneunlock.SoloSupportSHA256 ||
		program.Provenance.FunctionName != "nTutorial_SoloSupportUnlock.main" {
		return fmt.Errorf("identity: got %d/%s/%s", program.Provenance.LuaChunkID,
			program.Provenance.BytecodeSHA256, program.Provenance.FunctionName)
	}
	wantWait := []time.Duration{
		2 * time.Second, 4 * time.Second, 2 * time.Second, 5 * time.Second, 2 * time.Second,
	}
	if len(program.Steps) != len(wantWait)+1 {
		return fmt.Errorf("stepCount: %d", len(program.Steps))
	}
	waitIndex := 0
	for index, step := range program.Steps {
		emit, isEmit := step.(sim.EmitStep)
		if isEmit {
			unlock, isUnlock := emit.Intent.(sim.UnlockIntent)
			if index != 2 || !isUnlock || unlock != (sim.UnlockIntent{
				PlayerRole: PlayerRole, UnlockKind: sim.UnlockNextAbility, Count: 1,
			}) {
				return fmt.Errorf("unlock[%d]: %#v", index, emit.Intent)
			}
			continue
		}
		wait, isWait := step.(sim.WaitStep)
		if !isWait || waitIndex >= len(wantWait) || wait.Duration != wantWait[waitIndex] {
			return fmt.Errorf("wait[%d]: %#v", index, step)
		}
		waitIndex++
	}
	if waitIndex != len(wantWait) {
		return fmt.Errorf("waitCount: %d", waitIndex)
	}
	return nil
}

func validateSupportUnlockProgram(program sim.Program) error {
	if program.Provenance.LuaChunkID != zoneunlock.SupportChunkID ||
		program.Provenance.BytecodeSHA256 != zoneunlock.SupportSHA256 ||
		program.Provenance.FunctionName != "nTutorial_SupportUnlock.main" {
		return fmt.Errorf("identity: got %d/%s/%s", program.Provenance.LuaChunkID,
			program.Provenance.BytecodeSHA256, program.Provenance.FunctionName)
	}
	wantWait := []time.Duration{
		2 * time.Second, 4 * time.Second, 6 * time.Second,
		5 * time.Second, 2 * time.Second,
	}
	if len(program.Steps) != len(wantWait)+1 {
		return fmt.Errorf("stepCount: %d", len(program.Steps))
	}
	waitIndex := 0
	for index, step := range program.Steps {
		emit, isEmit := step.(sim.EmitStep)
		if isEmit {
			unlock, isUnlock := emit.Intent.(sim.UnlockIntent)
			if index != 2 || !isUnlock || unlock != (sim.UnlockIntent{
				PlayerRole: PlayerRole, UnlockKind: sim.UnlockNextAbility, Count: 1,
			}) {
				return fmt.Errorf("unlock[%d]: %#v", index, emit.Intent)
			}
			continue
		}
		wait, isWait := step.(sim.WaitStep)
		if !isWait || waitIndex >= len(wantWait) || wait.Duration != wantWait[waitIndex] {
			return fmt.Errorf("wait[%d]: %#v", index, step)
		}
		waitIndex++
	}
	if waitIndex != len(wantWait) {
		return fmt.Errorf("waitCount: %d", waitIndex)
	}
	return nil
}

func validateOverdriveUnlockProgram(program sim.Program) error {
	if program.Provenance.LuaChunkID != zoneunlock.OverdriveChunkID ||
		program.Provenance.BytecodeSHA256 != zoneunlock.OverdriveSHA256 ||
		program.Provenance.FunctionName != "nTutorial_OverdriveUnlock.main" {
		return fmt.Errorf("identity: got %d/%s/%s", program.Provenance.LuaChunkID,
			program.Provenance.BytecodeSHA256, program.Provenance.FunctionName)
	}
	wantWait := []time.Duration{
		3 * time.Second, 5 * time.Second, 5 * time.Second,
		4 * time.Second, 4 * time.Second, 3 * time.Second,
	}
	if len(program.Steps) != len(wantWait)+1 {
		return fmt.Errorf("stepCount: %d", len(program.Steps))
	}
	waitIndex := 0
	for index, step := range program.Steps {
		emit, isEmit := step.(sim.EmitStep)
		if isEmit {
			unlock, isUnlock := emit.Intent.(sim.UnlockIntent)
			if index != 2 || !isUnlock || unlock != (sim.UnlockIntent{
				PlayerRole: PlayerRole, UnlockKind: sim.UnlockOverdrive, Count: 1,
			}) {
				return fmt.Errorf("unlock[%d]: %#v", index, emit.Intent)
			}
			continue
		}
		wait, isWait := step.(sim.WaitStep)
		if !isWait || waitIndex >= len(wantWait) || wait.Duration != wantWait[waitIndex] {
			return fmt.Errorf("wait[%d]: %#v", index, step)
		}
		waitIndex++
	}
	if waitIndex != len(wantWait) {
		return fmt.Errorf("waitCount: %d", waitIndex)
	}
	return nil
}

func (r *Run) Advance(ctx context.Context, deadline time.Duration) ([][]byte, error) {
	if r == nil || r.session == nil || r.outbox == nil {
		return nil, errors.New("nil player feature unlock run")
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

func (r *Run) Stop() {
	if r == nil {
		return
	}
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	if r.session != nil {
		r.session.Director().Simulator().Stop()
	}
}

func (r *CatalystRun) Advance(
	ctx context.Context, deadline time.Duration,
) (CatalystBatch, error) {
	if r == nil || r.session == nil || r.outbox == nil {
		return CatalystBatch{}, errors.New("nil catalyst unlock run")
	}
	if ctx == nil {
		return CatalystBatch{}, errors.New("nil context")
	}
	now := r.session.Director().Simulator().Now()
	if deadline < now {
		return CatalystBatch{}, fmt.Errorf("deadlineBeforeNow: %s < %s", deadline, now)
	}
	err := r.session.AdvanceBy(ctx, deadline-now)
	if err != nil {
		return CatalystBatch{}, fmt.Errorf("sessionAdvance: %w", err)
	}
	return r.outbox.drainCatalyst(), nil
}

func (r *CatalystRun) Stop() {
	if r == nil {
		return
	}
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	if r.session != nil {
		r.session.Director().Simulator().Stop()
	}
}

func (r *Run) SetCancel(cancel raknet.CancelSchedule) {
	if r != nil {
		r.cancel = cancel
	}
}

func (r *CatalystRun) SetCancel(cancel raknet.CancelSchedule) {
	if r != nil {
		r.cancel = cancel
	}
}

func (o *playerFeatureUnlockOutbox) Unlock(
	_ context.Context, meta sim.EventMeta, intent sim.UnlockIntent,
) error {
	if o == nil {
		return errors.New("nil player feature unlock outbox")
	}
	if meta.ID == (sim.EventID{}) {
		return errors.New("event ID missing")
	}
	if intent.PlayerRole != PlayerRole || intent.Count != 1 {
		return fmt.Errorf("unlockIntent: %#v", intent)
	}
	previous, isApplied := o.appliedIntents[meta.ID]
	if isApplied {
		if previous != intent {
			return fmt.Errorf("eventConflict: %#v", meta.ID)
		}
		return nil
	}
	var message raknet.ApplicationMessage
	nextAbilityCount := o.abilityCount
	isAbilityCountChanged := false
	switch intent.UnlockKind {
	case sim.UnlockNextAbility:
		nextAbilityCount = o.abilityCount + 1
		if nextAbilityCount > 5 {
			nextAbilityCount = 9
		}
		message = raknet.LabsPlayerAbilityCountMessage{
			Slot: o.slot, AbilityCount: nextAbilityCount,
		}
		isAbilityCountChanged = true
	case sim.UnlockOverdrive:
		message = raknet.LabsPlayerOverdriveUnlockMessage{
			Slot: o.slot, MaximumEnergy: playerOverdriveMaximumEnergy,
		}
	case sim.UnlockCrystals:
		message = raknet.LabsPlayerCrystalUnlockMessage{Slot: o.slot}
	default:
		return fmt.Errorf("unlockKind: %s", intent.UnlockKind)
	}
	packet, err := raknet.MarshalApplication(message)
	if err != nil {
		return fmt.Errorf("unlockMarshal: %w", err)
	}
	if isAbilityCountChanged {
		o.abilityCount = nextAbilityCount
	}
	o.packets = append(o.packets, packet)
	o.appliedIntents[meta.ID] = intent
	return nil
}

func (o *playerFeatureUnlockOutbox) drain() [][]byte {
	if o == nil {
		return nil
	}
	packet := o.packets
	o.packets = nil
	return packet
}

func (o *playerFeatureUnlockOutbox) DropCrystals(
	_ context.Context, meta sim.EventMeta, intent sim.CrystalDropIntent,
) error {
	if o == nil {
		return errors.New("nil player feature unlock outbox")
	}
	if meta.ID == (sim.EventID{}) {
		return errors.New("event ID missing")
	}
	if intent.PlayerRole != PlayerRole {
		return fmt.Errorf("crystalDropIntent: %#v", intent)
	}
	previous, isDropped := o.droppedIntents[meta.ID]
	if isDropped {
		if previous != intent {
			return fmt.Errorf("eventConflict: %#v", meta.ID)
		}
		return nil
	}
	o.crystalDrops = append(o.crystalDrops, intent)
	o.droppedIntents[meta.ID] = intent
	return nil
}

func (o *playerFeatureUnlockOutbox) drainCatalyst() CatalystBatch {
	if o == nil {
		return CatalystBatch{}
	}
	batch := CatalystBatch{Packets: o.packets, CrystalDrops: o.crystalDrops}
	o.packets = nil
	o.crystalDrops = nil
	return batch
}

var _ sim.SquadPort = (*playerFeatureUnlockOutbox)(nil)
var _ sim.PickupPort = (*playerFeatureUnlockOutbox)(nil)
