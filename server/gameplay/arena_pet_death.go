package gameplay

import (
	"errors"
	"fmt"

	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	companionraknet "github.com/darkspinnet/darkspin/server/zone/companion/raknet103"
)

type arenaCombatPetDeathStep struct {
	run   *arenaCombatPetRun
	actor zonecompanion.Actor
}

func (e arenaCombatPetDeathStep) execute() {
	e.run.runtime.registry.mutex.Lock()
	defer e.run.runtime.registry.mutex.Unlock()
	match := e.run.runtime.registry.arenaMatches[e.run.gameID]
	if match == nil || match.abilityWorld != e.run.world ||
		!e.run.world.companion.FinishDeath(e.actor) {
		return
	}
	e.run.runtime.registry.queueArenaForcePulsePacketsLocked(e.run.gameID, [][]byte{e.run.deletePacket})
}

// The build-103 Plasma Sentinel death callback holds its corpse for the
// recovered animation duration. Keep that same identity in the Arena world.
func (e *arenaCombatPetRun) beginDeathLocked(actor zonecompanion.Actor, timestamp uint64) ([][]byte, error) {
	profile, isKnown := zonecompanion.DeathProfileForNoun(actor.Noun)
	if !isKnown || e.runtime.npc.timer == nil {
		return nil, errors.New("Arena pet death profile unavailable")
	}
	packets, err := companionraknet.Death(actor, profile, timestamp)
	if err != nil {
		return nil, fmt.Errorf("arenaPetDeathPacket: %w", err)
	}
	step := arenaCombatPetDeathStep{run: e, actor: actor}
	cancel, err := e.runtime.npc.timer.Schedule(profile.Duration, step.execute)
	if err == nil && cancel == nil {
		err = errors.New("Arena pet death cancellation unavailable")
	}
	if err != nil {
		return nil, fmt.Errorf("arenaPetDeathSchedule: %w", err)
	}
	if !e.world.companion.BeginDeath(actor) {
		cancel()
		return nil, errors.New("Arena pet death identity changed")
	}
	e.isPublished = false
	e.isCorpseRetained = true
	e.retireLocked()
	if len(e.modifierPacket) != 0 {
		packets = append([][]byte{e.modifierPacket}, packets...)
	}
	return packets, nil
}
