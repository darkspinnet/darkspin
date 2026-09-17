// Package party owns ship-party membership and invitation authority.
package party

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	// MaximumMembers is the build-103 ship-party capacity.
	MaximumMembers        = 4
	defaultInviteLifetime = 30 * time.Second
)

var (
	ErrAlreadyMember    = errors.New("party member already joined")
	ErrClosed           = errors.New("party is closed")
	ErrFull             = errors.New("party is full")
	ErrInviteMissing    = errors.New("party invitation missing")
	ErrMemberMissing    = errors.New("party member missing")
	ErrNotAuthorized    = errors.New("party operation not authorized")
	ErrPartyMissing     = errors.New("party missing")
	ErrProjectionFailed = errors.New("party membership projection failed")
)

// Member is the transport-neutral public identity retained by a party.
type Member struct {
	ID          int64
	Name        string
	AvatarID    uint32
	JoinOrdinal uint32
	JoinedAt    time.Time
}

// Snapshot is an immutable view of one party revision.
type Snapshot struct {
	ID          uint32
	Name        string
	LeaderID    int64
	Revision    uint64
	JoinControl uint8
	Members     []Member
}

// Membership projects committed party ownership onto active user sessions.
type Membership interface {
	ClaimParty(int64, uint32) bool
	ReleaseParty(int64, uint32)
}

// CreateRequest creates or returns the actor's current party.
type CreateRequest struct {
	Actor       Member
	Name        string
	JoinControl uint8
}

// InviteRequest records one direct native invitation.
type InviteRequest struct {
	PartyID  uint32
	Inviter  Member
	Invitee  Member
	Duration time.Duration
}

// JoinRequest accepts a pending invitation.
type JoinRequest struct {
	PartyID uint32
	Actor   Member
}

// MemberRequest applies an operation to the actor's current party.
type MemberRequest struct {
	PartyID uint32
	ActorID int64
}

type invitation struct {
	inviterID int64
	expiresAt time.Time
}

type aggregate struct {
	id          uint32
	name        string
	leaderID    int64
	revision    uint64
	joinControl uint8
	nextOrdinal uint32
	members     map[int64]Member
	invites     map[int64]invitation
}

// Service serializes all in-memory party mutations and their user projection.
type Service struct {
	membership Membership
	now        func() time.Time

	mu            sync.RWMutex
	nextID        uint32
	parties       map[uint32]*aggregate
	partyIDByUser map[int64]uint32
}

// NewService creates an empty process-lifetime party service.
func NewService(membership Membership) *Service {
	return &Service{
		membership:    membership,
		now:           time.Now,
		nextID:        1,
		parties:       make(map[uint32]*aggregate),
		partyIDByUser: make(map[int64]uint32),
	}
}

// Create creates a party for an ungrouped actor or returns its current party.
func (e *Service) Create(ctx context.Context, req CreateRequest) (Snapshot, error) {
	err := ctx.Err()
	if err != nil {
		return Snapshot{}, fmt.Errorf("createContext: %w", err)
	}
	if req.Actor.ID <= 0 {
		return Snapshot{}, fmt.Errorf("createActor: %w", ErrMemberMissing)
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if partyID := e.partyIDByUser[req.Actor.ID]; partyID != 0 {
		party := e.parties[partyID]
		if party == nil {
			delete(e.partyIDByUser, req.Actor.ID)
		} else {
			return snapshot(party), nil
		}
	}

	partyID := e.nextID
	e.nextID++
	if partyID == 0 {
		partyID = e.nextID
		e.nextID++
	}
	if req.Name == "" {
		req.Name = req.Actor.Name + "'s Playgroup"
	}
	if req.JoinControl == 0 {
		req.JoinControl = 1
	}
	joinedAt := e.now()
	member := req.Actor
	member.JoinOrdinal = 1
	member.JoinedAt = joinedAt
	party := &aggregate{
		id: partyID, name: req.Name, leaderID: member.ID, revision: 1,
		joinControl: req.JoinControl, nextOrdinal: 2,
		members: map[int64]Member{member.ID: member}, invites: make(map[int64]invitation),
	}
	if e.membership != nil {
		if !e.membership.ClaimParty(member.ID, partyID) {
			return Snapshot{}, fmt.Errorf("createClaim: %w", ErrProjectionFailed)
		}
	}
	e.parties[partyID] = party
	e.partyIDByUser[member.ID] = partyID
	return snapshot(party), nil
}

// Invite records the authoritative deadline for a native direct invitation.
func (e *Service) Invite(ctx context.Context, req InviteRequest) (Snapshot, error) {
	err := ctx.Err()
	if err != nil {
		return Snapshot{}, fmt.Errorf("inviteContext: %w", err)
	}
	if req.Inviter.ID <= 0 || req.Invitee.ID <= 0 || req.Inviter.ID == req.Invitee.ID {
		return Snapshot{}, fmt.Errorf("inviteMember: %w", ErrMemberMissing)
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	party := e.parties[req.PartyID]
	if party == nil {
		return Snapshot{}, fmt.Errorf("inviteParty: %w", ErrPartyMissing)
	}
	if _, isMember := party.members[req.Inviter.ID]; !isMember {
		return Snapshot{}, fmt.Errorf("inviteAuthority: %w", ErrNotAuthorized)
	}
	if _, isMember := party.members[req.Invitee.ID]; isMember || e.partyIDByUser[req.Invitee.ID] != 0 {
		return Snapshot{}, fmt.Errorf("inviteJoined: %w", ErrAlreadyMember)
	}
	if len(party.members) >= MaximumMembers {
		return Snapshot{}, fmt.Errorf("inviteCapacity: %w", ErrFull)
	}
	if party.joinControl == 0 {
		return Snapshot{}, fmt.Errorf("inviteControl: %w", ErrClosed)
	}
	duration := req.Duration
	if duration <= 0 {
		duration = defaultInviteLifetime
	}
	party.invites[req.Invitee.ID] = invitation{inviterID: req.Inviter.ID, expiresAt: e.now().Add(duration)}
	return snapshot(party), nil
}

// Join atomically consumes an invitation and adds the actor to the party.
func (e *Service) Join(ctx context.Context, req JoinRequest) (Snapshot, error) {
	err := ctx.Err()
	if err != nil {
		return Snapshot{}, fmt.Errorf("joinContext: %w", err)
	}
	if req.Actor.ID <= 0 {
		return Snapshot{}, fmt.Errorf("joinActor: %w", ErrMemberMissing)
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	party := e.parties[req.PartyID]
	if party == nil {
		return Snapshot{}, fmt.Errorf("joinParty: %w", ErrPartyMissing)
	}
	if _, isMember := party.members[req.Actor.ID]; isMember {
		return snapshot(party), nil
	}
	if e.partyIDByUser[req.Actor.ID] != 0 {
		return Snapshot{}, fmt.Errorf("joinExisting: %w", ErrAlreadyMember)
	}
	invite, isInvited := party.invites[req.Actor.ID]
	if !isInvited || !e.now().Before(invite.expiresAt) {
		delete(party.invites, req.Actor.ID)
		return Snapshot{}, fmt.Errorf("joinInvite: %w", ErrInviteMissing)
	}
	if party.joinControl == 0 {
		return Snapshot{}, fmt.Errorf("joinControl: %w", ErrClosed)
	}
	if len(party.members) >= MaximumMembers {
		return Snapshot{}, fmt.Errorf("joinCapacity: %w", ErrFull)
	}
	member := req.Actor
	member.JoinOrdinal = party.nextOrdinal
	member.JoinedAt = e.now()
	if e.membership != nil {
		if !e.membership.ClaimParty(member.ID, party.id) {
			return Snapshot{}, fmt.Errorf("joinClaim: %w", ErrProjectionFailed)
		}
	}
	party.nextOrdinal++
	party.revision++
	party.members[member.ID] = member
	delete(party.invites, member.ID)
	e.partyIDByUser[member.ID] = party.id
	return snapshot(party), nil
}

// Leave removes one actor and promotes the earliest surviving member.
func (e *Service) Leave(ctx context.Context, req MemberRequest) (Snapshot, error) {
	err := ctx.Err()
	if err != nil {
		return Snapshot{}, fmt.Errorf("leaveContext: %w", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	party := e.parties[req.PartyID]
	if party == nil {
		return Snapshot{}, fmt.Errorf("leaveParty: %w", ErrPartyMissing)
	}
	if _, isMember := party.members[req.ActorID]; !isMember {
		return Snapshot{}, fmt.Errorf("leaveMember: %w", ErrMemberMissing)
	}
	if e.membership != nil {
		e.membership.ReleaseParty(req.ActorID, party.id)
	}
	delete(party.members, req.ActorID)
	delete(e.partyIDByUser, req.ActorID)
	party.revision++
	if len(party.members) == 0 {
		delete(e.parties, party.id)
		return Snapshot{ID: req.PartyID, Revision: party.revision}, nil
	}
	if party.leaderID == req.ActorID {
		party.leaderID = earliestMemberID(party.members)
	}
	return snapshot(party), nil
}

// Destroy removes a leader-owned party and releases every member projection.
func (e *Service) Destroy(ctx context.Context, req MemberRequest) (Snapshot, error) {
	err := ctx.Err()
	if err != nil {
		return Snapshot{}, fmt.Errorf("destroyContext: %w", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	party := e.parties[req.PartyID]
	if party == nil {
		return Snapshot{}, fmt.Errorf("destroyParty: %w", ErrPartyMissing)
	}
	if party.leaderID != req.ActorID {
		return Snapshot{}, fmt.Errorf("destroyAuthority: %w", ErrNotAuthorized)
	}
	for _, member := range party.members {
		if e.membership != nil {
			e.membership.ReleaseParty(member.ID, party.id)
		}
	}
	for memberID := range party.members {
		delete(e.partyIDByUser, memberID)
	}
	party.revision++
	delete(e.parties, party.id)
	return Snapshot{ID: req.PartyID, Revision: party.revision}, nil
}

// Lookup returns one party after expiring stale invitations.
func (e *Service) Lookup(partyID uint32) (Snapshot, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	party := e.parties[partyID]
	if party == nil {
		return Snapshot{}, false
	}
	e.expireInvites(party)
	return snapshot(party), true
}

// PartyForMember returns the actor's current authoritative party.
func (e *Service) PartyForMember(memberID int64) (Snapshot, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	party := e.parties[e.partyIDByUser[memberID]]
	if party == nil {
		return Snapshot{}, false
	}
	return snapshot(party), true
}

// ReconcileMember restores the active-session projection for an authoritative
// party member after transport logout and login replaced the user aggregate.
// Party membership itself remains unchanged and process-local.
func (e *Service) ReconcileMember(memberID int64) bool {
	if e == nil || memberID <= 0 {
		return false
	}
	e.mu.RLock()
	partyID := e.partyIDByUser[memberID]
	party := e.parties[partyID]
	isMember := false
	if party != nil {
		_, isMember = party.members[memberID]
	}
	membership := e.membership
	e.mu.RUnlock()
	if party == nil || !isMember || membership == nil {
		return false
	}
	return membership.ClaimParty(memberID, partyID)
}

func (e *Service) expireInvites(party *aggregate) {
	now := e.now()
	for memberID, invite := range party.invites {
		if !now.Before(invite.expiresAt) {
			delete(party.invites, memberID)
		}
	}
}

func snapshot(party *aggregate) Snapshot {
	members := make([]Member, 0, len(party.members))
	for ordinal := uint32(1); ordinal < party.nextOrdinal; ordinal++ {
		for _, member := range party.members {
			if member.JoinOrdinal == ordinal {
				members = append(members, member)
				break
			}
		}
	}
	return Snapshot{
		ID: party.id, Name: party.name, LeaderID: party.leaderID, Revision: party.revision,
		JoinControl: party.joinControl, Members: members,
	}
}

func earliestMemberID(members map[int64]Member) int64 {
	var earliest Member
	for _, member := range members {
		if earliest.ID == 0 || member.JoinOrdinal < earliest.JoinOrdinal {
			earliest = member
		}
	}
	return earliest.ID
}
