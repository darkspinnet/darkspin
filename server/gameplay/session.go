package gameplay

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/darkspinnet/darkspin/server/squad"
	"github.com/darkspinnet/darkspin/server/util"
	zone "github.com/darkspinnet/darkspin/server/zone"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	actionraknet "github.com/darkspinnet/darkspin/server/zone/action/raknet103"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	companionraknet "github.com/darkspinnet/darkspin/server/zone/companion/raknet103"
	deathraknet "github.com/darkspinnet/darkspin/server/zone/death/raknet103"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonehero "github.com/darkspinnet/darkspin/server/zone/hero"
	heroraknet "github.com/darkspinnet/darkspin/server/zone/hero/raknet103"
	interactraknet "github.com/darkspinnet/darkspin/server/zone/interact/raknet103"
	zonemember "github.com/darkspinnet/darkspin/server/zone/member"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
	outcomeraknet "github.com/darkspinnet/darkspin/server/zone/outcome/raknet103"
	zoneresult "github.com/darkspinnet/darkspin/server/zone/result"
	securityraknet "github.com/darkspinnet/darkspin/server/zone/security/raknet103"
	spawnraknet "github.com/darkspinnet/darkspin/server/zone/spawn/raknet103"
	teleportraknet "github.com/darkspinnet/darkspin/server/zone/teleport/raknet103"
	unlockraknet "github.com/darkspinnet/darkspin/server/zone/unlock/raknet103"
)

type summonPassiveRun = zonecompanion.Passive
type summonPassiveRunInput = zonecompanion.PassiveInput
type summonPassiveWorldRequest = zonecompanion.PassiveRequest
type summonCompanionActivation = companionraknet.Activation
type summonCompanionAttackInput = companionraknet.MeleeInput
type summonCompanionAttackStart = companionraknet.MeleeStart

const zonePlayerMoveSpeed = float32(8.25)
const zonePlayerPoseCorrectionRange = float32(12)
const standardCreatureSwapCooldown = 5 * time.Second
const campaignCreatureWarpOutDelay = 500 * time.Millisecond

// campaignHeroDeathSelectionDelay is derived from the longest playable death
// clip: gen_death_pc_gun_m_tc and gen_death_pc_meditron are 74 frames at
// 30 FPS (about 2.47 seconds). Round that up to three seconds, then retain one
// additional second before presenting the replacement hero.
const campaignHeroDeathSelectionDelay = 4 * time.Second

const pendingPeerPacketLimit = 512

func newSummonPassiveRun(req summonPassiveRunInput) (*summonPassiveRun, error) {
	run, err := zonecompanion.NewPassive(req)
	if err != nil {
		return nil, fmt.Errorf("passiveCreate: %w", err)
	}
	return run, nil
}

func newSummonCompanionAttack(
	req summonCompanionAttackInput,
) (summonCompanionAttackStart, error) {
	start, err := companionraknet.NewMelee(req)
	if err != nil {
		return summonCompanionAttackStart{},
			fmt.Errorf("companionMeleeCreate: %w", err)
	}
	return start, nil
}

func summonPassiveSpawnPosition(
	ownerPosition raknet.Vector3,
	slotIndex int,
	slotCount int,
	radius float32,
) (raknet.Vector3, error) {
	position, err := zonecompanion.PassiveSpawnPosition(
		zonePosition(ownerPosition),
		slotIndex,
		slotCount,
		radius,
	)
	if err != nil {
		return raknet.Vector3{}, fmt.Errorf("passivePosition: %w", err)
	}
	return raknet.Vector3{X: position.X, Y: position.Y, Z: position.Z}, nil
}

func marshalSummonPassiveWorldRequest(
	req summonPassiveWorldRequest,
	position raknet.Vector3,
) ([][]byte, *summonCompanionActivation, error) {
	packets, activation, err := companionraknet.PassiveRequest(req, position)
	if err != nil {
		return nil, nil, fmt.Errorf("passiveMarshal: %w", err)
	}
	return packets, activation, nil
}

func summonCompanionPresentationPackets(packets [][]byte) [][]byte {
	return companionraknet.PresentationPackets(packets)
}

func stopSummonCompanionAttacks(
	activations map[uint32]summonCompanionActivation,
) {
	companionraknet.StopAttacks(activations)
}

func (s *gameplayPeerSession) reserveCampaignObjectID() (uint32, error) {
	if s == nil || s.zone == nil {
		return 0, errors.New("campaign object id session unavailable")
	}
	objectID, err := s.zone.ReserveObjectIDs(1)
	if err != nil {
		return 0, fmt.Errorf("campaignObjectIDReserve: %w", err)
	}
	return objectID, nil
}

func (s *gameplayPeerSession) reserveCampaignProjectileIDs(
	count uint32, minimum uint32,
) (uint32, error) {
	if s == nil || count == 0 || minimum == 0 {
		return 0, errors.New("campaign projectile id reservation invalid")
	}
	if s.zone != nil {
		objectID, nextObjectID, err := s.zone.ReserveProjectileIDs(count)
		if err != nil {
			return 0, fmt.Errorf("campaignProjectileIDReserve: %w", err)
		}
		s.nextProjectileObjectID = nextObjectID
		return objectID, nil
	}
	s.nextProjectileObjectID = max(s.nextProjectileObjectID, minimum)
	if count > uint32(0xffffffff)-s.nextProjectileObjectID {
		return 0, errors.New("campaign projectile id exhausted")
	}
	objectID := s.nextProjectileObjectID
	s.nextProjectileObjectID += count
	return objectID, nil
}

func (s *gameplayPeerSession) restoreCampaignProjectileID(previous uint32) {
	if s == nil || s.zone != nil {
		return
	}
	s.nextProjectileObjectID = previous
}

// zoneEffectPresentation owns connection-local runs that project active zone
// effects to one build-103 client.
type zoneEffectPresentation struct {
	campaignNPCModifiers                map[uint32]*campaignNPCModifierRun
	campaignNPCPoisons                  map[campaignNPCPoisonKey]*campaignNPCPoisonRun
	campaignNPCDiseaseImmunityEnds      map[uint32]uint64
	heroPassivePoisons                  map[uint32]*heroPassivePoisonRun
	heroHitPoisons                      map[heroHitPoisonKey]*heroHitPoisonRun
	heroTimeRavagerSlows                map[uint32]*heroTimeRavagerSlowRun
	heroEnergyVulnerabilities           map[uint32]*heroEnergyVulnerabilityRun
	heroHealingReductions               map[heroHealingReductionKey]*heroHealingReductionRun
	heroTaunts                          map[uint32]*heroTauntRun
	campaignNPCSilences                 map[uint32]*campaignNPCSilenceRun
	campaignNPCEnergyBuffs              map[uint32]*campaignNPCEnergyBuffRun
	campaignNPCEnergyBuffReadiness      map[uint32]uint64
	campaignNPCIntangibles              map[uint32]*campaignNPCIntangibleRun
	campaignNPCMunches                  map[uint32]*campaignNPCMunchRun
	campaignNPCMunchReadiness           map[uint32]uint64
	campaignNPCDrainRuns                map[uint32]*campaignNPCDrainRun
	campaignNPCPullEffects              map[uint32]*campaignNPCPullEffectRun
	campaignNPCOozeGrowths              map[uint32]*campaignNPCOozeGrowthRun
	campaignNPCPhysicalVulnerabilities  map[uint32]*campaignNPCPhysicalVulnerabilityRun
	campaignNPCEnergyVulnerabilities    map[uint32]*campaignNPCEnergyVulnerabilityRun
	campaignNPCFears                    map[uint32]*campaignNPCFearRun
	campaignNPCLobs                     map[uint32]*campaignNPCLobRun
	campaignNPCChargeReadiness          map[uint32]uint64
	campaignNPCResurrectionReadiness    map[uint32]uint64
	campaignNPCFleeDeathCounts          map[uint32]int
	campaignNPCPullReadiness            map[uint32]uint64
	campaignNPCSinkholeReadiness        map[uint32]uint64
	campaignNPCSinkholeEffectSlots      map[uint32]uint8
	campaignNPCNestleWanderReadiness    map[uint32]uint64
	campaignNPCCopterRuns               map[uint32]*campaignScaldronCopterRun
	campaignNPCSuppressionRuns          map[uint32]*campaignSuppressionRun
	campaignNPCDopplerCloneReadiness    map[uint32]uint64
	campaignNPCDopplerFakeObjectIDs     map[uint32]uint32
	campaignNPCMaserCleanseReadiness    map[uint32]uint64
	campaignNPCChargeups                map[uint32]campaignNPCChargeupState
	campaignNPCVoltroidCharges          map[uint32]uint32
	campaignNPCVoltroidChargeExpires    map[uint32]uint64
	campaignNPCVoltroidEffectSlots      map[uint32]uint8
	campaignNPCRepairStacks             map[uint32]uint32
	campaignNPCRepairLockouts           map[uint32]uint64
	campaignNPCRecoveryActionCounts     map[uint32]uint32
	campaignNPCPolarisStates            map[uint32]campaignNPCPolarisState
	campaignNPCCitadelSpecialFourStates map[uint32]campaignCitadelSpecialFourState
	campaignNPCOrcusStates              map[uint32]campaignOrcusState
	campaignTwinLaserEndpointIDs        map[uint32]uint32
	campaignArcturusStates              map[uint32]*campaignArcturusState
	pickupPursuit                       *campaignPickupTimeout
	campaignCorruptorStates             map[uint32]campaignCorruptorState
	campaignNPCRuptionNextMagmas        map[uint32]uint64
	campaignNPCDragSlowShieldReadiness  map[uint32]uint64
	campaignNPCShielderNextGrenades     map[uint32]uint64
	campaignNPCNextSleepMushrooms       map[uint32]uint64
	campaignNPCShielderShieldSetups     map[uint32]bool
	campaignNPCShielderShields          map[uint32]*campaignNomadShielderShieldRun
	campaignNPCGravityOrbs              map[uint32]*campaignNPCGravityOrbRun
	campaignNPCDeathMines               map[uint32]*campaignScaldronDeathMineRun
	campaignPlasmaModifiers             map[uint32]*campaignPlasmaModifierRun
	campaignNPCProjectiles              map[uint32]*abilityraknet.ProjectileRun
}

// zonePresentationRuntime owns connection-local scheduling and transfers.
// Authoritative world state remains on Zone.
type zonePresentationRuntime struct {
	zoneCombatPresentation
	campaignSchedule            *zoneaction.ScheduleSession
	campaignUnlockPresentation  *unlockraknet.ActiveSession
	securityTransfer            *securityraknet.Transfer
	teleporterHandoff           *teleportraknet.Handoff
	teleporterContact           sim.SphereContactState
	isTutorialTeleporterActive  bool
	isTutorialTeleporterUsed    bool
	campaignTeleporterStates    map[uint32]bool
	campaignTunnelExitSource    game.Vec3
	campaignTunnelExitRadius    float32
	isCampaignTunnelExitPending bool
	isClientBossBoundaryPending bool
}

// chainPeerRuntime survives the boundary between one completed zone and the
// next zone selected by the same connected player.
type chainPeerRuntime struct {
	chainResult           *zoneresult.Session
	chainPlanetsCompleted uint8
	chainMedalCounts      [4]zoneresult.MedalCount
}

type zoneMembership struct {
	generation uint64
	zone       *zone.Zone
}

type controlledHeroState struct {
	squad                         *squad.Session
	abilityCooldown               *zoneability.CooldownSession
	abilityRelease                *zoneaction.ReleaseSession
	attackPose                    retainedAttackPose
	campaignPlayerPursuit         *zoneaction.Pursuit
	basicSequence                 *zoneability.Sequence
	playerPosition                raknet.Vector3
	playerMovementGoal            raknet.Vector3
	playerMotion                  *zoneaction.Motion
	followTargetUserID            uint64
	followUpdatedAt               time.Time
	playerAI                      playerAIState
	assistTargetObjectID          uint32
	assistTargetAt                time.Time
	isPartyDefeatQueued           bool
	deployedCreatureIndex         uint32
	deployedObjectID              uint32
	heroInputLockedObjectID       uint32
	heroInputLockedUntil          time.Time
	maximumHitPoints              [squad.Size]float32
	maximumManaPoints             [squad.Size]float32
	isHeroSelectionPending        bool
	isHeroSelectionScheduled      bool
	heroSelectionReadyAt          time.Time
	physicsByNoun                 map[string]zoneNounPhysics
	wraithActiveGeneration        uint64
	areaBasicGeneration           uint64
	movementContactGeneration     uint64
	nextProjectileObjectID        uint32
	enemySilenceExpiresAt         time.Time
	enemySleepExpiresAt           time.Time
	enemyStunExpiresAt            time.Time
	enemyStunTargetObjectID       uint32
	operativeCage                 *campaignNPCModifierRun
	enemyRootExpiresAt            time.Time
	enemyRootTargetObjectID       uint32
	enemyFearExpiresAt            time.Time
	enemyFearTargetObjectID       uint32
	cryosLavaReadyAt              time.Time
	passiveKillStack              [squad.Size]uint32
	passiveReductionStack         [squad.Size]uint32
	passiveReductionExpiresAt     [squad.Size]time.Time
	passiveStationarySince        [squad.Size]time.Time
	fireRavagerBasicCount         [squad.Size]uint32
	tcShieldAmount                [squad.Size]float32
	tcShieldReadyAt               [squad.Size]time.Time
	tcShieldEffectSlot            [squad.Size]uint8
	isTCShieldEffectAttached      [squad.Size]bool
	lightspeedPassiveEpoch        uint64
	lightspeedEffectObjectID      uint32
	lightspeedEffectSlot          uint8
	lightspeedEffectTier          uint8
	isOverdrivePersistencePending bool
	isOverdriveSpent              bool
	overdriveExpiresAt            time.Time
}

func (e gameplayPeerSession) isOverdriveActiveAt(now time.Time) bool {
	return e.binding.IsOverdriveUnlocked && !now.IsZero() &&
		now.Before(e.overdriveExpiresAt)
}

type retainedAttackPose struct {
	facing         raknet.Vector3
	targetPosition raknet.Vector3
	targetObjectID uint32
	expiresAt      time.Time
	isActive       bool
}

func (e *gameplayPeerSession) retainAttackPose(
	facing raknet.Vector3, targetPosition raknet.Vector3,
	targetObjectID uint32, expiresAt time.Time,
) {
	if e == nil || expiresAt.IsZero() {
		return
	}
	e.attackPose = retainedAttackPose{
		facing: facing, targetPosition: targetPosition,
		targetObjectID: targetObjectID,
		expiresAt:      expiresAt, isActive: true,
	}
}

func (e retainedAttackPose) isActiveAt(now time.Time) bool {
	return e.isActive && !now.IsZero() && now.Before(e.expiresAt)
}

func (s *gameplayPeerSession) extendEnemySleep(
	targetObjectID uint32, expiresAt time.Time,
) bool {
	if s == nil || targetObjectID == 0 || expiresAt.IsZero() ||
		s.deployedObjectID != targetObjectID {
		return false
	}
	if s.enemySleepExpiresAt.Before(expiresAt) {
		s.enemySleepExpiresAt = expiresAt
	}
	return true
}

func (s *gameplayPeerSession) isEnemySleepActive(at time.Time) bool {
	return s != nil && !at.IsZero() && at.Before(s.enemySleepExpiresAt)
}

func (s *gameplayPeerSession) extendEnemyStun(
	targetObjectID uint32, expiresAt time.Time,
) bool {
	if s == nil || targetObjectID == 0 || expiresAt.IsZero() ||
		s.deployedObjectID != targetObjectID {
		return false
	}
	if s.enemyStunExpiresAt.Before(expiresAt) {
		s.enemyStunExpiresAt = expiresAt
	}
	s.enemyStunTargetObjectID = targetObjectID
	return true
}

func (s *gameplayPeerSession) isEnemyStunActive(at time.Time) bool {
	if s != nil && s.isOperativeCaged(at) {
		return true
	}
	return s != nil && !at.IsZero() &&
		s.deployedObjectID == s.enemyStunTargetObjectID &&
		at.Before(s.enemyStunExpiresAt)
}

func (s *gameplayPeerSession) extendEnemyRoot(
	targetObjectID uint32, expiresAt time.Time,
) bool {
	if s == nil || targetObjectID == 0 || expiresAt.IsZero() ||
		s.deployedObjectID != targetObjectID {
		return false
	}
	if s.enemyRootExpiresAt.Before(expiresAt) {
		s.enemyRootExpiresAt = expiresAt
	}
	s.enemyRootTargetObjectID = targetObjectID
	return true
}

func (s *gameplayPeerSession) isEnemyRootActive(at time.Time) bool {
	return s != nil && !at.IsZero() &&
		s.deployedObjectID == s.enemyRootTargetObjectID &&
		at.Before(s.enemyRootExpiresAt)
}

func (s *gameplayPeerSession) extendEnemyFear(
	targetObjectID uint32, expiresAt time.Time,
) bool {
	if s == nil || targetObjectID == 0 || expiresAt.IsZero() ||
		s.deployedObjectID != targetObjectID {
		return false
	}
	if s.enemyFearExpiresAt.Before(expiresAt) {
		s.enemyFearExpiresAt = expiresAt
	}
	s.enemyFearTargetObjectID = targetObjectID
	return true
}

func (s *gameplayPeerSession) isEnemyFearActive(at time.Time) bool {
	return s != nil && !at.IsZero() &&
		s.deployedObjectID == s.enemyFearTargetObjectID &&
		at.Before(s.enemyFearExpiresAt)
}

func (s *gameplayPeerSession) extendEnemySilence(
	targetObjectID uint32, expiresAt time.Time,
) bool {
	if s == nil || targetObjectID == 0 || expiresAt.IsZero() ||
		s.deployedObjectID != targetObjectID {
		return false
	}
	if s.enemySilenceExpiresAt.Before(expiresAt) {
		s.enemySilenceExpiresAt = expiresAt
	}
	return true
}

type controlledHeroPresentation struct {
	passiveModifierInstance      [3]uint32
	arePassiveModifiersAllocated bool
	isDancing                    bool
	plasmaWreathRun              *abilityraknet.PlasmaWreathRun
	enrageRun                    *abilityraknet.EnrageRun
	ghostFormRun                 *abilityraknet.GhostFormRun
	heroModifierRun              *heroModifierRun
	missileFlakRun               *missileFlakRun
	poisonNovaCooldownRun        *poisonNovaCooldownRun
	fieldMedicHeroBuffs          map[uint32]*fieldMedicHeroBuffRun
	fieldMedicCompanionBuffs     map[uint32]*fieldMedicCompanionBuffRun
	enemyDeaths                  map[uint32]*deathraknet.Run
	basicAttack                  *abilityraknet.MeleeRun
	basicAttackSyncStamp         uint8
	sageAttacks                  map[uint32]*abilityraknet.ProjectileRun
	heroBurstAttacks             map[uint32]*abilityraknet.BurstRun
	heroTraps                    map[uint32]*heroTrapRun
	heroDrain                    *heroDrainRun
	heroTimedArea                *heroTimedAreaRun
	heroStatusAreas              map[uint32]*heroStatusAreaRun
	heroChannelArea              *heroChannelAreaRun
	heroQuantumBlink             *heroQuantumBlinkRun
	fireTempestActive            *fireTempestActiveRun
	plasmaSentinelActive         *plasmaSentinelActiveRun
	heroInfection                *heroInfectionRun
	heroHealingTicks             *heroHealingTicksRun
	heroAuraAreas                map[uint32]*heroAuraAreaRun
	heroProjectileRuns           map[uint32]*heroProjectileStatusRun
	heroCharge                   *heroChargeRun
	roarReductionObjectID        uint32
	roarReductionExpiresAt       time.Time
	fireTempestPassive           *fireTempestPassiveRun
	energySentinelPassive        *energySentinelPassiveRun
	tossAttacks                  map[uint32]*campaignTossRun
	cloudLobAttacks              map[uint32]*campaignCloudLobRun
	cloudPoison                  zoneability.CloudPoisonRuntime
	sphereAttack                 *abilityraknet.TargetedAOERun
	sphereObjectID               uint32
	sagePassive                  *summonPassiveRun
	sagePassiveActivations       map[uint32]summonCompanionActivation
	fieldMedicDroneObjectID      uint32
	fieldMedicDroneAttack        *abilityraknet.ProjectileRun
	beastPetObjectID             uint32
	beastPetDamage               float32
	beastPetEnrage               *beastPetEnrageRun
	beastPetDamageIncrease       float32
	heroSummons                  map[uint32]*heroSummonRun
	trapperStealthEpoch          uint64
	isTrapperStealthed           bool
	treeOfLifeObjectID           uint32
	treeOfLifeRun                *abilityraknet.AreaHealingRun
	rideCancel                   raknet.CancelSchedule
	obeliskCancel                raknet.CancelSchedule
	obeliskRun                   *interactraknet.LootObeliskRun
	movementContactCancel        raknet.CancelSchedule
}

type zoneCombatPresentation struct {
	spawnModifiers map[uint32]*spawnraknet.Run
}

type gameplayPeerSession struct {
	zoneEffectPresentation
	zonePresentationRuntime
	chainPeerRuntime
	controlledHeroState
	controlledHeroPresentation
	zoneMembership
	binding                              game.GameplayBinding
	stage                                zonemember.Stage
	lastPlayerStatus                     raknet.PlayerStatus
	isPlayerStatusKnown                  bool
	dungeonSetup                         zonemember.Setup
	transportGeneration                  uint64
	schedulePackets                      func(time.Duration, [][]byte) error
	schedulePacket                       raknet.Packet
	pendingPacketBatches                 []pendingPeerPacketBatch
	nextPendingPacketID                  uint64
	isPendingPacketOverflow              bool
	pendingStatDeltas                    []sporenet.PlayerStatDelta
	knownPlayerMask                      uint32
	heroCombatPresentation               *heroCombatPresentation
	isPartyMerged                        bool
	isArenaLobbyTransitionSent           bool
	isArenaLobbyEntered                  bool
	isArenaSquadAccepted                 bool
	isArenaPreparationSent               bool
	isArenaPreparationAcknowledged       bool
	isArenaCombatActive                  bool
	isArenaResultReady                   bool
	arenaWinningTeam                     uint32
	arenaLobbyTransitionAt               time.Time
	arenaLobbyTransitionCount            uint32
	isRejoinPending                      bool
	isNPCRecoveryPending                 bool
	isTutorialCapsuleDropUnlocked        bool
	fullOrbContactObjectIDs              map[uint32]struct{}
	tutorialCapsulePlans                 []tutorialCapsulePlan
	tutorialHorde                        *tutorialHordeSession
	crystalInventory                     sim.CrystalInventory
	campaignNashiraSplitObjectIDs        map[uint32][]uint32
	campaignNashiraSplitPendingObjectIDs map[uint32]uint32
	campaignNashiraPanicReadiness        map[uint32]uint64
	campaignNashiraFiendReadiness        map[uint32]uint64
	campaignMerakPassiveStates           map[uint32]campaignMerakPassiveState
}

func (s *gameplayPeerSession) publishPackets(packets [][]byte) error {
	if s == nil || len(packets) == 0 {
		return nil
	}
	if s.schedulePackets != nil {
		err := s.schedulePackets(0, clonePendingPackets(packets))
		if err == nil {
			return nil
		}
		s.queuePackets(packets)
		return fmt.Errorf("peerSchedule: %w", err)
	}
	s.queuePackets(packets)
	return nil
}

func (s *gameplayPeerSession) setCrystalInventory(inventory sim.CrystalInventory) {
	if s == nil {
		return
	}
	previous := s.crystalInventory.Attributes()
	next := inventory.Attributes()
	for creatureIndex := range s.binding.Creatures {
		creature := s.binding.Creatures[creatureIndex]
		for attributeIndex := range creature.PartAttribute {
			creature.PartAttribute[attributeIndex] +=
				next[attributeIndex] - previous[attributeIndex]
		}
		creature.CriticalRating += next[10] - previous[10] + (next[1]-previous[1])*4
		physicalDefenseDelta := next[7] - previous[7] + (next[1]-previous[1])*6
		if creature.PassiveAbility == util.HashID("QuantumPositioning") {
			physicalDefenseDelta *= 2
		}
		creature.PhysicalDefense += physicalDefenseDelta
		creature.EnergyDefense += next[9] - previous[9] + (next[2]-previous[2])*6
		creature.PetDamage += next[63] - previous[63]
		creature.PetHealthIncrease += next[64] - previous[64]
		creature.RangeIncrease += next[67] - previous[67]
		creature.AreaDurationIncrease += next[95] - previous[95]
		creature.OverdriveDurationIncrease += next[70] - previous[70]
		creature.LifeSteal += next[35] - previous[35]
		creature.CriticalDamageIncrease += next[22] - previous[22]
		creature.PassiveMovementIncrease += next[48] - previous[48]
		// Apply the same inventory delta to the combat snapshots as to the
		// raw attributes. Rebuilding the profiles would erase active buffs.
		primaryDelta := float32(0)
		switch creature.ClassType {
		case "ravager":
			primaryDelta = next[0] - previous[0]
		case "sentinel":
			primaryDelta = next[1] - previous[1]
		case "tempest":
			primaryDelta = next[2] - previous[2]
		}
		if creature.DamageProfile.IsPrimaryAttributeFound {
			creature.DamageProfile.PrimaryAttribute += primaryDelta
		}
		if creature.HealingProfile.IsPrimaryAttributeFound {
			creature.HealingProfile.PrimaryAttribute += primaryDelta
		}
		creature.DamageProfile.PhysicalDefenseBoost += next[7] - previous[7]
		creature.DamageProfile.EnergyDefenseBoost += next[9] - previous[9]
		for index := range creature.DamageProfile.ScienceDamage {
			attributeIndex := 38 + index
			creature.DamageProfile.ScienceDamage[index] += next[attributeIndex] - previous[attributeIndex]
		}
		creature.DamageProfile.AreaDamage += next[37] - previous[37]
		creature.DamageProfile.DirectAttackDamagePercent += next[109] - previous[109]
		creature.TimingProfile.AttackSpeed += next[23] - previous[23]
		creature.TimingProfile.CooldownReduction += next[24] - previous[24]
		creature.TimingProfile.ProjectileSpeedIncrease += next[26] - previous[26]
		s.binding.Creatures[creatureIndex] = creature
	}
	s.crystalInventory = inventory
}

type pendingPeerPacketBatch struct {
	id                     uint64
	packets                [][]byte
	isCampaignPresentation bool
}

func (s *gameplayPeerSession) queuePackets(packets [][]byte) {
	if s == nil || len(packets) == 0 {
		return
	}
	s.nextPendingPacketID++
	batch := pendingPeerPacketBatch{
		id: s.nextPendingPacketID,
	}
	if len(packets) >= pendingPeerPacketLimit {
		batch.packets = retainPendingPackets(packets, pendingPeerPacketLimit)
		s.pendingPacketBatches = []pendingPeerPacketBatch{batch}
		s.isPendingPacketOverflow = true
		return
	}
	batch.packets = clonePendingPackets(packets)
	s.pendingPacketBatches = append(s.pendingPacketBatches, batch)
	packetCount := 0
	for _, pendingBatch := range s.pendingPacketBatches {
		packetCount += len(pendingBatch.packets)
	}
	for packetCount > pendingPeerPacketLimit && len(s.pendingPacketBatches) > 1 {
		removedIndex := oldestDroppablePendingBatch(s.pendingPacketBatches)
		packetCount -= len(s.pendingPacketBatches[removedIndex].packets)
		s.pendingPacketBatches = slices.Delete(
			s.pendingPacketBatches, removedIndex, removedIndex+1,
		)
		s.isPendingPacketOverflow = true
	}
}

func retainPendingPackets(packets [][]byte, limit int) [][]byte {
	if limit <= 0 || len(packets) == 0 {
		return nil
	}
	criticalCount := 0
	for _, packet := range packets {
		if isCriticalPendingPacket(packet) {
			criticalCount++
		}
	}
	if criticalCount >= limit {
		criticalPackets := make([][]byte, 0, criticalCount)
		for _, packet := range packets {
			if isCriticalPendingPacket(packet) {
				criticalPackets = append(criticalPackets, packet)
			}
		}
		return clonePendingPackets(criticalPackets[len(criticalPackets)-limit:])
	}
	noncriticalLimit := limit - criticalCount
	noncriticalStart := 0
	noncriticalCount := len(packets) - criticalCount
	if noncriticalCount > noncriticalLimit {
		noncriticalStart = noncriticalCount - noncriticalLimit
	}
	retainedPackets := make([][]byte, 0, limit)
	noncriticalIndex := 0
	for _, packet := range packets {
		if isCriticalPendingPacket(packet) || noncriticalIndex >= noncriticalStart {
			retainedPackets = append(retainedPackets, packet)
		}
		if !isCriticalPendingPacket(packet) {
			noncriticalIndex++
		}
	}
	return clonePendingPackets(retainedPackets)
}

func oldestDroppablePendingBatch(batches []pendingPeerPacketBatch) int {
	for index, batch := range batches {
		isCritical := false
		for _, packet := range batch.packets {
			if isCriticalPendingPacket(packet) {
				isCritical = true
				break
			}
		}
		if !isCritical {
			return index
		}
	}
	return 0
}

func isCriticalPendingPacket(packet []byte) bool {
	if len(packet) == 0 {
		return false
	}
	switch raknet.PacketID(packet[0]) {
	case raknet.ObjectCreate, raknet.ObjectUpdate, raknet.ObjectDelete,
		raknet.ObjectTeleport, raknet.LootDataUpdate,
		raknet.InteractableUpdate,
		raknet.AttributeDataUpdate,
		raknet.CombatantDataUpdate, raknet.PlayerCharacterDeploy,
		raknet.LabsPlayerUpdate, raknet.PlayerDeparted, raknet.ArenaGameMsgs,
		raknet.ArenaResultsMsgs, raknet.ChainGameMsgs:
		return true
	default:
		return false
	}
}

func clonePendingPackets(packets [][]byte) [][]byte {
	clonedPackets := make([][]byte, len(packets))
	for index, packet := range packets {
		clonedPackets[index] = append([]byte(nil), packet...)
	}
	return clonedPackets
}

func (s *gameplayPeerSession) queueStatDelta(delta sporenet.PlayerStatDelta) {
	if s == nil || delta == (sporenet.PlayerStatDelta{}) {
		return
	}
	if len(s.pendingStatDeltas) >= pendingPeerPacketLimit {
		s.pendingStatDeltas = append(
			[]sporenet.PlayerStatDelta(nil), s.pendingStatDeltas[1:]...,
		)
	}
	s.pendingStatDeltas = append(s.pendingStatDeltas, delta)
}

func (s *gameplayPeerSession) pendingPackets() ([][]byte, uint64) {
	if s == nil || len(s.pendingPacketBatches) == 0 {
		return nil, 0
	}
	packetCount := 0
	for _, batch := range s.pendingPacketBatches {
		packetCount += len(batch.packets)
	}
	packets := make([][]byte, 0, packetCount)
	var batchID uint64
	for _, batch := range s.pendingPacketBatches {
		// A connected peer may still be loading its scene. Keep world packets
		// queued until its dungeon setup, and retain their original ordering.
		if batch.isCampaignPresentation && !s.dungeonSetup.IsCommitted() {
			break
		}
		packets = append(packets, batch.packets...)
		batchID = batch.id
	}
	return packets, batchID
}

func (s *gameplayPeerSession) commitPendingPackets(batchID uint64) {
	if s == nil || batchID == 0 {
		return
	}
	firstRetained := 0
	for firstRetained < len(s.pendingPacketBatches) &&
		s.pendingPacketBatches[firstRetained].id <= batchID {
		firstRetained++
	}
	s.pendingPacketBatches = s.pendingPacketBatches[firstRetained:]
}

func (s *gameplayPeerSession) takeStatDeltas() []sporenet.PlayerStatDelta {
	if s == nil || len(s.pendingStatDeltas) == 0 {
		return nil
	}
	deltas := s.pendingStatDeltas
	s.pendingStatDeltas = nil
	return deltas
}

func (e gameplayPeerSession) abilityRandom() (*sim.SimulatorRandom, error) {
	if e.zone != nil {
		random := e.zone.NPCRandom()
		if random != nil {
			return random, nil
		}
	}
	return nil, errors.New("combat random unavailable")
}

func (e gameplayPeerSession) heroInputLockRemaining(at time.Time) time.Duration {
	if e.heroInputLockedObjectID == 0 || at.IsZero() ||
		!at.Before(e.heroInputLockedUntil) {
		return 0
	}
	return e.heroInputLockedUntil.Sub(at)
}

type campaignEnrageState struct {
	session *gameplayPeerSession
}

func (e campaignEnrageState) EnrageTarget(
	creatureIndex uint32, targetObjectID uint32, isCompanion bool,
) (abilityraknet.EnrageTarget, bool) {
	if e.session == nil || e.session.squad == nil ||
		creatureIndex >= uint32(len(e.session.binding.Creatures)) {
		return abilityraknet.EnrageTarget{}, false
	}
	if isCompanion {
		if e.session.zone == nil || e.session.zone.Companion() == nil {
			return abilityraknet.EnrageTarget{}, false
		}
		actor, isFound := e.session.zone.Companion().Snapshot(targetObjectID)
		if !isFound || actor.UserID != e.session.binding.UserID ||
			actor.PeerGeneration != e.session.generation || actor.HitPoint <= 0 {
			return abilityraknet.EnrageTarget{}, false
		}
		creature := e.session.binding.Creatures[creatureIndex]
		creature.HitPoint = actor.MaximumHitPoint
		creature.DamageProfile.PrimaryAttribute = creature.PetDamage
		creature.DamageProfile.IsPrimaryAttributeFound = true
		return abilityraknet.EnrageTarget{
			Creature: creature,
			Character: squad.Character{
				HitPoints: actor.HitPoint, IsAvailable: true,
			},
			ObjectID: actor.ObjectID,
		}, true
	}
	character, isFound := e.session.squad.Character(creatureIndex)
	objectID := zonehero.ObjectID(e.session.binding.Slot, creatureIndex)
	if !isFound || objectID == 0 {
		return abilityraknet.EnrageTarget{}, false
	}
	return abilityraknet.EnrageTarget{
		Creature:  e.session.binding.Creatures[creatureIndex],
		Character: character,
		ObjectID:  objectID,
	}, true
}

func (e campaignEnrageState) ApplyEnrage(
	creatureIndex uint32, targetObjectID uint32, isCompanion bool,
	damageBonus float32, hitPoint float32,
) error {
	if e.session == nil ||
		creatureIndex >= uint32(len(e.session.binding.Creatures)) {
		return errors.New("campaign Enrage target unavailable")
	}
	if isCompanion {
		if e.session.zone == nil || e.session.zone.Companion() == nil {
			return errors.New("campaign Enrage companion unavailable")
		}
		err := e.session.zone.Companion().AddBuff(
			targetObjectID, zonecompanion.Buff{DamageBuff: damageBonus},
		)
		if err != nil {
			return fmt.Errorf("enrageCompanionBuff: %w", err)
		}
		_, _, err = e.session.zone.Companion().SetHitPoint(targetObjectID, hitPoint)
		if err != nil {
			e.session.zone.Companion().RemoveBuff(
				targetObjectID, zonecompanion.Buff{DamageBuff: damageBonus},
			)
			return fmt.Errorf("enrageCompanionHitPoint: %w", err)
		}
		return nil
	}
	profile := &e.session.binding.Creatures[creatureIndex].DamageProfile
	previousDamage := profile.DirectAttackDamage
	profile.DirectAttackDamage += damageBonus
	_, err := e.session.setCampaignCharacterHitPoints(creatureIndex, hitPoint)
	if err != nil {
		profile.DirectAttackDamage = previousDamage
		return fmt.Errorf("enrageHitPoint: %w", err)
	}
	return nil
}

func (e campaignEnrageState) RemoveEnrage(
	creatureIndex uint32, targetObjectID uint32, isCompanion bool,
	damageBonus float32,
) error {
	if e.session == nil ||
		creatureIndex >= uint32(len(e.session.binding.Creatures)) {
		return errors.New("campaign Enrage target unavailable")
	}
	if isCompanion {
		if e.session.zone != nil && e.session.zone.Companion() != nil {
			e.session.zone.Companion().RemoveBuff(
				targetObjectID, zonecompanion.Buff{DamageBuff: damageBonus},
			)
		}
		return nil
	}
	profile := &e.session.binding.Creatures[creatureIndex].DamageProfile
	profile.DirectAttackDamage = max(float32(0), profile.DirectAttackDamage-damageBonus)
	return nil
}

func (s *gameplayPeerSession) isCampaignNPCSourceActive(
	generation uint64, sourceObjectID uint32,
) bool {
	if s == nil || s.generation != generation || sourceObjectID == 0 || s.squad == nil ||
		s.isZoneTerminal() || s.zone.NPCs() == nil {
		return false
	}
	if s.zone.Boss() != nil && s.zone.Boss().IsBeamOutCommitted() {
		return false
	}
	enemy, isFound := s.zone.NPCs().NPC(sourceObjectID)
	owner := zonenpc.ActionOwner{
		UserID: s.binding.UserID, PeerGeneration: generation,
	}
	return isFound && !enemy.IsDefeated && enemy.HitPoint > 0 &&
		enemy.IsActionStarted && enemy.ActionOwner == owner
}

func (s *gameplayPeerSession) isCampaignNPCSourceGenerationActive(
	generation uint64, sourceObjectID uint32, actionGeneration uint64,
) bool {
	if actionGeneration == 0 ||
		!s.isCampaignNPCSourceActive(generation, sourceObjectID) {
		return false
	}
	enemy, isFound := s.zone.NPCs().NPC(sourceObjectID)
	return isFound && enemy.ActionGeneration == actionGeneration
}

func (s *gameplayPeerSession) isCampaignNPCAttackActive(
	generation uint64, sourceObjectID uint32, targetObjectID uint32,
) bool {
	if targetObjectID == 0 ||
		!s.isCampaignNPCSourceActive(generation, sourceObjectID) {
		return false
	}
	target, isFound := s.campaignNPCTarget(generation, targetObjectID)
	return isFound && target.ObjectID == targetObjectID && target.HitPoint > 0
}

func (s *gameplayPeerSession) isCampaignNPCAttackActiveAt(
	generation uint64, sourceObjectID uint32, targetObjectID uint32, at time.Time,
) bool {
	return s.isCampaignNPCAttackActive(generation, sourceObjectID, targetObjectID) &&
		s.isCampaignNPCActionActiveAt(generation, sourceObjectID, at)
}

func (s *gameplayPeerSession) isCampaignNPCAttackGenerationActiveAt(
	generation uint64, sourceObjectID uint32, targetObjectID uint32,
	actionGeneration uint64, at time.Time,
) bool {
	return s.isCampaignNPCAttackActiveAt(
		generation, sourceObjectID, targetObjectID, at,
	) && s.isCampaignNPCSourceGenerationActive(
		generation, sourceObjectID, actionGeneration,
	)
}

func (s *gameplayPeerSession) isCampaignNPCActionActiveAt(
	generation uint64, sourceObjectID uint32, at time.Time,
) bool {
	return s.isCampaignNPCSourceActive(generation, sourceObjectID) &&
		s.zone.NPCs().StunRemaining(sourceObjectID, at) == 0 &&
		s.zone.NPCs().SleepRemaining(sourceObjectID, at) == 0 &&
		s.zone.NPCs().FearRemaining(sourceObjectID, at) == 0
}

func (s *gameplayPeerSession) campaignNPCTarget(
	generation uint64, targetObjectID uint32,
) (zone.NPCTarget, bool) {
	if s == nil || generation == 0 || s.generation != generation ||
		s.binding.UserID == 0 || s.zone == nil || targetObjectID == 0 {
		return zone.NPCTarget{}, false
	}
	return s.zone.NPCTarget(targetObjectID)
}

func (s *gameplayPeerSession) isHeroDebuffImmune(targetObjectID uint32) bool {
	return s != nil && targetObjectID == s.deployedObjectID &&
		s.heroModifierRun.IsDebuffImmune()
}

func (s *gameplayPeerSession) trackCampaignNPCProjectile(
	objectID uint32, run *abilityraknet.ProjectileRun,
) error {
	if s == nil || objectID == 0 || run == nil {
		return errors.New("enemy projectile track invalid")
	}
	if s.campaignNPCProjectiles == nil {
		s.campaignNPCProjectiles = make(map[uint32]*abilityraknet.ProjectileRun)
	}
	if _, isFound := s.campaignNPCProjectiles[objectID]; isFound {
		return errors.New("enemy projectile already tracked")
	}
	s.campaignNPCProjectiles[objectID] = run
	return nil
}

func (s *gameplayPeerSession) untrackCampaignNPCProjectile(
	objectID uint32, run *abilityraknet.ProjectileRun,
) {
	if s == nil || objectID == 0 || run == nil || s.campaignNPCProjectiles[objectID] != run {
		return
	}
	delete(s.campaignNPCProjectiles, objectID)
}

func (s *gameplayPeerSession) stopCampaignNPCProjectiles() {
	if s == nil {
		return
	}
	for objectID, run := range s.campaignNPCProjectiles {
		run.Stop()
		delete(s.campaignNPCProjectiles, objectID)
	}
	for objectID, run := range s.campaignNPCDrainRuns {
		run.End()
		run.ReleaseEffects()
		delete(s.campaignNPCDrainRuns, objectID)
	}
	for objectID, run := range s.campaignNPCPullEffects {
		_, _, isReleased := run.release()
		if !isReleased {
			// The scheduled cleanup already released this presentation lease.
		}
		delete(s.campaignNPCPullEffects, objectID)
	}
	s.campaignNPCPolarisStates = make(map[uint32]campaignNPCPolarisState)
	for objectID, run := range s.campaignNPCGravityOrbs {
		if run.cancel != nil {
			run.cancel()
		}
		delete(s.campaignNPCGravityOrbs, objectID)
	}
	for objectID, run := range s.campaignNPCLobs {
		if run.cancel != nil {
			run.cancel()
		}
		delete(s.campaignNPCLobs, objectID)
	}
	clear(s.campaignNPCChargeReadiness)
	clear(s.campaignNPCMunchReadiness)
	clear(s.campaignNPCResurrectionReadiness)
	clear(s.campaignNPCEnergyBuffReadiness)
	clear(s.campaignNPCDiseaseImmunityEnds)
	clear(s.campaignNPCFleeDeathCounts)
	clear(s.campaignNPCDragSlowShieldReadiness)
	clear(s.campaignNPCPullReadiness)
	clear(s.campaignNPCSinkholeReadiness)
	clear(s.campaignNPCSinkholeEffectSlots)
	clear(s.campaignNPCNestleWanderReadiness)
	for objectID, run := range s.campaignNPCCopterRuns {
		run.releaseLinkSlots()
		delete(s.campaignNPCCopterRuns, objectID)
	}
	clear(s.campaignNPCSuppressionRuns)
	clear(s.campaignNPCDopplerCloneReadiness)
	clear(s.campaignNPCDopplerFakeObjectIDs)
	clear(s.campaignNPCMaserCleanseReadiness)
	clear(s.campaignNPCDeathMines)
	clear(s.campaignNPCChargeups)
	clear(s.campaignNPCVoltroidCharges)
	clear(s.campaignNPCVoltroidChargeExpires)
	clear(s.campaignNPCVoltroidEffectSlots)
	clear(s.campaignNPCRepairStacks)
	clear(s.campaignNPCRepairLockouts)
	clear(s.campaignNPCCitadelSpecialFourStates)
	clear(s.campaignNPCOrcusStates)
	clear(s.campaignTwinLaserEndpointIDs)
	clear(s.campaignArcturusStates)
	s.pickupPursuit = nil
	clear(s.campaignNPCRuptionNextMagmas)
	clear(s.campaignNPCShielderNextGrenades)
	clear(s.campaignNPCNextSleepMushrooms)
	clear(s.campaignNPCShielderShieldSetups)
	clear(s.campaignNashiraSplitObjectIDs)
	clear(s.campaignNashiraSplitPendingObjectIDs)
	clear(s.campaignNashiraPanicReadiness)
	clear(s.campaignNashiraFiendReadiness)
	clear(s.campaignMerakPassiveStates)
	for objectID, run := range s.campaignNPCShielderShields {
		run.stop()
		delete(s.campaignNPCShielderShields, objectID)
	}
}

func (s *gameplayPeerSession) abilityCooldownSession() *zoneability.CooldownSession {
	if s == nil {
		return nil
	}
	if s.abilityCooldown == nil {
		s.abilityCooldown = zoneability.NewCooldownSession()
	}
	return s.abilityCooldown
}

func (s *gameplayPeerSession) abilityReleaseSession() *zoneaction.ReleaseSession {
	if s == nil {
		return nil
	}
	if s.abilityRelease == nil {
		s.abilityRelease = zoneaction.NewReleaseSession()
	}
	return s.abilityRelease
}

func (e *gameplayPeerSession) campaignScheduleSession() *zoneaction.ScheduleSession {
	if e == nil {
		return nil
	}
	if e.campaignSchedule == nil {
		e.campaignSchedule = zoneaction.NewScheduleSession()
	}
	return e.campaignSchedule
}

func (e *gameplayPeerSession) campaignUnlockPresentationSession() *unlockraknet.ActiveSession {
	if e == nil {
		return nil
	}
	if e.campaignUnlockPresentation == nil {
		e.campaignUnlockPresentation = unlockraknet.NewActiveSession()
	}
	return e.campaignUnlockPresentation
}

func (s *gameplayPeerSession) campaignPlayerPursuitSession() *zoneaction.Pursuit {
	if s == nil {
		return nil
	}
	if s.campaignPlayerPursuit == nil {
		s.campaignPlayerPursuit = &zoneaction.Pursuit{}
	}
	return s.campaignPlayerPursuit
}

func (s *gameplayPeerSession) basicSequenceSession() *zoneability.Sequence {
	if s == nil {
		return nil
	}
	if s.basicSequence == nil {
		s.basicSequence = &zoneability.Sequence{}
	}
	return s.basicSequence
}

func (s *gameplayPeerSession) playerMotionSnapshot() zoneaction.MotionSnapshot {
	if s == nil || s.playerMotion == nil {
		return zoneaction.MotionSnapshot{}
	}
	return s.playerMotion.Snapshot()
}

func (e *gameplayPeerSession) advancePlayerMovement(
	now time.Time, reportedPosition raknet.Vector3,
	goal raknet.Vector3, isStop bool, movementIncrease float32,
) (raknet.Vector3, raknet.Vector3, error) {
	return e.advancePlayerMovementMode(
		now, reportedPosition, goal, isStop, movementIncrease, false,
	)
}

func (e *gameplayPeerSession) advancePlayerPursuitMovement(
	now time.Time, reportedPosition raknet.Vector3,
	goal raknet.Vector3, movementIncrease float32,
) (raknet.Vector3, raknet.Vector3, error) {
	return e.advancePlayerMovementMode(
		now, reportedPosition, goal, false, movementIncrease, true,
	)
}

func (e *gameplayPeerSession) advancePlayerMovementMode(
	now time.Time, reportedPosition raknet.Vector3,
	goal raknet.Vector3, isStop bool, movementIncrease float32,
	isPursuit bool,
) (raknet.Vector3, raknet.Vector3, error) {
	if e.playerMotion == nil {
		if !isFiniteZonePosition(reportedPosition) {
			return raknet.Vector3{}, raknet.Vector3{},
				errors.New("movement seed non-finite")
		}
		motion, err := zoneaction.NewMotion(
			toSimPosition(reportedPosition), now,
		)
		if err != nil {
			return raknet.Vector3{}, raknet.Vector3{},
				fmt.Errorf("movementSeed: %w", err)
		}
		e.playerMotion = motion
		e.playerPosition = reportedPosition
	}
	if !isStop && !isFiniteZonePosition(goal) {
		return raknet.Vector3{}, raknet.Vector3{},
			errors.New("movement goal non-finite")
	}
	correctionRange := zonePlayerPoseCorrectionRange
	moveSpeed := zonePlayerMoveSpeed
	if e.deployedCreatureIndex < uint32(len(e.binding.Creatures)) {
		increase := max(
			float32(-0.9), movementIncrease+e.enemyMovementSpeedBuff(),
		)
		moveSpeed *= 1 + increase
	}
	if e.zone != nil {
		e.playerMotion.SetNavigation(e.zone.Navigation(), e.deployedCampaignFootprintRadius())
	}
	previous := sim.Position{}
	position := sim.Position{}
	var err error
	if isPursuit {
		previous, position, err = e.playerMotion.AdvancePursuit(
			now, toSimPosition(reportedPosition), toSimPosition(goal),
			isReportedZonePosition(reportedPosition), false,
			correctionRange, moveSpeed,
		)
	} else {
		previous, position, err = e.playerMotion.Advance(
			now, toSimPosition(reportedPosition), toSimPosition(goal),
			isReportedZonePosition(reportedPosition), isStop,
			correctionRange, moveSpeed,
		)
	}
	if err != nil {
		return raknet.Vector3{}, raknet.Vector3{},
			fmt.Errorf("movementAdvance: %w", err)
	}
	e.playerPosition = toRakNetPosition(position)
	if isStop {
		e.playerMovementGoal = e.playerPosition
	} else {
		e.playerMovementGoal = toRakNetPosition(e.playerMotion.Snapshot().Movement().Goal)
	}
	if e.deployedCreatureIndex < uint32(len(e.passiveStationarySince)) {
		if isStop {
			if e.passiveStationarySince[e.deployedCreatureIndex].IsZero() {
				e.passiveStationarySince[e.deployedCreatureIndex] = now
			}
		} else {
			e.passiveStationarySince[e.deployedCreatureIndex] = time.Time{}
		}
	}
	return toRakNetPosition(previous), e.playerPosition, nil
}

func (e *gameplayPeerSession) advancePlayerPosition(
	now time.Time, reportedPosition raknet.Vector3,
) error {
	if e.playerMotion == nil {
		if isReportedZonePosition(reportedPosition) {
			e.playerPosition = reportedPosition
		}
		return nil
	}
	correctionRange := zonePlayerPoseCorrectionRange
	if e.zone != nil {
		e.playerMotion.SetNavigation(e.zone.Navigation(), e.deployedCampaignFootprintRadius())
	}
	position, err := e.playerMotion.AdvancePosition(
		now, toSimPosition(reportedPosition),
		isReportedZonePosition(reportedPosition), correctionRange,
	)
	if err != nil {
		return fmt.Errorf("positionAdvance: %w", err)
	}
	e.playerPosition = toRakNetPosition(position)
	return nil
}

func (e *gameplayPeerSession) stopPlayerMovement(now time.Time) error {
	if e == nil {
		return errors.New("nil player movement session")
	}
	if e.deployedCreatureIndex < uint32(len(e.passiveStationarySince)) &&
		e.passiveStationarySince[e.deployedCreatureIndex].IsZero() {
		e.passiveStationarySince[e.deployedCreatureIndex] = now
	}
	if e.playerMotion == nil {
		err := e.syncZoneHeroPose()
		if err != nil {
			return fmt.Errorf("stopHeroPose: %w", err)
		}
		return nil
	}
	position, err := e.playerMotion.Stop(now)
	if err != nil {
		return fmt.Errorf("stopMovement: %w", err)
	}
	e.playerPosition = toRakNetPosition(position)
	e.playerMovementGoal = e.playerPosition
	err = e.syncZoneHeroPose()
	if err != nil {
		return fmt.Errorf("stopHeroPose: %w", err)
	}
	return nil
}

func (e *gameplayPeerSession) startEnemyFearMovement(
	now time.Time, destination raknet.Vector3,
) error {
	if e == nil || !isFiniteZonePosition(destination) {
		return errors.New("fear movement invalid")
	}
	if e.playerMotion == nil {
		motion, err := zoneaction.NewMotion(toSimPosition(e.playerPosition), now)
		if err != nil {
			return fmt.Errorf("fearMovementSeed: %w", err)
		}
		e.playerMotion = motion
	}
	_, position, err := e.playerMotion.Advance(
		now, toSimPosition(e.playerPosition), toSimPosition(destination),
		false, false, zonePlayerPoseCorrectionRange, zonePlayerMoveSpeed*0.5,
	)
	if err != nil {
		return fmt.Errorf("fearMovementAdvance: %w", err)
	}
	e.playerPosition = toRakNetPosition(position)
	if e.deployedCreatureIndex < uint32(len(e.passiveStationarySince)) {
		e.passiveStationarySince[e.deployedCreatureIndex] = time.Time{}
	}
	return nil
}

func (e *gameplayPeerSession) teleportPlayer(
	now time.Time, destination raknet.Vector3,
) error {
	if e == nil {
		return errors.New("nil player teleport session")
	}
	if e.zone != nil {
		err := zonenavigation.ValidateTeleport(
			e.zone.Navigation(),
			game.Vec3{
				X: destination.X, Y: destination.Y, Z: destination.Z,
			},
			e.deployedCampaignFootprintRadius(),
		)
		if err != nil {
			return fmt.Errorf("teleportNavigation: %w", err)
		}
	}
	if e.playerMotion == nil {
		e.playerPosition = destination
		if e.deployedCreatureIndex < uint32(len(e.passiveStationarySince)) {
			e.passiveStationarySince[e.deployedCreatureIndex] = now
		}
		return nil
	}
	err := e.playerMotion.Teleport(now, toSimPosition(destination))
	if err != nil {
		return fmt.Errorf("movementTeleport: %w", err)
	}
	e.playerPosition = destination
	if e.deployedCreatureIndex < uint32(len(e.passiveStationarySince)) {
		e.passiveStationarySince[e.deployedCreatureIndex] = now
	}
	return nil
}

func toSimPosition(position raknet.Vector3) sim.Position {
	return sim.Position{X: position.X, Y: position.Y, Z: position.Z}
}

func toRakNetPosition(position sim.Position) raknet.Vector3 {
	return raknet.Vector3{X: position.X, Y: position.Y, Z: position.Z}
}

func isInsideZoneTrigger(
	position raknet.Vector3,
	center raknet.Vector3,
	radius float32,
) bool {
	return zonegeometry.ContainsSphere(
		zonePosition(position),
		zonePosition(center),
		radius,
	)
}

func isFiniteZonePosition(position raknet.Vector3) bool {
	return zonegeometry.IsFinite(zonePosition(position))
}

func isReportedZonePosition(position raknet.Vector3) bool {
	return zonegeometry.IsReported(zonePosition(position))
}

func (s *gameplayPeerSession) playerMotionRevision() uint64 {
	if s == nil || s.playerMotion == nil {
		return 0
	}
	return s.playerMotion.Revision()
}

func (s *gameplayPeerSession) restorePlayerMotion(
	snapshot zoneaction.MotionSnapshot, expectedRevision uint64,
) bool {
	if s == nil || s.playerMotion == nil {
		return false
	}
	position, isRestored := s.playerMotion.Restore(snapshot, expectedRevision)
	if isRestored {
		s.playerPosition = toRakNetPosition(position)
	}
	return isRestored
}

func (s *gameplayPeerSession) isAbilityReleaseReady(now time.Time) bool {
	if s == nil {
		return false
	}
	return s.abilityReleaseSession().IsReady(now)
}

func (s *gameplayPeerSession) resetAbilityRelease() {
	if s == nil {
		return
	}
	s.abilityReleaseSession().Reset()
	s.attackPose = retainedAttackPose{}
}

// clearEnemyHeroStatuses prevents control effects applied to one hero from
// surviving that hero's defeat or being inherited by the next deployed squad
// member. The client presents those modifiers on a specific hero object even
// though their authoritative expiry is owned by the peer session.
func (s *gameplayPeerSession) clearEnemyHeroStatuses() {
	if s == nil {
		return
	}
	s.enemySilenceExpiresAt = time.Time{}
	s.enemySleepExpiresAt = time.Time{}
	s.enemyStunExpiresAt = time.Time{}
	s.enemyStunTargetObjectID = 0
	s.enemyRootExpiresAt = time.Time{}
	s.enemyRootTargetObjectID = 0
	s.enemyFearExpiresAt = time.Time{}
	s.enemyFearTargetObjectID = 0
}

// resetRetainedTransportState removes transient admission and presentation
// state that was canceled with the old connection. Zone-owned world progress,
// squad resources, cooldowns, and the stopped authoritative pose survive.
func (s *gameplayPeerSession) resetRetainedTransportState() {
	if s == nil {
		return
	}
	s.operativeCage = nil
	s.resetAbilityRelease()
	s.playerAI = playerAIState{}
	// The rejoin baseline supersedes packets encoded for the retired transport.
	// Durable stat deltas remain queued for the replacement peer.
	s.pendingPacketBatches = nil
	s.isPendingPacketOverflow = false
	s.enemySilenceExpiresAt = time.Time{}
	s.enemySleepExpiresAt = time.Time{}
	s.enemyStunExpiresAt = time.Time{}
	s.enemyStunTargetObjectID = 0
	s.enemyRootExpiresAt = time.Time{}
	s.enemyRootTargetObjectID = 0
	s.enemyFearExpiresAt = time.Time{}
	s.enemyFearTargetObjectID = 0
	s.isHeroSelectionScheduled = false
	for index := range s.tcShieldAmount {
		s.resetTCShield(uint32(index))
	}
	s.lightspeedPassiveEpoch = 0
	s.lightspeedEffectObjectID = 0
	s.lightspeedEffectSlot = 0
	s.lightspeedEffectTier = 0
}

func (s *gameplayPeerSession) syncZoneHero() error {
	if s == nil || s.zone == nil || s.deployedObjectID == 0 {
		return nil
	}
	character, err := s.normalizeCampaignCharacterResources(
		s.deployedCreatureIndex,
	)
	if err != nil {
		return fmt.Errorf("zoneHeroResources: %w", err)
	}
	footprintRadius := s.deployedCampaignFootprintRadius()
	maximumHitPoint, maximumManaPoint := s.characterResourceMaximum(
		s.deployedCreatureIndex,
	)
	err = s.zone.Hero().Put(zonehero.Actor{
		UserID:         s.binding.UserID,
		PeerGeneration: s.generation,
		ObjectID:       s.deployedObjectID,
		CreatureIndex:  s.deployedCreatureIndex,
		Position: game.Vec3{
			X: s.playerPosition.X,
			Y: s.playerPosition.Y,
			Z: s.playerPosition.Z,
		},
		LinearVelocity:   game.Vec3{},
		FootprintRadius:  footprintRadius,
		HitPoint:         character.HitPoints,
		ManaPoint:        character.ManaPoints,
		MaximumHitPoint:  maximumHitPoint,
		MaximumManaPoint: maximumManaPoint,
	})
	if err != nil {
		return fmt.Errorf("zoneHeroSync: %w", err)
	}
	return nil
}

func (s *gameplayPeerSession) syncZoneSquadCheckpoint() error {
	if s == nil || s.zone == nil || s.squad == nil {
		return nil
	}
	for creatureIndex := uint32(0); creatureIndex < squad.Size; creatureIndex++ {
		character, isFound := s.squad.Character(creatureIndex)
		if !isFound {
			continue
		}
		s.binding.Creatures[creatureIndex].HitPoint = character.HitPoints
		s.binding.Creatures[creatureIndex].PowerPoint = character.ManaPoints
	}
	err := s.zone.UpdateMemberRoster(
		s.binding.UserID, s.generation, s.binding.Roster(),
	)
	if err != nil {
		return fmt.Errorf("zoneRosterSync: %w", err)
	}
	err = s.zone.SetCheckpointSquad(
		s.binding.UserID, s.generation, s.squad.State(),
		zonePosition(s.playerPosition),
	)
	if err != nil {
		return fmt.Errorf("zoneSquadSync: %w", err)
	}
	return nil
}

func (s *gameplayPeerSession) syncZoneHeroPose() error {
	if s == nil || s.zone == nil || s.deployedObjectID == 0 {
		return nil
	}
	velocity := sim.Position{}
	if s.playerMotion != nil {
		velocity = s.playerMotion.Velocity()
	}
	err := s.zone.Hero().UpdatePose(
		s.binding.UserID,
		s.generation,
		s.deployedObjectID,
		game.Vec3{
			X: s.playerPosition.X,
			Y: s.playerPosition.Y,
			Z: s.playerPosition.Z,
		},
		game.Vec3{X: velocity.X, Y: velocity.Y, Z: velocity.Z},
		s.deployedCampaignFootprintRadius(),
	)
	if err != nil {
		return fmt.Errorf("zoneHeroPose: %w", err)
	}
	return nil
}

func (s *gameplayPeerSession) interruptBasicForMovement() *abilityraknet.MeleeRun {
	if s == nil {
		return nil
	}
	run := s.basicAttack
	s.basicAttack = nil
	s.basicAttackSyncStamp = 0
	s.basicSequenceSession().ReleaseHeld()
	s.campaignPlayerPursuitSession().Cancel()
	return run
}

func (s *gameplayPeerSession) resetSharedActionAdmission() *abilityraknet.MeleeRun {
	if s == nil {
		return nil
	}
	run := s.basicAttack
	s.basicAttack = nil
	s.basicAttackSyncStamp = 0
	s.basicSequenceSession().ReleaseHeld()
	s.resetAbilityRelease()
	s.campaignPlayerPursuitSession().Cancel()
	s.isDancing = false
	return run
}

func (s *gameplayPeerSession) resetInterruptibleActionAdmission() *abilityraknet.MeleeRun {
	if s == nil {
		return nil
	}
	run := s.resetSharedActionAdmission()
	if s.heroDrain != nil {
		s.heroDrain.Stop()
		s.heroDrain = nil
	}
	if s.heroChannelArea != nil {
		s.heroChannelArea.Stop()
		s.heroChannelArea = nil
	}
	if s.heroQuantumBlink != nil {
		s.heroQuantumBlink.Stop()
		s.heroQuantumBlink = nil
	}
	if s.heroCharge != nil {
		s.heroCharge.Interrupt()
		s.heroCharge = nil
	}
	return run
}

func (s *gameplayPeerSession) resetAbilityAdmissionForSwitch() *abilityraknet.MeleeRun {
	if s == nil {
		return nil
	}
	run := s.resetInterruptibleActionAdmission()
	for _, attack := range s.sageAttacks {
		attack.Stop()
	}
	s.sageAttacks = nil
	for _, attack := range s.heroBurstAttacks {
		attack.Stop()
	}
	s.heroBurstAttacks = nil
	if s.heroTimedArea != nil {
		s.heroTimedArea.Stop()
		s.queuePackets([][]byte{s.heroTimedArea.removalPacket})
		s.heroTimedArea.ReleaseEffect()
		s.heroTimedArea = nil
	}
	if s.heroInfection != nil {
		s.heroInfection.Stop()
		s.heroInfection = nil
	}
	if s.heroHealingTicks != nil {
		s.heroHealingTicks.Stop()
		s.heroHealingTicks = nil
	}
	s.roarReductionObjectID = 0
	s.roarReductionExpiresAt = time.Time{}
	if s.fireTempestPassive != nil {
		s.fireTempestPassive.Stop()
		s.fireTempestPassive = nil
	}
	if s.energySentinelPassive != nil {
		s.energySentinelPassive.Stop()
		s.energySentinelPassive = nil
	}
	s.basicSequence = &zoneability.Sequence{}
	return run
}

func (e *gameplayPeerSession) resetFailedActionAdmission(
	modifierInstancePool *modifierPool, effectPool *attachedEffectPool,
) *abilityraknet.MeleeRun {
	if e == nil {
		return nil
	}
	run := e.resetAbilityAdmissionForSwitch()
	for _, trap := range e.heroTraps {
		trap.Stop()
	}
	e.heroTraps = nil
	for _, statusRun := range e.heroStatusAreas {
		statusRun.Stop()
	}
	e.heroStatusAreas = nil
	for _, auraRun := range e.heroAuraAreas {
		auraRun.Stop()
	}
	e.heroAuraAreas = nil
	for _, projectileRun := range e.heroProjectileRuns {
		projectileRun.Stop()
	}
	e.heroProjectileRuns = nil
	for _, attack := range e.tossAttacks {
		attack.stop()
	}
	e.tossAttacks = nil
	for _, attack := range e.cloudLobAttacks {
		if attack != nil && attack.cancel != nil {
			attack.cancel()
		}
	}
	e.cloudLobAttacks = nil
	e.cloudPoison = zoneability.CloudPoisonRuntime{}
	if e.sphereAttack != nil {
		e.sphereAttack.Stop()
		e.sphereAttack = nil
	}
	_, _ = e.stopFireTempestActive()
	_, _ = e.stopPlasmaSentinelActive()
	_, _ = e.stopFieldMedicDrone()
	_, _ = e.stopBeastPet()
	_, _ = e.stopHeroSummons()
	_, _ = e.stopCampaignTreeOfLife()
	_, _ = e.stopTrapperStealth()
	if e.enrageRun != nil {
		e.enrageRun.Cancel()
		e.enrageRun.ReleaseEffect()
		if modifierInstancePool != nil {
			_ = modifierInstancePool.Release(e.enrageRun.InstanceID())
		}
		e.enrageRun = nil
	}
	if e.ghostFormRun != nil {
		e.ghostFormRun.Cancel()
		e.ghostFormRun.ReleaseEffect()
		if modifierInstancePool != nil {
			_ = modifierInstancePool.Release(e.ghostFormRun.InstanceID())
		}
		e.ghostFormRun = nil
	}
	if e.heroModifierRun != nil {
		e.heroModifierRun.Cancel()
		if e.heroModifierRun.isShadowStealth {
			acquired, stealthPacket, err := e.setShadowRavagerStealth(
				e.heroModifierRun.objectID, false,
			)
			_ = acquired
			_ = stealthPacket
			if err != nil {
				// Session teardown cannot publish recovery packets; zone teardown
				// removes the same hero authority immediately afterward.
			}
		}
		e.heroModifierRun.Remove(e)
		if modifierInstancePool != nil {
			_ = modifierInstancePool.Release(e.heroModifierRun.instanceID)
		}
		e.heroModifierRun = nil
	}
	_, _ = stopMissileFlak(e, modifierInstancePool)
	_, _ = stopPoisonNovaCooldown(e, modifierInstancePool)
	if e.plasmaWreathRun != nil {
		_, _ = e.plasmaWreathRun.Stop()
		e.plasmaWreathRun = nil
	}
	if effectPool != nil {
		effectPool.ReleaseObject(e.deployedObjectID)
	}
	return run
}

type schedulePacketFunc func(time.Duration, func() ([][]byte, error)) error

type campaignAbilityPursuitAdmission struct {
	pursuit        zoneaction.PursuitSnapshot
	playerPosition game.Vec3
	isSessionFound bool
	isRetry        bool
}

// gameplaySessionRegistry owns the active peer-session collection and the
// validity checks used by delayed packet producers. Sessions remain stored by
// value during the first refactor phase so this extraction does not alter
// runtime behavior.
type gameplaySessionRegistry struct {
	// Keep every atomically accessed 64-bit field first so it remains aligned
	// in the Windows 386 build.
	nextGeneration       uint64
	nextActionGeneration uint64
	memberMutexes        [64]sync.Mutex
	mutex                sync.RWMutex
	isClosed             bool
	activeRequest        int
	requestIdle          chan struct{}
	sessions             map[string]gameplayPeerSession
	retainedSessions     map[gameplayMemberKey]*gameplayRetainedSession
	memberTransports     map[gameplayMemberKey]uint64
	memberEndpoints      map[gameplayMemberKey]string
	actionLeases         map[gameplayActionLeaseKey]gameplayActionLease
	actionAdmissions     map[gameplayActionLeaseKey]struct{}
	actionTerminals      map[gameplayActionLeaseKey]struct{}
	arenaMatches         map[uint32]*arenaMatchState
	producerGuard        gameplayProducerGuard
	lifecycle            gameplaySessionLifecycle
	timer                zone.Timer
	now                  func() time.Time
	logger               *log.Logger
}

type gameplayMemberKey struct {
	gameID uint32
	userID uint64
}

type gameplayRetainedSession struct {
	peerSession gameplayPeerSession
	expiresAt   time.Time
	cancel      func()
	ready       chan struct{}
}

const gameplayRejoinGrace = 10 * time.Minute

const memberLockRetryDelay = 10 * time.Millisecond

const disconnectRetryDelay = 25 * time.Millisecond

const gameplayCleanupWait = 5 * time.Second

func newGameplaySessionRegistry(
	timer zone.Timer, now func() time.Time, logger *log.Logger,
) *gameplaySessionRegistry {
	if now == nil {
		now = time.Now
	}
	registry := &gameplaySessionRegistry{
		sessions:         make(map[string]gameplayPeerSession),
		retainedSessions: make(map[gameplayMemberKey]*gameplayRetainedSession),
		memberTransports: make(map[gameplayMemberKey]uint64),
		memberEndpoints:  make(map[gameplayMemberKey]string),
		actionLeases:     make(map[gameplayActionLeaseKey]gameplayActionLease),
		actionAdmissions: make(map[gameplayActionLeaseKey]struct{}),
		actionTerminals:  make(map[gameplayActionLeaseKey]struct{}),
		arenaMatches:     make(map[uint32]*arenaMatchState),
		timer:            timer, now: now, logger: logger,
	}
	registry.producerGuard.registry = registry
	registry.lifecycle.registry = registry
	return registry
}

func gameplaySessionMemberKey(peerSession gameplayPeerSession) gameplayMemberKey {
	return gameplayMemberKey{
		gameID: peerSession.binding.GameID,
		userID: peerSession.binding.UserID,
	}
}

func (e *gameplaySessionRegistry) memberMutex(
	key gameplayMemberKey,
) *sync.Mutex {
	index := (uint64(key.gameID) ^ key.userID) % uint64(len(e.memberMutexes))
	return &e.memberMutexes[index]
}

func (e *gameplaySessionRegistry) lockMember(
	ctx context.Context, key gameplayMemberKey,
) (*sync.Mutex, error) {
	memberMutex := e.memberMutex(key)
	if memberMutex.TryLock() {
		return memberMutex, nil
	}
	ticker := time.NewTicker(memberLockRetryDelay)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("memberLock: %w", ctx.Err())
		case <-ticker.C:
			if memberMutex.TryLock() {
				return memberMutex, nil
			}
		}
	}
}

func (e *gameplaySessionRegistry) beginRequest() bool {
	if e == nil {
		return false
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.isClosed {
		return false
	}
	if e.activeRequest == 0 {
		e.requestIdle = make(chan struct{})
	}
	e.activeRequest++
	return true
}

func (e *gameplaySessionRegistry) endRequest() {
	if e == nil {
		return
	}
	e.mutex.Lock()
	if e.activeRequest > 0 {
		e.activeRequest--
	}
	if e.activeRequest == 0 && e.requestIdle != nil {
		close(e.requestIdle)
		e.requestIdle = nil
	}
	e.mutex.Unlock()
}

func (e *gameplaySessionRegistry) activeSessionKeys(
	key gameplayMemberKey, excludedSessionKey string,
) []string {
	if e == nil || key.gameID == 0 || key.userID == 0 {
		return nil
	}
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	sessionKeys := make([]string, 0, 1)
	for sessionKey, peerSession := range e.sessions {
		if sessionKey == excludedSessionKey {
			continue
		}
		if gameplaySessionMemberKey(peerSession) == key {
			sessionKeys = append(sessionKeys, sessionKey)
		}
	}
	slices.Sort(sessionKeys)
	return sessionKeys
}

func (e *gameplaySessionRegistry) takeRetained(
	ctx context.Context, key gameplayMemberKey,
) (gameplayPeerSession, bool) {
	e.mutex.RLock()
	retained := e.retainedSessions[key]
	e.mutex.RUnlock()
	if retained == nil {
		return gameplayPeerSession{}, false
	}
	select {
	case <-retained.ready:
	case <-ctx.Done():
		return gameplayPeerSession{}, false
	}
	e.mutex.Lock()
	if e.retainedSessions[key] != retained {
		e.mutex.Unlock()
		return gameplayPeerSession{}, false
	}
	if e.now().After(retained.expiresAt) {
		delete(e.retainedSessions, key)
		if retained.cancel != nil {
			retained.cancel()
		}
		e.mutex.Unlock()
		leaveGameplayPeerMembership(retained.peerSession)
		return gameplayPeerSession{}, false
	}
	delete(e.retainedSessions, key)
	if retained.cancel != nil {
		retained.cancel()
	}
	e.mutex.Unlock()
	return retained.peerSession, true
}

func (e *gameplaySessionRegistry) clearActionLeasesLocked(
	sessionKey string, generation uint64,
) {
	for key, lease := range e.actionLeases {
		if key.sessionKey == sessionKey &&
			(generation == 0 || key.transportGeneration == generation) {
			delete(e.actionLeases, key)
			if lease.cancel != nil {
				lease.cancel()
			}
		}
	}
	for key := range e.actionTerminals {
		if key.sessionKey == sessionKey &&
			(generation == 0 || key.transportGeneration == generation) {
			delete(e.actionTerminals, key)
		}
	}
	for key := range e.actionAdmissions {
		if key.sessionKey == sessionKey &&
			(generation == 0 || key.transportGeneration == generation) {
			delete(e.actionAdmissions, key)
		}
	}
}

func (e *gameplaySessionRegistry) clearActionLeasesForSync(
	sessionKey string, transportGeneration uint64, objectID uint32,
	syncStamp uint8,
) int {
	e.mutex.Lock()
	cancel := make([]raknet.CancelSchedule, 0, 1)
	cleared := 0
	for key, lease := range e.actionLeases {
		isMatch := key.sessionKey == sessionKey &&
			key.transportGeneration == transportGeneration &&
			key.syncStamp == syncStamp &&
			lease.command.Common.ObjectID == objectID
		if !isMatch {
			continue
		}
		delete(e.actionLeases, key)
		delete(e.actionTerminals, key)
		if lease.cancel != nil {
			cancel = append(cancel, lease.cancel)
		}
		cleared++
	}
	e.mutex.Unlock()
	for _, stop := range cancel {
		stop()
	}
	return cleared
}

func (e *gameplaySessionRegistry) clearPursuitActionLeases(
	sessionKey string, transportGeneration uint64, objectID uint32,
) int {
	e.mutex.Lock()
	cancel := make([]raknet.CancelSchedule, 0, 1)
	cleared := 0
	for key, lease := range e.actionLeases {
		isMatch := key.sessionKey == sessionKey &&
			key.transportGeneration == transportGeneration &&
			lease.command.Common.ObjectID == objectID && lease.isPursuit
		if !isMatch {
			continue
		}
		delete(e.actionLeases, key)
		delete(e.actionTerminals, key)
		if lease.cancel != nil {
			cancel = append(cancel, lease.cancel)
		}
		cleared++
	}
	e.mutex.Unlock()
	for _, stop := range cancel {
		stop()
	}
	return cleared
}

type gameplaySessionLifecycle struct {
	registry *gameplaySessionRegistry
}

func (e gameplaySessionLifecycle) discardGame(
	gameID uint32, modifierInstancePool *modifierPool,
	effectPool *attachedEffectPool,
) {
	if e.registry == nil || gameID == 0 {
		return
	}
	e.registry.mutex.Lock()
	peerSessions := make([]gameplayPeerSession, 0)
	retainedSessions := make([]gameplayPeerSession, 0)
	cancellations := make([]func(), 0)
	for sessionKey, peerSession := range e.registry.sessions {
		if peerSession.binding.GameID != gameID {
			continue
		}
		peerSessions = append(peerSessions, peerSession)
		delete(e.registry.sessions, sessionKey)
		for key, lease := range e.registry.actionLeases {
			if key.sessionKey != sessionKey ||
				key.transportGeneration != peerSession.transportGeneration {
				continue
			}
			delete(e.registry.actionLeases, key)
			delete(e.registry.actionAdmissions, key)
			delete(e.registry.actionTerminals, key)
			if lease.cancel != nil {
				cancellations = append(cancellations, lease.cancel)
			}
		}
		for key := range e.registry.actionAdmissions {
			if key.sessionKey == sessionKey &&
				key.transportGeneration == peerSession.transportGeneration {
				delete(e.registry.actionAdmissions, key)
			}
		}
		for key := range e.registry.actionTerminals {
			if key.sessionKey == sessionKey &&
				key.transportGeneration == peerSession.transportGeneration {
				delete(e.registry.actionTerminals, key)
			}
		}
	}
	for key, retained := range e.registry.retainedSessions {
		if key.gameID != gameID {
			continue
		}
		if retained.cancel != nil {
			cancellations = append(cancellations, retained.cancel)
		}
		retainedSessions = append(
			retainedSessions, retained.peerSession,
		)
		delete(e.registry.retainedSessions, key)
	}
	for key := range e.registry.memberTransports {
		if key.gameID == gameID {
			delete(e.registry.memberTransports, key)
			delete(e.registry.memberEndpoints, key)
		}
	}
	e.registry.mutex.Unlock()
	for _, cancel := range cancellations {
		cancel()
	}
	for _, peerSession := range peerSessions {
		stopGameplayPeerSession(
			peerSession, modifierInstancePool, effectPool,
		)
	}
	for _, peerSession := range retainedSessions {
		leaveGameplayPeerMembership(peerSession)
	}
}

func (e gameplaySessionLifecycle) discardMember(
	gameID uint32, userID uint64, modifierInstancePool *modifierPool,
	effectPool *attachedEffectPool,
) {
	if e.registry == nil || gameID == 0 || userID == 0 {
		return
	}
	memberKey := gameplayMemberKey{gameID: gameID, userID: userID}
	e.registry.mutex.Lock()
	peerSessions := make([]gameplayPeerSession, 0, 1)
	retainedSessions := make([]gameplayPeerSession, 0, 1)
	cancellations := make([]func(), 0)
	for sessionKey, peerSession := range e.registry.sessions {
		if gameplaySessionMemberKey(peerSession) != memberKey {
			continue
		}
		peerSessions = append(peerSessions, peerSession)
		delete(e.registry.sessions, sessionKey)
		for key, lease := range e.registry.actionLeases {
			if key.sessionKey != sessionKey ||
				key.transportGeneration != peerSession.transportGeneration {
				continue
			}
			delete(e.registry.actionLeases, key)
			delete(e.registry.actionAdmissions, key)
			delete(e.registry.actionTerminals, key)
			if lease.cancel != nil {
				cancellations = append(cancellations, lease.cancel)
			}
		}
		for key := range e.registry.actionAdmissions {
			if key.sessionKey == sessionKey &&
				key.transportGeneration == peerSession.transportGeneration {
				delete(e.registry.actionAdmissions, key)
			}
		}
		for key := range e.registry.actionTerminals {
			if key.sessionKey == sessionKey &&
				key.transportGeneration == peerSession.transportGeneration {
				delete(e.registry.actionTerminals, key)
			}
		}
	}
	retained := e.registry.retainedSessions[memberKey]
	if retained != nil {
		if retained.cancel != nil {
			cancellations = append(cancellations, retained.cancel)
		}
		retainedSessions = append(retainedSessions, retained.peerSession)
		delete(e.registry.retainedSessions, memberKey)
	}
	delete(e.registry.memberTransports, memberKey)
	delete(e.registry.memberEndpoints, memberKey)
	e.registry.mutex.Unlock()
	for _, cancel := range cancellations {
		cancel()
	}
	for _, peerSession := range peerSessions {
		stopGameplayPeerSession(
			peerSession, modifierInstancePool, effectPool,
		)
	}
	for _, peerSession := range retainedSessions {
		leaveGameplayPeerMembership(peerSession)
	}
}

func (e gameplaySessionLifecycle) nextGeneration() uint64 {
	return atomic.AddUint64(&e.registry.nextGeneration, 1)
}

func (e gameplaySessionLifecycle) nextActionGeneration() uint64 {
	return atomic.AddUint64(&e.registry.nextActionGeneration, 1)
}

func (e gameplaySessionLifecycle) cleanup(
	modifierInstancePool *modifierPool, effectPool *attachedEffectPool,
) {
	e.registry.mutex.Lock()
	e.registry.isClosed = true
	requestIdle := e.registry.requestIdle
	activeRequest := e.registry.activeRequest
	e.registry.mutex.Unlock()
	if activeRequest > 0 && requestIdle != nil {
		timer := time.NewTimer(gameplayCleanupWait)
		select {
		case <-requestIdle:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-timer.C:
			if e.registry.logger != nil {
				e.registry.logger.Printf(
					"RakNet gameplay shutdown continuing with %d active requests after %s",
					activeRequest, gameplayCleanupWait,
				)
			}
		}
	}
	e.registry.mutex.Lock()
	peerSessions := make([]gameplayPeerSession, 0, len(e.registry.sessions))
	retainedSessions := make([]gameplayPeerSession, 0, len(e.registry.retainedSessions))
	arenaCancels := make([]func(), 0, len(e.registry.arenaMatches))
	for sessionKey, peerSession := range e.registry.sessions {
		peerSessions = append(peerSessions, peerSession)
		delete(e.registry.sessions, sessionKey)
	}
	for key, retained := range e.registry.retainedSessions {
		if retained.cancel != nil {
			retained.cancel()
		}
		retainedSessions = append(retainedSessions, retained.peerSession)
		delete(e.registry.retainedSessions, key)
	}
	for gameID, state := range e.registry.arenaMatches {
		if state.cancel != nil {
			arenaCancels = append(arenaCancels, state.cancel)
		}
		delete(e.registry.arenaMatches, gameID)
	}
	clear(e.registry.actionLeases)
	clear(e.registry.actionAdmissions)
	clear(e.registry.actionTerminals)
	clear(e.registry.memberTransports)
	clear(e.registry.memberEndpoints)
	e.registry.mutex.Unlock()

	for _, cancel := range arenaCancels {
		cancel()
	}
	for _, peerSession := range peerSessions {
		stopGameplayPeerSession(peerSession, modifierInstancePool, effectPool)
	}
	for _, peerSession := range retainedSessions {
		leaveGameplayPeerMembership(peerSession)
	}
}

func (e gameplaySessionLifecycle) disconnect(
	sessionKey string, transportGeneration uint64,
	modifierInstancePool *modifierPool,
	effectPool *attachedEffectPool,
) {
	e.registry.mutex.RLock()
	peerSession, isFound := e.registry.sessions[sessionKey]
	memberKey := gameplaySessionMemberKey(peerSession)
	currentTransport := e.registry.memberTransports[memberKey]
	e.registry.mutex.RUnlock()
	if !isFound || (transportGeneration != 0 &&
		peerSession.transportGeneration != transportGeneration) {
		return
	}
	if transportGeneration != 0 && currentTransport != 0 &&
		currentTransport != transportGeneration {
		e.recoverStaleTransport(sessionKey, transportGeneration)
		return
	}
	memberMutex := e.registry.memberMutex(memberKey)
	if !memberMutex.TryLock() {
		e.retryDisconnect(
			sessionKey, transportGeneration,
			modifierInstancePool, effectPool,
		)
		return
	}
	defer memberMutex.Unlock()
	e.registry.mutex.RLock()
	currentSession, isCurrentFound := e.registry.sessions[sessionKey]
	isCurrent := isCurrentFound && (transportGeneration == 0 ||
		currentSession.transportGeneration == transportGeneration)
	if isCurrent && transportGeneration != 0 {
		memberKey = gameplaySessionMemberKey(currentSession)
		currentTransport = e.registry.memberTransports[memberKey]
		isCurrent = currentTransport == 0 || currentTransport == transportGeneration
	}
	e.registry.mutex.RUnlock()
	if !isCurrent {
		return
	}
	e.disconnectMember(sessionKey, modifierInstancePool, effectPool)
}

func (e gameplaySessionLifecycle) recoverStaleTransport(
	sessionKey string, transportGeneration uint64,
) {
	if e.registry == nil || sessionKey == "" || transportGeneration == 0 {
		return
	}
	e.registry.mutex.Lock()
	peerSession, isFound := e.registry.sessions[sessionKey]
	if !isFound || peerSession.transportGeneration != transportGeneration {
		e.registry.mutex.Unlock()
		return
	}
	key := gameplaySessionMemberKey(peerSession)
	currentTransport := e.registry.memberTransports[key]
	currentEndpoint := e.registry.memberEndpoints[key]
	e.registry.clearActionLeasesLocked(sessionKey, transportGeneration)
	if currentTransport == 0 || currentEndpoint != sessionKey {
		delete(e.registry.sessions, sessionKey)
		e.registry.mutex.Unlock()
		return
	}
	peerSession.transportGeneration = currentTransport
	peerSession.isRejoinPending = true
	peerSession.isNPCRecoveryPending = false
	peerSession.resetRetainedTransportState()
	e.registry.sessions[sessionKey] = peerSession
	e.registry.mutex.Unlock()
	if peerSession.zone == nil || !peerSession.stage.IsDungeon() {
		return
	}
	_, _ = peerSession.zone.Disconnect(
		peerSession.binding.UserID, peerSession.generation,
	)
	if e.registry.logger != nil {
		e.registry.logger.Printf(
			"RakNet stale same-endpoint session converted to baseline recovery game=%d user=%d remote=%s old_transport_generation=%d current_transport_generation=%d",
			key.gameID, key.userID, sessionKey, transportGeneration,
			currentTransport,
		)
	}
}

func (e gameplaySessionLifecycle) retryDisconnect(
	sessionKey string, transportGeneration uint64,
	modifierInstancePool *modifierPool, effectPool *attachedEffectPool,
) {
	if e.registry.timer == nil {
		if e.registry.logger != nil {
			e.registry.logger.Printf(
				"RakNet gameplay disconnect deferred without timer remote=%s transport_generation=%d",
				sessionKey, transportGeneration,
			)
		}
		return
	}
	_, err := e.registry.timer.Schedule(disconnectRetryDelay, func() {
		e.disconnect(
			sessionKey, transportGeneration,
			modifierInstancePool, effectPool,
		)
	})
	if err != nil && e.registry.logger != nil {
		e.registry.logger.Printf(
			"RakNet gameplay disconnect retry failed remote=%s transport_generation=%d: %v",
			sessionKey, transportGeneration, err,
		)
	}
}

func (e gameplaySessionLifecycle) disconnectMember(
	sessionKey string, modifierInstancePool *modifierPool,
	effectPool *attachedEffectPool,
) {
	e.registry.mutex.Lock()
	peerSession, isFound := e.registry.sessions[sessionKey]
	if !isFound {
		e.registry.mutex.Unlock()
		return
	}
	delete(e.registry.sessions, sessionKey)
	e.registry.clearActionLeasesLocked(sessionKey, 0)
	key := gameplaySessionMemberKey(peerSession)
	if e.registry.memberTransports[key] == peerSession.transportGeneration {
		delete(e.registry.memberTransports, key)
		delete(e.registry.memberEndpoints, key)
	}
	isRetainable := peerSession.zone != nil && peerSession.stage.IsDungeon()
	if !isRetainable {
		e.registry.mutex.Unlock()
		stopGameplayPeerSession(peerSession, modifierInstancePool, effectPool)
		return
	}
	e.registry.mutex.Unlock()
	disconnectedAt := e.registry.now()
	moveErr := peerSession.stopPlayerMovement(disconnectedAt)
	if moveErr != nil && e.registry.logger != nil {
		e.registry.logger.Printf(
			"RakNet retained movement stop failed game=%d user=%d generation=%d: %v",
			key.gameID, key.userID, peerSession.generation, moveErr,
		)
	}
	peerSession.basicSequenceSession().ReleaseHeld()
	peerSession.campaignPlayerPursuitSession().Cancel()
	poseErr := peerSession.syncZoneHeroPose()
	if poseErr != nil && e.registry.logger != nil {
		e.registry.logger.Printf(
			"RakNet retained hero pose failed game=%d user=%d generation=%d: %v",
			key.gameID, key.userID, peerSession.generation, poseErr,
		)
	}
	_, isDisconnected := peerSession.zone.Disconnect(
		peerSession.binding.UserID, peerSession.generation,
	)
	if !isDisconnected {
		stopGameplayPeerRuntime(peerSession, modifierInstancePool, effectPool)
		if e.registry.logger != nil {
			e.registry.logger.Printf(
				"RakNet duplicate or stale gameplay disconnect quiesced game=%d user=%d generation=%d",
				key.gameID, key.userID, peerSession.generation,
			)
		}
		return
	}
	retainedCompanions := make([]zonecompanion.Actor, 0)
	if peerSession.zone.Companion() != nil {
		for _, companion := range peerSession.zone.Companion().Snapshots() {
			if companion.UserID == peerSession.binding.UserID &&
				companion.PeerGeneration == peerSession.generation {
				retainedCompanions = append(retainedCompanions, companion)
			}
		}
	}
	stopGameplayPeerRuntime(peerSession, modifierInstancePool, effectPool)
	for _, companion := range retainedCompanions {
		err := peerSession.zone.Companion().Put(companion)
		if err != nil && e.registry.logger != nil {
			e.registry.logger.Printf(
				"RakNet retained companion restore failed game=%d user=%d object=%d: %v",
				peerSession.binding.GameID, peerSession.binding.UserID,
				companion.ObjectID, err,
			)
		}
	}
	peerSession.zoneEffectPresentation = zoneEffectPresentation{}
	peerSession.zonePresentationRuntime = zonePresentationRuntime{}
	peerSession.controlledHeroPresentation = controlledHeroPresentation{}
	peerSession.resetRetainedTransportState()
	peerSession.isRejoinPending = true
	retained := &gameplayRetainedSession{
		peerSession: peerSession,
		expiresAt:   e.registry.now().Add(gameplayRejoinGrace),
		ready:       make(chan struct{}),
	}
	close(retained.ready)
	if e.registry.timer == nil {
		leaveGameplayPeerMembership(peerSession)
		return
	}
	e.registry.mutex.Lock()
	if e.registry.isClosed {
		e.registry.mutex.Unlock()
		leaveGameplayPeerMembership(peerSession)
		return
	}
	for activeKey, active := range e.registry.sessions {
		if active.zone != peerSession.zone || active.isZoneTerminal() {
			continue
		}
		active.isNPCRecoveryPending = true
		e.registry.sessions[activeKey] = active
	}
	previous := e.registry.retainedSessions[key]
	e.registry.retainedSessions[key] = retained
	e.registry.mutex.Unlock()
	cancel, err := e.registry.timer.Schedule(
		gameplayRejoinGrace, func() {
			e.expireRetained(key, retained)
		},
	)
	if err != nil {
		e.registry.mutex.Lock()
		if e.registry.retainedSessions[key] == retained {
			delete(e.registry.retainedSessions, key)
		}
		e.registry.mutex.Unlock()
		if previous != nil && previous.cancel != nil {
			previous.cancel()
		}
		leaveGameplayPeerMembership(peerSession)
		if e.registry.logger != nil {
			e.registry.logger.Printf(
				"RakNet rejoin expiry scheduling failed game=%d user=%d: %v",
				key.gameID, key.userID, err,
			)
		}
		return
	}
	e.registry.mutex.Lock()
	isCurrentRetained := e.registry.retainedSessions[key] == retained
	if isCurrentRetained {
		retained.cancel = cancel
	}
	e.registry.mutex.Unlock()
	if !isCurrentRetained {
		cancel()
		return
	}
	if previous != nil {
		if previous.cancel != nil {
			previous.cancel()
		}
		isSameMembership := previous.peerSession.zone == peerSession.zone &&
			previous.peerSession.generation == peerSession.generation
		if !isSameMembership {
			leaveGameplayPeerMembership(previous.peerSession)
		}
	}
	if e.registry.logger != nil {
		e.registry.logger.Printf(
			"RakNet gameplay transport retained game=%d user=%d until=%s",
			key.gameID, key.userID, retained.expiresAt,
		)
	}
}

func (e gameplaySessionLifecycle) expireRetained(
	key gameplayMemberKey, expected *gameplayRetainedSession,
) {
	e.registry.mutex.Lock()
	retained := e.registry.retainedSessions[key]
	if retained != expected {
		e.registry.mutex.Unlock()
		return
	}
	delete(e.registry.retainedSessions, key)
	e.registry.mutex.Unlock()
	leaveGameplayPeerMembership(retained.peerSession)
	if e.registry.logger != nil {
		e.registry.logger.Printf(
			"RakNet gameplay rejoin grace expired game=%d user=%d",
			key.gameID, key.userID,
		)
	}
}

// gameplayProducerGuard prevents delayed transport work from outliving the
// member, zone, or peer generation that admitted it. The session collection
// remains storage; this role owns callback validity and schedule decoration.
type gameplayProducerGuard struct {
	registry *gameplaySessionRegistry
}

type gameplayProducerObserver struct {
	guard       gameplayProducerGuard
	identity    gameplayProducerIdentity
	produceFunc func() ([][]byte, error)
	commitFunc  func()
	peerPackets [][]byte
	isProduced  bool
}

func (e *gameplayProducerObserver) produce() ([][]byte, error) {
	if !e.guard.isCurrent(e.identity) {
		return nil, nil
	}
	packets, err := e.produceFunc()
	if err != nil {
		return packets, err
	}
	e.peerPackets = packets
	e.isProduced = true
	return packets, nil
}

func (e *gameplayProducerObserver) commit() {
	if !e.isProduced {
		return
	}
	if e.commitFunc != nil {
		e.commitFunc()
	}
	e.guard.registry.queuePeerPresentation(e.identity, e.peerPackets)
}

func gameplayPeerPresentationPackets(packets [][]byte) [][]byte {
	presentations := make([][]byte, 0, len(packets))
	for _, packet := range packets {
		if len(packet) == 0 {
			continue
		}
		switch raknet.PacketID(packet[0]) {
		case raknet.ObjectCreate, raknet.ObjectUpdate, raknet.ObjectDelete,
			raknet.ObjectJump, raknet.ObjectTeleport, raknet.ObjectPlayerMove,
			raknet.ForcePhysicsUpdate, raknet.PhysicsChanged,
			raknet.LocomotionUpdate, raknet.LocomotionUnreliable,
			raknet.DirectorState,
			raknet.LootDataUpdate, raknet.InteractableUpdate,
			raknet.AttributeDataUpdate, raknet.CombatantDataUpdate,
			raknet.AgentBlackboardUpdate,
			raknet.ServerEvent, raknet.ModifierCreated, raknet.ModifierUpdated,
			raknet.ModifierDeleted, raknet.SetAnimationState,
			raknet.SetObjectGFXState, raknet.PlayerCharacterDeploy,
			raknet.CombatEvent,
			raknet.GravityForceUpdate:
			presentations = append(presentations, packet)
		}
	}
	return presentations
}

func (e *gameplaySessionRegistry) queuePeerPresentation(
	identity gameplayProducerIdentity, packets [][]byte,
) int {
	if e == nil || !identity.isFound || len(packets) == 0 {
		return 0
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	peerSession, isFound := e.sessions[identity.sessionKey]
	isArena := isFound && peerSession.binding.Mode == game.ModeArena
	isCurrent := isFound &&
		gameplaySessionMemberKey(peerSession) == identity.memberKey &&
		peerSession.generation == identity.zoneGeneration &&
		peerSession.transportGeneration == identity.transportGeneration &&
		peerSession.stage.IsDungeon() &&
		(isArena || peerSession.isCampaignPresentationAvailable())
	if !isCurrent {
		return 0
	}
	presentationPackets := gameplayPeerPresentationPackets(packets)
	if len(presentationPackets) == 0 {
		return 0
	}
	recipients := 0
	for sessionKey, candidate := range e.sessions {
		isCandidateArena := isArena && candidate.binding.Mode == game.ModeArena
		isCandidateCampaign := !isArena && candidate.zone == peerSession.zone &&
			candidate.isCampaignPresentationAvailable()
		if sessionKey == identity.sessionKey ||
			candidate.isRejoinPending ||
			candidate.binding.GameID != peerSession.binding.GameID ||
			(!candidate.stage.IsDungeon() && !isCandidateCampaign) ||
			(!isCandidateArena && !isCandidateCampaign) {
			continue
		}
		candidatePackets := presentationPackets
		if isCandidateCampaign {
			// Queue prerequisite zone events before the exact presentation. In
			// particular, a delayed movement must not overtake its NPC spawn.
			queueErr := candidate.queueCampaignPresentation(presentationPackets)
			if queueErr != nil && e.logger != nil {
				e.logger.Printf("RakNet co-op presentation queue failed game=%d user=%d: %v",
					candidate.binding.GameID, candidate.binding.UserID, queueErr)
			}
			e.sessions[sessionKey] = candidate
			recipients++
			continue
		}
		if len(candidatePackets) == 0 {
			continue
		}
		publishErr := candidate.publishPackets(candidatePackets)
		if publishErr != nil && e.logger != nil {
			e.logger.Printf(
				"RakNet co-op presentation queued after immediate delivery failed game=%d user=%d: %v",
				candidate.binding.GameID, candidate.binding.UserID, publishErr,
			)
		}
		e.sessions[sessionKey] = candidate
		recipients++
	}
	return recipients
}

type gameplayProducerIdentity struct {
	sessionKey          string
	memberKey           gameplayMemberKey
	zoneGeneration      uint64
	transportGeneration uint64
	isFound             bool
}

func (e gameplayProducerGuard) identity(
	sessionKey string,
) gameplayProducerIdentity {
	e.registry.mutex.RLock()
	peerSession, isFound := e.registry.sessions[sessionKey]
	e.registry.mutex.RUnlock()
	return gameplayProducerIdentityFromSession(sessionKey, peerSession, isFound)
}

func gameplayProducerIdentityFromSession(
	sessionKey string, peerSession gameplayPeerSession, isFound bool,
) gameplayProducerIdentity {
	return gameplayProducerIdentity{
		sessionKey:          sessionKey,
		memberKey:           gameplaySessionMemberKey(peerSession),
		zoneGeneration:      peerSession.generation,
		transportGeneration: peerSession.transportGeneration,
		isFound:             isFound,
	}
}

func (e gameplayProducerGuard) isCurrent(
	identity gameplayProducerIdentity,
) bool {
	if !identity.isFound || identity.zoneGeneration == 0 ||
		identity.transportGeneration == 0 {
		return false
	}
	e.registry.mutex.RLock()
	peerSession, isFound := e.registry.sessions[identity.sessionKey]
	isCurrent := isFound &&
		gameplaySessionMemberKey(peerSession) == identity.memberKey &&
		peerSession.generation == identity.zoneGeneration &&
		peerSession.transportGeneration == identity.transportGeneration &&
		!peerSession.isZoneTerminal()
	e.registry.mutex.RUnlock()
	return isCurrent
}

func (e gameplayProducerGuard) producer(
	sessionKey string, produce func() ([][]byte, error),
) func() ([][]byte, error) {
	identity := e.identity(sessionKey)
	return func() ([][]byte, error) {
		if !e.isCurrent(identity) {
			return nil, nil
		}
		return produce()
	}
}

func (e gameplayProducerGuard) schedule(
	sessionKey string, schedule schedulePacketFunc,
) schedulePacketFunc {
	return func(delay time.Duration, produce func() ([][]byte, error)) error {
		return schedule(delay, e.producer(sessionKey, produce))
	}
}

func (e gameplayProducerGuard) scheduledProducers(
	sessionKey string, producers []raknet.ScheduledPacketProducer,
) []raknet.ScheduledPacketProducer {
	identity := e.identity(sessionKey)
	return e.scheduledProducersForIdentity(identity, producers)
}

func (e gameplayProducerGuard) scheduledProducersForIdentity(
	identity gameplayProducerIdentity, producers []raknet.ScheduledPacketProducer,
) []raknet.ScheduledPacketProducer {
	guardedProducers := make([]raknet.ScheduledPacketProducer, len(producers))
	for index, producer := range producers {
		guardedProducers[index] = e.scheduledProducer(identity, producer)
	}
	return guardedProducers
}

func (e gameplayProducerGuard) scheduledProducer(
	identity gameplayProducerIdentity,
	producer raknet.ScheduledPacketProducer,
) raknet.ScheduledPacketProducer {
	if producer.Produce == nil {
		return producer
	}
	observer := &gameplayProducerObserver{
		guard: e, identity: identity, produceFunc: producer.Produce,
		commitFunc: producer.AfterCommit,
	}
	producer.Produce = observer.produce
	producer.AfterCommit = observer.commit
	return producer
}

type campaignNPCProjectileAuthority struct {
	registry *gameplaySessionRegistry
}

func (e campaignNPCProjectileAuthority) retire(
	sessionKey string, generation uint64, objectID uint32,
	run *abilityraknet.ProjectileRun,
) {
	e.registry.mutex.Lock()
	peerSession, isFound := e.registry.sessions[sessionKey]
	if isFound && peerSession.generation == generation {
		peerSession.untrackCampaignNPCProjectile(objectID, run)
		e.registry.sessions[sessionKey] = peerSession
	}
	e.registry.mutex.Unlock()
	run.Stop()
}

func (e campaignNPCProjectileAuthority) complete(
	sessionKey string, generation uint64, objectID uint32,
	run *abilityraknet.ProjectileRun,
) {
	e.registry.mutex.Lock()
	peerSession, isFound := e.registry.sessions[sessionKey]
	if isFound && peerSession.generation == generation {
		peerSession.untrackCampaignNPCProjectile(objectID, run)
		e.registry.sessions[sessionKey] = peerSession
	}
	e.registry.mutex.Unlock()
	run.Finish()
}

// campaignActionAuthority owns the atomic player-action transitions that span
// delayed transport callbacks. Keeping this role separate prevents the
// session collection itself from becoming an implicit campaign feature port.
type campaignActionAuthority struct {
	registry *gameplaySessionRegistry
}

func (e campaignActionAuthority) expirePursuit(
	sessionKey string, sessionGeneration uint64, pursuitGeneration uint64,
) (raknet.Vector3, bool, error) {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()

	peerSession, isFound := e.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == sessionGeneration
	if !isCurrent ||
		!peerSession.campaignPlayerPursuitSession().Expire(pursuitGeneration) {
		return raknet.Vector3{}, false, nil
	}
	err := peerSession.stopPlayerMovement(e.registry.now())
	e.registry.sessions[sessionKey] = peerSession
	if err != nil {
		return peerSession.playerPosition, true, fmt.Errorf("pursuitExpireStop: %w", err)
	}
	return peerSession.playerPosition, true, nil
}

func (e campaignActionAuthority) cancel(
	sessionKey string, objectID uint32,
) (zoneaction.PursuitSnapshot, *abilityraknet.MeleeRun, *heroDrainRun, bool) {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()

	peerSession, isFound := e.registry.sessions[sessionKey]
	isCurrent := isFound && objectID == peerSession.deployedObjectID
	if !isCurrent {
		return zoneaction.PursuitSnapshot{}, nil, nil, false
	}
	pursuit := peerSession.campaignPlayerPursuitSession().Snapshot()
	basicAttack := peerSession.basicAttack
	basicSyncStamp := peerSession.basicAttackSyncStamp
	heroDrain := peerSession.heroDrain
	peerSession.heroDrain = nil
	interruptedBasic := peerSession.resetInterruptibleActionAdmission()
	// Build 103 sends ActionCancel when held basic input is released. Once the
	// swing owns a transport schedule, cancellation releases the held-input
	// generation but must not discard its pending authored hit continuation.
	if basicAttack != nil && basicAttack.IsCancelSet() {
		peerSession.basicAttack = basicAttack
		peerSession.basicAttackSyncStamp = basicSyncStamp
		interruptedBasic = nil
	}
	e.registry.sessions[sessionKey] = peerSession
	return pursuit, interruptedBasic, heroDrain, true
}

func (e campaignActionAuthority) admitPursuit(
	sessionKey string,
	objectID uint32,
	targetObjectID uint32,
	abilityIndex uint32,
	syncStamp uint8,
) campaignAbilityPursuitAdmission {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()

	peerSession, isFound := e.registry.sessions[sessionKey]
	isCurrent := isFound && objectID == peerSession.deployedObjectID
	if !isCurrent {
		return campaignAbilityPursuitAdmission{}
	}
	pursuit := peerSession.campaignPlayerPursuitSession().Snapshot()
	isRetry := peerSession.campaignPlayerPursuitSession().IsRetry(
		objectID, targetObjectID, abilityIndex, syncStamp,
	)
	if !isRetry {
		peerSession.campaignPlayerPursuitSession().Cancel()
	}
	e.registry.sessions[sessionKey] = peerSession
	return campaignAbilityPursuitAdmission{
		pursuit: pursuit,
		playerPosition: game.Vec3{
			X: peerSession.playerPosition.X,
			Y: peerSession.playerPosition.Y,
			Z: peerSession.playerPosition.Z,
		},
		isSessionFound: true,
		isRetry:        isRetry,
	}
}

type zoneSquadHealing struct {
	creatureIndex uint32
	objectID      uint32
	hitPoint      float32
	amount        float32
	isCompanion   bool
}

type zoneSquadManaRestoration struct {
	creatureIndex uint32
	objectID      uint32
	manaPoint     float32
	amount        float32
}

func zoneHealingStatDelta(healing []zoneSquadHealing) sporenet.PlayerStatDelta {
	statDelta := sporenet.PlayerStatDelta{}
	for _, currentHealing := range healing {
		statDelta.PVEHealing += float64(currentHealing.amount)
		statDelta.PVEHealingReceived += float64(currentHealing.amount)
	}
	return statDelta
}

func (s gameplayPeerSession) isZoneGameOver() bool {
	if s.binding.Mode == game.ModeChain && s.zone != nil {
		return s.zone.IsPartyDefeated()
	}
	return s.squad != nil && s.squad.IsGameOver()
}

func (s gameplayPeerSession) isZoneTerminal() bool {
	if s.isArenaResultReady || s.isZoneGameOver() {
		return true
	}
	if s.binding.Mode == game.ModeChain {
		return s.chainResult != nil
	}
	return s.zone != nil && s.zone.Boss() != nil &&
		s.zone.Boss().IsBeamOutCommitted()
}

func (s *gameplayPeerSession) beginTutorialRestart() bool {
	if s == nil || s.squad == nil {
		return false
	}
	return s.squad.ReserveRestart()
}

func (s *gameplayPeerSession) rollbackTutorialRestart() {
	if s == nil || s.squad == nil {
		return
	}
	s.squad.RollbackRestart()
}

func (s gameplayPeerSession) isTutorialRestartReserved() bool {
	return s.squad != nil && s.squad.IsGameOver() && s.squad.IsRestartReserved()
}

func (s *gameplayPeerSession) beginTutorialCompletion() (bool, error) {
	if s == nil || s.zone == nil || s.zone.Boss() == nil ||
		s.isZoneGameOver() {
		return false, nil
	}
	return s.zone.Boss().ReserveBeamOut(), nil
}

func (s *gameplayPeerSession) rollbackTutorialCompletion() error {
	if s == nil || s.zone == nil || s.zone.Boss() == nil {
		return nil
	}
	err := s.zone.Boss().RollbackBeamOut()
	if err != nil {
		return fmt.Errorf("completionRollback: %w", err)
	}
	return nil
}

func (s *gameplayPeerSession) commitTutorialCompletion() (bool, error) {
	if s == nil || s.zone == nil || s.zone.Boss() == nil {
		return false, nil
	}
	return s.zone.Boss().CommitBeamOut(), nil
}

// restartTutorialSession preserves only transport identity and the durable
// gameplay binding. A returning player rebuilds every match-owned object,
// cooldown, fact, and schedule in a new epoch through the normal setup path.
func restartTutorialSession(
	previous gameplayPeerSession, generation uint64, experience sporenet.TutorialExperience,
) gameplayPeerSession {
	binding := previous.binding
	binding.AvatarXP = float32(experience.CumulativeXP)
	binding.AvatarLevel = experience.Level
	return gameplayPeerSession{
		zoneMembership:      zoneMembership{generation: generation},
		binding:             binding,
		transportGeneration: previous.transportGeneration,
		schedulePackets:     previous.schedulePackets,
		schedulePacket:      previous.schedulePacket,
	}
}

func (s gameplayPeerSession) deployedManaPoint() float32 {
	if s.squad == nil {
		return 0
	}
	character, isFound := s.squad.DeployedCharacter()
	if !isFound || !character.IsAvailable {
		return 0
	}
	return character.ManaPoints
}

func (s gameplayPeerSession) deployedHitPoint() float32 {
	if s.squad == nil {
		return 0
	}
	character, isFound := s.squad.DeployedCharacter()
	if !isFound || !character.IsAvailable {
		return 0
	}
	return character.HitPoints
}

func (s *gameplayPeerSession) setDeployedHitPoints(hitPoints float32) (bool, error) {
	if s == nil || s.squad == nil {
		return false, errors.New("nil zone squad")
	}
	if s.squad.IsGameOver() {
		return false, nil
	}
	previousHitPoint := s.deployedHitPoint()
	previousManaPoint := s.deployedManaPoint()
	if previousHitPoint <= 0 && hitPoints > 0 {
		return false, errors.New("defeated hero cannot receive ordinary healing")
	}
	isZone := s.zone != nil && s.deployedObjectID != 0
	if isZone {
		_, err := s.zone.SetHeroResources(
			s.binding.UserID,
			s.generation,
			s.deployedObjectID,
			hitPoints,
			previousManaPoint,
		)
		if err != nil {
			return false, fmt.Errorf("heroHealth: %w", err)
		}
	}
	isGameOver, err := s.squad.SetHitPoints(s.squad.DeployedIndex(), hitPoints)
	if err != nil {
		if isZone {
			_, rollbackErr := s.zone.SetHeroResources(
				s.binding.UserID,
				s.generation,
				s.deployedObjectID,
				previousHitPoint,
				previousManaPoint,
			)
			err = errors.Join(err, rollbackErr)
		}
		return false, fmt.Errorf("squadHealth: %w", err)
	}
	err = s.syncZoneSquadCheckpoint()
	if err != nil {
		return false, fmt.Errorf("squadHealthCheckpoint: %w", err)
	}
	return isGameOver, nil
}

func (s *gameplayPeerSession) setDeployedManaPoints(manaPoints float32) error {
	if s == nil || s.squad == nil {
		return errors.New("nil zone squad")
	}
	previousHitPoint := s.deployedHitPoint()
	previousManaPoint := s.deployedManaPoint()
	isZone := s.zone != nil && s.deployedObjectID != 0
	if isZone {
		_, err := s.zone.SetHeroResources(
			s.binding.UserID,
			s.generation,
			s.deployedObjectID,
			previousHitPoint,
			manaPoints,
		)
		if err != nil {
			return fmt.Errorf("heroMana: %w", err)
		}
	}
	err := s.squad.SetManaPoints(s.squad.DeployedIndex(), manaPoints)
	if err != nil {
		if isZone {
			_, rollbackErr := s.zone.SetHeroResources(
				s.binding.UserID,
				s.generation,
				s.deployedObjectID,
				previousHitPoint,
				previousManaPoint,
			)
			err = errors.Join(err, rollbackErr)
		}
		return fmt.Errorf("squadMana: %w", err)
	}
	err = s.syncZoneSquadCheckpoint()
	if err != nil {
		return fmt.Errorf("squadManaCheckpoint: %w", err)
	}
	return nil
}

func (s *gameplayPeerSession) applyDamageHitPoints(hitPoints float32, timestamp uint64) ([][]byte, error) {
	return s.applyDamageHitPointsWithOutcome(hitPoints, timestamp, true)
}

func (s *gameplayPeerSession) applyArenaDamageHitPoints(
	hitPoints float32, timestamp uint64,
) ([][]byte, error) {
	return s.applyDamageHitPointsWithOutcome(hitPoints, timestamp, false)
}

func (s *gameplayPeerSession) applyDamageHitPointsWithOutcome(
	hitPoints float32, timestamp uint64, isGameOverPacketEnabled bool,
) ([][]byte, error) {
	if s == nil || s.squad == nil {
		return nil, errors.New("damage squad unavailable")
	}
	previousHitPoint := s.deployedHitPoint()
	if previousHitPoint <= 0 && hitPoints <= 0 {
		return nil, nil
	}
	isLethal := previousHitPoint > 0 && hitPoints <= 0
	isExpectedGameOver := isLethal && s.squad.LivingCount() == 1
	var transitionPackets [][]byte
	livingIndex := uint32(squad.Size)
	if isExpectedGameOver && isGameOverPacketEnabled && s.binding.Mode != game.ModeChain {
		packet, err := outcomeraknet.GameOver()
		if err != nil {
			return nil, fmt.Errorf("gameOverMarshal: %w", err)
		}
		transitionPackets = [][]byte{packet}
	} else if isLethal && !isExpectedGameOver {
		for index := uint32(0); index < squad.Size; index++ {
			character, isFound := s.squad.Character(index)
			if isFound && character.IsAvailable && character.HitPoints > 0 {
				livingIndex = index
				break
			}
		}
		if livingIndex >= squad.Size ||
			s.deployedCreatureIndex >= uint32(len(s.binding.Creatures)) ||
			livingIndex >= uint32(len(s.binding.Creatures)) {
			return nil, errors.New("living zone character missing")
		}
		targetCharacter, isTargetCharacterFound := s.squad.Character(livingIndex)
		if !isTargetCharacterFound {
			return nil, fmt.Errorf("livingCharacter[%d]: missing", livingIndex)
		}
		targetObjectID := zonehero.ObjectID(s.binding.Slot, livingIndex)
		if targetObjectID == 0 {
			return nil, fmt.Errorf("livingObject[%d]: missing", livingIndex)
		}
		packets, err := marshalCampaignCharacterSwitch(
			uint8(s.binding.Slot), s.deployedObjectID, targetObjectID, livingIndex,
			s.binding.Creatures[s.deployedCreatureIndex], s.binding.Creatures[livingIndex],
			0, s.deployedManaPoint(), targetCharacter.HitPoints, targetCharacter.ManaPoints,
			s.playerPosition, raknet.Quaternion{W: 1}, timestamp,
			false,
		)
		if err != nil {
			return nil, fmt.Errorf("livingSwitchMarshal: %w", err)
		}
		transitionPackets = packets
	}
	isGameOver, err := s.setDeployedHitPoints(hitPoints)
	if err != nil {
		return nil, fmt.Errorf("damageHealth: %w", err)
	}
	if isGameOver {
		return transitionPackets, nil
	}
	if s.isZoneGameOver() {
		return nil, nil
	}
	if hitPoints > 0 {
		return nil, nil
	}
	if livingIndex >= squad.Size {
		return nil, errors.New("living zone character missing")
	}
	s.squad.ResetDeployCooldown()
	err = s.deployZoneCharacter(
		livingIndex, time.UnixMilli(int64(timestamp)),
		standardCreatureSwapCooldown,
	)
	if err != nil {
		return nil, fmt.Errorf("livingSwitchDeploy: %w", err)
	}
	return transitionPackets, nil
}

// applyDamageHitPackets preserves the accepted hit publication before any
// automatic reserve deployment or terminal game-over transition. The combat
// continuation owns both mutations, so callers must not replace the killing
// CombatEvent/HP update with the squad transition packets.
func (s *gameplayPeerSession) applyDamageHitPackets(
	hitPackets [][]byte, hitPoints float32, timestamp uint64,
) ([][]byte, sporenet.PlayerStatDelta, error) {
	previousHitPoint := s.deployedHitPoint()
	transitionPackets, err := s.applyDamageHitPoints(hitPoints, timestamp)
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("damageTransition: %w", err)
	}
	statDelta := sporenet.PlayerStatDelta{
		PVEDamageTaken: float64(max(float32(0), previousHitPoint-hitPoints)),
	}
	if previousHitPoint > 0 && hitPoints <= 0 {
		statDelta.PVEDeath = 1
	}
	return append(hitPackets, transitionPackets...), statDelta, nil
}

func (s *gameplayPeerSession) deployZoneCharacter(
	index uint32, now time.Time, cooldown time.Duration,
) error {
	if s == nil || s.squad == nil {
		return errors.New("nil zone squad")
	}
	objectID := zonehero.ObjectID(s.binding.Slot, index)
	if objectID == 0 {
		return errors.New("zone character not found")
	}
	err := s.squad.DeployWithCooldown(index, now, cooldown)
	if err != nil {
		return fmt.Errorf("squadDeploy: %w", err)
	}
	s.deployedCreatureIndex = index
	s.deployedObjectID = objectID
	s.basicSequenceSession().ReleaseHeld()
	return nil
}

func (s *gameplayPeerSession) healLivingZoneSquad(amount float32) ([]zoneSquadHealing, error) {
	return s.healLivingZoneSquadScaled(amount, 0)
}

func (s *gameplayPeerSession) healLivingZoneCompanions(
	amount float32, position raknet.Vector3, radius float32,
) ([]zoneSquadHealing, error) {
	if s == nil || s.zone == nil || amount <= 0 {
		return nil, errors.New("invalid zone companion healing")
	}
	type pendingHealing struct {
		previous float32
		result   zoneSquadHealing
	}
	pending := make([]pendingHealing, 0)
	for _, companion := range s.zone.Companion().Snapshots() {
		if companion.UserID != s.binding.UserID ||
			!isInsideZoneTrigger(raknet.Vector3(companion.Position), position, radius) ||
			companion.PeerGeneration != s.generation ||
			!companion.IsTargetable || companion.HitPoint <= 0 ||
			companion.HitPoint >= companion.MaximumHitPoint {
			continue
		}
		hitPoint := min(companion.MaximumHitPoint, companion.HitPoint+amount)
		pending = append(pending, pendingHealing{
			previous: companion.HitPoint,
			result: zoneSquadHealing{
				objectID: companion.ObjectID, hitPoint: hitPoint,
				amount: hitPoint - companion.HitPoint, isCompanion: true,
			},
		})
	}
	healing := make([]zoneSquadHealing, 0, len(pending))
	for index, mutation := range pending {
		_, _, err := s.zone.Companion().SetHitPoint(
			mutation.result.objectID, mutation.result.hitPoint,
		)
		if err != nil {
			for rollbackIndex := index - 1; rollbackIndex >= 0; rollbackIndex-- {
				rollback := pending[rollbackIndex]
				_, _, rollbackErr := s.zone.Companion().SetHitPoint(
					rollback.result.objectID, rollback.previous,
				)
				if rollbackErr != nil {
					err = errors.Join(err, rollbackErr)
				}
			}
			return nil, fmt.Errorf(
				"healCompanion[%d]: %w", mutation.result.objectID, err,
			)
		}
		healing = append(healing, mutation.result)
	}
	return healing, nil
}

func (s *gameplayPeerSession) healLivingZoneSquadByMaximum(
	maximumFraction float32,
) ([]zoneSquadHealing, error) {
	if maximumFraction <= 0 || maximumFraction > 1 {
		return nil, errors.New("invalid zone squad healing fraction")
	}
	return s.healLivingZoneSquadScaled(0, maximumFraction)
}

func (s *gameplayPeerSession) healLivingZoneSquadScaled(
	amount float32, maximumFraction float32,
) ([]zoneSquadHealing, error) {
	if s == nil || s.squad == nil || (amount <= 0 && maximumFraction <= 0) {
		return nil, errors.New("invalid zone squad healing")
	}
	if s.squad.IsGameOver() {
		return nil, nil
	}
	type pendingHealing struct {
		index  uint32
		result zoneSquadHealing
	}
	pending := make([]pendingHealing, 0, squad.Size)
	for index := uint32(0); index < squad.Size; index++ {
		character, isFound := s.squad.Character(index)
		maximumHitPoint := s.characterHitPointMaximum(index)
		if !isFound || !character.IsAvailable || character.HitPoints <= 0 ||
			character.HitPoints >= maximumHitPoint {
			continue
		}
		if index >= uint32(len(s.binding.Creatures)) {
			return nil, fmt.Errorf("healProfile[%d]: missing", index)
		}
		restoreAmount := amount
		if maximumFraction > 0 {
			restoreAmount = maximumHitPoint * maximumFraction
		}
		receivedAmount, err := game.ApplyTargetHealingReduction(
			restoreAmount, s.binding.Creatures[index].HealingTargetProfile,
		)
		if err != nil {
			return nil, fmt.Errorf("healReduction[%d]: %w", index, err)
		}
		if receivedAmount == 0 {
			continue
		}
		hitPoint := min(maximumHitPoint, character.HitPoints+receivedAmount)
		objectID := zonehero.ObjectID(s.binding.Slot, index)
		if objectID == 0 {
			return nil, fmt.Errorf("healObject[%d]: missing", index)
		}
		pending = append(pending, pendingHealing{
			index: index, result: zoneSquadHealing{
				creatureIndex: index, objectID: objectID,
				hitPoint: hitPoint, amount: hitPoint - character.HitPoints,
			},
		})
	}
	healing := make([]zoneSquadHealing, 0, len(pending))
	for _, mutation := range pending {
		_, err := s.squad.SetHitPoints(mutation.index, mutation.result.hitPoint)
		if err != nil {
			s.rollbackZoneSquadHealing(healing)
			return nil, fmt.Errorf("healCharacter[%d]: %w", mutation.index, err)
		}
		healing = append(healing, mutation.result)
	}
	err := s.syncZoneHero()
	if err != nil {
		s.rollbackZoneSquadHealing(healing)
		return nil, fmt.Errorf("healHeroSync: %w", err)
	}
	err = s.syncZoneSquadCheckpoint()
	if err != nil {
		s.rollbackZoneSquadHealing(healing)
		return nil, fmt.Errorf("healCheckpoint: %w", err)
	}
	return healing, nil
}

func (s *gameplayPeerSession) restoreLivingZoneSquadManaByMaximum(
	maximumFraction float32,
) ([]zoneSquadManaRestoration, error) {
	if s == nil || s.squad == nil || maximumFraction <= 0 || maximumFraction > 1 {
		return nil, errors.New("invalid zone squad mana restoration")
	}
	if s.squad.IsGameOver() {
		return nil, nil
	}
	restorations := make([]zoneSquadManaRestoration, 0, squad.Size)
	for index := uint32(0); index < squad.Size; index++ {
		character, isFound := s.squad.Character(index)
		_, maximumManaPoint := s.characterResourceMaximum(index)
		if !isFound || !character.IsAvailable || character.HitPoints <= 0 ||
			character.ManaPoints >= maximumManaPoint {
			continue
		}
		manaPoint := min(
			maximumManaPoint,
			character.ManaPoints+maximumManaPoint*maximumFraction,
		)
		objectID := zonehero.ObjectID(s.binding.Slot, index)
		if objectID == 0 {
			return nil, fmt.Errorf("manaObject[%d]: missing", index)
		}
		err := s.squad.SetManaPoints(index, manaPoint)
		if err != nil {
			s.rollbackZoneSquadManaRestoration(restorations)
			return nil, fmt.Errorf("manaCharacter[%d]: %w", index, err)
		}
		restorations = append(restorations, zoneSquadManaRestoration{
			creatureIndex: index, objectID: objectID,
			manaPoint: manaPoint, amount: manaPoint - character.ManaPoints,
		})
	}
	err := s.syncZoneHero()
	if err != nil {
		s.rollbackZoneSquadManaRestoration(restorations)
		return nil, fmt.Errorf("manaHeroSync: %w", err)
	}
	err = s.syncZoneSquadCheckpoint()
	if err != nil {
		s.rollbackZoneSquadManaRestoration(restorations)
		return nil, fmt.Errorf("manaCheckpoint: %w", err)
	}
	return restorations, nil
}

type gameplaySwitchRuntime struct {
	registry      *gameplaySessionRegistry
	program       Programs
	npc           campaignNPCActionRuntime
	damage        campaignDamageRuntime
	modifierPool  *modifierPool
	effectPool    *attachedEffectPool
	passive       fireTempestPassiveRuntime
	energyPassive energySentinelPassiveRuntime
	now           func() time.Time
	logger        *log.Logger
}

type gameplayDeathSelectionStep struct {
	runtime    gameplaySwitchRuntime
	packet     raknet.Packet
	command    raknet.ActionCommandData
	sessionKey string
	generation uint64
}

func (e gameplayDeathSelectionStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.isHeroSelectionPending &&
		peerSession.isHeroSelectionScheduled &&
		peerSession.deployedHitPoint() <= 0
	if isCurrent {
		peerSession.isHeroSelectionScheduled = false
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return nil, nil
	}
	packets, err := e.runtime.handle(e.packet, e.command, peerSession, true)
	if err != nil {
		return nil, fmt.Errorf("deathSelectionHandle: %w", err)
	}
	return packets, nil
}

type gameplaySwitchArrivalStep struct {
	runtime        gameplaySwitchRuntime
	packet         raknet.Packet
	packets        [][]byte
	sessionKey     string
	generation     uint64
	targetObjectID uint32
	timestamp      uint64
	isKnockback    bool
}

func (s gameplaySwitchArrivalStep) produce() ([][]byte, error) {
	isCurrent := true
	if s.runtime.registry != nil {
		s.runtime.registry.mutex.Lock()
		peerSession, isFound := s.runtime.registry.sessions[s.sessionKey]
		isCurrent = isFound && peerSession.generation == s.generation &&
			peerSession.deployedObjectID == s.targetObjectID
		if isCurrent {
			peerSession.heroInputLockedObjectID = 0
			peerSession.heroInputLockedUntil = time.Time{}
			s.runtime.registry.sessions[s.sessionKey] = peerSession
		}
		s.runtime.registry.mutex.Unlock()
	}
	if !isCurrent {
		return nil, nil
	}
	packets := clonePendingPackets(s.packets)
	if !s.isKnockback {
		return packets, nil
	}
	knockbackPackets, err := s.runtime.heroSwapArrivalKnockback(
		s.packet, s.sessionKey, s.generation, s.targetObjectID, s.timestamp,
	)
	if err != nil {
		if s.runtime.logger != nil {
			s.runtime.logger.Printf(
				"RakNet hero swap arrival knockback omitted target=%d: %v",
				s.targetObjectID, err,
			)
		}
		return packets, nil
	}
	return append(packets, knockbackPackets...), nil
}

func (r gameplaySwitchRuntime) recoverPassiveSchedule(
	sessionKey string, generation uint64, objectID uint32,
	run *summonPassiveRun, cause error,
) {
	_, stopErr := run.Stop()
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.deployedObjectID == objectID && peerSession.sagePassive == run
	if isCurrent {
		peerSession.sagePassive = nil
		peerSession.sagePassiveActivations = nil
		r.registry.sessions[sessionKey] = peerSession
	}
	r.registry.mutex.Unlock()
	r.logger.Printf(
		"RakNet campaign Sage passive rolled back after switch remote=%s: %v",
		sessionKey, errors.Join(cause, stopErr),
	)
}

type gameplaySwitchPassiveSchedule struct {
	runtime             gameplaySwitchRuntime
	packet              raknet.Packet
	sessionKey          string
	sessionGeneration   uint64
	targetObjectID      uint32
	targetCreatureIndex uint32
	run                 *summonPassiveRun
	isCampaign          bool
}

func (s gameplaySwitchPassiveSchedule) start(packet raknet.Packet) error {
	if packet.ScheduleGroup == nil && packet.ScheduleGroupResult == nil {
		return fmt.Errorf("%s: %w", s.errorTag(), errors.New("unavailable"))
	}
	s.packet = packet
	producer := raknet.ScheduledPacketProducer{
		Delay:   s.runtime.program.SupportHealerPassive.SpawnDelay,
		Produce: s.produce,
	}
	producers := s.runtime.registry.producerGuard.scheduledProducers(
		s.sessionKey, []raknet.ScheduledPacketProducer{producer},
	)
	var cancel raknet.CancelSchedule
	var err error
	if packet.ScheduleGroupResult != nil {
		cancel, err = packet.ScheduleGroupResult(
			producers, s.fail,
		)
	} else {
		cancel, err = packet.ScheduleGroup(producers)
	}
	if err != nil {
		s.fail(err)
		return fmt.Errorf("%s: %w", s.errorTag(), err)
	}
	s.run.AddCancel(cancel)
	return nil
}

func (s gameplaySwitchPassiveSchedule) produce() ([][]byte, error) {
	s.runtime.registry.mutex.Lock()
	peerSession, isFound := s.runtime.registry.sessions[s.sessionKey]
	isCurrent := isFound && peerSession.generation == s.sessionGeneration &&
		peerSession.sagePassive == s.run
	if s.isCampaign {
		isCurrent = isCurrent && peerSession.deployedObjectID == s.targetObjectID
	} else {
		isCurrent = isCurrent &&
			peerSession.deployedCreatureIndex == s.targetCreatureIndex
	}
	if !isCurrent {
		s.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	requests, err := s.run.Advance(
		s.runtime.program.SupportHealerPassive.SpawnDelay,
	)
	if err != nil {
		s.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("%s: %w", s.advanceTag(), err)
	}
	packets := make([][]byte, 0, len(requests)*2)
	activation := make(map[uint32]summonCompanionActivation, len(requests))
	for index, request := range requests {
		position, placementErr := summonPassiveSpawnPosition(
			peerSession.playerPosition, index, len(requests),
			request.Spawn.SpawnRadius,
		)
		if placementErr != nil {
			s.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("%s[%d]: %w", s.positionTag(), index, placementErr)
		}
		spawnPackets, companionActivation, marshalErr :=
			marshalSummonPassiveWorldRequest(request, position)
		if marshalErr != nil {
			s.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("%s[%d]: %w", s.spawnTag(), index, marshalErr)
		}
		packets = append(packets, spawnPackets...)
		if companionActivation != nil {
			activation[request.ObjectID] = *companionActivation
		}
	}
	if s.isCampaign {
		hitPoint := s.runtime.program.NonPlayerHitPoint[util.HashID("HelperMelee")]
		hitPoint = peerSession.petHitPoint(hitPoint)
		footprintRadius, footprintErr :=
			s.runtime.program.FootprintRadius("HelperMelee.Noun")
		if hitPoint <= 0 || footprintErr != nil || footprintRadius <= 0 {
			s.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("%s: companion profile unavailable", s.spawnTag())
		}
		for _, companion := range activation {
			err = peerSession.zone.Companion().Put(zonecompanion.Actor{
				UserID:         peerSession.binding.UserID,
				PeerGeneration: peerSession.generation,
				ObjectID:       companion.ObjectID, OwnerObjectID: companion.OwnerObjectID,
				Noun: util.HashID("HelperMelee.Noun"),
				Position: game.Vec3{
					X: companion.Position.X, Y: companion.Position.Y,
					Z: companion.Position.Z,
				},
				FootprintRadius: footprintRadius,
				HitPoint:        hitPoint, MaximumHitPoint: hitPoint,
				IsTargetable: true, IsCombatant: true,
			})
			if err != nil {
				for objectID := range activation {
					peerSession.zone.Companion().Remove(objectID)
				}
				s.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("companionPut: %w", err)
			}
		}
	}
	peerSession.sagePassiveActivations = activation
	campaignZone := peerSession.zone
	s.runtime.registry.sessions[s.sessionKey] = peerSession
	s.runtime.registry.mutex.Unlock()
	if s.isCampaign {
		acquired, acquireErr := campaignZone.RefreshNPCTargets()
		if acquireErr != nil {
			return nil, fmt.Errorf("companionTargetAcquire: %w", acquireErr)
		}
		plans := make([]zonenpc.SpawnPlan, 0, len(acquired))
		for _, npc := range acquired {
			plans = append(plans, npc.Plan)
		}
		actionPackets, actionErr := s.runtime.npc.scheduleFirstActions(
			s.packet, s.sessionKey, s.sessionGeneration, plans,
			s.packet.SourceTime+uint64(
				s.runtime.program.SupportHealerPassive.SpawnDelay/time.Millisecond,
			),
		)
		if actionErr != nil {
			return nil, fmt.Errorf("companionTargetSchedule: %w", actionErr)
		}
		packets = append(packets, actionPackets...)
		companionPackets, companionErr := s.runtime.damage.startCompanionAttacks(
			s.packet, s.sessionKey, s.sessionGeneration,
			s.packet.SourceTime+uint64(
				s.runtime.program.SupportHealerPassive.SpawnDelay/time.Millisecond,
			),
		)
		if companionErr != nil {
			return nil, fmt.Errorf("companionAttackSchedule: %w", companionErr)
		}
		packets = append(packets, companionPackets...)
		s.runtime.logger.Printf(
			"RakNet campaign Sage passive spawn sent to %s owner=%d companions=%d autonomous_packets=%d",
			s.sessionKey, s.targetObjectID, len(activation), len(companionPackets),
		)
	}
	return packets, nil
}

func (s gameplaySwitchPassiveSchedule) fail(scheduleErr error) {
	s.runtime.registry.mutex.Lock()
	peerSession, isFound := s.runtime.registry.sessions[s.sessionKey]
	isCurrent := isFound && peerSession.generation == s.sessionGeneration &&
		peerSession.sagePassive == s.run
	if isCurrent {
		if !s.isCampaign {
			stopSummonCompanionAttacks(peerSession.sagePassiveActivations)
		} else {
			peerSession.removeCampaignCompanions()
		}
		peerSession.sagePassive = nil
		peerSession.sagePassiveActivations = nil
		s.runtime.registry.sessions[s.sessionKey] = peerSession
	}
	s.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return
	}
	_, _ = s.run.Stop()
	s.runtime.logger.Printf(
		"RakNet Sage passive stopped after schedule failure for %s: %v",
		s.sessionKey, scheduleErr,
	)
}

func (s gameplaySwitchPassiveSchedule) errorTag() string {
	if s.isCampaign {
		return "switchCampaignPassiveSchedule"
	}
	return "switchPassiveSchedule"
}

func (s gameplaySwitchPassiveSchedule) advanceTag() string {
	if s.isCampaign {
		return "switchCampaignPassiveAdvance"
	}
	return "switchPassiveAdvance"
}

func (s gameplaySwitchPassiveSchedule) positionTag() string {
	if s.isCampaign {
		return "switchCampaignPassivePosition"
	}
	return "switchPassivePosition"
}

func (s gameplaySwitchPassiveSchedule) spawnTag() string {
	if s.isCampaign {
		return "switchCampaignPassiveSpawn"
	}
	return "switchPassiveSpawn"
}

func (r gameplaySwitchRuntime) handle(
	packet raknet.Packet, command raknet.ActionCommandData,
	commandSession gameplayPeerSession, isSessionFound bool,
) ([][]byte, error) {
	switchStartTime := r.now()
	if isSessionFound && commandSession.binding.Mode == game.ModeArena {
		return r.handleArenaSwitch(
			packet, command, commandSession, switchStartTime,
		)
	}
	if !isSessionFound || commandSession.squad == nil ||
		commandSession.deployedCreatureIndex >=
			uint32(len(commandSession.binding.Creatures)) {
		return r.reject(command, commandSession, switchStartTime, "session unavailable")
	}
	targetObjectID := zonehero.ObjectID(commandSession.binding.Slot, command.Value)
	sourceObjectID := commandSession.deployedObjectID
	isDeathSelection := commandSession.isHeroSelectionPending &&
		commandSession.deployedHitPoint() <= 0
	deathSelectionDelay := time.Duration(0)
	if isDeathSelection && switchStartTime.Before(commandSession.heroSelectionReadyAt) {
		deathSelectionDelay = commandSession.heroSelectionReadyAt.Sub(switchStartTime)
	}
	switchPresentationTime := switchStartTime.Add(deathSelectionDelay)
	isVoluntarySwitch := command.Common.ObjectID == sourceObjectID &&
		commandSession.squad.IsDeployReady(switchStartTime)
	if command.Value >= uint32(len(commandSession.binding.Creatures)) ||
		commandSession.binding.Creatures[command.Value].Noun == 0 ||
		targetObjectID == commandSession.deployedObjectID ||
		(!isDeathSelection && !isVoluntarySwitch) {
		return r.reject(command, commandSession, switchStartTime, "admission unavailable")
	}
	targetCharacter, isCharacterFound :=
		commandSession.squad.Character(command.Value)
	if !isCharacterFound || !targetCharacter.IsAvailable ||
		targetCharacter.HitPoints <= 0 {
		return r.reject(command, commandSession, switchStartTime, "target unavailable")
	}
	sourceCreature :=
		commandSession.binding.Creatures[commandSession.deployedCreatureIndex]
	targetCreature := commandSession.binding.Creatures[command.Value]
	sourceCharacter, isSourceFound :=
		commandSession.squad.Character(commandSession.deployedCreatureIndex)
	if !isSourceFound {
		return r.reject(command, commandSession, switchStartTime, "source unavailable")
	}
	if deathSelectionDelay > 0 {
		sessionKey := packet.Address.String()
		r.registry.mutex.Lock()
		peerSession, isCurrentFound := r.registry.sessions[sessionKey]
		isCurrent := isCurrentFound &&
			peerSession.generation == commandSession.generation &&
			peerSession.isHeroSelectionPending &&
			peerSession.deployedHitPoint() <= 0
		isAlreadyScheduled := isCurrent && peerSession.isHeroSelectionScheduled
		if isCurrent && !isAlreadyScheduled {
			peerSession.isHeroSelectionScheduled = true
			r.registry.sessions[sessionKey] = peerSession
		}
		r.registry.mutex.Unlock()
		if !isCurrent {
			return r.reject(command, commandSession, switchStartTime, "session replaced")
		}
		if isAlreadyScheduled {
			return nil, nil
		}
		deferredPacket := packet
		deferredPacket.SourceTime += uint64(deathSelectionDelay / time.Millisecond)
		step := gameplayDeathSelectionStep{
			runtime: r, packet: deferredPacket, command: command,
			sessionKey: sessionKey, generation: commandSession.generation,
		}
		producer := raknet.ScheduledPacketProducer{
			Delay: deathSelectionDelay, Produce: step.produce,
		}
		producers := r.registry.producerGuard.scheduledProducers(
			sessionKey, []raknet.ScheduledPacketProducer{producer},
		)
		cancel, scheduleErr := packet.Autonomous().ScheduleProducers(producers)
		if scheduleErr == nil && cancel == nil {
			scheduleErr = errors.New("death selection cancellation unavailable")
		}
		if scheduleErr == nil {
			r.logger.Printf(
				"RakNet campaign death selection scheduled source=%d target=%d delay_ms=%d",
				sourceObjectID, targetObjectID, deathSelectionDelay.Milliseconds(),
			)
			return nil, nil
		}
		r.registry.mutex.Lock()
		peerSession, isCurrentFound = r.registry.sessions[sessionKey]
		if isCurrentFound && peerSession.generation == commandSession.generation {
			peerSession.isHeroSelectionScheduled = false
			r.registry.sessions[sessionKey] = peerSession
		}
		r.registry.mutex.Unlock()
		r.logger.Printf(
			"RakNet campaign death selection schedule failed source=%d target=%d: %v",
			sourceObjectID, targetObjectID, scheduleErr,
		)
		deathSelectionDelay = 0
		switchPresentationTime = switchStartTime
	}
	isTargetSage := zonehero.IsBasicAbility(
		r.program.PlayerBasicAbility, targetCreature.Noun, "SupportHealerBasic",
	)
	isTargetFieldMedic := targetCreature.PassiveAbility ==
		util.HashID("FieldMedicPassive")
	var switchPackets [][]byte
	var departurePackets [][]byte
	var err error
	var startedPassive *summonPassiveRun
	var interruptedBasic *abilityraknet.MeleeRun
	var stoppedGhostRun *abilityraknet.GhostFormRun
	var stoppedHeroModifierRun *heroModifierRun
	stoppedPassiveRequest := []summonPassiveWorldRequest(nil)
	stoppedWreathPackets := make([][]byte, 0)
	stoppedGhostPackets := make([][]byte, 0)
	stoppedHeroModifierPackets := make([][]byte, 0)
	stoppedFlakPackets := make([][]byte, 0)
	stoppedPoisonNovaPackets := make([][]byte, 0)
	fieldMedicPackets := make([][]byte, 0)
	beastPetPackets := make([][]byte, 0)
	heroSummonPackets := make([][]byte, 0)
	trapperStealthPackets := make([][]byte, 0)
	restartedEnemyPlans := make([]zonenpc.SpawnPlan, 0)
	enemyTargetPackets := make([][]byte, 0)
	enemyCancelPackets := make([][]byte, 0)

	r.registry.mutex.Lock()
	peerSession, isCurrentFound :=
		r.registry.sessions[packet.Address.String()]
	isCurrent := isCurrentFound &&
		peerSession.generation == commandSession.generation &&
		peerSession.squad != nil &&
		((peerSession.isHeroSelectionPending &&
			peerSession.deployedHitPoint() <= 0) ||
			(peerSession.deployedObjectID == command.Common.ObjectID &&
				peerSession.squad.IsDeployReady(switchStartTime)))
	if !isCurrent {
		r.registry.mutex.Unlock()
		return r.reject(command, commandSession, switchStartTime, "session replaced")
	}
	if peerSession.zone == nil || peerSession.zone.NPCs() == nil {
		r.registry.mutex.Unlock()
		return nil, errors.New("switchCampaignNPCSession: unavailable")
	}
	targetCharacter, err = peerSession.normalizeCampaignCharacterResources(
		command.Value,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("switchCampaignResources: %w", err)
	}
	stoppedFieldMedicPackets, fieldMedicErr := peerSession.stopFieldMedicDrone()
	if fieldMedicErr == nil {
		fieldMedicPackets = append(fieldMedicPackets, stoppedFieldMedicPackets...)
	} else {
		r.logger.Printf("RakNet Field Medic cleanup omitted during switch remote=%s: %v", packet.Address, fieldMedicErr)
	}
	stoppedBeastPackets, beastErr := peerSession.stopBeastPet()
	if beastErr == nil {
		beastPetPackets = append(beastPetPackets, stoppedBeastPackets...)
	} else {
		r.logger.Printf("RakNet beast cleanup omitted during switch remote=%s: %v", packet.Address, beastErr)
	}
	stoppedSummonPackets, summonErr := peerSession.stopHeroSummons()
	if summonErr == nil {
		heroSummonPackets = append(heroSummonPackets, stoppedSummonPackets...)
	} else {
		r.logger.Printf("RakNet summon cleanup omitted during switch remote=%s: %v", packet.Address, summonErr)
	}
	stoppedFireTempestPackets, fireTempestErr := peerSession.stopFireTempestActive()
	if fireTempestErr == nil {
		heroSummonPackets = append(heroSummonPackets, stoppedFireTempestPackets...)
	} else {
		r.logger.Printf("RakNet Fire Tempest cleanup omitted during switch remote=%s: %v", packet.Address, fireTempestErr)
	}
	stoppedPlasmaPackets, plasmaErr := peerSession.stopPlasmaSentinelActive()
	if plasmaErr == nil {
		heroSummonPackets = append(heroSummonPackets, stoppedPlasmaPackets...)
	} else {
		r.logger.Printf("RakNet Plasma Sentinel cleanup omitted during switch remote=%s: %v", packet.Address, plasmaErr)
	}
	stoppedStealthPackets, stealthErr := peerSession.stopTrapperStealth()
	if stealthErr == nil {
		trapperStealthPackets = append(
			trapperStealthPackets, stoppedStealthPackets...,
		)
	} else {
		r.logger.Printf("RakNet Trapper stealth cleanup omitted during switch remote=%s: %v", packet.Address, stealthErr)
	}
	if isTargetSage && peerSession.sagePassive == nil {
		companionCount := int(r.program.SupportHealerPassive.MaximumCompanion)
		if companionCount <= 0 {
			r.logger.Printf("RakNet Sage passive unavailable during switch remote=%s: no companions", packet.Address)
		} else {
			firstObjectID, objectIDErr := peerSession.reserveCampaignProjectileIDs(
				uint32(companionCount), 2000,
			)
			if objectIDErr != nil {
				r.logger.Printf("RakNet Sage passive object reservation omitted during switch remote=%s: %v", packet.Address, objectIDErr)
			} else {
				objectIDs := make([]uint32, companionCount)
				for index := range objectIDs {
					objectIDs[index] = firstObjectID + uint32(index)
				}
				startedPassive, err = newSummonPassiveRun(summonPassiveRunInput{
					Definition:    r.program.SupportHealerPassive,
					OwnerObjectID: targetObjectID, CompanionObjectIDs: objectIDs,
				})
				if err != nil {
					r.logger.Printf("RakNet Sage passive start omitted during switch remote=%s: %v", packet.Address, err)
					startedPassive = nil
				}
			}
		}
	}
	err = peerSession.stopPlayerMovement(switchStartTime)
	if err != nil {
		r.logger.Printf("RakNet player movement cleanup omitted during switch remote=%s: %v", packet.Address, err)
	}
	switchPackets, err = marshalCampaignCharacterSwitch(
		uint8(peerSession.binding.Slot), sourceObjectID, targetObjectID,
		command.Value, sourceCreature, targetCreature,
		sourceCharacter.HitPoints, sourceCharacter.ManaPoints,
		targetCharacter.HitPoints, targetCharacter.ManaPoints,
		peerSession.playerPosition, command.Common.Orientation,
		uint64(switchPresentationTime.UnixMilli()),
		isDeathSelection || isVoluntarySwitch,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("switchCampaign: %w", err)
	}
	if isVoluntarySwitch {
		departurePackets, err = marshalCampaignCharacterDeparture(
			sourceObjectID, sourceCreature, peerSession.playerPosition,
			uint64(switchStartTime.UnixMilli()),
		)
		if err != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("switchDeparture: %w", err)
		}
	}
	previousCreatureIndex := peerSession.deployedCreatureIndex
	peerSession.resetPassiveDamageReduction(previousCreatureIndex)
	peerSession.resetFireRavagerBasic(previousCreatureIndex)
	shieldPackets, shieldErr := peerSession.stopTCShield(
		previousCreatureIndex, r.effectPool,
	)
	if shieldErr != nil {
		r.logger.Printf("RakNet shield cleanup omitted during switch remote=%s: %v", packet.Address, shieldErr)
		shieldPackets = nil
	}
	peerSession.resetTCShield(previousCreatureIndex)
	interruptedBasic = peerSession.resetAbilityAdmissionForSwitch()
	if peerSession.ghostFormRun != nil {
		stoppedGhostRun = peerSession.ghostFormRun
		stoppedGhostPackets, err = stoppedGhostRun.ExpiryPackets(
			uint64(switchStartTime.UnixMilli()),
		)
		if err != nil {
			r.logger.Printf("RakNet Ghost Form cleanup omitted during switch remote=%s: %v", packet.Address, err)
			stoppedGhostPackets = nil
		}
		peerSession.ghostFormRun = nil
	}
	if peerSession.heroModifierRun != nil {
		stoppedHeroModifierRun = peerSession.heroModifierRun
		stoppedHeroModifierRun.Remove(&peerSession)
		if stoppedHeroModifierRun.isShadowStealth {
			acquired, stealthPacket, stealthErr := peerSession.setShadowRavagerStealth(
				stoppedHeroModifierRun.objectID, false,
			)
			_ = acquired
			if stealthErr == nil {
				stoppedHeroModifierPackets = append(
					stoppedHeroModifierPackets, stealthPacket,
				)
			} else {
				r.logger.Printf("RakNet Shadow Cloak cleanup omitted during switch remote=%s: %v", packet.Address, stealthErr)
			}
		}
		modifierDeletePacket, modifierErr := effectraknet.ModifierDelete(
			stoppedHeroModifierRun.objectID,
			stoppedHeroModifierRun.instanceID,
		)
		if modifierErr == nil {
			stoppedHeroModifierPackets = append(
				stoppedHeroModifierPackets, modifierDeletePacket,
			)
		} else {
			r.logger.Printf("RakNet hero modifier cleanup omitted during switch remote=%s: %v", packet.Address, modifierErr)
		}
		effectPacket, effectErr := stoppedHeroModifierRun.stopEffect(r.effectPool)
		if effectErr == nil && effectPacket != nil {
			stoppedHeroModifierPackets = append(
				stoppedHeroModifierPackets, effectPacket,
			)
		} else if effectErr != nil {
			r.logger.Printf("RakNet hero modifier effect cleanup omitted during switch remote=%s: %v", packet.Address, effectErr)
		}
		if stoppedHeroModifierRun.isArborealMight {
			attributePacket, attributeErr := raknet.MarshalApplication(
				raknet.AttributeDataUpdateMessage{
					ObjectID: stoppedHeroModifierRun.objectID,
					Value:    map[uint8]float32{113: 0},
				},
			)
			if attributeErr == nil {
				stoppedHeroModifierPackets = append(
					stoppedHeroModifierPackets, attributePacket,
				)
			} else {
				r.logger.Printf("RakNet hero scale cleanup omitted during switch remote=%s: %v", packet.Address, attributeErr)
			}
		}
		peerSession.heroModifierRun = nil
	}
	flakPacket, flakErr := stopMissileFlak(&peerSession, r.modifierPool)
	if flakErr == nil && flakPacket != nil {
		stoppedFlakPackets = append(stoppedFlakPackets, flakPacket)
	} else if flakErr != nil {
		r.logger.Printf("RakNet Missile Flak cleanup omitted during switch remote=%s: %v", packet.Address, flakErr)
	}
	poisonNovaPacket, poisonNovaErr := stopPoisonNovaCooldown(
		&peerSession, r.modifierPool,
	)
	if poisonNovaErr == nil && poisonNovaPacket != nil {
		stoppedPoisonNovaPackets = append(
			stoppedPoisonNovaPackets, poisonNovaPacket,
		)
	} else if poisonNovaErr != nil {
		r.logger.Printf("RakNet Poison Nova cleanup omitted during switch remote=%s: %v", packet.Address, poisonNovaErr)
	}
	if peerSession.plasmaWreathRun != nil {
		stoppedWreathPackets, err = peerSession.plasmaWreathRun.Stop()
		if err != nil {
			r.logger.Printf("RakNet Plasma Wreath cleanup omitted during switch remote=%s: %v", packet.Address, err)
			stoppedWreathPackets = nil
		}
		peerSession.plasmaWreathRun = nil
	}
	if !isTargetSage && peerSession.sagePassive != nil {
		stoppedPassiveRequest, err = peerSession.sagePassive.Stop()
		if err != nil {
			r.logger.Printf("RakNet Sage passive cleanup omitted during switch remote=%s: %v", packet.Address, err)
			stoppedPassiveRequest = nil
		}
		stopSummonCompanionAttacks(peerSession.sagePassiveActivations)
		peerSession.removeCampaignCompanions()
		peerSession.sagePassive = nil
		peerSession.sagePassiveActivations = nil
	}
	err = peerSession.squad.DeployWithCooldown(
		command.Value, switchPresentationTime, standardCreatureSwapCooldown,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("switchCampaignSquad: %w", err)
	}
	peerSession.clearEnemyHeroStatuses()
	peerSession.deployedCreatureIndex = command.Value
	peerSession.passiveStationarySince[command.Value] = switchPresentationTime
	peerSession.startTCShieldRecharge(command.Value, switchPresentationTime)
	peerSession.deployedObjectID = targetObjectID
	if isVoluntarySwitch {
		peerSession.heroInputLockedObjectID = sourceObjectID
		peerSession.heroInputLockedUntil = switchStartTime.Add(
			campaignCreatureWarpOutDelay,
		)
	}
	peerSession.isHeroSelectionPending = false
	peerSession.isHeroSelectionScheduled = false
	peerSession.heroSelectionReadyAt = time.Time{}
	if startedPassive != nil {
		peerSession.sagePassive = startedPassive
	}
	if isTargetFieldMedic {
		firstObjectID, objectIDErr := peerSession.reserveCampaignProjectileIDs(1, 2000)
		if objectIDErr != nil {
			r.logger.Printf(
				"RakNet Field Medic drone object reservation skipped for %s: %v",
				packet.Address, objectIDErr,
			)
		} else {
			spawnPackets, spawnErr := peerSession.spawnFieldMedicDrone(
				r.program, firstObjectID,
			)
			if spawnErr != nil {
				r.logger.Printf(
					"RakNet Field Medic drone spawn skipped for %s: %v",
					packet.Address, spawnErr,
				)
			} else {
				fieldMedicPackets = append(fieldMedicPackets, spawnPackets...)
			}
		}
	}
	commandSession.playerPosition = peerSession.playerPosition
	err = peerSession.syncZoneHero()
	if err != nil {
		r.logger.Printf(
			"RakNet campaign hero zone sync omitted after switch remote=%s: %v",
			packet.Address, err,
		)
	} else {
		restartedEnemy, canceledEnemy, retargetErr := peerSession.zone.SwitchHeroTarget(
			peerSession.binding.UserID, peerSession.generation,
			sourceObjectID, targetObjectID, packet.SourceTime,
		)
		if retargetErr != nil {
			r.logger.Printf(
				"RakNet campaign NPC retarget omitted after switch remote=%s: %v",
				packet.Address, retargetErr,
			)
		} else {
			enemyCancelPackets, err = npcraknet.CancelActions(
				canceledEnemy, packet.SourceTime,
			)
			if err != nil {
				r.logger.Printf(
					"RakNet campaign NPC cancellation presentation omitted after switch remote=%s: %v",
					packet.Address, err,
				)
				enemyCancelPackets = nil
			}
			for _, npc := range restartedEnemy {
				restartedEnemyPlans = append(restartedEnemyPlans, npc.Plan)
			}
		}
	}
	checkpointErr := peerSession.syncZoneSquadCheckpoint()
	if checkpointErr != nil {
		r.logger.Printf(
			"RakNet campaign squad checkpoint sync omitted after switch remote=%s: %v",
			packet.Address, checkpointErr,
		)
	}
	enemyTargetPackets, err = npcraknet.TargetUpdates(
		peerSession.zone.NPCs().Snapshots(),
	)
	if err != nil {
		r.logger.Printf(
			"RakNet campaign NPC target presentation omitted after switch remote=%s: %v",
			packet.Address, err,
		)
		enemyTargetPackets = nil
	}
	r.registry.sessions[packet.Address.String()] = peerSession
	r.registry.mutex.Unlock()
	if interruptedBasic != nil {
		interruptedBasic.Stop()
	}
	if stoppedGhostRun != nil {
		stoppedGhostRun.Cancel()
		_ = r.modifierPool.Release(stoppedGhostRun.InstanceID())
	}
	if stoppedHeroModifierRun != nil {
		stoppedHeroModifierRun.Cancel()
		_ = r.modifierPool.Release(stoppedHeroModifierRun.instanceID)
	}
	switchPackets = append(switchPackets, enemyCancelPackets...)
	switchPackets = append(switchPackets, enemyTargetPackets...)
	switchPackets = append(switchPackets, stoppedWreathPackets...)
	switchPackets = append(switchPackets, stoppedGhostPackets...)
	switchPackets = append(switchPackets, stoppedHeroModifierPackets...)
	switchPackets = append(switchPackets, stoppedFlakPackets...)
	switchPackets = append(switchPackets, stoppedPoisonNovaPackets...)
	switchPackets = append(switchPackets, fieldMedicPackets...)
	switchPackets = append(switchPackets, beastPetPackets...)
	switchPackets = append(switchPackets, heroSummonPackets...)
	switchPackets = append(switchPackets, trapperStealthPackets...)
	switchPackets = append(switchPackets, shieldPackets...)
	for index, req := range stoppedPassiveRequest {
		passivePackets, _, passiveErr := marshalSummonPassiveWorldRequest(
			req, raknet.Vector3{},
		)
		if passiveErr != nil {
			r.logger.Printf(
				"RakNet campaign passive cleanup omitted remote=%s index=%d: %v",
				packet.Address, index, passiveErr,
			)
			continue
		}
		switchPackets = append(switchPackets, passivePackets...)
	}
	if startedPassive != nil {
		schedule := gameplaySwitchPassiveSchedule{
			runtime: r, sessionKey: packet.Address.String(),
			sessionGeneration: commandSession.generation,
			targetObjectID:    targetObjectID, run: startedPassive, isCampaign: true,
		}
		err = schedule.start(packet)
		if err != nil {
			r.recoverPassiveSchedule(
				packet.Address.String(), commandSession.generation,
				targetObjectID, startedPassive, err,
			)
		} else {
			r.logger.Printf(
				"RakNet campaign Sage passive scheduled for %s owner=%d delay=%s",
				packet.Address, targetObjectID,
				r.program.SupportHealerPassive.SpawnDelay,
			)
		}
	}
	firePassivePacket, err := r.passive.start(
		packet, packet.Address.String(), commandSession.generation,
	)
	if err != nil {
		r.logger.Printf(
			"RakNet campaign fire passive omitted after switch remote=%s: %v",
			packet.Address, err,
		)
	}
	if firePassivePacket != nil {
		switchPackets = append(switchPackets, firePassivePacket)
	}
	err = r.energyPassive.start(
		packet, packet.Address.String(), commandSession.generation,
	)
	if err != nil {
		r.logger.Printf(
			"RakNet campaign energy passive omitted after switch remote=%s: %v",
			packet.Address, err,
		)
	}
	err = startTrapperStealth(
		r.damage, packet, packet.Address.String(),
		commandSession.generation,
	)
	if err != nil {
		r.logger.Printf(
			"RakNet campaign Trapper stealth omitted after switch remote=%s: %v",
			packet.Address, err,
		)
	}
	lightspeedPackets, err := startLightspeedPassive(
		r.damage, packet, packet.Address.String(), commandSession.generation,
	)
	if err != nil {
		r.logger.Printf(
			"RakNet campaign Lightspeed passive omitted after switch remote=%s: %v",
			packet.Address, err,
		)
		lightspeedPackets = nil
	}
	switchPackets = append(switchPackets, lightspeedPackets...)
	restartedEnemyPackets, restartErr := r.npc.scheduleFirstActions(
		packet, packet.Address.String(), commandSession.generation,
		restartedEnemyPlans, packet.SourceTime,
	)
	if restartErr != nil {
		r.logger.Printf(
			"RakNet campaign NPC restart omitted after switch remote=%s: %v",
			packet.Address, restartErr,
		)
		restartedEnemyPackets = nil
	}
	switchPackets = append(switchPackets, restartedEnemyPackets...)
	droneAttackPackets, droneAttackErr := r.damage.startCompanionAttacks(
		packet, packet.Address.String(), commandSession.generation,
		packet.SourceTime,
	)
	if droneAttackErr != nil {
		r.logger.Printf(
			"RakNet campaign companion attack omitted after switch remote=%s: %v",
			packet.Address, droneAttackErr,
		)
		droneAttackPackets = nil
	}
	switchPackets = append(switchPackets, droneAttackPackets...)
	r.logger.Printf(
		"RakNet campaign character switch accepted source=%d creature=%d target=%d position=(%.3f,%.3f,%.3f)",
		sourceObjectID, command.Value, targetObjectID,
		peerSession.playerPosition.X, peerSession.playerPosition.Y,
		peerSession.playerPosition.Z,
	)
	if isVoluntarySwitch {
		step := gameplaySwitchArrivalStep{
			runtime: r, packet: packet.Autonomous(), packets: switchPackets,
			sessionKey:     packet.Address.String(),
			generation:     commandSession.generation,
			targetObjectID: targetObjectID,
			timestamp: packet.SourceTime +
				uint64(campaignCreatureWarpOutDelay/time.Millisecond),
			isKnockback: true,
		}
		producer := raknet.ScheduledPacketProducer{
			Delay: campaignCreatureWarpOutDelay, Produce: step.produce,
		}
		producers := r.registry.producerGuard.scheduledProducers(
			packet.Address.String(), []raknet.ScheduledPacketProducer{producer},
		)
		cancel, scheduleErr := packet.Autonomous().ScheduleProducers(producers)
		if scheduleErr == nil && cancel == nil {
			scheduleErr = errors.New("switch arrival cancellation unavailable")
		}
		if scheduleErr == nil {
			r.logger.Printf(
				"RakNet campaign character switch arrival scheduled source=%d target=%d delay_ms=%d",
				sourceObjectID, targetObjectID, campaignCreatureWarpOutDelay.Milliseconds(),
			)
			return departurePackets, nil
		}
		r.logger.Printf(
			"RakNet campaign character switch arrival schedule failed source=%d target=%d: %v",
			sourceObjectID, targetObjectID, scheduleErr,
		)
		arrivalPackets, arrivalErr := step.produce()
		if arrivalErr != nil {
			return nil, fmt.Errorf("switchArrivalFallback: %w", arrivalErr)
		}
		return append(departurePackets, arrivalPackets...), nil
	}
	step := gameplaySwitchArrivalStep{
		runtime: r, packet: packet.Autonomous(), packets: switchPackets,
		sessionKey:     packet.Address.String(),
		generation:     commandSession.generation,
		targetObjectID: targetObjectID,
		timestamp:      packet.SourceTime + uint64(deathSelectionDelay/time.Millisecond),
		isKnockback:    true,
	}
	return step.produce()
}

func (r gameplaySwitchRuntime) reject(
	command raknet.ActionCommandData, commandSession gameplayPeerSession,
	switchStartTime time.Time, reason string,
) ([][]byte, error) {
	if command.Common.ObjectID == 0 && commandSession.isHeroSelectionPending {
		command.Common.ObjectID = commandSession.deployedObjectID
	}
	ackPacket, err := actionraknet.Reject(command)
	if err != nil {
		return nil, fmt.Errorf("switchReject: %w", err)
	}
	sourceHitPoint := float32(0)
	targetHitPoint := float32(0)
	isCooldownReady := false
	if commandSession.squad != nil {
		sourceCharacter, isSourceCharacterFound := commandSession.squad.DeployedCharacter()
		if isSourceCharacterFound {
			sourceHitPoint = sourceCharacter.HitPoints
		}
		targetCharacter, isTargetCharacterFound := commandSession.squad.Character(command.Value)
		if isTargetCharacterFound {
			targetHitPoint = targetCharacter.HitPoints
		}
		isCooldownReady = commandSession.squad.IsDeployReady(switchStartTime)
	}
	r.logger.Printf(
		"RakNet campaign character switch rejected source=%d creature=%d deployed=%d source_hp=%.1f target_hp=%.1f selection_pending=%t cooldown_ready=%t reason=%s",
		command.Common.ObjectID, command.Value, commandSession.deployedObjectID,
		sourceHitPoint, targetHitPoint, commandSession.isHeroSelectionPending,
		isCooldownReady, reason,
	)
	return [][]byte{ackPacket}, nil
}

const campaignHeroResourceFallback = float32(200)

type characterResourceMaximum struct {
	hitPoint  float32
	manaPoint float32
}

func (s gameplayPeerSession) characterResourceMaximums(
	creatureIndex uint32,
) characterResourceMaximum {
	hitPoint := campaignHeroResourceFallback
	powerPoint := campaignHeroResourceFallback
	if (s.binding.Mode != game.ModeChain && s.binding.Mode != game.ModeTutorial) ||
		creatureIndex >= uint32(len(s.binding.Creatures)) {
		return characterResourceMaximum{hitPoint: hitPoint, manaPoint: powerPoint}
	}
	if s.maximumHitPoints[creatureIndex] > 0 {
		hitPoint = s.maximumHitPoints[creatureIndex]
	}
	if s.maximumManaPoints[creatureIndex] > 0 {
		powerPoint = s.maximumManaPoints[creatureIndex]
	}
	attributes := s.crystalInventory.Attributes()
	hitPoint += attributes[4]
	powerPoint += attributes[5]
	return characterResourceMaximum{hitPoint: hitPoint, manaPoint: powerPoint}
}

func (s gameplayPeerSession) characterResourceMaximum(
	creatureIndex uint32,
) (float32, float32) {
	maximum := s.characterResourceMaximums(creatureIndex)
	return maximum.hitPoint, maximum.manaPoint
}

func (s gameplayPeerSession) characterHitPointMaximum(creatureIndex uint32) float32 {
	return s.characterResourceMaximums(creatureIndex).hitPoint
}

func (s gameplayPeerSession) characterManaPointMaximum(creatureIndex uint32) float32 {
	return s.characterResourceMaximums(creatureIndex).manaPoint
}

func (s *gameplayPeerSession) normalizeCampaignCharacterResources(
	creatureIndex uint32,
) (squad.Character, error) {
	if s == nil || s.squad == nil {
		return squad.Character{}, errors.New("campaign squad unavailable")
	}
	character, isFound := s.squad.Character(creatureIndex)
	if !isFound || !character.IsAvailable {
		return squad.Character{}, errors.New("campaign character unavailable")
	}
	maximum := s.characterResourceMaximums(creatureIndex)
	hitPoint := min(character.HitPoints, maximum.hitPoint)
	manaPoint := min(character.ManaPoints, maximum.manaPoint)
	if hitPoint == character.HitPoints && manaPoint == character.ManaPoints {
		return character, nil
	}
	_, err := s.squad.SetHitPoints(creatureIndex, hitPoint)
	if err != nil {
		return squad.Character{}, fmt.Errorf("resourceHealth: %w", err)
	}
	err = s.squad.SetManaPoints(creatureIndex, manaPoint)
	if err != nil {
		return squad.Character{}, fmt.Errorf("resourceMana: %w", err)
	}
	err = s.syncZoneSquadCheckpoint()
	if err != nil {
		return squad.Character{}, fmt.Errorf("resourceCheckpoint: %w", err)
	}
	character.HitPoints = hitPoint
	character.ManaPoints = manaPoint
	return character, nil
}

func (s *gameplayPeerSession) initializeCampaignResourceMaximums() {
	if s == nil {
		return
	}
	for creatureIndex, creature := range s.binding.Creatures {
		if creature.Noun == 0 {
			continue
		}
		if s.maximumHitPoints[creatureIndex] <= 0 {
			s.maximumHitPoints[creatureIndex] = creature.HitPoint
			if s.maximumHitPoints[creatureIndex] <= 0 {
				s.maximumHitPoints[creatureIndex] = campaignHeroResourceFallback
			}
		}
		if s.maximumManaPoints[creatureIndex] <= 0 {
			s.maximumManaPoints[creatureIndex] = creature.PowerPoint
			if s.maximumManaPoints[creatureIndex] <= 0 {
				s.maximumManaPoints[creatureIndex] = campaignHeroResourceFallback
			}
		}
	}
}

func newCampaignSquad(creatures [squad.Size]game.GameplayCreature) (*squad.Session, error) {
	characters := [squad.Size]squad.Character{}
	if creatures[0].Noun == 0 {
		creatures[0] = game.GameplayCreature{
			Noun: util.HashID("PC_EL_Rogue.Noun"),
		}
	}
	for index, creature := range creatures {
		if creature.Noun == 0 {
			continue
		}
		hitPoint := creature.HitPoint
		if hitPoint <= 0 {
			hitPoint = campaignHeroResourceFallback
		}
		powerPoint := creature.PowerPoint
		if powerPoint <= 0 {
			powerPoint = campaignHeroResourceFallback
		}
		characters[index] = squad.Character{
			HitPoints: hitPoint, ManaPoints: powerPoint,
			IsAvailable: true,
		}
	}
	squad, err := squad.New(characters, 0)
	if err != nil {
		return nil, fmt.Errorf("campaignSquadCreate: %w", err)
	}
	return squad, nil
}

func (s *gameplayPeerSession) setCampaignCharacterHitPoints(
	creatureIndex uint32, hitPoint float32,
) (bool, error) {
	if s == nil || s.squad == nil ||
		(s.binding.Mode != game.ModeChain && s.binding.Mode != game.ModeTutorial) {
		return false, errors.New("campaign squad unavailable")
	}
	if creatureIndex == s.squad.DeployedIndex() {
		return s.setDeployedHitPoints(hitPoint)
	}
	isGameOver, err := s.squad.SetHitPoints(creatureIndex, hitPoint)
	if err != nil {
		return false, fmt.Errorf("campaignCharacterHealth: %w", err)
	}
	err = s.syncZoneSquadCheckpoint()
	if err != nil {
		return false, fmt.Errorf("campaignCharacterHealthCheckpoint: %w", err)
	}
	return isGameOver, nil
}

func (s *gameplayPeerSession) setCampaignCharacterManaPoints(
	creatureIndex uint32, manaPoint float32,
) error {
	if s == nil || s.squad == nil ||
		(s.binding.Mode != game.ModeChain && s.binding.Mode != game.ModeTutorial) {
		return errors.New("campaign squad unavailable")
	}
	if creatureIndex == s.squad.DeployedIndex() {
		return s.setDeployedManaPoints(manaPoint)
	}
	err := s.squad.SetManaPoints(creatureIndex, manaPoint)
	if err != nil {
		return fmt.Errorf("campaignCharacterMana: %w", err)
	}
	err = s.syncZoneSquadCheckpoint()
	if err != nil {
		return fmt.Errorf("campaignCharacterManaCheckpoint: %w", err)
	}
	return nil
}

func (s *gameplayPeerSession) applyCampaignDamageHitPackets(
	hitPackets [][]byte, sourceObjectID uint32, hitPoint float32,
	timestamp uint64, now time.Time,
) ([][]byte, sporenet.PlayerStatDelta, error) {
	return s.applyCampaignDamageHitPacketsWithCommit(
		hitPackets, sourceObjectID, hitPoint, 0, timestamp, now, false,
	)
}

func (s *gameplayPeerSession) applyCampaignCommittedDamageHitPackets(
	hitPackets [][]byte, previousHitPoint float32, hitPoint float32,
	timestamp uint64, now time.Time,
) ([][]byte, sporenet.PlayerStatDelta, error) {
	return s.applyCampaignDamageHitPacketsWithCommit(
		hitPackets, 0, hitPoint, previousHitPoint, timestamp, now, true,
	)
}

func (s *gameplayPeerSession) applyCampaignDamageHitPacketsWithCommit(
	hitPackets [][]byte, sourceObjectID uint32, hitPoint float32,
	committedPreviousHitPoint float32, timestamp uint64, now time.Time,
	isSharedCommitted bool,
) ([][]byte, sporenet.PlayerStatDelta, error) {
	if s == nil || s.squad == nil ||
		(s.binding.Mode != game.ModeChain && s.binding.Mode != game.ModeTutorial) {
		return nil, sporenet.PlayerStatDelta{}, errors.New("campaign squad unavailable")
	}
	if s.ghostFormRun != nil && s.ghostFormRun.IsActive(
		s.deployedCreatureIndex, s.deployedHitPoint() > 0,
	) {
		if isSharedCommitted {
			return nil, sporenet.PlayerStatDelta{},
				errors.New("campaign committed damage entered Ghost Form")
		}
		dodgePackets, err := abilityraknet.GhostFormDodgePackets(
			s.deployedObjectID, sourceObjectID,
			zoneability.GhostFormPassthroughEffect,
		)
		if err != nil {
			return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("campaignGhostDodge: %w", err)
		}
		return s.finishCampaignDamage(
			dodgePackets, sporenet.PlayerStatDelta{},
		)
	}
	previousHitPoint := s.deployedHitPoint()
	if isSharedCommitted {
		previousHitPoint = committedPreviousHitPoint
		if s.zone == nil || s.zone.Hero() == nil {
			return nil, sporenet.PlayerStatDelta{},
				errors.New("campaign committed hero authority unavailable")
		}
		actor, isFound := s.zone.Hero().Snapshot(
			s.binding.UserID, s.generation,
		)
		if !isFound || actor.ObjectID != s.deployedObjectID {
			return nil, sporenet.PlayerStatDelta{},
				errors.New("campaign committed hero unavailable")
		}
		hitPoint = actor.HitPoint
	}
	if previousHitPoint <= 0 && hitPoint <= 0 {
		return s.finishCampaignDamage(hitPackets, sporenet.PlayerStatDelta{})
	}
	isLethal := previousHitPoint > 0 && hitPoint <= 0
	isExpectedGameOver := isLethal && s.squad.LivingCount() == 1
	resourcePacket, err := s.marshalCampaignCharacterResourceAt(
		s.squad.DeployedIndex(), hitPoint,
	)
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("campaignResource: %w", err)
	}
	var stopPackets [][]byte
	var deathPackets [][]byte
	if isLethal {
		err = s.stopPlayerMovement(now)
		if err != nil {
			return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("campaignDeathMovement: %w", err)
		}
		stopPackets, err = marshalZonePlayerStop(
			s.deployedObjectID, s.playerPosition,
		)
		if err != nil {
			return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("campaignDeathStop: %w", err)
		}
		if s.deployedCreatureIndex >= uint32(len(s.binding.Creatures)) {
			return nil, sporenet.PlayerStatDelta{}, errors.New("campaign defeated creature missing")
		}
		deathPacket, marshalErr := raknet.MarshalApplication(
			raknet.SetAnimationStateMessage{
				ObjectID: s.deployedObjectID,
				State:    util.HashID("gen_player_death"), Timestamp: timestamp, Scale: 1,
			},
		)
		err = marshalErr
		if err != nil {
			return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("campaignDeathAnimation: %w", err)
		}
		deathPackets = [][]byte{deathPacket}
	}
	var gameOverPacket []byte
	if isExpectedGameOver && s.binding.Mode != game.ModeChain {
		gameOverPacket, err = outcomeraknet.GameOver()
		if err != nil {
			return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("campaignGameOverMarshal: %w", err)
		}
	}
	isGameOver := false
	if isSharedCommitted {
		isGameOver, err = s.squad.SetHitPoints(
			s.squad.DeployedIndex(), hitPoint,
		)
	} else {
		isGameOver, err = s.setCampaignCharacterHitPoints(
			s.squad.DeployedIndex(), hitPoint,
		)
	}
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("campaignHealth: %w", err)
	}
	if isSharedCommitted {
		err = s.syncZoneSquadCheckpoint()
		if err != nil {
			return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("campaignHealthCheckpoint: %w", err)
		}
	}
	statDelta := sporenet.PlayerStatDelta{
		PVEDamageTaken: float64(max(float32(0), previousHitPoint-hitPoint)),
	}
	if isLethal {
		statDelta.PVEDeath = 1
		if s.zone != nil {
			_ = s.zone.ApplyHeroDeathObjectives(
				context.Background(), s.binding.UserID, s.generation,
				s.deployedObjectID,
			)
		}
		s.resetPassiveKill(s.deployedCreatureIndex)
		s.resetPassiveDamageReduction(s.deployedCreatureIndex)
		s.resetFireRavagerBasic(s.deployedCreatureIndex)
		s.resetTCShield(s.deployedCreatureIndex)
	}
	if isLethal {
		hitPackets = append(hitPackets, stopPackets...)
	}
	hitPackets = append(hitPackets, resourcePacket)
	if hitPoint < previousHitPoint {
		stealthPackets, stealthErr := s.breakTrapperStealth()
		if stealthErr == nil {
			hitPackets = append(hitPackets, stealthPackets...)
		}
	}
	if isLethal {
		interruptedBasic := s.resetAbilityAdmissionForSwitch()
		if interruptedBasic != nil {
			interruptedBasic.Stop()
		}
		drainPackets, drainErr := s.stopCampaignNPCDrainsTargeting(
			s.deployedObjectID,
		)
		if drainErr != nil {
			return nil, sporenet.PlayerStatDelta{},
				fmt.Errorf("campaignDeathDrain: %w", drainErr)
		}
		hitPackets = append(hitPackets, drainPackets...)
		pullPackets, pullErr := s.stopCampaignNPCPullEffectsTargeting(
			s.deployedObjectID,
		)
		if pullErr != nil {
			return nil, sporenet.PlayerStatDelta{},
				fmt.Errorf("campaignDeathPullEffect: %w", pullErr)
		}
		hitPackets = append(hitPackets, pullPackets...)
		s.squad.ResetDeployCooldown()
		s.clearEnemyHeroStatuses()
		// The native death transition can replace the controlled object's local
		// combatant resources with its class defaults. Reassert both resource
		// channels after the animation so the squad HUD and active-object bars
		// continue to show the authoritative defeated state.
		deathResourcePacket, marshalErr := raknet.MarshalApplication(
			raknet.CombatantDataDeltaMessage{
				ObjectID:  s.deployedObjectID,
				HitPoints: hitPoint, IsHitPointChanged: true,
				ManaPoints: s.deployedManaPoint(), IsManaPointChanged: true,
			},
		)
		err = marshalErr
		if err != nil {
			return nil, sporenet.PlayerStatDelta{},
				fmt.Errorf("campaignDeathResource: %w", err)
		}
		hitPackets = append(hitPackets, resourcePacket, deathResourcePacket)
		// Keep death as the final pose mutation. Resource replication can make
		// some playable rigs return to their ordinary locomotion state when it
		// follows SetAnimationState in the same ordered batch.
		hitPackets = append(hitPackets, deathPackets...)
		treePackets, treeErr := s.stopCampaignTreeOfLife()
		if treeErr == nil {
			hitPackets = append(hitPackets, treePackets...)
		}
		passivePackets, passiveErr := s.stopCampaignSagePassive()
		if passiveErr == nil {
			hitPackets = append(hitPackets, passivePackets...)
		}
		dronePackets, droneErr := s.stopFieldMedicDrone()
		if droneErr == nil {
			hitPackets = append(hitPackets, dronePackets...)
		}
		beastPackets, beastErr := s.stopBeastPet()
		if beastErr == nil {
			hitPackets = append(hitPackets, beastPackets...)
		}
		summonPackets, summonErr := s.stopHeroSummons()
		if summonErr == nil {
			hitPackets = append(hitPackets, summonPackets...)
		}
		fireTempestPackets, fireTempestErr := s.stopFireTempestActive()
		if fireTempestErr == nil {
			hitPackets = append(hitPackets, fireTempestPackets...)
		}
		plasmaPackets, plasmaErr := s.stopPlasmaSentinelActive()
		if plasmaErr == nil {
			hitPackets = append(hitPackets, plasmaPackets...)
		}
		stealthPackets, stealthErr := s.stopTrapperStealth()
		if stealthErr == nil {
			hitPackets = append(hitPackets, stealthPackets...)
		}
		if s.plasmaWreathRun != nil {
			wreathPackets, wreathErr := s.plasmaWreathRun.Stop()
			s.plasmaWreathRun = nil
			if wreathErr == nil {
				hitPackets = append(hitPackets, wreathPackets...)
			}
		}
	}
	if isGameOver {
		if s.binding.Mode == game.ModeChain && s.zone != nil {
			s.isHeroSelectionPending = false
			s.isHeroSelectionScheduled = false
			s.heroSelectionReadyAt = time.Time{}
			_, err = s.zone.ReleaseHeroTarget(
				s.binding.UserID, s.generation, s.deployedObjectID,
			)
			if err != nil {
				return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("partyDeathTarget: %w", err)
			}
			return s.finishCampaignDamage(hitPackets, statDelta)
		}
		s.stopCampaignNPCProjectiles()
		s.isHeroSelectionPending = false
		s.isHeroSelectionScheduled = false
		s.heroSelectionReadyAt = time.Time{}
		if s.zone != nil && s.zone.NPCs() != nil {
			err = s.zone.NPCs().ClearTargets()
			if err == nil {
				targetPackets, marshalErr := npcraknet.TargetUpdates(
					s.zone.NPCs().Snapshots(),
				)
				if marshalErr == nil {
					hitPackets = append(hitPackets, targetPackets...)
				}
			}
		}
		return s.finishCampaignDamage(
			append(hitPackets, gameOverPacket), statDelta,
		)
	}
	if hitPoint > 0 {
		return s.finishCampaignDamage(hitPackets, statDelta)
	}
	if s.squad.LivingCount() > 0 {
		s.basicSequenceSession().ReleaseHeld()
		s.campaignPlayerPursuitSession().Cancel()
		s.stopCampaignNPCProjectiles()
		if s.zone == nil || s.zone.NPCs() == nil {
			s.isHeroSelectionPending = true
			s.isHeroSelectionScheduled = false
			s.heroSelectionReadyAt = now.Add(campaignHeroDeathSelectionDelay)
			return s.finishCampaignDamage(hitPackets, statDelta)
		}
		_, err = s.zone.ReleaseHeroTarget(
			s.binding.UserID, s.generation, s.deployedObjectID,
		)
		s.isHeroSelectionPending = true
		s.isHeroSelectionScheduled = false
		s.heroSelectionReadyAt = now.Add(campaignHeroDeathSelectionDelay)
		if err == nil {
			targetPackets, marshalErr := npcraknet.TargetUpdates(s.zone.NPCs().Snapshots())
			if marshalErr == nil {
				hitPackets = append(hitPackets, targetPackets...)
			}
		}
		return s.finishCampaignDamage(hitPackets, statDelta)
	}
	return nil, sporenet.PlayerStatDelta{}, errors.New("campaign living character missing")
}

func (s *gameplayPeerSession) finishCampaignDamage(
	packets [][]byte, statDelta sporenet.PlayerStatDelta,
) ([][]byte, sporenet.PlayerStatDelta, error) {
	return packets, statDelta, nil
}

func (s *gameplayPeerSession) stopCampaignTreeOfLife() ([][]byte, error) {
	if s == nil || s.treeOfLifeRun == nil {
		return nil, nil
	}
	s.treeOfLifeRun.Stop()
	objectID := s.treeOfLifeObjectID
	s.treeOfLifeRun = nil
	s.treeOfLifeObjectID = 0
	if objectID == 0 {
		return nil, nil
	}
	packet, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{
		ObjectID: []uint32{objectID},
	})
	if err != nil {
		return nil, fmt.Errorf("treeDeleteMarshal: %w", err)
	}
	return [][]byte{packet}, nil
}

func (s *gameplayPeerSession) stopCampaignSagePassive() ([][]byte, error) {
	if s == nil || s.sagePassive == nil {
		return nil, nil
	}
	run := s.sagePassive
	s.sagePassive = nil
	stopSummonCompanionAttacks(s.sagePassiveActivations)
	s.removeCampaignCompanions()
	s.sagePassiveActivations = nil
	requests, err := run.Stop()
	if err != nil {
		return nil, fmt.Errorf("passiveStop: %w", err)
	}
	packets := make([][]byte, 0, len(requests))
	for index, request := range requests {
		requestPackets, _, marshalErr := marshalSummonPassiveWorldRequest(request, raknet.Vector3{})
		if marshalErr != nil {
			return nil, fmt.Errorf("passiveDelete[%d]: %w", index, marshalErr)
		}
		packets = append(packets, requestPackets...)
	}
	return packets, nil
}

func (s *gameplayPeerSession) removeCampaignCompanions() {
	if s == nil || s.zone == nil || s.zone.Companion() == nil {
		return
	}
	for objectID := range s.sagePassiveActivations {
		s.zone.Companion().Remove(objectID)
	}
}

func (s gameplayPeerSession) marshalCampaignCharacterResource(creatureIndex uint32) ([]byte, error) {
	if s.squad == nil || creatureIndex >= squad.Size ||
		creatureIndex >= uint32(len(s.binding.Creatures)) {
		return nil, errors.New("campaign character resource unavailable")
	}
	character, isFound := s.squad.Character(creatureIndex)
	if !isFound {
		return nil, errors.New("campaign character resource missing")
	}
	return s.marshalCampaignCharacterResourceAt(creatureIndex, character.HitPoints)
}

func (s gameplayPeerSession) marshalCampaignCharacterResourceAt(
	creatureIndex uint32, hitPoint float32,
) ([]byte, error) {
	if s.squad == nil || creatureIndex >= squad.Size ||
		creatureIndex >= uint32(len(s.binding.Creatures)) {
		return nil, errors.New("campaign character resource unavailable")
	}
	character, isFound := s.squad.Character(creatureIndex)
	if !isFound {
		return nil, errors.New("campaign character resource missing")
	}
	return s.marshalCampaignCharacterResourceValues(
		creatureIndex, hitPoint, character.ManaPoints,
	)
}

func (s gameplayPeerSession) marshalCampaignCharacterResourceValues(
	creatureIndex uint32, hitPoint float32, manaPoint float32,
) ([]byte, error) {
	if creatureIndex >= squad.Size ||
		creatureIndex >= uint32(len(s.binding.Creatures)) {
		return nil, errors.New("campaign character resource unavailable")
	}
	maximumHitPoint, maximumManaPoint := s.characterResourceMaximum(creatureIndex)
	packet, err := heroraknet.Resource(heroraknet.ResourceRequest{
		PlayerIndex: uint8(s.binding.Slot), CreatureIndex: creatureIndex,
		HitPoint: hitPoint, MaximumHitPoint: maximumHitPoint,
		ManaPoint: manaPoint, MaximumManaPoint: maximumManaPoint,
	})
	if err != nil {
		return nil, fmt.Errorf("campaignCharacterResourceMarshal: %w", err)
	}
	return packet, nil
}
