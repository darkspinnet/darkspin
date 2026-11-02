package maintenance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"
)

// ScheduleOptions enables managed updates for a bootstrap child. An empty Cron
// disables automatic occurrences while retaining explicit update requests.
type ScheduleOptions struct {
	Cron              string
	WarningMinutes    int
	UpdateRequestPath string
}

var ErrWarningActive = errors.New("a warning-only countdown is active; cancel it before requesting an update")

// EnableUpdates permits local update requests when a bootstrap supervisor owns
// this server's clean-stop marker and subsequent backup/update lifecycle.
func (e *Control) EnableUpdates() {
	e.announcement.mu.Lock()
	e.announcement.areUpdatesEnabled = true
	e.announcement.mu.Unlock()
}

// IsScheduledUpdatePending identifies the managed shutdown countdown, including
// explicit update requests. Warning-only announcements cannot authorize it.
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
	if req.Cron == "" {
		return nil
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

// RunSchedule runs automatic occurrences and explicit update requests through
// one countdown owner. Cancellation skips the current request or occurrence.
func (e *Control) RunSchedule(ctx context.Context, req ScheduleOptions) (bool, error) {
	var schedule cronSchedule
	if req.Cron != "" {
		var err error
		schedule, err = parseCron(req.Cron)
		if err != nil {
			return false, fmt.Errorf("scheduleParse: %w", err)
		}
	}
	processedID := ""
	for ctx.Err() == nil {
		state := e.announcement.updateState()
		if state.IsActive && state.ID != processedID {
			processedID = state.ID
			isDue, countdownErr := e.runCountdown(ctx, state)
			if countdownErr != nil {
				return false, fmt.Errorf("scheduleCountdown: %w", countdownErr)
			}
			if isDue {
				return true, nil
			}
			continue
		}
		var timer *time.Timer
		var tick <-chan time.Time
		var next time.Time
		if req.Cron != "" {
			var nextErr error
			next, nextErr = schedule.next(time.Now())
			if nextErr != nil {
				return false, fmt.Errorf("scheduleNext: %w", nextErr)
			}
			timer = time.NewTimer(time.Until(next))
			tick = timer.C
		}
		select {
		case <-ctx.Done():
		case <-e.announcement.changed:
		case <-tick:
			id, beginErr := e.announcement.beginScheduled(next, req.WarningMinutes)
			if beginErr != nil {
				return false, fmt.Errorf("countdownBegin: %w", beginErr)
			}
			if id == "" {
				continue
			}
		}
		if timer != nil {
			timer.Stop()
		}
	}
	return false, nil
}

func (e *Control) runCountdown(ctx context.Context, state countdown) (bool, error) {
	id := state.ID
	start := state.StartedAt
	warningMinuteCount := state.WarningMinuteCount
	for remaining := warningMinuteCount; remaining >= 0; remaining-- {
		if remaining != warningMinuteCount && remaining > 5 && remaining%10 != 0 {
			continue
		}
		when := start.Add(time.Duration(warningMinuteCount-remaining) * time.Minute)
		if !e.waitForCountdown(ctx, when, id) {
			return false, nil
		}
		message := fmt.Sprintf("Server maintenance: restarting in %d minutes. Please finish your game and log out before then.", remaining)
		if remaining == 0 {
			message = "Server maintenance: restarting now. Disconnecting players and saving server data."
		}
		isActive, broadcastErr := e.announcement.remindScheduled(ctx, id, message)
		if broadcastErr != nil {
			slog.Warn("Maintenance notification failed; retaining fixed deadline",
				"countdown_id", id, "remaining_minutes", remaining, "error", broadcastErr)
		}
		if !isActive {
			return false, nil
		}
	}
	return ctx.Err() == nil, nil
}

func (e *Control) waitForCountdown(ctx context.Context, deadline time.Time, id string) bool {
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-e.announcement.changed:
			state := e.announcement.updateState()
			if !state.IsActive || state.ID != id {
				return false
			}
		case <-timer.C:
			return ctx.Err() == nil
		}
	}
}

func (e *announcement) beginScheduled(start time.Time, warningMinuteCount int) (string, error) {
	e.operationMu.Lock()
	defer e.operationMu.Unlock()
	if e.status().IsActive {
		return "", nil
	}
	state, err := e.newUpdate(start, warningMinuteCount, true)
	if err != nil {
		return "", fmt.Errorf("scheduledUpdate: %w", err)
	}
	return state.ID, nil
}

// newUpdate is called while operationMu owns the countdown transition.
func (e *announcement) newUpdate(start time.Time, warningMinuteCount int, isReady bool) (countdown, error) {
	id, err := randomToken()
	if err != nil {
		return countdown{}, fmt.Errorf("countdownID: %w", err)
	}
	e.mu.Lock()
	e.state = countdown{ID: id, Deadline: start.Add(time.Duration(warningMinuteCount) * time.Minute),
		StartedAt: start, WarningMinuteCount: warningMinuteCount, IsActive: true}
	e.scheduledID = id
	e.isUpdateReady = isReady
	e.isAccepted = true
	e.isCancellationPending = false
	state := e.state
	e.mu.Unlock()
	return state, nil
}

func (e *announcement) requestUpdate(warningMinuteCount int) (countdown, error) {
	if warningMinuteCount < 0 || warningMinuteCount > 1440 {
		return countdown{}, errors.New("update warning must be between 0 and 1440 minutes")
	}
	e.operationMu.Lock()
	defer e.operationMu.Unlock()
	e.mu.Lock()
	isEnabled := e.areUpdatesEnabled
	state := e.state
	isUpdate := state.ID == e.scheduledID
	e.mu.Unlock()
	if !isEnabled {
		return state, errors.New("self-update requires a bootstrap-managed server")
	}
	if state.IsActive {
		if isUpdate {
			return state, nil
		}
		return state, ErrWarningActive
	}
	state, err := e.newUpdate(time.Now().UTC(), warningMinuteCount, false)
	if err != nil {
		return countdown{}, fmt.Errorf("manualUpdate: %w", err)
	}
	return state, nil
}

func (e *announcement) activateUpdate(id string) {
	e.mu.Lock()
	if e.state.IsActive && e.state.ID == id && e.scheduledID == id {
		e.isUpdateReady = true
	}
	e.mu.Unlock()
	e.notifyChange()
}

func (e *announcement) discardUnstartedUpdate(id string) {
	e.operationMu.Lock()
	defer e.operationMu.Unlock()
	e.mu.Lock()
	if e.state.ID != id || e.isUpdateReady {
		e.mu.Unlock()
		return
	}
	e.state.IsActive = false
	e.isAccepted = false
	e.mu.Unlock()
	e.notifyChange()
}

func (e *announcement) updateState() countdown {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.isUpdateReady || e.state.ID != e.scheduledID {
		return countdown{}
	}
	return e.state
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
