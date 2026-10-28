//go:build scenario

package npc

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

type scenarioControlPlan struct {
	controlID                string
	isAIEnabled              bool
	isManualAction           bool
	generation               uint64
	lastCommand              string
	lastCommandAt            time.Time
	lastMoveAt               time.Time
	nextActionAt             time.Time
	selfDetonationGeneration uint64
}

type ScenarioControlRef struct {
	ObjectID  uint32
	ControlID string
}

type ScenarioControlState struct {
	ObjectID                  uint32
	ControlID                 string
	IsAIEnabled               bool
	ControlGeneration         uint64
	ActionGeneration          uint64
	IsActionStarted           bool
	IsDefeated                bool
	IsPublished               bool
	IsSelfDetonationCompleted bool
	Position                  game.Vec3
	HitPoint                  float32
	DefaultAbility            string
	SupportedAbilities        []string
	LastCommand               string
	LastCommandAt             time.Time
	NextActionAt              time.Time
}

type ScenarioAIRequest struct {
	Ref         ScenarioControlRef
	IsAIEnabled bool
}

type ScenarioActionRequest struct {
	Ref            ScenarioControlRef
	Owner          ActionOwner
	TargetObjectID uint32
}

type ScenarioMoveRequest struct {
	Ref               ScenarioControlRef
	ControlGeneration uint64
	Destination       game.Vec3
	Elapsed           time.Duration
	At                time.Time
}

type ScenarioMoveResult struct {
	State                           ScenarioControlState
	Step                            PursuitStep
	IsComplete                      bool
	RequestedDestination            game.Vec3
	ProjectedDestination            game.Vec3
	IsProjectedDestinationAvailable bool
}

// Controlled actors use the same authored default action as autonomous NPCs.
// Independent boss and modifier controllers require separate scenario support.
func PrepareScenarioControlledPlan(plan SpawnPlan, controlID string) (SpawnPlan, error) {
	profile, isKnown := ActionProfileForPlan(plan)
	_, isRecovered := ActionProfileForNoun(plan.NounName)
	name := strings.ToLower(plan.NounName)
	if controlID == "" || len(controlID) > 128 || strings.Contains(name, "boss") || strings.Contains(name, "_captain") ||
		plan.OwnerObjectID != 0 || plan.IsFixture || plan.IsBoss || plan.IsCaptain || plan.IsElite || plan.IsArena ||
		len(plan.AffixAuras) != 0 || plan.BossIdentity != (BossIdentity{}) || !isKnown || !isRecovered ||
		profile.AbilityName == "" || !isScenarioActionFamilySupported(profile.Family) {
		return SpawnPlan{}, errors.New("controlled npc noun or profile is unsupported")
	}
	now := time.Now()
	plan = plan.Clone()
	plan.scenarioControl = scenarioControlPlan{controlID: controlID, generation: 1,
		lastCommand: "spawn", lastCommandAt: now, lastMoveAt: now}
	return plan, nil
}

func isScenarioActionFamilySupported(family ActionFamily) bool {
	switch family {
	case ActionZelemRanged, ActionNomadSnipe, ActionNomadDrone,
		ActionProjectile, ActionRetainedArea, ActionPushPull, ActionChannelDrain,
		ActionCharge, ActionCone, ActionLeap, ActionMelee, ActionDetonate:
		return true
	default:
		return false
	}
}

func (e Snapshot) IsScenarioControlled() bool { return e.Plan.scenarioControl.controlID != "" }

func (e Snapshot) IsScenarioAIEnabled() bool {
	return !e.IsScenarioControlled() || e.Plan.scenarioControl.isAIEnabled
}

func admitScenarioAction(actor Snapshot, controlID string) bool {
	if !actor.IsScenarioControlled() {
		return controlID == ""
	}
	control := actor.Plan.scenarioControl
	if time.Now().Before(control.nextActionAt) {
		return false
	}
	if controlID == "" {
		return control.isAIEnabled
	}
	statusActor := actor
	if !actor.IsFirstActionStarted {
		// A hidden pre-aggro idle is revealed by the first normal action.
		// Keep all actual crowd-control and cinematic gates intact.
		statusActor.Plan.IsIntroductionHidden = false
	}
	return controlID == control.controlID && !control.isAIEnabled &&
		!isMutationAbilityBlocked(statusActor, time.Now()) && !time.Now().Before(actor.status.fearExpiresAt)
}

func markScenarioAction(actor *Snapshot, controlID string) {
	if !actor.IsScenarioControlled() {
		return
	}
	control := &actor.Plan.scenarioControl
	control.isManualAction = controlID != ""
	if controlID == "" {
		return
	}
	profile, isKnown := ActionProfileForPlan(actor.Plan)
	if !isKnown {
		return
	}
	control.generation++
	if control.generation == 0 {
		control.generation = 1
	}
	control.lastCommand = "cast"
	control.lastCommandAt = time.Now()
	control.nextActionAt = control.lastCommandAt.Add(max(profile.Cooldown, profile.ReleaseDelay))
}

func isScenarioPursuitAllowed(actor Snapshot) bool {
	return actor.IsScenarioAIEnabled() || actor.Plan.scenarioControl.isManualAction && actor.IsActionStarted
}

func scenarioControlState(actor Snapshot) ScenarioControlState {
	control := actor.Plan.scenarioControl
	profile, isKnown := ActionProfileForPlan(actor.Plan)
	state := ScenarioControlState{ObjectID: actor.Plan.ObjectID, ControlID: control.controlID,
		IsAIEnabled: control.isAIEnabled, ControlGeneration: control.generation,
		ActionGeneration: actor.ActionGeneration, IsActionStarted: actor.IsActionStarted,
		IsDefeated: actor.IsDefeated, IsPublished: actor.IsPublished,
		IsSelfDetonationCompleted: control.selfDetonationGeneration != 0 &&
			control.selfDetonationGeneration == actor.ActionGeneration,
		Position: actor.Plan.Position, HitPoint: actor.HitPoint,
		LastCommand: control.lastCommand, LastCommandAt: control.lastCommandAt, NextActionAt: control.nextActionAt,
		SupportedAbilities: []string{}}
	if isKnown {
		state.DefaultAbility = profile.AbilityName
		state.SupportedAbilities = []string{profile.AbilityName}
	}
	return state
}

func markScenarioSelfDetonation(actor *Snapshot, sourceObjectID uint32, targetObjectID uint32, isForcedDefeat bool) {
	if actor == nil || !isForcedDefeat || sourceObjectID != targetObjectID ||
		!actor.IsScenarioControlled() || !actor.IsActionStarted || !actor.IsDefeated {
		return
	}
	profile, isKnown := ActionProfileForPlan(actor.Plan)
	if !isKnown || profile.Family != ActionDetonate {
		return
	}
	actor.Plan.scenarioControl.selfDetonationGeneration = actor.ActionGeneration
}

func (e *Session) scenarioControlledLocked(ref ScenarioControlRef) (Snapshot, error) {
	actor, isFound := e.npcs[ref.ObjectID]
	if ref.ObjectID == 0 || ref.ControlID == "" || !isFound || !actor.IsScenarioControlled() ||
		actor.Plan.scenarioControl.controlID != ref.ControlID {
		return Snapshot{}, errors.New("controlled npc identity is unavailable")
	}
	return actor, nil
}

func (e *Session) ScenarioControlState(ref ScenarioControlRef) (ScenarioControlState, error) {
	if e == nil {
		return ScenarioControlState{}, errors.New("controlled npc session is unavailable")
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	actor, err := e.scenarioControlledLocked(ref)
	if err != nil {
		return ScenarioControlState{}, fmt.Errorf("controlState: %w", err)
	}
	return scenarioControlState(actor), nil
}

// Disabling fences pending action callbacks; HP, received statuses, external
// damage-over-time and already emitted projectiles are deliberately untouched.
func (e *Session) ScenarioSetAI(req ScenarioAIRequest) (ScenarioControlState, error) {
	if e == nil {
		return ScenarioControlState{}, errors.New("controlled npc session is unavailable")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, err := e.scenarioControlledLocked(req.Ref)
	if err != nil {
		return ScenarioControlState{}, fmt.Errorf("controlAI: %w", err)
	}
	if actor.IsDefeated || actor.HitPoint <= 0 {
		return ScenarioControlState{}, errors.New("controlled npc is defeated")
	}
	actor.ActionGeneration++
	if actor.ActionGeneration == 0 {
		actor.ActionGeneration = 1
	}
	actor.IsActionStarted = false
	actor.ActionOwner = ActionOwner{}
	actor.TargetObjectID = 0
	actor.TargetFaction = FactionUnknown
	actor.TargetOwner = ActionOwner{}
	control := &actor.Plan.scenarioControl
	control.isAIEnabled = req.IsAIEnabled
	control.isManualAction = false
	control.generation++
	if control.generation == 0 {
		control.generation = 1
	}
	control.lastCommand = "set_ai"
	control.lastCommandAt = time.Now()
	control.lastMoveAt = control.lastCommandAt
	e.npcs[req.Ref.ObjectID] = actor
	return scenarioControlState(actor), nil
}

// ScenarioRestoreHitPoint refreshes a living idle controlled target for the
// next cast without attributing gameplay healing or reviving a defeated NPC.
func (e *Session) ScenarioRestoreHitPoint(ref ScenarioControlRef) (ScenarioControlState, error) {
	if e == nil {
		return ScenarioControlState{}, errors.New("controlled npc session is unavailable")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, err := e.scenarioControlledLocked(ref)
	if err != nil {
		return ScenarioControlState{}, fmt.Errorf("controlRestore: %w", err)
	}
	maximumHitPoint := actor.Plan.NPCProfile.HitPoint
	if actor.IsScenarioAIEnabled() || actor.IsDefeated || !actor.IsPublished || actor.HitPoint <= 0 ||
		actor.IsActionStarted || maximumHitPoint <= 0 {
		return ScenarioControlState{}, errors.New("controlled npc restore requires a living idle AI-off actor")
	}
	actor.HitPoint = maximumHitPoint
	control := &actor.Plan.scenarioControl
	control.lastCommand = "restore"
	control.lastCommandAt = time.Now()
	e.npcs[ref.ObjectID] = actor
	return scenarioControlState(actor), nil
}

// ScenarioRetire removes one controlled actor from live combat without death
// rewards or encounter progress. Its handle remains readable until reclaimed.
func (e *Session) ScenarioRetire(ref ScenarioControlRef) (ScenarioControlState, error) {
	if e == nil {
		return ScenarioControlState{}, errors.New("controlled npc session is unavailable")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, err := e.scenarioControlledLocked(ref)
	if err != nil {
		return ScenarioControlState{}, fmt.Errorf("controlRetire: %w", err)
	}
	if actor.IsScenarioAIEnabled() || actor.Plan.scenarioControl.lastCommand == "retire" {
		return ScenarioControlState{}, errors.New("controlled npc retire requires an AI-off actor not already retired")
	}
	actor.HitPoint = 0
	actor.IsDefeated = true
	actor.IsPublished = false
	actor.ActionGeneration++
	if actor.ActionGeneration == 0 {
		actor.ActionGeneration = 1
	}
	actor.TargetObjectID = 0
	actor.TargetFaction = FactionUnknown
	actor.TargetOwner = ActionOwner{}
	actor.IsActionStarted = false
	actor.ActionOwner = ActionOwner{}
	control := &actor.Plan.scenarioControl
	control.isManualAction = false
	control.generation++
	if control.generation == 0 {
		control.generation = 1
	}
	control.lastCommand = "retire"
	control.lastCommandAt = time.Now()
	e.npcs[ref.ObjectID] = actor
	return scenarioControlState(actor), nil
}

func (e *Session) ScenarioStartAction(req ScenarioActionRequest) (Snapshot, bool, bool, error) {
	if req.Ref.ControlID == "" {
		return Snapshot{}, false, false, errors.New("controlled npc identity is unavailable")
	}
	actor, isStarted, isFirst, err := e.startAction(req.Ref.ObjectID, req.Owner, req.TargetObjectID, req.Ref.ControlID)
	if err != nil {
		return Snapshot{}, false, false, fmt.Errorf("controlStart: %w", err)
	}
	return actor, isStarted, isFirst, nil
}

// Each step uses the ordinary actor-bound mesh corridor and authored speed.
// The caller schedules these bounded steps and owns normal movement projection.
func (e *Session) ScenarioMove(req ScenarioMoveRequest) (ScenarioMoveResult, error) {
	if e == nil || req.At.IsZero() || req.Elapsed <= 0 || req.Elapsed > 250*time.Millisecond || !zonegeometry.IsFinite(req.Destination) {
		return ScenarioMoveResult{}, errors.New("controlled npc movement request is invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, err := e.scenarioControlledLocked(req.Ref)
	if err != nil {
		return ScenarioMoveResult{}, fmt.Errorf("controlMove: %w", err)
	}
	if actor.IsScenarioAIEnabled() || actor.IsDefeated || actor.HitPoint <= 0 || !actor.IsPublished || actor.IsActionStarted ||
		req.ControlGeneration != actor.Plan.scenarioControl.generation || e.navigationMesh == nil || !actor.Navigation.IsPresent {
		return ScenarioMoveResult{}, errors.New("controlled npc movement is unavailable")
	}
	if isMutationMovementBlocked(actor, req.At) || req.At.Before(actor.status.fearExpiresAt) {
		return ScenarioMoveResult{}, errors.New("controlled npc movement is status blocked")
	}
	profile, isKnown := ActionProfileForPlan(actor.Plan)
	if !isKnown || !zonegeometry.IsFinitePositiveScalar(profile.MovementSpeed) {
		return ScenarioMoveResult{}, errors.New("controlled npc movement speed is unavailable")
	}
	footprintRadius := max(actor.NavigationRadius(), minimumNavigationFootprintRadius)
	projectedDestination, isProjected, err := zoneaction.NPCProjectPosition(
		e.navigationMesh, req.Destination, footprintRadius, actor.Navigation,
	)
	if err != nil {
		return ScenarioMoveResult{}, fmt.Errorf("controlProject: %w", err)
	}
	if !isProjected || !zonegeometry.IsFinite(projectedDestination) ||
		zonegeometry.Distance(req.Destination, projectedDestination) > 2 {
		return ScenarioMoveResult{}, errors.New("controlled npc destination has no nearby mesh projection")
	}
	result := ScenarioMoveResult{State: scenarioControlState(actor),
		RequestedDestination: req.Destination, ProjectedDestination: projectedDestination,
		IsProjectedDestinationAvailable: true}
	elapsed := min(req.Elapsed, req.At.Sub(actor.Plan.scenarioControl.lastMoveAt))
	if elapsed <= 0 {
		return result, errors.New("controlled npc movement clock did not advance")
	}
	step, err := zoneaction.AdvancePursuitPathStep(e.navigationMesh, actor.Plan.Position, projectedDestination,
		footprintRadius, profile.MovementSpeed*float32(elapsed.Seconds()), actor.Navigation)
	if err != nil {
		return result, fmt.Errorf("controlPath: %w", err)
	}
	previous := actor.Plan.Position
	if previous != step.Position {
		actor.Facing = directionTo(previous, step.Position)
	}
	actor.Plan.Position = step.Position
	if previous != step.Position {
		actor.PositionRevision++
	}
	control := &actor.Plan.scenarioControl
	control.lastCommand = "move"
	control.lastCommandAt = req.At
	control.lastMoveAt = req.At
	e.npcs[req.Ref.ObjectID] = actor
	isComplete := zonegeometry.Distance(step.Position, projectedDestination) <= 0.1
	result.State = scenarioControlState(actor)
	result.IsComplete = isComplete
	result.Step = PursuitStep{Position: step.Position, Waypoint: step.Waypoint, IsWaypointKnown: true,
		IsInRange: isComplete, IsBlocked: !isComplete && previous == step.Position}
	return result, nil
}
