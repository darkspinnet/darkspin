package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

// ScaldronTempleColumn, modifier chunk 278: random integer 5..8s rest,
// 3s warning, then RandomTeleport on living hostile objects within 8 units.
// Share the clock between zone members; each connection presents each phase
// once, while only that member's teleport is broadcast to its allies.
type warpTotemCycle struct {
	warningAt      time.Time
	dischargeAt    time.Time
	completedAt    time.Time
	cycle          uint64
	completedCycle uint64
}

func (e *warpTotemCycle) rest(random *sim.SimulatorRandom, now time.Time) {
	e.warningAt = now.Add(time.Duration(5+int(random.Float64()*4)) * time.Second)
	e.dischargeAt = e.warningAt.Add(3 * time.Second)
	e.cycle++
}

type warpTotemTeleport struct {
	objectID    uint32
	destination game.Vec3
	moveAt      time.Time
	resetAt     time.Time
	reservation zoneaction.ReleaseReservation
	isMoved     bool
}

func (e gameplayPendingRuntime) pollWarpTotems(packet raknet.Packet) ([][]byte, error) {
	key := packet.Address.String()
	e.registry.mutex.Lock()
	member, isFound := e.registry.sessions[key]
	if !isFound || member.transportGeneration != packet.TransportGeneration || member.zone == nil ||
		member.isRejoinPending || !member.stage.IsDungeon() || !member.dungeonSetup.IsCommitted() || member.isZoneTerminal() {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	now := e.now()
	peerPackets, err := member.advanceWarpTotemTeleport(now, packet.SourceTime)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("totemTeleportAdvance: %w", err)
	}
	packets, reactionPackets, err := e.updateWarpTotemsLocked(&member, now, packet.SourceTime)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("totemCycle: %w", err)
	}
	peerPackets = append(peerPackets, reactionPackets...)
	e.registry.sessions[key] = member
	e.registry.mutex.Unlock()
	err = publishCampaignPeersAfterCommit(e.registry, packet, peerPackets)
	if err != nil {
		return nil, fmt.Errorf("totemPublish: %w", err)
	}
	return append(packets, peerPackets...), nil
}

func (e gameplayPendingRuntime) updateWarpTotemsLocked(member *gameplayPeerSession, now time.Time, timestamp uint64) ([][]byte, [][]byte, error) {
	packets := make([][]byte, 0)
	peerPackets := make([][]byte, 0)
	if member.warpTotems == nil {
		member.warpTotems = make(map[uint32]*warpTotemCycle)
		member.warpTotemWarnings = make(map[uint32]uint64)
		member.warpTotemDischarges = make(map[uint32]uint64)
	}
	for _, source := range member.zone.NPCs().Snapshots() {
		if !zonenpc.IsWarpTotem(source.Plan) || !source.IsPublished || source.IsDefeated {
			continue
		}
		objectID := source.Plan.ObjectID
		state := member.warpTotems[objectID]
		if state == nil {
			for _, ally := range e.registry.sessions {
				if ally.zone == member.zone && ally.warpTotems[objectID] != nil {
					state = ally.warpTotems[objectID]
					break
				}
			}
			if state == nil {
				if member.zone.NPCRandom() == nil {
					return nil, nil, fmt.Errorf("totemRandom: random unavailable")
				}
				state = &warpTotemCycle{}
				state.rest(member.zone.NPCRandom(), now)
			}
			member.warpTotems[objectID] = state
		}
		if !now.Before(state.dischargeAt) {
			state.completedCycle = state.cycle
			state.completedAt = now
			state.rest(member.zone.NPCRandom(), now)
		}
		if !now.Before(state.warningAt) && member.warpTotemWarnings[objectID] != state.cycle {
			warning, err := warpTotemEffect(objectID, "effect_environment_Scaldron_tem_column.ServerEventDef")
			if err != nil {
				return nil, nil, fmt.Errorf("totemWarning: %w", err)
			}
			packets = append(packets, warning)
			member.warpTotemWarnings[objectID] = state.cycle
		}
		if state.completedCycle == 0 || member.warpTotemDischarges[objectID] == state.completedCycle ||
			!now.Before(state.completedAt.Add(time.Second)) {
			continue
		}
		discharge, err := warpTotemEffect(objectID, "effect_environment_Scaldron_tem_column_2.ServerEventDef")
		if err != nil {
			return nil, nil, fmt.Errorf("totemDischarge: %w", err)
		}
		packets = append(packets, discharge)
		member.warpTotemDischarges[objectID] = state.completedCycle
		reactionPackets, err := e.beginWarpTotemTeleport(member, source.Plan.Position, now, timestamp)
		if err != nil {
			return nil, nil, fmt.Errorf("totemTarget: %w", err)
		}
		peerPackets = append(peerPackets, reactionPackets...)
	}
	return packets, peerPackets, nil
}

// nEvent.Notify uses the instrument object, so the authored event supplies
// its local 3D sound and effects rather than a global sound/UI notification.
func warpTotemEffect(objectID uint32, name string) ([]byte, error) {
	asset := util.HashID(name)
	packet, err := raknet.MarshalApplication(raknet.ServerEventContractMessage{Asset: &asset, ObjectID: &objectID})
	if err != nil {
		return nil, fmt.Errorf("totemEvent: %w", err)
	}
	return packet, nil
}

func (e gameplayPendingRuntime) beginWarpTotemTeleport(member *gameplayPeerSession, source game.Vec3, now time.Time, timestamp uint64) ([][]byte, error) {
	if member.deployedObjectID == 0 || member.deployedHitPoint() <= 0 || member.isHeroSelectionPending ||
		member.warpTotemTeleport.objectID != 0 || e.damage.npc.isTargetStatusImmuneLocked(member, member.deployedObjectID) {
		return nil, nil
	}
	position := game.Vec3(member.playerPosition)
	if member.playerMotion != nil {
		sampled, err := member.playerMotion.SamplePosition(now)
		if err != nil {
			return nil, fmt.Errorf("totemTargetPosition: %w", err)
		}
		position = game.Vec3(sampled)
	}
	if position.Sub(source).Length() > 8 {
		return nil, nil
	}
	// RandomTeleport (chunk 77) chooses around the affected hero, not the
	// totem, using the hero's footprint, four attempts and the 2/8/10 limits.
	destination, isFound, err := zonenavigation.RandomTeleportDestination(member.zone.Navigation(), member.zone.NPCRandom(), zonenavigation.RandomTeleportRequest{
		SourcePosition: position, FootprintRadius: member.deployedCampaignNavigationRadius(),
		MinimumDistance: 2, NormalDistance: 8, MaximumDistance: 10,
	})
	if err != nil {
		return nil, fmt.Errorf("totemDestination: %w", err)
	}
	if !isFound {
		return nil, nil
	}
	stopFlags := uint32(0x20)
	goal := raknet.Vector3(position)
	stopPacket, err := raknet.MarshalApplication(raknet.LocomotionUpdateContractMessage{ObjectID: member.deployedObjectID,
		Locomotion: raknet.LocomotionReflection{GoalFlags: &stopFlags, GoalPosition: &goal, PartialGoalPosition: &goal},
	})
	if err != nil {
		return nil, fmt.Errorf("totemStop: %w", err)
	}
	reactionPacket, err := npcraknet.AnimationState(member.deployedObjectID, "react_teleported", timestamp)
	if err != nil {
		return nil, fmt.Errorf("totemReaction: %w", err)
	}
	exitPacket, err := npcraknet.PositionedEffect("warp_impact_exit_effect.ServerEventDef", destination)
	if err != nil {
		return nil, fmt.Errorf("totemExit: %w", err)
	}
	err = member.stopPlayerMovement(now)
	if err != nil {
		return nil, fmt.Errorf("totemStopMotion: %w", err)
	}
	interrupted := member.interruptBasicForMovement()
	if interrupted != nil {
		interrupted.Stop()
	}
	member.campaignPlayerPursuitSession().Cancel()
	member.resetAbilityRelease()
	reservation, isReserved := member.abilityReleaseSession().Reserve(now, 200*time.Millisecond)
	if !isReserved {
		return nil, fmt.Errorf("totemReserve: release unavailable")
	}
	member.warpTotemTeleport = warpTotemTeleport{objectID: member.deployedObjectID, destination: destination,
		moveAt: now.Add(100 * time.Millisecond), resetAt: now.Add(200 * time.Millisecond), reservation: reservation}
	return [][]byte{stopPacket, exitPacket, reactionPacket}, nil
}

func (e *gameplayPeerSession) advanceWarpTotemTeleport(now time.Time, timestamp uint64) ([][]byte, error) {
	state := e.warpTotemTeleport
	if state.objectID == 0 {
		return nil, nil
	}
	if state.objectID != e.deployedObjectID || e.deployedHitPoint() <= 0 || !e.abilityReleaseSession().IsCurrent(state.reservation) {
		e.warpTotemTeleport = warpTotemTeleport{}
		return nil, nil
	}
	packets := make([][]byte, 0)
	if !state.isMoved && !now.Before(state.moveAt) {
		movementPackets, err := marshalPlayerMovementCorrection(state.objectID, raknet.Vector3(state.destination), raknet.Quaternion{W: 1})
		if err != nil {
			return nil, fmt.Errorf("totemPlacement: %w", err)
		}
		err = e.teleportPlayer(now, raknet.Vector3(state.destination))
		if err != nil {
			return nil, fmt.Errorf("totemMove: %w", err)
		}
		err = e.syncZoneHeroPose()
		if err != nil {
			return nil, fmt.Errorf("totemPose: %w", err)
		}
		e.playerMovementGoal = raknet.Vector3(state.destination)
		packets = append(packets, movementPackets...)
		state.isMoved = true
		e.warpTotemTeleport = state
	}
	if !now.Before(state.resetAt) {
		resetPacket, err := npcraknet.ResetAnimation(state.objectID, timestamp)
		if err != nil {
			return nil, fmt.Errorf("totemReset: %w", err)
		}
		packets = append(packets, resetPacket)
		e.warpTotemTeleport = warpTotemTeleport{}
	}
	return packets, nil
}
