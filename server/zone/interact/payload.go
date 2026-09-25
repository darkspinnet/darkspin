package interact

import (
	"errors"
	"sync"

	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

type EquipmentPickup struct {
	ObjectID               uint32
	WinnerUserID           uint64
	WinnerRewardChoice     uint32
	WinnerRewardDifficulty uint32
	WinnerRewardRigblockID uint16
	Rolls                  []EquipmentPickupRoll
	Part                   sporenet.Part
	IsWinnerReward         bool
}

type EquipmentPickupRoll struct {
	UserID   uint64
	ObjectID uint32
	Roll     uint32
}

type CrystalPickup struct {
	ObjectID uint32
	Request  sim.CrystalPickupRequest
	Object   sim.CrystalPickupObject
}

// PickupPayloadRegistry owns the world data represented by registered pickup
// objects. Player-local inventory and in-flight collection schedules remain
// outside this registry.
type PickupPayloadRegistry struct {
	mu               sync.RWMutex
	equipmentPickups map[uint32]EquipmentPickup
	crystalPickups   map[uint32]CrystalPickup
}

func NewPickupPayloadRegistry() *PickupPayloadRegistry {
	return &PickupPayloadRegistry{
		equipmentPickups: make(map[uint32]EquipmentPickup),
		crystalPickups:   make(map[uint32]CrystalPickup),
	}
}

func (e *PickupPayloadRegistry) AddEquipment(pickup EquipmentPickup) error {
	if e == nil || pickup.ObjectID == 0 {
		return errors.New("invalid equipment pickup")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, isFound := e.equipmentPickups[pickup.ObjectID]; isFound {
		return errors.New("duplicate equipment pickup")
	}
	e.equipmentPickups[pickup.ObjectID] = cloneEquipmentPickup(pickup)
	return nil
}

func (e *PickupPayloadRegistry) Equipment(objectID uint32) (EquipmentPickup, bool) {
	if e == nil || objectID == 0 {
		return EquipmentPickup{}, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	pickup, isFound := e.equipmentPickups[objectID]
	return cloneEquipmentPickup(pickup), isFound
}

func (e *PickupPayloadRegistry) SetEquipmentRoll(
	objectID uint32, userID uint64, rolls []EquipmentPickupRoll,
) (EquipmentPickup, error) {
	if e == nil || objectID == 0 || userID == 0 || len(rolls) == 0 {
		return EquipmentPickup{}, errors.New("invalid equipment winner")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	pickup, isFound := e.equipmentPickups[objectID]
	if !isFound {
		return EquipmentPickup{}, errors.New("equipment pickup unavailable")
	}
	if pickup.WinnerUserID != 0 && pickup.WinnerUserID != userID {
		return EquipmentPickup{}, errors.New("equipment winner already selected")
	}
	pickup.WinnerUserID = userID
	pickup.Rolls = append([]EquipmentPickupRoll(nil), rolls...)
	e.equipmentPickups[objectID] = cloneEquipmentPickup(pickup)
	return cloneEquipmentPickup(pickup), nil
}

func (e *PickupPayloadRegistry) ClearEquipmentRoll(
	objectID uint32, userID uint64,
) error {
	if e == nil || objectID == 0 || userID == 0 {
		return errors.New("invalid equipment winner reset")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	pickup, isFound := e.equipmentPickups[objectID]
	if !isFound {
		return errors.New("equipment pickup unavailable")
	}
	if pickup.WinnerUserID != userID {
		return errors.New("equipment winner changed")
	}
	pickup.WinnerUserID = 0
	pickup.Rolls = nil
	e.equipmentPickups[objectID] = cloneEquipmentPickup(pickup)
	return nil
}

// SetEquipmentWinnerPart replaces a multiplayer pickup's neutral preview with
// the reward generated for its selected winner.
func (e *PickupPayloadRegistry) SetEquipmentWinnerPart(
	objectID uint32, userID uint64, part sporenet.Part,
) (EquipmentPickup, error) {
	if e == nil || objectID == 0 || userID == 0 || part.RigblockAssetID == 0 {
		return EquipmentPickup{}, errors.New("invalid equipment winner part")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	pickup, isFound := e.equipmentPickups[objectID]
	if !isFound {
		return EquipmentPickup{}, errors.New("equipment pickup unavailable")
	}
	if pickup.WinnerUserID != userID || !pickup.IsWinnerReward {
		return EquipmentPickup{}, errors.New("equipment winner unavailable")
	}
	pickup.Part = part
	e.equipmentPickups[objectID] = cloneEquipmentPickup(pickup)
	return cloneEquipmentPickup(pickup), nil
}

func cloneEquipmentPickup(pickup EquipmentPickup) EquipmentPickup {
	pickup.Rolls = append([]EquipmentPickupRoll(nil), pickup.Rolls...)
	return pickup
}

func (e *PickupPayloadRegistry) RemoveEquipment(objectID uint32) bool {
	if e == nil || objectID == 0 {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, isFound := e.equipmentPickups[objectID]; !isFound {
		return false
	}
	delete(e.equipmentPickups, objectID)
	return true
}

func (e *PickupPayloadRegistry) AddCrystal(pickup CrystalPickup) error {
	if e == nil || pickup.ObjectID == 0 || !pickup.Object.IsLive {
		return errors.New("invalid crystal pickup")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, isFound := e.crystalPickups[pickup.ObjectID]; isFound {
		return errors.New("duplicate crystal pickup")
	}
	e.crystalPickups[pickup.ObjectID] = pickup
	return nil
}

func (e *PickupPayloadRegistry) Crystal(objectID uint32) (CrystalPickup, bool) {
	if e == nil || objectID == 0 {
		return CrystalPickup{}, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	pickup, isFound := e.crystalPickups[objectID]
	return pickup, isFound
}

func (e *PickupPayloadRegistry) UpdateCrystalObject(
	objectID uint32,
	object sim.CrystalPickupObject,
) bool {
	if e == nil || objectID == 0 {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	pickup, isFound := e.crystalPickups[objectID]
	if !isFound {
		return false
	}
	pickup.Object = object
	e.crystalPickups[objectID] = pickup
	return true
}

func (e *PickupPayloadRegistry) RemoveCrystal(objectID uint32) bool {
	if e == nil || objectID == 0 {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, isFound := e.crystalPickups[objectID]; !isFound {
		return false
	}
	delete(e.crystalPickups, objectID)
	return true
}
