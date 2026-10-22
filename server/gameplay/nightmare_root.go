package gameplay

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	zoneobject "github.com/darkspinnet/darkspin/server/zone/object"
)

const nightmareRootDamageInterval = time.Second
const nightmareRootDamageRadius = float32(4)
const nightmareRootMinimumDamage = float32(5)
const nightmareRootMaximumDamage = float32(8)
const nightmareRootImpactEffectID = uint32(0x8227211a)

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

// pollNightmareVineRootsLocked applies each root's retained environmental hit while a
// hero stands on an authored live root. Roots may remain in the completed
// zone, but result voting must not attempt another damage transaction.
// The caller holds registry.mutex.
func (e gameplayPendingRuntime) pollNightmareVineRootsLocked(
	peerSession *gameplayPeerSession, timestamp uint64,
) ([][]byte, error) {
	if peerSession == nil || peerSession.zone == nil ||
		peerSession.isZoneTerminal() || !peerSession.zone.IsActive() ||
		peerSession.zone.NPCs() == nil || peerSession.deployedObjectID == 0 ||
		peerSession.deployedHitPoint() <= 0 ||
		e.now == nil {
		return nil, nil
	}
	heroPosition := game.Vec3(peerSession.playerPosition)
	at := e.now()
	if peerSession.nightmareRootDamageReadyAts == nil {
		peerSession.nightmareRootDamageReadyAts = make(map[uint32]time.Time)
	}
	liveRoots := make(map[uint32]struct{})
	packets := make([][]byte, 0)
	for _, plan := range peerSession.zone.ScriptObjectPlans() {
		publication, err := zoneobject.PublishScript(plan)
		if err != nil {
			return nil, fmt.Errorf("rootHazardPublication: %w", err)
		}
		// TreeRootPassive passes its radius directly to CreateTriggerVolume;
		// the root's visual scale is not an input to that volume.
		if !isNightmareVineRoot(publication.NounName) {
			continue
		}
		var source zonenpc.Snapshot
		isSourceFound := false
		for _, candidate := range peerSession.zone.NPCs().LiveSnapshots() {
			if !strings.EqualFold(candidate.Plan.NounName, nightmareVineNounName) ||
				candidate.Plan.Position.Sub(publication.Position).Length() > 10 {
				continue
			}
			source = candidate
			isSourceFound = true
			break
		}
		if !isSourceFound {
			continue
		}
		liveRoots[publication.ObjectID] = struct{}{}
		readyAt, isTracked := peerSession.nightmareRootDamageReadyAts[publication.ObjectID]
		if !isTracked {
			readyAt = at
		}
		if at.Before(readyAt) {
			continue
		}
		// The modifier's timer runs even while its trigger has no occupant.
		// Skip missed polls without moving the remaining cadence to poll time.
		elapsedTicks := at.Sub(readyAt)/nightmareRootDamageInterval + 1
		peerSession.nightmareRootDamageReadyAts[publication.ObjectID] = readyAt.Add(elapsedTicks * nightmareRootDamageInterval)
		if publication.Position.Sub(heroPosition).Length() > nightmareRootDamageRadius || peerSession.deployedHitPoint() <= 0 {
			continue
		}
		rootPackets, err := e.applyNightmareRootDamageLocked(peerSession, source, timestamp)
		if err != nil {
			return nil, fmt.Errorf("rootHazardHit: %w", err)
		}
		packets = append(packets, rootPackets...)
	}
	for objectID := range peerSession.nightmareRootDamageReadyAts {
		if _, isLive := liveRoots[objectID]; !isLive {
			delete(peerSession.nightmareRootDamageReadyAts, objectID)
		}
	}
	return packets, nil
}

func (e gameplayPendingRuntime) applyNightmareRootDamageLocked(
	peerSession *gameplayPeerSession, source zonenpc.Snapshot, timestamp uint64,
) ([][]byte, error) {
	profile := zonenpc.ActionProfile{
		Family: zonenpc.ActionRetainedArea, AbilityName: "NightmareVineRoots",
		MinimumDamage: nightmareRootMinimumDamage, MaximumDamage: nightmareRootMaximumDamage,
		DamageType: 3, DamageSource: 1, DescriptorMask: 1 << 14, IsDamageProfileKnown: true,
	}
	plan := zonenpc.AttackPlan{
		SourceObjectID: source.Plan.ObjectID,
		TargetObjectID: peerSession.deployedObjectID,
		SourcePosition: source.Plan.Position, TargetPosition: game.Vec3(peerSession.playerPosition),
		Profile: profile,
	}
	damage, err := sim.SelectRankDamage(peerSession.zone.NPCRandom(), sim.DamageRange{
		Minimum: nightmareRootMinimumDamage, Maximum: nightmareRootMaximumDamage,
	})
	if err != nil {
		return nil, fmt.Errorf("rootDamageSelect: %w", err)
	}
	impactEffectID := nightmareRootImpactEffectID
	impactPacket, err := raknet.MarshalApplication(raknet.ServerEventContractMessage{
		SimpleSwarmEffectID: &impactEffectID, ObjectID: &plan.TargetObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("rootImpact: %w", err)
	}
	result := zonenpc.AttackResult{Damage: damage}
	packets, statDelta, isDamageApplied, err := e.damage.npc.applyEnemyStatusDamage(
		peerSession, peerSession.generation, plan, result, timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("rootHazardDamage: %w", err)
	}
	if !isDamageApplied && len(packets) == 0 {
		return nil, nil
	}
	peerSession.queueStatDelta(statDelta)
	return append(packets, impactPacket), nil
}
