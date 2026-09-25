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
const cryosLavaMinimumRestDuration = 4 * time.Second
const cryosLavaRestVariationCount = uint32(4)
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

func (s *gameplayPeerSession) applyCampaignLavaContact(
	position game.Vec3, timestamp uint64, now time.Time,
) ([][]byte, sporenet.PlayerStatDelta, error) {
	if s == nil || s.zone == nil || s.deployedObjectID == 0 ||
		s.deployedHitPoint() <= 0 {
		return nil, sporenet.PlayerStatDelta{}, nil
	}
	hazard, isContact := s.campaignLavaContact(position, now)
	if !isContact {
		return nil, sporenet.PlayerStatDelta{}, nil
	}
	packets, err := s.campaignLavaPresentation(hazard)
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("lavaPresentation: %w", err)
	}
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
		hitPacket, hitErr := raknet.MarshalApplication(raknet.ObjectEffectMessage{
			Asset: cryosLavaHitEffectID, ObjectID: s.deployedObjectID,
			AttackerID: hazard.sourceObjectID,
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
		deltaX := position.X - crack.Position.X
		deltaY := position.Y - crack.Position.Y
		deltaZ := float32(math.Abs(float64(position.Z - crack.Position.Z)))
		if deltaX*deltaX+deltaY*deltaY <= cryosLavaRadius*cryosLavaRadius &&
			deltaZ <= cryosLavaHeight {
			phase, cycle := campaignCryosLavaPhase(crack.MarkerID, now)
			return campaignLavaHazard{
				sourceObjectID: crack.MarkerID, position: crack.Position,
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
	presentationKey := uint64(hazard.sourceObjectID)<<32 | hazard.cycle&0xffffffff
	assetID := cryosLavaSpoutEffectID
	if hazard.phase == campaignLavaPhaseWarning {
		if s.campaignLavaWarningKey == presentationKey {
			return nil, nil
		}
		s.campaignLavaWarningKey = presentationKey
		assetID = util.HashID("effect_Environment_Cryos_Geyser_Warning.ServerEventDef")
	} else {
		if s.campaignLavaSpoutKey == presentationKey {
			return nil, nil
		}
		s.campaignLavaSpoutKey = presentationKey
	}
	packet, err := raknet.MarshalApplication(raknet.PositionedEffectMessage{
		Asset: assetID,
		Position: raknet.Vector3{
			X: hazard.position.X, Y: hazard.position.Y, Z: hazard.position.Z,
		},
		Facing: raknet.Vector3{Z: 1},
	})
	if err != nil {
		return nil, fmt.Errorf("lavaEffect: %w", err)
	}
	return [][]byte{packet}, nil
}
