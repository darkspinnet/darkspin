package sporenet

import (
	"context"
	"errors"
	"fmt"
)

const MaximumDesktopUsernameLength = 20

var (
	ErrUsernameRequired  = errors.New("Username is required.")
	ErrUsernamePattern   = errors.New("Username can only contain letters and numbers.")
	ErrUsernameTooLong   = errors.New("Username is too long (maximum 20 characters).")
	ErrPasswordRequired  = errors.New("Password is required.")
	ErrPasswordTooLong   = errors.New("Password is too long (maximum 72 UTF-8 bytes).")
	ErrPasswordIncorrect = errors.New("incorrect password")
)

// ValidateDesktopUsername checks names for newly created desktop accounts.
// Existing credentials retain their legacy login-name compatibility.
func ValidateDesktopUsername(username string) error {
	if username == "" {
		return ErrUsernameRequired
	}
	for _, character := range username {
		isLetter := character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
		isDigit := character >= '0' && character <= '9'
		if !isLetter && !isDigit {
			return ErrUsernamePattern
		}
	}
	if len(username) > MaximumDesktopUsernameLength {
		return ErrUsernameTooLong
	}
	return nil
}

// ValidateDesktopPassword exposes bcrypt's actual storage limit. Loopback-only
// local account creation may omit a password; remote requests must supply one.
func ValidateDesktopPassword(password string, isRequired bool) error {
	if isRequired && password == "" {
		return ErrPasswordRequired
	}
	if len(password) > 72 {
		return ErrPasswordTooLong
	}
	return nil
}

// AdmissionCredentialPolicy binds grants and game logins to durable credentials.
type AdmissionCredentialPolicy interface {
	CredentialRevision(context.Context, int64) (int64, error)
	ValidateCredential(context.Context, int64, int64) error
}

type credentialAdmissionKey struct{}

type credentialAdmission struct {
	userID   int64
	revision int64
}

func (e *UserManager) CredentialRevision(ctx context.Context, userID int64) (int64, error) {
	if ctx == nil || userID <= 0 {
		return 0, errors.New("invalid credential revision request")
	}
	e.mu.RLock()
	policy := e.admissionPolicy
	e.mu.RUnlock()
	if policy == nil {
		return 1, nil
	}
	credentialPolicy, isSupported := policy.(AdmissionCredentialPolicy)
	if !isSupported {
		return 0, errors.New("credential revision policy unavailable")
	}
	revision, err := credentialPolicy.CredentialRevision(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("credentialRevision: %w", err)
	}
	if revision <= 0 {
		return 0, errors.New("invalid durable credential revision")
	}
	return revision, nil
}

func (e *UserManager) AuthorizeCredential(ctx context.Context, userID, revision int64) error {
	if ctx == nil || userID <= 0 || revision < 0 {
		return errors.New("invalid credential authorization")
	}
	e.mu.RLock()
	policy := e.admissionPolicy
	e.mu.RUnlock()
	err := validateCredentialPolicy(ctx, policy, userID, revision)
	if err != nil {
		return fmt.Errorf("credentialAuthorize: %w", err)
	}
	return nil
}

func validateCredentialPolicy(ctx context.Context, policy AdmissionPolicy, userID, revision int64) error {
	if policy == nil && (revision == 0 || revision == 1) {
		return nil
	}
	credentialPolicy, isSupported := policy.(AdmissionCredentialPolicy)
	if !isSupported {
		return errors.New("credential authorization policy unavailable")
	}
	err := credentialPolicy.ValidateCredential(ctx, userID, revision)
	if err != nil {
		return fmt.Errorf("credentialValidate: %w", err)
	}
	return nil
}

func validateAdmissionCredential(ctx context.Context, policy AdmissionPolicy, userID int64) error {
	if ctx == nil {
		return errors.New("nil admission context")
	}
	credential, isBound := ctx.Value(credentialAdmissionKey{}).(credentialAdmission)
	if !isBound {
		return nil
	}
	if credential.userID != userID {
		return errors.New("credential account binding changed")
	}
	err := validateCredentialPolicy(ctx, policy, userID, credential.revision)
	if err != nil {
		return fmt.Errorf("admissionRevision: %w", err)
	}
	return nil
}

// AuthenticateCredential prevents a concurrent password operation from changing
// the revision between password verification and launch/panel grant creation.
func (e *UserManager) AuthenticateCredential(ctx context.Context, loginName, password string) (UserIdentity, int64, error) {
	session := e.sessionLock(loginName)
	session.Lock()
	defer session.Unlock()
	identity, err := e.Authenticate(ctx, loginName, password)
	if err != nil {
		return UserIdentity{}, 0, fmt.Errorf("credentialAuthenticate: %w", err)
	}
	revision, err := e.CredentialRevision(ctx, identity.ID)
	if err != nil {
		return UserIdentity{}, 0, fmt.Errorf("credentialCapture: %w", err)
	}
	return identity, revision, nil
}

// LoginCredential checks the revision inside the existing login identity lock
// via its admission boundary, including the already-active account branch.
func (e *UserManager) LoginCredential(ctx context.Context, loginName string, accountID, notBefore, revision int64) LoginResult {
	if ctx == nil || accountID < 0 || revision < 0 {
		return LoginResult{}
	}
	boundAccountID := accountID
	if boundAccountID == 0 {
		var err error
		boundAccountID, err = e.FindUserID(ctx, loginName)
		if err != nil {
			return LoginResult{}
		}
	}
	ctx = context.WithValue(ctx, credentialAdmissionKey{}, credentialAdmission{userID: boundAccountID, revision: revision})
	return e.LoginVerified(ctx, loginName, accountID, notBefore)
}
