package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

// LoadProfileSummary reads identity and lifetime counters without inventory.
func (e *Repository) LoadProfileSummary(ctx context.Context, userID int64) (sporenet.ProfileSummary, error) {
	if ctx == nil {
		return sporenet.ProfileSummary{}, errors.New("nil context")
	}
	if userID <= 0 {
		return sporenet.ProfileSummary{}, fmt.Errorf("summaryID: %w", sporenet.ErrUserNotFound)
	}
	tx, err := e.db.BeginTxx(ctx, e.readOptions())
	if err != nil {
		return sporenet.ProfileSummary{}, fmt.Errorf("summaryBegin: %w", err)
	}
	defer rollback(tx)
	row := userIdentityRow{}
	err = tx.GetContext(ctx, &row, tx.Rebind(`
		SELECT id, login_name, display_name, create_dt, last_connection_dt,
			avatar_id, level, xp, chain_progression, onboarding_progress,
			is_tutorial_completion_pending
		FROM "user" WHERE id = ? AND trmt_id = 0`), userID)
	if errors.Is(err, sql.ErrNoRows) {
		return sporenet.ProfileSummary{}, fmt.Errorf("summaryMissing: %w", sporenet.ErrUserNotFound)
	}
	if err != nil {
		return sporenet.ProfileSummary{}, fmt.Errorf("summaryIdentity: %w", err)
	}
	createDT, err := time.Parse(time.RFC3339Nano, row.CreateDT)
	if err != nil {
		return sporenet.ProfileSummary{}, fmt.Errorf("summaryCreateDT: %w", err)
	}
	lastConnectionDT := time.Time{}
	if row.LastConnectionDT != "" {
		lastConnectionDT, err = time.Parse(time.RFC3339Nano, row.LastConnectionDT)
		if err != nil {
			return sporenet.ProfileSummary{}, fmt.Errorf("summaryConnectionDT: %w", err)
		}
	}
	stat := userStatRow{}
	err = tx.GetContext(ctx, &stat, tx.Rebind(`
		SELECT user_id, dna_earned, pve_play_time_second, pve_minion_kill, pve_special_kill,
			pve_boss_kill, pve_total_kill, pve_death, pve_damage_dealt,
			pve_damage_taken, pve_damage_maximum, pve_healing,
			pve_healing_received, pve_healing_maximum, pvp_play_time_second,
			pvp_win, pvp_loss, pvp_player_kill, pvp_death, pvp_damage_dealt,
			pvp_damage_taken, pvp_damage_maximum, pvp_healing,
			pvp_healing_received, pvp_healing_maximum
		FROM user_stat WHERE user_id = ?`), userID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return sporenet.ProfileSummary{}, fmt.Errorf("summaryStat: %w", err)
	}
	err = tx.Commit()
	if err != nil {
		return sporenet.ProfileSummary{}, fmt.Errorf("summaryCommit: %w", err)
	}
	return sporenet.ProfileSummary{Identity: sporenet.UserIdentity{
		ID: row.ID, LoginName: row.LoginName, DisplayName: row.DisplayName,
		CreateDT: createDT, LastConnectionDT: lastConnectionDT,
		AvatarID: row.AvatarID, Level: row.Level, XP: row.XP,
		ChainProgression:            row.ChainProgression,
		IsTutorialCompleted:         sporenet.Account{OnboardingProgress: row.OnboardingProgress}.IsTutorialCompleted(),
		IsTutorialCompletionPending: row.IsTutorialCompletionPending != 0,
	}, Stats: stat.stats()}, nil
}
