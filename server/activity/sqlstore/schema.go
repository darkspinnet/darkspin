package sqlstore

import (
	"context"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// EnsureSchema migrates legacy minute observations atomically before collection.
func (e *Store) EnsureSchema(ctx context.Context) error {
	tx, err := e.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("schemaBegin: %w", err)
	}
	defer rollback(tx)
	err = e.lock(ctx, tx)
	if err != nil {
		return fmt.Errorf("schemaLock: %w", err)
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS activity_mission (user_id BIGINT NOT NULL REFERENCES "user"(id) ON DELETE CASCADE, result_id BIGINT NOT NULL, campaign_number BIGINT NOT NULL, level_number BIGINT NOT NULL, map_name TEXT NOT NULL, difficulty BIGINT NOT NULL, started_at BIGINT NOT NULL, completed_at BIGINT NOT NULL DEFAULT 0 CHECK (completed_at = 0 OR completed_at >= started_at), PRIMARY KEY (user_id, result_id))`,
		`CREATE INDEX IF NOT EXISTS activity_mission_started ON activity_mission (started_at)`,
		`CREATE INDEX IF NOT EXISTS activity_mission_completed ON activity_mission (user_id, completed_at)`,
		`CREATE TABLE IF NOT EXISTS activity_mission_day (day_at BIGINT PRIMARY KEY, started_count BIGINT NOT NULL CHECK (started_count >= 0), completed_count BIGINT NOT NULL CHECK (completed_count >= 0), resolved_start_count BIGINT NOT NULL CHECK (resolved_start_count >= 0 AND resolved_start_count <= started_count))`,
		`CREATE TABLE IF NOT EXISTS activity_campaign_level (day_at BIGINT NOT NULL, campaign_number BIGINT NOT NULL, level_number BIGINT NOT NULL, completion_count BIGINT NOT NULL CHECK (completion_count >= 0), replay_count BIGINT NOT NULL CHECK (replay_count >= 0 AND replay_count <= completion_count), PRIMARY KEY (day_at, campaign_number, level_number))`,
		`CREATE TABLE IF NOT EXISTS activity_hour_queue (hour_at BIGINT NOT NULL, kind TEXT NOT NULL, observed_at BIGINT NOT NULL, queued_count BIGINT NOT NULL CHECK (queued_count >= 0), wait_second DOUBLE PRECISION NOT NULL CHECK (wait_second >= 0), maximum_wait_second DOUBLE PRECISION NOT NULL CHECK (maximum_wait_second >= 0), PRIMARY KEY (hour_at, kind))`,
		`CREATE TABLE IF NOT EXISTS activity_day_queue (day_at BIGINT NOT NULL, kind TEXT NOT NULL, queued_count BIGINT NOT NULL CHECK (queued_count >= 0), wait_second DOUBLE PRECISION NOT NULL CHECK (wait_second >= 0), maximum_wait_second DOUBLE PRECISION NOT NULL CHECK (maximum_wait_second >= 0), observation_count BIGINT NOT NULL CHECK (observation_count > 0), PRIMARY KEY (day_at, kind))`,
		`CREATE TABLE IF NOT EXISTS activity_hour_distribution (hour_at BIGINT PRIMARY KEY, observed_at BIGINT NOT NULL, pve_count BIGINT NOT NULL CHECK (pve_count >= 0), pvp_count BIGINT NOT NULL CHECK (pvp_count >= 0), lobby_count BIGINT NOT NULL CHECK (lobby_count >= 0), matchmaking_count BIGINT NOT NULL CHECK (matchmaking_count >= 0), other_count BIGINT NOT NULL CHECK (other_count >= 0))`,
		`CREATE TABLE IF NOT EXISTS activity_day_distribution (day_at BIGINT PRIMARY KEY, pve_count BIGINT NOT NULL CHECK (pve_count >= 0), pvp_count BIGINT NOT NULL CHECK (pvp_count >= 0), lobby_count BIGINT NOT NULL CHECK (lobby_count >= 0), matchmaking_count BIGINT NOT NULL CHECK (matchmaking_count >= 0), other_count BIGINT NOT NULL CHECK (other_count >= 0), observation_count BIGINT NOT NULL CHECK (observation_count > 0))`,
		`CREATE TABLE IF NOT EXISTS activity_campaign_sample (day_at BIGINT PRIMARY KEY, completion_count BIGINT NOT NULL CHECK (completion_count >= 0), replay_count BIGINT NOT NULL CHECK (replay_count >= 0 AND replay_count <= completion_count))`,
		`CREATE TABLE IF NOT EXISTS activity_collection (id BIGINT PRIMARY KEY, started_at BIGINT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS activity_hour_dna (hour_at BIGINT PRIMARY KEY, earned BIGINT NOT NULL CHECK (earned >= 0))`,
		`CREATE TABLE IF NOT EXISTS activity_day_dna (day_at BIGINT PRIMARY KEY, earned BIGINT NOT NULL CHECK (earned >= 0))`,
		`CREATE TABLE IF NOT EXISTS activity_user_hour_dna (user_id BIGINT NOT NULL REFERENCES "user"(id) ON DELETE CASCADE, hour_at BIGINT NOT NULL, earned BIGINT NOT NULL CHECK (earned >= 0), PRIMARY KEY (user_id, hour_at))`,
		`CREATE TABLE IF NOT EXISTS activity_user_day_dna (user_id BIGINT NOT NULL REFERENCES "user"(id) ON DELETE CASCADE, day_at BIGINT NOT NULL, earned BIGINT NOT NULL CHECK (earned >= 0), PRIMARY KEY (user_id, day_at))`,
		`CREATE TABLE IF NOT EXISTS activity_user_day_presence (user_id BIGINT NOT NULL REFERENCES "user"(id) ON DELETE CASCADE, day_at BIGINT NOT NULL, observed_hour_count BIGINT NOT NULL, PRIMARY KEY (user_id, day_at))`,
		`CREATE TABLE IF NOT EXISTS activity_user_sample (user_id BIGINT NOT NULL REFERENCES "user"(id) ON DELETE CASCADE, day_at BIGINT NOT NULL, observed_at BIGINT NOT NULL, level BIGINT NOT NULL, chain_progression BIGINT NOT NULL, PRIMARY KEY (user_id, day_at))`,
		`CREATE TABLE IF NOT EXISTS activity_sample (minute_at BIGINT PRIMARY KEY, online_count BIGINT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS activity_presence (minute_at BIGINT NOT NULL, user_id BIGINT NOT NULL, PRIMARY KEY (minute_at, user_id))`,
		`CREATE TABLE IF NOT EXISTS activity_hour_sample (hour_at BIGINT PRIMARY KEY, online_count DOUBLE PRECISION NOT NULL, peak_count BIGINT NOT NULL, source_count BIGINT NOT NULL, coverage_count BIGINT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS activity_hour_presence (hour_at BIGINT NOT NULL, user_id BIGINT NOT NULL, PRIMARY KEY (hour_at, user_id))`,
		`CREATE TABLE IF NOT EXISTS activity_day_sample (day_at BIGINT PRIMARY KEY, online_sum DOUBLE PRECISION NOT NULL, sample_count BIGINT NOT NULL, last_sample_at BIGINT NOT NULL, peak_count BIGINT NOT NULL, source_count BIGINT NOT NULL, coverage_count BIGINT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS activity_day_presence (day_at BIGINT NOT NULL, user_id BIGINT NOT NULL, PRIMARY KEY (day_at, user_id))`,
		`CREATE TABLE IF NOT EXISTS activity_first_admission (user_id BIGINT PRIMARY KEY REFERENCES "user"(id) ON DELETE CASCADE, first_at BIGINT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS activity_first_admission_at ON activity_first_admission (first_at)`,
		`CREATE TABLE IF NOT EXISTS activity_campaign_first_completion (user_id BIGINT NOT NULL REFERENCES "user"(id) ON DELETE CASCADE, campaign_number BIGINT NOT NULL, level_number BIGINT NOT NULL, result_id BIGINT NOT NULL, PRIMARY KEY (user_id, campaign_number, level_number))`,
		`CREATE TABLE IF NOT EXISTS activity_campaign_completion (user_id BIGINT NOT NULL REFERENCES "user"(id) ON DELETE CASCADE, result_id BIGINT NOT NULL, campaign_number BIGINT NOT NULL, level_number BIGINT NOT NULL, completed_at BIGINT NOT NULL, is_replay BIGINT NOT NULL CHECK (is_replay IN (0, 1)), PRIMARY KEY (user_id, result_id))`,
		`CREATE INDEX IF NOT EXISTS activity_campaign_completed ON activity_campaign_completion (user_id, completed_at)`,
		`CREATE TABLE IF NOT EXISTS activity_signup_sample (day_at BIGINT PRIMARY KEY, account_count BIGINT NOT NULL CHECK (account_count >= 0))`,
		`CREATE TABLE IF NOT EXISTS activity_population_sample (day_at BIGINT PRIMARY KEY, observed_at BIGINT NOT NULL, account_count BIGINT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS activity_level_sample (day_at BIGINT PRIMARY KEY, observed_at BIGINT NOT NULL, average_level DOUBLE PRECISION, account_count BIGINT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS activity_progression_sample (day_at BIGINT PRIMARY KEY, observed_at BIGINT NOT NULL, average_progression DOUBLE PRECISION, highest_completed_campaign BIGINT, account_count BIGINT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS activity_maintenance (name TEXT PRIMARY KEY, completed_at BIGINT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS activity_pvp_completed ON pvp_match_result (user_id, completed_at_unix)`,
	}
	for index, statement := range statements {
		err = execStatement(ctx, tx, statement)
		if err != nil {
			return fmt.Errorf("schemaStep[%d]: %w", index, err)
		}
	}
	err = e.pruneMetrics(ctx, tx)
	if err != nil {
		return fmt.Errorf("schemaPrune: %w", err)
	}
	// DNA has its own deployment boundary: existing population observations do
	// not imply that historical reward earnings were collected.
	err = execStatement(ctx, tx, `INSERT INTO activity_collection (id, started_at) VALUES (2, ?) ON CONFLICT (id) DO NOTHING`, time.Now().UTC().Unix())
	if err != nil {
		return fmt.Errorf("schemaDNAStart: %w", err)
	}
	err = execStatement(ctx, tx, `INSERT INTO activity_collection (id, started_at) VALUES (3, ?) ON CONFLICT (id) DO NOTHING`, time.Now().UTC().Unix())
	if err != nil {
		return fmt.Errorf("schemaUserStart: %w", err)
	}
	err = execStatement(ctx, tx, `INSERT INTO activity_collection (id, started_at) VALUES (4, ?) ON CONFLICT (id) DO NOTHING`, time.Now().UTC().Unix())
	if err != nil {
		return fmt.Errorf("schemaAdmissionStart: %w", err)
	}
	err = execStatement(ctx, tx, `INSERT INTO activity_collection (id, started_at) VALUES (5, ?) ON CONFLICT (id) DO NOTHING`, time.Now().UTC().Unix())
	if err != nil {
		return fmt.Errorf("schemaSignupStart: %w", err)
	}
	err = execStatement(ctx, tx, `INSERT INTO activity_collection (id, started_at) VALUES (7, ?) ON CONFLICT (id) DO NOTHING`, time.Now().UTC().Unix())
	if err != nil {
		return fmt.Errorf("schemaCompletionStart: %w", err)
	}
	// This anonymous ranking begins at deployment; older receipts cannot recover
	// completions from accounts already purged from the server.
	err = execStatement(ctx, tx, `INSERT INTO activity_collection (id, started_at) VALUES (10, ?) ON CONFLICT (id) DO NOTHING`, time.Now().UTC().Unix())
	if err != nil {
		return fmt.Errorf("schemaCampaignLevelStart: %w", err)
	}
	err = execStatement(ctx, tx, `INSERT INTO activity_collection (id, started_at) VALUES (11, ?) ON CONFLICT (id) DO NOTHING`, time.Now().UTC().Unix())
	if err != nil {
		return fmt.Errorf("schemaMissionStart: %w", err)
	}
	err = migrateMinutes(ctx, tx)
	if err != nil {
		return fmt.Errorf("schemaMigrate: %w", err)
	}
	err = maintain(ctx, tx, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("schemaMaintain: %w", err)
	}
	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("schemaCommit: %w", err)
	}
	return nil
}

func migrateMinutes(ctx context.Context, tx *sqlx.Tx) error {
	var migrationCount int64
	err := tx.GetContext(ctx, &migrationCount, `SELECT COUNT(*) FROM activity_maintenance WHERE name = 'hourlyMigration'`)
	if err != nil {
		return fmt.Errorf("migrationRead: %w", err)
	}
	if migrationCount > 0 {
		return nil
	}
	// Legacy minute peaks become one hourly mean, while their peak and source
	// count survive packing. Incomplete legacy hours remain partial coverage.
	statements := []string{
		`INSERT INTO activity_hour_sample (hour_at, online_count, peak_count, source_count, coverage_count)
   SELECT (minute_at / 3600) * 3600, AVG(CAST(online_count AS DOUBLE PRECISION)), MAX(online_count), COUNT(*), CASE WHEN COUNT(*) = 60 THEN 1 ELSE 0 END
   FROM activity_sample GROUP BY 1`,
		`INSERT INTO activity_hour_presence (hour_at, user_id)
   SELECT DISTINCT (minute_at / 3600) * 3600, user_id FROM activity_presence`,
		`DELETE FROM activity_sample`,
		`DELETE FROM activity_presence`,
		`INSERT INTO activity_maintenance (name, completed_at) VALUES ('hourlyMigration', 1)`,
	}
	for index, statement := range statements {
		err = execStatement(ctx, tx, statement)
		if err != nil {
			return fmt.Errorf("migrationStep[%d]: %w", index, err)
		}
	}
	return nil
}
