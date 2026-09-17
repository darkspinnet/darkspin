package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/zone"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func (e campaignProjectileSchedule) applyDamageHealingLocked(
	sourceZone *zone.Zone, center game.Vec3, damage float32,
) ([][]byte, error) {
	if sourceZone == nil || e.definition.Name != "SupportHealerBasic" ||
		e.definition.DamageHealingFraction <= 0 ||
		e.definition.HealingRadius <= 0 || damage <= 0 {
		return nil, nil
	}
	projectedHealing, err := zoneability.ProjectHealing(
		e.creature, e.definition, damage*e.definition.DamageHealingFraction,
	)
	if err != nil {
		return nil, fmt.Errorf("supportBasicProject: %w", err)
	}
	packets := make([][]byte, 0)
	for sessionKey, targetSession := range e.runtime.registry.sessions {
		if targetSession.zone != sourceZone || targetSession.deployedObjectID == 0 ||
			targetSession.deployedCreatureIndex >=
				uint32(len(targetSession.binding.Creatures)) ||
			targetSession.deployedHitPoint() <= 0 ||
			zonegeometry.Distance(
				center, game.Vec3(targetSession.playerPosition),
			) > e.definition.HealingRadius {
			continue
		}
		healing, reductionErr := game.ApplyTargetHealingReduction(
			projectedHealing,
			targetSession.binding.Creatures[targetSession.deployedCreatureIndex].HealingTargetProfile,
		)
		if reductionErr != nil {
			return nil, fmt.Errorf("supportBasicReduction: %w", reductionErr)
		}
		maximumHitPoint := targetSession.characterHitPointMaximum(
			targetSession.deployedCreatureIndex,
		)
		previousHitPoint := targetSession.deployedHitPoint()
		hitPoint := min(maximumHitPoint, previousHitPoint+healing)
		healedAmount := hitPoint - previousHitPoint
		if healedAmount <= 0 {
			continue
		}
		_, commitErr := targetSession.setDeployedHitPoints(hitPoint)
		if commitErr != nil {
			return nil, fmt.Errorf("supportBasicCommit: %w", commitErr)
		}
		if sessionKey != e.sessionKey {
			targetSession.zone.PublishHeroResourceTo(
				targetSession.binding.UserID, targetSession.generation,
			)
		}
		healingPackets, marshalErr := abilityraknet.DamageHealing(
			e.sourceObjectID, targetSession.deployedObjectID,
			hitPoint, healedAmount,
		)
		if marshalErr != nil {
			return nil, fmt.Errorf("supportBasicMarshal: %w", marshalErr)
		}
		resourcePacket, resourceErr := targetSession.marshalCampaignCharacterResource(
			targetSession.deployedCreatureIndex,
		)
		if resourceErr != nil {
			return nil, fmt.Errorf("supportBasicResource: %w", resourceErr)
		}
		packets = append(packets, healingPackets...)
		packets = append(packets, resourcePacket)
		e.runtime.registry.sessions[sessionKey] = targetSession
	}
	for _, companion := range sourceZone.Companion().Snapshots() {
		if !companion.IsTargetable || companion.HitPoint <= 0 ||
			companion.HitPoint >= companion.MaximumHitPoint ||
			zonegeometry.Distance(center, companion.Position) >
				e.definition.HealingRadius {
			continue
		}
		hitPoint := min(
			companion.MaximumHitPoint, companion.HitPoint+projectedHealing,
		)
		healedAmount := hitPoint - companion.HitPoint
		if healedAmount <= 0 {
			continue
		}
		_, _, commitErr := sourceZone.Companion().SetHitPoint(
			companion.ObjectID, hitPoint,
		)
		if commitErr != nil {
			return nil, fmt.Errorf("supportBasicCompanionCommit: %w", commitErr)
		}
		healingPackets, marshalErr := abilityraknet.DamageHealing(
			e.sourceObjectID, companion.ObjectID, hitPoint, healedAmount,
		)
		if marshalErr != nil {
			return nil, fmt.Errorf("supportBasicCompanionMarshal: %w", marshalErr)
		}
		resourcePacket, resourceErr := raknet.MarshalApplication(
			raknet.CombatantDataDeltaMessage{
				ObjectID: companion.ObjectID, HitPoints: hitPoint,
				IsHitPointChanged: true,
			},
		)
		if resourceErr != nil {
			return nil, fmt.Errorf("supportBasicCompanionResource: %w", resourceErr)
		}
		if companion.UserID != e.binding.UserID ||
			companion.PeerGeneration != e.generation {
			sourceZone.PublishCompanionResourceTo(
				companion.UserID, companion.PeerGeneration, companion.ObjectID,
			)
		}
		packets = append(packets, healingPackets...)
		packets = append(packets, resourcePacket)
	}
	return packets, nil
}

func isSupportHealerBasicImpactTarget(target zonenpc.Snapshot) bool {
	return target.Plan.IsFixture ||
		target.Faction == zonenpc.FactionNonPlayerAligned
}
