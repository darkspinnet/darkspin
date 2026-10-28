package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
)

// Shockwave keeps the campaign scheduler and authored forward hemisphere.
// The hit replaces only the NPC target and mutation boundary with match-owned
// players; accepted hits leave their four-second stun on the recipient.
func (e areaSpecialHitStep) produceArenaShockwave() ([][]byte, error) {
	schedule := e.schedule
	runtime := schedule.runtime
	runtime.registry.mutex.Lock()
	member, isFound := runtime.registry.sessions[schedule.sessionKey]
	match := runtime.registry.arenaMatches[schedule.binding.GameID]
	if !e.schedule.isCurrent(member, isFound) || member.binding.Mode != game.ModeArena ||
		member.deployedObjectID != schedule.sourceObjectID || match == nil || !match.isCombatActive {
		runtime.registry.mutex.Unlock()
		return nil, nil
	}
	targets := runtime.registry.abilityTargetsLocked(runtime, member, schedule.definition,
		schedule.packet.SourceTime+uint64(schedule.plan.Definition.HitDelay/time.Millisecond)).(*arenaAbilityTargets)
	livePlan, err := zoneability.PlanShockwave(targets, schedule.sourceObjectID,
		schedule.targetObjectID, game.Vec3(member.playerPosition), schedule.targetPosition,
		schedule.creature, schedule.definition)
	if err != nil {
		runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaShockwavePlan: %w", err)
	}
	results, err := zoneability.CommitArea(match.random, targets, livePlan,
		schedule.creature, member.binding.Difficulty, runtime.program.Critical)
	if err != nil {
		runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaShockwaveDamage: %w", err)
	}
	effect := areaSpecialEffect{assetID: util.HashID(livePlan.Definition.HitEffectName)}
	packets, err := targets.packetsWithEffects(results, effect.produce, true)
	if err != nil {
		runtime.registry.mutex.Unlock()
		targets.finish()
		return nil, fmt.Errorf("arenaShockwavePackets: %w", err)
	}
	objectIDs := make([]uint32, 0, len(results))
	for _, result := range results {
		if result.Damage.IsDefeated || result.Damage.IsDamageImmune ||
			result.Damage.IsShieldStarted || result.Damage.IsTurtleStarted || result.Damage.Damage <= 0 {
			continue
		}
		objectIDs = append(objectIDs, result.Damage.ObjectID)
	}
	stun := livePlan
	stun.Definition.StatusKind = sim.AbilityStatusKindStun
	stun.Definition.StatusDuration = campaignShockwaveStunDuration
	stun.Definition.RootModifierID = util.HashID("EnergySentinelStun")
	_, statusErr := runtime.applyArenaTargetStatusLocked(schedule.sessionKey, schedule.generation,
		schedule.sourceObjectID, schedule.packet.SourceTime+uint64(schedule.plan.Definition.HitDelay/time.Millisecond),
		stun, objectIDs)
	if statusErr != nil && runtime.logger != nil {
		// A presentation failure cannot reverse already committed player damage.
		runtime.logger.Printf("Arena Shockwave stun failed source=%d: %v", schedule.sourceObjectID, statusErr)
	}
	runtime.registry.mutex.Unlock()
	targets.finish()
	return packets, nil
}
