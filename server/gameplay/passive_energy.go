package gameplay

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const (
	energySentinelSearchRadius      = 16
	energySentinelInitialDelay      = time.Duration(0)
	energySentinelTargetSwitchDelay = 4 * time.Second
	energySentinelPollDelay         = 500 * time.Millisecond
)

type energySentinelPassiveTarget struct {
	objectID  uint32
	modifier  *campaignNPCModifierRun
	startedAt time.Time
}

type energySentinelPassiveRun struct {
	mutex          sync.Mutex
	cancel         raknet.CancelSchedule
	targets        []energySentinelPassiveTarget
	modifierPool   *modifierPool
	npc            *zonenpc.Session
	sourceObjectID uint32
	damageIncrease float32
	packet         raknet.Packet
	isStopped      bool
}

type energySentinelCleanupStep struct {
	packets [][]byte
}

func (e energySentinelCleanupStep) produce() ([][]byte, error) {
	return e.packets, nil
}

func releaseEnergySentinelModifier(modifier *campaignNPCModifierRun, pool *modifierPool) error {
	isCreated, err := modifier.release(pool)
	if err != nil {
		return fmt.Errorf("modifierRelease: %w", err)
	}
	if !isCreated {
		// A reservation that never reached publication still needs releasing.
		return nil
	}
	return nil
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
	targets := e.targets
	e.targets = nil
	e.mutex.Unlock()
	if cancel != nil {
		cancel()
	}
	packets := make([][]byte, 0, len(targets))
	for _, target := range targets {
		if target.objectID == 0 {
			continue
		}
		if e.npc != nil {
			e.npc.ClearDamageVulnerability(target.objectID, e.sourceObjectID)
		}
		packet, marshalErr := effectraknet.ModifierDelete(target.objectID, target.modifier.instanceID)
		if marshalErr != nil {
			log.Printf("Goliath passive cleanup presentation source=%d: %v", e.sourceObjectID, marshalErr)
		} else {
			packets = append(packets, packet)
		}
		err := releaseEnergySentinelModifier(target.modifier, e.modifierPool)
		if err != nil {
			log.Printf("Goliath passive cleanup source=%d target=%d: %v", e.sourceObjectID, target.objectID, err)
		}
	}
	if len(packets) != 0 {
		step := energySentinelCleanupStep{packets: packets}
		err := e.packet.ScheduleFunc(0, step.produce)
		if err != nil {
			// A disconnected transport may already have canceled its scheduler;
			// authoritative marks and leases were still removed above.
			log.Printf("Goliath passive cleanup publication source=%d: %v", e.sourceObjectID, err)
		}
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

func (e *energySentinelPassiveRun) needsTargetSelection(
	selection energySentinelTargetSelection, now time.Time,
) bool {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.damageIncrease != selection.damageIncrease || len(e.targets) != selection.maximumTargetCount {
		return true
	}
	for _, target := range e.targets {
		if target.objectID == 0 || now.Sub(target.startedAt) >= energySentinelTargetSwitchDelay {
			return true
		}
		npc, isFound := selection.npc.NPC(target.objectID)
		if !isFound || npc.IsDefeated || npc.HitPoint <= 0 {
			return true
		}
	}
	return false
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

type energySentinelTargetSelection struct {
	npc                *zonenpc.Session
	objectIDs          []uint32
	maximumTargetCount int
	damageIncrease     float32
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

func (e energySentinelPassiveSchedule) currentTargets() (energySentinelTargetSelection, bool, error) {
	e.runtime.registry.mutex.RLock()
	defer e.runtime.registry.mutex.RUnlock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.deployedObjectID == e.objectID && peerSession.deployedCreatureIndex == e.creatureID &&
		peerSession.deployedHitPoint() > 0 && peerSession.energySentinelPassive == e.run &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil && peerSession.zone.Population() != nil &&
		peerSession.zone.Population().Random() != nil
	if !isCurrent {
		return energySentinelTargetSelection{}, false, nil
	}
	selection := energySentinelTargetSelection{npc: peerSession.zone.NPCs(), maximumTargetCount: 1, damageIncrease: 0.15}
	// Chunks263/535 rank tables: {1,1,3,3} targets, {.15,.15,.30,.30} vulnerability.
	if peerSession.isOverdriveActiveAt(e.runtime.now()) {
		selection.maximumTargetCount = 3
		selection.damageIncrease = 0.30
	}
	if !e.run.needsTargetSelection(selection, e.runtime.now()) {
		return selection, true, nil
	}
	targets, err := selection.npc.ActorsInSphere(zonenpc.SphereRequest{
		Center: game.Vec3(peerSession.playerPosition), Radius: energySentinelSearchRadius,
	})
	if err != nil {
		return energySentinelTargetSelection{}, false, fmt.Errorf("energyPassiveTargets: %w", err)
	}
	for len(targets) > 0 {
		index, drawErr := peerSession.zone.Population().Random().Index(uint32(len(targets)))
		if drawErr != nil {
			return energySentinelTargetSelection{}, false, fmt.Errorf("energyPassiveShuffle: %w", drawErr)
		}
		selection.objectIDs = append(selection.objectIDs, targets[index].Plan.ObjectID)
		targets[index] = targets[len(targets)-1]
		targets = targets[:len(targets)-1]
	}
	return selection, true, nil
}

func (e energySentinelPassiveSchedule) rotate() ([][]byte, error) {
	selection, isCurrent, err := e.currentTargets()
	if err != nil {
		return nil, fmt.Errorf("energyPassiveSelect: %w", err)
	}
	if !isCurrent {
		e.run.Stop()
		return nil, nil
	}
	packets, err := e.replaceTargets(selection)
	if err != nil {
		return nil, fmt.Errorf("energyPassiveReplace: %w", err)
	}
	err = e.schedule(energySentinelPollDelay)
	if err != nil {
		e.run.Stop()
		return nil, fmt.Errorf("energyPassiveRetarget: %w", err)
	}
	return packets, nil
}

func (e energySentinelPassiveSchedule) deleteTarget(target energySentinelPassiveTarget) ([]byte, error) {
	if target.objectID == 0 {
		return nil, nil
	}
	packet, err := effectraknet.ModifierDelete(target.objectID, target.modifier.instanceID)
	if err != nil {
		return nil, fmt.Errorf("energyPassiveDelete: %w", err)
	}
	err = releaseEnergySentinelModifier(target.modifier, e.runtime.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("energyPassiveRelease: %w", err)
	}
	e.run.npc.ClearDamageVulnerability(target.objectID, e.objectID)
	return packet, nil
}

func (e energySentinelPassiveSchedule) createTarget(objectID uint32, increase float32) (energySentinelPassiveTarget, []byte, error) {
	modifier, err := newCampaignNPCModifierRun(e.runtime.modifierPool)
	if err != nil {
		return energySentinelPassiveTarget{}, nil, fmt.Errorf("energyPassiveModifier: %w", err)
	}
	isAccepted, err := e.run.npc.TryApplyDamageVulnerability(objectID, e.objectID, increase)
	if err != nil || !isAccepted {
		releaseErr := releaseEnergySentinelModifier(modifier, e.runtime.modifierPool)
		if releaseErr != nil {
			return energySentinelPassiveTarget{}, nil, fmt.Errorf("energyPassiveRollback: %w", releaseErr)
		}
		if err != nil {
			return energySentinelPassiveTarget{}, nil, fmt.Errorf("energyPassiveApply: %w", err)
		}
		return energySentinelPassiveTarget{}, nil, nil
	}
	now := e.runtime.now()
	timestamp := e.sourceTime + uint64(now.Sub(e.startedAt)/time.Millisecond)
	packet, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
		SourceObjectID: e.objectID, TargetObjectID: objectID, ModifierID: util.HashID("EnergySentinelVulnerability"),
		InstanceID: modifier.instanceID, IsIndefinite: true, Timestamp: timestamp,
	})
	if err != nil {
		e.run.npc.ClearDamageVulnerability(objectID, e.objectID)
		releaseErr := releaseEnergySentinelModifier(modifier, e.runtime.modifierPool)
		if releaseErr != nil {
			return energySentinelPassiveTarget{}, nil, fmt.Errorf("energyPassiveCreateRollback: %w", releaseErr)
		}
		return energySentinelPassiveTarget{}, nil, fmt.Errorf("energyPassiveCreate: %w", err)
	}
	modifier.create()
	return energySentinelPassiveTarget{objectID: objectID, modifier: modifier, startedAt: now}, packet, nil
}

func (e energySentinelPassiveSchedule) replaceTargets(selection energySentinelTargetSelection) ([][]byte, error) {
	e.run.mutex.Lock()
	defer e.run.mutex.Unlock()
	if e.run.isStopped {
		return nil, nil
	}
	e.run.npc = selection.npc
	packets := make([][]byte, 0)
	// Recreate rank-dependent children on Overdrive transitions, removing extra
	// target slots on exit before any new ordinary mark is chosen.
	if e.run.damageIncrease != selection.damageIncrease {
		for index, target := range e.run.targets {
			packet, err := e.deleteTarget(target)
			if err != nil {
				return nil, fmt.Errorf("energyPassiveRankDelete: %w", err)
			}
			if packet != nil {
				packets = append(packets, packet)
			}
			e.run.targets[index] = energySentinelPassiveTarget{}
		}
	}
	e.run.damageIncrease = selection.damageIncrease
	e.run.targets = e.run.targets[:min(len(e.run.targets), selection.maximumTargetCount)]
	for len(e.run.targets) < selection.maximumTargetCount {
		e.run.targets = append(e.run.targets, energySentinelPassiveTarget{})
	}
	now := e.runtime.now()
	for index, target := range e.run.targets {
		if target.objectID != 0 {
			npc, isFound := selection.npc.NPC(target.objectID)
			if !isFound || npc.IsDefeated || npc.HitPoint <= 0 {
				packet, err := e.deleteTarget(target)
				if err != nil {
					return nil, fmt.Errorf("energyPassiveDeadDelete: %w", err)
				}
				packets = append(packets, packet)
				target = energySentinelPassiveTarget{}
				e.run.targets[index] = target
			}
		}
		if target.objectID != 0 && now.Sub(target.startedAt) < energySentinelTargetSwitchDelay {
			continue
		}
		for _, objectID := range selection.objectIDs {
			if objectID == target.objectID {
				continue
			}
			replacement, packet, err := e.createTarget(objectID, selection.damageIncrease)
			if err != nil {
				return nil, fmt.Errorf("energyPassiveTargetCreate: %w", err)
			}
			if replacement.objectID == 0 {
				continue
			}
			deletePacket, deleteErr := e.deleteTarget(target)
			if deleteErr != nil {
				rollbackPacket, rollbackErr := e.deleteTarget(replacement)
				if rollbackErr != nil {
					return nil, fmt.Errorf("energyPassiveSwapRollback: %w", rollbackErr)
				}
				if rollbackPacket == nil {
					return nil, fmt.Errorf("energyPassiveSwapEmpty: %w", deleteErr)
				}
				return nil, fmt.Errorf("energyPassiveSwapDelete: %w", deleteErr)
			}
			if deletePacket != nil {
				packets = append(packets, deletePacket)
			}
			packets = append(packets, packet)
			e.run.targets[index] = replacement
			break
		}
	}
	return packets, nil
}

func (e energySentinelPassiveRuntime) start(packet raknet.Packet, sessionKey string, generation uint64) error {
	if e.registry == nil || e.modifierPool == nil || e.now == nil {
		return errors.New("energy passive runtime unavailable")
	}
	e.registry.mutex.Lock()
	peerSession, isFound := e.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation && peerSession.deployedCreatureIndex < uint32(len(peerSession.binding.Creatures))
	if !isCurrent {
		e.registry.mutex.Unlock()
		return nil
	}
	creature := peerSession.binding.Creatures[peerSession.deployedCreatureIndex]
	if creature.PassiveAbility != util.HashID("EnergySentinelPassive") || peerSession.energySentinelPassive != nil {
		e.registry.mutex.Unlock()
		return nil
	}
	run := &energySentinelPassiveRun{
		modifierPool: e.modifierPool, sourceObjectID: peerSession.deployedObjectID,
		packet: packet.Autonomous(),
	}
	peerSession.energySentinelPassive = run
	objectID := peerSession.deployedObjectID
	creatureID := peerSession.deployedCreatureIndex
	e.registry.sessions[sessionKey] = peerSession
	e.registry.mutex.Unlock()
	schedule := energySentinelPassiveSchedule{runtime: e, packet: packet, sessionKey: sessionKey, generation: generation,
		objectID: objectID, creatureID: creatureID, sourceTime: packet.SourceTime, startedAt: e.now(), run: run}
	err := schedule.schedule(energySentinelInitialDelay)
	if err == nil {
		return nil
	}
	run.Stop()
	e.registry.mutex.Lock()
	latest, isLatestFound := e.registry.sessions[sessionKey]
	if isLatestFound && latest.generation == generation && latest.energySentinelPassive == run {
		latest.energySentinelPassive = nil
		e.registry.sessions[sessionKey] = latest
	}
	e.registry.mutex.Unlock()
	return fmt.Errorf("energyPassiveStart: %w", err)
}
