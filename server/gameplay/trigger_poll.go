package gameplay

import (
	"fmt"
	"slices"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zoneprojection "github.com/darkspinnet/darkspin/server/zone/projection"
)

// The caller holds the registry lock. Positions are sampled at the same time,
// including peers who are still travelling along their accepted movement.
func (e *gameplaySessionRegistry) advanceCampaignTriggersLocked(
	actor gameplayPeerSession, previous game.Vec3, current game.Vec3, now time.Time,
) ([]game.CampaignDirectorPublication, error) {
	if actor.binding.Mode != game.ModeChain {
		publications, err := actor.zone.AdvanceDirector(previous, current)
		if err != nil {
			return nil, fmt.Errorf("triggerMovement: %w", err)
		}
		return publications, nil
	}
	participants := make([]game.CampaignTriggerParticipant, 0)
	for _, candidate := range e.sessions {
		if candidate.binding.UserID == actor.binding.UserID {
			candidate = actor
		}
		if candidate.zone != actor.zone || !isCampaignTriggerParticipant(candidate) {
			continue
		}
		position := game.Vec3(candidate.playerPosition)
		if candidate.binding.UserID == actor.binding.UserID {
			position = current
		} else if candidate.playerMotion != nil {
			sampledPosition, err := candidate.playerMotion.SamplePosition(now)
			if err != nil {
				return nil, fmt.Errorf("triggerPosition: %w", err)
			}
			position = game.Vec3(sampledPosition)
		}
		isReplay, isMember := actor.zone.IsMemberReplay(candidate.binding.UserID)
		if !isMember {
			continue
		}
		participants = append(participants, game.CampaignTriggerParticipant{
			Slot: candidate.binding.Slot, ObjectID: candidate.deployedObjectID,
			Position: position, IsLevelBeaten: isReplay,
		})
	}
	slices.SortFunc(participants, func(a, b game.CampaignTriggerParticipant) int {
		return int(a.Slot) - int(b.Slot)
	})
	publications, err := actor.zone.AdvanceDirectorParticipants(game.CampaignTriggerRequest{
		ActorObjectID: actor.deployedObjectID, Previous: previous, Current: current,
		Elapsed: actor.zone.Elapsed(now), Participants: participants,
	})
	if err != nil {
		return nil, fmt.Errorf("triggerAdvance: %w", err)
	}
	return publications, nil
}

func isCampaignTriggerParticipant(e gameplayPeerSession) bool {
	return e.zone != nil && e.binding.Mode == game.ModeChain && e.stage.IsDungeon() &&
		e.dungeonSetup.IsCommitted() && !e.isRejoinPending && !e.isZoneTerminal() &&
		!e.isHeroSelectionPending && e.deployedObjectID != 0 && e.deployedHitPoint() > 0
}

// Polling uses the same encounter consumer as movement. A party standing in an
// arena therefore finishes dwell without needing to issue another move command.
func (e gameplayPendingRuntime) pollCampaignTriggers(packet raknet.Packet) ([][]byte, error) {
	sessionKey := packet.Address.String()
	e.registry.mutex.Lock()
	peerSession, isFound := e.registry.sessions[sessionKey]
	if !isFound || peerSession.transportGeneration != packet.TransportGeneration ||
		peerSession.zone == nil || peerSession.binding.Mode != game.ModeChain ||
		!peerSession.stage.IsDungeon() || !peerSession.dungeonSetup.IsCommitted() ||
		peerSession.isRejoinPending || peerSession.isZoneTerminal() {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	now := e.now()
	position := game.Vec3(peerSession.playerPosition)
	if peerSession.playerMotion != nil {
		sampledPosition, err := peerSession.playerMotion.SamplePosition(now)
		if err != nil {
			e.registry.mutex.Unlock()
			return nil, fmt.Errorf("triggerPollPosition: %w", err)
		}
		position = game.Vec3(sampledPosition)
	}
	publications, err := e.registry.advanceCampaignTriggersLocked(peerSession, position, position, now)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("triggerPollAdvance: %w", err)
	}
	if !isCampaignTriggerParticipant(peerSession) {
		// Still sample party occupancy when every hero is down or switching.
		// An absent actor receives no publications, but cannot accrue dwell.
		e.registry.mutex.Unlock()
		return nil, nil
	}
	if len(publications) == 0 {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	runtime := e.action.movement.campaign.encounter
	peerSession.tutorialActivationRuntime = &runtime
	peerSession.tutorialActivationSourceTime = packet.SourceTime
	encounter, err := runtime.acceptTriggerPublicationsLocked(packet, &peerSession,
		campaignEncounterAdvance{current: position, publications: publications})
	e.registry.sessions[sessionKey] = peerSession
	e.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("triggerPollAccept: %w", err)
	}
	if len(encounter.hordePlans) > 0 {
		err = peerSession.zone.PublishNPCSpawn(zoneprojection.NPCSpawn{
			Plans: encounter.hordePlans, TargetObjectID: peerSession.deployedObjectID,
		}, peerSession.binding.UserID, peerSession.generation)
		if err != nil {
			return nil, fmt.Errorf("triggerPollSpawn: %w", err)
		}
	}
	firstPackets, err := e.action.movement.campaign.npc.scheduleFirstActionsWithIntroductions(
		packet, sessionKey, peerSession.generation, encounter.hordePlans, packet.SourceTime, nil,
	)
	if err != nil {
		return nil, fmt.Errorf("triggerPollAction: %w", err)
	}
	unlockPackets, bossPackets, err := runtime.schedulePublications(packet, peerSession, encounter)
	if err != nil {
		return nil, fmt.Errorf("triggerPollSchedule: %w", err)
	}
	err = runtime.scheduleNamedBossTriggers(sessionKey, peerSession, publications)
	if err != nil {
		return nil, fmt.Errorf("triggerPollBoss: %w", err)
	}
	packets := append(encounter.hordePackets, encounter.unlockPackets...)
	packets = append(packets, firstPackets...)
	packets = append(packets, unlockPackets...)
	packets = append(packets, bossPackets...)
	err = publishCampaignPeersAfterCommit(e.registry, packet, packets)
	if err != nil {
		return nil, fmt.Errorf("triggerPollPublish: %w", err)
	}
	return packets, nil
}
