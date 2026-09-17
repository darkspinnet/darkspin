package companion

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
)

const passivePhase sim.Phase = "summonPassive"
const passiveOwnerRole sim.Role = "summonPassiveOwner"

type PassiveRequest struct {
	ObjectID      uint32
	OwnerObjectID uint32
	Spawn         *sim.CompanionSpawnIntent
	IsDespawn     bool
}

type PassiveInput struct {
	Definition         sim.SummonPassiveDefinition
	OwnerObjectID      uint32
	CompanionObjectIDs []uint32
}

type Passive struct {
	simulator *sim.Simulator
	session   *sim.Session
	behavior  *sim.SummonPassiveBehavior
	outbox    *passiveOutbox
	cancels   []func()
}

type passiveOutbox struct {
	ownerObjectID  uint32
	roleByObjectID map[uint32]sim.Role
	objectIDByRole map[sim.Role]uint32
	requests       []PassiveRequest
}

func NewPassive(req PassiveInput) (*Passive, error) {
	if req.OwnerObjectID == 0 || req.Definition.MaximumCompanion == 0 ||
		len(req.CompanionObjectIDs) != int(req.Definition.MaximumCompanion) {
		return nil, errors.New("invalid summon passive input")
	}
	objectIDs := slices.Clone(req.CompanionObjectIDs)
	slices.Sort(objectIDs)
	for index, objectID := range objectIDs {
		if objectID == 0 || (index > 0 && objectIDs[index-1] == objectID) {
			return nil, fmt.Errorf("companionObjectID[%d]: invalid", index)
		}
	}
	roles := make([]sim.Role, len(req.CompanionObjectIDs))
	roleByObjectID := make(map[uint32]sim.Role, len(roles))
	objectIDByRole := make(map[sim.Role]uint32, len(roles))
	for index, objectID := range req.CompanionObjectIDs {
		role := sim.Role(fmt.Sprintf("summonPassiveCompanion%d", index+1))
		roles[index] = role
		roleByObjectID[objectID] = role
		objectIDByRole[role] = objectID
	}
	activeRoles := make([]sim.Role, 0, len(roles)+1)
	activeRoles = append(activeRoles, passiveOwnerRole)
	activeRoles = append(activeRoles, roles...)
	director, err := sim.NewDirector([]sim.PhaseDefinition{{
		Name: passivePhase, ActiveRoles: activeRoles,
	}}, passivePhase)
	if err != nil {
		return nil, fmt.Errorf("directorCreate: %w", err)
	}
	outbox := &passiveOutbox{
		ownerObjectID:  req.OwnerObjectID,
		roleByObjectID: roleByObjectID,
		objectIDByRole: objectIDByRole,
	}
	dispatcher := sim.NewDispatcher(sim.Ports{Object: outbox, Companion: outbox})
	session, err := sim.NewSession(director, dispatcher)
	if err != nil {
		return nil, fmt.Errorf("sessionCreate: %w", err)
	}
	simulator := director.Simulator()
	scope := simulator.Scope(passiveOwnerRole)
	behavior, err := sim.StartSummonPassive(
		simulator,
		scope,
		sim.SummonPassiveInput{
			Definition:     req.Definition,
			OwnerRole:      passiveOwnerRole,
			CompanionRoles: roles,
		},
	)
	if err != nil {
		simulator.Stop()
		return nil, fmt.Errorf("behaviorStart: %w", err)
	}
	return &Passive{
		simulator: simulator,
		session:   session,
		behavior:  behavior,
		outbox:    outbox,
	}, nil
}

func (e *Passive) Advance(deadline time.Duration) ([]PassiveRequest, error) {
	if e == nil || e.simulator == nil || e.session == nil ||
		e.behavior == nil || e.outbox == nil {
		return nil, errors.New("invalid summon passive")
	}
	now := e.simulator.Now()
	if deadline < now {
		return nil, fmt.Errorf("deadlineBeforeNow: %s < %s", deadline, now)
	}
	err := e.session.AdvanceBy(context.Background(), deadline-now)
	if err != nil {
		return nil, fmt.Errorf("sessionAdvance: %w", err)
	}
	return e.outbox.drain(), nil
}

func (e *Passive) CompanionDied(objectID uint32) error {
	if e == nil || e.behavior == nil || objectID == 0 {
		return errors.New("invalid summon passive death")
	}
	role, isFound := e.outbox.roleByObjectID[objectID]
	if !isFound {
		return fmt.Errorf("companionObjectID: %d", objectID)
	}
	err := e.behavior.CompanionDied(role)
	if err != nil {
		return fmt.Errorf("behaviorDeath: %w", err)
	}
	return nil
}

func (e *Passive) RespawnCompanion(
	objectID uint32, replacementObjectID uint32,
) ([]PassiveRequest, error) {
	if e == nil || e.behavior == nil || e.session == nil || objectID == 0 ||
		replacementObjectID == 0 || objectID == replacementObjectID {
		return nil, errors.New("invalid summon passive respawn")
	}
	role, isFound := e.outbox.roleByObjectID[objectID]
	if !isFound {
		return nil, fmt.Errorf("companionObjectID: %d", objectID)
	}
	if _, isReplacementUsed := e.outbox.roleByObjectID[replacementObjectID]; isReplacementUsed {
		return nil, fmt.Errorf("replacementObjectID: %d", replacementObjectID)
	}
	err := e.behavior.RespawnCompanion(role)
	if err != nil {
		return nil, fmt.Errorf("behaviorRespawn: %w", err)
	}
	delete(e.outbox.roleByObjectID, objectID)
	e.outbox.roleByObjectID[replacementObjectID] = role
	e.outbox.objectIDByRole[role] = replacementObjectID
	err = e.session.DispatchPending(context.Background())
	if err != nil {
		e.outbox.drain()
		delete(e.outbox.roleByObjectID, replacementObjectID)
		e.outbox.roleByObjectID[objectID] = role
		e.outbox.objectIDByRole[role] = objectID
		return nil, fmt.Errorf("respawnDispatch: %w", err)
	}
	return e.outbox.drain(), nil
}

func (e *Passive) CompanionSeparated(
	objectID uint32,
) ([]PassiveRequest, error) {
	if e == nil || e.behavior == nil || objectID == 0 {
		return nil, errors.New("invalid summon passive separation")
	}
	role, isFound := e.outbox.roleByObjectID[objectID]
	if !isFound {
		return nil, fmt.Errorf("companionObjectID: %d", objectID)
	}
	err := e.behavior.CompanionSeparated(role)
	if err != nil {
		return nil, fmt.Errorf("behaviorSeparation: %w", err)
	}
	err = e.session.DispatchPending(context.Background())
	if err != nil {
		return nil, fmt.Errorf("separationDispatch: %w", err)
	}
	return e.outbox.drain(), nil
}

func (e *Passive) Stop() ([]PassiveRequest, error) {
	if e == nil || e.simulator == nil || e.session == nil ||
		e.behavior == nil || e.outbox == nil {
		return nil, nil
	}
	for _, cancel := range e.cancels {
		if cancel != nil {
			cancel()
		}
	}
	e.cancels = nil
	err := e.behavior.Stop()
	if err != nil {
		return nil, fmt.Errorf("behaviorStop: %w", err)
	}
	err = e.session.DispatchPending(context.Background())
	if err != nil {
		return nil, fmt.Errorf("stopDispatch: %w", err)
	}
	requests := e.outbox.drain()
	e.simulator.InvalidateRole(passiveOwnerRole)
	e.simulator.Stop()
	return requests, nil
}

func (e *Passive) AddCancel(cancel func()) {
	if e == nil || cancel == nil {
		return
	}
	e.cancels = append(e.cancels, cancel)
}

func (e *Passive) DeadlineAfter(delay time.Duration) (time.Duration, error) {
	if e == nil || e.simulator == nil || delay < 0 {
		return 0, errors.New("invalid summon passive deadline")
	}
	return e.simulator.Now() + delay, nil
}

func (e *passiveOutbox) SpawnCompanion(
	_ context.Context,
	_ sim.EventMeta,
	intent sim.CompanionSpawnIntent,
) error {
	objectID, isFound := e.objectIDByRole[intent.Role]
	if !isFound || intent.OwnerRole != passiveOwnerRole {
		return fmt.Errorf("spawnRole: %#v", intent)
	}
	spawn := intent
	e.requests = append(e.requests, PassiveRequest{
		ObjectID:      objectID,
		OwnerObjectID: e.ownerObjectID,
		Spawn:         &spawn,
	})
	return nil
}

func (e *passiveOutbox) Despawn(
	_ context.Context,
	_ sim.EventMeta,
	intent sim.DespawnIntent,
) error {
	objectID, isFound := e.objectIDByRole[intent.Role]
	if !isFound {
		return fmt.Errorf("despawnRole: %s", intent.Role)
	}
	e.requests = append(e.requests, PassiveRequest{
		ObjectID:      objectID,
		OwnerObjectID: e.ownerObjectID,
		IsDespawn:     true,
	})
	return nil
}

func (*passiveOutbox) Spawn(
	context.Context,
	sim.EventMeta,
	sim.SpawnIntent,
) error {
	return errors.New("spawn unsupported")
}

func (*passiveOutbox) Move(
	context.Context,
	sim.EventMeta,
	sim.MovementIntent,
) error {
	return errors.New("move unsupported")
}

func (*passiveOutbox) Teleport(
	context.Context,
	sim.EventMeta,
	sim.TeleportIntent,
) error {
	return errors.New("teleport unsupported")
}

func (*passiveOutbox) CreateTriggerVolume(
	context.Context,
	sim.EventMeta,
	sim.TriggerVolumeIntent,
) error {
	return errors.New("trigger create unsupported")
}

func (*passiveOutbox) DestroyTriggerVolume(
	context.Context,
	sim.EventMeta,
	sim.DestroyTriggerVolumeIntent,
) error {
	return errors.New("trigger destroy unsupported")
}

func (e *passiveOutbox) drain() []PassiveRequest {
	requests := e.requests
	e.requests = nil
	return requests
}

func PassiveSpawnPosition(
	ownerPosition game.Vec3,
	slotIndex int,
	slotCount int,
	radius float32,
) (game.Vec3, error) {
	if slotIndex < 0 || slotIndex >= slotCount || slotCount <= 0 ||
		!isFinitePositive(radius) || !isFinitePosition(ownerPosition) {
		return game.Vec3{}, errors.New("invalid summon passive placement")
	}
	// Authored content preserves only the maximum spawn radius, not the random
	// placement sample. Even angular spacing remains deterministic and inside
	// that bound until the sampler is recovered.
	angle := 2 * math.Pi * float64(slotIndex) / float64(slotCount)
	distance := float64(radius) * 0.5
	return game.Vec3{
		X: ownerPosition.X + float32(math.Cos(angle)*distance),
		Y: ownerPosition.Y + float32(math.Sin(angle)*distance),
		Z: ownerPosition.Z,
	}, nil
}
