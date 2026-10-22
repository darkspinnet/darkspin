package gameplay

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
)

// Continue only after the preceding tick's packets have been committed. A
// finite cast-time NPC count cannot bound a disease that reaches later spawns.
func (e heroInfectionTickStep) commit() {
	err := e.scheduleNext()
	if err != nil {
		e.schedule.fail(err)
	}
}

func (e heroInfectionTickStep) scheduleNext() error {
	schedule := e.schedule
	schedule.runtime.registry.mutex.RLock()
	member, isFound := schedule.runtime.registry.sessions[schedule.sessionKey]
	isCurrent := schedule.isCurrent(member, isFound) && schedule.run.isAccepted && !schedule.run.IsEmpty()
	schedule.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil
	}
	next := heroInfectionTickStep{schedule: schedule, deadline: e.deadline + schedule.definition.TickDuration}
	producers := schedule.runtime.registry.producerGuard.scheduledProducers(schedule.sessionKey,
		[]raknet.ScheduledPacketProducer{{
			Delay:   max(time.Duration(0), schedule.startedAt.Add(next.deadline).Sub(schedule.runtime.now())),
			Produce: next.produce, AfterCommit: next.commit,
		}})
	var cancel raknet.CancelSchedule
	var err error
	if schedule.packet.ScheduleGroupResult != nil {
		cancel, err = schedule.packet.ScheduleGroupResult(producers, schedule.fail)
	} else if schedule.packet.ScheduleGroup != nil {
		cancel, err = schedule.packet.ScheduleGroup(producers)
	} else {
		err = errors.New("infection continuation scheduler unavailable")
	}
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return fmt.Errorf("tickSchedule: %w", err)
	}
	if cancel == nil {
		return errors.New("infection continuation cancellation unavailable")
	}
	schedule.runtime.registry.mutex.Lock()
	member, isFound = schedule.runtime.registry.sessions[schedule.sessionKey]
	isCurrent = schedule.isCurrent(member, isFound)
	schedule.run.mutex.Lock()
	isCurrent = isCurrent && !schedule.run.isCleaned
	previousCancel := schedule.run.tickCancel
	if isCurrent {
		schedule.run.tickCancel = cancel
	}
	schedule.run.mutex.Unlock()
	schedule.runtime.registry.mutex.Unlock()
	if !isCurrent {
		cancel()
		return nil
	}
	if previousCancel != nil {
		previousCancel()
	}
	return nil
}

func (e *gameplayPeerSession) stopHeroInfections() {
	e.queueCampaignPackets(e.takeHeroInfections())
}

func (e *gameplayPeerSession) takeHeroInfections() [][]byte {
	packets := make([][]byte, 0)
	for run := range e.heroInfections {
		entries := run.Stop()
		packets = append(packets, infectionDeletionPackets(entries)...)
	}
	e.heroInfections = nil
	return packets
}

func infectionDeletionPackets(entries []heroInfectionEntry) [][]byte {
	packets := make([][]byte, 0, len(entries)*2)
	for _, entry := range entries {
		for _, instanceID := range []uint32{entry.modifierInstanceID, entry.rootInstanceID} {
			if instanceID == 0 {
				continue
			}
			packet, err := effectraknet.ModifierDelete(entry.objectID, instanceID)
			if err != nil {
				log.Printf("Infection cleanup deletion failed target=%d instance=%d: %v", entry.objectID, instanceID, err)
				continue
			}
			packets = append(packets, packet)
		}
	}
	return packets
}
