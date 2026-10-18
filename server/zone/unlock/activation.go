package unlock

import (
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	zonecallback "github.com/darkspinnet/darkspin/server/zone/callback"
)

// HasBeatenThisLevel mirrors the native completion predicate, including the
// configured tutorial campaign bound rather than the executable's defaults.
func HasBeatenThisLevel(binding game.GameplayBinding, majorCount uint32, minorCount uint32) bool {
	stage := binding.ChainLevelIndex
	return stage != 0 && (binding.ChainProgression >= stage ||
		majorCount != 0 && minorCount != 0 && uint64(stage) > uint64(majorCount)*uint64(minorCount))
}

func IsTutorialActivationCallback(callback string) bool {
	return callback == zonecallback.SoloSupportUnlock || callback == zonecallback.CatalystUnlock || callback == zonecallback.OverdriveUnlock
}

func TutorialActivationDelay(callback string, isAnyUnbeaten bool) time.Duration {
	if callback == zonecallback.OverdriveUnlock {
		if isAnyUnbeaten {
			return OverdriveFinalDeadline
		}
		return 3 * time.Second
	}
	if callback == zonecallback.CatalystUnlock {
		if isAnyUnbeaten {
			return CatalystFinalDeadline
		}
		return 2 * time.Second
	}
	if isAnyUnbeaten {
		return 15 * time.Second
	}
	return 2 * time.Second
}

func TutorialMutationDelay(callback string) time.Duration {
	if callback == zonecallback.OverdriveUnlock {
		return OverdriveMutationDeadline
	}
	return SupportMutationDeadline
}

// ActivationSession reserves each authored trigger once for the whole party.
// Zone teardown owns its timeline cancellation; membership generations fence
// mutations and the triggering player's controlled hero.
type ActivationSession struct {
	mu     sync.Mutex
	claims map[string]struct{}
}

func NewActivationSession() *ActivationSession {
	return &ActivationSession{claims: make(map[string]struct{})}
}

func (e *ActivationSession) Claim(key string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, isFound := e.claims[key]; isFound {
		return false
	}
	e.claims[key] = struct{}{}
	return true
}

func (e *ActivationSession) Release(key string) {
	e.mu.Lock()
	delete(e.claims, key)
	e.mu.Unlock()
}
