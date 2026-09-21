package gameplay

import (
	"errors"
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zonecheckpoint "github.com/darkspinnet/darkspin/server/zone/checkpoint"
)

const campaignDropStreamSalt = uint64(0xd40f)

func newCampaignDropRandom(
	binding game.GameplayBinding, restore *zonecheckpoint.Snapshot,
) (*sim.SimulatorRandom, error) {
	if binding.RunSeed == 0 || binding.Level == "" || binding.Difficulty == 0 {
		return nil, errors.New("campaign drop seed unavailable")
	}
	if restore != nil && restore.RunSeed != 0 && restore.RunSeed != binding.RunSeed {
		return nil, errors.New("campaign drop run seed mismatch")
	}
	if restore != nil && restore.IsDropRandomSet {
		random, err := sim.NewSimulatorRandomFromSnapshot(restore.DropRandom)
		if err != nil {
			return nil, fmt.Errorf("dropRandomRestore: %w", err)
		}
		return random, nil
	}
	return sim.NewSimulatorRandom(campaignDropSeed(binding)), nil
}

func campaignDropSeed(binding game.GameplayBinding) uint32 {
	levelHash := uint64(util.HashID(strings.ToLower(binding.Level)))
	mixed := binding.RunSeed ^ levelHash<<32 ^ uint64(binding.Difficulty)<<16 ^
		campaignDropStreamSalt
	mixed ^= mixed >> 30
	mixed *= 0xbf58476d1ce4e5b9
	mixed ^= mixed >> 27
	mixed *= 0x94d049bb133111eb
	mixed ^= mixed >> 31
	seed := uint32(mixed) ^ uint32(mixed>>32)
	if seed == 0 {
		return uint32(campaignDropStreamSalt)
	}
	return seed
}
