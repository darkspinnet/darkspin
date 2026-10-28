//go:build scenario

package gameplay

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/scenario"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
)

func (e *gameplaySessionRegistry) resetScenarioCooldownsLocked(member gameplayPeerSession, req scenario.HeroResetRequest, recipientCount int) (scenario.HeroResetReceipt, error) {
	if req.Noun != 0 || req.IsPlacement || req.HitPoint != nil || req.ManaPoint != nil {
		return scenario.HeroResetReceipt{}, errors.New("cooldown reset does not replace, move or heal a hero")
	}
	packets := make([][]byte, 0)
	cooldownCount := 0
	for _, cooldown := range member.abilityCooldownSession().Snapshots() {
		if !cooldown.IsHeroAbility || cooldown.AbilityID == 0 {
			continue
		}
		packet, err := abilityraknet.CooldownReset(member.deployedObjectID, cooldown.AbilityID)
		if err != nil {
			return scenario.HeroResetReceipt{}, fmt.Errorf("resetAbility: %w", err)
		}
		packets = append(packets, packet)
		cooldownCount++
	}
	for index := range member.binding.Creatures {
		packet, err := raknet.MarshalApplication(raknet.LabsPlayerDeployCooldownMessage{
			PlayerSlot: uint8(member.binding.Slot), DeployedCreatureIndex: uint32(index),
		})
		if err != nil {
			return scenario.HeroResetReceipt{}, fmt.Errorf("resetDeployment: %w", err)
		}
		packets = append(packets, packet)
	}
	isChanged := member.abilityCooldownSession().ResetAll()
	if isChanged && e.logger != nil {
		e.logger.Printf("Scenario cooldown reset game=%d user=%d abilities=%d", req.GameID, member.binding.UserID, cooldownCount)
	}
	member.squad.ResetDeployCooldown()
	for key, candidate := range e.sessions {
		if candidate.binding.UserID == member.binding.UserID && candidate.generation == member.generation {
			e.sessions[key] = member
			break
		}
	}
	// Recovery/arrival locks and in-flight producers retain their lifetimes.
	e.queueScenarioHeroPacketsLocked(member, packets)
	return scenario.HeroResetReceipt{GameID: req.GameID, ObjectID: req.ObjectID,
		Noun: member.binding.Creatures[member.deployedCreatureIndex].Noun, Index: member.deployedCreatureIndex,
		HitPoint: member.deployedHitPoint(), ManaPoint: member.deployedManaPoint(), RecipientCount: recipientCount,
		ZoneGeneration: req.ZoneGeneration, IsCooldownReset: true, CooldownCount: cooldownCount, IsQueued: true}, nil
}
