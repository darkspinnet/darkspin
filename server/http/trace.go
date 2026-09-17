package recaphttp

import (
	"net/http"
	"sort"
	"time"

	"github.com/darkspinnet/darkspin/server/protocoltrace"
)

type traceResponseWriter struct {
	http.ResponseWriter
	status int
	length int
}

func (w *traceResponseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *traceResponseWriter) Write(contents []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	count, err := w.ResponseWriter.Write(contents)
	w.length += count
	return count, err
}

func (w *traceResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// TraceHandler records redacted HTTP exchanges. It records parameter names but
// never values, except for the non-secret API method discriminator.
func TraceHandler(handler http.Handler, recorder protocoltrace.Recorder) http.Handler {
	if recorder == nil {
		return handler
	}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started := time.Now()
		response := &traceResponseWriter{ResponseWriter: writer}
		handler.ServeHTTP(response, request)

		method := request.URL.Query().Get("method")
		if method == "" && request.Form != nil {
			method = request.Form.Get("method")
		}
		recorder.Record(protocoltrace.Event{
			Protocol: "http", Kind: "exchange", Direction: "request",
			Remote: request.RemoteAddr, Path: request.URL.Path, Method: method,
			Keys: requestKeys(request), Status: response.status, Length: response.length,
			DurationUS: time.Since(started).Microseconds(),
		})
	})
}

func requestKeys(request *http.Request) []string {
	keys := make(map[string]struct{})
	for key := range request.URL.Query() {
		keys[key] = struct{}{}
	}
	for key := range request.Form {
		keys[key] = struct{}{}
	}
	values := make([]string, 0, len(keys))
	for key := range keys {
		values = append(values, key)
	}
	sort.Strings(values)
	return values
}
