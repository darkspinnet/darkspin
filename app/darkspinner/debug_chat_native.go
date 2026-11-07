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
	"time"
)

type debugNativeChatEvent struct {
	Event    string `json:"event"`
	Value    uint32 `json:"value"`
	Sequence int64  `json:"sequence"`
	Source   string `json:"source"`
}

func waitDebugNativeChat(ctx context.Context, host *debugHost, clientManager *debugClientManager, req debugChatRequest) (debugNativeChatEvent, error) {
	if host == nil || clientManager == nil || req.TimeoutMS < 1 || req.TimeoutMS > 120000 || req.Cursor.NativeOffset < 0 {
		return debugNativeChatEvent{}, errors.New("native chat wait requires an owned client and bounded cursor")
	}
	switch req.Event {
	case "effect_preview_override", "effect_preview_failure", "chat_reset_post", "chat_exit":
	default:
		return debugNativeChatEvent{}, errors.New("unsupported native chat observation")
	}
	waitContext, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutMS)*time.Millisecond)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	offset := req.Cursor.NativeOffset
	for {
		// A native exit can end the process before the observer polls. The
		// retained managed launch identity still owns that exact trace file.
		client, stateErr := clientManager.State(req.ClientID)
		if stateErr != nil {
			return debugNativeChatEvent{}, fmt.Errorf("nativeChatClient: %w", stateErr)
		}
		host.mutex.Lock()
		credential, isProfileFound := host.credentialsByName[client.ProfileName]
		host.mutex.Unlock()
		if !isProfileFound {
			return debugNativeChatEvent{}, errors.New("native chat profile is no longer owned")
		}
		if client.InstanceID != req.Cursor.InstanceID || client.UserID != req.Cursor.UserID || credential.UserID != req.Cursor.UserID || req.Cursor.LoginSessionID == "" {
			return debugNativeChatEvent{}, errors.New("native chat cursor does not belong to this managed launch")
		}
		events, readErr := readDebugNativeChat(client.TracePath, &offset)
		if readErr != nil {
			return debugNativeChatEvent{}, fmt.Errorf("nativeChatRead: %w", readErr)
		}
		var match *debugNativeChatEvent
		for index := range events {
			event := &events[index]
			if req.Event == "effect_preview_override" && event.Event == "effect_preview_failure" {
				return debugNativeChatEvent{}, fmt.Errorf("native effect preview rejected: %d", event.Value)
			}
			if event.Event == req.Event {
				match = event
			}
		}
		if match != nil {
			return *match, nil
		}
		select {
		case <-waitContext.Done():
			return debugNativeChatEvent{}, fmt.Errorf("nativeChatDeadline: %w", waitContext.Err())
		case <-ticker.C:
		}
	}
}

func readDebugNativeChat(path string, offset *int64) ([]debugNativeChatEvent, error) {
	r, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("nativeOpen: %w", err)
	}
	fi, statErr := r.Stat()
	if statErr != nil || fi.Size() < *offset || fi.Size()-*offset > 16*1024*1024 {
		closeErr := r.Close()
		if statErr != nil || closeErr != nil {
			return nil, fmt.Errorf("nativeStat: %w", errors.Join(statErr, closeErr))
		}
		return nil, errors.New("native chat trace rotated or exceeded its observation bound")
	}
	payload := make([]byte, 1024*1024)
	count, readErr := r.ReadAt(payload, *offset)
	closeErr := r.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return nil, fmt.Errorf("nativeRead: %w", errors.Join(readErr, closeErr))
	}
	if closeErr != nil {
		return nil, fmt.Errorf("nativeClose: %w", closeErr)
	}
	end := bytes.LastIndexByte(payload[:count], '\n')
	if end < 0 {
		if count == len(payload) {
			return nil, errors.New("native chat trace line exceeded its observation bound")
		}
		return []debugNativeChatEvent{}, nil
	}
	events := make([]debugNativeChatEvent, 0)
	position := *offset
	for _, line := range bytes.Split(payload[:end], []byte{'\n'}) {
		position += int64(len(line) + 1)
		if !bytes.Contains(line, []byte("effect_preview_")) && !bytes.Contains(line, []byte("chat_reset_post")) && !bytes.Contains(line, []byte("chat_exit")) {
			continue
		}
		var record struct {
			Protocol string `json:"protocol"`
			Kind     string `json:"kind"`
			Value    uint32 `json:"value"`
		}
		err = json.Unmarshal(line, &record)
		if err != nil {
			return nil, fmt.Errorf("nativeDecode: %w", err)
		}
		if record.Protocol == "client_state" {
			events = append(events, debugNativeChatEvent{Event: record.Kind, Value: record.Value, Sequence: position, Source: "native_client_trace"})
		}
	}
	*offset += int64(end + 1)
	return events, nil
}
