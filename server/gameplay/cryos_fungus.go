package gameplay

import (
	"fmt"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func (e gameplayPendingRuntime) pollCryosFungusLocked(
	member *gameplayPeerSession, timestamp uint64,
) ([][]byte, error) {
	now := e.now()
	packets := make([][]byte, 0)
	position := game.Vec3(member.playerPosition)
	for _, fixture := range member.zone.NPCs().Snapshots() {
		if !fixture.IsDefeated || !strings.EqualFold(fixture.Plan.NounName, game.CryosFungusNoun) {
			continue
		}
		if member.cryosFungusExpirations == nil {
			member.cryosFungusExpirations = make(map[uint32]time.Time)
		}
		expiresAt, isSeen := member.cryosFungusExpirations[fixture.Plan.ObjectID]
		if !isSeen {
			effectID := util.HashID("hazard_poisonStalk_explosion")
			cloudPosition := raknet.Vector3(fixture.Plan.Position)
			cloudPacket, err := raknet.MarshalApplication(raknet.ServerEventContractMessage{
				SimpleSwarmEffectID: &effectID, Position: &cloudPosition,
			})
			if err != nil {
				return nil, fmt.Errorf("fungusCloud: %w", err)
			}
			packets = append(packets, cloudPacket)
			expiresAt = now.Add(8 * time.Second)
			member.cryosFungusExpirations[fixture.Plan.ObjectID] = expiresAt
		}
		if now.Before(expiresAt) && fixture.Plan.Position.Sub(position).Length() <= 3 {
			member.cryosPoisonExpiresAt = now.Add(3 * time.Second)
			member.cryosPoisonSourceID = fixture.Plan.ObjectID
		}
	}
	if member.deployedHitPoint() <= 0 || !now.Before(member.cryosPoisonExpiresAt) ||
		now.Before(member.cryosPoisonReadyAt) {
		return packets, nil
	}
	plan := zonenpc.AttackPlan{
		SourceObjectID: member.cryosPoisonSourceID, TargetObjectID: member.deployedObjectID,
		TargetPosition: position,
		Profile: zonenpc.ActionProfile{
			Family: zonenpc.ActionRetainedArea, AbilityName: "ToxicFungusPoison",
			MinimumDamage: 4, MaximumDamage: 4, DamageType: 4, IsDamageProfileKnown: true,
		},
	}
	damagePackets, delta, isApplied, err := e.damage.npc.applyEnemyStatusDamage(
		member, member.generation, plan, zonenpc.AttackResult{Damage: 4}, timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("fungusPoison: %w", err)
	}
	member.cryosPoisonReadyAt = now.Add(time.Second)
	if isApplied {
		member.queueStatDelta(delta)
	}
	return append(packets, damagePackets...), nil
}
