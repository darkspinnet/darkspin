//go:build scenario

package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/scenario"
	"github.com/darkspinnet/darkspin/server/zone"
)

func isScenarioHeroPeer(source, candidate gameplayPeerSession) bool {
	if source.binding.GameID != candidate.binding.GameID || source.binding.Mode != candidate.binding.Mode {
		return false
	}
	if source.binding.Mode == game.ModeArena {
		return true
	}
	return source.binding.Mode == game.ModeChain && source.zone != nil && candidate.zone == source.zone
}

func (e *gameplaySessionRegistry) validateScenarioHeroScopeLocked(member gameplayPeerSession, req scenario.HeroResetRequest) (int, error) {
	if member.binding.GameID != req.GameID || member.generation != req.PeerGeneration ||
		member.transportGeneration != req.TransportGeneration || member.deployedObjectID != req.ObjectID ||
		member.squad == nil || member.squad.State().IsGameOver || !member.dungeonSetup.IsCommitted() ||
		!member.stage.IsDungeon() || member.isRejoinPending {
		return 0, errors.New("hero fixture requires the observed committed living transport")
	}
	if member.binding.Mode == game.ModeArena {
		match := e.arenaMatches[req.GameID]
		if req.ZoneGeneration != 0 || !member.isArenaCombatActive || match == nil || !match.isCombatActive || match.isTransferActive {
			return 0, errors.New("hero fixture arena is unavailable or transitioning")
		}
	} else {
		if member.binding.Mode != game.ModeChain || member.zone == nil || member.zone.Hero() == nil {
			return 0, errors.New("hero fixture requires campaign or arena gameplay")
		}
		ss := member.zone.Snapshot()
		if req.ZoneGeneration == 0 || ss.Generation != req.ZoneGeneration || ss.State != zone.StateActive {
			return 0, errors.New("hero fixture campaign epoch changed or is terminal")
		}
	}
	ownedUsers := make(map[uint64]bool, len(req.OwnedUserIDs))
	for _, userID := range req.OwnedUserIDs {
		if userID > 0 {
			ownedUsers[uint64(userID)] = true
		}
	}
	count := 0
	for _, candidate := range e.sessions {
		if !isScenarioHeroPeer(member, candidate) {
			continue
		}
		if !ownedUsers[candidate.binding.UserID] || !candidate.dungeonSetup.IsCommitted() ||
			candidate.isRejoinPending || !candidate.stage.IsDungeon() ||
			(candidate.binding.Mode == game.ModeArena && !candidate.isArenaCombatActive) {
			return 0, errors.New("hero fixture requires every participant to be an owned ready client")
		}
		count++
	}
	if member.binding.Mode == game.ModeChain {
		ss := member.zone.Snapshot()
		if len(ss.Members) != count {
			return 0, errors.New("hero fixture campaign has unavailable participants")
		}
		for _, participant := range ss.Members {
			if !participant.IsConnected || !ownedUsers[participant.UserID] {
				return 0, errors.New("hero fixture campaign includes a disconnected or unowned participant")
			}
		}
	}
	return count, nil
}

func (e *gameplaySessionRegistry) scenarioCampaignMembers(member gameplayPeerSession, at time.Time) *scenario.WorldCampaign {
	if member.binding.Mode != game.ModeChain || member.zone == nil {
		return nil
	}
	ss := member.zone.Snapshot()
	result := &scenario.WorldCampaign{}
	for _, participant := range ss.Members {
		entry := scenario.WorldCampaignMember{UserID: participant.UserID, PeerGeneration: participant.PeerGeneration, ZoneGeneration: ss.Generation}
		for endpoint, candidate := range e.sessions {
			if !isScenarioHeroPeer(member, candidate) || candidate.binding.UserID != participant.UserID || candidate.generation != participant.PeerGeneration {
				continue
			}
			key := gameplaySessionMemberKey(candidate)
			entry.TransportGeneration = candidate.transportGeneration
			entry.DeployedIndex = candidate.deployedCreatureIndex
			entry.IsReady = participant.IsConnected && candidate.dungeonSetup.IsCommitted() && !candidate.isRejoinPending &&
				candidate.stage.IsDungeon() && e.memberEndpoints[key] == endpoint && e.memberTransports[key] == candidate.transportGeneration
			if candidate.squad == nil {
				entry.IsReady = false
				continue
			}
			for index, creature := range candidate.binding.Creatures {
				character, isFound := candidate.squad.Character(uint32(index))
				if !isFound || !character.IsAvailable {
					continue
				}
				maximumHealth, maximumPower := candidate.characterResourceMaximum(uint32(index))
				hero := scenario.WorldHero{Index: uint32(index), Noun: creature.Noun, HitPoint: character.HitPoints,
					ManaPoint: character.ManaPoints, MaximumHitPoint: maximumHealth, MaximumManaPoint: maximumPower, IsDefeated: character.HitPoints <= 0}
				if uint32(index) == candidate.deployedCreatureIndex {
					hero.ObjectID = candidate.deployedObjectID
				}
				candidate.fillScenarioHeroMotion(&hero, at)
				entry.Characters = append(entry.Characters, hero)
			}
		}
		result.Members = append(result.Members, entry)
	}
	return result
}

func (e *gameplaySessionRegistry) queueScenarioHeroPacketsLocked(member gameplayPeerSession, packets [][]byte) {
	e.queueFieldMedicRealmPacketsLocked(member.zone, arenaAbilityGameID(member), packets)
}

func (e gameplayPeerSession) isScenarioHeroRetired() bool { return e.scenarioAbility.retirement != nil }

func (e gameplayPeerSession) scenarioHeroZoneGeneration() uint64 {
	if e.zone == nil {
		return 0
	}
	return e.zone.Snapshot().Generation
}

func (e *gameplaySessionRegistry) syncScenarioCampaignHero(member *gameplayPeerSession) error {
	if member.binding.Mode != game.ModeChain {
		return nil
	}
	err := member.syncZoneHero()
	if err != nil {
		return fmt.Errorf("fixtureActor: %w", err)
	}
	err = member.syncZoneSquadCheckpoint()
	if err != nil {
		return fmt.Errorf("fixtureRoster: %w", err)
	}
	return nil
}

func (e *gameplaySessionRegistry) retireScenarioCampaignPresentation(member *gameplayPeerSession) {
	firePackets, fireErr := member.stopFireTempestActive()
	member.queuePackets(firePackets)
	if fireErr != nil && e.logger != nil {
		e.logger.Printf("Scenario fire cleanup user=%d: %v", member.binding.UserID, fireErr)
	}
	stealthPackets, stealthErr := member.stopTrapperStealth()
	member.queuePackets(stealthPackets)
	if stealthErr != nil && e.logger != nil {
		e.logger.Printf("Scenario stealth cleanup user=%d: %v", member.binding.UserID, stealthErr)
	}
	sagePackets, sageErr := member.stopCampaignSagePassive()
	member.queuePackets(sagePackets)
	if sageErr != nil && e.logger != nil {
		e.logger.Printf("Scenario Sage cleanup user=%d: %v", member.binding.UserID, sageErr)
	}
}

func (e gameplayPendingRuntime) observeScenarioHeroClock(packet raknet.Packet) {
	if packet.Address == nil || packet.SourceTime == 0 {
		return
	}
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	key := packet.Address.String()
	member, isFound := e.registry.sessions[key]
	if !isFound || member.transportGeneration != packet.TransportGeneration {
		return
	}
	member.scenarioAbility.lastSourceTime = packet.SourceTime
	member.scenarioAbility.lastSourceAt = e.now()
	member.scenarioAbility.clockTransport = member.transportGeneration
	e.registry.sessions[key] = member
}

func (e gameplayPeerSession) scenarioHeroTimestamp(at time.Time) uint64 {
	if e.scenarioAbility.lastSourceAt.IsZero() || e.scenarioAbility.clockTransport != e.transportGeneration {
		return 0
	}
	elapsed := max(time.Duration(0), at.Sub(e.scenarioAbility.lastSourceAt))
	return e.scenarioAbility.lastSourceTime + uint64(elapsed/time.Millisecond)
}
