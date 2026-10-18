package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type heroKnockbackLanding struct {
	objectID     uint32
	readyAt      time.Time
	holdUntil    time.Time
	isNativeJump bool
	isLanded     bool
}

func (e *gameplayPeerSession) retainKnockbackLanding(
	plan zonenpc.AttackPlan, origin, destination game.Vec3, now time.Time,
) {
	// A newer forced reaction replaces the previous one, including a pull.
	e.heroKnockbackLanding = heroKnockbackLanding{}
	if plan.Profile.ForcedMovementReactionName != "react_knockback" ||
		plan.Profile.ForcedMovementSpeed <= 0 {
		return
	}
	duration := time.Duration(float64(destination.Sub(origin).Length()/
		plan.Profile.ForcedMovementSpeed) * float64(time.Second))
	e.heroKnockbackLanding = heroKnockbackLanding{
		objectID: plan.TargetObjectID, readyAt: now.Add(duration),
	}
	if plan.Profile.ModifierName == "NomadShielderBashKnockback" {
		e.heroKnockbackLanding.isNativeJump = true
		e.heroKnockbackLanding.holdUntil = now.Add(duration + 500*time.Millisecond)
	}
}

func (e *gameplayPeerSession) isKnockbackActive(at time.Time) bool {
	return e != nil && e.deployedObjectID != 0 &&
		e.heroKnockbackLanding.objectID == e.deployedObjectID &&
		e.heroKnockbackLanding.isNativeJump && at.Before(e.heroKnockbackLanding.holdUntil)
}

func (e gameplayPendingRuntime) pollKnockbackLanding(packet raknet.Packet) ([][]byte, error) {
	sessionKey := packet.Address.String()
	e.registry.mutex.Lock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || member.transportGeneration != packet.TransportGeneration ||
		member.zone == nil || member.isRejoinPending {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	landing := member.heroKnockbackLanding
	if landing.objectID == 0 {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	isCanceled := member.deployedObjectID != landing.objectID ||
		member.deployedHitPoint() <= 0 || member.isHeroSelectionPending
	if !isCanceled && e.now().Before(landing.readyAt) {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	if !isCanceled && landing.isLanded && e.now().Before(landing.holdUntil) {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	isLandingDue := !isCanceled && !landing.isLanded
	member.heroKnockbackLanding = heroKnockbackLanding{}
	if !isCanceled && landing.isNativeJump && !landing.isLanded {
		landing.isLanded = true
		member.heroKnockbackLanding = landing
	}
	e.registry.sessions[sessionKey] = member
	e.registry.mutex.Unlock()
	if !isLandingDue {
		return nil, nil
	}
	// Authored knockback modifiers wait for the jump to finish, then call
	// ResetAnimationState before waiting for the outro. The reset exits the
	// looping airborne reaction and lets the client play its landing phase.
	landingPacket, err := npcraknet.ResetAnimation(landing.objectID, packet.SourceTime)
	if err != nil {
		return nil, fmt.Errorf("landingMarshal: %w", err)
	}
	packets := [][]byte{landingPacket}
	err = publishCampaignPeersAfterCommit(e.registry, packet, packets)
	if err != nil {
		return nil, fmt.Errorf("landingPublish: %w", err)
	}
	return packets, nil
}
