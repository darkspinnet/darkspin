package savebuffer

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

func (e *Repository) LoadAccountCredential(ctx context.Context, userID int64) (sporenet.AccountCredential, error) {
	repository, isSupported := e.store.(sporenet.AccountControlRepository)
	if !isSupported {
		return sporenet.AccountCredential{}, errors.New("account control unavailable")
	}
	err := e.acquire(ctx)
	if err != nil {
		return sporenet.AccountCredential{}, fmt.Errorf("credentialGate: %w", err)
	}
	defer e.release()
	credential, err := repository.LoadAccountCredential(ctx, userID)
	if err != nil {
		return sporenet.AccountCredential{}, fmt.Errorf("credentialRead: %w", err)
	}
	return credential, nil
}

func (e *Repository) RenameAccount(ctx context.Context, req sporenet.AccountRename) error {
	repository, err := e.prepareAccountControl(ctx, req.Match.UserID)
	if err != nil {
		return fmt.Errorf("renamePrepare: %w", err)
	}
	defer e.release()
	err = repository.RenameAccount(ctx, req)
	if err != nil {
		return fmt.Errorf("renameStore: %w", err)
	}
	e.mutex.Lock()
	for userID, pending := range e.snapshots {
		if pending.record == nil {
			continue
		}
		if userID == req.Match.UserID {
			// save() retired the exact snapshot flushed before this operation.
			// Preserve a newer accepted progression snapshot that arrived during
			// I/O, while replacing its obsolete identity scalar.
			record := *pending.record
			record.DisplayName = req.DisplayName
			pending.record = &record
			e.snapshots[userID] = pending
		}
		isReferenced := false
		for _, members := range pending.record.Associations {
			for _, member := range members {
				if member.ID == req.Match.UserID {
					isReferenced = true
				}
			}
		}
		if !isReferenced {
			continue
		}
		record := pending.record.Clone()
		for listType, members := range record.Associations {
			for index := range members {
				if members[index].ID == req.Match.UserID {
					members[index].Name = req.DisplayName
				}
			}
			record.Associations[listType] = members
		}
		pending.record = &record
		e.snapshots[userID] = pending
	}
	e.mutex.Unlock()
	return nil
}

func (e *Repository) ChangeAccountPassword(ctx context.Context, req sporenet.AccountPasswordChange) error {
	repository, err := e.prepareAccountControl(ctx, req.Match.UserID)
	if err != nil {
		return fmt.Errorf("passwordPrepare: %w", err)
	}
	defer e.release()
	err = repository.ChangeAccountPassword(ctx, req)
	if err != nil {
		return fmt.Errorf("passwordStore: %w", err)
	}
	e.mutex.Lock()
	pending, isPending := e.snapshots[req.Match.UserID]
	if isPending && pending.record != nil {
		// Retain concurrent accepted gameplay changes without retaining the
		// previous credential in the read overlay.
		record := *pending.record
		record.Password = req.Password
		pending.record = &record
		e.snapshots[req.Match.UserID] = pending
	}
	e.mutex.Unlock()
	return nil
}

func (e *Repository) DeleteAccount(ctx context.Context, req sporenet.AccountCredentialMatch) error {
	repository, err := e.prepareAccountControl(ctx, req.UserID)
	if err != nil {
		return fmt.Errorf("deletePrepare: %w", err)
	}
	defer e.release()
	err = repository.DeleteAccount(ctx, req)
	if err != nil {
		return fmt.Errorf("deleteStore: %w", err)
	}
	e.mutex.Lock()
	delete(e.snapshots, req.UserID)
	e.mutex.Unlock()
	return nil
}

// prepareAccountControl flushes the last accepted gameplay state while holding
// the storage gate. The caller releases that gate after its account transaction.
func (e *Repository) prepareAccountControl(ctx context.Context, userID int64) (sporenet.AccountControlRepository, error) {
	repository, isSupported := e.store.(sporenet.AccountControlRepository)
	if !isSupported {
		return nil, errors.New("account control unavailable")
	}
	err := e.acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("controlGate: %w", err)
	}
	isSaved, err := e.save(ctx, userID, true)
	if err != nil {
		e.release()
		return nil, fmt.Errorf("controlFlush: %w", err)
	}
	if isSaved && e.logger != nil {
		e.logger.Printf("Profile save flushed before account operation user=%d", userID)
	}
	return repository, nil
}
