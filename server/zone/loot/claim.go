package loot

type claimState uint8

const (
	claimUnavailable claimState = iota
	claimPrepared
	claimPublished
	claimReserved
	claimCollected
)

// Drop owns the reservation transaction for the tutorial's one-time opening
// loot. Its payload is opaque so persistence and presentation remain adapter
// responsibilities.
type Drop[T any] struct {
	payload T
	state   claimState
}

func NewDrop[T any](payload T) Drop[T] {
	return Drop[T]{payload: payload, state: claimPrepared}
}

func (e Drop[T]) Payload() T {
	return e.payload
}

func (e *Drop[T]) Publish() bool {
	if e == nil || e.state != claimPrepared {
		return false
	}
	e.state = claimPublished
	return true
}

func (e *Drop[T]) Reserve() bool {
	if e == nil || e.state != claimPublished {
		return false
	}
	e.state = claimReserved
	return true
}

func (e *Drop[T]) Rollback() bool {
	if e == nil || e.state != claimReserved {
		return false
	}
	e.state = claimPublished
	return true
}

func (e *Drop[T]) Collect() bool {
	if e == nil || e.state != claimReserved {
		return false
	}
	e.state = claimCollected
	return true
}

func (e Drop[T]) IsAvailable() bool {
	return e.state == claimPublished
}

func (e Drop[T]) IsReserved() bool {
	return e.state == claimReserved
}

func (e *Drop[T]) Reset() {
	if e == nil {
		return
	}
	*e = Drop[T]{}
}
