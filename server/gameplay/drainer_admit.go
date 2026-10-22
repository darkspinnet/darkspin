package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/zone"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type drainerDeathOwner struct {
	world      *zone.Zone
	gameID     uint32
	userID     uint64
	generation uint64
	sessionKey string
}

func (e campaignNPCActionRuntime) publishDrainerDeath(packet raknet.Packet, owner drainerDeathOwner,
	source zonenpc.Snapshot, result zonenpc.DamageResult,
	transition campaignDamageTransition, timestamp uint64,
	initialPackets [][]byte, initialErr error,
) ([][]byte, error) {
	run, err := e.reserveDrainerDeath(owner, source, timestamp)
	if err != nil {
		return nil, fmt.Errorf("drainerReserve: %w", err)
	}
	if initialErr != nil {
		run.reject(initialPackets)
		return nil, fmt.Errorf("drainerChannel: %w", initialErr)
	}
	packets, err := run.prepare(packet, owner.sessionKey, result, transition, timestamp)
	packets = append(append([][]byte(nil), initialPackets...), packets...)
	if err != nil {
		run.reject(packets)
		return nil, fmt.Errorf("drainerPrepare: %w", err)
	}
	err = run.admit(packets)
	if err != nil {
		run.reject(packets)
		return nil, fmt.Errorf("drainerAdmit: %w", err)
	}
	return nil, nil
}

func (e campaignNPCActionRuntime) reserveDrainerDeath(owner drainerDeathOwner, source zonenpc.Snapshot, timestamp uint64,
) (*drainerDeathRun, error) {
	if e.registry == nil || e.now == nil || source.Faction != zonenpc.FactionNonPlayerAligned {
		return nil, errors.New("drainer death runtime unavailable")
	}
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	if e.registry.isClosed || owner.world == nil {
		return nil, errors.New("drainer death world unavailable")
	}
	current, isFound := owner.world.NPCs().NPC(source.Plan.ObjectID)
	if !isFound || !current.IsPublished || !current.IsDefeated || current.ActionGeneration != source.ActionGeneration || current.Plan.NounName != source.Plan.NounName {
		return nil, errors.New("drainer death source replaced")
	}
	key := companionDeathKey{world: owner.world, objectID: source.Plan.ObjectID}
	if e.registry.drainerDeaths[key] != nil {
		return nil, errors.New("drainer death already reserved")
	}
	startedAt := e.now()
	source.Plan = source.Plan.Clone()
	run := &drainerDeathRun{runtime: e, key: key, gameID: owner.gameID,
		userID: owner.userID, generation: owner.generation, source: source,
		criticalRating: source.EffectiveCriticalRating(), deadline: startedAt.Add(zonenpc.DrainerDeathDelay),
		timestamp: timestamp + uint64(zonenpc.DrainerDeathDelay/time.Millisecond)}
	if e.registry.drainerDeaths == nil {
		e.registry.drainerDeaths = make(map[companionDeathKey]*drainerDeathRun)
	}
	// Reservation precedes objectives, rewards and transition side effects.
	e.registry.drainerDeaths[key] = run
	plan, err := zonenpc.CaptureDrainerDeath(source, startedAt)
	if err != nil {
		delete(e.registry.drainerDeaths, key)
		if owner.world.NPCs().FinishDrainerDeath(source) {
			deletePacket, deleteErr := raknet.MarshalApplication(raknet.ObjectDeleteMessage{ObjectID: []uint32{source.Plan.ObjectID}})
			if deleteErr == nil {
				e.registry.queueCompanionDeathWorldLocked(owner.world, [][]byte{deletePacket})
			} else if e.logger != nil {
				e.logger.Printf("Drainer snapshot failure deletion object=%d: %v", source.Plan.ObjectID, deleteErr)
			}
		}
		return nil, fmt.Errorf("drainerSnapshot: %w", err)
	}
	run.plan = plan
	deletePacket, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{ObjectID: []uint32{source.Plan.ObjectID}})
	if err != nil {
		delete(e.registry.drainerDeaths, key)
		return nil, fmt.Errorf("drainerDelete: %w", err)
	}
	run.deletePacket = deletePacket
	return run, nil
}

func (e *drainerDeathRun) prepare(packet raknet.Packet, sessionKey string,
	result zonenpc.DamageResult, transition campaignDamageTransition, timestamp uint64,
) ([][]byte, error) {
	runtime := e.runtime
	definition, err := campaignNPCDeathDefinition(e.source, runtime.program.NPCDeathPhysics(e.source.Plan.NounName))
	if err != nil {
		return nil, fmt.Errorf("drainerDefinition: %w", err)
	}
	definition.ordinaryDeathAnimation = "is_dead"
	definition.isDeathAnimationSuppressed = false
	definition.isRemnantRetained = true
	definition.isCollisionRetained = false
	definition.deleteDelay = zonenpc.DrainerDeathDelay
	definition.explosionEffectName = ""
	publication, err := marshalZoneNPCDamage(definition,
		zoneNPCDamageResult{hitPoint: 0, isDefeated: true, isFound: true},
		e.source.Plan.ObjectID, result.Damage, false, timestamp, runtime.effectPool)
	if err != nil {
		return nil, fmt.Errorf("drainerEncode: %w", err)
	}
	deathEvents := publication.deathRun.DrainProjection()
	// The named world callback owns the corpse; no generic fade/delete timer is
	// admitted. This transient encoder owns no retained effect after Stop.
	publication.deathRun.Stop()
	packets := publication.packets
	explosionAnimation, err := npcraknet.AnimationState(e.key.objectID, "nct_minn_su_drainer_explode", timestamp)
	if err != nil {
		return packets, fmt.Errorf("drainerAnimation: %w", err)
	}
	packets = append(packets, explosionAnimation)
	err = e.key.world.RecordNPCDeathObjectives(e.key.world.Context(), deathEvents)
	if err != nil {
		return packets, fmt.Errorf("drainerObjectives: %w", err)
	}
	defeatedSource := e.source
	defeatedSource.IsDefeated = true
	defeatedSource.HitPoint = 0
	lootPackets, err := runtime.spawnLoot(sessionKey, e.generation, defeatedSource, timestamp)
	packets = append(packets, lootPackets...)
	if err != nil {
		return packets, fmt.Errorf("drainerLoot: %w", err)
	}
	damageRuntime := campaignDamageRuntime{registry: runtime.registry, npc: runtime, projection: runtime.projection,
		effectPool: runtime.effectPool, gameplayJoin: runtime.gameplayJoin, logger: runtime.logger}
	transitionPackets, err := damageRuntime.publishTransition(packet, sessionKey, e.generation, transition, timestamp)
	packets = append(packets, transitionPackets...)
	if err != nil {
		return packets, fmt.Errorf("drainerTransition: %w", err)
	}
	return packets, nil
}

func (e *drainerDeathRun) admit(packets [][]byte) error {
	registry := e.runtime.registry
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	current, isFound := e.key.world.NPCs().NPC(e.key.objectID)
	if registry.isClosed || registry.drainerDeaths[e.key] != e || !isFound || !current.IsPublished || !current.IsDefeated ||
		current.ActionGeneration != e.source.ActionGeneration || current.Plan.NounName != e.source.Plan.NounName {
		return errors.New("drainer admission replaced")
	}
	if e.runtime.timer == nil {
		return errors.New("drainer timer unavailable")
	}
	cancel, err := e.runtime.timer.Schedule(max(time.Duration(0), e.deadline.Sub(e.runtime.now())), e.execute)
	if err == nil && cancel == nil {
		err = errors.New("drainer cancellation unavailable")
	}
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return fmt.Errorf("drainerSchedule: %w", err)
	}
	e.cancel = cancel
	registry.queueDrainerWorldLocked(e.key.world, e.userID, e.generation, packets)
	return nil
}

func (e *drainerDeathRun) reject(packets [][]byte) {
	registry := e.runtime.registry
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	if registry.drainerDeaths[e.key] != e {
		return
	}
	delete(registry.drainerDeaths, e.key)
	current, isFound := e.key.world.NPCs().NPC(e.key.objectID)
	if !isFound || !current.IsDefeated || current.ActionGeneration != e.source.ActionGeneration || current.Plan.NounName != e.source.Plan.NounName {
		return
	}
	registry.queueDrainerWorldLocked(e.key.world, e.userID, e.generation, packets)
	e.finishLocked()
}

func (e *gameplaySessionRegistry) queueDrainerWorldLocked(world *zone.Zone, userID uint64, generation uint64, packets [][]byte) {
	peerPackets := gameplayPeerPresentationPackets(packets)
	for sessionKey, member := range e.sessions {
		if member.zone != world || member.isZoneTerminal() {
			continue
		}
		if member.binding.UserID == userID && member.generation == generation {
			member.queueCampaignPackets(packets)
		} else {
			member.queueCampaignPackets(peerPackets)
		}
		e.sessions[sessionKey] = member
	}
	for _, retained := range e.retainedSessions {
		if retained.peerSession.zone != world || retained.peerSession.isZoneTerminal() {
			continue
		}
		if retained.peerSession.binding.UserID == userID && retained.peerSession.generation == generation {
			retained.peerSession.queueCampaignPackets(packets)
		} else {
			retained.peerSession.queueCampaignPackets(peerPackets)
		}
	}
}
