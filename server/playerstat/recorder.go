// Package playerstat owns persistent gameplay-stat recording.
package playerstat

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

// Store persists one player's accumulated stat delta.
type Store interface {
	RecordPlayerStats(context.Context, int64, sporenet.PlayerStatDelta) error
}

// Recorder applies accepted combat counters independently of transport
// cancellation. The runtime profile save buffer owns disk timing and deadlines.
type Recorder struct {
	store Store
}

// NewRecorder creates a stat recorder backed by store.
func NewRecorder(store Store) *Recorder {
	return &Recorder{store: store}
}

// Record accepts a nonempty stat delta for binding into the profile snapshot.
func (r *Recorder) Record(
	ctx context.Context, binding game.GameplayBinding,
	statDelta sporenet.PlayerStatDelta,
) error {
	if statDelta == (sporenet.PlayerStatDelta{}) || r == nil || r.store == nil {
		return nil
	}
	if binding.IsServerBot {
		return nil
	}
	if ctx == nil {
		return errors.New("player stats unavailable")
	}
	err := r.store.RecordPlayerStats(
		context.WithoutCancel(ctx), int64(binding.UserID), statDelta,
	)
	if err != nil {
		return fmt.Errorf("playerStatsRecord: %w", err)
	}
	return nil
}
