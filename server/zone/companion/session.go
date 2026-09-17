package companion

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const CompatibilityAggroRadius = 10
const CompatibilityMovementSpeed = 6

const attackRangeTolerance = 0.25
const minimumPursuitDuration = 50 * time.Millisecond

type Actor struct {
	UserID            uint64
	PeerGeneration    uint64
	ObjectID          uint32
	OwnerObjectID     uint32
	Noun              uint32
	Position          game.Vec3
	FootprintRadius   float32
	HitPoint          float32
	MaximumHitPoint   float32
	IsTargetable      bool
	IsCombatant       bool
	TargetObjectID    uint32
	PursuitObjectID   uint32
	PursuitPosition   game.Vec3
	CooldownEnd       time.Time
	DamageBuff        float32
	EnergyDamageBuff  float32
	AttackSpeed       float32
	CooldownReduction float32
	MovementSpeedBuff float32
	FollowRevision    uint64
	IsFollowing       bool
}

type Buff struct {
	DamageBuff        float32
	EnergyDamageBuff  float32
	AttackSpeed       float32
	CooldownReduction float32
	MovementSpeedBuff float32
}

type Follow struct {
	ObjectID              uint32
	OwnerObjectID         uint32
	Position              game.Vec3
	Goal                  game.Vec3
	DesiredStopDistance   float32
	AttackTargetObjectID  uint32
	PursuitTargetObjectID uint32
	Destination           game.Vec3
	TravelDuration        time.Duration
	Revision              uint64
}

type Attack struct {
	ObjectID          uint32
	TargetObjectID    uint32
	Position          game.Vec3
	TargetPosition    game.Vec3
	TargetHitPoint    float32
	CenterRange       float32
	CooldownEnd       time.Time
	DamageBuff        float32
	EnergyDamageBuff  float32
	AttackSpeed       float32
	CooldownReduction float32
}

type Pursuit struct {
	ObjectID            uint32
	TargetObjectID      uint32
	Position            game.Vec3
	TargetPosition      game.Vec3
	DesiredStopDistance float32
	TravelDuration      time.Duration
}

type Session struct {
	mu     sync.RWMutex
	actors map[uint32]Actor
}

func NewSession() *Session {
	return &Session{actors: make(map[uint32]Actor)}
}

func (e *Session) Put(actor Actor) error {
	if e == nil {
		return errors.New("nil companion session")
	}
	if actor.UserID == 0 || actor.PeerGeneration == 0 ||
		actor.ObjectID == 0 || actor.OwnerObjectID == 0 || actor.Noun == 0 ||
		!isFinitePosition(actor.Position) ||
		!isFinitePositive(actor.FootprintRadius) ||
		!isFinitePositive(actor.MaximumHitPoint) ||
		math.IsNaN(float64(actor.HitPoint)) ||
		math.IsInf(float64(actor.HitPoint), 0) ||
		actor.HitPoint < 0 || actor.HitPoint > actor.MaximumHitPoint {
		return errors.New("companion actor invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	current, isFound := e.actors[actor.ObjectID]
	if isFound && actor.PeerGeneration < current.PeerGeneration {
		return fmt.Errorf(
			"companion generation stale: got %d, want >= %d",
			actor.PeerGeneration, current.PeerGeneration,
		)
	}
	e.actors[actor.ObjectID] = actor
	return nil
}

func (e *Session) Snapshot(objectID uint32) (Actor, bool) {
	if e == nil || objectID == 0 {
		return Actor{}, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	actor, isFound := e.actors[objectID]
	return actor, isFound
}

func (e *Session) SetPosition(objectID uint32, position game.Vec3) error {
	if e == nil {
		return errors.New("nil companion session")
	}
	if objectID == 0 || !isFinitePosition(position) {
		return errors.New("companion position invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.actors[objectID]
	if !isFound || actor.HitPoint <= 0 {
		return errors.New("companion actor unavailable")
	}
	actor.Position = position
	actor.PursuitObjectID = 0
	actor.PursuitPosition = game.Vec3{}
	actor.FollowRevision++
	actor.IsFollowing = false
	e.actors[objectID] = actor
	return nil
}

func (e *Session) AddBuff(objectID uint32, buff Buff) error {
	if e == nil {
		return errors.New("nil companion session")
	}
	if objectID == 0 || !isFiniteNonNegative(buff.DamageBuff) ||
		!isFiniteNonNegative(buff.EnergyDamageBuff) ||
		!isFiniteNonNegative(buff.AttackSpeed) ||
		!isFiniteNonNegative(buff.CooldownReduction) ||
		!isFiniteNonNegative(buff.MovementSpeedBuff) {
		return errors.New("companion buff invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.actors[objectID]
	if !isFound {
		return errors.New("companion actor unavailable")
	}
	actor.DamageBuff += buff.DamageBuff
	actor.EnergyDamageBuff += buff.EnergyDamageBuff
	actor.AttackSpeed += buff.AttackSpeed
	actor.CooldownReduction += buff.CooldownReduction
	actor.MovementSpeedBuff += buff.MovementSpeedBuff
	e.actors[objectID] = actor
	return nil
}

func (e *Session) RemoveBuff(objectID uint32, buff Buff) {
	if e == nil || objectID == 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.actors[objectID]
	if !isFound {
		return
	}
	actor.DamageBuff = max(float32(0), actor.DamageBuff-buff.DamageBuff)
	actor.EnergyDamageBuff = max(
		float32(0), actor.EnergyDamageBuff-buff.EnergyDamageBuff,
	)
	actor.AttackSpeed = max(float32(0), actor.AttackSpeed-buff.AttackSpeed)
	actor.CooldownReduction = max(
		float32(0), actor.CooldownReduction-buff.CooldownReduction,
	)
	actor.MovementSpeedBuff = max(
		float32(0), actor.MovementSpeedBuff-buff.MovementSpeedBuff,
	)
	e.actors[objectID] = actor
}

func (e *Session) Snapshots() []Actor {
	if e == nil {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	actor := make([]Actor, 0, len(e.actors))
	for _, current := range e.actors {
		actor = append(actor, current)
	}
	sort.Slice(actor, func(left int, right int) bool {
		return actor[left].ObjectID < actor[right].ObjectID
	})
	return actor
}

func (e *Session) SetHitPoint(
	objectID uint32, hitPoint float32,
) (Actor, Actor, error) {
	if e == nil {
		return Actor{}, Actor{}, errors.New("nil companion session")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.actors[objectID]
	if !isFound {
		return Actor{}, Actor{}, errors.New("companion actor unavailable")
	}
	if math.IsNaN(float64(hitPoint)) || math.IsInf(float64(hitPoint), 0) ||
		hitPoint < 0 || hitPoint > actor.MaximumHitPoint {
		return Actor{}, Actor{}, errors.New("companion hit point invalid")
	}
	previous := actor
	actor.HitPoint = hitPoint
	e.actors[objectID] = actor
	return previous, actor, nil
}

func (e *Session) SetMaximumHitPoint(
	objectID uint32, maximumHitPoint float32,
) (Actor, Actor, error) {
	if e == nil {
		return Actor{}, Actor{}, errors.New("nil companion session")
	}
	if !isFinitePositive(maximumHitPoint) {
		return Actor{}, Actor{}, errors.New("companion maximum hit point invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.actors[objectID]
	if !isFound {
		return Actor{}, Actor{}, errors.New("companion actor unavailable")
	}
	previous := actor
	actor.MaximumHitPoint = maximumHitPoint
	actor.HitPoint = min(actor.HitPoint, maximumHitPoint)
	e.actors[objectID] = actor
	return previous, actor, nil
}

func (e *Session) FollowOwner(
	userID uint64, peerGeneration uint64, ownerObjectID uint32,
	ownerPosition game.Vec3, targets []zonenpc.Snapshot,
	desiredStopDistance float32, movementSpeed float32,
) ([]Follow, error) {
	if e == nil {
		return nil, errors.New("nil companion session")
	}
	if userID == 0 || peerGeneration == 0 || ownerObjectID == 0 ||
		!isFinitePosition(ownerPosition) ||
		!isFinitePositive(desiredStopDistance) ||
		!isFinitePositive(movementSpeed) {
		return nil, errors.New("companion follow invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	follow := make([]Follow, 0)
	for objectID, actor := range e.actors {
		if actor.UserID != userID ||
			actor.PeerGeneration != peerGeneration ||
			actor.OwnerObjectID != ownerObjectID ||
			actor.HitPoint <= 0 {
			continue
		}
		delta := actor.Position.Sub(ownerPosition)
		distance := delta.Length()
		if distance <= desiredStopDistance {
			continue
		}
		isCombatReserved := actor.TargetObjectID != 0 || actor.PursuitObjectID != 0
		if isCombatReserved && (distance <= desiredStopDistance*2 ||
			hasNearbyCombatTarget(actor, targets, CompatibilityAggroRadius)) {
			continue
		}
		goal := ownerPosition.Add(delta.Scale(desiredStopDistance / distance))
		actorMovementSpeed := movementSpeed * max(float32(0.05), 1+actor.MovementSpeedBuff)
		travelDuration := time.Duration(
			float64((distance-desiredStopDistance)/actorMovementSpeed) *
				float64(time.Second),
		)
		travelDuration = max(travelDuration, minimumPursuitDuration)
		actor.FollowRevision++
		follow = append(follow, Follow{
			ObjectID: objectID, OwnerObjectID: ownerObjectID,
			Position: actor.Position, Goal: ownerPosition,
			DesiredStopDistance:   desiredStopDistance,
			AttackTargetObjectID:  actor.TargetObjectID,
			PursuitTargetObjectID: actor.PursuitObjectID,
			Destination:           goal,
			TravelDuration:        travelDuration,
			Revision:              actor.FollowRevision,
		})
		actor.TargetObjectID = 0
		actor.PursuitObjectID = 0
		actor.PursuitPosition = game.Vec3{}
		actor.IsFollowing = true
		e.actors[objectID] = actor
	}
	sort.Slice(follow, func(left int, right int) bool {
		return follow[left].ObjectID < follow[right].ObjectID
	})
	return follow, nil
}

func hasNearbyCombatTarget(
	actor Actor, targets []zonenpc.Snapshot, maximumRange float32,
) bool {
	targetObjectID := actor.TargetObjectID
	if targetObjectID == 0 {
		targetObjectID = actor.PursuitObjectID
	}
	if targetObjectID == 0 || maximumRange <= 0 {
		return false
	}
	for _, target := range targets {
		if target.Plan.ObjectID != targetObjectID || !target.IsPublished ||
			target.IsDefeated ||
			target.HitPoint <= 0 || target.Plan.IsFixture ||
			target.Faction == zonenpc.FactionPlayerAligned {
			continue
		}
		retentionRange := maximumRange + actor.FootprintRadius +
			max(float32(0), target.Plan.NPCProfile.FootprintRadius)
		return actor.Position.Sub(target.Plan.Position).Length() <= retentionRange
	}
	return false
}

func (e *Session) ReleaseFollow(
	objectID uint32, revision uint64, destination game.Vec3,
) bool {
	if e == nil || objectID == 0 || revision == 0 ||
		!isFinitePosition(destination) {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.actors[objectID]
	if !isFound || !actor.IsFollowing || actor.FollowRevision != revision {
		return false
	}
	actor.Position = destination
	actor.IsFollowing = false
	e.actors[objectID] = actor
	return true
}

func (e *Session) CancelFollow(objectID uint32, revision uint64) bool {
	if e == nil || objectID == 0 || revision == 0 {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.actors[objectID]
	if !isFound || !actor.IsFollowing || actor.FollowRevision != revision {
		return false
	}
	actor.IsFollowing = false
	e.actors[objectID] = actor
	return true
}

func (e *Session) ReserveAttacks(
	targets []zonenpc.Snapshot, attackRange float32,
	at time.Time, cooldown time.Duration,
) ([]Attack, error) {
	if e == nil {
		return nil, errors.New("nil companion session")
	}
	if !isFinitePositive(attackRange) || at.IsZero() || cooldown <= 0 {
		return nil, errors.New("companion attack policy invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	attack := make([]Attack, 0)
	for objectID, actor := range e.actors {
		if !actor.IsCombatant || actor.HitPoint <= 0 ||
			actor.TargetObjectID != 0 || actor.PursuitObjectID != 0 ||
			at.Before(actor.CooldownEnd) {
			continue
		}
		var selected zonenpc.Snapshot
		selectedDistance := float32(math.MaxFloat32)
		selectedRange := float32(0)
		for _, target := range targets {
			if !target.IsPublished || target.IsDefeated || target.HitPoint <= 0 ||
				target.Plan.IsFixture ||
				target.Faction == zonenpc.FactionPlayerAligned {
				continue
			}
			centerRange := attackRange + actor.FootprintRadius +
				max(float32(0), target.Plan.NPCProfile.FootprintRadius) +
				attackRangeTolerance
			distance := actor.Position.Sub(target.Plan.Position).Length()
			if distance > centerRange || distance > selectedDistance {
				continue
			}
			if distance == selectedDistance && selected.Plan.ObjectID != 0 &&
				target.Plan.ObjectID > selected.Plan.ObjectID {
				continue
			}
			selected = target
			selectedDistance = distance
			selectedRange = centerRange
		}
		if selected.Plan.ObjectID == 0 {
			continue
		}
		// Nearby combat takes precedence over an owner-follow segment. Bump the
		// revision so its scheduled arrival cannot later pull the companion away
		// from the target it just acquired.
		if actor.IsFollowing {
			actor.IsFollowing = false
			actor.FollowRevision++
		}
		actor.TargetObjectID = selected.Plan.ObjectID
		actorCooldown := resolveCooldown(cooldown, actor)
		actor.CooldownEnd = at.Add(actorCooldown)
		e.actors[objectID] = actor
		attack = append(attack, Attack{
			ObjectID: objectID, TargetObjectID: selected.Plan.ObjectID,
			Position: actor.Position, TargetPosition: selected.Plan.Position,
			TargetHitPoint: selected.HitPoint, CenterRange: selectedRange,
			CooldownEnd:       actor.CooldownEnd,
			DamageBuff:        actor.DamageBuff,
			EnergyDamageBuff:  actor.EnergyDamageBuff,
			AttackSpeed:       actor.AttackSpeed,
			CooldownReduction: actor.CooldownReduction,
		})
	}
	sort.Slice(attack, func(left int, right int) bool {
		return attack[left].ObjectID < attack[right].ObjectID
	})
	return attack, nil
}

// ReserveActorAttack reserves one attack for a specific companion. Specialized
// pets use this boundary so they cannot accidentally consume another pet's
// target or cooldown while sharing the same zone companion session.
func (e *Session) ReserveActorAttack(
	objectID uint32, targets []zonenpc.Snapshot, attackRange float32,
	at time.Time, cooldown time.Duration,
) (Attack, bool, error) {
	if e == nil {
		return Attack{}, false, errors.New("nil companion session")
	}
	if objectID == 0 || !isFinitePositive(attackRange) ||
		at.IsZero() || cooldown <= 0 {
		return Attack{}, false, errors.New("companion actor attack policy invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.actors[objectID]
	if !isFound || !actor.IsCombatant || actor.HitPoint <= 0 ||
		actor.TargetObjectID != 0 || actor.PursuitObjectID != 0 ||
		at.Before(actor.CooldownEnd) {
		return Attack{}, false, nil
	}
	var selected zonenpc.Snapshot
	selectedDistance := float32(math.MaxFloat32)
	selectedRange := float32(0)
	for _, target := range targets {
		if !target.IsPublished || target.IsDefeated || target.HitPoint <= 0 ||
			target.Plan.IsFixture ||
			target.Faction == zonenpc.FactionPlayerAligned {
			continue
		}
		centerRange := attackRange + actor.FootprintRadius +
			max(float32(0), target.Plan.NPCProfile.FootprintRadius) +
			attackRangeTolerance
		distance := actor.Position.Sub(target.Plan.Position).Length()
		if distance > centerRange || distance > selectedDistance {
			continue
		}
		if distance == selectedDistance && selected.Plan.ObjectID != 0 &&
			target.Plan.ObjectID > selected.Plan.ObjectID {
			continue
		}
		selected = target
		selectedDistance = distance
		selectedRange = centerRange
	}
	if selected.Plan.ObjectID == 0 {
		return Attack{}, false, nil
	}
	if actor.IsFollowing {
		actor.IsFollowing = false
		actor.FollowRevision++
	}
	actor.TargetObjectID = selected.Plan.ObjectID
	actorCooldown := resolveCooldown(cooldown, actor)
	actor.CooldownEnd = at.Add(actorCooldown)
	e.actors[objectID] = actor
	return Attack{
		ObjectID: objectID, TargetObjectID: selected.Plan.ObjectID,
		Position: actor.Position, TargetPosition: selected.Plan.Position,
		TargetHitPoint: selected.HitPoint, CenterRange: selectedRange,
		CooldownEnd:       actor.CooldownEnd,
		DamageBuff:        actor.DamageBuff,
		EnergyDamageBuff:  actor.EnergyDamageBuff,
		AttackSpeed:       actor.AttackSpeed,
		CooldownReduction: actor.CooldownReduction,
	}, true, nil
}

func (e *Session) ReservePursuits(
	targets []zonenpc.Snapshot, attackRange float32,
	aggroRadius float32, movementSpeed float32,
) ([]Pursuit, error) {
	if e == nil {
		return nil, errors.New("nil companion session")
	}
	if !isFinitePositive(attackRange) ||
		!isFinitePositive(aggroRadius) ||
		!isFinitePositive(movementSpeed) {
		return nil, errors.New("companion pursuit policy invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	pursuit := make([]Pursuit, 0)
	for objectID, actor := range e.actors {
		if !actor.IsCombatant || actor.HitPoint <= 0 ||
			actor.TargetObjectID != 0 || actor.PursuitObjectID != 0 {
			continue
		}
		var selected zonenpc.Snapshot
		selectedDistance := float32(math.MaxFloat32)
		selectedStopDistance := float32(0)
		for _, target := range targets {
			if !target.IsPublished || target.IsDefeated || target.HitPoint <= 0 ||
				target.Plan.IsFixture ||
				target.Faction == zonenpc.FactionPlayerAligned {
				continue
			}
			stopDistance := attackRange + actor.FootprintRadius +
				max(float32(0), target.Plan.NPCProfile.FootprintRadius) +
				attackRangeTolerance
			distance := actor.Position.Sub(target.Plan.Position).Length()
			if distance <= stopDistance || distance > aggroRadius ||
				distance > selectedDistance {
				continue
			}
			if distance == selectedDistance && selected.Plan.ObjectID != 0 &&
				target.Plan.ObjectID > selected.Plan.ObjectID {
				continue
			}
			selected = target
			selectedDistance = distance
			selectedStopDistance = stopDistance
		}
		if selected.Plan.ObjectID == 0 {
			continue
		}
		if actor.IsFollowing {
			actor.IsFollowing = false
			actor.FollowRevision++
		}
		source := actor.Position
		delta := source.Sub(selected.Plan.Position)
		destination := selected.Plan.Position.Add(
			delta.Scale(selectedStopDistance / selectedDistance),
		)
		actorMovementSpeed := movementSpeed * (1 + actor.MovementSpeedBuff)
		travelDuration := time.Duration(
			float64((selectedDistance-selectedStopDistance)/actorMovementSpeed) *
				float64(time.Second),
		)
		if travelDuration <= 0 {
			continue
		}
		travelDuration = max(travelDuration, minimumPursuitDuration)
		actor.PursuitObjectID = selected.Plan.ObjectID
		actor.PursuitPosition = destination
		e.actors[objectID] = actor
		pursuit = append(pursuit, Pursuit{
			ObjectID: objectID, TargetObjectID: selected.Plan.ObjectID,
			Position: source, TargetPosition: selected.Plan.Position,
			DesiredStopDistance: selectedStopDistance,
			TravelDuration:      travelDuration,
		})
	}
	sort.Slice(pursuit, func(left int, right int) bool {
		return pursuit[left].ObjectID < pursuit[right].ObjectID
	})
	return pursuit, nil
}

// ReserveActorPursuit reserves movement for one specialized companion without
// changing the state of other pets owned by the same player.
func (e *Session) ReserveActorPursuit(
	objectID uint32, targets []zonenpc.Snapshot, attackRange float32,
	aggroRadius float32, movementSpeed float32,
) (Pursuit, bool, error) {
	if e == nil {
		return Pursuit{}, false, errors.New("nil companion session")
	}
	if objectID == 0 || !isFinitePositive(attackRange) ||
		!isFinitePositive(aggroRadius) || !isFinitePositive(movementSpeed) {
		return Pursuit{}, false, errors.New("companion actor pursuit policy invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.actors[objectID]
	if !isFound || !actor.IsCombatant || actor.HitPoint <= 0 ||
		actor.TargetObjectID != 0 || actor.PursuitObjectID != 0 {
		return Pursuit{}, false, nil
	}
	var selected zonenpc.Snapshot
	selectedDistance := float32(math.MaxFloat32)
	selectedStopDistance := float32(0)
	for _, target := range targets {
		if !target.IsPublished || target.IsDefeated || target.HitPoint <= 0 ||
			target.Plan.IsFixture ||
			target.Faction == zonenpc.FactionPlayerAligned {
			continue
		}
		stopDistance := attackRange + actor.FootprintRadius +
			max(float32(0), target.Plan.NPCProfile.FootprintRadius) +
			attackRangeTolerance
		distance := actor.Position.Sub(target.Plan.Position).Length()
		if distance <= stopDistance || distance > aggroRadius ||
			distance > selectedDistance {
			continue
		}
		if distance == selectedDistance && selected.Plan.ObjectID != 0 &&
			target.Plan.ObjectID > selected.Plan.ObjectID {
			continue
		}
		selected = target
		selectedDistance = distance
		selectedStopDistance = stopDistance
	}
	if selected.Plan.ObjectID == 0 {
		return Pursuit{}, false, nil
	}
	if actor.IsFollowing {
		actor.IsFollowing = false
		actor.FollowRevision++
	}
	source := actor.Position
	delta := source.Sub(selected.Plan.Position)
	destination := selected.Plan.Position.Add(
		delta.Scale(selectedStopDistance / selectedDistance),
	)
	actorMovementSpeed := movementSpeed * (1 + actor.MovementSpeedBuff)
	travelDuration := time.Duration(
		float64((selectedDistance-selectedStopDistance)/actorMovementSpeed) *
			float64(time.Second),
	)
	if travelDuration <= 0 {
		return Pursuit{}, false, nil
	}
	travelDuration = max(travelDuration, minimumPursuitDuration)
	actor.PursuitObjectID = selected.Plan.ObjectID
	actor.PursuitPosition = destination
	e.actors[objectID] = actor
	return Pursuit{
		ObjectID: objectID, TargetObjectID: selected.Plan.ObjectID,
		Position: source, TargetPosition: selected.Plan.Position,
		DesiredStopDistance: selectedStopDistance,
		TravelDuration:      travelDuration,
	}, true, nil
}

func resolveCooldown(cooldown time.Duration, actor Actor) time.Duration {
	attackScale := max(float32(0.05), 1+actor.AttackSpeed)
	return time.Duration(float64(cooldown) / float64(attackScale))
}

func isFiniteNonNegative(number float32) bool {
	return !math.IsNaN(float64(number)) && !math.IsInf(float64(number), 0) &&
		number >= 0
}

func (e *Session) ReleasePursuit(
	objectID uint32, targetObjectID uint32,
) bool {
	if e == nil || objectID == 0 || targetObjectID == 0 {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.actors[objectID]
	if !isFound || actor.PursuitObjectID != targetObjectID {
		return false
	}
	actor.Position = actor.PursuitPosition
	actor.PursuitObjectID = 0
	actor.PursuitPosition = game.Vec3{}
	e.actors[objectID] = actor
	return true
}

func (e *Session) CancelPursuit(
	objectID uint32, targetObjectID uint32, position game.Vec3,
) bool {
	if e == nil || objectID == 0 || targetObjectID == 0 ||
		!isFinitePosition(position) {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.actors[objectID]
	if !isFound || actor.PursuitObjectID != targetObjectID {
		return false
	}
	actor.PursuitObjectID = 0
	actor.PursuitPosition = game.Vec3{}
	actor.Position = position
	e.actors[objectID] = actor
	return true
}

func (e *Session) ReleaseAttack(objectID uint32, targetObjectID uint32) bool {
	if e == nil || objectID == 0 || targetObjectID == 0 {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.actors[objectID]
	if !isFound || actor.TargetObjectID != targetObjectID {
		return false
	}
	actor.TargetObjectID = 0
	e.actors[objectID] = actor
	return true
}

func (e *Session) Remove(objectID uint32) bool {
	if e == nil || objectID == 0 {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, isFound := e.actors[objectID]; !isFound {
		return false
	}
	delete(e.actors, objectID)
	return true
}

func (e *Session) RemoveOwner(userID uint64, peerGeneration uint64) []Actor {
	if e == nil || userID == 0 || peerGeneration == 0 {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	removed := make([]Actor, 0)
	for objectID, actor := range e.actors {
		if actor.UserID != userID || actor.PeerGeneration != peerGeneration {
			continue
		}
		removed = append(removed, actor)
		delete(e.actors, objectID)
	}
	sort.Slice(removed, func(left int, right int) bool {
		return removed[left].ObjectID < removed[right].ObjectID
	})
	return removed
}

func isFinitePosition(position game.Vec3) bool {
	return isFinite(position.X) && isFinite(position.Y) && isFinite(position.Z)
}

func isFinitePositive(scalar float32) bool {
	return isFinite(scalar) && scalar > 0
}

func isFinite(scalar float32) bool {
	return !math.IsNaN(float64(scalar)) && !math.IsInf(float64(scalar), 0)
}
