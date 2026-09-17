package raknet103

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/sim"
	simraknet103 "github.com/darkspinnet/darkspin/server/sim/raknet103"
	zonespawn "github.com/darkspinnet/darkspin/server/zone/spawn"
)

type Definition struct {
	ObjectID uint32
	Position sim.Position
}

type resolver struct {
	binding simraknet103.Binding
}

func (r resolver) ResolveRole(
	_ context.Context, role sim.Role,
) (simraknet103.Binding, error) {
	if role != zonespawn.NPCRole {
		return simraknet103.Binding{}, fmt.Errorf("roleMissing: %s", role)
	}
	return r.binding, nil
}

type Run struct {
	run     *zonespawn.Run
	encoder *simraknet103.Encoder
}

func New(
	program sim.Program, definition Definition, sourceTime uint64,
) (*Run, [][]byte, error) {
	if definition.ObjectID == 0 || !isFinitePosition(definition.Position) {
		return nil, nil, errors.New("invalid spawn modifier npc")
	}
	encoder, err := simraknet103.NewEncoder(resolver{binding: simraknet103.Binding{
		ObjectID: definition.ObjectID,
		Position: definition.Position,
	}}, sourceTime)
	if err != nil {
		return nil, nil, fmt.Errorf("encoderCreate: %w", err)
	}
	run, event, err := zonespawn.New(program)
	if err != nil {
		return nil, nil, fmt.Errorf("runCreate: %w", err)
	}
	packet, err := encoder.EncodeBatch(context.Background(), event)
	if err != nil {
		run.Stop()
		return nil, nil, fmt.Errorf("eventEncode: %w", err)
	}
	return &Run{run: run, encoder: encoder}, packet, nil
}

func (r *Run) Advance(ctx context.Context, deadline time.Duration) ([][]byte, error) {
	if r == nil || r.run == nil || r.encoder == nil {
		return nil, errors.New("invalid spawn modifier adapter")
	}
	event, err := r.run.Advance(ctx, deadline)
	if err != nil {
		return nil, fmt.Errorf("runAdvance: %w", err)
	}
	packet, err := r.encoder.EncodeBatch(ctx, event)
	if err != nil {
		return nil, fmt.Errorf("eventEncode: %w", err)
	}
	return packet, nil
}

func (r *Run) Stop() {
	if r == nil || r.run == nil {
		return
	}
	r.run.Stop()
}

func (r *Run) IsImmobilized() bool {
	return r != nil && r.run != nil && r.run.IsImmobilized()
}

func isFinitePosition(position sim.Position) bool {
	return isFinite(position.X) && isFinite(position.Y) && isFinite(position.Z)
}

func isFinite(number float32) bool {
	return !math.IsNaN(float64(number)) && !math.IsInf(float64(number), 0)
}
