package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/darkspinnet/darkspin/server/util"
	"github.com/darkspinnet/darkspin/server/zone"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func (e *gameplayPeerSession) pollCryosGeysers(runtime campaignNPCActionRuntime, req raknet.Packet, now time.Time) ([][]byte, sporenet.PlayerStatDelta, error) {
	states, err := e.zone.CryosGeysers(now)
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("geyserClock: %w", err)
	}
	if e.cryosGeyserWarnings == nil {
		e.cryosGeyserWarnings = make(map[uint32]uint64)
	}
	if e.cryosGeyserSpouts == nil {
		e.cryosGeyserSpouts = make(map[uint32]uint64)
	}
	packets := make([][]byte, 0)
	stats := sporenet.PlayerStatDelta{}
	for _, state := range states {
		if now.Before(state.WarningAt) {
			continue
		}
		if now.Before(state.SpoutAt) {
			if e.cryosGeyserWarnings[state.ObjectID] == state.Cycle {
				continue
			}
			asset := util.HashID("effect_Environment_Cryos_Geyser_Warning.ServerEventDef")
			packet, marshalErr := raknet.MarshalApplication(raknet.ServerEventContractMessage{Asset: &asset, ObjectID: &state.ObjectID})
			if marshalErr != nil {
				return nil, stats, fmt.Errorf("geyserWarning: %w", marshalErr)
			}
			e.cryosGeyserWarnings[state.ObjectID] = state.Cycle
			packets = append(packets, packet)
			continue
		}
		if e.cryosGeyserSpouts[state.ObjectID] == state.Cycle {
			continue
		}
		asset := uint32(0xa6120b9c)
		position := raknet.Vector3(state.Position)
		packet, marshalErr := raknet.MarshalApplication(raknet.ServerEventContractMessage{SimpleSwarmEffectID: &asset, Position: &position})
		if marshalErr != nil {
			return nil, stats, fmt.Errorf("geyserSpout: %w", marshalErr)
		}
		e.cryosGeyserSpouts[state.ObjectID] = state.Cycle
		packets = append(packets, packet)
		hits, delta, hitErr := e.cryosGeyserHitsLocked(runtime, state, req, now)
		if hitErr != nil {
			return nil, stats, fmt.Errorf("geyserHits: %w", hitErr)
		}
		stats.PVEDamageTaken += delta.PVEDamageTaken
		packets = append(packets, hits...)
	}
	return packets, stats, nil
}

func (e *gameplayPeerSession) cryosGeyserHitsLocked(runtime campaignNPCActionRuntime, state zone.CryosGeyser, req raknet.Packet, now time.Time) ([][]byte, sporenet.PlayerStatDelta, error) {
	targets := state.Targets
	if !state.IsQueryCaptured {
		var queryErr error
		targets, queryErr = e.cryosGeyserTargetsLocked(runtime, state.Position, now)
		if queryErr != nil {
			return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("geyserQuery: %w", queryErr)
		}
	}
	targets = e.zone.CaptureCryosGeyserTargets(state.ObjectID, state.Cycle, targets)
	packets := make([][]byte, 0)
	stats := sporenet.PlayerStatDelta{}
	for _, target := range targets {
		if target.UserID != e.binding.UserID || target.PeerGeneration != e.generation {
			continue
		}
		damage, err := sim.SelectRankDamage(e.zone.NPCRandom(), sim.DamageRange{Minimum: 20, Maximum: 40})
		if err != nil {
			return nil, stats, fmt.Errorf("geyserRoll: %w", err)
		}
		plan := zonenpc.AttackPlan{SourceObjectID: 0, InitiatorObjectID: state.ObjectID,
			TargetObjectID: target.ObjectID, SourcePosition: state.Position, TargetPosition: target.Position,
			Profile: zonenpc.ActionProfile{Family: zonenpc.ActionRetainedArea, AbilityName: "CryosGeyserPassive",
				MinimumDamage: 20, MaximumDamage: 40, DamageType: 3, DamageSource: 1,
				DescriptorMask: 1 << 14, IsRetainedVolumeDamage: true, IsDamageProfileKnown: true}}
		hits, delta, isApplied, err := runtime.applyEnemyStatusDamage(e, e.generation, plan, zonenpc.AttackResult{Damage: damage}, req.SourceTime)
		if err != nil {
			return nil, stats, fmt.Errorf("geyserDamage: %w", err)
		}
		// Lua46 does not branch on TakeDamage's result before the hit swarm.
		if !isApplied && runtime.logger != nil {
			runtime.logger.Printf("RakNet geyser contact had no committed damage source=%d target=%d cycle=%d", state.ObjectID, target.ObjectID, state.Cycle)
		}
		asset := uint32(0x78da313e)
		hit, err := raknet.MarshalApplication(raknet.ServerEventContractMessage{SimpleSwarmEffectID: &asset, ObjectID: &target.ObjectID})
		if err != nil {
			return nil, stats, fmt.Errorf("geyserHit: %w", err)
		}
		hits = append(hits, hit)
		packets = append(packets, hits...)
		stats.PVEDamageTaken += delta.PVEDamageTaken
	}
	err := e.publishFireVentHitAfterCommit(runtime.registry, req, packets)
	if err != nil {
		return nil, stats, fmt.Errorf("geyserPeers: %w", err)
	}
	return packets, stats, nil
}

// Current recipient adapters cover connected heroes and owned companion
// creatures. Player-aligned NPC-aggregate recipients require their own damage
// operation and are not certified by this co-op hero/companion path.
func (e *gameplayPeerSession) cryosGeyserTargetsLocked(runtime campaignNPCActionRuntime, position game.Vec3, now time.Time) ([]zone.NPCTarget, error) {
	objectIDs := make([]uint32, 0)
	for _, hero := range e.zone.Hero().Snapshots() {
		objectIDs = append(objectIDs, hero.ObjectID)
	}
	for _, actor := range e.zone.Companion().Snapshots() {
		objectIDs = append(objectIDs, actor.ObjectID)
	}
	targets := make([]zone.NPCTarget, 0, len(objectIDs))
	for _, objectID := range objectIDs {
		// This existing creature predicate includes actual friendly companion
		// bodies and visible or stealthed heroes, not AI acquisition targets.
		target, isFound := runtime.drainerDeathTargetLocked(e.zone, objectID)
		if !isFound {
			continue
		}
		targets = append(targets, target)
	}
	samples, err := runtime.registry.sampleNPCTargetsLocked(e.zone, targets, now)
	if err != nil {
		return nil, fmt.Errorf("geyserPosition: %w", err)
	}
	targets = targets[:0]
	for _, target := range samples {
		// Native103 spatial membership is strict leaf-radius + query-radius.
		// ActorFootprintRadius is the established server leaf projection.
		if target.Position.Sub(position).Length() >= 0.1+target.ActorFootprintRadius {
			continue
		}
		targets = append(targets, target)
	}
	return targets, nil
}

// Keep the sourceless Lua call distinct from its validated world initiator.
func geyserImmunePacket(targetObjectID uint32) ([]byte, error) {
	packet, err := raknet.MarshalApplication(raknet.DamageCombatEventMessage{Flags: 0x40, SourceID: 0, TargetID: targetObjectID})
	if err != nil {
		return nil, fmt.Errorf("geyserImmune: %w", err)
	}
	return packet, nil
}
