// Package loginhistory records the peer addresses observed at accepted logins.
package loginhistory

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"
)

var ErrNotRecorded = errors.New("login IP was not recorded for an active account")

type RecordRequest struct {
	UserID    int64
	IPAddress string
	At        time.Time
}

// Record contains a canonical address without a port or IPv6 zone.
type Record struct {
	UserID    int64
	IPAddress string
	At        time.Time
}

type Store interface {
	Record(ctx context.Context, req Record) error
}

type Service struct {
	store Store
}

func New(store Store) *Service {
	return &Service{store: store}
}

func (e *Service) Record(ctx context.Context, req RecordRequest) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if e == nil || e.store == nil {
		return errors.New("nil login history store")
	}
	if req.UserID <= 0 {
		return errors.New("invalid user ID")
	}
	if req.At.IsZero() {
		return errors.New("missing login time")
	}
	address, err := netip.ParseAddr(strings.TrimSpace(req.IPAddress))
	if err != nil {
		return fmt.Errorf("addressParse: %w", err)
	}
	address = address.WithZone("").Unmap()
	err = e.store.Record(ctx, Record{
		UserID: req.UserID, IPAddress: address.String(), At: req.At.UTC(),
	})
	if err != nil {
		return fmt.Errorf("loginRecord: %w", err)
	}
	return nil
}
