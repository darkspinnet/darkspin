package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

// activateTCShield follows chunk747 Activate -> GenerateShield. A broken shield
// continues to use the separate delayed recharge path in absorbTCShield.
func (e *gameplayPeerSession) activateTCShield(
	creatureIndex uint32, now time.Time, effectPool *attachedEffectPool, registry *gameplaySessionRegistry,
) ([][]byte, error) {
	packets, err := e.generateTCShield(creatureIndex, now, effectPool)
	if err != nil {
		return nil, fmt.Errorf("tcShieldActivate: %w", err)
	}
	if len(packets) == 0 {
		return nil, nil
	}
	previous := e.tcShieldRuns[creatureIndex]
	if previous != nil && previous.cancel != nil {
		previous.cancel()
	}
	e.tcShieldRuns[creatureIndex] = &tcShieldPassiveRun{
		registry: registry, effectPool: effectPool, memberKey: gameplaySessionMemberKey(*e),
		generation: e.generation, objectID: e.deployedObjectID, creatureIndex: creatureIndex,
	}
	return packets, nil
}

func (e *gameplayPeerSession) generateTCShield(
	creatureIndex uint32, now time.Time, effectPool *attachedEffectPool,
) ([][]byte, error) {
	if e == nil || now.IsZero() || creatureIndex >= uint32(len(e.binding.Creatures)) ||
		creatureIndex >= uint32(len(e.tcShieldAmount)) ||
		e.binding.Creatures[creatureIndex].PassiveAbility != util.HashID("TCShieldedSentinelPassive") {
		return nil, nil
	}
	baseStrength := tcShieldBaseStrength
	if e.isOverdriveActiveAt(now) {
		baseStrength = 30
	}
	shieldStrength := baseStrength +
		(e.binding.Creatures[creatureIndex].EnergyDefense-tcShieldDefenseOffset)*tcShieldDefenseScale
	generationPacket, err := raknet.MarshalApplication(raknet.ObjectEffectMessage{
		Asset: util.HashID("cyber_shield_generate_shield.ServerEventDef"), ObjectID: e.deployedObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("tcShieldActivation: %w", err)
	}
	shieldPacket, err := e.attachTCShield(creatureIndex, effectPool)
	if err != nil {
		return nil, fmt.Errorf("tcShieldInitialAttach: %w", err)
	}
	e.tcShieldAmount[creatureIndex] = shieldStrength
	e.tcShieldReadyAt[creatureIndex] = time.Time{}
	packets := [][]byte{generationPacket}
	if shieldPacket != nil {
		packets = append(packets, shieldPacket)
	}
	return packets, nil
}

type tcShieldPassiveRun struct {
	registry      *gameplaySessionRegistry
	effectPool    *attachedEffectPool
	memberKey     gameplayMemberKey
	generation    uint64
	objectID      uint32
	creatureIndex uint32
	cancel        func()
}

type tcShieldRechargeStep struct {
	run     *tcShieldPassiveRun
	readyAt time.Time
}

func (e *gameplayPeerSession) resetTCShieldTransport() {
	for index, run := range e.tcShieldRuns {
		if run != nil && run.effectPool != nil && e.isTCShieldEffectAttached[index] {
			run.effectPool.Release(run.objectID, e.tcShieldEffectSlot[index])
		}
		e.isTCShieldEffectAttached[index] = false
		e.tcShieldEffectSlot[index] = 0
	}
}

func (e *gameplayPeerSession) scheduleTCShieldRecharge() error {
	index := e.deployedCreatureIndex
	if index >= uint32(len(e.tcShieldRuns)) || e.tcShieldReadyAt[index].IsZero() {
		return nil
	}
	run := e.tcShieldRuns[index]
	if run == nil || run.registry == nil || run.registry.timer == nil {
		return nil
	}
	if run.cancel != nil {
		run.cancel()
	}
	step := tcShieldRechargeStep{run: run, readyAt: e.tcShieldReadyAt[index]}
	cancel, err := run.registry.timer.Schedule(max(time.Duration(0), step.readyAt.Sub(run.registry.now())), step.execute)
	if err != nil {
		return fmt.Errorf("tcShieldTimer: %w", err)
	}
	run.cancel = cancel
	return nil
}

func (e tcShieldRechargeStep) execute() {
	run := e.run
	registry := run.registry
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	for sessionKey, member := range registry.sessions {
		if gameplaySessionMemberKey(member) != run.memberKey || member.generation != run.generation {
			continue
		}
		packets, err := e.recharge(&member)
		if err != nil {
			if registry.logger != nil {
				registry.logger.Printf("RakNet Titan shield recharge failed user=%d: %v", run.memberKey.userID, err)
			}
			return
		}
		registry.sessions[sessionKey] = member
		e.publish(member, packets)
		return
	}
	retained := registry.retainedSessions[run.memberKey]
	if retained == nil || retained.peerSession.generation != run.generation {
		return
	}
	packets, err := e.recharge(&retained.peerSession)
	if err != nil {
		if registry.logger != nil {
			registry.logger.Printf("RakNet retained Titan shield recharge failed user=%d: %v", run.memberKey.userID, err)
		}
		return
	}
	e.publish(retained.peerSession, packets)
}

func (e tcShieldRechargeStep) recharge(member *gameplayPeerSession) ([][]byte, error) {
	run := e.run
	index := run.creatureIndex
	if member.isZoneTerminal() || member.deployedObjectID != run.objectID ||
		member.deployedCreatureIndex != index || member.deployedHitPoint() <= 0 ||
		member.tcShieldRuns[index] != run || member.tcShieldAmount[index] > 0 ||
		!member.tcShieldReadyAt[index].Equal(e.readyAt) {
		return nil, nil
	}
	packets, err := member.generateTCShield(index, run.registry.now(), run.effectPool)
	if err != nil {
		return nil, fmt.Errorf("tcShieldRecharge: %w", err)
	}
	run.cancel = nil
	return packets, nil
}

// publish runs under the registry lock and follows the current member endpoint.
// The world timer therefore survives a retained disconnect without using an old
// transport producer; reconnect receives the current shield presentation.
func (e tcShieldRechargeStep) publish(owner gameplayPeerSession, packets [][]byte) {
	if len(packets) == 0 {
		return
	}
	for sessionKey, member := range e.run.registry.sessions {
		if member.binding.GameID != owner.binding.GameID || member.isZoneTerminal() ||
			member.zone != owner.zone || !member.stage.IsDungeon() || member.isRejoinPending {
			continue
		}
		if member.zone != nil {
			err := member.queueCampaignPresentation(packets)
			if err != nil && e.run.registry.logger != nil {
				e.run.registry.logger.Printf("RakNet Titan shield projection failed user=%d: %v", member.binding.UserID, err)
			}
		} else {
			member.queuePackets(packets)
		}
		e.run.registry.sessions[sessionKey] = member
	}
}

func (e *gameplaySessionRegistry) tcShieldReplay(
	viewer gameplayPeerSession, effectPool *attachedEffectPool,
) ([][]byte, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	packets := make([][]byte, 0)
	for sessionKey, member := range e.sessions {
		if member.binding.GameID != viewer.binding.GameID || member.zone != viewer.zone {
			continue
		}
		packet, err := member.tcShieldSnapshot(effectPool)
		if err != nil {
			return nil, fmt.Errorf("tcShieldPeerSnapshot: %w", err)
		}
		e.sessions[sessionKey] = member
		if packet != nil {
			packets = append(packets, packet)
		}
	}
	for _, retained := range e.retainedSessions {
		member := &retained.peerSession
		if member.binding.GameID != viewer.binding.GameID || member.zone != viewer.zone {
			continue
		}
		packet, err := member.tcShieldSnapshot(effectPool)
		if err != nil {
			return nil, fmt.Errorf("tcShieldRetainedSnapshot: %w", err)
		}
		if packet != nil {
			packets = append(packets, packet)
		}
	}
	return packets, nil
}

func (e *gameplayPeerSession) tcShieldSnapshot(effectPool *attachedEffectPool) ([]byte, error) {
	index := e.deployedCreatureIndex
	if e.isZoneTerminal() || e.deployedHitPoint() <= 0 ||
		index >= uint32(len(e.tcShieldAmount)) || e.tcShieldAmount[index] <= 0 {
		return nil, nil
	}
	if !e.isTCShieldEffectAttached[index] {
		packet, err := e.attachTCShield(index, effectPool)
		if err != nil {
			return nil, fmt.Errorf("tcShieldRestoreAttach: %w", err)
		}
		return packet, nil
	}
	packet, err := npcraknet.ShieldEffectAsset(
		e.deployedObjectID, e.tcShieldEffectSlot[index], "status_shielded.ServerEventDef", false,
	)
	if err != nil {
		return nil, fmt.Errorf("tcShieldSnapshot: %w", err)
	}
	return packet, nil
}
