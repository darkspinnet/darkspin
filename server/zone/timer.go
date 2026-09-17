package zone

import (
	"errors"
	"time"

	"github.com/darkspinnet/darkspin/server/scheduler"
)

// Timer schedules zone-owned world actions without exposing the process
// scheduler to individual gameplay features.
type Timer interface {
	Schedule(time.Duration, func()) (func(), error)
}

type SchedulerTimer struct {
	scheduler *scheduler.Scheduler
}

type schedulerTask struct {
	execute func()
}

type schedulerCancel struct {
	scheduler *scheduler.Scheduler
	taskID    scheduler.TaskID
}

func NewSchedulerTimer(taskScheduler *scheduler.Scheduler) *SchedulerTimer {
	return &SchedulerTimer{scheduler: taskScheduler}
}

func (e schedulerTask) run(scheduler.TaskID) {
	e.execute()
}

func (e schedulerCancel) cancel() {
	e.scheduler.Cancel(e.taskID)
}

func (e *SchedulerTimer) Schedule(
	delay time.Duration, execute func(),
) (func(), error) {
	if e == nil || e.scheduler == nil || execute == nil {
		return nil, errors.New("zone timer unavailable")
	}
	task := schedulerTask{execute: execute}
	taskID := e.scheduler.Add(delay, task.run)
	if taskID == 0 {
		return nil, errors.New("zone timer stopped")
	}
	cancel := schedulerCancel{scheduler: e.scheduler, taskID: taskID}
	return cancel.cancel, nil
}
