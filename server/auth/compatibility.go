package auth

import (
	"net/http"
	"strings"

	"github.com/darkspinnet/darkspin/server/accountaccess"
	"github.com/darkspinnet/darkspin/server/buildinfo"
)

// ClientVersionHeader carries the launcher's semantic version on desktop auth.
const ClientVersionHeader = "X-Darkspin-Client-Version"

func (e *LocalBroker) compatibleRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		request = request.WithContext(accountaccess.WithPeer(request.Context(), request.RemoteAddr))
		isLoopback, isAllowed := e.requestAccess(request)
		if isAllowed && request.Method == http.MethodPost && (!isLoopback ||
			request.URL.Path == "/api/desktop/profile" || request.URL.Path == "/api/desktop/delete") {
			if !e.allowCredentialAttempt(request.RemoteAddr) {
				writer.Header().Set("Retry-After", "60")
				writeAuthError(writer, http.StatusTooManyRequests, "too many authentication attempts")
				return
			}
		}
		clientVersion := strings.TrimSpace(request.Header.Get(ClientVersionHeader))
		// Legacy password-free desktop auth is local only. Dedicated brokers
		// classify even loopback callers as remote credentialed clients.
		if isAllowed && (!isLoopback || clientVersion != "") {
			err := buildinfo.CheckCompatibility(clientVersion, buildinfo.Version)
			if err != nil {
				writeAuthError(writer, http.StatusConflict, err.Error())
				return
			}
		}
		next.ServeHTTP(writer, request)
	})
}
