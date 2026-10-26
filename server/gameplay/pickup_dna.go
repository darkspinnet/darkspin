package gameplay

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/zone"
	zoneloot "github.com/darkspinnet/darkspin/server/zone/loot"
	lootraknet "github.com/darkspinnet/darkspin/server/zone/loot/raknet103"
)

const campaignDNACollectionLimit = 512

type campaignDNAGrant struct {
	userID uint64
	total  uint32
}

type campaignDNACollection struct {
	world       *zone.Zone
	userID      uint64
	reservation *zoneloot.DNAReservation
	progression zoneloot.DNAGranter
	packets     [][]byte
	userIDs     []uint64
}

// Called with registry.mutex held. Reserve contact now, but never wait for an
// account mutation or a database flush while the gameplay state is locked.
func (e *gameplayPeerSession) collectCampaignDNA(
	ctx context.Context, progression zoneloot.DNAGranter, registry *gameplaySessionRegistry,
	start raknet.Vector3, end raknet.Vector3, now time.Time, objectIDs ...uint32,
) ([][]byte, error) {
	if e == nil || progression == nil || e.zone == nil || e.zone.DNA() == nil {
		return nil, nil
	}
	if registry == nil || registry.isClosed {
		return nil, errors.New("DNA collection registry unavailable")
	}
	err := ctx.Err()
	if err != nil {
		return nil, fmt.Errorf("dnaContext: %w", err)
	}
	if len(registry.dnaCollections) >= campaignDNACollectionLimit {
		return nil, errors.New("DNA collection queue full")
	}
	objectID := uint32(0)
	if len(objectIDs) == 1 {
		objectID = objectIDs[0]
	}
	reservation, isReserved := e.zone.DNA().ReserveContactID(
		objectID, game.Vec3(start), game.Vec3(end), now, campaignDNAPickupRadius,
	)
	if !isReserved {
		// Another accepted contact owns persistence. Do not let that pending
		// pickup block the bounded backlog's unrelated capsule contacts.
		return nil, nil
	}
	pickup := reservation.Pickup()
	packets, err := lootraknet.MarshalDNACollection(lootraknet.DNACollectionRequest{
		Slot: uint8(e.binding.Slot), Amount: pickup.Amount,
		ActorObjectID: e.deployedObjectID, PickupObjectID: pickup.ObjectID,
		Position: raknet.Vector3(pickup.Position),
	})
	if err != nil {
		if !reservation.Release() {
			return nil, fmt.Errorf("dnaMarshalRelease: %w", errors.Join(err, errors.New("DNA reservation missing")))
		}
		return nil, fmt.Errorf("dnaMarshal: %w", err)
	}
	userIDs := []uint64{e.binding.UserID}
	for _, member := range registry.sessions {
		if isActiveCoopPickupAlly(member, *e) {
			userIDs = append(userIDs, member.binding.UserID)
		}
	}
	sort.Slice(userIDs[1:], func(left int, right int) bool { return userIDs[left+1] < userIDs[right+1] })
	collection := &campaignDNACollection{
		world: e.zone, userID: e.binding.UserID, reservation: reservation,
		progression: progression, packets: packets, userIDs: userIDs,
	}
	if !registry.isDNACollectionRunning {
		if !registry.beginRequestLocked() {
			collection.release(registry)
			return nil, errors.New("DNA collection registry closed")
		}
		registry.isDNACollectionRunning = true
		// Register before launching so shutdown waits for admitted grants.
		registry.dnaCollections = append(registry.dnaCollections, collection)
		go registry.runDNACollections()
		return nil, nil
	}
	registry.dnaCollections = append(registry.dnaCollections, collection)
	return nil, nil
}

func (e *gameplaySessionRegistry) runDNACollections() {
	defer e.endRequest()
	for {
		e.mutex.Lock()
		if len(e.dnaCollections) == 0 {
			e.isDNACollectionRunning = false
			e.mutex.Unlock()
			return
		}
		collection := e.dnaCollections[0]
		e.dnaCollections[0] = nil
		e.dnaCollections = e.dnaCollections[1:]
		isClosed := e.isClosed
		e.mutex.Unlock()
		if isClosed {
			collection.release(e)
			continue
		}
		grants, err := collection.persist(e)
		if err != nil {
			collection.release(e)
			if e.logger != nil {
				e.logger.Printf("RakNet DNA collection deferred object=%d: %v", collection.reservation.Pickup().ObjectID, err)
			}
			continue
		}
		e.mutex.Lock()
		collection.commitLocked(e, grants)
		e.mutex.Unlock()
	}
}

func (e *campaignDNACollection) release(registry *gameplaySessionRegistry) {
	if !e.reservation.Release() && registry.logger != nil {
		registry.logger.Printf("RakNet DNA collection reservation release missing object=%d", e.reservation.Pickup().ObjectID)
	}
}

func (e *campaignDNACollection) persist(registry *gameplaySessionRegistry) ([]campaignDNAGrant, error) {
	ctx, cancel := context.WithTimeout(e.world.Context(), 5*time.Second)
	defer cancel()
	grants := make([]campaignDNAGrant, 0, len(e.userIDs))
	seenUserIDs := make(map[uint64]bool, len(e.userIDs))
	for _, userID := range e.userIDs {
		if seenUserIDs[userID] {
			continue
		}
		seenUserIDs[userID] = true
		total, err := e.progression.GrantDNA(ctx, int64(userID), e.reservation.Pickup().Amount)
		if err != nil {
			if userID == e.userID {
				return nil, fmt.Errorf("dnaGrant: %w", err)
			}
			if registry.logger != nil {
				registry.logger.Printf("RakNet co-op DNA grant failed user=%d: %v", userID, err)
			}
			continue
		}
		grants = append(grants, campaignDNAGrant{userID: userID, total: total})
	}
	return grants, nil
}

func (e *campaignDNACollection) commitLocked(registry *gameplaySessionRegistry, grants []campaignDNAGrant) {
	if !e.reservation.Commit() && registry.logger != nil {
		// Accepted durable grants must never be released for another attempt.
		registry.logger.Printf("RakNet committed DNA reservation missing object=%d", e.reservation.Pickup().ObjectID)
	}
	worldPackets := gameplayPeerPresentationPackets(e.packets)
	for sessionKey, member := range registry.sessions {
		e.publishLocked(&member, grants, worldPackets)
		registry.sessions[sessionKey] = member
	}
	for _, retained := range registry.retainedSessions {
		if retained != nil {
			e.publishLocked(&retained.peerSession, grants, worldPackets)
		}
	}
}

func (e *campaignDNACollection) publishLocked(member *gameplayPeerSession, grants []campaignDNAGrant, worldPackets [][]byte) {
	if member.zone != e.world {
		return
	}
	for _, grant := range grants {
		if member.binding.UserID != grant.userID {
			continue
		}
		member.binding.DNA = grant.total
		member.queueCampaignPackets([][]byte{lootraknet.DNAAccountUpdate(uint8(member.binding.Slot), grant.total)})
		break
	}
	if member.binding.UserID == e.userID {
		member.queueCampaignPackets(e.packets[1:])
		return
	}
	member.queueCampaignPackets(worldPackets)
}
