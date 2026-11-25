package sqlstore

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/activity"
	"github.com/jmoiron/sqlx"
)

type dnaRow struct {
	BucketAt int64 `db:"bucket_at"`
	Earned   int64 `db:"earned"`
}

// RecordDNAEarned participates in the profile save transaction. The caller
// supplies only newly persisted gameplay earnings, making a save retry safe.
func RecordDNAEarned(ctx context.Context, tx *sqlx.Tx, userID int64, earned uint64) error {
	if earned == 0 {
		return nil
	}
	if earned > math.MaxInt64 {
		return errors.New("DNA earnings exceed storage limit")
	}
	// Serialize with activity packing on PostgreSQL. SQLite serializes writers.
	if tx.DriverName() == "pgx" || tx.DriverName() == "postgres" {
		err := execStatement(ctx, tx, `SELECT pg_advisory_xact_lock(1936748398, 3)`)
		if err != nil {
			return fmt.Errorf("dnaLock: %w", err)
		}
	}
	hour := time.Now().UTC().Truncate(time.Hour).Unix()
	var savedEarned int64
	err := tx.GetContext(ctx, &savedEarned, tx.Rebind(`INSERT INTO activity_hour_dna (hour_at, earned) VALUES (?, ?)
 ON CONFLICT (hour_at) DO UPDATE SET earned = activity_hour_dna.earned + excluded.earned
 WHERE activity_hour_dna.earned <= ? - excluded.earned RETURNING earned`), hour, int64(earned), int64(math.MaxInt64))
	if err != nil {
		return fmt.Errorf("dnaHourSave: %w", err)
	}
	err = tx.GetContext(ctx, &savedEarned, tx.Rebind(`INSERT INTO activity_user_hour_dna (user_id, hour_at, earned) VALUES (?, ?, ?)
 ON CONFLICT (user_id, hour_at) DO UPDATE SET earned = activity_user_hour_dna.earned + excluded.earned
 WHERE activity_user_hour_dna.earned <= ? - excluded.earned RETURNING earned`), userID, hour, int64(earned), int64(math.MaxInt64))
	if err != nil {
		return fmt.Errorf("dnaUserSave: %w", err)
	}
	return nil
}

func compactDNA(ctx context.Context, tx *sqlx.Tx, cutoff int64) error {
	hourRows := make([]dnaRow, 0)
	err := tx.SelectContext(ctx, &hourRows, tx.Rebind(`SELECT hour_at AS bucket_at, earned FROM activity_hour_dna WHERE hour_at < ? ORDER BY hour_at`), cutoff)
	if err != nil {
		return fmt.Errorf("dnaHourRead: %w", err)
	}
	earningsByDay := make(map[int64]int64)
	for _, hourRow := range hourRows {
		day := hourRow.BucketAt / 86400 * 86400
		if hourRow.Earned > math.MaxInt64-earningsByDay[day] {
			return errors.New("daily DNA earnings exceed storage limit")
		}
		earningsByDay[day] += hourRow.Earned
	}
	for day, earned := range earningsByDay {
		var savedEarned int64
		err = tx.GetContext(ctx, &savedEarned, tx.Rebind(`INSERT INTO activity_day_dna (day_at, earned) VALUES (?, ?)
 ON CONFLICT (day_at) DO UPDATE SET earned = activity_day_dna.earned + excluded.earned
 WHERE activity_day_dna.earned <= ? - excluded.earned RETURNING earned`), day, earned, int64(math.MaxInt64))
		if err != nil {
			return fmt.Errorf("dnaDaySave: %w", err)
		}
	}
	err = execStatement(ctx, tx, `DELETE FROM activity_hour_dna WHERE hour_at < ?`, cutoff)
	if err != nil {
		return fmt.Errorf("dnaHourDelete: %w", err)
	}
	return nil
}

func readDNA(ctx context.Context, tx *sqlx.Tx, req activity.Window) (uint64, time.Time, []activity.DNASample, error) {
	var startedAt int64
	err := tx.GetContext(ctx, &startedAt, `SELECT COALESCE((SELECT started_at FROM activity_collection WHERE id = 2), 0)`)
	if err != nil {
		return 0, time.Time{}, nil, fmt.Errorf("dnaStartRead: %w", err)
	}
	// Packing deletes source hours in its transaction; the two tables are disjoint.
	// Sum in Go with an explicit bound instead of allowing SQL integer overflow.
	dnaRows := make([]dnaRow, 0)
	err = tx.SelectContext(ctx, &dnaRows, tx.Rebind(`SELECT sample_at AS bucket_at, earned FROM (
 SELECT hour_at AS sample_at, earned FROM activity_hour_dna
 UNION ALL SELECT day_at AS sample_at, earned FROM activity_day_dna) AS dna
 WHERE sample_at >= ? AND sample_at <= ? ORDER BY sample_at`), req.Since.Unix(), req.Until.Unix())
	if err != nil {
		return 0, time.Time{}, nil, fmt.Errorf("dnaSampleRead: %w", err)
	}
	dnaSamples := make([]activity.DNASample, 0)
	var totalEarned uint64
	bucketSecond := int64(req.Bucket / time.Second)
	for _, row := range dnaRows {
		if row.Earned < 0 || uint64(row.Earned) > math.MaxUint64-totalEarned {
			return 0, time.Time{}, nil, errors.New("DNA earnings exceed summary limit")
		}
		earned := uint64(row.Earned)
		totalEarned += earned
		bucketAt := time.Unix(row.BucketAt/bucketSecond*bucketSecond, 0).UTC()
		lastIndex := len(dnaSamples) - 1
		if lastIndex >= 0 && dnaSamples[lastIndex].At.Equal(bucketAt) {
			dnaSamples[lastIndex].Earned += earned
			continue
		}
		dnaSamples = append(dnaSamples, activity.DNASample{At: bucketAt, Earned: earned})
	}
	var start time.Time
	if startedAt != 0 {
		start = time.Unix(startedAt, 0).UTC()
	}
	return totalEarned, start, dnaSamples, nil
}
