package gameplay

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const supportHealerName = "SupportHealerSupport"

func (e targetedAOESchedule) queueSupportHealerPacketsLocked(npc *zonenpc.Session, packets [][]byte) {
	for key, member := range e.runtime.registry.sessions {
		if member.zone == nil || member.zone.NPCs() != npc {
			continue
		}
		member.queueCampaignPackets(packets)
		e.runtime.registry.sessions[key] = member
	}
}

func (e targetedAOESchedule) commitSupportHealerPulseLocked(caster gameplayPeerSession, plan zoneability.AreaPlan, timestamp uint64) ([]zoneability.AreaResult, [][]byte, error) {
	results := make([]zoneability.AreaResult, 0, len(plan.Target))
	packets := make([][]byte, 0)
	for _, target := range plan.Target {
		single := plan
		single.Target = []zonenpc.Snapshot{target}
		committed, err := zoneability.CommitArea(caster.zone.Population().Random(), caster.zone.NPCs(), single, e.creature, caster.binding.Difficulty, e.runtime.program.Critical)
		if err != nil {
			return results, packets, fmt.Errorf("briarPulseDamage: %w", err)
		}
		results = append(results, committed...)
		for _, result := range committed {
			if result.Damage.IsDefeated || result.Damage.IsDamageImmune {
				continue
			}
			movementScale := float32(0.25)
			if e.rank == 7 || e.rank == 8 {
				movementScale = 0.4
			}
			err = caster.zone.NPCs().ApplySlow(result.Damage.ObjectID, e.runtime.now().Add(zonenpc.CrowdControlDuration(target.Plan, time.Second)), movementScale, 1)
			if err != nil {
				return results, packets, fmt.Errorf("briarSnare: %w", err)
			}
		}
		rootPackets, rootErr := e.applySupportHealerRootsLocked(caster, committed, timestamp)
		packets = append(packets, rootPackets...)
		if rootErr != nil {
			e.runtime.logger.Printf("Strangling Briars root callback: %v", rootErr)
		}
	}
	return results, packets, nil
}

func projectSupportHealerRank(definition sim.AbilityDefinition, rank int32) sim.AbilityDefinition {
	if definition.Name != supportHealerName {
		return definition
	}
	definition.MinimumHealingPerTick = 0
	definition.MaximumHealingPerTick = 0
	if rank == 5 || rank == 6 {
		definition.MinimumHealingPerTick = 1
		definition.MaximumHealingPerTick = 1
		definition.EffectNouns = []string{"SphereOfTransfusion_Flowers.Noun"}
	}
	if rank == 7 || rank == 8 {
		definition.EffectNouns = []string{"SphereOfTransfusion_Roots.Noun"}
	}
	return definition
}

func supportHealerRootChance(rank int32) float64 {
	if rank == 7 {
		return float64(float32(0.2))
	}
	if rank == 8 {
		// Chunk432's last entry is literally20. GetRankedValueHelper returns
		// it unchanged; keep the authored comparison rather than correcting it.
		return 20
	}
	return 0
}

type supportHealerRootRun struct {
	runtime    campaignAbilityCommandRuntime
	npc        *zonenpc.Session
	targetID   uint32
	instanceID uint32
	sourceID   uint32
	expiresAt  time.Time
	rank       int32
	cancel     func()
}

func (e *gameplaySessionRegistry) supportHealerRootReplay(member gameplayPeerSession, at time.Time, timestamp uint64) ([][]byte, error) {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	if member.zone == nil {
		return nil, nil
	}
	npc := member.zone.NPCs()
	objectIDs := make([]uint32, 0, len(e.supportHealerRoots[npc]))
	for objectID := range e.supportHealerRoots[npc] {
		objectIDs = append(objectIDs, objectID)
	}
	sort.Slice(objectIDs, func(i, j int) bool { return objectIDs[i] < objectIDs[j] })
	packets := make([][]byte, 0, len(objectIDs))
	for _, objectID := range objectIDs {
		run := e.supportHealerRoots[npc][objectID]
		if !npc.IsSupportHealerRootCurrent(objectID, run.instanceID, at) {
			continue
		}
		packet, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{TargetID: objectID, SourceID: run.sourceID,
			ModifierGUID: util.HashID("SupportHealer_RootModifier"), InstanceID: run.instanceID,
			DurationMilliseconds: uint32(run.expiresAt.Sub(at) / time.Millisecond), StartMilliseconds: timestamp, StackCount: 1})
		if err != nil {
			return nil, fmt.Errorf("briarRootReplay: %w", err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func (e *supportHealerRootRun) execute() {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	if e.runtime.registry.supportHealerRoots[e.npc][e.targetID] != e {
		return
	}
	packets, err := e.runtime.registry.retireSupportHealerRootLocked(e.npc, e.targetID)
	if err != nil {
		e.runtime.logger.Printf("Strangling Briars root expiry target=%d: %v", e.targetID, err)
	}
	for key, member := range e.runtime.registry.sessions {
		if member.zone == nil || member.zone.NPCs() != e.npc {
			continue
		}
		member.queueCampaignPackets(packets)
		e.runtime.registry.sessions[key] = member
	}
}

func (e *gameplaySessionRegistry) retireSupportHealerRootLocked(npc *zonenpc.Session, targetID uint32) ([][]byte, error) {
	run := e.supportHealerRoots[npc][targetID]
	if run == nil {
		return nil, nil
	}
	packet, err := raknet.MarshalApplication(raknet.ModifierDeletedMessage{TargetID: targetID, InstanceID: run.instanceID})
	if err != nil {
		return nil, fmt.Errorf("briarRootDelete: %w", err)
	}
	return e.retireSupportHealerRootPreparedLocked(run, packet)
}

func (e *gameplaySessionRegistry) retireSupportHealerRootPreparedLocked(run *supportHealerRootRun, packet []byte) ([][]byte, error) {
	npc, targetID := run.npc, run.targetID
	delete(e.supportHealerRoots[npc], targetID)
	if len(e.supportHealerRoots[npc]) == 0 {
		delete(e.supportHealerRoots, npc)
	}
	if run.cancel != nil {
		run.cancel()
		run.cancel = nil
	}
	npc.ClearSupportHealerRoot(targetID, run.instanceID)
	err := run.runtime.modifierPool.Release(run.instanceID)
	if err != nil {
		return [][]byte{packet}, fmt.Errorf("briarRootRelease: %w", err)
	}
	return [][]byte{packet}, nil
}

// Called for every still-hostile recipient, even when TakeDamage returned
// false. Template956 places OnDamage after its success-gated modifier loop.
func (e targetedAOESchedule) applySupportHealerRootsLocked(peerSession gameplayPeerSession, results []zoneability.AreaResult, timestamp uint64) ([][]byte, error) {
	if e.definition.Name != supportHealerName {
		return nil, nil
	}
	packets := make([][]byte, 0)
	npc := peerSession.zone.NPCs()
	for _, result := range results {
		targetID := result.Damage.ObjectID
		target, isFound := npc.NPC(targetID)
		if !isFound || target.IsDefeated || target.HitPoint <= 0 || target.Faction != zonenpc.FactionNonPlayerAligned {
			continue
		}
		roll, err := sim.ScriptRandom(peerSession.zone.Population().Random())
		if err != nil {
			return packets, fmt.Errorf("briarRootRoll: %w", err)
		}
		if roll >= supportHealerRootChance(e.rank) {
			continue
		}
		if e.runtime.npc.timer == nil {
			return packets, errors.New("support root timer unavailable")
		}
		instanceID, err := e.runtime.modifierPool.Allocate()
		if err != nil {
			return packets, fmt.Errorf("briarRootAllocate: %w", err)
		}
		duration := zonenpc.CrowdControlDuration(target.Plan, 2*time.Second)
		create, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{TargetID: targetID, SourceID: e.sourceObjectID,
			ModifierGUID: util.HashID("SupportHealer_RootModifier"), InstanceID: instanceID,
			DurationMilliseconds: uint32(duration / time.Millisecond), StartMilliseconds: timestamp, StackCount: 1})
		if err != nil {
			releaseErr := e.runtime.modifierPool.Release(instanceID)
			return packets, fmt.Errorf("briarRootCreate: %w", errors.Join(err, releaseErr))
		}
		run := &supportHealerRootRun{runtime: e.runtime, npc: npc, targetID: targetID, instanceID: instanceID,
			sourceID: e.sourceObjectID, expiresAt: e.runtime.now().Add(duration), rank: e.rank}
		cancel, err := e.runtime.npc.timer.Schedule(duration, run.execute)
		if err != nil || cancel == nil {
			if cancel != nil {
				cancel()
			}
			if err == nil {
				err = errors.New("root cancellation unavailable")
			}
			releaseErr := e.runtime.modifierPool.Release(instanceID)
			return packets, fmt.Errorf("briarRootSchedule: %w", errors.Join(err, releaseErr))
		}
		run.cancel = cancel
		previous := e.runtime.registry.supportHealerRoots[npc][targetID]
		var previousDelete []byte
		if previous != nil {
			previousDelete, err = raknet.MarshalApplication(raknet.ModifierDeletedMessage{TargetID: targetID, InstanceID: previous.instanceID})
			if err != nil {
				cancel()
				releaseErr := e.runtime.modifierPool.Release(instanceID)
				return packets, fmt.Errorf("briarPreviousDelete: %w", errors.Join(err, releaseErr))
			}
		}
		isApplied, err := npc.ApplySupportHealerRoot(targetID, instanceID, run.expiresAt)
		if err != nil || !isApplied {
			cancel()
			releaseErr := e.runtime.modifierPool.Release(instanceID)
			if err != nil || releaseErr != nil {
				return packets, fmt.Errorf("briarRootApply: %w", errors.Join(err, releaseErr))
			}
			continue
		}
		if previous != nil {
			retired, retireErr := e.runtime.registry.retireSupportHealerRootPreparedLocked(previous, previousDelete)
			packets = append(packets, retired...)
			if retireErr != nil {
				e.runtime.logger.Printf("Strangling Briars replaced root target=%d: %v", targetID, retireErr)
			}
		}
		if e.runtime.registry.supportHealerRoots == nil {
			e.runtime.registry.supportHealerRoots = make(map[*zonenpc.Session]map[uint32]*supportHealerRootRun)
		}
		if e.runtime.registry.supportHealerRoots[npc] == nil {
			e.runtime.registry.supportHealerRoots[npc] = make(map[uint32]*supportHealerRootRun)
		}
		e.runtime.registry.supportHealerRoots[npc][targetID] = run
		packets = append(packets, create)
	}
	return packets, nil
}

func supportHealerHealingPackets(sourceID, targetID uint32, before, hitPoint, healing float32, isCritical bool) ([][]byte, error) {
	packets := make([][]byte, 0, 2)
	flags := uint16(2)
	if isCritical {
		flags |= 8
	}
	for _, message := range []raknet.ApplicationMessage{
		raknet.DamageCombatEventMessage{Flags: flags, TargetID: targetID, SourceID: sourceID, DeltaHealth: -healing, IntegerHPChange: int32(before) - int32(hitPoint)},
		raknet.CombatantDataDeltaMessage{ObjectID: targetID, HitPoints: hitPoint, IsHitPointChanged: true},
	} {
		packet, err := raknet.MarshalApplication(message)
		if err != nil {
			return nil, fmt.Errorf("briarHealMarshal: %w", err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func (e targetedAOESchedule) criticalSupportHealingLocked(caster gameplayPeerSession, pulse sim.AreaPulseIntent) (sim.CriticalDamageResult, error) {
	// HealDamage uses the captured source's critical rating and auto-crit,
	// with MagicNumbers' ordinary multiplier and no CriticalDamageIncrease.
	multiplier := e.runtime.program.Critical.DamageBonus
	if multiplier <= 0 || math.IsNaN(float64(multiplier)) || math.IsInf(float64(multiplier), 0) {
		return sim.CriticalDamageResult{}, errors.New("invalid healing critical multiplier")
	}
	minimum := float64(pulse.HealingMinimum)
	maximum := float64(pulse.HealingMaximum)
	if math.IsNaN(minimum) || math.IsInf(minimum, 0) ||
		math.IsNaN(maximum) || math.IsInf(maximum, 0) || minimum < 0 || maximum < minimum {
		return sim.CriticalDamageResult{}, errors.New("invalid healing range")
	}
	random := caster.zone.Population().Random()
	if random == nil {
		return sim.CriticalDamageResult{}, errors.New("healing random unavailable")
	}
	// Native 9BCEF0 samples a continuous float range, including one draw
	// when both endpoints are equal. Healing is not an integer damage range.
	amount := float32(minimum + (maximum-minimum)*random.Float64())
	amount, err := zoneability.ProjectHealing(e.creature, e.definition, amount)
	if err != nil {
		return sim.CriticalDamageResult{}, fmt.Errorf("briarHealProject: %w", err)
	}
	isCritical, err := sim.RollCritical(caster.zone.Population().Random(), e.creature.CriticalRating,
		e.creature.AutoCrit, e.binding.Difficulty, e.runtime.program.Critical.RatingConversions)
	if err != nil {
		return sim.CriticalDamageResult{}, fmt.Errorf("briarHealCritical: %w", err)
	}
	if isCritical {
		amount *= multiplier
	}
	if math.IsNaN(float64(amount)) || math.IsInf(float64(amount), 0) {
		return sim.CriticalDamageResult{}, errors.New("invalid critical healing")
	}
	return sim.CriticalDamageResult{Damage: amount, IsCritical: isCritical}, nil
}

func (e targetedAOESchedule) healSupportHealerPartyLocked(caster *gameplayPeerSession, pulse sim.AreaPulseIntent) ([][]byte, error) {
	if e.definition.Name != supportHealerName {
		return nil, nil
	}
	packets := make([][]byte, 0)
	for key, member := range e.runtime.registry.sessions {
		if key == e.sessionKey {
			member = *caster
		}
		if !isActivePartyRecipient(member, *caster) || member.deployedHitPoint() <= 0 || game.Vec3(member.playerPosition).Sub(game.Vec3(pulse.Position)).Length() > pulse.Radius {
			continue
		}
		critical, criticalErr := e.criticalSupportHealingLocked(*caster, pulse)
		if criticalErr != nil {
			return packets, fmt.Errorf("briarHeroCritical: %w", criticalErr)
		}
		reduced, reduceErr := game.ApplyTargetHealingReduction(critical.Damage, member.binding.Creatures[member.deployedCreatureIndex].HealingTargetProfile)
		if reduceErr != nil {
			return packets, fmt.Errorf("briarHealReduction: %w", reduceErr)
		}
		if reduced <= 0 {
			continue
		}
		before := member.deployedHitPoint()
		hitPoint := min(member.characterHitPointMaximum(member.deployedCreatureIndex), before+reduced)
		healedPackets, marshalErr := supportHealerHealingPackets(e.sourceObjectID, member.deployedObjectID, before, hitPoint, reduced, critical.IsCritical)
		if marshalErr != nil {
			return packets, fmt.Errorf("briarHeroPackets: %w", marshalErr)
		}
		resource, resourceErr := member.marshalCampaignCharacterResourceValues(member.deployedCreatureIndex, hitPoint, member.deployedManaPoint())
		if resourceErr != nil {
			return packets, fmt.Errorf("briarHeroResource: %w", resourceErr)
		}
		isGameOver, commitErr := member.setDeployedHitPoints(hitPoint)
		if commitErr != nil {
			return packets, fmt.Errorf("briarHeroCommit: %w", commitErr)
		}
		if isGameOver {
			return packets, errors.New("healed squad became terminal")
		}
		member.queueStatDelta(sporenet.PlayerStatDelta{PVEHealingReceived: float64(hitPoint - before)})
		if key == e.sessionKey {
			*caster = member
		} else {
			e.runtime.registry.sessions[key] = member
			member.zone.PublishHeroResourceTo(member.binding.UserID, member.generation)
		}
		packets = append(packets, healedPackets...)
		packets = append(packets, resource)
		caster.queueStatDelta(sporenet.PlayerStatDelta{PVEHealing: float64(hitPoint - before)})
	}
	for _, actor := range caster.zone.Companion().Snapshots() {
		if !e.runtime.registry.isActivePartyCompanionLocked(actor, *caster) || !actor.IsTargetable || actor.HitPoint <= 0 || game.Vec3(pulse.Position).Sub(actor.PositionAt(e.runtime.now())).Length() > pulse.Radius {
			continue
		}
		critical, criticalErr := e.criticalSupportHealingLocked(*caster, pulse)
		if criticalErr != nil {
			return packets, fmt.Errorf("briarPetCritical: %w", criticalErr)
		}
		hitPoint := min(actor.MaximumHitPoint, actor.HitPoint+critical.Damage)
		if hitPoint <= actor.HitPoint {
			continue
		}
		healedPackets, marshalErr := supportHealerHealingPackets(e.sourceObjectID, actor.ObjectID, actor.HitPoint, hitPoint, critical.Damage, critical.IsCritical)
		if marshalErr != nil {
			return packets, fmt.Errorf("briarPetPackets: %w", marshalErr)
		}
		isRestored, restoreErr := caster.zone.Companion().RestoreHitPoint(actor, hitPoint)
		if restoreErr != nil {
			return packets, fmt.Errorf("briarPetCommit: %w", restoreErr)
		}
		if !isRestored {
			continue
		}
		caster.queueStatDelta(sporenet.PlayerStatDelta{PVEHealing: float64(hitPoint - actor.HitPoint)})
		caster.zone.PublishCompanionResourceTo(actor.UserID, actor.PeerGeneration, actor.ObjectID)
		packets = append(packets, healedPackets...)
	}
	for _, actor := range caster.zone.NPCs().LiveSnapshots() {
		if actor.Faction != zonenpc.FactionPlayerAligned || actor.Plan.IsFixture || !actor.IsPublished || actor.HitPoint <= 0 || game.Vec3(pulse.Position).Sub(actor.Plan.Position).Length() > pulse.Radius {
			continue
		}
		critical, criticalErr := e.criticalSupportHealingLocked(*caster, pulse)
		if criticalErr != nil {
			return packets, fmt.Errorf("briarNPCCritical: %w", criticalErr)
		}
		if critical.Damage <= 0 {
			continue
		}
		expected, reduced, hitPoint, previewErr := caster.zone.NPCs().PreviewHealing(actor.Plan.ObjectID, critical.Damage, e.runtime.now())
		if previewErr != nil {
			return packets, fmt.Errorf("briarNPCPreview: %w", previewErr)
		}
		if hitPoint <= expected.HitPoint {
			continue
		}
		healedPackets, marshalErr := supportHealerHealingPackets(e.sourceObjectID, actor.Plan.ObjectID, expected.HitPoint, hitPoint, reduced, critical.IsCritical)
		if marshalErr != nil {
			return packets, fmt.Errorf("briarNPCPackets: %w", marshalErr)
		}
		isRestored, restoreErr := caster.zone.NPCs().RestoreHealing(expected, hitPoint)
		if restoreErr != nil {
			return packets, fmt.Errorf("briarNPCCommit: %w", restoreErr)
		}
		if !isRestored {
			continue
		}
		caster.queueStatDelta(sporenet.PlayerStatDelta{PVEHealing: float64(hitPoint - expected.HitPoint)})
		packets = append(packets, healedPackets...)
	}
	return packets, nil
}
