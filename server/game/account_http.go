package game

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
	"time"
)

var accountHTTPSequence atomic.Uint64

// accountHTTPResponse records delivery metadata, never request parameters,
// credentials, cookies, or account response bodies.
type accountHTTPResponse struct {
	http.ResponseWriter
	logger    *log.Logger
	id        uint64
	method    string
	remoteIP  string
	startedAt time.Time
	accountID int64
	status    int
	byteCount int
	writeErr  error
}

func (e *API) beginAccountHTTP(writer http.ResponseWriter, request *http.Request, method string) *accountHTTPResponse {
	if e.logger == nil || (method != "" && method != "api.account.auth" && method != "api.account.getAccount") {
		return nil
	}
	response := &accountHTTPResponse{
		ResponseWriter: writer, logger: e.logger, id: accountHTTPSequence.Add(1),
		method: method, remoteIP: requestRemoteIP(request.RemoteAddr), startedAt: time.Now(),
	}
	e.logger.Printf("account_http_started request=%d method=%q remote_ip=%q", response.id, response.method, response.remoteIP)
	return response
}

func (e *accountHTTPResponse) WriteHeader(status int) {
	if e.status != 0 {
		return
	}
	// Account responses are protocol XML, including player-controlled setting
	// names. Never let a browser interpret those elements as active content.
	e.Header().Set("Content-Type", "application/xml; charset=utf-8")
	e.Header().Set("X-Content-Type-Options", "nosniff")
	e.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	e.status = status
	e.ResponseWriter.WriteHeader(status)
}

func (e *accountHTTPResponse) Write(payload []byte) (int, error) {
	if e.status == 0 {
		e.WriteHeader(http.StatusOK)
	}
	count, err := e.ResponseWriter.Write(payload)
	e.byteCount += count
	if err != nil {
		e.writeErr = err
		return count, fmt.Errorf("accountWrite: %w", err)
	}
	return count, nil
}

func (e *accountHTTPResponse) Unwrap() http.ResponseWriter { return e.ResponseWriter }

func (e *accountHTTPResponse) finish(ctx context.Context) {
	status := e.status
	if status == 0 {
		status = http.StatusOK
	}
	e.logger.Printf("account_http_completed request=%d method=%q remote_ip=%q user=%d status=%d bytes=%d duration=%s write_error=%v context_error=%v",
		e.id, e.method, e.remoteIP, e.accountID, status, e.byteCount, time.Since(e.startedAt), e.writeErr, ctx.Err())
}
