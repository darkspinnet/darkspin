package gameplay

import (
	"errors"
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	memberraknet "github.com/darkspinnet/darkspin/server/zone/member/raknet103"
)

func marshalZoneSetup(binding game.GameplayBinding) ([]byte, error) {
	message, err := zonePrepareMessage(binding)
	if err != nil {
		return nil, fmt.Errorf("setupMessage: %w", err)
	}
	packet, err := raknet.MarshalApplication(message)
	if err != nil {
		return nil, fmt.Errorf("setupMarshal: %w", err)
	}
	return packet, nil
}

func zonePrepareMessage(binding game.GameplayBinding) (raknet.PrepareForStartMessage, error) {
	if binding.Level == "" || binding.RunSeed == 0 {
		return raknet.PrepareForStartMessage{}, errors.New("zone setup level or run seed unavailable")
	}
	levelIndex, err := zoneLevelIndex(binding)
	if err != nil {
		return raknet.PrepareForStartMessage{}, fmt.Errorf("setupStage: %w", err)
	}
	levelAsset := strings.TrimSuffix(strings.ToLower(binding.Level), "_v2")
	mapSeed, prepareMask, err := prepareMapInputs(binding, levelAsset, levelIndex)
	if err != nil {
		return raknet.PrepareForStartMessage{}, fmt.Errorf("setupInputs: %w", err)
	}
	message := raknet.PrepareForStartMessage{
		Level:       util.HashID(levelAsset + ".Level"),
		VariantSeed: mapSeed,
		// Build 103 consumes marker-set conditions here, not party membership.
		// Shipped sets have no conditions; no special level flags are requested.
		ConditionMask: prepareMask,
		LevelIndex:    levelIndex,
	}
	return message, nil
}

func marshalZoneStart(binding game.GameplayBinding) ([]byte, error) {
	levelIndex, err := zoneLevelIndex(binding)
	if err != nil {
		return nil, fmt.Errorf("startStage: %w", err)
	}
	packet, err := memberraknet.GameStart(levelIndex)
	if err != nil {
		return nil, fmt.Errorf("startMarshal: %w", err)
	}
	return packet, nil
}

func zoneLevelIndex(binding game.GameplayBinding) (uint32, error) {
	switch binding.Mode {
	case game.ModeTutorial, game.ModeArena:
		return 0, nil
	case game.ModeChain:
		// The native prepare and start handlers share sub_9BD4F0, whose
		// campaign stage range is 1..72. A player slot is never a stage.
		if binding.ChainLevelIndex == 0 || binding.ChainLevelIndex > 72 {
			return 0, fmt.Errorf("zone campaign stage %d out of range", binding.ChainLevelIndex)
		}
		return binding.ChainLevelIndex, nil
	default:
		return 0, fmt.Errorf("zone mode %d unavailable", binding.Mode)
	}
}

// zoneVariantSeed is a separate random stream from loot and population. Its
// inputs are shared by all members and survive checkpoint restoration; player
// slots, party size, progression, and transient game IDs must not affect it.
func zoneVariantSeed(runSeed uint64, levelAsset string, levelIndex uint32) uint32 {
	const streamSalt = uint64(0x6d6170)
	mixed := runSeed ^ uint64(util.HashID(levelAsset))<<32 ^
		uint64(levelIndex)<<16 ^ streamSalt
	mixed ^= mixed >> 30
	mixed *= 0xbf58476d1ce4e5b9
	mixed ^= mixed >> 27
	mixed *= 0x94d049bb133111eb
	mixed ^= mixed >> 31
	seed := uint32(mixed) ^ uint32(mixed>>32)
	// Both are native automatic-seed sentinels. Sending either would allow
	// clients to choose different variants from their local random state/time.
	if seed == 0 || seed == ^uint32(0) {
		return uint32(streamSalt)
	}
	return seed
}
