package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/zone"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// A status is selected once, at accepted contact, against the struck object.
// Its existing modifier operation runs after the collision lock is released.
type campaignNPCProjectileStatus struct {
	plan      zonenpc.AttackPlan
	timestamp uint64
	isStun    bool
	isSilence bool
	isFear    bool
}

func (e campaignNPCProjectileStep) selectStatusLocked(
	current *gameplayPeerSession, plan zonenpc.AttackPlan, isEligible bool,
) *campaignNPCProjectileStatus {
	if !isEligible || plan.Profile.ModifierName == "" {
		return nil
	}
	if e.schedule.runtime.isTargetStatusImmuneLocked(current, plan.TargetObjectID) {
		return nil
	}
	isStun := plan.Profile.ModifierName == "StalkerShock"
	isSilence := plan.Profile.ModifierName == "NocturnaBasicRanged_SilenceModifier"
	isFear := plan.Profile.ModifierName == "NocturnaSpecialHomerFear"
	if !isStun && !isSilence && !isFear && !isCampaignNPCDamageOverTimeProfile(plan.Profile) {
		return nil
	}
	// StalkerShock always rolls its chance; damage-over-time profiles with no
	// positive chance apply unconditionally, preserving the existing rules.
	if isStun || (!isSilence && !isFear && plan.Profile.ModifierChance > 0) {
		draw := current.zone.NPCRandom().Float64() * 100
		if draw >= float64(plan.Profile.ModifierChance) {
			return nil
		}
	}
	return &campaignNPCProjectileStatus{
		plan: plan, isStun: isStun, isSilence: isSilence, isFear: isFear,
		timestamp: e.schedule.timestamp + uint64(e.deadline/time.Millisecond),
	}
}

func (e campaignNPCProjectileStatus) apply(schedule campaignNPCProjectileSchedule) ([][]byte, error) {
	if e.isFear {
		packets, err := schedule.runtime.applyCampaignNPCFear(
			schedule.packet, schedule.sessionKey, schedule.generation,
			schedule.source, zone.NPCTarget{ObjectID: e.plan.TargetObjectID}, e.plan.Profile, e.timestamp,
		)
		if err != nil {
			return nil, fmt.Errorf("projectileFear: %w", err)
		}
		return packets, nil
	}
	if e.isSilence {
		packets, err := schedule.runtime.applyCampaignNPCSilence(
			schedule.packet, schedule.sessionKey, schedule.generation, e.plan, e.timestamp,
		)
		if err != nil {
			return nil, fmt.Errorf("projectileSilence: %w", err)
		}
		return packets, nil
	}
	if e.isStun {
		packets, err := schedule.runtime.applyCampaignNPCTimedModifier(
			schedule.packet, schedule.sessionKey, schedule.generation, e.plan, e.timestamp,
		)
		if err != nil {
			return nil, fmt.Errorf("projectileStun: %w", err)
		}
		return packets, nil
	}
	packets, err := schedule.runtime.applyCampaignNPCPoison(
		schedule.packet, schedule.sessionKey, schedule.generation, e.plan, e.timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("projectileStatus: %w", err)
	}
	return packets, nil
}
