package gameplay

import (
	"fmt"
	"sort"
	"time"

	"github.com/darkspinnet/darkspin/server/sim"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func (e *gameplaySessionRegistry) consumeBasicBurnsLocked(peerSession gameplayPeerSession, targetID uint32, pool *modifierPool) ([]expungeRemainingDamage, []expungeModifierDelete, error) {
	npc := peerSession.zone.NPCs()
	runs := e.basicHitModifiers[npc]
	keys := make([]basicHitModifierKey, 0)
	for key := range runs {
		if key.targetID == targetID && key.modifier.Kind == zonenpc.BasicModifierBurn {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].modifier.SourceObjectID < keys[j].modifier.SourceObjectID })
	damages := make([]expungeRemainingDamage, 0, len(keys))
	deletes := make([]expungeModifierDelete, 0, len(keys))
	for _, key := range keys {
		run := runs[key]
		remaining := time.Until(run.state.ExpiresAt)
		if remaining > 0 && npc.BasicModifier(targetID, key.modifier) == run.state {
			// DOT template149.GetRemainingDamage returns raw tickDamage *
			// ceil(remainingDuration/tickDuration), not projected damage.
			tickCount := uint32((remaining + 500*time.Millisecond - 1) / (500 * time.Millisecond))
			sessionKey, owner := e.basicModifierOwnerLocked(npc, run.binding.UserID)
			definition := sim.AbilityDefinition{DamageType: 3, DamageSource: 1, IsDamageTypeFound: true, IsDamageSourceFound: true}
			damages = append(damages, expungeRemainingDamage{creature: run.sourceCreature, definition: expungeDamageDefinition(definition, tickCount, 1, 1), sourceObjectID: run.sourceID, sessionKey: sessionKey, generation: owner.generation, binding: run.binding})
		}
		instanceID := run.modifier.instanceID
		packets, err := e.retireBasicHitModifierLocked(npc, peerSession.zone.Effect(), key, pool)
		if err != nil {
			return nil, nil, fmt.Errorf("basicBurnConsume: %w", err)
		}
		if len(packets) > 0 {
			deletes = append(deletes, expungeModifierDelete{targetObjectID: targetID, instanceID: instanceID})
		}
	}
	return damages, deletes, nil
}
