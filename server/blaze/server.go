package blaze

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/darkspinnet/darkspin/server/blaze/tdf"
	"github.com/darkspinnet/darkspin/server/databuffer"
	"github.com/darkspinnet/darkspin/server/protocoltrace"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

var nextSessionID atomic.Uint32

const (
	requestReplayLimit      = 64
	requestReplayWindow     = 10 * time.Second
	defaultHandshakeTimeout = 10 * time.Second
	defaultReadTimeout      = 2 * time.Minute
	defaultWriteTimeout     = 15 * time.Second
	sessionShutdownWait     = 5 * time.Second
)

// ErrSessionUnavailable indicates that a notification has no live Blaze
// session directory through which it can be delivered.
var ErrSessionUnavailable = errors.New("blaze session unavailable")

// Server accepts Blaze RPC client connections.
type Server struct {
	address          string
	registry         *Registry
	tlsConfig        *tls.Config
	logger           *log.Logger
	recorder         protocoltrace.Recorder
	handshakeTimeout time.Duration
	readTimeout      time.Duration
	writeTimeout     time.Duration

	mu              sync.Mutex
	listener        net.Listener
	cancel          context.CancelFunc
	serveGeneration uint64
	sessions        map[uint32]*Session
	sessionIdle     chan struct{}
	sessionsByUser  map[int64]map[uint32]*Session
}

// Session is one connected Blaze client.
type Session struct {
	ID                    uint32
	server                *Server
	conn                  net.Conn
	write                 sync.Mutex
	data                  sync.Map
	userID                int64
	account               string
	displayName           string
	recentRequests        map[requestKey]requestRecord
	pendingPlaygroupLeave *playgroupDisconnectLeave
}

type requestKey struct {
	component uint16
	command   uint16
	messageID uint32
}

type requestRecord struct {
	digest           [sha256.Size]byte
	time             time.Time
	reply            Frame
	isReplyAvailable bool
}

// NewServer creates a Blaze server. A nil TLS config runs a plain TCP listener,
// which is useful for protocol tests and non-encrypted auxiliary endpoints.
func NewServer(host string, port uint16, registry *Registry, tlsConfig *tls.Config, logger *log.Logger, recorder protocoltrace.Recorder) *Server {
	if registry == nil {
		registry = NewRegistry()
	}
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	return &Server{
		address:          net.JoinHostPort(host, fmt.Sprintf("%d", port)),
		registry:         registry,
		tlsConfig:        tlsConfig,
		logger:           logger,
		recorder:         recorder,
		handshakeTimeout: defaultHandshakeTimeout,
		readTimeout:      defaultReadTimeout,
		writeTimeout:     defaultWriteTimeout,
		sessions:         make(map[uint32]*Session),
		sessionsByUser:   make(map[int64]map[uint32]*Session),
	}
}

// ListenAndServe runs until context cancellation or a listener failure.
func (s *Server) ListenAndServe(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.address)
	if err != nil {
		return fmt.Errorf("blazeListen[%s]: %w", s.address, err)
	}
	err = s.Serve(ctx, listener)
	if err != nil {
		return fmt.Errorf("blazeServe: %w", err)
	}
	return nil
}

// Serve accepts Blaze sessions from an existing listener. The listener is
// closed when the context ends or the server stops.
func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	if listener == nil {
		return errors.New("nil listener")
	}
	serveCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if s.tlsConfig != nil {
		listener = tls.NewListener(listener, s.tlsConfig)
	}
	s.mu.Lock()
	s.serveGeneration++
	if s.serveGeneration == 0 {
		s.serveGeneration++
	}
	serveGeneration := s.serveGeneration
	s.listener = listener
	s.cancel = cancel
	s.mu.Unlock()
	defer s.closeServe(serveGeneration)

	go func() {
		<-serveCtx.Done()
		_ = s.closeServe(serveGeneration)
	}()

	for {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			if serveCtx.Err() != nil || errors.Is(acceptErr, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("blazeAccept: %w", acceptErr)
		}
		if serveCtx.Err() != nil {
			_ = connection.Close()
			return nil
		}
		session := &Session{ID: nextSessionID.Add(1), server: s, conn: connection}
		if !s.addSessionForServe(serveCtx, serveGeneration, session) {
			_ = connection.Close()
			return nil
		}
		s.traceConnection(session, "open")
		go session.serve(serveCtx)
	}
}

// Close stops the listener and all active client sessions.
func (s *Server) Close() error {
	return s.closeServe(0)
}

func (s *Server) closeServe(expectedGeneration uint64) error {
	s.mu.Lock()
	if expectedGeneration != 0 && s.serveGeneration != expectedGeneration {
		s.mu.Unlock()
		return nil
	}
	listener := s.listener
	cancel := s.cancel
	s.listener = nil
	s.cancel = nil
	s.serveGeneration++
	if s.serveGeneration == 0 {
		s.serveGeneration++
	}
	sessions := make([]*Session, 0, len(s.sessions))
	for _, session := range s.sessions {
		sessions = append(sessions, session)
	}
	sessionIdle := s.sessionIdle
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	for _, session := range sessions {
		_ = session.conn.Close()
	}
	if len(sessions) != 0 && sessionIdle != nil {
		timer := time.NewTimer(sessionShutdownWait)
		select {
		case <-sessionIdle:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-timer.C:
			s.logger.Printf(
				"Blaze shutdown continuing with %d active sessions after %s",
				len(sessions), sessionShutdownWait,
			)
		}
	}
	if listener == nil {
		return nil
	}
	err := listener.Close()
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return fmt.Errorf("blazeClose: %w", err)
}

// Set stores arbitrary session state for component handlers.
func (s *Session) Set(key string, value any) {
	s.data.Store(key, value)
	if key != sessionUserKey || s.server == nil {
		return
	}
	user, isValid := value.(*sporenet.User)
	if !isValid || user == nil {
		return
	}
	s.account = user.LoginName
	s.displayName = user.DisplayName
	s.server.bindUserSession(s, user.Account.ID)
}

// Get retrieves arbitrary session state.
func (s *Session) Get(key string) (any, bool) {
	return s.data.Load(key)
}

// Delete removes arbitrary session state and updates indexes owned by special
// session keys.
func (s *Session) Delete(key string) {
	s.data.Delete(key)
	if key != sessionUserKey || s.server == nil {
		return
	}
	s.server.unbindUserSession(s)
}

// Notify sends an unsolicited Blaze notification.
func (s *Session) Notify(component, command uint16, fields []tdf.Field) error {
	payload := databuffer.New()
	err := tdf.Encode(payload, fields)
	if err != nil {
		return fmt.Errorf("notifyEncode[%04x/%04x]: %w", component, command, err)
	}
	err = s.send(Frame{
		Component: component,
		Command:   command,
		Type:      Notification,
		Payload:   payload.Bytes(),
	})
	if err != nil {
		return fmt.Errorf("notifySend[%04x/%04x]: %w", component, command, err)
	}
	return nil
}

func (s *Session) serve(ctx context.Context) {
	defer func() {
		s.logPlayerDisconnect()
		s.server.traceConnection(s, "close")
		_ = s.conn.Close()
		s.server.removeSession(s.ID)
		s.completeDisconnectedPlaygroupLeave(context.WithoutCancel(ctx))
	}()
	err := s.prepareConnection(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.server.logger.Printf("Blaze session %d handshake failed: %v", s.ID, err)
		}
		return
	}
	for {
		frame, err := s.readFrame()
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) && ctx.Err() == nil {
				s.server.logger.Printf("Blaze session %d read failed: %v", s.ID, err)
			}
			return
		}
		s.traceFrame("in", frame)
		if s.isRequestReplay(frame, time.Now()) {
			s.traceReplay(frame)
			reply, isFound := s.cachedRequestReply(frame)
			if isFound {
				s.traceFrame("out", reply)
				err = s.send(reply)
				if err != nil {
					s.server.logger.Printf("Blaze session %d replay write failed: %v", s.ID, err)
					return
				}
				s.server.logger.Printf(
					"Blaze session %d replayed duplicate response component=%d command=%d message_id=%d",
					s.ID, frame.Component, frame.Command, frame.MessageID,
				)
				continue
			}
		}
		// Cleanup/attribute RPCs can trail LeavePlaygroup during shutdown.
		// Only an actual new game/party transition supersedes that leave.
		if isPlaygroupHandoffFrame(frame) {
			s.pendingPlaygroupLeave = nil
		}
		result := s.server.registry.dispatch(ctx, s, frame)
		for _, notification := range result.beforeReplyFrames {
			s.traceFrame("out", notification)
			err = s.send(notification)
			if err != nil {
				s.server.logger.Printf("Blaze session %d pre-reply notification failed: %v", s.ID, err)
				return
			}
		}
		s.cacheRequestReply(frame, result.reply)
		s.traceFrame("out", result.reply)
		err = s.send(result.reply)
		if err != nil {
			s.server.logger.Printf("Blaze session %d write failed: %v", s.ID, err)
			return
		}
		for _, notification := range result.notifications {
			s.traceFrame("out", notification)
			err = s.send(notification)
			if err != nil {
				s.server.logger.Printf("Blaze session %d notification failed: %v", s.ID, err)
				return
			}
		}
		for _, notification := range result.targetedNotifications {
			target := notification.session
			if target == nil {
				continue
			}
			target.traceFrame("out", notification.frame)
			err = target.send(notification.frame)
			if err != nil {
				s.server.logger.Printf("Blaze target session %d notification failed: %v", target.ID, err)
				if target == s {
					return
				}
			}
		}
	}
}

func (s *Session) prepareConnection(ctx context.Context) error {
	tlsConnection, isTLS := s.conn.(*tls.Conn)
	if !isTLS {
		return nil
	}
	timeout := defaultHandshakeTimeout
	if s.server != nil && s.server.handshakeTimeout > 0 {
		timeout = s.server.handshakeTimeout
	}
	handshakeContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	err := s.conn.SetDeadline(time.Now().Add(timeout))
	if err != nil {
		return fmt.Errorf("handshakeDeadline: %w", err)
	}
	err = tlsConnection.HandshakeContext(handshakeContext)
	clearErr := s.conn.SetDeadline(time.Time{})
	if err != nil {
		return fmt.Errorf("tlsHandshake: %w", err)
	}
	if clearErr != nil {
		return fmt.Errorf("handshakeClear: %w", clearErr)
	}
	return nil
}

func (s *Session) readFrame() (Frame, error) {
	timeout := defaultReadTimeout
	if s.server != nil && s.server.readTimeout > 0 {
		timeout = s.server.readTimeout
	}
	err := s.conn.SetReadDeadline(time.Now().Add(timeout))
	if err != nil {
		return Frame{}, fmt.Errorf("readDeadline: %w", err)
	}
	frame, err := ReadFrame(s.conn)
	if err != nil {
		return Frame{}, fmt.Errorf("frameRead: %w", err)
	}
	return frame, nil
}

func (s *Session) cacheRequestReply(request, reply Frame) {
	if request.Type != Message || s.recentRequests == nil {
		return
	}
	key := requestKey{
		component: request.Component,
		command:   request.Command,
		messageID: request.MessageID,
	}
	record, isFound := s.recentRequests[key]
	if !isFound || record.digest != sha256.Sum256(request.Payload) {
		return
	}
	record.reply = reply
	record.isReplyAvailable = true
	s.recentRequests[key] = record
}

func (s *Session) cachedRequestReply(request Frame) (Frame, bool) {
	if request.Type != Message || s.recentRequests == nil {
		return Frame{}, false
	}
	key := requestKey{
		component: request.Component,
		command:   request.Command,
		messageID: request.MessageID,
	}
	record, isFound := s.recentRequests[key]
	if !isFound || !record.isReplyAvailable || record.digest != sha256.Sum256(request.Payload) {
		return Frame{}, false
	}
	return record.reply, true
}

func (s *Session) logPlayerDisconnect() {
	if s == nil || s.server == nil || s.server.logger == nil || s.account == "" {
		return
	}
	s.server.logger.Printf(
		"player_disconnect remote_ip=%q account=%q display_name=%q session_id=%d",
		sessionRemoteIP(s), s.account, s.displayName, s.ID,
	)
}

func sessionRemoteIP(session *Session) string {
	if session == nil || session.conn == nil {
		return ""
	}
	remoteAddress := session.conn.RemoteAddr().String()
	address, _, err := net.SplitHostPort(remoteAddress)
	if err == nil {
		return address
	}
	return remoteAddress
}

func (s *Session) isRequestReplay(frame Frame, now time.Time) bool {
	if frame.Type != Message {
		return false
	}
	if s.recentRequests == nil {
		s.recentRequests = make(map[requestKey]requestRecord)
	}
	key := requestKey{
		component: frame.Component,
		command:   frame.Command,
		messageID: frame.MessageID,
	}
	digest := sha256.Sum256(frame.Payload)
	record, isFound := s.recentRequests[key]
	if isFound && record.digest == digest && now.Sub(record.time) <= requestReplayWindow {
		record.time = now
		s.recentRequests[key] = record
		return true
	}
	if len(s.recentRequests) >= requestReplayLimit && !isFound {
		s.evictOldestRequest()
	}
	s.recentRequests[key] = requestRecord{digest: digest, time: now}
	return false
}

func (s *Session) evictOldestRequest() {
	var oldestKey requestKey
	var oldestTime time.Time
	isFound := false
	for key, record := range s.recentRequests {
		if isFound && !record.time.Before(oldestTime) {
			continue
		}
		oldestKey = key
		oldestTime = record.time
		isFound = true
	}
	if isFound {
		delete(s.recentRequests, oldestKey)
	}
}

func (s *Session) traceFrame(direction string, frame Frame) {
	if s.server.recorder == nil {
		return
	}
	event := protocoltrace.Event{
		Protocol: "blaze", Kind: "frame", Direction: direction,
		Remote: s.conn.RemoteAddr().String(), Session: s.ID,
		Component: frame.Component, Command: frame.Command,
		MessageID: frame.MessageID, MessageType: uint8(frame.Type),
		ErrorCode: frame.ErrorCode, Length: len(frame.Payload),
	}
	if len(frame.Payload) != 0 {
		fields, err := tdf.Decode(databuffer.FromBytes(frame.Payload))
		if err != nil {
			event.Error = fmt.Sprintf("tdfDecode: %v", err)
		} else {
			event.Fields = traceFieldShapes(fields)
		}
	}
	s.server.recorder.Record(event)
}

func (s *Session) traceReplay(frame Frame) {
	if s.server.recorder == nil {
		return
	}
	s.server.recorder.Record(protocoltrace.Event{
		Protocol: "blaze", Kind: "duplicate", Direction: "in",
		Remote: s.conn.RemoteAddr().String(), Session: s.ID,
		Component: frame.Component, Command: frame.Command,
		MessageID: frame.MessageID, MessageType: uint8(frame.Type),
		Length: len(frame.Payload),
	})
}

func traceFieldShapes(fields []tdf.Field) []string {
	shapes := make([]string, 0, len(fields))
	for _, field := range fields {
		shape := fmt.Sprintf("%s:%s", field.Label, traceTypeName(field.Value.Type))
		if field.Value.Type == tdf.String && field.Value.String == "" {
			shape += "(empty)"
		}
		if field.Value.Type == tdf.Binary && len(field.Value.Binary) == 0 {
			shape += "(empty)"
		}
		shapes = append(shapes, shape)
	}
	return shapes
}

func traceTypeName(valueType tdf.Type) string {
	names := [...]string{
		"integer", "string", "binary", "struct", "list", "map", "union",
		"variable", "object-type", "object-id", "float", "time",
	}
	if int(valueType) >= len(names) {
		return fmt.Sprintf("unknown-%d", valueType)
	}
	return names[valueType]
}

func (s *Server) traceConnection(session *Session, kind string) {
	if s.recorder == nil {
		return
	}
	s.recorder.Record(protocoltrace.Event{
		Protocol: "blaze", Kind: kind, Remote: session.conn.RemoteAddr().String(), Session: session.ID,
	})
}

func (s *Session) send(frame Frame) error {
	s.write.Lock()
	defer s.write.Unlock()
	timeout := defaultWriteTimeout
	if s.server != nil && s.server.writeTimeout > 0 {
		timeout = s.server.writeTimeout
	}
	err := s.conn.SetWriteDeadline(time.Now().Add(timeout))
	if err != nil {
		return fmt.Errorf("writeDeadline: %w", err)
	}
	writeErr := WriteFrame(s.conn, frame)
	clearErr := s.conn.SetWriteDeadline(time.Time{})
	if writeErr != nil {
		return fmt.Errorf("frameWrite: %w", writeErr)
	}
	if clearErr != nil {
		return fmt.Errorf("writeClear: %w", clearErr)
	}
	return nil
}

func (s *Server) addSessionForServe(
	ctx context.Context, serveGeneration uint64, session *Session,
) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil || s.listener == nil ||
		s.serveGeneration != serveGeneration {
		return false
	}
	if len(s.sessions) == 0 {
		s.sessionIdle = make(chan struct{})
	}
	s.sessions[session.ID] = session
	return true
}

func (s *Server) removeSession(id uint32) {
	s.mu.Lock()
	session := s.sessions[id]
	delete(s.sessions, id)
	disconnectedUserID := int64(0)
	if session != nil && session.userID != 0 {
		userSessions := s.sessionsByUser[session.userID]
		delete(userSessions, id)
		if len(userSessions) == 0 {
			disconnectedUserID = session.userID
			delete(s.sessionsByUser, session.userID)
		}
	}
	recipients := make([]*Session, 0, len(s.sessions))
	if disconnectedUserID != 0 && s.listener != nil {
		for _, recipient := range s.sessions {
			if recipient.userID == 0 || recipient.userID == disconnectedUserID {
				continue
			}
			recipients = append(recipients, recipient)
		}
	}
	if len(s.sessions) == 0 && s.sessionIdle != nil {
		close(s.sessionIdle)
		s.sessionIdle = nil
	}
	s.mu.Unlock()
	for _, recipient := range recipients {
		err := recipient.Notify(
			UserSessionComponentID, userSessionUserUpdated,
			userOfflineNotificationFields(disconnectedUserID),
		)
		if err != nil {
			s.logger.Printf(
				"Blaze offline presence notification failed user=%d recipient_session=%d: %v",
				disconnectedUserID, recipient.ID, err,
			)
		}
	}
}

func (s *Server) sessionsForUser(userID int64) []*Session {
	s.mu.Lock()
	indexed := s.sessionsByUser[userID]
	sessions := make([]*Session, 0, len(indexed))
	for _, session := range indexed {
		sessions = append(sessions, session)
	}
	s.mu.Unlock()
	return sessions
}

func (s *Server) uniqueOtherUserID(userID int64) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var candidateID int64
	for indexedID, sessions := range s.sessionsByUser {
		if indexedID == userID || len(sessions) == 0 {
			continue
		}
		if candidateID != 0 {
			return 0, false
		}
		candidateID = indexedID
	}
	return candidateID, candidateID != 0
}

func (s *Server) bindUserSession(session *Session, userID int64) {
	if session == nil || userID == 0 {
		return
	}
	s.mu.Lock()
	if s.sessionsByUser == nil {
		s.sessionsByUser = make(map[int64]map[uint32]*Session)
	}
	if session.userID != 0 && session.userID != userID {
		previous := s.sessionsByUser[session.userID]
		delete(previous, session.ID)
		if len(previous) == 0 {
			delete(s.sessionsByUser, session.userID)
		}
	}
	userSessions := s.sessionsByUser[userID]
	if userSessions == nil {
		userSessions = make(map[uint32]*Session)
		s.sessionsByUser[userID] = userSessions
	}
	userSessions[session.ID] = session
	session.userID = userID
	s.mu.Unlock()
}

func (s *Server) unbindUserSession(session *Session) {
	if session == nil {
		return
	}
	s.mu.Lock()
	userID := session.userID
	if userID == 0 {
		s.mu.Unlock()
		return
	}
	userSessions := s.sessionsByUser[userID]
	delete(userSessions, session.ID)
	if len(userSessions) == 0 {
		delete(s.sessionsByUser, userID)
	}
	session.userID = 0
	s.mu.Unlock()
}
