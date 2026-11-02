package pvp

import (
	"cmp"
	"fmt"
	"slices"
)

type arenaParty struct {
	partyID uint32
	members []MatchmakingSession
}

// OrderArenaSessions fills contiguous team slots with complete parties first.
// In 2v2, a singleton playgroup must not straddle a later two-player party
// across the team boundary. The native two-player 1v1 party remains opposing.
func OrderArenaSessions(sessions []MatchmakingSession, criteria MatchmakingCriteria) ([]MatchmakingSession, error) {
	if !IsArenaCriteria(criteria) || len(sessions) != int(criteria.ExpectedPlayerCount) {
		return nil, ErrMatchmakingInvalid
	}
	parties := make([]arenaParty, 0, len(sessions))
	users := make(map[int64]bool, len(sessions))
	for _, session := range sessions {
		if session.UserID <= 0 || users[session.UserID] {
			return nil, ErrMatchmakingInvalid
		}
		users[session.UserID] = true
		partyIndex := len(parties)
		if session.PartyID != 0 {
			for index, party := range parties {
				if party.partyID == session.PartyID {
					partyIndex = index
					break
				}
			}
		}
		if partyIndex == len(parties) {
			parties = append(parties, arenaParty{partyID: session.PartyID})
		}
		parties[partyIndex].members = append(parties[partyIndex].members, session)
	}
	for _, party := range parties {
		if !IsArenaPartySizeAllowed(criteria, party.partyID, len(party.members)) {
			return nil, fmt.Errorf("rosterParty: %w", ErrMatchmakingInvalid)
		}
	}
	slices.SortStableFunc(parties, func(first, second arenaParty) int {
		return cmp.Compare(len(second.members), len(first.members))
	})
	orderedSessions := make([]MatchmakingSession, 0, len(sessions))
	for _, party := range parties {
		orderedSessions = append(orderedSessions, party.members...)
	}
	return orderedSessions, nil
}
