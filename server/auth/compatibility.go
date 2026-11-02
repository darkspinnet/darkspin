package auth

import (
	"net/http"
	"strings"

	"github.com/darkspinnet/darkspin/server/buildinfo"
)

// ClientVersionHeader carries the launcher's semantic version on desktop auth.
const ClientVersionHeader = "X-Darkspin-Client-Version"

func (e *LocalBroker) compatibleRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		isLoopback, isAllowed := e.requestAccess(request)
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
