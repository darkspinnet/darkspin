package game

import "fmt"

// FindLiveResume includes admitted games that have not produced a checkpoint
// yet. Native account login can rejoin these in-memory game memberships too.
func (e *Manager) FindLiveResume(userID int64) (ResumeCheckpoint, bool, error) {
	if e == nil || userID <= 0 {
		return ResumeCheckpoint{}, false, nil
	}
	for _, instance := range e.Games() {
		instance.mu.RLock()
		user := instance.players[userID]
		slot, isMember := instance.slots[userID]
		if !isMember || user == nil || user.CurrentGameID() != instance.ID ||
			instance.Info.Mode != ModeChain {
			instance.mu.RUnlock()
			continue
		}
		difficulty, err := resolveGameplayDifficulty(instance.Info)
		resume := ResumeCheckpoint{
			GameID: instance.ID, Level: instance.Info.Level,
			Difficulty: difficulty, Slot: slot,
			ExpectedPlayerCount: instance.Info.ExpectedPlayerCount,
		}
		instance.mu.RUnlock()
		if err != nil {
			return ResumeCheckpoint{}, false, fmt.Errorf("liveDifficulty: %w", err)
		}
		return resume, true, nil
	}
	return ResumeCheckpoint{}, false, nil
}
