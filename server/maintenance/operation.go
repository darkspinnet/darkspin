// Package maintenance owns local maintenance announcements.
package maintenance

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Broadcast sends through the server's existing player-notification adapter.
// A nil error acknowledges completed delivery attempts; zero online is valid.
type Broadcast func(context.Context, string) (int, error)

// countdown identifies one countdown. Repeated announcements retain its deadline.
type countdown struct {
	ID        string
	Deadline  time.Time
	Delivered int
	IsActive  bool
}

type announcement struct {
	mu                    sync.Mutex
	operationMu           sync.Mutex
	broadcast             Broadcast
	state                 countdown
	scheduledID           string
	isAccepted            bool
	isCancellationPending bool
}

func (e *announcement) announce(ctx context.Context, minutes int) (countdown, error) {
	if minutes < 1 || minutes > 1440 {
		return countdown{}, errors.New("minutes must be between 1 and 1440")
	}
	e.operationMu.Lock()
	defer e.operationMu.Unlock()
	err := ctx.Err()
	if err != nil {
		return e.state, fmt.Errorf("announceContext: %w", err)
	}
	if e.state.IsActive {
		if !e.state.Deadline.After(time.Now()) {
			return e.state, errors.New("maintenance countdown expired; cancel before starting another countdown")
		}
		if !e.isAccepted {
			return e.state, errors.New("announcement delivery failed; cancel before starting another countdown")
		}
		return e.state, nil
	}
	e.isAccepted = false
	e.isCancellationPending = false
	id, err := randomToken()
	if err != nil {
		return countdown{}, fmt.Errorf("countdownID: %w", err)
	}
	message := fmt.Sprintf("Server update: restarting in %d minutes. Please finish your game and log out before then.", minutes)
	delivered, err := e.broadcast(ctx, message)
	deadline := time.Now().UTC().Add(time.Duration(minutes) * time.Minute)
	// Retain partial announcements so cancel can notify affected players.
	e.mu.Lock()
	e.state = countdown{ID: id, Deadline: deadline, Delivered: delivered, IsActive: true}
	e.mu.Unlock()
	if err != nil {
		return e.state, fmt.Errorf("announceBroadcast: %w", err)
	}
	e.isAccepted = true
	return e.state, nil
}

func (e *announcement) cancel(ctx context.Context, id string) (countdown, error) {
	e.operationMu.Lock()
	defer e.operationMu.Unlock()
	err := ctx.Err()
	if err != nil {
		return e.state, fmt.Errorf("cancelContext: %w", err)
	}
	if id != "" && id != e.state.ID {
		return e.state, errors.New("maintenance countdown ID does not match")
	}
	if !e.state.IsActive && !e.isCancellationPending {
		return e.state, nil
	}
	e.mu.Lock()
	e.state.IsActive = false
	e.isAccepted = false
	e.mu.Unlock()
	delivered, err := e.broadcast(ctx, "Server update cancelled. You can continue playing; the announced restart will not happen.")
	e.mu.Lock()
	e.state.Delivered = delivered
	e.mu.Unlock()
	e.isCancellationPending = err != nil
	if err != nil {
		return e.state, fmt.Errorf("cancelBroadcast: %w", err)
	}
	return e.state, nil
}

func (e *announcement) status() countdown {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.state
}

func randomToken() (string, error) {
	var tokenBytes [32]byte
	_, err := rand.Read(tokenBytes[:])
	if err != nil {
		return "", fmt.Errorf("tokenRead: %w", err)
	}
	return hex.EncodeToString(tokenBytes[:]), nil
}
