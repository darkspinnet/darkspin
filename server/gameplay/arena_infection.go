package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
)

type arenaInfectionWorld struct {
	registry   *gameplaySessionRegistry
	gameID     uint32
	runs       map[*arenaInfectionRun]struct{}
	entries    map[uint32]*arenaInfectionEntry
	immunities map[uint32]time.Time
}

type arenaInfectionRun struct {
	runtime        campaignAbilityCommandRuntime
	packet         raknet.Packet
	world          *arenaInfectionWorld
	sessionKey     string
	gameID         uint32
	generation     uint64
	sourceObjectID uint32
	source         gameplayPeerSession
	startedAt      time.Time
	creature       game.GameplayCreature
	definition     sim.AbilityDefinition
	abilityID      uint32
	releasePacket  []byte
	entries        map[uint32]*arenaInfectionEntry
	tickCancel     raknet.CancelSchedule
	releaseCancel  raknet.CancelSchedule
	isAccepted     bool
}

type arenaInfectionEntry struct {
	run       *arenaInfectionRun
	objectID  uint32
	status    *arenaHitStatus
	periodic  *arenaBasicBurnRun
	age       uint32
	tickLimit uint32
	isPrimary bool
	expiresAt time.Time
}

func (e *gameplaySessionRegistry) arenaInfectionWorldLocked(gameID uint32) (*arenaInfectionWorld, error) {
	world, err := e.arenaAbilityWorldLocked(gameID)
	if err != nil {
		return nil, fmt.Errorf("infectionAbilityWorld: %w", err)
	}
	if world.infectionWorld == nil {
		world.infectionWorld = &arenaInfectionWorld{registry: e, gameID: gameID,
			runs: make(map[*arenaInfectionRun]struct{}), entries: make(map[uint32]*arenaInfectionEntry),
			immunities: make(map[uint32]time.Time)}
	}
	return world.infectionWorld, nil
}

func (e *arenaInfectionRun) currentLocked() (gameplayPeerSession, bool) {
	source, isFound := e.runtime.registry.sessions[e.sessionKey]
	match := e.runtime.registry.arenaMatches[e.gameID]
	_, isRunFound := e.world.runs[e]
	isCurrent := isFound && isRunFound && !e.runtime.registry.isClosed && match != nil && match.isCombatActive &&
		match.abilityWorld != nil && match.abilityWorld.infectionWorld == e.world &&
		source.generation == e.generation && source.binding.Mode == game.ModeArena &&
		source.binding.GameID == e.gameID && source.isArenaCombatActive &&
		source.deployedObjectID == e.sourceObjectID && source.deployedHitPoint() > 0
	return source, isCurrent
}

func (e *arenaInfectionRun) retireLocked() {
	if _, isFound := e.world.runs[e]; !isFound {
		return
	}
	delete(e.world.runs, e)
	if e.tickCancel != nil {
		e.tickCancel()
		e.tickCancel = nil
	}
	if e.releaseCancel != nil {
		e.releaseCancel()
		e.releaseCancel = nil
	}
	for _, entry := range e.entries {
		e.world.retireEntryLocked(entry, e.runtime.now())
	}
}

func (e *arenaInfectionRun) retireRootsLocked() {
	for key, member := range e.runtime.registry.sessions {
		if member.binding.Mode != game.ModeArena || member.binding.GameID != e.gameID {
			continue
		}
		status := member.arenaHitStatuses[arenaHitStatusKey{modifierID: e.definition.SecondaryModifierID}]
		if status == nil || status.sourceObjectID != e.sourceObjectID || status.kind != arenaChargeRoot {
			continue
		}
		status.retire(&member)
		e.runtime.registry.sessions[key] = member
	}
}

func (e *arenaInfectionWorld) retireEntryLocked(entry *arenaInfectionEntry, now time.Time) {
	if e.entries[entry.objectID] != entry {
		return
	}
	delete(e.entries, entry.objectID)
	delete(entry.run.entries, entry.objectID)
	if entry.run.isAccepted && (entry.isPrimary || !now.Before(e.immunities[entry.objectID])) {
		e.immunities[entry.objectID] = now.Add(7 * time.Second)
	}
	member, isFound := e.registry.sessions[entry.status.targetKey]
	if !isFound {
		member = gameplayPeerSession{}
	}
	entry.status.retire(&member)
	if isFound {
		e.registry.sessions[entry.status.targetKey] = member
	}
}

func (e *gameplaySessionRegistry) retireArenaInfectionsLocked(gameID uint32) {
	match := e.arenaMatches[gameID]
	if match == nil || match.abilityWorld == nil || match.abilityWorld.infectionWorld == nil {
		return
	}
	world := match.abilityWorld.infectionWorld
	for run := range world.runs {
		run.retireLocked()
		run.retireRootsLocked()
	}
	match.abilityWorld.infectionWorld = nil
}

func (e *gameplaySessionRegistry) retireArenaInfectionsForOwnerLocked(gameID uint32, objectID uint32) {
	match := e.arenaMatches[gameID]
	if match == nil || match.abilityWorld == nil || match.abilityWorld.infectionWorld == nil {
		return
	}
	for run := range match.abilityWorld.infectionWorld.runs {
		if run.sourceObjectID == objectID {
			run.retireLocked()
			run.retireRootsLocked()
		}
	}
}
