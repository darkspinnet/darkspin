package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
)

// The Arena branch keeps the authored point-blank cast and hit schedule while
// committing against current opponent heroes at impact rather than NPC state.
// The registry lock is held on entry and released before return.
func (e wraithActiveSchedule) produceArenaHitLocked(source gameplayPeerSession) ([][]byte, error) {
	match := e.runtime.registry.arenaMatches[source.binding.GameID]
	if match == nil || !match.isCombatActive || match.random == nil {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	timestamp := e.packet.SourceTime + uint64(e.plan.Definition.HitDelay/time.Millisecond)
	targets := e.runtime.registry.abilityTargetsLocked(e.runtime, source, e.plan.Definition, timestamp).(*arenaAbilityTargets)
	plan, err := zoneability.PlanArea(targets, e.sourceObjectID, game.Vec3(source.playerPosition),
		e.creature, e.plan.Definition)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		targets.finish()
		return nil, fmt.Errorf("arenaEmbracePlan: %w", err)
	}
	results, err := zoneability.CommitArea(match.random, targets, plan, e.creature,
		source.binding.Difficulty, e.runtime.program.Critical)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		targets.finish()
		return nil, fmt.Errorf("arenaEmbraceDamage: %w", err)
	}
	var effect func(zoneability.AreaResult) ([]byte, error)
	if plan.Definition.ImpactEffectName != "" {
		effect = areaSpecialEffect{assetID: util.HashID(plan.Definition.ImpactEffectName)}.produce
	}
	packets, err := targets.packetsWithEffects(results, effect, false)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		targets.finish()
		return nil, fmt.Errorf("arenaEmbracePublish: %w", err)
	}
	e.runtime.registry.queueArenaForcePulsePacketsLocked(source.binding.GameID, packets)
	if plan.Definition.StatusKind == sim.AbilityStatusKindFear && plan.Definition.StatusDuration > 0 {
		for _, result := range results {
			isFeared := !result.Damage.IsDefeated && !result.Damage.IsShieldStarted &&
				!result.Damage.IsTurtleStarted && !result.Damage.IsDamageImmune
			if !isFeared {
				continue
			}
			err = e.runtime.applyArenaFearLocked(source, result.Damage.ObjectID,
				util.HashID("DeathsEmbraceDebuff"), plan.Definition.StatusDuration, timestamp)
			if err != nil {
				if e.runtime.logger != nil {
					e.runtime.logger.Printf("Arena Death's Embrace committed fear failed target=%d: %v", result.Damage.ObjectID, err)
				}
			}
		}
	}
	e.runtime.registry.mutex.Unlock()
	targets.finish()
	return nil, nil
}
