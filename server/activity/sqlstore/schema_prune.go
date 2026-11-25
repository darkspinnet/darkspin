package sqlstore

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
)

// pruneMetrics removes discarded graph metrics while retaining the daily level
// and progression observations already collected by development deployments.
func (e *Store) pruneMetrics(ctx context.Context, tx *sqlx.Tx) error {
	query := `SELECT name FROM pragma_table_info('activity_user_sample')`
	if e.isPostgres {
		query = `SELECT column_name FROM information_schema.columns
 WHERE table_schema = CURRENT_SCHEMA() AND table_name = 'activity_user_sample'`
	}
	columns := make([]string, 0)
	err := tx.SelectContext(ctx, &columns, query)
	if err != nil {
		return fmt.Errorf("pruneColumns: %w", err)
	}
	for _, column := range columns {
		switch column {
		case "pve_play_time_second", "pvp_play_time_second", "pve_total_kill", "pvp_player_kill",
			"pve_death", "pvp_death", "pvp_win", "pvp_loss":
			// Only the explicit obsolete column allowlist can enter this statement.
		default:
			continue
		}
		err = execStatement(ctx, tx, `ALTER TABLE activity_user_sample DROP COLUMN `+column)
		if err != nil {
			return fmt.Errorf("pruneColumn[%s]: %w", column, err)
		}
	}
	err = execStatement(ctx, tx, `DROP TABLE IF EXISTS activity_campaign_day`)
	if err != nil {
		return fmt.Errorf("pruneCampaign: %w", err)
	}
	err = execStatement(ctx, tx, `DELETE FROM activity_collection WHERE id = 6`)
	if err != nil {
		return fmt.Errorf("pruneCollection: %w", err)
	}
	return nil
}
