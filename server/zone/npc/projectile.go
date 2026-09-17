package npc

import (
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
)

type ProjectileGeometry struct {
	ProjectileHalfExtent sim.Position
	TargetMinimum        sim.Position
	TargetMaximum        sim.Position
}

func ResolveProjectileCollision(
	launchPosition game.Vec3,
	targetPosition game.Vec3,
	currentTargetPosition game.Vec3,
	travelDistance float32,
	geometry ProjectileGeometry,
) (sim.ProjectileBoxCollision, error) {
	if travelDistance <= 0 {
		return sim.ProjectileBoxCollision{}, fmt.Errorf(
			"collisionSize: %g", travelDistance,
		)
	}
	facing := projectileDirection(launchPosition, targetPosition)
	collision, err := sim.ResolveProjectileBoxCollision(sim.ProjectileBoxCollisionInput{
		ProjectilePosition: sim.Position{
			X: launchPosition.X, Y: launchPosition.Y, Z: launchPosition.Z,
		},
		ProjectileDirection:  facing,
		ProjectileHalfExtent: geometry.ProjectileHalfExtent,
		TargetPosition: sim.Position{
			X: currentTargetPosition.X,
			Y: currentTargetPosition.Y,
			Z: currentTargetPosition.Z,
		},
		TargetMinimum:   geometry.TargetMinimum,
		TargetMaximum:   geometry.TargetMaximum,
		MaximumDistance: travelDistance,
	})
	if err != nil {
		return sim.ProjectileBoxCollision{}, fmt.Errorf("collisionResolve: %w", err)
	}
	return collision, nil
}

func projectileDirection(sourcePosition game.Vec3, targetPosition game.Vec3) sim.Position {
	direction := sim.Position{
		X: targetPosition.X - sourcePosition.X,
		Y: targetPosition.Y - sourcePosition.Y,
		Z: targetPosition.Z - sourcePosition.Z,
	}
	lengthSquared := direction.X*direction.X +
		direction.Y*direction.Y +
		direction.Z*direction.Z
	if lengthSquared == 0 {
		return sim.Position{X: 1}
	}
	length := float32(math.Sqrt(float64(lengthSquared)))
	return sim.Position{
		X: direction.X / length,
		Y: direction.Y / length,
		Z: direction.Z / length,
	}
}

func PredictProjectileTarget(
	sourcePosition game.Vec3,
	targetPosition game.Vec3,
	targetVelocity game.Vec3,
	projectileSpeed float32,
	maximumLeadAngle float32,
) game.Vec3 {
	if projectileSpeed <= 0 || maximumLeadAngle <= 0 {
		return targetPosition
	}
	relative := targetPosition.Sub(sourcePosition)
	interceptTime, isFound := projectileInterceptTime(
		relative, targetVelocity, projectileSpeed,
	)
	if !isFound {
		return targetPosition
	}
	predicted := targetPosition.Add(targetVelocity.Scale(interceptTime))
	directLength := relative.Length()
	predictedDelta := predicted.Sub(sourcePosition)
	predictedLength := predictedDelta.Length()
	if directLength <= 0 || predictedLength <= 0 {
		return targetPosition
	}
	direct := relative.Scale(1 / directLength)
	predictedDirection := predictedDelta.Scale(1 / predictedLength)
	dot := max(float32(-1), min(float32(1), projectileDot(direct, predictedDirection)))
	angle := float32(math.Acos(float64(dot)))
	maximumAngle := maximumLeadAngle * math.Pi / 180
	if angle <= maximumAngle {
		return predicted
	}
	sine := float32(math.Sin(float64(angle)))
	if sine == 0 {
		return targetPosition
	}
	portion := maximumAngle / angle
	directScale := float32(math.Sin(float64((1-portion)*angle))) / sine
	predictedScale := float32(math.Sin(float64(portion*angle))) / sine
	clampedDirection := direct.Scale(directScale).Add(
		predictedDirection.Scale(predictedScale),
	)
	return sourcePosition.Add(clampedDirection.Scale(predictedLength))
}

func projectileInterceptTime(
	relative game.Vec3, targetVelocity game.Vec3, projectileSpeed float32,
) (float32, bool) {
	quadratic := projectileDot(targetVelocity, targetVelocity) - projectileSpeed*projectileSpeed
	linear := 2 * projectileDot(relative, targetVelocity)
	constant := projectileDot(relative, relative)
	const epsilon = float32(0.000001)
	if float32(math.Abs(float64(quadratic))) < epsilon {
		if float32(math.Abs(float64(linear))) < epsilon {
			return 0, false
		}
		interceptTime := -constant / linear
		return interceptTime, interceptTime > 0
	}
	discriminant := linear*linear - 4*quadratic*constant
	if discriminant < 0 {
		return 0, false
	}
	root := float32(math.Sqrt(float64(discriminant)))
	first := (-linear - root) / (2 * quadratic)
	second := (-linear + root) / (2 * quadratic)
	interceptTime := float32(math.Inf(1))
	if first > 0 {
		interceptTime = first
	}
	if second > 0 && second < interceptTime {
		interceptTime = second
	}
	return interceptTime, !float32IsInfinite(interceptTime)
}

func float32IsInfinite(scalar float32) bool {
	return math.IsInf(float64(scalar), 0)
}

func projectileDot(left game.Vec3, right game.Vec3) float32 {
	return left.X*right.X + left.Y*right.Y + left.Z*right.Z
}
