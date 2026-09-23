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
	"github.com/darkspinnet/darkspin/server/squad"
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
	lootraknet "github.com/darkspinnet/darkspin/server/zone/loot/raknet103"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	objectraknet "github.com/darkspinnet/darkspin/server/zone/object/raknet103"
	zoneobjective "github.com/darkspinnet/darkspin/server/zone/objective"
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
	source sim.Position, destination sim.Position,
) error {
	if s == nil {
		return errors.New("nil campaign pickup session")
	}
	if s.zone.Pickups() == nil {
		return errors.New("campaign pickup registry unavailable")
	}
	err := s.zone.Pickups().Register(zoneinteract.Pickup{
		ObjectID: objectID, Kind: kind,
		Position: game.Vec3{
			X: destination.X, Y: destination.Y, Z: destination.Z,
		},
		SourcePosition: game.Vec3{
			X: source.X, Y: source.Y, Z: source.Z,
		},
		IsSourcePositionKnown: true,
	})
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
	})
}

func (s *gameplayPeerSession) reserveCampaignPickupContact(
	objectID uint32, start raknet.Vector3, end raknet.Vector3, maximumDistance float32,
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
	if command.Ability != nil {
		abilityIndex = command.Ability.Index
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
		inventoryReader, isInventoryReader := r.progression.(campaignInventoryReader)
		if !isInventoryReader {
			currentSession.zone.Pickups().Release(command.Value)
			r.registry.mutex.Unlock()
			return nil, errors.New("campaignEquipmentCapacity: unavailable")
		}
		if equipmentPickup.WinnerUserID == 0 {
			participants := r.equipmentRollParticipantsLocked(currentSession)
			eligibleParticipants := make(
				[]zoneloot.EquipmentRollParticipant, 0, len(participants),
			)
			inventoryStatuses := make(
				map[uint64]sporenet.PartInventoryStatus, len(participants),
			)
			for _, participant := range participants {
				inventoryStatus, statusErr := inventoryReader.PartInventoryStatus(
					ctx, int64(participant.UserID),
				)
				if statusErr != nil {
					currentSession.zone.Pickups().Release(command.Value)
					r.registry.mutex.Unlock()
					return nil, fmt.Errorf(
						"campaignEquipmentCapacity[%d]: %w", participant.UserID, statusErr,
					)
				}
				inventoryStatuses[participant.UserID] = inventoryStatus
				if inventoryStatus.IsFull {
					if r.logger != nil {
						r.logger.Printf(
							"RakNet campaign equipment roll excluded full inventory game=%d user=%d object=%d owned=%d capacity=%d",
							currentSession.binding.GameID, participant.UserID,
							equipmentPickup.ObjectID, inventoryStatus.OwnedCount,
							inventoryStatus.Capacity,
						)
					}
					continue
				}
				eligibleParticipants = append(eligibleParticipants, participant)
			}
			if len(eligibleParticipants) == 0 {
				currentSession.zone.Pickups().Release(command.Value)
				gameID := currentSession.binding.GameID
				userID := currentSession.binding.UserID
				inventoryStatus := inventoryStatuses[userID]
				r.registry.mutex.Unlock()
				notificationErr := r.gameplayJoin.PublishInventoryFull(
					context.WithoutCancel(ctx), int64(userID), gameID,
					inventoryStatus.OwnedCount, inventoryStatus.Capacity,
				)
				if notificationErr != nil && r.logger != nil {
					r.logger.Printf(
						"RakNet campaign equipment all-full notice failed user=%d object=%d: %v",
						userID, equipmentPickup.ObjectID, notificationErr,
					)
				}
				return r.rejectPickup(command, "inventory full")
			}
			rollResult, rollErr := zoneloot.RollEquipment(
				eligibleParticipants, currentSession.zone.DropRandom(),
			)
			if rollErr != nil {
				currentSession.zone.Pickups().Release(command.Value)
				r.registry.mutex.Unlock()
				return nil, fmt.Errorf("campaignEquipmentRoll: %w", rollErr)
			}
			pickupRolls := make(
				[]zoneinteract.EquipmentPickupRoll, 0, len(rollResult.Rolls),
			)
			for _, roll := range rollResult.Rolls {
				pickupRolls = append(pickupRolls, zoneinteract.EquipmentPickupRoll{
					UserID: roll.UserID, ObjectID: roll.ObjectID, Roll: roll.Roll,
				})
			}
			equipmentPickup, rollErr = currentSession.zone.PickupPayload().
				SetEquipmentRoll(
					command.Value, rollResult.Winner.UserID, pickupRolls,
				)
			if rollErr != nil {
				currentSession.zone.Pickups().Release(command.Value)
				r.registry.mutex.Unlock()
				return nil, fmt.Errorf("campaignEquipmentWinner: %w", rollErr)
			}
			if r.logger != nil {
				r.logger.Printf(
					"RakNet campaign equipment roll target=%d winner_user=%d winner_slot=%d winner_roll=%d rolls=%v",
					command.Value, rollResult.Winner.UserID,
					rollResult.Winner.Slot, rollResult.Winner.Roll,
					rollResult.Rolls,
				)
			}
		}
		inventoryStatus, statusErr := inventoryReader.PartInventoryStatus(
			ctx, int64(equipmentPickup.WinnerUserID),
		)
		if statusErr != nil {
			currentSession.zone.Pickups().Release(command.Value)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignEquipmentCapacity: %w", statusErr)
		}
		if inventoryStatus.IsFull {
			clearErr := currentSession.zone.PickupPayload().ClearEquipmentRoll(
				equipmentPickup.ObjectID, equipmentPickup.WinnerUserID,
			)
			currentSession.zone.Pickups().Release(command.Value)
			gameID := currentSession.binding.GameID
			r.registry.mutex.Unlock()
			if clearErr != nil && r.logger != nil {
				r.logger.Printf(
					"RakNet campaign equipment full winner reset failed user=%d object=%d: %v",
					equipmentPickup.WinnerUserID, equipmentPickup.ObjectID, clearErr,
				)
			}
			notificationErr := r.gameplayJoin.PublishInventoryFull(
				context.WithoutCancel(ctx), int64(equipmentPickup.WinnerUserID), gameID,
				inventoryStatus.OwnedCount, inventoryStatus.Capacity,
			)
			if notificationErr != nil && r.logger != nil {
				r.logger.Printf(
					"RakNet campaign equipment full-inventory notice failed user=%d object=%d: %v",
					equipmentPickup.WinnerUserID, equipmentPickup.ObjectID, notificationErr,
				)
			}
			if r.logger != nil {
				r.logger.Printf(
					"RakNet campaign equipment rejected for full inventory user=%d object=%d owned=%d capacity=%d",
					equipmentPickup.WinnerUserID, equipmentPickup.ObjectID,
					inventoryStatus.OwnedCount, inventoryStatus.Capacity,
				)
			}
			return r.rejectPickup(command, "inventory full")
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
		generation := currentSession.generation
		objectID := equipmentPickup.ObjectID
		step := campaignEquipmentPickupStep{
			runtime: r, ctx: context.WithoutCancel(ctx), sessionKey: sessionKey,
			generation: generation, userID: equipmentPickup.WinnerUserID,
			sourceTime: packet.SourceTime,
			pickup:     equipmentPickup, deletePacket: deletePacket,
			releasePacket: releasePacket, rejectPacket: rejectPacket,
			progression: r.progression,
		}
		producer := raknet.ScheduledPacketProducer{
			Delay:   campaignEquipmentPickupDelay,
			Produce: step.produce,
		}
		scheduleFailure := campaignEquipmentPickupFailure{
			runtime: r, sessionKey: sessionKey,
			generation: generation, objectID: objectID,
		}
		var equipmentCancel raknet.CancelSchedule
		var scheduleErr error
		if packet.ScheduleGroupResult != nil {
			equipmentCancel, scheduleErr = packet.ScheduleGroupResult(
				[]raknet.ScheduledPacketProducer{producer},
				scheduleFailure.handle,
			)
		} else {
			equipmentCancel, scheduleErr = packet.ScheduleGroup(
				[]raknet.ScheduledPacketProducer{producer},
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
		r.registry.sessions[sessionKey] = currentSession
		r.registry.mutex.Unlock()
		r.logger.Printf(
			"RakNet campaign equipment pickup accepted source=%d target=%d",
			command.Common.ObjectID, objectID,
		)
		responsePackets := append([][]byte{acceptPacket}, movementPackets...)
		return responsePackets, nil
	}
	crystalPickup := zoneinteract.CrystalPickup{}
	isCrystalPickup := false
	if isCurrentFound && currentSession.zone.PickupPayload() != nil {
		crystalPickup, isCrystalPickup =
			currentSession.zone.PickupPayload().Crystal(command.Value)
	}
	if isCurrentFound && isCrystalPickup {
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
			pickupPosition: crystalPickup.Object.Position,
			releasePacket:  releasePacket,
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
const campaignEquipmentPickupAnimation = "pickup_catalyst"

const campaignNPCEquipmentSourceAmount = 10
const campaignNPCOrbSourceAmount = 25
const campaignBossLimitedEditionChanceBasis = uint32(100)
const campaignBossLimitedEditionChanceThreshold = uint32(10)

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

type campaignLootProgression interface {
	GrantPartWithinCapacity(context.Context, int64, sporenet.Part) (sporenet.Part, error)
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

func (s *gameplayPeerSession) spawnCampaignEquipment(
	invocation game.CampaignScriptInvocation,
	gameplayJoin *game.GameplayJoin,
	sourceTime uint64,
	isBoss bool,
) ([][]byte, uint32, error) {
	packets, objectID, roll, err := s.spawnCampaignEquipmentWithPolicy(
		invocation, gameplayJoin, sourceTime, isBoss, false, "",
	)
	if err != nil {
		return nil, 0, fmt.Errorf("equipmentSpawn: %w", err)
	}
	if objectID != 0 && roll.Part.RigblockAssetID == 0 {
		return nil, 0, errors.New("campaign equipment roll incomplete")
	}
	return packets, objectID, nil
}

func (s *gameplayPeerSession) spawnCampaignEquipmentWithPolicy(
	invocation game.CampaignScriptInvocation,
	gameplayJoin *game.GameplayJoin,
	sourceTime uint64,
	isBoss bool,
	isForced bool,
	slotType string,
) ([][]byte, uint32, campaignEquipmentRoll, error) {
	roll := campaignEquipmentRoll{RequestedSlotType: slotType}
	if s == nil || gameplayJoin == nil || invocation.Challenge <= 0 {
		return nil, 0, roll, errors.New("campaign equipment unavailable")
	}
	if s.zone.DropRandom() == nil {
		return nil, 0, roll, errors.New("campaign drop random unavailable")
	}
	roll.ChanceScale = 1 + s.campaignPartAttribute(campaignLootFindAttribute)
	roll.ChanceDraw = s.zone.DropRandom().Float64()
	var err error
	roll.ChanceThreshold, err = sim.EquipmentDropThreshold(
		1, invocation.Challenge, 0.45, roll.ChanceScale,
	)
	if err != nil {
		return nil, 0, roll, fmt.Errorf("equipmentThreshold: %w", err)
	}
	roll.IsNaturalDrop, err = zoneloot.IsEquipmentDrop(
		invocation.Challenge, roll.ChanceScale, roll.ChanceDraw,
	)
	if err != nil {
		return nil, 0, roll, fmt.Errorf("equipmentDecision: %w", err)
	}
	if !roll.IsNaturalDrop && !isBoss && !isForced {
		return nil, 0, roll, nil
	}
	if s.deployedCreatureIndex >= uint32(len(s.binding.Creatures)) {
		return nil, 0, roll, errors.New("campaign equipment creature unavailable")
	}
	partSubjects := make([]game.GameplayCreature, 0, len(s.binding.ActivatedCreatures))
	for _, creature := range s.binding.ActivatedCreatures {
		if creature.Noun != 0 && creature.ClassType != "" && creature.ElementType != "" {
			partSubjects = append(partSubjects, creature)
		}
	}
	if len(partSubjects) == 0 {
		for _, creature := range s.binding.Creatures {
			if creature.Noun != 0 && creature.ClassType != "" && creature.ElementType != "" {
				partSubjects = append(partSubjects, creature)
			}
		}
	}
	if len(partSubjects) == 0 {
		return nil, 0, roll, errors.New("campaign equipment roster unavailable")
	}
	roll.PartChoice = s.zone.DropRandom().Uint32()
	roll.PartSubjectCount = len(partSubjects)
	roll.PartSubjectIndex = int(roll.PartChoice % uint32(len(partSubjects)))
	roll.PartSubject = partSubjects[roll.PartSubjectIndex]
	limitedEditionRigblockID := uint16(0)
	if isBoss && slotType == "" {
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
		roll.Part, err = gameplayJoin.GenerateCampaignSpecialPart(
			roll.PartSubject, s.binding.Difficulty, s.binding.AvatarLevel,
			roll.PartChoice, limitedEditionRigblockID,
		)
	} else if slotType != "" {
		roll.Part, err = gameplayJoin.GenerateCampaignPartForSlot(
			roll.PartSubject, s.binding.Difficulty, s.binding.AvatarLevel,
			roll.PartChoice, slotType,
		)
	} else {
		roll.Part, err = gameplayJoin.GenerateCampaignPart(
			roll.PartSubject, s.binding.Difficulty, s.binding.AvatarLevel, roll.PartChoice,
		)
	}
	if err != nil {
		return nil, 0, roll, fmt.Errorf("equipmentGenerate: %w", err)
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
		Source: source, Destination: destination,
		SimulationTime: time.Duration(sourceTime) * time.Millisecond,
	})
	if err != nil {
		return nil, 0, roll, fmt.Errorf("equipmentPlan: %w", err)
	}
	packets, err := lootraknet.MarshalEquipmentDrop(plan, roll.Part)
	if err != nil {
		return nil, 0, roll, fmt.Errorf("equipmentMarshal: %w", err)
	}
	err = s.registerCampaignPickup(
		zoneinteract.PickupEquipment, objectID, source, destination,
	)
	if err != nil {
		return nil, 0, roll, fmt.Errorf("equipmentRegister: %w", err)
	}
	err = s.zone.PickupPayload().AddEquipment(zoneinteract.EquipmentPickup{
		ObjectID: objectID, Part: roll.Part,
	})
	if err != nil {
		s.zone.Pickups().Remove(objectID)
		return nil, 0, roll, fmt.Errorf("equipmentTrack: %w", err)
	}
	return packets, objectID, roll, nil
}

func (s *gameplayPeerSession) spawnCampaignNPCEquipment(
	enemy zonenpc.Snapshot,
	gameplayJoin *game.GameplayJoin,
	sourceTime uint64,
) ([][]byte, uint32, error) {
	if s == nil || !enemy.IsDefeated || enemy.Plan.ObjectID == 0 {
		return nil, 0, errors.New("campaign enemy equipment unavailable")
	}
	reservation, isReserved := s.reserveCampaignNPCDrop(
		enemy.Plan.ObjectID, zoneloot.NPCDropEquipment,
	)
	if !isReserved {
		return nil, 0, nil
	}
	// A map boss awards equipment once through the shared NPC reservation;
	// ordinary enemies and interactables retain their normal chance roll.
	packets, objectID, err := s.spawnCampaignEquipment(game.CampaignScriptInvocation{
		Position: enemy.Plan.Position, Challenge: campaignNPCEquipmentSourceAmount,
	}, gameplayJoin, sourceTime, enemy.Plan.IsBoss)
	if err != nil {
		reservation.Release()
		return nil, 0, fmt.Errorf("enemyEquipmentSpawn: %w", err)
	}
	err = reservation.Commit()
	if err != nil {
		return nil, 0, fmt.Errorf("enemyEquipmentCommit: %w", err)
	}
	return packets, objectID, nil
}

func (s *gameplayPeerSession) reserveCampaignNPCDrop(
	objectID uint32, kind zoneloot.NPCDropKind,
) (*zoneloot.Reservation, bool) {
	if s == nil {
		return nil, false
	}
	if s.zone.Loot() == nil {
		return nil, false
	}
	return s.zone.Loot().ReserveNPCDrop(objectID, kind)
}

type campaignEquipmentPickupStep struct {
	runtime       campaignInteractionRuntime
	ctx           context.Context
	sessionKey    string
	generation    uint64
	userID        uint64
	sourceTime    uint64
	pickup        zoneinteract.EquipmentPickup
	deletePacket  []byte
	releasePacket []byte
	rejectPacket  []byte
	progression   campaignLootProgression
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

func (s campaignEquipmentPickupStep) produce() ([][]byte, error) {
	s.runtime.registry.mutex.RLock()
	sourceSession, isFound := s.runtime.registry.sessions[s.sessionKey]
	isCurrent := isFound && sourceSession.generation == s.generation
	s.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	grantedPart, err := s.progression.GrantPartWithinCapacity(
		s.ctx, int64(s.userID), s.pickup.Part,
	)
	s.runtime.registry.mutex.Lock()
	peerSession, isFound := s.runtime.registry.sessions[s.sessionKey]
	isCurrent = isFound && peerSession.generation == s.generation
	if isCurrent {
		peerSession.campaignScheduleSession().Remove(
			zoneaction.ScheduleEquipment, s.pickup.ObjectID, nil,
		)
		s.runtime.registry.sessions[s.sessionKey] = peerSession
	}
	if err != nil {
		if errors.Is(err, sporenet.ErrInventoryFull) {
			clearErr := sourceSession.zone.PickupPayload().ClearEquipmentRoll(
				s.pickup.ObjectID, s.userID,
			)
			if clearErr != nil && s.runtime.logger != nil {
				s.runtime.logger.Printf(
					"RakNet campaign equipment delayed full winner reset failed user=%d object=%d: %v",
					s.userID, s.pickup.ObjectID, clearErr,
				)
			}
		}
		sourceSession.zone.Pickups().Release(s.pickup.ObjectID)
		s.runtime.registry.mutex.Unlock()
		if errors.Is(err, sporenet.ErrInventoryFull) {
			s.runtime.logger.Printf(
				"RakNet campaign equipment retained for full inventory user=%d object=%d",
				s.userID, s.pickup.ObjectID,
			)
			if isCurrent {
				return [][]byte{s.rejectPacket}, nil
			}
			return nil, nil
		}
		return nil, fmt.Errorf("campaignEquipmentGrant: %w", err)
	}
	if !sourceSession.zone.Pickups().Commit(s.pickup.ObjectID) {
		s.runtime.registry.mutex.Unlock()
		return nil, errors.New("campaign equipment pickup commit missing")
	}
	sourceSession.zone.PickupPayload().RemoveEquipment(s.pickup.ObjectID)
	winnerSessionKey := ""
	winnerSession := gameplayPeerSession{}
	for candidateSessionKey, candidate := range s.runtime.registry.sessions {
		if candidate.zone != sourceSession.zone ||
			candidate.binding.UserID != s.userID {
			continue
		}
		winnerSessionKey = candidateSessionKey
		winnerSession = candidate
		break
	}
	awardPacket := []byte(nil)
	if winnerSessionKey != "" {
		awardPacket, err = lootraknet.MarshalEquipmentAward(
			grantedPart, winnerSession.deployedObjectID,
			winnerSession.playerPosition,
		)
		if err != nil {
			s.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignEquipmentAward: %w", err)
		}
	}
	pickupAnimationPacket := []byte(nil)
	isRemoteWinner := winnerSessionKey != "" && winnerSessionKey != s.sessionKey
	if isRemoteWinner {
		pickupAnimationPacket, err = abilityraknet.Animation(
			winnerSession.deployedObjectID,
			campaignEquipmentPickupAnimation,
			s.sourceTime,
		)
		if err != nil {
			s.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignEquipmentAnimation: %w", err)
		}
	}
	rollPackets := make([][]byte, 0, len(s.pickup.Rolls))
	if len(s.pickup.Rolls) > 1 {
		for index, roll := range s.pickup.Rolls {
			rollPacket, rollErr := raknet.MarshalApplication(raknet.LootRollMessage{
				ObjectID: roll.ObjectID, Roll: roll.Roll,
			})
			if rollErr != nil {
				s.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf(
					"campaignEquipmentRollMarshal[%d]: %w", index, rollErr,
				)
			}
			rollPackets = append(rollPackets, rollPacket)
		}
	}
	responsePackets := make([][]byte, 0, 3)
	for candidateSessionKey, candidate := range s.runtime.registry.sessions {
		if candidate.zone != sourceSession.zone {
			continue
		}
		packets := append([][]byte(nil), rollPackets...)
		if pickupAnimationPacket != nil {
			packets = append(packets, pickupAnimationPacket)
		}
		if candidateSessionKey == winnerSessionKey {
			packets = append(packets, awardPacket)
		}
		packets = append(packets, s.deletePacket)
		if candidateSessionKey == s.sessionKey && isCurrent {
			responsePackets = append(responsePackets, packets...)
			continue
		}
		publishErr := candidate.publishPackets(packets)
		if publishErr != nil && s.runtime.logger != nil {
			s.runtime.logger.Printf(
				"RakNet campaign equipment pickup peer delivery queued game=%d user=%d object=%d: %v",
				candidate.binding.GameID, candidate.binding.UserID,
				s.pickup.ObjectID, publishErr,
			)
		}
		s.runtime.registry.sessions[candidateSessionKey] = candidate
	}
	if isCurrent {
		responsePackets = append(responsePackets, s.releasePacket)
	}
	s.runtime.registry.mutex.Unlock()
	sourceSession.zone.SaveCheckpointIfSafe(zonecheckpoint.ReasonSafePickup)
	if winnerSessionKey == "" && s.runtime.logger != nil {
		s.runtime.logger.Printf(
			"RakNet campaign equipment granted without connected winner presentation user=%d object=%d",
			s.userID, s.pickup.ObjectID,
		)
	}
	return responsePackets, nil
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
	if s == nil || !enemy.IsDefeated || enemy.Plan.ObjectID == 0 {
		return nil, 0, errors.New("campaign enemy DNA unavailable")
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
	if s.binding.Difficulty < game.MinimumCampaignDifficulty {
		err := reservation.Commit()
		if err != nil {
			return nil, 0, fmt.Errorf("dnaDifficultyCommit: %w", err)
		}
		return nil, 0, nil
	}
	draw, err := s.zone.DropRandom().Index(zoneloot.DNAChanceBasis)
	if err != nil {
		reservation.Release()
		return nil, 0, fmt.Errorf("dnaChance: %w", err)
	}
	isDrop, err := zoneloot.IsNPCDNADrop(draw)
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
	amount, err := zoneloot.NPCDNAAmount(
		s.binding.Difficulty, s.campaignPartAttribute(campaignDNADroppedAttribute), amountDraw,
	)
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
	packets, err := lootraknet.MarshalDNADrop(plan)
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

func (s *gameplayPeerSession) collectCampaignDNA(
	ctx context.Context, progression zoneloot.DNAGranter,
	registry *gameplaySessionRegistry,
	start raknet.Vector3, end raknet.Vector3, now time.Time,
) ([][]byte, error) {
	if s == nil || progression == nil || s.zone == nil || s.zone.DNA() == nil {
		return nil, nil
	}
	dnaReservation, isReserved := s.zone.DNA().ReserveContact(
		game.Vec3{X: start.X, Y: start.Y, Z: start.Z},
		game.Vec3{X: end.X, Y: end.Y, Z: end.Z},
		now, campaignDNAPickupRadius,
	)
	if !isReserved {
		return nil, nil
	}
	pickup := dnaReservation.Pickup()
	if pickup.Amount > ^uint32(0)-s.binding.DNA {
		dnaReservation.Release()
		return nil, errors.New("DNA overflow")
	}
	grantedDNA, err := progression.GrantDNA(
		ctx, int64(s.binding.UserID), pickup.Amount,
	)
	if err != nil {
		dnaReservation.Release()
		return nil, fmt.Errorf("dnaGrant: %w", err)
	}
	s.binding.DNA = grantedDNA
	packets, err := lootraknet.MarshalDNACollection(lootraknet.DNACollectionRequest{
		Slot: uint8(s.binding.Slot), DNA: grantedDNA, Amount: pickup.Amount,
		ActorObjectID: s.deployedObjectID, PickupObjectID: pickup.ObjectID,
		Position: raknet.Vector3{
			X: pickup.Position.X,
			Y: pickup.Position.Y,
			Z: pickup.Position.Z,
		},
	})
	if err != nil {
		if !dnaReservation.Commit() {
			return nil, errors.Join(
				fmt.Errorf("dnaCollectionMarshal: %w", err),
				errors.New("DNA reservation commit missing"),
			)
		}
		return nil, fmt.Errorf("dnaCollectionMarshal: %w", err)
	}
	if registry != nil {
		type allyDNAUpdate struct {
			sessionKey string
			packet     []byte
		}
		allyUpdates := make([]allyDNAUpdate, 0, len(registry.sessions))
		sessionKeys := make([]string, 0, len(registry.sessions))
		for sessionKey, candidate := range registry.sessions {
			if !isActiveCoopPickupAlly(candidate, *s) {
				continue
			}
			sessionKeys = append(sessionKeys, sessionKey)
		}
		sort.Strings(sessionKeys)
		allyUserIDs := make(map[uint64]struct{}, len(sessionKeys))
		for _, sessionKey := range sessionKeys {
			candidate := registry.sessions[sessionKey]
			if _, isFound := allyUserIDs[candidate.binding.UserID]; isFound {
				continue
			}
			allyUserIDs[candidate.binding.UserID] = struct{}{}
			if pickup.Amount > ^uint32(0)-candidate.binding.DNA {
				if registry.logger != nil {
					registry.logger.Printf(
						"RakNet co-op DNA grant skipped for overflow game=%d user=%d",
						candidate.binding.GameID, candidate.binding.UserID,
					)
				}
				continue
			}
			allyDNA, grantErr := progression.GrantDNA(
				ctx, int64(candidate.binding.UserID), pickup.Amount,
			)
			if grantErr != nil {
				if registry.logger != nil {
					registry.logger.Printf(
						"RakNet co-op DNA grant failed game=%d user=%d: %v",
						candidate.binding.GameID, candidate.binding.UserID, grantErr,
					)
				}
				continue
			}
			candidate.binding.DNA = allyDNA
			updatePacket, marshalErr := raknet.MarshalApplication(
				raknet.LabsPlayerDNAUpdateMessage{
					Slot: uint8(candidate.binding.Slot), DNA: allyDNA,
				},
			)
			if marshalErr != nil {
				registry.sessions[sessionKey] = candidate
				if registry.logger != nil {
					registry.logger.Printf(
						"RakNet co-op DNA update marshal failed game=%d user=%d: %v",
						candidate.binding.GameID, candidate.binding.UserID, marshalErr,
					)
				}
				continue
			}
			registry.sessions[sessionKey] = candidate
			allyUpdates = append(allyUpdates, allyDNAUpdate{
				sessionKey: sessionKey, packet: updatePacket,
			})
		}
		if !dnaReservation.Commit() {
			return nil, errors.New("DNA reservation commit missing")
		}
		for _, update := range allyUpdates {
			candidate := registry.sessions[update.sessionKey]
			publishErr := candidate.publishPackets([][]byte{update.packet})
			if publishErr != nil && registry.logger != nil {
				registry.logger.Printf(
					"RakNet co-op DNA update queued game=%d user=%d: %v",
					candidate.binding.GameID, candidate.binding.UserID, publishErr,
				)
			}
			registry.sessions[update.sessionKey] = candidate
		}
		return packets, nil
	}
	if !dnaReservation.Commit() {
		return nil, errors.New("DNA reservation commit missing")
	}
	return packets, nil
}

const campaignOrbLifetime = 30 * time.Second
const campaignOrbPickupRadius = float32(2.5)
const campaignOrbLobDuration = 500 * time.Millisecond

func (s *gameplayPeerSession) spawnCampaignHealthOrb(
	invocation game.CampaignScriptInvocation, sourceTime uint64, now time.Time,
) ([][]byte, uint32, error) {
	if s == nil || s.squad == nil || invocation.Challenge <= 0 {
		return nil, 0, errors.New("campaign orb unavailable")
	}
	if s.zone.DropRandom() == nil {
		return nil, 0, errors.New("campaign drop random unavailable")
	}
	if s.zone.Orbs() == nil {
		return nil, 0, errors.New("campaign orb registry unavailable")
	}
	roster := make([]sim.OrbResourceSample, 0, squad.Size)
	isResurrectionEnabled := !strings.EqualFold(
		s.binding.Level, game.InitialChainLevel,
	) && invocation.CallbackName != "InteractHealthObelisk"
	isDeadSquadMemberFound := false
	for index, creature := range s.binding.Creatures {
		character, isFound := s.squad.Character(uint32(index))
		if !isFound || !character.IsAvailable || creature.Noun == 0 {
			continue
		}
		maximumHitPoint, maximumMana := s.characterResourceMaximum(uint32(index))
		roster = append(roster, sim.OrbResourceSample{
			HitPoint:        min(character.HitPoints, maximumHitPoint),
			MaximumHitPoint: maximumHitPoint,
			Mana:            min(character.ManaPoints, maximumMana),
			MaximumMana:     maximumMana,
		})
		if character.HitPoints <= 0 {
			isDeadSquadMemberFound = true
		}
	}
	isResurrectionEnabled = isResurrectionEnabled && isDeadSquadMemberFound
	if len(roster) == 0 {
		return nil, 0, errors.New("campaign orb roster unavailable")
	}
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
	destination := s.reachableCampaignDropDestination(source)
	objectID, err := s.reserveCampaignObjectID()
	if err != nil {
		return nil, 0, fmt.Errorf("orbObjectID: %w", err)
	}
	pickup, isDrop, err := zoneloot.PlanOrb(sim.OrbDropInput{
		ScaledBudget: uint32(invocation.Challenge),
		SourceRole:   zoneinteract.Role, SourcePosition: source,
		SimulationTime: time.Duration(sourceTime) * time.Millisecond,
		PickupLifetime: campaignOrbLifetime, Roster: roster,
		Destinations: []sim.Position{destination},
		PickupRoles: []sim.Role{
			sim.Role(fmt.Sprintf("campaignOrb.%d", objectID)),
		},
		Random: s.zone.DropRandom(), IsResurrectionEnabled: isResurrectionEnabled,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("orbPlan: %w", err)
	}
	if !isDrop {
		return nil, 0, nil
	}
	components, err := lootraknet.MarshalOrbDrop(pickup, objectID)
	if err != nil {
		return nil, 0, fmt.Errorf("orbMarshal: %w", err)
	}
	err = s.zone.Orbs().Add(zoneinteract.Orb{
		ObjectID: objectID, Request: pickup,
		AvailableAt: now.Add(campaignOrbLobDuration),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("orbTrack: %w", err)
	}
	err = s.registerCampaignPickup(
		zoneinteract.PickupOrb, objectID, pickup.Position,
		pickup.Destination,
	)
	if err != nil {
		s.zone.Orbs().Remove(objectID)
		return nil, 0, fmt.Errorf("orbRegister: %w", err)
	}
	return [][]byte{
		components.CreatePacket,
		components.PresentationPacket,
		components.LocomotionPacket,
	}, objectID, nil
}

func (s *gameplayPeerSession) spawnCampaignNPCOrb(
	enemy zonenpc.Snapshot, sourceTime uint64, now time.Time,
) ([][]byte, uint32, error) {
	if s == nil || !enemy.IsDefeated || enemy.Plan.ObjectID == 0 {
		return nil, 0, errors.New("campaign enemy orb unavailable")
	}
	reservation, isReserved := s.reserveCampaignNPCDrop(
		enemy.Plan.ObjectID, zoneloot.NPCDropOrb,
	)
	if !isReserved {
		return nil, 0, nil
	}
	packets, objectID, err := s.spawnCampaignHealthOrb(
		game.CampaignScriptInvocation{
			Position:  enemy.Plan.Position,
			Challenge: campaignNPCOrbSourceAmount,
		},
		sourceTime, now,
	)
	if err != nil {
		reservation.Release()
		return nil, 0, fmt.Errorf("enemyOrbSpawn: %w", err)
	}
	err = reservation.Commit()
	if err != nil {
		return nil, 0, fmt.Errorf("enemyOrbCommit: %w", err)
	}
	return packets, objectID, nil
}

func (s *gameplayPeerSession) collectCampaignOrbs(
	registry *gameplaySessionRegistry,
	start raknet.Vector3, end raknet.Vector3, now time.Time,
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
	packets := make([][]byte, 0, len(pickups)*3)
	for _, pickup := range pickups {
		registeredPickup, admission := s.reserveCampaignPickupContact(
			pickup.ObjectID, start, end, campaignOrbPickupRadius,
		)
		if admission != zoneinteract.PickupAccepted {
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
		resurrections := make([]zoneSquadResurrection, 0)
		restoreFraction := campaignOrbRestoreFraction *
			(1 + s.campaignPartAttribute(campaignOrbEffectAttribute))
		if pickup.Request.Kind == sim.ManaOrbDrop {
			kind = zoneManaOrb
		}
		if pickup.Request.Kind == sim.ResurrectionOrbDrop {
			kind = zoneResurrectionOrb
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
		if kind == zoneResurrectionOrb {
			var err error
			resurrections, err = s.resurrectDeadZoneSquad()
			if err != nil {
				s.zone.Pickups().Release(pickup.ObjectID)
				return nil, fmt.Errorf("orbResurrection: %w", err)
			}
			isFull = len(resurrections) == 0
		}
		if registry != nil && (kind == zoneHealthOrb || kind == zoneManaOrb) {
			var allyPackets [][]byte
			var allyErr error
			alliedRestorations, allyPackets, alliedWorldPackets, allyErr =
				registry.restoreAlliedZoneSquadsLocked(*s, kind, restoreFraction)
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
			s.rollbackZoneSquadResurrection(resurrections)
			registry.rollbackAlliedZoneSquadRestorationsLocked(alliedRestorations)
			s.zone.Pickups().Release(pickup.ObjectID)
			return nil, fmt.Errorf("orbPickupMarshal: %w", err)
		}
		if isFull {
			isFirstNotice := s.markCampaignOrbFullContact(pickup.ObjectID)
			s.zone.Pickups().Release(pickup.ObjectID)
			if isFirstNotice {
				packets = append(packets, encoded...)
			}
			continue
		}
		s.clearCampaignOrbFullContact(pickup.ObjectID)
		for _, resurrection := range resurrections {
			encoded = append(encoded, resurrection.packet)
		}
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
			s.rollbackZoneSquadResurrection(resurrections)
			registry.rollbackAlliedZoneSquadRestorationsLocked(alliedRestorations)
			return nil, errors.New("campaign orb commit missing")
		}
		s.zone.Orbs().Remove(pickup.ObjectID)
		if registry != nil {
			registry.publishAlliedPickupResourcesLocked(*s, partyResourcePackets)
		}
		packets = append(packets, encoded...)
	}
	s.clearExitedCampaignOrbFullContacts(endPosition)
	return packets, nil
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

type zoneSquadResurrection struct {
	creatureIndex uint32
	hitPoint      float32
	packet        []byte
}

func (s *gameplayPeerSession) resurrectDeadZoneSquad() (
	[]zoneSquadResurrection, error,
) {
	if s == nil || s.squad == nil {
		return nil, errors.New("campaign resurrection squad unavailable")
	}
	resurrections := make([]zoneSquadResurrection, 0, squad.Size-1)
	for index := uint32(0); index < squad.Size; index++ {
		character, isFound := s.squad.Character(index)
		if !isFound || !character.IsAvailable || character.HitPoints > 0 {
			continue
		}
		maximumHitPoint := s.characterHitPointMaximum(index)
		if maximumHitPoint <= 0 {
			return nil, fmt.Errorf("resurrectionMaximum[%d]: invalid", index)
		}
		packet, err := s.marshalCampaignCharacterResourceValues(
			index, maximumHitPoint, character.ManaPoints,
		)
		if err != nil {
			return nil, fmt.Errorf("resurrectionMarshal[%d]: %w", index, err)
		}
		resurrections = append(resurrections, zoneSquadResurrection{
			creatureIndex: index, hitPoint: character.HitPoints, packet: packet,
		})
	}
	for _, resurrection := range resurrections {
		maximumHitPoint := s.characterHitPointMaximum(resurrection.creatureIndex)
		_, err := s.squad.SetHitPoints(resurrection.creatureIndex, maximumHitPoint)
		if err != nil {
			s.rollbackZoneSquadResurrection(resurrections)
			return nil, fmt.Errorf(
				"resurrectionHealth[%d]: %w", resurrection.creatureIndex, err,
			)
		}
	}
	if len(resurrections) == 0 {
		return nil, nil
	}
	err := s.syncZoneSquadCheckpoint()
	if err != nil {
		s.rollbackZoneSquadResurrection(resurrections)
		return nil, fmt.Errorf("resurrectionCheckpoint: %w", err)
	}
	return resurrections, nil
}

func (s *gameplayPeerSession) rollbackZoneSquadResurrection(
	resurrections []zoneSquadResurrection,
) {
	if s == nil || s.squad == nil {
		return
	}
	for _, resurrection := range resurrections {
		_, _ = s.squad.SetHitPoints(
			resurrection.creatureIndex, resurrection.hitPoint,
		)
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

func (s *gameplayPeerSession) expireCampaignOrb(
	objectID uint32,
) ([]byte, error) {
	if s == nil || s.zone.Orbs() == nil ||
		!s.zone.Orbs().Remove(objectID) {
		return nil, nil
	}
	if s.zone.Pickups() != nil {
		s.zone.Pickups().Remove(objectID)
	}
	packet, err := interactraknet.DeletePickup(objectID)
	if err != nil {
		return nil, fmt.Errorf("orbExpireMarshal: %w", err)
	}
	return packet, nil
}

// A source amount of 26 produces an exact four-percent attempt for the
// integer [0,100) draw through the recovered source*0.15 threshold.
const campaignNPCCrystalSourceAmount = int32(26)

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
	planLayer, isLayerFound := campaignNav.SelectLayer(
		campaignSecurityBlitzFootprintFallback, zonenavigation.HeroHeight,
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
) ([][]byte, uint32, error) {
	if s == nil || invocation.Challenge <= 0 {
		return nil, 0, errors.New("campaign crystal unavailable")
	}
	// Focused transport fixtures may omit the content-owned weighted catalog.
	// Production program loading validates the complete 192-row/offset set.
	if len(definitions) == 0 || len(offsets) == 0 {
		return nil, 0, nil
	}
	if s.zone.DropRandom() == nil {
		return nil, 0, errors.New("campaign drop random unavailable")
	}
	draw, err := s.zone.DropRandom().Index(100)
	if err != nil {
		return nil, 0, fmt.Errorf("crystalChance: %w", err)
	}
	source := sim.Position{
		X: invocation.Position.X,
		Y: invocation.Position.Y,
		Z: invocation.Position.Z,
	}
	destination := s.reachableCampaignDropDestination(source)
	pickup, isDrop, err := zoneloot.PlanCrystal(
		zoneloot.CrystalPlanInput{
			Challenge:   invocation.Challenge * int32(max(uint16(1), s.binding.ParticipantCount)),
			ChanceScale: 1 + s.campaignPartAttribute(campaignCrystalFindAttribute),
			RandomDraw:  draw,
			World: sim.CrystalDropInput{
				CurrentDifficulty: s.binding.Difficulty,
				PlayerRole:        zoneinteract.PlayerRole,
				SourceRole:        zoneinteract.Role,
				ControlledRole:    "playerAgent", PlayerCount: 1,
				SimulationTime: time.Duration(sourceTime) * time.Millisecond,
				SourcePosition: source,
				Destinations:   []sim.Position{destination},
				PickupRoles:    []sim.Role{"crystal"},
				Definitions:    definitions, LevelOffsets: offsets,
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
	packets, err := lootraknet.MarshalCrystalDrop(pickup, objectID)
	if err != nil {
		return nil, 0, fmt.Errorf("crystalMarshal: %w", err)
	}
	err = s.registerCampaignPickup(
		zoneinteract.PickupCrystal, objectID, pickup.Position,
		pickup.Destination,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("crystalRegister: %w", err)
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
	if len(batch.Packets) != 1 || len(batch.CrystalDrops) != 1 ||
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
	if len(request.Pickups) != 1 {
		return nil, fmt.Errorf("catalystCount: %d", len(request.Pickups))
	}
	objectID, err := s.reserveCampaignObjectID()
	if err != nil {
		return nil, fmt.Errorf("catalystObjectID: %w", err)
	}
	pickup := request.Pickups[0]
	worldPackets, err := lootraknet.MarshalCrystalDrop(pickup, objectID)
	if err != nil {
		return nil, fmt.Errorf("catalystMarshal: %w", err)
	}
	err = s.registerCampaignPickup(
		zoneinteract.PickupCrystal, objectID, pickup.Position,
		pickup.Destination,
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
	packets := append([][]byte(nil), batch.Packets...)
	return append(packets, worldPackets...), nil
}

func (s *gameplayPeerSession) spawnCampaignNPCCrystal(
	enemy zonenpc.Snapshot,
	definitions []sim.CrystalDefinition,
	offsets []sim.CrystalLevelOffset,
	sourceTime uint64,
) ([][]byte, uint32, error) {
	if s == nil || !enemy.IsDefeated || enemy.Plan.ObjectID == 0 {
		return nil, 0, errors.New("campaign enemy crystal unavailable")
	}
	reservation, isReserved := s.reserveCampaignNPCDrop(
		enemy.Plan.ObjectID, zoneloot.NPCDropCrystal,
	)
	if !isReserved {
		return nil, 0, nil
	}
	packets, objectID, err := s.spawnCampaignCrystal(
		game.CampaignScriptInvocation{
			Position:  enemy.Plan.Position,
			Challenge: campaignNPCCrystalSourceAmount,
		},
		definitions, offsets, sourceTime,
	)
	if err != nil {
		reservation.Release()
		return nil, 0, fmt.Errorf("enemyCrystalSpawn: %w", err)
	}
	err = reservation.Commit()
	if err != nil {
		return nil, 0, fmt.Errorf("enemyCrystalCommit: %w", err)
	}
	return packets, objectID, nil
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

type campaignOrbExpiryStep struct {
	runtime    campaignInteractionRuntime
	sessionKey string
	generation uint64
	objectID   uint32
}

func (s campaignOrbExpiryStep) produce() ([][]byte, error) {
	s.runtime.registry.mutex.Lock()
	peerSession, isFound := s.runtime.registry.sessions[s.sessionKey]
	if !isFound || peerSession.generation != s.generation {
		s.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	packet, err := peerSession.expireCampaignOrb(s.objectID)
	if err == nil {
		s.runtime.registry.sessions[s.sessionKey] = peerSession
	}
	s.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("campaignOrbExpire: %w", err)
	}
	if packet == nil {
		return nil, nil
	}
	return [][]byte{packet}, nil
}

type campaignInteractableDropStep struct {
	runtime    campaignInteractionRuntime
	adapter    *campaignInteractablePacketAdapter
	sessionKey string
	generation uint64
	use        game.CampaignScriptUse
	sourceTime uint64
	deadline   time.Duration
	schedule   schedulePacketFunc
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
	orbObjectIDs := []uint32(nil)
	dropSourceTime := s.sourceTime + uint64(s.deadline/time.Millisecond)
	switch s.use.Invocation.CallbackName {
	case "InteractHealthObelisk":
		dropPacket, orbObjectIDs, err = peerSession.spawnHealthObeliskCapsules(
			s.use.Invocation, dropSourceTime, s.runtime.now(),
		)
	case "InteractWithObelisk":
		var equipmentPacket [][]byte
		equipmentPacket, _, err = peerSession.spawnCampaignEquipment(
			s.use.Invocation, s.runtime.gameplayJoin, dropSourceTime, false,
		)
		if err != nil {
			s.runtime.logger.Printf(
				"RakNet campaign equipment not generated for %s: %v",
				s.sessionKey, err,
			)
		}
		var crystalPacket [][]byte
		var crystalObjectID uint32
		var crystalErr error
		crystalPacket, crystalObjectID, crystalErr = peerSession.spawnCampaignCrystal(
			s.use.Invocation, s.runtime.crystalDefinitions,
			s.runtime.crystalLevelOffsets, dropSourceTime,
		)
		dropPacket = append(dropPacket, equipmentPacket...)
		if crystalObjectID != 0 {
			dropPacket = append(dropPacket, crystalPacket...)
		}
		err = crystalErr
	}
	if err == nil {
		s.runtime.registry.sessions[s.sessionKey] = peerSession
	}
	s.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("campaignInteractableOrb: %w", err)
	}
	for _, orbObjectID := range orbObjectIDs {
		if s.schedule == nil {
			break
		}
		expiryStep := campaignOrbExpiryStep{
			runtime: s.runtime, sessionKey: s.sessionKey,
			generation: s.generation, objectID: orbObjectID,
		}
		err = s.schedule(campaignOrbLifetime, expiryStep.produce)
		if err != nil {
			s.runtime.logger.Printf(
				"RakNet campaign orb expiry not scheduled object=%d: %v",
				orbObjectID, err,
			)
		}
	}
	return append(packet, dropPacket...), nil
}

type campaignInteractableFinalStep struct {
	runtime    campaignInteractionRuntime
	adapter    *campaignInteractablePacketAdapter
	sessionKey string
	generation uint64
	objectID   uint32
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
	if err == nil {
		packet = append(packet, s.release)
		s.runtime.releaseSchedule(
			s.sessionKey, s.generation, s.objectID, s.run,
		)
	}
	return packet, err
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
	if isCurrentFound && currentSession.zone.Script() != nil {
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
	if rejection == "" || rejection == game.CampaignScriptUseRejectedDistance {
		navigationErr := zonenavigation.ValidateInteraction(
			currentSession.zone.Navigation(),
			game.Vec3{
				X: currentSession.playerPosition.X,
				Y: currentSession.playerPosition.Y,
				Z: currentSession.playerPosition.Z,
			},
			use.Invocation.Position,
			currentSession.deployedCampaignFootprintRadius(),
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
			r.registry.mutex.Unlock()
			if marshalErr != nil {
				return nil, fmt.Errorf("campaignInteractablePursuit: %w", marshalErr)
			}
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
		deadline: deadline[1], schedule: packet.ScheduleFunc,
	}
	finalStep := campaignInteractableFinalStep{
		runtime: r, adapter: packetAdapter, sessionKey: sessionKey,
		generation: generation, objectID: use.Invocation.TargetObjectID,
		run: interactableRun, deadline: deadline[2], release: releasePacket,
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
	return response, nil
}
