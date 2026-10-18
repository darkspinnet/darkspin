package gameplay

import (
	"context"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/zone"
	zonedeath "github.com/darkspinnet/darkspin/server/zone/death"
	deathraknet "github.com/darkspinnet/darkspin/server/zone/death/raknet103"
)

func (e campaignDeathRuntime) resetRepairTimer(instance *zone.Zone, objectID uint32) error {
	if e.timer == nil {
		return nil
	}
	retainedRun, delays, isReset, err := instance.Death().ResetTimer(context.Background(), objectID, 10*time.Second)
	if err != nil {
		return fmt.Errorf("repairTimer: %w", err)
	}
	if !isReset {
		return nil
	}
	run, isRunFound := retainedRun.(*deathraknet.Run)
	if !isRunFound {
		return fmt.Errorf("repairRun: unexpected death implementation %T", retainedRun)
	}
	deadlines := run.Deadlines()
	for index, delay := range delays {
		step := campaignDeathDeadline{
			zone: instance, objectID: objectID, run: run,
			deadline: deadlines[index], isFinal: index == len(delays)-1, logger: e.logger,
		}
		cancel, scheduleErr := e.timer.Schedule(delay, step.execute)
		if scheduleErr != nil {
			return fmt.Errorf("repairDeadline[%d]: %w", index, scheduleErr)
		}
		scheduleErr = instance.Death().AddCancel(objectID, run, zonedeath.Cancel(cancel))
		if scheduleErr != nil {
			cancel()
			return fmt.Errorf("repairCancel[%d]: %w", index, scheduleErr)
		}
	}
	return nil
}
