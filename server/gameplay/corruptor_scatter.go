package gameplay

import (
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	zonepopulation "github.com/darkspinnet/darkspin/server/zone/population"
)

func (e campaignNPCActionRuntime) corruptorScatterPosition(
	peerSession *gameplayPeerSession, origin game.Vec3, nounName string,
	pendingPlans []campaignCorruptorSpawnPlan,
) game.Vec3 {
	size, isSizeFound := e.program.SpawnExtentsByNoun[strings.ToLower(nounName)]
	if !isSizeFound {
		return origin
	}
	var blockers []game.BoundingBox
	for _, npc := range peerSession.zone.NPCs().Snapshots() {
		if !npc.IsNavigationCollisionEnabled {
			continue
		}
		blockerSize, isBlockerFound := e.program.SpawnExtentsByNoun[strings.ToLower(npc.Plan.NounName)]
		if isBlockerFound {
			blockers = append(blockers, corruptorCollisionBox(npc.Plan.Position, blockerSize))
		}
	}
	for _, spawn := range pendingPlans {
		blockerSize, isBlockerFound := e.program.SpawnExtentsByNoun[strings.ToLower(spawn.Plan.NounName)]
		if isBlockerFound {
			blockers = append(blockers, corruptorCollisionBox(spawn.Plan.Position, blockerSize))
		}
	}
	members := peerSession.zone.Snapshot().Members
	for _, hero := range peerSession.zone.Hero().Snapshots() {
		if hero.HitPoint <= 0 {
			continue
		}
		isHeroBoundFound := false
		for _, member := range members {
			if member.UserID != hero.UserID || hero.CreatureIndex >= uint32(len(member.Roster.Creatures)) {
				continue
			}
			noun := member.Roster.Creatures[hero.CreatureIndex].Noun
			physics, isPhysicsFound := e.program.NounPhysicsByID[noun]
			if !isPhysicsFound {
				break
			}
			minimum := game.Vec3{X: physics.BoundMinimum.X, Y: physics.BoundMinimum.Y, Z: physics.BoundMinimum.Z}
			maximum := game.Vec3{X: physics.BoundMaximum.X, Y: physics.BoundMaximum.Y, Z: physics.BoundMaximum.Z}
			blockers = append(blockers, game.NewBoundingBox(minimum.Add(hero.Position), maximum.Add(hero.Position)))
			isHeroBoundFound = true
			break
		}
		if isHeroBoundFound {
			continue
		}
		blockers = append(blockers, game.BoundingBox{
			Center: hero.Position.Add(game.Vec3{Z: 0.875}),
			Extent: game.Vec3{X: hero.FootprintRadius, Y: hero.FootprintRadius, Z: 0.875},
		})
	}
	director := peerSession.zone.DirectorDefinition()
	for _, markerSet := range director.MarkerSets {
		for _, marker := range markerSet.Markers {
			if !marker.IsCollisionEnabled || marker.NPCProfile.IsTargetable {
				// Attackable objects use their current NPC collision state above.
				continue
			}
			blockerSize, isBlockerFound := e.program.SpawnExtentsByNoun[strings.ToLower(marker.NounName)]
			if isBlockerFound {
				blockers = append(blockers, corruptorCollisionBox(marker.Position, blockerSize.Scale(marker.Scale)))
			}
		}
	}
	footprint, isFootprintFound := peerSession.zone.DirectorDefinition().NavigationFootprintForAsset(nounName)
	if !isFootprintFound {
		return origin
	}
	return zonepopulation.ScatterPlacement(zonepopulation.ScatterRequest{
		Origin: origin, Mode: uint8(peerSession.binding.Mode), Size: size, Footprint: &footprint, Mesh: peerSession.zone.Navigation(), Blockers: blockers,
	})
}

func corruptorCollisionBox(position game.Vec3, size game.Vec3) game.BoundingBox {
	return game.BoundingBox{
		Center: position.Add(game.Vec3{Z: size.Z * 0.5}), Extent: size.Scale(0.5),
	}
}
