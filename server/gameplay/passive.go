package gameplay

import (
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/combat"
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

const (
	graspingDeadRadius                   = 8
	graspingDeadSlow                     = 0.15
	campaignSameGenetypeDamageMultiplier = 2
	missileTargetingEffectName           = "cyber_missileTempest_targetScanner.ServerEventDef"
	missileTargetingDamagePerStack       = 0.05
	missileTargetingOverdrivePerStack    = 0.08
	missileTargetingMaximumStack         = uint32(5)
)

func (e *gameplayPeerSession) startMissileTargetingPresentation(
	creatureIndex uint32, pool *attachedEffectPool,
) ([]byte, error) {
	if e == nil || creatureIndex >= uint32(len(e.binding.Creatures)) ||
		creatureIndex >= uint32(len(e.isMissileTargetingPresented)) ||
		e.isMissileTargetingPresented[creatureIndex] ||
		e.binding.Creatures[creatureIndex].PassiveAbility !=
			util.HashID("MissileTempestPassive") {
		return nil, nil
	}
	slot, isAllocated := pool.Allocate(e.deployedObjectID)
	if !isAllocated {
		return nil, nil
	}
	packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
		Slot: slot, IsForceAttached: true,
		Asset: util.HashID(missileTargetingEffectName), ObjectID: e.deployedObjectID,
	})
	if err != nil {
		if !pool.Release(e.deployedObjectID, slot) {
			return nil, fmt.Errorf("missileTargetingSlotRelease: %w", err)
		}
		return nil, fmt.Errorf("missileTargetingMarshal: %w", err)
	}
	e.missileTargetingEffectSlots[creatureIndex] = slot
	e.isMissileTargetingPresented[creatureIndex] = true
	return packet, nil
}

func (e *gameplayPeerSession) stopMissileTargetingPresentation(
	creatureIndex uint32, pool *attachedEffectPool,
) ([]byte, error) {
	if e == nil || creatureIndex >= uint32(len(e.isMissileTargetingPresented)) ||
		!e.isMissileTargetingPresented[creatureIndex] {
		return nil, nil
	}
	packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
		Slot: e.missileTargetingEffectSlots[creatureIndex], IsRemovalRequested: true,
		IsHardStop: true, ObjectID: e.deployedObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("missileTargetingStopMarshal: %w", err)
	}
	if !pool.Release(e.deployedObjectID, e.missileTargetingEffectSlots[creatureIndex]) {
		// Object teardown may have released its slots already; still send the
		// removal so the client drops any presentation it retained.
	}
	e.missileTargetingEffectSlots[creatureIndex] = 0
	e.isMissileTargetingPresented[creatureIndex] = false
	return packet, nil
}

func (s gameplayPeerSession) applySameGenetypeDamage(
	damage float32, damageType uint8, isDamageTypeKnown bool,
) float32 {
	if damage <= 0 || !isDamageTypeKnown ||
		s.deployedCreatureIndex >= uint32(len(s.binding.Creatures)) {
		return damage
	}
	creature := s.binding.Creatures[s.deployedCreatureIndex]
	genetypeDamageType, isGenetypeKnown := campaignGenetypeDamageType(
		creature.ElementType,
	)
	if !isGenetypeKnown || genetypeDamageType != damageType {
		return damage
	}
	return damage * campaignSameGenetypeDamageMultiplier
}

func campaignGenetypeDamageType(elementType string) (uint8, bool) {
	switch elementType {
	case "cyber":
		return 0, true
	case "chrono":
		return 1, true
	case "bio":
		return 2, true
	case "plasma":
		return 3, true
	case "necro":
		return 4, true
	default:
		return 0, false
	}
}

func graspingDeadMovementSpeed(
	peerSession gameplayPeerSession, npcPosition game.Vec3, movementSpeed float32,
) float32 {
	if movementSpeed <= 0 ||
		peerSession.deployedCreatureIndex >= uint32(len(peerSession.binding.Creatures)) {
		return movementSpeed
	}
	creature := peerSession.binding.Creatures[peerSession.deployedCreatureIndex]
	if creature.PassiveAbility != util.HashID("GraspingDead") ||
		peerSession.deployedHitPoint() <= 0 {
		return movementSpeed
	}
	radius, slow := float32(graspingDeadRadius), float32(graspingDeadSlow)
	if peerSession.isOverdriveActiveAt(time.Now()) {
		// Chunk448's rank-three aura radius and MovementSpeedBuff operand.
		radius, slow = 12, 0.40
	}
	if npcPosition.Sub(game.Vec3(peerSession.playerPosition)).Length() > radius {
		return movementSpeed
	}
	return movementSpeed * (1 - slow)
}

const (
	physicalDamageSource = uint8(0)
	energyDamageSource   = uint8(1)
)

const (
	tcShieldRechargeDuration = 15 * time.Second
	tcShieldBaseStrength     = float32(25)
	tcShieldDefenseOffset    = float32(100)
	tcShieldDefenseScale     = float32(0.25)
)

type tcShieldAbsorption struct {
	Damage   float32
	Absorbed float32
	Packets  [][]byte
}

func (e *gameplayPeerSession) startTCShieldRecharge(
	creatureIndex uint32, now time.Time,
) {
	if e == nil || now.IsZero() ||
		creatureIndex >= uint32(len(e.binding.Creatures)) ||
		creatureIndex >= uint32(len(e.tcShieldAmount)) ||
		e.binding.Creatures[creatureIndex].PassiveAbility !=
			util.HashID("TCShieldedSentinelPassive") {
		return
	}
	e.tcShieldAmount[creatureIndex] = 0
	e.tcShieldReadyAt[creatureIndex] = now.Add(e.tcShieldRechargeDurationAt(now))
}

func (e gameplayPeerSession) tcShieldRechargeDurationAt(now time.Time) time.Duration {
	if e.isOverdriveActiveAt(now) {
		return 10 * time.Second
	}
	return tcShieldRechargeDuration
}

func (e *gameplayPeerSession) resetTCShield(creatureIndex uint32) {
	if e == nil || creatureIndex >= uint32(len(e.tcShieldAmount)) {
		return
	}
	run := e.tcShieldRuns[creatureIndex]
	if run != nil && run.cancel != nil {
		run.cancel()
	}
	e.tcShieldRuns[creatureIndex] = nil
	e.tcShieldAmount[creatureIndex] = 0
	e.tcShieldReadyAt[creatureIndex] = time.Time{}
}

func (e *gameplayPeerSession) attachTCShield(
	creatureIndex uint32, effectPool *attachedEffectPool,
) ([]byte, error) {
	if e == nil || effectPool == nil || e.deployedObjectID == 0 ||
		creatureIndex >= uint32(len(e.isTCShieldEffectAttached)) ||
		e.isTCShieldEffectAttached[creatureIndex] {
		return nil, nil
	}
	effectSlot, isAllocated := effectPool.Allocate(e.deployedObjectID)
	if !isAllocated {
		return nil, nil
	}
	packet, err := npcraknet.ShieldEffectAsset(
		e.deployedObjectID, effectSlot, "status_shielded.ServerEventDef", false,
	)
	if err != nil {
		effectPool.Release(e.deployedObjectID, effectSlot)
		return nil, fmt.Errorf("tcShieldAttach: %w", err)
	}
	e.tcShieldEffectSlot[creatureIndex] = effectSlot
	e.isTCShieldEffectAttached[creatureIndex] = true
	return packet, nil
}

func (e *gameplayPeerSession) stopTCShield(
	creatureIndex uint32, effectPool *attachedEffectPool,
) ([][]byte, error) {
	if e == nil || creatureIndex >= uint32(len(e.isTCShieldEffectAttached)) ||
		!e.isTCShieldEffectAttached[creatureIndex] {
		return nil, nil
	}
	effectSlot := e.tcShieldEffectSlot[creatureIndex]
	e.tcShieldEffectSlot[creatureIndex] = 0
	e.isTCShieldEffectAttached[creatureIndex] = false
	if effectPool == nil {
		return nil, nil
	}
	effectPool.Release(e.deployedObjectID, effectSlot)
	packet, err := npcraknet.ShieldEffectAsset(
		e.deployedObjectID, effectSlot, "status_shielded.ServerEventDef", true,
	)
	if err != nil {
		return nil, fmt.Errorf("tcShieldDetach: %w", err)
	}
	return [][]byte{packet}, nil
}

func (e *gameplayPeerSession) absorbTCShield(
	damage float32, now time.Time, effectPool *attachedEffectPool,
) (tcShieldAbsorption, error) {
	result := tcShieldAbsorption{Damage: damage}
	if e == nil || damage <= 0 || now.IsZero() ||
		e.deployedCreatureIndex >= uint32(len(e.binding.Creatures)) ||
		e.deployedCreatureIndex >= uint32(len(e.tcShieldAmount)) {
		return result, nil
	}
	creatureIndex := e.deployedCreatureIndex
	creature := e.binding.Creatures[creatureIndex]
	if creature.PassiveAbility != util.HashID("TCShieldedSentinelPassive") {
		return result, nil
	}
	readyAt := e.tcShieldReadyAt[creatureIndex]
	if e.tcShieldAmount[creatureIndex] <= 0 {
		if readyAt.IsZero() {
			e.startTCShieldRecharge(creatureIndex, now)
			return result, nil
		}
		if now.Before(readyAt) {
			return result, nil
		}
		baseStrength := tcShieldBaseStrength
		if e.isOverdriveActiveAt(now) {
			baseStrength = 30
		}
		shieldStrength := baseStrength +
			(creature.EnergyDefense-tcShieldDefenseOffset)*tcShieldDefenseScale
		if shieldStrength <= 0 {
			e.tcShieldReadyAt[creatureIndex] = now.Add(e.tcShieldRechargeDurationAt(now))
			return result, nil
		}
		e.tcShieldAmount[creatureIndex] = shieldStrength
		e.tcShieldReadyAt[creatureIndex] = time.Time{}
		generationPacket, err := raknet.MarshalApplication(raknet.ObjectEffectMessage{
			Asset:    util.HashID("cyber_shield_generate_shield.ServerEventDef"),
			ObjectID: e.deployedObjectID,
		})
		if err != nil {
			return tcShieldAbsorption{}, fmt.Errorf("tcShieldGenerate: %w", err)
		}
		result.Packets = append(result.Packets, generationPacket)
		shieldPacket, err := e.attachTCShield(creatureIndex, effectPool)
		if err != nil {
			return tcShieldAbsorption{}, fmt.Errorf("tcShieldPresentation: %w", err)
		}
		if shieldPacket != nil {
			result.Packets = append(result.Packets, shieldPacket)
		}
	}
	absorbed := min(damage, e.tcShieldAmount[creatureIndex])
	e.tcShieldAmount[creatureIndex] -= absorbed
	result.Damage -= absorbed
	result.Absorbed = absorbed
	hitPacket, err := raknet.MarshalApplication(raknet.ObjectEffectMessage{
		Asset:    util.HashID("status_shield_hit.ServerEventDef"),
		ObjectID: e.deployedObjectID,
	})
	if err != nil {
		return tcShieldAbsorption{}, fmt.Errorf("tcShieldHit: %w", err)
	}
	result.Packets = append(result.Packets, hitPacket)
	if e.tcShieldAmount[creatureIndex] <= 0 {
		e.tcShieldAmount[creatureIndex] = 0
		e.tcShieldReadyAt[creatureIndex] = now.Add(e.tcShieldRechargeDurationAt(now))
		stopPackets, err := e.stopTCShield(creatureIndex, effectPool)
		if err != nil {
			return tcShieldAbsorption{}, fmt.Errorf("tcShieldDeplete: %w", err)
		}
		result.Packets = append(result.Packets, stopPackets...)
		if effectPool != nil {
			err = e.scheduleTCShieldRecharge()
			if err != nil {
				return tcShieldAbsorption{}, fmt.Errorf("tcShieldRechargeSchedule: %w", err)
			}
		}
	}
	return result, nil
}

func (s gameplayPeerSession) applyPassiveDamageReduction(
	damage float32, damageSource uint8, sourceObjectID uint32, now time.Time,
) float32 {
	if s.isHeroArrivalProtected(now) {
		return 0
	}
	if damage <= 0 || math.IsNaN(float64(damage)) || math.IsInf(float64(damage), 0) ||
		s.deployedCreatureIndex >= uint32(len(s.binding.Creatures)) {
		return damage
	}
	creature := s.projectPassiveCreature(s.deployedCreatureIndex, now)
	// Crushing Dread is selected once across the whole party below, including
	// this hero's aura, rather than stacking a personal and an allied copy.
	if creature.PassiveAbility == util.HashID("CrushingDread") {
		creature.PhysicalDamageReduction = 0
		creature.EnergyDamageReduction = 0
	}
	if creature.PassiveAuraRadius > 0 {
		isInAura := false
		if s.zone != nil && s.zone.NPCs() != nil && sourceObjectID != 0 {
			npc, isFound := s.zone.NPCs().NPC(sourceObjectID)
			isInAura = isFound && npc.Plan.Position.Sub(game.Vec3(s.playerPosition)).Length() <= creature.PassiveAuraRadius
		}
		if !isInAura {
			creature.PhysicalDamageReduction = 0
			creature.EnergyDamageReduction = 0
		}
	}
	reduction := s.passiveDamageReduction(now)
	if s.deployedObjectID == s.roarReductionObjectID &&
		now.Before(s.roarReductionExpiresAt) {
		reduction += 0.25
	}
	switch damageSource {
	case physicalDamageSource:
		reduction += creature.PhysicalDamageReduction
	case energyDamageSource:
		reduction += creature.EnergyDamageReduction
	}
	return combat.ApplyDamageReduction(damage, reduction)
}

func (r campaignNPCActionRuntime) applyCrushingDreadReduction(
	peerSession gameplayPeerSession,
	sourceObjectID uint32, damage float32, damageSource uint8,
) float32 {
	if damage <= 0 || peerSession.zone == nil || sourceObjectID == 0 {
		return damage
	}
	npc, isFound := peerSession.zone.NPCs().NPC(sourceObjectID)
	if !isFound {
		return damage
	}
	reduction := peerSession.crushingDreadReduction(npc.Plan.Position, damageSource)
	for _, candidate := range r.registry.sessions {
		if candidate.zone != peerSession.zone ||
			(candidate.binding.UserID == peerSession.binding.UserID &&
				candidate.generation == peerSession.generation) {
			continue
		}
		reduction = max(reduction, candidate.crushingDreadReduction(npc.Plan.Position, damageSource))
	}
	return combat.ApplyDamageReduction(damage, reduction)
}

func (e gameplayPeerSession) crushingDreadReduction(sourcePosition game.Vec3, damageSource uint8) float32 {
	if e.deployedHitPoint() <= 0 || e.deployedCreatureIndex >= uint32(len(e.binding.Creatures)) {
		return 0
	}
	creature := e.projectPassiveCreature(e.deployedCreatureIndex, time.Now())
	if creature.PassiveAbility != util.HashID("CrushingDread") ||
		creature.PassiveAuraRadius <= 0 ||
		sourcePosition.Sub(game.Vec3(e.playerPosition)).Length() > creature.PassiveAuraRadius {
		return 0
	}
	switch damageSource {
	case physicalDamageSource:
		return creature.PhysicalDamageReduction
	case energyDamageSource:
		return creature.EnergyDamageReduction
	default:
		return 0
	}
}

func (s gameplayPeerSession) passiveDamageReduction(now time.Time) float32 {
	reduction := float32(0)
	if s.heroDrain != nil && s.heroDrain.isProtectionActive {
		reduction += s.heroDrain.damageReduction
	}
	if s.deployedCreatureIndex >= uint32(len(s.binding.Creatures)) ||
		s.deployedCreatureIndex >= uint32(len(s.passiveReductionStack)) ||
		now.IsZero() ||
		s.binding.Creatures[s.deployedCreatureIndex].PassiveAbility !=
			util.HashID("PlasmaSentinelPassive") ||
		!now.Before(s.passiveReductionExpiresAt[s.deployedCreatureIndex]) {
		return reduction
	}
	// Modifiers/0x0A09D13C.lua ranks 1-2 use 4%; overdrive ranks
	// 3-4 use 10%. Both retain the five-stack cap.
	damageReductionPerStack := float32(0.04)
	if s.isOverdriveActiveAt(now) {
		damageReductionPerStack = 0.10
	}
	return reduction +
		damageReductionPerStack*float32(s.passiveReductionStack[s.deployedCreatureIndex])
}

func (s *gameplayPeerSession) applyPassiveDamageTaken(now time.Time) {
	if s == nil || now.IsZero() ||
		s.deployedCreatureIndex >= uint32(len(s.binding.Creatures)) ||
		s.deployedCreatureIndex >= uint32(len(s.passiveReductionStack)) ||
		s.binding.Creatures[s.deployedCreatureIndex].PassiveAbility !=
			util.HashID("PlasmaSentinelPassive") {
		return
	}
	index := s.deployedCreatureIndex
	if !now.Before(s.passiveReductionExpiresAt[index]) {
		s.passiveReductionStack[index] = 0
	}
	s.passiveReductionStack[index] = min(
		uint32(5), s.passiveReductionStack[index]+1,
	)
	s.passiveReductionExpiresAt[index] = now.Add(6 * time.Second)
}

func (s *gameplayPeerSession) resetPassiveDamageReduction(creatureIndex uint32) {
	if s == nil || creatureIndex >= uint32(len(s.passiveReductionStack)) {
		return
	}
	s.passiveReductionStack[creatureIndex] = 0
	s.passiveReductionExpiresAt[creatureIndex] = time.Time{}
	s.stopHardenedMantle()
}

func (s *gameplayPeerSession) resetFireRavagerBasic(creatureIndex uint32) {
	if s == nil || creatureIndex >= uint32(len(s.fireRavagerBasicCount)) {
		return
	}
	s.fireRavagerBasicCount[creatureIndex] = 0
	s.fireRavagerPassive = fireRavagerPassiveState{}
}

func (s gameplayPeerSession) projectPassiveCreature(
	creatureIndex uint32, now time.Time,
) game.GameplayCreature {
	if creatureIndex >= uint32(len(s.binding.Creatures)) {
		return game.GameplayCreature{}
	}
	creature := s.binding.Creatures[creatureIndex]
	if creatureIndex != s.deployedCreatureIndex {
		return creature
	}
	if s.binding.Mode == game.ModeArena {
		creature.DamageProfile.DamageBuff -= s.arenaBasicWeakness(now)
		creature.DamageProfile.DamageBuff += s.arenaVoodooDamageBuff(now)
		creature.TimingProfile.AttackSpeed += s.arenaBasicHasteIncrease(now)
	}
	creature.TimingProfile.AttackSpeed += s.arenaSphereAttackSpeed()
	if s.isOverdriveActiveAt(now) {
		creature = projectOverdrivePassiveRank(creature)
		if creature.PassiveAbility == util.HashID("SoulRavagerPassive") &&
			creatureIndex < uint32(len(s.passiveKillStack)) {
			creature.DamageProfile.DamageBuff += float32(s.passiveKillStack[creatureIndex]) * 0.03
		}
	}
	// Enemy modifiers live in the shared zone, including those applied by
	// another peer's NPC simulation. Project them without changing base stats
	// so removal immediately restores the next attack's normal timing.
	if s.zone != nil && s.zone.Effect() != nil {
		for _, modifier := range s.zone.Effect().Snapshot() {
			if modifier.TargetObjectID == s.deployedObjectID &&
				modifier.Kind == zoneeffect.ModifierKindDebuff && modifier.AttackSpeed < 0 {
				creature.TimingProfile.AttackSpeed += modifier.AttackSpeed
			}
		}
	}
	if creature.PassiveAbility == util.HashID("LightningTempest_Passive") {
		creature.CriticalRating += chargeFieldCriticalIncrease(
			creature.CriticalRating, s.isOverdriveActiveAt(now),
		)
	}
	if creature.PassiveAbility == util.HashID("LightspeedTempestPassive") {
		healthRatio := s.passiveHealthRatio(creatureIndex)
		creature.TimingProfile.AttackSpeed +=
			healthRatio * creature.PassiveHealthAttackMax
		creature.PassiveMovementIncrease +=
			healthRatio * creature.PassiveHealthMovementMax
	}
	if s.isOverdriveActiveAt(now) {
		// Modifiers/0x1EF02FF9.lua (OverdriveModifier): additive attributes.
		creature.TimingProfile.AttackSpeed += 0.20
		creature.TimingProfile.CooldownReduction += 0.40
		switch creature.PassiveAbility {
		case util.HashID("LightningRoguePassive"):
			creature.CriticalRating += blitzOverdriveCriticalIncrease(creature)
		case util.HashID("QuantumPositioning"):
			// Chunk 430 applies EnergyDefense with kPercentChange, not a
			// direct reduction to incoming energy damage.
			creature.EnergyDefense *= 1.50
		case util.HashID("ThornBarkModifier"):
			// Chunk 188 applies its 20% physical reduction to energy too
			// when the passive activates at the overdrive rank.
			creature.EnergyDamageReduction += 0.20
		case util.HashID("ShadowRavagerPassive"):
			creature.PassiveBehindDamage = max(
				creature.PassiveBehindDamage, float32(0.30),
			)
		}
	}
	if s.plasmaWreathRun != nil && s.plasmaWreathState.isActive(now) {
		creature.EnergyDefense += s.plasmaWreathState.energyDefenseIncrease
		creature.TimingProfile.AttackSpeed += s.plasmaWreathState.attackSpeedIncrease
	}
	if now.IsZero() || creature.PassiveAbility != util.HashID("MissileTempestPassive") ||
		creatureIndex >= uint32(len(s.passiveStationarySince)) {
		return creature
	}
	stationarySince := s.passiveStationarySince[creatureIndex]
	if stationarySince.IsZero() || now.Before(stationarySince) {
		return creature
	}
	stackCount := min(
		missileTargetingMaximumStack,
		uint32(now.Sub(stationarySince)/time.Second),
	)
	damagePerStack := float32(missileTargetingDamagePerStack)
	if s.isOverdriveActiveAt(now) {
		damagePerStack = missileTargetingOverdrivePerStack
	}
	creature.DamageProfile.DamageBuff += damagePerStack * float32(stackCount)
	return creature
}

func (e *gameplaySessionRegistry) projectPassiveCreature(
	peerSession gameplayPeerSession, creatureIndex uint32, now time.Time,
) game.GameplayCreature {
	creature := peerSession.projectPassiveCreature(creatureIndex, now)
	if e == nil || (peerSession.zone == nil && peerSession.binding.Mode != game.ModeArena) ||
		creatureIndex != peerSession.deployedCreatureIndex {
		return creature
	}
	damageIncrease := float32(0)
	healingIncrease := float32(0)
	projectileDamageIncrease := float32(0)
	projectileSpeedIncrease := float32(0)
	attackSpeedIncrease := float32(0)
	criticalRatingIncrease := float32(0)
	selfCriticalRatingIncrease := float32(0)
	if creature.PassiveAbility == util.HashID("LightningTempest_Passive") {
		selfCriticalRatingIncrease = chargeFieldCriticalIncrease(
			peerSession.binding.Creatures[creatureIndex].CriticalRating,
			peerSession.isOverdriveActiveAt(now),
		)
	}
	for _, candidate := range e.sessions {
		if !isPassiveAuraRecipient(candidate, peerSession) ||
			candidate.deployedObjectID == peerSession.deployedObjectID ||
			candidate.deployedHitPoint() <= 0 ||
			candidate.deployedCreatureIndex >=
				uint32(len(candidate.binding.Creatures)) {
			continue
		}
		aura := candidate.projectPassiveCreature(candidate.deployedCreatureIndex, now)
		if aura.PassiveAuraRadius <= 0 ||
			game.Vec3(candidate.playerPosition).Sub(
				game.Vec3(peerSession.playerPosition),
			).Length() > aura.PassiveAuraRadius {
			continue
		}
		switch aura.PassiveAbility {
		case util.HashID("RampantGrowth"):
			if creature.PassiveAbility == aura.PassiveAbility {
				continue
			}
			damageIncrease = max(
				damageIncrease, aura.DamageProfile.DamageOverTimeIncrease,
			)
			healingIncrease = max(
				healingIncrease, aura.HealingProfile.HealingOverTimeIncrease,
			)
		case util.HashID("GravityTempestPassive"):
			if creature.PassiveAbility == aura.PassiveAbility {
				continue
			}
			projectileDamageIncrease = max(
				projectileDamageIncrease, aura.DamageProfile.ProjectileDamage,
			)
			projectileSpeedIncrease = max(
				projectileSpeedIncrease,
				aura.TimingProfile.ProjectileSpeedIncrease,
			)
		case util.HashID("TimeRavagerPassiveModifier"):
			if creature.PassiveAbility == aura.PassiveAbility {
				continue
			}
			attackSpeedIncrease = max(
				attackSpeedIncrease, float32(0.10),
			)
		case util.HashID("LightningTempest_Passive"):
			// Both modifiers use kAddBest on the initiator's rating. A Lumin
			// recipient can receive a stronger allied field than its own.
			criticalRatingIncrease = max(
				criticalRatingIncrease, chargeFieldCriticalIncrease(
					aura.CriticalRating, candidate.isOverdriveActiveAt(now),
				)-selfCriticalRatingIncrease,
			)
		}
	}
	creature.DamageProfile.DamageOverTimeIncrease += damageIncrease
	creature.HealingProfile.HealingOverTimeIncrease += healingIncrease
	creature.DamageProfile.ProjectileDamage += projectileDamageIncrease
	creature.TimingProfile.ProjectileSpeedIncrease += projectileSpeedIncrease
	creature.TimingProfile.AttackSpeed += attackSpeedIncrease
	creature.CriticalRating += criticalRatingIncrease
	return creature
}

func chargeFieldCriticalIncrease(criticalRating float32, isOverdrive bool) float32 {
	// Modifiers/0x7D667E50.lua and 0xAA8F880A.lua use 50% and 75%
	// respectively. The authored overdrive critical-damage bonus is zero.
	if isOverdrive {
		return criticalRating * 0.75
	}
	return criticalRating * 0.50
}

func (e *gameplaySessionRegistry) passiveMovementIncrease(
	peerSession gameplayPeerSession,
) float32 {
	increase := peerSession.passiveMovementIncrease()
	if e == nil || (peerSession.zone == nil && peerSession.binding.Mode != game.ModeArena) ||
		peerSession.deployedCreatureIndex >= uint32(len(peerSession.binding.Creatures)) {
		return increase
	}
	creature := peerSession.binding.Creatures[peerSession.deployedCreatureIndex]
	if creature.PassiveAbility == util.HashID("TimeRavagerPassiveModifier") {
		return increase
	}
	allyIncrease := float32(0)
	for _, candidate := range e.sessions {
		if !isPassiveAuraRecipient(candidate, peerSession) ||
			candidate.deployedObjectID == peerSession.deployedObjectID ||
			candidate.deployedHitPoint() <= 0 ||
			candidate.deployedCreatureIndex >=
				uint32(len(candidate.binding.Creatures)) {
			continue
		}
		aura := candidate.projectPassiveCreature(candidate.deployedCreatureIndex, time.Now())
		if aura.PassiveAbility != util.HashID("TimeRavagerPassiveModifier") ||
			aura.PassiveAuraRadius <= 0 ||
			game.Vec3(candidate.playerPosition).Sub(
				game.Vec3(peerSession.playerPosition),
			).Length() > aura.PassiveAuraRadius {
			continue
		}
		allyIncrease = max(allyIncrease, aura.PassiveMovementIncrease)
	}
	return increase + allyIncrease
}

func isPassiveAuraRecipient(candidate, source gameplayPeerSession) bool {
	if source.binding.Mode == game.ModeArena {
		return source.isArenaCombatActive && isActivePartyRecipient(candidate, source)
	}
	return source.zone != nil && candidate.zone == source.zone
}

func (s gameplayPeerSession) passiveHealthRatio(creatureIndex uint32) float32 {
	if s.squad == nil || creatureIndex >= uint32(len(s.binding.Creatures)) {
		return 0
	}
	character, isFound := s.squad.Character(creatureIndex)
	maximumHitPoint := s.characterHitPointMaximum(creatureIndex)
	if !isFound || !character.IsAvailable || maximumHitPoint <= 0 {
		return 0
	}
	return min(max(character.HitPoints/maximumHitPoint, float32(0)), float32(1))
}

func (s gameplayPeerSession) passiveMovementIncrease() float32 {
	if s.deployedCreatureIndex >= uint32(len(s.binding.Creatures)) {
		return 0
	}
	creature := s.projectPassiveCreature(s.deployedCreatureIndex, time.Now())
	return creature.PassiveMovementIncrease
}

func (s *gameplayPeerSession) applyPassiveKill() (uint32, bool) {
	if s == nil || s.deployedCreatureIndex >= uint32(len(s.binding.Creatures)) ||
		s.deployedCreatureIndex >= uint32(len(s.passiveKillStack)) {
		return 0, false
	}
	creature := &s.binding.Creatures[s.deployedCreatureIndex]
	if creature.PassiveKillDamageIncrease <= 0 || creature.PassiveMaximumStack == 0 ||
		creature.PassiveStackPerKill == 0 {
		return 0, false
	}
	stack := s.passiveKillStack[s.deployedCreatureIndex]
	if stack >= creature.PassiveMaximumStack {
		return stack, false
	}
	stackPerKill := creature.PassiveStackPerKill
	if creature.PassiveAbility == util.HashID("SoulRavagerPassive") && s.isOverdriveActiveAt(time.Now()) {
		stackPerKill = 2
	}
	addedStack := min(stackPerKill, creature.PassiveMaximumStack-stack)
	stack += addedStack
	s.passiveKillStack[s.deployedCreatureIndex] = stack
	creature.DamageProfile.DamageBuff +=
		float32(addedStack) * creature.PassiveKillDamageIncrease
	return stack, true
}

func (s *gameplayPeerSession) resetPassiveKill(creatureIndex uint32) {
	if s == nil || creatureIndex >= uint32(len(s.binding.Creatures)) ||
		creatureIndex >= uint32(len(s.passiveKillStack)) {
		return
	}
	stack := s.passiveKillStack[creatureIndex]
	if stack == 0 {
		return
	}
	creature := &s.binding.Creatures[creatureIndex]
	bonus := float32(stack) * creature.PassiveKillDamageIncrease
	creature.DamageProfile.DamageBuff -= bonus
	s.passiveKillStack[creatureIndex] = 0
}

func (s gameplayPeerSession) projectSoulRavagerBurst(
	creature game.GameplayCreature, definition sim.AbilityDefinition,
) (game.GameplayCreature, sim.AbilityDefinition, uint32) {
	if (definition.Name != "SoulRavagerActive" &&
		definition.Name != "SoulRavagerSupport") ||
		s.deployedCreatureIndex >= uint32(len(s.passiveKillStack)) {
		return creature, definition, 0
	}
	stack := s.passiveKillStack[s.deployedCreatureIndex]
	if stack > creature.PassiveMaximumStack {
		stack = creature.PassiveMaximumStack
	}
	if stack > 0 {
		creature.DamageProfile.DamageBuff -=
			float32(stack) * creature.PassiveKillDamageIncrease
	}
	shotCount := soulRavagerShotCount(definition.Name, stack)
	definition.HitDelays = make([]time.Duration, shotCount)
	firstDelay := 100 * time.Millisecond
	interval := 10 * time.Millisecond
	if definition.Name == "SoulRavagerSupport" {
		firstDelay = 300 * time.Millisecond
		interval = 400 * time.Millisecond
	}
	for index := range definition.HitDelays {
		definition.HitDelays[index] = firstDelay + time.Duration(index)*interval
	}
	return creature, definition, stack
}

func soulRavagerShotCount(abilityName string, stack uint32) uint32 {
	if abilityName == "SoulRavagerSupport" {
		return 3 + stack/2
	}
	if stack == 0 {
		return 6
	}
	return 7 + stack
}

func (s gameplayPeerSession) soulRavagerBasicProjectileEffect() string {
	if s.deployedCreatureIndex >= uint32(len(s.passiveKillStack)) {
		return "shadow_soul_power_up_shot_1.ServerEventDef"
	}
	stack := s.passiveKillStack[s.deployedCreatureIndex]
	if stack > 3 {
		return "shadow_soul_power_up_shot_3.ServerEventDef"
	}
	if stack > 1 {
		return "shadow_soul_power_up_shot_2.ServerEventDef"
	}
	return "shadow_soul_power_up_shot_1.ServerEventDef"
}

func (s *gameplayPeerSession) consumePassiveKill(
	creatureIndex uint32, stack uint32,
) bool {
	if stack == 0 {
		return true
	}
	if s == nil || creatureIndex >= uint32(len(s.binding.Creatures)) ||
		creatureIndex >= uint32(len(s.passiveKillStack)) ||
		s.passiveKillStack[creatureIndex] < stack {
		return false
	}
	creature := &s.binding.Creatures[creatureIndex]
	creature.DamageProfile.DamageBuff -=
		float32(stack) * creature.PassiveKillDamageIncrease
	s.passiveKillStack[creatureIndex] -= stack
	return true
}

func (s *gameplayPeerSession) restorePassiveKill(
	creatureIndex uint32, stack uint32,
) {
	if s == nil || stack == 0 ||
		creatureIndex >= uint32(len(s.binding.Creatures)) ||
		creatureIndex >= uint32(len(s.passiveKillStack)) {
		return
	}
	creature := &s.binding.Creatures[creatureIndex]
	availableStack := creature.PassiveMaximumStack -
		min(creature.PassiveMaximumStack, s.passiveKillStack[creatureIndex])
	restoredStack := min(stack, availableStack)
	s.passiveKillStack[creatureIndex] += restoredStack
	creature.DamageProfile.DamageBuff +=
		float32(restoredStack) * creature.PassiveKillDamageIncrease
}

type voodooCharmReturn struct {
	manaRatio   float32
	healthRatio float32
}

func (r campaignDamageRuntime) applyVoodooCharmKill(
	sessionKey string, generation uint64, sourceObjectID uint32,
) ([][]byte, error) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.squad != nil &&
		peerSession.deployedCreatureIndex < uint32(len(peerSession.binding.Creatures))
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	if peerSession.deployedObjectID != sourceObjectID {
		packets, err := r.applyVoodooCompanionKillLocked(peerSession, sourceObjectID)
		r.registry.mutex.Unlock()
		if err != nil {
			return nil, fmt.Errorf("voodooCompanion: %w", err)
		}
		return packets, nil
	}
	creatureIndex := peerSession.deployedCreatureIndex
	charm := r.registry.voodooCharmReturn(peerSession)
	if charm.manaRatio == 0 && charm.healthRatio == 0 {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	character, isCharacterFound := peerSession.squad.Character(creatureIndex)
	maximumHitPoint, maximumManaPoint := peerSession.characterResourceMaximum(creatureIndex)
	if !isCharacterFound || !character.IsAvailable || character.HitPoints <= 0 {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	manaAward := maximumManaPoint * charm.manaRatio
	manaPoint := character.ManaPoints
	if manaAward > 0 {
		manaPoint = min(maximumManaPoint, character.ManaPoints+manaAward)
	}
	healing, err := game.ApplyTargetHealingReduction(
		maximumHitPoint*charm.healthRatio,
		peerSession.healingTargetProfile(creatureIndex),
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("voodooCharmHealing: %w", err)
	}
	hitPoint := min(maximumHitPoint, character.HitPoints+healing)
	isResourceChanged := manaPoint != character.ManaPoints || hitPoint != character.HitPoints
	packets := make([][]byte, 0, 2)
	if isResourceChanged {
		resourcePacket, resourceErr := raknet.MarshalApplication(raknet.CombatantDataUpdateMessage{
			ObjectID: sourceObjectID, HitPoints: hitPoint, ManaPoints: manaPoint,
		})
		if resourceErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("voodooCharmResource: %w", resourceErr)
		}
		packets = append(packets, resourcePacket)
	}
	// Child811 notifies on a positive computed award, even when SetManaPoints
	// clamps it to an already-full pool. Healing is a separate branch.
	if manaAward > 0 {
		effectPacket, effectErr := raknet.MarshalApplication(raknet.ObjectEffectMessage{
			Asset:    util.HashID("shadow_mana_healing_target_effect.ServerEventDef"),
			ObjectID: sourceObjectID,
		})
		if effectErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("voodooCharmEffect: %w", effectErr)
		}
		packets = append(packets, effectPacket)
	}
	if healing > 0 {
		healingPacket, healingErr := raknet.MarshalApplication(raknet.DamageCombatEventMessage{
			Flags: 0x0002, DeltaHealth: -healing,
			TargetID: sourceObjectID, SourceID: 0,
			IntegerHPChange: int32(character.HitPoints) - int32(hitPoint),
		})
		if healingErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("voodooCharmHealEvent: %w", healingErr)
		}
		packets = append(packets, healingPacket)
	}
	if !isResourceChanged {
		r.registry.mutex.Unlock()
		return packets, nil
	}
	if manaPoint != character.ManaPoints {
		err = peerSession.setCampaignCharacterManaPoints(creatureIndex, manaPoint)
		if err != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("voodooCharmMana: %w", err)
		}
	}
	if hitPoint > character.HitPoints {
		isGameOver, healErr := peerSession.setCampaignCharacterHitPoints(creatureIndex, hitPoint)
		if healErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("voodooCharmHealth: %w", healErr)
		}
		if isGameOver {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("voodooCharmHealth: living recipient marked game over")
		}
	}
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	return packets, nil
}
