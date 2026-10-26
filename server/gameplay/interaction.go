package gameplay

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/navigation"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/sim/raknet103"
	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	actionraknet "github.com/darkspinnet/darkspin/server/zone/action/raknet103"
	zonecheckpoint "github.com/darkspinnet/darkspin/server/zone/checkpoint"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zoneinteract "github.com/darkspinnet/darkspin/server/zone/interact"
	interactraknet "github.com/darkspinnet/darkspin/server/zone/interact/raknet103"
	zoneloot "github.com/darkspinnet/darkspin/server/zone/loot"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	objectraknet "github.com/darkspinnet/darkspin/server/zone/object/raknet103"
	zoneobjective "github.com/darkspinnet/darkspin/server/zone/objective"
	objectiveraknet "github.com/darkspinnet/darkspin/server/zone/objective/raknet103"
	zoneunlock "github.com/darkspinnet/darkspin/server/zone/unlock"
	unlockraknet "github.com/darkspinnet/darkspin/server/zone/unlock/raknet103"
)

var errCampaignPickupNotFound = errors.New("campaign pickup not found")

const campaignPickupAbilityRange = float32(2)
const campaignPickupObjectFootprintRadius = float32(math.Sqrt2)

// Pickup requests carry a client pose that can trail the server-owned hero
// transform while the client is finishing its approach. Keep the authored
// two-unit surface range, but allow two units of replicated-pose drift so a
// marginal request completes instead of entering the client's unstable
// pickup-pursuit path.
const campaignPickupPoseTolerance = float32(2)

func (s *gameplayPeerSession) registerCampaignPickup(
	kind zoneinteract.PickupKind, objectID uint32,
	source sim.Position, destination sim.Position, flights ...sim.DropFlight,
) error {
	if s == nil {
		return errors.New("nil campaign pickup session")
	}
	if s.zone.Pickups() == nil {
		return errors.New("campaign pickup registry unavailable")
	}
	lifetime := time.Duration(0)
	switch kind {
	case zoneinteract.PickupCrystal:
		lifetime = 120 * time.Second
	case zoneinteract.PickupOrb:
		lifetime = campaignOrbLifetime
	}
	expiresAt := time.Duration(0)
	if lifetime > 0 {
		expiresAt = s.zone.Elapsed(time.Now()) + lifetime
	}
	pickup := zoneinteract.Pickup{
		ObjectID: objectID, Kind: kind, ExpiresAt: expiresAt,
		Position: game.Vec3{
			X: destination.X, Y: destination.Y, Z: destination.Z,
		},
		SourcePosition: game.Vec3{
			X: source.X, Y: source.Y, Z: source.Z,
		},
		IsSourcePositionKnown: true,
	}
	if len(flights) > 0 {
		pickup.Flight = flights[0]
		if !pickup.Flight.IsProjectilePresent {
			pickup.Position = game.Vec3(source)
		}
	}
	err := s.zone.Pickups().Register(pickup)
	if err != nil {
		return fmt.Errorf("pickupRegister: %w", err)
	}
	return nil
}

func (s *gameplayPeerSession) campaignPickupMaximumDistance() float32 {
	if s == nil {
		return campaignPickupAbilityRange + campaignPickupObjectFootprintRadius +
			campaignPickupPoseTolerance
	}
	return campaignPickupAbilityRange +
		max(float32(0), s.deployedCampaignFootprintRadius()) +
		campaignPickupObjectFootprintRadius + campaignPickupPoseTolerance
}

func (s *gameplayPeerSession) campaignPickupSurfaceDistance(distance float32) float32 {
	if s == nil {
		return max(
			float32(0),
			distance-campaignPickupObjectFootprintRadius-
				campaignPickupPoseTolerance,
		)
	}
	return max(
		float32(0),
		distance-s.deployedCampaignFootprintRadius()-
			campaignPickupObjectFootprintRadius-campaignPickupPoseTolerance,
	)
}

func (s *gameplayPeerSession) stopCampaignPickup(
	now time.Time,
) ([][]byte, error) {
	if s == nil {
		return nil, errors.New("nil campaign pickup session")
	}
	err := s.stopPlayerMovement(now)
	if err != nil {
		return nil, fmt.Errorf("pickupMovement: %w", err)
	}
	err = s.syncZoneHeroPose()
	if err != nil {
		return nil, fmt.Errorf("pickupHeroPose: %w", err)
	}
	packets, err := marshalZonePlayerStop(s.deployedObjectID, s.playerPosition)
	if err != nil {
		return nil, fmt.Errorf("pickupStopMarshal: %w", err)
	}
	return packets, nil
}

func (s *gameplayPeerSession) reserveCampaignPickup(
	command raknet.ActionCommandData, maximumDistance float32,
) (zoneinteract.Pickup, zoneinteract.PickupAdmission) {
	if s == nil || s.zone.Pickups() == nil {
		return zoneinteract.Pickup{}, zoneinteract.PickupRejectedNotFound
	}
	return s.zone.Pickups().Reserve(zoneinteract.PickupCommand{
		ActorObjectID: command.Common.ObjectID, ActiveObjectID: s.deployedObjectID,
		TargetObjectID: command.Value,
		ActorPosition: game.Vec3{
			X: s.playerPosition.X, Y: s.playerPosition.Y, Z: s.playerPosition.Z,
		},
		MaximumDistance: maximumDistance,
		SimulationTime:  s.zone.Elapsed(time.Now()),
	})
}

func (s *gameplayPeerSession) reserveCampaignPickupContact(
	objectID uint32, start raknet.Vector3, end raknet.Vector3, maximumDistance float32, now time.Time,
) (zoneinteract.Pickup, zoneinteract.PickupAdmission) {
	if s == nil || s.zone.Pickups() == nil {
		return zoneinteract.Pickup{}, zoneinteract.PickupRejectedNotFound
	}
	return s.zone.Pickups().ReserveContact(zoneinteract.PickupContactCommand{
		ActorObjectID: s.deployedObjectID, ActiveObjectID: s.deployedObjectID,
		TargetObjectID:  objectID,
		SegmentStart:    game.Vec3{X: start.X, Y: start.Y, Z: start.Z},
		SegmentEnd:      game.Vec3{X: end.X, Y: end.Y, Z: end.Z},
		MaximumDistance: maximumDistance,
		SimulationTime:  s.zone.Elapsed(now), SampleTime: now,
	})
}

func campaignPickupRejectionReason(admission zoneinteract.PickupAdmission) string {
	switch admission {
	case zoneinteract.PickupRejectedActor:
		return "actor unavailable"
	case zoneinteract.PickupRejectedReserved:
		return "already scheduled"
	case zoneinteract.PickupRejectedRange:
		return "out of range"
	default:
		return "pickup unavailable"
	}
}

func (r campaignInteractionRuntime) rejectPickup(
	command raknet.ActionCommandData, reason string,
) ([][]byte, error) {
	rejectionPacket, err := actionraknet.Reject(command)
	if err != nil {
		return nil, fmt.Errorf("campaignPickupReject: %w", err)
	}
	r.logger.Printf(
		"RakNet campaign pickup rejected source=%d target=%d reason=%s",
		command.Common.ObjectID, command.Value, reason,
	)
	return [][]byte{rejectionPacket}, nil
}

func (r campaignInteractionRuntime) pursuePickup(
	packet raknet.Packet, sessionKey string,
	command raknet.ActionCommandData, pickup zoneinteract.Pickup,
	maximumDistance float32,
) ([][]byte, error) {
	pursuitPackets, err := actionraknet.PursuitTransfer(
		command.Common.Unknown[0], command.Common.ObjectID,
	)
	if err != nil {
		return nil, fmt.Errorf("campaignPickupPursuit: %w", err)
	}
	redirectPackets, err := actionraknet.PursuitRedirect(
		command.Common.ObjectID, game.Vec3(command.Common.Position),
		command.Value, pickup.Position, max(float32(0.1), maximumDistance-1),
	)
	if err != nil {
		return nil, fmt.Errorf("pickupRedirect: %w", err)
	}
	r.registry.mutex.Lock()
	session, isSessionFound := r.registry.sessions[sessionKey]
	if !isSessionFound || !session.isPickupActorAvailable() {
		r.registry.mutex.Unlock()
		return r.rejectPickup(command, "pursuit actor unavailable")
	}
	if session.isEnemyRootActive(r.now()) {
		r.registry.mutex.Unlock()
		return r.rejectPickup(command, "cannot pursue pickup while rooted")
	}
	previousPosition, playerPosition, movementErr := session.advancePlayerPursuitMovement(
		r.now(), session.playerPosition, raknet.Vector3(pickup.Position),
		r.registry.passiveMovementIncrease(session),
	)
	if movementErr != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("pickupPursuitPath: %w", movementErr)
	}
	// Retain the authoritative approach so polling can finish the pickup
	// on arrival without requiring another click.
	session.playerPosition = playerPosition
	if previousPosition != playerPosition {
		poseErr := session.syncZoneHeroPose()
		if poseErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("pickupPursuitPose: %w", poseErr)
		}
	}
	r.registry.sessions[sessionKey] = session
	r.registry.mutex.Unlock()
	err = r.schedulePickupTimeout(packet, sessionKey, command)
	if err != nil {
		return r.rejectPickup(command, "pursuit scheduling unavailable")
	}
	pursuitPackets = append(pursuitPackets, redirectPackets...)
	distance := zoneability.Distance(
		game.Vec3{
			X: command.Common.Position.X,
			Y: command.Common.Position.Y,
			Z: command.Common.Position.Z,
		},
		pickup.Position,
	)
	if pickup.IsSourcePositionKnown {
		sourceDistance := zoneability.Distance(
			game.Vec3{
				X: command.Common.Position.X,
				Y: command.Common.Position.Y,
				Z: command.Common.Position.Z,
			},
			pickup.SourcePosition,
		)
		distance = min(distance, sourceDistance)
	}
	r.logger.Printf(
		"RakNet campaign pickup pursuit transferred source=%d target=%d distance=%g maximum=%g authored_range=%g pose_tolerance=%g",
		command.Common.ObjectID, command.Value, distance, maximumDistance,
		campaignPickupAbilityRange, campaignPickupPoseTolerance,
	)
	return pursuitPackets, nil
}

func (r campaignInteractionRuntime) handlePickup(
	ctx context.Context, packet raknet.Packet, command raknet.ActionCommandData, sessionKey string,
) ([][]byte, error) {
	r.registry.mutex.Lock()
	currentSession, isCurrentFound := r.registry.sessions[sessionKey]
	if !isCurrentFound ||
		command.Common.ObjectID != currentSession.deployedObjectID {
		r.registry.mutex.Unlock()
		return r.rejectPickup(command, "actor unavailable")
	}
	currentSession.pickupPursuit = nil
	r.registry.sessions[sessionKey] = currentSession
	positionErr := currentSession.advancePlayerPosition(
		r.now(), command.Common.Position,
	)
	if positionErr != nil {
		r.registry.mutex.Unlock()
		return r.rejectPickup(command, "position unavailable")
	}
	positionErr = currentSession.syncZoneHeroPose()
	if positionErr != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignPickupHeroPose: %w", positionErr)
	}
	r.registry.sessions[sessionKey] = currentSession
	abilityIndex := uint32(0)
	questCrystal, isQuestCrystal := currentSession.zone.QuestCrystals().Crystal(command.Value, r.now())
	if isQuestCrystal {
		maximumDistance := questCrystalDistance(currentSession)
		if zoneability.Distance(game.Vec3(currentSession.playerPosition), game.Vec3(questCrystal.Flight.Position)) > maximumDistance {
			r.registry.mutex.Unlock()
			return r.pursuePickup(packet, sessionKey, command, zoneinteract.Pickup{ObjectID: questCrystal.ObjectID,
				Kind: zoneinteract.PickupQuestCrystal, Position: game.Vec3(questCrystal.Flight.Position)}, maximumDistance)
		}
		return r.beginQuestCrystalPickupLocked(packet, command, sessionKey, currentSession, questCrystal)
	}
	if command.Ability != nil {
		abilityIndex = command.Ability.Index
	}
	orbPickup := zoneinteract.Orb{}
	isOrbPickup := false
	if currentSession.zone.Orbs() != nil {
		orbPickup, isOrbPickup = currentSession.zone.Orbs().Orb(command.Value)
	}
	if isOrbPickup && orbPickup.Request.Kind == sim.ResurrectionOrbDrop {
		return r.beginResurrectionPickupLocked(packet, command, sessionKey, currentSession, orbPickup)
	}
	equipmentPickup := zoneinteract.EquipmentPickup{}
	isEquipmentPickup := false
	if isCurrentFound && currentSession.zone.PickupPayload() != nil {
		equipmentPickup, isEquipmentPickup =
			currentSession.zone.PickupPayload().Equipment(command.Value)
	}
	if isCurrentFound && isEquipmentPickup {
		maximumDistance := currentSession.campaignPickupMaximumDistance()
		pickup, admission := currentSession.reserveCampaignPickup(
			command, maximumDistance,
		)
		if admission != zoneinteract.PickupAccepted {
			r.registry.mutex.Unlock()
			if admission == zoneinteract.PickupRejectedReserved {
				// The native client can repeat the click while the first accepted
				// pickup is waiting for its authored commit frame. Do not replace
				// that accepted presentation with a rejection, but do complete the
				// repeated command's own action-response exchange.
				r.logger.Printf(
					"RakNet campaign equipment pickup duplicate ignored source=%d target=%d",
					command.Common.ObjectID, command.Value,
				)
				duplicatePacket, duplicateErr := actionraknet.Accept(
					command, "PickUpLoot", packet.SourceTime,
					campaignEquipmentPickupCommitDelay, campaignEquipmentPickupDelay,
				)
				if duplicateErr != nil {
					return nil, fmt.Errorf("campaignEquipmentDuplicateMarshal: %w", duplicateErr)
				}
				return [][]byte{duplicatePacket}, nil
			}
			if admission == zoneinteract.PickupRejectedRange {
				return r.pursuePickup(packet, sessionKey, command, pickup, maximumDistance)
			}
			return r.rejectPickup(command, campaignPickupRejectionReason(admission))
		}
		if r.progression == nil {
			currentSession.zone.Pickups().Release(command.Value)
			r.registry.mutex.Unlock()
			return nil, errors.New("campaignEquipmentProgression: unavailable")
		}
		movementPackets, movementErr := currentSession.stopCampaignPickup(r.now())
		if movementErr != nil {
			currentSession.zone.Pickups().Release(command.Value)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignEquipmentStop: %w", movementErr)
		}
		deletePacket, marshalErr := interactraknet.DeletePickup(equipmentPickup.ObjectID)
		if marshalErr != nil {
			currentSession.zone.Pickups().Release(command.Value)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignEquipmentDeleteMarshal: %w", marshalErr)
		}
		if packet.ScheduleGroupResult == nil && packet.ScheduleGroup == nil {
			currentSession.zone.Pickups().Release(command.Value)
			r.registry.mutex.Unlock()
			return r.rejectPickup(command, "scheduler unavailable")
		}
		acceptPacket, marshalErr := actionraknet.Accept(
			command, "PickUpLoot", packet.SourceTime,
			campaignEquipmentPickupCommitDelay, campaignEquipmentPickupDelay,
		)
		if marshalErr != nil {
			currentSession.zone.Pickups().Release(command.Value)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignEquipmentAcceptMarshal: %w", marshalErr)
		}
		releasePacket, marshalErr := abilityraknet.ReleaseResponse(
			command.Common.Unknown[0], util.HashID("PickUpLoot"),
			abilityIndex, packet.SourceTime,
			campaignEquipmentPickupCommitDelay, campaignEquipmentPickupDelay,
		)
		if marshalErr != nil {
			currentSession.zone.Pickups().Release(command.Value)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignEquipmentReleaseMarshal: %w", marshalErr)
		}
		rejectPacket, marshalErr := actionraknet.Reject(command)
		if marshalErr != nil {
			currentSession.zone.Pickups().Release(command.Value)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignEquipmentRejectMarshal: %w", marshalErr)
		}
		animationPacket, marshalErr := abilityraknet.Animation(
			currentSession.deployedObjectID, campaignEquipmentPickupAnimation, packet.SourceTime,
		)
		if marshalErr != nil {
			currentSession.zone.Pickups().Release(command.Value)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignEquipmentAnimation: %w", marshalErr)
		}
		generation := currentSession.generation
		objectID := equipmentPickup.ObjectID
		step := &campaignEquipmentPickupStep{
			runtime: r, ctx: context.WithoutCancel(ctx), sessionKey: sessionKey,
			generation: generation, actorObjectID: command.Common.ObjectID,
			pickup: equipmentPickup, deletePacket: deletePacket,
			releasePacket: releasePacket, rejectPacket: rejectPacket,
			progression: r.progression,
		}
		// Both waits are absolute offsets from the same accepted action.
		producers := []raknet.ScheduledPacketProducer{
			{Delay: campaignEquipmentPickupCommitDelay, Produce: step.produce},
			{Delay: campaignEquipmentPickupDelay, Produce: step.finish},
		}
		scheduleFailure := campaignEquipmentPickupFailure{
			runtime: r, sessionKey: sessionKey,
			generation: generation, objectID: objectID,
		}
		var equipmentCancel raknet.CancelSchedule
		var scheduleErr error
		if packet.ScheduleGroupResult != nil {
			equipmentCancel, scheduleErr = packet.ScheduleGroupResult(
				producers,
				scheduleFailure.handle,
			)
		} else {
			equipmentCancel, scheduleErr = packet.ScheduleGroup(
				producers,
			)
		}
		if scheduleErr != nil || equipmentCancel == nil {
			currentSession.zone.Pickups().Release(objectID)
			r.registry.mutex.Unlock()
			if scheduleErr == nil {
				scheduleErr = errors.New("nil cancellation")
			}
			r.logger.Printf(
				"RakNet campaign equipment pickup scheduler rejected source=%d target=%d: %v",
				command.Common.ObjectID, objectID, scheduleErr,
			)
			return r.rejectPickup(command, "scheduler rejected")
		}
		scheduleErr = currentSession.campaignScheduleSession().Add(
			zoneaction.ScheduleEquipment, objectID, nil, equipmentCancel,
			&campaignPickupScheduleCleaner{
				pickup: currentSession.zone.Pickups(), objectID: objectID,
			},
		)
		if scheduleErr != nil {
			equipmentCancel()
			currentSession.zone.Pickups().Release(objectID)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignEquipmentTrack: %w", scheduleErr)
		}
		for candidateSessionKey, candidate := range r.registry.sessions {
			if candidateSessionKey == sessionKey || candidate.zone != currentSession.zone {
				continue
			}
			publishErr := candidate.publishPackets([][]byte{animationPacket})
			if publishErr != nil && r.logger != nil {
				r.logger.Printf("RakNet campaign equipment picker animation queued user=%d: %v",
					candidate.binding.UserID, publishErr)
			}
			r.registry.sessions[candidateSessionKey] = candidate
		}
		r.registry.sessions[sessionKey] = currentSession
		r.registry.mutex.Unlock()
		r.logger.Printf(
			"RakNet campaign equipment pickup accepted source=%d target=%d",
			command.Common.ObjectID, objectID,
		)
		responsePackets := append([][]byte{acceptPacket}, movementPackets...)
		return append(responsePackets, animationPacket), nil
	}
	crystalPickup := zoneinteract.CrystalPickup{}
	isCrystalPickup := false
	if isCurrentFound && currentSession.zone.PickupPayload() != nil {
		crystalPickup, isCrystalPickup =
			currentSession.zone.PickupPayload().Crystal(command.Value)
	}
	if isCurrentFound && isCrystalPickup {
		if !zoneunlock.AreCatalystDropsUnlocked(currentSession.binding) {
			r.registry.mutex.Unlock()
			return r.rejectPickup(command, "catalysts locked")
		}
		if !crystalPickup.Object.IsLive {
			r.registry.mutex.Unlock()
			return r.rejectPickup(command, "crystal unavailable")
		}
		maximumDistance := currentSession.campaignPickupMaximumDistance()
		pickup, admission := currentSession.reserveCampaignPickup(
			command, maximumDistance,
		)
		if admission != zoneinteract.PickupAccepted {
			r.registry.mutex.Unlock()
			if admission == zoneinteract.PickupRejectedRange {
				return r.pursuePickup(packet, sessionKey, command, pickup, maximumDistance)
			}
			return r.rejectPickup(command, campaignPickupRejectionReason(admission))
		}
		distance := zoneability.Distance(
			game.Vec3{
				X: currentSession.playerPosition.X, Y: currentSession.playerPosition.Y,
				Z: currentSession.playerPosition.Z,
			}, game.Vec3{
				X: crystalPickup.Object.Position.X, Y: crystalPickup.Object.Position.Y,
				Z: crystalPickup.Object.Position.Z,
			},
		)
		sourceDistance := zoneability.Distance(
			game.Vec3{
				X: currentSession.playerPosition.X, Y: currentSession.playerPosition.Y,
				Z: currentSession.playerPosition.Z,
			},
			game.Vec3{
				X: crystalPickup.Request.Position.X,
				Y: crystalPickup.Request.Position.Y,
				Z: crystalPickup.Request.Position.Z,
			},
		)
		distance = min(distance, sourceDistance)
		pendingInventory, isInventoryFound := currentSession.zone.CrystalInventory(
			currentSession.binding.UserID, currentSession.generation,
		)
		if !isInventoryFound {
			currentSession.zone.Pickups().Release(command.Value)
			r.registry.mutex.Unlock()
			return nil, errors.New("campaignCrystalInventory: unavailable")
		}
		previousInventory := pendingInventory
		pendingPickup := crystalPickup.Object
		// Each pickup interaction owns a fresh simulator clock. Admission takes
		// one second, longer than the retained half-second lob, so the pickup is
		// stationary again by this run's release boundary.
		pendingPickup.LobEnd = 0
		crystalRun, crystalErr := interactraknet.NewCrystalPickupRun(
			r.crystalPickup, sim.CrystalPickupCommand{
				PlayerRole: "player", PlayerIndex: uint8(currentSession.binding.Slot),
				AgentRole: "playerAgent", TargetRole: crystalPickup.Object.Role,
				RangeDistance: currentSession.campaignPickupSurfaceDistance(distance),
				IsAgentOwned:  true, IsTargetLive: true,
				IsTargetPhaseOwned: true, IsLootDataPresent: true, IsAbleToHit: true,
				SlotCount:          int(currentSession.binding.CatalystSlotCount),
				IsDiagonalUnlocked: currentSession.binding.IsDiagonalCatalystUnlocked,
			}, &pendingInventory, &pendingPickup,
		)
		if crystalErr != nil {
			currentSession.zone.Pickups().Release(command.Value)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignCrystalRun: %w", crystalErr)
		}
		if crystalRun.Admission() != sim.CrystalPickupAccepted {
			crystalRun.Stop()
			currentSession.zone.Pickups().Release(command.Value)
			r.registry.mutex.Unlock()
			return r.rejectPickup(command, "admission rejected")
		}
		if packet.ScheduleGroupResult == nil && packet.ScheduleGroup == nil {
			crystalRun.Stop()
			currentSession.zone.Pickups().Release(command.Value)
			r.registry.mutex.Unlock()
			return r.rejectPickup(command, "scheduler unavailable")
		}
		movementPackets, movementErr := currentSession.stopCampaignPickup(r.now())
		if movementErr != nil {
			crystalRun.Stop()
			currentSession.zone.Pickups().Release(command.Value)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("crystalPickupPose: %w", movementErr)
		}
		animationPacket, animationErr := abilityraknet.Animation(
			currentSession.deployedObjectID, "pickup_catalyst", packet.SourceTime,
		)
		if animationErr != nil {
			crystalRun.Stop()
			currentSession.zone.Pickups().Release(command.Value)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("crystalPickupAnimation: %w", animationErr)
		}
		acceptPacket, marshalErr := actionraknet.Accept(
			command, "PickUpCrystal", packet.SourceTime,
			300*time.Millisecond, zoneinteract.CrystalPickupReleaseDuration,
		)
		if marshalErr != nil {
			crystalRun.Stop()
			currentSession.zone.Pickups().Release(command.Value)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignCrystalAcceptMarshal: %w", marshalErr)
		}
		releasePacket, marshalErr := abilityraknet.ReleaseResponse(
			command.Common.Unknown[0], util.HashID("PickUpCrystal"),
			abilityIndex, packet.SourceTime, 300*time.Millisecond,
			zoneinteract.CrystalPickupReleaseDuration,
		)
		if marshalErr != nil {
			crystalRun.Stop()
			currentSession.zone.Pickups().Release(command.Value)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignCrystalReleaseMarshal: %w", marshalErr)
		}
		generation := currentSession.generation
		objectID := command.Value
		step := campaignCrystalPickupStep{
			runtime: r, ctx: ctx, sessionKey: sessionKey,
			generation: generation, objectID: objectID, run: crystalRun,
			previousInventory: previousInventory,
			pendingInventory:  &pendingInventory, pendingPickup: &pendingPickup,
			pickupPosition:  crystalPickup.Object.Position,
			isLootBagReward: crystalPickup.IsLootBagReward,
			releasePacket:   releasePacket,
		}
		producer := raknet.ScheduledPacketProducer{
			Delay:   time.Second,
			Produce: step.produce,
		}
		scheduleFailure := campaignCrystalPickupFailure{
			runtime: r, sessionKey: sessionKey,
			generation: generation, objectID: objectID, run: crystalRun,
		}
		var crystalCancel raknet.CancelSchedule
		if packet.ScheduleGroupResult != nil {
			crystalCancel, crystalErr = packet.ScheduleGroupResult(
				[]raknet.ScheduledPacketProducer{producer},
				scheduleFailure.handle,
			)
		} else {
			crystalCancel, crystalErr = packet.ScheduleGroup([]raknet.ScheduledPacketProducer{producer})
		}
		if crystalErr != nil || crystalCancel == nil {
			crystalRun.Stop()
			currentSession.zone.Pickups().Release(objectID)
			r.registry.mutex.Unlock()
			if crystalErr == nil {
				crystalErr = errors.New("nil cancellation")
			}
			r.logger.Printf(
				"RakNet campaign crystal pickup scheduler rejected source=%d target=%d: %v",
				command.Common.ObjectID, objectID, crystalErr,
			)
			return r.rejectPickup(command, "scheduler rejected")
		}
		crystalErr = currentSession.campaignScheduleSession().Add(
			zoneaction.ScheduleCrystal, objectID, crystalRun, crystalCancel,
			&campaignPickupScheduleCleaner{
				pickup: currentSession.zone.Pickups(), objectID: objectID,
			},
		)
		if crystalErr != nil {
			crystalCancel()
			crystalRun.Stop()
			currentSession.zone.Pickups().Release(objectID)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignCrystalTrack: %w", crystalErr)
		}
		r.registry.sessions[sessionKey] = currentSession
		peerPackets := append(append([][]byte(nil), movementPackets...), animationPacket)
		for candidateKey, candidate := range r.registry.sessions {
			if candidateKey == sessionKey || candidate.zone != currentSession.zone {
				continue
			}
			publishErr := candidate.publishPackets(peerPackets)
			if publishErr != nil && r.logger != nil {
				r.logger.Printf("RakNet catalyst animation peer delivery queued user=%d: %v",
					candidate.binding.UserID, publishErr)
			}
			r.registry.sessions[candidateKey] = candidate
		}
		r.registry.mutex.Unlock()
		r.logger.Printf(
			"RakNet campaign crystal pickup accepted source=%d target=%d distance=%g surface=%g authored_range=%g pose_tolerance=%g",
			command.Common.ObjectID, objectID, distance,
			currentSession.campaignPickupSurfaceDistance(distance),
			campaignPickupAbilityRange, campaignPickupPoseTolerance,
		)
		return append([][]byte{acceptPacket}, movementPackets...), nil
	}
	r.registry.mutex.Unlock()
	return nil, errCampaignPickupNotFound
}

func (r campaignInteractionRuntime) equipmentRollParticipantsLocked(
	currentSession gameplayPeerSession,
) []zoneloot.EquipmentRollParticipant {
	participants := make([]zoneloot.EquipmentRollParticipant, 0)
	for _, candidate := range r.registry.sessions {
		if candidate.zone != currentSession.zone || candidate.binding.UserID == 0 ||
			candidate.isZoneTerminal() {
			continue
		}
		participants = append(participants, zoneloot.EquipmentRollParticipant{
			UserID:   candidate.binding.UserID,
			ObjectID: candidate.deployedObjectID,
			Slot:     candidate.binding.Slot,
		})
	}
	sort.Slice(participants, func(left int, right int) bool {
		return participants[left].Slot < participants[right].Slot
	})
	return participants
}

const campaignEquipmentPickupDelay = 400 * time.Millisecond
const campaignEquipmentPickupCommitDelay = 100 * time.Millisecond
const campaignEquipmentPickupAnimation = "pickup"

// Compatibility/default debug budget only; resolved NPCs use class challenge.
const campaignNPCEquipmentSourceAmount = 10
const campaignLootObeliskDropMask = uint32(12) // Authored Loot | Crystal request.
// Compatibility budget only for nouns without a resolved NonPlayerClass.
const campaignNPCOrbFallbackChallenge = int32(25)
const campaignBossLimitedEditionChanceBasis = uint32(100)
const campaignBossLimitedEditionChanceThreshold = uint32(10)
const campaignBossLimitedEditionPityMaximumMisses = uint32(11)
const campaignEquipmentPityMaximumMisses = uint32(30)
const campaignEquipmentPityScalePerMiss = float32(0.10)

var campaignBossLimitedEditionRigblockIDs = [...]uint16{10782, 10783, 10784}

const (
	campaignCrystalFindAttribute = 65
	campaignDNADroppedAttribute  = 66
	campaignLootFindAttribute    = 71
	campaignOrbEffectAttribute   = 68
)

func (s *gameplayPeerSession) campaignPartAttribute(attributeIndex int) float32 {
	if s == nil || s.deployedCreatureIndex >= uint32(len(s.binding.Creatures)) ||
		attributeIndex < 0 || attributeIndex >= len(s.binding.Creatures[s.deployedCreatureIndex].PartAttribute) {
		return 0
	}
	return s.binding.Creatures[s.deployedCreatureIndex].PartAttribute[attributeIndex]
}

func (s *gameplayPeerSession) campaignPartSubjects() []game.GameplayCreature {
	if s == nil {
		return nil
	}
	partSubjects := make([]game.GameplayCreature, 0, len(s.binding.Creatures))
	for _, creature := range s.binding.Creatures {
		if creature.Noun != 0 && creature.ClassType != "" && creature.ElementType != "" {
			partSubjects = append(partSubjects, creature)
		}
	}
	if len(partSubjects) != 0 {
		return partSubjects
	}
	for _, creature := range s.binding.ActivatedCreatures {
		if creature.Noun != 0 && creature.ClassType != "" && creature.ElementType != "" {
			partSubjects = append(partSubjects, creature)
		}
	}
	return partSubjects
}

type campaignLootProgression interface {
	LimitedEditionPityStatus(
		context.Context, int64,
	) (sporenet.LimitedEditionPity, error)
}

type campaignInventoryReader interface {
	PartInventoryStatus(context.Context, int64) (sporenet.PartInventoryStatus, error)
}

type campaignEquipmentRoll struct {
	Part                     sporenet.Part
	PartSubject              game.GameplayCreature
	ChanceDraw               float64
	ChanceScale              float32
	ChanceThreshold          float32
	PartChoice               uint32
	PartSubjectIndex         int
	PartSubjectCount         int
	LimitedEditionDraw       uint32
	LimitedEditionRigblockID uint16
	RequestedSlotType        string
	IsNaturalDrop            bool
	IsLimitedEditionRolled   bool
}

type campaignEquipmentDropBag struct {
	missCount uint32
}

type campaignWeaponSubjectBag struct {
	usedCreatureIDs map[uint32]struct{}
	lastCreatureID  uint32
}

func (e campaignWeaponSubjectBag) Clone() campaignWeaponSubjectBag {
	clone := campaignWeaponSubjectBag{lastCreatureID: e.lastCreatureID}
	if len(e.usedCreatureIDs) == 0 {
		return clone
	}
	clone.usedCreatureIDs = make(map[uint32]struct{}, len(e.usedCreatureIDs))
	for creatureID := range e.usedCreatureIDs {
		clone.usedCreatureIDs[creatureID] = struct{}{}
	}
	return clone
}

func (e *campaignWeaponSubjectBag) selectSubject(
	partSubjects []game.GameplayCreature, choice uint32,
) (game.GameplayCreature, bool) {
	candidateIndexes := make([]int, 0, len(partSubjects))
	for subjectIndex, partSubject := range partSubjects {
		if _, isUsed := e.usedCreatureIDs[partSubject.ID]; !isUsed {
			candidateIndexes = append(candidateIndexes, subjectIndex)
		}
	}
	isRefill := len(candidateIndexes) == 0
	if isRefill {
		for subjectIndex, partSubject := range partSubjects {
			if len(partSubjects) > 1 && partSubject.ID == e.lastCreatureID {
				continue
			}
			candidateIndexes = append(candidateIndexes, subjectIndex)
		}
	}
	if len(candidateIndexes) == 0 {
		candidateIndexes = append(candidateIndexes, 0)
	}
	subjectIndex := candidateIndexes[choice%uint32(len(candidateIndexes))]
	return partSubjects[subjectIndex], isRefill
}

func (e *campaignWeaponSubjectBag) recordSubject(creatureID uint32, isRefill bool) {
	if isRefill || e.usedCreatureIDs == nil {
		e.usedCreatureIDs = make(map[uint32]struct{})
	}
	e.usedCreatureIDs[creatureID] = struct{}{}
	e.lastCreatureID = creatureID
}

type campaignEquipmentSlotCandidate struct {
	Part         sporenet.Part
	PartSubject  game.GameplayCreature
	SubjectIndex int
}

type campaignPartBagCommit struct {
	partSlotBag             game.CampaignPartSlotBag
	weaponSubjectBag        campaignWeaponSubjectBag
	partRarityBag           game.CampaignPartRarityBag
	limitedEditionPity      sporenet.LimitedEditionPity
	isPending               bool
	isLimitedEditionPending bool
}

func (s *gameplayPeerSession) materializeCampaignWinnerPart(
	ctx context.Context,
	gameplayJoin *game.GameplayJoin,
	progression campaignLootProgression,
	pickup zoneinteract.EquipmentPickup,
) (sporenet.Part, campaignPartBagCommit, error) {
	commit := campaignPartBagCommit{}
	if s == nil || gameplayJoin == nil || progression == nil || !pickup.IsWinnerReward {
		return sporenet.Part{}, commit, errors.New("campaign winner reward unavailable")
	}
	// NPC, captain and Destructor drops already publish a complete item in
	// cLootData. Native AddLootToPlayer (103 sub_9C3740) retains its rigblock,
	// rarity and affixes; choosing the recipient must not reroll that preview.
	// Deferred reward generation is only valid for an unmaterialized pickup.
	if pickup.Part.RigblockAssetID != 0 {
		err := gameplayJoin.ValidateGeneratedCampaignPart(pickup.Part)
		if err != nil {
			return sporenet.Part{}, commit, fmt.Errorf("winnerPublishedPart: %w", err)
		}
		return pickup.Part, commit, nil
	}
	partSubjects := s.campaignPartSubjects()
	if len(partSubjects) == 0 {
		return sporenet.Part{}, commit, errors.New("campaign winner roster unavailable")
	}
	commit.partSlotBag = s.campaignPartSlotBag.Clone()
	commit.weaponSubjectBag = s.campaignWeaponSubjectBag.Clone()
	commit.partRarityBag = s.campaignPartRarityBag.Clone()
	commit.partRarityBag.IsDestructorReward = pickup.IsDestructorReward
	partSubject, isSubjectRefill := commit.weaponSubjectBag.selectSubject(
		partSubjects, pickup.WinnerRewardChoice,
	)
	var part sporenet.Part
	var err error
	if pickup.IsWinnerRewardBoss {
		pity, pityErr := progression.LimitedEditionPityStatus(
			ctx, int64(s.binding.UserID),
		)
		if pityErr != nil {
			return sporenet.Part{}, campaignPartBagCommit{}, fmt.Errorf(
				"winnerLimitedStatus: %w", pityErr,
			)
		}
		commit.limitedEditionPity = pity
		pending := s.zone.MissionEquipment(s.binding.UserID)
		if pending.IsLimitedEditionPending {
			pity = sporenet.LimitedEditionPity{
				MissCount: pending.LimitedEditionMissCount, UsedMask: pending.LimitedEditionUsedMask,
			}
			commit.limitedEditionPity = pity
		}
		commit.isLimitedEditionPending = true
		isLimitedEdition := pity.MissCount >= campaignBossLimitedEditionPityMaximumMisses ||
			pickup.WinnerRewardChoice%campaignBossLimitedEditionChanceBasis <
				campaignBossLimitedEditionChanceThreshold
		if isLimitedEdition {
			part, partSubject, err = materializeCampaignLimitedEditionPart(
				gameplayJoin, partSubjects, pickup.WinnerRewardDifficulty,
				pickup, pity, &commit.partRarityBag,
			)
			if err != nil {
				return sporenet.Part{}, campaignPartBagCommit{}, fmt.Errorf("winnerLimitedGenerate: %w", err)
			}
			commit.limitedEditionPity.MissCount = 0
			allLimitedEditionMask := uint32(
				(1 << len(campaignBossLimitedEditionRigblockIDs)) - 1,
			)
			if commit.limitedEditionPity.UsedMask&allLimitedEditionMask ==
				allLimitedEditionMask {
				commit.limitedEditionPity.UsedMask = 0
			}
			for rigblockIndex, rigblockID := range campaignBossLimitedEditionRigblockIDs {
				if part.RigblockAssetID == rigblockID {
					commit.limitedEditionPity.UsedMask |= 1 << rigblockIndex
					break
				}
			}
		}
		if part.RigblockAssetID == 0 {
			commit.limitedEditionPity.MissCount = min(
				pity.MissCount+1, campaignBossLimitedEditionPityMaximumMisses,
			)
			part, err = gameplayJoin.GenerateCampaignPartFromBag(
				partSubject, pickup.WinnerRewardDifficulty, pickup.WinnerRewardDifficulty,
				pickup.WinnerRewardChoice, &commit.partSlotBag, &commit.partRarityBag, pickup.IsWinnerRewardBoss,
			)
		}
	} else if pickup.WinnerRewardRigblockID != 0 {
		part, err = gameplayJoin.GenerateCampaignSpecialPartFromBag(
			partSubject, pickup.WinnerRewardDifficulty, pickup.WinnerRewardDifficulty,
			pickup.WinnerRewardChoice, pickup.WinnerRewardRigblockID,
			&commit.partRarityBag, pickup.IsWinnerRewardBoss,
		)
	} else {
		part, err = gameplayJoin.GenerateCampaignPartFromBag(
			partSubject, pickup.WinnerRewardDifficulty, pickup.WinnerRewardDifficulty,
			pickup.WinnerRewardChoice, &commit.partSlotBag, &commit.partRarityBag, pickup.IsWinnerRewardBoss,
		)
	}
	if err != nil {
		return sporenet.Part{}, campaignPartBagCommit{}, fmt.Errorf(
			"winnerPartGenerate: %w", err,
		)
	}
	err = gameplayJoin.ValidateGeneratedCampaignPart(part)
	if err != nil {
		return sporenet.Part{}, campaignPartBagCommit{}, fmt.Errorf("winnerPartComplete: %w", err)
	}
	partDefinition, isPartDefined := gameplayJoin.CampaignPartDefinition(part.RigblockAssetID)
	commit.partRarityBag.IsDestructorReward = false
	if isPartDefined && partDefinition.SlotType == "weapon" {
		commit.weaponSubjectBag.recordSubject(partSubject.ID, isSubjectRefill)
	}
	commit.isPending = true
	return part, commit, nil
}

func materializeCampaignLimitedEditionPart(
	gameplayJoin *game.GameplayJoin,
	partSubjects []game.GameplayCreature,
	campaignStage uint32,
	pickup zoneinteract.EquipmentPickup,
	pity sporenet.LimitedEditionPity,
	rarityBag *game.CampaignPartRarityBag,
) (sporenet.Part, game.GameplayCreature, error) {
	usedMask := pity.UsedMask & ((1 << len(campaignBossLimitedEditionRigblockIDs)) - 1)
	if usedMask == (1<<len(campaignBossLimitedEditionRigblockIDs))-1 {
		usedMask = 0
	}
	startIndex := int(
		(pickup.WinnerRewardChoice / campaignBossLimitedEditionChanceBasis) %
			uint32(len(campaignBossLimitedEditionRigblockIDs)),
	)
	var candidateErr error
	for passIndex := 0; passIndex < 2; passIndex++ {
		excludedMask := usedMask
		if passIndex == 1 {
			if usedMask == 0 {
				break
			}
			excludedMask = 0
		}
		for offset := range campaignBossLimitedEditionRigblockIDs {
			rigblockIndex := (startIndex + offset) % len(campaignBossLimitedEditionRigblockIDs)
			if excludedMask&(1<<rigblockIndex) != 0 {
				continue
			}
			for subjectOffset := range partSubjects {
				subjectIndex := (startIndex + subjectOffset) % len(partSubjects)
				pendingRarityBag := rarityBag.Clone()
				part, err := gameplayJoin.GenerateCampaignSpecialPartFromBag(
					partSubjects[subjectIndex], pickup.WinnerRewardDifficulty,
					campaignStage, pickup.WinnerRewardChoice,
					campaignBossLimitedEditionRigblockIDs[rigblockIndex],
					&pendingRarityBag, pickup.IsWinnerRewardBoss,
				)
				if err != nil {
					candidateErr = err
					continue
				}
				*rarityBag = pendingRarityBag
				return part, partSubjects[subjectIndex], nil
			}
		}
	}
	if candidateErr != nil {
		return sporenet.Part{}, game.GameplayCreature{}, fmt.Errorf(
			"limitedCandidates: %w", candidateErr,
		)
	}
	return sporenet.Part{}, game.GameplayCreature{}, errors.New(
		"limited candidates unavailable",
	)
}

func (e campaignEquipmentDropBag) chanceThreshold(baseThreshold float32) float32 {
	if e.missCount >= campaignEquipmentPityMaximumMisses {
		return 1
	}
	boost := 1 + float32(e.missCount)*campaignEquipmentPityScalePerMiss
	return min(float32(1), baseThreshold*boost)
}

func (e *campaignEquipmentDropBag) recordDrop(isDropped bool) {
	if isDropped {
		e.missCount = 0
		return
	}
	e.missCount = min(e.missCount+1, campaignEquipmentPityMaximumMisses)
}

func (s *gameplayPeerSession) spawnCampaignEquipment(
	invocation game.CampaignScriptInvocation,
	gameplayJoin *game.GameplayJoin,
	sourceTime uint64,
	dropMask uint32,
	isBoss bool,
) ([][]byte, uint32, error) {
	if s == nil || s.zone == nil {
		return nil, 0, errors.New("equipment emitter unavailable")
	}
	if invocation.Challenge <= 0 || !zoneloot.IsEquipmentEmissionAllowed(dropMask, s.zone.DirectorDefinition().IsEquipmentDropEnabled) {
		return nil, 0, nil
	}
	// This wrapper is the interactable source path (native type4).
	attemptCount := s.campaignLootParticipantCount()
	packets := make([][]byte, 0)
	objectIDs := make([]uint32, 0, attemptCount)
	for attempt := uint32(0); attempt < attemptCount; attempt++ {
		dropPackets, objectID, roll, err := s.spawnCampaignEquipmentWithPolicy(
			invocation, gameplayJoin, sourceTime, dropMask, isBoss, false, "", nil, nil, nil, false,
		)
		if objectID != 0 {
			objectIDs = append(objectIDs, objectID)
		}
		if err != nil {
			s.removeUnpublishedLoot(objectIDs)
			return nil, 0, fmt.Errorf("equipmentAttempt[%d]: %w", attempt, err)
		}
		if objectID != 0 && roll.Part.RigblockAssetID == 0 {
			s.removeUnpublishedLoot(objectIDs)
			return nil, 0, errors.New("campaign equipment roll incomplete")
		}
		packets = append(packets, dropPackets...)
	}
	if len(objectIDs) == 0 {
		return packets, 0, nil
	}
	return packets, objectIDs[0], nil
}

func (s *gameplayPeerSession) spawnCampaignEquipmentWithPolicy(
	invocation game.CampaignScriptInvocation,
	gameplayJoin *game.GameplayJoin,
	sourceTime uint64,
	dropMask uint32,
	isBoss bool,
	isDebugGuaranteed bool,
	slotType string,
	partSlotBag *game.CampaignPartSlotBag,
	equipmentDropBag *campaignEquipmentDropBag,
	partRarityBag *game.CampaignPartRarityBag,
	isWinnerReward bool,
) ([][]byte, uint32, campaignEquipmentRoll, error) {
	roll := campaignEquipmentRoll{RequestedSlotType: slotType}
	if s == nil || s.zone == nil || gameplayJoin == nil {
		return nil, 0, roll, errors.New("campaign equipment unavailable")
	}
	// Developer guarantees are server policy, separate from native force.
	if !isDebugGuaranteed && !zoneloot.IsEquipmentEmissionAllowed(
		dropMask, s.zone.DirectorDefinition().IsEquipmentDropEnabled,
	) {
		return nil, 0, roll, nil
	}
	if invocation.Challenge <= 0 {
		return nil, 0, roll, errors.New("campaign equipment challenge unavailable")
	}
	if s.zone.DropRandom() == nil {
		return nil, 0, roll, errors.New("campaign drop random unavailable")
	}
	roll.ChanceScale = 1 + s.campaignPartAttribute(campaignLootFindAttribute)
	var err error
	roll.ChanceThreshold, err = sim.EquipmentDropThreshold(
		int(s.campaignLootParticipantCount()), invocation.Challenge, 0.45, roll.ChanceScale,
	)
	if err != nil {
		return nil, 0, roll, fmt.Errorf("equipmentThreshold: %w", err)
	}
	if equipmentDropBag != nil {
		roll.ChanceThreshold = equipmentDropBag.chanceThreshold(
			roll.ChanceThreshold,
		)
	}
	// Native compares the float32 threshold against the unrounded Float64 draw.
	roll.ChanceDraw = s.zone.DropRandom().Float64()
	roll.IsNaturalDrop = roll.ChanceDraw < float64(roll.ChanceThreshold)
	if !roll.IsNaturalDrop && !isDebugGuaranteed {
		return nil, 0, roll, nil
	}
	if s.deployedCreatureIndex >= uint32(len(s.binding.Creatures)) {
		return nil, 0, roll, errors.New("campaign equipment creature unavailable")
	}
	partSubjects := s.campaignPartSubjects()
	if len(partSubjects) == 0 {
		return nil, 0, roll, errors.New("campaign equipment roster unavailable")
	}
	roll.PartChoice = s.zone.DropRandom().Uint32()
	roll.PartSubjectCount = len(partSubjects)
	if slotType == "" {
		roll.PartSubjectIndex = int(roll.PartChoice % uint32(len(partSubjects)))
		roll.PartSubject = partSubjects[roll.PartSubjectIndex]
	}
	limitedEditionRigblockID := uint16(0)
	if isBoss && slotType == "" && !isWinnerReward {
		chanceDraw, drawErr := s.zone.DropRandom().Index(
			campaignBossLimitedEditionChanceBasis,
		)
		if drawErr != nil {
			return nil, 0, roll, fmt.Errorf("equipmentLimitedChance: %w", drawErr)
		}
		roll.LimitedEditionDraw = chanceDraw
		roll.IsLimitedEditionRolled = true
		if chanceDraw < campaignBossLimitedEditionChanceThreshold {
			rigblockDraw, rigblockErr := s.zone.DropRandom().Index(
				uint32(len(campaignBossLimitedEditionRigblockIDs)),
			)
			if rigblockErr != nil {
				return nil, 0, roll, fmt.Errorf("equipmentLimitedRigblock: %w", rigblockErr)
			}
			limitedEditionRigblockID =
				campaignBossLimitedEditionRigblockIDs[rigblockDraw]
			roll.LimitedEditionRigblockID = limitedEditionRigblockID
		}
	}
	if limitedEditionRigblockID != 0 {
		if partRarityBag != nil {
			roll.Part, err = gameplayJoin.GenerateCampaignSpecialPartFromBag(
				roll.PartSubject, s.binding.Difficulty, s.binding.ChainLevelIndex,
				roll.PartChoice, limitedEditionRigblockID, partRarityBag, isBoss,
			)
		} else {
			roll.Part, err = gameplayJoin.GenerateCampaignSpecialPart(
				roll.PartSubject, s.binding.Difficulty, s.binding.ChainLevelIndex,
				roll.PartChoice, limitedEditionRigblockID, isBoss,
			)
		}
	} else if slotType != "" {
		candidate, candidateErr := campaignEquipmentCandidateForSlot(
			gameplayJoin, partSubjects, s.binding.Difficulty, s.binding.ChainLevelIndex,
			roll.PartChoice, slotType, isBoss,
		)
		err = candidateErr
		roll.Part = candidate.Part
		roll.PartSubject = candidate.PartSubject
		roll.PartSubjectIndex = candidate.SubjectIndex
	} else if partSlotBag != nil {
		roll.Part, err = gameplayJoin.GenerateCampaignPartFromBag(
			roll.PartSubject, s.binding.Difficulty, s.binding.ChainLevelIndex,
			roll.PartChoice, partSlotBag, partRarityBag, isBoss,
		)
	} else if partRarityBag != nil {
		roll.Part, err = gameplayJoin.GenerateCampaignPartFromBag(
			roll.PartSubject, s.binding.Difficulty, s.binding.ChainLevelIndex,
			roll.PartChoice, nil, partRarityBag, isBoss,
		)
	} else {
		roll.Part, err = gameplayJoin.GenerateCampaignPart(
			roll.PartSubject, s.binding.Difficulty, s.binding.ChainLevelIndex, roll.PartChoice, isBoss,
		)
	}
	if err != nil {
		return nil, 0, roll, fmt.Errorf("equipmentGenerate: %w", err)
	}
	presentationPolicy := zoneloot.EquipmentOrdinaryWorldDrop
	if limitedEditionRigblockID != 0 || (isWinnerReward && isBoss) {
		presentationPolicy = zoneloot.EquipmentUniqueRewardGroundDrop
	}
	nounName, isSupported := presentationPolicy.ContainerNoun(zoneloot.Rarity(roll.Part.Rarity))
	if !isSupported || nounName == "" {
		return nil, 0, roll, nil
	}
	err = gameplayJoin.ValidateGeneratedCampaignPart(roll.Part)
	if err != nil {
		return nil, 0, roll, fmt.Errorf("equipmentComplete: %w", err)
	}
	objectID, err := s.reserveCampaignObjectID()
	if err != nil {
		return nil, 0, roll, fmt.Errorf("equipmentObjectID: %w", err)
	}
	source := sim.Position{
		X: invocation.Position.X,
		Y: invocation.Position.Y,
		Z: invocation.Position.Z,
	}
	destination := s.reachableCampaignDropDestination(source)
	plan, err := zoneloot.PlanEquipment(zoneloot.EquipmentPlanInput{
		ObjectID: objectID, Rarity: zoneloot.Rarity(roll.Part.Rarity),
		PresentationPolicy: presentationPolicy,
		Source:             source, Destination: destination,
		SimulationTime: time.Duration(sourceTime) * time.Millisecond,
	})
	if err != nil {
		return nil, 0, roll, fmt.Errorf("equipmentPlan: %w", err)
	}
	packets, err := s.marshalEquipmentDrop(plan, roll.Part)
	if err != nil {
		return nil, 0, roll, fmt.Errorf("equipmentMarshal: %w", err)
	}
	err = s.registerCampaignPickup(
		zoneinteract.PickupEquipment, objectID, source, destination,
		s.pickupFlight(plan.NounName, source, destination, plan.Lob),
	)
	if err != nil {
		return nil, 0, roll, fmt.Errorf("equipmentRegister: %w", err)
	}
	err = s.zone.PickupPayload().AddEquipment(zoneinteract.EquipmentPickup{
		ObjectID: objectID, Part: roll.Part,
		PresentationPolicy:     presentationPolicy,
		WinnerRewardChoice:     roll.PartChoice,
		WinnerRewardDifficulty: s.binding.Difficulty,
		WinnerRewardRigblockID: roll.LimitedEditionRigblockID,
		IsWinnerReward:         isWinnerReward,
		IsWinnerRewardBoss:     isWinnerReward && isBoss,
	})
	if err != nil {
		s.zone.Pickups().Remove(objectID)
		return nil, 0, roll, fmt.Errorf("equipmentTrack: %w", err)
	}
	return packets, objectID, roll, nil
}

func campaignEquipmentCandidateForSlot(
	gameplayJoin *game.GameplayJoin,
	partSubjects []game.GameplayCreature,
	difficulty uint32,
	campaignStage uint32,
	choice uint32,
	slotType string, isBoss bool,
) (campaignEquipmentSlotCandidate, error) {
	candidates := make([]campaignEquipmentSlotCandidate, 0, len(partSubjects))
	var candidateErr error
	for subjectIndex, partSubject := range partSubjects {
		part, err := gameplayJoin.GenerateCampaignPartForSlot(
			partSubject, difficulty, campaignStage, choice, slotType, isBoss,
		)
		if err != nil {
			candidateErr = err
			continue
		}
		candidates = append(candidates, campaignEquipmentSlotCandidate{
			Part: part, PartSubject: partSubject, SubjectIndex: subjectIndex,
		})
	}
	if len(candidates) == 0 {
		if candidateErr != nil {
			return campaignEquipmentSlotCandidate{}, fmt.Errorf(
				"slotCandidates: %w", candidateErr,
			)
		}
		return campaignEquipmentSlotCandidate{}, errors.New(
			"campaign equipment slot candidates empty",
		)
	}
	candidateIndex := int(choice % uint32(len(candidates)))
	return candidates[candidateIndex], nil
}

func (s *gameplayPeerSession) reserveCampaignNPCDrop(
	objectID uint32, kind zoneloot.NPCDropKind,
) (*zoneloot.Reservation, bool) {
	if s == nil || s.zone == nil || s.zone.NPCs() == nil {
		return nil, false
	}
	if s.zone.Loot() == nil {
		return nil, false
	}
	enemy, isEnemyFound := s.zone.NPCs().NPC(objectID)
	if !isEnemyFound || enemy.Plan.IsLootSuppressed {
		return nil, false
	}
	return s.zone.Loot().ReserveNPCDrop(objectID, kind)
}

type campaignPickupScheduleCleaner struct {
	pickup   *zoneinteract.PickupRegistry
	objectID uint32
}

func (e *campaignPickupScheduleCleaner) Cleanup() {
	if e == nil {
		return
	}
	if e.pickup != nil {
		e.pickup.Release(e.objectID)
	}
}

type campaignEquipmentPickupFailure struct {
	runtime    campaignInteractionRuntime
	sessionKey string
	generation uint64
	objectID   uint32
}

func (f campaignEquipmentPickupFailure) handle(_ error) {
	f.runtime.registry.mutex.Lock()
	peerSession, isFound := f.runtime.registry.sessions[f.sessionKey]
	if isFound && peerSession.generation == f.generation {
		peerSession.campaignScheduleSession().Remove(
			zoneaction.ScheduleEquipment, f.objectID, nil,
		)
		peerSession.zone.Pickups().Release(f.objectID)
		f.runtime.registry.sessions[f.sessionKey] = peerSession
	}
	f.runtime.registry.mutex.Unlock()
}

type campaignCrystalPickupStep struct {
	runtime           campaignInteractionRuntime
	ctx               context.Context
	sessionKey        string
	generation        uint64
	objectID          uint32
	run               *interactraknet.CrystalPickupRun
	previousInventory sim.CrystalInventory
	pendingInventory  *sim.CrystalInventory
	pendingPickup     *sim.CrystalPickupObject
	pickupPosition    sim.Position
	isLootBagReward   bool
	releasePacket     []byte
}

func (s campaignCrystalPickupStep) produce() ([][]byte, error) {
	s.runtime.registry.mutex.Lock()
	peerSession, isFound := s.runtime.registry.sessions[s.sessionKey]
	if !isFound || peerSession.generation != s.generation {
		s.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	if s.pendingInventory == nil || s.pendingPickup == nil {
		s.runtime.registry.mutex.Unlock()
		return nil, errors.New("campaign crystal transaction unavailable")
	}
	pickup, isPickupFound := peerSession.zone.Pickups().Pickup(s.objectID)
	if !isPickupFound || !peerSession.zone.Pickups().IsReserved(s.objectID) ||
		(pickup.ExpiresAt > 0 && peerSession.zone.Elapsed(time.Now()) >= pickup.ExpiresAt) {
		s.run.Stop()
		peerSession.zone.Pickups().Release(s.objectID)
		peerSession.releaseCampaignCrystalSchedule(s.objectID, s.run)
		s.runtime.registry.sessions[s.sessionKey] = peerSession
		s.runtime.registry.mutex.Unlock()
		return [][]byte{s.releasePacket}, nil
	}
	result, err := s.run.Advance(s.ctx, time.Second)
	if err != nil {
		peerSession.zone.Pickups().Release(s.objectID)
		peerSession.releaseCampaignCrystalSchedule(s.objectID, s.run)
		s.runtime.registry.sessions[s.sessionKey] = peerSession
		s.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignCrystalAdvance: %w", err)
	}
	s.run.Stop()
	if len(result) != 1 {
		peerSession.zone.Pickups().Release(s.objectID)
		peerSession.releaseCampaignCrystalSchedule(s.objectID, s.run)
		s.runtime.registry.sessions[s.sessionKey] = peerSession
		s.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignCrystalResult: %d", len(result))
	}
	packet := make([][]byte, 0, 2)
	worldPackets := make([][]byte, 0, 1)
	isCheckpointReady := false
	if len(result[0].Actions) != 0 &&
		result[0].Actions[0].Kind == sim.CrystalSlotAssigned {
		packet, err = interactraknet.MarshalCrystalCollection(
			result[0], s.objectID, uint8(peerSession.binding.Slot),
			*s.pendingInventory,
		)
		if err == nil {
			// Deletion and player-indexed inventory/bonus state are shared.
			// The final CrystalAcquired packet has no player selector and must
			// remain local to the collecting player's HUD.
			worldPackets = append(worldPackets, packet[:3]...)
			err = peerSession.zone.SetCrystalInventory(
				peerSession.binding.UserID, peerSession.generation,
				*s.pendingInventory,
			)
			if err == nil {
				if !peerSession.zone.Pickups().Commit(s.objectID) {
					rollbackErr := peerSession.zone.SetCrystalInventory(
						peerSession.binding.UserID, peerSession.generation,
						s.previousInventory,
					)
					err = fmt.Errorf(
						"crystalPickupCommit: %w",
						errors.Join(errors.New("reservation missing"), rollbackErr),
					)
				} else {
					peerSession.setCrystalInventory(*s.pendingInventory)
					peerSession.zone.PickupPayload().RemoveCrystal(s.objectID)
					if s.isLootBagReward {
						peerSession.campaignCrystalSelectionBag.record(
							s.pendingPickup.CrystalType, s.pendingPickup.Rarity,
						)
					}
					isCheckpointReady = true
				}
			}
		}
	} else if len(result[0].Actions) != 0 {
		var publication interactraknet.CrystalFullPublication
		publication, err = interactraknet.MarshalCrystalFull(
			result[0], s.objectID, s.pickupPosition,
		)
		if err == nil {
			peerSession.zone.Pickups().Release(s.objectID)
			peerSession.zone.PickupPayload().UpdateCrystalObject(
				s.objectID, *s.pendingPickup,
			)
			if publication.RelaunchPacket != nil {
				packet = append(packet, publication.RelaunchPacket)
				worldPackets = append(worldPackets, publication.RelaunchPacket)
			}
			packet = append(packet, publication.EventPacket)
			inventory, isInventoryFound := peerSession.zone.CrystalInventory(
				peerSession.binding.UserID, peerSession.generation,
			)
			if !isInventoryFound {
				err = errors.New("crystal repair inventory unavailable")
			} else {
				peerSession.setCrystalInventory(inventory)
				var repairPackets [][]byte
				repairPackets, err = marshalCrystalRepair(peerSession)
				if err == nil {
					packet = append(packet, repairPackets...)
				}
			}
		}
	} else {
		peerSession.zone.Pickups().Release(s.objectID)
	}
	if err != nil {
		peerSession.zone.Pickups().Release(s.objectID)
	}
	peerSession.releaseCampaignCrystalSchedule(s.objectID, s.run)
	if err == nil {
		s.runtime.registry.sessions[s.sessionKey] = peerSession
		for candidateSessionKey, candidate := range s.runtime.registry.sessions {
			if candidateSessionKey == s.sessionKey || candidate.zone != peerSession.zone {
				continue
			}
			if isCheckpointReady && s.isLootBagReward {
				candidate.campaignCrystalSelectionBag =
					peerSession.campaignCrystalSelectionBag.Clone()
			}
			publishErr := candidate.publishPackets(worldPackets)
			if publishErr != nil && s.runtime.logger != nil {
				s.runtime.logger.Printf(
					"RakNet campaign crystal pickup peer delivery queued game=%d user=%d object=%d: %v",
					candidate.binding.GameID, candidate.binding.UserID,
					s.objectID, publishErr,
				)
			}
			s.runtime.registry.sessions[candidateSessionKey] = candidate
		}
	}
	s.runtime.registry.mutex.Unlock()
	if isCheckpointReady {
		peerSession.zone.SaveCheckpointIfSafe(zonecheckpoint.ReasonSafePickup)
	}
	if err != nil {
		return nil, fmt.Errorf("campaignCrystalMarshal: %w", err)
	}
	return append(packet, s.releasePacket), nil
}

type campaignCrystalPickupFailure struct {
	runtime    campaignInteractionRuntime
	sessionKey string
	generation uint64
	objectID   uint32
	run        *interactraknet.CrystalPickupRun
}

func (f campaignCrystalPickupFailure) handle(scheduleErr error) {
	f.runtime.registry.mutex.Lock()
	peerSession, isFound := f.runtime.registry.sessions[f.sessionKey]
	if isFound && peerSession.generation == f.generation {
		peerSession.zone.Pickups().Release(f.objectID)
		peerSession.releaseCampaignCrystalSchedule(f.objectID, f.run)
		f.runtime.registry.sessions[f.sessionKey] = peerSession
	}
	f.runtime.registry.mutex.Unlock()
	f.run.Stop()
	f.runtime.logger.Printf(
		"RakNet campaign crystal schedule failed for %s: %v",
		f.sessionKey, scheduleErr,
	)
}

const campaignDNAPickupDelay = 500 * time.Millisecond
const campaignDNAPickupRadius = float32(2.5)

func (s *gameplayPeerSession) spawnCampaignNPCDNA(
	enemy zonenpc.Snapshot, sourceTime uint64, now time.Time,
) ([][]byte, uint32, error) {
	if s == nil || s.zone == nil || !enemy.IsDefeated || enemy.Plan.ObjectID == 0 {
		return nil, 0, errors.New("campaign enemy DNA unavailable")
	}
	if !zoneloot.IsNPCDropAllowed(enemy.Plan.NPCProfile.DropTypes, zoneloot.NPCDropDNA) {
		return nil, 0, nil
	}
	tuning := s.zone.DirectorDefinition().DNADropTuning
	err := tuning.Validate()
	if err != nil {
		return nil, 0, fmt.Errorf("dnaTuning: %w", err)
	}
	stage, err := zoneLevelIndex(s.binding)
	if err != nil {
		return nil, 0, fmt.Errorf("dnaStage: %w", err)
	}
	reservation, isReserved := s.reserveCampaignNPCDrop(
		enemy.Plan.ObjectID, zoneloot.NPCDropDNA,
	)
	if !isReserved {
		return nil, 0, nil
	}
	if s.zone.DropRandom() == nil {
		reservation.Release()
		return nil, 0, errors.New("campaign drop random unavailable")
	}
	if stage < tuning.MinimumStage {
		err := reservation.Commit()
		if err != nil {
			return nil, 0, fmt.Errorf("dnaDifficultyCommit: %w", err)
		}
		return nil, 0, nil
	}
	draw := s.zone.DropRandom().Float64()
	isDrop, err := tuning.IsDrop(draw)
	if err != nil {
		reservation.Release()
		return nil, 0, fmt.Errorf("dnaDecision: %w", err)
	}
	if !isDrop {
		err = reservation.Commit()
		if err != nil {
			return nil, 0, fmt.Errorf("dnaMissCommit: %w", err)
		}
		return nil, 0, nil
	}
	amountDraw, err := s.zone.DropRandom().Index(zoneloot.DNAChanceBasis + 1)
	if err != nil {
		reservation.Release()
		return nil, 0, fmt.Errorf("dnaAmount: %w", err)
	}
	// Unresolved source classes retain the prior 11-challenge compatibility
	// amount; resolved classes and active Elite attachments own their challenge.
	challenge, npcType := s.campaignNPCRewardSource(enemy.Plan, 11)
	// DNA has one emission per source, independent of the NPC attempt class.
	_ = npcType
	amount, err := zoneloot.NPCDNAAmount(zoneloot.NPCDNAAmountInput{
		Stage: stage, Challenge: challenge, Tuning: tuning,
		DroppedMultiplier: s.campaignPartAttribute(campaignDNADroppedAttribute), RandomDraw: amountDraw,
	})
	if err != nil {
		reservation.Release()
		return nil, 0, fmt.Errorf("dnaAmountResolve: %w", err)
	}
	source := sim.Position{
		X: enemy.Plan.Position.X,
		Y: enemy.Plan.Position.Y,
		Z: enemy.Plan.Position.Z,
	}
	destination := s.reachableCampaignDropDestination(source)
	objectID, err := s.reserveCampaignObjectID()
	if err != nil {
		reservation.Release()
		return nil, 0, fmt.Errorf("dnaObjectID: %w", err)
	}
	plan, err := zoneloot.PlanDNA(zoneloot.DNAPlanInput{
		ObjectID: objectID, Amount: amount, Source: source,
		Destination:    destination,
		SimulationTime: time.Duration(sourceTime) * time.Millisecond,
	})
	if err != nil {
		reservation.Release()
		return nil, 0, fmt.Errorf("dnaPlan: %w", err)
	}
	packets, err := s.marshalDNADrop(plan)
	if err != nil {
		reservation.Release()
		return nil, 0, fmt.Errorf("dnaMarshal: %w", err)
	}
	if s.zone == nil || s.zone.DNA() == nil {
		reservation.Release()
		return nil, 0, errors.New("dna zone unavailable")
	}
	err = s.zone.DNA().Add(zoneloot.DNAPickup{
		ObjectID: objectID, Amount: amount,
		Flight: s.pickupFlight("DNA.Noun", source, destination, plan.Lob),
		Position: game.Vec3{
			X: destination.X, Y: destination.Y, Z: destination.Z,
		},
		AvailableAt: now.Add(campaignDNAPickupDelay),
	})
	if err != nil {
		reservation.Release()
		return nil, 0, fmt.Errorf("dnaTrack: %w", err)
	}
	err = reservation.Commit()
	if err != nil {
		s.zone.DNA().Remove(objectID)
		return nil, 0, fmt.Errorf("dnaCommit: %w", err)
	}
	return packets, objectID, nil
}

const campaignOrbLifetime = 30 * time.Second
const campaignOrbPickupRadius = float32(2.5)
const campaignOrbLobDuration = 500 * time.Millisecond

func (s *gameplayPeerSession) spawnCampaignHealthOrb(
	invocation game.CampaignScriptInvocation, sourceTime uint64, now time.Time,
	registry *gameplaySessionRegistry,
) ([][]byte, []uint32, error) {
	packets, objectIDs, err := s.spawnCampaignResourceOrb(invocation, sourceTime, now, true, registry)
	if err != nil {
		return nil, nil, fmt.Errorf("healthOrb: %w", err)
	}
	return packets, objectIDs, nil
}

func (s *gameplayPeerSession) spawnCampaignResourceOrb(
	invocation game.CampaignScriptInvocation, sourceTime uint64, now time.Time,
	isResurrectionAllowed bool,
	registry *gameplaySessionRegistry,
) ([][]byte, []uint32, error) {
	if s == nil || s.zone == nil || s.squad == nil || invocation.Challenge <= 0 {
		return nil, nil, errors.New("campaign orb unavailable")
	}
	if s.zone.DropRandom() == nil {
		return nil, nil, errors.New("campaign drop random unavailable")
	}
	if s.zone.Orbs() == nil {
		return nil, nil, errors.New("campaign orb registry unavailable")
	}
	roster, err := s.campaignOrbRosterLocked(registry)
	if err != nil {
		return nil, nil, fmt.Errorf("orbRoster: %w", err)
	}
	isResurrectionEnabled := isResurrectionAllowed && !strings.EqualFold(
		s.binding.Level, game.InitialChainLevel,
	) && invocation.CallbackName != "InteractHealthObelisk"
	isDeadSquadMemberFound := false
	for index, creature := range s.binding.Creatures {
		character, isFound := s.squad.Character(uint32(index))
		if !isFound || !character.IsAvailable || creature.Noun == 0 {
			continue
		}
		if character.HitPoints <= 0 {
			isDeadSquadMemberFound = true
		}
	}
	isResurrectionEnabled = isResurrectionEnabled && isDeadSquadMemberFound
	if isResurrectionEnabled {
		for _, orb := range s.zone.Orbs().Orbs() {
			if orb.Request.Kind == sim.ResurrectionOrbDrop {
				isResurrectionEnabled = false
				break
			}
		}
	}
	source := sim.Position{
		X: invocation.Position.X,
		Y: invocation.Position.Y,
		Z: invocation.Position.Z,
	}
	director := s.zone.DirectorDefinition()
	difficultyScale := float32(1)
	stage := s.binding.ChainLevelIndex
	if stage > 0 && uint64(stage) <= uint64(len(director.OrbDifficultyScales)) {
		difficultyScale = director.OrbDifficultyScales[stage-1]
	}
	budget, err := sim.PlanOrbDropBudget(invocation.Challenge, difficultyScale)
	if err != nil {
		return nil, nil, fmt.Errorf("orbBudget: %w", err)
	}
	if budget.ScaledBudget == 0 {
		return nil, nil, nil
	}
	footprint := float32(0)
	if s.zone.NPCs() != nil && invocation.TargetObjectID != 0 {
		sourceNPC, isSourceFound := s.zone.NPCs().NPC(invocation.TargetObjectID)
		if isSourceFound {
			footprint = sourceNPC.Plan.NPCProfile.FootprintRadius
		}
	}
	// Native DropStuffForObject retains one ring sampler across the orb
	// budget. Reusing the player-directed compatibility point piles up drops.
	placement, err := zoneloot.NewPlacement(source, footprint, s.zone.DropRandom())
	if err != nil {
		return nil, nil, fmt.Errorf("orbPlacement: %w", err)
	}
	maximumDrop := budget.GuaranteedSelection
	if budget.RemainderThreshold != 0 {
		maximumDrop++
	}
	objectIDs := make([]uint32, 0, maximumDrop)
	roles := make([]sim.Role, 0, maximumDrop)
	destinations := make([]sim.Position, 0, maximumDrop)
	for index := uint32(0); index < maximumDrop; index++ {
		objectID, reserveErr := s.reserveCampaignObjectID()
		if reserveErr != nil {
			return nil, nil, fmt.Errorf("orbObjectID[%d]: %w", index, reserveErr)
		}
		objectIDs = append(objectIDs, objectID)
		roles = append(roles, sim.Role(fmt.Sprintf("campaignOrb.%d", objectID)))
		destination, placementErr := placement.Next(campaignDropProjector{mesh: s.zone.Navigation()})
		if placementErr != nil {
			return nil, nil, fmt.Errorf("orbDestination[%d]: %w", index, placementErr)
		}
		destinations = append(destinations, destination)
	}
	pickups, err := zoneloot.PlanOrbs(sim.OrbDropInput{
		ScaledBudget: budget.ScaledBudget,
		SourceRole:   zoneinteract.Role, SourcePosition: source,
		SimulationTime: time.Duration(sourceTime) * time.Millisecond,
		PickupLifetime: campaignOrbLifetime, Roster: roster,
		Destinations: destinations, PickupRoles: roles,
		Random: s.zone.DropRandom(), SelectionRandom: s.zone.CapsuleRandom(),
		IsResurrectionEnabled: isResurrectionEnabled,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("orbPlan: %w", err)
	}
	if len(pickups) == 0 {
		return nil, nil, nil
	}
	// Only the final fractional attempt can fail; accepted pickups retain
	// the prefix of reserved object IDs, destinations, and roles.
	objectIDs = objectIDs[:len(pickups)]
	packets := make([][]byte, 0, len(pickups)*4)
	for index, pickup := range pickups {
		components, marshalErr := s.marshalOrbDrop(pickup, objectIDs[index])
		if marshalErr != nil {
			return nil, nil, fmt.Errorf("orbMarshal[%d]: %w", index, marshalErr)
		}
		packets = append(packets, components.CreatePacket)
		if components.InteractablePacket != nil {
			packets = append(packets, components.InteractablePacket)
		}
		packets = append(packets, components.PresentationPacket, components.LocomotionPacket)
	}
	for index, pickup := range pickups {
		objectID := objectIDs[index]
		err = s.zone.Orbs().Add(zoneinteract.Orb{
			ObjectID: objectID, Request: pickup,
			AvailableAt: now.Add(campaignOrbLobDuration),
		})
		if err != nil {
			rollbackErr := s.rollbackCampaignOrbs(objectIDs[:index])
			return nil, nil, fmt.Errorf("orbTrack[%d]: %w", index, errors.Join(err, rollbackErr))
		}
		err = s.registerCampaignPickup(zoneinteract.PickupOrb, objectID,
			pickup.Position, pickup.Destination, s.pickupFlight(pickup.NounName, pickup.Position, pickup.Destination, pickup.Lob))
		if err != nil {
			isOrbRemoved := s.zone.Orbs().Remove(objectID)
			if !isOrbRemoved {
				err = errors.Join(err, errors.New("orb rollback registration missing"))
			}
			rollbackErr := s.rollbackCampaignOrbs(objectIDs[:index])
			return nil, nil, fmt.Errorf("orbRegister[%d]: %w", index, errors.Join(err, rollbackErr))
		}
	}
	return packets, objectIDs, nil
}

// A failed batch publishes no packets and removes every earlier registration.
func (s *gameplayPeerSession) rollbackCampaignOrbs(objectIDs []uint32) error {
	var rollbackErr error
	for _, objectID := range objectIDs {
		isPickupRemoved := s.zone.Pickups().Remove(objectID)
		isOrbRemoved := s.zone.Orbs().Remove(objectID)
		if !isPickupRemoved || !isOrbRemoved {
			rollbackErr = errors.Join(rollbackErr,
				fmt.Errorf("orbRollback[%d]: registration missing", objectID))
		}
	}
	return rollbackErr
}

func (s *gameplayPeerSession) spawnCampaignNPCOrb(
	enemy zonenpc.Snapshot, sourceTime uint64, now time.Time,
	registry *gameplaySessionRegistry,
) ([][]byte, []uint32, error) {
	if s == nil || !enemy.IsDefeated || enemy.Plan.ObjectID == 0 {
		return nil, nil, errors.New("campaign enemy orb unavailable")
	}
	if !zoneloot.IsNPCDropAllowed(enemy.Plan.NPCProfile.DropTypes, zoneloot.NPCDropOrb) {
		return nil, nil, nil
	}
	sourceAmount := campaignNPCOrbFallbackChallenge
	if enemy.Plan.NPCProfile.IsClassKnown {
		sourceAmount = enemy.Plan.NPCProfile.ChallengeValue
	}
	if sourceAmount <= 0 {
		return nil, nil, nil
	}
	reservation, isReserved := s.reserveCampaignNPCDrop(
		enemy.Plan.ObjectID, zoneloot.NPCDropOrb,
	)
	if !isReserved {
		return nil, nil, nil
	}
	packets, objectIDs, err := s.spawnCampaignResourceOrb(
		game.CampaignScriptInvocation{
			TargetObjectID: enemy.Plan.ObjectID,
			Position:       enemy.Plan.Position,
			Challenge:      sourceAmount,
		},
		sourceTime, now, true, registry,
	)
	if err != nil {
		reservation.Release()
		return nil, nil, fmt.Errorf("enemyOrbSpawn: %w", err)
	}
	err = reservation.Commit()
	if err != nil {
		rollbackErr := s.rollbackCampaignOrbs(objectIDs)
		return nil, nil, fmt.Errorf("enemyOrbCommit: %w", errors.Join(err, rollbackErr))
	}
	return packets, objectIDs, nil
}

func (s *gameplayPeerSession) collectCampaignOrbs(
	registry *gameplaySessionRegistry,
	start raknet.Vector3, end raknet.Vector3, now time.Time, objectIDs ...uint32,
) ([][]byte, error) {
	if s == nil || s.zone.Orbs() == nil || s.squad == nil {
		return nil, nil
	}
	endPosition := game.Vec3{X: end.X, Y: end.Y, Z: end.Z}
	pickups := s.zone.Orbs().Contacts(
		game.Vec3{X: start.X, Y: start.Y, Z: start.Z},
		endPosition,
		campaignOrbPickupRadius,
		now,
	)
	for _, pickup := range pickups {
		if len(objectIDs) == 1 && pickup.ObjectID != objectIDs[0] {
			continue
		}
		if pickup.Request.Kind == sim.ResurrectionOrbDrop {
			continue
		}
		registeredPickup, admission := s.reserveCampaignPickupContact(
			pickup.ObjectID, start, end, campaignOrbPickupRadius, now,
		)
		if admission != zoneinteract.PickupAccepted {
			if admission == zoneinteract.PickupRejectedReserved {
				return nil, errors.New("orb contact temporarily reserved")
			}
			continue
		}
		if registeredPickup.Kind != zoneinteract.PickupOrb {
			s.zone.Pickups().Release(pickup.ObjectID)
			continue
		}
		kind := zoneHealthOrb
		isFull := false
		restoredAmount := float32(0)
		healing := make([]zoneSquadHealing, 0)
		manaRestorations := make([]zoneSquadManaRestoration, 0)
		alliedRestorations := make([]alliedZoneSquadRestoration, 0)
		partyResourcePackets := make([][]byte, 0)
		alliedWorldPackets := make([][]byte, 0)
		restoreFraction := campaignOrbRestoreFraction *
			(1 + s.campaignPartAttribute(campaignOrbEffectAttribute))
		if pickup.Request.Kind == sim.ManaOrbDrop {
			kind = zoneManaOrb
		}
		if kind == zoneHealthOrb {
			var err error
			healing, err = s.healLivingZoneSquadByMaximum(restoreFraction)
			if err != nil {
				s.zone.Pickups().Release(pickup.ObjectID)
				return nil, fmt.Errorf("orbHealth: %w", err)
			}
			isFull = len(healing) == 0
			for _, healedCharacter := range healing {
				restoredAmount += healedCharacter.amount
			}
		}
		if kind == zoneManaOrb {
			var err error
			manaRestorations, err = s.restoreLivingZoneSquadManaByMaximum(
				restoreFraction,
			)
			if err != nil {
				s.zone.Pickups().Release(pickup.ObjectID)
				return nil, fmt.Errorf("orbMana: %w", err)
			}
			isFull = len(manaRestorations) == 0
			for _, restoration := range manaRestorations {
				restoredAmount += restoration.amount
			}
		}
		if registry != nil && (kind == zoneHealthOrb || kind == zoneManaOrb) {
			var allyPackets [][]byte
			var allyErr error
			alliedRestorations, allyPackets, alliedWorldPackets, allyErr =
				registry.restoreAlliedZoneSquadsLocked(*s, kind, campaignOrbRestoreFraction)
			if allyErr != nil {
				s.rollbackZoneSquadHealing(healing)
				s.rollbackZoneSquadManaRestoration(manaRestorations)
				s.zone.Pickups().Release(pickup.ObjectID)
				return nil, fmt.Errorf("orbAllies: %w", allyErr)
			}
			partyResourcePackets = append(partyResourcePackets, allyPackets...)
			for _, alliedRestoration := range alliedRestorations {
				for _, healedCharacter := range alliedRestoration.healing {
					restoredAmount += healedCharacter.amount
				}
				for _, manaRestoration := range alliedRestoration.manaRestorations {
					restoredAmount += manaRestoration.amount
				}
			}
			isFull = isFull && len(alliedRestorations) == 0
		}
		restored := campaignOrbRestoredText(restoredAmount)
		encoded, err := marshalCampaignOrbPickup(
			pickup, s.deployedObjectID, kind, isFull,
			s.deployedHitPoint(), s.deployedManaPoint(), restored,
		)
		if err != nil {
			s.rollbackZoneSquadHealing(healing)
			s.rollbackZoneSquadManaRestoration(manaRestorations)
			registry.rollbackAlliedZoneSquadRestorationsLocked(alliedRestorations)
			s.zone.Pickups().Release(pickup.ObjectID)
			return nil, fmt.Errorf("orbPickupMarshal: %w", err)
		}
		if isFull {
			isFirstNotice := s.markCampaignOrbFullContact(pickup.ObjectID)
			s.zone.Pickups().Release(pickup.ObjectID)
			if isFirstNotice {
				s.queuePickupNoticeLocked(registry, encoded)
			}
			continue
		}
		s.clearCampaignOrbFullContact(pickup.ObjectID)
		encoded = append(encoded, partyResourcePackets...)
		encoded = append(encoded, alliedWorldPackets...)
		for _, healedCharacter := range healing {
			resourcePacket, resourceErr := s.marshalCampaignCharacterResource(
				healedCharacter.creatureIndex,
			)
			if resourceErr != nil {
				s.rollbackZoneSquadHealing(healing)
				registry.rollbackAlliedZoneSquadRestorationsLocked(alliedRestorations)
				s.zone.Pickups().Release(pickup.ObjectID)
				return nil, fmt.Errorf("orbSquadResource: %w", resourceErr)
			}
			encoded = append(encoded, resourcePacket)
			partyResourcePackets = append(partyResourcePackets, resourcePacket)
		}
		for _, restoration := range manaRestorations {
			resourcePacket, resourceErr := s.marshalCampaignCharacterResource(
				restoration.creatureIndex,
			)
			if resourceErr != nil {
				s.rollbackZoneSquadManaRestoration(manaRestorations)
				registry.rollbackAlliedZoneSquadRestorationsLocked(alliedRestorations)
				s.zone.Pickups().Release(pickup.ObjectID)
				return nil, fmt.Errorf("orbSquadMana: %w", resourceErr)
			}
			encoded = append(encoded, resourcePacket)
			partyResourcePackets = append(partyResourcePackets, resourcePacket)
		}
		if !s.zone.Pickups().Commit(pickup.ObjectID) {
			s.rollbackZoneSquadHealing(healing)
			s.rollbackZoneSquadManaRestoration(manaRestorations)
			registry.rollbackAlliedZoneSquadRestorationsLocked(alliedRestorations)
			return nil, errors.New("campaign orb commit missing")
		}
		s.zone.Orbs().Remove(pickup.ObjectID)
		s.publishCommittedPickupLocked(registry, encoded, partyResourcePackets)
	}
	return nil, nil
}

func (s *gameplayPeerSession) markCampaignOrbFullContact(objectID uint32) bool {
	if s == nil {
		return false
	}
	if s.fullOrbContactObjectIDs == nil {
		s.fullOrbContactObjectIDs = make(map[uint32]struct{})
	}
	if _, isFound := s.fullOrbContactObjectIDs[objectID]; isFound {
		return false
	}
	s.fullOrbContactObjectIDs[objectID] = struct{}{}
	return true
}

func (s *gameplayPeerSession) clearCampaignOrbFullContact(objectID uint32) {
	if s == nil || s.fullOrbContactObjectIDs == nil {
		return
	}
	delete(s.fullOrbContactObjectIDs, objectID)
}

func (s *gameplayPeerSession) clearExitedCampaignOrbFullContacts(
	actorPosition game.Vec3,
) {
	if s == nil || s.zone == nil || s.zone.Orbs() == nil ||
		len(s.fullOrbContactObjectIDs) == 0 {
		return
	}
	for objectID := range s.fullOrbContactObjectIDs {
		orb, isFound := s.zone.Orbs().Orb(objectID)
		if !isFound {
			delete(s.fullOrbContactObjectIDs, objectID)
			continue
		}
		orbPosition := game.Vec3{
			X: orb.Request.Destination.X,
			Y: orb.Request.Destination.Y,
			Z: orb.Request.Destination.Z,
		}
		if zonegeometry.ContainsSphere(
			actorPosition, orbPosition, campaignOrbPickupRadius,
		) {
			continue
		}
		delete(s.fullOrbContactObjectIDs, objectID)
	}
}

func (s *gameplayPeerSession) rollbackZoneSquadHealing(healing []zoneSquadHealing) {
	if s == nil || s.squad == nil {
		return
	}
	for _, healedCharacter := range healing {
		previousHitPoint := healedCharacter.hitPoint - healedCharacter.amount
		_, _ = s.squad.SetHitPoints(healedCharacter.creatureIndex, previousHitPoint)
	}
	_ = s.syncZoneHero()
	_ = s.syncZoneSquadCheckpoint()
}

func (s *gameplayPeerSession) rollbackZoneSquadManaRestoration(
	restorations []zoneSquadManaRestoration,
) {
	if s == nil || s.squad == nil {
		return
	}
	for _, restoration := range restorations {
		previousManaPoint := restoration.manaPoint - restoration.amount
		_ = s.squad.SetManaPoints(restoration.creatureIndex, previousManaPoint)
	}
	_ = s.syncZoneHero()
	_ = s.syncZoneSquadCheckpoint()
}

type zoneOrbKind uint8

const (
	campaignOrbRestoreFraction             = float32(0.10)
	zoneHealthOrb              zoneOrbKind = iota
	zoneManaOrb
	zoneResurrectionOrb
)

func campaignOrbRestoredText(amount float32) uint32 {
	amount = max(float32(0), amount)
	restored := uint32(math.Round(float64(amount)))
	if amount > 0 && restored == 0 {
		return 1
	}
	return restored
}

func marshalCampaignOrbPickup(
	pickup zoneinteract.Orb, activeObjectID uint32, kind zoneOrbKind,
	isFull bool, hitPoint float32, mana float32, restored uint32,
) ([][]byte, error) {
	orbKind := interactraknet.OrbHealth
	if kind == zoneManaOrb {
		orbKind = interactraknet.OrbMana
	}
	if kind == zoneResurrectionOrb {
		orbKind = interactraknet.OrbResurrection
	}
	encoded, err := interactraknet.OrbPickup(interactraknet.OrbPickupRequest{
		PickupObjectID: pickup.ObjectID,
		ActiveObjectID: activeObjectID,
		Kind:           orbKind,
		Position: raknet.Vector3{
			X: pickup.Request.Destination.X,
			Y: pickup.Request.Destination.Y,
			Z: pickup.Request.Destination.Z,
		},
		HitPoint: hitPoint, ManaPoint: mana, Restored: restored, IsFull: isFull,
	})
	if err != nil {
		return nil, fmt.Errorf("pickupMarshal: %w", err)
	}
	return encoded, nil
}

// Compatibility budgets only for unresolved classes, not ordinary NPC tuning.
const campaignNPCCrystalSourceAmount = int32(26)
const campaignVerdanthTotemCrystalSourceAmount = int32(500)

func campaignDropDestination(
	source sim.Position, player raknet.Vector3,
) sim.Position {
	deltaX := player.X - source.X
	deltaY := player.Y - source.Y
	length := float32Hypot(deltaX, deltaY)
	if length == 0 {
		return sim.Position{X: source.X + 2, Y: source.Y, Z: source.Z}
	}
	// Build 103 samples source-centered navmesh rings, but the server does not
	// yet own the authored random ring query. Keep the compatibility destination
	// two units from the source in the player's direction so the drop remains
	// near the defeated object instead of suddenly landing beside the camera.
	return sim.Position{
		X: source.X + 2*deltaX/length,
		Y: source.Y + 2*deltaY/length,
		Z: source.Z,
	}
}

func (s *gameplayPeerSession) reachableCampaignDropDestination(
	source sim.Position,
) sim.Position {
	if s == nil {
		return source
	}
	fallback := campaignDropDestination(source, s.playerPosition)
	campaignNav := s.zone.Navigation()
	if campaignNav == nil {
		return fallback
	}
	planLayer, isLayerFound := campaignNav.SelectLayerForMode(
		campaignSecurityBlitzFootprintFallback, uint8(s.binding.Mode),
	)
	if !isLayerFound {
		return fallback
	}
	options := navigation.ProjectionOptions{
		PlanLayer:   planLayer,
		MaxDistance: zonenavigation.ProjectionDistance,
	}
	playerProjection, err := campaignNav.Project(navigation.Vec3{
		X: s.playerPosition.X,
		Y: s.playerPosition.Y,
		Z: s.playerPosition.Z,
	}, options)
	if err != nil {
		return fallback
	}
	options.ComponentID = playerProjection.ComponentID
	options.IsComponentConstrained = true
	options.MaxDistance = 6
	destination, err := campaignNav.Project(navigation.Vec3{
		X: fallback.X, Y: fallback.Y, Z: fallback.Z,
	}, options)
	if err != nil {
		destination = playerProjection
	}
	return sim.Position{
		X: destination.Position.X,
		Y: destination.Position.Y,
		Z: destination.Position.Z,
	}
}

func float32Hypot(x float32, y float32) float32 {
	return float32(math.Hypot(float64(x), float64(y)))
}

func (s *gameplayPeerSession) spawnCampaignCrystal(
	invocation game.CampaignScriptInvocation,
	definitions []sim.CrystalDefinition,
	offsets []sim.CrystalLevelOffset,
	sourceTime uint64,
	dropBag *campaignCrystalDropBag,
	selectionBag *campaignCrystalSelectionBag,
) ([][]byte, uint32, error) {
	if s == nil || s.zone == nil || invocation.Challenge <= 0 {
		return nil, 0, errors.New("campaign crystal unavailable")
	}
	if !zoneunlock.AreCatalystDropsUnlocked(s.binding) {
		return nil, 0, nil
	}
	// Focused transport fixtures may omit the content-owned weighted catalog.
	// Production program loading validates the complete 192-row/offset set.
	if len(definitions) == 0 {
		return nil, 0, nil
	}
	if s.zone.DropRandom() == nil {
		return nil, 0, errors.New("campaign drop random unavailable")
	}
	source := sim.Position{
		X: invocation.Position.X,
		Y: invocation.Position.Y,
		Z: invocation.Position.Z,
	}
	destination := s.reachableCampaignDropDestination(source)
	// Destination resolution belongs before the ordinary attempt. A stage
	// below the catalyst minimum consumes neither chance nor selection draws.
	stage, err := zoneLevelIndex(s.binding)
	if err != nil {
		return nil, 0, fmt.Errorf("crystalStage: %w", err)
	}
	if !sim.IsCrystalDropStageEligible(stage, 0) {
		return nil, 0, nil
	}
	draw, err := s.zone.DropRandom().Index(100)
	if err != nil {
		return nil, 0, fmt.Errorf("crystalChance: %w", err)
	}
	challenge := invocation.Challenge
	chanceScale := 1 + s.campaignPartAttribute(campaignCrystalFindAttribute)
	chanceThreshold := float32(0)
	if dropBag != nil {
		chanceThreshold, err = sim.CrystalDropThreshold(challenge, chanceScale)
		if err != nil {
			return nil, 0, fmt.Errorf("crystalThreshold: %w", err)
		}
		chanceThreshold = dropBag.chanceThreshold(chanceThreshold)
	}
	recentTypes := []int32(nil)
	rarityMisses := []uint32(nil)
	if selectionBag != nil {
		recentTypes, rarityMisses = selectionBag.policy()
	}
	pickup, isDrop, err := zoneloot.PlanCrystal(
		zoneloot.CrystalPlanInput{
			Challenge: challenge, ChanceScale: chanceScale,
			RandomDraw: draw, ChanceThreshold: chanceThreshold,
			IsChanceThresholdOverridden: dropBag != nil,
			World: sim.CrystalDropInput{
				CurrentDifficulty: stage,
				MinorStageCount:   s.zone.CrystalMinorStageCount(),
				PlayerRole:        zoneinteract.PlayerRole,
				SourceRole:        zoneinteract.Role,
				ControlledRole:    "playerAgent", PlayerCount: 1,
				SimulationTime: time.Duration(sourceTime) * time.Millisecond,
				SourcePosition: source,
				Destinations:   []sim.Position{destination},
				PickupRoles:    []sim.Role{"crystal"},
				Definitions:    definitions, LevelOffsets: offsets,
				RecentTypes: recentTypes, RarityMisses: rarityMisses,
				Random: s.zone.DropRandom(),
			},
		},
	)
	if err != nil {
		return nil, 0, fmt.Errorf("crystalPlan: %w", err)
	}
	if !isDrop {
		return nil, 0, nil
	}
	objectID, err := s.reserveCampaignObjectID()
	if err != nil {
		return nil, 0, fmt.Errorf("crystalObjectID: %w", err)
	}
	packets, err := s.marshalCrystalDrop(pickup, objectID)
	if err != nil {
		return nil, 0, fmt.Errorf("crystalMarshal: %w", err)
	}
	err = s.registerCampaignPickup(
		zoneinteract.PickupCrystal, objectID, pickup.Position,
		pickup.Destination,
		s.pickupFlight(pickup.NounName, pickup.Position, pickup.Destination, pickup.Lob),
	)
	if err != nil {
		return nil, 0, fmt.Errorf("crystalRegister: %w", err)
	}
	err = s.zone.PickupPayload().AddCrystal(zoneinteract.CrystalPickup{
		ObjectID: objectID, Request: pickup,
		IsLootBagReward: selectionBag != nil,
		Object: sim.CrystalPickupObject{
			Role: pickup.Role, NounName: pickup.NounName,
			NounAsset:   util.HashID(pickup.NounName),
			CrystalType: pickup.CrystalType, CrystalLevel: pickup.CrystalLevel,
			Rarity:   pickup.Rarity,
			Position: pickup.Destination,
			IsLive:   true, IsPhaseOwned: true, IsLootDataPresent: true,
		},
	})
	if err != nil {
		s.zone.Pickups().Remove(objectID)
		return nil, 0, fmt.Errorf("crystalTrack: %w", err)
	}
	return packets, objectID, nil
}

func (s *gameplayPeerSession) materializeCampaignCatalystUnlock(
	batch unlockraknet.CatalystBatch,
	source sim.Position,
	simulationTime time.Duration,
) ([][]byte, error) {
	if s == nil ||
		!isFiniteCampaignPopulationPosition(game.Vec3{
			X: source.X, Y: source.Y, Z: source.Z,
		}) {
		return nil, errors.New("campaign catalyst unavailable")
	}
	if len(batch.Packets) == 0 && len(batch.CrystalDrops) == 0 {
		return nil, nil
	}
	if len(batch.Packets) > 1 || len(batch.CrystalDrops) != 1 ||
		len(s.zone.CrystalDefinitions()) == 0 ||
		len(s.zone.CrystalLevelOffsets()) == 0 {
		return nil, errors.New("campaign catalyst batch incomplete")
	}
	if s.zone.DropRandom() == nil {
		return nil, errors.New("campaign drop random unavailable")
	}
	destination := s.reachableCampaignDropDestination(source)
	request, err := batch.BuildCrystalWorldRequest(sim.CrystalDropInput{
		CurrentDifficulty: s.binding.Difficulty,
		MinorStageCount:   s.zone.CrystalMinorStageCount(),
		PlayerRole:        zoneunlock.PlayerRole,
		SourceRole:        "boss", ControlledRole: "playerAgent", PlayerCount: 1,
		SimulationTime: simulationTime, SourcePosition: source,
		Destinations: []sim.Position{destination},
		PickupRoles:  []sim.Role{"crystal"},
		Definitions:  s.zone.CrystalDefinitions(),
		LevelOffsets: s.zone.CrystalLevelOffsets(),
		Random:       s.zone.DropRandom(),
	})
	if err != nil {
		return nil, fmt.Errorf("catalystPlan: %w", err)
	}
	if len(request.Pickups) == 0 {
		s.binding.IsCatalystUnlocked = true
		return append([][]byte(nil), batch.Packets...), nil
	}
	if len(request.Pickups) != 1 {
		return nil, fmt.Errorf("catalystCount: %d", len(request.Pickups))
	}
	objectID, err := s.reserveCampaignObjectID()
	if err != nil {
		return nil, fmt.Errorf("catalystObjectID: %w", err)
	}
	pickup := request.Pickups[0]
	worldPackets, err := s.marshalCrystalDrop(pickup, objectID)
	if err != nil {
		return nil, fmt.Errorf("catalystMarshal: %w", err)
	}
	err = s.registerCampaignPickup(
		zoneinteract.PickupCrystal, objectID, pickup.Position,
		pickup.Destination,
		s.pickupFlight(pickup.NounName, pickup.Position, pickup.Destination, pickup.Lob),
	)
	if err != nil {
		return nil, fmt.Errorf("catalystRegister: %w", err)
	}
	err = s.zone.PickupPayload().AddCrystal(zoneinteract.CrystalPickup{
		ObjectID: objectID, Request: pickup,
		Object: sim.CrystalPickupObject{
			Role: pickup.Role, NounName: pickup.NounName,
			NounAsset:   util.HashID(pickup.NounName),
			CrystalType: pickup.CrystalType, CrystalLevel: pickup.CrystalLevel,
			Rarity:   pickup.Rarity,
			Position: pickup.Destination,
			IsLive:   true, IsPhaseOwned: true, IsLootDataPresent: true,
		},
	})
	if err != nil {
		s.zone.Pickups().Remove(objectID)
		return nil, fmt.Errorf("catalystTrack: %w", err)
	}
	s.binding.IsCatalystUnlocked = true
	packets := append([][]byte(nil), batch.Packets...)
	return append(packets, worldPackets...), nil
}

func (s *gameplayPeerSession) releaseCampaignCrystalSchedule(
	objectID uint32, run *interactraknet.CrystalPickupRun,
) bool {
	if s == nil || s.campaignSchedule == nil || objectID == 0 || run == nil {
		return false
	}
	return s.campaignSchedule.Remove(zoneaction.ScheduleCrystal, objectID, run)
}

type campaignInteractionRuntime struct {
	registry      *gameplaySessionRegistry
	progression   campaignLootProgression
	crystalPickup sim.Program
	logger        *log.Logger
	gameplayJoin  *game.GameplayJoin
	now           func() time.Time

	interactWithObelisk   sim.AbilityDefinition
	interactHealthObelisk sim.AbilityDefinition
	crystalDefinitions    []sim.CrystalDefinition
	crystalLevelOffsets   []sim.CrystalLevelOffset
}

func (r campaignInteractionRuntime) releaseSchedule(
	sessionKey string, generation uint64, objectID uint32,
	run *zoneinteract.Run,
) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isReleased := isFound && peerSession.generation == generation &&
		peerSession.releaseCampaignInteractableSchedule(objectID, run)
	if isReleased {
		r.registry.sessions[sessionKey] = peerSession
	}
	r.registry.mutex.Unlock()
}

func (s *gameplayPeerSession) releaseCampaignInteractableSchedule(
	objectID uint32, run *zoneinteract.Run,
) bool {
	if s == nil || objectID == 0 || run == nil ||
		s.campaignSchedule == nil {
		return false
	}
	return s.campaignSchedule.Remove(
		zoneaction.ScheduleInteractable, objectID, run,
	)
}

type campaignInteractableResolver struct {
	objectBinding raknet103.Binding
	playerBinding raknet103.Binding
}

func (r campaignInteractableResolver) ResolveRole(
	_ context.Context, role sim.Role,
) (raknet103.Binding, error) {
	switch role {
	case zoneinteract.Role:
		return r.objectBinding, nil
	case zoneinteract.PlayerRole:
		return r.playerBinding, nil
	default:
		return raknet103.Binding{}, fmt.Errorf("roleMissing: %s", role)
	}
}

type campaignInteractablePacketAdapter struct {
	ctx     context.Context
	run     *zoneinteract.Run
	encoder *raknet103.Encoder
}

func newCampaignInteractablePacketAdapter(
	ctx context.Context, run *zoneinteract.Run,
	invocation game.CampaignScriptInvocation, playerPosition raknet.Vector3,
	sourceTime uint64,
) (*campaignInteractablePacketAdapter, error) {
	resolver := campaignInteractableResolver{
		objectBinding: raknet103.Binding{
			ObjectID: invocation.TargetObjectID,
			Position: sim.Position{
				X: invocation.Position.X, Y: invocation.Position.Y,
				Z: invocation.Position.Z,
			},
		},
		playerBinding: raknet103.Binding{
			ObjectID: invocation.SourceObjectID,
			Position: sim.Position{
				X: playerPosition.X, Y: playerPosition.Y, Z: playerPosition.Z,
			},
		},
	}
	encoder, err := raknet103.NewEncoder(resolver, sourceTime)
	if err != nil {
		return nil, fmt.Errorf("encoderCreate: %w", err)
	}
	return &campaignInteractablePacketAdapter{ctx: ctx, run: run, encoder: encoder}, nil
}

func (a *campaignInteractablePacketAdapter) encode(event []sim.Event) ([][]byte, error) {
	packets, err := a.encoder.EncodeBatch(a.ctx, event)
	if err != nil {
		return nil, fmt.Errorf("eventEncode: %w", err)
	}
	return packets, nil
}

func (a *campaignInteractablePacketAdapter) advance(
	deadline time.Duration,
) ([][]byte, error) {
	event, err := a.run.Advance(a.ctx, deadline)
	if err != nil {
		return nil, fmt.Errorf("advance[%s]: %w", deadline, err)
	}
	return a.encode(event)
}

type campaignInteractableAdvanceStep struct {
	registry   *gameplaySessionRegistry
	adapter    *campaignInteractablePacketAdapter
	sessionKey string
	generation uint64
	deadline   time.Duration
}

func (s campaignInteractableAdvanceStep) produce() ([][]byte, error) {
	s.registry.mutex.RLock()
	peerSession, isFound := s.registry.sessions[s.sessionKey]
	isCurrent := isFound && peerSession.generation == s.generation
	s.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	return s.adapter.advance(s.deadline)
}

type campaignInteractableDropStep struct {
	runtime    campaignInteractionRuntime
	adapter    *campaignInteractablePacketAdapter
	sessionKey string
	generation uint64
	use        game.CampaignScriptUse
	sourceTime uint64
	deadline   time.Duration
}

func (s campaignInteractableDropStep) produce() ([][]byte, error) {
	s.runtime.registry.mutex.RLock()
	currentSession, isCurrentFound :=
		s.runtime.registry.sessions[s.sessionKey]
	isCurrent := isCurrentFound &&
		currentSession.generation == s.generation
	s.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	packet, err := s.adapter.advance(s.deadline)
	if err != nil {
		return packet, err
	}
	s.runtime.registry.mutex.Lock()
	peerSession, isFound := s.runtime.registry.sessions[s.sessionKey]
	if !isFound || peerSession.generation != s.generation {
		s.runtime.registry.mutex.Unlock()
		return packet, nil
	}
	dropPacket := make([][]byte, 0, 3)
	dropSourceTime := s.sourceTime + uint64(s.deadline/time.Millisecond)
	switch s.use.Invocation.CallbackName {
	case "InteractHealthObelisk":
		dropPacket, _, err = peerSession.spawnHealthObeliskCapsules(
			s.use.Invocation, dropSourceTime, s.runtime.now(), s.runtime.registry,
		)
	case "InteractWithObelisk":
		crystalPackets, crystalObjectIDs, crystalErr := peerSession.spawnCampaignCrystalAttempts(
			s.use.Invocation, s.runtime.crystalDefinitions,
			s.runtime.crystalLevelOffsets, dropSourceTime, 4, nil,
		)
		dropPacket = append(dropPacket, crystalPackets...)
		equipmentPackets, equipmentObjectID, equipmentErr := peerSession.spawnCampaignEquipment(
			s.use.Invocation, s.runtime.gameplayJoin, dropSourceTime,
			campaignLootObeliskDropMask, false,
		)
		dropPacket = append(dropPacket, equipmentPackets...)
		// Each category rolls back its unpublished batch on error. Preserve
		// the other category's successful publication and consumed object IDs.
		if len(crystalObjectIDs) == 0 && len(crystalPackets) != 0 {
			crystalErr = errors.New("crystal batch identity missing")
		}
		if equipmentObjectID == 0 && len(equipmentPackets) != 0 {
			equipmentErr = errors.New("equipment batch identity missing")
		}
		err = errors.Join(crystalErr, equipmentErr)
		if err != nil {
			s.runtime.logger.Printf("RakNet campaign interactable loot partially generated for %s: %v", s.sessionKey, err)
			// The schedule drops output on failure. Publish the successful
			// independent category instead of turning it into invisible loot.
			err = nil
		}
	}
	s.runtime.registry.sessions[s.sessionKey] = peerSession
	s.runtime.registry.mutex.Unlock()
	if err != nil {
		return append(packet, dropPacket...), fmt.Errorf("campaignInteractableLoot: %w", err)
	}
	return append(packet, dropPacket...), nil
}

type campaignInteractableFinalStep struct {
	runtime    campaignInteractionRuntime
	adapter    *campaignInteractablePacketAdapter
	sessionKey string
	generation uint64
	objectID   uint32
	markerID   uint32
	run        *zoneinteract.Run
	deadline   time.Duration
	release    []byte
}

func (s campaignInteractableFinalStep) produce() ([][]byte, error) {
	s.runtime.registry.mutex.RLock()
	peerSession, isFound := s.runtime.registry.sessions[s.sessionKey]
	isCurrent := isFound && peerSession.generation == s.generation
	s.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	packet, err := s.adapter.advance(s.deadline)
	if err != nil {
		return nil, fmt.Errorf("interactableFinal: %w", err)
	}
	// InteractWithObelisk/InteractHealthObelisk call
	// RemovePhysicsForObject after finalWaitTime. The wire event already
	// removes client physics; retire the same server navigation obstacle.
	peerSession.zone.Navigation().DisableObstacle(s.markerID)
	packet = append(packet, s.release)
	s.runtime.releaseSchedule(
		s.sessionKey, s.generation, s.objectID, s.run,
	)
	return packet, nil
}

type campaignInteractableScheduleFailure struct {
	runtime    campaignInteractionRuntime
	sessionKey string
	generation uint64
	objectID   uint32
	run        *zoneinteract.Run
}

func (f campaignInteractableScheduleFailure) handle(scheduleErr error) {
	f.runtime.releaseSchedule(
		f.sessionKey, f.generation, f.objectID, f.run,
	)
	f.run.Stop()
	f.runtime.logger.Printf(
		"RakNet campaign interactable schedule failed for %s: %v",
		f.sessionKey, scheduleErr,
	)
}

func (r campaignInteractionRuntime) handleScriptUse(
	ctx context.Context, packet raknet.Packet, command raknet.ActionCommandData, sessionKey string,
	isCatalystPickupCommand bool,
) ([][]byte, error) {
	var err error
	r.registry.mutex.Lock()
	currentSession, isCurrentFound := r.registry.sessions[sessionKey]
	if isCatalystPickupCommand {
		r.registry.mutex.Unlock()
		return r.rejectPickup(command, "target unavailable")
	}
	isDefeatedHero := isCurrentFound && currentSession.squad != nil &&
		(currentSession.isHeroSelectionPending ||
			currentSession.deployedHitPoint() <= 0)
	if isDefeatedHero {
		r.registry.mutex.Unlock()
		rejectionPacket, marshalErr := actionraknet.Reject(command)
		if marshalErr != nil {
			return nil, fmt.Errorf("campaignInteractableDefeatedReject: %w", marshalErr)
		}
		r.logger.Printf(
			"RakNet campaign interactable rejected source=%d target=%d reason=hero selection pending",
			command.Common.ObjectID, command.Value,
		)
		return [][]byte{rejectionPacket}, nil
	}
	var use game.CampaignScriptUse
	rejection := game.CampaignScriptUseRejectedIdentity
	if isCurrentFound && currentSession.zone != nil && command.Common.ObjectID == currentSession.deployedObjectID &&
		currentSession.zone.Script() != nil {
		currentSession.interactablePursuit = nil
		// Native interactable pursuit moves the hero without ground-move
		// commands. Sample its bounded server route before testing use range.
		err = currentSession.advancePlayerPosition(r.now(), command.Common.Position)
		if err != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("interactablePosition: %w", err)
		}
		err = currentSession.syncZoneHeroPose()
		if err != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("interactablePose: %w", err)
		}
		r.registry.sessions[sessionKey] = currentSession
		use, rejection, err = currentSession.zone.Script().PrepareUse(
			currentSession.deployedObjectID, command.Common.ObjectID, command.Value,
			game.Vec3{
				X: currentSession.playerPosition.X, Y: currentSession.playerPosition.Y,
				Z: currentSession.playerPosition.Z,
			},
			campaignInteractableCompatibilityRange,
		)
	}
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignInteractablePrepare: %w", err)
	}
	approach := use.Invocation.Position
	if rejection == "" || rejection == game.CampaignScriptUseRejectedDistance {
		var navigationErr error
		approach, navigationErr = zonenavigation.InteractionDestination(
			currentSession.zone.Navigation(),
			game.Vec3{
				X: currentSession.playerPosition.X,
				Y: currentSession.playerPosition.Y,
				Z: currentSession.playerPosition.Z,
			},
			use.Invocation.Position,
			currentSession.deployedCampaignNavigationRadius(),
		)
		if navigationErr != nil {
			r.registry.mutex.Unlock()
			r.logger.Printf(
				"RakNet campaign interactable rejected source=%d target=%d reason=navigation source_position=(%g,%g,%g) target_position=(%g,%g,%g): %v",
				command.Common.ObjectID, command.Value,
				currentSession.playerPosition.X, currentSession.playerPosition.Y,
				currentSession.playerPosition.Z,
				use.Invocation.Position.X, use.Invocation.Position.Y,
				use.Invocation.Position.Z, navigationErr,
			)
			rejectionPacket, marshalErr := actionraknet.Reject(command)
			if marshalErr != nil {
				return nil, fmt.Errorf("campaignInteractableNavigationReject: %w", marshalErr)
			}
			return [][]byte{rejectionPacket}, nil
		}
	}
	if rejection != "" {
		invocation := use.Invocation
		if rejection == game.CampaignScriptUseRejectedDistance {
			pursuitPackets, marshalErr := actionraknet.PursuitTransfer(
				command.Common.Unknown[0], command.Common.ObjectID,
			)
			if marshalErr != nil {
				r.registry.mutex.Unlock()
				return nil, fmt.Errorf("campaignInteractablePursuit: %w", marshalErr)
			}
			if currentSession.isEnemyRootActive(r.now()) || currentSession.isSleepingCloudActive(r.now()) {
				r.registry.mutex.Unlock()
				packets, rejectErr := r.rejectScriptUse(command, "cannot pursue while immobilized")
				if rejectErr != nil {
					return nil, fmt.Errorf("interactableRootReject: %w", rejectErr)
				}
				return packets, nil
			}
			previousPosition, playerPosition, movementErr := currentSession.advancePlayerPursuitMovement(
				r.now(), currentSession.playerPosition, raknet.Vector3(approach),
				r.registry.passiveMovementIncrease(currentSession),
			)
			if movementErr != nil {
				r.registry.mutex.Unlock()
				return nil, fmt.Errorf("interactablePursuitPath: %w", movementErr)
			}
			if previousPosition != playerPosition {
				poseErr := currentSession.syncZoneHeroPose()
				if poseErr != nil {
					r.registry.mutex.Unlock()
					return nil, fmt.Errorf("interactablePursuitPose: %w", poseErr)
				}
			}
			currentSession.interactablePursuit = &campaignInteractablePursuit{
				runtime: r, sessionKey: sessionKey, generation: currentSession.generation,
				transportGeneration: currentSession.transportGeneration,
				motionRevision:      currentSession.playerMotionRevision(), command: command,
				destination: approach, expiresAt: r.now().Add(zoneaction.PursuitTimeout),
			}
			r.registry.sessions[sessionKey] = currentSession
			r.registry.mutex.Unlock()
			r.logger.Printf("RakNet campaign interactable pursuit transferred source=%d target=%d source_position=(%g,%g,%g) target_position=(%g,%g,%g) range=%g",
				command.Common.ObjectID, command.Value,
				currentSession.playerPosition.X, currentSession.playerPosition.Y,
				currentSession.playerPosition.Z,
				invocation.Position.X, invocation.Position.Y, invocation.Position.Z,
				campaignInteractableCompatibilityRange)
			return pursuitPackets, nil
		}
		r.registry.mutex.Unlock()
		r.logger.Printf("RakNet campaign interactable rejected source=%d target=%d reason=%s source_position=(%g,%g,%g) target_position=(%g,%g,%g) range=%g",
			command.Common.ObjectID, command.Value, rejection,
			currentSession.playerPosition.X, currentSession.playerPosition.Y,
			currentSession.playerPosition.Z,
			invocation.Position.X, invocation.Position.Y, invocation.Position.Z,
			campaignInteractableCompatibilityRange)
		if rejection != game.CampaignScriptUseRejectedDistance {
			rejectionPacket, marshalErr := actionraknet.Reject(command)
			if marshalErr != nil {
				return nil, fmt.Errorf("campaignInteractableReject: %w", marshalErr)
			}
			return [][]byte{rejectionPacket}, nil
		}
		return nil, nil
	}
	ability := sim.AbilityDefinition{}
	switch use.Invocation.CallbackName {
	case "InteractWithObelisk":
		ability = r.interactWithObelisk
	case "InteractHealthObelisk":
		ability = r.interactHealthObelisk
	default:
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignInteractableAbility: %q", use.Invocation.CallbackName)
	}
	interactableRun, startEvent, runErr := zoneinteract.NewRun(ctx, use, ability)
	if runErr != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignInteractableRun: %w", runErr)
	}
	packetAdapter, adapterErr := newCampaignInteractablePacketAdapter(
		ctx, interactableRun, use.Invocation,
		currentSession.playerPosition, packet.SourceTime,
	)
	if adapterErr != nil {
		interactableRun.Stop()
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignInteractableAdapter: %w", adapterErr)
	}
	startPackets, encodeErr := packetAdapter.encode(startEvent)
	if encodeErr != nil {
		interactableRun.Stop()
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignInteractableStart: %w", encodeErr)
	}
	scriptPackets, marshalErr := objectraknet.ScriptUse(use)
	if marshalErr != nil {
		interactableRun.Stop()
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignInteractableMarshal: %w", marshalErr)
	}
	if packet.ScheduleGroupResult == nil && packet.ScheduleGroup == nil {
		interactableRun.Stop()
		r.registry.mutex.Unlock()
		return nil, errors.New("campaignInteractableSchedule: unavailable")
	}
	if currentSession.campaignScheduleSession().Has(
		zoneaction.ScheduleInteractable, use.Invocation.TargetObjectID,
	) {
		interactableRun.Stop()
		r.registry.mutex.Unlock()
		return nil, errors.New("campaignInteractableSchedule: duplicate")
	}
	deadline := interactableRun.Deadlines()
	if len(deadline) != 3 {
		interactableRun.Stop()
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignInteractableDeadlines: %d", len(deadline))
	}
	acceptPacket, marshalErr := actionraknet.Accept(
		command, ability.Name, packet.SourceTime, deadline[0], deadline[2],
	)
	if marshalErr != nil {
		interactableRun.Stop()
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignInteractableAccept: %w", marshalErr)
	}
	abilityIndex := uint32(0)
	if command.Ability != nil {
		abilityIndex = command.Ability.Index
	}
	releasePacket, marshalErr := abilityraknet.ReleaseResponse(
		command.Common.Unknown[0], util.HashID(ability.Name), abilityIndex,
		packet.SourceTime, deadline[0], deadline[2],
	)
	if marshalErr != nil {
		interactableRun.Stop()
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignInteractableRelease: %w", marshalErr)
	}
	stopErr := currentSession.stopPlayerMovement(r.now())
	if stopErr != nil {
		interactableRun.Stop()
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignInteractableStop: %w", stopErr)
	}
	stopPackets, marshalErr := marshalZonePlayerStop(
		currentSession.deployedObjectID, currentSession.playerPosition,
	)
	if marshalErr != nil {
		interactableRun.Stop()
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignInteractableStopMarshal: %w", marshalErr)
	}
	generation := currentSession.generation
	producerIdentity := gameplayProducerIdentityFromSession(
		sessionKey, currentSession, isCurrentFound,
	)
	advanceStep := campaignInteractableAdvanceStep{
		registry: r.registry, adapter: packetAdapter,
		sessionKey: sessionKey, generation: generation,
		deadline: deadline[0],
	}
	dropStep := campaignInteractableDropStep{
		runtime: r, adapter: packetAdapter, sessionKey: sessionKey,
		generation: generation, use: use, sourceTime: packet.SourceTime,
		deadline: deadline[1],
	}
	finalStep := campaignInteractableFinalStep{
		runtime: r, adapter: packetAdapter, sessionKey: sessionKey,
		generation: generation, objectID: use.Invocation.TargetObjectID,
		markerID: use.Invocation.MarkerID,
		run:      interactableRun, deadline: deadline[2], release: releasePacket,
	}
	producers := []raknet.ScheduledPacketProducer{
		{Delay: deadline[0], Produce: advanceStep.produce},
		{Delay: deadline[1], Produce: dropStep.produce},
		{Delay: deadline[2], Produce: finalStep.produce},
	}
	producers = r.registry.producerGuard.scheduledProducersForIdentity(
		producerIdentity, producers,
	)
	scheduleFailure := campaignInteractableScheduleFailure{
		runtime: r, sessionKey: sessionKey, generation: generation,
		objectID: use.Invocation.TargetObjectID, run: interactableRun,
	}
	var interactableCancel raknet.CancelSchedule
	if packet.ScheduleGroupResult != nil {
		interactableCancel, runErr = packet.ScheduleGroupResult(
			producers, scheduleFailure.handle,
		)
	} else {
		interactableCancel, runErr = packet.ScheduleGroup(producers)
	}
	if runErr != nil || interactableCancel == nil {
		interactableRun.Stop()
		r.registry.mutex.Unlock()
		if runErr == nil {
			runErr = errors.New("nil cancellation")
		}
		return nil, fmt.Errorf("campaignInteractableSchedule: %w", runErr)
	}
	var objectiveUpdates []zoneobjective.Update
	isObjectiveObelisk := use.Invocation.CallbackName == "InteractWithObelisk" ||
		use.Invocation.CallbackName == "InteractHealthObelisk"
	if isObjectiveObelisk {
		if currentSession.zone == nil || currentSession.zone.Objective() == nil {
			interactableCancel()
			interactableRun.Stop()
			r.registry.mutex.Unlock()
			return nil, errors.New("campaignInteractableObjective: unavailable")
		}
	}
	err = currentSession.campaignScheduleSession().Add(
		zoneaction.ScheduleInteractable, use.Invocation.TargetObjectID,
		interactableRun, interactableCancel, nil,
	)
	if err != nil {
		interactableCancel()
		interactableRun.Stop()
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignInteractableTrack: %w", err)
	}
	err = currentSession.zone.Script().CommitUse(use)
	if err != nil {
		currentSession.releaseCampaignInteractableSchedule(
			use.Invocation.TargetObjectID, interactableRun,
		)
		interactableCancel()
		interactableRun.Stop()
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignInteractableCommit: %w", err)
	}
	if isObjectiveObelisk {
		objectiveEventKind := sim.LuaObjectiveEventTouchedObelisk
		if use.Invocation.CallbackName == "InteractHealthObelisk" {
			objectiveEventKind = sim.LuaObjectiveEventTouchedHealthObelisk
		}
		objectiveUpdates, err = currentSession.zone.Objective().ApplyEvent(
			ctx, sim.LuaObjectiveEvent{
				Kind:                       objectiveEventKind,
				ObjectID:                   use.Invocation.TargetObjectID,
				IsInteractableUseAvailable: true,
			},
		)
		if err != nil {
			objectiveUpdates = nil
			if r.logger != nil {
				r.logger.Printf(
					"RakNet campaign obelisk objective omitted target=%d callback=%q: %v",
					use.Invocation.TargetObjectID,
					use.Invocation.CallbackName, err,
				)
			}
		}
	}
	var obeliskVoicePackets [][]byte
	if use.Invocation.CallbackName == "InteractWithObelisk" {
		obeliskVoicePackets, err = objectiveraknet.ObeliskAccessedPackets(
			objectiveUpdates, uint8(currentSession.binding.Slot),
		)
		if err != nil {
			// The use and objective are committed. Optional voice projection
			// must not cancel the accepted interactable execution afterward.
			obeliskVoicePackets = nil
			if r.logger != nil {
				r.logger.Printf("RakNet campaign obelisk voice omitted: %v", err)
			}
		}
	}
	currentSession.zone.PublishObjective(objectiveUpdates)
	r.registry.sessions[sessionKey] = currentSession
	r.registry.mutex.Unlock()
	invocation := use.Invocation
	r.logger.Printf("RakNet campaign interactable consumed source=%d target=%d marker=%d callback=%q scripts=%d used=%d allowed=%d execution_scheduled=true",
		invocation.SourceObjectID, invocation.TargetObjectID, invocation.MarkerID,
		invocation.CallbackName, len(invocation.Bindings), use.UseCount, use.UseLimit)
	response := append([][]byte{acceptPacket}, stopPackets...)
	response = append(response, startPackets...)
	response = append(response, scriptPackets...)
	response = append(response, obeliskVoicePackets...)
	return response, nil
}
