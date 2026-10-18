package game

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/server/sim"
)

// CampaignPartyCompletedStages snapshots every game member, including members who
// have not connected a gameplay peer yet. Progress belongs to the account;
// individual transport sessions must not choose different party rosters.
func (e *GameplayJoin) CampaignPartyCompletedStages(
	ctx context.Context, req GameplayBinding,
) ([]uint32, error) {
	if e == nil || e.gameManager == nil || e.userFinder == nil || ctx == nil {
		return nil, errors.New("party progress dependency unavailable")
	}
	err := ctx.Err()
	if err != nil {
		return nil, fmt.Errorf("partyContext: %w", err)
	}
	user := e.userFinder.UserByID(int64(req.UserID))
	if user == nil || user.CurrentGameID() != req.GameID {
		return nil, fmt.Errorf("partyUser: %w", ErrGameplayUserNotFound)
	}
	instance := e.gameManager.Game(req.GameID)
	if instance == nil {
		return nil, fmt.Errorf("partyGame: %w", ErrGameplayGameNotFound)
	}
	slot, playerMask, isMember := instance.PlayerBinding(int64(req.UserID))
	if !isMember || slot != req.Slot || playerMask&req.PlayerMask == 0 {
		return nil, fmt.Errorf("partySlot: %w", ErrGameplaySlotNotFound)
	}
	players := instance.Players()
	if len(players) == 0 {
		return nil, errors.New("party has no members")
	}
	completedStages := make([]uint32, 0, len(players))
	for _, player := range players {
		if player == nil {
			return nil, errors.New("party member unavailable")
		}
		completedStages = append(completedStages, player.PresenceSnapshot().ChainProgression)
	}
	return completedStages, nil
}

// sub_9F6DB0 compares each member's native campaign progress to the full
// stage. This predicate does not decide whether first-time mode is enabled.
func isPartyBelowStage(completedStages []uint32, stage uint32) bool {
	for _, progress := range completedStages {
		if progress < stage {
			return true
		}
	}
	return false
}

// sub_9F6640 draws at most three first-time minions and specials. The caller
// falls back to normal composition when the filtered minion list is empty,
// regardless of whether any first-time special qualifies.
func selectFirstTimeRoster(
	director CampaignDirector, random *sim.SimulatorRandom,
) (CampaignDirector, error) {
	pools := append([]CampaignDirectorPool(nil), director.Pools...)
	isMinionFound := false
	for poolIndex := range pools {
		pool := &pools[poolIndex]
		if !strings.EqualFold(pool.ConfigurationName, "firstTimeConfig") ||
			(!strings.EqualFold(pool.ConfigKind, "minion") && !strings.EqualFold(pool.ConfigKind, "special")) {
			continue
		}
		entries := make([]CampaignDirectorEntry, 0, len(pool.Entries))
		for _, entry := range pool.Entries {
			if director.Difficulty >= entry.MinimumDifficulty && director.Difficulty <= entry.MaximumDifficulty {
				entries = append(entries, entry)
			}
		}
		if len(entries) > 3 {
			err := shuffleCampaignRoster(entries, random)
			if err != nil {
				return CampaignDirector{}, fmt.Errorf("firstTimeShuffle: %w", err)
			}
			entries = entries[:3]
		}
		pool.Entries = entries
		if strings.EqualFold(pool.ConfigKind, "minion") && len(entries) != 0 {
			isMinionFound = true
		}
	}
	director.Pools = pools
	director.IsFirstTimeRosterSelected = isMinionFound
	return director, nil
}
