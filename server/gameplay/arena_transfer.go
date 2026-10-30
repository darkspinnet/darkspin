package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
)

// Build 103's ArenaTeleporterModifier (Lua chunk 766) stops the hero,
// plays character_teleport_out for .5 seconds, teleports, then plays
// character_teleport_in for .5 seconds before releasing its immobilization.
const arenaTeleportPhaseDuration = 500 * time.Millisecond

type arenaTransferMember struct {
	generation  uint64
	objectID    uint32
	lockedUntil time.Time
}

func arenaTeleportAnimation(objectID uint32, name string, timestamp uint64) ([]byte, error) {
	packet, err := raknet.MarshalApplication(raknet.SetAnimationStateMessage{
		ObjectID: objectID, State: util.HashID(name),
		Timestamp: timestamp, Scale: 1,
	})
	if err != nil {
		return nil, fmt.Errorf("arenaAnimation: %w", err)
	}
	return packet, nil
}

func (e arenaCountdownJob) animationTimestamp(now time.Time) uint64 {
	return advanceHeroArrivalTimestamp(e.timestamp, e.startedAt, now)
}

func (e arenaCountdownJob) execute() {
	if e.registry == nil {
		return
	}
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	state := e.registry.arenaMatches[e.gameID]
	if e.registry.isClosed || state == nil || state != e.match ||
		state.result.roundIndex != e.round || !state.isCountdownScheduled || state.isCombatActive {
		return
	}
	animationPackets := make([][]byte, 0, 4)
	now := e.registry.now()
	expectedCount := 0
	e.members = make(map[string]arenaTransferMember)
	for key, member := range e.registry.sessions {
		if member.binding.GameID != e.gameID || member.binding.Mode != game.ModeArena {
			continue
		}
		if !member.stage.IsDungeon() || !member.dungeonSetup.IsCommitted() || member.deployedObjectID == 0 {
			e.releaseTransferLocked(state)
			return
		}
		expectedCount = int(member.binding.ParticipantCount)
		e.members[key] = arenaTransferMember{generation: member.generation, objectID: member.deployedObjectID,
			lockedUntil: now.Add(2 * arenaTeleportPhaseDuration)}
		packet, err := arenaTeleportAnimation(member.deployedObjectID, "character_teleport_out", e.animationTimestamp(now))
		if err != nil {
			e.releaseTransferLocked(state)
			if e.logger != nil {
				e.logger.Printf("Arena departure animation omitted game=%d: %v", e.gameID, err)
			}
			return
		}
		animationPackets = append(animationPackets, packet)
	}
	if expectedCount == 0 || len(animationPackets) != expectedCount {
		e.releaseTransferLocked(state)
		return
	}
	state.isTransferActive = true
	packets := make([][]byte, 0, 12)
	for key, member := range e.registry.sessions {
		if member.binding.GameID != e.gameID || member.binding.Mode != game.ModeArena {
			continue
		}
		err := member.stopPlayerMovement(now)
		if err != nil {
			e.releaseTransferLocked(state)
			if e.logger != nil {
				e.logger.Printf("Arena transfer stop failed game=%d: %v", e.gameID, err)
			}
			return
		}
		member.heroInputLockedObjectID = member.deployedObjectID
		member.heroInputLockedUntil = e.members[key].lockedUntil
		holdPackets, err := member.refreshEnemyImmobilizedAttributes(now)
		if err != nil {
			e.registry.sessions[key] = member
			e.releaseTransferLocked(state)
			if e.logger != nil {
				e.logger.Printf("Arena transfer hold failed game=%d: %v", e.gameID, err)
			}
			return
		}
		stopPackets, err := marshalZonePlayerStop(member.deployedObjectID, member.playerPosition)
		if err != nil {
			e.registry.sessions[key] = member
			e.releaseTransferLocked(state)
			if e.logger != nil {
				e.logger.Printf("Arena transfer stop presentation failed game=%d: %v", e.gameID, err)
			}
			return
		}
		packets = append(packets, stopPackets...)
		packets = append(packets, holdPackets...)
		e.registry.sessions[key] = member
	}
	packets = append(packets, animationPackets...)
	for key, member := range e.registry.sessions {
		if member.binding.GameID != e.gameID || member.binding.Mode != game.ModeArena {
			continue
		}
		member.queuePackets(packets)
		e.registry.sessions[key] = member
	}
	e.scheduleTransferLocked(state, e.teleport)
}

func (e arenaCountdownJob) scheduleTransferLocked(state *arenaMatchState, next func()) {
	cancel, err := e.registry.timer.Schedule(arenaTeleportPhaseDuration, next)
	if err == nil && cancel == nil {
		err = errors.New("arena transfer cancellation unavailable")
	}
	if err != nil {
		if cancel != nil {
			cancel()
		}
		e.releaseTransferLocked(state)
		if e.logger != nil {
			e.logger.Printf("Arena transfer schedule failed game=%d: %v", e.gameID, err)
		}
		return
	}
	state.cancel = cancel
}

func (e arenaCountdownJob) activate() {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	state := e.registry.arenaMatches[e.gameID]
	if e.registry.isClosed || state == nil || state != e.match ||
		state.result.roundIndex != e.round || !state.isCountdownScheduled || state.isCombatActive {
		return
	}
	expectedCount := 0
	readyCount := 0
	for _, member := range e.registry.sessions {
		if member.binding.GameID != e.gameID || member.binding.Mode != game.ModeArena {
			continue
		}
		expectedCount = int(member.binding.ParticipantCount)
		if member.stage.IsDungeon() && member.dungeonSetup.IsCommitted() && member.deployedObjectID != 0 {
			readyCount++
		}
	}
	if expectedCount == 0 || readyCount != expectedCount {
		e.releaseTransferLocked(state)
		return
	}
	if !e.areTransferMembersCurrentLocked() {
		e.releaseTransferLocked(state)
		return
	}
	e.releaseTransferLocked(state)
	for key, member := range e.registry.sessions {
		if member.binding.GameID != e.gameID || member.binding.Mode != game.ModeArena {
			continue
		}
		member.isArenaCombatActive = true
		e.registry.sessions[key] = member
	}
	state.isCombatActive = true
	state.isCountdownScheduled = false
	state.cancel = nil
	e.registry.startArenaResultLocked(e.gameID, e.registry.now())
	clock := &arenaRoundClock{registry: e.registry, match: state, gameID: e.gameID,
		roundIndex: state.result.roundIndex, startedAt: state.result.startedAt,
		sourceTime: e.animationTimestamp(state.result.startedAt)}
	state.clock = clock
	clockErr := clock.scheduleLocked()
	if clockErr != nil && e.logger != nil {
		e.logger.Printf("Arena round clock start failed game=%d: %v", e.gameID, clockErr)
	}
	if e.logger != nil {
		e.logger.Printf("RakNet Arena combat started game=%d after authored teleport phases", e.gameID)
	}
}

func (e arenaCountdownJob) areTransferMembersCurrentLocked() bool {
	for key, expected := range e.members {
		member, isFound := e.registry.sessions[key]
		if !isFound || member.generation != expected.generation || member.deployedObjectID != expected.objectID ||
			member.binding.GameID != e.gameID || member.binding.Mode != game.ModeArena {
			return false
		}
	}
	return len(e.members) > 0
}

func (e arenaCountdownJob) releaseTransferLocked(state *arenaMatchState) {
	packets := make([][]byte, 0, len(e.members))
	for key, expected := range e.members {
		member, isFound := e.registry.sessions[key]
		if !isFound || member.generation != expected.generation || member.deployedObjectID != expected.objectID ||
			member.heroInputLockedObjectID != expected.objectID || !member.heroInputLockedUntil.Equal(expected.lockedUntil) {
			continue
		}
		member.heroInputLockedObjectID = 0
		member.heroInputLockedUntil = time.Time{}
		holdPackets, err := member.refreshEnemyImmobilizedAttributes(e.registry.now())
		if err != nil {
			if e.logger != nil {
				e.logger.Printf("Arena transfer release failed game=%d object=%d: %v", e.gameID, expected.objectID, err)
			}
		} else {
			packets = append(packets, holdPackets...)
		}
		e.registry.sessions[key] = member
	}
	for key, member := range e.registry.sessions {
		if member.binding.GameID == e.gameID && member.binding.Mode == game.ModeArena {
			member.queuePackets(packets)
			e.registry.sessions[key] = member
		}
	}
	state.isTransferActive = false
	state.isCountdownScheduled = false
	state.cancel = nil
}
