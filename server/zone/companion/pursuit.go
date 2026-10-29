package companion

import (
	"time"

	"github.com/darkspinnet/darkspin/server/game"
)

// nextPursuitRevision is called with the session lock held. A session-wide
// sequence prevents a removed and reinserted object from aliasing an old plan.
func (e *Session) nextPursuitRevision() uint64 {
	e.pursuitRevision++
	if e.pursuitRevision == 0 {
		e.pursuitRevision++
	}
	return e.pursuitRevision
}

func (e *Actor) clearPursuit() {
	e.PursuitObjectID = 0
	e.PursuitPosition = game.Vec3{}
	e.PursuitRevision = 0
	e.pursuitSource = game.Vec3{}
	e.pursuitStartedAt = time.Time{}
	e.pursuitDeadline = time.Time{}
	e.isForcedPursuit = false
}
