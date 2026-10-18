package npc

import (
	"slices"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
)

type MerakGeyser struct {
	ObjectID     uint32
	BossObjectID uint32
	Position     game.Vec3
	ExpiresAt    uint64
}

func MerakGeyserProfile(nounName string) (ActionProfile, bool) {
	var duration time.Duration
	switch strings.ToLower(nounName) {
	case "cryosboss_2.noun":
		duration = 8 * time.Second
	case "cryosboss_3.noun":
		duration = 10 * time.Second
	default:
		return ActionProfile{}, false
	}
	// CryosBossPassive chunk 607 requests OnFire; chunk 926 defines its ticks.
	return ActionProfile{AbilityName: "CryosBossPassive", Radius: 2,
		ModifierName: "OnFire", ModifierDuration: duration, ModifierMaximumStack: 1,
		ModifierTickDuration: 2 * time.Second, ModifierMinimumTickDamage: 6,
		ModifierMaximumTickDamage: 12, ModifierTickDamageCoefficient: .05,
		ModifierDescriptorMask: 36, ModifierDamageType: 3, ModifierDamageSource: 1,
		TargetEffectName: "status_burning.ServerEventDef", IsModifierDamageProfileKnown: true}, true
}

func (e *Session) ClaimMerakController(objectID uint32) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.merakControllers[objectID] {
		return false
	}
	if e.merakControllers == nil {
		e.merakControllers = make(map[uint32]bool)
	}
	e.merakControllers[objectID] = true
	return true
}

func (e *Session) ReleaseMerakController(objectID uint32) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.merakControllers, objectID)
}

func (e *Session) PutMerakGeyser(geyser MerakGeyser) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.merakGeysers == nil {
		e.merakGeysers = make(map[uint32]MerakGeyser)
	}
	e.merakGeysers[geyser.ObjectID] = geyser
}

func (e *Session) RemoveMerakGeyser(objectID uint32) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.merakGeysers, objectID)
}

func (e *Session) MerakGeysers() []MerakGeyser {
	e.mu.RLock()
	defer e.mu.RUnlock()
	geysers := make([]MerakGeyser, 0, len(e.merakGeysers))
	for _, geyser := range e.merakGeysers {
		geysers = append(geysers, geyser)
	}
	slices.SortFunc(geysers, func(a, b MerakGeyser) int {
		if a.ObjectID < b.ObjectID {
			return -1
		}
		if a.ObjectID > b.ObjectID {
			return 1
		}
		return 0
	})
	return geysers
}
