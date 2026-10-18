package gameplay

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/zone"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zoneloot "github.com/darkspinnet/darkspin/server/zone/loot"
)

const campaignPickupContactBudget = 8

type pickupContact struct {
	objectID uint32
	start    raknet.Vector3
	end      raknet.Vector3
	isDNA    bool
}

type pickupContactState struct {
	position            raknet.Vector3
	zone                *zone.Zone
	motion              *zoneaction.Motion
	generation          uint64
	transportGeneration uint64
	objectID            uint32
	creatureIndex       uint32
	isSet               bool
	contacts            []pickupContact
}

func (e *gameplayPeerSession) isPickupSampleCurrent() bool {
	sample := e.pickupContact
	return sample.isSet && sample.zone == e.zone && sample.motion == e.playerMotion &&
		sample.generation == e.generation && sample.transportGeneration == e.transportGeneration &&
		sample.objectID == e.deployedObjectID && sample.creatureIndex == e.deployedCreatureIndex
}

func (e *gameplayPeerSession) retainPickupSample(position raknet.Vector3) {
	e.pickupContact.position = position
	e.pickupContact.zone = e.zone
	e.pickupContact.motion = e.playerMotion
	e.pickupContact.generation = e.generation
	e.pickupContact.transportGeneration = e.transportGeneration
	e.pickupContact.objectID = e.deployedObjectID
	e.pickupContact.creatureIndex = e.deployedCreatureIndex
	e.pickupContact.isSet = true
}

func (e *gameplayPeerSession) resetPickupContacts() {
	e.pickupContact = pickupContactState{}
	e.retainPickupSample(e.playerPosition)
}

func (e *gameplayPeerSession) isPickupActorAvailable() bool {
	return e.zone != nil && e.zone.Context() != nil && e.zone.Context().Err() == nil &&
		(e.binding.Mode == game.ModeChain || e.binding.Mode == game.ModeTutorial) &&
		!e.isHeroSelectionPending && e.deployedHitPoint() > 0 && isActivePartyRecipient(*e, *e)
}

func (e *gameplayPeerSession) retainPickupContactSessionLocked(registry *gameplaySessionRegistry) {
	if registry == nil {
		return
	}
	for sessionKey, member := range registry.sessions {
		if member.zone == e.zone && member.binding.UserID == e.binding.UserID && member.generation == e.generation && member.transportGeneration == e.transportGeneration {
			registry.sessions[sessionKey] = *e
			return
		}
	}
}

// Commands and polls share the same bounded backlog. Older eligible crossings
// remain ahead of newly observed contacts, so full capsules cannot starve them.
// No transport/domain reservation survives this pass.
func (e *gameplayPeerSession) collectPickupContacts(ctx context.Context, progression zoneloot.DNAGranter, registry *gameplaySessionRegistry, start raknet.Vector3, end raknet.Vector3, now time.Time) error {
	defer e.retainPickupContactSessionLocked(registry)
	if !e.isPickupActorAvailable() {
		e.pickupContact = pickupContactState{}
		return nil
	}
	if !e.isPickupSampleCurrent() {
		e.pickupContact = pickupContactState{}
	} else {
		// Polls may already have swept past the command's retained start.
		// Continue from their shared cursor so a later command cannot replay
		// a capsule contact after the exit has rearmed its full notice.
		// Teleports reset this cursor to the destination before collecting.
		start = e.pickupContact.position
	}
	contactContext, cancel := context.WithCancel(ctx)
	defer cancel()
	stopZone := context.AfterFunc(e.zone.Context(), cancel)
	defer stopPickupZoneContext(stopZone)
	contextErr := contactContext.Err()
	if contextErr != nil {
		return fmt.Errorf("pickupContext: %w", contextErr)
	}
	e.retainPickupSample(end)
	defer e.clearExitedCampaignOrbFullContacts(game.Vec3(end))
	contacts := make([]pickupContact, 0)
	for _, orb := range e.zone.Orbs().Contacts(game.Vec3(start), game.Vec3(end), campaignOrbPickupRadius, now) {
		if orb.Request.Kind == sim.ResurrectionOrbDrop {
			continue
		}
		contacts = append(contacts, pickupContact{objectID: orb.ObjectID, start: start, end: end})
	}
	if progression != nil {
		for _, dna := range e.zone.DNA().Contacts(game.Vec3(start), game.Vec3(end), now, campaignDNAPickupRadius) {
			contacts = append(contacts, pickupContact{objectID: dna.ObjectID, start: start, end: end, isDNA: true})
		}
	}
	sort.Slice(contacts, func(left int, right int) bool { return contacts[left].objectID < contacts[right].objectID })
	contactsByObjectIDs := make(map[uint32]struct{}, len(e.pickupContact.contacts))
	for _, contact := range e.pickupContact.contacts {
		contactsByObjectIDs[contact.objectID] = struct{}{}
	}
	for _, contact := range contacts {
		if _, isFound := contactsByObjectIDs[contact.objectID]; isFound {
			continue
		}
		e.pickupContact.contacts = append(e.pickupContact.contacts, contact)
		contactsByObjectIDs[contact.objectID] = struct{}{}
	}
	for count := 0; count < campaignPickupContactBudget && len(e.pickupContact.contacts) != 0; count++ {
		contextErr = contactContext.Err()
		if contextErr != nil {
			return fmt.Errorf("pickupGrantContext: %w", contextErr)
		}
		contact := e.pickupContact.contacts[0]
		var packets [][]byte
		var err error
		if contact.isDNA {
			if progression == nil {
				return nil
			}
			packets, err = e.collectCampaignDNA(contactContext, progression, registry, contact.start, contact.end, now, contact.objectID)
		} else {
			packets, err = e.collectCampaignOrbs(registry, contact.start, contact.end, now, contact.objectID)
		}
		if err != nil {
			return fmt.Errorf("pickupContact[%d]: %w", contact.objectID, err)
		}
		if len(packets) != 0 {
			// Committed collectors normally retain their output themselves.
			e.queuePickupNoticeLocked(registry, packets)
		}
		e.pickupContact.contacts = e.pickupContact.contacts[1:]
	}
	return nil
}

func stopPickupZoneContext(stop func() bool) {
	if !stop() {
		// A dispatched cancellation only closes this pass's derived context.
	}
}

func (e gameplayPendingRuntime) pollPickupContactsLocked(ctx context.Context, packet raknet.Packet, peerSession *gameplayPeerSession, now time.Time) {
	if peerSession.transportGeneration != packet.TransportGeneration {
		return
	}
	if !peerSession.isPickupActorAvailable() {
		peerSession.pickupContact = pickupContactState{}
		return
	}
	current := peerSession.playerPosition
	if peerSession.playerMotion != nil {
		position, err := peerSession.playerMotion.SamplePosition(now)
		if err != nil {
			if e.logger != nil {
				e.logger.Printf("RakNet pickup contact sample deferred: %v", err)
			}
			return
		}
		current = toRakNetPosition(position)
	}
	previous := current
	if peerSession.isPickupSampleCurrent() {
		previous = peerSession.pickupContact.position
	}
	err := peerSession.collectPickupContacts(ctx, e.action.movement.campaign.encounter.progression, e.registry, previous, current, now)
	if err != nil && e.logger != nil {
		e.logger.Printf("RakNet pickup contact pass deferred object=%d: %v", peerSession.deployedObjectID, err)
	}
}
