package npc

import (
	"errors"
	"fmt"
	"strings"
)

const nashiraMaximumFiendCount = 12

func hasNashiraPassive(plan SpawnPlan) bool {
	graph := plan.NPCProfile.AIGraph
	return graph != nil && graph.IsResolved && graph.PassiveAbility != nil &&
		strings.EqualFold(strings.TrimSuffix(*graph.PassiveAbility, ".Ability"), "ShadowBossPassive")
}

// AddNashiraFiend admits and registers the modifier under the same lock as
// actor creation. Registration also reserves capacity until spawn publication
// succeeds; RollbackAdd removes the actor and its reservation together.
func (e *Session) AddNashiraFiend(
	plan SpawnPlan, casterObjectID, targetObjectID uint32,
) (bool, error) {
	if e == nil || casterObjectID == 0 || targetObjectID == 0 {
		return false, errors.New("fiend session unavailable")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	caster, isFound := e.npcs[casterObjectID]
	if !isFound || caster.IsDefeated {
		return false, nil
	}
	ownerObjectID := caster.Plan.OwnerObjectID
	if ownerObjectID == 0 {
		ownerObjectID = casterObjectID
	}
	owner, isOwnerFound := e.npcs[ownerObjectID]
	if !isOwnerFound || owner.IsDefeated || !owner.IsNashiraPassiveActive {
		return false, nil
	}
	count := 0
	for _, actor := range e.npcs {
		if actor.NashiraFiendOwnerObjectID == ownerObjectID && !actor.IsDefeated {
			count++
		}
	}
	if count >= nashiraMaximumFiendCount {
		return false, nil
	}
	if plan.OwnerObjectID != ownerObjectID {
		return false, errors.New("fiend owner mismatch")
	}
	profile, isFiend := NashiraFiendProfile(plan.NounName)
	if !isFiend || profile.AbilityName != plan.ActionProfile.AbilityName {
		return false, errors.New("fiend modifier unavailable")
	}
	err := e.add([]SpawnPlan{plan}, targetObjectID)
	if err != nil {
		return false, fmt.Errorf("fiendCreate: %w", err)
	}
	fiend := e.npcs[plan.ObjectID]
	fiend.NashiraFiendOwnerObjectID = ownerObjectID
	fiend.NashiraFiendCasterObjectID = casterObjectID
	// ShadowBossMinion activation registers with the owner; it does not
	// activate ShadowBossPassive on the minion.
	fiend.IsNashiraPassiveActive = false
	e.npcs[plan.ObjectID] = fiend
	return true, nil
}

// Membership is one scalar per minion, so repeated death/removal cannot
// decrement a count twice. Teardown visits only explicitly registered fiends.
func (e *Session) deactivateNashiraPassive(actor *Snapshot) []uint32 {
	actor.NashiraFiendOwnerObjectID = 0
	if !actor.IsNashiraPassiveActive {
		return nil
	}
	actor.IsNashiraPassiveActive = false
	objectIDs := make([]uint32, 0)
	for _, objectID := range e.objectIDs {
		fiend := e.npcs[objectID]
		if fiend.NashiraFiendOwnerObjectID != actor.Plan.ObjectID {
			continue
		}
		fiend.NashiraFiendOwnerObjectID = 0
		if !fiend.IsDefeated {
			fiend.HitPoint = 0
			fiend.IsDefeated = true
			fiend.TargetObjectID = 0
			fiend.TargetFaction = FactionUnknown
			fiend.TargetOwner = ActionOwner{}
			fiend.IsActionStarted = false
			fiend.ActionOwner = ActionOwner{}
			objectIDs = append(objectIDs, objectID)
		}
		e.npcs[objectID] = fiend
	}
	return objectIDs
}

func ValidateNashiraFiends(snapshots []Snapshot) error {
	actorsByID := make(map[uint32]Snapshot, len(snapshots))
	countsByOwner := make(map[uint32]int)
	for _, actor := range snapshots {
		actorsByID[actor.Plan.ObjectID] = actor
		if actor.IsNashiraPassiveActive && (!hasNashiraPassive(actor.Plan) || actor.IsDefeated || actor.HitPoint <= 0) {
			return fmt.Errorf("fiendPassive[%d]: invalid", actor.Plan.ObjectID)
		}
	}
	for _, actor := range snapshots {
		ownerObjectID := actor.NashiraFiendOwnerObjectID
		if ownerObjectID == 0 {
			continue
		}
		owner, isFound := actorsByID[ownerObjectID]
		profile, isFiend := NashiraFiendProfile(actor.Plan.NounName)
		if !isFound || !owner.IsNashiraPassiveActive || actor.IsDefeated || actor.HitPoint <= 0 ||
			actor.Plan.OwnerObjectID != ownerObjectID || actor.NashiraFiendCasterObjectID == 0 ||
			!isFiend || profile.AbilityName != actor.Plan.ActionProfile.AbilityName {
			return fmt.Errorf("fiendMember[%d]: invalid", actor.Plan.ObjectID)
		}
		countsByOwner[ownerObjectID]++
		if countsByOwner[ownerObjectID] > nashiraMaximumFiendCount {
			return fmt.Errorf("fiendBudget[%d]: exceeded", ownerObjectID)
		}
	}
	return nil
}
