package gameplay

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type plasmaWallSegment struct {
	run             *plasmaWallRun
	objectID        uint32
	origin          game.Vec3
	end             game.Vec3
	timestamp       uint64
	startedAt       time.Time
	damageAt        time.Time
	damagePollCount int
	contacts        map[uint32]bool
	cancel          raknet.CancelSchedule
	tickDamage      game.DamageRange
}

func (e *plasmaWallRun) completeLegLocked(member *gameplayPeerSession, actor zonenpc.Snapshot) ([][]byte, error) {
	e.isMoving = false
	packets, err := npcraknet.StopAtPose(e.objectID, actor.Plan.Position, actor.Facing)
	if err != nil {
		return nil, fmt.Errorf("plasmaStop: %w", err)
	}
	if actor.Plan.Position.Sub(e.origin).Length() < 0.01 {
		return packets, nil
	}
	// The moving line captures at ability start; the completed wall's own
	// passive captures its owner's current attributes at segment creation.
	tickDamage, err := zonenpc.ResolveModifierTickDamage(actor, zonenpc.PlasmaWallBurnProfile(1))
	if err != nil {
		return packets, fmt.Errorf("wallSnapshot: %w", err)
	}
	objectID, err := member.zone.ReserveObjectIDs(1)
	if err != nil {
		return packets, fmt.Errorf("wallReserve: %w", err)
	}
	now := e.runtime.now()
	segment := &plasmaWallSegment{run: e, objectID: objectID,
		origin: e.origin, end: actor.Plan.Position, timestamp: e.timestamp,
		startedAt: now, damageAt: now,
		contacts: make(map[uint32]bool), tickDamage: tickDamage}
	segmentPackets, err := segment.createPackets()
	if err != nil {
		return packets, fmt.Errorf("wallCreate: %w", err)
	}
	if member.plasmaWall.segments == nil {
		member.plasmaWall.segments = make(map[uint32]*plasmaWallSegment)
	}
	member.plasmaWall.segments[objectID] = segment
	// Tick zero is the authored first trigger-volume poll; the following
	// twelve polls happen one second apart before the thirteenth wait/delete.
	err = segment.scheduleLocked(0)
	if err != nil {
		delete(member.plasmaWall.segments, objectID)
		return packets, fmt.Errorf("wallSchedule: %w", err)
	}
	return append(packets, segmentPackets...), nil
}

func (e *plasmaWallSegment) createPackets() ([][]byte, error) {
	delta := e.end.Sub(e.origin)
	center := e.origin.Add(e.end).Scale(0.5)
	rotation := raknet.Vector3{
		X: float32(math.Atan2(float64(delta.Z), math.Hypot(float64(delta.X), float64(delta.Y)))),
		Z: float32(math.Atan2(-float64(delta.X), float64(delta.Y)))}
	create, err := raknet.MarshalApplication(raknet.ObjectCreateMessage{
		ObjectID: e.objectID, Noun: util.HashID("FireWallSegment.Noun"),
		OwnerID: e.run.objectID, PositionX: center.X, PositionY: center.Y, PositionZ: center.Z,
		Rotation: rotation, Scale: delta.Length() / 2, Team: 0})
	if err != nil {
		return nil, fmt.Errorf("wallSpawn: %w", err)
	}
	// Chunk720 captures the creation scale for its box, then resets the
	// runtime object scale to one. Preserve both sides of that transition.
	transform, err := raknet.MarshalApplication(raknet.ForcePhysicsUpdateMessage{
		ObjectID: e.objectID, Position: raknet.Vector3{X: center.X, Y: center.Y, Z: center.Z},
		Rotation: rotation, Scale: raknet.Vector3{X: 1, Y: 1, Z: 1}})
	if err != nil {
		return nil, fmt.Errorf("wallScale: %w", err)
	}
	return [][]byte{create, transform}, nil
}

func (e *plasmaWallSegment) schedule(delay time.Duration) error {
	e.run.runtime.registry.mutex.Lock()
	defer e.run.runtime.registry.mutex.Unlock()
	member, isFound := e.run.runtime.registry.sessions[e.run.sessionKey]
	if !isFound || member.zone != e.run.zone || member.plasmaWall.segments[e.objectID] != e {
		return nil
	}
	err := e.scheduleLocked(delay)
	if err != nil {
		return fmt.Errorf("wallSchedule: %w", err)
	}
	return nil
}

func (e *plasmaWallSegment) scheduleLocked(delay time.Duration) error {
	cancel, err := scheduleNPCProducers(e.run.runtime.registry, e.run.packet,
		[]raknet.ScheduledPacketProducer{{Delay: delay, Produce: e.produce}})
	if err != nil {
		return fmt.Errorf("wallPoll: %w", err)
	}
	if cancel == nil {
		return fmt.Errorf("wallPoll: unavailable")
	}
	e.cancel = cancel
	return nil
}

func (e *plasmaWallSegment) produce() ([][]byte, error) {
	packets, err := e.step()
	if err == nil {
		return packets, nil
	}
	e.run.runtime.registry.mutex.Lock()
	member, isFound := e.run.runtime.registry.sessions[e.run.sessionKey]
	if isFound && member.plasmaWall.segments[e.objectID] == e {
		delete(member.plasmaWall.segments, e.objectID)
		if e.cancel != nil {
			e.cancel()
		}
		e.run.runtime.registry.sessions[e.run.sessionKey] = member
		deleted, deleteErr := raknet.MarshalApplication(raknet.ObjectDeleteMessage{ObjectID: []uint32{e.objectID}})
		if deleteErr != nil {
			err = errors.Join(err, fmt.Errorf("wallFailureDelete: %w", deleteErr))
		} else {
			e.run.runtime.registry.queueChannelPresentationLocked(e.run.zone, [][]byte{deleted})
		}
	}
	e.run.runtime.registry.mutex.Unlock()
	return packets, fmt.Errorf("wallStep: %w", err)
}

func (e *plasmaWallSegment) step() ([][]byte, error) {
	run := e.run
	run.runtime.registry.mutex.Lock()
	member, isFound := run.runtime.registry.sessions[run.sessionKey]
	isCurrent := isFound && member.generation == run.generation && member.zone == run.zone &&
		member.plasmaWall.segments[e.objectID] == e && !member.isZoneTerminal()
	if !isCurrent {
		run.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	now := run.runtime.now()
	if e.damagePollCount == 13 && !now.Before(e.damageAt) {
		delete(member.plasmaWall.segments, e.objectID)
		run.runtime.registry.sessions[run.sessionKey] = member
		run.runtime.registry.mutex.Unlock()
		deleted, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{ObjectID: []uint32{e.objectID}})
		if err != nil {
			return nil, fmt.Errorf("wallDelete: %w", err)
		}
		return [][]byte{deleted}, nil
	}
	// The segment's own passive rank is not promoted from its summoner;
	// CreateObject supplies no rank override and all burn rows are identical.
	contacts := make(map[uint32]bool)
	plans := []zonenpc.AttackPlan{}
	isDamageDue := !now.Before(e.damageAt)
	for _, plan := range plasmaWallContactPlans(member, e.objectID, run.objectID, e.origin, e.end, 0.1, 1) {
		contacts[plan.TargetObjectID] = true
		// The trigger-enter callback is independent of the thirteen authored
		// periodic polls. Leaving drops contact so a later entry can fire again.
		if !e.contacts[plan.TargetObjectID] {
			plans = append(plans, plan)
		}
		if isDamageDue {
			plans = append(plans, plan)
		}
	}
	e.contacts = contacts
	if isDamageDue {
		e.damagePollCount++
		e.damageAt = now.Add(time.Second)
	}
	timestamp := e.timestamp + uint64(now.Sub(e.startedAt)/time.Millisecond)
	run.runtime.registry.mutex.Unlock()
	packets := make([][]byte, 0)
	for _, plan := range plans {
		burnPackets, err := run.runtime.applyCampaignNPCDisease(nil, run.packet, run.sessionKey, run.generation,
			plan, timestamp, e.tickDamage)
		if err != nil {
			return packets, fmt.Errorf("wallBurn: %w", err)
		}
		packets = append(packets, burnPackets...)
	}
	// Contact entry is a bounded server reconstruction of the native volume
	// callback; its cadence does not add periodic damage applications.
	err := e.schedule(plasmaWallStep)
	if err != nil {
		return packets, fmt.Errorf("wallContinue: %w", err)
	}
	return packets, nil
}

func plasmaWallContactPlans(member gameplayPeerSession, sourceObjectID uint32, initiatorObjectID uint32,
	origin game.Vec3, end game.Vec3, width float32, rank int32,
) []zonenpc.AttackPlan {
	plans := []zonenpc.AttackPlan{}
	for _, target := range member.zone.LiveNPCTargets() {
		if target.HitPoint <= 0 || !plasmaWallContact(origin, end, target.Position, width, target.ActorFootprintRadius) {
			continue
		}
		plans = append(plans, zonenpc.AttackPlan{SourceObjectID: sourceObjectID, InitiatorObjectID: initiatorObjectID,
			TargetObjectID: target.ObjectID, SourcePosition: origin, TargetPosition: target.Position,
			Profile: zonenpc.PlasmaWallBurnProfile(rank)})
	}
	return plans
}

func plasmaWallContact(origin game.Vec3, end game.Vec3, target game.Vec3, width float32, footprint float32) bool {
	delta := end.Sub(origin)
	length := delta.Length()
	if length == 0 {
		return false
	}
	forward := delta.Scale(1 / length)
	right := game.Vec3{X: forward.Y, Y: -forward.X}
	if right.Length() == 0 {
		right.X = 1
	} else {
		right = right.Scale(1 / right.Length())
	}
	up := game.Vec3{X: right.Y * forward.Z, Y: -right.X * forward.Z,
		Z: right.X*forward.Y - right.Y*forward.X}
	relative := target.Sub(origin)
	along := relative.X*forward.X + relative.Y*forward.Y + relative.Z*forward.Z
	side := relative.X*right.X + relative.Y*right.Y + relative.Z*right.Z
	height := relative.X*up.X + relative.Y*up.Y + relative.Z*up.Z
	along -= max(float32(0), min(length, along))
	side -= max(-width, min(width, side))
	height -= max(-width, min(width, height))
	return along*along+side*side+height*height <= footprint*footprint
}

// Completed wall volumes belong to their thirteen-second passive, not the
// moving caster. Source death stops the motion but leaves those volumes alive;
// whole-session teardown retires both owners and all remaining presentation.
func (e *gameplayPeerSession) stopPlasmaWalls(sourceObjectID uint32) ([][]byte, error) {
	packets := make([][]byte, 0)
	for objectID, run := range e.plasmaWall.runs {
		if sourceObjectID != 0 && sourceObjectID != objectID {
			continue
		}
		run.stop()
		delete(e.plasmaWall.runs, objectID)
	}
	if sourceObjectID != 0 {
		return packets, nil
	}
	objectIDs := []uint32{}
	var registry *gameplaySessionRegistry
	for objectID, segment := range e.plasmaWall.segments {
		registry = segment.run.runtime.registry
		if segment.cancel != nil {
			segment.cancel()
		}
		objectIDs = append(objectIDs, objectID)
		delete(e.plasmaWall.segments, objectID)
	}
	if len(objectIDs) == 0 {
		return packets, nil
	}
	deleted, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{ObjectID: objectIDs})
	if err != nil {
		return nil, fmt.Errorf("wallsDelete: %w", err)
	}
	// The transport may be disappearing while teammates keep the zone. Do
	// not leave invisible, unticked orphan objects on those surviving clients.
	registry.queueChannelPresentationLocked(e.zone, [][]byte{deleted})
	return packets, nil
}
