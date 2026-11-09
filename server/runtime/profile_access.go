package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/accountaccess"
	accessstore "github.com/darkspinnet/darkspin/server/accountaccess/sqlstore"
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/instance"
	"github.com/darkspinnet/darkspin/server/runtime/playerstore"
)

// SetAccountStatus preserves an existing IP restriction while changing status.
func SetAccountStatus(
	ctx context.Context, configFilename string, userID int64, loginName string, status int64,
) (accountaccess.StatusChange, error) {
	change, err := SetAccountStatusWithIPLimit(ctx, configFilename, userID, loginName, status, nil)
	if err != nil {
		return change, fmt.Errorf("statusSet: %w", err)
	}
	return change, nil
}

// SetAccountStatusWithIPLimit atomically provisions status and an optional IP
// restriction. Dedicated PostgreSQL changes run online without schema migration;
// local storage requires an exclusive runtime lease.
func SetAccountStatusWithIPLimit(
	ctx context.Context, configFilename string, userID int64, loginName string, status int64, ipLimit *string,
) (change accountaccess.StatusChange, resultErr error) {
	if status < 0 {
		return change, fmt.Errorf("statusValidate: %w", accountaccess.ErrInvalidStatus)
	}
	if ipLimit != nil {
		normalized, err := accountaccess.NormalizeIPLimit(*ipLimit)
		if err != nil {
			return change, fmt.Errorf("limitValidate: %w", err)
		}
		ipLimit = &normalized
	}
	operator, operationCtx, err := openAccountAccessOperator(ctx, configFilename, userID, loginName)
	if err != nil {
		return change, fmt.Errorf("statusOpen: %w", err)
	}
	defer func() {
		closeErr := operator.Close()
		if closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("statusClose: %w", closeErr))
		}
	}()
	change, err = operator.service.SetStatus(operationCtx, accountaccess.SetStatusRequest{
		UserID: operator.userID, LoginName: operator.loginName, Status: status, IPLimit: ipLimit,
		Reason: "local operator status command",
	})
	if err != nil {
		return change, fmt.Errorf("statusChange: %w", err)
	}
	return change, nil
}

// SetAccountIPLimit changes the network restriction through the audited access
// operation, using the same trusted operator boundary as status provisioning.
func SetAccountIPLimit(
	ctx context.Context, configFilename string, userID int64, loginName, ipLimit string,
) (change accountaccess.IPLimitChange, resultErr error) {
	normalized, err := accountaccess.NormalizeIPLimit(ipLimit)
	if err != nil {
		return change, fmt.Errorf("limitValidate: %w", err)
	}
	operator, operationCtx, err := openAccountAccessOperator(ctx, configFilename, userID, loginName)
	if err != nil {
		return change, fmt.Errorf("limitOpen: %w", err)
	}
	defer func() {
		closeErr := operator.Close()
		if closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("limitClose: %w", closeErr))
		}
	}()
	change, err = operator.service.SetIPLimit(operationCtx, accountaccess.SetIPLimitRequest{
		UserID: operator.userID, LoginName: operator.loginName, IPLimit: normalized,
		Reason: "local operator IP restriction command",
	})
	if err != nil {
		return change, fmt.Errorf("limitChange: %w", err)
	}
	return change, nil
}

type accountAccessOperator struct {
	cancel    context.CancelFunc
	service   *accountaccess.Service
	userID    int64
	loginName string
	closers   []io.Closer
}

func (e *accountAccessOperator) Close() error {
	var resultErr error
	for index := len(e.closers) - 1; index >= 0; index-- {
		err := e.closers[index].Close()
		if err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("accessClose[%d]: %w", index, err))
		}
	}
	if e.cancel != nil {
		e.cancel()
	}
	return resultErr
}

func openAccountAccessOperator(
	ctx context.Context, configFilename string, userID int64, loginName string,
) (operator *accountAccessOperator, operationCtx context.Context, resultErr error) {
	if ctx == nil {
		return nil, nil, errors.New("nil context")
	}
	if (userID > 0) == (loginName != "") || userID < 0 || strings.TrimSpace(loginName) != loginName {
		return nil, nil, errors.New("access requires exactly one positive account ID or exact login name")
	}
	configPath, err := filepath.Abs(configFilename)
	if err != nil {
		return nil, nil, fmt.Errorf("configPath: %w", err)
	}
	var config *game.Config
	var isGenerated bool
	if playerstore.IsEnvironmentConfigured() {
		config, isGenerated, err = game.LoadDedicatedConfig(configPath)
	} else {
		config, isGenerated, err = LoadLocalConfiguration(configPath)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("configLoad: %w", err)
	}
	if isGenerated {
		return nil, nil, errors.New("account access requires an existing server configuration")
	}
	operationCtx = ctx
	operator = &accountAccessOperator{userID: userID, loginName: loginName}
	defer func() {
		if resultErr != nil {
			closeErr := operator.Close()
			if closeErr != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("accessCleanup: %w", closeErr))
			}
		}
	}()
	if config.Bool(game.ConfigServerIsDedicated) {
		configuration, environmentErr := playerstore.FromEnvironment()
		if environmentErr != nil {
			return operator, operationCtx, fmt.Errorf("accessEnvironment: %w", environmentErr)
		}
		operationCtx, operator.cancel = context.WithTimeout(ctx, 10*time.Second)
		database, poolErr := playerstore.OpenPostgresPool(operationCtx, configuration)
		if poolErr != nil {
			return operator, operationCtx, fmt.Errorf("accessPool: %w", poolErr)
		}
		operator.closers = append(operator.closers, database)
		operator.service = accountaccess.New(accessstore.New(database, true))
		return operator, operationCtx, nil
	}
	runtimePath := filepath.Join(filepath.Dir(configPath), DarkspinDirectory)
	lease, err := instance.Acquire(runtimePath)
	if errors.Is(err, instance.ErrRunning) {
		return operator, operationCtx, fmt.Errorf("accessOffline: stop the server before changing account access: %w", err)
	}
	if err != nil {
		return operator, operationCtx, fmt.Errorf("accessLease: %w", err)
	}
	operator.closers = append(operator.closers, lease)
	databasePath := filepath.Join(runtimePath, SaveDirectory, UserDatabaseFilename)
	repository, closer, err := openUserRepository(config, databasePath)
	if err != nil {
		return operator, operationCtx, fmt.Errorf("profileOpen: %w", err)
	}
	operator.closers = append(operator.closers, closer)
	if loginName != "" {
		record, loadErr := repository.LoadByLoginName(ctx, loginName)
		if loadErr != nil {
			return operator, operationCtx, fmt.Errorf("accessResolve: %w", loadErr)
		}
		operator.userID = record.Account.ID
		operator.loginName = ""
	}
	provider, isSupported := repository.(interface {
		AccountAccess() *accessstore.Store
	})
	if !isSupported || provider.AccountAccess() == nil {
		return operator, operationCtx, errors.New("account access storage unavailable")
	}
	operator.service = accountaccess.New(provider.AccountAccess())
	return operator, operationCtx, nil
}
