package gameplay

import (
	"fmt"
	"math"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/zone"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type campaignChronoStrikerFleeResume struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	objectID   uint32
}

type campaignRayKillerFleeResume struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	objectID   uint32
}

type campaignMendingTanglidFleeResume struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	objectID   uint32
}

type campaignElectronBursterFleeStep struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	objectID   uint32
	timestamp  uint64
}

func (e campaignElectronBursterFleeStep) produce() ([][]byte, error) {
	return e.runtime.produceElectronBursterFlee(
		e.packet, e.sessionKey, e.generation, e.objectID, e.timestamp,
	)
}

func (e campaignElectronBursterFleeStep) resume(
	timestamp uint64,
) ([][]byte, error) {
	return e.runtime.produceEnemyCone(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
}

func (e campaignChronoStrikerFleeResume) produce(
	timestamp uint64,
) ([][]byte, error) {
	return e.runtime.produceEnemyMelee(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
}

func (e campaignRayKillerFleeResume) produce(
	timestamp uint64,
) ([][]byte, error) {
	return e.runtime.produceZelemShot(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
}

func (e campaignMendingTanglidFleeResume) produce(
	timestamp uint64,
) ([][]byte, error) {
	return e.runtime.produceEnemyMelee(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
}

func campaignDifficultyNounFamily(nounName string) string {
	family := strings.TrimSuffix(strings.ToLower(nounName), ".noun")
	family = strings.TrimSuffix(family, "_2")
	return strings.TrimSuffix(family, "_3")
}

func campaignDefeatedSameTypeCount(
	source zonenpc.Snapshot, snapshots []zonenpc.Snapshot,
) int {
	family := campaignDifficultyNounFamily(source.Plan.NounName)
	count := 0
	for _, candidate := range snapshots {
		if candidate.Plan.ObjectID == source.Plan.ObjectID ||
			!candidate.IsDefeated || candidate.Plan.IsFixture ||
			campaignDifficultyNounFamily(candidate.Plan.NounName) != family {
			continue
		}
		count++
	}
	return count
}

func campaignChronoStrikerFleeDestination(
	source game.Vec3, target game.Vec3, randomScalar func() float64,
	project func(game.Vec3) (game.Vec3, bool, error),
) (game.Vec3, bool, error) {
	deltaX := source.X - target.X
	deltaY := source.Y - target.Y
	length := float32(math.Sqrt(float64(deltaX*deltaX + deltaY*deltaY)))
	if length <= 0 {
		deltaX = 1
		deltaY = 0
		length = 1
	}
	baseAngle := math.Atan2(float64(deltaY/length), float64(deltaX/length))
	span := math.Pi / 3
	for range 3 {
		angle := baseAngle + (randomScalar()*2-1)*span
		distance := 3 + float32(randomScalar()*3)
		candidate := game.Vec3{
			X: source.X + float32(math.Cos(angle))*distance,
			Y: source.Y + float32(math.Sin(angle))*distance,
			Z: source.Z,
		}
		destination, isFound, err := project(candidate)
		if err != nil {
			return game.Vec3{}, false, fmt.Errorf("fleeProject: %w", err)
		}
		if isFound {
			return destination, true, nil
		}
		span *= 2
	}
	return source, true, nil
}

func (r campaignNPCActionRuntime) produceMendingTanglidFlee(
	packet raknet.Packet, sessionKey string, generation uint64,
	source zonenpc.Snapshot, target zone.NPCTarget, profile zonenpc.ActionProfile,
	timestamp uint64,
) ([][]byte, bool, error) {
	if campaignDifficultyNounFamily(source.Plan.NounName) != "verdanthspecialtwo" ||
		zonegeometry.Distance(source.Plan.Position, target.Position) >= 10 {
		return nil, false, nil
	}
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(
		generation, source.Plan.ObjectID,
	)
	if !isCurrent || peerSession.zone.NPCRandom() == nil {
		r.registry.mutex.RUnlock()
		return nil, true, nil
	}
	destination, isDestinationFound, err := campaignChronoStrikerFleeDestination(
		source.Plan.Position, target.Position,
		peerSession.zone.NPCRandom().Float64,
		func(candidate game.Vec3) (game.Vec3, bool, error) {
			return zoneaction.NPCDirectMovementDestination(
				peerSession.zone.Navigation(), source.Plan.Position, candidate,
				source.Plan.NPCProfile.FootprintRadius,
			)
		},
	)
	r.registry.mutex.RUnlock()
	if err != nil {
		return nil, true, fmt.Errorf("mendingFleeDestination: %w", err)
	}
	if !isDestinationFound || zonegeometry.Distance(
		source.Plan.Position, destination,
	) < source.Plan.NPCProfile.FootprintRadius {
		return nil, false, nil
	}
	fleeProfile := zonenpc.ActionProfile{
		Family: zonenpc.ActionMelee, AbilityName: "Flee",
		MovementSpeed: profile.MovementSpeed,
		Range:         1.5 * source.Plan.NPCProfile.FootprintRadius,
	}
	action := zonenpc.FirstActionPlan{
		ObjectID: source.Plan.ObjectID, TargetObjectID: target.ObjectID,
		ActionGeneration: source.ActionGeneration,
		SourcePosition:   source.Plan.Position, TargetPosition: destination,
		Profile: fleeProfile, IsPursuitNeeded: true,
	}
	packets, err := npcraknet.Pursuit(action)
	if err != nil {
		return nil, true, fmt.Errorf("mendingFleeMarshal: %w", err)
	}
	resume := campaignMendingTanglidFleeResume{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: source.Plan.ObjectID,
	}
	err = r.pursuit.schedule(
		packet, sessionKey, generation, source.Plan.ObjectID, timestamp,
		destination, fleeProfile, resume.produce,
	)
	if err != nil {
		return nil, true, fmt.Errorf("mendingFleeSchedule: %w", err)
	}
	return packets, true, nil
}

func (r campaignNPCActionRuntime) produceElectronBursterFlee(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, error) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(generation, objectID)
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	isElectronBurster := isEnemyFound && campaignDifficultyNounFamily(
		enemy.Plan.NounName,
	) == "cryosbasiclightningranged"
	if !isElectronBurster || peerSession.zone.NPCRandom() == nil {
		r.registry.mutex.Unlock()
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, enemy.TargetObjectID,
	)
	if !isTargetFound {
		r.registry.mutex.Unlock()
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	profile := zonenpc.ActionProfile{
		Family: zonenpc.ActionMelee, AbilityName: "Flee", MovementSpeed: 5,
		Range: 1.5 * enemy.Plan.NPCProfile.FootprintRadius,
	}
	destination := target.Position
	if zonegeometry.Distance(enemy.Plan.Position, target.Position) > 10 {
		profile.Range = 8
	} else {
		var err error
		destination, _, err = campaignChronoStrikerFleeDestination(
			enemy.Plan.Position, target.Position,
			peerSession.zone.NPCRandom().Float64,
			func(candidate game.Vec3) (game.Vec3, bool, error) {
				return zoneaction.NPCDirectMovementDestination(
					peerSession.zone.Navigation(), enemy.Plan.Position, candidate,
					enemy.Plan.NPCProfile.FootprintRadius,
				)
			},
		)
		if err != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("electronFleeDestination: %w", err)
		}
	}
	r.registry.mutex.Unlock()

	action := zonenpc.FirstActionPlan{
		ObjectID: objectID, TargetObjectID: target.ObjectID,
		SourcePosition: enemy.Plan.Position, TargetPosition: destination,
		Profile: profile, IsPursuitNeeded: true,
	}
	pursuitPackets, err := npcraknet.Pursuit(action)
	if err != nil {
		return nil, fmt.Errorf("electronFleeMarshal: %w", err)
	}
	clearPacket, err := raknet.MarshalApplication(raknet.AgentBlackboardUpdateMessage{
		ObjectID: objectID, IsInCombat: true, IsTargetable: true,
	})
	if err != nil {
		return nil, fmt.Errorf("electronFleeClear: %w", err)
	}
	restorePacket, err := raknet.MarshalApplication(raknet.AgentBlackboardUpdateMessage{
		ObjectID: objectID, TargetID: target.ObjectID,
		IsInCombat: true, IsTargetable: true,
	})
	if err != nil {
		return nil, fmt.Errorf("electronFleeRestore: %w", err)
	}
	resume := campaignElectronBursterFleeStep{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID,
	}
	err = r.pursuit.schedule(
		packet, sessionKey, generation, objectID, timestamp,
		destination, profile, resume.resume,
	)
	if err != nil {
		return nil, fmt.Errorf("electronFleeSchedule: %w", err)
	}
	packets := make([][]byte, 0, len(pursuitPackets)+2)
	packets = append(packets, clearPacket)
	packets = append(packets, pursuitPackets...)
	packets = append(packets, restorePacket)
	return packets, nil
}

func (r campaignNPCActionRuntime) produceChronoStrikerFlee(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, bool, error) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(generation, objectID)
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	if !isEnemyFound || campaignDifficultyNounFamily(enemy.Plan.NounName) !=
		"verdanthbasicmelee" {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	defeatedCount := campaignDefeatedSameTypeCount(
		enemy, peerSession.zone.NPCs().Snapshots(),
	)
	if peerSession.campaignNPCFleeDeathCounts == nil {
		peerSession.campaignNPCFleeDeathCounts = make(map[uint32]int)
	}
	if defeatedCount <= peerSession.campaignNPCFleeDeathCounts[objectID] {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, enemy.TargetObjectID,
	)
	if !isTargetFound || peerSession.zone.NPCRandom() == nil {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	profile := zonenpc.ActionProfile{
		Family: zonenpc.ActionMelee, AbilityName: "Flee", MovementSpeed: 7,
		Range: 1.5 * enemy.Plan.NPCProfile.FootprintRadius,
	}
	destination := target.Position
	centerDistance := zonegeometry.Distance(enemy.Plan.Position, target.Position)
	if centerDistance > 10 {
		profile.Range = 8
	} else {
		var destinationFound bool
		var err error
		destination, destinationFound, err = campaignChronoStrikerFleeDestination(
			enemy.Plan.Position, target.Position,
			peerSession.zone.NPCRandom().Float64,
			func(candidate game.Vec3) (game.Vec3, bool, error) {
				return zoneaction.NPCDirectMovementDestination(
					peerSession.zone.Navigation(), enemy.Plan.Position, candidate,
					enemy.Plan.NPCProfile.FootprintRadius,
				)
			},
		)
		if err != nil {
			r.registry.mutex.Unlock()
			return nil, false, fmt.Errorf("enemyFleeDestination: %w", err)
		}
		if !destinationFound {
			r.registry.mutex.Unlock()
			return nil, true, nil
		}
	}
	peerSession.campaignNPCFleeDeathCounts[objectID] = defeatedCount
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	action := zonenpc.FirstActionPlan{
		ObjectID: objectID, TargetObjectID: target.ObjectID,
		SourcePosition: enemy.Plan.Position, TargetPosition: destination,
		Profile: profile, IsPursuitNeeded: true,
	}
	packets, err := npcraknet.Pursuit(action)
	if err != nil {
		return nil, false, fmt.Errorf("enemyFleeMarshal: %w", err)
	}
	resume := campaignChronoStrikerFleeResume{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID,
	}
	err = r.pursuit.schedule(
		packet, sessionKey, generation, objectID, timestamp,
		destination, profile, resume.produce,
	)
	if err != nil {
		return nil, false, fmt.Errorf("enemyFleeSchedule: %w", err)
	}
	return packets, true, nil
}

func (r campaignNPCActionRuntime) produceRayKillerFlee(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, bool, error) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(generation, objectID)
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	if !isEnemyFound || campaignDifficultyNounFamily(enemy.Plan.NounName) !=
		"cryoselementalspecialthree" {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, enemy.TargetObjectID,
	)
	if !isTargetFound || peerSession.zone.NPCRandom() == nil ||
		!peerSession.zone.NPCs().ConsumeDamageFlee(objectID) {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	destination, isDestinationFound, err := campaignChronoStrikerFleeDestination(
		enemy.Plan.Position, target.Position,
		peerSession.zone.NPCRandom().Float64,
		func(candidate game.Vec3) (game.Vec3, bool, error) {
			return zoneaction.NPCDirectMovementDestination(
				peerSession.zone.Navigation(), enemy.Plan.Position, candidate,
				enemy.Plan.NPCProfile.FootprintRadius,
			)
		},
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, false, fmt.Errorf("rayKillerFleeDestination: %w", err)
	}
	if !isDestinationFound {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	profile, isProfileFound := zonenpc.ActionProfileForPlan(enemy.Plan)
	if !isProfileFound {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	profile.Family = zonenpc.ActionMelee
	profile.AbilityName = "Flee"
	profile.Range = 1.5 * enemy.Plan.NPCProfile.FootprintRadius
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	action := zonenpc.FirstActionPlan{
		ObjectID: objectID, TargetObjectID: target.ObjectID,
		SourcePosition: enemy.Plan.Position, TargetPosition: destination,
		Profile: profile, IsPursuitNeeded: true,
	}
	packets, err := npcraknet.Pursuit(action)
	if err != nil {
		return nil, false, fmt.Errorf("rayKillerFleeMarshal: %w", err)
	}
	resume := campaignRayKillerFleeResume{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID,
	}
	err = r.pursuit.schedule(
		packet, sessionKey, generation, objectID, timestamp,
		destination, profile, resume.produce,
	)
	if err != nil {
		return nil, false, fmt.Errorf("rayKillerFleeSchedule: %w", err)
	}
	return packets, true, nil
}
