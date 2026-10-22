package gameplay

import (
	"fmt"
	"sort"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type basicHitModifierKey struct {
	targetID uint32
	modifier zonenpc.BasicModifierKey
}

type basicHitModifierRun struct {
	modifier           *campaignNPCModifierRun
	silence            *campaignNPCModifierRun
	state              zonenpc.BasicModifier
	timer              *time.Timer
	sourceID           uint32
	tickCount          uint32
	damage             game.DamageRange
	sourceCreature     game.GameplayCreature
	binding            game.GameplayBinding
	packet             raknet.Packet
	timestamp          uint64
	isMovementPending  bool
	movementDeadline   time.Time
	movementTimestamp  uint64
	movementGeneration uint64
	movementRevision   uint64
}

type basicHitModifierStep struct {
	runtime campaignDamageRuntime
	npc     *zonenpc.Session
	effect  *zoneeffect.Inventory
	key     basicHitModifierKey
	run     *basicHitModifierRun
}

func basicHitModifierKind(name string) (zonenpc.BasicModifierKind, string) {
	switch name {
	case "FireRavagerBasic":
		return zonenpc.BasicModifierBurn, "FireRavagerBurn"
	case "LightningTempest_Basic":
		return zonenpc.BasicModifierEnergyVulnerability, "Modifier_LightningTempest_Basic"
	case "LightspeedTempestBasic":
		return zonenpc.BasicModifierHaste, "LightSpeedHaste"
	case "Weakness":
		return zonenpc.BasicModifierWeakness, "WeaknessDebuff"
	case "Pummel":
		return zonenpc.BasicModifierPummel, "PummelDebuff"
	case "ForcePulse":
		return zonenpc.BasicModifierKnockback, "KnockbackWithImmunityModifier"
	default:
		return 0, ""
	}
}

func (e campaignDamageRuntime) applyBasicHitModifier(packet raknet.Packet, sessionKey string, generation uint64, sourceID uint32, timestamp uint64, result zoneability.AreaResult) ([][]byte, error) {
	kind, name := basicHitModifierKind(result.Definition.Name)
	if kind == 0 || result.Damage.IsDefeated {
		return nil, nil
	}
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	peerSession, isFound := e.registry.sessions[sessionKey]
	if !isFound || peerSession.generation != generation || peerSession.zone == nil || result.SourceRank == 0 {
		return nil, nil
	}
	npc, effect := peerSession.zone.NPCs(), peerSession.zone.Effect()
	target, isTargetFound := npc.NPC(result.Damage.ObjectID)
	if !isTargetFound || target.IsDefeated {
		return nil, nil
	}
	if kind == zonenpc.BasicModifierKnockback && (target.Plan.IsBoss || target.IsTurtleActive || target.HasAffixModifier(surefootedNPCAffixModifierName)) {
		return nil, nil
	}
	modifierKey := zonenpc.BasicModifierKey{Kind: kind}
	if kind == zonenpc.BasicModifierBurn || kind == zonenpc.BasicModifierKnockback {
		modifierKey.SourceObjectID = sourceID
	}
	key := basicHitModifierKey{targetID: result.Damage.ObjectID, modifier: modifierKey}
	now := e.npc.now()
	rank := result.SourceRank
	previousState := npc.BasicModifier(key.targetID, modifierKey)
	state, isApplied := npc.ApplyBasicModifier(key.targetID, modifierKey, rank, now)
	if !isApplied {
		return nil, nil
	}
	previous := e.registry.basicHitModifiers[npc][key]
	admission := basicHitModifierAdmission{runtime: e, npc: npc, effect: effect, targetID: key.targetID, state: state, previousState: previousState}
	defer admission.rollbackUnlessCommitted()
	packets := make([][]byte, 0, 4)
	var replaced *basicHitModifierRun
	if previous != nil && (kind == zonenpc.BasicModifierBurn || state.StackCount == 1) {
		replaced = previous
		previous = nil
	}
	var modifier *campaignNPCModifierRun
	if previous != nil {
		modifier = previous.modifier
	} else {
		var err error
		modifier, err = newCampaignNPCModifierRun(e.npc.modifierPool)
		if err != nil {
			return nil, fmt.Errorf("basicAllocate: %w", err)
		}
		admission.isAllocated = true
	}
	admission.modifier = modifier
	admission.previousRecord = modifier.record
	record := modifier.record
	if previous == nil {
		record = zoneeffect.Modifier{InstanceID: modifier.instanceID, GUID: util.HashID(name), SourceObjectID: sourceID, TargetObjectID: key.targetID, Rank: state.Rank, Kind: zoneeffect.ModifierKindDebuff, InitiatorObject: sourceID}
	}
	record.StackCount = state.StackCount
	record.Duration = state.ExpiresAt.Sub(now)
	record.ExpiresAt = state.ExpiresAt
	if kind == zonenpc.BasicModifierHaste {
		record.Kind = zoneeffect.ModifierKindBuff
		record.IsHaste = true
		record.AttackSpeed = .08
		record.MovementSpeedBuff = .08
	}
	if kind == zonenpc.BasicModifierEnergyVulnerability {
		record.EnergyDamageTakenIncrease = .1 * float32(state.StackCount)
	}
	createPacket, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{SourceObjectID: record.SourceObjectID, TargetObjectID: key.targetID, ModifierID: record.GUID, InstanceID: record.InstanceID, StackCount: state.StackCount, Duration: record.Duration, Timestamp: timestamp})
	if err != nil {
		return nil, fmt.Errorf("basicCreate: %w", err)
	}
	if previous == nil {
		err = effect.Put(record)
	} else {
		err = effect.Update(record)
	}
	if err != nil {
		return nil, fmt.Errorf("basicInventory: %w", err)
	}
	admission.isInventoryChanged = true
	if previous == nil {
		modifier.create()
	}
	modifier.record = record
	run := &basicHitModifierRun{modifier: modifier, state: state, sourceID: record.SourceObjectID, sourceCreature: result.SourceCreature, binding: peerSession.binding, packet: packet, timestamp: timestamp}
	if kind == zonenpc.BasicModifierBurn {
		creature := result.SourceCreature
		definition := sim.AbilityDefinition{Name: "FireRavagerBurn", MinimumDamage: 1, MaximumDamage: 1, DamageCoefficient: .05, DamageType: 3, DamageSource: 1, IsDamageTypeFound: true, IsDamageSourceFound: true, IsDescriptorFound: true, DescriptorMask: 36}
		run.damage, err = zoneability.ProjectDamage(creature, definition, 1, 1)
		if err != nil {
			return nil, fmt.Errorf("basicBurnDamage: %w", err)
		}
	}
	packets = append(packets, createPacket)
	if kind == zonenpc.BasicModifierPummel && now.Before(state.ActiveUntil) {
		run.silence, err = newCampaignNPCModifierRun(e.npc.modifierPool)
		if err != nil {
			return nil, fmt.Errorf("basicSilenceAllocate: %w", err)
		}
		admission.silence = run.silence
		silencePacket, silenceErr := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{SourceObjectID: sourceID, TargetObjectID: key.targetID, ModifierID: util.HashID("PummelSilence"), InstanceID: run.silence.instanceID, Duration: state.ActiveUntil.Sub(now), Timestamp: timestamp})
		if silenceErr != nil {
			return nil, fmt.Errorf("basicSilenceCreate: %w", silenceErr)
		}
		run.silence.create()
		packets = append(packets, silencePacket)
	}
	if kind == zonenpc.BasicModifierKnockback {
		movementPackets, movementErr := e.applyBasicKnockbackLocked(sessionKey, &peerSession, target, timestamp)
		if movementErr != nil {
			return nil, fmt.Errorf("basicKnockback: %w", movementErr)
		}
		packets = append(packets, movementPackets...)
		run.isMovementPending = len(movementPackets) > 0
		if run.isMovementPending {
			moved, isMovedFound := npc.NPC(key.targetID)
			if isMovedFound {
				travel := time.Duration(float64(moved.Plan.Position.Sub(target.Plan.Position).Length()/25) * float64(time.Second))
				run.movementDeadline = e.npc.now().Add(travel)
				run.movementTimestamp = timestamp + uint64(travel/time.Millisecond)
				run.movementGeneration = moved.ActionGeneration
				run.movementRevision = moved.PositionRevision
			}
		}
		e.registry.sessions[sessionKey] = peerSession
	}
	if e.registry.basicHitModifiers == nil {
		e.registry.basicHitModifiers = make(map[*zonenpc.Session]map[basicHitModifierKey]*basicHitModifierRun)
	}
	if replaced != nil {
		deleted, retireErr := e.registry.retireBasicHitModifierLocked(npc, effect, key, e.npc.modifierPool)
		if retireErr != nil {
			return nil, fmt.Errorf("basicPrevious: %w", retireErr)
		}
		packets = append(deleted, packets...)
	}
	if previous != nil {
		previous.timer.Stop()
	}
	if e.registry.basicHitModifiers[npc] == nil {
		e.registry.basicHitModifiers[npc] = make(map[basicHitModifierKey]*basicHitModifierRun)
	}
	e.registry.basicHitModifiers[npc][key] = run
	step := basicHitModifierStep{runtime: e, npc: npc, effect: effect, key: key, run: run}
	run.timer = time.AfterFunc(step.nextDelay(now), step.execute)
	admission.isCommitted = true
	return packets, nil
}

func (e basicHitModifierStep) nextDelay(at time.Time) time.Duration {
	if e.key.modifier.Kind == zonenpc.BasicModifierBurn {
		deadline := e.run.state.ExpiresAt.Add(-4 * time.Second).Add(time.Duration(e.run.tickCount+1) * 500 * time.Millisecond)
		return max(time.Millisecond, deadline.Sub(at))
	}
	if e.run.silence != nil && at.Before(e.run.state.ActiveUntil) {
		return e.run.state.ActiveUntil.Sub(at)
	}
	if e.run.isMovementPending {
		return max(time.Millisecond, e.run.movementDeadline.Sub(at))
	}
	return max(time.Millisecond, e.run.state.ExpiresAt.Sub(at))
}

func (e *gameplaySessionRegistry) retireBasicHitModifierLocked(npc *zonenpc.Session, effect *zoneeffect.Inventory, key basicHitModifierKey, pool *modifierPool) ([][]byte, error) {
	runs := e.basicHitModifiers[npc]
	run := runs[key]
	if run == nil {
		return nil, nil
	}
	run.timer.Stop()
	npc.RemoveBasicModifier(key.targetID, run.state)
	effect.Remove(run.modifier.instanceID)
	delete(runs, key)
	if len(runs) == 0 {
		delete(e.basicHitModifiers, npc)
	}
	packets := make([][]byte, 0, 2)
	for _, modifier := range []*campaignNPCModifierRun{run.modifier, run.silence} {
		if modifier == nil {
			continue
		}
		isCreated, err := modifier.release(pool)
		if err != nil {
			return nil, fmt.Errorf("basicRelease: %w", err)
		}
		if !isCreated {
			continue
		}
		packet, err := effectraknet.ModifierDelete(key.targetID, modifier.instanceID)
		if err != nil {
			return nil, fmt.Errorf("basicDelete: %w", err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func (e *gameplaySessionRegistry) basicHitModifierReplay(peerSession gameplayPeerSession, at time.Time, timestamp uint64) ([][]byte, error) {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	if peerSession.zone == nil {
		return nil, nil
	}
	runs := e.basicHitModifiers[peerSession.zone.NPCs()]
	keys := make([]basicHitModifierKey, 0, len(runs))
	for key := range runs {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].targetID != keys[j].targetID {
			return keys[i].targetID < keys[j].targetID
		}
		if keys[i].modifier.Kind != keys[j].modifier.Kind {
			return keys[i].modifier.Kind < keys[j].modifier.Kind
		}
		return keys[i].modifier.SourceObjectID < keys[j].modifier.SourceObjectID
	})
	packets := make([][]byte, 0, len(keys))
	for _, key := range keys {
		run := runs[key]
		if !at.Before(run.state.ExpiresAt) || peerSession.zone.NPCs().BasicModifier(key.targetID, key.modifier) != run.state {
			continue
		}
		record := run.modifier.record
		packet, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{SourceObjectID: record.SourceObjectID, TargetObjectID: key.targetID, ModifierID: record.GUID, InstanceID: record.InstanceID, StackCount: record.StackCount, Duration: run.state.ExpiresAt.Sub(at), Timestamp: timestamp})
		if err != nil {
			return nil, fmt.Errorf("basicReplay: %w", err)
		}
		packets = append(packets, packet)
		if run.silence != nil && at.Before(run.state.ActiveUntil) {
			packet, err = effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{SourceObjectID: record.SourceObjectID, TargetObjectID: key.targetID, ModifierID: util.HashID("PummelSilence"), InstanceID: run.silence.instanceID, Duration: run.state.ActiveUntil.Sub(at), Timestamp: timestamp})
			if err != nil {
				return nil, fmt.Errorf("basicSilenceReplay: %w", err)
			}
			packets = append(packets, packet)
		}
	}
	return packets, nil
}
