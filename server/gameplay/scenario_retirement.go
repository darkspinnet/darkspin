//go:build scenario

package gameplay

import (
	"context"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/scenario"
	"github.com/darkspinnet/darkspin/server/squad"
)

// A retirement survives a canceled observer wait. Only the same owned actor,
// transport and requested noun may complete it; no cancellation recreates it.
type scenarioHeroRetirement struct {
	userID              uint64
	gameID              uint32
	objectID            uint32
	index               uint32
	noun                uint32
	peerGeneration      uint64
	transportGeneration uint64
	squad               *squad.Session
}

func (e *scenarioHeroRetirement) matches(member gameplayPeerSession, req scenario.HeroResetRequest) bool {
	return e.userID == member.binding.UserID && e.gameID == req.GameID &&
		e.objectID == req.ObjectID && e.index == member.deployedCreatureIndex && e.noun == req.Noun &&
		e.peerGeneration == req.PeerGeneration && e.transportGeneration == req.TransportGeneration &&
		e.squad == member.squad
}

func scenarioHeroRosterPacket(member gameplayPeerSession) ([]byte, error) {
	rosterBinding := member.binding
	for index := range rosterBinding.Creatures {
		if uint32(index) == member.deployedCreatureIndex {
			continue
		}
		character, isFound := member.squad.Character(uint32(index))
		if !isFound {
			return nil, fmt.Errorf("rosterCharacter: unavailable")
		}
		maximumHealth, maximumPower := member.characterResourceMaximum(uint32(index))
		rosterBinding.Creatures[index].HitPoint = character.HitPoints
		rosterBinding.Creatures[index].PowerPoint = character.ManaPoints
		rosterBinding.Creatures[index].MaximumHitPoint = maximumHealth
		rosterBinding.Creatures[index].MaximumPowerPoint = maximumPower
	}
	packet, err := marshalCampaignPlayer(rosterBinding, raknet.PlayerStatus{}, member.overdriveEnergy, false, true)
	if err != nil {
		return nil, fmt.Errorf("rosterMarshal: %w", err)
	}
	return packet, nil
}

func (e *gameplaySessionRegistry) retireScenarioHeroLocked(ctx context.Context, member gameplayPeerSession, req scenario.HeroResetRequest, creature game.GameplayCreature, recipientCount int) (scenario.HeroResetReceipt, error) {
	deletePacket, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{ObjectID: []uint32{req.ObjectID}})
	if err != nil {
		return scenario.HeroResetReceipt{}, fmt.Errorf("retireDelete: %w", err)
	}
	state := member.squad.State()
	for index := range state.Characters {
		if state.Characters[index].IsAvailable {
			state.Characters[index].HitPoints = min(state.Characters[index].HitPoints, member.binding.Creatures[index].HitPoint)
		}
	}
	fencedSquad, err := squad.Restore(state)
	if err != nil {
		return scenario.HeroResetReceipt{}, fmt.Errorf("retireSquad: %w", err)
	}
	preview := member
	preview.binding.Creatures[member.deployedCreatureIndex] = creature
	preview.squad = fencedSquad
	preview.arenaHitPointIncreases = [squad.Size]float32{}
	rosterPacket, err := scenarioHeroRosterPacket(preview)
	if err != nil {
		return scenario.HeroResetReceipt{}, fmt.Errorf("retireRoster: %w", err)
	}
	previousBatchID := member.nextPendingPacketID
	previousBatchIDs := e.scenarioCleanupBatchIDsLocked(req.GameID)
	member.retireArenaHitStatuses()
	if member.sphereAttack != nil {
		// Ordinary rejection preserves accepted effect-owned targeted areas.
		// Retirement ends their owner, so cancel those producers explicitly too.
		spherePackets, sphereErr := member.sphereAttack.CancelPackets(ctx)
		member.sphereAttack.Stop()
		member.sphereAttack = nil
		member.sphereObjectID = 0
		member.queuePackets(spherePackets)
		if sphereErr != nil && e.logger != nil {
			e.logger.Printf("Scenario hero retirement area cleanup user=%d: %v", member.binding.UserID, sphereErr)
		}
	}
	e.retireFieldMedicBuffsLocked(&member, e.scenarioModifierPool)
	e.retirePlasmaWreathLocked(&member)
	e.retireCampaignTreeOfLifeLocked(&member)
	member.stopHardenedMantle()
	soulPackets, soulErr := member.clearSoulRavagerPresentation()
	if soulErr != nil && e.logger != nil {
		e.logger.Printf("Scenario hero retirement soul cleanup user=%d: %v", member.binding.UserID, soulErr)
	}
	member.queuePackets(soulPackets)
	interrupted := member.resetFailedActionAdmission(e.scenarioModifierPool, e.scenarioEffectPool)
	if interrupted != nil {
		interrupted.Stop()
	}
	member.queuePackets(member.takeHeroInfections())
	// Fencing the squad invalidates retained projectile/burst/cast identities
	// before the deletion can be sent. Canceled schedules cannot re-publish it.
	member.squad = fencedSquad
	member.scenarioAbility.last = nil
	member.scenarioAbility.rejection = nil
	member.scenarioAbility.isCaptureUnavailable = true
	member.scenarioAbility.retirement = &scenarioHeroRetirement{
		userID: member.binding.UserID, gameID: req.GameID, objectID: req.ObjectID,
		index: member.deployedCreatureIndex, noun: req.Noun, peerGeneration: req.PeerGeneration,
		transportGeneration: req.TransportGeneration, squad: fencedSquad,
	}
	member.heroInputLockedObjectID = req.ObjectID
	member.heroInputLockedUntil = e.now().Add(30 * time.Second)
	member.isHeroSelectionPending = false
	member.isHeroSelectionScheduled = false
	member.heroSelectionReadyAt = time.Time{}
	cleanupPackets := make([][]byte, 0)
	for _, batch := range member.pendingPacketBatches {
		if batch.id > previousBatchID && batch.channelCreation == nil && batch.channelRemoval == nil {
			cleanupPackets = append(cleanupPackets, batch.packets...)
		}
	}
	for key, candidate := range e.sessions {
		if candidate.binding.UserID == member.binding.UserID && candidate.generation == member.generation {
			e.clearActionLeasesLocked(key, member.transportGeneration)
			e.sessions[key] = member
			break
		}
	}
	e.queueScenarioCleanupLocked(req.GameID, member.binding.UserID, cleanupPackets, previousBatchIDs)
	e.commitChannelRemovalsLocked()
	// Publish appearance metadata while the old object is absent. ObjectCreate
	// remains deferred until the host observes each owned client's frame fence.
	e.queueFieldMedicRealmPacketsLocked(nil, req.GameID, [][]byte{deletePacket, rosterPacket})
	return scenario.HeroResetReceipt{GameID: req.GameID, ObjectID: req.ObjectID, Noun: req.Noun,
		Index: member.deployedCreatureIndex, HitPoint: member.deployedHitPoint(), ManaPoint: member.deployedManaPoint(),
		RecipientCount: recipientCount, IsRetirement: true, IsQueued: true}, nil
}
