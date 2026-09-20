package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/squad"
	heroraknet "github.com/darkspinnet/darkspin/server/zone/hero/raknet103"
)

// A recap belongs to the active party, including squads whose final hero died.
// Each owner needs its private squad/control updates; all peers need the revived
// actor's presentation. Keep both deliveries in the reliable pending queue.
func (e gameplayPendingRuntime) recapParty(
	packet raknet.Packet, queuedSession gameplayPeerSession,
) ([][]byte, bool, error) {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	caller, isFound := e.registry.sessions[packet.Address.String()]
	if !isFound || caller.generation != queuedSession.generation ||
		caller.transportGeneration != queuedSession.transportGeneration ||
		caller.binding.Mode != game.ModeChain || caller.zone == nil ||
		caller.isZoneTerminal() {
		return nil, false, errors.New("recap party unavailable")
	}
	for sessionKey, member := range e.registry.sessions {
		if member.zone != caller.zone || member.binding.GameID != caller.binding.GameID ||
			member.isRejoinPending || !member.stage.IsDungeon() ||
			!member.dungeonSetup.IsCommitted() || member.squad == nil ||
			member.deployedObjectID == 0 {
			continue
		}
		next, packets, err := member.prepareRecap(packet.SourceTime)
		if err != nil {
			return nil, false, fmt.Errorf("recapPrepare: %w", err)
		}
		if len(packets) == 0 {
			continue
		}
		err = next.syncZoneSquadCheckpoint()
		if err != nil {
			return nil, false, fmt.Errorf("recapCheckpoint: %w", err)
		}
		if member.deployedHitPoint() <= 0 {
			err = next.syncZoneHero()
			if err != nil {
				rollbackErr := member.syncZoneSquadCheckpoint()
				if rollbackErr != nil {
					return nil, false, fmt.Errorf("recapRollback: %w", errors.Join(err, rollbackErr))
				}
				return nil, false, fmt.Errorf("recapHero: %w", err)
			}
			next.clearEnemyHeroStatuses()
			next.isNPCRecoveryPending = true
			e.registry.clearActionLeasesLocked(sessionKey, next.transportGeneration)
		}
		e.registry.sessions[sessionKey] = next
		sharedPackets := gameplayPeerPresentationPackets(packets)
		for targetKey, target := range e.registry.sessions {
			if target.zone != caller.zone || target.isRejoinPending ||
				!target.stage.IsDungeon() || !target.dungeonSetup.IsCommitted() {
				continue
			}
			targetPackets := sharedPackets
			if targetKey == sessionKey {
				targetPackets = packets
			}
			queueErr := target.queueCampaignPresentation(targetPackets)
			e.registry.sessions[targetKey] = target
			if queueErr != nil {
				// queueCampaignPresentation retains these packets on failure.
				e.logger.Printf("RakNet recap presentation queued for retry user=%d: %v",
					target.binding.UserID, queueErr)
			}
		}
	}
	e.logger.Printf("RakNet developer recap resurrected fallen party heroes for %s", packet.Address)
	return nil, true, nil
}

// Prepare against a copied squad so invalid resources or packet encoding cannot
// leave a half-resurrected squad behind. Existing living heroes remain unchanged.
func (e gameplayPeerSession) prepareRecap(timestamp uint64) (gameplayPeerSession, [][]byte, error) {
	next := e
	nextSquad := *e.squad
	next.squad = &nextSquad
	maximumHitPoints := [squad.Size]float32{}
	for index := uint32(0); index < squad.Size; index++ {
		maximumHitPoints[index] = e.characterHitPointMaximum(index)
	}
	indexes, err := next.squad.Resurrect(maximumHitPoints)
	if err != nil {
		return e, nil, fmt.Errorf("recapSquad: %w", err)
	}
	packets := make([][]byte, 0)
	for _, index := range indexes {
		character, isFound := next.squad.Character(index)
		if !isFound {
			return e, nil, errors.New("recap character unavailable")
		}
		resource, marshalErr := next.marshalCampaignCharacterResourceValues(
			index, character.HitPoints, character.ManaPoints,
		)
		if marshalErr != nil {
			return e, nil, fmt.Errorf("recapResource: %w", marshalErr)
		}
		packets = append(packets, resource)
	}
	if len(indexes) == 0 || e.deployedHitPoint() > 0 {
		return next, packets, nil
	}
	next.isHeroSelectionPending = false
	next.isHeroSelectionScheduled = false
	next.heroSelectionReadyAt = time.Time{}
	next.heroInputLockedObjectID = 0
	next.heroInputLockedUntil = time.Time{}
	next.isPartyDefeatQueued = false
	next.squad.ResetDeployCooldown()
	messages := []raknet.ApplicationMessage{
		raknet.CombatantDataUpdateMessage{
			ObjectID: next.deployedObjectID, HitPoints: next.deployedHitPoint(),
			ManaPoints: next.deployedManaPoint(),
		},
		raknet.LabsPlayerControlledObjectMessage{
			Slot: uint8(next.binding.Slot), ObjectID: next.deployedObjectID,
		},
		raknet.PlayerCharacterDeployMessage{
			PlayerIndex: uint8(next.binding.Slot), CreatureIndex: next.deployedCreatureIndex,
			ObjectID: next.deployedObjectID,
		},
		raknet.LabsPlayerDeployCooldownMessage{
			PlayerSlot: uint8(next.binding.Slot), DeployedCreatureIndex: next.deployedCreatureIndex,
		},
	}
	for _, message := range messages {
		encoded, marshalErr := raknet.MarshalApplication(message)
		if marshalErr != nil {
			return e, nil, fmt.Errorf("recapDeploy: %w", marshalErr)
		}
		packets = append(packets, encoded)
	}
	beamPackets, err := heroraknet.BeamIn(next.deployedObjectID,
		campaignCharacterBeam(next.binding.Creatures[next.deployedCreatureIndex], true),
		zonePosition(next.playerPosition), timestamp,
	)
	if err != nil {
		return e, nil, fmt.Errorf("recapBeam: %w", err)
	}
	return next, append(packets, beamPackets...), nil
}
