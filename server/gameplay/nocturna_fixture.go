package gameplay

import (
	"fmt"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const nocturnaFixtureTerrorRadius = float32(12)
const nocturnaFixtureTerrorDuration = 3 * time.Second

// Both Nocturna egg death scripts emit their world-positioned burst, wait
// chainingDelay=0.2 seconds, then delete the original plant object.
const nocturnaPlantDeleteDelay = 200 * time.Millisecond

func nocturnaPlantDeathEffect(nounName string) string {
	switch {
	case strings.EqualFold(nounName, nocturnaExplosivePlantNounName):
		return "nocturna_alien_egg_plasma_explosion.ServerEventDef"
	case strings.EqualFold(nounName, nocturnaSupernaturalPlantNounName),
		strings.EqualFold(nounName, nocturnaPrefabSupernaturalPlantNounName):
		return "nocturna_alien_egg_necro_explosion.ServerEventDef"
	default:
		return ""
	}
}

func isNocturnaTerrorFixture(nounName string) bool {
	switch {
	case strings.EqualFold(nounName, nightmareVineNounName),
		strings.EqualFold(nounName, nocturnaExplosivePlantNounName),
		strings.EqualFold(nounName, nocturnaSupernaturalPlantNounName),
		strings.EqualFold(nounName, nocturnaPrefabSupernaturalPlantNounName):
		return true
	default:
		return false
	}
}

func (r campaignDamageRuntime) publishNocturnaFixtureTerror(
	packet raknet.Packet, sessionKey string, generation uint64,
	sourceObjectID uint32, sourcePosition game.Vec3, timestamp uint64,
) ([][]byte, error) {
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	if !isFound || peerSession.generation != generation || peerSession.zone == nil ||
		peerSession.zone.NPCs() == nil {
		r.registry.mutex.RUnlock()
		return nil, nil
	}
	targetObjectIDs := make([]uint32, 0)
	for _, target := range peerSession.zone.NPCs().LiveSnapshots() {
		if target.Plan.ObjectID == sourceObjectID || target.Plan.IsFixture ||
			target.Plan.IsBoss || target.Faction != zonenpc.FactionNonPlayerAligned ||
			target.Plan.Position.Sub(sourcePosition).Length() > nocturnaFixtureTerrorRadius {
			continue
		}
		targetObjectIDs = append(targetObjectIDs, target.Plan.ObjectID)
	}
	r.registry.mutex.RUnlock()

	npcRuntime := r.npc
	npcRuntime.registry = r.registry
	npcRuntime.pursuit.registry = r.registry
	npcRuntime.pursuit.now = npcRuntime.now
	npcRuntime.pursuit.logger = r.logger
	abilityRuntime := campaignAbilityCommandRuntime{
		registry: r.registry, modifierPool: r.npc.modifierPool,
		npc: npcRuntime, now: npcRuntime.now, logger: r.logger,
	}
	packets := make([][]byte, 0)
	for index, targetObjectID := range targetObjectIDs {
		fearPackets, err := abilityRuntime.applyHeroNPCFear(
			packet, sessionKey, generation, targetObjectID, sourceObjectID,
			nocturnaFixtureTerrorDuration, timestamp,
		)
		if err != nil {
			return nil, fmt.Errorf("fixtureFear[%d]: %w", index, err)
		}
		packets = append(packets, fearPackets...)
	}
	return packets, nil
}
