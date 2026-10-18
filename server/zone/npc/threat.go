package npc

import (
	"errors"
	"fmt"
	"math"
	"slices"
)

// Threat is one explicit target entry. Numeric AggroType modes deliberately
// carry no behavioral names: these gates do not establish proximity policy.
type Threat struct {
	ObjectID uint32
	Amount   float32
}

type AddThreatRequest struct {
	ObjectID             uint32
	TargetObjectID       uint32
	Amount               float32
	TargetThreatIncrease float32
	TargetThreatDecrease float32
	ReasonFlags          uint32
}

// AlertObject ports sub_9E9800. Duplicates never modify threat or first-alert
// state, even when the current combat target has subsequently been cleared.
func (e *Session) AlertObject(objectID, targetObjectID uint32) (Snapshot, bool, error) {
	if e == nil || objectID == 0 || targetObjectID == 0 {
		return Snapshot{}, false, errors.New("npc alert invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	npc, isAdded, err := e.alertObjectLocked(objectID, targetObjectID)
	if err != nil {
		return Snapshot{}, false, fmt.Errorf("alertObject: %w", err)
	}
	return npc, isAdded, nil
}

// alertObjectLocked shares the recovered primitive with server propagation.
// The caller holds e.mu. Assigning a combat target is a server bridge; the
// native primitive itself only updates threat and first-alert state.
func (e *Session) alertObjectLocked(objectID, targetObjectID uint32) (Snapshot, bool, error) {
	npc, isFound := e.npcs[objectID]
	if !isFound || npc.IsDefeated || !npc.IsPublished || npc.HitPoint <= 0 {
		return Snapshot{}, false, fmt.Errorf("npcAlertUnavailable: %d", objectID)
	}
	if npc.Plan.IsFixture || npc.Plan.NPCProfile.AggroType != 0 {
		return npc, false, nil
	}
	for _, threat := range npc.threats {
		if threat.ObjectID == targetObjectID {
			return npc, false, nil
		}
	}
	if len(npc.threats) == 0 && !npc.IsInitialAggroSet {
		npc.IsInitialAggroSet = true
		npc.InitialAggroAnimationFlag = 512
	}
	npc.threats = append(slices.Clone(npc.threats), Threat{ObjectID: targetObjectID, Amount: 5})
	if npc.TargetObjectID == 0 {
		npc.TargetObjectID = targetObjectID
		npc.TargetFaction = FactionPlayerAligned
		target, isTargetFound := e.npcs[targetObjectID]
		if isTargetFound {
			npc.TargetFaction = target.Faction
		}
		npc.IsInvisibleToSecurityTeleporter = false
	}
	e.npcs[objectID] = npc
	e.trackAttacker(objectID, targetObjectID)
	return npc, true, nil
}

// AddThreat ports sub_9E95B0, including target attributes 86/87 and its
// signed minimum magnitude. It records threat without inventing a target
// selection policy or changing the automatic acquisition path.
func (e *Session) AddThreat(req AddThreatRequest) (Snapshot, bool, error) {
	if e == nil || req.ObjectID == 0 || req.TargetObjectID == 0 {
		return Snapshot{}, false, errors.New("npc threat invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	npc, isFound := e.npcs[req.ObjectID]
	if !isFound || npc.IsDefeated || !npc.IsPublished || npc.HitPoint <= 0 {
		return Snapshot{}, false, fmt.Errorf("npcThreatUnavailable: %d", req.ObjectID)
	}
	if npc.Plan.IsFixture || npc.Plan.NPCProfile.AggroType == 2 {
		return npc, false, nil
	}
	adjusted := float32((1 + req.TargetThreatIncrease) * req.Amount)
	decrease := float32(req.TargetThreatDecrease * adjusted)
	amount := float32(adjusted - decrease)
	if math.IsNaN(float64(amount)) || math.IsInf(float64(amount), 0) {
		return Snapshot{}, false, errors.New("npc threat amount invalid")
	}
	if amount > 0 {
		amount = max(float32(0.01), amount)
	} else {
		amount = min(float32(-0.01), amount)
	}
	npc.threats = slices.Clone(npc.threats)
	for index := range npc.threats {
		if npc.threats[index].ObjectID != req.TargetObjectID {
			continue
		}
		npc.threats[index].Amount += amount
		e.npcs[req.ObjectID] = npc
		return npc, true, nil
	}
	if len(npc.threats) == 0 && !npc.IsInitialAggroSet {
		if req.ReasonFlags&6 != 0 {
			npc.InitialAggroAnimationFlag = 32
		} else if req.ReasonFlags&8 != 0 {
			npc.InitialAggroAnimationFlag = 512
		}
	}
	npc.IsInitialAggroSet = true
	npc.threats = append(npc.threats, Threat{ObjectID: req.TargetObjectID, Amount: amount})
	e.npcs[req.ObjectID] = npc
	e.trackAttacker(req.ObjectID, req.TargetObjectID)
	return npc, true, nil
}

func (e *Session) Threats(objectID uint32) []Threat {
	if e == nil {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return slices.Clone(e.npcs[objectID].threats)
}

// trackAttacker requires the session lock. A new threat entry contributes
// exactly one reciprocal attacker, preserving registration order.
func (e *Session) trackAttacker(objectID, targetObjectID uint32) {
	if e.attackersByObject == nil {
		e.attackersByObject = make(map[uint32][]uint32)
	}
	attackers := e.attackersByObject[targetObjectID]
	if slices.Contains(attackers, objectID) {
		return
	}
	e.attackersByObject[targetObjectID] = append(attackers, objectID)
}

// Attackers is the reciprocal side of the explicit threat ledger, including
// deployed hero targets that have no NPC controller in this session.
func (e *Session) Attackers(targetObjectID uint32) []uint32 {
	if e == nil || targetObjectID == 0 {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return slices.Clone(e.attackersByObject[targetObjectID])
}
