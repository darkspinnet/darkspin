package sporenet

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	// ErrUserNotFound indicates that a repository has no matching durable user.
	ErrUserNotFound = errors.New("user not found")
	// ErrUserExists indicates that a repository uniqueness constraint failed.
	ErrUserExists = errors.New("user already exists")
	// ErrUserAmbiguous indicates that a public lookup matched multiple users.
	ErrUserAmbiguous = errors.New("user lookup ambiguous")
	// ErrUserActive indicates that a destructive profile operation targeted a
	// currently authenticated user.
	ErrUserActive = errors.New("user is active")
)

// UserProfileRepository resolves durable profiles by their public identity.
// It is an optional read port because authentication only requires login-name
// lookup. Transports consume the redacted UserView returned by UserManager,
// never these storage records directly.
type UserProfileRepository interface {
	LoadByID(context.Context, int64) (UserRecord, error)
	LoadByDisplayName(context.Context, string) (UserRecord, error)
}

// UserDeletionRepository archives one complete durable profile. It is an
// optional administrative port and is never used by gameplay transports.
type UserDeletionRepository interface {
	DeleteByLoginName(context.Context, string) error
}

// UserRecoveryRepository restores an archived profile for dedicated-server administration.
// An empty login name requests its original identity, if still available.
type UserRecoveryRepository interface {
	RestoreDeletedProfile(context.Context, int64, string) (string, error)
}

// UserNameSwapRepository exchanges only the names of an archived and active profile.
type UserNameSwapRepository interface {
	SwapProfileNames(context.Context, ProfileNameSwapRequest) (ProfileNameSwap, error)
}

// UserPurgeRepository permanently removes profiles archived before a cutoff.
type UserPurgeRepository interface {
	PurgeDeletedProfiles(context.Context, time.Time) (int64, error)
}

// UserDeletionHistoryRepository invalidates legacy name-bound credentials after
// an identity has been released by an archived profile.
type UserDeletionHistoryRepository interface {
	LatestProfileDeletion(context.Context, string) (time.Time, error)
}

// TutorialCompletionQueueRepository lists profiles whose content-backed
// starter loadout still needs to be materialized.
type TutorialCompletionQueueRepository interface {
	PendingTutorialCompletionLoginNames(context.Context) ([]string, error)
}

// UserRepository stores durable SporeNet profiles independently of login and
// transport sessions. Implementations must enforce unique login names.
type UserRepository interface {
	Create(context.Context, UserRecord) (int64, error)
	// LoadByLoginName matches the player identity without case sensitivity and
	// returns its canonically stored spelling in UserRecord.LoginName. Archived
	// profiles must be excluded from every normal repository lookup.
	LoadByLoginName(context.Context, string) (UserRecord, error)
	// Save atomically accepts a detached profile snapshot. Runtime adapters may
	// buffer durability; failed admission must not replace the accepted snapshot.
	Save(context.Context, UserRecord) error
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return errors.New("user repository: nil context")
	}
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("contextDone: %w", err)
	}
	return nil
}
