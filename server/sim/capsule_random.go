package sim

import (
	"errors"
	"sync"
)

// CapsuleRandom reproduces the AEAB30 floating draw used by the native
// capsule selector. Its zone ownership and run-derived seed are server policy;
// they do not reproduce the client's process-global consumer interleaving.
type CapsuleRandom struct {
	mu    sync.Mutex
	state uint32
	draw  uint64
}

type CapsuleRandomSnapshot struct {
	State     uint32
	DrawCount uint64
}

func NewCapsuleRandom(seed uint32) *CapsuleRandom {
	// AEAAE0 normalizes an explicit zero seed. Unlike native startup, the
	// server never takes the nondeterministic RDTSC initialization branch.
	if seed == 0 {
		seed = 0xaaaaaaaa
	}
	return &CapsuleRandom{state: seed}
}

func NewCapsuleRandomFromSnapshot(
	snapshot CapsuleRandomSnapshot,
) (*CapsuleRandom, error) {
	if snapshot.State == 0 {
		return nil, errors.New("capsule random state unavailable")
	}
	return &CapsuleRandom{
		state: snapshot.State, draw: snapshot.DrawCount,
	}, nil
}

func (e *CapsuleRandom) Float64() float64 {
	if e == nil {
		return 0
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.state *= 663608941
	e.draw++
	unit := float64(int32(e.state))*(1.0/4294967296.0) + 0.5
	if unit >= 1 {
		return 0
	}
	return unit
}

func (e *CapsuleRandom) Snapshot() CapsuleRandomSnapshot {
	if e == nil {
		return CapsuleRandomSnapshot{}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return CapsuleRandomSnapshot{State: e.state, DrawCount: e.draw}
}
