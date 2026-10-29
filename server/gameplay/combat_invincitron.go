package gameplay

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zonedifficulty "github.com/darkspinnet/darkspin/server/zone/difficulty"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
	zoneprojection "github.com/darkspinnet/darkspin/server/zone/projection"
)

const (
	invincitronDroneOrbitRadius   = float32(2)
	invincitronDroneOrbitHeight   = float32(2)
	invincitronDroneOrbitDuration = 6 * time.Second
	invincitronDroneOrbitSpeed    = float32(2 * math.Pi * 2 / 6)
	invincitronDroneFollowDelay   = 100 * time.Millisecond
	invincitronDroneGoalLead      = 300 * time.Millisecond
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
	if !isOwnerFound || owner.IsDefeated || !isInvincitron {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	if peerSession.zone.NPCs().OwnedActiveCount(ownerObjectID) != 0 {
		existingPlans := make([]zonenpc.SpawnPlan, 0, 1)
		for _, child := range peerSession.zone.NPCs().LiveSnapshots() {
			if child.Plan.OwnerObjectID == ownerObjectID && campaignDifficultyNounFamily(child.Plan.NounName) == "nomaddrone" {
				existingPlans = append(existingPlans, child.Plan)
			}
		}
		r.registry.mutex.Unlock()
		err := r.startInvincitronDroneFollows(packet, sessionKey, generation, existingPlans)
		if err != nil {
			return nil, fmt.Errorf("droneFollowResume: %w", err)
		}
		return nil, nil
	}
	profile, err := zonenpc.NomadDroneLaserProfile(r.program.NomadDroneLaser)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("invincitronDroneProfile: %w", err)
	}
	profile.MovementSpeed = invincitronDroneOrbitSpeed
	profile.NonCombatMovementSpeed = invincitronDroneOrbitSpeed
	objectID, err := peerSession.reserveCampaignObjectID()
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("invincitronDroneReserve: %w", err)
	}
	footprintRadius, footprintErr := r.program.FootprintRadius("NomadDrone.Noun")
	if footprintErr != nil || footprintRadius <= 0 {
		footprintRadius = 0.5
	}
	authoredProfile, profileErr := zonedifficulty.ProjectOwnedSummon(peerSession.zone.DirectorDefinition(), "NomadDrone.Noun")
	if profileErr != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("droneClass: %w", profileErr)
	}
	authoredProfile.IsTargetable = false
	authoredProfile.FootprintRadius = footprintRadius
	plan := zonenpc.SpawnPlan{
		ObjectID: objectID, OwnerObjectID: ownerObjectID,
		NounName: "NomadDrone.Noun", Position: invincitronDronePosition(owner.Plan.Position, 0),
		LocusID: owner.Plan.LocusID, MarkerSetName: owner.Plan.MarkerSetName,
		IsEncounterAuxiliary: true,
		NPCProfile:           authoredProfile,
		ActionProfile:        profile, IsActionKnown: true,
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
	return append(packets, actionPackets...), nil
}

// startInvincitronDroneFollows also admits restored drones, whose former
// scheduler callbacks do not survive a checkpoint or transport replacement.
func (r campaignNPCActionRuntime) startInvincitronDroneFollows(
	packet raknet.Packet, sessionKey string, generation uint64, plans []zonenpc.SpawnPlan,
) error {
	for _, plan := range plans {
		if plan.OwnerObjectID == 0 || campaignDifficultyNounFamily(plan.NounName) != "nomaddrone" {
			continue
		}
		err := r.startInvincitronDroneFollow(packet.Autonomous(), sessionKey, generation, plan.ObjectID)
		if err != nil {
			return fmt.Errorf("droneFollowStart[%d]: %w", plan.ObjectID, err)
		}
	}
	return nil
}

func (r campaignNPCActionRuntime) startInvincitronDroneFollow(
	packet raknet.Packet, sessionKey string, generation uint64, objectID uint32,
) error {
	r.registry.mutex.Lock()
	member, isFound := r.registry.sessions[sessionKey]
	if !isFound || member.generation != generation || member.transportGeneration != packet.TransportGeneration ||
		member.zone == nil || member.squad == nil || member.isZoneTerminal() {
		r.registry.mutex.Unlock()
		return nil
	}
	drone, isDroneFound := member.zone.NPCs().NPC(objectID)
	owner, isOwnerFound := member.zone.NPCs().NPC(drone.Plan.OwnerObjectID)
	_, isInvincitron := zonenpc.NomadWithDroneShieldDuration(owner.Plan.NounName)
	if !isDroneFound || !isOwnerFound || !isInvincitron || drone.IsDefeated || owner.IsDefeated || !drone.IsPublished {
		r.registry.mutex.Unlock()
		return nil
	}
	// Several peers can discover the same actor. Keep one live zone callback,
	// while allowing an obsolete transport's registration to be replaced.
	for _, peer := range r.registry.sessions {
		if peer.zone != member.zone || peer.squad == nil || peer.isZoneTerminal() {
			continue
		}
		current := peer.droneFollows[objectID]
		if current != nil && current.generation == peer.generation && current.packet.TransportGeneration == peer.transportGeneration {
			r.registry.mutex.Unlock()
			return nil
		}
	}
	angle := math.Atan2(float64(drone.Plan.Position.Y-owner.Plan.Position.Y), float64(drone.Plan.Position.X-owner.Plan.Position.X))
	if angle < 0 {
		angle += 2 * math.Pi
	}
	follow := &campaignDroneFollowStep{
		runtime: r, packet: packet, sessionKey: sessionKey, generation: generation,
		objectID: objectID, ownerObjectID: drone.Plan.OwnerObjectID, advancedAt: r.now(),
		orbitElapsed: time.Duration(angle / (2 * math.Pi) * float64(invincitronDroneOrbitDuration)),
	}
	if member.droneFollows == nil {
		member.droneFollows = make(map[uint32]*campaignDroneFollowStep)
	}
	member.droneFollows[objectID] = follow
	r.registry.sessions[sessionKey] = member
	r.registry.mutex.Unlock()
	err := follow.schedule()
	if err != nil {
		r.registry.mutex.Lock()
		current, isCurrentFound := r.registry.sessions[sessionKey]
		if isCurrentFound && current.droneFollows[objectID] == follow {
			delete(current.droneFollows, objectID)
			r.registry.sessions[sessionKey] = current
		}
		r.registry.mutex.Unlock()
		return fmt.Errorf("droneFollowSchedule: %w", err)
	}
	return nil
}

type campaignDroneFollowStep struct {
	runtime       campaignNPCActionRuntime
	packet        raknet.Packet
	sessionKey    string
	generation    uint64
	objectID      uint32
	ownerObjectID uint32
	advancedAt    time.Time
	orbitElapsed  time.Duration
}

func (e *campaignDroneFollowStep) release() {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || member.droneFollows[e.objectID] != e {
		return
	}
	delete(member.droneFollows, e.objectID)
	e.runtime.registry.sessions[e.sessionKey] = member
}

// The packaged description requires an orbit, but its native trajectory is
// unrecovered. Keep the visual and authoritative firing origin on one circle.
func invincitronDronePosition(owner game.Vec3, elapsed time.Duration) game.Vec3 {
	angle := elapsed.Seconds() * 2 * math.Pi /
		invincitronDroneOrbitDuration.Seconds()
	return owner.Add(game.Vec3{
		X: invincitronDroneOrbitRadius * float32(math.Cos(angle)),
		Y: invincitronDroneOrbitRadius * float32(math.Sin(angle)),
		Z: invincitronDroneOrbitHeight,
	})
}

func invincitronDroneFacing(position game.Vec3, goal game.Vec3) raknet.Vector3 {
	delta := goal.Sub(position)
	length := float32(math.Sqrt(float64(
		delta.X*delta.X + delta.Y*delta.Y + delta.Z*delta.Z,
	)))
	if length == 0 {
		return raknet.Vector3{X: 1}
	}
	return raknet.Vector3{
		X: delta.X / length, Y: delta.Y / length, Z: delta.Z / length,
	}
}

func (e *campaignDroneFollowStep) schedule() error {
	cancel, err := scheduleNPCProducers(e.runtime.registry, e.packet, []raknet.ScheduledPacketProducer{{
		Delay: invincitronDroneFollowDelay, Produce: e.produce,
	}})
	if err != nil {
		return fmt.Errorf("droneFollowSchedule: %w", err)
	}
	if cancel == nil {
		return errors.New("drone follow cancellation unavailable")
	}
	return nil
}

func (e *campaignDroneFollowStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	// Orbiting belongs to the live drone, even between combat actions when
	// IsActionStarted and ActionOwner are cleared.
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.transportGeneration == e.packet.TransportGeneration &&
		peerSession.droneFollows[e.objectID] == e &&
		peerSession.squad != nil && peerSession.zone != nil &&
		!peerSession.isZoneTerminal() &&
		peerSession.zone.NPCs() != nil &&
		(peerSession.zone.Boss() == nil || !peerSession.zone.Boss().IsBeamOutCommitted())
	if !isCurrent {
		e.runtime.registry.mutex.RUnlock()
		e.release()
		return nil, nil
	}
	owner, isOwnerFound := peerSession.zone.NPCs().NPC(e.ownerObjectID)
	drone, isDroneFound := peerSession.zone.NPCs().NPC(e.objectID)
	if !isOwnerFound || !isDroneFound || owner.IsDefeated || drone.IsDefeated ||
		!drone.IsPublished || drone.HitPoint <= 0 ||
		drone.Plan.OwnerObjectID != e.ownerObjectID {
		e.runtime.registry.mutex.RUnlock()
		e.release()
		return nil, nil
	}
	now := e.runtime.now()
	interval := max(time.Duration(0), now.Sub(e.advancedAt))
	e.orbitElapsed += interval
	// Integrate the active interval so rage expiry cannot jump the orbit phase.
	activeInterval := min(interval, max(time.Duration(0), drone.Plan.InvincitronRage.ExpiresAt.Sub(e.advancedAt)))
	e.orbitElapsed += time.Duration(float64(activeInterval) * float64(drone.Plan.InvincitronRage.MovementBuff))
	e.advancedAt = now
	position := invincitronDronePosition(owner.Plan.Position, e.orbitElapsed)
	lead := invincitronDroneGoalLead
	if drone.Plan.InvincitronRage.IsActive(now) {
		lead = time.Duration(float64(lead) * float64(1+drone.Plan.InvincitronRage.MovementBuff))
	}
	goal := invincitronDronePosition(
		owner.Plan.Position, e.orbitElapsed+lead,
	)
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
		goalPosition := raknet.Vector3(goal)
		goalPacket, marshalErr := raknet.MarshalApplication(raknet.ObjectPlayerMoveMessage{
			ObjectID: e.objectID, GoalFlags: 0x01, GoalPosition: goalPosition,
			Facing: invincitronDroneFacing(position, goal),
		})
		if marshalErr != nil {
			return nil, fmt.Errorf("droneFollowGoal: %w", marshalErr)
		}
		locomotionPacket, marshalErr := raknet.MarshalApplication(
			raknet.LocomotionUnreliableMessage{
				ObjectID: e.objectID, GoalPosition: goalPosition,
			},
		)
		if marshalErr != nil {
			return nil, fmt.Errorf("droneFollowLocomotion: %w", marshalErr)
		}
		packets = append(packets, goalPacket, locomotionPacket)
	}
	err = e.schedule()
	if err != nil {
		e.release()
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
