//go:build scenario

package scenario

import "errors"

var ErrPVPSessionMissing = errors.New("owned PvP participant is not connected to this session provider")

// PVPParticipant pins one owned authenticated client to an explicit arena team.
type PVPParticipant struct {
	UserID    int64
	SessionID string
	Team      uint16
}

// PVPRequest prepares a server-owned 1v1 or 2v2 arena fixture. Each team has
// TeamSize explicit owned clients. It does not assert that clients requested
// matchmaking or accepted a native matchmaking job.
// Participants establish their parties before preparation; 1v1 opponents share
// one two-member party. Build 103 receives user-group setup for those actual
// groups without an invented matchmaking job.
type PVPRequest struct {
	Participants []PVPParticipant
	OwnedUserIDs []int64
	TeamSize     uint16
}

// PVPResult distinguishes authoritative formation from notification delivery.
// Native loading and readiness must be observed separately by the supervisor.
type PVPResult struct {
	GameID                     uint32
	Participants               []PVPParticipant
	IsMutationAttempted        bool
	IsMutationAccepted         bool
	IsProjectionComplete       bool
	NotificationCount          int
	DeliveredNotificationCount int
}
