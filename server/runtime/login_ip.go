package runtime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/loginhistory"
	"github.com/darkspinnet/darkspin/server/loginhistory/sqlstore"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

func installLoginIPRecorder(ctx context.Context, repository sporenet.UserRepository, userManager *sporenet.UserManager) (*sqlstore.Store, error) {
	provider, isSupported := repository.(interface {
		LoginIPStore() *sqlstore.Store
	})
	if !isSupported {
		return nil, errors.New("login IP history storage unavailable")
	}
	store := provider.LoginIPStore()
	if store == nil {
		return nil, errors.New("nil login IP history store")
	}
	schemaCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	err := store.EnsureSchema(schemaCtx)
	if err != nil {
		return nil, fmt.Errorf("historySchema: %w", err)
	}
	userManager.SetGameplayLoginIPRecorder(loginhistory.New(store))
	return store, nil
}
