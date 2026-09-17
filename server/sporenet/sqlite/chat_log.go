package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/chat"
)

type chatLogRow struct {
	MessageID  uint64 `db:"message_id"`
	SenderID   int64  `db:"sender_id"`
	SenderName string `db:"sender_name"`
	Scope      uint8  `db:"scope"`
	TargetID   uint64 `db:"target_id"`
	Body       string `db:"body"`
	Recipient  string `db:"recipient"`
	SentAt     string `db:"sent_at"`
}

// Record inserts one accepted chat event into the durable audit log.
func (r *Repository) Record(ctx context.Context, message chat.Message) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	recipient, err := json.Marshal(message.RecipientIDs)
	if err != nil {
		return fmt.Errorf("recipientEncode: %w", err)
	}
	_, err = r.db.NamedExecContext(ctx, `
		INSERT INTO chat_log (message_id, sender_id, sender_name, scope, target_id, body, recipient, sent_at)
		VALUES (:message_id, :sender_id, :sender_name, :scope, :target_id, :body, :recipient, :sent_at)`, chatLogRow{
		MessageID: message.ID, SenderID: message.Sender.ID, SenderName: message.Sender.Name,
		Scope: uint8(message.Target.Scope), TargetID: message.Target.ID, Body: message.Body,
		Recipient: string(recipient), SentAt: message.SentAt.UTC().Format(chatLogTimeFormat),
	})
	if err != nil {
		return fmt.Errorf("chatInsert: %w", err)
	}
	return nil
}

var _ chat.Recorder = (*Repository)(nil)
