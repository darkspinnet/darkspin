package raknet103

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

type Activation struct {
	ObjectID         uint32
	OwnerObjectID    uint32
	SpawnAbilityID   uint32
	BurrowModifierID uint32
	Position         raknet.Vector3
	TargetObjectID   uint32
	CooldownEnd      time.Time
	Attack           *abilityraknet.MeleeRun
}

type MeleeInput struct {
	Ability        sim.AbilityDefinition
	CompanionID    uint32
	TargetID       uint32
	Companion      sim.Position
	Target         sim.Position
	TargetHitPoint float32
	Damage         float32
	SourceTime     uint64
	CenterRange    float32
	StartedAt      time.Time
}

type MeleeStart struct {
	CompanionID uint32
	TargetID    uint32
	Run         *abilityraknet.MeleeRun
	Packet      [][]byte
	CooldownEnd time.Time
}

func PassiveRequest(
	req zonecompanion.PassiveRequest,
	position raknet.Vector3,
) ([][]byte, *Activation, error) {
	if req.ObjectID == 0 || req.OwnerObjectID == 0 {
		return nil, nil, errors.New("invalid summon passive request")
	}
	if req.IsDespawn {
		if req.Spawn != nil {
			return nil, nil, errors.New("despawn request includes spawn")
		}
		packet, err := Defeat(req.ObjectID)
		if err != nil {
			return nil, nil, fmt.Errorf("despawnMarshal: %w", err)
		}
		return [][]byte{packet}, nil, nil
	}
	if req.Spawn == nil || req.Spawn.NounName == "" ||
		req.Spawn.SpawnEffectID == 0 || req.Spawn.SpawnAbilityID == 0 ||
		req.Spawn.BurrowModifierID == 0 ||
		!zonegeometry.IsFinite(gamePosition(position)) {
		return nil, nil, errors.New("invalid summon passive spawn")
	}
	packets, err := Spawn(SpawnRequest{
		ObjectID:      req.ObjectID,
		OwnerObjectID: req.OwnerObjectID,
		Intent:        *req.Spawn,
		Position:      gamePosition(position),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("spawnMarshal: %w", err)
	}
	activation := &Activation{
		ObjectID:         req.ObjectID,
		OwnerObjectID:    req.OwnerObjectID,
		SpawnAbilityID:   req.Spawn.SpawnAbilityID,
		BurrowModifierID: req.Spawn.BurrowModifierID,
		Position:         position,
	}
	return packets, activation, nil
}

func SeparatedObjectIDs(
	activations map[uint32]Activation,
	ownerPosition raknet.Vector3,
	maximumDistance float32,
) []uint32 {
	if len(activations) == 0 || maximumDistance <= 0 ||
		!zonegeometry.IsFinite(gamePosition(ownerPosition)) {
		return nil
	}
	objectIDs := make([]uint32, 0, len(activations))
	for objectID, companion := range activations {
		if objectID == 0 ||
			!zonegeometry.IsFinite(gamePosition(companion.Position)) ||
			zonegeometry.Distance(
				gamePosition(ownerPosition),
				gamePosition(companion.Position),
			) <=
				maximumDistance {
			continue
		}
		objectIDs = append(objectIDs, objectID)
	}
	slices.Sort(objectIDs)
	return objectIDs
}

func StopAttacks(activations map[uint32]Activation) {
	for objectID, companion := range activations {
		if companion.Attack == nil {
			continue
		}
		companion.Attack.Stop()
		companion.Attack = nil
		companion.TargetObjectID = 0
		activations[objectID] = companion
	}
}

func PresentationPackets(packets [][]byte) [][]byte {
	presentation := make([][]byte, 0, len(packets))
	for _, packet := range packets {
		if len(packet) == 0 || (packet[0] != byte(raknet.CombatEvent) &&
			packet[0] != byte(raknet.CombatantDataUpdate)) {
			presentation = append(presentation, packet)
		}
	}
	return presentation
}

func NewMelee(req MeleeInput) (MeleeStart, error) {
	if req.Ability.Kind != sim.AbilityKindMelee ||
		req.CompanionID == 0 || req.TargetID == 0 ||
		req.CompanionID == req.TargetID || req.Damage <= 0 ||
		req.TargetHitPoint <= 0 || req.CenterRange <= 0 ||
		req.StartedAt.IsZero() {
		return MeleeStart{}, errors.New("invalid summon companion attack")
	}
	companionPosition := game.Vec3{
		X: req.Companion.X,
		Y: req.Companion.Y,
		Z: req.Companion.Z,
	}
	targetPosition := game.Vec3{
		X: req.Target.X,
		Y: req.Target.Y,
		Z: req.Target.Z,
	}
	if !zonegeometry.ContainsSphere(
		targetPosition,
		companionPosition,
		req.CenterRange,
	) {
		return MeleeStart{}, errors.New("summon companion target out of range")
	}
	facing := sim.Position{
		X: req.Target.X - req.Companion.X,
		Y: req.Target.Y - req.Companion.Y,
		Z: req.Target.Z - req.Companion.Z,
	}
	lengthSquared := facing.X*facing.X + facing.Y*facing.Y +
		facing.Z*facing.Z
	if lengthSquared <= 0 {
		return MeleeStart{}, errors.New(
			"summon companion target overlaps actor",
		)
	}
	length := float32(math.Sqrt(float64(lengthSquared)))
	facing.X /= length
	facing.Y /= length
	facing.Z /= length
	run, packets, err := abilityraknet.NewMeleeRun(abilityraknet.MeleeInput{
		Ability:        req.Ability,
		ActorObjectID:  req.CompanionID,
		TargetObjectID: req.TargetID,
		ActorPosition:  req.Companion,
		TargetPosition: req.Target,
		TargetFacing:   facing,
		Damage:         req.Damage,
		TargetHitPoint: req.TargetHitPoint,
		ActorTeam:      1,
		SourceTime:     req.SourceTime,
	})
	if err != nil {
		return MeleeStart{}, fmt.Errorf("meleeCreate: %w", err)
	}
	return MeleeStart{
		CompanionID: req.CompanionID,
		TargetID:    req.TargetID,
		Run:         run,
		Packet:      packets,
		CooldownEnd: req.StartedAt.Add(
			req.Ability.HitDelay + req.Ability.Cooldown,
		),
	}, nil
}

func gamePosition(position raknet.Vector3) game.Vec3 {
	return game.Vec3{X: position.X, Y: position.Y, Z: position.Z}
}
