package gameplay

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
)

func (e *gameplayPeerSession) spawnHealthObeliskCapsules(
	req game.CampaignScriptInvocation, sourceTime uint64, now time.Time,
) ([][]byte, []uint32, error) {
	if e == nil || e.zone == nil || e.zone.DropRandom() == nil {
		return nil, nil, errors.New("obelisk drop runtime unavailable")
	}
	count := 4 + int(e.zone.DropRandom().Uint32()%2)
	packets := make([][]byte, 0, count*3)
	objectIDs := make([]uint32, 0, count)
	phase := e.zone.DropRandom().Float64() * 2 * math.Pi
	for index := 0; index < count; index++ {
		invocation := req
		invocation.Challenge = 100
		angle := phase + float64(index)*2*math.Pi/float64(count)
		invocation.Position.X += float32(math.Cos(angle)) * 1.5
		invocation.Position.Y += float32(math.Sin(angle)) * 1.5
		orbPackets, objectID, err := e.spawnCampaignHealthOrb(invocation, sourceTime, now)
		if err != nil {
			for _, previousID := range objectIDs {
				isPickupRemoved := e.zone.Pickups().Remove(previousID)
				isOrbRemoved := e.zone.Orbs().Remove(previousID)
				if !isPickupRemoved || !isOrbRemoved {
					err = errors.Join(err, fmt.Errorf("capsuleRollback[%d]: registration missing", previousID))
				}
			}
			return nil, nil, fmt.Errorf("obeliskCapsule[%d]: %w", index, err)
		}
		if objectID != 0 {
			objectIDs = append(objectIDs, objectID)
			packets = append(packets, orbPackets...)
		}
	}
	return packets, objectIDs, nil
}
