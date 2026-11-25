package npc

import "time"

// CombatIdleSlot keeps the two authored combat cooldowns independent.
type CombatIdleSlot uint8

const (
	CombatIdleSecondary CombatIdleSlot = iota + 1
	CombatIdlePrimary
	CombatIdleFallback
	CombatIdlePreAggro
	CombatIdlePassive
	CombatIdleTargetTooFar
)

type CombatIdleChoice struct {
	Behavior         string
	Slot             CombatIdleSlot
	AuthoredCooldown uint32
}

type CombatIdleRequest struct {
	ObjectID         uint32
	Owner            ActionOwner
	ActionGeneration uint64
	Choice           CombatIdleChoice
	Duration         time.Duration
	Cooldown         time.Duration
	Jitter           time.Duration
	Now              time.Time
}

// CombatIdleRun is a lease for one actor life and action, not an attack lease.
type CombatIdleRun struct {
	ObjectID         uint32
	Owner            ActionOwner
	ActionGeneration uint64
	LifeGeneration   uint64
	Serial           uint64
	Choice           CombatIdleChoice
	Deadline         time.Time
}

type combatIdleState struct {
	lifeGeneration   uint64
	serial           uint64
	primaryReadyAt   time.Time
	secondaryReadyAt time.Time
	windowOwner      ActionOwner
	windowGeneration uint64
	windowStartsAt   time.Time
	windowUntil      time.Time
	run              CombatIdleRun
}

// SelectCombatIdle does not consume randomness or restart any deadline.
// Build 103 A21D20/A211C0/A213E0 and the 12-child tree at 11814E8 put
// conditional secondary idle before primary idle and the combat-idle fallback.
func (e *Session) SelectCombatIdle(objectID uint32, owner ActionOwner, actionGeneration uint64,
	now time.Time,
) (CombatIdleChoice, bool) {
	if e == nil || now.IsZero() || actionGeneration == 0 {
		return CombatIdleChoice{}, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	actor, isFound := e.npcs[objectID]
	if !isFound || isTargetTooFarRunning(actor) || !isCombatIdleActorValid(actor, owner, actionGeneration) ||
		!isCombatIdleUnblocked(actor, now) || !isCombatIdleWindowOpen(actor, owner, actionGeneration, now) {
		return CombatIdleChoice{}, false
	}
	if isCombatIdleRunCurrent(actor, actor.combatIdle.run) && now.Before(actor.combatIdle.run.Deadline) {
		choice := higherPriorityCombatIdle(actor, actor.combatIdle.run, now)
		if choice.Slot != 0 {
			return choice, true
		}
		return actor.combatIdle.run.Choice, true
	}
	return selectCombatIdle(actor, now)
}

// SelectPassiveIdle leaves the pre-aggro/passive phase decision to its caller;
// the startup secondary choice has already been made once at spawn admission.
func (e *Session) SelectPassiveIdle(objectID uint32, isPassive bool, now time.Time) (CombatIdleChoice, bool) {
	if e == nil || now.IsZero() {
		return CombatIdleChoice{}, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	actor, isFound := e.npcs[objectID]
	if !isFound || !isCombatIdleActorValid(actor, ActionOwner{}, 0) || !isCombatIdleUnblocked(actor, now) {
		return CombatIdleChoice{}, false
	}
	if isCombatIdleRunCurrent(actor, actor.combatIdle.run) && now.Before(actor.combatIdle.run.Deadline) {
		return actor.combatIdle.run.Choice, true
	}
	return selectPassiveIdle(actor, isPassive)
}

func isCombatIdleActorValid(actor Snapshot, owner ActionOwner, generation uint64) bool {
	return !actor.IsIntroductionPending() && isCombatIdleOwnerValid(actor, owner, generation)
}

func isCombatIdleOwnerValid(actor Snapshot, owner ActionOwner, generation uint64) bool {
	if !actor.IsPublished || actor.IsDefeated || actor.HitPoint <= 0 || actor.IsBirthPending ||
		actor.Plan.IsIntroductionHidden || actor.IsTurtleActive {
		return false
	}
	if generation == 0 {
		return !actor.IsActionStarted && actor.TargetObjectID == 0
	}
	return owner.UserID != 0 && owner.PeerGeneration != 0 && actor.IsActionStarted &&
		actor.ActionOwner == owner && actor.ActionGeneration == generation
}

func isCombatIdleUnblocked(actor Snapshot, now time.Time) bool {
	status := actor.status
	return !now.Before(actor.cinematic.immobilizedUntil) && !now.Before(actor.cinematic.intangibleUntil) &&
		!actor.cinematic.isPending && !now.Before(status.stunEnd()) && !now.Before(status.sleepEnd()) &&
		!now.Before(status.rootEnd()) && !now.Before(status.bioEntangleExpiresAt) &&
		!now.Before(status.supportHealerRootExpiresAt) && !now.Before(status.fearExpiresAt) &&
		!now.Before(status.banishEnd()) && !now.Before(status.intangibleExpiresAt)
}

func isCombatIdleWindowOpen(actor Snapshot, owner ActionOwner, generation uint64, now time.Time) bool {
	state := actor.combatIdle
	return generation != 0 && state.lifeGeneration == actor.Mutation.Generation &&
		state.windowOwner == owner && state.windowGeneration == generation &&
		!now.Before(state.windowStartsAt) && now.Before(state.windowUntil)
}

// MarkCombatIdleWindow is called only after an attack is admitted. Its release
// and next-attack delays bound idle behavior; the next attack replaces the lease.
func (e *Session) MarkCombatIdleWindow(objectID uint32, owner ActionOwner, actionGeneration uint64,
	now time.Time, releaseDelay time.Duration, nextDelay time.Duration,
) bool {
	if e == nil || now.IsZero() || actionGeneration == 0 || releaseDelay < 0 || nextDelay < releaseDelay {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.npcs[objectID]
	// The first attack and entrance completion can produce on the same
	// deadline. Register its recovery now, but keep behavior admission gated
	// on the entrance publication committing.
	if !isFound || !isCombatIdleOwnerValid(actor, owner, actionGeneration) {
		return false
	}
	state := actor.combatIdle
	if state.lifeGeneration != actor.Mutation.Generation {
		state = combatIdleState{lifeGeneration: actor.Mutation.Generation}
	}
	if isCombatIdleRunStored(actor, state.run) {
		// Its adapter must retire the previous movement modifier first.
		return false
	}
	state.windowOwner = owner
	state.windowGeneration = actionGeneration
	state.windowStartsAt = now.Add(releaseDelay)
	state.windowUntil = now.Add(nextDelay)
	actor.combatIdle = state
	e.npcs[objectID] = actor
	return true
}

func (e *Session) CombatIdleWindow(objectID uint32, owner ActionOwner, actionGeneration uint64,
	now time.Time,
) (time.Duration, bool) {
	if e == nil || now.IsZero() {
		return 0, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	actor, isFound := e.npcs[objectID]
	if !isFound || !isCombatIdleActorValid(actor, owner, actionGeneration) ||
		!isCombatIdleUnblocked(actor, now) || !isCombatIdleWindowOpen(actor, owner, actionGeneration, now) {
		return 0, false
	}
	return actor.combatIdle.windowUntil.Sub(now), true
}

// CurrentCombatIdle includes expired or interrupted leases awaiting cleanup,
// even from another passive peer. This is an occupancy query, not authority to
// drive the returned lease; no new entry may overwrite its movement modifier.
func (e *Session) CurrentCombatIdle(objectID uint32, now time.Time) (CombatIdleRun, bool) {
	if e == nil || now.IsZero() {
		return CombatIdleRun{}, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	actor, isFound := e.npcs[objectID]
	if !isFound || !isCombatIdleRunStored(actor, actor.combatIdle.run) {
		return CombatIdleRun{}, false
	}
	return actor.combatIdle.run, true
}

func selectCombatIdle(actor Snapshot, now time.Time) (CombatIdleChoice, bool) {
	graph := actor.Plan.NPCProfile.AIGraph
	if graph == nil || !graph.IsResolved {
		return CombatIdleChoice{}, false
	}
	state := actor.combatIdle
	if state.lifeGeneration != actor.Mutation.Generation {
		state = combatIdleState{}
	}
	if graph.CombatIdle2 != "" && now.After(state.secondaryReadyAt) && combatIdleCondition(actor, now) {
		return CombatIdleChoice{Behavior: graph.CombatIdle2, Slot: CombatIdleSecondary,
			AuthoredCooldown: graph.CombatIdle2Cooldown}, true
	}
	if graph.CombatIdle != "" && now.After(state.primaryReadyAt) {
		return CombatIdleChoice{Behavior: graph.CombatIdle, Slot: CombatIdlePrimary,
			AuthoredCooldown: graph.CombatIdleCooldown}, true
	}
	return CombatIdleChoice{Behavior: "nBehavior_CombatIdle", Slot: CombatIdleFallback}, true
}

func selectPassiveIdle(actor Snapshot, isPassive bool) (CombatIdleChoice, bool) {
	graph := actor.Plan.NPCProfile.AIGraph
	if graph == nil || !graph.IsResolved {
		return CombatIdleChoice{}, false
	}
	choice := CombatIdleChoice{Behavior: graph.PreAggroIdle, Slot: CombatIdlePreAggro}
	if actor.Plan.IsSecondaryStart {
		choice.Behavior = graph.PreAggroIdle2
	}
	if isPassive {
		choice = CombatIdleChoice{Behavior: graph.PassiveIdle, Slot: CombatIdlePassive}
	}
	return choice, choice.Behavior != ""
}

func combatIdleCondition(actor Snapshot, now time.Time) bool {
	condition := actor.Plan.NPCProfile.AIGraph.CombatIdle2Condition
	if condition == nil || *condition == "" {
		return true
	}
	if *condition != "RecentDamageCheck" {
		return false
	}
	// Build-103 chunk 775: actual non-DOT health loss in the last three seconds.
	cutoff := now.Add(-3 * time.Second)
	damage := float32(0)
	for _, hit := range actor.recentDamages {
		if hit.at.After(cutoff) && !hit.at.After(now) && !hit.isDamageOverTime {
			damage += hit.amount
		}
	}
	return damage > 10
}

// BeginCombatIdle reserves entry once. Cooldown conversion and seeded jitter
// belong to the caller; polling this feature never draws from a random stream.
// The boolean reports a new entry; an existing lease returns false.
func (e *Session) BeginCombatIdle(req CombatIdleRequest) (CombatIdleRun, bool) {
	if e == nil || req.Now.IsZero() || req.Duration <= 0 || req.Cooldown < 0 || req.Jitter < 0 ||
		req.Jitter > time.Duration(1<<63-1)-req.Cooldown ||
		req.Owner.UserID == 0 || req.Owner.PeerGeneration == 0 {
		return CombatIdleRun{}, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.npcs[req.ObjectID]
	if !isFound || !isCombatIdleActorValid(actor, req.Owner, req.ActionGeneration) ||
		!isCombatIdleUnblocked(actor, req.Now) {
		return CombatIdleRun{}, false
	}
	if req.ActionGeneration != 0 && (isTargetTooFarRunning(actor) || !isCombatIdleWindowOpen(actor, req.Owner, req.ActionGeneration, req.Now)) {
		return CombatIdleRun{}, false
	}
	state := actor.combatIdle
	if state.lifeGeneration != actor.Mutation.Generation {
		state = combatIdleState{lifeGeneration: actor.Mutation.Generation}
	}
	if isCombatIdleRunStored(actor, state.run) {
		return state.run, false
	}
	var choice CombatIdleChoice
	var isEligible bool
	if req.ActionGeneration == 0 {
		choice, isEligible = selectPassiveIdle(actor, req.Choice.Slot == CombatIdlePassive)
	} else {
		choice, isEligible = selectCombatIdle(actor, req.Now)
	}
	if !isEligible || choice != req.Choice {
		return CombatIdleRun{}, false
	}
	state.serial++
	if state.serial == 0 {
		state.serial = 1
	}
	state.run = CombatIdleRun{ObjectID: req.ObjectID, Owner: req.Owner,
		ActionGeneration: req.ActionGeneration, LifeGeneration: actor.Mutation.Generation,
		Serial: state.serial, Choice: choice, Deadline: req.Now.Add(req.Duration)}
	if req.ActionGeneration != 0 && state.run.Deadline.After(state.windowUntil) {
		state.run.Deadline = state.windowUntil
	}
	if choice.Slot == CombatIdlePrimary && req.Cooldown > 0 {
		state.primaryReadyAt = req.Now.Add(req.Cooldown + req.Jitter)
	}
	if choice.Slot == CombatIdleSecondary && req.Cooldown > 0 {
		state.secondaryReadyAt = req.Now.Add(req.Cooldown + req.Jitter)
	}
	actor.combatIdle = state
	e.npcs[req.ObjectID] = actor
	return state.run, true
}

func isCombatIdleRunCurrent(actor Snapshot, run CombatIdleRun) bool {
	return isCombatIdleRunStored(actor, run) && isCombatIdleActorValid(actor, run.Owner, run.ActionGeneration)
}

func isCombatIdleRunStored(actor Snapshot, run CombatIdleRun) bool {
	return run.Serial != 0 && actor.Mutation.Generation == run.LifeGeneration && actor.combatIdle.run == run
}

// Build-103 selector A323C0 reevaluates strictly earlier children. Admission
// conditions do not continuously expire the current secondary behavior.
func higherPriorityCombatIdle(actor Snapshot, run CombatIdleRun, now time.Time) CombatIdleChoice {
	if run.ActionGeneration == 0 ||
		(run.Choice.Slot != CombatIdlePrimary && run.Choice.Slot != CombatIdleFallback) {
		return CombatIdleChoice{}
	}
	choice, isEligible := selectCombatIdle(actor, now)
	if !isEligible {
		return CombatIdleChoice{}
	}
	if choice.Slot == CombatIdleSecondary ||
		(run.Choice.Slot == CombatIdleFallback && choice.Slot == CombatIdlePrimary) {
		return choice
	}
	return CombatIdleChoice{}
}

func isCombatIdleRunActive(actor Snapshot, run CombatIdleRun, now time.Time) bool {
	if isTargetTooFarRunning(actor) || !now.Before(run.Deadline) || !isCombatIdleRunCurrent(actor, run) || !isCombatIdleUnblocked(actor, now) ||
		(run.ActionGeneration != 0 && !isCombatIdleWindowOpen(actor, run.Owner, run.ActionGeneration, now)) {
		return false
	}
	choice := higherPriorityCombatIdle(actor, run, now)
	return choice.Slot == 0
}

func (e *Session) IsCombatIdleActive(run CombatIdleRun, now time.Time) bool {
	if e == nil || now.IsZero() || !now.Before(run.Deadline) {
		return false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	actor, isFound := e.npcs[run.ObjectID]
	return isFound && isCombatIdleRunActive(actor, run, now)
}

// RetireCombatIdle lets the current attack owner remove a previous action's
// movement lease before presenting a new attack. The returned lease tells its
// adapter which modifier and movement presentation need cleanup.
func (e *Session) RetireCombatIdle(objectID uint32, owner ActionOwner,
	actionGeneration uint64,
) (CombatIdleRun, bool) {
	if e == nil || actionGeneration == 0 || owner.UserID == 0 || owner.PeerGeneration == 0 {
		return CombatIdleRun{}, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.npcs[objectID]
	if !isFound || actor.IsDefeated || !actor.IsPublished || !actor.IsActionStarted ||
		actor.ActionOwner != owner || actor.ActionGeneration != actionGeneration ||
		!isCombatIdleRunStored(actor, actor.combatIdle.run) {
		return CombatIdleRun{}, false
	}
	run := actor.combatIdle.run
	actor.combatIdle.run = CombatIdleRun{}
	clearCombatIdleMovementBuff(&actor, run)
	e.npcs[objectID] = actor
	return run, true
}

func clearCombatIdleMovementBuff(actor *Snapshot, run CombatIdleRun) {
	switch run.Choice.Behavior {
	case "nBehavior_Strafe", "nBehavior_SmallStrafe", "nBehavior_Flee", "nBehavior_FleeFacing":
		actor.Plan.SupportIdleMovementBuff = 0
	}
}

// FinishCombatIdle always cleans an exact stored same-life lease. Its second
// result permits Stop presentation only while that lease still owns movement;
// a new action, target or actor life cannot be stopped by an old callback.
func (e *Session) FinishCombatIdle(run CombatIdleRun) (bool, bool) {
	if e == nil || run.Serial == 0 {
		return false, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.npcs[run.ObjectID]
	if !isFound || !isCombatIdleRunStored(actor, run) {
		return false, false
	}
	isStopAllowed := actor.IsPublished && !actor.IsDefeated && actor.HitPoint > 0
	if run.ActionGeneration == 0 {
		isStopAllowed = isStopAllowed && !actor.IsActionStarted && actor.TargetObjectID == 0
	} else {
		isStopAllowed = isStopAllowed && actor.ActionGeneration == run.ActionGeneration && actor.ActionOwner == run.Owner
	}
	actor.combatIdle.run = CombatIdleRun{}
	clearCombatIdleMovementBuff(&actor, run)
	e.npcs[run.ObjectID] = actor
	return true, isStopAllowed
}
