package gameplay

import (
	"fmt"
	"time"

	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonecontent "github.com/darkspinnet/darkspin/server/zone/content"
)

// marshalGameplayRejoinCooldowns restores only the owner's current action bar.
// Authored IDs remain shared across roster slots; internal admission keys and
// unidentified projectile-basic cooldowns have no client ability identity.
func marshalGameplayRejoinCooldowns(
	peerSession gameplayPeerSession, program Programs,
	now time.Time, sourceTime uint64,
) ([][]byte, error) {
	if peerSession.abilityCooldown == nil || peerSession.deployedObjectID == 0 ||
		peerSession.deployedCreatureIndex >= uint32(len(peerSession.binding.Creatures)) {
		return nil, nil
	}
	abilityIDs := make(map[uint32]struct{})
	deployedCreature := peerSession.binding.Creatures[peerSession.deployedCreatureIndex]
	activeSlots := []zonecontent.HeroAbilitySlot{
		zonecontent.HeroAbilitySpecialTwo, zonecontent.HeroAbilityRandom,
	}
	for _, slot := range activeSlots {
		ability, isFound := program.HeroAbility(deployedCreature.Noun, slot)
		if !isFound || ability.ID == 0 {
			continue
		}
		abilityIDs[ability.ID] = struct{}{}
	}
	for _, creature := range peerSession.binding.Creatures {
		ability, isFound := program.HeroAbility(
			creature.Noun, zonecontent.HeroAbilitySpecialOne,
		)
		if !isFound || ability.ID == 0 {
			continue
		}
		abilityIDs[ability.ID] = struct{}{}
	}
	snapshots := peerSession.abilityCooldown.Snapshots()
	packets := make([][]byte, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if !snapshot.IsHeroAbility || !snapshot.End.After(now) {
			continue
		}
		_, isMapped := abilityIDs[snapshot.AbilityID]
		if !isMapped {
			continue
		}
		remaining := snapshot.End.Sub(now)
		if remaining.Milliseconds() <= 0 {
			continue
		}
		packet, err := abilityraknet.Cooldown(abilityraknet.CooldownRequest{
			ObjectID: peerSession.deployedObjectID, AbilityID: snapshot.AbilityID,
			Duration: remaining, StartTime: sourceTime,
		})
		if err != nil {
			return nil, fmt.Errorf("rejoinCooldown[%d]: %w", snapshot.AbilityID, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}
