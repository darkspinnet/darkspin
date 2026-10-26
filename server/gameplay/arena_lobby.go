package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
)

func (e gameplayArenaRuntime) acceptSquad(
	sessionKey string, peerSession gameplayPeerSession, req raknet.ArenaPlayerCommand,
) ([][]byte, error) {
	if !req.IsAccepted || req.DeckID <= 0 || !peerSession.isArenaLobbyEntered ||
		peerSession.isArenaPreparationSent {
		e.recordSquadDecision(sessionKey, peerSession, req, "ignored", "acceptance, deck or lobby gate unavailable")
		return nil, nil
	}
	binding, err := e.gameplayJoin.SelectArenaSquad(peerSession.binding, uint32(req.DeckID))
	if err != nil {
		e.recordSquadDecision(sessionKey, peerSession, req, "rejected", err.Error())
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
		e.recordSquadDecision(sessionKey, peerSession, req, "ignored", "lobby changed during squad selection")
		return nil, nil
	}
	current.binding = binding
	current.isArenaSquadAccepted = true
	e.registry.sessions[sessionKey] = current
	e.registry.mutex.Unlock()
	e.recordSquadDecision(sessionKey, current, req, "accepted", "owned unlocked PVP squad selected")
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

// Retain the admission reason in sync snapshots even if the ordinary log tail
// has already rolled past the failed Continue attempt.
func (e gameplayArenaRuntime) recordSquadDecision(
	sessionKey string, member gameplayPeerSession, req raknet.ArenaPlayerCommand,
	outcome string, reason string,
) {
	e.registry.recordDiagnosticDecision(gameplayDiagnosticDecision{
		occurredAt: time.Now(), kind: "arena_squad_admission", remote: sessionKey,
		transportGeneration: member.transportGeneration, zoneGeneration: member.generation,
		outcome: outcome, reason: fmt.Sprintf("deck=%d: %s", req.DeckID, reason),
	})
}
