package gameplay

import (
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/zone"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

func (e campaignConeSchedule) twinLaserEndpoints(campaignZone *zone.Zone) ([2]game.Vec3, error) {
	direction := e.plan.TargetPosition.Sub(e.plan.SourcePosition)
	length := float32(math.Hypot(float64(direction.X), float64(direction.Y)))
	if length == 0 {
		direction = game.Vec3{Y: 1}
		length = 1
	}
	side := game.Vec3{X: direction.Y / length, Y: -direction.X / length}
	hold := float64(e.plan.TargetPosition.Sub(e.plan.SourcePosition).Length()) * 0.1
	elapsed := max(0, (e.hitDelay - e.plan.Profile.HitDelay).Seconds())
	// Chunk 565 holds at 0.001 units/second, then accelerates by
	// 2*sweepLength/sweepTime^2 (24) for the half-second inward sweep.
	sweep := min(0.5, max(0, elapsed-hold))
	distance := max(float32(0), 3-float32(min(elapsed, hold)*0.001+sweep*0.001+12*sweep*sweep))
	endpoints := [2]game.Vec3{e.plan.TargetPosition.Add(side.Scale(distance)), e.plan.TargetPosition.Sub(side.Scale(distance))}
	for index, endpoint := range endpoints {
		projected, isProjected, err := zonenavigation.ProjectPosition(campaignZone.Navigation(), endpoint, 0.1)
		if err != nil {
			return endpoints, fmt.Errorf("laserProject: %w", err)
		}
		if isProjected {
			endpoints[index] = projected
		}
	}
	return endpoints, nil
}

func (e campaignConeSchedule) twinLaserOrigin(source zonenpc.Snapshot, index int) game.Vec3 {
	direction := e.plan.TargetPosition.Sub(e.plan.SourcePosition)
	length := float32(math.Hypot(float64(direction.X), float64(direction.Y)))
	if length == 0 {
		direction = game.Vec3{Y: 1}
		length = 1
	}
	forward := game.Vec3{X: direction.X / length, Y: direction.Y / length}
	right := game.Vec3{X: forward.Y, Y: -forward.X}
	sign := float32(1)
	if index == 1 {
		sign = -1
	}
	scale := source.Plan.NPCProfile.GraphicsScale
	if scale <= 0 {
		scale = 1
	}
	origin := source.Plan.Position.Add(forward.Scale(1.1 * scale)).Add(right.Scale(sign * 0.6 * scale))
	origin.Z += 1.2 * scale
	return origin
}

func (e campaignConeSchedule) moveTwinLaser() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	current, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || current.generation != e.generation || current.campaignTwinLaserEndpointIDs[e.objectID] != e.twinLaserEndpointID {
		return nil, nil
	}
	if !current.isCampaignNPCAttackGenerationActiveAt(e.generation, e.objectID, e.plan.TargetObjectID, e.actionGeneration, e.runtime.now()) {
		return nil, nil
	}
	endpoints, err := e.twinLaserEndpoints(current.zone)
	if err != nil {
		return nil, fmt.Errorf("laserMoveEndpoints: %w", err)
	}
	packets := make([][]byte, 0, 2)
	for index, objectID := range [2]uint32{e.twinLaserEndpointID, e.twinLaserSecondID} {
		packet, moveErr := npcraknet.RestorePose(objectID, endpoints[index], game.Vec3{Y: 1})
		if moveErr != nil {
			return packets, fmt.Errorf("laserMove: %w", moveErr)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func (e campaignConeSchedule) endTwinLaserAnimation() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	current, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || current.generation != e.generation || current.campaignTwinLaserEndpointIDs[e.objectID] != e.twinLaserEndpointID ||
		!current.isCampaignNPCAttackGenerationActiveAt(e.generation, e.objectID, e.plan.TargetObjectID, e.actionGeneration, e.runtime.now()) {
		return nil, nil
	}
	packet, err := npcraknet.AnimationState(e.objectID, e.plan.Profile.EndAnimationName,
		e.timestamp+uint64(e.hitDelay/time.Millisecond))
	if err != nil {
		return nil, fmt.Errorf("laserEndAnimation: %w", err)
	}
	return [][]byte{packet}, nil
}
