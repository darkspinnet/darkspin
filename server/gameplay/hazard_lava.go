package gameplay

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/darkspinnet/darkspin/server/util"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const cryosLavaRadius = float32(2.75)
const cryosLavaHeight = float32(3)
const infinityMoltenMetalLevel = "infinity_2"
const infinityMoltenMetalCenterX = float32(-211.25)
const infinityMoltenMetalCenterY = float32(83.90)
const infinityMoltenMetalCenterZ = float32(110.40)
const infinityMoltenMetalRadius = float32(7.5)
const infinityMoltenMetalHeight = float32(2.5)
const campaignLavaDamageFraction = float32(0.10)
const campaignLavaDamageCooldown = 2 * time.Second
const cryosLavaWarningDuration = 2 * time.Second
const cryosLavaSpoutDuration = time.Second
const cryosLavaMinimumRestDuration = 7 * time.Second
const cryosLavaRestVariationCount = uint32(1)
const cryosLavaSpoutEffectID = uint32(0xa6120b9c)
const cryosLavaHitEffectID = uint32(0x78da313e)

type campaignLavaPhase uint8

const (
	campaignLavaPhaseContinuous campaignLavaPhase = iota
	campaignLavaPhaseRest
	campaignLavaPhaseWarning
	campaignLavaPhaseSpout
)

type campaignLavaHazard struct {
	sourceObjectID uint32
	position       game.Vec3
	phase          campaignLavaPhase
	cycle          uint64
}

func (e *gameplayPeerSession) pollCryosGeyserEffects(now time.Time) ([][]byte, error) {
	packets := make([][]byte, 0)
	for _, crack := range e.zone.DirectorDefinition().CryosLavaCracks() {
		sourceObjectID, isActive := e.cryosGeyserSourceObjectID(crack.MarkerID)
		if !isActive {
			continue
		}
		phase, cycle := campaignCryosLavaPhase(crack.MarkerID, now)
		effects, err := e.campaignLavaPresentation(campaignLavaHazard{
			sourceObjectID: sourceObjectID, position: crack.Position,
			phase: phase, cycle: cycle,
		})
		if err != nil {
			return nil, fmt.Errorf("geyserEffect: %w", err)
		}
		packets = append(packets, effects...)
	}
	return packets, nil
}

func (s *gameplayPeerSession) applyCampaignLavaContact(
	position game.Vec3, timestamp uint64, now time.Time,
	runtime campaignNPCActionRuntime,
) ([][]byte, sporenet.PlayerStatDelta, error) {
	if s != nil && s.isHeroArrivalProtected(now) {
		return nil, sporenet.PlayerStatDelta{}, nil
	}
	plasmaPackets, plasmaDelta, isPlasmaActive, plasmaErr := runtime.applyPlasmaPoolContact(s, position, timestamp, now)
	if plasmaErr != nil {
		return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("plasmaContact: %w", plasmaErr)
	}
	if isPlasmaActive {
		return plasmaPackets, plasmaDelta, nil
	}
	if s == nil || s.zone == nil || s.deployedObjectID == 0 ||
		s.deployedHitPoint() <= 0 {
		return nil, sporenet.PlayerStatDelta{}, nil
	}
	hazard, isContact := s.campaignLavaContact(position, now)
	if (!isContact || hazard.phase != campaignLavaPhaseSpout) && now.Before(s.cryosBurnExpiresAt) {
		hazard, isContact = s.cryosBurnHazard, true
		hazard.phase = campaignLavaPhaseContinuous
	}
	if !isContact {
		return plasmaPackets, sporenet.PlayerStatDelta{}, nil
	}
	packets, err := s.campaignLavaPresentation(hazard)
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("lavaPresentation: %w", err)
	}
	packets = append(plasmaPackets, packets...)
	if hazard.phase == campaignLavaPhaseRest ||
		hazard.phase == campaignLavaPhaseWarning || now.Before(s.campaignLavaReadyAt) {
		return packets, sporenet.PlayerStatDelta{}, nil
	}
	maximumHitPoint, _, err := s.deployedResourceMaximum()
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("lavaMaximum: %w", err)
	}
	previousHitPoint := s.deployedHitPoint()
	damage := max(float32(1), maximumHitPoint*campaignLavaDamageFraction)
	damage = min(damage, previousHitPoint)
	if damage <= 0 {
		return nil, sporenet.PlayerStatDelta{}, nil
	}
	hitPoint := previousHitPoint - damage
	flags := uint16(0x0001)
	if hitPoint == 0 {
		flags |= 0x0004
	}
	eventPacket, err := raknet.MarshalApplication(raknet.DamageCombatEventMessage{
		Flags: flags, DeltaHealth: damage,
		TargetID: s.deployedObjectID, SourceID: hazard.sourceObjectID,
		IntegerHPChange: -int32(damage),
	})
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("lavaEvent: %w", err)
	}
	healthPacket, err := raknet.MarshalApplication(raknet.CombatantDataDeltaMessage{
		ObjectID: s.deployedObjectID, HitPoints: hitPoint, IsHitPointChanged: true,
	})
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("lavaHealth: %w", err)
	}
	textPacket, err := effectraknet.CombatText(effectraknet.CombatTextRequest{
		ObjectID: s.deployedObjectID, Position: position,
		Amount: damage, IsEnemyStyle: true,
	})
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("lavaText: %w", err)
	}
	damagePresentationPackets := [][]byte{eventPacket, textPacket, healthPacket}
	if hazard.phase == campaignLavaPhaseSpout {
		s.cryosBurnExpiresAt = now.Add(3 * time.Second)
		s.cryosBurnHazard = hazard
		hitEffectID := cryosLavaHitEffectID
		hitPacket, hitErr := raknet.MarshalApplication(raknet.ServerEventContractMessage{
			SimpleSwarmEffectID: &hitEffectID, ObjectID: &s.deployedObjectID,
		})
		if hitErr != nil {
			return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("lavaHit: %w", hitErr)
		}
		damagePresentationPackets = append(damagePresentationPackets, hitPacket)
	}
	timestamp = max(uint64(1), timestamp)
	damagePackets, statDelta, err := s.applyCampaignDamageHitPackets(
		damagePresentationPackets, hazard.sourceObjectID,
		hitPoint, timestamp, now,
	)
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("lavaDamage: %w", err)
	}
	s.campaignLavaReadyAt = now.Add(campaignLavaDamageCooldown)
	packets = append(packets, damagePackets...)
	return packets, statDelta, nil
}

func (s *gameplayPeerSession) campaignLavaContact(
	position game.Vec3, now time.Time,
) (campaignLavaHazard, bool) {
	director := s.zone.DirectorDefinition()
	cracks := director.CryosLavaCracks()
	for _, crack := range cracks {
		sourceObjectID, isActive := s.cryosGeyserSourceObjectID(crack.MarkerID)
		if !isActive {
			continue
		}
		deltaX := position.X - crack.Position.X
		deltaY := position.Y - crack.Position.Y
		deltaZ := float32(math.Abs(float64(position.Z - crack.Position.Z)))
		if deltaX*deltaX+deltaY*deltaY <= cryosLavaRadius*cryosLavaRadius &&
			deltaZ <= cryosLavaHeight {
			phase, cycle := campaignCryosLavaPhase(crack.MarkerID, now)
			return campaignLavaHazard{
				sourceObjectID: sourceObjectID, position: crack.Position,
				phase: phase, cycle: cycle,
			}, true
		}
	}
	if !strings.EqualFold(director.Level, infinityMoltenMetalLevel) {
		return campaignLavaHazard{}, false
	}
	deltaX := position.X - infinityMoltenMetalCenterX
	deltaY := position.Y - infinityMoltenMetalCenterY
	deltaZ := float32(math.Abs(float64(position.Z - infinityMoltenMetalCenterZ)))
	isContact := deltaX*deltaX+deltaY*deltaY <=
		infinityMoltenMetalRadius*infinityMoltenMetalRadius &&
		deltaZ <= infinityMoltenMetalHeight
	return campaignLavaHazard{
		position: game.Vec3{
			X: infinityMoltenMetalCenterX, Y: infinityMoltenMetalCenterY,
			Z: infinityMoltenMetalCenterZ,
		},
		phase: campaignLavaPhaseContinuous,
	}, isContact
}

func (s *gameplayPeerSession) cryosGeyserSourceObjectID(markerID uint32) (uint32, bool) {
	if s == nil || s.zone == nil {
		return 0, false
	}
	levelName := s.zone.DirectorDefinition().Level
	if !strings.EqualFold(levelName, "cryos_3") &&
		!strings.EqualFold(levelName, "cryos_1") {
		return markerID, true
	}
	if s.zone.NPCs() == nil {
		return 0, false
	}
	for _, fixture := range s.zone.NPCs().Snapshots() {
		if fixture.Plan.LocusID == markerID && zonenpc.IsCryosGeyser(fixture.Plan) {
			// The passive starts in CryosGeyserDeath, after the ice is opened.
			return fixture.Plan.ObjectID, fixture.IsPublished && fixture.IsDefeated
		}
	}
	return 0, false
}

func campaignCryosLavaPhase(markerID uint32, now time.Time) (campaignLavaPhase, uint64) {
	restVariation := time.Duration(markerID%cryosLavaRestVariationCount) * time.Second
	cycleDuration := cryosLavaMinimumRestDuration + restVariation +
		cryosLavaWarningDuration + cryosLavaSpoutDuration
	offset := time.Duration(markerID%uint32(cycleDuration/time.Millisecond)) * time.Millisecond
	elapsed := now.UnixMilli()*int64(time.Millisecond) + int64(offset)
	cycle := uint64(elapsed / int64(cycleDuration))
	phaseTime := time.Duration(elapsed % int64(cycleDuration))
	if phaseTime < cryosLavaMinimumRestDuration+restVariation {
		return campaignLavaPhaseRest, cycle
	}
	if phaseTime < cycleDuration-cryosLavaSpoutDuration {
		return campaignLavaPhaseWarning, cycle
	}
	return campaignLavaPhaseSpout, cycle
}

func (s *gameplayPeerSession) campaignLavaPresentation(
	hazard campaignLavaHazard,
) ([][]byte, error) {
	if hazard.phase != campaignLavaPhaseWarning && hazard.phase != campaignLavaPhaseSpout {
		return nil, nil
	}
	assetID := cryosLavaSpoutEffectID
	if hazard.phase == campaignLavaPhaseWarning {
		if cycle, isFound := s.cryosGeyserWarnings[hazard.sourceObjectID]; isFound && cycle == hazard.cycle {
			return nil, nil
		}
		if s.cryosGeyserWarnings == nil {
			s.cryosGeyserWarnings = make(map[uint32]uint64)
		}
		s.cryosGeyserWarnings[hazard.sourceObjectID] = hazard.cycle
		assetID = util.HashID("effect_Environment_Cryos_Geyser_Warning.ServerEventDef")
	} else {
		if cycle, isFound := s.cryosGeyserSpouts[hazard.sourceObjectID]; isFound && cycle == hazard.cycle {
			return nil, nil
		}
		if s.cryosGeyserSpouts == nil {
			s.cryosGeyserSpouts = make(map[uint32]uint64)
		}
		s.cryosGeyserSpouts[hazard.sourceObjectID] = hazard.cycle
	}
	position := raknet.Vector3(hazard.position)
	message := raknet.ServerEventContractMessage{}
	if hazard.phase == campaignLavaPhaseSpout {
		// The shipped passive emits a world-position swarm with no facing.
		message.SimpleSwarmEffectID = &assetID
		message.Position = &position
	} else {
		// Its warning event resolves presentation/audio through the vent owner.
		// A position-only event loses that attachment and spatial sound context.
		message.Asset = &assetID
		message.ObjectID = &hazard.sourceObjectID
	}
	packet, err := raknet.MarshalApplication(message)
	if err != nil {
		return nil, fmt.Errorf("lavaEffect: %w", err)
	}
	return [][]byte{packet}, nil
}
