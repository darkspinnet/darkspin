package sqlstore

import (
	"context"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/activity"
	"github.com/jmoiron/sqlx"
)

func recordDistribution(ctx context.Context, tx *sqlx.Tx, req activity.Observation) error {
	if req.Distribution == nil {
		return nil
	}
	err := execStatement(ctx, tx, `INSERT INTO activity_collection (id, started_at) VALUES (8, ?) ON CONFLICT (id) DO NOTHING`, req.At.UTC().Unix())
	if err != nil {
		return fmt.Errorf("distributionStart: %w", err)
	}
	// Replace the whole observation on a same-hour startup. Per-state maxima
	// could combine different players/states and inflate the observed total.
	distribution := req.Distribution
	err = execStatement(ctx, tx, `INSERT INTO activity_hour_distribution
 (hour_at, observed_at, pve_count, pvp_count, lobby_count, matchmaking_count, other_count)
 VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT (hour_at) DO UPDATE SET
 observed_at = excluded.observed_at, pve_count = excluded.pve_count, pvp_count = excluded.pvp_count,
 lobby_count = excluded.lobby_count, matchmaking_count = excluded.matchmaking_count, other_count = excluded.other_count
 WHERE excluded.observed_at > activity_hour_distribution.observed_at`, req.At.UTC().Truncate(time.Hour).Unix(), req.At.UTC().Unix(),
		distribution.PVECount, distribution.PVPCount, distribution.LobbyCount, distribution.MatchmakingCount, distribution.OtherCount)
	if err != nil {
		return fmt.Errorf("distributionHour: %w", err)
	}
	return nil
}

func compactDistribution(ctx context.Context, tx *sqlx.Tx, cutoff int64) error {
	// Packing and removing the contributing hours share the caller's transaction.
	err := execStatement(ctx, tx, `INSERT INTO activity_day_distribution
 (day_at, pve_count, pvp_count, lobby_count, matchmaking_count, other_count, observation_count)
 SELECT (hour_at / 86400) * 86400, SUM(pve_count), SUM(pvp_count), SUM(lobby_count), SUM(matchmaking_count), SUM(other_count), COUNT(*)
 FROM activity_hour_distribution WHERE hour_at < ? GROUP BY 1
 ON CONFLICT (day_at) DO UPDATE SET pve_count = activity_day_distribution.pve_count + excluded.pve_count,
 pvp_count = activity_day_distribution.pvp_count + excluded.pvp_count, lobby_count = activity_day_distribution.lobby_count + excluded.lobby_count,
 matchmaking_count = activity_day_distribution.matchmaking_count + excluded.matchmaking_count,
 other_count = activity_day_distribution.other_count + excluded.other_count,
 observation_count = activity_day_distribution.observation_count + excluded.observation_count`, cutoff)
	if err != nil {
		return fmt.Errorf("distributionPack: %w", err)
	}
	err = execStatement(ctx, tx, `DELETE FROM activity_hour_distribution WHERE hour_at < ?`, cutoff)
	if err != nil {
		return fmt.Errorf("distributionDelete: %w", err)
	}
	return nil
}

type distributionRow struct {
	BucketAt         int64   `db:"bucket_at"`
	PVECount         float64 `db:"pve_count"`
	PVPCount         float64 `db:"pvp_count"`
	LobbyCount       float64 `db:"lobby_count"`
	MatchmakingCount float64 `db:"matchmaking_count"`
	OtherCount       float64 `db:"other_count"`
	ObservationCount int64   `db:"observation_count"`
}

func readDistribution(ctx context.Context, tx *sqlx.Tx, req activity.Window) (time.Time, []activity.DistributionSample, error) {
	var startedAt int64
	err := tx.GetContext(ctx, &startedAt, `SELECT COALESCE(MAX(started_at), 0) FROM activity_collection WHERE id = 8`)
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("distributionBoundary: %w", err)
	}
	rows := make([]distributionRow, 0)
	bucketSecond := int64(req.Bucket / time.Second)
	// Daily storage contains sums and denominators, not rounded averages. The
	// two sources are disjoint; missing hours never enter the denominator.
	err = tx.SelectContext(ctx, &rows, tx.Rebind(`SELECT (sample_at / ?) * ? AS bucket_at,
 1.0 * SUM(pve_count) / SUM(observation_count) AS pve_count,
 1.0 * SUM(pvp_count) / SUM(observation_count) AS pvp_count,
 1.0 * SUM(lobby_count) / SUM(observation_count) AS lobby_count,
 1.0 * SUM(matchmaking_count) / SUM(observation_count) AS matchmaking_count,
 1.0 * SUM(other_count) / SUM(observation_count) AS other_count, SUM(observation_count) AS observation_count
 FROM (SELECT hour_at AS sample_at, pve_count, pvp_count, lobby_count, matchmaking_count, other_count, 1 AS observation_count
 FROM activity_hour_distribution UNION ALL SELECT day_at AS sample_at, pve_count, pvp_count, lobby_count, matchmaking_count, other_count, observation_count
 FROM activity_day_distribution) AS distribution WHERE sample_at >= ? AND sample_at <= ? GROUP BY 1 ORDER BY 1`), bucketSecond, bucketSecond, req.Since.Unix(), req.Until.Unix())
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("distributionRead: %w", err)
	}
	samples := make([]activity.DistributionSample, 0, len(rows))
	for _, row := range rows {
		samples = append(samples, activity.DistributionSample{At: time.Unix(row.BucketAt, 0).UTC(), PVECount: row.PVECount, PVPCount: row.PVPCount,
			LobbyCount: row.LobbyCount, MatchmakingCount: row.MatchmakingCount, OtherCount: row.OtherCount, ObservationCount: row.ObservationCount})
	}
	if startedAt == 0 {
		return time.Time{}, samples, nil
	}
	return time.Unix(startedAt, 0).UTC(), samples, nil
}
