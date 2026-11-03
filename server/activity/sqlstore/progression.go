package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/activity"
	"github.com/jmoiron/sqlx"
)

type progressionRow struct {
	DayAt                    int64           `db:"day_at"`
	ObservedAt               int64           `db:"observed_at"`
	AverageProgression       sql.NullFloat64 `db:"average_progression"`
	HighestCompletedCampaign sql.NullInt64   `db:"highest_completed_campaign"`
	AccountCount             int64           `db:"account_count"`
}

// recordProgression preserves today's first observation. Offline days remain
// absent; saved chain_progression is the highest completed campaign index.
func recordProgression(ctx context.Context, tx *sqlx.Tx, at time.Time) error {
	day := at.UTC().Truncate(24 * time.Hour).Unix()
	var sampleCount int64
	err := tx.GetContext(ctx, &sampleCount, tx.Rebind(`SELECT COUNT(*) FROM activity_progression_sample WHERE day_at = ?`), day)
	if err != nil {
		return fmt.Errorf("progressionExists: %w", err)
	}
	if sampleCount > 0 {
		return nil
	}
	err = execStatement(ctx, tx, `INSERT INTO activity_progression_sample
 (day_at, observed_at, average_progression, highest_completed_campaign, account_count)
 SELECT ?, ?, AVG(CAST(chain_progression AS DOUBLE PRECISION)), MAX(chain_progression), COUNT(*)
 FROM "user" WHERE trmt_id = 0 ON CONFLICT (day_at) DO NOTHING`, day, at.UTC().Unix())
	if err != nil {
		return fmt.Errorf("progressionInsert: %w", err)
	}
	return nil
}

func readProgression(ctx context.Context, tx *sqlx.Tx, req activity.Window) (progressionRow, []activity.ProgressionSample, error) {
	var latest progressionRow
	err := tx.GetContext(ctx, &latest, tx.Rebind(`SELECT day_at, observed_at, average_progression, highest_completed_campaign, account_count
 FROM activity_progression_sample WHERE observed_at <= ? ORDER BY day_at DESC LIMIT 1`), req.Until.Unix())
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return progressionRow{}, nil, fmt.Errorf("progressionLatest: %w", err)
	}
	progressionRows := make([]progressionRow, 0)
	err = tx.SelectContext(ctx, &progressionRows, tx.Rebind(`SELECT day_at, observed_at, average_progression, highest_completed_campaign, account_count
 FROM activity_progression_sample WHERE day_at >= ? AND day_at <= ? AND observed_at <= ? ORDER BY day_at`),
		req.Since.UTC().Truncate(24*time.Hour).Unix(), req.Until.Unix(), req.Until.Unix())
	if err != nil {
		return progressionRow{}, nil, fmt.Errorf("progressionHistory: %w", err)
	}
	progressionSamples := make([]activity.ProgressionSample, 0, len(progressionRows))
	for _, row := range progressionRows {
		progressionSamples = append(progressionSamples, activity.ProgressionSample{
			At: time.Unix(row.DayAt, 0).UTC(), ObservedAt: time.Unix(row.ObservedAt, 0).UTC(), AverageProgression: row.AverageProgression.Float64,
			HighestCompletedCampaign: uint32(row.HighestCompletedCampaign.Int64), AccountCount: row.AccountCount,
		})
	}
	return latest, progressionSamples, nil
}
