package zone

import (
	"errors"
	"fmt"

	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func (e *Zone) ReacquireBossTarget(objectID uint32) ([]zonenpc.Snapshot, error) {
	if e == nil {
		return nil, errors.New("boss zone unavailable")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != StateActive {
		return nil, nil
	}
	acquired, err := e.info.NPCs.ReacquireBossTarget(objectID, e.livePlayerAlignedTargets())
	if err != nil {
		return nil, fmt.Errorf("bossTarget: %w", err)
	}
	for _, npc := range acquired {
		e.info.Timeline.Cancel(zonenpc.ReturnTimelineKey(npc.Plan.ObjectID))
	}
	return acquired, nil
}
