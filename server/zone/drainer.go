package zone

import (
	"errors"
	"fmt"
	"math"

	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// ApplyDrainerDeathDamage is the named dead-caster damage boundary for chunk
// 621. Its caller has admitted the concrete creature type and captured team.
// Unlike AI attacks, the callback does not require AI targetability.
func (e *Zone) ApplyDrainerDeathDamage(owner zonenpc.ActionOwner, sourceObjectID,
	targetObjectID uint32, damage float32,
) (NPCTargetDamage, bool, error) {
	if e == nil || damage <= 0 || math.IsNaN(float64(damage)) || math.IsInf(float64(damage), 0) {
		return NPCTargetDamage{}, false, errors.New("drainer damage invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	member, isMemberFound := e.members[owner.UserID]
	if e.state != StateActive || !isMemberFound || member.PeerGeneration != owner.PeerGeneration {
		return NPCTargetDamage{}, false, nil
	}
	source, isSourceFound := e.info.NPCs.NPC(sourceObjectID)
	if !isSourceFound || !source.IsDefeated || !zonenpc.IsDrainerDeath(source) {
		return NPCTargetDamage{}, false, nil
	}
	actor, isHeroFound := e.info.Hero.SnapshotByObjectID(targetObjectID)
	if isHeroFound {
		if actor.HitPoint <= 0 {
			return NPCTargetDamage{}, false, nil
		}
		damageResult, isApplied, err := e.applyNPCTargetDamage(owner, sourceObjectID, targetObjectID, damage)
		if err != nil {
			return NPCTargetDamage{}, false, fmt.Errorf("drainerHero: %w", err)
		}
		return damageResult, isApplied, nil
	}
	actorCompanion, isCompanionFound := e.info.Companion.Snapshot(targetObjectID)
	if !isCompanionFound || !actorCompanion.IsCombatant {
		return NPCTargetDamage{}, false, nil
	}
	damageResult, isApplied, err := e.applyNPCCompanionDamage(owner, sourceObjectID, targetObjectID, damage, false)
	if err != nil {
		return NPCTargetDamage{}, false, fmt.Errorf("drainerCompanion: %w", err)
	}
	return damageResult, isApplied, nil
}
