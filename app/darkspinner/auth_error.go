package main

// authServiceError retains the HTTP result while presenting the server's
// readable error instead of dumping its JSON envelope into launcher dialogs.
type authServiceError struct {
	statusCode int
	code       string
	message    string
}

func (e *authServiceError) Error() string {
	return e.message
}
