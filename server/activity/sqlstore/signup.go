package sqlstore

import (
	"context"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/activity"
	"github.com/jmoiron/sqlx"
)

// RecordSignup is called exclusively by the registration adapter in its account
// creation transaction. No user identity is retained and deletion cannot lower totals.
func RecordSignup(ctx context.Context, tx *sqlx.Tx, at time.Time) error {
	err := execStatement(ctx, tx, `INSERT INTO activity_signup_sample (day_at, account_count)
 VALUES (?, 1) ON CONFLICT (day_at) DO UPDATE SET account_count = activity_signup_sample.account_count + 1`, at.UTC().Truncate(24*time.Hour).Unix())
	if err != nil {
		return fmt.Errorf("signupInsert: %w", err)
	}
	return nil
}

func readSignup(ctx context.Context, tx *sqlx.Tx, req activity.Window) (time.Time, []activity.SignupSample, error) {
	var startedAt int64
	err := tx.GetContext(ctx, &startedAt, `SELECT COALESCE(MAX(started_at), 0) FROM activity_collection WHERE id = 5`)
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("signupBoundary: %w", err)
	}
	if startedAt == 0 {
		return time.Time{}, nil, nil
	}
	signupRows := make([]accountPopulationRow, 0)
	err = tx.SelectContext(ctx, &signupRows, tx.Rebind(`SELECT day_at, 0 AS observed_at, account_count
 FROM activity_signup_sample WHERE day_at >= ? AND day_at <= ? ORDER BY day_at`), req.Since.UTC().Truncate(24*time.Hour).Unix(), req.Until.Unix())
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("signupHistory: %w", err)
	}
	samples := make([]activity.SignupSample, 0, len(signupRows))
	for _, row := range signupRows {
		samples = append(samples, activity.SignupSample{At: time.Unix(row.DayAt, 0).UTC(), AccountCount: row.AccountCount})
	}
	return time.Unix(startedAt, 0).UTC(), samples, nil
}
