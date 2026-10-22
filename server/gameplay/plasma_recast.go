package gameplay

import (
	"fmt"
	"log"

	"github.com/darkspinnet/darkspin/server/raknet"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
)

// A candidate never hides the accepted modifier while scheduler admission is
// pending. In particular, rejected recasts cannot strand its old continuations.
type plasmaWreathAdmission struct {
	run                 *abilityraknet.PlasmaWreathRun
	state               plasmaWreathState
	creatureIndex       uint32
	previousManaPoint   float32
	cooldownReservation zoneability.CooldownReservation
	releaseReservation  zoneaction.ReleaseReservation
	scheduleErr         error
}

func (e *gameplayPeerSession) rollbackPendingPlasmaWreath() {
	admission := e.plasmaWreathPending
	if admission == nil {
		return
	}
	e.plasmaWreathPending = nil
	admission.state.cancel()
	admission.run.Abort()
	err := e.setCampaignCharacterManaPoints(admission.creatureIndex, admission.previousManaPoint)
	if err != nil {
		log.Printf("Plasma Wreath pending mana rollback user=%d: %v", e.binding.UserID, err)
	}
	e.abilityCooldownSession().Rollback(admission.cooldownReservation)
	e.abilityReleaseSession().Rollback(admission.releaseReservation)
}

func (e plasmaWreathSchedule) acceptRecastLocked(member gameplayPeerSession, cancel raknet.CancelSchedule) (gameplayPeerSession, [][]byte, []byte, error) {
	admission := e.admission
	if admission.scheduleErr != nil {
		return member, nil, nil, fmt.Errorf("pendingSchedule: %w", admission.scheduleErr)
	}
	candidate := member
	candidate.plasmaWreathPending = nil
	candidate.plasmaWreathRun = e.run
	candidate.plasmaWreathState = admission.state
	candidate.plasmaWreathState.initialCancel = cancel
	candidate.plasmaWreathState.isAccepted = true
	attributePacket, err := e.runtime.registry.plasmaWreathAttributePacket(candidate, e.runtime.now())
	if err != nil {
		return member, nil, nil, fmt.Errorf("recastAttribute: %w", err)
	}
	packets := make([][]byte, 0)
	if member.plasmaWreathRun != nil {
		member.plasmaWreathState.cancel()
		var stopErr error
		packets, stopErr = member.plasmaWreathRun.Stop()
		if stopErr != nil {
			// Candidate serialization and scheduling have already succeeded.
			// Preserve any usable old removals and accept the complete new run.
			log.Printf("Plasma Wreath replaced cleanup source=%d: %v", e.sourceObjectID, stopErr)
		}
		e.runtime.registry.queuePlasmaWreathRemovalsLocked(&candidate, member.plasmaWreathRun, packets)
		packets = nil
	}
	return candidate, packets, attributePacket, nil
}
