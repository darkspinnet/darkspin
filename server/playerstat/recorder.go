// Package playerstat owns persistent gameplay-stat recording.
package playerstat

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

// Store persists one player's accumulated stat delta.
type Store interface {
	RecordPlayerStats(context.Context, int64, sporenet.PlayerStatDelta) error
}

// Recorder bounds asynchronous stat persistence independently of a transport
// request's cancellation.
type Recorder struct {
	store Store
}

// NewRecorder creates a stat recorder backed by store.
func NewRecorder(store Store) *Recorder {
	return &Recorder{store: store}
}

// Record persists a nonempty stat delta for binding.
func (r *Recorder) Record(
	ctx context.Context, binding game.GameplayBinding,
	statDelta sporenet.PlayerStatDelta,
) error {
	if statDelta == (sporenet.PlayerStatDelta{}) || r == nil || r.store == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("player stats unavailable")
	}
	statContext, cancel := context.WithTimeout(
		context.WithoutCancel(ctx), 5*time.Second,
	)
	defer cancel()
	err := r.store.RecordPlayerStats(
		statContext, int64(binding.UserID), statDelta,
	)
	if err != nil {
		return fmt.Errorf("playerStatsRecord: %w", err)
	}
	return nil
}
