package playerstat

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

type PVPStore interface {
	RecordPVPMatch(context.Context, int64, sporenet.PVPMatchResult) error
}

func (e *Recorder) RecordPVPMatch(ctx context.Context, userID uint64, req sporenet.PVPMatchResult) error {
	if e == nil || ctx == nil || userID == 0 || userID > uint64(^uint64(0)>>1) {
		return errors.New("PVP stat recorder unavailable")
	}
	store, isAvailable := e.store.(PVPStore)
	if !isAvailable {
		return errors.New("PVP result store unavailable")
	}
	statContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err := store.RecordPVPMatch(statContext, int64(userID), req)
	if err != nil {
		return fmt.Errorf("pvpRecord: %w", err)
	}
	return nil
}
