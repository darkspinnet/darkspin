package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	"github.com/darkspinnet/darkspin/server/zone"
	companionraknet "github.com/darkspinnet/darkspin/server/zone/companion/raknet103"
	deathraknet "github.com/darkspinnet/darkspin/server/zone/death/raknet103"
)

const dendroneCorpseDuration = 10 * time.Second

type companionCorpseCleanup struct {
	registry *gameplaySessionRegistry
	zone     *zone.Zone
	packet   []byte
}

func (e companionCorpseCleanup) execute() {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	for sessionKey, peerSession := range e.registry.sessions {
		if peerSession.zone != e.zone || peerSession.isZoneTerminal() {
			continue
		}
		peerSession.queueCampaignPackets([][]byte{e.packet})
		e.registry.sessions[sessionKey] = peerSession
	}
}

// Ordinary despawns still delete immediately. A killed Dendrone keeps its
// presentation long enough to play the noun's death animation before cleanup.
func (e campaignNPCActionRuntime) companionDefeat(
	peerSession *gameplayPeerSession, objectID uint32, timestamp uint64,
) ([][]byte, error) {
	deletePacket, err := companionraknet.Defeat(objectID)
	if err != nil {
		return nil, fmt.Errorf("companionDelete: %w", err)
	}
	actor, isFound := peerSession.zone.Companion().Snapshot(objectID)
	nounName := e.program.SupportHealerPassive.CompanionNoun
	if !isFound || nounName == "" || actor.Noun != util.HashID(nounName) || e.timer == nil {
		return [][]byte{deletePacket}, nil
	}
	physics := e.program.NPCDeathPhysics(nounName)
	messages := []raknet.ApplicationMessage{
		raknet.ObjectPlayerMoveMessage{ObjectID: objectID, GoalFlags: 0x20,
			GoalPosition: raknet.Vector3(actor.Position)},
		raknet.ObjectCollisionUpdateMessage{ObjectID: objectID, IsCollisionEnabled: false},
		raknet.SetAnimationStateMessage{ObjectID: objectID,
			State:     util.HashID(deathraknet.DeathAnimation(physics.OrdinaryDeathAnimation)),
			Timestamp: timestamp, Scale: 1},
		raknet.CombatantDataDeltaMessage{ObjectID: objectID, HitPoints: 0, IsHitPointChanged: true},
	}
	packets := make([][]byte, 0, len(messages))
	for _, message := range messages {
		packet, marshalErr := raknet.MarshalApplication(message)
		if marshalErr != nil {
			return nil, fmt.Errorf("companionDeathMarshal: %w", marshalErr)
		}
		packets = append(packets, packet)
	}
	cleanup := companionCorpseCleanup{registry: e.registry, zone: peerSession.zone, packet: deletePacket}
	cancel, err := e.timer.Schedule(dendroneCorpseDuration, cleanup.execute)
	if err != nil {
		return nil, fmt.Errorf("companionDeathSchedule: %w", err)
	}
	if cancel == nil {
		return nil, fmt.Errorf("companionDeathSchedule: missing cancellation")
	}
	return packets, nil
}
