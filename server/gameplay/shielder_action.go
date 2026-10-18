package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/zone"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func isNomadShielderBashTarget(enemy zonenpc.Snapshot, target zone.NPCTarget) bool {
	if !enemy.IsShieldActive || target.HitPoint <= 0 {
		return false
	}
	profile, isFound := zonenpc.ActionProfileForPlan(enemy.Plan)
	if !isFound || profile.AbilityName != "NomadShielderBash" {
		return false
	}
	// Bash inherits the melee template's ninety-degree hit arc. Restrict
	// admission to its actual attack reach so it never pursues with a shield.
	return isCampaignConeTarget(enemy.Plan.Position, enemy.Facing, target,
		profile.Range+enemy.Plan.ActorFootprintRadius(), 90)
}

func (e campaignNPCActionRuntime) produceShieldedNomadAction(
	packet raknet.Packet, sessionKey string, generation uint64,
	enemy zonenpc.Snapshot, target zone.NPCTarget, isTargetFound bool,
	timestamp uint64,
) ([][]byte, bool, error) {
	if isTargetFound && isNomadShielderBashTarget(enemy, target) {
		return nil, false, nil
	}
	grenade, isGrenadeFound := zonenpc.NomadShielderGrenadeProfile(enemy.Plan.NounName)
	shield, isShieldFound := zonenpc.NomadShielderDirectionalShieldProfile(enemy.Plan.NounName)
	// PositionForAttack's IsAbleToHit requires a target within twenty metres,
	// facing within ten degrees. Leave the shield stance before repositioning.
	isPositioned := isTargetFound && isGrenadeFound && isShieldFound &&
		isCampaignConeTarget(enemy.Plan.Position, enemy.Facing, target,
			min(shield.Range, grenade.Range)+enemy.Plan.ActorFootprintRadius(), 20)
	if !isPositioned {
		e.registry.mutex.Lock()
		member, isFound := e.registry.sessions[sessionKey]
		if !isFound || member.generation != generation || member.zone == nil {
			e.registry.mutex.Unlock()
			return nil, true, nil
		}
		err := member.zone.NPCs().EndShield(enemy.Plan.ObjectID)
		e.registry.mutex.Unlock()
		if err != nil {
			return nil, true, fmt.Errorf("shielderWithdraw: %w", err)
		}
		// The shield monitor owns effect removal, stop animation, and the
		// 0.86-second recovery; its run remains registered until recovery ends.
		return nil, true, nil
	}
	if e.isNomadShielderGrenadeReady(sessionKey, generation, enemy.Plan.ObjectID, timestamp) {
		return nil, false, nil
	}
	const retryDelay = 250 * time.Millisecond
	request := campaignNPCAttackRequest{
		runtime: e, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: enemy.Plan.ObjectID,
		kind: campaignNPCAttackMelee,
	}
	step := campaignNPCStunResumeStep{
		timestamp: timestamp + uint64(retryDelay/time.Millisecond), resume: request.resume,
	}
	cancel, err := scheduleNPCProducers(e.registry, packet, []raknet.ScheduledPacketProducer{{
		Delay: retryDelay, Produce: step.produce,
	}})
	if err != nil {
		return nil, true, fmt.Errorf("shielderIdleSchedule: %w", err)
	}
	if cancel == nil {
		return nil, true, errors.New("shielder idle cancellation unavailable")
	}
	return nil, true, nil
}
