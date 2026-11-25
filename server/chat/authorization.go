package chat

import (
	"context"
	"fmt"

	"github.com/darkspinnet/darkspin/server/accountaccess"
)

// CommandAuthorizer reads dedicated-server permissions from durable account access.
type CommandAuthorizer interface {
	Read(context.Context, int64) (accountaccess.Access, error)
}

func (e *Service) UseCommandAuthorizer(authorizer CommandAuthorizer) {
	e.commandAuthorizer = authorizer
}

// UseLocalCommandAccess makes authenticated local-play accounts administrative
// for in-game commands. Only the server composition selects this policy.
func (e *Service) UseLocalCommandAccess() {
	e.isLocalCommandAccess = true
}

// AuthorizeCommand keeps local play administrative by default. Dedicated
// servers require durable administrator access for privileged commands.
func (e *Service) AuthorizeCommand(ctx context.Context, userID int64, name string) error {
	if ctx == nil || userID <= 0 {
		return accountaccess.ErrAccessDenied
	}
	if e.isLocalCommandAccess {
		return nil
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
