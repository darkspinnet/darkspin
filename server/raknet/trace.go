package raknet

import "context"

type traceIDContextKey struct{}

// WithTraceID associates one socket-ingress identity with all transport and
// gameplay work derived from that datagram.
func WithTraceID(ctx context.Context, traceID uint64) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if traceID == 0 {
		return ctx
	}
	return context.WithValue(ctx, traceIDContextKey{}, traceID)
}

// TraceID returns the socket-ingress identity carried by ctx.
func TraceID(ctx context.Context) uint64 {
	if ctx == nil {
		return 0
	}
	traceID, isTraceID := ctx.Value(traceIDContextKey{}).(uint64)
	if !isTraceID {
		return 0
	}
	return traceID
}
