package raknet103

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
)

type StatusRequest struct {
	OnlineID            uint64
	AvatarLevel         uint32
	AvatarXP            float32
	ChainProgression    uint32
	DNA                 uint32
	PlayerIndex         uint8
	Status              uint32
	Progress            float32
	IsInitial           bool
	ControlledObjectID  uint32
	HeroNounID          uint32
	HeroAssetID         uint64
	HeroVersion         int32
	HeroType            uint32
	SecondHeroNounID    uint32
	SecondHeroAssetID   uint64
	SecondHeroVersion   int32
	SecondHeroType      uint32
	ThirdHeroNounID     uint32
	ThirdHeroAssetID    uint64
	ThirdHeroVersion    int32
	ThirdHeroType       uint32
	AbilityCount        uint32
	LockedDeckMinimum   uint32
	EnergyPoint         float32
	IsOverdriveUnlocked bool
}

func Status(req StatusRequest) ([]byte, error) {
	packet, err := raknet.MarshalApplication(raknet.LabsPlayerStatusMessage{
		OnlineID: req.OnlineID, AvatarLevel: req.AvatarLevel,
		AvatarXP: req.AvatarXP, ChainProgression: req.ChainProgression,
		DNA: req.DNA, Slot: req.PlayerIndex, Status: req.Status,
		Progress: req.Progress, IsInitial: req.IsInitial,
		ControlledObjectID: req.ControlledObjectID,
		HeroNoun:           req.HeroNounID, HeroAsset: req.HeroAssetID,
		HeroVersion: req.HeroVersion, HeroType: req.HeroType,
		SecondHeroNoun:    req.SecondHeroNounID,
		SecondHeroAsset:   req.SecondHeroAssetID,
		SecondHeroVersion: req.SecondHeroVersion,
		SecondHeroType:    req.SecondHeroType,
		ThirdHeroNoun:     req.ThirdHeroNounID,
		ThirdHeroAsset:    req.ThirdHeroAssetID,
		ThirdHeroVersion:  req.ThirdHeroVersion,
		ThirdHeroType:     req.ThirdHeroType,
		AbilityCount:      req.AbilityCount,
		LockedDeckMinimum: req.LockedDeckMinimum,
		EnergyPoint:       req.EnergyPoint, IsOverdriveUnlocked: req.IsOverdriveUnlocked,
	})
	if err != nil {
		return nil, fmt.Errorf("statusMarshal: %w", err)
	}
	return packet, nil
}

func GameStart(levelIndex uint32) ([]byte, error) {
	packet, err := raknet.MarshalApplication(
		raknet.GameStartMessage{LevelIndex: levelIndex},
	)
	if err != nil {
		return nil, fmt.Errorf("gameStartMarshal: %w", err)
	}
	return packet, nil
}
