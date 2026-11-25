package savebuffer

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

func (e *Repository) LoadByID(ctx context.Context, userID int64) (sporenet.UserRecord, error) {
	err := checkContext(ctx)
	if err != nil {
		return sporenet.UserRecord{}, fmt.Errorf("idContext: %w", err)
	}
	cached, isCached := e.cachedByID(userID)
	if isCached {
		return cached, nil
	}
	err = e.acquire(ctx)
	if err != nil {
		return sporenet.UserRecord{}, fmt.Errorf("idGate: %w", err)
	}
	defer e.release()
	cached, isCached = e.cachedByID(userID)
	if isCached {
		return cached, nil
	}
	record, err := e.store.LoadByID(ctx, userID)
	if err != nil {
		return sporenet.UserRecord{}, fmt.Errorf("idLoad: %w", err)
	}
	return e.overlay(record), nil
}

func (e *Repository) LoadByLoginName(ctx context.Context, loginName string) (sporenet.UserRecord, error) {
	err := checkContext(ctx)
	if err != nil {
		return sporenet.UserRecord{}, fmt.Errorf("loginContext: %w", err)
	}
	cached, isCached := e.cachedByLogin(loginName)
	if isCached {
		return cached, nil
	}
	err = e.acquire(ctx)
	if err != nil {
		return sporenet.UserRecord{}, fmt.Errorf("loginGate: %w", err)
	}
	defer e.release()
	cached, isCached = e.cachedByLogin(loginName)
	if isCached {
		return cached, nil
	}
	record, err := e.store.LoadByLoginName(ctx, loginName)
	if err != nil {
		return sporenet.UserRecord{}, fmt.Errorf("loginLoad: %w", err)
	}
	return e.overlay(record), nil
}

func (e *Repository) cachedByID(userID int64) (sporenet.UserRecord, bool) {
	e.mutex.Lock()
	pending := e.snapshots[userID]
	e.mutex.Unlock()
	return pending.profile()
}

func (e *Repository) cachedByLogin(loginName string) (sporenet.UserRecord, bool) {
	e.mutex.Lock()
	var pending snapshot
	for _, current := range e.snapshots {
		if current.record != nil && strings.EqualFold(current.record.LoginName, loginName) {
			pending = current
			break
		}
	}
	e.mutex.Unlock()
	return pending.profile()
}

func (e snapshot) profile() (sporenet.UserRecord, bool) {
	if e.record == nil {
		return sporenet.UserRecord{}, false
	}
	record := e.record.Clone()
	record.Stats = e.statSnapshot
	return record, true
}

// Display-name lookup retains ambiguity checks, including pending profiles.
func (e *Repository) LoadByDisplayName(ctx context.Context, displayName string) (sporenet.UserRecord, error) {
	err := e.acquire(ctx)
	if err != nil {
		return sporenet.UserRecord{}, fmt.Errorf("displayGate: %w", err)
	}
	defer e.release()
	record, err := e.store.LoadByDisplayName(ctx, displayName)
	if err != nil && !errors.Is(err, sporenet.ErrUserNotFound) {
		return sporenet.UserRecord{}, fmt.Errorf("displayLoad: %w", err)
	}
	if err == nil {
		record = e.overlay(record)
		if !strings.EqualFold(record.DisplayName, displayName) {
			record = sporenet.UserRecord{}
		}
	}
	e.mutex.Lock()
	for userID, pending := range e.snapshots {
		if pending.record == nil || !strings.EqualFold(pending.record.DisplayName, displayName) {
			continue
		}
		if record.Account.ID != 0 && record.Account.ID != userID {
			e.mutex.Unlock()
			return sporenet.UserRecord{}, sporenet.ErrUserAmbiguous
		}
		record = *pending.record
		record.Stats = pending.statSnapshot
	}
	e.mutex.Unlock()
	if record.Account.ID == 0 {
		return sporenet.UserRecord{}, sporenet.ErrUserNotFound
	}
	return record.Clone(), nil
}

// Readers use the storage gate so a completed flush cannot retire the overlay
// between reading an older database row and applying its pending changes.
func (e *Repository) overlay(record sporenet.UserRecord) sporenet.UserRecord {
	e.mutex.Lock()
	pending, isFound := e.snapshots[record.Account.ID]
	e.mutex.Unlock()
	if !isFound {
		return record
	}
	if pending.record != nil {
		record = pending.record.Clone()
	}
	record.Stats = pending.statSnapshot
	return record
}

func (e *Repository) PendingTutorialCompletionLoginNames(ctx context.Context) ([]string, error) {
	err := e.acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("tutorialGate: %w", err)
	}
	defer e.release()
	loginNames, err := e.store.PendingTutorialCompletionLoginNames(ctx)
	if err != nil {
		return nil, fmt.Errorf("tutorialLoad: %w", err)
	}
	loginNamesByKey := make(map[string]string, len(loginNames))
	for _, loginName := range loginNames {
		loginNamesByKey[strings.ToLower(loginName)] = loginName
	}
	e.mutex.Lock()
	for _, pending := range e.snapshots {
		if pending.record == nil {
			continue
		}
		key := strings.ToLower(pending.record.LoginName)
		if pending.record.IsTutorialCompletionPending {
			loginNamesByKey[key] = pending.record.LoginName
		} else {
			delete(loginNamesByKey, key)
		}
	}
	e.mutex.Unlock()
	loginNames = make([]string, 0, len(loginNamesByKey))
	for _, loginName := range loginNamesByKey {
		loginNames = append(loginNames, loginName)
	}
	sort.Strings(loginNames)
	return loginNames, nil
}

// Account creation/deletion are infrequent identity operations. Keep them
// synchronous and serialize them with the worker to prevent stale resurrection.
func (e *Repository) Create(ctx context.Context, record sporenet.UserRecord) (int64, error) {
	err := e.acquire(ctx)
	if err != nil {
		return 0, fmt.Errorf("createGate: %w", err)
	}
	defer e.release()
	userID, err := e.store.Create(ctx, record)
	if err != nil {
		return 0, fmt.Errorf("createStore: %w", err)
	}
	return userID, nil
}

// CreateRegistered preserves the registration boundary through the synchronous gate.
func (e *Repository) CreateRegistered(ctx context.Context, record sporenet.UserRecord) (int64, error) {
	err := e.acquire(ctx)
	if err != nil {
		return 0, fmt.Errorf("registrationGate: %w", err)
	}
	defer e.release()
	registrationRepository, isRegistrationAvailable := e.store.(sporenet.UserRegistrationRepository)
	if !isRegistrationAvailable {
		userID, createErr := e.store.Create(ctx, record)
		if createErr != nil {
			return 0, fmt.Errorf("registrationLegacy: %w", createErr)
		}
		return userID, nil
	}
	userID, err := registrationRepository.CreateRegistered(ctx, record)
	if err != nil {
		return 0, fmt.Errorf("registrationStore: %w", err)
	}
	return userID, nil
}

func (e *Repository) DeleteByLoginName(ctx context.Context, loginName string) error {
	err := e.acquire(ctx)
	if err != nil {
		return fmt.Errorf("deleteGate: %w", err)
	}
	defer e.release()
	record, err := e.store.LoadByLoginName(ctx, loginName)
	if err != nil {
		return fmt.Errorf("deleteIdentity: %w", err)
	}
	// Preserve the latest accepted logout snapshot before archiving the row.
	// The storage gate prevents the worker from racing this final write.
	isSaved, err := e.save(ctx, record.Account.ID, true)
	if err != nil {
		return fmt.Errorf("deleteFlush: %w", err)
	}
	if isSaved && e.logger != nil {
		e.logger.Printf("Profile save flushed before archival user=%d", record.Account.ID)
	}
	err = e.store.DeleteByLoginName(ctx, loginName)
	if err != nil {
		return fmt.Errorf("deleteStore: %w", err)
	}
	e.mutex.Lock()
	delete(e.snapshots, record.Account.ID)
	e.mutex.Unlock()
	return nil
}

func (e *Repository) RestoreDeletedProfile(ctx context.Context, userID int64, loginName string) (string, error) {
	repository, isSupported := e.store.(sporenet.UserRecoveryRepository)
	if !isSupported {
		return "", errors.New("recovery repository unavailable")
	}
	err := e.acquire(ctx)
	if err != nil {
		return "", fmt.Errorf("restoreGate: %w", err)
	}
	defer e.release()
	restoredLoginName, err := repository.RestoreDeletedProfile(ctx, userID, loginName)
	if err != nil {
		return "", fmt.Errorf("restoreStore: %w", err)
	}
	e.mutex.Lock()
	delete(e.snapshots, userID)
	e.mutex.Unlock()
	return restoredLoginName, nil
}

func (e *Repository) PurgeDeletedProfiles(ctx context.Context, cutoff time.Time) (int64, error) {
	repository, isSupported := e.store.(sporenet.UserPurgeRepository)
	if !isSupported {
		return 0, errors.New("purge repository unavailable")
	}
	err := e.acquire(ctx)
	if err != nil {
		return 0, fmt.Errorf("purgeGate: %w", err)
	}
	defer e.release()
	purgedCount, err := repository.PurgeDeletedProfiles(ctx, cutoff)
	if err != nil {
		return 0, fmt.Errorf("purgeStore: %w", err)
	}
	return purgedCount, nil
}

func (e *Repository) LatestProfileDeletion(ctx context.Context, loginName string) (time.Time, error) {
	repository, isSupported := e.store.(sporenet.UserDeletionHistoryRepository)
	if !isSupported {
		return time.Time{}, errors.New("deletion history repository unavailable")
	}
	err := e.acquire(ctx)
	if err != nil {
		return time.Time{}, fmt.Errorf("historyGate: %w", err)
	}
	defer e.release()
	deletedAt, err := repository.LatestProfileDeletion(ctx, loginName)
	if err != nil {
		return time.Time{}, fmt.Errorf("historyStore: %w", err)
	}
	return deletedAt, nil
}
