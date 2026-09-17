package raknet

import (
	"context"
	"fmt"
	"runtime/debug"
)

func invokeHandler(
	ctx context.Context, handler Handler, packet Packet,
) (packets [][]byte, err error) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		packets = nil
		err = fmt.Errorf("handlerPanic: %v\n%s", recovered, debug.Stack())
	}()
	return handler(ctx, packet)
}

func invokeCommit(callback func()) (err error) {
	if callback == nil {
		return nil
	}
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		err = fmt.Errorf("commitPanic: %v\n%s", recovered, debug.Stack())
	}()
	callback()
	return nil
}

func invokeProducer(
	producer func() ([][]byte, error),
) (packets [][]byte, err error) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		packets = nil
		err = fmt.Errorf("producerPanic: %v\n%s", recovered, debug.Stack())
	}()
	return producer()
}
