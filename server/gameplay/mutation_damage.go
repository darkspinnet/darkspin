package gameplay

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/darkspinnet/darkspin/server/util"
	"github.com/darkspinnet/darkspin/server/zone"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type mutationDamageKey struct {
	world *zone.Zone
	id    zonenpc.MutationEventID
}

type mutationDamageRun struct {
	targets          map[uint32]*enemyDamageReceipt
	packets          [][]byte
	stopWorldCleanup func() bool
}

type mutationDamageWorldCleanup struct {
	runtime gameplayPendingRuntime
	world   *zone.Zone
}

func (e mutationDamageWorldCleanup) retire() {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	e.runtime.retireMutationAuraDamageWorldLocked(e.world)
}

func (e campaignNPCActionRuntime) mutationHeroTargetSession(member *gameplayPeerSession, target zone.NPCTarget) (*gameplayPeerSession, string) {
	owner, key := e.heroTargetSession(member, target)
	if owner != nil || member == nil || !target.IsHero {
		return owner, key
	}
	for _, retained := range e.registry.retainedSessions {
		candidate := &retained.peerSession
		if candidate.zone == member.zone && candidate.binding.UserID == target.UserID &&
			candidate.generation == target.PeerGeneration && candidate.deployedObjectID == target.ObjectID {
			return candidate, ""
		}
	}
	return nil, ""
}

func (e campaignNPCActionRuntime) retainMutationTargetSessionLocked(member *gameplayPeerSession, key string) {
	if member != nil && key != "" {
		e.registry.sessions[key] = *member
	}
}

func (e gameplayPendingRuntime) retainMutationDamageMemberLocked(member *gameplayPeerSession) {
	e.registry.storeMutationDamageMemberLocked(member)
}

func (e *gameplaySessionRegistry) storeMutationDamageMemberLocked(member *gameplayPeerSession) {
	for key, candidate := range e.sessions {
		if candidate.zone == member.zone && gameplaySessionMemberKey(candidate) == gameplaySessionMemberKey(*member) &&
			candidate.generation == member.generation && candidate.transportGeneration == member.transportGeneration {
			e.sessions[key] = *member
			return
		}
	}
}

func (e *gameplaySessionRegistry) refreshMutationDamageMemberLocked(member *gameplayPeerSession) {
	for _, candidate := range e.sessions {
		if candidate.zone == member.zone && gameplaySessionMemberKey(candidate) == gameplaySessionMemberKey(*member) &&
			candidate.generation == member.generation && candidate.transportGeneration == member.transportGeneration {
			*member = candidate
			return
		}
	}
}

// Call only after the exact returned packets have transferred to the reliable
// world publication owner. Later target failures must not replay that batch.
func (e gameplayPendingRuntime) acceptMutationAuraDamagePacketsLocked(world *zone.Zone, id zonenpc.MutationEventID) {
	run := e.registry.mutationDamageWorks[mutationDamageKey{world: world, id: id}]
	if run == nil {
		return
	}
	run.packets = nil
}

func (e gameplayPendingRuntime) releaseMutationAuraDamageLocked(world *zone.Zone, id zonenpc.MutationEventID) {
	key := mutationDamageKey{world: world, id: id}
	run := e.registry.mutationDamageWorks[key]
	if run == nil || len(run.packets) == 0 {
		if run != nil && run.stopWorldCleanup != nil {
			run.stopWorldCleanup()
		}
		delete(e.registry.mutationDamageWorks, key)
	}
}

func (e gameplayPendingRuntime) retireMutationAuraDamageWorldLocked(world *zone.Zone) {
	for key := range e.registry.mutationDamageWorks {
		if key.world == world {
			run := e.registry.mutationDamageWorks[key]
			if run.stopWorldCleanup != nil {
				run.stopWorldCleanup()
			}
			delete(e.registry.mutationDamageWorks, key)
		}
	}
}

// enemyDamageReceipt belongs to one accepted target operation. It separates
// committed authority from the fallible presentation/transition continuation.
type enemyDamageReceipt struct {
	registry                 *gameplaySessionRegistry
	EventID                  zonenpc.MutationEventID
	Faction                  zonenpc.Faction
	DeathRevision            uint64
	IsCommitted              bool
	IsComplete               bool
	IsDefenseRolled          bool
	DefenseFlags             uint16
	Target                   zone.NPCTarget
	Plan                     zonenpc.AttackPlan
	Result                   zonenpc.AttackResult
	Damage                   zone.NPCTargetDamage
	Distribution             soulLinkDistribution
	Vulnerability            *campaignNPCPhysicalVulnerabilityRun
	ShieldPackets            [][]byte
	Absorbed                 float32
	VulnerabilityPackets     [][]byte
	EnergyPackets            [][]byte
	Packets                  [][]byte
	Stats                    sporenet.PlayerStatDelta
	Phase                    uint8
	IsShareCommitted         bool
	SharePackets             [][]byte
	SharedDamage             float32
	HitEffect                []byte
	Shield                   mutationShieldReceipt
	HeroTransitionPackets    [][]byte
	IsHeroBusinessDone       bool
	IsHeroOutputPrepared     bool
	IsHeroInitialCleanupDone bool
	AreHeroDrainsDone        bool
	AreHeroPullsDone         bool
	IsHeroHeaderPrepared     bool
	IsHeroRevivalPending     bool
	IsHeroExpectedGameOver   bool
}

func mutationPrivateDamagePackets(packets [][]byte) [][]byte {
	privatePackets := make([][]byte, 0)
	for _, packet := range packets {
		if len(gameplayPeerPresentationPackets([][]byte{packet})) == 0 {
			privatePackets = append(privatePackets, packet)
		}
	}
	return privatePackets
}

func (e *enemyDamageReceipt) retainHeroTransitionOutput(packets *[][]byte, baseCount int) {
	e.HeroTransitionPackets = append([][]byte(nil), (*packets)[baseCount:]...)
}

func (e *gameplayPeerSession) stopMutationPullEffectsTargeting(objectID uint32, receipt *enemyDamageReceipt) ([][]byte, error) {
	if receipt.registry == nil {
		return nil, errors.New("mutation pull removal owner unavailable")
	}
	for sourceID, run := range e.campaignNPCPullEffects {
		if !run.targets(objectID) {
			continue
		}
		run.mutex.Lock()
		if run.isReleased {
			run.mutex.Unlock()
			continue
		}
		for len(run.attachments) != 0 {
			attachment := run.attachments[0]
			packet, err := npcraknet.ForcedMovementEffect(attachment.objectID, attachment.slot, "", true)
			if err != nil {
				run.mutex.Unlock()
				return nil, fmt.Errorf("mutationPullEncode: %w", err)
			}
			removalLease, err := receipt.registry.adoptMutationRemovalLocked(e, run.pool, attachment.objectID, attachment.slot, nil, [][]byte{packet})
			if err != nil {
				run.mutex.Unlock()
				return nil, fmt.Errorf("mutationPullAdopt: %w", err)
			}
			if removalLease == nil {
				run.mutex.Unlock()
				return nil, errors.New("mutation pull removal lease unavailable")
			}
			// The journal now owns this slot and its pending removal. Retain
			// only unadopted attachments if a later adoption must be retried.
			run.attachments = run.attachments[1:]
		}
		run.isReleased = true
		run.mutex.Unlock()
		delete(e.campaignNPCPullEffects, sourceID)
	}
	return nil, nil
}

// The caller owns registry.mutex and the exact world event's publication.
// Every target roll is frozen before the first authority mutation. A failure
// keeps its target pending while the other targets still receive their turn.
func (e gameplayPendingRuntime) applyMutationAuraDamageLocked(member *gameplayPeerSession, event zonenpc.MutationEvent, timestamp uint64) ([][]byte, error) {
	if member == nil || member.zone == nil || e.registry == nil || e.now == nil ||
		e.damage.npc.registry == nil || member.zone.NPCRandom() == nil || event.Kind != zonenpc.MutationEventAuraHit {
		return nil, errors.New("mutation damage runtime unavailable")
	}
	defer e.retainMutationDamageMemberLocked(member)
	world := member.zone
	if !world.BeginMutationDamage() {
		return nil, errors.New("mutation checkpoint admission unavailable")
	}
	defer world.EndMutationDamage()
	work, isFound := world.NPCs().MutationDamageWork(event.ID)
	if !isFound {
		return nil, nil
	}
	if !work.IsPrepared {
		targets, err := e.prepareMutationDamageTargets(member, event)
		if err != nil {
			return nil, fmt.Errorf("mutationPrepare: %w", err)
		}
		work, err = world.NPCs().PrepareMutationDamage(event.ID, targets)
		if err != nil {
			return nil, fmt.Errorf("mutationClaim: %w", err)
		}
	}
	if e.registry.mutationDamageWorks == nil {
		e.registry.mutationDamageWorks = make(map[mutationDamageKey]*mutationDamageRun)
	}
	key := mutationDamageKey{world: world, id: event.ID}
	run := e.registry.mutationDamageWorks[key]
	if run == nil {
		run = &mutationDamageRun{targets: make(map[uint32]*enemyDamageReceipt)}
		cleanup := mutationDamageWorldCleanup{runtime: e, world: world}
		run.stopWorldCleanup = context.AfterFunc(world.Context(), cleanup.retire)
		e.registry.mutationDamageWorks[key] = run
	}
	damageErrors := make([]error, 0)
	for index := uint32(0); index < work.TargetCount; index++ {
		target := work.Targets[index]
		if target.IsComplete {
			continue
		}
		receipt := run.targets[target.ObjectID]
		if receipt == nil {
			// An in-progress committed receipt cannot be reconstructed from a
			// transport session. Safe checkpoints must exclude unfinished work.
			if target.IsCommitted {
				damageErrors = append(damageErrors, errors.New("mutation committed receipt unavailable"))
				continue
			}
			receipt = &enemyDamageReceipt{registry: e.registry, EventID: event.ID, Faction: event.DamageSource.Faction, DeathRevision: target.DeathRevision}
			run.targets[target.ObjectID] = receipt
		}
		current, isCurrent := world.MutationTarget(event.DamageSource.Faction, target.ObjectID, e.now())
		deathRevision, isLifeFound := mutationDamageTargetRevision(world, target.ObjectID, current.IsHero)
		if !receipt.IsCommitted && (!isCurrent || !isLifeFound || deathRevision != target.DeathRevision || current.UserID != target.UserID || current.PeerGeneration != target.PeerGeneration || current.HitPoint <= 0 || target.Damage <= 0) {
			target.IsComplete = true
			err := world.NPCs().UpdateMutationDamageTarget(event.ID, target)
			if err != nil {
				damageErrors = append(damageErrors, fmt.Errorf("mutationExpired: %w", err))
			}
			continue
		}
		if receipt.HitEffect == nil {
			hit, hitErr := raknet.MarshalApplication(raknet.ObjectEffectMessage{ObjectID: target.ObjectID, Asset: util.HashID("mutant_agent_infect_smoke_hit.ServerEventDef")})
			if hitErr != nil {
				damageErrors = append(damageErrors, fmt.Errorf("mutationHitEffect: %w", hitErr))
				continue
			}
			receipt.HitEffect = hit
		}
		plan := zonenpc.AttackPlan{SourceObjectID: event.DamageSource.ObjectID, TargetObjectID: target.ObjectID,
			SourcePosition: event.Position, TargetPosition: current.Position,
			Profile: zonenpc.ActionProfile{AbilityName: "MutationAgent_AoE", Radius: 6,
				DamageType: 5, DamageSource: 1, DescriptorMask: 72,
				IsDamageProfileKnown: true, IsRetainedVolumeDamage: true}}
		packets, stats, isApplied, err := e.damage.npc.applyEnemyDamage(member, member.generation, plan,
			zonenpc.AttackResult{Damage: target.Damage, IsCritical: target.IsCritical}, timestamp, false, true, false, receipt)
		target.IsCommitted = receipt.IsCommitted
		target.PreviousHitPoint = receipt.Damage.PreviousHitPoint
		target.HitPoint = receipt.Damage.HitPoint
		if err == nil {
			receipt.IsComplete = true
			target.IsComplete = true
			member.queueStatDelta(stats)
			run.packets = append(run.packets, packets...)
			if isApplied {
				run.packets = append(run.packets, receipt.HitEffect)
			}
		} else {
			damageErrors = append(damageErrors, fmt.Errorf("mutationTarget[%d]: %w", target.ObjectID, err))
		}
		updateErr := world.NPCs().UpdateMutationDamageTarget(event.ID, target)
		if updateErr != nil {
			damageErrors = append(damageErrors, fmt.Errorf("mutationReceipt: %w", updateErr))
		}
	}
	return append([][]byte(nil), run.packets...), errors.Join(damageErrors...)
}

func (e gameplayPendingRuntime) prepareMutationDamageTargets(member *gameplayPeerSession, event zonenpc.MutationEvent) ([]zonenpc.MutationDamageTarget, error) {
	if event.DamageSource.Faction == zonenpc.FactionPlayerAligned {
		return nil, nil
	}
	if event.DamageSource.Faction != zonenpc.FactionNonPlayerAligned {
		return nil, errors.New("mutation damage faction unavailable")
	}
	for _, scalar := range []float32{event.Position.X, event.Position.Y, event.Position.Z, event.DamageSource.CriticalRating, event.DamageSource.DifficultyMultiplier} {
		if math.IsNaN(float64(scalar)) || math.IsInf(float64(scalar), 0) {
			return nil, errors.New("mutation damage scalar invalid")
		}
	}
	// Generic is the sixth wire type and has no science-specific multiplier.
	damage, err := game.ResolveAbilityDamageRange(game.AbilityDamage{Minimum: 6, Maximum: 10,
		Descriptor: 72, DamageType: 5, DamageSource: 1,
		IsDescriptorFound: true, IsDamageSourceFound: true}, event.DamageSource.Profile)
	if err != nil {
		return nil, fmt.Errorf("mutationRange: %w", err)
	}
	multiplier := event.DamageSource.DifficultyMultiplier
	if multiplier == 0 {
		multiplier = 1
	}
	if multiplier < 0 {
		return nil, errors.New("mutation difficulty invalid")
	}
	damage.Minimum = float32(math.Floor(float64(damage.Minimum * multiplier)))
	damage.Maximum = float32(math.Ceil(float64(damage.Maximum * multiplier)))
	if math.IsNaN(float64(damage.Minimum)) || math.IsInf(float64(damage.Minimum), 0) ||
		math.IsNaN(float64(damage.Maximum)) || math.IsInf(float64(damage.Maximum), 0) {
		return nil, errors.New("mutation damage overflow")
	}
	candidates := member.zone.MutationTargets(event.DamageSource.Faction, e.now())
	sort.Slice(candidates, func(left, right int) bool { return candidates[left].ObjectID < candidates[right].ObjectID })
	targets := make([]zonenpc.MutationDamageTarget, 0, len(candidates))
	for _, target := range candidates {
		if target.ObjectID == event.DamageSource.ObjectID || target.HitPoint <= 0 ||
			zonegeometry.Distance(target.Position, event.Position) > 6 {
			continue
		}
		if len(targets) == zonenpc.MutationDamageTargetLimit {
			return nil, errors.New("mutation target capacity exceeded")
		}
		deathRevision, isLifeFound := mutationDamageTargetRevision(member.zone, target.ObjectID, target.IsHero)
		if !isLifeFound {
			continue
		}
		targets = append(targets, zonenpc.MutationDamageTarget{ObjectID: target.ObjectID, UserID: target.UserID, PeerGeneration: target.PeerGeneration, DeathRevision: deathRevision})
	}
	for index := range targets {
		result, rollErr := zonenpc.CommitAttack(member.zone.NPCRandom(), zonenpc.AttackPlan{Damage: damage}, event.DamageSource.CriticalRating, e.damage.npc.program.Critical)
		if rollErr != nil {
			return nil, fmt.Errorf("mutationRoll[%d]: %w", index, rollErr)
		}
		targets[index].Damage = result.Damage
		targets[index].IsCritical = result.IsCritical
	}
	return targets, nil
}

func mutationDamageTargetRevision(world *zone.Zone, objectID uint32, isHero bool) (uint64, bool) {
	if isHero {
		actor, isFound := world.Hero().SnapshotByObjectID(objectID)
		return actor.DeathRevision, isFound
	}
	actor, isFound := world.Companion().Snapshot(objectID)
	return actor.DeathRevision, isFound
}

// Postcommit continuation is separate so a wire failure cannot deal HP damage
// again. Each successfully completed phase retains its output before advancing.
func (e campaignNPCActionRuntime) finishEnemyDamage(peerSession *gameplayPeerSession, generation uint64, timestamp uint64, receipt *enemyDamageReceipt) ([][]byte, sporenet.PlayerStatDelta, bool, error) {
	plan, result, damage := receipt.Plan, receipt.Result, receipt.Damage
	targetSession, targetSessionKey := e.mutationHeroTargetSession(peerSession, receipt.Target)
	defer e.retainMutationTargetSessionLocked(targetSession, targetSessionKey)
	deathRevision, isLifeFound := mutationDamageTargetRevision(peerSession.zone, plan.TargetObjectID, damage.IsHero)
	expectedRevision := receipt.DeathRevision
	if damage.IsDefeated {
		expectedRevision++
	}
	isTargetLifeCurrent := isLifeFound && deathRevision == expectedRevision
	if damage.IsHero {
		actor, isFound := peerSession.zone.Hero().SnapshotByObjectID(plan.TargetObjectID)
		isTargetLifeCurrent = isTargetLifeCurrent && isFound && !(damage.IsDefeated && actor.HitPoint > 0)
		if !isTargetLifeCurrent || targetSession == nil || targetSession.deployedObjectID != plan.TargetObjectID {
			targetSession = nil
		}
	} else {
		actor, isFound := peerSession.zone.Companion().Snapshot(plan.TargetObjectID)
		isTargetLifeCurrent = isTargetLifeCurrent && isFound && !(damage.IsDefeated && actor.HitPoint > 0)
	}
	if !isTargetLifeCurrent && receipt.Phase < 2 {
		receipt.Phase = 2
	}
	if receipt.Phase == 0 {
		packets, err := peerSession.commitPhysicalVulnerability(plan.TargetObjectID, receipt.Vulnerability, e.modifierPool)
		if err != nil {
			return nil, sporenet.PlayerStatDelta{}, true, fmt.Errorf("damageVulnerability: %w", err)
		}
		receipt.VulnerabilityPackets = packets
		if damage.IsDefeated {
			peerSession.retirePhysicalVulnerability(plan.TargetObjectID, e.modifierPool)
		}
		receipt.Phase = 1
	}
	if receipt.Phase == 1 {
		if damage.IsDefeated {
			packets, err := peerSession.retireCampaignNPCEnergyVulnerability(plan.TargetObjectID, e.modifierPool)
			if err != nil {
				return nil, sporenet.PlayerStatDelta{}, true, fmt.Errorf("damageEnergy: %w", err)
			}
			receipt.EnergyPackets = packets
			supportPackets, supportErr := e.retireNPCSupportTargetLocked(peerSession, plan.TargetObjectID)
			if supportErr != nil {
				return nil, sporenet.PlayerStatDelta{}, true, fmt.Errorf("mutationSupportRetire: %w", supportErr)
			}
			receipt.EnergyPackets = append(receipt.EnergyPackets, supportPackets...)
		}
		receipt.Phase = 2
	}
	if receipt.Phase == 2 {
		packets, err := npcraknet.AttackHit(plan, result, damage.HitPoint)
		if err != nil {
			return nil, sporenet.PlayerStatDelta{}, true, fmt.Errorf("damageHit: %w", err)
		}
		if receipt.Absorbed > 0 {
			packets, err = replaceShieldCombatEvent(packets, plan.SourceObjectID, plan.TargetObjectID, result.Damage, receipt.Absorbed, damage.HitPoint, result.IsCritical)
			if err != nil {
				return nil, sporenet.PlayerStatDelta{}, true, fmt.Errorf("damageAbsorb: %w", err)
			}
		}
		receipt.Packets = append(append(append(append([][]byte(nil), receipt.ShieldPackets...), packets...), receipt.VulnerabilityPackets...), receipt.EnergyPackets...)
		receipt.Phase = 3
	}
	if receipt.Phase == 3 {
		if !damage.IsHero {
			if damage.IsDefeated && isTargetLifeCurrent {
				packets, isTrapHandled, err := peerSession.breakHeroTrap(plan.TargetObjectID)
				if err != nil {
					return nil, sporenet.PlayerStatDelta{}, true, fmt.Errorf("damageTrap: %w", err)
				}
				if !isTrapHandled {
					packets, err = e.companionDefeat(peerSession, plan.TargetObjectID, timestamp)
					if err != nil {
						return nil, sporenet.PlayerStatDelta{}, true, fmt.Errorf("damageCompanion: %w", err)
					}
				}
				receipt.Packets = append(receipt.Packets, packets...)
			}
		} else if targetSession != nil {
			if targetSession != peerSession {
				e.registry.storeMutationDamageMemberLocked(peerSession)
			}
			packets, stats, err := e.commitHeroTargetDamage(peerSession, targetSession, targetSessionKey, receipt.Packets, damage, receipt.Distribution, timestamp, receipt)
			if targetSession != peerSession {
				e.registry.refreshMutationDamageMemberLocked(peerSession)
			}
			if err != nil {
				return nil, sporenet.PlayerStatDelta{}, true, fmt.Errorf("damageHero: %w", err)
			}
			receipt.Packets, receipt.Stats = packets, stats
		}
		receipt.Phase = 4
	}
	if receipt.Phase == 4 && damage.IsHero {
		if targetSession != nil && targetSession != peerSession {
			e.registry.storeMutationDamageMemberLocked(peerSession)
		}
		packets, stats, err := e.commitHeroTargetReactions(peerSession, targetSession, targetSessionKey, receipt.Packets, receipt.Stats, damage, plan, timestamp, receipt)
		if targetSession != nil && targetSession != peerSession {
			e.registry.refreshMutationDamageMemberLocked(peerSession)
		}
		if err != nil {
			return nil, sporenet.PlayerStatDelta{}, true, fmt.Errorf("damageReaction: %w", err)
		}
		receipt.Packets, receipt.Stats = packets, stats
	}
	receipt.Phase = 5
	receipt.IsComplete = true
	return receipt.Packets, receipt.Stats, true, nil
}
