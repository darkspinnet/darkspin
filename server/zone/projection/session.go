package projection

import (
	"errors"
	"sync"

	"github.com/darkspinnet/darkspin/server/game"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	zoneobjective "github.com/darkspinnet/darkspin/server/zone/objective"
)

const pendingEventLimit = 4096

type Subscriber struct {
	UserID         uint64
	PeerGeneration uint64
}

type EventKind uint8

const (
	EventObjective EventKind = iota + 1
	EventNPCDamage
	EventNPCDeath
	EventNPCSpawn
	EventNPCAction
	EventHeroResource
	EventCompanionDamage
	EventCompanionResource
	EventNPCForcedMovement
	EventHeroMovement
	EventHeroRoster
	EventHeroDeploy
	EventHeroLeave
)

type NPCSpawn struct {
	Plans          []zonenpc.SpawnPlan
	TargetObjectID uint32
	IsBossAddPhase bool
	IsBossActive   bool
	BossObjectID   uint32
	IsFinalBoss    bool
	IsDormant      bool
}

type HeroResource struct {
	PlayerSlot       uint8
	CreatureIndex    uint32
	ObjectID         uint32
	HitPoint         float32
	ManaPoint        float32
	MaximumHitPoint  float32
	MaximumManaPoint float32
}

type HeroMovement struct {
	ObjectID           uint32
	Goal               game.Vec3
	AnimationTimestamp uint64
	IsDanceStopped     bool
}

type HeroRoster struct {
	UserID        uint64
	PlayerSlot    uint16
	Team          uint16
	Roster        game.GameplayRoster
	Position      game.Vec3
	CreatureIndex uint32
}

type HeroDeploy struct {
	PlayerSlot    uint16
	CreatureIndex uint32
	ObjectID      uint32
	Position      game.Vec3
}

type HeroLeave struct {
	ObjectIDs []uint32
}

type CompanionDamage struct {
	SourceObjectID uint32
	TargetObjectID uint32
	Damage         float32
	HitPoint       float32
	IsDefeated     bool
}

type CompanionResource struct {
	ObjectID        uint32
	HitPoint        float32
	MaximumHitPoint float32
}

type Event struct {
	Sequence          uint64
	Kind              EventKind
	Objective         zoneobjective.Update
	Damage            zonenpc.DamageEvent
	Death             zonenpc.DeathEvent
	Spawn             NPCSpawn
	Action            zonenpc.ActionEvent
	Resource          HeroResource
	Companion         CompanionDamage
	CompanionResource CompanionResource
	ForcedMovement    zonenpc.ForcedMovementEvent
	HeroMovement      HeroMovement
	HeroRoster        HeroRoster
	HeroDeploy        HeroDeploy
	HeroLeave         HeroLeave
}

func (s *Session) PublishHeroLeave(leave HeroLeave) {
	if s == nil || len(leave.ObjectIDs) == 0 {
		return
	}
	leave.ObjectIDs = append([]uint32(nil), leave.ObjectIDs...)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	event := Event{Sequence: s.next, Kind: EventHeroLeave, HeroLeave: leave}
	for subscriber := range s.eventsBySubscriber {
		s.enqueueLocked(subscriber, event)
	}
}

func (s *Session) PublishHeroDeploy(deploy HeroDeploy, excluded Subscriber) {
	if s == nil || deploy.ObjectID == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	event := Event{Sequence: s.next, Kind: EventHeroDeploy, HeroDeploy: deploy}
	for subscriber := range s.eventsBySubscriber {
		if subscriber == excluded {
			continue
		}
		s.enqueueLocked(subscriber, event)
	}
}

func (s *Session) PublishHeroRoster(roster HeroRoster, excluded Subscriber) {
	if s == nil || roster.UserID == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	event := Event{Sequence: s.next, Kind: EventHeroRoster, HeroRoster: roster}
	for subscriber := range s.eventsBySubscriber {
		if subscriber == excluded {
			continue
		}
		s.enqueueLocked(subscriber, event)
	}
}

func (s *Session) PublishHeroMovement(
	movement HeroMovement, excluded Subscriber,
) {
	if s == nil || movement.ObjectID == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	event := Event{
		Sequence: s.next, Kind: EventHeroMovement, HeroMovement: movement,
	}
	for subscriber := range s.eventsBySubscriber {
		if subscriber == excluded {
			continue
		}
		s.enqueueLocked(subscriber, event)
	}
}

func (s *Session) PublishNPCForcedMovement(
	movement zonenpc.ForcedMovementEvent, excluded Subscriber,
) {
	if s == nil || movement.Plan.SourceObjectID == 0 ||
		movement.Plan.TargetObjectID == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	event := Event{
		Sequence: s.next, Kind: EventNPCForcedMovement,
		ForcedMovement: movement,
	}
	for subscriber := range s.eventsBySubscriber {
		if subscriber == excluded {
			continue
		}
		s.enqueueLocked(subscriber, event)
	}
}

// Session retains instance events until each active member's transport adapter
// consumes its own projection.
type Session struct {
	mu                     sync.Mutex
	next                   uint64
	eventsBySubscriber     map[Subscriber][]Event
	isBaselineBySubscriber map[Subscriber]bool
	deletedNPCObjectIDs    map[uint32]struct{}
}

func NewSession() *Session {
	return &Session{
		eventsBySubscriber:     make(map[Subscriber][]Event),
		isBaselineBySubscriber: make(map[Subscriber]bool),
		deletedNPCObjectIDs:    make(map[uint32]struct{}),
	}
}

func (s *Session) enqueueLocked(subscriber Subscriber, event Event) {
	if s.isBaselineBySubscriber[subscriber] {
		pending := s.eventsBySubscriber[subscriber]
		if len(pending) >= pendingEventLimit {
			s.eventsBySubscriber[subscriber] = nil
			return
		}
		s.eventsBySubscriber[subscriber] = append(pending, event)
		return
	}
	pending := s.eventsBySubscriber[subscriber]
	if len(pending) >= pendingEventLimit {
		s.eventsBySubscriber[subscriber] = nil
		s.isBaselineBySubscriber[subscriber] = true
		return
	}
	s.eventsBySubscriber[subscriber] = append(pending, event)
}

// Revision returns the latest semantic event sequence. A transport baseline
// uses this as a fence so events published while it is encoded remain queued.
func (s *Session) Revision() uint64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.next
}

func (s *Session) Join(subscriber Subscriber) error {
	if s == nil {
		return errors.New("projection join: nil session")
	}
	if subscriber.UserID == 0 || subscriber.PeerGeneration == 0 {
		return errors.New("projection join: invalid subscriber")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for current := range s.eventsBySubscriber {
		if current.UserID == subscriber.UserID && current != subscriber {
			delete(s.eventsBySubscriber, current)
			delete(s.isBaselineBySubscriber, current)
		}
	}
	if _, isFound := s.eventsBySubscriber[subscriber]; !isFound {
		s.eventsBySubscriber[subscriber] = nil
	}
	s.isBaselineBySubscriber[subscriber] = false
	return nil
}

func (s *Session) Leave(subscriber Subscriber) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, isFound := s.eventsBySubscriber[subscriber]; !isFound {
		return false
	}
	delete(s.eventsBySubscriber, subscriber)
	delete(s.isBaselineBySubscriber, subscriber)
	return true
}

// ResetThrough advances a subscriber through a baseline revision without
// dropping events published while the baseline was being encoded.
func (s *Session) ResetThrough(subscriber Subscriber, sequence uint64) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	event, isFound := s.eventsBySubscriber[subscriber]
	if !isFound {
		return false
	}
	advanced := 0
	for advanced < len(event) && event[advanced].Sequence <= sequence {
		advanced++
	}
	s.eventsBySubscriber[subscriber] = append([]Event(nil), event[advanced:]...)
	s.isBaselineBySubscriber[subscriber] = false
	return true
}

func (s *Session) IsBaselineRequired(subscriber Subscriber) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.isBaselineBySubscriber[subscriber]
}

// RequireBaseline repairs a missing or invalid subscriber cursor without
// guessing which events reached its client. The next projection poll must
// publish an authoritative baseline before incremental delivery resumes.
func (s *Session) RequireBaseline(subscriber Subscriber) error {
	if s == nil {
		return errors.New("projection baseline: nil session")
	}
	if subscriber.UserID == 0 || subscriber.PeerGeneration == 0 {
		return errors.New("projection baseline: invalid subscriber")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, isFound := s.eventsBySubscriber[subscriber]; !isFound {
		s.eventsBySubscriber[subscriber] = nil
	}
	s.isBaselineBySubscriber[subscriber] = true
	return nil
}

func (s *Session) PublishObjective(updates []zoneobjective.Update) {
	if s == nil || len(updates) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, update := range updates {
		s.next++
		event := Event{
			Sequence: s.next, Kind: EventObjective, Objective: update,
		}
		for subscriber := range s.eventsBySubscriber {
			s.enqueueLocked(subscriber, event)
		}
	}
}

func (s *Session) PublishNPCDamage(damage zonenpc.DamageEvent) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	event := Event{
		Sequence: s.next, Kind: EventNPCDamage, Damage: damage,
	}
	for subscriber := range s.eventsBySubscriber {
		s.enqueueLocked(subscriber, event)
	}
}

func (s *Session) PublishNPCDeath(
	deaths []zonenpc.DeathEvent, excluded Subscriber,
) {
	if s == nil || len(deaths) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deletedNPCObjectIDs == nil {
		s.deletedNPCObjectIDs = make(map[uint32]struct{})
	}
	for _, current := range deaths {
		if current.Kind == zonenpc.DeathDelete {
			if _, isDeleted := s.deletedNPCObjectIDs[current.TargetObjectID]; isDeleted {
				continue
			}
			s.deletedNPCObjectIDs[current.TargetObjectID] = struct{}{}
		}
		s.next++
		event := Event{
			Sequence: s.next, Kind: EventNPCDeath, Death: current,
		}
		for subscriber := range s.eventsBySubscriber {
			if subscriber == excluded {
				continue
			}
			s.enqueueLocked(subscriber, event)
		}
	}
}

func (s *Session) PublishNPCSpawn(spawn NPCSpawn, excluded Subscriber) {
	if s == nil || len(spawn.Plans) == 0 ||
		(!spawn.IsDormant && spawn.TargetObjectID == 0) {
		return
	}
	spawn.Plans = append([]zonenpc.SpawnPlan(nil), spawn.Plans...)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, plan := range spawn.Plans {
		delete(s.deletedNPCObjectIDs, plan.ObjectID)
	}
	s.next++
	event := Event{
		Sequence: s.next, Kind: EventNPCSpawn, Spawn: spawn,
	}
	for subscriber := range s.eventsBySubscriber {
		if subscriber == excluded {
			continue
		}
		s.enqueueLocked(subscriber, event)
	}
}

func (s *Session) PublishNPCAction(
	action zonenpc.ActionEvent, excluded Subscriber,
) {
	if s == nil || action.Kind == 0 || action.Plan.ObjectID == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	event := Event{
		Sequence: s.next, Kind: EventNPCAction, Action: action,
	}
	for subscriber := range s.eventsBySubscriber {
		if subscriber == excluded {
			continue
		}
		s.enqueueLocked(subscriber, event)
	}
}

func (s *Session) PublishNPCActionAll(action zonenpc.ActionEvent) {
	if s == nil || action.Kind == 0 || action.Plan.ObjectID == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	event := Event{
		Sequence: s.next, Kind: EventNPCAction, Action: action,
	}
	for subscriber := range s.eventsBySubscriber {
		s.enqueueLocked(subscriber, event)
	}
}

func (s *Session) PublishHeroResource(
	resource HeroResource, excludedSubscribers ...Subscriber,
) {
	if s == nil || resource.ObjectID == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	event := Event{
		Sequence: s.next, Kind: EventHeroResource, Resource: resource,
	}
	for subscriber := range s.eventsBySubscriber {
		isExcluded := false
		for _, excluded := range excludedSubscribers {
			if subscriber == excluded {
				isExcluded = true
				break
			}
		}
		if isExcluded {
			continue
		}
		s.enqueueLocked(subscriber, event)
	}
}

func (s *Session) PublishHeroResourceTo(
	resource HeroResource, subscriber Subscriber,
) {
	if s == nil || resource.ObjectID == 0 || subscriber.UserID == 0 ||
		subscriber.PeerGeneration == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, isFound := s.eventsBySubscriber[subscriber]; !isFound {
		return
	}
	s.next++
	s.enqueueLocked(subscriber, Event{
		Sequence: s.next, Kind: EventHeroResource, Resource: resource,
	})
}

func (s *Session) PublishCompanionDamage(
	damage CompanionDamage, excluded Subscriber,
) {
	if s == nil || damage.SourceObjectID == 0 || damage.TargetObjectID == 0 ||
		damage.Damage <= 0 || damage.HitPoint < 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	event := Event{
		Sequence: s.next, Kind: EventCompanionDamage, Companion: damage,
	}
	for subscriber := range s.eventsBySubscriber {
		if subscriber == excluded {
			continue
		}
		s.enqueueLocked(subscriber, event)
	}
}

func (s *Session) PublishCompanionResourceTo(
	resource CompanionResource, subscriber Subscriber,
) {
	if s == nil || resource.ObjectID == 0 || subscriber.UserID == 0 ||
		subscriber.PeerGeneration == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, isFound := s.eventsBySubscriber[subscriber]; !isFound {
		return
	}
	s.next++
	s.enqueueLocked(subscriber, Event{
		Sequence: s.next, Kind: EventCompanionResource,
		CompanionResource: resource,
	})
}

func (s *Session) Drain(subscriber Subscriber) []Event {
	event := s.Peek(subscriber)
	if len(event) == 0 {
		return nil
	}
	s.Commit(subscriber, event[len(event)-1].Sequence)
	return event
}

// Peek returns the subscriber's pending events without advancing its delivery
// cursor. The transport adapter commits only the prefix it has encoded.
func (s *Session) Peek(subscriber Subscriber) []Event {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	event := s.eventsBySubscriber[subscriber]
	if len(event) == 0 {
		return nil
	}
	return append([]Event(nil), event...)
}

// Commit advances one subscriber through the supplied event sequence while
// preserving any events published after the peek. A successful overlapping
// delivery may already have committed the same prefix, so stale completions
// remain idempotent.
func (s *Session) Commit(subscriber Subscriber, sequence uint64) bool {
	if s == nil || sequence == 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	event, isFound := s.eventsBySubscriber[subscriber]
	if !isFound || sequence > s.next {
		return false
	}
	if len(event) == 0 || event[0].Sequence > sequence {
		return true
	}
	committed := 0
	for committed < len(event) && event[committed].Sequence <= sequence {
		committed++
	}
	remaining := append([]Event(nil), event[committed:]...)
	s.eventsBySubscriber[subscriber] = remaining
	return true
}
