package sporenet

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
)

// PVPMatchResult is one player's immutable completed-match receipt. Totals and
// event maxima are independent: a whole match's damage is not one large hit.
type PVPMatchResult struct {
	MatchID         string
	CompletedAtUnix int64
	IsVictory       bool
	IsRanked        bool
	RoundWins       uint32
	RoundLosses     uint32
	PlayTimeSecond  uint64
	PlayerKills     uint64
	Deaths          uint64
	DamageDealt     float64
	DamageTaken     float64
	DamageMaximum   float64
	Healing         float64
	HealingReceived float64
	HealingMaximum  float64
}

func (e PVPMatchResult) validate() error {
	identity, err := hex.DecodeString(e.MatchID)
	if err != nil {
		return fmt.Errorf("matchIdentity: %w", err)
	}
	if len(identity) != 16 || e.CompletedAtUnix <= 0 || e.RoundWins > 3 || e.RoundLosses > 3 ||
		e.RoundWins+e.RoundLosses == 0 || e.RoundWins+e.RoundLosses > 3 ||
		e.IsVictory != (e.RoundWins > e.RoundLosses) {
		return errors.New("invalid completed PVP match")
	}
	amounts := []float64{e.DamageDealt, e.DamageTaken, e.DamageMaximum, e.Healing, e.HealingReceived, e.HealingMaximum}
	for _, amount := range amounts {
		if amount < 0 || math.IsNaN(amount) || math.IsInf(amount, 0) {
			return errors.New("invalid PVP match amount")
		}
	}
	if e.DamageMaximum > e.DamageDealt || e.HealingMaximum > e.Healing {
		return errors.New("PVP event maximum exceeds match total")
	}
	return nil
}

// RecordPVPMatch commits the receipt and lifetime counters in one repository
// transaction. A repeated completed match is a no-op, including after reload.
func (e *UserManager) RecordPVPMatch(ctx context.Context, userID int64, req PVPMatchResult) error {
	if ctx == nil {
		return errors.New("PVP result context unavailable")
	}
	err := req.validate()
	if err != nil {
		return fmt.Errorf("pvpValidate: %w", err)
	}
	if e == nil || e.repository == nil || userID <= 0 {
		return fmt.Errorf("pvpUser: %w", ErrInvalidUser)
	}
	repository, isSupported := e.repository.(UserProfileRepository)
	if !isSupported {
		return errors.New("PVP profile lookup unavailable")
	}
	record, err := repository.LoadByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("pvpIdentity: %w", err)
	}
	session := e.sessionLock(record.LoginName)
	session.Lock()
	defer session.Unlock()
	user := e.UserByID(userID)
	if user == nil {
		// Reload under the login/logout lock, so an offline save cannot replace
		// a newer profile or race with a new login.
		record, err = repository.LoadByID(ctx, userID)
		if err != nil {
			return fmt.Errorf("pvpReload: %w", err)
		}
		user = NewUserFromRecord(record, e.template)
	}
	user.mutation.Lock()
	defer user.mutation.Unlock()
	user.mu.Lock()
	for _, result := range user.PVPResults {
		if result.MatchID != req.MatchID {
			continue
		}
		user.mu.Unlock()
		if result != req {
			return errors.New("PVP match receipt conflict")
		}
		return nil
	}
	previous := user.Stats
	next := previous
	delta := PlayerStatDelta{
		PVPPlayTimeSecond: req.PlayTimeSecond, PVPPlayerKill: req.PlayerKills, PVPDeath: req.Deaths,
		PVPDamageDealt: req.DamageDealt, PVPDamageTaken: req.DamageTaken,
		PVPHealing: req.Healing, PVPHealingReceived: req.HealingReceived,
	}
	if req.IsVictory {
		delta.PVPWin = 1
	} else {
		delta.PVPLoss = 1
	}
	err = applyPlayerStatDelta(&next, delta)
	if err != nil {
		user.mu.Unlock()
		return fmt.Errorf("pvpStats: %w", err)
	}
	next.PVPDamageMaximum = max(previous.PVPDamageMaximum, req.DamageMaximum)
	next.PVPHealingMaximum = max(previous.PVPHealingMaximum, req.HealingMaximum)
	previousResults := user.PVPResults
	user.Stats = next
	user.PVPResults = append(append([]PVPMatchResult(nil), previousResults...), req)
	user.mu.Unlock()
	err = e.repository.Save(ctx, user.Record())
	if err != nil {
		user.mu.Lock()
		user.Stats = previous
		user.PVPResults = previousResults
		user.mu.Unlock()
		return fmt.Errorf("pvpSave: %w", err)
	}
	return nil
}
