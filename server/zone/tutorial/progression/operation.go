package progression

import (
	"context"
	"errors"
	"fmt"
)

type Completion struct {
	CumulativeXP int32
	IsChanged    bool
}

type Experience struct {
	CumulativeXP uint32
	Level        uint32
}

type CompletionStore interface {
	CompleteTutorial(context.Context, int64) (Completion, error)
}

type RestartStore interface {
	ResetTutorialExperience(context.Context, int64) (Experience, error)
}

type AwardStore interface {
	GrantTutorialExperience(context.Context, int64, uint32) (Experience, error)
}

func Complete(
	ctx context.Context, store CompletionStore, userID int64,
) (Completion, error) {
	if ctx == nil || store == nil || userID <= 0 {
		return Completion{}, errors.New("tutorial completion invalid")
	}
	completion, err := store.CompleteTutorial(ctx, userID)
	if err != nil {
		return Completion{}, fmt.Errorf("completionStore: %w", err)
	}
	return completion, nil
}

func Restart(
	ctx context.Context, store RestartStore, userID int64,
) (Experience, error) {
	if ctx == nil || store == nil || userID <= 0 {
		return Experience{}, errors.New("tutorial restart invalid")
	}
	experience, err := store.ResetTutorialExperience(ctx, userID)
	if err != nil {
		return Experience{}, fmt.Errorf("restartStore: %w", err)
	}
	return experience, nil
}

func Award(
	ctx context.Context, store AwardStore, userID int64, amount uint32,
) (Experience, error) {
	if ctx == nil || store == nil || userID <= 0 || amount == 0 {
		return Experience{}, errors.New("tutorial award invalid")
	}
	experience, err := store.GrantTutorialExperience(ctx, userID, amount)
	if err != nil {
		return Experience{}, fmt.Errorf("awardStore: %w", err)
	}
	return experience, nil
}
