package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/sim"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
)

func (e *gameplayPeerSession) consumeExpungePlasma(
	targetID uint32, at time.Time, pool *modifierPool,
) (*expungeRemainingDamage, *expungeModifierDelete, func(), error) {
	run := e.campaignPlasmaModifiers[targetID]
	if run == nil || run.kind != zoneability.PlasmaModifierBurn || run.tick <= 0 {
		return nil, nil, nil, nil
	}
	isCreated, err := run.modifier.release(pool)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("plasmaRelease: %w", err)
	}
	isRemoved := e.removeCampaignPlasmaModifier(run)
	if !isRemoved {
		return nil, nil, nil, errors.New("plasma modifier ownership changed")
	}
	cancel := run.cancel
	run.cancel = nil
	var deleted *expungeModifierDelete
	if isCreated {
		deleted = &expungeModifierDelete{targetObjectID: targetID, instanceID: run.modifier.instanceID}
	}
	remaining := max(time.Duration(0), run.expiresAt.Sub(at))
	tickCount := uint32(remaining / run.tick)
	if remaining%run.tick != 0 {
		tickCount++
	}
	if tickCount == 0 {
		return nil, deleted, cancel, nil
	}
	// PlasmaSentinelBurn306 inherits DOT149.GetRemainingDamage and authors
	// raw tick damage {1,1}, independent of its ordinary damage projection.
	source := sim.AbilityDefinition{DamageType: 3, DamageSource: 1, IsDamageTypeFound: true, IsDamageSourceFound: true}
	damage := &expungeRemainingDamage{
		activationID: run.activationID,
		creature:     run.creature, sourceObjectID: run.sourceID,
		definition: expungeDamageDefinition(source, tickCount, 1, 1),
	}
	return damage, deleted, cancel, nil
}
