package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type eggLivingTarget struct {
	objectID uint32
	position game.Vec3
	faction  zonenpc.Faction
}

// Chunk231 queries all living objects, not only Creature. The exact installed
// source is nonplayer-aligned; modeled heroes/companions are player-aligned.
// NPC snapshots include friendly living destructibles and owned NPC actors.
func (e campaignFixtureBlastStep) producePoisonEgg() ([][]byte, error) {
	graph := e.source.Plan.NPCProfile.AIGraph
	if !e.source.Plan.IsFixture || graph == nil || !graph.IsResolved || graph.DeathAbility == nil || *graph.DeathAbility != "ExplodingEggDeath" {
		return nil, errors.New("poison egg authored death callback unavailable")
	}
	if e.source.Faction != zonenpc.FactionNonPlayerAligned {
		return nil, errors.New("poison egg source faction has no authored production owner")
	}
	r := e.runtime
	r.registry.mutex.RLock()
	member, isFound := r.registry.sessions[e.sessionKey]
	if !isFound || member.generation != e.generation || member.zone == nil || member.zone.NPCs() != e.npc || member.isZoneTerminal() {
		r.registry.mutex.RUnlock()
		return nil, nil
	}
	targets := make([]eggLivingTarget, 0)
	for _, actor := range member.zone.NPCs().LiveSnapshots() {
		if actor.IsPublished {
			targets = append(targets, eggLivingTarget{objectID: actor.Plan.ObjectID, position: actor.Plan.Position, faction: actor.Faction})
		}
	}
	// Unlike the creature-only Nocturna egg, include every modeled living
	// family. Heroes/companions use their actual player faction, and are
	// rejected by ValidateFriendlyTarget for this nonplayer source.
	actors, sampleErr := r.registry.sampleNPCTargetsLocked(member.zone, member.zone.LiveNPCTargets(), r.npc.now())
	if sampleErr != nil {
		r.registry.mutex.RUnlock()
		return nil, fmt.Errorf("eggQueryPoses: %w", sampleErr)
	}
	for _, actor := range actors {
		if actor.HitPoint > 0 {
			targets = append(targets, eggLivingTarget{objectID: actor.ObjectID, position: actor.Position, faction: zonenpc.FactionPlayerAligned})
		}
	}
	r.registry.mutex.RUnlock()
	packets := make([][]byte, 0)
	profile := zonenpc.PoisonEggDeathProfile()
	for _, target := range targets {
		if target.objectID == e.source.Plan.ObjectID || target.faction != e.source.Faction || target.position.Sub(e.source.Plan.Position).Length() > profile.Radius {
			continue
		}
		poisonPackets, err := e.startBurn(target.objectID, profile)
		packets = append(packets, poisonPackets...)
		e.publication.recordPreparedPackets(poisonPackets)
		if err != nil {
			return packets, fmt.Errorf("eggPoisonAdmit: %w", err)
		}
	}
	deletePackets, err := e.finishMutationEgg()
	packets = append(packets, deletePackets...)
	if err != nil {
		return packets, fmt.Errorf("poisonEggFinish: %w", err)
	}
	return packets, nil
}

func (e campaignNPCActionRuntime) attachEggPoisonLocked(member gameplayPeerSession, run *campaignNPCModifierRun) error {
	effects, err := e.registry.allocateChannelEffectsLocked(member.zone, e.effectPool, []uint32{run.record.TargetObjectID})
	if err != nil {
		return fmt.Errorf("eggEffectAllocate: %w", err)
	}
	slot := effects.Slots()[0]
	packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
		ObjectID: run.record.TargetObjectID, Slot: slot.slot + 1, IsForceAttached: true,
		Asset: util.HashID("status_poisoned.ServerEventDef"),
	})
	if err != nil {
		effects.Retire()
		return fmt.Errorf("eggEffectMarshal: %w", err)
	}
	run.fixtureBurn.effects = effects
	err = e.registry.publishChannelEffectsLocked(effects, [][]byte{packet})
	if err != nil {
		effects.Retire()
		return fmt.Errorf("eggEffectPublish: %w", err)
	}
	return nil
}

// The state is registry-locked, including refresh, completion and handoff.
func (e campaignFixtureBurnStep) produceEggPoison() ([][]byte, error) {
	registry := e.blast.runtime.registry
	registry.mutex.Lock()
	state := e.run.fixtureBurn
	isExpired := !e.blast.runtime.npc.now().Before(state.expiresAt)
	if isExpired {
		if state.isRetiring || !e.run.isActive() {
			registry.mutex.Unlock()
			return nil, nil
		}
		packet, err := effectraknet.ModifierDelete(e.targetID, e.run.instanceID)
		if err != nil {
			registry.mutex.Unlock()
			return nil, fmt.Errorf("eggPoisonExpire: %w", err)
		}
		state.isRetiring = true
		registry.mutex.Unlock()
		return [][]byte{packet}, nil
	}
	registry.mutex.Unlock()
	if e.isExpiry {
		// A refresh moved expiry after this already scheduled deadline. Its
		// publication will schedule the new minimum(tick, expiry), without
		// restarting the original coroutine or creating a second producer.
		return nil, nil
	}
	profile := e.profile
	profile.MinimumDamage = profile.ModifierMinimumTickDamage
	profile.MaximumDamage = profile.ModifierMaximumTickDamage
	profile.DamageCoefficient = profile.ModifierTickDamageCoefficient
	profile.DamageType = profile.ModifierDamageType
	profile.DamageSource = profile.ModifierDamageSource
	profile.DescriptorMask = profile.ModifierDescriptorMask
	e.blast.timestamp = state.sourceTimestamp + uint64(e.deadline/time.Millisecond)
	packets, isSurviving, err := e.blast.damageOwned(e.targetID, profile, true, e.run)
	if err != nil || !isSurviving {
		deleted, deleteErr := e.stop()
		if err != nil || deleteErr != nil {
			return append(packets, deleted...), fmt.Errorf("eggPoisonTick: %w", errors.Join(err, deleteErr))
		}
		return append(packets, deleted...), nil
	}
	return packets, nil
}

func (e campaignFixtureBurnStep) nextEggPoisonLocked() campaignFixtureBurnStep {
	state := e.run.fixtureBurn
	if !e.isExpiry {
		state.nextTickDeadline = e.deadline + e.profile.ModifierTickDuration
	}
	expiryDeadline := state.expiresAt.Sub(state.startedAt)
	e.deadline = min(state.nextTickDeadline, expiryDeadline)
	e.isExpiry = expiryDeadline <= state.nextTickDeadline
	return e
}
