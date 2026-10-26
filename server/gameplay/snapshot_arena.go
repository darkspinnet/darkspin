package gameplay

import (
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/snapshot"
)

func snapshotArenaLoading(member gameplayPeerSession) *snapshot.ArenaLoadingState {
	if member.binding.Mode != game.ModeArena {
		return nil
	}
	state := &snapshot.ArenaLoadingState{
		ExpectedPlayerMask: member.binding.PlayerMask, KnownPlayerMask: member.knownPlayerMask,
		ParticipantCount: member.binding.ParticipantCount, SquadID: member.binding.SquadID,
		IsPartyMerged: member.isPartyMerged, IsLobbyTransitionSent: member.isArenaLobbyTransitionSent,
		IsLobbyEntered: member.isArenaLobbyEntered, IsSquadAccepted: member.isArenaSquadAccepted,
		IsPreparationSent:         member.isArenaPreparationSent,
		IsPreparationAcknowledged: member.isArenaPreparationAcknowledged,
		IsPlayerStatusKnown:       member.isPlayerStatusKnown,
		PlayerStatus:              uint32(member.lastPlayerStatus.Status), PlayerProgress: member.lastPlayerStatus.Progress,
		LobbyTransitionAt: member.arenaLobbyTransitionAt, LobbyTransitionCount: member.arenaLobbyTransitionCount,
	}
	for index, creature := range member.binding.Creatures {
		state.CreatureIDs[index] = creature.ID
		state.Nouns[index] = creature.Noun
	}
	return state
}
