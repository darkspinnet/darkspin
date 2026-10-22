package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/sim"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func (e *gameplaySessionRegistry) consumeCloudPoisonLocked(npc *zonenpc.Session, targetID uint32, pool *modifierPool) (*expungeRemainingDamage, *expungeModifierDelete, error) {
	world := e.cloudPoisons[npc]
	if world == nil || world.targets[targetID] == nil {
		return nil, nil, nil
	}
	target := world.targets[targetID]
	origin := target.region.schedule
	instanceID := target.instanceID
	packets, err := e.retireCloudPoisonLocked(npc, targetID, pool)
	if err != nil {
		return nil, nil, fmt.Errorf("cloudConsume: %w", err)
	}
	if len(packets) == 0 {
		return nil, nil, nil
	}
	// SproutPoison replaces Tick but inherits DOT149.GetRemainingDamage and
	// its default tickDamage={{0,0}}. Expunge still applies its own coefficient.
	source := sim.AbilityDefinition{DamageType: 2, DamageSource: 1, IsDamageTypeFound: true, IsDamageSourceFound: true}
	damage := &expungeRemainingDamage{
		activationID: origin.runtime.damage.activationID,
		creature:     target.creature, sourceObjectID: origin.sourceObjectID,
		definition: expungeDamageDefinition(source, 1, 0, 0),
		sessionKey: origin.sessionKey, generation: origin.generation, binding: origin.binding,
	}
	return damage, &expungeModifierDelete{targetObjectID: targetID, instanceID: instanceID}, nil
}

func (e *gameplaySessionRegistry) hasCloudPoisonLocked(npc *zonenpc.Session, targetID uint32) bool {
	world := e.cloudPoisons[npc]
	return world != nil && world.targets[targetID] != nil
}
