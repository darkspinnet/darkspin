// Package scheduler runs delayed callbacks for game and protocol services.
package scheduler

import (
	"container/heap"
	"log"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

const (
	taskWarningDuration = 250 * time.Millisecond
	taskDetachDuration  = time.Second
	taskShutdownWait    = 5 * time.Second
)

// TaskID uniquely identifies a scheduled task. Zero is never a valid ID.
type TaskID uint32

// Task is invoked once with its scheduler-assigned ID.
type Task func(TaskID)

type scheduledTask struct {
	id        TaskID
	executeAt time.Time
	function  Task
	index     int
}

type taskHeap []*scheduledTask

func (h taskHeap) Len() int { return len(h) }

func (h taskHeap) Less(left, right int) bool {
	return h[left].executeAt.Before(h[right].executeAt)
}

func (h taskHeap) Swap(left, right int) {
	h[left], h[right] = h[right], h[left]
	h[left].index = left
	h[right].index = right
}

func (h *taskHeap) Push(value any) {
	task := value.(*scheduledTask)
	task.index = len(*h)
	*h = append(*h, task)
}

func (h *taskHeap) Pop() any {
	old := *h
	last := len(old) - 1
	task := old[last]
	task.index = -1
	old[last] = nil
	*h = old[:last]
	return task
}

// Scheduler is a safe Go equivalent of recap_server's delayed-task worker.
type Scheduler struct {
	mu              sync.Mutex
	tasks           taskHeap
	activeTasks     map[TaskID]*scheduledTask
	wake            chan struct{}
	stop            chan struct{}
	done            chan struct{}
	once            sync.Once
	isStopped       bool
	activeTaskCount int
	taskIdle        chan struct{}
	nextID          atomic.Uint32
}

// New starts a scheduler worker.
func New() *Scheduler {
	scheduler := &Scheduler{
		activeTasks: make(map[TaskID]*scheduledTask),
		wake:        make(chan struct{}, 1),
		stop:        make(chan struct{}),
		done:        make(chan struct{}),
	}
	heap.Init(&scheduler.tasks)
	go scheduler.run()
	return scheduler
}

// Add schedules a task after delay. It returns zero when the scheduler has
// stopped or the callback is nil.
func (s *Scheduler) Add(delay time.Duration, function Task) TaskID {
	if function == nil {
		return 0
	}
	if delay < 0 {
		delay = 0
	}

	id := TaskID(s.nextID.Add(1))
	if id == 0 {
		id = TaskID(s.nextID.Add(1))
	}
	task := &scheduledTask{
		id:        id,
		executeAt: time.Now().Add(delay),
		function:  function,
	}

	s.mu.Lock()
	if s.isStopped {
		s.mu.Unlock()
		return 0
	}
	s.activeTasks[id] = task
	heap.Push(&s.tasks, task)
	s.mu.Unlock()
	s.notify()
	return id
}

// Cancel prevents a pending task from running.
func (s *Scheduler) Cancel(id TaskID) {
	if id == 0 {
		return
	}
	s.mu.Lock()
	delete(s.activeTasks, id)
	s.mu.Unlock()
	s.notify()
}

// Shutdown discards pending tasks and waits for the worker to finish.
func (s *Scheduler) Shutdown() {
	s.once.Do(func() {
		s.mu.Lock()
		s.isStopped = true
		s.activeTasks = make(map[TaskID]*scheduledTask)
		s.tasks = nil
		s.mu.Unlock()
		close(s.stop)
	})
	<-s.done
	s.mu.Lock()
	activeTaskCount := s.activeTaskCount
	taskIdle := s.taskIdle
	s.mu.Unlock()
	if activeTaskCount == 0 || taskIdle == nil {
		return
	}
	timer := time.NewTimer(taskShutdownWait)
	defer timer.Stop()
	select {
	case <-taskIdle:
	case <-timer.C:
		log.Printf(
			"scheduler shutdown continuing with %d active tasks after %s",
			activeTaskCount, taskShutdownWait,
		)
	}
}

func (s *Scheduler) run() {
	defer close(s.done)
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}

	for {
		task, wait := s.nextTask()
		if task == nil {
			select {
			case <-s.wake:
				continue
			case <-s.stop:
				return
			}
		}
		if wait > 0 {
			timer.Reset(wait)
			select {
			case <-timer.C:
			case <-s.wake:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				continue
			case <-s.stop:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return
			}
		}

		function := s.take(task)
		if function != nil {
			if !s.executeTask(task.id, function) {
				return
			}
		}
	}
}

func (s *Scheduler) executeTask(id TaskID, function Task) bool {
	if !s.beginTask() {
		return false
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer s.endTask()
		invokeTask(id, function)
	}()
	warningTimer := time.NewTimer(taskWarningDuration)
	defer warningTimer.Stop()
	select {
	case <-done:
		return true
	case <-s.stop:
		return false
	case <-warningTimer.C:
		log.Printf(
			"scheduler task %d remains active after %s", id, taskWarningDuration,
		)
	}
	detachTimer := time.NewTimer(taskDetachDuration - taskWarningDuration)
	defer detachTimer.Stop()
	select {
	case <-done:
		return true
	case <-s.stop:
		return false
	case <-detachTimer.C:
		log.Printf(
			"scheduler task %d detached after %s so later tasks can continue",
			id, taskDetachDuration,
		)
		return true
	}
}

func (s *Scheduler) beginTask() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.isStopped {
		return false
	}
	if s.activeTaskCount == 0 {
		s.taskIdle = make(chan struct{})
	}
	s.activeTaskCount++
	return true
}

func (s *Scheduler) endTask() {
	s.mu.Lock()
	if s.activeTaskCount > 0 {
		s.activeTaskCount--
	}
	if s.activeTaskCount == 0 && s.taskIdle != nil {
		close(s.taskIdle)
		s.taskIdle = nil
	}
	s.mu.Unlock()
}

func invokeTask(id TaskID, function Task) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		log.Printf(
			"scheduler task %d panicked: %v\n%s", id, recovered, debug.Stack(),
		)
	}()
	function(id)
}

func (s *Scheduler) nextTask() (*scheduledTask, time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.tasks) > 0 {
		task := s.tasks[0]
		if _, active := s.activeTasks[task.id]; active {
			return task, time.Until(task.executeAt)
		}
		heap.Pop(&s.tasks)
	}
	return nil, 0
}

func (s *Scheduler) take(expected *scheduledTask) Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.tasks) == 0 || s.tasks[0] != expected {
		return nil
	}
	heap.Pop(&s.tasks)
	task, active := s.activeTasks[expected.id]
	if !active {
		return nil
	}
	delete(s.activeTasks, expected.id)
	return task.function
}

func (s *Scheduler) notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
