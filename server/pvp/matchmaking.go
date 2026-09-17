// Package pvp owns campaign and competitive matchmaking and match rules.
package pvp

import (
	"errors"
	"sync"
)

const (
	CampaignGameType             uint32 = 2
	CampaignExpectedPlayerCount  uint16 = 4
	CampaignTeamSize             uint16 = 0
	ArenaGameType                uint32 = 3
	ArenaTeamCount               uint16 = 2
	ArenaOneVersusOnePlayerCount uint16 = 2
	ArenaOneVersusOneTeamSize    uint16 = 1
	ArenaExpectedPlayerCount     uint16 = 4
	ArenaTeamSize                uint16 = 2
)

var (
	ErrMatchmakingInvalid     = errors.New("matchmaking request invalid")
	ErrMatchmakingUnsupported = errors.New("matchmaking request unsupported")
)

// MatchmakingCriteria is the transport-neutral queue contract.
type MatchmakingCriteria struct {
	GameType            uint32
	ExpectedPlayerCount uint16
	TeamSize            uint16
	SelectedDifficulty  string
	IsRanked            bool
}

// MatchmakingRequest identifies one authenticated queue entrant.
type MatchmakingRequest struct {
	UserID   int64
	UserIDs  []int64
	PartyID  uint32
	Criteria MatchmakingCriteria
}

// MatchmakingSession is one owned queue reservation.
type MatchmakingSession struct {
	ID       uint64
	OwnerID  int64
	UserID   int64
	PartyID  uint32
	Criteria MatchmakingCriteria
}

// MatchmakingAdmission contains the caller's queue session and, when the
// compatible queue reaches capacity, the atomically consumed match roster.
type MatchmakingAdmission struct {
	Session  MatchmakingSession
	Sessions []MatchmakingSession
}

// Matchmaking owns active campaign and PVP queue sessions. Match formation
// remains a separate operation so incomplete Blaze completion cannot consume
// entrants.
type Matchmaking struct {
	mutex          sync.Mutex
	nextID         uint64
	sessions       map[uint64]matchmakingReservation
	sessionByUsers map[int64]uint64
}

type matchmakingReservation struct {
	ID       uint64
	OwnerID  int64
	UserIDs  []int64
	PartyID  uint32
	Criteria MatchmakingCriteria
}

func NewMatchmaking() *Matchmaking {
	return &Matchmaking{
		nextID: 1, sessions: make(map[uint64]matchmakingReservation),
		sessionByUsers: make(map[int64]uint64),
	}
}

// Enter validates and reserves one supported queue session. Repeated entry by
// the same user is idempotent while the original session remains active.
func (e *Matchmaking) Enter(req MatchmakingRequest) (MatchmakingAdmission, error) {
	if e == nil || req.UserID <= 0 {
		return MatchmakingAdmission{}, ErrMatchmakingInvalid
	}
	userIDs, err := matchmakingUserIDs(req)
	if err != nil {
		return MatchmakingAdmission{}, err
	}
	isCampaign := req.Criteria.GameType == CampaignGameType &&
		req.Criteria.ExpectedPlayerCount == CampaignExpectedPlayerCount &&
		req.Criteria.TeamSize == CampaignTeamSize &&
		!req.Criteria.IsRanked && req.Criteria.SelectedDifficulty != ""
	isArena := isArenaCriteria(req.Criteria)
	if !isCampaign && !isArena {
		return MatchmakingAdmission{}, ErrMatchmakingUnsupported
	}
	if isArena && len(userIDs) > int(req.Criteria.TeamSize) {
		return MatchmakingAdmission{}, ErrMatchmakingInvalid
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if sessionID, isFound := e.sessionByUsers[req.UserID]; isFound {
		reservation := e.sessions[sessionID]
		if reservation.Criteria != req.Criteria {
			return MatchmakingAdmission{}, ErrMatchmakingInvalid
		}
		return MatchmakingAdmission{Session: MatchmakingSession{
			ID: sessionID, OwnerID: reservation.OwnerID, UserID: req.UserID,
			PartyID: reservation.PartyID, Criteria: reservation.Criteria,
		}}, nil
	}
	for _, userID := range userIDs {
		if _, isFound := e.sessionByUsers[userID]; isFound {
			return MatchmakingAdmission{}, ErrMatchmakingInvalid
		}
	}
	reservation := matchmakingReservation{
		ID: e.nextID, OwnerID: req.UserID, UserIDs: userIDs,
		PartyID: req.PartyID, Criteria: req.Criteria,
	}
	e.nextID++
	e.sessions[reservation.ID] = reservation
	for _, userID := range reservation.UserIDs {
		e.sessionByUsers[userID] = reservation.ID
	}
	compatibleSessions := make([]MatchmakingSession, 0, req.Criteria.ExpectedPlayerCount)
	for sessionID := uint64(1); sessionID < e.nextID; sessionID++ {
		queuedReservation, isFound := e.sessions[sessionID]
		if !isFound || queuedReservation.Criteria != req.Criteria {
			continue
		}
		if len(compatibleSessions)+len(queuedReservation.UserIDs) >
			int(req.Criteria.ExpectedPlayerCount) {
			continue
		}
		for _, userID := range queuedReservation.UserIDs {
			compatibleSessions = append(compatibleSessions, MatchmakingSession{
				ID: queuedReservation.ID, OwnerID: queuedReservation.OwnerID,
				UserID: userID, PartyID: queuedReservation.PartyID,
				Criteria: queuedReservation.Criteria,
			})
		}
		if len(compatibleSessions) == int(req.Criteria.ExpectedPlayerCount) {
			break
		}
	}
	if len(compatibleSessions) != int(req.Criteria.ExpectedPlayerCount) {
		return MatchmakingAdmission{Session: MatchmakingSession{
			ID: reservation.ID, OwnerID: reservation.OwnerID,
			UserID: req.UserID, PartyID: reservation.PartyID,
			Criteria: req.Criteria,
		}}, nil
	}
	for _, matchedSession := range compatibleSessions {
		delete(e.sessionByUsers, matchedSession.UserID)
		delete(e.sessions, matchedSession.ID)
	}
	return MatchmakingAdmission{Session: MatchmakingSession{
		ID: reservation.ID, OwnerID: reservation.OwnerID,
		UserID: req.UserID, PartyID: reservation.PartyID,
		Criteria: req.Criteria,
	}, Sessions: compatibleSessions}, nil
}

// IsArenaCriteria reports whether the requested competitive roster shape is
// one of the two modes exposed by the shipped Map Room.
func IsArenaCriteria(criteria MatchmakingCriteria) bool {
	return isArenaCriteria(criteria)
}

func isArenaCriteria(criteria MatchmakingCriteria) bool {
	if criteria.GameType != ArenaGameType {
		return false
	}
	isOneVersusOne := criteria.ExpectedPlayerCount == ArenaOneVersusOnePlayerCount &&
		criteria.TeamSize == ArenaOneVersusOneTeamSize
	isTwoVersusTwo := criteria.ExpectedPlayerCount == ArenaExpectedPlayerCount &&
		criteria.TeamSize == ArenaTeamSize
	return isOneVersusOne || isTwoVersusTwo
}

func matchmakingUserIDs(req MatchmakingRequest) ([]int64, error) {
	userIDs := req.UserIDs
	if len(userIDs) == 0 {
		userIDs = []int64{req.UserID}
	}
	uniqueUserIDs := make(map[int64]struct{}, len(userIDs))
	isOwnerFound := false
	for _, userID := range userIDs {
		if userID <= 0 {
			return nil, ErrMatchmakingInvalid
		}
		if _, isFound := uniqueUserIDs[userID]; isFound {
			return nil, ErrMatchmakingInvalid
		}
		uniqueUserIDs[userID] = struct{}{}
		if userID == req.UserID {
			isOwnerFound = true
		}
	}
	if !isOwnerFound {
		return nil, ErrMatchmakingInvalid
	}
	return append([]int64(nil), userIDs...), nil
}

// Cancel removes a session only when it belongs to the authenticated user.
// Missing and already-cancelled sessions are harmless.
func (e *Matchmaking) Cancel(userID int64, sessionID uint64) bool {
	if e == nil || userID <= 0 || sessionID == 0 {
		return false
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	reservation, isFound := e.sessions[sessionID]
	if !isFound || reservation.OwnerID != userID {
		return false
	}
	delete(e.sessions, sessionID)
	for _, participantID := range reservation.UserIDs {
		delete(e.sessionByUsers, participantID)
	}
	return true
}

// IsQueued reports whether a user participates in an active reservation.
func (e *Matchmaking) IsQueued(userID int64) bool {
	if e == nil || userID <= 0 {
		return false
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	_, isFound := e.sessionByUsers[userID]
	return isFound
}
