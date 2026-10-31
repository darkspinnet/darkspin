package companion

import (
	"maps"

	"github.com/darkspinnet/darkspin/server/game"
)

// NewSessionWithAggroRanges binds noun-resolved class ranges once for every
// admission, including specialized pets and replacement/respawn paths.
func NewSessionWithAggroRanges(aggroRanges map[uint32]float32) *Session {
	session := NewSession()
	session.aggroRangesByNoun = maps.Clone(aggroRanges)
	return session
}

// IsInAcquisitionRange gates automatic attacks independently of ability reach.
// Match the NPC and owner-follow boundaries by including both actor footprints.
// Missing/zero class ranges retain the compatibility policy; their native
// acquisition semantics are not established by the Lua perception predicate.
func (e Actor) IsInAcquisitionRange(position game.Vec3, targetRadius float32) bool {
	if !isFinitePosition(position) || !isFiniteNonNegative(targetRadius) {
		return false
	}
	if e.AggroRadius <= 0 {
		return true
	}
	return e.Position.Sub(position).Length() <= e.acquisitionCenterRange(targetRadius)
}

func (e Actor) acquisitionCenterRange(targetRadius float32) float32 {
	return e.AcquisitionRange() + e.ActorFootprintRadius() + max(float32(0), targetRadius)
}
