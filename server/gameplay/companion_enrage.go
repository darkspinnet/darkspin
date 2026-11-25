package gameplay

import (
	"errors"
	"fmt"

	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
)

func (e campaignEnrageState) ApplyEnrageModifier(creatureIndex, targetObjectID uint32, isCompanion bool,
	instanceID, previousID uint32, damageBonus, hitPoint float32,
) error {
	if !isCompanion {
		err := e.ApplyEnrage(creatureIndex, targetObjectID, false, damageBonus, hitPoint)
		if err != nil {
			return fmt.Errorf("heroEnrage: %w", err)
		}
		return nil
	}
	if e.session == nil || e.session.zone == nil || e.session.zone.Companion() == nil {
		return errors.New("companion Enrage unavailable")
	}
	companion := e.session.zone.Companion()
	err := companion.ReplaceBuffModifierWithHitPoint(targetObjectID, instanceID, previousID,
		zonecompanion.Buff{DamageBuff: damageBonus}, hitPoint)
	if err != nil {
		return fmt.Errorf("companionEnrage: %w", err)
	}
	return nil
}

func (e campaignEnrageState) RemoveEnrageModifier(creatureIndex, targetObjectID uint32, isCompanion bool,
	instanceID uint32, damageBonus float32,
) error {
	if !isCompanion {
		err := e.RemoveEnrage(creatureIndex, targetObjectID, false, damageBonus)
		if err != nil {
			return fmt.Errorf("heroEnrageRemove: %w", err)
		}
		return nil
	}
	if e.session != nil && e.session.zone != nil && e.session.zone.Companion() != nil {
		e.session.zone.Companion().ClearBuffModifier(targetObjectID, instanceID)
	}
	return nil
}
