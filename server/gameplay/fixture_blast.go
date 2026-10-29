package gameplay

import (
	"fmt"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const factoryBlastEffect = "sentios_fueltank_explosion.ServerEventDef"

func isFactoryPipe(nounName string) bool {
	return zonenpc.IsFactoryPipe(nounName)
}

func fixtureBlastProfile(nounName string) (zonenpc.ActionProfile, bool) {
	if isExplodingTree(nounName) {
		return explodingTreeDeathProfile(), true
	}
	if zonenpc.IsMutationEgg(nounName) {
		return zonenpc.MutationEggDeathProfile(), true
	}
	instrumentProfile, isInstrument := zonenpc.InstrumentDeathProfile(nounName)
	if isInstrument {
		return instrumentProfile, true
	}
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
	runtime     campaignDamageRuntime
	packet      raknet.Packet
	sessionKey  string
	generation  uint64
	source      zonenpc.Snapshot
	npc         *zonenpc.Session
	publication *campaignFixturePublication
	timestamp   uint64
	notBefore   time.Time
}

func (e campaignFixtureBlastStep) produce() ([][]byte, error) {
	profile, isKnown := fixtureBlastProfile(e.source.Plan.NounName)
	if !isKnown {
		return nil, nil
	}
	e.runtime.registry.mutex.RLock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && member.generation == e.generation && !member.isZoneTerminal() &&
		member.zone != nil && (e.npc == nil || member.zone.NPCs() == e.npc)
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
		if zonenpc.IsMutationEgg(e.source.Plan.NounName) && target.Plan.IsFixture {
			continue
		}
		if isExplodingTree(e.source.Plan.NounName) {
			fearPackets, fearErr := e.treeFear(target)
			packets = append(packets, fearPackets...)
			e.publication.recordPreparedPackets(fearPackets)
			if fearErr != nil {
				return packets, fmt.Errorf("treeFear: %w", fearErr)
			}
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
				return packets, fmt.Errorf("pustulePoison: %w", poisonErr)
			}
			packets = append(packets, poisonPackets...)
			e.publication.recordPreparedPackets(poisonPackets)
			continue
		}
		hitPackets, isSurviving, err := e.damage(target.Plan.ObjectID, profile, false)
		if err != nil {
			return append(packets, hitPackets...), fmt.Errorf("fixtureBlastHit: %w", err)
		}
		packets = append(packets, hitPackets...)
		if !isSurviving {
			continue
		}
		if zonenpc.IsMutationEgg(e.source.Plan.NounName) {
			continue
		}
		if profile.AbilityName == "InstrumentDeath" || profile.AbilityName == "InstrumentDeath2" {
			pushPackets, pushErr := e.instrumentKnockback(target.Plan.ObjectID, profile)
			if pushErr != nil {
				return packets, fmt.Errorf("instrumentPush: %w", pushErr)
			}
			packets = append(packets, pushPackets...)
			e.publication.recordPreparedPackets(pushPackets)
			continue
		}
		burnPackets, burnErr := e.startBurn(target.Plan.ObjectID, profile)
		if burnErr != nil {
			return packets, fmt.Errorf("fixtureBlastBurn: %w", burnErr)
		}
		packets = append(packets, burnPackets...)
		e.publication.recordPreparedPackets(burnPackets)
	}
	treePackets, err := e.finishExplodingTree()
	packets = append(packets, treePackets...)
	if err != nil {
		return packets, fmt.Errorf("treeFinish: %w", err)
	}
	deletePackets, err := e.finishMutationEgg()
	packets = append(packets, deletePackets...)
	if err != nil {
		return packets, fmt.Errorf("fixtureEggFinish: %w", err)
	}
	return packets, nil
}

func (e campaignFixtureBlastStep) damage(targetID uint32, profile zonenpc.ActionProfile, isDOT bool) ([][]byte, bool, error) {
	packets, isSurviving, err := e.damageOwned(targetID, profile, isDOT, nil)
	if err != nil {
		return packets, false, fmt.Errorf("fixtureOwned: %w", err)
	}
	return packets, isSurviving, nil
}

func (e campaignFixtureBlastStep) damageOwned(targetID uint32, profile zonenpc.ActionProfile, isDOT bool, run *campaignNPCModifierRun) ([][]byte, bool, error) {
	packets, isSurviving, err := e.damageProjected(targetID, profile, isDOT, run, nil)
	if err != nil {
		return packets, false, fmt.Errorf("fixtureProjected: %w", err)
	}
	return packets, isSurviving, nil
}

func (e campaignFixtureBlastStep) damageProjected(targetID uint32, profile zonenpc.ActionProfile, isDOT bool, run *campaignNPCModifierRun, projected *game.DamageRange) ([][]byte, bool, error) {
	r := e.runtime
	r.registry.mutex.Lock()
	member, isFound := r.registry.sessions[e.sessionKey]
	if !isFound || member.generation != e.generation || member.isZoneTerminal() ||
		member.zone == nil || (e.npc != nil && member.zone.NPCs() != e.npc) {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	if e.publication != nil && e.publication.burn == nil && r.registry.isFixtureDiscardingLocked(member.binding.GameID) {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	if run != nil && (member.campaignNPCModifiers[run.instanceID] != run || !run.isActive() || run.fixtureBurn.isRetiring) {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	target, isTargetFound := member.zone.NPCs().LiveNPC(targetID)
	if !isTargetFound || !target.IsPublished {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	if run != nil && profile.ModifierName == "LifePlagueSpread" && member.zone.NPCs().IsInfectionImmune(targetID, r.npc.now()) {
		r.registry.mutex.Unlock()
		return nil, true, nil
	}
	plan, err := zonenpc.PlanRetainedAreaAttackWithProfile(e.source, targetID, target.Plan.Position, profile)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, false, fmt.Errorf("fixtureDamagePlan: %w", err)
	}
	if run != nil && run.fixtureBurn != nil {
		plan.Damage, err = run.fixtureBurn.projectDamage(profile)
		if err != nil {
			r.registry.mutex.Unlock()
			return nil, false, fmt.Errorf("fixtureTickProjection: %w", err)
		}
	}
	if projected != nil {
		plan.Damage = *projected
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
	acceptedAt := r.npc.now()
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
	transition, err := member.applyCampaignDamageTransitionWithKill(damage, false)
	spreadTargets := make([]uint32, 0)
	if err == nil && run != nil && profile.ModifierName == "LifePlagueSpread" && !damage.IsDamageImmune && damage.Damage > 0 {
		for _, candidate := range member.zone.NPCs().LiveSnapshots() {
			if candidate.Plan.ObjectID == targetID || candidate.Faction != target.Faction ||
				candidate.Plan.Position.Sub(target.Plan.Position).Length() > 5 ||
				member.zone.NPCs().IsInfectionSpreadBlocked(candidate.Plan.ObjectID, r.npc.now()) {
				continue
			}
			spreadTargets = append(spreadTargets, candidate.Plan.ObjectID)
		}
	}
	r.registry.sessions[e.sessionKey] = member
	r.registry.mutex.Unlock()
	if err != nil {
		return nil, false, fmt.Errorf("fixtureDamageTransition: %w", err)
	}
	packets, err := e.publishDamage(target, damage, transition, acceptedAt)
	if err != nil {
		return packets, false, fmt.Errorf("fixtureDamagePublish: %w", err)
	}
	for _, objectID := range spreadTargets {
		spreadPackets, spreadErr := e.startContagiousBurn(objectID, run)
		if spreadErr != nil {
			return packets, false, fmt.Errorf("fixtureDamageSpread: %w", spreadErr)
		}
		packets = append(packets, spreadPackets...)
	}
	isSurviving := !damage.IsDefeated && (isDOT || (!damage.IsDamageImmune && damage.Damage > 0))
	return packets, isSurviving, nil
}

func (e campaignFixtureBlastStep) publishDamage(target zonenpc.Snapshot, damage zonenpc.DamageResult, transition campaignDamageTransition, acceptedAt time.Time) ([][]byte, error) {
	r := e.runtime
	packets := make([][]byte, 0)
	if damage.IsDefeated {
		definition, err := campaignNPCDeathDefinition(target, r.npc.program.NPCDeathPhysics(target.Plan.NounName))
		if err != nil {
			return packets, fmt.Errorf("fixtureDeathDefinition: %w", err)
		}
		publication, err := marshalZoneNPCDamage(definition, zoneNPCDamageResult{
			hitPoint: damage.HitPoint, isDefeated: true, isFound: true,
		}, e.source.Plan.ObjectID, damage.ResolvedDamage, false, e.timestamp, r.effectPool)
		if err != nil {
			return packets, fmt.Errorf("fixtureDeathMarshal: %w", err)
		}
		packets = append(packets, publication.packets...)
		e.publication.recordPreparedPackets(publication.packets)
		isInitial := e.publication != nil && e.publication.burn == nil
		if isInitial {
			e.publication.stageDeath(campaignFixtureDeath{run: publication.deathRun, targetID: target.Plan.ObjectID, acceptedAt: acceptedAt})
		}
		deathEvents := publication.deathRun.DrainProjection()
		if e.publication != nil {
			err = e.recordOwnedDeath(deathEvents)
		} else {
			err = r.projection.publishEnemyDeath(e.sessionKey, e.generation, deathEvents)
		}
		if err != nil {
			publication.deathRun.Stop()
			return packets, fmt.Errorf("fixtureDeathProjection: %w", err)
		}
		if !isInitial {
			err = r.npc.scheduleEnemyDeath(e.packet, e.sessionKey, e.generation, target.Plan.ObjectID, publication.deathRun)
			if err != nil {
				publication.deathRun.Stop()
				return packets, fmt.Errorf("fixtureDeathSchedule: %w", err)
			}
		}
		target.IsDefeated = true
		lootPackets, lootErr := r.npc.spawnLoot(e.sessionKey, e.generation, target, e.timestamp)
		packets = append(packets, lootPackets...)
		e.publication.recordPreparedPackets(lootPackets)
		if lootErr != nil {
			return packets, fmt.Errorf("fixtureDeathLoot: %w", lootErr)
		}
	} else if e.publication != nil && !damage.IsDamageImmune && damage.Damage > 0 {
		hitPackets, err := e.publishOwnedHit(target, damage)
		packets = append(packets, hitPackets...)
		if err != nil {
			return packets, fmt.Errorf("fixtureHitOwned: %w", err)
		}
	} else {
		err := r.projection.publishEnemyDamage(e.sessionKey, e.generation, zonenpc.DamageEvent{
			SourceObjectID: e.source.Plan.ObjectID, TargetObjectID: target.Plan.ObjectID,
			Position: target.Plan.Position, Damage: damage.Damage, HitPoint: damage.HitPoint,
			IsDamageImmune: damage.IsDamageImmune,
		})
		if err != nil {
			return packets, fmt.Errorf("fixtureHitProjection: %w", err)
		}
	}
	transitionPackets, err := r.publishTransition(e.packet, e.sessionKey, e.generation, transition, e.timestamp)
	packets = append(packets, transitionPackets...)
	e.publication.recordPreparedPackets(transitionPackets)
	if err != nil {
		return packets, fmt.Errorf("fixtureHitTransition: %w", err)
	}
	return packets, nil
}

func (e campaignDamageRuntime) scheduleFixtureBlast(packet raknet.Packet, sessionKey string, generation uint64, objectID uint32, timestamp uint64) error {
	// Source capture and index admission share one registry transaction so a
	// reset cannot complete between the defeated snapshot and its ownership.
	e.registry.mutex.Lock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || member.generation != generation || member.transportGeneration != packet.TransportGeneration || member.zone == nil || member.isZoneTerminal() || e.registry.isClosed || e.registry.isFixtureDiscardingLocked(member.binding.GameID) {
		e.registry.mutex.Unlock()
		return nil
	}
	source, isSourceFound := member.zone.NPCs().NPC(objectID)
	profile, isKnown := fixtureBlastProfile(source.Plan.NounName)
	if !isSourceFound || !source.IsDefeated || !isKnown || profile.Radius <= 0 {
		e.registry.mutex.Unlock()
		return nil
	}
	step := campaignFixtureBlastStep{runtime: e, packet: packet, sessionKey: sessionKey,
		generation: generation, source: source, npc: member.zone.NPCs(), timestamp: timestamp}
	if zonenpc.IsMutationEgg(source.Plan.NounName) {
		step.notBefore = e.npc.now().Add(mutationEggDelay)
		step.timestamp += uint64(mutationEggDelay / time.Millisecond)
	}
	if isExplodingTree(source.Plan.NounName) {
		step.notBefore = e.npc.now().Add(explodingTreeDelay)
		step.timestamp += uint64(explodingTreeDelay / time.Millisecond)
	}
	publication := &campaignFixturePublication{blast: &step}
	for current := range e.registry.fixturePublications {
		if current.blast.npc == step.npc && current.blast.source.Plan.ObjectID == objectID {
			e.registry.mutex.Unlock()
			return nil
		}
	}
	if e.registry.fixturePublications == nil {
		e.registry.fixturePublications = make(map[*campaignFixturePublication]struct{})
	}
	publication.identity = gameplayProducerIdentityFromSession(sessionKey, member, true)
	e.registry.fixturePublications[publication] = struct{}{}
	e.registry.mutex.Unlock()
	err := publication.scheduleBlast()
	if err != nil {
		publication.fail(err)
		if isFixturePeerUnavailable(err) {
			// The indexed child owns the exact transfer/retirement outcome. A
			// recursive source blast must not mistake that recovery for a domain
			// failure and abandon its already accepted transition or target loop.
			return nil
		}
		return fmt.Errorf("fixtureBlastAdmission: %w", err)
	}
	return nil
}
