package gameplay

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	zoneobject "github.com/darkspinnet/darkspin/server/zone/object"
)

const nocturnaFixtureTerrorRadius = float32(12)
const nocturnaFixtureTerrorDuration = 3 * time.Second
const nightmareRootDamageInterval = time.Second
const nightmareRootDamageRadius = float32(4)
const nightmareRootDamage = float32(4)

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

func isNightmareVineRoot(nounName string) bool {
	return strings.HasPrefix(
		strings.ToLower(strings.TrimSpace(nounName)),
		"dest_prefab_nocturna_root_",
	)
}

func (e gameplayPeerSession) nightmareVineRootObjectIDs(
	vinePosition game.Vec3,
) ([]uint32, error) {
	if e.zone == nil {
		return nil, nil
	}
	objectIDs := make([]uint32, 0)
	for index, plan := range e.zone.ScriptObjectPlans() {
		publication, err := zoneobject.PublishScript(plan)
		if err != nil {
			return nil, fmt.Errorf("rootPublication[%d]: %w", index, err)
		}
		if !isNightmareVineRoot(publication.NounName) ||
			publication.Position.Sub(vinePosition).Length() > 10 {
			continue
		}
		objectIDs = append(objectIDs, publication.ObjectID)
	}
	return objectIDs, nil
}

func (e gameplayPeerSession) nightmareVineDeadRootPackets(
	timestamp uint64,
) ([][]byte, error) {
	if e.zone == nil || e.zone.NPCs() == nil {
		return nil, nil
	}
	objectIDs := make(map[uint32]struct{})
	for _, fixture := range e.zone.NPCs().Snapshots() {
		if !fixture.IsDefeated || !strings.EqualFold(
			fixture.Plan.NounName, nightmareVineNounName,
		) {
			continue
		}
		rootObjectIDs, err := e.nightmareVineRootObjectIDs(fixture.Plan.Position)
		if err != nil {
			return nil, fmt.Errorf("deadRootObjects: %w", err)
		}
		for _, objectID := range rootObjectIDs {
			objectIDs[objectID] = struct{}{}
		}
	}
	rootObjectIDs := make([]uint32, 0, len(objectIDs))
	for objectID := range objectIDs {
		rootObjectIDs = append(rootObjectIDs, objectID)
	}
	slices.Sort(rootObjectIDs)
	packets := make([][]byte, 0, len(rootObjectIDs))
	for _, objectID := range rootObjectIDs {
		packet, err := raknet.MarshalApplication(raknet.SetObjectGFXStateMessage{
			ObjectID: objectID, State: util.HashID("dead"), Timestamp: timestamp,
		})
		if err != nil {
			return nil, fmt.Errorf("deadRootGraphics: %w", err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
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

// pollNightmareVineRootsLocked applies one retained environmental hit while a
// hero stands on an authored live root. Roots may remain in the completed
// zone, but result voting must not attempt another damage transaction.
// The caller holds registry.mutex.
func (r gameplayPendingRuntime) pollNightmareVineRootsLocked(
	peerSession *gameplayPeerSession, timestamp uint64,
) ([][]byte, error) {
	if peerSession == nil || peerSession.zone == nil ||
		peerSession.isZoneTerminal() || !peerSession.zone.IsActive() ||
		peerSession.zone.NPCs() == nil || peerSession.deployedObjectID == 0 ||
		r.now == nil || r.now().Before(peerSession.nightmareRootDamageReadyAt) {
		return nil, nil
	}
	heroPosition := game.Vec3(peerSession.playerPosition)
	var source zonenpc.Snapshot
	isSourceFound := false
	for _, plan := range peerSession.zone.ScriptObjectPlans() {
		publication, err := zoneobject.PublishScript(plan)
		if err != nil {
			return nil, fmt.Errorf("rootHazardPublication: %w", err)
		}
		contactRadius := nightmareRootDamageRadius * max(publication.Scale, float32(1))
		if !isNightmareVineRoot(publication.NounName) ||
			publication.Position.Sub(heroPosition).Length() > contactRadius {
			continue
		}
		for _, candidate := range peerSession.zone.NPCs().LiveSnapshots() {
			if !strings.EqualFold(candidate.Plan.NounName, nightmareVineNounName) ||
				candidate.Plan.Position.Sub(publication.Position).Length() > 10 {
				continue
			}
			source = candidate
			isSourceFound = true
			break
		}
		if isSourceFound {
			break
		}
	}
	if !isSourceFound {
		return nil, nil
	}
	profile := zonenpc.ActionProfile{
		Family: zonenpc.ActionRetainedArea, AbilityName: "NightmareVineRoots",
		MinimumDamage: nightmareRootDamage, MaximumDamage: nightmareRootDamage,
		DamageType: 4, DamageSource: 0, IsDamageProfileKnown: true,
	}
	plan := zonenpc.AttackPlan{
		SourceObjectID: source.Plan.ObjectID,
		TargetObjectID: peerSession.deployedObjectID,
		SourcePosition: source.Plan.Position, TargetPosition: heroPosition,
		Profile: profile,
	}
	result := zonenpc.AttackResult{Damage: nightmareRootDamage}
	packets, statDelta, isDamageApplied, err := r.damage.npc.applyEnemyStatusDamage(
		peerSession, peerSession.generation, plan, result, timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("rootHazardDamage: %w", err)
	}
	peerSession.nightmareRootDamageReadyAt = r.now().Add(
		nightmareRootDamageInterval,
	)
	if !isDamageApplied && len(packets) == 0 {
		return nil, nil
	}
	peerSession.queueStatDelta(statDelta)
	return packets, nil
}
