package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

// LightspeedTempestSupport's OnHitTarget scales friendly cooldowns around the
// projectile, after standard damage. Radius uses the accepted caster snapshot.
func (e campaignProjectileSchedule) applyLightspeedCooldownsLocked(center game.Vec3) ([][]byte, error) {
	if e.definition.Name != "LightspeedTempestSupport" {
		return nil, nil
	}
	radius, err := zoneability.ProjectAreaRadius(e.creature, 10)
	if err != nil {
		return nil, fmt.Errorf("cooldownRadius: %w", err)
	}
	source, isSourceFound := e.runtime.registry.sessions[e.sessionKey]
	if !isSourceFound || source.generation != e.generation {
		return nil, nil
	}
	at := e.runtime.now()
	packets := make([][]byte, 0)
	for sessionKey, candidate := range e.runtime.registry.sessions {
		if !isActivePartyRecipient(candidate, source) {
			continue
		}
		companionErr := e.scaleCompanionCooldownsLocked(sessionKey, candidate, center, radius, at)
		if companionErr != nil {
			return nil, fmt.Errorf("companionScale: %w", companionErr)
		}
		if candidate.deployedHitPoint() <= 0 ||
			zonegeometry.Distance(center, game.Vec3(candidate.playerPosition)) > radius {
			continue
		}
		snapshots, scaleErr := candidate.abilityCooldownSession().ScaleRemaining(at, float32(0.33000001311302185), candidate.abilityReleaseSession().Snapshot().End)
		if scaleErr != nil {
			return nil, fmt.Errorf("cooldownScale: %w", scaleErr)
		}
		candidatePackets := make([][]byte, 0, len(snapshots))
		for _, snapshot := range snapshots {
			abilityID := snapshot.AbilityID
			if !snapshot.IsHeroAbility {
				if snapshot.Key != zoneability.CooldownProjectileBasic {
					continue
				}
				basic, isBasicFound := e.runtime.program.PlayerBasicAbility[candidate.binding.Creatures[candidate.deployedCreatureIndex].Noun]
				if !isBasicFound {
					continue
				}
				abilityID = util.HashID(basic.Name)
			}
			packet, marshalErr := abilityraknet.Cooldown(abilityraknet.CooldownRequest{
				ObjectID: candidate.deployedObjectID, AbilityID: abilityID, Duration: max(0, snapshot.End.Sub(at)), StartTime: advanceHeroArrivalTimestamp(e.packet.SourceTime, e.startedAt, at),
			})
			if marshalErr != nil {
				return nil, fmt.Errorf("cooldownPacket: %w", marshalErr)
			}
			candidatePackets = append(candidatePackets, packet)
		}
		if sessionKey == e.sessionKey {
			packets = append(packets, candidatePackets...)
		} else {
			candidate.queueCampaignPackets(candidatePackets)
		}
		e.runtime.registry.sessions[sessionKey] = candidate
	}
	return packets, nil
}
