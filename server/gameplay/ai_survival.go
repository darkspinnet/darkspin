package gameplay

import "time"

const playerAIDamageWindow = 4 * time.Second

type playerAIDamageSample struct {
	at          time.Time
	healthRatio float32
}

// Each deployed hero owns a fresh pressure window. Sampling before action
// readiness keeps damage visible while the AI is occupied with a cast.
type playerAISurvival struct {
	objectID      uint32
	lastHitPoint  float32
	nextRollAt    time.Time
	damageSamples []playerAIDamageSample
}

func (e *playerAISurvival) observe(member gameplayPeerSession, now time.Time) {
	hitPoint := member.deployedHitPoint()
	if e.objectID != member.deployedObjectID {
		*e = playerAISurvival{objectID: member.deployedObjectID, lastHitPoint: hitPoint}
		return
	}
	maximum := member.characterHitPointMaximum(member.deployedCreatureIndex)
	if maximum > 0 && hitPoint < e.lastHitPoint {
		e.damageSamples = append(e.damageSamples, playerAIDamageSample{
			at: now, healthRatio: (e.lastHitPoint - hitPoint) / maximum})
	}
	e.lastHitPoint = hitPoint
	first := 0
	for first < len(e.damageSamples) && now.Sub(e.damageSamples[first].at) >= playerAIDamageWindow {
		first++
	}
	if first > 0 {
		copy(e.damageSamples, e.damageSamples[first:])
		e.damageSamples = e.damageSamples[:len(e.damageSamples)-first]
	}
}

func (e playerAISurvival) recentLoss(now time.Time) float32 {
	loss := float32(0)
	for _, sample := range e.damageSamples {
		if now.Sub(sample.at) < playerAIDamageWindow {
			loss += sample.healthRatio
		}
	}
	return loss
}
