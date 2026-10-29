package gameplay

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	zoneunlock "github.com/darkspinnet/darkspin/server/zone/unlock"
)

func (r gameplayPendingRuntime) consumePlayerEventCommand(
	ctx context.Context, packet raknet.Packet,
) ([][]byte, bool, error) {
	r.registry.mutex.RLock()
	queuedSession, isSessionFound := r.registry.sessions[packet.Address.String()]
	isCommandEligible := isSessionFound && queuedSession.stage.IsDungeon() &&
		queuedSession.dungeonSetup.IsCommitted() &&
		queuedSession.deployedObjectID != 0 && !queuedSession.isZoneTerminal()
	r.registry.mutex.RUnlock()
	if !isSessionFound {
		return nil, false, nil
	}
	spawnPackets, isSpawnHandled, spawnErr := r.consumeScenarioSpawn(ctx, packet, queuedSession, isCommandEligible)
	if spawnErr != nil {
		return spawnPackets, isSpawnHandled, fmt.Errorf("scenarioSpawn: %w", spawnErr)
	}
	if isSpawnHandled {
		return spawnPackets, true, nil
	}
	controlPackets, isControlHandled, controlErr := r.consumeScenarioNPCControl(ctx, packet, queuedSession, isCommandEligible)
	if controlErr != nil {
		return controlPackets, isControlHandled, fmt.Errorf("scenarioNPC: %w", controlErr)
	}
	if isControlHandled {
		return controlPackets, true, nil
	}
	command, isCommandFound, err := r.gameplayJoin.ConsumePlayerEventCommand(
		ctx, int64(queuedSession.binding.UserID), queuedSession.binding.GameID,
		packet.ScheduleGroup != nil || packet.ScheduleGroupResult != nil,
	)
	if err != nil {
		return nil, false, fmt.Errorf("eventConsume: %w", err)
	}
	if !isCommandFound {
		return nil, false, nil
	}
	if !isCommandEligible {
		r.logger.Printf("RakNet discarded developer event %q during unavailable state for %s",
			command.Name, packet.Address)
		return nil, false, nil
	}
	if command.Name == "spore-emote" {
		return r.playModEmote(packet, queuedSession, command)
	}
	if command.Name == "spawn" {
		return r.spawnDeveloperNPC(packet, queuedSession, command)
	}
	if command.Name == "follow" {
		return r.activatePlayerFollow(packet, queuedSession, command)
	}
	if command.Name == "ai" {
		return r.togglePlayerAI(packet, queuedSession)
	}
	if command.Name == "drop-create" {
		return r.createDeveloperEquipmentDrop(ctx, packet, queuedSession, command)
	}
	if command.Name == "recap" {
		return r.recapParty(packet, queuedSession)
	}
	if command.Name == "victory" && queuedSession.binding.Mode == game.ModeArena {
		return r.completeDeveloperArenaVictory(packet, queuedSession)
	}
	if command.Name == "victory" && queuedSession.binding.Mode == game.ModeTutorial {
		return r.completeDeveloperTutorialVictory(ctx, packet, queuedSession)
	}
	if command.Name == "victory" &&
		queuedSession.binding.Mode == game.ModeChain &&
		queuedSession.binding.ChainLevelIndex == zoneunlock.OverdriveChainLevelIndex &&
		!queuedSession.binding.IsOverdriveUnlocked {
		if r.overdriveProgression == nil {
			return nil, false, errors.New("eventVictoryOverdrive: progression unavailable")
		}
		_, unlockErr := r.overdriveProgression.UnlockOverdrive(
			ctx, int64(queuedSession.binding.UserID),
		)
		if unlockErr != nil {
			return nil, false, fmt.Errorf("eventVictoryOverdrive: %w", unlockErr)
		}
		queuedSession.binding.IsOverdriveUnlocked = true
	}
	r.registry.mutex.Lock()
	currentSession, isCurrentFound := r.registry.sessions[packet.Address.String()]
	isCurrent := isCurrentFound && currentSession.generation == queuedSession.generation
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, false, errors.New("eventSession: replaced")
	}
	if queuedSession.binding.IsOverdriveUnlocked {
		currentSession.binding.IsOverdriveUnlocked = true
		if !currentSession.isOverdriveSpent && currentSession.overdriveEnergy == 0 {
			currentSession.overdriveEnergy = queuedSession.overdriveEnergy
		}
		currentSession.isOverdrivePersistencePending = false
	}
	if command.Name == "victory" {
		completionPacket, bossObjectID, victoryErr :=
			currentSession.applyDeveloperVictoryCommand()
		if victoryErr != nil {
			r.registry.mutex.Unlock()
			return nil, false, fmt.Errorf("eventVictoryApply: %w", victoryErr)
		}
		r.registry.sessions[packet.Address.String()] = currentSession
		r.registry.mutex.Unlock()
		publishErr := publishCampaignPeersAfterCommit(
			r.registry, packet, [][]byte{completionPacket},
		)
		if publishErr != nil {
			return nil, false, fmt.Errorf("eventVictoryPublish: %w", publishErr)
		}
		r.logger.Printf(
			"RakNet developer victory published shared boss completion boss=%d for %s; awaiting Return to Ship",
			bossObjectID, packet.Address,
		)
		return [][]byte{completionPacket}, true, nil
	}
	if command.Name == "defeat" {
		if currentSession.binding.Mode != game.ModeChain || currentSession.squad == nil ||
			currentSession.deployedObjectID == 0 || currentSession.isZoneTerminal() {
			r.registry.mutex.Unlock()
			return nil, false, errors.New("eventDefeatApply: defeat session unavailable")
		}
		r.registry.retireHeroInfectionsLocked(&currentSession)
		r.registry.retirePlasmaWreathLocked(&currentSession)
		r.registry.retireOmniShieldsLocked(&currentSession, 0)
		packets, defeatErr := currentSession.applyDeveloperDefeatCommand()
		if defeatErr != nil {
			r.registry.mutex.Unlock()
			return nil, false, fmt.Errorf("eventDefeatApply: %w", defeatErr)
		}
		r.registry.sessions[packet.Address.String()] = currentSession
		r.registry.mutex.Unlock()
		r.logger.Printf("RakNet developer defeat entered Game Over for %s", packet.Address)
		return packets, true, nil
	}
	if command.Name == "kill" {
		result, transition, killErr := currentSession.applyDeveloperKillCommand()
		if killErr != nil {
			r.registry.mutex.Unlock()
			return nil, false, fmt.Errorf("eventKillApply: %w", killErr)
		}
		binding := currentSession.binding
		generation := currentSession.generation
		r.registry.sessions[packet.Address.String()] = currentSession
		r.registry.mutex.Unlock()
		packets, publishErr := r.damage.publishAreaResults(
			packet, packet.Address.String(), generation, currentSession.deployedObjectID,
			packet.SourceTime, binding, result, transition, nil, false,
		)
		if publishErr != nil {
			return nil, false, fmt.Errorf("eventKillPublish: %w", publishErr)
		}
		publishErr = publishCampaignPeersAfterCommit(r.registry, packet, packets)
		if publishErr != nil {
			return nil, false, fmt.Errorf("eventKillPeers: %w", publishErr)
		}
		r.logger.Printf("RakNet developer kill defeated %d enemies for %s", len(result), packet.Address)
		return packets, true, nil
	}
	var fixtureTicket *fixtureDiscard
	if command.Name == "reset" {
		fixtureTicket = r.registry.beginFixtureBlastDiscardLocked(currentSession.binding.GameID)
		r.registry.mutex.Unlock()
		fixtureTicket.wait()
		r.registry.mutex.Lock()
		currentSession, isCurrentFound = r.registry.sessions[packet.Address.String()]
		isCurrent = isCurrentFound && !r.registry.isClosed &&
			currentSession.generation == queuedSession.generation &&
			currentSession.transportGeneration == queuedSession.transportGeneration &&
			currentSession.zone == queuedSession.zone
		if !isCurrent {
			fixtureTicket.finishLocked()
			r.registry.mutex.Unlock()
			return nil, false, errors.New("eventResetSession: replaced")
		}
	}
	if command.Name == "reset" && currentSession.securityTransfer != nil {
		currentSession.securityTransfer.Stop()
		currentSession.securityTransfer = nil
	}
	if command.Name == "reset" {
		currentSession.retireChannelsLocked()
		r.registry.retireOmniShieldsLocked(&currentSession, 0)
		currentSession.retireAuraAreasLocked()
		currentSession.retireChargeRunsLocked()
		r.registry.retirePeerProjectileFreezesLocked(&currentSession)
		r.registry.retireFieldMedicBuffsLocked(&currentSession, r.modifierPool)
		r.registry.retireCampaignTreeOfLifeLocked(&currentSession)
		r.registry.retireHeroInfectionsLocked(&currentSession)
		r.registry.retirePlasmaWreathLocked(&currentSession)
		r.registry.clearActionLeasesLocked(
			packet.Address.String(), currentSession.transportGeneration,
		)
	}
	packets, applyErr := currentSession.applyDeveloperEventCommand(
		r.registry, command, r.now(), packet.SourceTime,
	)
	if applyErr != nil {
		if fixtureTicket != nil {
			fixtureTicket.finishLocked()
		}
		r.registry.mutex.Unlock()
		return nil, false, fmt.Errorf("eventApply: %w", applyErr)
	}
	if command.Name == "reset" {
		resetGameplayPeerRuntime(
			&currentSession, r.modifierPool, r.effectPool,
		)
	}
	restartedEnemyPlans := make([]zonenpc.SpawnPlan, 0)
	if command.Name == "reset" && currentSession.zone != nil {
		syncErr := currentSession.syncZoneHero()
		if syncErr != nil {
			r.logger.Printf(
				"RakNet developer reset hero resync skipped for %s: %v",
				packet.Address, syncErr,
			)
		} else {
			restartedEnemy, restartErr := currentSession.zone.RestartHeroTarget(
				currentSession.binding.UserID, currentSession.generation,
				currentSession.deployedObjectID,
			)
			if restartErr != nil {
				r.logger.Printf(
					"RakNet developer reset enemy restart skipped for %s: %v",
					packet.Address, restartErr,
				)
			} else {
				for _, enemy := range restartedEnemy {
					restartedEnemyPlans = append(
						restartedEnemyPlans, enemy.Plan,
					)
				}
			}
		}
	}
	generation := currentSession.generation
	r.registry.sessions[packet.Address.String()] = currentSession
	if command.Name == "reset" {
		r.registry.commitChannelRemovalsLocked()
		fixtureTicket.finishLocked()
	}
	r.registry.mutex.Unlock()
	if command.Name == "reset" {
		restartedEnemyPackets, restartErr := r.damage.npc.scheduleFirstActions(
			packet, packet.Address.String(), generation, restartedEnemyPlans,
			packet.SourceTime,
		)
		if restartErr != nil {
			r.logger.Printf(
				"RakNet developer reset retained player recovery for %s while NPC restart failed: %v",
				packet.Address, restartErr,
			)
		} else {
			packets = append(packets, restartedEnemyPackets...)
		}
	}
	r.logger.Printf("RakNet developer event %q accepted for %s", command.Name, packet.Address)
	return packets, true, nil
}
