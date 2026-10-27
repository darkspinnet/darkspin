package gameplay

import (
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/util"
)

// Keep the authored introductory roster's existing stable sequence. Replay
// population instead belongs to the durable run, actual map and difficulty;
// transient game IDs and concurrent admission order must not change it.
// This is server seed policy, not the client's independently clock-seeded MT.
func campaignPopulationSeed(director game.CampaignDirector, binding game.GameplayBinding) uint32 {
	seed, isConfigured := binding.ScenarioPopulationSeed()
	if isConfigured {
		return seed
	}
	if director.IsFirstTimeRosterSelected {
		return (binding.Difficulty << 24) ^ 0x103 ^ ((binding.ChainLevelIndex - 1) << 16)
	}
	if binding.Mode != game.ModeChain || !binding.IsReplay {
		return binding.GameID ^ (binding.Difficulty << 24) ^ 0x103
	}
	mixed := binding.RunSeed ^ uint64(util.HashID(director.Level))<<32 ^
		uint64(binding.Difficulty)<<16 ^ uint64(0x706f70756c617469)
	mixed ^= mixed >> 30
	mixed *= 0xbf58476d1ce4e5b9
	mixed ^= mixed >> 27
	mixed *= 0x94d049bb133111eb
	mixed ^= mixed >> 31
	return uint32(mixed) ^ uint32(mixed>>32)
}
