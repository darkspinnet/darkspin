package gameplay

import (
	"errors"
	"sort"

	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/squad"
)

// The caller holds the registry lock through sampling and drop generation.
// Unlike pickup recipients, an absent/dead active actor does not exclude that
// player's inactive squad slots from native sub_9F51B0's resource averages.
func (e *gameplayPeerSession) campaignOrbRosterLocked(registry *gameplaySessionRegistry) ([]sim.OrbResourceSample, error) {
	if e == nil || e.zone == nil || registry == nil {
		return nil, errors.New("orb party roster unavailable")
	}
	members := e.zone.Snapshot().Members
	sort.Slice(members, func(left, right int) bool {
		if members[left].Slot == members[right].Slot {
			return members[left].UserID < members[right].UserID
		}
		return members[left].Slot < members[right].Slot
	})
	roster := make([]sim.OrbResourceSample, 0, len(members)*squad.Size)
	for _, member := range members {
		if !member.IsConnected {
			continue
		}
		var candidate gameplayPeerSession
		isFound := false
		if member.UserID == e.binding.UserID && member.PeerGeneration == e.generation {
			candidate, isFound = *e, true
		} else {
			for _, peer := range registry.sessions {
				if peer.zone == e.zone && peer.binding.UserID == member.UserID && peer.generation == member.PeerGeneration {
					candidate, isFound = peer, true
					break
				}
			}
		}
		if !isFound || candidate.squad == nil || candidate.binding.GameID != e.binding.GameID ||
			candidate.binding.Team != e.binding.Team ||
			!candidate.stage.IsDungeon() || !candidate.dungeonSetup.IsCommitted() ||
			candidate.isRejoinPending || candidate.isZoneTerminal() || candidate.lastPlayerStatus.Status&0x40 != 0 {
			continue
		}
		actor, isActorFound := e.zone.Hero().Snapshot(member.UserID, member.PeerGeneration)
		state := candidate.squad.State()
		for index, character := range state.Characters {
			// Unassigned slots have no resource definition to average.
			if !character.IsAvailable || index >= len(candidate.binding.Creatures) || candidate.binding.Creatures[index].Noun == 0 {
				continue
			}
			maximumHitPoint, maximumMana := candidate.characterResourceMaximum(uint32(index))
			sample := sim.OrbResourceSample{
				HitPoint: character.HitPoints, MaximumHitPoint: maximumHitPoint,
				Mana: character.ManaPoints, MaximumMana: maximumMana,
			}
			if uint32(index) == state.CharacterIndex {
				if !isActorFound || actor.ObjectID != candidate.deployedObjectID ||
					actor.CreatureIndex != uint32(index) || actor.HitPoint <= 0 {
					continue
				}
				sample.HitPoint, sample.MaximumHitPoint = actor.HitPoint, actor.MaximumHitPoint
				sample.Mana, sample.MaximumMana = actor.ManaPoint, actor.MaximumManaPoint
				sample.IsActive = true
			}
			roster = append(roster, sample)
		}
	}
	return roster, nil
}
