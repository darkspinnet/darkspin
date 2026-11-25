package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
)

type passiveFieldKey struct {
	objectID uint32
	assetID  uint32
}

// Build 103 declares recipient foot effects and caster effects in its aura modifiers.
// Server-projected bonuses retain their existing owner.
func (e gameplayPendingRuntime) pollPassiveFieldsLocked(sessionKey string, member *gameplayPeerSession) error {
	positions := make(map[uint32]game.Vec3)
	if member.deployedObjectID != 0 && member.deployedHitPoint() > 0 {
		positions[member.deployedObjectID] = game.Vec3(member.playerPosition)
	}
	for _, actor := range member.zone.Companion().Snapshots() {
		if actor.UserID == member.binding.UserID && actor.PeerGeneration == member.generation && actor.HitPoint > 0 {
			positions[actor.ObjectID] = actor.Position
		}
	}
	targets := make(map[passiveFieldKey]bool)
	critEffectID := util.HashID("crit_aura_effect.ServerEventDef")
	overdriveEffectID := util.HashID("shadow_mana_healing_aura_effect.ServerEventDef")
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
			if position.Sub(game.Vec3(source.playerPosition)).Length() > creature.PassiveAuraRadius {
				continue
			}
			assetID := uint32(0)
			switch creature.PassiveAbility {
			case util.HashID("TimeRavagerPassiveModifier"):
				assetID = util.HashID("spacetime_haste_target_effect.ServerEventDef")
			case util.HashID("LightningTempest_Passive"):
				assetID = critEffectID
				if source.isOverdriveActiveAt(e.now()) {
					assetID = overdriveEffectID
				}
			case util.HashID("CrushingDread"):
				// Build-103 chunk 944 adds the aura to its caster; nearby
				// allies do not receive this enemy-facing debuff presentation.
				if objectID == source.deployedObjectID {
					assetID = util.HashID("shadow_dread_aura_rings_effect.ServerEventDef")
				}
			}
			if assetID != 0 {
				targets[passiveFieldKey{objectID: objectID, assetID: assetID}] = true
			}
		}
		if targets[passiveFieldKey{objectID: objectID, assetID: overdriveEffectID}] {
			delete(targets, passiveFieldKey{objectID: objectID, assetID: critEffectID})
		}
	}
	for key, effects := range member.passiveFieldEffects {
		if !targets[key] {
			effects.Retire()
			delete(member.passiveFieldEffects, key)
		}
	}
	for key := range targets {
		if member.passiveFieldEffects[key] != nil {
			continue
		}
		effects, err := e.registry.allocateChannelEffectsLocked(member.zone, e.damage.effectPool, []uint32{key.objectID})
		if err != nil {
			return fmt.Errorf("fieldAllocate: %w", err)
		}
		attachment := effects.Slots()[0]
		packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
			Slot: attachment.slot + 1, ObjectID: key.objectID, IsForceAttached: true,
			Asset: key.assetID,
		})
		if err != nil {
			effects.Retire()
			return fmt.Errorf("fieldMarshal: %w", err)
		}
		if member.passiveFieldEffects == nil {
			member.passiveFieldEffects = make(map[passiveFieldKey]*channelEffects)
		}
		member.passiveFieldEffects[key] = effects
		e.registry.sessions[sessionKey] = *member
		err = e.registry.publishChannelEffectsLocked(effects, [][]byte{packet})
		*member = e.registry.sessions[sessionKey]
		if err != nil {
			effects.Retire()
			delete(member.passiveFieldEffects, key)
			return fmt.Errorf("fieldPublish: %w", err)
		}
	}
	return nil
}

func (e *gameplayPeerSession) retirePassiveFields() {
	if e == nil {
		return
	}
	for key, effects := range e.passiveFieldEffects {
		effects.Retire()
		delete(e.passiveFieldEffects, key)
	}
}
