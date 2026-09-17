package sporenet

import (
	"context"
	"errors"
	"fmt"
	"math"
)

// PlayerStats contains durable lifetime counters shown by View Profile.
// Ratios and totals derived from these counters remain projection concerns.
type PlayerStats struct {
	PVEPlayTimeSecond  uint64
	PVEMinionKill      uint64
	PVESpecialKill     uint64
	PVEBossKill        uint64
	PVETotalKill       uint64
	PVEDeath           uint64
	PVEDamageDealt     float64
	PVEDamageTaken     float64
	PVEDamageMaximum   float64
	PVEHealing         float64
	PVEHealingReceived float64
	PVEHealingMaximum  float64
	PVPPlayTimeSecond  uint64
	PVPWin             uint64
	PVPLoss            uint64
	PVPPlayerKill      uint64
	PVPDeath           uint64
	PVPDamageDealt     float64
	PVPDamageTaken     float64
	PVPDamageMaximum   float64
	PVPHealing         float64
	PVPHealingReceived float64
	PVPHealingMaximum  float64
}

// PlayerStatDelta is one accepted authoritative gameplay result. Damage and
// healing amounts are single-event values so the aggregate can update maxima.
type PlayerStatDelta struct {
	PVEPlayTimeSecond  uint64
	PVEMinionKill      uint64
	PVESpecialKill     uint64
	PVEBossKill        uint64
	PVEDeath           uint64
	PVEDamageDealt     float64
	PVEDamageTaken     float64
	PVEHealing         float64
	PVEHealingReceived float64
	PVPPlayTimeSecond  uint64
	PVPWin             uint64
	PVPLoss            uint64
	PVPPlayerKill      uint64
	PVPDeath           uint64
	PVPDamageDealt     float64
	PVPDamageTaken     float64
	PVPHealing         float64
	PVPHealingReceived float64
}

// RecordPlayerStats atomically applies and persists one lifetime-stat delta.
func (m *UserManager) RecordPlayerStats(ctx context.Context, userID int64, delta PlayerStatDelta) error {
	err := validatePlayerStatDelta(delta)
	if err != nil {
		return fmt.Errorf("statValidate: %w", err)
	}
	user := m.UserByID(userID)
	if user == nil {
		return ErrInvalidUser
	}
	user.mutation.Lock()
	defer user.mutation.Unlock()
	user.mu.Lock()
	previous := user.Stats
	err = applyPlayerStatDelta(&user.Stats, delta)
	user.mu.Unlock()
	if err != nil {
		return fmt.Errorf("statApply: %w", err)
	}
	if delta == (PlayerStatDelta{}) {
		return nil
	}
	err = m.repository.Save(ctx, user.Record())
	if err != nil {
		user.mu.Lock()
		user.Stats = previous
		user.mu.Unlock()
		return fmt.Errorf("statSave: %w", err)
	}
	return nil
}

func validatePlayerStatDelta(delta PlayerStatDelta) error {
	numbers := []float64{
		delta.PVEDamageDealt, delta.PVEDamageTaken,
		delta.PVEHealing, delta.PVEHealingReceived,
		delta.PVPDamageDealt, delta.PVPDamageTaken,
		delta.PVPHealing, delta.PVPHealingReceived,
	}
	for _, number := range numbers {
		if math.IsNaN(number) || math.IsInf(number, 0) || number < 0 {
			return errors.New("invalid stat amount")
		}
	}
	return nil
}

func applyPlayerStatDelta(stats *PlayerStats, delta PlayerStatDelta) error {
	if stats == nil {
		return errors.New("nil player stats")
	}
	counts := []struct {
		current *uint64
		amount  uint64
	}{
		{current: &stats.PVEPlayTimeSecond, amount: delta.PVEPlayTimeSecond},
		{current: &stats.PVEMinionKill, amount: delta.PVEMinionKill},
		{current: &stats.PVESpecialKill, amount: delta.PVESpecialKill},
		{current: &stats.PVEBossKill, amount: delta.PVEBossKill},
		{current: &stats.PVEDeath, amount: delta.PVEDeath},
		{current: &stats.PVPPlayTimeSecond, amount: delta.PVPPlayTimeSecond},
		{current: &stats.PVPWin, amount: delta.PVPWin},
		{current: &stats.PVPLoss, amount: delta.PVPLoss},
		{current: &stats.PVPPlayerKill, amount: delta.PVPPlayerKill},
		{current: &stats.PVPDeath, amount: delta.PVPDeath},
	}
	for _, entry := range counts {
		if entry.amount > ^uint64(0)-*entry.current {
			return errors.New("stat count overflow")
		}
	}
	totalKillDelta := delta.PVEMinionKill + delta.PVESpecialKill
	if totalKillDelta < delta.PVEMinionKill || delta.PVEBossKill > ^uint64(0)-totalKillDelta {
		return errors.New("stat kill overflow")
	}
	totalKillDelta += delta.PVEBossKill
	if totalKillDelta > ^uint64(0)-stats.PVETotalKill {
		return errors.New("stat total kill overflow")
	}
	for _, entry := range counts {
		*entry.current += entry.amount
	}
	stats.PVETotalKill += totalKillDelta
	stats.PVEDamageDealt += delta.PVEDamageDealt
	stats.PVEDamageTaken += delta.PVEDamageTaken
	stats.PVEHealing += delta.PVEHealing
	stats.PVEHealingReceived += delta.PVEHealingReceived
	stats.PVEDamageMaximum = max(stats.PVEDamageMaximum, delta.PVEDamageDealt)
	stats.PVEHealingMaximum = max(stats.PVEHealingMaximum, delta.PVEHealing)
	stats.PVPDamageDealt += delta.PVPDamageDealt
	stats.PVPDamageTaken += delta.PVPDamageTaken
	stats.PVPHealing += delta.PVPHealing
	stats.PVPHealingReceived += delta.PVPHealingReceived
	stats.PVPDamageMaximum = max(stats.PVPDamageMaximum, delta.PVPDamageDealt)
	stats.PVPHealingMaximum = max(stats.PVPHealingMaximum, delta.PVPHealing)
	return nil
}
