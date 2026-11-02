package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type additionalKitStep struct {
	runtime          campaignNPCActionRuntime
	packet           raknet.Packet
	sessionKey       string
	generation       uint64
	objectID         uint32
	timestamp        uint64
	actionGeneration uint64
}

func (e additionalKitStep) resume(timestamp uint64) ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && member.isCampaignNPCSourceGenerationActive(e.generation, e.objectID, e.actionGeneration)
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	packets, isHandled, err := e.runtime.produceAdditionalKitAction(e.packet, e.sessionKey, e.generation, e.objectID, timestamp)
	if err != nil {
		return nil, fmt.Errorf("additionalResume: %w", err)
	}
	if !isHandled {
		return nil, nil
	}
	return packets, nil
}

func (e additionalKitStep) next() ([][]byte, error) { return e.resume(e.timestamp) }

// Selection delegates actual casts to their existing melee/cone/projectile
// owners. It is called at admission/continuation boundaries, not recursively
// from those producers' entries.
func (e campaignNPCActionRuntime) produceAdditionalKitAction(packet raknet.Packet, sessionKey string, generation uint64, objectID uint32, timestamp uint64) ([][]byte, bool, error) {
	e.registry.mutex.RLock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || member.generation != generation || member.zone == nil {
		e.registry.mutex.RUnlock()
		return nil, false, nil
	}
	actor, isActorFound := member.zone.NPCs().NPC(objectID)
	profile, isAdditional := zonenpc.AdditionalKitProfile(actor.Plan.NounName)
	if !isActorFound || !isAdditional {
		e.registry.mutex.RUnlock()
		return nil, false, nil
	}
	if !member.isCampaignNPCSourceActive(generation, objectID) {
		e.registry.mutex.RUnlock()
		return nil, true, nil
	}
	target, isTargetFound := member.campaignNPCTarget(generation, actor.TargetObjectID)
	if !isTargetFound {
		e.registry.mutex.RUnlock()
		return nil, true, nil
	}
	distance := target.Position.Sub(actor.Plan.Position).Length() - actor.Plan.ActorFootprintRadius() - target.ActorFootprintRadius
	npcSession := member.zone.NPCs()
	e.registry.mutex.RUnlock()
	step := additionalKitStep{runtime: e, packet: packet, sessionKey: sessionKey, generation: generation, objectID: objectID, timestamp: timestamp, actionGeneration: actor.ActionGeneration}
	isDeferred, err := e.pursuit.deferAction(packet, sessionKey, generation, objectID, timestamp, step.resume)
	if err != nil {
		return nil, true, fmt.Errorf("additionalDefer: %w", err)
	}
	if isDeferred {
		return nil, true, nil
	}
	if e.isSilenced(sessionKey, generation, objectID) {
		step.timestamp += 250
		cancel, scheduleErr := scheduleNPCProducers(e.registry, packet, []raknet.ScheduledPacketProducer{{Delay: 250 * time.Millisecond, Produce: step.next}})
		if scheduleErr != nil {
			return nil, true, fmt.Errorf("additionalSilence: %w", scheduleErr)
		}
		if cancel == nil {
			return nil, true, fmt.Errorf("additionalSilence: unavailable")
		}
		return nil, true, nil
	}
	profile, isAdditional, err = npcSession.SelectAdditionalKitAction(objectID, timestamp, e.now(), distance)
	if err != nil {
		return nil, true, fmt.Errorf("additionalSelect: %w", err)
	}
	if !isAdditional {
		return nil, false, nil
	}
	if actor.Plan.NounName == "VerdanthBossTest.Noun" && profile.AbilityName != "SelfHeal" {
		consumePackets, isConsumed, consumeErr := e.produceOrcusConsume(packet, sessionKey, generation, objectID, timestamp)
		if consumeErr != nil {
			return nil, true, fmt.Errorf("testConsume: %w", consumeErr)
		}
		if isConsumed {
			return consumePackets, true, nil
		}
		if profile.AbilityName == "" {
			spawnPackets, isSpawned, spawnErr := e.produceOrcusSpawn(packet, sessionKey, generation, objectID, timestamp)
			if spawnErr != nil {
				return nil, true, fmt.Errorf("testSpawn: %w", spawnErr)
			}
			if isSpawned {
				return spawnPackets, true, nil
			}
		}
	}
	if profile.AbilityName == "" {
		step.timestamp += 250
		cancel, scheduleErr := scheduleNPCProducers(e.registry, packet, []raknet.ScheduledPacketProducer{{Delay: 250 * time.Millisecond, Produce: step.next}})
		if scheduleErr != nil {
			return nil, true, fmt.Errorf("additionalWait: %w", scheduleErr)
		}
		if cancel == nil {
			return nil, true, fmt.Errorf("additionalWait: unavailable")
		}
		return nil, true, nil
	}
	if profile.AbilityName == "SelfHeal" {
		packets, healErr := step.startHeal(profile, actor)
		if healErr != nil {
			return nil, true, fmt.Errorf("additionalHeal: %w", healErr)
		}
		return packets, true, nil
	}
	if profile.AbilityName == "InstantNova" {
		packets, instantErr := step.startInstant(profile, actor)
		if instantErr != nil {
			return nil, true, fmt.Errorf("additionalInstant: %w", instantErr)
		}
		return packets, true, nil
	}
	if profile.AbilityName == "Flee" || profile.AbilityName == "Burrow" {
		packets, moveErr := step.startMove(profile, actor, target.Position)
		if moveErr != nil {
			return nil, true, fmt.Errorf("additionalMove: %w", moveErr)
		}
		return packets, true, nil
	}
	err = npcSession.SetAdditionalKitProfile(objectID, profile)
	if err != nil {
		return nil, true, fmt.Errorf("additionalProfile: %w", err)
	}
	var packets [][]byte
	switch profile.Family {
	case zonenpc.ActionProjectile:
		if profile.AbilityName == "BabyToss" {
			packets, err = e.produceEnemyLob(packet, sessionKey, generation, objectID, timestamp)
		} else {
			packets, err = e.produceZelemShot(packet, sessionKey, generation, objectID, timestamp)
		}
	case zonenpc.ActionCone:
		packets, err = e.produceEnemyCone(packet, sessionKey, generation, objectID, timestamp)
	default:
		packets, err = e.produceEnemyMelee(packet, sessionKey, generation, objectID, timestamp)
	}
	if err != nil {
		return nil, true, fmt.Errorf("additionalCast: %w", err)
	}
	return packets, true, nil
}

func (e additionalKitStep) startHeal(profile zonenpc.ActionProfile, actor zonenpc.Snapshot) ([][]byte, error) {
	packet, err := npcraknet.AnimationState(e.objectID, profile.AnimationName, e.timestamp)
	if err != nil {
		return nil, fmt.Errorf("selfHealAnimation: %w", err)
	}
	e.timestamp += uint64(profile.HitDelay / time.Millisecond)
	e.actionGeneration = actor.ActionGeneration
	cancel, err := scheduleNPCProducers(e.runtime.registry, e.packet, []raknet.ScheduledPacketProducer{{Delay: profile.HitDelay, Produce: e.heal}})
	if err != nil {
		return nil, fmt.Errorf("selfHealSchedule: %w", err)
	}
	if cancel == nil {
		return nil, fmt.Errorf("selfHealSchedule: unavailable")
	}
	return [][]byte{packet}, nil
}

func (e additionalKitStep) heal() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || !member.isCampaignNPCSourceGenerationActive(e.generation, e.objectID, e.actionGeneration) {
		e.runtime.registry.mutex.RUnlock()
		return nil, nil
	}
	actor, isActorFound := member.zone.NPCs().NPC(e.objectID)
	if !isActorFound || !member.isCampaignNPCActionActiveAt(e.generation, e.objectID, e.runtime.now()) || member.zone.NPCs().SilenceRemaining(e.objectID, e.runtime.now()) > 0 {
		e.runtime.registry.mutex.RUnlock()
		return e.resume(e.timestamp)
	}
	healed, amount, err := member.zone.NPCs().Heal(e.objectID, 1000)
	e.runtime.registry.mutex.RUnlock()
	if err != nil {
		return nil, fmt.Errorf("selfHealApply: %w", err)
	}
	var packets [][]byte
	if amount > 0 {
		packets, err = npcraknet.HealDelta(actor.Plan.ObjectID, healed, amount)
		if err != nil {
			return nil, fmt.Errorf("selfHealMarshal: %w", err)
		}
	}
	nextPackets, err := e.resume(e.timestamp)
	if err != nil {
		return nil, fmt.Errorf("selfHealResume: %w", err)
	}
	return append(packets, nextPackets...), nil
}
