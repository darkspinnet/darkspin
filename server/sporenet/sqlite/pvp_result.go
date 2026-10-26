package sqlite

import (
	"context"
	"fmt"

	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/jmoiron/sqlx"
)

type pvpMatchResultRow struct {
	UserID          int64   `db:"user_id"`
	MatchID         string  `db:"match_id"`
	CompletedAtUnix int64   `db:"completed_at_unix"`
	IsVictory       int64   `db:"is_victory"`
	IsRanked        int64   `db:"is_ranked"`
	RoundWins       uint32  `db:"round_win"`
	RoundLosses     uint32  `db:"round_loss"`
	PlayTimeSecond  uint64  `db:"play_time_second"`
	PlayerKills     uint64  `db:"player_kill"`
	Deaths          uint64  `db:"death"`
	DamageDealt     float64 `db:"damage_dealt"`
	DamageTaken     float64 `db:"damage_taken"`
	DamageMaximum   float64 `db:"damage_maximum"`
	Healing         float64 `db:"healing"`
	HealingReceived float64 `db:"healing_received"`
	HealingMaximum  float64 `db:"healing_maximum"`
}

func writePVPResults(ctx context.Context, tx *sqlx.Tx, record sporenet.UserRecord) error {
	for index, result := range record.PVPResults {
		row := pvpMatchResultRow{
			UserID: record.Account.ID, MatchID: result.MatchID, CompletedAtUnix: result.CompletedAtUnix,
			IsVictory: boolInteger(result.IsVictory), IsRanked: boolInteger(result.IsRanked),
			RoundWins: result.RoundWins, RoundLosses: result.RoundLosses, PlayTimeSecond: result.PlayTimeSecond,
			PlayerKills: result.PlayerKills, Deaths: result.Deaths, DamageDealt: result.DamageDealt,
			DamageTaken: result.DamageTaken, DamageMaximum: result.DamageMaximum,
			Healing: result.Healing, HealingReceived: result.HealingReceived, HealingMaximum: result.HealingMaximum,
		}
		resultExec, err := tx.NamedExecContext(ctx, `
			INSERT INTO pvp_match_result (
				user_id, match_id, completed_at_unix, is_victory, is_ranked, round_win, round_loss,
				play_time_second, player_kill, death, damage_dealt, damage_taken, damage_maximum,
				healing, healing_received, healing_maximum
			) VALUES (
				:user_id, :match_id, :completed_at_unix, :is_victory, :is_ranked, :round_win, :round_loss,
				:play_time_second, :player_kill, :death, :damage_dealt, :damage_taken, :damage_maximum,
				:healing, :healing_received, :healing_maximum
			)`, row)
		if err != nil {
			return fmt.Errorf("pvpInsert[%d]: %w", index, err)
		}
		rowCount, err := resultExec.RowsAffected()
		if err != nil {
			return fmt.Errorf("pvpRows[%d]: %w", index, err)
		}
		if rowCount != 1 {
			return fmt.Errorf("pvpInsert[%d]: expected one result row, got %d", index, rowCount)
		}
	}
	return nil
}

func loadPVPResults(ctx context.Context, tx *sqlx.Tx, userID int64) ([]sporenet.PVPMatchResult, error) {
	rows := make([]pvpMatchResultRow, 0)
	err := tx.SelectContext(ctx, &rows, `SELECT * FROM pvp_match_result WHERE user_id = ? ORDER BY match_id`, userID)
	if err != nil {
		return nil, fmt.Errorf("pvpSelect: %w", err)
	}
	results := make([]sporenet.PVPMatchResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, sporenet.PVPMatchResult{
			MatchID: row.MatchID, CompletedAtUnix: row.CompletedAtUnix,
			IsVictory: row.IsVictory != 0, IsRanked: row.IsRanked != 0,
			RoundWins: row.RoundWins, RoundLosses: row.RoundLosses, PlayTimeSecond: row.PlayTimeSecond,
			PlayerKills: row.PlayerKills, Deaths: row.Deaths, DamageDealt: row.DamageDealt,
			DamageTaken: row.DamageTaken, DamageMaximum: row.DamageMaximum,
			Healing: row.Healing, HealingReceived: row.HealingReceived, HealingMaximum: row.HealingMaximum,
		})
	}
	return results, nil
}
