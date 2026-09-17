package blaze

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"

	"github.com/darkspinnet/darkspin/server/blaze/tdf"
	"github.com/darkspinnet/darkspin/server/databuffer"
)

const (
	ErrorNone              uint16 = 0
	ErrorSystem            uint16 = 0x4001
	ErrorComponentNotFound uint16 = 0x4002
	ErrorCommandNotFound   uint16 = 0x4003
)

// Request is a decoded Blaze RPC request.
type Request struct {
	Session               *Session
	Component             uint16
	Command               uint16
	MessageID             uint32
	Fields                []tdf.Field
	beforeReplyFrames     []Frame
	notifications         []Frame
	targetedNotifications []targetedNotification
}

type targetedNotification struct {
	session *Session
	frame   Frame
}

// QueueNotification schedules a notification after the request reply.
func (r *Request) QueueNotification(component, command uint16, fields []tdf.Field) error {
	frame, err := encodeNotification(component, command, fields)
	if err != nil {
		return fmt.Errorf("notificationQueue: %w", err)
	}
	r.notifications = append(r.notifications, frame)
	return nil
}

// QueueNotificationBeforeReply schedules a notification before the request
// reply. Some Blaze SDK callbacks immediately resolve objects installed by an
// earlier notification and cannot consume those objects from the reply alone.
func (r *Request) QueueNotificationBeforeReply(component, command uint16, fields []tdf.Field) error {
	frame, err := encodeNotification(component, command, fields)
	if err != nil {
		return fmt.Errorf("notificationBeforeReply: %w", err)
	}
	r.beforeReplyFrames = append(r.beforeReplyFrames, frame)
	return nil
}

// QueueUserNotification schedules a post-reply notification for every Blaze
// session authenticated as the requested user.
func (r *Request) QueueUserNotification(userID int64, component, command uint16, fields []tdf.Field) error {
	if r.Session == nil || r.Session.server == nil {
		return fmt.Errorf("notificationSession: %w", ErrSessionUnavailable)
	}
	frame, err := encodeNotification(component, command, fields)
	if err != nil {
		return fmt.Errorf("userNotification: %w", err)
	}
	for _, session := range r.Session.server.sessionsForUser(userID) {
		r.targetedNotifications = append(r.targetedNotifications, targetedNotification{session: session, frame: frame})
	}
	return nil
}

// QueueSessionNotification schedules a post-reply notification for one exact
// Blaze session when its projection depends on that connection's network path.
func (r *Request) QueueSessionNotification(
	session *Session, component, command uint16, fields []tdf.Field,
) error {
	if session == nil {
		return fmt.Errorf("notificationTarget: %w", ErrSessionUnavailable)
	}
	frame, err := encodeNotification(component, command, fields)
	if err != nil {
		return fmt.Errorf("sessionNotification: %w", err)
	}
	r.targetedNotifications = append(
		r.targetedNotifications, targetedNotification{session: session, frame: frame},
	)
	return nil
}

func encodeNotification(component, command uint16, fields []tdf.Field) (Frame, error) {
	payload := databuffer.New()
	err := tdf.Encode(payload, fields)
	if err != nil {
		return Frame{}, fmt.Errorf("notificationEncode[%04x/%04x]: %w", component, command, err)
	}
	return Frame{
		Component: component, Command: command, Type: Notification, Payload: payload.Bytes(),
	}, nil
}

// Response is encoded as a reply to a request.
type Response struct {
	Fields    []tdf.Field
	ErrorCode uint16
}

// Handler implements one Blaze component command.
type Handler func(context.Context, *Request) (*Response, error)

// Component groups handlers under a Blaze component ID.
type Component struct {
	ID       uint16
	Name     string
	Commands map[uint16]Handler
}

// Registry owns all Blaze component command handlers.
type Registry struct {
	mu         sync.RWMutex
	components map[uint16]Component
}

// NewRegistry returns an empty component registry.
func NewRegistry() *Registry {
	return &Registry{components: make(map[uint16]Component)}
}

// Register adds or replaces a complete component definition.
func (r *Registry) Register(component Component) error {
	if component.ID == 0 {
		return fmt.Errorf("register Blaze component %q: ID is zero", component.Name)
	}
	if component.Commands == nil {
		component.Commands = make(map[uint16]Handler)
	}
	r.mu.Lock()
	r.components[component.ID] = component
	r.mu.Unlock()
	return nil
}

// Component returns a snapshot of a registered component.
func (r *Registry) Component(id uint16) (Component, bool) {
	r.mu.RLock()
	component, isFound := r.components[id]
	r.mu.RUnlock()
	return component, isFound
}

type dispatchResult struct {
	reply                 Frame
	beforeReplyFrames     []Frame
	notifications         []Frame
	targetedNotifications []targetedNotification
}

func (r *Registry) dispatch(
	ctx context.Context, session *Session, frame Frame,
) (result dispatchResult) {
	reply := Frame{
		Component: frame.Component,
		Command:   frame.Command,
		Type:      Reply,
		MessageID: frame.MessageID,
	}
	defer recoverDispatch(session, frame, reply, &result)

	component, isFound := r.Component(frame.Component)
	if !isFound {
		reply.Type = ErrorReply
		reply.ErrorCode = ErrorComponentNotFound
		return dispatchResult{reply: reply}
	}
	handler, isFound := component.Commands[frame.Command]
	if !isFound || handler == nil {
		reply.Type = ErrorReply
		reply.ErrorCode = ErrorCommandNotFound
		return dispatchResult{reply: reply}
	}

	fields, err := tdf.Decode(databuffer.FromBytes(frame.Payload))
	if err != nil {
		reply.Type = ErrorReply
		reply.ErrorCode = ErrorSystem
		return dispatchResult{reply: reply}
	}
	request := &Request{
		Session:   session,
		Component: frame.Component,
		Command:   frame.Command,
		MessageID: frame.MessageID,
		Fields:    fields,
	}
	response, err := handler(ctx, request)
	if err != nil {
		reply.Type = ErrorReply
		reply.ErrorCode = ErrorSystem
		return dispatchResult{reply: reply}
	}
	if response == nil {
		return dispatchResult{
			reply: reply, beforeReplyFrames: request.beforeReplyFrames,
			notifications: request.notifications, targetedNotifications: request.targetedNotifications,
		}
	}
	if response.ErrorCode != ErrorNone {
		reply.Type = ErrorReply
		reply.ErrorCode = response.ErrorCode
	}
	payload := databuffer.New()
	err = tdf.Encode(payload, response.Fields)
	if err != nil {
		reply.Type = ErrorReply
		reply.ErrorCode = ErrorSystem
		return dispatchResult{reply: reply}
	}
	reply.Payload = payload.Bytes()
	return dispatchResult{
		reply: reply, beforeReplyFrames: request.beforeReplyFrames,
		notifications: request.notifications, targetedNotifications: request.targetedNotifications,
	}
}

func recoverDispatch(
	session *Session, frame Frame, reply Frame, result *dispatchResult,
) {
	recovered := recover()
	if recovered == nil {
		return
	}
	reply.Type = ErrorReply
	reply.ErrorCode = ErrorSystem
	reply.Payload = nil
	*result = dispatchResult{reply: reply}
	if session == nil || session.server == nil || session.server.logger == nil {
		return
	}
	session.server.logger.Printf(
		"Blaze handler panic session=%d component=%d command=%d message_id=%d: %v\n%s",
		session.ID, frame.Component, frame.Command, frame.MessageID,
		recovered, debug.Stack(),
	)
}
