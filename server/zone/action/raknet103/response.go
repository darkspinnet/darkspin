package raknet103

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
)

func Reject(command raknet.ActionCommandData) ([]byte, error) {
	if command.Common.ObjectID == 0 {
		return nil, errors.New("invalid campaign action rejection")
	}
	packet, err := raknet.MarshalApplication(raknet.ActionCommandResponseMessage{
		SyncStamp: command.Common.Unknown[0], ResponseType: 2,
		ObjectID: command.Common.ObjectID, UserData: 0xffffffff,
	})
	if err != nil {
		return nil, fmt.Errorf("actionRejectMarshal: %w", err)
	}
	return packet, nil
}

func Accept(
	command raknet.ActionCommandData, abilityName string, sourceTime uint64,
	commitDelay time.Duration, releaseDelay time.Duration,
) ([]byte, error) {
	if command.Common.ObjectID == 0 || abilityName == "" || commitDelay < 0 ||
		releaseDelay <= 0 || commitDelay > releaseDelay {
		return nil, errors.New("invalid campaign action acceptance")
	}
	packet, err := raknet.MarshalApplication(raknet.ActionCommandResponseMessage{
		SyncStamp:               command.Common.Unknown[0],
		ResponseType:            raknet.ActionResponseAccepted,
		ObjectID:                util.HashID(abilityName),
		SourceStartMilliseconds: sourceTime,
		SourceCommitMilliseconds: sourceTime +
			uint64(commitDelay/time.Millisecond),
		SourceEndMilliseconds: sourceTime +
			uint64(releaseDelay/time.Millisecond),
		UserData: 0xffffffff,
	})
	if err != nil {
		return nil, fmt.Errorf("actionAcceptMarshal: %w", err)
	}
	return packet, nil
}
