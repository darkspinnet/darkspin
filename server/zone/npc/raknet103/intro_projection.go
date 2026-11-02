package raknet103

import (
	"fmt"
	"time"

	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// FirstAggroCurrent ages a queued entrance at the recipient's projection drain.
// Its camera deadline must not restart when another peer receives it later.
func FirstAggroCurrent(plan zonenpc.FirstActionPlan, timestamp uint64, snapshot zonenpc.Snapshot, now time.Time) ([][]byte, error) {
	if !zonenpc.CanRetainIntroduction(snapshot.Plan, plan.Profile) {
		packets, err := FirstAggro(plan, timestamp)
		if err != nil {
			return nil, fmt.Errorf("introInitial: %w", err)
		}
		return packets, nil
	}
	if !snapshot.IsIntroductionPending() {
		return nil, nil
	}
	duration, radius := snapshot.IntroductionRemaining(now)
	plan.Profile.FirstAggroCinematicRadius = radius
	packets, err := firstAggro(plan, timestamp, duration, snapshot.Plan.IsIntroductionHidden)
	if err != nil {
		return nil, fmt.Errorf("introCurrent: %w", err)
	}
	return packets, nil
}
