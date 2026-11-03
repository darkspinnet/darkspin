package sqlstore

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/activity"
	"github.com/jmoiron/sqlx"
)

// Anonymous totals commit with the new receipt and survive account purging.
// A separate boundary avoids claiming complete totals from older replay-only data.
func recordCampaignSample(ctx context.Context, tx *sqlx.Tx, req activity.CampaignCompletion, isReplay int64) error {
	var startedAt int64
	err := tx.GetContext(ctx, &startedAt, `SELECT started_at FROM activity_collection WHERE id = 7`)
	if err != nil {
		return fmt.Errorf("completionBoundary: %w", err)
	}
	if req.At.Unix() < startedAt {
		return nil
	}
	var completionCount int64
	err = tx.GetContext(ctx, &completionCount, tx.Rebind(`INSERT INTO activity_campaign_sample (day_at, completion_count, replay_count)
 VALUES (?, 1, ?) ON CONFLICT (day_at) DO UPDATE SET completion_count = activity_campaign_sample.completion_count + 1,
 replay_count = activity_campaign_sample.replay_count + excluded.replay_count
 WHERE activity_campaign_sample.completion_count < ? RETURNING completion_count`), req.At.UTC().Truncate(24*time.Hour).Unix(), isReplay, int64(math.MaxInt64))
	if err != nil {
		return fmt.Errorf("completionTotal: %w", err)
	}
	return nil
}

type campaignSampleRow struct {
	DayAt           int64 `db:"day_at"`
	CompletionCount int64 `db:"completion_count"`
	ReplayCount     int64 `db:"replay_count"`
}

type campaignSampleReader interface {
	GetContext(context.Context, any, string, ...any) error
	SelectContext(context.Context, any, string, ...any) error
	Rebind(string) string
}

func readCampaignSamples(ctx context.Context, reader campaignSampleReader, req activity.Window, userID int64) (time.Time, []activity.CampaignSample, error) {
	var startedAt int64
	err := reader.GetContext(ctx, &startedAt, `SELECT COALESCE(MAX(started_at), 0) FROM activity_collection WHERE id = 7`)
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("completionStart: %w", err)
	}
	samples := make([]activity.CampaignSample, 0)
	if startedAt == 0 {
		return time.Time{}, samples, nil
	}
	start := req.Since.UTC().Truncate(24 * time.Hour).Unix()
	rows := make([]campaignSampleRow, 0)
	if userID > 0 {
		// The boundary is exact for receipts, while the graph groups by UTC day.
		err = reader.SelectContext(ctx, &rows, reader.Rebind(`SELECT (completed_at / 86400) * 86400 AS day_at,
 COUNT(*) AS completion_count, SUM(is_replay) AS replay_count FROM activity_campaign_completion
 WHERE user_id = ? AND completed_at >= ? AND completed_at >= ? AND completed_at <= ?
 GROUP BY 1 ORDER BY 1`), userID, startedAt, start, req.Until.Unix())
	} else {
		err = reader.SelectContext(ctx, &rows, reader.Rebind(`SELECT day_at, completion_count, replay_count
 FROM activity_campaign_sample WHERE day_at >= ? AND day_at <= ? ORDER BY day_at`), start, req.Until.Unix())
	}
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("completionRead: %w", err)
	}
	for _, row := range rows {
		samples = append(samples, activity.CampaignSample{At: time.Unix(row.DayAt, 0).UTC(), CompletionCount: row.CompletionCount, RepeatCount: row.ReplayCount})
	}
	return time.Unix(startedAt, 0).UTC(), samples, nil
}
