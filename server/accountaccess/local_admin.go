package accountaccess

import "context"

type localAdministratorKey struct{}

// WithLocalAdministrator marks an authenticated session owned by a local host.
// A network address alone must never grant this authority on a dedicated host.
func WithLocalAdministrator(ctx context.Context) context.Context {
	if ctx == nil {
		return nil
	}
	return context.WithValue(WithTrustedLocal(ctx), localAdministratorKey{}, true)
}

func IsLocalAdministrator(ctx context.Context) bool {
	return ctx != nil && ctx.Value(localAdministratorKey{}) == true
}
