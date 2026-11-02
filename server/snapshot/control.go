package snapshot

import (
	"bufio"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// controlBroker carries transient commands only; it never writes configuration.
type controlBroker struct {
	mu          sync.Mutex
	listener    net.Listener
	token       string
	mode        Mode
	connections map[net.Conn]controlSubscriber
	credentials map[int64]string
	isClosed    bool
	workers     sync.WaitGroup
	logger      *log.Logger
}

type controlSubscriber struct {
	userID          int64
	isAuthenticated bool
}

func newControlBroker(mode Mode, logger *log.Logger) (*controlBroker, error) {
	secret := make([]byte, 32)
	byteCount, err := rand.Read(secret)
	if err != nil {
		return nil, fmt.Errorf("controlSecret: %w", err)
	}
	if byteCount != len(secret) {
		return nil, fmt.Errorf("controlSecret: %w", io.ErrUnexpectedEOF)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("controlListen: %w", err)
	}
	token := hex.EncodeToString(secret)
	broker := &controlBroker{listener: listener, token: token, mode: mode,
		connections: make(map[net.Conn]controlSubscriber), credentials: map[int64]string{0: token}, logger: logger}
	broker.workers.Add(1)
	go broker.accept()
	return broker, nil
}

func (e *controlBroker) accept() {
	defer e.workers.Done()
	for {
		connection, err := e.listener.Accept()
		if err != nil {
			return
		}
		e.mu.Lock()
		if e.isClosed || len(e.connections) >= 32 {
			e.closeConnection(connection)
			e.mu.Unlock()
			continue
		}
		e.connections[connection] = controlSubscriber{}
		e.workers.Add(1)
		e.mu.Unlock()
		go e.authenticate(connection)
	}
}

func (e *controlBroker) authenticate(connection net.Conn) {
	defer e.workers.Done()
	defer e.remove(connection)
	err := connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err != nil {
		return
	}
	r := bufio.NewReaderSize(connection, 128)
	token, err := r.ReadSlice('\n')
	if err != nil || len(token) != len(e.token)+1 {
		return
	}
	e.mu.Lock()
	if e.isClosed {
		e.mu.Unlock()
		return
	}
	userID := int64(0)
	isAuthenticated := false
	for credentialUserID, credentialToken := range e.credentials {
		if subtle.ConstantTimeCompare(token[:len(token)-1], []byte(credentialToken)) == 1 {
			userID = credentialUserID
			isAuthenticated = true
		}
	}
	if !isAuthenticated {
		e.mu.Unlock()
		return
	}
	err = e.write(connection, "mode="+string(e.mode)+"\n\n")
	if err == nil {
		if userID > 0 {
			for previous, subscriber := range e.connections {
				if previous != connection && subscriber.isAuthenticated && subscriber.userID == userID {
					e.closeConnection(previous)
					delete(e.connections, previous)
				}
			}
		}
		e.connections[connection] = controlSubscriber{userID: userID, isAuthenticated: true}
	}
	e.mu.Unlock()
	if err != nil {
		return
	}
	err = connection.SetReadDeadline(time.Time{})
	if err != nil {
		return
	}
	// The client only consumes commands after authentication. Any further input
	// or disconnect retires its subscription rather than growing an input queue.
	unexpectedByte, err := r.ReadByte()
	if err != nil && err != io.EOF {
		return
	}
	if err == nil {
		e.logger.Printf("Sync Snapshot IPC subscriber sent unexpected byte: %d", unexpectedByte)
	}
}

func (e *controlBroker) write(connection net.Conn, frame string) error {
	err := connection.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
	if err != nil {
		return fmt.Errorf("controlDeadline: %w", err)
	}
	byteCount, err := io.WriteString(connection, frame)
	if err != nil {
		return fmt.Errorf("controlWrite: %w", err)
	}
	if byteCount != len(frame) {
		return fmt.Errorf("controlWrite: %w", io.ErrShortWrite)
	}
	return nil
}

func (e *controlBroker) publish(frame string, mode Mode) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.isClosed || len(frame) > 1023 {
		return 0
	}
	e.mode = mode
	count := 0
	for connection, subscriber := range e.connections {
		if !subscriber.isAuthenticated {
			continue
		}
		err := e.write(connection, frame)
		if err != nil {
			e.closeConnection(connection)
			delete(e.connections, connection)
			continue
		}
		count++
	}
	return count
}

func (e *controlBroker) publishCapture(frame string, userID int64) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.isClosed || len(frame) > 1023 {
		return false
	}
	var recipient net.Conn
	authenticatedCount := 0
	for connection, subscriber := range e.connections {
		if !subscriber.isAuthenticated {
			continue
		}
		authenticatedCount++
		if userID > 0 && subscriber.userID == userID {
			recipient = connection
		}
	}
	if recipient == nil && authenticatedCount == 1 {
		for connection, subscriber := range e.connections {
			if subscriber.isAuthenticated && (userID == 0 || subscriber.userID == 0) {
				recipient = connection
			}
		}
	}
	if recipient == nil {
		return false
	}
	err := e.write(recipient, frame)
	if err != nil {
		e.closeConnection(recipient)
		delete(e.connections, recipient)
		return false
	}
	return true
}

// ControlConnectionForUser issues credentials scoped to a single owned client.
// A replacement connection for that user retires its older subscription.
func (e *Service) ControlConnectionForUser(userID int64) (endpoint string, token string, err error) {
	if e == nil || e.controlBroker == nil || userID <= 0 {
		return "", "", errors.New("snapshot control requires a positive user ID")
	}
	secret := make([]byte, 32)
	byteCount, err := rand.Read(secret)
	if err != nil {
		return "", "", fmt.Errorf("controlSecret: %w", err)
	}
	if byteCount != len(secret) {
		return "", "", fmt.Errorf("controlSecret: %w", io.ErrUnexpectedEOF)
	}
	broker := e.controlBroker
	broker.mu.Lock()
	defer broker.mu.Unlock()
	if broker.isClosed {
		return "", "", errors.New("snapshot control is closed")
	}
	if _, isPresent := broker.credentials[userID]; !isPresent && len(broker.credentials) >= 256 {
		return "", "", errors.New("snapshot control credential limit reached")
	}
	token = hex.EncodeToString(secret)
	broker.credentials[userID] = token
	return broker.listener.Addr().String(), token, nil
}

func (e *controlBroker) remove(connection net.Conn) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, isPresent := e.connections[connection]; !isPresent {
		return
	}
	delete(e.connections, connection)
	e.closeConnection(connection)
}

func (e *controlBroker) closeConnection(connection net.Conn) {
	err := connection.Close()
	if err != nil {
		e.logger.Printf("Sync Snapshot IPC connection close failed: %v", err)
	}
}

func (e *controlBroker) close() {
	e.mu.Lock()
	e.isClosed = true
	err := e.listener.Close()
	if err != nil {
		e.logger.Printf("Sync Snapshot IPC listener close failed: %v", err)
	}
	for connection := range e.connections {
		e.closeConnection(connection)
		delete(e.connections, connection)
	}
	e.mu.Unlock()
	e.workers.Wait()
}

// ControlConnection provides legacy single-client launch credentials for Fang.
// Multi-client launchers use ControlConnectionForUser to route captures safely.
// Neither value belongs in persistent configuration or diagnostic reports.
func (e *Service) ControlConnection() (endpoint string, token string) {
	if e == nil || e.controlBroker == nil {
		return "", ""
	}
	return e.controlBroker.listener.Addr().String(), e.controlBroker.token
}

// ControlDirectory is the client-memory diagnostic root for this recorder.
func (e *Service) ControlDirectory() string {
	if e == nil {
		return ""
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return filepath.Dir(e.directory)
}

// removeLegacyControl removes only the obsolete file in the specified trusted
// diagnostic directory, never a directory or a path through a symlink.
func removeLegacyControl(directory string, logger *log.Logger) {
	if directory == "" {
		return
	}
	root, err := filepath.Abs(directory)
	if err != nil {
		logger.Printf("Sync Snapshot legacy control path failed: %v", err)
		return
	}
	for ancestor := root; ; ancestor = filepath.Dir(ancestor) {
		fi, statErr := os.Lstat(ancestor)
		if statErr != nil {
			if !os.IsNotExist(statErr) {
				logger.Printf("Sync Snapshot legacy directory check failed: %v", statErr)
			}
			return
		}
		if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
			return
		}
		if filepath.Dir(ancestor) == ancestor {
			break
		}
	}
	path := filepath.Join(root, "snapshot-control.txt")
	fi, err := os.Lstat(path)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Printf("Sync Snapshot legacy control check failed: %v", err)
		}
		return
	}
	if !fi.Mode().IsRegular() {
		logger.Printf("Sync Snapshot legacy control is not a regular file: %s", path)
		return
	}
	err = os.Remove(path)
	if err != nil {
		logger.Printf("Sync Snapshot legacy control cleanup failed: %v", err)
	}
}
