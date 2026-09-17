package raknet103

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zonehero "github.com/darkspinnet/darkspin/server/zone/hero"
)

func MarshalSageUnlock(
	binding game.GameplayBinding, position raknet.Vector3,
) ([][]byte, error) {
	playerObjectID := zonehero.ObjectID(binding.Slot, 0)
	sageObjectID := zonehero.ObjectID(binding.Slot, 1)
	statusPacket, err := raknet.MarshalApplication(raknet.LabsPlayerStatusMessage{
		OnlineID: binding.UserID, AvatarLevel: binding.AvatarLevel, AvatarXP: binding.AvatarXP,
		ChainProgression: binding.ChainProgression, DNA: binding.DNA,
		Slot: uint8(binding.Slot), Status: 8, Progress: 1, IsInitial: true,
		ControlledObjectID: playerObjectID,
		HeroNoun:           util.HashID("PC_EL_Rogue.Noun"), HeroAsset: 0xa93aff21,
		HeroVersion: 1, HeroType: 2, SecondHeroNoun: util.HashID("PC_LF_Mage.Noun"),
		SecondHeroAsset: 0x55d1408f, SecondHeroVersion: 1, SecondHeroType: 2,
		AbilityCount: 3, LockedDeckMinimum: 2,
	})
	if err != nil {
		return nil, fmt.Errorf("sageUnlockStatus: %w", err)
	}
	characterPacket, err := MarshalSageCharacter(
		sageObjectID, position, uint8(binding.Slot),
	)
	if err != nil {
		return nil, fmt.Errorf("sageUnlockCharacter: %w", err)
	}
	hiddenPacket, err := raknet.MarshalApplication(raknet.ObjectUpdateMessage{
		ObjectID: sageObjectID, PositionX: position.X, PositionY: position.Y,
		PositionZ: position.Z, IsVisible: false,
	})
	if err != nil {
		return nil, fmt.Errorf("sageUnlockHidden: %w", err)
	}
	deployPacket, err := raknet.MarshalApplication(raknet.PlayerCharacterDeployMessage{
		PlayerIndex: uint8(binding.Slot), CreatureIndex: 0,
		ObjectID: playerObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("sageUnlockDeploy: %w", err)
	}
	packet := [][]byte{statusPacket}
	packet = append(packet, characterPacket...)
	packet = append(packet, hiddenPacket, deployPacket)
	return packet, nil
}

func MarshalSageCharacter(
	objectID uint32, position raknet.Vector3, playerIndex uint8,
) ([][]byte, error) {
	createPacket, err := raknet.MarshalApplication(raknet.ObjectCreateMessage{
		ObjectID: objectID, Noun: util.HashID("PC_LF_Mage.Noun"),
		AssetID:   0x55d1408f,
		PositionX: position.X, PositionY: position.Y, PositionZ: position.Z,
		Scale: 1, Team: 1, PlayerIndex: playerIndex,
		IsCollisionEnabled: true, IsPlayerControlled: true,
	})
	if err != nil {
		return nil, fmt.Errorf("sageCreate: %w", err)
	}
	packet := [][]byte{createPacket}
	for _, message := range raknet.TutorialHeroStateMessages(objectID) {
		statePacket, marshalErr := raknet.MarshalApplication(message)
		if marshalErr != nil {
			return nil, fmt.Errorf("sageState: %w", marshalErr)
		}
		packet = append(packet, statePacket)
	}
	return packet, nil
}
