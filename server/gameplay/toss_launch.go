package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
)

type campaignTossRun struct {
	cancel        raknet.CancelSchedule
	landingCancel raknet.CancelSchedule
	isLaunched    bool
	isLanded      bool
	isReleased    bool
}

func (e *campaignTossRun) stop() {
	if e == nil {
		return
	}
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	if e.landingCancel != nil {
		e.landingCancel()
		e.landingCancel = nil
	}
}

func (e campaignTossSchedule) produceLaunch() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) || e.run.isLaunched {
		return nil, nil
	}
	destination := e.plan.Destination
	target, isTargetFound := peerSession.zone.NPCs().NPC(e.plan.TargetObjectID)
	if isTargetFound && !target.IsDefeated && target.HitPoint > 0 {
		destination = sim.Position(target.Plan.Position)
	}
	launchTime := max(e.plan.Definition.HitDelay, e.runtime.now().Sub(e.startTime))
	plan, err := zoneability.RefreshTossLaunch(e.plan, destination, launchTime)
	if err != nil {
		return nil, fmt.Errorf("tossRefresh: %w", err)
	}
	e.plan = plan
	packets, err := abilityraknet.TossLaunch(e.plan, e.projectileObjectID, e.packet.SourceTime)
	if err != nil {
		return nil, fmt.Errorf("tossLaunch: %w", err)
	}
	landingElapses := []time.Duration{0}
	if e.plan.Definition.Toss.Behavior == sim.TossAbilityBehaviorTrapper {
		landingElapses = []time.Duration{
			0, 500 * time.Millisecond, time.Second, e.plan.Definition.Toss.GrenadeTimer,
		}
	}
	producers := make([]raknet.ScheduledPacketProducer, 0, len(landingElapses))
	for _, elapsed := range landingElapses {
		producers = append(producers, e.landingProducer(elapsed))
	}
	// This callback already owns the registry write lock. Capture the guard
	// identity from that session instead of acquiring the same lock again.
	identity := gameplayProducerIdentityFromSession(e.sessionKey, peerSession, isFound)
	producers = e.runtime.registry.producerGuard.scheduledProducersForIdentity(identity, producers)
	var cancel raknet.CancelSchedule
	if e.packet.ScheduleGroupResult != nil {
		cancel, err = e.packet.ScheduleGroupResult(producers, e.fail)
	} else {
		cancel, err = e.packet.ScheduleProducers(producers)
	}
	if err != nil {
		return nil, fmt.Errorf("tossLandingSchedule: %w", err)
	}
	e.run.landingCancel = cancel
	e.run.isLaunched = true
	e.runtime.logger.Printf(
		"RakNet projectile trajectory launched kind=hero-toss projectile=%d source=%d target=%d ability=%q launch=(%.3f,%.3f,%.3f) destination=(%.3f,%.3f,%.3f) flight_ms=%d release_ms=%d launch_ms=%d target_refreshed=%t",
		e.projectileObjectID, e.sourceObjectID, e.plan.TargetObjectID,
		e.plan.Definition.Name, e.plan.LaunchPosition.X, e.plan.LaunchPosition.Y,
		e.plan.LaunchPosition.Z, e.plan.Destination.X, e.plan.Destination.Y,
		e.plan.Destination.Z, e.plan.Lob.Duration.Milliseconds(),
		e.plan.Definition.ReleaseDelay.Milliseconds(), launchTime.Milliseconds(),
		isTargetFound && !target.IsDefeated && target.HitPoint > 0,
	)
	return packets, nil
}
