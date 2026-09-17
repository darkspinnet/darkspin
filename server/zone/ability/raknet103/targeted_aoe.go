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

type TargetedAOEInput struct {
	Ability        sim.AbilityDefinition
	ActorObjectID  uint32
	EffectObjectID uint32
	Position       sim.Position
	ManaPoint      float32
	Team           uint8
	SourceTime     uint64
}

type TargetedAOERun struct {
	run     *zoneability.TargetedAOERun
	encoder *targetedAOEEncoder
	cancel  raknet.CancelSchedule
}

type targetedAOEEncoder struct {
	encoder *simraknet103.Encoder
	req     TargetedAOEInput
}

func NewTargetedAOERun(req TargetedAOEInput) (*TargetedAOERun, [][]byte, error) {
	if req.ActorObjectID == 0 || req.EffectObjectID == 0 ||
		req.ManaPoint < 0 ||
		req.Ability.Kind != sim.AbilityKindTargetedAOE {
		return nil, nil, errors.New("invalid targeted AOE input")
	}
	resolver := roleResolver{
		zoneability.TargetedAOEActorRole: {
			ObjectID: req.ActorObjectID,
			Position: req.Position,
			Team:     req.Team,
		},
		zoneability.TargetedAOEEffectRole: {
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
	run, result, err := zoneability.NewTargetedAOERun(
		zoneability.TargetedAOERequest{
			Ability:  req.Ability,
			Position: req.Position,
		},
	)
	if err != nil {
		return nil, nil, fmt.Errorf("runCreate: %w", err)
	}
	encoder := &targetedAOEEncoder{encoder: wireEncoder, req: req}
	packet, pulse, err := encoder.encode(context.Background(), result)
	if err != nil {
		run.Stop()
		return nil, nil, fmt.Errorf("initialEncode: %w", err)
	}
	if len(pulse) != 0 {
		run.Stop()
		return nil, nil, errors.New("immediate pulse")
	}
	return &TargetedAOERun{run: run, encoder: encoder}, packet, nil
}

func (r *TargetedAOERun) Advance(
	ctx context.Context, deadline time.Duration,
) ([][]byte, []sim.AreaPulseIntent, error) {
	if r == nil || r.run == nil || r.encoder == nil || ctx == nil {
		return nil, nil, errors.New("invalid targeted AOE run")
	}
	result, err := r.run.Advance(ctx, deadline)
	if err != nil {
		return nil, nil, fmt.Errorf("runAdvance: %w", err)
	}
	packet, pulse, err := r.encoder.encode(ctx, result)
	if err != nil {
		return nil, nil, fmt.Errorf("resultEncode: %w", err)
	}
	return packet, pulse, nil
}

func (r *TargetedAOERun) Stop() {
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

func (r *TargetedAOERun) CancelPackets(ctx context.Context) ([][]byte, error) {
	if r == nil || r.run == nil || r.encoder == nil || ctx == nil {
		return nil, errors.New("invalid targeted AOE cancellation")
	}
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	result, err := r.run.Cancel(ctx)
	if err != nil {
		return nil, fmt.Errorf("runCancel: %w", err)
	}
	packet, pulse, err := r.encoder.encode(ctx, result)
	if err != nil {
		return nil, fmt.Errorf("resultEncode: %w", err)
	}
	if len(pulse) != 0 {
		return nil, errors.New("unexpected cancellation pulse")
	}
	return packet, nil
}

func (r *TargetedAOERun) SetCancel(cancel raknet.CancelSchedule) {
	if r != nil {
		r.cancel = cancel
	}
}

func (r *TargetedAOERun) ClearCancel() {
	if r != nil {
		r.cancel = nil
	}
}

func (e *targetedAOEEncoder) encode(
	ctx context.Context, result zoneability.TargetedAOEResult,
) ([][]byte, []sim.AreaPulseIntent, error) {
	if e == nil || e.encoder == nil || ctx == nil {
		return nil, nil, errors.New("invalid targeted AOE encoder")
	}
	packet := make([][]byte, 0, len(result.Events))
	pulse := make([]sim.AreaPulseIntent, 0)
	for index, event := range result.Events {
		switch intent := event.Intent.(type) {
		case sim.SpawnIntent:
			encoded, err := e.encodeSpawn(intent)
			if err != nil {
				return nil, nil, fmt.Errorf("spawn[%d]: %w", index, err)
			}
			packet = append(packet, encoded)
		case sim.TeleportIntent:
			if intent.Destination != e.req.Position {
				return nil, nil, fmt.Errorf(
					"teleport[%d]: unexpected destination", index,
				)
			}
		case sim.CooldownIntent:
			encoded, err := e.encodeCooldown(event, intent)
			if err != nil {
				return nil, nil, fmt.Errorf("cooldown[%d]: %w", index, err)
			}
			packet = append(packet, encoded)
		case sim.ResourceChangeIntent:
			encoded, err := e.encodeResource(intent)
			if err != nil {
				return nil, nil, fmt.Errorf("resource[%d]: %w", index, err)
			}
			packet = append(packet, encoded)
		case sim.AreaPulseIntent:
			pulse = append(pulse, intent)
		default:
			encoded, err := e.encoder.EncodeBatch(ctx, []sim.Event{event})
			if err != nil {
				return nil, nil, fmt.Errorf("event[%d]: %w", index, err)
			}
			packet = append(packet, encoded...)
		}
	}
	return packet, pulse, nil
}

func (e *targetedAOEEncoder) encodeSpawn(
	intent sim.SpawnIntent,
) ([]byte, error) {
	if len(e.req.Ability.EffectNouns) == 0 ||
		intent.NounName != e.req.Ability.EffectNouns[0] {
		return nil, errors.New("unexpected noun")
	}
	packet, err := raknet.MarshalApplication(raknet.ObjectCreateMessage{
		ObjectID:  e.req.EffectObjectID,
		Noun:      util.HashID(intent.NounName),
		PositionX: e.req.Position.X,
		PositionY: e.req.Position.Y,
		PositionZ: e.req.Position.Z,
		Scale:     1,
		Team:      e.req.Team,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	return packet, nil
}

func (e *targetedAOEEncoder) encodeCooldown(
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

func (e *targetedAOEEncoder) encodeResource(
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
