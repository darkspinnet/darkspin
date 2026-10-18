package gameplay

import (
	"fmt"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const factoryBlastEffect = "sentios_fueltank_explosion.ServerEventDef"

func isFactoryPipe(nounName string) bool {
	return zonenpc.IsFactoryPipe(nounName)
}

func fixtureBlastProfile(nounName string) (zonenpc.ActionProfile, bool) {
	pustuleProfile, isPustule := scaldronPustuleProfile(nounName)
	if isPustule {
		return pustuleProfile, true
	}
	profile := zonenpc.ActionProfile{
		MinimumDamage: 6, MaximumDamage: 12, Radius: 5,
		DamageType: 0, DamageSource: 1, DescriptorMask: 1<<3 | 1<<7,
		IsDamageProfileKnown: true,
		ModifierName:         "PlasmaSentinelBurn", ModifierDuration: 5 * time.Second,
		ModifierMinimumTickDamage: 1, ModifierMaximumTickDamage: 1,
		ModifierTickDamageCoefficient: 0.05,
	}
	if isFactoryPipe(nounName) {
		return profile, true
	}
	if !strings.EqualFold(nounName, "DEST_citadel_fuelcanister.Noun") {
		return zonenpc.ActionProfile{}, false
	}
	profile.MinimumDamage = 1
	profile.MaximumDamage = 3
	profile.Radius = 4
	profile.DamageType = 3
	// The script declares Physical/AoE descriptors but Energy damage source.
	profile.DescriptorMask = 1<<3 | 1<<6
	profile.ModifierName = "CitadelPlasmaBurn"
	profile.ModifierDuration = 0
	profile.ModifierMinimumTickDamage = 10
	profile.ModifierMaximumTickDamage = 20
	return profile, true
}

type campaignFixtureBlastStep struct {
	runtime    campaignDamageRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	source     zonenpc.Snapshot
	timestamp  uint64
}

func (e campaignFixtureBlastStep) produce() ([][]byte, error) {
	profile, isKnown := fixtureBlastProfile(e.source.Plan.NounName)
	if !isKnown {
		return nil, nil
	}
	e.runtime.registry.mutex.RLock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && member.generation == e.generation && !member.isZoneTerminal()
	if !isCurrent {
		e.runtime.registry.mutex.RUnlock()
		return nil, nil
	}
	targets := member.zone.NPCs().LiveSnapshots()
	e.runtime.registry.mutex.RUnlock()
	packets := make([][]byte, 0)
	for _, target := range targets {
		// Both scripts use ValidateFriendlyTarget(sourceTeam, target): scenery
		// hurts other NPCs on its side, including destructibles, rather than heroes.
		if target.Plan.ObjectID == e.source.Plan.ObjectID || !target.IsPublished ||
			target.Faction != e.source.Faction ||
			target.Plan.Position.Sub(e.source.Plan.Position).Length() > profile.Radius {
			continue
		}
		if profile.ModifierName == "LifePlagueSpread" {
			// The authored kType_Creature query excludes other scenery. The
			// pustule requests disease without an initial explosion damage hit.
			if target.Plan.IsFixture {
				continue
			}
			poisonPackets, poisonErr := e.startBurn(target.Plan.ObjectID, profile)
			if poisonErr != nil {
				return nil, fmt.Errorf("pustulePoison: %w", poisonErr)
			}
			packets = append(packets, poisonPackets...)
			continue
		}
		hitPackets, isSurviving, err := e.damage(target.Plan.ObjectID, profile, false)
		if err != nil {
			return nil, fmt.Errorf("fixtureBlastHit: %w", err)
		}
		packets = append(packets, hitPackets...)
		if !isSurviving {
			continue
		}
		burnPackets, burnErr := e.startBurn(target.Plan.ObjectID, profile)
		if burnErr != nil {
			return nil, fmt.Errorf("fixtureBlastBurn: %w", burnErr)
		}
		packets = append(packets, burnPackets...)
	}
	return packets, nil
}

func (e campaignFixtureBlastStep) damage(targetID uint32, profile zonenpc.ActionProfile, isDOT bool) ([][]byte, bool, error) {
	r := e.runtime
	r.registry.mutex.Lock()
	member, isFound := r.registry.sessions[e.sessionKey]
	if !isFound || member.generation != e.generation || member.isZoneTerminal() {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	target, isTargetFound := member.zone.NPCs().LiveNPC(targetID)
	if !isTargetFound || !target.IsPublished {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	plan, err := zonenpc.PlanRetainedAreaAttackWithProfile(e.source, targetID, target.Plan.Position, profile)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, false, fmt.Errorf("fixtureDamagePlan: %w", err)
	}
	result, err := zonenpc.CommitAttack(member.zone.NPCRandom(), plan, 0, r.npc.program.Critical)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, false, fmt.Errorf("fixtureDamageRoll: %w", err)
	}
	if result.Damage <= 0 {
		r.registry.mutex.Unlock()
		return nil, true, nil
	}
	sourcePosition := e.source.Plan.Position
	damage, err := member.zone.NPCs().Hit(zonenpc.HitRequest{
		SourceObjectID: e.source.Plan.ObjectID, TargetObjectID: targetID,
		Damage: result.Damage, SourcePosition: &sourcePosition, IsArea: !isDOT, IsPeriodic: isDOT,
		Metadata: zonenpc.DamageMetadata{
			DamageSource: uint32(profile.DamageSource), DamageType: uint32(profile.DamageType),
			DescriptorMask: profile.DescriptorMask, IsDamageTypeKnown: true,
		},
	})
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, false, fmt.Errorf("fixtureDamageCommit: %w", err)
	}
	transition, err := member.applyCampaignDamageTransition(damage)
	r.registry.sessions[e.sessionKey] = member
	r.registry.mutex.Unlock()
	if err != nil {
		return nil, false, fmt.Errorf("fixtureDamageTransition: %w", err)
	}
	packets, err := e.publishDamage(target, damage, transition)
	if err != nil {
		return nil, false, fmt.Errorf("fixtureDamagePublish: %w", err)
	}
	isSurviving := !damage.IsDefeated && (isDOT || (!damage.IsDamageImmune && damage.Damage > 0))
	return packets, isSurviving, nil
}

func (e campaignFixtureBlastStep) publishDamage(target zonenpc.Snapshot, damage zonenpc.DamageResult, transition campaignDamageTransition) ([][]byte, error) {
	r := e.runtime
	packets := make([][]byte, 0)
	if damage.IsDefeated {
		definition, err := campaignNPCDeathDefinition(target, r.npc.program.NPCDeathPhysics(target.Plan.NounName))
		if err != nil {
			return nil, fmt.Errorf("fixtureDeathDefinition: %w", err)
		}
		publication, err := marshalZoneNPCDamage(definition, zoneNPCDamageResult{
			hitPoint: damage.HitPoint, isDefeated: true, isFound: true,
		}, e.source.Plan.ObjectID, damage.ResolvedDamage, false, e.timestamp, r.effectPool)
		if err != nil {
			return nil, fmt.Errorf("fixtureDeathMarshal: %w", err)
		}
		err = r.projection.publishEnemyDeath(e.sessionKey, e.generation, publication.deathRun.DrainProjection())
		if err != nil {
			publication.deathRun.Stop()
			return nil, fmt.Errorf("fixtureDeathProjection: %w", err)
		}
		err = r.npc.scheduleEnemyDeath(e.packet, e.sessionKey, e.generation, target.Plan.ObjectID, publication.deathRun)
		if err != nil {
			publication.deathRun.Stop()
			return nil, fmt.Errorf("fixtureDeathSchedule: %w", err)
		}
		packets = append(packets, publication.packets...)
		target.IsDefeated = true
		lootPackets, lootErr := r.npc.spawnLoot(e.sessionKey, e.generation, target, e.timestamp)
		if lootErr != nil {
			return nil, fmt.Errorf("fixtureDeathLoot: %w", lootErr)
		}
		packets = append(packets, lootPackets...)
	} else {
		err := r.projection.publishEnemyDamage(e.sessionKey, e.generation, zonenpc.DamageEvent{
			SourceObjectID: e.source.Plan.ObjectID, TargetObjectID: target.Plan.ObjectID,
			Position: target.Plan.Position, Damage: damage.Damage, HitPoint: damage.HitPoint,
			IsDamageImmune: damage.IsDamageImmune,
		})
		if err != nil {
			return nil, fmt.Errorf("fixtureHitProjection: %w", err)
		}
	}
	transitionPackets, err := r.publishTransition(e.packet, e.sessionKey, e.generation, transition, e.timestamp)
	if err != nil {
		return nil, fmt.Errorf("fixtureHitTransition: %w", err)
	}
	return append(packets, transitionPackets...), nil
}

func (e campaignDamageRuntime) scheduleFixtureBlast(packet raknet.Packet, sessionKey string, generation uint64, objectID uint32, timestamp uint64) error {
	e.registry.mutex.RLock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || member.generation != generation || member.zone == nil {
		e.registry.mutex.RUnlock()
		return nil
	}
	source, isSourceFound := member.zone.NPCs().NPC(objectID)
	e.registry.mutex.RUnlock()
	profile, isKnown := fixtureBlastProfile(source.Plan.NounName)
	if !isSourceFound || !source.IsDefeated || !isKnown || profile.Radius <= 0 {
		return nil
	}
	step := campaignFixtureBlastStep{runtime: e, packet: packet, sessionKey: sessionKey,
		generation: generation, source: source, timestamp: timestamp}
	cancel, err := scheduleNPCProducers(e.registry, packet, []raknet.ScheduledPacketProducer{{Produce: step.produce}})
	if err != nil {
		return fmt.Errorf("fixtureBlastSchedule: %w", err)
	}
	if cancel == nil {
		return fmt.Errorf("fixtureBlastCancel: cancellation unavailable")
	}
	return nil
}
