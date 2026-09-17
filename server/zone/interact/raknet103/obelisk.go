package raknet103

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	simraknet103 "github.com/darkspinnet/darkspin/server/sim/raknet103"
	"github.com/darkspinnet/darkspin/server/util"
)

const lootObeliskPhase sim.Phase = "lootObelisk"
const lootObeliskRole sim.Role = "lootObelisk"
const lootPlayerRole sim.Role = "player"

type ObeliskDefinition struct {
	ObjectID    uint32
	MarkerID    uint32
	Ability     string
	Position    raknet.Vector3
	UsesAllowed int32
}

type LootDefinition struct {
	ObjectID      uint32
	Noun          string
	RigblockAsset uint32
	SpawnPosition raknet.Vector3
	Position      raknet.Vector3
	ItemID        uint64
	InstanceID    uint64
	ItemLevel     int32
	Rarity        int32
}

type LootObeliskInput struct {
	Obelisk        ObeliskDefinition
	Loot           LootDefinition
	Ability        sim.AbilityDefinition
	PlayerObjectID uint32
	PlayerPosition raknet.Vector3
	SourceTime     uint64
}

type LootObeliskRun struct {
	session    *sim.Session
	outbox     *lootObeliskOutbox
	loot       LootDefinition
	deadlines  []time.Duration
	isComplete bool
}

type lootObeliskOutbox struct {
	*simraknet103.PacketOutbox
	obelisk ObeliskDefinition
	loot    LootDefinition
}

type lootObeliskResolver struct {
	objectBinding simraknet103.Binding
	playerBinding simraknet103.Binding
}

func (r lootObeliskResolver) ResolveRole(
	_ context.Context, role sim.Role,
) (simraknet103.Binding, error) {
	switch role {
	case lootObeliskRole:
		return r.objectBinding, nil
	case lootPlayerRole:
		return r.playerBinding, nil
	default:
		return simraknet103.Binding{}, fmt.Errorf("roleMissing: %s", role)
	}
}

func NewLootObeliskRun(input LootObeliskInput) (*LootObeliskRun, [][]byte, error) {
	if input.Obelisk.ObjectID == 0 || input.Loot.ObjectID == 0 ||
		input.PlayerObjectID == 0 {
		return nil, nil, errors.New("invalid loot obelisk input")
	}
	program, err := sim.InteractableProgram(input.Ability, lootObeliskRole, lootPlayerRole)
	if err != nil {
		return nil, nil, fmt.Errorf("programCreate: %w", err)
	}
	deadlines, err := sim.InteractableDeadlines(input.Ability)
	if err != nil {
		return nil, nil, fmt.Errorf("deadlineCreate: %w", err)
	}
	encoder, err := simraknet103.NewEncoder(lootObeliskResolver{
		objectBinding: simraknet103.Binding{
			ObjectID: input.Obelisk.ObjectID,
			Position: sim.Position{
				X: input.Obelisk.Position.X,
				Y: input.Obelisk.Position.Y,
				Z: input.Obelisk.Position.Z,
			},
		},
		playerBinding: simraknet103.Binding{
			ObjectID: input.PlayerObjectID,
			Position: sim.Position{
				X: input.PlayerPosition.X, Y: input.PlayerPosition.Y,
				Z: input.PlayerPosition.Z,
			},
		},
	}, input.SourceTime)
	if err != nil {
		return nil, nil, fmt.Errorf("encoderCreate: %w", err)
	}
	packetOutbox, err := simraknet103.NewPacketOutbox(encoder)
	if err != nil {
		return nil, nil, fmt.Errorf("outboxCreate: %w", err)
	}
	outbox := &lootObeliskOutbox{
		PacketOutbox: packetOutbox,
		obelisk:      input.Obelisk,
		loot:         input.Loot,
	}
	director, err := sim.NewDirector([]sim.PhaseDefinition{{
		Name: lootObeliskPhase,
		ActiveRoles: []sim.Role{
			lootObeliskRole,
			lootPlayerRole,
		},
	}}, lootObeliskPhase)
	if err != nil {
		return nil, nil, fmt.Errorf("directorCreate: %w", err)
	}
	dispatcher := sim.NewDispatcher(sim.Ports{
		Graphics: outbox, Physics: outbox, Interactable: outbox, Loot: outbox,
		Presentation: outbox,
	})
	session, err := sim.NewSession(director, dispatcher)
	if err != nil {
		return nil, nil, fmt.Errorf("sessionCreate: %w", err)
	}
	err = session.RunProgram(
		context.Background(), director.Simulator().Scope(lootObeliskRole), program,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("programRun: %w", err)
	}
	packets := outbox.Drain()
	if len(packets) == 0 {
		return nil, nil, errors.New("immediate packets missing")
	}
	run := &LootObeliskRun{
		session: session, outbox: outbox, loot: input.Loot, deadlines: deadlines,
	}
	return run, packets, nil
}

func (r *LootObeliskRun) Producers() []raknet.ScheduledPacketProducer {
	producers := make([]raknet.ScheduledPacketProducer, 0, len(r.deadlines))
	for _, deadline := range r.deadlines {
		step := lootObeliskStep{run: r, deadline: deadline}
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: deadline, Produce: step.produce,
		})
	}
	return producers
}

func (r *LootObeliskRun) Loot() LootDefinition {
	if r == nil {
		return LootDefinition{}
	}
	return r.loot
}

func (r *LootObeliskRun) Stop() {
	if r == nil || r.session == nil {
		return
	}
	r.isComplete = true
	simulator := r.session.Director().Simulator()
	simulator.InvalidateRole(lootObeliskRole)
	simulator.Stop()
}

func (r *LootObeliskRun) advance(
	ctx context.Context, deadline time.Duration,
) ([][]byte, error) {
	if r == nil || r.session == nil || r.outbox == nil || ctx == nil || r.isComplete {
		return nil, errors.New("invalid obelisk run")
	}
	now := r.session.Director().Simulator().Now()
	if deadline < now {
		return nil, fmt.Errorf("deadlineBeforeNow: %s < %s", deadline, now)
	}
	err := r.session.AdvanceBy(ctx, deadline-now)
	if err != nil {
		return nil, fmt.Errorf("sessionAdvance: %w", err)
	}
	packets := r.outbox.Drain()
	if len(packets) == 0 {
		return nil, fmt.Errorf("deadlineEmpty: %s", deadline)
	}
	if deadline == r.deadlines[len(r.deadlines)-1] {
		r.isComplete = true
		r.session.Director().Simulator().Stop()
	}
	return packets, nil
}

type lootObeliskStep struct {
	run      *LootObeliskRun
	deadline time.Duration
}

func (s lootObeliskStep) produce() ([][]byte, error) {
	packets, err := s.run.advance(context.Background(), s.deadline)
	if err != nil {
		return nil, fmt.Errorf("advance[%s]: %w", s.deadline, err)
	}
	return packets, nil
}

func (o *lootObeliskOutbox) Use(
	_ context.Context, _ sim.EventMeta, intent sim.InteractableUseIntent,
) error {
	if intent.Role != lootObeliskRole || intent.UseCount != 1 {
		return fmt.Errorf("interactableIntent: %#v", intent)
	}
	dataPacket, err := raknet.MarshalApplication(raknet.InteractableDataUpdateMessage{
		ObjectID: o.obelisk.ObjectID, TimesUsed: 1,
		UsesAllowed: o.obelisk.UsesAllowed, Ability: util.HashID(o.obelisk.Ability),
	})
	if err != nil {
		return fmt.Errorf("consumedData: %w", err)
	}
	statePacket, err := raknet.MarshalApplication(raknet.ObjectInteractableStateMessage{
		ObjectID: o.obelisk.ObjectID, State: 1, MarkerID: o.obelisk.MarkerID,
	})
	if err != nil {
		return fmt.Errorf("consumedState: %w", err)
	}
	o.Append(dataPacket, statePacket)
	return nil
}

func (o *lootObeliskOutbox) Drop(
	_ context.Context, _ sim.EventMeta, intent sim.LootDropIntent,
) error {
	if intent.SourceRole != lootObeliskRole || intent.PlayerRole != lootPlayerRole ||
		!intent.IsLoot || !intent.IsCrystal {
		return fmt.Errorf("lootIntent: %#v", intent)
	}
	createPacket, err := raknet.MarshalApplication(raknet.EnemyObjectCreateMessage{
		ObjectID: o.loot.ObjectID, Noun: util.HashID(o.loot.Noun),
		Position: o.loot.SpawnPosition, Scale: 1, IsCollidable: false,
	})
	if err != nil {
		return fmt.Errorf("lootCreate: %w", err)
	}
	dataPacket, err := raknet.MarshalApplication(raknet.LootDataUpdateMessage{
		ObjectID: o.loot.ObjectID, ItemID: o.loot.ItemID,
		RigblockAsset: o.loot.RigblockAsset, ItemLevel: o.loot.ItemLevel,
		Rarity: o.loot.Rarity, InstanceID: o.loot.InstanceID,
	})
	if err != nil {
		return fmt.Errorf("lootData: %w", err)
	}
	movePacket, err := raknet.MarshalApplication(raknet.LocomotionUnreliableMessage{
		ObjectID: o.loot.ObjectID, GoalPosition: o.loot.Position,
	})
	if err != nil {
		return fmt.Errorf("lootMove: %w", err)
	}
	o.Append(createPacket, dataPacket, movePacket)
	return nil
}
