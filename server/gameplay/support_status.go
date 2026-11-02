package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

// The shipped TookDamage callbacks react only to melee, including lethal hits.
func (e campaignDamageRuntime) applyNPCSupportThorns(sessionKey string, generation uint64,
	attackerObjectID uint32, timestamp uint64, result zoneability.AreaResult,
) ([][]byte, sporenet.PlayerStatDelta, error) {
	if attackerObjectID == 0 || result.Damage.Damage <= 0 || result.Damage.IsDamageImmune ||
		result.Definition.DescriptorMask&1 == 0 {
		return nil, sporenet.PlayerStatDelta{}, nil
	}
	e.registry.mutex.Lock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || member.generation != generation || member.zone == nil {
		e.registry.mutex.Unlock()
		return nil, sporenet.PlayerStatDelta{}, nil
	}
	target, isTargetFound := member.campaignNPCTarget(generation, attackerObjectID)
	if !isTargetFound || target.HitPoint <= 0 {
		e.registry.mutex.Unlock()
		return nil, sporenet.PlayerStatDelta{}, nil
	}
	packets := make([][]byte, 0)
	statDelta := sporenet.PlayerStatDelta{}
	for _, modifier := range member.zone.Effect().Snapshot() {
		if modifier.TargetObjectID != result.Snapshot.Plan.ObjectID {
			continue
		}
		index := min(max(modifier.Rank, uint32(1)), uint32(3)) - 1
		var reflectedDamage float32
		var abilityName string
		switch modifier.GUID {
		case util.HashID("ScopeThorns"):
			abilityName = "ScopeThorns"
			reflectedDamage = [...]float32{3, 4, 5}[index]
		case util.HashID("Modifier_FireThorns"):
			abilityName = "Modifier_FireThorns"
			// The authored property is .4/.8/1.2, and its callback divides by100.
			reflectedDamage = result.Damage.Damage * [...]float32{.004, .008, .012}[index]
		default:
			continue
		}
		plan := zonenpc.AttackPlan{SourceObjectID: result.Snapshot.Plan.ObjectID,
			TargetObjectID: attackerObjectID, SourcePosition: result.Snapshot.Plan.Position,
			TargetPosition: target.Position, Profile: zonenpc.ActionProfile{
				AbilityName: abilityName, DamageType: 3, DamageSource: 1,
				DescriptorMask: 1<<4 | 1<<11 | 1<<15 | 1<<18, IsDamageProfileKnown: true}}
		hitPackets, targetStatDelta, isApplied, err := e.npc.applyEnemyReflectedDamage(&member,
			generation, plan, zonenpc.AttackResult{Damage: reflectedDamage}, timestamp)
		if err != nil {
			e.registry.mutex.Unlock()
			return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("supportReflect: %w", err)
		}
		packets = append(packets, hitPackets...)
		if isApplied {
			statDelta.PVEDamageTaken += targetStatDelta.PVEDamageTaken
			if abilityName == "ScopeThorns" {
				impactPacket, impactErr := npcraknet.PositionedEffect("thorns_target_effect.ServerEventDef", target.Position)
				if impactErr != nil {
					e.registry.mutex.Unlock()
					return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("supportReflectEffect: %w", impactErr)
				}
				packets = append(packets, impactPacket)
			}
		}
	}
	e.registry.sessions[sessionKey] = member
	e.registry.mutex.Unlock()
	return packets, statDelta, nil
}

// Buff/debuff deactivation follows OnAgentDeath: the affected actor's death,
// rather than the caster's death, removes the installed modifier.
func (e *gameplayPeerSession) stopNPCSupportModifiers(targetObjectID uint32, pool *modifierPool) ([][]byte, error) {
	packets := make([][]byte, 0)
	for instanceID, run := range e.campaignNPCModifiers {
		if targetObjectID != 0 && run.record.TargetObjectID != targetObjectID {
			continue
		}
		guid := run.record.GUID
		if guid != util.HashID("ScopeThorns") && guid != util.HashID("Modifier_FireThorns") &&
			guid != util.HashID("ScopeDebuff") && guid != util.HashID("CitadelSpecialOne_EnergyDefenseDebuff") &&
			guid != util.HashID(nativeSilenceChild) && guid != util.HashID("Chilled") && guid != util.HashID("SlowNovaModifier") {
			continue
		}
		if run.cancel != nil {
			run.cancel()
		}
		delete(e.campaignNPCModifiers, instanceID)
		e.zone.Effect().Remove(instanceID)
		if guid == util.HashID(nativeSilenceChild) {
			if run.record.TargetObjectID == e.deployedObjectID && e.enemySilenceExpiresAt == run.record.ExpiresAt {
				e.enemySilenceExpiresAt = time.Time{}
			}
			e.zone.Companion().ClearSilence(run.record.TargetObjectID, run.record.ExpiresAt)
			e.zone.NPCs().ClearSilence(run.record.TargetObjectID, run.record.ExpiresAt)
		}
		isCreated, err := run.release(pool)
		if err != nil {
			return packets, fmt.Errorf("supportRelease: %w", err)
		}
		if !isCreated {
			continue
		}
		packet, err := effectraknet.ModifierDelete(run.record.TargetObjectID, instanceID)
		if err != nil {
			return packets, fmt.Errorf("supportDelete: %w", err)
		}
		packets = append(packets, packet)
	}
	for key, run := range e.campaignNPCPoisons {
		if targetObjectID != 0 && run.plan.TargetObjectID != targetObjectID {
			continue
		}
		if run.plan.Profile.AbilityName != "CastScopeBurn" && run.plan.Profile.AbilityName != "CitadelSpecialOne_BurningDOT" {
			continue
		}
		delete(e.campaignNPCPoisons, key)
		e.untrackCampaignNPCModifier(run.modifier)
		if run.cancel != nil {
			run.cancel()
		}
		isCreated, err := run.modifier.release(pool)
		if err != nil {
			return packets, fmt.Errorf("supportBurnRelease: %w", err)
		}
		if !isCreated {
			continue
		}
		packet, err := effectraknet.ModifierDelete(run.plan.TargetObjectID, run.modifier.instanceID)
		if err != nil {
			return packets, fmt.Errorf("supportBurnDelete: %w", err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

// The caller holds the registry lock and may own an updated session copy that
// has not yet been written back. Debuffs can have a different co-op caster owner.
func (e campaignNPCActionRuntime) retireNPCSupportTargetLocked(member *gameplayPeerSession,
	targetObjectID uint32,
) ([][]byte, error) {
	if member == nil || member.zone == nil || targetObjectID == 0 {
		return nil, nil
	}
	isMovementRemoved := member.hasNPCDeathMovementModifier(targetObjectID)
	packets, err := member.stopNPCSupportModifiers(targetObjectID, e.modifierPool)
	if err != nil {
		return packets, fmt.Errorf("supportLocalRetire: %w", err)
	}
	cursePackets, err := member.stopNashiraCurseTarget(targetObjectID, e.modifierPool)
	if err != nil {
		return packets, fmt.Errorf("curseLocalRetire: %w", err)
	}
	packets = append(packets, cursePackets...)
	for key, candidate := range e.registry.sessions {
		if candidate.zone != member.zone ||
			(candidate.binding.UserID == member.binding.UserID && candidate.generation == member.generation) {
			continue
		}
		isMovementRemoved = candidate.hasNPCDeathMovementModifier(targetObjectID) || isMovementRemoved
		stopPackets, stopErr := candidate.stopNPCSupportModifiers(targetObjectID, e.modifierPool)
		e.registry.sessions[key] = candidate
		if stopErr != nil {
			return packets, fmt.Errorf("supportPeerRetire: %w", stopErr)
		}
		packets = append(packets, stopPackets...)
		cursePackets, curseErr := candidate.stopNashiraCurseTarget(targetObjectID, e.modifierPool)
		e.registry.sessions[key] = candidate
		if curseErr != nil {
			return packets, fmt.Errorf("cursePeerRetire: %w", curseErr)
		}
		packets = append(packets, cursePackets...)
	}
	if isMovementRemoved {
		movementPacket, movementErr := e.retiredNPCMovementAttribute(member, targetObjectID)
		if movementErr != nil {
			return packets, fmt.Errorf("supportMovementRetire: %w", movementErr)
		}
		if movementPacket != nil {
			packets = append(packets, movementPacket)
		}
	}
	return packets, nil
}

func (e *gameplayPeerSession) hasNPCDeathMovementModifier(targetObjectID uint32) bool {
	for _, run := range e.campaignNPCModifiers {
		if run == nil || run.record.TargetObjectID != targetObjectID {
			continue
		}
		if run.record.GUID == util.HashID("Chilled") || run.record.GUID == util.HashID("SlowNovaModifier") {
			return true
		}
	}
	return false
}

// Death can be followed by resurrection of the same object. Clear the explicit
// speed projection as well as the native modifier before that object is reused.
func (e campaignNPCActionRuntime) retiredNPCMovementAttribute(member *gameplayPeerSession,
	targetObjectID uint32,
) ([]byte, error) {
	at := e.now()
	targetMember := member
	if targetObjectID != member.deployedObjectID {
		for _, candidate := range e.registry.sessions {
			if candidate.zone == member.zone && candidate.deployedObjectID == targetObjectID {
				targetMember = &candidate
				break
			}
		}
	}
	if targetObjectID == targetMember.deployedObjectID {
		if targetMember.playerMotion != nil {
			increase := max(float32(-.9), e.registry.passiveMovementIncrease(*targetMember)+targetMember.enemyMovementSpeedBuff())
			err := targetMember.playerMotion.SetSpeed(at, zonePlayerMoveSpeed*(1+increase))
			if err != nil {
				return nil, fmt.Errorf("retiredHeroMotion: %w", err)
			}
		}
		packet, err := marshalGraviticSpeed(*targetMember)
		if err != nil {
			return nil, fmt.Errorf("retiredHeroAttribute: %w", err)
		}
		return packet, nil
	}
	buff, isFound := member.zone.Companion().MovementSpeedBuff(targetObjectID, at)
	if !isFound {
		return nil, nil
	}
	packet, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
		ObjectID: targetObjectID, Value: map[uint8]float32{48: buff},
	})
	if err != nil {
		return nil, fmt.Errorf("retiredCompanionAttribute: %w", err)
	}
	return packet, nil
}
