package maintenance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"path/filepath"
	"time"
)

type ScheduleOptions struct {
	Cron              string
	WarningMinutes    int
	UpdateRequestPath string
}

func (e *Control) EnableUpdates() {
	e.announcement.mu.Lock()
	e.announcement.areUpdatesEnabled = true
	e.announcement.mu.Unlock()
}

func (e *Control) IsScheduledUpdatePending() bool {
	state := e.Status()
	return state.IsActive && state.Phase == "stopping" && (state.Action == "update" || state.Action == "restart")
}

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

// RunSchedule owns the only countdown loop, including direct-server holds.
func (e *Control) RunSchedule(ctx context.Context, req ScheduleOptions) (bool, error) {
	if ctx == nil {
		return false, errors.New("nil maintenance context")
	}
	var schedule cronSchedule
	var next time.Time
	if req.Cron != "" {
		var err error
		schedule, err = parseCron(req.Cron)
		if err != nil {
			return false, fmt.Errorf("scheduleParse: %w", err)
		}
		next, err = schedule.next(time.Now())
		if err != nil {
			return false, fmt.Errorf("scheduleNext: %w", err)
		}
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		now := time.Now().UTC()
		if !next.IsZero() && !now.Before(next) {
			operationID, idErr := randomToken()
			if idErr != nil {
				return false, fmt.Errorf("scheduledID: %w", idErr)
			}
			state, requestErr := e.requestLocal(ctx, Request{Action: "update", WarningMinutes: req.WarningMinutes, OperationID: operationID, ActorType: "system"})
			if requestErr != nil && !errors.Is(requestErr, ErrConflict) {
				return false, fmt.Errorf("scheduledRequest: %w", requestErr)
			}
			// An existing same-action request is idempotent. Its acceptance may
			// still be flushing, so only activate this cron occurrence's own ID.
			if requestErr == nil && state.ID == operationID {
				activateErr := e.activateLocal(ctx, state.ID)
				if activateErr != nil {
					return false, fmt.Errorf("scheduledActivate: %w", activateErr)
				}
			}
			var nextErr error
			next, nextErr = schedule.next(now)
			if nextErr != nil {
				return false, fmt.Errorf("scheduleAdvance: %w", nextErr)
			}
		}
		isDue, err := e.advance(ctx, now)
		if err != nil {
			return false, fmt.Errorf("countdownAdvance: %w", err)
		}
		if isDue {
			return true, nil
		}
		select {
		case <-ctx.Done():
		case <-e.announcement.changed:
		case <-ticker.C:
		}
	}
	return false, nil
}

func (e *Control) advance(ctx context.Context, now time.Time) (bool, error) {
	a := &e.announcement
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	state := a.status()
	if !state.IsActive || state.Action == "announce" || (state.Phase != "warning" && state.Phase != "draining") {
		return false, nil
	}
	isDue := !now.Before(state.Deadline)
	a.mu.Lock()
	observer := a.drainObserver
	a.mu.Unlock()
	if observer != nil && observer.ActiveMatchCount() == 0 {
		isDue = true
	}
	remaining := int(math.Ceil(state.Deadline.Sub(now).Minutes()))
	if remaining < 0 || isDue && state.Action != "hold" {
		remaining = 0
	}
	isReminder := remaining == state.WarningMinuteCount || remaining <= 5 || remaining%10 == 0
	if isReminder && remaining < state.LastReminderMinute {
		message := fmt.Sprintf("Server maintenance: restarting in %d minutes. Please finish your game and log out before then.", remaining)
		if state.Action == "hold" {
			message = "Server maintenance: new games are paused while current games finish."
		}
		if remaining == 0 && state.Action != "hold" {
			message = "Server maintenance: restarting now. Disconnecting players and saving server data."
		}
		delivered, broadcastErr := a.broadcast(ctx, message)
		state.Delivered, state.LastReminderMinute = delivered, remaining
		err := a.commit(state)
		if err != nil {
			return false, fmt.Errorf("reminderSave: %w", err)
		}
		if broadcastErr != nil {
			slog.Warn("Maintenance notification failed; retaining fixed deadline", "operation_id", state.ID, "error", broadcastErr)
		}
	}
	if ctx.Err() != nil {
		return false, nil
	}
	if !isDue {
		return false, nil
	}
	if state.Action == "hold" && observer != nil && (state.IsDrainPending || observer.ActiveMatchCount() > 0) {
		forceObserver, isSupported := observer.(ForceDrainObserver)
		if !isSupported {
			return false, errors.New("hold deadline cleanup is unavailable")
		}
		if !state.IsDrainPending {
			state.IsDrainPending = true
			persistErr := a.commit(state)
			if persistErr != nil {
				return false, fmt.Errorf("drainClaim: %w", persistErr)
			}
		}
		// Disconnect waits for gameplay and identity owners. A remote change
		// owns the admission writer before acquiring operationMu, so retaining
		// operationMu here would invert those owners against gameplay entry.
		a.operationMu.Unlock()
		drainCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		drainErr := forceObserver.ForceDrain(drainCtx)
		cancel()
		a.operationMu.Lock()
		current := a.status()
		if current.ID != state.ID || !current.IsActive || current.Phase != state.Phase {
			return false, nil
		}
		state = current
		if drainErr != nil {
			return false, fmt.Errorf("holdDrain: %w", drainErr)
		}
		if ctx.Err() != nil {
			return false, nil
		}
		if observer.ActiveMatchCount() > 0 {
			return false, errors.New("hold deadline cleanup has active matches remaining")
		}
		state.IsDrainPending = false
	}
	if state.Action == "hold" && observer == nil {
		return false, errors.New("hold drain observation is unavailable")
	}
	state.Phase = "stopping"
	if state.Action == "hold" {
		state.Phase = "held"
	}
	err := a.commit(state)
	if err != nil {
		return false, fmt.Errorf("deadlineSave: %w", err)
	}
	return state.Phase == "stopping", nil
}
