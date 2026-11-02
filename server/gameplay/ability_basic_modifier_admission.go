package gameplay

import (
	"fmt"

	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type basicHitModifierAdmission struct {
	runtime            campaignDamageRuntime
	npc                *zonenpc.Session
	effect             *zoneeffect.Inventory
	targetID           uint32
	state              zonenpc.BasicModifier
	previousState      zonenpc.BasicModifier
	previousRecord     zoneeffect.Modifier
	modifier           *campaignNPCModifierRun
	silence            *campaignNPCModifierRun
	isAllocated        bool
	isInventoryChanged bool
	isCommitted        bool
}

func (e *basicHitModifierAdmission) rollbackUnlessCommitted() {
	if e.isCommitted {
		return
	}
	err := e.rollback()
	if err != nil {
		e.runtime.logger.Printf("Basic modifier rollback target=%d: %v", e.targetID, err)
	}
}

func (e *basicHitModifierAdmission) rollback() error {
	e.npc.RestoreBasicModifier(e.targetID, e.state, e.previousState)
	if e.isInventoryChanged {
		if e.isAllocated {
			e.effect.Remove(e.modifier.instanceID)
		} else {
			err := e.effect.Update(e.previousRecord)
			if err != nil {
				return fmt.Errorf("basicRestoreInventory: %w", err)
			}
			e.modifier.record = e.previousRecord
		}
	}
	modifiers := make([]*campaignNPCModifierRun, 0, 2)
	if e.isAllocated && e.modifier != nil {
		modifiers = append(modifiers, e.modifier)
	}
	if e.silence != nil {
		modifiers = append(modifiers, e.silence)
	}
	for _, modifier := range modifiers {
		isCreated, err := modifier.release(e.runtime.npc.modifierPool)
		if err != nil {
			return fmt.Errorf("basicAdmissionRelease: %w", err)
		}
		if isCreated { /* Packets have not been published; no delete is needed. */
		}
	}
	return nil
}
