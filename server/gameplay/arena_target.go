package gameplay

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/darkspinnet/darkspin/server/combat"
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// These adapters are used only while holding the gameplay registry mutex.
// Snapshots are query projections; hits mutate the owning player's real squad.
type arenaAbilityTargets struct {
	runtime    campaignAbilityCommandRuntime
	source     gameplayPeerSession
	definition sim.AbilityDefinition
	timestamp  uint64
	hits       map[uint32]arenaAbilityHit
}

type arenaAbilityHit struct {
	// The casting object can differ from the player's deployed hero at impact.
	sourceObjectID uint32
	flags          uint16
	packets        [][]byte
	memberKey      string
	generation     uint64
	isTeamDefeated bool
	isLethal       bool
	isGhostDodge   bool
}

func (e arenaAbilityTargets) member(objectID uint32) (string, gameplayPeerSession, bool) {
	for key, member := range e.runtime.registry.sessions {
		if member.binding.Mode != game.ModeArena || member.binding.GameID != e.source.binding.GameID ||
			member.binding.Team == e.source.binding.Team || !member.isArenaCombatActive ||
			!member.stage.IsDungeon() || member.deployedObjectID != objectID || member.deployedHitPoint() <= 0 {
			continue
		}
		return key, member, true
	}
	return "", gameplayPeerSession{}, false
}

func (e arenaAbilityTargets) NPC(objectID uint32) (zonenpc.Snapshot, bool) {
	key, member, isFound := e.member(objectID)
	if !isFound {
		if target, isTurret := e.turretSnapshot(objectID); isTurret {
			return target, true
		}
		if target, isSprite := e.spriteSnapshot(objectID); isSprite {
			return target, true
		}
		return e.combatPetSnapshot(objectID)
	}
	err := member.advancePlayerPosition(e.runtime.now(), raknet.Vector3{})
	if err != nil {
		if e.runtime.logger != nil {
			e.runtime.logger.Printf("Arena ability target position unavailable object=%d: %v", objectID, err)
		}
		return zonenpc.Snapshot{}, false
	}
	e.runtime.registry.sessions[key] = member
	return zonenpc.Snapshot{
		Plan: zonenpc.SpawnPlan{
			ObjectID: objectID, Position: game.Vec3(member.playerPosition),
			NPCProfile: game.CampaignNPCProfile{FootprintRadius: member.deployedCampaignActorFootprintRadius(), IsTargetable: true},
		},
		HitPoint: member.deployedHitPoint(), ManaPoint: member.deployedManaPoint(),
		Faction: zonenpc.FactionNonPlayerAligned, IsPublished: true,
	}, true
}

func (e arenaAbilityTargets) LiveSnapshots() []zonenpc.Snapshot {
	targets := make([]zonenpc.Snapshot, 0)
	for _, member := range e.runtime.registry.sessions {
		if member.binding.Mode != game.ModeArena || member.binding.GameID != e.source.binding.GameID {
			continue
		}
		target, isFound := e.NPC(member.deployedObjectID)
		if isFound {
			targets = append(targets, target)
		}
	}
	match := e.runtime.registry.arenaMatches[e.source.binding.GameID]
	if match != nil && match.abilityWorld != nil {
		for objectID := range match.abilityWorld.claymores {
			target, isFound := e.turretSnapshot(objectID)
			if isFound {
				targets = append(targets, target)
			}
		}
		for objectID := range match.abilityWorld.sprites {
			target, isFound := e.spriteSnapshot(objectID)
			if isFound {
				targets = append(targets, target)
			}
		}
		for objectID := range match.abilityWorld.combatPets {
			target, isFound := e.combatPetSnapshot(objectID)
			if isFound {
				targets = append(targets, target)
			}
		}
	}
	slices.SortFunc(targets, func(first, second zonenpc.Snapshot) int {
		return cmp.Compare(first.Plan.ObjectID, second.Plan.ObjectID)
	})
	return targets
}

func (e arenaAbilityTargets) HasCurseOrFear(objectID uint32, at time.Time) bool {
	key, member, isFound := e.member(objectID)
	if !isFound || key == "" {
		return false
	}
	return member.isEnemyFearActive(at) || member.isArenaHitCurseActive(at)
}

func (e *arenaAbilityTargets) Hit(req zonenpc.HitRequest) (zonenpc.DamageResult, error) {
	if run, isFound := e.turret(req.TargetObjectID); isFound {
		return e.hitTurretLocked(run, req)
	}
	if run, actor, isFound := e.sprite(req.TargetObjectID); isFound {
		return e.hitSpriteLocked(run, actor, req)
	}
	if run, actor, isFound := e.combatPet(req.TargetObjectID); isFound {
		return e.hitCombatPetLocked(run, actor, req)
	}
	key, target, isFound := e.member(req.TargetObjectID)
	match := e.runtime.registry.arenaMatches[e.source.binding.GameID]
	if !isFound || match == nil || !match.isCombatActive {
		return zonenpc.DamageResult{}, errors.New("arena hit target unavailable")
	}
	now := e.runtime.now()
	defenseReq := combat.DefenseRequest{
		Damage: req.Damage, DamageSource: uint8(req.Metadata.DamageSource), DamageType: uint8(req.Metadata.DamageType),
		IsSourceKnown: e.definition.IsDamageSourceFound, IsTypeKnown: e.definition.IsDamageTypeFound,
		IsArea: req.IsArea, IsPeriodic: req.IsPeriodic,
	}
	flags, err := target.rollDefense(match.random, defenseReq, e.runtime.program.Critical)
	if err != nil {
		return zonenpc.DamageResult{}, fmt.Errorf("arenaHitDefense: %w", err)
	}
	if target.isHeroDamageImmune(now) {
		flags = 0x40
	}
	damage := arenaDamageAfterReduction(target, e.source, req.Damage, uint8(req.Metadata.DamageSource), now)
	damage = combat.ReduceIncomingDamage(damage, target.equipmentDefense(), defenseReq)
	if flags != 0 {
		damage = 0
	}
	if damage <= 0 && flags == 0 {
		flags = 0x40
	}
	shield, err := target.absorbTCShield(damage, now, e.runtime.effectPool)
	if err != nil {
		return zonenpc.DamageResult{}, fmt.Errorf("arenaHitShield: %w", err)
	}
	previousHealth := target.deployedHitPoint()
	distribution, err := target.prepareSoulLinkDamage(shield.Damage)
	if err != nil {
		return zonenpc.DamageResult{}, fmt.Errorf("arenaHitSoulLink: %w", err)
	}
	damage = max(float32(0), min(distribution.activeDamage, previousHealth))
	health := previousHealth - damage
	creatureIndex := target.deployedCreatureIndex
	creature := target.binding.Creatures[creatureIndex]
	resourcePacket, err := raknet.MarshalApplication(raknet.LabsPlayerCharacterResourceMessage{
		PlayerSlot: uint8(target.binding.Slot), CreatureIndex: creatureIndex,
		Resource: raknet.LabsCharacterResource{Health: health, MaxHealth: target.characterHitPointMaximum(creatureIndex),
			Mana: target.deployedManaPoint(), MaxMana: creature.PowerPoint},
	})
	if err != nil {
		return zonenpc.DamageResult{}, fmt.Errorf("arenaHitResource: %w", err)
	}
	healthPacket, err := raknet.MarshalApplication(raknet.CombatantDataDeltaMessage{
		ObjectID: req.TargetObjectID, HitPoints: health, IsHitPointChanged: true,
	})
	if err != nil {
		return zonenpc.DamageResult{}, fmt.Errorf("arenaHitHealth: %w", err)
	}
	var ghostPacket []byte
	isGhostDodge := flags == 0x20 && target.ghostFormRun != nil &&
		target.ghostFormRun.IsActive(creatureIndex, previousHealth > 0)
	if isGhostDodge {
		ghostPacket, err = raknet.MarshalApplication(raknet.ServerEventMessage{
			Asset:    util.HashID(zoneability.GhostFormPassthroughEffect),
			ObjectID: req.TargetObjectID,
		})
		if err != nil {
			return zonenpc.DamageResult{}, fmt.Errorf("arenaGhostEffect: %w", err)
		}
	}
	backupSquad := target.squad
	if len(distribution.shares) != 0 {
		stagedSquad, stageErr := target.squad.Clone()
		if stageErr != nil {
			return zonenpc.DamageResult{}, fmt.Errorf("arenaHitSquadStage: %w", stageErr)
		}
		backupSquad = stagedSquad
	}
	sharePackets, sharedDamage, err := distribution.commit(&target)
	if err != nil {
		if len(distribution.shares) != 0 {
			rollbackErr := target.squad.CommitClone(backupSquad)
			err = errors.Join(err, rollbackErr)
		}
		return zonenpc.DamageResult{}, fmt.Errorf("arenaHitSoulShare: %w", err)
	}
	transitions, err := target.applyArenaDamageHitPoints(health, e.timestamp)
	if err != nil {
		if len(distribution.shares) != 0 {
			rollbackErr := target.squad.CommitClone(backupSquad)
			err = errors.Join(err, rollbackErr)
		}
		return zonenpc.DamageResult{}, fmt.Errorf("arenaHitCommit: %w", err)
	}
	if health > 0 && damage > 0 {
		target.applyPassiveDamageTaken(now)
		mantlePackets, mantleErr := target.presentHardenedMantle(e.runtime.modifierPool, e.runtime.effectPool, e.timestamp)
		if mantleErr != nil {
			if e.runtime.logger != nil {
				e.runtime.logger.Printf("Arena Hardened Mantle presentation failed object=%d: %v", req.TargetObjectID, mantleErr)
			}
		}
		transitions = append(transitions, mantlePackets...)
	}
	privatePackets := arenaTargetPrivateTransitionPackets(transitions)
	target.queuePackets(privatePackets)
	e.runtime.registry.sessions[key] = target
	if health > 0 && damage > 0 {
		quantumErr := e.reserveArenaQuantumStateLocked(key, &target, req.SourceObjectID)
		if quantumErr != nil && e.runtime.logger != nil {
			e.runtime.logger.Printf("Arena Quantum State reaction reservation failed target=%d: %v", req.TargetObjectID, quantumErr)
		}
	}
	if req.Metadata.DescriptorMask&4 == 0 {
		e.runtime.registry.breakArenaSleepingCloudLocked(target.binding.GameID, req.TargetObjectID)
	}
	if health <= 0 {
		e.runtime.registry.retireArenaClaymoresForOwnerLocked(e.source.binding.GameID, req.TargetObjectID)
		e.runtime.registry.retireArenaSpritesForOwnerLocked(e.source.binding.GameID, req.TargetObjectID)
		e.runtime.registry.retireArenaCombatPetsForOwnerLocked(e.source.binding.GameID, req.TargetObjectID)
		e.runtime.registry.retireArenaChargeForOwnerLocked(e.source.binding.GameID, req.TargetObjectID)
		e.runtime.registry.retireArenaRideForOwnerLocked(e.source.binding.GameID, req.TargetObjectID)
		e.runtime.registry.retireArenaQuantumBlinkForOwnerLocked(e.source.binding.GameID, req.TargetObjectID)
		// Save the committed victim first: marker removal queues presentation
		// against the current session rather than an earlier local copy.
		e.runtime.registry.retireArenaForcePulseVictimLocked(e.source.binding.GameID, req.TargetObjectID)
	}
	match.result.recordDamage(e.source.binding.UserID, target.binding.UserID, damage+sharedDamage, health <= 0)
	match.recordRecentDamage(req.TargetObjectID, now, damage)
	resourcePackets := append(sharePackets, resourcePacket)
	e.runtime.registry.queueArenaAbilityResourcesLocked(e.source, resourcePackets)
	packets := append(shield.Packets, healthPacket)
	packets = append(packets, resourcePackets...)
	if len(ghostPacket) != 0 {
		packets = append(packets, ghostPacket)
	}
	packets = append(packets, gameplayPeerPresentationPackets(transitions)...)
	if health > 0 && damage > 0 && target.deployedObjectID == req.TargetObjectID &&
		target.plasmaSentinelActive != nil {
		petPackets, petErr := e.runtime.npc.triggerPlasmaSentinelActive(&target, e.timestamp)
		if petErr != nil {
			if e.runtime.logger != nil {
				e.runtime.logger.Printf("Arena Pain Hounds trigger failed object=%d: %v", req.TargetObjectID, petErr)
			}
		} else {
			packets = append(packets, petPackets...)
			e.runtime.registry.sessions[key] = target
		}
	}
	e.hits[req.TargetObjectID] = arenaAbilityHit{
		sourceObjectID: req.SourceObjectID,
		flags:          flags, packets: packets, memberKey: key, generation: target.generation,
		isTeamDefeated: target.isZoneGameOver(),
		isLethal:       health <= 0 && damage > 0,
		isGhostDodge:   isGhostDodge,
	}
	return zonenpc.DamageResult{
		ObjectID: req.TargetObjectID, PreviousHealth: previousHealth, HitPoint: health,
		Damage: damage, ResolvedDamage: shield.Damage, AbsorbedDamage: shield.Absorbed,
		IsDefeated: health <= 0, IsDamageImmune: flags != 0,
	}, nil
}

// finish runs after releasing the registry mutex. Round completion follows all
// hit publications and checks the whole opposing team, including reserve slots.
func (e arenaAbilityTargets) finish() {
	for objectID, hit := range e.hits {
		if hit.isLethal {
			hit.isLethal = false
			e.hits[objectID] = hit
			e.runtime.registry.mutex.Lock()
			e.runtime.registry.applyArenaKillPassiveLocked(e.source, hit.sourceObjectID)
			e.runtime.registry.mutex.Unlock()
		}
		if hit.isTeamDefeated {
			e.runtime.commitArenaVictory(e.source.binding.GameID, uint32(e.source.binding.Team), hit.memberKey, hit.generation)
		}
	}
}

func (e arenaAbilityTargets) packets(results []zoneability.AreaResult) ([][]byte, error) {
	packets := make([][]byte, 0)
	for _, result := range results {
		hit := e.hits[result.Damage.ObjectID]
		flags := uint16(1)
		if result.Damage.IsDefeated {
			flags |= 4
		}
		if result.IsCritical {
			flags |= 8
		}
		if hit.flags != 0 {
			flags = hit.flags
		}
		packet, err := raknet.MarshalApplication(raknet.DamageCombatEventMessage{
			Flags: flags, DeltaHealth: result.Damage.Damage, TargetID: result.Damage.ObjectID,
			SourceID: hit.sourceObjectID, IntegerHPChange: -int32(result.Damage.Damage),
		})
		if err != nil {
			return nil, fmt.Errorf("arenaAreaEvent: %w", err)
		}
		if result.Damage.AbsorbedDamage > 0 {
			packet, err = shieldCombatEvent(hit.sourceObjectID, result.Damage.ObjectID,
				result.Damage.Damage, result.Damage.AbsorbedDamage, result.Damage.HitPoint, result.IsCritical && hit.flags == 0)
			if err != nil {
				return nil, fmt.Errorf("arenaAreaAbsorb: %w", err)
			}
		}
		packets = append(packets, packet)
		packets = append(packets, hit.packets...)
	}
	return packets, nil
}

func (e arenaAbilityTargets) packetsWithEffects(results []zoneability.AreaResult,
	effect func(zoneability.AreaResult) ([]byte, error), isEffectAfterDamage bool,
) ([][]byte, error) {
	packets := make([][]byte, 0)
	for _, result := range results {
		var effectPacket []byte
		var err error
		if effect != nil && !e.hits[result.Damage.ObjectID].isGhostDodge {
			effectPacket, err = effect(result)
			if err != nil {
				return nil, fmt.Errorf("arenaImpact: %w", err)
			}
		}
		if len(effectPacket) != 0 && !isEffectAfterDamage {
			packets = append(packets, effectPacket)
		}
		hitPackets, hitErr := e.packets([]zoneability.AreaResult{result})
		if hitErr != nil {
			return nil, fmt.Errorf("arenaHit: %w", hitErr)
		}
		packets = append(packets, hitPackets...)
		if len(effectPacket) != 0 && isEffectAfterDamage {
			packets = append(packets, effectPacket)
		}
	}
	return packets, nil
}

func (e *gameplaySessionRegistry) abilityTargetsLocked(runtime campaignAbilityCommandRuntime, member gameplayPeerSession,
	definition sim.AbilityDefinition, timestamp uint64,
) zoneability.DamageTargets {
	if member.binding.Mode == game.ModeArena {
		return &arenaAbilityTargets{runtime: runtime, source: member, definition: definition,
			timestamp: timestamp, hits: make(map[uint32]arenaAbilityHit)}
	}
	return member.zone.NPCs()
}

func (e arenaAbilityTargets) RecentDamage(objectID uint32, now time.Time) float32 {
	match := e.runtime.registry.arenaMatches[e.source.binding.GameID]
	if match == nil {
		return 0
	}
	total := float32(0)
	for _, hit := range match.recentDamages[objectID] {
		if hit.at.After(now.Add(-10*time.Second)) && !hit.at.After(now) {
			total += hit.amount
		}
	}
	return total
}

type arenaRecentDamage struct {
	at     time.Time
	amount float32
}

func (e *arenaMatchState) recordRecentDamage(objectID uint32, now time.Time, amount float32) {
	if amount <= 0 {
		return
	}
	if e.recentDamages == nil {
		e.recentDamages = make(map[uint32][]arenaRecentDamage)
	}
	hits := e.recentDamages[objectID]
	first := 0
	for first < len(hits) && !hits[first].at.After(now.Add(-10*time.Second)) {
		first++
	}
	e.recentDamages[objectID] = append(hits[first:], arenaRecentDamage{at: now, amount: amount})
}
