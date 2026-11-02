package blaze

import "github.com/darkspinnet/darkspin/server/blaze/tdf"

type loginTokenFailureReporter interface {
	LoginFailure(error) (string, string)
}

// REAS/MESG are Darkspin extensions to Authentication.Login's error reply.
// Build 103's native error UI uses only the numeric error; Fang observes these
// additive fields to explain the rejection through the owning launcher.
func loginFailureResponse(reason, message string) *Response {
	return &Response{
		ErrorCode: authInvalidUser,
		Fields: []tdf.Field{
			tdf.FieldNamed("REAS", tdf.StringValue(reason)),
			tdf.FieldNamed("MESG", tdf.StringValue(message)),
		},
	}
}

func loginTokenFailureResponse(verifier LoginTokenVerifier, err error) *Response {
	reason := "launch-token-invalid"
	message := "The server rejected the launch token. Sign in again from Darkspinner."
	reporter, isReporter := verifier.(loginTokenFailureReporter)
	if isReporter {
		reason, message = reporter.LoginFailure(err)
	}
	return loginFailureResponse(reason, message)
}
