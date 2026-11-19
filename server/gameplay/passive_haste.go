package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
)

// Build 103 modifier 652 places this effect at each haste recipient's feet.
// Server-projected movement and attack bonuses retain their existing owner.
func (e gameplayPendingRuntime) pollHasteFieldLocked(sessionKey string, member *gameplayPeerSession) error {
	positions := make(map[uint32]game.Vec3)
	if member.deployedObjectID != 0 && member.deployedHitPoint() > 0 {
		positions[member.deployedObjectID] = game.Vec3(member.playerPosition)
	}
	for _, actor := range member.zone.Companion().Snapshots() {
		if actor.UserID == member.binding.UserID && actor.PeerGeneration == member.generation && actor.HitPoint > 0 {
			positions[actor.ObjectID] = actor.Position
		}
	}
	targets := make(map[uint32]bool)
	for objectID, position := range positions {
		for _, source := range e.registry.sessions {
			if source.binding.UserID == member.binding.UserID && source.generation == member.generation {
				source = *member
			}
			if !isPassiveAuraRecipient(source, *member) || source.isZoneTerminal() || source.deployedHitPoint() <= 0 ||
				source.deployedCreatureIndex >= uint32(len(source.binding.Creatures)) {
				continue
			}
			creature := source.projectPassiveCreature(source.deployedCreatureIndex, e.now())
			if creature.PassiveAbility == util.HashID("TimeRavagerPassiveModifier") &&
				position.Sub(game.Vec3(source.playerPosition)).Length() <= creature.PassiveAuraRadius {
				targets[objectID] = true
				break
			}
		}
	}
	for objectID, effects := range member.hasteFieldEffects {
		if !targets[objectID] {
			effects.Retire()
			delete(member.hasteFieldEffects, objectID)
		}
	}
	for objectID := range targets {
		if member.hasteFieldEffects[objectID] != nil {
			continue
		}
		effects, err := e.registry.allocateChannelEffectsLocked(member.zone, e.damage.effectPool, []uint32{objectID})
		if err != nil {
			return fmt.Errorf("hasteAllocate: %w", err)
		}
		attachment := effects.Slots()[0]
		packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
			Slot: attachment.slot + 1, ObjectID: objectID, IsForceAttached: true,
			Asset: util.HashID("spacetime_haste_target_effect.ServerEventDef"),
		})
		if err != nil {
			effects.Retire()
			return fmt.Errorf("hasteMarshal: %w", err)
		}
		if member.hasteFieldEffects == nil {
			member.hasteFieldEffects = make(map[uint32]*channelEffects)
		}
		member.hasteFieldEffects[objectID] = effects
		e.registry.sessions[sessionKey] = *member
		err = e.registry.publishChannelEffectsLocked(effects, [][]byte{packet})
		*member = e.registry.sessions[sessionKey]
		if err != nil {
			effects.Retire()
			delete(member.hasteFieldEffects, objectID)
			return fmt.Errorf("hastePublish: %w", err)
		}
	}
	return nil
}

func (e *gameplayPeerSession) retireHasteFields() {
	if e == nil {
		return
	}
	for objectID, effects := range e.hasteFieldEffects {
		effects.Retire()
		delete(e.hasteFieldEffects, objectID)
	}
}
