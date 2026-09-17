package raknet103

import (
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
)

type ResourceRequest struct {
	PlayerIndex      uint8
	CreatureIndex    uint32
	HitPoint         float32
	MaximumHitPoint  float32
	ManaPoint        float32
	MaximumManaPoint float32
}

func Resource(req ResourceRequest) ([]byte, error) {
	if !isFiniteResource(req.HitPoint, req.MaximumHitPoint) ||
		!isFiniteResource(req.ManaPoint, req.MaximumManaPoint) {
		return nil, errors.New("hero resource invalid")
	}
	packet, err := raknet.MarshalApplication(raknet.LabsPlayerCharacterResourceMessage{
		PlayerSlot: req.PlayerIndex, CreatureIndex: req.CreatureIndex,
		Resource: raknet.LabsCharacterResource{
			Health: req.HitPoint, MaxHealth: req.MaximumHitPoint,
			Mana: req.ManaPoint, MaxMana: req.MaximumManaPoint,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("resourceMarshal: %w", err)
	}
	return packet, nil
}

func HitPoint(objectID uint32, hitPoint float32) ([]byte, error) {
	if objectID == 0 || !isFiniteCurrentResource(hitPoint) {
		return nil, errors.New("hero hit point invalid")
	}
	packet, err := raknet.MarshalApplication(raknet.CombatantDataDeltaMessage{
		ObjectID: objectID, HitPoints: hitPoint, IsHitPointChanged: true,
	})
	if err != nil {
		return nil, fmt.Errorf("hitPointMarshal: %w", err)
	}
	return packet, nil
}

func ManaPoint(objectID uint32, manaPoint float32) ([]byte, error) {
	if objectID == 0 || !isFiniteCurrentResource(manaPoint) {
		return nil, errors.New("hero mana point invalid")
	}
	packet, err := raknet.MarshalApplication(raknet.CombatantDataDeltaMessage{
		ObjectID: objectID, ManaPoints: manaPoint, IsManaPointChanged: true,
	})
	if err != nil {
		return nil, fmt.Errorf("manaPointMarshal: %w", err)
	}
	return packet, nil
}

func Visibility(objectID uint32, position game.Vec3, isVisible bool) ([]byte, error) {
	if objectID == 0 || !isFinitePosition(position) {
		return nil, errors.New("hero visibility invalid")
	}
	packet, err := raknet.MarshalApplication(raknet.ObjectUpdateMessage{
		ObjectID: objectID, PositionX: position.X, PositionY: position.Y,
		PositionZ: position.Z, IsVisible: isVisible,
	})
	if err != nil {
		return nil, fmt.Errorf("visibilityMarshal: %w", err)
	}
	return packet, nil
}

func DeployCooldown(
	playerIndex uint8, creatureIndex uint32, deadlineMilliseconds uint64,
) ([]byte, error) {
	packet, err := raknet.MarshalApplication(raknet.LabsPlayerDeployCooldownMessage{
		PlayerSlot: playerIndex, DeployedCreatureIndex: creatureIndex,
		DeadlineMilliseconds: deadlineMilliseconds,
	})
	if err != nil {
		return nil, fmt.Errorf("deployCooldownMarshal: %w", err)
	}
	return packet, nil
}

func isFiniteResource(current float32, maximum float32) bool {
	return !math.IsNaN(float64(current)) && !math.IsInf(float64(current), 0) &&
		!math.IsNaN(float64(maximum)) && !math.IsInf(float64(maximum), 0) &&
		current >= 0 && maximum > 0 && current <= maximum
}

func isFiniteCurrentResource(current float32) bool {
	return !math.IsNaN(float64(current)) && !math.IsInf(float64(current), 0) &&
		current >= 0
}
