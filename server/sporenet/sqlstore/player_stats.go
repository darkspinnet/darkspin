package sqlstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/jmoiron/sqlx"
)

// SavePlayerStats updates the hot combat-stat row without rewriting the user's
// creatures, equipment, rewards and other unrelated profile collections.
// DNA earnings belong to full reward saves so the balance and earnings commit together.
func (e *Repository) SavePlayerStats(ctx context.Context, userID int64, stats sporenet.PlayerStats) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if userID <= 0 {
		return sporenet.ErrInvalidUser
	}
	result, err := e.db.NamedExecContext(ctx, `
		UPDATE user_stat SET
			pve_play_time_second = :pve_play_time_second,
			pve_minion_kill = :pve_minion_kill,
			pve_special_kill = :pve_special_kill,
			pve_boss_kill = :pve_boss_kill,
			pve_total_kill = :pve_total_kill,
			pve_death = :pve_death,
			pve_damage_dealt = :pve_damage_dealt,
			pve_damage_taken = :pve_damage_taken,
			pve_damage_maximum = :pve_damage_maximum,
			pve_healing = :pve_healing,
			pve_healing_received = :pve_healing_received,
			pve_healing_maximum = :pve_healing_maximum,
			pvp_play_time_second = :pvp_play_time_second,
			pvp_win = :pvp_win,
			pvp_loss = :pvp_loss,
			pvp_player_kill = :pvp_player_kill,
			pvp_death = :pvp_death,
			pvp_damage_dealt = :pvp_damage_dealt,
			pvp_damage_taken = :pvp_damage_taken,
			pvp_damage_maximum = :pvp_damage_maximum,
			pvp_healing = :pvp_healing,
			pvp_healing_received = :pvp_healing_received,
			pvp_healing_maximum = :pvp_healing_maximum
		WHERE user_id = :user_id AND EXISTS (
			SELECT 1 FROM "user" WHERE id = :user_id AND trmt_id = 0
		)`, userStatRowFromStats(userID, stats))
	if err != nil {
		return fmt.Errorf("statUpdate: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("statAffected: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("statMissing: %w", sporenet.ErrUserNotFound)
	}
	return nil
}

func ensureDNAEarnedColumn(ctx context.Context, tx *sqlx.Tx) error {
	var count int
	err := tx.GetContext(ctx, &count, `SELECT COUNT(*) FROM pragma_table_info('user_stat') WHERE name = 'dna_earned'`)
	if err != nil {
		return fmt.Errorf("columnCheck: %w", err)
	}
	if count != 0 {
		return nil
	}
	result, err := tx.ExecContext(ctx, `ALTER TABLE user_stat ADD COLUMN dna_earned BIGINT NOT NULL DEFAULT 0`)
	if err != nil {
		return fmt.Errorf("columnAdd: %w", err)
	}
	affectedCount, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("columnAffected: %w", err)
	}
	if affectedCount < 0 {
		return errors.New("negative DNA column affected count")
	}
	return nil
}
