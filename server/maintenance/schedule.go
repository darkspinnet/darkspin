package maintenance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"
)

// ScheduleOptions enables server-owned UTC maintenance for a bootstrap child.
type ScheduleOptions struct {
	Cron              string
	WarningMinutes    int
	UpdateRequestPath string
}

// IsScheduledUpdatePending remains false after cancellation, even if a new
// manually announced countdown subsequently starts.
func (e *Control) IsScheduledUpdatePending() bool {
	e.announcement.mu.Lock()
	defer e.announcement.mu.Unlock()
	return e.announcement.scheduledID != "" && e.announcement.state.IsActive &&
		e.announcement.state.ID == e.announcement.scheduledID
}

// ValidateSchedule rejects invalid schedules before listeners or storage open.
func ValidateSchedule(req ScheduleOptions) error {
	if req.WarningMinutes < 1 || req.WarningMinutes > 1440 {
		return errors.New("maintenance warning must be between 1 and 1440 minutes")
	}
	if !filepath.IsAbs(req.UpdateRequestPath) {
		return errors.New("maintenance update request path must be absolute")
	}
	schedule, err := parseCron(req.Cron)
	if err != nil {
		return fmt.Errorf("scheduleParse: %w", err)
	}
	next, err := schedule.next(time.Now())
	if err != nil {
		return fmt.Errorf("scheduleNext: %w", err)
	}
	if next.IsZero() {
		return errors.New("maintenance schedule has no next occurrence")
	}
	return nil
}

// RunSchedule blocks until its countdown requests shutdown or ctx is cancelled.
// A cancelled countdown skips that occurrence and waits for the next cron time.
func (e *Control) RunSchedule(ctx context.Context, req ScheduleOptions) (bool, error) {
	schedule, err := parseCron(req.Cron)
	if err != nil {
		return false, fmt.Errorf("scheduleParse: %w", err)
	}
	for ctx.Err() == nil {
		next, nextErr := schedule.next(time.Now())
		if nextErr != nil {
			return false, fmt.Errorf("scheduleNext: %w", nextErr)
		}
		if !waitUntil(ctx, next) {
			return false, nil
		}
		isDue, countdownErr := e.runCountdown(ctx, next, req.WarningMinutes)
		if countdownErr != nil {
			return false, fmt.Errorf("scheduleCountdown: %w", countdownErr)
		}
		if isDue {
			return true, nil
		}
	}
	return false, nil
}

func (e *Control) runCountdown(ctx context.Context, start time.Time, warningMinuteCount int) (bool, error) {
	id, err := e.announcement.beginScheduled(start.Add(time.Duration(warningMinuteCount) * time.Minute))
	if err != nil {
		return false, fmt.Errorf("countdownBegin: %w", err)
	}
	if id == "" {
		return false, nil
	}
	for remaining := warningMinuteCount; remaining >= 0; remaining-- {
		if remaining != warningMinuteCount && remaining > 5 && remaining%10 != 0 {
			continue
		}
		when := start.Add(time.Duration(warningMinuteCount-remaining) * time.Minute)
		if !waitUntil(ctx, when) {
			return false, nil
		}
		message := fmt.Sprintf("Server maintenance: restarting in %d minutes. Please finish your game and log out before then.", remaining)
		if remaining == 0 {
			message = "Server maintenance: restarting now. Disconnecting players and saving server data."
		}
		isActive, broadcastErr := e.announcement.remindScheduled(ctx, id, message)
		if broadcastErr != nil {
			slog.Warn("Scheduled maintenance notification failed; retaining fixed deadline",
				"countdown_id", id, "remaining_minutes", remaining, "error", broadcastErr)
		}
		if !isActive {
			return false, nil
		}
	}
	return ctx.Err() == nil, nil
}

func waitUntil(ctx context.Context, deadline time.Time) bool {
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return ctx.Err() == nil
	}
}

func (e *announcement) beginScheduled(deadline time.Time) (string, error) {
	e.operationMu.Lock()
	defer e.operationMu.Unlock()
	if e.status().IsActive {
		return "", nil
	}
	id, err := randomToken()
	if err != nil {
		return "", fmt.Errorf("countdownID: %w", err)
	}
	e.mu.Lock()
	e.state = countdown{ID: id, Deadline: deadline, IsActive: true}
	e.scheduledID = id
	e.isAccepted = true
	e.isCancellationPending = false
	e.mu.Unlock()
	return id, nil
}

func (e *announcement) remindScheduled(ctx context.Context, id, message string) (bool, error) {
	e.operationMu.Lock()
	defer e.operationMu.Unlock()
	state := e.status()
	if ctx.Err() != nil || !state.IsActive || state.ID != id {
		return false, nil
	}
	delivered, err := e.broadcast(ctx, message)
	e.mu.Lock()
	e.state.Delivered = delivered
	e.mu.Unlock()
	if err != nil {
		return ctx.Err() == nil, fmt.Errorf("reminderSend: %w", err)
	}
	return ctx.Err() == nil, nil
}
