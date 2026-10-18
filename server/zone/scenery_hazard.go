package zone

import (
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/game"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// ApplySceneryHazardDamage admits only a hazard in this run's selected layout.
// A scenery owner has no NPC action or health; ordinary retained NPC modifiers
// still require their original NPC source through ApplyNPCTargetStatusDamage.
func (e *Zone) ApplySceneryHazardDamage(
	owner zonenpc.ActionOwner, sourceObjectID, targetObjectID uint32,
	abilityName string, damage float32,
) (NPCTargetDamage, bool, error) {
	if e == nil || owner.UserID == 0 || owner.PeerGeneration == 0 ||
		sourceObjectID == 0 || targetObjectID == 0 || damage <= 0 ||
		math.IsNaN(float64(damage)) || math.IsInf(float64(damage), 0) {
		return NPCTargetDamage{}, false, errors.New("invalid scenery hazard damage")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != StateActive {
		return NPCTargetDamage{}, false, errors.New("campaign zone not active")
	}
	member, isFound := e.members[owner.UserID]
	if !isFound || member.PeerGeneration != owner.PeerGeneration {
		return NPCTargetDamage{}, false, nil
	}
	director := e.info.DirectorDefinition
	marker, isFound := director.SelectedMarkerDefinition(sourceObjectID)
	npc, isNPCFound := e.info.NPCs.NPC(sourceObjectID)
	isSelected := isFound && game.SceneryHazardAbility(marker.NounName) == abilityName
	isFixture := isNPCFound && npc.Plan.IsFixture &&
		game.SceneryHazardAbility(npc.Plan.NounName) == abilityName
	if !director.IsInitialLayoutSelected || abilityName == "" || (!isSelected && !isFixture) {
		return NPCTargetDamage{}, false, nil
	}
	if isNPCFound && (!npc.IsPublished || npc.IsDefeated) {
		return NPCTargetDamage{}, false, nil
	}
	result, isApplied, err := e.applyNPCTargetDamage(owner, sourceObjectID, targetObjectID, damage)
	if err != nil {
		return NPCTargetDamage{}, false, fmt.Errorf("hazardTarget: %w", err)
	}
	return result, isApplied, nil
}
