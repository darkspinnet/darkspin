package gameplay

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	"github.com/darkspinnet/darkspin/server/zone"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	actionraknet "github.com/darkspinnet/darkspin/server/zone/action/raknet103"
	zonecheckpoint "github.com/darkspinnet/darkspin/server/zone/checkpoint"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
	zonequest "github.com/darkspinnet/darkspin/server/zone/quest"
)

func (e gameplayPendingRuntime) publishQuestCrystals(ctx context.Context, packet raknet.Packet, sessionKey string) error {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || member.zone == nil || member.transportGeneration != packet.TransportGeneration {
		return nil
	}
	world := member.zone
	prepareErr := world.PrepareQuestCrystals(ctx, campaignDropProjector{mesh: world.Navigation()}, packet.SourceTime)
	for _, state := range world.QuestCrystals().Snapshots(e.now()) {
		if state.IsCollected || state.IsPublished {
			continue
		}
		crystal, isCrystalFound := world.QuestCrystals().Crystal(state.ObjectID, e.now())
		if !isCrystalFound {
			continue
		}
		e.registry.queueQuestFrameLocked(world, crystal.ObjectID, false)
		world.QuestCrystals().Publish(state.Request.ID)
	}
	if prepareErr != nil {
		return fmt.Errorf("questPrepare: %w", prepareErr)
	}
	return nil
}

type questCrystalPickup struct {
	runtime       campaignInteractionRuntime
	world         *zone.Zone
	identity      gameplayProducerIdentity
	actorObjectID uint32
	creatureIndex uint32
	deathRevision uint64
	claim         zonequest.Claim
	schedule      *zoneaction.ScheduleSession
	releasePacket []byte
}

func (e *questCrystalPickup) Cleanup() { e.world.QuestCrystals().Release(e.claim) }
func (e *questCrystalPickup) Stop()    { e.Cleanup() }

func (e *questCrystalPickup) current(member gameplayPeerSession, isFound bool) bool {
	return isFound && member.zone == e.world &&
		gameplayProducerIdentityFromSession(e.identity.sessionKey, member, true) == e.identity &&
		member.deployedObjectID == e.actorObjectID && member.deployedCreatureIndex == e.creatureIndex &&
		member.voodooDeathRevision == e.deathRevision && member.isPickupActorAvailable()
}

func (e *questCrystalPickup) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	member, isFound := e.runtime.registry.sessions[e.identity.sessionKey]
	if !e.current(member, isFound) {
		e.Cleanup()
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	crystal, isCrystalFound := e.world.QuestCrystals().Crystal(e.claim.ObjectID, e.runtime.now())
	maximumDistance := questCrystalDistance(member)
	if !isCrystalFound || zoneability.Distance(game.Vec3(member.playerPosition), game.Vec3(crystal.Flight.Position)) > maximumDistance {
		e.Cleanup()
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	isCollected, err := e.world.CollectQuestCrystal(e.world.Context(), e.claim)
	if err != nil {
		e.Cleanup()
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("questPickup: %w", err)
	}
	if isCollected {
		e.runtime.registry.queueQuestFrameLocked(e.world, e.claim.ObjectID, true)
	}
	e.runtime.registry.mutex.Unlock()
	if isCollected {
		e.world.SaveCheckpointIfSafe(zonecheckpoint.ReasonSafePickup)
	}
	return nil, nil
}

func (e *questCrystalPickup) finish() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	member, isFound := e.runtime.registry.sessions[e.identity.sessionKey]
	if !e.current(member, isFound) {
		e.schedule.Remove(zoneaction.ScheduleQuestCrystal, e.claim.ObjectID, e)
		e.Cleanup()
		return nil, nil
	}
	e.schedule.Remove(zoneaction.ScheduleQuestCrystal, e.claim.ObjectID, e)
	e.Cleanup()
	e.runtime.registry.sessions[e.identity.sessionKey] = member
	return [][]byte{e.releasePacket}, nil
}

func (e *questCrystalPickup) failed(err error) {
	e.schedule.Remove(zoneaction.ScheduleQuestCrystal, e.claim.ObjectID, e)
	e.Cleanup()
	if e.runtime.logger != nil {
		e.runtime.logger.Printf("Quest crystal pickup schedule failed: %v", err)
	}
}

func questCrystalDistance(member gameplayPeerSession) float32 {
	maximumDistance := zonequest.PickupRange + max(float32(0), member.deployedCampaignFootprintRadius())
	footprint, isFound := member.zone.DirectorDefinition().NavigationFootprintForAsset(zonequest.CrystalNoun)
	if !isFound {
		return maximumDistance
	}
	radius, err := footprint.ActorRadius(1)
	if err != nil {
		log.Printf("Quest crystal noun radius unavailable: %v", err)
		return maximumDistance
	}
	return maximumDistance + radius
}

// The caller holds registry.mutex; this method releases it on every exit.
func (e campaignInteractionRuntime) beginQuestCrystalPickupLocked(packet raknet.Packet, command raknet.ActionCommandData, sessionKey string, member gameplayPeerSession, crystal zonequest.Crystal) ([][]byte, error) {
	defer e.registry.mutex.Unlock()
	if !member.isPickupActorAvailable() {
		return e.rejectPickup(command, "quest pickup actor unavailable")
	}
	distance := questCrystalDistance(member)
	if zoneability.Distance(game.Vec3(member.playerPosition), game.Vec3(crystal.Flight.Position)) > distance {
		return e.rejectPickup(command, "quest crystal out of range")
	}
	claim, isClaimed := member.zone.QuestCrystals().Claim(crystal.ObjectID)
	if !isClaimed {
		return e.rejectPickup(command, "quest crystal already claimed")
	}
	step := &questCrystalPickup{runtime: e, world: member.zone,
		identity:      gameplayProducerIdentityFromSession(sessionKey, member, true),
		actorObjectID: member.deployedObjectID, creatureIndex: member.deployedCreatureIndex,
		deathRevision: member.voodooDeathRevision, claim: claim, schedule: member.campaignScheduleSession()}
	if packet.ScheduleGroupResult == nil && packet.ScheduleGroup == nil {
		step.Cleanup()
		return e.rejectPickup(command, "quest pickup scheduler unavailable")
	}
	accept, err := actionraknet.Accept(command, zonequest.PickupAbility, packet.SourceTime, zonequest.CastTime, zonequest.ReleaseTime)
	if err != nil {
		step.Cleanup()
		return nil, fmt.Errorf("questAccept: %w", err)
	}
	abilityIndex := uint32(0)
	if command.Ability != nil {
		abilityIndex = command.Ability.Index
	}
	step.releasePacket, err = abilityraknet.ReleaseResponse(command.Common.Unknown[0], util.HashID(zonequest.PickupAbility), abilityIndex, packet.SourceTime, zonequest.CastTime, zonequest.ReleaseTime)
	if err != nil {
		step.Cleanup()
		return nil, fmt.Errorf("questRelease: %w", err)
	}
	animation, err := npcraknet.AnimationState(member.deployedObjectID, "pickup_quest_crystal", packet.SourceTime)
	if err != nil {
		step.Cleanup()
		return nil, fmt.Errorf("questAnimation: %w", err)
	}
	movementPackets, err := member.stopCampaignPickup(e.now())
	if err != nil {
		step.Cleanup()
		return nil, fmt.Errorf("questStop: %w", err)
	}
	producers := []raknet.ScheduledPacketProducer{{Delay: zonequest.CastTime, Produce: step.produce}, {Delay: zonequest.ReleaseTime, Produce: step.finish}}
	var cancel raknet.CancelSchedule
	if packet.ScheduleGroupResult != nil {
		cancel, err = packet.ScheduleGroupResult(producers, step.failed)
	} else {
		cancel, err = packet.ScheduleGroup(producers)
	}
	if err != nil || cancel == nil {
		if cancel != nil {
			cancel()
		}
		step.Cleanup()
		if err == nil {
			err = errors.New("nil quest cancellation")
		}
		return nil, fmt.Errorf("questSchedule: %w", err)
	}
	err = step.schedule.Add(zoneaction.ScheduleQuestCrystal, crystal.ObjectID, step, cancel, step)
	if err != nil {
		cancel()
		step.Cleanup()
		return nil, fmt.Errorf("questTrack: %w", err)
	}
	e.registry.sessions[sessionKey] = member
	for key, recipient := range e.registry.sessions {
		if recipient.zone != member.zone || recipient.isRejoinPending {
			continue
		}
		queueErr := recipient.queueCampaignPresentation([][]byte{animation})
		if queueErr != nil && e.logger != nil {
			e.logger.Printf("Quest pickup animation retained: %v", queueErr)
		}
		e.registry.sessions[key] = recipient
	}
	return append([][]byte{accept}, movementPackets...), nil
}
