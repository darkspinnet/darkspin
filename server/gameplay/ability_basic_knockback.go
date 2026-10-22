package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

func (e campaignDamageRuntime) applyBasicKnockbackLocked(sessionKey string, peerSession *gameplayPeerSession, target zonenpc.Snapshot, timestamp uint64) ([][]byte, error) {
	if peerSession.zone.NPCs().RootRemaining(target.Plan.ObjectID, e.npc.now()) > 0 {
		return nil, nil
	}
	source := game.Vec3(peerSession.playerPosition)
	delta := target.Plan.Position.Sub(source)
	distance := delta.Length()
	if distance <= 0 {
		return nil, nil
	}
	desired := target.Plan.Position.Add(delta.Scale(3 / distance))
	destination, isFound, err := navigationClippedMovementDestination(peerSession.zone.Navigation(), target.Plan.Position, desired, max(target.NavigationRadius(), float32(.25)), target.Navigation)
	if err != nil {
		return nil, fmt.Errorf("basicKnockbackDestination: %w", err)
	}
	if !isFound {
		return nil, nil
	}
	interrupts, err := e.registry.interruptCampaignNPCForForcedMovementLocked(sessionKey, peerSession, target.Plan.ObjectID)
	if err != nil {
		return nil, fmt.Errorf("basicKnockbackInterrupt: %w", err)
	}
	plan := zonenpc.AttackPlan{SourceObjectID: peerSession.deployedObjectID, TargetObjectID: target.Plan.ObjectID, SourcePosition: source, TargetPosition: target.Plan.Position, Profile: zonenpc.ActionProfile{ForcedMovementSpeed: 25, ForcedMovementDistance: 3, ForcedMovementReactionName: "react_knockback"}}
	// ForcePulse supplies no optional JumpInDirection arc arguments. Native
	// sub_A09C50 reads nil Lua slots10-12 as zero, then sub_A15240 copies them.
	packets, err := npcraknet.ForcedJump(plan, destination, timestamp, [3]float32{})
	if err != nil {
		return nil, fmt.Errorf("basicKnockbackMarshal: %w", err)
	}
	err = peerSession.zone.NPCs().SetPosition(target.Plan.ObjectID, destination)
	if err != nil {
		return nil, fmt.Errorf("basicKnockbackMove: %w", err)
	}
	travel := time.Duration(float64(destination.Sub(target.Plan.Position).Length()/25) * float64(time.Second))
	err = peerSession.zone.NPCs().ApplyStun(target.Plan.ObjectID, e.npc.now().Add(travel))
	if err != nil {
		return nil, fmt.Errorf("basicKnockbackHold: %w", err)
	}
	return append(interrupts, packets...), nil
}
