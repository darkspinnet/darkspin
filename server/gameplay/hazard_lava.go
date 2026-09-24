package gameplay

import (
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sporenet"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
)

const cryosLavaRadius = float32(2.75)
const cryosLavaHeight = float32(3)
const cryosLavaDamageFraction = float32(0.10)
const cryosLavaDamageCooldown = 2 * time.Second

func (s *gameplayPeerSession) applyCryosLavaContact(
	position game.Vec3, timestamp uint64, now time.Time,
) ([][]byte, sporenet.PlayerStatDelta, error) {
	if s == nil || s.zone == nil || s.deployedObjectID == 0 ||
		s.deployedHitPoint() <= 0 || now.Before(s.cryosLavaReadyAt) {
		return nil, sporenet.PlayerStatDelta{}, nil
	}
	cracks := s.zone.DirectorDefinition().CryosLavaCracks()
	var sourceObjectID uint32
	for _, crack := range cracks {
		deltaX := position.X - crack.Position.X
		deltaY := position.Y - crack.Position.Y
		deltaZ := float32(math.Abs(float64(position.Z - crack.Position.Z)))
		if deltaX*deltaX+deltaY*deltaY <= cryosLavaRadius*cryosLavaRadius &&
			deltaZ <= cryosLavaHeight {
			sourceObjectID = crack.MarkerID
			break
		}
	}
	if sourceObjectID == 0 {
		return nil, sporenet.PlayerStatDelta{}, nil
	}
	maximumHitPoint, _, err := s.deployedResourceMaximum()
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("lavaMaximum: %w", err)
	}
	previousHitPoint := s.deployedHitPoint()
	damage := max(float32(1), maximumHitPoint*cryosLavaDamageFraction)
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
		TargetID: s.deployedObjectID, SourceID: sourceObjectID,
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
	timestamp = max(uint64(1), timestamp)
	packets, statDelta, err := s.applyCampaignDamageHitPackets(
		[][]byte{eventPacket, textPacket, healthPacket}, sourceObjectID,
		hitPoint, timestamp, now,
	)
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("lavaDamage: %w", err)
	}
	s.cryosLavaReadyAt = now.Add(cryosLavaDamageCooldown)
	return packets, statDelta, nil
}
