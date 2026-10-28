package gameplay

import (
	"context"
	"errors"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
)

const arenaRoundOverlayDuration = 5 * time.Second

type arenaRoundEndJob struct {
	registry     *gameplaySessionRegistry
	modifierPool *modifierPool
	effectPool   *attachedEffectPool
	gameID       uint32
	roundIndex   int
}

func (e campaignAbilityCommandRuntime) commitArenaVictory(
	gameID uint32, winnerTeam uint32,
	targetSessionKey string, targetGeneration uint64,
) {
	e.registry.mutex.Lock()
	target, isFound := e.registry.sessions[targetSessionKey]
	match := e.registry.arenaMatches[gameID]
	if !isFound || target.generation != targetGeneration || target.binding.GameID != gameID ||
		target.binding.Mode != game.ModeArena || !target.isZoneGameOver() ||
		match == nil || !match.isCombatActive {
		e.registry.mutex.Unlock()
		return
	}
	for _, member := range e.registry.sessions {
		if member.binding.GameID == gameID && member.binding.Team == target.binding.Team &&
			member.binding.Mode == game.ModeArena && !member.isZoneGameOver() {
			e.registry.mutex.Unlock()
			return
		}
	}
	e.registry.completeArenaResultLocked(gameID, winnerTeam)
	e.registry.retireArenaForcePulsesLocked(gameID)
	for key, member := range e.registry.sessions {
		if member.binding.GameID != gameID || member.binding.Mode != game.ModeArena {
			continue
		}
		member.isArenaCombatActive = false
		member.isArenaSquadAccepted = false
		member.arenaWinningTeam = winnerTeam
		e.registry.clearActionLeasesLocked(key, member.transportGeneration)
		e.registry.sessions[key] = member
	}
	job := arenaRoundEndJob{registry: e.registry, modifierPool: e.modifierPool, effectPool: e.effectPool, gameID: gameID, roundIndex: match.result.roundIndex}
	e.registry.mutex.Unlock()
	// Persistence is independent of both the presentation timer and Continue.
	err := e.registry.saveArenaStats(context.Background(), gameID)
	if err != nil && e.logger != nil {
		e.logger.Printf("Arena completed-match statistics will retry game=%d: %v", gameID, err)
	}
	cancel, err := e.registry.timer.Schedule(arenaRoundOverlayDuration, job.execute)
	if err == nil && cancel == nil {
		err = errors.New("arena round timer returned no cancellation")
	}
	if err != nil {
		if e.logger != nil {
			e.logger.Printf("Arena round transition schedule failed game=%d: %v", gameID, err)
		}
		return
	}
	e.registry.mutex.Lock()
	if e.registry.arenaMatches[gameID] == match {
		match.cancel = cancel
	} else if cancel != nil {
		cancel()
	}
	e.registry.mutex.Unlock()
	if e.logger != nil {
		e.logger.Printf("Arena round completed game=%d round=%d winner_team=%d overlay=5s", gameID, job.roundIndex+1, winnerTeam)
	}
}

func (e arenaRoundEndJob) execute() {
	err := e.registry.saveArenaStats(context.Background(), e.gameID)
	if err != nil {
		e.retryStatistics(err)
		return
	}
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	match := e.registry.arenaMatches[e.gameID]
	if e.registry.isClosed || match == nil || !match.result.isRoundComplete || match.result.roundIndex != e.roundIndex {
		return
	}
	branchPacket, err := raknet.MarshalApplication(raknet.ArenaGameBranchMessage{IsAlternate: match.result.isCompleted})
	if err != nil {
		if e.registry.logger != nil {
			e.registry.logger.Printf("Arena round branch failed: %v", err)
		}
		return
	}
	for key, member := range e.registry.sessions {
		if member.binding.GameID != e.gameID || member.binding.Mode != game.ModeArena {
			continue
		}
		member.isArenaResultReady = match.result.isCompleted
		e.registry.retireFieldMedicBuffsLocked(&member, e.modifierPool)
		e.registry.retireCampaignTreeOfLifeLocked(&member)
		interrupted := member.resetFailedActionAdmission(e.modifierPool, e.effectPool)
		if interrupted != nil {
			interrupted.Stop()
		}
		e.registry.sessions[key] = member
	}
	// Retired channel attachments must precede the scene branch in every
	// recipient FIFO, while their old hero objects still belong to this round.
	e.registry.commitChannelRemovalsLocked()
	for key, member := range e.registry.sessions {
		if member.binding.GameID != e.gameID || member.binding.Mode != game.ModeArena {
			continue
		}
		if !match.result.isCompleted {
			member.arenaWinningTeam = 0
			member.isArenaLobbyEntered = false
			member.isArenaLobbyTransitionSent = true
			member.isArenaSquadAccepted = false
			member.isArenaPreparationSent = false
			member.isArenaPreparationAcknowledged = false
			member.dungeonSetup.Reset()
			member.stage.AwaitResume()
			member.squad = nil
			member.abilityCooldownSession().ResetAll()
			member.abilityReleaseSession().Reset()
		}
		member.queuePackets([][]byte{branchPacket})
		e.registry.sessions[key] = member
	}
	match.cancel = nil
	if e.registry.logger != nil {
		e.registry.logger.Printf("Arena round transition game=%d round=%d score=%d-%d final=%t", e.gameID, e.roundIndex+1, match.result.body.CountA, match.result.body.CountB, match.result.isCompleted)
	}
}

func (e arenaRoundEndJob) retryStatistics(cause error) {
	if e.registry.logger != nil {
		e.registry.logger.Printf("Arena final results waiting for statistics save game=%d: %v", e.gameID, cause)
	}
	cancel, err := e.registry.timer.Schedule(2*time.Second, e.execute)
	if err == nil && cancel == nil {
		err = errors.New("Arena statistics retry cancellation unavailable")
	}
	if err != nil {
		if e.registry.logger != nil {
			e.registry.logger.Printf("Arena statistics retry schedule failed game=%d: %v", e.gameID, err)
		}
		return
	}
	e.registry.mutex.Lock()
	match := e.registry.arenaMatches[e.gameID]
	if !e.registry.isClosed && match != nil && match.result.roundIndex == e.roundIndex {
		match.cancel = cancel
	} else {
		cancel()
	}
	e.registry.mutex.Unlock()
}
