package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/navigation"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	zoneobjectid "github.com/darkspinnet/darkspin/server/zone/objectid"
)

// Arena shares one object namespace and effect lifetime store across teams.
// It has no campaign director, objectives or NPC population to manufacture.
type arenaAbilityWorld struct {
	objectIDSession *zoneobjectid.Session
	companion       *zonecompanion.Session
	treeSession     *zoneability.TreeOfLifePresentationSession
	forcePulses     map[arenaForcePulseKey]*arenaForcePulseRun
	claymores       map[uint32]*arenaClaymoreRun
	sprites         map[uint32]*arenaSpriteRun
	combatPets      map[uint32]*arenaCombatPetRun
	sages           map[uint32]*arenaSagePassive
	charges         map[uint32]*arenaHeroChargeRun
	gravityStorms   map[uint32]*arenaGravityStormRun
	sproutWorld     *arenaSproutWorld
	infectionWorld  *arenaInfectionWorld
	rides           map[uint32]*arenaRideRun
	effectInventory *zoneeffect.Inventory
}

func abilityActorTeam(member gameplayPeerSession) uint8 {
	if member.binding.Mode == game.ModeArena {
		return uint8(member.binding.Team)
	}
	return 1
}

func (e *gameplaySessionRegistry) abilityNavigationLocked(member gameplayPeerSession) *navigation.Mesh {
	if member.binding.Mode == game.ModeArena {
		match := e.arenaMatches[member.binding.GameID]
		if match == nil {
			return nil
		}
		return match.navigation
	}
	if member.zone == nil {
		return nil
	}
	return member.zone.Navigation()
}

func (e *gameplaySessionRegistry) arenaAbilityWorldLocked(gameID uint32) (*arenaAbilityWorld, error) {
	match := e.arenaMatches[gameID]
	if match == nil {
		return nil, errors.New("arena ability world unavailable")
	}
	if match.abilityWorld != nil {
		return match.abilityWorld, nil
	}
	objectIDSession, err := zoneobjectid.NewSession(1000, ^uint32(0))
	if err != nil {
		return nil, fmt.Errorf("arenaObjectIDs: %w", err)
	}
	match.abilityWorld = &arenaAbilityWorld{objectIDSession: objectIDSession, companion: zonecompanion.NewSessionWithAggroRanges(e.companionAggroRanges), treeSession: zoneability.NewTreeOfLifePresentationSession(), effectInventory: zoneeffect.NewInventory()}
	return match.abilityWorld, nil
}

func (e *gameplaySessionRegistry) abilityEffectsLocked(member gameplayPeerSession) (*zoneeffect.Inventory, error) {
	if member.binding.Mode != game.ModeArena {
		if member.zone == nil || member.zone.Effect() == nil {
			return nil, errors.New("ability effect inventory unavailable")
		}
		return member.zone.Effect(), nil
	}
	world, err := e.arenaAbilityWorldLocked(member.binding.GameID)
	if err != nil {
		return nil, fmt.Errorf("effectInventory: %w", err)
	}
	return world.effectInventory, nil
}

func (e *gameplaySessionRegistry) reserveAbilityObjectIDsLocked(member *gameplayPeerSession, count uint32, minimum uint32) (uint32, error) {
	if member.binding.Mode != game.ModeArena {
		objectID, err := member.reserveCampaignProjectileIDs(count, minimum)
		if err != nil {
			return 0, fmt.Errorf("campaignEffectID: %w", err)
		}
		return objectID, nil
	}
	world, err := e.arenaAbilityWorldLocked(member.binding.GameID)
	if err != nil {
		return 0, fmt.Errorf("effectWorld: %w", err)
	}
	objectID, err := world.objectIDSession.Reserve(count)
	if err != nil {
		return 0, fmt.Errorf("effectReserve: %w", err)
	}
	return objectID, nil
}

func (e *gameplaySessionRegistry) abilityTreeSessionLocked(member gameplayPeerSession) (*zoneability.TreeOfLifePresentationSession, error) {
	if member.binding.Mode != game.ModeArena {
		if member.zone == nil {
			return nil, errors.New("tree world unavailable")
		}
		return member.zone.TreeOfLife(), nil
	}
	world, err := e.arenaAbilityWorldLocked(member.binding.GameID)
	if err != nil {
		return nil, fmt.Errorf("treeWorld: %w", err)
	}
	return world.treeSession, nil
}

func (e *gameplaySessionRegistry) abilityElapsedLocked(member gameplayPeerSession, now time.Time) time.Duration {
	if member.binding.Mode != game.ModeArena {
		return member.zone.Elapsed(now)
	}
	match := e.arenaMatches[member.binding.GameID]
	if match == nil || match.result.startedAt.IsZero() {
		return 0
	}
	return max(time.Duration(0), now.Sub(match.result.startedAt))
}

func (e *gameplaySessionRegistry) abilityRandomLocked(member gameplayPeerSession) (*sim.SimulatorRandom, error) {
	if member.binding.Mode != game.ModeArena {
		random, err := member.abilityRandom()
		if err != nil {
			return nil, fmt.Errorf("campaignRandom: %w", err)
		}
		return random, nil
	}
	match := e.arenaMatches[member.binding.GameID]
	if match == nil || match.random == nil {
		return nil, errors.New("arena random unavailable")
	}
	return match.random, nil
}

func (e *gameplaySessionRegistry) queueArenaAbilityResourcesLocked(source gameplayPeerSession, packets [][]byte) {
	if source.binding.Mode != game.ModeArena {
		return
	}
	resources := make([][]byte, 0)
	for _, packet := range packets {
		if len(packet) != 0 && raknet.PacketID(packet[0]) == raknet.LabsPlayerUpdate {
			resources = append(resources, packet)
		}
	}
	if len(resources) == 0 {
		return
	}
	for key, member := range e.sessions {
		if member.binding.Mode != game.ModeArena || member.binding.GameID != source.binding.GameID || member.binding.UserID == source.binding.UserID {
			continue
		}
		member.queuePackets(resources)
		e.sessions[key] = member
	}
}
