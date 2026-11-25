package horde

import (
	"errors"
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	zoneobject "github.com/darkspinnet/darkspin/server/zone/object"
	zonepopulation "github.com/darkspinnet/darkspin/server/zone/population"
)

func planFixedWave(
	director game.CampaignDirector, publication game.CampaignDirectorPublication,
	firstObjectID uint32, waveOrdinal int, authoredPositions []game.Vec3,
	authoredRotations []game.Vec3, policy Policy,
) ([]zonenpc.SpawnPlan, uint32, error) {
	err := game.ValidateZoneHordeWaves(policy.FixedWaves, policy.WaveCount)
	if err != nil {
		return nil, firstObjectID, fmt.Errorf("fixedRoster: %w", err)
	}
	if waveOrdinal < 1 || waveOrdinal > len(policy.FixedWaves) {
		return nil, firstObjectID, errors.New("fixed wave ordinal unavailable")
	}
	enemies := policy.FixedWaves[waveOrdinal-1]
	actorCount := uint32(0)
	for _, enemy := range enemies {
		actorCount += enemy.Count
	}
	if actorCount > zoneobject.ProjectileIDStart-firstObjectID {
		return nil, firstObjectID, errors.New("fixed wave object ID exhausted")
	}
	positions := zonepopulation.GroupPositions(authoredPositions, int(actorCount))
	if len(positions) != int(actorCount) {
		return nil, firstObjectID, errors.New("fixed wave positions unavailable")
	}
	plans := make([]zonenpc.SpawnPlan, 0, actorCount)
	for _, enemy := range enemies {
		profile, isFound := director.NPCProfilesByNoun[strings.ToLower(enemy.NounName)]
		if !isFound || !profile.IsKnown || profile.HitPoint <= 0 {
			return nil, firstObjectID, fmt.Errorf("fixedProfile[%s]: unavailable", enemy.NounName)
		}
		for count := uint32(0); count < enemy.Count; count++ {
			index := len(plans)
			plans = append(plans, zonenpc.SpawnPlan{
				ObjectID: firstObjectID + uint32(index), NounName: enemy.NounName,
				Position: positions[index], Rotation: zonepopulation.GroupRotation(authoredRotations, index),
				LocusID: publication.TriggerMarkerID, Kind: sim.DirectorLocusHorde,
				MarkerSetName: publication.MarkerSetName, NPCProfile: profile,
				Introduction: zonenpc.SpawnIntroductionFloorWarp,
			})
		}
	}
	return appendFinalAgent(director, publication, firstObjectID, waveOrdinal, plans, policy)
}

func appendFinalAgent(
	director game.CampaignDirector, publication game.CampaignDirectorPublication,
	firstObjectID uint32, waveOrdinal int, plans []zonenpc.SpawnPlan, policy Policy,
) ([]zonenpc.SpawnPlan, uint32, error) {
	nextObjectID := firstObjectID + uint32(len(plans))
	if !policy.IsMutationAgentEnabled || waveOrdinal != policy.WaveCount {
		return plans, nextObjectID, nil
	}
	profile, isFound := director.NPCProfilesByNoun[strings.ToLower(zonenpc.MutationAgentNounName)]
	if !isFound || !profile.IsKnown || len(plans) == 0 {
		return nil, firstObjectID, errors.New("mutation agent profile or position unavailable")
	}
	if nextObjectID >= zoneobject.ProjectileIDStart {
		return nil, firstObjectID, errors.New("mutation agent object ID exhausted")
	}
	plans = append(plans, zonenpc.SpawnPlan{
		ObjectID: nextObjectID, NounName: zonenpc.MutationAgentNounName,
		Position: plans[0].Position, LocusID: publication.TriggerMarkerID,
		Kind: sim.DirectorLocusHorde, MarkerSetName: publication.MarkerSetName,
		NPCProfile: profile, ActionProfile: zonenpc.MutationAgentActionProfile(), IsActionKnown: true,
		Introduction: zonenpc.SpawnIntroductionFloorWarp,
	})
	return plans, nextObjectID + 1, nil
}
