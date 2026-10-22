package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type expungeRemainingDamage struct {
	creature       game.GameplayCreature
	definition     sim.AbilityDefinition
	sourceObjectID uint32
	sessionKey     string
	generation     uint64
	binding        game.GameplayBinding
}

type expungeDamagePublication struct {
	remaining   expungeRemainingDamage
	results     []zoneability.AreaResult
	transitions []campaignDamageTransition
}

type expungeModifierDelete struct {
	targetObjectID uint32
	instanceID     uint32
}

func (e campaignMeleeSchedule) expunge() ([][]byte, error) {
	if e.selected.Name != "LFPoisonRavager_Expunge" {
		return nil, nil
	}
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.basicAttack == e.run && peerSession.zone != nil &&
		peerSession.zone.NPCs() != nil
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	target, isTargetFound := peerSession.zone.NPCs().NPC(e.targetObjectID)
	if !isTargetFound || target.IsDefeated || target.HitPoint <= 0 {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	damages := make([]expungeRemainingDamage, 0)
	deletes := make([]expungeModifierDelete, 0)
	cancels := make([]func(), 0)
	for sessionKey, candidate := range e.runtime.registry.sessions {
		if candidate.zone != peerSession.zone {
			continue
		}
		if sessionKey == e.sessionKey {
			candidate = peerSession
		}
		consumedDamages, consumedDeletes, consumedCancels, consumeErr := consumeExpungeDamageOverTime(&candidate, e.targetObjectID, e.runtime.modifierPool)
		if consumeErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("expungeConsume: %w", consumeErr)
		}
		plasmaDamage, plasmaDelete, plasmaCancel, plasmaErr := candidate.consumeExpungePlasma(e.targetObjectID, e.runtime.now(), e.runtime.modifierPool)
		if plasmaErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("expungePlasma: %w", plasmaErr)
		}
		if plasmaDamage != nil {
			consumedDamages = append(consumedDamages, *plasmaDamage)
		}
		if plasmaDelete != nil {
			consumedDeletes = append(consumedDeletes, *plasmaDelete)
		}
		if plasmaCancel != nil {
			consumedCancels = append(consumedCancels, plasmaCancel)
		}
		infectionDamage, infectionDelete, infectionErr := candidate.heroInfection.consumeExpunge(e.targetObjectID, e.runtime.now())
		if infectionErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("expungeInfection: %w", infectionErr)
		}
		if infectionDamage != nil {
			consumedDamages = append(consumedDamages, *infectionDamage)
		}
		if infectionDelete != nil {
			consumedDeletes = append(consumedDeletes, *infectionDelete)
		}
		for index := range consumedDamages {
			consumedDamages[index].sessionKey = sessionKey
			consumedDamages[index].generation = candidate.generation
			consumedDamages[index].binding = candidate.binding
		}
		damages = append(damages, consumedDamages...)
		deletes = append(deletes, consumedDeletes...)
		cancels = append(cancels, consumedCancels...)
		e.runtime.registry.sessions[sessionKey] = candidate
		if sessionKey == e.sessionKey {
			peerSession = candidate
		}
	}
	burnDamages, burnDeletes, burnErr := e.runtime.registry.consumeBasicBurnsLocked(peerSession, e.targetObjectID, e.runtime.modifierPool)
	if burnErr != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("expungeBasicBurn: %w", burnErr)
	}
	damages = append(damages, burnDamages...)
	deletes = append(deletes, burnDeletes...)
	publications := make([]expungeDamagePublication, 0, len(damages))
	for _, remaining := range damages {
		live, isLiveFound := peerSession.zone.NPCs().NPC(e.targetObjectID)
		if !isLiveFound || live.IsDefeated || live.HitPoint <= 0 {
			break
		}
		damage, err := zoneability.ProjectDamage(
			remaining.creature, remaining.definition,
			remaining.definition.MinimumDamage,
			remaining.definition.MaximumDamage,
		)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("expungeProjection: %w", err)
		}
		plan := zoneability.AreaPlan{
			SourceObjectID: remaining.sourceObjectID,
			AbilityID:      e.plan.AbilityID,
			Definition:     remaining.definition,
			Damage:         damage,
			Target:         []zonenpc.Snapshot{live},
			Center:         game.Vec3(peerSession.playerPosition),
			IsSingleTarget: true,
		}
		committed, err := zoneability.CommitArea(
			peerSession.zone.Population().Random(), peerSession.zone.NPCs(),
			plan, remaining.creature, peerSession.binding.Difficulty,
			e.runtime.program.Critical,
		)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("expungeDamage: %w", err)
		}
		transitions := make([]campaignDamageTransition, 0, len(committed))
		transitionKey := remaining.sessionKey
		owner, isOwnerFound := e.runtime.registry.sessions[transitionKey]
		if !isOwnerFound || owner.generation != remaining.generation || owner.zone != peerSession.zone {
			// World transitions still run when a zone-owned burn outlives its
			// initiator, without awarding another player's life steal or kills.
			transitionKey = e.sessionKey
			owner = peerSession
		}
		isOriginalOwner := owner.binding.UserID == remaining.binding.UserID
		for _, result := range committed {
			transition, transitionErr :=
				owner.applyCampaignDamageTransitionWithKill(result.Damage, isOriginalOwner)
			if transitionErr != nil {
				e.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("expungeTransition: %w", transitionErr)
			}
			transitions = append(transitions, transition)
		}
		e.runtime.registry.sessions[transitionKey] = owner
		if transitionKey == e.sessionKey {
			peerSession = owner
		}
		publications = append(publications, expungeDamagePublication{remaining: remaining, results: committed, transitions: transitions})
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	for _, cancel := range cancels {
		if cancel != nil {
			cancel()
		}
	}
	packets := make([][]byte, 0)
	for _, publication := range publications {
		remaining := publication.remaining
		damagePackets, publishErr := e.runtime.damage.publishAreaResults(
			e.packet, remaining.sessionKey, remaining.generation, remaining.sourceObjectID,
			e.packet.SourceTime+500, remaining.binding, publication.results, publication.transitions, nil, false,
		)
		if publishErr != nil {
			return nil, fmt.Errorf("expungePublish: %w", publishErr)
		}
		packets = append(packets, damagePackets...)
	}
	for _, deleted := range deletes {
		packet, marshalErr := effectraknet.ModifierDelete(
			deleted.targetObjectID, deleted.instanceID,
		)
		if marshalErr != nil {
			return nil, fmt.Errorf("expungeDelete: %w", marshalErr)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func consumeExpungeDamageOverTime(
	peerSession *gameplayPeerSession, targetObjectID uint32,
	modifierPool *modifierPool,
) ([]expungeRemainingDamage, []expungeModifierDelete, []func(), error) {
	damages := make([]expungeRemainingDamage, 0)
	deletes := make([]expungeModifierDelete, 0)
	cancels := make([]func(), 0)
	for key, run := range peerSession.heroHitPoisons {
		if run == nil || key.targetObjectID != targetObjectID ||
			run.completedTick >= run.definition.NumberOfTicks {
			continue
		}
		remainingTick := run.definition.NumberOfTicks - run.completedTick
		damages = append(damages, expungeRemainingDamage{
			creature:       run.creature,
			sourceObjectID: run.sourceObjectID,
			definition: expungeDamageDefinition(
				run.definition, remainingTick,
				run.definition.MinimumDamagePerTick,
				run.definition.MaximumDamagePerTick,
			),
		})
		if run.cancel != nil {
			cancels = append(cancels, run.cancel)
			run.cancel = nil
		}
		peerSession.removeHeroHitPoison(key, run)
		isCreated, err := run.modifier.release(modifierPool)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("hitPoisonRelease: %w", err)
		}
		if isCreated {
			deletes = append(deletes, expungeModifierDelete{
				targetObjectID: targetObjectID,
				instanceID:     run.modifier.instanceID,
			})
		}
	}
	if run := peerSession.heroPassivePoisons[targetObjectID]; run != nil &&
		run.completedTick < poisonRavagerTickCount {
		remainingTick := poisonRavagerTickCount - run.completedTick
		definition := poisonRavagerDefinition(run.stackCount)
		damages = append(damages, expungeRemainingDamage{
			creature:       run.creature,
			sourceObjectID: run.sourceObjectID,
			definition: expungeDamageDefinition(
				definition, remainingTick,
				definition.MinimumDamage, definition.MaximumDamage,
			),
		})
		if run.cancel != nil {
			cancels = append(cancels, run.cancel)
			run.cancel = nil
		}
		peerSession.removeHeroPassivePoison(run)
		isCreated, err := run.modifier.release(modifierPool)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("passivePoisonRelease: %w", err)
		}
		if isCreated {
			deletes = append(deletes, expungeModifierDelete{
				targetObjectID: targetObjectID,
				instanceID:     run.modifier.instanceID,
			})
		}
	}
	for projectileObjectID, run := range peerSession.heroProjectileRuns {
		if run != nil && run.definition.Name == "AfflictionBolt" {
			contact, isContactFound := run.Splash(targetObjectID)
			if !isContactFound {
				continue
			}
			remainingTick := uint32(max(
				0, len(run.definition.HitDelays)-int(contact.completedTick),
			))
			if remainingTick > 0 {
				damages = append(damages, expungeRemainingDamage{
					creature:       run.creature,
					sourceObjectID: run.sourceObjectID,
					definition: expungeDamageDefinition(
						run.definition, remainingTick,
						run.definition.MinimumDamagePerTick,
						run.definition.MaximumDamagePerTick,
					),
				})
			}
			deleted := run.RemoveSplash(targetObjectID, contact.expiresAt)
			if deleted.instanceID != 0 {
				deletes = append(deletes, expungeModifierDelete{
					targetObjectID: targetObjectID,
					instanceID:     deleted.instanceID,
				})
			}
			continue
		}
		if run == nil || run.targetID != targetObjectID || !run.IsApplied() {
			continue
		}
		run.mutex.Lock()
		remainingTick := uint32(max(0, len(run.definition.HitDelays)-int(run.completedTick)))
		definition := run.definition
		creature := run.creature
		cancel := run.cancel
		run.cancel = nil
		run.mutex.Unlock()
		if remainingTick > 0 {
			damages = append(damages, expungeRemainingDamage{
				creature:       creature,
				sourceObjectID: run.sourceObjectID,
				definition: expungeDamageDefinition(
					definition, remainingTick,
					definition.MinimumDamagePerTick,
					definition.MaximumDamagePerTick,
				),
			})
		}
		delete(peerSession.heroProjectileRuns, projectileObjectID)
		deleted := run.Cleanup()
		run.projectile.Finish()
		if cancel != nil {
			cancels = append(cancels, cancel)
		}
		if deleted.instanceID != 0 {
			deletes = append(deletes, expungeModifierDelete{
				targetObjectID: deleted.targetID,
				instanceID:     deleted.instanceID,
			})
		}
	}
	return damages, deletes, cancels, nil
}

func expungeDamageDefinition(
	source sim.AbilityDefinition, remainingTick uint32,
	minimumPerTick float32, maximumPerTick float32,
) sim.AbilityDefinition {
	return sim.AbilityDefinition{
		Name:              "LFPoisonRavager_ExpungeRemainingDamage",
		Kind:              sim.AbilityKindMelee,
		MinimumDamage:     minimumPerTick * float32(remainingTick),
		MaximumDamage:     maximumPerTick * float32(remainingTick),
		DamageCoefficient: 0.05,
		DescriptorMask:    5, DamageType: source.DamageType,
		DamageSource:      source.DamageSource,
		IsDescriptorFound: true, IsDamageTypeFound: source.IsDamageTypeFound,
		IsDamageSourceFound: source.IsDamageSourceFound,
	}
}
