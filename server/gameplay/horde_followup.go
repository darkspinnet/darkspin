package gameplay

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/zone"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	zoneprojection "github.com/darkspinnet/darkspin/server/zone/projection"
)

const campaignHordeRetryDelay = 250 * time.Millisecond

type campaignHordeFollowupKey struct {
	zone        *zone.Zone
	markerSet   string
	waveOrdinal int
}

// The registry owns these records across transport departure. Admission and
// publication have separate receipts so a projection failure cannot add actors
// twice. The deadline is fixed when retained and never shortened by a retry.
type campaignHordeFollowup struct {
	runtime     campaignDamageRuntime
	key         campaignHordeFollowupKey
	plans       []zonenpc.SpawnPlan
	readyAt     time.Time
	timer       *time.Timer
	isAdmitted  bool
	isPublished bool
}

func (e campaignDamageRuntime) retainHordeFollowup(transition campaignDamageTransition) error {
	if e.registry == nil || transition.hordeZone == nil || len(transition.hordePlans) == 0 {
		return errors.New("horde followup authority unavailable")
	}
	key := campaignHordeFollowupKey{
		zone:        transition.hordeZone,
		markerSet:   strings.ToLower(transition.hordeTransition.MarkerSetName),
		waveOrdinal: transition.hordeTransition.NextWaveOrdinal,
	}
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	if e.registry.isClosed || !key.zone.IsActive() {
		return nil
	}
	if e.registry.hordeFollowups == nil {
		e.registry.hordeFollowups = make(map[campaignHordeFollowupKey]*campaignHordeFollowup)
	}
	if e.registry.hordeFollowups[key] != nil {
		return nil
	}
	snapshot, isFound := key.zone.Horde().Snapshot(key.markerSet)
	if !isFound || snapshot.WaveOrdinal >= key.waveOrdinal {
		// A repeated damage-publication continuation must not reserve or admit
		// an already completed followup again.
		return nil
	}
	delay := max(time.Duration(0), transition.hordeTransition.NextWaveDelay)
	run := &campaignHordeFollowup{
		runtime: e, key: key,
		plans:   append([]zonenpc.SpawnPlan(nil), transition.hordePlans...),
		readyAt: e.registry.now().Add(delay),
	}
	e.registry.hordeFollowups[key] = run
	run.scheduleLocked(delay)
	return nil
}

func (e *campaignHordeFollowup) scheduleLocked(delay time.Duration) {
	if e.timer != nil {
		return
	}
	// A world timer does not depend on the initiating client's packet scheduler.
	e.timer = time.AfterFunc(delay, e.execute)
}

func (e *campaignHordeFollowup) execute() {
	registry := e.runtime.registry
	if !registry.beginRequest() {
		return
	}
	defer registry.endRequest()
	registry.mutex.Lock()
	if registry.hordeFollowups[e.key] != e {
		registry.mutex.Unlock()
		return
	}
	e.timer = nil
	if registry.isClosed || !e.key.zone.IsActive() {
		delete(registry.hordeFollowups, e.key)
		registry.mutex.Unlock()
		return
	}
	snapshot, isFound := e.key.zone.Horde().Snapshot(e.key.markerSet)
	if !isFound || snapshot.WaveOrdinal > e.key.waveOrdinal {
		delete(registry.hordeFollowups, e.key)
		registry.mutex.Unlock()
		return
	}
	at := registry.now()
	if at.Before(e.readyAt) {
		e.scheduleLocked(e.readyAt.Sub(at))
		registry.mutex.Unlock()
		return
	}
	sessionKey, member := e.driverLocked()
	if sessionKey == "" {
		e.scheduleLocked(campaignHordeRetryDelay)
		registry.mutex.Unlock()
		return
	}
	var err error
	if !e.isAdmitted {
		err = e.key.zone.AdmitHordeWave(member.deployedObjectID, e.key.markerSet, e.plans)
		if err == nil {
			e.isAdmitted = true
		}
	}
	if err == nil && !e.isPublished {
		err = e.key.zone.PublishNPCSpawn(zoneprojection.NPCSpawn{
			Plans: e.plans, TargetObjectID: member.deployedObjectID,
		}, 0, 0)
		if err == nil {
			e.isPublished = true
		}
	}
	if err == nil {
		err = e.transferSpawnsLocked()
	}
	if err != nil {
		e.scheduleLocked(campaignHordeRetryDelay)
		registry.mutex.Unlock()
		e.logFailure(err)
		return
	}
	// Creates now precede any newly polled mutation frames in every recipient's
	// retained queue. Ordinary idle-action recovery supplies current transport
	// ownership after that queue drains.
	delete(registry.hordeFollowups, e.key)
	registry.mutex.Unlock()
}

func (e *campaignHordeFollowup) transferSpawnsLocked() error {
	transferErrors := make([]error, 0)
	for key, member := range e.runtime.registry.sessions {
		if member.zone != e.key.zone || member.isRejoinPending ||
			!member.dungeonSetup.IsCommitted() || member.isZoneTerminal() ||
			!member.isCampaignPresentationAvailable() ||
			member.isMutationBaselineRequired ||
			member.zone.IsProjectionBaselineRequired(member.binding.UserID, member.generation) {
			continue
		}
		// The same registry lock protects admission, semantic publication and
		// this transfer. Mutation polling cannot enqueue a source effect before
		// the corresponding create has entered this observer's reliable queue.
		err := member.queueCampaignPresentation(nil)
		if member.isPendingPacketOverflow {
			baselineErr := member.zone.RequireProjectionBaseline(member.binding.UserID, member.generation)
			if baselineErr != nil {
				err = errors.Join(err, fmt.Errorf("hordeOverflow: %w", baselineErr))
			}
		}
		e.runtime.registry.sessions[key] = member
		if err != nil {
			transferErrors = append(transferErrors, fmt.Errorf("hordeTransfer: %w", err))
		}
	}
	transferErr := errors.Join(transferErrors...)
	if transferErr != nil {
		return fmt.Errorf("hordeObservers: %w", transferErr)
	}
	return nil
}

func (e *campaignHordeFollowup) driverLocked() (string, gameplayPeerSession) {
	sessionKey := ""
	var member gameplayPeerSession
	for key, candidate := range e.runtime.registry.sessions {
		if candidate.zone != e.key.zone || candidate.isRejoinPending ||
			candidate.isZoneTerminal() || !candidate.dungeonSetup.IsCommitted() ||
			candidate.deployedObjectID == 0 || candidate.deployedHitPoint() <= 0 ||
			candidate.transportGeneration == 0 {
			continue
		}
		if sessionKey != "" && key >= sessionKey {
			continue
		}
		sessionKey, member = key, candidate
	}
	return sessionKey, member
}

func (e *campaignHordeFollowup) logFailure(err error) {
	if e.runtime.logger != nil {
		e.runtime.logger.Printf("Campaign horde followup retained marker_set=%q wave=%d: %v", e.key.markerSet, e.key.waveOrdinal, err)
	}
}

func (e *gameplaySessionRegistry) stopHordeFollowupsLocked() {
	for key, run := range e.hordeFollowups {
		if run.timer != nil {
			run.timer.Stop()
			run.timer = nil
		}
		delete(e.hordeFollowups, key)
	}
}
