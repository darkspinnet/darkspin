package npc

import "github.com/darkspinnet/darkspin/server/game"

// IntroductionFocus preserves the authored camera offset independently of the
// actor's movement and animation position.
func (e FirstActionPlan) IntroductionFocus() game.Vec3 {
	return e.SourcePosition.Add(e.Profile.FirstAggroCinematicOffset)
}

func (e Snapshot) IntroductionFocus() game.Vec3 {
	return e.introduction.sourcePosition.Add(e.introduction.profile.FirstAggroCinematicOffset)
}
