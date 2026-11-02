package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/runtime/playerstore"
	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/jackc/pgx/v5"
)

func validatePlayerDriver(config *game.Config, postgres *pgx.ConnConfig) error {
	if postgres != nil {
		if !config.Bool(game.ConfigServerIsDedicated) {
			return errors.New("PostgreSQL player storage requires dedicated server mode")
		}
		return nil
	}
	if config.Bool(game.ConfigServerIsDedicated) {
		return errors.New("dedicated server requires PostgreSQL connection settings")
	}
	driver := strings.ToLower(strings.TrimSpace(config.String(game.ConfigStorageDriver)))
	if driver != "" && driver != "sqlite" {
		return errors.New("player storage defaults to sqlite; configure PostgreSQL through environment variables")
	}
	return nil
}

func openPlayerStore(config *game.Config, path string, postgres *pgx.ConnConfig) (*playerstore.Store, error) {
	err := validatePlayerDriver(config, postgres)
	if err != nil {
		return nil, fmt.Errorf("driverValidate: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := playerstore.Open(ctx, path, postgres)
	if err != nil {
		return nil, fmt.Errorf("playerOpen: %w", err)
	}
	return store, nil
}

// Administrative profile operations use the same selection as darkrun server.
func openUserRepository(config *game.Config, path string) (sporenet.UserRepository, io.Closer, error) {
	var postgres *pgx.ConnConfig
	var err error
	if config.Bool(game.ConfigServerIsDedicated) {
		postgres, err = playerstore.FromEnvironment()
		if err != nil {
			return nil, nil, fmt.Errorf("postgresEnvironment: %w", err)
		}
	}
	err = validatePlayerDriver(config, postgres)
	if err != nil {
		return nil, nil, fmt.Errorf("driverValidate: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	repository, err := playerstore.OpenProfile(ctx, path, postgres)
	if err != nil {
		return nil, nil, fmt.Errorf("profileOpen: %w", err)
	}
	return repository, repository, nil
}
