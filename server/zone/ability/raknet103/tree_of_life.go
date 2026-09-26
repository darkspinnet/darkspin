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
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
)

const treeOfLifeWorldScale = float32(0.45)

type AreaHealingInput struct {
	Ability        sim.AbilityDefinition
	ActorObjectID  uint32
	EffectObjectID uint32
	Position       sim.Position
	ManaPoint      float32
	Team           uint8
	SourceTime     uint64
}

type AreaHealingOutput struct {
	Packet  [][]byte
	Pulse   []sim.AreaPulseIntent
	Cleanup [][]byte
}

type AreaHealingRun struct {
	run     *zoneability.AreaEffectRun
	encoder *areaHealingEncoder
	cancel  raknet.CancelSchedule
}

type areaHealingEncoder struct {
	encoder *simraknet103.Encoder
	req     AreaHealingInput
}

func NewAreaHealingRun(req AreaHealingInput) (*AreaHealingRun, [][]byte, error) {
	if req.ActorObjectID == 0 || req.EffectObjectID == 0 ||
		req.ManaPoint < 0 ||
		req.Ability.Kind != sim.AbilityKindAreaHealing {
		return nil, nil, errors.New("invalid area healing input")
	}
	resolver := roleResolver{
		zoneability.AreaEffectActorRole: {
			ObjectID: req.ActorObjectID,
			Position: req.Position,
			Team:     req.Team,
		},
		zoneability.AreaEffectObjectRole: {
			ObjectID:    req.EffectObjectID,
			Position:    req.Position,
			Team:        req.Team,
			Orientation: raknet.Quaternion{W: 1},
		},
	}
	wireEncoder, err := simraknet103.NewEncoder(resolver, req.SourceTime)
	if err != nil {
		return nil, nil, fmt.Errorf("encoderCreate: %w", err)
	}
	run, result, err := zoneability.NewAreaEffectRun(
		zoneability.AreaEffectRequest{
			Ability:  req.Ability,
			Position: req.Position,
		},
	)
	if err != nil {
		return nil, nil, fmt.Errorf("runCreate: %w", err)
	}
	encoder := &areaHealingEncoder{encoder: wireEncoder, req: req}
	output, err := encoder.encode(context.Background(), result)
	if err != nil {
		run.Stop()
		return nil, nil, fmt.Errorf("initialEncode: %w", err)
	}
	if len(output.Pulse) != 0 || len(output.Cleanup) != 0 {
		run.Stop()
		return nil, nil, errors.New("unexpected immediate continuation")
	}
	return &AreaHealingRun{run: run, encoder: encoder}, output.Packet, nil
}

func (r *AreaHealingRun) Advance(
	ctx context.Context, deadline time.Duration,
) (AreaHealingOutput, error) {
	if r == nil || r.run == nil || r.encoder == nil || ctx == nil {
		return AreaHealingOutput{}, errors.New("invalid area healing run")
	}
	result, err := r.run.Advance(ctx, deadline)
	if err != nil {
		return AreaHealingOutput{}, fmt.Errorf("runAdvance: %w", err)
	}
	output, err := r.encoder.encode(ctx, result)
	if err != nil {
		return AreaHealingOutput{}, fmt.Errorf("resultEncode: %w", err)
	}
	return output, nil
}

func (r *AreaHealingRun) Stop() {
	if r == nil {
		return
	}
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	if r.run != nil {
		r.run.Stop()
	}
}

func (r *AreaHealingRun) SetCancel(cancel raknet.CancelSchedule) {
	if r != nil {
		r.cancel = cancel
	}
}

func (r *AreaHealingRun) ClearCancel() {
	if r != nil {
		r.cancel = nil
	}
}

func (e *areaHealingEncoder) encode(
	ctx context.Context, result zoneability.AreaEffectResult,
) (AreaHealingOutput, error) {
	if e == nil || e.encoder == nil || ctx == nil {
		return AreaHealingOutput{}, errors.New("invalid area healing encoder")
	}
	var output AreaHealingOutput
	for index, event := range result.Events {
		switch intent := event.Intent.(type) {
		case sim.SpawnIntent:
			packet, err := e.encodeSpawn(intent)
			if err != nil {
				return AreaHealingOutput{}, fmt.Errorf("spawn[%d]: %w", index, err)
			}
			output.Packet = append(output.Packet, packet)
		case sim.TeleportIntent:
			if intent.Destination != e.req.Position {
				return AreaHealingOutput{}, fmt.Errorf(
					"teleport[%d]: unexpected destination", index,
				)
			}
		case sim.CooldownIntent:
			packet, err := e.encodeCooldown(event, intent)
			if err != nil {
				return AreaHealingOutput{}, fmt.Errorf(
					"cooldown[%d]: %w", index, err,
				)
			}
			output.Packet = append(output.Packet, packet)
		case sim.ResourceChangeIntent:
			packet, err := e.encodeResource(intent)
			if err != nil {
				return AreaHealingOutput{}, fmt.Errorf(
					"resource[%d]: %w", index, err,
				)
			}
			output.Packet = append(output.Packet, packet)
		case sim.AreaPulseIntent:
			output.Pulse = append(output.Pulse, intent)
		case sim.DespawnIntent:
			packet, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{
				ObjectID: []uint32{e.req.EffectObjectID},
			})
			if err != nil {
				return AreaHealingOutput{}, fmt.Errorf(
					"despawn[%d]: %w", index, err,
				)
			}
			output.Cleanup = append(output.Cleanup, packet)
		default:
			packet, err := e.encoder.EncodeBatch(ctx, []sim.Event{event})
			if err != nil {
				return AreaHealingOutput{}, fmt.Errorf(
					"event[%d]: %w", index, err,
				)
			}
			output.Packet = append(output.Packet, packet...)
		}
	}
	return output, nil
}

func (e *areaHealingEncoder) encodeSpawn(intent sim.SpawnIntent) ([]byte, error) {
	if intent.NounName != e.req.Ability.SpawnNoun {
		return nil, errors.New("unexpected noun")
	}
	packet, err := raknet.MarshalApplication(raknet.ObjectCreateMessage{
		ObjectID:  e.req.EffectObjectID,
		Noun:      util.HashID(intent.NounName),
		PositionX: e.req.Position.X,
		PositionY: e.req.Position.Y,
		PositionZ: e.req.Position.Z,
		Scale:     treeOfLifeWorldScale,
		Team:      e.req.Team,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	return packet, nil
}

func (e *areaHealingEncoder) encodeCooldown(
	event sim.Event, intent sim.CooldownIntent,
) ([]byte, error) {
	if intent.AbilityName != e.req.Ability.Name ||
		intent.Duration != e.req.Ability.Cooldown {
		return nil, errors.New("unexpected cooldown")
	}
	packet, err := raknet.MarshalApplication(raknet.CooldownUpdateMessage{
		ObjectID:             e.req.ActorObjectID,
		AbilityKey:           uint64(util.HashID(intent.AbilityName)),
		DurationMilliseconds: intent.Duration.Milliseconds(),
		SourceStartMilliseconds: int64(
			e.req.SourceTime + uint64(event.At/time.Millisecond),
		),
	})
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	return packet, nil
}

func (e *areaHealingEncoder) encodeResource(
	intent sim.ResourceChangeIntent,
) ([]byte, error) {
	if intent.Delta != -e.req.Ability.ManaCost {
		return nil, errors.New("unexpected resource change")
	}
	packet, err := raknet.MarshalApplication(raknet.CombatantDataDeltaMessage{
		ObjectID:           e.req.ActorObjectID,
		ManaPoints:         e.req.ManaPoint,
		IsManaPointChanged: true,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	return packet, nil
}

type Healing struct {
	SourceObjectID uint32
	ObjectID       uint32
	HitPoint       float32
	Amount         float32
}

func MarshalTreeOfLifeHealing(
	ability sim.AbilityDefinition, position raknet.Vector3,
	healing []Healing, isFinal bool,
) ([][]byte, error) {
	messages := make([]raknet.ApplicationMessage, 0, 2+len(healing))
	isEffectVisible := len(healing) != 0 || isFinal
	if ability.Name == "TreeOfLife" {
		// The authored heal event is the tree's final burst and disappearance
		// sound. Ordinary healing ticks must not replay it.
		isEffectVisible = isFinal
	}
	if ability.HealEffectName != "" && isEffectVisible {
		messages = append(messages, raknet.PositionedEffectMessage{
			Asset: util.HashID(ability.HealEffectName), Position: position,
		})
	}
	for _, healedCharacter := range healing {
		if healedCharacter.SourceObjectID == 0 || healedCharacter.ObjectID == 0 ||
			healedCharacter.Amount <= 0 {
			return nil, errors.New("invalid Tree of Life healing")
		}
		previousHitPoint := healedCharacter.HitPoint - healedCharacter.Amount
		integerChange := int32(healedCharacter.HitPoint) - int32(previousHitPoint)
		messages = append(
			messages,
			raknet.DamageCombatEventMessage{
				Flags: 0x0002, DeltaHealth: -healedCharacter.Amount,
				TargetID:        healedCharacter.ObjectID,
				SourceID:        healedCharacter.SourceObjectID,
				IntegerHPChange: -integerChange,
			},
			raknet.CombatantDataDeltaMessage{
				ObjectID:          healedCharacter.ObjectID,
				HitPoints:         healedCharacter.HitPoint,
				IsHitPointChanged: true,
			},
		)
	}
	return marshalApplicationMessages(messages, "treeHealing")
}

func marshalApplicationMessages(
	messages []raknet.ApplicationMessage, operation string,
) ([][]byte, error) {
	packets := make([][]byte, 0, len(messages))
	for index, message := range messages {
		packet, err := raknet.MarshalApplication(message)
		if err != nil {
			return nil, fmt.Errorf("%s[%d]: %w", operation, index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}
