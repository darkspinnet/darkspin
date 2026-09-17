// Package textlog writes accepted chat messages as JSON Lines.
package textlog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/chat"
)

// Writer serializes chat messages to one append-only stream.
type Writer struct {
	mu     sync.Mutex
	output io.Writer
}

type entry struct {
	MessageID    uint64    `json:"message_id"`
	SenderID     int64     `json:"sender_id"`
	SenderName   string    `json:"sender_name"`
	Scope        uint8     `json:"scope"`
	TargetID     uint64    `json:"target_id"`
	Body         string    `json:"body"`
	RecipientIDs []int64   `json:"recipient_ids"`
	SentAt       time.Time `json:"sent_at"`
}

// New creates a JSONL chat recorder around output.
func New(output io.Writer) (*Writer, error) {
	if output == nil {
		return nil, errors.New("nil chat log output")
	}
	return &Writer{output: output}, nil
}

// Record writes one complete JSON object and trailing newline.
func (w *Writer) Record(ctx context.Context, message chat.Message) error {
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("contextCheck: %w", err)
	}
	value := entry{
		MessageID: message.ID, SenderID: message.Sender.ID, SenderName: message.Sender.Name,
		Scope: uint8(message.Target.Scope), TargetID: message.Target.ID, Body: message.Body,
		RecipientIDs: message.RecipientIDs, SentAt: message.SentAt,
	}
	w.mu.Lock()
	err = json.NewEncoder(w.output).Encode(value)
	w.mu.Unlock()
	if err != nil {
		return fmt.Errorf("entryEncode: %w", err)
	}
	return nil
}

var _ chat.Recorder = (*Writer)(nil)
