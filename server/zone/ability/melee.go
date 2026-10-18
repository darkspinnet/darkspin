package ability

import (
	"errors"
	"math"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type MeleeActor struct {
	ObjectID uint32
	Noun     string
	Position game.Vec3
}

type MeleeCandidate struct {
	Actor             MeleeActor
	Ability           sim.AbilityDefinition
	ActorPosition     game.Vec3
	AttackerFootprint float32
	TargetFootprint   float32
}

type MeleeAuthority interface {
	HasEnemy(uint32) bool
	EnemyPosition(uint32) (game.Vec3, bool)
	SelectRankDamage(sim.AbilityDefinition) (float32, error)
}

// MeleeArc retains the actor-center sector captured at the first impact.
type MeleeArc struct {
	Position  game.Vec3
	Facing    game.Vec3
	Length    float32
	HalfAngle float64
}

func NewMeleeArc(position game.Vec3, facing game.Vec3, length float32, angle float32) (MeleeArc, error) {
	if !isFinitePosition(position) || !isFinitePosition(facing) ||
		invalidNumber(length) || length <= 0 || invalidNumber(angle) || angle <= 0 || angle > 360 {
		return MeleeArc{}, errors.New("invalid melee arc")
	}
	forwardLength := math.Hypot(float64(facing.X), float64(facing.Y))
	if forwardLength == 0 {
		return MeleeArc{}, errors.New("zero melee facing")
	}
	return MeleeArc{
		Position: position,
		Facing:   game.Vec3{X: float32(float64(facing.X) / forwardLength), Y: float32(float64(facing.Y) / forwardLength)},
		Length:   length, HalfAngle: float64(angle) * math.Pi / 360,
	}, nil
}

// Contains uses the native strict circle/sector overlap, including both finite
// radial sides. Vertical admission is independent of the XY footprint.
func (e MeleeArc) Contains(position game.Vec3, radius float32) bool {
	if !isFinitePosition(position) || !isFinitePosition(e.Position) || !isFinitePosition(e.Facing) ||
		invalidNumber(radius) || radius < 0 || invalidNumber(e.Length) ||
		math.IsNaN(e.HalfAngle) || math.IsInf(e.HalfAngle, 0) ||
		(e.Facing.X == 0 && e.Facing.Y == 0) ||
		math.Abs(float64(position.Z)-float64(e.Position.Z)) > 20 || e.Length <= 0 || e.HalfAngle <= 0 {
		return false
	}
	x := float64(position.X) - float64(e.Position.X)
	y := float64(position.Y) - float64(e.Position.Y)
	distance := math.Hypot(x, y)
	if distance >= float64(e.Length)+float64(radius) {
		return false
	}
	if distance == 0 || e.angle(x, y) < e.HalfAngle {
		return true
	}
	for _, sideAngle := range []float64{-e.HalfAngle, e.HalfAngle} {
		cosine, sine := math.Cos(sideAngle), math.Sin(sideAngle)
		sideX := (float64(e.Facing.X)*cosine - float64(e.Facing.Y)*sine) * float64(e.Length)
		sideY := (float64(e.Facing.X)*sine + float64(e.Facing.Y)*cosine) * float64(e.Length)
		projection := max(0.0, min(1.0, (x*sideX+y*sideY)/(sideX*sideX+sideY*sideY)))
		if math.Hypot(x-projection*sideX, y-projection*sideY) < float64(radius) {
			return true
		}
	}
	return false
}

func (e MeleeArc) angle(x float64, y float64) float64 {
	return math.Abs(math.Atan2(float64(e.Facing.X)*y-float64(e.Facing.Y)*x,
		float64(e.Facing.X)*x+float64(e.Facing.Y)*y))
}

func IsHostileMeleeTarget(enemy zonenpc.Snapshot) bool {
	return enemy.Plan.ObjectID != 0 && enemy.IsPublished && !enemy.IsDefeated &&
		enemy.HitPoint > 0 && enemy.Faction == zonenpc.FactionNonPlayerAligned
}

// BestTarget keeps the first enumerated hostile candidate on equal native
// distance-plus-angle scores. Query enumeration remains the session's policy.
func (e MeleeArc) BestTarget(enemies *zonenpc.Session, actorPosition game.Vec3, facing game.Vec3) (zonenpc.Snapshot, bool) {
	if enemies == nil || !isFinitePosition(actorPosition) || !isFinitePosition(facing) {
		return zonenpc.Snapshot{}, false
	}
	selected := zonenpc.Snapshot{}
	bestScore := math.Inf(1)
	actorArc := e
	actorArc.Facing = facing
	for _, enemy := range enemies.LiveSnapshots() {
		if !IsHostileMeleeTarget(enemy) || !e.Contains(enemy.Plan.Position, enemy.Plan.ActorFootprintRadius()) {
			continue
		}
		x := float64(enemy.Plan.Position.X) - float64(actorPosition.X)
		y := float64(enemy.Plan.Position.Y) - float64(actorPosition.Y)
		distance := float64(positionDistance(actorPosition, enemy.Plan.Position))
		score := min(1.0, distance/float64(e.Length)) + min(1.0, actorArc.angle(x, y)/e.HalfAngle)
		if score < bestScore {
			selected, bestScore = enemy, score
		}
	}
	return selected, selected.Plan.ObjectID != 0
}
