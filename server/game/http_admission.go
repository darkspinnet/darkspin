package game

import (
	"net/http"

	"github.com/darkspinnet/darkspin/server/accountaccess"
	recaphttp "github.com/darkspinnet/darkspin/server/http"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

func observedPeerHandler(handler recaphttp.Handler) recaphttp.Handler {
	return func(writer http.ResponseWriter, request *http.Request, uri *recaphttp.URI) {
		request = request.WithContext(accountaccess.WithPeer(request.Context(), request.RemoteAddr))
		handler(writer, request, uri)
	}
}

// A valid game token still requires authorization from the current HTTP peer.
func (e *API) userFromRequest(request *http.Request, fields interface{ Get(string) string }) *sporenet.User {
	token := authTokenFromRequest(request, fields)
	user := e.userManager.UserByAuthToken(token)
	if user == nil {
		return nil
	}
	authorizer, isSupported := e.userManager.(accountAdmissionAuthorizer)
	if !isSupported {
		return nil
	}
	ctx := accountaccess.WithPeer(request.Context(), request.RemoteAddr)
	err := authorizer.AuthorizeSession(ctx, user.AccountID())
	if err != nil {
		e.logger.Printf("Game HTTP account access denied user=%d: %v", user.AccountID(), err)
		return nil
	}
	return user
}
