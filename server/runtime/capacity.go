package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/admissionconfig"
	capacitysql "github.com/darkspinnet/darkspin/server/admissionconfig/sqlstore"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

func initializeCapacity(ctx context.Context, repository sporenet.UserRepository, auditPort admissionconfig.AuditPort, limiter admissionconfig.Limiter) (*admissionconfig.Service, error) {
	provider, isSupported := repository.(interface{ CapacityStore() *capacitysql.Store })
	if !isSupported {
		return nil, errors.New("capacity storage unavailable")
	}
	store := provider.CapacityStore()
	service := admissionconfig.New(store, auditPort, limiter)
	err := service.Apply(ctx)
	if err != nil {
		return nil, fmt.Errorf("capacityApply: %w", err)
	}
	return service, nil
}
