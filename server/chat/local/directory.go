// Package local adapts the in-process player, lobby, and game registries to chat.
package local

import (
	"context"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/chat"
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/party"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

type partyDirectory interface {
	Lookup(uint32) (party.Snapshot, bool)
}

// directory resolves chat audiences from the monolithic server's active state.
type directory struct {
	userManager    *sporenet.UserManager
	roomManager    *sporenet.RoomManager
	gameManager    *game.Manager
	partyDirectory partyDirectory
}

// NewDirectory adapts the active user, room, and game managers to chat.
func NewDirectory(
	userManager *sporenet.UserManager, roomManager *sporenet.RoomManager,
	gameManager *game.Manager, partyDirectories ...partyDirectory,
) directory {
	var partyDirectory partyDirectory
	if len(partyDirectories) != 0 {
		partyDirectory = partyDirectories[0]
	}
	return directory{
		userManager: userManager, roomManager: roomManager,
		gameManager: gameManager, partyDirectory: partyDirectory,
	}
}

// Members returns a snapshot of the requested active audience.
func (d directory) Members(ctx context.Context, target chat.Target) ([]chat.Participant, error) {
	err := ctx.Err()
	if err != nil {
		return nil, fmt.Errorf("contextCheck: %w", err)
	}
	switch target.Scope {
	case chat.ScopeDirect:
		return d.directMembers(target.ID), nil
	case chat.ScopeParty:
		return d.partyMembers(target.ID), nil
	case chat.ScopeGame:
		return d.gameMembers(target.ID), nil
	case chat.ScopeLobby:
		return d.lobbyMembers(target.ID), nil
	case chat.ScopeGlobal:
		return d.globalMembers(), nil
	default:
		return nil, chat.ErrTargetType
	}
}

func (d directory) globalMembers() []chat.Participant {
	if d.userManager == nil {
		return nil
	}
	return participants(d.userManager.Users()...)
}

func (d directory) directMembers(id uint64) []chat.Participant {
	if d.userManager == nil || id > math.MaxInt64 {
		return nil
	}
	return participants(d.userManager.UserByID(int64(id)))
}

func (d directory) partyMembers(id uint64) []chat.Participant {
	if d.userManager == nil || id == 0 || id > math.MaxUint32 {
		return nil
	}
	if d.partyDirectory != nil {
		partySnapshot, isFound := d.partyDirectory.Lookup(uint32(id))
		if !isFound {
			return nil
		}
		members := make([]chat.Participant, 0, len(partySnapshot.Members))
		for _, partyMember := range partySnapshot.Members {
			user := d.userManager.UserByID(partyMember.ID)
			if user != nil {
				members = append(members, participant(user))
			}
		}
		return members
	}
	members := make([]chat.Participant, 0)
	for _, user := range d.userManager.Users() {
		if user.CurrentPlaygroupID() == uint32(id) {
			members = append(members, participant(user))
		}
	}
	return members
}

func (d directory) gameMembers(id uint64) []chat.Participant {
	if d.gameManager == nil || id == 0 || id > math.MaxUint32 {
		return nil
	}
	instance := d.gameManager.Game(uint32(id))
	if instance == nil {
		return nil
	}
	return participants(instance.Players()...)
}

func (d directory) lobbyMembers(id uint64) []chat.Participant {
	if d.roomManager == nil || id == 0 || id > math.MaxUint32 {
		return nil
	}
	room := d.roomManager.Room(uint32(id))
	if room == nil {
		return nil
	}
	return participants(room.Users()...)
}

func participants(users ...*sporenet.User) []chat.Participant {
	members := make([]chat.Participant, 0, len(users))
	for _, user := range users {
		if user != nil {
			members = append(members, participant(user))
		}
	}
	return members
}

func participant(user *sporenet.User) chat.Participant {
	return chat.Participant{ID: user.Account.ID, Name: user.DisplayName}
}
