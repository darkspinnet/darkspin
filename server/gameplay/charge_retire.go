package gameplay

import (
	"fmt"
	"log"
	"time"

	"github.com/darkspinnet/darkspin/server/zone"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
)

// The registry owns these leases after the hero action slot has been cleared.
// All operations below run with the registry lock held.
func (e *gameplaySessionRegistry) queueChargePacketsLocked(originZone *zone.Zone, packets [][]byte) {
	if originZone == nil || len(packets) == 0 {
		return
	}
	for sessionKey, member := range e.sessions {
		if member.zone != originZone {
			continue
		}
		member.queueCampaignPackets(packets)
		e.sessions[sessionKey] = member
	}
}

func (e *gameplaySessionRegistry) queueChargeReleaseLocked(run *heroChargeRun, ownerSessionKey string, packets [][]byte) {
	presentations := gameplayPeerPresentationPackets(packets)
	for sessionKey, member := range e.sessions {
		if member.zone != run.zone {
			continue
		}
		if sessionKey == ownerSessionKey && member.generation == run.generation {
			member.queueCampaignPackets(packets)
		} else {
			if member.isRejoinPending || !member.isCampaignPresentationAvailable() {
				continue
			}
			err := member.queueCampaignPresentation(presentations)
			if err != nil {
				log.Printf("RakNet charge release presentation failed user=%d: %v", member.binding.UserID, err)
			}
		}
		e.sessions[sessionKey] = member
	}
}

func (e heroChargeSchedule) commitModifierLocked(peerSession *gameplayPeerSession, instanceID uint32, packet []byte) error {
	if e.runtime.registry.chargeRuns == nil {
		e.runtime.registry.chargeRuns = make(map[*heroChargeRun]struct{})
	}
	e.runtime.registry.chargeRuns[e.run] = struct{}{}
	if peerSession.chargeRuns == nil {
		peerSession.chargeRuns = make(map[*heroChargeRun]struct{})
	}
	peerSession.chargeRuns[e.run] = struct{}{}
	e.runtime.registry.sessions[e.sessionKey] = *peerSession
	e.runtime.registry.queueChargePacketsLocked(e.run.zone, [][]byte{packet})
	*peerSession = e.runtime.registry.sessions[e.sessionKey]
	e.run.mutex.Lock()
	for index := range e.run.modifiers {
		modifier := &e.run.modifiers[index]
		if modifier.instanceID != instanceID {
			continue
		}
		modifier.isPublished = true
		deadline := modifier.expiresAt
		e.run.mutex.Unlock()
		if e.run.timer == nil {
			e.runtime.registry.retireChargeModifiersLocked(e.run, instanceID)
			*peerSession = e.runtime.registry.sessions[e.sessionKey]
			return fmt.Errorf("chargeExpiry: timer unavailable")
		}
		expiry := chargeModifierExpiry{registry: e.runtime.registry, run: e.run, instanceID: instanceID}
		cancel, err := e.run.timer.Schedule(max(time.Duration(0), deadline.Sub(e.runtime.now())), expiry.execute)
		if err != nil {
			e.runtime.registry.retireChargeModifiersLocked(e.run, instanceID)
			*peerSession = e.runtime.registry.sessions[e.sessionKey]
			return fmt.Errorf("chargeExpirySchedule: %w", err)
		}
		if cancel == nil {
			e.runtime.registry.retireChargeModifiersLocked(e.run, instanceID)
			*peerSession = e.runtime.registry.sessions[e.sessionKey]
			return fmt.Errorf("chargeExpiry: cancellation unavailable")
		}
		e.run.mutex.Lock()
		e.run.modifiers[index].cancel = cancel
		e.run.mutex.Unlock()
		return nil
	}
	e.run.mutex.Unlock()
	return fmt.Errorf("chargeLease: instance %d unavailable", instanceID)
}

type chargeModifierExpiry struct {
	registry   *gameplaySessionRegistry
	run        *heroChargeRun
	instanceID uint32
}

func (e chargeModifierExpiry) execute() {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	_, isRetained := e.registry.chargeRuns[e.run]
	if !isRetained {
		return
	}
	e.registry.retireChargeModifiersLocked(e.run, e.instanceID)
}

func (e *gameplaySessionRegistry) retireChargeModifiersLocked(run *heroChargeRun, instanceID uint32) {
	run.mutex.Lock()
	defer run.mutex.Unlock()
	retainedModifiers := make([]heroChargeModifier, 0, len(run.modifiers))
	for _, modifier := range run.modifiers {
		if instanceID != 0 && modifier.instanceID != instanceID {
			retainedModifiers = append(retainedModifiers, modifier)
			continue
		}
		packet, err := effectraknet.ModifierDelete(modifier.targetObjectID, modifier.instanceID)
		if err != nil {
			log.Printf("RakNet charge modifier deletion failed instance=%d: %v", modifier.instanceID, err)
			retainedModifiers = append(retainedModifiers, modifier)
			continue
		}
		if modifier.isPublished {
			e.queueChargePacketsLocked(run.zone, [][]byte{packet})
		}
		if modifier.cancel != nil {
			modifier.cancel()
		}
		switch run.assetName {
		case "BeastCharge":
			run.npc.ClearStun(modifier.targetObjectID, modifier.expiresAt)
		case "EntanglingRush":
			run.npc.ClearRoot(modifier.targetObjectID, modifier.expiresAt)
		case "PhantomCharge":
			run.npc.ClearSilence(modifier.targetObjectID, modifier.expiresAt)
		case "BioRandom2":
			// The short taunt presentation does not own ForceTargetInRadius.
			// Only the caster buff owns this exact reduction deadline.
			for sessionKey, member := range e.sessions {
				if member.zone == run.zone && member.roarReductionObjectID == modifier.targetObjectID &&
					member.roarReductionExpiresAt == modifier.expiresAt {
					member.roarReductionObjectID = 0
					member.roarReductionExpiresAt = time.Time{}
					e.sessions[sessionKey] = member
				}
			}
		}
		err = run.modifierPool.Release(modifier.instanceID)
		if err != nil {
			log.Printf("RakNet charge modifier release failed instance=%d: %v", modifier.instanceID, err)
		}
	}
	run.modifiers = retainedModifiers
	if len(run.modifiers) == 0 && !run.isEffectAttached {
		delete(e.chargeRuns, run)
		for sessionKey, member := range e.sessions {
			delete(member.chargeRuns, run)
			e.sessions[sessionKey] = member
		}
	}
}

func (e *gameplayPeerSession) retireChargeRunsLocked() {
	for run := range e.chargeRuns {
		if run.registry == nil {
			continue
		}
		ownerSessionKey := ""
		for sessionKey, member := range run.registry.sessions {
			if member.zone == e.zone && member.binding.UserID == e.binding.UserID && member.generation == e.generation {
				ownerSessionKey = sessionKey
				run.registry.sessions[sessionKey] = *e
				break
			}
		}
		run.stopLocked()
		if ownerSessionKey != "" {
			*e = run.registry.sessions[ownerSessionKey]
		}
		delete(run.registry.chargeRuns, run)
		delete(e.chargeRuns, run)
	}
}

func (e *gameplaySessionRegistry) pruneChargeRunLocked(run *heroChargeRun) {
	run.mutex.Lock()
	isRetained := len(run.modifiers) != 0 || run.isEffectAttached
	run.mutex.Unlock()
	if isRetained {
		return
	}
	delete(e.chargeRuns, run)
	for sessionKey, member := range e.sessions {
		delete(member.chargeRuns, run)
		e.sessions[sessionKey] = member
	}
}

type chargeRunRetirement struct {
	run *heroChargeRun
}

func (e chargeRunRetirement) execute() {
	e.run.registry.mutex.Lock()
	defer e.run.registry.mutex.Unlock()
	e.run.stopLocked()
}

func (e *gameplayPeerSession) interruptChargeLocked(run *heroChargeRun) [][]byte {
	if run.registry == nil {
		return run.Interrupt()
	}
	for sessionKey, member := range run.registry.sessions {
		if member.zone != e.zone || member.binding.UserID != e.binding.UserID || member.generation != e.generation {
			continue
		}
		run.registry.sessions[sessionKey] = *e
		packets := run.Interrupt()
		*e = run.registry.sessions[sessionKey]
		return packets
	}
	return run.Interrupt()
}

type chargeEffectExpiry struct {
	run      *heroChargeRun
	deadline time.Time
	packet   []byte
}

func (e chargeEffectExpiry) arm() {
	e.run.registry.mutex.Lock()
	e.run.mutex.Lock()
	isCurrent := !e.run.isCleaned && e.run.isEffectAttached
	e.run.mutex.Unlock()
	if isCurrent {
		e.run.registry.queueChargePacketsLocked(e.run.zone, [][]byte{e.packet})
	}
	e.run.registry.mutex.Unlock()
	if !isCurrent {
		return
	}
	if e.run.timer == nil {
		log.Printf("RakNet charge effect expiry timer unavailable owner=%d", e.run.ownerObjectID)
		e.execute()
		return
	}
	cancel, err := e.run.timer.Schedule(max(time.Duration(0), e.deadline.Sub(e.run.now())), e.execute)
	if err != nil || cancel == nil {
		log.Printf("RakNet charge effect expiry schedule failed owner=%d: %v", e.run.ownerObjectID, err)
		e.execute()
	}
}

func (e chargeEffectExpiry) execute() {
	e.run.registry.mutex.Lock()
	defer e.run.registry.mutex.Unlock()
	e.run.releaseEffectLocked()
	e.run.resetPhantomAnimationLocked()
	e.run.registry.pruneChargeRunLocked(e.run)
}
