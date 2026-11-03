package sqlstore

import serverinfosql "github.com/darkspinnet/darkspin/server/serverinfo/sqlstore"

// ServerIdentityStore shares the profile database without initializing local schema.
func (e *Repository) ServerIdentityStore() *serverinfosql.Store {
	return serverinfosql.New(e.db)
}
