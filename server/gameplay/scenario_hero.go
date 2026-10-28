//go:build scenario

package gameplay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/scenario"
	"github.com/darkspinnet/darkspin/server/squad"
	actionraknet "github.com/darkspinnet/darkspin/server/zone/action/raknet103"
	zonehero "github.com/darkspinnet/darkspin/server/zone/hero"
)

type scenarioHeroResetProvider interface {
	ScenarioResetHero(context.Context, scenario.HeroResetRequest, game.GameplayCreature) (scenario.HeroResetReceipt, error)
}

func (e Lifecycle) ScenarioResetHero(ctx context.Context, req scenario.HeroResetRequest, creature game.GameplayCreature) (scenario.HeroResetReceipt, error) {
	provider, isSupported := e.syncSnapshot.(scenarioHeroResetProvider)
	if !isSupported {
		return scenario.HeroResetReceipt{}, errors.New("hero reset provider unavailable")
	}
	receipt, err := provider.ScenarioResetHero(ctx, req, creature)
	if err != nil {
		return receipt, fmt.Errorf("resetProvider: %w", err)
	}
	return receipt, nil
}

func (e *gameplaySessionRegistry) configureScenarioHeroPools(modifierPool *modifierPool, effectPool *attachedEffectPool) {
	e.scenarioModifierPool = modifierPool
	e.scenarioEffectPool = effectPool
}

// This fixture changes only an active owned arena, never saved creatures,
// match statistics, immunity, or the production ability acceptance rules.
func (e *gameplaySessionRegistry) ScenarioResetHero(ctx context.Context, req scenario.HeroResetRequest, creature game.GameplayCreature) (scenario.HeroResetReceipt, error) {
	result := scenario.HeroResetReceipt{}
	if ctx == nil {
		return result, errors.New("hero reset requires context")
	}
	contextErr := ctx.Err()
	if contextErr != nil {
		return result, fmt.Errorf("resetContext: %w", contextErr)
	}
	if !req.IsAbilityRankSet && req.AbilityRank != 0 {
		return result, errors.New("ability rank requires its explicit server fixture flag")
	}
	err := e.lockScenarioSpawn(ctx)
	if err != nil {
		return result, fmt.Errorf("resetLock: %w", err)
	}
	defer e.mutex.Unlock()
	member, observation, err := e.scenarioSessionLocked(ctx, req.UserID)
	if err != nil {
		return result, fmt.Errorf("resetSession: %w", err)
	}
	match := e.arenaMatches[req.GameID]
	if observation.Outcome != scenario.Passed || member.binding.Mode != game.ModeArena ||
		member.binding.GameID != req.GameID || member.generation != req.PeerGeneration ||
		member.transportGeneration != req.TransportGeneration || member.deployedObjectID != req.ObjectID ||
		member.squad == nil || member.squad.State().IsGameOver || !member.dungeonSetup.IsCommitted() ||
		!member.isArenaCombatActive || match == nil || !match.isCombatActive || match.isTransferActive {
		return result, errors.New("hero reset requires the observed committed live arena transport")
	}
	ownedUsers := make(map[uint64]bool, len(req.OwnedUserIDs))
	for _, userID := range req.OwnedUserIDs {
		if userID > 0 {
			ownedUsers[uint64(userID)] = true
		}
	}
	for _, candidate := range e.sessions {
		if candidate.binding.GameID != req.GameID {
			continue
		}
		if !ownedUsers[candidate.binding.UserID] || candidate.binding.Mode != game.ModeArena ||
			!candidate.dungeonSetup.IsCommitted() || !candidate.isArenaCombatActive {
			return result, errors.New("hero reset requires every arena participant to be an owned ready client")
		}
		result.RecipientCount++
	}
	index := member.deployedCreatureIndex
	character, isFound := member.squad.Character(index)
	if !isFound || !character.IsAvailable || character.HitPoints <= 0 {
		return result, errors.New("hero reset requires a living deployed hero; completed deaths are not rewound")
	}
	retirement := member.scenarioAbility.retirement
	if retirement != nil {
		if req.IsRetirement || req.IsPlacement || req.IsCooldownReset || req.IsAbilityRankSet || req.Noun == 0 ||
			req.HitPoint != nil || req.ManaPoint != nil || !retirement.matches(member, req) {
			return result, errors.New("hero replacement retirement is pending; only its matching commit is permitted")
		}
	} else if req.Noun != 0 && !req.IsRetirement {
		return result, errors.New("hero replacement requires retirement and a native deletion fence before commit")
	}
	if req.IsRetirement && (req.Noun == 0 || req.IsPlacement || req.IsCooldownReset || req.IsAbilityRankSet || req.HitPoint != nil || req.ManaPoint != nil) {
		return result, errors.New("hero retirement requires only a replacement noun and observed position")
	}
	if req.IsAbilityRankSet {
		return e.setScenarioHeroAbilityRankLocked(member, req, result.RecipientCount)
	}
	if req.IsPlacement {
		return e.placeScenarioHeroLocked(member, req, result.RecipientCount)
	}
	if req.IsCooldownReset {
		return e.resetScenarioCooldownsLocked(member, req, result.RecipientCount)
	}
	if req.Noun != 0 && (req.HitPoint != nil || req.ManaPoint != nil) {
		return result, errors.New("resource fields require a resource-only restore")
	}
	maximumHitPoint, maximumManaPoint := member.characterResourceMaximum(index)
	if req.Noun != 0 {
		if creature.Noun != req.Noun || creature.ID == 0 || creature.HitPoint <= 0 || creature.PowerPoint <= 0 ||
			req.Position == nil || !isWorldPathPointFinite(*req.Position) {
			return result, errors.New("replacement hero template unavailable")
		}
		maximumHitPoint, maximumManaPoint = creature.HitPoint, creature.PowerPoint
	}
	if req.IsRetirement {
		return e.retireScenarioHeroLocked(ctx, member, req, creature, result.RecipientCount)
	}
	// Prepare the complete wire projection before any mutation.
	projected := member
	projected.binding.Creatures = member.binding.Creatures
	var replacement *squad.Session
	if req.Noun != 0 {
		projected.binding.Creatures[index] = creature
		projected.playerPosition = raknet.Vector3(*req.Position)
		projected.arenaHitPointIncreases = [squad.Size]float32{}
		state := member.squad.State()
		for characterIndex := range state.Characters {
			if !state.Characters[characterIndex].IsAvailable {
				continue
			}
			// The retirement below removes temporary Field Medic capacity from
			// every owned slot. Preserve its clamped result in the fresh squad
			// and in the roster projection instead of resurrecting excess HP.
			state.Characters[characterIndex].HitPoints = min(state.Characters[characterIndex].HitPoints, projected.binding.Creatures[characterIndex].HitPoint)
		}
		state.Characters[index] = squad.Character{HitPoints: maximumHitPoint, ManaPoints: maximumManaPoint, IsAvailable: true}
		replacement, err = squad.Restore(state)
		if err != nil {
			return result, fmt.Errorf("resetSquad: %w", err)
		}
		projected.squad = replacement
	}
	restoredHitPoint, restoredManaPoint := maximumHitPoint, maximumManaPoint
	if req.HitPoint != nil {
		restoredHitPoint = *req.HitPoint
	}
	if req.ManaPoint != nil {
		restoredManaPoint = *req.ManaPoint
	}
	if !isFiniteZonePosition(raknet.Vector3{X: restoredHitPoint, Y: restoredManaPoint}) ||
		restoredHitPoint <= 0 || restoredHitPoint > maximumHitPoint || restoredManaPoint < 0 || restoredManaPoint > maximumManaPoint {
		return result, errors.New("fixture resources must be finite and within the living hero maximum")
	}
	packets, err := scenarioHeroResetPackets(projected, restoredHitPoint, restoredManaPoint, req.Noun != 0)
	if err != nil {
		return result, fmt.Errorf("resetPackets: %w", err)
	}
	previousBatchID := member.nextPendingPacketID
	previousBatchIDs := e.scenarioCleanupBatchIDsLocked(req.GameID)
	if req.Noun != 0 {
		// A fresh squad fences delayed callbacks holding the old actor identity.
		e.retireFieldMedicBuffsLocked(&member, e.scenarioModifierPool)
		e.retirePlasmaWreathLocked(&member)
		e.retireCampaignTreeOfLifeLocked(&member)
		member.stopHardenedMantle()
		soulPackets, soulErr := member.clearSoulRavagerPresentation()
		if soulErr != nil && e.logger != nil {
			e.logger.Printf("Scenario hero soul cleanup user=%d: %v", member.binding.UserID, soulErr)
		}
		member.queuePackets(soulPackets)
		interrupted := member.resetFailedActionAdmission(e.scenarioModifierPool, e.scenarioEffectPool)
		if interrupted != nil {
			interrupted.Stop()
		}
		member.queuePackets(member.takeHeroInfections())
		member.binding.Creatures[index] = creature
		member.squad = replacement
		member.scenarioAbility.last = nil
		member.clearScenarioHeroAbilityRank()
		member.scenarioAbility.rejection = nil
		member.scenarioAbility.retirement = nil
		member.scenarioAbility.isCaptureUnavailable = false
		err = member.teleportPlayer(e.now(), projected.playerPosition)
		if err != nil && e.logger != nil {
			e.logger.Printf("Scenario hero position preservation user=%d: %v", member.binding.UserID, err)
		}
		member.arenaHitPointIncreases = projected.arenaHitPointIncreases
		member.passiveKillStack[index] = 0
		member.resetPassiveDamageReduction(index)
		member.resetFireRavagerBasic(index)
		member.resetTCShield(index)
		member.isTCShieldEffectAttached[index] = false
		member.tcShieldEffectSlot[index] = 0
		member.isMissileTargetingPresented[index] = false
		member.missileTargetingEffectSlots[index] = 0
		member.attackPose = retainedAttackPose{}
		delete(match.recentDamages, member.deployedObjectID)
		member.passiveStationarySince[index] = e.now()
		member.abilityCooldownSession().ResetAll()
		member.abilityReleaseSession().Reset()
		member.squad.ResetDeployCooldown()
		member.heroInputLockedObjectID = 0
		member.heroInputLockedUntil = time.Time{}
		member.isHeroSelectionPending = false
		member.isHeroSelectionScheduled = false
		member.heroSelectionReadyAt = time.Time{}
		initialPackets, initialErr := member.activateTCShield(index, e.now(), e.scenarioEffectPool, e)
		if initialErr != nil && e.logger != nil {
			e.logger.Printf("Scenario hero passive activation user=%d: %v", member.binding.UserID, initialErr)
		}
		packets = append(packets, initialPackets...)
	} else {
		isGameOver, healthErr := member.squad.SetHitPoints(index, restoredHitPoint)
		if healthErr != nil {
			return result, fmt.Errorf("resetHealth: %w", healthErr)
		}
		if isGameOver {
			return result, errors.New("resource restore unexpectedly ended the squad")
		}
		manaErr := member.squad.SetManaPoints(index, restoredManaPoint)
		if manaErr != nil {
			return result, fmt.Errorf("resetMana: %w", manaErr)
		}
	}
	for key, candidate := range e.sessions {
		if candidate.binding.UserID == member.binding.UserID && candidate.generation == member.generation {
			if req.Noun != 0 {
				e.clearActionLeasesLocked(key, member.transportGeneration)
			}
			e.sessions[key] = member
			break
		}
	}
	if req.Noun != 0 {
		// Action recovery queues local summon/modifier cleanup. A live fixture
		// must publish that cleanup to the opposing client too, before replacing
		// the actor. Channels retain their own per-recipient retirement leases.
		cleanupPackets := make([][]byte, 0)
		for _, batch := range member.pendingPacketBatches {
			if batch.id > previousBatchID && batch.channelCreation == nil && batch.channelRemoval == nil {
				cleanupPackets = append(cleanupPackets, batch.packets...)
			}
		}
		e.queueScenarioCleanupLocked(req.GameID, member.binding.UserID, cleanupPackets, previousBatchIDs)
	}
	e.commitChannelRemovalsLocked()
	e.queueFieldMedicRealmPacketsLocked(nil, req.GameID, packets)
	return scenario.HeroResetReceipt{GameID: req.GameID, ObjectID: req.ObjectID, Noun: member.binding.Creatures[index].Noun,
		Index: index, HitPoint: restoredHitPoint, ManaPoint: restoredManaPoint, RecipientCount: result.RecipientCount,
		IsReplacement: req.Noun != 0, IsQueued: true}, nil
}

// Positioning is an explicit fixture action, never evidence for native movement.
// Use a point observed on a loaded client; Arena has no campaign navigation mesh.
func (e *gameplaySessionRegistry) placeScenarioHeroLocked(member gameplayPeerSession, req scenario.HeroResetRequest, recipientCount int) (scenario.HeroResetReceipt, error) {
	result := scenario.HeroResetReceipt{}
	if req.Noun != 0 || req.Position == nil || !isWorldPathPointFinite(*req.Position) {
		return result, errors.New("hero placement requires only a finite fixture position")
	}
	packets, err := actionraknet.Teleport(actionraknet.TeleportRequest{ObjectID: member.deployedObjectID,
		Position: game.Vec3(*req.Position), Orientation: game.Quaternion{W: 1}})
	if err != nil {
		return result, fmt.Errorf("placePackets: %w", err)
	}
	err = member.teleportPlayer(e.now(), raknet.Vector3(*req.Position))
	if err != nil {
		return result, fmt.Errorf("placePosition: %w", err)
	}
	interrupted := member.interruptBasicForMovement()
	if interrupted != nil {
		interrupted.Stop()
	}
	for key, candidate := range e.sessions {
		if candidate.binding.UserID == member.binding.UserID && candidate.generation == member.generation {
			e.sessions[key] = member
			break
		}
	}
	e.queueFieldMedicRealmPacketsLocked(nil, req.GameID, packets)
	return scenario.HeroResetReceipt{GameID: req.GameID, ObjectID: req.ObjectID,
		Noun: member.binding.Creatures[member.deployedCreatureIndex].Noun, Index: member.deployedCreatureIndex,
		HitPoint: member.deployedHitPoint(), ManaPoint: member.deployedManaPoint(),
		RecipientCount: recipientCount, IsPlacement: true, IsQueued: true}, nil
}

func scenarioHeroResetPackets(member gameplayPeerSession, hitPoint, manaPoint float32, isReplacement bool) ([][]byte, error) {
	index := member.deployedCreatureIndex
	objectID := member.deployedObjectID
	messages := make([]raknet.ApplicationMessage, 0, 12)
	packets := make([][]byte, 0, 12)
	if isReplacement {
		statusPacket, err := scenarioHeroRosterPacket(member)
		if err != nil {
			return nil, fmt.Errorf("resetRoster: %w", err)
		}
		packets = append(packets, statusPacket)
		creature := member.binding.Creatures[index]
		messages = append(messages, raknet.ObjectCreateMessage{
			ObjectID: objectID, Noun: creature.Noun, AssetID: zonehero.AppearanceAsset(creature),
			PositionX: member.playerPosition.X, PositionY: member.playerPosition.Y, PositionZ: member.playerPosition.Z,
			Scale: 1, Team: uint8(member.binding.Team), PlayerIndex: uint8(member.binding.Slot),
			IsCollisionEnabled: true, IsPlayerControlled: true,
		})
		messages = append(messages, campaignHeroResourceStateMessages(objectID, creature)...)
		if creature.PassiveAbility != 0 && member.passiveModifierInstance[index] != 0 {
			messages = append(messages, raknet.ModifierCreatedMessage{TargetID: objectID,
				ModifierGUID: creature.PassiveAbility, InstanceID: member.passiveModifierInstance[index], StackCount: 1, SourceID: objectID})
		}
		messages = append(messages,
			raknet.LabsPlayerControlledObjectMessage{Slot: uint8(member.binding.Slot), ObjectID: objectID},
			raknet.PlayerCharacterDeployMessage{PlayerIndex: uint8(member.binding.Slot), CreatureIndex: index, ObjectID: objectID},
			raknet.LabsPlayerDeployCooldownMessage{PlayerSlot: uint8(member.binding.Slot), DeployedCreatureIndex: index},
			raknet.ObjectUpdateMessage{ObjectID: objectID, PositionX: member.playerPosition.X, PositionY: member.playerPosition.Y, PositionZ: member.playerPosition.Z, IsVisible: true},
		)
	}
	maximumHitPoint, maximumManaPoint := member.characterResourceMaximum(index)
	if isReplacement {
		maximumHitPoint, maximumManaPoint = hitPoint, manaPoint
	}
	messages = append(messages,
		raknet.CombatantDataDeltaMessage{ObjectID: objectID, HitPoints: hitPoint, ManaPoints: manaPoint, IsHitPointChanged: true, IsManaPointChanged: true},
		raknet.LabsPlayerCharacterResourceMessage{PlayerSlot: uint8(member.binding.Slot), CreatureIndex: index,
			Resource: raknet.LabsCharacterResource{Health: hitPoint, MaxHealth: maximumHitPoint, Mana: manaPoint, MaxMana: maximumManaPoint}},
	)
	for _, message := range messages {
		packet, err := raknet.MarshalApplication(message)
		if err != nil {
			return nil, fmt.Errorf("resetMessage: %w", err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}
