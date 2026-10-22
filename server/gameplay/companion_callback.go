package gameplay

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/zone"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	companionraknet "github.com/darkspinnet/darkspin/server/zone/companion/raknet103"
)

type companionDeathKey struct {
	world    *zone.Zone
	objectID uint32
}
type companionDeathRun struct {
	registry     *gameplaySessionRegistry
	key          companionDeathKey
	gameID       uint32
	actor        zonecompanion.Actor
	deletePacket []byte
	cancel       func()
}

// The caller holds registry.mutex at the established committed-HP death boundary.
func (e campaignNPCActionRuntime) scheduleCompanionDeathLocked(member *gameplayPeerSession, actor zonecompanion.Actor, timestamp uint64) (bool, error) {
	profile, isKnown := zonecompanion.DeathProfileForNoun(actor.Noun)
	if !isKnown {
		return false, nil
	}
	if e.registry.isClosed {
		return false, errors.New("pet death registry closed")
	}
	if e.timer == nil || member.zone == nil || member.zone.Companion() == nil {
		return false, errors.New("pet death timer unavailable")
	}
	key := companionDeathKey{world: member.zone, objectID: actor.ObjectID}
	if e.registry.companionDeaths[key] != nil {
		return true, nil
	}
	packets, err := companionraknet.Death(actor, profile, timestamp)
	if err != nil {
		return false, fmt.Errorf("deathEncode: %w", err)
	}
	deletePacket, err := companionraknet.Defeat(actor.ObjectID)
	if err != nil {
		return false, fmt.Errorf("deleteEncode: %w", err)
	}
	run := &companionDeathRun{registry: e.registry, key: key, actor: actor, gameID: member.binding.GameID, deletePacket: deletePacket}
	if e.registry.companionDeaths == nil {
		e.registry.companionDeaths = make(map[companionDeathKey]*companionDeathRun)
	}
	e.registry.companionDeaths[key] = run
	cancel, err := e.timer.Schedule(profile.Duration, run.execute)
	if err == nil && cancel == nil {
		err = errors.New("pet death cancellation unavailable")
	}
	if err != nil {
		if cancel != nil {
			cancel()
		}
		delete(e.registry.companionDeaths, key)
		return false, fmt.Errorf("deathSchedule: %w", err)
	}
	run.cancel = cancel
	if !member.zone.Companion().BeginDeath(actor) {
		cancel()
		delete(e.registry.companionDeaths, key)
		return false, errors.New("pet death identity changed")
	}
	// Both animation and deletion use the same retained FIFO, so a slow original
	// transport cannot publish the old death pose after its cleanup packet.
	member.queueCampaignPackets(packets)
	e.registry.queueCompanionDeathPeersLocked(member, packets)
	return true, nil
}

func (e *companionDeathRun) execute() {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	if e.registry.companionDeaths[e.key] != e {
		return
	}
	delete(e.registry.companionDeaths, e.key)
	e.cancel = nil
	if e.registry.isClosed || e.key.world.Companion() == nil || !e.key.world.Companion().FinishDeath(e.actor) {
		return
	}
	e.registry.queueCompanionDeathWorldLocked(e.key.world, [][]byte{e.deletePacket})
}

func (e *gameplaySessionRegistry) queueCompanionDeathPeersLocked(caster *gameplayPeerSession, packets [][]byte) {
	for sessionKey, member := range e.sessions {
		if member.isZoneTerminal() || member.zone != caster.zone || (member.binding.UserID == caster.binding.UserID && member.generation == caster.generation) {
			continue
		}
		member.queueCampaignPackets(packets)
		e.sessions[sessionKey] = member
	}
	for _, retained := range e.retainedSessions {
		if retained.peerSession.zone == caster.zone && !retained.peerSession.isZoneTerminal() {
			retained.peerSession.queueCampaignPackets(packets)
		}
	}
}

func (e *gameplaySessionRegistry) queueCompanionDeathWorldLocked(world *zone.Zone, packets [][]byte) {
	for sessionKey, member := range e.sessions {
		if member.isZoneTerminal() || member.zone != world {
			continue
		}
		member.queueCampaignPackets(packets)
		e.sessions[sessionKey] = member
	}
	for _, retained := range e.retainedSessions {
		if retained.peerSession.zone == world && !retained.peerSession.isZoneTerminal() {
			retained.peerSession.queueCampaignPackets(packets)
		}
	}
}

// Shutdown/zone discard owns terminal cleanup; ordinary transport disconnects
// need no callback handoff because the timer owns world and corpse identities.
func (e *gameplaySessionRegistry) retireCompanionDeathsLocked(gameID uint32) {
	for key, run := range e.companionDeaths {
		if gameID != 0 && run.gameID != gameID {
			continue
		}
		if run.cancel != nil {
			run.cancel()
			run.cancel = nil
		}
		delete(e.companionDeaths, key)
		if key.world.Companion() != nil {
			isFinished := key.world.Companion().FinishDeath(run.actor)
			if !isFinished {
				// An owner expiry or earlier deletion already retired this exact corpse.
				continue
			}
		}
		if !e.isClosed {
			e.queueCompanionDeathWorldLocked(key.world, [][]byte{run.deletePacket})
		}
	}
}
