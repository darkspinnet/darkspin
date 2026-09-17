package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
)

func (e gameplayArenaRuntime) acceptSquad(
	sessionKey string, peerSession gameplayPeerSession, req raknet.ArenaPlayerCommand,
) ([][]byte, error) {
	if !req.IsAccepted || req.DeckID <= 0 || !peerSession.isArenaLobbyEntered ||
		peerSession.isArenaPreparationSent {
		return nil, nil
	}
	binding, err := e.gameplayJoin.SelectArenaSquad(peerSession.binding, uint32(req.DeckID))
	if err != nil {
		if e.logger != nil {
			e.logger.Printf("RakNet Arena squad rejected user=%d deck=%d: %v",
				peerSession.binding.UserID, req.DeckID, err)
		}
		return nil, nil
	}
	e.registry.mutex.Lock()
	current, isFound := e.registry.sessions[sessionKey]
	if !isFound || current.generation != peerSession.generation ||
		current.isArenaPreparationSent || !current.isArenaLobbyEntered {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	current.binding = binding
	current.isArenaSquadAccepted = true
	e.registry.sessions[sessionKey] = current
	e.registry.mutex.Unlock()
	if e.logger != nil {
		e.logger.Printf("RakNet Arena squad accepted game=%d user=%d deck=%d",
			binding.GameID, binding.UserID, binding.SquadID)
	}
	packets, err := e.prepareSelectedDecks(sessionKey, binding.GameID, false)
	if err != nil {
		return nil, fmt.Errorf("arenaPrepare: %w", err)
	}
	return packets, nil
}
