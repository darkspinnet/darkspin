package unlock

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	zonecallback "github.com/darkspinnet/darkspin/server/zone/callback"
)

const (
	FirstChainLevelIndex      = 1
	SecondChainLevelIndex     = 2
	CatalystChainLevelIndex   = 3
	OverdriveChainLevelIndex  = 5
	TutorialInitialBoundary   = 2
	TutorialAbilityBoundary   = 3
	InitialAbilityBoundary    = 3
	RandomAbilityBoundary     = 4
	FullAbilityBoundary       = 5
	SupportAbilityBoundary    = 9
	RandomDelay               = 6 * time.Second
	FirstClearSupportDelay    = 6 * time.Second
	FirstClearBossArmingDelay = 13 * time.Second
	SupportMutationDeadline   = 6 * time.Second
	SupportFinalDeadline      = 19 * time.Second
	CatalystMutationDeadline  = 6 * time.Second
	CatalystFinalDeadline     = 20 * time.Second
	OverdriveMutationDeadline = 8 * time.Second
	OverdriveFinalDeadline    = 24 * time.Second
)

var supportDeadlines = []time.Duration{
	SupportMutationDeadline,
	12 * time.Second,
	17 * time.Second,
	SupportFinalDeadline,
}

var catalystDeadlines = []time.Duration{
	CatalystMutationDeadline,
	10 * time.Second,
	14 * time.Second,
	18 * time.Second,
	CatalystFinalDeadline,
}

var overdriveDeadlines = []time.Duration{
	OverdriveMutationDeadline,
	13 * time.Second,
	17 * time.Second,
	21 * time.Second,
	OverdriveFinalDeadline,
}

func SupportDeadlines() []time.Duration {
	return append([]time.Duration(nil), supportDeadlines...)
}

func CatalystDeadlines() []time.Duration {
	return append([]time.Duration(nil), catalystDeadlines...)
}

func OverdriveDeadlines() []time.Duration {
	return append([]time.Duration(nil), overdriveDeadlines...)
}

func IsFirstClear(binding game.GameplayBinding) bool {
	return !binding.IsWarped && binding.ChainLevelIndex == FirstChainLevelIndex &&
		binding.ChainProgression < FirstChainLevelIndex
}

func AreCatalystDropsUnlocked(binding game.GameplayBinding) bool {
	return binding.Mode == game.ModeChain && binding.IsCatalystUnlocked
}

func InitialAbilityCount(binding game.GameplayBinding) uint32 {
	if binding.Mode == game.ModeTutorial {
		return TutorialInitialBoundary
	}
	if binding.ChainProgression >= SecondChainLevelIndex {
		return SupportAbilityBoundary
	}
	if IsFirstClear(binding) {
		return InitialAbilityBoundary
	}
	return FullAbilityBoundary
}

func IsRandomPublication(publication game.CampaignDirectorPublication) bool {
	return zonecallback.HasCallback(publication, zonecallback.RandomUnlock)
}

func IsSupportPublication(publication game.CampaignDirectorPublication) bool {
	return zonecallback.HasCallback(publication, zonecallback.SupportUnlock)
}

type AbilityPublication struct {
	Slot         uint16
	AbilityCount uint32
}

func NewAbilityPublication(slot uint16, abilityCount uint32) (AbilityPublication, error) {
	if abilityCount < InitialAbilityBoundary ||
		(abilityCount > FullAbilityBoundary &&
			abilityCount != SupportAbilityBoundary) {
		return AbilityPublication{}, fmt.Errorf("abilityCount: %d", abilityCount)
	}
	return AbilityPublication{Slot: slot, AbilityCount: abilityCount}, nil
}
