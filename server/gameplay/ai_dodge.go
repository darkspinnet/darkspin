package gameplay

import (
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
)

// Read projectile motion without consuming collision samples or changing the
// simulation. NPC attacks may be owned by any peer in the shared zone.
func (e gameplayPendingRuntime) playerAIDodgeGoalLocked(
	member gameplayPeerSession, now time.Time,
) (raknet.Vector3, bool) {
	projectiles := make([]abilityraknet.ProjectileSnapshot, 0)
	for _, candidate := range e.registry.sessions {
		if candidate.binding.GameID != member.binding.GameID || candidate.zone != member.zone {
			continue
		}
		for _, run := range candidate.campaignNPCProjectiles {
			projectiles = append(projectiles, run.Snapshot(now))
		}
		if member.binding.Mode != game.ModeArena || candidate.binding.Team == member.binding.Team {
			continue
		}
		for _, run := range candidate.sageAttacks {
			projectiles = append(projectiles, run.Snapshot(now))
		}
		for _, run := range candidate.heroProjectileRuns {
			if run != nil {
				projectiles = append(projectiles, run.projectile.Snapshot(now))
			}
		}
		for _, run := range candidate.heroBurstAttacks {
			projectiles = append(projectiles, run.Snapshots(now)...)
		}
		projectiles = append(projectiles, candidate.fieldMedicDroneAttack.Snapshot(now))
		if candidate.fireTempestActive != nil {
			projectiles = append(projectiles, candidate.fireTempestActive.attack.Snapshot(now))
		}
	}
	goal := member.playerPosition
	soonest := float32(0.8)
	selectedID := uint32(0)
	clearance := member.deployedCampaignFootprintRadius() + 1.5
	for _, projectile := range projectiles {
		if !projectile.IsActive || projectile.IsFrozen || projectile.Speed <= 0 ||
			projectile.SourceObjectID == member.deployedObjectID {
			continue
		}
		vx := projectile.Direction.X * projectile.Speed
		vy := projectile.Direction.Y * projectile.Speed
		speedSquared := vx*vx + vy*vy
		if speedSquared < 0.01 {
			continue
		}
		dx := member.playerPosition.X - projectile.Position.X
		dy := member.playerPosition.Y - projectile.Position.Y
		arrival := (dx*vx + dy*vy) / speedSquared
		if arrival < 0 || arrival > soonest ||
			arrival*projectile.Speed > projectile.RemainingDistance ||
			time.Duration(float64(arrival)*float64(time.Second)) > projectile.RemainingFlightDuration {
			continue
		}
		missX, missY := dx-vx*arrival, dy-vy*arrival
		height := member.playerPosition.Z - (projectile.Position.Z + projectile.Direction.Z*projectile.Speed*arrival)
		if missX*missX+missY*missY > clearance*clearance || math.Abs(float64(height)) > 3 {
			continue
		}
		if selectedID != 0 && arrival == soonest && projectile.ObjectID >= selectedID {
			continue
		}
		speed := float32(math.Sqrt(float64(speedSquared)))
		nx, ny := -vy/speed, vx/speed
		if dx*nx+dy*ny < 0 {
			nx, ny = -nx, -ny
		}
		// Move perpendicular to the incoming path, away from its centerline.
		goal = raknet.Vector3{X: member.playerPosition.X + nx*(clearance+1),
			Y: member.playerPosition.Y + ny*(clearance+1), Z: member.playerPosition.Z}
		soonest, selectedID = arrival, projectile.ObjectID
	}
	return goal, selectedID != 0
}

func playerAIStrafeGoal(source, target raknet.Vector3, isLeft bool) raknet.Vector3 {
	dx, dy := source.X-target.X, source.Y-target.Y
	distance := float32(math.Sqrt(float64(dx*dx + dy*dy)))
	if distance < 0.1 {
		return source
	}
	angle := float64(min(float32(0.25), 2/distance))
	if !isLeft {
		angle = -angle
	}
	sine, cosine := math.Sincos(angle)
	return raknet.Vector3{X: target.X + dx*float32(cosine) - dy*float32(sine),
		Y: target.Y + dx*float32(sine) + dy*float32(cosine), Z: source.Z}
}
