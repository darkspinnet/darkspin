package raknet103

import (
	"errors"
	"sync"
)

// ActiveSession owns build-103 presentation runs local to one connected
// campaign member. Durable ability boundaries remain in the zone unlock
// session.
type ActiveSession struct {
	mu               sync.Mutex
	tutorialAbility  *AbilityUnlockRun
	tutorialCreature *CreatureUnlockRun
	support          *Run
	catalyst         *CatalystRun
	overdrive        *Run
}

func (e *ActiveSession) TutorialCreature() *CreatureUnlockRun {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.tutorialCreature
}

func (e *ActiveSession) InstallTutorialCreature(run *CreatureUnlockRun) error {
	if e == nil || run == nil {
		return errors.New("invalid tutorial creature presentation")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.tutorialCreature != nil {
		return errors.New("tutorial creature presentation already active")
	}
	e.tutorialCreature = run
	return nil
}

func (e *ActiveSession) ClearTutorialCreature(run *CreatureUnlockRun) bool {
	if e == nil || run == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.tutorialCreature != run {
		return false
	}
	e.tutorialCreature = nil
	return true
}

func NewActiveSession() *ActiveSession {
	return &ActiveSession{}
}

func (e *ActiveSession) TutorialAbility() *AbilityUnlockRun {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.tutorialAbility
}

func (e *ActiveSession) InstallTutorialAbility(run *AbilityUnlockRun) error {
	if e == nil || run == nil {
		return errors.New("invalid tutorial ability presentation")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.tutorialAbility != nil {
		return errors.New("tutorial ability presentation already active")
	}
	e.tutorialAbility = run
	return nil
}

func (e *ActiveSession) ClearTutorialAbility(run *AbilityUnlockRun) bool {
	if e == nil || run == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.tutorialAbility != run {
		return false
	}
	e.tutorialAbility = nil
	return true
}

func (e *ActiveSession) Support() *Run {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.support
}

func (e *ActiveSession) InstallSupport(run *Run) error {
	if e == nil || run == nil {
		return errors.New("invalid support presentation")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.support != nil {
		return errors.New("support presentation already active")
	}
	e.support = run
	return nil
}

func (e *ActiveSession) ClearSupport(run *Run) bool {
	if e == nil || run == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.support != run {
		return false
	}
	e.support = nil
	return true
}

func (e *ActiveSession) Catalyst() *CatalystRun {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.catalyst
}

func (e *ActiveSession) InstallCatalyst(run *CatalystRun) error {
	if e == nil || run == nil {
		return errors.New("invalid catalyst presentation")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.catalyst != nil {
		return errors.New("catalyst presentation already active")
	}
	e.catalyst = run
	return nil
}

func (e *ActiveSession) ClearCatalyst(run *CatalystRun) bool {
	if e == nil || run == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.catalyst != run {
		return false
	}
	e.catalyst = nil
	return true
}

func (e *ActiveSession) Overdrive() *Run {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.overdrive
}

func (e *ActiveSession) InstallOverdrive(run *Run) error {
	if e == nil || run == nil {
		return errors.New("invalid overdrive presentation")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.overdrive != nil {
		return errors.New("overdrive presentation already active")
	}
	e.overdrive = run
	return nil
}

func (e *ActiveSession) ClearOverdrive(run *Run) bool {
	if e == nil || run == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.overdrive != run {
		return false
	}
	e.overdrive = nil
	return true
}

func (e *ActiveSession) StopAll() {
	if e == nil {
		return
	}
	e.mu.Lock()
	tutorialAbility := e.tutorialAbility
	tutorialCreature := e.tutorialCreature
	support := e.support
	catalyst := e.catalyst
	overdrive := e.overdrive
	e.tutorialAbility = nil
	e.tutorialCreature = nil
	e.support = nil
	e.catalyst = nil
	e.overdrive = nil
	e.mu.Unlock()
	if tutorialAbility != nil {
		tutorialAbility.Stop()
	}
	if tutorialCreature != nil {
		tutorialCreature.Stop()
	}
	if support != nil {
		support.Stop()
	}
	if catalyst != nil {
		catalyst.Stop()
	}
	if overdrive != nil {
		overdrive.Stop()
	}
}
