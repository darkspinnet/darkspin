package gameplay

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/sporenet"
	zone "github.com/darkspinnet/darkspin/server/zone"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type campaignNPCPiercingContact struct {
	target         zone.NPCTarget
	entryPosition  game.Vec3
	exitPosition   game.Vec3
	travelDistance float32
}

func (e campaignNPCProjectileStep) samplePiercingFlightLocked(
	member *gameplayPeerSession, snapshot abilityraknet.ProjectileSnapshot,
	segments []sim.ProjectileMotionSegment, targets []sim.ProjectileCollisionTarget,
) ([][]byte, sporenet.PlayerStatDelta, error) {
	schedule := e.schedule
	flight := schedule.flight
	travel, err := flight.collision.AdvanceMotion(e.deadline-flight.hitDelay, segments,
		snapshot.IsActive && snapshot.RemainingDistance <= 0, nil)
	statDelta := sporenet.PlayerStatDelta{}
	if err != nil {
		return nil, statDelta, fmt.Errorf("piercingTravel: %w", err)
	}
	if travel.IsResolved {
		packets, resolveErr := schedule.run.ResolveCollision(context.Background(), e.deadline,
			false, false, 0, schedule.result.Damage, schedule.result.IsCritical, travel.Position, snapshot.Direction)
		if resolveErr != nil {
			return nil, statDelta, fmt.Errorf("piercingExpire: %w", resolveErr)
		}
		return packets, statDelta, nil
	}
	remainingDistance := snapshot.RemainingDistance
	for _, segment := range segments {
		remainingDistance += segment.Distance
	}
	packets := make([][]byte, 0)
	for _, segment := range segments {
		contacts := make([]campaignNPCPiercingContact, 0)
		for _, target := range targets {
			if _, isPierced := flight.piercedTargets[target.ObjectID]; isPierced {
				continue
			}
			collision, collisionErr := sim.ResolveProjectileBoxCollision(sim.ProjectileBoxCollisionInput{
				ProjectilePosition: segment.Position, ProjectileDirection: segment.Direction,
				ProjectileHalfExtent: schedule.geometry.ProjectileHalfExtent,
				MaximumDistance:      remainingDistance, TargetPosition: target.Position,
				TargetMinimum: target.Minimum, TargetMaximum: target.Maximum,
			})
			if collisionErr != nil {
				return nil, statDelta, fmt.Errorf("piercingTarget[%d]: %w", target.ObjectID, collisionErr)
			}
			if !collision.IsDirectHit || collision.TravelDistance > segment.Distance {
				continue
			}
			current, isFound := member.zone.NPCTarget(target.ObjectID)
			if isFound {
				contacts = append(contacts, campaignNPCPiercingContact{
					target: current, entryPosition: game.Vec3(collision.Position),
					exitPosition: game.Vec3(collision.ExitPosition), travelDistance: collision.TravelDistance,
				})
			}
		}
		sort.Slice(contacts, func(left, right int) bool {
			if contacts[left].travelDistance == contacts[right].travelDistance {
				return contacts[left].target.ObjectID < contacts[right].target.ObjectID
			}
			return contacts[left].travelDistance < contacts[right].travelDistance
		})
		for _, contact := range contacts {
			flight.piercedTargets[contact.target.ObjectID] = struct{}{}
			contact.travelDistance += schedule.plan.Profile.ProjectileDistance - remainingDistance
			current, delta, contactErr := schedule.piercingContact(member, contact, e.deadline)
			if contactErr != nil {
				return nil, statDelta, fmt.Errorf("piercingContact: %w", contactErr)
			}
			packets = append(packets, current...)
			statDelta.PVEDamageTaken += delta.PVEDamageTaken
		}
		remainingDistance -= segment.Distance
	}
	return packets, statDelta, nil
}

func campaignNPCProjectileRangeEndpoint(
	source game.Vec3, aim game.Vec3, projectileDistance float32,
) game.Vec3 {
	deltaX := aim.X - source.X
	deltaY := aim.Y - source.Y
	deltaZ := aim.Z - source.Z
	length := float32(math.Sqrt(float64(deltaX*deltaX + deltaY*deltaY + deltaZ*deltaZ)))
	if length <= 0 || projectileDistance <= 0 {
		return aim
	}
	scale := projectileDistance / length
	return game.Vec3{
		X: source.X + deltaX*scale,
		Y: source.Y + deltaY*scale,
		Z: source.Z + deltaZ*scale,
	}
}

func campaignNPCPiercingContacts(
	launch game.Vec3,
	endpoint game.Vec3,
	geometry zoneability.ProjectileCollisionGeometry,
	targets []zone.NPCTarget,
) ([]campaignNPCPiercingContact, error) {
	travelDistance := float32(math.Sqrt(float64(
		(endpoint.X-launch.X)*(endpoint.X-launch.X) +
			(endpoint.Y-launch.Y)*(endpoint.Y-launch.Y) +
			(endpoint.Z-launch.Z)*(endpoint.Z-launch.Z),
	)))
	contacts := make([]campaignNPCPiercingContact, 0, len(targets))
	hitTarget := make(map[uint32]struct{}, len(targets))
	for _, target := range targets {
		if _, isHit := hitTarget[target.ObjectID]; isHit {
			continue
		}
		collision, err := zonenpc.ResolveProjectileCollision(
			launch, endpoint, target.Position, travelDistance,
			zonenpc.ProjectileGeometry{
				ProjectileHalfExtent: geometry.ProjectileHalfExtent,
				TargetMinimum:        geometry.TargetMinimum,
				TargetMaximum:        geometry.TargetMaximum,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("targetCollision[%d]: %w", target.ObjectID, err)
		}
		if !collision.IsDirectHit {
			continue
		}
		hitTarget[target.ObjectID] = struct{}{}
		contacts = append(contacts, campaignNPCPiercingContact{
			target: target,
			entryPosition: game.Vec3{
				X: collision.Position.X,
				Y: collision.Position.Y,
				Z: collision.Position.Z,
			},
			exitPosition: game.Vec3{
				X: collision.ExitPosition.X,
				Y: collision.ExitPosition.Y,
				Z: collision.ExitPosition.Z,
			},
			travelDistance: collision.TravelDistance,
		})
	}
	sort.Slice(contacts, func(left int, right int) bool {
		if contacts[left].travelDistance == contacts[right].travelDistance {
			return contacts[left].target.ObjectID < contacts[right].target.ObjectID
		}
		return contacts[left].travelDistance < contacts[right].travelDistance
	})
	return contacts, nil
}

func campaignNPCPiercingDamageMultiplier(
	travelDistance float32, projectileDistance float32,
	minimumDamagePercent float32, isGrowing bool,
) float32 {
	if projectileDistance <= 0 || minimumDamagePercent <= 0 {
		return 1
	}
	if isGrowing {
		travelPercent := min(max(travelDistance/projectileDistance, 0), 1)
		return minimumDamagePercent +
			(1-minimumDamagePercent)*travelPercent
	}
	damageMultiplier := (projectileDistance - travelDistance) /
		projectileDistance
	return max(minimumDamagePercent, damageMultiplier)
}

func (e campaignNPCProjectileSchedule) producePiercingImpact(
	peerSession *gameplayPeerSession,
	deadline time.Duration,
) ([][]byte, sporenet.PlayerStatDelta, error) {
	if peerSession == nil || peerSession.zone == nil || e.run == nil {
		return nil, sporenet.PlayerStatDelta{}, errors.New("piercing projectile runtime unavailable")
	}
	launchPosition := campaignProjectileLaunchPosition(
		e.projectileSourcePosition, e.projectileTargetPosition,
		e.source.Plan.NPCProfile.FootprintRadius,
	)
	contacts, err := campaignNPCPiercingContacts(
		launchPosition, e.projectileTargetPosition, e.geometry,
		peerSession.zone.LiveNPCTargets(),
	)
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("contactsResolve: %w", err)
	}
	packets := make([][]byte, 0, len(contacts)*3+2)
	statDelta := sporenet.PlayerStatDelta{}
	for _, contact := range contacts {
		hitPackets, targetStatDelta, contactErr := e.piercingContact(peerSession, contact, deadline)
		if contactErr != nil {
			return nil, statDelta, fmt.Errorf("impactContact: %w", contactErr)
		}
		packets = append(packets, hitPackets...)
		statDelta.PVEDamageTaken += targetStatDelta.PVEDamageTaken
	}
	terminalPackets, err := e.run.ResolveCollision(
		context.Background(), deadline, false, false, 0,
		e.result.Damage, e.result.IsCritical,
		sim.Position{
			X: e.projectileTargetPosition.X,
			Y: e.projectileTargetPosition.Y,
			Z: e.projectileTargetPosition.Z,
		},
		sim.Position{X: e.facing.X, Y: e.facing.Y, Z: e.facing.Z},
	)
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("terminalResolve: %w", err)
	}
	packets = append(packets, terminalPackets...)
	return packets, statDelta, nil
}

func (e campaignNPCProjectileSchedule) piercingContact(
	member *gameplayPeerSession, contact campaignNPCPiercingContact, deadline time.Duration,
) ([][]byte, sporenet.PlayerStatDelta, error) {
	plan := e.plan
	plan.TargetObjectID = contact.target.ObjectID
	plan.TargetPosition = contact.target.Position
	result := e.result
	result.Damage *= campaignNPCPiercingDamageMultiplier(
		contact.travelDistance, plan.Profile.ProjectileDistance, plan.Profile.MinimumDamagePercent,
		plan.Profile.AbilityName == "CryosBasicFireWave",
	)
	impactPacket, err := npcraknet.PositionedEffect(plan.Profile.ImpactEffectName, contact.entryPosition)
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("impactMarshal[%d]: %w", contact.target.ObjectID, err)
	}
	packets := [][]byte{impactPacket}
	var exitPacket []byte
	if plan.Profile.ProjectileExitEffectName != "" {
		exitPacket, err = npcraknet.PositionedEffect(plan.Profile.ProjectileExitEffectName, contact.exitPosition)
		if err != nil {
			return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("exitMarshal[%d]: %w", contact.target.ObjectID, err)
		}
	}
	timestamp := e.timestamp + uint64(deadline/time.Millisecond)
	hitPackets, statDelta, isStatusEligible, err := e.runtime.applyEnemyAreaAttackDamage(member, e.generation, plan, result, timestamp)
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("targetDamage[%d]: %w", contact.target.ObjectID, err)
	}
	// Piercing profiles on this path have no on-hit status to schedule.
	_ = isStatusEligible
	packets = append(packets, hitPackets...)
	if exitPacket != nil {
		packets = append(packets, exitPacket)
	}
	return packets, statDelta, nil
}
