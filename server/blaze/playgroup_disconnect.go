package blaze

import (
	"context"

	"github.com/darkspinnet/darkspin/server/party"
)

// A leave RPC has no reason field. Preserve actual new game/party handoffs,
// but honor a leave followed by shutdown even when cleanup RPCs trail it.
type playgroupDisconnectLeave struct {
	partyService    *party.Service
	networkRegistry *playgroupNetworkRegistry
	partyID         uint32
	userID          int64
}

func isPlaygroupHandoffFrame(frame Frame) bool {
	if frame.Component == PlaygroupsComponentID {
		return frame.Command == 0x01 || frame.Command == 0x03
	}
	if frame.Component != GameManagerComponentID {
		return false
	}
	switch frame.Command {
	case 0x01, 0x03, 0x09, 0x0d, 0x0f, 0x19:
		return true
	default:
		return false
	}
}

func (e *Session) completeDisconnectedPlaygroupLeave(ctx context.Context) {
	pending := e.pendingPlaygroupLeave
	e.pendingPlaygroupLeave = nil
	if pending == nil || e.server == nil {
		return
	}
	if len(e.server.sessionsForUser(pending.userID)) != 0 {
		return
	}
	before, isFound := pending.partyService.Lookup(pending.partyID)
	if !isFound {
		return
	}
	req := &Request{Session: e}
	response, err := completePlaygroupLeave(
		ctx, req, pending.partyService, pending.networkRegistry, before, pending.userID,
	)
	if err != nil {
		e.server.logger.Printf("Disconnected playgroup leave failed user=%d party=%d: %v",
			pending.userID, pending.partyID, err)
	}
	// Deliver already-encoded notifications even if a later encoding failed.
	for _, notification := range req.targetedNotifications {
		target := notification.session
		target.traceFrame("out", notification.frame)
		sendErr := target.send(notification.frame)
		if sendErr != nil {
			e.server.logger.Printf("Disconnected playgroup notification failed user=%d recipient=%d: %v",
				pending.userID, target.ID, sendErr)
		}
	}
	if err == nil && response != nil && response.ErrorCode != 0 {
		e.server.logger.Printf("Disconnected playgroup leave rejected user=%d party=%d code=%d",
			pending.userID, pending.partyID, response.ErrorCode)
	}
}
