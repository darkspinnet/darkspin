package gameplay

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/squad"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	barrierraknet "github.com/darkspinnet/darkspin/server/zone/barrier/raknet103"
	zoneboss "github.com/darkspinnet/darkspin/server/zone/boss"
	bossraknet "github.com/darkspinnet/darkspin/server/zone/boss/raknet103"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	companionraknet "github.com/darkspinnet/darkspin/server/zone/companion/raknet103"
	zonehero "github.com/darkspinnet/darkspin/server/zone/hero"
	heroraknet "github.com/darkspinnet/darkspin/server/zone/hero/raknet103"
	zoneinteract "github.com/darkspinnet/darkspin/server/zone/interact"
	zoneloot "github.com/darkspinnet/darkspin/server/zone/loot"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
	objectraknet "github.com/darkspinnet/darkspin/server/zone/object/raknet103"
	questraknet "github.com/darkspinnet/darkspin/server/zone/quest/raknet103"
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
	treeRevision := uint64(0)
	if peerSession.zone != nil && peerSession.zone.TreeOfLife() != nil {
		treeRevision = peerSession.zone.TreeOfLife().Revision()
	}
	if peerSession.zone == nil {
		return nil, errors.New("rejoin zone unavailable")
	}
	companionRevision := peerSession.zone.Companion().MotionRevision()
	baselineTime := r.now()
	baseline, revision, err := marshalGameplayRejoinBaseline(
		peerSession, packet.SourceTime, baselineTime,
	)
	if err != nil {
		return nil, fmt.Errorf("rejoinBaseline: %w", err)
	}
	cooldownPackets, err := marshalGameplayRejoinCooldowns(
		peerSession, r.program, r.now(), packet.SourceTime,
	)
	if err != nil {
		return nil, fmt.Errorf("rejoinCooldown: %w", err)
	}
	// The baseline's final deployment can reset the native action bar. Restore
	// remaining cooldowns afterward, only in this owner's reconnect response.
	baseline = append(baseline, cooldownPackets...)
	overdrivePackets, err := r.registry.overdriveRejoinPackets(peerSession, baselineTime)
	if err != nil {
		return nil, fmt.Errorf("rejoinOverdrive: %w", err)
	}
	baseline = append(baseline, overdrivePackets...)
	mutationPackets, mutationFrames, err := r.registry.mutationRejoinPackets(peerSession, baselineTime, packet.SourceTime)
	if err != nil {
		return nil, fmt.Errorf("rejoinMutation: %w", err)
	}
	baseline = append(baseline, mutationPackets...)
	mutationDelivery := campaignProjectionDelivery{
		registry: r.registry, sessionKey: packet.Address.String(),
		generation: peerSession.generation, transportGeneration: peerSession.transportGeneration,
		mutationEvents: mutationFrames,
	}
	err = packet.AfterResponseCommit(mutationDelivery.commit)
	if err != nil {
		return nil, fmt.Errorf("rejoinMutationCommit: %w", err)
	}
	shieldPackets, err := r.registry.nomadDragShieldPackets(peerSession.zone, r.now())
	if err != nil {
		return nil, fmt.Errorf("rejoinDragShield: %w", err)
	}
	baseline = append(baseline, shieldPackets...)
	effectPool := r.damage.effectPool
	if peerSession.binding.Mode == game.ModeArena && peerSession.arenaEffectPool != nil {
		effectPool = peerSession.arenaEffectPool
	}
	titanPackets, err := r.registry.tcShieldReplay(peerSession, effectPool)
	if err != nil {
		return nil, fmt.Errorf("rejoinTitanShield: %w", err)
	}
	baseline = append(baseline, titanPackets...)
	energyPackets, err := r.registry.energySentinelPresentation(peerSession, packet.SourceTime)
	if err != nil {
		return nil, fmt.Errorf("rejoinEnergyPassive: %w", err)
	}
	baseline = append(baseline, energyPackets...)
	silencePackets, err := r.registry.silenceFieldPresentation(peerSession, packet.SourceTime)
	if err != nil {
		return nil, fmt.Errorf("rejoinSilenceField: %w", err)
	}
	baseline = append(baseline, silencePackets...)
	beastPackets, err := r.registry.beastPetVulnerabilityReplay(peerSession, baselineTime, packet.SourceTime)
	if err != nil {
		return nil, fmt.Errorf("rejoinBeastDebuff: %w", err)
	}
	baseline = append(baseline, beastPackets...)
	modifierPackets, err := r.registry.basicHitModifierReplay(peerSession, baselineTime, packet.SourceTime)
	if err != nil {
		return nil, fmt.Errorf("rejoinBasicModifier: %w", err)
	}
	baseline = append(baseline, modifierPackets...)
	briarPackets, err := r.registry.supportHealerRootReplay(peerSession, baselineTime, packet.SourceTime)
	if err != nil {
		return nil, fmt.Errorf("rejoinBriarRoot: %w", err)
	}
	baseline = append(baseline, briarPackets...)
	omniPackets, err := r.registry.omniShieldReplay(peerSession, baselineTime, packet.SourceTime)
	if err != nil {
		return nil, fmt.Errorf("rejoinOmniShield: %w", err)
	}
	baseline = append(baseline, omniPackets...)
	if peerSession.zone.TreeOfLife() != nil && peerSession.zone.TreeOfLife().Revision() != treeRevision {
		return nil, fmt.Errorf("rejoinTreeCapture: %w", errRejoinSnapshotActive)
	}
	if peerSession.zone.Companion().MotionRevision() != companionRevision {
		return nil, fmt.Errorf("rejoinCompanionCapture: %w", errRejoinSnapshotActive)
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
			revision, treeRevision, companionRevision,
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
	sessionKey string, transportGeneration uint64, revision uint64, treeRevision uint64,
	companionRevisions ...uint64,
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
	// A tree may spawn or retire after baseline encoding but before transport
	// commit. Keep the reconnect pending for a fresh baseline in that case;
	// queued removals remain behind it rather than preceding a stale create.
	if peerSession.zone.TreeOfLife() != nil &&
		peerSession.zone.TreeOfLife().Revision() != treeRevision {
		r.registry.mutex.Unlock()
		return
	}
	if len(companionRevisions) == 1 &&
		peerSession.zone.Companion().MotionRevision() != companionRevisions[0] {
		r.registry.mutex.Unlock()
		return
	}
	isCurrent = isFound &&
		current.transportGeneration == transportGeneration &&
		current.isRejoinPending
	if isCurrent {
		current.isRejoinPending = false
		current.isGraviticSpeedPresented = false
		current.isNPCRecoveryPending = true
		current.securityTeleporterStates = nil
		current.campaignTeleporterStates = nil
		current.isTutorialTeleporterPresentationPending = true
		if current.beastPetResurrection != nil {
			beastErr := current.rebindBeastPassive(current.beastPetResurrection.packet, sessionKey)
			if beastErr != nil && r.logger != nil {
				r.logger.Printf("RakNet rejoined Savage attack scheduling failed user=%d: %v", current.binding.UserID, beastErr)
			}
		}
		r.registry.sessions[sessionKey] = current
	}
	r.registry.mutex.Unlock()
	if !isCurrent {
		_, _ = peerSession.zone.Disconnect(
			peerSession.binding.UserID, peerSession.generation,
		)
		return
	}
	companionErr := r.publishRejoinedCompanions(sessionKey, peerSession)
	if companionErr != nil {
		if r.logger != nil {
			r.logger.Printf("RakNet reconnect companion publication failed user=%d: %v",
				peerSession.binding.UserID, companionErr)
		}
		return
	}
	current.commitSagePassiveReplay()
}

func marshalGameplayRejoinBaseline(
	peerSession gameplayPeerSession, sourceTime uint64, capturedTimes ...time.Time,
) ([][]byte, uint64, error) {
	if peerSession.zone == nil {
		return nil, 0, errors.New("rejoin zone unavailable")
	}
	for attempt := 0; attempt < rejoinSnapshotAttemptLimit; attempt++ {
		revision := peerSession.zone.ProjectionRevision()
		companionRevision := peerSession.zone.Companion().MotionRevision()
		trees := peerSession.zone.TreeOfLife()
		treeRevision := uint64(0)
		if trees != nil {
			treeRevision = trees.Revision()
		}
		packets, err := marshalGameplayRejoinBaselineState(
			peerSession, sourceTime, capturedTimes...,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("rejoinSnapshot[%d]: %w", attempt, err)
		}
		isTreeSnapshotCurrent := trees == nil || trees.Revision() == treeRevision
		isCompanionSnapshotCurrent := peerSession.zone.Companion().MotionRevision() == companionRevision
		if peerSession.zone.ProjectionRevision() == revision && isTreeSnapshotCurrent && isCompanionSnapshotCurrent {
			return packets, revision, nil
		}
	}
	return nil, 0, errRejoinSnapshotActive
}

func marshalGameplayRejoinBaselineState(
	peerSession gameplayPeerSession, sourceTime uint64, capturedTimes ...time.Time,
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
		maximum := peerSession.characterResourceMaximums(index)
		binding.Creatures[index].MaximumHitPoint = maximum.hitPoint
		binding.Creatures[index].MaximumPowerPoint = maximum.manaPoint
	}
	activeHitPoint := binding.Creatures[peerSession.deployedCreatureIndex].HitPoint
	activeManaPoint := binding.Creatures[peerSession.deployedCreatureIndex].PowerPoint
	orientation := raknet.Quaternion{W: 1}
	actor, isFound := peerSession.zone.Hero().Snapshot(
		binding.UserID, peerSession.generation,
	)
	if isFound && actor.ObjectID == peerSession.deployedObjectID &&
		actor.CreatureIndex == peerSession.deployedCreatureIndex {
		orientation = raknet.Quaternion(actor.Orientation)
		activeHitPoint = actor.HitPoint
		activeManaPoint = actor.ManaPoint
		binding.Creatures[peerSession.deployedCreatureIndex].HitPoint = activeHitPoint
		binding.Creatures[peerSession.deployedCreatureIndex].PowerPoint = activeManaPoint
		binding.Creatures[peerSession.deployedCreatureIndex].MaximumHitPoint = actor.MaximumHitPoint
		binding.Creatures[peerSession.deployedCreatureIndex].MaximumPowerPoint = actor.MaximumManaPoint
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
		peerSession.zone.SceneryPlans(), peerSession.zone.SceneryDeleteObjectIDs(),
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
	rootPackets, err := peerSession.nightmareVineDeadRootPackets(sourceTime)
	if err != nil {
		return nil, fmt.Errorf("rejoinVineRoots: %w", err)
	}
	packets = append(packets, rootPackets...)
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
	soulPackets, err := peerSession.marshalSoulRavagerPresentation()
	if err != nil {
		return nil, fmt.Errorf("rejoinSoulRavager: %w", err)
	}
	packets = append(packets, soulPackets...)
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
	placementPacket, err := raknet.MarshalApplication(raknet.ObjectTeleportMessage{
		ObjectID: peerSession.deployedObjectID, Position: peerSession.playerPosition,
		Orientation: orientation,
	})
	if err != nil {
		return nil, fmt.Errorf("rejoinPlacement: %w", err)
	}
	packets = append(packets, placementPacket)
	for index, use := range peerSession.zone.Script().SnapshotUses() {
		usePackets, marshalErr := objectraknet.ScriptUseSnapshot(use)
		if marshalErr != nil {
			return nil, fmt.Errorf("rejoinScriptUse[%d]: %w", index, marshalErr)
		}
		packets = append(packets, usePackets...)
	}
	activeBarrierPlans := peerSession.zone.ActiveHordeBarrierPlans()
	for index, plans := range peerSession.zone.OpenHordeBarrierPlans() {
		barrierPackets, marshalErr := barrierraknet.CreateOpened(plans)
		if marshalErr != nil {
			return nil, fmt.Errorf("rejoinOpenBarrier[%d]: %w", index, marshalErr)
		}
		packets = append(packets, barrierPackets...)
	}
	for index, plans := range activeBarrierPlans {
		barrierPackets, marshalErr := barrierraknet.Create(plans)
		if marshalErr != nil {
			return nil, fmt.Errorf("rejoinBarrier[%d]: %w", index, marshalErr)
		}
		packets = append(packets, barrierPackets...)
	}
	if len(activeBarrierPlans) != 0 {
		statePacket, stateErr := raknet.MarshalApplication(raknet.DirectorStateMessage{
			IsHordeSpawned: true, IsHordeSpawnedPresent: true,
		})
		if stateErr != nil {
			return nil, fmt.Errorf("rejoinHordeState: %w", stateErr)
		}
		packets = append(packets, statePacket)
	}
	objectiveMessages, err := campaignObjectiveMessages(
		peerSession.zone.Objective().State(), uint8(binding.Slot),
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
	introTime := time.Now()
	if len(capturedTimes) > 0 {
		introTime = capturedTimes[0]
	}
	liveNPCs := peerSession.zone.NPCs().LivePresentationSnapshots()
	for index, npc := range liveNPCs {
		if !npc.IsPublished {
			continue
		}
		spawnPackets, marshalErr := npcraknet.SpawnSnapshot(npc, introTime, sourceTime)
		if marshalErr != nil {
			return nil, fmt.Errorf("rejoinNPC[%d]: %w", index, marshalErr)
		}
		if len(spawnPackets) == 0 {
			continue
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
	bossState := peerSession.zone.Boss().Snapshot()
	if bossState.Phase == zoneboss.PhaseActive ||
		(bossState.Phase == zoneboss.PhaseArming && bossState.IsSecondWaveRequested) {
		if bossState.IsLeaderDeferred && !bossState.IsSecondWaveAdmitted {
			phasePacket, phaseErr := bossraknet.AddPhase()
			if phaseErr != nil {
				return nil, fmt.Errorf("rejoinBossPhase: %w", phaseErr)
			}
			packets = append(packets, phasePacket)
		} else {
			for _, npc := range liveNPCs {
				if npc.Plan.ObjectID != bossState.LeaderObjectID || !npc.IsPublished || !npc.IsIntroductionBossActive() || npc.IsCorruptorTransitionHidden(introTime) {
					continue
				}
				activePacket, activeErr := bossraknet.Active(
					bossState.LeaderObjectID,
					zoneboss.IsFinalBossNoun(npc.Plan.NounName),
				)
				if activeErr != nil {
					return nil, fmt.Errorf("rejoinBossActive: %w", activeErr)
				}
				packets = append(packets, activePacket)
				break
			}
		}
	}
	remnantPackets, err := npcraknet.Remnants(peerSession.zone.NPCs().Snapshots())
	if err != nil {
		return nil, fmt.Errorf("rejoinRemnants: %w", err)
	}
	packets = append(packets, remnantPackets...)
	drainerPackets, err := npcraknet.DrainerRemnants(peerSession.zone.NPCs().Snapshots(), sourceTime)
	if err != nil {
		return nil, fmt.Errorf("rejoinDrainer: %w", err)
	}
	packets = append(packets, drainerPackets...)
	for _, geyser := range peerSession.zone.NPCs().MerakGeysers() {
		geyserPacket, geyserErr := merakGeyserCreate(geyser)
		if geyserErr != nil {
			return nil, fmt.Errorf("rejoinGeyser: %w", geyserErr)
		}
		packets = append(packets, geyserPacket)
	}
	targetPackets, err := npcraknet.TargetUpdates(
		peerSession.zone.NPCs().Snapshots(),
	)
	if err != nil {
		return nil, fmt.Errorf("rejoinNPCTarget: %w", err)
	}
	packets = append(packets, targetPackets...)
	for _, npc := range liveNPCs {
		if !npc.IsPublished {
			continue
		}
		introPackets, introErr := npcraknet.Introduction(npc, introTime)
		if introErr != nil {
			return nil, fmt.Errorf("rejoinIntroduction: %w", introErr)
		}
		packets = append(packets, introPackets...)
	}
	companionPackets, err := marshalGameplayRejoinCompanions(peerSession, capturedTimes...)
	if err != nil {
		return nil, fmt.Errorf("rejoinCompanion: %w", err)
	}
	packets = append(packets, companionPackets...)
	treePackets, err := marshalGameplayRejoinTrees(peerSession)
	if err != nil {
		return nil, fmt.Errorf("rejoinTrees: %w", err)
	}
	packets = append(packets, treePackets...)
	pickupPackets, err := marshalGameplayRejoinPickups(peerSession, sourceTime)
	if err != nil {
		return nil, fmt.Errorf("rejoinPickup: %w", err)
	}
	packets = append(packets, pickupPackets...)
	securityPackets := make([][]byte, 0)
	if peerSession.zone.Security() != nil {
		securityPackets, err = securityraknet.SnapshotStateWithRotations(
			peerSession.zone.Security().Snapshot(),
			peerSession.securityTeleporterRotations(),
			peerSession.zone.SecurityThreats(),
		)
		if err != nil {
			return nil, fmt.Errorf("rejoinSecurity: %w", err)
		}
	}
	packets = append(packets, securityPackets...)
	tutorialTeleporterPackets, err := peerSession.tutorialTeleporterInitialState()
	if err != nil {
		return nil, fmt.Errorf("rejoinTutorialTeleporter: %w", err)
	}
	packets = append(packets, tutorialTeleporterPackets...)
	campaignTeleporterPackets, err := peerSession.campaignTeleporterInitialState()
	if err != nil {
		return nil, fmt.Errorf("rejoinCampaignTeleporter: %w", err)
	}
	packets = append(packets, campaignTeleporterPackets...)
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

func marshalGameplayRejoinTrees(peerSession gameplayPeerSession) ([][]byte, error) {
	if peerSession.zone == nil || peerSession.zone.TreeOfLife() == nil {
		return nil, nil
	}
	now := peerSession.zone.Elapsed(time.Now())
	trees := peerSession.zone.TreeOfLife().SnapshotsAt(now)
	packets := make([][]byte, 0, len(trees))
	for index, tree := range trees {
		packet, err := abilityraknet.MarshalTreeOfLifePresentation(tree, now)
		if err != nil {
			return nil, fmt.Errorf("treeSnapshot[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func marshalGameplayRejoinPickups(
	peerSession gameplayPeerSession, sourceTimes ...uint64,
) ([][]byte, error) {
	if peerSession.zone == nil || peerSession.zone.Pickups() == nil {
		return nil, nil
	}
	packets := make([][]byte, 0)
	for index, pickup := range peerSession.zone.Pickups().SnapshotsAt(
		peerSession.zone.Elapsed(time.Now()),
	) {
		encoded, err := marshalGameplayRejoinPickup(peerSession, pickup, sourceTimes...)
		if err != nil {
			return nil, fmt.Errorf("pickup[%d]: %w", index, err)
		}
		packets = append(packets, encoded...)
	}
	for index, pickup := range peerSession.zone.DNA().Snapshots() {
		err := peerSession.validatePickupNounType("DNA.Noun", game.NounTypeLoot)
		if err != nil {
			return nil, fmt.Errorf("dnaSnapshotNounType[%d]: %w", index, err)
		}
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
		flightPackets, err := marshalPickupFlight(pickup.ObjectID, pickup.Flight, true)
		if err != nil {
			return nil, fmt.Errorf("dnaFlight[%d]: %w", index, err)
		}
		encoded = append(encoded, flightPackets...)
		packets = append(packets, encoded...)
	}
	return packets, nil
}

func marshalGameplayRejoinPickup(peerSession gameplayPeerSession, pickup zoneinteract.Pickup, sourceTimes ...uint64) ([][]byte, error) {
	packets, err := marshalGameplayRejoinPickupPayload(peerSession, pickup, sourceTimes...)
	if err != nil {
		return nil, fmt.Errorf("pickupPayload: %w", err)
	}
	if len(packets) == 0 {
		return nil, nil
	}
	if pickup.Kind == zoneinteract.PickupQuestCrystal {
		return packets, nil
	}
	flightPackets, err := marshalPickupFlight(pickup.ObjectID, pickup.Flight, true)
	if err != nil {
		return nil, fmt.Errorf("pickupFlight: %w", err)
	}
	return append(packets, flightPackets...), nil
}

func marshalGameplayRejoinPickupPayload(
	peerSession gameplayPeerSession, pickup zoneinteract.Pickup, sourceTimes ...uint64,
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
		noun, isSupported := payload.PresentationPolicy.ContainerNoun(zoneloot.Rarity(payload.Part.Rarity))
		if !isSupported {
			return nil, nil
		}
		err := peerSession.validatePickupNounType(noun, game.NounTypeLoot)
		if err != nil {
			return nil, fmt.Errorf("equipmentNounType: %w", err)
		}
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
		err := peerSession.validatePickupNounType(payload.Request.NounName, game.NounTypeCrystal)
		if err != nil {
			return nil, fmt.Errorf("crystalNounType: %w", err)
		}
		return marshalGameplayRejoinSimplePickup(
			pickup.ObjectID, util.HashID(payload.Request.NounName), position,
			raknet.CrystalLootDataUpdateMessage{
				ObjectID:     pickup.ObjectID,
				CrystalLevel: payload.Request.CrystalLevel,
			},
		)
	case zoneinteract.PickupQuestCrystal:
		crystal, isFound := peerSession.zone.QuestCrystals().Crystal(pickup.ObjectID, time.Now())
		if !isFound {
			return nil, errors.New("quest crystal payload unavailable")
		}
		sourceTime := uint64(0)
		if len(sourceTimes) > 0 {
			sourceTime = sourceTimes[0]
		}
		return questraknet.SpawnAt(crystal, time.Now(), sourceTime)
	case zoneinteract.PickupDNA:
		payload, isFound := peerSession.zone.DNA().Lookup(pickup.ObjectID)
		if !isFound {
			return nil, errors.New("DNA payload unavailable")
		}
		err := peerSession.validatePickupNounType("DNA.Noun", game.NounTypeLoot)
		if err != nil {
			return nil, fmt.Errorf("dnaNounType: %w", err)
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
		err := peerSession.validateOrbNounType(payload.Request)
		if err != nil {
			return nil, fmt.Errorf("orbNounType: %w", err)
		}
		var data raknet.ApplicationMessage
		if payload.Request.Kind == sim.ResurrectionOrbDrop {
			data = raknet.InteractableDataUpdateMessage{
				ObjectID: pickup.ObjectID, UsesAllowed: 1,
				Ability: util.HashID("PickUpResurrectOrb"),
			}
		}
		return marshalGameplayRejoinSimplePickup(
			pickup.ObjectID, util.HashID(payload.Request.NounName), position, data,
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
	peerSession gameplayPeerSession, capturedTimes ...time.Time,
) ([][]byte, error) {
	packets, err := marshalGameplayCompanions(peerSession, 0, capturedTimes...)
	if err != nil {
		return nil, fmt.Errorf("rejoinCompanions: %w", err)
	}
	return packets, nil
}

func marshalGameplayCompanions(
	peerSession gameplayPeerSession, ownerUserID uint64, capturedTimes ...time.Time,
) ([][]byte, error) {
	if peerSession.zone == nil || peerSession.zone.Companion() == nil {
		return nil, nil
	}
	connectedGenerations := make(map[uint64]uint64)
	retainedGenerations := make(map[uint64]uint64)
	for _, member := range peerSession.zone.Snapshot().Members {
		if member.IsConnected || member.UserID == peerSession.binding.UserID {
			connectedGenerations[member.UserID] = member.PeerGeneration
		} else {
			retainedGenerations[member.UserID] = member.PeerGeneration
		}
	}
	packets := make([][]byte, 0)
	at := time.Now()
	if len(capturedTimes) > 0 {
		at = capturedTimes[0]
	}
	for index, motion := range peerSession.zone.Companion().MotionSnapshots(at) {
		companion := motion.Actor
		isRetainedSage := companion.IsSageCompanion &&
			retainedGenerations[companion.UserID] == companion.PeerGeneration
		if companion.HitPoint <= 0 {
			deathProfile, isDeathKnown := zonecompanion.DeathProfileForNoun(companion.Noun)
			if isDeathKnown && deathProfile.Duration > 0 {
				deletePacket, deleteErr := companionraknet.Defeat(companion.ObjectID)
				if deleteErr != nil {
					return nil, fmt.Errorf("corpseDelete[%d]: %w", index, deleteErr)
				}
				packets = append(packets, deletePacket)
			}
			continue
		}
		if (ownerUserID != 0 && companion.UserID != ownerUserID) ||
			(connectedGenerations[companion.UserID] != companion.PeerGeneration && !isRetainedSage) {
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
		movementSpeed := float32(zonecompanion.CompatibilityMovementSpeed)
		movementBuff, isMovementFound := peerSession.zone.Companion().MovementSpeedBuff(companion.ObjectID, at)
		if !isMovementFound {
			return nil, fmt.Errorf("companionMovement[%d]: actor missing", index)
		}
		if companion.MovementSpeed > 0 {
			movementSpeed = companion.MovementSpeed
		}
		if motion.IsFollowing {
			movementSpeed = motion.MovementSpeed / (1 + movementBuff)
			if movementSpeed <= 0 || math.IsNaN(float64(movementSpeed)) || math.IsInf(float64(movementSpeed), 0) {
				return nil, fmt.Errorf("companionSpeed[%d]: invalid retained motion", index)
			}
		}
		attribute, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
			ObjectID: companion.ObjectID,
			Value: map[uint8]float32{
				4:   companion.MaximumHitPoint,
				11:  movementSpeed,
				12:  movementSpeed,
				48:  movementBuff,
				113: companion.BodyScale,
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
		packets = append(packets, create, position, attribute, resource)
		if companion.IsAbilityControlled && companion.AbilityAnimation != "" {
			animation, animationErr := raknet.MarshalApplication(raknet.SetAnimationStateMessage{
				ObjectID: companion.ObjectID, State: util.HashID(companion.AbilityAnimation), Scale: 1,
			})
			if animationErr != nil {
				return nil, fmt.Errorf("companionAnimation[%d]: %w", index, animationErr)
			}
			packets = append(packets, animation)
		}
		if motion.IsFollowing {
			followPackets, followErr := companionraknet.Follow([]zonecompanion.Follow{motion.Follow})
			if followErr != nil {
				return nil, fmt.Errorf("companionFollow[%d]: %w", index, followErr)
			}
			packets = append(packets, followPackets...)
			continue
		}
		stop, err := raknet.MarshalApplication(raknet.ObjectPlayerMoveMessage{
			ObjectID: companion.ObjectID, GoalFlags: 0x20, GoalPosition: raknet.Vector3(companion.Position),
		})
		if err != nil {
			return nil, fmt.Errorf("companionStop[%d]: %w", index, err)
		}
		packets = append(packets, stop)
	}
	signPackets, signErr := marshalBeastResurrectionSigns(peerSession, ownerUserID)
	if signErr != nil {
		return nil, fmt.Errorf("companionResurrectionSigns: %w", signErr)
	}
	packets = append(packets, signPackets...)
	return packets, nil
}
