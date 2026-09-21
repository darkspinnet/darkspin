package game

import (
	"context"
	"fmt"
)

// SystemChatPublisher delivers gameplay-owned diagnostic feedback through the
// player's existing control connection.
type SystemChatPublisher interface {
	PublishSystemChat(context.Context, int64, uint32, string) error
}

// UseSystemChatPublisher is configured before accepting gameplay connections.
func (e *GameplayJoin) UseSystemChatPublisher(publisher SystemChatPublisher) {
	e.systemChatPublisher = publisher
}

// PublishSystemChat sends one gameplay-owned diagnostic line to the player.
func (e *GameplayJoin) PublishSystemChat(
	ctx context.Context, userID int64, gameID uint32, message string,
) error {
	if e == nil || e.systemChatPublisher == nil {
		return nil
	}
	err := e.systemChatPublisher.PublishSystemChat(ctx, userID, gameID, message)
	if err != nil {
		return fmt.Errorf("systemChatPublish: %w", err)
	}
	return nil
}
