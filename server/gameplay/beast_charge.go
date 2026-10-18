package gameplay

import (
	"log"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/zone"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
)

// A pet leg belongs to the admitted deployment and pursuit, independently of
// the hero action slot once hero impact has committed.
func (e heroChargeSchedule) isPetCurrent(peerSession gameplayPeerSession, isFound bool) bool {
	if !isFound || e.run == nil || peerSession.generation != e.generation ||
		peerSession.zone != e.run.zone || peerSession.zone == nil ||
		peerSession.zone.Companion() == nil || peerSession.zone.NPCs() == nil ||
		peerSession.deployedObjectID != e.sourceObjectID ||
		peerSession.deployedCreatureIndex != e.creatureIndex ||
		peerSession.deployedHitPoint() <= 0 || peerSession.isZoneTerminal() {
		return false
	}
	e.run.mutex.Lock()
	defer e.run.mutex.Unlock()
	if e.run.isCleaned || !e.run.isPetPending ||
		(!e.run.isHeroImpacted && peerSession.heroCharge != e.run) {
		return false
	}
	pet, isPetFound := e.run.companion.Snapshot(e.petPursuit.ObjectID)
	if !isPetFound || pet.HitPoint <= 0 || pet.UserID != e.run.userID ||
		pet.PeerGeneration != e.generation || pet.OwnerObjectID != e.sourceObjectID ||
		pet.ObjectID != peerSession.beastPetObjectID || pet.FollowRevision != e.run.petFollowRevision {
		return false
	}
	if e.petPursuit.TravelDuration == 0 {
		return pet.PursuitObjectID == 0
	}
	return pet.PursuitObjectID == e.petPursuit.TargetObjectID &&
		pet.PursuitRevision == e.petPursuit.Revision
}

func (e *heroChargeRun) retirePet(at time.Time) [][]byte {
	if e == nil {
		return nil
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	return e.retirePetLocked(at)
}

// Called with the run lock held. A replaced or teleported pet must never be
// repositioned or have its replacement movement presentation reset.
func (e *heroChargeRun) retirePetLocked(at time.Time) [][]byte {
	if !e.isPetPending || e.companion == nil {
		return nil
	}
	e.isPetPending = false
	pursuit := e.petPursuit
	e.petPursuit.ObjectID = 0
	pet, isFound := e.companion.Snapshot(pursuit.ObjectID)
	if !isFound || pet.HitPoint <= 0 || pet.UserID != e.userID ||
		pet.PeerGeneration != e.generation || pet.OwnerObjectID != e.ownerObjectID ||
		pet.FollowRevision != e.petFollowRevision || pursuit.TravelDuration <= 0 ||
		pet.PursuitObjectID != pursuit.TargetObjectID || pet.PursuitRevision != pursuit.Revision {
		return nil
	}
	position := pursuit.Position
	if !at.Before(e.petDeadline) {
		position = e.petDestination
	} else if at.After(e.petStartedAt) {
		fraction := float32(float64(at.Sub(e.petStartedAt)) /
			float64(e.petDeadline.Sub(e.petStartedAt)))
		position = position.Add(e.petDestination.Sub(position).Scale(fraction))
	}
	isCancelled := e.companion.CancelPursuit(pursuit.ObjectID, pursuit.TargetObjectID, position, pursuit.Revision)
	if !isCancelled {
		return nil
	}
	pet.Position = position
	pet.PursuitObjectID = 0
	pet.PursuitRevision = 0
	e.retiredPet = pet
	packets, err := beastPetChargeArrivalPackets(pet)
	if err != nil {
		log.Printf("RakNet Beast Charge pet retirement failed object=%d: %v", pet.ObjectID, err)
		return nil
	}
	stopPackets, err := marshalZonePlayerStop(pet.ObjectID, raknet.Vector3(position))
	if err != nil {
		log.Printf("RakNet Beast Charge pet stop failed object=%d: %v", pet.ObjectID, err)
		return packets
	}
	return append(packets, stopPackets...)
}

func (e *gameplayPeerSession) retireBeastChargePets(at time.Time, excludedRuns ...*heroChargeRun) {
	if e == nil {
		return
	}
	for run := range e.beastChargeRuns {
		if len(excludedRuns) == 1 && run == excludedRuns[0] && e.heroCharge == run {
			continue
		}
		runTime := at
		if runTime.IsZero() && run.now != nil {
			runTime = run.now()
		}
		packets := run.retirePet(runTime)
		run.deferPetRetirement(packets)
		delete(e.beastChargeRuns, run)
	}
}

func (e *heroChargeRun) queuePetRetirementLocked(packets [][]byte) {
	if e == nil || e.registry == nil || len(packets) == 0 {
		return
	}
	queueBeastChargePetRetirementLocked(e.registry, e.zone, e.userID, e.generation, packets)
}

func queueBeastChargePetRetirementLocked(
	registry *gameplaySessionRegistry, originZone *zone.Zone, userID uint64, generation uint64, packets [][]byte,
) {
	isOwnerConnected := false
	for _, member := range originZone.Snapshot().Members {
		if member.UserID == userID && member.PeerGeneration == generation {
			isOwnerConnected = member.IsConnected
			break
		}
	}
	if !isOwnerConnected {
		return
	}
	for sessionKey, peerSession := range registry.sessions {
		if peerSession.zone != originZone || peerSession.isZoneTerminal() {
			continue
		}
		peerSession.queueCampaignPackets(packets)
		registry.sessions[sessionKey] = peerSession
	}
}

type beastChargePetRetirement struct {
	registry *gameplaySessionRegistry
	zone     *zone.Zone
	actor    zonecompanion.Actor
	packets  [][]byte
}

func (e beastChargePetRetirement) execute() {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	actor, isFound := e.zone.Companion().Snapshot(e.actor.ObjectID)
	if !isFound || actor.HitPoint <= 0 || actor.UserID != e.actor.UserID ||
		actor.PeerGeneration != e.actor.PeerGeneration || actor.OwnerObjectID != e.actor.OwnerObjectID ||
		actor.FollowRevision != e.actor.FollowRevision || actor.PursuitRevision != 0 ||
		actor.TargetObjectID != 0 || actor.IsFollowing || actor.Position != e.actor.Position {
		return
	}
	queueBeastChargePetRetirementLocked(e.registry, e.zone, actor.UserID, actor.PeerGeneration, e.packets)
}

// Session-only lifecycle hooks may run either inside or outside the registry
// lock. Publish their corrections from one named original-zone callback.
func (e *heroChargeRun) deferPetRetirement(packets [][]byte) {
	if len(packets) == 0 || e.registry == nil {
		return
	}
	if e.timer == nil {
		log.Printf("RakNet Beast Charge pet retirement schedule unavailable object=%d", e.retiredPet.ObjectID)
		return
	}
	publication := beastChargePetRetirement{
		registry: e.registry, zone: e.zone, actor: e.retiredPet,
		packets: clonePendingPackets(packets),
	}
	cancel, err := e.timer.Schedule(0, publication.execute)
	if err != nil {
		log.Printf("RakNet Beast Charge pet retirement schedule failed object=%d: %v", e.retiredPet.ObjectID, err)
		return
	}
	if cancel == nil {
		log.Printf("RakNet Beast Charge pet retirement cancellation unavailable object=%d", e.retiredPet.ObjectID)
	}
}
