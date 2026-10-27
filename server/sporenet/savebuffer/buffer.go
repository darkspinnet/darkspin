// Package savebuffer keeps the latest accepted profile snapshot in memory and
// serializes durable writes outside gameplay. Acceptance is not disk durability:
// a process failure can lose recent changes, including a recent battle's rewards.
package savebuffer

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

const (
	maximumPlayers = 4096
	maximumDelay   = 5 * time.Minute
	idleDelay      = 5 * time.Second
	writeTimeout   = 5 * time.Second
	retryDelay     = 5 * time.Second
)

// Store preserves each complete profile transaction and its reward receipts.
type Store interface {
	sporenet.UserRepository
	sporenet.UserProfileRepository
	sporenet.UserDeletionRepository
	sporenet.TutorialCompletionQueueRepository
	sporenet.PlayerStatsRepository
}

type snapshot struct {
	record          *sporenet.UserRecord
	statSnapshot    sporenet.PlayerStats
	revision        uint64
	profileRevision uint64
	firstDirty      time.Time
	lastChange      time.Time
	retryAfter      time.Time
	isReady         bool
}

// Repository bounds memory to one current snapshot per dirty player and one
// in-flight write. Neither Save method acquires the storage gate. Stored record
// pointers are immutable; focused stat updates replace only the scalar overlay.
type Repository struct {
	store     Store
	logger    *log.Logger
	mutex     sync.Mutex
	snapshots map[int64]snapshot
	revision  uint64
	isClosed  bool
	wake      chan struct{}
	done      chan struct{}
	gate      chan struct{}
	cancel    context.CancelFunc
	closeErr  error
}

func New(store Store, logger *log.Logger) *Repository {
	e := &Repository{
		store: store, logger: logger, snapshots: make(map[int64]snapshot),
		wake: make(chan struct{}, 1), done: make(chan struct{}),
		gate: make(chan struct{}, 1),
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	if logger != nil {
		logger.Printf("Profile save buffer enabled players=%d idle=%s maximum_delay=%s drain=%s",
			maximumPlayers, idleDelay, maximumDelay, writeTimeout)
	}
	go e.run(ctx)
	return e
}

// Save accepts a detached snapshot. Later saves replace it; a failed background
// write retains the latest version rather than restoring an obsolete snapshot.
func (e *Repository) Save(ctx context.Context, record sporenet.UserRecord) error {
	err := checkContext(ctx)
	if err != nil {
		return fmt.Errorf("saveContext: %w", err)
	}
	if record.Account.ID <= 0 || record.LoginName == "" {
		return sporenet.ErrInvalidUser
	}
	record = record.Clone()
	e.mutex.Lock()
	defer e.mutex.Unlock()
	pending, err := e.admit(record.Account.ID)
	if err != nil {
		return fmt.Errorf("saveAdmission: %w", err)
	}
	pending.record = &record
	pending.statSnapshot = record.Stats
	pending.profileRevision = pending.revision
	e.snapshots[record.Account.ID] = pending
	return nil
}

// SavePlayerStats updates the in-memory counters without copying an entire
// inventory on every hit. A pending full profile and its stat overlay share one
// revision stream, so neither can overwrite a newer accepted change.
func (e *Repository) SavePlayerStats(ctx context.Context, userID int64, statSnapshot sporenet.PlayerStats) error {
	err := checkContext(ctx)
	if err != nil {
		return fmt.Errorf("statContext: %w", err)
	}
	if userID <= 0 {
		return sporenet.ErrInvalidUser
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	pending, err := e.admit(userID)
	if err != nil {
		return fmt.Errorf("statAdmission: %w", err)
	}
	pending.statSnapshot = statSnapshot
	e.snapshots[userID] = pending
	return nil
}

// admit is called with mutex held and never performs I/O or waits for capacity.
func (e *Repository) admit(userID int64) (snapshot, error) {
	if e.isClosed {
		return snapshot{}, errors.New("save buffer closed")
	}
	pending, isFound := e.snapshots[userID]
	if !isFound && len(e.snapshots) >= maximumPlayers {
		return snapshot{}, errors.New("save buffer full")
	}
	now := time.Now()
	if !isFound {
		pending.firstDirty = now
	}
	e.revision++
	pending.revision = e.revision
	pending.lastChange = now
	return pending, nil
}

// RequestSave marks one player's current snapshot ready at a safe checkpoint.
// It only wakes the existing worker; callers never wait for persistence.
func (e *Repository) RequestSave(userID uint64) {
	e.mutex.Lock()
	pending, isFound := e.snapshots[int64(userID)]
	if isFound {
		pending.isReady = true
		e.snapshots[int64(userID)] = pending
	}
	e.mutex.Unlock()
	if isFound {
		select {
		case e.wake <- struct{}{}:
		default:
		}
	}
}

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return errors.New("nil save context")
	}
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("saveCancel: %w", err)
	}
	return nil
}

func (e *Repository) acquire(ctx context.Context) error {
	err := checkContext(ctx)
	if err != nil {
		return fmt.Errorf("gateContext: %w", err)
	}
	select {
	case e.gate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("gateWait: %w", ctx.Err())
	}
}

func (e *Repository) release() {
	<-e.gate
}
