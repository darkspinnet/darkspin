package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

// RestoreUserProfile is an offline administrative operation used only by darkrun.
// Opening the repository migrates its schema but does not run startup expiry.
func RestoreUserProfile(
	ctx context.Context, configFilename string, userID int64, loginName string,
) (string, error) {
	if ctx == nil {
		return "", errors.New("nil context")
	}
	repository, closer, err := openAdministrativeProfileRepository(configFilename)
	if err != nil {
		return "", fmt.Errorf("profileOpen: %w", err)
	}
	recovery, isSupported := repository.(sporenet.UserRecoveryRepository)
	if !isSupported {
		closeErr := closer.Close()
		return "", fmt.Errorf("restorePort: %w", errors.Join(errors.New("profile recovery unavailable"), closeErr))
	}
	restoredLogin, restoreErr := sporenet.RestoreDeletedProfile(ctx, recovery, userID, loginName)
	closeErr := closer.Close()
	if restoreErr != nil || closeErr != nil {
		return "", fmt.Errorf("profileRestore: %w", errors.Join(restoreErr, closeErr))
	}
	return restoredLogin, nil
}

// SwapUserProfileNames is available only to offline dedicated-server tools.
func SwapUserProfileNames(
	ctx context.Context, configFilename string, req sporenet.ProfileNameSwapRequest,
) (sporenet.ProfileNameSwap, error) {
	if ctx == nil {
		return sporenet.ProfileNameSwap{}, errors.New("nil context")
	}
	repository, closer, err := openAdministrativeProfileRepository(configFilename)
	if err != nil {
		return sporenet.ProfileNameSwap{}, fmt.Errorf("swapOpen: %w", err)
	}
	swapRepository, isSupported := repository.(sporenet.UserNameSwapRepository)
	if !isSupported {
		closeErr := closer.Close()
		return sporenet.ProfileNameSwap{}, fmt.Errorf("swapPort: %w", errors.Join(errors.New("profile name swap unavailable"), closeErr))
	}
	result, swapErr := sporenet.SwapProfileNames(ctx, swapRepository, req)
	closeErr := closer.Close()
	if swapErr != nil || closeErr != nil {
		return sporenet.ProfileNameSwap{}, fmt.Errorf("profileSwap: %w", errors.Join(swapErr, closeErr))
	}
	return result, nil
}

func openAdministrativeProfileRepository(configFilename string) (sporenet.UserRepository, io.Closer, error) {
	configPath, err := filepath.Abs(configFilename)
	if err != nil {
		return nil, nil, fmt.Errorf("configPath: %w", err)
	}
	config, isGenerated, err := game.LoadDedicatedConfig(configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("configLoad: %w", err)
	}
	if isGenerated {
		return nil, nil, errors.New("dedicated profile administration requires environment configuration")
	}
	runtimePath := filepath.Join(filepath.Dir(configPath), DarkspinDirectory)
	databasePath := filepath.Join(runtimePath, SaveDirectory, UserDatabaseFilename)
	repository, closer, err := openUserRepository(config, databasePath)
	if err != nil {
		return nil, nil, fmt.Errorf("profileOpen: %w", err)
	}
	return repository, closer, nil
}
