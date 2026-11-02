package gameplay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	actionraknet "github.com/darkspinnet/darkspin/server/zone/action/raknet103"
)

// campaignPendingBasic owns a native repeat until the preceding
// attack has released. Replacing this pointer coalesces repeats without
// canceling the preceding attack's remaining hits or borrowing its sync token.
type campaignPendingBasic struct {
	runtime             campaignAbilityCommandRuntime
	packet              raknet.Packet
	command             raknet.ActionCommandData
	sessionKey          string
	generation          uint64
	transportGeneration uint64
	heldGeneration      uint64
	startedAt           time.Time
	expiresAt           time.Time
}

type campaignPendingBasicKey struct{}

// Every native predicted request owns its sync token, including a repeat that
// has not reached its hit/release admission yet. Cancellation must answer it
// before invalidating its held generation, or the client can retain a busy
// prediction and stop submitting later abilities.
func (e *gameplayPeerSession) cancelPendingBasic() error {
	if e == nil || e.pendingBasic == nil {
		return nil
	}
	packet, err := actionraknet.Reject(e.pendingBasic.command)
	if err != nil {
		return fmt.Errorf("pendingCancelMarshal: %w", err)
	}
	e.pendingBasic = nil
	e.queuePackets([][]byte{packet})
	return nil
}

func (e *campaignPendingBasic) isCurrent(session gameplayPeerSession, isFound bool) bool {
	return isFound && session.pendingBasic == e &&
		session.generation == e.generation &&
		session.transportGeneration == e.transportGeneration &&
		session.deployedObjectID == e.command.Common.ObjectID &&
		session.basicSequenceSession().HeldGeneration() == e.heldGeneration &&
		!session.isHeroSelectionPending && !session.isZoneTerminal() &&
		session.deployedHitPoint() > 0
}

func pendingBasicDelay(session gameplayPeerSession, now time.Time) time.Duration {
	end := session.basicSequenceSession().CooldownEnd()
	releaseEnd := session.abilityReleaseSession().Snapshot().End
	if releaseEnd.After(end) {
		end = releaseEnd
	}
	if session.attackPose.expiresAt.After(end) {
		end = session.attackPose.expiresAt
	}
	return max(time.Millisecond, end.Sub(now)+time.Millisecond)
}

func (e *campaignPendingBasic) schedule(delay time.Duration) error {
	// The resumed outer action owns peer presentation and its own guarded
	// schedules. Wrapping this admission again would publish its result twice.
	producers := []raknet.ScheduledPacketProducer{{Delay: delay, Produce: e.produce}}
	var cancel raknet.CancelSchedule
	var err error
	if e.packet.ScheduleGroupResult != nil {
		cancel, err = e.packet.ScheduleGroupResult(producers, e.fail)
	} else if e.packet.ScheduleGroup != nil {
		cancel, err = e.packet.ScheduleGroup(producers)
	} else {
		return errors.New("pending basic schedule unavailable")
	}
	if err != nil {
		return fmt.Errorf("pendingSchedule: %w", err)
	}
	if cancel == nil {
		return errors.New("pending basic schedule returned no cancellation")
	}
	return nil
}

func (e *campaignPendingBasic) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	session, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(session, isFound) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	now := e.runtime.now()
	if now.Before(e.expiresAt) && (session.basicAttack != nil ||
		!session.basicSequenceSession().IsReady(now) || !session.isAbilityReleaseReady(now)) {
		delay := pendingBasicDelay(session, now)
		e.runtime.registry.mutex.Unlock()
		err := e.schedule(delay)
		if err != nil {
			return e.reject(err)
		}
		return nil, nil
	}
	e.runtime.registry.mutex.Unlock()
	if !now.Before(e.expiresAt) {
		return e.reject(errors.New("pending basic release timed out"))
	}
	packet := e.packet.Autonomous()
	packet.SourceTime += uint64(max(time.Duration(0), now.Sub(e.startedAt)) / time.Millisecond)
	command := e.command
	ctx := context.WithValue(context.Background(), campaignPendingBasicKey{}, e)
	if e.runtime.actionRuntime == nil {
		return e.reject(errors.New("pending basic action runtime unavailable"))
	}
	packets, err := e.runtime.actionRuntime.handleCommand(ctx, packet, command, false)
	if err != nil {
		return packets, fmt.Errorf("pendingAdmission: %w", err)
	}
	return packets, nil
}

func (e *campaignPendingBasic) reject(cause error) ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	session, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(session, isFound) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	session.pendingBasic = nil
	e.runtime.registry.sessions[e.sessionKey] = session
	e.runtime.registry.mutex.Unlock()
	e.runtime.logger.Printf("RakNet pending basic stopped remote=%s sync=%d: %v",
		e.sessionKey, e.command.Common.Unknown[0], cause)
	packet, err := actionraknet.Reject(e.command)
	if err != nil {
		return nil, fmt.Errorf("pendingReject: %w", err)
	}
	return [][]byte{packet}, nil
}

func (e *campaignPendingBasic) fail(cause error) {
	packet, err := actionraknet.Reject(e.command)
	if err != nil {
		e.runtime.logger.Printf("RakNet pending basic failure cleanup: %v", err)
		return
	}
	e.runtime.registry.mutex.Lock()
	session, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(session, isFound) {
		e.runtime.registry.mutex.Unlock()
		return
	}
	// Retire and publish atomically so a replacement intent cannot slip
	// between failure cleanup and queuing its native response.
	session.pendingBasic = nil
	session.queuePackets([][]byte{packet})
	e.runtime.registry.sessions[e.sessionKey] = session
	e.runtime.registry.mutex.Unlock()
	e.runtime.logger.Printf("RakNet pending basic stopped remote=%s sync=%d: %v",
		e.sessionKey, e.command.Common.Unknown[0], cause)
}
