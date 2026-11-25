package sqlstore

import (
	"context"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/activity"
	"github.com/jmoiron/sqlx"
)

// maintain runs once per UTC week, including after missed weeks or restarts.
// Packing and source deletion share the caller's transaction, so a retry cannot
// count the same hour twice. Only complete UTC days older than a week are packed.
func maintain(ctx context.Context, tx *sqlx.Tx, at time.Time) error {
	// Drop the oldest partial day as well: packed days cannot be split, and
	// retaining that day would keep observations beyond the retention limit.
	retentionAt := at.UTC().Add(-activity.Retention)
	retentionDay := retentionAt.Truncate(24 * time.Hour)
	if retentionAt.After(retentionDay) {
		retentionDay = retentionDay.Add(24 * time.Hour)
	}
	retentionCutoff := retentionDay.Unix()
	pruneStatements := []string{
		`DELETE FROM activity_mission WHERE started_at < ?`,
		`DELETE FROM activity_mission_day WHERE day_at < ?`,
		`DELETE FROM activity_campaign_level WHERE day_at < ?`,
		`DELETE FROM activity_hour_queue WHERE hour_at < ?`,
		`DELETE FROM activity_day_queue WHERE day_at < ?`,
		`DELETE FROM activity_hour_distribution WHERE hour_at < ?`,
		`DELETE FROM activity_day_distribution WHERE day_at < ?`,
		`DELETE FROM activity_campaign_sample WHERE day_at < ?`,
		`DELETE FROM activity_user_sample WHERE day_at < ?`,
		`DELETE FROM activity_user_hour_dna WHERE hour_at < ?`,
		`DELETE FROM activity_user_day_dna WHERE day_at < ?`,
		`DELETE FROM activity_user_day_presence WHERE day_at < ?`,
		`DELETE FROM activity_day_dna WHERE day_at < ?`,
		`DELETE FROM activity_hour_dna WHERE hour_at < ?`,
		`DELETE FROM activity_campaign_completion WHERE completed_at < ?`,
		`DELETE FROM activity_signup_sample WHERE day_at < ?`,
		`DELETE FROM activity_population_sample WHERE day_at < ?`,
		`DELETE FROM activity_level_sample WHERE day_at < ?`,
		`DELETE FROM activity_progression_sample WHERE day_at < ?`,
		`DELETE FROM activity_day_sample WHERE day_at < ?`,
		`DELETE FROM activity_day_presence WHERE day_at < ?`,
		`DELETE FROM activity_hour_sample WHERE hour_at < ?`,
		`DELETE FROM activity_hour_presence WHERE hour_at < ?`,
	}
	for index, statement := range pruneStatements {
		err := execStatement(ctx, tx, statement, retentionCutoff)
		if err != nil {
			return fmt.Errorf("pruneStep[%d]: %w", index, err)
		}
	}
	day := at.UTC().Truncate(24 * time.Hour)
	weekday := (int(day.Weekday()) + 6) % 7
	week := day.AddDate(0, 0, -weekday).Unix()
	var completedAt int64
	err := tx.GetContext(ctx, &completedAt, `SELECT COALESCE(MAX(completed_at), 0) FROM activity_maintenance WHERE name = 'weeklyCompaction'`)
	if err != nil {
		return fmt.Errorf("maintenanceRead: %w", err)
	}
	if completedAt >= week {
		return nil
	}
	cutoff := day.AddDate(0, 0, -7).Unix()
	err = compactQueues(ctx, tx, cutoff)
	if err != nil {
		return fmt.Errorf("compactQueues: %w", err)
	}
	err = compactDistribution(ctx, tx, cutoff)
	if err != nil {
		return fmt.Errorf("compactDistribution: %w", err)
	}
	err = compactDNA(ctx, tx, cutoff)
	if err != nil {
		return fmt.Errorf("compactDNA: %w", err)
	}
	err = compactUserDNA(ctx, tx, cutoff)
	if err != nil {
		return fmt.Errorf("compactUserDNA: %w", err)
	}
	statements := []string{
		`INSERT INTO activity_user_day_presence (user_id, day_at, observed_hour_count)
 SELECT user_id, (hour_at / 86400) * 86400, COUNT(*) FROM activity_hour_presence
 WHERE hour_at < ? AND hour_at >= (SELECT (started_at / 3600) * 3600 FROM activity_collection WHERE id = 3)
 AND EXISTS (SELECT 1 FROM "user" WHERE "user".id = activity_hour_presence.user_id)
 GROUP BY 1, 2 ON CONFLICT (user_id, day_at) DO UPDATE SET observed_hour_count = activity_user_day_presence.observed_hour_count + excluded.observed_hour_count`,
		`INSERT INTO activity_day_sample (day_at, online_sum, sample_count, last_sample_at, peak_count, source_count, coverage_count)
   SELECT (hour_at / 86400) * 86400, SUM(online_count), COUNT(*), MAX(hour_at), MAX(peak_count), SUM(source_count), SUM(coverage_count)
   FROM activity_hour_sample WHERE hour_at < ? GROUP BY 1
   ON CONFLICT (day_at) DO UPDATE SET
   online_sum = activity_day_sample.online_sum + excluded.online_sum,
   sample_count = activity_day_sample.sample_count + excluded.sample_count,
   last_sample_at = CASE WHEN excluded.last_sample_at > activity_day_sample.last_sample_at THEN excluded.last_sample_at ELSE activity_day_sample.last_sample_at END,
   peak_count = CASE WHEN excluded.peak_count > activity_day_sample.peak_count THEN excluded.peak_count ELSE activity_day_sample.peak_count END,
   source_count = activity_day_sample.source_count + excluded.source_count,
   coverage_count = activity_day_sample.coverage_count + excluded.coverage_count`,
		`INSERT INTO activity_day_presence (day_at, user_id)
   SELECT DISTINCT (hour_at / 86400) * 86400, user_id FROM activity_hour_presence WHERE hour_at < ?
   ON CONFLICT (day_at, user_id) DO NOTHING`,
		`DELETE FROM activity_hour_sample WHERE hour_at < ?`,
		`DELETE FROM activity_hour_presence WHERE hour_at < ?`,
	}
	for index, statement := range statements {
		err = execStatement(ctx, tx, statement, cutoff)
		if err != nil {
			return fmt.Errorf("compactStep[%d]: %w", index, err)
		}
	}
	err = execStatement(ctx, tx, `INSERT INTO activity_maintenance (name, completed_at) VALUES ('weeklyCompaction', ?)
  ON CONFLICT (name) DO UPDATE SET completed_at = excluded.completed_at`, week)
	if err != nil {
		return fmt.Errorf("maintenanceSave: %w", err)
	}
	return nil
}
