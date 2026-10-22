package raknet103

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// Introduction restores a joining peer's remaining hold without replaying
// one-shot animation or effect messages to the zone.
func Introduction(snapshot zonenpc.Snapshot, now time.Time) ([][]byte, error) {
	if !snapshot.IsIntroductionPending() {
		return nil, nil
	}
	messages := []raknet.ApplicationMessage{raknet.AgentBlackboardUpdateMessage{
		ObjectID: snapshot.Plan.ObjectID, TargetID: 0,
		IsInCombat: false, IsTargetable: false, AttackerCount: 0,
	}}
	duration, radius := snapshot.IntroductionRemaining(now)
	if duration > 0 {
		messages = append(messages, raknet.NewCinematicMessage(duration, vector(snapshot.Plan.Position), radius))
	}
	packets, err := marshalMessages(messages, "introRestore")
	if err != nil {
		return nil, fmt.Errorf("introMarshal: %w", err)
	}
	return packets, nil
}
