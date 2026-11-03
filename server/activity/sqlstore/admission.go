package sqlstore

import (
	"context"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/activity"
	"github.com/jmoiron/sqlx"
)

// Markers survive chart retention so a returning account never becomes new again.
// They are created exclusively by accepted gameplay admissions, never panel login.
func readFirstAdmission(ctx context.Context, tx *sqlx.Tx, req activity.Window) (time.Time, []activity.CreationSample, error) {
	var startedAt int64
	err := tx.GetContext(ctx, &startedAt, `SELECT COALESCE(MAX(started_at), 0) FROM activity_collection WHERE id = 4`)
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("admissionBoundary: %w", err)
	}
	if startedAt == 0 {
		return time.Time{}, nil, nil
	}
	admissionRows := make([]accountPopulationRow, 0)
	start := req.Since.UTC().Truncate(24 * time.Hour)
	err = tx.SelectContext(ctx, &admissionRows, tx.Rebind(`SELECT (first_at / 86400) * 86400 AS day_at, 0 AS observed_at, COUNT(*) AS account_count
 FROM activity_first_admission WHERE first_at >= ? AND first_at <= ?
 AND EXISTS (SELECT 1 FROM "user" WHERE "user".id = activity_first_admission.user_id AND trmt_id = 0)
 GROUP BY 1 ORDER BY 1`), start.Unix(), req.Until.Unix())
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("admissionHistory: %w", err)
	}
	admissionsByDay := make(map[int64]int64, len(admissionRows))
	for _, row := range admissionRows {
		admissionsByDay[row.DayAt] = row.AccountCount
	}
	samples := make([]activity.CreationSample, 0)
	boundary := time.Unix(startedAt, 0).UTC()
	if start.Before(boundary) {
		start = boundary.Truncate(24 * time.Hour)
	}
	observedDays := make([]int64, 0)
	err = tx.SelectContext(ctx, &observedDays, tx.Rebind(`SELECT DISTINCT (sample_at / 86400) * 86400 FROM (`+sampleSource+`) AS sample
 WHERE sample_at >= ? AND sample_at <= ?
 UNION SELECT DISTINCT (first_at / 86400) * 86400 FROM activity_first_admission
 WHERE first_at >= ? AND first_at <= ? ORDER BY 1`), start.Unix(), req.Until.Unix(), start.Unix(), req.Until.Unix())
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("admissionCoverage: %w", err)
	}
	for _, day := range observedDays {
		samples = append(samples, activity.CreationSample{At: time.Unix(day, 0).UTC(), AccountCount: admissionsByDay[day]})
	}
	return boundary, samples, nil
}
