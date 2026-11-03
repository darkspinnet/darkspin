package sqlstore

import (
	"context"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/activity"
	"github.com/jmoiron/sqlx"
)

type userDayRow struct {
	DayAt       int64  `db:"day_at"`
	ObservedAt  int64  `db:"observed_at"`
	Level       uint32 `db:"level"`
	Progression uint32 `db:"chain_progression"`
}

// A collector samples all saved accounts once per UTC day. Login fills only a
// missing user's row; no historical state is reconstructed from today's save.
func recordUserDays(ctx context.Context, tx *sqlx.Tx, at time.Time, userID int64) error {
	day := at.UTC().Truncate(24 * time.Hour).Unix()
	if userID == 0 {
		var completedAt int64
		err := tx.GetContext(ctx, &completedAt, `SELECT COALESCE(MAX(completed_at), 0) FROM activity_maintenance WHERE name = 'userDailySnapshot'`)
		if err != nil {
			return fmt.Errorf("userDayCheck: %w", err)
		}
		if completedAt >= day {
			return nil
		}
	}
	err := execStatement(ctx, tx, `INSERT INTO activity_user_sample
 (user_id, day_at, observed_at, level, chain_progression)
 SELECT u.id, ?, ?, u.level, u.chain_progression
 FROM "user" AS u
 WHERE u.trmt_id = 0 AND (? = 0 OR u.id = ?)
 ON CONFLICT (user_id, day_at) DO NOTHING`, day, at.UTC().Unix(), userID, userID)
	if err != nil {
		return fmt.Errorf("userDayInsert: %w", err)
	}
	if userID != 0 {
		return nil
	}
	err = execStatement(ctx, tx, `INSERT INTO activity_maintenance (name, completed_at) VALUES ('userDailySnapshot', ?) ON CONFLICT (name) DO UPDATE SET completed_at = excluded.completed_at`, day)
	if err != nil {
		return fmt.Errorf("userDayMark: %w", err)
	}
	return nil
}

func (e userDayRow) day() activity.UserDay {
	return activity.UserDay{At: time.Unix(e.DayAt, 0).UTC(), ObservedAt: time.Unix(e.ObservedAt, 0).UTC(), Level: e.Level, Progression: e.Progression}
}

func (e *Store) ReadUserDaily(ctx context.Context, userID int64, req activity.Window) (activity.UserSummary, error) {
	userDayRows := make([]userDayRow, 0)
	// Include exactly one preceding observation as an explicitly dated baseline.
	err := e.db.SelectContext(ctx, &userDayRows, e.db.Rebind(`SELECT day_at, observed_at, level, chain_progression
 FROM activity_user_sample WHERE user_id = ? AND observed_at <= ? AND
 (day_at >= ? OR day_at = (SELECT MAX(day_at) FROM activity_user_sample WHERE user_id = ? AND day_at < ?)) ORDER BY day_at`), userID, req.Until.Unix(), req.Since.Truncate(24*time.Hour).Unix(), userID, req.Since.Truncate(24*time.Hour).Unix())
	if err != nil {
		return activity.UserSummary{}, fmt.Errorf("userDaysRead: %w", err)
	}
	pvpSamples, err := e.readUserPVP(ctx, userID, req)
	if err != nil {
		return activity.UserSummary{}, fmt.Errorf("userPVP: %w", err)
	}
	result := activity.UserSummary{PVPSamples: pvpSamples, Days: make([]activity.UserDay, 0, len(userDayRows))}
	result.MissionStartedAt, result.MissionSamples, err = readMissionSamples(ctx, e.db, req, userID)
	if err != nil {
		return activity.UserSummary{}, fmt.Errorf("userMission: %w", err)
	}
	result.CampaignLevelStartedAt, result.CampaignLevels, err = readCampaignLevels(ctx, e.db, req, userID)
	if err != nil {
		return activity.UserSummary{}, fmt.Errorf("userCampaignLevel: %w", err)
	}
	result.CampaignStartedAt, result.CampaignSamples, err = readCampaignSamples(ctx, e.db, req, userID)
	if err != nil {
		return activity.UserSummary{}, fmt.Errorf("userCampaignSample: %w", err)
	}
	for _, row := range userDayRows {
		result.Days = append(result.Days, row.day())
	}
	return result, nil
}
