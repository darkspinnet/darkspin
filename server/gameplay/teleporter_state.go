package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	zonesecurity "github.com/darkspinnet/darkspin/server/zone/security"
	securityraknet "github.com/darkspinnet/darkspin/server/zone/security/raknet103"
)

// Build-103 Modifiers/0xA36FCF98.lua replaces each power transition after
// teleportActivationTime=1. The current run identity prevents older finishes
// from restoring a loop after another threat-state change or scene reload.
type campaignTeleporterTransition struct {
	clock    *campaignClockRun
	markerID uint32
	objectID uint32
	teleport zonesecurity.Teleport
	isActive bool
}

func (e *campaignTeleporterTransition) finish() ([][]byte, error) {
	e.clock.registry.mutex.Lock()
	defer e.clock.registry.mutex.Unlock()
	member, isFound := e.clock.registry.sessions[e.clock.sessionKey]
	if !isFound || member.campaignClock != e.clock ||
		!member.stage.IsDungeon() || member.isZoneTerminal() ||
		member.generation != e.clock.generation ||
		member.transportGeneration != e.clock.transportGeneration ||
		member.campaignTeleporterTransitions[e.markerID] != e ||
		member.campaignTeleporterStates[e.markerID] != e.isActive {
		return nil, nil
	}
	packets, err := securityraknet.State(e.objectID, e.teleport, e.isActive, false)
	if err != nil {
		return nil, fmt.Errorf("teleporterFinishState: %w", err)
	}
	delete(member.campaignTeleporterTransitions, e.markerID)
	e.clock.registry.sessions[e.clock.sessionKey] = member
	return packets, nil
}

func (e *gameplayPeerSession) beginCampaignTeleporterTransition(
	markerID, objectID uint32, teleport zonesecurity.Teleport, isActive bool,
) ([][]byte, error) {
	if e.campaignClock == nil {
		packets, err := securityraknet.State(objectID, teleport, isActive, false)
		if err != nil {
			return nil, fmt.Errorf("teleporterSteadyState: %w", err)
		}
		return packets, nil
	}
	packets, err := securityraknet.Transition(objectID, teleport, isActive)
	if err != nil {
		return nil, fmt.Errorf("teleporterTransition: %w", err)
	}
	run := &campaignTeleporterTransition{
		clock: e.campaignClock, markerID: markerID, objectID: objectID,
		teleport: teleport, isActive: isActive,
	}
	cancel, err := e.campaignClock.packet.Autonomous().ScheduleProducers([]raknet.ScheduledPacketProducer{
		{Delay: time.Second, Produce: run.finish},
	})
	if err != nil {
		return nil, fmt.Errorf("teleporterSchedule: %w", err)
	}
	if cancel == nil {
		return nil, fmt.Errorf("teleporterSchedule: cancellation unavailable")
	}
	if e.campaignTeleporterTransitions == nil {
		e.campaignTeleporterTransitions = make(map[uint32]*campaignTeleporterTransition)
	}
	e.campaignTeleporterTransitions[markerID] = run
	return packets, nil
}
