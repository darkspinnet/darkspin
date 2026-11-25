//go:build scenario

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

type debugChatCursor struct {
	Offset         int64  `json:"offset"`
	NativeOffset   int64  `json:"native_offset"`
	InstanceID     string `json:"instance_id"`
	UserID         int64  `json:"user_id"`
	LoginSessionID string `json:"login_session_id"`
}

type debugChatReply struct {
	Sequence         int64  `json:"sequence"`
	UserID           int64  `json:"user_id"`
	SessionID        uint32 `json:"session_id"`
	Command          string `json:"command"`
	Body             string `json:"body"`
	Reply            string `json:"reply"`
	Source           string `json:"source"`
	IsNativeRendered bool   `json:"is_native_rendered"`
}

type debugChatRequest struct {
	ClientID  string          `json:"client_id"`
	Cursor    debugChatCursor `json:"cursor"`
	Command   string          `json:"command"`
	Event     string          `json:"event"`
	TimeoutMS uint32          `json:"timeout_ms"`
}

type debugChatSnapshot struct {
	Cursor  debugChatCursor  `json:"cursor"`
	Source  string           `json:"source"`
	Replies []debugChatReply `json:"replies"`
}

func executeDebugChatOperation(ctx context.Context, host *debugHost, clientManager *debugClientManager, operation string, payload json.RawMessage) (any, error) {
	var req debugChatRequest
	err := decodeDebugPayload(payload, &req)
	if err != nil {
		return nil, fmt.Errorf("chatRequest: %w", err)
	}
	if operation == "client.chat_wait_native" {
		result, nativeErr := waitDebugNativeChat(ctx, host, clientManager, req)
		if nativeErr != nil {
			return result, fmt.Errorf("chatNative: %w", nativeErr)
		}
		return result, nil
	}
	owner, err := observeDebugWorldOwner(ctx, host, clientManager, req.ClientID)
	if err != nil {
		return nil, fmt.Errorf("chatOwner: %w", err)
	}
	if operation == "client.chat_artifacts" {
		result, artifactErr := observeDebugChatArtifacts(ctx, host, owner)
		if artifactErr != nil {
			return result, fmt.Errorf("chatArtifacts: %w", artifactErr)
		}
		return result, nil
	}
	if operation == "client.chat_snapshot" {
		fi, statErr := os.Stat(host.serverLogPath)
		if statErr != nil {
			return nil, fmt.Errorf("chatStat: %w", statErr)
		}
		nativeFI, nativeErr := os.Stat(owner.client.TracePath)
		if nativeErr != nil {
			return nil, fmt.Errorf("chatNativeStat: %w", nativeErr)
		}
		return debugChatSnapshot{Cursor: debugChatCursor{
			Offset: fi.Size(), NativeOffset: nativeFI.Size(), InstanceID: owner.client.InstanceID,
			UserID: owner.profile.UserID, LoginSessionID: owner.profile.SessionID,
		}, Source: "server_command_reply", Replies: []debugChatReply{}}, nil
	}
	if operation != "client.chat_wait" {
		return nil, errors.New("unsupported chat observation")
	}
	if req.TimeoutMS < 1 || req.TimeoutMS > 120000 || req.Cursor.Offset < 0 ||
		req.Cursor.InstanceID != owner.client.InstanceID || req.Cursor.UserID != owner.profile.UserID ||
		req.Cursor.LoginSessionID != owner.profile.SessionID {
		return nil, errors.New("chat wait requires a fresh cursor for this owned login")
	}
	if !strings.HasPrefix(req.Command, "/") || len(req.Command) < 2 ||
		strings.ContainsAny(req.Command, " \t\r\n\x1f") || strings.ToLower(req.Command) != req.Command {
		return nil, errors.New("chat wait command must be a canonical slash verb without arguments")
	}
	waitContext, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutMS)*time.Millisecond)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	cursor := req.Cursor
	for {
		current, ownerErr := observeDebugWorldOwner(waitContext, host, clientManager, req.ClientID)
		if ownerErr != nil {
			return nil, fmt.Errorf("chatRecheck: %w", ownerErr)
		}
		// Commands may legitimately change worlds. The accepted login and owned
		// process must remain the same; game ID is deliberately not pinned.
		if current.client.InstanceID != cursor.InstanceID || current.profile.UserID != cursor.UserID ||
			current.profile.SessionID != cursor.LoginSessionID {
			return nil, errors.New("owned chat process or accepted login changed")
		}
		replies, readErr := readDebugChatReplies(host.serverLogPath, &cursor)
		if readErr != nil {
			return nil, fmt.Errorf("chatRead: %w", readErr)
		}
		for _, reply := range replies {
			if reply.Command == req.Command {
				return reply, nil
			}
		}
		select {
		case <-waitContext.Done():
			return nil, fmt.Errorf("chatDeadline: %w", waitContext.Err())
		case <-ticker.C:
		}
	}
}

// The path comes only from the owned host, never from Lua or an HTTP request.
// Offsets advance only over whole lines; partial writes are retried next poll.
func readDebugChatReplies(path string, cursor *debugChatCursor) ([]debugChatReply, error) {
	r, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("replyOpen: %w", err)
	}
	fi, statErr := r.Stat()
	if statErr != nil {
		closeErr := r.Close()
		if closeErr != nil {
			return nil, fmt.Errorf("replyStatClose: %w", errors.Join(statErr, closeErr))
		}
		return nil, fmt.Errorf("replyStat: %w", statErr)
	}
	if fi.Size() < cursor.Offset || fi.Size()-cursor.Offset > 8*1024*1024 {
		closeErr := r.Close()
		if closeErr != nil {
			return nil, fmt.Errorf("replyBoundClose: %w", closeErr)
		}
		return nil, errors.New("chat log rotated or exceeded the bounded observation window")
	}
	payload := make([]byte, 512*1024)
	count, readErr := r.ReadAt(payload, cursor.Offset)
	closeErr := r.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return nil, fmt.Errorf("replyRead: %w", errors.Join(readErr, closeErr))
	}
	if closeErr != nil {
		return nil, fmt.Errorf("replyClose: %w", closeErr)
	}
	end := bytes.LastIndexByte(payload[:count], '\n')
	if end < 0 {
		if count == len(payload) {
			return nil, errors.New("chat log line exceeded the observation bound")
		}
		return []debugChatReply{}, nil
	}
	replies := make([]debugChatReply, 0)
	offset := cursor.Offset
	for _, line := range bytes.Split(payload[:end], []byte{'\n'}) {
		offset += int64(len(line) + 1)
		marker := bytes.Index(line, []byte("ScenarioCommandReply "))
		if marker < 0 {
			continue
		}
		var reply debugChatReply
		err = json.Unmarshal(line[marker+len("ScenarioCommandReply "):], &reply)
		if err != nil {
			return nil, fmt.Errorf("replyDecode: %w", err)
		}
		if reply.UserID != cursor.UserID {
			continue
		}
		reply.Sequence, reply.Source = offset, "server_command_reply"
		replies = append(replies, reply)
	}
	cursor.Offset += int64(end + 1)
	return replies, nil
}
