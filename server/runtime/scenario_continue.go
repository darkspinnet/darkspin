//go:build scenario

package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

func (e *Server) scenarioContinue(
	ctx context.Context, loginName, runID string, profile sporenet.ScenarioProfile,
) error {
	if e.logger == nil {
		return errors.New("scenario Continue receipt logger unavailable")
	}
	member, err := e.userManager.ScenarioContinueMember(ctx, sporenet.ScenarioContinueMemberRequest{
		LoginName: loginName, UserID: profile.UserID,
		SquadID: profile.ActualSquadID, HeroNouns: profile.HeroNouns,
	})
	if err != nil {
		return fmt.Errorf("continueProfile: %w", err)
	}
	receipt, err := e.gameManager.PrepareScenarioContinue(ctx, game.ScenarioContinueRequest{
		RunID: runID, UserID: profile.UserID,
	}, member)
	if err != nil {
		return fmt.Errorf("continuePrepare: %w", err)
	}
	// Only the allowlisted pre-game receipt enters the existing run-owned log.
	// No credentials, detached member, readiness or synthetic start is recorded.
	e.logger.Printf("scenario_continue game_id=%d run_seed=%d level=%q map_seed=%d occurrence=%d difficulty=%d slot=%d",
		receipt.GameID, receipt.RunSeed, receipt.Level, receipt.MapSeed,
		receipt.Occurrence, receipt.Difficulty, receipt.Slot)
	return nil
}
