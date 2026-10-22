package gameplay

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
)

const campaignOverdriveMaximumEnergy = game.OverdriveMaximumEnergy
const campaignOverdriveBaseDrainPerSecond = game.OverdriveDrainPerSecond

func (e gameplayPeerSession) overdriveKillCharge(objectID uint32) uint32 {
	if e.zone == nil || e.zone.NPCs() == nil {
		return 0
	}
	enemy, isFound := e.zone.NPCs().NPC(objectID)
	if !isFound || !enemy.IsDefeated || enemy.Plan.IsFixture {
		return 0
	}
	// Requested balance policy, not a recovered native reward formula:
	// 84 ordinary kills fill 420 energy, and non-minions award four times more.
	if enemy.Plan.NPCProfile.NPCType != 0 {
		return 20
	}
	return 5
}

type campaignOverdriveRuntime struct {
	registry *gameplaySessionRegistry
	switcher gameplaySwitchRuntime
	now      func() time.Time
	logger   *log.Logger
}

func (r campaignOverdriveRuntime) handle(
	packet raknet.Packet, command raknet.ActionCommandData,
) ([][]byte, error) {
	if command.Common.Type != raknet.ActionOverdrive {
		return nil, errors.New("overdrive command invalid")
	}
	sessionKey := packet.Address.String()
	now := r.now()
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isAccepted := isFound && peerSession.stage.IsDungeon() &&
		peerSession.binding.IsOverdriveUnlocked &&
		command.Common.ObjectID == peerSession.deployedObjectID &&
		peerSession.deployedHitPoint() > 0 &&
		peerSession.isAbilityReleaseReady(now) &&
		!peerSession.isHeroArrivalProtected(now) &&
		!peerSession.isOverdriveSpent &&
		peerSession.overdriveEnergy >= float32(campaignOverdriveMaximumEnergy) &&
		!peerSession.isOverdriveActiveAt(now)
	if !isAccepted {
		r.registry.mutex.Unlock()
		return nil, errors.New("overdrive unavailable")
	}
	duration := campaignOverdriveDuration(peerSession)
	ackPacket, err := abilityraknet.Acknowledge(abilityraknet.AcknowledgeRequest{
		SyncStamp:    command.Common.Unknown[0],
		ResponseType: raknet.ActionResponseAccepted,
		// Accepted responses resolve an ability definition, not a hero object.
		// Build 103 sub_4D64D0 dereferences that lookup without a null guard.
		ObjectID:                 util.HashID("Overdrive"),
		SourceStartMilliseconds:  packet.SourceTime,
		SourceCommitMilliseconds: packet.SourceTime + 300,
		SourceEndMilliseconds:    packet.SourceTime + 400,
	})
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("overdriveAcknowledge: %w", err)
	}
	animationPacket, err := raknet.MarshalApplication(raknet.SetAnimationStateMessage{
		ObjectID: peerSession.deployedObjectID, State: util.HashID("overdrive_initiate"),
		Timestamp: packet.SourceTime, Scale: 1,
	})
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("overdriveAnimation: %w", err)
	}
	stopPackets, err := marshalZonePlayerStop(peerSession.deployedObjectID, peerSession.playerPosition)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("overdriveStop: %w", err)
	}
	err = peerSession.stopPlayerMovement(now)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("overdriveMovement: %w", err)
	}
	releaseReservation, isReserved := peerSession.abilityReleaseSession().Reserve(now, 400*time.Millisecond)
	if !isReserved {
		r.registry.mutex.Unlock()
		return nil, errors.New("overdrive release unavailable")
	}
	peerSession.isOverdriveSpent = true
	peerSession.overdriveEnergy = 0
	peerSession.overdriveStartsAt = now.Add(300 * time.Millisecond)
	peerSession.overdriveExpiresAt = peerSession.overdriveStartsAt.Add(duration)
	peerSession.overdriveActivation++
	peerSession.overdriveObjectID = peerSession.deployedObjectID
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	step := campaignOverdriveMeterStep{
		runtime: r, packet: packet.Autonomous(), sessionKey: sessionKey,
		generation: peerSession.generation, activation: peerSession.overdriveActivation,
		objectID: peerSession.deployedObjectID, creatureIndex: peerSession.deployedCreatureIndex,
		identity: gameplayProducerIdentityFromSession(sessionKey, peerSession, true),
	}
	scheduleErr := step.schedule(300 * time.Millisecond)
	if scheduleErr != nil {
		r.registry.mutex.Lock()
		current, isCurrent := r.registry.sessions[sessionKey]
		if isCurrent && current.generation == peerSession.generation &&
			current.overdriveActivation == step.activation {
			current.isOverdriveSpent = false
			current.overdriveEnergy = float32(campaignOverdriveMaximumEnergy)
			current.overdriveExpiresAt = time.Time{}
			current.overdriveStartsAt = time.Time{}
			current.overdriveObjectID = 0
			isRolledBack := current.abilityReleaseSession().Rollback(releaseReservation)
			if !isRolledBack && r.logger != nil {
				r.logger.Printf("RakNet Overdrive release rollback superseded user=%d", current.binding.UserID)
			}
			r.registry.sessions[sessionKey] = current
		}
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("overdriveSchedule: %w", scheduleErr)
	}
	if r.logger != nil {
		r.logger.Printf(
			"RakNet campaign Overdrive activated game=%d user=%d duration=%s",
			peerSession.binding.GameID, peerSession.binding.UserID, duration,
		)
	}
	packets := append([][]byte{ackPacket}, stopPackets...)
	return append(packets, animationPacket), nil
}

type campaignOverdriveMeterStep struct {
	runtime       campaignOverdriveRuntime
	packet        raknet.Packet
	sessionKey    string
	generation    uint64
	activation    uint64
	objectID      uint32
	creatureIndex uint32
	isPresented   bool
	identity      gameplayProducerIdentity
	packets       [][]byte
	nextDelay     time.Duration
	isContinuing  bool
	sagePoll      *sagePassivePoll
}

func (e *campaignOverdriveMeterStep) schedule(delay time.Duration) error {
	producer := raknet.ScheduledPacketProducer{
		Delay: delay, Produce: e.produce, AfterCommit: e.commit,
	}
	cancel, err := e.packet.ScheduleProducers([]raknet.ScheduledPacketProducer{producer})
	if err != nil {
		return fmt.Errorf("overdriveGroup: %w", err)
	}
	if cancel == nil {
		return errors.New("overdrive group cancellation unavailable")
	}
	return nil
}

func (e *campaignOverdriveMeterStep) produce() ([][]byte, error) {
	e.packets = nil
	e.isContinuing = false
	e.sagePoll = nil
	now := e.runtime.now()
	e.runtime.registry.mutex.RLock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || member.generation != e.generation ||
		member.transportGeneration != e.identity.transportGeneration ||
		member.overdriveActivation != e.activation {
		e.runtime.registry.mutex.RUnlock()
		return nil, nil
	}
	isActive := member.deployedObjectID == e.objectID && member.isOverdriveActiveAt(now)
	remaining := member.overdriveExpiresAt.Sub(now)
	energy := member.presentedOverdriveEnergy(now)
	e.runtime.registry.mutex.RUnlock()
	if now.Before(member.overdriveStartsAt) && remaining > 0 {
		e.nextDelay = member.overdriveStartsAt.Sub(now)
		e.isContinuing = true
		return nil, nil
	}
	statePacket, err := raknet.MarshalApplication(raknet.LabsPlayerOverdriveStateMessage{
		Slot: uint8(member.binding.Slot), Energy: energy, IsActive: isActive,
	})
	if err != nil {
		return nil, fmt.Errorf("overdriveMeter: %w", err)
	}
	packets := [][]byte{statePacket}
	if !e.isPresented && isActive {
		passivePackets, passiveErr := e.runtime.switcher.restartOverdriveEntryPassives(
			e.packet, e.sessionKey, e.generation,
		)
		if passiveErr != nil {
			return nil, fmt.Errorf("overdrivePassiveEntry: %w", passiveErr)
		}
		packets = append(packets, passivePackets...)
		sagePackets, sagePoll, sageErr := e.runtime.switcher.restartSagePassiveScheduled(
			e.packet, e.sessionKey, e.generation, 3,
		)
		if sageErr != nil {
			return nil, fmt.Errorf("overdriveSageEntry: %w", sageErr)
		}
		packets = append(packets, sagePackets...)
		e.sagePoll = sagePoll
	}
	if isActive != e.isPresented {
		creature := member.projectPassiveCreature(e.creatureIndex, now)
		attributePacket, attributeErr := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
			ObjectID: e.objectID,
			Value: map[uint8]float32{
				9:  creature.EnergyDefense,
				10: creature.CriticalRating,
				23: creature.TimingProfile.AttackSpeed,
				24: creature.TimingProfile.CooldownReduction,
			},
		})
		if attributeErr != nil {
			return nil, fmt.Errorf("overdriveAttribute: %w", attributeErr)
		}
		packets = append(packets, attributePacket)
		e.isPresented = isActive
	}
	if isActive {
		e.nextDelay = min(500*time.Millisecond, remaining)
		e.isContinuing = true
	}
	e.packets = clonePendingPackets(packets)
	return packets, nil
}

// Scheduled publications use the producer's commit boundary. The action
// packet's response callback list has already been consumed before this runs.
func (e *campaignOverdriveMeterStep) commit() {
	e.runtime.registry.mutex.RLock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && member.generation == e.generation &&
		member.transportGeneration == e.identity.transportGeneration &&
		member.overdriveActivation == e.activation
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return
	}
	e.runtime.registry.queuePeerPresentation(e.identity, e.packets)
	if member.deployedObjectID == e.objectID && e.sagePoll != nil {
		e.runtime.switcher.commitSagePassive(e.sessionKey, e.generation, e.sagePoll)
	}
	if !e.isContinuing {
		return
	}
	err := e.schedule(e.nextDelay)
	if err != nil && e.runtime.logger != nil {
		e.runtime.logger.Printf("RakNet Overdrive continuation scheduling failed user=%d: %v", member.binding.UserID, err)
	}
}

func (e *gameplayPeerSession) rechargeOverdrive(
	baseEnergy uint32, now time.Time,
) (float32, bool) {
	if e == nil || baseEnergy == 0 || !e.binding.IsOverdriveUnlocked {
		return 0, false
	}
	if now.Before(e.overdriveStartsAt) && now.Before(e.overdriveExpiresAt) {
		return 0, false
	}
	creatureIndex := e.deployedCreatureIndex
	if creatureIndex >= uint32(len(e.binding.Creatures)) {
		return 0, false
	}
	bonus := max(float32(-1), e.binding.Creatures[creatureIndex].OverdriveBuildupIncrease)
	if e.isOverdriveActiveAt(now) {
		// OverdriveModifier adds 1.5 to OverdriveBuildup while active.
		bonus += 1.5
	}
	// Base charge is independent of difficulty-scaled experience rewards.
	recharge := float32(baseEnergy) * (1 + bonus)
	if recharge <= 0 {
		return 0, false
	}
	if e.isOverdriveActiveAt(now) {
		duration := campaignOverdriveDuration(*e)
		remaining := float32(e.overdriveExpiresAt.Sub(now)) /
			float32(duration) * float32(campaignOverdriveMaximumEnergy)
		energy := min(float32(campaignOverdriveMaximumEnergy), remaining+recharge)
		e.overdriveExpiresAt = now.Add(time.Duration(
			float64(duration) * float64(energy) / campaignOverdriveMaximumEnergy,
		))
		return energy, true
	}
	if e.overdriveEnergy >= float32(campaignOverdriveMaximumEnergy) {
		return 0, false
	}
	e.overdriveEnergy = min(
		float32(campaignOverdriveMaximumEnergy), e.overdriveEnergy+recharge,
	)
	if e.overdriveEnergy >= float32(campaignOverdriveMaximumEnergy) {
		e.isOverdriveSpent = false
	}
	return e.overdriveEnergy, true
}

func (e gameplayPeerSession) presentedOverdriveEnergy(now time.Time) float32 {
	if !e.binding.IsOverdriveUnlocked {
		return 0
	}
	if !e.isOverdriveActiveAt(now) {
		// Do not publish an empty meter between accepting the command and its
		// authored 0.3-second StartOverdrive commit.
		if now.Before(e.overdriveStartsAt) && now.Before(e.overdriveExpiresAt) {
			return float32(campaignOverdriveMaximumEnergy)
		}
		return e.overdriveEnergy
	}
	duration := campaignOverdriveDuration(e)
	remaining := float32(e.overdriveExpiresAt.Sub(now)) /
		float32(duration) * float32(campaignOverdriveMaximumEnergy)
	return min(float32(campaignOverdriveMaximumEnergy), max(float32(0), remaining))
}

func campaignOverdriveDuration(peerSession gameplayPeerSession) time.Duration {
	creatureIndex := peerSession.deployedCreatureIndex
	if creatureIndex >= uint32(len(peerSession.binding.Creatures)) {
		return campaignOverdriveBaseDuration()
	}
	duration, err := game.ResolveOverdriveDuration(
		campaignOverdriveMaximumEnergy, campaignOverdriveBaseDrainPerSecond,
		peerSession.binding.Creatures[creatureIndex].OverdriveDurationIncrease,
	)
	if err != nil {
		return campaignOverdriveBaseDuration()
	}
	return duration
}

func campaignOverdriveBaseDuration() time.Duration {
	duration, err := game.ResolveOverdriveDuration(
		campaignOverdriveMaximumEnergy, campaignOverdriveBaseDrainPerSecond, 0,
	)
	if err != nil {
		return 6 * time.Second
	}
	return duration
}
