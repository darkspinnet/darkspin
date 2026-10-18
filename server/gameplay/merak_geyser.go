package gameplay

import (
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	"github.com/darkspinnet/darkspin/server/zone"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type merakGeyserStep struct {
	isStopped  bool
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	zone       *zone.Zone
	sessionKey string
	generation uint64
	objectID   uint32
	timestamp  uint64
	nextSpawn  uint64
	delay      float32
	contacts   map[uint32]map[uint32]bool
}

func (e campaignNPCActionRuntime) startMerakControllers(packet raknet.Packet,
	sessionKey string, generation uint64, plans []zonenpc.SpawnPlan, timestamp uint64,
) error {
	for _, plan := range plans {
		profile, isFound := zonenpc.MerakGeyserProfile(plan.NounName)
		if !isFound || profile.Radius <= 0 || plan.OwnerObjectID != 0 {
			continue
		}
		e.registry.mutex.RLock()
		member, isMemberFound := e.registry.sessions[sessionKey]
		isCurrent := isMemberFound && member.generation == generation && member.zone != nil
		e.registry.mutex.RUnlock()
		if !isCurrent || !member.zone.NPCs().ClaimMerakController(plan.ObjectID) {
			continue
		}
		step := &merakGeyserStep{runtime: e, packet: packet.Autonomous(), zone: member.zone,
			sessionKey: sessionKey, generation: generation, objectID: plan.ObjectID,
			timestamp: timestamp + 1000, delay: 10, contacts: make(map[uint32]map[uint32]bool)}
		e.registry.mutex.Lock()
		current, isCurrentFound := e.registry.sessions[sessionKey]
		if !isCurrentFound || current.generation != generation || current.zone != step.zone {
			e.registry.mutex.Unlock()
			member.zone.NPCs().ReleaseMerakController(plan.ObjectID)
			continue
		}
		if current.campaignMerakControllers == nil {
			current.campaignMerakControllers = make(map[uint32]*merakGeyserStep)
		}
		current.campaignMerakControllers[plan.ObjectID] = step
		e.registry.sessions[sessionKey] = current
		e.registry.mutex.Unlock()
		err := scheduleNPCProducer(e.registry, step.packet, time.Second, step.produce)
		if err != nil {
			e.registry.mutex.Lock()
			current = e.registry.sessions[sessionKey]
			delete(current.campaignMerakControllers, plan.ObjectID)
			e.registry.mutex.Unlock()
			member.zone.NPCs().ReleaseMerakController(plan.ObjectID)
			return fmt.Errorf("merakSchedule: %w", err)
		}
	}
	return nil
}

func merakGeyserCreate(geyser zonenpc.MerakGeyser) ([]byte, error) {
	packet, err := raknet.MarshalApplication(raknet.ObjectCreateMessage{
		ObjectID: geyser.ObjectID, Noun: util.HashID("PlasmaGeyser.Noun"), Scale: 1,
		PositionX: geyser.Position.X, PositionY: geyser.Position.Y, PositionZ: geyser.Position.Z,
	})
	if err != nil {
		return nil, fmt.Errorf("geyserCreate: %w", err)
	}
	return packet, nil
}

func (e *merakGeyserStep) spawn(boss zonenpc.Snapshot) ([]byte, error) {
	random := e.zone.NPCRandom()
	angle := random.Float64() * 2 * math.Pi
	distance := 5 + 25*random.Float64()
	// Rotate the current right vector. The angle is uniform, but preserve the
	// authored orientation for a fixed seed rather than using a global axis.
	position := boss.Plan.Position
	rightX, rightY := float64(boss.Facing.Y), -float64(boss.Facing.X)
	if rightX == 0 && rightY == 0 {
		rightX = 1
	}
	position.X += float32(distance * (rightX*math.Cos(angle) - rightY*math.Sin(angle)))
	position.Y += float32(distance * (rightX*math.Sin(angle) + rightY*math.Cos(angle)))
	projected, isProjected, err := zonenavigation.ProjectPosition(e.zone.Navigation(), position, 0)
	if err != nil {
		return nil, fmt.Errorf("geyserProject: %w", err)
	}
	if e.zone.Navigation() != nil && !isProjected {
		return nil, nil
	}
	if isProjected {
		position = projected
	}
	objectID, err := e.zone.ReserveObjectIDs(1)
	if err != nil {
		return nil, fmt.Errorf("geyserReserve: %w", err)
	}
	geyser := zonenpc.MerakGeyser{ObjectID: objectID, BossObjectID: e.objectID,
		Position: position, ExpiresAt: e.timestamp + 10000}
	packet, err := merakGeyserCreate(geyser)
	if err != nil {
		return nil, fmt.Errorf("geyserMarshal: %w", err)
	}
	e.zone.NPCs().PutMerakGeyser(geyser)
	return packet, nil
}

func (e *merakGeyserStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	if e.isStopped {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || member.generation != e.generation || member.zone != e.zone {
		// Keep the single zone controller attached to a connected co-op member.
		isFound = false
		for key, candidate := range e.runtime.registry.sessions {
			if candidate.zone == e.zone && !candidate.isZoneTerminal() {
				e.sessionKey, e.generation, member, isFound = key, candidate.generation, candidate, true
				break
			}
		}
	}
	boss, isBossFound := e.zone.NPCs().NPC(e.objectID)
	isActive := isFound && !member.isZoneTerminal() && isBossFound && !boss.IsDefeated && boss.HitPoint > 0
	packets := make([][]byte, 0)
	plans := make([]zonenpc.AttackPlan, 0)
	if isActive && e.nextSpawn == 0 && boss.TargetObjectID != 0 {
		// One-second aggro polling, then eight seconds plus the initial ten-second wait.
		e.nextSpawn = e.timestamp + 18000
	}
	if isActive && e.nextSpawn != 0 && e.timestamp >= e.nextSpawn && e.zone.NPCRandom() != nil {
		packet, err := e.spawn(boss)
		if err != nil {
			e.runtime.logger.Printf("Merak geyser omitted source=%d: %v", e.objectID, err)
		}
		if packet != nil {
			packets = append(packets, packet)
		}
		e.delay = max(e.delay*float32(.95), 1)
		e.nextSpawn = e.timestamp + uint64(e.delay*1000)
	}
	profile, isProfileFound := zonenpc.MerakGeyserProfile(boss.Plan.NounName)
	for _, geyser := range e.zone.NPCs().MerakGeysers() {
		if geyser.BossObjectID != e.objectID {
			continue
		}
		if !isActive || e.timestamp >= geyser.ExpiresAt {
			packet, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{ObjectID: []uint32{geyser.ObjectID}})
			if err != nil {
				e.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("geyserDelete: %w", err)
			}
			packets = append(packets, packet)
			e.zone.NPCs().RemoveMerakGeyser(geyser.ObjectID)
			delete(e.contacts, geyser.ObjectID)
			continue
		}
		previousContacts := e.contacts[geyser.ObjectID]
		contacts := make(map[uint32]bool)
		for _, target := range e.zone.LiveNPCTargets() {
			if !isProfileFound || target.HitPoint <= 0 || target.Position.Sub(geyser.Position).Length() > profile.Radius {
				continue
			}
			contacts[target.ObjectID] = true
			if previousContacts[target.ObjectID] {
				continue
			}
			plans = append(plans, zonenpc.AttackPlan{SourceObjectID: e.objectID, TargetObjectID: target.ObjectID,
				SourcePosition: boss.Plan.Position, TargetPosition: target.Position, Profile: profile})
		}
		e.contacts[geyser.ObjectID] = contacts
	}
	// Hazard creation/deletion is shared even when the originating peer leaves.
	for key, candidate := range e.runtime.registry.sessions {
		if candidate.zone != e.zone {
			continue
		}
		candidate.queueCampaignPackets(packets)
		e.runtime.registry.sessions[key] = candidate
	}
	e.runtime.registry.mutex.Unlock()
	for _, plan := range plans {
		burnPackets, err := e.runtime.applyCampaignNPCPoison(e.packet, e.sessionKey, e.generation, plan, e.timestamp)
		if err != nil {
			return nil, fmt.Errorf("geyserBurn: %w", err)
		}
		packets = burnPackets
		// Status publication uses the same shared target path as other enemy DoTs.
		e.runtime.registry.mutex.Lock()
		for key, candidate := range e.runtime.registry.sessions {
			if candidate.zone != e.zone {
				continue
			}
			candidate.queueCampaignPackets(packets)
			e.runtime.registry.sessions[key] = candidate
		}
		e.runtime.registry.mutex.Unlock()
	}
	if !isActive {
		e.zone.NPCs().ReleaseMerakController(e.objectID)
		return nil, nil
	}
	e.timestamp += 100
	err := scheduleNPCProducer(e.runtime.registry, e.packet, 100*time.Millisecond, e.produce)
	if err != nil {
		return nil, fmt.Errorf("geyserTick: %w", err)
	}
	return nil, nil
}

// Retire dynamic scenery before the peer scheduler is stopped, so neither the
// shared controller claim nor a rendered geyser survives an interrupted run.
func (e *gameplayPeerSession) stopMerakControllers() {
	for objectID, step := range e.campaignMerakControllers {
		step.isStopped = true
		packets := make([][]byte, 0)
		for _, geyser := range step.zone.NPCs().MerakGeysers() {
			if geyser.BossObjectID != objectID {
				continue
			}
			packet, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{ObjectID: []uint32{geyser.ObjectID}})
			if err != nil {
				step.runtime.logger.Printf("Merak geyser cleanup omitted object=%d: %v", geyser.ObjectID, err)
			} else {
				packets = append(packets, packet)
			}
			step.zone.NPCs().RemoveMerakGeyser(geyser.ObjectID)
		}
		e.queueCampaignPackets(packets)
		for key, member := range step.runtime.registry.sessions {
			if member.zone != step.zone || key == step.sessionKey {
				continue
			}
			member.queueCampaignPackets(packets)
			step.runtime.registry.sessions[key] = member
		}
		step.zone.NPCs().ReleaseMerakController(objectID)
		delete(e.campaignMerakControllers, objectID)
	}
}
