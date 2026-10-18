package sporenet

import "errors"

var ErrRoomLeaveActorInvalid = errors.New("room leave actor invalid")
var ErrRoomLeaveNotFound = errors.New("room leave room not found")

type RoomLeaveRequest struct {
	User   *User
	RoomID uint32
}

type RoomLeaveResult struct {
	RoomID          uint32
	CategoryID      uint32
	MemberID        int64
	RecipientIDs    []int64
	IsMemberRemoved bool
}

// LeaveRoom removes only the authenticated profile instance from the named
// lobby. Repeated leaves succeed without notifying members a second time.
func (e *RoomManager) LeaveRoom(req RoomLeaveRequest) (RoomLeaveResult, error) {
	if req.User == nil {
		return RoomLeaveResult{}, ErrRoomLeaveActorInvalid
	}
	room := e.Room(req.RoomID)
	if room == nil {
		return RoomLeaveResult{}, ErrRoomLeaveNotFound
	}
	room.mu.Lock()
	result := RoomLeaveResult{
		RoomID: room.ID, CategoryID: room.CategoryID,
		MemberID: req.User.Account.ID,
	}
	if room.users[req.User.Account.ID] != req.User {
		room.mu.Unlock()
		return result, nil
	}
	result.RecipientIDs = make([]int64, 0, len(room.users))
	for memberID := range room.users {
		result.RecipientIDs = append(result.RecipientIDs, memberID)
	}
	delete(room.users, req.User.Account.ID)
	result.IsMemberRemoved = true
	room.mu.Unlock()
	req.User.mu.Lock()
	if req.User.RoomID == req.RoomID {
		req.User.RoomID = 0
	}
	req.User.mu.Unlock()
	return result, nil
}
