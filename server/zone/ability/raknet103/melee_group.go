package raknet103

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/zone/ability"
)

type MeleeGroupRequest struct {
	Candidates      []ability.MeleeCandidate
	TargetObjectID  uint32
	TargetPosition  game.Vec3
	TargetManaPoint float32
	SourceTime      uint64
}

type meleeGroupMember struct {
	actor               ability.MeleeActor
	ability             sim.AbilityDefinition
	attackStartPosition game.Vec3
	attackFacing        game.Vec3
	targetStartPosition game.Vec3
	attackerFootprint   float32
	targetFootprint     float32
	run                 *MeleeRun
}

type MeleeGroup struct {
	members        []meleeGroupMember
	targetObjectID uint32
}

func NewMeleeGroup(req MeleeGroupRequest) (*MeleeGroup, [][]byte, error) {
	if len(req.Candidates) == 0 || req.TargetObjectID == 0 {
		return nil, nil, errors.New("invalid melee group request")
	}
	group := &MeleeGroup{
		members:        make([]meleeGroupMember, 0, len(req.Candidates)),
		targetObjectID: req.TargetObjectID,
	}
	packets := make([][]byte, 0, len(req.Candidates))
	for index, candidate := range req.Candidates {
		actorPosition := candidate.ActorPosition
		if actorPosition == (game.Vec3{}) {
			actorPosition = candidate.Actor.Position
		}
		if candidate.AttackerFootprint <= 0 || candidate.TargetFootprint <= 0 {
			group.Stop()
			return nil, nil, fmt.Errorf("memberFootprint[%d]: invalid", index)
		}
		if candidate.Ability.Kind != sim.AbilityKindMelee ||
			candidate.Ability.MaximumDamage <= 0 || candidate.Ability.Range <= 0 {
			group.Stop()
			return nil, nil, fmt.Errorf(
				"memberAbility[%d]: invalid melee ability", index,
			)
		}
		facing := meleeDirection(actorPosition, req.TargetPosition)
		run, immediatePacket, err := NewMeleeRun(MeleeInput{
			Ability:        candidate.Ability,
			ActorObjectID:  candidate.Actor.ObjectID,
			TargetObjectID: req.TargetObjectID,
			ActorPosition: sim.Position{
				X: actorPosition.X, Y: actorPosition.Y, Z: actorPosition.Z,
			},
			TargetPosition: sim.Position{
				X: req.TargetPosition.X,
				Y: req.TargetPosition.Y,
				Z: req.TargetPosition.Z,
			},
			TargetFacing:    sim.Position{X: facing.X, Y: facing.Y, Z: facing.Z},
			Damage:          candidate.Ability.MaximumDamage,
			TargetManaPoint: req.TargetManaPoint,
			ActorTeam:       2,
			SourceTime:      req.SourceTime,
		})
		if err != nil {
			group.Stop()
			return nil, nil, fmt.Errorf("memberCreate[%d]: %w", index, err)
		}
		group.members = append(group.members, meleeGroupMember{
			actor: candidate.Actor, ability: candidate.Ability,
			attackStartPosition: actorPosition, attackFacing: facing,
			targetStartPosition: req.TargetPosition,
			attackerFootprint:   candidate.AttackerFootprint,
			targetFootprint:     candidate.TargetFootprint,
			run:                 run,
		})
		packets = append(packets, immediatePacket...)
	}
	return group, packets, nil
}

func (e *MeleeGroup) AdvanceHit(
	ctx context.Context, authority ability.MeleeAuthority,
	playerPosition game.Vec3, hitPoint float32,
	resolveCritical ability.ProjectileCriticalResolver,
) ([][]byte, float32, error) {
	if e == nil || len(e.members) == 0 {
		return nil, hitPoint, errors.New("empty melee group")
	}
	if ctx == nil {
		return nil, hitPoint, errors.New("nil context")
	}
	packets := make([][]byte, 0, len(e.members)*3)
	currentHitPoint := hitPoint
	for index, member := range e.members {
		isEnemyAlive := authority != nil &&
			authority.HasEnemy(member.actor.ObjectID)
		actorPosition := member.attackStartPosition
		if authority != nil {
			livePosition, isFound := authority.EnemyPosition(member.actor.ObjectID)
			if isFound {
				actorPosition = livePosition
			}
		}
		isMovementBypass :=
			meleeDistance(member.targetStartPosition, playerPosition) < 1
		isInArc := circleIntersectsMeleeArc(
			actorPosition, member.attackFacing, member.attackerFootprint,
			playerPosition, member.targetFootprint,
		)
		isInRange := isMovementBypass || isInArc
		selectedDamage := member.ability.MaximumDamage
		if isEnemyAlive && isInRange {
			var err error
			selectedDamage, err = authority.SelectRankDamage(member.ability)
			if err != nil {
				return nil, hitPoint, fmt.Errorf("memberDamage[%d]: %w", index, err)
			}
		}
		isCritical := false
		if isEnemyAlive && isInRange && currentHitPoint > 0 &&
			resolveCritical != nil {
			criticalResult, err := resolveCritical(member.actor.Noun, selectedDamage)
			if err != nil {
				return nil, hitPoint, fmt.Errorf(
					"memberCritical[%d]: %w", index, err,
				)
			}
			selectedDamage = criticalResult.Damage
			isCritical = criticalResult.IsCritical
		}
		damage := min(selectedDamage, currentHitPoint)
		isTargetValid := isEnemyAlive && isInRange && damage > 0
		preparedDamage := damage
		if preparedDamage <= 0 {
			preparedDamage = selectedDamage
		}
		facing := meleeDirection(actorPosition, playerPosition)
		err := member.run.PrepareHit(
			isTargetValid, currentHitPoint, preparedDamage, isCritical,
			sim.Position{
				X: playerPosition.X, Y: playerPosition.Y, Z: playerPosition.Z,
			},
			sim.Position{X: facing.X, Y: facing.Y, Z: facing.Z},
		)
		if err != nil {
			return nil, hitPoint, fmt.Errorf("memberPrepare[%d]: %w", index, err)
		}
		memberPacket, err := member.run.Advance(ctx, member.run.HitDeadline())
		if err != nil {
			return nil, hitPoint, fmt.Errorf("memberHit[%d]: %w", index, err)
		}
		packets = append(packets, memberPacket...)
		if isTargetValid {
			currentHitPoint -= damage
		}
	}
	return packets, currentHitPoint, nil
}

func (e *MeleeGroup) AdvanceRelease(ctx context.Context) ([][]byte, error) {
	if e == nil || len(e.members) == 0 {
		return nil, errors.New("empty melee group")
	}
	packets := make([][]byte, 0)
	for index, member := range e.members {
		memberPacket, err := member.run.Advance(ctx, member.run.ReleaseDeadline())
		if err != nil {
			return nil, fmt.Errorf("memberRelease[%d]: %w", index, err)
		}
		packets = append(packets, memberPacket...)
	}
	return packets, nil
}

func (e *MeleeGroup) TargetObjectID() uint32 {
	if e == nil {
		return 0
	}
	return e.targetObjectID
}

func (e *MeleeGroup) ActorObjectID() uint32 {
	if e == nil || len(e.members) == 0 {
		return 0
	}
	return e.members[0].actor.ObjectID
}

func (e *MeleeGroup) ActorObjectIDs() []uint32 {
	if e == nil {
		return nil
	}
	objectIDs := make([]uint32, 0, len(e.members))
	for _, member := range e.members {
		objectIDs = append(objectIDs, member.actor.ObjectID)
	}
	return objectIDs
}

func (e *MeleeGroup) Stop() {
	if e == nil {
		return
	}
	for _, member := range e.members {
		member.run.Stop()
	}
}

func meleeDistance(start game.Vec3, end game.Vec3) float32 {
	x := end.X - start.X
	y := end.Y - start.Y
	z := end.Z - start.Z
	return float32(math.Sqrt(float64(x*x + y*y + z*z)))
}

func meleeDirection(start game.Vec3, end game.Vec3) game.Vec3 {
	direction := end.Sub(start)
	lengthSquared := direction.LengthSquared()
	if lengthSquared == 0 {
		return game.Vec3{X: 1}
	}
	return direction.Scale(1 / float32(math.Sqrt(float64(lengthSquared))))
}

func circleIntersectsMeleeArc(
	origin game.Vec3, facing game.Vec3, attackerFootprint float32,
	target game.Vec3, targetFootprint float32,
) bool {
	if math.Abs(float64(target.Z-origin.Z)) > 20 {
		return false
	}
	x := target.X - origin.X
	y := target.Y - origin.Y
	distance := float32(math.Sqrt(float64(x*x + y*y)))
	arcLength := float32(1.25) + attackerFootprint
	if distance > arcLength+targetFootprint {
		return false
	}
	if distance <= targetFootprint {
		return true
	}
	facingLength := float32(math.Sqrt(float64(facing.X*facing.X + facing.Y*facing.Y)))
	if facingLength == 0 {
		return false
	}
	dot := (x*facing.X + y*facing.Y) / (distance * facingLength)
	dot = max(-1, min(1, dot))
	angle := math.Acos(float64(dot))
	angularPadding := math.Asin(float64(min(1, targetFootprint/distance)))
	return angle <= math.Pi/4+angularPadding
}
