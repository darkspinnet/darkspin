package game

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
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

type campaignDirectorListenerKey struct {
	markerSetOrdinal int
	eventName        string
}

type campaignDirectorNamedEventKey struct {
	markerSetOrdinal int
	sourceObjectID   uint32
	eventName        string
}

// CampaignDirectorPublication is one server-observed entry into an authored
// trigger. It exposes the named event and matching listener metadata without
// claiming that build 103 published the event locally; consumers still own
// acceptance, spawn, gate, objective, or presentation policy.
type CampaignDirectorPublication struct {
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
	Rotation         Vec3
	MarkerSetOrdinal int
	MarkerSetName    string
	MarkerOrdinal    int
	MarkerID         uint32
	MarkerName       string
	NounName         string
	SpawnKind        uint32
	PoolKind         string
	IsSpawnKindKnown bool
	Position         Vec3
	EventOrdinal     int
	CallbackName     string
}

// CampaignDirectorNamedEventPublication is one server-authoritative named
// event prepared for the authored listeners in its marker set. The encounter
// that owns SourceObjectID decides when the event exists; the director session
// only scopes listeners and makes preparation/acceptance transactional.
type CampaignDirectorNamedEventPublication struct {
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
	mu                      sync.RWMutex
	events                  []campaignDirectorRuntimeEvent
	listenersByEvent        map[campaignDirectorListenerKey][]CampaignDirectorListenerPublication
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
		insideStates:            make(map[campaignDirectorEventKey]bool),
		onceOnlyEventPolicies:   make(map[campaignDirectorEventKey]bool),
		pendingPublications:     make(map[campaignDirectorEventKey]CampaignDirectorPublication),
		firedPublications:       make(map[campaignDirectorEventKey]CampaignDirectorPublication),
		listenersByEvent:        make(map[campaignDirectorListenerKey][]CampaignDirectorListenerPublication),
		markerSetNamesByOrdinal: make(map[int]string),
		pendingNamedEvents:      make(map[campaignDirectorNamedEventKey]CampaignDirectorNamedEventPublication),
		nextNamedPublicationID:  1,
	}
	for _, markerSet := range director.MarkerSets {
		if _, isFound := session.markerSetNamesByOrdinal[markerSet.Ordinal]; isFound {
			return nil, fmt.Errorf("createMarkerSet[%d]: duplicate", markerSet.Ordinal)
		}
		session.markerSetNamesByOrdinal[markerSet.Ordinal] = markerSet.Name
		for _, marker := range markerSet.Markers {
			for _, event := range marker.Events {
				if event.CallbackName == "" {
					continue
				}
				eventKey := campaignDirectorEventName(event.EventName)
				if eventKey == "" {
					eventKey = campaignDirectorEventName(
						CampaignDirectorCallbackEventName(
							marker.MarkerID, event.CallbackName,
						),
					)
				}
				if eventKey == "" {
					continue
				}
				listenerKey := campaignDirectorListenerKey{
					markerSetOrdinal: markerSet.Ordinal, eventName: eventKey,
				}
				session.listenersByEvent[listenerKey] = append(session.listenersByEvent[listenerKey],
					CampaignDirectorListenerPublication{
						MarkerSetOrdinal: markerSet.Ordinal, MarkerSetName: markerSet.Name,
						MarkerOrdinal: marker.Ordinal, MarkerID: marker.MarkerID,
						MarkerName: marker.Name, NounName: marker.NounName,
						SpawnKind: marker.SpawnKind, PoolKind: marker.PoolKind,
						IsSpawnKindKnown: marker.IsSpawnKindKnown, Position: marker.Position,
						Rotation:     marker.Rotation,
						EventOrdinal: event.Ordinal, CallbackName: event.CallbackName,
					})
			}
		}
		for _, trigger := range markerSet.Triggers {
			if !isFiniteCampaignPosition(trigger.Position) {
				return nil, fmt.Errorf("createTriggerPosition[%d]: invalid", trigger.MarkerID)
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
			}
		}
	}
	return session, nil
}

// Advance evaluates one accepted authoritative movement segment. A repeatable
// event fires only on entry; a once-only event cannot fire again after exit.
func (s *CampaignDirectorSession) Advance(previous Vec3, current Vec3) ([]CampaignDirectorPublication, error) {
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
			Listeners: append([]CampaignDirectorListenerPublication(nil),
				s.listenersByEvent[campaignDirectorListenerKey{
					markerSetOrdinal: runtimeEvent.key.markerSetOrdinal,
					eventName:        campaignDirectorEventName(runtimeEvent.event.EventName),
				}]...),
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
		pendingPublication.EventName != publication.EventName ||
		pendingPublication.CallbackName != publication.CallbackName {
		return errors.New("accept campaign director: unknown publication")
	}
	return nil
}

// PrepareNamedEvent resolves one encounter-owned event to the authored
// listeners in the same marker set. Repeating the same preparation before
// acceptance returns the original publication and does not allocate a second
// transaction.
func (s *CampaignDirectorSession) PrepareNamedEvent(
	markerSetOrdinal int, sourceObjectID uint32, eventName string,
) (CampaignDirectorNamedEventPublication, error) {
	if s == nil {
		return CampaignDirectorNamedEventPublication{}, errors.New("prepare named event: nil session")
	}
	eventKey := campaignDirectorEventName(eventName)
	if sourceObjectID == 0 || eventKey == "" {
		return CampaignDirectorNamedEventPublication{}, errors.New("prepare named event: invalid identity")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	markerSetName, isMarkerSetFound := s.markerSetNamesByOrdinal[markerSetOrdinal]
	if !isMarkerSetFound {
		return CampaignDirectorNamedEventPublication{}, errors.New("prepare named event: marker set unavailable")
	}
	key := campaignDirectorNamedEventKey{
		markerSetOrdinal: markerSetOrdinal, sourceObjectID: sourceObjectID, eventName: eventKey,
	}
	pending, isPending := s.pendingNamedEvents[key]
	if isPending {
		return cloneCampaignDirectorNamedEventPublication(pending), nil
	}
	listeners := s.listenersByEvent[campaignDirectorListenerKey{
		markerSetOrdinal: markerSetOrdinal, eventName: eventKey,
	}]
	if len(listeners) == 0 {
		return CampaignDirectorNamedEventPublication{}, errors.New("prepare named event: listener unavailable")
	}
	publication := CampaignDirectorNamedEventPublication{
		PublicationID: s.nextNamedPublicationID, MarkerSetOrdinal: markerSetOrdinal,
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
		eventName:        campaignDirectorEventName(publication.EventName),
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
		eventName:        campaignDirectorEventName(publication.EventName),
	}
	pending, isPending := s.pendingNamedEvents[key]
	if !isPending || pending.PublicationID != publication.PublicationID ||
		pending.MarkerSetName != publication.MarkerSetName ||
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
