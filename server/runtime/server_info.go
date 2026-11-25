package runtime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/blaze"
	"github.com/darkspinnet/darkspin/server/serverinfo"
	serverinfosql "github.com/darkspinnet/darkspin/server/serverinfo/sqlstore"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

// Dedicated composition owns schema creation; local servers never initialize it.
func initializeServerIdentity(ctx context.Context, repository sporenet.UserRepository) (serverinfo.IdentityStore, error) {
	provider, isSupported := repository.(interface {
		ServerIdentityStore() *serverinfosql.Store
	})
	if !isSupported {
		return nil, errors.New("server identity storage unavailable")
	}
	store := provider.ServerIdentityStore()
	if store == nil {
		return nil, errors.New("nil server identity store")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err := store.EnsureSchema(ctx)
	if err != nil {
		return nil, fmt.Errorf("identitySchema: %w", err)
	}
	return store, nil
}

type panelAnnouncementBroadcast struct{ server *Server }

func (e *panelAnnouncementBroadcast) Broadcast(ctx context.Context, message string) (int, error) {
	if e.server == nil {
		return 0, errors.New("announcement server unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	delivered, err := blaze.BroadcastSystemChat(ctx, e.server.maintenanceServers, message)
	if err != nil {
		return delivered, fmt.Errorf("announcementBroadcast: %w", err)
	}
	return delivered, nil
}
