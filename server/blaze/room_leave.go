package blaze

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/blaze/tdf"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

func roomsLeaveHandler(roomManager *sporenet.RoomManager) Handler {
	return func(_ context.Context, req *Request) (*Response, error) {
		user := requestUser(req)
		if user == nil {
			return &Response{ErrorCode: authInvalidUser}, nil
		}
		result, err := roomManager.LeaveRoom(sporenet.RoomLeaveRequest{
			User: user, RoomID: uint32(fieldInteger(req.Fields, "RMID")),
		})
		if errors.Is(err, sporenet.ErrRoomLeaveNotFound) {
			return &Response{ErrorCode: roomsErrorNotFound}, nil
		}
		if err != nil {
			return nil, fmt.Errorf("roomLeave: %w", err)
		}
		if result.IsMemberRemoved {
			fields := []tdf.Field{
				tdf.FieldNamed("MBID", tdf.IntegerValue(uint64(result.MemberID))),
				tdf.FieldNamed("RMID", tdf.IntegerValue(uint64(result.RoomID))),
			}
			for index, recipientID := range result.RecipientIDs {
				err = req.QueueUserNotification(recipientID, RoomsComponentID, 0x33, fields)
				if err != nil {
					return nil, fmt.Errorf("roomLeaveNotify[%d]: %w", index, err)
				}
			}
		}
		// Build 103 LeaveRoomResponse, sub_E3F410, reads CTID and RMID.
		return &Response{Fields: []tdf.Field{
			tdf.FieldNamed("CTID", tdf.IntegerValue(uint64(result.CategoryID))),
			tdf.FieldNamed("RMID", tdf.IntegerValue(uint64(result.RoomID))),
		}}, nil
	}
}
