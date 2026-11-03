package sqlstore

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/activity"
)

// Presence counts observed hour intervals. It never estimates playtime.
func (e *Store) ReadUserHourly(ctx context.Context, userID int64, req activity.Window) (activity.UserSummary, error) {
	options := &sql.TxOptions{ReadOnly: true}
	if e.isPostgres {
		options.Isolation = sql.LevelRepeatableRead
	}
	tx, err := e.db.BeginTxx(ctx, options)
	if err != nil {
		return activity.UserSummary{}, fmt.Errorf("userReadBegin: %w", err)
	}
	defer rollback(tx)
	var startedAt int64
	err = tx.GetContext(ctx, &startedAt, `SELECT COALESCE((SELECT started_at FROM activity_collection WHERE id = 3), 0)`)
	if err != nil {
		return activity.UserSummary{}, fmt.Errorf("userReadStart: %w", err)
	}
	result := activity.UserSummary{IsPartial: startedAt == 0 || time.Unix(startedAt, 0).After(req.Since)}
	if startedAt != 0 {
		result.CollectionStartedAt = time.Unix(startedAt, 0).UTC()
	}
	result.DNAEarned, result.DNASamples, err = readUserDNA(ctx, tx, userID, req)
	if err != nil {
		return activity.UserSummary{}, fmt.Errorf("userReadDNA: %w", err)
	}
	presenceRows := make([]sampleRow, 0)
	bucketSecond := int64(req.Bucket / time.Second)
	err = tx.SelectContext(ctx, &presenceRows, tx.Rebind(`SELECT (sample_at / ?) * ? AS bucket_at, SUM(observed_hour_count) AS online_count FROM (
 SELECT hour_at AS sample_at, 1 AS observed_hour_count FROM activity_hour_presence WHERE user_id = ? AND hour_at >= ?
 UNION ALL SELECT day_at AS sample_at, observed_hour_count FROM activity_user_day_presence WHERE user_id = ?) AS presence
 WHERE sample_at >= ? AND sample_at <= ? GROUP BY 1 ORDER BY 1`), bucketSecond, bucketSecond, userID, startedAt/3600*3600, userID, req.Since.Unix(), req.Until.Unix())
	if err != nil {
		return activity.UserSummary{}, fmt.Errorf("userReadPresence: %w", err)
	}
	result.PresenceSamples = make([]activity.Sample, 0, len(presenceRows))
	for _, row := range presenceRows {
		result.ObservedHourCount += uint64(row.OnlineCount)
		result.PresenceSamples = append(result.PresenceSamples, activity.Sample{At: time.Unix(row.BucketAt, 0).UTC(), OnlineCount: row.OnlineCount})
	}
	// Population collection coverage detects outages without treating absence of
	// this particular user as missing collection.
	var sampleCount int64
	err = tx.GetContext(ctx, &sampleCount, tx.Rebind(`SELECT COALESCE(SUM(coverage_count), 0) FROM (`+sampleSource+`) AS sample WHERE sample_at >= ? AND sample_at < ?`), req.Since.Unix(), req.Until.Truncate(time.Hour).Unix())
	if err != nil {
		return activity.UserSummary{}, fmt.Errorf("userReadCoverage: %w", err)
	}
	expectedCount := int64(req.Until.Truncate(time.Hour).Sub(req.Since) / time.Hour)
	if sampleCount < expectedCount {
		result.IsPartial = true
	}
	err = tx.Commit()
	if err != nil {
		return activity.UserSummary{}, fmt.Errorf("userReadCommit: %w", err)
	}
	return result, nil
}
