package objective

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
)

// NewDifficultySession binds the current campaign difficulty before any
// packaged objective initializer runs. The catalog inputs remain immutable so
// simultaneous zones can evaluate their own difficulty-dependent thresholds.
// Native GetMajorDifficulty uses the authored campaign minor-stage count, the
// same one-based stage decomposition used by campaign reward progression.
func NewDifficultySession(
	ctx context.Context, inputs []sim.LuaObjectiveInput,
	objects []game.CampaignScriptObject, playerIndex uint8,
	difficulty, minorStageCount uint32, modes ...game.Mode,
) (*Session, error) {
	if difficulty == 0 {
		return nil, errors.New("objective difficulty: zero")
	}
	if minorStageCount == 0 {
		return nil, errors.New("objective minor stage count: zero")
	}
	majorDifficulty := float64((difficulty-1)/minorStageCount + 1)
	zoneInputs := make([]sim.LuaObjectiveInput, len(inputs))
	copy(zoneInputs, inputs)
	for index := range zoneInputs {
		zoneInputs[index].MajorDifficulty = majorDifficulty
	}
	session, err := NewSession(ctx, zoneInputs, objects, playerIndex, modes...)
	if err != nil {
		return nil, fmt.Errorf("difficultySession: %w", err)
	}
	return session, nil
}
