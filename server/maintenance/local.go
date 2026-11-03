package maintenance

import (
	"context"
	"fmt"
)

// LocalTransition serializes trusted console and scheduled operations with
// gameplay entry. The authenticated remote adapter already owns that gate.
type LocalTransition interface {
	AnnounceLocal(context.Context, int) (State, error)
	RequestLocal(context.Context, Request) (State, error)
	ActivateLocal(context.Context, string) error
	CancelLocal(context.Context, string) (State, error)
	AbortLocal(context.Context, string) error
}

func (e *Control) announceLocal(ctx context.Context, minutes int) (State, error) {
	transition := e.transition()
	var state State
	var err error
	if transition != nil {
		state, err = transition.AnnounceLocal(ctx, minutes)
	} else {
		state, err = e.Announce(ctx, minutes, "")
	}
	if err != nil {
		return state, fmt.Errorf("localAnnounce: %w", err)
	}
	return state, nil
}

func (e *Control) ConfigureLocalTransition(transition LocalTransition) {
	e.announcement.mu.Lock()
	e.localTransition = transition
	e.announcement.mu.Unlock()
}

func (e *Control) transition() LocalTransition {
	e.announcement.mu.Lock()
	defer e.announcement.mu.Unlock()
	return e.localTransition
}

func (e *Control) requestLocal(ctx context.Context, req Request) (State, error) {
	transition := e.transition()
	var state State
	var err error
	if transition != nil {
		state, err = transition.RequestLocal(ctx, req)
	} else {
		state, err = e.Request(ctx, req)
	}
	if err != nil {
		return state, fmt.Errorf("localRequest: %w", err)
	}
	return state, nil
}

func (e *Control) activateLocal(ctx context.Context, id string) error {
	transition := e.transition()
	var err error
	if transition != nil {
		err = transition.ActivateLocal(ctx, id)
	} else {
		err = e.Activate(id)
	}
	if err != nil {
		return fmt.Errorf("localActivate: %w", err)
	}
	return nil
}

func (e *Control) cancelLocal(ctx context.Context, id string) (State, error) {
	transition := e.transition()
	var state State
	var err error
	if transition != nil {
		state, err = transition.CancelLocal(ctx, id)
	} else {
		state, err = e.Cancel(ctx, id)
	}
	if err != nil {
		return state, fmt.Errorf("localCancel: %w", err)
	}
	return state, nil
}

func (e *Control) abortLocal(ctx context.Context, id string) error {
	transition := e.transition()
	var err error
	if transition != nil {
		err = transition.AbortLocal(ctx, id)
	} else {
		err = e.Abort(id)
	}
	if err != nil {
		return fmt.Errorf("localAbort: %w", err)
	}
	return nil
}
