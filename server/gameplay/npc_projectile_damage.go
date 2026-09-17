package gameplay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/combat"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/sporenet"
	zone "github.com/darkspinnet/darkspin/server/zone"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	companionraknet "github.com/darkspinnet/darkspin/server/zone/companion/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

func (r campaignNPCActionRuntime) applyEnemyProjectileDamage(
	peerSession *gameplayPeerSession,
	generation uint64,
	plan zonenpc.AttackPlan,
	result zonenpc.AttackResult,
	projectileRun *abilityraknet.ProjectileRun,
	attack *zone.NPCProjectileAttack,
	deadline time.Duration,
	target zone.NPCTarget,
	impactPosition sim.Position,
	facing sim.Position,
	timestamp uint64,
) ([][]byte, sporenet.PlayerStatDelta, bool, error) {
	if peerSession == nil || peerSession.zone == nil ||
		projectileRun == nil || r.now == nil {
		return nil, sporenet.PlayerStatDelta{}, false,
			errors.New("enemy projectile damage runtime unavailable")
	}
	targetPosition := impactPosition
	targetSession, targetSessionKey := r.heroTargetSession(peerSession, target)
	if target.IsHero && targetSession == nil {
		return nil, sporenet.PlayerStatDelta{}, false, nil
	}
	defenseReq := enemyDefenseRequest(plan.Profile, result.Damage, false, false)
	plan.Profile.DamageSource = defenseReq.DamageSource
	defenseFlags := uint16(0)
	if target.IsHero {
		var defenseErr error
		defenseFlags, defenseErr = targetSession.rollDefense(
			peerSession.zone.NPCRandom(), defenseReq, r.program.Critical,
		)
		if defenseErr != nil {
			return nil, sporenet.PlayerStatDelta{}, false,
				fmt.Errorf("enemyDefense: %w", defenseErr)
		}
	}
	if defenseFlags != 0 {
		feedbackPacket, feedbackErr := defenseFeedback(plan.SourceObjectID, target.ObjectID, defenseFlags)
		if feedbackErr != nil {
			return nil, sporenet.PlayerStatDelta{}, false,
				fmt.Errorf("enemyDefenseFeedback: %w", feedbackErr)
		}
		hitPackets, err := projectileRun.ResolveCollision(
			context.Background(), deadline, true, true,
			target.HitPoint, 0, false,
			targetPosition, facing,
		)

		if err != nil {
			return nil, sporenet.PlayerStatDelta{}, false,
				fmt.Errorf("enemyProjectileDodgeResolve: %w", err)
		}
		hitPackets = filterProjectileCombatPackets(hitPackets)
		packets, statDelta, err := targetSession.applyCampaignDamageHitPackets(
			hitPackets, plan.SourceObjectID, target.HitPoint, timestamp, r.now(),
		)
		if err != nil {
			return nil, sporenet.PlayerStatDelta{}, false,
				fmt.Errorf("enemyProjectileDodge: %w", err)
		}
		packets = append(packets, feedbackPacket)
		packets, statDelta, err = r.deliverHeroTargetPackets(
			peerSession, targetSession, targetSessionKey, packets, statDelta,
		)
		return packets, statDelta, false, err
	}
	if target.IsHero && (targetSession.heroModifierRun.IsDamageImmune() ||
		targetSession.heroQuantumBlink != nil) {
		hitPackets, immunityErr := projectileRun.ResolveCollision(
			context.Background(), deadline, true, true,
			target.HitPoint, 0, false, targetPosition, facing,
		)
		if immunityErr != nil {
			return nil, sporenet.PlayerStatDelta{}, false,
				fmt.Errorf("enemyProjectileImmunityResolve: %w", immunityErr)
		}
		hitPackets = filterProjectileCombatPackets(hitPackets)
		immunePacket, immunityErr := npcraknet.Immune(
			plan.SourceObjectID, target.ObjectID,
		)
		if immunityErr != nil {
			return nil, sporenet.PlayerStatDelta{}, false,
				fmt.Errorf("enemyProjectileImmuneMarshal: %w", immunityErr)
		}
		hitPackets = append(hitPackets, immunePacket)
		packets, statDelta, immunityErr := targetSession.applyCampaignDamageHitPackets(
			hitPackets, plan.SourceObjectID, target.HitPoint, timestamp, r.now(),
		)
		if immunityErr != nil {
			return nil, sporenet.PlayerStatDelta{}, false,
				fmt.Errorf("enemyProjectileImmunity: %w", immunityErr)
		}
		packets, statDelta, immunityErr = r.deliverHeroTargetPackets(
			peerSession, targetSession, targetSessionKey, packets, statDelta,
		)
		return packets, statDelta, false, immunityErr
	}
	if target.IsHero {
		result.Damage = targetSession.applySameGenetypeDamage(
			result.Damage, plan.Profile.DamageType,
			plan.Profile.IsDamageProfileKnown,
		)
	}
	var vulnerabilityRun *campaignNPCPhysicalVulnerabilityRun
	result.Damage, vulnerabilityRun = peerSession.preparePhysicalVulnerability(
		plan.TargetObjectID, result.Damage, plan.Profile.DamageSource,
	)
	result.Damage = peerSession.applyCampaignNPCEnergyVulnerability(
		plan.TargetObjectID, result.Damage, plan.Profile.DamageSource,
	)
	distribution := soulLinkDistribution{activeDamage: result.Damage}
	shieldPackets := [][]byte(nil)
	absorbedAmount := float32(0)
	defenseReq.Damage = result.Damage
	if target.IsHero {
		result.Damage = targetSession.applyPassiveDamageReduction(
			result.Damage, plan.Profile.DamageSource, plan.SourceObjectID, r.now(),
		)
	}
	result.Damage = r.applyCrushingDreadReduction(
		*peerSession, plan.SourceObjectID, result.Damage,
		plan.Profile.DamageSource,
	)
	result.Damage = r.reduceCompanionDamage(*peerSession, target, result.Damage, defenseReq)
	if target.IsHero {
		result.Damage = combat.ReduceIncomingDamage(result.Damage, targetSession.equipmentDefense(), defenseReq)
		if result.Damage <= 0 {
			feedbackPacket, feedbackErr := defenseFeedback(plan.SourceObjectID, target.ObjectID, 0x40)
			if feedbackErr != nil {
				return nil, sporenet.PlayerStatDelta{}, false, fmt.Errorf("blockedFeedback: %w", feedbackErr)
			}
			blockedPackets := [][]byte{feedbackPacket}
			collisionPackets, collisionErr := projectileRun.ResolveCollision(
				context.Background(), deadline, true, true, target.HitPoint, 0, false, targetPosition, facing,
			)
			if collisionErr != nil {
				return nil, sporenet.PlayerStatDelta{}, false, fmt.Errorf("blockedCollision: %w", collisionErr)
			}
			blockedPackets = append(filterProjectileCombatPackets(collisionPackets), feedbackPacket)
			packets, statDelta, deliveryErr := r.deliverHeroTargetPackets(
				peerSession, targetSession, targetSessionKey, blockedPackets, sporenet.PlayerStatDelta{},
			)
			if deliveryErr != nil {
				return nil, sporenet.PlayerStatDelta{}, false, fmt.Errorf("blockedDeliver: %w", deliveryErr)
			}
			return packets, statDelta, false, nil
		}
		shield, shieldErr := targetSession.absorbTCShield(
			result.Damage, r.now(), r.effectPool,
		)
		if shieldErr != nil {
			return nil, sporenet.PlayerStatDelta{}, false,
				fmt.Errorf("enemyProjectileShield: %w", shieldErr)
		}
		result.Damage = shield.Damage
		absorbedAmount = shield.Absorbed
		shieldPackets = append(shieldPackets, shield.Packets...)
		if result.Damage <= 0 {
			hitPackets, resolveErr := projectileRun.ResolveCollision(
				context.Background(), deadline, true, true,
				target.HitPoint, 0, false, targetPosition, facing,
			)
			if resolveErr != nil {
				return nil, sporenet.PlayerStatDelta{}, false,
					fmt.Errorf("enemyProjectileShieldResolve: %w", resolveErr)
			}
			hitPackets, resolveErr = replaceShieldCombatEvent(
				hitPackets, plan.SourceObjectID, target.ObjectID,
				0, shield.Absorbed, target.HitPoint, result.IsCritical,
			)
			if resolveErr != nil {
				return nil, sporenet.PlayerStatDelta{}, false,
					fmt.Errorf("enemyProjectileAbsorbReplace: %w", resolveErr)
			}
			hitPackets = append(shieldPackets, hitPackets...)
			packets, statDelta, hitErr := targetSession.applyCampaignDamageHitPackets(
				hitPackets, plan.SourceObjectID, target.HitPoint, timestamp, r.now(),
			)
			if hitErr != nil {
				return nil, sporenet.PlayerStatDelta{}, false,
					fmt.Errorf("enemyProjectileShieldHit: %w", hitErr)
			}
			packets, statDelta, hitErr = r.deliverHeroTargetPackets(
				peerSession, targetSession, targetSessionKey, packets, statDelta,
			)
			return packets, statDelta, false, hitErr
		}
		preparedDistribution, distributionErr :=
			targetSession.prepareSoulLinkDamage(result.Damage)
		if distributionErr != nil {
			return nil, sporenet.PlayerStatDelta{}, false,
				fmt.Errorf("enemyProjectileSoulLink: %w", distributionErr)
		}
		distribution = preparedDistribution
		result.Damage = distribution.activeDamage
	}
	var damage zone.NPCTargetDamage
	var isApplied bool
	var err error
	if attack != nil {
		damage, isApplied, err = attack.Damage(plan.TargetObjectID, result.Damage)
	} else {
		damage, isApplied, err = peerSession.zone.ApplyNPCTargetDamage(
			zonenpc.ActionOwner{
				UserID:         peerSession.binding.UserID,
				PeerGeneration: generation,
			},
			plan.SourceObjectID,
			plan.TargetObjectID,
			result.Damage,
			r.now(),
		)
	}
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, false,
			fmt.Errorf("enemyProjectileCommit: %w", err)
	}
	if !isApplied {
		packets, resolveErr := projectileRun.ResolveCollision(
			context.Background(), deadline, false, false,
			0, result.Damage, result.IsCritical,
			targetPosition, facing,
		)
		if resolveErr != nil {
			return nil, sporenet.PlayerStatDelta{}, false,
				fmt.Errorf("enemyProjectileReject: %w", resolveErr)
		}
		return packets, sporenet.PlayerStatDelta{}, false, nil
	}
	vulnerabilityPackets, err := peerSession.commitPhysicalVulnerability(
		plan.TargetObjectID, vulnerabilityRun, r.modifierPool,
	)
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, false,
			fmt.Errorf("enemyProjectileVulnerability: %w", err)
	}
	if damage.IsDefeated {
		peerSession.retirePhysicalVulnerability(
			plan.TargetObjectID, r.modifierPool,
		)
	}
	energyVulnerabilityPackets := [][]byte(nil)
	if damage.IsDefeated {
		energyVulnerabilityPackets, err =
			peerSession.retireCampaignNPCEnergyVulnerability(
				plan.TargetObjectID, r.modifierPool,
			)
		if err != nil {
			return nil, sporenet.PlayerStatDelta{}, false,
				fmt.Errorf("enemyProjectileEnergyVulnerability: %w", err)
		}
	}
	hitPackets, err := projectileRun.ResolveCollision(
		context.Background(), deadline, true, true,
		damage.HitPoint, result.Damage, result.IsCritical,
		targetPosition, facing,
	)
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, false,
			fmt.Errorf("enemyProjectileResolve: %w", err)
	}
	if absorbedAmount > 0 {
		hitPackets, err = replaceShieldCombatEvent(
			hitPackets, plan.SourceObjectID, target.ObjectID,
			result.Damage, absorbedAmount, damage.HitPoint, result.IsCritical,
		)
		if err != nil {
			return nil, sporenet.PlayerStatDelta{}, false,
				fmt.Errorf("enemyProjectileAbsorbReplace: %w", err)
		}
	}
	hitPackets = append(shieldPackets, hitPackets...)
	hitPackets = append(hitPackets, vulnerabilityPackets...)
	hitPackets = append(hitPackets, energyVulnerabilityPackets...)
	if !damage.IsHero {
		if damage.IsDefeated {
			deletePacket, deleteErr := companionraknet.Defeat(
				plan.TargetObjectID,
			)
			if deleteErr != nil {
				return nil, sporenet.PlayerStatDelta{}, false, deleteErr
			}
			hitPackets = append(hitPackets, deletePacket)
		}
		teleportPackets, teleportErr := r.applyEnemyProjectileTeleport(
			peerSession, plan, target, damage.HitPoint, timestamp,
		)
		if teleportErr != nil {
			return nil, sporenet.PlayerStatDelta{}, false,
				fmt.Errorf("enemyProjectileTeleport: %w", teleportErr)
		}
		hitPackets = append(hitPackets, teleportPackets...)
		return hitPackets, sporenet.PlayerStatDelta{}, !damage.IsDefeated, nil
	}
	packets, statDelta, err := r.commitHeroTargetDamage(
		peerSession, targetSession, targetSessionKey, hitPackets, damage, distribution, timestamp,
	)
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, false,
			fmt.Errorf("enemyProjectileTransition: %w", err)
	}
	packets, statDelta, err = r.commitHeroTargetReactions(
		peerSession, targetSession, targetSessionKey, packets, statDelta,
		damage, plan, timestamp,
	)
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, false,
			fmt.Errorf("enemyProjectileReaction: %w", err)
	}
	teleportPackets, err := r.applyEnemyProjectileTeleport(
		peerSession, plan, target, damage.HitPoint, timestamp,
	)
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, false,
			fmt.Errorf("enemyProjectileTeleport: %w", err)
	}
	packets = append(packets, teleportPackets...)
	return packets, statDelta, !damage.IsDefeated, nil
}
