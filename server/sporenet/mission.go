package sporenet

import (
	"context"
	"fmt"

	"github.com/darkspinnet/darkspin/server/activity"
)

type campaignMissionObserver interface {
	BeginMission(context.Context, activity.MissionStart) error
}

// ObserveCampaignMissionStart forwards accepted gameplay participation to the
// optional metrics owner. The metrics store verifies the current account state.
func (e *UserManager) ObserveCampaignMissionStart(ctx context.Context, req activity.MissionStart) error {
	if e == nil {
		return nil
	}
	e.mu.RLock()
	observer := e.activityObserver
	e.mu.RUnlock()
	missionObserver, isSupported := observer.(campaignMissionObserver)
	if !isSupported {
		return nil
	}
	err := missionObserver.BeginMission(ctx, req)
	if err != nil {
		return fmt.Errorf("missionBegin: %w", err)
	}
	return nil
}
