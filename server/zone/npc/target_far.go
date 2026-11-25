package npc

import "time"

type TargetTooFarRun struct {
	ObjectID, TargetObjectID                 uint32
	Owner                                    ActionOwner
	ActionGeneration, LifeGeneration, Serial uint64
	Behavior                                 string
}

type TargetTooFarRequest struct {
	ObjectID, TargetObjectID uint32
	Owner                    ActionOwner
	ActionGeneration         uint64
	Choice                   CombatIdleChoice
	Cooldown, Jitter         time.Duration
	Now                      time.Time
}

// The native TargetTooFar deadline is separate from both combat-idle deadlines.
type targetTooFarState struct {
	lifeGeneration, serial uint64
	readyAt                time.Time
	run                    TargetTooFarRun
}

func isTargetTooFarRunning(actor Snapshot) bool {
	run := actor.targetTooFar.run
	return run.Serial != 0 && run.LifeGeneration == actor.Mutation.Generation &&
		run.Owner == actor.ActionOwner && run.ActionGeneration == actor.ActionGeneration && actor.IsActionStarted
}

func selectTargetTooFar(actor Snapshot, now time.Time) (CombatIdleChoice, bool) {
	graph := actor.Plan.NPCProfile.AIGraph
	if graph == nil || !graph.IsResolved || graph.TargetTooFar == "" {
		return CombatIdleChoice{}, false
	}
	state := actor.targetTooFar
	if state.lifeGeneration == actor.Mutation.Generation && !now.After(state.readyAt) {
		return CombatIdleChoice{}, false
	}
	return CombatIdleChoice{Behavior: graph.TargetTooFar, Slot: CombatIdleTargetTooFar,
		AuthoredCooldown: graph.TargetTooFarCooldown}, true
}

func (e *Session) SelectTargetTooFar(objectID uint32, owner ActionOwner, actionGeneration uint64, now time.Time) (CombatIdleChoice, bool) {
	if e == nil || now.IsZero() || actionGeneration == 0 {
		return CombatIdleChoice{}, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	actor, isFound := e.npcs[objectID]
	if !isFound || !isCombatIdleActorValid(actor, owner, actionGeneration) || !isCombatIdleUnblocked(actor, now) {
		return CombatIdleChoice{}, false
	}
	return selectTargetTooFar(actor, now)
}

// This occupancy check includes interrupted runs until their controller retires.
func (e *Session) CurrentTargetTooFar(objectID uint32, owner ActionOwner, actionGeneration uint64, now time.Time) (TargetTooFarRun, bool) {
	if e == nil || now.IsZero() {
		return TargetTooFarRun{}, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	actor, isFound := e.npcs[objectID]
	if !isFound || !isTargetTooFarRunning(actor) || actor.ActionOwner != owner || actor.ActionGeneration != actionGeneration {
		return TargetTooFarRun{}, false
	}
	return actor.targetTooFar.run, true
}

func (e *Session) BeginTargetTooFar(req TargetTooFarRequest) (TargetTooFarRun, bool) {
	if e == nil || req.Now.IsZero() || req.ActionGeneration == 0 || req.TargetObjectID == 0 ||
		req.Cooldown < 0 || req.Jitter < 0 || req.Jitter > time.Duration(1<<63-1)-req.Cooldown {
		return TargetTooFarRun{}, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.npcs[req.ObjectID]
	if !isFound || !isCombatIdleActorValid(actor, req.Owner, req.ActionGeneration) ||
		!isCombatIdleUnblocked(actor, req.Now) || actor.TargetObjectID != req.TargetObjectID {
		return TargetTooFarRun{}, false
	}
	if isTargetTooFarRunning(actor) {
		return actor.targetTooFar.run, false
	}
	choice, isEligible := selectTargetTooFar(actor, req.Now)
	if !isEligible || choice != req.Choice {
		return TargetTooFarRun{}, false
	}
	state := actor.targetTooFar
	if state.lifeGeneration != actor.Mutation.Generation {
		state = targetTooFarState{lifeGeneration: actor.Mutation.Generation}
	}
	state.serial++
	if state.serial == 0 {
		state.serial = 1
	}
	state.run = TargetTooFarRun{ObjectID: req.ObjectID, TargetObjectID: req.TargetObjectID,
		Owner: req.Owner, ActionGeneration: req.ActionGeneration, LifeGeneration: actor.Mutation.Generation,
		Serial: state.serial, Behavior: choice.Behavior}
	state.readyAt = time.Time{}
	if req.Cooldown > 0 {
		state.readyAt = req.Now.Add(req.Cooldown + req.Jitter)
	}
	actor.targetTooFar = state
	e.npcs[req.ObjectID] = actor
	return state.run, true
}

func (e *Session) IsTargetTooFarActive(run TargetTooFarRun, now time.Time) bool {
	if e == nil || now.IsZero() || run.Serial == 0 {
		return false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	actor, isFound := e.npcs[run.ObjectID]
	return isFound && isTargetTooFarRunning(actor) && actor.targetTooFar.run == run &&
		actor.TargetObjectID == run.TargetObjectID &&
		isCombatIdleActorValid(actor, run.Owner, run.ActionGeneration) && isCombatIdleUnblocked(actor, now)
}

// Only the exact current life/action may stop movement or resume its attack.
func (e *Session) FinishTargetTooFar(run TargetTooFarRun) (bool, bool) {
	if e == nil || run.Serial == 0 {
		return false, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.npcs[run.ObjectID]
	if !isFound || actor.Mutation.Generation != run.LifeGeneration || actor.targetTooFar.run != run {
		return false, false
	}
	isStopAllowed := actor.IsPublished && !actor.IsDefeated && actor.HitPoint > 0 &&
		actor.IsActionStarted && actor.ActionOwner == run.Owner && actor.ActionGeneration == run.ActionGeneration
	actor.targetTooFar.run = TargetTooFarRun{}
	e.npcs[run.ObjectID] = actor
	return true, isStopAllowed
}
