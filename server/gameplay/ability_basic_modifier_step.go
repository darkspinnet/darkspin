package gameplay

import (
	"fmt"
	"sort"
	"time"

	"github.com/darkspinnet/darkspin/server/sim"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

func (e basicHitModifierStep) execute() {
	packets, err := e.produce()
	if err != nil {
		e.runtime.logger.Printf("Basic hit modifier target=%d kind=%d: %v", e.key.targetID, e.key.modifier.Kind, err)
	}
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	for sessionKey, peerSession := range e.runtime.registry.sessions {
		if peerSession.zone == nil || peerSession.zone.NPCs() != e.npc {
			continue
		}
		peerSession.queueCampaignPackets(packets)
		e.runtime.registry.sessions[sessionKey] = peerSession
	}
}

func (e basicHitModifierStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	if e.runtime.registry.basicHitModifiers[e.npc][e.key] != e.run {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	now := e.runtime.npc.now()
	state := e.npc.BasicModifier(e.key.targetID, e.key.modifier)
	if state != e.run.state {
		packets, err := e.runtime.registry.retireBasicHitModifierLocked(e.npc, e.effect, e.key, e.runtime.npc.modifierPool)
		e.runtime.registry.mutex.Unlock()
		if err != nil {
			return nil, fmt.Errorf("basicRetire: %w", err)
		}
		return packets, nil
	}
	packets := make([][]byte, 0)
	if e.run.silence != nil && !now.Before(state.ActiveUntil) {
		packet, err := effectraknet.ModifierDelete(e.key.targetID, e.run.silence.instanceID)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("basicSilenceDelete: %w", err)
		}
		isCreated, err := e.run.silence.release(e.runtime.npc.modifierPool)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("basicSilenceRelease: %w", err)
		}
		if isCreated {
			packets = append(packets, packet)
		}
		e.run.silence = nil
	}
	if e.run.isMovementPending {
		e.run.isMovementPending = false
		moved, isMovedFound := e.npc.NPC(e.key.targetID)
		if isMovedFound && moved.ActionGeneration == e.run.movementGeneration && moved.PositionRevision == e.run.movementRevision {
			packet, err := npcraknet.ResetAnimation(e.key.targetID, e.run.movementTimestamp)
			if err != nil {
				e.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("basicKnockbackReset: %w", err)
			}
			packets = append(packets, packet)
		}
	}
	if e.key.modifier.Kind != zonenpc.BasicModifierBurn {
		if !now.Before(state.ExpiresAt) {
			deleted, err := e.runtime.registry.retireBasicHitModifierLocked(e.npc, e.effect, e.key, e.runtime.npc.modifierPool)
			e.runtime.registry.mutex.Unlock()
			if err != nil {
				return nil, fmt.Errorf("basicExpire: %w", err)
			}
			return append(packets, deleted...), nil
		}
		e.run.timer = time.AfterFunc(e.nextDelay(now), e.execute)
		e.runtime.registry.mutex.Unlock()
		return packets, nil
	}
	if !now.Before(state.ExpiresAt) && e.run.tickCount != 7 {
		deleted, err := e.runtime.registry.retireBasicHitModifierLocked(e.npc, e.effect, e.key, e.runtime.npc.modifierPool)
		e.runtime.registry.mutex.Unlock()
		if err != nil {
			return nil, fmt.Errorf("basicBurnLate: %w", err)
		}
		return append(packets, deleted...), nil
	}
	target, isFound := e.npc.NPC(e.key.targetID)
	if !isFound || target.IsDefeated {
		e.runtime.registry.mutex.Unlock()
		return packets, nil
	}
	// CasterUnique burn retains the initiator's creation-time damage snapshot.
	sessionKey, peerSession := e.runtime.registry.basicModifierOwnerLocked(e.npc, e.run.binding.UserID)
	damage := e.run.damage.Minimum
	if sessionKey != "" {
		selected, err := sim.SelectRankDamage(peerSession.zone.NPCRandom(), sim.DamageRange{Minimum: e.run.damage.Minimum, Maximum: e.run.damage.Maximum})
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("basicBurnSelect: %w", err)
		}
		damage = selected
	}
	result, err := e.npc.Hit(zonenpc.HitRequest{SourceObjectID: e.run.sourceID, TargetObjectID: e.key.targetID, Damage: damage, Metadata: zonenpc.DamageMetadata{DamageSource: 1, DamageType: 3, IsDamageTypeKnown: true, DescriptorMask: 36}})
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("basicBurnHit: %w", err)
	}
	e.run.tickCount++
	transition := campaignDamageTransition{}
	if sessionKey != "" {
		transition, err = peerSession.applyCampaignDamageTransitionWithKill(result, peerSession.binding.UserID == e.run.binding.UserID)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("basicBurnTransition: %w", err)
		}
		e.runtime.registry.sessions[sessionKey] = peerSession
	}
	if e.run.tickCount >= 8 || result.IsDefeated {
		deleted, deleteErr := e.runtime.registry.retireBasicHitModifierLocked(e.npc, e.effect, e.key, e.runtime.npc.modifierPool)
		if deleteErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("basicBurnExpire: %w", deleteErr)
		}
		packets = append(packets, deleted...)
	} else {
		e.run.timer = time.AfterFunc(e.nextDelay(now), e.execute)
	}
	timestamp := e.run.timestamp + uint64(e.run.tickCount)*500
	e.runtime.registry.mutex.Unlock()
	if sessionKey == "" {
		return packets, nil
	}
	published, err := e.runtime.publishAreaResults(e.run.packet, sessionKey, peerSession.generation, e.run.sourceID, timestamp, e.run.binding, []zoneability.AreaResult{{Snapshot: target, Damage: result}}, []campaignDamageTransition{transition}, nil, false)
	if err != nil {
		return nil, fmt.Errorf("basicBurnPublish: %w", err)
	}
	return append(packets, published...), nil
}

func (e *gameplaySessionRegistry) basicModifierOwnerLocked(npc *zonenpc.Session, userID uint64) (string, gameplayPeerSession) {
	keys := make([]string, 0)
	for sessionKey, peerSession := range e.sessions {
		if peerSession.zone != nil && peerSession.zone.NPCs() == npc {
			keys = append(keys, sessionKey)
		}
	}
	sort.Strings(keys)
	for _, sessionKey := range keys {
		peerSession := e.sessions[sessionKey]
		if peerSession.binding.UserID == userID {
			return sessionKey, peerSession
		}
	}
	if len(keys) > 0 {
		return keys[0], e.sessions[keys[0]]
	}
	return "", gameplayPeerSession{}
}

func (e campaignDamageRuntime) retireBasicHitModifiers(sessionKey string, generation uint64, result zonenpc.DamageResult) ([][]byte, error) {
	if !result.IsDefeated {
		return nil, nil
	}
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	peerSession, isFound := e.registry.sessions[sessionKey]
	if !isFound || peerSession.generation != generation || peerSession.zone == nil {
		return nil, nil
	}
	return e.registry.retireTargetBasicModifiersLocked(peerSession, result.ObjectID, e.npc.modifierPool, false)
}

func (e *gameplaySessionRegistry) retireTargetBasicModifiersLocked(peerSession gameplayPeerSession, targetID uint32, pool *modifierPool, isCleanse bool) ([][]byte, error) {
	packets := make([][]byte, 0)
	for key, run := range e.basicHitModifiers[peerSession.zone.NPCs()] {
		if key.targetID != targetID {
			continue
		}
		if isCleanse && !run.state.IsDebuff() {
			if run.silence != nil {
				packet, err := effectraknet.ModifierDelete(targetID, run.silence.instanceID)
				if err != nil {
					return nil, fmt.Errorf("basicSilencePurgeDelete: %w", err)
				}
				isCreated, err := run.silence.release(pool)
				if err != nil {
					return nil, fmt.Errorf("basicSilencePurgeRelease: %w", err)
				}
				if isCreated {
					packets = append(packets, packet)
				}
				run.silence = nil
			}
			continue
		}
		deleted, err := e.retireBasicHitModifierLocked(peerSession.zone.NPCs(), peerSession.zone.Effect(), key, pool)
		if err != nil {
			return nil, fmt.Errorf("basicTargetRetire: %w", err)
		}
		packets = append(packets, deleted...)
	}
	return packets, nil
}
