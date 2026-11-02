package runtime

import (
	"context"
	"fmt"

	"github.com/darkspinnet/darkspin/server/blaze"
	"github.com/darkspinnet/darkspin/server/chat"
)

// reportContextProvider adds client-reported ship locations when there is no
// active mission. Gameplay context remains authoritative during mission loading.
type reportContextProvider struct {
	isModded bool
	modNames []string
	gameplay chat.BugContextProvider
	presence *blaze.PresenceRegistry
}

func (e reportContextProvider) BugContext(
	ctx context.Context, req chat.BugContextRequest,
) (chat.BugContext, error) {
	reportContext, err := e.gameplay.BugContext(ctx, req)
	reportContext.IsModded = e.isModded
	reportContext.ModNames = append([]string(nil), e.modNames...)
	if !reportContext.IsGameplayActive {
		reportContext.LocationLabel = e.presence.ReportLocation(req.Sender.ID)
	}
	if err != nil {
		return reportContext, fmt.Errorf("reportContext: %w", err)
	}
	return reportContext, nil
}
