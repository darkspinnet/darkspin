package raknet103

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneobject "github.com/darkspinnet/darkspin/server/zone/object"
)

func ScriptUse(use game.CampaignScriptUse) ([][]byte, error) {
	packets, err := scriptUse(use, false)
	if err != nil {
		return nil, fmt.Errorf("scriptUse: %w", err)
	}
	return packets, nil
}

// ScriptUseSnapshot restores consumed state without replaying the use effect.
func ScriptUseSnapshot(use game.CampaignScriptUse) ([][]byte, error) {
	packets, err := scriptUse(use, true)
	if err != nil {
		return nil, fmt.Errorf("scriptSnapshot: %w", err)
	}
	return packets, nil
}

func scriptUse(use game.CampaignScriptUse, isSnapshot bool) ([][]byte, error) {
	publication, err := zoneobject.PublishScriptUse(use)
	if err != nil {
		return nil, fmt.Errorf("scriptUsePublication: %w", err)
	}
	dataPacket, err := raknet.MarshalApplication(raknet.InteractableDataUpdateMessage{
		ObjectID: publication.ObjectID, TimesUsed: publication.UseCount,
		UsesAllowed: publication.UseLimit,
		Ability:     util.HashID(publication.AbilityName),
	})
	if err != nil {
		return nil, fmt.Errorf("scriptUseDataMarshal: %w", err)
	}
	statePacket, err := raknet.MarshalApplication(raknet.ObjectInteractableStateMessage{
		ObjectID: publication.ObjectID, State: publication.State,
		MarkerID: publication.MarkerID,
	})
	if err != nil {
		return nil, fmt.Errorf("scriptUseStateMarshal: %w", err)
	}
	packets := [][]byte{dataPacket, statePacket}
	if isSnapshot {
		// Restored uses represent the completed interaction, including the
		// authored final RemovePhysicsForObject call.
		collisionPacket, collisionErr := raknet.MarshalApplication(raknet.ObjectCollisionUpdateMessage{
			ObjectID: publication.ObjectID, IsCollisionEnabled: false,
		})
		if collisionErr != nil {
			return nil, fmt.Errorf("scriptSnapshotCollision: %w", collisionErr)
		}
		packets = append(packets, collisionPacket)
	}
	effectPackets, err := obeliskEffects(publication.ObjectID, publication.AbilityName, false, !isSnapshot)
	if err != nil {
		return nil, fmt.Errorf("scriptUseEffect: %w", err)
	}
	return append(packets, effectPackets...), nil
}

func Script(plan zoneobject.ScriptPlan) ([][]byte, error) {
	publication, err := zoneobject.PublishScript(plan)
	if err != nil {
		return nil, fmt.Errorf("scriptPublication: %w", err)
	}
	createPacket, err := raknet.MarshalApplication(raknet.EnemyObjectCreateMessage{
		ObjectID: publication.ObjectID,
		Noun:     util.HashID(publication.NounName),
		Position: raknet.Vector3{
			X: publication.Position.X, Y: publication.Position.Y,
			Z: publication.Position.Z,
		},
		Rotation: raknet.Vector3{
			X: publication.Rotation.X, Y: publication.Rotation.Y,
			Z: publication.Rotation.Z,
		},
		Scale: publication.Scale, IsCollidable: publication.IsCollisionEnabled,
	})
	if err != nil {
		return nil, fmt.Errorf("scriptCreateMarshal: %w", err)
	}
	updatePacket, err := raknet.MarshalApplication(raknet.ObjectUpdateMessage{
		ObjectID: publication.ObjectID, PositionX: publication.Position.X,
		PositionY: publication.Position.Y, PositionZ: publication.Position.Z,
		IsVisible: publication.IsVisible,
	})
	if err != nil {
		return nil, fmt.Errorf("scriptUpdateMarshal: %w", err)
	}
	packets := [][]byte{createPacket, updatePacket}
	if publication.AbilityName == "" {
		return packets, nil
	}
	dataPacket, err := raknet.MarshalApplication(raknet.InteractableDataUpdateMessage{
		ObjectID: publication.ObjectID, UsesAllowed: publication.UseLimit,
		Ability: util.HashID(publication.AbilityName),
	})
	if err != nil {
		return nil, fmt.Errorf("scriptDataMarshal: %w", err)
	}
	statePacket, err := raknet.MarshalApplication(raknet.ObjectInteractableStateMessage{
		ObjectID: publication.ObjectID, State: 3,
		MarkerID: publication.MarkerID,
	})
	if err != nil {
		return nil, fmt.Errorf("scriptStateMarshal: %w", err)
	}
	packets = append(packets, dataPacket, statePacket)
	if !publication.IsVisible {
		return packets, nil
	}
	effectPackets, err := obeliskEffects(publication.ObjectID, publication.AbilityName, true, false)
	if err != nil {
		return nil, fmt.Errorf("scriptEffect: %w", err)
	}
	return append(packets, effectPackets...), nil
}
