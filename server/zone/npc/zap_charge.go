package npc

import "time"

// CitadelZapCharging is a target-owned UniqueIrreplaceable child. All reads
// and writes of its state occur under the owning NPC session mutex.
type zapChargeState struct {
	revision               uint64
	sourceObjectID         uint32
	sourceActionGeneration uint64
	expiresAt              time.Time
	isActive               bool
}

// ZapChargeLease deliberately retains a private identity, not mutable state.
// Replacing that identity on resurrection fences callbacks from the old life.
type ZapChargeLease struct {
	targetObjectID uint32
	state          *zapChargeState
	revision       uint64
}

func (e *Session) ReserveZapCharge(plan AttackPlan, at time.Time) (ZapChargeLease, bool) {
	if e == nil || at.IsZero() || plan.SourceObjectID == 0 || plan.TargetObjectID == 0 || plan.ActionGeneration == 0 {
		return ZapChargeLease{}, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	source, isSourceFound := e.npcs[plan.SourceObjectID]
	target, isTargetFound := e.npcs[plan.TargetObjectID]
	if !isSourceFound || !isTargetFound || !source.IsPublished || !target.IsPublished || source.IsDefeated || target.IsDefeated ||
		source.HitPoint <= 0 || target.HitPoint <= 0 || source.Faction != target.Faction ||
		!source.IsActionStarted || source.ActionGeneration != plan.ActionGeneration {
		return ZapChargeLease{}, false
	}
	state := target.zapCharge
	if state != nil && state.isActive && at.Before(state.expiresAt) {
		previousSource, isPreviousFound := e.npcs[state.sourceObjectID]
		if isPreviousFound && !previousSource.IsDefeated && previousSource.HitPoint > 0 &&
			previousSource.IsActionStarted && previousSource.ActionGeneration == state.sourceActionGeneration {
			return ZapChargeLease{}, false
		}
	}
	if state == nil {
		state = &zapChargeState{}
		target.zapCharge = state
	}
	state.revision++
	if state.revision == 0 {
		state.revision = 1
	}
	state.sourceObjectID = plan.SourceObjectID
	state.sourceActionGeneration = plan.ActionGeneration
	// Child609 waits .533333361 seconds, then6 seconds. Ordinary source
	// completion releases it sooner; the timeout also fences detached drivers.
	state.expiresAt = at.Add(6533333361 * time.Nanosecond)
	state.isActive = true
	e.npcs[plan.TargetObjectID] = target
	return ZapChargeLease{targetObjectID: plan.TargetObjectID, state: state, revision: state.revision}, true
}

func (e *Session) IsZapChargeCurrent(lease ZapChargeLease, at time.Time) bool {
	if e == nil || lease.state == nil || at.IsZero() {
		return false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.isZapChargeCurrentLocked(lease, at)
}

// CompleteZapCharge validates only the original target life after the source
// has accepted the hit check. Lua868 does not check IsModifierRunning again at
// stop: interruption or a newer child cannot revoke that earned continuation.
func (e *Session) CompleteZapCharge(lease ZapChargeLease) bool {
	if e == nil || lease.state == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	target, isFound := e.npcs[lease.targetObjectID]
	if !isFound || !target.IsPublished || target.IsDefeated || target.HitPoint <= 0 || target.zapCharge != lease.state {
		return false
	}
	if target.zapCharge.revision == lease.revision {
		target.zapCharge.isActive = false
	}
	return true
}

func (e *Session) isZapChargeCurrentLocked(lease ZapChargeLease, at time.Time) bool {
	target, isFound := e.npcs[lease.targetObjectID]
	if !isFound || !target.IsPublished || target.IsDefeated || target.HitPoint <= 0 || target.zapCharge != lease.state {
		return false
	}
	state := target.zapCharge
	if !state.isActive || state.revision != lease.revision || !at.Before(state.expiresAt) {
		return false
	}
	source, isSourceFound := e.npcs[state.sourceObjectID]
	return isSourceFound && source.IsPublished && !source.IsDefeated && source.HitPoint > 0 &&
		source.IsActionStarted && source.ActionGeneration == state.sourceActionGeneration && source.Faction == target.Faction
}

func (e *Session) ReleaseZapCharge(lease ZapChargeLease) {
	if e == nil || lease.state == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	target, isFound := e.npcs[lease.targetObjectID]
	if isFound && target.zapCharge == lease.state && target.zapCharge.revision == lease.revision {
		target.zapCharge.isActive = false
	}
}

// Called only by a newly admitted MinionCinematic while the NPC mutex is held.
// Both children have priority850; the incoming hold interrupts this child, but
// an already active hold does not reject a later equal-priority charge.
func (e *Snapshot) interruptZapCharge() {
	if e.zapCharge != nil {
		e.zapCharge.isActive = false
	}
}
