package gameplay

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/combat"
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/playerstat"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/darkspinnet/darkspin/server/util"
	zone "github.com/darkspinnet/darkspin/server/zone"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	barrierraknet "github.com/darkspinnet/darkspin/server/zone/barrier/raknet103"
	zoneboss "github.com/darkspinnet/darkspin/server/zone/boss"
	bossraknet "github.com/darkspinnet/darkspin/server/zone/boss/raknet103"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	companionraknet "github.com/darkspinnet/darkspin/server/zone/companion/raknet103"
	zonedeath "github.com/darkspinnet/darkspin/server/zone/death"
	deathraknet "github.com/darkspinnet/darkspin/server/zone/death/raknet103"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	heroraknet "github.com/darkspinnet/darkspin/server/zone/hero/raknet103"
	zonehorde "github.com/darkspinnet/darkspin/server/zone/horde"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
	objectraknet "github.com/darkspinnet/darkspin/server/zone/object/raknet103"
	zoneprojection "github.com/darkspinnet/darkspin/server/zone/projection"
	securityraknet "github.com/darkspinnet/darkspin/server/zone/security/raknet103"
	zonespawn "github.com/darkspinnet/darkspin/server/zone/spawn"
	spawnraknet "github.com/darkspinnet/darkspin/server/zone/spawn/raknet103"
	zoneunlock "github.com/darkspinnet/darkspin/server/zone/unlock"
	unlockraknet "github.com/darkspinnet/darkspin/server/zone/unlock/raknet103"
)

const campaignShockwaveStunDuration = 4 * time.Second

const destructibleLargeFootprint = float32(2.5)

const destructibleSmallExplosion = "effect_environment_Zelems_instrument_minor_explosion.ServerEventDef"
const destructibleLargeExplosion = "zelem_gasCollectorUnit_destruction_effect.ServerEventDef"

const destructibleSmallDeleteDelay = 1500 * time.Millisecond
const destructibleLargeDeleteDelay = 2500 * time.Millisecond

const nocturnaThornNounName = "PHYS_moon1_plant_thorny_1.Noun"

func applyNPCSlowTiming(
	profile zonenpc.ActionProfile, attackScale float32,
) zonenpc.ActionProfile {
	if attackScale <= 0 || attackScale >= 1 {
		return profile
	}
	profile.HitDelay = time.Duration(
		float64(profile.HitDelay) / float64(attackScale),
	)
	profile.ReleaseDelay = time.Duration(
		float64(profile.ReleaseDelay) / float64(attackScale),
	)
	profile.Cooldown = time.Duration(
		float64(profile.Cooldown) / float64(attackScale),
	)
	return profile
}

func applyNPCHaste(
	profile zonenpc.ActionProfile, attackSpeed float32,
	cooldownReduction float32, movementSpeedBuff float32,
) zonenpc.ActionProfile {
	if attackSpeed > 0 {
		profile.HitDelay = time.Duration(
			float64(profile.HitDelay) / float64(1+attackSpeed),
		)
		profile.ReleaseDelay = time.Duration(
			float64(profile.ReleaseDelay) / float64(1+attackSpeed),
		)
		profile.Cooldown = time.Duration(
			float64(profile.Cooldown) / float64(1+attackSpeed),
		)
	}
	if cooldownReduction > 0 && cooldownReduction < 1 {
		profile.Cooldown = time.Duration(
			float64(profile.Cooldown) * float64(1-cooldownReduction),
		)
	}
	if movementSpeedBuff > 0 {
		profile.MovementSpeed *= 1 + movementSpeedBuff
		profile.NonCombatMovementSpeed *= 1 + movementSpeedBuff
	}
	return profile
}

type zoneNPCCriticalResolver struct {
	peerSession     *gameplayPeerSession
	profilesByClass map[uint32]sim.CriticalProfile
	tuning          sim.CriticalTuning
	isHordeActor    bool
}

func (e zoneNPCCriticalResolver) resolve(
	noun string, damage float32,
) (sim.CriticalDamageResult, error) {
	return resolveZoneNPCCritical(
		e.peerSession, e.profilesByClass, e.tuning, e.isHordeActor,
		noun, damage,
	)
}

func resolveZoneNonPlayerCritical(
	random *sim.SimulatorRandom, profile sim.CriticalProfile,
	tuning sim.CriticalTuning, damage float32,
) (sim.CriticalDamageResult, error) {
	if tuning.DamageBonus == 0 && len(tuning.RatingConversions) == 0 {
		return sim.CriticalDamageResult{Damage: damage}, nil
	}
	result, err := combat.ResolveNonPlayer(random, damage, profile, tuning)
	if err != nil {
		return sim.CriticalDamageResult{}, fmt.Errorf("nonPlayerCritical: %w", err)
	}
	return result, nil
}

func resolveZoneNPCCritical(
	peerSession *gameplayPeerSession, profilesByClass map[uint32]sim.CriticalProfile,
	tuning sim.CriticalTuning, isHordeActor bool,
	noun string, damage float32,
) (sim.CriticalDamageResult, error) {
	if peerSession == nil {
		return sim.CriticalDamageResult{}, errors.New("nil gameplay session")
	}
	if tuning.DamageBonus == 0 && len(tuning.RatingConversions) == 0 {
		return sim.CriticalDamageResult{Damage: damage}, nil
	}
	random, err := zoneCriticalRandom(peerSession)
	if err != nil {
		return sim.CriticalDamageResult{}, fmt.Errorf("enemyCriticalRandom: %w", err)
	}
	result, err := combat.ResolveNPC(
		random, damage, noun, profilesByClass, tuning,
	)
	if err != nil {
		return sim.CriticalDamageResult{}, fmt.Errorf("enemyCritical: %w", err)
	}
	return result, nil
}

func zoneCriticalRandom(
	peerSession *gameplayPeerSession,
) (*sim.SimulatorRandom, error) {
	if peerSession == nil || peerSession.zone == nil {
		return nil, errors.New("zone random unavailable")
	}
	random := peerSession.zone.NPCRandom()
	if random == nil {
		return nil, errors.New("zone random unavailable")
	}
	return random, nil
}

type zoneNPCDamagePublication struct {
	packets  [][]byte
	deathRun *deathraknet.Run
}

type zoneNPCDamageResult struct {
	hitPoint   float32
	isDefeated bool
	isFound    bool
}

type zoneNPCDeathDefinition struct {
	objectID                   uint32
	noun                       string
	position                   raknet.Vector3
	hitPoint                   float32
	creatureType               uint32
	isCreatureTypeKnown        bool
	isFixture                  bool
	isBoss                     bool
	isDeathAnimationSuppressed bool
	ordinaryDeathAnimation     string
	corpseFadeDelay            time.Duration
	graphicsState              uint32
	explosionEffectName        string
	deleteDelay                time.Duration
}

func destructibleDeathPresentation(
	snapshot zonenpc.Snapshot, physics zoneNounPhysics,
) (string, time.Duration) {
	if strings.EqualFold(snapshot.Plan.NounName, campaignCorruptorPortalNounName) {
		return "scaldron_boss_portal_explosion_effect.ServerEventDef", time.Millisecond
	}
	if snapshot.Plan.NounName == "ZelemGravityOrb.Noun" {
		// GravityOrbPassive.Deactivate emits its fizzle and marks the orb
		// for deletion; it is not an ordinary exploding scenery fixture.
		return "gravity_orb_fizzle.ServerEventDef", time.Millisecond
	}
	footprint := max(snapshot.Plan.NPCProfile.FootprintRadius, physics.FootprintRadius)
	halfWidth := max(
		(physics.BoundMaximum.X-physics.BoundMinimum.X)*0.5,
		(physics.BoundMaximum.Y-physics.BoundMinimum.Y)*0.5,
	)
	footprint = max(footprint, halfWidth)
	if footprint >= destructibleLargeFootprint {
		return destructibleLargeExplosion, destructibleLargeDeleteDelay
	}
	return destructibleSmallExplosion, destructibleSmallDeleteDelay
}

func marshalZoneNPCDamage(
	definition zoneNPCDeathDefinition, damageResult zoneNPCDamageResult,
	sourceObjectID uint32, damage float32, isCritical bool, sourceTime uint64,
	effectPool *attachedEffectPool,
) (zoneNPCDamagePublication, error) {
	if definition.objectID == 0 || !damageResult.isFound || sourceObjectID == 0 ||
		damage <= 0 || effectPool == nil {
		return zoneNPCDamagePublication{}, errors.New("invalid enemy damage publication")
	}
	publication, err := npcraknet.Damage(npcraknet.DamageRequest{
		Target:         campaignDeathTarget(definition),
		SourceObjectID: sourceObjectID, HitPoint: damageResult.hitPoint,
		Damage: damage, SourceTime: sourceTime, EffectPool: effectPool,
		IsCritical: isCritical, IsDefeated: damageResult.isDefeated,
	})
	if err != nil {
		return zoneNPCDamagePublication{}, fmt.Errorf("damageProject: %w", err)
	}
	return zoneNPCDamagePublication{
		packets: publication.Packet, deathRun: publication.DeathRun,
	}, nil
}

func campaignNPCDeathDefinition(
	snapshot zonenpc.Snapshot, physics zoneNounPhysics,
) (zoneNPCDeathDefinition, error) {
	if snapshot.Plan.ObjectID == 0 || snapshot.Plan.NounName == "" ||
		!isFiniteCampaignPopulationPosition(snapshot.Plan.Position) {
		return zoneNPCDeathDefinition{}, errors.New(
			"invalid campaign enemy death definition",
		)
	}
	ordinaryDeathAnimation := physics.OrdinaryDeathAnimation
	deathPresentation, isDeathPresentationFound :=
		zonenpc.DeathPresentationForNoun(snapshot.Plan.NounName)
	if isDeathPresentationFound &&
		(ordinaryDeathAnimation == "" || deathPresentation.PresentationDuration > 0) {
		ordinaryDeathAnimation = deathPresentation.AnimationName
	}
	isFixture := snapshot.Plan.IsFixture
	isDestructor := zoneboss.IsFinalBossNoun(snapshot.Plan.NounName)
	if isDestructor {
		isFixture = false
	}
	detonation, isDetonationFound := zonenpc.BoomerDeathDetonationProfile(
		snapshot.Plan.NounName,
	)
	if isDetonationFound {
		ordinaryDeathAnimation = detonation.AnimationName
	}
	graphicsState := uint32(0)
	explosionEffectName := ""
	deleteDelay := detonation.HitDelay
	if isFixture {
		graphicsState = util.HashID("dead")
		explosionEffectName, deleteDelay = destructibleDeathPresentation(
			snapshot, physics,
		)
	}
	isIllusion := snapshot.Plan.OwnerObjectID != 0 && zonenpc.IsNashiraNoun(snapshot.Plan.NounName)
	if isIllusion {
		// Duplicates dissolve; only the real boss owns the long death scene.
		explosionEffectName = "shadow_boss_duplicate_effect.ServerEventDef"
		deleteDelay = 100 * time.Millisecond
		deathPresentation.PresentationDuration = 0
	}
	return zoneNPCDeathDefinition{
		objectID: snapshot.Plan.ObjectID,
		noun:     snapshot.Plan.NounName,
		position: raknet.Vector3{
			X: snapshot.Plan.Position.X,
			Y: snapshot.Plan.Position.Y,
			Z: snapshot.Plan.Position.Z,
		},
		hitPoint:                   snapshot.HitPoint,
		creatureType:               physics.CreatureType,
		isCreatureTypeKnown:        physics.IsCreatureTypeKnown,
		isFixture:                  isFixture,
		isBoss:                     snapshot.Plan.IsBoss || isDestructor,
		isDeathAnimationSuppressed: isIllusion,
		ordinaryDeathAnimation:     ordinaryDeathAnimation,
		corpseFadeDelay:            deathPresentation.PresentationDuration,
		graphicsState:              graphicsState,
		explosionEffectName:        explosionEffectName,
		deleteDelay:                deleteDelay,
	}, nil
}

type campaignShockwaveStunExpiryStep struct {
	runtime        campaignDamageRuntime
	sessionKey     string
	generation     uint64
	targetObjectID uint32
	stunExpiresAt  time.Time
	run            *campaignNPCModifierRun
}

func (s campaignShockwaveStunExpiryStep) produce() ([][]byte, error) {
	s.runtime.registry.mutex.Lock()
	peerSession, isFound := s.runtime.registry.sessions[s.sessionKey]
	isCurrent := isFound && peerSession.generation == s.generation &&
		peerSession.zone.NPCs() != nil
	if isCurrent {
		peerSession.zone.NPCs().ClearStun(
			s.targetObjectID, s.stunExpiresAt,
		)
		peerSession.untrackCampaignNPCModifier(s.run)
		s.runtime.registry.sessions[s.sessionKey] = peerSession
	}
	s.runtime.registry.mutex.Unlock()
	isCreated, err := s.run.release(s.runtime.npc.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("shockwaveStunDeleteRelease: %w", err)
	}
	if !isCurrent || !isCreated {
		return nil, nil
	}
	packet, err := effectraknet.ModifierDelete(
		s.targetObjectID, s.run.instanceID,
	)
	if err != nil {
		return nil, fmt.Errorf("shockwaveStunDelete: %w", err)
	}
	return [][]byte{packet}, nil
}

func (r campaignDamageRuntime) publishShockwaveStuns(
	packet raknet.Packet, sessionKey string, generation uint64,
	sourceObjectID uint32, timestamp uint64, results []zoneability.AreaResult,
) ([][]byte, error) {
	return r.publishNPCStuns(
		packet, sessionKey, generation, sourceObjectID, timestamp, results,
		util.HashID("EnergySentinelStun"), campaignShockwaveStunDuration,
	)
}

func (r campaignDamageRuntime) publishNPCStuns(
	packet raknet.Packet, sessionKey string, generation uint64,
	sourceObjectID uint32, timestamp uint64, results []zoneability.AreaResult,
	modifierID uint32, duration time.Duration,
) ([][]byte, error) {
	if modifierID == 0 || duration <= 0 {
		return nil, errors.New("invalid NPC stun")
	}
	packets := make([][]byte, 0, len(results))
	for _, result := range results {
		if result.Damage.IsDefeated || result.Damage.IsDamageImmune ||
			result.Damage.IsShieldStarted || result.Damage.IsTurtleStarted {
			continue
		}
		run, err := newCampaignNPCModifierRun(r.npc.modifierPool)
		if err != nil {
			return nil, fmt.Errorf("shockwaveStunRun: %w", err)
		}
		stunExpiresAt := r.npc.now().Add(duration)
		r.registry.mutex.Lock()
		currentSession, isFound := r.registry.sessions[sessionKey]
		isCurrent := isFound && currentSession.generation == generation &&
			currentSession.zone.NPCs() != nil
		if isCurrent {
			err = currentSession.zone.NPCs().ApplyStun(
				result.Damage.ObjectID, stunExpiresAt,
			)
			if err == nil {
				err = currentSession.trackCampaignNPCModifier(run)
			}
			if err == nil {
				r.registry.sessions[sessionKey] = currentSession
			}
		}
		r.registry.mutex.Unlock()
		if !isCurrent || err != nil {
			_, releaseErr := run.release(r.npc.modifierPool)
			if err != nil {
				return nil, fmt.Errorf("shockwaveStunTrack: %w", errors.Join(err, releaseErr))
			}
			if releaseErr != nil {
				return nil, fmt.Errorf("shockwaveStunRelease: %w", releaseErr)
			}
			continue
		}
		createPacket, err := effectraknet.ModifierCreate(
			effectraknet.ModifierCreateRequest{
				SourceObjectID: sourceObjectID,
				TargetObjectID: result.Damage.ObjectID,
				ModifierID:     modifierID,
				InstanceID:     run.instanceID,
				Duration:       duration,
				Timestamp:      timestamp,
			},
		)
		if err != nil {
			r.registry.mutex.Lock()
			latestSession, isLatestFound := r.registry.sessions[sessionKey]
			if isLatestFound && latestSession.generation == generation &&
				latestSession.zone.NPCs() != nil {
				latestSession.zone.NPCs().ClearStun(
					result.Damage.ObjectID, stunExpiresAt,
				)
				latestSession.untrackCampaignNPCModifier(run)
				r.registry.sessions[sessionKey] = latestSession
			}
			r.registry.mutex.Unlock()
			_, releaseErr := run.release(r.npc.modifierPool)
			return nil, fmt.Errorf("shockwaveStunCreate: %w", errors.Join(err, releaseErr))
		}
		if !run.create() {
			continue
		}
		packets = append(packets, createPacket)
		targetObjectID := result.Damage.ObjectID
		expiryStep := campaignShockwaveStunExpiryStep{
			runtime: r, sessionKey: sessionKey, generation: generation,
			targetObjectID: targetObjectID, stunExpiresAt: stunExpiresAt,
			run: run,
		}
		deleteProducer := raknet.ScheduledPacketProducer{
			Delay:   duration,
			Produce: expiryStep.produce,
		}
		_, scheduleErr := scheduleNPCProducers(r.registry, packet,
			[]raknet.ScheduledPacketProducer{deleteProducer},
		)
		if scheduleErr != nil {
			r.registry.mutex.Lock()
			latestSession, isLatestFound := r.registry.sessions[sessionKey]
			if isLatestFound && latestSession.generation == generation &&
				latestSession.zone.NPCs() != nil {
				latestSession.zone.NPCs().ClearStun(targetObjectID, stunExpiresAt)
				latestSession.untrackCampaignNPCModifier(run)
				r.registry.sessions[sessionKey] = latestSession
			}
			r.registry.mutex.Unlock()
			_, releaseErr := run.release(r.npc.modifierPool)
			return nil, fmt.Errorf("shockwaveStunSchedule: %w", errors.Join(scheduleErr, releaseErr))
		}
	}
	return packets, nil
}

func (r campaignDamageRuntime) publishAreaResults(
	packet raknet.Packet, sessionKey string, generation uint64, sourceObjectID uint32,
	timestamp uint64, binding game.GameplayBinding, results []zoneability.AreaResult,
	transitions []campaignDamageTransition,
	effect func(zoneability.AreaResult) ([]byte, error), isEffectAfterDamage bool,
) ([][]byte, error) {
	packets := make([][]byte, 0, len(results)*3)
	if len(results) == 0 {
		return packets, nil
	}
	stealthPackets, err := breakTrapperStealthForDamage(
		r.registry, sessionKey, generation, sourceObjectID,
	)
	if err != nil {
		return nil, fmt.Errorf("areaStealthBreak: %w", err)
	}
	packets = append(packets, stealthPackets...)
	statDelta := sporenet.PlayerStatDelta{}
	for _, result := range results {
		if effect != nil && !isEffectAfterDamage {
			effectPacket, err := effect(result)
			if err != nil {
				return nil, fmt.Errorf("areaEffect: %w", err)
			}
			if len(effectPacket) != 0 {
				packets = append(packets, effectPacket)
			}
		}
		if result.Damage.IsDamageImmune {
			err = r.projection.publishEnemyDamage(
				sessionKey, generation, zonenpc.DamageEvent{
					SourceObjectID: sourceObjectID,
					TargetObjectID: result.Damage.ObjectID,
					Position:       result.Snapshot.Plan.Position,
					HitPoint:       result.Damage.HitPoint,
					IsDamageImmune: true,
				},
			)
			if err != nil {
				return nil, fmt.Errorf("areaImmuneProjection: %w", err)
			}
			continue
		}
		physics := r.npc.program.NPCDeathPhysics(result.Snapshot.Plan.NounName)
		definition, err := campaignNPCDeathDefinition(result.Snapshot, physics)
		if err != nil {
			return nil, fmt.Errorf("areaDeathDefinition: %w", err)
		}
		publication := zoneNPCDamagePublication{}
		if result.Damage.IsDefeated {
			objectiveErr := r.projection.recordEnemyDamageObjective(
				sessionKey, generation, zonenpc.DamageEvent{
					SourceObjectID: sourceObjectID,
					TargetObjectID: result.Damage.ObjectID,
					Position:       result.Snapshot.Plan.Position,
					Damage:         result.Damage.Damage,
					HitPoint:       result.Damage.HitPoint,
					IsCritical:     result.IsCritical,
				},
			)
			if objectiveErr != nil {
				r.logger.Printf(
					"RakNet lethal damage objective omitted target=%d: %v",
					result.Damage.ObjectID, objectiveErr,
				)
			}
			publication, err = marshalZoneNPCDamage(
				definition,
				zoneNPCDamageResult{
					hitPoint:   result.Damage.HitPoint,
					isDefeated: true, isFound: true,
				},
				sourceObjectID, result.Damage.Damage, result.IsCritical,
				timestamp, r.effectPool,
			)
			if err != nil {
				return nil, fmt.Errorf("areaDamagePublish: %w", err)
			}
			err = r.projection.publishEnemyDeath(
				sessionKey, generation, publication.deathRun.DrainProjection(),
			)
			if err != nil {
				publication.deathRun.Stop()
				return nil, fmt.Errorf("areaDeathProjection: %w", err)
			}
		} else {
			err = r.projection.publishEnemyDamage(
				sessionKey, generation, zonenpc.DamageEvent{
					SourceObjectID: sourceObjectID,
					TargetObjectID: result.Damage.ObjectID,
					Position:       result.Snapshot.Plan.Position,
					Damage:         result.Damage.Damage,
					HitPoint:       result.Damage.HitPoint,
					IsCritical:     result.IsCritical,
				},
			)
			if err != nil {
				return nil, fmt.Errorf("areaDamageProjection: %w", err)
			}
		}
		packets = append(packets, publication.packets...)
		wakePackets, err := r.breakSleepingCloudOnDamage(
			sessionKey, generation, result.Damage.ObjectID,
			result.Definition.DescriptorMask,
		)
		if err != nil {
			return nil, fmt.Errorf("areaSleepBreak: %w", err)
		}
		packets = append(packets, wakePackets...)
		retaliationPackets, err := r.publishCryosBasicFieryRetaliation(
			packet, sessionKey, generation, sourceObjectID, timestamp, result,
		)
		if err != nil {
			return nil, fmt.Errorf("areaFieryRetaliation: %w", err)
		}
		packets = append(packets, retaliationPackets...)
		reflectionPackets, reflectionStatDelta, reflectionErr :=
			r.applyScaldronThornoReflection(
				sessionKey, generation, sourceObjectID, timestamp, result,
			)
		if reflectionErr != nil {
			return nil, fmt.Errorf("areaThornoReflection: %w", reflectionErr)
		}
		packets = append(packets, reflectionPackets...)
		statDelta.PVEDamageTaken += reflectionStatDelta.PVEDamageTaken
		err = r.reserveQuantumStateReaction(
			sessionKey, generation, sourceObjectID,
			result.Damage.ObjectID, timestamp,
		)
		if err != nil {
			return nil, fmt.Errorf("areaQuantumState: %w", err)
		}
		if effect != nil && isEffectAfterDamage {
			effectPacket, err := effect(result)
			if err != nil {
				return nil, fmt.Errorf("areaEffect: %w", err)
			}
			if len(effectPacket) != 0 {
				packets = append(packets, effectPacket)
			}
		}
		if publication.deathRun != nil {
			deathRun := publication.deathRun
			err = r.npc.scheduleEnemyDeath(
				packet, sessionKey, generation, result.Damage.ObjectID, deathRun,
			)
			if err != nil {
				deathRun.Stop()
				return nil, fmt.Errorf("areaDeathSchedule: %w", err)
			}
		}
		if result.Damage.IsDefeated {
			defeatedEnemy := result.Snapshot
			defeatedEnemy.IsDefeated = true
			lootPackets, err := r.npc.spawnLoot(
				packet, sessionKey, generation, defeatedEnemy, timestamp,
			)
			if err != nil {
				return nil, fmt.Errorf("areaLoot: %w", err)
			}
			packets = append(packets, lootPackets...)
			refreshPackets, refreshErr := r.refreshArborealMightOnDeath(
				sessionKey, generation, result.Snapshot.Plan.Position, timestamp,
			)
			if refreshErr != nil {
				return nil, fmt.Errorf("areaArborealRefresh: %w", refreshErr)
			}
			packets = append(packets, refreshPackets...)
			charmPackets, charmErr := r.applyVoodooCharmKill(
				sessionKey, generation, sourceObjectID,
			)
			if charmErr != nil {
				r.logger.Printf(
					"Jinx passive recovery skipped source=%d: %v",
					sourceObjectID, charmErr,
				)
			} else {
				packets = append(packets, charmPackets...)
			}
		}
		poisonPackets, poisonErr := r.applyPoisonRavagerPassive(
			packet, sessionKey, generation, sourceObjectID, timestamp, result,
		)
		if poisonErr != nil {
			r.logger.Printf(
				"Viper passive poison skipped source=%d target=%d: %v",
				sourceObjectID, result.Damage.ObjectID, poisonErr,
			)
		} else {
			packets = append(packets, poisonPackets...)
		}
		hitPoisonPackets, hitPoisonErr := r.applyHeroHitPoison(
			packet, sessionKey, generation, sourceObjectID, timestamp, result,
		)
		if hitPoisonErr != nil {
			r.logger.Printf(
				"Hero hit poison skipped source=%d target=%d: %v",
				sourceObjectID, result.Damage.ObjectID, hitPoisonErr,
			)
		} else {
			packets = append(packets, hitPoisonPackets...)
		}
		vulnerabilityPackets, vulnerabilityErr := r.applyHeroEnergyVulnerability(
			packet, sessionKey, generation, sourceObjectID, timestamp, result,
		)
		if vulnerabilityErr != nil {
			r.logger.Printf(
				"Hero energy vulnerability skipped source=%d target=%d: %v",
				sourceObjectID, result.Damage.ObjectID, vulnerabilityErr,
			)
		} else {
			packets = append(packets, vulnerabilityPackets...)
		}
		statDelta.PVEDamageDealt += float64(result.Damage.Damage)
		if result.Damage.IsDefeated {
			if result.Snapshot.Plan.IsBoss {
				statDelta.PVEBossKill++
			} else if result.Snapshot.Plan.IsCaptain || result.Snapshot.Plan.IsElite {
				statDelta.PVESpecialKill++
			} else {
				statDelta.PVEMinionKill++
			}
		}
	}
	for _, transition := range transitions {
		transitionPackets, err := r.publishTransition(
			packet, sessionKey, generation, transition, timestamp,
		)
		if err != nil {
			return nil, fmt.Errorf("areaTransitionPublish: %w", err)
		}
		packets = append(packets, transitionPackets...)
	}
	err = r.npc.stats.Record(context.Background(), binding, statDelta)
	if err != nil {
		return nil, fmt.Errorf("areaStats: %w", err)
	}
	return packets, nil
}

func (r campaignDamageRuntime) publishCryosBasicFieryRetaliation(
	packet raknet.Packet, sessionKey string, generation uint64,
	sourceObjectID uint32, timestamp uint64, result zoneability.AreaResult,
) ([][]byte, error) {
	if result.Definition.DescriptorMask&1 == 0 || result.Damage.Damage <= 0 ||
		result.Damage.IsDamageImmune || result.Damage.IsDefeated {
		return nil, nil
	}
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	target, isTargetFound := zone.NPCTarget{}, false
	if isCurrent {
		target, isTargetFound = peerSession.zone.NPCTarget(sourceObjectID)
	}
	r.registry.mutex.RUnlock()
	if !isCurrent || !isTargetFound || target.HitPoint <= 0 {
		return nil, nil
	}
	retaliator := result.Snapshot
	retaliator.HitPoint = result.Damage.HitPoint
	retaliator.IsDefeated = result.Damage.IsDefeated
	plan, isPlanned := zonenpc.PlanCryosBasicFieryRetaliation(
		retaliator, sourceObjectID, target.Position,
	)
	if !isPlanned {
		return nil, nil
	}
	packets, err := r.npc.applyCampaignNPCPoison(
		packet, sessionKey, generation, plan, timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("fieryRetaliationApply: %w", err)
	}
	return packets, nil
}

type campaignCatalystUnlockStep struct {
	runtime    campaignDamageRuntime
	sessionKey string
	generation uint64
	run        *unlockraknet.CatalystRun
	source     sim.Position
	deadline   time.Duration
}

func (s campaignCatalystUnlockStep) produce() ([][]byte, error) {
	s.runtime.registry.mutex.Lock()
	peerSession, isFound := s.runtime.registry.sessions[s.sessionKey]
	isCurrent := isFound && peerSession.generation == s.generation &&
		peerSession.campaignUnlockPresentationSession().Catalyst() == s.run
	if !isCurrent {
		s.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	batch, err := s.run.Advance(context.Background(), s.deadline)
	if err != nil {
		s.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf(
			"transitionCatalystAdvance[%s]: %w", s.deadline, err,
		)
	}
	packet, err := peerSession.materializeCampaignCatalystUnlock(
		batch, s.source, s.deadline,
	)
	if err != nil {
		s.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf(
			"transitionCatalystMaterialize[%s]: %w", s.deadline, err,
		)
	}
	if s.deadline == zoneunlock.CatalystFinalDeadline {
		peerSession.campaignUnlockPresentationSession().ClearCatalyst(s.run)
		s.run.SetCancel(nil)
	}
	s.runtime.registry.sessions[s.sessionKey] = peerSession
	s.runtime.registry.mutex.Unlock()
	return packet, nil
}

type campaignCatalystUnlockFailure struct {
	runtime    campaignDamageRuntime
	sessionKey string
	generation uint64
	run        *unlockraknet.CatalystRun
}

func (f campaignCatalystUnlockFailure) handle(_ error) {
	f.runtime.registry.mutex.Lock()
	peerSession, isFound := f.runtime.registry.sessions[f.sessionKey]
	isCurrent := isFound && peerSession.generation == f.generation &&
		peerSession.campaignUnlockPresentationSession().Catalyst() == f.run
	if isCurrent {
		peerSession.campaignUnlockPresentationSession().ClearCatalyst(f.run)
		f.runtime.registry.sessions[f.sessionKey] = peerSession
	}
	f.runtime.registry.mutex.Unlock()
	if isCurrent {
		f.run.Stop()
	}
}

type campaignOverdriveUnlockStep struct {
	runtime    campaignDamageRuntime
	sessionKey string
	generation uint64
	run        *unlockraknet.Run
	deadline   time.Duration
}

func (s campaignOverdriveUnlockStep) produce() ([][]byte, error) {
	s.runtime.registry.mutex.Lock()
	peerSession, isFound := s.runtime.registry.sessions[s.sessionKey]
	isCurrent := isFound && peerSession.generation == s.generation &&
		peerSession.campaignUnlockPresentationSession().Overdrive() == s.run
	if !isCurrent {
		s.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	packet, err := s.run.Advance(context.Background(), s.deadline)
	if err != nil {
		s.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf(
			"transitionOverdriveAdvance[%s]: %w", s.deadline, err,
		)
	}
	if s.deadline == zoneunlock.OverdriveFinalDeadline {
		peerSession.campaignUnlockPresentationSession().ClearOverdrive(s.run)
		s.run.SetCancel(nil)
	}
	s.runtime.registry.sessions[s.sessionKey] = peerSession
	s.runtime.registry.mutex.Unlock()
	return packet, nil
}

type campaignOverdriveUnlockFailure struct {
	runtime    campaignDamageRuntime
	sessionKey string
	generation uint64
	run        *unlockraknet.Run
}

func (f campaignOverdriveUnlockFailure) handle(_ error) {
	f.runtime.registry.mutex.Lock()
	peerSession, isFound := f.runtime.registry.sessions[f.sessionKey]
	isCurrent := isFound && peerSession.generation == f.generation &&
		peerSession.campaignUnlockPresentationSession().Overdrive() == f.run
	if isCurrent {
		peerSession.campaignUnlockPresentationSession().ClearOverdrive(f.run)
		f.runtime.registry.sessions[f.sessionKey] = peerSession
	}
	f.runtime.registry.mutex.Unlock()
	if isCurrent {
		f.run.Stop()
	}
}

type campaignBossFollowupStep struct {
	runtime       campaignDamageRuntime
	packet        raknet.Packet
	sessionKey    string
	generation    uint64
	plans         []zonenpc.SpawnPlan
	outputPackets [][]byte
	timestamp     uint64
	delay         time.Duration
}

func (s campaignBossFollowupStep) produce() ([][]byte, error) {
	s.runtime.registry.mutex.Lock()
	peerSession, isFound := s.runtime.registry.sessions[s.sessionKey]
	if !isFound || peerSession.generation != s.generation {
		s.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	err := peerSession.admitReservedCampaignBoss(s.plans)
	var publishErr error
	if err == nil {
		publishErr = peerSession.zone.PublishNPCSpawn(zoneprojection.NPCSpawn{
			Plans: s.plans, TargetObjectID: peerSession.deployedObjectID,
			IsBossAddPhase: true, IsBossActive: true,
			BossObjectID: s.plans[0].ObjectID,
			IsFinalBoss:  zoneboss.IsFinalBossNoun(s.plans[0].NounName),
		}, peerSession.binding.UserID, s.generation)
	}
	if err == nil {
		s.runtime.registry.sessions[s.sessionKey] = peerSession
	}
	s.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("transitionBossAdmit: %w", err)
	}
	if publishErr != nil && s.runtime.logger != nil {
		s.runtime.logger.Printf(
			"RakNet campaign boss follow-up projection omitted object=%d: %v",
			s.plans[0].ObjectID, publishErr,
		)
	}
	actionPacket, err := s.runtime.npc.scheduleFirstActions(
		s.packet, s.sessionKey, s.generation, s.plans,
		s.timestamp+uint64(s.delay/time.Millisecond),
	)
	if err != nil {
		return nil, fmt.Errorf("transitionBossAction: %w", err)
	}
	packets := append([][]byte(nil), s.outputPackets...)
	return append(packets, actionPacket...), nil
}

type campaignHordeFollowupStep struct {
	runtime       campaignDamageRuntime
	packet        raknet.Packet
	sessionKey    string
	generation    uint64
	markerSet     string
	plans         []zonenpc.SpawnPlan
	outputPackets [][]byte
	timestamp     uint64
	delay         time.Duration
}

func (s campaignHordeFollowupStep) produce() ([][]byte, error) {
	s.runtime.registry.mutex.Lock()
	peerSession, isFound := s.runtime.registry.sessions[s.sessionKey]
	if !isFound || peerSession.generation != s.generation {
		s.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	err := peerSession.admitReservedCampaignHorde(s.markerSet, s.plans)
	if err == nil {
		s.runtime.registry.sessions[s.sessionKey] = peerSession
	}
	s.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("transitionHordeAdmit: %w", err)
	}
	actionPacket, err := s.runtime.npc.scheduleFirstActions(
		s.packet, s.sessionKey, s.generation, s.plans,
		s.timestamp+uint64(s.delay/time.Millisecond),
	)
	if err != nil {
		return nil, fmt.Errorf("transitionHordeAction: %w", err)
	}
	packet := append([][]byte(nil), s.outputPackets...)
	return append(packet, actionPacket...), nil
}

type campaignDamageRuntime struct {
	registry     *gameplaySessionRegistry
	npc          campaignNPCActionRuntime
	projection   gameplayProjectionRuntime
	effectPool   *attachedEffectPool
	gameplayJoin *game.GameplayJoin
	logger       *log.Logger
}

type campaignCompanionAttackStep struct {
	runtime          campaignDamageRuntime
	packet           raknet.Packet
	sessionKey       string
	generation       uint64
	sourceTime       uint64
	plan             zonecompanion.Attack
	start            summonCompanionAttackStart
	ability          sim.AbilityDefinition
	criticalNounID   uint32
	isBeast          bool
	isPlasmaSentinel bool
}

type campaignCompanionPursuitStep struct {
	runtime    campaignDamageRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	sourceTime uint64
	plan       zonecompanion.Pursuit
}

func applyCompanionAttackBuff(
	ability sim.AbilityDefinition, plan zonecompanion.Attack,
) sim.AbilityDefinition {
	damageScale := 1 + plan.DamageBuff + plan.EnergyDamageBuff
	ability.MinimumDamage = float32(math.Floor(float64(
		ability.MinimumDamage * damageScale,
	)))
	ability.MaximumDamage = float32(math.Ceil(float64(
		ability.MaximumDamage * damageScale,
	)))
	attackScale := max(float32(0.05), 1+plan.AttackSpeed)
	ability.HitDelay = time.Duration(float64(ability.HitDelay) / float64(attackScale))
	ability.ReleaseDelay = time.Duration(
		float64(ability.ReleaseDelay) / float64(attackScale),
	)
	ability.Cooldown = time.Duration(float64(ability.Cooldown) / float64(attackScale))
	return ability
}

func (r campaignDamageRuntime) startCompanionAttacks(
	packet raknet.Packet, sessionKey string, generation uint64, sourceTime uint64,
) ([][]byte, error) {
	if packet.ScheduleGroup == nil && packet.ScheduleGroupResult == nil {
		return nil, errors.New("companion schedule unavailable")
	}
	packets := make([][]byte, 0)
	fieldPackets, err := r.startFieldMedicDroneAttack(
		packet, sessionKey, generation, sourceTime,
	)
	if err != nil {
		return nil, fmt.Errorf("fieldMedicCompanion: %w", err)
	}
	packets = append(packets, fieldPackets...)
	fireTempestPackets, err := r.startFireTempestPetAttack(
		packet, sessionKey, generation, sourceTime,
	)
	if err != nil {
		return nil, fmt.Errorf("fireTempestCompanion: %w", err)
	}
	packets = append(packets, fireTempestPackets...)
	plasmaPackets, err := r.startPlasmaSentinelPetAttack(
		packet, sessionKey, generation, sourceTime,
	)
	if err != nil {
		return nil, fmt.Errorf("plasmaSentinelCompanion: %w", err)
	}
	packets = append(packets, plasmaPackets...)
	beastPackets, err := r.startBeastPetAttack(
		packet, sessionKey, generation, sourceTime,
	)
	if err != nil {
		return nil, fmt.Errorf("beastCompanion: %w", err)
	}
	packets = append(packets, beastPackets...)
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.sagePassive != nil && peerSession.zone != nil &&
		peerSession.zone.Companion() != nil && peerSession.zone.NPCs() != nil
	if !isCurrent {
		r.registry.mutex.Unlock()
		return packets, nil
	}
	ability := r.npc.program.SupportHealerPetBasic
	petDamage := float32(0)
	if peerSession.deployedCreatureIndex < uint32(len(peerSession.binding.Creatures)) {
		petDamage = peerSession.binding.Creatures[peerSession.deployedCreatureIndex].PetDamage
	}
	damageRange, err := game.ResolveAbilityDamageRange(
		game.AbilityDamage{
			Minimum: ability.MinimumDamage, Maximum: ability.MaximumDamage,
			Coefficient: ability.DamageCoefficient,
		},
		game.DamageProfile{PrimaryAttribute: petDamage, IsPrimaryAttributeFound: true},
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("companionDamageRange: %w", err)
	}
	ability.MinimumDamage = damageRange.Minimum
	ability.MaximumDamage = damageRange.Maximum
	plans, err := peerSession.zone.Companion().ReserveAttacks(
		peerSession.zone.NPCs().LiveSnapshots(), ability.Range, r.npc.now(),
		ability.HitDelay+ability.Cooldown,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("companionReserve: %w", err)
	}
	starts := make([]campaignCompanionAttackStep, 0, len(plans))
	for _, plan := range plans {
		actorAbility := applyCompanionAttackBuff(ability, plan)
		activation, isActivationFound :=
			peerSession.sagePassiveActivations[plan.ObjectID]
		if !isActivationFound {
			peerSession.zone.Companion().ReleaseAttack(
				plan.ObjectID, plan.TargetObjectID,
			)
			continue
		}
		activation.Position = raknet.Vector3{
			X: plan.Position.X, Y: plan.Position.Y, Z: plan.Position.Z,
		}
		start, startErr := newSummonCompanionAttack(summonCompanionAttackInput{
			Ability: actorAbility, CompanionID: plan.ObjectID,
			TargetID: plan.TargetObjectID,
			Companion: sim.Position{
				X: plan.Position.X, Y: plan.Position.Y, Z: plan.Position.Z,
			},
			Target: sim.Position{
				X: plan.TargetPosition.X, Y: plan.TargetPosition.Y,
				Z: plan.TargetPosition.Z,
			},
			TargetHitPoint: plan.TargetHitPoint,
			Damage:         actorAbility.MinimumDamage, SourceTime: sourceTime,
			CenterRange: plan.CenterRange, StartedAt: r.npc.now(),
		})
		if startErr != nil {
			for _, current := range starts {
				current.start.Run.Stop()
				peerSession.zone.Companion().ReleaseAttack(
					current.plan.ObjectID, current.plan.TargetObjectID,
				)
			}
			peerSession.zone.Companion().ReleaseAttack(
				plan.ObjectID, plan.TargetObjectID,
			)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("companionStart[%d]: %w", plan.ObjectID, startErr)
		}
		activation.Attack = start.Run
		activation.TargetObjectID = plan.TargetObjectID
		activation.CooldownEnd = plan.CooldownEnd
		peerSession.sagePassiveActivations[plan.ObjectID] = activation
		step := campaignCompanionAttackStep{
			runtime: r, packet: packet, sessionKey: sessionKey,
			generation: generation, sourceTime: sourceTime,
			plan: plan, start: start, ability: actorAbility,
			criticalNounID: util.HashID("HelperMelee"),
		}
		starts = append(starts, step)
	}
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	for _, step := range starts {
		err = step.schedule()
		if err != nil {
			r.logger.Printf(
				"RakNet campaign companion attack schedule rejected for %s/%d: %v",
				sessionKey, step.plan.ObjectID, err,
			)
			continue
		}
		packets = append(packets, step.start.Packet...)
	}
	pursuitPackets, err := r.startCompanionPursuits(
		packet, sessionKey, generation, sourceTime,
	)
	if err != nil {
		return nil, fmt.Errorf("companionPursuit: %w", err)
	}
	packets = append(packets, pursuitPackets...)
	return packets, nil
}

func (r campaignDamageRuntime) startCompanionPursuits(
	packet raknet.Packet, sessionKey string, generation uint64, sourceTime uint64,
) ([][]byte, error) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.sagePassive != nil && peerSession.zone != nil &&
		peerSession.zone.Companion() != nil && peerSession.zone.NPCs() != nil
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	plans, err := peerSession.zone.Companion().ReservePursuits(
		peerSession.zone.NPCs().LiveSnapshots(),
		r.npc.program.SupportHealerPetBasic.Range,
		zonecompanion.CompatibilityAggroRadius,
		zonecompanion.CompatibilityMovementSpeed,
	)
	r.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("pursuitReserve: %w", err)
	}
	packets := make([][]byte, 0, len(plans))
	for _, plan := range plans {
		pursuitPackets, marshalErr := companionraknet.Pursuit(plan)
		if marshalErr != nil {
			r.cancelCompanionPursuit(
				sessionKey, generation, plan.ObjectID, plan.TargetObjectID,
				plan.Position,
			)
			return nil, fmt.Errorf("pursuitMarshal[%d]: %w", plan.ObjectID, marshalErr)
		}
		step := campaignCompanionPursuitStep{
			runtime: r, packet: packet, sessionKey: sessionKey,
			generation: generation, sourceTime: sourceTime, plan: plan,
		}
		scheduleErr := step.schedule()
		if scheduleErr != nil {
			r.logger.Printf(
				"RakNet campaign companion pursuit schedule rejected for %s/%d: %v",
				sessionKey, plan.ObjectID, scheduleErr,
			)
			continue
		}
		packets = append(packets, pursuitPackets...)
	}
	return packets, nil
}

func (e campaignCompanionPursuitStep) schedule() error {
	producers := e.runtime.registry.producerGuard.scheduledProducers(
		e.sessionKey, []raknet.ScheduledPacketProducer{{
			Delay: e.plan.TravelDuration, Produce: e.arrive,
		}},
	)
	var err error
	if e.packet.ScheduleGroupResult != nil {
		_, err = e.packet.ScheduleGroupResult(producers, e.fail)
	} else if e.packet.ScheduleGroup != nil {
		_, err = e.packet.ScheduleGroup(producers)
	} else {
		err = errors.New("schedule unavailable")
	}
	if err != nil {
		e.fail(err)
		return fmt.Errorf("schedule: %w", err)
	}
	return nil
}

func (e campaignCompanionPursuitStep) arrive() ([][]byte, error) {
	isReleased := e.runtime.releaseCompanionPursuit(
		e.sessionKey, e.generation, e.plan.ObjectID, e.plan.TargetObjectID,
	)
	if !isReleased {
		return nil, nil
	}
	timestamp := e.sourceTime +
		uint64(e.plan.TravelDuration/time.Millisecond)
	npcPackets, err := e.runtime.refreshNPCTargets(
		e.packet, e.sessionKey, e.generation, timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("companionArrivalTarget: %w", err)
	}
	companionPackets, err := e.runtime.startCompanionAttacks(
		e.packet, e.sessionKey, e.generation,
		timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("companionArrivalAttack: %w", err)
	}
	return append(npcPackets, companionPackets...), nil
}

func (e campaignCompanionPursuitStep) fail(scheduleErr error) {
	isReleased := e.runtime.cancelCompanionPursuit(
		e.sessionKey, e.generation, e.plan.ObjectID, e.plan.TargetObjectID,
		e.plan.Position,
	)
	if !isReleased {
		return
	}
	e.runtime.logger.Printf(
		"RakNet campaign companion pursuit stopped after schedule failure for %s/%d: %v",
		e.sessionKey, e.plan.ObjectID, scheduleErr,
	)
}

func (r campaignDamageRuntime) cancelCompanionPursuit(
	sessionKey string, generation uint64,
	objectID uint32, targetObjectID uint32, position game.Vec3,
) bool {
	r.registry.mutex.Lock()
	defer r.registry.mutex.Unlock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	if !isFound || peerSession.generation != generation ||
		peerSession.zone == nil || peerSession.zone.Companion() == nil {
		return false
	}
	return peerSession.zone.Companion().CancelPursuit(
		objectID, targetObjectID, position,
	)
}

func (r campaignDamageRuntime) releaseCompanionPursuit(
	sessionKey string, generation uint64,
	objectID uint32, targetObjectID uint32,
) bool {
	r.registry.mutex.Lock()
	defer r.registry.mutex.Unlock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	if !isFound || peerSession.generation != generation ||
		peerSession.zone == nil || peerSession.zone.Companion() == nil {
		return false
	}
	return peerSession.zone.Companion().ReleasePursuit(
		objectID, targetObjectID,
	)
}

func (r campaignDamageRuntime) refreshNPCTargets(
	packet raknet.Packet, sessionKey string, generation uint64, timestamp uint64,
) ([][]byte, error) {
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.zone != nil
	var campaignZone *zone.Zone
	if isCurrent {
		campaignZone = peerSession.zone
	}
	r.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	acquired, err := campaignZone.RefreshNPCTargets()
	if err != nil {
		return nil, fmt.Errorf("npcTargetRefresh: %w", err)
	}
	plans := make([]zonenpc.SpawnPlan, 0, len(acquired))
	for _, npc := range acquired {
		plans = append(plans, npc.Plan)
	}
	packets, err := r.npc.scheduleFirstActions(
		packet, sessionKey, generation, plans, timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("npcTargetSchedule: %w", err)
	}
	return packets, nil
}

func (e campaignCompanionAttackStep) schedule() error {
	ability := e.ability
	repeatDelay := max(
		ability.ReleaseDelay, ability.HitDelay+ability.Cooldown,
	)
	producers := e.runtime.registry.producerGuard.scheduledProducers(
		e.sessionKey, []raknet.ScheduledPacketProducer{
			{
				Delay:   ability.HitDelay,
				Produce: e.hit,
			},
			{
				Delay:   ability.ReleaseDelay,
				Produce: e.release,
			},
			{
				Delay:   repeatDelay,
				Produce: e.repeat,
			},
		},
	)
	var cancel raknet.CancelSchedule
	var err error
	if e.packet.ScheduleGroupResult != nil {
		cancel, err = e.packet.ScheduleGroupResult(producers, e.fail)
	} else {
		cancel, err = e.packet.ScheduleGroup(producers)
	}
	if err != nil {
		e.fail(err)
		return fmt.Errorf("schedule: %w", err)
	}
	e.start.Run.SetCancel(cancel)
	return nil
}

func (e campaignCompanionAttackStep) repeat() ([][]byte, error) {
	ability := e.ability
	repeatDelay := max(
		ability.ReleaseDelay, ability.HitDelay+ability.Cooldown,
	)
	if e.isBeast {
		return e.runtime.startBeastPetAttack(
			e.packet, e.sessionKey, e.generation,
			e.sourceTime+uint64(repeatDelay/time.Millisecond),
		)
	}
	if e.isPlasmaSentinel {
		return e.runtime.startPlasmaSentinelPetAttack(
			e.packet, e.sessionKey, e.generation,
			e.sourceTime+uint64(repeatDelay/time.Millisecond),
		)
	}
	return e.runtime.startCompanionAttacks(
		e.packet, e.sessionKey, e.generation,
		e.sourceTime+uint64(repeatDelay/time.Millisecond),
	)
}

func (e campaignCompanionAttackStep) hit() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	activation, isActivationFound :=
		peerSession.sagePassiveActivations[e.plan.ObjectID]
	companion, isCompanionFound := zonecompanion.Actor{}, false
	if isFound && peerSession.zone != nil {
		companion, isCompanionFound =
			peerSession.zone.Companion().Snapshot(e.plan.ObjectID)
	}
	isCurrent := isFound && peerSession.generation == e.generation &&
		isActivationFound && activation.Attack == e.start.Run &&
		isCompanionFound && companion.HitPoint > 0
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	target, isTargetFound := peerSession.zone.NPCs().NPC(e.plan.TargetObjectID)
	isHitValid := isTargetFound && !target.IsDefeated && target.HitPoint > 0 &&
		companion.Position.Sub(target.Plan.Position).Length() <= e.plan.CenterRange
	damage := e.ability.MinimumDamage
	isCritical := false
	if isHitValid {
		selectedDamage, damageErr := sim.SelectRankDamage(
			peerSession.zone.NPCRandom(),
			sim.DamageRange{
				Minimum: e.ability.MinimumDamage,
				Maximum: e.ability.MaximumDamage,
			},
		)
		if damageErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("damageSelect: %w", damageErr)
		}
		damage = selectedDamage
		profile, isProfileFound := e.runtime.npc.program.NonPlayerCritical[e.criticalNounID]
		if isProfileFound || e.runtime.npc.program.Critical.DamageBonus == 0 {
			critical, criticalErr := resolveZoneNonPlayerCritical(
				peerSession.zone.NPCRandom(), profile,
				e.runtime.npc.program.Critical, damage,
			)
			if criticalErr != nil {
				e.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("criticalResolve: %w", criticalErr)
			}
			damage = critical.Damage
			isCritical = critical.IsCritical
		}
	}
	targetPosition := e.plan.TargetPosition
	targetHitPoint := e.plan.TargetHitPoint
	if isTargetFound {
		targetPosition = target.Plan.Position
		targetHitPoint = target.HitPoint
	}
	facing := targetPosition.Sub(companion.Position)
	facingLength := facing.Length()
	if facingLength > 0 {
		facing = facing.Scale(1 / facingLength)
	}
	err := e.start.Run.PrepareHit(
		isHitValid, max(targetHitPoint, damage), damage, isCritical,
		sim.Position{
			X: targetPosition.X, Y: targetPosition.Y, Z: targetPosition.Z,
		},
		sim.Position{X: facing.X, Y: facing.Y, Z: facing.Z},
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("hitPrepare: %w", err)
	}
	result := zonenpc.DamageResult{}
	transition := campaignDamageTransition{}
	if isHitValid {
		result, err = peerSession.zone.NPCs().Hit(zonenpc.HitRequest{
			SourceObjectID: e.plan.ObjectID, TargetObjectID: e.plan.TargetObjectID, Damage: damage,
			SourcePosition: &companion.Position, Metadata: zoneability.NPCDamageMetadata(e.ability),
		})
		if err == nil {
			transition, err = peerSession.applyCampaignDamageTransition(result)
		}
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("damageCommit: %w", err)
		}
	}
	binding := peerSession.binding
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	packets, err := e.start.Run.Advance(
		context.Background(), e.start.Run.HitDeadline(),
	)
	if err != nil {
		return nil, fmt.Errorf("hitAdvance: %w", err)
	}
	packets = summonCompanionPresentationPackets(packets)
	if !isHitValid {
		return packets, nil
	}
	resultPackets, err := e.runtime.publishAreaResults(
		e.packet, e.sessionKey, e.generation, e.plan.ObjectID,
		e.sourceTime+uint64(
			e.ability.HitDelay/time.Millisecond,
		),
		binding, []zoneability.AreaResult{{
			Snapshot: target, Damage: result, IsCritical: isCritical,
			Definition: e.ability,
		}},
		[]campaignDamageTransition{transition}, nil, false,
	)
	if err != nil {
		return nil, fmt.Errorf("damagePublish: %w", err)
	}
	return append(packets, resultPackets...), nil
}

func (e campaignCompanionAttackStep) release() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	activation, isActivationFound :=
		peerSession.sagePassiveActivations[e.plan.ObjectID]
	isCurrent := isFound && peerSession.generation == e.generation &&
		isActivationFound && activation.Attack == e.start.Run
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	packets, err := e.start.Run.Advance(
		context.Background(), e.start.Run.ReleaseDeadline(),
	)
	if err == nil {
		e.start.Run.ClearCancel()
		activation.Attack = nil
		activation.TargetObjectID = 0
		peerSession.sagePassiveActivations[e.plan.ObjectID] = activation
		peerSession.zone.Companion().ReleaseAttack(
			e.plan.ObjectID, e.plan.TargetObjectID,
		)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("releaseAdvance: %w", err)
	}
	return packets, nil
}

func (e campaignCompanionAttackStep) fail(scheduleErr error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	activation, isActivationFound :=
		peerSession.sagePassiveActivations[e.plan.ObjectID]
	isCurrent := isFound && peerSession.generation == e.generation &&
		isActivationFound && activation.Attack == e.start.Run
	if isCurrent {
		e.start.Run.ClearCancel()
		activation.Attack = nil
		activation.TargetObjectID = 0
		peerSession.sagePassiveActivations[e.plan.ObjectID] = activation
		peerSession.zone.Companion().ReleaseAttack(
			e.plan.ObjectID, e.plan.TargetObjectID,
		)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return
	}
	e.start.Run.Stop()
	e.runtime.logger.Printf(
		"RakNet campaign companion attack stopped after schedule failure for %s/%d: %v",
		e.sessionKey, e.plan.ObjectID, scheduleErr,
	)
}

func (r campaignDamageRuntime) publishTransition(
	packet raknet.Packet, sessionKey string, generation uint64,
	transition campaignDamageTransition, timestamp uint64,
) ([][]byte, error) {
	if transition.bossStartError != nil {
		r.logger.Printf("RakNet campaign later boss fallback skipped for %s: %v",
			sessionKey, transition.bossStartError)
	}
	packets := make([][]byte, 0, len(transition.immediatePackets)+1)
	var fearCleanupPackets [][]byte
	if transition.fearCleanupObjectID != 0 {
		fearPackets, err := r.stopHeroNPCFear(
			sessionKey, generation, transition.fearCleanupObjectID,
		)
		if err != nil {
			return nil, fmt.Errorf("transitionFearCleanup: %w", err)
		}
		fearCleanupPackets = fearPackets
	}
	var healingCleanupPackets [][]byte
	if transition.healingCleanupObjectID != 0 {
		healingPackets, err := r.stopHeroHealingReductions(
			sessionKey, generation, transition.healingCleanupObjectID,
		)
		if err != nil {
			return nil, fmt.Errorf("transitionHealingCleanup: %w", err)
		}
		healingCleanupPackets = healingPackets
	}
	var sinkholeCleanupPacket []byte
	if transition.sinkholeCleanupObjectID != 0 {
		r.registry.mutex.Lock()
		peerSession, isFound := r.registry.sessions[sessionKey]
		isCurrent := isFound && peerSession.generation == generation
		var err error
		if isCurrent {
			sinkholeCleanupPacket, err = peerSession.stopCampaignSinkholeEffect(
				transition.sinkholeCleanupObjectID, r.effectPool,
			)
			if err == nil {
				r.registry.sessions[sessionKey] = peerSession
			}
		}
		r.registry.mutex.Unlock()
		if err != nil {
			return nil, fmt.Errorf("transitionSinkholeCleanup: %w", err)
		}
	}
	if len(transition.selfResurrectionPlans) != 0 {
		for index, plan := range transition.selfResurrectionPlans {
			animationPacket, err := npcraknet.AnimationState(
				plan.ObjectID, campaignSelfResurrectionAnimation, timestamp,
			)
			if err != nil {
				return nil, fmt.Errorf(
					"transitionSelfResurrectAnimation[%d]: %w", index, err,
				)
			}
			packets = append(packets, animationPacket)
		}
	}
	if len(transition.corruptorStageTwoPlans) != 0 {
		for index, plan := range transition.corruptorStageTwoPlans {
			deletePacket, err := raknet.MarshalApplication(
				raknet.ObjectDeleteMessage{ObjectID: []uint32{plan.ObjectID}},
			)
			if err != nil {
				return nil, fmt.Errorf(
					"transitionCorruptorDelete[%d]: %w", index, err,
				)
			}
			spawnPackets, err := npcraknet.TargetedSpawn(
				plan, transition.corruptorStageTwoTargetObjectID,
			)
			if err != nil {
				return nil, fmt.Errorf(
					"transitionCorruptorSpawn[%d]: %w", index, err,
				)
			}
			packets = append(packets, deletePacket)
			packets = append(packets, spawnPackets...)
			animationPacket, err := npcraknet.AnimationState(
				plan.ObjectID, campaignCorruptorStageTwoAnimation, timestamp,
			)
			if err != nil {
				return nil, fmt.Errorf(
					"transitionCorruptorAnimation[%d]: %w", index, err,
				)
			}
			packets = append(packets, animationPacket)
		}
	}
	if transition.corruptorPhaseObjectID != 0 {
		step := campaignCorruptorPhaseStep{
			runtime: r.npc, packet: packet.Autonomous(), sessionKey: sessionKey,
			generation: generation, objectID: transition.corruptorPhaseObjectID,
			timestamp: timestamp,
		}
		phasePackets, phaseErr := step.produce()
		if phaseErr != nil {
			return nil, fmt.Errorf("transitionCorruptorPhase: %w", phaseErr)
		}
		packets = append(packets, phasePackets...)
	}
	if transition.corruptorPortalRespawn != nil {
		respawn := transition.corruptorPortalRespawn
		step := campaignCorruptorPortalRespawnStep{
			runtime: r.npc, packet: packet.Autonomous(), sessionKey: sessionKey,
			generation: generation, plan: *respawn,
			timestamp: timestamp + uint64(respawn.delay/time.Millisecond),
		}
		scheduleErr := scheduleNPCProducer(r.registry, step.packet, respawn.delay, step.produce)
		if scheduleErr != nil {
			fallbackPackets, fallbackErr := step.produce()
			if fallbackErr != nil {
				return nil, fmt.Errorf(
					"transitionCorruptorPortalFallback: %w",
					errors.Join(scheduleErr, fallbackErr),
				)
			}
			packets = append(packets, fallbackPackets...)
		}
	}
	packets = append(packets, transition.immediatePackets...)
	if sinkholeCleanupPacket != nil {
		packets = append(packets, sinkholeCleanupPacket)
	}
	// Publish the modifier deletion after the death state. The native corpse
	// transition snapshots attached status presentation, so deleting Terrified
	// first can leave its visual copied onto the defeated actor.
	packets = append(packets, fearCleanupPackets...)
	packets = append(packets, healingCleanupPackets...)
	if len(transition.bossCompletionPacket) != 0 {
		step := campaignBossCompletionStep{
			runtime: r, sessionKey: sessionKey, generation: generation,
			packet: transition.bossCompletionPacket,
		}
		scheduleErr := scheduleNPCProducer(r.registry, packet,
			transition.bossCompletionDelay, step.produce,
		)
		if scheduleErr != nil {
			packets = append(packets, transition.bossCompletionPacket)
			if r.logger != nil {
				r.logger.Printf(
					"RakNet delayed boss completion published immediately after schedule failure: %v",
					scheduleErr,
				)
			}
		}
	}
	if transition.nashiraSplit != nil {
		for index, objectID := range transition.nashiraSplit.objectIDs {
			animationPacket, err := npcraknet.AnimationState(
				objectID, "shadowboss_split_a", timestamp,
			)
			if err != nil {
				return nil, fmt.Errorf(
					"transitionNashiraSplitAnimation[%d]: %w", index, err,
				)
			}
			packets = append(packets, animationPacket)
		}
		step := campaignNashiraSplitStep{
			runtime: r.npc, packet: packet.Autonomous(), sessionKey: sessionKey,
			generation: generation, plan: *transition.nashiraSplit,
			timestamp: timestamp +
				uint64(campaignNashiraPreSplitDuration/time.Millisecond),
		}
		scheduleErr := scheduleNPCProducer(r.registry, step.packet,
			campaignNashiraPreSplitDuration, step.produce,
		)
		if scheduleErr != nil {
			step.timestamp = timestamp
			fallbackPackets, fallbackErr := step.produce()
			if fallbackErr != nil {
				return nil, fmt.Errorf(
					"transitionNashiraSplitFallback: %w",
					errors.Join(scheduleErr, fallbackErr),
				)
			}
			packets = append(packets, fallbackPackets...)
			r.logger.Printf(
				"RakNet Nashira split published immediately after schedule failure: %v",
				scheduleErr,
			)
		}
	}
	if len(transition.selfResurrectionPlans) != 0 {
		step := campaignSelfResurrectionActionStep{
			runtime: r.npc, packet: packet.Autonomous(), sessionKey: sessionKey,
			generation: generation, plans: transition.selfResurrectionPlans,
			timestamp: timestamp +
				uint64(campaignSelfResurrectionDuration/time.Millisecond),
		}
		scheduleErr := scheduleNPCProducer(r.registry, step.packet,
			campaignSelfResurrectionDuration, step.produce,
		)
		if scheduleErr != nil {
			step.timestamp = timestamp
			fallbackPackets, fallbackErr := step.produce()
			if fallbackErr != nil {
				return nil, fmt.Errorf(
					"transitionSelfResurrectFallback: %w",
					errors.Join(scheduleErr, fallbackErr),
				)
			}
			packets = append(packets, fallbackPackets...)
			r.logger.Printf(
				"RakNet Pouncing Stalker resumed immediately after resurrection schedule failure: %v",
				scheduleErr,
			)
		}
	}
	if len(transition.corruptorStageTwoPlans) != 0 {
		plan := transition.corruptorStageTwoPlans[0]
		step := campaignCorruptorPhaseStep{
			runtime: r.npc, packet: packet.Autonomous(), sessionKey: sessionKey,
			generation: generation, objectID: plan.ObjectID, isStageTwoStart: true,
			timestamp: timestamp +
				uint64(campaignCorruptorStageTwoDuration/time.Millisecond),
		}
		scheduleErr := scheduleNPCProducer(r.registry, step.packet,
			campaignCorruptorStageTwoDuration, step.produce,
		)
		if scheduleErr != nil {
			step.timestamp = timestamp
			fallbackPackets, fallbackErr := step.produce()
			if fallbackErr != nil {
				return nil, fmt.Errorf(
					"transitionCorruptorFallback: %w",
					errors.Join(scheduleErr, fallbackErr),
				)
			}
			packets = append(packets, fallbackPackets...)
			r.logger.Printf(
				"RakNet Corruptor stage two resumed immediately after schedule failure: %v",
				scheduleErr,
			)
		}
	}
	if transition.isTutorialHordeComplete {
		completionPackets, completionErr := r.scheduleTutorialHordeCompletion(
			packet, sessionKey, generation,
		)
		if completionErr != nil {
			return nil, fmt.Errorf(
				"transitionTutorialHordeComplete: %w", completionErr,
			)
		}
		packets = append(packets, completionPackets...)
	}
	if transition.isTutorialHordeNextWave {
		step := tutorialHordeWaveStep{
			registry: r.registry, npc: r.npc, logger: r.logger,
			packet: packet, sessionKey: sessionKey, generation: generation,
			timestamp: timestamp, delay: tutorialHordeNextWaveDelay,
		}
		scheduleErr := scheduleTutorialHordeWave(packet, step)
		if scheduleErr != nil {
			fallbackPackets, fallbackErr := step.produce()
			if fallbackErr != nil {
				return nil, fmt.Errorf(
					"transitionTutorialHordeFallback: %w",
					errors.Join(scheduleErr, fallbackErr),
				)
			}
			packets = append(packets, fallbackPackets...)
			r.logger.Printf(
				"RakNet tutorial horde next wave published immediately after schedule failure: %v",
				scheduleErr,
			)
		}
	}
	experiencePackets, err := r.publishExperience(
		sessionKey, generation, transition.defeatedObjectID, transition.experience,
	)
	if err != nil {
		return nil, fmt.Errorf("transitionExperience: %w", err)
	}
	packets = append(packets, experiencePackets...)
	if transition.defeatedObjectID != 0 {
		fearPackets, err := r.npc.stopCampaignNPCFearSource(
			sessionKey, generation, transition.defeatedObjectID,
		)
		if err != nil {
			return nil, fmt.Errorf("transitionFear: %w", err)
		}
		packets = append(packets, fearPackets...)
		drainPackets, err := r.npc.stopHealthDrain(
			sessionKey, generation, transition.defeatedObjectID,
		)
		if err != nil {
			return nil, fmt.Errorf("transitionHealthDrain: %w", err)
		}
		packets = append(packets, drainPackets...)
		modifierPackets, err := r.npc.stopZelemEnergyBuff(
			sessionKey, generation, transition.defeatedObjectID,
		)
		if err != nil {
			return nil, fmt.Errorf("transitionEnergyBuff: %w", err)
		}
		packets = append(packets, modifierPackets...)
		munchPackets, err := r.npc.stopCarrionMunch(
			sessionKey, generation, transition.defeatedObjectID,
		)
		if err != nil {
			return nil, fmt.Errorf("transitionMunch: %w", err)
		}
		packets = append(packets, munchPackets...)
		growthPackets, err := r.npc.stopOozeGrowth(
			sessionKey, generation, transition.defeatedObjectID,
		)
		if err != nil {
			return nil, fmt.Errorf("transitionOozeGrowth: %w", err)
		}
		packets = append(packets, growthPackets...)
		minePackets, isMineHandled, mineErr := r.npc.spawnScaldronDeathMines(
			packet, sessionKey, generation, transition.defeatedObjectID, timestamp,
		)
		if mineErr != nil {
			r.logger.Printf(
				"RakNet Scaldron death mines omitted object=%d: %v",
				transition.defeatedObjectID, mineErr,
			)
		} else if isMineHandled {
			packets = append(packets, minePackets...)
		}
		isDetonationHandled, detonationErr := r.npc.scheduleBoomerDeathDetonation(
			packet, sessionKey, generation, transition.defeatedObjectID, timestamp,
		)
		if detonationErr != nil {
			r.logger.Printf(
				"RakNet Lightning Juggernaut death detonation omitted object=%d: %v",
				transition.defeatedObjectID, detonationErr,
			)
		} else if isDetonationHandled {
			r.logger.Printf(
				"RakNet Lightning Juggernaut death detonation scheduled object=%d",
				transition.defeatedObjectID,
			)
		}
	}
	if transition.shieldObjectID != 0 {
		if transition.shieldDuration <= 0 {
			return nil, errors.New("transition shield duration unavailable")
		}
		startPacket, err := npcraknet.ShieldAnimation(
			transition.shieldObjectID, "nomad_lieu_tc_3_shield_start", timestamp,
		)
		if err != nil {
			return nil, fmt.Errorf("transitionShieldStart: %w", err)
		}
		packets = append(packets, startPacket)
		shieldRun := &campaignShieldTransitionRun{
			runtime: r, sessionKey: sessionKey, generation: generation,
			objectID: transition.shieldObjectID, timestamp: timestamp,
			duration: transition.shieldDuration,
		}
		shieldProducer := shieldRun.producers()
		shieldProducer = r.registry.producerGuard.scheduledProducers(sessionKey, shieldProducer)
		var scheduleErr error
		if packet.ScheduleGroupResult != nil {
			_, scheduleErr = packet.ScheduleGroupResult(
				shieldProducer, shieldRun.cleanup,
			)
		} else if packet.ScheduleGroup != nil {
			_, scheduleErr = packet.ScheduleGroup(shieldProducer)
		} else {
			scheduleErr = errors.New("scheduler unavailable")
		}
		if scheduleErr != nil {
			shieldRun.cleanup(scheduleErr)
			return nil, fmt.Errorf("transitionShieldSchedule: %w", scheduleErr)
		}
	}
	if transition.turtleObjectID != 0 {
		turtlePackets, err := r.startNomadSpecialThreeTurtle(
			packet, sessionKey, generation, transition.turtleObjectID, timestamp,
		)
		if err != nil {
			return nil, fmt.Errorf("transitionTurtle: %w", err)
		}
		packets = append(packets, turtlePackets...)
	}
	firstActionPackets, err := r.npc.scheduleFirstActions(
		packet, sessionKey, generation, transition.immediatePlans, timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("transitionImmediateAction: %w", err)
	}
	packets = append(packets, firstActionPackets...)
	if transition.catalystUnlock != nil {
		catalystUnlock := transition.catalystUnlock
		producers := make(
			[]raknet.ScheduledPacketProducer, 0, len(zoneunlock.CatalystDeadlines()),
		)
		for _, deadline := range zoneunlock.CatalystDeadlines() {
			step := campaignCatalystUnlockStep{
				runtime: r, sessionKey: sessionKey, generation: generation,
				run: catalystUnlock, source: transition.catalystSource,
				deadline: deadline,
			}
			producers = append(producers, raknet.ScheduledPacketProducer{
				Delay: deadline, Produce: step.produce,
			})
		}
		producers = r.registry.producerGuard.scheduledProducers(sessionKey, producers)
		scheduleFailure := campaignCatalystUnlockFailure{
			runtime: r, sessionKey: sessionKey,
			generation: generation, run: catalystUnlock,
		}
		var cancel raknet.CancelSchedule
		var scheduleErr error
		if packet.ScheduleGroupResult != nil {
			cancel, scheduleErr = packet.ScheduleGroupResult(
				producers, scheduleFailure.handle,
			)
		} else if packet.ScheduleGroup != nil {
			cancel, scheduleErr = packet.ScheduleGroup(producers)
		} else {
			scheduleErr = errors.New("scheduler unavailable")
		}
		if scheduleErr == nil && cancel == nil {
			scheduleErr = errors.New("nil cancellation")
		}
		if scheduleErr == nil {
			catalystUnlock.SetCancel(cancel)
		} else {
			fallbackStep := campaignCatalystUnlockStep{
				runtime: r, sessionKey: sessionKey, generation: generation,
				run: catalystUnlock, source: transition.catalystSource,
				deadline: zoneunlock.CatalystMutationDeadline,
			}
			fallbackPackets, fallbackErr := fallbackStep.produce()
			if fallbackErr != nil {
				scheduleFailure.handle(scheduleErr)
				return nil, fmt.Errorf("transitionCatalystFallback: %w", fallbackErr)
			}
			packets = append(packets, fallbackPackets...)
			scheduleFailure.handle(scheduleErr)
			r.logger.Printf("RakNet campaign catalyst unlock published immediately after schedule failure: %v",
				scheduleErr)
		}
	}
	if transition.overdriveUnlock != nil {
		overdriveUnlock := transition.overdriveUnlock
		producers := make(
			[]raknet.ScheduledPacketProducer, 0, len(zoneunlock.OverdriveDeadlines()),
		)
		for _, deadline := range zoneunlock.OverdriveDeadlines() {
			step := campaignOverdriveUnlockStep{
				runtime: r, sessionKey: sessionKey, generation: generation,
				run: overdriveUnlock, deadline: deadline,
			}
			producers = append(producers, raknet.ScheduledPacketProducer{
				Delay: deadline, Produce: step.produce,
			})
		}
		producers = r.registry.producerGuard.scheduledProducers(sessionKey, producers)
		scheduleFailure := campaignOverdriveUnlockFailure{
			runtime: r, sessionKey: sessionKey,
			generation: generation, run: overdriveUnlock,
		}
		var cancel raknet.CancelSchedule
		var scheduleErr error
		if packet.ScheduleGroupResult != nil {
			cancel, scheduleErr = packet.ScheduleGroupResult(
				producers, scheduleFailure.handle,
			)
		} else if packet.ScheduleGroup != nil {
			cancel, scheduleErr = packet.ScheduleGroup(producers)
		} else {
			scheduleErr = errors.New("scheduler unavailable")
		}
		if scheduleErr == nil && cancel == nil {
			scheduleErr = errors.New("nil cancellation")
		}
		if scheduleErr == nil {
			overdriveUnlock.SetCancel(cancel)
		} else {
			fallbackStep := campaignOverdriveUnlockStep{
				runtime: r, sessionKey: sessionKey, generation: generation,
				run:      overdriveUnlock,
				deadline: zoneunlock.OverdriveMutationDeadline,
			}
			fallbackPackets, fallbackErr := fallbackStep.produce()
			if fallbackErr != nil {
				scheduleFailure.handle(scheduleErr)
				return nil, fmt.Errorf("transitionOverdriveFallback: %w", fallbackErr)
			}
			packets = append(packets, fallbackPackets...)
			scheduleFailure.handle(scheduleErr)
			r.logger.Printf("RakNet campaign overdrive unlock published immediately after schedule failure: %v",
				scheduleErr)
		}
	}
	if len(transition.bossPlans) != 0 {
		delay := zoneboss.InitialSecondWaveDelay
		step := campaignBossFollowupStep{
			runtime: r, packet: packet, sessionKey: sessionKey,
			generation: generation, plans: transition.bossPlans,
			outputPackets: transition.bossPackets, timestamp: timestamp,
			delay: delay,
		}
		producer := raknet.ScheduledPacketProducer{
			Delay:   delay,
			Produce: step.produce,
		}
		cancel, scheduleErr := scheduleNPCProducers(r.registry, packet, []raknet.ScheduledPacketProducer{producer})
		if scheduleErr == nil && cancel == nil {
			scheduleErr = errors.New("nil cancellation")
		}
		if scheduleErr != nil {
			fallbackPackets, err := producer.Produce()
			if err != nil {
				return nil, fmt.Errorf("transitionBossFallback: %w", err)
			}
			packets = append(packets, fallbackPackets...)
			r.logger.Printf("RakNet campaign boss follow-up published immediately after schedule failure: %v",
				scheduleErr)
		}
	}
	if len(transition.hordePlans) == 0 {
		return packets, nil
	}
	delay := transition.hordeTransition.NextWaveDelay
	step := campaignHordeFollowupStep{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation,
		markerSet:  transition.hordeTransition.MarkerSetName,
		plans:      transition.hordePlans, outputPackets: transition.hordePackets,
		timestamp: timestamp, delay: delay,
	}
	producer := raknet.ScheduledPacketProducer{
		Delay:   delay,
		Produce: step.produce,
	}
	cancel, scheduleErr := scheduleNPCProducers(r.registry, packet, []raknet.ScheduledPacketProducer{producer})
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr == nil {
		return packets, nil
	}
	fallbackPackets, err := producer.Produce()
	if err != nil {
		return nil, fmt.Errorf("transitionHordeFallback: %w", err)
	}
	r.logger.Printf("RakNet campaign horde follow-up published immediately after schedule failure: %v", scheduleErr)
	return append(packets, fallbackPackets...), nil
}

const (
	campaignInvincitronEffectDelay = 330000013 * time.Nanosecond
	campaignInvincitronLoopDelay   = 600000023 * time.Nanosecond
	campaignInvincitronEndDuration = 633333027 * time.Nanosecond
)

type campaignShieldTransitionRun struct {
	runtime          campaignDamageRuntime
	sessionKey       string
	generation       uint64
	objectID         uint32
	timestamp        uint64
	duration         time.Duration
	mutex            sync.Mutex
	effectSlot       uint8
	isEffectAttached bool
}

func (r *campaignShieldTransitionRun) isCurrent() bool {
	r.runtime.registry.mutex.RLock()
	peerSession, isFound := r.runtime.registry.sessions[r.sessionKey]
	if !isFound || peerSession.generation != r.generation ||
		peerSession.zone.NPCs() == nil {
		r.runtime.registry.mutex.RUnlock()
		return false
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(r.objectID)
	r.runtime.registry.mutex.RUnlock()
	return isEnemyFound && !enemy.IsDefeated && enemy.IsShieldActive
}

func (r *campaignShieldTransitionRun) attachEffect() ([][]byte, error) {
	if !r.isCurrent() {
		return nil, nil
	}
	r.mutex.Lock()
	defer r.mutex.Unlock()
	slot, isAllocated := r.runtime.effectPool.Allocate(r.objectID)
	if !isAllocated {
		return nil, nil
	}
	packet, err := npcraknet.ShieldEffect(r.objectID, slot, false)
	if err != nil {
		r.runtime.effectPool.Release(r.objectID, slot)
		return nil, fmt.Errorf("transitionShieldEffect: %w", err)
	}
	r.effectSlot = slot
	r.isEffectAttached = true
	return [][]byte{packet}, nil
}

func (r *campaignShieldTransitionRun) loop() ([][]byte, error) {
	if !r.isCurrent() {
		return nil, nil
	}
	packet, err := npcraknet.ShieldAnimation(
		r.objectID, "nomad_lieu_tc_3_shield_loop",
		r.timestamp+uint64(campaignInvincitronLoopDelay/time.Millisecond),
	)
	if err != nil {
		return nil, fmt.Errorf("transitionShieldLoop: %w", err)
	}
	return [][]byte{packet}, nil
}

func (r *campaignShieldTransitionRun) end() ([][]byte, error) {
	isCurrent := r.isCurrent()
	r.mutex.Lock()
	defer r.mutex.Unlock()
	packet := make([][]byte, 0, 2)
	if r.isEffectAttached &&
		r.runtime.effectPool.Release(r.objectID, r.effectSlot) {
		r.isEffectAttached = false
		removePacket, err := npcraknet.ShieldEffect(
			r.objectID, r.effectSlot, true,
		)
		if err != nil {
			return nil, fmt.Errorf("transitionShieldRemove: %w", err)
		}
		packet = append(packet, removePacket)
	}
	if !isCurrent {
		return packet, nil
	}
	endPacket, err := npcraknet.ShieldAnimation(
		r.objectID, "nomad_lieu_tc_3_shield_end",
		r.timestamp+uint64(
			(r.duration-
				campaignInvincitronEndDuration)/time.Millisecond,
		),
	)
	if err != nil {
		return nil, fmt.Errorf("transitionShieldEnd: %w", err)
	}
	return append(packet, endPacket), nil
}

func (r *campaignShieldTransitionRun) finish() ([][]byte, error) {
	if !r.isCurrent() {
		return nil, nil
	}
	r.runtime.registry.mutex.Lock()
	peerSession, isFound := r.runtime.registry.sessions[r.sessionKey]
	isCurrent := isFound && peerSession.generation == r.generation &&
		peerSession.zone.NPCs() != nil
	var err error
	if isCurrent {
		err = peerSession.zone.NPCs().EndShield(r.objectID)
		if err == nil {
			r.runtime.registry.sessions[r.sessionKey] = peerSession
		}
	}
	r.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("transitionShieldFinish: %w", err)
	}
	packet, err := abilityraknet.AnimationReset(
		r.objectID, r.timestamp+
			uint64(r.duration/time.Millisecond),
	)
	if err != nil {
		return nil, fmt.Errorf("transitionShieldFinishMarshal: %w", err)
	}
	return [][]byte{packet}, nil
}

func (r *campaignShieldTransitionRun) cleanup(_ error) {
	r.mutex.Lock()
	if r.isEffectAttached {
		r.runtime.effectPool.Release(r.objectID, r.effectSlot)
		r.isEffectAttached = false
	}
	r.mutex.Unlock()
	r.runtime.registry.mutex.Lock()
	peerSession, isFound := r.runtime.registry.sessions[r.sessionKey]
	if isFound && peerSession.generation == r.generation &&
		peerSession.zone.NPCs() != nil {
		_ = peerSession.zone.NPCs().EndShield(r.objectID)
		r.runtime.registry.sessions[r.sessionKey] = peerSession
	}
	r.runtime.registry.mutex.Unlock()
}

func (r *campaignShieldTransitionRun) producers() []raknet.ScheduledPacketProducer {
	return []raknet.ScheduledPacketProducer{
		{
			Delay:   campaignInvincitronEffectDelay,
			Produce: r.attachEffect,
		},
		{
			Delay:   campaignInvincitronLoopDelay,
			Produce: r.loop,
		},
		{
			Delay: r.duration -
				campaignInvincitronEndDuration,
			Produce: r.end,
		},
		{
			Delay:   r.duration,
			Produce: r.finish,
		},
	}
}

type campaignDamageTransition struct {
	immediatePlans                  []zonenpc.SpawnPlan
	immediatePackets                [][]byte
	nashiraSplit                    *campaignNashiraSplitPlan
	selfResurrectionPlans           []zonenpc.SpawnPlan
	corruptorStageTwoPlans          []zonenpc.SpawnPlan
	corruptorStageTwoTargetObjectID uint32
	corruptorPhaseObjectID          uint32
	corruptorPortalRespawn          *campaignCorruptorPortalRespawnPlan
	defeatedObjectID                uint32
	fearCleanupObjectID             uint32
	healingCleanupObjectID          uint32
	sinkholeCleanupObjectID         uint32
	experience                      uint32
	hordePlans                      []zonenpc.SpawnPlan
	hordePackets                    [][]byte
	hordeTransition                 zonehorde.Transition
	isTutorialHordeNextWave         bool
	isTutorialHordeComplete         bool
	bossPlans                       []zonenpc.SpawnPlan
	bossPackets                     [][]byte
	bossTransition                  zoneboss.Transition
	bossCompletionPacket            []byte
	bossCompletionDelay             time.Duration
	bossStartError                  error
	catalystUnlock                  *unlockraknet.CatalystRun
	catalystSource                  sim.Position
	overdriveUnlock                 *unlockraknet.Run
	shieldObjectID                  uint32
	shieldDuration                  time.Duration
	turtleObjectID                  uint32
}

const (
	campaignSelfResurrectionAnimation  = "gen_death_resurrected"
	campaignSelfResurrectionDuration   = time.Duration(62) * time.Second / 30
	campaignCorruptorStageTwoAnimation = "sca_boss_blink_in"
	campaignCorruptorStageTwoNounName  = "ScaldronBoss_Stage2.Noun"
	campaignCorruptorStageTwoDuration  = 11200 * time.Millisecond
)

type campaignBossCompletionStep struct {
	runtime    campaignDamageRuntime
	sessionKey string
	generation uint64
	packet     []byte
}

func (e campaignBossCompletionStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return nil, nil
	}
	return [][]byte{e.packet}, nil
}

type campaignSelfResurrectionActionStep struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	plans      []zonenpc.SpawnPlan
	timestamp  uint64
}

func (e campaignSelfResurrectionActionStep) produce() ([][]byte, error) {
	packets, err := e.runtime.scheduleFirstActions(
		e.packet, e.sessionKey, e.generation, e.plans, e.timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("selfResurrectAction: %w", err)
	}
	return packets, nil
}

// applyCampaignDamageTransition keeps non-melee damage on the same retained
// encounter path as melee basics. Horde follow-up admission remains reserved
// for the caller's retained inter-wave scheduler.
func (s *gameplayPeerSession) applyCampaignDamageTransition(
	result zonenpc.DamageResult,
) (campaignDamageTransition, error) {
	return s.applyCampaignDamageTransitionWithKill(result, true)
}

func (s *gameplayPeerSession) applyCampaignNPCSelfDamageTransition(
	result zonenpc.DamageResult,
) (campaignDamageTransition, error) {
	return s.applyCampaignDamageTransitionWithKill(result, false)
}

func (s *gameplayPeerSession) applyCampaignDamageTransitionWithKill(
	result zonenpc.DamageResult, isPlayerKill bool,
) (campaignDamageTransition, error) {
	if s == nil || s.zone.NPCs() == nil {
		return campaignDamageTransition{}, errors.New("campaign transition unavailable")
	}
	transition := campaignDamageTransition{}
	if s.requestCampaignCorruptorPhase(result) {
		transition.corruptorPhaseObjectID = result.ObjectID
	}
	transition.corruptorPortalRespawn =
		s.requestCampaignCorruptorPortalRespawn(result)
	nashiraSplit, nashiraPackets, err := s.planCampaignNashiraSplit(result)
	if err != nil {
		return campaignDamageTransition{}, fmt.Errorf("nashiraSplit: %w", err)
	}
	transition.nashiraSplit = nashiraSplit
	transition.immediatePackets = append(
		transition.immediatePackets, nashiraPackets...,
	)
	merakPlans, merakPackets, err := s.planCampaignMerakAdds(result)
	if err != nil {
		return campaignDamageTransition{}, fmt.Errorf("merakAdds: %w", err)
	}
	transition.immediatePlans = append(transition.immediatePlans, merakPlans...)
	transition.immediatePackets = append(
		transition.immediatePackets, merakPackets...,
	)
	if isPlayerKill && !result.IsDamageImmune && result.Damage > 0 {
		lifeStealPackets, healedAmount, err := s.applyEquippedLifeSteal(result.Damage)
		if err != nil {
			return campaignDamageTransition{}, fmt.Errorf("lifeSteal: %w", err)
		}
		transition.immediatePackets = append(
			transition.immediatePackets, lifeStealPackets...,
		)
		if healedAmount > 0 {
			s.queueStatDelta(sporenet.PlayerStatDelta{
				PVEHealing:         float64(healedAmount),
				PVEHealingReceived: float64(healedAmount),
			})
		}
	}
	if len(result.DeletedOwnedObjectIDs) != 0 {
		deletePacket, marshalErr := raknet.MarshalApplication(
			raknet.ObjectDeleteMessage{ObjectID: result.DeletedOwnedObjectIDs},
		)
		if marshalErr != nil {
			return campaignDamageTransition{},
				fmt.Errorf("ownedDeleteMarshal: %w", marshalErr)
		}
		transition.immediatePackets = append(
			transition.immediatePackets, deletePacket,
		)
		deaths := make([]zonenpc.DeathEvent, 0, len(result.DeletedOwnedObjectIDs))
		for _, objectID := range result.DeletedOwnedObjectIDs {
			deaths = append(deaths, zonenpc.DeathEvent{
				Kind: zonenpc.DeathDelete, TargetObjectID: objectID,
			})
		}
		s.zone.PublishNPCDeath(deaths, s.binding.UserID, s.generation)
	}
	for index, healing := range result.PassiveHeals {
		healPackets, marshalErr := npcraknet.HealDelta(
			healing.SourceObjectID, healing.Target, healing.Amount,
		)
		if marshalErr != nil {
			return campaignDamageTransition{}, fmt.Errorf(
				"passiveHealMarshal[%d]: %w", index, marshalErr,
			)
		}
		transition.immediatePackets = append(
			transition.immediatePackets, healPackets...,
		)
	}
	for index, enrage := range result.PassiveEnrages {
		effectPacket, marshalErr := raknet.MarshalApplication(
			raknet.ServerEventMessage{
				Asset:    util.HashID("status_enraged.ServerEventDef"),
				ObjectID: enrage.Target.Plan.ObjectID,
			},
		)
		if marshalErr != nil {
			return campaignDamageTransition{}, fmt.Errorf(
				"passiveEnrageEffect[%d]: %w", index, marshalErr,
			)
		}
		scalePacket, marshalErr := raknet.MarshalApplication(
			raknet.AttributeDataUpdateMessage{
				ObjectID: enrage.Target.Plan.ObjectID,
				Value:    map[uint8]float32{113: enrage.BodyScale},
			},
		)
		if marshalErr != nil {
			return campaignDamageTransition{}, fmt.Errorf(
				"passiveEnrageScale[%d]: %w", index, marshalErr,
			)
		}
		transition.immediatePackets = append(
			transition.immediatePackets, effectPacket, scalePacket,
		)
	}
	if result.IsAbsorptionShieldRenewed {
		generationPacket, marshalErr := raknet.MarshalApplication(
			raknet.ObjectEffectMessage{
				Asset:    util.HashID("cyber_shield_generate_shield.ServerEventDef"),
				ObjectID: result.ObjectID,
			},
		)
		if marshalErr != nil {
			return campaignDamageTransition{},
				fmt.Errorf("absorptionShieldGenerate: %w", marshalErr)
		}
		shieldPacket, marshalErr := npcraknet.ShieldEffectAsset(
			result.ObjectID, 15,
			"citadelBasicShield_Shield.ServerEventDef", false,
		)
		if marshalErr != nil {
			return campaignDamageTransition{},
				fmt.Errorf("absorptionShieldAttach: %w", marshalErr)
		}
		transition.immediatePackets = append(
			transition.immediatePackets, generationPacket, shieldPacket,
		)
	}
	if result.IsAbsorptionShieldHit && !result.IsAbsorptionShieldBroken {
		hitPacket, marshalErr := raknet.MarshalApplication(
			raknet.ObjectEffectMessage{
				Asset:    util.HashID("status_shield_hit.ServerEventDef"),
				ObjectID: result.ObjectID,
			},
		)
		if marshalErr != nil {
			return campaignDamageTransition{},
				fmt.Errorf("absorptionShieldHit: %w", marshalErr)
		}
		transition.immediatePackets = append(
			transition.immediatePackets, hitPacket,
		)
	}
	if result.IsAbsorptionShieldBroken {
		removePacket, marshalErr := npcraknet.ShieldEffectAsset(
			result.ObjectID, 15,
			"citadelBasicShield_Shield.ServerEventDef", true,
		)
		if marshalErr != nil {
			return campaignDamageTransition{},
				fmt.Errorf("absorptionShieldRemove: %w", marshalErr)
		}
		transition.immediatePackets = append(
			transition.immediatePackets, removePacket,
		)
	}
	if result.IsDefeated || result.IsCorruptorStageTwoStarted {
		transition.sinkholeCleanupObjectID = result.ObjectID
		chargeupState := s.campaignNPCChargeups[result.ObjectID]
		if chargeupState.isCharged {
			removePacket, marshalErr := npcraknet.ChargeupEffect(
				result.ObjectID, "zelem_chargeup_charge.ServerEventDef", true,
			)
			if marshalErr != nil {
				return campaignDamageTransition{},
					fmt.Errorf("chargeupDefeatRemove: %w", marshalErr)
			}
			delete(s.campaignNPCChargeups, result.ObjectID)
			transition.immediatePackets = append(
				transition.immediatePackets, removePacket,
			)
		}
		if result.IsDefeated {
			transition.defeatedObjectID = result.ObjectID
		}
	}
	if result.IsDefeated || result.IsSelfResurrectionStarted ||
		result.IsCorruptorStageTwoStarted {
		transition.fearCleanupObjectID = result.ObjectID
	}
	if (result.IsDefeated && !result.IsSelfResurrectionStarted) ||
		result.IsCorruptorStageTwoStarted {
		transition.healingCleanupObjectID = result.ObjectID
	}
	if result.IsShieldStarted {
		transition.shieldObjectID = result.ObjectID
		shieldNPC, isShieldNPCFound := s.zone.NPCs().NPC(result.ObjectID)
		if isShieldNPCFound {
			transition.shieldDuration, _ = zonenpc.NomadWithDroneShieldDuration(
				shieldNPC.Plan.NounName,
			)
		}
	}
	if result.IsTurtleStarted {
		transition.turtleObjectID = result.ObjectID
	}
	if result.IsSelfResurrectionStarted {
		passiveEffectRemove, marshalErr := raknet.MarshalApplication(
			raknet.AttachedEffectMessage{
				Slot: 16, IsRemovalRequested: true, ObjectID: result.ObjectID,
			},
		)
		if marshalErr != nil {
			return campaignDamageTransition{},
				fmt.Errorf("selfResurrectPassiveRemove: %w", marshalErr)
		}
		revived, resurrectErr := s.zone.NPCs().Resurrect(result.ObjectID, 0.4)
		if resurrectErr != nil {
			return campaignDamageTransition{},
				fmt.Errorf("selfResurrectApply: %w", resurrectErr)
		}
		var isTargetAcquired bool
		revived, isTargetAcquired, resurrectErr = s.zone.NPCs().AcquireTarget(
			revived.Plan.ObjectID, s.deployedObjectID,
		)
		if resurrectErr != nil {
			return campaignDamageTransition{},
				fmt.Errorf("selfResurrectTarget: %w", resurrectErr)
		}
		if !isTargetAcquired {
			return campaignDamageTransition{},
				errors.New("self resurrection target not acquired")
		}
		profile := zonenpc.ActionProfile{
			ImpactEffectName: "self_rez_aura_effect.ServerEventDef",
		}
		resurrectionPackets, marshalErr := npcraknet.ResurrectionHit(
			revived.Plan.ObjectID, revived, profile,
		)
		if marshalErr != nil {
			return campaignDamageTransition{},
				fmt.Errorf("selfResurrectMarshal: %w", marshalErr)
		}
		transition.immediatePackets = append(
			transition.immediatePackets, passiveEffectRemove,
		)
		transition.immediatePackets = append(
			transition.immediatePackets, resurrectionPackets...,
		)
		transition.selfResurrectionPlans = append(
			transition.selfResurrectionPlans, revived.Plan,
		)
	}
	if result.IsCorruptorStageTwoStarted {
		s.prepareCampaignCorruptorStageTwo(result.ObjectID)
		revived, resurrectErr := s.zone.NPCs().Resurrect(result.ObjectID, 1)
		if resurrectErr != nil {
			return campaignDamageTransition{},
				fmt.Errorf("corruptorStageTwoApply: %w", resurrectErr)
		}
		var isTargetAcquired bool
		revived, isTargetAcquired, resurrectErr = s.zone.NPCs().AcquireTarget(
			revived.Plan.ObjectID, s.deployedObjectID,
		)
		if resurrectErr != nil {
			return campaignDamageTransition{},
				fmt.Errorf("corruptorStageTwoTarget: %w", resurrectErr)
		}
		if !isTargetAcquired {
			return campaignDamageTransition{},
				errors.New("corruptor stage two target not acquired")
		}
		revived, resurrectErr = s.zone.NPCs().SetNounName(
			revived.Plan.ObjectID, campaignCorruptorStageTwoNounName,
		)
		if resurrectErr != nil {
			return campaignDamageTransition{},
				fmt.Errorf("corruptorStageTwoNoun: %w", resurrectErr)
		}
		profile := zonenpc.ActionProfile{
			ImpactEffectName: "scaldronboss_transition_shader_effect.ServerEventDef",
		}
		resurrectionPackets, marshalErr := npcraknet.ResurrectionHit(
			revived.Plan.ObjectID, revived, profile,
		)
		if marshalErr != nil {
			return campaignDamageTransition{},
				fmt.Errorf("corruptorStageTwoMarshal: %w", marshalErr)
		}
		transition.immediatePackets = append(
			transition.immediatePackets, resurrectionPackets...,
		)
		transition.corruptorStageTwoPlans = append(
			transition.corruptorStageTwoPlans, revived.Plan,
		)
		transition.corruptorStageTwoTargetObjectID = revived.TargetObjectID
	}
	if result.IsAreaShiftStarted {
		shiftPacket, marshalErr := raknet.MarshalApplication(
			raknet.ObjectEffectMessage{
				Asset:    util.HashID("shifted_in_and_out_effect.ServerEventDef"),
				ObjectID: result.ObjectID,
			},
		)
		if marshalErr != nil {
			return campaignDamageTransition{},
				fmt.Errorf("areaShiftEffect: %w", marshalErr)
		}
		transition.immediatePackets = append(
			transition.immediatePackets, shiftPacket,
		)
	}
	if result.IsSpawnStealthRevealed {
		revealedNPC, isRevealedNPCFound := s.zone.NPCs().NPC(result.ObjectID)
		if !isRevealedNPCFound {
			return campaignDamageTransition{}, errors.New("stealth reveal npc unavailable")
		}
		profile, isProfileFound := zonenpc.ActionProfileForPlan(revealedNPC.Plan)
		if !isProfileFound || profile.RevealEffectName == "" {
			return campaignDamageTransition{}, errors.New("stealth reveal profile unavailable")
		}
		revealPackets, revealErr := npcraknet.StealthReveal(
			result.ObjectID, revealedNPC.Plan.Position, profile.RevealEffectName,
		)
		if revealErr != nil {
			return campaignDamageTransition{}, fmt.Errorf("stealthReveal: %w", revealErr)
		}
		transition.immediatePackets = append(
			transition.immediatePackets, revealPackets...,
		)
	}
	encounterTransition, err := s.zone.ObserveNPCDamage(result)
	if err != nil {
		return campaignDamageTransition{}, fmt.Errorf("encounterDamage: %w", err)
	}
	if result.IsDefeated && s.binding.Mode == game.ModeTutorial &&
		s.tutorialHorde != nil {
		transition.isTutorialHordeNextWave,
			transition.isTutorialHordeComplete =
			s.tutorialHorde.observeDefeat(result.ObjectID)
	}
	defeatedNPC, isDefeatedNPCFound := s.zone.NPCs().NPC(result.ObjectID)
	if isDefeatedNPCFound && corruptorRank(defeatedNPC.Plan.NounName) != 0 &&
		(result.IsDefeated || result.IsCorruptorStageTwoStarted) {
		phaseEffectRemove, marshalErr := raknet.MarshalApplication(
			raknet.AttachedEffectMessage{
				Slot: corruptorPhaseEffectSlot, ObjectID: result.ObjectID,
				IsRemovalRequested: true, IsHardStop: true,
			},
		)
		if marshalErr != nil {
			return campaignDamageTransition{},
				fmt.Errorf("corruptorPhaseRemove: %w", marshalErr)
		}
		transition.immediatePackets = append(
			transition.immediatePackets, phaseEffectRemove,
		)
	}
	if result.IsDefeated && !result.IsSelfResurrectionStarted &&
		isDefeatedNPCFound {
		profile, isProfileFound := zonenpc.ActionProfileForPlan(defeatedNPC.Plan)
		if isProfileFound && profile.PassiveEffectName != "" {
			passiveEffectRemove, marshalErr := raknet.MarshalApplication(
				raknet.AttachedEffectMessage{
					Slot: 16, ObjectID: result.ObjectID,
					IsRemovalRequested: true, IsHardStop: true,
				},
			)
			if marshalErr != nil {
				return campaignDamageTransition{},
					fmt.Errorf("passiveDefeatRemove: %w", marshalErr)
			}
			transition.immediatePackets = append(
				transition.immediatePackets, passiveEffectRemove,
			)
			s.zone.PublishNPCDeath([]zonenpc.DeathEvent{{
				Kind: zonenpc.DeathEffect, TargetObjectID: result.ObjectID,
				EffectSlot: 16, IsEffectStopped: true,
			}}, s.binding.UserID, s.generation)
		}
	}
	if isPlayerKill && result.IsDefeated && isDefeatedNPCFound &&
		!defeatedNPC.Plan.IsFixture {
		transition.experience = defeatedNPC.Plan.Experience
		_, _ = s.applyPassiveKill()
	}
	if result.IsDefeated && s.zone.Security() != nil {
		decision, activationErr := s.zone.Security().
			ReserveClearedPresentation(s.zone.SecurityThreats())
		if activationErr != nil {
			return campaignDamageTransition{},
				fmt.Errorf("securityReserve: %w", activationErr)
		}
		if decision.IsActivation {
			activationPackets, marshalErr := securityraknet.State(
				decision.ObjectID, decision.Teleport, true, true,
			)
			if marshalErr != nil {
				s.zone.Security().CancelPresentation(decision.RouteIndex)
				return campaignDamageTransition{},
					fmt.Errorf("securityMarshal: %w", marshalErr)
			}
			commitErr := s.zone.Security().CommitPresentation(
				decision.RouteIndex,
			)
			if commitErr != nil {
				return campaignDamageTransition{},
					fmt.Errorf("securityCommit: %w", commitErr)
			}
			transition.immediatePackets = append(
				transition.immediatePackets, activationPackets...,
			)
		}
	}
	if result.IsDefeated {
		teleporterPackets, teleporterErr := s.syncCampaignTeleporterStates()
		if teleporterErr != nil {
			return campaignDamageTransition{},
				fmt.Errorf("campaignTeleporterState: %w", teleporterErr)
		}
		transition.immediatePackets = append(
			transition.immediatePackets, teleporterPackets...,
		)
	}
	if result.IsDefeated {
		activationPackets, activationErr := s.activateTutorialTeleporter()
		if activationErr != nil {
			return campaignDamageTransition{},
				fmt.Errorf("tutorialTeleporterActivate: %w", activationErr)
		}
		transition.immediatePackets = append(
			transition.immediatePackets, activationPackets...,
		)
	}
	if result.IsDefeated && result.RemainingMarkerSetCount == 0 {
		thornObjectIDs := s.zone.DirectorDefinition().MarkerObjectIDs(
			result.MarkerSetName, nocturnaThornNounName,
		)
		if len(thornObjectIDs) != 0 {
			thornDeletePacket, thornDeleteErr := objectraknet.Delete(thornObjectIDs)
			if thornDeleteErr != nil {
				return campaignDamageTransition{},
					fmt.Errorf("campaignThornDelete: %w", thornDeleteErr)
			}
			transition.immediatePackets = append(
				transition.immediatePackets, thornDeletePacket,
			)
		}
	}
	hordeTransition := encounterTransition.Horde
	if result.IsDefeated && s.zone.Horde() != nil {
		transition.hordeTransition = hordeTransition
		if hordeTransition.IsComplete {
			barrierPlans := s.zone.HordeBarrierPlans(hordeTransition.MarkerSetName)
			if len(barrierPlans) != 0 {
				packet, err := barrierraknet.Delete(barrierPlans)
				if err != nil {
					return campaignDamageTransition{}, fmt.Errorf("hordeBarrierComplete: %w", err)
				}
				transition.immediatePackets = append(transition.immediatePackets, packet)
			}
		}
		if hordeTransition.NextWaveActorCount != 0 {
			plans, packets, err := s.planCampaignHordeFollowup(hordeTransition)
			if err != nil {
				return campaignDamageTransition{}, fmt.Errorf("hordeFollowup: %w", err)
			}
			transition.hordePlans = plans
			transition.hordePackets = packets
		}
		if hordeTransition.IsComplete {
			plans, packets, isStarted, err := s.admitCampaignGenericBossAfterHorde(
				hordeTransition.Completion,
			)
			if err != nil {
				transition.bossStartError = fmt.Errorf("hordeBossStart: %w", err)
			} else if isStarted {
				transition.immediatePlans = append(transition.immediatePlans, plans...)
				transition.immediatePackets = append(transition.immediatePackets, packets...)
				transition.catalystUnlock = s.campaignUnlockPresentationSession().
					Catalyst()
				transition.overdriveUnlock = s.campaignUnlockPresentationSession().
					Overdrive()
				transition.catalystSource = sim.Position{
					X: plans[0].Position.X, Y: plans[0].Position.Y, Z: plans[0].Position.Z,
				}
			}
		}
	}
	bossTransition := encounterTransition.Boss
	transition.bossTransition = bossTransition
	if bossTransition.NextWaveActorCount != 0 {
		plans, packets, err := s.planCampaignBossFollowup(
			bossTransition.NextWaveActorCount,
		)
		if err != nil {
			return campaignDamageTransition{}, fmt.Errorf("bossFollowup: %w", err)
		}
		transition.bossPlans = plans
		transition.bossPackets = packets
		warningPacket, err := bossraknet.AddPhase()
		if err != nil {
			return campaignDamageTransition{}, fmt.Errorf("bossWarning: %w", err)
		}
		transition.immediatePackets = append(transition.immediatePackets, warningPacket)
	}
	if bossTransition.IsComplete {
		packet, err := bossraknet.Complete(bossTransition.LeaderObjectID)
		if err != nil {
			return campaignDamageTransition{}, fmt.Errorf("bossComplete: %w", err)
		}
		deathPresentation, isDeathPresentationFound :=
			zonenpc.DeathPresentationForNoun(defeatedNPC.Plan.NounName)
		isDestructorDefeated := isDefeatedNPCFound &&
			zoneboss.IsFinalBossNoun(defeatedNPC.Plan.NounName) &&
			isDeathPresentationFound && deathPresentation.PresentationDuration > 0
		if isDestructorDefeated {
			transition.bossCompletionPacket = packet
			transition.bossCompletionDelay = deathPresentation.PresentationDuration
		} else {
			transition.immediatePackets = append(transition.immediatePackets, packet)
		}
	}
	isOrdinarySpawnGroup := result.IsLocusCleared &&
		isDefeatedNPCFound && !defeatedNPC.Plan.IsBoss &&
		!strings.Contains(strings.ToLower(result.MarkerSetName), "_ai_horde_")
	if isOrdinarySpawnGroup {
		s.zone.SaveSpawnGroupCheckpoint(result.LocusID)
	}
	return transition, nil
}

func (s *gameplayPeerSession) applyEquippedLifeSteal(
	damage float32,
) ([][]byte, float32, error) {
	if s == nil || damage <= 0 ||
		s.deployedCreatureIndex >= uint32(len(s.binding.Creatures)) {
		return nil, 0, nil
	}
	creature := s.binding.Creatures[s.deployedCreatureIndex]
	if creature.LifeSteal <= 0 {
		return nil, 0, nil
	}
	healing, err := game.ApplyTargetHealingReduction(
		damage*creature.LifeSteal, creature.HealingTargetProfile,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("itemLifeStealReduction: %w", err)
	}
	maximumHitPoint, _, err := s.deployedResourceMaximum()
	if err != nil {
		return nil, 0, fmt.Errorf("itemLifeStealMaximum: %w", err)
	}
	previousHitPoint := s.deployedHitPoint()
	hitPoint := min(maximumHitPoint, previousHitPoint+healing)
	healedAmount := hitPoint - previousHitPoint
	if healedAmount <= 0 {
		return nil, 0, nil
	}
	_, err = s.setDeployedHitPoints(hitPoint)
	if err != nil {
		return nil, 0, fmt.Errorf("itemLifeStealCommit: %w", err)
	}
	packets, err := abilityraknet.ChannelDrainHealing(
		s.deployedObjectID, hitPoint, healedAmount,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("itemLifeStealMarshal: %w", err)
	}
	resourcePacket, err := s.marshalCampaignCharacterResource(
		s.deployedCreatureIndex,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("itemLifeStealResource: %w", err)
	}
	return append(packets, resourcePacket), healedAmount, nil
}

func (s *gameplayPeerSession) planCampaignHordeFollowup(
	transition zonehorde.Transition,
) ([]zonenpc.SpawnPlan, [][]byte, error) {
	plans, err := s.zone.PlanHordeFollowup(
		transition, s.deployedObjectID, s.binding.GameID,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("hordePlan: %w", err)
	}
	packets, err := npcraknet.TargetedSpawns(plans, s.deployedObjectID)
	if err != nil {
		return nil, nil, fmt.Errorf("hordeMarshal: %w", err)
	}
	return plans, packets, nil
}

func (s *gameplayPeerSession) admitReservedCampaignHorde(
	markerSetName string, plans []zonenpc.SpawnPlan,
) error {
	if s == nil || len(plans) == 0 || markerSetName == "" {
		return errors.New("horde reservation invalid")
	}
	err := s.zone.AdmitHordeWave(
		s.deployedObjectID, markerSetName, plans,
	)
	if err != nil {
		return fmt.Errorf("hordeAdmit: %w", err)
	}
	return nil
}

func campaignDeathTarget(target zoneNPCDeathDefinition) deathraknet.Target {
	return deathraknet.Target{
		ObjectID: target.objectID,
		Position: sim.Position{
			X: target.position.X, Y: target.position.Y, Z: target.position.Z,
		},
		OrdinaryDeathAnimation: target.ordinaryDeathAnimation,
		IsAnimationSuppressed:  target.isDeathAnimationSuppressed,
		CorpseFadeDelay:        target.corpseFadeDelay,
		GraphicsState:          target.graphicsState,
		ExplosionEffectName:    target.explosionEffectName,
		CreatureType:           target.creatureType, IsCreatureTypeKnown: target.isCreatureTypeKnown,
		IsFixture:   target.isFixture,
		IsBoss:      target.isBoss,
		DeleteDelay: target.deleteDelay,
	}
}

type campaignOrbExpiryProducer struct {
	registry   *gameplaySessionRegistry
	sessionKey string
	generation uint64
	objectID   uint32
}

func (p campaignOrbExpiryProducer) produce() ([][]byte, error) {
	p.registry.mutex.Lock()
	peerSession, isFound := p.registry.sessions[p.sessionKey]
	isCurrent := isFound && peerSession.generation == p.generation
	if !isCurrent {
		p.registry.mutex.Unlock()
		return nil, nil
	}
	expiryPacket, err := peerSession.expireCampaignOrb(p.objectID)
	if err == nil {
		p.registry.sessions[p.sessionKey] = peerSession
	}
	p.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("enemyOrbExpire: %w", err)
	}
	if expiryPacket == nil {
		return nil, nil
	}
	return [][]byte{expiryPacket}, nil
}

func (r campaignNPCActionRuntime) spawnLoot(
	packet raknet.Packet, sessionKey string, generation uint64,
	enemy zonenpc.Snapshot, sourceTime uint64,
) ([][]byte, error) {
	if !enemy.IsDefeated {
		return nil, nil
	}
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		!peerSession.isZoneTerminal()
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	isTutorial := peerSession.binding.Mode == game.ModeTutorial
	equipmentPackets := make([][]byte, 0)
	var equipmentErr error
	if !isTutorial && !enemy.Plan.IsFixture {
		equipmentPackets, _, equipmentErr = peerSession.spawnCampaignNPCEquipment(
			enemy, r.gameplayJoin, sourceTime,
		)
	}
	orbPackets := make([][]byte, 0)
	var orbObjectID uint32
	var orbErr error
	if !isTutorial || peerSession.isTutorialCapsuleDropUnlocked {
		orbPackets, orbObjectID, orbErr = peerSession.spawnCampaignNPCOrb(
			enemy, sourceTime, r.now(),
		)
	}
	crystalPackets := make([][]byte, 0)
	var crystalErr error
	if zoneunlock.AreCatalystDropsUnlocked(peerSession.binding) {
		crystalPackets, _, crystalErr = peerSession.spawnCampaignNPCCrystal(
			enemy, r.program.CrystalDefinitions,
			r.program.CrystalLevelOffsets, sourceTime,
		)
	}
	dnaPackets := make([][]byte, 0)
	var dnaErr error
	if !isTutorial {
		dnaPackets, _, dnaErr = peerSession.spawnCampaignNPCDNA(
			enemy, sourceTime, r.now(),
		)
	}
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	if equipmentErr != nil {
		r.logger.Printf(
			"RakNet campaign enemy loot not generated actor=%d: %v",
			enemy.Plan.ObjectID, equipmentErr,
		)
	}
	if orbErr != nil {
		r.logger.Printf(
			"RakNet campaign enemy orb not generated actor=%d: %v",
			enemy.Plan.ObjectID, orbErr,
		)
	}
	if crystalErr != nil {
		r.logger.Printf(
			"RakNet campaign enemy crystal not generated actor=%d: %v",
			enemy.Plan.ObjectID, crystalErr,
		)
	}
	if dnaErr != nil {
		r.logger.Printf(
			"RakNet campaign enemy DNA not generated actor=%d: %v",
			enemy.Plan.ObjectID, dnaErr,
		)
	}
	if orbObjectID != 0 && packet.ScheduleFunc != nil {
		expiry := campaignOrbExpiryProducer{
			registry: r.registry, sessionKey: sessionKey,
			generation: generation, objectID: orbObjectID,
		}
		err := scheduleNPCProducer(r.registry, packet, campaignOrbLifetime, expiry.produce)
		if err != nil {
			r.logger.Printf(
				"RakNet campaign enemy orb expiry not scheduled object=%d: %v",
				orbObjectID, err,
			)
		}
	}
	packets := append(equipmentPackets, orbPackets...)
	packets = append(packets, crystalPackets...)
	packets = append(packets, dnaPackets...)
	return packets, nil
}

type campaignExperienceMember struct {
	sessionKey string
	generation uint64
	userID     uint64
	gameID     uint32
	playerSlot uint16
	startingXP uint32
	bonus      float32
}

func (r campaignDamageRuntime) publishExperience(
	sessionKey string, generation uint64, objectID uint32, baseExperience uint32,
) ([][]byte, error) {
	if baseExperience == 0 {
		return nil, nil
	}
	r.registry.mutex.RLock()
	source, isSourceFound := r.registry.sessions[sessionKey]
	if !isSourceFound || source.generation != generation || source.zone == nil {
		r.registry.mutex.RUnlock()
		return nil, nil
	}
	members := make(map[uint64]campaignExperienceMember)
	for candidateKey, candidate := range r.registry.sessions {
		if candidate.zone != source.zone || candidate.binding.UserID == 0 ||
			candidate.deployedCreatureIndex >= uint32(len(candidate.binding.Creatures)) {
			continue
		}
		current, isFound := members[candidate.binding.UserID]
		if isFound && current.generation > candidate.generation {
			continue
		}
		startingXP := uint32(max(float32(0), candidate.binding.StartingAvatarXP))
		members[candidate.binding.UserID] = campaignExperienceMember{
			sessionKey: candidateKey, generation: candidate.generation,
			userID: candidate.binding.UserID, gameID: candidate.binding.GameID,
			playerSlot: candidate.binding.Slot, startingXP: startingXP,
			bonus: candidate.binding.Creatures[candidate.deployedCreatureIndex].
				PlayerExperienceIncrease,
		}
	}
	r.registry.mutex.RUnlock()
	memberAwards := make(map[uint64]uint32, len(members))
	for userID, member := range members {
		multiplier := max(float64(0), 1+float64(member.bonus))
		modified := math.Floor(float64(baseExperience)*multiplier + 0.5)
		if modified <= 0 {
			continue
		}
		if modified > float64(math.MaxUint32) {
			modified = float64(math.MaxUint32)
		}
		memberAwards[userID] = uint32(modified)
	}
	if len(memberAwards) == 0 {
		return nil, nil
	}
	totals, isAccepted, err := source.zone.RecordNPCExperience(
		objectID, baseExperience, memberAwards,
	)
	if err != nil {
		return nil, fmt.Errorf("experienceRecord: %w", err)
	}
	if !isAccepted {
		return nil, nil
	}
	r.registry.mutex.Lock()
	for userID, total := range totals {
		member, isFound := members[userID]
		if !isFound || total > ^uint32(0)-member.startingXP {
			continue
		}
		candidate, isSessionFound := r.registry.sessions[member.sessionKey]
		if !isSessionFound || candidate.generation != member.generation ||
			candidate.zone != source.zone {
			continue
		}
		cumulativeXP := member.startingXP + total
		candidate.binding.AvatarXP = float32(cumulativeXP)
		candidate.binding.AvatarLevel = sporenet.AccountLevelForExperience(cumulativeXP)
		r.registry.sessions[member.sessionKey] = candidate
	}
	r.registry.mutex.Unlock()
	packets := make([][]byte, 0, 1)
	for userID, total := range totals {
		member, isFound := members[userID]
		if !isFound || total > math.MaxUint32-member.startingXP {
			continue
		}
		cumulativeXP := member.startingXP + total
		level := sporenet.AccountLevelForExperience(cumulativeXP)
		update := game.PlayerLevelUpdate{Level: level, XP: float32(cumulativeXP)}
		if member.sessionKey != sessionKey {
			if r.gameplayJoin != nil {
				r.gameplayJoin.RequestPlayerLevelUpdate(int64(userID), member.gameID, update)
			}
			continue
		}
		progressionPacket, marshalErr := heroraknet.Progression(
			heroraknet.ProgressionRequest{
				PlayerIndex: uint8(member.playerSlot), AvatarLevel: level,
				AvatarXP: float32(cumulativeXP),
			},
		)
		if marshalErr != nil {
			return nil, fmt.Errorf("experienceMarshal: %w", marshalErr)
		}
		packets = append(packets, progressionPacket)
	}
	return packets, nil
}

type campaignDeathRuntime struct {
	timer  zone.Timer
	logger *log.Logger
}

type campaignDeathDeadline struct {
	zone     *zone.Zone
	objectID uint32
	run      *deathraknet.Run
	deadline time.Duration
	isFinal  bool
	logger   *log.Logger
}

func (d campaignDeathDeadline) execute() {
	if d.zone == nil || d.run == nil {
		return
	}
	death := d.zone.Death()
	event, isCurrent, err := death.Advance(
		context.Background(), d.objectID, d.run, d.deadline,
	)
	if err != nil {
		death.Remove(d.objectID, d.run)
		if d.logger != nil {
			d.logger.Printf(
				"Campaign instance death advance failed object=%d deadline=%s: %v",
				d.objectID, d.deadline, err,
			)
		}
		return
	}
	if !isCurrent {
		return
	}
	err = d.zone.RecordNPCDeaths(context.Background(), event, 0, 0)
	if err != nil {
		if d.logger != nil {
			d.logger.Printf(
				"Campaign instance death objective failed object=%d deadline=%s: %v",
				d.objectID, d.deadline, err,
			)
		}
	}
	if d.isFinal {
		death.Complete(d.objectID, d.run)
	}
}

func (r campaignDeathRuntime) schedule(
	zone *zone.Zone, objectID uint32, run *deathraknet.Run,
) error {
	if r.timer == nil || zone == nil || zone.Death() == nil {
		return errors.New("campaign death timer unavailable")
	}
	if objectID == 0 || run == nil {
		return errors.New("campaign death run invalid")
	}
	death := zone.Death()
	err := death.Add(objectID, run)
	if err != nil {
		return fmt.Errorf("deathAdd: %w", err)
	}
	deadline := run.Deadlines()
	for index, current := range deadline {
		step := campaignDeathDeadline{
			zone: zone, objectID: objectID, run: run,
			deadline: current, isFinal: index == len(deadline)-1,
			logger: r.logger,
		}
		cancel, scheduleErr := r.timer.Schedule(current, step.execute)
		if scheduleErr != nil {
			death.Remove(objectID, run)
			return fmt.Errorf("deathSchedule[%d]: %w", index, scheduleErr)
		}
		scheduleErr = death.AddCancel(
			objectID, run, zonedeath.Cancel(cancel),
		)
		if scheduleErr != nil {
			cancel()
			death.Remove(objectID, run)
			return fmt.Errorf("deathCancel[%d]: %w", index, scheduleErr)
		}
	}
	return nil
}

var _ zonedeath.Run = (*deathraknet.Run)(nil)

func (r campaignNPCActionRuntime) scheduleEnemyDeath(
	packet raknet.Packet, sessionKey string, generation uint64,
	targetID uint32, run *deathraknet.Run,
) error {
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation
	instance := peerSession.zone
	r.registry.mutex.RUnlock()
	if !isCurrent {
		return errors.New("enemy death session replaced")
	}
	if r.death.timer != nil && instance != nil {
		err := r.death.schedule(instance, targetID, run)
		if err != nil {
			return fmt.Errorf("instanceDeath: %w", err)
		}
		return nil
	}
	deathProducers := make(
		[]raknet.ScheduledPacketProducer, 0, len(run.Deadlines()),
	)
	for _, deadline := range run.Deadlines() {
		deathProducers = append(deathProducers, raknet.ScheduledPacketProducer{
			Delay: deadline,
			Produce: r.enemyDeathProducer(
				sessionKey, generation, targetID, run, deadline,
			),
		})
	}
	deathProducers = r.registry.producerGuard.scheduledProducers(sessionKey, deathProducers)
	var cancel raknet.CancelSchedule
	var err error
	if packet.ScheduleGroupResult != nil {
		cancel, err = packet.ScheduleGroupResult(
			deathProducers,
			r.retireEnemyDeath(sessionKey, generation, targetID, run),
		)
	} else if packet.ScheduleGroup != nil {
		cancel, err = packet.ScheduleGroup(deathProducers)
	} else {
		err = errors.New("enemy death scheduler unavailable")
	}
	if err != nil {
		return fmt.Errorf("peerDeath: %w", err)
	}
	if cancel == nil {
		return errors.New("peer death cancellation unavailable")
	}
	run.SetCancel(cancel)
	r.registry.mutex.Lock()
	latestSession, isLatestFound := r.registry.sessions[sessionKey]
	if isLatestFound && latestSession.generation == generation {
		if latestSession.enemyDeaths == nil {
			latestSession.enemyDeaths = make(map[uint32]*deathraknet.Run)
		}
		latestSession.enemyDeaths[targetID] = run
		r.registry.sessions[sessionKey] = latestSession
	}
	r.registry.mutex.Unlock()
	return nil
}

type campaignNPCDeathStep struct {
	registry   *gameplaySessionRegistry
	logger     *log.Logger
	sessionKey string
	generation uint64
	targetID   uint32
	run        *deathraknet.Run
	deadline   time.Duration
}

func (s campaignNPCDeathStep) produce() ([][]byte, error) {
	s.registry.mutex.Lock()
	peerSession, isFound := s.registry.sessions[s.sessionKey]
	isCurrent := isFound && peerSession.generation == s.generation &&
		peerSession.enemyDeaths[s.targetID] == s.run
	if !isCurrent {
		s.registry.mutex.Unlock()
		return nil, nil
	}
	packets, err := s.run.Advance(context.Background(), s.deadline)
	if err != nil {
		s.registry.mutex.Unlock()
		return nil, fmt.Errorf(
			"enemyDeathAdvance[%d/%s]: %w", s.targetID, s.deadline, err,
		)
	}
	if peerSession.zone != nil {
		err = peerSession.zone.RecordNPCDeaths(
			context.Background(), s.run.DrainProjection(),
			peerSession.binding.UserID, s.generation,
		)
		if err != nil {
			if s.logger != nil {
				s.logger.Printf(
					"RakNet enemy-death objective omitted object=%d: %v",
					s.targetID, err,
				)
			}
		}
	}
	if s.run.IsFinalDeadline(s.deadline) {
		if peerSession.zone != nil && peerSession.zone.Security() != nil {
			decision, activationErr := peerSession.zone.Security().
				ReserveClearedPresentation(peerSession.zone.SecurityThreats())
			if activationErr != nil {
				s.registry.mutex.Unlock()
				return nil, fmt.Errorf(
					"enemyDeathSecurityReserve[%d]: %w",
					s.targetID, activationErr,
				)
			}
			if decision.IsActivation {
				activationPackets, marshalErr := securityraknet.State(
					decision.ObjectID, decision.Teleport, true, true,
				)
				if marshalErr != nil {
					peerSession.zone.Security().CancelPresentation(
						decision.RouteIndex,
					)
					s.registry.mutex.Unlock()
					return nil, fmt.Errorf(
						"enemyDeathSecurityMarshal[%d]: %w",
						s.targetID, marshalErr,
					)
				}
				commitErr := peerSession.zone.Security().CommitPresentation(
					decision.RouteIndex,
				)
				if commitErr != nil {
					s.registry.mutex.Unlock()
					return nil, fmt.Errorf(
						"enemyDeathSecurityCommit[%d]: %w",
						s.targetID, commitErr,
					)
				}
				packets = append(packets, activationPackets...)
			}
		}
		teleporterPackets, teleporterErr := peerSession.syncCampaignTeleporterStates()
		if teleporterErr != nil {
			s.registry.mutex.Unlock()
			return nil, fmt.Errorf(
				"enemyDeathCampaignTeleporter[%d]: %w",
				s.targetID, teleporterErr,
			)
		}
		packets = append(packets, teleporterPackets...)
		activationPackets, activationErr := peerSession.activateTutorialTeleporter()
		if activationErr != nil {
			s.registry.mutex.Unlock()
			return nil, fmt.Errorf(
				"enemyDeathTutorialTeleporter[%d]: %w",
				s.targetID, activationErr,
			)
		}
		packets = append(packets, activationPackets...)
		s.run.ClearCancel()
		delete(peerSession.enemyDeaths, s.targetID)
		s.registry.sessions[s.sessionKey] = peerSession
	}
	s.registry.mutex.Unlock()
	return packets, nil
}

type campaignNPCDeathFailure struct {
	registry   *gameplaySessionRegistry
	logger     *log.Logger
	sessionKey string
	generation uint64
	targetID   uint32
	run        *deathraknet.Run
}

func (f campaignNPCDeathFailure) handle(scheduleErr error) {
	f.registry.mutex.Lock()
	peerSession, isFound := f.registry.sessions[f.sessionKey]
	isCurrent := isFound && peerSession.generation == f.generation &&
		peerSession.enemyDeaths[f.targetID] == f.run
	if isCurrent {
		f.run.ClearCancel()
		delete(peerSession.enemyDeaths, f.targetID)
		f.registry.sessions[f.sessionKey] = peerSession
	}
	f.registry.mutex.Unlock()
	if !isCurrent {
		return
	}
	f.run.Stop()
	f.logger.Printf(
		"RakNet enemy-death simulator stopped after schedule failure for %s/%d: %v",
		f.sessionKey, f.targetID, scheduleErr,
	)
}

func (r campaignNPCActionRuntime) enemyDeathProducer(
	sessionKey string, generation uint64, targetID uint32, run *deathraknet.Run,
	deadline time.Duration,
) func() ([][]byte, error) {
	step := campaignNPCDeathStep{
		registry: r.registry, logger: r.logger,
		sessionKey: sessionKey, generation: generation,
		targetID: targetID, run: run, deadline: deadline,
	}
	return step.produce
}

func (r campaignNPCActionRuntime) retireEnemyDeath(
	sessionKey string, generation uint64, targetID uint32, run *deathraknet.Run,
) func(error) {
	failure := campaignNPCDeathFailure{
		registry: r.registry, logger: r.logger, sessionKey: sessionKey,
		generation: generation, targetID: targetID, run: run,
	}
	return failure.handle
}

type campaignPlasmaModifierRun struct {
	modifier  *campaignNPCModifierRun
	kind      zoneability.PlasmaModifierKind
	targetID  uint32
	expiresAt time.Time
	cancel    raknet.CancelSchedule
}

type campaignPlasmaBurnStep struct {
	runtime        campaignDamageRuntime
	packet         raknet.Packet
	sessionKey     string
	generation     uint64
	sourceObjectID uint32
	targetObjectID uint32
	timestamp      uint64
	binding        game.GameplayBinding
	plan           zoneability.PlasmaModifierPlan
	run            *campaignPlasmaModifierRun
	tickIndex      int
	deadline       time.Duration
}

func (s campaignPlasmaBurnStep) produce() ([][]byte, error) {
	s.runtime.registry.mutex.Lock()
	peerSession, isFound := s.runtime.registry.sessions[s.sessionKey]
	isCurrent := isFound && peerSession.generation == s.generation &&
		peerSession.campaignPlasmaModifiers[s.targetObjectID] == s.run &&
		peerSession.zone.NPCs() != nil
	if !isCurrent {
		s.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	target, isTargetFound := peerSession.zone.NPCs().NPC(s.targetObjectID)
	if !isTargetFound || target.IsDefeated {
		s.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	damage, err := sim.SelectRankDamage(
		peerSession.zone.Population().Random(),
		sim.DamageRange{
			Minimum: s.plan.Damage.Minimum,
			Maximum: s.plan.Damage.Maximum,
		},
	)
	if err != nil {
		s.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("plasmaBurnDamage[%d]: %w", s.tickIndex, err)
	}
	damageResult, err := peerSession.zone.NPCs().Hit(zonenpc.HitRequest{
		SourceObjectID: s.sourceObjectID, TargetObjectID: s.targetObjectID, Damage: damage,
		Metadata: zonenpc.DamageMetadata{DamageSource: 1, DamageType: 3, IsDamageTypeKnown: true, DescriptorMask: 36},
	})
	if err != nil {
		s.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("plasmaBurnCommit[%d]: %w", s.tickIndex, err)
	}
	transition, err := peerSession.applyCampaignDamageTransition(damageResult)
	if err != nil {
		s.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("plasmaBurnTransition[%d]: %w", s.tickIndex, err)
	}
	s.runtime.registry.sessions[s.sessionKey] = peerSession
	s.runtime.registry.mutex.Unlock()
	result := zoneability.AreaResult{Snapshot: target, Damage: damageResult}
	packet, err := s.runtime.publishAreaResults(
		s.packet, s.sessionKey, s.generation, s.sourceObjectID,
		s.timestamp+uint64(s.deadline/time.Millisecond), s.binding,
		[]zoneability.AreaResult{result},
		[]campaignDamageTransition{transition}, nil, false,
	)
	if err != nil {
		return nil, fmt.Errorf("plasmaBurnPublish[%d]: %w", s.tickIndex, err)
	}
	return packet, nil
}

type campaignPlasmaModifierExpiryStep struct {
	runtime        campaignDamageRuntime
	sessionKey     string
	generation     uint64
	targetObjectID uint32
	run            *campaignPlasmaModifierRun
	modifier       *campaignNPCModifierRun
}

func (s campaignPlasmaModifierExpiryStep) produce() ([][]byte, error) {
	s.runtime.registry.mutex.Lock()
	peerSession, isFound := s.runtime.registry.sessions[s.sessionKey]
	isCurrent := isFound && peerSession.generation == s.generation &&
		peerSession.removeCampaignPlasmaModifier(s.run)
	if isCurrent {
		s.runtime.registry.sessions[s.sessionKey] = peerSession
	}
	s.runtime.registry.mutex.Unlock()
	isCreated, err := s.modifier.release(s.runtime.npc.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("plasmaModifierDeleteRelease: %w", err)
	}
	if !isCurrent || !isCreated {
		return nil, nil
	}
	packet, err := effectraknet.ModifierDelete(
		s.targetObjectID, s.modifier.instanceID,
	)
	if err != nil {
		return nil, fmt.Errorf("plasmaModifierDelete: %w", err)
	}
	return [][]byte{packet}, nil
}

func (s *gameplayPeerSession) installCampaignPlasmaModifier(
	run *campaignPlasmaModifierRun,
) (*campaignPlasmaModifierRun, error) {
	if s == nil || run == nil || run.modifier == nil || run.targetID == 0 ||
		run.expiresAt.IsZero() {
		return nil, errors.New("plasma modifier install invalid")
	}
	err := s.trackCampaignNPCModifier(run.modifier)
	if err != nil {
		return nil, fmt.Errorf("plasmaModifierTrack: %w", err)
	}
	if s.campaignPlasmaModifiers == nil {
		s.campaignPlasmaModifiers = make(map[uint32]*campaignPlasmaModifierRun)
	}
	previous := s.campaignPlasmaModifiers[run.targetID]
	s.campaignPlasmaModifiers[run.targetID] = run
	return previous, nil
}

func (s *gameplayPeerSession) removeCampaignPlasmaModifier(
	run *campaignPlasmaModifierRun,
) bool {
	if s == nil || run == nil || s.campaignPlasmaModifiers[run.targetID] != run {
		return false
	}
	if run.kind == zoneability.PlasmaModifierShock && s.zone.NPCs() != nil {
		s.zone.NPCs().ClearStun(run.targetID, run.expiresAt)
	}
	delete(s.campaignPlasmaModifiers, run.targetID)
	s.untrackCampaignNPCModifier(run.modifier)
	return true
}

func (s *gameplayPeerSession) stopCampaignPlasmaModifiers(pool *modifierPool) {
	if s == nil {
		return
	}
	for targetID, run := range s.campaignPlasmaModifiers {
		if run.cancel != nil {
			run.cancel()
		}
		if run.kind == zoneability.PlasmaModifierShock &&
			s.zone.NPCs() != nil {
			s.zone.NPCs().ClearStun(targetID, run.expiresAt)
		}
		s.untrackCampaignNPCModifier(run.modifier)
		if run.modifier != nil && pool != nil {
			_, _ = run.modifier.release(pool)
		}
		delete(s.campaignPlasmaModifiers, targetID)
	}
}

func (r campaignDamageRuntime) publishPlasmaModifier(
	packet raknet.Packet, sessionKey string, generation uint64,
	sourceObjectID uint32, targetObjectID uint32, timestamp uint64,
	binding game.GameplayBinding, plan zoneability.PlasmaModifierPlan,
) ([][]byte, error) {
	modifier, err := newCampaignNPCModifierRun(r.npc.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("plasmaModifierRun: %w", err)
	}
	run := &campaignPlasmaModifierRun{
		modifier: modifier, kind: plan.Kind, targetID: targetObjectID,
		expiresAt: r.npc.now().Add(plan.Duration),
	}
	createPacket, err := effectraknet.ModifierCreate(
		effectraknet.ModifierCreateRequest{
			SourceObjectID: sourceObjectID,
			TargetObjectID: targetObjectID,
			ModifierID:     plan.ModifierID,
			InstanceID:     modifier.instanceID,
			Duration:       plan.Duration,
			Timestamp:      timestamp,
		},
	)
	if err != nil {
		_, releaseErr := modifier.release(r.npc.modifierPool)
		return nil, fmt.Errorf("plasmaModifierCreate: %w", errors.Join(err, releaseErr))
	}
	producers := make([]raknet.ScheduledPacketProducer, 0, 6)
	if plan.Kind == zoneability.PlasmaModifierBurn {
		if plan.Tick <= 0 || plan.Duration%plan.Tick != 0 {
			_, releaseErr := modifier.release(r.npc.modifierPool)
			if releaseErr != nil {
				return nil, fmt.Errorf("plasmaBurnTimingRelease: %w", releaseErr)
			}
			return nil, errors.New("plasma burn timing invalid")
		}
		tickCount := int(plan.Duration / plan.Tick)
		for tickIndex := 1; tickIndex <= tickCount; tickIndex++ {
			deadline := time.Duration(tickIndex) * plan.Tick
			step := campaignPlasmaBurnStep{
				runtime: r, packet: packet, sessionKey: sessionKey,
				generation: generation, sourceObjectID: sourceObjectID,
				targetObjectID: targetObjectID, timestamp: timestamp,
				binding: binding, plan: plan, run: run,
				tickIndex: tickIndex, deadline: deadline,
			}
			producers = append(producers, raknet.ScheduledPacketProducer{
				Delay:   deadline,
				Produce: step.produce,
			})
		}
	}
	expiryStep := campaignPlasmaModifierExpiryStep{
		runtime: r, sessionKey: sessionKey, generation: generation,
		targetObjectID: targetObjectID, run: run, modifier: modifier,
	}
	producers = append(producers, raknet.ScheduledPacketProducer{
		Delay:   plan.Duration,
		Produce: expiryStep.produce,
	})
	sortScheduledPacketProducersByDelay(producers)
	cancel, scheduleErr := scheduleNPCProducers(r.registry, packet, producers)
	if scheduleErr != nil {
		_, releaseErr := modifier.release(r.npc.modifierPool)
		return nil, fmt.Errorf("plasmaModifierSchedule: %w", errors.Join(scheduleErr, releaseErr))
	}
	run.cancel = cancel
	r.registry.mutex.Lock()
	currentSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && currentSession.generation == generation &&
		currentSession.zone.NPCs() != nil
	var previous *campaignPlasmaModifierRun
	if isCurrent && plan.Kind == zoneability.PlasmaModifierShock {
		err = currentSession.zone.NPCs().ApplyStun(targetObjectID, run.expiresAt)
		isCurrent = err == nil
	}
	if isCurrent {
		previous, err = currentSession.installCampaignPlasmaModifier(run)
		isCurrent = err == nil
	}
	if !isCurrent && plan.Kind == zoneability.PlasmaModifierShock &&
		currentSession.zone.NPCs() != nil {
		currentSession.zone.NPCs().ClearStun(targetObjectID, run.expiresAt)
	}
	if isCurrent {
		r.registry.sessions[sessionKey] = currentSession
	}
	r.registry.mutex.Unlock()
	if !isCurrent {
		cancel()
		_, releaseErr := modifier.release(r.npc.modifierPool)
		if err != nil {
			return nil, fmt.Errorf("plasmaModifierInstall: %w", errors.Join(err, releaseErr))
		}
		if releaseErr != nil {
			return nil, fmt.Errorf("plasmaModifierRelease: %w", releaseErr)
		}
		return nil, nil
	}
	packets := make([][]byte, 0, 2)
	if previous != nil {
		if previous.cancel != nil {
			previous.cancel()
		}
		r.registry.mutex.Lock()
		latestSession, isLatestFound := r.registry.sessions[sessionKey]
		if isLatestFound && latestSession.generation == generation {
			latestSession.untrackCampaignNPCModifier(previous.modifier)
			if previous.kind == zoneability.PlasmaModifierShock &&
				latestSession.zone.NPCs() != nil {
				latestSession.zone.NPCs().ClearStun(
					previous.targetID, previous.expiresAt,
				)
			}
			r.registry.sessions[sessionKey] = latestSession
		}
		r.registry.mutex.Unlock()
		isCreated, releaseErr := previous.modifier.release(r.npc.modifierPool)
		if releaseErr != nil {
			return nil, fmt.Errorf("plasmaModifierReplaceRelease: %w", releaseErr)
		}
		if isCreated {
			deletePacket, deleteErr := effectraknet.ModifierDelete(
				targetObjectID, previous.modifier.instanceID,
			)
			if deleteErr != nil {
				return nil, fmt.Errorf("plasmaModifierReplaceDelete: %w", deleteErr)
			}
			packets = append(packets, deletePacket)
		}
	}
	if modifier.create() {
		packets = append(packets, createPacket)
	}
	return packets, nil
}

func (s *gameplayPeerSession) planCampaignBossFollowup(
	actorCount int,
) (
	[]zonenpc.SpawnPlan, [][]byte, error,
) {
	plans, err := s.zone.PlanBossFollowup(
		s.deployedObjectID, s.binding.GameID, actorCount,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("bossPlan: %w", err)
	}
	packets, err := npcraknet.TargetedSpawns(plans, s.deployedObjectID)
	if err != nil {
		return nil, nil, fmt.Errorf("bossMarshal: %w", err)
	}
	if isCampaignBossIntroDelayed(plans[0]) {
		return plans, packets, nil
	}
	activePacket, err := bossraknet.Active(
		plans[0].ObjectID, zoneboss.IsFinalBossNoun(plans[0].NounName),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("bossActiveMarshal: %w", err)
	}
	packets = append(packets, activePacket)
	return plans, packets, nil
}

func (s *gameplayPeerSession) admitReservedCampaignBoss(
	plans []zonenpc.SpawnPlan,
) error {
	if s == nil || len(plans) == 0 {
		return errors.New("boss reservation invalid")
	}
	err := s.zone.AdmitBossSecondWave(
		s.deployedObjectID, plans,
	)
	if err != nil {
		return fmt.Errorf("bossAdmit: %w", err)
	}
	return nil
}

func planCampaignNPCZelemBlink(
	enemy zonenpc.Snapshot, targetObjectID uint32,
	targetPosition game.Vec3, targetFootprintRadius float32,
) (zonenpc.AttackPlan, error) {
	profile, isFound := zonenpc.ActionProfileForPlan(enemy.Plan)
	if !isFound || profile.Family != zonenpc.ActionZelemRanged ||
		profile.TeleportNormalDistance <= 0 {
		return zonenpc.AttackPlan{}, errors.New("enemy blink unsupported")
	}
	if enemy.IsDefeated || enemy.HitPoint <= 0 || targetObjectID == 0 ||
		enemy.TargetObjectID != targetObjectID {
		return zonenpc.AttackPlan{},
			errors.New("enemy blink target unavailable")
	}
	stopDistance, err := zoneaction.NPCStopDistance(
		profile.Range, enemy.Plan.NPCProfile.FootprintRadius,
		targetFootprintRadius,
	)
	if err != nil {
		return zonenpc.AttackPlan{},
			fmt.Errorf("enemyBlinkRange: %w", err)
	}
	profile.Range = stopDistance
	if zoneability.Distance(enemy.Plan.Position, targetPosition) >=
		profile.Range {
		return zonenpc.AttackPlan{},
			errors.New("enemy blink target out of range")
	}
	return zonenpc.AttackPlan{
		SourceObjectID: enemy.Plan.ObjectID, TargetObjectID: targetObjectID,
		ActionGeneration: enemy.ActionGeneration,
		SourcePosition:   enemy.Plan.Position, TargetPosition: targetPosition,
		Profile: profile,
	}, nil
}

func campaignNPCZelemBlinkDestination(
	plan zonenpc.AttackPlan,
) (game.Vec3, error) {
	if plan.SourceObjectID == 0 || plan.TargetObjectID == 0 ||
		!isFiniteCampaignPopulationPosition(plan.TargetPosition) ||
		plan.Profile.TeleportMinimumDistance <= 0 ||
		plan.Profile.TeleportNormalDistance <
			plan.Profile.TeleportMinimumDistance ||
		plan.Profile.TeleportNormalDistance >
			plan.Profile.TeleportMaximumDistance {
		return game.Vec3{}, errors.New("enemy blink destination invalid")
	}
	angle := float64(plan.SourceObjectID%360) * math.Pi / 180
	distance := float64(plan.Profile.TeleportNormalDistance)
	return game.Vec3{
		X: plan.TargetPosition.X + float32(math.Cos(angle)*distance),
		Y: plan.TargetPosition.Y + float32(math.Sin(angle)*distance),
		Z: plan.TargetPosition.Z,
	}, nil
}

func campaignNPCProjectileAbility(
	profile zonenpc.ActionProfile,
) (sim.AbilityDefinition, error) {
	if profile.ProjectileNoun == "" || profile.ProjectileSpeed <= 0 ||
		profile.ProjectileDistance <= 0 || profile.HitDelay <= 0 ||
		profile.ReleaseDelay < profile.HitDelay ||
		profile.Cooldown <= 0 {
		return sim.AbilityDefinition{}, errors.New("enemy projectile invalid")
	}
	impactEffectName := profile.ImpactEffectName
	if profile.IsProjectilePiercing {
		if profile.MissEffectName == "" {
			return sim.AbilityDefinition{}, errors.New("enemy piercing projectile exit unavailable")
		}
		impactEffectName = profile.MissEffectName
	}
	return sim.AbilityDefinition{
		Name: profile.AbilityName, Kind: sim.AbilityKindProjectile,
		AnimationName:    profile.AnimationName,
		ProjectileNoun:   profile.ProjectileNoun,
		TrailEffectName:  profile.TrailEffectName,
		MuzzleEffectName: profile.SourceEffectName,
		ImpactEffectName: impactEffectName,
		MissEffectName:   profile.MissEffectName,
		Speed:            profile.ProjectileSpeed, Distance: profile.ProjectileDistance,
		HitDelay: profile.HitDelay, ReleaseDelay: profile.ReleaseDelay,
		Cooldown: profile.Cooldown, MinimumDamage: profile.MinimumDamage,
		MaximumDamage:     profile.MaximumDamage,
		DamageCoefficient: profile.DamageCoefficient, Range: profile.Range,
	}, nil
}

func campaignNPCProjectileEndpoints(
	enemy zonenpc.Snapshot, target zone.NPCTarget,
	geometry zoneability.ProjectileCollisionGeometry,
	physics zoneNounPhysics,
) (game.Vec3, game.Vec3) {
	source := enemy.Plan.Position
	targetPosition := target.Position
	sourceHeight := max(
		enemy.Plan.NPCProfile.FootprintRadius,
		(physics.BoundMinimum.Z+physics.BoundMaximum.Z)*0.5,
	)
	targetHeight := max(
		target.FootprintRadius,
		(geometry.TargetMinimum.Z+geometry.TargetMaximum.Z)*0.5,
	)
	source.Z += max(float32(0), sourceHeight)
	targetPosition.Z += max(float32(0), targetHeight)
	return source, targetPosition
}

func campaignProjectileLaunchPosition(
	sourcePosition game.Vec3, targetPosition game.Vec3, footprintRadius float32,
) game.Vec3 {
	direction := targetPosition.Sub(sourcePosition)
	distance := direction.Length()
	if distance <= 0 || footprintRadius <= 0 {
		return sourcePosition
	}
	return sourcePosition.Add(direction.Scale(footprintRadius / distance))
}

func campaignProjectileAimPosition(
	targetPosition game.Vec3,
	footprintRadius float32,
	geometry zonenpc.ProjectileGeometry,
) game.Vec3 {
	targetHeight := max(
		footprintRadius,
		(geometry.TargetMinimum.Z+geometry.TargetMaximum.Z)*0.5,
	)
	targetPosition.Z += max(float32(0), targetHeight)
	return targetPosition
}

func campaignTargetProjectileGeometry(
	program Programs, target zonenpc.Snapshot,
) (zonenpc.ProjectileGeometry, bool, error) {
	physics, isFound := program.NounPhysics[target.Plan.NounName]
	halfExtent := program.ProjectileHalfExtent
	if halfExtent.X <= 0 || halfExtent.Y <= 0 || halfExtent.Z <= 0 {
		return zonenpc.ProjectileGeometry{}, false,
			errors.New("projectile geometry unavailable")
	}
	isTargetGeometryFound := isFound &&
		physics.BoundMinimum.X < physics.BoundMaximum.X &&
		physics.BoundMinimum.Y < physics.BoundMaximum.Y &&
		physics.BoundMinimum.Z < physics.BoundMaximum.Z
	if isTargetGeometryFound {
		return zonenpc.ProjectileGeometry{
			ProjectileHalfExtent: halfExtent,
			TargetMinimum:        physics.BoundMinimum,
			TargetMaximum:        physics.BoundMaximum,
		}, false, nil
	}
	footprint := target.Plan.NPCProfile.FootprintRadius
	if footprint <= 0 || math.IsNaN(float64(footprint)) ||
		math.IsInf(float64(footprint), 0) {
		return zonenpc.ProjectileGeometry{}, false, fmt.Errorf(
			"targetGeometry: %q unavailable", target.Plan.NounName,
		)
	}
	return zonenpc.ProjectileGeometry{
		ProjectileHalfExtent: halfExtent,
		TargetMinimum: sim.Position{
			X: -footprint, Y: -footprint,
		},
		TargetMaximum: sim.Position{
			X: footprint, Y: footprint, Z: footprint * 2,
		},
	}, true, nil
}

func campaignNPCParallelProjectileSource(
	source game.Vec3, target game.Vec3, offset game.Vec3, shotIndex int,
) game.Vec3 {
	lateralOffset := offset.X
	if shotIndex%2 == 1 {
		lateralOffset = -lateralOffset
	}
	offset.X = lateralOffset
	return campaignNPCProjectileSourceOffset(source, target, offset)
}

func campaignNPCProjectileSourceOffset(
	source game.Vec3, target game.Vec3, offset game.Vec3,
) game.Vec3 {
	deltaX := target.X - source.X
	deltaY := target.Y - source.Y
	length := float32(math.Hypot(float64(deltaX), float64(deltaY)))
	if length <= 0 {
		return source
	}
	forwardX := deltaX / length
	forwardY := deltaY / length
	source.X += -forwardY*offset.X + forwardX*offset.Y
	source.Y += forwardX*offset.X + forwardY*offset.Y
	return source
}

func campaignNPCFirstAction(
	plan zonenpc.SpawnPlan, deployedObjectID uint32,
	deployedPosition game.Vec3, targetFootprintRadius float32,
) (zonenpc.FirstActionPlan, bool, error) {
	profile, isFound := campaignNPCActionProfile(
		plan, deployedPosition, targetFootprintRadius,
	)
	if !isFound {
		return zonenpc.FirstActionPlan{}, false, nil
	}
	if plan.OwnerObjectID != 0 && zonenpc.IsNashiraNoun(plan.NounName) {
		// Distance-based selection may replace the clone's stored ranged profile.
		profile = zonenpc.NashiraCloneProfile(profile)
	}
	action, err := zonenpc.PlanActionWithProfile(
		campaignNPCActionCommand(
			plan,
			deployedObjectID,
			deployedPosition,
			targetFootprintRadius,
		),
		profile,
	)
	if err != nil {
		return zonenpc.FirstActionPlan{}, false,
			fmt.Errorf("npcFirstAction: %w", err)
	}
	return action, true, nil
}

func campaignNPCActionProfile(
	plan zonenpc.SpawnPlan, targetPosition game.Vec3,
	targetFootprintRadius float32,
) (zonenpc.ActionProfile, bool) {
	profile, isFound := zonenpc.ActionProfileForPlan(plan)
	if !isFound {
		return zonenpc.ActionProfile{}, false
	}
	if zonenpc.ArcturusRank(plan.NounName) != 0 {
		surfaceDistance := max(float32(0), targetPosition.Sub(plan.Position).Length()-
			plan.NPCProfile.FootprintRadius-targetFootprintRadius)
		if surfaceDistance > profile.Range {
			return zonenpc.ArcturusSawBladeProfile(plan.NounName)
		}
	}
	nashiraSwipeProfile, isNashiraSwipeFound :=
		zonenpc.NashiraSwipeProfile(plan.NounName)
	if isNashiraSwipeFound {
		deltaX := targetPosition.X - plan.Position.X
		deltaY := targetPosition.Y - plan.Position.Y
		centerDistance := float32(math.Hypot(float64(deltaX), float64(deltaY)))
		surfaceDistance := max(
			float32(0), centerDistance-plan.NPCProfile.FootprintRadius-
				targetFootprintRadius,
		)
		if surfaceDistance <= nashiraSwipeProfile.Range {
			return nashiraSwipeProfile, true
		}
	}
	cryosBossMeleeProfile, isCryosBossMeleeFound :=
		zonenpc.CryosBossMeleeProfile(plan.NounName)
	if isCryosBossMeleeFound {
		deltaX := targetPosition.X - plan.Position.X
		deltaY := targetPosition.Y - plan.Position.Y
		centerDistance := float32(math.Hypot(float64(deltaX), float64(deltaY)))
		surfaceDistance := max(
			float32(0), centerDistance-plan.NPCProfile.FootprintRadius-
				targetFootprintRadius,
		)
		if surfaceDistance <= cryosBossMeleeProfile.Range {
			return cryosBossMeleeProfile, true
		}
	}
	stealthProfile, isStealthFound := zonenpc.StealtherStealthProfile(plan.NounName)
	if isStealthFound {
		deltaX := targetPosition.X - plan.Position.X
		deltaY := targetPosition.Y - plan.Position.Y
		centerDistance := float32(math.Hypot(float64(deltaX), float64(deltaY)))
		surfaceDistance := max(
			float32(0), centerDistance-plan.NPCProfile.FootprintRadius-
				targetFootprintRadius,
		)
		if surfaceDistance > profile.Range {
			return stealthProfile, true
		}
	}
	homerMeleeProfile, isHomerMeleeFound :=
		zonenpc.NocturnaSpecialHomerMeleeProfile(plan.NounName)
	if isHomerMeleeFound {
		deltaX := targetPosition.X - plan.Position.X
		deltaY := targetPosition.Y - plan.Position.Y
		centerDistance := float32(math.Hypot(float64(deltaX), float64(deltaY)))
		surfaceDistance := max(
			float32(0), centerDistance-plan.NPCProfile.FootprintRadius-
				targetFootprintRadius,
		)
		if surfaceDistance <= homerMeleeProfile.Range {
			return homerMeleeProfile, true
		}
	}
	hybridMeleeProfile, isHybridMeleeFound :=
		zonenpc.ZelemBasicHybridMeleeProfile(plan.NounName)
	if isHybridMeleeFound {
		deltaX := targetPosition.X - plan.Position.X
		deltaY := targetPosition.Y - plan.Position.Y
		centerDistance := float32(math.Hypot(float64(deltaX), float64(deltaY)))
		surfaceDistance := max(
			float32(0), centerDistance-plan.NPCProfile.FootprintRadius-
				targetFootprintRadius,
		)
		if surfaceDistance <= hybridMeleeProfile.Range {
			return hybridMeleeProfile, true
		}
	}
	specialTwoMeleeProfile, isSpecialTwoMeleeFound :=
		zonenpc.CitadelSpecialTwoMeleeProfile(plan.NounName)
	if isSpecialTwoMeleeFound {
		deltaX := targetPosition.X - plan.Position.X
		deltaY := targetPosition.Y - plan.Position.Y
		distance := float32(math.Hypot(float64(deltaX), float64(deltaY)))
		if distance <= 10 {
			return specialTwoMeleeProfile, true
		}
	}
	jumpProfile, isJumpFound := zonenpc.NomadBioSpecialTwoJumpProfile(plan.NounName)
	if !isJumpFound {
		return profile, true
	}
	deltaX := targetPosition.X - plan.Position.X
	deltaY := targetPosition.Y - plan.Position.Y
	centerDistance := float32(math.Hypot(float64(deltaX), float64(deltaY)))
	surfaceDistance := max(
		float32(0), centerDistance-plan.NPCProfile.FootprintRadius-
			targetFootprintRadius,
	)
	if surfaceDistance > profile.Range {
		return jumpProfile, true
	}
	return profile, true
}

func campaignNPCActionWithProfile(
	plan zonenpc.SpawnPlan, deployedObjectID uint32,
	deployedPosition game.Vec3, profile zonenpc.ActionProfile,
	targetFootprintRadius float32,
) (zonenpc.FirstActionPlan, error) {
	return zonenpc.PlanActionWithProfile(campaignNPCActionCommand(
		plan, deployedObjectID, deployedPosition, targetFootprintRadius,
	), profile)
}

func campaignNPCActionCommand(
	plan zonenpc.SpawnPlan, deployedObjectID uint32,
	deployedPosition game.Vec3, targetFootprintRadius float32,
) zonenpc.FirstActionCommand {
	return zonenpc.FirstActionCommand{
		ObjectID: plan.ObjectID, NounName: plan.NounName,
		SourcePosition:       plan.Position,
		ActorFootprintRadius: plan.NPCProfile.FootprintRadius,
		TargetObjectID:       deployedObjectID, TargetPosition: deployedPosition,
		TargetFootprintRadius: targetFootprintRadius,
	}
}

type campaignNPCActionRuntime struct {
	registry     *gameplaySessionRegistry
	projectile   campaignNPCProjectileAuthority
	pursuit      campaignNPCPursuitRuntime
	program      Programs
	logger       *log.Logger
	stats        *playerstat.Recorder
	modifierPool *modifierPool
	now          func() time.Time
	gameplayJoin *game.GameplayJoin
	death        campaignDeathRuntime
	timer        zone.Timer
	projection   gameplayProjectionRuntime
	effectPool   *attachedEffectPool
}

type campaignSageCompanionRespawnStep struct {
	runtime       campaignNPCActionRuntime
	sessionKey    string
	generation    uint64
	ownerObjectID uint32
	objectID      uint32
	run           *summonPassiveRun
}

func (r campaignNPCActionRuntime) reserveSageCompanionRespawnLocked(
	peerSession *gameplayPeerSession, sessionKey string, generation uint64,
	objectID uint32,
) {
	if peerSession == nil || sessionKey == "" || generation == 0 || objectID == 0 ||
		peerSession.sagePassive == nil || peerSession.zone == nil ||
		peerSession.zone.Companion() == nil || r.timer == nil {
		return
	}
	actor, isActorFound := peerSession.zone.Companion().Snapshot(objectID)
	activation, isActivationFound := peerSession.sagePassiveActivations[objectID]
	if !isActorFound || actor.HitPoint > 0 || !isActivationFound {
		return
	}
	if activation.Attack != nil {
		activation.Attack.Stop()
	}
	err := peerSession.sagePassive.CompanionDied(objectID)
	if err != nil {
		r.logger.Printf(
			"RakNet Sage companion respawn admission omitted remote=%s object=%d: %v",
			sessionKey, objectID, err,
		)
		return
	}
	peerSession.zone.Companion().Remove(objectID)
	delete(peerSession.sagePassiveActivations, objectID)
	step := campaignSageCompanionRespawnStep{
		runtime: r, sessionKey: sessionKey, generation: generation,
		ownerObjectID: peerSession.deployedObjectID, objectID: objectID,
		run: peerSession.sagePassive,
	}
	cancel, err := r.timer.Schedule(
		r.program.SupportHealerPassive.RespawnDelay, step.execute,
	)
	if err != nil {
		r.logger.Printf(
			"RakNet Sage companion respawn schedule omitted remote=%s object=%d: %v",
			sessionKey, objectID, err,
		)
		return
	}
	peerSession.sagePassive.AddCancel(cancel)
	r.logger.Printf(
		"RakNet Sage companion respawn scheduled remote=%s object=%d delay=%s",
		sessionKey, objectID, r.program.SupportHealerPassive.RespawnDelay,
	)
}

func (e campaignSageCompanionRespawnStep) execute() {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.deployedObjectID == e.ownerObjectID &&
		peerSession.sagePassive == e.run && peerSession.zone != nil &&
		peerSession.zone.Companion() != nil
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return
	}
	replacementObjectID, err := peerSession.reserveCampaignObjectID()
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		e.runtime.logger.Printf(
			"RakNet Sage companion respawn object reservation omitted remote=%s object=%d: %v",
			e.sessionKey, e.objectID, err,
		)
		return
	}
	requests, err := e.run.RespawnCompanion(e.objectID, replacementObjectID)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		e.runtime.logger.Printf(
			"RakNet Sage companion respawn omitted remote=%s object=%d: %v",
			e.sessionKey, e.objectID, err,
		)
		return
	}
	if len(requests) != 1 || requests[0].ObjectID != replacementObjectID ||
		requests[0].Spawn == nil {
		e.runtime.registry.mutex.Unlock()
		e.runtime.logger.Printf(
			"RakNet Sage companion respawn intent omitted remote=%s object=%d count=%d",
			e.sessionKey, e.objectID, len(requests),
		)
		return
	}
	packets := make([][]byte, 0, len(requests)*2)
	for index, request := range requests {
		position, placementErr := summonPassiveSpawnPosition(
			peerSession.playerPosition, index, len(requests),
			request.Spawn.SpawnRadius,
		)
		if placementErr != nil {
			e.runtime.registry.mutex.Unlock()
			e.runtime.logger.Printf(
				"RakNet Sage companion respawn placement omitted remote=%s object=%d: %v",
				e.sessionKey, e.objectID, placementErr,
			)
			return
		}
		spawnPackets, activation, marshalErr :=
			marshalSummonPassiveWorldRequest(request, position)
		if marshalErr != nil || activation == nil {
			e.runtime.registry.mutex.Unlock()
			e.runtime.logger.Printf(
				"RakNet Sage companion respawn marshal omitted remote=%s object=%d: %v",
				e.sessionKey, e.objectID, marshalErr,
			)
			return
		}
		hitPoint := e.runtime.program.NonPlayerHitPoint[util.HashID("HelperMelee")]
		hitPoint = peerSession.petHitPoint(hitPoint)
		footprintRadius, footprintErr :=
			e.runtime.program.FootprintRadius("HelperMelee.Noun")
		if hitPoint <= 0 || footprintErr != nil || footprintRadius <= 0 {
			e.runtime.registry.mutex.Unlock()
			e.runtime.logger.Printf(
				"RakNet Sage companion respawn profile omitted remote=%s object=%d",
				e.sessionKey, e.objectID,
			)
			return
		}
		err = peerSession.zone.Companion().Put(zonecompanion.Actor{
			UserID: peerSession.binding.UserID, PeerGeneration: peerSession.generation,
			ObjectID: activation.ObjectID, OwnerObjectID: activation.OwnerObjectID,
			Noun:            util.HashID("HelperMelee.Noun"),
			Position:        game.Vec3{X: activation.Position.X, Y: activation.Position.Y, Z: activation.Position.Z},
			FootprintRadius: footprintRadius, HitPoint: hitPoint,
			MaximumHitPoint: hitPoint, IsTargetable: true, IsCombatant: true,
		})
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			e.runtime.logger.Printf(
				"RakNet Sage companion respawn world admission omitted remote=%s object=%d: %v",
				e.sessionKey, e.objectID, err,
			)
			return
		}
		peerSession.sagePassiveActivations[request.ObjectID] = *activation
		packets = append(packets, spawnPackets...)
	}
	peerSession.queuePackets(packets)
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	e.runtime.registry.queuePeerPresentation(
		gameplayProducerIdentityFromSession(e.sessionKey, peerSession, true), packets,
	)
	e.runtime.logger.Printf(
		"RakNet Sage companion respawned remote=%s prior_object=%d object=%d",
		e.sessionKey, e.objectID, replacementObjectID,
	)
}

func (r campaignNPCActionRuntime) releaseAction(
	sessionKey string, generation uint64, objectID uint32,
) bool {
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	r.registry.mutex.RUnlock()
	if !isCurrent {
		return false
	}
	return peerSession.zone.NPCs().ReleaseAction(
		objectID, zonenpc.ActionOwner{
			UserID: peerSession.binding.UserID, PeerGeneration: generation,
		},
	)
}

func (r campaignNPCActionRuntime) releaseActionGeneration(
	sessionKey string, generation uint64, objectID uint32,
	actionGeneration uint64,
) bool {
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	r.registry.mutex.RUnlock()
	if !isCurrent {
		return false
	}
	return peerSession.zone.NPCs().ReleaseActionGeneration(
		objectID, zonenpc.ActionOwner{
			UserID: peerSession.binding.UserID, PeerGeneration: generation,
		}, actionGeneration,
	)
}

type campaignNPCAttackKind uint8

const (
	campaignNPCAttackDronePunch campaignNPCAttackKind = iota + 1
	campaignNPCAttackSnipeMelee
	campaignNPCAttackPlunge
	campaignNPCAttackBoomerSmash
	campaignNPCAttackChargeFollowup
	campaignNPCAttackCryosRezMelee
	campaignNPCAttackCryosChargeHeadbutt
	campaignNPCAttackPickyCharging
	campaignNPCAttackPickyMelee
	campaignNPCAttackMelee
	campaignNPCAttackNomadDragMeteor
	campaignNPCAttackExploderScarab
)

type campaignNPCAttackRequest struct {
	runtime          campaignNPCActionRuntime
	packet           raknet.Packet
	sessionKey       string
	generation       uint64
	objectID         uint32
	timestamp        uint64
	slowEndTimestamp uint64
	isPullSuppressed bool
	kind             campaignNPCAttackKind
}

func (e campaignNPCAttackRequest) produce() ([][]byte, error) {
	return e.resume(e.timestamp)
}

func (e campaignNPCAttackRequest) resume(timestamp uint64) ([][]byte, error) {
	switch e.kind {
	case campaignNPCAttackDronePunch:
		return e.runtime.produceDronePunch(
			e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
		)
	case campaignNPCAttackSnipeMelee:
		return e.runtime.produceSnipeMelee(
			e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
			e.slowEndTimestamp,
		)
	case campaignNPCAttackPlunge:
		return e.runtime.producePlunge(
			e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
		)
	case campaignNPCAttackBoomerSmash:
		return e.runtime.produceBoomerSmash(
			e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
		)
	case campaignNPCAttackChargeFollowup:
		return e.runtime.produceNomadChargeAttack(
			e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
		)
	case campaignNPCAttackCryosRezMelee:
		return e.runtime.produceZelemShot(
			e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
		)
	case campaignNPCAttackCryosChargeHeadbutt:
		return e.runtime.produceCryosBasicChargeHeadbutt(
			e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
		)
	case campaignNPCAttackPickyCharging:
		return e.runtime.produceVerdanthBasicPickyAttack(
			e.packet, e.sessionKey, e.generation, e.objectID, timestamp, true,
		)
	case campaignNPCAttackPickyMelee:
		return e.runtime.produceVerdanthBasicPickyAttack(
			e.packet, e.sessionKey, e.generation, e.objectID, timestamp, false,
		)
	case campaignNPCAttackMelee:
		if e.isPullSuppressed {
			return e.runtime.produceEnemyMeleeWithPull(
				e.packet, e.sessionKey, e.generation, e.objectID, timestamp, false,
			)
		}
		return e.runtime.produceEnemyMelee(
			e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
		)
	case campaignNPCAttackNomadDragMeteor:
		return e.runtime.producePlunge(
			e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
		)
	case campaignNPCAttackExploderScarab:
		return e.runtime.produceExploderScarab(
			e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
		)
	default:
		return nil, errors.New("enemy attack kind unsupported")
	}
}

type campaignNPCAttackSchedule struct {
	request campaignNPCAttackRequest
	plan    zonenpc.AttackPlan
	meteor  *campaignNPCMeteorRun
}

func (e campaignNPCAttackSchedule) fail(
	step string, err error,
) ([][]byte, error) {
	e.request.runtime.releaseActionGeneration(
		e.request.sessionKey, e.request.generation, e.request.objectID,
		e.plan.ActionGeneration,
	)
	return nil, fmt.Errorf("%s: %w", step, err)
}

func (e campaignNPCAttackSchedule) terminate() {
	e.request.runtime.releaseActionGeneration(
		e.request.sessionKey, e.request.generation, e.request.objectID,
		e.plan.ActionGeneration,
	)
}

func (e campaignNPCAttackSchedule) emerge() ([][]byte, error) {
	if e.request.kind != campaignNPCAttackPlunge &&
		e.request.kind != campaignNPCAttackNomadDragMeteor {
		return e.fail("enemyEmergeKind", errors.New("unsupported"))
	}
	if e.plan.Profile.EmergeDelay <= 0 {
		return e.fail("enemyEmergeDelay", errors.New("invalid"))
	}
	runtime := e.request.runtime
	runtime.registry.mutex.RLock()
	current, isFound := runtime.registry.sessions[e.request.sessionKey]
	isCurrent := isFound && current.isCampaignNPCAttackGenerationActiveAt(
		e.request.generation, e.request.objectID,
		e.plan.TargetObjectID, e.plan.ActionGeneration, runtime.now(),
	)
	runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	position := e.plan.TargetPosition
	if e.meteor != nil {
		position = e.meteor.positionSnapshot()
	}
	packet, err := npcraknet.PositionedEffect(
		e.plan.Profile.EmergeEffectName, position,
	)
	if err != nil {
		return e.fail("enemyPlungeEmerge", err)
	}
	return [][]byte{packet}, nil
}

func (e campaignNPCAttackSchedule) hit() ([][]byte, error) {
	if e.request.kind == campaignNPCAttackNomadDragMeteor {
		return e.hitNomadDragMeteor()
	}
	if e.request.kind == campaignNPCAttackPlunge {
		return e.hitVerdanthBasicPlunge()
	}
	req := e.request
	runtime := req.runtime
	runtime.registry.mutex.Lock()
	current, isFound := runtime.registry.sessions[req.sessionKey]
	isCurrent := isFound && current.isCampaignNPCAttackGenerationActiveAt(
		req.generation, req.objectID, e.plan.TargetObjectID,
		e.plan.ActionGeneration, runtime.now(),
	)
	if !isCurrent {
		runtime.registry.mutex.Unlock()
		return nil, nil
	}
	currentNPC, isNPCFound := current.zone.NPCs().NPC(req.objectID)
	if isNPCFound && currentNPC.IsShieldActive {
		runtime.registry.mutex.Unlock()
		return nil, nil
	}
	currentTarget, isTargetFound := current.campaignNPCTarget(
		req.generation, currentNPC.TargetObjectID,
	)
	if !isNPCFound || !isTargetFound {
		runtime.registry.mutex.Unlock()
		return nil, nil
	}
	currentPlan := e.plan
	var impactPacket []byte
	var err error
	if req.kind == campaignNPCAttackSnipeMelee {
		currentPlan, err = zonenpc.PlanAttackWithProfile(
			currentNPC, currentTarget.ObjectID, currentTarget.Position,
			zonenpc.NomadSnipeMeleeProfile(currentNPC.Plan.NounName),
			currentTarget.FootprintRadius,
		)
	} else if req.kind == campaignNPCAttackBoomerSmash {
		currentPlan, err = zonenpc.PlanAttackWithProfile(
			currentNPC, currentTarget.ObjectID, currentTarget.Position,
			zonenpc.BoomerSmashProfile(), currentTarget.FootprintRadius,
		)
		if err == nil {
			impactPacket, err = npcraknet.AttackImpact(currentPlan)
		}
	} else if req.kind == campaignNPCAttackMelee ||
		req.kind == campaignNPCAttackChargeFollowup ||
		req.kind == campaignNPCAttackCryosRezMelee ||
		req.kind == campaignNPCAttackCryosChargeHeadbutt ||
		req.kind == campaignNPCAttackPickyCharging ||
		req.kind == campaignNPCAttackPickyMelee {
		currentPlan, err = zonenpc.PlanAttackWithProfile(
			currentNPC, currentTarget.ObjectID, currentTarget.Position,
			e.plan.Profile, currentTarget.FootprintRadius,
		)
		if err == nil && currentPlan.Profile.ImpactEffectName != "" {
			impactPacket, err = npcraknet.AttackImpact(currentPlan)
		}
	} else {
		currentPlan, err = zonenpc.PlanAttack(
			currentNPC, currentTarget.ObjectID, currentTarget.Position,
			currentTarget.FootprintRadius,
		)
	}
	if err != nil {
		runtime.registry.mutex.Unlock()
		if errors.Is(err, zonenpc.ErrControlTargetOutOfRange) {
			return nil, nil
		}
		return e.fail("enemyAttackPlan", err)
	}
	if current.zone.NPCRandom() == nil {
		runtime.registry.mutex.Unlock()
		return e.fail("enemyAttackRandom", errors.New("unavailable"))
	}
	result, err := zonenpc.CommitAttack(
		current.zone.NPCRandom(), currentPlan,
		currentNPC.Plan.NPCProfile.CriticalRating, runtime.program.Critical,
	)
	if err != nil {
		runtime.registry.mutex.Unlock()
		return e.fail("enemyAttackCommit", err)
	}
	packets, statDelta, reflection, isApplied, err := runtime.applyEnemyAttackDamage(
		&current, req.generation, currentPlan, result, req.timestamp,
	)
	if err != nil {
		runtime.registry.mutex.Unlock()
		return e.fail("enemyAttackDamage", err)
	}
	runtime.reserveSageCompanionRespawnLocked(
		&current, req.sessionKey, req.generation, currentPlan.TargetObjectID,
	)
	if !isApplied {
		runtime.registry.mutex.Unlock()
		if impactPacket != nil {
			packets = append(packets, impactPacket)
		}
		// A resisted or fully blocked hit still carries combat feedback.
		// Only damage-dependent reactions below require an applied hit.
		return packets, nil
	}
	var voltroidEffectPacket []byte
	if currentPlan.Profile.AbilityName == "CitadelDischarge" {
		delete(current.campaignNPCVoltroidCharges, currentPlan.SourceObjectID)
		delete(current.campaignNPCVoltroidChargeExpires, currentPlan.SourceObjectID)
		effectSlot, isEffectFound :=
			current.campaignNPCVoltroidEffectSlots[currentPlan.SourceObjectID]
		delete(current.campaignNPCVoltroidEffectSlots, currentPlan.SourceObjectID)
		if isEffectFound && runtime.effectPool.Release(
			currentPlan.SourceObjectID, effectSlot,
		) {
			voltroidEffectPacket, err = npcraknet.VoltroidEffect(
				currentPlan.SourceObjectID, 0, effectSlot, "", true,
			)
			if err != nil {
				runtime.logger.Printf(
					"RakNet Voltroid discharge cleanup omitted object=%d: %v",
					currentPlan.SourceObjectID, err,
				)
				voltroidEffectPacket = nil
			}
		}
	}
	if voltroidEffectPacket != nil {
		packets = append([][]byte{voltroidEffectPacket}, packets...)
	}
	if impactPacket != nil {
		packets = append([][]byte{impactPacket}, packets...)
	}
	if isApplied && campaignDifficultyNounFamily(currentNPC.Plan.NounName) ==
		"cryosbasicmelee" {
		chainProfile := currentPlan.Profile.Clone()
		chainProfile.ModifierName = ""
		chainProfile.ModifierDuration = 0
		chainProfile.ModifierDamageBuff = 0
		chainProfile.ModifierMaximumStack = 0
		chainProfile.MinimumDamage *= 0.5
		chainProfile.MaximumDamage *= 0.5
		chainOrigin := currentTarget.Position
		hitObjectIDs := map[uint32]bool{currentTarget.ObjectID: true}
		for jumpIndex := 0; jumpIndex < 2; jumpIndex++ {
			chainTarget := zone.NPCTarget{}
			isChainTargetFound := false
			for _, candidate := range current.zone.LiveNPCTargets() {
				if hitObjectIDs[candidate.ObjectID] || candidate.HitPoint <= 0 ||
					zonegeometry.Distance(chainOrigin, candidate.Position) > 5 {
					continue
				}
				chainTarget = candidate
				isChainTargetFound = true
				break
			}
			if !isChainTargetFound {
				break
			}
			chainPlan, chainErr := zonenpc.PlanAreaAttackWithProfile(
				currentNPC, chainTarget.ObjectID, chainTarget.Position, chainProfile,
			)
			if chainErr != nil {
				break
			}
			chainResult, chainErr := zonenpc.CommitAttack(
				current.zone.NPCRandom(), chainPlan,
				currentNPC.Plan.NPCProfile.CriticalRating, runtime.program.Critical,
			)
			if chainErr != nil {
				runtime.registry.mutex.Unlock()
				return e.fail("enemyArcCommit", chainErr)
			}
			chainPackets, chainStatDelta, _, chainErr :=
				runtime.applyEnemyAreaAttackDamage(
					&current, req.generation, chainPlan, chainResult, req.timestamp,
				)
			if chainErr != nil {
				runtime.registry.mutex.Unlock()
				return e.fail("enemyArcDamage", chainErr)
			}
			runtime.reserveSageCompanionRespawnLocked(
				&current, req.sessionKey, req.generation, chainPlan.TargetObjectID,
			)
			packets = append(packets, chainPackets...)
			statDelta.PVEDamageTaken += chainStatDelta.PVEDamageTaken
			hitObjectIDs[chainTarget.ObjectID] = true
			chainOrigin = chainTarget.Position
			chainProfile.MinimumDamage *= 0.5
			chainProfile.MaximumDamage *= 0.5
		}
	}
	isForcedMovement := req.kind == campaignNPCAttackChargeFollowup ||
		currentPlan.Profile.AbilityName == "ZelemChargeupDischargeAttack" ||
		currentPlan.Profile.AbilityName == "NomadShielderBash" ||
		currentPlan.Profile.AbilityName == "CryosBossMelee"
	if isForcedMovement && current.deployedHitPoint() > 0 {
		forcedPackets, forcedErr := runtime.applyEnemyForcedMovement(
			&current, currentPlan, currentTarget, req.timestamp,
		)
		if forcedErr != nil {
			runtime.logger.Printf(
				"RakNet campaign NPC forced movement omitted object=%d: %v",
				req.objectID, forcedErr,
			)
		} else {
			packets = append(packets, forcedPackets...)
		}
	}
	liveTarget, isLiveTargetFound := current.campaignNPCTarget(
		req.generation, currentPlan.TargetObjectID,
	)
	isDamageOverTimeApplied := isLiveTargetFound && liveTarget.HitPoint > 0 &&
		liveTarget.HitPoint < currentTarget.HitPoint &&
		isCampaignNPCDamageOverTimeProfile(currentPlan.Profile)
	isPhysicalVulnerabilityApplied :=
		currentPlan.Profile.ModifierName == "PhysicalVulnerabilityModifier" ||
			currentPlan.Profile.ModifierName ==
				"ScaldronBasicDog_VulnerabilityModifier"
	isTimedModifierApplied := !isDamageOverTimeApplied &&
		!isPhysicalVulnerabilityApplied &&
		currentPlan.Profile.ModifierName != "" &&
		currentPlan.Profile.ModifierDuration > 0
	if isTimedModifierApplied && currentPlan.Profile.ModifierChance > 0 {
		draw := current.zone.NPCRandom().Float64() * 100
		isTimedModifierApplied = draw < float64(currentPlan.Profile.ModifierChance)
	}
	isEnergyVulnerabilityApplied := isLiveTargetFound && liveTarget.HitPoint > 0 &&
		currentPlan.Profile.ModifierName == "CryosBasicLightningDebuff"
	isNomadScopeDebuffApplied := isLiveTargetFound && liveTarget.HitPoint > 0 &&
		currentPlan.Profile.AbilityName == "NomadScope_BurningDebuff"
	binding := current.binding
	runtime.registry.sessions[req.sessionKey] = current
	runtime.registry.mutex.Unlock()
	reflectionPackets, reflectionErr := runtime.publishThornBarkReflection(
		req.packet, req.sessionKey, req.generation, req.timestamp, binding,
		reflection,
	)
	if reflectionErr != nil {
		runtime.logger.Printf(
			"RakNet campaign Thorn Bark reflection omitted object=%d: %v",
			req.objectID, reflectionErr,
		)
	} else {
		packets = append(packets, reflectionPackets...)
	}
	err = runtime.stats.Record(context.Background(), binding, statDelta)
	if err != nil {
		runtime.logger.Printf(
			"RakNet campaign NPC attack stats omitted object=%d: %v",
			req.objectID, err,
		)
	}
	if isDamageOverTimeApplied {
		modifierPackets, modifierErr := runtime.applyCampaignNPCPoison(
			req.packet, req.sessionKey, req.generation, currentPlan, req.timestamp,
		)
		if modifierErr != nil {
			runtime.logger.Printf(
				"RakNet campaign NPC damage-over-time omitted object=%d: %v",
				req.objectID, modifierErr,
			)
		} else {
			packets = append(packets, modifierPackets...)
		}
	}
	if isTimedModifierApplied {
		modifierPackets, modifierErr := runtime.applyCampaignNPCTimedModifier(
			req.packet, req.sessionKey, req.generation, currentPlan, req.timestamp,
		)
		if modifierErr != nil {
			runtime.logger.Printf(
				"RakNet campaign NPC timed modifier omitted object=%d: %v",
				req.objectID, modifierErr,
			)
		} else {
			packets = append(packets, modifierPackets...)
		}
	}
	if isPhysicalVulnerabilityApplied {
		modifierPackets, modifierErr :=
			runtime.applyCampaignNPCPhysicalVulnerability(
				req.packet, req.sessionKey, req.generation,
				currentPlan, req.timestamp,
			)
		if modifierErr != nil {
			runtime.logger.Printf(
				"RakNet campaign NPC physical vulnerability omitted object=%d: %v",
				req.objectID, modifierErr,
			)
		} else {
			packets = append(packets, modifierPackets...)
		}
	}
	if isEnergyVulnerabilityApplied {
		modifierPackets, modifierErr :=
			runtime.stackCampaignNPCEnergyVulnerability(
				req.sessionKey, req.generation, currentPlan, req.timestamp,
			)
		if modifierErr != nil {
			runtime.logger.Printf(
				"RakNet campaign NPC energy vulnerability omitted object=%d: %v",
				req.objectID, modifierErr,
			)
		} else {
			packets = append(packets, modifierPackets...)
		}
	}
	if isNomadScopeDebuffApplied {
		vulnerabilityPlan := currentPlan
		vulnerabilityPlan.Profile.ModifierName = "CryosBasicLightningDebuff"
		vulnerabilityPlan.Profile.ModifierDamageBuff = 0.10
		vulnerabilityPlan.Profile.ModifierMaximumStack = 3
		modifierPackets, modifierErr :=
			runtime.stackCampaignNPCEnergyVulnerability(
				req.sessionKey, req.generation, vulnerabilityPlan, req.timestamp,
			)
		if modifierErr != nil {
			runtime.logger.Printf(
				"RakNet campaign NPC scope vulnerability omitted object=%d: %v",
				req.objectID, modifierErr,
			)
		} else {
			packets = append(packets, modifierPackets...)
		}
		buffPacket, buffErr := npcraknet.PositionedEffect(
			"nomad_lieu_el_2_scope_fire_thorns_hit_effect.ServerEventDef",
			currentPlan.SourcePosition,
		)
		if buffErr != nil {
			runtime.logger.Printf(
				"RakNet campaign NPC scope effect omitted object=%d: %v",
				req.objectID, buffErr,
			)
		} else {
			packets = append(packets, buffPacket)
		}
	}
	return packets, nil
}

func (e campaignNPCAttackSchedule) next() ([][]byte, error) {
	e.request.runtime.registry.mutex.RLock()
	current, isFound := e.request.runtime.registry.sessions[e.request.sessionKey]
	isCurrent := isFound && current.isCampaignNPCSourceGenerationActive(
		e.request.generation, e.request.objectID, e.plan.ActionGeneration,
	)
	e.request.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	timeline, err := zonenpc.TimelineForAttack(e.plan)
	if err != nil {
		return e.fail("enemyAttackTimeline", err)
	}
	timestamp := e.request.timestamp + uint64(timeline.NextDelay/time.Millisecond)
	if strings.HasPrefix(e.plan.Profile.AbilityName, "ScaldronBoss_Melee") {
		step := campaignNPCFirstActionStep{
			runtime: e.request.runtime, packet: e.request.packet,
			sessionKey: e.request.sessionKey, generation: e.request.generation,
			objectID: e.request.objectID, actionGeneration: e.plan.ActionGeneration,
			timestamp: timestamp,
		}
		return step.produce()
	}
	if e.plan.Profile.AbilityName == "DrainerMelee" {
		packets, produceErr := e.request.runtime.produceManaDrain(
			e.request.packet, e.request.sessionKey, e.request.generation,
			e.request.objectID, timestamp,
		)
		if produceErr != nil {
			return e.fail("enemyAttackNextDrain", produceErr)
		}
		return packets, nil
	}
	if e.plan.Profile.AbilityName == "NomadDrag_Meteor" {
		timestamp = e.request.timestamp + uint64(e.plan.Profile.ReleaseDelay/time.Millisecond)
		packets, produceErr := e.request.runtime.produceNomadDragPostMeteorTaunt(
			e.request.packet, e.request.sessionKey, e.request.generation,
			e.request.objectID, e.plan.ActionGeneration, timestamp,
			e.plan.Profile, e.request.resume,
		)
		if produceErr != nil {
			return e.fail("enemyAttackNextDragTaunt", produceErr)
		}
		return packets, nil
	}
	packets, produceErr := e.request.resume(timestamp)
	if produceErr != nil {
		return e.fail("enemyAttackNext", produceErr)
	}
	return packets, nil
}

type campaignNPCFirstActionStep struct {
	runtime          campaignNPCActionRuntime
	packet           raknet.Packet
	sessionKey       string
	generation       uint64
	actionGeneration uint64
	objectID         uint32
	timestamp        uint64
	isDelayedReveal  bool
}

type campaignNPCFirstAggroRevealStep struct {
	runtime          campaignNPCActionRuntime
	sessionKey       string
	generation       uint64
	actionGeneration uint64
	objectID         uint32
	timestamp        uint64
}

type campaignNPCFirstAggroEffectStep struct {
	runtime          campaignNPCActionRuntime
	sessionKey       string
	generation       uint64
	actionGeneration uint64
	objectID         uint32
}

func (e campaignNPCFirstAggroEffectStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		!peerSession.isZoneTerminal() && peerSession.zone != nil &&
		peerSession.zone.NPCs() != nil
	if !isCurrent {
		e.runtime.registry.mutex.RUnlock()
		return nil, nil
	}
	npc, isNPCFound := peerSession.zone.NPCs().NPC(e.objectID)
	e.runtime.registry.mutex.RUnlock()
	owner := zonenpc.ActionOwner{
		UserID: peerSession.binding.UserID, PeerGeneration: e.generation,
	}
	if !isNPCFound || npc.IsDefeated || !npc.IsActionStarted ||
		npc.ActionOwner != owner ||
		npc.ActionGeneration != e.actionGeneration {
		return nil, nil
	}
	profile, isProfileFound := zonenpc.ActionProfileForPlan(npc.Plan)
	if !isProfileFound || profile.FirstAggroEffectName == "" ||
		profile.FirstAggroEffectDelay <= 0 {
		return nil, nil
	}
	plan := zonenpc.FirstActionPlan{
		ObjectID: e.objectID, TargetObjectID: npc.TargetObjectID,
		ActionGeneration: e.actionGeneration,
		SourcePosition:   npc.Plan.Position, Profile: profile,
	}
	packets, err := npcraknet.FirstAggroEffect(plan)
	if err != nil {
		return nil, fmt.Errorf("enemyFirstAggroEffect: %w", err)
	}
	return packets, nil
}

func (e campaignNPCFirstAggroRevealStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		!peerSession.isZoneTerminal() && peerSession.zone != nil &&
		peerSession.zone.NPCs() != nil
	if !isCurrent {
		e.runtime.registry.mutex.RUnlock()
		return nil, nil
	}
	npc, isNPCFound := peerSession.zone.NPCs().NPC(e.objectID)
	e.runtime.registry.mutex.RUnlock()
	owner := zonenpc.ActionOwner{
		UserID: peerSession.binding.UserID, PeerGeneration: e.generation,
	}
	if !isNPCFound || npc.IsDefeated || !npc.IsActionStarted ||
		npc.ActionOwner != owner ||
		npc.ActionGeneration != e.actionGeneration {
		return nil, nil
	}
	profile, isProfileFound := zonenpc.ActionProfileForPlan(npc.Plan)
	if !isProfileFound || profile.FirstAggroRevealDelay <= 0 {
		return nil, nil
	}
	plan := zonenpc.FirstActionPlan{
		ObjectID: e.objectID, TargetObjectID: npc.TargetObjectID,
		ActionGeneration: e.actionGeneration,
		SourcePosition:   npc.Plan.Position, Profile: profile,
	}
	packets, err := npcraknet.FirstAggroReveal(plan, e.timestamp)
	if err != nil {
		return nil, fmt.Errorf("enemyFirstAggroReveal: %w", err)
	}
	e.runtime.logger.Printf(
		"RakNet campaign enemy first-aggro reveal object=%d timestamp=%d",
		e.objectID, e.timestamp,
	)
	return packets, nil
}

func (e campaignNPCFirstActionStep) droneArrival(
	timestamp uint64,
) ([][]byte, error) {
	dronePackets, err := e.runtime.ensureInvincitronDrone(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyDroneSpawn: %w", err)
	}
	punchPackets, err := e.runtime.produceDronePunch(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyDronePunch: %w", err)
	}
	return append(dronePackets, punchPackets...), nil
}

func (e campaignNPCFirstActionStep) snipeArrival(
	timestamp uint64,
) ([][]byte, error) {
	return e.runtime.produceSnipeSlow(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
}

func (e campaignNPCFirstActionStep) rangedArrival(
	timestamp uint64,
) ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	current, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || current.zone == nil || current.zone.NPCs() == nil {
		e.runtime.registry.mutex.RUnlock()
		return nil, nil
	}
	npc, isNPCFound := current.zone.NPCs().NPC(e.objectID)
	e.runtime.registry.mutex.RUnlock()
	if !isNPCFound {
		return nil, nil
	}
	profile, isProfileFound := zonenpc.ActionProfileForPlan(npc.Plan)
	if !isProfileFound {
		return nil, nil
	}
	if profile.TeleportNormalDistance <= 0 {
		return e.runtime.produceZelemShot(
			e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
		)
	}
	return e.runtime.produceZelemBlink(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
}

func (e campaignNPCFirstActionStep) projectileArrival(
	timestamp uint64,
) ([][]byte, error) {
	err := e.runtime.syncInvincitronDroneTarget(
		e.sessionKey, e.generation, e.objectID,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyProjectileTarget: %w", err)
	}
	return e.runtime.produceZelemShot(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
}

func (e campaignNPCFirstActionStep) plungeArrival(
	timestamp uint64,
) ([][]byte, error) {
	return e.runtime.producePlunge(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
}

func (e campaignNPCFirstActionStep) pushPullArrival(
	timestamp uint64,
) ([][]byte, error) {
	return e.runtime.producePushPull(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
}

func (e campaignNPCFirstActionStep) channelDrainArrival(
	timestamp uint64,
) ([][]byte, error) {
	return e.runtime.produceChannelDrain(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
}

func (e campaignNPCFirstActionStep) resurrectArrival(
	timestamp uint64,
) ([][]byte, error) {
	return e.runtime.produceResurrection(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
}

func (e campaignNPCFirstActionStep) chargeArrival(
	timestamp uint64,
) ([][]byte, error) {
	return e.runtime.produceEnemyCharge(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
}

func (e campaignNPCFirstActionStep) coneArrival(
	timestamp uint64,
) ([][]byte, error) {
	return e.runtime.produceEnemyCone(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
}

func (e campaignNPCFirstActionStep) leapArrival(
	timestamp uint64,
) ([][]byte, error) {
	return e.runtime.produceEnemyLeap(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
}

func (e campaignNPCFirstActionStep) meleeArrival(
	timestamp uint64,
) ([][]byte, error) {
	return e.runtime.produceEnemyMelee(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
}

func (e campaignNPCFirstActionStep) detonateArrival(
	timestamp uint64,
) ([][]byte, error) {
	return e.runtime.produceExploderScarab(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
}

func (e campaignNPCFirstActionStep) produceArrival(
	family zonenpc.ActionFamily, timestamp uint64,
) ([][]byte, error) {
	switch family {
	case zonenpc.ActionNomadDrone:
		return e.droneArrival(timestamp)
	case zonenpc.ActionNomadSnipe:
		return e.snipeArrival(timestamp)
	case zonenpc.ActionZelemHaster:
		return e.runtime.produceHasterBuff(
			e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
		)
	case zonenpc.ActionZelemRanged:
		return e.rangedArrival(timestamp)
	case zonenpc.ActionProjectile:
		return e.projectileArrival(timestamp)
	case zonenpc.ActionRetainedArea:
		return e.plungeArrival(timestamp)
	case zonenpc.ActionPushPull:
		return e.pushPullArrival(timestamp)
	case zonenpc.ActionChannelDrain:
		return e.channelDrainArrival(timestamp)
	case zonenpc.ActionResurrect:
		return e.resurrectArrival(timestamp)
	case zonenpc.ActionCharge:
		return e.chargeArrival(timestamp)
	case zonenpc.ActionCone:
		return e.coneArrival(timestamp)
	case zonenpc.ActionLeap:
		return e.leapArrival(timestamp)
	case zonenpc.ActionMelee:
		return e.meleeArrival(timestamp)
	case zonenpc.ActionDetonate:
		return e.detonateArrival(timestamp)
	default:
		return nil, nil
	}
}

func (e campaignNPCFirstActionStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	latest, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && latest.generation == e.generation &&
		!latest.isZoneTerminal() && latest.zone.NPCs() != nil &&
		(latest.zone.Boss() == nil || !latest.zone.Boss().IsBeamOutCommitted())
	if !isCurrent {
		e.runtime.registry.mutex.RUnlock()
		return nil, nil
	}
	npc, isNPCFound := latest.zone.NPCs().NPC(e.objectID)
	target, isTargetFound := latest.campaignNPCTarget(
		e.generation, npc.TargetObjectID,
	)
	graspingDeadSpeed := graspingDeadMovementSpeed(
		latest, npc.Plan.Position, 1,
	)
	slowMovementScale := latest.zone.NPCs().SlowMovementScale(
		e.objectID, e.runtime.now(),
	)
	e.runtime.registry.mutex.RUnlock()
	if !isNPCFound || npc.IsDefeated ||
		npc.ActionGeneration != e.actionGeneration {
		return nil, nil
	}
	if !isTargetFound {
		e.releaseAction(latest)
		return nil, nil
	}
	if zonenpc.IsCorruptorNoun(npc.Plan.NounName) {
		randomRoll := 0.5
		if latest.zone.NPCRandom() != nil {
			randomRoll = latest.zone.NPCRandom().Float64()
		}
		npc, isNPCFound = latest.zone.NPCs().SelectCorruptorAction(zonenpc.CorruptorActionRequest{
			ObjectID: e.objectID, ActionGeneration: e.actionGeneration,
			Timestamp: e.timestamp, TargetPosition: target.Position,
			RandomRoll: randomRoll,
		})
		if !isNPCFound {
			e.releaseAction(latest)
			return nil, nil
		}
	}
	repairPackets, isRepairHandled, repairErr := e.runtime.produceZelemBasicRepair(
		e.packet, e.sessionKey, e.generation, e.objectID, e.timestamp,
	)
	if repairErr != nil {
		e.releaseAction(latest)
		return nil, fmt.Errorf("enemyFirstRepair: %w", repairErr)
	}
	if isRepairHandled {
		return repairPackets, nil
	}
	action, isActionFound, err := campaignNPCFirstAction(
		npc.Plan, target.ObjectID, target.Position, target.FootprintRadius,
	)
	if err != nil {
		e.releaseAction(latest)
		return nil, fmt.Errorf("enemyPursuitAction: %w", err)
	}
	if !isActionFound {
		e.releaseAction(latest)
		return nil, nil
	}
	action.ActionGeneration = e.actionGeneration
	action.Profile.MovementSpeed *= graspingDeadSpeed * slowMovementScale
	actionTimestamp := e.timestamp +
		uint64(action.Profile.FirstAggroDelay/time.Millisecond)
	activationPackets := make([][]byte, 0)
	if e.isDelayedReveal {
		activationPackets, err = npcraknet.FirstAggroActivate(action)
		if err != nil {
			e.releaseAction(latest)
			return nil, fmt.Errorf("enemyFirstActivate: %w", err)
		}
		e.runtime.logger.Printf(
			"RakNet campaign enemy first-aggro activation object=%d timestamp=%d position=(%.3f,%.3f,%.3f) target=%d",
			e.objectID, actionTimestamp,
			action.SourcePosition.X, action.SourcePosition.Y,
			action.SourcePosition.Z, action.TargetObjectID,
		)
	}
	if action.IsPursuitNeeded && action.Profile.MovementSpeed <= 0 {
		e.releaseAction(latest)
		return nil, nil
	}
	if !action.IsPursuitNeeded {
		e.runtime.logger.Printf(
			"RakNet campaign enemy remains in attack range object=%d noun=%q source=(%.3f,%.3f,%.3f) target=%d position=(%.3f,%.3f,%.3f) range=%.3f",
			e.objectID, npc.Plan.NounName,
			action.SourcePosition.X, action.SourcePosition.Y, action.SourcePosition.Z,
			action.TargetObjectID,
			action.TargetPosition.X, action.TargetPosition.Y, action.TargetPosition.Z,
			action.Profile.Range,
		)
		packets, arrivalErr := e.produceArrival(
			action.Profile.Family, actionTimestamp,
		)
		if arrivalErr != nil {
			e.releaseAction(latest)
			return nil, fmt.Errorf("enemyFirstArrival: %w", arrivalErr)
		}
		stopPackets, stopErr := npcraknet.MovementStop(
			e.objectID, action.SourcePosition,
		)
		if stopErr != nil {
			e.releaseAction(latest)
			return nil, fmt.Errorf("enemyFirstStop: %w", stopErr)
		}
		packets = append(stopPackets, packets...)
		packets = append(activationPackets, packets...)
		// The committed producer forwards the actual cast presentation to peers.
		return packets, nil
	}
	var arrival func(uint64) ([][]byte, error)
	switch action.Profile.Family {
	case zonenpc.ActionNomadDrone:
		arrival = e.droneArrival
	case zonenpc.ActionNomadSnipe:
		arrival = e.snipeArrival
	case zonenpc.ActionZelemRanged:
		arrival = e.rangedArrival
	case zonenpc.ActionProjectile:
		arrival = e.projectileArrival
	case zonenpc.ActionRetainedArea:
		arrival = e.plungeArrival
	case zonenpc.ActionPushPull:
		arrival = e.pushPullArrival
	case zonenpc.ActionChannelDrain:
		arrival = e.channelDrainArrival
	case zonenpc.ActionResurrect:
		arrival = e.resurrectArrival
	case zonenpc.ActionCharge:
		arrival = e.chargeArrival
	case zonenpc.ActionCone:
		arrival = e.coneArrival
	case zonenpc.ActionLeap:
		arrival = e.leapArrival
	case zonenpc.ActionMelee:
		arrival = e.meleeArrival
	case zonenpc.ActionDetonate:
		arrival = e.detonateArrival
	}
	if arrival == nil {
		e.releaseAction(latest)
		return nil, fmt.Errorf(
			"enemyPursuitFamily[%d]: arrival unavailable",
			action.Profile.Family,
		)
	}
	pursuitGoalFlags := uint8(0x41)
	if action.Profile.Family == zonenpc.ActionProjectile {
		pursuitGoalFlags = 0x01
	}
	e.runtime.logger.Printf(
		"RakNet campaign enemy pursuit starting object=%d noun=%q source=(%.3f,%.3f,%.3f) target=%d position=(%.3f,%.3f,%.3f) range=%.3f speed=%.3f flags=0x%02x",
		e.objectID, npc.Plan.NounName,
		action.SourcePosition.X, action.SourcePosition.Y, action.SourcePosition.Z,
		action.TargetObjectID,
		action.TargetPosition.X, action.TargetPosition.Y, action.TargetPosition.Z,
		action.Profile.Range, action.Profile.MovementSpeed, pursuitGoalFlags,
	)
	err = e.runtime.pursuit.schedule(
		e.packet, e.sessionKey, e.generation, e.objectID,
		actionTimestamp, action.TargetPosition, action.Profile, arrival,
	)
	if err != nil {
		e.releaseAction(latest)
		e.runtime.logger.Printf(
			"RakNet campaign first pursuit not scheduled object=%d: %v",
			e.objectID, err,
		)
		return nil, nil
	}
	pursuitPacket, err := npcraknet.Pursuit(action)
	if err != nil {
		e.releaseAction(latest)
		return nil, fmt.Errorf("enemyPursuitMarshal: %w", err)
	}
	return append(activationPackets, pursuitPacket...), nil
}

func (e campaignNPCFirstActionStep) releaseAction(
	peerSession gameplayPeerSession,
) {
	if peerSession.zone == nil || peerSession.zone.NPCs() == nil {
		return
	}
	peerSession.zone.NPCs().ReleaseActionGeneration(
		e.objectID, zonenpc.ActionOwner{
			UserID: peerSession.binding.UserID, PeerGeneration: e.generation,
		}, e.actionGeneration,
	)
}

func (r campaignNPCActionRuntime) produceDronePunch(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, error) {
	orcusPackets, isOrcusHandled, orcusErr := r.produceOrcusSpawn(
		packet, sessionKey, generation, objectID, timestamp,
	)
	if orcusErr != nil {
		return nil, fmt.Errorf("enemyPunchOrcus: %w", orcusErr)
	}
	if isOrcusHandled {
		return orcusPackets, nil
	}
	orcusPackets, isOrcusHandled, orcusErr = r.produceOrcusAbility(
		packet, sessionKey, generation, objectID, timestamp,
	)
	if orcusErr != nil {
		return nil, fmt.Errorf("enemyPunchOrcusAbility: %w", orcusErr)
	}
	if isOrcusHandled {
		return orcusPackets, nil
	}
	repairPackets, isRepairHandled, repairErr := r.produceZelemBasicRepair(
		packet, sessionKey, generation, objectID, timestamp,
	)
	if repairErr != nil {
		return nil, fmt.Errorf("enemyPunchRepair: %w", repairErr)
	}
	if isRepairHandled {
		return repairPackets, nil
	}
	request := campaignNPCAttackRequest{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
		kind: campaignNPCAttackDronePunch,
	}
	isDeferred, err := r.pursuit.deferAction(
		packet, sessionKey, generation, objectID, timestamp,
		request.resume,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyPunchStun: %w", err)
	}
	if isDeferred {
		return nil, nil
	}
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound &&
		peerSession.isCampaignNPCSourceActive(generation, objectID) &&
		peerSession.zone.NPCs() != nil
	if !isCurrent {
		r.registry.mutex.RUnlock()
		return nil, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, enemy.TargetObjectID,
	)
	r.registry.mutex.RUnlock()
	if !isEnemyFound || enemy.IsDefeated || !isTargetFound {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	if enemy.IsShieldActive {
		const shieldPollDelay = 250 * time.Millisecond
		request.timestamp = timestamp + uint64(shieldPollDelay/time.Millisecond)
		producers := []raknet.ScheduledPacketProducer{{
			Delay: shieldPollDelay, Produce: request.produce,
		}}
		producers = r.registry.producerGuard.scheduledProducers(sessionKey, producers)
		cancel, scheduleErr := packet.ScheduleProducers(producers)
		if scheduleErr == nil && cancel == nil {
			scheduleErr = errors.New("nil cancellation")
		}
		if scheduleErr != nil {
			r.releaseAction(sessionKey, generation, objectID)
			return nil, fmt.Errorf("enemyPunchShieldSchedule: %w", scheduleErr)
		}
		return nil, nil
	}
	orcusMeleeProfile, isOrcus := zonenpc.OrcusFallbackProfile(enemy.Plan.NounName)
	if isOrcus {
		enemy.Plan.ActionProfile = orcusMeleeProfile
		enemy.Plan.IsActionKnown = true
	}
	attackPlan, err := zonenpc.PlanAttack(
		enemy, target.ObjectID, target.Position, target.FootprintRadius,
	)
	if err != nil {
		action, isActionFound, actionErr := campaignNPCFirstAction(
			enemy.Plan, target.ObjectID, target.Position, target.FootprintRadius,
		)
		if actionErr != nil {
			return nil, fmt.Errorf("enemyPunchPursuitAction: %w", actionErr)
		}
		if !isActionFound || !action.IsPursuitNeeded {
			return nil, nil
		}
		pursuitPackets, marshalErr := npcraknet.Pursuit(action)
		if marshalErr != nil {
			return nil, fmt.Errorf("enemyPunchPursuitMarshal: %w", marshalErr)
		}
		scheduleErr := r.pursuit.schedule(
			packet, sessionKey, generation, objectID, timestamp,
			action.TargetPosition, action.Profile, request.resume,
		)
		if scheduleErr != nil {
			r.releaseAction(sessionKey, generation, objectID)
			r.logger.Printf("RakNet campaign enemy pursuit not scheduled object=%d: %v", objectID, scheduleErr)
		}
		return pursuitPackets, nil
	}
	revealPackets := make([][]byte, 0)
	if enemy.IsSpawnStealthActive && attackPlan.Profile.RevealEffectName != "" {
		r.registry.mutex.Lock()
		currentSession, isCurrentFound := r.registry.sessions[sessionKey]
		isCurrent = isCurrentFound &&
			currentSession.isCampaignNPCSourceActive(generation, objectID) &&
			currentSession.zone.NPCs() != nil
		isRevealed := false
		if isCurrent {
			isRevealed, err = currentSession.zone.NPCs().RevealSpawnStealth(objectID)
			if err == nil && isRevealed {
				r.registry.sessions[sessionKey] = currentSession
			}
		}
		r.registry.mutex.Unlock()
		if err != nil {
			return nil, fmt.Errorf("enemyPunchStealth: %w", err)
		}
		if !isCurrent {
			return nil, nil
		}
		if isRevealed {
			revealPackets, err = npcraknet.StealthReveal(
				objectID, enemy.Plan.Position, attackPlan.Profile.RevealEffectName,
			)
			if err != nil {
				return nil, fmt.Errorf("enemyPunchReveal: %w", err)
			}
		}
	}
	startPackets, err := r.startNPCAttack(sessionKey, generation, attackPlan, timestamp)
	if err != nil {
		return nil, fmt.Errorf("enemyPunchStart: %w", err)
	}
	timeline, err := zonenpc.TimelineForAttack(attackPlan)
	if err != nil {
		return nil, fmt.Errorf("enemyPunchTimeline: %w", err)
	}
	schedule := campaignNPCAttackSchedule{request: request, plan: attackPlan}
	hitProducer := raknet.ScheduledPacketProducer{
		Delay: timeline.HitDelay, Produce: schedule.hit,
	}
	nextProducer := raknet.ScheduledPacketProducer{
		Delay: timeline.NextDelay, Produce: schedule.next,
	}
	producers := []raknet.ScheduledPacketProducer{hitProducer, nextProducer}
	var voltroidEffectPacket []byte
	voltroidEffectSlot := uint8(0)
	isVoltroidEffectAllocated := false
	if attackPlan.Profile.AbilityName == "CitadelDischarge" &&
		attackPlan.Profile.TrailEffectName != "" {
		voltroidEffectSlot, isVoltroidEffectAllocated =
			r.effectPool.Allocate(objectID)
		if isVoltroidEffectAllocated {
			voltroidEffectPacket, err = npcraknet.VoltroidEffect(
				objectID, attackPlan.TargetObjectID, voltroidEffectSlot,
				attackPlan.Profile.TrailEffectName, false,
			)
			if err != nil {
				isEffectReleased := r.effectPool.Release(
					objectID, voltroidEffectSlot,
				)
				if !isEffectReleased {
					r.logger.Printf(
						"RakNet Voltroid discharge slot already released object=%d slot=%d",
						objectID, voltroidEffectSlot,
					)
				}
				return nil, fmt.Errorf("enemyPunchVoltroidEffect: %w", err)
			}
			cleanup := campaignVoltroidVisualCleanupStep{
				runtime: r, objectID: objectID,
				effectSlot: voltroidEffectSlot,
			}
			producers = append(producers, raknet.ScheduledPacketProducer{
				Delay:   attackPlan.Profile.ReleaseDelay,
				Produce: cleanup.produce,
			})
		}
	}
	producers = r.registry.producerGuard.scheduledProducers(sessionKey, producers)
	cancel, scheduleErr := packet.ScheduleProducers(producers)
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		if isVoltroidEffectAllocated {
			isEffectReleased := r.effectPool.Release(
				objectID, voltroidEffectSlot,
			)
			if !isEffectReleased {
				r.logger.Printf(
					"RakNet Voltroid discharge cleanup slot already released object=%d slot=%d",
					objectID, voltroidEffectSlot,
				)
			}
			voltroidEffectPacket = nil
		}
		r.releaseAction(sessionKey, generation, objectID)
		r.logger.Printf("RakNet campaign enemy punch continuation not scheduled object=%d: %v", objectID, scheduleErr)
	}
	packets := append(revealPackets, startPackets...)
	if voltroidEffectPacket != nil {
		packets = append(packets, voltroidEffectPacket)
	}
	return packets, nil
}

func (r campaignNPCActionRuntime) producePlunge(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, error) {
	request := campaignNPCAttackRequest{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
		kind: campaignNPCAttackPlunge,
	}
	isDeferred, err := r.pursuit.deferAction(
		packet, sessionKey, generation, objectID, timestamp,
		request.resume,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyPlungeStun: %w", err)
	}
	if isDeferred {
		return nil, nil
	}
	posePackets, isPoseHandled, poseErr := r.produceCorruptorPose(
		packet, sessionKey, generation, objectID, timestamp,
	)
	if poseErr != nil {
		return nil, fmt.Errorf("corruptorPose: %w", poseErr)
	}
	if isPoseHandled {
		return posePackets, nil
	}
	gravityPackets, isGravityHandled, gravityErr := r.produceCorruptorGravityOrb(
		packet, sessionKey, generation, objectID, timestamp,
	)
	if gravityErr != nil {
		return nil, fmt.Errorf("enemyPlungeCorruptorGravityOrb: %w", gravityErr)
	}
	if isGravityHandled {
		return gravityPackets, nil
	}
	panicPackets, isPanicHandled, panicErr := r.produceNashiraPanic(
		packet, sessionKey, generation, objectID, timestamp,
	)
	if panicErr != nil {
		return nil, fmt.Errorf("enemyPlungeShadowPanic: %w", panicErr)
	}
	if isPanicHandled {
		return panicPackets, nil
	}
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound &&
		peerSession.isCampaignNPCSourceActive(generation, objectID) &&
		peerSession.zone.NPCs() != nil
	if !isCurrent {
		r.registry.mutex.RUnlock()
		return nil, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, enemy.TargetObjectID,
	)
	r.registry.mutex.RUnlock()
	if !isEnemyFound || enemy.IsDefeated || !isTargetFound {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	dragPackets, isDragHandled, dragErr := r.produceNomadDragPhase(
		packet, sessionKey, generation, enemy, target, timestamp,
	)
	if dragErr != nil {
		return nil, fmt.Errorf("enemyPlungeDrag: %w", dragErr)
	}
	if isDragHandled {
		return dragPackets, nil
	}
	profile, isProfileFound := zonenpc.ActionProfileForPlan(enemy.Plan)
	if !isProfileFound || profile.Family != zonenpc.ActionRetainedArea {
		return nil, nil
	}
	if profile.AbilityName == "NomadDrag_Meteor" {
		request.kind = campaignNPCAttackNomadDragMeteor
	}
	attackPlan, err := zonenpc.PlanAttackWithProfile(
		enemy, target.ObjectID, target.Position, profile,
		target.FootprintRadius,
	)
	if err != nil {
		action, actionErr := campaignNPCActionWithProfile(
			enemy.Plan, target.ObjectID, target.Position,
			profile, target.FootprintRadius,
		)
		if actionErr != nil {
			return nil, fmt.Errorf("enemyPlungePursuitAction: %w", actionErr)
		}
		if !action.IsPursuitNeeded {
			return nil, nil
		}
		pursuitPackets, marshalErr := npcraknet.Pursuit(action)
		if marshalErr != nil {
			return nil, fmt.Errorf("enemyPlungePursuitMarshal: %w", marshalErr)
		}
		scheduleErr := r.pursuit.schedule(
			packet, sessionKey, generation, objectID, timestamp,
			action.TargetPosition, action.Profile, request.resume,
		)
		if scheduleErr != nil {
			r.releaseAction(sessionKey, generation, objectID)
			r.logger.Printf(
				"RakNet campaign enemy plunge pursuit not scheduled object=%d: %v",
				objectID, scheduleErr,
			)
		}
		return pursuitPackets, nil
	}
	startPackets, err := r.startNPCAttack(sessionKey, generation, attackPlan, timestamp)
	if err != nil {
		return nil, fmt.Errorf("enemyPlungeStart: %w", err)
	}
	timeline, err := zonenpc.TimelineForAttack(attackPlan)
	if err != nil {
		return nil, fmt.Errorf("enemyPlungeTimeline: %w", err)
	}
	schedule := campaignNPCAttackSchedule{request: request, plan: attackPlan}
	if request.kind == campaignNPCAttackNomadDragMeteor {
		schedule.meteor = &campaignNPCMeteorRun{position: attackPlan.TargetPosition}
	}
	emergeProducer := raknet.ScheduledPacketProducer{
		Delay: attackPlan.Profile.EmergeDelay, Produce: schedule.emerge,
	}
	hitProducer := raknet.ScheduledPacketProducer{
		Delay: timeline.HitDelay, Produce: schedule.hit,
	}
	nextProducer := raknet.ScheduledPacketProducer{
		Delay: timeline.NextDelay, Produce: schedule.next,
	}
	producers := []raknet.ScheduledPacketProducer{
		emergeProducer, hitProducer, nextProducer,
	}
	if schedule.meteor != nil {
		// Meteor recovery owns the laugh; cooldown readiness must not delay it.
		producers[2].Delay = attackPlan.Profile.ReleaseDelay
		producers = append([]raknet.ScheduledPacketProducer{{
			Delay: nomadDragMeteorTargetDelay, Produce: schedule.sampleNomadDragMeteor,
		}}, producers...)
	}
	_, scheduleErr := scheduleNPCProducers(r.registry, packet, producers)
	if scheduleErr != nil {
		r.releaseAction(sessionKey, generation, objectID)
		r.logger.Printf(
			"RakNet campaign enemy plunge continuation not scheduled object=%d: %v",
			objectID, scheduleErr,
		)
	}
	return startPackets, nil
}

func (r campaignNPCActionRuntime) scheduleFirstActions(
	packet raknet.Packet, sessionKey string, generation uint64,
	plans []zonenpc.SpawnPlan, timestamp uint64,
) ([][]byte, error) {
	return r.scheduleFirstActionsWithIntroductions(
		packet, sessionKey, generation, plans, timestamp, nil,
	)
}

func (r campaignNPCActionRuntime) scheduleFirstActionsWithIntroductions(
	packet raknet.Packet, sessionKey string, generation uint64,
	plans []zonenpc.SpawnPlan, timestamp uint64,
	introductionObjectIDs map[uint32]struct{},
) ([][]byte, error) {
	if len(plans) == 0 {
		return nil, nil
	}
	// NPC actions belong to the zone, not to the player command which happened
	// to discover or aggro them. A later movement admission may cancel the
	// player's prior command, but must not cancel autonomous NPC combat.
	packet = packet.Autonomous()
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		!peerSession.isZoneTerminal() &&
		peerSession.zone.NPCs() != nil &&
		(peerSession.zone.Boss() == nil ||
			!peerSession.zone.Boss().IsBeamOutCommitted())
	userID := peerSession.binding.UserID
	npcSession := peerSession.zone.NPCs()
	zone := peerSession.zone
	r.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	err := r.startCorruptorControllers(
		packet, sessionKey, generation, plans, timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("firstActionCorruptor: %w", err)
	}
	err = r.startArcturusControllers(packet, sessionKey, generation, plans, timestamp)
	if err != nil {
		return nil, fmt.Errorf("firstActionArcturus: %w", err)
	}
	immediatePackets := make([][]byte, 0, len(plans)*4)
	producers := make([]raknet.ScheduledPacketProducer, 0, len(plans)*2)
	aggroEvents := make([]zonenpc.ActionEvent, 0, len(plans))
	owner := zonenpc.ActionOwner{UserID: userID, PeerGeneration: generation}
	startedObjectIDs := make([]uint32, 0, len(plans))
	spawnPresentationObjectIDs := make(map[uint32]struct{}, len(plans))
	spawnPresentationRuns := make(map[uint32]*spawnraknet.Run, len(plans))
	for index, plan := range plans {
		if zonenpc.IsCorruptorNoun(plan.NounName) {
			npc, isNPCFound := npcSession.NPC(plan.ObjectID)
			if isNPCFound && npc.IsFirstActionStarted {
				continue
			}
		}
		if plan.IsFixture {
			continue
		}
		_, isIntroductionRequested := introductionObjectIDs[plan.ObjectID]
		isIntroductionConsumed :=
			plan.Introduction == zonenpc.SpawnIntroductionFloorWarp ||
				plan.Introduction == zonenpc.SpawnIntroductionAmbush
		if isIntroductionConsumed && !isIntroductionRequested {
			continue
		}
		isFloorWarpRequested := isIntroductionRequested &&
			plan.Introduction == zonenpc.SpawnIntroductionFloorWarp
		profile, isProfileFound := zonenpc.ActionProfileForPlan(plan)
		isAuthoredPresentationAvailable := false
		if !isFloorWarpRequested && isProfileFound &&
			profile.FirstAggroAnimationName != "" &&
			isCampaignNPCFirstActionSupported(profile.Family) {
			npc, isNPCFound := npcSession.NPC(plan.ObjectID)
			if isNPCFound && !npc.IsDefeated {
				target, isTargetFound := peerSession.campaignNPCTarget(
					generation, npc.TargetObjectID,
				)
				if isTargetFound {
					_, isActionFound, actionErr := campaignNPCFirstAction(
						plan, target.ObjectID, target.Position, target.FootprintRadius,
					)
					if actionErr != nil {
						return nil, fmt.Errorf("enemySpawnAction[%d]: %w", index, actionErr)
					}
					isAuthoredPresentationAvailable = isActionFound
				}
			}
		}
		if isAuthoredPresentationAvailable {
			continue
		}
		spawnRun, spawnPackets, spawnErr := spawnraknet.New(
			r.program.SpawnModifier,
			spawnraknet.Definition{
				ObjectID: plan.ObjectID,
				Position: sim.Position{
					X: plan.Position.X, Y: plan.Position.Y, Z: plan.Position.Z,
				},
			},
			timestamp,
		)
		if spawnErr != nil {
			for _, current := range spawnPresentationRuns {
				current.Stop()
			}
			return nil, fmt.Errorf("enemySpawnPresentation[%d]: %w", index, spawnErr)
		}
		immediatePackets = append(immediatePackets, spawnPackets...)
		spawnPresentationObjectIDs[plan.ObjectID] = struct{}{}
		spawnPresentationRuns[plan.ObjectID] = spawnRun
		step := campaignNPCSpawnPresentationStep{
			registry: r.registry, sessionKey: sessionKey, generation: generation,
			objectID: plan.ObjectID, run: spawnRun,
		}
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: zonespawn.Duration, Produce: step.produce,
		})
	}
	if len(spawnPresentationRuns) != 0 {
		r.registry.mutex.Lock()
		current, isCurrentFound := r.registry.sessions[sessionKey]
		isCurrentSession := isCurrentFound && current.generation == generation &&
			!current.isZoneTerminal()
		if isCurrentSession {
			if current.spawnModifiers == nil {
				current.spawnModifiers = make(map[uint32]*spawnraknet.Run)
			}
			for objectID, spawnRun := range spawnPresentationRuns {
				if previous := current.spawnModifiers[objectID]; previous != nil {
					previous.Stop()
				}
				current.spawnModifiers[objectID] = spawnRun
			}
			r.registry.sessions[sessionKey] = current
		}
		r.registry.mutex.Unlock()
		if !isCurrentSession {
			for _, spawnRun := range spawnPresentationRuns {
				spawnRun.Stop()
			}
			return nil, nil
		}
	}
	spawnPresentationGuard := campaignNPCSpawnPresentationGuard{
		registry: r.registry, sessionKey: sessionKey, generation: generation,
		runs: spawnPresentationRuns,
	}
	defer spawnPresentationGuard.releaseUnlessCommitted()
	for index, plan := range plans {
		npc, isNPCFound := npcSession.NPC(plan.ObjectID)
		if !isNPCFound || npc.IsDefeated {
			continue
		}
		target, isTargetFound := peerSession.campaignNPCTarget(
			generation, npc.TargetObjectID,
		)
		if !isTargetFound {
			continue
		}
		action, isActionFound, err := campaignNPCFirstAction(
			plan, target.ObjectID, target.Position, target.FootprintRadius,
		)
		if err != nil {
			releaseCampaignNPCFirstActions(npcSession, owner, startedObjectIDs)
			return nil, fmt.Errorf("enemyFirstAction[%d]: %w", index, err)
		}
		if !isActionFound {
			continue
		}
		if action.IsPursuitNeeded && action.Profile.MovementSpeed <= 0 {
			continue
		}
		if !isCampaignNPCFirstActionSupported(action.Profile.Family) {
			continue
		}
		npc, isStarted, isFirstAction, startErr := npcSession.StartAction(
			plan.ObjectID, owner, target.ObjectID,
		)
		if startErr != nil {
			releaseCampaignNPCFirstActions(npcSession, owner, startedObjectIDs)
			return nil, fmt.Errorf("enemyFirstActionStart[%d]: %w", index, startErr)
		}
		if !isStarted {
			continue
		}
		action, isActionFound, err = campaignNPCFirstAction(
			npc.Plan, target.ObjectID, target.Position, target.FootprintRadius,
		)
		if err != nil {
			npcSession.ReleaseAction(plan.ObjectID, owner)
			releaseCampaignNPCFirstActions(npcSession, owner, startedObjectIDs)
			return nil, fmt.Errorf("enemyFirstActionRefresh[%d]: %w", index, err)
		}
		if !isActionFound {
			npcSession.ReleaseAction(plan.ObjectID, owner)
			continue
		}
		action.ActionGeneration = npc.ActionGeneration
		isFloorWarpIntroduction :=
			plan.Introduction == zonenpc.SpawnIntroductionFloorWarp
		firstAggroDelay := time.Duration(0)
		if isFirstAction && !isFloorWarpIntroduction &&
			action.Profile.IsFirstAggroDurationKnown {
			firstAggroDelay = action.Profile.FirstAggroDelay
		}
		if _, isSpawnPresentation := spawnPresentationObjectIDs[plan.ObjectID]; isSpawnPresentation {
			firstAggroDelay += zonespawn.Duration
		}
		if isFirstAction && !isFloorWarpIntroduction &&
			action.Profile.IsFirstAggroDurationKnown {
			aggroPackets, err := npcraknet.FirstAggro(action, timestamp)
			if err != nil {
				npcSession.ReleaseAction(plan.ObjectID, owner)
				releaseCampaignNPCFirstActions(npcSession, owner, startedObjectIDs)
				return nil, fmt.Errorf("enemyFirstAggro[%d]: %w", index, err)
			}
			if !npcSession.CommitFacing(action.FirstAggroFacingPlan()) {
				continue
			}
			immediatePackets = append(immediatePackets, aggroPackets...)
			if zone != nil {
				aggroEvents = append(aggroEvents, zonenpc.ActionEvent{
					Kind: zonenpc.ActionEventAggro, Plan: action,
					Timestamp: timestamp,
				})
			}
		}
		if isFirstAction && !isFloorWarpIntroduction &&
			action.Profile.FirstAggroRevealDelay > 0 {
			revealStep := campaignNPCFirstAggroRevealStep{
				runtime: r, sessionKey: sessionKey, generation: generation,
				actionGeneration: npc.ActionGeneration,
				objectID:         plan.ObjectID,
				timestamp: timestamp + uint64(
					action.Profile.FirstAggroRevealDelay/time.Millisecond,
				),
			}
			producers = append(producers, raknet.ScheduledPacketProducer{
				Delay:   action.Profile.FirstAggroRevealDelay,
				Produce: revealStep.produce,
			})
		}
		if isFirstAction && !isFloorWarpIntroduction &&
			action.Profile.FirstAggroEffectName != "" &&
			action.Profile.FirstAggroEffectDelay > 0 {
			effectStep := campaignNPCFirstAggroEffectStep{
				runtime: r, sessionKey: sessionKey, generation: generation,
				actionGeneration: npc.ActionGeneration, objectID: plan.ObjectID,
			}
			producers = append(producers, raknet.ScheduledPacketProducer{
				Delay: action.Profile.FirstAggroEffectDelay, Produce: effectStep.produce,
			})
		}
		objectID := plan.ObjectID
		actionTimestamp := timestamp
		if _, isSpawnPresentation := spawnPresentationObjectIDs[plan.ObjectID]; isSpawnPresentation {
			actionTimestamp += uint64(zonespawn.Duration / time.Millisecond)
		}
		step := campaignNPCFirstActionStep{
			runtime: r, packet: packet, sessionKey: sessionKey,
			generation: generation, actionGeneration: npc.ActionGeneration,
			objectID: objectID, timestamp: actionTimestamp,
			isDelayedReveal: isFirstAction && !isFloorWarpIntroduction &&
				action.Profile.FirstAggroRevealDelay > 0,
		}
		producer := raknet.ScheduledPacketProducer{
			Delay: firstAggroDelay, Produce: step.produce,
		}
		if isFirstAction && isCampaignBossIntroDelayed(plan) {
			bossStep := campaignBossIntroStep{runtime: r, zone: zone, sessionKey: sessionKey,
				generation: generation, plan: action, readyAt: r.now().Add(firstAggroDelay)}
			producers = append(producers, raknet.ScheduledPacketProducer{
				Delay: firstAggroDelay, Produce: bossStep.produce,
			})
		}
		producers = append(producers, producer)
		startedObjectIDs = append(startedObjectIDs, objectID)
	}
	// Publish target state before a zero-delay producer can commit movement.
	r.publishFirstAggro(zone, userID, generation, aggroEvents)
	if len(producers) == 0 {
		spawnPresentationGuard.isCommitted = true
		return immediatePackets, nil
	}
	sortScheduledPacketProducersByDelay(producers)
	producers = r.registry.producerGuard.scheduledProducers(sessionKey, producers)
	var scheduleErr error
	if packet.ScheduleGroupResult != nil {
		failure := campaignNPCFirstActionFailure{
			npc: npcSession, owner: owner, objectIDs: startedObjectIDs,
			spawnRegistry: r.registry, spawnSessionKey: sessionKey,
			spawnGeneration: generation, spawnRuns: spawnPresentationRuns,
			logger: r.logger,
		}
		_, scheduleErr = packet.ScheduleGroupResult(producers, failure.handle)
	} else if packet.ScheduleGroup != nil {
		_, scheduleErr = packet.ScheduleGroup(producers)
	} else {
		scheduleErr = errors.New("scheduler unavailable")
	}
	if scheduleErr == nil {
		spawnPresentationGuard.isCommitted = true
		return immediatePackets, nil
	}
	for index, producer := range producers {
		pursuitPackets, err := producer.Produce()
		if err != nil {
			releaseCampaignNPCFirstActions(npcSession, owner, startedObjectIDs)
			return nil, fmt.Errorf("enemyPursuitFallback[%d]: %w", index, err)
		}
		immediatePackets = append(immediatePackets, pursuitPackets...)
	}
	r.logger.Printf("RakNet campaign enemy pursuit published immediately after schedule failure: %v", scheduleErr)
	spawnPresentationGuard.isCommitted = true
	return immediatePackets, nil
}

func isCampaignNPCFirstActionSupported(family zonenpc.ActionFamily) bool {
	switch family {
	case zonenpc.ActionNomadDrone,
		zonenpc.ActionNomadSnipe,
		zonenpc.ActionZelemHaster,
		zonenpc.ActionZelemRanged,
		zonenpc.ActionProjectile,
		zonenpc.ActionRetainedArea,
		zonenpc.ActionPushPull,
		zonenpc.ActionChannelDrain,
		zonenpc.ActionResurrect,
		zonenpc.ActionCharge,
		zonenpc.ActionCone,
		zonenpc.ActionLeap,
		zonenpc.ActionMelee,
		zonenpc.ActionDetonate:
		return true
	default:
		return false
	}
}

func releaseCampaignNPCFirstActions(
	npc *zonenpc.Session,
	owner zonenpc.ActionOwner,
	objectIDs []uint32,
) {
	for _, objectID := range objectIDs {
		npc.ReleaseAction(objectID, owner)
	}
}

type campaignNPCFirstActionFailure struct {
	npc             *zonenpc.Session
	owner           zonenpc.ActionOwner
	objectIDs       []uint32
	spawnRegistry   *gameplaySessionRegistry
	spawnSessionKey string
	spawnGeneration uint64
	spawnRuns       map[uint32]*spawnraknet.Run
	logger          *log.Logger
}

type campaignNPCSpawnPresentationStep struct {
	registry   *gameplaySessionRegistry
	sessionKey string
	generation uint64
	objectID   uint32
	run        *spawnraknet.Run
}

func (e campaignNPCSpawnPresentationStep) produce() ([][]byte, error) {
	if e.registry == nil || e.run == nil {
		return nil, errors.New("spawn presentation unavailable")
	}
	e.registry.mutex.Lock()
	peerSession, isFound := e.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.spawnModifiers[e.objectID] == e.run
	if isCurrent {
		delete(peerSession.spawnModifiers, e.objectID)
		e.registry.sessions[e.sessionKey] = peerSession
	}
	e.registry.mutex.Unlock()
	if !isCurrent {
		e.run.Stop()
		return nil, nil
	}
	packets, err := e.run.Advance(context.Background(), zonespawn.Duration)
	if err != nil {
		e.run.Stop()
		return nil, fmt.Errorf("spawnPresentationAdvance: %w", err)
	}
	return packets, nil
}

func (e campaignNPCFirstActionFailure) handle(scheduleErr error) {
	releaseCampaignNPCFirstActions(e.npc, e.owner, e.objectIDs)
	releaseCampaignNPCSpawnPresentations(
		e.spawnRegistry, e.spawnSessionKey, e.spawnGeneration, e.spawnRuns,
	)
	if e.logger != nil {
		e.logger.Printf("RakNet campaign first action schedule failed: %v", scheduleErr)
	}
}

type campaignNPCSpawnPresentationGuard struct {
	registry    *gameplaySessionRegistry
	sessionKey  string
	generation  uint64
	runs        map[uint32]*spawnraknet.Run
	isCommitted bool
}

func (e *campaignNPCSpawnPresentationGuard) releaseUnlessCommitted() {
	if e == nil || e.isCommitted {
		return
	}
	releaseCampaignNPCSpawnPresentations(
		e.registry, e.sessionKey, e.generation, e.runs,
	)
}

func releaseCampaignNPCSpawnPresentations(
	registry *gameplaySessionRegistry, sessionKey string, generation uint64,
	runs map[uint32]*spawnraknet.Run,
) {
	if registry != nil {
		registry.mutex.Lock()
		peerSession, isFound := registry.sessions[sessionKey]
		if isFound && peerSession.generation == generation {
			for objectID, run := range runs {
				if peerSession.spawnModifiers[objectID] == run {
					delete(peerSession.spawnModifiers, objectID)
				}
			}
			registry.sessions[sessionKey] = peerSession
		}
		registry.mutex.Unlock()
	}
	for _, run := range runs {
		run.Stop()
	}
}

func (campaignNPCActionRuntime) publishFirstAggro(
	zone *zone.Zone, userID uint64, generation uint64,
	events []zonenpc.ActionEvent,
) {
	if zone == nil {
		return
	}
	for _, current := range events {
		zone.PublishNPCAction(current, userID, generation)
	}
}

type campaignNPCModifierRun struct {
	mu                 sync.Mutex
	instanceID         uint32
	record             zoneeffect.Modifier
	cancel             raknet.CancelSchedule
	fearTargetObjectID uint32
	fearExpiresAt      time.Time
	isCreated          bool
	isReleased         bool
}

func newCampaignNPCModifierRun(pool *modifierPool) (*campaignNPCModifierRun, error) {
	instanceID, err := pool.Allocate()
	if err != nil {
		return nil, fmt.Errorf("enemyModifierAllocate: %w", err)
	}
	return &campaignNPCModifierRun{instanceID: instanceID}, nil
}

func (r *campaignNPCModifierRun) create() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.isCreated || r.isReleased {
		return false
	}
	r.isCreated = true
	return true
}

func (r *campaignNPCModifierRun) isActive() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.isCreated && !r.isReleased
}

func (r *campaignNPCModifierRun) release(pool *modifierPool) (bool, error) {
	if r == nil {
		return false, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.isReleased {
		return false, nil
	}
	err := pool.Release(r.instanceID)
	if err != nil {
		return false, fmt.Errorf("enemyModifierRelease: %w", err)
	}
	r.isReleased = true
	return r.isCreated, nil
}

func (s *gameplayPeerSession) trackCampaignNPCModifier(
	run *campaignNPCModifierRun,
) error {
	if s == nil || run == nil || run.instanceID == 0 {
		return errors.New("enemy modifier track invalid")
	}
	if s.campaignNPCModifiers == nil {
		s.campaignNPCModifiers = make(map[uint32]*campaignNPCModifierRun)
	}
	if _, isFound := s.campaignNPCModifiers[run.instanceID]; isFound {
		return errors.New("enemy modifier already tracked")
	}
	s.campaignNPCModifiers[run.instanceID] = run
	return nil
}

func (s *gameplayPeerSession) untrackCampaignNPCModifier(
	run *campaignNPCModifierRun,
) {
	if s == nil || run == nil || s.campaignNPCModifiers[run.instanceID] != run {
		return
	}
	delete(s.campaignNPCModifiers, run.instanceID)
}

func (s *gameplayPeerSession) enemyMovementSpeedBuff() float32 {
	if s == nil || s.deployedObjectID == 0 {
		return 0
	}
	movementSpeedBuff := float32(0)
	for _, run := range s.campaignNPCModifiers {
		if run == nil || run.record.TargetObjectID != s.deployedObjectID ||
			!run.isActive() {
			continue
		}
		movementSpeedBuff += run.record.MovementSpeedBuff
	}
	return min(float32(0), movementSpeedBuff)
}

func (s *gameplayPeerSession) enemyAttackSpeed() float32 {
	if s == nil || s.deployedObjectID == 0 {
		return 0
	}
	attackSpeed := float32(0)
	for _, run := range s.campaignNPCModifiers {
		if run == nil || run.record.TargetObjectID != s.deployedObjectID ||
			!run.isActive() {
			continue
		}
		attackSpeed += run.record.AttackSpeed
	}
	return max(float32(-0.9), min(float32(0), attackSpeed))
}

func marshalCampaignNPCSlowAttributes(
	objectID uint32, attackSpeed float32, movementSpeedBuff float32,
) ([]byte, error) {
	packet, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
		ObjectID: objectID,
		Value: map[uint8]float32{
			23: attackSpeed,
			48: movementSpeedBuff,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("enemySlowAttributes: %w", err)
	}
	return packet, nil
}

func (s *gameplayPeerSession) stopCampaignNPCModifiers(pool *modifierPool) {
	if s == nil {
		return
	}
	for key, run := range s.campaignNPCPoisons {
		if run.cancel != nil {
			run.cancel()
		}
		delete(s.campaignNPCPoisons, key)
	}
	for targetObjectID, run := range s.heroPassivePoisons {
		if run.cancel != nil {
			run.cancel()
		}
		delete(s.heroPassivePoisons, targetObjectID)
	}
	for key, run := range s.heroHitPoisons {
		if run.cancel != nil {
			run.cancel()
		}
		s.untrackCampaignNPCModifier(run.modifier)
		_, _ = run.modifier.release(pool)
		delete(s.heroHitPoisons, key)
	}
	for targetObjectID, run := range s.heroTimeRavagerSlows {
		if run.cancel != nil {
			run.cancel()
		}
		if s.zone != nil && s.zone.NPCs() != nil {
			s.zone.NPCs().ClearSlow(targetObjectID, run.expiresAt)
		}
		if s.zone != nil && s.zone.Effect() != nil {
			s.zone.Effect().Remove(run.modifier.instanceID)
		}
		s.untrackCampaignNPCModifier(run.modifier)
		_, _ = run.modifier.release(pool)
		delete(s.heroTimeRavagerSlows, targetObjectID)
	}
	for targetObjectID, run := range s.heroEnergyVulnerabilities {
		if run.cancel != nil {
			run.cancel()
		}
		if s.zone != nil && s.zone.NPCs() != nil {
			s.zone.NPCs().ClearEnergyDamageVulnerability(
				targetObjectID, run.sourceObjectID, run.expiresAt,
			)
		}
		if s.zone != nil && s.zone.Effect() != nil {
			s.zone.Effect().Remove(run.modifier.instanceID)
		}
		s.untrackCampaignNPCModifier(run.modifier)
		_, _ = run.modifier.release(pool)
		delete(s.heroEnergyVulnerabilities, targetObjectID)
	}
	for key, run := range s.heroHealingReductions {
		if run.cancel != nil {
			run.cancel()
		}
		if s.zone != nil && s.zone.NPCs() != nil {
			s.zone.NPCs().ClearHealingReduction(key.targetObjectID, run.expiresAt)
		}
		if s.zone != nil && s.zone.Effect() != nil {
			s.zone.Effect().Remove(run.modifier.instanceID)
		}
		s.untrackCampaignNPCModifier(run.modifier)
		_, _ = run.modifier.release(pool)
		delete(s.heroHealingReductions, key)
	}
	for targetObjectID, run := range s.heroTaunts {
		if run.cancel != nil {
			run.cancel()
		}
		if s.zone != nil && s.zone.NPCs() != nil {
			s.zone.NPCs().ClearTaunt(
				targetObjectID, run.sourceObjectID, run.expiresAt,
			)
		}
		if s.zone != nil && s.zone.Effect() != nil {
			s.zone.Effect().Remove(run.modifier.instanceID)
		}
		s.untrackCampaignNPCModifier(run.modifier)
		_, _ = run.modifier.release(pool)
		delete(s.heroTaunts, targetObjectID)
	}
	for targetObjectID, run := range s.campaignNPCSilences {
		if run.cancel != nil {
			run.cancel()
		}
		delete(s.campaignNPCSilences, targetObjectID)
	}
	for targetObjectID, run := range s.campaignNPCEnergyBuffs {
		if run.cancel != nil {
			run.cancel()
		}
		if s.zone != nil && s.zone.NPCs() != nil {
			s.zone.NPCs().ClearEnergyBuff(targetObjectID, run.expiresAt)
		}
		delete(s.campaignNPCEnergyBuffs, targetObjectID)
	}
	for objectID, run := range s.campaignNPCIntangibles {
		if run.cancel != nil {
			run.cancel()
		}
		if len(run.revealPacket) > 0 {
			s.queuePackets([][]byte{run.revealPacket})
		}
		if s.zone != nil && s.zone.NPCs() != nil {
			s.zone.NPCs().ClearIntangible(objectID, run.expiresAt)
		}
		delete(s.campaignNPCIntangibles, objectID)
	}
	for objectID, run := range s.campaignNPCMunches {
		if s.zone != nil && s.zone.NPCs() != nil {
			s.zone.NPCs().ClearMunch(objectID, run.expiresAt)
		}
		delete(s.campaignNPCMunches, objectID)
	}
	for targetObjectID, run := range s.campaignNPCPhysicalVulnerabilities {
		if run.cancel != nil {
			run.cancel()
		}
		_, _ = run.modifier.release(pool)
		delete(s.campaignNPCPhysicalVulnerabilities, targetObjectID)
	}
	for targetObjectID, run := range s.campaignNPCEnergyVulnerabilities {
		_, _ = run.modifier.release(pool)
		delete(s.campaignNPCEnergyVulnerabilities, targetObjectID)
	}
	for targetObjectID, run := range s.campaignNPCFears {
		if run.cancel != nil {
			run.cancel()
		}
		_, _ = run.modifier.release(pool)
		delete(s.campaignNPCFears, targetObjectID)
	}
	s.enemySilenceExpiresAt = time.Time{}
	s.enemySleepExpiresAt = time.Time{}
	s.enemyRootExpiresAt = time.Time{}
	s.enemyRootTargetObjectID = 0
	s.enemyFearExpiresAt = time.Time{}
	s.enemyFearTargetObjectID = 0
	for instanceID, run := range s.campaignNPCModifiers {
		if run.cancel != nil {
			run.cancel()
		}
		if run.fearTargetObjectID != 0 && s.zone != nil && s.zone.NPCs() != nil {
			s.zone.NPCs().ClearFear(run.fearTargetObjectID, run.fearExpiresAt)
		}
		if s.zone != nil && s.zone.Effect() != nil {
			s.zone.Effect().Remove(instanceID)
		}
		_, _ = run.release(pool)
		delete(s.campaignNPCModifiers, instanceID)
	}
}

func (r campaignNPCActionRuntime) produceSnipeMelee(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64, slowEndTimestamp uint64,
) ([][]byte, error) {
	request := campaignNPCAttackRequest{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
		slowEndTimestamp: slowEndTimestamp, kind: campaignNPCAttackSnipeMelee,
	}
	isDeferred, err := r.pursuit.deferAction(
		packet, sessionKey, generation, objectID, timestamp,
		request.resume,
	)
	if err != nil {
		return nil, fmt.Errorf("enemySnipeMeleeStun: %w", err)
	}
	if isDeferred {
		return nil, nil
	}
	if zonenpc.ShouldRefreshSnipeSlow(timestamp, slowEndTimestamp) {
		return r.produceSnipeSlow(packet, sessionKey, generation, objectID, timestamp)
	}
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound &&
		peerSession.isCampaignNPCSourceActive(generation, objectID)
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, enemy.TargetObjectID,
	)
	r.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	if !isEnemyFound || enemy.IsDefeated || !isTargetFound {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	profile := zonenpc.NomadSnipeMeleeProfile(enemy.Plan.NounName)
	attackPlan, err := zonenpc.PlanAttackWithProfile(
		enemy, target.ObjectID, target.Position, profile, target.FootprintRadius,
	)
	if err != nil {
		action, actionErr := campaignNPCActionWithProfile(
			enemy.Plan, target.ObjectID, target.Position, profile, target.FootprintRadius,
		)
		if actionErr != nil {
			return nil, fmt.Errorf("enemySnipePursuitAction: %w", actionErr)
		}
		if !action.IsPursuitNeeded {
			return nil, nil
		}
		pursuitPackets, marshalErr := npcraknet.Pursuit(action)
		if marshalErr != nil {
			return nil, fmt.Errorf("enemySnipePursuitMarshal: %w", marshalErr)
		}
		scheduleErr := r.pursuit.schedule(
			packet, sessionKey, generation, objectID, timestamp,
			action.TargetPosition, profile, request.resume,
		)
		if scheduleErr != nil {
			r.releaseAction(sessionKey, generation, objectID)
			r.logger.Printf("RakNet campaign Snipe pursuit not scheduled object=%d: %v", objectID, scheduleErr)
		}
		return pursuitPackets, nil
	}
	startPackets, err := r.startNPCAttack(sessionKey, generation, attackPlan, timestamp)

	if err != nil {
		return nil, fmt.Errorf("enemySnipeStart: %w", err)
	}
	timeline, err := zonenpc.TimelineForAttack(attackPlan)
	if err != nil {
		return nil, fmt.Errorf("enemySnipeTimeline: %w", err)
	}
	schedule := campaignNPCAttackSchedule{request: request, plan: attackPlan}
	hitProducer := raknet.ScheduledPacketProducer{
		Delay: timeline.HitDelay, Produce: schedule.hit,
	}
	nextProducer := raknet.ScheduledPacketProducer{
		Delay: timeline.NextDelay, Produce: schedule.next,
	}
	_, scheduleErr := scheduleNPCProducers(r.registry, packet,
		[]raknet.ScheduledPacketProducer{hitProducer, nextProducer},
	)
	if scheduleErr != nil {
		r.releaseAction(sessionKey, generation, objectID)
		r.logger.Printf("RakNet campaign Snipe melee continuation not scheduled object=%d: %v", objectID, scheduleErr)
	}
	return startPackets, nil
}

type campaignSnipeSlowSchedule struct {
	runtime          campaignNPCActionRuntime
	packet           raknet.Packet
	sessionKey       string
	generation       uint64
	objectID         uint32
	timestamp        uint64
	hitTimestamp     uint64
	slowEndTimestamp uint64
	plan             zonenpc.AttackPlan
	run              *campaignNPCModifierRun
}

func (e campaignSnipeSlowSchedule) resume(
	timestamp uint64,
) ([][]byte, error) {
	return e.runtime.produceSnipeSlow(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
}

func (e campaignSnipeSlowSchedule) meleeArrival(
	timestamp uint64,
) ([][]byte, error) {
	return e.runtime.produceSnipeMelee(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
		e.slowEndTimestamp,
	)
}

func (e campaignSnipeSlowSchedule) hit() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	current, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && current.isCampaignNPCAttackActiveAt(
		e.generation, e.objectID, e.plan.TargetObjectID, e.runtime.now(),
	)
	if !isCurrent {
		e.runtime.registry.mutex.RUnlock()
		_, err := e.run.release(e.runtime.modifierPool)
		if err != nil {
			return nil, fmt.Errorf("enemySlowHitRelease: %w", err)
		}
		return nil, nil
	}
	currentNPC, isNPCFound := current.zone.NPCs().NPC(e.objectID)
	target, isTargetFound := current.campaignNPCTarget(
		e.generation, currentNPC.TargetObjectID,
	)
	e.runtime.registry.mutex.RUnlock()
	if !isNPCFound || currentNPC.IsDefeated || !isTargetFound {
		_, err := e.run.release(e.runtime.modifierPool)
		if err != nil {
			return nil, fmt.Errorf("enemySlowHitRelease: %w", err)
		}
		return nil, nil
	}
	_, err := zonenpc.PlanSnipeSlow(
		currentNPC, target.ObjectID, target.Position, target.FootprintRadius,
	)
	if err != nil || target.ObjectID != e.plan.TargetObjectID {
		_, releaseErr := e.run.release(e.runtime.modifierPool)
		if releaseErr != nil {
			return nil, fmt.Errorf("enemySlowPlanRelease: %w", releaseErr)
		}
		return nil, nil
	}
	packet, err := npcraknet.ModifierCreate(
		e.plan, e.run.instanceID, e.hitTimestamp,
	)
	if err != nil {
		_, releaseErr := e.run.release(e.runtime.modifierPool)
		return nil, fmt.Errorf(
			"enemySlowCreate: %w", errors.Join(err, releaseErr),
		)
	}
	if !e.run.create() {
		return nil, nil
	}
	err = current.zone.Effect().Put(e.run.record)
	if err != nil {
		e.runtime.registry.mutex.Lock()
		latest, isLatestFound := e.runtime.registry.sessions[e.sessionKey]
		if isLatestFound && latest.generation == e.generation {
			latest.untrackCampaignNPCModifier(e.run)
			e.runtime.registry.sessions[e.sessionKey] = latest
		}
		e.runtime.registry.mutex.Unlock()
		_, releaseErr := e.run.release(e.runtime.modifierPool)
		return nil, fmt.Errorf(
			"enemySlowInventory: %w", errors.Join(err, releaseErr),
		)
	}
	e.runtime.registry.mutex.RLock()
	latest, isLatestFound := e.runtime.registry.sessions[e.sessionKey]
	attackSpeed := float32(0)
	movementSpeedBuff := float32(0)
	if isLatestFound && latest.generation == e.generation {
		attackSpeed = latest.enemyAttackSpeed()
		movementSpeedBuff = latest.enemyMovementSpeedBuff()
	}
	e.runtime.registry.mutex.RUnlock()
	attributePacket, err := marshalCampaignNPCSlowAttributes(
		e.plan.TargetObjectID, attackSpeed, movementSpeedBuff,
	)
	if err != nil {
		return nil, fmt.Errorf("enemySlowHitAttributes: %w", err)
	}
	return [][]byte{packet, attributePacket}, nil
}

func (e campaignSnipeSlowSchedule) melee() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	current, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && current.isCampaignNPCAttackActiveAt(
		e.generation, e.objectID, e.plan.TargetObjectID, e.runtime.now(),
	)
	if !isCurrent {
		e.runtime.registry.mutex.RUnlock()
		return nil, nil
	}
	currentNPC, isNPCFound := current.zone.NPCs().NPC(e.objectID)
	target, isTargetFound := current.campaignNPCTarget(
		e.generation, currentNPC.TargetObjectID,
	)
	e.runtime.registry.mutex.RUnlock()
	if !isNPCFound || currentNPC.IsDefeated || !isTargetFound {
		e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
		return nil, nil
	}
	action, err := campaignNPCActionWithProfile(
		currentNPC.Plan, target.ObjectID, target.Position,
		zonenpc.NomadSnipeMeleeProfile(currentNPC.Plan.NounName),
		target.FootprintRadius,
	)
	if err != nil {
		return nil, fmt.Errorf("enemySlowMeleeAction: %w", err)
	}
	meleeTimestamp := e.timestamp +
		uint64(e.plan.Profile.ReleaseDelay/time.Millisecond)
	if !action.IsPursuitNeeded {
		return e.meleeArrival(meleeTimestamp)
	}
	packets, err := npcraknet.Pursuit(action)
	if err != nil {
		return nil, fmt.Errorf("enemySlowMeleePursuit: %w", err)
	}
	err = e.runtime.pursuit.schedule(
		e.packet, e.sessionKey, e.generation, e.objectID,
		meleeTimestamp, action.TargetPosition, action.Profile, e.meleeArrival,
	)
	if err != nil {
		e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
		e.runtime.logger.Printf(
			"RakNet campaign Snipe melee pursuit not scheduled object=%d: %v",
			e.objectID, err,
		)
	}
	return packets, nil
}

func (e campaignSnipeSlowSchedule) remove() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	current, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && current.generation == e.generation
	attackSpeed := float32(0)
	movementSpeedBuff := float32(0)
	if isCurrent {
		if current.zone != nil && current.zone.Effect() != nil {
			current.zone.Effect().Remove(e.run.instanceID)
		}
		current.untrackCampaignNPCModifier(e.run)
		attackSpeed = current.enemyAttackSpeed()
		movementSpeedBuff = current.enemyMovementSpeedBuff()
		e.runtime.registry.sessions[e.sessionKey] = current
	}
	e.runtime.registry.mutex.Unlock()
	isCreated, err := e.run.release(e.runtime.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("enemySlowDeleteRelease: %w", err)
	}
	if !isCurrent || !isCreated {
		return nil, nil
	}
	packet, err := effectraknet.ModifierDelete(
		e.plan.TargetObjectID, e.run.instanceID,
	)
	if err != nil {
		return nil, fmt.Errorf("enemySlowDelete: %w", err)
	}
	attributePacket, err := marshalCampaignNPCSlowAttributes(
		e.plan.TargetObjectID, attackSpeed, movementSpeedBuff,
	)
	if err != nil {
		return nil, fmt.Errorf("enemySlowDeleteAttributes: %w", err)
	}
	return [][]byte{packet, attributePacket}, nil
}

func (r campaignNPCActionRuntime) produceSnipeSlow(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, error) {
	resume := campaignSnipeSlowSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
	}
	isDeferred, err := r.pursuit.deferAction(
		packet, sessionKey, generation, objectID, timestamp,
		resume.resume,
	)
	if err != nil {
		return nil, fmt.Errorf("enemySnipeSlowStun: %w", err)
	}
	if isDeferred {
		return nil, nil
	}
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound &&
		peerSession.isCampaignNPCSourceActive(generation, objectID) &&
		peerSession.zone.NPCs() != nil
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, enemy.TargetObjectID,
	)
	r.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	if !isEnemyFound || enemy.IsDefeated || !isTargetFound {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	slowPlan, err := zonenpc.PlanSnipeSlow(
		enemy, target.ObjectID, target.Position, target.FootprintRadius,
	)
	if err != nil {
		action, isActionFound, actionErr := campaignNPCFirstAction(
			enemy.Plan, target.ObjectID, target.Position, target.FootprintRadius,
		)
		if actionErr != nil {
			return nil, fmt.Errorf("enemySlowPursuitAction: %w", actionErr)
		}
		if !isActionFound || !action.IsPursuitNeeded {
			return nil, nil
		}
		pursuitPackets, marshalErr := npcraknet.Pursuit(action)
		if marshalErr != nil {
			return nil, fmt.Errorf("enemySlowPursuitMarshal: %w", marshalErr)
		}
		scheduleErr := r.pursuit.schedule(
			packet, sessionKey, generation, objectID, timestamp,
			action.TargetPosition, action.Profile, resume.resume,
		)
		if scheduleErr != nil {
			r.releaseAction(sessionKey, generation, objectID)
			r.logger.Printf("RakNet campaign Snipe Slow pursuit not scheduled object=%d: %v", objectID, scheduleErr)
		}
		return pursuitPackets, nil
	}
	startPackets, err := r.startNPCAttack(sessionKey, generation, slowPlan, timestamp)
	if err != nil {
		return nil, fmt.Errorf("enemySlowStart: %w", err)
	}
	run, err := newCampaignNPCModifierRun(r.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("enemySlowRun: %w", err)
	}
	run.record = zoneeffect.Modifier{
		InstanceID: run.instanceID, GUID: slowPlan.Profile.ModifierGUID(),
		SourceObjectID: objectID, TargetObjectID: slowPlan.TargetObjectID,
		Rank: 1, Duration: slowPlan.Profile.ModifierDuration,
		Kind: zoneeffect.ModifierKindDebuff, InitiatorObject: objectID,
		StackCount: 1, AttackSpeed: -0.30, MovementSpeedBuff: -0.70,
	}
	r.registry.mutex.Lock()
	currentSession, isCurrentFound := r.registry.sessions[sessionKey]
	isCurrent = isCurrentFound && currentSession.isCampaignNPCAttackActiveAt(
		generation, objectID, slowPlan.TargetObjectID, r.now(),
	)
	if isCurrent {
		err = currentSession.trackCampaignNPCModifier(run)
		if err == nil {
			r.registry.sessions[sessionKey] = currentSession
		}
	}
	r.registry.mutex.Unlock()
	if !isCurrent {
		_, releaseErr := run.release(r.modifierPool)
		if releaseErr != nil {
			return nil, fmt.Errorf("enemySlowRelease: %w", releaseErr)
		}
		return nil, nil
	}
	if err != nil {
		_, releaseErr := run.release(r.modifierPool)
		return nil, fmt.Errorf("enemySlowTrack: %w", errors.Join(err, releaseErr))
	}
	hitTimestamp := timestamp + uint64(slowPlan.Profile.HitDelay/time.Millisecond)
	slowEndTimestamp := hitTimestamp + uint64(slowPlan.Profile.ModifierDuration/time.Millisecond)
	schedule := campaignSnipeSlowSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
		hitTimestamp: hitTimestamp, slowEndTimestamp: slowEndTimestamp,
		plan: slowPlan, run: run,
	}
	hitProducer := raknet.ScheduledPacketProducer{
		Delay: slowPlan.Profile.HitDelay, Produce: schedule.hit,
	}
	meleeProducer := raknet.ScheduledPacketProducer{
		Delay: slowPlan.Profile.ReleaseDelay, Produce: schedule.melee,
	}
	deleteProducer := raknet.ScheduledPacketProducer{
		Delay:   slowPlan.Profile.HitDelay + slowPlan.Profile.ModifierDuration,
		Produce: schedule.remove,
	}
	_, scheduleErr := scheduleNPCProducers(r.registry, packet, []raknet.ScheduledPacketProducer{
		hitProducer, meleeProducer, deleteProducer,
	})
	if scheduleErr != nil {
		r.registry.mutex.Lock()
		currentSession, isCurrentFound := r.registry.sessions[sessionKey]
		if isCurrentFound && currentSession.generation == generation {
			currentSession.untrackCampaignNPCModifier(run)
			r.registry.sessions[sessionKey] = currentSession
		}
		r.registry.mutex.Unlock()
		_, releaseErr := run.release(r.modifierPool)
		return nil, fmt.Errorf("enemySlowSchedule: %w", errors.Join(scheduleErr, releaseErr))
	}
	return startPackets, nil
}

func planCampaignNPCHasterBuff(
	enemy zonenpc.Snapshot, target zonenpc.Snapshot,
) (zonenpc.AttackPlan, error) {
	profile, isFound := zonenpc.ActionProfileForNoun(enemy.Plan.NounName)
	if !isFound || profile.Family != zonenpc.ActionZelemHaster ||
		profile.ModifierName == "" || profile.ModifierDuration <= 0 {
		return zonenpc.AttackPlan{}, errors.New("enemy haste unsupported")
	}
	if enemy.IsDefeated || enemy.HitPoint <= 0 || target.IsDefeated ||
		target.HitPoint <= 0 || target.Faction != enemy.Faction {
		return zonenpc.AttackPlan{}, errors.New("enemy haste source unavailable")
	}
	return zonenpc.AttackPlan{
		SourceObjectID: enemy.Plan.ObjectID, TargetObjectID: target.Plan.ObjectID,
		ActionGeneration: enemy.ActionGeneration,
		SourcePosition:   enemy.Plan.Position, TargetPosition: target.Plan.Position,
		Profile: profile,
	}, nil
}

func planCampaignNPCHasterAttack(
	enemy zonenpc.Snapshot, targetObjectID uint32,
	targetPosition game.Vec3, targetFootprintRadius float32,

) (zonenpc.AttackPlan, error) {
	profile, isFound := zonenpc.ActionProfileForNoun(enemy.Plan.NounName)
	if !isFound || profile.Family != zonenpc.ActionZelemHaster {
		return zonenpc.AttackPlan{}, errors.New("enemy haste attack unsupported")
	}
	attackProfile := zonenpc.ZelemHasterAttackProfile()
	attackProfile.MovementSpeed = profile.MovementSpeed
	attackProfile.NonCombatMovementSpeed = profile.NonCombatMovementSpeed
	return zonenpc.PlanAttackWithProfile(
		enemy, targetObjectID, targetPosition,
		attackProfile, targetFootprintRadius,
	)
}

func campaignNPCHasterProjectileAbility(
	profile zonenpc.ActionProfile,
) (sim.AbilityDefinition, error) {
	return campaignNPCProjectileAbility(profile)
}

type campaignHasterBuffSchedule struct {
	runtime           campaignNPCActionRuntime
	packet            raknet.Packet
	sessionKey        string
	generation        uint64
	objectID          uint32
	timestamp         uint64
	hitTimestamp      uint64
	hasteEndTimestamp uint64
	hasteExpiresAt    time.Time
	plan              zonenpc.AttackPlan
	run               *campaignNPCModifierRun
}

func (e campaignHasterBuffSchedule) resume(
	timestamp uint64,
) ([][]byte, error) {
	return e.runtime.produceHasterBuff(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
}

func (e campaignHasterBuffSchedule) hit() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	current, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || current.zone.NPCs() == nil {
		e.runtime.registry.mutex.RUnlock()
		_, err := e.run.release(e.runtime.modifierPool)
		if err != nil {
			return nil, fmt.Errorf("enemyHasterBuffHitRelease: %w", err)
		}
		return nil, nil
	}
	isCurrent := current.isCampaignNPCSourceActive(e.generation, e.objectID)
	npc, isNPCFound := current.zone.NPCs().NPC(e.objectID)
	target, isTargetFound := current.zone.NPCs().NPC(e.plan.TargetObjectID)
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent || !isNPCFound || npc.IsDefeated || !isTargetFound ||
		target.IsDefeated || target.Faction != npc.Faction {
		_, err := e.run.release(e.runtime.modifierPool)
		if err != nil {
			return nil, fmt.Errorf("enemyHasterBuffHitRelease: %w", err)
		}
		return nil, nil
	}
	packet, err := npcraknet.ModifierCreate(
		e.plan, e.run.instanceID, e.hitTimestamp,
	)
	if err != nil {
		_, releaseErr := e.run.release(e.runtime.modifierPool)
		return nil, fmt.Errorf(
			"enemyHasterBuffCreate: %w", errors.Join(err, releaseErr),
		)
	}
	if !e.run.create() {
		return nil, nil
	}
	err = current.zone.Effect().Put(e.run.record)
	if err != nil {
		e.runtime.registry.mutex.Lock()
		latest, isLatestFound := e.runtime.registry.sessions[e.sessionKey]
		if isLatestFound && latest.generation == e.generation {
			latest.untrackCampaignNPCModifier(e.run)
			e.runtime.registry.sessions[e.sessionKey] = latest
		}
		e.runtime.registry.mutex.Unlock()
		_, releaseErr := e.run.release(e.runtime.modifierPool)
		return nil, fmt.Errorf(
			"enemyHasterBuffInventory: %w", errors.Join(err, releaseErr),
		)
	}
	err = current.zone.NPCs().ApplyHaste(
		e.plan.TargetObjectID, e.hasteExpiresAt, 0.25, 0.25, 0.50,
	)
	if err != nil {
		current.zone.Effect().Remove(e.run.instanceID)
		e.runtime.registry.mutex.Lock()
		latest, isLatestFound := e.runtime.registry.sessions[e.sessionKey]
		if isLatestFound && latest.generation == e.generation {
			latest.untrackCampaignNPCModifier(e.run)
			e.runtime.registry.sessions[e.sessionKey] = latest
		}
		e.runtime.registry.mutex.Unlock()
		_, releaseErr := e.run.release(e.runtime.modifierPool)
		return nil, fmt.Errorf(
			"enemyHasterBuffApply: %w", errors.Join(err, releaseErr),
		)
	}
	return [][]byte{packet}, nil
}

func (e campaignHasterBuffSchedule) release() ([][]byte, error) {
	timestamp := e.timestamp +
		uint64(e.plan.Profile.ReleaseDelay/time.Millisecond)
	return e.runtime.produceHasterProjectile(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
		e.hasteEndTimestamp,
	)
}

func (e campaignHasterBuffSchedule) remove() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	current, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && current.generation == e.generation
	if isCurrent {
		if current.zone != nil && current.zone.Effect() != nil {
			current.zone.Effect().Remove(e.run.instanceID)
		}
		current.zone.NPCs().ClearHaste(e.plan.TargetObjectID, e.hasteExpiresAt)
		current.untrackCampaignNPCModifier(e.run)
		e.runtime.registry.sessions[e.sessionKey] = current
	}
	e.runtime.registry.mutex.Unlock()
	isCreated, err := e.run.release(e.runtime.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("enemyHasterBuffDeleteRelease: %w", err)
	}
	if !isCurrent || !isCreated {
		return nil, nil
	}
	packet, err := effectraknet.ModifierDelete(
		e.plan.TargetObjectID, e.run.instanceID,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyHasterBuffDelete: %w", err)
	}
	return [][]byte{packet}, nil
}

func (r campaignNPCActionRuntime) produceHasterBuff(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, error) {
	resume := campaignHasterBuffSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
	}
	isDeferred, err := r.pursuit.deferAction(
		packet, sessionKey, generation, objectID, timestamp,
		resume.resume,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyHasterBuffStun: %w", err)
	}
	if isDeferred {
		return nil, nil
	}
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound &&
		peerSession.isCampaignNPCSourceActive(generation, objectID)
	enemy := zonenpc.Snapshot{}
	isEnemyFound := false
	if isCurrent {
		enemy, isEnemyFound = peerSession.zone.NPCs().NPC(objectID)
	}
	r.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	if !isEnemyFound || enemy.IsDefeated {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	target := enemy
	profile, isProfileFound := zonenpc.ActionProfileForNoun(enemy.Plan.NounName)
	if isProfileFound {
		ally, isAllyFound := peerSession.zone.NPCs().FirstLivingAlly(
			objectID, profile.Range,
		)
		if isAllyFound {
			target = ally
		}
	}
	plan, err := planCampaignNPCHasterBuff(enemy, target)
	if err != nil {
		return nil, fmt.Errorf("enemyHasterBuffPlan: %w", err)
	}
	startPackets, err := r.startNPCAttack(sessionKey, generation, plan, timestamp)
	if err != nil {
		return nil, fmt.Errorf("enemyHasterBuffStart: %w", err)
	}
	run, err := newCampaignNPCModifierRun(r.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("enemyHasterBuffRun: %w", err)
	}
	run.record = zoneeffect.Modifier{
		InstanceID: run.instanceID, GUID: util.HashID(plan.Profile.ModifierName),
		SourceObjectID: objectID, TargetObjectID: plan.TargetObjectID,
		Rank: 1, Duration: plan.Profile.ModifierDuration,
		Kind: zoneeffect.ModifierKindBuff, IsHaste: true,
		InitiatorObject: objectID,
		StackCount:      1, AttackSpeed: 0.25, CooldownReduction: 0.25,
		MovementSpeedBuff: 0.50,
	}
	r.registry.mutex.Lock()
	currentSession, isCurrentFound := r.registry.sessions[sessionKey]
	isCurrent = isCurrentFound &&
		currentSession.isCampaignNPCSourceActive(generation, objectID)
	if isCurrent {
		err = currentSession.trackCampaignNPCModifier(run)
		if err == nil {
			r.registry.sessions[sessionKey] = currentSession

		}
	}
	r.registry.mutex.Unlock()
	if !isCurrent {
		_, releaseErr := run.release(r.modifierPool)
		if releaseErr != nil {
			return nil, fmt.Errorf("enemyHasterBuffRelease: %w", releaseErr)
		}
		return nil, nil
	}
	if err != nil {
		_, releaseErr := run.release(r.modifierPool)
		return nil, fmt.Errorf("enemyHasterBuffTrack: %w", errors.Join(err, releaseErr))
	}
	hitTimestamp := timestamp + uint64(plan.Profile.HitDelay/time.Millisecond)
	hasteEndTimestamp := hitTimestamp + uint64(plan.Profile.ModifierDuration/time.Millisecond)
	hasteExpiresAt := r.now().Add(
		plan.Profile.HitDelay + plan.Profile.ModifierDuration,
	)
	schedule := campaignHasterBuffSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
		hitTimestamp: hitTimestamp, hasteEndTimestamp: hasteEndTimestamp,
		hasteExpiresAt: hasteExpiresAt,
		plan:           plan, run: run,
	}
	hitProducer := raknet.ScheduledPacketProducer{
		Delay: plan.Profile.HitDelay, Produce: schedule.hit,
	}
	releaseProducer := raknet.ScheduledPacketProducer{
		Delay: plan.Profile.ReleaseDelay, Produce: schedule.release,
	}
	deleteProducer := raknet.ScheduledPacketProducer{
		Delay:   plan.Profile.HitDelay + plan.Profile.ModifierDuration,
		Produce: schedule.remove,
	}
	cancel, scheduleErr := scheduleNPCProducers(r.registry, packet, []raknet.ScheduledPacketProducer{
		hitProducer, releaseProducer, deleteProducer,
	})
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		r.registry.mutex.Lock()
		currentSession, isCurrentFound := r.registry.sessions[sessionKey]
		if isCurrentFound && currentSession.generation == generation {
			currentSession.untrackCampaignNPCModifier(run)
			r.registry.sessions[sessionKey] = currentSession
		}
		r.registry.mutex.Unlock()
		_, releaseErr := run.release(r.modifierPool)
		return nil, fmt.Errorf("enemyHasterBuffSchedule: %w", errors.Join(scheduleErr, releaseErr))
	}
	run.cancel = cancel
	return startPackets, nil
}

func isCampaignNPCSecondaryPursuit(abilityName string) bool {
	switch abilityName {
	case "GhostlyBoltFlee", "NomadBioSpecialTwoJumpAttack", "Smash",
		"StealthAttack", "RezMelee":
		return true
	default:
		return false
	}
}

func (r campaignNPCActionRuntime) applyEnemyAreaAttackDamage(
	peerSession *gameplayPeerSession,
	generation uint64,
	plan zonenpc.AttackPlan,
	result zonenpc.AttackResult,
	timestamp uint64,
) ([][]byte, sporenet.PlayerStatDelta, bool, error) {
	return r.applyEnemyDamage(
		peerSession, generation, plan, result, timestamp, false, true, false,
	)
}

func (r campaignNPCActionRuntime) isSilenced(
	sessionKey string, generation uint64, objectID uint32,
) bool {
	if sessionKey == "" || generation == 0 || objectID == 0 || r.now == nil {
		return false
	}
	r.registry.mutex.RLock()
	defer r.registry.mutex.RUnlock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	if !isFound || !peerSession.isCampaignNPCSourceActive(generation, objectID) {
		return false
	}
	return peerSession.zone.NPCs().SilenceRemaining(objectID, r.now()) > 0
}

func (r campaignNPCActionRuntime) applyEnemyStatusDamage(
	peerSession *gameplayPeerSession,
	generation uint64,
	plan zonenpc.AttackPlan,
	result zonenpc.AttackResult,
	timestamp uint64,
) ([][]byte, sporenet.PlayerStatDelta, bool, error) {
	return r.applyEnemyDamage(
		peerSession, generation, plan, result, timestamp, true, false, false,
	)
}

func (r campaignNPCActionRuntime) applyEnemyReflectedDamage(
	peerSession *gameplayPeerSession,
	generation uint64,
	plan zonenpc.AttackPlan,
	result zonenpc.AttackResult,
	timestamp uint64,
) ([][]byte, sporenet.PlayerStatDelta, bool, error) {
	return r.applyEnemyDamage(
		peerSession, generation, plan, result, timestamp, false, false, true,
	)
}

// heroTargetSession resolves the connection-local owner of a shared-zone hero.
// The caller holds registry.mutex while using the returned map-backed copy.
func (r campaignNPCActionRuntime) heroTargetSession(
	peerSession *gameplayPeerSession, target zone.NPCTarget,
) (*gameplayPeerSession, string) {
	if peerSession == nil || !target.IsHero {
		return nil, ""
	}
	if target.UserID == peerSession.binding.UserID &&
		target.PeerGeneration == peerSession.generation &&
		target.ObjectID == peerSession.deployedObjectID {
		return peerSession, ""
	}
	for sessionKey, candidate := range r.registry.sessions {
		if candidate.zone != peerSession.zone ||
			candidate.binding.UserID != target.UserID ||
			candidate.generation != target.PeerGeneration ||
			candidate.deployedObjectID != target.ObjectID {
			continue
		}
		targetSession := candidate
		return &targetSession, sessionKey
	}
	return nil, ""
}

func (r campaignNPCActionRuntime) commitHeroTargetDamage(
	peerSession *gameplayPeerSession,
	targetSession *gameplayPeerSession,
	targetSessionKey string,
	hitPackets [][]byte,
	damage zone.NPCTargetDamage,
	distribution soulLinkDistribution,
	timestamp uint64,
) ([][]byte, sporenet.PlayerStatDelta, error) {
	if targetSession == nil {
		return hitPackets, sporenet.PlayerStatDelta{},
			errors.New("enemy target session unavailable")
	}
	// Reserve heroes must take their share before a lethal active hit selects
	// a replacement or decides whether the whole squad has been defeated.
	sharePackets, sharedDamage, err := distribution.commit(targetSession)
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("targetSoulLink: %w", err)
	}
	isLocalTarget := targetSession == peerSession
	transitionPackets := hitPackets
	if !isLocalTarget {
		transitionPackets = append([][]byte(nil), hitPackets...)
	}
	transitionPackets = append(transitionPackets, sharePackets...)
	packets, statDelta, err := targetSession.applyCampaignCommittedDamageHitPackets(
		transitionPackets, damage.PreviousHitPoint, damage.HitPoint,
		timestamp, r.now(),
	)
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("targetTransition: %w", err)
	}
	statDelta.PVEDamageTaken += float64(sharedDamage)
	if isLocalTarget {
		return packets, statDelta, nil
	}
	targetSession.queuePackets(packets)
	targetSession.queueStatDelta(statDelta)
	r.registry.sessions[targetSessionKey] = *targetSession
	// The target receives its private squad transition above. Preserve the
	// shared death presentation too, including removal of Sage's Dendrones,
	// for the NPC simulation's client and its normal teammate broadcast.
	return gameplayPeerPresentationPackets(packets), sporenet.PlayerStatDelta{}, nil
}

func (r campaignNPCActionRuntime) deliverHeroTargetPackets(
	peerSession *gameplayPeerSession,
	targetSession *gameplayPeerSession,
	targetSessionKey string,
	packets [][]byte,
	statDelta sporenet.PlayerStatDelta,
) ([][]byte, sporenet.PlayerStatDelta, error) {
	if targetSession == nil || targetSession == peerSession {
		return packets, statDelta, nil
	}
	targetSession.queuePackets(packets)
	targetSession.queueStatDelta(statDelta)
	r.registry.sessions[targetSessionKey] = *targetSession
	return packets, sporenet.PlayerStatDelta{}, nil
}

func (r campaignNPCActionRuntime) commitHeroTargetReactions(
	peerSession *gameplayPeerSession,
	targetSession *gameplayPeerSession,
	targetSessionKey string,
	packets [][]byte,
	statDelta sporenet.PlayerStatDelta,
	damage zone.NPCTargetDamage,
	plan zonenpc.AttackPlan,
	timestamp uint64,
) ([][]byte, sporenet.PlayerStatDelta, error) {
	// The transition may already have deployed a reserve hero. Reactions
	// belong only to the surviving hero who actually received this hit.
	if targetSession == nil || damage.HitPoint <= 0 ||
		damage.PreviousHitPoint <= damage.HitPoint ||
		targetSession.deployedObjectID != plan.TargetObjectID {
		return packets, statDelta, nil
	}
	plasmaPackets, err := r.triggerPlasmaSentinelActive(targetSession, timestamp)
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("plasmaSentinel: %w", err)
	}
	reactionPackets := plasmaPackets
	err = reserveQuantumStateReactionLocked(
		targetSession, targetSession.deployedObjectID, plan.SourceObjectID, timestamp,
	)
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("quantumState: %w", err)
	}
	if damage.HitPoint > 0 {
		targetSession.applyPassiveDamageTaken(r.now())
	}
	if targetSession == peerSession {
		return append(packets, reactionPackets...), statDelta, nil
	}
	targetSession.queuePackets(reactionPackets)
	r.registry.sessions[targetSessionKey] = *targetSession
	return append(packets, reactionPackets...), statDelta, nil
}

func shieldCombatEvent(
	sourceObjectID uint32, targetObjectID uint32, damage float32,
	absorbedAmount float32, hitPoint float32, isCritical bool,
) ([]byte, error) {
	if sourceObjectID == 0 || targetObjectID == 0 || damage < 0 ||
		absorbedAmount <= 0 || hitPoint < 0 ||
		math.IsNaN(float64(damage)) || math.IsInf(float64(damage), 0) ||
		math.IsNaN(float64(absorbedAmount)) || math.IsInf(float64(absorbedAmount), 0) ||
		math.IsNaN(float64(hitPoint)) || math.IsInf(float64(hitPoint), 0) {
		return nil, errors.New("shield combat event invalid")
	}
	flags := uint16(0)
	if damage > 0 {
		flags |= 0x0001
	}
	if hitPoint == 0 && damage > 0 {
		flags |= 0x0004
	}
	if isCritical {
		flags |= 0x0008
	}
	packet, err := raknet.MarshalApplication(raknet.CombatEventMessage{
		Flags: flags, DeltaHealth: damage, AbsorbedAmount: absorbedAmount,
		TargetID: targetObjectID, SourceID: sourceObjectID,
		IntegerHPChange: -int32(damage),
	})
	if err != nil {
		return nil, fmt.Errorf("shieldEventMarshal: %w", err)
	}
	return packet, nil
}

func replaceShieldCombatEvent(
	packets [][]byte, sourceObjectID uint32, targetObjectID uint32,
	damage float32, absorbedAmount float32, hitPoint float32, isCritical bool,
) ([][]byte, error) {
	eventPacket, err := shieldCombatEvent(
		sourceObjectID, targetObjectID, damage, absorbedAmount, hitPoint, isCritical,
	)
	if err != nil {
		return nil, fmt.Errorf("shieldEvent: %w", err)
	}
	replacedPackets := append([][]byte(nil), packets...)
	isReplaced := false
	for index, packet := range replacedPackets {
		if len(packet) == 0 || packet[0] != byte(raknet.CombatEvent) {
			continue
		}
		replacedPackets[index] = eventPacket
		isReplaced = true
		break
	}
	if !isReplaced {
		return nil, errors.New("shield combat event missing")
	}
	return replacedPackets, nil
}

func (r campaignNPCActionRuntime) applyEnemyDamage(
	peerSession *gameplayPeerSession,
	generation uint64,
	plan zonenpc.AttackPlan,
	result zonenpc.AttackResult,
	timestamp uint64,
	isRetainedStatus bool, isAreaAttack bool, isDefeatedSourceAllowed bool,
) ([][]byte, sporenet.PlayerStatDelta, bool, error) {
	if peerSession == nil || peerSession.zone == nil || r.now == nil {
		return nil, sporenet.PlayerStatDelta{}, false,
			errors.New("enemy damage runtime unavailable")
	}
	if !isRetainedStatus && !isDefeatedSourceAllowed &&
		!peerSession.isCampaignNPCActionActiveAt(
			generation, plan.SourceObjectID, r.now(),
		) {
		return nil, sporenet.PlayerStatDelta{}, false, nil
	}
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, plan.TargetObjectID,
	)
	if !isTargetFound || target.ObjectID != plan.TargetObjectID {
		return nil, sporenet.PlayerStatDelta{}, false, nil
	}
	targetSession, targetSessionKey := r.heroTargetSession(peerSession, target)
	if target.IsHero && targetSession == nil {
		return nil, sporenet.PlayerStatDelta{}, false, nil
	}
	defenseReq := enemyDefenseRequest(plan.Profile, result.Damage, isAreaAttack, isRetainedStatus)
	plan.Profile.DamageSource = defenseReq.DamageSource
	defenseFlags := uint16(0)
	if target.IsHero {
		var defenseErr error
		defenseFlags, defenseErr = targetSession.rollDefense(
			peerSession.zone.NPCRandom(), defenseReq, r.program.Critical,
		)
		if defenseErr != nil {
			return nil, sporenet.PlayerStatDelta{}, false,
				fmt.Errorf("enemyDefense: %w", defenseErr)
		}
	}
	if defenseFlags != 0 {
		feedbackPacket, feedbackErr := defenseFeedback(plan.SourceObjectID, target.ObjectID, defenseFlags)
		if feedbackErr != nil {
			return nil, sporenet.PlayerStatDelta{}, false,
				fmt.Errorf("enemyDefenseFeedback: %w", feedbackErr)
		}
		packets, statDelta, err := targetSession.applyCampaignDamageHitPackets(
			nil, plan.SourceObjectID, target.HitPoint, timestamp, r.now(),
		)
		if err != nil {
			return nil, sporenet.PlayerStatDelta{}, false,
				fmt.Errorf("enemyDamageDodge: %w", err)
		}
		packets = append(packets, feedbackPacket)
		packets, statDelta, err = r.deliverHeroTargetPackets(
			peerSession, targetSession, targetSessionKey, packets, statDelta,
		)
		return packets, statDelta, false, err
	}
	if target.IsHero && (targetSession.heroModifierRun.IsDamageImmune() ||
		targetSession.heroQuantumBlink != nil) {
		immunePacket, immunityErr := npcraknet.Immune(
			plan.SourceObjectID, target.ObjectID,
		)
		if immunityErr != nil {
			return nil, sporenet.PlayerStatDelta{}, false,
				fmt.Errorf("enemyDamageImmuneMarshal: %w", immunityErr)
		}
		packets, statDelta, immunityErr := targetSession.applyCampaignDamageHitPackets(
			[][]byte{immunePacket}, plan.SourceObjectID, target.HitPoint, timestamp, r.now(),
		)
		if immunityErr != nil {
			return nil, sporenet.PlayerStatDelta{}, false,
				fmt.Errorf("enemyDamageImmunity: %w", immunityErr)
		}
		packets, statDelta, immunityErr = r.deliverHeroTargetPackets(
			peerSession, targetSession, targetSessionKey, packets, statDelta,
		)
		return packets, statDelta, true, immunityErr
	}
	if target.IsHero {
		result.Damage = targetSession.applySameGenetypeDamage(
			result.Damage, plan.Profile.DamageType,
			plan.Profile.IsDamageProfileKnown,
		)
	}
	var vulnerabilityRun *campaignNPCPhysicalVulnerabilityRun
	result.Damage, vulnerabilityRun = peerSession.preparePhysicalVulnerability(
		plan.TargetObjectID, result.Damage, plan.Profile.DamageSource,
	)
	result.Damage = peerSession.applyCampaignNPCEnergyVulnerability(
		plan.TargetObjectID, result.Damage, plan.Profile.DamageSource,
	)
	distribution := soulLinkDistribution{activeDamage: result.Damage}
	shieldPackets := [][]byte(nil)
	absorbedAmount := float32(0)
	defenseReq.Damage = result.Damage
	if target.IsHero {
		result.Damage = targetSession.applyPassiveDamageReduction(
			result.Damage, plan.Profile.DamageSource, plan.SourceObjectID, r.now(),
		)
	}
	result.Damage = r.applyCrushingDreadReduction(
		*peerSession, plan.SourceObjectID, result.Damage,
		plan.Profile.DamageSource,
	)
	result.Damage = r.reduceCompanionDamage(*peerSession, target, result.Damage, defenseReq)
	if target.IsHero {
		result.Damage = combat.ReduceIncomingDamage(result.Damage, targetSession.equipmentDefense(), defenseReq)
		if result.Damage <= 0 {
			feedbackPacket, feedbackErr := defenseFeedback(plan.SourceObjectID, target.ObjectID, 0x40)
			if feedbackErr != nil {
				return nil, sporenet.PlayerStatDelta{}, false, fmt.Errorf("blockedFeedback: %w", feedbackErr)
			}
			blockedPackets := [][]byte{feedbackPacket}

			packets, statDelta, deliveryErr := r.deliverHeroTargetPackets(
				peerSession, targetSession, targetSessionKey, blockedPackets, sporenet.PlayerStatDelta{},
			)
			if deliveryErr != nil {
				return nil, sporenet.PlayerStatDelta{}, false, fmt.Errorf("blockedDeliver: %w", deliveryErr)
			}
			return packets, statDelta, false, nil
		}
		shield, shieldErr := targetSession.absorbTCShield(
			result.Damage, r.now(), r.effectPool,
		)
		if shieldErr != nil {
			return nil, sporenet.PlayerStatDelta{}, false,
				fmt.Errorf("enemyDamageShield: %w", shieldErr)
		}
		result.Damage = shield.Damage
		absorbedAmount = shield.Absorbed
		shieldPackets = append(shieldPackets, shield.Packets...)
		if result.Damage <= 0 {
			absorbPacket, absorbErr := shieldCombatEvent(
				plan.SourceObjectID, target.ObjectID, 0, shield.Absorbed,
				target.HitPoint, result.IsCritical,
			)
			if absorbErr != nil {
				return nil, sporenet.PlayerStatDelta{}, false,
					fmt.Errorf("enemyDamageAbsorbMarshal: %w", absorbErr)
			}
			shieldPackets = append(shieldPackets, absorbPacket)
			packets, statDelta, hitErr := targetSession.applyCampaignDamageHitPackets(
				shieldPackets, plan.SourceObjectID, target.HitPoint, timestamp, r.now(),
			)
			if hitErr != nil {
				return nil, sporenet.PlayerStatDelta{}, false,
					fmt.Errorf("enemyDamageShieldHit: %w", hitErr)
			}
			packets, statDelta, hitErr = r.deliverHeroTargetPackets(
				peerSession, targetSession, targetSessionKey, packets, statDelta,
			)
			return packets, statDelta, true, hitErr
		}
		preparedDistribution, distributionErr :=
			targetSession.prepareSoulLinkDamage(result.Damage)
		if distributionErr != nil {
			return nil, sporenet.PlayerStatDelta{}, false,
				fmt.Errorf("enemyDamageSoulLink: %w", distributionErr)
		}
		distribution = preparedDistribution
		result.Damage = distribution.activeDamage
	}
	owner := zonenpc.ActionOwner{
		UserID:         peerSession.binding.UserID,
		PeerGeneration: generation,
	}
	damage := zone.NPCTargetDamage{}
	isApplied := false
	var err error
	if isDefeatedSourceAllowed {
		damage, isApplied, err = peerSession.zone.ApplyNPCReflectedTargetDamage(
			owner, plan.SourceObjectID, plan.TargetObjectID, result.Damage,
		)
	} else if isRetainedStatus {
		damage, isApplied, err = peerSession.zone.ApplyNPCTargetStatusDamage(
			owner, plan.SourceObjectID, plan.TargetObjectID, result.Damage,
		)
	} else if isAreaAttack {
		damage, isApplied, err = peerSession.zone.ApplyNPCAreaTargetDamage(
			owner, plan.SourceObjectID, plan.TargetObjectID, result.Damage,
			r.now(),
		)
	} else {
		damage, isApplied, err = peerSession.zone.ApplyNPCTargetDamage(
			owner, plan.SourceObjectID, plan.TargetObjectID, result.Damage,
			r.now(),
		)
	}
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, false,
			fmt.Errorf("enemyDamageCommit: %w", err)
	}
	if !isApplied {
		return nil, sporenet.PlayerStatDelta{}, false, nil
	}
	vulnerabilityPackets, err := peerSession.commitPhysicalVulnerability(
		plan.TargetObjectID, vulnerabilityRun, r.modifierPool,
	)
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, false,
			fmt.Errorf("enemyDamageVulnerability: %w", err)
	}
	if damage.IsDefeated {
		peerSession.retirePhysicalVulnerability(
			plan.TargetObjectID, r.modifierPool,
		)
	}
	energyVulnerabilityPackets := [][]byte(nil)
	if damage.IsDefeated {
		energyVulnerabilityPackets, err =
			peerSession.retireCampaignNPCEnergyVulnerability(
				plan.TargetObjectID, r.modifierPool,
			)
		if err != nil {
			return nil, sporenet.PlayerStatDelta{}, false,
				fmt.Errorf("enemyDamageEnergyVulnerability: %w", err)
		}
	}
	hitPackets, err := npcraknet.AttackHit(
		plan, result, damage.HitPoint,
	)
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, false,
			fmt.Errorf("enemyDamageMarshal: %w", err)
	}
	if absorbedAmount > 0 {
		hitPackets, err = replaceShieldCombatEvent(
			hitPackets, plan.SourceObjectID, target.ObjectID,
			result.Damage, absorbedAmount, damage.HitPoint, result.IsCritical,
		)
		if err != nil {
			return nil, sporenet.PlayerStatDelta{}, false,
				fmt.Errorf("enemyDamageAbsorbReplace: %w", err)
		}
	}
	hitPackets = append(shieldPackets, hitPackets...)
	hitPackets = append(hitPackets, vulnerabilityPackets...)
	hitPackets = append(hitPackets, energyVulnerabilityPackets...)
	if !damage.IsHero {
		if damage.IsDefeated {
			trapPackets, isTrapHandled, trapErr :=
				peerSession.breakHeroTrap(plan.TargetObjectID)
			if trapErr != nil {
				return nil, sporenet.PlayerStatDelta{}, false,
					fmt.Errorf("enemyDamageTrap: %w", trapErr)
			}
			if isTrapHandled {
				return append(hitPackets, trapPackets...),
					sporenet.PlayerStatDelta{}, true, nil
			}
			deathPackets, deathErr := r.companionDefeat(
				peerSession, plan.TargetObjectID, timestamp,
			)
			if deathErr != nil {
				return nil, sporenet.PlayerStatDelta{}, false, fmt.Errorf("companionDeath: %w", deathErr)
			}
			hitPackets = append(hitPackets, deathPackets...)
		}
		return hitPackets, sporenet.PlayerStatDelta{}, true, nil
	}
	packets, statDelta, err := r.commitHeroTargetDamage(
		peerSession, targetSession, targetSessionKey, hitPackets, damage, distribution, timestamp,
	)
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, false,
			fmt.Errorf("enemyDamageTransition: %w", err)
	}
	packets, statDelta, err = r.commitHeroTargetReactions(
		peerSession, targetSession, targetSessionKey, packets, statDelta,
		damage, plan, timestamp,
	)
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, false,
			fmt.Errorf("enemyDamageReaction: %w", err)
	}
	return packets, statDelta, true, nil
}

func (r campaignNPCActionRuntime) applyEnemyProjectileTeleport(
	peerSession *gameplayPeerSession,
	plan zonenpc.AttackPlan,
	target zone.NPCTarget,
	targetHitPoint float32,
	timestamp uint64,
) ([][]byte, error) {
	profile := plan.Profile
	if profile.TeleportEffectName == "" || targetHitPoint <= 0 {
		return nil, nil
	}
	if peerSession == nil || peerSession.zone == nil ||
		target.ObjectID != plan.TargetObjectID ||
		peerSession.zone.NPCRandom() == nil {
		return nil, nil
	}
	var targetSession *gameplayPeerSession
	targetSessionKey := ""
	if target.IsHero && target.UserID == peerSession.binding.UserID &&
		target.PeerGeneration == peerSession.generation &&
		peerSession.deployedObjectID == target.ObjectID {
		targetSession = peerSession
	} else if target.IsHero {
		for sessionKey, candidate := range r.registry.sessions {
			if candidate.zone != peerSession.zone ||
				candidate.binding.UserID != target.UserID ||
				candidate.generation != target.PeerGeneration ||
				candidate.deployedObjectID != target.ObjectID {
				continue
			}
			targetSessionCopy := candidate
			targetSession = &targetSessionCopy
			targetSessionKey = sessionKey
			break
		}
	}
	if target.IsHero && targetSession == nil {
		return nil, nil
	}
	if !target.IsHero {
		companion, isCompanionFound :=
			peerSession.zone.Companion().Snapshot(target.ObjectID)
		if !isCompanionFound || companion.HitPoint <= 0 {
			return nil, nil
		}
	}
	sourcePosition := target.Position
	footprintRadius := max(target.FootprintRadius, float32(0.25))
	if target.IsHero {
		sourcePosition = game.Vec3(targetSession.playerPosition)
		footprintRadius = targetSession.deployedCampaignFootprintRadius()
	}
	destination, isFound, err := zonenavigation.ConnectedTeleportDestination(
		peerSession.zone.Navigation(), peerSession.zone.NPCRandom(),
		zonenavigation.ConnectedTeleportRequest{
			SourcePosition:  sourcePosition,
			FootprintRadius: footprintRadius,
			Radius:          profile.TeleportNormalDistance,
			MinimumDistance: profile.TeleportMinimumDistance,
			AttemptCount:    20,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("destinationSelect: %w", err)
	}
	if !isFound {
		return nil, nil
	}
	packets, err := npcraknet.RandomTeleport(plan, destination, timestamp)
	if err != nil {
		return nil, fmt.Errorf("targetProject: %w", err)
	}
	if !target.IsHero {
		err = peerSession.zone.Companion().SetPosition(target.ObjectID, destination)
		if err != nil {
			return nil, fmt.Errorf("targetCompanionMove: %w", err)
		}
		return packets, nil
	}
	previousPosition := targetSession.playerPosition
	motionSnapshot := targetSession.playerMotionSnapshot()
	err = targetSession.teleportPlayer(
		r.now(),
		raknet.Vector3{X: destination.X, Y: destination.Y, Z: destination.Z},
	)
	if err != nil {
		return nil, fmt.Errorf("targetMove: %w", err)
	}
	err = targetSession.syncZoneHeroPose()
	if err != nil {
		if targetSession.playerMotion == nil {
			targetSession.playerPosition = previousPosition
		} else {
			targetSession.restorePlayerMotion(
				motionSnapshot, targetSession.playerMotionRevision(),
			)
		}
		return nil, fmt.Errorf("targetSync: %w", err)
	}
	if targetSession != peerSession {
		r.registry.sessions[targetSessionKey] = *targetSession
	}
	return packets, nil
}

type campaignNPCPushPullSchedule struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	objectID   uint32
	timestamp  uint64
	plan       zonenpc.AttackPlan
}

const campaignNPCPullEffectDuration = 400 * time.Millisecond

type campaignNPCPullEffectSchedule struct {
	runtime  campaignNPCActionRuntime
	objectID uint32
	slot     uint8
}

type campaignNPCPushPullResumeSchedule struct {
	schedule  campaignNPCPushPullSchedule
	timestamp uint64
}

func (e campaignNPCPushPullResumeSchedule) produce() ([][]byte, error) {
	return e.schedule.resume(e.timestamp)
}

func (e campaignNPCPullEffectSchedule) remove() ([][]byte, error) {
	packet, err := npcraknet.ForcedMovementEffect(
		e.objectID, e.slot, "", true,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyPullEffectRemove: %w", err)
	}
	if !e.runtime.effectPool.Release(e.objectID, e.slot) {
		return [][]byte{packet}, nil
	}
	return [][]byte{packet}, nil
}

func (e campaignNPCPushPullSchedule) resume(
	timestamp uint64,
) ([][]byte, error) {
	if e.plan.ActionGeneration != 0 {
		e.runtime.registry.mutex.RLock()
		peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
		isCurrent := isFound && peerSession.isCampaignNPCSourceGenerationActive(
			e.generation, e.objectID, e.plan.ActionGeneration,
		)
		e.runtime.registry.mutex.RUnlock()
		if !isCurrent {
			return nil, nil
		}
	}
	return e.runtime.producePushPull(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
}

func (e campaignNPCPushPullSchedule) next() ([][]byte, error) {
	timestamp := e.timestamp + uint64(e.plan.Profile.ReleaseDelay/time.Millisecond)
	return e.runtime.produceBoundedStrafeOrIdle(
		e.packet, e.sessionKey, e.generation, e.objectID,
		e.plan.ActionGeneration, timestamp, e.plan.Profile,
		campaignNPCStrafeModeRetreat,
		e.resumeAfterCooldown,
	)
}

func (e campaignNPCPushPullSchedule) resumeAfterCooldown(
	timestamp uint64,
) ([][]byte, error) {
	cooldownTimestamp := e.timestamp +
		uint64(e.plan.Profile.Cooldown/time.Millisecond)
	if timestamp >= cooldownTimestamp {
		return e.resume(timestamp)
	}
	delay := time.Duration(cooldownTimestamp-timestamp) * time.Millisecond
	step := campaignNPCPushPullResumeSchedule{
		schedule: e, timestamp: cooldownTimestamp,
	}
	cancel, err := scheduleNPCProducers(e.runtime.registry, e.packet, []raknet.ScheduledPacketProducer{{
		Delay: delay, Produce: step.produce,
	}})
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		return nil, fmt.Errorf("enemyPushPullCooldownSchedule: %w", err)
	}
	return nil, nil
}

func (e campaignNPCPushPullSchedule) hit() ([][]byte, error) {
	runtime := e.runtime
	runtime.registry.mutex.Lock()
	current, isFound := runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && current.isCampaignNPCSourceGenerationActive(
		e.generation, e.objectID, e.plan.ActionGeneration,
	)
	if isCurrent {
		isCurrent = current.zone.NPCs().StunRemaining(e.objectID, runtime.now()) == 0 &&
			current.zone.NPCs().SleepRemaining(e.objectID, runtime.now()) == 0 &&
			current.zone.NPCs().FearRemaining(e.objectID, runtime.now()) == 0
	}
	if !isCurrent {
		runtime.registry.mutex.Unlock()
		return nil, nil
	}
	enemy, isEnemyFound := current.zone.NPCs().NPC(e.objectID)
	if !isEnemyFound {
		runtime.registry.mutex.Unlock()
		return nil, nil
	}
	var packets [][]byte
	statDelta := sporenet.PlayerStatDelta{}
	if !e.plan.Profile.IsPull {
		if current.zone.NPCRandom() == nil {
			runtime.registry.mutex.Unlock()
			return nil, errors.New("enemy push random unavailable")
		}
		hitTimestamp := e.timestamp +
			uint64(e.plan.Profile.HitDelay/time.Millisecond)
		for _, target := range campaignLobAreaTargets(
			current.zone.LiveNPCTargets(), enemy.Plan.Position,
			e.plan.Profile.Radius,
		) {
			currentPlan, planErr := zonenpc.PlanAreaAttackWithProfile(
				enemy, target.ObjectID, target.Position, e.plan.Profile,
			)
			if planErr != nil {
				continue
			}
			result, commitErr := zonenpc.CommitAttack(
				current.zone.NPCRandom(), currentPlan,
				enemy.Plan.NPCProfile.CriticalRating, runtime.program.Critical,
			)
			if commitErr != nil {
				runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("enemyPushCommit: %w", commitErr)
			}
			hitPackets, targetStatDelta, isApplied, damageErr :=
				runtime.applyEnemyAreaAttackDamage(
					&current, e.generation, currentPlan, result, hitTimestamp,
				)
			if damageErr != nil {
				runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("enemyPushDamage: %w", damageErr)
			}
			runtime.reserveSageCompanionRespawnLocked(
				&current, e.sessionKey, e.generation, currentPlan.TargetObjectID,
			)
			packets = append(packets, hitPackets...)
			statDelta.PVEDamageTaken += targetStatDelta.PVEDamageTaken
			if !isApplied {
				continue
			}
			forcedPackets, forcedErr := runtime.applyEnemyForcedMovement(
				&current, currentPlan, target, hitTimestamp,
			)
			if forcedErr != nil {
				runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("enemyPushMovement: %w", forcedErr)
			}
			packets = append(packets, forcedPackets...)
		}
		binding := current.binding
		runtime.registry.sessions[e.sessionKey] = current
		runtime.registry.mutex.Unlock()
		err := runtime.stats.Record(context.Background(), binding, statDelta)
		if err != nil {
			return nil, fmt.Errorf("enemyPushStats: %w", err)
		}
		return packets, nil
	}
	target, isTargetFound := current.campaignNPCTarget(
		e.generation, e.plan.TargetObjectID,
	)
	if !isTargetFound || target.HitPoint <= 0 {
		runtime.registry.mutex.Unlock()
		return nil, nil
	}
	_, planErr := zonenpc.PlanControlWithProfile(
		enemy, target.ObjectID, target.Position, e.plan.Profile,
		target.FootprintRadius,
	)
	if planErr != nil {
		runtime.registry.mutex.Unlock()
		return nil, nil
	}
	movementPlan := e.plan
	movementPlan.Profile.ForcedMovementEffectName = ""
	forcedPackets, err := runtime.applyEnemyForcedMovement(
		&current, movementPlan, target,
		e.timestamp+uint64(e.plan.Profile.HitDelay/time.Millisecond),
	)
	if err != nil {
		runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyForcedMovement: %w", err)
	}
	packets = append(packets, forcedPackets...)
	if len(forcedPackets) != 0 && runtime.effectPool != nil &&
		e.plan.Profile.ForcedMovementEffectName != "" {
		effectSlot, isEffectAllocated :=
			runtime.effectPool.Allocate(target.ObjectID)
		if isEffectAllocated {
			effectPacket, effectErr := npcraknet.ForcedMovementEffect(
				target.ObjectID, effectSlot,
				e.plan.Profile.ForcedMovementEffectName, false,
			)
			if effectErr != nil {
				runtime.effectPool.Release(target.ObjectID, effectSlot)
				runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("enemyPullEffectStart: %w", effectErr)
			}
			packets = append(packets, effectPacket)
			cleanup := campaignNPCPullEffectSchedule{
				runtime: runtime, objectID: target.ObjectID, slot: effectSlot,
			}
			cancel, scheduleErr := scheduleNPCProducers(e.runtime.registry, e.packet,
				[]raknet.ScheduledPacketProducer{{
					Delay: campaignNPCPullEffectDuration, Produce: cleanup.remove,
				}},
			)
			if scheduleErr == nil && cancel == nil {
				scheduleErr = errors.New("nil cancellation")
			}
			if scheduleErr != nil {
				runtime.effectPool.Release(target.ObjectID, effectSlot)
				runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("enemyPullEffectSchedule: %w", scheduleErr)
			}
		}
	}
	runtime.registry.sessions[e.sessionKey] = current
	runtime.registry.mutex.Unlock()
	return packets, nil
}

func (r campaignNPCActionRuntime) applyEnemyForcedMovement(
	peerSession *gameplayPeerSession,
	plan zonenpc.AttackPlan,
	target zone.NPCTarget,
	timestamp uint64,
) ([][]byte, error) {
	if peerSession == nil || peerSession.zone == nil || target.ObjectID == 0 ||
		target.HitPoint <= 0 {
		return nil, nil
	}
	targetSession := peerSession
	targetSessionKey := ""
	if target.IsHero && (target.UserID != peerSession.binding.UserID ||
		target.PeerGeneration != peerSession.generation ||
		peerSession.deployedObjectID != target.ObjectID) {
		targetSession = nil
		for sessionKey, candidate := range r.registry.sessions {
			if candidate.zone != peerSession.zone ||
				candidate.binding.UserID != target.UserID ||
				candidate.generation != target.PeerGeneration ||
				candidate.deployedObjectID != target.ObjectID {
				continue
			}
			targetSessionCopy := candidate
			targetSession = &targetSessionCopy
			targetSessionKey = sessionKey
			break
		}
	}
	if target.IsHero && targetSession == nil {
		return nil, nil
	}
	if !target.IsHero {
		companion, isCompanionFound :=
			peerSession.zone.Companion().Snapshot(target.ObjectID)
		if !isCompanionFound || companion.HitPoint <= 0 {
			return nil, nil
		}
	}
	if target.IsHero && targetSession.isEnemyRootActive(r.now()) {
		return nil, nil
	}
	deltaX := target.Position.X - plan.SourcePosition.X
	deltaY := target.Position.Y - plan.SourcePosition.Y
	length := float32(math.Sqrt(float64(deltaX*deltaX + deltaY*deltaY)))
	if length == 0 {
		deltaX = 1
		length = 1
	}
	directionX := deltaX / length
	directionY := deltaY / length
	desired := target.Position
	if plan.Profile.IsPull && plan.Profile.ForcedMovementDuration > 0 {
		travelDistance := min(
			length,
			plan.Profile.ForcedMovementSpeed*
				float32(plan.Profile.ForcedMovementDuration)/float32(time.Second),
		)
		desired.X -= directionX * travelDistance
		desired.Y -= directionY * travelDistance
	} else if plan.Profile.IsPull {
		desired.X = plan.SourcePosition.X + directionX*plan.Profile.ForcedMovementStopDistance
		desired.Y = plan.SourcePosition.Y + directionY*plan.Profile.ForcedMovementStopDistance
	} else {
		desired.X += directionX * plan.Profile.ForcedMovementDistance
		desired.Y += directionY * plan.Profile.ForcedMovementDistance
	}
	destination, isFound, err := navigationClippedMovementDestination(
		peerSession.zone.Navigation(), target.Position, desired,
		max(target.FootprintRadius, float32(0.25)),
	)
	if err != nil {
		return nil, fmt.Errorf("destinationSelect: %w", err)
	}
	if !isFound {
		return nil, nil
	}
	packets, err := npcraknet.ForcedMovement(plan, destination, timestamp)
	if err != nil {
		return nil, fmt.Errorf("movementProject: %w", err)
	}
	if !target.IsHero {
		err = peerSession.zone.Companion().SetPosition(target.ObjectID, destination)
		if err != nil {
			return nil, fmt.Errorf("targetCompanionMove: %w", err)
		}
		peerSession.zone.PublishNPCForcedMovement(
			zonenpc.ForcedMovementEvent{
				Plan: plan, Destination: destination, Timestamp: timestamp,
			},
			peerSession.binding.UserID, peerSession.generation,
		)
		return packets, nil
	}
	previousPosition := targetSession.playerPosition
	motionSnapshot := targetSession.playerMotionSnapshot()
	err = targetSession.teleportPlayer(
		r.now(), raknet.Vector3(destination),
	)
	if err == nil {
		err = targetSession.syncZoneHeroPose()
	}
	if err != nil {
		if targetSession.playerMotion == nil {
			targetSession.playerPosition = previousPosition
		} else {
			targetSession.restorePlayerMotion(
				motionSnapshot, targetSession.playerMotionRevision(),
			)
		}
		return nil, fmt.Errorf("targetHeroMove: %w", err)
	}
	if targetSession != peerSession {
		r.registry.sessions[targetSessionKey] = *targetSession
	}
	peerSession.zone.PublishNPCForcedMovement(
		zonenpc.ForcedMovementEvent{
			Plan: plan, Destination: destination, Timestamp: timestamp,
		},
		peerSession.binding.UserID, peerSession.generation,
	)
	targetSession.playerMovementGoal = targetSession.playerPosition
	interruptedBasic := targetSession.interruptBasicForMovement()
	if interruptedBasic != nil {
		interruptedBasic.Stop()
	}
	resourcePackets, resourceErr := targetSession.forcedMovementResources()
	if resourceErr != nil {
		return nil, fmt.Errorf("forcedResources: %w", resourceErr)
	}
	packets = append(packets, resourcePackets...)
	if targetSession != peerSession {
		r.registry.sessions[targetSessionKey] = *targetSession
	}
	return packets, nil
}

func (r campaignNPCActionRuntime) producePushPull(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, error) {
	resume := campaignNPCPushPullSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
	}
	isDeferred, err := r.pursuit.deferAction(
		packet, sessionKey, generation, objectID, timestamp, resume.resume,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyPushPullStun: %w", err)
	}
	if isDeferred {
		return nil, nil
	}
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(generation, objectID)
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	if !isEnemyFound || enemy.IsDefeated {
		r.registry.mutex.Unlock()
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	peerSession, err = r.pursuit.advanceTargetPoseLocked(
		peerSession, enemy.TargetObjectID,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyPushPullTargetPose: %w", err)
	}
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, enemy.TargetObjectID,
	)
	if !isTargetFound {
		r.registry.mutex.Unlock()
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	distance := zonegeometry.Distance(enemy.Plan.Position, target.Position)
	profile, err := zonenpc.ZelemSpecialTwoActionProfile(distance)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyPushPullProfile: %w", err)
	}
	plan, planErr := zonenpc.PlanControlWithProfile(
		enemy, target.ObjectID, target.Position, profile, target.FootprintRadius,
	)
	if planErr != nil {
		decisionProfile, isProfileFound := zonenpc.ActionProfileForPlan(enemy.Plan)
		if !isProfileFound {
			r.registry.mutex.Unlock()
			return nil, nil
		}
		action, actionErr := campaignNPCActionWithProfile(
			enemy.Plan, target.ObjectID, target.Position,
			decisionProfile, target.FootprintRadius,
		)
		if actionErr != nil || !action.IsPursuitNeeded {
			r.registry.mutex.Unlock()
			return nil, nil
		}
		pursuitPackets, marshalErr := npcraknet.Pursuit(action)
		if marshalErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("enemyPushPullPursuitMarshal: %w", marshalErr)
		}
		r.registry.mutex.Unlock()
		scheduleErr := r.pursuit.schedule(
			packet, sessionKey, generation, objectID, timestamp,
			action.TargetPosition, action.Profile, resume.resume,
		)
		if scheduleErr != nil {
			r.releaseAction(sessionKey, generation, objectID)
			r.logger.Printf(
				"RakNet campaign push/pull pursuit not scheduled object=%d: %v",
				objectID, scheduleErr,
			)
		}
		return pursuitPackets, nil
	}
	if !profile.IsPull {
		damagePlan, damagePlanErr := zonenpc.PlanAttackWithProfile(
			enemy, target.ObjectID, target.Position, profile,
			target.FootprintRadius,
		)
		if damagePlanErr != nil {
			r.registry.mutex.Unlock()
			return nil, nil
		}
		plan = damagePlan
	}
	startPackets, err := marshalNPCAttack(peerSession.zone.NPCs(), plan, timestamp)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyPushPullStart: %w", err)
	}
	r.registry.mutex.Unlock()
	schedule := campaignNPCPushPullSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
		plan: plan,
	}
	hitProducer := raknet.ScheduledPacketProducer{
		Delay: profile.HitDelay, Produce: schedule.hit,
	}
	nextProducer := raknet.ScheduledPacketProducer{
		Delay: profile.ReleaseDelay, Produce: schedule.next,
	}
	_, scheduleErr := scheduleNPCProducers(r.registry, packet,
		[]raknet.ScheduledPacketProducer{hitProducer, nextProducer},
	)
	if scheduleErr != nil {
		return nil, fmt.Errorf("enemyPushPullSchedule: %w", scheduleErr)
	}
	return startPackets, nil
}

type campaignZelemBlinkSchedule struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	objectID   uint32
	timestamp  uint64
	plan       zonenpc.AttackPlan
}

func (e campaignZelemBlinkSchedule) resume(
	timestamp uint64,
) ([][]byte, error) {
	return e.runtime.produceZelemBlink(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
}

func (e campaignZelemBlinkSchedule) hit() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	current, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && current.isCampaignNPCAttackActiveAt(
		e.generation, e.objectID, e.plan.TargetObjectID, e.runtime.now(),
	)
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	currentNPC, isNPCFound := current.zone.NPCs().NPC(e.objectID)
	target, isTargetFound := current.campaignNPCTarget(
		e.generation, currentNPC.TargetObjectID,
	)
	if !isNPCFound || !isTargetFound {
		e.runtime.registry.mutex.Unlock()
		e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
		return nil, nil
	}
	plan, err := planCampaignNPCZelemBlink(
		currentNPC, target.ObjectID, target.Position, target.FootprintRadius,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	destination, err := campaignNPCZelemBlinkDestination(plan)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyZelemDestination: %w", err)
	}
	err = current.zone.NPCs().SetPosition(e.objectID, destination)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyZelemPosition: %w", err)
	}
	e.runtime.registry.sessions[e.sessionKey] = current
	e.runtime.registry.mutex.Unlock()
	timestamp := e.timestamp + uint64(plan.Profile.HitDelay/time.Millisecond)
	packets, err := npcraknet.Blink(plan, destination, timestamp)
	if err != nil {
		return nil, fmt.Errorf("enemyZelemBlink: %w", err)
	}
	shotPackets, err := e.runtime.produceZelemShotWithProfile(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
		zonenpc.ZelemRangedShotProfile(),
	)
	if err != nil {
		return nil, fmt.Errorf("enemyZelemShot: %w", err)
	}
	return append(packets, shotPackets...), nil
}

func (e campaignZelemBlinkSchedule) next() ([][]byte, error) {
	timestamp := e.timestamp + uint64(e.plan.Profile.Cooldown/time.Millisecond)
	return e.resume(timestamp)
}

func (r campaignNPCActionRuntime) produceZelemBlink(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, error) {
	resume := campaignZelemBlinkSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
	}
	isDeferred, err := r.pursuit.deferAction(
		packet, sessionKey, generation, objectID, timestamp,
		resume.resume,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyZelemBlinkStun: %w", err)
	}
	if isDeferred {
		return nil, nil
	}
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound &&
		peerSession.isCampaignNPCSourceActive(generation, objectID)
	if !isCurrent {
		r.registry.mutex.RUnlock()
		return nil, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, enemy.TargetObjectID,
	)
	r.registry.mutex.RUnlock()
	if !isEnemyFound || enemy.IsDefeated || !isTargetFound {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	plan, err := planCampaignNPCZelemBlink(
		enemy, target.ObjectID, target.Position, target.FootprintRadius,
	)
	if err != nil {
		profile, isProfileFound := zonenpc.ActionProfileForNoun(enemy.Plan.NounName)
		if !isProfileFound {
			return nil, nil
		}
		action, actionErr := campaignNPCActionWithProfile(
			enemy.Plan, target.ObjectID, target.Position, profile,
			target.FootprintRadius,
		)
		if actionErr != nil || !action.IsPursuitNeeded {
			return nil, nil
		}
		pursuitPackets, marshalErr := npcraknet.Pursuit(action)
		if marshalErr != nil {
			return nil, fmt.Errorf("enemyZelemPursuitMarshal: %w", marshalErr)
		}
		scheduleErr := r.pursuit.schedule(
			packet, sessionKey, generation, objectID, timestamp,
			action.TargetPosition, action.Profile, resume.resume,
		)
		if scheduleErr != nil {
			r.releaseAction(sessionKey, generation, objectID)
			r.logger.Printf("RakNet campaign Zelem pursuit not scheduled object=%d: %v", objectID, scheduleErr)
		}
		return pursuitPackets, nil
	}
	startPackets, err := r.startNPCAttack(sessionKey, generation, plan, timestamp)
	if err != nil {
		return nil, fmt.Errorf("enemyZelemBlinkStart: %w", err)
	}
	schedule := campaignZelemBlinkSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
		plan: plan,
	}
	hitProducer := raknet.ScheduledPacketProducer{
		Delay: plan.Profile.HitDelay, Produce: schedule.hit,
	}
	nextProducer := raknet.ScheduledPacketProducer{
		Delay: plan.Profile.Cooldown, Produce: schedule.next,
	}
	_, scheduleErr := scheduleNPCProducers(r.registry, packet,
		[]raknet.ScheduledPacketProducer{hitProducer, nextProducer},
	)
	if scheduleErr != nil {
		return nil, fmt.Errorf("enemyZelemBlinkSchedule: %w", scheduleErr)
	}
	return startPackets, nil
}
