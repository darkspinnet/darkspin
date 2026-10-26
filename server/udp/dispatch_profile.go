package udp

import (
	"context"
	"errors"
	"io"
	"runtime/pprof"
	"strconv"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
)

const (
	dispatchProfileInterval  = 10 * time.Second
	dispatchProfileByteLimit = 128 * 1024
)

// Label the actual dispatch goroutine so a slow-operation profile can separate
// its blocked stack from concurrent polls, snapshot collection and other peers.
func (e *SharedServer) dispatchRakNetWork(
	ctx context.Context, work rakNetPeerWork, server *raknet.Server,
	results chan<- rakNetPeerResult,
) {
	pprof.SetGoroutineLabels(pprof.WithLabels(ctx, pprof.Labels(
		"darkspin.dispatch_trace", strconv.FormatUint(work.traceID, 10),
	)))
	responses, protocol, err := e.processRakNetPacketSafe(
		ctx, work.remote, work.packet, server, work.channel,
	)
	results <- rakNetPeerResult{responses: responses, protocol: protocol, err: err}
}

// The existing five-second timeout stack misses stalls that recover in hundreds
// of milliseconds. Sample at the warning boundary, globally rate-limited per
// server, and keep the evidence in ordinary logs already attached to /bug ZIPs.
func (e *SharedServer) captureDispatchProfile(
	work rakNetPeerWork, startedAt time.Time, queueDelay time.Duration, generation uint64,
) {
	now := time.Now()
	e.dispatchProfileMu.Lock()
	if !e.lastDispatchProfileAt.IsZero() && now.Sub(e.lastDispatchProfileAt) < dispatchProfileInterval {
		e.dispatchProfileMu.Unlock()
		return
	}
	e.lastDispatchProfileAt = now
	e.dispatchProfileMu.Unlock()
	profile := pprof.Lookup("goroutine")
	if profile == nil {
		e.logger.Printf("RakNet dispatch profile unavailable trace=%d: goroutine profile missing", work.traceID)
		return
	}
	buffer := dispatchProfileBuffer{payload: make([]byte, 0, dispatchProfileByteLimit)}
	err := profile.WriteTo(&buffer, 1)
	if err != nil && !(buffer.isTruncated && errors.Is(err, io.ErrShortWrite)) {
		e.logger.Printf("RakNet dispatch profile failed trace=%d: %v", work.traceID, err)
		return
	}
	e.logger.Printf(
		"RakNet slow dispatch profile trace=%d remote=%s generation=%d sampled_at=%s queue_delay=%s dispatch_age=%s profile_duration=%s is_truncated=%t; correlate darkspin.dispatch_trace label; sample may race dispatch completion\n%s",
		work.traceID, work.remote, generation, now.UTC().Format(time.RFC3339Nano),
		queueDelay, now.Sub(startedAt), time.Since(now), buffer.isTruncated, buffer.payload,
	)
}

type dispatchProfileBuffer struct {
	payload     []byte
	isTruncated bool
}

func (e *dispatchProfileBuffer) Write(payload []byte) (int, error) {
	remaining := dispatchProfileByteLimit - len(e.payload)
	if len(payload) <= remaining {
		e.payload = append(e.payload, payload...)
		return len(payload), nil
	}
	e.payload = append(e.payload, payload[:remaining]...)
	e.isTruncated = true
	return remaining, io.ErrShortWrite
}
