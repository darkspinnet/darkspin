package gameplay

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sporenet"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
)

const infinityMoltenMetalLevel = "infinity_2"
const infinityMoltenMetalCenterX = float32(-211.25)
const infinityMoltenMetalCenterY = float32(83.90)
const infinityMoltenMetalCenterZ = float32(110.40)
const infinityMoltenMetalRadius = float32(7.5)
const infinityMoltenMetalHeight = float32(2.5)
const campaignLavaDamageFraction = float32(0.10)
const campaignLavaDamageCooldown = 2 * time.Second

type campaignLavaPhase uint8

const (
	campaignLavaPhaseContinuous campaignLavaPhase = iota
)

type campaignLavaHazard struct {
	sourceObjectID uint32
	position       game.Vec3
	phase          campaignLavaPhase
	cycle          uint64
}

func (s *gameplayPeerSession) applyCampaignLavaContact(
	position game.Vec3, timestamp uint64, now time.Time,
	runtime campaignNPCActionRuntime,
) ([][]byte, sporenet.PlayerStatDelta, error) {
	if s != nil && (s.isGodModeEnabled || s.isHeroArrivalProtected(now)) {
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
	if !isContact {
		return plasmaPackets, sporenet.PlayerStatDelta{}, nil
	}
	packets := plasmaPackets
	if now.Before(s.campaignLavaReadyAt) {
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
