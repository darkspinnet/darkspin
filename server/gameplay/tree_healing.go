package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

// Called with the registry locked. Resolve each resource through its owning
// session: global object IDs are not local squad indexes in multiplayer.
func (e treeOfLifeSchedule) healPartyLocked(
	caster *gameplayPeerSession, pulse sim.AreaPulseIntent, amount float32,
) ([]zoneSquadHealing, [][]byte, error) {
	healings := make([]zoneSquadHealing, 0)
	packets := make([][]byte, 0)
	for sessionKey, candidate := range e.runtime.registry.sessions {
		if candidate.binding.GameID != caster.binding.GameID || candidate.zone != caster.zone ||
			candidate.binding.Team != caster.binding.Team || candidate.squad == nil ||
			!candidate.stage.IsDungeon() || candidate.isRejoinPending {
			continue
		}
		if sessionKey == e.sessionKey {
			candidate = *caster
		}
		memberHealings := make([]zoneSquadHealing, 0)
		if isInsideZoneTrigger(candidate.playerPosition, raknet.Vector3(pulse.Position), pulse.Radius) {
			var err error
			memberHealings, err = candidate.healLivingZoneSquad(amount)
			if err != nil {
				return nil, nil, fmt.Errorf("treeSquad: %w", err)
			}
		}
		companionHealings, err := candidate.healLivingZoneCompanions(amount, raknet.Vector3(pulse.Position), pulse.Radius)
		if err != nil {
			return nil, nil, fmt.Errorf("treeCompanions: %w", err)
		}
		memberHealings = append(memberHealings, companionHealings...)
		received := sporenet.PlayerStatDelta{}
		for _, healed := range memberHealings {
			var packet []byte
			if healed.isCompanion {
				packet, err = raknet.MarshalApplication(raknet.CombatantDataDeltaMessage{
					ObjectID: healed.objectID, HitPoints: healed.hitPoint, IsHitPointChanged: true,
				})
			} else {
				packet, err = candidate.marshalCampaignCharacterResource(healed.creatureIndex)
			}
			if err != nil {
				return nil, nil, fmt.Errorf("treeResource: %w", err)
			}
			packets = append(packets, packet)
			received.PVEHealingReceived += float64(healed.amount)
		}
		candidate.queueStatDelta(received)
		if sessionKey == e.sessionKey {
			*caster = candidate
		} else {
			e.runtime.registry.sessions[sessionKey] = candidate
			if len(memberHealings) != 0 {
				candidate.zone.PublishHeroResourceTo(candidate.binding.UserID, candidate.generation)
				for _, healed := range companionHealings {
					candidate.zone.PublishCompanionResourceTo(candidate.binding.UserID, candidate.generation, healed.objectID)
				}
			}
		}
		healings = append(healings, memberHealings...)
	}
	return healings, packets, nil
}
