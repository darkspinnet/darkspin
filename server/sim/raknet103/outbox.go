package raknet103

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/sim"
)

// PacketOutbox adapts packet-only build-103 intents to the generic
// simulator ports. Gameplay mutations continue to use feature-owned outboxes.
type PacketOutbox struct {
	encoder *Encoder
	packets [][]byte
}

func NewPacketOutbox(encoder *Encoder) (*PacketOutbox, error) {
	if encoder == nil {
		return nil, errors.New("nil encoder")
	}
	return &PacketOutbox{encoder: encoder}, nil
}

func (o *PacketOutbox) SetGraphicsState(
	ctx context.Context, meta sim.EventMeta, intent sim.GraphicsStateIntent,
) error {
	return o.Encode(ctx, meta, intent)
}

func (o *PacketOutbox) SetPhysicsState(
	ctx context.Context, meta sim.EventMeta, intent sim.PhysicsStateIntent,
) error {
	return o.Encode(ctx, meta, intent)
}

func (o *PacketOutbox) SetVisibility(
	ctx context.Context, meta sim.EventMeta, intent sim.VisibilityIntent,
) error {
	return o.Encode(ctx, meta, intent)
}

func (o *PacketOutbox) Animate(
	ctx context.Context, meta sim.EventMeta, intent sim.AnimationIntent,
) error {
	return o.Encode(ctx, meta, intent)
}

func (o *PacketOutbox) ApplyEffect(
	ctx context.Context, meta sim.EventMeta, intent sim.EffectIntent,
) error {
	return o.Encode(ctx, meta, intent)
}

func (o *PacketOutbox) StartCinematic(
	ctx context.Context, meta sim.EventMeta, intent sim.CinematicIntent,
) error {
	return o.Encode(ctx, meta, intent)
}

func (o *PacketOutbox) ShowDialogue(
	ctx context.Context, meta sim.EventMeta, intent sim.DialogueIntent,
) error {
	return o.Encode(ctx, meta, intent)
}

func (o *PacketOutbox) Notify(
	ctx context.Context, meta sim.EventMeta, intent sim.ClientEventIntent,
) error {
	return o.Encode(ctx, meta, intent)
}

func (o *PacketOutbox) ResetAnimation(
	ctx context.Context, meta sim.EventMeta, intent sim.AnimationResetIntent,
) error {
	return o.Encode(ctx, meta, intent)
}

func (o *PacketOutbox) ApplyPositionedEffect(
	ctx context.Context, meta sim.EventMeta, intent sim.PositionedEffectIntent,
) error {
	return o.Encode(ctx, meta, intent)
}

func (o *PacketOutbox) Encode(ctx context.Context, meta sim.EventMeta, intent sim.Intent) error {
	if o == nil || o.encoder == nil {
		return errors.New("encoder missing")
	}
	event := sim.Event{
		ID: meta.ID, At: meta.At, Phase: meta.Phase, Provenance: meta.Provenance, Intent: intent,
	}
	packets, err := o.encoder.EncodeBatch(ctx, []sim.Event{event})
	if err != nil {
		return fmt.Errorf("eventEncode[%T]: %w", intent, err)
	}
	o.packets = append(o.packets, packets...)
	return nil
}

func (o *PacketOutbox) Drain() [][]byte {
	if o == nil {
		return nil
	}
	packets := o.packets
	o.packets = nil
	return packets
}

func (o *PacketOutbox) Append(packets ...[]byte) {
	if o == nil {
		return
	}
	o.packets = append(o.packets, packets...)
}
