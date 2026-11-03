package sqlstore

import (
	"context"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/activity"
)

type pvpOutcomeRow struct {
	DayAt           int64  `db:"day_at"`
	MatchCount      uint64 `db:"match_count"`
	PlaytimeSeconds uint64 `db:"playtime_second"`
}

func (e *Store) readUserPVP(ctx context.Context, userID int64, req activity.Window) ([]activity.PVPOutcomeSample, error) {
	rows := make([]pvpOutcomeRow, 0)
	// A first-to-two result is a completed best-of-three receipt. Round-only and
	// malformed legacy receipts cannot become completed-match graph points.
	// Aggregate in SQL to bound the result by UTC days without synthesizing gaps.
	err := e.db.SelectContext(ctx, &rows, e.db.Rebind(`SELECT
		(completed_at_unix / 86400) * 86400 AS day_at,
		COUNT(*) AS match_count, SUM(play_time_second) AS playtime_second
		FROM pvp_match_result
		WHERE user_id = ? AND completed_at_unix >= ? AND completed_at_unix <= ?
		AND round_win >= 0 AND round_loss >= 0
		AND round_win + round_loss BETWEEN 2 AND 3
		AND ((round_win = 2 AND round_loss < 2 AND is_victory = 1)
			OR (round_loss = 2 AND round_win < 2 AND is_victory = 0))
		GROUP BY 1 ORDER BY 1`),
		userID, req.Since.UTC().Truncate(24*time.Hour).Unix(), req.Until.Unix())
	if err != nil {
		return nil, fmt.Errorf("pvpHistory: %w", err)
	}
	samples := make([]activity.PVPOutcomeSample, 0, len(rows))
	for _, row := range rows {
		samples = append(samples, activity.PVPOutcomeSample{
			At: time.Unix(row.DayAt, 0).UTC(), MatchCount: row.MatchCount,
			PlaytimeSeconds: row.PlaytimeSeconds,
		})
	}
	return samples, nil
}
