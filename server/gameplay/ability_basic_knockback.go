package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

func (e campaignDamageRuntime) applyBasicKnockbackLocked(sessionKey string, peerSession *gameplayPeerSession, target zonenpc.Snapshot, timestamp uint64) ([][]byte, error) {
	if target.IsMutationKnockbackImmune() {
		return nil, nil
	}
	if peerSession.zone.NPCs().RootRemaining(target.Plan.ObjectID, e.npc.now()) > 0 {
		return nil, nil
	}
	source := game.Vec3(peerSession.playerPosition)
	plan, desired, isPlanned := basicKnockbackPlan(peerSession.deployedObjectID, target.Plan.ObjectID, source, target.Plan.Position)
	if !isPlanned {
		return nil, nil
	}
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
	travel := basicKnockbackTravel(plan, destination)
	err = peerSession.zone.NPCs().ApplyStun(target.Plan.ObjectID, e.npc.now().Add(travel))
	if err != nil {
		return nil, fmt.Errorf("basicKnockbackHold: %w", err)
	}
	return append(interrupts, packets...), nil
}

func basicKnockbackPlan(sourceObjectID, targetObjectID uint32, source, target game.Vec3) (zonenpc.AttackPlan, game.Vec3, bool) {
	delta := target.Sub(source)
	distance := delta.Length()
	if distance <= 0 {
		return zonenpc.AttackPlan{}, game.Vec3{}, false
	}
	profile := zonenpc.BasicKnockbackProfile()
	plan := zonenpc.AttackPlan{
		SourceObjectID: sourceObjectID, TargetObjectID: targetObjectID,
		SourcePosition: source, TargetPosition: target, Profile: profile,
	}
	return plan, target.Add(delta.Scale(profile.ForcedMovementDistance / distance)), true
}

func basicKnockbackTravel(plan zonenpc.AttackPlan, destination game.Vec3) time.Duration {
	return time.Duration(float64(destination.Sub(plan.TargetPosition).Length()/plan.Profile.ForcedMovementSpeed) * float64(time.Second))
}
