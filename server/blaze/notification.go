package blaze

import (
	"context"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/blaze/tdf"
	"github.com/darkspinnet/darkspin/server/databuffer"
)

// notifyWithContext uses the existing notification encoding and frame writer,
// bounding both writer-mutex acquisition and the actual socket write.
func (e *Session) notifyWithContext(ctx context.Context, component, command uint16, fields []tdf.Field) error {
	payload := databuffer.New()
	err := tdf.Encode(payload, fields)
	if err != nil {
		return fmt.Errorf("notifyEncode: %w", err)
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		err = ctx.Err()
		if err != nil {
			return fmt.Errorf("notifyContext: %w", err)
		}
		if e.write.TryLock() {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("notifyWait: %w", ctx.Err())
		case <-ticker.C:
		}
	}
	defer e.write.Unlock()
	timeout := defaultWriteTimeout
	if e.server != nil && e.server.writeTimeout > 0 {
		timeout = e.server.writeTimeout
	}
	deadline := time.Now().Add(timeout)
	contextDeadline, isDeadlineSet := ctx.Deadline()
	if isDeadlineSet && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	err = e.writeFrame(Frame{
		Component: component, Command: command, Type: Notification, Payload: payload.Bytes(),
	}, deadline)
	if err != nil {
		return fmt.Errorf("notifyWrite: %w", err)
	}
	return nil
}
