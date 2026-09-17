package gameplay

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	actionraknet "github.com/darkspinnet/darkspin/server/zone/action/raknet103"
)

const (
	actionLeaseFallbackDuration = 10 * time.Second
	actionLeaseMinimumDuration  = time.Second
	actionLeaseMaximumDuration  = 30 * time.Second
	actionLeaseGraceDuration    = time.Second
)

type gameplayActionLeaseKey struct {
	sessionKey          string
	memberKey           gameplayMemberKey
	traceID             uint64
	zoneGeneration      uint64
	transportGeneration uint64
	actionGeneration    uint64
	syncStamp           uint8
}

type gameplayActionLease struct {
	command   raknet.ActionCommandData
	expiresAt time.Time
	cancel    raknet.CancelSchedule
	isPursuit bool
}

type gameplayActionLeaseExpiry struct {
	runtime gameplayActionRuntime
	key     gameplayActionLeaseKey
}

type gameplayActionScheduleGuard struct {
	runtime             gameplayActionRuntime
	key                 gameplayActionLeaseKey
	command             raknet.ActionCommandData
	scheduleSet         *gameplayActionScheduleSet
	scheduleGroup       func([]raknet.ScheduledPacketProducer) (raknet.CancelSchedule, error)
	scheduleGroupResult func([]raknet.ScheduledPacketProducer, func(error)) (raknet.CancelSchedule, error)
}

type gameplayActionScheduleSet struct {
	mutex      sync.Mutex
	schedules  []*gameplayActionSchedule
	isSealed   bool
	isCanceled bool
}

type gameplayActionSchedule struct {
	cancel  raknet.CancelSchedule
	cleanup *gameplayActionScheduleCleanup
}

type gameplayActionScheduleCleanup struct {
	once sync.Once
	next func(error)
}

func (e *gameplayActionScheduleCleanup) run(scheduleErr error) {
	if e == nil || e.next == nil {
		return
	}
	e.once.Do(func() {
		e.next(scheduleErr)
	})
}

func (e *gameplayActionScheduleSet) add(
	cancel raknet.CancelSchedule,
	cleanup *gameplayActionScheduleCleanup,
) {
	if e == nil || cancel == nil {
		return
	}
	e.mutex.Lock()
	isCanceled := e.isCanceled
	if e.isSealed {
		e.mutex.Unlock()
		if isCanceled {
			cancel()
			cleanup.run(errors.New("action schedule canceled after admission"))
		}
		return
	}
	e.schedules = append(e.schedules, &gameplayActionSchedule{
		cancel: cancel, cleanup: cleanup,
	})
	e.mutex.Unlock()
}

func (e *gameplayActionScheduleSet) seal() {
	if e == nil {
		return
	}
	e.mutex.Lock()
	e.schedules = nil
	e.isSealed = true
	e.mutex.Unlock()
}

func (e *gameplayActionScheduleSet) cancelAll() bool {
	if e == nil {
		return false
	}
	e.mutex.Lock()
	schedules := e.schedules
	e.schedules = nil
	e.isSealed = true
	e.isCanceled = true
	e.mutex.Unlock()
	scheduleErr := errors.New("action schedule canceled during admission")
	for _, current := range schedules {
		current.cancel()
		current.cleanup.run(scheduleErr)
	}
	return len(schedules) > 0
}

type gameplayActionScheduleFailure struct {
	runtime             gameplayActionRuntime
	key                 gameplayActionLeaseKey
	command             raknet.ActionCommandData
	scheduleGroupResult func(
		[]raknet.ScheduledPacketProducer, func(error),
	) (raknet.CancelSchedule, error)
	cleanup *gameplayActionScheduleCleanup
}

func (e gameplayActionScheduleFailure) handle(scheduleErr error) {
	e.cleanup.run(scheduleErr)
	isReleased := e.runtime.releaseFailedActionAdmission(e.key, scheduleErr)
	if !isReleased || e.scheduleGroupResult == nil {
		return
	}
	err := e.runtime.scheduleFailedActionRejection(
		e.key, e.command, e.scheduleGroupResult,
	)
	if err != nil {
		e.runtime.logger.Printf(
			"RakNet failed action rejection deferred to watchdog trace=%d remote=%s generation=%d sync=%d: %v",
			e.key.traceID, e.key.sessionKey, e.key.transportGeneration,
			e.key.syncStamp, err,
		)
	}
}

type staticActionPackets struct {
	packets [][]byte
}

func (e staticActionPackets) produce() ([][]byte, error) {
	return e.packets, nil
}

func (e gameplayActionScheduleGuard) schedule(
	delay time.Duration, packets [][]byte,
) error {
	producer := staticActionPackets{packets: packets}
	_, err := e.groupResult(
		[]raknet.ScheduledPacketProducer{{
			Delay: delay, Produce: producer.produce,
		}}, nil,
	)
	return err
}

func (e gameplayActionScheduleGuard) scheduleFunc(
	delay time.Duration, produce func() ([][]byte, error),
) error {
	_, err := e.groupResult(
		[]raknet.ScheduledPacketProducer{{
			Delay: delay, Produce: produce,
		}}, nil,
	)
	return err
}

func (e gameplayActionScheduleGuard) group(
	producers []raknet.ScheduledPacketProducer,
) (raknet.CancelSchedule, error) {
	if e.scheduleGroupResult != nil {
		return e.groupResult(producers, nil)
	}
	if e.scheduleGroup == nil {
		return nil, errors.New("action schedule unavailable")
	}
	cancel, err := e.scheduleGroup(
		e.runtime.observeActionProducers(e.key, producers),
	)
	if err == nil {
		e.scheduleSet.add(cancel, nil)
	}
	return cancel, err
}

func (e gameplayActionScheduleGuard) groupResult(
	producers []raknet.ScheduledPacketProducer, onFailure func(error),
) (raknet.CancelSchedule, error) {
	cleanup := &gameplayActionScheduleCleanup{next: onFailure}
	if e.scheduleGroupResult == nil {
		if e.scheduleGroup == nil {
			return nil, errors.New("action result schedule unavailable")
		}
		cancel, err := e.scheduleGroup(
			e.runtime.observeActionProducers(e.key, producers),
		)
		if err == nil {
			e.scheduleSet.add(cancel, cleanup)
		}
		return cancel, err
	}
	failure := gameplayActionScheduleFailure{
		runtime: e.runtime, key: e.key, command: e.command,
		scheduleGroupResult: e.scheduleGroupResult,
		cleanup:             cleanup,
	}
	cancel, err := e.scheduleGroupResult(
		e.runtime.observeActionProducers(e.key, producers), failure.handle,
	)
	if err == nil {
		e.scheduleSet.add(cancel, cleanup)
	}
	return cancel, err
}

func (r gameplayActionRuntime) releaseFailedActionAdmission(
	key gameplayActionLeaseKey, scheduleErr error,
) bool {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[key.sessionKey]
	_, isLeased := r.registry.actionLeases[key]
	_, isAdmitting := r.registry.actionAdmissions[key]
	isCurrent := isFound && (isLeased || isAdmitting) &&
		isActionSessionCurrent(peerSession, key)
	if !isCurrent {
		r.registry.mutex.Unlock()
		return false
	}
	pursuit := peerSession.campaignPlayerPursuitSession().Snapshot()
	interruptedBasic := peerSession.resetFailedActionAdmission(
		r.switcher.modifierPool, r.switcher.effectPool,
	)
	if pursuit.IsActive {
		err := peerSession.stopPlayerMovement(r.now())
		if err != nil {
			r.logger.Printf(
				"RakNet failed action movement cleanup skipped remote=%s generation=%d: %v",
				key.sessionKey, key.transportGeneration, err,
			)
		}
	}
	r.registry.sessions[key.sessionKey] = peerSession
	r.registry.mutex.Unlock()
	if interruptedBasic != nil {
		interruptedBasic.Stop()
	}
	r.logger.Printf(
		"RakNet failed action admission released trace=%d remote=%s generation=%d sync=%d: %v",
		key.traceID, key.sessionKey, key.transportGeneration, key.syncStamp, scheduleErr,
	)
	return true
}

func (r gameplayActionRuntime) recoverRejectedActionAdmission(
	key gameplayActionLeaseKey, cause error,
) [][]byte {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[key.sessionKey]
	if !isFound || !isActionSessionCurrent(peerSession, key) {
		r.registry.mutex.Unlock()
		return nil
	}
	interruptedBasic := peerSession.resetFailedActionAdmission(
		r.switcher.modifierPool, r.switcher.effectPool,
	)
	movementErr := peerSession.stopPlayerMovement(r.now())
	r.registry.sessions[key.sessionKey] = peerSession
	r.registry.mutex.Unlock()
	if interruptedBasic != nil {
		interruptedBasic.Stop()
	}
	if movementErr != nil {
		r.logger.Printf(
			"RakNet rejected action movement cleanup skipped remote=%s generation=%d: %v",
			key.sessionKey, key.transportGeneration, movementErr,
		)
	}
	r.logger.Printf(
		"RakNet rejected action admission recovered trace=%d remote=%s generation=%d sync=%d: %v",
		key.traceID, key.sessionKey, key.transportGeneration, key.syncStamp, cause,
	)
	return r.marshalActionRecoveryBaseline(key, "rejected")
}

func (r gameplayActionRuntime) marshalActionRecoveryBaseline(
	key gameplayActionLeaseKey, reason string,
) [][]byte {
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[key.sessionKey]
	isCurrent := isFound && isActionSessionCurrent(peerSession, key)
	r.registry.mutex.RUnlock()
	if !isCurrent || peerSession.isHeroSelectionPending ||
		peerSession.deployedObjectID == 0 || peerSession.deployedHitPoint() <= 0 {
		return nil
	}
	packets, err := marshalZonePlayerStop(
		peerSession.deployedObjectID, peerSession.playerPosition,
	)
	if err != nil {
		r.logger.Printf(
			"RakNet %s action movement baseline skipped remote=%s generation=%d: %v",
			reason, key.sessionKey, key.transportGeneration, err,
		)
		packets = nil
	}
	baselinePackets, err := peerSession.marshalDeveloperResetBaseline()
	if err != nil {
		r.logger.Printf(
			"RakNet %s action hero baseline skipped remote=%s generation=%d: %v",
			reason, key.sessionKey, key.transportGeneration, err,
		)
		return packets
	}
	return append(packets, baselinePackets...)
}

func (r gameplayActionRuntime) scheduleFailedActionRejection(
	key gameplayActionLeaseKey, command raknet.ActionCommandData,
	scheduleGroupResult func(
		[]raknet.ScheduledPacketProducer, func(error),
	) (raknet.CancelSchedule, error),
) error {
	packet, err := actionraknet.Reject(command)
	if err != nil {
		return fmt.Errorf("failureRejectMarshal: %w", err)
	}
	packets := [][]byte{packet}
	packets = append(packets, r.marshalActionRecoveryBaseline(key, "failed")...)
	producer := staticActionPackets{packets: packets}
	observer := &gameplayActionProducerObserver{
		runtime: r, key: key, produceFunc: producer.produce,
		isRecoveryRequired: true,
	}
	observed := raknet.ScheduledPacketProducer{
		Produce: observer.produce, AfterCommit: observer.commit,
	}
	_, err = scheduleGroupResult([]raknet.ScheduledPacketProducer{observed}, nil)
	if err != nil {
		return fmt.Errorf("failureRejectSchedule: %w", err)
	}
	return nil
}

type gameplayActionProducerObserver struct {
	runtime            gameplayActionRuntime
	key                gameplayActionLeaseKey
	produceFunc        func() ([][]byte, error)
	commitFunc         func()
	producedPackets    [][]byte
	isProduced         bool
	isRecoveryRequired bool
}

func (e *gameplayActionProducerObserver) produce() ([][]byte, error) {
	isCurrent := e.runtime.isActionSessionCurrent(e.key)
	if e.isRecoveryRequired {
		isCurrent = e.runtime.isActionRecoveryCurrent(e.key)
	}
	if !isCurrent {
		return nil, nil
	}
	packets, err := e.produceFunc()
	if err != nil {
		return packets, err
	}
	e.producedPackets = packets
	e.isProduced = true
	return packets, nil
}

func (e *gameplayActionProducerObserver) commit() {
	if !e.isProduced {
		return
	}
	if e.commitFunc != nil {
		e.commitFunc()
	}
	e.runtime.observeActionResponses(e.key, e.producedPackets)
}

func (r gameplayActionRuntime) protectScheduledResponses(
	packet raknet.Packet, key gameplayActionLeaseKey,
	command raknet.ActionCommandData, scheduleSet *gameplayActionScheduleSet,
) raknet.Packet {
	originalSchedule := packet.Schedule
	originalScheduleFunc := packet.ScheduleFunc
	originalScheduleGroup := packet.ScheduleGroup
	originalScheduleGroupResult := packet.ScheduleGroupResult
	guard := gameplayActionScheduleGuard{
		runtime: r, key: key, command: command,
		scheduleSet:         scheduleSet,
		scheduleGroup:       originalScheduleGroup,
		scheduleGroupResult: originalScheduleGroupResult,
	}
	if originalScheduleGroupResult != nil {
		packet.Schedule = guard.schedule
		packet.ScheduleFunc = guard.scheduleFunc
	} else if originalScheduleFunc != nil {
		packet.Schedule = originalSchedule
		packet.ScheduleFunc = originalScheduleFunc
	} else {
		packet.Schedule = originalSchedule
	}
	if originalScheduleGroup != nil {
		packet.ScheduleGroup = guard.group
	}
	if originalScheduleGroupResult != nil {
		packet.ScheduleGroupResult = guard.groupResult
	}
	return packet
}

func (r gameplayActionRuntime) observeActionProducers(
	key gameplayActionLeaseKey,
	producers []raknet.ScheduledPacketProducer,
) []raknet.ScheduledPacketProducer {
	observed := make([]raknet.ScheduledPacketProducer, len(producers))
	for index, producer := range producers {
		observed[index] = r.observeActionProducerCommit(
			key, producer,
		)
	}
	return observed
}

func (r gameplayActionRuntime) observeActionProducerCommit(
	key gameplayActionLeaseKey,
	producer raknet.ScheduledPacketProducer,
) raknet.ScheduledPacketProducer {
	if producer.Produce == nil {
		return producer
	}
	observer := &gameplayActionProducerObserver{
		runtime: r, key: key, produceFunc: producer.Produce,
		commitFunc: producer.AfterCommit,
	}
	producer.Produce = observer.produce
	producer.AfterCommit = observer.commit
	return producer
}

func (r gameplayActionRuntime) isActionRecoveryCurrent(
	key gameplayActionLeaseKey,
) bool {
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[key.sessionKey]
	_, isAdmitting := r.registry.actionAdmissions[key]
	_, isLeased := r.registry.actionLeases[key]
	isCurrent := isFound && (isAdmitting || isLeased) &&
		isActionSessionCurrent(peerSession, key)
	r.registry.mutex.RUnlock()
	return isCurrent
}

func (r gameplayActionRuntime) isActionSessionCurrent(
	key gameplayActionLeaseKey,
) bool {
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[key.sessionKey]
	isCurrent := isFound && isActionSessionCurrent(peerSession, key)
	r.registry.mutex.RUnlock()
	return isCurrent
}

func isActionSessionCurrent(
	peerSession gameplayPeerSession, key gameplayActionLeaseKey,
) bool {
	return gameplaySessionMemberKey(peerSession) == key.memberKey &&
		peerSession.generation == key.zoneGeneration &&
		peerSession.transportGeneration == key.transportGeneration &&
		!peerSession.isZoneTerminal()
}

func (r gameplayActionRuntime) protectResponse(
	packet raknet.Packet, command raknet.ActionCommandData,
	key gameplayActionLeaseKey, packets [][]byte,
) ([][]byte, error) {
	duration, isDeferred, isPursuit := actionResponseLeaseDuration(
		packet.SourceTime, packets,
	)
	r.observeActionResponses(key, packets)
	if !isDeferred {
		r.registry.mutex.Lock()
		delete(r.registry.actionTerminals, key)
		r.registry.mutex.Unlock()
		return packets, nil
	}
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[key.sessionKey]
	if !isFound || !isActionSessionCurrent(peerSession, key) {
		r.registry.mutex.Unlock()
		return r.reject(
			packet, command, 0, errors.New("action session replaced"),
		)
	}
	if _, isCompleted := r.registry.actionTerminals[key]; isCompleted {
		delete(r.registry.actionTerminals, key)
		r.registry.mutex.Unlock()
		return packets, nil
	}
	r.registry.actionLeases[key] = gameplayActionLease{
		command: command, expiresAt: r.now().Add(duration),
		isPursuit: isPursuit,
	}
	r.registry.mutex.Unlock()
	expiry := gameplayActionLeaseExpiry{runtime: r, key: key}
	cancel, err := packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: duration, Produce: expiry.produce,
	}})
	if err != nil || cancel == nil {
		r.registry.mutex.Lock()
		delete(r.registry.actionLeases, key)
		delete(r.registry.actionTerminals, key)
		r.registry.mutex.Unlock()
		if err == nil {
			err = errors.New("nil cancellation")
		}
		return r.reject(
			packet, command, 0, fmt.Errorf("actionLeaseSchedule: %w", err),
		)
	}
	r.registry.mutex.Lock()
	lease, isActive := r.registry.actionLeases[key]
	if isActive {
		lease.cancel = cancel
		r.registry.actionLeases[key] = lease
	}
	r.registry.mutex.Unlock()
	if !isActive {
		cancel()
	}
	return packets, nil
}

func (e gameplayActionLeaseExpiry) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	lease, isFound := e.runtime.registry.actionLeases[e.key]
	peerSession, isSessionFound :=
		e.runtime.registry.sessions[e.key.sessionKey]
	isCurrent := isFound && isSessionFound &&
		isActionSessionCurrent(peerSession, e.key)
	if !isCurrent {
		delete(e.runtime.registry.actionLeases, e.key)
		delete(e.runtime.registry.actionTerminals, e.key)
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	delete(e.runtime.registry.actionLeases, e.key)
	delete(e.runtime.registry.actionTerminals, e.key)
	interruptedBasic := peerSession.resetFailedActionAdmission(
		e.runtime.switcher.modifierPool, e.runtime.switcher.effectPool,
	)
	movementErr := peerSession.stopPlayerMovement(e.runtime.now())
	e.runtime.registry.sessions[e.key.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	if interruptedBasic != nil {
		interruptedBasic.Stop()
	}
	if movementErr != nil {
		e.runtime.logger.Printf(
			"RakNet expired action movement cleanup skipped remote=%s generation=%d: %v",
			e.key.sessionKey, e.key.transportGeneration, movementErr,
		)
	}
	packet, err := actionraknet.Reject(lease.command)
	if err != nil {
		return nil, fmt.Errorf("actionLeaseReject: %w", err)
	}
	packets := [][]byte{packet}
	packets = append(
		packets, e.runtime.marshalActionRecoveryBaseline(e.key, "expired")...,
	)
	e.runtime.logger.Printf(
		"RakNet expired action lease recovered trace=%d remote=%s generation=%d type=%d object=%d sync=%d deadline=%s packets=%d",
		e.key.traceID, e.key.sessionKey, e.key.transportGeneration, lease.command.Common.Type,
		lease.command.Common.ObjectID, e.key.syncStamp, lease.expiresAt,
		len(packets),
	)
	return packets, nil
}

func (r gameplayActionRuntime) observeActionResponses(
	key gameplayActionLeaseKey, packets [][]byte,
) {
	for _, packet := range packets {
		syncStamp, responseType, isFound := actionResponse(packet)
		if !isFound || (responseType != raknet.ActionResponseRejected &&
			responseType != raknet.ActionResponseReleased) {
			continue
		}
		if syncStamp != key.syncStamp {
			continue
		}
		var cancel raknet.CancelSchedule
		r.registry.mutex.Lock()
		if lease, isLeased := r.registry.actionLeases[key]; isLeased {
			cancel = lease.cancel
			delete(r.registry.actionLeases, key)
		} else if _, isAdmitting := r.registry.actionAdmissions[key]; isAdmitting {
			r.registry.actionTerminals[key] = struct{}{}
		}
		r.registry.mutex.Unlock()
		if cancel != nil {
			cancel()
		}
	}
}

func actionResponseLeaseDuration(
	sourceTime uint64, packets [][]byte,
) (time.Duration, bool, bool) {
	duration := time.Duration(0)
	isDeferred := false
	isPursuit := false
	for _, packet := range packets {
		_, responseType, isFound := actionResponse(packet)
		if !isFound {
			continue
		}
		if responseType == raknet.ActionResponseRejected ||
			responseType == raknet.ActionResponseReleased {
			return 0, false, false
		}
		if responseType == raknet.ActionResponsePursuit {
			duration = actionLeaseFallbackDuration
			isDeferred = true
			isPursuit = true
			continue
		}
		if responseType != raknet.ActionResponseAccepted {
			continue
		}
		endTime := binary.LittleEndian.Uint64(packet[33:41])
		if endTime <= sourceTime {
			duration = actionLeaseFallbackDuration
			isDeferred = true
			continue
		}
		duration = time.Duration(endTime-sourceTime)*time.Millisecond +
			actionLeaseGraceDuration
		duration = min(max(duration, actionLeaseMinimumDuration),
			actionLeaseMaximumDuration)
		isDeferred = true
	}
	return duration, isDeferred, isPursuit
}

func actionResponse(packet []byte) (uint8, raknet.ActionResponseType, bool) {
	if len(packet) != 57 || packet[0] != byte(raknet.ActionCommandResponse) {
		return 0, 0, false
	}
	return packet[1], raknet.ActionResponseType(packet[2]), true
}
