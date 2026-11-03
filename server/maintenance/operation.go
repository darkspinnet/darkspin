// Package maintenance owns the server's shared maintenance operation lifecycle.
package maintenance

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

type Broadcast func(context.Context, string) (int, error)

type State struct {
	ID                 string
	Action             string
	Reason             string
	Phase              string
	Deadline           time.Time
	IsActive           bool
	IsAdmissionBlocked bool
	Delivered          int
}

type Request struct {
	Action         string
	Reason         string
	WarningMinutes int
	// OperationID is assigned by a trusted journal, never an HTTP DTO.
	OperationID string
	// ActorType is set only by trusted in-process callers, never an HTTP DTO.
	ActorType string
}

type DrainObserver interface{ ActiveMatchCount() int }

// ForceDrainObserver stops remaining game instances at a hold deadline without
// stopping listeners or the server lifecycle itself.
type ForceDrainObserver interface {
	ForceDrain(context.Context) error
}

var ErrConflict = errors.New("another maintenance operation is active")
var ErrWarningActive = ErrConflict
var ErrUnsupported = errors.New("maintenance action requires a bootstrap-managed server")
var ErrStopping = errors.New("maintenance is already stopping the server")
var ErrInvalidRequest = errors.New("invalid maintenance request")

type countdown struct {
	State
	StartedAt          time.Time
	WarningMinuteCount int
	LastReminderMinute int
	IsDrainPending     bool
}

type announcement struct {
	mu                sync.Mutex
	operationMu       sync.Mutex
	broadcast         Broadcast
	state             countdown
	statePath         string
	drainObserver     DrainObserver
	areUpdatesEnabled bool
	changed           chan struct{}
}

func (e *Control) Status() State { return e.announcement.status().State }

func (e *Control) ConfigureDrain(observer DrainObserver) {
	e.announcement.mu.Lock()
	e.announcement.drainObserver = observer
	e.announcement.mu.Unlock()
	e.announcement.notifyChange()
}

func (e *Control) Request(ctx context.Context, req Request) (State, error) {
	if ctx == nil {
		return State{}, errors.New("nil maintenance context")
	}
	err := ctx.Err()
	if err != nil {
		return e.Status(), fmt.Errorf("requestContext: %w", err)
	}
	state, err := e.announcement.request(req)
	if err != nil {
		return state.State, fmt.Errorf("maintenanceRequest: %w", err)
	}
	return state.State, nil
}

func validateRequest(req Request) error {
	err := validateRequestFields(req)
	if err != nil {
		return fmt.Errorf("requestFields: %w", errors.Join(ErrInvalidRequest, err))
	}
	return nil
}

func validateRequestFields(req Request) error {
	if req.Action != "restart" && req.Action != "update" && req.Action != "hold" {
		return errors.New("maintenance action must be restart, update, or hold")
	}
	if req.WarningMinutes < 0 || req.WarningMinutes > 1440 {
		return errors.New("maintenance warning must be between 0 and 1440 minutes")
	}
	if !utf8.ValidString(req.Reason) || len(req.Reason) > 512 {
		return errors.New("maintenance reason must be valid UTF-8 within 512 bytes")
	}
	for _, character := range req.Reason {
		if unicode.IsControl(character) {
			return errors.New("maintenance reason contains control characters")
		}
	}
	if len(req.OperationID) > 128 {
		return errors.New("maintenance operation ID is too long")
	}
	for _, character := range req.OperationID {
		isAllowed := character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_'
		if !isAllowed {
			return errors.New("maintenance operation ID is invalid")
		}
	}
	return nil
}

// ValidateRequest checks input before an adapter accepts or journals an action.
func ValidateRequest(req Request) error {
	err := validateRequest(req)
	if err != nil {
		return fmt.Errorf("maintenanceValidate: %w", err)
	}
	return nil
}

func (e *announcement) request(req Request) (countdown, error) {
	err := validateRequest(req)
	if err != nil {
		return e.status(), fmt.Errorf("requestValidate: %w", err)
	}
	e.operationMu.Lock()
	defer e.operationMu.Unlock()
	state := e.status()
	e.mu.Lock()
	isEnabled := e.areUpdatesEnabled
	e.mu.Unlock()
	if req.Action != "hold" && !isEnabled {
		return state, ErrUnsupported
	}
	if state.IsActive {
		if state.Action == req.Action {
			return state, nil
		}
		return state, ErrConflict
	}
	id := req.OperationID
	if id == "" {
		id, err = randomToken()
		if err != nil {
			return state, fmt.Errorf("requestID: %w", err)
		}
	}
	now := time.Now().UTC()
	state = countdown{State: State{ID: id, Action: req.Action, Reason: req.Reason, Phase: "preparing", Deadline: now.Add(time.Duration(req.WarningMinutes) * time.Minute), IsActive: true, IsAdmissionBlocked: true}, StartedAt: now, WarningMinuteCount: req.WarningMinutes, LastReminderMinute: req.WarningMinutes + 1}
	err = e.commit(state)
	if err != nil {
		return e.status(), fmt.Errorf("requestSave: %w", err)
	}
	return state, nil
}

func (e *Control) Activate(id string) error {
	a := &e.announcement
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	state := a.status()
	if id == "" || state.ID != id || !state.IsActive {
		return ErrConflict
	}
	if state.Phase != "preparing" {
		return nil
	}
	state.Phase = "warning"
	if state.Action == "hold" {
		state.Phase = "draining"
	}
	err := a.commit(state)
	if err != nil {
		return fmt.Errorf("activateSave: %w", err)
	}
	return nil
}

// Abort discards only a request whose acceptance response was not flushed.
func (e *Control) Abort(id string) error {
	a := &e.announcement
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	state := a.status()
	if state.ID != id || state.Phase != "preparing" {
		return nil
	}
	state.Phase, state.IsActive, state.IsAdmissionBlocked = "cancelled", false, false
	err := a.commit(state)
	if err != nil {
		return fmt.Errorf("abortSave: %w", err)
	}
	return nil
}

func (e *Control) Cancel(ctx context.Context, id string) (State, error) {
	state, err := e.announcement.cancel(ctx, id)
	if err != nil {
		return state.State, fmt.Errorf("maintenanceCancel: %w", err)
	}
	return state.State, nil
}

func (e *announcement) cancel(ctx context.Context, id string) (countdown, error) {
	if ctx == nil {
		return e.status(), errors.New("nil maintenance context")
	}
	e.operationMu.Lock()
	defer e.operationMu.Unlock()
	state := e.status()
	err := ctx.Err()
	if err != nil {
		return state, fmt.Errorf("cancelContext: %w", err)
	}
	if state.Phase == "stopping" {
		return state, ErrStopping
	}
	if state.Action == "hold" && state.Phase == "draining" && !time.Now().Before(state.Deadline) {
		// Deadline cleanup may temporarily release operationMu while existing
		// gameplay owners disconnect. Reopen only after it durably reaches held.
		return state, ErrStopping
	}
	if id == "" || id != state.ID {
		return state, ErrConflict
	}
	if !state.IsActive {
		return state, nil
	}
	state.Phase, state.IsActive, state.IsAdmissionBlocked = "cancelled", false, false
	err = e.commit(state)
	if err != nil {
		return e.status(), fmt.Errorf("cancelSave: %w", err)
	}
	delivered, broadcastErr := e.broadcast(ctx, "Server maintenance cancelled. You can continue playing.")
	state.Delivered = delivered
	err = e.commit(state)
	if err != nil {
		return e.status(), fmt.Errorf("cancelDeliverySave: %w", errors.Join(err, broadcastErr))
	}
	if broadcastErr != nil {
		return state, fmt.Errorf("cancelBroadcast: %w", broadcastErr)
	}
	return state, nil
}

// Resolve accepts matching supervisor outcomes; failed recovery remains closed
// but can be explicitly cancelled. Readiness remains a separate observation.
func (e *Control) Resolve(id, phase string) error {
	a := &e.announcement
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	state := a.status()
	if state.ID != id || id == "" {
		return ErrConflict
	}
	if state.Phase == "completed" && (phase == "healthy" || phase == "rolled_back") {
		return nil
	}
	if state.Phase == "failed" && phase == "failed" {
		return nil
	}
	if state.Phase != "stopping" && state.Phase != "failed" {
		return ErrConflict
	}
	switch phase {
	case "healthy", "rolled_back":
		state.Phase, state.IsActive, state.IsAdmissionBlocked = "completed", false, false
	case "failed":
		state.Phase, state.IsActive, state.IsAdmissionBlocked = "failed", true, true
	default:
		return errors.New("maintenance resolution must be healthy, rolled_back, or failed")
	}
	err := a.commit(state)
	if err != nil {
		return fmt.Errorf("resolveSave: %w", err)
	}
	return nil
}

func (e *Control) Announce(ctx context.Context, minutes int, operationID string) (State, error) {
	state, err := e.announcement.announce(ctx, minutes, operationID)
	if err != nil {
		return state.State, fmt.Errorf("maintenanceAnnounce: %w", err)
	}
	return state.State, nil
}

func (e *announcement) announce(ctx context.Context, minutes int, operationID string) (countdown, error) {
	if ctx == nil || minutes < 1 || minutes > 1440 {
		return e.status(), errors.New("announcement minutes must be between 1 and 1440")
	}
	e.operationMu.Lock()
	defer e.operationMu.Unlock()
	state := e.status()
	if state.IsActive {
		if state.Action == "announce" {
			if state.Phase == "failed" {
				return state, errors.New("announcement delivery failed; cancel before starting another warning")
			}
			return state, nil
		}
		return state, ErrConflict
	}
	id := operationID
	var err error
	if id == "" {
		id, err = randomToken()
		if err != nil {
			return state, fmt.Errorf("announceID: %w", err)
		}
	}
	now := time.Now().UTC()
	state = countdown{State: State{ID: id, Action: "announce", Phase: "warning", Deadline: now.Add(time.Duration(minutes) * time.Minute), IsActive: true}, StartedAt: now, WarningMinuteCount: minutes}
	err = e.commit(state)
	if err != nil {
		return e.status(), fmt.Errorf("announceSave: %w", err)
	}
	delivered, broadcastErr := e.broadcast(ctx, fmt.Sprintf("Server maintenance warning: restarting in %d minutes. Please finish your game and log out before then.", minutes))
	state.Delivered = delivered
	if broadcastErr != nil {
		state.Phase = "failed"
	}
	err = e.commit(state)
	if err != nil {
		return e.status(), fmt.Errorf("announceDeliverySave: %w", errors.Join(err, broadcastErr))
	}
	if broadcastErr != nil {
		return state, fmt.Errorf("announceBroadcast: %w", broadcastErr)
	}
	return state, nil
}

func (e *announcement) status() countdown { e.mu.Lock(); defer e.mu.Unlock(); return e.state }

// commit runs with operationMu held; durability precedes memory acceptance.
func (e *announcement) commit(state countdown) error {
	err := saveState(e.statePath, state)
	if err != nil {
		return fmt.Errorf("statePersist: %w", err)
	}
	e.mu.Lock()
	e.state = state
	e.mu.Unlock()
	e.notifyChange()
	return nil
}

func (e *announcement) notifyChange() {
	select {
	case e.changed <- struct{}{}:
	default:
	}
}

func randomToken() (string, error) {
	var tokenBytes [32]byte
	byteCount, err := rand.Read(tokenBytes[:])
	if err != nil {
		return "", fmt.Errorf("tokenRead: %w", err)
	}
	if byteCount != len(tokenBytes) {
		return "", errors.New("incomplete maintenance token randomness")
	}
	return hex.EncodeToString(tokenBytes[:]), nil
}
