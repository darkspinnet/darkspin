package gameplay

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/zone"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
	zoneprojection "github.com/darkspinnet/darkspin/server/zone/projection"
)

type orcusFollower struct {
	readyAt          time.Time
	slot             uint8
	stopPackets      [][]byte
	isEffectAttached bool
}

type orcusConsumeRun struct {
	zone               *zone.Zone
	request            campaignOrcusRequest
	actionGeneration   uint64
	profile            zonenpc.ActionProfile
	followers          map[uint32]orcusFollower
	reductionExpiresAt time.Time
	isFinished         bool
}

func (e campaignNPCActionRuntime) produceOrcusConsume(packet raknet.Packet,
	sessionKey string, generation uint64, objectID uint32, timestamp uint64,
) ([][]byte, bool, error) {
	e.registry.mutex.Lock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || !member.isCampaignNPCSourceActive(generation, objectID) {
		e.registry.mutex.Unlock()
		return nil, false, nil
	}
	boss, isBossFound := member.zone.NPCs().NPC(objectID)
	profile, isProfileFound := zonenpc.OrcusConsumeProfile(boss.Plan.NounName)
	definition, isDefinitionFound := zonenpc.OrcusSpawnProfile(boss.Plan.NounName)
	if !isBossFound || !isProfileFound || !isDefinitionFound || boss.IsDefeated ||
		member.zone.NPCs().SilenceRemaining(objectID, e.now()) > 0 {
		e.registry.mutex.Unlock()
		return nil, false, nil
	}
	authoredProfile, isAuthoredProfileFound := member.zone.DirectorDefinition().NPCProfilesByNoun[strings.ToLower(definition.ServantNoun)]
	if !isAuthoredProfileFound || !authoredProfile.IsKnown || authoredProfile.HitPoint <= 0 {
		e.registry.mutex.Unlock()
		return nil, true, fmt.Errorf("consumeClass: unavailable %q", definition.ServantNoun)
	}
	band, isClaimed := member.zone.NPCs().ClaimOrcusConsume(objectID)
	if !isClaimed {
		e.registry.mutex.Unlock()
		return nil, false, nil
	}
	definition.ServantProfile = authoredProfile
	run := &orcusConsumeRun{zone: member.zone, request: campaignOrcusRequest{runtime: e, packet: packet.Autonomous(),
		sessionKey: sessionKey, generation: generation, objectID: objectID, timestamp: timestamp},
		actionGeneration: boss.ActionGeneration, profile: profile, followers: make(map[uint32]orcusFollower)}
	packets, err := run.spawnRing(&member, boss, definition)
	if err != nil {
		member.zone.NPCs().RollbackOrcusConsume(objectID, band)
		e.registry.mutex.Unlock()
		return nil, true, fmt.Errorf("consumeRing: %w", err)
	}
	e.registry.sessions[sessionKey] = member
	if member.campaignOrcusConsumeRuns == nil {
		member.campaignOrcusConsumeRuns = make(map[uint32]*orcusConsumeRun)
	}
	member.campaignOrcusConsumeRuns[objectID] = run
	e.registry.sessions[sessionKey] = member
	for _, servant := range member.zone.NPCs().OwnedActiveCandidates(objectID, math.MaxFloat32) {
		member.zone.NPCs().SetOrcusFollowing(servant.Plan.ObjectID, true)
		follower := run.followers[servant.Plan.ObjectID]
		startPacket, stopPackets, slot, effectErr := preparePhantomChargeEffect(e.effectPool,
			servant.Plan.ObjectID, "status_mesmerized.ServerEventDef")
		if effectErr != nil {
			e.logger.Printf("Orcus follow effect omitted servant=%d: %v", servant.Plan.ObjectID, effectErr)
		} else {
			follower.slot = slot
			follower.stopPackets = stopPackets
			follower.isEffectAttached = true
			packets = append(packets, startPacket)
		}
		run.followers[servant.Plan.ObjectID] = follower
	}
	run.reductionExpiresAt = e.now().Add(time.Second)
	err = member.zone.NPCs().ApplyDamageReduction(objectID, .75, run.reductionExpiresAt)
	e.registry.mutex.Unlock()
	if err != nil {
		run.finish()
		return nil, true, fmt.Errorf("consumeShield: %w", err)
	}
	shieldPacket, err := npcraknet.ShieldEffectAsset(objectID, 0, "verdanth_boss_shield.ServerEventDef", false)
	if err != nil {
		run.finish()
		return nil, true, fmt.Errorf("consumeShieldPacket: %w", err)
	}
	animationPacket, err := npcraknet.AnimationState(objectID, "ver_boss_lf_spawneater_channel_spawn", timestamp)
	if err != nil {
		run.finish()
		return nil, true, fmt.Errorf("consumeChannel: %w", err)
	}
	packets = append(packets, shieldPacket, animationPacket)
	run.request.timestamp += 2500
	cancel, err := scheduleNPCProducers(e.registry, run.request.packet, []raknet.ScheduledPacketProducer{
		{Delay: 250 * time.Millisecond, Produce: run.follow},
		{Delay: 2500 * time.Millisecond, Produce: run.eat},
	})
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		run.finish()
		return nil, true, fmt.Errorf("consumeSchedule: %w", err)
	}
	return packets, true, nil
}

func (e *orcusConsumeRun) spawnRing(member *gameplayPeerSession, boss zonenpc.Snapshot,
	definition zonenpc.OrcusSpawnDefinition,
) ([][]byte, error) {
	// Consume's six/eight/ten-spawn ring is independent of ordinary Spawn's cap.
	count := 6
	switch strings.ToLower(boss.Plan.NounName) {
	case "verdanthboss_2.noun":
		count = 8
	case "verdanthboss_3.noun":
		count = 10
	}
	firstID, err := member.zone.ReserveObjectIDs(uint32(count))
	if err != nil {
		return nil, fmt.Errorf("ringReserve: %w", err)
	}
	plans := make([]zonenpc.SpawnPlan, 0, count)
	packets := make([][]byte, 0, count*6)
	for index := 0; index < count; index++ {
		angle := float64(index) * 2 * math.Pi / float64(count)
		position := boss.Plan.Position
		position.X += float32(25 * math.Cos(angle))
		position.Y += float32(25 * math.Sin(angle))
		projected, isProjected, projectErr := zonenavigation.ProjectPosition(member.zone.Navigation(), position, definition.ServantProfile.FootprintRadius)
		if projectErr != nil {
			return nil, fmt.Errorf("ringProject: %w", projectErr)
		}
		if member.zone.Navigation() != nil && !isProjected {
			continue
		}
		if isProjected {
			position = projected
		}
		plan := zonenpc.SpawnPlan{ObjectID: firstID + uint32(index), OwnerObjectID: e.request.objectID,
			NounName: definition.ServantNoun, Position: position, IsEncounterAuxiliary: true,
			NPCProfile: definition.ServantProfile, ActionProfile: definition.ServantAction,
			IsActionKnown: true, IsIntroductionComplete: true}
		spawnPackets, spawnErr := npcraknet.TargetedSpawn(plan, boss.TargetObjectID)
		if spawnErr != nil {
			return nil, fmt.Errorf("ringSpawnPacket: %w", spawnErr)
		}
		animationPacket, animationErr := npcraknet.AnimationState(plan.ObjectID,
			definition.ServantAction.FirstAggroAnimationName, e.request.timestamp)
		if animationErr != nil {
			return nil, fmt.Errorf("ringAnimation: %w", animationErr)
		}
		packets = append(packets, spawnPackets...)
		packets = append(packets, animationPacket)
		e.followers[plan.ObjectID] = orcusFollower{readyAt: e.request.runtime.now().Add(definition.ServantAction.FirstAggroDelay)}
		plans = append(plans, plan)
	}
	if len(plans) == 0 {
		return nil, errors.New("no navigable consume spawn position")
	}
	err = member.zone.NPCs().Add(plans, boss.TargetObjectID)
	if err != nil {
		return nil, fmt.Errorf("ringAdd: %w", err)
	}
	err = member.zone.PublishNPCSpawn(zoneprojection.NPCSpawn{Plans: plans, TargetObjectID: boss.TargetObjectID},
		member.binding.UserID, e.request.generation)
	if err != nil {
		rollbackErr := member.zone.NPCs().RollbackAdd(plans)
		return nil, fmt.Errorf("ringPublish: %w", errors.Join(err, rollbackErr))
	}
	return packets, nil
}

func (e *orcusConsumeRun) follow() ([][]byte, error) {
	runtime := e.request.runtime
	runtime.registry.mutex.Lock()
	member, isFound := runtime.registry.sessions[e.request.sessionKey]
	isCurrent := !e.isFinished && isFound && member.zone == e.zone && member.isCampaignNPCSourceGenerationActive(
		e.request.generation, e.request.objectID, e.actionGeneration)
	if !isCurrent {
		runtime.registry.mutex.Unlock()
		return e.finish(), nil
	}
	boss, isBossFound := member.zone.NPCs().NPC(e.request.objectID)
	if !isBossFound {
		runtime.registry.mutex.Unlock()
		return e.finish(), nil
	}
	e.reductionExpiresAt = runtime.now().Add(time.Second)
	err := member.zone.NPCs().ApplyDamageReduction(e.request.objectID, .75, e.reductionExpiresAt)
	if err != nil {
		runtime.registry.mutex.Unlock()
		e.finish()
		return nil, fmt.Errorf("consumeRenewShield: %w", err)
	}
	packets := make([][]byte, 0)
	for objectID, follower := range e.followers {
		if runtime.now().Before(follower.readyAt) {
			continue
		}
		servant, isServantFound := member.zone.NPCs().NPC(objectID)
		if !isServantFound || servant.IsDefeated || servant.HitPoint <= 0 {
			continue
		}
		speed := servant.Plan.ActionProfile.MovementSpeed * .5
		member.zone.NPCs().UpdateOrcusFollowCollision(objectID, servant.Plan.Position.Sub(boss.Plan.Position).Length() < 5)
		progress, moveErr := member.zone.NPCs().AdvancePursuit(member.zone.Navigation(), objectID,
			boss.Plan.Position, 1, speed, servant.Plan.NPCProfile.FootprintRadius, 250*time.Millisecond)
		if moveErr != nil {
			runtime.logger.Printf("Orcus following movement omitted servant=%d: %v", objectID, moveErr)
			continue
		}
		movePackets, moveErr := npcraknet.OrcusFollow(objectID, progress.Position, boss.Plan.Position)
		if moveErr != nil {
			runtime.registry.mutex.Unlock()
			e.finish()
			return nil, fmt.Errorf("consumeFollowPacket: %w", moveErr)
		}
		speedPacket, speedErr := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
			ObjectID: objectID, Value: map[uint8]float32{11: speed, 12: speed}})
		if speedErr != nil {
			runtime.registry.mutex.Unlock()
			e.finish()
			return nil, fmt.Errorf("consumeSpeed: %w", speedErr)
		}
		packets = append(packets, speedPacket)
		packets = append(packets, movePackets...)
	}
	runtime.registry.mutex.Unlock()
	err = scheduleNPCProducer(runtime.registry, e.request.packet, 250*time.Millisecond, e.follow)
	if err != nil {
		e.finish()
		return nil, fmt.Errorf("consumeFollowTick: %w", err)
	}
	return packets, nil
}

func (e *orcusConsumeRun) eat() ([][]byte, error) {
	runtime := e.request.runtime
	runtime.registry.mutex.RLock()
	member, isFound := runtime.registry.sessions[e.request.sessionKey]
	isCurrent := !e.isFinished && isFound && member.zone == e.zone && member.isCampaignNPCSourceGenerationActive(e.request.generation,
		e.request.objectID, e.actionGeneration)
	if !isCurrent {
		runtime.registry.mutex.RUnlock()
		return e.finish(), nil
	}
	// Retain the selection at the channel boundary, before the contact wait.
	candidates := member.zone.NPCs().OwnedActiveCandidates(e.request.objectID, e.profile.Radius)
	isAlive := member.zone.NPCs().OwnedActiveCount(e.request.objectID) > 0
	runtime.registry.mutex.RUnlock()
	if !isAlive {
		packets := e.finish()
		nextPackets, err := e.request.resume(e.request.timestamp)
		if err != nil {
			return nil, fmt.Errorf("consumeResume: %w", err)
		}
		return append(packets, nextPackets...), nil
	}
	if len(candidates) == 0 {
		return e.channel()
	}
	animationPacket, err := npcraknet.AnimationState(e.request.objectID, e.profile.AnimationName, e.request.timestamp)
	if err != nil {
		e.finish()
		return nil, fmt.Errorf("consumeEatAnimation: %w", err)
	}
	hit := campaignOrcusConsumeSchedule{request: e.request, actionGeneration: e.actionGeneration,
		profile: e.profile, candidates: candidates}
	cancel, err := scheduleNPCProducers(runtime.registry, e.request.packet, []raknet.ScheduledPacketProducer{
		{Delay: e.profile.HitDelay, Produce: hit.hit},
		{Delay: e.profile.ReleaseDelay + 100*time.Millisecond, Produce: e.channel},
	})
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		e.finish()
		return nil, fmt.Errorf("consumeEatSchedule: %w", err)
	}
	e.request.timestamp += uint64((e.profile.ReleaseDelay + 100*time.Millisecond) / time.Millisecond)
	return [][]byte{animationPacket}, nil
}

func (e *orcusConsumeRun) channel() ([][]byte, error) {
	e.request.runtime.registry.mutex.RLock()
	member, isFound := e.request.runtime.registry.sessions[e.request.sessionKey]
	isCurrent := !e.isFinished && isFound && member.zone == e.zone && member.isCampaignNPCSourceGenerationActive(
		e.request.generation, e.request.objectID, e.actionGeneration)
	e.request.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return e.finish(), nil
	}
	packet, err := npcraknet.AnimationState(e.request.objectID, "ver_boss_lf_spawneater_channel_spawn", e.request.timestamp)
	if err != nil {
		e.finish()
		return nil, fmt.Errorf("consumeChannelAnimation: %w", err)
	}
	err = scheduleNPCProducer(e.request.runtime.registry, e.request.packet, 2500*time.Millisecond, e.eat)
	if err != nil {
		e.finish()
		return nil, fmt.Errorf("consumeChannelWait: %w", err)
	}
	e.request.timestamp += 2500
	return [][]byte{packet}, nil
}

func (e *orcusConsumeRun) finish() [][]byte {
	runtime := e.request.runtime
	runtime.registry.mutex.Lock()
	defer runtime.registry.mutex.Unlock()
	return e.finishLocked()
}

func (e *orcusConsumeRun) finishLocked() [][]byte {
	runtime := e.request.runtime
	if e.isFinished {
		return nil
	}
	e.isFinished = true
	member, isFound := runtime.registry.sessions[e.request.sessionKey]
	if isFound && member.campaignOrcusConsumeRuns[e.request.objectID] == e {
		delete(member.campaignOrcusConsumeRuns, e.request.objectID)
	}
	packets := make([][]byte, 0)
	for objectID, follower := range e.followers {
		if follower.isEffectAttached {
			runtime.effectPool.Release(objectID, follower.slot)
			packets = append(packets, follower.stopPackets...)
		}
		e.zone.NPCs().SetOrcusFollowing(objectID, false)
		servant, isFound := e.zone.NPCs().NPC(objectID)
		if isFound && !servant.IsDefeated {
			packet, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
				ObjectID: objectID, Value: map[uint8]float32{11: servant.Plan.ActionProfile.NonCombatMovementSpeed,
					12: servant.Plan.ActionProfile.MovementSpeed}})
			if err != nil {
				runtime.logger.Printf("Orcus follow speed cleanup omitted servant=%d: %v", objectID, err)
			} else {
				packets = append(packets, packet)
			}
		}
	}
	e.zone.NPCs().ClearDamageReduction(e.request.objectID, e.reductionExpiresAt)
	packet, err := npcraknet.ShieldEffectAsset(e.request.objectID, 0, "verdanth_boss_shield.ServerEventDef", true)
	if err != nil {
		runtime.logger.Printf("Orcus shield removal omitted source=%d: %v", e.request.objectID, err)
	} else {
		packets = append(packets, packet)
	}
	return packets
}

// The caller holds the registry lock, as for all NPC modifier retirement.
func (e *gameplayPeerSession) stopOrcusConsumeRuns() {
	for objectID, run := range e.campaignOrcusConsumeRuns {
		packets := run.finishLocked()
		e.queueCampaignPackets(packets)
		for key, member := range run.request.runtime.registry.sessions {
			if member.zone != run.zone || key == run.request.sessionKey {
				continue
			}
			member.queueCampaignPackets(packets)
			run.request.runtime.registry.sessions[key] = member
		}
		delete(e.campaignOrcusConsumeRuns, objectID)
	}
}
