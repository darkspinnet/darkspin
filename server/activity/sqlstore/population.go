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

type accountPopulationRow struct {
	DayAt        int64 `db:"day_at"`
	ObservedAt   int64 `db:"observed_at"`
	AccountCount int64 `db:"account_count"`
}

func recordPopulation(ctx context.Context, tx *sqlx.Tx, at time.Time) error {
	day := at.UTC().Truncate(24 * time.Hour).Unix()
	var sampleCount int64
	err := tx.GetContext(ctx, &sampleCount, tx.Rebind(`SELECT COUNT(*) FROM activity_population_sample WHERE day_at = ?`), day)
	if err != nil {
		return fmt.Errorf("populationExists: %w", err)
	}
	if sampleCount > 0 {
		return nil
	}
	err = execStatement(ctx, tx, `INSERT INTO activity_population_sample (day_at, observed_at, account_count)
 SELECT ?, ?, COUNT(*) FROM "user" WHERE trmt_id = 0 ON CONFLICT (day_at) DO NOTHING`, day, at.UTC().Unix())
	if err != nil {
		return fmt.Errorf("populationInsert: %w", err)
	}
	return nil
}

func readPopulation(ctx context.Context, tx *sqlx.Tx, req activity.Window, isPostgres bool) (accountPopulationRow, []activity.PopulationSample, []activity.CreationSample, error) {
	var latest accountPopulationRow
	err := tx.GetContext(ctx, &latest, tx.Rebind(`SELECT day_at, observed_at, account_count
 FROM activity_population_sample WHERE observed_at <= ? ORDER BY day_at DESC LIMIT 1`), req.Until.Unix())
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return latest, nil, nil, fmt.Errorf("populationLatest: %w", err)
	}
	start := req.Since.UTC().Truncate(24 * time.Hour)
	populationRows := make([]accountPopulationRow, 0)
	err = tx.SelectContext(ctx, &populationRows, tx.Rebind(`SELECT day_at, observed_at, account_count
 FROM activity_population_sample WHERE day_at >= ? AND observed_at <= ? ORDER BY day_at`), start.Unix(), req.Until.Unix())
	if err != nil {
		return latest, nil, nil, fmt.Errorf("populationHistory: %w", err)
	}
	populationSamples := make([]activity.PopulationSample, 0, len(populationRows))
	for _, row := range populationRows {
		populationSamples = append(populationSamples, activity.PopulationSample{At: time.Unix(row.DayAt, 0).UTC(), AccountCount: row.AccountCount})
	}
	createEpoch := `UNIXEPOCH(create_dt, 'subsec')`
	if isPostgres {
		createEpoch = `EXTRACT(EPOCH FROM NULLIF(create_dt, '')::TIMESTAMPTZ)`
	}
	creationRows := make([]accountPopulationRow, 0)
	err = tx.SelectContext(ctx, &creationRows, tx.Rebind(`SELECT CAST(FLOOR(`+createEpoch+` / 86400) AS BIGINT) * 86400 AS day_at,
 0 AS observed_at, COUNT(*) AS account_count FROM "user"
 WHERE trmt_id = 0 AND `+createEpoch+` >= ? AND `+createEpoch+` <= ?
 GROUP BY 1 ORDER BY 1`), float64(start.Unix()), float64(req.Until.UnixNano())/float64(time.Second))
	if err != nil {
		return latest, nil, nil, fmt.Errorf("creationHistory: %w", err)
	}
	creationsByDay := make(map[int64]int64, len(creationRows))
	for _, row := range creationRows {
		creationsByDay[row.DayAt] = row.AccountCount
	}
	creationSamples := make([]activity.CreationSample, 0)
	// Creation dates describe a complete inventory of remaining accounts, unlike observations.
	for day := start; !day.After(req.Until); day = day.Add(24 * time.Hour) {
		creationSamples = append(creationSamples, activity.CreationSample{At: day, AccountCount: creationsByDay[day.Unix()]})
	}
	return latest, populationSamples, creationSamples, nil
}
