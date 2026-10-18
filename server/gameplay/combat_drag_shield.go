package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	"github.com/darkspinnet/darkspin/server/zone"
)

// Abilities/0x37A367CA.lua (chunk 487) creates a separate TabulaRasa at
// timetohit. RunSphere owns its 16-second field and five-second visual tail;
// neither its transform nor its lifetime belongs to the moving caster.
type nomadDragShield struct {
	objectID   uint32
	position   game.Vec3
	radius     float32
	effectName string
	expiresAt  time.Time
}

func (e nomadDragShield) packets() ([][]byte, error) {
	messages := []raknet.ApplicationMessage{
		raknet.ObjectCreateMessage{
			ObjectID: e.objectID, Noun: util.HashID("TabulaRasa.Noun"),
			PositionX: e.position.X, PositionY: e.position.Y, PositionZ: e.position.Z,
			Scale: e.radius, Team: 0, IsCollisionEnabled: false,
		},
		raknet.AttachedEffectMessage{
			Slot: 1, IsForceAttached: true, ObjectID: e.objectID,
			Asset: util.HashID(e.effectName),
		},
	}
	packets := make([][]byte, 0, len(messages))
	for _, message := range messages {
		packet, err := raknet.MarshalApplication(message)
		if err != nil {
			return nil, fmt.Errorf("shieldSpawn: %w", err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

type nomadDragShieldStep struct {
	runtime        campaignNPCActionRuntime
	sessionKey     string
	generation     uint64
	zone           *zone.Zone
	sourceObjectID uint32
	shield         nomadDragShield
}

func (e nomadDragShieldStep) spawn() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || member.generation != e.generation || member.zone != e.zone ||
		!member.isCampaignNPCSourceActive(e.generation, e.sourceObjectID) {
		return nil, nil
	}
	enemy, isEnemyFound := member.zone.NPCs().NPC(e.sourceObjectID)
	if !isEnemyFound || enemy.IsDefeated {
		return nil, nil
	}
	shield := e.shield
	shield.position = enemy.Plan.Position
	shield.expiresAt = e.runtime.now().Add(nomadDragSlowShieldCooldown)
	packets, err := shield.packets()
	if err != nil {
		return nil, fmt.Errorf("dragShieldSpawn: %w", err)
	}
	if member.campaignNPCDragShields == nil {
		member.campaignNPCDragShields = make(map[uint32]nomadDragShield)
	}
	member.campaignNPCDragShields[shield.objectID] = shield
	e.runtime.registry.sessions[e.sessionKey] = member
	return packets, nil
}

func (e nomadDragShieldStep) expire() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || member.generation != e.generation || member.zone != e.zone {
		return nil, nil
	}
	shield, isShieldFound := member.campaignNPCDragShields[e.shield.objectID]
	if !isShieldFound {
		return nil, nil
	}
	packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
		Slot: 1, ObjectID: shield.objectID, IsRemovalRequested: true,
		Asset: util.HashID(shield.effectName), IsHardStop: false,
	})
	if err != nil {
		return nil, fmt.Errorf("shieldRampDown: %w", err)
	}
	delete(member.campaignNPCDragShields, shield.objectID)
	e.runtime.registry.sessions[e.sessionKey] = member
	return [][]byte{packet}, nil
}

func (e nomadDragShieldStep) remove() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && member.generation == e.generation && member.zone == e.zone
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	packet, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{ObjectID: []uint32{e.shield.objectID}})
	if err != nil {
		return nil, fmt.Errorf("shieldDelete: %w", err)
	}
	return [][]byte{packet}, nil
}

func (e *gameplaySessionRegistry) nomadDragShieldPackets(campaignZone *zone.Zone, now time.Time) ([][]byte, error) {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	packets := make([][]byte, 0)
	for _, member := range e.sessions {
		if member.zone != campaignZone {
			continue
		}
		for _, shield := range member.campaignNPCDragShields {
			if !now.Before(shield.expiresAt) {
				continue
			}
			shieldPackets, err := shield.packets()
			if err != nil {
				return nil, fmt.Errorf("shieldBaseline: %w", err)
			}
			packets = append(packets, shieldPackets...)
		}
	}
	return packets, nil
}

func (e campaignNPCActionRuntime) scheduleNomadDragShield(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, radius float32, effectName string, timestamp uint64,
) error {
	e.registry.mutex.Lock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || member.generation != generation || member.zone == nil {
		e.registry.mutex.Unlock()
		return errors.New("shield source replaced")
	}
	shieldObjectID, err := member.reserveCampaignObjectID()
	e.registry.mutex.Unlock()
	if err != nil {
		return fmt.Errorf("shieldObjectID: %w", err)
	}
	step := nomadDragShieldStep{
		runtime: e, sessionKey: sessionKey, generation: generation,
		zone: member.zone, sourceObjectID: objectID,
		shield: nomadDragShield{objectID: shieldObjectID, radius: radius, effectName: effectName},
	}
	phase := campaignNomadDragPhaseStep{
		runtime: e, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID,
		timestamp: timestamp + uint64(nomadDragSlowShieldRelease/time.Millisecond),
	}
	producers := []raknet.ScheduledPacketProducer{
		{Delay: nomadDragSlowShieldHitDelay, Produce: step.spawn},
		{Delay: nomadDragSlowShieldRelease, Produce: phase.produce},
		{Delay: nomadDragSlowShieldHitDelay + nomadDragSlowShieldCooldown, Produce: step.expire},
		{Delay: nomadDragSlowShieldHitDelay + nomadDragSlowShieldCooldown + 5*time.Second, Produce: step.remove},
	}
	cancel, err := scheduleNPCProducers(e.registry, packet, producers)
	if err != nil {
		return fmt.Errorf("shieldSchedule: %w", err)
	}
	if cancel == nil {
		return errors.New("shield cancellation unavailable")
	}
	return nil
}
