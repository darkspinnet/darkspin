//go:build scenario

package game

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

type ScenarioContinueRequest struct {
	RunID  string
	UserID int64
}

// ScenarioContinueReceipt describes a fresh pre-game shell, not client entry,
// readiness, a restored checkpoint or an accepted gameplay setup.
type ScenarioContinueReceipt struct {
	GameID     uint32
	RunSeed    uint64
	Level      string
	MapSeed    uint32
	Occurrence uint32
	Difficulty uint32
	Slot       uint16
}

func (e *Manager) PrepareScenarioContinue(
	ctx context.Context, req ScenarioContinueRequest, member *sporenet.User,
) (ScenarioContinueReceipt, error) {
	if ctx == nil || e == nil || member == nil {
		return ScenarioContinueReceipt{}, errors.New("scenario Continue dependencies unavailable")
	}
	err := ctx.Err()
	if err != nil {
		return ScenarioContinueReceipt{}, fmt.Errorf("continueContext: %w", err)
	}
	e.mu.RLock()
	configured, isConfigured := e.requests[req.UserID]
	isJoined := false
	for _, instance := range e.games {
		if instance.HasPlayer(req.UserID) {
			isJoined = true
			break
		}
	}
	warpLevel := e.pendingCampaignWarps[req.UserID]
	isLevelSelected := configured.Occurrence > 0 && int(configured.Occurrence) <= len(e.chainLevels) &&
		strings.TrimSuffix(e.chainLevels[configured.Occurrence-1], ".Level") == configured.Level
	isGameIDAvailable := e.nextID != 0 && e.games[e.nextID] == nil
	e.mu.RUnlock()
	if !isConfigured || configured.RunID != req.RunID || req.UserID <= 0 ||
		!isLevelSelected || !isGameIDAvailable || isJoined || warpLevel != "" {
		return ScenarioContinueReceipt{}, errors.New("scenario Continue configured fresh game unavailable")
	}
	err = configured.validate()
	if err != nil {
		return ScenarioContinueReceipt{}, fmt.Errorf("continueInputs: %w", err)
	}
	view := member.View()
	if view.Account.ID != req.UserID || view.AuthToken != "" || member.CurrentGameID() != 0 ||
		member.CurrentPlaygroupID() != 0 || view.Account.ChainProgression != 1 ||
		!view.Account.IsTutorialCompleted() {
		return ScenarioContinueReceipt{}, errors.New("scenario Continue detached member mismatch")
	}
	err = validateScenarioContinueSquad(view, configured)
	if err != nil {
		return ScenarioContinueReceipt{}, fmt.Errorf("continueSquad: %w", err)
	}
	err = ctx.Err()
	if err != nil {
		return ScenarioContinueReceipt{}, fmt.Errorf("continueCreateContext: %w", err)
	}
	instance := e.Create()
	instance.mu.Lock()
	instance.RunSeed = uint64(configured.MapSeed)
	instance.Info.Mode = ModeChain
	instance.Info.Level = configured.Level
	instance.Info.NetworkTopology = 1
	instance.Info.MaxPlayers = 1
	instance.Info.Attributes["GameType"] = "2"
	instance.Info.Attributes["SelectedDifficulty"] = "2"
	instance.mu.Unlock()
	instance.SetExpectedPlayerCount(1)
	// Serialize admission against another preparation for this same profile.
	// The existing registry remains the owner; no extra run registry is added.
	e.mu.Lock()
	isCurrent := e.games[instance.ID] == instance && e.requests[req.UserID] == configured
	for _, other := range e.games {
		if other != instance && other.HasPlayer(req.UserID) {
			isCurrent = false
			break
		}
	}
	if !isCurrent {
		e.mu.Unlock()
		e.removeScenarioContinue(instance)
		return ScenarioContinueReceipt{}, errors.New("scenario Continue membership changed")
	}
	err = instance.AddPlayerAtSlot(member, 0)
	e.mu.Unlock()
	if err != nil {
		e.removeScenarioContinue(instance)
		return ScenarioContinueReceipt{}, fmt.Errorf("continueMember: %w", err)
	}
	err = ctx.Err()
	if err != nil {
		e.removeScenarioContinue(instance)
		return ScenarioContinueReceipt{}, fmt.Errorf("continueRegisterContext: %w", err)
	}
	e.mu.Lock()
	err = ctx.Err()
	if err != nil {
		e.mu.Unlock()
		e.removeScenarioContinue(instance)
		return ScenarioContinueReceipt{}, fmt.Errorf("continueReturnContext: %w", err)
	}
	if e.games[instance.ID] != instance || e.requests[req.UserID] != configured {
		e.mu.Unlock()
		e.removeScenarioContinue(instance)
		return ScenarioContinueReceipt{}, errors.New("scenario Continue prepared game changed")
	}
	if e.continueGamesByUser == nil {
		e.continueGamesByUser = make(map[int64]*Instance)
	}
	e.continueGamesByUser[req.UserID] = instance
	e.mu.Unlock()
	return ScenarioContinueReceipt{
		GameID: instance.ID, RunSeed: instance.RunSeed, Level: configured.Level,
		MapSeed: configured.MapSeed, Occurrence: configured.Occurrence,
		Difficulty: configured.Difficulty, Slot: 0,
	}, nil
}

// IsScenarioContinueHost identifies only the fresh generated host setup.
// It does not assert client entry or readiness.
func (e *Manager) IsScenarioContinueHost(gameID uint32, userID int64) bool {
	if e == nil || gameID == 0 || userID <= 0 {
		return false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	instance := e.continueGamesByUser[userID]
	if instance == nil || instance.ID != gameID || e.games[gameID] != instance {
		return false
	}
	instance.mu.RLock()
	defer instance.mu.RUnlock()
	if instance.Info.State != StateInitializing || !instance.StartedAt.IsZero() ||
		instance.isStartPublished || instance.isStartPublicationReserved ||
		instance.isCheckpointRestore || instance.Info.IsWarped ||
		instance.Info.Mode != ModeChain || instance.hostUserID != userID ||
		instance.Info.ExpectedPlayerCount != 1 || len(instance.players) != 1 {
		return false
	}
	member := instance.players[userID]
	slot, isSlotAssigned := instance.slots[userID]
	return member != nil && member.Account.ID == userID &&
		member.CurrentGameID() == gameID && isSlotAssigned && slot == 0
}

func validateScenarioContinueSquad(view sporenet.UserView, configured ScenarioMapRequest) error {
	if view.Account.DefaultDeckPVEID != configured.SquadID {
		return errors.New("scenario Continue default squad mismatch")
	}
	var squad sporenet.Squad
	squadCount := 0
	for _, candidate := range view.Squads {
		if candidate.ID == configured.SquadID {
			squad = candidate
			squadCount++
		}
	}
	if squadCount != 1 || squad.Category != "pve" || squad.IsLockedFor(view.Account) {
		return errors.New("scenario Continue squad unavailable")
	}
	for index, creatureID := range squad.CreatureIDs {
		if creatureID == 0 {
			return errors.New("scenario Continue creature identity unavailable")
		}
		creatureCount := 0
		for _, creature := range view.Creatures {
			if creature == nil || creature.ID != creatureID {
				continue
			}
			if creature.Template == nil || creature.Template.Noun != configured.HeroNouns[index] {
				return fmt.Errorf("scenario Continue hero[%d] mismatch", index)
			}
			creatureCount++
		}
		if creatureCount != 1 {
			return fmt.Errorf("scenario Continue hero[%d] unavailable or ambiguous", index)
		}
	}
	return nil
}

// Remove only this newly owned shell, never another game that reused its ID.
func (e *Manager) removeScenarioContinue(instance *Instance) {
	e.mu.Lock()
	isOwned := e.games[instance.ID] == instance
	if isOwned {
		delete(e.games, instance.ID)
	}
	for userID, prepared := range e.continueGamesByUser {
		if prepared == instance {
			delete(e.continueGamesByUser, userID)
		}
	}
	observer := e.removalObserver
	e.mu.Unlock()
	for _, player := range instance.Players() {
		instance.RemovePlayer(player.Account.ID)
	}
	if isOwned && observer != nil {
		observer(instance.ID)
	}
}
