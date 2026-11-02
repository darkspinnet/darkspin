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
	blitz := binding.Creatures[0]
	if blitz.Noun == 0 {
		blitz.Noun = util.HashID("PC_EL_Rogue.Noun")
	}
	sage := binding.Creatures[1]
	if sage.Noun == 0 {
		sage.Noun = util.HashID("PC_LF_Mage.Noun")
	}
	statusPacket, err := raknet.MarshalApplication(raknet.LabsPlayerStatusMessage{
		OnlineID: binding.UserID, AvatarLevel: binding.AvatarLevel, AvatarXP: binding.AvatarXP,
		ChainProgression: binding.ChainProgression, DNA: binding.DNA,
		Slot: uint8(binding.Slot), Status: 8, Progress: 1, IsInitial: true,
		ControlledObjectID: playerObjectID,
		HeroNoun:           blitz.Noun, HeroAsset: zonehero.AppearanceAsset(blitz),
		HeroVersion: int32(max(uint32(1), blitz.AppearanceVersion)), HeroType: 2,
		SecondHeroNoun: sage.Noun, SecondHeroAsset: zonehero.AppearanceAsset(sage),
		SecondHeroVersion: int32(max(uint32(1), sage.AppearanceVersion)), SecondHeroType: 2,
		AbilityCount: 3, LockedDeckMinimum: 2,
		CharacterResources: [3]raknet.LabsCharacterResource{
			tutorialCreatureResources(blitz), tutorialCreatureResources(sage),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("sageUnlockStatus: %w", err)
	}
	characterPacket, err := marshalSageCharacter(
		sageObjectID, position, uint8(binding.Slot), sage,
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
	packets, err := marshalSageCharacter(objectID, position, playerIndex, game.GameplayCreature{
		Noun: util.HashID("PC_LF_Mage.Noun"), HitPoint: 200, PowerPoint: 200,
	})
	if err != nil {
		return nil, fmt.Errorf("sageCharacter: %w", err)
	}
	return packets, nil
}

func marshalSageCharacter(
	objectID uint32, position raknet.Vector3, playerIndex uint8, creature game.GameplayCreature,
) ([][]byte, error) {
	createPacket, err := raknet.MarshalApplication(raknet.ObjectCreateMessage{
		ObjectID: objectID, Noun: creature.Noun,
		AssetID:   zonehero.AppearanceAsset(creature),
		PositionX: position.X, PositionY: position.Y, PositionZ: position.Z,
		Scale: 1, Team: 1, PlayerIndex: playerIndex,
		IsCollisionEnabled: true, IsPlayerControlled: true,
	})
	if err != nil {
		return nil, fmt.Errorf("sageCreate: %w", err)
	}
	packet := [][]byte{createPacket}
	resources := tutorialCreatureResources(creature)
	messages := raknet.HeroResourceStateMessages(
		objectID, resources.Health, resources.Mana, resources.MaxHealth, resources.MaxMana,
	)
	for _, message := range messages {
		statePacket, marshalErr := raknet.MarshalApplication(message)
		if marshalErr != nil {
			return nil, fmt.Errorf("sageState: %w", marshalErr)
		}
		packet = append(packet, statePacket)
	}
	return packet, nil
}

func tutorialCreatureResources(creature game.GameplayCreature) raknet.LabsCharacterResource {
	health := creature.HitPoint
	maximumHealth := creature.MaximumHitPoint
	if maximumHealth <= 0 {
		maximumHealth = health
		if maximumHealth <= 0 {
			maximumHealth = 200
			health = maximumHealth
		}
	}
	power := creature.PowerPoint
	maximumPower := creature.MaximumPowerPoint
	if maximumPower <= 0 {
		maximumPower = power
		if maximumPower <= 0 {
			maximumPower = 200
			power = maximumPower
		}
	}
	return raknet.LabsCharacterResource{
		Health: health, MaxHealth: maximumHealth, Mana: power, MaxMana: maximumPower,
		GearScore: creature.GearScore, FlattenedGearScore: creature.FlattenedGearScore,
		PartAttribute: creature.PartAttribute,
	}
}
