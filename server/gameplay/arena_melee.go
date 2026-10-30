package gameplay

import (
	"context"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	geometryraknet "github.com/darkspinnet/darkspin/server/zone/geometry/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// The normal melee scheduler retains its combo, hit frames, arc and movement
// grace. Only the target mutation/publication boundary differs for player hits.
func (e campaignMeleeHitStep) commitArenaMeleeLocked(current gameplayPeerSession, targets *arenaAbilityTargets,
	plan zoneability.BasicPlan, primary zonenpc.Snapshot, result zoneability.BasicResult,
	results []zoneability.AreaResult,
) ([][]byte, error) {
	schedule := e.schedule
	runtime := schedule.runtime
	additionalCount := uint32(0)
	if schedule.selection.AnimationIndex < len(schedule.selected.AnimationAdditionalTargetCounts) {
		additionalCount = schedule.selected.AnimationAdditionalTargetCounts[schedule.selection.AnimationIndex]
	}
	if additionalCount > 0 {
		additionalTargets, err := zoneability.AdditionalMeleeTargets(targets, plan.TargetObjectID,
			plan.SourcePosition, schedule.hitState.arc.Facing, schedule.selected.AdditionalTargetRange,
			schedule.selected.AdditionalTargetAngle, additionalCount)
		if err != nil {
			return nil, fmt.Errorf("arenaMeleeTargets: %w", err)
		}
		match := runtime.registry.arenaMatches[current.binding.GameID]
		for _, target := range additionalTargets {
			additionalPlan := plan
			additionalPlan.TargetObjectID = target.Plan.ObjectID
			additional, commitErr := zoneability.CommitBasic(match.random, targets, additionalPlan,
				current.binding.Creatures[current.deployedCreatureIndex], current.binding.Difficulty, runtime.program.Critical)
			if commitErr != nil {
				return nil, fmt.Errorf("arenaMeleeAdditional: %w", commitErr)
			}
			results = append(results, zoneability.AreaResult{Snapshot: target, Damage: additional.Damage,
				IsCritical: additional.IsCritical, Definition: plan.Definition})
		}
	}
	position := raknet.Vector3(primary.Plan.Position)
	facing := geometryraknet.Direction(current.playerPosition, position)
	damage := result.Damage.Damage
	if result.Damage.IsDamageImmune {
		damage = schedule.plan.Damage.Maximum
	}
	err := schedule.run.PrepareTargetHitAt(e.index, plan.TargetObjectID, !result.Damage.IsDamageImmune,
		result.Damage.PreviousHealth, damage, result.IsCritical, toSimPosition(position),
		sim.Position{X: facing.X, Y: facing.Y, Z: facing.Z})
	if err != nil {
		return nil, fmt.Errorf("arenaMeleePrepare: %w", err)
	}
	packets, err := schedule.run.Advance(context.Background(), e.delay)
	if err != nil {
		return nil, fmt.Errorf("arenaMeleeAdvance: %w", err)
	}
	packets = filterProjectileCombatPackets(packets)
	for _, hit := range results {
		hitPackets, hitErr := targets.packets([]zoneability.AreaResult{hit})
		if hitErr != nil {
			return nil, fmt.Errorf("arenaMeleeEvent: %w", hitErr)
		}
		packets = append(packets, hitPackets...)
		statusErr := targets.applyBasicStatusLocked(hit.Damage, schedule.creature, schedule.plan.SourceRank,
			schedule.packet.SourceTime+uint64(e.delay/time.Millisecond))
		if statusErr != nil && runtime.logger != nil {
			// Damage is committed; keep the hit and record an optional status failure.
			runtime.logger.Printf("Arena melee status callback failed source=%d target=%d: %v",
				schedule.sourceObjectID, hit.Damage.ObjectID, statusErr)
		}
		plasmaErr := targets.applyPlasmaModifierLocked(hit.Damage, schedule.creature,
			schedule.plan.SourceRank, schedule.selection.AnimationIndex,
			schedule.packet.SourceTime+uint64(e.delay/time.Millisecond))
		if plasmaErr != nil && runtime.logger != nil {
			runtime.logger.Printf("Arena plasma modifier failed source=%d target=%d: %v",
				schedule.sourceObjectID, hit.Damage.ObjectID, plasmaErr)
		}
		if schedule.selected.HitEffectName != "" && schedule.selected.ChainLightning.MaximumHopCount == 0 {
			impact := campaignMeleeImpact{assetName: schedule.selected.HitEffectName,
				sourceObjectID: schedule.sourceObjectID, sourcePosition: current.playerPosition}
			effectPacket, effectErr := impact.marshal(hit)
			if effectErr != nil {
				return nil, fmt.Errorf("arenaMeleeImpact: %w", effectErr)
			}
			if len(effectPacket) != 0 {
				packets = append(packets, effectPacket)
			}
		}
	}
	if schedule.selected.Name == "ShadowRavagerActive" || schedule.selected.Name == "TechRandom3" {
		targetObjectIDs := make([]uint32, 0, len(results))
		for _, hit := range results {
			if hit.Damage.IsDefeated || hit.Damage.IsDamageImmune || hit.Damage.Damage <= 0 {
				continue
			}
			targetObjectIDs = append(targetObjectIDs, hit.Damage.ObjectID)
		}
		plan := zoneability.AreaPlan{SourceObjectID: schedule.sourceObjectID,
			AbilityID: schedule.plan.AbilityID, Definition: schedule.selected}
		_, statusErr := runtime.applyArenaTargetStatusLocked(schedule.sessionKey, schedule.generation,
			schedule.sourceObjectID, schedule.packet.SourceTime+uint64(e.delay/time.Millisecond), plan, targetObjectIDs)
		if statusErr != nil && runtime.logger != nil {
			// The melee hit is committed; do not discard its damage when the
			// optional child modifier cannot be published.
			runtime.logger.Printf("Arena melee modifier failed ability=%s source=%d: %v",
				schedule.selected.Name, schedule.sourceObjectID, statusErr)
		}
	}
	if runtime.logger != nil {
		runtime.logger.Printf("Arena shared melee hit source=%d target=%d hit=%d delay=%s damage=%g",
			schedule.sourceObjectID, plan.TargetObjectID, e.index, e.delay.Round(time.Millisecond), result.Damage.Damage)
	}
	return packets, nil
}
