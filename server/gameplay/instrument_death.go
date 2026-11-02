package gameplay

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/zone"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

// Only these original fixtures have the recovered 8x10 foundation envelope.
// Callback aliases keep their own imported model geometry.
func hasGraviticFoundationGeometry(nounName string) bool {
	return strings.EqualFold(nounName, "DEST_prefab_islands_instrument_scitech_7.Noun") ||
		strings.EqualFold(nounName, "DEST_prefab_islands_instrument_scitech_7_noShadow.Noun")
}

// This short world-owned modifier survives the already-dead instrument and
// its publishing transport. The publication gates landing behind actual birth
// delivery; the world timer owns both landing and the authored half-second outro.
type instrumentKnockbackRun struct {
	runtime          campaignDamageRuntime
	world            *zone.Zone
	modifier         *campaignNPCModifierRun
	actionGeneration uint64
	positionRevision uint64
	timestamp        uint64
	expiresAt        time.Time
	isPublished      bool
	isLanded         bool
}

func (e campaignFixtureBlastStep) instrumentKnockback(targetID uint32, profile zonenpc.ActionProfile) ([][]byte, error) {
	if profile.AbilityName != "InstrumentDeath" {
		return nil, nil
	}
	r := e.runtime
	r.registry.mutex.Lock()
	defer r.registry.mutex.Unlock()
	member, isFound := r.registry.sessions[e.sessionKey]
	if !isFound || member.generation != e.generation || member.zone == nil || member.zone.NPCs() != e.npc ||
		member.isZoneTerminal() || r.registry.isFixtureDiscardingLocked(member.binding.GameID) {
		return nil, nil
	}
	if e.publication == nil || r.registry.timer == nil {
		return nil, errors.New("instrument publication timer unavailable")
	}
	target, isTargetFound := member.zone.NPCs().LiveNPC(targetID)
	now := r.npc.now()
	if !isTargetFound || !target.IsPublished || target.IsMutationKnockbackImmune() ||
		target.Plan.IsBoss || target.HasAffixModifier(surefootedNPCAffixModifierName) ||
		member.zone.NPCs().RootRemaining(targetID, now) > 0 {
		return nil, nil
	}
	for _, modifier := range member.zone.Effect().Snapshot() {
		if modifier.TargetObjectID == targetID && modifier.GUID == profile.ModifierGUID() {
			return nil, nil
		}
	}
	delta := target.Plan.Position.Sub(e.source.Plan.Position)
	length := delta.Length()
	desired := target.Plan.Position
	if length > 0 {
		desired = desired.Add(delta.Scale(profile.ForcedMovementDistance / length))
	}
	destination, isDestinationFound, err := navigationClippedMovementDestination(
		member.zone.Navigation(), target.Plan.Position, desired,
		max(target.NavigationRadius(), float32(.25)), target.Navigation)
	if err != nil {
		return nil, fmt.Errorf("instrumentDestination: %w", err)
	}
	if !isDestinationFound {
		return nil, nil
	}
	plan := zonenpc.AttackPlan{SourceObjectID: e.source.Plan.ObjectID, TargetObjectID: targetID,
		SourcePosition: e.source.Plan.Position, TargetPosition: target.Plan.Position, Profile: profile}
	jumpPackets, err := npcraknet.ForcedJump(plan, destination, e.timestamp, [3]float32{2, 4, 4})
	if err != nil {
		return nil, fmt.Errorf("instrumentJump: %w", err)
	}
	modifier, err := newCampaignNPCModifierRun(r.npc.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("instrumentAllocate: %w", err)
	}
	travel := time.Duration(float64(destination.Sub(target.Plan.Position).Length()/profile.ForcedMovementSpeed) * float64(time.Second))
	duration := travel + 500*time.Millisecond
	run := &instrumentKnockbackRun{runtime: r, world: member.zone, modifier: modifier,
		timestamp: e.timestamp + uint64(travel/time.Millisecond),
		expiresAt: now.Add(duration)}
	modifier.record = zoneeffect.Modifier{InstanceID: modifier.instanceID, GUID: profile.ModifierGUID(),
		SourceObjectID: e.source.Plan.ObjectID, TargetObjectID: targetID, InitiatorObject: e.source.Plan.ObjectID,
		Rank: 1, StackCount: 1, Duration: duration, ExpiresAt: run.expiresAt, Kind: zoneeffect.ModifierKindDebuff}
	create, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
		SourceObjectID: e.source.Plan.ObjectID, TargetObjectID: targetID,
		ModifierID: modifier.record.GUID, InstanceID: modifier.instanceID,
		StackCount: 1, Duration: duration, Timestamp: e.timestamp})
	if err != nil {
		return nil, fmt.Errorf("instrumentCreate: %w", errors.Join(err, run.releaseLocked()))
	}
	err = member.zone.Effect().Put(modifier.record)
	if err != nil {
		return nil, fmt.Errorf("instrumentInventory: %w", errors.Join(err, run.releaseLocked()))
	}
	interruptPackets, err := r.registry.interruptCampaignNPCForForcedMovementLocked(e.sessionKey, &member, targetID)
	if err != nil {
		return nil, fmt.Errorf("instrumentInterrupt: %w", errors.Join(err, run.releaseLocked()))
	}
	err = member.zone.NPCs().SetPosition(targetID, destination)
	if err != nil {
		return nil, fmt.Errorf("instrumentPosition: %w", errors.Join(err, run.releaseLocked()))
	}
	err = member.zone.NPCs().ApplyStun(targetID, run.expiresAt)
	if err != nil {
		return nil, fmt.Errorf("instrumentHold: %w", errors.Join(err, run.releaseLocked()))
	}
	latest, isLatestFound := member.zone.NPCs().NPC(targetID)
	if !isLatestFound {
		return nil, fmt.Errorf("instrumentRecipient: %w", errors.Join(errors.New("recipient disappeared"), run.releaseLocked()))
	}
	run.actionGeneration, run.positionRevision = latest.ActionGeneration, latest.PositionRevision
	r.registry.sessions[e.sessionKey] = member
	err = run.scheduleLocked(max(time.Millisecond, travel))
	if err != nil {
		return nil, fmt.Errorf("instrumentSchedule: %w", errors.Join(err, run.releaseLocked()))
	}
	if !modifier.create() {
		return nil, fmt.Errorf("instrumentAdmission: %w", errors.Join(errors.New("modifier allocation retired"), run.releaseLocked()))
	}
	e.publication.instrumentKnockbacks = append(e.publication.instrumentKnockbacks, run)
	packets := append(interruptPackets, create)
	return append(packets, jumpPackets...), nil
}

func (e *instrumentKnockbackRun) scheduleLocked(delay time.Duration) error {
	cancel, err := e.runtime.registry.timer.Schedule(max(time.Millisecond, delay), e.execute)
	if err != nil {
		return fmt.Errorf("instrumentTimer: %w", err)
	}
	if cancel == nil {
		return errors.New("instrument timer cancellation unavailable")
	}
	e.modifier.cancel = cancel
	return nil
}

func (e *instrumentKnockbackRun) releaseLocked() error {
	removed, isRemoved := e.world.Effect().Remove(e.modifier.instanceID)
	var inventoryErr error
	if isRemoved && removed.TargetObjectID != e.modifier.record.TargetObjectID {
		inventoryErr = errors.New("instrument inventory recipient changed")
	}
	e.world.NPCs().ClearStun(e.modifier.record.TargetObjectID, e.expiresAt)
	if e.modifier.cancel != nil {
		e.modifier.cancel()
		e.modifier.cancel = nil
	}
	isCreated, err := e.modifier.release(e.runtime.npc.modifierPool)
	if err != nil {
		return fmt.Errorf("instrumentRelease: %w", errors.Join(err, inventoryErr))
	}
	if !isCreated {
		return inventoryErr
	}
	deleted, err := effectraknet.ModifierDelete(e.modifier.record.TargetObjectID, e.modifier.instanceID)
	if err != nil {
		return fmt.Errorf("instrumentDelete: %w", err)
	}
	e.runtime.registry.queueChannelPresentationLocked(e.world, [][]byte{deleted})
	return inventoryErr
}

func (e *instrumentKnockbackRun) execute() {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	err := e.advanceLocked()
	if err == nil {
		return
	}
	releaseErr := e.releaseLocked()
	if e.runtime.logger != nil {
		e.runtime.logger.Printf("Instrument knockback failed target=%d: %v", e.modifier.record.TargetObjectID, errors.Join(err, releaseErr))
	}
}

func (e *instrumentKnockbackRun) advanceLocked() error {
	if !e.modifier.isActive() {
		return nil
	}
	isWorldActive := false
	for _, member := range e.runtime.registry.sessions {
		if member.zone == e.world && !member.isZoneTerminal() {
			isWorldActive = true
			break
		}
	}
	if !isWorldActive || e.runtime.registry.isClosed {
		return e.releaseLocked()
	}
	if !e.isPublished {
		return e.scheduleLocked(10 * time.Millisecond)
	}
	if e.isLanded {
		return e.releaseLocked()
	}
	e.isLanded = true
	targetID := e.modifier.record.TargetObjectID
	target, isFound := e.world.NPCs().LiveNPC(targetID)
	if isFound && target.ActionGeneration == e.actionGeneration && target.PositionRevision == e.positionRevision {
		packets, err := npcraknet.MovementStop(targetID, target.Plan.Position)
		if err != nil {
			return fmt.Errorf("instrumentStop: %w", err)
		}
		reset, err := npcraknet.ResetAnimation(targetID, e.timestamp)
		if err != nil {
			return fmt.Errorf("instrumentReset: %w", err)
		}
		e.runtime.registry.queueChannelPresentationLocked(e.world, append(packets, reset))
	}
	return e.scheduleLocked(max(time.Millisecond, e.expiresAt.Sub(e.runtime.npc.now())))
}
