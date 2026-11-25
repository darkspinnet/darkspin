package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/matchmaking"
	matchmakingsql "github.com/darkspinnet/darkspin/server/matchmaking/sqlstore"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

func initializeMatchmakingConfiguration(ctx context.Context, repository sporenet.UserRepository, config *game.Config, configPath string, auditPort matchmaking.AuditPort) (*matchmaking.Service, error) {
	provider, isSupported := repository.(interface{ MatchmakingStore() *matchmakingsql.Store })
	if !isSupported {
		return nil, errors.New("matchmaking storage unavailable")
	}
	store := provider.MatchmakingStore()
	if store == nil {
		return nil, errors.New("nil matchmaking store")
	}
	err := store.EnsureSchema(ctx)
	if err != nil {
		return nil, fmt.Errorf("matchmakingSchema: %w", err)
	}
	service := matchmaking.New(store, auditPort)
	settings, err := service.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("matchmakingRead: %w", err)
	}
	err = matchmaking.Validate(settings)
	if err != nil {
		return nil, fmt.Errorf("matchmakingValidate: %w", err)
	}
	err = config.RetireMatchmakingConfiguration(configPath)
	if err != nil {
		return nil, fmt.Errorf("legacyRetire: %w", err)
	}
	return service, nil
}

// ReadMatchmakingConfiguration reads local settings without starting a game server.
// A running launcher uses its Server method instead, preserving shared live state.
func ReadMatchmakingConfiguration(ctx context.Context, configPath string) (matchmaking.Settings, error) {
	service, closer, err := openMatchmakingConfiguration(ctx, configPath)
	if err != nil {
		return matchmaking.Settings{}, fmt.Errorf("matchmakingOpen: %w", err)
	}
	settings, readErr := service.Read(ctx)
	closeErr := closer.Close()
	if readErr != nil || closeErr != nil {
		return matchmaking.Settings{}, fmt.Errorf("matchmakingRead: %w", errors.Join(readErr, closeErr))
	}
	return settings, nil
}

func SetMatchmakingConfiguration(ctx context.Context, configPath string, req matchmaking.Settings) error {
	err := matchmaking.Validate(req)
	if err != nil {
		return fmt.Errorf("matchmakingValidate: %w", err)
	}
	service, closer, err := openMatchmakingConfiguration(ctx, configPath)
	if err != nil {
		return fmt.Errorf("matchmakingOpen: %w", err)
	}
	saveErr := service.Set(ctx, req)
	closeErr := closer.Close()
	if saveErr != nil || closeErr != nil {
		return fmt.Errorf("matchmakingSave: %w", errors.Join(saveErr, closeErr))
	}
	return nil
}

func openMatchmakingConfiguration(ctx context.Context, configFilename string) (*matchmaking.Service, io.Closer, error) {
	if ctx == nil {
		return nil, nil, errors.New("nil context")
	}
	configPath, err := filepath.Abs(configFilename)
	if err != nil {
		return nil, nil, fmt.Errorf("configPath: %w", err)
	}
	config, isGenerated, err := LoadLocalConfiguration(configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("configLoad: %w", err)
	}
	if isGenerated {
		err = SaveLocalConfiguration(configPath, config)
		if err != nil {
			return nil, nil, fmt.Errorf("configSave: %w", err)
		}
	}
	databasePath := filepath.Join(filepath.Dir(configPath), DarkspinDirectory, SaveDirectory, UserDatabaseFilename)
	repository, closer, err := openUserRepository(config, databasePath)
	if err != nil {
		return nil, nil, fmt.Errorf("profileOpen: %w", err)
	}
	service, err := initializeMatchmakingConfiguration(ctx, repository, config, configPath, nil)
	if err != nil {
		closeErr := closer.Close()
		return nil, nil, fmt.Errorf("matchmakingInit: %w", errors.Join(err, closeErr))
	}
	return service, closer, nil
}

func (e *Server) ReadMatchmakingConfiguration(ctx context.Context) (matchmaking.Settings, error) {
	if e == nil || e.matchmakingConfiguration == nil {
		return matchmaking.Settings{}, errors.New("matchmaking configuration unavailable")
	}
	settings, err := e.matchmakingConfiguration.Read(ctx)
	if err != nil {
		return matchmaking.Settings{}, fmt.Errorf("matchmakingRead: %w", err)
	}
	return settings, nil
}

func (e *Server) SetMatchmakingConfiguration(ctx context.Context, req matchmaking.Settings) error {
	if e == nil || e.matchmakingConfiguration == nil {
		return errors.New("local matchmaking configuration unavailable")
	}
	err := e.matchmakingConfiguration.Set(ctx, req)
	if err != nil {
		return fmt.Errorf("matchmakingSave: %w", err)
	}
	return nil
}
