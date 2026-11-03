package sqlstore

import (
	"context"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/activity"
	"github.com/jmoiron/sqlx"
)

func recordQueues(ctx context.Context, tx *sqlx.Tx, req activity.Observation) error {
	if req.Queues == nil {
		return nil
	}
	err := execStatement(ctx, tx, `INSERT INTO activity_collection (id, started_at) VALUES (9, ?) ON CONFLICT (id) DO NOTHING`, req.At.UTC().Unix())
	if err != nil {
		return fmt.Errorf("queueStart: %w", err)
	}
	for _, queue := range req.Queues {
		// All queues share the sampling transaction. Restart replaces counts and
		// ages together; it cannot combine observations from different moments.
		err = execStatement(ctx, tx, `INSERT INTO activity_hour_queue
 (hour_at, kind, observed_at, queued_count, wait_second, maximum_wait_second) VALUES (?, ?, ?, ?, ?, ?)
 ON CONFLICT (hour_at, kind) DO UPDATE SET observed_at = excluded.observed_at, queued_count = excluded.queued_count,
 wait_second = excluded.wait_second, maximum_wait_second = excluded.maximum_wait_second
 WHERE excluded.observed_at > activity_hour_queue.observed_at`, req.At.UTC().Truncate(time.Hour).Unix(), string(queue.Kind),
			req.At.UTC().Unix(), queue.QueuedCount, queue.WaitSeconds, queue.MaximumWaitSeconds)
		if err != nil {
			return fmt.Errorf("queueHour[%s]: %w", queue.Kind, err)
		}
	}
	return nil
}

func compactQueues(ctx context.Context, tx *sqlx.Tx, cutoff int64) error {
	err := execStatement(ctx, tx, `INSERT INTO activity_day_queue
 (day_at, kind, queued_count, wait_second, maximum_wait_second, observation_count)
 SELECT (hour_at / 86400) * 86400, kind, SUM(queued_count), SUM(wait_second), MAX(maximum_wait_second), COUNT(*)
 FROM activity_hour_queue WHERE hour_at < ? GROUP BY 1, 2
 ON CONFLICT (day_at, kind) DO UPDATE SET queued_count = activity_day_queue.queued_count + excluded.queued_count,
 wait_second = activity_day_queue.wait_second + excluded.wait_second,
 maximum_wait_second = CASE WHEN excluded.maximum_wait_second > activity_day_queue.maximum_wait_second
 THEN excluded.maximum_wait_second ELSE activity_day_queue.maximum_wait_second END,
 observation_count = activity_day_queue.observation_count + excluded.observation_count`, cutoff)
	if err != nil {
		return fmt.Errorf("queuePack: %w", err)
	}
	err = execStatement(ctx, tx, `DELETE FROM activity_hour_queue WHERE hour_at < ?`, cutoff)
	if err != nil {
		return fmt.Errorf("queueDelete: %w", err)
	}
	return nil
}

type queueRow struct {
	BucketAt           int64   `db:"bucket_at"`
	Kind               string  `db:"kind"`
	QueuedCount        float64 `db:"queued_count"`
	AverageWaitSeconds float64 `db:"average_wait_second"`
	MaximumWaitSeconds float64 `db:"maximum_wait_second"`
	ObservationCount   int64   `db:"observation_count"`
}

func readQueues(ctx context.Context, tx *sqlx.Tx, req activity.Window) (time.Time, []activity.QueueSample, error) {
	var startedAt int64
	err := tx.GetContext(ctx, &startedAt, `SELECT COALESCE(MAX(started_at), 0) FROM activity_collection WHERE id = 9`)
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("queueBoundary: %w", err)
	}
	rows := make([]queueRow, 0)
	bucketSecond := int64(req.Bucket / time.Second)
	// Queue size is averaged over observations; waiting age is weighted by
	// queued-player observations. Empty queues do not invent zero-second waits.
	err = tx.SelectContext(ctx, &rows, tx.Rebind(`SELECT (sample_at / ?) * ? AS bucket_at, kind,
 1.0 * SUM(queued_count) / SUM(observation_count) AS queued_count,
 CASE WHEN SUM(queued_count) > 0 THEN SUM(wait_second) / SUM(queued_count) ELSE 0 END AS average_wait_second,
 MAX(maximum_wait_second) AS maximum_wait_second, SUM(observation_count) AS observation_count
 FROM (SELECT hour_at AS sample_at, kind, queued_count, wait_second, maximum_wait_second, 1 AS observation_count
 FROM activity_hour_queue UNION ALL SELECT day_at AS sample_at, kind, queued_count, wait_second, maximum_wait_second, observation_count
 FROM activity_day_queue) AS queue WHERE sample_at >= ? AND sample_at <= ? GROUP BY 1, 2 ORDER BY 1, 2`),
		bucketSecond, bucketSecond, req.Since.Unix(), req.Until.Unix())
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("queueRead: %w", err)
	}
	samples := make([]activity.QueueSample, 0, len(rows))
	for _, row := range rows {
		samples = append(samples, activity.QueueSample{At: time.Unix(row.BucketAt, 0).UTC(), Kind: activity.QueueKind(row.Kind),
			QueuedCount: row.QueuedCount, AverageWaitSeconds: row.AverageWaitSeconds, MaximumWaitSeconds: row.MaximumWaitSeconds,
			ObservationCount: row.ObservationCount})
	}
	if startedAt == 0 {
		return time.Time{}, samples, nil
	}
	return time.Unix(startedAt, 0).UTC(), samples, nil
}
