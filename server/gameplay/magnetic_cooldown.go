package gameplay

import (
	"fmt"
	"time"

	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// Lua chunks 387/533 reset the OTHER ability's cooldown before casting their
// own ability. Native ResetAbilityCooldown (sub_A66830 -> sub_9DFC70) sets its
// deadline to now + that ability's ranked cooldown; it does not clear it.
// Both timers therefore share the last cast's start but retain distinct lengths.
func (e campaignNPCPushPullSchedule) selectedCooldownDelay(
	timestamp uint64,
) (time.Duration, bool, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || !member.isCampaignNPCSourceGenerationActive(
		e.generation, e.objectID, e.plan.ActionGeneration,
	) {
		return 0, false, nil
	}
	enemy, isEnemyFound := member.zone.NPCs().NPC(e.objectID)
	if !isEnemyFound || enemy.IsDefeated {
		return 0, false, nil
	}
	member, err := e.runtime.pursuit.advanceTargetPoseLocked(member, enemy.TargetObjectID)
	if err != nil {
		return 0, false, fmt.Errorf("cooldownTargetPose: %w", err)
	}
	target, isTargetFound := member.campaignNPCTarget(e.generation, enemy.TargetObjectID)
	if !isTargetFound {
		// The ordinary continuation owns target loss and action retirement.
		return 0, true, nil
	}
	profile, err := zonenpc.ZelemSpecialTwoActionProfileForPlan(
		enemy.Plan, zonegeometry.Distance(enemy.Plan.Position, target.Position),
	)
	if err != nil {
		return 0, false, fmt.Errorf("cooldownProfile: %w", err)
	}
	pushProfile, err := zonenpc.ZelemSpecialTwoActionProfileForPlan(enemy.Plan, 0)
	if err != nil {
		return 0, false, fmt.Errorf("cooldownPush: %w", err)
	}
	deadline := e.timestamp + uint64(profile.Cooldown/time.Millisecond)
	if timestamp >= deadline {
		return 0, true, nil
	}
	delay := time.Duration(deadline-timestamp) * time.Millisecond
	pushDeadline := e.timestamp + uint64(pushProfile.Cooldown/time.Millisecond)
	if timestamp < pushDeadline {
		return min(delay, time.Duration(pushDeadline-timestamp)*time.Millisecond), true, nil
	}
	// Once Push is available, keep checking distance while Pull's longer timer
	// runs. A hero entering melee range must not wait for the old Pull deadline.
	return min(delay, 250*time.Millisecond), true, nil
}
