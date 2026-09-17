package raknet103

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
)

func ReleaseResponse(
	syncStamp uint8, abilityID uint32, abilityIndex uint32,
	sourceTime uint64, hitDelay time.Duration, releaseDelay time.Duration,
) ([]byte, error) {
	packet, err := raknet.MarshalApplication(raknet.ActionCommandResponseMessage{
		SyncStamp:                syncStamp,
		ResponseType:             raknet.ActionResponseReleased,
		ObjectID:                 abilityID,
		AbilityIndex:             abilityIndex,
		SourceStartMilliseconds:  sourceTime,
		SourceCommitMilliseconds: sourceTime + uint64(hitDelay/time.Millisecond),
		SourceEndMilliseconds:    sourceTime + uint64(releaseDelay/time.Millisecond),
		UserData:                 0xffffffff,
	})
	if err != nil {
		return nil, fmt.Errorf("releaseMarshal: %w", err)
	}
	return packet, nil
}
