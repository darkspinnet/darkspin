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

type userDNARow struct {
	UserID   int64 `db:"user_id"`
	BucketAt int64 `db:"bucket_at"`
	Earned   int64 `db:"earned"`
}
type userDayKey struct{ userID, day int64 }

func compactUserDNA(ctx context.Context, tx *sqlx.Tx, cutoff int64) error {
	userDNARows := make([]userDNARow, 0)
	err := tx.SelectContext(ctx, &userDNARows, tx.Rebind(`SELECT user_id, hour_at AS bucket_at, earned FROM activity_user_hour_dna WHERE hour_at < ? ORDER BY user_id, hour_at`), cutoff)
	if err != nil {
		return fmt.Errorf("userDNAHours: %w", err)
	}
	earningsByDay := make(map[userDayKey]int64)
	for _, row := range userDNARows {
		key := userDayKey{row.UserID, row.BucketAt / 86400 * 86400}
		if row.Earned < 0 || row.Earned > math.MaxInt64-earningsByDay[key] {
			return errors.New("daily user DNA exceeds storage limit")
		}
		earningsByDay[key] += row.Earned
	}
	for key, earned := range earningsByDay {
		var savedEarned int64
		err = tx.GetContext(ctx, &savedEarned, tx.Rebind(`INSERT INTO activity_user_day_dna (user_id, day_at, earned) VALUES (?, ?, ?)
 ON CONFLICT (user_id, day_at) DO UPDATE SET earned = activity_user_day_dna.earned + excluded.earned
 WHERE activity_user_day_dna.earned <= ? - excluded.earned RETURNING earned`), key.userID, key.day, earned, int64(math.MaxInt64))
		if err != nil {
			return fmt.Errorf("userDNADay: %w", err)
		}
	}
	err = execStatement(ctx, tx, `DELETE FROM activity_user_hour_dna WHERE hour_at < ?`, cutoff)
	if err != nil {
		return fmt.Errorf("userDNADelete: %w", err)
	}
	return nil
}

func readUserDNA(ctx context.Context, tx *sqlx.Tx, userID int64, req activity.Window) (uint64, []activity.DNASample, error) {
	dnaRows := make([]dnaRow, 0)
	err := tx.SelectContext(ctx, &dnaRows, tx.Rebind(`SELECT sample_at AS bucket_at, earned FROM (
 SELECT hour_at AS sample_at, user_id, earned FROM activity_user_hour_dna
 UNION ALL SELECT day_at AS sample_at, user_id, earned FROM activity_user_day_dna) AS dna
 WHERE user_id = ? AND sample_at >= ? AND sample_at <= ? ORDER BY sample_at`), userID, req.Since.Unix(), req.Until.Unix())
	if err != nil {
		return 0, nil, fmt.Errorf("userDNARead: %w", err)
	}
	dnaSamples := make([]activity.DNASample, 0)
	var totalEarned uint64
	bucketSecond := int64(req.Bucket / time.Second)
	for _, row := range dnaRows {
		if row.Earned < 0 || uint64(row.Earned) > math.MaxUint64-totalEarned {
			return 0, nil, errors.New("user DNA exceeds summary limit")
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
	return totalEarned, dnaSamples, nil
}
