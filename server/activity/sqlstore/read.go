package sqlstore

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/activity"
)

type sampleRow struct {
	BucketAt    int64   `db:"bucket_at"`
	OnlineCount float64 `db:"online_count"`
}

type observationRow struct {
	BucketAt          int64           `db:"bucket_at"`
	OnlineCount       sql.NullFloat64 `db:"online_count"`
	PeakOnlineCount   sql.NullInt64   `db:"peak_online_count"`
	UniquePlayerCount int64           `db:"unique_player_count"`
}

type coverageRow struct {
	StartedAt    int64 `db:"started_at"`
	LastSampleAt int64 `db:"last_sample_at"`
	SampleCount  int64 `db:"sample_count"`
	PeakOnline   int64 `db:"peak_online"`
}

// The two sources are disjoint: daily packing deletes the contributing hours
// in the same transaction. Preserve observed counts instead of filling outages.
const sampleSource = `SELECT hour_at AS sample_at, online_count AS online_sum,
 1 AS sample_count, hour_at AS last_sample_at, peak_count, coverage_count FROM activity_hour_sample
 UNION ALL SELECT day_at AS sample_at, online_sum, sample_count, last_sample_at, peak_count, coverage_count FROM activity_day_sample`

func (e *Store) ReadHourly(ctx context.Context, req activity.Window) (activity.Snapshot, error) {
	options := &sql.TxOptions{ReadOnly: true}
	if e.isPostgres {
		options.Isolation = sql.LevelRepeatableRead
	}
	tx, err := e.db.BeginTxx(ctx, options)
	if err != nil {
		return activity.Snapshot{}, fmt.Errorf("readBegin: %w", err)
	}
	defer rollback(tx)
	var uniqueCount int64
	err = tx.GetContext(ctx, &uniqueCount, tx.Rebind(`SELECT COUNT(DISTINCT user_id) FROM (
 SELECT hour_at AS observed_at, user_id FROM activity_hour_presence
 UNION ALL SELECT day_at AS observed_at, user_id FROM activity_day_presence) AS presence
 WHERE observed_at >= ? AND observed_at <= ?`), req.Since.Unix(), req.Until.Unix())
	if err != nil {
		return activity.Snapshot{}, fmt.Errorf("readUnique: %w", err)
	}
	var coverage coverageRow
	err = tx.GetContext(ctx, &coverage, tx.Rebind(`SELECT
 COALESCE((SELECT started_at FROM activity_collection WHERE id = 1), 0) AS started_at,
 COALESCE((SELECT MAX(last_sample_at) FROM (`+sampleSource+`) AS all_sample), 0) AS last_sample_at,
 COALESCE(SUM(CASE WHEN sample_at < ? THEN coverage_count ELSE 0 END), 0) AS sample_count,
 COALESCE(MAX(peak_count), 0) AS peak_online FROM (`+sampleSource+`) AS sample
 WHERE sample_at >= ? AND sample_at <= ?`),
		req.Until.Truncate(time.Hour).Unix(), req.Since.Unix(), req.Until.Unix())
	if err != nil {
		return activity.Snapshot{}, fmt.Errorf("readCoverage: %w", err)
	}
	bucketSecond := int64(req.Bucket / time.Second)
	sampleRows := make([]observationRow, 0)
	// GROUP BY the selected position avoids PostgreSQL treating separate bound
	// parameters in matching bucket expressions as distinct expressions.
	err = tx.SelectContext(ctx, &sampleRows, tx.Rebind(`WITH bucket AS (
 SELECT (sample_at / ?) * ? AS bucket_at,
 SUM(online_sum) / SUM(sample_count) AS online_count, MAX(peak_count) AS peak_online_count
 FROM (`+sampleSource+`) AS sample
 WHERE sample_at >= ? AND sample_at <= ? GROUP BY 1
), presence AS (
 SELECT (observed_at / ?) * ? AS bucket_at, COUNT(DISTINCT user_id) AS unique_player_count
 FROM (
 SELECT hour_at AS observed_at, user_id FROM activity_hour_presence
 UNION ALL SELECT day_at AS observed_at, user_id FROM activity_day_presence
 ) AS observed WHERE observed_at >= ? AND observed_at <= ? GROUP BY 1
), bucket_key AS (
 SELECT bucket_at FROM bucket UNION SELECT bucket_at FROM presence
)
 SELECT bucket_key.bucket_at, bucket.online_count, bucket.peak_online_count,
 COALESCE(presence.unique_player_count, 0) AS unique_player_count
 FROM bucket_key LEFT JOIN bucket ON bucket.bucket_at = bucket_key.bucket_at
 LEFT JOIN presence ON presence.bucket_at = bucket_key.bucket_at ORDER BY bucket_key.bucket_at`),
		bucketSecond, bucketSecond, req.Since.Unix(), req.Until.Unix(), bucketSecond, bucketSecond, req.Since.Unix(), req.Until.Unix())
	if err != nil {
		return activity.Snapshot{}, fmt.Errorf("readSample: %w", err)
	}
	samples := make([]activity.Sample, 0, len(sampleRows))
	for _, row := range sampleRows {
		samples = append(samples, activity.Sample{At: time.Unix(row.BucketAt, 0).UTC(), OnlineCount: row.OnlineCount.Float64, IsOnlineUnknown: !row.OnlineCount.Valid, UniquePlayerCount: row.UniquePlayerCount, PeakOnlineCount: row.PeakOnlineCount.Int64})
	}
	signupStartedAt, signupSamples, err := readSignup(ctx, tx, req)
	if err != nil {
		return activity.Snapshot{}, fmt.Errorf("readSignup: %w", err)
	}
	firstAdmissionStartedAt, firstAdmissionSamples, err := readFirstAdmission(ctx, tx, req)
	if err != nil {
		return activity.Snapshot{}, fmt.Errorf("readAdmission: %w", err)
	}
	dnaEarned, dnaStartedAt, dnaSamples, err := readDNA(ctx, tx, req)
	if err != nil {
		return activity.Snapshot{}, fmt.Errorf("readDNA: %w", err)
	}
	distributionStartedAt, distributionSamples, err := readDistribution(ctx, tx, req)
	if err != nil {
		return activity.Snapshot{}, fmt.Errorf("readDistribution: %w", err)
	}
	queueStartedAt, queueSamples, err := readQueues(ctx, tx, req)
	if err != nil {
		return activity.Snapshot{}, fmt.Errorf("readQueues: %w", err)
	}
	err = tx.Commit()
	if err != nil {
		return activity.Snapshot{}, fmt.Errorf("readCommit: %w", err)
	}
	result := activity.Snapshot{
		QueueStartedAt: queueStartedAt, QueueSamples: queueSamples,
		DistributionStartedAt: distributionStartedAt, DistributionSamples: distributionSamples,
		SignupStartedAt: signupStartedAt, SignupSamples: signupSamples,
		FirstAdmissionStartedAt: firstAdmissionStartedAt, FirstAdmissionSamples: firstAdmissionSamples,
		DNAEarned: dnaEarned, DNAStartedAt: dnaStartedAt, DNASamples: dnaSamples,
		ObservedUniquePlayers: uniqueCount, PeakOnline: coverage.PeakOnline,
		Samples: samples, SampleCount: coverage.SampleCount,
	}
	if coverage.StartedAt != 0 {
		result.StartedAt = time.Unix(coverage.StartedAt, 0).UTC()
	}
	if coverage.LastSampleAt != 0 {
		result.LastSampleAt = time.Unix(coverage.LastSampleAt, 0).UTC()
	}
	return result, nil
}

// ReadDaily reads population and daily account snapshots without hourly activity.
func (e *Store) ReadDaily(ctx context.Context, req activity.Window) (activity.Snapshot, error) {
	options := &sql.TxOptions{ReadOnly: true}
	if e.isPostgres {
		options.Isolation = sql.LevelRepeatableRead
	}
	tx, err := e.db.BeginTxx(ctx, options)
	if err != nil {
		return activity.Snapshot{}, fmt.Errorf("dailyBegin: %w", err)
	}
	defer rollback(tx)
	missionStartedAt, missionSamples, err := readMissionSamples(ctx, tx, req, 0)
	if err != nil {
		return activity.Snapshot{}, fmt.Errorf("dailyMission: %w", err)
	}
	populationSnapshot, populationSamples, creationSamples, err := readPopulation(ctx, tx, req, e.isPostgres)
	if err != nil {
		return activity.Snapshot{}, fmt.Errorf("readPopulationHistory: %w", err)
	}
	campaignStartedAt, campaignSamples, err := readCampaignSamples(ctx, tx, req, 0)
	if err != nil {
		return activity.Snapshot{}, fmt.Errorf("dailyCampaignSample: %w", err)
	}
	campaignLevelStartedAt, campaignLevels, err := readCampaignLevels(ctx, tx, req, 0)
	if err != nil {
		return activity.Snapshot{}, fmt.Errorf("dailyCampaignLevel: %w", err)
	}
	level, levelSamples, err := readLevel(ctx, tx, req)
	if err != nil {
		return activity.Snapshot{}, fmt.Errorf("readLevel: %w", err)
	}
	progression, progressionSamples, err := readProgression(ctx, tx, req)
	if err != nil {
		return activity.Snapshot{}, fmt.Errorf("readProgression: %w", err)
	}
	err = tx.Commit()
	if err != nil {
		return activity.Snapshot{}, fmt.Errorf("dailyCommit: %w", err)
	}
	result := activity.Snapshot{
		MissionStartedAt:           missionStartedAt,
		MissionSamples:             missionSamples,
		CampaignLevelStartedAt:     campaignLevelStartedAt,
		CampaignLevels:             campaignLevels,
		CampaignStartedAt:          campaignStartedAt,
		CampaignSamples:            campaignSamples,
		AverageCrogenitorLevel:     level.AverageLevel.Float64,
		LevelAccountCount:          level.AccountCount,
		LevelSamples:               levelSamples,
		AverageCampaignProgression: progression.AverageProgression.Float64,
		HighestCompletedCampaign:   uint32(progression.HighestCompletedCampaign.Int64),
		ProgressionAccountCount:    progression.AccountCount,
		ProgressionSamples:         progressionSamples,
		PopulationSamples:          populationSamples, CreationSamples: creationSamples,
	}
	if populationSnapshot.ObservedAt != 0 {
		result.PopulationObservedAt = time.Unix(populationSnapshot.ObservedAt, 0).UTC()
	}
	if level.ObservedAt != 0 {
		result.LevelObservedAt = time.Unix(level.ObservedAt, 0).UTC()
	}
	if progression.ObservedAt != 0 {
		result.ProgressionObservedAt = time.Unix(progression.ObservedAt, 0).UTC()
	}
	return result, nil
}
