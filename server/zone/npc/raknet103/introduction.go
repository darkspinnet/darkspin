package raknet103

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// FirstAggroAnimation publishes the separate authored post-wait animation
// without replaying the entrance's camera, facing, effect or reveal stages.
func FirstAggroAnimation(plan zonenpc.FirstActionPlan, timestamp uint64) ([][]byte, error) {
	if plan.ObjectID == 0 || plan.Profile.FirstAggroAnimationDelay <= 0 ||
		plan.Profile.FirstAggroAnimationName == "" {
		return nil, errors.New("invalid delayed introduction animation")
	}
	packets, err := marshalMessages([]raknet.ApplicationMessage{
		raknet.SetAnimationStateMessage{
			ObjectID: plan.ObjectID, State: util.HashID(plan.Profile.FirstAggroAnimationName),
			Timestamp: timestamp, Scale: 1,
		},
	}, "introAnimation")
	if err != nil {
		return nil, fmt.Errorf("animationMarshal: %w", err)
	}
	return packets, nil
}

// Introduction restores a joining peer's remaining hold without replaying
// one-shot animation or effect messages to the zone.
func Introduction(snapshot zonenpc.Snapshot, now time.Time) ([][]byte, error) {
	secondDuration, secondFocus := snapshot.SecondAggroCinematic(now)
	if secondDuration > 0 {
		packets, err := marshalMessages([]raknet.ApplicationMessage{
			raknet.NewCinematicMessage(secondDuration, vector(secondFocus), zonenpc.SecondAggroCinematicRadius),
		}, "secondAggroRestore")
		if err != nil {
			return nil, fmt.Errorf("secondAggroMarshal: %w", err)
		}
		return packets, nil
	}
	// Ordinary entrances are already targetable in FirstAggro. Rejoining must
	// not invent a protected blackboard that their continuation never clears.
	if !snapshot.IsIntroductionCombatSuppressed() {
		return nil, nil
	}
	messages := []raknet.ApplicationMessage{raknet.AgentBlackboardUpdateMessage{
		ObjectID: snapshot.Plan.ObjectID, TargetID: 0,
		IsInCombat: false, IsTargetable: false, AttackerCount: 0,
	}}
	duration, radius := snapshot.IntroductionRemaining(now)
	if duration > 0 {
		messages = append(messages, raknet.NewCinematicMessage(duration, vector(snapshot.IntroductionFocus()), radius))
	}
	packets, err := marshalMessages(messages, "introRestore")
	if err != nil {
		return nil, fmt.Errorf("introMarshal: %w", err)
	}
	return packets, nil
}
