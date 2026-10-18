package gameplay

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/sporenet"
	zoneloot "github.com/darkspinnet/darkspin/server/zone/loot"
	lootraknet "github.com/darkspinnet/darkspin/server/zone/loot/raknet103"
)

func (e gameplayPeerSession) validatePickupNounType(nounName string, expectedType game.NounType) error {
	if e.zone == nil {
		return errors.New("pickup noun zone unavailable")
	}
	// Resolve the noun asset instance only. Category IDs come directly from
	// noun_navigation.noun_type, never from display labels or noun filenames.
	nounType := e.zone.DirectorDefinition().NounTypeForAsset(nounName)
	if nounType == 0 {
		return fmt.Errorf("pickup noun category missing: %s", nounName)
	}
	if nounType != expectedType {
		return fmt.Errorf("pickup noun category mismatch: %s has %#x, want %#x",
			nounName, nounType, expectedType)
	}
	return nil
}

func (e gameplayPeerSession) marshalCrystalDrop(req sim.CrystalPickupRequest, objectID uint32) ([][]byte, error) {
	err := e.validatePickupNounType(req.NounName, game.NounTypeCrystal)
	if err != nil {
		return nil, fmt.Errorf("crystalNounType: %w", err)
	}
	packets, err := lootraknet.MarshalCrystalDrop(req, objectID)
	if err != nil {
		return nil, fmt.Errorf("crystalProject: %w", err)
	}
	return packets, nil
}

func (e gameplayPeerSession) marshalEquipmentDrop(plan zoneloot.EquipmentPlan, part sporenet.Part) ([][]byte, error) {
	// Equipment and DNA share Loot. Their explicit operations select the
	// payload; category validation never chooses one subtype from the other.
	err := e.validatePickupNounType(plan.NounName, game.NounTypeLoot)
	if err != nil {
		return nil, fmt.Errorf("equipmentNounType: %w", err)
	}
	packets, err := lootraknet.MarshalEquipmentDrop(plan, part)
	if err != nil {
		return nil, fmt.Errorf("equipmentProject: %w", err)
	}
	return packets, nil
}

func (e gameplayPeerSession) marshalDNADrop(plan zoneloot.DNAPlan) ([][]byte, error) {
	err := e.validatePickupNounType("DNA.Noun", game.NounTypeLoot)
	if err != nil {
		return nil, fmt.Errorf("dnaNounType: %w", err)
	}
	packets, err := lootraknet.MarshalDNADrop(plan)
	if err != nil {
		return nil, fmt.Errorf("dnaProject: %w", err)
	}
	return packets, nil
}

func (e gameplayPeerSession) validateOrbNounType(req sim.OrbPickupRequest) error {
	var nounType game.NounType
	switch req.Kind {
	case sim.HealthOrbDrop:
		nounType = game.NounTypeHealthOrb
	case sim.ManaOrbDrop:
		nounType = game.NounTypeManaOrb
	case sim.ResurrectionOrbDrop:
		nounType = game.NounTypeResurrectOrb
	default:
		return errors.New("orb pickup subtype unsupported")
	}
	err := e.validatePickupNounType(req.NounName, nounType)
	if err != nil {
		return fmt.Errorf("orbNounType: %w", err)
	}
	return nil
}

func (e gameplayPeerSession) marshalOrbDrop(req sim.OrbPickupRequest, objectID uint32) (lootraknet.OrbDrop, error) {
	err := e.validateOrbNounType(req)
	if err != nil {
		return lootraknet.OrbDrop{}, fmt.Errorf("orbCategory: %w", err)
	}
	drop, err := lootraknet.MarshalOrbDrop(req, objectID)
	if err != nil {
		return lootraknet.OrbDrop{}, fmt.Errorf("orbProject: %w", err)
	}
	return drop, nil
}
