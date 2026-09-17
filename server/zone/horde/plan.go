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

func PlanInitialWave(
	director game.CampaignDirector,
	publication game.CampaignDirectorPublication,
	firstObjectID uint32, gameID uint32,
) ([]zonenpc.SpawnPlan, uint32, error) {
	_, isHorde := InitialActorCount(publication.MarkerSetName)
	if !isHorde {
		return nil, firstObjectID, nil
	}
	plans, nextObjectID, err := PlanFirstWave(
		director, publication, firstObjectID, gameID,
	)
	if err != nil {
		return nil, firstObjectID, fmt.Errorf("initialWave: %w", err)
	}
	return plans, nextObjectID, nil
}

func PlanFirstWave(
	director game.CampaignDirector,
	publication game.CampaignDirectorPublication,
	firstObjectID uint32, gameID uint32,
) ([]zonenpc.SpawnPlan, uint32, error) {
	if !IsTrigger(publication) {
		return nil, firstObjectID, nil
	}
	actorCount, isHorde := ListenerCount(publication)
	if !isHorde {
		return nil, firstObjectID,
			errors.New("hordePlanPublication: invalid")
	}
	plans, nextObjectID, err := PlanWave(
		director, publication, firstObjectID, gameID, actorCount, 1,
	)
	if err != nil {
		return nil, firstObjectID, fmt.Errorf("firstWave: %w", err)
	}
	return plans, nextObjectID, nil
}

func PlanWave(
	director game.CampaignDirector,
	publication game.CampaignDirectorPublication,
	firstObjectID uint32, gameID uint32,
	actorCount int, waveOrdinal int,
) ([]zonenpc.SpawnPlan, uint32, error) {
	listenerCount, isHorde := ListenerCount(publication)
	if !isHorde || actorCount <= 0 || waveOrdinal <= 0 {
		return nil, firstObjectID, errors.New("hordePlanWave: invalid")
	}
	if firstObjectID == 0 ||
		actorCount > int(zoneobject.ProjectileIDStart-firstObjectID) {
		return nil, firstObjectID,
			errors.New("hordePlanObjectID: exhausted")
	}
	eligibleEntry := eligibleAgents(director)
	if len(eligibleEntry) == 0 {
		return nil, firstObjectID,
			errors.New("hordePlanAgent: no complete candidate")
	}
	authoredPosition := make([]game.Vec3, 0, listenerCount)
	authoredRotations := make([]game.Vec3, 0, listenerCount)
	for _, listener := range publication.Listeners {
		if listener.CallbackName != "HordeSpawner_Register" {
			continue
		}
		authoredPosition = append(authoredPosition, listener.Position)
		authoredRotations = append(authoredRotations, listener.Rotation)
	}
	positions := zonepopulation.GroupPositions(
		authoredPosition, actorCount,
	)
	if len(positions) != actorCount {
		return nil, firstObjectID,
			errors.New("hordePlanPosition: incomplete")
	}
	startIndex := int(
		(gameID ^ publication.TriggerMarkerID ^
			uint32(waveOrdinal*0x103)) % uint32(len(eligibleEntry)),
	)
	plans := make([]zonenpc.SpawnPlan, 0, actorCount)
	for index := 0; index < actorCount; index++ {
		entry := eligibleEntry[(startIndex+index)%len(eligibleEntry)]
		plans = append(plans, zonenpc.SpawnPlan{
			ObjectID: firstObjectID + uint32(index),
			NounName: entry.NounName, Position: positions[index],
			Rotation:      zonepopulation.GroupRotation(authoredRotations, index),
			LocusID:       publication.TriggerMarkerID,
			Kind:          sim.DirectorLocusHorde,
			MarkerSetName: publication.MarkerSetName,
			NPCProfile:    entry.NPCProfile,
		})
	}
	return plans, firstObjectID + uint32(actorCount), nil
}

func InitialActorCount(markerSetName string) (int, bool) {
	switch strings.ToLower(markerSetName) {
	case "zelems_1_ai_horde_1.markerset":
		return 3, true
	case "zelems_1_ai_horde_2.markerset":
		return 2, true
	default:
		return 0, false
	}
}

func ValidateInitialOrder(
	publication game.CampaignDirectorPublication,
	session *Session,
) error {
	_, isHorde := InitialActorCount(publication.MarkerSetName)
	if !isHorde {
		return nil
	}
	if session == nil {
		return errors.New("hordeOrder: unavailable")
	}
	if !strings.EqualFold(
		publication.MarkerSetName, "zelems_1_Ai_Horde_2.Markerset",
	) {
		return nil
	}
	if !session.IsComplete("zelems_1_Ai_Horde_1.Markerset") {
		return errors.New("hordeOrder: first horde incomplete")
	}
	return nil
}

func IsInitialDeferred(
	publication game.CampaignDirectorPublication,
	session *Session,
) bool {
	return strings.EqualFold(
		publication.MarkerSetName, "zelems_1_Ai_Horde_2.Markerset",
	) &&
		(session == nil ||
			!session.IsComplete("zelems_1_Ai_Horde_1.Markerset"))
}

func eligibleAgents(
	director game.CampaignDirector,
) []game.CampaignDirectorEntry {
	agentEntry := zonepopulation.PoolEntries(director, "agent")
	eligibleEntry := make([]game.CampaignDirectorEntry, 0, len(agentEntry))
	for _, entry := range agentEntry {
		if !entry.IsHordeLegal || entry.NounName == "" ||
			!entry.NPCProfile.IsKnown {
			continue
		}
		eligibleEntry = append(eligibleEntry, entry)
	}
	return eligibleEntry
}
