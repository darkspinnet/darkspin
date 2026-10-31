// Package playerstat owns persistent gameplay-stat recording.
package playerstat

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

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
	store        Store
	mutex        sync.Mutex
	missionStats map[missionStatKey]MissionStat
}

type missionStatKey struct {
	gameID uint32
	level  string
	slot   uint16
}

// MissionStat is one participant's accepted activity in a campaign mission.
type MissionStat struct {
	Kills           uint64
	DamageDealt     float64
	DamageTaken     float64
	HealingDone     float64
	HealingReceived float64
	updatedAt       time.Time
}

// NewRecorder creates a stat recorder backed by store.
func NewRecorder(store Store) *Recorder {
	return &Recorder{store: store, missionStats: make(map[missionStatKey]MissionStat)}
}

// SnapshotMission returns the four native result rows in player-slot order.
func (r *Recorder) SnapshotMission(binding game.GameplayBinding) [4]MissionStat {
	stats := [4]MissionStat{}
	if r == nil {
		return stats
	}
	r.mutex.Lock()
	defer r.mutex.Unlock()
	for slot := range stats {
		key := missionStatKey{gameID: binding.GameID, level: binding.Level, slot: uint16(slot)}
		stats[slot] = r.missionStats[key]
	}
	return stats
}

func (r *Recorder) recordMission(binding game.GameplayBinding, delta sporenet.PlayerStatDelta) {
	if binding.Mode != game.ModeChain || binding.Slot >= 4 {
		return
	}
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if len(r.missionStats) > 2048 {
		cutoff := time.Now().Add(-2 * time.Hour)
		for key, stat := range r.missionStats {
			if stat.updatedAt.Before(cutoff) {
				delete(r.missionStats, key)
			}
		}
	}
	key := missionStatKey{gameID: binding.GameID, level: binding.Level, slot: binding.Slot}
	stat := r.missionStats[key]
	stat.Kills += delta.PVEMinionKill + delta.PVESpecialKill + delta.PVEBossKill
	stat.DamageDealt += delta.PVEDamageDealt
	stat.DamageTaken += delta.PVEDamageTaken
	stat.HealingDone += delta.PVEHealing
	stat.HealingReceived += delta.PVEHealingReceived
	stat.updatedAt = time.Now()
	r.missionStats[key] = stat
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
		r.recordMission(binding, statDelta)
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
	r.recordMission(binding, statDelta)
	return nil
}
