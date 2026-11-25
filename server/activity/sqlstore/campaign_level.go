package sqlstore

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/activity"
	"github.com/jmoiron/sqlx"
)

// recordCampaignLevel runs only after inserting a new completion receipt in the
// same reward transaction. Anonymous totals survive deletion of that account.
func recordCampaignLevel(ctx context.Context, tx *sqlx.Tx, req activity.CampaignCompletion, isReplay int64) error {
	var startedAt int64
	err := tx.GetContext(ctx, &startedAt, `SELECT started_at FROM activity_collection WHERE id = 10`)
	if err != nil {
		return fmt.Errorf("levelBoundary: %w", err)
	}
	if req.At.Before(time.Unix(startedAt, 0)) {
		return nil
	}
	var completionCount int64
	err = tx.GetContext(ctx, &completionCount, tx.Rebind(`INSERT INTO activity_campaign_level (day_at, campaign_number, level_number, completion_count, replay_count)
 VALUES (?, ?, ?, 1, ?) ON CONFLICT (day_at, campaign_number, level_number) DO UPDATE SET
 completion_count = activity_campaign_level.completion_count + 1,
 replay_count = activity_campaign_level.replay_count + excluded.replay_count
 WHERE activity_campaign_level.completion_count < ? RETURNING completion_count`),
		req.At.UTC().Truncate(24*time.Hour).Unix(), req.CampaignNumber, req.LevelNumber, isReplay, int64(math.MaxInt64))
	if err != nil {
		return fmt.Errorf("levelTotal: %w", err)
	}
	return nil
}

type campaignLevelRow struct {
	CampaignNumber  uint32 `db:"campaign_number"`
	LevelNumber     uint32 `db:"level_number"`
	CompletionCount int64  `db:"completion_count"`
	RepeatCount     int64  `db:"repeat_count"`
}

// readCampaignLevels matches daily charts' containing UTC day. Personal receipts
// also use the exact collection boundary so both scopes begin with the same event.
func readCampaignLevels(ctx context.Context, reader campaignSampleReader, req activity.Window, userID int64) (time.Time, []activity.CampaignLevel, error) {
	var startedAt int64
	err := reader.GetContext(ctx, &startedAt, `SELECT COALESCE(MAX(started_at), 0) FROM activity_collection WHERE id = 10`)
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("levelStart: %w", err)
	}
	levels := make([]activity.CampaignLevel, 0)
	if startedAt == 0 {
		return time.Time{}, levels, nil
	}
	start := req.Since.UTC().Truncate(24 * time.Hour).Unix()
	rows := make([]campaignLevelRow, 0)
	if userID > 0 {
		err = reader.SelectContext(ctx, &rows, reader.Rebind(`SELECT campaign_number, level_number,
 COUNT(*) AS completion_count, SUM(is_replay) AS repeat_count FROM activity_campaign_completion
 WHERE user_id = ? AND completed_at >= ? AND completed_at >= ? AND completed_at <= ?
 GROUP BY campaign_number, level_number HAVING COUNT(*) > 0
 ORDER BY completion_count DESC, campaign_number ASC, level_number ASC LIMIT 10`), userID, startedAt, start, req.Until.Unix())
	} else {
		err = reader.SelectContext(ctx, &rows, reader.Rebind(`SELECT campaign_number, level_number,
 SUM(completion_count) AS completion_count, SUM(replay_count) AS repeat_count FROM activity_campaign_level
 WHERE day_at >= ? AND day_at <= ? GROUP BY campaign_number, level_number HAVING SUM(completion_count) > 0
 ORDER BY completion_count DESC, campaign_number ASC, level_number ASC LIMIT 10`), start, req.Until.Unix())
	}
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("levelRanking: %w", err)
	}
	for _, row := range rows {
		levels = append(levels, activity.CampaignLevel{CampaignNumber: row.CampaignNumber, LevelNumber: row.LevelNumber, CompletionCount: row.CompletionCount, RepeatCount: row.RepeatCount})
	}
	return time.Unix(startedAt, 0).UTC(), levels, nil
}
