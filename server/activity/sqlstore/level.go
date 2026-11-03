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

type levelRow struct {
	DayAt        int64           `db:"day_at"`
	ObservedAt   int64           `db:"observed_at"`
	AverageLevel sql.NullFloat64 `db:"average_level"`
	AccountCount int64           `db:"account_count"`
}

// recordLevel observes only today. Restarts preserve the first observation and
// offline days remain absent rather than being reconstructed from current data.
func recordLevel(ctx context.Context, tx *sqlx.Tx, at time.Time) error {
	day := at.UTC().Truncate(24 * time.Hour).Unix()
	var sampleCount int64
	err := tx.GetContext(ctx, &sampleCount, tx.Rebind(`SELECT COUNT(*) FROM activity_level_sample WHERE day_at = ?`), day)
	if err != nil {
		return fmt.Errorf("levelExists: %w", err)
	}
	if sampleCount > 0 {
		return nil
	}
	err = execStatement(ctx, tx, `INSERT INTO activity_level_sample (day_at, observed_at, average_level, account_count)
 SELECT ?, ?, AVG(CAST(level AS DOUBLE PRECISION)), COUNT(*) FROM "user" WHERE trmt_id = 0
 ON CONFLICT (day_at) DO NOTHING`, day, at.UTC().Unix())
	if err != nil {
		return fmt.Errorf("levelInsert: %w", err)
	}
	return nil
}

func readLevel(ctx context.Context, tx *sqlx.Tx, req activity.Window) (levelRow, []activity.LevelSample, error) {
	var latest levelRow
	err := tx.GetContext(ctx, &latest, tx.Rebind(`SELECT day_at, observed_at, average_level, account_count
 FROM activity_level_sample WHERE observed_at <= ? ORDER BY day_at DESC LIMIT 1`), req.Until.Unix())
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return levelRow{}, nil, fmt.Errorf("levelLatest: %w", err)
	}
	levelRows := make([]levelRow, 0)
	err = tx.SelectContext(ctx, &levelRows, tx.Rebind(`SELECT day_at, observed_at, average_level, account_count
 FROM activity_level_sample WHERE day_at >= ? AND day_at <= ? AND observed_at <= ? ORDER BY day_at`),
		req.Since.UTC().Truncate(24*time.Hour).Unix(), req.Until.Unix(), req.Until.Unix())
	if err != nil {
		return levelRow{}, nil, fmt.Errorf("levelHistory: %w", err)
	}
	levelSamples := make([]activity.LevelSample, 0, len(levelRows))
	for _, row := range levelRows {
		levelSamples = append(levelSamples, activity.LevelSample{
			At: time.Unix(row.DayAt, 0).UTC(), ObservedAt: time.Unix(row.ObservedAt, 0).UTC(), AverageLevel: row.AverageLevel.Float64, AccountCount: row.AccountCount,
		})
	}
	return latest, levelSamples, nil
}
