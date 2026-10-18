package game

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"

	"github.com/darkspinnet/darkspin/server/util"
)

type campaignDirectorEventKey struct {
	markerSetOrdinal int
	triggerOrdinal   int
	eventOrdinal     int
}

type campaignDirectorRuntimeEvent struct {
	key           campaignDirectorEventKey
	markerSetName string
	trigger       CampaignDirectorTrigger
	event         CampaignDirectorEvent
}

type campaignDirectorNamedEventKey struct {
	markerSetOrdinal int
	sourceObjectID   uint32
	eventHash        uint32
}

// CampaignDirectorPublication is one server-observed entry into an authored
// trigger. It exposes the named event and matching listener metadata without
// claiming that build 103 published the event locally; consumers still own
// acceptance, spawn, gate, objective, or presentation policy.
type CampaignDirectorPublication struct {
	ActorObjectID    uint32
	IsDwellComplete  bool
	EventHash        uint32
	MarkerSetOrdinal int
	MarkerSetName    string
	TriggerOrdinal   int
	TriggerMarkerID  uint32
	TriggerName      string
	EventOrdinal     int
	EventName        string
	CallbackName     string
	Listeners        []CampaignDirectorListenerPublication
}

// CampaignDirectorListenerPublication is one authored director-marker
// listener selected by a trigger's named event. It does not choose a noun,
// budget, count, object ID, or packet policy.
type CampaignDirectorListenerPublication struct {
	EventHash          uint32
	NativeCallbackHash uint32
	NativeCallbackName string
	LuaCallbackName    string
	Rotation           Vec3
	MarkerSetOrdinal   int
	MarkerSetName      string
	MarkerOrdinal      int
	MarkerID           uint32
	MarkerName         string
	NounName           string
	SpawnKind          uint32
	PoolKind           string
	IsSpawnKindKnown   bool
	Position           Vec3
	EventOrdinal       int
	CallbackName       string
}

// CampaignDirectorNamedEventPublication is one server-authoritative named
// event prepared for live listeners across the simulator. The encounter
// that owns SourceObjectID decides when the event exists; the director session
// retains provenance and makes preparation/acceptance transactional.
type CampaignDirectorNamedEventPublication struct {
	EventHash        uint32
	PublicationID    uint64
	MarkerSetOrdinal int
	MarkerSetName    string
	SourceObjectID   uint32
	EventName        string
	Listeners        []CampaignDirectorListenerPublication
}

// CampaignDirectorSnapshot contains the accepted publication history for a
// match and can seed reconnect or late-join reconstruction without replaying
// movement commands.
type CampaignDirectorSnapshot struct {
	Publications      []CampaignDirectorPublication
	NamedPublications []CampaignDirectorNamedEventPublication
}

// CampaignDirectorSession owns movement-trigger state for one campaign match.
type CampaignDirectorSession struct {
	triggerStates           map[campaignDirectorEventKey]campaignTriggerState
	mu                      sync.RWMutex
	events                  []campaignDirectorRuntimeEvent
	listenersByEvent        map[uint32][]CampaignDirectorListenerPublication
	listenerOwners          map[campaignDirectorListenerOwnerKey]struct{}
	insideStates            map[campaignDirectorEventKey]bool
	onceOnlyEventPolicies   map[campaignDirectorEventKey]bool
	pendingPublications     map[campaignDirectorEventKey]CampaignDirectorPublication
	firedPublications       map[campaignDirectorEventKey]CampaignDirectorPublication
	publications            []CampaignDirectorPublication
	markerSetNamesByOrdinal map[int]string
	pendingNamedEvents      map[campaignDirectorNamedEventKey]CampaignDirectorNamedEventPublication
	namedPublications       []CampaignDirectorNamedEventPublication
	nextNamedPublicationID  uint64
}

func NewCampaignDirectorSession(director CampaignDirector) (*CampaignDirectorSession, error) {
	if director.Level == "" {
		return nil, errors.New("create campaign director: empty level")
	}
	session := &CampaignDirectorSession{
		triggerStates:           make(map[campaignDirectorEventKey]campaignTriggerState),
		insideStates:            make(map[campaignDirectorEventKey]bool),
		onceOnlyEventPolicies:   make(map[campaignDirectorEventKey]bool),
		pendingPublications:     make(map[campaignDirectorEventKey]CampaignDirectorPublication),
		firedPublications:       make(map[campaignDirectorEventKey]CampaignDirectorPublication),
		listenersByEvent:        make(map[uint32][]CampaignDirectorListenerPublication),
		listenerOwners:          make(map[campaignDirectorListenerOwnerKey]struct{}),
		markerSetNamesByOrdinal: make(map[int]string),
		pendingNamedEvents:      make(map[campaignDirectorNamedEventKey]CampaignDirectorNamedEventPublication),
		nextNamedPublicationID:  1,
	}
	markerSets := slices.Clone(director.MarkerSets)
	slices.SortStableFunc(markerSets, func(a, b CampaignDirectorMarkerSet) int {
		if a.Ordinal < b.Ordinal {
			return -1
		}
		if a.Ordinal > b.Ordinal {
			return 1
		}
		return 0
	})
	for _, markerSet := range markerSets {
		if _, isFound := session.markerSetNamesByOrdinal[markerSet.Ordinal]; isFound {
			return nil, fmt.Errorf("createMarkerSet[%d]: duplicate", markerSet.Ordinal)
		}
		session.markerSetNamesByOrdinal[markerSet.Ordinal] = markerSet.Name
		// Selected placements instantiate server-owned listener components even
		// when their scenery presentation remains client-owned. No unselected
		// marker set contributes an owner.
		session.instantiateMarkerSetListeners(markerSet)
		for _, trigger := range markerSet.Triggers {
			if !isFiniteCampaignPosition(trigger.Position) {
				return nil, fmt.Errorf("createTriggerPosition[%d]: invalid", trigger.MarkerID)
			}
			if trigger.SpawnTrigger != nil && trigger.SpawnTrigger.TriggerVolume != nil {
				volume := trigger.SpawnTrigger.TriggerVolume
				if !isFiniteCampaignPosition(trigger.Rotation) ||
					!isFiniteCampaignPosition(volume.Offset) ||
					!isFiniteCampaignPosition(volume.BoxDimensions) ||
					math.IsNaN(float64(volume.SphereRadius)) ||
					math.IsInf(float64(volume.SphereRadius), 0) ||
					(volume.Shape == uint32(TriggerSphere) && volume.SphereRadius <= 0) ||
					(volume.Shape == uint32(TriggerBox) &&
						(volume.BoxDimensions.X <= 0 || volume.BoxDimensions.Y <= 0 || volume.BoxDimensions.Z <= 0)) ||
					math.IsNaN(float64(volume.TimeToActivate)) ||
					math.IsInf(float64(volume.TimeToActivate), 0) || volume.TimeToActivate < 0 ||
					float64(volume.TimeToActivate) >= float64(math.MaxInt64)/1e9 {
					return nil, fmt.Errorf("createTriggerVolume[%d]: invalid", trigger.MarkerID)
				}
			}
			for _, event := range trigger.Events {
				if math.IsNaN(float64(event.TriggerRadius)) || math.IsInf(float64(event.TriggerRadius), 0) ||
					event.TriggerRadius < 0 {
					return nil, fmt.Errorf("createTriggerRadius[%d/%d]: %g",
						trigger.MarkerID, event.Ordinal, event.TriggerRadius)
				}
				if event.TriggerRadius == 0 || (event.EventName == "" && event.CallbackName == "") {
					continue
				}
				key := campaignDirectorEventKey{
					markerSetOrdinal: markerSet.Ordinal,
					triggerOrdinal:   trigger.Ordinal,
					eventOrdinal:     event.Ordinal,
				}
				session.events = append(session.events, campaignDirectorRuntimeEvent{
					key:           key,
					markerSetName: markerSet.Name,
					trigger:       trigger,
					event:         event,
				})
				session.onceOnlyEventPolicies[key] = event.IsTriggerOnceOnly
				if trigger.SpawnTrigger != nil && trigger.SpawnTrigger.TriggerVolume != nil {
					session.onceOnlyEventPolicies[key] = trigger.SpawnTrigger.TriggerVolume.IsTriggerOnceOnly
				}
			}
		}
	}
	return session, nil
}

// Advance evaluates one accepted authoritative movement segment. A repeatable
// event fires only on entry; a once-only event cannot fire again after exit.
func (s *CampaignDirectorSession) Advance(previous Vec3, current Vec3) ([]CampaignDirectorPublication, error) {
	publications, err := s.advanceMovement(previous, current, false)
	if err != nil {
		return nil, fmt.Errorf("directorMovement: %w", err)
	}
	return publications, nil
}

func (s *CampaignDirectorSession) advanceMovement(
	previous Vec3, current Vec3, isPartySample bool,
) ([]CampaignDirectorPublication, error) {
	if s == nil {
		return nil, errors.New("advance campaign director: nil session")
	}
	if !isFiniteCampaignPosition(previous) || !isFiniteCampaignPosition(current) {
		return nil, errors.New("advance campaign director: invalid position")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	publications := make([]CampaignDirectorPublication, 0)
	for _, runtimeEvent := range s.events {
		if isPartySample && runtimeEvent.trigger.SpawnTrigger != nil &&
			runtimeEvent.trigger.SpawnTrigger.TriggerVolume != nil {
			// Production spawn triggers require party and elapsed-time input.
			continue
		}
		radius := runtimeEvent.event.TriggerRadius
		isCurrentInside := campaignDistanceSquared(current, runtimeEvent.trigger.Position) <= radius*radius
		wasInside := s.insideStates[runtimeEvent.key]
		isCrossed := campaignSegmentDistanceSquared(
			previous, current, runtimeEvent.trigger.Position,
		) <= radius*radius
		s.insideStates[runtimeEvent.key] = isCurrentInside
		pendingPublication, isPending := s.pendingPublications[runtimeEvent.key]
		if isPending && isCurrentInside {
			publications = append(
				publications,
				cloneCampaignDirectorPublication(pendingPublication),
			)
			continue
		}
		if wasInside || !isCrossed {
			continue
		}
		if runtimeEvent.event.IsTriggerOnceOnly {
			if _, isFired := s.firedPublications[runtimeEvent.key]; isFired {
				continue
			}
		}
		publication := CampaignDirectorPublication{
			MarkerSetOrdinal: runtimeEvent.key.markerSetOrdinal,
			MarkerSetName:    runtimeEvent.markerSetName,
			TriggerOrdinal:   runtimeEvent.key.triggerOrdinal,
			TriggerMarkerID:  runtimeEvent.trigger.MarkerID,
			TriggerName:      runtimeEvent.trigger.Name,
			EventOrdinal:     runtimeEvent.key.eventOrdinal,
			EventName:        runtimeEvent.event.EventName,
			CallbackName:     runtimeEvent.event.CallbackName,
			EventHash:        campaignDirectorEventHash(runtimeEvent.event),
			Listeners: append([]CampaignDirectorListenerPublication(nil),
				s.listenersByEvent[campaignDirectorEventHash(runtimeEvent.event)]...),
		}
		publications = append(publications, publication)
		s.pendingPublications[runtimeEvent.key] = cloneCampaignDirectorPublication(publication)
	}
	return publications, nil
}

// Accept commits one previously observed trigger request after an
// authoritative consumer has applied its complete operation. Once-only policy
// is consumed here, never merely by movement observation.
func (s *CampaignDirectorSession) Accept(publication CampaignDirectorPublication) error {
	if s == nil {
		return errors.New("accept campaign director: nil session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.canAccept(publication)
	if err != nil {
		return fmt.Errorf("directorAcceptCheck: %w", err)
	}
	key := campaignDirectorEventKey{
		markerSetOrdinal: publication.MarkerSetOrdinal,
		triggerOrdinal:   publication.TriggerOrdinal,
		eventOrdinal:     publication.EventOrdinal,
	}
	pendingPublication := s.pendingPublications[key]
	delete(s.pendingPublications, key)
	acceptedPublication := cloneCampaignDirectorPublication(pendingPublication)
	s.publications = append(s.publications, acceptedPublication)
	if s.onceOnlyEventPolicies[key] {
		s.firedPublications[key] = acceptedPublication
	}
	return nil
}

func (s *CampaignDirectorSession) CanAccept(publication CampaignDirectorPublication) error {
	if s == nil {
		return errors.New("accept campaign director: nil session")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.canAccept(publication)
}

func (s *CampaignDirectorSession) canAccept(
	publication CampaignDirectorPublication,
) error {
	key := campaignDirectorEventKey{
		markerSetOrdinal: publication.MarkerSetOrdinal,
		triggerOrdinal:   publication.TriggerOrdinal,
		eventOrdinal:     publication.EventOrdinal,
	}
	pendingPublication, isFound := s.pendingPublications[key]
	if !isFound || pendingPublication.TriggerMarkerID != publication.TriggerMarkerID ||
		pendingPublication.EventHash != publication.EventHash ||
		pendingPublication.EventName != publication.EventName ||
		pendingPublication.CallbackName != publication.CallbackName {
		return errors.New("accept campaign director: unknown publication")
	}
	return nil
}

// PrepareNamedEvent resolves an encounter-owned event to live listeners under
// the full simulator event hash. Repeating the same preparation before
// acceptance returns the original publication and does not allocate a second
// transaction.
func (s *CampaignDirectorSession) PrepareNamedEvent(
	markerSetOrdinal int, sourceObjectID uint32, eventName string,
) (CampaignDirectorNamedEventPublication, error) {
	if s == nil {
		return CampaignDirectorNamedEventPublication{}, errors.New("prepare named event: nil session")
	}
	if strings.TrimSpace(eventName) == "" {
		return CampaignDirectorNamedEventPublication{}, errors.New("prepare named event: empty name")
	}
	publication, err := s.PrepareNamedEventHash(markerSetOrdinal, sourceObjectID, util.HashID(eventName), eventName)
	if err != nil {
		return CampaignDirectorNamedEventPublication{}, fmt.Errorf("namedEventHash: %w", err)
	}
	return publication, nil
}

// PrepareNamedEventHash permits unnamed authored IDs as well as named events.
// Dispatch takes a snapshot: callbacks may remove owners safely, and removal
// affects subsequent publications rather than invalidating this iteration.
func (s *CampaignDirectorSession) PrepareNamedEventHash(
	markerSetOrdinal int, sourceObjectID, eventHash uint32, eventName string,
) (CampaignDirectorNamedEventPublication, error) {
	if s == nil || sourceObjectID == 0 {
		return CampaignDirectorNamedEventPublication{}, errors.New("prepare named event: invalid source")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	markerSetName, isMarkerSetFound := s.markerSetNamesByOrdinal[markerSetOrdinal]
	if !isMarkerSetFound {
		return CampaignDirectorNamedEventPublication{}, errors.New("prepare named event: marker set unavailable")
	}
	key := campaignDirectorNamedEventKey{
		markerSetOrdinal: markerSetOrdinal, sourceObjectID: sourceObjectID, eventHash: eventHash,
	}
	pending, isPending := s.pendingNamedEvents[key]
	if isPending {
		return cloneCampaignDirectorNamedEventPublication(pending), nil
	}
	listeners := s.listenersByEvent[eventHash]
	publication := CampaignDirectorNamedEventPublication{
		EventHash: eventHash, PublicationID: s.nextNamedPublicationID, MarkerSetOrdinal: markerSetOrdinal,
		MarkerSetName: markerSetName, SourceObjectID: sourceObjectID, EventName: eventName,
		Listeners: append([]CampaignDirectorListenerPublication(nil), listeners...),
	}
	s.nextNamedPublicationID++
	s.pendingNamedEvents[key] = cloneCampaignDirectorNamedEventPublication(publication)
	return publication, nil
}

func (s *CampaignDirectorSession) AcceptNamedEvent(
	publication CampaignDirectorNamedEventPublication,
) error {
	if s == nil {
		return errors.New("accept named event: nil session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.canAcceptNamedEvent(publication)
	if err != nil {
		return fmt.Errorf("namedEventAcceptCheck: %w", err)
	}
	key := campaignDirectorNamedEventKey{
		markerSetOrdinal: publication.MarkerSetOrdinal,
		sourceObjectID:   publication.SourceObjectID,
		eventHash:        publication.EventHash,
	}
	pending := s.pendingNamedEvents[key]
	delete(s.pendingNamedEvents, key)
	s.namedPublications = append(
		s.namedPublications, cloneCampaignDirectorNamedEventPublication(pending),
	)
	return nil
}

func (s *CampaignDirectorSession) CanAcceptNamedEvent(
	publication CampaignDirectorNamedEventPublication,
) error {
	if s == nil {
		return errors.New("accept named event: nil session")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.canAcceptNamedEvent(publication)
}

func (s *CampaignDirectorSession) canAcceptNamedEvent(
	publication CampaignDirectorNamedEventPublication,
) error {
	key := campaignDirectorNamedEventKey{
		markerSetOrdinal: publication.MarkerSetOrdinal,
		sourceObjectID:   publication.SourceObjectID,
		eventHash:        publication.EventHash,
	}
	pending, isPending := s.pendingNamedEvents[key]
	if !isPending || pending.PublicationID != publication.PublicationID ||
		pending.MarkerSetName != publication.MarkerSetName ||
		pending.EventName != publication.EventName ||
		!slices.Equal(pending.Listeners, publication.Listeners) {
		return errors.New("accept named event: unknown publication")
	}
	return nil
}

func campaignDirectorEventName(eventName string) string {
	return strings.ToLower(strings.TrimSpace(eventName))
}

// CampaignDirectorCallbackEventName gives an authored marker callback a stable
// internal event identity when its event-name field is empty. The marker ID
// keeps unrelated empty callbacks in the same marker set isolated.
func CampaignDirectorCallbackEventName(
	markerID uint32, callbackName string,
) string {
	callbackKey := campaignDirectorEventName(callbackName)
	if markerID == 0 || callbackKey == "" {
		return ""
	}
	return fmt.Sprintf("@callback:%d:%s", markerID, callbackKey)
}

func (s *CampaignDirectorSession) Snapshot() CampaignDirectorSnapshot {
	if s == nil {
		return CampaignDirectorSnapshot{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	publications := make([]CampaignDirectorPublication, 0, len(s.publications))
	for _, publication := range s.publications {
		publications = append(publications, cloneCampaignDirectorPublication(publication))
	}
	namedPublications := make(
		[]CampaignDirectorNamedEventPublication, 0, len(s.namedPublications),
	)
	for _, publication := range s.namedPublications {
		namedPublications = append(
			namedPublications, cloneCampaignDirectorNamedEventPublication(publication),
		)
	}
	return CampaignDirectorSnapshot{
		Publications: publications, NamedPublications: namedPublications,
	}
}

func cloneCampaignDirectorPublication(publication CampaignDirectorPublication) CampaignDirectorPublication {
	publication.Listeners = append([]CampaignDirectorListenerPublication(nil), publication.Listeners...)
	return publication
}

func cloneCampaignDirectorNamedEventPublication(
	publication CampaignDirectorNamedEventPublication,
) CampaignDirectorNamedEventPublication {
	publication.Listeners = append([]CampaignDirectorListenerPublication(nil), publication.Listeners...)
	return publication
}

func isFiniteCampaignPosition(position Vec3) bool {
	for _, coordinate := range []float32{position.X, position.Y, position.Z} {
		if math.IsNaN(float64(coordinate)) || math.IsInf(float64(coordinate), 0) {
			return false
		}
	}
	return true
}

func campaignDistanceSquared(left Vec3, right Vec3) float32 {
	x := left.X - right.X
	y := left.Y - right.Y
	z := left.Z - right.Z
	return x*x + y*y + z*z
}

func campaignSegmentDistanceSquared(start Vec3, end Vec3, point Vec3) float32 {
	delta := Vec3{X: end.X - start.X, Y: end.Y - start.Y, Z: end.Z - start.Z}
	lengthSquared := campaignDistanceSquared(start, end)
	if lengthSquared == 0 {
		return campaignDistanceSquared(start, point)
	}
	projection := ((point.X-start.X)*delta.X + (point.Y-start.Y)*delta.Y +
		(point.Z-start.Z)*delta.Z) / lengthSquared
	projection = min(float32(1), max(float32(0), projection))
	closest := Vec3{
		X: start.X + projection*delta.X,
		Y: start.Y + projection*delta.Y,
		Z: start.Z + projection*delta.Z,
	}
	return campaignDistanceSquared(closest, point)
}
