package gameplay

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const (
	energySentinelSearchRadius        = 16
	energySentinelInitialDelay        = time.Second
	energySentinelTargetSwitchDelay   = 4 * time.Second
	energySentinelDamageTakenIncrease = 0.15
)

type energySentinelPassiveRun struct {
	mutex          sync.Mutex
	cancel         raknet.CancelSchedule
	modifier       *campaignNPCModifierRun
	modifierPool   *modifierPool
	npc            *zonenpc.Session
	sourceObjectID uint32
	targetObjectID uint32
	isStopped      bool
}

func (e *energySentinelPassiveRun) Stop() {
	if e == nil {
		return
	}
	e.mutex.Lock()
	if e.isStopped {
		e.mutex.Unlock()
		return
	}
	e.isStopped = true
	cancel := e.cancel
	e.cancel = nil
	modifier := e.modifier
	e.modifier = nil
	targetObjectID := e.targetObjectID
	e.targetObjectID = 0
	e.mutex.Unlock()
	if cancel != nil {
		cancel()
	}
	if e.npc != nil {
		e.npc.ClearDamageVulnerability(targetObjectID, e.sourceObjectID)
	}
	if modifier != nil && e.modifierPool != nil {
		_, _ = modifier.release(e.modifierPool)
	}
}

func (e *energySentinelPassiveRun) setCancel(cancel raknet.CancelSchedule) bool {
	if e == nil {
		if cancel != nil {
			cancel()
		}
		return false
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.isStopped {
		if cancel != nil {
			cancel()
		}
		return false
	}
	e.cancel = cancel
	return true
}

type energySentinelPassiveRuntime struct {
	registry     *gameplaySessionRegistry
	modifierPool *modifierPool
	now          func() time.Time
}

type energySentinelPassiveSchedule struct {
	runtime    energySentinelPassiveRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	objectID   uint32
	creatureID uint32
	sourceTime uint64
	startedAt  time.Time
	run        *energySentinelPassiveRun
}

func (e energySentinelPassiveSchedule) schedule(delay time.Duration) error {
	producer := raknet.ScheduledPacketProducer{Delay: delay, Produce: e.rotate}
	cancel, err := e.packet.ScheduleProducers([]raknet.ScheduledPacketProducer{producer})
	if err != nil {
		return fmt.Errorf("energyPassiveSchedule: %w", err)
	}
	e.run.setCancel(cancel)
	return nil
}

func (e energySentinelPassiveSchedule) currentTarget() (
	*zonenpc.Session, uint32, bool, error,
) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.deployedObjectID == e.objectID &&
		peerSession.deployedCreatureIndex == e.creatureID &&
		peerSession.energySentinelPassive == e.run && peerSession.zone != nil &&
		peerSession.zone.NPCs() != nil && peerSession.zone.Population() != nil &&
		peerSession.zone.Population().Random() != nil
	if !isCurrent {
		e.runtime.registry.mutex.RUnlock()
		return nil, 0, false, nil
	}
	npcSession := peerSession.zone.NPCs()
	targets, err := npcSession.ActorsInSphere(zonenpc.SphereRequest{
		Center: game.Vec3(peerSession.playerPosition), Radius: energySentinelSearchRadius,
	})
	if err != nil {
		e.runtime.registry.mutex.RUnlock()
		return nil, 0, false, fmt.Errorf("energyPassiveTargets: %w", err)
	}
	if len(targets) == 0 {
		e.runtime.registry.mutex.RUnlock()
		return npcSession, 0, true, nil
	}
	targetIndex, err := peerSession.zone.Population().Random().Index(uint32(len(targets)))
	e.runtime.registry.mutex.RUnlock()
	if err != nil {
		return nil, 0, false, fmt.Errorf("energyPassiveTarget: %w", err)
	}
	return npcSession, targets[targetIndex].Plan.ObjectID, true, nil
}

func (e energySentinelPassiveSchedule) rotate() ([][]byte, error) {
	npcSession, targetObjectID, isCurrent, err := e.currentTarget()
	if err != nil {
		return nil, err
	}
	if !isCurrent {
		return nil, nil
	}
	packets, err := e.replaceTarget(npcSession, targetObjectID)
	if err != nil {
		return nil, err
	}
	err = e.schedule(energySentinelTargetSwitchDelay)
	if err != nil {
		return nil, fmt.Errorf("energyPassiveRetarget: %w", err)
	}
	return packets, nil
}

func (e energySentinelPassiveSchedule) replaceTarget(
	npcSession *zonenpc.Session, targetObjectID uint32,
) ([][]byte, error) {
	e.run.mutex.Lock()
	defer e.run.mutex.Unlock()
	if e.run.isStopped {
		return nil, nil
	}
	packets := make([][]byte, 0, 2)
	if e.run.targetObjectID != 0 {
		npcSession.ClearDamageVulnerability(e.run.targetObjectID, e.objectID)
		deletePacket, err := effectraknet.ModifierDelete(
			e.run.targetObjectID, e.run.modifier.instanceID,
		)
		if err != nil {
			return nil, fmt.Errorf("energyPassiveDelete: %w", err)
		}
		packets = append(packets, deletePacket)
		_, err = e.run.modifier.release(e.runtime.modifierPool)
		if err != nil {
			return nil, fmt.Errorf("energyPassiveRelease: %w", err)
		}
		e.run.modifier = nil
		e.run.targetObjectID = 0
	}
	if targetObjectID == 0 {
		return packets, nil
	}
	modifier, err := newCampaignNPCModifierRun(e.runtime.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("energyPassiveModifier: %w", err)
	}
	err = npcSession.ApplyDamageVulnerability(
		targetObjectID, e.objectID, energySentinelDamageTakenIncrease,
	)
	if err != nil {
		_, _ = modifier.release(e.runtime.modifierPool)
		return nil, fmt.Errorf("energyPassiveApply: %w", err)
	}
	modifier.create()
	timestamp := e.sourceTime + uint64(e.runtime.now().Sub(e.startedAt)/time.Millisecond)
	createPacket, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
		SourceObjectID: e.objectID, TargetObjectID: targetObjectID,
		ModifierID: util.HashID("EnergySentinelVulnerability"),
		InstanceID: modifier.instanceID, Duration: energySentinelTargetSwitchDelay,
		Timestamp: timestamp,
	})
	if err != nil {
		npcSession.ClearDamageVulnerability(targetObjectID, e.objectID)
		_, _ = modifier.release(e.runtime.modifierPool)
		return nil, fmt.Errorf("energyPassiveCreate: %w", err)
	}
	e.run.npc = npcSession
	e.run.modifier = modifier
	e.run.targetObjectID = targetObjectID
	return append(packets, createPacket), nil
}

func (e energySentinelPassiveRuntime) start(
	packet raknet.Packet, sessionKey string, generation uint64,
) error {
	if e.registry == nil || e.modifierPool == nil || e.now == nil {
		return errors.New("energy passive runtime unavailable")
	}
	e.registry.mutex.Lock()
	peerSession, isFound := e.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.deployedCreatureIndex < uint32(len(peerSession.binding.Creatures))
	if !isCurrent {
		e.registry.mutex.Unlock()
		return nil
	}
	creature := peerSession.binding.Creatures[peerSession.deployedCreatureIndex]
	if creature.PassiveAbility != util.HashID("EnergySentinelPassive") ||
		peerSession.energySentinelPassive != nil {
		e.registry.mutex.Unlock()
		return nil
	}
	run := &energySentinelPassiveRun{
		modifierPool: e.modifierPool, sourceObjectID: peerSession.deployedObjectID,
	}
	peerSession.energySentinelPassive = run
	objectID := peerSession.deployedObjectID
	creatureID := peerSession.deployedCreatureIndex
	e.registry.sessions[sessionKey] = peerSession
	e.registry.mutex.Unlock()
	schedule := energySentinelPassiveSchedule{
		runtime: e, packet: packet, sessionKey: sessionKey, generation: generation,
		objectID: objectID, creatureID: creatureID, sourceTime: packet.SourceTime,
		startedAt: e.now(), run: run,
	}
	err := schedule.schedule(energySentinelInitialDelay)
	if err == nil {
		return nil
	}
	run.Stop()
	e.registry.mutex.Lock()
	latest, isLatestFound := e.registry.sessions[sessionKey]
	if isLatestFound && latest.generation == generation &&
		latest.energySentinelPassive == run {
		latest.energySentinelPassive = nil
		e.registry.sessions[sessionKey] = latest
	}
	e.registry.mutex.Unlock()
	return fmt.Errorf("energyPassiveStart: %w", err)
}
