package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type invincitronRageExpiry struct {
	runtime    campaignDamageRuntime
	sessionKey string
	generation uint64
	objectID   uint32
	expiresAt  time.Time
	run        *campaignNPCModifierRun
}

// Called by the accepted first-half-health shield transition. The feature has
// already cleansed authoritative status; retire its corresponding producers too.
func (e campaignDamageRuntime) startInvincitronRage(packet raknet.Packet, sessionKey string, generation uint64, ownerObjectID uint32, timestamp uint64) ([][]byte, error) {
	e.registry.mutex.Lock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || member.generation != generation || member.zone == nil {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	packets, err := e.registry.cleanseInvincitronLocked(member, ownerObjectID, e.npc.modifierPool)
	if err != nil {
		e.registry.mutex.Unlock()
		return packets, fmt.Errorf("invincitronCleanse: %w", err)
	}
	starts, err := member.zone.NPCs().StartInvincitronRage(ownerObjectID, e.npc.now())
	e.registry.mutex.Unlock()
	if err != nil {
		return packets, fmt.Errorf("invincitronRageStart: %w", err)
	}
	for _, start := range starts {
		ragePackets, err := e.publishInvincitronRage(packet, sessionKey, generation, start, timestamp)
		if err != nil {
			e.registry.mutex.Lock()
			current, isCurrentFound := e.registry.sessions[sessionKey]
			if isCurrentFound && current.zone == member.zone {
				actor, isExpired := current.zone.NPCs().ExpireInvincitronRage(start.Actor.Plan.ObjectID, start.Actor.Plan.InvincitronRage.ExpiresAt)
				if isExpired {
					restorePacket, restoreErr := marshalInvincitronRageAttributes(actor)
					if restoreErr != nil {
						err = errors.Join(err, restoreErr)
					} else {
						packets = append(packets, restorePacket)
					}
				}
			}
			e.registry.mutex.Unlock()
			return packets, fmt.Errorf("invincitronRagePublish: %w", err)
		}
		packets = append(packets, ragePackets...)
	}
	return packets, nil
}

func (e campaignDamageRuntime) publishInvincitronRage(packet raknet.Packet, sessionKey string, generation uint64, start zonenpc.InvincitronRageStart, timestamp uint64) ([][]byte, error) {
	attributes, err := marshalInvincitronRageAttributes(start.Actor)
	if err != nil {
		return nil, fmt.Errorf("rageAttributes: %w", err)
	}
	run, err := newCampaignNPCModifierRun(e.npc.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("rageModifierRun: %w", err)
	}
	objectID := start.Actor.Plan.ObjectID
	run.record = zoneeffect.Modifier{InstanceID: run.instanceID, GUID: util.HashID("NomadDroneRage"), SourceObjectID: start.Actor.Plan.OwnerObjectID, TargetObjectID: objectID, Rank: uint32(start.Rank), Duration: start.Duration, ExpiresAt: start.Actor.Plan.InvincitronRage.ExpiresAt, Kind: zoneeffect.ModifierKindBuff}
	createPacket, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{SourceObjectID: run.record.SourceObjectID, TargetObjectID: objectID, ModifierID: run.record.GUID, InstanceID: run.instanceID, Duration: start.Duration, Timestamp: timestamp})
	if err != nil {
		isCreated, releaseErr := run.release(e.npc.modifierPool)
		if isCreated {
			return nil, errors.New("unpublished rage modifier was created")
		}
		return nil, fmt.Errorf("rageModifierCreate: %w", errors.Join(err, releaseErr))
	}
	e.registry.mutex.Lock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || member.generation != generation || member.zone == nil {
		e.registry.mutex.Unlock()
		isReleased, releaseErr := run.release(e.npc.modifierPool)
		if releaseErr != nil {
			return nil, fmt.Errorf("rageStaleRelease: %w", releaseErr)
		}
		if isReleased {
			return nil, nil
		}
		return nil, nil
	}
	err = member.trackCampaignNPCModifier(run)
	if err == nil {
		err = member.zone.Effect().Put(run.record)
	}
	if err != nil {
		member.untrackCampaignNPCModifier(run)
		e.registry.sessions[sessionKey] = member
		e.registry.mutex.Unlock()
		isCreated, releaseErr := run.release(e.npc.modifierPool)
		if isCreated {
			return nil, errors.New("unpublished rage modifier was created")
		}
		return nil, fmt.Errorf("rageModifierTrack: %w", errors.Join(err, releaseErr))
	}
	isCreated := run.create()
	e.registry.sessions[sessionKey] = member
	e.registry.mutex.Unlock()
	if !isCreated {
		return nil, errors.New("rage modifier publication unavailable")
	}
	expiry := invincitronRageExpiry{runtime: e, sessionKey: sessionKey, generation: generation, objectID: objectID, expiresAt: start.Actor.Plan.InvincitronRage.ExpiresAt, run: run}
	cancel, err := scheduleNPCProducers(e.registry, packet, []raknet.ScheduledPacketProducer{{Delay: start.Duration, Produce: expiry.produce}})
	if err == nil && cancel == nil {
		err = errors.New("rage expiry cancellation unavailable")
	}
	if err != nil {
		cleanupPackets, cleanupErr := expiry.produce()
		e.registry.mutex.Lock()
		member = e.registry.sessions[sessionKey]
		member.queueCampaignPackets(cleanupPackets)
		e.registry.sessions[sessionKey] = member
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("rageSchedule: %w", errors.Join(err, cleanupErr))
	}
	e.registry.mutex.Lock()
	if run.isActive() {
		run.cancel = cancel
	} else {
		cancel()
	}
	e.registry.mutex.Unlock()
	return [][]byte{createPacket, attributes}, nil
}

func (e invincitronRageExpiry) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || member.generation != e.generation || member.zone == nil {
		return nil, nil
	}
	actor, isExpired := member.zone.NPCs().ExpireInvincitronRage(e.objectID, e.expiresAt)
	member.untrackCampaignNPCModifier(e.run)
	member.zone.Effect().Remove(e.run.instanceID)
	e.runtime.registry.sessions[e.sessionKey] = member
	isCreated, err := e.run.release(e.runtime.npc.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("rageExpireRelease: %w", err)
	}
	var packets [][]byte
	if isCreated {
		packet, deleteErr := effectraknet.ModifierDelete(e.objectID, e.run.instanceID)
		if deleteErr != nil {
			return nil, fmt.Errorf("rageExpireDelete: %w", deleteErr)
		}
		packets = append(packets, packet)
	}
	if isExpired && !actor.IsDefeated {
		packet, attributeErr := marshalInvincitronRageAttributes(actor)
		if attributeErr != nil {
			return nil, fmt.Errorf("rageExpireAttributes: %w", attributeErr)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func marshalInvincitronRageAttributes(actor zonenpc.Snapshot) ([]byte, error) {
	packet, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{ObjectID: actor.Plan.ObjectID, Value: map[uint8]float32{23: zonenpc.EffectiveAttackSpeedBonus(actor.Plan), 48: zonenpc.EffectiveMovementSpeedBuff(actor.Plan)}})
	if err != nil {
		return nil, fmt.Errorf("rageAttributeMarshal: %w", err)
	}
	return packet, nil
}

func (e *gameplaySessionRegistry) cleanseInvincitronLocked(member gameplayPeerSession, objectID uint32, pool *modifierPool) ([][]byte, error) {
	packets, err := e.retireFixtureBurnsLocked(member.zone.NPCs(), objectID, pool)
	if err != nil {
		return packets, fmt.Errorf("cleanseFixture: %w", err)
	}
	cloudPackets, err := e.retireCloudPoisonLocked(member.zone.NPCs(), objectID, pool)
	if err != nil {
		return packets, fmt.Errorf("cleanseCloud: %w", err)
	}
	packets = append(packets, cloudPackets...)
	packets = append(packets, e.retireTargetInfectionRootsLocked(member.zone.NPCs(), objectID)...)
	beastPacket, err := e.retireBeastVulnerabilityLocked(member.zone.NPCs(), member.zone.Effect(), objectID, pool)
	if err != nil {
		return packets, fmt.Errorf("cleanseBeast: %w", err)
	}
	if beastPacket != nil {
		packets = append(packets, beastPacket)
	}
	basicPackets, err := e.retireTargetBasicModifiersLocked(member, objectID, pool, true)
	if err != nil {
		return packets, fmt.Errorf("cleanseBasic: %w", err)
	}
	packets = append(packets, basicPackets...)
	for memberKey, observer := range e.sessions {
		if observer.zone != member.zone {
			continue
		}
		for _, modifier := range member.zone.Effect().Snapshot() {
			if modifier.TargetObjectID != objectID || modifier.Kind != zoneeffect.ModifierKindDebuff {
				continue
			}
			run := observer.campaignNPCModifiers[modifier.InstanceID]
			if run == nil {
				continue
			}
			deletePacket, deleteErr := purgeHeroSpecialDebuff(objectID, run, pool)
			if deleteErr != nil {
				return packets, fmt.Errorf("cleanseSpecial: %w", deleteErr)
			}
			if run.cancel != nil {
				run.cancel()
				run.cancel = nil
			}
			observer.untrackCampaignNPCModifier(run)
			member.zone.Effect().Remove(modifier.InstanceID)
			if deletePacket != nil {
				packets = append(packets, deletePacket)
			}
		}
		e.sessions[memberKey] = observer
	}
	return packets, nil
}
