package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/activity"
	"github.com/jmoiron/sqlx"
)

var _ activity.MissionStore = (*Store)(nil)

func (e *Store) BeginMission(ctx context.Context, req activity.MissionStart) error {
	err := activity.ValidateMissionStart(req)
	if err != nil {
		return fmt.Errorf("missionValidate: %w", err)
	}
	tx, err := e.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("missionBegin: %w", err)
	}
	defer rollback(tx)
	// Use the completion lock so a concurrent receipt cannot precede its start.
	err = lockMission(ctx, tx)
	if err != nil {
		return fmt.Errorf("missionLock: %w", err)
	}
	var boundary int64
	err = tx.GetContext(ctx, &boundary, `SELECT started_at FROM activity_collection WHERE id = 11`)
	if err != nil {
		return fmt.Errorf("missionBoundary: %w", err)
	}
	if req.At.Unix() < boundary {
		return nil
	}
	var resultID uint64
	err = tx.GetContext(ctx, &resultID, tx.Rebind(`INSERT INTO activity_mission
 (user_id, result_id, campaign_number, level_number, map_name, difficulty, started_at)
 SELECT id, ?, ?, ?, ?, ?, ? FROM "user" WHERE id = ? AND trmt_id = 0
 AND NOT EXISTS (SELECT 1 FROM activity_campaign_completion WHERE user_id = ? AND result_id = ?)
 ON CONFLICT (user_id, result_id) DO NOTHING RETURNING result_id`), req.ResultID, req.CampaignNumber,
		req.LevelNumber, req.Map, req.Difficulty, req.At.UTC().Unix(), req.UserID, req.UserID, req.ResultID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("missionInsert: %w", err)
	}
	if resultID != req.ResultID {
		return errors.New("mission start identity conflict")
	}
	var startedCount int64
	err = tx.GetContext(ctx, &startedCount, tx.Rebind(`INSERT INTO activity_mission_day
 (day_at, started_count, completed_count, resolved_start_count) VALUES (?, 1, 0, 0)
 ON CONFLICT (day_at) DO UPDATE SET started_count = activity_mission_day.started_count + 1
 WHERE activity_mission_day.started_count < ? RETURNING started_count`), req.At.UTC().Truncate(24*time.Hour).Unix(), int64(math.MaxInt64))
	if err != nil {
		return fmt.Errorf("missionStarted: %w", err)
	}
	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("missionCommit: %w", err)
	}
	return nil
}

func lockMission(ctx context.Context, tx *sqlx.Tx) error {
	if tx.DriverName() != "pgx" && tx.DriverName() != "postgres" {
		return nil
	}
	err := execStatement(ctx, tx, `SELECT pg_advisory_xact_lock(1936748398, 3)`)
	if err != nil {
		return fmt.Errorf("missionAdvisory: %w", err)
	}
	return nil
}

// completeMission runs only for a newly inserted authoritative success receipt.
// Unknown starts stay unknown; checkpoint replay cannot duplicate a resolution.
func completeMission(ctx context.Context, tx *sqlx.Tx, req activity.CampaignCompletion) error {
	var startedAt int64
	err := tx.GetContext(ctx, &startedAt, tx.Rebind(`UPDATE activity_mission SET completed_at = ?
 WHERE user_id = ? AND result_id = ? AND completed_at = 0 AND started_at <= ?
 RETURNING started_at`), req.At.UTC().Unix(), req.UserID, req.ResultID, req.At.UTC().Unix())
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("missionResolve: %w", err)
	}
	var completedCount int64
	err = tx.GetContext(ctx, &completedCount, tx.Rebind(`INSERT INTO activity_mission_day
 (day_at, started_count, completed_count, resolved_start_count) VALUES (?, 0, 1, 0)
 ON CONFLICT (day_at) DO UPDATE SET completed_count = activity_mission_day.completed_count + 1
 WHERE activity_mission_day.completed_count < ? RETURNING completed_count`), req.At.UTC().Truncate(24*time.Hour).Unix(), int64(math.MaxInt64))
	if err != nil {
		return fmt.Errorf("missionCompleted: %w", err)
	}
	var resolvedCount int64
	err = tx.GetContext(ctx, &resolvedCount, tx.Rebind(`UPDATE activity_mission_day
 SET resolved_start_count = resolved_start_count + 1
 WHERE day_at = ? AND resolved_start_count < started_count AND resolved_start_count < ?
 RETURNING resolved_start_count`), time.Unix(startedAt, 0).UTC().Truncate(24*time.Hour).Unix(), int64(math.MaxInt64))
	if err != nil {
		return fmt.Errorf("missionResolved: %w", err)
	}
	return nil
}
