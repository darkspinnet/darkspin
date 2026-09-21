package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/squad"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	barrierraknet "github.com/darkspinnet/darkspin/server/zone/barrier/raknet103"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	zonehero "github.com/darkspinnet/darkspin/server/zone/hero"
	heroraknet "github.com/darkspinnet/darkspin/server/zone/hero/raknet103"
	zoneinteract "github.com/darkspinnet/darkspin/server/zone/interact"
	zoneloot "github.com/darkspinnet/darkspin/server/zone/loot"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
	objectraknet "github.com/darkspinnet/darkspin/server/zone/object/raknet103"
	securityraknet "github.com/darkspinnet/darkspin/server/zone/security/raknet103"
)

const rejoinSnapshotAttemptLimit = 8

var errRejoinSnapshotActive = errors.New("rejoin snapshot remained active")

func (r gameplaySetupRuntime) publishRejoin(
	packet raknet.Packet, peerSession gameplayPeerSession,
) ([][]byte, error) {
	if !peerSession.arePassiveModifiersAllocated {
		passiveModifierInstance, err := zoneability.AllocatePassiveModifiers(
			r.modifierPool, peerSession.binding.Creatures,
		)
		if err != nil {
			return nil, fmt.Errorf("rejoinPassive: %w", err)
		}
		peerSession.passiveModifierInstance = passiveModifierInstance
		peerSession.arePassiveModifiersAllocated = true
		r.registry.mutex.Lock()
		current, isFound := r.registry.sessions[packet.Address.String()]
		if !isFound ||
			current.transportGeneration != peerSession.transportGeneration {
			r.registry.mutex.Unlock()
			for _, instanceID := range passiveModifierInstance {
				if instanceID != 0 && r.modifierPool != nil {
					_ = r.modifierPool.Release(instanceID)
				}
			}
			return nil, errors.New("rejoin passive transport replaced")
		}
		current.passiveModifierInstance = passiveModifierInstance
		current.arePassiveModifiersAllocated = true
		r.registry.sessions[packet.Address.String()] = current
		r.registry.mutex.Unlock()
	}
	baseline, revision, err := marshalGameplayRejoinBaseline(
		peerSession, packet.SourceTime,
	)
	if err != nil {
		return nil, fmt.Errorf("rejoinBaseline: %w", err)
	}
	r.registry.mutex.Lock()
	current, isFound := r.registry.sessions[packet.Address.String()]
	isCurrent := isFound &&
		current.transportGeneration == peerSession.transportGeneration &&
		current.isRejoinPending
	r.registry.mutex.Unlock()
	if !isCurrent {
		return nil, errors.New("rejoin transport replaced")
	}
	err = packet.AfterResponseCommit(func() {
		r.commitRejoin(
			packet.Address.String(), peerSession.transportGeneration,
			revision,
		)
		r.scheduleCampaignClock(packet, peerSession)
	})
	if err != nil {
		return nil, fmt.Errorf("rejoinCommit: %w", err)
	}
	r.logger.Printf(
		"RakNet gameplay baseline rejoined game=%d user=%d remote=%s packets=%d",
		peerSession.binding.GameID, peerSession.binding.UserID,
		packet.Address, len(baseline),
	)
	return baseline, nil
}

func (r gameplaySetupRuntime) commitRejoin(
	sessionKey string, transportGeneration uint64, revision uint64,
) {
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound &&
		peerSession.transportGeneration == transportGeneration &&
		peerSession.isRejoinPending
	r.registry.mutex.RUnlock()
	if !isCurrent || peerSession.zone == nil {
		return
	}
	memberKey := gameplaySessionMemberKey(peerSession)
	memberMutex := r.registry.memberMutex(memberKey)
	if !memberMutex.TryLock() {
		if r.logger != nil {
			r.logger.Printf(
				"RakNet gameplay rejoin commit deferred game=%d user=%d remote=%s: member busy",
				peerSession.binding.GameID, peerSession.binding.UserID, sessionKey,
			)
		}
		return
	}
	defer memberMutex.Unlock()
	r.registry.mutex.Lock()
	peerSession, isFound = r.registry.sessions[sessionKey]
	isCurrent = isFound &&
		peerSession.transportGeneration == transportGeneration &&
		peerSession.isRejoinPending
	r.registry.mutex.Unlock()
	if !isCurrent || peerSession.zone == nil {
		return
	}
	err := peerSession.zone.CommitReconnect(
		peerSession.binding.UserID, peerSession.generation, revision,
	)
	if err != nil {
		if r.logger != nil {
			r.logger.Printf(
				"RakNet gameplay rejoin commit failed game=%d user=%d remote=%s: %v",
				peerSession.binding.GameID, peerSession.binding.UserID,
				sessionKey, err,
			)
		}
		return
	}
	r.registry.mutex.Lock()
	current, isFound := r.registry.sessions[sessionKey]
	isCurrent = isFound &&
		current.transportGeneration == transportGeneration &&
		current.isRejoinPending
	if isCurrent {
		current.isRejoinPending = false
		current.isNPCRecoveryPending = true
		r.registry.sessions[sessionKey] = current
	}
	r.registry.mutex.Unlock()
	if !isCurrent {
		_, _ = peerSession.zone.Disconnect(
			peerSession.binding.UserID, peerSession.generation,
		)
		return
	}
	companionPackets, companionErr := marshalGameplayCompanions(peerSession, peerSession.binding.UserID)
	if companionErr != nil {
		if r.logger != nil {
			r.logger.Printf("RakNet reconnect companion publication failed user=%d: %v",
				peerSession.binding.UserID, companionErr)
		}
		return
	}
	r.registry.queuePeerPresentation(
		gameplayProducerIdentityFromSession(sessionKey, peerSession, true), companionPackets,
	)
}

func marshalGameplayRejoinBaseline(
	peerSession gameplayPeerSession, sourceTime uint64,
) ([][]byte, uint64, error) {
	if peerSession.zone == nil {
		return nil, 0, errors.New("rejoin zone unavailable")
	}
	for attempt := 0; attempt < rejoinSnapshotAttemptLimit; attempt++ {
		revision := peerSession.zone.ProjectionRevision()
		packets, err := marshalGameplayRejoinBaselineState(
			peerSession, sourceTime,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("rejoinSnapshot[%d]: %w", attempt, err)
		}
		if peerSession.zone.ProjectionRevision() == revision {
			return packets, revision, nil
		}
	}
	return nil, 0, errRejoinSnapshotActive
}

func marshalGameplayRejoinBaselineState(
	peerSession gameplayPeerSession, sourceTime uint64,
) ([][]byte, error) {
	if peerSession.zone == nil || peerSession.squad == nil ||
		peerSession.deployedObjectID == 0 ||
		peerSession.deployedCreatureIndex >= uint32(len(peerSession.binding.Creatures)) {
		return nil, errors.New("rejoin state unavailable")
	}
	binding := peerSession.binding
	for index := uint32(0); index < uint32(len(binding.Creatures)); index++ {
		character, isFound := peerSession.squad.Character(index)
		if !isFound {
			continue
		}
		binding.Creatures[index].HitPoint = character.HitPoints
		binding.Creatures[index].PowerPoint = character.ManaPoints
	}
	activeHitPoint := binding.Creatures[peerSession.deployedCreatureIndex].HitPoint
	activeManaPoint := binding.Creatures[peerSession.deployedCreatureIndex].PowerPoint
	actor, isFound := peerSession.zone.Hero().Snapshot(
		binding.UserID, peerSession.generation,
	)
	if isFound && actor.ObjectID == peerSession.deployedObjectID &&
		actor.CreatureIndex == peerSession.deployedCreatureIndex {
		activeHitPoint = actor.HitPoint
		activeManaPoint = actor.ManaPoint
		binding.Creatures[peerSession.deployedCreatureIndex].HitPoint = activeHitPoint
		binding.Creatures[peerSession.deployedCreatureIndex].PowerPoint = activeManaPoint
	}
	err := peerSession.zone.UpdateMemberRoster(
		binding.UserID, peerSession.generation, binding.Roster(),
	)
	if err != nil {
		return nil, fmt.Errorf("rejoinRoster: %w", err)
	}
	reconnectPacket, err := marshalDungeonReconnectState()
	if err != nil {
		return nil, fmt.Errorf("rejoinState: %w", err)
	}
	packets, err := marshalCampaignDungeonSetup(
		binding, peerSession.zone.ScriptObjectPlans(),
		peerSession.passiveModifierInstance, peerSession.playerPosition,
		sourceTime, campaignElapsedMilliseconds(peerSession.zone, time.Now()),
		peerSession.deployedCreatureIndex, true,
	)
	if err != nil {
		return nil, fmt.Errorf("rejoinDungeon: %w", err)
	}
	// Enter the dungeon presentation before publishing GameState and its
	// elapsed clock. When this state follows the baseline, the native client
	// can discard the preceding timer setup while still showing the ship UI.
	packets = append([][]byte{reconnectPacket}, packets...)
	crystalPackets, err := marshalGameplayCrystalState(peerSession)
	if err != nil {
		return nil, fmt.Errorf("rejoinCrystal: %w", err)
	}
	packets = append(packets, crystalPackets...)
	memberPackets, err := marshalOtherZoneHeroRosters(
		peerSession.zone, binding.UserID,
	)
	if err != nil {
		return nil, fmt.Errorf("rejoinMembers: %w", err)
	}
	packets = append(packets, memberPackets...)
	for index := uint32(0); index < uint32(len(binding.Creatures)); index++ {
		if binding.Creatures[index].Noun == 0 {
			continue
		}
		objectID := zonehero.ObjectID(peerSession.binding.Slot, index)
		updatePacket, marshalErr := raknet.MarshalApplication(
			raknet.ObjectUpdateMessage{
				ObjectID:  objectID,
				PositionX: peerSession.playerPosition.X,
				PositionY: peerSession.playerPosition.Y,
				PositionZ: peerSession.playerPosition.Z,
				IsVisible: index == peerSession.deployedCreatureIndex,
			},
		)
		if marshalErr != nil {
			return nil, fmt.Errorf("rejoinHero[%d]: %w", index, marshalErr)
		}
		packets = append(packets, updatePacket)
	}
	controlledPacket, err := raknet.MarshalApplication(
		raknet.LabsPlayerControlledObjectMessage{
			Slot: uint8(binding.Slot), ObjectID: peerSession.deployedObjectID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("rejoinControlled: %w", err)
	}
	deployPacket, err := raknet.MarshalApplication(
		raknet.PlayerCharacterDeployMessage{
			PlayerIndex:   uint8(binding.Slot),
			CreatureIndex: peerSession.deployedCreatureIndex,
			ObjectID:      peerSession.deployedObjectID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("rejoinDeploy: %w", err)
	}
	packets = append(packets, controlledPacket, deployPacket)
	for index, use := range peerSession.zone.Script().SnapshotUses() {
		usePackets, marshalErr := objectraknet.ScriptUse(use)
		if marshalErr != nil {
			return nil, fmt.Errorf("rejoinScriptUse[%d]: %w", index, marshalErr)
		}
		packets = append(packets, usePackets...)
	}
	for index, plans := range peerSession.zone.ActiveHordeBarrierPlans() {
		barrierPackets, marshalErr := barrierraknet.Create(plans)
		if marshalErr != nil {
			return nil, fmt.Errorf("rejoinBarrier[%d]: %w", index, marshalErr)
		}
		packets = append(packets, barrierPackets...)
	}
	objectiveMessages, err := campaignObjectiveMessages(
		peerSession.zone.Objective().State(), uint8(binding.Slot), 0,
	)
	if err != nil {
		return nil, fmt.Errorf("rejoinObjective: %w", err)
	}
	for index, message := range objectiveMessages {
		objectivePacket, marshalErr := raknet.MarshalApplication(message)
		if marshalErr != nil {
			return nil, fmt.Errorf("rejoinObjective[%d]: %w", index, marshalErr)
		}
		packets = append(packets, objectivePacket)
	}
	for index, npc := range peerSession.zone.NPCs().LiveSnapshots() {
		if !npc.IsPublished {
			continue
		}
		spawnPackets, marshalErr := npcraknet.Spawn(npc.FacingSpawnPlan())
		if marshalErr != nil {
			return nil, fmt.Errorf("rejoinNPC[%d]: %w", index, marshalErr)
		}
		packets = append(packets, spawnPackets...)
		facingPackets, facingErr := npcraknet.RestoreFacing(
			npc.Plan.ObjectID, npc.Plan.Position, npc.Facing,
		)
		if facingErr != nil {
			return nil, fmt.Errorf("rejoinNPCFacing[%d]: %w", index, facingErr)
		}
		packets = append(packets, facingPackets...)
		resourcePacket, marshalErr := raknet.MarshalApplication(
			raknet.CombatantDataUpdateMessage{
				ObjectID: npc.Plan.ObjectID, HitPoints: npc.HitPoint,
				ManaPoints: npc.ManaPoint,
			},
		)
		if marshalErr != nil {
			return nil, fmt.Errorf("rejoinNPCResource[%d]: %w", index, marshalErr)
		}
		packets = append(packets, resourcePacket)
	}
	targetPackets, err := npcraknet.TargetUpdates(
		peerSession.zone.NPCs().Snapshots(),
	)
	if err != nil {
		return nil, fmt.Errorf("rejoinNPCTarget: %w", err)
	}
	packets = append(packets, targetPackets...)
	companionPackets, err := marshalGameplayRejoinCompanions(peerSession)
	if err != nil {
		return nil, fmt.Errorf("rejoinCompanion: %w", err)
	}
	packets = append(packets, companionPackets...)
	pickupPackets, err := marshalGameplayRejoinPickups(peerSession)
	if err != nil {
		return nil, fmt.Errorf("rejoinPickup: %w", err)
	}
	packets = append(packets, pickupPackets...)
	securityPackets := make([][]byte, 0)
	if peerSession.zone.Security() != nil {
		securityPackets, err = securityraknet.SnapshotState(
			peerSession.zone.Security().Snapshot(),
		)
		if err != nil {
			return nil, fmt.Errorf("rejoinSecurity: %w", err)
		}
	}
	packets = append(packets, securityPackets...)
	if peerSession.isHeroSelectionPending && activeHitPoint <= 0 {
		beamPackets, marshalErr := heroraknet.BeamOut(
			peerSession.deployedObjectID,
			campaignCharacterBeam(
				binding.Creatures[peerSession.deployedCreatureIndex], false,
			),
			zonePosition(peerSession.playerPosition), sourceTime,
		)
		if marshalErr != nil {
			return nil, fmt.Errorf("rejoinDeathSelection: %w", marshalErr)
		}
		packets = append(packets, beamPackets...)
		return packets, nil
	}
	resourcePackets, err := peerSession.marshalResetBaselineAt(
		activeHitPoint, activeManaPoint,
	)
	if err != nil {
		return nil, fmt.Errorf("rejoinPlayer: %w", err)
	}
	packets = append(packets, resourcePackets...)
	if activeHitPoint <= 0 {
		deathPacket, marshalErr := raknet.MarshalApplication(raknet.SetAnimationStateMessage{
			ObjectID: peerSession.deployedObjectID,
			State:    util.HashID("gen_player_death"), Timestamp: sourceTime, Scale: 1,
		})
		if marshalErr != nil {
			return nil, fmt.Errorf("rejoinDeath: %w", marshalErr)
		}
		return append(packets, deathPacket), nil
	}
	beamPackets, err := heroraknet.BeamIn(
		peerSession.deployedObjectID,
		campaignCharacterBeam(
			binding.Creatures[peerSession.deployedCreatureIndex], true,
		),
		zonePosition(peerSession.playerPosition), sourceTime,
	)
	if err != nil {
		return nil, fmt.Errorf("rejoinBeam: %w", err)
	}
	packets = append(packets, beamPackets...)
	return packets, nil
}

func marshalGameplayRejoinCrystalInventory(
	peerSession gameplayPeerSession,
) ([]byte, error) {
	if peerSession.binding.Slot > 255 {
		return nil, fmt.Errorf("playerSlot: %d", peerSession.binding.Slot)
	}
	crystals := [9]raknet.LabsCrystalResource{}
	for index, slot := range peerSession.crystalInventory.Slots {
		if !slot.IsOccupied {
			continue
		}
		if slot.NounAsset == 0 || slot.CrystalLevel < 0 || slot.CrystalLevel > 0xffff {
			return nil, fmt.Errorf("slot[%d]: %#v", index, slot)
		}
		crystals[index] = raknet.LabsCrystalResource{
			Noun: slot.NounAsset, Level: uint16(slot.CrystalLevel),
		}
	}
	packet, err := raknet.MarshalApplication(
		raknet.LabsPlayerCrystalInventoryMessage{
			PlayerSlot: uint8(peerSession.binding.Slot), Crystals: crystals,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("inventoryMarshal: %w", err)
	}
	return packet, nil
}

func marshalGameplayCrystalBonuses(
	peerSession gameplayPeerSession,
) ([]byte, error) {
	if peerSession.binding.Slot > 255 {
		return nil, fmt.Errorf("playerSlot: %d", peerSession.binding.Slot)
	}
	links := peerSession.crystalInventory.Links()
	packet, err := raknet.MarshalApplication(
		raknet.LabsPlayerCrystalBonusesMessage{
			PlayerSlot:       uint8(peerSession.binding.Slot),
			AreBonusesActive: links.AreLinesActive,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("bonusesMarshal: %w", err)
	}
	return packet, nil
}

func marshalGameplayCrystalState(
	peerSession gameplayPeerSession,
) ([][]byte, error) {
	inventoryPacket, err := marshalGameplayRejoinCrystalInventory(peerSession)
	if err != nil {
		return nil, fmt.Errorf("inventory: %w", err)
	}
	bonusPacket, err := marshalGameplayCrystalBonuses(peerSession)
	if err != nil {
		return nil, fmt.Errorf("bonuses: %w", err)
	}
	return [][]byte{inventoryPacket, bonusPacket}, nil
}

func marshalDungeonReconnectState() ([]byte, error) {
	packet, err := raknet.MarshalApplication(raknet.StateMessage{
		State: raknet.GameDungeon,
	})
	if err != nil {
		return nil, fmt.Errorf("reconnectMarshal: %w", err)
	}
	return packet, nil
}

func marshalGameplayRejoinPickups(
	peerSession gameplayPeerSession,
) ([][]byte, error) {
	if peerSession.zone == nil || peerSession.zone.Pickups() == nil {
		return nil, nil
	}
	packets := make([][]byte, 0)
	for index, pickup := range peerSession.zone.Pickups().Snapshots() {
		encoded, err := marshalGameplayRejoinPickup(peerSession, pickup)
		if err != nil {
			return nil, fmt.Errorf("pickup[%d]: %w", index, err)
		}
		packets = append(packets, encoded...)
	}
	for index, pickup := range peerSession.zone.DNA().Snapshots() {
		position := raknet.Vector3{
			X: pickup.Position.X, Y: pickup.Position.Y, Z: pickup.Position.Z,
		}
		encoded, err := marshalGameplayRejoinSimplePickup(
			pickup.ObjectID, util.HashID("DNA.Noun"), position,
			raknet.LootDataUpdateMessage{
				ObjectID: pickup.ObjectID, DNAAmount: float32(pickup.Amount),
			},
		)
		if err != nil {
			return nil, fmt.Errorf("DNA[%d]: %w", index, err)
		}
		packets = append(packets, encoded...)
	}
	return packets, nil
}

func marshalGameplayRejoinPickup(
	peerSession gameplayPeerSession, pickup zoneinteract.Pickup,
) ([][]byte, error) {
	position := raknet.Vector3{
		X: pickup.Position.X, Y: pickup.Position.Y, Z: pickup.Position.Z,
	}
	switch pickup.Kind {
	case zoneinteract.PickupEquipment:
		payload, isFound := peerSession.zone.PickupPayload().Equipment(pickup.ObjectID)
		if !isFound {
			return nil, errors.New("equipment payload unavailable")
		}
		noun := zoneloot.EquipmentContainerNoun(zoneloot.Rarity(payload.Part.Rarity))
		create, err := raknet.MarshalApplication(raknet.EnemyObjectCreateMessage{
			ObjectID: pickup.ObjectID, Noun: util.HashID(noun),
			Position: position, Scale: 1, IsCollidable: true,
		})
		if err != nil {
			return nil, fmt.Errorf("equipmentCreate: %w", err)
		}
		interactable, err := raknet.MarshalApplication(raknet.InteractableDataUpdateMessage{
			ObjectID: pickup.ObjectID, UsesAllowed: 1,
			Ability: util.HashID("PickUpLoot"),
		})
		if err != nil {
			return nil, fmt.Errorf("equipmentInteractable: %w", err)
		}
		data, err := raknet.MarshalApplication(raknet.LootDataUpdateMessage{
			ObjectID: pickup.ObjectID, ItemID: uint64(pickup.ObjectID),
			RigblockAsset:        payload.Part.RigblockAssetHash,
			SuffixAsset:          payload.Part.SuffixAssetHash,
			PrefixAsset:          payload.Part.PrefixAssetHash,
			SecondaryPrefixAsset: payload.Part.PrefixSecondaryAssetHash,
			ItemLevel:            int32(payload.Part.Level), Rarity: int32(payload.Part.Rarity),
		})
		if err != nil {
			return nil, fmt.Errorf("equipmentData: %w", err)
		}
		return [][]byte{create, interactable, data}, nil
	case zoneinteract.PickupCrystal:
		payload, isFound := peerSession.zone.PickupPayload().Crystal(pickup.ObjectID)
		if !isFound {
			return nil, errors.New("crystal payload unavailable")
		}
		return marshalGameplayRejoinSimplePickup(
			pickup.ObjectID, util.HashID(payload.Request.NounName), position,
			raknet.CrystalLootDataUpdateMessage{
				ObjectID:     pickup.ObjectID,
				CrystalLevel: payload.Request.CrystalLevel,
			},
		)
	case zoneinteract.PickupDNA:
		payload, isFound := peerSession.zone.DNA().Lookup(pickup.ObjectID)
		if !isFound {
			return nil, errors.New("DNA payload unavailable")
		}
		return marshalGameplayRejoinSimplePickup(
			pickup.ObjectID, util.HashID("DNA.Noun"), position,
			raknet.LootDataUpdateMessage{
				ObjectID: pickup.ObjectID, DNAAmount: float32(payload.Amount),
			},
		)
	case zoneinteract.PickupOrb:
		payload, isFound := peerSession.zone.Orbs().Orb(pickup.ObjectID)
		if !isFound {
			return nil, errors.New("orb payload unavailable")
		}
		return marshalGameplayRejoinSimplePickup(
			pickup.ObjectID, util.HashID(payload.Request.NounName), position, nil,
		)
	default:
		return nil, errors.New("pickup kind unsupported")
	}
}

func marshalGameplayRejoinSimplePickup(
	objectID uint32, noun uint32, position raknet.Vector3,
	data raknet.ApplicationMessage,
) ([][]byte, error) {
	create, err := raknet.MarshalApplication(raknet.EnemyObjectCreateMessage{
		ObjectID: objectID, Noun: noun, Position: position,
		Scale: 1, IsCollidable: true,
	})
	if err != nil {
		return nil, fmt.Errorf("create: %w", err)
	}
	if data == nil {
		return [][]byte{create}, nil
	}
	encoded, err := raknet.MarshalApplication(data)
	if err != nil {
		return nil, fmt.Errorf("data: %w", err)
	}
	return [][]byte{create, encoded}, nil
}

func marshalGameplayRejoinCompanions(
	peerSession gameplayPeerSession,
) ([][]byte, error) {
	packets, err := marshalGameplayCompanions(peerSession, 0)
	if err != nil {
		return nil, fmt.Errorf("rejoinCompanions: %w", err)
	}
	return packets, nil
}

func marshalGameplayCompanions(
	peerSession gameplayPeerSession, ownerUserID uint64,
) ([][]byte, error) {
	if peerSession.zone == nil || peerSession.zone.Companion() == nil {
		return nil, nil
	}
	connectedGenerations := make(map[uint64]uint64)
	for _, member := range peerSession.zone.Snapshot().Members {
		if member.IsConnected || member.UserID == peerSession.binding.UserID {
			connectedGenerations[member.UserID] = member.PeerGeneration
		}
	}
	packets := make([][]byte, 0)
	for index, companion := range peerSession.zone.Companion().Snapshots() {
		if companion.HitPoint <= 0 ||
			(ownerUserID != 0 && companion.UserID != ownerUserID) ||
			connectedGenerations[companion.UserID] != companion.PeerGeneration {
			continue
		}
		if companion.OwnerObjectID == 0 || companion.OwnerObjectID >= zonehero.FirstSharedObjectID() {
			return nil, fmt.Errorf("companionOwner[%d]: invalid hero", index)
		}
		create, err := raknet.MarshalApplication(raknet.ObjectCreateMessage{
			ObjectID: companion.ObjectID, Noun: companion.Noun,
			PositionX: companion.Position.X, PositionY: companion.Position.Y,
			PositionZ: companion.Position.Z, Scale: 1, Team: 1,
			OwnerID: companion.OwnerObjectID, IsCollisionEnabled: true,
			PlayerIndex: uint8((companion.OwnerObjectID - 1) / squad.Size),
		})
		if err != nil {
			return nil, fmt.Errorf("create[%d]: %w", index, err)
		}
		attribute, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
			ObjectID: companion.ObjectID,
			Value: map[uint8]float32{
				11: zonecompanion.CompatibilityMovementSpeed,
				12: zonecompanion.CompatibilityMovementSpeed,
				48: 0,
			},
		})
		if err != nil {
			return nil, fmt.Errorf("attribute[%d]: %w", index, err)
		}
		resource, err := raknet.MarshalApplication(raknet.CombatantDataUpdateMessage{
			ObjectID: companion.ObjectID, HitPoints: companion.HitPoint,
		})
		if err != nil {
			return nil, fmt.Errorf("resource[%d]: %w", index, err)
		}
		position, err := raknet.MarshalApplication(raknet.ObjectUpdateMessage{
			ObjectID: companion.ObjectID, PositionX: companion.Position.X,
			PositionY: companion.Position.Y, PositionZ: companion.Position.Z, IsVisible: true,
		})
		if err != nil {
			return nil, fmt.Errorf("companionPosition[%d]: %w", index, err)
		}
		stop, err := raknet.MarshalApplication(raknet.ObjectPlayerMoveMessage{
			ObjectID: companion.ObjectID, GoalFlags: 0x20, GoalPosition: raknet.Vector3(companion.Position),
		})
		if err != nil {
			return nil, fmt.Errorf("companionStop[%d]: %w", index, err)
		}
		packets = append(packets, create, position, stop, attribute, resource)
	}
	return packets, nil
}
