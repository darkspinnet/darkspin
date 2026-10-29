package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zone "github.com/darkspinnet/darkspin/server/zone"

	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

func (e operativeCageSchedule) beginSupportChannel(producer *operativeCageProducer) ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	member, isCurrent := e.currentLocked()
	isContinuing := isCurrent && !e.state.isComplete && member.isCampaignNPCActionActiveAt(e.generation, e.state.plan.SourceObjectID, e.runtime.now())
	isTargetAlive := false
	if isCurrent {
		target, isTargetFound := member.campaignNPCTarget(e.generation, e.state.plan.TargetObjectID)
		isTargetAlive = isTargetFound && target.HitPoint > 0
	}
	isContinuing = isContinuing && isTargetAlive
	e.runtime.registry.mutex.RUnlock()
	if !isContinuing {
		if e.state.plan.Profile.AbilityName == "NocturnaSpecialHauntLockdown" && !e.state.isHauntSnared && !isTargetAlive {
			e.runtime.registry.mutex.Lock()
			e.state.isCooldownRemoved = true
			e.runtime.registry.mutex.Unlock()
		}
		return e.finish(producer)
	}
	if e.state.plan.Profile.AbilityName == "FireStream" {
		return e.tickFire(producer)
	}
	if e.state.plan.Profile.AbilityName == "NocturnaSpecialHauntLockdown" && !e.state.isHauntSnared {
		plan := e.state.plan
		plan.Profile.ModifierName = "NocturnaSpecialHaunt_Snare"
		plan.Profile.ModifierDuration = 2 * time.Second
		plan.Profile.MovementSpeedBuff = -0.8999999761581421
		packets, err := e.runtime.applyCampaignNPCTimedModifier(e.packet, e.sessionKey, e.generation, plan, e.timestamp+300)
		if err != nil {
			return e.fail("hauntSnare", err, packets)
		}
		e.runtime.registry.mutex.Lock()
		e.state.isHauntSnared = true
		e.runtime.registry.mutex.Unlock()
		producer.nextStep = operativeCageBegin
		producer.nextDelay = 1700 * time.Millisecond
		return packets, nil
	}
	bindDelay := e.state.plan.Profile.HitDelay
	if e.state.plan.Profile.AbilityName == "NocturnaSpecialHauntLockdown" {
		// Chunk365 separates the .3-second snare from its two-second lockdown.
		bindDelay = 2 * time.Second
	}
	packets, err := e.runtime.applyCampaignNPCTimedModifier(e.packet, e.sessionKey, e.generation, e.state.plan, e.timestamp+uint64(bindDelay/time.Millisecond))
	if err != nil {
		return e.fail("agentCageBind", err, packets)
	}
	e.runtime.registry.mutex.RLock()
	member, isCurrent = e.currentLocked()
	isBound := isCurrent && e.state.run != nil && e.state.run.isActive()
	e.runtime.registry.mutex.RUnlock()
	if !isBound {
		return e.finish(producer)
	}
	_ = member
	// The two channels have distinct intros: Haunt2.333s, LightningPet.466667s.
	animation, err := npcraknet.AnimationState(e.state.plan.SourceObjectID, e.state.plan.Profile.LoopAnimationName, e.timestamp+uint64(e.state.plan.Profile.ReleaseDelay/time.Millisecond))
	if err != nil {
		return e.fail("agentChannelLoop", err, packets)
	}
	effectPackets, effectErr := e.attachSupportEffects()
	if effectErr != nil {
		return e.fail("supportChannelEffect", effectErr, packets)
	}
	packets = append(packets, effectPackets...)
	packets = append(packets, animation)
	producer.nextStep = operativeCageTick
	producer.nextDelay = operativeChannelStep
	if e.state.plan.Profile.AbilityName == "NocturnaSpecialHauntLockdown" {
		producer.nextDelay = 1200 * time.Millisecond
	}
	return packets, nil
}

func (e operativeCageSchedule) tickHaunt(producer *operativeCageProducer) ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	member, isCurrent := e.currentLocked()
	isContinuing := isCurrent && !e.state.isComplete && e.runtime.registry.isOperativeChannelLocked(member, e.state.plan.SourceObjectID, e.state.plan.TargetObjectID)
	if isContinuing {
		enemy, isEnemyFound := member.zone.NPCs().NPC(e.state.plan.SourceObjectID)
		target, isTargetFound := member.campaignNPCTarget(e.generation, e.state.plan.TargetObjectID)
		isContinuing = isEnemyFound && isTargetFound && enemy.Plan.Position.Sub(target.Position).Length() < 8+enemy.Plan.ActorFootprintRadius()+target.ActorFootprintRadius
	}
	if !isContinuing {
		e.runtime.registry.mutex.Unlock()
		return e.finish(producer)
	}
	e.state.hauntLoopCount++
	e.state.elapsed = 2900*time.Millisecond + time.Duration(e.state.hauntLoopCount-1)*e.state.plan.Profile.TickDuration
	e.runtime.registry.mutex.Unlock()
	packets, err := e.damage()
	if err != nil {
		return e.fail("hauntPulse", err, packets)
	}
	producer.nextStep = operativeCageTick
	producer.nextDelay = e.state.plan.Profile.TickDuration
	return packets, nil
}

// Build-103 chunks804/733 select the greatest sum of allied distances while
// stealthed (radius50), then the closest hostile within30 after revealing.
func (e campaignNPCActionRuntime) selectHauntTarget(sessionKey string, generation uint64, objectID uint32) error {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || !member.isCampaignNPCSourceActive(generation, objectID) {
		return nil
	}
	enemy, isEnemyFound := member.zone.NPCs().NPC(objectID)
	if !isEnemyFound {
		return nil
	}
	profile, isHaunt := zonenpc.HauntLockdownProfile(enemy.Plan.NounName)
	if !isHaunt || profile.AbilityName != "NocturnaSpecialHauntLockdown" {
		return nil
	}
	targets := member.zone.LiveNPCTargets()
	allies := append([]zone.NPCTarget(nil), targets...)
	for _, observer := range e.registry.sessions {
		if observer.zone == member.zone && observer.deployedObjectID != 0 && observer.deployedHitPoint() <= 0 {
			allies = append(allies, zone.NPCTarget{ObjectID: observer.deployedObjectID, Position: game.Vec3(observer.playerPosition), IsHero: true})
		}
	}
	for _, companion := range member.zone.Companion().Snapshots() {
		if companion.HitPoint <= 0 {
			allies = append(allies, zone.NPCTarget{ObjectID: companion.ObjectID, Position: companion.Position})
		}
	}
	selectedObjectID := uint32(0)
	isStealthed := member.zone.NPCs().IsStealthed(objectID)
	if isStealthed {
		maximumScore := float32(0)
		for _, target := range targets {
			if !target.IsHero || target.HitPoint <= 0 || enemy.Plan.Position.Sub(target.Position).Length() > 50 {
				continue
			}
			score := float32(0)
			for _, ally := range allies {
				if ally.ObjectID == target.ObjectID {
					continue
				}
				distance := target.Position.Sub(ally.Position).Length()
				if distance <= 50 {
					score += distance
				}
			}
			if score >= maximumScore {
				maximumScore = score
				selectedObjectID = target.ObjectID
			}
		}
	}
	if selectedObjectID == 0 {
		closestDistance := float32(30)
		for _, target := range targets {
			if !target.IsHero || target.HitPoint <= 0 {
				continue
			}
			distance := enemy.Plan.Position.Sub(target.Position).Length()
			if distance <= closestDistance {
				closestDistance = distance
				selectedObjectID = target.ObjectID
			}
		}
	}
	if selectedObjectID == 0 || selectedObjectID == enemy.TargetObjectID {
		return nil
	}
	selected, isTargetFound := member.campaignNPCTarget(generation, selectedObjectID)
	if !isTargetFound {
		return nil
	}
	replaced, isReplaced, err := member.zone.NPCs().Retarget(objectID, zonenpc.Target{ObjectID: selectedObjectID, Position: selected.Position, FootprintRadius: selected.ActorFootprintRadius, Faction: zonenpc.FactionPlayerAligned, Owner: zonenpc.ActionOwner{UserID: selected.UserID, PeerGeneration: selected.PeerGeneration}, IsAlive: true})
	if err != nil {
		return fmt.Errorf("hauntRetarget: %w", err)
	}
	if !isReplaced || replaced.TargetObjectID != selectedObjectID {
		return nil
	}
	return nil
}
func (e operativeCageSchedule) healHauntAlliesLocked(member gameplayPeerSession, damage float32) ([][]byte, error) {
	source, isSourceFound := member.zone.NPCs().NPC(e.state.plan.SourceObjectID)
	if !isSourceFound || damage <= 0 {
		return nil, nil
	}
	var packets [][]byte
	for _, ally := range member.zone.NPCs().LiveSnapshots() {
		if ally.Plan.ObjectID == source.Plan.ObjectID || ally.Plan.Position.Sub(source.Plan.Position).Length() > 10 {
			continue
		}
		healed, amount, err := member.zone.NPCs().Heal(ally.Plan.ObjectID, damage)
		if err != nil {
			return nil, fmt.Errorf("hauntAllyHeal: %w", err)
		}
		if amount <= 0 {
			continue
		}
		healPackets, err := npcraknet.HealDelta(ally.Plan.ObjectID, healed, amount)
		if err != nil {
			return nil, fmt.Errorf("hauntAllyHealMarshal: %w", err)
		}
		packets = append(packets, healPackets...)
	}
	return packets, nil
}

func revealHauntLocked(member gameplayPeerSession, objectID uint32) ([][]byte, error) {
	revealErr := member.zone.NPCs().RevealHaunt(objectID)
	if revealErr != nil {
		return nil, fmt.Errorf("hauntRevealWorld: %w", revealErr)
	}
	statePacket, err := npcraknet.StealthState(objectID, 0)
	if err != nil {
		return nil, fmt.Errorf("hauntRevealState: %w", err)
	}
	effectPacket, err := raknet.MarshalApplication(raknet.ObjectEffectMessage{Asset: util.HashID("shadow_stealther_minion_smoke_effect.ServerEventDef"), ObjectID: objectID, AttackerID: objectID})
	if err != nil {
		return nil, fmt.Errorf("hauntRevealEffect: %w", err)
	}
	return [][]byte{statePacket, effectPacket}, nil
}
