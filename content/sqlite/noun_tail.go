package sqlite

import (
	"encoding/binary"
	"fmt"
)

type NounLocomotionTuning struct {
	Acceleration float32
	Deceleration float32
	TurnRate     float32
}

// NounCrystalDefinition identifies the modifier and HUD metadata authored on a
// catalyst noun. It is independent of CrystalTuning's weighted drop entries.
type NounCrystalDefinition struct {
	ModifierHash uint32
	ModifierName *string
	Color        uint32
	Rarity       uint32
}

// decodeNounTail projects the prefix through shared combatant definitions. Later noun
// fields remain outside this projection; all preceding descendants share the
// same bounded cursor, following sub_F689D0's reflection order.
func decodeNounTail(payload []byte, noun *NounNavigation) error {
	cursor := assetCursor{payload: payload}
	base, err := cursor.reserve(1, 480)
	if err != nil {
		return fmt.Errorf("nounFixedHeader: %w", err)
	}
	_ = base
	noun.IsDoor = binary.LittleEndian.Uint32(payload[136:]) != 0
	noun.IsSwitch = binary.LittleEndian.Uint32(payload[140:]) != 0
	noun.IsPressureSwitch = binary.LittleEndian.Uint32(payload[144:]) != 0
	err = cursor.references(36, 16, 52, 96, 112, 128)
	if err != nil {
		return fmt.Errorf("nounGraphicsReferences: %w", err)
	}
	gfxBase, err := cursor.optionalBody(132, 8)
	if err != nil {
		return fmt.Errorf("nounGraphicsStates: %w", err)
	}
	if gfxBase >= 0 {
		count := binary.LittleEndian.Uint32(payload[gfxBase+4:])
		start, arrayErr := cursor.reserve(count, 56)
		if arrayErr != nil {
			return fmt.Errorf("graphicsStateArray: %w", arrayErr)
		}
		for index := range int(count) {
			stateBase := start + index*56
			err = cursor.references(stateBase+12, stateBase+28, stateBase+48, stateBase+44)
			if err != nil {
				return fmt.Errorf("graphicsStateReferences[%d]: %w", index, err)
			}
		}
	}
	for index, size := range []int{24, 12, 40} {
		stateBase, bodyErr := cursor.optionalBody(136+index*4, size)
		if bodyErr != nil {
			return fmt.Errorf("interactionBody[%d]: %w", index, bodyErr)
		}
		if stateBase < 0 {
			continue
		}
		stateCount := 3
		if index == 0 {
			stateCount = 4
			isClickToOpen := payload[stateBase+16] == 1
			isClickToClose := payload[stateBase+17] == 1
			initialState := binary.LittleEndian.Uint32(payload[stateBase+20:])
			noun.IsClickToOpen, noun.IsClickToClose = &isClickToOpen, &isClickToClose
			noun.DoorInitialState = &initialState
		}
		for stateIndex := range stateCount {
			err = walkNewGraphicsState(&cursor, stateBase+stateIndex*4)
			if err != nil {
				return fmt.Errorf("interactionGraphics[%d:%d]: %w", index, stateIndex, err)
			}
		}
	}
	crystalBase, err := cursor.optionalBody(148, 24)
	if err != nil {
		return fmt.Errorf("nounCrystal: %w", err)
	}
	if crystalBase >= 0 {
		modifierName, referenceErr := cursor.reference(crystalBase + 12)
		err = referenceErr
		if err != nil {
			return fmt.Errorf("crystalModifier: %w", err)
		}
		noun.CrystalDefinition = &NounCrystalDefinition{
			ModifierHash: binary.LittleEndian.Uint32(payload[crystalBase:]),
			ModifierName: modifierName,
			Color:        binary.LittleEndian.Uint32(payload[crystalBase+16:]),
			Rarity:       binary.LittleEndian.Uint32(payload[crystalBase+20:]),
		}
	}
	err = cursor.references(160, 164, 168)
	if err != nil {
		return fmt.Errorf("nounClassReferences: %w", err)
	}
	thumbnailBase, err := cursor.optionalBody(172, 108)
	if err != nil {
		return fmt.Errorf("nounThumbnail: %w", err)
	}
	_ = thumbnailBase // All thumbnail fields are fixed inline scalars/vectors.
	eliteStart, err := cursor.reserve(binary.LittleEndian.Uint32(payload[180:]), 8)
	if err != nil {
		return fmt.Errorf("nounEliteAssets: %w", err)
	}
	_ = eliteStart // uint64 elements have no reference tails.
	err = cursor.references(204, 212, 232, 236, 240, 244, 248)
	if err != nil {
		return fmt.Errorf("nounBehaviorReferences: %w", err)
	}
	trigger, err := decodeOptionalTriggerVolume(&cursor, 292)
	if err != nil {
		return fmt.Errorf("nounTrigger: %w", err)
	}
	noun.TriggerVolume = trigger
	projectileBase, err := cursor.optionalBody(296, 12)
	if err != nil {
		return fmt.Errorf("nounProjectile: %w", err)
	}
	isProjectilePresent := projectileBase >= 0
	noun.IsProjectilePresent = &isProjectilePresent
	if projectileBase >= 0 {
		for index := range 2 {
			volumeBase, volumeErr := cursor.optionalBody(projectileBase+index*4, 20)
			if volumeErr != nil {
				return fmt.Errorf("projectileVolume[%d]: %w", index, volumeErr)
			}
			_ = volumeBase // CollisionVolumeDef contains only fixed scalars.
		}
	}
	orbitBase, err := cursor.optionalBody(300, 12)
	if err != nil {
		return fmt.Errorf("nounOrbit: %w", err)
	}
	_ = orbitBase
	tuningBase, err := cursor.optionalBody(304, 12)
	if err != nil {
		return fmt.Errorf("nounLocomotion: %w", err)
	}
	if tuningBase >= 0 {
		noun.LocomotionTuning = &NounLocomotionTuning{Acceleration: readFloat32(payload, tuningBase),
			Deceleration: readFloat32(payload, tuningBase+4), TurnRate: readFloat32(payload, tuningBase+8)}
		if !isFinite(noun.LocomotionTuning.Acceleration) || !isFinite(noun.LocomotionTuning.Deceleration) ||
			!isFinite(noun.LocomotionTuning.TurnRate) {
			return fmt.Errorf("nounLocomotionNumbers: nonfinite")
		}
	}
	err = cursor.references(308)
	if err != nil {
		return fmt.Errorf("nounGravity: %w", err)
	}
	definition, err := decodeSharedComponents(&cursor, 252)
	if err != nil {
		return fmt.Errorf("nounSharedComponents: %w", err)
	}
	noun.EventListener = definition.listener
	noun.Interactable = definition.interactable
	noun.Combatant = definition.combatant
	return nil
}

func walkNewGraphicsState(cursor *assetCursor, fieldOffset int) error {
	base, err := cursor.optionalBody(fieldOffset, 40)
	if err != nil {
		return fmt.Errorf("graphicsBody: %w", err)
	}
	if base < 0 {
		return nil
	}
	err = cursor.references(base, base+16, base+32)
	if err != nil {
		return fmt.Errorf("graphicsReferences: %w", err)
	}
	return nil
}
