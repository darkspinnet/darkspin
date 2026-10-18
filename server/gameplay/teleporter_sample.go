package gameplay

import (
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/zone"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
)

// Polling owns this endpoint independently of command-driven pose updates.
// Identity fences prevent a retained endpoint from crossing a deployment,
// connection, zone, or replacement motion boundary.
type teleporterContactSample struct {
	position            game.Vec3
	zone                *zone.Zone
	motion              *zoneaction.Motion
	generation          uint64
	transportGeneration uint64
	objectID            uint32
	creatureIndex       uint32
	isSet               bool
}

func (e *gameplayPeerSession) previousTeleporterSample(current game.Vec3) game.Vec3 {
	sample := e.teleporterSample
	if !sample.isSet || sample.zone != e.zone || sample.motion != e.playerMotion ||
		sample.generation != e.generation ||
		sample.transportGeneration != e.transportGeneration ||
		sample.objectID != e.deployedObjectID ||
		sample.creatureIndex != e.deployedCreatureIndex {
		return current
	}
	return sample.position
}

func (e *gameplayPeerSession) retainTeleporterSample(position game.Vec3) {
	e.teleporterSample = teleporterContactSample{
		position: position, zone: e.zone, motion: e.playerMotion,
		generation: e.generation, transportGeneration: e.transportGeneration,
		objectID: e.deployedObjectID, creatureIndex: e.deployedCreatureIndex,
		isSet: true,
	}
}

// Seed at the authoritative relocation pose so the next ordinary movement
// still sweeps from its new origin, including before the first poll.
func (e *gameplayPeerSession) resetTeleporterSample() {
	e.retainTeleporterSample(game.Vec3(e.playerPosition))
}
