package sporenet

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

type RenameAccountRequest struct {
	UserID          int64
	Revision        int64
	CurrentPassword string
	DisplayName     string
}

type ChangeAccountPasswordRequest struct {
	UserID          int64
	Revision        int64
	CurrentPassword string
	NewPassword     string
}

type DeleteAccountRequest struct {
	UserID          int64
	Revision        int64
	CurrentPassword string
}

// AccountCredential is private credential data, never a player projection.
type AccountCredential struct {
	UserID    int64
	LoginName string
	Password  string
}

type AccountCredentialMatch struct {
	UserID           int64
	Revision         int64
	ExpectedPassword string
}

type AccountRename struct {
	Match       AccountCredentialMatch
	DisplayName string
}

type AccountPasswordChange struct {
	Match    AccountCredentialMatch
	Password string
}

// AccountControlRepository atomically verifies credentials and current access
// within each mutation. Password fields contain storage representations only.
type AccountControlRepository interface {
	LoadAccountCredential(context.Context, int64) (AccountCredential, error)
	RenameAccount(context.Context, AccountRename) error
	ChangeAccountPassword(context.Context, AccountPasswordChange) error
	DeleteAccount(context.Context, AccountCredentialMatch) error
}

func (e *UserManager) RenameAccount(ctx context.Context, req RenameAccountRequest) (ProfileSummary, error) {
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	err := ValidateDesktopUsername(req.DisplayName)
	if err != nil {
		return ProfileSummary{}, fmt.Errorf("renameName: %w", err)
	}
	repository, isSupported := e.repository.(AccountControlRepository)
	if !isSupported {
		return ProfileSummary{}, errors.New("account control unavailable")
	}
	session, credential, err := e.lockAccountControl(ctx, repository, req.UserID, req.Revision, req.CurrentPassword)
	if err != nil {
		return ProfileSummary{}, fmt.Errorf("renameAuthorize: %w", err)
	}
	defer session.Unlock()
	summary, err := e.ProfileSummary(ctx, req.UserID)
	if err != nil {
		return ProfileSummary{}, fmt.Errorf("renameSummary: %w", err)
	}
	err = repository.RenameAccount(ctx, AccountRename{
		Match:       AccountCredentialMatch{UserID: req.UserID, Revision: req.Revision, ExpectedPassword: credential.Password},
		DisplayName: req.DisplayName,
	})
	if err != nil {
		return ProfileSummary{}, fmt.Errorf("renameSave: %w", err)
	}
	e.refreshAssociationDisplayName(req.UserID, req.DisplayName)
	summary.Identity.DisplayName = req.DisplayName
	return summary, nil
}

// The committed identity is authoritative. Refresh current social projections;
// ordinary profile saves resolve persisted names from that durable identity.
func (e *UserManager) refreshAssociationDisplayName(memberID int64, displayName string) {
	users := e.Users()
	for _, user := range users {
		user.mutation.Lock()
		user.mu.Lock()
		for listType, members := range user.Associations {
			for index := range members {
				if members[index].ID == memberID {
					members[index].Name = displayName
				}
			}
			user.Associations[listType] = members
		}
		user.mu.Unlock()
		user.mutation.Unlock()
	}
}

func (e *UserManager) ChangeAccountPassword(ctx context.Context, req ChangeAccountPasswordRequest) error {
	err := ValidateDesktopPassword(req.NewPassword, true)
	if err != nil {
		return fmt.Errorf("passwordValidate: %w", err)
	}
	repository, isSupported := e.repository.(AccountControlRepository)
	if !isSupported {
		return errors.New("account control unavailable")
	}
	session, credential, err := e.lockAccountControl(ctx, repository, req.UserID, req.Revision, req.CurrentPassword)
	if err != nil {
		return fmt.Errorf("passwordAuthorize: %w", err)
	}
	defer session.Unlock()
	password, err := passwordForStorage(req.NewPassword)
	if err != nil {
		return fmt.Errorf("passwordHash: %w", err)
	}
	err = repository.ChangeAccountPassword(ctx, AccountPasswordChange{
		Match:    AccountCredentialMatch{UserID: req.UserID, Revision: req.Revision, ExpectedPassword: credential.Password},
		Password: password,
	})
	if err != nil {
		return fmt.Errorf("passwordSave: %w", err)
	}
	return nil
}

func (e *UserManager) DeleteAccount(ctx context.Context, req DeleteAccountRequest) error {
	repository, isSupported := e.repository.(AccountControlRepository)
	if !isSupported {
		return errors.New("account control unavailable")
	}
	session, credential, err := e.lockAccountControl(ctx, repository, req.UserID, req.Revision, req.CurrentPassword)
	if err != nil {
		return fmt.Errorf("deleteAuthorize: %w", err)
	}
	defer session.Unlock()
	err = repository.DeleteAccount(ctx, AccountCredentialMatch{
		UserID: req.UserID, Revision: req.Revision, ExpectedPassword: credential.Password,
	})
	if err != nil {
		return fmt.Errorf("deleteSave: %w", err)
	}
	return nil
}

// lockAccountControl owns the same identity lock as login/logout. The caller
// releases the returned lock after the durable operation completes.
func (e *UserManager) lockAccountControl(
	ctx context.Context, repository AccountControlRepository, userID, revision int64, currentPassword string,
) (*sync.Mutex, AccountCredential, error) {
	if currentPassword == "" {
		return nil, AccountCredential{}, fmt.Errorf("controlConfirmation: %w", ErrPasswordRequired)
	}
	if ctx == nil || userID <= 0 || revision <= 0 || len(currentPassword) > 1024 {
		return nil, AccountCredential{}, errors.New("invalid account confirmation")
	}
	credential, err := repository.LoadAccountCredential(ctx, userID)
	if err != nil {
		return nil, AccountCredential{}, fmt.Errorf("controlIdentity: %w", err)
	}
	session := e.sessionLock(credential.LoginName)
	session.Lock()
	credential, err = repository.LoadAccountCredential(ctx, userID)
	if err != nil {
		session.Unlock()
		return nil, AccountCredential{}, fmt.Errorf("controlReload: %w", err)
	}
	if e.UserByID(userID) != nil {
		session.Unlock()
		return nil, AccountCredential{}, fmt.Errorf("controlActive: %w", ErrUserActive)
	}
	if credential.UserID != userID || !isPasswordMatch(credential.Password, currentPassword) {
		session.Unlock()
		return nil, AccountCredential{}, fmt.Errorf("controlPassword: %w", ErrPasswordIncorrect)
	}
	err = e.checkAdmission(ctx, userID)
	if err != nil {
		session.Unlock()
		return nil, AccountCredential{}, fmt.Errorf("controlAccess: %w", err)
	}
	return session, credential, nil
}
