package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/zone"
)

type heroCombatPresentation struct {
	zone        *zone.Zone
	publishedAt time.Time
}

type heroCombatCommit struct {
	registry            *gameplaySessionRegistry
	sessionKey          string
	generation          uint64
	transportGeneration uint64
	presentation        *heroCombatPresentation
}

func (e heroCombatCommit) commit() {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	peerSession, isFound := e.registry.sessions[e.sessionKey]
	if !isFound || peerSession.generation != e.generation ||
		peerSession.transportGeneration != e.transportGeneration ||
		peerSession.zone != e.presentation.zone {
		return
	}
	peerSession.heroCombatPresentation = e.presentation
	e.registry.sessions[e.sessionKey] = peerSession
}

func (e gameplayPendingRuntime) heroCombatPackets(req raknet.Packet) ([][]byte, error) {
	sessionKey := req.Address.String()
	e.registry.mutex.RLock()
	peerSession, isFound := e.registry.sessions[sessionKey]
	e.registry.mutex.RUnlock()
	if !isFound || !peerSession.stage.IsDungeon() || peerSession.zone == nil ||
		peerSession.binding.Mode == game.ModeArena || peerSession.isRejoinPending ||
		!peerSession.dungeonSetup.IsCommitted() {
		return nil, nil
	}
	now := e.now()
	previous := peerSession.heroCombatPresentation
	// Refresh after object recreation as well as on transitions. A sparse
	// update does not restart the client's combat/idle animation timers.
	if previous != nil && previous.zone == peerSession.zone &&
		now.Sub(previous.publishedAt) < 250*time.Millisecond {
		return nil, nil
	}
	presentation := &heroCombatPresentation{zone: peerSession.zone, publishedAt: now}
	packets := make([][]byte, 0)
	for _, state := range peerSession.zone.HeroCombatStates() {
		packet, err := raknet.MarshalApplication(raknet.HeroCombatStateMessage{
			ObjectID: state.ObjectID, IsInCombat: state.IsInCombat,
		})
		if err != nil {
			return nil, fmt.Errorf("heroCombatMarshal: %w", err)
		}
		packets = append(packets, packet)
	}
	if len(packets) == 0 {
		return nil, nil
	}
	commit := heroCombatCommit{
		registry: e.registry, sessionKey: sessionKey, generation: peerSession.generation,
		transportGeneration: peerSession.transportGeneration, presentation: presentation,
	}
	err := req.AfterResponseCommit(commit.commit)
	if err != nil {
		return nil, fmt.Errorf("heroCombatCommit: %w", err)
	}
	return packets, nil
}
