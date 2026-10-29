package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/zone"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	companionraknet "github.com/darkspinnet/darkspin/server/zone/companion/raknet103"
)

// One registered SpoffitCrystal instance owns one attempted spawn, then waits
// forever. There is no respawn, combat target, Sage pet limit or owner mirror.
type spoffitRun struct {
	runtime            campaignNPCActionRuntime
	world              *zone.Zone
	profile            game.CampaignNPCProfile
	packet             raknet.Packet
	userID             uint64
	generation         uint64
	ownerObjectID      uint32
	objectID           uint32
	instanceID         uint32
	footprintRadius    float32
	sourceTime         uint64
	startedAt          time.Time
	spawnReleaseAt     time.Time
	nextAbilityAt      time.Time
	burrowEndsAt       time.Time
	missingSince       time.Time
	follow             *zonecompanion.Follow
	lastOwnerPosition  game.Vec3
	isAbilityFollowing bool
	isCreatorPublished bool
	isBirthPublished   bool
	isRemovalPublished bool
	isRemovalProducing bool
	isBurrowing        bool
	isStopped          bool
	creatorPackets     [][]byte
	birthPackets       [][]byte
	removalPackets     [][]byte
	birthCancel        func()
	pollCancel         func()
	requestID          string
	removalRequestID   string
}

func (e *spoffitRun) ownerLocked() (gameplayPeerSession, bool) {
	for _, member := range e.runtime.registry.sessions {
		if member.zone == e.world && member.binding.UserID == e.userID && member.generation == e.generation && member.deployedObjectID == e.ownerObjectID {
			return member, true
		}
	}
	for _, retained := range e.runtime.registry.retainedSessions {
		if retained == nil {
			continue
		}
		member := retained.peerSession
		if member.zone == e.world && member.binding.UserID == e.userID && member.generation == e.generation && member.deployedObjectID == e.ownerObjectID {
			return member, true
		}
	}
	return gameplayPeerSession{}, false
}

func (e *spoffitRun) ownerPosition(member gameplayPeerSession, now time.Time) (game.Vec3, error) {
	position := game.Vec3(member.playerPosition)
	if member.playerMotion != nil {
		sampled, err := member.playerMotion.SamplePosition(now)
		if err != nil {
			return game.Vec3{}, fmt.Errorf("spoffitOwnerMotion: %w", err)
		}
		position = game.Vec3(sampled)
	}
	return position, nil
}

func (e *spoffitRun) queueLocked(packets [][]byte, isOwnerIncluded bool) {
	for key, member := range e.runtime.registry.sessions {
		if member.zone != e.world || member.isZoneTerminal() ||
			(!isOwnerIncluded && member.binding.UserID == e.userID && member.generation == e.generation) {
			continue
		}
		member.queueCampaignPackets(packets)
		e.runtime.registry.sessions[key] = member
	}
}

func (e *spoffitRun) schedulePollLocked() error {
	if e.isStopped || e.pollCancel != nil {
		return nil
	}
	cancel, err := e.runtime.timer.Schedule(sagePassivePollDelay, e.poll)
	if err != nil {
		return fmt.Errorf("spoffitPollTimer: %w", err)
	}
	if cancel == nil {
		return errors.New("Spoffit poll cancellation unavailable")
	}
	e.pollCancel = cancel
	return nil
}

func (e *spoffitRun) poll() {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	e.pollCancel = nil
	if e.isStopped {
		return
	}
	now := e.runtime.now()
	packets, err := e.advanceLocked(now)
	e.queueLocked(packets, true)
	if err != nil {
		deleted, deleteErr := e.deleteLocked()
		e.queueLocked(deleted, true)
		e.runtime.registry.recordSpoffitFailureLocked(e.requestID, e.removalRequestID, err)
		e.runtime.logger.Printf("Spoffit world update failed owner=%d child=%d: %v; cleanup=%v", e.ownerObjectID, e.objectID, err, deleteErr)
		return
	}
	if !e.isStopped {
		scheduleErr := e.schedulePollLocked()
		if scheduleErr != nil {
			deleted, deleteErr := e.deleteLocked()
			e.queueLocked(deleted, true)
			e.runtime.registry.recordSpoffitFailureLocked(e.requestID, e.removalRequestID, scheduleErr)
			e.runtime.logger.Printf("Spoffit poll scheduling failed owner=%d: %v; cleanup=%v", e.ownerObjectID, scheduleErr, deleteErr)
		}
	}
}

func (e *spoffitRun) advanceLocked(now time.Time) ([][]byte, error) {
	if e.runtime.registry.isClosed || !e.world.IsActive() {
		return e.deleteLocked()
	}
	if e.isBurrowing {
		if e.removalRequestID != "" && !e.isRemovalPublished {
			return nil, nil
		}
		if !now.Before(e.burrowEndsAt) {
			return e.deleteLocked()
		}
		return nil, nil
	}
	member, isOwner := e.ownerLocked()
	if !isOwner {
		if e.missingSince.IsZero() {
			e.missingSince = now
		}
		if now.Sub(e.missingSince) < gameplayRejoinGrace {
			return nil, nil
		}
		return e.beginBurrowLocked(now)
	}
	e.missingSince = time.Time{}
	if !e.isBirthPublished {
		// Before the first birth commit, an old transport cannot hand off a
		// half-published constructor. Retire it explicitly; never respawn or
		// replay that unknown operation on a replacement transport.
		if member.transportGeneration != e.packet.TransportGeneration || member.isRejoinPending {
			return e.deleteLocked()
		}
		return nil, nil
	}
	actor, isFound := e.world.Companion().Snapshot(e.objectID)
	if !isFound || actor.HitPoint <= 0 {
		// The creator never retries after its one successful creation.
		return nil, nil
	}
	if now.Before(e.spawnReleaseAt) {
		return nil, nil
	}
	if actor.IsAbilityControlled && actor.AbilityAnimation == sageSpawnAnimation {
		isReleased := e.world.Companion().ReleaseRecall(e.objectID, sageSpawnAnimation)
		if !isReleased {
			return nil, nil
		}
	}
	ownerPosition, err := e.ownerPosition(member, now)
	if err != nil {
		return nil, fmt.Errorf("spoffitFollowOwner: %w", err)
	}
	packets := make([][]byte, 0)
	if e.follow != nil {
		remaining, isCurrent := e.world.Companion().FollowRemaining(e.objectID, e.follow.Revision, now)
		if isCurrent && remaining <= 0 {
			arrivals, arrivalErr := companionraknet.FollowArrival(*e.follow)
			if arrivalErr != nil {
				return nil, fmt.Errorf("spoffitArrival: %w", arrivalErr)
			}
			isReleased := e.world.Companion().ReleaseFollow(e.objectID, e.follow.Revision, e.follow.Destination)
			if isReleased {
				packets = append(packets, arrivals...)
			}
			e.follow = nil
			e.isAbilityFollowing = false
		} else if isCurrent && ownerPosition.Sub(e.lastOwnerPosition).Length() <= 0.05 && (e.isAbilityFollowing || now.Before(e.nextAbilityAt)) {
			return nil, nil
		} else if !isCurrent {
			e.follow = nil
			e.isAbilityFollowing = false
		}
	}
	distance := float32(2) // Behavior70 FollowAsPet between ordinary actions.
	if !now.Before(e.nextAbilityAt) {
		e.nextAbilityAt = now.Add(5 * time.Second)
		e.isAbilityFollowing = true // Ability975's blocking MoveTowardObject.
	}
	if e.isAbilityFollowing {
		distance = 5
	}
	plans, err := e.world.Companion().FollowActor(e.userID, e.generation, e.ownerObjectID, e.objectID, ownerPosition, distance, e.profile.BaseCombatSpeed, now)
	if err != nil {
		return packets, fmt.Errorf("spoffitFollow: %w", err)
	}
	if len(plans) == 0 {
		e.isAbilityFollowing = false
		return packets, nil
	}
	encoded, err := companionraknet.Follow(plans)
	if err != nil {
		return packets, fmt.Errorf("spoffitFollowMarshal: %w", err)
	}
	plan := plans[0]
	e.follow = &plan
	e.lastOwnerPosition = ownerPosition
	return append(packets, encoded...), nil
}
