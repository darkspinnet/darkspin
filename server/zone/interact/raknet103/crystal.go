package raknet103

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneinteract "github.com/darkspinnet/darkspin/server/zone/interact"
)

const (
	crystalPickupPhase     sim.Phase = "crystalPickup"
	crystalPlayerRole      sim.Role  = "player"
	crystalPlayerAgentRole sim.Role  = "playerAgent"
	crystalTargetRole      sim.Role  = "crystal"
)

type CrystalPickupRun struct {
	admission sim.CrystalPickupAdmission
	session   *sim.Session
	outbox    *crystalPickupOutbox
}

type crystalPickupOutbox struct {
	token     sim.CrystalPickupToken
	inventory *sim.CrystalInventory
	pickup    *sim.CrystalPickupObject
	results   []sim.CrystalCollectionResult
}

func NewCrystalPickupRun(
	program sim.Program, command sim.CrystalPickupCommand,
	inventory *sim.CrystalInventory, pickup *sim.CrystalPickupObject,
) (*CrystalPickupRun, error) {
	admission, err := sim.AdmitCrystalPickup(0, command)
	if err != nil {
		return nil, fmt.Errorf("pickupAdmit: %w", err)
	}
	run := &CrystalPickupRun{admission: admission.Kind}
	if admission.Kind != sim.CrystalPickupAccepted {
		return run, nil
	}
	if inventory == nil {
		return nil, errors.New("nil crystal inventory")
	}
	err = validateCrystalPickupProgram(program)
	if err != nil {
		return nil, fmt.Errorf("programValidate: %w", err)
	}
	program = bindCrystalPickupProgram(program, admission.Token)
	director, err := sim.NewDirector([]sim.PhaseDefinition{{
		Name: crystalPickupPhase,
		ActiveRoles: []sim.Role{
			admission.Token.PlayerRole, admission.Token.AgentRole, admission.Token.TargetRole,
		},
	}}, crystalPickupPhase)
	if err != nil {
		return nil, fmt.Errorf("directorCreate: %w", err)
	}
	outbox := &crystalPickupOutbox{
		token: admission.Token, inventory: inventory, pickup: pickup,
	}
	session, err := sim.NewSession(director, sim.NewDispatcher(sim.Ports{CrystalPickup: outbox}))
	if err != nil {
		return nil, fmt.Errorf("sessionCreate: %w", err)
	}
	nativeProvenance := sim.Provenance{
		FunctionName: "PickUpCrystal.release", Confidence: sim.ConfidenceNative,
	}
	scheduledProgram := program
	scheduledProgram.Steps = append([]sim.Step{sim.WaitStep{
		Duration: zoneinteract.CrystalPickupReleaseDuration, Provenance: &nativeProvenance,
	}}, program.Steps...)
	err = session.RunProgram(
		context.Background(), director.Simulator().Scope(admission.Token.TargetRole), scheduledProgram,
	)
	if err != nil {
		return nil, fmt.Errorf("programRun: %w", err)
	}
	if len(outbox.results) != 0 {
		return nil, errors.New("unexpected immediate crystal pickup")
	}
	run.session = session
	run.outbox = outbox
	return run, nil
}

func validateCrystalPickupProgram(program sim.Program) error {
	if program.Provenance.LuaChunkID != zoneinteract.CrystalPickupChunkID ||
		program.Provenance.BytecodeSHA256 != zoneinteract.CrystalPickupSHA256 ||
		program.Provenance.FunctionName != "PickUpCrystal" || len(program.Steps) != 2 {
		return fmt.Errorf("identity: %#v", program.Provenance)
	}
	pickupStep, isPickupStep := program.Steps[0].(sim.EmitStep)
	if !isPickupStep || pickupStep.Intent != (sim.CrystalPickupIntent{
		PlayerRole: crystalPlayerRole, AgentRole: crystalPlayerAgentRole,
		TargetRole: crystalTargetRole,
	}) {
		return fmt.Errorf("pickupStep: %#v", program.Steps[0])
	}
	completeStep, isCompleteStep := program.Steps[1].(sim.EmitStep)
	if !isCompleteStep || completeStep.Intent != (sim.SequenceCompleteIntent{
		Role: crystalPlayerAgentRole,
	}) {
		return fmt.Errorf("completeStep: %#v", program.Steps[1])
	}
	return nil
}

func bindCrystalPickupProgram(
	program sim.Program, token sim.CrystalPickupToken,
) sim.Program {
	steps := append([]sim.Step(nil), program.Steps...)
	pickupStep := steps[0].(sim.EmitStep)
	pickupStep.Intent = sim.CrystalPickupIntent{
		PlayerRole: token.PlayerRole, AgentRole: token.AgentRole,
		TargetRole: token.TargetRole,
	}
	steps[0] = pickupStep
	completeStep := steps[1].(sim.EmitStep)
	completeStep.Intent = sim.SequenceCompleteIntent{Role: token.AgentRole}
	steps[1] = completeStep
	program.Steps = steps
	return program
}

func (r *CrystalPickupRun) Advance(
	ctx context.Context, deadline time.Duration,
) ([]sim.CrystalCollectionResult, error) {
	if r == nil {
		return nil, errors.New("nil crystal pickup run")
	}
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	if r.admission != sim.CrystalPickupAccepted {
		return nil, nil
	}
	if r.session == nil || r.outbox == nil {
		return nil, errors.New("incomplete crystal pickup run")
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

func (r *CrystalPickupRun) Stop() {
	if r == nil || r.session == nil {
		return
	}
	r.session.Director().Simulator().Stop()
}

func (r *CrystalPickupRun) Admission() sim.CrystalPickupAdmission {
	if r == nil {
		return 0
	}
	return r.admission
}

func (o *crystalPickupOutbox) PickupCrystal(
	_ context.Context, meta sim.EventMeta, intent sim.CrystalPickupIntent,
) error {
	if o == nil || o.inventory == nil {
		return errors.New("nil crystal pickup outbox")
	}
	if intent != (sim.CrystalPickupIntent{
		PlayerRole: o.token.PlayerRole, AgentRole: o.token.AgentRole, TargetRole: o.token.TargetRole,
	}) {
		return fmt.Errorf("pickupIntent: %#v", intent)
	}
	result, err := o.inventory.ReleasePickup(meta.At, o.token, o.pickup)
	if err != nil {
		return fmt.Errorf("pickupRelease: %w", err)
	}
	o.results = append(o.results, result)
	return nil
}

func (o *crystalPickupOutbox) drain() []sim.CrystalCollectionResult {
	if o == nil {
		return nil
	}
	results := o.results
	o.results = nil
	return results
}

var _ sim.CrystalPickupPort = (*crystalPickupOutbox)(nil)

// crystalLobLocomotion converts the simulator-owned trajectory into the exact
// receiver vocabulary without deciding packet scheduling or ordering.
func CrystalLobLocomotion(
	request sim.CrystalPickupRequest, pickupObjectID uint32,
) (raknet.LobLocomotionMessage, error) {
	if pickupObjectID == 0 || request.Role == "" || request.Lob.StartTime < 0 ||
		request.Lob.Duration <= 0 {
		return raknet.LobLocomotionMessage{}, errors.New("invalid crystal lob")
	}
	startTimeMilliseconds := request.Lob.StartTime.Milliseconds()
	if startTimeMilliseconds < 0 {
		return raknet.LobLocomotionMessage{}, errors.New("invalid crystal lob time")
	}
	parameter := raknet.LobParameter{
		StartPosition: raknet.Vector3{
			X: request.Position.X, Y: request.Position.Y, Z: request.Position.Z,
		},
		Destination: raknet.Vector3{
			X: request.Destination.X, Y: request.Destination.Y, Z: request.Destination.Z,
		},
		UpDirection:                  raknet.Vector3{Z: 1},
		PlaneDirectionVelocity:       request.Lob.PlaneDirectionVelocity,
		Height:                       request.Lob.Height,
		DurationSecond:               float32(request.Lob.Duration.Seconds()),
		BounceNumber:                 int32(request.Lob.BounceCount),
		BounceRestitution:            request.Lob.BounceRestitution,
		IsGroundCollisionOnly:        request.Lob.IsGroundCollisionOnly,
		IsStopBounceOnCreature:       request.Lob.IsStopBounceOnCreature,
		PlaneDirection:               raknet.Vector3{X: request.Lob.PlaneDirection.X, Y: request.Lob.PlaneDirection.Y, Z: request.Lob.PlaneDirection.Z},
		ReflectedPlaneDirectionSpeed: request.Lob.PlaneDirectionVelocity,
		UpLinearParameter:            request.Lob.UpLinearParameter,
		UpQuadraticParameter:         request.Lob.UpQuadraticParameter,
	}
	return raknet.LobLocomotionMessage{
		ObjectID: pickupObjectID, StartTimeMilliseconds: uint64(startTimeMilliseconds),
		Parameter: parameter,
	}, nil
}

type CrystalFullPublication struct {
	PlayerSelector uint8
	EventPacket    []byte
	RelaunchPacket []byte
}

// marshalCrystalFull publishes the recovered full-inventory feedback and, when
// present, the already-applied stationary relaunch. NotifyPlayer stores the
// complement player mask in reflected ServerEvent field 16.
func MarshalCrystalFull(
	result sim.CrystalCollectionResult, pickupObjectID uint32, pickupPosition sim.Position,
) (CrystalFullPublication, error) {
	if pickupObjectID == 0 || len(result.Actions) == 0 || len(result.Actions) > 2 {
		return CrystalFullPublication{}, errors.New("invalid crystal full result")
	}
	publication := CrystalFullPublication{}
	index := 0
	if result.Actions[0].Kind == sim.CrystalPickupRelaunched {
		relaunch := result.Actions[0]
		message, err := CrystalLobLocomotion(sim.CrystalPickupRequest{
			Role: relaunch.Role, Position: pickupPosition, Destination: pickupPosition,
			Lob: relaunch.Lob,
		}, pickupObjectID)
		if err != nil {
			return CrystalFullPublication{}, fmt.Errorf("relaunchMessage: %w", err)
		}
		publication.RelaunchPacket, err = raknet.MarshalApplication(message)
		if err != nil {
			return CrystalFullPublication{}, fmt.Errorf("relaunchMarshal: %w", err)
		}
		index++
	}
	if index != len(result.Actions)-1 {
		return CrystalFullPublication{}, errors.New("invalid crystal full action order")
	}
	feedback := result.Actions[index]
	if feedback.Kind != sim.CrystalFullPublished || feedback.ClientEventID != 0x6ea4091e ||
		feedback.PlayerSelector == 0 {
		return CrystalFullPublication{}, fmt.Errorf("crystal full feedback: %#v", feedback)
	}
	eventPacket, err := raknet.MarshalApplication(raknet.ClientEventMessage{
		ClientEventID: feedback.ClientEventID, PlayerExclusionMask: feedback.PlayerSelector,
	})
	if err != nil {
		return CrystalFullPublication{}, fmt.Errorf("fullEventMarshal: %w", err)
	}
	publication.PlayerSelector = feedback.PlayerSelector
	publication.EventPacket = eventPacket
	return publication, nil
}

// marshalCrystalCollection publishes only the fully recovered successful
// collection transaction. Full-slot feedback/relaunch remains gated on its
// unresolved transport ownership and locomotion reflection.
func MarshalCrystalCollection(
	result sim.CrystalCollectionResult, pickupObjectID uint32,
	playerSlot uint8, inventory sim.CrystalInventory,
) ([][]byte, error) {
	if pickupObjectID == 0 || len(result.Actions) != 4 {
		return nil, errors.New("invalid crystal collection result")
	}
	assigned := result.Actions[0]
	deleted := result.Actions[1]
	published := result.Actions[2]
	recomputed := result.Actions[3]
	if assigned.Kind != sim.CrystalSlotAssigned || deleted.Kind != sim.CrystalPickupDeleted ||
		published.Kind != sim.CrystalAcquiredPublished || recomputed.Kind != sim.CrystalBonusRecomputed ||
		assigned.Slot < 0 || assigned.Slot >= sim.CrystalSlotCount || assigned.NounName == "" ||
		assigned.NounAsset == 0 ||
		deleted.Role == "" || published.Slot != assigned.Slot ||
		published.NounName != assigned.NounName || published.NounAsset != assigned.NounAsset ||
		published.CrystalType != assigned.CrystalType {
		return nil, fmt.Errorf("crystal collection actions: %#v", result.Actions)
	}
	if published.CrystalLevel != assigned.CrystalLevel {
		return nil, fmt.Errorf("crystal collection level: %#v", result.Actions)
	}
	crystals := [sim.CrystalSlotCount]raknet.LabsCrystalResource{}
	for index, slot := range inventory.Slots {
		if !slot.IsOccupied {
			continue
		}
		if slot.NounAsset == 0 || slot.CrystalLevel < 0 || slot.CrystalLevel > 0xffff {
			return nil, fmt.Errorf("crystal inventory slot[%d]: %#v", index, slot)
		}
		crystals[index] = raknet.LabsCrystalResource{
			Noun: slot.NounAsset, Level: uint16(slot.CrystalLevel),
		}
	}
	assignedSlot := inventory.Slots[assigned.Slot]
	if !assignedSlot.IsOccupied || assignedSlot.NounAsset != assigned.NounAsset ||
		assignedSlot.CrystalLevel != assigned.CrystalLevel {
		return nil, fmt.Errorf("crystal inventory assignment: %#v", assignedSlot)
	}
	deletePacket, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{
		ObjectID: []uint32{pickupObjectID},
	})
	if err != nil {
		return nil, fmt.Errorf("pickupDelete: %w", err)
	}
	inventoryPacket, err := raknet.MarshalApplication(
		raknet.LabsPlayerCrystalInventoryMessage{
			PlayerSlot: playerSlot, Crystals: crystals,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("crystalInventory: %w", err)
	}
	links := inventory.Links()
	bonusPacket, err := raknet.MarshalApplication(
		raknet.LabsPlayerCrystalBonusesMessage{
			PlayerSlot: playerSlot, AreBonusesActive: links.AreLinesActive,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("crystalBonuses: %w", err)
	}
	crystalPacket, err := raknet.MarshalApplication(raknet.CrystalAcquiredMessage{
		Slot: published.Slot, NounAsset: published.NounAsset,
		CrystalColor: sim.CrystalColorForNoun(published.NounName, published.CrystalType),
		CrystalLevel: published.CrystalLevel,
	})
	if err != nil {
		return nil, fmt.Errorf("crystalAcquired: %w", err)
	}
	return [][]byte{deletePacket, inventoryPacket, bonusPacket, crystalPacket}, nil
}
