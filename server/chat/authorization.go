package chat

import (
	"context"
	"fmt"

	"github.com/darkspinnet/darkspin/server/accountaccess"
)

// CommandAuthorizer reads durable permissions rather than client/profile flags.
type CommandAuthorizer interface {
	Read(context.Context, int64) (accountaccess.Access, error)
}

func (e *Service) UseCommandAuthorizer(authorizer CommandAuthorizer) {
	e.commandAuthorizer = authorizer
}

// AuthorizeCommand keeps ordinary player commands available and denies all
// other commands unless the authenticated sender is currently an administrator.
func (e *Service) AuthorizeCommand(ctx context.Context, userID int64, name string) error {
	if ctx == nil || userID <= 0 {
		return accountaccess.ErrAccessDenied
	}
	switch name {
	case "/help", "/emote", "/taunt", "/bug", "/b", "/ping", "/hint", "/loc", "/stat", "/follow", "/exit":
		return nil
	}
	if e.commandAuthorizer == nil {
		return accountaccess.ErrAccessDenied
	}
	access, err := e.commandAuthorizer.Read(ctx, userID)
	if err != nil {
		return fmt.Errorf("commandAccess: %w", err)
	}
	if access.UserID != userID || access.Revision <= 0 || !access.IsAdmin() {
		return accountaccess.ErrAccessDenied
	}
	err = accountaccess.CheckPeer(ctx, access)
	if err != nil {
		return fmt.Errorf("commandPeer: %w", err)
	}
	return nil
}
