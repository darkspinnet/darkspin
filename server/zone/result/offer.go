package result

import (
	"errors"
	"fmt"
	"strings"
)

// OfferCommand describes the completed chain level and authored level order.
type OfferCommand struct {
	CurrentLevel    string
	ChainLevelIndex uint32
	ChainLevel      []string
}

// Offer identifies the completed progression index and next playable level.
type Offer struct {
	CompletedIndex uint32
	NextLevel      string
	IsTerminal     bool
}

// ResolveOffer validates the active level against the authored chain before
// exposing the next level.
func ResolveOffer(command OfferCommand) (Offer, error) {
	if command.CurrentLevel == "" || command.ChainLevelIndex == 0 {
		return Offer{}, errors.New("offer binding unavailable")
	}
	completedOrdinal := int(command.ChainLevelIndex - 1)
	if completedOrdinal < 0 || completedOrdinal >= len(command.ChainLevel) {
		return Offer{}, fmt.Errorf("offer index unavailable: %d", command.ChainLevelIndex)
	}
	currentReference := strings.TrimSuffix(command.ChainLevel[completedOrdinal], ".Level")
	if !strings.EqualFold(currentReference, command.CurrentLevel) {
		return Offer{}, fmt.Errorf(
			"offer level mismatch: got %s, want %s",
			command.CurrentLevel, currentReference,
		)
	}
	if completedOrdinal+1 == len(command.ChainLevel) {
		return Offer{
			CompletedIndex: command.ChainLevelIndex,
			NextLevel:      currentReference,
			IsTerminal:     true,
		}, nil
	}
	nextLevel := strings.TrimSuffix(command.ChainLevel[completedOrdinal+1], ".Level")
	if nextLevel == "" {
		return Offer{}, errors.New("offer next level empty")
	}
	return Offer{
		CompletedIndex: command.ChainLevelIndex,
		NextLevel:      nextLevel,
	}, nil
}
