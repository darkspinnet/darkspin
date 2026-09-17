package gameplay

import (
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	"github.com/darkspinnet/darkspin/server/zone"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

const (
	graspingDeadRadius                   = 8
	graspingDeadSlow                     = 0.15
	campaignSameGenetypeDamageMultiplier = 2
)

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
		npcPosition.Sub(game.Vec3(peerSession.playerPosition)).Length() > graspingDeadRadius {
		return movementSpeed
	}
	return movementSpeed * (1 - graspingDeadSlow)
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
	e.tcShieldReadyAt[creatureIndex] = now.Add(tcShieldRechargeDuration)
}

func (e *gameplayPeerSession) resetTCShield(creatureIndex uint32) {
	if e == nil || creatureIndex >= uint32(len(e.tcShieldAmount)) {
		return
	}
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
		shieldStrength := tcShieldBaseStrength +
			(creature.EnergyDefense-tcShieldDefenseOffset)*tcShieldDefenseScale
		if shieldStrength <= 0 {
			e.tcShieldReadyAt[creatureIndex] = now.Add(tcShieldRechargeDuration)
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
		e.tcShieldReadyAt[creatureIndex] = now.Add(tcShieldRechargeDuration)
		stopPackets, err := e.stopTCShield(creatureIndex, effectPool)
		if err != nil {
			return tcShieldAbsorption{}, fmt.Errorf("tcShieldDeplete: %w", err)
		}
		result.Packets = append(result.Packets, stopPackets...)
	}
	return result, nil
}

func (s gameplayPeerSession) applyPassiveDamageReduction(
	damage float32, damageSource uint8, sourceObjectID uint32, now time.Time,
) float32 {
	if damage <= 0 || math.IsNaN(float64(damage)) || math.IsInf(float64(damage), 0) ||
		s.deployedCreatureIndex >= uint32(len(s.binding.Creatures)) {
		return damage
	}
	creature := s.binding.Creatures[s.deployedCreatureIndex]
	if s.isOverdriveActiveAt(now) &&
		creature.PassiveAbility == util.HashID("QuantumPositioning") {
		creature.EnergyDamageReduction += 0.50
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
	reduction = min(max(reduction, float32(0)), float32(1))
	return damage * (1 - reduction)
}

func (r campaignNPCActionRuntime) applyCrushingDreadAllyReduction(
	peerSession gameplayPeerSession, target zone.NPCTarget,
	sourceObjectID uint32, damage float32, damageSource uint8,
) float32 {
	if damage <= 0 || peerSession.zone == nil || sourceObjectID == 0 {
		return damage
	}
	npc, isFound := peerSession.zone.NPCs().NPC(sourceObjectID)
	if !isFound {
		return damage
	}
	reduction := float32(0)
	for _, candidate := range r.registry.sessions {
		if candidate.zone != peerSession.zone ||
			candidate.deployedObjectID == target.ObjectID ||
			candidate.deployedHitPoint() <= 0 ||
			candidate.deployedCreatureIndex >=
				uint32(len(candidate.binding.Creatures)) {
			continue
		}
		creature := candidate.binding.Creatures[candidate.deployedCreatureIndex]
		if creature.PassiveAbility != util.HashID("CrushingDread") ||
			creature.PassiveAuraRadius <= 0 ||
			npc.Plan.Position.Sub(game.Vec3(candidate.playerPosition)).Length() >
				creature.PassiveAuraRadius {
			continue
		}
		candidateReduction := creature.PhysicalDamageReduction
		if damageSource == energyDamageSource {
			candidateReduction = creature.EnergyDamageReduction
		}
		reduction = max(reduction, candidateReduction)
	}
	return damage * (1 - min(max(reduction, float32(0)), float32(1)))
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
	return reduction +
		0.04*float32(s.passiveReductionStack[s.deployedCreatureIndex])
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
}

func (s *gameplayPeerSession) advanceFireRavagerBasic(
	creatureIndex uint32,
) bool {
	if s == nil || creatureIndex >= uint32(len(s.binding.Creatures)) ||
		creatureIndex >= uint32(len(s.fireRavagerBasicCount)) ||
		s.binding.Creatures[creatureIndex].PassiveAbility !=
			util.HashID("FireRavagerPassive") {
		return false
	}
	const useCount = uint32(4)
	s.fireRavagerBasicCount[creatureIndex]++
	if s.fireRavagerBasicCount[creatureIndex] < useCount {
		return false
	}
	s.fireRavagerBasicCount[creatureIndex] = 0
	return true
}

func (s *gameplayPeerSession) resetFireRavagerBasic(creatureIndex uint32) {
	if s == nil || creatureIndex >= uint32(len(s.fireRavagerBasicCount)) {
		return
	}
	s.fireRavagerBasicCount[creatureIndex] = 0
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
	if creature.PassiveAbility == util.HashID("LightningTempest_Passive") {
		creature.CriticalRating += creature.CriticalRating * 0.50
	}
	if creature.PassiveAbility == util.HashID("LightspeedTempestPassive") {
		healthRatio := s.passiveHealthRatio(creatureIndex)
		creature.TimingProfile.AttackSpeed +=
			healthRatio * creature.PassiveHealthAttackMax
		creature.PassiveMovementIncrease +=
			healthRatio * creature.PassiveHealthMovementMax
	}
	if s.isOverdriveActiveAt(now) {
		switch creature.PassiveAbility {
		case util.HashID("QuantumPositioning"):
			creature.EnergyDamageReduction += 0.50
		case util.HashID("ShadowRavagerPassive"):
			creature.PassiveBehindDamage = max(
				creature.PassiveBehindDamage, float32(0.30),
			)
		}
	}
	if now.IsZero() || creature.PassiveAbility != util.HashID("MissileTempestPassive") ||
		creatureIndex >= uint32(len(s.passiveStationarySince)) {
		return creature
	}
	stationarySince := s.passiveStationarySince[creatureIndex]
	if stationarySince.IsZero() || now.Before(stationarySince) {
		return creature
	}
	stackCount := min(uint32(5), uint32(now.Sub(stationarySince)/time.Second))
	creature.DamageProfile.DamageBuff += 0.05 * float32(stackCount)
	return creature
}

func (s gameplayPeerSession) isMissileTempestHoming(
	creatureIndex uint32, now time.Time,
) bool {
	if now.IsZero() || creatureIndex >= uint32(len(s.binding.Creatures)) ||
		creatureIndex >= uint32(len(s.passiveStationarySince)) {
		return false
	}
	creature := s.binding.Creatures[creatureIndex]
	if creature.PassiveAbility != util.HashID("MissileTempestPassive") {
		return false
	}
	stationarySince := s.passiveStationarySince[creatureIndex]
	return !stationarySince.IsZero() && !now.Before(stationarySince) &&
		now.Sub(stationarySince) >= 5*time.Second
}

func (e *gameplaySessionRegistry) projectPassiveCreature(
	peerSession gameplayPeerSession, creatureIndex uint32, now time.Time,
) game.GameplayCreature {
	creature := peerSession.projectPassiveCreature(creatureIndex, now)
	if e == nil || peerSession.zone == nil ||
		creatureIndex != peerSession.deployedCreatureIndex {
		return creature
	}
	damageIncrease := float32(0)
	healingIncrease := float32(0)
	projectileDamageIncrease := float32(0)
	projectileSpeedIncrease := float32(0)
	attackSpeedIncrease := float32(0)
	criticalRatingIncrease := float32(0)
	for _, candidate := range e.sessions {
		if candidate.zone != peerSession.zone ||
			candidate.deployedObjectID == peerSession.deployedObjectID ||
			candidate.deployedHitPoint() <= 0 ||
			candidate.deployedCreatureIndex >=
				uint32(len(candidate.binding.Creatures)) {
			continue
		}
		aura := candidate.binding.Creatures[candidate.deployedCreatureIndex]
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
				attackSpeedIncrease, aura.TimingProfile.AttackSpeed,
			)
		case util.HashID("LightningTempest_Passive"):
			if creature.PassiveAbility == aura.PassiveAbility {
				continue
			}
			criticalRatingIncrease = max(
				criticalRatingIncrease, aura.CriticalRating*0.50,
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

func (e *gameplaySessionRegistry) passiveMovementIncrease(
	peerSession gameplayPeerSession,
) float32 {
	increase := peerSession.passiveMovementIncrease()
	if e == nil || peerSession.zone == nil ||
		peerSession.deployedCreatureIndex >= uint32(len(peerSession.binding.Creatures)) {
		return increase
	}
	creature := peerSession.binding.Creatures[peerSession.deployedCreatureIndex]
	if creature.PassiveAbility == util.HashID("TimeRavagerPassiveModifier") {
		return increase
	}
	allyIncrease := float32(0)
	for _, candidate := range e.sessions {
		if candidate.zone != peerSession.zone ||
			candidate.deployedObjectID == peerSession.deployedObjectID ||
			candidate.deployedHitPoint() <= 0 ||
			candidate.deployedCreatureIndex >=
				uint32(len(candidate.binding.Creatures)) {
			continue
		}
		aura := candidate.binding.Creatures[candidate.deployedCreatureIndex]
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
	creature := s.binding.Creatures[s.deployedCreatureIndex]
	increase := creature.PassiveMovementIncrease
	if creature.PassiveAbility == util.HashID("LightspeedTempestPassive") {
		increase += s.passiveHealthRatio(s.deployedCreatureIndex) *
			creature.PassiveHealthMovementMax
	}
	return increase
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
	addedStack := min(creature.PassiveStackPerKill, creature.PassiveMaximumStack-stack)
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

const voodooCharmManaReturn = float32(0.02)

func (r campaignDamageRuntime) applyVoodooCharmKill(
	sessionKey string, generation uint64, sourceObjectID uint32,
) ([][]byte, error) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.deployedObjectID == sourceObjectID && peerSession.squad != nil &&
		peerSession.deployedCreatureIndex < uint32(len(peerSession.binding.Creatures))
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	creatureIndex := peerSession.deployedCreatureIndex
	if !r.registry.isVoodooCharmEligible(peerSession) {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	character, isCharacterFound := peerSession.squad.Character(creatureIndex)
	_, maximumManaPoint := peerSession.characterResourceMaximum(creatureIndex)
	if !isCharacterFound || !character.IsAvailable || character.HitPoints <= 0 ||
		maximumManaPoint <= 0 || character.ManaPoints >= maximumManaPoint {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	manaPoint := min(
		maximumManaPoint,
		character.ManaPoints+maximumManaPoint*voodooCharmManaReturn,
	)
	err := peerSession.setCampaignCharacterManaPoints(creatureIndex, manaPoint)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("voodooCharmMana: %w", err)
	}
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	manaPacket, err := abilityraknet.Mana(sourceObjectID, manaPoint)
	if err != nil {
		return nil, fmt.Errorf("voodooCharmResource: %w", err)
	}
	effectPacket, err := raknet.MarshalApplication(raknet.ObjectEffectMessage{
		Asset:    util.HashID("shadow_mana_healing_target_effect.ServerEventDef"),
		ObjectID: sourceObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("voodooCharmEffect: %w", err)
	}
	return [][]byte{manaPacket, effectPacket}, nil
}

func (e *gameplaySessionRegistry) isVoodooCharmEligible(
	peerSession gameplayPeerSession,
) bool {
	if e == nil || peerSession.zone == nil {
		return false
	}
	for _, candidate := range e.sessions {
		if candidate.zone != peerSession.zone || candidate.deployedHitPoint() <= 0 ||
			candidate.deployedCreatureIndex >=
				uint32(len(candidate.binding.Creatures)) {
			continue
		}
		aura := candidate.binding.Creatures[candidate.deployedCreatureIndex]
		if aura.PassiveAbility != util.HashID("VoodooTempestPassive") ||
			aura.PassiveAuraRadius <= 0 {
			continue
		}
		distance := game.Vec3(candidate.playerPosition).Sub(
			game.Vec3(peerSession.playerPosition),
		).Length()
		if distance <= aura.PassiveAuraRadius {
			return true
		}
	}
	return false
}
