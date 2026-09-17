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
	zoneprojection "github.com/darkspinnet/darkspin/server/zone/projection"
)

func (r campaignNPCActionRuntime) ensureInvincitronDrone(
	packet raknet.Packet, sessionKey string, generation uint64,
	ownerObjectID uint32, timestamp uint64,
) ([][]byte, error) {
	if sessionKey == "" || generation == 0 || ownerObjectID == 0 {
		return nil, errors.New("invincitron drone request invalid")
	}
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(
		generation, ownerObjectID,
	)
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	owner, isOwnerFound := peerSession.zone.NPCs().NPC(ownerObjectID)
	_, isInvincitron := zonenpc.NomadWithDroneShieldDuration(owner.Plan.NounName)
	if !isOwnerFound || owner.IsDefeated || !isInvincitron ||
		peerSession.zone.NPCs().OwnedActiveCount(ownerObjectID) != 0 {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	profile, err := zonenpc.NomadDroneLaserProfile(r.program.SentryDroneLaser)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("invincitronDroneProfile: %w", err)
	}
	objectID, err := peerSession.reserveCampaignObjectID()
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("invincitronDroneReserve: %w", err)
	}
	hitPoint := r.program.NonPlayerHitPoint[util.HashID("NomadDrone")]
	if hitPoint <= 0 {
		hitPoint = 1
	}
	footprintRadius, footprintErr := r.program.FootprintRadius("NomadDrone.Noun")
	if footprintErr != nil || footprintRadius <= 0 {
		footprintRadius = 0.5
	}
	plan := zonenpc.SpawnPlan{
		ObjectID: objectID, OwnerObjectID: ownerObjectID,
		NounName: "NomadDrone.Noun", Position: invincitronDronePosition(owner.Plan.Position, 0),
		LocusID: owner.Plan.LocusID, MarkerSetName: owner.Plan.MarkerSetName,
		IsRewardSuppressed: true,
		NPCProfile: game.CampaignNPCProfile{
			NPCRank:      owner.Plan.NPCProfile.NPCRank,
			IsTargetable: false, HitPoint: hitPoint,
			Mind:          owner.Plan.NPCProfile.Mind,
			GraphicsScale: 1, FootprintRadius: footprintRadius,
			DifficultyDamageMultiplier: owner.Plan.NPCProfile.DifficultyDamageMultiplier,
			IsKnown:                    true,
		},
		ActionProfile: profile, IsActionKnown: true,
	}
	err = peerSession.zone.NPCs().Add(
		[]zonenpc.SpawnPlan{plan}, owner.TargetObjectID,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("invincitronDroneAdd: %w", err)
	}
	err = peerSession.zone.PublishNPCSpawn(zoneprojection.NPCSpawn{
		Plans: []zonenpc.SpawnPlan{plan}, TargetObjectID: owner.TargetObjectID,
	}, peerSession.binding.UserID, generation)
	if err != nil {
		rollbackErr := peerSession.zone.NPCs().RollbackAdd([]zonenpc.SpawnPlan{plan})
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf(
			"invincitronDronePublish: %w", errors.Join(err, rollbackErr),
		)
	}
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	packets, err := npcraknet.TargetedSpawn(plan, owner.TargetObjectID)
	if err != nil {
		return nil, fmt.Errorf("invincitronDroneMarshal: %w", err)
	}
	actionPackets, err := r.scheduleFirstActions(
		packet, sessionKey, generation, []zonenpc.SpawnPlan{plan}, timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("invincitronDroneAction: %w", err)
	}
	follow := campaignDroneFollowStep{
		runtime: r, packet: packet.Autonomous(), sessionKey: sessionKey,
		generation: generation, objectID: objectID, ownerObjectID: ownerObjectID,
		startedAt: r.now(),
	}
	err = follow.schedule()
	if err != nil {
		return nil, fmt.Errorf("droneFollowStart: %w", err)
	}
	return append(packets, actionPackets...), nil
}

type campaignDroneFollowStep struct {
	runtime       campaignNPCActionRuntime
	packet        raknet.Packet
	sessionKey    string
	generation    uint64
	objectID      uint32
	ownerObjectID uint32
	startedAt     time.Time
}

// The packaged description requires an orbit, but its native trajectory is
// unrecovered. Keep the visual and authoritative firing origin on one circle.
func invincitronDronePosition(owner game.Vec3, elapsed time.Duration) game.Vec3 {
	angle := elapsed.Seconds() * 2 * math.Pi / 6
	return owner.Add(game.Vec3{
		X: 2 * float32(math.Cos(angle)),
		Y: 2 * float32(math.Sin(angle)),
		Z: 2,
	})
}

func (e campaignDroneFollowStep) schedule() error {
	cancel, err := e.packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: 100 * time.Millisecond, Produce: e.produce,
	}})
	if err != nil {
		return fmt.Errorf("droneFollowSchedule: %w", err)
	}
	if cancel == nil {
		return errors.New("drone follow cancellation unavailable")
	}
	return nil
}

func (e campaignDroneFollowStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(e.generation, e.objectID)
	if !isCurrent {
		e.runtime.registry.mutex.RUnlock()
		return nil, nil
	}
	owner, isOwnerFound := peerSession.zone.NPCs().NPC(e.ownerObjectID)
	drone, isDroneFound := peerSession.zone.NPCs().NPC(e.objectID)
	if !isOwnerFound || !isDroneFound || owner.IsDefeated {
		e.runtime.registry.mutex.RUnlock()
		return nil, nil
	}
	position := invincitronDronePosition(owner.Plan.Position, e.runtime.now().Sub(e.startedAt))
	isMoved := position != drone.Plan.Position
	var err error
	if isMoved {
		err = peerSession.zone.NPCs().SetPosition(e.objectID, position)
	}
	e.runtime.registry.mutex.RUnlock()
	if err != nil {
		return nil, fmt.Errorf("droneFollowPosition: %w", err)
	}
	packets := make([][]byte, 0, 2)
	if isMoved {
		position := raknet.Vector3(position)
		updatePacket, marshalErr := raknet.MarshalApplication(raknet.ObjectPositionUpdateMessage{
			ObjectID: e.objectID, PositionX: position.X, PositionY: position.Y,
			PositionZ: position.Z,
		})
		if marshalErr != nil {
			return nil, fmt.Errorf("droneFollowUpdate: %w", marshalErr)
		}
		goalPacket, marshalErr := raknet.MarshalApplication(raknet.ObjectPlayerMoveMessage{
			ObjectID: e.objectID, GoalFlags: 0x20, GoalPosition: position,
		})
		if marshalErr != nil {
			return nil, fmt.Errorf("droneFollowGoal: %w", marshalErr)
		}
		packets = append(packets, updatePacket, goalPacket)
	}
	err = e.schedule()
	if err != nil {
		return nil, fmt.Errorf("droneFollowNext: %w", err)
	}
	return packets, nil
}

func (r campaignNPCActionRuntime) syncInvincitronDroneTarget(
	sessionKey string, generation uint64, objectID uint32,
) error {
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	if !isCurrent {
		r.registry.mutex.RUnlock()
		return nil
	}
	drone, isDroneFound := peerSession.zone.NPCs().NPC(objectID)
	owner, isOwnerFound := peerSession.zone.NPCs().NPC(drone.Plan.OwnerObjectID)
	_, isInvincitron := zonenpc.NomadWithDroneShieldDuration(owner.Plan.NounName)
	if !isDroneFound || !isOwnerFound || owner.IsDefeated || !isInvincitron ||
		owner.TargetObjectID == 0 || drone.TargetObjectID == owner.TargetObjectID {
		r.registry.mutex.RUnlock()
		return nil
	}
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, owner.TargetObjectID,
	)
	if !isTargetFound {
		r.registry.mutex.RUnlock()
		return nil
	}
	_, _, err := peerSession.zone.NPCs().Retarget(objectID, zonenpc.Target{
		ObjectID: target.ObjectID, Position: target.Position,
		FootprintRadius: target.FootprintRadius,
		Faction:         zonenpc.FactionPlayerAligned,
		Owner: zonenpc.ActionOwner{
			UserID: target.UserID, PeerGeneration: target.PeerGeneration,
		},
		IsAlive: true,
	})
	r.registry.mutex.RUnlock()
	if err != nil {
		return fmt.Errorf("invincitronDroneRetarget: %w", err)
	}
	return nil
}
