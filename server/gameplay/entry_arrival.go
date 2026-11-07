package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/zone"
	heroraknet "github.com/darkspinnet/darkspin/server/zone/hero/raknet103"
)

const campaignEntrySlotInterval = 200 * time.Millisecond

func campaignEntryDelay(binding game.GameplayBinding) time.Duration {
	if binding.Mode != game.ModeChain {
		return 0
	}
	return time.Duration(binding.Slot) * campaignEntrySlotInterval
}

// Keep scene setup immediate, but reveal each hero and start its beam together
// after its slot delay. Every observer receives the same arrival publication.
type campaignEntryArrival struct {
	registry   *gameplaySessionRegistry
	packet     raknet.Packet
	generation uint64
	setupEpoch uint64
	objectID   uint32
	beamName   string
	position   game.Vec3
	timestamp  uint64
	startedAt  time.Time
	delay      time.Duration
}

func (e gameplaySetupRuntime) campaignEntryPackets(
	packet raknet.Packet, member gameplayPeerSession, setupEpoch uint64,
	position game.Vec3, timestamp uint64,
) ([][]byte, error) {
	beamName := campaignCharacterEntry(member.binding.Creatures[member.deployedCreatureIndex])
	delay := campaignEntryDelay(member.binding)
	if delay == 0 {
		packets, err := heroraknet.BeamIn(member.deployedObjectID, beamName, position, timestamp)
		if err != nil {
			return nil, fmt.Errorf("entryBeam: %w", err)
		}
		return packets, nil
	}
	hidePacket, err := raknet.MarshalApplication(raknet.ObjectUpdateMessage{
		ObjectID: member.deployedObjectID, PositionX: position.X,
		PositionY: position.Y, PositionZ: position.Z, IsVisible: false,
	})
	if err != nil {
		return nil, fmt.Errorf("entryHide: %w", err)
	}
	arrival := campaignEntryArrival{
		registry: e.registry, packet: packet, generation: member.generation,
		setupEpoch: setupEpoch, objectID: member.deployedObjectID,
		beamName: beamName, position: position, timestamp: timestamp,
		startedAt: e.now(), delay: delay,
	}
	err = packet.AfterResponseCommit(arrival.commit)
	if err != nil {
		return nil, fmt.Errorf("entryCommit: %w", err)
	}
	e.registry.mutex.Lock()
	current, isFound := e.registry.sessions[packet.Address.String()]
	if isFound && current.generation == member.generation {
		current.campaignEntryPendingObjectID = member.deployedObjectID
		e.registry.sessions[packet.Address.String()] = current
	}
	e.registry.mutex.Unlock()
	return [][]byte{hidePacket}, nil
}

// A joining observer's baseline must not reveal a teammate whose beam is
// still waiting for its slot. The scheduled arrival publishes the reveal.
func (e gameplaySetupRuntime) pendingCampaignEntryPackets(campaignZone *zone.Zone) ([][]byte, error) {
	e.registry.mutex.RLock()
	defer e.registry.mutex.RUnlock()
	packets := make([][]byte, 0)
	for _, member := range e.registry.sessions {
		if member.zone != campaignZone || member.campaignEntryPendingObjectID == 0 {
			continue
		}
		packet, err := raknet.MarshalApplication(raknet.ObjectUpdateMessage{
			ObjectID:  member.campaignEntryPendingObjectID,
			PositionX: member.playerPosition.X, PositionY: member.playerPosition.Y,
			PositionZ: member.playerPosition.Z, IsVisible: false,
		})
		if err != nil {
			return nil, fmt.Errorf("entryBaselineHide: %w", err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func (e campaignEntryArrival) commit() {
	if e.packet.ScheduleFunc != nil {
		err := e.packet.ScheduleFunc(e.delay, e.produce)
		if err == nil {
			return
		}
		e.registry.logger.Printf("RakNet campaign entry schedule failed object=%d: %v", e.objectID, err)
	} else {
		e.registry.logger.Printf("RakNet campaign entry scheduler unavailable object=%d", e.objectID)
	}
	// Preserve a playable entry if transport scheduling fails after setup was
	// delivered. The fallback still uses the shared reliable presentation queue.
	packets, err := e.produce()
	if err != nil {
		e.registry.logger.Printf("RakNet campaign entry fallback failed object=%d: %v", e.objectID, err)
	}
	if len(packets) != 0 {
		e.registry.logger.Printf("RakNet campaign entry fallback returned unexpected packets object=%d", e.objectID)
	}
}

func (e campaignEntryArrival) produce() ([][]byte, error) {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	member, isFound := e.registry.sessions[e.packet.Address.String()]
	if !isFound || member.generation != e.generation ||
		member.transportGeneration != e.packet.TransportGeneration ||
		member.deployedObjectID != e.objectID || !member.stage.IsDungeon() ||
		member.campaignEntryPendingObjectID != e.objectID ||
		!member.dungeonSetup.IsCommittedGeneration(e.setupEpoch) || member.isRejoinPending {
		return nil, nil
	}
	packets, err := heroraknet.BeamIn(e.objectID, e.beamName, e.position,
		advanceHeroArrivalTimestamp(e.timestamp, e.startedAt, e.registry.now()))
	if err != nil {
		return nil, fmt.Errorf("entryArrival: %w", err)
	}
	member.campaignEntryPendingObjectID = 0
	member.beginHeroArrival(e.registry.now())
	e.registry.sessions[e.packet.Address.String()] = member
	for key, candidate := range e.registry.sessions {
		if candidate.binding.IsServerBot || candidate.zone != member.zone ||
			candidate.isRejoinPending || !candidate.isCampaignPresentationAvailable() {
			continue
		}
		queueErr := candidate.queueCampaignPresentation(packets)
		if queueErr != nil {
			e.registry.logger.Printf("RakNet campaign entry publication failed object=%d user=%d: %v",
				e.objectID, candidate.binding.UserID, queueErr)
		}
		e.registry.sessions[key] = candidate
	}
	return nil, nil
}
