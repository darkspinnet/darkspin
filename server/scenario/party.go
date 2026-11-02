//go:build scenario

package scenario

import "errors"

var ErrPartySessionMissing = errors.New("owned party actor is not connected to this session provider")

// PartyRequest describes an explicitly authorized server-owned party operation.
// It never asserts that a client initiated an input or accepted a native UI job.
type PartyRequest struct {
	Operation        string
	ActorID          int64
	ActorSessionID   string
	InviteeID        int64
	InviteeSessionID string
	LeaderID         int64
	LeaderSessionID  string
	PartyID          uint32
	Name             string
	OwnedUserIDs     []int64
}

type PartyMember struct {
	UserID      int64
	Name        string
	AvatarID    uint32
	JoinOrdinal uint32
}

type PartyState struct {
	ID       uint32
	Name     string
	LeaderID int64
	Revision uint64
	Members  []PartyMember
}

// PartyResult retains partial authority/projection evidence. A failed attempted
// mutation must not be replayed automatically. Sent notifications are not proof
// that a native client has applied or accepted its resulting party state.
type PartyResult struct {
	Operation                  string
	ActorID                    int64
	ActorSessionID             string
	IsMutationAttempted        bool
	IsMutationAccepted         bool
	IsProjectionComplete       bool
	IsNativeAcceptanceObserved bool
	NotificationCount          int
	DeliveredNotificationCount int
	State                      PartyState
}
