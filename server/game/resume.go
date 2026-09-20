package game

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

// ResumeCoordinator restores the Blaze-visible membership that lets build 103
// discover and join a durable zone through its native reconnect path.
type ResumeCoordinator struct {
	gameManager  *Manager
	resumeSource ResumeSource
}

func NewResumeCoordinator(
	gameManager *Manager, resumeSource ResumeSource,
) (*ResumeCoordinator, error) {
	if gameManager == nil {
		return nil, errors.New("resume game manager unavailable")
	}
	if resumeSource == nil {
		return nil, errors.New("resume source unavailable")
	}
	return &ResumeCoordinator{
		gameManager: gameManager, resumeSource: resumeSource,
	}, nil
}

// ActivateResume is idempotent. It recreates only game membership; the
// gameplay adapter restores and publishes the richer zone after native rejoin.
func (e *ResumeCoordinator) ActivateResume(
	ctx context.Context, user *sporenet.User,
) (bool, error) {
	if e == nil || e.gameManager == nil || e.resumeSource == nil || user == nil {
		return false, nil
	}
	if user.CurrentGameID() != 0 {
		return true, nil
	}
	// A fresh login may replace the user object while its game is still live.
	// Reattach that membership without treating an unsaved game as a checkpoint.
	liveResume, isLive, err := e.gameManager.FindLiveResume(user.Account.ID)
	if err != nil {
		return false, fmt.Errorf("resumeLive: %w", err)
	}
	if isLive {
		instance := e.gameManager.Game(liveResume.GameID)
		if instance != nil && instance.AddPlayer(user) {
			return true, nil
		}
		return false, errors.New("live resume membership unavailable")
	}
	checkpoint, isFound, err := e.resumeSource.FindResume(ctx, user.Account.ID)
	if err != nil {
		return false, fmt.Errorf("resumeFind: %w", err)
	}
	if !isFound {
		return false, nil
	}
	_, err = e.gameManager.Restore(checkpoint, user)
	if err != nil {
		return false, fmt.Errorf("resumeRestore: %w", err)
	}
	return true, nil
}
