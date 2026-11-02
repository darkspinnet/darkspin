package snapshot

import "time"

// ArenaLoadingState preserves lobby gates before a zone exists. Transport
// health alone cannot distinguish a missing player from an unaccepted deck.
type ArenaLoadingState struct {
	ExpectedPlayerMask        uint32    `json:"expected_player_mask"`
	KnownPlayerMask           uint32    `json:"known_player_mask"`
	ParticipantCount          uint16    `json:"participant_count"`
	SquadID                   uint32    `json:"squad_id"`
	CreatureIDs               [3]uint32 `json:"creature_ids"`
	Nouns                     [3]uint32 `json:"nouns"`
	IsPartyMerged             bool      `json:"is_party_merged"`
	IsLobbyTransitionSent     bool      `json:"is_lobby_transition_sent"`
	IsLobbyEntered            bool      `json:"is_lobby_entered"`
	IsSquadAccepted           bool      `json:"is_squad_accepted"`
	IsPreparationSent         bool      `json:"is_preparation_sent"`
	IsPreparationAcknowledged bool      `json:"is_preparation_acknowledged"`
	IsPlayerStatusKnown       bool      `json:"is_player_status_known"`
	PlayerStatus              uint32    `json:"player_status"`
	PlayerProgress            float32   `json:"player_progress"`
	LobbyTransitionAt         time.Time `json:"lobby_transition_at"`
	LobbyTransitionCount      uint32    `json:"lobby_transition_count"`
}
