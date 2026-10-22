package gameplay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/zone"
)

// poll appends current hero combat state after world creates and updates, even
// when queued combat traffic takes one of pollWorld's early return paths.
func (e gameplayPendingRuntime) poll(ctx context.Context, req raknet.Packet) ([][]byte, error) {
	if !e.registry.beginRequest() {
		return nil, errors.New("gameplay registry closed")
	}
	defer e.registry.endRequest()
	if req.Address == nil {
		return nil, errors.New("pending poll: nil address")
	}
	packets, err := e.pollWorld(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("worldPoll: %w", err)
	}
	combatPackets, err := e.heroCombatPackets(req)
	if err != nil {
		return nil, fmt.Errorf("heroCombatPoll: %w", err)
	}
	return append(packets, combatPackets...), nil
}

type heroCombatPresentation struct {
	zone                *zone.Zone
	publishedAt         time.Time
	transportGeneration uint64
	objectID            uint32
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
		peerSession.zone != e.presentation.zone ||
		peerSession.deployedObjectID != e.presentation.objectID {
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
	// Always refresh on a new transport or deployed hero; unchanged counts,
	// including zero, also repair recreated objects on subsequent polls.
	if previous != nil && previous.zone == peerSession.zone &&
		previous.transportGeneration == peerSession.transportGeneration &&
		previous.objectID == peerSession.deployedObjectID &&
		now.Sub(previous.publishedAt) < 250*time.Millisecond {
		return nil, nil
	}
	presentation := &heroCombatPresentation{
		zone: peerSession.zone, publishedAt: now,
		transportGeneration: peerSession.transportGeneration,
		objectID:            peerSession.deployedObjectID,
	}
	packets := make([][]byte, 0)
	for _, state := range peerSession.zone.HeroCombatStates() {
		packet, err := raknet.MarshalApplication(raknet.HeroCombatStateMessage{
			ObjectID: state.ObjectID, IsInCombat: state.IsInCombat,
			AttackerCount: state.AttackerCount,
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
