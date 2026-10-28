package gameplay

import (
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	actionraknet "github.com/darkspinnet/darkspin/server/zone/action/raknet103"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type arenaQuantumRun struct {
	runtime            campaignAbilityCommandRuntime
	packet             raknet.Packet
	sessionKey         string
	gameID             uint32
	generation         uint64
	objectID           uint32
	abilityID          uint32
	index              uint32
	syncStamp          uint8
	creature           game.GameplayCreature
	definition         sim.AbilityDefinition
	damage             game.DamageRange
	blink              *heroQuantumBlinkRun
	targetObjectIDs    []uint32
	initialTargetIndex int
	initialDestination raknet.Vector3
	slideDelay         time.Duration
	releaseDelay       time.Duration
	releaseReservation zoneaction.ReleaseReservation
	nextTargetIndex    int
}

type arenaQuantumStep struct {
	run      *arenaQuantumRun
	index    uint32
	deadline time.Duration
}

func (e *gameplaySessionRegistry) retireArenaQuantumBlinksLocked(gameID uint32) {
	for key, member := range e.sessions {
		if member.binding.Mode != game.ModeArena || member.binding.GameID != gameID || member.heroQuantumBlink == nil {
			continue
		}
		if member.deployedHitPoint() <= 0 {
			member.heroQuantumBlink.Stop()
			member.heroQuantumBlink = nil
		} else {
			err := member.retireQuantumBlinkLocked(e.now())
			if err != nil && e.logger != nil {
				e.logger.Printf("Arena Quantum Blink retirement failed owner=%d: %v", member.deployedObjectID, err)
			}
		}
		e.sessions[key] = member
	}
}

func (e *gameplaySessionRegistry) retireArenaQuantumBlinkForOwnerLocked(gameID uint32, objectID uint32) {
	for key, member := range e.sessions {
		if member.binding.Mode != game.ModeArena || member.binding.GameID != gameID ||
			member.deployedObjectID != objectID || member.heroQuantumBlink == nil {
			continue
		}
		if member.deployedHitPoint() <= 0 {
			member.heroQuantumBlink.Stop()
			member.heroQuantumBlink = nil
		} else {
			err := member.retireQuantumBlinkLocked(e.now())
			if err != nil && e.logger != nil {
				e.logger.Printf("Arena Quantum Blink owner retirement failed owner=%d: %v", objectID, err)
			}
		}
		e.sessions[key] = member
		return
	}
}

func (e arenaQuantumStep) produce() ([][]byte, error) {
	return e.run.strike(e.index, e.deadline)
}

func (e *arenaQuantumRun) currentLocked() (gameplayPeerSession, bool) {
	source, isFound := e.runtime.registry.sessions[e.sessionKey]
	match := e.runtime.registry.arenaMatches[e.gameID]
	isCurrent := isFound && !e.runtime.registry.isClosed && match != nil && match.isCombatActive &&
		source.generation == e.generation && source.binding.Mode == game.ModeArena &&
		source.binding.GameID == e.gameID && source.isArenaCombatActive && source.stage.IsDungeon() &&
		source.deployedObjectID == e.objectID && source.deployedHitPoint() > 0 &&
		source.heroQuantumBlink == e.blink
	return source, isCurrent
}

func (e *arenaQuantumRun) slide() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	_, isCurrent := e.currentLocked()
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return nil, nil
	}
	packet, err := npcraknet.AnimationState(e.objectID, e.definition.SlideAnimationName,
		e.packet.SourceTime+uint64(e.definition.HitDelay/time.Millisecond))
	if err != nil {
		return nil, fmt.Errorf("arenaQuantumSlide: %w", err)
	}
	return [][]byte{packet}, nil
}

func (e *arenaQuantumRun) targetLocked(index uint32, targets *arenaAbilityTargets) (zonenpc.Snapshot, bool) {
	if len(e.targetObjectIDs) == 0 || e.initialTargetIndex < 0 {
		return zonenpc.Snapshot{}, false
	}
	start := e.nextTargetIndex
	count := len(e.targetObjectIDs)
	if index == 0 {
		start = e.initialTargetIndex
		count = 1
	}
	for offset := range count {
		candidateIndex := (start + offset) % len(e.targetObjectIDs)
		target, isFound := targets.NPC(e.targetObjectIDs[candidateIndex])
		if !isFound || target.IsDefeated || target.HitPoint <= 0 {
			continue
		}
		if index > 0 {
			e.nextTargetIndex = (candidateIndex + 1) % len(e.targetObjectIDs)
		}
		return target, true
	}
	return zonenpc.Snapshot{}, false
}

func (e *arenaQuantumRun) finishAt(deadline time.Duration) ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	source, isCurrent := e.currentLocked()
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	releasePacket, err := abilityraknet.ReleaseResponse(e.syncStamp, e.abilityID,
		e.index, e.packet.SourceTime, e.slideDelay, deadline)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaQuantumRelease: %w", err)
	}
	packets := make([][]byte, 0, 4)
	if e.blink.isPositionSaved {
		teleportPackets, teleportErr := actionraknet.Teleport(actionraknet.TeleportRequest{
			ObjectID: e.objectID, Position: game.Vec3(e.initialDestination), Orientation: e.blink.orientation,
		})
		if teleportErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaQuantumReturnPacket: %w", teleportErr)
		}
		packets = append(packets, teleportPackets...)
	}
	resetPacket, err := npcraknet.ResetAnimation(e.objectID,
		e.packet.SourceTime+uint64(deadline/time.Millisecond))
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaQuantumReset: %w", err)
	}
	packets = append(packets, resetPacket)
	if e.blink.isPositionSaved {
		err = source.teleportPlayer(e.runtime.now(), e.initialDestination)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaQuantumReturn: %w", err)
		}
	}
	source.abilityReleaseSession().Complete(e.releaseReservation, e.blink.startedAt.Add(deadline))
	source.heroQuantumBlink = nil
	e.blink.mutex.Lock()
	e.blink.cancel = nil
	e.blink.isStopped = true
	e.blink.mutex.Unlock()
	e.runtime.registry.sessions[e.sessionKey] = source
	e.runtime.registry.mutex.Unlock()
	return append(packets, releasePacket), nil
}

func (e *arenaQuantumRun) finish() ([][]byte, error) {
	return e.finishAt(e.releaseDelay)
}

func (e *arenaQuantumRun) strike(index uint32, deadline time.Duration) ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	source, isCurrent := e.currentLocked()
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	timestamp := e.packet.SourceTime + uint64(deadline/time.Millisecond)
	targets := e.runtime.registry.abilityTargetsLocked(e.runtime, source, e.definition, timestamp).(*arenaAbilityTargets)
	target, isTargetFound := e.targetLocked(index, targets)
	if index > 0 && !isTargetFound {
		e.runtime.registry.mutex.Unlock()
		return e.finishAt(deadline)
	}
	destination := e.initialDestination
	if index > 0 && isTargetFound {
		destination = raknet.Vector3(target.Plan.Position)
	}
	match := e.runtime.registry.arenaMatches[e.gameID]
	err := zonenavigation.ValidateTeleport(match.navigation, game.Vec3(destination), source.deployedCampaignNavigationRadius())
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaQuantumLanding: %w", err)
	}
	orientation := e.blink.orientation
	if index > 0 {
		yaw := match.random.Float64()*2*math.Pi - math.Pi/2
		orientation = game.Quaternion{Z: float32(math.Sin(yaw / 2)), W: float32(math.Cos(yaw / 2))}
	}
	teleportPackets, err := actionraknet.Teleport(actionraknet.TeleportRequest{
		ObjectID: e.objectID, Position: game.Vec3(destination), Orientation: orientation,
	})
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaQuantumTeleportPacket: %w", err)
	}
	if index > 0 {
		animationIndex, indexErr := match.random.Index(uint32(len(e.definition.AnimationNames)))
		if indexErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaQuantumPoseIndex: %w", indexErr)
		}
		animationPacket, animationErr := npcraknet.AnimationState(e.objectID,
			e.definition.AnimationNames[animationIndex], timestamp)
		if animationErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaQuantumPose: %w", animationErr)
		}
		teleportPackets = append(teleportPackets, animationPacket)
	}
	err = source.teleportPlayer(e.runtime.now(), destination)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaQuantumTeleport: %w", err)
	}
	if index == 0 {
		e.blink.isPositionSaved = true
	}
	e.blink.orientation = orientation
	e.runtime.registry.sessions[e.sessionKey] = source
	if !isTargetFound {
		e.runtime.registry.mutex.Unlock()
		if index == 0 {
			returnPackets, finishErr := e.finishAt(deadline)
			return append(teleportPackets, returnPackets...), finishErr
		}
		return teleportPackets, nil
	}
	plan := zoneability.AreaPlan{SourceObjectID: e.objectID,
		AbilityID: e.abilityID, Definition: e.definition, Damage: e.damage,
		Center: game.Vec3(destination), Target: []zonenpc.Snapshot{target}}
	results, err := zoneability.CommitArea(match.random, targets, plan, e.creature,
		source.binding.Difficulty, e.runtime.program.Critical)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		targets.finish()
		if e.runtime.logger != nil {
			e.runtime.logger.Printf("Arena Quantum Blink damage failed after teleport owner=%d: %v", e.objectID, err)
		}
		return teleportPackets, nil
	}
	packets := append([][]byte(nil), teleportPackets...)
	for _, result := range results {
		effectPacket, effectErr := raknet.MarshalApplication(raknet.ServerEventMessage{
			Asset: util.HashID(e.definition.HitEffectName), ObjectID: result.Damage.ObjectID,
			Position: raknet.Vector3(result.Snapshot.Plan.Position),
		})
		if effectErr == nil {
			packets = append(packets, effectPacket)
		} else if e.runtime.logger != nil {
			e.runtime.logger.Printf("Arena Quantum Blink hit effect failed target=%d: %v", result.Damage.ObjectID, effectErr)
		}
	}
	hitPackets, err := targets.packets(results)
	if err == nil {
		packets = append(packets, hitPackets...)
	} else if e.runtime.logger != nil {
		e.runtime.logger.Printf("Arena Quantum Blink hit publication failed owner=%d: %v", e.objectID, err)
	}
	e.runtime.registry.mutex.Unlock()
	targets.finish()
	return packets, nil
}

func (e *arenaQuantumRun) fail(cause error) {
	e.runtime.registry.mutex.Lock()
	source, isCurrent := e.currentLocked()
	if isCurrent {
		cleanupErr := source.retireQuantumBlinkLocked(e.runtime.now())
		if cleanupErr != nil && e.runtime.logger != nil {
			e.runtime.logger.Printf("Arena Quantum Blink cleanup failed owner=%d: %v", e.objectID, cleanupErr)
		}
		e.runtime.registry.sessions[e.sessionKey] = source
	}
	e.runtime.registry.mutex.Unlock()
	if cause != nil && e.runtime.logger != nil {
		e.runtime.logger.Printf("Arena Quantum Blink producer failed owner=%d: %v", e.objectID, cause)
	}
}
