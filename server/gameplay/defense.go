package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/combat"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/zone"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func (e campaignNPCActionRuntime) reduceCompanionDamage(session gameplayPeerSession,
	target zone.NPCTarget, damage float32, source uint8,
) float32 {
	if target.IsHero || session.zone == nil || session.zone.Companion() == nil {
		return damage
	}
	companion, isFound := session.zone.Companion().Snapshot(target.ObjectID)
	if !isFound {
		return damage
	}
	profile := e.program.NonPlayerDefenses[companion.Noun]
	return session.zone.NPCs().ReduceCompanionDamage(damage, profile, uint32(source))
}

func (e gameplayPeerSession) equipmentDefense() combat.DefenseProfile {
	if e.deployedCreatureIndex >= uint32(len(e.binding.Creatures)) {
		return combat.DefenseProfile{}
	}
	creature := e.binding.Creatures[e.deployedCreatureIndex]
	attributes := creature.PartAttribute
	profile := combat.DefenseProfile{
		DodgeRating: creature.PhysicalDefense, ResistRating: creature.EnergyDefense,
		DamageReduction: attributes[6], PhysicalReduction: attributes[8],
		EnergyReduction: attributes[54], AreaResistance: attributes[27],
		PhysicalArmor: attributes[102], EnergyArmor: attributes[103],
	}
	copy(profile.ScienceResistances[:], attributes[43:48])
	return profile
}

func enemyDefenseRequest(profile zonenpc.ActionProfile, damage float32, isArea, isPeriodic bool) combat.DefenseRequest {
	// Unprofiled NPC attacks use the same physical fallback as resolveAttackDamage.
	if !profile.IsDamageProfileKnown {
		profile.DamageSource = 0
	}
	return combat.DefenseRequest{
		Damage: damage, DamageSource: profile.DamageSource, DamageType: profile.DamageType,
		IsSourceKnown: true, IsTypeKnown: profile.IsDamageProfileKnown,
		IsArea:     isArea || profile.DescriptorMask&8 != 0,
		IsPeriodic: isPeriodic || profile.DescriptorMask&4 != 0,
	}
}

func (e gameplayPeerSession) rollDefense(random *sim.SimulatorRandom,
	req combat.DefenseRequest, tuning sim.CriticalTuning,
) (uint16, error) {
	if e.heroModifierRun.IsDamageImmune() || e.heroQuantumBlink != nil {
		return 0, nil
	}
	if e.ghostFormRun != nil && e.ghostFormRun.IsActive(e.deployedCreatureIndex, e.deployedHitPoint() > 0) {
		return 0x20, nil
	}
	isAvoided, err := combat.RollAvoidance(random, e.equipmentDefense(), req,
		e.binding.Difficulty, tuning.RatingConversions)
	if err != nil {
		return 0, fmt.Errorf("heroAvoidance: %w", err)
	}
	if !isAvoided {
		return 0, nil
	}
	if req.DamageSource == energyDamageSource {
		return 0x80, nil
	}
	return 0x20, nil
}

func defenseFeedback(sourceObjectID, targetObjectID uint32, flags uint16) ([]byte, error) {
	packet, err := raknet.MarshalApplication(raknet.DamageCombatEventMessage{
		Flags: flags, SourceID: sourceObjectID, TargetID: targetObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("defenseEvent: %w", err)
	}
	return packet, nil
}
