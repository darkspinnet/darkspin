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
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type campaignNPCPiercingContact struct {
	target         zone.NPCTarget
	entryPosition  game.Vec3
	exitPosition   game.Vec3
	travelDistance float32
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
	timestamp := e.timestamp + uint64(deadline/time.Millisecond)
	for _, contact := range contacts {
		plan := e.plan
		plan.TargetObjectID = contact.target.ObjectID
		plan.TargetPosition = contact.target.Position
		result := e.result
		result.Damage *= campaignNPCPiercingDamageMultiplier(
			contact.travelDistance, plan.Profile.ProjectileDistance,
			plan.Profile.MinimumDamagePercent,
			plan.Profile.AbilityName == "CryosBasicFireWave",
		)
		impactPacket, marshalErr := npcraknet.PositionedEffect(
			plan.Profile.ImpactEffectName, contact.entryPosition,
		)
		if marshalErr != nil {
			return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("impactMarshal[%d]: %w", contact.target.ObjectID, marshalErr)
		}
		packets = append(packets, impactPacket)
		hitPackets, targetStatDelta, _, damageErr := e.runtime.applyEnemyAreaAttackDamage(
			peerSession, e.generation, plan, result, timestamp,
		)
		if damageErr != nil {
			return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("targetDamage[%d]: %w", contact.target.ObjectID, damageErr)
		}
		packets = append(packets, hitPackets...)
		statDelta.PVEDamageTaken += targetStatDelta.PVEDamageTaken
		if plan.Profile.ProjectileExitEffectName == "" {
			continue
		}
		exitPacket, marshalErr := npcraknet.PositionedEffect(
			plan.Profile.ProjectileExitEffectName, contact.exitPosition,
		)
		if marshalErr != nil {
			return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("exitMarshal[%d]: %w", contact.target.ObjectID, marshalErr)
		}
		packets = append(packets, exitPacket)
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
