//go:build scenario

package blaze

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/darkspinnet/darkspin/server/blaze/tdf"
	"github.com/darkspinnet/darkspin/server/scenario"
)

// DebugParty reuses the registered production party handlers and their exact
// queued projections. No synthetic RPC reply is sent to a client that did not
// issue that RPC. The receipt proves server processing/delivery, not native UI
// acceptance. Only a pinned authenticated loopback actor can enter this seam.
func (e *Server) DebugParty(ctx context.Context, req scenario.PartyRequest) (scenario.PartyResult, error) {
	result := scenario.PartyResult{Operation: req.Operation, ActorID: req.ActorID, ActorSessionID: req.ActorSessionID}
	actor, err := e.debugPartySession(ctx, req.ActorID, req.ActorSessionID)
	if err != nil {
		return result, fmt.Errorf("partyActor: %w", err)
	}
	err = actor.lockOperation(ctx)
	if err != nil {
		return result, fmt.Errorf("partyStage: %w", err)
	}
	defer actor.operationMutex.Unlock()
	current, err := e.debugPartySession(ctx, req.ActorID, req.ActorSessionID)
	if err != nil {
		return result, fmt.Errorf("partyRecheck: %w", err)
	}
	if current != actor {
		return result, errors.New("party actor session changed before execution")
	}
	if req.Operation == "party.state" {
		return result, nil
	}
	user := requestUser(&Request{Session: actor})
	if user == nil {
		return result, errors.New("party actor binding disappeared before execution")
	}
	switch req.Operation {
	case "party.create", "party.join":
		if user.CurrentPlaygroupID() != 0 {
			return result, errors.New("party membership changed before execution; inspect state instead of replaying")
		}
	case "party.invite":
		if user.CurrentPlaygroupID() != req.PartyID {
			return result, errors.New("party inviter membership changed before execution")
		}
	}
	if req.InviteeID != 0 {
		invitee, targetErr := e.debugPartySession(ctx, req.InviteeID, req.InviteeSessionID)
		if targetErr != nil {
			return result, fmt.Errorf("partyInvitee: %w", targetErr)
		}
		if invitee == actor {
			return result, errors.New("party invitee must be a different owned session")
		}
	}
	if req.LeaderID != 0 {
		leader, leaderErr := e.debugPartySession(ctx, req.LeaderID, req.LeaderSessionID)
		if leaderErr != nil {
			return result, fmt.Errorf("partyLeader: %w", leaderErr)
		}
		if leader == actor {
			return result, errors.New("party join leader must be a different owned session")
		}
		leaderUser := requestUser(&Request{Session: leader})
		if leaderUser == nil || leaderUser.CurrentPlaygroupID() != req.PartyID {
			return result, errors.New("party leader membership changed before execution")
		}
	}
	request, err := debugPartyRequest(actor, req)
	if err != nil {
		return result, fmt.Errorf("partyRequest: %w", err)
	}
	request.isPartyInviteRecoverySuppressed = true
	request.ownedPartyUsers = make(map[int64]bool, len(req.OwnedUserIDs))
	for _, userID := range req.OwnedUserIDs {
		request.ownedPartyUsers[userID] = true
	}
	if !request.ownedPartyUsers[req.ActorID] {
		return result, errors.New("party actor is outside the owned client allowlist")
	}
	component, isComponent := e.registry.Component(request.Component)
	if !isComponent {
		return result, errors.New("production party component is unavailable")
	}
	handler, isHandler := component.Commands[request.Command]
	if !isHandler || handler == nil {
		return result, errors.New("production party operation is unavailable")
	}
	if isPlaygroupHandoffFrame(Frame{Component: request.Component, Command: request.Command}) {
		actor.pendingPlaygroupLeave = nil
	}
	result.IsMutationAttempted = true
	response, handlerErr := executeDebugPartyHandler(ctx, handler, request)
	if handlerErr == nil && (response == nil || response.ErrorCode != ErrorNone) {
		if response == nil {
			handlerErr = errors.New("production party handler did not return a result")
		} else {
			handlerErr = fmt.Errorf("production party operation rejected with code 0x%04x", response.ErrorCode)
		}
	}
	result.IsMutationAccepted = handlerErr == nil
	projectionErr := actor.deliverDebugParty(ctx, request, &result)
	if handlerErr == nil && result.NotificationCount == 0 {
		projectionErr = errors.Join(projectionErr, errors.New("accepted party operation produced no client projection"))
	}
	result.IsProjectionComplete = handlerErr == nil && projectionErr == nil
	if handlerErr != nil || projectionErr != nil {
		return result, fmt.Errorf("partyExecution: %w", errors.Join(handlerErr, projectionErr))
	}
	return result, nil
}

func (e *Server) debugPartySession(ctx context.Context, userID int64, identity string) (*Session, error) {
	session, err := e.debugPartyIndexedSession(ctx, userID, identity)
	if err != nil {
		return nil, fmt.Errorf("partyBinding: %w", err)
	}
	// Do not hold the connection index while consulting business/session data.
	// The caller revalidates the binding after acquiring the actor operation stage.
	user := requestUser(&Request{Session: session})
	if user == nil || user.Account.ID != userID {
		return nil, errors.New("party authenticated user binding is unavailable")
	}
	for _, address := range []net.Addr{session.conn.LocalAddr(), session.conn.RemoteAddr()} {
		if address == nil {
			return nil, errors.New("party peer address is unavailable")
		}
		host, port, addressErr := net.SplitHostPort(address.String())
		if addressErr != nil {
			return nil, fmt.Errorf("partyAddress: %w", addressErr)
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() || port == "" {
			return nil, errors.New("party development operations require actual loopback peers")
		}
	}
	return session, nil
}

func (e *Server) debugPartyIndexedSession(ctx context.Context, userID int64, identity string) (*Session, error) {
	if e == nil || userID <= 0 || identity == "" {
		return nil, errors.New("party operation requires a pinned authenticated user")
	}
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for !e.mu.TryLock() {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("partyIndexWait: %w", ctx.Err())
		case <-ticker.C:
		}
	}
	defer e.mu.Unlock()
	err := ctx.Err()
	if err != nil {
		return nil, fmt.Errorf("partyIndexContext: %w", err)
	}
	sessions := e.sessionsByUser[userID]
	if len(sessions) == 0 {
		return nil, scenario.ErrPartySessionMissing
	}
	if len(sessions) != 1 {
		return nil, errors.New("party user has ambiguous authenticated connections")
	}
	for sessionID, session := range sessions {
		if session == nil || session.conn == nil || session.server != e || e.sessions[sessionID] != session || session.userID != userID || fmt.Sprintf("blaze:%d/session:%d", e.serveGeneration, sessionID) != identity {
			return nil, errors.New("party authenticated session identity changed")
		}
		return session, nil
	}
	return nil, scenario.ErrPartySessionMissing
}

func debugPartyRequest(actor *Session, req scenario.PartyRequest) (*Request, error) {
	request := &Request{Session: actor, Component: PlaygroupsComponentID}
	switch req.Operation {
	case "party.create":
		request.Command = 0x01
		request.Fields = []tdf.Field{tdf.FieldNamed("PGRP", tdf.StructValue(tdf.FieldNamed("NAME", tdf.StringValue(req.Name)))), tdf.FieldNamed("JOIN", tdf.IntegerValue(1))}
	case "party.join":
		if req.PartyID == 0 || req.LeaderID <= 0 {
			return nil, errors.New("party join requires an owned leader's party")
		}
		request.Command = 0x03
		request.Fields = []tdf.Field{tdf.FieldNamed("PGID", tdf.IntegerValue(uint64(req.PartyID)))}
	case "party.invite":
		if req.PartyID == 0 || req.InviteeID <= 0 {
			return nil, errors.New("party invite requires an owned target and current party")
		}
		request.Component = MessagingComponentID
		request.Command = 0x01
		request.Fields = []tdf.Field{
			tdf.FieldNamed("ATTR", tdf.MapValue(tdf.Integer, tdf.String, tdf.MapEntry{Key: tdf.IntegerValue(720897), Value: tdf.StringValue(strconv.FormatUint(uint64(req.PartyID), 10))})),
			tdf.FieldNamed("TARG", tdf.Value{Type: tdf.ObjectID, ObjectID: [3]uint64{uint64(UserSessionComponentID), userSessionObjectType, uint64(req.InviteeID)}}),
			tdf.FieldNamed("TYPE", tdf.IntegerValue(messagingPartyInviteType)), tdf.FieldNamed("FLAG", tdf.IntegerValue(4)),
		}
	default:
		return nil, errors.New("unsupported development party operation")
	}
	return request, nil
}

func executeDebugPartyHandler(ctx context.Context, handler Handler, req *Request) (response *Response, err error) {
	defer func() {
		failure := recover()
		if failure != nil {
			err = fmt.Errorf("partyHandlerPanic: %v", failure)
		}
	}()
	response, err = handler(ctx, req)
	if err != nil {
		return response, fmt.Errorf("partyHandler: %w", err)
	}
	return response, nil
}

func (e *Session) deliverDebugParty(ctx context.Context, req *Request, result *scenario.PartyResult) error {
	result.NotificationCount = len(req.beforeReplyFrames) + len(req.notifications) + len(req.targetedNotifications)
	var failures []error
	for _, frames := range [][]Frame{req.beforeReplyFrames, req.notifications} {
		for _, frame := range frames {
			err := e.sendDebugParty(ctx, frame)
			if err != nil {
				failures = append(failures, fmt.Errorf("partyNotifyActor: %w", err))
				continue
			}
			result.DeliveredNotificationCount++
		}
	}
	for _, notification := range req.targetedNotifications {
		if notification.session == nil || notification.session.server == nil {
			failures = append(failures, errors.New("party notification target is unavailable"))
			continue
		}
		notification.session.server.mu.Lock()
		isOwned := req.ownedPartyUsers[notification.session.userID]
		targetUserID := notification.session.userID
		targetIdentity := fmt.Sprintf("blaze:%d/session:%d", notification.session.server.serveGeneration, notification.session.ID)
		notification.session.server.mu.Unlock()
		if !isOwned {
			failures = append(failures, errors.New("party notification target is outside the owned client allowlist"))
			continue
		}
		current, bindingErr := notification.session.server.debugPartySession(ctx, targetUserID, targetIdentity)
		if bindingErr != nil {
			failures = append(failures, fmt.Errorf("partyNotifyBinding: %w", bindingErr))
			continue
		}
		if current != notification.session {
			failures = append(failures, errors.New("party notification target session changed"))
			continue
		}
		err := notification.session.sendDebugParty(ctx, notification.frame)
		if err != nil {
			failures = append(failures, fmt.Errorf("partyNotifyTarget: %w", err))
			continue
		}
		result.DeliveredNotificationCount++
	}
	if len(failures) != 0 {
		return fmt.Errorf("partyProjection: %w", errors.Join(failures...))
	}
	return nil
}

func (e *Session) sendDebugParty(ctx context.Context, frame Frame) error {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for !e.write.TryLock() {
		select {
		case <-ctx.Done():
			return fmt.Errorf("partySendWait: %w", ctx.Err())
		case <-ticker.C:
		}
	}
	defer e.write.Unlock()
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("partySendContext: %w", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	contextDeadline, isDeadline := ctx.Deadline()
	if isDeadline && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	err = e.conn.SetWriteDeadline(deadline)
	if err != nil {
		return fmt.Errorf("partySendDeadline: %w", err)
	}
	e.traceFrame("out", frame)
	writeErr := WriteFrame(e.conn, frame)
	clearErr := e.conn.SetWriteDeadline(time.Time{})
	if writeErr != nil {
		// A partial Blaze frame cannot be retried or followed by another frame.
		closeErr := e.conn.Close()
		return fmt.Errorf("partySendFrame: %w", errors.Join(writeErr, clearErr, closeErr))
	}
	if clearErr != nil {
		return fmt.Errorf("partySendClear: %w", clearErr)
	}
	return nil
}
