package raknet103

import (
	"context"
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneobjective "github.com/darkspinnet/darkspin/server/zone/objective"
)

const obeliskAccessedVoiceName = "vo_ship_obelisk_accessed"

func ApplyPackets(
	ctx context.Context,
	state *sim.ObjectiveState,
	runtimes map[uint32]*sim.LuaObjectiveRuntime,
	objectiveID uint32,
	event sim.LuaObjectiveEvent,
) ([][]byte, error) {
	update, err := zoneobjective.ApplyState(
		ctx, state, runtimes, objectiveID, event,
	)
	if err != nil {
		return nil, fmt.Errorf("objectiveApply: %w", err)
	}
	packet, err := UpdatePackets(update)
	if err != nil {
		return nil, fmt.Errorf("objectiveProject: %w", err)
	}
	return packet, nil
}

func ApplySessionPackets(
	ctx context.Context,
	session *zoneobjective.Session,
	objectiveID uint32,
	event sim.LuaObjectiveEvent,
) ([][]byte, error) {
	if session == nil {
		return nil, fmt.Errorf("objectiveSession: unavailable")
	}
	update, err := session.Apply(ctx, objectiveID, event)
	if err != nil {
		return nil, fmt.Errorf("objectiveApply: %w", err)
	}
	packet, err := UpdatePackets(update)
	if err != nil {
		return nil, fmt.Errorf("objectiveProject: %w", err)
	}
	return packet, nil
}

func UpdatePackets(update []zoneobjective.Update) ([][]byte, error) {
	packet := make([][]byte, 0, len(update))
	for index, current := range update {
		voiceover := uint32(0)
		if current.IsObeliskAccessed {
			voiceover = util.HashID(obeliskAccessedVoiceName)
		}
		encoded, err := raknet.MarshalApplication(raknet.ObjectiveUpdatedMessage{
			ObjectiveID: current.ObjectiveID,
			PlayerIndex: current.PlayerIndex,
			Medal:       current.Medal,
			Voiceover:   voiceover,
			Token:       current.Token,
		})
		if err != nil {
			return nil, fmt.Errorf("updateMarshal[%d]: %w", index, err)
		}
		packet = append(packet, encoded)
	}
	return packet, nil
}

func Messages(
	initialization zoneobjective.Initialization,
) []raknet.ApplicationMessage {
	record := make([]raknet.ObjectiveRecord, len(initialization.Records))
	for index, current := range initialization.Records {
		record[index] = raknet.ObjectiveRecord{
			ObjectiveID: current.ObjectiveID,
			State:       current.State,
			Token:       current.Token,
		}
	}
	return raknet.ObjectiveInitializationMessages(
		record,
		raknet.ObjectiveUpdatedMessage{
			ObjectiveID: initialization.Update.ObjectiveID,
			PlayerIndex: initialization.Update.PlayerIndex,
			Medal:       initialization.Update.Medal,
			Token:       initialization.Update.Token,
		},
	)
}
