package gameplay

import (
	"fmt"
	"log"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/zone"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
)

// Removal packets enter the original zone's durable queue before any owned
// modifier instance or attached-effect slot can be reused.
func (e *gameplaySessionRegistry) queueChannelRemovalsLocked(originZone *zone.Zone, packets [][]byte) {
	for sessionKey, member := range e.sessions {
		if member.zone != originZone {
			continue
		}
		member.queueCampaignPackets(packets)
		e.sessions[sessionKey] = member
	}
}

func (e *gameplaySessionRegistry) queueChannelPresentationLocked(originZone *zone.Zone, packets [][]byte) {
	for sessionKey, member := range e.sessions {
		if member.zone != originZone || member.isRejoinPending || !member.isCampaignPresentationAvailable() {
			continue
		}
		err := member.queueCampaignPresentation(packets)
		if err != nil {
			log.Printf("RakNet channel presentation failed user=%d: %v", member.binding.UserID, err)
		}
		e.sessions[sessionKey] = member
	}
}

func (e *gameplaySessionRegistry) queueChannelReleaseLocked(originZone *zone.Zone, ownerSessionKey string, generation uint64, packets [][]byte) {
	presentations := gameplayPeerPresentationPackets(packets)
	for sessionKey, member := range e.sessions {
		if member.zone != originZone {
			continue
		}
		if sessionKey == ownerSessionKey && member.generation == generation {
			member.queueCampaignPackets(packets)
		} else {
			if member.isRejoinPending || !member.isCampaignPresentationAvailable() {
				continue
			}
			err := member.queueCampaignPresentation(presentations)
			if err != nil {
				log.Printf("RakNet channel release presentation failed user=%d: %v", member.binding.UserID, err)
			}
		}
		e.sessions[sessionKey] = member
	}
}

func (e *heroDrainRun) retireLocked() error {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.isEffectsReleased {
		return nil
	}
	if e.areEffectsAttached {
		packets, err := abilityraknet.ChannelDrainEffectRemovals(e.sourceObjectID, e.sourceEffectSlot, e.targetObjectID, e.targetEffectSlot)
		if err != nil {
			return fmt.Errorf("drainRetireEncode: %w", err)
		}
		e.registry.queueChannelRemovalsLocked(e.zone, packets)
	}
	e.isStopped = true
	e.isProtectionActive = false
	e.isEffectsReleased = true
	e.areEffectsAttached = false
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	e.effectPool.Release(e.sourceObjectID, e.sourceEffectSlot)
	e.effectPool.Release(e.targetObjectID, e.targetEffectSlot)
	return nil
}

func (e *heroChannelAreaRun) retireLocked() error {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.isCleaned {
		return nil
	}
	packets := make([][]byte, 0, len(e.targets)+2)
	for _, target := range e.targets {
		if !target.isPublished {
			continue
		}
		packet, err := raknet.MarshalApplication(raknet.ModifierDeletedMessage{TargetID: target.snapshot.Plan.ObjectID, InstanceID: target.instanceID})
		if err != nil {
			return fmt.Errorf("areaRetireModifier: %w", err)
		}
		packets = append(packets, packet)
	}
	if e.isEffectPublished && e.isEffectBound {
		packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{Slot: e.effectSlot + 1, ObjectID: e.effectObjectID, IsRemovalRequested: true, IsHardStop: true})
		if err != nil {
			return fmt.Errorf("areaRetireEffect: %w", err)
		}
		packets = append(packets, packet)
	}
	if e.isEffectPublished && !e.isEffectObjectDeleted {
		packet, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{ObjectID: []uint32{e.effectObjectID}})
		if err != nil {
			return fmt.Errorf("areaRetireObject: %w", err)
		}
		packets = append(packets, packet)
	}
	targetPackets, err := e.targetRetirementPacketsLocked()
	if err != nil {
		return fmt.Errorf("areaRetireTarget: %w", err)
	}
	packets = append(packets, targetPackets...)
	e.registry.queueChannelRemovalsLocked(e.zone, packets)
	e.isCleaned = true
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	e.ReleaseTargets(e.targets)
	e.finishInterruptedTargetsLocked()
	e.targets = nil
	if e.isEffectBound {
		e.effectPool.Release(e.effectObjectID, e.effectSlot)
		e.isEffectBound = false
	}
	e.isEffectObjectDeleted = true
	return nil
}

func (e *gameplayPeerSession) retireChannelsLocked(sourceTimes ...uint64) {
	registry := (*gameplaySessionRegistry)(nil)
	if e.heroDrain != nil {
		registry = e.heroDrain.registry
	} else if e.heroChannelArea != nil {
		registry = e.heroChannelArea.registry
	}
	if registry == nil {
		return
	}
	ownerSessionKey := ""
	for sessionKey, member := range registry.sessions {
		if member.zone == e.zone && member.binding.UserID == e.binding.UserID && member.generation == e.generation {
			ownerSessionKey = sessionKey
			registry.sessions[sessionKey] = *e
			break
		}
	}
	if e.heroDrain != nil {
		err := e.heroDrain.retireLocked()
		if err != nil {
			log.Printf("RakNet drain retirement failed owner=%d: %v", e.heroDrain.sourceObjectID, err)
		}
		if len(sourceTimes) != 0 {
			packet, marshalErr := abilityraknet.AnimationReset(e.heroDrain.sourceObjectID, sourceTimes[0])
			if marshalErr != nil {
				log.Printf("RakNet drain interrupt animation failed: %v", marshalErr)
			} else {
				registry.queueChannelPresentationLocked(e.zone, [][]byte{packet})
			}
		}
	}
	if e.heroChannelArea != nil {
		err := e.heroChannelArea.retireLocked()
		if err != nil {
			log.Printf("RakNet area channel retirement failed owner=%d: %v", e.heroChannelArea.effectObjectID, err)
		}
	}
	if ownerSessionKey != "" {
		*e = registry.sessions[ownerSessionKey]
	}
}

type channelRetirement struct {
	registry *gameplaySessionRegistry
	drain    *heroDrainRun
	area     *heroChannelAreaRun
}

func (e channelRetirement) schedule(timer zone.Timer) {
	if timer == nil || e.registry == nil {
		log.Printf("RakNet channel retirement timer unavailable")
		return
	}
	cancel, err := timer.Schedule(0, e.execute)
	if err != nil {
		if cancel != nil {
			cancel()
		}
		log.Printf("RakNet channel retirement schedule failed: %v", err)
		return
	}
	if cancel == nil {
		log.Printf("RakNet channel retirement cancellation unavailable")
	}
}

func (e channelRetirement) execute() {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	var err error
	if e.drain != nil {
		err = e.drain.retireLocked()
	}
	if e.area != nil {
		err = e.area.retireLocked()
	}
	if err != nil {
		log.Printf("RakNet channel retirement failed: %v", err)
	}
}

type channelAreaAdmission struct {
	run     *heroChannelAreaRun
	packets [][]byte
}

func (e channelAreaAdmission) commit() {
	e.run.registry.mutex.Lock()
	defer e.run.registry.mutex.Unlock()
	// Publication commits the paid cast even if its visuals were interrupted.
	e.run.cast.activateLocked(e.run.cast.revision)
	e.run.mutex.Lock()
	defer e.run.mutex.Unlock()
	if e.run.isCleaned || !e.run.isEffectBound || e.run.isEffectObjectDeleted {
		return
	}
	e.run.registry.queueChannelPresentationLocked(e.run.zone, e.packets)
	e.run.isAdmitted = true
	e.run.isEffectPublished = true
}
