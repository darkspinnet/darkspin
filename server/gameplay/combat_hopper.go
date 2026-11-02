package gameplay

import (
	"errors"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

const campaignHopperMaxJumpDistance = float32(15)
const campaignHopperFlightStep = 50 * time.Millisecond

func (e campaignLeapSchedule) launchHopper(
	enemy zonenpc.Snapshot, targetPosition game.Vec3, animationPacket []byte,
) ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	e.runtime.registry.mutex.RUnlock()
	if !isFound || peerSession.zone == nil {
		return nil, nil
	}
	forward := targetPosition.Sub(enemy.Plan.Position)
	if forward.Length() <= 0 {
		forward = game.Vec3{X: enemy.Facing.X, Y: enemy.Facing.Y}
	}
	if forward.Length() <= 0 {
		forward = game.Vec3{X: 1}
	}
	forward = forward.Scale(1 / forward.Length())
	direction := forward
	jumpDistance := e.hopperDistance
	desired := enemy.Plan.Position.Add(forward.Scale(e.hopperDistance))
	if e.isHopperRetreat {
		random := peerSession.zone.NPCRandom()
		if random == nil {
			return e.fail("hopperJumpRandom", errors.New("npc random unavailable"))
		}
		angle := random.Float64()*2 - 1
		jumpDistance = e.plan.Profile.MinimumRange *
			(0.75 + 0.3*float32(random.Float64()))
		sine, cosine := math.Sincos(angle)
		rotated := game.Vec3{
			X: -forward.X*float32(cosine) + forward.Y*float32(sine),
			Y: -forward.X*float32(sine) - forward.Y*float32(cosine),
			Z: -forward.Z,
		}
		direction = rotated
		desired = enemy.Plan.Position.Add(rotated.Scale(jumpDistance))
	}
	destination := desired
	isReachable := true
	var err error
	mesh := peerSession.zone.Navigation()
	if mesh != nil {
		destination, isReachable, err = zoneaction.NPCDirectMovementDestination(
			mesh, enemy.Plan.Position, desired,
			enemy.Plan.ActorFootprintRadius(), enemy.Navigation,
		)
	}
	if err != nil {
		return e.fail("hopperJumpDestination", err)
	}
	landing := e
	landing.origin = enemy.Plan.Position
	landing.destination = enemy.Plan.Position
	landing.flightDuration = 0
	packets := [][]byte{animationPacket}
	travelDistance := destination.Sub(enemy.Plan.Position).Length()
	if isReachable && travelDistance > 0.1 {
		jumpPacket, jumpErr := npcraknet.HopperJump(
			e.objectID, destination, direction, jumpDistance,
		)
		if jumpErr != nil {
			return e.fail("hopperJumpMarshal", jumpErr)
		}
		landing.destination = destination
		landing.isHopperJumped = true
		landing.hopperDistance = jumpDistance
		// Native jump travel advances by speed * frame delta; animation timing
		// is independent of WaitForJumpComplete.
		landing.flightDuration = time.Duration(
			float64(travelDistance) / float64(e.plan.Profile.ForcedMovementSpeed) * float64(time.Second),
		)
		packets = append(packets, jumpPacket)
	}
	producers := make([]raknet.ScheduledPacketProducer, 0, 14)
	if landing.isHopperJumped {
		for elapsed := campaignHopperFlightStep; elapsed < landing.flightDuration; elapsed += campaignHopperFlightStep {
			step := landing
			step.flightElapsed = elapsed
			producers = append(producers, raknet.ScheduledPacketProducer{
				Delay: elapsed, Produce: step.advanceHopperFlight,
			})
		}
	}
	producers = append(producers, raknet.ScheduledPacketProducer{
		Delay: landing.flightDuration, Produce: landing.landHopper,
	})
	cancel, scheduleErr := scheduleNPCProducers(e.runtime.registry, e.packet, producers)
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		return e.fail("hopperJumpSchedule", scheduleErr)
	}
	return packets, nil
}

func (e campaignLeapSchedule) advanceHopperFlight() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceGenerationActive(
		e.generation, e.objectID, e.actionGeneration,
	)
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	source, isSourceFound := peerSession.zone.NPCs().NPC(e.objectID)
	if !isSourceFound || source.ActionGeneration != e.actionGeneration {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	travelDistance := e.destination.Sub(e.origin).Length()
	traveled := min(travelDistance,
		e.plan.Profile.ForcedMovementSpeed*float32(e.flightElapsed.Seconds()))
	fraction := traveled / travelDistance
	position := e.origin.Add(e.destination.Sub(e.origin).Scale(fraction))
	position.Z += hopperFlightHeight(travelDistance, e.hopperDistance, traveled)
	err := peerSession.zone.NPCs().SetPosition(e.objectID, position)
	e.runtime.registry.mutex.Unlock()
	if err != nil {
		return e.fail("hopperFlightPosition", err)
	}
	return nil, nil
}

// hopperFlightHeight mirrors build 103's jump arc initialized by sub_A145E0.
// The fourth jump parameter is half the requested distance, while travel may
// be shortened by the native destination projection.
func hopperFlightHeight(travelDistance float32, requestedDistance float32, traveled float32) float32 {
	if travelDistance <= 0 || requestedDistance <= 0 {
		return 0
	}
	half := travelDistance * 0.5
	requestedHalf := requestedDistance * 0.5
	arcWidth := min(requestedHalf, half)
	peak := float32(4)
	if requestedDistance >= travelDistance {
		peak = max(float32(2), 4*travelDistance/requestedDistance)
	}
	denominator := half*half - (half-arcWidth)*(half-arcWidth)
	if denominator <= 0.001 {
		denominator = half * half
	}
	curvature := peak / denominator
	fromMiddle := traveled - half
	height := curvature * (half*half - fromMiddle*fromMiddle)
	return max(float32(0), min(peak, height))
}
