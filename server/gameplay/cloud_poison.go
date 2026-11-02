package gameplay

import (
	"fmt"
	"net"
	"sort"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonehero "github.com/darkspinnet/darkspin/server/zone/hero"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	objectraknet "github.com/darkspinnet/darkspin/server/zone/object/raknet103"
)

func (e campaignCloudLobSchedule) commitPoison(isLanding bool, timestamp uint64) ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) || peerSession.zone == nil {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	npc := peerSession.zone.NPCs()
	world := e.runtime.registry.cloudPoisons[npc]
	if world == nil {
		world = &campaignCloudPoisonWorld{regions: make(map[*campaignCloudLobRun]campaignCloudPoisonRegion), targets: make(map[uint32]*campaignCloudPoisonTarget), startedAt: e.runtime.now(), startTimestamp: timestamp, effect: peerSession.zone.Effect()}
		if e.runtime.registry.cloudPoisons == nil {
			e.runtime.registry.cloudPoisons = make(map[*zonenpc.Session]*campaignCloudPoisonWorld)
		}
		e.runtime.registry.cloudPoisons[npc] = world
	}
	at := e.runtime.now()
	if isLanding {
		world.nextOrder++
		expiresAt := at.Add(e.plan.Definition.CloudLob.CloudDuration)
		regionSchedule := e
		regionSchedule.packet = gameplaySchedulePacket(e.packet)
		world.regions[e.run] = campaignCloudPoisonRegion{schedule: regionSchedule, expiresAt: expiresAt, deleteAt: expiresAt.Add(300 * time.Millisecond), order: world.nextOrder, occupants: make(map[uint32]struct{})}
	}
	regions := world.liveRegions(at, e.run)
	publications, err := e.resolveCloudPoisonsLocked(world, regions, at)
	e.runtime.registry.mutex.Unlock()
	if err != nil {
		if len(publications) == 0 {
			return nil, fmt.Errorf("cloudResolve: %w", err)
		}
		e.runtime.logger.Printf("Sprout poison partial damage: %v", err)
	}
	return e.publishCloudPoisons(publications, timestamp)
}

func (e campaignCloudLobSchedule) publishCloudPoisons(publications []campaignCloudPoisonPublication, timestamp uint64) ([][]byte, error) {
	packets := make([][]byte, 0)
	for _, publication := range publications {
		packets = append(packets, publication.entries...)
		if len(publication.results) == 0 {
			continue
		}
		published, publishErr := e.runtime.damage.publishAreaResults(
			e.packet, publication.sessionKey, publication.generation, publication.sourceID,
			timestamp, publication.binding, publication.results, publication.transitions, nil, false,
		)
		if publishErr != nil {
			return packets, fmt.Errorf("cloudPublish: %w", publishErr)
		}
		packets = append(packets, published...)
	}
	return packets, nil
}

func (e *campaignCloudPoisonWorld) liveRegions(at time.Time, pendingRun *campaignCloudLobRun) []campaignCloudPoisonRegion {
	regions := make([]campaignCloudPoisonRegion, 0, len(e.regions))
	for _, region := range e.regions {
		if !at.Before(region.expiresAt) || (!region.isPublished && region.schedule.run != pendingRun) {
			continue
		}
		regions = append(regions, region)
	}
	sort.Slice(regions, func(i, j int) bool { return regions[i].order < regions[j].order })
	return regions
}

func cloudPoisonRegionAt(regions []campaignCloudPoisonRegion, position game.Vec3) (campaignCloudPoisonRegion, bool) {
	for _, region := range regions {
		plan := region.schedule.plan
		if game.Vec3(plan.Destination).Sub(position).Length() <= plan.Definition.CloudLob.CloudRadius {
			return region, true
		}
	}
	return campaignCloudPoisonRegion{}, false
}

func (e campaignCloudLobSchedule) resolveCloudPoisonsLocked(world *campaignCloudPoisonWorld, regions []campaignCloudPoisonRegion, at time.Time) ([]campaignCloudPoisonPublication, error) {
	driver := e.runtime.registry.sessions[e.sessionKey]
	npc := driver.zone.NPCs()
	enemies := npc.LiveSnapshots()
	enteredRegions := world.observeEntries(regions, enemies)
	installedMods := make(map[uint32]zoneeffect.Modifier)
	for _, modifier := range world.effect.Snapshot() {
		installedMods[modifier.InstanceID] = modifier
	}
	liveTargets := make(map[uint32]struct{})
	publications := make([]campaignCloudPoisonPublication, 0)
	for _, enemy := range enemies {
		if enemy.Faction != zonenpc.FactionNonPlayerAligned || !enemy.IsPublished || enemy.IsDefeated || enemy.HitPoint <= 0 {
			continue
		}
		objectID := enemy.Plan.ObjectID
		liveTargets[objectID] = struct{}{}
		_, isInside := cloudPoisonRegionAt(regions, enemy.Plan.Position)
		region, isEnteredRegion := enteredRegions[objectID]
		target := world.targets[objectID]
		if target != nil && target.isCreated {
			if _, isInstalled := installedMods[target.instanceID]; !isInstalled {
				deletes, err := e.runtime.registry.retireCloudPoisonLocked(npc, objectID, e.runtime.modifierPool)
				if err != nil {
					return publications, fmt.Errorf("cloudRemoved: %w", err)
				}
				publications = append(publications, campaignCloudPoisonPublication{entries: deletes})
				continue
			}
		}
		isEntered := target == nil && isEnteredRegion
		if target == nil {
			if !isEnteredRegion {
				continue
			}
			instanceID, err := e.runtime.modifierPool.Allocate()
			if err != nil {
				return publications, fmt.Errorf("cloudAllocate: %w", err)
			}
			target = &campaignCloudPoisonTarget{region: region, creature: region.schedule.creature, nextTickAt: at.Add(region.schedule.plan.Definition.CloudLob.TickDuration), instanceID: instanceID, startTimestamp: world.startTimestamp + uint64(max(time.Duration(0), at.Sub(world.startedAt))/time.Millisecond), isPending: !region.isPublished}
			world.targets[objectID] = target
		} else {
			if target.isPending {
				continue
			}
			if at.Before(target.nextTickAt) {
				continue
			}
			interval := target.region.schedule.plan.Definition.CloudLob.TickDuration
			// WaitForXSeconds resumes this modifier once. A late poll must not
			// consume additional waits or burst several missed damage ticks.
			target.nextTickAt = at.Add(interval)
			if isInside {
				target.outsideWaitCount = 0
			} else {
				target.outsideWaitCount++
				if target.outsideWaitCount >= target.region.schedule.plan.Definition.CloudLob.TickCount {
					packets, err := e.runtime.registry.retireCloudPoisonLocked(npc, objectID, e.runtime.modifierPool)
					if err != nil {
						return publications, fmt.Errorf("cloudExpire: %w", err)
					}
					publications = append(publications, campaignCloudPoisonPublication{entries: packets})
					continue
				}
			}
		}
		publication, err := e.commitCloudPoisonTargetLocked(target, enemy, isEntered)
		if err != nil {
			deletes, cleanupErr := e.runtime.registry.retireCloudPoisonLocked(npc, objectID, e.runtime.modifierPool)
			if cleanupErr != nil {
				e.runtime.logger.Printf("Sprout failed target cleanup target=%d: %v", objectID, cleanupErr)
			}
			publications = append(publications, campaignCloudPoisonPublication{entries: deletes})
			return publications, fmt.Errorf("cloudTarget[%d]: %w", objectID, err)
		}
		target.isCreated = true
		publications = append(publications, publication)
	}
	for objectID := range world.targets {
		if _, isLive := liveTargets[objectID]; !isLive {
			packets, err := e.runtime.registry.retireCloudPoisonLocked(npc, objectID, e.runtime.modifierPool)
			if err != nil {
				return publications, fmt.Errorf("cloudDead: %w", err)
			}
			publications = append(publications, campaignCloudPoisonPublication{entries: packets})
		}
	}
	return publications, nil
}

func (e campaignCloudLobSchedule) commitCloudPoisonTargetLocked(target *campaignCloudPoisonTarget, enemy zonenpc.Snapshot, isEntered bool) (campaignCloudPoisonPublication, error) {
	origin := target.region.schedule
	driver := e.runtime.registry.sessions[e.sessionKey]
	owner := driver
	sessionKey := e.sessionKey
	creature := target.creature
	// GetWeaponDamage reads the original object each iteration, while
	// TakeDamage receives the modifier's retained attribute snapshot.
	for sourceKey, candidate := range e.runtime.registry.sessions {
		if candidate.generation != origin.generation || candidate.zone != driver.zone || candidate.binding.UserID != origin.binding.UserID {
			continue
		}
		currentEndpoint := e.runtime.registry.memberEndpoints[gameplaySessionMemberKey(candidate)]
		if currentEndpoint != "" && currentEndpoint != sourceKey {
			continue
		}
		// Membership survives an endpoint handover. Keep the original semantic
		// owner for transitions as well as live weapon reads after that handover.
		owner = candidate
		sessionKey = sourceKey
		for index, currentCreature := range candidate.binding.Creatures {
			if zonehero.ObjectID(candidate.binding.Slot, uint32(index)) == origin.sourceObjectID && currentCreature.Noun == target.creature.Noun {
				creature.MinimumWeaponDamage = currentCreature.MinimumWeaponDamage
				creature.MaximumWeaponDamage = currentCreature.MaximumWeaponDamage
			}
		}
		break
	}
	definition := origin.plan.Definition
	definition.IsWeaponDamageRange = false
	// The requested child modifier has IsDoT|IsEnergyDamage|IsDebuff,
	// independently of the launching basic ability's Basic/AoE flags.
	definition.DescriptorMask = 1<<2 | 1<<5 | 1<<7
	definition.IsDescriptorFound = true
	minimum := creature.MinimumWeaponDamage / float32(definition.CloudLob.TickCount)
	maximum := creature.MaximumWeaponDamage / float32(definition.CloudLob.TickCount)
	damage, err := zoneability.ProjectDamage(target.creature, definition, minimum, maximum)
	if err != nil {
		return campaignCloudPoisonPublication{}, fmt.Errorf("cloudDamage: %w", err)
	}
	entries := make([][]byte, 0)
	if isEntered {
		entries, err = abilityraknet.CloudEntryEffects(definition, origin.sourceObjectID, []uint32{enemy.Plan.ObjectID})
		if err != nil {
			return campaignCloudPoisonPublication{}, fmt.Errorf("cloudEntry: %w", err)
		}
		create, createErr := raknet.MarshalApplication(raknet.ModifierCreatedMessage{TargetID: enemy.Plan.ObjectID, ModifierGUID: util.HashID("SproutPoison"), InstanceID: target.instanceID, StackCount: 1, NPCAffixAssetID: 0, StartMilliseconds: target.startTimestamp})
		if createErr != nil {
			return campaignCloudPoisonPublication{}, fmt.Errorf("cloudCreate: %w", createErr)
		}
		entries = append([][]byte{create}, entries...)
		err = driver.zone.Effect().Put(zoneeffect.Modifier{InstanceID: target.instanceID, GUID: util.HashID("SproutPoison"), SourceObjectID: origin.sourceObjectID, TargetObjectID: enemy.Plan.ObjectID, InitiatorObject: origin.sourceObjectID, Kind: zoneeffect.ModifierKindDebuff, StackCount: 1, Rank: 1, IsIndefinite: true})
		if err != nil {
			return campaignCloudPoisonPublication{}, fmt.Errorf("cloudInventory: %w", err)
		}
	}
	plan := zoneability.AreaPlan{SourceObjectID: origin.sourceObjectID, AbilityID: origin.plan.AbilityID, Definition: definition, Damage: damage, Center: game.Vec3(origin.plan.Destination), Target: []zonenpc.Snapshot{enemy}}
	results, err := zoneability.CommitArea(driver.zone.Population().Random(), driver.zone.NPCs(), plan, target.creature, origin.binding.Difficulty, e.runtime.program.Critical)
	if err != nil {
		return campaignCloudPoisonPublication{}, fmt.Errorf("cloudCommit: %w", err)
	}
	for index := range results {
		results[index].ActivationID = origin.runtime.damage.activationID
	}
	transitions := make([]campaignDamageTransition, 0, len(results))
	for _, result := range results {
		isOriginalActor := owner.generation == origin.generation && owner.binding.UserID == origin.binding.UserID && owner.deployedObjectID == origin.sourceObjectID
		transition, transitionErr := owner.applyCampaignDamageTransitionWithKill(result.Damage, isOriginalActor)
		if transitionErr != nil {
			// The target damage is already committed. Preserve its publication
			// even when an ancillary world/kill transition cannot complete.
			e.runtime.logger.Printf("Sprout committed damage transition target=%d: %v", result.Damage.ObjectID, transitionErr)
		}
		transitions = append(transitions, transition)
	}
	e.runtime.registry.sessions[sessionKey] = owner
	return campaignCloudPoisonPublication{sessionKey: sessionKey, generation: owner.generation, sourceID: origin.sourceObjectID, binding: origin.binding, results: results, transitions: transitions, entries: entries}, nil
}

// Each cloud's private inCloud table records transitions independently. A
// consumed or cleansed poison cannot be requested again until another entry.
func (e *campaignCloudPoisonWorld) observeEntries(regions []campaignCloudPoisonRegion, enemies []zonenpc.Snapshot) map[uint32]campaignCloudPoisonRegion {
	enteredRegions := make(map[uint32]campaignCloudPoisonRegion)
	for _, region := range regions {
		occupants := make(map[uint32]struct{})
		plan := region.schedule.plan
		for _, enemy := range enemies {
			if enemy.Faction != zonenpc.FactionNonPlayerAligned || !enemy.IsPublished || enemy.IsDefeated || enemy.HitPoint <= 0 || game.Vec3(plan.Destination).Sub(enemy.Plan.Position).Length() > plan.Definition.CloudLob.CloudRadius {
				continue
			}
			objectID := enemy.Plan.ObjectID
			occupants[objectID] = struct{}{}
			if _, isInside := region.occupants[objectID]; isInside {
				continue
			}
			if _, isEntered := enteredRegions[objectID]; !isEntered {
				enteredRegions[objectID] = region
			}
		}
		region.occupants = occupants
		e.regions[region.schedule.run] = region
	}
	return enteredRegions
}

func (e *gameplaySessionRegistry) retireCloudPoisonLocked(npc *zonenpc.Session, objectID uint32, pool *modifierPool) ([][]byte, error) {
	world := e.cloudPoisons[npc]
	if world == nil || world.targets[objectID] == nil {
		return nil, nil
	}
	target := world.targets[objectID]
	packet, err := effectraknet.ModifierDelete(objectID, target.instanceID)
	if err != nil {
		return nil, fmt.Errorf("cloudDelete: %w", err)
	}
	err = pool.Release(target.instanceID)
	if err != nil {
		return nil, fmt.Errorf("cloudRelease: %w", err)
	}
	delete(world.targets, objectID)
	removed, isRemoved := world.effect.Remove(target.instanceID)
	if isRemoved && removed.TargetObjectID != objectID {
		return nil, fmt.Errorf("cloud target mismatch: expected %d, got %d", objectID, removed.TargetObjectID)
	}
	if !target.isCreated {
		return nil, nil
	}
	return [][]byte{packet}, nil
}

func (e campaignCloudLobSchedule) retireCloudWorldLocked(npc *zonenpc.Session, world *campaignCloudPoisonWorld) {
	for objectID := range world.targets {
		packets, err := e.runtime.registry.retireCloudPoisonLocked(npc, objectID, e.runtime.modifierPool)
		if err != nil {
			e.runtime.logger.Printf("Sprout detached target cleanup target=%d: %v", objectID, err)
		}
		// No connected driver remains to publish these exact-instance deletions.
		if len(packets) != 0 {
			continue
		}
	}
}

func (e *gameplaySessionRegistry) cloudPublicationDriverLocked(npc *zonenpc.Session, preferredUserID uint64) (string, gameplayPeerSession) {
	keys := make([]string, 0)
	for sessionKey, peerSession := range e.sessions {
		if peerSession.zone == nil || peerSession.zone.NPCs() != npc || peerSession.isRejoinPending ||
			peerSession.transportGeneration == 0 ||
			(peerSession.schedulePacket.ScheduleGroup == nil && peerSession.schedulePacket.ScheduleGroupResult == nil) {
			continue
		}
		keys = append(keys, sessionKey)
	}
	sort.Strings(keys)
	for _, key := range keys {
		peerSession := e.sessions[key]
		if peerSession.binding.UserID == preferredUserID {
			return key, peerSession
		}
	}
	if len(keys) == 0 {
		return "", gameplayPeerSession{}
	}
	return keys[0], e.sessions[keys[0]]
}

// gameplaySchedulePacket intentionally strips identity. Restore the current
// publication driver's endpoint/generation without retaining an old request.
func cloudSchedulePacket(sessionKey string, peerSession gameplayPeerSession) (raknet.Packet, error) {
	address, err := net.ResolveUDPAddr("udp", sessionKey)
	if err != nil {
		return raknet.Packet{}, fmt.Errorf("cloudAddress: %w", err)
	}
	packet := peerSession.schedulePacket.Autonomous()
	packet.Address = address
	packet.TransportGeneration = peerSession.transportGeneration
	return packet, nil
}

func (e campaignCloudLobSchedule) removeCloudPoisonRegionLocked(peerSession gameplayPeerSession) {
	if peerSession.zone == nil {
		return
	}
	world := e.runtime.registry.cloudPoisons[peerSession.zone.NPCs()]
	if world == nil {
		return
	}
	region, isFound := world.regions[e.run]
	if !isFound {
		return
	}
	at := e.runtime.now()
	region.expiresAt = at
	region.deleteAt = at
	world.regions[e.run] = region
	world.scheduleNextLocked(e, peerSession.zone.NPCs(), at)
}

func (e campaignCloudLobSchedule) deleteCloudObjectsLocked(world *campaignCloudPoisonWorld, at time.Time) ([][]byte, error) {
	objectIDs := make([]uint32, 0)
	packets := make([][]byte, 0)
	for run, region := range world.regions {
		if !region.isPublished {
			continue
		}
		if !at.Before(region.expiresAt) && !region.isDeactivated {
			packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{ObjectID: region.schedule.cloudObjectID, Slot: 1, IsRemovalRequested: true})
			if err != nil {
				return packets, fmt.Errorf("cloudDetach: %w", err)
			}
			packets = append(packets, packet)
			region.isDeactivated = true
			world.regions[run] = region
		}
		if at.Before(region.deleteAt) {
			continue
		}
		objectIDs = append(objectIDs, region.schedule.cloudObjectID)
		delete(world.regions, run)
		owner, isFound := e.runtime.registry.sessions[region.schedule.sessionKey]
		if isFound && region.schedule.isCurrent(owner, true) {
			delete(owner.cloudLobAttacks, region.schedule.projectileObjectID)
			run.cancel = nil
			e.runtime.registry.sessions[region.schedule.sessionKey] = owner
		}
	}
	if len(objectIDs) == 0 {
		return packets, nil
	}
	sort.Slice(objectIDs, func(i, j int) bool { return objectIDs[i] < objectIDs[j] })
	packet, err := objectraknet.Delete(objectIDs)
	if err != nil {
		return packets, fmt.Errorf("cloudDelete: %w", err)
	}
	return append(packets, packet), nil
}
