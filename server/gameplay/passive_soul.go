package gameplay

import (
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
)

const (
	soulRavagerSlotCount  = 5
	soulRavagerRadius     = float32(1)
	soulRavagerHeight     = float32(0.5)
	soulRavagerEffectSlot = uint8(1)
)

const (
	soulRavagerNoun        = "TrappedSoul.Noun"
	soulRavagerEmptyEffect = "trapped_soul_slot.ServerEventDef"
	soulRavagerFullEffect  = "trapped_soul.ServerEventDef"
)

func startSoulRavagerPresentation(
	runtime campaignDamageRuntime, sessionKey string, generation uint64,
) ([][]byte, error) {
	if runtime.registry == nil || sessionKey == "" || generation == 0 {
		return nil, errors.New("soul ravager presentation unavailable")
	}
	runtime.registry.mutex.Lock()
	defer runtime.registry.mutex.Unlock()
	peerSession, isFound := runtime.registry.sessions[sessionKey]
	if !isFound || peerSession.generation != generation {
		return nil, nil
	}
	packets, err := peerSession.syncSoulRavagerPresentation()
	if err != nil {
		return nil, fmt.Errorf("soulPresentationSync: %w", err)
	}
	runtime.registry.sessions[sessionKey] = peerSession
	return packets, nil
}

func (s *gameplayPeerSession) syncSoulRavagerPresentation() ([][]byte, error) {
	if s == nil {
		return nil, nil
	}
	isActive := s.deployedObjectID != 0 &&
		s.deployedCreatureIndex < uint32(len(s.binding.Creatures)) &&
		s.binding.Creatures[s.deployedCreatureIndex].PassiveAbility ==
			util.HashID("SoulRavagerPassive")
	if !isActive || (s.soulRavagerOwnerObjectID != 0 &&
		s.soulRavagerOwnerObjectID != s.deployedObjectID) {
		packets, err := s.clearSoulRavagerPresentation()
		if err != nil {
			return nil, fmt.Errorf("soulPresentationClear: %w", err)
		}
		if !isActive {
			return packets, nil
		}
		spawnPackets, err := s.createSoulRavagerPresentation()
		if err != nil {
			return nil, fmt.Errorf("soulPresentationCreate: %w", err)
		}
		return append(packets, spawnPackets...), nil
	}
	if s.soulRavagerOwnerObjectID == 0 {
		packets, err := s.createSoulRavagerPresentation()
		if err != nil {
			return nil, fmt.Errorf("soulPresentationCreate: %w", err)
		}
		return packets, nil
	}
	stack := min(
		uint32(soulRavagerSlotCount),
		s.passiveKillStack[s.deployedCreatureIndex],
	)
	if stack == s.soulRavagerVisualStack {
		return nil, nil
	}
	packets := make([][]byte, 0, soulRavagerSlotCount*2)
	firstChanged := min(stack, s.soulRavagerVisualStack)
	lastChanged := max(stack, s.soulRavagerVisualStack)
	for index := firstChanged; index < lastChanged; index++ {
		objectID := s.soulRavagerObjectIDs[index]
		if objectID == 0 {
			continue
		}
		removePacket, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
			Slot: soulRavagerEffectSlot, ObjectID: objectID,
			IsRemovalRequested: true, IsHardStop: true,
		})
		if err != nil {
			return nil, fmt.Errorf("soulEffectRemove[%d]: %w", index, err)
		}
		effectName := soulRavagerEmptyEffect
		if index < stack {
			effectName = soulRavagerFullEffect
		}
		attachPacket, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
			Slot: soulRavagerEffectSlot, ObjectID: objectID,
			Asset: util.HashID(effectName), IsForceAttached: true,
		})
		if err != nil {
			return nil, fmt.Errorf("soulEffectAttach[%d]: %w", index, err)
		}
		packets = append(packets, removePacket, attachPacket)
	}
	s.soulRavagerVisualStack = stack
	return packets, nil
}

func (s *gameplayPeerSession) createSoulRavagerPresentation() ([][]byte, error) {
	if s == nil || s.zone == nil || s.deployedObjectID == 0 ||
		s.deployedCreatureIndex >= uint32(len(s.passiveKillStack)) {
		return nil, errors.New("soul ravager owner unavailable")
	}
	firstObjectID, err := s.zone.ReserveObjectIDs(soulRavagerSlotCount)
	if err != nil {
		return nil, fmt.Errorf("soulObjectReserve: %w", err)
	}
	stack := min(
		uint32(soulRavagerSlotCount),
		s.passiveKillStack[s.deployedCreatureIndex],
	)
	packets := make([][]byte, 0, soulRavagerSlotCount*2)
	objectIDs := [soulRavagerSlotCount]uint32{}
	for index := range soulRavagerSlotCount {
		objectID := firstObjectID + uint32(index)
		objectIDs[index] = objectID
		angle := 2 * math.Pi * float64(index) / soulRavagerSlotCount
		positionX := s.playerPosition.X + float32(math.Cos(angle))*soulRavagerRadius
		positionY := s.playerPosition.Y + float32(math.Sin(angle))*soulRavagerRadius
		positionZ := s.playerPosition.Z + soulRavagerHeight
		createPacket, marshalErr := raknet.MarshalApplication(raknet.ObjectCreateMessage{
			ObjectID: objectID, Noun: util.HashID(soulRavagerNoun),
			PositionX: positionX, PositionY: positionY, PositionZ: positionZ,
			Scale: 1, Team: s.soulRavagerTeam(), OwnerID: s.deployedObjectID,
			IsCollisionEnabled: false,
		})
		if marshalErr != nil {
			return nil, fmt.Errorf("soulObjectCreate[%d]: %w", index, marshalErr)
		}
		effectName := soulRavagerEmptyEffect
		if uint32(index) < stack {
			effectName = soulRavagerFullEffect
		}
		effectPacket, marshalErr := raknet.MarshalApplication(
			raknet.AttachedEffectMessage{
				Slot: soulRavagerEffectSlot, ObjectID: objectID,
				Asset: util.HashID(effectName), IsForceAttached: true,
			},
		)
		if marshalErr != nil {
			return nil, fmt.Errorf("soulObjectEffect[%d]: %w", index, marshalErr)
		}
		packets = append(packets, createPacket, effectPacket)
	}
	s.soulRavagerObjectIDs = objectIDs
	s.soulRavagerOwnerObjectID = s.deployedObjectID
	s.soulRavagerVisualStack = stack
	return packets, nil
}

func (s *gameplayPeerSession) clearSoulRavagerPresentation() ([][]byte, error) {
	if s == nil || s.soulRavagerOwnerObjectID == 0 {
		return nil, nil
	}
	objectIDs := make([]uint32, 0, soulRavagerSlotCount)
	for _, objectID := range s.soulRavagerObjectIDs {
		if objectID != 0 {
			objectIDs = append(objectIDs, objectID)
		}
	}
	s.soulRavagerObjectIDs = [soulRavagerSlotCount]uint32{}
	s.soulRavagerOwnerObjectID = 0
	s.soulRavagerVisualStack = 0
	if len(objectIDs) == 0 {
		return nil, nil
	}
	packet, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{
		ObjectID: objectIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("soulObjectDelete: %w", err)
	}
	return [][]byte{packet}, nil
}

func (s gameplayPeerSession) marshalSoulRavagerPresentation() ([][]byte, error) {
	if s.soulRavagerOwnerObjectID == 0 ||
		s.soulRavagerOwnerObjectID != s.deployedObjectID {
		return nil, nil
	}
	packets := make([][]byte, 0, soulRavagerSlotCount*2)
	for index, objectID := range s.soulRavagerObjectIDs {
		if objectID == 0 {
			continue
		}
		angle := 2 * math.Pi * float64(index) / soulRavagerSlotCount
		positionX := s.playerPosition.X + float32(math.Cos(angle))*soulRavagerRadius
		positionY := s.playerPosition.Y + float32(math.Sin(angle))*soulRavagerRadius
		positionZ := s.playerPosition.Z + soulRavagerHeight
		createPacket, err := raknet.MarshalApplication(raknet.ObjectCreateMessage{
			ObjectID: objectID, Noun: util.HashID(soulRavagerNoun),
			PositionX: positionX, PositionY: positionY, PositionZ: positionZ,
			Scale: 1, Team: s.soulRavagerTeam(), OwnerID: s.deployedObjectID,
			IsCollisionEnabled: false,
		})
		if err != nil {
			return nil, fmt.Errorf("soulRejoinCreate[%d]: %w", index, err)
		}
		effectName := soulRavagerEmptyEffect
		if uint32(index) < s.soulRavagerVisualStack {
			effectName = soulRavagerFullEffect
		}
		effectPacket, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
			Slot: soulRavagerEffectSlot, ObjectID: objectID,
			Asset: util.HashID(effectName), IsForceAttached: true,
		})
		if err != nil {
			return nil, fmt.Errorf("soulRejoinEffect[%d]: %w", index, err)
		}
		packets = append(packets, createPacket, effectPacket)
	}
	return packets, nil
}

func (s gameplayPeerSession) soulRavagerTeam() uint8 {
	team := uint8(s.binding.Team)
	if team == 0 {
		return 1
	}
	return team
}
