package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/zone"
)

// The caller pins the member with the registry lock. Sample a copy of retained
// movement so volume checks do not consume command-driven trigger intervals.
func (e gameplayPeerSession) sampleNPCTarget(target zone.NPCTarget, at time.Time) (zone.NPCTarget, error) {
	if !target.IsHero || target.UserID != e.binding.UserID || target.PeerGeneration != e.generation ||
		target.ObjectID != e.deployedObjectID || e.playerMotion == nil {
		return target, nil
	}
	position, err := e.playerMotion.SamplePosition(at)
	if err != nil {
		return target, fmt.Errorf("targetMotion: %w", err)
	}
	target.Position = game.Vec3(position)
	return target, nil
}

func (e *gameplaySessionRegistry) sampleNPCTargetsLocked(world *zone.Zone, targets []zone.NPCTarget, at time.Time) ([]zone.NPCTarget, error) {
	samples := append([]zone.NPCTarget(nil), targets...)
	for index, target := range samples {
		if !target.IsHero {
			continue
		}
		for _, member := range e.sessions {
			if member.zone != world || member.binding.UserID != target.UserID ||
				member.generation != target.PeerGeneration || member.deployedObjectID != target.ObjectID {
				continue
			}
			sampled, err := member.sampleNPCTarget(target, at)
			if err != nil {
				return nil, fmt.Errorf("targetSample[%d]: %w", index, err)
			}
			samples[index] = sampled
			break
		}
	}
	return samples, nil
}
