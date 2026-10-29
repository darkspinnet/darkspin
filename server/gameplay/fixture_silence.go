package gameplay

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zone "github.com/darkspinnet/darkspin/server/zone"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const silenceFieldModifier = "SilenceTnxModifier"

func isSilenceField(nounName string) bool {
	return strings.EqualFold(nounName, "DEST_prefab_tnx_object_silenceField.Noun")
}

func isSilenceFieldSource(world *zone.Zone, objectID uint32) bool {
	if world == nil || world.IsNPCDestroyed(objectID) {
		return false
	}
	source, isFound := world.NPCs().NPC(objectID)
	return isFound && source.IsPublished && source.Plan.IsFixture && isSilenceField(source.Plan.NounName) &&
		source.Plan.NPCProfile.AIGraph != nil && source.Plan.NPCProfile.AIGraph.IsResolved &&
		source.Plan.NPCProfile.AIGraph.DefinitionID == 3248692283
}

// TNXSuppressionAura639 deactivates OnAgentDestroyed, not OnAgentDeath.
// There is no authored death ability, eruption, corpse timer or physics removal.
func withSilenceFieldDeath(source zonenpc.Snapshot, definition zoneNPCDeathDefinition) zoneNPCDeathDefinition {
	if !source.Plan.IsFixture || !isSilenceField(source.Plan.NounName) {
		return definition
	}
	definition.isRemnantRetained = true
	definition.isCollisionRetained = true
	definition.isDeathAnimationSuppressed = true
	definition.ordinaryDeathAnimation = ""
	definition.corpseFadeDelay = 0
	definition.deleteDelay = 0
	definition.graphicsState = 0
	definition.explosionEffectName = ""
	return definition
}

type silenceFieldPair struct {
	sourceObjectID uint32
	targetObjectID uint32
}

// The registry lock serializes this shared world trigger. Membership belongs
// to the zone, so another peer polling cannot install another copy or lose it
// when the first polling peer disconnects.
func (e gameplayPendingRuntime) pollSilenceFieldsLocked(member *gameplayPeerSession, req raknet.Packet, now time.Time) ([][]byte, error) {
	world := member.zone
	targets, err := e.silenceFieldTargetsLocked(world, now)
	if err != nil {
		return nil, fmt.Errorf("fieldTargetPoses: %w", err)
	}
	insideTargets := make(map[silenceFieldPair]zone.NPCTarget)
	sources := make(map[uint32]zonenpc.Snapshot)
	for _, source := range world.NPCs().Snapshots() {
		if !isSilenceFieldSource(world, source.Plan.ObjectID) || source.Faction != zonenpc.FactionNonPlayerAligned {
			continue
		}
		sources[source.Plan.ObjectID] = source
		for _, target := range targets {
			if source.Plan.Position.Sub(target.Position).Length() <= 11 {
				insideTargets[silenceFieldPair{source.Plan.ObjectID, target.ObjectID}] = target
			}
		}
	}
	contacts := make(map[silenceFieldPair]zone.SilenceFieldContact)
	packets := make([][]byte, 0)
	for _, contact := range world.SilenceFieldContacts() {
		key := silenceFieldPair{contact.SourceObjectID, contact.TargetObjectID}
		target, isInside := insideTargets[key]
		if !isInside || target.HitPoint <= 0 {
			deleted, retireErr := e.retireSilenceFieldModifierLocked(world, contact)
			if retireErr != nil {
				return nil, fmt.Errorf("fieldExit: %w", retireErr)
			}
			packets = append(packets, deleted...)
			if !isInside {
				world.RemoveSilenceFieldContact(contact)
				continue
			}
			contact.InstanceID = 0
			err = world.PutSilenceFieldContact(contact)
			if err != nil {
				return nil, fmt.Errorf("fieldDeadMembership: %w", err)
			}
		}
		contacts[key] = contact
	}
	for key, target := range insideTargets {
		if _, isEntered := contacts[key]; isEntered {
			continue
		}
		contact := zone.SilenceFieldContact{SourceObjectID: key.sourceObjectID, TargetObjectID: target.ObjectID}
		if target.HitPoint > 0 {
			created, createErr := e.enterSilenceFieldLocked(member, req, now, sources[key.sourceObjectID], target, &contact)
			if createErr != nil {
				return nil, fmt.Errorf("fieldEnter: %w", createErr)
			}
			packets = append(packets, created...)
		}
		err = world.PutSilenceFieldContact(contact)
		if err != nil {
			return nil, fmt.Errorf("fieldMembership: %w", err)
		}
	}
	if len(packets) == 0 {
		return nil, nil
	}
	publication := campaignPeerPublication{registry: e.registry,
		identity: gameplayProducerIdentityFromSession(req.Address.String(), *member, true), packets: clonePendingPackets(packets)}
	err = req.AfterResponseCommit(publication.commit)
	if err != nil {
		return nil, fmt.Errorf("fieldPublication: %w", err)
	}
	return packets, nil
}

func (e gameplayPendingRuntime) silenceFieldTargetsLocked(world *zone.Zone, now time.Time) ([]zone.NPCTarget, error) {
	targets := make([]zone.NPCTarget, 0)
	for _, actor := range world.Hero().Snapshots() {
		targets = append(targets, zone.NPCTarget{UserID: actor.UserID, PeerGeneration: actor.PeerGeneration,
			ObjectID: actor.ObjectID, Position: actor.Position, HitPoint: actor.HitPoint, IsHero: true})
	}
	for _, actor := range world.Companion().Snapshots() {
		targets = append(targets, zone.NPCTarget{UserID: actor.UserID, PeerGeneration: actor.PeerGeneration,
			ObjectID: actor.ObjectID, Position: actor.Position, HitPoint: actor.HitPoint})
	}
	samples, err := e.registry.sampleNPCTargetsLocked(world, targets, now)
	if err != nil {
		return nil, fmt.Errorf("fieldTargetSample: %w", err)
	}
	return samples, nil
}

func (e gameplayPendingRuntime) enterSilenceFieldLocked(caller *gameplayPeerSession, req raknet.Packet,
	now time.Time, source zonenpc.Snapshot, target zone.NPCTarget, contact *zone.SilenceFieldContact,
) ([][]byte, error) {
	world := caller.zone
	targetKey := ""
	var targetMember gameplayPeerSession
	for key, candidate := range e.registry.sessions {
		if candidate.zone == world && candidate.binding.UserID == target.UserID && candidate.generation == target.PeerGeneration {
			targetKey, targetMember = key, candidate
			break
		}
	}
	if targetKey == "" {
		return nil, nil
	}
	if targetKey == req.Address.String() {
		targetMember = *caller
	}
	var immunity float32
	if target.IsHero {
		if targetMember.deployedObjectID != target.ObjectID || int(targetMember.deployedCreatureIndex) >= len(targetMember.binding.Creatures) {
			return nil, nil
		}
		creature := targetMember.projectPassiveCreature(targetMember.deployedCreatureIndex, now)
		immunity = creature.PartAttribute[78]
		// Native9E16C0 checks ModImmuneDebuff49 for descriptor IsDebuff
		// independently of this modifier's chain-only ImmuneToSilence gate.
		if creature.PartAttribute[49] > 0 || targetMember.heroModifierRun.IsDebuffImmune() {
			return nil, nil
		}
	} else {
		var isKnown bool
		immunity, isKnown = world.Companion().SilenceImmunity(target.ObjectID)
		if !isKnown {
			return nil, errors.New("silence field companion immunity unavailable")
		}
	}
	if immunity > 0 && targetMember.binding.Mode == game.ModeChain {
		return nil, nil
	}
	instanceID, err := e.modifierPool.Allocate()
	if err != nil {
		return nil, fmt.Errorf("fieldAllocate: %w", err)
	}
	created, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
		SourceObjectID: source.Plan.ObjectID, TargetObjectID: target.ObjectID,
		ModifierID: util.HashID(silenceFieldModifier), InstanceID: instanceID, IsIndefinite: true, Timestamp: req.SourceTime})
	if err == nil {
		err = world.Effect().Put(zoneeffect.Modifier{InstanceID: instanceID, GUID: util.HashID(silenceFieldModifier),
			SourceObjectID: source.Plan.ObjectID, TargetObjectID: target.ObjectID,
			InitiatorObject: source.Plan.ObjectID, Rank: uint32(zonenpc.EffectivePassiveRank(source.Plan)),
			Kind: zoneeffect.ModifierKindDebuff, IsIndefinite: true})
	}
	if err != nil {
		releaseErr := e.modifierPool.Release(instanceID)
		return nil, fmt.Errorf("fieldCreate: %w", errors.Join(err, releaseErr))
	}
	contact.InstanceID = instanceID
	if !target.IsHero {
		err = world.Companion().AddSilenceLease(target.ObjectID, instanceID)
		if err != nil {
			removed, retireErr := e.retireSilenceFieldModifierLocked(world, *contact)
			if len(removed) != 0 {
				// The create was never published; rollback emits no wire deletion.
			}
			return nil, fmt.Errorf("fieldCompanionLease: %w", errors.Join(err, retireErr))
		}
	}
	packets := [][]byte{created}
	if target.IsHero {
		interrupts, interruptErr := targetMember.interruptNativeSilenceChannelsLocked(now, req.SourceTime)
		if interruptErr != nil {
			return nil, fmt.Errorf("fieldChannelInterrupt: %w", interruptErr)
		}
		packets = append(packets, interrupts...)
		e.registry.sessions[targetKey] = targetMember
		if targetKey == req.Address.String() {
			*caller = targetMember
		}
	}
	attribute, err := silenceFieldAttribute(world, target.ObjectID, e.registry, now)
	if err != nil {
		return nil, fmt.Errorf("fieldEnterAttribute: %w", err)
	}
	return append(packets, attribute), nil
}

func (e gameplayPendingRuntime) retireSilenceFieldModifierLocked(world *zone.Zone, contact zone.SilenceFieldContact) ([][]byte, error) {
	if contact.InstanceID == 0 {
		return nil, nil
	}
	deleted, err := effectraknet.ModifierDelete(contact.TargetObjectID, contact.InstanceID)
	if err != nil {
		return nil, fmt.Errorf("fieldDelete: %w", err)
	}
	modifier, isRemoved := world.Effect().Remove(contact.InstanceID)
	if !isRemoved {
		return nil, nil
	}
	if modifier.GUID != util.HashID(silenceFieldModifier) {
		return nil, errors.New("silence field modifier identity changed")
	}
	world.Companion().RemoveSilenceLease(contact.TargetObjectID, contact.InstanceID)
	err = e.modifierPool.Release(contact.InstanceID)
	if err != nil {
		return nil, fmt.Errorf("fieldRelease: %w", err)
	}
	attribute, err := silenceFieldAttribute(world, contact.TargetObjectID, e.registry, e.now())
	if err != nil {
		return nil, fmt.Errorf("fieldExitAttribute: %w", err)
	}
	return [][]byte{deleted, attribute}, nil
}
