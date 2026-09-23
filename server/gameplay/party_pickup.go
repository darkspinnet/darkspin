package gameplay

import (
	"fmt"
	"sort"

	"github.com/darkspinnet/darkspin/server/game"
	heroraknet "github.com/darkspinnet/darkspin/server/zone/hero/raknet103"
)

type alliedZoneSquadRestoration struct {
	sessionKey       string
	healing          []zoneSquadHealing
	manaRestorations []zoneSquadManaRestoration
}

func isActiveCoopPickupAlly(
	candidate gameplayPeerSession, source gameplayPeerSession,
) bool {
	return source.binding.Mode == game.ModeChain &&
		candidate.binding.Mode == game.ModeChain &&
		candidate.binding.GameID == source.binding.GameID &&
		candidate.binding.UserID != source.binding.UserID &&
		candidate.zone == source.zone && candidate.squad != nil &&
		candidate.stage.IsDungeon() && !candidate.isRejoinPending
}

func (e *gameplaySessionRegistry) restoreAlliedZoneSquadsLocked(
	source gameplayPeerSession, kind zoneOrbKind, restoreFraction float32,
) ([]alliedZoneSquadRestoration, [][]byte, [][]byte, error) {
	if e == nil || source.zone == nil || restoreFraction <= 0 {
		return nil, nil, nil, nil
	}
	sessionKeys := make([]string, 0, len(e.sessions))
	for sessionKey, candidate := range e.sessions {
		if isActiveCoopPickupAlly(candidate, source) {
			sessionKeys = append(sessionKeys, sessionKey)
		}
	}
	sort.Strings(sessionKeys)
	restorations := make([]alliedZoneSquadRestoration, 0, len(sessionKeys))
	resourcePackets := make([][]byte, 0, len(sessionKeys)*3)
	worldPackets := make([][]byte, 0, len(sessionKeys))
	for _, sessionKey := range sessionKeys {
		candidate := e.sessions[sessionKey]
		restoration := alliedZoneSquadRestoration{sessionKey: sessionKey}
		var err error
		if kind == zoneHealthOrb {
			restoration.healing, err =
				candidate.healLivingZoneSquadByMaximum(restoreFraction)
		}
		if kind == zoneManaOrb {
			restoration.manaRestorations, err =
				candidate.restoreLivingZoneSquadManaByMaximum(restoreFraction)
		}
		if err != nil {
			e.rollbackAlliedZoneSquadRestorationsLocked(restorations)
			return nil, nil, nil, fmt.Errorf(
				"allyRestore[%d]: %w", candidate.binding.UserID, err,
			)
		}
		for _, healedCharacter := range restoration.healing {
			packet, marshalErr := candidate.marshalCampaignCharacterResource(
				healedCharacter.creatureIndex,
			)
			if marshalErr != nil {
				candidate.rollbackZoneSquadHealing(restoration.healing)
				e.rollbackAlliedZoneSquadRestorationsLocked(restorations)
				return nil, nil, nil, fmt.Errorf(
					"allyHealthMarshal[%d]: %w", candidate.binding.UserID, marshalErr,
				)
			}
			resourcePackets = append(resourcePackets, packet)
			if healedCharacter.objectID == candidate.deployedObjectID {
				worldPacket, worldErr := heroraknet.HitPoint(
					healedCharacter.objectID, healedCharacter.hitPoint,
				)
				if worldErr != nil {
					candidate.rollbackZoneSquadHealing(restoration.healing)
					e.rollbackAlliedZoneSquadRestorationsLocked(restorations)
					return nil, nil, nil, fmt.Errorf(
						"allyHealthWorld[%d]: %w", candidate.binding.UserID, worldErr,
					)
				}
				worldPackets = append(worldPackets, worldPacket)
			}
		}
		for _, manaRestoration := range restoration.manaRestorations {
			packet, marshalErr := candidate.marshalCampaignCharacterResource(
				manaRestoration.creatureIndex,
			)
			if marshalErr != nil {
				candidate.rollbackZoneSquadManaRestoration(
					restoration.manaRestorations,
				)
				e.rollbackAlliedZoneSquadRestorationsLocked(restorations)
				return nil, nil, nil, fmt.Errorf(
					"allyManaMarshal[%d]: %w", candidate.binding.UserID, marshalErr,
				)
			}
			resourcePackets = append(resourcePackets, packet)
			if manaRestoration.objectID == candidate.deployedObjectID {
				worldPacket, worldErr := heroraknet.ManaPoint(
					manaRestoration.objectID, manaRestoration.manaPoint,
				)
				if worldErr != nil {
					candidate.rollbackZoneSquadManaRestoration(
						restoration.manaRestorations,
					)
					e.rollbackAlliedZoneSquadRestorationsLocked(restorations)
					return nil, nil, nil, fmt.Errorf(
						"allyManaWorld[%d]: %w", candidate.binding.UserID, worldErr,
					)
				}
				worldPackets = append(worldPackets, worldPacket)
			}
		}
		if len(restoration.healing) == 0 &&
			len(restoration.manaRestorations) == 0 {
			continue
		}
		e.sessions[sessionKey] = candidate
		restorations = append(restorations, restoration)
	}
	return restorations, resourcePackets, worldPackets, nil
}

func (e *gameplaySessionRegistry) rollbackAlliedZoneSquadRestorationsLocked(
	restorations []alliedZoneSquadRestoration,
) {
	if e == nil {
		return
	}
	for index := len(restorations) - 1; index >= 0; index-- {
		restoration := restorations[index]
		candidate, isFound := e.sessions[restoration.sessionKey]
		if !isFound {
			continue
		}
		candidate.rollbackZoneSquadHealing(restoration.healing)
		candidate.rollbackZoneSquadManaRestoration(restoration.manaRestorations)
		e.sessions[restoration.sessionKey] = candidate
	}
}

func (e *gameplaySessionRegistry) publishAlliedPickupResourcesLocked(
	source gameplayPeerSession, packets [][]byte,
) {
	if e == nil || len(packets) == 0 {
		return
	}
	for sessionKey, candidate := range e.sessions {
		if !isActiveCoopPickupAlly(candidate, source) {
			continue
		}
		err := candidate.publishPackets(packets)
		if err != nil && e.logger != nil {
			e.logger.Printf(
				"RakNet co-op capsule resource delivery queued game=%d user=%d: %v",
				candidate.binding.GameID, candidate.binding.UserID, err,
			)
		}
		e.sessions[sessionKey] = candidate
	}
}
